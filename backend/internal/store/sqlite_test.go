package store

import (
	"path/filepath"
	"testing"
	"time"

	"message-share/backend/internal/domain"
)

func TestOpenCreatesCoreTables(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"local_device", "trusted_peers", "conversations", "messages", "transfers"} {
		if !db.HasTable(table) {
			t.Fatalf("expected table %s", table)
		}

		row := db.raw.QueryRow(`select count(*) from sqlite_master where type='table' and name=?`, table)
		var count int
		if err := row.Scan(&count); err != nil {
			t.Fatalf("unexpected scan error for %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("expected sqlite_master to contain %s, got %d", table, count)
		}
	}
}

func TestOpenReusesDiskDatabaseSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "message-share.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("unexpected open error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("unexpected reopen error: %v", err)
	}
	defer reopened.Close()

	for _, table := range []string{"local_device", "trusted_peers", "conversations", "messages", "transfers"} {
		if !reopened.HasTable(table) {
			t.Fatalf("expected reopened database to keep table %s", table)
		}
	}
}

func TestOpenCreatesMissingParentDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "level1", "level2", "message-share.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("expected missing parent directories to be created, got error: %v", err)
	}
	defer db.Close()

	if !db.HasTable("local_device") {
		t.Fatal("expected sqlite schema to be initialized after creating parent directories")
	}
}

func TestSaveAndLoadLocalDevice(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	device := domain.LocalDevice{
		DeviceID:      "device-1",
		DeviceName:    "我的电脑",
		PublicKeyPEM:  "public",
		PrivateKeyPEM: "private",
		CreatedAt:     time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC),
	}

	if err := db.SaveLocalDevice(device); err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	loaded, ok, err := db.LoadLocalDevice()
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if !ok {
		t.Fatal("expected local device to exist")
	}
	if loaded.DeviceID != device.DeviceID || loaded.DeviceName != device.DeviceName {
		t.Fatalf("unexpected local device: %#v", loaded)
	}
}

func TestUpsertAndListTrustedPeers(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	peer := domain.Peer{
		DeviceID:          "peer-1",
		DeviceName:        "会议室电脑",
		PinnedFingerprint: "fingerprint-a",
		RemarkName:        "会议室",
		Trusted:           true,
		UpdatedAt:         time.Date(2026, 4, 10, 8, 5, 0, 0, time.UTC),
	}

	if err := db.UpsertTrustedPeer(peer); err != nil {
		t.Fatalf("unexpected upsert error: %v", err)
	}

	peers, err := db.ListTrustedPeers()
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected one trusted peer, got %#v", peers)
	}
	if peers[0].DeviceID != peer.DeviceID || peers[0].RemarkName != peer.RemarkName || !peers[0].Trusted {
		t.Fatalf("unexpected peer: %#v", peers[0])
	}
}

func TestEnsureConversationAndListMessages(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	conversation, err := db.EnsureConversation("peer-1")
	if err != nil {
		t.Fatalf("unexpected ensure conversation error: %v", err)
	}
	if conversation.PeerDeviceID != "peer-1" {
		t.Fatalf("unexpected conversation: %#v", conversation)
	}

	message := domain.Message{
		MessageID:      "msg-1",
		ConversationID: conversation.ConversationID,
		Direction:      "outgoing",
		Kind:           "text",
		Body:           "hello",
		Status:         "sent",
		CreatedAt:      time.Date(2026, 4, 10, 8, 10, 0, 0, time.UTC),
	}
	if err := db.SaveMessage(message); err != nil {
		t.Fatalf("unexpected save message error: %v", err)
	}

	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("unexpected list message error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %#v", messages)
	}
	if messages[0].MessageID != "msg-1" || messages[0].Direction != "outgoing" || messages[0].Body != "hello" {
		t.Fatalf("unexpected message: %#v", messages[0])
	}

	conversations, err := db.ListConversations()
	if err != nil {
		t.Fatalf("unexpected list conversation error: %v", err)
	}
	if len(conversations) != 1 || conversations[0].ConversationID != conversation.ConversationID {
		t.Fatalf("unexpected conversations: %#v", conversations)
	}
}

func TestSaveTransferAndUpdateState(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	transfer := domain.Transfer{
		TransferID: "transfer-1",
		MessageID:  "msg-1",
		FileName:   "hello.txt",
		FileSize:   5,
		State:      "sending",
		CreatedAt:  time.Date(2026, 4, 10, 8, 12, 0, 0, time.UTC),
	}

	if err := db.SaveTransfer(transfer); err != nil {
		t.Fatalf("unexpected save transfer error: %v", err)
	}
	if err := db.UpdateTransferState("transfer-1", "done"); err != nil {
		t.Fatalf("unexpected update transfer error: %v", err)
	}

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("unexpected list transfer error: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected one transfer, got %#v", transfers)
	}
	if transfers[0].TransferID != "transfer-1" || transfers[0].State != "done" || transfers[0].FileName != "hello.txt" {
		t.Fatalf("unexpected transfer: %#v", transfers[0])
	}
}
