package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteAssociatedFiles(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "demo [abc].mp4")
	partFile := filepath.Join(dir, "demo [abc].f137.mp4.part")
	otherFile := filepath.Join(dir, "other.mp4")

	for _, file := range []string{mainFile, partFile, otherFile} {
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", file, err)
		}
	}

	if err := DeleteAssociatedFiles(mainFile); err != nil {
		t.Fatalf("DeleteAssociatedFiles() error = %v", err)
	}

	if FileExists(mainFile) {
		t.Fatal("expected main file to be deleted")
	}
	if FileExists(partFile) {
		t.Fatal("expected part file to be deleted")
	}
	if !FileExists(otherFile) {
		t.Fatal("expected unrelated file to remain")
	}
}
