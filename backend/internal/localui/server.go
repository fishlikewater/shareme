package localui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"shareme/backend/internal/api"
	appruntime "shareme/backend/internal/app"
	"shareme/backend/internal/config"
)

type ServiceDeps struct {
	Service *Service
	Assets  fs.FS
}

type Server struct {
	service *Service
	assets  fs.FS
}

type HostOptions struct {
	Config         config.AppConfig
	Logger         *log.Logger
	Assets         fs.FS
	ResolveRuntime func() RuntimeCommands
	Bus            *api.EventBus
}

type Host struct {
	mu      sync.Mutex
	cfg     config.AppConfig
	logger  *log.Logger
	server  *http.Server
	url     string
	started bool
}

func NewServer(deps ServiceDeps) *Server {
	return &Server{
		service: deps.Service,
		assets:  deps.Assets,
	}
}

func NewHost(opts HostOptions) *Host {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}

	server := NewServer(ServiceDeps{
		Service: NewService(opts.ResolveRuntime, opts.Bus),
		Assets:  opts.Assets,
	})

	return &Host{
		cfg:    opts.Config,
		logger: opts.Logger,
		server: &http.Server{
			Addr:    net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Config.LocalHTTPPort)),
			Handler: server.Handler(),
		},
		url: fmt.Sprintf("http://127.0.0.1:%d/", opts.Config.LocalHTTPPort),
	}
}

func (h *Host) Start(_ context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return nil
	}
	h.started = true
	server := h.server
	logger := h.logger
	h.mu.Unlock()

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		h.mu.Lock()
		h.started = false
		h.mu.Unlock()
		return fmt.Errorf("listen localhost web ui: %w", err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && logger != nil {
			logger.Printf("localhost web ui stopped with error: %v", err)
		}
	}()
	return nil
}

func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	server := h.server
	started := h.started
	h.started = false
	h.mu.Unlock()

	if !started || server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func (h *Host) URL() string {
	if h == nil {
		return ""
	}
	return h.url
}

func (s *Server) Handler() http.Handler {
	return s.loopbackOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.handleAPI(w, r)
			return
		}
		s.handleFrontend(w, r)
	}))
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/bootstrap":
		s.handleBootstrap(w, r)
	case r.URL.Path == "/api/events/stream":
		s.handleEvents(w, r)
	case r.URL.Path == "/api/pairings":
		s.handleStartPairing(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/pairings/") && strings.HasSuffix(r.URL.Path, "/confirm"):
		s.handleConfirmPairing(w, r)
	case r.URL.Path == "/api/local-files/pick":
		s.handlePickLocalFile(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/peers/"):
		s.handlePeerRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/conversations/") && strings.HasSuffix(r.URL.Path, "/messages"):
		s.handleListMessageHistory(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot, err := s.service.Bootstrap()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, struct {
		appruntime.BootstrapSnapshot
		EventSeq int64 `json:"eventSeq"`
	}{
		BootstrapSnapshot: snapshot,
		EventSeq:          s.service.EventSeq(),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	afterSeq, err := parseAfterSeq(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	backlog, stream, unsubscribe := s.service.Subscribe(afterSeq)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, event := range backlog {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-stream:
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleStartPairing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		PeerDeviceID string `json:"peerDeviceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := s.service.StartPairing(r.Context(), request.PeerDeviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleConfirmPairing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pairingID, ok := extractPathValue(r.URL.Path, "/api/pairings/", "/confirm")
	if !ok {
		http.NotFound(w, r)
		return
	}

	snapshot, err := s.service.ConfirmPairing(r.Context(), pairingID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handlePeerRoutes(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages/text"):
		s.handleSendText(w, r)
	case strings.HasSuffix(r.URL.Path, "/transfers/browser-upload"):
		s.handleBrowserUpload(w, r)
	case strings.HasSuffix(r.URL.Path, "/transfers/accelerated"):
		s.handleAccelerated(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSendText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	peerDeviceID, ok := extractPathValue(r.URL.Path, "/api/peers/", "/messages/text")
	if !ok {
		http.NotFound(w, r)
		return
	}

	var request struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := s.service.SendText(r.Context(), peerDeviceID, request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleBrowserUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	peerDeviceID, ok := extractPathValue(r.URL.Path, "/api/peers/", "/transfers/browser-upload")
	if !ok {
		http.NotFound(w, r)
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var fileSize int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch part.FormName() {
		case "fileSize":
			raw, err := io.ReadAll(part)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fileSize, err = strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
			if err != nil {
				http.Error(w, "invalid fileSize", http.StatusBadRequest)
				return
			}
		case "file":
			fileName := part.FileName()
			if fileName == "" {
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}

			snapshot, err := s.service.SendFile(r.Context(), peerDeviceID, fileName, fileSize, part)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, snapshot)
			return
		}
	}

	http.Error(w, "missing file", http.StatusBadRequest)
}

func (s *Server) handlePickLocalFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot, err := s.service.PickLocalFile(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleAccelerated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	peerDeviceID, ok := extractPathValue(r.URL.Path, "/api/peers/", "/transfers/accelerated")
	if !ok {
		http.NotFound(w, r)
		return
	}

	var request struct {
		LocalFileID string `json:"localFileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := s.service.SendAcceleratedFile(r.Context(), peerDeviceID, request.LocalFileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleListMessageHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conversationID, ok := extractPathValue(r.URL.Path, "/api/conversations/", "/messages")
	if !ok {
		http.NotFound(w, r)
		return
	}

	snapshot, err := s.service.ListMessageHistory(r.Context(), conversationID, r.URL.Query().Get("beforeCursor"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.NotFound(w, r)
		return
	}

	assetPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if assetPath == "." || assetPath == "" {
		assetPath = "index.html"
	}

	content, err := fs.ReadFile(s.assets, assetPath)
	if err != nil {
		content, err = fs.ReadFile(s.assets, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		assetPath = "index.html"
	}

	http.ServeContent(w, r, assetPath, time.Time{}, bytes.NewReader(content))
}

func (s *Server) loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemoteAddr(r.RemoteAddr) {
			http.Error(w, "loopback access only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseAfterSeq(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("afterSeq"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid afterSeq")
	}
	return value, nil
}

func writeSSEEvent(w http.ResponseWriter, event api.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func extractPathValue(requestPath string, prefix string, suffix string) (string, bool) {
	if !strings.HasPrefix(requestPath, prefix) || !strings.HasSuffix(requestPath, suffix) {
		return "", false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(requestPath, prefix), suffix)
	value = strings.Trim(value, "/")
	if value == "" {
		return "", false
	}
	return value, true
}
