package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"message-share/backend/internal/config"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/domain"
	"message-share/backend/internal/protocol"
	"message-share/backend/internal/session"
	"message-share/backend/internal/store"
	"message-share/backend/internal/transfer"
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

func TestAcceptIncomingTextMessageMarksPeerReachableAgain(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-5",
		DeviceName:        "peer-five",
		PinnedFingerprint: "fingerprint-e",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-5",
		DeviceName:      "peer-five",
		AgentTCPPort:    19090,
	}, "192.168.1.8:19090", time.Now().Add(-10*time.Second))
	registry.MarkReachable("peer-5", false)

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
	})

	if _, err := svc.AcceptIncomingTextMessage(context.Background(), protocol.TextMessageRequest{
		MessageID:        "msg-2",
		SenderDeviceID:   "peer-5",
		Body:             "hi back",
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("unexpected accept text error: %v", err)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected peer to recover reachable state, got %#v", bootstrap.Peers)
	}
}

func TestAcceptIncomingTextMessageLearnsDirectEndpointForReply(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-5",
		DeviceName:        "peer-five",
		PinnedFingerprint: "fingerprint-e",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	transport := &fakePeerTransport{
		ackResponse: protocol.AckResponse{
			RequestID: "reply-1",
			Status:    "accepted",
		},
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
		Transport: transport,
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-e",
		RemoteAddr:  "192.168.1.8:54321",
	})
	if _, err := svc.AcceptIncomingTextMessage(ctx, protocol.TextMessageRequest{
		MessageID:        "msg-2",
		SenderDeviceID:   "peer-5",
		Body:             "hi back",
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
		AgentTCPPort:     19090,
	}); err != nil {
		t.Fatalf("unexpected accept text error: %v", err)
	}

	if _, err := svc.SendTextMessage(context.Background(), "peer-5", "reply"); err != nil {
		t.Fatalf("unexpected send text error: %v", err)
	}
	if transport.textPeer.LastKnownAddr != "192.168.1.8:19090" {
		t.Fatalf("expected learned direct endpoint, got %#v", transport.textPeer)
	}
}

func TestAcceptIncomingTextMessageFailureStillRecoversPeerReachability(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-text-fail",
		DeviceName:        "peer-text-fail",
		PinnedFingerprint: "fingerprint-text-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-text-fail",
		DeviceName:      "peer-text-fail",
		AgentTCPPort:    19090,
	}, "192.168.1.20:19090", time.Now().Add(-10*time.Second))

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config: config.Default(),
		Store: &faultInjectStore{
			DB:                    db,
			ensureConversationErr: errors.New("ensure conversation failed"),
		},
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-text-fail",
		RemoteAddr:  "192.168.1.88:54321",
	})
	if _, err := svc.AcceptIncomingTextMessage(ctx, protocol.TextMessageRequest{
		MessageID:        "msg-text-fail",
		SenderDeviceID:   "peer-text-fail",
		Body:             "hi",
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
		AgentTCPPort:     19090,
	}); err == nil {
		t.Fatal("expected incoming text failure")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected failed incoming text to recover peer reachability, got %#v", bootstrap.Peers)
	}
	if bootstrap.Peers[0].LastKnownAddr != "192.168.1.88:19090" {
		t.Fatalf("expected failed incoming text to learn direct endpoint, got %#v", bootstrap.Peers[0])
	}
	if got := publisher.CountKind("peer.updated"); got != 1 {
		t.Fatalf("expected one peer.updated event after failed incoming text recovery, got %d events: %#v", got, publisher.events)
	}
}

func TestHeartbeatMarksTrustedPeerReachableWithoutUserTraffic(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.18:19090", time.Now().Add(-10*time.Second))
	registry.MarkReachable("peer-6", false)

	transport := &fakePeerTransport{
		heartbeatResponse: protocol.HeartbeatResponse{
			ResponderDeviceID:   "peer-6",
			ResponderDeviceName: "peer-six",
			AgentTCPPort:        19090,
			ReceivedAtRFC3339:   time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:                    config.Default(),
		Store:                     db,
		Discovery:                 registry,
		Pairings:                  session.NewService(),
		Transport:                 transport,
		HeartbeatInterval:         10 * time.Millisecond,
		HeartbeatFailureThreshold: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.RunHeartbeatLoop(ctx)

	waitForCondition(t, 300*time.Millisecond, func() bool {
		bootstrap, err := svc.Bootstrap()
		if err != nil || len(bootstrap.Peers) != 1 {
			return false
		}
		return bootstrap.Peers[0].Reachable
	})

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected peer to recover reachable state, got %#v", bootstrap.Peers)
	}
	if transport.heartbeatPeer.PinnedFingerprint != "fingerprint-f" {
		t.Fatalf("expected heartbeat to use pinned fingerprint, got %#v", transport.heartbeatPeer)
	}
}

func TestHeartbeatRecoveryPublishesPeerUpdatedWhenAnnouncementExpired(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-heartbeat-event",
		DeviceName:        "peer-heartbeat-event",
		PinnedFingerprint: "fingerprint-heartbeat-event",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-heartbeat-event",
		DeviceName:      "peer-heartbeat-event",
		AgentTCPPort:    19090,
	}, "192.168.1.18:19090", time.Now().Add(-10*time.Second))

	transport := &fakePeerTransport{
		heartbeatResponse: protocol.HeartbeatResponse{
			ResponderDeviceID:   "peer-heartbeat-event",
			ResponderDeviceName: "peer-heartbeat-event",
			AgentTCPPort:        19090,
			ReceivedAtRFC3339:   time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:                    config.Default(),
		Store:                     db,
		Discovery:                 registry,
		Pairings:                  session.NewService(),
		Transport:                 transport,
		Events:                    publisher,
		HeartbeatFailureThreshold: 2,
	})

	svc.runHeartbeatSweep(context.Background())

	if got := publisher.CountKind("peer.updated"); got != 1 {
		t.Fatalf("expected one peer.updated event after heartbeat recovery, got %d events: %#v", got, publisher.events)
	}
}

func TestHeartbeatFailureThresholdMarksPeerUnreachable(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	now := time.Now().UTC()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-7",
		DeviceName:      "peer-seven",
		AgentTCPPort:    19090,
	}, "192.168.1.19:19090", now.Add(-10*time.Second))
	registry.MarkDirectActive("peer-7", "192.168.1.19:19090", 19090, now)

	transport := &fakePeerTransport{
		sendHeartbeatErr: errors.New("heartbeat refused"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:                    config.Default(),
		Store:                     db,
		Discovery:                 registry,
		Pairings:                  session.NewService(),
		Transport:                 transport,
		HeartbeatInterval:         10 * time.Millisecond,
		HeartbeatFailureThreshold: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.RunHeartbeatLoop(ctx)

	waitForCondition(t, 300*time.Millisecond, func() bool {
		if transport.heartbeatCalls < 2 {
			return false
		}
		bootstrap, err := svc.Bootstrap()
		if err != nil || len(bootstrap.Peers) != 1 {
			return false
		}
		return !bootstrap.Peers[0].Reachable
	})

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || bootstrap.Peers[0].Reachable {
		t.Fatalf("expected peer to fall back to unreachable after heartbeat failures, got %#v", bootstrap.Peers)
	}
}

func TestHeartbeatFailureStateSurvivesFreshBroadcastUntilDirectRecovery(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-8",
		DeviceName:        "peer-eight",
		PinnedFingerprint: "fingerprint-h",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	now := time.Now().UTC()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-8",
		DeviceName:      "peer-eight",
		AgentTCPPort:    19090,
	}, "192.168.1.20:19090", now.Add(-time.Second))
	registry.MarkDirectActive("peer-8", "192.168.1.20:19090", 19090, now.Add(-500*time.Millisecond))

	transport := &fakePeerTransport{
		sendHeartbeatErr: errors.New("heartbeat refused"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:                    config.Default(),
		Store:                     db,
		Discovery:                 registry,
		Pairings:                  session.NewService(),
		Transport:                 transport,
		HeartbeatInterval:         10 * time.Millisecond,
		HeartbeatFailureThreshold: 1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go svc.RunHeartbeatLoop(ctx)

	waitForCondition(t, 300*time.Millisecond, func() bool {
		bootstrap, err := svc.Bootstrap()
		if err != nil || len(bootstrap.Peers) != 1 {
			return false
		}
		return !bootstrap.Peers[0].Reachable
	})
	cancel()

	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-8",
		DeviceName:      "peer-eight",
		AgentTCPPort:    19090,
	}, "192.168.1.20:19090", time.Now().UTC())

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || bootstrap.Peers[0].Reachable {
		t.Fatalf("expected fresh broadcast to keep failed peer unreachable until direct recovery, got %#v", bootstrap.Peers)
	}
}

func TestDirectRecoveryResetsHeartbeatFailureCount(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-9",
		DeviceName:        "peer-nine",
		PinnedFingerprint: "fingerprint-i",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	now := time.Now().UTC()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-9",
		DeviceName:      "peer-nine",
		AgentTCPPort:    19090,
	}, "192.168.1.21:19090", now)
	registry.MarkDirectActive("peer-9", "192.168.1.21:19090", 19090, now)

	transport := &fakePeerTransport{
		sendHeartbeatErr: errors.New("heartbeat refused"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:                    config.Default(),
		Store:                     db,
		Discovery:                 registry,
		Pairings:                  session.NewService(),
		Transport:                 transport,
		HeartbeatFailureThreshold: 2,
	})

	svc.runHeartbeatSweep(context.Background())
	svc.runHeartbeatSweep(context.Background())

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap after failures: %v", err)
	}
	if len(bootstrap.Peers) != 1 || bootstrap.Peers[0].Reachable {
		t.Fatalf("expected peer to become unreachable after threshold, got %#v", bootstrap.Peers)
	}

	if _, err := svc.AcceptHeartbeat(context.Background(), protocol.HeartbeatRequest{
		SenderDeviceID: "peer-9",
		SentAtRFC3339:  time.Now().UTC().Format(time.RFC3339Nano),
		AgentTCPPort:   19090,
	}); err != nil {
		t.Fatalf("accept heartbeat: %v", err)
	}

	bootstrap, err = svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap after direct recovery: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected direct recovery to restore reachable, got %#v", bootstrap.Peers)
	}

	svc.runHeartbeatSweep(context.Background())

	bootstrap, err = svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap after next failure: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected first failure after direct recovery to stay below threshold, got %#v", bootstrap.Peers)
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

func TestSendFilePublishesActiveTransferSnapshot(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.10:19090", time.Now().UTC())

	publisher := &capturingEventPublisher{}
	transport := &slowPeerTransport{
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
		Events:    publisher,
	})

	if _, err := svc.SendFile(
		context.Background(),
		"peer-6",
		"hello.txt",
		32,
		bytes.NewReader(bytes.Repeat([]byte("a"), 32)),
	); err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}

	if !publisher.HasTransferState("sending") {
		t.Fatalf("expected at least one active transfer.updated event, got %#v", publisher.events)
	}
	if !publisher.HasTransferState("done") {
		t.Fatalf("expected final done event, got %#v", publisher.events)
	}
}

func TestSendFileThrottlesIntermediateTransferSnapshots(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.10:19090", time.Now().UTC())

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: &slowPeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-1",
				State:      "done",
			},
		},
		Events: publisher,
	})

	if _, err := svc.SendFile(
		context.Background(),
		"peer-6",
		"hello.txt",
		1<<20,
		bytes.NewReader(make([]byte, 1<<20)),
	); err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}

	if got := publisher.CountTransferState("sending"); got > 3 {
		t.Fatalf("expected sending events to stay on time cadence for a fast stream, got %d events: %#v", got, publisher.events)
	}
	foundEarlyIntermediate := false
	for _, event := range publisher.events {
		if event.kind != "transfer.updated" {
			continue
		}
		snapshot, ok := event.payload.(TransferSnapshot)
		if !ok || snapshot.State != "sending" {
			continue
		}
		if snapshot.BytesTransferred > 0 && snapshot.BytesTransferred < 256*1024 {
			foundEarlyIntermediate = true
			break
		}
	}
	if !foundEarlyIntermediate {
		t.Fatalf("expected time-based gate to publish an early intermediate sending snapshot, got %#v", publisher.events)
	}
	if !publisher.HasTransferState("done") {
		t.Fatalf("expected final done event, got %#v", publisher.events)
	}
}

func TestSendFilePublishesMeaningfulIntermediateTelemetryUnderThrottle(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.10:19090", time.Now().UTC())

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: &slowPeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-meaningful-progress",
				State:      "done",
			},
		},
		Events: publisher,
	})

	if _, err := svc.SendFile(
		context.Background(),
		"peer-6",
		"hello.txt",
		32,
		&delayedChunkReader{
			payload: bytes.Repeat([]byte("a"), 32),
			step:    8,
			delay:   10 * time.Millisecond,
		},
	); err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}

	foundMeaningfulIntermediateSnapshot := false
	for _, event := range publisher.events {
		if event.kind != "transfer.updated" {
			continue
		}
		snapshot, ok := event.payload.(TransferSnapshot)
		if !ok || snapshot.State != "sending" {
			continue
		}
		if snapshot.RateBytesPerSec > 0 && snapshot.EtaSeconds != nil && snapshot.BytesTransferred < snapshot.FileSize {
			foundMeaningfulIntermediateSnapshot = true
			break
		}
	}
	if !foundMeaningfulIntermediateSnapshot {
		t.Fatalf("expected sending snapshot to expose positive rate and eta before completion, got %#v", publisher.events)
	}
}

func TestSendFilePersistsOutgoingFileMessageAsSent(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.10:19090", time.Now().UTC())

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: &fakePeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-sent",
				State:      "done",
			},
		},
	})

	if _, err := svc.SendFile(
		context.Background(),
		"peer-6",
		"deliver.txt",
		int64(len("hello")),
		bytes.NewReader([]byte("hello")),
	); err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}

	conversation, err := db.EnsureConversation("peer-6")
	if err != nil {
		t.Fatalf("ensure conversation: %v", err)
	}
	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Kind != "file" || messages[0].Status != "sent" {
		t.Fatalf("expected stored outgoing file message to be sent, got %#v", messages)
	}
}

func TestSendFileClearsActiveTelemetryWhenOutcomePersistFails(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.10:19090", time.Now().UTC())

	faultyStore := &faultInjectStore{
		DB:                        db,
		persistTransferOutcomeErr: errors.New("disk full"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     faultyStore,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: &fakePeerTransport{
			fileResponse: protocol.FileTransferResponse{
				TransferID: "transfer-sent",
				State:      "done",
			},
		},
	})

	transferSnapshot, err := svc.SendFile(
		context.Background(),
		"peer-6",
		"deliver.txt",
		int64(len("hello")),
		bytes.NewReader([]byte("hello")),
	)
	if err != nil {
		t.Fatalf("expected send file to stay successful when only local outcome persistence fails, got %v", err)
	}
	if transferSnapshot.State != "done" {
		t.Fatalf("expected transfer snapshot to keep done state, got %#v", transferSnapshot)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Transfers) != 1 || bootstrap.Transfers[0].Active {
		t.Fatalf("expected inactive transfer after persist failure, got %#v", bootstrap.Transfers)
	}
	if bootstrap.Transfers[0].State != "done" {
		t.Fatalf("expected runtime override to keep done transfer state, got %#v", bootstrap.Transfers[0])
	}

	conversation, err := db.EnsureConversation("peer-6")
	if err != nil {
		t.Fatalf("ensure conversation: %v", err)
	}
	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Status != "sending" {
		t.Fatalf("expected message status to stay sending when outcome persistence fails, got %#v", messages)
	}
}

func TestSendFileUsesMultipartSessionForLargeFiles(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-large",
		DeviceName:        "peer-large",
		PinnedFingerprint: "fingerprint-large",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-large",
			DeviceName:      "peer-large",
			AgentTCPPort:    19090,
		},
		"127.0.0.1:19090",
		time.Now().UTC(),
	)

	transport := &fakePeerTransport{
		sessionStartResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-large",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v1",
		},
		sessionCompleteResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-large",
			State:      "done",
		},
		requireLiveSessionCompleteContext: true,
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
	})

	fileSize := int64(multipartThreshold + 1)
	transferSnapshot, err := svc.SendFile(
		context.Background(),
		"peer-large",
		"large.bin",
		fileSize,
		&repeatingByteReader{remaining: fileSize, value: 'a'},
	)
	if err != nil {
		t.Fatalf("send file: %v", err)
	}
	if transferSnapshot.State != "done" {
		t.Fatalf("unexpected transfer snapshot: %#v", transferSnapshot)
	}
	if transport.sendFileCalls != 0 {
		t.Fatalf("expected single-stream path to stay unused, got %d calls", transport.sendFileCalls)
	}
	if transport.sessionStartCalls != 1 || transport.sessionCompleteCalls != 1 {
		t.Fatalf("unexpected session transport call counts: start=%d complete=%d", transport.sessionStartCalls, transport.sessionCompleteCalls)
	}
	if len(transport.uploadedParts) < 2 {
		t.Fatalf("expected multipart uploads, got %#v", transport.uploadedParts)
	}
}

func TestSendFileMultipartFailureStateMarksMessageFailed(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-large-fail",
		DeviceName:        "peer-large-fail",
		PinnedFingerprint: "fingerprint-large-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-large-fail",
			DeviceName:      "peer-large-fail",
			AgentTCPPort:    19090,
		},
		"127.0.0.1:19090",
		time.Now().UTC(),
	)

	transport := &fakePeerTransport{
		sessionStartResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-large-fail",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v1",
		},
		sessionCompleteResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-large-fail",
			State:      transfer.StateFailed,
		},
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
	})

	fileSize := int64(multipartThreshold + 1)
	transferSnapshot, err := svc.SendFile(
		context.Background(),
		"peer-large-fail",
		"large.bin",
		fileSize,
		&repeatingByteReader{remaining: fileSize, value: 'a'},
	)
	if err != nil {
		t.Fatalf("send file: %v", err)
	}
	if transferSnapshot.State != transfer.StateFailed {
		t.Fatalf("expected transfer snapshot to keep failed state, got %#v", transferSnapshot)
	}

	conversation, err := db.EnsureConversation("peer-large-fail")
	if err != nil {
		t.Fatalf("ensure conversation: %v", err)
	}
	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Status != "failed" {
		t.Fatalf("expected outgoing file message to be failed when remote complete fails, got %#v", messages)
	}

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if len(transfers) != 1 || transfers[0].State != transfer.StateFailed {
		t.Fatalf("expected persisted transfer to keep failed state when remote complete fails, got %#v", transfers)
	}
}

func TestMultipartTransferPublishesSingleAggregatedTransferSnapshot(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-large",
		DeviceName:        "peer-large",
		PinnedFingerprint: "fingerprint-large",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-large",
			DeviceName:      "peer-large",
			AgentTCPPort:    19090,
		},
		"127.0.0.1:19090",
		time.Now().UTC(),
	)

	publisher := &capturingEventPublisher{}
	transport := &fakePeerTransport{
		sessionStartResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-large",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v1",
		},
		sessionCompleteResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-large",
			State:      "done",
		},
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
		Transport: transport,
	})

	fileSize := int64(multipartThreshold + 1)
	snapshot, err := svc.SendFile(
		context.Background(),
		"peer-large",
		"large.bin",
		fileSize,
		&repeatingByteReader{remaining: fileSize, value: 'a'},
	)
	if err != nil {
		t.Fatalf("send file: %v", err)
	}

	transferIDs := make(map[string]struct{})
	for _, event := range publisher.events {
		if event.kind != "transfer.updated" {
			continue
		}
		transferSnapshot, ok := event.payload.(TransferSnapshot)
		if !ok {
			continue
		}
		transferIDs[transferSnapshot.TransferID] = struct{}{}
	}
	if len(transferIDs) != 1 {
		t.Fatalf("expected a single aggregated transfer id, got %#v", transferIDs)
	}
	if _, ok := transferIDs[snapshot.TransferID]; !ok {
		t.Fatalf("expected published transfer id %s, got %#v", snapshot.TransferID, transferIDs)
	}
}

func TestMultipartTransferTerminalEventIsAlwaysPublished(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-large",
		DeviceName:        "peer-large",
		PinnedFingerprint: "fingerprint-large",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-large",
			DeviceName:      "peer-large",
			AgentTCPPort:    19090,
		},
		"127.0.0.1:19090",
		time.Now().UTC(),
	)

	publisher := &capturingEventPublisher{}
	transport := &fakePeerTransport{
		sessionStartResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-large",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        4,
			AdaptivePolicyVersion: "v1",
		},
		sessionCompleteResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-large",
			State:      "done",
		},
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
		Transport: transport,
	})

	fileSize := int64(multipartThreshold + 1)
	if _, err := svc.SendFile(
		context.Background(),
		"peer-large",
		"large.bin",
		fileSize,
		&repeatingByteReader{remaining: fileSize, value: 'a'},
	); err != nil {
		t.Fatalf("send file: %v", err)
	}

	if !publisher.HasTransferState("done") {
		t.Fatalf("expected final done transfer.updated event, got %#v", publisher.events)
	}
}

func TestSendFileMultipartUsesSuggestedParallelism(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-wide",
		DeviceName:        "peer-wide",
		PinnedFingerprint: "fingerprint-wide",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(
		discovery.Announcement{
			ProtocolVersion: "1",
			DeviceID:        "peer-wide",
			DeviceName:      "peer-wide",
			AgentTCPPort:    19090,
		},
		"127.0.0.1:19090",
		time.Now().UTC(),
	)

	transport := &fakePeerTransport{
		sessionStartResponse: protocol.TransferSessionStartResponse{
			SessionID:             "session-wide",
			ChunkSize:             1 << 20,
			InitialParallelism:    6,
			MaxParallelism:        6,
			AdaptivePolicyVersion: "v1",
		},
		sessionCompleteResponse: protocol.TransferSessionCompleteResponse{
			TransferID: "transfer-wide",
			State:      "done",
		},
		uploadPartDelay: 150 * time.Millisecond,
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
	})

	fileSize := int64(12 * 8 << 20)
	if _, err := svc.SendFile(
		context.Background(),
		"peer-wide",
		"large.bin",
		fileSize,
		bytes.NewReader(make([]byte, int(fileSize))),
	); err != nil {
		t.Fatalf("send file: %v", err)
	}

	if transport.maxActivePartUploads < 6 {
		t.Fatalf("expected sender to honor suggested initial parallelism, got max active uploads %d", transport.maxActivePartUploads)
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

func TestIncomingTransferSessionLifecyclePersistsDoneFile(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	payload := []byte("hello world")
	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-1",
		MessageID:      "msg-session-1",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		FileSHA256:     appSessionTestSHA256Hex(payload),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}
	if startResponse.SessionID == "" {
		t.Fatal("expected session id")
	}

	if _, err := svc.AcceptIncomingTransferPart(context.Background(), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-1",
		PartIndex:  1,
		Offset:     6,
		Length:     int64(len("world")),
	}, bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("write part 1: %v", err)
	}
	if _, err := svc.AcceptIncomingTransferPart(context.Background(), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-1",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello ")),
	}, bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("write part 0: %v", err)
	}

	completeResponse, err := svc.CompleteIncomingTransferSession(context.Background(), protocol.TransferSessionCompleteRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-1",
		TotalSize:  int64(len(payload)),
		PartCount:  2,
		FileSHA256: appSessionTestSHA256Hex(payload),
	})
	if err != nil {
		t.Fatalf("complete incoming transfer session: %v", err)
	}
	if completeResponse.State != "done" {
		t.Fatalf("unexpected complete response: %#v", completeResponse)
	}

	files, err := os.ReadDir(cfg.DefaultDownloadDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one completed file, got %#v", files)
	}
	content, err := os.ReadFile(filepath.Join(cfg.DefaultDownloadDir, files[0].Name()))
	if err != nil {
		t.Fatalf("read completed file: %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("unexpected completed content: %q", string(content))
	}
}

func TestStartIncomingTransferSessionReturnsAdaptiveChunkProfile(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-profile",
		DeviceName:        "peer-profile",
		PinnedFingerprint: "fingerprint-profile",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	fileSize := int64(64 << 20)
	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-profile",
		MessageID:      "msg-profile",
		SenderDeviceID: "peer-profile",
		FileName:       "large.bin",
		FileSize:       fileSize,
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}
	defer svc.expireIncomingTransferSession(startResponse.SessionID)
	if startResponse.ChunkSize >= transfer.DefaultSessionChunkSize {
		t.Fatalf("expected adaptive chunk size smaller than %d, got %d", transfer.DefaultSessionChunkSize, startResponse.ChunkSize)
	}
	if startResponse.InitialParallelism < transfer.DefaultSessionInitialParallelism {
		t.Fatalf(
			"expected initial parallelism >= %d, got %d",
			transfer.DefaultSessionInitialParallelism,
			startResponse.InitialParallelism,
		)
	}
	partCount := int((fileSize + startResponse.ChunkSize - 1) / startResponse.ChunkSize)
	if partCount < startResponse.InitialParallelism*8 {
		t.Fatalf(
			"expected adaptive chunk profile to keep at least 8 waves, got partCount=%d initial=%d chunkSize=%d",
			partCount,
			startResponse.InitialParallelism,
			startResponse.ChunkSize,
		)
	}
}

func TestAcceptIncomingTransferPartFailureCleansSessionAndTempFile(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-fail",
		MessageID:      "msg-session-fail",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}

	sessionState, ok := svc.getIncomingTransferSession(startResponse.SessionID)
	if !ok {
		t.Fatal("expected active session state")
	}
	_ = sessionState
	tempPath := findSingleTempPartPath(t, cfg.DefaultDownloadDir)

	if _, err := svc.AcceptIncomingTransferPart(context.Background(), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-fail",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello")),
	}, bytes.NewReader([]byte("he"))); err == nil {
		t.Fatal("expected short part payload to fail")
	}

	if _, ok := svc.getIncomingTransferSession(startResponse.SessionID); ok {
		t.Fatal("expected failed session to be removed")
	}
	if _, ok := svc.transfers.Snapshot("transfer-session-fail", time.Now().UTC()); ok {
		t.Fatal("expected failed session telemetry to be cleared")
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed, got err=%v", err)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Transfers) != 1 {
		t.Fatalf("expected one transfer snapshot, got %#v", bootstrap.Transfers)
	}
	if bootstrap.Transfers[0].State != "failed" || bootstrap.Transfers[0].Active {
		t.Fatalf("expected failed inactive transfer snapshot, got %#v", bootstrap.Transfers[0])
	}
}

func TestAcceptIncomingTransferPartFailureRecoversPeerReachability(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-part-fail",
		DeviceName:        "peer-part-fail",
		PinnedFingerprint: "fingerprint-part-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-part-fail",
		DeviceName:      "peer-part-fail",
		AgentTCPPort:    19090,
	}, "192.168.1.40:19090", time.Now().Add(-10*time.Second))
	registry.MarkReachable("peer-part-fail", false)

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
	})

	startResponse, err := svc.StartIncomingTransferSession(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.99:54321"},
	), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-part-fail",
		MessageID:      "msg-part-fail",
		SenderDeviceID: "peer-part-fail",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}

	if _, err := svc.AcceptIncomingTransferPart(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.99:54321"},
	), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-part-fail",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello")),
	}, bytes.NewReader([]byte("he"))); err == nil {
		t.Fatal("expected short part payload to fail")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected failed part to still recover peer reachability, got %#v", bootstrap.Peers)
	}
	if bootstrap.Peers[0].LastKnownAddr != "192.168.1.99:19090" {
		t.Fatalf("expected failed part to learn direct endpoint, got %#v", bootstrap.Peers[0])
	}
}

func TestAcceptIncomingTransferPartTreatsDuplicateCommittedPartAsIdempotent(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	payload := []byte("hello world")
	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-idempotent",
		MessageID:      "msg-session-idempotent",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		FileSHA256:     appSessionTestSHA256Hex(payload),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}

	part0 := protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-idempotent",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello ")),
	}
	if _, err := svc.AcceptIncomingTransferPart(context.Background(), part0, bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("write part 0: %v", err)
	}
	if _, err := svc.AcceptIncomingTransferPart(context.Background(), part0, bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("expected duplicate committed part to be idempotent, got %v", err)
	}
	if _, ok := svc.getIncomingTransferSession(startResponse.SessionID); !ok {
		t.Fatal("expected session to remain active after duplicate committed part")
	}

	if _, err := svc.AcceptIncomingTransferPart(context.Background(), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-idempotent",
		PartIndex:  1,
		Offset:     6,
		Length:     int64(len("world")),
	}, bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("write part 1: %v", err)
	}

	completeResponse, err := svc.CompleteIncomingTransferSession(context.Background(), protocol.TransferSessionCompleteRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-idempotent",
		TotalSize:  int64(len(payload)),
		PartCount:  2,
		FileSHA256: appSessionTestSHA256Hex(payload),
	})
	if err != nil {
		t.Fatalf("complete incoming transfer session: %v", err)
	}
	if completeResponse.State != "done" {
		t.Fatalf("unexpected complete response: %#v", completeResponse)
	}
}

func TestIncomingMultipartTransferSkipsRedundantPeerUpdatedEvents(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-10",
		DeviceName:        "peer-ten",
		PinnedFingerprint: "fingerprint-j",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-10",
		DeviceName:      "peer-ten",
		AgentTCPPort:    19090,
	}, "192.168.1.22:19090", time.Now().UTC())

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	payload := []byte("hello world")
	startResponse, err := svc.StartIncomingTransferSession(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.22:19090"},
	), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-peer-events",
		MessageID:      "msg-session-peer-events",
		SenderDeviceID: "peer-10",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		FileSHA256:     appSessionTestSHA256Hex(payload),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}

	if _, err := svc.AcceptIncomingTransferPart(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.22:19090"},
	), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-peer-events",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello ")),
	}, bytes.NewReader([]byte("hello "))); err != nil {
		t.Fatalf("write part 0: %v", err)
	}
	if _, err := svc.AcceptIncomingTransferPart(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.22:19090"},
	), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-peer-events",
		PartIndex:  1,
		Offset:     6,
		Length:     int64(len("world")),
	}, bytes.NewReader([]byte("world"))); err != nil {
		t.Fatalf("write part 1: %v", err)
	}

	if _, err := svc.CompleteIncomingTransferSession(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.22:19090"},
	), protocol.TransferSessionCompleteRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-session-peer-events",
		TotalSize:  int64(len(payload)),
		PartCount:  2,
		FileSHA256: appSessionTestSHA256Hex(payload),
	}); err != nil {
		t.Fatalf("complete incoming transfer session: %v", err)
	}

	if got := publisher.CountKind("peer.updated"); got != 0 {
		t.Fatalf("expected no redundant peer.updated events for unchanged reachable peer, got %d events: %#v", got, publisher.events)
	}
}

func TestIncomingTransferSessionExpiresAfterIdleTimeout(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:                         cfg,
		Store:                          db,
		Discovery:                      discovery.NewRegistry(),
		Pairings:                       session.NewService(),
		IncomingTransferSessionTimeout: 30 * time.Millisecond,
	})

	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-timeout",
		MessageID:      "msg-session-timeout",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}

	sessionState, ok := svc.getIncomingTransferSession(startResponse.SessionID)
	if !ok {
		t.Fatal("expected active session state")
	}
	_ = sessionState
	tempPath := findSingleTempPartPath(t, cfg.DefaultDownloadDir)

	waitForCondition(t, 500*time.Millisecond, func() bool {
		_, ok := svc.getIncomingTransferSession(startResponse.SessionID)
		return !ok
	})

	if _, ok := svc.transfers.Snapshot("transfer-session-timeout", time.Now().UTC()); ok {
		t.Fatal("expected expired session telemetry to be cleared")
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed after timeout, got err=%v", err)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Transfers) != 1 {
		t.Fatalf("expected one transfer snapshot, got %#v", bootstrap.Transfers)
	}
	if bootstrap.Transfers[0].State != "failed" || bootstrap.Transfers[0].Active {
		t.Fatalf("expected expired transfer to become failed and inactive, got %#v", bootstrap.Transfers[0])
	}
}

func TestCompleteIncomingTransferSessionFailureMarksTransferFailedAndCleansSession(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-complete-fail",
		DeviceName:        "peer-complete-fail",
		PinnedFingerprint: "fingerprint-complete-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	payload := []byte("hello world")
	startResponse, err := svc.StartIncomingTransferSession(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.30:54321"},
	), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-complete-fail",
		MessageID:      "msg-complete-fail",
		SenderDeviceID: "peer-complete-fail",
		FileName:       "hello.txt",
		FileSize:       int64(len(payload)),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}

	if _, err := svc.AcceptIncomingTransferPart(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.30:54321"},
	), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-complete-fail",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len(payload)),
	}, bytes.NewReader(payload)); err != nil {
		t.Fatalf("write part: %v", err)
	}

	if _, err := svc.CompleteIncomingTransferSession(protocol.ContextWithPeerCaller(
		context.Background(),
		protocol.PeerCaller{RemoteAddr: "192.168.1.30:54321"},
	), protocol.TransferSessionCompleteRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-complete-fail",
		TotalSize:  int64(len(payload)),
		PartCount:  1,
		FileSHA256: appSessionTestSHA256Hex([]byte("corrupted")),
	}); err == nil {
		t.Fatal("expected checksum mismatch to fail complete")
	}

	if _, ok := svc.getIncomingTransferSession(startResponse.SessionID); ok {
		t.Fatal("expected failed complete to remove session")
	}
	if _, ok := svc.transfers.Snapshot("transfer-complete-fail", time.Now().UTC()); ok {
		t.Fatal("expected failed complete to clear telemetry")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Transfers) != 1 {
		t.Fatalf("expected one transfer snapshot, got %#v", bootstrap.Transfers)
	}
	if bootstrap.Transfers[0].State != "failed" || bootstrap.Transfers[0].Active {
		t.Fatalf("expected failed inactive transfer snapshot, got %#v", bootstrap.Transfers[0])
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected complete failure to still preserve direct reachability, got %#v", bootstrap.Peers)
	}
}

func TestAcceptIncomingFileTransferPublishesReceivingSnapshot(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	if _, err := svc.AcceptIncomingFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:       "transfer-2",
		MessageID:        "msg-file-2",
		SenderDeviceID:   "peer-7",
		FileName:         "hello.txt",
		FileSize:         32,
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}, bytes.NewReader(bytes.Repeat([]byte("b"), 32))); err != nil {
		t.Fatalf("unexpected incoming file error: %v", err)
	}

	if !publisher.HasTransferState("receiving") {
		t.Fatalf("expected receiving transfer.updated event, got %#v", publisher.events)
	}
	if !publisher.HasTransferState("done") {
		t.Fatalf("expected final done transfer.updated event, got %#v", publisher.events)
	}
}

func TestAcceptIncomingFileTransferUsesLargeCopyBuffer(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	response, err := svc.AcceptIncomingFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:       "transfer-buffer",
		MessageID:        "msg-buffer",
		SenderDeviceID:   "peer-7",
		FileName:         "hello.txt",
		FileSize:         5,
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}, &minimumReadBufferReader{
		remaining:     []byte("hello"),
		minBufferSize: 128 << 10,
	})
	if err != nil {
		t.Fatalf("expected incoming file transfer to use large copy buffer, got %v", err)
	}
	if response.TransferID != "transfer-buffer" || response.State != "done" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestAcceptIncomingFileTransferThrottlesIntermediateTransferSnapshots(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	if _, err := svc.AcceptIncomingFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:       "transfer-throttled",
		MessageID:        "msg-throttled",
		SenderDeviceID:   "peer-7",
		FileName:         "hello.txt",
		FileSize:         1 << 20,
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}, bytes.NewReader(make([]byte, 1<<20))); err != nil {
		t.Fatalf("unexpected incoming file error: %v", err)
	}

	if got := publisher.CountTransferState("receiving"); got > 3 {
		t.Fatalf("expected receiving events to stay on time cadence for a fast stream, got %d events: %#v", got, publisher.events)
	}
	if !publisher.HasTransferState("done") {
		t.Fatalf("expected final done transfer.updated event, got %#v", publisher.events)
	}
}

func TestAcceptIncomingFileTransferUsesLocalClockForTelemetry(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-g",
		RemoteAddr:  "192.168.1.7:54321",
	})
	if _, err := svc.AcceptIncomingFileTransfer(ctx, protocol.FileTransferRequest{
		TransferID:       "transfer-local-clock",
		MessageID:        "msg-file-local-clock",
		SenderDeviceID:   "peer-7",
		FileName:         "hello.txt",
		FileSize:         32,
		CreatedAtRFC3339: time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339Nano),
		AgentTCPPort:     19090,
	}, &delayedChunkReader{
		payload: bytes.Repeat([]byte("b"), 32),
		step:    8,
		delay:   10 * time.Millisecond,
	}); err != nil {
		t.Fatalf("unexpected incoming file error: %v", err)
	}

	foundIntermediateSnapshot := false
	for _, event := range publisher.events {
		if event.kind != "transfer.updated" {
			continue
		}
		snapshot, ok := event.payload.(TransferSnapshot)
		if !ok || snapshot.State != "receiving" {
			continue
		}
		if snapshot.RateBytesPerSec > 0 && snapshot.EtaSeconds != nil && snapshot.BytesTransferred < snapshot.FileSize {
			foundIntermediateSnapshot = true
			break
		}
	}
	if !foundIntermediateSnapshot {
		t.Fatalf("expected receiving snapshot to use local clock for positive rate and eta, got %#v", publisher.events)
	}
}

func TestAcceptIncomingFileTransferClearsActiveTelemetryWhenOutcomePersistFails(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	faultyStore := &faultInjectStore{
		DB:                        db,
		persistTransferOutcomeErr: errors.New("disk full"),
	}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     faultyStore,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	response, err := svc.AcceptIncomingFileTransfer(context.Background(), protocol.FileTransferRequest{
		TransferID:     "transfer-incoming-fail",
		MessageID:      "msg-incoming-fail",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
	}, bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("expected incoming file response to stay successful after commit, got %v", err)
	}
	if response.State != "done" {
		t.Fatalf("expected done response despite local persistence failure, got %#v", response)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Transfers) != 1 || bootstrap.Transfers[0].Active {
		t.Fatalf("expected inactive incoming transfer after persist failure, got %#v", bootstrap.Transfers)
	}
	if bootstrap.Transfers[0].State != "done" {
		t.Fatalf("expected runtime override to keep done incoming transfer state, got %#v", bootstrap.Transfers[0])
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
	if len(transfers) != 1 {
		t.Fatalf("expected one failed transfer record, got %#v", transfers)
	}
	if transfers[0].State != "failed" || transfers[0].BytesTransferred != int64(len("hello")) {
		t.Fatalf("expected failed transfer with partial progress, got %#v", transfers[0])
	}
}

func TestAcceptIncomingFileTransferFailureRecoversPeerReachability(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-7",
		DeviceName:        "peer-seven",
		PinnedFingerprint: "fingerprint-g",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-7",
		DeviceName:      "peer-seven",
		AgentTCPPort:    19090,
	}, "192.168.1.20:19090", time.Now().Add(-10*time.Second))
	registry.MarkReachable("peer-7", false)

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-g",
		RemoteAddr:  "192.168.1.88:54321",
	})
	if _, err := svc.AcceptIncomingFileTransfer(ctx, protocol.FileTransferRequest{
		TransferID:     "transfer-failure-reachable",
		MessageID:      "msg-file-failure-reachable",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello") + 5),
		AgentTCPPort:   19090,
	}, bytes.NewReader([]byte("hello"))); err == nil {
		t.Fatal("expected size mismatch error")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected failed incoming file to still recover peer reachability, got %#v", bootstrap.Peers)
	}
	if bootstrap.Peers[0].LastKnownAddr != "192.168.1.88:19090" {
		t.Fatalf("expected failed incoming file to learn direct endpoint, got %#v", bootstrap.Peers[0])
	}
}

func TestAcceptIncomingFileTransferSetupFailureStillRecoversPeerReachability(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-file-setup-fail",
		DeviceName:        "peer-file-setup-fail",
		PinnedFingerprint: "fingerprint-file-setup-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-file-setup-fail",
		DeviceName:      "peer-file-setup-fail",
		AgentTCPPort:    19090,
	}, "192.168.1.21:19090", time.Now().Add(-10*time.Second))

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config: cfg,
		Store: &faultInjectStore{
			DB:                    db,
			ensureConversationErr: errors.New("ensure conversation failed"),
		},
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-file-setup-fail",
		RemoteAddr:  "192.168.1.89:54321",
	})
	if _, err := svc.AcceptIncomingFileTransfer(ctx, protocol.FileTransferRequest{
		TransferID:     "transfer-file-setup-fail",
		MessageID:      "msg-file-setup-fail",
		SenderDeviceID: "peer-file-setup-fail",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
		AgentTCPPort:   19090,
	}, bytes.NewReader([]byte("hello"))); err == nil {
		t.Fatal("expected incoming file setup failure")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected file setup failure to still recover peer reachability, got %#v", bootstrap.Peers)
	}
	if bootstrap.Peers[0].LastKnownAddr != "192.168.1.89:19090" {
		t.Fatalf("expected file setup failure to learn direct endpoint, got %#v", bootstrap.Peers[0])
	}
	if got := publisher.CountKind("peer.updated"); got != 1 {
		t.Fatalf("expected one peer.updated event after failed incoming file setup, got %d events: %#v", got, publisher.events)
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

func TestAuthorizeTransferSessionStartRejectsFingerprintMismatchForTrustedPeer(t *testing.T) {
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
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	err = svc.AuthorizeTransferSessionStart(context.Background(), protocol.TransferSessionStartRequest{
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

func TestAuthorizeTransferPartRejectsFingerprintMismatchForTrustedPeer(t *testing.T) {
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
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-auth-part",
		MessageID:      "msg-auth-part",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}
	t.Cleanup(func() {
		if sessionState, ok := svc.getIncomingTransferSession(startResponse.SessionID); ok {
			_ = sessionState.receiver.Cleanup()
			svc.deleteIncomingTransferSession(startResponse.SessionID)
			svc.transfers.Finish("transfer-auth-part")
		}
	})

	err = svc.AuthorizeTransferPart(context.Background(), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-auth-part",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello")),
	}, protocol.PeerCaller{Fingerprint: "fingerprint-other"})
	if !errors.Is(err, protocol.ErrPeerForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func TestAuthorizeTransferPartRejectsRawUploadWhenSessionDidNotNegotiateFastPath(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.DefaultDownloadDir = t.TempDir()

	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-raw-auth",
		DeviceName:        "peer-raw-auth",
		PinnedFingerprint: "fingerprint-raw-auth",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-raw-auth",
		MessageID:      "msg-raw-auth",
		SenderDeviceID: "peer-raw-auth",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}
	sessionState, ok := svc.getIncomingTransferSession(startResponse.SessionID)
	if !ok {
		t.Fatal("expected active session state")
	}
	sessionState.adaptivePolicyVersion = "v1"
	t.Cleanup(func() {
		if sessionState, ok := svc.getIncomingTransferSession(startResponse.SessionID); ok {
			_ = sessionState.receiver.Cleanup()
			svc.deleteIncomingTransferSession(startResponse.SessionID)
			svc.transfers.Finish("transfer-raw-auth")
		}
	})

	err = svc.AuthorizeTransferPart(context.Background(), protocol.TransferPartRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-raw-auth",
		PartIndex:  0,
		Offset:     0,
		Length:     int64(len("hello")),
		RawBody:    true,
	}, protocol.PeerCaller{Fingerprint: "fingerprint-raw-auth"})
	if !errors.Is(err, protocol.ErrPeerForbidden) {
		t.Fatalf("expected raw upload without fast negotiation to be forbidden, got %v", err)
	}
}

func TestStartIncomingTransferSessionFailureStillRecoversPeerReachability(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-session-setup-fail",
		DeviceName:        "peer-session-setup-fail",
		PinnedFingerprint: "fingerprint-session-setup-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-session-setup-fail",
		DeviceName:      "peer-session-setup-fail",
		AgentTCPPort:    19090,
	}, "192.168.1.22:19090", time.Now().Add(-10*time.Second))

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config: cfg,
		Store: &faultInjectStore{
			DB:                         db,
			saveMessageWithTransferErr: errors.New("save message with transfer failed"),
		},
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-session-setup-fail",
		RemoteAddr:  "192.168.1.90:54321",
	})
	if _, err := svc.StartIncomingTransferSession(ctx, protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-setup-fail",
		MessageID:      "msg-session-setup-fail",
		SenderDeviceID: "peer-session-setup-fail",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
		AgentTCPPort:   19090,
	}); err == nil {
		t.Fatal("expected incoming transfer session setup failure")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected transfer session setup failure to still recover peer reachability, got %#v", bootstrap.Peers)
	}
	if bootstrap.Peers[0].LastKnownAddr != "192.168.1.90:19090" {
		t.Fatalf("expected transfer session setup failure to learn direct endpoint, got %#v", bootstrap.Peers[0])
	}
	if got := publisher.CountKind("peer.updated"); got != 1 {
		t.Fatalf("expected one peer.updated event after failed incoming transfer session setup, got %d events: %#v", got, publisher.events)
	}
}

func TestStartIncomingTransferSessionValidationFailureStillRecoversPeerReachability(t *testing.T) {
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
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-session-validate-fail",
		DeviceName:        "peer-session-validate-fail",
		PinnedFingerprint: "fingerprint-session-validate-fail",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-session-validate-fail",
		DeviceName:      "peer-session-validate-fail",
		AgentTCPPort:    19090,
	}, "192.168.1.23:19090", time.Now().Add(-10*time.Second))

	publisher := &capturingEventPublisher{}
	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Events:    publisher,
	})

	ctx := protocol.ContextWithPeerCaller(context.Background(), protocol.PeerCaller{
		Fingerprint: "fingerprint-session-validate-fail",
		RemoteAddr:  "192.168.1.91:54321",
	})
	if _, err := svc.StartIncomingTransferSession(ctx, protocol.TransferSessionStartRequest{
		TransferID:     "transfer-session-validate-fail",
		MessageID:      "msg-session-validate-fail",
		SenderDeviceID: "peer-session-validate-fail",
		FileName:       "hello.txt",
		FileSize:       0,
		AgentTCPPort:   19090,
	}); err == nil {
		t.Fatal("expected incoming transfer session validation failure")
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected transfer session validation failure to still recover peer reachability, got %#v", bootstrap.Peers)
	}
	if bootstrap.Peers[0].LastKnownAddr != "192.168.1.91:19090" {
		t.Fatalf("expected transfer session validation failure to learn direct endpoint, got %#v", bootstrap.Peers[0])
	}
	if got := publisher.CountKind("peer.updated"); got != 1 {
		t.Fatalf("expected one peer.updated event after failed incoming transfer session validation, got %d events: %#v", got, publisher.events)
	}
}

func TestAuthorizeTransferSessionCompleteRejectsFingerprintMismatchForTrustedPeer(t *testing.T) {
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
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    cfg,
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	startResponse, err := svc.StartIncomingTransferSession(context.Background(), protocol.TransferSessionStartRequest{
		TransferID:     "transfer-auth-complete",
		MessageID:      "msg-auth-complete",
		SenderDeviceID: "peer-7",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello")),
	})
	if err != nil {
		t.Fatalf("start incoming transfer session: %v", err)
	}
	t.Cleanup(func() {
		if sessionState, ok := svc.getIncomingTransferSession(startResponse.SessionID); ok {
			_ = sessionState.receiver.Cleanup()
			svc.deleteIncomingTransferSession(startResponse.SessionID)
			svc.transfers.Finish("transfer-auth-complete")
		}
	})

	err = svc.AuthorizeTransferSessionComplete(context.Background(), protocol.TransferSessionCompleteRequest{
		SessionID:  startResponse.SessionID,
		TransferID: "transfer-auth-complete",
		PartCount:  1,
		TotalSize:  int64(len("hello")),
	}, protocol.PeerCaller{Fingerprint: "fingerprint-other"})
	if !errors.Is(err, protocol.ErrPeerForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

type fakePeerTransport struct {
	startResponse                     protocol.PairingStartResponse
	confirmResponse                   protocol.PairingConfirmResponse
	ackResponse                       protocol.AckResponse
	fileResponse                      protocol.FileTransferResponse
	heartbeatResponse                 protocol.HeartbeatResponse
	sessionStartResponse              protocol.TransferSessionStartResponse
	sessionCompleteResponse           protocol.TransferSessionCompleteResponse
	confirmPeer                       discovery.PeerRecord
	textPeer                          discovery.PeerRecord
	filePeer                          discovery.PeerRecord
	sendTextErr                       error
	heartbeatPeer                     discovery.PeerRecord
	sendHeartbeatErr                  error
	heartbeatCalls                    int
	sendFileCalls                     int
	sessionStartCalls                 int
	sessionCompleteCalls              int
	uploadedParts                     []protocol.TransferPartRequest
	requireLiveSessionCompleteContext bool
	uploadPartDelay                   time.Duration
	activePartUploads                 int
	maxActivePartUploads              int
	uploadMu                          sync.Mutex
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
	f.sendFileCalls++
	return f.fileResponse, nil
}

func (f *fakePeerTransport) SendHeartbeat(
	_ context.Context,
	peer discovery.PeerRecord,
	_ protocol.HeartbeatRequest,
) (protocol.HeartbeatResponse, error) {
	f.heartbeatPeer = peer
	f.heartbeatCalls++
	if f.sendHeartbeatErr != nil {
		return protocol.HeartbeatResponse{}, f.sendHeartbeatErr
	}
	return f.heartbeatResponse, nil
}

func (f *fakePeerTransport) StartTransferSession(
	_ context.Context,
	peer discovery.PeerRecord,
	_ protocol.TransferSessionStartRequest,
) (protocol.TransferSessionStartResponse, error) {
	f.filePeer = peer
	f.sessionStartCalls++
	return f.sessionStartResponse, nil
}

func (f *fakePeerTransport) UploadTransferPart(
	_ context.Context,
	peer discovery.PeerRecord,
	request protocol.TransferPartRequest,
	content io.Reader,
) (protocol.TransferPartResponse, error) {
	f.filePeer = peer
	f.uploadMu.Lock()
	f.activePartUploads++
	if f.activePartUploads > f.maxActivePartUploads {
		f.maxActivePartUploads = f.activePartUploads
	}
	f.uploadMu.Unlock()
	defer func() {
		f.uploadMu.Lock()
		if f.activePartUploads > 0 {
			f.activePartUploads--
		}
		f.uploadMu.Unlock()
	}()
	if f.uploadPartDelay > 0 {
		time.Sleep(f.uploadPartDelay)
	}
	if _, err := io.ReadAll(content); err != nil {
		return protocol.TransferPartResponse{}, err
	}
	f.uploadedParts = append(f.uploadedParts, request)
	return protocol.TransferPartResponse{
		SessionID:     request.SessionID,
		PartIndex:     request.PartIndex,
		BytesWritten:  request.Length,
		BytesReceived: request.Length,
	}, nil
}

func (f *fakePeerTransport) CompleteTransferSession(
	ctx context.Context,
	peer discovery.PeerRecord,
	_ protocol.TransferSessionCompleteRequest,
) (protocol.TransferSessionCompleteResponse, error) {
	if f.requireLiveSessionCompleteContext && ctx.Err() != nil {
		return protocol.TransferSessionCompleteResponse{}, ctx.Err()
	}
	f.filePeer = peer
	f.sessionCompleteCalls++
	return f.sessionCompleteResponse, nil
}

type capturedEvent struct {
	kind    string
	payload any
}

type capturingEventPublisher struct {
	events []capturedEvent
}

func (c *capturingEventPublisher) Publish(kind string, payload any) {
	c.events = append(c.events, capturedEvent{kind: kind, payload: payload})
}

func (c *capturingEventPublisher) HasTransferState(state string) bool {
	for _, event := range c.events {
		if event.kind != "transfer.updated" {
			continue
		}
		snapshot, ok := event.payload.(TransferSnapshot)
		if ok && snapshot.State == state {
			return true
		}
	}
	return false
}

func (c *capturingEventPublisher) LastTransferSnapshotByState(state string) (TransferSnapshot, bool) {
	for index := len(c.events) - 1; index >= 0; index-- {
		event := c.events[index]
		if event.kind != "transfer.updated" {
			continue
		}
		snapshot, ok := event.payload.(TransferSnapshot)
		if ok && snapshot.State == state {
			return snapshot, true
		}
	}
	return TransferSnapshot{}, false
}

func (c *capturingEventPublisher) CountTransferState(state string) int {
	count := 0
	for _, event := range c.events {
		if event.kind != "transfer.updated" {
			continue
		}
		snapshot, ok := event.payload.(TransferSnapshot)
		if ok && snapshot.State == state {
			count++
		}
	}
	return count
}

func (c *capturingEventPublisher) CountKind(kind string) int {
	count := 0
	for _, event := range c.events {
		if event.kind == kind {
			count++
		}
	}
	return count
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("condition not met before timeout")
}

type slowPeerTransport struct {
	fileResponse protocol.FileTransferResponse
}

type delayedChunkReader struct {
	payload []byte
	offset  int
	step    int
	delay   time.Duration
}

func (r *delayedChunkReader) Read(buffer []byte) (int, error) {
	if r.offset >= len(r.payload) {
		return 0, io.EOF
	}
	if r.delay > 0 && r.offset > 0 {
		time.Sleep(r.delay)
	}
	limit := len(r.payload) - r.offset
	if r.step > 0 && r.step < limit {
		limit = r.step
	}
	if limit > len(buffer) {
		limit = len(buffer)
	}
	copy(buffer[:limit], r.payload[r.offset:r.offset+limit])
	r.offset += limit
	return limit, nil
}

type minimumReadBufferReader struct {
	remaining     []byte
	minBufferSize int
}

func (r *minimumReadBufferReader) Read(buffer []byte) (int, error) {
	if len(buffer) < r.minBufferSize {
		return 0, errors.New("copy buffer too small")
	}
	if len(r.remaining) == 0 {
		return 0, io.EOF
	}

	written := copy(buffer, r.remaining)
	r.remaining = r.remaining[written:]
	return written, nil
}

type faultInjectStore struct {
	*store.DB
	ensureConversationErr      error
	saveMessageErr             error
	saveMessageWithTransferErr error
	persistTransferOutcomeErr  error
}

type repeatingByteReader struct {
	remaining int64
	value     byte
}

func (r *repeatingByteReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	limit := int64(len(buffer))
	if limit > r.remaining {
		limit = r.remaining
	}
	for index := int64(0); index < limit; index++ {
		buffer[index] = r.value
	}
	r.remaining -= limit
	return int(limit), nil
}

func (s *faultInjectStore) PersistTransferOutcome(message *domain.Message, transfer domain.Transfer) error {
	if s.persistTransferOutcomeErr != nil {
		return s.persistTransferOutcomeErr
	}
	return s.DB.PersistTransferOutcome(message, transfer)
}

func (s *faultInjectStore) EnsureConversation(peerDeviceID string) (domain.Conversation, error) {
	if s.ensureConversationErr != nil {
		return domain.Conversation{}, s.ensureConversationErr
	}
	return s.DB.EnsureConversation(peerDeviceID)
}

func (s *faultInjectStore) SaveMessage(message domain.Message) error {
	if s.saveMessageErr != nil {
		return s.saveMessageErr
	}
	return s.DB.SaveMessage(message)
}

func (s *faultInjectStore) SaveMessageWithTransfer(message domain.Message, transfer domain.Transfer) error {
	if s.saveMessageWithTransferErr != nil {
		return s.saveMessageWithTransferErr
	}
	return s.DB.SaveMessageWithTransfer(message, transfer)
}

func (s *slowPeerTransport) StartPairing(
	_ context.Context,
	_ discovery.PeerRecord,
	_ protocol.PairingStartRequest,
) (protocol.PairingStartResponse, error) {
	return protocol.PairingStartResponse{}, nil
}

func (s *slowPeerTransport) ConfirmPairing(
	_ context.Context,
	_ discovery.PeerRecord,
	_ protocol.PairingConfirmRequest,
) (protocol.PairingConfirmResponse, error) {
	return protocol.PairingConfirmResponse{}, nil
}

func (s *slowPeerTransport) SendTextMessage(
	_ context.Context,
	_ discovery.PeerRecord,
	_ protocol.TextMessageRequest,
) (protocol.AckResponse, error) {
	return protocol.AckResponse{}, nil
}

func (s *slowPeerTransport) SendHeartbeat(
	_ context.Context,
	_ discovery.PeerRecord,
	_ protocol.HeartbeatRequest,
) (protocol.HeartbeatResponse, error) {
	return protocol.HeartbeatResponse{}, nil
}

func (s *slowPeerTransport) SendFile(
	_ context.Context,
	_ discovery.PeerRecord,
	_ protocol.FileTransferRequest,
	content io.Reader,
) (protocol.FileTransferResponse, error) {
	buffer := make([]byte, 7)
	for {
		_, err := content.Read(buffer)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return protocol.FileTransferResponse{}, err
		}
	}
	return s.fileResponse, nil
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

func findSingleTempPartPath(t *testing.T, dir string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*.part"))
	if err != nil {
		t.Fatalf("glob temp parts: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one temp part file in %s, got %#v", dir, matches)
	}
	return matches[0]
}

func appSessionTestSHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
