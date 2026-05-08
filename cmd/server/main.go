package main

import (
	"os"
	"path/filepath"

	"example.com/downgo/internal/logging"
	"example.com/downgo/internal/tray"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		tray.ShowFatalError("DownGo 启动失败", err.Error())
		return
	}
	baseDir := filepath.Dir(exe)

	logger, err := logging.New(baseDir)
	if err != nil {
		tray.ShowFatalError("DownGo 启动失败", err.Error())
		return
	}
	defer logger.Close()

	_ = tray.Run(baseDir, logger.Dir(), logger.Logger())
}
