package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"message-share/backend/internal/app"
)

type localFileTestService struct {
	stubService
	pickLocalFile func(context.Context) (app.LocalFileSnapshot, error)
}

func (s localFileTestService) PickLocalFile(ctx context.Context) (app.LocalFileSnapshot, error) {
	return s.pickLocalFile(ctx)
}

func TestHTTPServerPicksLocalFileFromLoopbackPage(t *testing.T) {
	service := localFileTestService{
		pickLocalFile: func(context.Context) (app.LocalFileSnapshot, error) {
			return app.LocalFileSnapshot{
				LocalFileID:         "lf-1",
				DisplayName:         "demo.bin",
				Size:                128,
				AcceleratedEligible: true,
			}, nil
		},
	}

	server := NewHTTPServer(service, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/local-files/pick", http.NoBody)
	req.Header.Set("Origin", "http://127.0.0.1:52350")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHTTPServerRejectsLocalFilePickFromNonLoopbackOrigin(t *testing.T) {
	server := NewHTTPServer(localFileTestService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/local-files/pick", http.NoBody)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
