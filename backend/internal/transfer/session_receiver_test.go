package transfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransferSessionReceiverWritesPartAtOffset(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	if written, err := receiver.WritePart(1, 6, int64(len("world")), bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("unexpected tail write error: %v", err)
	} else if written != int64(len("world")) {
		t.Fatalf("unexpected tail bytes written: %d", written)
	}
	if written, err := receiver.WritePart(0, 0, int64(len("hello ")), bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("unexpected head write error: %v", err)
	} else if written != int64(len("hello ")) {
		t.Fatalf("unexpected head bytes written: %d", written)
	}

	finalPath, err := receiver.Complete(2, sha256Hex(payload))
	if err != nil {
		t.Fatalf("unexpected complete error: %v", err)
	}

	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("unexpected read final file error: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("unexpected final payload: %q", string(content))
	}
	if filepath.Base(finalPath) != "hello.txt" {
		t.Fatalf("unexpected final file name: %s", filepath.Base(finalPath))
	}
}

func TestTransferSessionReceiverCompletesOnlyWhenAllPartsArrive(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.WritePart(1, 6, int64(len("world")), bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("unexpected tail write error: %v", err)
	}

	if _, err := receiver.Complete(2, sha256Hex(payload)); err == nil {
		t.Fatal("expected complete to reject missing parts")
	}

	if _, err := receiver.WritePart(0, 0, int64(len("hello ")), bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("unexpected head write error: %v", err)
	}

	finalPath, err := receiver.Complete(2, sha256Hex(payload))
	if err != nil {
		t.Fatalf("unexpected complete after all parts error: %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("expected committed file, got stat error: %v", err)
	}
}

func TestTransferSessionReceiverFastPathSkipsHashButStillSyncsWithoutChecksum(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.WritePart(0, 0, int64(len(payload)), bytes.NewReader(payload)); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	originalHasher := sessionReceiverHashFile
	originalSyncer := sessionReceiverSyncFile
	t.Cleanup(func() {
		sessionReceiverHashFile = originalHasher
		sessionReceiverSyncFile = originalSyncer
	})

	syncCalls := 0
	sessionReceiverHashFile = func(string) (string, error) {
		return "", errors.New("hash should not be called on fast path")
	}
	sessionReceiverSyncFile = func(*os.File) error {
		syncCalls++
		return nil
	}

	finalPath, err := receiver.Complete(1, "")
	if err != nil {
		t.Fatalf("expected fast path complete without checksum to skip hash only, got %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("expected fast path to sync once before commit, got %d", syncCalls)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("expected committed file, got stat error: %v", err)
	}
}

func TestSessionReceiverCompleteAvoidsOverwritingExistingFile(t *testing.T) {
	payload := []byte("hello world")
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(originalPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	receiver, err := NewSessionReceiver(dir, "hello.txt", int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.WritePart(0, 0, int64(len("hello ")), bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("unexpected head write error: %v", err)
	}
	if _, err := receiver.WritePart(1, 6, int64(len("world")), bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("unexpected tail write error: %v", err)
	}

	finalPath, err := receiver.Complete(2, sha256Hex(payload))
	if err != nil {
		t.Fatalf("unexpected complete error: %v", err)
	}
	if filepath.Base(finalPath) != "hello (1).txt" {
		t.Fatalf("expected collision-safe file name, got %s", filepath.Base(finalPath))
	}

	originalContent, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}
	if string(originalContent) != "old" {
		t.Fatalf("expected original file untouched, got %q", string(originalContent))
	}

	newContent, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(newContent) != string(payload) {
		t.Fatalf("unexpected final payload: %q", string(newContent))
	}
}

func TestSessionReceiverCompleteReturnsErrorAfterSuccessfulCommit(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.WritePart(0, 0, int64(len(payload)), bytes.NewReader(payload)); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if _, err := receiver.Complete(1, sha256Hex(payload)); err != nil {
		t.Fatalf("unexpected first complete error: %v", err)
	}

	if _, err := receiver.Complete(1, sha256Hex(payload)); !errors.Is(err, errSessionReceiverClosed) {
		t.Fatalf("expected repeated complete to return errSessionReceiverClosed, got %v", err)
	}
}

func TestSessionReceiverCompleteBlocksLateWritesAndSecondCompleteDuringFinalize(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", int64(len(payload)))
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.WritePart(0, 0, int64(len("hello ")), bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("unexpected head write error: %v", err)
	}
	if _, err := receiver.WritePart(1, 6, int64(len("world")), bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("unexpected tail write error: %v", err)
	}

	originalSyncer := sessionReceiverSyncFile
	t.Cleanup(func() {
		sessionReceiverSyncFile = originalSyncer
	})

	started := make(chan struct{})
	release := make(chan struct{})
	sessionReceiverSyncFile = func(*os.File) error {
		close(started)
		<-release
		return nil
	}

	type completeResult struct {
		path string
		err  error
	}
	resultCh := make(chan completeResult, 1)
	go func() {
		path, err := receiver.Complete(2, "")
		resultCh <- completeResult{path: path, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected complete to enter finalizing state")
	}

	if _, err := receiver.WritePart(2, 0, int64(len("HELLO")), bytes.NewReader([]byte("HELLO"))); !errors.Is(err, errSessionReceiverClosed) {
		t.Fatalf("expected late write to be rejected while finalizing, got %v", err)
	}
	if _, err := receiver.Complete(2, ""); !errors.Is(err, errSessionReceiverClosed) {
		t.Fatalf("expected second complete to be rejected while finalizing, got %v", err)
	}
	if err := receiver.Cleanup(); !errors.Is(err, errSessionReceiverClosed) {
		t.Fatalf("expected cleanup to be rejected while finalizing, got %v", err)
	}

	close(release)
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("expected first complete to succeed, got %v", result.err)
	}

	content, err := os.ReadFile(result.path)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("expected final payload to remain unchanged, got %q", string(content))
	}
}

func TestSessionReceiverRejectsPartAlreadyInProgress(t *testing.T) {
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", 5)
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	reader, writer := io.Pipe()
	writeDone := make(chan error, 1)
	go func() {
		_, err := receiver.WritePart(0, 0, 5, reader)
		writeDone <- err
	}()

	waitForSessionPartInFlight(t, receiver)

	if _, err := receiver.WritePart(0, 0, 5, bytes.NewReader([]byte("hello"))); !errors.Is(err, ErrPartAlreadyInProgress) {
		t.Fatalf("expected ErrPartAlreadyInProgress, got %v", err)
	}

	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected pipe write error: %v", err)
	}
	_ = writer.Close()
	if err := <-writeDone; err != nil {
		t.Fatalf("unexpected first write error: %v", err)
	}
}

func TestSessionReceiverCompleteRejectsPartsStillInProgress(t *testing.T) {
	receiver, err := NewSessionReceiver(t.TempDir(), "hello.txt", 5)
	if err != nil {
		t.Fatalf("unexpected new session receiver error: %v", err)
	}
	defer receiver.Cleanup()

	reader, writer := io.Pipe()
	writeDone := make(chan error, 1)
	go func() {
		_, err := receiver.WritePart(0, 0, 5, reader)
		writeDone <- err
	}()

	waitForSessionPartInFlight(t, receiver)

	if _, err := receiver.Complete(1, ""); err == nil || !strings.Contains(err.Error(), "parts still in progress") {
		t.Fatalf("expected parts still in progress error, got %v", err)
	}

	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected pipe write error: %v", err)
	}
	_ = writer.Close()
	if err := <-writeDone; err != nil {
		t.Fatalf("unexpected first write error: %v", err)
	}
}

func waitForSessionPartInFlight(t *testing.T, receiver *SessionReceiver) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		receiver.mu.Lock()
		inFlight := len(receiver.inFlight)
		receiver.mu.Unlock()
		if inFlight > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected part to enter in-flight state")
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
