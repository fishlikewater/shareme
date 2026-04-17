package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func TestAcceleratedListenerRejectsUnregisteredSession(t *testing.T) {
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len("hello")), 5)
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen accelerated tcp: %v", err)
	}

	acceleratedListener := NewAcceleratedListener(listener)
	acceleratedListener.Register(AcceleratedSessionRegistration{
		SessionID:     "session-unregister",
		TransferToken: "token-unregister",
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
		case serveErr := <-errCh:
			if serveErr != nil {
				t.Errorf("accelerated listener exited with error: %v", serveErr)
			}
		case <-time.After(time.Second):
			t.Error("accelerated listener did not stop")
		}
	})

	acceleratedListener.Unregister("session-unregister")

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial accelerated listener: %v", err)
	}
	defer conn.Close()

	if err := WriteAcceleratedHello(conn, AcceleratedHelloFrame{
		SessionID:     "session-unregister",
		TransferToken: "token-unregister",
		LaneIndex:     0,
	}); err != nil {
		t.Fatalf("write accelerated hello: %v", err)
	}
	writeErr := WriteAcceleratedDataFrame(conn, AcceleratedDataFrame{
		Offset:  0,
		Payload: []byte("hello"),
	})
	if writeErr == nil {
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		if _, err := ReadAcceleratedAckFrame(conn); err == nil {
			t.Fatal("expected unregistered accelerated session to reject lane")
		}
	}
	if receiver.BytesReceived() != 0 {
		t.Fatalf("expected unregistered session to write no bytes, got %d", receiver.BytesReceived())
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

func TestAcceleratedReceiverRecordsAckBeforeExposingAckFrame(t *testing.T) {
	payload := []byte("hello")
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len(payload)), int64(len(payload)))
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ackExposed := make(chan struct{})
	releaseAckReturn := make(chan struct{})
	ackConn := &blockingAckConn{
		Conn:             serverConn,
		ackExposed:       ackExposed,
		releaseAckReturn: releaseAckReturn,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- receiver.ServeLane(context.Background(), 0, ackConn)
	}()

	ackReadCh := make(chan AcceleratedAckFrame, 1)
	ackReadErrCh := make(chan error, 1)
	go func() {
		ack, err := ReadAcceleratedAckFrame(clientConn)
		if err != nil {
			ackReadErrCh <- err
			return
		}
		ackReadCh <- ack
	}()

	if err := WriteAcceleratedDataFrame(clientConn, AcceleratedDataFrame{
		Offset:  0,
		Payload: payload,
	}); err != nil {
		t.Fatalf("write accelerated data frame: %v", err)
	}

	select {
	case <-ackExposed:
	case <-time.After(time.Second):
		t.Fatal("expected ack to be exposed to sender")
	}

	select {
	case err := <-ackReadErrCh:
		t.Fatalf("read accelerated ack frame: %v", err)
	case ack := <-ackReadCh:
		if ack.Offset != 0 || ack.Length != int64(len(payload)) {
			t.Fatalf("unexpected ack frame: %#v", ack)
		}
	default:
		t.Fatal("expected sender to receive ack before complete")
	}

	finalPath, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload))
	if err != nil {
		t.Fatalf("expected complete to succeed after ack exposure, got %v", err)
	}

	close(releaseAckReturn)
	_ = clientConn.Close()
	if err := <-serveErrCh; err != nil {
		t.Fatalf("ServeLane() error = %v", err)
	}

	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read committed file: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("unexpected committed payload: %q", string(content))
	}
}

func TestAcceleratedReceiverCleanupWaitsForInflightFrameAndRejectsAck(t *testing.T) {
	payload := []byte("hello")
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "hello.txt", int64(len(payload)), int64(len(payload)))
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	originalWriteAt := acceleratedReceiverWriteAt
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	acceleratedReceiverWriteAt = func(file *os.File, chunk []byte, offset int64) (int, error) {
		close(writeStarted)
		<-releaseWrite
		return file.WriteAt(chunk, offset)
	}
	t.Cleanup(func() {
		acceleratedReceiverWriteAt = originalWriteAt
	})

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- receiver.ServeLane(context.Background(), 0, serverConn)
	}()

	if err := WriteAcceleratedDataFrame(clientConn, AcceleratedDataFrame{
		Offset:  0,
		Payload: payload,
	}); err != nil {
		t.Fatalf("write accelerated data frame: %v", err)
	}

	select {
	case <-writeStarted:
	case <-time.After(time.Second):
		t.Fatal("expected accelerated frame write to start")
	}

	cleanupErrCh := make(chan error, 1)
	go func() {
		cleanupErrCh <- receiver.Cleanup()
	}()

	select {
	case err := <-cleanupErrCh:
		t.Fatalf("expected cleanup to wait for in-flight frame, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseWrite)

	if err := <-cleanupErrCh; err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if err := clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, err := ReadAcceleratedAckFrame(clientConn); err == nil {
		t.Fatal("expected cleanup during in-flight frame to suppress ack")
	}
	if err := <-serveErrCh; !errors.Is(err, errAcceleratedReceiverClosed) {
		t.Fatalf("expected ServeLane() to stop with receiver closed, got %v", err)
	}
	if receiver.BytesReceived() != 0 {
		t.Fatalf("expected cleanup to discard in-flight bytes, got %d", receiver.BytesReceived())
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
	acknowledgeAcceleratedTestFrames(t, receiver, map[int64]int64{
		0: int64(len("hello ")),
		6: int64(len("world")),
	})

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
	acknowledgeAcceleratedTestFrames(t, receiver, map[int64]int64{
		0: int64(len("hello ")),
		6: int64(len("world")),
	})

	if _, err := receiver.Complete(""); err == nil {
		t.Fatal("expected missing checksum to fail")
	}

	if _, err := os.Stat(filepath.Join(receiver.dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected missing checksum to skip commit, got stat error %v", err)
	}
}

func TestAcceleratedReceiverCompleteAvoidsOverwritingExistingFile(t *testing.T) {
	payload := []byte("hello world")
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(originalPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	receiver, err := NewAcceleratedReceiver(dir, "hello.txt", int64(len(payload)), 6)
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
	acknowledgeAcceleratedTestFrames(t, receiver, map[int64]int64{
		0: int64(len("hello ")),
		6: int64(len("world")),
	})

	finalPath, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload))
	if err != nil {
		t.Fatalf("complete accelerated receive: %v", err)
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

func TestAcceleratedReceiverCompleteReturnsErrorAfterSuccessfulCommit(t *testing.T) {
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
	acknowledgeAcceleratedTestFrames(t, receiver, map[int64]int64{
		0: int64(len("hello ")),
		6: int64(len("world")),
	})
	if _, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload)); err != nil {
		t.Fatalf("unexpected first complete error: %v", err)
	}

	if _, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload)); !errors.Is(err, errAcceleratedReceiverClosed) {
		t.Fatalf("expected repeated complete to return errAcceleratedReceiverClosed, got %v", err)
	}
}

func TestAcceleratedReceiverCompleteRequiresAckedFrames(t *testing.T) {
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

	if _, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload)); err == nil || !strings.Contains(err.Error(), "ack") {
		t.Fatalf("expected complete to reject unacked frames, got %v", err)
	}

	if err := receiver.AcknowledgeFrame(0, int64(len("hello "))); err != nil {
		t.Fatalf("acknowledge first frame: %v", err)
	}
	if err := receiver.AcknowledgeFrame(6, int64(len("world"))); err != nil {
		t.Fatalf("acknowledge second frame: %v", err)
	}

	finalPath, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload))
	if err != nil {
		t.Fatalf("expected complete to succeed after acking frames, got %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("expected committed file, got %v", err)
	}
}

func TestAcceleratedReceiverCompleteBlocksLateFramesAndSecondCompleteDuringFinalize(t *testing.T) {
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
	acknowledgeAcceleratedTestFrames(t, receiver, map[int64]int64{
		0: int64(len("hello ")),
		6: int64(len("world")),
	})

	originalSyncer := acceleratedReceiverSyncFile
	t.Cleanup(func() {
		acceleratedReceiverSyncFile = originalSyncer
	})

	started := make(chan struct{})
	release := make(chan struct{})
	acceleratedReceiverSyncFile = func(*os.File) error {
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
		path, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload))
		resultCh <- completeResult{path: path, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected complete to enter finalizing state")
	}

	if _, err := receiver.ReceiveFrame(1, []byte("A")); !errors.Is(err, errAcceleratedReceiverClosed) {
		t.Fatalf("expected late frame to be rejected while finalizing, got %v", err)
	}
	if _, err := receiver.Complete(acceleratedReceiverSHA256Hex(payload)); !errors.Is(err, errAcceleratedReceiverClosed) {
		t.Fatalf("expected second complete to be rejected while finalizing, got %v", err)
	}
	if err := receiver.Cleanup(); !errors.Is(err, errAcceleratedReceiverClosed) {
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
		ack, err := ReadAcceleratedAckFrame(conn)
		if err != nil {
			return err
		}
		if ack.Offset != frame.Offset || ack.Length != int64(len(frame.Payload)) {
			return errors.New("unexpected accelerated ack frame")
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

func acknowledgeAcceleratedTestFrames(t *testing.T, receiver *AcceleratedReceiver, frames map[int64]int64) {
	t.Helper()

	for offset, length := range frames {
		if err := receiver.AcknowledgeFrame(offset, length); err != nil {
			t.Fatalf("acknowledge frame offset=%d length=%d: %v", offset, length, err)
		}
	}
}

type blockingAckConn struct {
	net.Conn
	ackExposed       chan<- struct{}
	releaseAckReturn <-chan struct{}
	blocked          bool
}

func (c *blockingAckConn) Write(p []byte) (int, error) {
	written, err := c.Conn.Write(p)
	if err != nil {
		return written, err
	}
	if !c.blocked {
		c.blocked = true
		close(c.ackExposed)
		<-c.releaseAckReturn
	}
	return written, nil
}
