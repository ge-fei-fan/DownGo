package util

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"go.uber.org/zap"
)

func OpenFolderAndSelectFile(path string) error {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	zap.L().Info("opening containing folder", zap.String("path", clean), zap.String("dir", dir))
	if err := OpenFolder(dir); err != nil {
		zap.L().Error("failed to open containing folder", zap.String("path", clean), zap.String("dir", dir), zap.Error(err))
		return err
	}
	zap.L().Info("containing folder open requested", zap.String("path", clean), zap.String("dir", dir))
	return nil
}

func OpenFolder(path string) error {
	clean := filepath.Clean(path)
	zap.L().Info("opening folder via shell execute", zap.String("dir", clean))
	result, err := shellExecute("open", clean, "", "")
	if err != nil {
		zap.L().Error("shell execute open folder failed", zap.String("dir", clean), zap.Error(err))
		return err
	}
	zap.L().Info("shell execute open folder returned", zap.String("dir", clean), zap.Uintptr("result", result))
	return nil
}

func shellExecute(operation, file, parameters, directory string) (uintptr, error) {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteW := shell32.NewProc("ShellExecuteW")
	operationPtr := syscall.StringToUTF16Ptr(operation)
	filePtr := syscall.StringToUTF16Ptr(file)
	parametersPtr := uintptr(0)
	directoryPtr := uintptr(0)
	if parameters != "" {
		parametersPtr = uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(parameters)))
	}
	if directory != "" {
		directoryPtr = uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(directory)))
	}

	result, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(operationPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		parametersPtr,
		directoryPtr,
		1,
	)
	if result <= 32 {
		if callErr != syscall.Errno(0) {
			return result, callErr
		}
		return result, fmt.Errorf("ShellExecuteW failed with code %d", result)
	}
	return result, nil
}

func SelectFolder(initialDir string) (string, error) {
	zap.L().Info("opening folder picker", zap.String("initialDir", initialDir))
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false",
		"$owner = New-Object System.Windows.Forms.Form",
		"$owner.TopMost = $true",
		"$owner.ShowInTaskbar = $false",
		"$owner.StartPosition = 'CenterScreen'",
		"$owner.Width = 1",
		"$owner.Height = 1",
		"$owner.Show()",
		"$owner.Activate()",
		"$dialog = New-Object System.Windows.Forms.FolderBrowserDialog",
		"$dialog.Description = '选择下载目录'",
		"$dialog.ShowNewFolderButton = $true",
		"$initial = [Environment]::GetEnvironmentVariable('DOWNGO_INITIAL_DIR')",
		"if ($initial -and [System.IO.Directory]::Exists($initial)) { $dialog.SelectedPath = $initial }",
		"try { if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $dialog.SelectedPath } } finally { $owner.Close(); $owner.Dispose() }",
	}, "; ")

	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), "DOWNGO_INITIAL_DIR="+initialDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		zap.L().Error("folder picker failed", zap.String("initialDir", initialDir), zap.ByteString("output", output), zap.Error(err))
		return "", fmt.Errorf("选择目录窗口启动失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	selected := strings.TrimSpace(string(output))
	if selected == "" {
		zap.L().Info("folder picker canceled", zap.String("initialDir", initialDir))
		return "", nil
	}
	zap.L().Info("folder selected", zap.String("path", selected))
	return selected, nil
}

func SelectTextFile(initialPath string) (string, error) {
	zap.L().Info("opening text file picker", zap.String("initialPath", initialPath))
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false",
		"$owner = New-Object System.Windows.Forms.Form",
		"$owner.TopMost = $true",
		"$owner.ShowInTaskbar = $false",
		"$owner.StartPosition = 'CenterScreen'",
		"$owner.Width = 1",
		"$owner.Height = 1",
		"$owner.Show()",
		"$owner.Activate()",
		"$dialog = New-Object System.Windows.Forms.OpenFileDialog",
		"$dialog.Title = '选择 yt-dlp Cookie txt 文件'",
		"$dialog.Filter = 'Text files (*.txt)|*.txt|All files (*.*)|*.*'",
		"$dialog.CheckFileExists = $true",
		"$dialog.Multiselect = $false",
		"$initial = [Environment]::GetEnvironmentVariable('DOWNGO_INITIAL_FILE')",
		"if ($initial -and [System.IO.File]::Exists($initial)) { $dialog.InitialDirectory = [System.IO.Path]::GetDirectoryName($initial); $dialog.FileName = [System.IO.Path]::GetFileName($initial) }",
		"try { if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $dialog.FileName } } finally { $owner.Close(); $owner.Dispose(); $dialog.Dispose() }",
	}, "; ")

	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), "DOWNGO_INITIAL_FILE="+initialPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		zap.L().Error("text file picker failed", zap.String("initialPath", initialPath), zap.ByteString("output", output), zap.Error(err))
		return "", fmt.Errorf("选择文件窗口启动失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	selected := strings.TrimSpace(string(output))
	if selected == "" {
		zap.L().Info("text file picker canceled", zap.String("initialPath", initialPath))
		return "", nil
	}
	zap.L().Info("text file selected", zap.String("path", selected))
	return selected, nil
}
