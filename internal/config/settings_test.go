package config

import (
	"errors"
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

func TestSettingsLoadDefaultsAutoStartDisabled(t *testing.T) {
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
	if settings.AutoStartEnabled {
		t.Fatal("expected auto start to be disabled by default")
	}
}

func TestSettingsUpdatePersistsAutoStartEnabled(t *testing.T) {
	originalSetAutoStartEnabled := setAutoStartEnabled
	t.Cleanup(func() { setAutoStartEnabled = originalSetAutoStartEnabled })

	var calls []bool
	setAutoStartEnabled = func(enabled bool) error {
		calls = append(calls, enabled)
		return nil
	}

	dir := t.TempDir()
	store, err := db.Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service, err := NewService(store, Defaults(dir))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	updated, err := service.Update(UpdateInput{AutoStartEnabled: true}, func(value string) string { return value })
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !updated.AutoStartEnabled || !service.Current().AutoStartEnabled {
		t.Fatal("expected auto start to be enabled")
	}
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("setAutoStartEnabled calls = %v, want [true]", calls)
	}

	reloaded, err := NewService(store, Defaults(dir))
	if err != nil {
		t.Fatalf("NewService(reloaded) error = %v", err)
	}
	if !reloaded.Current().AutoStartEnabled {
		t.Fatal("expected reloaded auto start to be enabled")
	}
}

func TestSettingsUpdateAutoStartFailureDoesNotPersist(t *testing.T) {
	originalSetAutoStartEnabled := setAutoStartEnabled
	t.Cleanup(func() { setAutoStartEnabled = originalSetAutoStartEnabled })

	setAutoStartEnabled = func(enabled bool) error {
		if enabled {
			return errors.New("registry failed")
		}
		return nil
	}

	dir := t.TempDir()
	store, err := db.Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service, err := NewService(store, Defaults(dir))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Update(UpdateInput{AutoStartEnabled: true}, func(value string) string { return value })
	if err == nil || !strings.Contains(err.Error(), "registry failed") {
		t.Fatalf("Update() error = %v, want registry failure", err)
	}
	if service.Current().AutoStartEnabled {
		t.Fatal("expected in-memory auto start to remain disabled")
	}

	reloaded, err := NewService(store, Defaults(dir))
	if err != nil {
		t.Fatalf("NewService(reloaded) error = %v", err)
	}
	if reloaded.Current().AutoStartEnabled {
		t.Fatal("expected persisted auto start to remain disabled")
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
