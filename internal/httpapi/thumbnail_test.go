package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/downgo/internal/auth"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/deps"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/download"
	"example.com/downgo/webui"
)

func TestHandleThumbnailServesCachedCover(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	defaults := config.Defaults(baseDir)
	if err := os.MkdirAll(filepath.Dir(defaults.FfmpegPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(defaults.FfmpegPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(ffmpeg) error = %v", err)
	}

	store, err := db.Open(baseDir)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	defer store.Close()

	settings, err := config.NewService(store, defaults)
	if err != nil {
		t.Fatalf("config.NewService() error = %v", err)
	}
	manager, err := download.NewManagerWithBaseDir(baseDir, store, settings, &thumbnailTestRunner{})
	if err != nil {
		t.Fatalf("NewManagerWithBaseDir() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	}()

	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.bilibili.com/video/BV1xx411c7mD",
		NormalizedURL:  "https://www.bilibili.com/video/BV1xx411c7mD?p=1",
		Platform:       domain.PlatformBilibili,
		VideoID:        "BV1xx411c7mD#1001",
		Title:          "Test Video",
		OutputFilename: "Test Video.mp4",
		OutputPath:     filepath.Join(baseDir, "data", "downloads", "Test Video.mp4"),
		Status:         domain.StatusQueued,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}
	item.ThumbnailURL = fmt.Sprintf("/api/downloads/%d/thumbnail", item.ID)
	if _, err := store.UpdateMetadata(item.ID, item, domain.StatusQueued, ""); err != nil {
		t.Fatalf("UpdateMetadata() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(manager.ThumbnailPath(item.ID)), 0o755); err != nil {
		t.Fatalf("MkdirAll(thumbnail) error = %v", err)
	}
	if err := os.WriteFile(manager.ThumbnailPath(item.ID), []byte("cover"), 0o644); err != nil {
		t.Fatalf("WriteFile(thumbnail) error = %v", err)
	}

	tokens := auth.NewTokenManager("test")
	token, err := tokens.Issue("test", time.Hour)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	api := NewAPI(baseDir, settings, manager, deps.NewService(baseDir, nil), nil, nil, tokens)
	server := httptest.NewServer(NewRouter(api, webui.Assets))
	defer server.Close()

	res, err := http.Get(fmt.Sprintf("%s/api/downloads/%d/thumbnail?token=%s", server.URL, item.ID, token))
	if err != nil {
		t.Fatalf("GET thumbnail error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
}

type thumbnailTestRunner struct{}

func (r *thumbnailTestRunner) Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
	return nil, nil
}

func (r *thumbnailTestRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	return nil
}
