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
	Direction      string
	Kind           string
	Body           string
	Status         string
	CreatedAt      time.Time
}

type MessageBoundary struct {
	CreatedAt time.Time
	MessageID string
}

type Transfer struct {
	TransferID       string
	MessageID        string
	FileName         string
	FileSize         int64
	State            string
	Direction        string
	BytesTransferred int64
	ProgressPercent  float64
	RateBytesPerSec  float64
	EtaSeconds       *int64
	Active           bool
	CreatedAt        time.Time
}
