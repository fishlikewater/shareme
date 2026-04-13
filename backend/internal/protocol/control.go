package protocol

const (
	KindPairRequest = "pair_request"
	KindPairAccept  = "pair_accept"
	KindTextMessage = "text_message"
	KindFileOffer   = "file_offer"
	KindFileAccept  = "file_accept"
	KindFileDone    = "file_done"
	KindError       = "error"
)

type ControlEnvelope struct {
	ProtocolVersion string `json:"protocolVersion"`
	RequestID       string `json:"requestId"`
	MessageID       string `json:"messageId"`
	SenderDeviceID  string `json:"senderDeviceId"`
	Kind            string `json:"kind"`
	ErrorCode       string `json:"errorCode,omitempty"`
}
