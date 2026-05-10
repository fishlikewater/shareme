package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"shareme/backend/internal/discovery"
	"shareme/backend/internal/protocol"
)

type SessionTransport interface {
	StartTransferSession(ctx context.Context, peer discovery.PeerRecord, request protocol.TransferSessionStartRequest) (protocol.TransferSessionStartResponse, error)
	UploadTransferPart(ctx context.Context, peer discovery.PeerRecord, request protocol.TransferPartRequest, content io.Reader) (protocol.TransferPartResponse, error)
	CompleteTransferSession(ctx context.Context, peer discovery.PeerRecord, request protocol.TransferSessionCompleteRequest) (protocol.TransferSessionCompleteResponse, error)
}

type sessionTransportCleaner interface {
	ForgetTransferSession(sessionID string)
}

type SessionMeta struct {
	TransferID     string
	MessageID      string
	SenderDeviceID string
	FileName       string
	FileSize       int64
	AgentTCPPort   int
}

type SessionSender struct {
	transport        SessionTransport
	peer             discovery.PeerRecord
	controller       *AdaptiveParallelism
	onCommittedBytes func(int64)
}

type chunkBufferPool struct {
	size int
	pool sync.Pool
}

func NewSessionSender(
	transport SessionTransport,
	peer discovery.PeerRecord,
	controller *AdaptiveParallelism,
	onCommittedBytes func(int64),
) *SessionSender {
	return &SessionSender{
		transport:        transport,
		peer:             peer,
		controller:       controller,
		onCommittedBytes: onCommittedBytes,
	}
}

func newChunkBufferPool(size int64) *chunkBufferPool {
	if size <= 0 || size > int64(^uint(0)>>1) {
		return nil
	}
	bufferSize := int(size)
	return &chunkBufferPool{
		size: bufferSize,
		pool: sync.Pool{
			New: func() any {
				return make([]byte, bufferSize)
			},
		},
	}
}

func (p *chunkBufferPool) Get() []byte {
	if p == nil {
		return nil
	}
	return p.pool.Get().([]byte)
}

func (p *chunkBufferPool) Put(buffer []byte) {
	if p == nil || cap(buffer) != p.size {
		return
	}
	p.pool.Put(buffer[:p.size])
}

func (s *SessionSender) Send(
	ctx context.Context,
	content io.Reader,
	meta SessionMeta,
) (protocol.TransferSessionCompleteResponse, error) {
	parentCtx := ctx
	startResponse, err := s.transport.StartTransferSession(parentCtx, s.peer, protocol.TransferSessionStartRequest{
		TransferID:     meta.TransferID,
		MessageID:      meta.MessageID,
		SenderDeviceID: meta.SenderDeviceID,
		FileName:       meta.FileName,
		FileSize:       meta.FileSize,
		AgentTCPPort:   meta.AgentTCPPort,
	})
	if err != nil {
		return protocol.TransferSessionCompleteResponse{}, err
	}
	var cleanupSession func()
	if cleaner, ok := s.transport.(sessionTransportCleaner); ok {
		cleanupSession = func() {
			cleaner.ForgetTransferSession(startResponse.SessionID)
			cleanupSession = nil
		}
	}

	chunkSize := startResponse.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultSessionChunkSize
	}
	controller := s.controller
	if controller == nil {
		var initial int
		var maxParallelism int
		chunkSize, initial, maxParallelism = ClampSessionProfile(
			startResponse.ChunkSize,
			startResponse.InitialParallelism,
			startResponse.MaxParallelism,
		)
		controller = NewAdaptiveParallelism(initial, SuggestedSessionParallelismFloor(initial), maxParallelism)
	}
	gate := newParallelGate(controller.Current())
	bufferPool := newChunkBufferPool(chunkSize)

	sessionCtx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	var (
		wg            sync.WaitGroup
		once          sync.Once
		sendErr       error
		windowBytes   atomic.Int64
		windowRetries atomic.Int64
	)

	windowDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer close(windowDone)
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				next := controller.Observe(WindowMetrics{
					BytesCommitted: windowBytes.Swap(0),
					Duration:       time.Second,
					RetryCount:     int(windowRetries.Swap(0)),
				})
				gate.SetLimit(next)
			}
		}
	}()

	var fileHash hash.Hash
	if !SessionPolicySupportsFastPath(startResponse.AdaptivePolicyVersion) {
		fileHash = sha256.New()
	}
	partCount := 0
	offset := int64(0)

	for offset < meta.FileSize {
		if err := gate.Acquire(sessionCtx); err != nil {
			break
		}

		remaining := meta.FileSize - offset
		currentChunkSize := chunkSize
		if remaining < currentChunkSize {
			currentChunkSize = remaining
		}
		buffer := bufferPool.Get()
		if buffer == nil {
			buffer = make([]byte, chunkSize)
		}
		chunk := buffer[:currentChunkSize]
		readBytes, err := io.ReadFull(content, chunk)
		if err != nil || int64(readBytes) != currentChunkSize {
			gate.Release()
			bufferPool.Put(buffer)
			cancel()
			wg.Wait()
			<-windowDone
			if cleanupSession != nil {
				cleanupSession()
			}
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return protocol.TransferSessionCompleteResponse{}, fmt.Errorf("read source chunk: %w", err)
		}
		if fileHash != nil {
			_, _ = fileHash.Write(chunk[:readBytes])
		}

		request := protocol.TransferPartRequest{
			SessionID:  startResponse.SessionID,
			TransferID: meta.TransferID,
			PartIndex:  partCount,
			Offset:     offset,
			Length:     int64(readBytes),
		}
		partData := chunk[:readBytes]

		wg.Add(1)
		go func(partRequest protocol.TransferPartRequest, data []byte, reusableBuffer []byte) {
			defer wg.Done()
			defer gate.Release()
			defer bufferPool.Put(reusableBuffer)

			if err := s.uploadPartWithRetry(sessionCtx, partRequest, data, &windowRetries); err != nil {
				once.Do(func() {
					sendErr = err
					cancel()
				})
				return
			}

			windowBytes.Add(int64(len(data)))
			if s.onCommittedBytes != nil {
				s.onCommittedBytes(int64(len(data)))
			}
		}(request, partData, buffer)

		offset += int64(readBytes)
		partCount++
	}

	wg.Wait()
	cancel()
	<-windowDone

	if sendErr != nil {
		if cleanupSession != nil {
			cleanupSession()
		}
		return protocol.TransferSessionCompleteResponse{}, sendErr
	}
	if err := parentCtx.Err(); err != nil {
		if cleanupSession != nil {
			cleanupSession()
		}
		return protocol.TransferSessionCompleteResponse{}, err
	}

	fileSHA256 := ""
	if fileHash != nil {
		fileSHA256 = hex.EncodeToString(fileHash.Sum(nil))
	}
	response, err := s.transport.CompleteTransferSession(parentCtx, s.peer, protocol.TransferSessionCompleteRequest{
		SessionID:  startResponse.SessionID,
		TransferID: meta.TransferID,
		TotalSize:  meta.FileSize,
		PartCount:  partCount,
		FileSHA256: fileSHA256,
	})
	if cleanupSession != nil {
		cleanupSession()
	}
	return response, err
}

func (s *SessionSender) uploadPartWithRetry(
	ctx context.Context,
	request protocol.TransferPartRequest,
	data []byte,
	retryCounter *atomic.Int64,
) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err = s.transport.UploadTransferPart(ctx, s.peer, request, bytes.NewReader(data))
		if err == nil {
			return nil
		}
		if attempt < 2 {
			retryCounter.Add(1)
		}
	}
	return err
}

type parallelGate struct {
	mu    sync.Mutex
	cond  *sync.Cond
	limit int
	inUse int
}

func newParallelGate(limit int) *parallelGate {
	if limit <= 0 {
		limit = 1
	}
	gate := &parallelGate{limit: limit}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (g *parallelGate) Acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	waitDone := make(chan struct{})
	defer close(waitDone)
	go func() {
		select {
		case <-ctx.Done():
			g.mu.Lock()
			g.cond.Broadcast()
			g.mu.Unlock()
		case <-waitDone:
		}
	}()

	g.mu.Lock()
	defer g.mu.Unlock()
	for g.inUse >= g.limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		g.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g.inUse++
	return nil
}

func (g *parallelGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inUse > 0 {
		g.inUse--
	}
	g.cond.Broadcast()
}

func (g *parallelGate) SetLimit(limit int) {
	if limit <= 0 {
		limit = 1
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.limit = limit
	g.cond.Broadcast()
}
