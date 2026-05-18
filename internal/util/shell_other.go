//go:build !windows

package util

import "errors"

func SelectTextFile(initialPath string) (string, error) {
	return "", errors.New("当前平台不支持选择文件")
}
