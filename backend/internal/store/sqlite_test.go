package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"shareme/backend/internal/domain"
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
	dbPath := filepath.Join(t.TempDir(), "shareme.db")

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
	dbPath := filepath.Join(t.TempDir(), "level1", "level2", "shareme.db")

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

func TestOpenCreatesMessageHistoryPagingIndex(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	row := db.raw.QueryRow(`select count(*) from sqlite_master where type='index' and name='idx_messages_conversation_created_message'`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("scan index count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected paging index to exist, got count=%d", count)
	}
}

func TestListMessagesPageUsesStableCursorBoundary(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	conversation, err := db.EnsureConversation("peer-paging")
	if err != nil {
		t.Fatalf("ensure conversation: %v", err)
	}

	createdAt := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 11; i++ {
		if err := db.SaveMessage(domain.Message{
			MessageID:      fmt.Sprintf("msg-%02d", i),
			ConversationID: conversation.ConversationID,
			Direction:      "incoming",
			Kind:           "text",
			Body:           fmt.Sprintf("body-%02d", i),
			Status:         "sent",
			CreatedAt:      createdAt,
		}); err != nil {
			t.Fatalf("save message %d: %v", i, err)
		}
	}

	page, hasMore, nextBoundary, err := db.ListMessagesPage(conversation.ConversationID, domain.MessageBoundary{}, 10)
	if err != nil {
		t.Fatalf("ListMessagesPage() error = %v", err)
	}
	if !hasMore {
		t.Fatalf("expected more history after first page")
	}
	if len(page) != 10 {
		t.Fatalf("expected first page to contain 10 messages, got %d", len(page))
	}
	if page[0].MessageID != "msg-01" || page[9].MessageID != "msg-10" {
		t.Fatalf("unexpected first page window: %#v", page)
	}
	if nextBoundary.MessageID != "msg-01" || !nextBoundary.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected next boundary: %#v", nextBoundary)
	}

	olderPage, hasMoreOlder, olderBoundary, err := db.ListMessagesPage(conversation.ConversationID, nextBoundary, 10)
	if err != nil {
		t.Fatalf("ListMessagesPage() older error = %v", err)
	}
	if hasMoreOlder {
		t.Fatalf("expected second page to exhaust history")
	}
	if len(olderPage) != 1 || olderPage[0].MessageID != "msg-00" {
		t.Fatalf("unexpected older page: %#v", olderPage)
	}
	if !olderBoundary.CreatedAt.IsZero() || olderBoundary.MessageID != "" {
		t.Fatalf("expected empty boundary when history exhausted, got %#v", olderBoundary)
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

func TestSaveTransferPersistsDirectionAndProgress(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	transfer := domain.Transfer{
		TransferID:       "transfer-progress",
		MessageID:        "msg-progress",
		FileName:         "draft.zip",
		FileSize:         16,
		State:            "sending",
		Direction:        "outgoing",
		BytesTransferred: 0,
		ProgressPercent:  0,
		RateBytesPerSec:  0,
		EtaSeconds:       nil,
		CreatedAt:        time.Date(2026, 4, 10, 8, 15, 0, 0, time.UTC),
	}

	if err := db.SaveTransfer(transfer); err != nil {
		t.Fatalf("unexpected save transfer error: %v", err)
	}
	if err := db.UpdateTransferProgress("transfer-progress", "sending", 9); err != nil {
		t.Fatalf("unexpected update transfer progress error: %v", err)
	}

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("unexpected list transfer error: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected one transfer, got %#v", transfers)
	}
	if transfers[0].Direction != "outgoing" || transfers[0].BytesTransferred != 9 {
		t.Fatalf("unexpected persisted transfer progress: %#v", transfers[0])
	}
}

func TestOpenBackfillsLegacyTransferDirectionFromMessage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("unexpected raw open error: %v", err)
	}
	defer raw.Close()

	schema := []string{
		`create table messages (
			message_id text primary key,
			conversation_id text not null,
			direction text not null,
			kind text not null,
			body text not null,
			status text not null,
			created_at text not null
		);`,
		`create table transfers (
			transfer_id text primary key,
			message_id text not null,
			file_name text not null,
			file_size integer not null,
			state text not null,
			created_at text not null
		);`,
	}
	for _, stmt := range schema {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("unexpected schema exec error: %v", err)
		}
	}
	if _, err := raw.Exec(
		`insert into messages (message_id, conversation_id, direction, kind, body, status, created_at)
		 values (?, ?, ?, ?, ?, ?, ?)`,
		"msg-legacy",
		"conv-peer-legacy",
		"incoming",
		"file",
		"hello.txt",
		"sent",
		time.Date(2026, 4, 10, 8, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("unexpected insert message error: %v", err)
	}
	if _, err := raw.Exec(
		`insert into transfers (transfer_id, message_id, file_name, file_size, state, created_at)
		 values (?, ?, ?, ?, ?, ?)`,
		"transfer-legacy",
		"msg-legacy",
		"hello.txt",
		5,
		"done",
		time.Date(2026, 4, 10, 8, 0, 1, 0, time.UTC).Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("unexpected insert transfer error: %v", err)
	}

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("unexpected migrated open error: %v", err)
	}
	defer db.Close()

	transfers, err := db.ListTransfers()
	if err != nil {
		t.Fatalf("unexpected list transfer error: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected one migrated transfer, got %#v", transfers)
	}
	if transfers[0].Direction != "incoming" {
		t.Fatalf("expected migrated transfer direction to follow message direction, got %#v", transfers[0])
	}
}

func TestSaveMessageWithTransferRollsBackWhenTransferInsertFails(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	conversation, err := db.EnsureConversation("peer-atomic")
	if err != nil {
		t.Fatalf("unexpected ensure conversation error: %v", err)
	}
	if err := db.SaveTransfer(domain.Transfer{
		TransferID: "transfer-duplicate",
		MessageID:  "msg-existing",
		FileName:   "existing.txt",
		FileSize:   4,
		State:      "done",
		CreatedAt:  time.Date(2026, 4, 10, 8, 20, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("unexpected seed transfer error: %v", err)
	}

	message := domain.Message{
		MessageID:      "msg-atomic",
		ConversationID: conversation.ConversationID,
		Direction:      "outgoing",
		Kind:           "file",
		Body:           "draft.zip",
		Status:         "sending",
		CreatedAt:      time.Date(2026, 4, 10, 8, 21, 0, 0, time.UTC),
	}
	transfer := domain.Transfer{
		TransferID:       "transfer-duplicate",
		MessageID:        message.MessageID,
		FileName:         "draft.zip",
		FileSize:         8,
		State:            "sending",
		Direction:        "outgoing",
		BytesTransferred: 0,
		CreatedAt:        message.CreatedAt,
	}

	if err := db.SaveMessageWithTransfer(message, transfer); err == nil {
		t.Fatal("expected duplicate transfer insert to fail")
	}

	messages, err := db.ListMessages(conversation.ConversationID)
	if err != nil {
		t.Fatalf("unexpected list message error: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected message insert to roll back with transfer failure, got %#v", messages)
	}
}
