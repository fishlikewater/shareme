package protocol

import (
	"context"
	"errors"
)

type PeerCaller struct {
	Fingerprint string
}

var (
	ErrPeerAuthenticationRequired = errors.New("peer client certificate required")
	ErrPeerForbidden              = errors.New("peer request forbidden")
)

type PeerRequestAuthorizer interface {
	AuthorizePairingStart(ctx context.Context, request PairingStartRequest, caller PeerCaller) error
	AuthorizePairingConfirm(ctx context.Context, request PairingConfirmRequest, caller PeerCaller) error
	AuthorizeTextMessage(ctx context.Context, request TextMessageRequest, caller PeerCaller) error
	AuthorizeFileTransfer(ctx context.Context, request FileTransferRequest, caller PeerCaller) error
}

type PairingStartRequest struct {
	PairingID            string `json:"pairingId"`
	InitiatorDeviceID    string `json:"initiatorDeviceId"`
	InitiatorDeviceName  string `json:"initiatorDeviceName"`
	InitiatorFingerprint string `json:"initiatorFingerprint"`
	InitiatorNonce       string `json:"initiatorNonce"`
}

type PairingStartResponse struct {
	PairingID            string `json:"pairingId"`
	ResponderDeviceID    string `json:"responderDeviceId"`
	ResponderDeviceName  string `json:"responderDeviceName"`
	ResponderFingerprint string `json:"responderFingerprint"`
	ResponderNonce       string `json:"responderNonce"`
}

type PairingConfirmRequest struct {
	PairingID            string `json:"pairingId"`
	ConfirmerDeviceID    string `json:"confirmerDeviceId"`
	ConfirmerFingerprint string `json:"confirmerFingerprint"`
	Confirmed            bool   `json:"confirmed"`
}

type PairingConfirmResponse struct {
	PairingID       string `json:"pairingId"`
	Status          string `json:"status"`
	RemoteConfirmed bool   `json:"remoteConfirmed"`
}

type TextMessageRequest struct {
	MessageID        string `json:"messageId"`
	ConversationID   string `json:"conversationId"`
	SenderDeviceID   string `json:"senderDeviceId"`
	Body             string `json:"body"`
	CreatedAtRFC3339 string `json:"createdAt"`
}

type FileTransferRequest struct {
	TransferID       string `json:"transferId"`
	MessageID        string `json:"messageId"`
	SenderDeviceID   string `json:"senderDeviceId"`
	FileName         string `json:"fileName"`
	FileSize         int64  `json:"fileSize"`
	CreatedAtRFC3339 string `json:"createdAt"`
}

type FileTransferResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
}

type AckResponse struct {
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
}
