# Transfer Telemetry And Presence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为文件发送与接收补齐实时进度、速率、已传/总量和 ETA 展示，同时修正设备 `reachable` 状态语义，解决休眠唤醒后“能收消息但 UI 仍显示不可达”的假阴性问题，并把 Web UI 重构为更有层次感的“温润层叠工作台”。

**Architecture:** 后端在现有流式传输链路上新增轻量 telemetry 采样与活动传输注册表，通过既有事件总线持续推送 `transfer.updated`。发现态与直连态分离建模，`online` 主要由广播 TTL 决定，`reachable` 由广播证据与直连成功证据共同决定。前端在 `AppShell` 汇总活跃传输，在聊天区拆出文件消息状态卡，并用新的视觉层次重做现有布局。

**Tech Stack:** Go、React、TypeScript、Vite、Vitest、SQLite、现有 HTTP + 事件流 API。

---

## 文件结构与职责

### 后端

- Modify: `backend/internal/domain/models.go`
  责任：扩展 `domain.Transfer` 与发现记录所需字段，承接运行态 telemetry 基础字段和可达状态证据字段。
- Modify: `backend/internal/store/sqlite.go`
  责任：扩展 transfer 表结构与读写逻辑，支持必要的中间状态更新。
- Modify: `backend/internal/store/sqlite_test.go`
  责任：验证 transfer 表迁移与新增字段读写。
- Modify: `backend/internal/discovery/service.go`
  责任：将广播发现状态和直连成功状态分离建模，新增 `MarkDirectActive` 等方法。
- Modify: `backend/internal/discovery/service_test.go`
  责任：验证 TTL、`online/reachable` 组合语义和直连成功恢复可达状态。
- Create: `backend/internal/transfer/telemetry.go`
  责任：封装传输计数、平滑速率、ETA 计算与活动传输注册表。
- Create: `backend/internal/transfer/telemetry_test.go`
  责任：验证 telemetry 采样、ETA 边界和 registry 行为。
- Modify: `backend/internal/app/service.go`
  责任：把 telemetry 注入发送/接收链路；在入站成功时恢复 peer 可达状态；扩展 `TransferSnapshot`。
- Modify: `backend/internal/app/service_test.go`
  责任：覆盖发送/接收实时状态、bootstrap 合并活动传输、入站成功恢复可达状态。

### 前端

- Modify: `frontend/src/lib/types.ts`
  责任：同步扩展 `TransferSnapshot` 类型和活跃传输所需字段。
- Modify: `frontend/src/AppShell.tsx`
  责任：汇总活动传输、插入顶部总览条、调整事件合并逻辑。
- Modify: `frontend/src/components/ChatPane.tsx`
  责任：接入新的文件消息卡组件与顶部总览条后的内容层次。
- Create: `frontend/src/components/FileMessageCard.tsx`
  责任：渲染文件消息的进度条、速率、已传/总量、ETA、完成态和失败态。
- Create: `frontend/src/components/TransferStatusBanner.tsx`
  责任：渲染顶部“当前传输中”状态条。
- Modify: `frontend/src/styles.css`
  责任：落地“温润层叠工作台”视觉方向和新增传输组件样式。
- Modify: `frontend/src/App.test.tsx`
  责任：验证整体集成后的主界面、消息卡与顶栏。
- Create: `frontend/src/components/FileMessageCard.test.tsx`
  责任：验证文件卡指标展示与边界状态。
- Create: `frontend/src/components/TransferStatusBanner.test.tsx`
  责任：验证顶部活跃传输条的主任务展示与 ETA 文案。

### 验证与构建

- Modify: `scripts/test.ps1`
  责任：如新增测试文件路径不需要特殊处理则不改；如需要调整顺序，保持后端与前端测试全量覆盖。
- Modify: `scripts/build-agent.ps1`
  责任：仅当新增前端组件或静态资源后验证打包链路仍正常；通常无需逻辑改动。

---

### Task 1: 修正发现与直连可达状态语义

**Files:**
- Modify: `backend/internal/domain/models.go`
- Modify: `backend/internal/discovery/service.go`
- Test: `backend/internal/discovery/service_test.go`
- Test: `backend/internal/app/service_test.go`

- [ ] **Step 1: 写 discovery 侧失败测试，锁定“直连成功可恢复 reachable”**

```go
func TestRegistryMarkDirectActiveKeepsPeerReachableWithoutFreshBroadcast(t *testing.T) {
	registry := NewRegistry()
	now := time.Now().UTC()

	registry.Upsert(Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-1",
		DeviceName:      "peer-one",
		AgentTCPPort:    19090,
	}, "192.168.1.20:52351", now.Add(-10*time.Second))

	registry.MarkDirectActive("peer-1", now)

	records := registry.List()
	if len(records) != 1 {
		t.Fatalf("expected one peer, got %#v", records)
	}
	if records[0].Online {
		t.Fatalf("expected peer to be offline after discovery TTL, got %#v", records[0])
	}
	if !records[0].Reachable {
		t.Fatalf("expected direct activity to keep peer reachable, got %#v", records[0])
	}
}
```

- [ ] **Step 2: 运行 discovery 定向测试，确认新增测试先红灯**

Run: `go test ./internal/discovery -run TestRegistryMarkDirectActiveKeepsPeerReachableWithoutFreshBroadcast`

Expected: FAIL，报 `MarkDirectActive` 未定义或 `Reachable` 仍为 `false`。

- [ ] **Step 3: 写 app 层失败测试，锁定“收到入站文字后恢复可达状态”**

```go
func TestAcceptIncomingTextMessageMarksPeerReachableAgain(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-5",
		DeviceName:        "peer-five",
		PinnedFingerprint: "fingerprint-e",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-5",
		DeviceName:      "peer-five",
		AgentTCPPort:    19090,
	}, "192.168.1.8:19090", time.Now().Add(-10*time.Second))
	registry.MarkReachable("peer-5", false)

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
	})

	if _, err := svc.AcceptIncomingTextMessage(context.Background(), protocol.TextMessageRequest{
		MessageID:        "msg-2",
		SenderDeviceID:   "peer-5",
		Body:             "hi back",
		CreatedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("unexpected accept text error: %v", err)
	}

	bootstrap, err := svc.Bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bootstrap.Peers) != 1 || !bootstrap.Peers[0].Reachable {
		t.Fatalf("expected peer to recover reachable state, got %#v", bootstrap.Peers)
	}
}
```

- [ ] **Step 4: 运行 app 定向测试，确认第二个场景也先红灯**

Run: `go test ./internal/app -run TestAcceptIncomingTextMessageMarksPeerReachableAgain`

Expected: FAIL，`bootstrap.Peers[0].Reachable` 仍为 `false`。

- [ ] **Step 5: 实现 discovery 侧最小改动**

```go
type PeerRecord struct {
	DeviceID           string    `json:"deviceId"`
	DeviceName         string    `json:"deviceName"`
	AgentTCPPort       int       `json:"agentTcpPort"`
	LastKnownAddr      string    `json:"lastKnownAddr,omitempty"`
	PinnedFingerprint  string    `json:"pinnedFingerprint,omitempty"`
	Online             bool      `json:"online"`
	Reachable          bool      `json:"reachable"`
	Trusted            bool      `json:"trusted"`
	DiscoverySource    string    `json:"discoverySource"`
	LastSeenAt         time.Time `json:"lastSeenAt"`
	LastDirectActiveAt time.Time `json:"lastDirectActiveAt"`
}

const (
	peerTTL               = 6 * time.Second
	directReachabilityTTL = 15 * time.Second
)

func (r *Registry) MarkDirectActive(deviceID string, seenAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.peers[deviceID]
	if !ok {
		record.DeviceID = deviceID
	}
	record.LastDirectActiveAt = seenAt.UTC()
	record.Reachable = true
	r.peers[deviceID] = record
}

func (r *Registry) List() []PeerRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	peers := make([]PeerRecord, 0, len(r.peers))
	now := time.Now().UTC()
	for _, peer := range r.peers {
		peer.Online = !peer.LastSeenAt.IsZero() && now.Sub(peer.LastSeenAt) <= peerTTL
		peer.Reachable = (peer.Online && peer.LastKnownAddr != "") ||
			(!peer.LastDirectActiveAt.IsZero() && now.Sub(peer.LastDirectActiveAt) <= directReachabilityTTL)
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].DeviceName == peers[j].DeviceName {
			return peers[i].DeviceID < peers[j].DeviceID
		}
		return peers[i].DeviceName < peers[j].DeviceName
	})
	return peers
}
```

- [ ] **Step 6: 在 app 入站成功路径补最小恢复逻辑**

```go
func (s *RuntimeService) markPeerDirectActive(deviceID string) {
	if s.discovery == nil || strings.TrimSpace(deviceID) == "" {
		return
	}
	s.discovery.MarkDirectActive(deviceID, time.Now().UTC())
	s.publishPeerEvent(deviceID)
}

func (s *RuntimeService) AcceptIncomingTextMessage(
	_ context.Context,
	request protocol.TextMessageRequest,
) (protocol.AckResponse, error) {
	// 现有鉴权和入库逻辑保留
	...
	s.markPeerDirectActive(request.SenderDeviceID)
	s.publishMessageEvent(message)
	return protocol.AckResponse{
		RequestID: request.MessageID,
		Status:    "accepted",
	}, nil
}

func (s *RuntimeService) AcceptIncomingFileTransfer(
	_ context.Context,
	request protocol.FileTransferRequest,
	content io.Reader,
) (protocol.FileTransferResponse, error) {
	// 现有接收逻辑保留
	...
	s.markPeerDirectActive(request.SenderDeviceID)
	s.publishTransferEvent(transferRecord)
	return protocol.FileTransferResponse{
		TransferID: transferID,
		State:      transferRecord.State,
	}, nil
}
```

- [ ] **Step 7: 运行后端相关测试，确认状态语义已闭环**

Run: `go test ./internal/discovery ./internal/app`

Expected: PASS，新增 discovery / app 状态恢复测试通过。

- [ ] **Step 8: 提交本任务**

```bash
git add backend/internal/domain/models.go backend/internal/discovery/service.go backend/internal/discovery/service_test.go backend/internal/app/service.go backend/internal/app/service_test.go
git commit -m "fix: restore peer reachability from direct traffic"
```

---

### Task 2: 为文件传输增加 telemetry 采样与活动注册表

**Files:**
- Create: `backend/internal/transfer/telemetry.go`
- Test: `backend/internal/transfer/telemetry_test.go`
- Modify: `backend/internal/domain/models.go`
- Modify: `backend/internal/store/sqlite.go`
- Test: `backend/internal/store/sqlite_test.go`
- Modify: `backend/internal/app/service.go`
- Test: `backend/internal/app/service_test.go`

- [ ] **Step 1: 写 telemetry 纯单元失败测试，锁定百分比、速率和 ETA**

```go
func TestTelemetrySnapshotComputesProgressRateAndEta(t *testing.T) {
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	telemetry := NewTelemetry("transfer-1", 100)
	telemetry.Start(now)
	telemetry.Advance(40, now.Add(2*time.Second))

	snapshot := telemetry.Snapshot(now.Add(2 * time.Second))
	if snapshot.BytesTransferred != 40 {
		t.Fatalf("expected 40 transferred bytes, got %#v", snapshot)
	}
	if snapshot.ProgressPercent != 40 {
		t.Fatalf("expected 40 percent, got %#v", snapshot)
	}
	if snapshot.RateBytesPerSec <= 0 {
		t.Fatalf("expected positive transfer rate, got %#v", snapshot)
	}
	if snapshot.EtaSeconds == nil || *snapshot.EtaSeconds != 3 {
		t.Fatalf("expected eta to be 3 seconds, got %#v", snapshot)
	}
}
```

- [ ] **Step 2: 运行 telemetry 定向测试，确认先红灯**

Run: `go test ./internal/transfer -run TestTelemetrySnapshotComputesProgressRateAndEta`

Expected: FAIL，`NewTelemetry` 未定义。

- [ ] **Step 3: 写 app 侧失败测试，锁定发送文件过程中先出现活动 transfer**

```go
func TestSendFilePublishesActiveTransferSnapshot(t *testing.T) {
	db, err := openAppTestStore(t)
	if err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
	defer db.Close()

	if err := db.SaveLocalDevice(domain.LocalDevice{
		DeviceID:      "local-1",
		DeviceName:    "my-pc",
		PublicKeyPEM:  "local-public",
		PrivateKeyPEM: "local-private",
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save local device: %v", err)
	}
	if err := db.UpsertTrustedPeer(domain.Peer{
		DeviceID:          "peer-6",
		DeviceName:        "peer-six",
		PinnedFingerprint: "fingerprint-f",
		Trusted:           true,
		UpdatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert trusted peer: %v", err)
	}

	registry := discovery.NewRegistry()
	registry.Upsert(discovery.Announcement{
		ProtocolVersion: "1",
		DeviceID:        "peer-6",
		DeviceName:      "peer-six",
		AgentTCPPort:    19090,
	}, "192.168.1.10:19090", time.Now().UTC())

	publisher := &capturingEventPublisher{}
	transport := &slowPeerTransport{
		fileResponse: protocol.FileTransferResponse{
			TransferID: "transfer-1",
			State:      "done",
		},
	}

	svc := NewRuntimeService(RuntimeDeps{
		Config:    config.Default(),
		Store:     db,
		Discovery: registry,
		Pairings:  session.NewService(),
		Transport: transport,
		Events:    publisher,
	})

	if _, err := svc.SendFile(context.Background(), "peer-6", "hello.txt", 32, bytes.NewReader(bytes.Repeat([]byte("a"), 32))); err != nil {
		t.Fatalf("unexpected send file error: %v", err)
	}

	if !publisher.HasTransferState("sending") {
		t.Fatalf("expected at least one active transfer.updated event, got %#v", publisher.events)
	}
	if !publisher.HasTransferState("done") {
		t.Fatalf("expected final done event, got %#v", publisher.events)
	}
}
```

- [ ] **Step 4: 运行 app 定向测试，确认发送 telemetry 先红灯**

Run: `go test ./internal/app -run TestSendFilePublishesActiveTransferSnapshot`

Expected: FAIL，当前只会在结束后发布一次 `done` 事件。

- [ ] **Step 5: 实现 telemetry 模块最小骨架**

```go
package transfer

import (
	"sync"
	"time"
)

type Snapshot struct {
	BytesTransferred int64
	ProgressPercent  float64
	RateBytesPerSec  float64
	EtaSeconds       *int64
	StartedAt        time.Time
	UpdatedAt        time.Time
}

type Telemetry struct {
	totalBytes       int64
	bytesTransferred int64
	startedAt        time.Time
	updatedAt        time.Time
	mu               sync.Mutex
}

func NewTelemetry(_ string, totalBytes int64) *Telemetry {
	return &Telemetry{totalBytes: totalBytes}
}

func (t *Telemetry) Start(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.startedAt = now.UTC()
	t.updatedAt = now.UTC()
}

func (t *Telemetry) Advance(delta int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bytesTransferred += delta
	t.updatedAt = now.UTC()
}

func (t *Telemetry) Snapshot(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	elapsed := now.Sub(t.startedAt).Seconds()
	var rate float64
	if elapsed > 0 {
		rate = float64(t.bytesTransferred) / elapsed
	}
	progress := 0.0
	if t.totalBytes > 0 {
		progress = (float64(t.bytesTransferred) / float64(t.totalBytes)) * 100
	}
	var eta *int64
	if rate > 0 && t.totalBytes > t.bytesTransferred {
		seconds := int64((float64(t.totalBytes-t.bytesTransferred) / rate) + 0.5)
		eta = &seconds
	}
	return Snapshot{
		BytesTransferred: t.bytesTransferred,
		ProgressPercent:  progress,
		RateBytesPerSec:  rate,
		EtaSeconds:       eta,
		StartedAt:        t.startedAt,
		UpdatedAt:        t.updatedAt,
	}
}
```

- [ ] **Step 6: 在 app 发送/接收链路接入 telemetry 与活动事件**

```go
type RuntimeService struct {
	cfg       config.AppConfig
	store     Store
	discovery *discovery.Registry
	pairings  PairingManager
	events    EventPublisher
	transport PeerTransport
	transfers *transfer.Registry
}

func NewRuntimeService(deps RuntimeDeps) *RuntimeService {
	...
	if deps.Transfers == nil {
		deps.Transfers = transfer.NewRegistry()
	}
	return &RuntimeService{
		cfg:       deps.Config,
		store:     deps.Store,
		discovery: deps.Discovery,
		pairings:  deps.Pairings,
		events:    deps.Events,
		transport: deps.Transport,
		transfers: deps.Transfers,
	}
}

func (s *RuntimeService) SendFile(...) (TransferSnapshot, error) {
	...
	transferRecord := domain.Transfer{
		TransferID:       newRandomID("transfer"),
		MessageID:        message.MessageID,
		FileName:         filepath.Base(fileName),
		FileSize:         fileSize,
		State:            "sending",
		Direction:        "outgoing",
		BytesTransferred: 0,
		CreatedAt:        message.CreatedAt,
	}
	if err := s.store.SaveMessage(message); err != nil { ... }
	if err := s.store.SaveTransfer(transferRecord); err != nil { ... }
	s.publishMessageEvent(message)
	s.publishTransferEvent(transferRecord)

	telemetry := s.transfers.Start(transferRecord.TransferID, transferRecord.FileSize, transferRecord.Direction, transferRecord.CreatedAt)
	reader := transfer.NewProgressReader(content, func(delta int64) {
		telemetry.Advance(delta, time.Now().UTC())
		s.publishTransferEvent(s.mergeTransferTelemetry(transferRecord))
	})

	response, err := s.transport.SendFile(ctx, peer, protocol.FileTransferRequest{...}, reader)
	...
	transferRecord.State = "done"
	transferRecord.BytesTransferred = transferRecord.FileSize
	if err := s.store.UpdateTransferProgress(transferRecord.TransferID, transferRecord.State, transferRecord.FileSize); err != nil { ... }
	s.transfers.Finish(transferRecord.TransferID, time.Now().UTC())
	s.publishTransferEvent(s.mergeTransferTelemetry(transferRecord))
	return toTransferSnapshot(transferRecord), nil
}
```

- [ ] **Step 7: 补 SQLite 迁移与更新接口**

```go
schema := []string{
	`create table if not exists transfers (
		transfer_id text primary key,
		message_id text not null,
		file_name text not null,
		file_size integer not null,
		state text not null,
		direction text not null default 'outgoing',
		bytes_transferred integer not null default 0,
		created_at text not null
	);`,
}

if _, err := raw.Exec(`alter table transfers add column direction text not null default 'outgoing'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
	_ = raw.Close()
	return nil, err
}
if _, err := raw.Exec(`alter table transfers add column bytes_transferred integer not null default 0`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
	_ = raw.Close()
	return nil, err
}

func (db *DB) UpdateTransferProgress(transferID string, state string, bytesTransferred int64) error {
	_, err := db.raw.Exec(
		`update transfers set state = ?, bytes_transferred = ? where transfer_id = ?`,
		state,
		bytesTransferred,
		transferID,
	)
	return err
}
```

- [ ] **Step 8: 运行相关后端测试，确认 telemetry 闭环**

Run: `go test ./internal/transfer ./internal/store ./internal/app`

Expected: PASS，telemetry 单测、发送过程事件、SQLite 迁移与进度更新测试全部通过。

- [ ] **Step 9: 提交本任务**

```bash
git add backend/internal/transfer/telemetry.go backend/internal/transfer/telemetry_test.go backend/internal/domain/models.go backend/internal/store/sqlite.go backend/internal/store/sqlite_test.go backend/internal/app/service.go backend/internal/app/service_test.go
git commit -m "feat: add live transfer telemetry"
```

---

### Task 3: 将活动传输 telemetry 接入前端状态与组件

**Files:**
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/AppShell.tsx`
- Modify: `frontend/src/components/ChatPane.tsx`
- Create: `frontend/src/components/FileMessageCard.tsx`
- Create: `frontend/src/components/FileMessageCard.test.tsx`
- Create: `frontend/src/components/TransferStatusBanner.tsx`
- Create: `frontend/src/components/TransferStatusBanner.test.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: 写文件消息卡失败测试，锁定百分比、速率、总量和 ETA**

```tsx
it("展示文件消息卡的实时进度指标", () => {
  render(
    <FileMessageCard
      message={{
        messageId: "msg-file-1",
        conversationId: "conv-peer-1",
        direction: "outgoing",
        kind: "file",
        body: "design.fig",
        status: "sending",
        createdAt: "2026-04-13T10:00:00Z",
        transfer: {
          transferId: "transfer-1",
          messageId: "msg-file-1",
          fileName: "design.fig",
          fileSize: 34600000,
          state: "sending",
          direction: "outgoing",
          bytesTransferred: 23400000,
          progressPercent: 67.63,
          rateBytesPerSec: 8100000,
          etaSeconds: 2,
          active: true,
          createdAt: "2026-04-13T10:00:00Z",
        },
      }}
    />,
  );

  expect(screen.getByText("design.fig")).toBeInTheDocument();
  expect(screen.getByText("68%")).toBeInTheDocument();
  expect(screen.getByText("8.1 MB/s")).toBeInTheDocument();
  expect(screen.getByText("23.4 / 34.6 MB")).toBeInTheDocument();
  expect(screen.getByText("预计 2 秒")).toBeInTheDocument();
});
```

- [ ] **Step 2: 运行文件消息卡定向测试，确认先红灯**

Run: `npm test -- FileMessageCard.test.tsx`

Expected: FAIL，`FileMessageCard` 文件不存在。

- [ ] **Step 3: 写顶部总览条失败测试，锁定主任务展示**

```tsx
it("展示主活跃传输和附加任务数量", () => {
  render(
    <TransferStatusBanner
      items={[
        {
          transferId: "transfer-1",
          fileName: "design.fig",
          peerDeviceName: "会议室电脑",
          direction: "outgoing",
          progressPercent: 68,
          rateBytesPerSec: 8100000,
          bytesTransferred: 23400000,
          fileSize: 34600000,
          etaSeconds: 2,
          state: "sending",
        },
        {
          transferId: "transfer-2",
          fileName: "notes.zip",
          peerDeviceName: "办公室副机",
          direction: "incoming",
          progressPercent: 41,
          rateBytesPerSec: 5600000,
          bytesTransferred: 12800000,
          fileSize: 31000000,
          etaSeconds: 3,
          state: "receiving",
        },
      ]}
    />,
  );

  expect(screen.getByText("当前传输中")).toBeInTheDocument();
  expect(screen.getByText("design.fig")).toBeInTheDocument();
  expect(screen.getByText("另有 1 个任务进行中")).toBeInTheDocument();
});
```

- [ ] **Step 4: 运行顶栏定向测试，确认第二个场景先红灯**

Run: `npm test -- TransferStatusBanner.test.tsx`

Expected: FAIL，`TransferStatusBanner` 文件不存在。

- [ ] **Step 5: 扩展前端类型和 `AppShell` 派生逻辑**

```ts
export type TransferSnapshot = {
  transferId: string;
  messageId: string;
  fileName: string;
  fileSize: number;
  state: string;
  direction?: "incoming" | "outgoing" | string;
  bytesTransferred?: number;
  progressPercent?: number;
  rateBytesPerSec?: number;
  etaSeconds?: number | null;
  active?: boolean;
  createdAt: string;
};

type ActiveTransferView = {
  transferId: string;
  messageId: string;
  fileName: string;
  fileSize: number;
  state: string;
  direction: string;
  bytesTransferred: number;
  progressPercent: number;
  rateBytesPerSec: number;
  etaSeconds?: number | null;
  peerDeviceName: string;
};

const activeTransfers = useMemo(() => (snapshot ? buildActiveTransfers(snapshot) : []), [snapshot]);
const primaryTransfer = activeTransfers[0];
```

- [ ] **Step 6: 实现文件卡和顶部总览条最小组件**

```tsx
export function TransferStatusBanner({ items }: { items: ActiveTransferView[] }) {
  if (items.length === 0) {
    return null;
  }
  const primary = items[0];
  return (
    <section className="ms-panel ms-transfer-banner" aria-label="当前传输中">
      <div className="ms-transfer-banner__copy">
        <span className="ms-eyebrow">当前传输中</span>
        <h2 className="ms-transfer-banner__title">
          {primary.fileName} 正在{primary.direction === "incoming" ? "接收" : "发送"}
        </h2>
        <p className="ms-transfer-banner__meta">
          {primary.direction === "incoming" ? "来自" : "发送给"} {primary.peerDeviceName}
          {items.length > 1 ? ` · 另有 ${items.length - 1} 个任务进行中` : ""}
        </p>
      </div>
      <div className="ms-transfer-banner__stats">
        <span>{formatProgress(primary.progressPercent)}%</span>
        <span>{formatSpeed(primary.rateBytesPerSec)}</span>
        <span>{formatTransferred(primary.bytesTransferred, primary.fileSize)}</span>
        <span>{formatEta(primary.etaSeconds)}</span>
      </div>
      <div className="ms-progress"><span style={{ width: `${primary.progressPercent}%` }} /></div>
    </section>
  );
}

export function FileMessageCard({ message }: { message: ConversationMessage }) {
  const transfer = message.transfer;
  if (!transfer) {
    return null;
  }
  return (
    <article className={`ms-file-card ms-file-card--${message.direction}`}>
      <div className="ms-file-card__header">
        <strong>{transfer.fileName || message.body}</strong>
        <span className="ms-file-card__state">{formatTransferState(transfer.state)}</span>
      </div>
      <div className="ms-progress ms-progress--file">
        <span style={{ width: `${transfer.progressPercent ?? 0}%` }} />
      </div>
      <div className="ms-file-card__stats">
        <span>{formatProgress(transfer.progressPercent ?? 0)}%</span>
        <span>{formatSpeed(transfer.rateBytesPerSec)}</span>
        <span>{formatTransferred(transfer.bytesTransferred ?? 0, transfer.fileSize)}</span>
        <span>{formatEta(transfer.etaSeconds)}</span>
      </div>
    </article>
  );
}
```

- [ ] **Step 7: 在 `ChatPane` 与 `App.test.tsx` 接入新组件**

```tsx
{messages.map((message) =>
  message.kind === "file" && message.transfer ? (
    <FileMessageCard key={message.messageId} message={message} />
  ) : (
    <article
      key={message.messageId}
      className={`ms-message-card ${
        message.direction === "outgoing" ? "ms-message-card--outgoing" : "ms-message-card--incoming"
      }`}
    >
      ...
    </article>
  ),
)}
```

```tsx
expect(screen.getByText("当前传输中")).toBeInTheDocument();
expect(screen.getByText("设计稿-v2.fig")).toBeInTheDocument();
expect(screen.getByText("预计 2 秒")).toBeInTheDocument();
```

- [ ] **Step 8: 运行前端定向测试，确认组件与集成都转绿**

Run: `npm test -- FileMessageCard.test.tsx TransferStatusBanner.test.tsx App.test.tsx`

Expected: PASS，文件卡与顶部状态条测试通过。

- [ ] **Step 9: 提交本任务**

```bash
git add frontend/src/lib/types.ts frontend/src/AppShell.tsx frontend/src/components/ChatPane.tsx frontend/src/components/FileMessageCard.tsx frontend/src/components/FileMessageCard.test.tsx frontend/src/components/TransferStatusBanner.tsx frontend/src/components/TransferStatusBanner.test.tsx frontend/src/App.test.tsx
git commit -m "feat: surface live transfer status in web ui"
```

---

### Task 4: 落地“温润层叠工作台”视觉重构

**Files:**
- Modify: `frontend/src/styles.css`
- Modify: `frontend/src/components/ChatPane.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.tsx`
- Modify: `frontend/src/components/FileMessageCard.tsx`
- Test: `frontend/src/App.test.tsx`

- [ ] **Step 1: 写主界面视觉失败测试，锁定新层级结构类名**

```tsx
it("渲染温润层叠工作台的传输区层级结构", async () => {
  const api = new FakeApi(bootstrapSnapshotWithTransfer);
  render(<App api={api} />);

  expect(await screen.findByRole("region", { name: "当前传输中" })).toHaveClass("ms-transfer-banner");
  expect(screen.getByText("设计稿-v2.fig").closest("article")).toHaveClass("ms-file-card");
});
```

- [ ] **Step 2: 运行主界面测试，确认视觉结构测试先红灯**

Run: `npm test -- App.test.tsx`

Expected: FAIL，当前不存在 `ms-transfer-banner` 或 `ms-file-card` 结构。

- [ ] **Step 3: 在 `styles.css` 写最小新视觉骨架**

```css
:root {
  --ms-bg: #f6efe6;
  --ms-bg-2: #d8ebe4;
  --ms-panel: rgba(255, 252, 247, 0.86);
  --ms-panel-strong: linear-gradient(180deg, rgba(8, 72, 76, 0.96), rgba(18, 43, 58, 0.98));
  --ms-border: rgba(25, 52, 64, 0.08);
  --ms-shadow: 0 28px 70px rgba(24, 49, 58, 0.14);
  --ms-shadow-soft: 0 18px 32px rgba(24, 49, 58, 0.10);
}

.ms-transfer-banner {
  padding: 22px 24px;
  background:
    radial-gradient(circle at top right, rgba(255, 181, 89, 0.22), transparent 26%),
    linear-gradient(140deg, rgba(14, 63, 71, 0.96), rgba(19, 77, 82, 0.94));
  color: #f5fbfa;
  display: grid;
  gap: 14px;
}

.ms-transfer-banner__stats {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.ms-file-card {
  display: grid;
  gap: 14px;
  padding: 18px;
  border-radius: 24px;
  border: 1px solid rgba(17, 48, 56, 0.08);
  box-shadow: var(--ms-shadow-soft);
}

.ms-file-card--outgoing {
  background: linear-gradient(135deg, rgba(226, 242, 238, 0.96), rgba(244, 250, 248, 0.94));
}

.ms-file-card--incoming {
  background: linear-gradient(135deg, rgba(238, 247, 244, 0.96), rgba(249, 252, 251, 0.94));
}

.ms-file-card__stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}
```

- [ ] **Step 4: 调整组件结构以匹配新视觉层级**

```tsx
<section className="ms-panel ms-chat-panel">
  <div className="ms-chat-header">...</div>
  {canSend ? (
    <>
      <div className="ms-section-head">
        <span className="ms-section-title">最近消息</span>
        <span className="ms-section-hint">传输状态会实时同步到当前页面</span>
      </div>
      <div className="ms-message-list">...</div>
      <form className="ms-composer" onSubmit={handleSubmit}>...</form>
    </>
  ) : null}
</section>
```

- [ ] **Step 5: 运行前端全量测试，确认视觉重构不破坏现有行为**

Run: `npm test`

Expected: PASS，Vitest 全绿。

- [ ] **Step 6: 提交本任务**

```bash
git add frontend/src/styles.css frontend/src/components/ChatPane.tsx frontend/src/components/TransferStatusBanner.tsx frontend/src/components/FileMessageCard.tsx frontend/src/App.test.tsx
git commit -m "feat: restyle web ui for transfer workspace"
```

---

### Task 5: 全量验证、构建与交付验收

**Files:**
- Verify: `backend/internal/app/service_test.go`
- Verify: `backend/internal/discovery/service_test.go`
- Verify: `backend/internal/transfer/telemetry_test.go`
- Verify: `frontend/src/App.test.tsx`
- Verify: `frontend/src/components/FileMessageCard.test.tsx`
- Verify: `frontend/src/components/TransferStatusBanner.test.tsx`
- Verify: `scripts/test.ps1`
- Verify: `scripts/build-agent.ps1`

- [ ] **Step 1: 运行后端全量测试**

Run: `go test ./...`

Expected: PASS，全部 Go 测试通过，无新增失败。

- [ ] **Step 2: 运行前端全量测试**

Run: `npm test`

Expected: PASS，Vitest 全部通过。

- [ ] **Step 3: 运行项目统一测试脚本**

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`

Expected: PASS，后端与前端整体验证通过。

- [ ] **Step 4: 重建单文件代理**

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1`

Expected: PASS，输出新的 `backend/message-share-agent.exe`。

- [ ] **Step 5: 做人工冒烟验收**

```text
1. A、B 已配对时，从 A 向 B 发送大文件：
   - A 顶部出现“当前传输中”总览条
   - A 文件消息卡展示实时进度、速率、已传/总量和 ETA
   - B 同步出现接收中的文件卡与同类指标
2. 让 B 休眠再唤醒：
   - 在广播尚未完全恢复的短窗口内，如果 A 再发文字到 B 且 B 收到，
     B 端应把 A 恢复为 reachable=true，而不是继续显示不可达
3. 刷新页面：
   - 仍能恢复正在进行中的活动传输
```

- [ ] **Step 6: 提交本任务**

```bash
git add backend/message-share-agent.exe
git commit -m "chore: verify transfer telemetry delivery"
```

---

## 自检

### 规格覆盖检查

- 实时传输 telemetry：Task 2、Task 3、Task 5 覆盖。
- 顶部总览条与文件卡：Task 3、Task 4 覆盖。
- “温润层叠工作台”视觉方向：Task 4 覆盖。
- `reachable` 语义修正与休眠唤醒假阴性：Task 1、Task 5 覆盖。
- 刷新页面恢复活动传输：Task 2、Task 3、Task 5 覆盖。

### 占位词检查

- 无 `TODO`、`TBD`、`之后再说` 等占位描述。
- 每个代码步骤都给出了明确代码骨架、文件路径与验证命令。

### 类型与命名一致性检查

- 后端统一使用 `BytesTransferred`、`ProgressPercent`、`RateBytesPerSec`、`EtaSeconds`。
- 前端统一使用 `bytesTransferred`、`progressPercent`、`rateBytesPerSec`、`etaSeconds`。
- 活动传输组件名称固定为 `TransferStatusBanner` 与 `FileMessageCard`。
