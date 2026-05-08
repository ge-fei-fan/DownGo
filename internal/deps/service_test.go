package deps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallMissingDownloadsOnlyAbsentFiles(t *testing.T) {
	baseDir := t.TempDir()
	binDir := filepath.Join(baseDir, "data", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ffmpegPath := filepath.Join(binDir, "ffmpeg.exe")
	if err := os.WriteFile(ffmpegPath, []byte("existing ffmpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp.exe":
			_, _ = w.Write([]byte("yt-dlp"))
		default:
			t.Fatalf("unexpected download request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	service := newService(baseDir, server.Client(), server.URL+"/yt-dlp.exe", server.URL+"/ffmpeg.exe")

	status := service.InstallMissing(context.Background())
	if !status.YtDlp.Downloaded || !status.YtDlp.Exists {
		t.Fatalf("expected yt-dlp to be downloaded, got %+v", status.YtDlp)
	}
	if status.Ffmpeg.Downloaded {
		t.Fatalf("expected ffmpeg to be skipped, got %+v", status.Ffmpeg)
	}
}

func TestInstallMissingReturnsErrorAndCleansTempFile(t *testing.T) {
	baseDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := newService(baseDir, server.Client(), server.URL+"/yt-dlp.exe", server.URL+"/ffmpeg.exe")

	status := service.InstallMissing(context.Background())
	if status.YtDlp.Error == "" {
		t.Fatalf("expected yt-dlp error, got %+v", status.YtDlp)
	}
	if status.YtDlp.Exists {
		t.Fatalf("expected yt-dlp file to be absent after failure, got %+v", status.YtDlp)
	}

	entries, err := os.ReadDir(filepath.Join(baseDir, "data", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Fatalf("expected temp file cleanup, found %s", entry.Name())
		}
	}
}
