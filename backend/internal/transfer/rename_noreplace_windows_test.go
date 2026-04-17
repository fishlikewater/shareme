//go:build windows

package transfer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRenameStagedDownloadFileWithoutReplaceTreatsOccupiedDirectoryAsConflict(t *testing.T) {
	dir := t.TempDir()
	occupiedPath := filepath.Join(dir, "hello.txt")
	if err := os.Mkdir(occupiedPath, 0o755); err != nil {
		t.Fatalf("create occupied directory: %v", err)
	}

	file, _, tempPath, err := createStagedDownloadFile(dir, "hello.txt", 0)
	if err != nil {
		t.Fatalf("create staged file: %v", err)
	}
	if _, err := file.Write([]byte("hello")); err != nil {
		t.Fatalf("write staged file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close staged file: %v", err)
	}

	finalPath, err := renameStagedDownloadFileWithoutReplace(tempPath, dir, "hello.txt")
	if err != nil {
		t.Fatalf("expected directory conflict to be avoided, got %v", err)
	}

	expectedPath := filepath.Join(dir, "hello (1).txt")
	if filepath.Clean(finalPath) != filepath.Clean(expectedPath) {
		t.Fatalf("expected renamed path %s, got %s", expectedPath, finalPath)
	}
	if info, err := os.Stat(occupiedPath); err != nil {
		t.Fatalf("stat occupied directory: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected occupied path to remain a directory")
	}
	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read committed file: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("expected committed content to survive conflict, got %q", string(content))
	}
}

func TestMapNoReplaceRenameErrorTreatsOccupiedDirectoryAccessDeniedAsConflict(t *testing.T) {
	dir := t.TempDir()
	occupiedPath := filepath.Join(dir, "hello.txt")
	if err := os.Mkdir(occupiedPath, 0o755); err != nil {
		t.Fatalf("create occupied directory: %v", err)
	}

	err := mapNoReplaceRenameError(occupiedPath, windows.ERROR_ACCESS_DENIED)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected occupied directory to map to conflict, got %v", err)
	}
}

func TestMapNoReplaceRenameErrorPreservesRealAccessDenied(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "hello.txt")
	err := mapNoReplaceRenameError(targetPath, windows.ERROR_ACCESS_DENIED)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("expected plain access denied to be preserved, got %v", err)
	}
}
