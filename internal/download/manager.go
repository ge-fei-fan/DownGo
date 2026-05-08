package download

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/util"
)

type Manager struct {
	store    *db.Store
	settings *config.Service
	runner   Runner

	mu          sync.Mutex
	queue       []int64
	active      map[int64]*activeJob
	subscribers map[chan domain.DownloadEvent]struct{}
}

type activeJob struct {
	cancel context.CancelFunc
	pid    int
}

func NewManager(store *db.Store, settings *config.Service, runner Runner) (*Manager, error) {
	m := &Manager{
		store:       store,
		settings:    settings,
		runner:      runner,
		active:      map[int64]*activeJob{},
		subscribers: map[chan domain.DownloadEvent]struct{}{},
	}

	if err := m.store.MarkStaleActiveAsFailed(); err != nil {
		return nil, err
	}
	if repaired, err := m.store.MarkMissingCompletedAsFailed(); err != nil {
		return nil, err
	} else {
		for _, item := range repaired {
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: item})
		}
	}
	return m, nil
}

func (m *Manager) Inspect(ctx context.Context, url string) (domain.InspectResult, error) {
	normalizedInput, err := normalizeYouTubeURL(url)
	if err != nil {
		return domain.InspectResult{}, err
	}

	settings := m.settings.Current()
	if err := ensureBinaries(settings); err != nil {
		return domain.InspectResult{}, err
	}

	meta, err := m.runner.Inspect(ctx, settings, normalizedInput)
	if err != nil {
		return domain.InspectResult{}, err
	}
	duplicate, err := m.findBlockingDuplicate(meta.VideoID)
	if err != nil {
		return domain.InspectResult{}, err
	}
	meta.DuplicateOf = duplicate
	return meta, nil
}

func (m *Manager) Create(ctx context.Context, url string) (domain.DownloadItem, error) {
	sourceURL, err := normalizeYouTubeURL(url)
	if err != nil {
		return domain.DownloadItem{}, err
	}

	meta, err := m.Inspect(ctx, sourceURL)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	if meta.DuplicateOf != nil {
		return domain.DownloadItem{}, ErrAlreadyDownloaded{Existing: *meta.DuplicateOf}
	}

	settings := m.settings.Current()
	if err := util.EnsureDir(settings.DownloadDir); err != nil {
		return domain.DownloadItem{}, err
	}

	item := domain.DownloadItem{
		SourceURL:       sourceURL,
		NormalizedURL:   meta.NormalizedURL,
		Platform:        domain.PlatformYouTube,
		VideoID:         meta.VideoID,
		Title:           meta.Title,
		ThumbnailURL:    meta.ThumbnailURL,
		QualityLabel:    meta.QualityLabel,
		Container:       meta.Container,
		OutputFilename:  meta.SuggestedFilename,
		OutputPath:      filepath.Join(settings.DownloadDir, meta.SuggestedFilename),
		Status:          domain.StatusQueued,
		ProgressPercent: 0,
		SpeedBPS:        0,
		ETASeconds:      0,
	}

	item, err = m.store.CreateDownload(item)
	if err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			duplicate, dupErr := m.findBlockingDuplicate(item.VideoID)
			if dupErr != nil {
				return domain.DownloadItem{}, dupErr
			}
			if duplicate != nil {
				return domain.DownloadItem{}, ErrAlreadyDownloaded{Existing: *duplicate}
			}
		}
		return domain.DownloadItem{}, err
	}

	m.broadcast(domain.DownloadEvent{Type: "created", Item: item})
	m.enqueue(item.ID)
	return item, nil
}

func (m *Manager) Retry(id int64) (domain.DownloadItem, error) {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	if item.Status != domain.StatusFailed && item.Status != domain.StatusCanceled {
		return domain.DownloadItem{}, errors.New("只有失败或已取消的任务才能重试")
	}

	updated, err := m.store.UpdateProgress(id, domain.StatusQueued, 0, 0, 0, 0, "", item.QualityLabel, item.Container, nil, nil)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
	m.enqueue(id)
	return updated, nil
}

func (m *Manager) Cancel(id int64) (domain.DownloadItem, error) {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return domain.DownloadItem{}, err
	}

	m.mu.Lock()
	job, ok := m.active[id]
	m.mu.Unlock()

	if ok {
		if job.pid > 0 {
			_ = KillProcessTree(job.pid)
		}
		job.cancel()
	}

	now := time.Now().UTC()
	updated, err := m.store.UpdateProgress(id, domain.StatusCanceled, item.ProgressPercent, 0, 0, 0, "用户已取消", item.QualityLabel, item.Container, item.StartedAt, &now)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	m.finishActive(id)
	m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
	return updated, nil
}

func (m *Manager) Delete(id int64) error {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return err
	}

	if item.Status == domain.StatusQueued || item.Status == domain.StatusDownloading || item.Status == domain.StatusPostprocessing {
		if _, err := m.Cancel(id); err != nil {
			return err
		}
	}

	if err := util.DeleteAssociatedFiles(item.OutputPath); err != nil {
		return err
	}
	if err := m.store.MarkDeleted(id); err != nil {
		return err
	}

	m.broadcast(domain.DownloadEvent{Type: "removed", Item: item})
	return nil
}

func (m *Manager) List(view string, page int, pageSize int) (domain.PagedDownloads, error) {
	return m.store.ListDownloads(view, page, pageSize)
}

func (m *Manager) Get(id int64) (domain.DownloadItem, error) {
	return m.store.GetDownload(id)
}

func (m *Manager) OpenPath(id int64) error {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return err
	}
	if item.Status != domain.StatusCompleted {
		return errors.New("只有已完成任务才能打开文件路径")
	}
	if !util.FileExists(item.OutputPath) {
		return errors.New("文件不存在，无法打开文件路径")
	}
	return util.OpenFolderAndSelectFile(item.OutputPath)
}

func (m *Manager) Subscribe() (<-chan domain.DownloadEvent, func()) {
	ch := make(chan domain.DownloadEvent, 16)
	m.mu.Lock()
	m.subscribers[ch] = struct{}{}
	m.mu.Unlock()

	return ch, func() {
		m.mu.Lock()
		delete(m.subscribers, ch)
		close(ch)
		m.mu.Unlock()
	}
}

func (m *Manager) enqueue(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.queue {
		if existing == id {
			return
		}
	}
	if _, ok := m.active[id]; ok {
		return
	}

	m.queue = append(m.queue, id)
	m.scheduleLocked()
}

func (m *Manager) scheduleLocked() {
	limit := m.settings.Current().ConcurrentDownloads
	if limit <= 0 {
		limit = 1
	}

	for len(m.active) < limit && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		ctx, cancel := context.WithCancel(context.Background())
		m.active[id] = &activeJob{cancel: cancel}
		go m.runJob(ctx, id)
	}
}

func (m *Manager) runJob(ctx context.Context, id int64) {
	item, err := m.store.GetDownload(id)
	if err != nil {
		m.finishActive(id)
		return
	}

	settings := m.settings.Current()
	startedAt := time.Now().UTC()
	currentQualityLabel := item.QualityLabel
	currentContainer := item.Container
	_, _ = m.store.UpdateProgress(id, domain.StatusDownloading, 0, 0, 0, 0, "", currentQualityLabel, currentContainer, &startedAt, nil)

	onStart := func(pid int) {
		m.mu.Lock()
		if job, ok := m.active[id]; ok {
			job.pid = pid
		}
		m.mu.Unlock()
		updated, err := m.store.UpdateProgress(id, domain.StatusDownloading, 0, 0, 0, pid, "", currentQualityLabel, currentContainer, &startedAt, nil)
		if err == nil {
			currentQualityLabel = updated.QualityLabel
			currentContainer = updated.Container
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
	}
	onProgress := func(status string, percent float64, speed float64, eta int64, qualityLabel string, container string) {
		if qualityLabel != "" {
			currentQualityLabel = qualityLabel
		}
		if container != "" {
			currentContainer = container
		}
		updated, err := m.store.UpdateProgress(id, status, percent, speed, eta, m.activePID(id), "", currentQualityLabel, currentContainer, &startedAt, nil)
		if err == nil {
			currentQualityLabel = updated.QualityLabel
			currentContainer = updated.Container
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
	}

	err = m.runner.Download(ctx, settings, item, onStart, onProgress)
	completedAt := time.Now().UTC()
	switch {
	case err == nil:
		updated, updateErr := m.store.UpdateProgress(id, domain.StatusCompleted, 100, 0, 0, 0, "", currentQualityLabel, currentContainer, &startedAt, &completedAt)
		if updateErr == nil {
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
	case errors.Is(err, context.Canceled):
		updated, updateErr := m.store.UpdateProgress(id, domain.StatusCanceled, item.ProgressPercent, 0, 0, 0, "用户已取消", currentQualityLabel, currentContainer, &startedAt, &completedAt)
		if updateErr == nil {
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
	default:
		updated, updateErr := m.store.UpdateProgress(id, domain.StatusFailed, item.ProgressPercent, 0, 0, 0, err.Error(), currentQualityLabel, currentContainer, &startedAt, &completedAt)
		if updateErr == nil {
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
	}

	m.finishActive(id)
}

func (m *Manager) finishActive(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, id)
	m.scheduleLocked()
}

func (m *Manager) activePID(id int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job, ok := m.active[id]; ok {
		return job.pid
	}
	return 0
}

func (m *Manager) broadcast(event domain.DownloadEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *Manager) findBlockingDuplicate(videoID string) (*domain.DownloadItem, error) {
	items, err := m.store.FindByVideoID(videoID)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		switch item.Status {
		case domain.StatusQueued, domain.StatusDownloading, domain.StatusPostprocessing:
			return &item, nil
		case domain.StatusCompleted:
			if util.FileExists(item.OutputPath) {
				return &item, nil
			}
			if _, err := m.store.UpdateProgress(item.ID, domain.StatusFailed, 0, 0, 0, 0, "文件丢失", item.QualityLabel, item.Container, item.StartedAt, nil); err != nil {
				return nil, err
			}
		}
	}
	return nil, nil
}

func ensureBinaries(settings config.Settings) error {
	if !util.FileExists(settings.YtDlpPath) {
		return errors.New("未找到 yt-dlp.exe，请到设置页下载安装依赖或手动配置路径")
	}
	if !util.FileExists(settings.FfmpegPath) {
		return errors.New("未找到 ffmpeg.exe，请到设置页下载安装依赖或手动配置路径")
	}
	return nil
}

type ErrAlreadyDownloaded struct {
	Existing domain.DownloadItem
}

func (e ErrAlreadyDownloaded) Error() string {
	return "该下载任务已存在"
}
