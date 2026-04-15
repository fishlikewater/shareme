package protocol

import (
	"context"
	"errors"
)

type PeerCaller struct {
	Fingerprint string
	RemoteAddr  string
}

type peerCallerContextKey struct{}

func ContextWithPeerCaller(ctx context.Context, caller PeerCaller) context.Context {
	return context.WithValue(ctx, peerCallerContextKey{}, caller)
}

func CallerFromContext(ctx context.Context) (PeerCaller, bool) {
	caller, ok := ctx.Value(peerCallerContextKey{}).(PeerCaller)
	return caller, ok
}

var (
	ErrPeerAuthenticationRequired = errors.New("peer client certificate required")
	ErrPeerForbidden              = errors.New("peer request forbidden")
)

type PeerRequestAuthorizer interface {
	AuthorizePairingStart(ctx context.Context, request PairingStartRequest, caller PeerCaller) error
	AuthorizePairingConfirm(ctx context.Context, request PairingConfirmRequest, caller PeerCaller) error
	AuthorizeHeartbeat(ctx context.Context, request HeartbeatRequest, caller PeerCaller) error
	AuthorizeTransferSessionStart(ctx context.Context, request TransferSessionStartRequest, caller PeerCaller) error
	AuthorizeTransferPart(ctx context.Context, request TransferPartRequest, caller PeerCaller) error
	AuthorizeTransferSessionComplete(ctx context.Context, request TransferSessionCompleteRequest, caller PeerCaller) error
	AuthorizeTextMessage(ctx context.Context, request TextMessageRequest, caller PeerCaller) error
	AuthorizeFileTransfer(ctx context.Context, request FileTransferRequest, caller PeerCaller) error
}

type PairingStartRequest struct {
	PairingID            string `json:"pairingId"`
	InitiatorDeviceID    string `json:"initiatorDeviceId"`
	InitiatorDeviceName  string `json:"initiatorDeviceName"`
	InitiatorFingerprint string `json:"initiatorFingerprint"`
	InitiatorNonce       string `json:"initiatorNonce"`
	AgentTCPPort         int    `json:"agentTcpPort"`
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
	AgentTCPPort         int    `json:"agentTcpPort"`
}

type PairingConfirmResponse struct {
	PairingID       string `json:"pairingId"`
	Status          string `json:"status"`
	RemoteConfirmed bool   `json:"remoteConfirmed"`
}

type HeartbeatRequest struct {
	SenderDeviceID string `json:"senderDeviceId"`
	SentAtRFC3339  string `json:"sentAt"`
	AgentTCPPort   int    `json:"agentTcpPort"`
}

type HeartbeatResponse struct {
	ResponderDeviceID   string `json:"responderDeviceId"`
	ResponderDeviceName string `json:"responderDeviceName"`
	AgentTCPPort        int    `json:"agentTcpPort"`
	ReceivedAtRFC3339   string `json:"receivedAt"`
}

type TextMessageRequest struct {
	MessageID        string `json:"messageId"`
	ConversationID   string `json:"conversationId"`
	SenderDeviceID   string `json:"senderDeviceId"`
	Body             string `json:"body"`
	CreatedAtRFC3339 string `json:"createdAt"`
	AgentTCPPort     int    `json:"agentTcpPort"`
}

type FileTransferRequest struct {
	TransferID       string `json:"transferId"`
	MessageID        string `json:"messageId"`
	SenderDeviceID   string `json:"senderDeviceId"`
	FileName         string `json:"fileName"`
	FileSize         int64  `json:"fileSize"`
	CreatedAtRFC3339 string `json:"createdAt"`
	AgentTCPPort     int    `json:"agentTcpPort"`
}

type FileTransferResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
}

type TransferSessionStartRequest struct {
	TransferID     string `json:"transferId"`
	MessageID      string `json:"messageId"`
	SenderDeviceID string `json:"senderDeviceId"`
	FileName       string `json:"fileName"`
	FileSize       int64  `json:"fileSize"`
	FileSHA256     string `json:"fileSha256"`
	AgentTCPPort   int    `json:"agentTcpPort"`
}

type TransferSessionStartResponse struct {
	SessionID             string `json:"sessionId"`
	ChunkSize             int64  `json:"chunkSize"`
	InitialParallelism    int    `json:"initialParallelism"`
	MaxParallelism        int    `json:"maxParallelism"`
	AdaptivePolicyVersion string `json:"adaptivePolicyVersion"`
}

type TransferPartRequest struct {
	SessionID  string `json:"sessionId"`
	TransferID string `json:"transferId"`
	PartIndex  int    `json:"partIndex"`
	Offset     int64  `json:"offset"`
	Length     int64  `json:"length"`
	RawBody    bool   `json:"-"`
}

type TransferPartResponse struct {
	SessionID     string `json:"sessionId"`
	PartIndex     int    `json:"partIndex"`
	BytesWritten  int64  `json:"bytesWritten"`
	BytesReceived int64  `json:"bytesReceived"`
}

type TransferSessionCompleteRequest struct {
	SessionID  string `json:"sessionId"`
	TransferID string `json:"transferId"`
	TotalSize  int64  `json:"totalSize"`
	PartCount  int    `json:"partCount"`
	FileSHA256 string `json:"fileSha256"`
}

type TransferSessionCompleteResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
}

type AckResponse struct {
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
}
