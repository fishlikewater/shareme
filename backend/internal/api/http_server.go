package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"

	"message-share/backend/internal/app"
	"message-share/backend/internal/diagnostics"
)

type HTTPServer struct {
	app       app.Service
	bus       *EventBus
	mux       *http.ServeMux
	webAssets fs.FS
}

var eventStreamUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return isLoopbackOriginAllowed(r.Header.Get("Origin"))
	},
}

func NewHTTPServer(appService app.Service, eventBus *EventBus, webAssets ...fs.FS) *HTTPServer {
	if eventBus == nil {
		eventBus = NewEventBus()
	}

	var resolvedWebAssets fs.FS
	if len(webAssets) > 0 {
		resolvedWebAssets = webAssets[0]
	}

	server := &HTTPServer{
		app:       appService,
		bus:       eventBus,
		mux:       http.NewServeMux(),
		webAssets: resolvedWebAssets,
	}
	server.mux.HandleFunc("/api/bootstrap", server.handleBootstrap)
	server.mux.HandleFunc("/api/health", server.handleHealth)
	server.mux.HandleFunc("/api/events", server.handleEvents)
	server.mux.HandleFunc("/api/events/stream", server.handleEventStream)
	server.mux.HandleFunc("/api/pairings", server.handlePairings)
	server.mux.HandleFunc("/api/pairings/", server.handlePairingCommands)
	server.mux.HandleFunc("/api/messages/text", server.handleMessages)
	server.mux.HandleFunc("/api/transfers/file", server.handleFileTransfers)
	return server
}

func (s *HTTPServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowLoopbackBrowserAccess(w, r) {
			return
		}
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			s.mux.ServeHTTP(w, r)
			return
		}
		if s.serveWebUI(w, r) {
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) serveWebUI(w http.ResponseWriter, r *http.Request) bool {
	if s.webAssets == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}

	requestPath := resolveWebAssetPath(r.URL.Path)
	if requestPath == "" {
		return s.serveEmbeddedAsset(w, r, "index.html")
	}

	if fileExists(s.webAssets, requestPath) {
		return s.serveEmbeddedAsset(w, r, requestPath)
	}
	if hasFileExtension(requestPath) {
		http.NotFound(w, r)
		return true
	}
	return s.serveEmbeddedAsset(w, r, "index.html")
}

func (s *HTTPServer) serveEmbeddedAsset(w http.ResponseWriter, r *http.Request, assetPath string) bool {
	if !fileExists(s.webAssets, assetPath) {
		if assetPath == "index.html" {
			http.Error(w, "web ui assets not built", http.StatusServiceUnavailable)
			return true
		}
		http.NotFound(w, r)
		return true
	}

	http.ServeFileFS(w, r, s.webAssets, assetPath)
	return true
}

func (s *HTTPServer) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot, err := s.app.Bootstrap()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"localDeviceName": snapshot.LocalDeviceName,
		"health":          snapshot.Health,
		"peers":           snapshot.Peers,
		"pairings":        snapshot.Pairings,
		"conversations":   snapshot.Conversations,
		"messages":        snapshot.Messages,
		"transfers":       snapshot.Transfers,
		"eventSeq":        s.bus.LastSeq(),
	})
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot, err := s.app.Bootstrap()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot.Health)
}

func (s *HTTPServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	afterSeq := int64(0)
	if raw := r.URL.Query().Get("afterSeq"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid afterSeq", http.StatusBadRequest)
			return
		}
		afterSeq = value
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events":       s.bus.Since(afterSeq),
		"lastEventSeq": s.bus.LastSeq(),
	})
}

func (s *HTTPServer) handleEventStream(w http.ResponseWriter, r *http.Request) {
	afterSeq, ok := parseAfterSeq(w, r)
	if !ok {
		return
	}

	conn, err := eventStreamUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	backlog, stream, unsubscribe := s.bus.Subscribe(afterSeq)
	defer unsubscribe()

	for _, event := range backlog {
		if err := conn.WriteJSON(event); err != nil {
			return
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-stream:
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		}
	}
}

func (s *HTTPServer) handlePairings(w http.ResponseWriter, r *http.Request) {
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
	if strings.TrimSpace(request.PeerDeviceID) == "" {
		http.Error(w, "peerDeviceId is required", http.StatusBadRequest)
		return
	}

	pairing, err := s.app.StartPairing(r.Context(), request.PeerDeviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pairing)
}

func (s *HTTPServer) handlePairingCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pairingID, action, ok := parsePairingAction(r.URL.Path)
	if !ok || action != "confirm" {
		http.NotFound(w, r)
		return
	}

	pairing, err := s.app.ConfirmPairing(r.Context(), pairingID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pairing)
}

func (s *HTTPServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		PeerDeviceID string `json:"peerDeviceId"`
		Body         string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.PeerDeviceID) == "" || strings.TrimSpace(request.Body) == "" {
		http.Error(w, "peerDeviceId and body are required", http.StatusBadRequest)
		return
	}

	message, err := s.app.SendTextMessage(r.Context(), request.PeerDeviceID, request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(message)
}

func (s *HTTPServer) handleFileTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	peerDeviceID, upload, err := parseStreamingUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer upload.Close()

	transferSnapshot, err := s.app.SendFile(r.Context(), peerDeviceID, upload.fileName, upload.fileSize, upload.file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(transferSnapshot)
}

func parseAfterSeq(w http.ResponseWriter, r *http.Request) (int64, bool) {
	afterSeq := int64(0)
	if raw := r.URL.Query().Get("afterSeq"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid afterSeq", http.StatusBadRequest)
			return 0, false
		}
		afterSeq = value
	}
	if raw := r.URL.Query().Get("lastEventSeq"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid lastEventSeq", http.StatusBadRequest)
			return 0, false
		}
		afterSeq = value
	}
	return afterSeq, true
}

func parsePairingAction(path string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/pairings/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type streamedUpload struct {
	file     io.ReadCloser
	fileName string
	fileSize int64
}

func (u *streamedUpload) Close() {
	if u == nil || u.file == nil {
		return
	}
	_ = u.file.Close()
}

func parseStreamingUpload(r *http.Request) (peerDeviceID string, upload *streamedUpload, err error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, err
	}

	defer func() {
		if err != nil && upload != nil {
			upload.Close()
		}
	}()

	fileSize := int64(0)
	fileSizeSet := false

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, err
		}

		switch part.FormName() {
		case "peerDeviceId":
			value, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				return "", nil, err
			}
			peerDeviceID = strings.TrimSpace(string(value))
		case "fileSize":
			value, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				return "", nil, err
			}
			parsedSize, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
			if err != nil || parsedSize < 0 {
				return "", nil, errors.New("invalid fileSize")
			}
			fileSize = parsedSize
			fileSizeSet = true
		case "file":
			if strings.TrimSpace(peerDeviceID) == "" {
				_ = part.Close()
				return "", nil, errors.New("peerDeviceId is required")
			}
			if !fileSizeSet {
				_ = part.Close()
				return "", nil, errors.New("fileSize is required")
			}
			upload = &streamedUpload{
				file:     part,
				fileName: part.FileName(),
				fileSize: fileSize,
			}
			return peerDeviceID, upload, nil
		default:
			if _, err := io.Copy(io.Discard, part); err != nil {
				_ = part.Close()
				return "", nil, err
			}
			_ = part.Close()
		}
	}

	if strings.TrimSpace(peerDeviceID) == "" {
		return "", nil, errors.New("peerDeviceId is required")
	}
	if !fileSizeSet {
		return "", nil, errors.New("fileSize is required")
	}
	if upload == nil {
		return "", nil, errors.New("file is required")
	}

	return peerDeviceID, upload, nil
}

func allowLoopbackBrowserAccess(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if !isLoopbackOriginAllowed(origin) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return false
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	return true
}

func isLoopbackOriginAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveWebAssetPath(requestPath string) string {
	cleaned := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func fileExists(fsys fs.FS, filePath string) bool {
	if fsys == nil {
		return false
	}
	info, err := fs.Stat(fsys, filePath)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	return !info.IsDir()
}

func hasFileExtension(filePath string) bool {
	return path.Ext(path.Base(filePath)) != ""
}

type stubService struct{}

func StubAppService() app.Service {
	return stubService{}
}

func (stubService) Bootstrap() (app.BootstrapSnapshot, error) {
	return app.BootstrapSnapshot{
		LocalDeviceName: "office-pc",
		Health:          diagnostics.BuildHealthSnapshot(true, 19090, "broadcast-pending"),
		Peers:           []app.PeerSnapshot{},
		Pairings:        []app.PairingSnapshot{},
		Conversations:   []app.ConversationSnapshot{},
		Messages:        []app.MessageSnapshot{},
		Transfers:       []app.TransferSnapshot{},
	}, nil
}

func (stubService) StartPairing(_ context.Context, _ string) (app.PairingSnapshot, error) {
	return app.PairingSnapshot{}, nil
}

func (stubService) ConfirmPairing(_ context.Context, _ string) (app.PairingSnapshot, error) {
	return app.PairingSnapshot{}, nil
}

func (stubService) SendTextMessage(_ context.Context, _ string, body string) (app.MessageSnapshot, error) {
	return app.MessageSnapshot{
		MessageID:      "msg-1",
		ConversationID: "conv-peer-1",
		Direction:      "outgoing",
		Kind:           "text",
		Body:           body,
		Status:         "sent",
	}, nil
}

func (stubService) SendFile(_ context.Context, _ string, fileName string, _ int64, content io.Reader) (app.TransferSnapshot, error) {
	if _, err := io.Copy(io.Discard, content); err != nil {
		return app.TransferSnapshot{}, err
	}
	return app.TransferSnapshot{
		TransferID: "transfer-1",
		FileName:   fileName,
		State:      "done",
	}, nil
}
