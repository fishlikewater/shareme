package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBootstrapReturnsLocalDeviceAndHealth(t *testing.T) {
	server := NewHTTPServer(StubAppService())
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
