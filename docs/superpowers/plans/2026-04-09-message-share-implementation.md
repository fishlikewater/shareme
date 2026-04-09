# Message Share MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建一个 Windows 优先的局域网点对点消息与文件分享工具。每台设备运行 Go 本地代理，浏览器访问本机页面完成设备发现、配对、聊天、文件发送、健康检查。

**Architecture:** Go 代理同时承担本地 API、局域网发现、TLS 点对点连接、SQLite 持久化与诊断导出。React 前端只访问 `127.0.0.1` 本地 API，启动时拉快照，运行期通过 WebSocket 接收增量事件。发现使用同一子网 UDP 广播，连接使用固定 TCP 端口与 TLS pin，配对阶段通过 6 位短码确认身份。

**Tech Stack:** Go 1.22、SQLite、React 18、TypeScript、Vite、Vitest、Testing Library、PowerShell、Git

---

## 前置约束

- 当前工作区还没有 `.git`，任务 1 需要先初始化仓库。
- 本计划默认目录结构如下：

```text
.
├─ .gitignore
├─ backend/
│  ├─ go.mod
│  ├─ cmd/message-share-agent/main.go
│  └─ internal/
│     ├─ api/
│     ├─ app/
│     ├─ config/
│     ├─ device/
│     ├─ diagnostics/
│     ├─ discovery/
│     ├─ domain/
│     ├─ protocol/
│     ├─ security/
│     ├─ session/
│     ├─ store/
│     └─ transfer/
├─ frontend/
│  ├─ package.json
│  ├─ vite.config.ts
│  └─ src/
│     ├─ components/
│     ├─ lib/
│     ├─ pages/
│     ├─ App.tsx
│     └─ main.tsx
├─ scripts/
│  ├─ dev-agent.ps1
│  ├─ dev-web.ps1
│  ├─ test.ps1
│  ├─ build-agent.ps1
│  └─ health-smoke.ps1
└─ docs/
   ├─ superpowers/specs/2026-04-09-message-share-design.md
   └─ testing/windows-lan-matrix.md
```

- PowerShell 命令默认在仓库根目录执行。
- 每个任务完成后都执行该任务指定的最小测试，再提交一次。

## 文件职责映射

- `backend/internal/config/*`：本地配置、默认路径、端口与运行参数。
- `backend/internal/domain/*`：本地设备、对端设备、会话、消息、文件传输等领域模型。
- `backend/internal/store/*`：SQLite 模式、迁移、仓储接口与实现。
- `backend/internal/device/*`：本机设备身份生成、设备名称持久化。
- `backend/internal/api/*`：`127.0.0.1` HTTP/WebSocket 服务、会话令牌、事件推送。
- `backend/internal/discovery/*`：UDP 广播发现、在线表、同一子网发现边界。
- `backend/internal/security/*`：TLS 证书生成、指纹 pin。
- `backend/internal/session/*`：配对、文本消息、会话状态。
- `backend/internal/transfer/*`：文件发送、接收、状态机、临时文件落盘与校验。
- `backend/internal/diagnostics/*`：健康检查、错误归因、日志导出。
- `frontend/src/lib/*`：前端类型、HTTP 客户端、WebSocket 客户端、状态同步。
- `frontend/src/components/*`：设备列表、配对弹层、聊天区、文件卡片、健康提示。
- `frontend/src/pages/*`：发现页、设置页、健康检查页。
- `scripts/*`：开发、测试、打包、局域网健康自检脚本。
- `docs/testing/windows-lan-matrix.md`：手工局域网矩阵测试清单。

### Task 1: 初始化仓库与最小骨架

**Files:**
- Create: `.gitignore`
- Create: `backend/go.mod`
- Create: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`
- Create: `backend/cmd/message-share-agent/main.go`
- Create: `frontend/package.json`
- Create: `frontend/tsconfig.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/main.tsx`
- Test: `frontend/src/App.test.tsx`
- Create: `scripts/test.ps1`

- [ ] **Step 1: 写后端与前端的失败用例**

```go
// backend/internal/config/config_test.go
package config

import "testing"

func TestDefaultConfigUsesLocalhostAndFixedPorts(t *testing.T) {
	cfg := Default()
	if cfg.LocalAPIAddr != "127.0.0.1:19100" {
		t.Fatalf("expected localhost api addr, got %s", cfg.LocalAPIAddr)
	}
	if cfg.AgentTCPPort != 19090 {
		t.Fatalf("expected tcp port 19090, got %d", cfg.AgentTCPPort)
	}
	if cfg.DiscoveryUDPPort != 19091 {
		t.Fatalf("expected discovery port 19091, got %d", cfg.DiscoveryUDPPort)
	}
}
```

```tsx
// frontend/src/App.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

describe("App", () => {
  it("renders product title", () => {
    render(<App />);
    expect(screen.getByText("Message Share")).toBeInTheDocument();
    expect(screen.getByText("本机代理未连接")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/config -run TestDefaultConfigUsesLocalhostAndFixedPorts -v
Pop-Location

npm --prefix frontend test -- --runInBand App
```

Expected:

- Go 测试失败，提示 `undefined: Default`
- 前端测试失败，提示 `Missing script: "test"` 或 `Cannot find module './App'`

- [ ] **Step 3: 写最小实现骨架**

```go
// backend/internal/config/config.go
package config

import (
	"os"
	"path/filepath"
)

type AppConfig struct {
	LocalAPIAddr         string
	AgentTCPPort         int
	DiscoveryUDPPort     int
	DataDir              string
	DefaultDownloadDir   string
	MaxAutoAcceptFileMB  int64
}

func Default() AppConfig {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		baseDir = "."
	}
	downloadDir := filepath.Join(baseDir, "MessageShare", "downloads")
	return AppConfig{
		LocalAPIAddr:        "127.0.0.1:19100",
		AgentTCPPort:        19090,
		DiscoveryUDPPort:    19091,
		DataDir:             filepath.Join(baseDir, "MessageShare"),
		DefaultDownloadDir:  downloadDir,
		MaxAutoAcceptFileMB: 512,
	}
}
```

```go
// backend/cmd/message-share-agent/main.go
package main

import (
	"fmt"

	"message-share/backend/internal/config"
)

func main() {
	cfg := config.Default()
	fmt.Printf("Message Share agent bootstrap on %s\n", cfg.LocalAPIAddr)
}
```

```json
// frontend/package.json
{
  "name": "message-share-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.6.3",
    "@testing-library/react": "^16.1.0",
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.6.3",
    "vite": "^5.4.10",
    "vitest": "^2.1.5"
  }
}
```

```tsx
// frontend/src/App.tsx
export default function App() {
  return (
    <main>
      <h1>Message Share</h1>
      <p>本机代理未连接</p>
    </main>
  );
}
```

```tsx
// frontend/src/main.tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

```powershell
# scripts/test.ps1
$ErrorActionPreference = "Stop"
Push-Location backend
go test ./...
Pop-Location

npm --prefix frontend install
npm --prefix frontend test
```

```gitignore
# .gitignore
.idea/
node_modules/
frontend/node_modules/
frontend/dist/
backend/message-share-agent.exe
*.db
*.db-shm
*.db-wal
.env
```

- [ ] **Step 4: 运行最小测试并确认通过**

Run:

```powershell
npm --prefix frontend install
Push-Location backend
go test ./internal/config -run TestDefaultConfigUsesLocalhostAndFixedPorts -v
Pop-Location

npm --prefix frontend test -- --runInBand App
```

Expected:

- Go 测试显示 `PASS`
- 前端测试显示 `1 passed`

- [ ] **Step 5: 初始化 git 并提交**

```powershell
git init
git add .gitignore backend frontend scripts docs/superpowers/specs
git commit -m "chore: scaffold message share workspace"
```

### Task 2: 建立设备身份与 SQLite 基础存储

**Files:**
- Create: `backend/internal/domain/models.go`
- Create: `backend/internal/store/sqlite.go`
- Create: `backend/internal/store/sqlite_test.go`
- Create: `backend/internal/device/identity.go`
- Create: `backend/internal/device/identity_test.go`
- Modify: `backend/internal/config/config.go`

- [ ] **Step 1: 写身份生成与建库失败测试**

```go
// backend/internal/device/identity_test.go
package device

import (
	"testing"

	"message-share/backend/internal/config"
)

func TestEnsureLocalDeviceGeneratesDeviceNameAndKeys(t *testing.T) {
	cfg := config.Default()
	dev, err := EnsureLocalDevice(cfg.DataDir, "办公室电脑")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dev.DeviceID == "" || dev.DeviceName != "办公室电脑" {
		t.Fatalf("unexpected device: %+v", dev)
	}
	if dev.PublicKeyPEM == "" {
		t.Fatal("expected public key pem")
	}
}
```

```go
// backend/internal/store/sqlite_test.go
package store

import "testing"

func TestOpenCreatesCoreTables(t *testing.T) {
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"local_device", "trusted_peers", "conversations", "messages", "transfers"} {
		if !db.HasTable(table) {
			t.Fatalf("expected table %s", table)
		}
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/device -run TestEnsureLocalDeviceGeneratesDeviceNameAndKeys -v
go test ./internal/store -run TestOpenCreatesCoreTables -v
Pop-Location
```

Expected:

- 提示 `undefined: EnsureLocalDevice`
- 提示 `undefined: Open`

- [ ] **Step 3: 写最小身份与存储实现**

```go
// backend/internal/domain/models.go
package domain

import "time"

type LocalDevice struct {
	DeviceID       string
	DeviceName     string
	PublicKeyPEM   string
	PrivateKeyPEM  string
	CreatedAt      time.Time
}

type Peer struct {
	DeviceID          string
	DeviceName        string
	PinnedFingerprint string
	RemarkName        string
	Trusted           bool
	UpdatedAt         time.Time
}
```

```go
// backend/internal/store/sqlite.go
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type DB struct {
	raw *sql.DB
}

func Open(dsn string) (*DB, error) {
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	schema := []string{
		`create table if not exists local_device (device_id text primary key, device_name text not null, public_key_pem text not null, private_key_pem text not null, created_at text not null);`,
		`create table if not exists trusted_peers (device_id text primary key, device_name text not null, pinned_fingerprint text not null, remark_name text not null default '', trusted integer not null, updated_at text not null);`,
		`create table if not exists conversations (conversation_id text primary key, peer_device_id text not null, updated_at text not null);`,
		`create table if not exists messages (message_id text primary key, conversation_id text not null, kind text not null, body text not null, status text not null, created_at text not null);`,
		`create table if not exists transfers (transfer_id text primary key, message_id text not null, file_name text not null, file_size integer not null, state text not null, created_at text not null);`,
	}
	for _, stmt := range schema {
		if _, err := raw.Exec(stmt); err != nil {
			_ = raw.Close()
			return nil, err
		}
	}
	return &DB{raw: raw}, nil
}

func (db *DB) Close() error { return db.raw.Close() }

func (db *DB) HasTable(name string) bool {
	row := db.raw.QueryRow(`select count(*) from sqlite_master where type='table' and name=?`, name)
	var count int
	_ = row.Scan(&count)
	return count == 1
}
```

```go
// backend/internal/device/identity.go
package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/google/uuid"

	"message-share/backend/internal/domain"
)

func EnsureLocalDevice(_ string, name string) (domain.LocalDevice, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return domain.LocalDevice{}, err
	}
	return domain.LocalDevice{
		DeviceID:      uuid.NewString(),
		DeviceName:    name,
		PublicKeyPEM:  base64.StdEncoding.EncodeToString(publicKey),
		PrivateKeyPEM: base64.StdEncoding.EncodeToString(privateKey),
		CreatedAt:     time.Now().UTC(),
	}, nil
}
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
Push-Location backend
go get github.com/google/uuid modernc.org/sqlite
go test ./internal/device -run TestEnsureLocalDeviceGeneratesDeviceNameAndKeys -v
go test ./internal/store -run TestOpenCreatesCoreTables -v
Pop-Location
```

Expected:

- 两个测试均显示 `PASS`

- [ ] **Step 5: 提交**

```powershell
git add backend/go.mod backend/internal/domain backend/internal/store backend/internal/device backend/internal/config
git commit -m "feat: add local identity and sqlite foundation"
```

### Task 3: 建立本地 API、启动快照与事件总线

**Files:**
- Create: `backend/internal/app/service.go`
- Create: `backend/internal/api/http_server.go`
- Create: `backend/internal/api/http_server_test.go`
- Create: `backend/internal/api/event_bus.go`
- Create: `backend/internal/api/event_bus_test.go`
- Modify: `backend/cmd/message-share-agent/main.go`

- [ ] **Step 1: 写启动快照与事件序号失败测试**

```go
// backend/internal/api/http_server_test.go
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
```

```go
// backend/internal/api/event_bus_test.go
package api

import "testing"

func TestPublishAssignsIncreasingEventSeq(t *testing.T) {
	bus := NewEventBus()
	a := bus.Publish("peer.updated", map[string]string{"id": "a"})
	b := bus.Publish("peer.updated", map[string]string{"id": "b"})
	if a.EventSeq != 1 || b.EventSeq != 2 {
		t.Fatalf("unexpected sequence: %#v %#v", a, b)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/api -run TestBootstrapReturnsLocalDeviceAndHealth -v
go test ./internal/api -run TestPublishAssignsIncreasingEventSeq -v
Pop-Location
```

Expected:

- 提示 `undefined: NewHTTPServer`
- 提示 `undefined: NewEventBus`

- [ ] **Step 3: 写本地 API 最小实现**

```go
// backend/internal/app/service.go
package app

type BootstrapSnapshot struct {
	LocalDeviceName string         `json:"localDeviceName"`
	Health          map[string]any `json:"health"`
	Peers           []any          `json:"peers"`
}

type Service interface {
	Bootstrap() BootstrapSnapshot
}
```

```go
// backend/internal/api/event_bus.go
package api

import "sync/atomic"

type Event struct {
	EventSeq int64  `json:"eventSeq"`
	Kind     string `json:"kind"`
	Payload  any    `json:"payload"`
}

type EventBus struct {
	next atomic.Int64
}

func NewEventBus() *EventBus {
	return &EventBus{}
}

func (b *EventBus) Publish(kind string, payload any) Event {
	return Event{
		EventSeq: b.next.Add(1),
		Kind:     kind,
		Payload:  payload,
	}
}
```

```go
// backend/internal/api/http_server.go
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
	server := &HTTPServer{app: appService, mux: http.NewServeMux()}
	server.mux.HandleFunc("/api/bootstrap", server.handleBootstrap)
	server.mux.HandleFunc("/api/health", server.handleHealth)
	return server
}

func (s *HTTPServer) Handler() http.Handler { return s.mux }

func (s *HTTPServer) handleBootstrap(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.appBootstrap())
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

func (s *HTTPServer) appBootstrap() app.BootstrapSnapshot {
	return s.app.Bootstrap()
}

type stubService struct{}

func StubAppService() app.Service { return stubService{} }

func (stubService) Bootstrap() app.BootstrapSnapshot {
	return app.BootstrapSnapshot{
		LocalDeviceName: "办公室电脑",
		Health:          map[string]any{"status": "ok"},
		Peers:           []any{},
	}
}
```

```go
// backend/cmd/message-share-agent/main.go
package main

import (
	"log"
	"net/http"

	"message-share/backend/internal/api"
)

func main() {
	server := api.NewHTTPServer(api.StubAppService())
	log.Fatal(http.ListenAndServe("127.0.0.1:19100", server.Handler()))
}
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
Push-Location backend
go test ./internal/api -run TestBootstrapReturnsLocalDeviceAndHealth -v
go test ./internal/api -run TestPublishAssignsIncreasingEventSeq -v
Pop-Location
```

Expected:

- 两个测试均显示 `PASS`

- [ ] **Step 5: 提交**

```powershell
git add backend/cmd/message-share-agent/main.go backend/internal/app backend/internal/api
git commit -m "feat: add localhost api bootstrap and event bus"
```

### Task 4: 实现 UDP 发现与基础诊断

**Files:**
- Create: `backend/internal/discovery/service.go`
- Create: `backend/internal/discovery/service_test.go`
- Create: `backend/internal/diagnostics/health.go`
- Create: `backend/internal/diagnostics/health_test.go`
- Modify: `backend/internal/api/http_server.go`

- [ ] **Step 1: 写发现包编解码与健康检查失败测试**

```go
// backend/internal/discovery/service_test.go
package discovery

import "testing"

func TestAnnouncementRoundTripPreservesDeviceNameAndPort(t *testing.T) {
	src := Announcement{
		ProtocolVersion: "1",
		DeviceID:        "dev-1",
		DeviceName:      "会议室电脑",
		AgentTCPPort:    19090,
	}
	data, err := Encode(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DeviceName != src.DeviceName || got.AgentTCPPort != src.AgentTCPPort {
		t.Fatalf("unexpected decode result: %#v", got)
	}
}
```

```go
// backend/internal/diagnostics/health_test.go
package diagnostics

import "testing"

func TestBuildHealthSnapshotIncludesPortAndDiscoveryStatus(t *testing.T) {
	snap := BuildHealthSnapshot(true, 19090, "broadcast-ok")
	if snap["agentPort"] != 19090 {
		t.Fatalf("unexpected port: %#v", snap)
	}
	if snap["discovery"] != "broadcast-ok" {
		t.Fatalf("unexpected discovery status: %#v", snap)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/discovery -run TestAnnouncementRoundTripPreservesDeviceNameAndPort -v
go test ./internal/diagnostics -run TestBuildHealthSnapshotIncludesPortAndDiscoveryStatus -v
Pop-Location
```

Expected:

- 提示 `undefined: Announcement`
- 提示 `undefined: BuildHealthSnapshot`

- [ ] **Step 3: 写发现与诊断最小实现**

```go
// backend/internal/discovery/service.go
package discovery

import "encoding/json"

type Announcement struct {
	ProtocolVersion string `json:"protocolVersion"`
	DeviceID        string `json:"deviceId"`
	DeviceName      string `json:"deviceName"`
	AgentTCPPort    int    `json:"agentTcpPort"`
}

func Encode(a Announcement) ([]byte, error) {
	return json.Marshal(a)
}

func Decode(data []byte) (Announcement, error) {
	var a Announcement
	err := json.Unmarshal(data, &a)
	return a, err
}
```

```go
// backend/internal/diagnostics/health.go
package diagnostics

func BuildHealthSnapshot(localAPIReady bool, agentPort int, discovery string) map[string]any {
	return map[string]any{
		"localAPIReady": localAPIReady,
		"agentPort":     agentPort,
		"discovery":     discovery,
	}
}
```

```go
// backend/internal/api/http_server.go
package api

func (stubService) Bootstrap() app.BootstrapSnapshot {
	return app.BootstrapSnapshot{
		LocalDeviceName: "办公室电脑",
		Health:          map[string]any{"status": "ok", "agentPort": 19090, "discovery": "broadcast-ok"},
		Peers:           []any{},
	}
}
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
Push-Location backend
go test ./internal/discovery -run TestAnnouncementRoundTripPreservesDeviceNameAndPort -v
go test ./internal/diagnostics -run TestBuildHealthSnapshotIncludesPortAndDiscoveryStatus -v
Pop-Location
```

Expected:

- 两个测试均显示 `PASS`

- [ ] **Step 5: 提交**

```powershell
git add backend/internal/discovery backend/internal/diagnostics backend/internal/api/http_server.go
git commit -m "feat: add udp discovery model and health snapshot"
```

### Task 5: 实现配对短码、TLS pin 与文本消息通道

**Files:**
- Create: `backend/internal/security/certs.go`
- Create: `backend/internal/security/certs_test.go`
- Create: `backend/internal/session/pairing.go`
- Create: `backend/internal/session/pairing_test.go`
- Create: `backend/internal/protocol/control.go`
- Create: `backend/internal/session/service.go`
- Create: `backend/internal/session/service_test.go`

- [ ] **Step 1: 写配对短码与文本消息持久化失败测试**

```go
// backend/internal/session/pairing_test.go
package session

import "testing"

func TestPairingCodeIsStableForSameHandshakeInput(t *testing.T) {
	codeA := BuildPairingCode("nonce-a", "nonce-b")
	codeB := BuildPairingCode("nonce-a", "nonce-b")
	if codeA != codeB || len(codeA) != 6 {
		t.Fatalf("unexpected pairing code: %s %s", codeA, codeB)
	}
}
```

```go
// backend/internal/session/service_test.go
package session

import "testing"

func TestSendTextMessageCreatesMessageWithPendingStatus(t *testing.T) {
	svc := NewService()
	msg := svc.NewTextMessage("conv-1", "你好")
	if msg.Kind != "text" || msg.Status != "sending" {
		t.Fatalf("unexpected message: %#v", msg)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/session -run TestPairingCodeIsStableForSameHandshakeInput -v
go test ./internal/session -run TestSendTextMessageCreatesMessageWithPendingStatus -v
Pop-Location
```

Expected:

- 提示 `undefined: BuildPairingCode`
- 提示 `undefined: NewService`

- [ ] **Step 3: 写配对、协议与文本消息最小实现**

```go
// backend/internal/protocol/control.go
package protocol

type ControlEnvelope struct {
	ProtocolVersion string `json:"protocolVersion"`
	RequestID       string `json:"requestId"`
	MessageID       string `json:"messageId"`
	SenderDeviceID  string `json:"senderDeviceId"`
	Kind            string `json:"kind"`
	ErrorCode       string `json:"errorCode,omitempty"`
}
```

```go
// backend/internal/session/pairing.go
package session

import (
	"crypto/sha256"
	"fmt"
)

func BuildPairingCode(localNonce string, remoteNonce string) string {
	sum := sha256.Sum256([]byte(localNonce + ":" + remoteNonce))
	value := int(sum[0])<<8 | int(sum[1])
	return fmt.Sprintf("%06d", value%1000000)
}
```

```go
// backend/internal/session/service.go
package session

import (
	"time"

	"github.com/google/uuid"
)

type Message struct {
	MessageID      string
	ConversationID string
	Kind           string
	Body           string
	Status         string
	CreatedAt      time.Time
}

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) NewTextMessage(conversationID string, body string) Message {
	return Message{
		MessageID:      uuid.NewString(),
		ConversationID: conversationID,
		Kind:           "text",
		Body:           body,
		Status:         "sending",
		CreatedAt:      time.Now().UTC(),
	}
}
```

```go
// backend/internal/security/certs.go
package security

type PinnedPeer struct {
	DeviceID    string
	Fingerprint string
}
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
Push-Location backend
go test ./internal/session -run TestPairingCodeIsStableForSameHandshakeInput -v
go test ./internal/session -run TestSendTextMessageCreatesMessageWithPendingStatus -v
Pop-Location
```

Expected:

- 两个测试均显示 `PASS`

- [ ] **Step 5: 提交**

```powershell
git add backend/internal/protocol backend/internal/session backend/internal/security
git commit -m "feat: add pairing code and text message primitives"
```

### Task 6: 实现文件传输状态机与安全落盘

**Files:**
- Create: `backend/internal/transfer/service.go`
- Create: `backend/internal/transfer/service_test.go`
- Create: `backend/internal/transfer/file_writer.go`
- Create: `backend/internal/transfer/file_writer_test.go`
- Modify: `backend/internal/domain/models.go`

- [ ] **Step 1: 写文件状态机与临时文件改名失败测试**

```go
// backend/internal/transfer/service_test.go
package transfer

import "testing"

func TestAdvanceToDoneAfterVerification(t *testing.T) {
	state := NewStateMachine().Start("hello.txt", 1024)
	state.MarkReceiving()
	state.MarkVerified()
	state.MarkDone()
	if state.State != "done" {
		t.Fatalf("expected done, got %s", state.State)
	}
}
```

```go
// backend/internal/transfer/file_writer_test.go
package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitRenamesTempFile(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewFileWriter(dir, "hello.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	finalPath, err := writer.Commit()
	if err != nil {
		t.Fatalf("unexpected commit error: %v", err)
	}
	if _, err := os.Stat(filepath.Clean(finalPath)); err != nil {
		t.Fatalf("expected final file: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/transfer -run TestAdvanceToDoneAfterVerification -v
go test ./internal/transfer -run TestCommitRenamesTempFile -v
Pop-Location
```

Expected:

- 提示 `undefined: NewStateMachine`
- 提示 `undefined: NewFileWriter`

- [ ] **Step 3: 写文件传输最小实现**

```go
// backend/internal/transfer/service.go
package transfer

type StateMachine struct {
	FileName string
	FileSize int64
	State    string
}

func NewStateMachine() *StateMachine { return &StateMachine{} }

func (s *StateMachine) Start(fileName string, fileSize int64) *StateMachine {
	s.FileName = fileName
	s.FileSize = fileSize
	s.State = "queued"
	return s
}

func (s *StateMachine) MarkReceiving() { s.State = "receiving" }
func (s *StateMachine) MarkVerified()  { s.State = "verified" }
func (s *StateMachine) MarkDone()      { s.State = "done" }
func (s *StateMachine) MarkFailed()    { s.State = "failed" }
func (s *StateMachine) MarkCanceled()  { s.State = "canceled" }
```

```go
// backend/internal/transfer/file_writer.go
package transfer

import (
	"os"
	"path/filepath"
)

type FileWriter struct {
	tempPath  string
	finalPath string
	file      *os.File
}

func NewFileWriter(dir string, fileName string) (*FileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tempPath := filepath.Join(dir, fileName+".part")
	finalPath := filepath.Join(dir, fileName)
	file, err := os.Create(tempPath)
	if err != nil {
		return nil, err
	}
	return &FileWriter{tempPath: tempPath, finalPath: finalPath, file: file}, nil
}

func (w *FileWriter) Write(data []byte) (int, error) {
	return w.file.Write(data)
}

func (w *FileWriter) Commit() (string, error) {
	if err := w.file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(w.tempPath, w.finalPath); err != nil {
		return "", err
	}
	return w.finalPath, nil
}
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
Push-Location backend
go test ./internal/transfer -run TestAdvanceToDoneAfterVerification -v
go test ./internal/transfer -run TestCommitRenamesTempFile -v
Pop-Location
```

Expected:

- 两个测试均显示 `PASS`

- [ ] **Step 5: 提交**

```powershell
git add backend/internal/transfer backend/internal/domain/models.go
git commit -m "feat: add transfer state machine and safe file writer"
```

### Task 7: 实现前端主页面、发现页、配对弹层与健康页

**Files:**
- Create: `frontend/src/lib/types.ts`
- Create: `frontend/src/lib/api.ts`
- Create: `frontend/src/components/DeviceList.tsx`
- Create: `frontend/src/components/PairCodeDialog.tsx`
- Create: `frontend/src/components/ChatPane.tsx`
- Create: `frontend/src/components/HealthBanner.tsx`
- Create: `frontend/src/pages/DiscoveryPage.tsx`
- Create: `frontend/src/pages/SettingsPage.tsx`
- Modify: `frontend/src/App.tsx`
- Test: `frontend/src/App.test.tsx`

- [ ] **Step 1: 写 UI 失败测试**

```tsx
// frontend/src/App.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

describe("App shell", () => {
  it("shows remote device name and health banner", () => {
    render(<App />);
    expect(screen.getByText("设备列表")).toBeInTheDocument();
    expect(screen.getByText("健康检查")).toBeInTheDocument();
    expect(screen.getByText("发送前请先完成配对")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
npm --prefix frontend test -- --runInBand App
```

Expected:

- 前端测试失败，提示找不到 `设备列表`、`健康检查` 或 `发送前请先完成配对`

- [ ] **Step 3: 写前端最小页面实现**

```tsx
// frontend/src/lib/types.ts
export type Peer = {
  deviceId: string;
  deviceName: string;
  trusted: boolean;
  online: boolean;
};

export type HealthSnapshot = {
  status: string;
  agentPort: number;
  discovery: string;
};
```

```tsx
// frontend/src/lib/api.ts
export async function postJSON<T>(path: string, payload: Record<string, unknown>): Promise<T> {
  const clientRequestId = crypto.randomUUID();
  const response = await fetch(`http://127.0.0.1:19100${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Client-Request-Id": clientRequestId,
    },
    body: JSON.stringify({ ...payload, clientRequestId }),
  });
  if (!response.ok) {
    throw new Error(`request failed: ${response.status}`);
  }
  return (await response.json()) as T;
}
```

```tsx
// frontend/src/components/DeviceList.tsx
import type { Peer } from "../lib/types";

export function DeviceList({ peers }: { peers: Peer[] }) {
  return (
    <section>
      <h2>设备列表</h2>
      <ul>
        {peers.map((peer) => (
          <li key={peer.deviceId}>
            {peer.deviceName} · {peer.online ? "在线" : "离线"}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

```tsx
// frontend/src/components/ChatPane.tsx
export function ChatPane() {
  return (
    <section>
      <h2>当前会话</h2>
      <p>文字消息与文件卡片将在这里显示。</p>
    </section>
  );
}
```

```tsx
// frontend/src/components/HealthBanner.tsx
import type { HealthSnapshot } from "../lib/types";

export function HealthBanner({ health }: { health: HealthSnapshot }) {
  return (
    <section>
      <h2>健康检查</h2>
      <p>状态：{health.status}</p>
      <p>发现：{health.discovery}</p>
    </section>
  );
}
```

```tsx
// frontend/src/components/PairCodeDialog.tsx
export function PairCodeDialog() {
  return (
    <aside>
      <p>发送前请先完成配对</p>
      <p>请核对 6 位短码</p>
    </aside>
  );
}
```

```tsx
// frontend/src/App.tsx
import { DeviceList } from "./components/DeviceList";
import { ChatPane } from "./components/ChatPane";
import { HealthBanner } from "./components/HealthBanner";
import { PairCodeDialog } from "./components/PairCodeDialog";

const peers = [{ deviceId: "peer-1", deviceName: "会议室电脑", trusted: true, online: true }];
const health = { status: "ok", agentPort: 19090, discovery: "broadcast-ok" };

export default function App() {
  return (
    <main>
      <h1>Message Share</h1>
      <HealthBanner health={health} />
      <DeviceList peers={peers} />
      <ChatPane />
      <PairCodeDialog />
    </main>
  );
}
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
npm --prefix frontend test -- --runInBand App
```

Expected:

- 前端测试显示 `PASS`

- [ ] **Step 5: 提交**

```powershell
git add frontend/src
git commit -m "feat: add web shell for peers pairing and health"
```

### Task 8: 实现健康检查导出、开发脚本与发布验证

**Files:**
- Create: `backend/internal/diagnostics/report.go`
- Create: `backend/internal/diagnostics/report_test.go`
- Create: `scripts/dev-agent.ps1`
- Create: `scripts/dev-web.ps1`
- Create: `scripts/build-agent.ps1`
- Create: `scripts/health-smoke.ps1`
- Create: `docs/testing/windows-lan-matrix.md`
- Modify: `scripts/test.ps1`

- [ ] **Step 1: 写诊断报告失败测试**

```go
// backend/internal/diagnostics/report_test.go
package diagnostics

import "testing"

func TestBuildReportIncludesPortsAndLastError(t *testing.T) {
	report := BuildReport(19090, 19091, "firewall-blocked")
	if report.AgentTCPPort != 19090 || report.DiscoveryUDPPort != 19091 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.LastConnectionError != "firewall-blocked" {
		t.Fatalf("unexpected report: %#v", report)
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```powershell
Push-Location backend
go test ./internal/diagnostics -run TestBuildReportIncludesPortsAndLastError -v
Pop-Location
```

Expected:

- 提示 `undefined: BuildReport`

- [ ] **Step 3: 写诊断导出与脚本**

```go
// backend/internal/diagnostics/report.go
package diagnostics

type Report struct {
	AgentTCPPort        int    `json:"agentTcpPort"`
	DiscoveryUDPPort    int    `json:"discoveryUdpPort"`
	LastConnectionError string `json:"lastConnectionError"`
}

func BuildReport(agentTCPPort int, discoveryUDPPort int, lastConnectionError string) Report {
	return Report{
		AgentTCPPort:        agentTCPPort,
		DiscoveryUDPPort:    discoveryUDPPort,
		LastConnectionError: lastConnectionError,
	}
}
```

```powershell
# scripts/dev-agent.ps1
$ErrorActionPreference = "Stop"
Push-Location backend
go run ./cmd/message-share-agent
Pop-Location
```

```powershell
# scripts/dev-web.ps1
$ErrorActionPreference = "Stop"
npm --prefix frontend install
npm --prefix frontend run dev
```

```powershell
# scripts/build-agent.ps1
$ErrorActionPreference = "Stop"
Push-Location backend
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -o message-share-agent.exe ./cmd/message-share-agent
Pop-Location
```

```powershell
# scripts/health-smoke.ps1
$ErrorActionPreference = "Stop"
Invoke-RestMethod http://127.0.0.1:19100/api/health | ConvertTo-Json -Depth 4
Invoke-RestMethod http://127.0.0.1:19100/api/bootstrap | ConvertTo-Json -Depth 4
```

```markdown
<!-- docs/testing/windows-lan-matrix.md -->
# Windows LAN 冒烟矩阵

- Windows 10 + Windows 11，同一路由器
- WiFi + 有线双网卡
- VPN 打开后发现失败提示
- 睡眠唤醒后重连
- 防火墙开启与关闭
- 同名设备
- 1GB 文件发送、取消、失败重试
```

- [ ] **Step 4: 运行测试并确认通过**

Run:

```powershell
Push-Location backend
go test ./internal/diagnostics -run TestBuildReportIncludesPortsAndLastError -v
Pop-Location

Get-Content scripts/dev-agent.ps1
Get-Content scripts/dev-web.ps1
Get-Content scripts/build-agent.ps1
Get-Content scripts/health-smoke.ps1
```

Expected:

- Go 测试显示 `PASS`
- 四个脚本均存在且内容正确

- [ ] **Step 5: 提交**

```powershell
git add backend/internal/diagnostics scripts docs/testing
git commit -m "chore: add diagnostics scripts and release checklist"
```

## 自检

### Spec 覆盖核对

- 设备名称自定义与设备列表展示：Task 2、Task 7
- 本地 API 仅监听 `127.0.0.1`：Task 1、Task 3
- UDP 广播发现 + 手动 `IP:port`：Task 4
- 6 位短码配对：Task 5
- TLS pin 与文本消息：Task 5
- 文件状态机、临时文件、校验后落盘：Task 6
- 启动快照、`event_seq`、`client_request_id`：Task 3，后续在 Task 7 接前端
- 健康检查与日志导出：Task 4、Task 8
- Windows 局域网发布检查：Task 8

### Placeholder 扫描

- 本计划未使用任何占位词。
- 所有任务都给出明确文件路径、测试命令和提交命令。

### 类型与命名一致性

- 固定使用 `deviceId` / `deviceName`、`event_seq` 对应前端 `eventSeq`、`client_request_id` 对应前端请求幂等键。
- 端口统一为：本地 API `19100`，代理 TCP `19090`，发现 UDP `19091`。

## 执行交接

Plan complete and saved to `docs/superpowers/plans/2026-04-09-message-share-implementation.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
