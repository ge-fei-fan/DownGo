package deps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/util"
)

const (
	defaultYtDlpURL    = "http://8.210.7.201:22222/pd/1/yt-dlp.exe"
	defaultFfmpegURL   = "http://8.210.7.201:22222/pd/1/ffmpeg.exe"
	defaultSmartctlURL = "http://8.210.7.201:22222/pd/1/smartctl.exe"
)

type FileStatus struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Downloaded bool   `json:"downloaded"`
	Error      string `json:"error,omitempty"`
}

type Status struct {
	BinDir   string     `json:"binDir"`
	YtDlp    FileStatus `json:"ytDlp"`
	Ffmpeg   FileStatus `json:"ffmpeg"`
	Smartctl FileStatus `json:"smartctl"`
}

type ProgressEvent struct {
	Type       string  `json:"type"`
	Name       string  `json:"name,omitempty"`
	Path       string  `json:"path,omitempty"`
	Bytes      int64   `json:"bytes,omitempty"`
	TotalBytes int64   `json:"totalBytes,omitempty"`
	Percent    float64 `json:"percent,omitempty"`
	Error      string  `json:"error,omitempty"`
	Status     *Status `json:"status,omitempty"`
}

type ProgressEmitter func(ProgressEvent)

type InstallSnapshot struct {
	Installing bool                     `json:"installing"`
	Events     map[string]ProgressEvent `json:"events"`
	Status     Status                   `json:"status"`
	Error      string                   `json:"error,omitempty"`
}

type Service struct {
	mu                 sync.Mutex
	installMu          sync.Mutex
	client             *http.Client
	binDir             string
	ytDlpPath          string
	ffmpegPath         string
	smartctlPath       string
	ytDlpURL           string
	ffmpegURL          string
	smartctlURL        string
	installing         bool
	installEvents      map[string]ProgressEvent
	installStatus      Status
	installError       string
	installSubscribers map[chan ProgressEvent]struct{}
}

func NewService(baseDir string, client *http.Client) *Service {
	return newService(baseDir, client, defaultYtDlpURL, defaultFfmpegURL, defaultSmartctlURL)
}

func newService(baseDir string, client *http.Client, ytDlpURL, ffmpegURL, smartctlURL string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	defaults := config.Defaults(baseDir)
	service := &Service{
		client:             client,
		binDir:             filepath.Dir(defaults.YtDlpPath),
		ytDlpPath:          defaults.YtDlpPath,
		ffmpegPath:         defaults.FfmpegPath,
		smartctlPath:       filepath.Join(filepath.Dir(defaults.YtDlpPath), "smartctl.exe"),
		ytDlpURL:           ytDlpURL,
		ffmpegURL:          ffmpegURL,
		smartctlURL:        smartctlURL,
		installEvents:      map[string]ProgressEvent{},
		installSubscribers: map[chan ProgressEvent]struct{}{},
	}
	service.installStatus = service.Status()
	return service
}

func (s *Service) Status() Status {
	return Status{
		BinDir: s.binDir,
		YtDlp: FileStatus{
			Path:   s.ytDlpPath,
			Exists: util.FileExists(s.ytDlpPath),
		},
		Ffmpeg: FileStatus{
			Path:   s.ffmpegPath,
			Exists: util.FileExists(s.ffmpegPath),
		},
		Smartctl: FileStatus{
			Path:   s.smartctlPath,
			Exists: util.FileExists(s.smartctlPath),
		},
	}
}

func (s *Service) InstallMissing(ctx context.Context) Status {
	return s.InstallMissingWithProgress(ctx, nil)
}

func (s *Service) StartInstall() InstallSnapshot {
	s.installMu.Lock()
	if s.installing {
		snapshot := s.installSnapshotLocked()
		s.installMu.Unlock()
		return snapshot
	}
	s.installing = true
	s.installError = ""
	s.installEvents = map[string]ProgressEvent{}
	s.installStatus = s.Status()
	snapshot := s.installSnapshotLocked()
	s.installMu.Unlock()

	go s.runInstall()
	return snapshot
}

func (s *Service) InstallSnapshot() InstallSnapshot {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if !s.installing {
		s.installStatus = s.Status()
	}
	return s.installSnapshotLocked()
}

func (s *Service) SubscribeInstall() (InstallSnapshot, <-chan ProgressEvent, func()) {
	ch := make(chan ProgressEvent, 32)
	s.installMu.Lock()
	s.installSubscribers[ch] = struct{}{}
	snapshot := s.installSnapshotLocked()
	s.installMu.Unlock()

	return snapshot, ch, func() {
		s.installMu.Lock()
		delete(s.installSubscribers, ch)
		close(ch)
		s.installMu.Unlock()
	}
}

func (s *Service) runInstall() {
	status := s.InstallMissingWithProgress(context.Background(), s.recordInstallEvent)
	s.installMu.Lock()
	s.installing = false
	s.installStatus = status
	s.installMu.Unlock()
}

func (s *Service) InstallMissingWithProgress(ctx context.Context, emit ProgressEmitter) Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.Status()
	if !status.YtDlp.Exists {
		status.YtDlp.Error = s.download(ctx, "yt-dlp.exe", s.ytDlpURL, s.ytDlpPath, emit)
		status.YtDlp.Exists = util.FileExists(s.ytDlpPath)
		status.YtDlp.Downloaded = status.YtDlp.Error == "" && status.YtDlp.Exists
	} else {
		emitProgress(emit, ProgressEvent{Type: "skipped", Name: "yt-dlp.exe", Path: s.ytDlpPath})
	}
	if !status.Ffmpeg.Exists {
		status.Ffmpeg.Error = s.download(ctx, "ffmpeg.exe", s.ffmpegURL, s.ffmpegPath, emit)
		status.Ffmpeg.Exists = util.FileExists(s.ffmpegPath)
		status.Ffmpeg.Downloaded = status.Ffmpeg.Error == "" && status.Ffmpeg.Exists
	} else {
		emitProgress(emit, ProgressEvent{Type: "skipped", Name: "ffmpeg.exe", Path: s.ffmpegPath})
	}
	if !status.Smartctl.Exists {
		status.Smartctl.Error = s.download(ctx, "smartctl.exe", s.smartctlURL, s.smartctlPath, emit)
		status.Smartctl.Exists = util.FileExists(s.smartctlPath)
		status.Smartctl.Downloaded = status.Smartctl.Error == "" && status.Smartctl.Exists
	} else {
		emitProgress(emit, ProgressEvent{Type: "skipped", Name: "smartctl.exe", Path: s.smartctlPath})
	}
	emitProgress(emit, ProgressEvent{Type: "done", Status: &status})
	return status
}

func (s *Service) download(ctx context.Context, name, url, target string, emit ProgressEmitter) string {
	emitProgress(emit, ProgressEvent{Type: "started", Name: name, Path: target})
	if err := util.EnsureDir(filepath.Dir(target)); err != nil {
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: err.Error()})
		return err.Error()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: err.Error()})
		return err.Error()
	}

	resp, err := s.client.Do(req)
	if err != nil {
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: err.Error()})
		return err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		message := fmt.Sprintf("下载失败：%s 返回 %s", url, resp.Status)
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: message})
		return message
	}
	totalBytes := resp.ContentLength
	emitProgress(emit, ProgressEvent{Type: "progress", Name: name, Path: target, TotalBytes: totalBytes})

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")
	if err != nil {
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: err.Error()})
		return err.Error()
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	written, err := copyWithProgress(tmp, resp.Body, totalBytes, func(written int64, percent float64) {
		emitProgress(emit, ProgressEvent{Type: "progress", Name: name, Path: target, Bytes: written, TotalBytes: totalBytes, Percent: percent})
	})
	if err != nil {
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Bytes: written, TotalBytes: totalBytes, Error: err.Error()})
		return err.Error()
	}
	if written == 0 {
		message := "下载失败：文件内容为空"
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: message})
		return message
	}
	if err := tmp.Close(); err != nil {
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: err.Error()})
		return err.Error()
	}

	if err := os.Rename(tmpPath, target); err != nil {
		if util.FileExists(target) {
			emitProgress(emit, ProgressEvent{Type: "completed", Name: name, Path: target, Bytes: written, TotalBytes: totalBytes, Percent: 100})
			return ""
		}
		emitProgress(emit, ProgressEvent{Type: "failed", Name: name, Path: target, Error: err.Error()})
		return err.Error()
	}
	emitProgress(emit, ProgressEvent{Type: "completed", Name: name, Path: target, Bytes: written, TotalBytes: totalBytes, Percent: 100})
	return ""
}

func copyWithProgress(dst io.Writer, src io.Reader, totalBytes int64, onProgress func(int64, float64)) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	lastEmit := time.Now().Add(-time.Second)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			writeN, writeErr := dst.Write(buf[:n])
			written += int64(writeN)
			percent := 0.0
			if totalBytes > 0 {
				percent = float64(written) * 100 / float64(totalBytes)
			}
			if time.Since(lastEmit) >= 150*time.Millisecond || writeErr != nil || readErr == io.EOF {
				onProgress(written, percent)
				lastEmit = time.Now()
			}
			if writeErr != nil {
				return written, writeErr
			}
			if writeN != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func emitProgress(emit ProgressEmitter, event ProgressEvent) {
	if emit != nil {
		emit(event)
	}
}

func (s *Service) recordInstallEvent(event ProgressEvent) {
	s.installMu.Lock()
	if event.Name != "" {
		s.installEvents[event.Name] = event
	}
	if event.Type == "failed" && event.Error != "" {
		s.installError = event.Error
	}
	if event.Status != nil {
		s.installStatus = *event.Status
	}
	for ch := range s.installSubscribers {
		select {
		case ch <- event:
		default:
		}
	}
	s.installMu.Unlock()
}

func (s *Service) installSnapshotLocked() InstallSnapshot {
	events := make(map[string]ProgressEvent, len(s.installEvents))
	for key, event := range s.installEvents {
		events[key] = event
	}
	return InstallSnapshot{
		Installing: s.installing,
		Events:     events,
		Status:     s.installStatus,
		Error:      s.installError,
	}
}
