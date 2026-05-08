package util

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

func OpenFolderAndSelectFile(path string) error {
	cmd := exec.Command("explorer.exe", "/select,", filepath.Clean(path))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
