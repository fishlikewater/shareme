package transfer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

func TestTransferSessionReceiverFastPathSkipsHashAndSyncWithoutChecksum(t *testing.T) {
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

	sessionReceiverHashFile = func(string) (string, error) {
		return "", errors.New("hash should not be called on fast path")
	}
	sessionReceiverSyncFile = func(*os.File) error {
		return errors.New("sync should not be called on fast path")
	}

	finalPath, err := receiver.Complete(1, "")
	if err != nil {
		t.Fatalf("expected fast path complete without checksum to skip hash/sync, got %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("expected committed file, got stat error: %v", err)
	}
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
