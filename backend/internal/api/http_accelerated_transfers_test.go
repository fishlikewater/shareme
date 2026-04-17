package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"message-share/backend/internal/app"
)

type acceleratedTransferTestService struct {
	peerDeviceID string
	localFileID  string
}

func (s *acceleratedTransferTestService) Bootstrap() (app.BootstrapSnapshot, error) {
	return StubAppService().Bootstrap()
}

func (s *acceleratedTransferTestService) StartPairing(_ context.Context, _ string) (app.PairingSnapshot, error) {
	return app.PairingSnapshot{}, nil
}

func (s *acceleratedTransferTestService) ConfirmPairing(_ context.Context, _ string) (app.PairingSnapshot, error) {
	return app.PairingSnapshot{}, nil
}

func (s *acceleratedTransferTestService) SendTextMessage(_ context.Context, _ string, _ string) (app.MessageSnapshot, error) {
	return app.MessageSnapshot{}, nil
}

func (s *acceleratedTransferTestService) SendFile(_ context.Context, _ string, _ string, _ int64, _ io.Reader) (app.TransferSnapshot, error) {
	return app.TransferSnapshot{}, nil
}

func (s *acceleratedTransferTestService) PickLocalFile(_ context.Context) (app.LocalFileSnapshot, error) {
	return app.LocalFileSnapshot{}, nil
}

func (s *acceleratedTransferTestService) SendAcceleratedFile(_ context.Context, peerDeviceID string, localFileID string) (app.TransferSnapshot, error) {
	s.peerDeviceID = peerDeviceID
	s.localFileID = localFileID
	return app.TransferSnapshot{
		TransferID: "transfer-accelerated-1",
		MessageID:  "msg-accelerated-1",
		FileName:   "demo.bin",
		State:      "sending",
	}, nil
}

func (s *acceleratedTransferTestService) ListMessageHistory(_ context.Context, conversationID string, _ string) (app.MessageHistoryPageSnapshot, error) {
	return app.MessageHistoryPageSnapshot{
		ConversationID: conversationID,
		Messages:       []app.MessageSnapshot{},
	}, nil
}

func TestAcceleratedTransferEndpointReturnsTransferSnapshot(t *testing.T) {
	service := &acceleratedTransferTestService{}
	server := NewHTTPServer(service, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/transfers/accelerated", strings.NewReader(`{"peerDeviceId":"peer-1","localFileId":"lf-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"transferId":"transfer-accelerated-1"`) {
		t.Fatalf("unexpected accelerated transfer response: %q", rec.Body.String())
	}
	if service.peerDeviceID != "peer-1" || service.localFileID != "lf-1" {
		t.Fatalf("unexpected accelerated transfer args: peer=%q localFile=%q", service.peerDeviceID, service.localFileID)
	}
}
