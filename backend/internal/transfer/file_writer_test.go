package transfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

func TestFileWriterCommitReturnsErrorAfterSuccessfulCommit(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewFileWriter(dir, "hello.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if _, err := writer.Commit(); err != nil {
		t.Fatalf("unexpected first commit error: %v", err)
	}

	if _, err := writer.Commit(); !errors.Is(err, errFileWriterClosed) {
		t.Fatalf("expected repeated commit to return errFileWriterClosed, got %v", err)
	}
}

func TestCommitStagedDownloadFileAvoidsConcurrentNameCollision(t *testing.T) {
	assertConcurrentStagedCommitSucceeds(t)
}

func TestCommitStagedDownloadFileAvoidsConcurrentNameCollisionWhenNoReplaceRenameUnsupported(t *testing.T) {
	originalRenameNoReplace := stagedDownloadRenameNoReplace
	originalLink := stagedDownloadLink
	t.Cleanup(func() {
		stagedDownloadRenameNoReplace = originalRenameNoReplace
		stagedDownloadLink = originalLink
	})

	var linkCalls int32
	stagedDownloadRenameNoReplace = func(string, string) error {
		return errNoReplaceRenameUnsupported
	}
	stagedDownloadLink = func(oldname string, newname string) error {
		atomic.AddInt32(&linkCalls, 1)
		return os.Link(oldname, newname)
	}

	assertConcurrentStagedCommitSucceeds(t)
	if atomic.LoadInt32(&linkCalls) == 0 {
		t.Fatal("expected hard-link fallback to be used when no-replace rename is unsupported")
	}
}

func TestCommitStagedDownloadFileFailsWhenAtomicCommitUnsupported(t *testing.T) {
	originalRenameNoReplace := stagedDownloadRenameNoReplace
	originalLink := stagedDownloadLink
	t.Cleanup(func() {
		stagedDownloadRenameNoReplace = originalRenameNoReplace
		stagedDownloadLink = originalLink
	})

	dir := t.TempDir()
	file, _, tempPath, err := createStagedDownloadFile(dir, "hello.txt", 0)
	if err != nil {
		t.Fatalf("create staged file: %v", err)
	}
	if _, err := file.Write([]byte("hello")); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	var linkCalls int32
	stagedDownloadRenameNoReplace = func(string, string) error {
		return errNoReplaceRenameUnsupported
	}
	stagedDownloadLink = func(string, string) error {
		atomic.AddInt32(&linkCalls, 1)
		return errors.New("hard link unsupported")
	}

	finalPath, err := commitStagedDownloadFile(file, tempPath, dir, "hello.txt")
	if err == nil || !strings.Contains(err.Error(), "atomic") {
		t.Fatalf("expected atomic commit unsupported error, got path=%q err=%v", finalPath, err)
	}
	if atomic.LoadInt32(&linkCalls) == 0 {
		t.Fatal("expected hard-link fallback to be attempted before failing")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("expected final destination to remain absent, got %v", statErr)
	}
}

func TestCommitStagedDownloadFileDoesNotUseHardLinkWhenNoReplaceRenameAvailable(t *testing.T) {
	originalLink := stagedDownloadLink
	t.Cleanup(func() {
		stagedDownloadLink = originalLink
	})

	stagedDownloadLink = func(string, string) error {
		return errors.New("hard link should not be used when no-replace rename is available")
	}

	assertConcurrentStagedCommitSucceeds(t)
}

func TestCommitStagedDownloadFileKeepsSuccessfulCommitWhenRemovingLinkedTempNameFails(t *testing.T) {
	originalRenameNoReplace := stagedDownloadRenameNoReplace
	originalRemove := stagedDownloadRemoveFile
	t.Cleanup(func() {
		stagedDownloadRenameNoReplace = originalRenameNoReplace
		stagedDownloadRemoveFile = originalRemove
	})

	dir := t.TempDir()
	file, _, tempPath, err := createStagedDownloadFile(dir, "hello.txt", 0)
	if err != nil {
		t.Fatalf("create staged file: %v", err)
	}
	if _, err := file.Write([]byte("hello")); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	stagedDownloadRenameNoReplace = func(string, string) error {
		return errNoReplaceRenameUnsupported
	}
	stagedDownloadRemoveFile = func(path string) error {
		if path == tempPath {
			return errors.New("remove temp name failed")
		}
		return os.Remove(path)
	}

	finalPath, err := commitStagedDownloadFile(file, tempPath, dir, "hello.txt")
	if err != nil {
		t.Fatalf("expected commit to stay successful after temp-name cleanup failure, got %v", err)
	}
	if finalPath == "" {
		t.Fatal("expected final path even when temp-name removal fails")
	}
	if _, statErr := os.Stat(finalPath); statErr != nil {
		t.Fatalf("expected final file to exist, got %v", statErr)
	}
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("expected leaked temp name to remain for diagnosis, got %v", statErr)
	}
}

func assertConcurrentStagedCommitSucceeds(t *testing.T) {
	const attempts = 64

	for attempt := 0; attempt < attempts; attempt++ {
		dir := filepath.Join(t.TempDir(), fmt.Sprintf("attempt-%d", attempt))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("prepare attempt dir: %v", err)
		}

		firstContent := []byte(fmt.Sprintf("first-%d", attempt))
		secondContent := []byte(fmt.Sprintf("second-%d", attempt))

		firstFile, _, firstTempPath, err := createStagedDownloadFile(dir, "hello.txt", 0)
		if err != nil {
			t.Fatalf("create first staged file: %v", err)
		}
		if _, err := firstFile.Write(firstContent); err != nil {
			t.Fatalf("write first staged file: %v", err)
		}

		secondFile, _, secondTempPath, err := createStagedDownloadFile(dir, "hello.txt", 0)
		if err != nil {
			t.Fatalf("create second staged file: %v", err)
		}
		if _, err := secondFile.Write(secondContent); err != nil {
			t.Fatalf("write second staged file: %v", err)
		}

		start := make(chan struct{})
		type commitResult struct {
			path string
			err  error
		}
		results := make(chan commitResult, 2)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			path, err := commitStagedDownloadFile(firstFile, firstTempPath, dir, "hello.txt")
			results <- commitResult{path: path, err: err}
		}()
		go func() {
			defer wg.Done()
			<-start
			path, err := commitStagedDownloadFile(secondFile, secondTempPath, dir, "hello.txt")
			results <- commitResult{path: path, err: err}
		}()

		close(start)
		wg.Wait()
		close(results)

		committedPaths := make([]string, 0, 2)
		for result := range results {
			if result.err != nil {
				t.Fatalf("expected concurrent commits to both succeed, got %v", result.err)
			}
			committedPaths = append(committedPaths, result.path)
		}

		if committedPaths[0] == committedPaths[1] {
			t.Fatalf("expected concurrent commits to use distinct paths, got %s", committedPaths[0])
		}

		contents := make([]string, 0, 2)
		for _, committedPath := range committedPaths {
			content, err := os.ReadFile(committedPath)
			if err != nil {
				t.Fatalf("read committed file %s: %v", committedPath, err)
			}
			contents = append(contents, string(content))
		}
		sort.Strings(contents)
		expected := []string{string(firstContent), string(secondContent)}
		sort.Strings(expected)
		if contents[0] != expected[0] || contents[1] != expected[1] {
			t.Fatalf("expected both payloads to survive concurrent commit, got %v", contents)
		}
	}
}
