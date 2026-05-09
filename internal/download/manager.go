package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"example.com/downgo/internal/bilibili"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/util"
)

const (
	placeholderTitle  = "正在解析链接..."
	deleteWaitTimeout = 5 * time.Second
)

type Manager struct {
	baseDir  string
	store    *db.Store
	settings *config.Service
	runner   Runner

	mu           sync.Mutex
	queue        []int64
	active       map[int64]*activeJob
	resolving    map[int64]*resolvingJob
	subscribers  map[chan domain.DownloadEvent]struct{}
	jobs         sync.WaitGroup
	shuttingDown bool
}

type activeJob struct {
	cancel context.CancelFunc
	pid    int
	done   chan struct{}
}

type resolvingJob struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type trackedTaskHandles struct {
	resolving *resolvingJob
	active    *activeJob
}

func NewManager(store *db.Store, settings *config.Service, runner Runner) (*Manager, error) {
	return NewManagerWithBaseDir("", store, settings, runner)
}

func NewManagerWithBaseDir(baseDir string, store *db.Store, settings *config.Service, runner Runner) (*Manager, error) {
	m := &Manager{
		baseDir:     baseDir,
		store:       store,
		settings:    settings,
		runner:      runner,
		active:      map[int64]*activeJob{},
		resolving:   map[int64]*resolvingJob{},
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

func (m *Manager) Inspect(ctx context.Context, url string) ([]domain.InspectResult, error) {
	source, err := normalizeSourceURL(url)
	if err != nil {
		return nil, err
	}

	settings := m.settings.Current()
	if err := ensureBinariesForPlatform(settings, source.Platform); err != nil {
		return nil, err
	}

	metas, err := m.runner.Inspect(ctx, settings, source.URL)
	if err != nil {
		return nil, err
	}
	for i := range metas {
		if metas[i].Platform == "" {
			metas[i].Platform = source.Platform
		}
		duplicate, err := m.findBlockingDuplicate(metas[i].Platform, metas[i].VideoID, 0)
		if err != nil {
			return nil, err
		}
		metas[i].DuplicateOf = duplicate
	}
	return metas, nil
}

func (m *Manager) Create(ctx context.Context, url string) (domain.DownloadItem, error) {
	return m.create(ctx, url, nil)
}

func (m *Manager) CreateWithOrigin(ctx context.Context, url string, origin *domain.FavoriteOrigin) (domain.DownloadItem, error) {
	return m.create(ctx, url, origin)
}

func (m *Manager) create(ctx context.Context, url string, origin *domain.FavoriteOrigin) (domain.DownloadItem, error) {
	source, err := normalizeSourceURL(url)
	if err != nil {
		return domain.DownloadItem{}, err
	}

	settings := m.settings.Current()
	if err := ensureBinariesForPlatform(settings, source.Platform); err != nil {
		return domain.DownloadItem{}, err
	}
	if err := util.EnsureDir(settings.DownloadDir); err != nil {
		return domain.DownloadItem{}, err
	}

	item := domain.DownloadItem{
		SourceURL:       source.URL,
		NormalizedURL:   source.URL,
		Platform:        source.Platform,
		Title:           placeholderTitle,
		Status:          domain.StatusResolving,
		ProgressPercent: 0,
		SpeedBPS:        0,
		ETASeconds:      0,
	}

	item, err = m.store.CreateDownload(item)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	if origin != nil {
		if err := m.store.LinkFavoriteDownload(item.ID, *origin); err != nil {
			return domain.DownloadItem{}, err
		}
	}

	m.broadcast(domain.DownloadEvent{Type: "created", Item: item})
	m.startResolving(item.ID, source.URL)
	return item, nil
}

func (m *Manager) Retry(id int64) (domain.DownloadItem, error) {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	if item.Status != domain.StatusFailed {
		return domain.DownloadItem{}, errors.New("只有失败的任务才能重试")
	}

	if item.VideoID == "" {
		updated, err := m.store.UpdateProgress(id, domain.StatusResolving, 0, 0, 0, 0, "", "", "", nil, nil)
		if err != nil {
			return domain.DownloadItem{}, err
		}
		m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		m.startResolving(id, item.SourceURL)
		return updated, nil
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

	handles := m.stopTrackedTask(id)
	m.cancelTrackedTask(handles)

	now := time.Now().UTC()
	updated, err := m.store.UpdateProgress(id, domain.StatusCanceled, item.ProgressPercent, 0, 0, 0, "用户已取消", item.QualityLabel, item.Container, item.StartedAt, &now)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
	return updated, nil
}

func (m *Manager) Delete(id int64) error {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return err
	}

	if isWorkingStatus(item.Status) {
		handles := m.stopTrackedTask(id)
		m.cancelTrackedTask(handles)
		m.waitForTrackedTask(handles, deleteWaitTimeout)
	} else {
		m.removeQueued(id)
	}

	if item.OutputPath != "" {
		if err := util.DeleteAssociatedFiles(item.OutputPath); err != nil {
			m.scheduleDeleteRetry(item.OutputPath)
		}
	}
	if item.ThumbnailURL == m.thumbnailURL(id) {
		_ = os.Remove(m.thumbnailPath(id))
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
	zap.L().Info("loading download for open path", zap.Int64("id", id))
	item, err := m.store.GetDownload(id)
	if err != nil {
		zap.L().Error("failed to load download for open path", zap.Int64("id", id), zap.Error(err))
		return err
	}
	zap.L().Info(
		"validating download path before opening",
		zap.Int64("id", id),
		zap.String("status", string(item.Status)),
		zap.String("outputPath", item.OutputPath),
	)
	if item.Status != domain.StatusCompleted {
		return errors.New("只有已完成任务才能打开文件路径")
	}
	if !util.FileExists(item.OutputPath) {
		zap.L().Warn("download output file missing", zap.Int64("id", id), zap.String("outputPath", item.OutputPath))
		return errors.New("文件不存在，无法打开文件路径")
	}
	if err := util.OpenFolderAndSelectFile(item.OutputPath); err != nil {
		zap.L().Error("open containing folder failed", zap.Int64("id", id), zap.String("outputPath", item.OutputPath), zap.Error(err))
		return err
	}
	zap.L().Info("open containing folder requested", zap.Int64("id", id), zap.String("outputPath", item.OutputPath))
	return nil
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

func (m *Manager) startResolving(id int64, sourceURL string) {
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return
	}
	if _, ok := m.resolving[id]; ok {
		m.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := &resolvingJob{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.resolving[id] = job
	m.jobs.Add(1)
	m.mu.Unlock()

	go m.resolveAndQueue(ctx, id, sourceURL, job)
}

func (m *Manager) resolveAndQueue(ctx context.Context, id int64, sourceURL string, job *resolvingJob) {
	defer m.jobs.Done()
	defer close(job.done)
	defer m.finishResolving(id, job)

	settings := m.settings.Current()
	metas, err := m.runner.Inspect(ctx, settings, sourceURL)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		updated, updateErr := m.store.UpdateProgress(id, domain.StatusFailed, 0, 0, 0, 0, err.Error(), "", "", nil, nil)
		if updateErr == nil {
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
		return
	}
	if len(metas) == 0 {
		updated, updateErr := m.store.UpdateProgress(id, domain.StatusFailed, 0, 0, 0, 0, "未解析到可下载视频", "", "", nil, nil)
		if updateErr == nil {
			m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		}
		return
	}

	placeholder, err := m.store.GetDownload(id)
	if err != nil {
		return
	}
	platform := placeholder.Platform
	if platform == "" {
		platform = metas[0].Platform
	}
	if platform == "" {
		platform = domain.PlatformYouTube
	}
	for i := range metas {
		if metas[i].Platform == "" {
			metas[i].Platform = platform
		}
	}
	origins, err := m.store.FavoriteOriginsForDownload(id)
	if err != nil {
		origins = nil
		zap.L().Warn("load favorite origins for download failed", zap.Int64("id", id), zap.Error(err))
	}

	for index, meta := range metas {
		targetID := id
		if index > 0 {
			created, err := m.store.CreateDownload(domain.DownloadItem{
				SourceURL:       sourceURL,
				NormalizedURL:   meta.NormalizedURL,
				Platform:        meta.Platform,
				Title:           placeholderTitle,
				Status:          domain.StatusResolving,
				ProgressPercent: 0,
			})
			if err != nil {
				continue
			}
			targetID = created.ID
			for _, origin := range origins {
				if err := m.store.LinkFavoriteDownload(targetID, origin); err != nil {
					zap.L().Warn("link favorite origin to expanded download failed", zap.Int64("downloadId", targetID), zap.Error(err))
				}
			}
			m.broadcast(domain.DownloadEvent{Type: "created", Item: created})
		}

		duplicate, err := m.findBlockingDuplicate(meta.Platform, meta.VideoID, targetID)
		if err != nil {
			updated, updateErr := m.store.UpdateProgress(targetID, domain.StatusFailed, 0, 0, 0, 0, err.Error(), "", "", nil, nil)
			if updateErr == nil {
				m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
			}
			continue
		}
		if duplicate != nil {
			m.linkFavoriteOriginsToDownload(duplicate.ID, origins)
			if err := m.removeDuplicatePlaceholder(targetID); err != nil {
				zap.L().Warn("remove duplicate placeholder failed", zap.Int64("id", targetID), zap.Error(err))
			}
			continue
		}
		update := domain.DownloadItem{
			NormalizedURL:  meta.NormalizedURL,
			Platform:       meta.Platform,
			VideoID:        meta.VideoID,
			Title:          meta.Title,
			ThumbnailURL:   m.cacheThumbnail(ctx, targetID, meta),
			QualityLabel:   meta.QualityLabel,
			Container:      meta.Container,
			OutputFilename: meta.SuggestedFilename,
			OutputPath:     filepath.Join(settings.DownloadDir, meta.SuggestedFilename),
		}

		updated, err := m.store.UpdateMetadata(targetID, update, domain.StatusQueued, "")
		if err != nil {
			if errors.Is(err, db.ErrDuplicate) {
				if duplicate, findErr := m.findBlockingDuplicate(meta.Platform, meta.VideoID, targetID); findErr != nil {
					zap.L().Warn("find duplicate after metadata update failed", zap.Int64("id", targetID), zap.Error(findErr))
				} else if duplicate != nil {
					m.linkFavoriteOriginsToDownload(duplicate.ID, origins)
				}
				if removeErr := m.removeDuplicatePlaceholder(targetID); removeErr != nil {
					zap.L().Warn("remove duplicate placeholder after metadata update failed", zap.Int64("id", targetID), zap.Error(removeErr))
				}
				continue
			}
			errMsg := err.Error()
			failed, updateErr := m.store.UpdateProgress(targetID, domain.StatusFailed, 0, 0, 0, 0, errMsg, "", "", nil, nil)
			if updateErr == nil {
				m.broadcast(domain.DownloadEvent{Type: "updated", Item: failed})
			}
			continue
		}

		m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
		m.enqueue(targetID)
	}
}

func (m *Manager) linkFavoriteOriginsToDownload(downloadID int64, origins []domain.FavoriteOrigin) {
	for _, origin := range origins {
		if err := m.store.LinkFavoriteDownload(downloadID, origin); err != nil {
			zap.L().Warn("link favorite origin to duplicate download failed", zap.Int64("downloadId", downloadID), zap.Error(err))
		}
	}
}

func (m *Manager) removeDuplicatePlaceholder(id int64) error {
	item, err := m.store.GetDownload(id)
	if err != nil {
		return err
	}
	if err := m.store.MarkDeleted(id); err != nil {
		return err
	}
	m.broadcast(domain.DownloadEvent{Type: "removed", Item: item})
	return nil
}

func (m *Manager) cacheThumbnail(ctx context.Context, id int64, meta domain.InspectResult) string {
	if m.baseDir == "" || meta.Platform != domain.PlatformBilibili || meta.ThumbnailURL == "" {
		return meta.ThumbnailURL
	}
	targetPath := m.thumbnailPath(id)
	if err := bilibili.NewClient(nil).DownloadImage(ctx, meta.ThumbnailURL, targetPath); err != nil {
		zap.L().Warn("cache bilibili thumbnail failed", zap.Int64("id", id), zap.String("thumbnailUrl", meta.ThumbnailURL), zap.Error(err))
		return meta.ThumbnailURL
	}
	return m.thumbnailURL(id)
}

func (m *Manager) ThumbnailPath(id int64) string {
	return m.thumbnailPath(id)
}

func (m *Manager) thumbnailPath(id int64) string {
	return filepath.Join(m.baseDir, "data", "bilibili", "covers", strconv.FormatInt(id, 10))
}

func (m *Manager) thumbnailURL(id int64) string {
	return "/api/downloads/" + strconv.FormatInt(id, 10) + "/thumbnail"
}

func (m *Manager) enqueue(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shuttingDown {
		return
	}

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
	if m.shuttingDown {
		return
	}

	limit := m.settings.Current().ConcurrentDownloads
	if limit <= 0 {
		limit = 1
	}

	for len(m.active) < limit && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]

		ctx, cancel := context.WithCancel(context.Background())
		job := &activeJob{
			cancel: cancel,
			done:   make(chan struct{}),
		}
		m.active[id] = job
		m.jobs.Add(1)
		go m.runJob(ctx, id, job)
	}
}

func (m *Manager) runJob(ctx context.Context, id int64, job *activeJob) {
	defer m.jobs.Done()
	defer close(job.done)
	defer m.finishActive(id, job)

	item, err := m.store.GetDownload(id)
	if err != nil {
		return
	}

	settings := m.settings.Current()
	startedAt := time.Now().UTC()
	currentQualityLabel := item.QualityLabel
	currentContainer := item.Container
	if updated, err := m.store.UpdateProgress(id, domain.StatusDownloading, 0, 0, 0, 0, "", currentQualityLabel, currentContainer, &startedAt, nil); err == nil {
		m.broadcast(domain.DownloadEvent{Type: "updated", Item: updated})
	}

	onStart := func(pid int) {
		m.mu.Lock()
		if current, ok := m.active[id]; ok && current == job {
			current.pid = pid
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
}

func (m *Manager) finishResolving(id int64, job *resolvingJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.resolving[id]; ok && current == job {
		delete(m.resolving, id)
	}
}

func (m *Manager) finishActive(id int64, job *activeJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.active[id]; ok && current == job {
		delete(m.active, id)
	}
	m.scheduleLocked()
}

func (m *Manager) stopTrackedTask(id int64) trackedTaskHandles {
	m.mu.Lock()
	defer m.mu.Unlock()

	handles := trackedTaskHandles{
		resolving: m.resolving[id],
		active:    m.active[id],
	}

	if handles.resolving != nil {
		delete(m.resolving, id)
	}
	if handles.active != nil {
		delete(m.active, id)
	}
	m.removeQueuedLocked(id)
	if handles.active != nil {
		m.scheduleLocked()
	}

	return handles
}

func (m *Manager) cancelTrackedTask(handles trackedTaskHandles) {
	if handles.resolving != nil {
		handles.resolving.cancel()
	}
	if handles.active != nil {
		if handles.active.pid > 0 {
			_ = KillProcessTree(handles.active.pid)
		}
		handles.active.cancel()
	}
}

func (m *Manager) waitForTrackedTask(handles trackedTaskHandles, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if handles.resolving != nil {
		select {
		case <-handles.resolving.done:
		case <-ctx.Done():
			return
		}
	}
	if handles.active != nil {
		select {
		case <-handles.active.done:
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) removeQueued(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeQueuedLocked(id)
}

func (m *Manager) removeQueuedLocked(id int64) {
	if len(m.queue) == 0 {
		return
	}
	next := m.queue[:0]
	for _, queuedID := range m.queue {
		if queuedID != id {
			next = append(next, queuedID)
		}
	}
	m.queue = next
}

func (m *Manager) cancelResolving(id int64) {
	m.mu.Lock()
	job := m.resolving[id]
	if job != nil {
		delete(m.resolving, id)
	}
	m.mu.Unlock()
	if job != nil {
		job.cancel()
	}
}

func (m *Manager) scheduleDeleteRetry(outputPath string) {
	go func(path string) {
		for attempt := 0; attempt < 5; attempt++ {
			time.Sleep(time.Second)
			if err := util.DeleteAssociatedFiles(path); err == nil {
				return
			}
		}
	}(outputPath)
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.shuttingDown = true
	m.queue = nil

	resolveJobs := make([]*resolvingJob, 0, len(m.resolving))
	for id, job := range m.resolving {
		resolveJobs = append(resolveJobs, job)
		delete(m.resolving, id)
	}

	activeJobs := make([]*activeJob, 0, len(m.active))
	for id, job := range m.active {
		activeJobs = append(activeJobs, job)
		delete(m.active, id)
	}
	m.mu.Unlock()

	for _, job := range resolveJobs {
		job.cancel()
	}
	for _, job := range activeJobs {
		if job.pid > 0 {
			_ = KillProcessTree(job.pid)
		}
		job.cancel()
	}

	done := make(chan struct{})
	go func() {
		m.jobs.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

func (m *Manager) findBlockingDuplicate(platform string, videoID string, excludeID int64) (*domain.DownloadItem, error) {
	items, err := m.store.FindByPlatformVideoID(platform, videoID)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		if excludeID > 0 && item.ID == excludeID {
			continue
		}

		switch item.Status {
		case domain.StatusResolving, domain.StatusQueued, domain.StatusDownloading, domain.StatusPostprocessing:
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

func ensureBinariesForPlatform(settings config.Settings, platform string) error {
	if platform == domain.PlatformYouTube && !util.FileExists(settings.YtDlpPath) {
		return errors.New("未找到 yt-dlp.exe，请到设置页下载安装依赖或手动配置路径")
	}
	if !util.FileExists(settings.FfmpegPath) {
		return errors.New("未找到 ffmpeg.exe，请到设置页下载安装依赖或手动配置路径")
	}
	return nil
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

func isWorkingStatus(status string) bool {
	switch status {
	case domain.StatusResolving, domain.StatusQueued, domain.StatusDownloading, domain.StatusPostprocessing:
		return true
	default:
		return false
	}
}

type ErrAlreadyDownloaded struct {
	Existing domain.DownloadItem
}

func (e ErrAlreadyDownloaded) Error() string {
	return "该下载任务已存在"
}
