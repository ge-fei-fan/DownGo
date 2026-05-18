package config

import (
	"path/filepath"
	"strings"
	"testing"

	"example.com/downgo/internal/db"
)

func TestSettingsLoadDefaultsYtDlpCookieDisabled(t *testing.T) {
	store, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service, err := NewService(store, Defaults(t.TempDir()))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	settings := service.Current()
	if settings.YtDlpCookieEnabled {
		t.Fatal("expected yt-dlp cookie to be disabled by default")
	}
	if settings.YtDlpCookiePath != "" {
		t.Fatalf("YtDlpCookiePath = %q, want empty", settings.YtDlpCookiePath)
	}
}

func TestSettingsUpdateValidatesEnabledYtDlpCookie(t *testing.T) {
	store, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	baseDir := t.TempDir()
	service, err := NewService(store, Defaults(baseDir))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Update(UpdateInput{YtDlpCookieEnabled: true}, func(value string) string { return value })
	if err == nil || !strings.Contains(err.Error(), "请选择") {
		t.Fatalf("Update() error = %v, want missing cookie path error", err)
	}

	_, err = service.Update(UpdateInput{
		YtDlpCookieEnabled: true,
		YtDlpCookiePath:    filepath.Join(baseDir, "cookies.json"),
	}, func(value string) string { return value })
	if err == nil || !strings.Contains(err.Error(), ".txt") {
		t.Fatalf("Update() error = %v, want txt extension error", err)
	}

	_, err = service.Update(UpdateInput{
		YtDlpCookieEnabled: true,
		YtDlpCookiePath:    filepath.Join(baseDir, "missing.txt"),
	}, func(value string) string { return value })
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("Update() error = %v, want missing file error", err)
	}
}
