package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"message-share/backend/internal/config"
	"message-share/backend/internal/diagnostics"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/domain"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/security"
	"message-share/backend/internal/session"
	"message-share/backend/internal/transfer"
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
	TransferID string `json:"transferId"`
	MessageID  string `json:"messageId"`
	FileName   string `json:"fileName"`
	FileSize   int64  `json:"fileSize"`
	State      string `json:"state"`
	CreatedAt  string `json:"createdAt"`
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
	ListMessages(conversationID string) ([]domain.Message, error)
	SaveTransfer(transfer domain.Transfer) error
	ListTransfers() ([]domain.Transfer, error)
	UpdateTransferState(transferID string, state string) error
}

type Service interface {
	Bootstrap() (BootstrapSnapshot, error)
	StartPairing(ctx context.Context, peerDeviceID string) (PairingSnapshot, error)
	ConfirmPairing(ctx context.Context, pairingID string) (PairingSnapshot, error)
	SendTextMessage(ctx context.Context, peerDeviceID string, body string) (MessageSnapshot, error)
	SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (TransferSnapshot, error)
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
	SendTextMessage(ctx context.Context, peer discovery.PeerRecord, request protocol.TextMessageRequest) (protocol.AckResponse, error)
	SendFile(ctx context.Context, peer discovery.PeerRecord, request protocol.FileTransferRequest, content io.Reader) (protocol.FileTransferResponse, error)
}

type RuntimeDeps struct {
	Config    config.AppConfig
	Store     Store
	Discovery *discovery.Registry
	Pairings  PairingManager
	Events    EventPublisher
	Transport PeerTransport
}

type RuntimeService struct {
	cfg       config.AppConfig
	store     Store
	discovery *discovery.Registry
	pairings  PairingManager
	events    EventPublisher
	transport PeerTransport
}

func NewRuntimeService(deps RuntimeDeps) *RuntimeService {
	if deps.Discovery == nil {
		deps.Discovery = discovery.NewRegistry()
	}
	if deps.Pairings == nil {
		deps.Pairings = session.NewService()
	}

	return &RuntimeService{
		cfg:       deps.Config,
		store:     deps.Store,
		discovery: deps.Discovery,
		pairings:  deps.Pairings,
		events:    deps.Events,
		transport: deps.Transport,
	}
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
		Transfers:       mapTransferSnapshots(transfers),
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
		TransferID: newRandomID("transfer"),
		MessageID:  message.MessageID,
		FileName:   filepath.Base(fileName),
		FileSize:   fileSize,
		State:      "sending",
		CreatedAt:  message.CreatedAt,
	}

	response, err := s.transport.SendFile(ctx, peer, protocol.FileTransferRequest{
		TransferID:       transferRecord.TransferID,
		MessageID:        message.MessageID,
		SenderDeviceID:   localDevice.DeviceID,
		FileName:         transferRecord.FileName,
		FileSize:         fileSize,
		CreatedAtRFC3339: message.CreatedAt.Format(time.RFC3339Nano),
	}, content)
	if err != nil {
		s.updatePeerReachability(peer.DeviceID, false)
		return TransferSnapshot{}, fmt.Errorf("send file: %w", err)
	}
	s.updatePeerReachability(peer.DeviceID, true)

	if response.TransferID != "" {
		transferRecord.TransferID = response.TransferID
	}
	if response.State != "" {
		transferRecord.State = response.State
	} else {
		transferRecord.State = "done"
	}
	message.Status = "sent"

	if err := s.store.SaveMessage(message); err != nil {
		return TransferSnapshot{}, fmt.Errorf("save outgoing file message: %w", err)
	}
	if err := s.store.SaveTransfer(transferRecord); err != nil {
		return TransferSnapshot{}, fmt.Errorf("save outgoing transfer: %w", err)
	}

	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)
	return toTransferSnapshot(transferRecord), nil
}

func (s *RuntimeService) AcceptIncomingPairing(
	_ context.Context,
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

	s.publishPairingEvent(pairing)
	s.publishPeerEvent(pairing.PeerDeviceID)

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
	_ context.Context,
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

	s.publishPairingEvent(updated)
	s.publishPeerEvent(updated.PeerDeviceID)

	return protocol.PairingConfirmResponse{
		PairingID:       updated.PairingID,
		Status:          string(updated.Status),
		RemoteConfirmed: updated.RemoteConfirmed,
	}, nil
}

func (s *RuntimeService) AcceptIncomingTextMessage(
	_ context.Context,
	request protocol.TextMessageRequest,
) (protocol.AckResponse, error) {
	if !s.isTrustedPeer(request.SenderDeviceID) {
		return protocol.AckResponse{}, fmt.Errorf("peer %s is not trusted", request.SenderDeviceID)
	}

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
	_ context.Context,
	request protocol.FileTransferRequest,
	content io.Reader,
) (protocol.FileTransferResponse, error) {
	if !s.isTrustedPeer(request.SenderDeviceID) {
		return protocol.FileTransferResponse{}, fmt.Errorf("peer %s is not trusted", request.SenderDeviceID)
	}

	conversation, err := s.store.EnsureConversation(request.SenderDeviceID)
	if err != nil {
		return protocol.FileTransferResponse{}, fmt.Errorf("ensure conversation: %w", err)
	}

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

	written, err := io.Copy(fileWriter, content)
	if err != nil {
		return protocol.FileTransferResponse{}, fmt.Errorf("write incoming file: %w", err)
	}
	if written != request.FileSize {
		return protocol.FileTransferResponse{}, fmt.Errorf("incoming file size mismatch: declared=%d actual=%d", request.FileSize, written)
	}

	finalPath, err := fileWriter.Commit()
	if err != nil {
		return protocol.FileTransferResponse{}, fmt.Errorf("commit incoming file: %w", err)
	}

	messageID := request.MessageID
	if messageID == "" {
		messageID = newRandomID("msg")
	}
	transferID := request.TransferID
	if transferID == "" {
		transferID = newRandomID("transfer")
	}

	message := domain.Message{
		MessageID:      messageID,
		ConversationID: conversation.ConversationID,
		Direction:      "incoming",
		Kind:           "file",
		Body:           filepath.Base(finalPath),
		Status:         "sent",
		CreatedAt:      createdAt,
	}
	transferRecord := domain.Transfer{
		TransferID: transferID,
		MessageID:  messageID,
		FileName:   filepath.Base(finalPath),
		FileSize:   written,
		State:      "done",
		CreatedAt:  createdAt,
	}

	if err := s.store.SaveMessage(message); err != nil {
		return protocol.FileTransferResponse{}, fmt.Errorf("save incoming file message: %w", err)
	}
	if err := s.store.SaveTransfer(transferRecord); err != nil {
		return protocol.FileTransferResponse{}, fmt.Errorf("save incoming transfer: %w", err)
	}

	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)
	return protocol.FileTransferResponse{
		TransferID: transferID,
		State:      transferRecord.State,
	}, nil
}

func (s *RuntimeService) AuthorizeFileTransfer(
	_ context.Context,
	request protocol.FileTransferRequest,
	caller protocol.PeerCaller,
) error {
	return s.authorizeTrustedPeerRequest(request.SenderDeviceID, caller)
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

func mapTransferSnapshots(transfers []domain.Transfer) []TransferSnapshot {
	snapshots := make([]TransferSnapshot, 0, len(transfers))
	for _, transfer := range transfers {
		snapshots = append(snapshots, toTransferSnapshot(transfer))
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

func toTransferSnapshot(transfer domain.Transfer) TransferSnapshot {
	return TransferSnapshot{
		TransferID: transfer.TransferID,
		MessageID:  transfer.MessageID,
		FileName:   transfer.FileName,
		FileSize:   transfer.FileSize,
		State:      transfer.State,
		CreatedAt:  transfer.CreatedAt.Format(time.RFC3339Nano),
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
	s.events.Publish("transfer.updated", toTransferSnapshot(transfer))
}

func (s *RuntimeService) updatePeerReachability(deviceID string, reachable bool) {
	if s.discovery == nil || strings.TrimSpace(deviceID) == "" {
		return
	}
	s.discovery.MarkReachable(deviceID, reachable)
	s.publishPeerEvent(deviceID)
}

func newRandomID(prefix string) string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(buffer)
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
