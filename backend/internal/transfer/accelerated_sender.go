package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"shareme/backend/internal/protocol"
)

const acceleratedDefaultChunkSize int64 = 8 << 20

const (
	acceleratedSenderBlockedThreshold = 2 * time.Second
	acceleratedDefaultAckTimeout      = 15 * time.Second
)

type AcceleratedSendPhase string

const (
	AcceleratedSendPhaseConnect AcceleratedSendPhase = "connect"
	AcceleratedSendPhaseStream  AcceleratedSendPhase = "stream"
	AcceleratedSendPhaseSource  AcceleratedSendPhase = "source"
)

type AcceleratedSendError struct {
	Phase AcceleratedSendPhase
	Err   error
}

func (e *AcceleratedSendError) Error() string {
	return fmt.Sprintf("accelerated %s failed: %v", e.Phase, e.Err)
}

func (e *AcceleratedSendError) Unwrap() error {
	return e.Err
}

type AcceleratedStripingWindow struct {
	BytesTransferred int64
	Duration         time.Duration
	SenderBlocked    bool
	ReceiverBacklog  bool
}

func (w AcceleratedStripingWindow) throughput() float64 {
	if w.Duration <= 0 {
		return 0
	}
	return float64(w.BytesTransferred) / w.Duration.Seconds()
}

type AcceleratedStripingController struct {
	levels      []int
	current     int
	initialized bool
	previousTPS float64
	declines    int
}

func NewAcceleratedStripingController(initial int, max int) *AcceleratedStripingController {
	levels := acceleratedLevelsUpTo(max)
	current := levels[0]
	for _, level := range levels {
		if initial >= level {
			current = level
		}
	}
	return &AcceleratedStripingController{
		levels:  levels,
		current: current,
	}
}

func (c *AcceleratedStripingController) Current() int {
	return c.current
}

func (c *AcceleratedStripingController) Observe(window AcceleratedStripingWindow) int {
	currentTPS := window.throughput()
	if !c.initialized {
		c.initialized = true
		c.previousTPS = currentTPS
		return c.current
	}

	switch {
	case window.SenderBlocked || window.ReceiverBacklog:
		c.declines = 0
		c.stepDown()
	case currentTPS > 0 && c.previousTPS > 0 && currentTPS >= c.previousTPS*1.15 && !window.SenderBlocked && !window.ReceiverBacklog:
		c.declines = 0
		c.stepUp()
	case currentTPS > 0 && c.previousTPS > 0 && currentTPS <= c.previousTPS*0.90:
		c.declines++
		if c.declines >= 2 {
			c.stepDown()
			c.declines = 0
		}
	default:
		c.declines = 0
	}

	if currentTPS > 0 {
		c.previousTPS = currentTPS
	}
	return c.current
}

func (c *AcceleratedStripingController) stepUp() {
	for index, level := range c.levels {
		if level == c.current && index < len(c.levels)-1 {
			c.current = c.levels[index+1]
			return
		}
	}
}

func (c *AcceleratedStripingController) stepDown() {
	for index, level := range c.levels {
		if level == c.current && index > 0 {
			c.current = c.levels[index-1]
			return
		}
	}
}

type AcceleratedDialFunc func(context.Context, int, protocol.AcceleratedPrepareResponse) (net.Conn, error)

type AcceleratedSender struct {
	dialLane         AcceleratedDialFunc
	controller       *AcceleratedStripingController
	onChunkCommitted func(int64)
}

type acceleratedChunkResult struct {
	ackLatency time.Duration
	duration   time.Duration
}

func NewAcceleratedSender(dialLane AcceleratedDialFunc, controller *AcceleratedStripingController) *AcceleratedSender {
	return &AcceleratedSender{
		dialLane:   dialLane,
		controller: controller,
	}
}

func (s *AcceleratedSender) SetOnChunkCommitted(onChunkCommitted func(int64)) {
	s.onChunkCommitted = onChunkCommitted
}

func (s *AcceleratedSender) Send(
	ctx context.Context,
	source io.ReaderAt,
	totalSize int64,
	prepare protocol.AcceleratedPrepareResponse,
) error {
	if source == nil {
		return fmt.Errorf("accelerated source reader required")
	}
	if s.dialLane == nil {
		return fmt.Errorf("accelerated dialer required")
	}
	if totalSize < 0 {
		return fmt.Errorf("invalid total size: %d", totalSize)
	}
	if totalSize == 0 {
		return nil
	}

	chunkSize := prepare.ChunkSize
	if chunkSize <= 0 {
		chunkSize = acceleratedDefaultChunkSize
	}
	ackTimeout := time.Duration(prepare.AckTimeoutMillis) * time.Millisecond
	if ackTimeout <= 0 {
		ackTimeout = acceleratedDefaultAckTimeout
	}
	controller := s.controller
	if controller == nil {
		controller = NewAcceleratedStripingController(prepare.InitialStripes, prepare.MaxStripes)
	}

	offset := int64(0)
	for offset < totalSize {
		stripes := controller.Current()
		if stripes <= 0 {
			stripes = 1
		}

		type chunkTask struct {
			laneIndex int
			offset    int64
			length    int64
		}

		tasks := make([]chunkTask, 0, stripes)
		maxInFlightBytes := prepare.MaxInFlightBytes
		if maxInFlightBytes <= 0 {
			maxInFlightBytes = chunkSize * int64(stripes)
		}
		if maxInFlightBytes < chunkSize {
			maxInFlightBytes = chunkSize
		}
		batchBytes := int64(0)
		for laneIndex := 0; laneIndex < stripes && offset < totalSize; laneIndex++ {
			length := chunkSize
			if remaining := totalSize - offset; remaining < length {
				length = remaining
			}
			if batchBytes > 0 && batchBytes+length > maxInFlightBytes {
				break
			}
			tasks = append(tasks, chunkTask{
				laneIndex: laneIndex,
				offset:    offset,
				length:    length,
			})
			batchBytes += length
			offset += length
		}
		if len(tasks) == 0 && offset < totalSize {
			length := chunkSize
			if remaining := totalSize - offset; remaining < length {
				length = remaining
			}
			tasks = append(tasks, chunkTask{
				laneIndex: 0,
				offset:    offset,
				length:    length,
			})
			batchBytes = length
			offset += length
		}
		receiverWindowLimited := len(tasks) < stripes && offset < totalSize

		startedAt := time.Now()
		var writtenBytes atomic.Int64
		var senderBlocked atomic.Bool
		var maxAckLatency atomic.Int64
		errCh := make(chan error, len(tasks))
		var wg sync.WaitGroup

		for _, task := range tasks {
			task := task
			wg.Add(1)
			go func() {
				defer wg.Done()
				laneStartedAt := time.Now()
				result, err := s.sendChunk(ctx, source, prepare, task.laneIndex, task.offset, task.length, ackTimeout)
				if err != nil {
					errCh <- err
					return
				}
				laneDuration := time.Since(laneStartedAt)
				if laneDuration >= acceleratedSenderBlockedThreshold {
					senderBlocked.Store(true)
				}
				ackLatency := result.ackLatency
				for {
					current := maxAckLatency.Load()
					if current >= int64(ackLatency) || maxAckLatency.CompareAndSwap(current, int64(ackLatency)) {
						break
					}
				}
				writtenBytes.Add(task.length)
			}()
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				return err
			}
		}

		receiverBacklog := receiverWindowLimited
		if !receiverBacklog && time.Duration(maxAckLatency.Load()) >= ackTimeout/2 {
			receiverBacklog = true
		}
		controller.Observe(AcceleratedStripingWindow{
			BytesTransferred: writtenBytes.Load(),
			Duration:         time.Since(startedAt),
			SenderBlocked:    senderBlocked.Load(),
			ReceiverBacklog:  receiverBacklog,
		})
	}
	return nil
}

func (s *AcceleratedSender) sendChunk(
	ctx context.Context,
	source io.ReaderAt,
	prepare protocol.AcceleratedPrepareResponse,
	laneIndex int,
	offset int64,
	length int64,
	ackTimeout time.Duration,
) (acceleratedChunkResult, error) {
	if length <= 0 {
		return acceleratedChunkResult{}, nil
	}
	if length > int64(^uint(0)>>1) {
		return acceleratedChunkResult{}, fmt.Errorf("chunk length too large: %d", length)
	}

	conn, err := s.dialLane(ctx, laneIndex, prepare)
	if err != nil {
		return acceleratedChunkResult{}, &AcceleratedSendError{Phase: AcceleratedSendPhaseConnect, Err: err}
	}
	defer conn.Close()

	if err := WriteAcceleratedHello(conn, AcceleratedHelloFrame{
		SessionID:     prepare.SessionID,
		TransferToken: prepare.TransferToken,
		LaneIndex:     laneIndex,
	}); err != nil {
		return acceleratedChunkResult{}, &AcceleratedSendError{Phase: AcceleratedSendPhaseStream, Err: err}
	}

	buffer := make([]byte, int(length))
	readBytes, err := source.ReadAt(buffer, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return acceleratedChunkResult{}, &AcceleratedSendError{Phase: AcceleratedSendPhaseSource, Err: err}
	}
	if int64(readBytes) != length {
		return acceleratedChunkResult{}, &AcceleratedSendError{Phase: AcceleratedSendPhaseSource, Err: io.ErrUnexpectedEOF}
	}

	chunkStartedAt := time.Now()
	if err := WriteAcceleratedDataFrame(conn, AcceleratedDataFrame{
		Offset:  offset,
		Payload: buffer[:readBytes],
	}); err != nil {
		return acceleratedChunkResult{}, &AcceleratedSendError{Phase: AcceleratedSendPhaseStream, Err: err}
	}

	if ackTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(ackTimeout))
	}
	ackStartedAt := time.Now()
	ack, err := ReadAcceleratedAckFrame(conn)
	if err != nil {
		if isAcceleratedAckTimeout(err) {
			return acceleratedChunkResult{}, &AcceleratedSendError{
				Phase: AcceleratedSendPhaseStream,
				Err:   fmt.Errorf("receiver ack timeout: %w", err),
			}
		}
		return acceleratedChunkResult{}, &AcceleratedSendError{
			Phase: AcceleratedSendPhaseStream,
			Err:   fmt.Errorf("read receiver ack: %w", err),
		}
	}
	if ack.Offset != offset || ack.Length != int64(readBytes) {
		return acceleratedChunkResult{}, &AcceleratedSendError{
			Phase: AcceleratedSendPhaseStream,
			Err: fmt.Errorf(
				"receiver ack mismatch: offset=%d length=%d",
				ack.Offset,
				ack.Length,
			),
		}
	}
	if s.onChunkCommitted != nil {
		s.onChunkCommitted(int64(readBytes))
	}
	return acceleratedChunkResult{
		ackLatency: time.Since(ackStartedAt),
		duration:   time.Since(chunkStartedAt),
	}, nil
}

func acceleratedLevelsUpTo(max int) []int {
	levels := []int{1}
	for _, level := range []int{2, 4, 8} {
		if max >= level {
			levels = append(levels, level)
		}
	}
	return levels
}

func isAcceleratedAckTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
