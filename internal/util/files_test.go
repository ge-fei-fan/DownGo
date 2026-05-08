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
	tempFile := filepath.Join(dir, "demo [abc].temp.mp4")
	metadataFile := filepath.Join(dir, "demo [abc].ytdl")
	otherFile := filepath.Join(dir, "other.mp4")

	for _, file := range []string{mainFile, partFile, tempFile, metadataFile, otherFile} {
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
	if FileExists(tempFile) {
		t.Fatal("expected temp file to be deleted")
	}
	if FileExists(metadataFile) {
		t.Fatal("expected metadata file to be deleted")
	}
	if !FileExists(otherFile) {
		t.Fatal("expected unrelated file to remain")
	}
}
