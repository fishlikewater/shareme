package api

import (
	"encoding/json"
	"net/http"

	"message-share/backend/internal/app"
)

type HTTPServer struct {
	app app.Service
	mux *http.ServeMux
}

func NewHTTPServer(appService app.Service) *HTTPServer {
	server := &HTTPServer{
		app: appService,
		mux: http.NewServeMux(),
	}
	server.mux.HandleFunc("/api/bootstrap", server.handleBootstrap)
	server.mux.HandleFunc("/api/health", server.handleHealth)
	return server
}

func (s *HTTPServer) Handler() http.Handler {
	return s.mux
}

func (s *HTTPServer) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.app.Bootstrap())
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

type stubService struct{}

func StubAppService() app.Service {
	return stubService{}
}

func (stubService) Bootstrap() app.BootstrapSnapshot {
	return app.BootstrapSnapshot{
		LocalDeviceName: "办公室电脑",
		Health: map[string]any{
			"status":     "ok",
			"agentPort":  19090,
			"discovery":  "bootstrap-pending",
			"localAPIUp": true,
		},
		Peers: []any{},
	}
}
