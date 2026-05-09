package deps

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

func TestInstallMissingEmitsProgressEvents(t *testing.T) {
	baseDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/yt-dlp.exe" {
			t.Fatalf("unexpected download request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()

	binDir := filepath.Join(baseDir, "data", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpegPath := filepath.Join(binDir, "ffmpeg.exe")
	if err := os.WriteFile(ffmpegPath, []byte("existing ffmpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	service := newService(baseDir, server.Client(), server.URL+"/yt-dlp.exe", server.URL+"/ffmpeg.exe")
	var events []ProgressEvent
	status := service.InstallMissingWithProgress(context.Background(), func(event ProgressEvent) {
		events = append(events, event)
	})

	if !status.YtDlp.Downloaded || !status.YtDlp.Exists {
		t.Fatalf("expected yt-dlp to be downloaded, got %+v", status.YtDlp)
	}
	if !hasEvent(events, "yt-dlp.exe", "started") {
		t.Fatalf("expected started event, got %s", formatEvents(events))
	}
	if !hasEvent(events, "yt-dlp.exe", "progress") {
		t.Fatalf("expected progress event, got %s", formatEvents(events))
	}
	if !hasEvent(events, "yt-dlp.exe", "completed") {
		t.Fatalf("expected completed event, got %s", formatEvents(events))
	}
	if !slices.ContainsFunc(events, func(event ProgressEvent) bool {
		return event.Type == "done" && event.Status != nil && event.Status.YtDlp.Exists
	}) {
		t.Fatalf("expected done event with status, got %s", formatEvents(events))
	}
}

func TestInstallMissingEmitsSkippedEvents(t *testing.T) {
	baseDir := t.TempDir()
	binDir := filepath.Join(baseDir, "data", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp.exe"), []byte("existing yt-dlp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ffmpeg.exe"), []byte("existing ffmpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	service := newService(baseDir, http.DefaultClient, "http://example.invalid/yt-dlp.exe", "http://example.invalid/ffmpeg.exe")
	var events []ProgressEvent
	service.InstallMissingWithProgress(context.Background(), func(event ProgressEvent) {
		events = append(events, event)
	})

	if !hasEvent(events, "yt-dlp.exe", "skipped") {
		t.Fatalf("expected yt-dlp skipped event, got %s", formatEvents(events))
	}
	if !hasEvent(events, "ffmpeg.exe", "skipped") {
		t.Fatalf("expected ffmpeg skipped event, got %s", formatEvents(events))
	}
}

func hasEvent(events []ProgressEvent, name, eventType string) bool {
	return slices.ContainsFunc(events, func(event ProgressEvent) bool {
		return event.Name == name && event.Type == eventType
	})
}

func formatEvents(events []ProgressEvent) string {
	formatted := make([]string, 0, len(events))
	for _, event := range events {
		formatted = append(formatted, fmt.Sprintf("%s:%s", event.Name, event.Type))
	}
	return fmt.Sprint(formatted)
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
