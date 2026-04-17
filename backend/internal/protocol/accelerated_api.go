package protocol

import "context"

type AcceleratedPrepareRequest struct {
	TransferID     string `json:"transferId"`
	MessageID      string `json:"messageId"`
	SenderDeviceID string `json:"senderDeviceId"`
	FileName       string `json:"fileName"`
	FileSize       int64  `json:"fileSize"`
	FileSHA256     string `json:"fileSha256"`
	AgentTCPPort   int    `json:"agentTcpPort"`
}

type AcceleratedPrepareResponse struct {
	SessionID             string `json:"sessionId"`
	TransferToken         string `json:"transferToken"`
	DataPort              int    `json:"dataPort"`
	ChunkSize             int64  `json:"chunkSize"`
	InitialStripes        int    `json:"initialStripes"`
	MaxStripes            int    `json:"maxStripes"`
	MaxInFlightBytes      int64  `json:"maxInFlightBytes"`
	AckTimeoutMillis      int    `json:"ackTimeoutMillis"`
	AdaptivePolicyVersion string `json:"adaptivePolicyVersion"`
	ExpiresAtRFC3339      string `json:"expiresAt"`
}

type AcceleratedCompleteRequest struct {
	SessionID  string `json:"sessionId"`
	TransferID string `json:"transferId"`
	FileSHA256 string `json:"fileSha256"`
}

type AcceleratedCompleteResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
}

type AcceleratedTransferHandler interface {
	PrepareAcceleratedTransfer(ctx context.Context, request AcceleratedPrepareRequest) (AcceleratedPrepareResponse, error)
	CompleteAcceleratedTransfer(ctx context.Context, request AcceleratedCompleteRequest) (AcceleratedCompleteResponse, error)
}

type AcceleratedTransferAuthorizer interface {
	AuthorizeAcceleratedPrepare(ctx context.Context, request AcceleratedPrepareRequest, caller PeerCaller) error
	AuthorizeAcceleratedComplete(ctx context.Context, request AcceleratedCompleteRequest, caller PeerCaller) error
}
