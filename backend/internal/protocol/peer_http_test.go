package protocol

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"message-share/backend/internal/device"
	"message-share/backend/internal/discovery"
	"message-share/backend/internal/security"
)

func TestHTTPPeerTransportStartPairingPostsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/pairings/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var request PairingStartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("unexpected decode error: %v", err)
		}
		if request.PairingID != "pair-1" || request.InitiatorDeviceID != "local-1" {
			t.Fatalf("unexpected request payload: %#v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PairingStartResponse{
			PairingID:            "pair-1",
			ResponderDeviceID:    "peer-1",
			ResponderDeviceName:  "meeting-room",
			ResponderFingerprint: "fingerprint-a",
			ResponderNonce:       "nonce-b",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	response, err := transport.StartPairing(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, PairingStartRequest{
		PairingID:            "pair-1",
		InitiatorDeviceID:    "local-1",
		InitiatorDeviceName:  "my-pc",
		InitiatorFingerprint: "fingerprint-local",
		InitiatorNonce:       "nonce-a",
	})
	if err != nil {
		t.Fatalf("unexpected start pairing error: %v", err)
	}
	if response.ResponderDeviceID != "peer-1" || response.ResponderNonce != "nonce-b" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestPeerHTTPServerDelegatesPairingRequests(t *testing.T) {
	handler := &fakePairingHandler{
		startResponse: PairingStartResponse{
			PairingID:            "pair-2",
			ResponderDeviceID:    "local-1",
			ResponderDeviceName:  "my-pc",
			ResponderFingerprint: "fingerprint-local",
			ResponderNonce:       "nonce-b",
		},
		confirmResponse: PairingConfirmResponse{
			PairingID:       "pair-2",
			Status:          "confirmed",
			RemoteConfirmed: true,
		},
	}

	server := NewPeerHTTPServer(handler)
	state, fingerprint := newPeerTLSState(t)
	startBody, _ := json.Marshal(PairingStartRequest{
		PairingID:            "pair-2",
		InitiatorDeviceID:    "peer-2",
		InitiatorDeviceName:  "peer-two",
		InitiatorFingerprint: fingerprint,
		InitiatorNonce:       "nonce-a",
	})
	startRequest := httptest.NewRequest(http.MethodPost, "/peer/pairings/start", bytes.NewReader(startBody))
	startRequest.TLS = state
	startRecorder := httptest.NewRecorder()
	server.ServeHTTP(startRecorder, startRequest)
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected start status: %d", startRecorder.Code)
	}

	confirmBody, _ := json.Marshal(PairingConfirmRequest{
		PairingID:            "pair-2",
		ConfirmerDeviceID:    "peer-2",
		ConfirmerFingerprint: fingerprint,
		Confirmed:            true,
	})
	confirmRequest := httptest.NewRequest(http.MethodPost, "/peer/pairings/confirm", bytes.NewReader(confirmBody))
	confirmRequest.TLS = state
	confirmRecorder := httptest.NewRecorder()
	server.ServeHTTP(confirmRecorder, confirmRequest)
	if confirmRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected confirm status: %d", confirmRecorder.Code)
	}

	if handler.startRequest.PairingID != "pair-2" || handler.confirmRequest.PairingID != "pair-2" {
		t.Fatalf("unexpected delegated requests: %#v %#v", handler.startRequest, handler.confirmRequest)
	}
}

func TestPeerHTTPServerRejectsTextMessageWithoutClientCertificate(t *testing.T) {
	handler := &fakePairingHandler{}
	server := NewPeerHTTPServer(handler)

	body, err := json.Marshal(TextMessageRequest{
		MessageID:      "msg-unauthorized",
		SenderDeviceID: "peer-2",
		Body:           "hello",
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/peer/messages/text", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d", recorder.Code)
	}
	if handler.textRequest.MessageID != "" {
		t.Fatalf("expected request not to be delegated, got %#v", handler.textRequest)
	}
}

func TestPeerHTTPServerRejectsPairingStartWhenFingerprintDoesNotMatchClientCertificate(t *testing.T) {
	handler := &fakePairingHandler{}
	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	body, err := json.Marshal(PairingStartRequest{
		PairingID:            "pair-mismatch",
		InitiatorDeviceID:    "peer-2",
		InitiatorDeviceName:  "peer-two",
		InitiatorFingerprint: "fingerprint-mismatch",
		InitiatorNonce:       "nonce-a",
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/peer/pairings/start", bytes.NewReader(body))
	request.TLS = state
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status, got %d", recorder.Code)
	}
	if handler.startRequest.PairingID != "" {
		t.Fatalf("expected request not to be delegated, got %#v", handler.startRequest)
	}
}

func TestHTTPPeerTransportSendFilePostsMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/file" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		if got := r.FormValue("transferId"); got != "transfer-1" {
			t.Fatalf("unexpected transfer id: %s", got)
		}
		if got := r.FormValue("senderDeviceId"); got != "local-1" {
			t.Fatalf("unexpected sender id: %s", got)
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("unexpected form file error: %v", err)
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if string(content) != "hello" {
			t.Fatalf("unexpected file content: %q", string(content))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FileTransferResponse{
			TransferID: "transfer-1",
			State:      "done",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	response, err := transport.SendFile(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, FileTransferRequest{
		TransferID:     "transfer-1",
		MessageID:      "msg-1",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       5,
	}, bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}
	if response.TransferID != "transfer-1" || response.State != "done" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestHTTPPeerTransportUsesPinnedFingerprintForTextRequests(t *testing.T) {
	var capturedFingerprints []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AckResponse{
			RequestID: "msg-1",
			Status:    "accepted",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		Scheme: "http",
		ClientFactory: func(expectedFingerprint string) *http.Client {
			capturedFingerprints = append(capturedFingerprints, expectedFingerprint)
			return server.Client()
		},
	})

	_, err := transport.SendTextMessage(context.Background(), discovery.PeerRecord{
		LastKnownAddr:     server.Listener.Addr().String(),
		PinnedFingerprint: "fingerprint-pinned",
	}, TextMessageRequest{
		MessageID:      "msg-1",
		SenderDeviceID: "local-1",
		Body:           "hello",
	})
	if err != nil {
		t.Fatalf("unexpected send text error: %v", err)
	}
	if len(capturedFingerprints) != 1 || capturedFingerprints[0] != "fingerprint-pinned" {
		t.Fatalf("unexpected pinned fingerprints: %#v", capturedFingerprints)
	}
}

func TestPeerHTTPServerDelegatesFileTransferRequests(t *testing.T) {
	handler := &fakePairingHandler{
		fileResponse: FileTransferResponse{
			TransferID: "transfer-2",
			State:      "done",
		},
	}

	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("transferId", "transfer-2")
	_ = writer.WriteField("messageId", "msg-2")
	_ = writer.WriteField("senderDeviceId", "peer-2")
	_ = writer.WriteField("fileName", "hello.txt")
	_ = writer.WriteField("fileSize", "5")
	part, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("unexpected create form file error: %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected multipart write error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("unexpected writer close error: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, "/peer/transfers/file", body)
	if err != nil {
		t.Fatalf("unexpected request error: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.TLS = state

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if handler.fileRequest.TransferID != "transfer-2" || string(handler.fileContent) != "hello" {
		t.Fatalf("unexpected delegated file payload: %#v %q", handler.fileRequest, string(handler.fileContent))
	}
}

func newPeerTLSState(t *testing.T) (*tls.ConnectionState, string) {
	t.Helper()

	localDevice, err := device.EnsureLocalDevice(filepath.Join(t.TempDir(), "peer.json"), "peer")
	if err != nil {
		t.Fatalf("unexpected local device error: %v", err)
	}

	certificate, err := security.BuildTLSCertificate(localDevice)
	if err != nil {
		t.Fatalf("unexpected certificate error: %v", err)
	}
	if len(certificate.Certificate) == 0 {
		t.Fatal("expected leaf certificate")
	}

	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("unexpected parse certificate error: %v", err)
	}
	fingerprint, err := security.FingerprintLeafDER(certificate.Certificate[0])
	if err != nil {
		t.Fatalf("unexpected fingerprint error: %v", err)
	}

	return &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{leaf},
	}, fingerprint
}

type fakePairingHandler struct {
	startRequest    PairingStartRequest
	startResponse   PairingStartResponse
	confirmRequest  PairingConfirmRequest
	confirmResponse PairingConfirmResponse
	textRequest     TextMessageRequest
	fileRequest     FileTransferRequest
	fileResponse    FileTransferResponse
	fileContent     []byte
}

func (f *fakePairingHandler) AcceptIncomingPairing(_ context.Context, request PairingStartRequest) (PairingStartResponse, error) {
	f.startRequest = request
	return f.startResponse, nil
}

func (f *fakePairingHandler) AcceptPairingConfirm(_ context.Context, request PairingConfirmRequest) (PairingConfirmResponse, error) {
	f.confirmRequest = request
	return f.confirmResponse, nil
}

func (f *fakePairingHandler) AcceptIncomingTextMessage(_ context.Context, request TextMessageRequest) (AckResponse, error) {
	f.textRequest = request
	return AckResponse{
		RequestID: request.MessageID,
		Status:    "accepted",
	}, nil
}

func (f *fakePairingHandler) AcceptIncomingFileTransfer(_ context.Context, request FileTransferRequest, content io.Reader) (FileTransferResponse, error) {
	f.fileRequest = request
	data, err := io.ReadAll(content)
	if err != nil {
		return FileTransferResponse{}, err
	}
	f.fileContent = data
	return f.fileResponse, nil
}
