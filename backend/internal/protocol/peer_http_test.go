package protocol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		if got := r.FormValue("agentTcpPort"); got != "19090" {
			t.Fatalf("unexpected agent tcp port: %s", got)
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
		AgentTCPPort:   19090,
	}, bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}
	if response.TransferID != "transfer-1" || response.State != "done" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestHTTPPeerTransportSendHeartbeatPostsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/heartbeat" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var request HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("unexpected decode error: %v", err)
		}
		if request.SenderDeviceID != "local-1" || request.AgentTCPPort != 19090 {
			t.Fatalf("unexpected heartbeat payload: %#v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{
			ResponderDeviceID:   "peer-9",
			ResponderDeviceName: "peer-nine",
			AgentTCPPort:        19090,
			ReceivedAtRFC3339:   time.Now().UTC().Format(time.RFC3339Nano),
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	response, err := transport.SendHeartbeat(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, HeartbeatRequest{
		SenderDeviceID: "local-1",
		SentAtRFC3339:  time.Now().UTC().Format(time.RFC3339Nano),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("unexpected heartbeat error: %v", err)
	}
	if response.ResponderDeviceID != "peer-9" || response.AgentTCPPort != 19090 {
		t.Fatalf("unexpected heartbeat response: %#v", response)
	}
}

func TestHTTPPeerTransportSendFileUsesLargeCopyBuffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("unexpected request body read error: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(FileTransferResponse{
			TransferID: "transfer-buffer",
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
		TransferID:     "transfer-buffer",
		MessageID:      "msg-buffer",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       5,
	}, &minimumReadBufferReader{
		remaining:     []byte("hello"),
		minBufferSize: 128 << 10,
	})
	if err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}
	if response.TransferID != "transfer-buffer" || response.State != "done" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestPeerTransportClientForcesHTTP2WhenTLSConfigIsCustomized(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}

	client := NewPeerHTTPClient(tlsConfig)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.TLSClientConfig != tlsConfig {
		t.Fatal("expected custom TLS config to be preserved on transport")
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("expected ForceAttemptHTTP2 to be enabled")
	}
	if transport.MaxIdleConns != 64 {
		t.Fatalf("expected MaxIdleConns=64, got %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 32 {
		t.Fatalf("expected MaxIdleConnsPerHost=32, got %d", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 16 {
		t.Fatalf("expected MaxConnsPerHost=16, got %d", transport.MaxConnsPerHost)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("expected IdleConnTimeout=90s, got %s", transport.IdleConnTimeout)
	}
	if transport.ReadBufferSize != multipartCopyBufferSize {
		t.Fatalf("expected ReadBufferSize=%d, got %d", multipartCopyBufferSize, transport.ReadBufferSize)
	}
	if transport.WriteBufferSize != multipartCopyBufferSize {
		t.Fatalf("expected WriteBufferSize=%d, got %d", multipartCopyBufferSize, transport.WriteBufferSize)
	}
}

func TestHTTPPeerTransportStartsTransferSessionPostsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var request TransferSessionStartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode session start request: %v", err)
		}
		if request.TransferID != "transfer-session-1" || request.FileSize != 11 {
			t.Fatalf("unexpected session start request: %#v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferSessionStartResponse{
			SessionID:             "session-1",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        8,
			AdaptivePolicyVersion: "v1",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	response, err := transport.StartTransferSession(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferSessionStartRequest{
		TransferID:     "transfer-session-1",
		MessageID:      "msg-session-1",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       11,
	})
	if err != nil {
		t.Fatalf("start transfer session: %v", err)
	}
	if response.SessionID != "session-1" {
		t.Fatalf("unexpected start response: %#v", response)
	}
}

func TestHTTPPeerTransportUploadsTransferPartPostsMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferSessionStartResponse{
			SessionID:             "session-1",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        8,
			AdaptivePolicyVersion: "v1",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	startResponse, err := transport.StartTransferSession(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferSessionStartRequest{
		TransferID:     "transfer-1",
		MessageID:      "msg-1",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       5,
	})
	if err != nil {
		t.Fatalf("start transfer session: %v", err)
	}
	if startResponse.SessionID != "session-1" {
		t.Fatalf("unexpected session start response: %#v", startResponse)
	}
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/part" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("sessionId"); got != "session-1" {
			t.Fatalf("unexpected session id: %s", got)
		}
		if got := r.FormValue("partIndex"); got != "1" {
			t.Fatalf("unexpected part index: %s", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read part content: %v", err)
		}
		if string(content) != "world" {
			t.Fatalf("unexpected part content: %q", string(content))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferPartResponse{
			SessionID:     "session-1",
			PartIndex:     1,
			BytesWritten:  5,
			BytesReceived: 10,
		})
	})

	response, err := transport.UploadTransferPart(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferPartRequest{
		SessionID:  "session-1",
		TransferID: "transfer-1",
		PartIndex:  1,
		Offset:     5,
		Length:     5,
	}, bytes.NewReader([]byte("world")))
	if err != nil {
		t.Fatalf("upload transfer part: %v", err)
	}
	if response.BytesWritten != 5 || response.BytesReceived != 10 {
		t.Fatalf("unexpected part response: %#v", response)
	}
}

func TestHTTPPeerTransportUploadsTransferPartPostsRawBodyForFastSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferSessionStartResponse{
			SessionID:             "session-fast",
			ChunkSize:             8 << 20,
			InitialParallelism:    4,
			MaxParallelism:        8,
			AdaptivePolicyVersion: "v2-lan-fast",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	startResponse, err := transport.StartTransferSession(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferSessionStartRequest{
		TransferID:     "transfer-fast",
		MessageID:      "msg-fast",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       5,
	})
	if err != nil {
		t.Fatalf("start transfer session: %v", err)
	}
	if startResponse.SessionID != "session-fast" {
		t.Fatalf("unexpected session start response: %#v", startResponse)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/part" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("expected raw octet-stream upload, got %q", got)
		}
		if got := r.Header.Get("X-Message-Share-Session-Id"); got != "session-fast" {
			t.Fatalf("unexpected session id header: %q", got)
		}
		if got := r.Header.Get("X-Message-Share-Part-Index"); got != "1" {
			t.Fatalf("unexpected part index header: %q", got)
		}
		content, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read raw part content: %v", err)
		}
		if string(content) != "world" {
			t.Fatalf("unexpected raw part content: %q", string(content))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferPartResponse{
			SessionID:     "session-fast",
			PartIndex:     1,
			BytesWritten:  5,
			BytesReceived: 10,
		})
	})

	response, err := transport.UploadTransferPart(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferPartRequest{
		SessionID:  "session-fast",
		TransferID: "transfer-fast",
		PartIndex:  1,
		Offset:     5,
		Length:     5,
	}, bytes.NewReader([]byte("world")))
	if err != nil {
		t.Fatalf("upload transfer part: %v", err)
	}
	if response.BytesWritten != 5 || response.BytesReceived != 10 {
		t.Fatalf("unexpected part response: %#v", response)
	}
}

func TestHTTPPeerTransportKeepsMultipartForUnknownV2Mode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/start" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferSessionStartResponse{
			SessionID:             "session-future",
			ChunkSize:             8 << 20,
			InitialParallelism:    4,
			MaxParallelism:        8,
			AdaptivePolicyVersion: "v2-next-experiment",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	if _, err := transport.StartTransferSession(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferSessionStartRequest{
		TransferID:     "transfer-future",
		MessageID:      "msg-future",
		SenderDeviceID: "local-1",
		FileName:       "hello.txt",
		FileSize:       5,
	}); err != nil {
		t.Fatalf("start transfer session: %v", err)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data;") {
			t.Fatalf("expected unknown v2 mode to stay on multipart path, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferPartResponse{
			SessionID:     "session-future",
			PartIndex:     1,
			BytesWritten:  5,
			BytesReceived: 5,
		})
	})

	if _, err := transport.UploadTransferPart(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferPartRequest{
		SessionID:  "session-future",
		TransferID: "transfer-future",
		PartIndex:  1,
		Offset:     0,
		Length:     5,
	}, bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatalf("upload transfer part: %v", err)
	}
}

func TestHTTPPeerTransportCompletesTransferSessionPostsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/peer/transfers/session/complete" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var request TransferSessionCompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode complete request: %v", err)
		}
		if request.PartCount != 3 || request.FileSHA256 == "" {
			t.Fatalf("unexpected complete request: %#v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferSessionCompleteResponse{
			TransferID: "transfer-1",
			State:      "done",
		})
	}))
	defer server.Close()

	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     "http",
	})

	response, err := transport.CompleteTransferSession(context.Background(), discovery.PeerRecord{
		LastKnownAddr: server.Listener.Addr().String(),
	}, TransferSessionCompleteRequest{
		SessionID:  "session-1",
		TransferID: "transfer-1",
		TotalSize:  11,
		PartCount:  3,
		FileSHA256: sessionTestSHA256Hex([]byte("hello world")),
	})
	if err != nil {
		t.Fatalf("complete transfer session: %v", err)
	}
	if response.State != "done" {
		t.Fatalf("unexpected complete response: %#v", response)
	}
}

func TestLANPeerTransportClientDisablesHTTP2AndRaisesConnectionPool(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}

	client := NewLANPeerHTTPClient(tlsConfig)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.TLSClientConfig != tlsConfig {
		t.Fatal("expected custom TLS config to be preserved on transport")
	}
	if transport.ForceAttemptHTTP2 {
		t.Fatal("expected LAN client to keep HTTP/1.1 for high-throughput transfer lanes")
	}
	if transport.MaxConnsPerHost < 32 {
		t.Fatalf("expected LAN client to raise MaxConnsPerHost, got %d", transport.MaxConnsPerHost)
	}
	if transport.ReadBufferSize < multipartCopyBufferSize {
		t.Fatalf("expected LAN client read buffer >= %d, got %d", multipartCopyBufferSize, transport.ReadBufferSize)
	}
	if transport.WriteBufferSize < multipartCopyBufferSize {
		t.Fatalf("expected LAN client write buffer >= %d, got %d", multipartCopyBufferSize, transport.WriteBufferSize)
	}
}

func TestHTTPPeerTransportReusesTransferClientFactoryResultPerFingerprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/peer/transfers/session/part" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferPartResponse{
			SessionID:     "session-1",
			PartIndex:     0,
			BytesWritten:  5,
			BytesReceived: 5,
		})
	}))
	defer server.Close()

	factoryCalls := 0
	transport := NewHTTPPeerTransport(HTTPPeerTransportOptions{
		Scheme: "http",
		TransferClientFactory: func(expectedFingerprint string) *http.Client {
			factoryCalls++
			if expectedFingerprint != "fingerprint-pinned" {
				t.Fatalf("unexpected fingerprint: %q", expectedFingerprint)
			}
			return server.Client()
		},
	})

	peer := discovery.PeerRecord{
		LastKnownAddr:     server.Listener.Addr().String(),
		PinnedFingerprint: "fingerprint-pinned",
	}
	for index := 0; index < 2; index++ {
		if _, err := transport.UploadTransferPart(context.Background(), peer, TransferPartRequest{
			SessionID:  "session-1",
			TransferID: "transfer-1",
			PartIndex:  index,
			Offset:     int64(index * 5),
			Length:     5,
		}, bytes.NewReader([]byte("world"))); err != nil {
			t.Fatalf("upload transfer part %d: %v", index, err)
		}
	}

	if factoryCalls != 1 {
		t.Fatalf("expected transfer client factory to be reused for same fingerprint, got %d calls", factoryCalls)
	}
}

func TestHTTPPeerTransportUsesPinnedFingerprintForTextRequests(t *testing.T) {
	var capturedFingerprints []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request TextMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("unexpected decode error: %v", err)
		}
		if request.AgentTCPPort != 19090 {
			t.Fatalf("unexpected agent tcp port: %#v", request)
		}
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
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("unexpected send text error: %v", err)
	}
	if len(capturedFingerprints) != 1 || capturedFingerprints[0] != "fingerprint-pinned" {
		t.Fatalf("unexpected pinned fingerprints: %#v", capturedFingerprints)
	}
}

func TestPeerHTTPServerDelegatesTextRequestsWithAgentPort(t *testing.T) {
	handler := &fakePairingHandler{}

	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	body, err := json.Marshal(TextMessageRequest{
		MessageID:      "msg-2",
		SenderDeviceID: "peer-2",
		Body:           "hello",
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/peer/messages/text", bytes.NewReader(body))
	request.TLS = state

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if handler.textRequest.AgentTCPPort != 19090 {
		t.Fatalf("expected delegated text request to keep agent port, got %#v", handler.textRequest)
	}
}

func TestPeerHTTPServerDelegatesHeartbeatRequests(t *testing.T) {
	handler := &fakePairingHandler{
		heartbeatResponse: HeartbeatResponse{
			ResponderDeviceID:   "peer-2",
			ResponderDeviceName: "peer-two",
			AgentTCPPort:        19090,
			ReceivedAtRFC3339:   time.Now().UTC().Format(time.RFC3339Nano),
		},
	}

	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	body, err := json.Marshal(HeartbeatRequest{
		SenderDeviceID: "peer-2",
		SentAtRFC3339:  time.Now().UTC().Format(time.RFC3339Nano),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/peer/heartbeat", bytes.NewReader(body))
	request.TLS = state
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if handler.heartbeatRequest.SenderDeviceID != "peer-2" || handler.heartbeatRequest.AgentTCPPort != 19090 {
		t.Fatalf("unexpected delegated heartbeat request: %#v", handler.heartbeatRequest)
	}
}

func TestPeerHTTPServerStartsTransferSession(t *testing.T) {
	handler := &fakePairingHandler{
		sessionStartResponse: TransferSessionStartResponse{
			SessionID:             "session-1",
			ChunkSize:             8 << 20,
			InitialParallelism:    2,
			MaxParallelism:        8,
			AdaptivePolicyVersion: "v1",
		},
	}

	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	body, err := json.Marshal(TransferSessionStartRequest{
		TransferID:     "transfer-session-1",
		MessageID:      "msg-session-1",
		SenderDeviceID: "peer-2",
		FileName:       "hello.txt",
		FileSize:       int64(len("hello world")),
		FileSHA256:     sessionTestSHA256Hex([]byte("hello world")),
		AgentTCPPort:   19090,
	})
	if err != nil {
		t.Fatalf("marshal session start: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/peer/transfers/session/start", bytes.NewReader(body))
	request.TLS = state
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if handler.sessionStartRequest.TransferID != "transfer-session-1" {
		t.Fatalf("unexpected delegated session start request: %#v", handler.sessionStartRequest)
	}
}

func TestPeerHTTPServerDelegatesRawTransferPartRequests(t *testing.T) {
	handler := &fakePairingHandler{
		sessionPartResponse: TransferPartResponse{
			SessionID:     "session-fast",
			PartIndex:     1,
			BytesWritten:  5,
			BytesReceived: 5,
		},
	}

	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	request := httptest.NewRequest(http.MethodPost, "/peer/transfers/session/part", bytes.NewReader([]byte("world")))
	request.TLS = state
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set(transferHeaderSessionID, "session-fast")
	request.Header.Set(transferHeaderTransferID, "transfer-fast")
	request.Header.Set(transferHeaderPartIndex, "1")
	request.Header.Set(transferHeaderOffset, "5")
	request.Header.Set(transferHeaderLength, "5")

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if handler.sessionPartRequest.SessionID != "session-fast" {
		t.Fatalf("unexpected delegated session id: %#v", handler.sessionPartRequest)
	}
	if handler.sessionPartRequest.TransferID != "transfer-fast" {
		t.Fatalf("unexpected delegated transfer id: %#v", handler.sessionPartRequest)
	}
	if handler.sessionPartRequest.PartIndex != 1 || handler.sessionPartRequest.Offset != 5 || handler.sessionPartRequest.Length != 5 {
		t.Fatalf("unexpected delegated part request: %#v", handler.sessionPartRequest)
	}
	if string(handler.sessionPartContent) != "world" {
		t.Fatalf("unexpected delegated part content: %q", string(handler.sessionPartContent))
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
	_ = writer.WriteField("agentTcpPort", "19090")
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
	if handler.fileRequest.AgentTCPPort != 19090 {
		t.Fatalf("expected delegated file request to keep agent port, got %#v", handler.fileRequest)
	}
}

func TestPeerHTTPServerStreamsFileTransferToHandlerBeforeMultipartCloses(t *testing.T) {
	handler := &fakePairingHandler{
		fileResponse: FileTransferResponse{
			TransferID: "transfer-stream",
			State:      "done",
		},
		fileReadStarted: make(chan struct{}),
	}

	server := NewPeerHTTPServer(handler)
	state, _ := newPeerTLSState(t)

	bodyReader, bodyWriter := io.Pipe()
	writer := multipart.NewWriter(bodyWriter)

	request, err := http.NewRequest(http.MethodPost, "/peer/transfers/file", bodyReader)
	if err != nil {
		t.Fatalf("unexpected request error: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.TLS = state

	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.ServeHTTP(recorder, request)
		close(done)
	}()

	for key, value := range map[string]string{
		"transferId":     "transfer-stream",
		"messageId":      "msg-stream",
		"senderDeviceId": "peer-2",
		"fileName":       "hello.txt",
		"fileSize":       "11",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("unexpected field write error: %v", err)
		}
	}

	part, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("unexpected create form file error: %v", err)
	}
	if _, err := io.WriteString(part, "hello"); err != nil {
		t.Fatalf("unexpected file prefix write error: %v", err)
	}

	select {
	case <-handler.fileReadStarted:
	case <-time.After(300 * time.Millisecond):
		_ = writer.Close()
		_ = bodyWriter.Close()
		<-done
		t.Fatal("expected handler to start reading file content before multipart body closed")
	}

	if _, err := io.WriteString(part, " world"); err != nil {
		t.Fatalf("unexpected file suffix write error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("unexpected writer close error: %v", err)
	}
	if err := bodyWriter.Close(); err != nil {
		t.Fatalf("unexpected body close error: %v", err)
	}

	<-done

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if string(handler.fileContent) != "hello world" {
		t.Fatalf("unexpected streamed file content: %q", string(handler.fileContent))
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
	startRequest            PairingStartRequest
	startResponse           PairingStartResponse
	confirmRequest          PairingConfirmRequest
	confirmResponse         PairingConfirmResponse
	heartbeatRequest        HeartbeatRequest
	heartbeatResponse       HeartbeatResponse
	sessionStartRequest     TransferSessionStartRequest
	sessionStartResponse    TransferSessionStartResponse
	sessionPartRequest      TransferPartRequest
	sessionPartContent      []byte
	sessionPartResponse     TransferPartResponse
	sessionCompleteRequest  TransferSessionCompleteRequest
	sessionCompleteResponse TransferSessionCompleteResponse
	textRequest             TextMessageRequest
	fileRequest             FileTransferRequest
	fileResponse            FileTransferResponse
	fileContent             []byte
	fileReadStarted         chan struct{}
}

func (f *fakePairingHandler) AcceptIncomingPairing(_ context.Context, request PairingStartRequest) (PairingStartResponse, error) {
	f.startRequest = request
	return f.startResponse, nil
}

func (f *fakePairingHandler) AcceptPairingConfirm(_ context.Context, request PairingConfirmRequest) (PairingConfirmResponse, error) {
	f.confirmRequest = request
	return f.confirmResponse, nil
}

func (f *fakePairingHandler) AcceptHeartbeat(_ context.Context, request HeartbeatRequest) (HeartbeatResponse, error) {
	f.heartbeatRequest = request
	return f.heartbeatResponse, nil
}

func (f *fakePairingHandler) StartIncomingTransferSession(_ context.Context, request TransferSessionStartRequest) (TransferSessionStartResponse, error) {
	f.sessionStartRequest = request
	return f.sessionStartResponse, nil
}

func (f *fakePairingHandler) AcceptIncomingTransferPart(_ context.Context, request TransferPartRequest, content io.Reader) (TransferPartResponse, error) {
	f.sessionPartRequest = request
	if content != nil {
		data, err := io.ReadAll(content)
		if err != nil {
			return TransferPartResponse{}, err
		}
		f.sessionPartContent = data
	}
	return f.sessionPartResponse, nil
}

func (f *fakePairingHandler) CompleteIncomingTransferSession(_ context.Context, request TransferSessionCompleteRequest) (TransferSessionCompleteResponse, error) {
	f.sessionCompleteRequest = request
	return f.sessionCompleteResponse, nil
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

	var data []byte
	if f.fileReadStarted != nil {
		prefix := make([]byte, len("hello"))
		if _, err := io.ReadFull(content, prefix); err != nil {
			return FileTransferResponse{}, err
		}
		if !bytes.Equal(prefix, []byte("hello")) {
			return FileTransferResponse{}, errors.New("unexpected streamed prefix")
		}
		close(f.fileReadStarted)
		data = append(data, prefix...)
	}

	remaining, err := io.ReadAll(content)
	if err != nil {
		return FileTransferResponse{}, err
	}
	data = append(data, remaining...)
	f.fileContent = data
	return f.fileResponse, nil
}

type minimumReadBufferReader struct {
	remaining     []byte
	minBufferSize int
}

func (r *minimumReadBufferReader) Read(buffer []byte) (int, error) {
	if len(buffer) < r.minBufferSize {
		return 0, errors.New("copy buffer too small")
	}
	if len(r.remaining) == 0 {
		return 0, io.EOF
	}

	written := copy(buffer, r.remaining)
	r.remaining = r.remaining[written:]
	return written, nil
}

func sessionTestSHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
