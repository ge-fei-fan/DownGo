//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

const (
	appName = "DownGo"
	runKey  = `Software\Microsoft\Windows\CurrentVersion\Run`
)

func SetEnabled(enabled bool) error {
	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("打开开机自启注册表失败: %w", err)
		}
		defer key.Close()
		if err := key.SetStringValue(appName, strconv.Quote(exe)); err != nil {
			return fmt.Errorf("写入开机自启注册表失败: %w", err)
		}
		return nil
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("打开开机自启注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除开机自启注册表失败: %w", err)
	}
	return nil
}
