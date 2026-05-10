package transfer

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"shareme/backend/internal/protocol"
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

func TestAcceleratedStripingControllerNeedsTwoThroughputDeclinesBeforeStepDown(t *testing.T) {
	controller := NewAcceleratedStripingController(2, 2)

	controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 100,
		Duration:         time.Second,
	})
	if next := controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 89,
		Duration:         time.Second,
	}); next != 2 {
		t.Fatalf("expected single throughput dip to keep striping at 2, got %d", next)
	}
	if next := controller.Observe(AcceleratedStripingWindow{
		BytesTransferred: 80,
		Duration:         time.Second,
	}); next != 1 {
		t.Fatalf("expected repeated throughput decline to scale down to 1, got %d", next)
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

func TestAcceleratedSenderWaitsForReceiverAckBeforeCommitting(t *testing.T) {
	payload := []byte("abcd")
	ackObserved := make(chan struct{})
	ackReleased := make(chan struct{})
	address := startAcceleratedAckServer(t, func(conn net.Conn) error {
		defer conn.Close()

		if _, err := ReadAcceleratedHello(conn); err != nil {
			return err
		}
		frame, err := ReadAcceleratedDataFrame(conn)
		if err != nil {
			return err
		}
		close(ackObserved)
		<-ackReleased
		return WriteAcceleratedAckFrame(conn, AcceleratedAckFrame{
			Offset: frame.Offset,
			Length: int64(len(frame.Payload)),
		})
	})

	var committed atomic.Int64
	sender := NewAcceleratedSender(func(ctx context.Context, laneIndex int, _ protocol.AcceleratedPrepareResponse) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}, NewAcceleratedStripingController(1, 1))
	sender.SetOnChunkCommitted(func(delta int64) {
		committed.Add(delta)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- sender.Send(context.Background(), bytes.NewReader(payload), int64(len(payload)), protocol.AcceleratedPrepareResponse{
			SessionID:        "session-ack",
			TransferToken:    "token-ack",
			ChunkSize:        4,
			InitialStripes:   1,
			MaxStripes:       1,
			MaxInFlightBytes: 4,
			AckTimeoutMillis: 500,
		})
	}()

	select {
	case <-ackObserved:
	case <-time.After(time.Second):
		t.Fatal("expected sender to wait for receiver ack")
	}
	if committed.Load() != 0 {
		t.Fatalf("expected committed bytes to remain 0 before ack, got %d", committed.Load())
	}

	close(ackReleased)
	if err := <-errCh; err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if committed.Load() != int64(len(payload)) {
		t.Fatalf("expected committed bytes after receiver ack, got %d", committed.Load())
	}
}

func TestAcceleratedSenderFailsWhenReceiverAckTimesOut(t *testing.T) {
	address := startAcceleratedAckServer(t, func(conn net.Conn) error {
		defer conn.Close()

		if _, err := ReadAcceleratedHello(conn); err != nil {
			return err
		}
		if _, err := ReadAcceleratedDataFrame(conn); err != nil {
			return err
		}
		time.Sleep(300 * time.Millisecond)
		return nil
	})

	sender := NewAcceleratedSender(func(ctx context.Context, laneIndex int, _ protocol.AcceleratedPrepareResponse) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}, NewAcceleratedStripingController(1, 1))

	err := sender.Send(context.Background(), bytes.NewReader([]byte("abcd")), 4, protocol.AcceleratedPrepareResponse{
		SessionID:        "session-timeout",
		TransferToken:    "token-timeout",
		ChunkSize:        4,
		InitialStripes:   1,
		MaxStripes:       1,
		MaxInFlightBytes: 4,
		AckTimeoutMillis: 50,
	})
	if err == nil || !strings.Contains(err.Error(), "ack") {
		t.Fatalf("expected ack timeout error, got %v", err)
	}
}

func TestAcceleratedSenderSucceedsWhenReceiverAckIsSlowButContinuous(t *testing.T) {
	payload := []byte("abcdefgh")
	address := startAcceleratedAckServer(t, func(conn net.Conn) error {
		defer conn.Close()

		if _, err := ReadAcceleratedHello(conn); err != nil {
			return err
		}
		frame, err := ReadAcceleratedDataFrame(conn)
		if err != nil {
			return err
		}
		time.Sleep(40 * time.Millisecond)
		return WriteAcceleratedAckFrame(conn, AcceleratedAckFrame{
			Offset: frame.Offset,
			Length: int64(len(frame.Payload)),
		})
	})

	var committed atomic.Int64
	sender := NewAcceleratedSender(func(ctx context.Context, laneIndex int, _ protocol.AcceleratedPrepareResponse) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}, NewAcceleratedStripingController(2, 2))
	sender.SetOnChunkCommitted(func(delta int64) {
		committed.Add(delta)
	})

	err := sender.Send(context.Background(), bytes.NewReader(payload), int64(len(payload)), protocol.AcceleratedPrepareResponse{
		SessionID:        "session-slow-ack",
		TransferToken:    "token-slow-ack",
		ChunkSize:        4,
		InitialStripes:   2,
		MaxStripes:       2,
		MaxInFlightBytes: 4,
		AckTimeoutMillis: 250,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if committed.Load() != int64(len(payload)) {
		t.Fatalf("expected all bytes committed after slow but continuous acks, got %d", committed.Load())
	}
}

func TestAcceleratedSenderDoesNotTreatFullWindowAsReceiverBacklog(t *testing.T) {
	payload := []byte("abcdefghijklmnop")
	address := startAcceleratedAckServer(t, func(conn net.Conn) error {
		defer conn.Close()

		if _, err := ReadAcceleratedHello(conn); err != nil {
			return err
		}
		frame, err := ReadAcceleratedDataFrame(conn)
		if err != nil {
			return err
		}
		return WriteAcceleratedAckFrame(conn, AcceleratedAckFrame{
			Offset: frame.Offset,
			Length: int64(len(frame.Payload)),
		})
	})

	controller := NewAcceleratedStripingController(2, 2)
	sender := NewAcceleratedSender(func(ctx context.Context, laneIndex int, _ protocol.AcceleratedPrepareResponse) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}, controller)

	if err := sender.Send(context.Background(), bytes.NewReader(payload), int64(len(payload)), protocol.AcceleratedPrepareResponse{
		SessionID:        "session-full-window",
		TransferToken:    "token-full-window",
		ChunkSize:        4,
		InitialStripes:   2,
		MaxStripes:       2,
		MaxInFlightBytes: 8,
		AckTimeoutMillis: 2000,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if controller.Current() != 2 {
		t.Fatalf("expected striping to remain at 2 when batches only fill the allowed window, got %d", controller.Current())
	}
}

func TestAcceleratedSenderDoesNotTreatHealthyFullWindowAckAsReceiverBacklog(t *testing.T) {
	payload := []byte("abcdefghijklmnop")
	address := startAcceleratedAckServer(t, func(conn net.Conn) error {
		defer conn.Close()

		if _, err := ReadAcceleratedHello(conn); err != nil {
			return err
		}
		frame, err := ReadAcceleratedDataFrame(conn)
		if err != nil {
			return err
		}
		time.Sleep(40 * time.Millisecond)
		return WriteAcceleratedAckFrame(conn, AcceleratedAckFrame{
			Offset: frame.Offset,
			Length: int64(len(frame.Payload)),
		})
	})

	controller := NewAcceleratedStripingController(2, 2)
	sender := NewAcceleratedSender(func(ctx context.Context, laneIndex int, _ protocol.AcceleratedPrepareResponse) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}, controller)

	if err := sender.Send(context.Background(), bytes.NewReader(payload), int64(len(payload)), protocol.AcceleratedPrepareResponse{
		SessionID:        "session-healthy-full-window",
		TransferToken:    "token-healthy-full-window",
		ChunkSize:        4,
		InitialStripes:   2,
		MaxStripes:       2,
		MaxInFlightBytes: 8,
		AckTimeoutMillis: 2000,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if controller.Current() != 2 {
		t.Fatalf("expected striping to remain at 2 when full-window ack stays healthy, got %d", controller.Current())
	}
}

func startAcceleratedAckServer(t *testing.T, handler func(net.Conn) error) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen accelerated ack server: %v", err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					continue
				}
				if strings.Contains(err.Error(), "closed") {
					return
				}
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := handler(conn); err != nil && !errorsIsEOF(err) {
					t.Errorf("accelerated ack server handler: %v", err)
				}
			}()
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
		<-done
	})

	return listener.Addr().String()
}

func errorsIsEOF(err error) bool {
	return err == nil || err == io.EOF
}
