package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"message-share/backend/internal/config"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/domain"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/session"
	"message-share/backend/internal/store"
)

func TestBootstrapBuildsSnapshotFromStoreAndDiscovery(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	localDevice := domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "我的电脑",
		PublicKeyPEM:  "public",
		PrivateKeyPEM: "private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}
	if err := db.SaveLocalDevice(localDevice); err != nil {
		t.Fatalf("unexpected save local device error: %v", err)
	}

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-1",
		DeviceName:        "会议室电脑",
		PinnedFingerprint: "fingerprint-a",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	registry := discovery.NewRegistry()
	now := time.Now().UTC()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-1",
			DeviceName:      "会议室电脑",
			AgentTCPPort:    19090,
		},
		"192.168.1.8:19090",
		now,
	)

	conversation, err := db.EnsureConversation("peer-1")
	if err != nil {
		t.Fatalf("unexpected ensure conversation error: %v", err)
	}
	if err := db.SaveMessage(domain.Message{
		MessageID:      "msg-1",
		ConversationID: conversation.ConversationID,
		Direction:      "outgoing",
		Kind:           "text",
		Body:           "hello",
		Status:         "sent",
		CreatedAt:      time.Date(2026, 4, 10, 8, 3, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save message error: %v", err)
	}
	if err := db.SaveTransfer(domain.Transfer{
		TransferID: "transfer-1",
		MessageID:  "msg-1",
		FileName:   "hello.txt",
		FileSize:   5,
		State:      "done",
		CreatedAt:  time.Date(2026, 4, 10, 8, 4, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save transfer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
	})
	snapshot, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}

	if snapshot.LocalDeviceName != "我的电脑" {
		t.Fatalf("unexpected local device name: %#v", snapshot)
	}
	if len(snapshot.Peers) != 1 {
		t.Fatalf("expected one peer, got %#v", snapshot.Peers)
	}

	peer := snapshot.Peers[0]
	if peer.DeviceID != "peer-1" || peer.DeviceName != "会议室电脑" {
		t.Fatalf("unexpected peer snapshot: %#v", peer)
	}
	if !peer.Trusted || !peer.Online || peer.LastKnownAddr != "192.168.1.8:19090" {
		t.Fatalf("unexpected peer flags: %#v", peer)
	}
	if snapshot.Health["agentPort"] != config.Default().AgentTCPPort {
		t.Fatalf("unexpected health snapshot: %#v", snapshot.Health)
	}
	if len(snapshot.Conversations) != 1 || snapshot.Conversations[0].ConversationID != conversation.ConversationID {
		t.Fatalf("unexpected conversations: %#v", snapshot.Conversations)
	}
	if len(snapshot.Messages) != 1 || snapshot.Messages[0].MessageID != "msg-1" {
		t.Fatalf("unexpected messages: %#v", snapshot.Messages)
	}
	if len(snapshot.Transfers) != 1 || snapshot.Transfers[0].TransferID != "transfer-1" {
		t.Fatalf("unexpected transfers: %#v", snapshot.Transfers)
	}
}

func TestBootstrapKeepsDiscoveryPendingWhenOnlyTrustedPeerExists(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	localDevice := domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "public",
		PrivateKeyPEM: "private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}
	if err := db.SaveLocalDevice(localDevice); err != nil {
		t.Fatalf("unexpected save local device error: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-1",
		DeviceName:        "meeting-room",
		PinnedFingerprint: "fingerprint-a",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	snapshot, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}
	if snapshot.Health["discovery"] != "broadcast-pending" {
		t.Fatalf("expected discovery pending health, got %#v", snapshot.Health)
	}
}

func TestStartAndConfirmPairingPersistsTrustedPeer(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-1",
			DeviceName:      "meeting-room",
			AgentTCPPort:    19090,
		},
		"192.168.1.8:19090",
		time.Date(2026, 4, 10, 8, 2, 0, 0, time.UTC),
	)

	publisher := &fakeEventPublisher{}
	transport := &fakePeerTransport{
		startResponse: protocol.PairingStartResponse{
			PairingID:            "pair-1",
			ResponderDeviceID:    "peer-1",
			ResponderDeviceName:  "meeting-room",
			ResponderFingerprint: "fingerprint-a",
			ResponderNonce:       "nonce-b",
		},
		confirmResponse: protocol.PairingConfirmResponse{
			PairingID:       "pair-1",
			Status:          "confirmed",
			RemoteConfirmed: true,
		},
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		Events:    publisher,
	})

	pairing, err := svc.StartPairing(context.Background(), "peer-1")
	if err != nil {
		t.Fatalf("unexpected start pairing error: %v", err)
	}
	if pairing.PairingID != "pair-1" || pairing.Status != string(session.PairingStatusPending) {
		t.Fatalf("unexpected pairing snapshot: %#v", pairing)
	}

	confirmed, err := svc.ConfirmPairing(context.Background(), "pair-1")
	if err != nil {
		t.Fatalf("unexpected confirm pairing error: %v", err)
	}
	if confirmed.Status != string(session.PairingStatusConfirmed) {
		t.Fatalf("expected confirmed pairing, got %#v", confirmed)
	}
	if transport.confirmPeer.PinnedFingerprint != "fingerprint-a" {
		t.Fatalf("expected confirm transport to use remote fingerprint, got %#v", transport.confirmPeer)
	}

	peers, err := db.ListTrustedPeers()
	if err != nil {
		t.Fatalf("unexpected list peers error: %v", err)
	}
	if len(peers) != 1 || peers[0].DeviceID != "peer-1" || peers[0].PinnedFingerprint != "fingerprint-a" {
		t.Fatalf("unexpected trusted peers: %#v", peers)
	}
	if len(publisher.events) < 2 {
		t.Fatalf("expected pairing and peer events, got %#v", publisher.events)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}
	if len(bootstrap.Pairings) != 1 || bootstrap.Pairings[0].Status != string(session.PairingStatusConfirmed) {
		t.Fatalf("unexpected bootstrap pairings: %#v", bootstrap.Pairings)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Trusted {
		t.Fatalf("expected trusted peer in bootstrap, got %#v", bootstrap.Peers)
	}
}

func TestAcceptIncomingPairingCreatesPendingSnapshot(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	response, err := svc.AcceptIncomingPairing(context.Background(), protocol.PairingStartRequest{
		PairingID:            "pair-2",
		InitiatorDeviceID:    "peer-2",
		InitiatorDeviceName:  "peer-two",
		InitiatorFingerprint: "fingerprint-b",
		InitiatorNonce:       "nonce-a",
	})
	if err != nil {
		t.Fatalf("unexpected accept pairing error: %v", err)
	}
	if response.PairingID != "pair-2" || response.ResponderDeviceID != "local-1" {
		t.Fatalf("unexpected pairing response: %#v", response)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}
	if len(bootstrap.Pairings) != 1 || bootstrap.Pairings[0].Status != string(session.PairingStatusPending) {
		t.Fatalf("unexpected bootstrap pairings: %#v", bootstrap.Pairings)
	}
}

func TestAcceptPairingConfirmPersistsTrustWhenLocalAlreadyConfirmed(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	if _, err := svc.AcceptIncomingPairing(context.Background(), protocol.PairingStartRequest{
		PairingID:            "pair-3",
		InitiatorDeviceID:    "peer-3",
		InitiatorDeviceName:  "peer-three",
		InitiatorFingerprint: "fingerprint-c",
		InitiatorNonce:       "nonce-a",
	}); err != nil {
		t.Fatalf("unexpected accept pairing error: %v", err)
	}
	if _, err := svc.pairings.MarkLocalConfirmed("pair-3"); err != nil {
		t.Fatalf("unexpected local confirm error: %v", err)
	}

	response, err := svc.AcceptPairingConfirm(context.Background(), protocol.PairingConfirmRequest{
		PairingID:            "pair-3",
		ConfirmerDeviceID:    "peer-3",
		ConfirmerFingerprint: "fingerprint-c",
		Confirmed:            true,
	})
	if err != nil {
		t.Fatalf("unexpected remote confirm error: %v", err)
	}
	if response.Status != string(session.PairingStatusConfirmed) || !response.RemoteConfirmed {
		t.Fatalf("unexpected confirm response: %#v", response)
	}

	peers, err := db.ListTrustedPeers()
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(peers) != 1 || peers[0].DeviceID != "peer-3" || peers[0].PinnedFingerprint != "fingerprint-c" {
		t.Fatalf("unexpected trusted peers: %#v", peers)
	}
}

func TestAuthorizePairingConfirmRejectsCallerFingerprintMismatch(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	if _, err := svc.AcceptIncomingPairing(context.Background(), protocol.PairingStartRequest{
		PairingID:            "pair-auth",
		InitiatorDeviceID:    "peer-auth",
		InitiatorDeviceName:  "peer-auth",
		InitiatorFingerprint: "fingerprint-expected",
		InitiatorNonce:       "nonce-a",
	}); err != nil {
		t.Fatalf("unexpected accept pairing error: %v", err)
	}

	err = svc.AuthorizePairingConfirm(context.Background(), protocol.PairingConfirmRequest{
		PairingID:            "pair-auth",
		ConfirmerDeviceID:    "peer-auth",
		ConfirmerFingerprint: "fingerprint-expected",
		Confirmed:            true,
	}, protocol.PeerCaller{Fingerprint: "fingerprint-other"})
	if !errors.Is(err, protocol.ErrPeerForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestSendTextMessagePersistsConversationAndMessage(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-4",
		DeviceName:        "peer-four",
		PinnedFingerprint: "fingerprint-d",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-4",
			DeviceName:      "peer-four",
			AgentTCPPort:    19090,
		},
		"192.168.1.9:19090",
		time.Date(2026, 4, 10, 8, 2, 0, 0, time.UTC),
	)

	publisher := &fakeEventPublisher{}
	transport := &fakePeerTransport{
		ackResponse: protocol.AckResponse{
			RequestID: "msg-1",
			Status:    "accepted",
		},
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		Events:    publisher,
	})

	message, err := svc.SendTextMessage(context.Background(), "peer-4", "hello")
	if err != nil {
		t.Fatalf("unexpected send text error: %v", err)
	}
	if message.Body != "hello" || message.Direction != "outgoing" || message.Status != "sent" {
		t.Fatalf("unexpected message snapshot: %#v", message)
	}
	if transport.textPeer.PinnedFingerprint != "fingerprint-d" {
		t.Fatalf("expected text transport to use trusted fingerprint, got %#v", transport.textPeer)
	}

	conversation, err := db.EnsureConversation("peer-4")
	if err != nil {
		t.Fatalf("unexpected ensure conversation error: %v", err)
	}
	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("unexpected list messages error: %v", err)
	}
	if len(messages) != 1 || messages[0].Body != "hello" || messages[0].Direction != "outgoing" {
		t.Fatalf("unexpected stored messages: %#v", messages)
	}
	if len(publisher.events) == 0 {
		t.Fatal("expected message event")
	}
}

func TestAcceptIncomingTextMessageStoresIncomingMessage(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-5",
		DeviceName:        "peer-five",
		PinnedFingerprint: "fingerprint-e",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	ack, err := svc.AcceptIncomingTextMessage(context.Background(), protocol.TextMessageRequest{
		MessageID:        "msg-2",
		SenderDeviceID:   "peer-5",
		Body:             "hi back",
		CreatedAtRFC3339: time.Date(2026, 4, 10, 8, 20, 0, 0, time.UTC).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("unexpected accept text error: %v", err)
	}
	if ack.Status != "accepted" {
		t.Fatalf("unexpected ack: %#v", ack)
	}

	conversation, err := db.EnsureConversation("peer-5")
	if err != nil {
		t.Fatalf("unexpected ensure conversation error: %v", err)
	}
	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("unexpected list messages error: %v", err)
	}
	if len(messages) != 1 || messages[0].Direction != "incoming" || messages[0].Body != "hi back" {
		t.Fatalf("unexpected stored incoming messages: %#v", messages)
	}
}

func TestAuthorizeTextMessageRejectsFingerprintMismatchForTrustedPeer(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-5",
		DeviceName:        "peer-five",
		PinnedFingerprint: "fingerprint-e",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	err = svc.AuthorizeTextMessage(context.Background(), protocol.TextMessageRequest{
		MessageID:      "msg-2",
		SenderDeviceID: "peer-5",
		Body:           "hi back",
	}, protocol.PeerCaller{Fingerprint: "fingerprint-other"})
	if !errors.Is(err, protocol.ErrPeerForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestSendFilePersistsTransferAndMarksDone(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-6",
			DeviceName:      "peer-six",
			AgentTCPPort:    19090,
		},
		"192.168.1.10:19090",
		time.Date(2026, 4, 10, 8, 2, 0, 0, time.UTC),
	)

	transport := &fakePeerTransport{
		fileResponse: protocol.FileTransferResponse{
			TransferID: "transfer-1",
			State:      "done",
		},
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
	})

	transfer, err := svc.SendFile(context.Background(), "peer-6", "hello.txt", int64(len("hello")), bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}
	if transfer.TransferID == "" || transfer.State != "done" {
		t.Fatalf("unexpected transfer snapshot: %#v", transfer)
	}
	if transport.filePeer.PinnedFingerprint != "fingerprint-f" {
		t.Fatalf("expected file transport to use trusted fingerprint, got %#v", transport.filePeer)
	}

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("unexpected list transfers error: %v", err)
	}
	if len(transfers) != 1 || transfers[0].State != "done" || transfers[0].FileName != "hello.txt" {
		t.Fatalf("unexpected stored transfers: %#v", transfers)
	}
}

func TestAcceptIncomingFileTransferWritesFileAndPersistsDone(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DefaultDownloadDir = t.TempDir()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	response, err := svc.AcceptIncomingFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:     "transfer-2",
		MessageID:      "msg-file-2",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
	}, bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("unexpected incoming file error: %v", err)
	}
	if response.State != "done" {
		t.Fatalf("unexpected file response: %#v", response)
	}

	files, err := os.ReadDir(cfg.DefaultDownloadDir)
	if err != nil {
		t.Fatalf("unexpected read dir error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one downloaded file, got %#v", files)
	}

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("unexpected list transfers error: %v", err)
	}
	if len(transfers) != 1 || transfers[0].State != "done" {
		t.Fatalf("unexpected stored transfers: %#v", transfers)
	}
}

func TestAcceptIncomingFileTransferRejectsSizeMismatch(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DefaultDownloadDir = t.TempDir()

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	_, err = svc.AcceptIncomingFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:     "transfer-mismatch",
		MessageID:      "msg-file-mismatch",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello") + 5),
	}, bytes.NewReader([]byte("hello")))
	if err == nil {
		t.Fatal("expected size mismatch error")
	}

	files, readErr := os.ReadDir(cfg.DefaultDownloadDir)
	if readErr != nil {
		t.Fatalf("unexpected read dir error: %v", readErr)
	}
	if len(files) != 0 {
		t.Fatalf("expected no committed files, got %#v", files)
	}

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("unexpected list transfers error: %v", err)
	}
	if len(transfers) != 0 {
		t.Fatalf("expected no stored transfers, got %#v", transfers)
	}
}

func TestSendTextMessageMarksPeerUnreachableWhenTransportFails(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-8",
		DeviceName:        "peer-eight",
		PinnedFingerprint: "fingerprint-h",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-8",
			DeviceName:      "peer-eight",
			AgentTCPPort:    19090,
		},
		"192.168.1.10:19090",
		time.Now().UTC(),
	)

	transport := &fakePeerTransport{
		sendTextErr: errors.New("dial failed"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
	})

	if _, err := svc.SendTextMessage(context.Background(), "peer-8", "hello"); err == nil {
		t.Fatal("expected send text error")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("unexpected bootstrap error: %v", err)
	}
	if len(bootstrap.Peers) != 1 || bootstrap.Peers[0].Reachable {
		t.Fatalf("expected peer to be marked unreachable, got %#v", bootstrap.Peers)
	}
}

func TestAuthorizeFileTransferRejectsFingerprintMismatchForTrustedPeer(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected trusted peer error: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	err = svc.AuthorizeFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:     "transfer-2",
		MessageID:      "msg-file-2",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
	}, protocol.PeerCaller{Fingerprint: "fingerprint-other"})
	if !errors.Is(err, protocol.ErrPeerForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

type fakePeerTransport struct {
	startResponse   protocol.PairingStartResponse
	confirmResponse protocol.PairingConfirmResponse
	ackResponse     protocol.AckResponse
	fileResponse    protocol.FileTransferResponse
	confirmPeer     discovery.PeerRecord
	textPeer        discovery.PeerRecord
	filePeer        discovery.PeerRecord
	sendTextErr     error
}

func (f *fakePeerTransport) StartPairing(
	_ context.Context,
	_ discovery.PeerRecord,
	_ protocol.PairingStartRequest,
) (protocol.PairingStartResponse, error) {
	return f.startResponse, nil
}

func (f *fakePeerTransport) ConfirmPairing(
	_ context.Context,
	peer discovery.PeerRecord,
	_ protocol.PairingConfirmRequest,
) (protocol.PairingConfirmResponse, error) {
	f.confirmPeer = peer
	return f.confirmResponse, nil
}

func (f *fakePeerTransport) SendTextMessage(
	_ context.Context,
	peer discovery.PeerRecord,
	_ protocol.TextMessageRequest,
) (protocol.AckResponse, error) {
	f.textPeer = peer
	if f.sendTextErr != nil {
		return protocol.AckResponse{}, f.sendTextErr
	}
	return f.ackResponse, nil
}

func (f *fakePeerTransport) SendFile(
	_ context.Context,
	peer discovery.PeerRecord,
	_ protocol.FileTransferRequest,
	_ io.Reader,
) (protocol.FileTransferResponse, error) {
	f.filePeer = peer
	return f.fileResponse, nil
}

type fakeEventPublisher struct {
	events []string
}

func (f *fakeEventPublisher) Publish(kind string, _ any) {
	f.events = append(f.events, kind)
}

func openAppTestStore(t *testing.T) (*store.DB, error) {
	t.Helper()
	return store.Open(filepath.Join(t.TempDir(), "app.db"))
}
