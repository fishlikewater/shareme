package transfer

import (
	"bytes"
	"context"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"message-share/backend/internal/protocol"
)

func TestAcceleratedStripingControllerMovesAcrossDiscreteLevels(t *testing.T) {
	controller := NewAcceleratedStripingController(1, 8)

	if controller.Current() != 1 {
		t.Fatalf("expected initial striping level 1, got %d", controller.Current())
	}

	controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 100,
		Duration:         time.Second,
	})
	if next := controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 130,
		Duration:         time.Second,
	}); next != 2 {
		t.Fatalf("expected controller to scale up to 2, got %d", next)
	}
	if next := controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 260,
		Duration:         time.Second,
	}); next != 4 {
		t.Fatalf("expected controller to scale up to 4, got %d", next)
	}
	if next := controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 240,
		Duration:         time.Second,
		SenderBlocked:    true,
	}); next != 2 {
		t.Fatalf("expected sender blocking to scale down to 2, got %d", next)
	}
	if next := controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 220,
		Duration:         time.Second,
		ReceiverBacklog:  true,
	}); next != 1 {
		t.Fatalf("expected receiver backlog to scale down to 1, got %d", next)
	}
}

func TestAcceleratedSenderStreamsFileThroughDedicatedTCPListener(t *testing.T) {
	payload := []byte("abcdefghijkl")
	receiver, err := NewAcceleratedReceiver(t.TempDir(), "payload.bin", int64(len(payload)), 4)
	if err != nil {
		t.Fatalf("new accelerated receiver: %v", err)
	}
	defer receiver.Cleanup()

	address := startAcceleratedTestListener(t, receiver, "session-send", "token-send")

	var dialCount atomic.Int32
	sender := NewAcceleratedSender(
		func(ctx context.Context, laneIndex int, _ protocol.AcceleratedPrepareResponse) (net.Conn, error) {
			dialCount.Add(1)
			return (&net.Dialer{}).DialContext(ctx, "tcp", address)
		},
		NewAcceleratedStripingController(2, 8),
	)

	err = sender.Send(context.Background(), bytes.NewReader(payload), int64(len(payload)), protocol.AcceleratedPrepareResponse{
		SessionID:      "session-send",
		TransferToken:  "token-send",
		ChunkSize:      4,
		InitialStripes: 2,
		MaxStripes:     8,
	})
	if err != nil {
		t.Fatalf("accelerated send: %v", err)
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
	if dialCount.Load() < 2 {
		t.Fatalf("expected dedicated sender to open multiple TCP lanes, got %d", dialCount.Load())
	}
}
