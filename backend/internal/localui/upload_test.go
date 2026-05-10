package localui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shareme/backend/internal/api"
	appruntime "shareme/backend/internal/app"
)

type uploadCommands struct{}

func (uploadCommands) Bootstrap() (appruntime.BootstrapSnapshot, error) {
	return fakeCommands{}.Bootstrap()
}

func (uploadCommands) StartPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}

func (uploadCommands) ConfirmPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}

func (uploadCommands) SendTextMessage(context.Context, string, string) (appruntime.MessageSnapshot, error) {
	return appruntime.MessageSnapshot{}, nil
}

func (uploadCommands) PickLocalFile(context.Context) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{LocalFileID: "lf-1", DisplayName: "demo.bin"}, nil
}

func (uploadCommands) SendAcceleratedFile(context.Context, string, string) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{TransferID: "tx-acc-1"}, nil
}

func (uploadCommands) ListMessageHistory(context.Context, string, string) (appruntime.MessageHistoryPageSnapshot, error) {
	return appruntime.MessageHistoryPageSnapshot{}, nil
}

func (uploadCommands) SendFile(_ context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (appruntime.TransferSnapshot, error) {
	body, err := io.ReadAll(content)
	if err != nil {
		return appruntime.TransferSnapshot{}, err
	}
	if peerDeviceID != "peer-1" || fileName != "demo.txt" || fileSize != 5 || string(body) != "hello" {
		panic("unexpected upload payload")
	}
	return appruntime.TransferSnapshot{TransferID: "tx-1", FileName: fileName}, nil
}

func TestBrowserUploadStreamsMultipartFileToRuntimeService(t *testing.T) {
	service := NewService(func() RuntimeCommands { return uploadCommands{} }, api.NewEventBus())
	server := NewServer(ServiceDeps{Service: service})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("fileSize", "5"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	part, err := writer.CreateFormFile("file", "demo.txt")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/peers/peer-1/transfers/browser-upload", &body)
	req.RemoteAddr = "127.0.0.1:34567"
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

type unsupportedPickCommands struct{ uploadCommands }

func (unsupportedPickCommands) PickLocalFile(context.Context) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{}, errors.New("local file picker unsupported on linux")
}

func TestPickLocalFileReturnsUnsupportedErrorVerbatim(t *testing.T) {
	service := NewService(func() RuntimeCommands { return unsupportedPickCommands{} }, api.NewEventBus())
	server := NewServer(ServiceDeps{Service: service})

	req := httptest.NewRequest(http.MethodPost, "/api/local-files/pick", nil)
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported") {
		t.Fatalf("expected unsupported message, got %q", rec.Body.String())
	}
}
