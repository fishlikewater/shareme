package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitRenamesTempFile(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewFileWriter(dir, "hello.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	finalPath, err := writer.Commit()
	if err != nil {
		t.Fatalf("unexpected commit error: %v", err)
	}

	if _, err := os.Stat(filepath.Clean(finalPath)); err != nil {
		t.Fatalf("expected final file: %v", err)
	}
}

func TestCommitDoesNotOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(originalPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("unexpected seed write error: %v", err)
	}

	writer, err := NewFileWriter(dir, "hello.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := writer.Write([]byte("new")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	finalPath, err := writer.Commit()
	if err != nil {
		t.Fatalf("unexpected commit error: %v", err)
	}
	if filepath.Clean(finalPath) == filepath.Clean(originalPath) {
		t.Fatalf("expected unique final path, got %s", finalPath)
	}

	originalContent, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if string(originalContent) != "old" {
		t.Fatalf("expected original file untouched, got %q", string(originalContent))
	}

	newContent, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("unexpected new file read error: %v", err)
	}
	if string(newContent) != "new" {
		t.Fatalf("expected new file content, got %q", string(newContent))
	}
}
