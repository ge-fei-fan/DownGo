package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestPublicDownloadEndpointsDoNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, store, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	if _, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:       "https://www.youtube.com/watch?v=active",
		NormalizedURL:   "https://www.youtube.com/watch?v=active",
		Platform:        domain.PlatformYouTube,
		VideoID:         "active",
		Title:           "Active",
		Status:          domain.StatusDownloading,
		ProgressPercent: 42,
	}); err != nil {
		t.Fatalf("CreateDownload(active) error = %v", err)
	}
	if _, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.youtube.com/watch?v=done",
		NormalizedURL: "https://www.youtube.com/watch?v=done",
		Platform:      domain.PlatformYouTube,
		VideoID:       "done",
		Title:         "Done",
		Status:        domain.StatusCompleted,
	}); err != nil {
		t.Fatalf("CreateDownload(completed) error = %v", err)
	}

	progress := getPublicDownloads(t, server.URL+"/api/public/downloads/progress?page=1&pageSize=10")
	if progress.Total != 1 || len(progress.Items) != 1 || progress.Items[0].VideoID != "active" || progress.Items[0].ProgressPercent != 42 {
		t.Fatalf("progress response = %+v", progress)
	}

	completed := getPublicDownloads(t, server.URL+"/api/public/downloads/completed?page=1&pageSize=10")
	if completed.Total != 1 || len(completed.Items) != 1 || completed.Items[0].VideoID != "done" {
		t.Fatalf("completed response = %+v", completed)
	}
}

func TestPublicCompletedListReturnsPublicThumbnailURL(t *testing.T) {
	t.Parallel()

	server, store, manager, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1public",
		NormalizedURL: "https://www.bilibili.com/video/BV1public",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1public",
		Title:         "Public Cover",
		ThumbnailURL:  protectedThumbnailURL(1),
		Status:        domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("CreateDownload(completed) error = %v", err)
	}
	item.ThumbnailURL = protectedThumbnailURL(item.ID)
	if _, err := store.UpdateMetadata(item.ID, item, domain.StatusCompleted, ""); err != nil {
		t.Fatalf("UpdateMetadata() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(manager.ThumbnailPath(item.ID)), 0o755); err != nil {
		t.Fatalf("MkdirAll(thumbnail) error = %v", err)
	}
	pngCover := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(manager.ThumbnailPath(item.ID), pngCover, 0o644); err != nil {
		t.Fatalf("WriteFile(thumbnail) error = %v", err)
	}

	completed := getPublicDownloads(t, server.URL+"/api/public/downloads/completed?page=1&pageSize=10")
	if completed.Total != 1 || len(completed.Items) != 1 {
		t.Fatalf("completed response = %+v", completed)
	}
	if completed.Items[0].ThumbnailURL != publicThumbnailURL(item.ID) {
		t.Fatalf("thumbnailUrl = %q, want %q", completed.Items[0].ThumbnailURL, publicThumbnailURL(item.ID))
	}

	res, err := http.Get(server.URL + completed.Items[0].ThumbnailURL)
	if err != nil {
		t.Fatalf("GET public thumbnail error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if contentType := res.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", contentType)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(thumbnail) error = %v", err)
	}
	if !bytes.Equal(body, pngCover) {
		t.Fatalf("thumbnail body = %q", body)
	}
}

func TestPublicThumbnailOnlyServesCompletedCachedCover(t *testing.T) {
	t.Parallel()

	server, store, manager, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	active, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1active",
		NormalizedURL: "https://www.bilibili.com/video/BV1active",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1active",
		Title:         "Active Cover",
		ThumbnailURL:  protectedThumbnailURL(1),
		Status:        domain.StatusDownloading,
	})
	if err != nil {
		t.Fatalf("CreateDownload(active) error = %v", err)
	}
	active.ThumbnailURL = protectedThumbnailURL(active.ID)
	if _, err := store.UpdateMetadata(active.ID, active, domain.StatusDownloading, ""); err != nil {
		t.Fatalf("UpdateMetadata(active) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(manager.ThumbnailPath(active.ID)), 0o755); err != nil {
		t.Fatalf("MkdirAll(active thumbnail) error = %v", err)
	}
	if err := os.WriteFile(manager.ThumbnailPath(active.ID), []byte("active-cover"), 0o644); err != nil {
		t.Fatalf("WriteFile(active thumbnail) error = %v", err)
	}

	missing, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1missing",
		NormalizedURL: "https://www.bilibili.com/video/BV1missing",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1missing",
		Title:         "Missing Cover",
		ThumbnailURL:  protectedThumbnailURL(2),
		Status:        domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("CreateDownload(missing) error = %v", err)
	}
	missing.ThumbnailURL = protectedThumbnailURL(missing.ID)
	if _, err := store.UpdateMetadata(missing.ID, missing, domain.StatusCompleted, ""); err != nil {
		t.Fatalf("UpdateMetadata(missing) error = %v", err)
	}

	for _, url := range []string{
		server.URL + publicThumbnailURL(active.ID),
		server.URL + publicThumbnailURL(missing.ID),
		server.URL + "/api/public/downloads/999999/thumbnail",
	} {
		res, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET %s error = %v", url, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want %d", url, res.StatusCode, http.StatusNotFound)
		}
	}
}

func TestPublicCreateDownloadDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	body := bytes.NewBufferString(`{"url":"https://www.youtube.com/watch?v=public123"}`)
	res, err := http.Post(server.URL+"/api/public/downloads", "application/json", body)
	if err != nil {
		t.Fatalf("POST public downloads error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusCreated)
	}
	var item domain.DownloadItem
	if err := json.NewDecoder(res.Body).Decode(&item); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if item.ID == 0 || item.SourceURL != "https://www.youtube.com/watch?v=public123" || item.Status != domain.StatusResolving {
		t.Fatalf("created item = %+v", item)
	}
}

func TestPublicDeleteCompletedDownloadDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, store, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "done.mp4")
	sidecarPath := filepath.Join(filepath.Dir(outputPath), "done.info.json")
	if err := os.WriteFile(outputPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}
	if err := os.WriteFile(sidecarPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile(sidecar) error = %v", err)
	}

	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=done-delete",
		NormalizedURL:  "https://www.youtube.com/watch?v=done-delete",
		Platform:       domain.PlatformYouTube,
		VideoID:        "done-delete",
		Title:          "Done Delete",
		OutputFilename: "done.mp4",
		OutputPath:     outputPath,
		Status:         domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("CreateDownload(completed) error = %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/public/downloads/"+strconv.FormatInt(item.ID, 10), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE public download error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNoContent)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("expected output file to be deleted, stat error = %v", err)
	}
	if _, err := os.Stat(sidecarPath); !os.IsNotExist(err) {
		t.Fatalf("expected sidecar file to be deleted, stat error = %v", err)
	}
	stored, err := store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
}

func TestPublicDeleteRejectsUnfinishedDownload(t *testing.T) {
	t.Parallel()

	server, store, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "active.mp4")
	if err := os.WriteFile(outputPath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}
	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=active-delete",
		NormalizedURL:  "https://www.youtube.com/watch?v=active-delete",
		Platform:       domain.PlatformYouTube,
		VideoID:        "active-delete",
		Title:          "Active Delete",
		OutputFilename: "active.mp4",
		OutputPath:     outputPath,
		Status:         domain.StatusDownloading,
	})
	if err != nil {
		t.Fatalf("CreateDownload(active) error = %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/public/downloads/"+strconv.FormatInt(item.ID, 10)+"/file", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE public download error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output file to remain, stat error = %v", err)
	}
	stored, err := store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt != nil {
		t.Fatal("expected deleted_at to remain empty")
	}
}

func TestProtectedDownloadsStillRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/downloads?view=active")
	if err != nil {
		t.Fatalf("GET protected downloads error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}

func getPublicDownloads(t *testing.T, url string) domain.PagedDownloads {
	t.Helper()

	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result domain.PagedDownloads
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	return result
}

func newPublicAPITestServer(t *testing.T, runner download.Runner) (*httptest.Server, *db.Store, *download.Manager, func()) {
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
	manager, err := download.NewManagerWithBaseDir(baseDir, store, settings, runner)
	if err != nil {
		t.Fatalf("NewManagerWithBaseDir() error = %v", err)
	}
	api := NewAPI(baseDir, settings, manager, deps.NewService(baseDir, nil), nil, auth.NewTokenManager("test"))
	server := httptest.NewServer(NewRouter(api, webui.Assets))

	return server, store, manager, func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
		_ = store.Close()
	}
}

type publicTestRunner struct{}

func (r *publicTestRunner) Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
	return []domain.InspectResult{{
		Platform:          domain.PlatformYouTube,
		NormalizedURL:     url,
		VideoID:           "public123",
		Title:             "Public",
		QualityLabel:      "1080p",
		Container:         "mp4",
		SuggestedFilename: "Public [public123].mp4",
	}}, nil
}

func (r *publicTestRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	<-ctx.Done()
	return ctx.Err()
}
