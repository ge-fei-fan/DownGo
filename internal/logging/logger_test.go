package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewCreatesLogFileAndWritesEntries(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	logger, err := New(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	logger.Logger().Info("hello", zap.String("component", "test"))
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	logFile := filepath.Join(baseDir, "data", "logs", "downgo.log")
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}

	text := string(content)
	if !strings.Contains(text, "hello") || !strings.Contains(text, "component") {
		t.Fatalf("unexpected log content: %s", text)
	}
}
