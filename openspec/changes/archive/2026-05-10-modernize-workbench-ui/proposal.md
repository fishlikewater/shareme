## Why

当前界面仍带有较强展示型首页结构：大标题、重卡片和暖色渐变占据首屏，用户进入后需要先理解页面层级，再完成“选择设备、判断状态、发送内容”的核心路径。shareme 已转为 Wails 桌面主入口与 localhost 兼容入口共用的工作工具，需要更直接、更清爽的工作台式 UI。

本变更将界面重心从“产品展示”调整为“立即操作”，降低设备状态判断和设备切换成本，并改善窄屏与桌面窗口缩放时的可用性。

## What Changes

- 将首屏大 hero 调整为紧凑应用栏，突出本机名称、同步状态和关键入口。
- 将现有设备列表重构为可折叠设备 Dock：收起态用于快速切换，展开态用于查看完整状态和最近消息。
- 将主区域重排为会话优先工作区：当前设备状态、消息流、发送器和配对/不可达提示在同一任务上下文内呈现。
- 将健康状态与传输状态改为更克制的状态条/抽屉式呈现，异常或传输活跃时提高可见性。
- 优化现代清爽视觉系统：浅灰白背景、青绿色状态色、橙色行动强调、稳定尺寸、清晰焦点态和低干扰动效。
- 保持 Wails 桌面入口和 localhost Web UI 入口共用同一 React UI；不新增后端 API，不改变传输协议、事件载荷或 SQLite 数据结构。

## Capabilities

### New Capabilities

- `workbench-ui`: 定义 shareme 前端工作台布局、设备 Dock、会话主区、状态表达、响应式行为和可访问性要求。

### Modified Capabilities

- None.

## Impact

- 影响入口：Wails 桌面 UI 与 `shareme-agent` localhost Web UI 共享前端。
- 主要代码：`frontend/src/AppShell.tsx`、`frontend/src/styles.css`、`frontend/src/pages/DiscoveryPage.tsx`、`frontend/src/components/DeviceList.tsx`、`frontend/src/components/ChatPane.tsx`、`frontend/src/components/HealthBanner.tsx`、`frontend/src/components/TransferStatusBanner.tsx`、`frontend/src/components/PairCodeDialog.tsx`。
- 测试影响：需要更新或新增 Testing Library 测试，覆盖设备切换、侧栏折叠、状态展示、未配对/不可达提示、发送入口可用性和响应式关键语义。
- 构建验证：至少执行 `npm test` 与 `npm run build`；实现完成后使用浏览器检查 375、768、1024、1440 宽度。
