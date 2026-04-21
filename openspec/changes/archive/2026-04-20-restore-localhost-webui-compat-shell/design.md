## Context

当前仓库已经完成 Wails 桌面运行时升级，正式入口是桌面应用。`message-share-agent` 仍保留无窗口 runtime 能力，但当前只负责启动 runtime，本地浏览器 UI 与对应 HTTP 层已不存在。

本次变更的目标不是恢复“浏览器版重新成为正式入口”，而是恢复一个兼容壳：

- 正式入口仍为 Wails 桌面版
- `message-share-agent` 额外挂载 localhost Web UI
- localhost Web UI 只允许本机访问
- 前端尽量复用现有桌面版 UI，而不是回到两套页面实现

该变更跨越前端宿主抽象、headless 启动链路、HTTP API、事件流和文件发送路径，属于典型的跨模块架构变更，需要先锁定设计口径再进入实现。

## Goals / Non-Goals

**Goals:**

- 恢复 `message-share-agent` 的 localhost Web UI 兼容入口。
- 让同一套 React UI 同时支持 Wails 和 localhost 宿主。
- 在浏览器兼容入口中支持设备发现、配对、文本发送、普通文件发送、极速发送、历史消息与实时事件。
- 保持现有 runtime、发现、配对与高速传输核心链路不被重写。
- 将宿主差异收敛到适配层，避免桌面版与 localhost 版长期分叉。

**Non-Goals:**

- 不把 localhost Web UI 重新定义为正式产品入口。
- 不开放局域网其他机器访问该页面。
- 不在桌面版中额外暴露 localhost 服务。
- 不在本次变更中重构 agent 间传输协议。
- 不顺带引入账号、离线消息、跨网段穿透或多端同步。

## Decisions

### 1. 使用双宿主前端，而不是维护两套 UI

前端继续以 `LocalApi` 作为宿主无关边界，新增 `LocalhostApiClient`，并由默认工厂在 Wails bindings 和 localhost 浏览器之间自动选择宿主。

- 选择原因：
  - 当前 `AppShell` 已经主要依赖 `LocalApi`
  - 可以把差异收敛到 `frontend/src/lib`
  - 避免维护第二套浏览器专用 UI
- 被否决方案：
  - 恢复一套独立浏览器前端。该方案短期快，长期一定分叉。

### 2. `message-share-agent` 增加 localhost HTTP 兼容壳，而不是回退旧浏览器产品形态

headless agent 继续负责 runtime 启动，同时新增本地 HTTP 服务，提供静态资源、JSON API 和 SSE 事件流。

- 选择原因：
  - 保持单进程、单 exe、单数据目录
  - 不复制业务服务
  - 与当前 headless 入口职责自然兼容
- 被否决方案：
  - 重新引入独立的浏览器版产品入口。该方案会模糊正式入口定位。

### 3. 浏览器事件通道使用 SSE，而不是 WebSocket

localhost 浏览器页面只需要服务端到客户端的单向事件推送，因此采用 SSE。

- 选择原因：
  - 实现简单
  - 浏览器端接入成本低
  - 可直接用 `eventSeq` 对应重放和续接
- 被否决方案：
  - WebSocket。收益有限，但实现和测试复杂度更高。
  - 轮询。实时性和语义都更差。

### 4. 普通文件发送使用流式浏览器上传，极速发送继续委托 agent 本地文件注册

兼容入口保留两条文件路径：

- 普通发送：浏览器选择文件，以 `multipart/form-data` 上传到本机 agent，handler 直接把输入流交给 runtime service，不强制先落临时文件。
- 极速发送：agent 调用本地文件选择器，返回 `localFileId`，再走既有高速传输链路。

- 选择原因：
  - 普通发送兼容浏览器心智
  - 极速发送继续走局域网高吞吐主路径
  - 避免“浏览器文件再次落本地临时文件”的重复 I/O
- 被否决方案：
  - 所有浏览器文件都先落临时文件。额外 I/O 明显，不符合需求。

### 5. 本机访问边界采用 loopback-only

localhost 兼容壳只监听回环地址，并拒绝非 loopback 请求。

- 选择原因：
  - 清晰限制安全边界
  - 避免引入额外鉴权与 CSRF 复杂度
  - 符合“兼容入口”定位
- 被否决方案：
  - 监听 `0.0.0.0` 供局域网浏览器访问。该方案扩大攻击面，也会改变产品定位。

### 6. 提取宿主无关业务门面，避免桌面与 localhost 重复包装

在 runtime service 之上新增一层宿主无关门面，统一提供 `Bootstrap`、配对、消息发送、普通文件发送、本地文件注册、极速发送、历史消息和事件重放能力。

- 选择原因：
  - Wails 绑定层和 localhost handler 都需要这组能力
  - 可以避免 `app.go` 和 localhost handler 各写一份近似代码
- 被否决方案：
  - localhost handler 直接复制 `DesktopApp` 的包装逻辑。短期可行，后续维护会持续分叉。

## Risks / Trade-offs

- [浏览器普通文件发送仍需经过一次 HTTP 上传] → 仅在“普通发送”路径存在，极速发送仍是吞吐优先主路径。
- [新增 localhost 宿主后测试矩阵扩大] → 将宿主差异限定在适配层，并补足前端/后端宿主适配测试。
- [非 Windows 当前缺少原生 picker] → 在兼容入口中显式返回 unsupported，前端做能力降级展示，不做假兼容。
- [localhost 服务增加额外运行时表面] → 严格限制为 loopback-only，且把启动失败视为 agent 启动失败。

## Migration Plan

1. 新建 localhost 兼容壳 OpenSpec capability 与实现任务。
2. 提取共享前端资源选择逻辑和共享业务门面。
3. 实现 localhost API、SSE 与浏览器普通文件发送。
4. 接回 `message-share-agent` 启动链路并打印本地访问地址。
5. 补齐前端、后端和 smoke 测试。
6. 验证桌面版仍可正常构建和运行。

回滚策略：

- 若兼容壳实现出现重大问题，可直接回滚 `message-share-agent` 的 localhost 服务接入层，保留原有纯 runtime 行为。

## Open Questions

- 当前不保留开放问题。Windows 优先完整支持、其他平台原生 picker 显式降级，已作为设计定案。
