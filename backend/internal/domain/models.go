package domain

import "time"

type LocalDevice struct {
	DeviceID      string
	DeviceName    string
	PublicKeyPEM  string
	PrivateKeyPEM string
	CreatedAt     time.Time
}

type Peer struct {
	DeviceID          string
	DeviceName        string
	PinnedFingerprint string
	RemarkName        string
	Trusted           bool
	UpdatedAt         time.Time
}

type Conversation struct {
	ConversationID string
	PeerDeviceID   string
	UpdatedAt      time.Time
}

type Message struct {
	MessageID      string
	ConversationID string
	Kind           string
	Body           string
	Status         string
	CreatedAt      time.Time
}

type Transfer struct {
	TransferID string
	MessageID  string
	FileName   string
	FileSize   int64
	State      string
	CreatedAt  time.Time
}
