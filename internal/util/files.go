package util

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func SanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "video"
	}
	name = invalidFilenameChars.ReplaceAllString(name, "_")
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, ". ")
	if name == "" {
		return "video"
	}
	return name
}

func RandomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func DeleteAssociatedFiles(outputPath string) error {
	if outputPath == "" {
		return nil
	}

	dir := filepath.Dir(outputPath)
	base := filepath.Base(outputPath)
	baseNoExt := strings.TrimSuffix(base, filepath.Ext(base))

	if err := removeIfExists(outputPath); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, baseNoExt) {
			continue
		}
		if err := removeIfExists(filepath.Join(dir, name)); err != nil {
			return err
		}
	}

	return nil
}

func removeIfExists(path string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		err := os.Remove(path)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		lastErr = err
		time.Sleep(150 * time.Millisecond)
	}
	return lastErr
}
