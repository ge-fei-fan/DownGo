package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/domain"
)

type testRunner struct {
	inspectFn  func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error)
	downloadFn func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error
}

func (r *testRunner) Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
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
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			select {
			case <-releaseInspect:
				return []domain.InspectResult{{
					Platform:          domain.PlatformYouTube,
					NormalizedURL:     "https://www.youtube.com/watch?v=abc123",
					VideoID:           "abc123",
					Title:             "Test Video",
					ThumbnailURL:      "https://img.youtube.com/test.jpg",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video [abc123].mp4",
				}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
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
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return nil, errors.New("inspect failed")
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

func TestCreateExpandsMultipleInspectResultsIntoTasks(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return []domain.InspectResult{
				{
					Platform:          domain.PlatformBilibili,
					NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=1",
					VideoID:           "BV1xx411c7mD#1001",
					Title:             "Test Video - P01 Intro",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video - P01 Intro [BV1xx411c7mD#1001].mp4",
				},
				{
					Platform:          domain.PlatformBilibili,
					NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=2",
					VideoID:           "BV1xx411c7mD#1002",
					Title:             "Test Video - P02 Middle",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video - P02 Middle [BV1xx411c7mD#1002].mp4",
				},
				{
					Platform:          domain.PlatformBilibili,
					NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=3",
					VideoID:           "BV1xx411c7mD#1003",
					Title:             "Test Video - P03 End",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video - P03 End [BV1xx411c7mD#1003].mp4",
				},
			}, nil
		},
		downloadFn: func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer cleanup()

	if _, err := manager.Create(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD?p=1"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForDownload(t, 2*time.Second, func() bool {
		result, err := manager.List("active", 1, 20)
		if err != nil || result.Total != 3 || len(result.Items) != 3 {
			return false
		}

		seen := map[string]bool{}
		for _, item := range result.Items {
			seen[item.VideoID] = true
			if item.Platform != domain.PlatformBilibili {
				return false
			}
			if item.Status != domain.StatusQueued && item.Status != domain.StatusDownloading {
				return false
			}
		}
		return seen["BV1xx411c7mD#1001"] && seen["BV1xx411c7mD#1002"] && seen["BV1xx411c7mD#1003"]
	})
}

func TestCreateRemovesDuplicatePlaceholder(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return []domain.InspectResult{{
				Platform:          domain.PlatformYouTube,
				NormalizedURL:     "https://www.youtube.com/watch?v=dup123",
				VideoID:           "dup123",
				Title:             "Duplicate Video",
				QualityLabel:      "1080p",
				Container:         "mp4",
				SuggestedFilename: "Duplicate Video [dup123].mp4",
			}}, nil
		},
	})
	defer cleanup()

	existing, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:       "https://www.youtube.com/watch?v=dup123",
		NormalizedURL:   "https://www.youtube.com/watch?v=dup123",
		Platform:        domain.PlatformYouTube,
		VideoID:         "dup123",
		Title:           "Existing Video",
		Status:          domain.StatusQueued,
		OutputFilename:  "Existing Video [dup123].mp4",
		OutputPath:      filepath.Join(t.TempDir(), "Existing Video [dup123].mp4"),
		ProgressPercent: 0,
	})
	if err != nil {
		t.Fatalf("CreateDownload(existing) error = %v", err)
	}

	placeholder, err := manager.Create(context.Background(), "https://www.youtube.com/watch?v=dup123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForDownload(t, 2*time.Second, func() bool {
		result, err := manager.List("active", 1, 20)
		if err != nil || result.Total != 1 || len(result.Items) != 1 {
			return false
		}
		if result.Items[0].ID != existing.ID {
			return false
		}
		deleted, err := manager.store.GetDownload(placeholder.ID)
		return err == nil && deleted.DeletedAt != nil
	})
}

func TestCreateRemovesDuplicateExpandedPageOnly(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return []domain.InspectResult{
				{
					Platform:          domain.PlatformBilibili,
					NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=1",
					VideoID:           "BV1xx411c7mD#1001",
					Title:             "Test Video - P01 Intro",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video - P01 Intro [BV1xx411c7mD#1001].mp4",
				},
				{
					Platform:          domain.PlatformBilibili,
					NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=2",
					VideoID:           "BV1xx411c7mD#1002",
					Title:             "Test Video - P02 Middle",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video - P02 Middle [BV1xx411c7mD#1002].mp4",
				},
				{
					Platform:          domain.PlatformBilibili,
					NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=3",
					VideoID:           "BV1xx411c7mD#1003",
					Title:             "Test Video - P03 End",
					QualityLabel:      "1080p",
					Container:         "mp4",
					SuggestedFilename: "Test Video - P03 End [BV1xx411c7mD#1003].mp4",
				},
			}, nil
		},
		downloadFn: func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer cleanup()

	existing, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=2",
		NormalizedURL: "https://www.bilibili.com/video/BV1xx411c7mD?p=2",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1xx411c7mD#1002",
		Title:         "Existing P02",
		Status:        domain.StatusQueued,
		OutputPath:    filepath.Join(t.TempDir(), "Existing P02.mp4"),
	})
	if err != nil {
		t.Fatalf("CreateDownload(existing) error = %v", err)
	}

	if _, err := manager.Create(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD?p=1"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForDownload(t, 2*time.Second, func() bool {
		result, err := manager.List("active", 1, 20)
		if err != nil || result.Total != 3 || len(result.Items) != 3 {
			return false
		}
		seen := map[string]bool{}
		for _, item := range result.Items {
			seen[item.VideoID] = true
			if item.VideoID == "BV1xx411c7mD#1002" && item.ID != existing.ID {
				return false
			}
			if item.Status == domain.StatusFailed {
				return false
			}
		}
		return seen["BV1xx411c7mD#1001"] && seen["BV1xx411c7mD#1002"] && seen["BV1xx411c7mD#1003"]
	})
}

func TestCreateWithOriginLinksDuplicateToExistingDownload(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return []domain.InspectResult{{
				Platform:          domain.PlatformBilibili,
				NormalizedURL:     "https://www.bilibili.com/video/BV1dup?p=1",
				VideoID:           "BV1dup#1001",
				Title:             "Favorite Duplicate",
				QualityLabel:      "1080p",
				Container:         "mp4",
				SuggestedFilename: "Favorite Duplicate [BV1dup#1001].mp4",
			}}, nil
		},
	})
	defer cleanup()

	existing, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1dup?p=1",
		NormalizedURL: "https://www.bilibili.com/video/BV1dup?p=1",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1dup#1001",
		Title:         "Existing Favorite Duplicate",
		Status:        domain.StatusQueued,
		OutputPath:    filepath.Join(t.TempDir(), "Existing Favorite Duplicate.mp4"),
	})
	if err != nil {
		t.Fatalf("CreateDownload(existing) error = %v", err)
	}

	origin := domain.FavoriteOrigin{
		MediaID:      101,
		ResourceID:   202,
		ResourceType: 2,
		Bvid:         "BV1dup",
		Title:        "Favorite Duplicate",
	}
	if err := manager.store.UpsertFavoriteResource(origin, "queued", ""); err != nil {
		t.Fatalf("UpsertFavoriteResource() error = %v", err)
	}

	placeholder, err := manager.CreateWithOrigin(context.Background(), "https://www.bilibili.com/video/BV1dup", &origin)
	if err != nil {
		t.Fatalf("CreateWithOrigin() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, err := manager.store.DownloadsForFavoriteOrigin(origin)
		deleted, getErr := manager.store.GetDownload(placeholder.ID)
		if err == nil && len(items) == 1 && items[0].ID == existing.ID && getErr == nil && deleted.DeletedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	items, err := manager.store.DownloadsForFavoriteOrigin(origin)
	deleted, getErr := manager.store.GetDownload(placeholder.ID)
	t.Fatalf("favorite duplicate not linked/removed: items=%+v itemsErr=%v placeholder=%+v placeholderErr=%v", items, err, deleted, getErr)
}

func TestCreateCachesBilibiliThumbnail(t *testing.T) {
	t.Parallel()

	coverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "https://www.bilibili.com/" {
			t.Fatalf("Referer = %q", got)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("cover"))
	}))
	defer coverServer.Close()

	manager, cleanup := newTestManagerWithBaseDir(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return []domain.InspectResult{{
				Platform:          domain.PlatformBilibili,
				NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=1",
				VideoID:           "BV1xx411c7mD#1001",
				Title:             "Test Video",
				ThumbnailURL:      coverServer.URL,
				QualityLabel:      "1080p",
				Container:         "mp4",
				SuggestedFilename: "Test Video [BV1xx411c7mD#1001].mp4",
			}}, nil
		},
		downloadFn: func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer cleanup()

	item, err := manager.Create(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD?p=1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForDownload(t, 2*time.Second, func() bool {
		current, err := manager.store.GetDownload(item.ID)
		if err != nil {
			return false
		}
		if current.ThumbnailURL != "/api/downloads/"+strconv.FormatInt(item.ID, 10)+"/thumbnail" {
			return false
		}
		content, err := os.ReadFile(manager.ThumbnailPath(item.ID))
		return err == nil && string(content) == "cover"
	})
}

func TestCreateKeepsBilibiliTaskWhenThumbnailCacheFails(t *testing.T) {
	t.Parallel()

	coverServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer coverServer.Close()

	manager, cleanup := newTestManagerWithBaseDir(t, &testRunner{
		inspectFn: func(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
			return []domain.InspectResult{{
				Platform:          domain.PlatformBilibili,
				NormalizedURL:     "https://www.bilibili.com/video/BV1xx411c7mD?p=1",
				VideoID:           "BV1xx411c7mD#1001",
				Title:             "Test Video",
				ThumbnailURL:      coverServer.URL,
				QualityLabel:      "1080p",
				Container:         "mp4",
				SuggestedFilename: "Test Video [BV1xx411c7mD#1001].mp4",
			}}, nil
		},
		downloadFn: func(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer cleanup()

	item, err := manager.Create(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD?p=1")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForDownload(t, 2*time.Second, func() bool {
		current, err := manager.store.GetDownload(item.ID)
		if err != nil {
			return false
		}
		return current.ThumbnailURL == coverServer.URL &&
			(current.Status == domain.StatusQueued || current.Status == domain.StatusDownloading)
	})
}

func newTestManager(t *testing.T, runner Runner) (*Manager, func()) {
	t.Helper()

	manager, cleanup, _ := newTestManagerBase(t, "", runner)
	return manager, cleanup
}

func newTestManagerWithBaseDir(t *testing.T, runner Runner) (*Manager, func()) {
	t.Helper()

	baseDir := t.TempDir()
	manager, cleanup, _ := newTestManagerBase(t, baseDir, runner)
	return manager, cleanup
}

func newTestManagerBase(t *testing.T, baseDir string, runner Runner) (*Manager, func(), string) {
	t.Helper()

	if baseDir == "" {
		baseDir = t.TempDir()
	}
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
	manager, err := NewManagerWithBaseDir(baseDir, store, settings, runner)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
		_ = store.Close()
	}, baseDir
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
