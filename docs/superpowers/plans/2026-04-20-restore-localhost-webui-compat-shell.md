# Localhost Web UI Compat Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 恢复 `message-share-agent` 的 localhost 浏览器兼容入口，同时保持 Wails 桌面版仍是正式产品入口。

**Architecture:** 前端继续以 `LocalApi` 作为唯一宿主边界，新增 `LocalhostApiClient` 适配浏览器 localhost 场景；后端在既有 runtime host 之外增加 loopback-only HTTP/SSE 兼容壳，并通过共享前端资源选择器与共享命令门面复用现有业务能力。

**Tech Stack:** Go 1.25, `net/http`, `embed`, React 18, Vite, Vitest, Wails v2, SQLite, SSE

---

## File Map

**前端宿主适配**
- Create: `frontend/src/lib/api.test.ts`
- Create: `frontend/src/lib/localhost-api.ts`
- Create: `frontend/src/lib/localhost-api.test.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/AppShell.tsx`

**共享后端能力**
- Create: `backend/internal/frontendassets/select.go`
- Create: `backend/internal/frontendassets/select_test.go`
- Create: `backend/internal/localui/service.go`
- Create: `backend/internal/localui/server.go`
- Create: `backend/internal/localui/server_test.go`
- Create: `backend/internal/localui/events_test.go`
- Create: `backend/internal/localui/upload_test.go`
- Modify: `backend/main.go`
- Modify: `backend/main_test.go`

**agent 兼容入口**
- Create: `backend/cmd/message-share-agent/assets_embed.go`
- Create: `backend/cmd/message-share-agent/frontend/index.html`
- Modify: `backend/cmd/message-share-agent/main.go`
- Modify: `backend/cmd/message-share-agent/main_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

**构建、验证与文档**
- Create: `scripts/build-agent.ps1`
- Create: `scripts/build-agent.sh`
- Create: `scripts/smoke-agent.ps1`
- Create: `scripts/smoke-agent.sh`
- Create: `docs/testing/agent-localhost-runtime.md`
- Modify: `scripts/test.ps1`
- Modify: `README.md`

### Task 1: 前端双宿主适配

**Files:**
- Create: `frontend/src/lib/api.test.ts`
- Create: `frontend/src/lib/localhost-api.ts`
- Create: `frontend/src/lib/localhost-api.test.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/AppShell.tsx`
- Test: `frontend/src/lib/api.test.ts`
- Test: `frontend/src/lib/localhost-api.test.ts`

- [x] **Step 1: 先写前端 failing tests，锁定宿主选择、SSE 去重和浏览器普通文件发送语义**

```ts
// frontend/src/lib/api.test.ts
import { afterEach, describe, expect, it, vi } from "vitest";

const mockApi = {
  bootstrap: vi.fn(),
  startPairing: vi.fn(),
  confirmPairing: vi.fn(),
  sendText: vi.fn(),
  sendFile: vi.fn(),
  pickLocalFile: vi.fn(),
  sendAcceleratedFile: vi.fn(),
  listMessageHistory: vi.fn(),
  subscribeEvents: vi.fn(() => ({ close: vi.fn(), reconnect: vi.fn() })),
};

const createDesktopApiClient = vi.fn(() => mockApi);
const createLocalhostApiClient = vi.fn(() => mockApi);
const hasDesktopApiBindings = vi.fn();

vi.mock("./desktop-api", () => ({
  createDesktopApiClient,
  hasDesktopApiBindings,
}));

vi.mock("./localhost-api", () => ({
  createLocalhostApiClient,
}));

describe("createDefaultLocalApi", () => {
  const originalWindow = globalThis.window;

  afterEach(() => {
    vi.resetModules();
    hasDesktopApiBindings.mockReset();
    createDesktopApiClient.mockClear();
    createLocalhostApiClient.mockClear();
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: originalWindow,
    });
  });

  it("prefers desktop bindings when available", async () => {
    hasDesktopApiBindings.mockReturnValue(true);
    const { createDefaultLocalApi } = await import("./api");
    const api = createDefaultLocalApi();
    expect(api).toBe(mockApi);
    expect(createDesktopApiClient).toHaveBeenCalledTimes(1);
    expect(createLocalhostApiClient).not.toHaveBeenCalled();
  });

  it("falls back to localhost client on loopback browser pages", async () => {
    hasDesktopApiBindings.mockReturnValue(false);
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        location: {
          hostname: "127.0.0.1",
          origin: "http://127.0.0.1:52350",
        },
      },
    });

    const { createDefaultLocalApi } = await import("./api");
    const api = createDefaultLocalApi();
    expect(api).toBe(mockApi);
    expect(createLocalhostApiClient).toHaveBeenCalledWith({
      origin: "http://127.0.0.1:52350",
    });
  });
});
```

```ts
// frontend/src/lib/localhost-api.test.ts
import { describe, expect, it, vi } from "vitest";

import { createLocalhostApiClient } from "./localhost-api";

class MockEventSource {
  url: string;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
  }

  emit(payload: unknown) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(payload) }));
  }

  close() {
    this.closed = true;
  }
}

it("streams events from afterSeq and ignores duplicates", async () => {
  const sources: MockEventSource[] = [];
  const api = createLocalhostApiClient({
    origin: "http://127.0.0.1:52350",
    fetchFn: vi.fn(),
    createEventSource: (url) => {
      const source = new MockEventSource(url);
      sources.push(source);
      return source as unknown as EventSource;
    },
    pickFile: vi.fn(),
  });

  const received: number[] = [];
  const subscription = api.subscribeEvents({
    lastEventSeq: 7,
    onEvent(event) {
      received.push(event.eventSeq);
    },
  });

  expect(sources[0]?.url).toContain("afterSeq=7");
  sources[0]?.emit({ eventSeq: 8, kind: "peer.updated", payload: { deviceId: "peer-1" } });
  sources[0]?.emit({ eventSeq: 8, kind: "peer.updated", payload: { deviceId: "peer-1" } });
  sources[0]?.emit({ eventSeq: 9, kind: "health.updated", payload: { status: "ok" } });

  expect(received).toEqual([8, 9]);
  subscription.close();
  expect(sources[0]?.closed).toBe(true);
});

it("uploads browser-selected file as multipart without a pre-stage file", async () => {
  const fetchFn = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ transferId: "tx-1", fileName: "demo.txt" }),
  });
  const file = new File(["hello"], "demo.txt", { type: "text/plain" });

  const api = createLocalhostApiClient({
    origin: "http://127.0.0.1:52350",
    fetchFn,
    createEventSource: vi.fn(),
    pickFile: vi.fn().mockResolvedValue(file),
  });

  await api.sendFile("peer-1");

  expect(fetchFn).toHaveBeenCalledWith(
    "http://127.0.0.1:52350/api/peers/peer-1/transfers/browser-upload",
    expect.objectContaining({
      method: "POST",
      body: expect.any(FormData),
    }),
  );
});
```

- [x] **Step 2: 运行前端测试，确认它们先失败**

Run: `npm test -- --run src/lib/api.test.ts src/lib/localhost-api.test.ts`

Expected: FAIL，错误点集中在 `createLocalhostApiClient` 缺失、`createDefaultLocalApi()` 仍然只支持桌面 bindings，以及 `sendFile()` 没有浏览器上传路径。

- [x] **Step 3: 实现前端宿主自动选择和 localhost API 客户端**

```ts
// frontend/src/lib/api.ts
import { createDesktopApiClient, hasDesktopApiBindings } from "./desktop-api";
import { createLocalhostApiClient } from "./localhost-api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageHistoryPage,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./types";

export interface EventSubscription {
  close: () => void;
  reconnect: () => void;
}

export interface LocalApi {
  bootstrap: () => Promise<BootstrapSnapshot>;
  startPairing: (peerDeviceId: string) => Promise<PairingSnapshot>;
  confirmPairing: (pairingId: string) => Promise<PairingSnapshot>;
  sendText: (peerDeviceId: string, body: string) => Promise<MessageSnapshot>;
  sendFile: (peerDeviceId: string) => Promise<TransferSnapshot>;
  pickLocalFile: () => Promise<LocalFileSnapshot>;
  sendAcceleratedFile: (peerDeviceId: string, localFileId: string) => Promise<TransferSnapshot>;
  listMessageHistory: (conversationId: string, beforeCursor?: string) => Promise<MessageHistoryPage>;
  subscribeEvents: (options: {
    lastEventSeq?: number;
    onEvent: (event: AgentEvent) => void;
  }) => EventSubscription;
}

export function createDefaultLocalApi(): LocalApi {
  if (hasDesktopApiBindings()) {
    return createDesktopApiClient();
  }
  if (isLoopbackBrowser()) {
    return createLocalhostApiClient({ origin: window.location.origin });
  }
  throw new Error("local api bindings not available");
}

function isLoopbackBrowser(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1" || window.location.hostname === "[::1]";
}
```

```ts
// frontend/src/lib/localhost-api.ts
import type { LocalApi } from "./api";
import type {
  AgentEvent,
  BootstrapSnapshot,
  LocalFileSnapshot,
  MessageHistoryPage,
  MessageSnapshot,
  PairingSnapshot,
  TransferSnapshot,
} from "./types";

type LocalhostApiDependencies = {
  origin?: string;
  fetchFn?: typeof fetch;
  createEventSource?: (url: string) => EventSource;
  pickFile?: () => Promise<File>;
};

export function createLocalhostApiClient(dependencies: LocalhostApiDependencies = {}): LocalApi {
  const origin = dependencies.origin ?? window.location.origin;
  const fetchFn = dependencies.fetchFn ?? fetch;
  const createEventSource = dependencies.createEventSource ?? ((url) => new EventSource(url));
  const pickFile = dependencies.pickFile ?? pickBrowserFile;

  return {
    bootstrap: () => requestJson<BootstrapSnapshot>(fetchFn, origin, "/api/bootstrap"),
    startPairing: (peerDeviceId) => requestJson<PairingSnapshot>(fetchFn, origin, "/api/pairings", {
      method: "POST",
      body: JSON.stringify({ peerDeviceId }),
    }),
    confirmPairing: (pairingId) =>
      requestJson<PairingSnapshot>(fetchFn, origin, `/api/pairings/${pairingId}/confirm`, { method: "POST" }),
    sendText: (peerDeviceId, body) =>
      requestJson<MessageSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/messages/text`, {
        method: "POST",
        body: JSON.stringify({ body }),
      }),
    async sendFile(peerDeviceId) {
      const file = await pickFile();
      const form = new FormData();
      form.set("fileSize", String(file.size));
      form.set("file", file, file.name);
      return requestForm<TransferSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/transfers/browser-upload`, form);
    },
    pickLocalFile: () => requestJson<LocalFileSnapshot>(fetchFn, origin, "/api/local-files/pick", { method: "POST" }),
    sendAcceleratedFile: (peerDeviceId, localFileId) =>
      requestJson<TransferSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/transfers/accelerated`, {
        method: "POST",
        body: JSON.stringify({ localFileId }),
      }),
    listMessageHistory: (conversationId, beforeCursor) =>
      requestJson<MessageHistoryPage>(
        fetchFn,
        origin,
        beforeCursor
          ? `/api/conversations/${conversationId}/messages?beforeCursor=${encodeURIComponent(beforeCursor)}`
          : `/api/conversations/${conversationId}/messages`,
      ),
    subscribeEvents({ lastEventSeq = 0, onEvent }) {
      let cursor = lastEventSeq;
      let source = open(cursor);
      let closed = false;

      function open(afterSeq: number) {
        const url = new URL("/api/events/stream", origin);
        url.searchParams.set("afterSeq", String(afterSeq));
        const next = createEventSource(url.toString());
        next.onmessage = (event) => {
          const payload = JSON.parse(event.data) as AgentEvent;
          if (payload.eventSeq <= cursor) {
            return;
          }
          cursor = payload.eventSeq;
          onEvent(payload);
        };
        return next;
      }

      return {
        close() {
          closed = true;
          source.close();
        },
        reconnect() {
          if (closed) {
            return;
          }
          source.close();
          source = open(cursor);
        },
      };
    },
  };
}

async function requestJson<T>(
  fetchFn: typeof fetch,
  origin: string,
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const response = await fetchFn(`${origin}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init.headers ?? {}),
    },
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as T;
}

async function requestForm<T>(fetchFn: typeof fetch, origin: string, path: string, body: FormData): Promise<T> {
  const response = await fetchFn(`${origin}${path}`, { method: "POST", body });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return (await response.json()) as T;
}

async function pickBrowserFile(): Promise<File> {
  const input = document.createElement("input");
  input.type = "file";
  input.multiple = false;

  return new Promise<File>((resolve, reject) => {
    input.addEventListener("change", () => {
      const file = input.files?.[0];
      if (!file) {
        reject(new Error("file selection cancelled"));
        return;
      }
      resolve(file);
    });
    input.click();
  });
}
```

```tsx
// frontend/src/AppShell.tsx
// 仅替换启动和错误文案，避免 localhost 模式仍显示“桌面运行时”。
<section className="ms-splash ms-splash--error">
  <span className="ms-eyebrow">Connection Error</span>
  <h1 className="ms-splash__title">无法连接本机服务</h1>
  <p className="ms-splash__body">{errorMessage}</p>
</section>

<section className="ms-splash">
  <span className="ms-eyebrow">LAN P2P Share</span>
  <h1 className="ms-splash__title">一页直传</h1>
  <p className="ms-splash__body">正在连接本机 Message Share 服务</p>
</section>
```

- [x] **Step 4: 重新运行前端测试，确认宿主适配通过**

Run: `npm test -- --run src/lib/api.test.ts src/lib/localhost-api.test.ts`

Expected: PASS，且 `createDefaultLocalApi` 能在 loopback 浏览器场景创建 localhost client，`sendFile()` 走 multipart 浏览器上传。

- [ ] **Step 5: 提交前端宿主适配**

```bash
git add frontend/src/lib/api.ts frontend/src/lib/api.test.ts frontend/src/lib/localhost-api.ts frontend/src/lib/localhost-api.test.ts frontend/src/AppShell.tsx
git commit -m "feat: add localhost frontend api client"
```

### Task 2: `message-share-agent` localhost 兼容壳基础能力

**Files:**
- Create: `backend/internal/frontendassets/select.go`
- Create: `backend/internal/frontendassets/select_test.go`
- Create: `backend/internal/localui/service.go`
- Create: `backend/internal/localui/server.go`
- Create: `backend/internal/localui/server_test.go`
- Create: `backend/internal/localui/events_test.go`
- Modify: `backend/main.go`
- Modify: `backend/main_test.go`
- Test: `backend/internal/frontendassets/select_test.go`
- Test: `backend/internal/localui/server_test.go`
- Test: `backend/internal/localui/events_test.go`

- [x] **Step 1: 先写 failing tests，锁定共享资源选择、loopback 限制和 SSE backlog/live 语义**

```go
// backend/internal/frontendassets/select_test.go
package frontendassets

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestSelectPrefersBuiltDist(t *testing.T) {
	assets, err := Select(fstest.MapFS{
		"frontend/index.html":      {Data: []byte("placeholder")},
		"frontend/dist/index.html": {Data: []byte("built")},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(index) != "built" {
		t.Fatalf("expected built assets, got %q", string(index))
	}
}
```

```go
// backend/internal/localui/server_test.go
package localui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appruntime "message-share/backend/internal/app"
	"message-share/backend/internal/api"
)

type fakeCommands struct{}

func (fakeCommands) Bootstrap() (appruntime.BootstrapSnapshot, error) {
	return appruntime.BootstrapSnapshot{
		LocalDeviceName: "office-pc",
		Peers:           []appruntime.PeerSnapshot{},
		Pairings:        []appruntime.PairingSnapshot{},
		Conversations:   []appruntime.ConversationSnapshot{},
		Messages:        []appruntime.MessageSnapshot{},
		Transfers:       []appruntime.TransferSnapshot{},
	}, nil
}
func (fakeCommands) StartPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}
func (fakeCommands) ConfirmPairing(context.Context, string) (appruntime.PairingSnapshot, error) {
	return appruntime.PairingSnapshot{}, nil
}
func (fakeCommands) SendTextMessage(context.Context, string, string) (appruntime.MessageSnapshot, error) {
	return appruntime.MessageSnapshot{}, nil
}
func (fakeCommands) SendFile(context.Context, string, string, int64, io.Reader) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}
func (fakeCommands) PickLocalFile(context.Context) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{}, nil
}
func (fakeCommands) SendAcceleratedFile(context.Context, string, string) (appruntime.TransferSnapshot, error) {
	return appruntime.TransferSnapshot{}, nil
}
func (fakeCommands) ListMessageHistory(context.Context, string, string) (appruntime.MessageHistoryPageSnapshot, error) {
	return appruntime.MessageHistoryPageSnapshot{}, nil
}

func TestBootstrapRejectsNonLoopback(t *testing.T) {
	service := NewService(func() RuntimeCommands { return fakeCommands{} }, api.NewEventBus())
	server := NewServer(ServiceDeps{Service: service})

	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	req.RemoteAddr = "192.168.1.10:45678"
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestBootstrapReturnsSnapshotAndEventSeq(t *testing.T) {
	bus := api.NewEventBus()
	bus.Publish("peer.updated", map[string]any{"deviceId": "peer-1"})

	service := NewService(func() RuntimeCommands { return fakeCommands{} }, bus)
	server := NewServer(ServiceDeps{Service: service})

	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	var payload struct {
		LocalDeviceName string `json:"localDeviceName"`
		EventSeq        int64  `json:"eventSeq"`
	}
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.LocalDeviceName != "office-pc" || payload.EventSeq != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}
```

```go
// backend/internal/localui/events_test.go
package localui

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"message-share/backend/internal/api"
)

func TestEventsStreamReplaysBacklogThenStreamsLiveEvents(t *testing.T) {
	bus := api.NewEventBus()
	bus.Publish("peer.updated", map[string]any{"deviceId": "peer-1"})
	bus.Publish("health.updated", map[string]any{"status": "ok"})

	service := NewService(func() RuntimeCommands { return fakeCommands{} }, bus)
	server := NewServer(ServiceDeps{Service: service})

	req := httptest.NewRequest(http.MethodGet, "/api/events/stream?afterSeq=1", nil)
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	bus.Publish("transfer.updated", map[string]any{"transferId": "tx-1"})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "\"eventSeq\":2") || !strings.Contains(body, "\"eventSeq\":3") {
		t.Fatalf("expected backlog and live events, got %q", body)
	}
}
```

- [x] **Step 2: 运行后端测试，确认基础兼容壳能力尚未存在**

Run: `go test ./internal/frontendassets ./internal/localui -count=1`

Expected: FAIL，原因应包括 `frontendassets.Select`、`localui.NewService`、`localui.NewServer` 等符号不存在。

- [x] **Step 3: 提取共享前端资源选择器，并让桌面入口切换到共享实现**

```go
// backend/internal/frontendassets/select.go
package frontendassets

import "io/fs"

func Select(assetFS fs.FS) (fs.FS, error) {
	if distAssets, err := fs.Sub(assetFS, "frontend/dist"); err == nil {
		if _, err := fs.Stat(distAssets, "index.html"); err == nil {
			return distAssets, nil
		}
	}
	placeholderAssets, err := fs.Sub(assetFS, "frontend")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(placeholderAssets, "index.html"); err != nil {
		return nil, err
	}
	return placeholderAssets, nil
}
```

```go
// backend/main.go
package main

import (
	"embed"
	"log"

	"message-share/backend/internal/frontendassets"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var embeddedFrontendAssets embed.FS

func main() {
	desktopApp, err := NewDesktopApp()
	if err != nil {
		log.Fatal(err)
	}
	frontendAssets, err := frontendassets.Select(embeddedFrontendAssets)
	if err != nil {
		log.Fatal(err)
	}

	if err := wails.Run(&options.App{
		Title:     "Message Share",
		Width:     1360,
		Height:    900,
		MinWidth:  1080,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		OnStartup:  desktopApp.Startup,
		OnShutdown: desktopApp.Shutdown,
		Bind: []any{
			desktopApp,
		},
	}); err != nil {
		log.Fatal(err)
	}
}
```

```go
// backend/main_test.go
package main

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"message-share/backend/internal/frontendassets"
)

func TestSelectFrontendAssetsPrefersBuiltDist(t *testing.T) {
	assets, err := frontendassets.Select(fstest.MapFS{
		"frontend/index.html":      {Data: []byte("placeholder")},
		"frontend/dist/index.html": {Data: []byte("built")},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(index) != "built" {
		t.Fatalf("expected built frontend assets, got %q", string(index))
	}
}
```

- [x] **Step 4: 实现 localhost 兼容壳共享门面和基础 HTTP/SSE server**

```go
// backend/internal/localui/service.go
package localui

import (
	"context"
	"io"

	appruntime "message-share/backend/internal/app"
	"message-share/backend/internal/api"
)

type RuntimeCommands interface {
	Bootstrap() (appruntime.BootstrapSnapshot, error)
	StartPairing(ctx context.Context, peerDeviceID string) (appruntime.PairingSnapshot, error)
	ConfirmPairing(ctx context.Context, pairingID string) (appruntime.PairingSnapshot, error)
	SendTextMessage(ctx context.Context, peerDeviceID string, body string) (appruntime.MessageSnapshot, error)
	SendFile(ctx context.Context, peerDeviceID string, fileName string, fileSize int64, content io.Reader) (appruntime.TransferSnapshot, error)
	PickLocalFile(ctx context.Context) (appruntime.LocalFileSnapshot, error)
	SendAcceleratedFile(ctx context.Context, peerDeviceID string, localFileID string) (appruntime.TransferSnapshot, error)
	ListMessageHistory(ctx context.Context, conversationID string, beforeCursor string) (appruntime.MessageHistoryPageSnapshot, error)
}

type eventLog interface {
	LastSeq() int64
	Subscribe(afterSeq int64) ([]api.Event, <-chan api.Event, func())
}

type Service struct {
	resolve func() RuntimeCommands
	events  eventLog
}

type BootstrapResponse struct {
	appruntime.BootstrapSnapshot
	EventSeq int64 `json:"eventSeq"`
}

func NewService(resolve func() RuntimeCommands, events eventLog) *Service {
	return &Service{resolve: resolve, events: events}
}

func (s *Service) Bootstrap() (BootstrapResponse, error) {
	snapshot, err := s.resolve().Bootstrap()
	if err != nil {
		return BootstrapResponse{}, err
	}
	return BootstrapResponse{BootstrapSnapshot: snapshot, EventSeq: s.events.LastSeq()}, nil
}
```

```go
// backend/internal/localui/server.go
package localui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

type ServiceDeps struct {
	Service *Service
}

type Server struct {
	service *Service
}

func NewServer(deps ServiceDeps) *Server {
	return &Server{service: deps.Service}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/bootstrap", s.loopbackOnly(http.HandlerFunc(s.handleBootstrap)))
	mux.Handle("/api/events/stream", s.loopbackOnly(http.HandlerFunc(s.handleEvents)))
	mux.Handle("/api/pairings", s.loopbackOnly(http.HandlerFunc(s.handlePairings)))
	mux.Handle("/api/pairings/", s.loopbackOnly(http.HandlerFunc(s.handlePairings)))
	mux.Handle("/api/peers/", s.loopbackOnly(http.HandlerFunc(s.handlePeerRoutes)))
	mux.Handle("/api/conversations/", s.loopbackOnly(http.HandlerFunc(s.handleConversationRoutes)))
	return mux
}

func (s *Server) loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, err := s.service.Bootstrap()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	afterSeq, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("afterSeq")), 10, 64)
	backlog, stream, unsubscribe := s.service.events.Subscribe(afterSeq)
	defer unsubscribe()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, event := range backlog {
		fmt.Fprintf(w, "id: %d\n", event.EventSeq)
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(event))
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-stream:
			fmt.Fprintf(w, "id: %d\n", event.EventSeq)
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(event))
			flusher.Flush()
		}
	}
}

func (s *Server) handlePairings(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/pairings":
		var request struct {
			PeerDeviceID string `json:"peerDeviceId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		snapshot, err := s.service.resolve().StartPairing(r.Context(), request.PeerDeviceID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/confirm"):
		pairingID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/pairings/"), "/confirm")
		snapshot, err := s.service.resolve().ConfirmPairing(r.Context(), pairingID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handlePeerRoutes(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/messages/text") {
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
	peerDeviceID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/peers/"), "/messages/text")
	message, err := s.service.resolve().SendTextMessage(r.Context(), peerDeviceID, request.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleConversationRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/messages") {
		http.NotFound(w, r)
		return
	}
	conversationID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/conversations/"), "/messages")
	beforeCursor := strings.TrimSpace(r.URL.Query().Get("beforeCursor"))
	page, err := s.service.resolve().ListMessageHistory(r.Context(), conversationID, beforeCursor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func mustJSON(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"kind":"encoding.error"}`
	}
	return string(data)
}
```

- [x] **Step 5: 重新运行后端基础测试，确认 loopback、资源选择和事件流通过**

Run: `go test ./internal/frontendassets ./internal/localui -count=1`

Expected: PASS，`frontendassets.Select`、`/api/bootstrap` 和 `/api/events/stream` 的最小闭环通过。

- [ ] **Step 6: 提交 localhost 兼容壳基础能力**

```bash
git add backend/internal/frontendassets/select.go backend/internal/frontendassets/select_test.go backend/internal/localui/service.go backend/internal/localui/server.go backend/internal/localui/server_test.go backend/internal/localui/events_test.go backend/main.go backend/main_test.go
git commit -m "feat: add localhost web shell foundation"
```

### Task 3: 极速文件发送委托、浏览器普通文件发送与 agent 启动链路

**Files:**
- Create: `backend/internal/localui/upload_test.go`
- Create: `backend/cmd/message-share-agent/assets_embed.go`
- Create: `backend/cmd/message-share-agent/frontend/index.html`
- Modify: `backend/internal/localui/server.go`
- Modify: `backend/cmd/message-share-agent/main.go`
- Modify: `backend/cmd/message-share-agent/main_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `frontend/src/lib/localhost-api.ts`
- Modify: `frontend/src/lib/localhost-api.test.ts`
- Test: `backend/internal/localui/upload_test.go`
- Test: `backend/cmd/message-share-agent/main_test.go`
- Test: `backend/internal/config/config_test.go`

- [x] **Step 1: 先写 failing tests，锁定浏览器上传流式发送、`/api/local-files/pick`、agent URL 输出和本地 HTTP 端口配置**

```go
// backend/internal/localui/upload_test.go
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

	appruntime "message-share/backend/internal/app"
	"message-share/backend/internal/api"
)

type uploadCommands struct{}

func (uploadCommands) Bootstrap() (appruntime.BootstrapSnapshot, error) { return appruntime.BootstrapSnapshot{}, nil }
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

func TestPickLocalFileReturnsUnsupportedErrorVerbatim(t *testing.T) {
	service := NewService(func() RuntimeCommands {
		return unsupportedPickCommands{}
	}, api.NewEventBus())
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

type unsupportedPickCommands struct{ uploadCommands }

func (unsupportedPickCommands) PickLocalFile(context.Context) (appruntime.LocalFileSnapshot, error) {
	return appruntime.LocalFileSnapshot{}, errors.New("local file picker unsupported on linux")
}
```

```go
// backend/internal/config/config_test.go
func TestDefaultConfigUsesDedicatedLocalHTTPPort(t *testing.T) {
	t.Setenv("MESSAGE_SHARE_DATA_DIR", t.TempDir())
	cfg := Default()
	if cfg.LocalHTTPPort != 52350 {
		t.Fatalf("expected default local http port 52350, got %d", cfg.LocalHTTPPort)
	}
}

func TestDefaultConfigHonorsLocalHTTPPortOverride(t *testing.T) {
	t.Setenv("MESSAGE_SHARE_DATA_DIR", t.TempDir())
	t.Setenv("MESSAGE_SHARE_LOCAL_HTTP_PORT", "52351")
	cfg := Default()
	if cfg.LocalHTTPPort != 52351 {
		t.Fatalf("expected override local http port 52351, got %d", cfg.LocalHTTPPort)
	}
}
```

```go
// backend/cmd/message-share-agent/main_test.go
func TestRunLogsLocalhostWebUIAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtimeHost := &stubRuntimeHost{errs: make(chan error)}
	localUIHost := &stubLocalUIHost{url: "http://127.0.0.1:52350/"}
	var output bytes.Buffer
	logger := log.New(&output, "", 0)

	done := make(chan error, 1)
	go func() {
		done <- run(
			ctx,
			logger,
			func() (config.AppConfig, error) {
				return config.AppConfig{
					DeviceName:          "office-pc",
					DataDir:             "C:/message-share",
					AgentTCPPort:        19090,
					DiscoveryUDPPort:    19091,
					AcceleratedDataPort: 19092,
					LocalHTTPPort:       52350,
				}, nil
			},
			func(config.AppConfig, *log.Logger, *api.EventBus) runtimeHost { return runtimeHost },
			func(config.AppConfig, *log.Logger, runtimeHost, *api.EventBus) localUIHost { return localUIHost },
		)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(output.String(), "http://127.0.0.1:52350/") {
		t.Fatalf("expected localhost url in output, got %q", output.String())
	}
}

type stubLocalUIHost struct {
	url string
}

func (s *stubLocalUIHost) Start(context.Context) error { return nil }
func (s *stubLocalUIHost) Close(context.Context) error { return nil }
func (s *stubLocalUIHost) URL() string                 { return s.url }

func (s *stubRuntimeHost) RuntimeService() *appruntime.RuntimeService {
	return nil
}
```

- [x] **Step 2: 运行 Go 测试，确认上传、picker 和 agent 启动链路都还未接上**

Run: `go test ./internal/localui ./internal/config ./cmd/message-share-agent -count=1`

Expected: FAIL，原因应包括 `LocalHTTPPort` 缺失、upload route 未实现、`run()` 未接收 localhost host 工厂以及 `/api/local-files/pick` 尚不存在。

- [x] **Step 3: 实现浏览器上传、`/api/local-files/pick`、`/api/peers/{id}/transfers/accelerated` 和本地 HTTP 端口配置**

```go
// backend/internal/config/config.go
type AppConfig struct {
	AgentTCPPort           int
	LocalHTTPPort          int
	AcceleratedDataPort    int
	AcceleratedEnabled     bool
	DiscoveryUDPPort       int
	DiscoveryListenAddr    string
	DiscoveryBroadcastAddr string
	DataDir                string
	DatabasePath           string
	LogDir                 string
	TempDir                string
	DeviceName             string
	IdentityFilePath       string
	DefaultDownloadDir     string
	MaxAutoAcceptFileMB    int64
}

cfg := AppConfig{
	AgentTCPPort:        19090,
	LocalHTTPPort:       52350,
	AcceleratedDataPort: 19092,
	AcceleratedEnabled:  true,
	DiscoveryUDPPort:    19091,
	DataDir:             layout.RootDir,
	DatabasePath:        layout.DatabasePath,
	LogDir:              layout.LogDir,
	TempDir:             layout.TempDir,
	DeviceName:          settings.DeviceName,
	IdentityFilePath:    layout.IdentityFilePath,
	MaxAutoAcceptFileMB: settings.MaxAutoAcceptFileMB,
}
if value, ok := lookupEnvInt("MESSAGE_SHARE_LOCAL_HTTP_PORT"); ok {
	cfg.LocalHTTPPort = value
}
```

```go
// backend/internal/localui/server.go
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/bootstrap", s.loopbackOnly(http.HandlerFunc(s.handleBootstrap)))
	mux.Handle("/api/events/stream", s.loopbackOnly(http.HandlerFunc(s.handleEvents)))
	mux.Handle("/api/local-files/pick", s.loopbackOnly(http.HandlerFunc(s.handlePickLocalFile)))
	mux.Handle("/api/peers/", s.loopbackOnly(http.HandlerFunc(s.handlePeerRoutes)))
	return mux
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

func (s *Server) handleBrowserUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	peerDeviceID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/peers/"), "/transfers/browser-upload")
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var (
		fileName string
		fileSize int64
		filePart io.Reader
	)
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
			raw, _ := io.ReadAll(part)
			fileSize, err = strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
			if err != nil {
				http.Error(w, "invalid fileSize", http.StatusBadRequest)
				return
			}
		case "file":
			fileName = part.FileName()
			filePart = part
		}
	}
	if filePart == nil || fileName == "" {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	transfer, err := s.service.resolve().SendFile(r.Context(), peerDeviceID, fileName, fileSize, filePart)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, transfer)
}

func (s *Server) handlePickLocalFile(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.service.resolve().PickLocalFile(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleAccelerated(w http.ResponseWriter, r *http.Request) {
	peerDeviceID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/peers/"), "/transfers/accelerated")
	var request struct {
		LocalFileID string `json:"localFileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	transfer, err := s.service.resolve().SendAcceleratedFile(r.Context(), peerDeviceID, request.LocalFileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, transfer)
}
```

```ts
// frontend/src/lib/localhost-api.ts
async sendFile(peerDeviceId) {
  const file = await pickFile();
  const form = new FormData();
  form.set("fileSize", String(file.size));
  form.set("file", file, file.name);
  return requestForm<TransferSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/transfers/browser-upload`, form);
},
pickLocalFile: () => requestJson<LocalFileSnapshot>(fetchFn, origin, "/api/local-files/pick", { method: "POST" }),
sendAcceleratedFile: (peerDeviceId, localFileId) =>
  requestJson<TransferSnapshot>(fetchFn, origin, `/api/peers/${peerDeviceId}/transfers/accelerated`, {
    method: "POST",
    body: JSON.stringify({ localFileId }),
  }),
```

- [x] **Step 4: 让 `message-share-agent` 启动 runtime host + localhost Web UI，并嵌入兼容前端资源**

```go
// backend/cmd/message-share-agent/assets_embed.go
package main

import "embed"

//go:embed all:frontend
var embeddedFrontendAssets embed.FS
```

```html
<!-- backend/cmd/message-share-agent/frontend/index.html -->
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <title>Message Share Agent</title>
  </head>
  <body>
    <div id="root">Message Share agent assets are not built yet.</div>
  </body>
</html>
```

```go
// backend/cmd/message-share-agent/main.go
type runtimeHost interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	Errors() <-chan error
	RuntimeService() *appruntime.RuntimeService
}

type localUIHost interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	URL() string
}

type hostFactory func(cfg config.AppConfig, logger *log.Logger, events *api.EventBus) runtimeHost
type localUIFactory func(cfg config.AppConfig, logger *log.Logger, runtime runtimeHost, events *api.EventBus) localUIHost

func run(
	ctx context.Context,
	logger *log.Logger,
	loadConfig configLoader,
	makeRuntimeHost hostFactory,
	makeLocalUIHost localUIFactory,
) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load default config: %w", err)
	}

	events := api.NewEventBus()
	runtime := makeRuntimeHost(cfg, logger, events)
	if err := runtime.Start(ctx); err != nil {
		return fmt.Errorf("start runtime host: %w", err)
	}
	defer shutdownHost(runtime, logger)

	localUI := makeLocalUIHost(cfg, logger, runtime, events)
	if err := localUI.Start(ctx); err != nil {
		return fmt.Errorf("start localhost web ui: %w", err)
	}
	defer shutdownLocalUI(localUI, logger)

	logger.Printf(
		"message-share-agent running: device=%s dataDir=%s tcp=%d discovery=%d accelerated=%d web=%s",
		cfg.DeviceName,
		cfg.DataDir,
		cfg.AgentTCPPort,
		cfg.DiscoveryUDPPort,
		cfg.AcceleratedDataPort,
		localUI.URL(),
	)

	select {
	case <-ctx.Done():
		return nil
	case err := <-runtime.Errors():
		if err == nil {
			return nil
		}
		return fmt.Errorf("runtime host async error: %w", err)
	}
}

func shutdownLocalUI(host localUIHost, logger *log.Logger) {
	if host == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := host.Close(shutdownCtx); err != nil && logger != nil {
		logger.Printf("shutdown localhost web ui: %v", err)
	}
}
```

- [x] **Step 5: 重新运行 Go 测试，确认上传、picker、端口配置和 agent 启动链路通过**

Run: `go test ./internal/localui ./internal/config ./cmd/message-share-agent -count=1`

Expected: PASS，`message-share-agent` 日志包含 localhost 访问地址，上传路径走流式 multipart，`/api/local-files/pick` 成功返回 `localFileId` 或明确 unsupported。

- [ ] **Step 6: 提交 agent 兼容入口和文件发送能力**

```bash
git add backend/internal/localui/server.go backend/internal/localui/upload_test.go backend/internal/config/config.go backend/internal/config/config_test.go backend/cmd/message-share-agent/assets_embed.go backend/cmd/message-share-agent/frontend/index.html backend/cmd/message-share-agent/main.go backend/cmd/message-share-agent/main_test.go frontend/src/lib/localhost-api.ts frontend/src/lib/localhost-api.test.ts
git commit -m "feat: restore localhost web ui agent shell"
```

### Task 4: 验证、构建脚本与文档

**Files:**
- Create: `scripts/build-agent.ps1`
- Create: `scripts/build-agent.sh`
- Create: `scripts/smoke-agent.ps1`
- Create: `scripts/smoke-agent.sh`
- Create: `docs/testing/agent-localhost-runtime.md`
- Modify: `scripts/test.ps1`
- Modify: `README.md`

- [x] **Step 1: 添加 agent 构建脚本，构建前同步浏览器资源到 command 目录**

```powershell
# scripts/build-agent.ps1
param(
    [string]$Output = "dist\\message-share-agent.exe"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$scriptDir = Split-Path -Parent $PSCommandPath
$repoRoot = Split-Path -Parent $scriptDir
$backendDir = Join-Path $repoRoot "backend"
$sourceAssets = Join-Path $backendDir "frontend"
$targetAssets = Join-Path $backendDir "cmd\\message-share-agent\\frontend"

Remove-Item -Recurse -Force $targetAssets -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $targetAssets | Out-Null
Copy-Item -Recurse -Force (Join-Path $sourceAssets "*") $targetAssets

Push-Location $backendDir
try {
    go build -o (Join-Path $repoRoot $Output) ./cmd/message-share-agent
}
finally {
    Pop-Location
}
```

```bash
# scripts/build-agent.sh
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
SOURCE_ASSETS="${BACKEND_DIR}/frontend"
TARGET_ASSETS="${BACKEND_DIR}/cmd/message-share-agent/frontend"
OUTPUT="${1:-${REPO_ROOT}/dist/message-share-agent}"

rm -rf "${TARGET_ASSETS}"
mkdir -p "${TARGET_ASSETS}"
cp -R "${SOURCE_ASSETS}/." "${TARGET_ASSETS}/"

cd "${BACKEND_DIR}"
go build -o "${OUTPUT}" ./cmd/message-share-agent
```

- [x] **Step 2: 添加 agent smoke 脚本，并把总测试脚本扩展到兼容入口**

```powershell
# scripts/smoke-agent.ps1
param(
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$scriptDir = Split-Path -Parent $PSCommandPath
$repoRoot = Split-Path -Parent $scriptDir
$binaryPath = Join-Path $repoRoot "dist\\message-share-agent.exe"

if (-not $SkipBuild) {
    & "$repoRoot\scripts\build-agent.ps1"
}

$process = Start-Process -FilePath $binaryPath -PassThru -WindowStyle Hidden
try {
    Start-Sleep -Seconds 3
    Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:52350/api/bootstrap" | Out-Null
}
finally {
    if ($process -and -not $process.HasExited) {
        Stop-Process -Id $process.Id -Force
    }
}
```

```powershell
# scripts/test.ps1
# 在现有桌面测试完成后追加：
& "$repoRoot\scripts\build-agent.ps1"
if ($LASTEXITCODE -ne 0) {
    throw ("Agent build failed with exit code {0}" -f $LASTEXITCODE)
}

& "$repoRoot\scripts\smoke-agent.ps1" -SkipBuild
if ($LASTEXITCODE -ne 0) {
    throw ("Agent smoke failed with exit code {0}" -f $LASTEXITCODE)
}
```

- [x] **Step 3: 更新 README 和测试文档，写清 localhost 兼容入口的运行和验证方式**

```md
<!-- README.md 新增片段 -->
## Headless Agent + Localhost Web UI

`backend/cmd/message-share-agent` 会构建出无窗口 agent，并在本机 `http://127.0.0.1:52350/` 提供浏览器兼容入口。

### 构建

```powershell
.\scripts\build-agent.ps1
```

### 验证

```powershell
.\scripts\smoke-agent.ps1
```

兼容入口只允许本机浏览器访问，不对局域网其他机器开放。
```

```md
<!-- docs/testing/agent-localhost-runtime.md -->
# Agent Localhost Runtime 验证

1. 运行 `.\scripts\build-agent.ps1`
2. 运行 `.\scripts\smoke-agent.ps1`
3. 手动打开 `http://127.0.0.1:52350/`
4. 检查设备列表、配对、文本发送、普通文件发送和极速发送入口
5. 确认桌面版仍可通过 `.\scripts\smoke-desktop.ps1 -SkipBuild` 通过
```

- [x] **Step 4: 跑完整验证，确认桌面版不回归且 agent 兼容入口闭环**

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`

Expected: PASS，包含 Go tests、前端 Vitest、桌面构建 + smoke、agent 构建 + smoke 全部通过。

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1`

Expected: 生成 `dist\message-share-agent.exe`，启动后控制台打印 `http://127.0.0.1:52350/`。

- [ ] **Step 5: 提交验证脚本和文档**

```bash
git add scripts/build-agent.ps1 scripts/build-agent.sh scripts/smoke-agent.ps1 scripts/smoke-agent.sh scripts/test.ps1 docs/testing/agent-localhost-runtime.md README.md
git commit -m "docs: add agent localhost runtime verification"
```
