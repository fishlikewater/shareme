package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcceleratedListenerRejectsInvalidTransferToken(t *testing.T) {
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len("hello")), 5)
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	address := startAcceleratedTestListener(t, receiver, "session-invalid-token", "token-valid")

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial accelerated listener: %v", err)
	}
	defer conn.Close()

	if err := WriteAcceleratedHello(conn, AcceleratedHelloFrame{
		SessionID:     "session-invalid-token",
		TransferToken: "token-invalid",
		LaneIndex:     0,
	}); err != nil {
		t.Fatalf("write accelerated hello: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	buffer := make([]byte, 1)
	_, err = conn.Read(buffer)
	if err == nil {
		t.Fatal("expected accelerated listener to close invalid connection")
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("expected accelerated listener to close invalid connection, got %v", err)
	}
	if receiver.BytesReceived() != 0 {
		t.Fatalf("expected invalid token to write no bytes, got %d", receiver.BytesReceived())
	}
}

func TestAcceleratedReceiverWritesFramesByOffsetAndCommitsAtomically(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len(payload)), 6)
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	address := startAcceleratedTestListener(t, receiver, "session-commit", "token-commit")

	sendDone := make(chan error, 2)
	go func() {
		sendDone <- sendAcceleratedFrames(
			address,
			AcceleratedHelloFrame{
				SessionID:     "session-commit",
				TransferToken: "token-commit",
				LaneIndex:     0,
			},
			AcceleratedDataFrame{
				Offset:  6,
				Payload: []byte("world"),
			},
		)
	}()
	go func() {
		sendDone <- sendAcceleratedFrames(
			address,
			AcceleratedHelloFrame{
				SessionID:     "session-commit",
				TransferToken: "token-commit",
				LaneIndex:     1,
			},
			AcceleratedDataFrame{
				Offset:  0,
				Payload: []byte("hello "),
			},
		)
	}()

	for index := 0; index < 2; index++ {
		if err := <-sendDone; err != nil {
			t.Fatalf("send accelerated frame set %d: %v", index, err)
		}
	}

	waitForAcceleratedReceiverBytes(t, receiver, int64(len(payload)))

	finalPath, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload))
	if err != nil {
		t.Fatalf("complete accelerated receive: %v", err)
	}

	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read committed file: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("unexpected committed payload: %q", string(content))
	}
	if filepath.Base(finalPath) != "hello.txt" {
		t.Fatalf("unexpected committed file name: %s", filepath.Base(finalPath))
	}
}

func TestAcceleratedReceiverRejectsChecksumMismatchWithoutCommit(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len(payload)), 6)
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.ReceiveFrame(0, []byte("hello ")); err != nil {
		t.Fatalf("receive first frame: %v", err)
	}
	if _, err := receiver.ReceiveFrame(6, []byte("world")); err != nil {
		t.Fatalf("receive second frame: %v", err)
	}

	if _, err := receiver.Complete(acceleratedReceiverSHA256Hex([]byte("other payload"))); err == nil {
		t.Fatal("expected checksum mismatch to fail")
	}

	if _, err := os.Stat(filepath.Join(receiver.dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected checksum mismatch to skip commit, got stat error %v", err)
	}
}

func TestAcceleratedReceiverRejectsMissingChecksumWithoutCommit(t *testing.T) {
	payload := []byte("hello world")
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len(payload)), 6)
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	if _, err := receiver.ReceiveFrame(0, []byte("hello ")); err != nil {
		t.Fatalf("receive first frame: %v", err)
	}
	if _, err := receiver.ReceiveFrame(6, []byte("world")); err != nil {
		t.Fatalf("receive second frame: %v", err)
	}

	if _, err := receiver.Complete(""); err == nil {
		t.Fatal("expected missing checksum to fail")
	}

	if _, err := os.Stat(filepath.Join(receiver.dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected missing checksum to skip commit, got stat error %v", err)
	}
}

func startAcceleratedTestListener(
	t *testing.T,
	receiver *AcceleratedReceiver,
	sessionID string,
	token string,
) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen accelerated tcp: %v", err)
	}

	acceleratedListener := NewAcceleratedListener(listener)
	acceleratedListener.Register(AcceleratedSessionRegistration{
		SessionID:     sessionID,
		TransferToken: token,
		ExpiresAt:     time.Now().Add(time.Hour),
		Receiver:      receiver,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- acceleratedListener.Serve(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		_ = acceleratedListener.Close()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("accelerated listener exited with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("accelerated listener did not stop")
		}
	})

	return listener.Addr().String()
}

func sendAcceleratedFrames(address string, hello AcceleratedHelloFrame, frames ...AcceleratedDataFrame) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := WriteAcceleratedHello(conn, hello); err != nil {
		return err
	}
	for _, frame := range frames {
		if err := WriteAcceleratedDataFrame(conn, frame); err != nil {
			return err
		}
	}
	return nil
}

func waitForAcceleratedReceiverBytes(t *testing.T, receiver *AcceleratedReceiver, expected int64) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if receiver.BytesReceived() == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected receiver bytes %d, got %d", expected, receiver.BytesReceived())
}

func acceleratedReceiverSHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
