package download

import (
	"context"
	"strings"
	"testing"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/domain"
)

type stubRunner struct {
	inspectCalls  int
	downloadCalls int
}

func (s *stubRunner) Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
	s.inspectCalls++
	return nil, nil
}

func (s *stubRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	s.downloadCalls++
	return nil
}

func TestNormalizeYouTubeURL(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://www.youtube.com/watch?v=abc",
		"https://m.youtube.com/watch?v=abc",
		"https://youtube.com/shorts/abc",
		"https://youtu.be/abc",
		"  HTTPS://WWW.YOUTUBE.COM:443/watch?v=abc  ",
	}
	for _, input := range valid {
		if _, err := normalizeYouTubeURL(input); err != nil {
			t.Fatalf("normalizeYouTubeURL(%q) unexpected error: %v", input, err)
		}
	}

	invalid := []string{
		"",
		"not a url",
		"ftp://www.youtube.com/watch?v=abc",
		"https://example.com/watch?v=abc",
		"https://music.youtube.com/watch?v=abc",
		"/watch?v=abc",
	}
	for _, input := range invalid {
		if _, err := normalizeYouTubeURL(input); err == nil {
			t.Fatalf("normalizeYouTubeURL(%q) expected error", input)
		}
	}
}

func TestNormalizeSourceURLSupportsBilibili(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://www.bilibili.com/video/BV1xx411c7mD?p=2",
		"https://m.bilibili.com/video/BV1xx411c7mD",
		"分享 https://www.bilibili.com/video/BV1xx411c7mD?p=3 看看",
		"https://b23.tv/abc123",
	}
	for _, input := range valid {
		source, err := normalizeSourceURL(input)
		if err != nil {
			t.Fatalf("normalizeSourceURL(%q) unexpected error: %v", input, err)
		}
		if source.Platform != domain.PlatformBilibili {
			t.Fatalf("normalizeSourceURL(%q) platform = %q, want %q", input, source.Platform, domain.PlatformBilibili)
		}
	}
}

func TestInspectRejectsUnsupportedURLBeforeDependencyCheck(t *testing.T) {
	t.Parallel()

	runner := &stubRunner{}
	manager := &Manager{runner: runner}

	_, err := manager.Inspect(context.Background(), "https://example.com/video")
	if err == nil {
		t.Fatal("expected inspect to reject non-youtube url")
	}
	if !strings.Contains(err.Error(), "仅支持 YouTube 或 Bilibili 链接") {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.inspectCalls != 0 {
		t.Fatalf("expected runner inspect not to be called, got %d", runner.inspectCalls)
	}
}

func TestCreateRejectsUnsupportedURLBeforeProcessing(t *testing.T) {
	t.Parallel()

	runner := &stubRunner{}
	manager := &Manager{runner: runner}

	_, err := manager.Create(context.Background(), "https://example.com/video")
	if err == nil {
		t.Fatal("expected create to reject non-youtube url")
	}
	if !strings.Contains(err.Error(), "仅支持 YouTube 或 Bilibili 链接") {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.inspectCalls != 0 {
		t.Fatalf("expected runner inspect not to be called, got %d", runner.inspectCalls)
	}
}
