package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"shareme/backend/internal/discovery"
	"shareme/backend/internal/protocol"
)

func TestSessionSenderUploadsLargeFileAsMultipleParts(t *testing.T) {
	payload := []byte("abcdefghijkl")
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-1",
			ChunkSize:             5,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v1",
		},
		completeResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-1",
			State:      "done",
		},
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(2, 2, 8),
		nil,
	)

	response, err := sender.Send(context.Background(), bytes.NewReader(payload), SessionMeta{
		TransferID:     "transfer-1",
		MessageID:      "msg-1",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("send multipart session: %v", err)
	}
	if response.State != "done" {
		t.Fatalf("unexpected complete response: %#v", response)
	}
	if len(transport.startRequests) != 1 {
		t.Fatalf("expected one session start request, got %d", len(transport.startRequests))
	}
	if len(transport.partRequests) != 3 {
		t.Fatalf("expected three uploaded parts, got %d", len(transport.partRequests))
	}
	lengthsByPart := make(map[int]int64, len(transport.partRequests))
	for _, request := range transport.partRequests {
		lengthsByPart[request.PartIndex] = request.Length
	}
	if lengthsByPart[0] != 5 || lengthsByPart[1] != 5 || lengthsByPart[2] != 2 {
		t.Fatalf("unexpected part lengths: %#v", transport.partRequests)
	}
	if len(transport.completeRequests) != 1 {
		t.Fatalf("expected one complete request, got %d", len(transport.completeRequests))
	}
	if transport.completeRequests[0].PartCount != 3 {
		t.Fatalf("unexpected part count: %#v", transport.completeRequests[0])
	}
	if transport.completeRequests[0].FileSHA256 != sessionSenderSHA256Hex(payload) {
		t.Fatalf("unexpected file hash: %#v", transport.completeRequests[0])
	}
}

func TestSessionSenderCompletesWithLiveContext(t *testing.T) {
	payload := []byte("abcdefghijkl")
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-1",
			ChunkSize:             5,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v1",
		},
		completeResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-1",
			State:      "done",
		},
		requireLiveCompleteContext: true,
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(2, 2, 8),
		nil,
	)

	if _, err := sender.Send(context.Background(), bytes.NewReader(payload), SessionMeta{
		TransferID:     "transfer-1",
		MessageID:      "msg-1",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		AgentTCPPort:   19090,
	}); err != nil {
		t.Fatalf("expected complete request to use live context, got %v", err)
	}
}

func TestSessionSenderStopsReadingSourceAfterPartFailure(t *testing.T) {
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-1",
			ChunkSize:             5,
			InitialParallelism:    1,
			MaxParallelism:        1,
			AdaptivePolicyVersion: "v1",
		},
		failPartIndex: 0,
		uploadErr:     errors.New("boom"),
	}
	reader := &countingReader{payload: []byte("abcdefghijklmnopqrst")}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(1, 1, 1),
		nil,
	)

	if _, err := sender.Send(context.Background(), reader, SessionMeta{
		TransferID:     "transfer-1",
		MessageID:      "msg-1",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       int64(len(reader.payload)),
		AgentTCPPort:   19090,
	}); err == nil {
		t.Fatal("expected multipart send to fail after first part error")
	}

	if reader.readBytes != 5 {
		t.Fatalf("expected sender to stop reading after first failed part, got %d bytes", reader.readBytes)
	}
}

func TestSessionSenderForgetsSessionModeAfterPartFailure(t *testing.T) {
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-forget",
			ChunkSize:             5,
			InitialParallelism:    1,
			MaxParallelism:        1,
			AdaptivePolicyVersion: "v2-lan-fast",
		},
		failPartIndex: 0,
		uploadErr:     errors.New("boom"),
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(1, 1, 1),
		nil,
	)

	if _, err := sender.Send(context.Background(), bytes.NewReader([]byte("hello")), SessionMeta{
		TransferID:     "transfer-forget",
		MessageID:      "msg-forget",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       5,
		AgentTCPPort:   19090,
	}); err == nil {
		t.Fatal("expected multipart send to fail")
	}

	if len(transport.forgottenSessionIDs) != 1 || transport.forgottenSessionIDs[0] != "session-forget" {
		t.Fatalf("expected sender to forget failed session mode, got %#v", transport.forgottenSessionIDs)
	}
}

func TestSessionSenderCancellationUnblocksGateWhenUploadRespectsContext(t *testing.T) {
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-cancel",
			ChunkSize:             5,
			InitialParallelism:    1,
			MaxParallelism:        1,
			AdaptivePolicyVersion: "v1",
		},
		blockPartIndex:     0,
		blockUploadStarted: make(chan struct{}),
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(1, 1, 1),
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan error, 1)
	go func() {
		_, err := sender.Send(ctx, bytes.NewReader([]byte("helloworld")), SessionMeta{
			TransferID:     "transfer-cancel",
			MessageID:      "msg-cancel",
			SenderDeviceID: "local-1",
			FileName:       "hello.txt",
			FileSize:       10,
			AgentTCPPort:   19090,
		})
		resultCh <- err
	}()

	select {
	case <-transport.blockUploadStarted:
	case <-time.After(time.Second):
		t.Fatal("expected first upload to start blocking")
	}

	cancel()

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected canceled multipart send to fail")
		}
	case <-time.After(time.Second):
		t.Fatal("expected sender cancellation to return without hanging on gate")
	}
}

func TestParallelGateAcquireReturnsWhenContextCanceledWhileWaiting(t *testing.T) {
	gate := newParallelGate(1)
	if err := gate.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire initial slot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- gate.Acquire(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled acquire, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected waiting acquire to exit after context cancellation")
	}
}

func TestSessionSenderReadFailureWaitsForInFlightUploadsBeforeForgettingSession(t *testing.T) {
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-read-fail",
			ChunkSize:             5,
			InitialParallelism:    2,
			MaxParallelism:        2,
			AdaptivePolicyVersion: "v2-lan-fast",
		},
		blockPartIndex:      0,
		blockUploadStarted:  make(chan struct{}),
		blockUploadReleased: make(chan struct{}),
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(2, 2, 2),
		nil,
	)

	reader := &stagedEOFReader{
		firstChunk: []byte("hello"),
		waitFor:    transport.blockUploadStarted,
	}
	if _, err := sender.Send(context.Background(), reader, SessionMeta{
		TransferID:     "transfer-read-fail",
		MessageID:      "msg-read-fail",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       10,
		AgentTCPPort:   19090,
	}); err == nil {
		t.Fatal("expected source read failure")
	}

	if transport.forgetBeforeUploadRelease {
		t.Fatal("expected sender to wait for in-flight upload cleanup before forgetting session")
	}
}

func TestSessionSenderClampsRemoteChunkSizeToLocalBounds(t *testing.T) {
	payload := make([]byte, 1024)
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-clamped-chunk",
			ChunkSize:             1,
			InitialParallelism:    2,
			MaxParallelism:        2,
			AdaptivePolicyVersion: "v1",
		},
		completeResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-clamped-chunk",
			State:      "done",
		},
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		nil,
		nil,
	)

	if _, err := sender.Send(context.Background(), bytes.NewReader(payload), SessionMeta{
		TransferID:     "transfer-clamped-chunk",
		MessageID:      "msg-clamped-chunk",
		SenderDeviceID: "local-1",
		FileName:       "small.bin",
		FileSize:       int64(len(payload)),
		AgentTCPPort:   19090,
	}); err != nil {
		t.Fatalf("send multipart session: %v", err)
	}

	if len(transport.partRequests) != 1 {
		t.Fatalf("expected sender to clamp tiny remote chunk size, got %d parts", len(transport.partRequests))
	}
}

func TestSessionSenderClampsRemoteParallelismToLocalBounds(t *testing.T) {
	payload := make([]byte, 20<<20)
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-clamped-parallelism",
			ChunkSize:             1 << 20,
			InitialParallelism:    999,
			MaxParallelism:        999,
			AdaptivePolicyVersion: "v1",
		},
		completeResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-clamped-parallelism",
			State:      "done",
		},
		uploadDelayForPart: func(_ int) time.Duration {
			return 50 * time.Millisecond
		},
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		nil,
		nil,
	)

	if _, err := sender.Send(context.Background(), bytes.NewReader(payload), SessionMeta{
		TransferID:     "transfer-clamped-parallelism",
		MessageID:      "msg-clamped-parallelism",
		SenderDeviceID: "local-1",
		FileName:       "parallel.bin",
		FileSize:       int64(len(payload)),
		AgentTCPPort:   19090,
	}); err != nil {
		t.Fatalf("send multipart session: %v", err)
	}

	if transport.maxActiveUploads > DefaultSessionMaxParallelism {
		t.Fatalf(
			"expected sender to clamp remote parallelism to %d, got %d",
			DefaultSessionMaxParallelism,
			transport.maxActiveUploads,
		)
	}
}

func TestSessionSenderFastPathSkipsWholeFileHashOnComplete(t *testing.T) {
	payload := []byte("abcdefghijkl")
	transport := &fakeSessionTransport{
		startResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-fast",
			ChunkSize:             5,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v2-lan-fast",
		},
		completeResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-fast",
			State:      "done",
		},
	}

	sender := NewSessionSender(
		transport,
		discovery.PeerRecord{DeviceID: "peer-1", LastKnownAddr: "127.0.0.1:19090"},
		NewAdaptiveParallelism(2, 2, 8),
		nil,
	)

	if _, err := sender.Send(context.Background(), bytes.NewReader(payload), SessionMeta{
		TransferID:     "transfer-fast",
		MessageID:      "msg-fast",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		AgentTCPPort:   19090,
	}); err != nil {
		t.Fatalf("send multipart session: %v", err)
	}

	if len(transport.completeRequests) != 1 {
		t.Fatalf("expected one complete request, got %d", len(transport.completeRequests))
	}
	if transport.completeRequests[0].FileSHA256 != "" {
		t.Fatalf("expected fast path to skip whole-file hash, got %#v", transport.completeRequests[0])
	}
}

type fakeSessionTransport struct {
	startResponse              protocol.TransferSessionStartResponse
	completeResponse           protocol.TransferSessionCompleteResponse
	startRequests              []protocol.TransferSessionStartRequest
	partRequests               []protocol.TransferPartRequest
	completeRequests           []protocol.TransferSessionCompleteRequest
	forgottenSessionIDs        []string
	requireLiveCompleteContext bool
	failPartIndex              int
	uploadErr                  error
	uploadDelayForPart         func(partIndex int) time.Duration
	blockPartIndex             int
	blockUploadStarted         chan struct{}
	blockUploadReleased        chan struct{}
	forgetBeforeUploadRelease  bool
	activeUploads              int
	maxActiveUploads           int
	mu                         sync.Mutex
}

func (f *fakeSessionTransport) StartTransferSession(
	_ context.Context,
	_ discovery.PeerRecord,
	request protocol.TransferSessionStartRequest,
) (protocol.TransferSessionStartResponse, error) {
	f.mu.Lock()
	f.startRequests = append(f.startRequests, request)
	f.mu.Unlock()
	return f.startResponse, nil
}

func (f *fakeSessionTransport) UploadTransferPart(
	ctx context.Context,
	_ discovery.PeerRecord,
	request protocol.TransferPartRequest,
	content io.Reader,
) (protocol.TransferPartResponse, error) {
	f.mu.Lock()
	f.activeUploads++
	if f.activeUploads > f.maxActiveUploads {
		f.maxActiveUploads = f.activeUploads
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if f.activeUploads > 0 {
			f.activeUploads--
		}
		f.mu.Unlock()
	}()

	if f.uploadDelayForPart != nil {
		time.Sleep(f.uploadDelayForPart(request.PartIndex))
	}
	if request.PartIndex == f.blockPartIndex && f.blockUploadStarted != nil {
		select {
		case <-f.blockUploadStarted:
		default:
			close(f.blockUploadStarted)
		}
		<-ctx.Done()
		if f.blockUploadReleased != nil {
			close(f.blockUploadReleased)
		}
		return protocol.TransferPartResponse{}, ctx.Err()
	}
	if _, err := io.ReadAll(content); err != nil {
		return protocol.TransferPartResponse{}, err
	}
	if f.uploadErr != nil && request.PartIndex == f.failPartIndex {
		return protocol.TransferPartResponse{}, f.uploadErr
	}
	f.mu.Lock()
	f.partRequests = append(f.partRequests, request)
	f.mu.Unlock()
	return protocol.TransferPartResponse{
		SessionID:     request.SessionID,
		PartIndex:     request.PartIndex,
		BytesWritten:  request.Length,
		BytesReceived: request.Length,
	}, nil
}

func (f *fakeSessionTransport) CompleteTransferSession(
	ctx context.Context,
	_ discovery.PeerRecord,
	request protocol.TransferSessionCompleteRequest,
) (protocol.TransferSessionCompleteResponse, error) {
	if f.requireLiveCompleteContext && ctx.Err() != nil {
		return protocol.TransferSessionCompleteResponse{}, ctx.Err()
	}
	f.mu.Lock()
	f.completeRequests = append(f.completeRequests, request)
	f.mu.Unlock()
	return f.completeResponse, nil
}

func (f *fakeSessionTransport) ForgetTransferSession(sessionID string) {
	if f.blockUploadReleased != nil {
		select {
		case <-f.blockUploadReleased:
		default:
			f.forgetBeforeUploadRelease = true
		}
	}
	f.mu.Lock()
	f.forgottenSessionIDs = append(f.forgottenSessionIDs, sessionID)
	f.mu.Unlock()
}

func sessionSenderSHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type countingReader struct {
	payload   []byte
	offset    int
	readBytes int
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	if r.offset >= len(r.payload) {
		return 0, io.EOF
	}
	written := copy(buffer, r.payload[r.offset:])
	r.offset += written
	r.readBytes += written
	return written, nil
}

type stagedEOFReader struct {
	firstChunk []byte
	waitFor    <-chan struct{}
	doneFirst  bool
}

func (r *stagedEOFReader) Read(buffer []byte) (int, error) {
	if !r.doneFirst {
		r.doneFirst = true
		return copy(buffer, r.firstChunk), nil
	}
	if r.waitFor != nil {
		<-r.waitFor
	}
	return 0, io.EOF
}
