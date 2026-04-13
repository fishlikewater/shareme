package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"message-share/backend/internal/discovery"
	"message-share/backend/internal/security"
)

type PairingHandler interface {
	AcceptIncomingPairing(ctx context.Context, request PairingStartRequest) (PairingStartResponse, error)
	AcceptPairingConfirm(ctx context.Context, request PairingConfirmRequest) (PairingConfirmResponse, error)
	AcceptIncomingTextMessage(ctx context.Context, request TextMessageRequest) (AckResponse, error)
	AcceptIncomingFileTransfer(ctx context.Context, request FileTransferRequest, content io.Reader) (FileTransferResponse, error)
}

type HTTPPeerTransportOptions struct {
	HTTPClient    *http.Client
	ClientFactory func(expectedFingerprint string) *http.Client
	Scheme        string
}

type HTTPPeerTransport struct {
	client        *http.Client
	clientFactory func(expectedFingerprint string) *http.Client
	scheme        string
}

func NewHTTPPeerTransport(options HTTPPeerTransportOptions) *HTTPPeerTransport {
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	scheme := options.Scheme
	if scheme == "" {
		scheme = "https"
	}

	return &HTTPPeerTransport{
		client:        client,
		clientFactory: options.ClientFactory,
		scheme:        scheme,
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
		if _, err := io.Copy(part, content); err != nil {
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

func NewPeerHTTPServer(handler PairingHandler) http.Handler {
	var authorizer PeerRequestAuthorizer
	if value, ok := handler.(PeerRequestAuthorizer); ok {
		authorizer = value
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
			if err := authorizer.AuthorizePairingStart(r.Context(), request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptIncomingPairing(r.Context(), request)
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
			if err := authorizer.AuthorizePairingConfirm(r.Context(), request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptPairingConfirm(r.Context(), request)
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

		var request TextMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if authorizer != nil {
			if err := authorizer.AuthorizeTextMessage(r.Context(), request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptIncomingTextMessage(r.Context(), request)
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
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		fileSize, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("fileSize")), 10, 64)
		if err != nil {
			http.Error(w, "invalid fileSize", http.StatusBadRequest)
			return
		}

		request := FileTransferRequest{
			TransferID:       strings.TrimSpace(r.FormValue("transferId")),
			MessageID:        strings.TrimSpace(r.FormValue("messageId")),
			SenderDeviceID:   strings.TrimSpace(r.FormValue("senderDeviceId")),
			FileName:         coalesce(r.FormValue("fileName"), header.Filename),
			FileSize:         fileSize,
			CreatedAtRFC3339: strings.TrimSpace(r.FormValue("createdAt")),
		}
		if authorizer != nil {
			if err := authorizer.AuthorizeFileTransfer(r.Context(), request, caller); err != nil {
				writePeerError(w, err)
				return
			}
		}

		response, err := handler.AcceptIncomingFileTransfer(r.Context(), request, file)
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

func (t *HTTPPeerTransport) httpClient(peer discovery.PeerRecord) *http.Client {
	if t.clientFactory != nil {
		return t.clientFactory(strings.TrimSpace(peer.PinnedFingerprint))
	}
	return t.client
}

func peerCallerFromRequest(r *http.Request) (PeerCaller, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return PeerCaller{}, ErrPeerAuthenticationRequired
	}

	fingerprint, err := security.FingerprintLeafDER(r.TLS.PeerCertificates[0].Raw)
	if err != nil {
		return PeerCaller{}, fmt.Errorf("%w: invalid peer certificate", ErrPeerAuthenticationRequired)
	}
	return PeerCaller{Fingerprint: fingerprint}, nil
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
