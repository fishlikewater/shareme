package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"message-share/backend/internal/discovery"
	"message-share/backend/internal/domain"
	"message-share/backend/internal/localfile"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/session"
	"message-share/backend/internal/transfer"
)

type AcceleratedTransport interface {
	PeerTransport
	PrepareAcceleratedTransfer(ctx context.Context, peer discovery.PeerRecord, request protocol.AcceleratedPrepareRequest) (protocol.AcceleratedPrepareResponse, error)
	CompleteAcceleratedTransfer(ctx context.Context, peer discovery.PeerRecord, request protocol.AcceleratedCompleteRequest) (protocol.AcceleratedCompleteResponse, error)
}

type AcceleratedFileSender interface {
	Send(ctx context.Context, source io.ReaderAt, totalSize int64, prepare protocol.AcceleratedPrepareResponse) error
}

type AcceleratedSenderFactory func(peer discovery.PeerRecord) AcceleratedFileSender

type AcceleratedSessionRegistrar interface {
	Register(registration transfer.AcceleratedSessionRegistration)
	Unregister(sessionID string)
}

type incomingAcceleratedSession struct {
	mu             sync.Mutex
	senderDeviceID string
	agentTCPPort   int
	transferRecord domain.Transfer
	receiver       *transfer.AcceleratedReceiver
	telemetry      *transfer.Telemetry
	progressGate   *transfer.ProgressEventGate
	progressWarm   bool
	expiresAt      time.Time
	timer          *time.Timer
	closed         bool
}

const acceleratedPrepareAckTimeoutMillis = 15000

func (s *RuntimeService) SendAcceleratedFile(
	ctx context.Context,
	peerDeviceID string,
	localFileID string,
) (TransferSnapshot, error) {
	lease, err := s.ResolveLocalFile(localFileID)
	if err != nil {
		return TransferSnapshot{}, err
	}
	if !s.cfg.AcceleratedEnabled || lease.Size < multipartThreshold {
		return s.sendLeaseViaStandardPath(ctx, peerDeviceID, lease)
	}

	transport, ok := s.transport.(AcceleratedTransport)
	if !ok {
		return s.sendLeaseViaStandardPath(ctx, peerDeviceID, lease)
	}

	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return TransferSnapshot{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("local device not initialized")
	}
	trustedPeer, ok, err := s.trustedPeer(peerDeviceID)
	if err != nil {
		return TransferSnapshot{}, fmt.Errorf("list trusted peers: %w", err)
	}
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("peer %s is not trusted", peerDeviceID)
	}

	peer, ok := findDiscoveryPeer(s.discovery.List(), peerDeviceID)
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("peer %s not found", peerDeviceID)
	}
	peer.PinnedFingerprint = trustedPeer.PinnedFingerprint

	conversation, err := s.store.EnsureConversation(peerDeviceID)
	if err != nil {
		return TransferSnapshot{}, fmt.Errorf("ensure conversation: %w", err)
	}

	message := session.NewService().NewTextMessage(conversation.ConversationID, lease.DisplayName)
	message.Direction = "outgoing"
	message.Kind = "file"

	transferRecord := domain.Transfer{
		TransferID:       newRandomID("transfer"),
		MessageID:        message.MessageID,
		FileName:         filepath.Base(lease.DisplayName),
		FileSize:         lease.Size,
		State:            transfer.StateSending,
		Direction:        "outgoing",
		BytesTransferred: 0,
		CreatedAt:        message.CreatedAt,
	}

	if err := s.store.SaveMessageWithTransfer(message, transferRecord); err != nil {
		return TransferSnapshot{}, fmt.Errorf("save outgoing accelerated payload: %w", err)
	}
	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)

	prepareResponse, err := transport.PrepareAcceleratedTransfer(ctx, peer, protocol.AcceleratedPrepareRequest{
		TransferID:     transferRecord.TransferID,
		MessageID:      transferRecord.MessageID,
		SenderDeviceID: localDevice.DeviceID,
		FileName:       transferRecord.FileName,
		FileSize:       transferRecord.FileSize,
		AgentTCPPort:   s.cfg.AgentTCPPort,
	})
	if err != nil {
		return s.sendFileFallbackWithExistingTransfer(ctx, transport, peer, localDevice, message, transferRecord, lease)
	}

	file, err := os.Open(lease.Path)
	if err != nil {
		return s.sendFileFallbackWithExistingTransfer(ctx, transport, peer, localDevice, message, transferRecord, lease)
	}
	defer file.Close()

	telemetry := s.transfers.Start(
		transferRecord.TransferID,
		transferRecord.FileSize,
		transferRecord.Direction,
		time.Now().UTC(),
	)
	progressGate := transfer.NewProgressEventGate(transferEventMinInterval, transferEventMinBytes)
	progressMu := sync.Mutex{}
	progressWarm := false

	sender := s.acceleratedSenderFactory(peer)
	if sender == nil {
		s.transfers.Finish(transferRecord.TransferID)
		return s.sendFileFallbackWithExistingTransfer(ctx, transport, peer, localDevice, message, transferRecord, lease)
	}
	if observedSender, ok := sender.(interface{ SetOnChunkCommitted(func(int64)) }); ok {
		observedSender.SetOnChunkCommitted(func(delta int64) {
			progressMu.Lock()
			defer progressMu.Unlock()
			now := time.Now().UTC()
			transferRecord.BytesTransferred += delta
			telemetry.Advance(delta, now)
			if shouldPublishThrottledTransferProgress(
				progressGate,
				&progressWarm,
				transferRecord,
				telemetry.Snapshot(now),
				delta,
				now,
			) {
				s.publishTransferEvent(transferRecord)
			}
		})
	}

	sendErr := sender.Send(ctx, file, lease.Size, prepareResponse)
	if sendErr != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		progressMu.Lock()
		committedBytes := transferRecord.BytesTransferred
		progressMu.Unlock()
		s.transfers.Finish(transferRecord.TransferID)
		if shouldFallbackAcceleratedSend(sendErr, committedBytes) {
			return s.sendFileFallbackWithExistingTransfer(ctx, transport, peer, localDevice, message, transferRecord, lease)
		}
		return s.failOutgoingAcceleratedTransfer(message, transferRecord, sendErr)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		return s.failOutgoingAcceleratedTransfer(message, transferRecord, err)
	}
	fileSHA256, err := fileSHA256Hex(file)
	if err != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		return s.failOutgoingAcceleratedTransfer(message, transferRecord, err)
	}

	completeResponse, err := transport.CompleteAcceleratedTransfer(ctx, peer, protocol.AcceleratedCompleteRequest{
		SessionID:  prepareResponse.SessionID,
		TransferID: transferRecord.TransferID,
		FileSHA256: fileSHA256,
	})
	if err != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		return s.failOutgoingAcceleratedTransfer(message, transferRecord, err)
	}

	s.updatePeerReachability(peer.DeviceID, true)
	if progressGate.Finish(time.Now().UTC()) {
		s.publishTransferEvent(transferRecord)
	}
	if completeResponse.State != "" {
		transferRecord.State = completeResponse.State
	} else {
		transferRecord.State = transfer.StateDone
	}
	transferRecord.BytesTransferred = transferRecord.FileSize
	if transferRecord.State == transfer.StateDone {
		message.Status = "sent"
	} else {
		message.Status = "failed"
	}
	if err := s.store.PersistTransferOutcome(&message, transferRecord); err != nil {
		s.rememberTransferOverride(transferRecord)
		s.transfers.Finish(transferRecord.TransferID)
		s.publishTransferEvent(transferRecord)
		return s.toTransferSnapshot(transferRecord), nil
	}

	s.clearTransferOverride(transferRecord.TransferID)
	s.transfers.Finish(transferRecord.TransferID)
	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)
	return s.toTransferSnapshot(transferRecord), nil
}

func (s *RuntimeService) PrepareAcceleratedTransfer(
	ctx context.Context,
	request protocol.AcceleratedPrepareRequest,
) (protocol.AcceleratedPrepareResponse, error) {
	if !s.cfg.AcceleratedEnabled {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("accelerated transfer disabled")
	}
	if s.acceleratedSessions == nil {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("accelerated session listener not configured")
	}
	if strings.TrimSpace(request.SenderDeviceID) == "" {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("senderDeviceID is required")
	}
	if strings.TrimSpace(request.FileName) == "" {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("fileName is required")
	}
	if request.FileSize <= 0 {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("invalid file size: %d", request.FileSize)
	}

	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
	if !s.isTrustedPeer(request.SenderDeviceID) {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("peer %s is not trusted", request.SenderDeviceID)
	}

	conversation, err := s.store.EnsureConversation(request.SenderDeviceID)
	if err != nil {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("ensure conversation: %w", err)
	}

	chunkSize, initialStripes, maxStripes := recommendedAcceleratedProfile(request.FileSize)
	maxInFlightBytes := recommendedAcceleratedInFlightWindow(chunkSize, maxStripes)
	receiver, err := transfer.NewAcceleratedReceiver(s.cfg.DefaultDownloadDir, request.FileName, request.FileSize, chunkSize)
	if err != nil {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("create accelerated receiver: %w", err)
	}

	messageID := strings.TrimSpace(request.MessageID)
	if messageID == "" {
		messageID = newRandomID("msg")
	}
	transferID := strings.TrimSpace(request.TransferID)
	if transferID == "" {
		transferID = newRandomID("transfer")
	}
	createdAt := time.Now().UTC()

	message := domain.Message{
		MessageID:      messageID,
		ConversationID: conversation.ConversationID,
		Direction:      "incoming",
		Kind:           "file",
		Body:           filepath.Base(request.FileName),
		Status:         "sent",
		CreatedAt:      createdAt,
	}
	transferRecord := domain.Transfer{
		TransferID:       transferID,
		MessageID:        messageID,
		FileName:         filepath.Base(request.FileName),
		FileSize:         request.FileSize,
		State:            transfer.StateReceiving,
		Direction:        "incoming",
		BytesTransferred: 0,
		CreatedAt:        createdAt,
	}

	if err := s.store.SaveMessageWithTransfer(message, transferRecord); err != nil {
		_ = receiver.Cleanup()
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("save incoming accelerated payload: %w", err)
	}
	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)

	sessionID := newRandomID("accel")
	transferToken := newRandomID("token")
	expiresAt := time.Now().UTC().Add(s.incomingTransferSessionTimeout)
	sessionState := &incomingAcceleratedSession{
		senderDeviceID: request.SenderDeviceID,
		agentTCPPort:   request.AgentTCPPort,
		transferRecord: transferRecord,
		receiver:       receiver,
		telemetry: s.transfers.Start(
			transferRecord.TransferID,
			transferRecord.FileSize,
			transferRecord.Direction,
			time.Now().UTC(),
		),
		progressGate: transfer.NewProgressEventGate(transferEventMinInterval, transferEventMinBytes),
		expiresAt:    expiresAt,
	}
	receiver.SetOnFrameCommitted(func(delta int64) {
		now := time.Now().UTC()
		sessionState.mu.Lock()
		if sessionState.closed {
			sessionState.mu.Unlock()
			return
		}
		sessionState.transferRecord.BytesTransferred = sessionState.receiver.BytesReceived()
		sessionState.telemetry.Advance(delta, now)
		snapshot := sessionState.transferRecord
		shouldPublish := shouldPublishThrottledTransferProgress(
			sessionState.progressGate,
			&sessionState.progressWarm,
			snapshot,
			sessionState.telemetry.Snapshot(now),
			delta,
			now,
		)
		sessionState.mu.Unlock()
		if shouldPublish {
			s.publishTransferEvent(snapshot)
		}
	})
	sessionState.timer = time.AfterFunc(s.incomingTransferSessionTimeout, func() {
		s.expireIncomingAcceleratedSession(sessionID)
	})

	s.acceleratedMu.Lock()
	s.incomingAcceleratedSession[sessionID] = sessionState
	s.acceleratedMu.Unlock()

	s.acceleratedSessions.Register(transfer.AcceleratedSessionRegistration{
		SessionID:     sessionID,
		TransferToken: transferToken,
		ExpiresAt:     expiresAt,
		Receiver:      receiver,
	})

	return protocol.AcceleratedPrepareResponse{
		SessionID:             sessionID,
		TransferToken:         transferToken,
		DataPort:              s.cfg.AcceleratedDataPort,
		ChunkSize:             chunkSize,
		InitialStripes:        initialStripes,
		MaxStripes:            maxStripes,
		MaxInFlightBytes:      maxInFlightBytes,
		AckTimeoutMillis:      acceleratedPrepareAckTimeoutMillis,
		AdaptivePolicyVersion: "v1-lan-accelerated",
		ExpiresAtRFC3339:      expiresAt.Format(time.RFC3339Nano),
	}, nil
}

func (s *RuntimeService) CompleteAcceleratedTransfer(
	ctx context.Context,
	request protocol.AcceleratedCompleteRequest,
) (protocol.AcceleratedCompleteResponse, error) {
	sessionState, ok := s.getIncomingAcceleratedSession(request.SessionID)
	if !ok {
		return protocol.AcceleratedCompleteResponse{}, fmt.Errorf("incoming accelerated session not found")
	}
	if strings.TrimSpace(request.TransferID) != "" && request.TransferID != sessionState.transferRecord.TransferID {
		return protocol.AcceleratedCompleteResponse{}, fmt.Errorf("incoming accelerated session transfer mismatch")
	}
	if !s.beginIncomingAcceleratedFinalization(sessionState) {
		return protocol.AcceleratedCompleteResponse{}, fmt.Errorf("incoming accelerated session already closed")
	}

	if _, err := sessionState.receiver.Complete(strings.TrimSpace(request.FileSHA256)); err != nil {
		failedSnapshot := s.finalizeIncomingAcceleratedFailure(request.SessionID, sessionState)
		s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
		s.publishTransferEvent(failedSnapshot)
		return protocol.AcceleratedCompleteResponse{}, fmt.Errorf("complete accelerated transfer: %w", err)
	}

	sessionState.mu.Lock()
	sessionState.transferRecord.State = transfer.StateDone
	sessionState.transferRecord.BytesTransferred = sessionState.receiver.BytesReceived()
	completedSnapshot := sessionState.transferRecord
	sessionState.mu.Unlock()

	if err := s.store.PersistTransferOutcome(nil, completedSnapshot); err != nil {
		s.rememberTransferOverride(completedSnapshot)
		s.transfers.Finish(completedSnapshot.TransferID)
		s.deleteIncomingAcceleratedSession(request.SessionID)
		s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
		s.publishTransferEvent(completedSnapshot)
		return protocol.AcceleratedCompleteResponse{
			TransferID: completedSnapshot.TransferID,
			State:      completedSnapshot.State,
		}, nil
	}

	s.clearTransferOverride(completedSnapshot.TransferID)
	s.transfers.Finish(completedSnapshot.TransferID)
	s.deleteIncomingAcceleratedSession(request.SessionID)
	s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
	s.publishTransferEvent(completedSnapshot)
	return protocol.AcceleratedCompleteResponse{
		TransferID: completedSnapshot.TransferID,
		State:      completedSnapshot.State,
	}, nil
}

func shouldFallbackAcceleratedSend(err error, committedBytes int64) bool {
	if committedBytes > 0 {
		return false
	}
	var sendErr *transfer.AcceleratedSendError
	if !errors.As(err, &sendErr) {
		return false
	}
	return sendErr.Phase == transfer.AcceleratedSendPhaseConnect || sendErr.Phase == transfer.AcceleratedSendPhaseStream
}

func (s *RuntimeService) AuthorizeAcceleratedPrepare(
	_ context.Context,
	request protocol.AcceleratedPrepareRequest,
	caller protocol.PeerCaller,
) error {
	return s.authorizeTrustedPeerRequest(request.SenderDeviceID, caller)
}

func (s *RuntimeService) AuthorizeAcceleratedComplete(
	_ context.Context,
	request protocol.AcceleratedCompleteRequest,
	caller protocol.PeerCaller,
) error {
	sessionState, ok := s.getIncomingAcceleratedSession(request.SessionID)
	if !ok {
		return fmt.Errorf("%w: accelerated session not found", protocol.ErrPeerForbidden)
	}
	return s.authorizeTrustedPeerRequest(sessionState.senderDeviceID, caller)
}

func (s *RuntimeService) sendLeaseViaStandardPath(
	ctx context.Context,
	peerDeviceID string,
	lease localfile.Lease,
) (TransferSnapshot, error) {
	file, err := os.Open(lease.Path)
	if err != nil {
		return TransferSnapshot{}, err
	}
	defer file.Close()
	return s.SendFile(ctx, peerDeviceID, lease.DisplayName, lease.Size, file)
}

func (s *RuntimeService) failOutgoingAcceleratedTransfer(
	message domain.Message,
	transferRecord domain.Transfer,
	cause error,
) (TransferSnapshot, error) {
	transferRecord.State = transfer.StateFailed
	message.Status = "failed"
	if err := s.store.PersistTransferOutcome(&message, transferRecord); err != nil {
		s.rememberTransferOverride(transferRecord)
	} else {
		s.clearTransferOverride(transferRecord.TransferID)
		s.publishMessageEvent(message)
	}
	s.publishTransferEvent(transferRecord)
	return TransferSnapshot{}, fmt.Errorf("accelerated transfer failed: %w", cause)
}

func (s *RuntimeService) sendFileFallbackWithExistingTransfer(
	ctx context.Context,
	transport AcceleratedTransport,
	peer discovery.PeerRecord,
	localDevice domain.LocalDevice,
	message domain.Message,
	transferRecord domain.Transfer,
	lease localfile.Lease,
) (TransferSnapshot, error) {
	file, err := os.Open(lease.Path)
	if err != nil {
		return TransferSnapshot{}, err
	}
	defer file.Close()

	s.transfers.Finish(transferRecord.TransferID)
	transferRecord.State = transfer.StateSending
	transferRecord.BytesTransferred = 0
	s.publishTransferEvent(transferRecord)

	telemetry := s.transfers.Start(
		transferRecord.TransferID,
		transferRecord.FileSize,
		transferRecord.Direction,
		time.Now().UTC(),
	)
	progressGate := transfer.NewProgressEventGate(transferEventMinInterval, transferEventMinBytes)
	progressWarm := false
	advanceProgress := func(delta int64) {
		now := time.Now().UTC()
		transferRecord.BytesTransferred += delta
		telemetry.Advance(delta, now)
		if shouldPublishThrottledTransferProgress(
			progressGate,
			&progressWarm,
			transferRecord,
			telemetry.Snapshot(now),
			delta,
			now,
		) {
			s.publishTransferEvent(transferRecord)
		}
	}

	response, err := transport.SendFile(ctx, peer, protocol.FileTransferRequest{
		TransferID:       transferRecord.TransferID,
		MessageID:        transferRecord.MessageID,
		SenderDeviceID:   localDevice.DeviceID,
		FileName:         transferRecord.FileName,
		FileSize:         transferRecord.FileSize,
		CreatedAtRFC3339: message.CreatedAt.Format(time.RFC3339Nano),
		AgentTCPPort:     s.cfg.AgentTCPPort,
	}, transfer.NewProgressReader(file, advanceProgress))
	if err != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		s.updatePeerReachability(peer.DeviceID, false)
		transferRecord.State = transfer.StateFailed
		message.Status = "failed"
		if saveErr := s.store.PersistTransferOutcome(&message, transferRecord); saveErr == nil {
			s.clearTransferOverride(transferRecord.TransferID)
			s.publishMessageEvent(message)
		} else {
			s.rememberTransferOverride(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		s.publishTransferEvent(transferRecord)
		return TransferSnapshot{}, fmt.Errorf("send file fallback: %w", err)
	}

	s.updatePeerReachability(peer.DeviceID, true)
	if progressGate.Finish(time.Now().UTC()) {
		s.publishTransferEvent(transferRecord)
	}
	if response.State != "" {
		transferRecord.State = response.State
	} else {
		transferRecord.State = transfer.StateDone
	}
	transferRecord.BytesTransferred = transferRecord.FileSize
	if transferRecord.State == transfer.StateDone {
		message.Status = "sent"
	} else {
		message.Status = "failed"
	}
	if err := s.store.PersistTransferOutcome(&message, transferRecord); err != nil {
		s.rememberTransferOverride(transferRecord)
		s.transfers.Finish(transferRecord.TransferID)
		s.publishTransferEvent(transferRecord)
		return s.toTransferSnapshot(transferRecord), nil
	}

	s.clearTransferOverride(transferRecord.TransferID)
	s.transfers.Finish(transferRecord.TransferID)
	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)
	return s.toTransferSnapshot(transferRecord), nil
}

func (s *RuntimeService) getIncomingAcceleratedSession(sessionID string) (*incomingAcceleratedSession, bool) {
	s.acceleratedMu.RLock()
	defer s.acceleratedMu.RUnlock()
	sessionState, ok := s.incomingAcceleratedSession[sessionID]
	return sessionState, ok
}

func (s *RuntimeService) deleteIncomingAcceleratedSession(sessionID string) {
	s.acceleratedMu.Lock()
	sessionState := s.incomingAcceleratedSession[sessionID]
	delete(s.incomingAcceleratedSession, sessionID)
	s.acceleratedMu.Unlock()
	s.unregisterIncomingAcceleratedSession(sessionID)
	if sessionState != nil && sessionState.timer != nil {
		sessionState.timer.Stop()
	}
}

func (s *RuntimeService) beginIncomingAcceleratedFinalization(sessionState *incomingAcceleratedSession) bool {
	sessionState.mu.Lock()
	defer sessionState.mu.Unlock()

	if sessionState.closed {
		return false
	}
	sessionState.closed = true
	if sessionState.timer != nil {
		sessionState.timer.Stop()
	}
	return true
}

func (s *RuntimeService) finalizeIncomingAcceleratedFailure(
	sessionID string,
	sessionState *incomingAcceleratedSession,
) domain.Transfer {
	sessionState.mu.Lock()
	sessionState.transferRecord.State = transfer.StateFailed
	sessionState.transferRecord.BytesTransferred = sessionState.receiver.BytesReceived()
	failedSnapshot := sessionState.transferRecord
	sessionState.mu.Unlock()

	_ = sessionState.receiver.Cleanup()
	if persistErr := s.store.PersistTransferOutcome(nil, failedSnapshot); persistErr == nil {
		s.clearTransferOverride(failedSnapshot.TransferID)
	} else {
		s.rememberTransferOverride(failedSnapshot)
	}
	s.transfers.Finish(failedSnapshot.TransferID)
	s.deleteIncomingAcceleratedSession(sessionID)
	return failedSnapshot
}

func (s *RuntimeService) expireIncomingAcceleratedSession(sessionID string) {
	sessionState, ok := s.getIncomingAcceleratedSession(sessionID)
	if !ok {
		return
	}
	if !s.beginIncomingAcceleratedFinalization(sessionState) {
		return
	}
	failedSnapshot := s.finalizeIncomingAcceleratedFailure(sessionID, sessionState)
	s.publishTransferEvent(failedSnapshot)
}

func (s *RuntimeService) takeIncomingAcceleratedSessionByTransferID(transferID string) (*incomingAcceleratedSession, bool) {
	s.acceleratedMu.Lock()
	defer s.acceleratedMu.Unlock()

	for sessionID, sessionState := range s.incomingAcceleratedSession {
		if sessionState.transferRecord.TransferID != strings.TrimSpace(transferID) {
			continue
		}
		delete(s.incomingAcceleratedSession, sessionID)
		s.unregisterIncomingAcceleratedSession(sessionID)
		if sessionState.timer != nil {
			sessionState.timer.Stop()
		}
		sessionState.mu.Lock()
		sessionState.closed = true
		sessionState.mu.Unlock()
		return sessionState, true
	}
	return nil, false
}

func (s *RuntimeService) unregisterIncomingAcceleratedSession(sessionID string) {
	if s.acceleratedSessions == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	s.acceleratedSessions.Unregister(sessionID)
}

func recommendedAcceleratedProfile(fileSize int64) (int64, int, int) {
	chunkSize, initial, max := transfer.RecommendedSessionProfile(fileSize)
	initial = normalizeAcceleratedStripes(initial)
	max = normalizeAcceleratedStripes(max)
	if max < initial {
		max = initial
	}
	return chunkSize, initial, max
}

func normalizeAcceleratedStripes(value int) int {
	switch {
	case value >= 8:
		return 8
	case value >= 4:
		return 4
	case value >= 2:
		return 2
	default:
		return 1
	}
}

func recommendedAcceleratedInFlightWindow(chunkSize int64, maxStripes int) int64 {
	if chunkSize <= 0 {
		chunkSize = 8 << 20
	}
	if maxStripes <= 0 {
		maxStripes = 1
	}
	return chunkSize * int64(maxStripes)
}

func newDefaultAcceleratedSender(peer discovery.PeerRecord) AcceleratedFileSender {
	return transfer.NewAcceleratedSender(func(ctx context.Context, _ int, prepare protocol.AcceleratedPrepareResponse) (net.Conn, error) {
		address, err := acceleratedPeerDataAddr(peer, prepare.DataPort)
		if err != nil {
			return nil, err
		}

		conn, err := (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
			_ = tcpConn.SetReadBuffer(4 << 20)
			_ = tcpConn.SetWriteBuffer(4 << 20)
		}
		return conn, nil
	}, nil)
}

func acceleratedPeerDataAddr(peer discovery.PeerRecord, dataPort int) (string, error) {
	if strings.TrimSpace(peer.LastKnownAddr) == "" {
		return "", fmt.Errorf("peer data address is empty")
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(peer.LastKnownAddr))
	if err != nil {
		return "", fmt.Errorf("parse peer data address: %w", err)
	}
	if dataPort <= 0 {
		return "", fmt.Errorf("invalid accelerated data port: %d", dataPort)
	}
	return net.JoinHostPort(host, strconv.Itoa(dataPort)), nil
}

func fileSHA256Hex(file *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, readerOnly{Reader: file}, make([]byte, transferCopyBufferSize)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
