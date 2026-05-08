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
	defaultYtDlpURL  = "http://8.210.7.201:22222/pd/1/yt-dlp.exe"
	defaultFfmpegURL = "http://8.210.7.201:22222/pd/1/ffmpeg.exe"
)

type FileStatus struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Downloaded bool   `json:"downloaded"`
	Error      string `json:"error,omitempty"`
}

type Status struct {
	BinDir string     `json:"binDir"`
	YtDlp  FileStatus `json:"ytDlp"`
	Ffmpeg FileStatus `json:"ffmpeg"`
}

type Service struct {
	mu         sync.Mutex
	client     *http.Client
	binDir     string
	ytDlpPath  string
	ffmpegPath string
	ytDlpURL   string
	ffmpegURL  string
}

func NewService(baseDir string, client *http.Client) *Service {
	return newService(baseDir, client, defaultYtDlpURL, defaultFfmpegURL)
}

func newService(baseDir string, client *http.Client, ytDlpURL, ffmpegURL string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	defaults := config.Defaults(baseDir)
	return &Service{
		client:     client,
		binDir:     filepath.Dir(defaults.YtDlpPath),
		ytDlpPath:  defaults.YtDlpPath,
		ffmpegPath: defaults.FfmpegPath,
		ytDlpURL:   ytDlpURL,
		ffmpegURL:  ffmpegURL,
	}
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
	}
}

func (s *Service) InstallMissing(ctx context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := s.Status()
	if !status.YtDlp.Exists {
		status.YtDlp.Error = s.download(ctx, s.ytDlpURL, s.ytDlpPath)
		status.YtDlp.Exists = util.FileExists(s.ytDlpPath)
		status.YtDlp.Downloaded = status.YtDlp.Error == "" && status.YtDlp.Exists
	}
	if !status.Ffmpeg.Exists {
		status.Ffmpeg.Error = s.download(ctx, s.ffmpegURL, s.ffmpegPath)
		status.Ffmpeg.Exists = util.FileExists(s.ffmpegPath)
		status.Ffmpeg.Downloaded = status.Ffmpeg.Error == "" && status.Ffmpeg.Exists
	}
	return status
}

func (s *Service) download(ctx context.Context, url, target string) string {
	if err := util.EnsureDir(filepath.Dir(target)); err != nil {
		return err.Error()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err.Error()
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("下载失败：%s 返回 %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")
	if err != nil {
		return err.Error()
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	written, err := io.Copy(tmp, resp.Body)
	if err != nil {
		return err.Error()
	}
	if written == 0 {
		return "下载失败：文件内容为空"
	}
	if err := tmp.Close(); err != nil {
		return err.Error()
	}

	if err := os.Rename(tmpPath, target); err != nil {
		if util.FileExists(target) {
			return ""
		}
		return err.Error()
	}
	return ""
}
