# Message Share

Message Share 是一个面向局域网场景的桌面应用，目标是让多台电脑在同一网段内通过桌面 UI 方便地发送文字与文件。

当前正式交付形态为 `Wails + Go + React` 的单桌面应用入口，不再依赖手动打开 `localhost` 页面。

## 核心能力

- 局域网设备自动发现与点对点配对
- 文字消息发送
- 文件发送与大文件直读本地磁盘发送
- 桌面宿主事件驱动 UI，不依赖本地 HTTP UI 入口
- 默认将配置与运行数据统一收敛到用户目录下的 `.message-share`

## 当前边界

- 当前范围是局域网内点对点传输
- 不包含离线消息
- 不包含跨网段穿透
- 不包含账号体系

## 技术栈

- 后端：Go、Wails v2、SQLite
- 前端：React 18、TypeScript、Vite
- 桌面宿主：Wails Desktop Runtime

## 仓库结构

- `backend/`：Go 桌面宿主、运行时核心、配置与本地存储
- `frontend/`：React 前端界面
- `scripts/`：开发、构建、冒烟验证、全量验证脚本
- `docs/testing/`：当前有效的验证记录与测试说明
- `openspec/changes/upgrade-to-wails-cross-platform-runtime/`：本轮桌面化改造的正式变更工件

## 运行数据目录

应用默认把数据放在当前用户主目录下的 `.message-share` 中：

- `~/.message-share/config.json`：用户可编辑配置
- `~/.message-share/local-device.json`：本机设备身份
- `~/.message-share/message-share.db`：SQLite 数据库
- `~/.message-share/downloads/`：下载回退目录
- `~/.message-share/logs/`：日志目录
- `~/.message-share/tmp/`：临时文件目录

如果设置了 `MESSAGE_SHARE_DATA_DIR`，则优先使用该目录作为运行根目录。

## 环境准备

### 通用依赖

- Go：建议直接安装与 [backend/go.mod](backend/go.mod) 一致的 `Go 1.25.x`
- Node.js 与 npm：用于前端依赖安装与构建
- Git：用于拉取代码与管理版本

### Wails 相关依赖

本仓库脚本通过 `go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0` 调用 Wails CLI，因此不是必须先全局安装 `wails` 命令；但若你想先做环境体检，建议额外安装 Wails CLI 并执行 `wails doctor`。

Wails 官方安装与构建文档：

- 安装与平台依赖：[Wails Installation](https://wails.io/docs/gettingstarted/installation/)
- 构建说明：[Wails Compiling your Project](https://wails.io/docs/gettingstarted/building/)

### 平台特定依赖

- Windows：需要安装 WebView2 Runtime
- macOS：需要安装 Xcode Command Line Tools，可执行 `xcode-select --install`
- Linux：需要标准 `gcc` 构建工具链以及 GTK3 / WebKitGTK 相关依赖

说明：

- Wails 官方文档指出，Linux 缺少 `webkit2gtk-4.0` 的发行版可能需要额外处理；例如 Ubuntu 24.04 一类环境，可能需要 `libwebkit2gtk-4.1-dev` 与 `-tags webkit2_41`
- 本仓库当前脚本没有内置该 Linux 特殊标签，如遇此类发行版，请结合 Wails 官方文档调整本地构建命令

## 本地开发

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\dev-desktop.ps1
```

### macOS / Linux

```bash
./scripts/dev-desktop.sh
```

说明：

- 开发模式实际调用的是 Wails `dev`
- 前端 Vite 开发服务由 Wails 配置自动接管

## 各平台构建说明

### 构建产物位置

- Windows：`backend/build/bin/message-share.exe`
- macOS：`backend/build/bin/message-share.app`
- Linux：`backend/build/bin/message-share`

### Windows 构建

在 Windows 主机上执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64
```

构建成功后产物位于：

```text
backend/build/bin/message-share.exe
```

### macOS 构建

在 macOS 主机上执行：

```bash
./scripts/build-desktop.sh darwin/universal
```

如只构建单架构，也可以改成：

```bash
./scripts/build-desktop.sh darwin/amd64
./scripts/build-desktop.sh darwin/arm64
```

构建成功后产物位于：

```text
backend/build/bin/message-share.app
```

### Linux 构建

在 Linux 主机上执行：

```bash
./scripts/build-desktop.sh linux/amd64
```

构建成功后产物位于：

```text
backend/build/bin/message-share
```

### 重要说明：跨平台构建边界

当前仓库的正式桌面构建，应在目标平台本机执行：

- Windows 桌面产物在 Windows 上构建
- macOS 桌面产物在 macOS 上构建
- Linux 桌面产物在 Linux 上构建

当前已验证的事实是：

- Windows 主机可以完整完成 Windows Wails 构建与桌面 smoke
- Windows 主机上的 `scripts/test.ps1` 会额外执行 `linux/amd64`、`darwin/amd64`、`darwin/arm64` 的 Go 层静态编译校验
- 这不等价于在 Linux 或 macOS 上完成真正的 Wails 桌面产物构建与 smoke

如果你需要 macOS / Linux 的正式桌面包，请在对应平台执行本仓库脚本。

## 冒烟验证

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1 -SkipBuild
```

### macOS

```bash
./scripts/smoke-desktop.sh darwin/universal
```

### Linux

```bash
./scripts/smoke-desktop.sh linux/amd64
```

`smoke-desktop` 会校验以下事项：

- 应用是否成功启动
- 运行根目录是否初始化
- `config.json` 是否落地
- UI ready marker 是否落地
- 注入的隔离端口是否被桌面宿主正确采用

## 全量验证

当前仓库内的一站式全量验证脚本是 Windows PowerShell 版本：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1
```

该脚本当前覆盖：

- 后端 `go test -count=1 -p 1 ./...`
- 前端 `npm ci` 与 `npm test`
- Wails `windows/amd64` 构建
- Windows 桌面 smoke
- `linux/amd64`、`darwin/amd64`、`darwin/arm64` 的 Go 层静态编译校验

说明：

- 脚本会在后端编译前确保 `backend/frontend/index.html` 编译兜底页存在，避免前端 `dist` 尚未生成时 embed 资源缺失导致验证失真
- 如果你在 macOS 或 Linux 上做正式交付验证，仍应在目标平台额外执行本机的 `build-desktop.sh` 与 `smoke-desktop.sh`

## 典型工作流

### Windows 开发者

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\dev-desktop.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64
```

### macOS 开发者

```bash
./scripts/dev-desktop.sh
./scripts/build-desktop.sh darwin/universal
./scripts/smoke-desktop.sh darwin/universal
```

### Linux 开发者

```bash
./scripts/dev-desktop.sh
./scripts/build-desktop.sh linux/amd64
./scripts/smoke-desktop.sh linux/amd64
```

## 相关文档

- [Wails 桌面运行时验证记录](docs/testing/wails-desktop-runtime.md)
- [Windows 局域网冒烟矩阵](docs/testing/windows-lan-matrix.md)
- [OpenSpec 与 superpowers 协作流程](docs/process/openspec-superpowers-workflow.md)

## Headless Agent + Localhost Web UI

除正式的 Wails 桌面入口外，仓库还提供一条兼容入口：`backend/cmd/message-share-agent`。

这条入口会启动无窗口 agent，并在本机提供 localhost Web UI：

- 默认访问地址：`http://127.0.0.1:52350/`
- 只允许本机 loopback 访问，不对局域网其他机器开放
- 前端仍复用同一套 React UI，不维护第二套页面
- 普通文件发送走浏览器 `multipart/form-data`
- 极速发送继续走 agent 原生本地文件选择 + `localFileId`

### Windows 构建

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1
```

### Windows 冒烟

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-agent.ps1
```

### macOS / Linux 构建

```bash
./scripts/build-agent.sh
```

### macOS / Linux 冒烟

```bash
./scripts/smoke-agent.sh
```

更多验证细节见：

- [Agent localhost runtime 验证](docs/testing/agent-localhost-runtime.md)
