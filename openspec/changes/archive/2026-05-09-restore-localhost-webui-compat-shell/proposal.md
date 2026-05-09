## Why

当前正式入口已经切换到 Wails 桌面版，但 `message-share-agent` 失去了 localhost 浏览器访问能力。对于既有浏览器使用场景、无窗口运行场景和调试场景，这造成了明显退化，因此需要恢复一个兼容入口，同时不动摇桌面版的正式入口定位。

## What Changes

- 在 `message-share-agent` 中恢复仅限本机访问的 localhost Web UI 兼容壳。
- 让当前 React 前端同时支持 Wails 宿主和 localhost 宿主，继续复用同一套 UI 与状态模型。
- 为 localhost 兼容壳增加 HTTP API、SSE 事件流、浏览器普通文件发送和 agent 原生本地文件选择入口。
- 保持桌面版仍为正式入口，且桌面版本身不额外暴露 localhost 服务。

## Capabilities

### New Capabilities
- `agent-localhost-ui`: 为 headless agent 提供 localhost Web UI 兼容入口，覆盖浏览器访问、实时事件同步、普通文件发送和极速文件发送委托能力。

### Modified Capabilities

## Impact

- 受影响代码：
  - `backend/cmd/message-share-agent`
  - `backend/internal/runtime`
  - `backend/internal/desktop`
  - 新增 localhost HTTP/SSE 表现层
  - `frontend/src/lib`
  - `frontend/src/AppShell.tsx`
- 受影响能力：
  - 本机浏览器访问
  - 普通文件发送
  - 极速文件发送入口委托
  - 事件订阅与启动快照
- 受影响测试：
  - 前端宿主适配测试
  - localhost HTTP handler 测试
  - SSE 重放与重连测试
  - `message-share-agent` 兼容入口 smoke 测试
