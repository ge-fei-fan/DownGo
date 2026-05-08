package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/domain"
)

type testRunner struct {
	inspectFn  func(ctx context.Context, settings config.Settings, url string) (domain.InspectResult, error)
	downloadFn func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error
}

func (r *testRunner) Inspect(ctx context.Context, settings config.Settings, url string) (domain.InspectResult, error) {
	return r.inspectFn(ctx, settings, url)
}

func (r *testRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	if r.downloadFn != nil {
		return r.downloadFn(ctx, settings, item, onStart, onProgress)
	}
	return nil
}

func TestCreateReturnsResolvingPlaceholderBeforeInspectCompletes(t *testing.T) {
	t.Parallel()

	releaseInspect := make(chan struct{})
	downloadStarted := make(chan struct{})
	manager, cleanup := newTestManager(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) (domain.InspectResult, error) {
			select {
			case <-releaseInspect:
				return domain.InspectResult{
					NormalizedURL:     "https://www.youtube.com/watch?v=abc123",
					VideoID:           "abc123",
					Title:             "Test Video",
					ThumbnailURL:      "https://img.youtube.com/test.jpg",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video [abc123].mp4",
				}, nil
			case <-ctx.Done():
				return domain.InspectResult{}, ctx.Err()
			}
		},
		downloadFn: func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
			onStart(1234)
			onProgress(domain.StatusDownloading, 35, 2*1024*1024, 12, "", "")
			close(downloadStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer cleanup()

	item, err := manager.Create(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if item.Status != domain.StatusResolving {
		t.Fatalf("Create() status = %q, want %q", item.Status, domain.StatusResolving)
	}
	if item.Title != placeholderTitle {
		t.Fatalf("Create() title = %q, want %q", item.Title, placeholderTitle)
	}

	stored, err := manager.store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.Status != domain.StatusResolving {
		t.Fatalf("stored status = %q, want %q", stored.Status, domain.StatusResolving)
	}

	close(releaseInspect)
	select {
	case <-downloadStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start after inspect resolved")
	}

	waitForDownload(t, 2*time.Second, func() bool {
		current, err := manager.store.GetDownload(item.ID)
		if err != nil {
			return false
		}
		return current.Title == "Test Video" &&
			current.VideoID == "abc123" &&
			current.Status == domain.StatusDownloading &&
			current.ProgressPercent == 35 &&
			current.SpeedBPS > 0
	})
}

func TestCreateKeepsFailedRecordWhenInspectFails(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) (domain.InspectResult, error) {
			return domain.InspectResult{}, errors.New("inspect failed")
		},
	})
	defer cleanup()

	item, err := manager.Create(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForDownload(t, 2*time.Second, func() bool {
		current, err := manager.store.GetDownload(item.ID)
		if err != nil {
			return false
		}
		return current.Status == domain.StatusFailed && strings.Contains(current.ErrorMessage, "inspect failed")
	})
}

func newTestManager(t *testing.T, runner Runner) (*Manager, func()) {
	t.Helper()

	baseDir := t.TempDir()
	defaults := config.Defaults(baseDir)
	if err := os.MkdirAll(filepath.Dir(defaults.YtDlpPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(defaults.YtDlpPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(yt-dlp) error = %v", err)
	}
	if err := os.WriteFile(defaults.FfmpegPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(ffmpeg) error = %v", err)
	}

	store, err := db.Open(baseDir)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	settings, err := config.NewService(store, defaults)
	if err != nil {
		t.Fatalf("config.NewService() error = %v", err)
	}
	manager, err := NewManager(store, settings, runner)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
		_ = store.Close()
	}
}

func waitForDownload(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
