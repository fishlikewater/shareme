# Wails 桌面化与跨平台运行时升级 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把当前 “Go agent + localhost Web UI” 升级为基于 Wails 的桌面应用，默认使用用户主目录 `.message-share` 作为配置与运行目录，并完成 Windows、macOS、Linux 的最小可交付闭环。

**Architecture:** 保留现有局域网发现、配对、TLS 对等通信、消息与文件传输核心，只把本地 UI 宿主从 loopback HTTP 改成 Wails 桌面壳。`backend` 目录成为 Wails Go 宿主根，`frontend` 保持现有 React/Vite 工程，通过 Wails 配置显式引用；本地命令与事件由桌面 bridge 直接调用/订阅运行时核心，不再经过 localhost API。

**Tech Stack:** Go 1.25、Wails v2、React 18、TypeScript、Vite、Vitest、SQLite、PowerShell、POSIX shell

---

## Source of Truth

- OpenSpec proposal: `openspec/changes/upgrade-to-wails-cross-platform-runtime/proposal.md`
- OpenSpec design: `openspec/changes/upgrade-to-wails-cross-platform-runtime/design.md`
- OpenSpec tasks: `openspec/changes/upgrade-to-wails-cross-platform-runtime/tasks.md`

## 实施约束

- 以 `openspec/changes/upgrade-to-wails-cross-platform-runtime/tasks.md` 为任务真源，顺序保持一致，完成即回写 checkbox。
- 不再保留 localhost Web UI 作为正式入口；测试辅助可以存在，但不能成为生产路径。
- 不引入第二套消息/传输状态模型；前端继续消费当前 `BootstrapSnapshot` / `AgentEvent` 语义。
- 用户可编辑配置以 `.message-share/config.json` 为准；设备身份材料继续保存在 `.message-share/local-device.json`。
- 仅保留必要文件，避免提交 Wails 自动生成且可再生的冗余产物。

## 文件布局与职责

### 新增文件

- `backend/main.go`
  - Wails 桌面主入口，负责运行桌面应用。
- `backend/app.go`
  - 组装桌面宿主、bridge、生命周期回调。
- `backend/wails.json`
  - Wails 项目配置，显式指定 `../frontend` 为前端目录，指定 `../frontend/wailsjs` 为生成绑定目录。
- `backend/internal/runtime/host.go`
  - 提取当前 agent 初始化逻辑，统一负责启动/关闭发现、存储、对等监听、传输监听与事件发布。
- `backend/internal/runtime/host_test.go`
  - 运行时生命周期与依赖装配测试。
- `backend/internal/config/layout.go`
  - 统一解析 `.message-share` 根目录及子路径。
- `backend/internal/config/settings.go`
  - 读写 `.message-share/config.json`，合并默认值与用户配置。
- `backend/internal/config/settings_test.go`
  - 配置生成、字段保留、设备名称优先级测试。
- `backend/internal/config/migration.go`
  - 旧目录探测、一次性迁移与迁移标记。
- `backend/internal/config/migration_test.go`
  - 迁移覆盖与新旧目录并存测试。
- `backend/internal/desktop/bridge.go`
  - 暴露给前端的桌面命令，桥接 `RuntimeService`。
- `backend/internal/desktop/bridge_test.go`
  - bridge 命令、原生对话框、错误传播测试。
- `backend/internal/desktop/events.go`
  - 把后端事件发布到 Wails runtime event 通道。
- `backend/internal/desktop/events_test.go`
  - 事件映射与事件名一致性测试。
- `frontend/src/lib/desktop-api.ts`
  - 基于 Wails 生成绑定的桌面客户端实现。
- `frontend/src/lib/desktop-api.test.ts`
  - 桌面客户端命令与事件订阅测试。
- `scripts/dev-desktop.ps1`
  - Windows 桌面开发入口。
- `scripts/build-desktop.ps1`
  - Windows 桌面构建入口。
- `scripts/smoke-desktop.ps1`
  - Windows 桌面 smoke 入口。
- `scripts/dev-desktop.sh`
  - macOS/Linux 桌面开发入口。
- `scripts/build-desktop.sh`
  - macOS/Linux 桌面构建入口。
- `scripts/smoke-desktop.sh`
  - macOS/Linux 桌面 smoke 入口。
- `docs/testing/wails-desktop-runtime.md`
  - 桌面运行、构建、目录布局与 smoke 验证记录。

### 重点修改文件

- `backend/cmd/message-share-agent/main.go`
  - 切到共享运行时初始化逻辑，只保留纯 agent/诊断入口。
- `backend/internal/config/config.go`
  - 基于 `.message-share` 布局与 `config.json` 输出运行时配置。
- `backend/internal/config/download_dir.go`
  - 下载目录回退到 `.message-share/downloads`。
- `backend/internal/device/identity.go`
  - 让配置文件中的设备名称能够同步到身份文件，而不是只在缺失时回填。
- `backend/internal/localfile/manager.go`
  - 新增基于真实路径注册 lease 的能力，不再强依赖内部 picker。
- `backend/internal/localfile/manager_test.go`
  - 覆盖 `RegisterPath`、过期 lease、文件变更检测。
- `backend/internal/app/local_file_service.go`
  - 使用新的 `RegisterPath` 能力输出 `LocalFileSnapshot`。
- `backend/internal/app/service.go`
  - 增加桌面普通文件发送辅助入口，并复用现有消息/传输状态模型。
- `frontend/src/lib/api.ts`
  - 退化为 HTTP 测试/兼容客户端；默认导出逻辑改为优先桌面客户端。
- `frontend/src/lib/types.ts`
  - 调整 `LocalApi` 调用形态相关类型。
- `frontend/src/AppShell.tsx`
  - 切到桌面默认客户端，移除对浏览器文件上传的依赖。
- `frontend/src/components/ChatPane.tsx`
  - 普通文件发送按钮直接触发桌面命令，不再使用 `<input type="file">`。
- `frontend/src/AppShell.default-api.test.tsx`
  - 调整默认客户端选择逻辑测试。
- `frontend/vite.config.ts`
  - 调整构建输出以适配桌面壳静态资源加载。
- `scripts/test.ps1`
  - 更新验证路径，面向桌面化版本执行单元测试与必要 smoke。

### 计划删除的文件

- `backend/internal/webui/assets.go`
- `backend/internal/webui/dist/.keep`
- `backend/internal/api/http_server.go`
- `backend/internal/api/http_server_test.go`
- `backend/internal/api/http_local_files_test.go`
- `backend/internal/api/http_accelerated_transfers_test.go`
- `scripts/dev-web.ps1`
- `scripts/build-agent.ps1`
- `scripts/smoke-agent.ps1`

### 不手工编辑的生成文件

- `frontend/wailsjs/**`
  - 由 Wails 生成的前端绑定，仅在构建/生成阶段刷新，不手工编辑。

## Task 1: Wails 桌面宿主骨架与共享运行时

**对应 OpenSpec：** 1.1、1.2、1.3、1.4

**Files:**
- Create: `backend/main.go`
- Create: `backend/app.go`
- Create: `backend/wails.json`
- Create: `backend/internal/runtime/host.go`
- Test: `backend/internal/runtime/host_test.go`
- Modify: `backend/cmd/message-share-agent/main.go`
- Modify: `backend/go.mod`

- [x] **Step 1: 先写共享运行时生命周期测试**

```go
package runtime

import (
	"context"
	"testing"
	"time"
)

func TestHostStartAndClose(t *testing.T) {
	host := NewHost(Options{
		Config: newTestConfig(t),
		Now:    func() time.Time { return time.Unix(1713398400, 0).UTC() },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := host.Start(ctx); err != nil {
		t.Fatalf("start host: %v", err)
	}
	if host.RuntimeService() == nil {
		t.Fatal("expected runtime service to be initialized")
	}
	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("close host: %v", err)
	}
}
```

- [x] **Step 2: 跑测试确认当前缺少 `runtime.Host`**

Run:

```powershell
Set-Location backend
go test ./internal/runtime
```

Expected:

```text
FAIL: package message-share/backend/internal/runtime is not found
```

- [x] **Step 3: 实现共享运行时宿主，提取当前 agent 初始化逻辑**

```go
package runtime

type Host struct {
	cfg            config.AppConfig
	store          *store.SQLiteStore
	runtimeService *app.RuntimeService
	eventBus       *api.EventBus
	discovery      *discovery.Runner
	peerServer     *http.Server
	accelerated    *transfer.AcceleratedListener
}

func NewHost(opts Options) *Host {
	return &Host{cfg: opts.Config}
}

func (h *Host) Start(ctx context.Context) error {
	db, err := store.Open(filepath.Join(h.cfg.DataDir, "message-share.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	h.store = db

	localDevice, err := device.EnsureLocalDevice(h.cfg.IdentityFilePath, h.cfg.DeviceName)
	if err != nil {
		return fmt.Errorf("ensure local device: %w", err)
	}

	h.eventBus = api.NewEventBus()
	h.runtimeService = app.NewRuntimeService(app.RuntimeDeps{
		Config: h.cfg,
		Store:  h.store,
		Events: app.EventPublisherFunc(func(kind string, payload any) {
			h.eventBus.Publish(kind, payload)
		}),
	})
	return h.startNetworkSurfaces(ctx, localDevice)
}

func (h *Host) Close(ctx context.Context) error {
	var errs []error
	if h.peerServer != nil {
		errs = append(errs, h.peerServer.Shutdown(ctx))
	}
	if h.accelerated != nil {
		errs = append(errs, h.accelerated.Close())
	}
	if h.discovery != nil {
		errs = append(errs, h.discovery.Close())
	}
	if h.store != nil {
		errs = append(errs, h.store.Close())
	}
	return errors.Join(errs...)
}

func (h *Host) RuntimeService() *app.RuntimeService {
	return h.runtimeService
}
```

- [x] **Step 4: 增加 Wails 主入口与桌面应用装配**

```go
package main

func main() {
	app := NewDesktopApp()
	if err := wails.Run(&options.App{
		Title:     "Message Share",
		Width:     1360,
		Height:    900,
		MinWidth:  1080,
		MinHeight: 720,
		OnStartup: app.Startup,
		OnShutdown: func(ctx context.Context) {
			app.Shutdown(ctx)
		},
		Bind: []any{app},
	}); err != nil {
		log.Fatal(err)
	}
}
```

```go
package main

type DesktopApp struct {
	host   *runtime.Host
	bridge *desktop.Bridge
}

func NewDesktopApp() *DesktopApp {
	cfg := config.Default()
	host := runtime.NewHost(runtime.Options{Config: cfg})
	bridge := desktop.NewBridge(host)
	return &DesktopApp{host: host, bridge: bridge}
}

func (a *DesktopApp) Startup(ctx context.Context) error {
	return a.host.Start(ctx)
}

func (a *DesktopApp) Shutdown(ctx context.Context) {
	_ = a.host.Close(ctx)
}

func (a *DesktopApp) Bootstrap() (app.BootstrapSnapshot, error) {
	return a.bridge.Bootstrap()
}

func (a *DesktopApp) SendFile(peerDeviceID string) (app.TransferSnapshot, error) {
	return a.bridge.SendFile(peerDeviceID)
}
```

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "message-share",
  "outputfilename": "message-share",
  "frontend:dir": "../frontend",
  "frontend:install": "npm ci",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  "wailsjsdir": "../frontend/wailsjs"
}
```

- [x] **Step 5: 让旧 agent 入口复用新宿主，而不是复制初始化逻辑**

```go
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	host := runtime.NewHost(runtime.Options{Config: config.Default()})
	if err := host.Start(ctx); err != nil {
		log.Fatalf("start runtime host: %v", err)
	}
	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := host.Close(shutdownCtx); err != nil {
		log.Printf("shutdown runtime host: %v", err)
	}
}
```

- [x] **Step 6: 运行后端基础测试并提交**

Run:

```powershell
Set-Location backend
go test ./internal/runtime ./cmd/message-share-agent ./internal/app ./internal/discovery
```

Expected:

```text
PASS
```

Commit:

```bash
git add backend/main.go backend/app.go backend/wails.json backend/internal/runtime/host.go backend/internal/runtime/host_test.go backend/cmd/message-share-agent/main.go backend/go.mod backend/go.sum
git commit -m "feat: add wails desktop host scaffold"
```

## Task 2: `.message-share` 根目录、用户配置与设备名称同步

**对应 OpenSpec：** 2.1、2.2、2.3

**Files:**
- Create: `backend/internal/config/layout.go`
- Create: `backend/internal/config/settings.go`
- Create: `backend/internal/config/settings_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/download_dir.go`
- Modify: `backend/internal/device/identity.go`
- Test: `backend/internal/config/config_test.go`
- Test: `backend/internal/device/identity_test.go`

- [x] **Step 1: 先补目录布局、配置保留与设备名称同步的失败测试**

```go
func TestLoadSettingsCreatesDefaultConfigWithoutOverwritingUserValues(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), ".message-share")

	first, err := LoadSettings(rootDir)
	if err != nil {
		t.Fatalf("load settings first time: %v", err)
	}
	if first.DeviceName == "" {
		t.Fatal("expected default device name")
	}

	configPath := filepath.Join(rootDir, "config.json")
	if err := os.WriteFile(configPath, []byte("{\n  \"deviceName\": \"客厅电脑\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	second, err := LoadSettings(rootDir)
	if err != nil {
		t.Fatalf("load settings second time: %v", err)
	}
	if second.DeviceName != "客厅电脑" {
		t.Fatalf("expected persisted device name, got %q", second.DeviceName)
	}
}
```

```go
func TestEnsureLocalDeviceUpdatesStoredNameWhenConfiguredNameChanges(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "local-device.json")
	if _, err := EnsureLocalDevice(identityPath, "初始设备"); err != nil {
		t.Fatalf("create device: %v", err)
	}
	device, err := EnsureLocalDevice(identityPath, "新的设备名")
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if device.DeviceName != "新的设备名" {
		t.Fatalf("expected updated device name, got %q", device.DeviceName)
	}
}
```

- [x] **Step 2: 跑测试确认当前缺少布局与配置文件能力**

Run:

```powershell
Set-Location backend
go test ./internal/config ./internal/device
```

Expected:

```text
FAIL with undefined: LoadSettings
FAIL with expected updated device name
```

- [x] **Step 3: 实现 `.message-share` 路径布局与 `config.json` 默认生成**

```go
package config

type Layout struct {
	RootDir          string
	ConfigFilePath   string
	IdentityFilePath string
	DatabasePath     string
	LogDir           string
	TempDir          string
	DownloadsDir     string
}

func ResolveLayout() (Layout, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	rootDir := filepath.Join(homeDir, ".message-share")
	return Layout{
		RootDir:          rootDir,
		ConfigFilePath:   filepath.Join(rootDir, "config.json"),
		IdentityFilePath: filepath.Join(rootDir, "local-device.json"),
		DatabasePath:     filepath.Join(rootDir, "message-share.db"),
		LogDir:           filepath.Join(rootDir, "logs"),
		TempDir:          filepath.Join(rootDir, "tmp"),
		DownloadsDir:     filepath.Join(rootDir, "downloads"),
	}, nil
}
```

```go
type Settings struct {
	DeviceName      string `json:"deviceName"`
	DownloadDir     string `json:"downloadDir,omitempty"`
	MaxAutoAcceptMB int64  `json:"maxAutoAcceptFileMB"`
}

func LoadSettings(rootDir string) (Settings, error) {
	defaults := Settings{
		DeviceName:      "本机设备",
		MaxAutoAcceptMB: 512,
	}
	configPath := filepath.Join(rootDir, "config.json")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return Settings{}, err
	}
	content, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, persistSettings(configPath, defaults)
	}
	if err != nil {
		return Settings{}, err
	}

	current := defaults
	if err := json.Unmarshal(content, &current); err != nil {
		return Settings{}, err
	}
	return current, persistSettings(configPath, current)
}
```

- [x] **Step 4: 把 `config.Default()` 与 `EnsureLocalDevice()` 切到新配置语义**

```go
func Default() AppConfig {
	layout, err := ResolveLayout()
	if err != nil {
		panic(err)
	}
	settings, err := LoadSettings(layout.RootDir)
	if err != nil {
		panic(err)
	}

	cfg := AppConfig{
		DataDir:              layout.RootDir,
		IdentityFilePath:     layout.IdentityFilePath,
		DefaultDownloadDir:   resolveDownloadDir(settings.DownloadDir, layout.DownloadsDir),
		DeviceName:           settings.DeviceName,
		MaxAutoAcceptFileMB:  settings.MaxAutoAcceptMB,
	}
	applyEnvOverrides(&cfg)
	return cfg
}

func resolveDownloadDir(configured string, fallback string) string {
	if dir := strings.TrimSpace(configured); dir != "" {
		if err := ensureDownloadDirUsable(dir); err == nil {
			return dir
		}
	}
	if systemDir, err := os.UserHomeDir(); err == nil {
		downloads := filepath.Join(systemDir, "Downloads")
		if ensureDownloadDirUsable(downloads) == nil {
			return downloads
		}
	}
	if err := ensureDownloadDirUsable(fallback); err == nil {
		return fallback
	}
	return fallback
}
```

```go
func EnsureLocalDevice(identityFilePath string, name string) (domain.LocalDevice, error) {
	if existing, err := readLocalDevice(identityFilePath); err == nil {
		if name != "" && existing.DeviceName != name {
			existing.DeviceName = name
			if err := persistLocalDevice(identityFilePath, existing); err != nil {
				return domain.LocalDevice{}, err
			}
		}
		return existing, nil
	}
	// 其余创建逻辑保持不变
}
```

- [x] **Step 5: 跑配置与身份测试并提交**

Run:

```powershell
Set-Location backend
go test ./internal/config ./internal/device
```

Expected:

```text
PASS
```

Commit:

```bash
git add backend/internal/config/layout.go backend/internal/config/settings.go backend/internal/config/settings_test.go backend/internal/config/config.go backend/internal/config/download_dir.go backend/internal/config/config_test.go backend/internal/device/identity.go backend/internal/device/identity_test.go
git commit -m "feat: add user home config layout"
```

## Task 3: 旧目录迁移与启动期路径切换

**对应 OpenSpec：** 2.4、2.5

**Files:**
- Create: `backend/internal/config/migration.go`
- Create: `backend/internal/config/migration_test.go`
- Modify: `backend/internal/runtime/host.go`
- Modify: `backend/internal/config/config.go`

- [x] **Step 1: 先补迁移行为测试**

```go
func TestMigrateLegacyDataCopiesIdentityAndDatabaseOnce(t *testing.T) {
	baseDir := t.TempDir()
	legacyDir := filepath.Join(baseDir, "AppData", "Roaming", "MessageShare")
	newDir := filepath.Join(baseDir, ".message-share")

	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "local-device.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "message-share.db"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	if err := MigrateLegacyData(MigrationOptions{
		LegacyDirs: []string{legacyDir},
		NewRootDir: newDir,
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := os.Stat(filepath.Join(newDir, "local-device.json")); err != nil {
		t.Fatalf("expected migrated identity: %v", err)
	}
}
```

- [x] **Step 2: 跑迁移测试确认当前行为缺失**

Run:

```powershell
Set-Location backend
go test ./internal/config -run TestMigrateLegacyDataCopiesIdentityAndDatabaseOnce -count=1
```

Expected:

```text
FAIL with undefined: MigrateLegacyData
```

- [x] **Step 3: 实现旧目录探测、一次性迁移与迁移标记**

```go
type MigrationOptions struct {
	LegacyDirs []string
	NewRootDir string
}

func MigrateLegacyData(opts MigrationOptions) error {
	if hasExistingRuntimeData(opts.NewRootDir) {
		return nil
	}
	for _, legacyDir := range opts.LegacyDirs {
		if !hasExistingRuntimeData(legacyDir) {
			continue
		}
		if err := copyRuntimeFiles(legacyDir, opts.NewRootDir); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(opts.NewRootDir, ".migrated"), []byte(legacyDir), 0o600)
	}
	return nil
}

func LegacyDataDirCandidates() []string {
	var candidates []string

	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		candidates = append(candidates, filepath.Join(appData, "MessageShare"))
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(homeDir, "AppData", "Roaming", "MessageShare"),
			filepath.Join(homeDir, ".config", "message-share"),
			filepath.Join(homeDir, "Library", "Application Support", "MessageShare"),
		)
	}
	return candidates
}
```

- [x] **Step 4: 在运行时启动前执行迁移**

```go
func NewHost(opts Options) *Host {
	if err := config.MigrateLegacyData(config.MigrationOptions{
		LegacyDirs: config.LegacyDataDirCandidates(),
		NewRootDir: opts.Config.DataDir,
	}); err != nil {
		opts.Logger.Printf("migrate legacy data: %v", err)
	}
	return &Host{cfg: opts.Config}
}
```

- [x] **Step 5: 跑迁移测试并提交**

Run:

```powershell
Set-Location backend
go test ./internal/config ./internal/runtime
```

Expected:

```text
PASS
```

Commit:

```bash
git add backend/internal/config/migration.go backend/internal/config/migration_test.go backend/internal/runtime/host.go backend/internal/config/config.go
git commit -m "feat: migrate legacy data into user home layout"
```

## Task 4: 桌面 bridge、本地文件桥接与普通文件直读发送

**对应 OpenSpec：** 3.1、3.3、1.4

**Files:**
- Create: `backend/internal/desktop/bridge.go`
- Create: `backend/internal/desktop/events.go`
- Create: `backend/internal/desktop/bridge_test.go`
- Create: `backend/internal/desktop/events_test.go`
- Modify: `backend/internal/localfile/manager.go`
- Modify: `backend/internal/localfile/manager_test.go`
- Modify: `backend/internal/app/local_file_service.go`
- Modify: `backend/internal/app/service.go`

- [x] **Step 1: 先写 path 注册与桌面命令测试**

```go
func TestRegisterPathCreatesLeaseWithoutPicker(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "demo.bin")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	manager := NewManager(nil, DefaultLeaseTTL, time.Now)
	lease, err := manager.RegisterPath(filePath)
	if err != nil {
		t.Fatalf("register path: %v", err)
	}
	if lease.DisplayName != "demo.bin" {
		t.Fatalf("unexpected display name: %q", lease.DisplayName)
	}
}
```

```go
func TestBridgeSendFileUsesNativeDialogAndStreamsFile(t *testing.T) {
	bridge := NewBridge(fakeHost{
		sendFileFunc: func(_ context.Context, peerID, fileName string, fileSize int64, content io.Reader) (app.TransferSnapshot, error) {
			if peerID != "peer-1" || fileName != "demo.txt" || fileSize != 5 {
				t.Fatalf("unexpected send args")
			}
			body, _ := io.ReadAll(content)
			if string(body) != "hello" {
				t.Fatalf("unexpected body: %q", string(body))
			}
			return app.TransferSnapshot{TransferID: "tx-1", FileName: fileName}, nil
		},
	}, fakeDialogs{openFileResult: "demo.txt"})

	if _, err := bridge.SendFile("peer-1"); err != nil {
		t.Fatalf("send file: %v", err)
	}
}
```

- [x] **Step 2: 跑测试确认缺少 `RegisterPath` 与桌面 bridge**

Run:

```powershell
Set-Location backend
go test ./internal/localfile ./internal/desktop
```

Expected:

```text
FAIL with undefined: RegisterPath
FAIL with package internal/desktop not found
```

- [x] **Step 3: 让 `localfile.Manager` 支持基于真实路径注册 lease**

```go
func (m *Manager) RegisterPath(path string) (Lease, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Lease{}, fmt.Errorf("stat local file: %w", err)
	}

	picked := PickedFile{
		Path:        path,
		DisplayName: filepath.Base(path),
		Size:        info.Size(),
		ModifiedAt:  info.ModTime().UTC(),
	}
	return m.newLease(picked)
}

func (m *Manager) Pick(ctx context.Context) (Lease, error) {
	if m.picker == nil {
		return Lease{}, fmt.Errorf("local file picker not configured")
	}
	picked, err := m.picker.Pick(ctx)
	if err != nil {
		return Lease{}, err
	}
	return m.newLease(picked)
}
```

- [x] **Step 4: 实现桌面命令与事件转发**

```go
type NativeDialogs interface {
	OpenFile(ctx context.Context) (string, error)
}

type Bridge struct {
	host    Host
	dialogs NativeDialogs
	events  EventEmitter
}

func (b *Bridge) Bootstrap() (app.BootstrapSnapshot, error) {
	return b.host.RuntimeService().Bootstrap()
}

func (b *Bridge) SendFile(peerDeviceID string) (app.TransferSnapshot, error) {
	path, err := b.dialogs.OpenFile(context.Background())
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return app.TransferSnapshot{}, err
	}
	return b.host.RuntimeService().SendFile(context.Background(), peerDeviceID, info.Name(), info.Size(), file)
}

func (b *Bridge) PickLocalFile() (app.LocalFileSnapshot, error) {
	path, err := b.dialogs.OpenFile(context.Background())
	if err != nil {
		return app.LocalFileSnapshot{}, err
	}
	return b.host.RuntimeService().RegisterLocalFile(context.Background(), path)
}
```

```go
type LocalFileResolver interface {
	Pick(ctx context.Context) (localfile.Lease, error)
	Resolve(localFileID string) (localfile.Lease, error)
	RegisterPath(path string) (localfile.Lease, error)
}

func (s *RuntimeService) RegisterLocalFile(_ context.Context, path string) (LocalFileSnapshot, error) {
	if s.localFiles == nil {
		return LocalFileSnapshot{}, fmt.Errorf("local file manager not configured")
	}
	lease, err := s.localFiles.RegisterPath(path)
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

```go
type WailsEventPublisher struct {
	emit func(eventName string, payload ...any)
}

func (p WailsEventPublisher) Publish(kind string, payload any) {
	p.emit("message-share:event", map[string]any{
		"kind":    kind,
		"payload": payload,
	})
}
```

- [x] **Step 5: 跑桌面 bridge 相关测试并验证**

Run:

```powershell
Set-Location backend
go test ./internal/localfile ./internal/desktop ./internal/app
```

Expected:

```text
PASS
```

Commit:

```bash
git add backend/internal/desktop/bridge.go backend/internal/desktop/events.go backend/internal/desktop/bridge_test.go backend/internal/desktop/events_test.go backend/internal/localfile/manager.go backend/internal/localfile/manager_test.go backend/internal/app/local_file_service.go backend/internal/app/service.go
git commit -m "feat: add desktop bridge and native file send"
```

## Task 5: 前端桌面客户端适配与无 localhost UI

**对应 OpenSpec：** 3.2、3.4、3.5

**Files:**
- Create: `frontend/src/lib/desktop-api.ts`
- Create: `frontend/src/lib/desktop-api.test.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/AppShell.tsx`
- Modify: `frontend/src/components/ChatPane.tsx`
- Modify: `frontend/src/AppShell.default-api.test.tsx`
- Modify: `frontend/vite.config.ts`

- [x] **Step 1: 先补桌面客户端与聊天面板交互测试**

```ts
it("桌面客户端通过 Wails 绑定调用 bridge 命令", async () => {
  const api = createDesktopApiClient({
    commands: {
      Bootstrap: vi.fn().mockResolvedValue({ localDeviceName: "我的电脑", peers: [], pairings: [], conversations: [], messages: [], transfers: [] }),
      SendFile: vi.fn().mockResolvedValue({ transferId: "tx-1", messageId: "msg-1", fileName: "demo.txt", fileSize: 5, state: "sending", direction: "outgoing", bytesTransferred: 0, progressPercent: 0, rateBytesPerSec: 0, etaSeconds: null, active: true, createdAt: new Date().toISOString() })
    },
    eventsOn: vi.fn(),
    eventsOff: vi.fn(),
  })

  await api.sendFile("peer-1")
  expect(api.bootstrap).toBeTypeOf("function")
})
```

```tsx
it("普通文件发送按钮不再依赖隐藏 file input", async () => {
  const onSendFile = vi.fn().mockResolvedValue(undefined)
  render(
    <ChatPane
      peer={trustedPeer}
      messages={[]}
      sendingText={false}
      sendingFile={false}
      pickingLocalFile={false}
      sendingAcceleratedFile={false}
      pickedLocalFile={null}
      historyHasMore={false}
      historyLoading={false}
      onSendText={vi.fn()}
      onSendFile={onSendFile}
      onPickLocalFile={vi.fn()}
      onSendAcceleratedFile={vi.fn()}
      onLoadOlderMessages={vi.fn()}
    />,
  )

  await userEvent.click(screen.getByRole("button", { name: "选择文件" }))
  expect(onSendFile).toHaveBeenCalledTimes(1)
})
```

- [x] **Step 2: 跑前端测试确认当前接口仍绑在 `File` 上传**

Run:

```powershell
Set-Location frontend
npm test -- --run src/lib/desktop-api.test.ts src/components/ChatPane.test.tsx
```

Expected:

```text
FAIL with module not found: ./desktop-api
FAIL because onSendFile expects File
```

- [x] **Step 3: 实现桌面客户端适配层**

```ts
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";
import {
  Bootstrap,
  ConfirmPairing,
  ListMessageHistory,
  PickLocalFile,
  SendAcceleratedFile,
  SendFile,
  SendText,
  StartPairing,
} from "../../wailsjs/go/main/DesktopApp";

export function createDesktopApiClient(): LocalApi {
  return {
    bootstrap: () => Bootstrap(),
    startPairing: (peerDeviceId) => StartPairing(peerDeviceId),
    confirmPairing: (pairingId) => ConfirmPairing(pairingId),
    sendText: (peerDeviceId, body) => SendText(peerDeviceId, body),
    sendFile: (peerDeviceId) => SendFile(peerDeviceId),
    pickLocalFile: () => PickLocalFile(),
    sendAcceleratedFile: (peerDeviceId, localFileId) => SendAcceleratedFile(peerDeviceId, localFileId),
    listMessageHistory: (conversationId, beforeCursor) => ListMessageHistory(conversationId, beforeCursor ?? ""),
    subscribeEvents: ({ onEvent }) => {
      const unsubscribe = EventsOn("message-share:event", (event) => onEvent(event as AgentEvent))
      return {
        close() {
          unsubscribe()
          EventsOff("message-share:event")
        },
        reconnect() {},
      }
    },
  }
}
```

- [x] **Step 4: 切换默认客户端与聊天面板文件发送入口**

```ts
function createLegacyHttpClient(options: LocalApiClientOptions = {}): LocalApi {
  return createLocalApiClient(options)
}

export function createHttpLocalApiClient(options: LocalApiClientOptions = {}): LocalApi {
  return createLegacyHttpClient(options)
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
  subscribeEvents: (options: { lastEventSeq?: number; onEvent: (event: AgentEvent) => void }) => EventSubscription;
}

export function createDefaultLocalApi(): LocalApi {
  if (typeof window !== "undefined" && "go" in window) {
    return createDesktopApiClient()
  }
  return createHttpLocalApiClient()
}
```

```tsx
async function handleSendFile() {
  if (!selectedPeer) {
    return
  }
  setBusyState((current) => ({ ...current, sendingFile: true }))
  try {
    const transfer = await resolvedApi.sendFile(selectedPeer.deviceId)
    startTransition(() => {
      setSnapshot((current) => current ? upsertOutgoingTransfer(current, selectedPeer.deviceId, transfer) : current)
    })
  } finally {
    setBusyState((current) => ({ ...current, sendingFile: false }))
  }
}

function upsertOutgoingTransfer(
  snapshot: BootstrapSnapshot,
  peerDeviceId: string,
  transfer: TransferSnapshot,
): BootstrapSnapshot {
  const conversationId = resolveConversationId(snapshot, peerDeviceId) ?? `conv-${peerDeviceId}`
  const nextSnapshot = ensureConversation(snapshot, peerDeviceId, conversationId)
  const withTransfer = upsertTransfer(nextSnapshot, transfer)

  return upsertMessage(withTransfer, {
    messageId: transfer.messageId,
    conversationId,
    direction: "outgoing",
    kind: "file",
    body: transfer.fileName,
    status: transfer.state === "done" ? "sent" : transfer.state,
    createdAt: transfer.createdAt,
  })
}
```

```tsx
<button
  className="ms-button ms-button--secondary"
  disabled={sendingFile || pickingLocalFile || sendingAcceleratedFile}
  onClick={() => {
    void onSendFile()
  }}
  type="button"
>
  {sendingFile ? "文件发送中..." : "选择文件"}
</button>
```

- [x] **Step 5: 调整 Vite 输出与前端测试并验证**

```ts
export default defineConfig({
  base: "./",
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    pool: "forks",
    poolOptions: {
      forks: {
        singleFork: true,
      },
    },
  },
})
```

Run:

```powershell
Set-Location frontend
npm test
```

Expected:

```text
PASS
```

Commit:

```bash
git add frontend/src/lib/desktop-api.ts frontend/src/lib/desktop-api.test.ts frontend/src/lib/api.ts frontend/src/lib/types.ts frontend/src/AppShell.tsx frontend/src/components/ChatPane.tsx frontend/src/AppShell.default-api.test.tsx frontend/vite.config.ts
git commit -m "feat: switch frontend to desktop api client"
```

## Task 6: 三端构建脚本、Wails CLI 集成与冗余清理

**对应 OpenSpec：** 4.1、4.2、4.3、4.4

**Files:**
- Create: `scripts/dev-desktop.ps1`
- Create: `scripts/build-desktop.ps1`
- Create: `scripts/smoke-desktop.ps1`
- Create: `scripts/dev-desktop.sh`
- Create: `scripts/build-desktop.sh`
- Create: `scripts/smoke-desktop.sh`
- Modify: `scripts/test.ps1`
- Delete: `backend/internal/webui/assets.go`
- Delete: `backend/internal/webui/dist/.keep`
- Delete: `backend/internal/api/http_server.go`
- Delete: `backend/internal/api/http_server_test.go`
- Delete: `backend/internal/api/http_local_files_test.go`
- Delete: `backend/internal/api/http_accelerated_transfers_test.go`
- Delete: `scripts/dev-web.ps1`
- Delete: `scripts/build-agent.ps1`
- Delete: `scripts/smoke-agent.ps1`

- [x] **Step 1: 先补桌面 smoke 脚本判定，确保桌面命令路径稳定**

```powershell
Describe "build-desktop.ps1" {
    It "uses backend as wails root" {
        $content = Get-Content -Raw "$PSScriptRoot\build-desktop.ps1"
        $content | Should -Match "Set-Location \\$backendDir"
        $content | Should -Match "wails build"
    }
}
```

- [x] **Step 2: 创建 Windows 与 POSIX 构建脚本**

```powershell
param(
  [string]$Platform = "windows/amd64"
)

$repoRoot = Split-Path -Parent $PSScriptRoot
$backendDir = Join-Path $repoRoot "backend"

Push-Location $backendDir
try {
  go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -clean -platform $Platform
} finally {
  Pop-Location
}
```

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
PLATFORM="${1:-linux/amd64}"

cd "${BACKEND_DIR}"
go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -clean -platform "${PLATFORM}"
```

- [x] **Step 3: 把测试脚本切到桌面化验证路径**

```powershell
Push-Location $backendDir
try {
    $env:GOCACHE = $goCacheDir
    $env:GOTELEMETRY = "off"
    go test -count=1 -p 1 ./...
} finally {
    Pop-Location
}

Push-Location $frontendDir
try {
    $env:npm_config_cache = $npmCacheDir
    npm ci
    npm test
} finally {
    Pop-Location
}

& "$repoRoot\scripts\smoke-desktop.ps1"
```

- [x] **Step 4: 删除旧 Web UI 入口与脚本**

Delete:

```text
backend/internal/webui/assets.go
backend/internal/webui/dist/.keep
backend/internal/api/http_server.go
backend/internal/api/http_server_test.go
backend/internal/api/http_local_files_test.go
backend/internal/api/http_accelerated_transfers_test.go
scripts/dev-web.ps1
scripts/build-agent.ps1
scripts/smoke-agent.ps1
```

- [x] **Step 5: 跑构建/脚本测试并验证**

Run:

```powershell
Set-Location .
powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64
```

Expected:

```text
PASS
desktop build succeeds
```

Commit:

```bash
git add scripts/dev-desktop.ps1 scripts/build-desktop.ps1 scripts/smoke-desktop.ps1 scripts/dev-desktop.sh scripts/build-desktop.sh scripts/smoke-desktop.sh scripts/test.ps1
git rm backend/internal/webui/assets.go backend/internal/webui/dist/.keep backend/internal/api/http_server.go backend/internal/api/http_server_test.go backend/internal/api/http_local_files_test.go backend/internal/api/http_accelerated_transfers_test.go scripts/dev-web.ps1 scripts/build-agent.ps1 scripts/smoke-agent.ps1
git commit -m "chore: add desktop scripts and remove legacy web ui"
```

## Task 7: 文档、全量验证与 OpenSpec 回写

**对应 OpenSpec：** 4.5、5.1、5.2、5.3、5.4

**Files:**
- Create: `docs/testing/wails-desktop-runtime.md`
- Modify: `openspec/changes/upgrade-to-wails-cross-platform-runtime/tasks.md`

- [x] **Step 1: 写桌面运行与验证文档**

````md
# Wails Desktop Runtime 验证记录

## 默认目录

- 配置：`~/.message-share/config.json`
- 身份：`~/.message-share/local-device.json`
- 数据库：`~/.message-share/message-share.db`
- 回退下载目录：`~/.message-share/downloads`

## Windows 验证

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1
```

## macOS / Linux 验证

```bash
./scripts/build-desktop.sh darwin/universal
./scripts/build-desktop.sh linux/amd64
./scripts/smoke-desktop.sh
```
````

- [x] **Step 2: 执行全量验证命令**

Run:

```powershell
Set-Location backend
go test -count=1 -p 1 ./...
```

Expected:

```text
PASS
```

Run:

```powershell
Set-Location ..\frontend
$env:npm_config_cache = 'E:\Projects\IdeaProjects\person\message-share\.cache\npm'
npm ci
npm test
```

Expected:

```text
PASS
```

Run:

```powershell
Set-Location ..
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1
```

Expected:

```text
desktop smoke succeeds
```

- [x] **Step 3: 逐项回写 OpenSpec `tasks.md`**

```md
- [x] 1.1 引入 Wails 桌面工程与新的桌面主入口，明确桌面窗口作为正式 UI 宿主
- [x] 1.2 将当前 agent 启动流程拆分为可复用的运行时核心初始化逻辑，保留局域网发现、配对、对等监听与存储能力
- [x] 1.3 移除正式运行路径中对 localhost Web UI server 的依赖，并改为由桌面宿主管理窗口生命周期与运行时生命周期
- [x] 1.4 建立桌面事件桥接，把现有配对、消息、传输与健康状态事件映射到 Wails 事件通道
```

- [x] **Step 4: 完成文档与状态回写收口**

```bash
git add docs/testing/wails-desktop-runtime.md openspec/changes/upgrade-to-wails-cross-platform-runtime/tasks.md
git commit -m "docs: record desktop runtime verification"
```

## 规格覆盖对照

- `desktop-shell-runtime`
  - Task 1 负责 Wails 宿主、窗口生命周期、bridge 主入口。
  - Task 4 负责桌面命令与事件桥接。
- `user-home-config-layout`
  - Task 2 负责 `.message-share` 布局、`config.json` 与设备名称同步。
  - Task 3 负责 legacy 迁移。
- `desktop-platform-support`
  - Task 6 负责 Windows/macOS/Linux 脚本与构建路径。
  - Task 7 负责跨平台验证说明。
- `local-file-bridge`
  - Task 4 负责 Wails 原生文件选择与 `LocalFileLease` 路径注册。
- `download-directory-delivery`
  - Task 2 负责 `.message-share/downloads` 回退目录接入。

## 风险复核

- Wails CLI 当前未预装，因此脚本必须使用 `go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0` 兜底，避免依赖用户手工安装。
- 前端默认客户端切换后，测试环境不得直接依赖真实 Wails runtime，必须通过 `createDesktopApiClient()` 注入假命令和假事件函数。
- `config.json` 与 `local-device.json` 的设备名称可能出现双写源，实施时必须以 `config.json` 为用户真源，并同步更新身份文件，不能保留“双真源”。
- 删除旧 HTTP server 之前，先确保桌面 bridge 已覆盖 bootstrap、配对、发消息、普通文件发送、极速文件发送与历史分页，否则会出现功能缺口。

## 占位符扫描结论

- 本计划未保留 `TODO`、`TBD`、`implement later`、`similar to task` 等占位描述。
- 所有任务均给出明确文件路径、代码骨架、验证命令与提交方式。
