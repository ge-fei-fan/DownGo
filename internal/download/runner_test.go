package download

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/domain"
)

func TestHandleDownloadOutputLineParsesProgressPrefix(t *testing.T) {
	t.Parallel()

	var status string
	var percent float64
	var speed float64
	var eta int64
	var stderr bytes.Buffer

	handleDownloadOutputLine(
		streamLine{stream: "stderr", text: progressPrefix + `{"percent":"35.0%","speed":"2.00MiB/s","eta":"12"}`},
		&stderr,
		func(nextStatus string, nextPercent float64, nextSpeed float64, nextETA int64, qualityLabel string, container string) {
			status = nextStatus
			percent = nextPercent
			speed = nextSpeed
			eta = nextETA
		},
	)

	if status != domain.StatusDownloading {
		t.Fatalf("status = %q, want %q", status, domain.StatusDownloading)
	}
	if percent != 35 {
		t.Fatalf("percent = %v, want 35", percent)
	}
	if speed <= 0 {
		t.Fatalf("speed = %v, want > 0", speed)
	}
	if eta != 12 {
		t.Fatalf("eta = %d, want 12", eta)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr buffer should stay empty for progress lines")
	}
}

func TestHandleDownloadOutputLineParsesPostprocessPrefix(t *testing.T) {
	t.Parallel()

	var status string
	var percent float64
	var stderr bytes.Buffer

	handleDownloadOutputLine(
		streamLine{stream: "stdout", text: postprocessPrefix + `{"status":"started","eta":"NA"}`},
		&stderr,
		func(nextStatus string, nextPercent float64, nextSpeed float64, nextETA int64, qualityLabel string, container string) {
			status = nextStatus
			percent = nextPercent
		},
	)

	if status != domain.StatusPostprocessing {
		t.Fatalf("status = %q, want %q", status, domain.StatusPostprocessing)
	}
	if percent != 100 {
		t.Fatalf("percent = %v, want 100", percent)
	}
}

func TestHandleDownloadOutputLineKeepsNonProgressStderr(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	handleDownloadOutputLine(
		streamLine{stream: "stderr", text: "ERROR: download failed"},
		&stderr,
		func(string, float64, float64, int64, string, string) {},
	)

	if got := stderr.String(); got != "ERROR: download failed\n" {
		t.Fatalf("stderr = %q, want %q", got, "ERROR: download failed\n")
	}
}

func TestBuildDownloadArgsIncludesProgressFlags(t *testing.T) {
	t.Parallel()

	settings := config.Settings{
		FfmpegPath: filepath.Join(`C:\tools`, "ffmpeg.exe"),
	}
	item := domain.DownloadItem{
		OutputPath:    filepath.Join(`C:\downloads`, "video.mp4"),
		NormalizedURL: "https://www.youtube.com/watch?v=abc123",
	}

	args := buildDownloadArgs(settings, item)

	if !slices.Contains(args, "--progress") {
		t.Fatal("expected --progress in download args")
	}
	if !slices.Contains(args, "--newline") {
		t.Fatal("expected --newline in download args")
	}
	if !slices.Contains(args, "--progress-delta") {
		t.Fatal("expected --progress-delta in download args")
	}
	if !slices.Contains(args, progressDeltaSeconds) {
		t.Fatalf("expected progress delta %q in download args", progressDeltaSeconds)
	}
	if !slices.Contains(args, "download:"+progressTemplate) {
		t.Fatal("expected download progress template with prefix in args")
	}
	if !slices.Contains(args, "postprocess:"+postprocessTemplate) {
		t.Fatal("expected postprocess progress template with prefix in args")
	}
}

func TestBuildDownloadArgsOmitsCookiesByDefault(t *testing.T) {
	t.Parallel()

	settings := config.Settings{
		FfmpegPath: filepath.Join(`C:\tools`, "ffmpeg.exe"),
	}
	item := domain.DownloadItem{
		OutputPath:    filepath.Join(`C:\downloads`, "video.mp4"),
		NormalizedURL: "https://www.youtube.com/watch?v=abc123",
	}

	args := buildDownloadArgs(settings, item)

	if slices.Contains(args, "--cookies") {
		t.Fatal("expected download args to omit --cookies by default")
	}
}

func TestBuildDownloadArgsIncludesEnabledCookies(t *testing.T) {
	t.Parallel()

	cookiePath := filepath.Join(`C:\cookies`, "youtube.txt")
	settings := config.Settings{
		FfmpegPath:         filepath.Join(`C:\tools`, "ffmpeg.exe"),
		YtDlpCookiePath:    cookiePath,
		YtDlpCookieEnabled: true,
	}
	item := domain.DownloadItem{
		OutputPath:    filepath.Join(`C:\downloads`, "video.mp4"),
		NormalizedURL: "https://www.youtube.com/watch?v=abc123",
	}

	args := buildDownloadArgs(settings, item)

	cookieIndex := slices.Index(args, "--cookies")
	if cookieIndex < 0 {
		t.Fatal("expected download args to include --cookies")
	}
	if cookieIndex+1 >= len(args) || args[cookieIndex+1] != cookiePath {
		t.Fatalf("cookie path after --cookies = %q, want %q", args[cookieIndex+1], cookiePath)
	}
}

func TestBuildInspectArgsIncludesEnabledCookies(t *testing.T) {
	t.Parallel()

	cookiePath := filepath.Join(`C:\cookies`, "youtube.txt")
	settings := config.Settings{
		YtDlpCookiePath:    cookiePath,
		YtDlpCookieEnabled: true,
	}

	args := buildInspectArgs(settings, "https://www.youtube.com/watch?v=abc123")

	cookieIndex := slices.Index(args, "--cookies")
	if cookieIndex < 0 {
		t.Fatal("expected inspect args to include --cookies")
	}
	if cookieIndex+1 >= len(args) || args[cookieIndex+1] != cookiePath {
		t.Fatalf("cookie path after --cookies = %q, want %q", args[cookieIndex+1], cookiePath)
	}
}
