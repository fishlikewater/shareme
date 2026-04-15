package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"message-share/backend/internal/domain"
	_ "modernc.org/sqlite"
)

type DB struct {
	raw *sql.DB
}

func Open(dsn string) (*DB, error) {
	if err := ensureSQLiteParentDir(dsn); err != nil {
		return nil, err
	}

	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if err := raw.Ping(); err != nil {
		_ = raw.Close()
		return nil, err
	}

	schema := []string{
		`create table if not exists local_device (device_id text primary key, device_name text not null, public_key_pem text not null, private_key_pem text not null, created_at text not null);`,
		`create table if not exists trusted_peers (device_id text primary key, device_name text not null, pinned_fingerprint text not null, remark_name text not null default '', trusted integer not null, updated_at text not null);`,
		`create table if not exists conversations (conversation_id text primary key, peer_device_id text not null, updated_at text not null);`,
		`create table if not exists messages (message_id text primary key, conversation_id text not null, direction text not null default 'outgoing', kind text not null, body text not null, status text not null, created_at text not null);`,
		`create table if not exists transfers (transfer_id text primary key, message_id text not null, file_name text not null, file_size integer not null, state text not null, direction text not null default 'outgoing', bytes_transferred integer not null default 0, created_at text not null);`,
	}

	for _, stmt := range schema {
		if _, err := raw.Exec(stmt); err != nil {
			_ = raw.Close()
			return nil, err
		}
	}

	if _, err := raw.Exec(`alter table messages add column direction text not null default 'outgoing'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = raw.Close()
		return nil, err
	}
	if _, err := raw.Exec(`alter table transfers add column direction text not null default 'outgoing'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = raw.Close()
		return nil, err
	}
	if _, err := raw.Exec(`alter table transfers add column bytes_transferred integer not null default 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		_ = raw.Close()
		return nil, err
	}
	if _, err := raw.Exec(`update transfers
		set direction = coalesce(
			(select messages.direction from messages where messages.message_id = transfers.message_id),
			direction,
			'outgoing'
		)`); err != nil {
		_ = raw.Close()
		return nil, err
	}

	return &DB{raw: raw}, nil
}

func ensureSQLiteParentDir(dsn string) error {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return nil
	}

	lower := strings.ToLower(trimmed)
	if lower == ":memory:" || strings.HasPrefix(lower, "file:") {
		return nil
	}

	parentDir := filepath.Dir(trimmed)
	if parentDir == "." || parentDir == "" {
		return nil
	}

	return os.MkdirAll(parentDir, 0o755)
}

func (db *DB) Close() error {
	return db.raw.Close()
}

func (db *DB) HasTable(name string) bool {
	row := db.raw.QueryRow(`select count(*) from sqlite_master where type='table' and name=?`, name)
	var count int
	if err := row.Scan(&count); err != nil {
		return false
	}

	return count == 1
}

func (db *DB) SaveLocalDevice(device domain.LocalDevice) error {
	tx, err := db.raw.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`delete from local_device`); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.Exec(
		`insert into local_device (device_id, device_name, public_key_pem, private_key_pem, created_at) values (?, ?, ?, ?, ?)`,
		device.DeviceID,
		device.DeviceName,
		device.PublicKeyPEM,
		device.PrivateKeyPEM,
		formatTime(device.CreatedAt),
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (db *DB) LoadLocalDevice() (domain.LocalDevice, bool, error) {
	row := db.raw.QueryRow(
		`select device_id, device_name, public_key_pem, private_key_pem, created_at from local_device order by created_at asc limit 1`,
	)

	var createdAt string
	var device domain.LocalDevice
	if err := row.Scan(
		&device.DeviceID,
		&device.DeviceName,
		&device.PublicKeyPEM,
		&device.PrivateKeyPEM,
		&createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return domain.LocalDevice{}, false, nil
		}
		return domain.LocalDevice{}, false, err
	}

	device.CreatedAt = parseTime(createdAt)
	return device, true, nil
}

func (db *DB) UpsertTrustedPeer(peer domain.Peer) error {
	_, err := db.raw.Exec(
		`insert into trusted_peers (device_id, device_name, pinned_fingerprint, remark_name, trusted, updated_at)
		values (?, ?, ?, ?, ?, ?)
		on conflict(device_id) do update set
			device_name = excluded.device_name,
			pinned_fingerprint = excluded.pinned_fingerprint,
			remark_name = excluded.remark_name,
			trusted = excluded.trusted,
			updated_at = excluded.updated_at`,
		peer.DeviceID,
		peer.DeviceName,
		peer.PinnedFingerprint,
		peer.RemarkName,
		boolToInt(peer.Trusted),
		formatTime(peer.UpdatedAt),
	)
	return err
}

func (db *DB) ListTrustedPeers() ([]domain.Peer, error) {
	rows, err := db.raw.Query(
		`select device_id, device_name, pinned_fingerprint, remark_name, trusted, updated_at
		from trusted_peers
		order by device_name asc, device_id asc`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	peers := make([]domain.Peer, 0)
	for rows.Next() {
		var peer domain.Peer
		var trusted int
		var updatedAt string
		if err := rows.Scan(
			&peer.DeviceID,
			&peer.DeviceName,
			&peer.PinnedFingerprint,
			&peer.RemarkName,
			&trusted,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		peer.Trusted = trusted == 1
		peer.UpdatedAt = parseTime(updatedAt)
		peers = append(peers, peer)
	}

	return peers, rows.Err()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func (db *DB) EnsureConversation(peerDeviceID string) (domain.Conversation, error) {
	conversationID := "conv-" + peerDeviceID
	conversation := domain.Conversation{
		ConversationID: conversationID,
		PeerDeviceID:   peerDeviceID,
		UpdatedAt:      time.Now().UTC(),
	}

	_, err := db.raw.Exec(
		`insert into conversations (conversation_id, peer_device_id, updated_at)
		values (?, ?, ?)
		on conflict(conversation_id) do update set updated_at = excluded.updated_at`,
		conversation.ConversationID,
		conversation.PeerDeviceID,
		formatTime(conversation.UpdatedAt),
	)
	if err != nil {
		return domain.Conversation{}, err
	}

	return conversation, nil
}

func (db *DB) ListConversations() ([]domain.Conversation, error) {
	rows, err := db.raw.Query(
		`select conversation_id, peer_device_id, updated_at
		from conversations
		order by updated_at asc, conversation_id asc`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := make([]domain.Conversation, 0)
	for rows.Next() {
		var conversation domain.Conversation
		var updatedAt string
		if err := rows.Scan(
			&conversation.ConversationID,
			&conversation.PeerDeviceID,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		conversation.UpdatedAt = parseTime(updatedAt)
		conversations = append(conversations, conversation)
	}

	return conversations, rows.Err()
}

func (db *DB) SaveMessage(message domain.Message) error {
	_, err := db.raw.Exec(
		`insert into messages (message_id, conversation_id, direction, kind, body, status, created_at)
		values (?, ?, ?, ?, ?, ?, ?)
		on conflict(message_id) do update set
			conversation_id = excluded.conversation_id,
			direction = excluded.direction,
			kind = excluded.kind,
			body = excluded.body,
			status = excluded.status,
			created_at = excluded.created_at`,
		message.MessageID,
		message.ConversationID,
		message.Direction,
		message.Kind,
		message.Body,
		message.Status,
		formatTime(message.CreatedAt),
	)
	return err
}

func (db *DB) SaveMessageWithTransfer(message domain.Message, transfer domain.Transfer) error {
	tx, err := db.raw.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`insert into messages (message_id, conversation_id, direction, kind, body, status, created_at)
		values (?, ?, ?, ?, ?, ?, ?)
		on conflict(message_id) do update set
			conversation_id = excluded.conversation_id,
			direction = excluded.direction,
			kind = excluded.kind,
			body = excluded.body,
			status = excluded.status,
			created_at = excluded.created_at`,
		message.MessageID,
		message.ConversationID,
		message.Direction,
		message.Kind,
		message.Body,
		message.Status,
		formatTime(message.CreatedAt),
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	direction := transfer.Direction
	if strings.TrimSpace(direction) == "" {
		direction = "outgoing"
	}
	if _, err := tx.Exec(
		`insert into transfers (transfer_id, message_id, file_name, file_size, state, direction, bytes_transferred, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)`,
		transfer.TransferID,
		transfer.MessageID,
		transfer.FileName,
		transfer.FileSize,
		transfer.State,
		direction,
		transfer.BytesTransferred,
		formatTime(transfer.CreatedAt),
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (db *DB) ListMessages(conversationID string) ([]domain.Message, error) {
	rows, err := db.raw.Query(
		`select message_id, conversation_id, direction, kind, body, status, created_at
		from messages
		where conversation_id = ?
		order by created_at asc`,
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]domain.Message, 0)
	for rows.Next() {
		var message domain.Message
		var createdAt string
		if err := rows.Scan(
			&message.MessageID,
			&message.ConversationID,
			&message.Direction,
			&message.Kind,
			&message.Body,
			&message.Status,
			&createdAt,
		); err != nil {
			return nil, err
		}
		message.CreatedAt = parseTime(createdAt)
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

func (db *DB) SaveTransfer(transfer domain.Transfer) error {
	direction := transfer.Direction
	if strings.TrimSpace(direction) == "" {
		direction = "outgoing"
	}
	_, err := db.raw.Exec(
		`insert into transfers (transfer_id, message_id, file_name, file_size, state, direction, bytes_transferred, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)`,
		transfer.TransferID,
		transfer.MessageID,
		transfer.FileName,
		transfer.FileSize,
		transfer.State,
		direction,
		transfer.BytesTransferred,
		formatTime(transfer.CreatedAt),
	)
	return err
}

func (db *DB) UpdateTransferState(transferID string, state string) error {
	_, err := db.raw.Exec(`update transfers set state = ? where transfer_id = ?`, state, transferID)
	return err
}

func (db *DB) UpdateTransferProgress(transferID string, state string, bytesTransferred int64) error {
	_, err := db.raw.Exec(
		`update transfers set state = ?, bytes_transferred = ? where transfer_id = ?`,
		state,
		bytesTransferred,
		transferID,
	)
	return err
}

func (db *DB) PersistTransferOutcome(message *domain.Message, transfer domain.Transfer) error {
	tx, err := db.raw.Begin()
	if err != nil {
		return err
	}

	if message != nil {
		if _, err := tx.Exec(
			`insert into messages (message_id, conversation_id, direction, kind, body, status, created_at)
			values (?, ?, ?, ?, ?, ?, ?)
			on conflict(message_id) do update set
				conversation_id = excluded.conversation_id,
				direction = excluded.direction,
				kind = excluded.kind,
				body = excluded.body,
				status = excluded.status,
				created_at = excluded.created_at`,
			message.MessageID,
			message.ConversationID,
			message.Direction,
			message.Kind,
			message.Body,
			message.Status,
			formatTime(message.CreatedAt),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	direction := transfer.Direction
	if strings.TrimSpace(direction) == "" {
		direction = "outgoing"
	}
	if _, err := tx.Exec(
		`update transfers
			set state = ?, direction = ?, bytes_transferred = ?, file_size = ?
			where transfer_id = ?`,
		transfer.State,
		direction,
		transfer.BytesTransferred,
		transfer.FileSize,
		transfer.TransferID,
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (db *DB) ListTransfers() ([]domain.Transfer, error) {
	rows, err := db.raw.Query(
		`select transfer_id, message_id, file_name, file_size, state, direction, bytes_transferred, created_at
		from transfers
		order by created_at asc`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transfers := make([]domain.Transfer, 0)
	for rows.Next() {
		var transfer domain.Transfer
		var createdAt string
		if err := rows.Scan(
			&transfer.TransferID,
			&transfer.MessageID,
			&transfer.FileName,
			&transfer.FileSize,
			&transfer.State,
			&transfer.Direction,
			&transfer.BytesTransferred,
			&createdAt,
		); err != nil {
			return nil, err
		}
		transfer.CreatedAt = parseTime(createdAt)
		transfers = append(transfers, transfer)
	}

	return transfers, rows.Err()
}
