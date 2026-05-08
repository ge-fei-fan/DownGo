package util

import (
	"fmt"
	"os/exec"
	"syscall"
)

func OpenFolderAndSelectFile(path string) error {
	cmd := exec.Command("explorer.exe", fmt.Sprintf("/select,%s", path))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
