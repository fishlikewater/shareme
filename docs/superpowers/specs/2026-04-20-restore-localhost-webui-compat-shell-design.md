# Message Share `message-share-agent` Localhost Web UI 兼容入口设计

## 1. 背景

当前仓库的正式产品入口已经切换为 Wails 桌面应用。`backend/cmd/message-share-agent` 仍保留无窗口 runtime 能力，但不再提供本地浏览器访问入口。

现有需求不是回退到“浏览器版重新成为正式入口”，而是恢复一个兼容入口：

- 正式入口仍然是 Wails 桌面版
- `message-share-agent` 恢复 localhost Web UI，作为兼容、调试与已知使用场景入口
- 兼容入口仅允许本机浏览器访问，不对局域网机器开放
- 前端应尽量复用当前桌面版 UI，不再维护一套分叉页面

## 2. 目标

- 在 `message-share-agent` 中恢复本地 Web UI 兼容壳
- 同一套 React UI 同时支持 Wails 宿主与 localhost 宿主
- 浏览器兼容入口支持以下能力：
  - 设备发现与设备列表
  - 配对发起与确认
  - 文本发送
  - 普通文件发送
  - 极速文件发送
  - 最近消息展示
  - 历史消息分页加载
  - 传输状态与事件同步
- 保持现有 runtime、发现、配对、高速传输链路不被重新实现

## 3. 非目标

- 不把 localhost Web UI 重新定义为正式入口
- 不允许局域网其他机器直接访问该页面
- 不引入账号体系、离线消息、跨网段穿透、多端同步
- 不在桌面版中额外暴露 localhost Web server
- 不在本次变更中重构 agent 间传输协议

## 4. 总体方案

采用“双宿主前端 + agent 本地 HTTP 兼容壳”方案。

- 前端继续保留单一 UI 实现
- `LocalApi` 作为宿主无关边界，保持业务接口稳定
- 新增 `LocalhostApiClient`，在浏览器 localhost 场景下通过 HTTP + SSE 访问本机 agent
- `message-share-agent` 在原有 runtime 之上增加本地 HTTP 兼容壳
- 桌面版仍通过 Wails bindings 调用同一组业务能力

该方案的核心原则：

- 不复制第二套 UI
- 不复制第二套业务实现
- 只在宿主适配层分流

## 5. 前端设计

### 5.1 `LocalApi` 保持稳定

当前 UI 主要依赖 `frontend/src/lib/api.ts` 中的 `LocalApi` 接口。兼容入口设计中，该接口保持不变：

- `bootstrap`
- `startPairing`
- `confirmPairing`
- `sendText`
- `sendFile`
- `pickLocalFile`
- `sendAcceleratedFile`
- `listMessageHistory`
- `subscribeEvents`

这样 `AppShell`、页面组件与状态收敛逻辑无需针对不同宿主做业务分支。

### 5.2 默认宿主选择

`createDefaultLocalApi()` 调整为双路选择：

1. 若存在 Wails bindings，则返回 `DesktopApiClient`
2. 若不存在 Wails bindings，且当前页面运行在 `localhost / 127.0.0.1 / [::1]`，则返回 `LocalhostApiClient`
3. 其他场景直接报错，拒绝作为远程浏览器页面运行

### 5.3 Localhost 客户端

新增 `frontend/src/lib/localhost-api.ts`，实现 `createLocalhostApiClient()`。

该客户端职责：

- 调用本机 agent 的 HTTP API
- 以 SSE 订阅实时事件
- 管理断线重连与 `eventSeq` 续接
- 与桌面宿主保持一致的数据结构和错误语义

### 5.4 文件发送模式

浏览器兼容入口保留两条文件发送路径：

1. 普通发送
   - 浏览器选中文件
   - 以 `multipart/form-data` 上传至本机 agent
   - agent 将文件流转交给 runtime service 发往对端
2. 极速发送
   - 调用本机 agent 原生文件选择
   - agent 注册本地文件，返回 `localFileId`
   - 后续由 agent 间直接走高速传输

这样既保留浏览器场景下的通用性，又保留“榨干局域网带宽”的主通路。

## 6. 本地 HTTP 兼容壳设计

### 6.1 定位

兼容壳是 `message-share-agent` 的一个宿主层，而不是一套新的业务后端。

它只负责：

- 静态资源分发
- HTTP API 包装
- SSE 事件推送
- 本机访问边界控制

核心业务仍由 runtime host 和 runtime service 提供。

### 6.2 启动链路

`message-share-agent` 启动顺序调整为：

1. 加载默认配置
2. 创建通用事件总线
3. 启动 `runtimehost.NewHost(...)`
4. 启动 localhost Web server
5. 控制台打印本地访问地址

启动成功后，agent 同时具备：

- 设备发现能力
- peer HTTPS 服务
- 高速传输监听
- localhost Web UI 兼容入口

### 6.3 共用前端静态资源

桌面版已经具备前端静态资源嵌入能力。localhost 兼容壳应复用同一份嵌入资源选择逻辑：

- 优先使用 `frontend/dist`
- 若未构建，则使用占位资源

该能力应抽为共享逻辑，避免桌面版与 localhost 版各维护一套资源选择代码。

## 7. 宿主无关业务门面

为避免 Wails 绑定层与 localhost HTTP handler 各自复制一套包装逻辑，新增宿主无关业务门面。

该层统一暴露：

- `Bootstrap`
- `StartPairing`
- `ConfirmPairing`
- `SendText`
- `SendFile`
- `RegisterLocalFile`
- `SendAcceleratedFile`
- `ListMessageHistory`
- `ReplayEvents / SubscribeEvents`

两种宿主共用该门面：

- Wails `DesktopApp` 调用门面
- localhost handler 调用门面

这样可以把差异严格收敛在“调用入口”和“传输协议”，而不是扩散到业务逻辑层。

## 8. HTTP API 设计

### 8.1 路由

- `GET /`
  - 返回 React SPA
- `GET /api/bootstrap`
  - 返回当前启动快照与 `eventSeq`
- `POST /api/pairings`
  - 发起配对
- `POST /api/pairings/{pairingId}/confirm`
  - 确认配对
- `POST /api/peers/{peerDeviceId}/messages/text`
  - 发送文字消息
- `POST /api/peers/{peerDeviceId}/transfers/browser-upload`
  - 普通文件发送，使用 `multipart/form-data`
- `POST /api/local-files/pick`
  - 弹本地文件选择并注册文件
- `POST /api/peers/{peerDeviceId}/transfers/accelerated`
  - 基于 `localFileId` 发起极速传输
- `GET /api/conversations/{conversationId}/messages`
  - 历史消息分页
- `GET /api/events/stream`
  - SSE 实时事件流

### 8.2 数据格式

- 响应结构尽量直接复用现有桌面版前端期望的数据形状
- 不为 localhost 单独发明第二套 view model
- 错误响应统一返回明确的人类可读错误信息，供 UI 直接展示

## 9. 事件流设计

### 9.1 选择 SSE

浏览器兼容入口的事件通道采用 SSE，而不是 WebSocket。

原因：

- 当前页面只需要服务端到客户端的单向事件推送
- SSE 在浏览器端接入更轻
- 天然适配断线重连
- 可以直接用 `eventSeq` 对应 SSE `id`
- 复杂度低于 WebSocket，足以满足当前需求

### 9.2 语义

事件流端点：

- `GET /api/events/stream?afterSeq=<n>`

服务端行为：

1. 先返回 `eventSeq > afterSeq` 的 backlog
2. 再进入 live 推送
3. 每条事件都携带递增 `eventSeq`

客户端行为：

- 记录最后收到的 `eventSeq`
- 断线后从最后序号续接
- 忽略序号倒退或重复事件

这样可与桌面端现有 `ReplayEvents + live events` 语义保持一致。

## 10. 文件发送与性能边界

### 10.1 普通文件发送

浏览器普通文件发送采用流式上传：

- 浏览器选中文件后直接 POST 到本机 agent
- handler 直接将输入流传给 runtime service
- 不要求先把整文件写入本地临时目录

该设计用于避免“浏览器文件再上传到本机 agent、再落临时文件”的重复 I/O。

### 10.2 极速发送

极速发送仍然是 V1 中吞吐优先的主路径：

- 文件由 agent 本地直接读取
- 浏览器不持有文件内容
- agent 间继续走现有高速传输链路

### 10.3 平台边界

当前本地原生文件选择能力在代码层面仅 Windows 已实现。

因此兼容入口 V1 采用显式平台边界：

- 普通文件发送：所有支持浏览器上传的平台可用
- `pickLocalFile + accelerated send`：Windows 完整支持
- 非 Windows 若尚未提供原生 picker，则返回明确的 unsupported 错误，前端做能力降级展示

## 11. 安全边界

localhost 兼容入口只允许本机访问。

约束如下：

- 服务端只监听回环地址
- 拒绝非 loopback 请求
- 页面、API、事件流采用同源部署
- 不开放跨域访问
- 不提供面向局域网浏览器的入口

该约束用于控制攻击面，并避免为兼容壳引入额外鉴权与 CSRF 复杂度。

## 12. 错误处理

- runtime host 启动失败：agent 启动失败，退出并打印明确错误
- localhost server 启动失败：视为 agent 启动失败
- `pickLocalFile` 在不支持平台调用：返回 unsupported
- SSE 连接中断：客户端自动续接
- 文件上传中断：服务端终止传输并回收状态
- 浏览器关闭：不影响 agent 存活

## 13. 测试与验收

### 13.1 后端验证

- localhost handler 路由测试
- loopback 限制测试
- SSE backlog + live 推送测试
- `afterSeq` 续接测试
- `multipart` 流式文件发送测试
- 静态资源选择与 SPA fallback 测试

### 13.2 前端验证

- `createDefaultLocalApi()` 宿主选择测试
- `LocalhostApiClient` endpoint 映射测试
- SSE 去重与续接测试
- `AppShell` localhost 模式主流程测试

### 13.3 集成验证

- 启动 `message-share-agent` 后可访问本地页面
- `bootstrap` 与事件流工作正常
- 文本发送闭环
- 普通文件发送闭环
- Windows 下极速发送闭环
- 历史消息分页闭环

### 13.4 共存验证

- Wails 桌面版仍可构建、启动和运行
- localhost 兼容入口只出现在 `message-share-agent`
- 前端不出现双套页面实现

## 14. OpenSpec 落地要求

当前已完成的 change `upgrade-to-wails-cross-platform-runtime` 与本需求方向不同，不能继续沿用其设计口径。

实现前必须新建 OpenSpec change，建议名称：

- `restore-localhost-webui-compat-shell`

该 change 应至少包含：

- `proposal.md`
- `design.md`
- `specs/agent-localhost-ui/spec.md`
- `tasks.md`

在没有新的 active change 之前，不进入正式编码实现。

## 15. 推荐结论

推荐按以下口径落地：

- 桌面版保持正式入口
- `message-share-agent` 恢复 localhost Web UI 兼容壳
- 前端继续维护单一 UI
- 宿主差异收敛在 `LocalApi` 适配层
- 浏览器实时事件使用 SSE
- 普通文件发送使用流式上传
- 极速发送继续走 agent 本地文件注册与高速传输链路
- Windows 先完整支持本地 picker，其他平台显式降级

该设计在复杂度、复用度、可维护性与用户兼容性之间取得当前最优平衡。
