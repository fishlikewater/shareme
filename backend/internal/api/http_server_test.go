package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gorilla/websocket"

	"message-share/backend/internal/app"
)

func TestBootstrapReturnsLocalDeviceAndHealth(t *testing.T) {
	bus := NewEventBus()
	server := NewHTTPServer(StubAppService(), bus)
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" || body[0] != '{' {
		t.Fatalf("expected json body, got %q", body)
	}
}

func TestRootServesEmbeddedWebUI(t *testing.T) {
	server := NewHTTPServer(StubAppService(), NewEventBus(), testWebAssets())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "message-share-app") {
		t.Fatalf("expected embedded index page, got %q", rec.Body.String())
	}
}

func TestClientRouteFallsBackToEmbeddedIndex(t *testing.T) {
	server := NewHTTPServer(StubAppService(), NewEventBus(), testWebAssets())
	req := httptest.NewRequest(http.MethodGet, "/conversations/peer-1", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "message-share-app") {
		t.Fatalf("expected embedded index fallback, got %q", rec.Body.String())
	}
}

func TestMissingEmbeddedAssetReturnsNotFound(t *testing.T) {
	server := NewHTTPServer(StubAppService(), NewEventBus(), testWebAssets())
	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestBootstrapAllowsLoopbackOrigin(t *testing.T) {
	server := NewHTTPServer(StubAppService(), NewEventBus())
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	req.Header.Set("Origin", "http://127.0.0.1:52350")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:52350" {
		t.Fatalf("expected allow origin header, got %#v", rec.Header())
	}
}

func TestBootstrapIncludesCurrentEventSeq(t *testing.T) {
	bus := NewEventBus()
	bus.Publish("peer.updated", map[string]string{"id": "a"})
	server := NewHTTPServer(StubAppService(), bus)
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "\"eventSeq\":1") {
		t.Fatalf("expected event sequence in bootstrap, got %q", rec.Body.String())
	}
}

func TestEventsEndpointReturnsEventsAfterSequence(t *testing.T) {
	bus := NewEventBus()
	bus.Publish("peer.updated", map[string]string{"id": "a"})
	server := NewHTTPServer(StubAppService(), bus)
	req := httptest.NewRequest(http.MethodGet, "/api/events?afterSeq=0", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	if !strings.Contains(string(body), "peer.updated") {
		t.Fatalf("expected events payload, got %q", string(body))
	}
}

func TestEventsStreamReplaysBufferedEventsAndPushesNewOnes(t *testing.T) {
	bus := NewEventBus()
	bus.Publish("peer.updated", map[string]string{"id": "a"})

	server := httptest.NewServer(NewHTTPServer(StubAppService(), bus).Handler())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/events/stream?lastEventSeq=0"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	defer conn.Close()

	var replayed Event
	if err := conn.ReadJSON(&replayed); err != nil {
		t.Fatalf("unexpected replay read error: %v", err)
	}
	if replayed.EventSeq != 1 || replayed.Kind != "peer.updated" {
		t.Fatalf("unexpected replayed event: %#v", replayed)
	}

	bus.Publish("health.updated", map[string]string{"status": "ok"})

	var pushed Event
	if err := conn.ReadJSON(&pushed); err != nil {
		t.Fatalf("unexpected pushed read error: %v", err)
	}
	if pushed.EventSeq != 2 || pushed.Kind != "health.updated" {
		t.Fatalf("unexpected pushed event: %#v", pushed)
	}
}

func TestStartPairingEndpointReturnsPairingSnapshot(t *testing.T) {
	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/pairings", strings.NewReader(`{"peerDeviceId":"peer-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"pairingId":"pair-1"`) {
		t.Fatalf("unexpected pairing response: %q", rec.Body.String())
	}
}

func TestStartPairingEndpointRejectsNonLoopbackOrigin(t *testing.T) {
	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/pairings", strings.NewReader(`{"peerDeviceId":"peer-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestConfirmPairingEndpointReturnsUpdatedPairingSnapshot(t *testing.T) {
	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/pairings/pair-1/confirm", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"confirmed"`) {
		t.Fatalf("unexpected confirm response: %q", rec.Body.String())
	}
}

func TestSendTextMessageEndpointReturnsMessageSnapshot(t *testing.T) {
	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/messages/text", strings.NewReader(`{"peerDeviceId":"peer-1","body":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"direction":"outgoing"`) || !strings.Contains(rec.Body.String(), `"body":"hello"`) {
		t.Fatalf("unexpected message response: %q", rec.Body.String())
	}
}

func TestMessagesEndpointHandlesLoopbackPreflight(t *testing.T) {
	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodOptions, "/api/messages/text", nil)
	req.Header.Set("Origin", "http://localhost:52350")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:52350" {
		t.Fatalf("expected allow origin header, got %#v", rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), http.MethodPost) {
		t.Fatalf("expected allow methods header, got %#v", rec.Header())
	}
}

func TestSendFileEndpointReturnsTransferSnapshot(t *testing.T) {
	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("peerDeviceId", "peer-1"); err != nil {
		t.Fatalf("unexpected field error: %v", err)
	}
	part, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("unexpected file field error: %v", err)
	}
	if _, err := io.WriteString(part, "hello"); err != nil {
		t.Fatalf("unexpected file write error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("unexpected writer close error: %v", err)
	}

	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/transfers/file", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"transferId":"transfer-1"`) || !strings.Contains(rec.Body.String(), `"state":"done"`) {
		t.Fatalf("unexpected transfer response: %q", rec.Body.String())
	}
}

func TestSendFileEndpointRejectsNonLoopbackOrigin(t *testing.T) {
	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("peerDeviceId", "peer-1"); err != nil {
		t.Fatalf("unexpected field error: %v", err)
	}
	part, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("unexpected file field error: %v", err)
	}
	if _, err := io.WriteString(part, "hello"); err != nil {
		t.Fatalf("unexpected file write error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("unexpected writer close error: %v", err)
	}

	server := NewHTTPServer(pairingTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/transfers/file", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

type pairingTestService struct{}

func (pairingTestService) Bootstrap() (app.BootstrapSnapshot, error) {
	return StubAppService().Bootstrap()
}

func (pairingTestService) StartPairing(_ context.Context, peerDeviceID string) (app.PairingSnapshot, error) {
	return app.PairingSnapshot{
		PairingID:      "pair-1",
		PeerDeviceID:   peerDeviceID,
		PeerDeviceName: "meeting-room",
		ShortCode:      "123456",
		Status:         "pending",
	}, nil
}

func (pairingTestService) ConfirmPairing(_ context.Context, pairingID string) (app.PairingSnapshot, error) {
	return app.PairingSnapshot{
		PairingID:      pairingID,
		PeerDeviceID:   "peer-1",
		PeerDeviceName: "meeting-room",
		ShortCode:      "123456",
		Status:         "confirmed",
	}, nil
}

func (pairingTestService) SendTextMessage(_ context.Context, peerDeviceID string, body string) (app.MessageSnapshot, error) {
	return app.MessageSnapshot{
		MessageID:      "msg-1",
		ConversationID: "conv-" + peerDeviceID,
		Direction:      "outgoing",
		Kind:           "text",
		Body:           body,
		Status:         "sent",
	}, nil
}

func (pairingTestService) SendFile(_ context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (app.TransferSnapshot, error) {
	body, err := io.ReadAll(content)
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	if peerDeviceID != "peer-1" {
		return app.TransferSnapshot{}, fmt.Errorf("unexpected peer: %s", peerDeviceID)
	}
	if fileName != "hello.txt" || fileSize != int64(len(body)) || string(body) != "hello" {
		return app.TransferSnapshot{}, fmt.Errorf("unexpected upload: %s %d %q", fileName, fileSize, string(body))
	}
	return app.TransferSnapshot{
		TransferID: "transfer-1",
		FileName:   fileName,
		State:      "done",
	}, nil
}

func testWebAssets() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!doctype html><html><body>message-share-app</body></html>"),
		},
		"assets/app.js": &fstest.MapFile{
			Data: []byte("console.log('message-share');"),
		},
	}
}
