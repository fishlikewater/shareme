## Why

当前 Message Share 已经具备局域网点对点消息与文件传输能力，但产品入口和运行时形态仍停留在“独立后台进程 + localhost Web 页面”阶段：用户需要手动访问本地地址，配置与数据目录分散且不统一，运行时默认路径对跨平台不友好，也缺少面向 macOS 与 Linux 的正式适配边界。这种状态已经开始阻碍产品交付、安装分发和后续维护。

本次变更要把系统从“本地网页工具”升级为“可分发的桌面应用”，同时统一用户目录布局、补齐跨平台运行时约束，并借此清理历史遗留接线与冗余文件，让后续功能迭代建立在更稳定、可维护的基础上。

## What Changes

- 引入 Wails 作为桌面壳，使用现有 React Web UI 作为桌面前端，替代当前依赖手动访问 `http://localhost` 的使用方式。
- **BREAKING** 调整默认配置与运行数据根目录为用户主目录下的 `.message-share`，统一承载本地身份、数据库、下载回退目录、日志与桌面运行时所需文件。
- 定义 Windows、macOS、Linux 三端统一的桌面运行时语义，包括目录布局、文件选择、窗口启动、构建产物与最小平台差异处理。
- 重构启动与 UI 接线：保留局域网传输核心能力，拆分可复用运行时核心与桌面壳绑定层，移除旧的 embed Web UI 与 localhost-only 主入口链路。
- 清理与新桌面运行时重复或无效的历史文件、脚本和接线代码，收敛目录结构与模块职责，以可读性和维护性为首要目标。

## Capabilities

### New Capabilities
- `desktop-shell-runtime`: 定义基于 Wails 的桌面壳启动、窗口内 UI 承载、前后端桥接与本地桌面主入口语义。
- `user-home-config-layout`: 定义默认用户根目录 `.message-share` 的目录结构、初始化规则、兼容迁移与运行时文件布局。
- `desktop-platform-support`: 定义 Windows、macOS、Linux 三端的桌面运行支持、平台差异适配与分发构建要求。

### Modified Capabilities
- `local-file-bridge`: 将本地文件选择与本地文件引用能力从 loopback Web UI 调用模型调整为 Wails 桌面桥接模型。
- `download-directory-delivery`: 将默认下载目录不可用时的回退落点从“应用数据目录”统一调整为用户根目录 `.message-share/downloads` 语义，并与新的目录布局保持一致。

## Impact

- 受影响后端模块：`backend/main.go`、`backend/app.go`、`backend/internal/app`、`backend/internal/api`、`backend/internal/config`、`backend/internal/desktop`、`backend/internal/localfile`、`backend/internal/runtime`
- 受影响前端模块：`frontend` 构建入口、API 调用层、文件选择接线与桌面运行时桥接
- 新增外部依赖与构建体系：Wails 桌面运行时、面向 Windows/macOS/Linux 的桌面构建脚本与产物组织
- 受影响默认行为：应用启动入口、默认配置根目录、下载回退目录、本地文件选择调用路径
- 受影响工程结构：旧 embed 静态资源链路、localhost-only UI 接线、重复脚本与冗余文件需要迁移或删除
