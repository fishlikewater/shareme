package protocol

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"shareme/backend/internal/discovery"
	"shareme/backend/internal/security"
)

const (
	multipartCopyBufferSize   = 1 << 20
	rawTransferCopyBufferSize = 1 << 20
	transferHeaderSessionID   = "X-Message-Share-Session-Id"
	transferHeaderTransferID  = "X-Message-Share-Transfer-Id"
	transferHeaderPartIndex   = "X-Message-Share-Part-Index"
	transferHeaderOffset      = "X-Message-Share-Offset"
	transferHeaderLength      = "X-Message-Share-Length"
)

func NewPeerHTTPClient(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Transport: NewPeerHTTPTransport(tlsConfig),
	}
}

func NewLANPeerHTTPClient(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Transport: NewLANPeerHTTPTransport(tlsConfig),
	}
}

func NewPeerHTTPTransport(tlsConfig *tls.Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	transport.ForceAttemptHTTP2 = true
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 32
	transport.MaxConnsPerHost = 16
	transport.IdleConnTimeout = 90 * time.Second
	transport.ReadBufferSize = multipartCopyBufferSize
	transport.WriteBufferSize = multipartCopyBufferSize
	return transport
}

func NewLANPeerHTTPTransport(tlsConfig *tls.Config) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	transport.ForceAttemptHTTP2 = false
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = 128
	transport.MaxConnsPerHost = 64
	transport.IdleConnTimeout = 90 * time.Second
	transport.ReadBufferSize = rawTransferCopyBufferSize
	transport.WriteBufferSize = rawTransferCopyBufferSize
	transport.DisableCompression = true
	return transport
}

type PairingHandler interface {
	AcceptIncomingPairing(ctx context.Context, request PairingStartRequest) (PairingStartResponse, error)
	AcceptPairingConfirm(ctx context.Context, request PairingConfirmRequest) (PairingConfirmResponse, error)
	AcceptIncomingTextMessage(ctx context.Context, request TextMessageRequest) (AckResponse, error)
	AcceptIncomingFileTransfer(ctx context.Context, request FileTransferRequest, content io.Reader) (FileTransferResponse, error)
}

type HeartbeatHandler interface {
	AcceptHeartbeat(ctx context.Context, request HeartbeatRequest) (HeartbeatResponse, error)
}

type TransferSessionHandler interface {
	StartIncomingTransferSession(ctx context.Context, request TransferSessionStartRequest) (TransferSessionStartResponse, error)
	AcceptIncomingTransferPart(ctx context.Context, request TransferPartRequest, content io.Reader) (TransferPartResponse, error)
	CompleteIncomingTransferSession(ctx context.Context, request TransferSessionCompleteRequest) (TransferSessionCompleteResponse, error)
}

type HTTPPeerTransportOptions struct {
	HTTPClient            *http.Client
	ClientFactory         func(expectedFingerprint string) *http.Client
	TransferHTTPClient    *http.Client
	TransferClientFactory func(expectedFingerprint string) *http.Client
	Scheme                string
}

type HTTPPeerTransport struct {
	client                *http.Client
	clientFactory         func(expectedFingerprint string) *http.Client
	transferClient        *http.Client
	transferClientFactory func(expectedFingerprint string) *http.Client
	scheme                string
	clientCacheMu         sync.RWMutex
	clientCache           map[string]*http.Client
	transferClientCache   map[string]*http.Client
	sessionModesMu        sync.RWMutex
	sessionModes          map[string]string
}

func NewHTTPPeerTransport(options HTTPPeerTransportOptions) *HTTPPeerTransport {
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	transferClient := options.TransferHTTPClient
	if transferClient == nil {
		transferClient = client
	}

	scheme := options.Scheme
	if scheme == "" {
		scheme = "https"
	}

	return &HTTPPeerTransport{
		client:                client,
		clientFactory:         options.ClientFactory,
		transferClient:        transferClient,
		transferClientFactory: options.TransferClientFactory,
		scheme:                scheme,
		clientCache:           make(map[string]*http.Client),
		transferClientCache:   make(map[string]*http.Client),
		sessionModes:          make(map[string]string),
	}
}

func (t *HTTPPeerTransport) StartPairing(
	ctx context.Context,
	peer discovery.PeerRecord,
	request PairingStartRequest,
) (PairingStartResponse, error) {
	var response PairingStartResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/pairings/start"), request, &response); err != nil {
		return PairingStartResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) ConfirmPairing(
	ctx context.Context,
	peer discovery.PeerRecord,
	request PairingConfirmRequest,
) (PairingConfirmResponse, error) {
	var response PairingConfirmResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/pairings/confirm"), request, &response); err != nil {
		return PairingConfirmResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) SendTextMessage(
	ctx context.Context,
	peer discovery.PeerRecord,
	request TextMessageRequest,
) (AckResponse, error) {
	var response AckResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/messages/text"), request, &response); err != nil {
		return AckResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) SendHeartbeat(
	ctx context.Context,
	peer discovery.PeerRecord,
	request HeartbeatRequest,
) (HeartbeatResponse, error) {
	var response HeartbeatResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/heartbeat"), request, &response); err != nil {
		return HeartbeatResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) SendFile(
	ctx context.Context,
	peer discovery.PeerRecord,
	request FileTransferRequest,
	content io.Reader,
) (FileTransferResponse, error) {
	var response FileTransferResponse
	if err := t.postMultipartFile(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/file"), request, content, &response); err != nil {
		return FileTransferResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) StartTransferSession(
	ctx context.Context,
	peer discovery.PeerRecord,
	request TransferSessionStartRequest,
) (TransferSessionStartResponse, error) {
	var response TransferSessionStartResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/session/start"), request, &response); err != nil {
		return TransferSessionStartResponse{}, err
	}
	t.rememberTransferSessionMode(response.SessionID, response.AdaptivePolicyVersion)
	return response, nil
}

func (t *HTTPPeerTransport) UploadTransferPart(
	ctx context.Context,
	peer discovery.PeerRecord,
	request TransferPartRequest,
	content io.Reader,
) (TransferPartResponse, error) {
	var response TransferPartResponse
	if t.sessionUsesRawTransfer(request.SessionID) {
		if err := t.postRawTransferPart(ctx, t.transferHTTPClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/session/part"), request, content, &response); err != nil {
			return TransferPartResponse{}, err
		}
		return response, nil
	}
	if err := t.postMultipartTransferPart(ctx, t.transferHTTPClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/session/part"), request, content, &response); err != nil {
		return TransferPartResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) CompleteTransferSession(
	ctx context.Context,
	peer discovery.PeerRecord,
	request TransferSessionCompleteRequest,
) (TransferSessionCompleteResponse, error) {
	defer t.clearTransferSessionMode(request.SessionID)
	var response TransferSessionCompleteResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/session/complete"), request, &response); err != nil {
		return TransferSessionCompleteResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) PrepareAcceleratedTransfer(
	ctx context.Context,
	peer discovery.PeerRecord,
	request AcceleratedPrepareRequest,
) (AcceleratedPrepareResponse, error) {
	var response AcceleratedPrepareResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/accelerated/prepare"), request, &response); err != nil {
		return AcceleratedPrepareResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) CompleteAcceleratedTransfer(
	ctx context.Context,
	peer discovery.PeerRecord,
	request AcceleratedCompleteRequest,
) (AcceleratedCompleteResponse, error) {
	var response AcceleratedCompleteResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/accelerated/complete"), request, &response); err != nil {
		return AcceleratedCompleteResponse{}, err
	}
	return response, nil
}

func (t *HTTPPeerTransport) postJSON(ctx context.Context, client *http.Client, url string, payload any, dest any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", response.StatusCode)
	}

	return json.NewDecoder(response.Body).Decode(dest)
}

func (t *HTTPPeerTransport) postMultipartFile(
	ctx context.Context,
	client *http.Client,
	url string,
	payload FileTransferRequest,
	content io.Reader,
	dest any,
) error {
	reader, writer := io.Pipe()
	formWriter := multipart.NewWriter(writer)

	go func() {
		defer writer.Close()
		defer formWriter.Close()

		fields := map[string]string{
			"transferId":     payload.TransferID,
			"messageId":      payload.MessageID,
			"senderDeviceId": payload.SenderDeviceID,
			"fileName":       payload.FileName,
			"fileSize":       strconv.FormatInt(payload.FileSize, 10),
			"createdAt":      payload.CreatedAtRFC3339,
			"agentTcpPort":   strconv.Itoa(payload.AgentTCPPort),
		}
		for key, value := range fields {
			if err := formWriter.WriteField(key, value); err != nil {
				_ = writer.CloseWithError(err)
				return
			}
		}

		part, err := formWriter.CreateFormFile("file", payload.FileName)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if _, err := io.CopyBuffer(part, readerOnly{Reader: content}, make([]byte, multipartCopyBufferSize)); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", formWriter.FormDataContentType())

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", response.StatusCode)
	}

	return json.NewDecoder(response.Body).Decode(dest)
}

func (t *HTTPPeerTransport) postMultipartTransferPart(
	ctx context.Context,
	client *http.Client,
	url string,
	payload TransferPartRequest,
	content io.Reader,
	dest any,
) error {
	reader, writer := io.Pipe()
	formWriter := multipart.NewWriter(writer)

	go func() {
		defer writer.Close()
		defer formWriter.Close()

		fields := map[string]string{
			"sessionId":  payload.SessionID,
			"transferId": payload.TransferID,
			"partIndex":  strconv.Itoa(payload.PartIndex),
			"offset":     strconv.FormatInt(payload.Offset, 10),
			"length":     strconv.FormatInt(payload.Length, 10),
		}
		for key, value := range fields {
			if err := formWriter.WriteField(key, value); err != nil {
				_ = writer.CloseWithError(err)
				return
			}
		}

		part, err := formWriter.CreateFormFile("file", fmt.Sprintf("part-%d.bin", payload.PartIndex))
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if _, err := io.CopyBuffer(part, readerOnly{Reader: content}, make([]byte, multipartCopyBufferSize)); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", formWriter.FormDataContentType())

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", response.StatusCode)
	}

	return json.NewDecoder(response.Body).Decode(dest)
}

func (t *HTTPPeerTransport) postRawTransferPart(
	ctx context.Context,
	client *http.Client,
	url string,
	payload TransferPartRequest,
	content io.Reader,
	dest any,
) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, readerOnly{Reader: content})
	if err != nil {
		return err
	}
	request.ContentLength = payload.Length
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set(transferHeaderSessionID, payload.SessionID)
	request.Header.Set(transferHeaderTransferID, payload.TransferID)
	request.Header.Set(transferHeaderPartIndex, strconv.Itoa(payload.PartIndex))
	request.Header.Set(transferHeaderOffset, strconv.FormatInt(payload.Offset, 10))
	request.Header.Set(transferHeaderLength, strconv.FormatInt(payload.Length, 10))

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", response.StatusCode)
	}

	return json.NewDecoder(response.Body).Decode(dest)
}

func NewPeerHTTPServer(handler PairingHandler) http.Handler {
	var authorizer PeerRequestAuthorizer
	if value, ok := handler.(PeerRequestAuthorizer); ok {
		authorizer = value
	}
	var acceleratedAuthorizer AcceleratedTransferAuthorizer
	if value, ok := handler.(AcceleratedTransferAuthorizer); ok {
		acceleratedAuthorizer = value
	}
	var acceleratedHandler AcceleratedTransferHandler
	if value, ok := handler.(AcceleratedTransferHandler); ok {
		acceleratedHandler = value
	}
	var heartbeatHandler HeartbeatHandler
	if value, ok := handler.(HeartbeatHandler); ok {
		heartbeatHandler = value
	}
	var transferSessionHandler TransferSessionHandler
	if value, ok := handler.(TransferSessionHandler); ok {
		transferSessionHandler = value
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/peer/pairings/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request PairingStartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validatePairingStartCaller(request, caller); err != nil {
			writePeerError(w, err)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizePairingStart(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptIncomingPairing(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/pairings/confirm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request PairingConfirmRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validatePairingConfirmCaller(request, caller); err != nil {
			writePeerError(w, err)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizePairingConfirm(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptPairingConfirm(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if heartbeatHandler == nil {
			http.NotFound(w, r)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizeHeartbeat(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := heartbeatHandler.AcceptHeartbeat(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/transfers/session/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if transferSessionHandler == nil {
			http.NotFound(w, r)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request TransferSessionStartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizeTransferSessionStart(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := transferSessionHandler.StartIncomingTransferSession(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/transfers/session/part", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if transferSessionHandler == nil {
			http.NotFound(w, r)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		request, filePart, err := parseTransferPartUpload(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer filePart.Close()
		if authorizer != nil {
			if err := authorizer.AuthorizeTransferPart(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := transferSessionHandler.AcceptIncomingTransferPart(ctx, request, filePart)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/transfers/session/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if transferSessionHandler == nil {
			http.NotFound(w, r)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request TransferSessionCompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizeTransferSessionComplete(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := transferSessionHandler.CompleteIncomingTransferSession(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/transfers/accelerated/prepare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if acceleratedHandler == nil {
			http.NotFound(w, r)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request AcceleratedPrepareRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if acceleratedAuthorizer != nil {
			if err := acceleratedAuthorizer.AuthorizeAcceleratedPrepare(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := acceleratedHandler.PrepareAcceleratedTransfer(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/transfers/accelerated/complete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if acceleratedHandler == nil {
			http.NotFound(w, r)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request AcceleratedCompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if acceleratedAuthorizer != nil {
			if err := acceleratedAuthorizer.AuthorizeAcceleratedComplete(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := acceleratedHandler.CompleteAcceleratedTransfer(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/messages/text", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		var request TextMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizeTextMessage(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptIncomingTextMessage(ctx, request)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/peer/transfers/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		caller, err := peerCallerFromRequest(r)
		if err != nil {
			writePeerError(w, err)
			return
		}
		ctx := ContextWithPeerCaller(r.Context(), caller)

		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var (
			request     FileTransferRequest
			filePart    *multipart.Part
			fileSizeSet bool
		)

	readParts:
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if part.FormName() == "file" {
				filePart = part
				request.FileName = coalesce(request.FileName, part.FileName())
				break readParts
			}

			value, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			switch part.FormName() {
			case "transferId":
				request.TransferID = strings.TrimSpace(string(value))
			case "messageId":
				request.MessageID = strings.TrimSpace(string(value))
			case "senderDeviceId":
				request.SenderDeviceID = strings.TrimSpace(string(value))
			case "fileName":
				request.FileName = coalesce(request.FileName, string(value))
			case "fileSize":
				fileSize, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
				if err != nil {
					http.Error(w, "invalid fileSize", http.StatusBadRequest)
					return
				}
				request.FileSize = fileSize
				fileSizeSet = true
			case "createdAt":
				request.CreatedAtRFC3339 = strings.TrimSpace(string(value))
			case "agentTcpPort":
				request.AgentTCPPort = parseOptionalInt(string(value))
			}
		}

		if filePart == nil {
			http.Error(w, "file is required", http.StatusBadRequest)
			return
		}
		if !fileSizeSet {
			http.Error(w, "invalid fileSize", http.StatusBadRequest)
			return
		}
		defer filePart.Close()
		if authorizer != nil {
			if err := authorizer.AuthorizeFileTransfer(ctx, request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptIncomingFileTransfer(ctx, request, filePart)
		if err != nil {
			writePeerError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	return mux
}

func peerURL(scheme string, addr string, path string) string {
	return fmt.Sprintf("%s://%s%s", scheme, strings.TrimSpace(addr), path)
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseTransferPartUpload(r *http.Request) (TransferPartRequest, io.ReadCloser, error) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "application/octet-stream") {
		return parseRawTransferPartUpload(r)
	}

	reader, err := r.MultipartReader()
	if err != nil {
		return TransferPartRequest{}, nil, err
	}

	var (
		request  TransferPartRequest
		filePart *multipart.Part
	)

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return TransferPartRequest{}, nil, err
		}
		if part.FormName() == "file" {
			filePart = part
			break
		}

		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return TransferPartRequest{}, nil, err
		}

		switch part.FormName() {
		case "sessionId":
			request.SessionID = strings.TrimSpace(string(value))
		case "transferId":
			request.TransferID = strings.TrimSpace(string(value))
		case "partIndex":
			partIndex, err := strconv.Atoi(strings.TrimSpace(string(value)))
			if err != nil {
				return TransferPartRequest{}, nil, fmt.Errorf("invalid partIndex")
			}
			request.PartIndex = partIndex
		case "offset":
			offset, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
			if err != nil {
				return TransferPartRequest{}, nil, fmt.Errorf("invalid offset")
			}
			request.Offset = offset
		case "length":
			length, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
			if err != nil {
				return TransferPartRequest{}, nil, fmt.Errorf("invalid length")
			}
			request.Length = length
		}
	}

	if strings.TrimSpace(request.SessionID) == "" {
		return TransferPartRequest{}, nil, fmt.Errorf("sessionId is required")
	}
	if filePart == nil {
		return TransferPartRequest{}, nil, fmt.Errorf("file is required")
	}
	return request, filePart, nil
}

func parseRawTransferPartUpload(r *http.Request) (TransferPartRequest, io.ReadCloser, error) {
	request := TransferPartRequest{
		SessionID:  strings.TrimSpace(r.Header.Get(transferHeaderSessionID)),
		TransferID: strings.TrimSpace(r.Header.Get(transferHeaderTransferID)),
		RawBody:    true,
	}
	if request.SessionID == "" {
		return TransferPartRequest{}, nil, fmt.Errorf("sessionId is required")
	}
	partIndex, err := strconv.Atoi(strings.TrimSpace(r.Header.Get(transferHeaderPartIndex)))
	if err != nil {
		return TransferPartRequest{}, nil, fmt.Errorf("invalid partIndex")
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(transferHeaderOffset)), 10, 64)
	if err != nil {
		return TransferPartRequest{}, nil, fmt.Errorf("invalid offset")
	}
	length, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(transferHeaderLength)), 10, 64)
	if err != nil {
		return TransferPartRequest{}, nil, fmt.Errorf("invalid length")
	}
	request.PartIndex = partIndex
	request.Offset = offset
	request.Length = length
	if r.Body == nil {
		return TransferPartRequest{}, nil, fmt.Errorf("file is required")
	}
	return request, r.Body, nil
}

type readerOnly struct {
	io.Reader
}

func (t *HTTPPeerTransport) httpClient(peer discovery.PeerRecord) *http.Client {
	return t.cachedClient(strings.TrimSpace(peer.PinnedFingerprint), t.client, t.clientFactory, t.clientCache)
}

func (t *HTTPPeerTransport) transferHTTPClient(peer discovery.PeerRecord) *http.Client {
	return t.cachedClient(strings.TrimSpace(peer.PinnedFingerprint), t.transferClient, t.transferClientFactory, t.transferClientCache)
}

func (t *HTTPPeerTransport) rememberTransferSessionMode(sessionID string, adaptivePolicyVersion string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	t.sessionModesMu.Lock()
	defer t.sessionModesMu.Unlock()
	t.sessionModes[sessionID] = strings.TrimSpace(adaptivePolicyVersion)
}

func (t *HTTPPeerTransport) clearTransferSessionMode(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	t.sessionModesMu.Lock()
	defer t.sessionModesMu.Unlock()
	delete(t.sessionModes, sessionID)
}

func (t *HTTPPeerTransport) sessionUsesRawTransfer(sessionID string) bool {
	t.sessionModesMu.RLock()
	mode := t.sessionModes[sessionID]
	t.sessionModesMu.RUnlock()
	return strings.EqualFold(strings.TrimSpace(mode), "v2-lan-fast")
}

func (t *HTTPPeerTransport) ForgetTransferSession(sessionID string) {
	t.clearTransferSessionMode(sessionID)
}

func (t *HTTPPeerTransport) cachedClient(
	fingerprint string,
	fallback *http.Client,
	factory func(expectedFingerprint string) *http.Client,
	cache map[string]*http.Client,
) *http.Client {
	if factory == nil {
		return fallback
	}

	t.clientCacheMu.RLock()
	client, ok := cache[fingerprint]
	t.clientCacheMu.RUnlock()
	if ok && client != nil {
		return client
	}

	t.clientCacheMu.Lock()
	defer t.clientCacheMu.Unlock()
	if client, ok = cache[fingerprint]; ok && client != nil {
		return client
	}

	client = factory(fingerprint)
	if client == nil {
		client = fallback
	}
	cache[fingerprint] = client
	return client
}

func peerCallerFromRequest(r *http.Request) (PeerCaller, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return PeerCaller{}, ErrPeerAuthenticationRequired
	}

	fingerprint, err := security.FingerprintLeafDER(r.TLS.PeerCertificates[0].Raw)
	if err != nil {
		return PeerCaller{}, fmt.Errorf("%w: invalid peer certificate", ErrPeerAuthenticationRequired)
	}
	return PeerCaller{Fingerprint: fingerprint, RemoteAddr: r.RemoteAddr}, nil
}

func validatePairingStartCaller(request PairingStartRequest, caller PeerCaller) error {
	if strings.TrimSpace(request.InitiatorFingerprint) == "" {
		return fmt.Errorf("%w: initiator fingerprint missing", ErrPeerForbidden)
	}
	if caller.Fingerprint != strings.TrimSpace(request.InitiatorFingerprint) {
		return fmt.Errorf("%w: initiator fingerprint mismatch", ErrPeerForbidden)
	}
	return nil
}

func validatePairingConfirmCaller(request PairingConfirmRequest, caller PeerCaller) error {
	if strings.TrimSpace(request.ConfirmerFingerprint) == "" {
		return fmt.Errorf("%w: confirmer fingerprint missing", ErrPeerForbidden)
	}
	if caller.Fingerprint != strings.TrimSpace(request.ConfirmerFingerprint) {
		return fmt.Errorf("%w: confirmer fingerprint mismatch", ErrPeerForbidden)
	}
	return nil
}

func writePeerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPeerAuthenticationRequired):
		http.Error(w, err.Error(), http.StatusUnauthorized)
	case errors.Is(err, ErrPeerForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseOptionalInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}
