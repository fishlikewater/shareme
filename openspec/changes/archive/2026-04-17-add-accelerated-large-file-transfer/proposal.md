## Why

当前文件发送路径主要围绕浏览器上传和通用 HTTP 传输构建，能够满足普通文件发送，但不适合“局域网内单个大文件吃满带宽”的目标。随着产品从可用走向可交付，需要把大文件极速发送定义为正式能力，并为后续群发、广播和断点续传预留稳定边界。

## What Changes

- 新增“极速大文件传输”能力，仅针对局域网内已配对设备和单个大文件启用专用高速通道。
- 保留现有控制面安全链路，引入独立高速数据面，用于高吞吐文件字节传输和自动失败回退。
- 新增“本地文件桥接”能力，由本机 agent 通过原生文件选择能力直接获取本地文件来源，避免浏览器再次搬运大文件数据。
- 在本地 API 中新增极速发送入口和本地文件选择入口，并将进度、速率、回退与完成状态统一暴露给 Web UI。
- 明确 V1 范围：仅 Windows、仅已配对设备、仅点对点、仅发送端本机选取单个大文件，不包含目录、多文件调度、断点续传和跨平台桥接。

## Capabilities

### New Capabilities
- `accelerated-large-file-transfer`: 定义局域网内单个大文件的高速控制面、数据面、会话鉴权、完整性校验与失败回退要求。
- `local-file-bridge`: 定义本机 agent 发起原生文件选择、维护 `LocalFileLease`、向 Web UI 暴露安全文件引用的要求。

### Modified Capabilities

无。

## Impact

- 受影响后端模块：`backend/internal/api`、`backend/internal/app`、`backend/internal/protocol`、`backend/internal/transfer`
- 新增后端模块：`backend/internal/localfile`
- 受影响前端模块：`frontend/src/lib/api.ts` 及相关 UI 状态展示与交互逻辑
- 新增本地 API：`POST /api/local-files/pick`、`POST /api/transfers/accelerated`
- 受影响系统行为：大文件分流策略、传输状态机、局域网高速传输路径、Windows 本地文件选择集成
