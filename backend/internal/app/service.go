package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"message-share/backend/internal/config"
	"message-share/backend/internal/diagnostics"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/domain"
	"message-share/backend/internal/localfile"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/security"
	"message-share/backend/internal/session"
	"message-share/backend/internal/transfer"
)

const (
	defaultHeartbeatInterval              = 30 * time.Second
	defaultHeartbeatTimeout               = 4 * time.Second
	defaultHeartbeatFailureThreshold      = 3
	defaultIncomingTransferSessionTimeout = 2 * time.Minute
	multipartThreshold                    = 64 << 20
	transferCopyBufferSize                = 1 << 20
	transferEventMinInterval              = 120 * time.Millisecond
	transferEventMinBytes                 = 0
)

type PeerSnapshot struct {
	DeviceID      string `json:"deviceId"`
	DeviceName    string `json:"deviceName"`
	Trusted       bool   `json:"trusted"`
	Online        bool   `json:"online"`
	Reachable     bool   `json:"reachable"`
	AgentTCPPort  int    `json:"agentTcpPort"`
	LastKnownAddr string `json:"lastKnownAddr,omitempty"`
}

type PairingSnapshot struct {
	PairingID      string `json:"pairingId"`
	PeerDeviceID   string `json:"peerDeviceId"`
	PeerDeviceName string `json:"peerDeviceName"`
	ShortCode      string `json:"shortCode"`
	Status         string `json:"status"`
}

type ConversationSnapshot struct {
	ConversationID string `json:"conversationId"`
	PeerDeviceID   string `json:"peerDeviceId"`
	PeerDeviceName string `json:"peerDeviceName"`
}

type MessageSnapshot struct {
	MessageID      string `json:"messageId"`
	ConversationID string `json:"conversationId"`
	Direction      string `json:"direction"`
	Kind           string `json:"kind"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
}

type TransferSnapshot struct {
	TransferID       string  `json:"transferId"`
	MessageID        string  `json:"messageId"`
	FileName         string  `json:"fileName"`
	FileSize         int64   `json:"fileSize"`
	State            string  `json:"state"`
	Direction        string  `json:"direction"`
	BytesTransferred int64   `json:"bytesTransferred"`
	ProgressPercent  float64 `json:"progressPercent"`
	RateBytesPerSec  float64 `json:"rateBytesPerSec"`
	EtaSeconds       *int64  `json:"etaSeconds,omitempty"`
	Active           bool    `json:"active"`
	CreatedAt        string  `json:"createdAt"`
}

type BootstrapSnapshot struct {
	LocalDeviceName string                 `json:"localDeviceName"`
	Health          map[string]any         `json:"health"`
	Peers           []PeerSnapshot         `json:"peers"`
	Pairings        []PairingSnapshot      `json:"pairings"`
	Conversations   []ConversationSnapshot `json:"conversations"`
	Messages        []MessageSnapshot      `json:"messages"`
	Transfers       []TransferSnapshot     `json:"transfers"`
}

type Store interface {
	LoadLocalDevice() (domain.LocalDevice, bool, error)
	ListTrustedPeers() ([]domain.Peer, error)
	UpsertTrustedPeer(peer domain.Peer) error
	EnsureConversation(peerDeviceID string) (domain.Conversation, error)
	ListConversations() ([]domain.Conversation, error)
	SaveMessage(message domain.Message) error
	SaveMessageWithTransfer(message domain.Message, transfer domain.Transfer) error
	ListMessages(conversationID string) ([]domain.Message, error)
	SaveTransfer(transfer domain.Transfer) error
	ListTransfers() ([]domain.Transfer, error)
	UpdateTransferState(transferID string, state string) error
	UpdateTransferProgress(transferID string, state string, bytesTransferred int64) error
	PersistTransferOutcome(message *domain.Message, transfer domain.Transfer) error
}

type Service interface {
	Bootstrap() (BootstrapSnapshot, error)
	StartPairing(ctx context.Context, peerDeviceID string) (PairingSnapshot, error)
	ConfirmPairing(ctx context.Context, pairingID string) (PairingSnapshot, error)
	SendTextMessage(ctx context.Context, peerDeviceID string, body string) (MessageSnapshot, error)
	SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (TransferSnapshot, error)
	PickLocalFile(ctx context.Context) (LocalFileSnapshot, error)
	SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (TransferSnapshot, error)
}

type EventPublisher interface {
	Publish(kind string, payload any)
}

type EventPublisherFunc func(kind string, payload any)

func (f EventPublisherFunc) Publish(kind string, payload any) {
	f(kind, payload)
}

type PairingManager interface {
	ListPairings() []session.PairingSession
	StartPairing(draft session.PairingDraft) session.PairingSession
	MarkLocalConfirmed(pairingID string) (session.PairingSession, error)
	MarkRemoteConfirmed(pairingID string) (session.PairingSession, error)
	GetPairing(pairingID string) (session.PairingSession, bool)
}

type PeerTransport interface {
	StartPairing(ctx context.Context, peer discovery.PeerRecord, request protocol.PairingStartRequest) (protocol.PairingStartResponse, error)
	ConfirmPairing(ctx context.Context, peer discovery.PeerRecord, request protocol.PairingConfirmRequest) (protocol.PairingConfirmResponse, error)
	SendHeartbeat(ctx context.Context, peer discovery.PeerRecord, request protocol.HeartbeatRequest) (protocol.HeartbeatResponse, error)
	SendTextMessage(ctx context.Context, peer discovery.PeerRecord, request protocol.TextMessageRequest) (protocol.AckResponse, error)
	SendFile(ctx context.Context, peer discovery.PeerRecord, request protocol.FileTransferRequest, content io.Reader) (protocol.FileTransferResponse, error)
}

type RuntimeDeps struct {
	Config                   config.AppConfig
	Store                    Store
	Discovery                *discovery.Registry
	Pairings                 PairingManager
	Events                   EventPublisher
	Transport                PeerTransport
	Transfers                *transfer.Registry
	LocalFiles               LocalFileResolver
	AcceleratedSessions      AcceleratedSessionRegistrar
	AcceleratedSenderFactory AcceleratedSenderFactory

	HeartbeatInterval              time.Duration
	HeartbeatTimeout               time.Duration
	HeartbeatFailureThreshold      int
	IncomingTransferSessionTimeout time.Duration
}

type RuntimeService struct {
	cfg                      config.AppConfig
	store                    Store
	discovery                *discovery.Registry
	pairings                 PairingManager
	events                   EventPublisher
	transport                PeerTransport
	transfers                *transfer.Registry
	localFiles               LocalFileResolver
	acceleratedSessions      AcceleratedSessionRegistrar
	acceleratedSenderFactory AcceleratedSenderFactory

	heartbeatInterval              time.Duration
	heartbeatTimeout               time.Duration
	heartbeatFailureThreshold      int
	incomingTransferSessionTimeout time.Duration
	heartbeatMu                    sync.Mutex
	heartbeatFailures              map[string]int

	overrideMu sync.RWMutex
	overrides  map[string]domain.Transfer

	sessionMu                  sync.RWMutex
	incomingTransferSession    map[string]*incomingTransferSession
	acceleratedMu              sync.RWMutex
	incomingAcceleratedSession map[string]*incomingAcceleratedSession
}

type incomingTransferSession struct {
	mu                    sync.Mutex
	senderDeviceID        string
	agentTCPPort          int
	adaptivePolicyVersion string
	transferRecord        domain.Transfer
	receiver              *transfer.SessionReceiver
	telemetry             *transfer.Telemetry
	progressGate          *transfer.ProgressEventGate
	progressWarm          bool
	idleTimeout           time.Duration
	idleTimer             *time.Timer
	activeParts           int
	closed                bool
}

func (s *incomingTransferSession) resetIdleTimerLocked() {
	if s.idleTimer != nil && s.idleTimeout > 0 {
		s.idleTimer.Reset(s.idleTimeout)
	}
}

func NewRuntimeService(deps RuntimeDeps) *RuntimeService {
	if deps.Discovery == nil {
		deps.Discovery = discovery.NewRegistry()
	}
	if deps.Pairings == nil {
		deps.Pairings = session.NewService()
	}
	if deps.Transfers == nil {
		deps.Transfers = transfer.NewRegistry()
	}
	if deps.HeartbeatInterval <= 0 {
		deps.HeartbeatInterval = defaultHeartbeatInterval
	}
	if deps.HeartbeatTimeout <= 0 {
		deps.HeartbeatTimeout = defaultHeartbeatTimeout
	}
	if deps.HeartbeatFailureThreshold <= 0 {
		deps.HeartbeatFailureThreshold = defaultHeartbeatFailureThreshold
	}
	if deps.IncomingTransferSessionTimeout <= 0 {
		deps.IncomingTransferSessionTimeout = defaultIncomingTransferSessionTimeout
	}
	if deps.AcceleratedSenderFactory == nil {
		deps.AcceleratedSenderFactory = newDefaultAcceleratedSender
	}

	return &RuntimeService{
		cfg:                            deps.Config,
		store:                          deps.Store,
		discovery:                      deps.Discovery,
		pairings:                       deps.Pairings,
		events:                         deps.Events,
		transport:                      deps.Transport,
		transfers:                      deps.Transfers,
		localFiles:                     deps.LocalFiles,
		acceleratedSessions:            deps.AcceleratedSessions,
		acceleratedSenderFactory:       deps.AcceleratedSenderFactory,
		heartbeatInterval:              deps.HeartbeatInterval,
		heartbeatTimeout:               deps.HeartbeatTimeout,
		heartbeatFailureThreshold:      deps.HeartbeatFailureThreshold,
		incomingTransferSessionTimeout: deps.IncomingTransferSessionTimeout,
		heartbeatFailures:              make(map[string]int),
		overrides:                      make(map[string]domain.Transfer),
		incomingTransferSession:        make(map[string]*incomingTransferSession),
		incomingAcceleratedSession:     make(map[string]*incomingAcceleratedSession),
	}
}

func (s *RuntimeService) ResolveLocalFile(localFileID string) (localfile.Lease, error) {
	if s.localFiles == nil {
		return localfile.Lease{}, fmt.Errorf("local file picker not configured")
	}
	return s.localFiles.Resolve(localFileID)
}

func (s *RuntimeService) Bootstrap() (BootstrapSnapshot, error) {
	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return BootstrapSnapshot{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return BootstrapSnapshot{}, fmt.Errorf("local device not initialized")
	}

	trustedPeers, err := s.store.ListTrustedPeers()
	if err != nil {
		return BootstrapSnapshot{}, fmt.Errorf("list trusted peers: %w", err)
	}
	conversations, err := s.store.ListConversations()
	if err != nil {
		return BootstrapSnapshot{}, fmt.Errorf("list conversations: %w", err)
	}
	transfers, err := s.store.ListTransfers()
	if err != nil {
		return BootstrapSnapshot{}, fmt.Errorf("list transfers: %w", err)
	}

	peerSnapshots := mergePeerSnapshots(trustedPeers, s.discovery.List())
	conversationSnapshots := mapConversationSnapshots(conversations, peerSnapshots)
	messageSnapshots, err := s.mapMessageSnapshots(conversations)
	if err != nil {
		return BootstrapSnapshot{}, fmt.Errorf("list messages: %w", err)
	}
	discoveryStatus := "broadcast-pending"
	for _, discoveredPeer := range s.discovery.List() {
		if discoveredPeer.Online || discoveredPeer.Reachable {
			discoveryStatus = "broadcast-ok"
			break
		}
	}

	return BootstrapSnapshot{
		LocalDeviceName: localDevice.DeviceName,
		Health:          diagnostics.BuildHealthSnapshot(true, s.cfg.AgentTCPPort, discoveryStatus),
		Peers:           peerSnapshots,
		Pairings:        mapPairingSnapshots(s.pairings.ListPairings()),
		Conversations:   conversationSnapshots,
		Messages:        messageSnapshots,
		Transfers:       s.mapTransferSnapshots(transfers),
	}, nil
}

func (s *RuntimeService) StartPairing(ctx context.Context, peerDeviceID string) (PairingSnapshot, error) {
	if s.transport == nil {
		return PairingSnapshot{}, fmt.Errorf("peer transport not configured")
	}

	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return PairingSnapshot{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return PairingSnapshot{}, fmt.Errorf("local device not initialized")
	}

	peer, ok := findDiscoveryPeer(s.discovery.List(), peerDeviceID)
	if !ok {
		return PairingSnapshot{}, fmt.Errorf("peer %s not found", peerDeviceID)
	}

	pairingID := newRandomID("pair")
	initiatorNonce := newRandomID("nonce")
	request := protocol.PairingStartRequest{
		PairingID:            pairingID,
		InitiatorDeviceID:    localDevice.DeviceID,
		InitiatorDeviceName:  localDevice.DeviceName,
		InitiatorFingerprint: security.BuildPinnedPeer(localDevice.DeviceID, localDevice.PublicKeyPEM).Fingerprint,
		InitiatorNonce:       initiatorNonce,
		AgentTCPPort:         s.cfg.AgentTCPPort,
	}

	response, err := s.transport.StartPairing(ctx, peer, request)
	if err != nil {
		s.updatePeerReachability(peer.DeviceID, false)
		return PairingSnapshot{}, fmt.Errorf("start remote pairing: %w", err)
	}
	s.updatePeerReachability(peer.DeviceID, true)

	pairing := s.pairings.StartPairing(session.PairingDraft{
		PairingID:         response.PairingID,
		PeerDeviceID:      response.ResponderDeviceID,
		PeerDeviceName:    response.ResponderDeviceName,
		InitiatorNonce:    initiatorNonce,
		ResponderNonce:    response.ResponderNonce,
		RemoteFingerprint: response.ResponderFingerprint,
		Initiator:         true,
	})

	s.publishPairingEvent(pairing)
	s.publishPeerEvent(pairing.PeerDeviceID)
	return toPairingSnapshot(pairing), nil
}

func (s *RuntimeService) ConfirmPairing(ctx context.Context, pairingID string) (PairingSnapshot, error) {
	if s.transport == nil {
		return PairingSnapshot{}, fmt.Errorf("peer transport not configured")
	}

	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return PairingSnapshot{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return PairingSnapshot{}, fmt.Errorf("local device not initialized")
	}

	pairing, ok := s.pairings.GetPairing(pairingID)
	if !ok {
		return PairingSnapshot{}, session.ErrPairingNotFound
	}

	updated, err := s.pairings.MarkLocalConfirmed(pairingID)
	if err != nil {
		return PairingSnapshot{}, err
	}

	peer, ok := findDiscoveryPeer(s.discovery.List(), pairing.PeerDeviceID)
	if !ok {
		return PairingSnapshot{}, fmt.Errorf("peer %s not found", pairing.PeerDeviceID)
	}
	peer.PinnedFingerprint = updated.RemoteFingerprint

	response, err := s.transport.ConfirmPairing(ctx, peer, protocol.PairingConfirmRequest{
		PairingID:            pairingID,
		ConfirmerDeviceID:    localDevice.DeviceID,
		ConfirmerFingerprint: security.BuildPinnedPeer(localDevice.DeviceID, localDevice.PublicKeyPEM).Fingerprint,
		Confirmed:            true,
		AgentTCPPort:         s.cfg.AgentTCPPort,
	})
	if err != nil {
		s.updatePeerReachability(peer.DeviceID, false)
		return PairingSnapshot{}, fmt.Errorf("confirm remote pairing: %w", err)
	}
	s.updatePeerReachability(peer.DeviceID, true)

	if response.RemoteConfirmed || response.Status == string(session.PairingStatusConfirmed) {
		updated, err = s.pairings.MarkRemoteConfirmed(pairingID)
		if err != nil {
			return PairingSnapshot{}, err
		}
	}

	if updated.Status == session.PairingStatusConfirmed {
		if err := s.store.UpsertTrustedPeer(domain.Peer{
			DeviceID:          updated.PeerDeviceID,
			DeviceName:        updated.PeerDeviceName,
			PinnedFingerprint: updated.RemoteFingerprint,
			Trusted:           true,
			UpdatedAt:         time.Now().UTC(),
		}); err != nil {
			return PairingSnapshot{}, fmt.Errorf("persist trusted peer: %w", err)
		}
	}

	s.publishPairingEvent(updated)
	s.publishPeerEvent(updated.PeerDeviceID)
	return toPairingSnapshot(updated), nil
}

func (s *RuntimeService) SendTextMessage(ctx context.Context, peerDeviceID string, body string) (MessageSnapshot, error) {
	if s.transport == nil {
		return MessageSnapshot{}, fmt.Errorf("peer transport not configured")
	}

	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return MessageSnapshot{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return MessageSnapshot{}, fmt.Errorf("local device not initialized")
	}
	trustedPeer, ok, err := s.trustedPeer(peerDeviceID)
	if err != nil {
		return MessageSnapshot{}, fmt.Errorf("list trusted peers: %w", err)
	}
	if !ok {
		return MessageSnapshot{}, fmt.Errorf("peer %s is not trusted", peerDeviceID)
	}

	peer, ok := findDiscoveryPeer(s.discovery.List(), peerDeviceID)
	if !ok {
		return MessageSnapshot{}, fmt.Errorf("peer %s not found", peerDeviceID)
	}
	peer.PinnedFingerprint = trustedPeer.PinnedFingerprint

	conversation, err := s.store.EnsureConversation(peerDeviceID)
	if err != nil {
		return MessageSnapshot{}, fmt.Errorf("ensure conversation: %w", err)
	}

	message := session.NewService().NewTextMessage(conversation.ConversationID, body)
	message.Direction = "outgoing"

	ack, err := s.transport.SendTextMessage(ctx, peer, protocol.TextMessageRequest{
		MessageID:        message.MessageID,
		ConversationID:   message.ConversationID,
		SenderDeviceID:   localDevice.DeviceID,
		Body:             body,
		CreatedAtRFC3339: message.CreatedAt.Format(time.RFC3339Nano),
		AgentTCPPort:     s.cfg.AgentTCPPort,
	})
	if err != nil {
		s.updatePeerReachability(peer.DeviceID, false)
		return MessageSnapshot{}, fmt.Errorf("send text message: %w", err)
	}
	s.updatePeerReachability(peer.DeviceID, true)
	if ack.Status == "accepted" {
		message.Status = "sent"
	}

	if err := s.store.SaveMessage(message); err != nil {
		return MessageSnapshot{}, fmt.Errorf("save outgoing message: %w", err)
	}

	s.publishMessageEvent(message)
	return toMessageSnapshot(message), nil
}

func (s *RuntimeService) SendFile(
	ctx context.Context,
	peerDeviceID string,
	fileName string,
	fileSize int64,
	content io.Reader,
) (TransferSnapshot, error) {
	if s.transport == nil {
		return TransferSnapshot{}, fmt.Errorf("peer transport not configured")
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

	message := session.NewService().NewTextMessage(conversation.ConversationID, filepath.Base(fileName))
	message.Direction = "outgoing"
	message.Kind = "file"

	transferRecord := domain.Transfer{
		TransferID:       newRandomID("transfer"),
		MessageID:        message.MessageID,
		FileName:         filepath.Base(fileName),
		FileSize:         fileSize,
		State:            transfer.StateSending,
		Direction:        "outgoing",
		BytesTransferred: 0,
		CreatedAt:        message.CreatedAt,
	}

	if err := s.store.SaveMessageWithTransfer(message, transferRecord); err != nil {
		return TransferSnapshot{}, fmt.Errorf("save outgoing file payload: %w", err)
	}

	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)

	telemetry := s.transfers.Start(
		transferRecord.TransferID,
		transferRecord.FileSize,
		transferRecord.Direction,
		time.Now().UTC(),
	)
	progressGate := transfer.NewProgressEventGate(transferEventMinInterval, transferEventMinBytes)
	progressMu := sync.Mutex{}
	progressWarm := false
	advanceProgress := func(delta int64) {
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
	}

	var (
		finalState string
		sendErr    error
	)
	if sessionTransport, ok := s.transport.(transfer.SessionTransport); ok && fileSize >= multipartThreshold {
		var completeResponse protocol.TransferSessionCompleteResponse
		completeResponse, sendErr = transfer.NewSessionSender(
			sessionTransport,
			peer,
			nil,
			advanceProgress,
		).Send(ctx, content, transfer.SessionMeta{
			TransferID:     transferRecord.TransferID,
			MessageID:      message.MessageID,
			SenderDeviceID: localDevice.DeviceID,
			FileName:       transferRecord.FileName,
			FileSize:       fileSize,
			AgentTCPPort:   s.cfg.AgentTCPPort,
		})
		if completeResponse.State != "" {
			finalState = completeResponse.State
		}
	} else {
		progressReader := transfer.NewProgressReader(content, advanceProgress)
		var response protocol.FileTransferResponse
		response, sendErr = s.transport.SendFile(ctx, peer, protocol.FileTransferRequest{
			TransferID:       transferRecord.TransferID,
			MessageID:        message.MessageID,
			SenderDeviceID:   localDevice.DeviceID,
			FileName:         transferRecord.FileName,
			FileSize:         fileSize,
			CreatedAtRFC3339: message.CreatedAt.Format(time.RFC3339Nano),
			AgentTCPPort:     s.cfg.AgentTCPPort,
		}, progressReader)
		if response.State != "" {
			finalState = response.State
		}
	}
	if sendErr != nil {
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
		return TransferSnapshot{}, fmt.Errorf("send file: %w", sendErr)
	}
	s.updatePeerReachability(peer.DeviceID, true)
	if progressGate.Finish(time.Now().UTC()) {
		s.publishTransferEvent(transferRecord)
	}

	if finalState != "" {
		transferRecord.State = finalState
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

func (s *RuntimeService) AcceptIncomingPairing(
	ctx context.Context,
	request protocol.PairingStartRequest,
) (protocol.PairingStartResponse, error) {
	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return protocol.PairingStartResponse{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return protocol.PairingStartResponse{}, fmt.Errorf("local device not initialized")
	}

	responderNonce := newRandomID("nonce")
	pairing := s.pairings.StartPairing(session.PairingDraft{
		PairingID:         request.PairingID,
		PeerDeviceID:      request.InitiatorDeviceID,
		PeerDeviceName:    request.InitiatorDeviceName,
		InitiatorNonce:    request.InitiatorNonce,
		ResponderNonce:    responderNonce,
		RemoteFingerprint: request.InitiatorFingerprint,
		Initiator:         false,
	})

	s.markPeerDirectActive(ctx, pairing.PeerDeviceID, request.AgentTCPPort)
	s.publishPairingEvent(pairing)

	return protocol.PairingStartResponse{
		PairingID:            pairing.PairingID,
		ResponderDeviceID:    localDevice.DeviceID,
		ResponderDeviceName:  localDevice.DeviceName,
		ResponderFingerprint: security.BuildPinnedPeer(localDevice.DeviceID, localDevice.PublicKeyPEM).Fingerprint,
		ResponderNonce:       responderNonce,
	}, nil
}

func (s *RuntimeService) AuthorizePairingStart(
	_ context.Context,
	request protocol.PairingStartRequest,
	caller protocol.PeerCaller,
) error {
	return authorizeCallerFingerprint(request.InitiatorFingerprint, caller.Fingerprint, "initiator")
}

func (s *RuntimeService) AuthorizePairingConfirm(
	_ context.Context,
	request protocol.PairingConfirmRequest,
	caller protocol.PeerCaller,
) error {
	if err := authorizeCallerFingerprint(request.ConfirmerFingerprint, caller.Fingerprint, "confirmer"); err != nil {
		return err
	}

	pairing, ok := s.pairings.GetPairing(request.PairingID)
	if !ok {
		return nil
	}
	if strings.TrimSpace(request.ConfirmerDeviceID) != pairing.PeerDeviceID {
		return fmt.Errorf("%w: confirmer device mismatch", protocol.ErrPeerForbidden)
	}
	if pairing.RemoteFingerprint == "" {
		return fmt.Errorf("%w: pairing fingerprint missing", protocol.ErrPeerForbidden)
	}
	if caller.Fingerprint != pairing.RemoteFingerprint {
		return fmt.Errorf("%w: confirmer fingerprint does not match pairing", protocol.ErrPeerForbidden)
	}
	return nil
}

func (s *RuntimeService) AcceptPairingConfirm(
	ctx context.Context,
	request protocol.PairingConfirmRequest,
) (protocol.PairingConfirmResponse, error) {
	if !request.Confirmed {
		return protocol.PairingConfirmResponse{
			PairingID:       request.PairingID,
			Status:          string(session.PairingStatusRejected),
			RemoteConfirmed: false,
		}, nil
	}

	updated, err := s.pairings.MarkRemoteConfirmed(request.PairingID)
	if err != nil {
		return protocol.PairingConfirmResponse{}, err
	}

	if updated.Status == session.PairingStatusConfirmed {
		if err := s.store.UpsertTrustedPeer(domain.Peer{
			DeviceID:          updated.PeerDeviceID,
			DeviceName:        updated.PeerDeviceName,
			PinnedFingerprint: updated.RemoteFingerprint,
			Trusted:           true,
			UpdatedAt:         time.Now().UTC(),
		}); err != nil {
			return protocol.PairingConfirmResponse{}, fmt.Errorf("persist trusted peer: %w", err)
		}
	}

	s.markPeerDirectActive(ctx, updated.PeerDeviceID, request.AgentTCPPort)
	s.publishPairingEvent(updated)

	return protocol.PairingConfirmResponse{
		PairingID:       updated.PairingID,
		Status:          string(updated.Status),
		RemoteConfirmed: updated.RemoteConfirmed,
	}, nil
}

func (s *RuntimeService) AcceptHeartbeat(
	ctx context.Context,
	request protocol.HeartbeatRequest,
) (protocol.HeartbeatResponse, error) {
	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return protocol.HeartbeatResponse{}, fmt.Errorf("load local device: %w", err)
	}
	if !ok {
		return protocol.HeartbeatResponse{}, fmt.Errorf("local device not initialized")
	}

	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
	return protocol.HeartbeatResponse{
		ResponderDeviceID:   localDevice.DeviceID,
		ResponderDeviceName: localDevice.DeviceName,
		AgentTCPPort:        s.cfg.AgentTCPPort,
		ReceivedAtRFC3339:   time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *RuntimeService) AuthorizeHeartbeat(
	_ context.Context,
	request protocol.HeartbeatRequest,
	caller protocol.PeerCaller,
) error {
	return s.authorizeTrustedPeerRequest(request.SenderDeviceID, caller)
}

func (s *RuntimeService) AcceptIncomingTextMessage(
	ctx context.Context,
	request protocol.TextMessageRequest,
) (protocol.AckResponse, error) {
	if !s.isTrustedPeer(request.SenderDeviceID) {
		return protocol.AckResponse{}, fmt.Errorf("peer %s is not trusted", request.SenderDeviceID)
	}

	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)

	conversation, err := s.store.EnsureConversation(request.SenderDeviceID)
	if err != nil {
		return protocol.AckResponse{}, fmt.Errorf("ensure conversation: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, request.CreatedAtRFC3339)
	if err != nil {
		return protocol.AckResponse{}, fmt.Errorf("parse created time: %w", err)
	}

	message := domain.Message{
		MessageID:      request.MessageID,
		ConversationID: conversation.ConversationID,
		Direction:      "incoming",
		Kind:           "text",
		Body:           request.Body,
		Status:         "sent",
		CreatedAt:      createdAt.UTC(),
	}
	if err := s.store.SaveMessage(message); err != nil {
		return protocol.AckResponse{}, fmt.Errorf("save incoming message: %w", err)
	}

	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
	s.publishMessageEvent(message)
	return protocol.AckResponse{
		RequestID: request.MessageID,
		Status:    "accepted",
	}, nil
}

func (s *RuntimeService) AuthorizeTextMessage(
	_ context.Context,
	request protocol.TextMessageRequest,
	caller protocol.PeerCaller,
) error {
	return s.authorizeTrustedPeerRequest(request.SenderDeviceID, caller)
}

func (s *RuntimeService) AcceptIncomingFileTransfer(
	ctx context.Context,
	request protocol.FileTransferRequest,
	content io.Reader,
) (protocol.FileTransferResponse, error) {
	if !s.isTrustedPeer(request.SenderDeviceID) {
		return protocol.FileTransferResponse{}, fmt.Errorf("peer %s is not trusted", request.SenderDeviceID)
	}

	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)

	createdAt := time.Now().UTC()
	if request.CreatedAtRFC3339 != "" {
		parsed, err := time.Parse(time.RFC3339Nano, request.CreatedAtRFC3339)
		if err != nil {
			return protocol.FileTransferResponse{}, fmt.Errorf("parse created time: %w", err)
		}
		createdAt = parsed.UTC()
	}

	fileWriter, err := transfer.NewFileWriter(s.cfg.DefaultDownloadDir, request.FileName)
	if err != nil {
		return protocol.FileTransferResponse{}, fmt.Errorf("create file writer: %w", err)
	}
	defer fileWriter.Cleanup()

	messageID := request.MessageID
	if messageID == "" {
		messageID = newRandomID("msg")
	}
	transferID := request.TransferID
	if transferID == "" {
		transferID = newRandomID("transfer")
	}

	reusedAcceleratedSession, reusedAccelerated := s.takeIncomingAcceleratedSessionByTransferID(transferID)
	if reusedAccelerated {
		_ = reusedAcceleratedSession.receiver.Cleanup()
		s.transfers.Finish(reusedAcceleratedSession.transferRecord.TransferID)
	}

	transferRecord := domain.Transfer{}
	if reusedAccelerated {
		transferRecord = reusedAcceleratedSession.transferRecord
		transferRecord.State = transfer.StateReceiving
		transferRecord.FileName = filepath.Base(request.FileName)
		transferRecord.FileSize = request.FileSize
		transferRecord.BytesTransferred = 0
		transferRecord.CreatedAt = createdAt
		messageID = transferRecord.MessageID
		transferID = transferRecord.TransferID
	} else {
		conversation, err := s.store.EnsureConversation(request.SenderDeviceID)
		if err != nil {
			return protocol.FileTransferResponse{}, fmt.Errorf("ensure conversation: %w", err)
		}

		message := domain.Message{
			MessageID:      messageID,
			ConversationID: conversation.ConversationID,
			Direction:      "incoming",
			Kind:           "file",
			Body:           filepath.Base(request.FileName),
			Status:         "sent",
			CreatedAt:      createdAt,
		}
		transferRecord = domain.Transfer{
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
			return protocol.FileTransferResponse{}, fmt.Errorf("save incoming file payload: %w", err)
		}

		s.publishMessageEvent(message)
		s.publishTransferEvent(transferRecord)
	}

	telemetry := s.transfers.Start(
		transferRecord.TransferID,
		transferRecord.FileSize,
		transferRecord.Direction,
		time.Now().UTC(),
	)
	progressGate := transfer.NewProgressEventGate(transferEventMinInterval, transferEventMinBytes)
	progressWarm := false
	progressWriter := transfer.NewProgressWriter(fileWriter, func(delta int64) {
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

	written, err := io.CopyBuffer(progressWriter, readerOnly{Reader: content}, make([]byte, transferCopyBufferSize))
	if err != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		transferRecord.State = transfer.StateFailed
		transferRecord.BytesTransferred = written
		if persistErr := s.store.PersistTransferOutcome(nil, transferRecord); persistErr == nil {
			s.clearTransferOverride(transferRecord.TransferID)
		} else {
			s.rememberTransferOverride(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
		s.publishTransferEvent(transferRecord)
		return protocol.FileTransferResponse{}, fmt.Errorf("write incoming file: %w", err)
	}
	if written != request.FileSize {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		transferRecord.State = transfer.StateFailed
		transferRecord.BytesTransferred = written
		if persistErr := s.store.PersistTransferOutcome(nil, transferRecord); persistErr == nil {
			s.clearTransferOverride(transferRecord.TransferID)
		} else {
			s.rememberTransferOverride(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
		s.publishTransferEvent(transferRecord)
		return protocol.FileTransferResponse{}, fmt.Errorf("incoming file size mismatch: declared=%d actual=%d", request.FileSize, written)
	}

	if _, err := fileWriter.Commit(); err != nil {
		if progressGate.Finish(time.Now().UTC()) {
			s.publishTransferEvent(transferRecord)
		}
		transferRecord.State = transfer.StateFailed
		transferRecord.BytesTransferred = written
		if persistErr := s.store.PersistTransferOutcome(nil, transferRecord); persistErr == nil {
			s.clearTransferOverride(transferRecord.TransferID)
		} else {
			s.rememberTransferOverride(transferRecord)
		}
		s.transfers.Finish(transferRecord.TransferID)
		s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
		s.publishTransferEvent(transferRecord)
		return protocol.FileTransferResponse{}, fmt.Errorf("commit incoming file: %w", err)
	}

	if progressGate.Finish(time.Now().UTC()) {
		s.publishTransferEvent(transferRecord)
	}
	transferRecord.State = transfer.StateDone
	transferRecord.FileSize = written
	transferRecord.BytesTransferred = written
	if err := s.store.PersistTransferOutcome(nil, transferRecord); err != nil {
		s.rememberTransferOverride(transferRecord)
		s.transfers.Finish(transferRecord.TransferID)
		s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
		s.publishTransferEvent(transferRecord)
		return protocol.FileTransferResponse{
			TransferID: transferID,
			State:      transferRecord.State,
		}, nil
	}

	s.clearTransferOverride(transferRecord.TransferID)
	s.transfers.Finish(transferRecord.TransferID)
	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)
	s.publishTransferEvent(transferRecord)
	return protocol.FileTransferResponse{
		TransferID: transferID,
		State:      transferRecord.State,
	}, nil
}

func (s *RuntimeService) StartIncomingTransferSession(
	ctx context.Context,
	request protocol.TransferSessionStartRequest,
) (protocol.TransferSessionStartResponse, error) {
	if strings.TrimSpace(request.SenderDeviceID) == "" {
		return protocol.TransferSessionStartResponse{}, fmt.Errorf("senderDeviceID is required")
	}

	s.markPeerDirectActive(ctx, request.SenderDeviceID, request.AgentTCPPort)

	if request.FileSize <= 0 {
		return protocol.TransferSessionStartResponse{}, fmt.Errorf("invalid file size: %d", request.FileSize)
	}

	conversation, err := s.store.EnsureConversation(request.SenderDeviceID)
	if err != nil {
		return protocol.TransferSessionStartResponse{}, fmt.Errorf("ensure conversation: %w", err)
	}

	receiver, err := transfer.NewSessionReceiver(s.cfg.DefaultDownloadDir, request.FileName, request.FileSize)
	if err != nil {
		return protocol.TransferSessionStartResponse{}, fmt.Errorf("create session receiver: %w", err)
	}

	messageID := request.MessageID
	if strings.TrimSpace(messageID) == "" {
		messageID = newRandomID("msg")
	}
	transferID := request.TransferID
	if strings.TrimSpace(transferID) == "" {
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
		return protocol.TransferSessionStartResponse{}, fmt.Errorf("save incoming session payload: %w", err)
	}

	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)

	sessionID := newRandomID("session")
	sessionState := &incomingTransferSession{
		senderDeviceID:        request.SenderDeviceID,
		agentTCPPort:          request.AgentTCPPort,
		adaptivePolicyVersion: transfer.SessionAdaptivePolicyVersion,
		transferRecord:        transferRecord,
		receiver:              receiver,
		telemetry: s.transfers.Start(
			transferRecord.TransferID,
			transferRecord.FileSize,
			transferRecord.Direction,
			time.Now().UTC(),
		),
		progressGate: transfer.NewProgressEventGate(transferEventMinInterval, transferEventMinBytes),
		idleTimeout:  s.incomingTransferSessionTimeout,
	}
	sessionState.idleTimer = time.AfterFunc(sessionState.idleTimeout, func() {
		s.expireIncomingTransferSession(sessionID)
	})
	s.sessionMu.Lock()
	s.incomingTransferSession[sessionID] = sessionState
	s.sessionMu.Unlock()

	chunkSize, initialParallelism, maxParallelism := transfer.RecommendedSessionProfile(request.FileSize)
	return protocol.TransferSessionStartResponse{
		SessionID:             sessionID,
		ChunkSize:             chunkSize,
		InitialParallelism:    initialParallelism,
		MaxParallelism:        maxParallelism,
		AdaptivePolicyVersion: transfer.SessionAdaptivePolicyVersion,
	}, nil
}

func (s *RuntimeService) AcceptIncomingTransferPart(
	ctx context.Context,
	request protocol.TransferPartRequest,
	content io.Reader,
) (protocol.TransferPartResponse, error) {
	sessionState, ok := s.getIncomingTransferSession(request.SessionID)
	if !ok {
		return protocol.TransferPartResponse{}, fmt.Errorf("incoming transfer session not found")
	}
	if strings.TrimSpace(request.TransferID) != "" && request.TransferID != sessionState.transferRecord.TransferID {
		return protocol.TransferPartResponse{}, fmt.Errorf("incoming transfer session transfer mismatch")
	}
	sessionState.mu.Lock()
	if sessionState.closed {
		sessionState.mu.Unlock()
		return protocol.TransferPartResponse{}, fmt.Errorf("incoming transfer session already closed")
	}
	sessionState.activeParts++
	sessionState.resetIdleTimerLocked()
	sessionState.mu.Unlock()

	written, err := sessionState.receiver.WritePart(request.PartIndex, request.Offset, request.Length, content)
	if err != nil {
		bytesReceived := int64(0)
		sessionState.mu.Lock()
		if sessionState.activeParts > 0 {
			sessionState.activeParts--
		}
		bytesReceived = sessionState.receiver.BytesReceived()
		sessionState.resetIdleTimerLocked()
		sessionState.mu.Unlock()
		if errors.Is(err, transfer.ErrPartAlreadyCompleted) {
			return protocol.TransferPartResponse{
				SessionID:     request.SessionID,
				PartIndex:     request.PartIndex,
				BytesWritten:  written,
				BytesReceived: bytesReceived,
			}, nil
		}
		if errors.Is(err, transfer.ErrPartAlreadyInProgress) {
			return protocol.TransferPartResponse{}, fmt.Errorf("write transfer part: %w", err)
		}
		if s.beginIncomingTransferSessionFinalization(sessionState) {
			failedSnapshot := s.finalizeIncomingTransferSessionFailure(request.SessionID, sessionState)
			s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
			s.publishTransferEvent(failedSnapshot)
		}
		return protocol.TransferPartResponse{}, fmt.Errorf("write transfer part: %w", err)
	}

	now := time.Now().UTC()
	sessionState.mu.Lock()
	if sessionState.activeParts > 0 {
		sessionState.activeParts--
	}
	sessionState.telemetry.Advance(written, now)
	sessionState.transferRecord.BytesTransferred = sessionState.receiver.BytesReceived()
	sessionState.resetIdleTimerLocked()
	snapshot := sessionState.transferRecord
	shouldPublish := shouldPublishThrottledTransferProgress(
		sessionState.progressGate,
		&sessionState.progressWarm,
		snapshot,
		sessionState.telemetry.Snapshot(now),
		written,
		now,
	)
	sessionState.mu.Unlock()

	if shouldPublish {
		s.publishTransferEvent(snapshot)
	}

	return protocol.TransferPartResponse{
		SessionID:     request.SessionID,
		PartIndex:     request.PartIndex,
		BytesWritten:  written,
		BytesReceived: snapshot.BytesTransferred,
	}, nil
}

func (s *RuntimeService) CompleteIncomingTransferSession(
	ctx context.Context,
	request protocol.TransferSessionCompleteRequest,
) (protocol.TransferSessionCompleteResponse, error) {
	sessionState, ok := s.getIncomingTransferSession(request.SessionID)
	if !ok {
		return protocol.TransferSessionCompleteResponse{}, fmt.Errorf("incoming transfer session not found")
	}
	if strings.TrimSpace(request.TransferID) != "" && request.TransferID != sessionState.transferRecord.TransferID {
		return protocol.TransferSessionCompleteResponse{}, fmt.Errorf("incoming transfer session transfer mismatch")
	}
	if !s.beginIncomingTransferSessionFinalization(sessionState) {
		return protocol.TransferSessionCompleteResponse{}, fmt.Errorf("incoming transfer session already closed")
	}

	if _, err := sessionState.receiver.Complete(request.PartCount, request.FileSHA256); err != nil {
		failedSnapshot := s.finalizeIncomingTransferSessionFailure(request.SessionID, sessionState)
		s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
		s.publishTransferEvent(failedSnapshot)
		return protocol.TransferSessionCompleteResponse{}, fmt.Errorf("complete incoming transfer session: %w", err)
	}

	sessionState.mu.Lock()
	sessionState.transferRecord.State = transfer.StateDone
	sessionState.transferRecord.FileSize = sessionState.receiver.BytesReceived()
	sessionState.transferRecord.BytesTransferred = sessionState.receiver.BytesReceived()
	completedSnapshot := sessionState.transferRecord
	sessionState.mu.Unlock()

	if err := s.store.PersistTransferOutcome(nil, completedSnapshot); err != nil {
		s.rememberTransferOverride(completedSnapshot)
		s.transfers.Finish(completedSnapshot.TransferID)
		s.deleteIncomingTransferSession(request.SessionID)
		s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
		s.publishTransferEvent(completedSnapshot)
		return protocol.TransferSessionCompleteResponse{
			TransferID: completedSnapshot.TransferID,
			State:      completedSnapshot.State,
		}, nil
	}

	s.clearTransferOverride(completedSnapshot.TransferID)
	s.transfers.Finish(completedSnapshot.TransferID)
	s.deleteIncomingTransferSession(request.SessionID)
	s.markPeerDirectActive(ctx, sessionState.senderDeviceID, sessionState.agentTCPPort)
	s.publishTransferEvent(completedSnapshot)
	return protocol.TransferSessionCompleteResponse{
		TransferID: completedSnapshot.TransferID,
		State:      completedSnapshot.State,
	}, nil
}

func (s *RuntimeService) AuthorizeFileTransfer(
	_ context.Context,
	request protocol.FileTransferRequest,
	caller protocol.PeerCaller,
) error {
	return s.authorizeTrustedPeerRequest(request.SenderDeviceID, caller)
}

func (s *RuntimeService) AuthorizeTransferSessionStart(
	_ context.Context,
	request protocol.TransferSessionStartRequest,
	caller protocol.PeerCaller,
) error {
	return s.authorizeTrustedPeerRequest(request.SenderDeviceID, caller)
}

func (s *RuntimeService) AuthorizeTransferPart(
	_ context.Context,
	request protocol.TransferPartRequest,
	caller protocol.PeerCaller,
) error {
	sessionState, ok := s.getIncomingTransferSession(request.SessionID)
	if !ok {
		return fmt.Errorf("%w: transfer session not found", protocol.ErrPeerForbidden)
	}
	if request.RawBody && !transfer.SessionPolicySupportsFastPath(sessionState.adaptivePolicyVersion) {
		return fmt.Errorf("%w: raw transfer part not allowed for session", protocol.ErrPeerForbidden)
	}
	return s.authorizeTrustedPeerRequest(sessionState.senderDeviceID, caller)
}

func (s *RuntimeService) AuthorizeTransferSessionComplete(
	_ context.Context,
	request protocol.TransferSessionCompleteRequest,
	caller protocol.PeerCaller,
) error {
	sessionState, ok := s.getIncomingTransferSession(request.SessionID)
	if !ok {
		return fmt.Errorf("%w: transfer session not found", protocol.ErrPeerForbidden)
	}
	return s.authorizeTrustedPeerRequest(sessionState.senderDeviceID, caller)
}

func (s *RuntimeService) getIncomingTransferSession(sessionID string) (*incomingTransferSession, bool) {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	sessionState, ok := s.incomingTransferSession[sessionID]
	return sessionState, ok
}

func (s *RuntimeService) deleteIncomingTransferSession(sessionID string) {
	s.sessionMu.Lock()
	sessionState := s.incomingTransferSession[sessionID]
	delete(s.incomingTransferSession, sessionID)
	s.sessionMu.Unlock()
	if sessionState != nil && sessionState.idleTimer != nil {
		sessionState.idleTimer.Stop()
	}
}

func (s *RuntimeService) beginIncomingTransferSessionFinalization(sessionState *incomingTransferSession) bool {
	sessionState.mu.Lock()
	defer sessionState.mu.Unlock()

	if sessionState.closed {
		return false
	}
	sessionState.closed = true
	if sessionState.idleTimer != nil {
		sessionState.idleTimer.Stop()
	}
	return true
}

func (s *RuntimeService) finalizeIncomingTransferSessionFailure(
	sessionID string,
	sessionState *incomingTransferSession,
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
	s.deleteIncomingTransferSession(sessionID)
	return failedSnapshot
}

func (s *RuntimeService) expireIncomingTransferSession(sessionID string) {
	sessionState, ok := s.getIncomingTransferSession(sessionID)
	if !ok {
		return
	}

	sessionState.mu.Lock()
	if sessionState.closed {
		sessionState.mu.Unlock()
		return
	}
	if sessionState.activeParts > 0 {
		sessionState.resetIdleTimerLocked()
		sessionState.mu.Unlock()
		return
	}
	sessionState.mu.Unlock()

	if !s.beginIncomingTransferSessionFinalization(sessionState) {
		return
	}
	failedSnapshot := s.finalizeIncomingTransferSessionFailure(sessionID, sessionState)
	s.publishTransferEvent(failedSnapshot)
}

func mergePeerSnapshots(trustedPeers []domain.Peer, discoveredPeers []discovery.PeerRecord) []PeerSnapshot {
	byID := make(map[string]PeerSnapshot, len(trustedPeers)+len(discoveredPeers))
	remarkNames := make(map[string]string, len(trustedPeers))
	for _, peer := range trustedPeers {
		remarkNames[peer.DeviceID] = peer.RemarkName
		byID[peer.DeviceID] = PeerSnapshot{
			DeviceID:   peer.DeviceID,
			DeviceName: resolvePeerDisplayName(peer.DeviceName, peer.RemarkName),
			Trusted:    peer.Trusted,
			Online:     false,
			Reachable:  false,
		}
	}

	for _, peer := range discoveredPeers {
		current := byID[peer.DeviceID]
		current.DeviceID = peer.DeviceID
		if remarkNames[peer.DeviceID] == "" {
			current.DeviceName = peer.DeviceName
		} else if current.DeviceName == "" {
			current.DeviceName = peer.DeviceName
		}
		current.Online = peer.Online
		current.Reachable = peer.Reachable
		current.AgentTCPPort = peer.AgentTCPPort
		current.LastKnownAddr = peer.LastKnownAddr
		byID[peer.DeviceID] = current
	}

	peers := make([]PeerSnapshot, 0, len(byID))
	for _, peer := range byID {
		peers = append(peers, peer)
	}

	sort.Slice(peers, func(i int, j int) bool {
		if peers[i].DeviceName == peers[j].DeviceName {
			return peers[i].DeviceID < peers[j].DeviceID
		}
		return peers[i].DeviceName < peers[j].DeviceName
	})

	return peers
}

func resolvePeerDisplayName(deviceName string, remarkName string) string {
	if remarkName != "" {
		return remarkName
	}
	return deviceName
}

func mapPairingSnapshots(pairings []session.PairingSession) []PairingSnapshot {
	snapshots := make([]PairingSnapshot, 0, len(pairings))
	for _, pairing := range pairings {
		snapshots = append(snapshots, toPairingSnapshot(pairing))
	}
	sort.Slice(snapshots, func(i int, j int) bool {
		if snapshots[i].PeerDeviceName == snapshots[j].PeerDeviceName {
			return snapshots[i].PairingID < snapshots[j].PairingID
		}
		return snapshots[i].PeerDeviceName < snapshots[j].PeerDeviceName
	})
	return snapshots
}

func mapConversationSnapshots(conversations []domain.Conversation, peers []PeerSnapshot) []ConversationSnapshot {
	peerNames := make(map[string]string, len(peers))
	for _, peer := range peers {
		peerNames[peer.DeviceID] = peer.DeviceName
	}

	snapshots := make([]ConversationSnapshot, 0, len(conversations))
	for _, conversation := range conversations {
		snapshots = append(snapshots, ConversationSnapshot{
			ConversationID: conversation.ConversationID,
			PeerDeviceID:   conversation.PeerDeviceID,
			PeerDeviceName: peerNames[conversation.PeerDeviceID],
		})
	}
	return snapshots
}

func (s *RuntimeService) mapTransferSnapshots(transfers []domain.Transfer) []TransferSnapshot {
	snapshots := make([]TransferSnapshot, 0, len(transfers))
	for _, transfer := range transfers {
		snapshots = append(snapshots, s.toTransferSnapshot(transfer))
	}
	return snapshots
}

func toPairingSnapshot(pairing session.PairingSession) PairingSnapshot {
	return PairingSnapshot{
		PairingID:      pairing.PairingID,
		PeerDeviceID:   pairing.PeerDeviceID,
		PeerDeviceName: pairing.PeerDeviceName,
		ShortCode:      pairing.ShortCode,
		Status:         string(pairing.Status),
	}
}

func (s *RuntimeService) mapMessageSnapshots(conversations []domain.Conversation) ([]MessageSnapshot, error) {
	snapshots := make([]MessageSnapshot, 0)
	for _, conversation := range conversations {
		messages, err := s.store.ListMessages(conversation.ConversationID)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			snapshots = append(snapshots, toMessageSnapshot(message))
		}
	}
	return snapshots, nil
}

func toMessageSnapshot(message domain.Message) MessageSnapshot {
	return MessageSnapshot{
		MessageID:      message.MessageID,
		ConversationID: message.ConversationID,
		Direction:      message.Direction,
		Kind:           message.Kind,
		Body:           message.Body,
		Status:         message.Status,
		CreatedAt:      message.CreatedAt.Format(time.RFC3339Nano),
	}
}

func (s *RuntimeService) toTransferSnapshot(transferRecord domain.Transfer) TransferSnapshot {
	transferRecord = s.mergeTransferTelemetry(transferRecord)
	return toTransferSnapshot(transferRecord)
}

func toTransferSnapshot(transferRecord domain.Transfer) TransferSnapshot {
	transferRecord = normalizeTransferRecord(transferRecord)
	return TransferSnapshot{
		TransferID:       transferRecord.TransferID,
		MessageID:        transferRecord.MessageID,
		FileName:         transferRecord.FileName,
		FileSize:         transferRecord.FileSize,
		State:            transferRecord.State,
		Direction:        transferRecord.Direction,
		BytesTransferred: transferRecord.BytesTransferred,
		ProgressPercent:  transferRecord.ProgressPercent,
		RateBytesPerSec:  transferRecord.RateBytesPerSec,
		EtaSeconds:       transferRecord.EtaSeconds,
		Active:           transferRecord.Active,
		CreatedAt:        transferRecord.CreatedAt.Format(time.RFC3339Nano),
	}
}

func findDiscoveryPeer(peers []discovery.PeerRecord, deviceID string) (discovery.PeerRecord, bool) {
	for _, peer := range peers {
		if peer.DeviceID == deviceID {
			return peer, true
		}
	}
	return discovery.PeerRecord{}, false
}

func (s *RuntimeService) publishPairingEvent(pairing session.PairingSession) {
	if s.events == nil {
		return
	}
	s.events.Publish("pairing.updated", toPairingSnapshot(pairing))
}

func (s *RuntimeService) publishPeerEvent(deviceID string) {
	if s.events == nil {
		return
	}

	snapshot, err := s.Bootstrap()
	if err != nil {
		return
	}

	for _, peer := range snapshot.Peers {
		if peer.DeviceID == deviceID {
			s.events.Publish("peer.updated", peer)
			return
		}
	}
}

func (s *RuntimeService) publishMessageEvent(message domain.Message) {
	if s.events == nil {
		return
	}
	s.events.Publish("message.upserted", toMessageSnapshot(message))
}

func (s *RuntimeService) publishTransferEvent(transfer domain.Transfer) {
	if s.events == nil {
		return
	}
	s.events.Publish("transfer.updated", s.toTransferSnapshot(transfer))
}

func (s *RuntimeService) RunHeartbeatLoop(ctx context.Context) {
	if s.transport == nil || s.store == nil || s.discovery == nil {
		<-ctx.Done()
		return
	}

	s.runHeartbeatSweep(ctx)

	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runHeartbeatSweep(ctx)
		}
	}
}

func (s *RuntimeService) runHeartbeatSweep(ctx context.Context) {
	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil || !ok {
		return
	}

	discoveredPeers := make(map[string]discovery.PeerRecord)
	for _, peer := range s.discovery.List() {
		discoveredPeers[peer.DeviceID] = peer
	}

	trustedPeers, err := s.store.ListTrustedPeers()
	if err != nil {
		return
	}

	for _, trustedPeer := range trustedPeers {
		if !trustedPeer.Trusted {
			continue
		}

		peer, ok := discoveredPeers[trustedPeer.DeviceID]
		if !ok || strings.TrimSpace(peer.LastKnownAddr) == "" {
			continue
		}
		peer.PinnedFingerprint = trustedPeer.PinnedFingerprint

		probeCtx := ctx
		cancel := func() {}
		if s.heartbeatTimeout > 0 {
			probeCtx, cancel = context.WithTimeout(ctx, s.heartbeatTimeout)
		}
		response, err := s.transport.SendHeartbeat(probeCtx, peer, protocol.HeartbeatRequest{
			SenderDeviceID: localDevice.DeviceID,
			SentAtRFC3339:  time.Now().UTC().Format(time.RFC3339Nano),
			AgentTCPPort:   s.cfg.AgentTCPPort,
		})
		cancel()
		if err != nil {
			if s.recordHeartbeatFailure(trustedPeer.DeviceID) >= s.heartbeatFailureThreshold {
				s.updatePeerReachability(trustedPeer.DeviceID, false)
			}
			continue
		}

		s.resetHeartbeatFailure(trustedPeer.DeviceID)
		s.markPeerDirectActiveAt(
			trustedPeer.DeviceID,
			heartbeatPeerAddr(peer.LastKnownAddr, response.AgentTCPPort),
			response.AgentTCPPort,
			time.Now().UTC(),
		)
	}
}

func (s *RuntimeService) updatePeerReachability(deviceID string, reachable bool) {
	if s.discovery == nil || strings.TrimSpace(deviceID) == "" {
		return
	}

	observedAt := time.Now().UTC()
	before, _ := s.discovery.Snapshot(deviceID, observedAt)
	if reachable {
		s.resetHeartbeatFailure(deviceID)
		s.discovery.MarkDirectActive(deviceID, "", 0, observedAt)
	} else {
		s.discovery.MarkReachable(deviceID, false)
	}
	if after, ok := s.discovery.Snapshot(deviceID, observedAt); ok && shouldPublishPeerUpdate(before, after) {
		s.publishPeerEvent(deviceID)
	}
}

func (s *RuntimeService) markPeerDirectActive(ctx context.Context, deviceID string, agentTCPPort int) {
	if s.discovery == nil || strings.TrimSpace(deviceID) == "" {
		return
	}

	addr := strings.TrimSpace(directPeerAddrFromContext(ctx, agentTCPPort))
	if addr == "" {
		if _, ok := s.discovery.Get(deviceID); !ok {
			return
		}
	}

	observedAt := time.Now().UTC()
	before, _ := s.discovery.Snapshot(deviceID, observedAt)
	s.resetHeartbeatFailure(deviceID)
	s.discovery.MarkDirectActive(
		deviceID,
		addr,
		agentTCPPort,
		observedAt,
	)
	if after, ok := s.discovery.Snapshot(deviceID, observedAt); ok && shouldPublishPeerUpdate(before, after) {
		s.publishPeerEvent(deviceID)
	}
}

func (s *RuntimeService) markPeerDirectActiveAt(deviceID string, addr string, agentTCPPort int, seenAt time.Time) {
	if s.discovery == nil || strings.TrimSpace(deviceID) == "" {
		return
	}

	addr = strings.TrimSpace(addr)
	if addr == "" {
		if _, ok := s.discovery.Get(deviceID); !ok {
			return
		}
	}

	observedAt := seenAt.UTC()
	before, _ := s.discovery.Snapshot(deviceID, observedAt)
	s.resetHeartbeatFailure(deviceID)
	s.discovery.MarkDirectActive(deviceID, addr, agentTCPPort, observedAt)
	if after, ok := s.discovery.Snapshot(deviceID, observedAt); ok && shouldPublishPeerUpdate(before, after) {
		s.publishPeerEvent(deviceID)
	}
}

func (s *RuntimeService) recordHeartbeatFailure(deviceID string) int {
	s.heartbeatMu.Lock()
	defer s.heartbeatMu.Unlock()

	s.heartbeatFailures[deviceID]++
	return s.heartbeatFailures[deviceID]
}

func (s *RuntimeService) resetHeartbeatFailure(deviceID string) {
	s.heartbeatMu.Lock()
	defer s.heartbeatMu.Unlock()

	delete(s.heartbeatFailures, deviceID)
}

func (s *RuntimeService) mergeTransferTelemetry(transferRecord domain.Transfer) domain.Transfer {
	if s.transfers == nil {
		return s.mergeTransferOverride(normalizeTransferRecord(transferRecord))
	}

	snapshot, ok := s.transfers.Snapshot(transferRecord.TransferID, time.Now().UTC())
	if !ok {
		return s.mergeTransferOverride(normalizeTransferRecord(transferRecord))
	}

	transferRecord.BytesTransferred = snapshot.BytesTransferred
	transferRecord.ProgressPercent = snapshot.ProgressPercent
	transferRecord.RateBytesPerSec = snapshot.RateBytesPerSec
	transferRecord.EtaSeconds = snapshot.EtaSeconds
	transferRecord.Active = true
	return s.mergeTransferOverride(normalizeTransferRecord(transferRecord))
}

func normalizeTransferRecord(transferRecord domain.Transfer) domain.Transfer {
	if strings.TrimSpace(transferRecord.Direction) == "" {
		transferRecord.Direction = "outgoing"
	}

	if transferRecord.State == transfer.StateDone && transferRecord.FileSize > 0 && transferRecord.BytesTransferred == 0 {
		transferRecord.BytesTransferred = transferRecord.FileSize
	}
	if transferRecord.BytesTransferred < 0 {
		transferRecord.BytesTransferred = 0
	}

	if transferRecord.ProgressPercent <= 0 {
		switch {
		case transferRecord.FileSize > 0:
			transferRecord.ProgressPercent = (float64(transferRecord.BytesTransferred) / float64(transferRecord.FileSize)) * 100
		case transferRecord.State == transfer.StateDone:
			transferRecord.ProgressPercent = 100
		}
	}
	if transferRecord.ProgressPercent > 100 {
		transferRecord.ProgressPercent = 100
	}

	if transferRecord.State == transfer.StateDone {
		transferRecord.Active = false
		transferRecord.RateBytesPerSec = 0
		transferRecord.EtaSeconds = nil
	}
	if transferRecord.State == transfer.StateFailed {
		transferRecord.Active = false
		transferRecord.EtaSeconds = nil
	}

	return transferRecord
}

func shouldPublishPeerUpdate(before discovery.PeerRecord, after discovery.PeerRecord) bool {
	return before.Online != after.Online ||
		before.Reachable != after.Reachable ||
		before.AgentTCPPort != after.AgentTCPPort ||
		before.LastKnownAddr != after.LastKnownAddr
}

func shouldPublishThrottledTransferProgress(
	progressGate *transfer.ProgressEventGate,
	progressWarm *bool,
	transferRecord domain.Transfer,
	telemetrySnapshot transfer.Snapshot,
	delta int64,
	now time.Time,
) bool {
	meaningfulIntermediate := hasMeaningfulIntermediateTransferTelemetry(transferRecord, telemetrySnapshot)
	if progressGate.Allow(delta, now) {
		if meaningfulIntermediate {
			*progressWarm = true
			return true
		}
		return transferRecord.FileSize <= 0 || transferRecord.BytesTransferred >= transferRecord.FileSize
	}
	if meaningfulIntermediate && !*progressWarm {
		*progressWarm = true
		return true
	}
	return false
}

func hasMeaningfulIntermediateTransferTelemetry(
	transferRecord domain.Transfer,
	telemetrySnapshot transfer.Snapshot,
) bool {
	return transferRecord.FileSize > 0 &&
		transferRecord.BytesTransferred > 0 &&
		transferRecord.BytesTransferred < transferRecord.FileSize &&
		telemetrySnapshot.RateBytesPerSec > 0 &&
		telemetrySnapshot.EtaSeconds != nil
}

func newRandomID(prefix string) string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(buffer)
}

func directPeerAddrFromContext(ctx context.Context, agentTCPPort int) string {
	if agentTCPPort <= 0 {
		return ""
	}
	caller, ok := protocol.CallerFromContext(ctx)
	if !ok || strings.TrimSpace(caller.RemoteAddr) == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(caller.RemoteAddr))
	if err != nil || strings.TrimSpace(host) == "" {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(agentTCPPort))
}

func heartbeatPeerAddr(lastKnownAddr string, agentTCPPort int) string {
	lastKnownAddr = strings.TrimSpace(lastKnownAddr)
	if lastKnownAddr == "" || agentTCPPort <= 0 {
		return lastKnownAddr
	}

	host, _, err := net.SplitHostPort(lastKnownAddr)
	if err != nil || strings.TrimSpace(host) == "" {
		return lastKnownAddr
	}
	return net.JoinHostPort(host, strconv.Itoa(agentTCPPort))
}

type readerOnly struct {
	io.Reader
}

func (s *RuntimeService) rememberTransferOverride(transferRecord domain.Transfer) {
	s.overrideMu.Lock()
	defer s.overrideMu.Unlock()
	if s.overrides == nil {
		s.overrides = make(map[string]domain.Transfer)
	}
	s.overrides[transferRecord.TransferID] = normalizeTransferRecord(transferRecord)
}

func (s *RuntimeService) clearTransferOverride(transferID string) {
	s.overrideMu.Lock()
	defer s.overrideMu.Unlock()
	delete(s.overrides, transferID)
}

func (s *RuntimeService) mergeTransferOverride(transferRecord domain.Transfer) domain.Transfer {
	s.overrideMu.RLock()
	override, ok := s.overrides[transferRecord.TransferID]
	s.overrideMu.RUnlock()
	if !ok {
		return transferRecord
	}
	return normalizeTransferRecord(override)
}

func (s *RuntimeService) isTrustedPeer(deviceID string) bool {
	_, ok, err := s.trustedPeer(deviceID)
	if err != nil {
		return false
	}
	return ok
}

func (s *RuntimeService) authorizeTrustedPeerRequest(deviceID string, caller protocol.PeerCaller) error {
	if strings.TrimSpace(caller.Fingerprint) == "" {
		return protocol.ErrPeerAuthenticationRequired
	}

	peer, ok, err := s.trustedPeer(deviceID)
	if err != nil {
		return fmt.Errorf("list trusted peers: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: peer %s is not trusted", protocol.ErrPeerForbidden, strings.TrimSpace(deviceID))
	}
	if peer.PinnedFingerprint == "" {
		return fmt.Errorf("%w: peer %s has no pinned fingerprint", protocol.ErrPeerForbidden, peer.DeviceID)
	}
	if caller.Fingerprint != peer.PinnedFingerprint {
		return fmt.Errorf("%w: peer fingerprint mismatch", protocol.ErrPeerForbidden)
	}
	return nil
}

func (s *RuntimeService) trustedPeer(deviceID string) (domain.Peer, bool, error) {
	peers, err := s.store.ListTrustedPeers()
	if err != nil {
		return domain.Peer{}, false, err
	}
	for _, peer := range peers {
		if peer.DeviceID == strings.TrimSpace(deviceID) && peer.Trusted {
			return peer, true, nil
		}
	}
	return domain.Peer{}, false, nil
}

func authorizeCallerFingerprint(expected string, actual string, role string) error {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" {
		return fmt.Errorf("%w: %s fingerprint missing", protocol.ErrPeerForbidden, role)
	}
	if actual == "" {
		return protocol.ErrPeerAuthenticationRequired
	}
	if expected != actual {
		return fmt.Errorf("%w: %s fingerprint mismatch", protocol.ErrPeerForbidden, role)
	}
	return nil
}
