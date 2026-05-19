//go:build !windows

package autostart

import "errors"

func SetEnabled(enabled bool) error {
	if !enabled {
		return nil
	}
	return errors.New("当前平台不支持开机自启")
}
