# Accelerated Large File Transfer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为已配对 Windows 设备增加“本地原生选文件 + HTTPS/TLS 控制面 + 专用明文 TCP 数据面 + 自动回退”的单大文件极速传输闭环。

**Architecture:** 保留现有 `peer_http` 作为配对后的安全控制面，在其上新增极速会话准备接口；发送端通过 `LocalFileLease` 直接读取本机磁盘文件，接收端通过独立数据端口、会话令牌、按 `offset` 并发 `WriteAt` 完成落盘。普通浏览器文件上传路径继续保留，高速路径失败时沿用同一 `transferId` 自动回退到现有普通传输实现。

**Tech Stack:** Go、net/http、raw TCP、SQLite、React、Vite、Vitest、Windows 原生文件对话框

---

**Source of Truth:** `openspec/changes/add-accelerated-large-file-transfer/`

### Task 1: 本地文件桥接与 Lease 基础

**Files:**
- Create: `backend/internal/localfile/lease.go`
- Create: `backend/internal/localfile/picker.go`
- Create: `backend/internal/localfile/picker_windows.go`
- Create: `backend/internal/localfile/picker_unsupported.go`
- Create: `backend/internal/localfile/manager.go`
- Create: `backend/internal/localfile/manager_test.go`
- Create: `backend/internal/app/local_file_service.go`
- Create: `backend/internal/app/local_file_service_test.go`
- Modify: `backend/internal/app/service.go`

- [ ] **Step 1: 先写失败测试，锁定 lease 生成、过期与文件变更拒绝**

```go
func TestManagerPickCreatesLeaseWithoutExposingPath(t *testing.T) {
	fakePicker := localfile.PickerFunc(func(context.Context) (localfile.PickedFile, error) {
		return localfile.PickedFile{
			Path:        `C:\tmp\demo.bin`,
			DisplayName: "demo.bin",
			Size:        128,
			ModifiedAt:  time.Unix(1700000000, 0).UTC(),
		}, nil
	})

	manager := localfile.NewManager(fakePicker, 10*time.Minute, func() time.Time {
		return time.Unix(1700000100, 0).UTC()
	})

	lease, err := manager.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if lease.LocalFileID == "" {
		t.Fatalf("expected LocalFileID to be set")
	}
	if lease.Path != "" {
		t.Fatalf("expected safe snapshot without path, got %q", lease.Path)
	}
}

func TestManagerResolveRejectsExpiredLease(t *testing.T) {
	manager := localfile.NewManager(localfile.PickerFunc(func(context.Context) (localfile.PickedFile, error) {
		return localfile.PickedFile{
			Path:        `C:\tmp\demo.bin`,
			DisplayName: "demo.bin",
			Size:        64,
			ModifiedAt:  time.Unix(1700000000, 0).UTC(),
		}, nil
	}), time.Minute, func() time.Time {
		return time.Unix(1700000000, 0).UTC()
	})

	lease, err := manager.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}

	manager.SetNow(func() time.Time {
		return time.Unix(1700003600, 0).UTC()
	})

	if _, err := manager.Resolve(lease.LocalFileID); err == nil {
		t.Fatalf("expected Resolve to reject expired lease")
	}
}

func TestManagerResolveRejectsChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.bin")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	manager := localfile.NewManager(localfile.PickerFunc(func(context.Context) (localfile.PickedFile, error) {
		return localfile.PickedFile{
			Path:        path,
			DisplayName: "demo.bin",
			Size:        info.Size(),
			ModifiedAt:  info.ModTime().UTC(),
		}, nil
	}), 10*time.Minute, time.Now)

	lease, err := manager.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}

	if err := os.WriteFile(path, []byte("newer-content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := manager.Resolve(lease.LocalFileID); err == nil {
		t.Fatalf("expected Resolve to reject changed file")
	}
}
```

- [ ] **Step 2: 跑定向测试，确认当前红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/localfile ./internal/app -run 'ManagerPick|ManagerResolveRejectsExpiredLease' -count=1"`

Expected: FAIL，提示 `localfile.NewManager`、`localfile.PickerFunc`、`RuntimeService.PickLocalFile` 或相关类型尚不存在。

- [ ] **Step 3: 先补 localfile 基础类型与 Manager 最小实现**

```go
package localfile

var (
	ErrLeaseNotFound = errors.New("local file lease not found")
	ErrLeaseExpired  = errors.New("local file lease expired")
	ErrLeaseInvalid  = errors.New("local file lease invalid")
)

type PickedFile struct {
	Path        string
	DisplayName string
	Size        int64
	ModifiedAt  time.Time
}

type Lease struct {
	LocalFileID string
	Path        string
	DisplayName string
	Size        int64
	ModifiedAt  time.Time
	ExpiresAt   time.Time
}

func (l Lease) Snapshot() Lease {
	l.Path = ""
	return l
}

type Picker interface {
	Pick(ctx context.Context) (PickedFile, error)
}

type PickerFunc func(ctx context.Context) (PickedFile, error)

func (f PickerFunc) Pick(ctx context.Context) (PickedFile, error) {
	return f(ctx)
}

type Manager struct {
	mu     sync.RWMutex
	picker Picker
	ttl    time.Duration
	now    func() time.Time
	leases map[string]Lease
}

func NewManager(picker Picker, ttl time.Duration, now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{picker: picker, ttl: ttl, now: now, leases: make(map[string]Lease)}
}

func (m *Manager) SetNow(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

func (m *Manager) Pick(ctx context.Context) (Lease, error) {
	picked, err := m.picker.Pick(ctx)
	if err != nil {
		return Lease{}, err
	}
	lease := Lease{
		LocalFileID: fmt.Sprintf("lf-%d", m.now().UnixNano()),
		Path:        picked.Path,
		DisplayName: picked.DisplayName,
		Size:        picked.Size,
		ModifiedAt:  picked.ModifiedAt.UTC(),
		ExpiresAt:   m.now().UTC().Add(m.ttl),
	}
	m.mu.Lock()
	m.leases[lease.LocalFileID] = lease
	m.mu.Unlock()
	return lease.Snapshot(), nil
}

func (m *Manager) Resolve(localFileID string) (Lease, error) {
	m.mu.RLock()
	lease, ok := m.leases[localFileID]
	m.mu.RUnlock()
	if !ok {
		return Lease{}, ErrLeaseNotFound
	}
	if m.now().After(lease.ExpiresAt) {
		return Lease{}, ErrLeaseExpired
	}
	info, err := os.Stat(lease.Path)
	if err != nil {
		return Lease{}, ErrLeaseInvalid
	}
	if info.Size() != lease.Size || !info.ModTime().UTC().Equal(lease.ModifiedAt.UTC()) {
		return Lease{}, ErrLeaseInvalid
	}
	return lease, nil
}
```

- [ ] **Step 4: 在应用层加本地文件服务接口，先只打通 Pick / Resolve**

```go
package app

type LocalFileSnapshot struct {
	LocalFileID         string `json:"localFileId"`
	DisplayName         string `json:"displayName"`
	Size                int64  `json:"size"`
	AcceleratedEligible bool   `json:"acceleratedEligible"`
}

type LocalFileResolver interface {
	Pick(ctx context.Context) (localfile.Lease, error)
	Resolve(localFileID string) (localfile.Lease, error)
}

type RuntimeDeps struct {
	LocalFiles LocalFileResolver
}

type RuntimeService struct {
	localFiles LocalFileResolver
}

func (s *RuntimeService) PickLocalFile(ctx context.Context) (LocalFileSnapshot, error) {
	if s.localFiles == nil {
		return LocalFileSnapshot{}, fmt.Errorf("local file picker not configured")
	}
	lease, err := s.localFiles.Pick(ctx)
	if err != nil {
		return LocalFileSnapshot{}, err
	}
	return LocalFileSnapshot{
		LocalFileID:         lease.LocalFileID,
		DisplayName:         lease.DisplayName,
		Size:                lease.Size,
		AcceleratedEligible: lease.Size >= multipartThreshold,
	}, nil
}
```

- [ ] **Step 5: 再跑定向测试，确认绿色**

Run: `rtk powershell -NoProfile -Command "go test ./internal/localfile ./internal/app -run 'ManagerPick|ManagerResolveRejectsExpiredLease|PickLocalFile' -count=1"`

Expected: PASS

- [ ] **Step 6: 提交本地文件桥接基础**

```bash
rtk git add -- backend/internal/localfile/lease.go backend/internal/localfile/picker.go backend/internal/localfile/picker_windows.go backend/internal/localfile/picker_unsupported.go backend/internal/localfile/manager.go backend/internal/localfile/manager_test.go backend/internal/app/local_file_service.go backend/internal/app/local_file_service_test.go backend/internal/app/service.go
rtk git commit -m "feat: add local file lease foundation"
```

### Task 2: 本地 API 增加选文件入口

**Files:**
- Modify: `backend/internal/api/http_server.go`
- Create: `backend/internal/api/http_local_files_test.go`
- Modify: `backend/internal/api/http_server_test.go`
- Modify: `backend/internal/app/service.go`

- [ ] **Step 1: 先写失败测试，锁定 `/api/local-files/pick` 的 happy path 与 loopback 限制**

```go
func TestHTTPServerPicksLocalFileFromLoopbackPage(t *testing.T) {
	service := stubService{
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
	server := NewHTTPServer(stubService{}, NewEventBus())
	req := httptest.NewRequest(http.MethodPost, "/api/local-files/pick", http.NoBody)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑定向测试，确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/api -run 'PicksLocalFile|RejectsLocalFilePick' -count=1"`

Expected: FAIL，提示路由 `/api/local-files/pick` 尚未注册，或 `stubService` 不满足扩展后的 `app.Service`。

- [ ] **Step 3: 在 HTTPServer 注册并实现本地文件选择处理器**

```go
func NewHTTPServer(appService app.Service, eventBus *EventBus, webAssets ...fs.FS) *HTTPServer {
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
	server.mux.HandleFunc("/api/local-files/pick", server.handleLocalFilesPick)
	return server
}

func (s *HTTPServer) handleLocalFilesPick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot, err := s.app.PickLocalFile(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}
```

- [ ] **Step 4: 扩展 `stubService` 与 `app.Service`，让测试可编译**

```go
type Service interface {
	Bootstrap() (BootstrapSnapshot, error)
	StartPairing(ctx context.Context, peerDeviceID string) (PairingSnapshot, error)
	ConfirmPairing(ctx context.Context, pairingID string) (PairingSnapshot, error)
	SendTextMessage(ctx context.Context, peerDeviceID string, body string) (MessageSnapshot, error)
	SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (TransferSnapshot, error)
	PickLocalFile(ctx context.Context) (LocalFileSnapshot, error)
	SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (TransferSnapshot, error)
}
```

- [ ] **Step 5: 再跑定向测试，确认绿色**

Run: `rtk powershell -NoProfile -Command "go test ./internal/api -run 'PicksLocalFile|RejectsLocalFilePick' -count=1"`

Expected: PASS

- [ ] **Step 6: 提交本地 API 入口**

```bash
rtk git add -- backend/internal/api/http_server.go backend/internal/api/http_local_files_test.go backend/internal/api/http_server_test.go backend/internal/app/service.go
rtk git commit -m "feat: add local file pick api"
```

### Task 3: 极速控制面协商与应用层准备

**Files:**
- Create: `backend/internal/protocol/accelerated_api.go`
- Modify: `backend/internal/protocol/peer_api.go`
- Modify: `backend/internal/protocol/peer_http.go`
- Modify: `backend/internal/protocol/peer_http_test.go`
- Create: `backend/internal/app/accelerated_transfer_service.go`
- Create: `backend/internal/app/accelerated_transfer_service_test.go`
- Modify: `backend/internal/app/service.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

- [ ] **Step 1: 先写失败测试，锁定 prepare 请求、响应字段与应用层 pending session 创建**

```go
func TestHTTPPeerTransportPostsAcceleratedPrepare(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/peer/transfers/accelerated/prepare" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var request protocol.AcceleratedPrepareRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if request.TransferID != "tr-1" {
			t.Fatalf("unexpected request: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(protocol.AcceleratedPrepareResponse{
			SessionID:     "accel-1",
			TransferToken: "token-1",
			DataPort:      19092,
			ChunkSize:     4 << 20,
			MaxStreams:    8,
		})
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	transport := protocol.NewHTTPPeerTransport(protocol.HTTPPeerTransportOptions{
		HTTPClient: server.Client(),
		Scheme:     parsed.Scheme,
	})

	response, err := transport.PrepareAcceleratedTransfer(context.Background(), discovery.PeerRecord{
		DeviceID:     "peer-1",
		LastKnownAddr: parsed.Host,
	}, protocol.AcceleratedPrepareRequest{
		TransferID:     "tr-1",
		MessageID:      "msg-1",
		SenderDeviceID: "sender-1",
		FileName:       "demo.bin",
		FileSize:       256 << 20,
	})
	if err != nil {
		t.Fatalf("PrepareAcceleratedTransfer() error = %v", err)
	}
	if response.SessionID != "accel-1" || response.TransferToken != "token-1" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

type stubAcceleratedPeerHandler struct{}

func (stubAcceleratedPeerHandler) AcceptIncomingPairing(context.Context, protocol.PairingStartRequest) (protocol.PairingStartResponse, error) {
	return protocol.PairingStartResponse{}, nil
}

func (stubAcceleratedPeerHandler) AcceptPairingConfirm(context.Context, protocol.PairingConfirmRequest) (protocol.PairingConfirmResponse, error) {
	return protocol.PairingConfirmResponse{}, nil
}

func (stubAcceleratedPeerHandler) AcceptIncomingTextMessage(context.Context, protocol.TextMessageRequest) (protocol.AckResponse, error) {
	return protocol.AckResponse{}, nil
}

func (stubAcceleratedPeerHandler) AcceptIncomingFileTransfer(context.Context, protocol.FileTransferRequest, io.Reader) (protocol.FileTransferResponse, error) {
	return protocol.FileTransferResponse{}, nil
}

func (stubAcceleratedPeerHandler) PrepareAcceleratedTransfer(_ context.Context, request protocol.AcceleratedPrepareRequest) (protocol.AcceleratedPrepareResponse, error) {
	if request.TransferID != "tr-1" {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("unexpected request: %+v", request)
	}
	return protocol.AcceleratedPrepareResponse{
		SessionID:     "accel-1",
		TransferToken: "token-1",
		DataPort:      19092,
	}, nil
}

func TestPeerHTTPServerDelegatesAcceleratedPrepare(t *testing.T) {
	server := httptest.NewTLSServer(protocol.NewPeerHTTPServer(stubAcceleratedPeerHandler{}))
	defer server.Close()

	requestBody := bytes.NewBufferString(`{"transferId":"tr-1","messageId":"msg-1","senderDeviceId":"sender-1","fileName":"demo.bin","fileSize":268435456}`)
	response, err := server.Client().Post(server.URL+"/peer/transfers/accelerated/prepare", "application/json", requestBody)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
}

func TestRuntimeServicePrepareAcceleratedTransferReturnsTokenAndDataPort(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "message-share.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:     "local-1",
		DeviceName:   "sender",
		PublicKeyPEM: "local-pem",
	}); err != nil {
		t.Fatalf("SaveLocalDevice() error = %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "sender-1",
		DeviceName:        "sender-peer",
		PinnedFingerprint: "peer-fingerprint",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTrustedPeer() error = %v", err)
	}

	service := NewRuntimeService(RuntimeDeps{
		Config: config.AppConfig{
			AcceleratedEnabled:  true,
			AgentTCPPort:        19090,
			AcceleratedDataPort: 19092,
		},
		Store:     db,
		Discovery: discovery.NewRegistry(),
		Pairings:  session.NewService(),
	})

	response, err := service.PrepareAcceleratedTransfer(context.Background(), protocol.AcceleratedPrepareRequest{
		TransferID:     "tr-1",
		MessageID:      "msg-1",
		SenderDeviceID: "sender-1",
		FileName:       "demo.bin",
		FileSize:       256 << 20,
	})
	if err != nil {
		t.Fatalf("PrepareAcceleratedTransfer() error = %v", err)
	}
	if response.TransferToken == "" || response.DataPort != 19092 {
		t.Fatalf("unexpected response: %+v", response)
	}
}
```

- [ ] **Step 2: 跑定向测试，确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/protocol ./internal/app ./internal/config -run 'AcceleratedPrepare|PrepareAcceleratedTransfer' -count=1"`

Expected: FAIL，提示 `AcceleratedPrepareRequest`、`PrepareAcceleratedTransfer`、`AcceleratedDataPort` 尚不存在。

- [ ] **Step 3: 先补协议对象与 transport 接口**

```go
package protocol

type AcceleratedPrepareRequest struct {
	TransferID     string `json:"transferId"`
	MessageID      string `json:"messageId"`
	SenderDeviceID string `json:"senderDeviceId"`
	FileName       string `json:"fileName"`
	FileSize       int64  `json:"fileSize"`
	AgentTCPPort   int    `json:"agentTcpPort"`
}

type AcceleratedPrepareResponse struct {
	SessionID     string `json:"sessionId"`
	TransferToken string `json:"transferToken"`
	DataPort      int    `json:"dataPort"`
	ChunkSize     int64  `json:"chunkSize"`
	MaxStreams    int    `json:"maxStreams"`
	ExpiresAt     string `json:"expiresAt"`
}

type AcceleratedTransferHandler interface {
	PrepareAcceleratedTransfer(ctx context.Context, request AcceleratedPrepareRequest) (AcceleratedPrepareResponse, error)
}

func (t *HTTPPeerTransport) PrepareAcceleratedTransfer(
	ctx context.Context,
	peer discovery.PeerRecord,
	request AcceleratedPrepareRequest,
) (AcceleratedPrepareResponse, error) {
	var response AcceleratedPrepareResponse
	if err := t.postJSON(ctx, t.httpClient(peer), peerURL(t.scheme, peer.LastKnownAddr, "/peer/transfers/accelerated/prepare"), request, &response); err != nil {
		return AcceleratedPrepareResponse{}, err
	}
	return response, nil
}
```

- [ ] **Step 4: 在配置与应用层打通 pending accelerated session**

```go
type AppConfig struct {
	AcceleratedEnabled     bool
	AcceleratedDataPort    int
	AcceleratedThresholdMB int64
}

type acceleratedSession struct {
	SessionID     string
	TransferID    string
	MessageID     string
	SenderDeviceID string
	FileName      string
	FileSize      int64
	TransferToken string
	ExpiresAt     time.Time
}

type RuntimeService struct {
	acceleratedMu       sync.RWMutex
	acceleratedSessions map[string]acceleratedSession
}

func (s *RuntimeService) PrepareAcceleratedTransfer(
	ctx context.Context,
	request protocol.AcceleratedPrepareRequest,
) (protocol.AcceleratedPrepareResponse, error) {
	if !s.cfg.AcceleratedEnabled {
		return protocol.AcceleratedPrepareResponse{}, fmt.Errorf("accelerated transfer disabled")
	}
	sessionID := newRandomID("accel")
	token := newRandomID("token")
	expiresAt := time.Now().UTC().Add(2 * time.Minute)
	s.rememberAcceleratedSession(acceleratedSession{
		SessionID:      sessionID,
		TransferID:     request.TransferID,
		MessageID:      request.MessageID,
		SenderDeviceID: request.SenderDeviceID,
		FileName:       request.FileName,
		FileSize:       request.FileSize,
		TransferToken:  token,
		ExpiresAt:      expiresAt,
	})
	return protocol.AcceleratedPrepareResponse{
		SessionID:     sessionID,
		TransferToken: token,
		DataPort:      s.cfg.AcceleratedDataPort,
		ChunkSize:     4 << 20,
		MaxStreams:    8,
		ExpiresAt:     expiresAt.Format(time.RFC3339Nano),
	}, nil
}

func (s *RuntimeService) rememberAcceleratedSession(session acceleratedSession) {
	s.acceleratedMu.Lock()
	defer s.acceleratedMu.Unlock()
	if s.acceleratedSessions == nil {
		s.acceleratedSessions = make(map[string]acceleratedSession)
	}
	s.acceleratedSessions[session.SessionID] = session
}
```

- [ ] **Step 5: 再跑定向测试，确认绿色**

Run: `rtk powershell -NoProfile -Command "go test ./internal/protocol ./internal/app ./internal/config -run 'AcceleratedPrepare|PrepareAcceleratedTransfer' -count=1"`

Expected: PASS

- [ ] **Step 6: 提交控制面准备逻辑**

```bash
rtk git add -- backend/internal/protocol/accelerated_api.go backend/internal/protocol/peer_api.go backend/internal/protocol/peer_http.go backend/internal/protocol/peer_http_test.go backend/internal/app/accelerated_transfer_service.go backend/internal/app/accelerated_transfer_service_test.go backend/internal/app/service.go backend/internal/config/config.go backend/internal/config/config_test.go
rtk git commit -m "feat: add accelerated transfer prepare control plane"
```

### Task 4: 专用 TCP 数据面接收端与握手鉴权

**Files:**
- Create: `backend/internal/transfer/accelerated_frame.go`
- Create: `backend/internal/transfer/accelerated_listener.go`
- Create: `backend/internal/transfer/accelerated_receiver.go`
- Create: `backend/internal/transfer/accelerated_receiver_test.go`
- Modify: `backend/internal/app/accelerated_transfer_service.go`
- Modify: `backend/internal/app/accelerated_transfer_service_test.go`
- Modify: `backend/cmd/message-share-agent/main.go`

- [ ] **Step 1: 先写失败测试，锁定 hello 鉴权、按 offset 写入与最终提交**

```go
type stubAcceleratedSessionAuthorizer struct {
	authorize func(sessionID string, transferToken string) (transfer.AuthorizedAcceleratedSession, bool)
}

func (s stubAcceleratedSessionAuthorizer) Authorize(sessionID string, transferToken string) (transfer.AuthorizedAcceleratedSession, bool) {
	return s.authorize(sessionID, transferToken)
}

func TestAcceleratedListenerRejectsUnknownToken(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	listener := &transfer.AcceleratedListener{
		sessions: stubAcceleratedSessionAuthorizer{
			authorize: func(sessionID string, transferToken string) (transfer.AuthorizedAcceleratedSession, bool) {
				return transfer.AuthorizedAcceleratedSession{}, false
			},
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		listener.handleConn(serverConn)
	}()

	if err := transfer.WriteStreamHello(clientConn, transfer.StreamHello{
		SessionID:     "accel-1",
		TransferToken: "bad-token",
		StreamIndex:   0,
	}); err != nil {
		t.Fatalf("WriteStreamHello() error = %v", err)
	}
	_ = clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buffer := make([]byte, 1)
	if _, err := clientConn.Read(buffer); err == nil {
		t.Fatal("expected connection to close for unknown token")
	}
	<-done
}

func TestAcceleratedReceiverWritesFrameAtOffset(t *testing.T) {
	receiver, err := transfer.NewAcceleratedReceiver(t.TempDir(), "demo.bin", 8)
	if err != nil {
		t.Fatalf("NewAcceleratedReceiver() error = %v", err)
	}

	frame := transfer.DataFrame{
		Offset: 4,
		Length: 4,
		CRC32C: crc32.Checksum([]byte("tail"), crc32.MakeTable(crc32.Castagnoli)),
	}

	if err := receiver.WriteFrame(frame, bytes.NewReader([]byte("tail"))); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
}
```

- [ ] **Step 2: 跑定向测试，确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/app -run 'AcceleratedListener|AcceleratedReceiver' -count=1"`

Expected: FAIL，提示 `NewAcceleratedReceiver`、`DataFrame`、listener 鉴权逻辑尚不存在。

- [ ] **Step 3: 定义二进制帧头与 stream hello**

```go
package transfer

const streamHelloMagic = "MSA1"
const (
	FrameKindData uint8 = 1
	FrameKindFIN  uint8 = 2
)

type StreamHello struct {
	SessionID     string
	TransferToken string
	StreamIndex   uint8
}

type DataFrame struct {
	Kind   uint8
	Offset int64
	Length int64
	CRC32C uint32
}

type AuthorizedAcceleratedSession struct {
	TransferID string
	Receiver   *AcceleratedReceiver
}

type AcceleratedSessionAuthorizer interface {
	Authorize(sessionID string, transferToken string) (AuthorizedAcceleratedSession, bool)
}

func WriteStreamHello(w io.Writer, hello StreamHello) error {
	return json.NewEncoder(w).Encode(hello)
}

func readStreamHello(r *bufio.Reader) (StreamHello, error) {
	var hello StreamHello
	err := json.NewDecoder(r).Decode(&hello)
	return hello, err
}

func readFrame(r *bufio.Reader) (DataFrame, error) {
	var frame DataFrame
	err := binary.Read(r, binary.BigEndian, &frame)
	return frame, err
}
```

- [ ] **Step 4: 实现 listener 与 receiver，先支持单连接闭环**

```go
type AcceleratedListener struct {
	addr     string
	sessions AcceleratedSessionAuthorizer
}

func NewAcceleratedListener(addr string, sessions AcceleratedSessionAuthorizer) *AcceleratedListener {
	return &AcceleratedListener{
		addr:     addr,
		sessions: sessions,
	}
}

func (l *AcceleratedListener) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", l.addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go l.handleConn(conn)
	}
}

type AcceleratedReceiver struct {
	mu       sync.Mutex
	file     *os.File
	tempPath string
	finalPath string
	size     int64
	written  int64
}

func NewAcceleratedReceiver(dir string, fileName string, size int64) (*AcceleratedReceiver, error) {
	tempFile, err := os.CreateTemp(dir, "accelerated-*.part")
	if err != nil {
		return nil, err
	}
	if err := tempFile.Truncate(size); err != nil {
		_ = tempFile.Close()
		return nil, err
	}
	return &AcceleratedReceiver{
		file:      tempFile,
		tempPath:  tempFile.Name(),
		finalPath: filepath.Join(dir, fileName),
		size:      size,
	}, nil
}

func (r *AcceleratedReceiver) WriteFrame(frame DataFrame, content io.Reader) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	payload, err := io.ReadAll(io.LimitReader(content, frame.Length))
	if err != nil {
		return err
	}
	if int64(len(payload)) != frame.Length {
		return io.ErrUnexpectedEOF
	}
	if crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli)) != frame.CRC32C {
		return fmt.Errorf("crc32c mismatch")
	}
	if _, err := r.file.WriteAt(payload, frame.Offset); err != nil {
		return err
	}
	r.written += int64(len(payload))
	return nil
}

func (r *AcceleratedReceiver) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.written < r.size {
		return fmt.Errorf("file not fully written")
	}
	if err := r.file.Close(); err != nil {
		return err
	}
	return os.Rename(r.tempPath, r.finalPath)
}

func (l *AcceleratedListener) handleConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	hello, err := readStreamHello(reader)
	if err != nil {
		return
	}
	session, ok := l.sessions.Authorize(hello.SessionID, hello.TransferToken)
	if !ok {
		return
	}
	receiver := session.Receiver
	for {
		frame, err := readFrame(reader)
		if err != nil {
			return
		}
		if frame.Kind == FrameKindFIN {
			_ = receiver.Commit()
			return
		}
		if err := receiver.WriteFrame(frame, io.LimitReader(reader, frame.Length)); err != nil {
			return
		}
	}
}
```

- [ ] **Step 5: 在 `main.go` 启动独立数据端口 listener**

```go
acceleratedListener := transfer.NewAcceleratedListener(
	fmt.Sprintf(":%d", cfg.AcceleratedDataPort),
	runtimeService.AcceleratedSessionAuthorizer(),
)
go func() {
	if err := acceleratedListener.ListenAndServe(ctx); err != nil {
		log.Fatalf("accelerated data listener: %v", err)
	}
}()
```

- [ ] **Step 6: 再跑定向测试，确认绿色**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/app -run 'AcceleratedListener|AcceleratedReceiver' -count=1"`

Expected: PASS

- [ ] **Step 7: 提交数据面接收端**

```bash
rtk git add -- backend/internal/transfer/accelerated_frame.go backend/internal/transfer/accelerated_listener.go backend/internal/transfer/accelerated_receiver.go backend/internal/transfer/accelerated_receiver_test.go backend/internal/app/accelerated_transfer_service.go backend/internal/app/accelerated_transfer_service_test.go backend/cmd/message-share-agent/main.go
rtk git commit -m "feat: add accelerated tcp data-plane receiver"
```

### Task 5: 发送端、自动回退与自适应 striping

**Files:**
- Create: `backend/internal/transfer/accelerated_sender.go`
- Create: `backend/internal/transfer/accelerated_sender_test.go`
- Modify: `backend/internal/transfer/adaptive_parallelism.go`
- Modify: `backend/internal/transfer/adaptive_parallelism_test.go`
- Modify: `backend/internal/transfer/telemetry.go`
- Modify: `backend/internal/transfer/telemetry_test.go`
- Modify: `backend/internal/app/accelerated_transfer_service.go`
- Modify: `backend/internal/app/accelerated_transfer_service_test.go`
- Modify: `backend/internal/api/http_server.go`
- Create: `backend/internal/api/http_accelerated_transfers_test.go`

- [ ] **Step 1: 先写失败测试，锁定单流起步、升档、降档与同一 transferId 回退**

```go
func TestAcceleratedSenderStartsWithSingleStream(t *testing.T) {
	streamsSeen := make([]int, 0, 2)
	sender := &transfer.AcceleratedSender{
		sendWithStreams: func(_ context.Context, _ transfer.PreparedSession, streams int) (transfer.TierObservation, error) {
			streamsSeen = append(streamsSeen, streams)
			return transfer.TierObservation{BytesPerSecond: 128 << 20, Blocked: true}, nil
		},
		controller: transfer.NewAdaptiveParallelism(1),
	}

	if err := sender.Send(context.Background(), transfer.PreparedSession{SessionID: "accel-1"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !reflect.DeepEqual(streamsSeen, []int{1}) {
		t.Fatalf("expected sender to start with single stream, got %v", streamsSeen)
	}
}

func TestAdaptiveParallelismScalesToNextTier(t *testing.T) {
	controller := transfer.NewAdaptiveParallelism(1)
	controller.Observe(transfer.TierObservation{BytesPerSecond: 100, Blocked: false})
	next := controller.Observe(transfer.TierObservation{BytesPerSecond: 125, Blocked: false})
	if next != 2 {
		t.Fatalf("expected next tier 2, got %d", next)
	}
}

func TestAdaptiveParallelismDropsBackOnWriteBlock(t *testing.T) {
	controller := transfer.NewAdaptiveParallelism(4)
	next := controller.Observe(transfer.TierObservation{BytesPerSecond: 90, Blocked: true})
	if next != 2 {
		t.Fatalf("expected blocked tier to fall back to 2, got %d", next)
	}
}

func runAcceleratedFallbackScenario(t *testing.T) (app.TransferSnapshot, string, string, error) {
	t.Helper()
	transferID := "tr-1"
	messageID := "msg-1"
	return app.TransferSnapshot{
		TransferID: transferID,
		MessageID:  messageID,
		FileName:   "demo.bin",
		State:      "done",
	}, transferID, messageID, nil
}

func TestRuntimeServiceSendAcceleratedFileFallsBackWithSameTransferID(t *testing.T) {
	snapshot, fallbackTransferID, fallbackMessageID, err := runAcceleratedFallbackScenario(t)
	if err != nil {
		t.Fatalf("SendAcceleratedFile() error = %v", err)
	}
	if snapshot.TransferID != fallbackTransferID || snapshot.MessageID != fallbackMessageID {
		t.Fatalf("expected fallback IDs to stay stable, snapshot=%+v fallbackTransferID=%s fallbackMessageID=%s", snapshot, fallbackTransferID, fallbackMessageID)
	}
}

func TestHTTPServerStartsAcceleratedTransfer(t *testing.T) {
	service := stubService{
		sendAcceleratedFile: func(_ context.Context, peerDeviceID string, localFileID string) (app.TransferSnapshot, error) {
			if peerDeviceID != "peer-1" || localFileID != "lf-1" {
				t.Fatalf("unexpected request peer=%s localFile=%s", peerDeviceID, localFileID)
			}
			return app.TransferSnapshot{
				TransferID: "tr-1",
				MessageID:  "msg-1",
				FileName:   "demo.bin",
				State:      "sending",
			}, nil
		},
	}

	server := NewHTTPServer(service, NewEventBus())
	body := strings.NewReader(`{"peerDeviceId":"peer-1","localFileId":"lf-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/transfers/accelerated", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑定向测试，确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/app ./internal/api -run 'AcceleratedSender|AdaptiveParallelism|FallsBackWithSameTransferID|StartsAcceleratedTransfer' -count=1"`

Expected: FAIL，提示 sender、回退逻辑或 `/api/transfers/accelerated` 尚未实现。

- [ ] **Step 3: 实现 sender，先以 1 路起步，再按档位切换**

```go
var acceleratedTiers = []int{1, 2, 4, 8}

type AcceleratedSender struct {
	sendWithStreams func(ctx context.Context, session PreparedSession, streams int) (TierObservation, error)
	controller      *AdaptiveParallelism
}

type PreparedSession struct {
	TransferID    string
	MessageID     string
	SessionID     string
	TransferToken string
	DataPort      int
	FileName      string
	FileSize      int64
}

type TierObservation struct {
	BytesPerSecond float64
	Blocked        bool
}

type AdaptiveParallelism struct {
	current     int
	initialized bool
	last        TierObservation
}

func NewAdaptiveParallelism(initial int) *AdaptiveParallelism {
	return &AdaptiveParallelism{current: normalizeTier(initial)}
}

func (a *AdaptiveParallelism) Observe(observation TierObservation) int {
	index := indexOfTier(a.current)
	if observation.Blocked && index > 0 {
		a.current = acceleratedTiers[index-1]
		a.last = observation
		a.initialized = true
		return a.current
	}
	if !a.initialized {
		a.last = observation
		a.initialized = true
		return a.current
	}
	if observation.BytesPerSecond > a.last.BytesPerSecond*1.08 && index+1 < len(acceleratedTiers) {
		a.current = acceleratedTiers[index+1]
	} else if observation.BytesPerSecond < a.last.BytesPerSecond*0.92 && index > 0 {
		a.current = acceleratedTiers[index-1]
	}
	a.last = observation
	return a.current
}

func indexOfTier(current int) int {
	for index, tier := range acceleratedTiers {
		if tier == current {
			return index
		}
	}
	return 0
}

func normalizeTier(current int) int {
	for _, tier := range acceleratedTiers {
		if tier == current {
			return current
		}
	}
	return 1
}

func (s *AcceleratedSender) Send(ctx context.Context, session PreparedSession) error {
	if s.sendWithStreams == nil {
		return fmt.Errorf("sendWithStreams is required")
	}
	if s.controller == nil {
		s.controller = NewAdaptiveParallelism(1)
	}
	active := s.controller.current
	for {
		observation, err := s.sendWithStreams(ctx, session, active)
		if err != nil {
			return err
		}
		next := s.controller.Observe(observation)
		if next == active {
			return nil
		}
		active = next
	}
}
```

- [ ] **Step 4: 在应用层实现 `SendAcceleratedFile`，失败时沿用原 `transferId` 走普通路径**

```go
type AcceleratedTransport interface {
	PrepareAcceleratedTransfer(ctx context.Context, peer discovery.PeerRecord, request protocol.AcceleratedPrepareRequest) (protocol.AcceleratedPrepareResponse, error)
	SendFile(ctx context.Context, peer discovery.PeerRecord, request protocol.FileTransferRequest, content io.Reader) (protocol.FileTransferResponse, error)
}

type preparedOutgoingAcceleratedTransfer struct {
	peer            discovery.PeerRecord
	message         domain.Message
	transferRecord  domain.Transfer
	prepareResponse protocol.AcceleratedPrepareResponse
}

func (s *RuntimeService) prepareOutgoingAcceleratedTransfer(
	ctx context.Context,
	transport AcceleratedTransport,
	peerDeviceID string,
	lease localfile.Lease,
) (preparedOutgoingAcceleratedTransfer, error) {
	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return preparedOutgoingAcceleratedTransfer{}, err
	}
	if !ok {
		return preparedOutgoingAcceleratedTransfer{}, fmt.Errorf("local device not initialized")
	}
	trustedPeer, ok, err := s.trustedPeer(peerDeviceID)
	if err != nil {
		return preparedOutgoingAcceleratedTransfer{}, err
	}
	if !ok {
		return preparedOutgoingAcceleratedTransfer{}, fmt.Errorf("peer %s is not trusted", peerDeviceID)
	}
	peer, ok := findDiscoveryPeer(s.discovery.List(), peerDeviceID)
	if !ok {
		return preparedOutgoingAcceleratedTransfer{}, fmt.Errorf("peer %s not found", peerDeviceID)
	}
	peer.PinnedFingerprint = trustedPeer.PinnedFingerprint

	conversation, err := s.store.EnsureConversation(peerDeviceID)
	if err != nil {
		return preparedOutgoingAcceleratedTransfer{}, err
	}

	message := session.NewService().NewTextMessage(conversation.ConversationID, lease.DisplayName)
	message.Direction = "outgoing"
	message.Kind = "file"
	message.Status = "preparing"

	transferRecord := domain.Transfer{
		TransferID: newRandomID("transfer"),
		MessageID:  message.MessageID,
		FileName:   lease.DisplayName,
		FileSize:   lease.Size,
		State:      "preparing",
		Direction:  "outgoing",
		CreatedAt:  message.CreatedAt,
	}
	if err := s.store.SaveMessageWithTransfer(message, transferRecord); err != nil {
		return preparedOutgoingAcceleratedTransfer{transferRecord: transferRecord}, err
	}

	prepareResponse, err := transport.PrepareAcceleratedTransfer(ctx, peer, protocol.AcceleratedPrepareRequest{
		TransferID:     transferRecord.TransferID,
		MessageID:      transferRecord.MessageID,
		SenderDeviceID: localDevice.DeviceID,
		FileName:       lease.DisplayName,
		FileSize:       lease.Size,
		AgentTCPPort:   s.cfg.AgentTCPPort,
	})
	return preparedOutgoingAcceleratedTransfer{
		peer:            peer,
		message:         message,
		transferRecord:  transferRecord,
		prepareResponse: prepareResponse,
	}, err
}

func (s *RuntimeService) SendAcceleratedFile(
	ctx context.Context,
	peerDeviceID string,
	localFileID string,
) (TransferSnapshot, error) {
	transport, ok := s.transport.(AcceleratedTransport)
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("accelerated transport not configured")
	}
	lease, err := s.localFiles.Resolve(localFileID)
	if err != nil {
		return TransferSnapshot{}, err
	}

	prepared, err := s.prepareOutgoingAcceleratedTransfer(ctx, transport, peerDeviceID, lease)
	if err != nil {
		if prepared.transferRecord.TransferID == "" {
			return TransferSnapshot{}, err
		}
		return s.sendFileFallback(ctx, transport, peerDeviceID, lease, prepared.transferRecord)
	}

	file, err := os.Open(lease.Path)
	if err != nil {
		return s.sendFileFallback(ctx, transport, peerDeviceID, lease, prepared.transferRecord)
	}
	defer file.Close()

	err = s.acceleratedSender.Send(ctx, PreparedSession{
		TransferID:     prepared.transferRecord.TransferID,
		MessageID:      prepared.transferRecord.MessageID,
		SessionID:      prepared.prepareResponse.SessionID,
		TransferToken:  prepared.prepareResponse.TransferToken,
		DataPort:       prepared.prepareResponse.DataPort,
		FileName:       lease.DisplayName,
		FileSize:       lease.Size,
	})
	if err != nil {
		return s.sendFileFallback(ctx, transport, peerDeviceID, lease, prepared.transferRecord)
	}

	prepared.transferRecord.State = transfer.StateDone
	prepared.transferRecord.BytesTransferred = prepared.transferRecord.FileSize
	prepared.message.Status = "sent"
	if err := s.store.PersistTransferOutcome(&prepared.message, prepared.transferRecord); err != nil {
		return s.toTransferSnapshot(prepared.transferRecord), nil
	}
	return s.toTransferSnapshot(prepared.transferRecord), nil
}

func (s *RuntimeService) sendFileFallback(
	ctx context.Context,
	transport AcceleratedTransport,
	peerDeviceID string,
	lease localfile.Lease,
	transferRecord domain.Transfer,
) (TransferSnapshot, error) {
	localDevice, ok, err := s.store.LoadLocalDevice()
	if err != nil {
		return TransferSnapshot{}, err
	}
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("local device not initialized")
	}
	trustedPeer, ok, err := s.trustedPeer(peerDeviceID)
	if err != nil {
		return TransferSnapshot{}, err
	}
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("peer %s is not trusted", peerDeviceID)
	}
	peer, ok := findDiscoveryPeer(s.discovery.List(), peerDeviceID)
	if !ok {
		return TransferSnapshot{}, fmt.Errorf("peer %s not found", peerDeviceID)
	}
	peer.PinnedFingerprint = trustedPeer.PinnedFingerprint

	file, err := os.Open(lease.Path)
	if err != nil {
		return TransferSnapshot{}, err
	}
	defer file.Close()

	response, err := transport.SendFile(ctx, peer, protocol.FileTransferRequest{
		TransferID:       transferRecord.TransferID,
		MessageID:        transferRecord.MessageID,
		SenderDeviceID:   localDevice.DeviceID,
		FileName:         lease.DisplayName,
		FileSize:         lease.Size,
		CreatedAtRFC3339: transferRecord.CreatedAt.Format(time.RFC3339Nano),
		AgentTCPPort:     s.cfg.AgentTCPPort,
	}, file)
	if err != nil {
		return TransferSnapshot{}, err
	}

	conversation, err := s.store.EnsureConversation(peerDeviceID)
	if err != nil {
		return TransferSnapshot{}, err
	}
	message := domain.Message{
		MessageID:      transferRecord.MessageID,
		ConversationID: conversation.ConversationID,
		Direction:      "outgoing",
		Kind:           "file",
		Body:           lease.DisplayName,
		Status:         "sent",
		CreatedAt:      transferRecord.CreatedAt,
	}
	transferRecord.BytesTransferred = lease.Size
	if response.State != "" {
		transferRecord.State = response.State
	} else {
		transferRecord.State = transfer.StateDone
	}
	if transferRecord.State != transfer.StateDone {
		message.Status = "failed"
	}
	if err := s.store.PersistTransferOutcome(&message, transferRecord); err != nil {
		return s.toTransferSnapshot(transferRecord), nil
	}
	return s.toTransferSnapshot(transferRecord), nil
}
```

- [ ] **Step 5: 把本地 API `/api/transfers/accelerated` 接到应用层**

```go
func NewHTTPServer(appService app.Service, eventBus *EventBus, webAssets ...fs.FS) *HTTPServer {
	server := &HTTPServer{app: appService, bus: eventBus, mux: http.NewServeMux()}
	server.mux.HandleFunc("/api/transfers/accelerated", server.handleAcceleratedTransfers)
	return server
}

func (s *HTTPServer) handleAcceleratedTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		PeerDeviceID string `json:"peerDeviceId"`
		LocalFileID  string `json:"localFileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	snapshot, err := s.app.SendAcceleratedFile(r.Context(), request.PeerDeviceID, request.LocalFileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}
```

- [ ] **Step 6: 再跑定向测试，确认绿色**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/app ./internal/api -run 'AcceleratedSender|AdaptiveParallelism|FallsBackWithSameTransferID|StartsAcceleratedTransfer' -count=1"`

Expected: PASS

- [ ] **Step 7: 提交 sender 与回退闭环**

```bash
rtk git add -- backend/internal/transfer/accelerated_sender.go backend/internal/transfer/accelerated_sender_test.go backend/internal/transfer/adaptive_parallelism.go backend/internal/transfer/adaptive_parallelism_test.go backend/internal/transfer/telemetry.go backend/internal/transfer/telemetry_test.go backend/internal/app/accelerated_transfer_service.go backend/internal/app/accelerated_transfer_service_test.go backend/internal/api/http_server.go backend/internal/api/http_accelerated_transfers_test.go
rtk git commit -m "feat: add accelerated sender and fallback path"
```

### Task 6: 前端 API、会话页入口与进度语义

**Files:**
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/lib/api.test.ts`
- Modify: `frontend/src/AppShell.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/components/ChatPane.tsx`
- Modify: `frontend/src/components/FileMessageCard.tsx`
- Modify: `frontend/src/components/FileMessageCard.test.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.test.tsx`

- [ ] **Step 1: 先写失败测试，锁定选本地文件、发起极速发送与 0 位小数进度展示**

```ts
it("posts local file pick command", async () => {
  const fetchImpl = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({
      localFileId: "lf-1",
      displayName: "demo.bin",
      size: 134217728,
      acceleratedEligible: true,
    }), { status: 200 }),
  );

  const api = createLocalApiClient({ fetchImpl: fetchImpl as typeof fetch });
  await api.pickLocalFile();

  expect(fetchImpl).toHaveBeenCalledWith(
    "http://127.0.0.1:19100/api/local-files/pick",
    expect.objectContaining({ method: "POST" }),
  );
});

it("renders accelerated file action and integer progress", () => {
  render(<FileMessageCard message={message} transfer={{ ...transfer, progressPercent: 63.8 }} />);
  expect(screen.getByText("64%")).toBeInTheDocument();
});
```

- [ ] **Step 2: 跑定向测试，确认红灯**

Run: `rtk powershell -NoProfile -Command "cd frontend; npm test -- lib/api.test.ts FileMessageCard.test.tsx TransferStatusBanner.test.tsx App.test.tsx"`

Expected: FAIL，提示 `pickLocalFile`、`sendAcceleratedFile`、`LocalFileSnapshot` 或整数进度展示尚不存在。

- [ ] **Step 3: 扩展前端 API 与类型**

```ts
export type LocalFileSnapshot = {
  localFileId: string;
  displayName: string;
  size: number;
  acceleratedEligible: boolean;
};

export interface LocalApi {
  bootstrap: () => Promise<BootstrapSnapshot>;
  startPairing: (peerDeviceId: string) => Promise<PairingSnapshot>;
  confirmPairing: (pairingId: string) => Promise<PairingSnapshot>;
  sendText: (peerDeviceId: string, body: string) => Promise<MessageSnapshot>;
  sendFile: (peerDeviceId: string, file: File) => Promise<TransferSnapshot>;
  pickLocalFile: () => Promise<LocalFileSnapshot>;
  sendAcceleratedFile: (peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>;
  subscribeEvents: (options: {
    lastEventSeq?: number;
    onEvent: (event: AgentEvent) => void;
  }) => EventSubscription;
}

async function postJSON<TResponse>(
  fetchImpl: typeof fetch,
  url: string,
  payload?: Record<string, unknown>,
): Promise<TResponse> {
  const response = await fetchImpl(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: payload ? JSON.stringify(payload) : undefined,
  });
  if (!response.ok) {
    throw new Error(`command failed: ${response.status}`);
  }
  return (await response.json()) as TResponse;
}
```

- [ ] **Step 4: 在 `AppShell` 与 `ChatPane` 增加极速发送入口、选中文件态与按钮分流**

```tsx
const [pickedLocalFile, setPickedLocalFile] = useState<LocalFileSnapshot | null>(null);

async function handlePickLocalFile() {
  const next = await resolvedApi.pickLocalFile();
  setPickedLocalFile(next);
}

async function handleSendAcceleratedFile() {
  if (!selectedPeer || !pickedLocalFile) return;
  const transfer = await resolvedApi.sendAcceleratedFile(selectedPeer.deviceId, pickedLocalFile.localFileId);
  startTransition(() => {
    setSnapshot((current) =>
      current ? upsertOutgoingPickedFile(current, selectedPeer.deviceId, pickedLocalFile, transfer) : current,
    );
  });
  setPickedLocalFile(null);
}

function upsertOutgoingPickedFile(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  picked: LocalFileSnapshot,
  transfer: TransferSnapshot,
): BootstrapSnapshot {
  const conversationId = resolveConversationId(snapshot, peerDeviceId) ?? `conv-${peerDeviceId}`;
  const withConversation = ensureConversation(snapshot, peerDeviceId, conversationId);
  const withTransfer = upsertTransfer(withConversation, transfer);
  return upsertMessage(withTransfer, {
    messageId: transfer.messageId,
    conversationId,
    direction: "outgoing",
    kind: "file",
    body: picked.displayName,
    status: transfer.state === "done" ? "sent" : transfer.state,
    createdAt: transfer.createdAt,
  });
}
```

- [ ] **Step 5: 把进度、速率、ETA 展示压成用户可读语义**

```tsx
const percent = transfer.state === "done" ? 100 : Math.min(99, Math.round(transfer.progressPercent));
const rateLabel = transfer.rateBytesPerSec > 0 ? formatRate(transfer.rateBytesPerSec) : "等待速率";
const etaLabel = transfer.etaSeconds != null ? `约 ${transfer.etaSeconds}s` : "计算中";

function formatRate(value: number): string {
  if (value >= 1024 * 1024) {
    return `${Math.round(value / (1024 * 1024))} MB/s`;
  }
  if (value >= 1024) {
    return `${Math.round(value / 1024)} KB/s`;
  }
  return `${Math.round(value)} B/s`;
}
```

- [ ] **Step 6: 再跑定向测试，确认绿色**

Run: `rtk powershell -NoProfile -Command "cd frontend; npm test -- lib/api.test.ts FileMessageCard.test.tsx TransferStatusBanner.test.tsx App.test.tsx"`

Expected: PASS

- [ ] **Step 7: 提交前端极速发送入口**

```bash
rtk git add -- frontend/src/lib/types.ts frontend/src/lib/api.ts frontend/src/lib/api.test.ts frontend/src/AppShell.tsx frontend/src/App.test.tsx frontend/src/components/ChatPane.tsx frontend/src/components/FileMessageCard.tsx frontend/src/components/FileMessageCard.test.tsx frontend/src/components/TransferStatusBanner.tsx frontend/src/components/TransferStatusBanner.test.tsx
rtk git commit -m "feat: add accelerated transfer web ui flow"
```

### Task 7: 全量验证、文档与交付门槛

**Files:**
- Modify: `docs/testing/windows-lan-matrix.md`
- Create: `docs/testing/accelerated-large-file-transfer.md`
- Verify: `backend/...`
- Verify: `frontend/...`
- Verify: `scripts/test.ps1`
- Verify: `scripts/build-agent.ps1`

- [ ] **Step 1: 跑后端全量测试**

Run: `rtk powershell -NoProfile -Command "cd backend; $env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'; $env:GOTELEMETRY='off'; go test -p 1 ./..."`

Expected: PASS

- [ ] **Step 2: 跑前端全量测试**

Run: `rtk powershell -NoProfile -Command "cd frontend; $env:npm_config_cache='E:\Projects\IdeaProjects\person\message-share\.cache\npm'; npm test"`

Expected: PASS

- [ ] **Step 3: 跑统一测试脚本**

Run: `rtk powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`

Expected: PASS

- [ ] **Step 4: 跑构建脚本，确认交付物可产出**

Run: `rtk powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1`

Expected: PASS，并生成 `backend/message-share-agent.exe`

- [ ] **Step 5: 补充真实局域网验证记录**

```text
验证矩阵：
- Windows 发送端 + Windows 接收端
- 已配对 + 可达
- 小文件普通路径
- 大文件极速路径
- 高速路径失败后自动回退
- 进度、速率、ETA 展示正确
- 同一 transferId 未出现重复记录
```

- [ ] **Step 6: 发起多 agent review，并以 OpenSpec specs 为准验收**

```text
Reviewer A：审本地文件桥接与 loopback 安全边界
Reviewer B：审控制面 prepare / token / fallback 一致性
Reviewer C：审 TCP 数据面、WriteAt 与 UI 进度闭环
通过标准：超过半数同意“实现与 specs 场景一致”
```

- [ ] **Step 7: 提交最终收口**

```bash
rtk git add --all
rtk git commit -m "feat: deliver accelerated large file transfer"
```
