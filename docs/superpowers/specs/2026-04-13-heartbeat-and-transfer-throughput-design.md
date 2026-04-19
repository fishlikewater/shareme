# 局域网心跳保活与传输提速设计

## 背景

当前产品已经具备局域网发现、配对、文字消息、文件发送和本地 Web UI，但仍有两个影响交付体验的问题：

- 可达状态仍然主要依赖广播和被动入站成功。设备休眠、唤醒、广播短时丢失后，双方状态容易不对称，典型表现是“A 能发给 B，但 B 不会自己恢复把 A 标成可达”。
- 文件传输链路虽然已经有 telemetry，但吞吐仍受到服务端 multipart 解析、默认小缓冲复制和高频事件发布的影响，大文件传输速度和页面流畅度都不够理想。

本次目标是把这两个问题收敛到“可交付产品”的标准：设备可达状态能自动自愈，文件传输吞吐明显提升，且界面反馈持续稳定。

## 目标

### 产品目标

- 已配对设备在局域网内能够自动恢复 `reachable`，不依赖用户再次手动发送消息。
- 设备休眠、唤醒、广播抖动后，状态在合理时间内自动收敛，而不是长期假阴性或长期假阳性。
- 文件传输保持实时进度、速率、ETA 展示，同时提升实际吞吐并减少前端重绘抖动。
- 继续维持 V1 的“单 exe + 本地 Web UI”形态，不引入账号体系、离线消息和复杂长连接架构。

### 非目标

- 跨网段穿透
- 离线消息
- 断点续传
- 群聊/公告板
- 长连接控制通道重构

## 方案比较

### 方案 A：已配对设备轻量心跳 + 真流式上传下载 + 进度事件节流

- 配对后由 agent 在后台对已配对设备定期发起轻量心跳。
- 心跳成功即刷新 `LastDirectActiveAt`，连续失败达到阈值才转不可达。
- 文件上传链路改成真正流式解析 multipart，避免 `ParseMultipartForm` 带来的额外落盘和复制。
- 传输读写改成显式大缓冲 `io.CopyBuffer`，并对 `transfer.updated` 做时间片/字节阈值节流。

优点：

- 同时解决可达自愈和吞吐问题。
- 变更仍在现有 HTTP/TLS 架构内，V1 成本可控。
- 后续支持断点续传或批量任务时，心跳和节流策略都可复用。

缺点：

- 需要新增心跳协议与后台循环。
- 需要补一组后台状态机与吞吐回归测试。

### 方案 B：只加心跳，不改文件接收链路

优点：

- 改动更小。

缺点：

- 只能修复可达状态，传输速度提升有限。
- 现有 multipart 解析仍可能成为吞吐瓶颈。

### 方案 C：持久控制通道 + 独立文件流

优点：

- 长期演进空间最好。

缺点：

- 超出 V1 当前范围，实施成本过高。

## 选型结论

采用方案 A。

原因：它在不重写整体架构的前提下，同时解决“状态无法自愈”和“传输速度不佳”两个客户可感知问题，最符合“可交付产品”的要求。

## 详细设计

### 1. 可达状态模型

保留两类状态，但语义进一步收紧：

- `online`：最近是否收到广播。
- `reachable`：最近是否存在有效直连证据，或最近广播仍然新鲜且存在可用 endpoint。

建议参数：

- `discoveryTTL = 6s`
- `directReachabilityTTL = 2m`
- `heartbeatInterval = 20s`
- `heartbeatFailureThreshold = 2`

派生规则：

- `online = now - LastSeenAt <= discoveryTTL`
- `directFresh = LastKnownAddr != "" && now - LastDirectActiveAt <= directReachabilityTTL`
- `reachable = (online && LastKnownAddr != "") || directFresh`

说明：

- 广播负责“设备出现在局域网中”。
- 心跳与任意成功直连负责“设备仍可直接通信”。
- `reachable` 不再需要依赖“用户再次发送一条消息”才能恢复。

### 2. 心跳协议

新增专用 peer API：

- `POST /peer/heartbeat`

请求体：

```json
{
  "senderDeviceId": "peer-a",
  "sentAt": "2026-04-13T18:00:00Z",
  "agentTcpPort": 19090
}
```

响应体：

```json
{
  "responderDeviceId": "peer-b",
  "responderDeviceName": "办公桌面机",
  "agentTcpPort": 19090,
  "receivedAt": "2026-04-13T18:00:00Z"
}
```

行为：

- 请求端成功拿到响应后，刷新对端 `LastDirectActiveAt`。
- 接收端收到心跳并完成鉴权后，也刷新发送端 `LastDirectActiveAt`。
- 响应中带回 `agentTcpPort`，便于请求端在对端端口变化时学习最新地址。
- 心跳不产生消息记录，也不写入会话历史。

### 3. 心跳调度器

新增后台心跳循环，只对“已配对”设备启用。

调度规则：

- 周期性扫描 `ListTrustedPeers()`。
- 对满足以下条件的 peer 尝试心跳：
  - 已配对
  - 已知 `LastKnownAddr`
  - 当前不是本机
- 成功：
  - `MarkDirectActive`
  - 清零连续失败计数
  - 发布 `peer.updated`
- 失败：
  - 失败计数加一
  - 达到阈值后 `MarkReachable(false)`
  - 发布 `peer.updated`

额外约束：

- 同一 peer 不并发发送多个心跳。
- 心跳请求超时短于用户消息超时，例如 `3s`，避免后台探测拖慢系统。

### 4. 文件传输吞吐优化

#### 4.1 真流式 multipart 解析

当前本地 API 和 peer API 都使用 `ParseMultipartForm`，这会引入额外解析和临时文件开销。

改造为：

- 使用 `r.MultipartReader()`
- 按 part 顺序读取字段和文件流
- 文件 part 一旦拿到就直接传给 `app.SendFile` 或 `AcceptIncomingFileTransfer`

效果：

- 降低临时落盘和二次复制
- 更早进入实际传输阶段
- 大文件场景下更利于稳定吞吐

#### 4.2 显式大缓冲复制

在以下链路统一改为 `io.CopyBuffer(..., make([]byte, 256*1024))`：

- `protocol.HTTPPeerTransport.postMultipartFile`
- `app.RuntimeService.AcceptIncomingFileTransfer`

目的：

- 减少默认小块复制带来的 syscall 和回调开销
- 为 TLS + 本地磁盘写入提供更稳定的大块吞吐

### 5. 进度事件节流

当前每次 `Read` / `Write` 都会发布一次 `transfer.updated`，容易造成：

- 后端事件风暴
- 前端频繁重渲染
- 速率计算抖动

新增节流门：

- `minPublishInterval = 120ms`
- `minPublishBytes = 256KB`
- 完成/失败/取消事件立即发布，不经过节流

发布策略：

- 只有满足“距离上次发布超过时间阈值”或“累计字节超过阈值”才发布中间进度事件
- 传输结束时强制发布最终状态

效果：

- 降低事件总量
- 前端进度更稳
- CPU 和 SSE 压力更小

### 6. UI 保持一致但语义更可靠

本轮不推翻现有 UI 结构，只确保：

- `online=false && reachable=true` 时，文案明确表达为“最近直连成功，可自动恢复探测”
- 心跳成功/失败会触发 `peer.updated`，设备列表无需手动刷新
- 传输条仍显示实时速率，但事件频率降低后体验会更稳定

## 文件与模块边界

### 后端

- Modify: `backend/internal/protocol/peer_api.go`
  - 新增心跳请求/响应类型与接口
- Modify: `backend/internal/protocol/peer_http.go`
  - 新增 `/peer/heartbeat`
  - 把 multipart 入口改成流式解析
- Modify: `backend/internal/app/service.go`
  - 新增心跳处理与后台心跳循环
  - 抽取/复用传输进度节流
- Modify: `backend/internal/discovery/service.go`
  - 维持 `reachable` 派生语义，承接心跳刷新
- Modify: `backend/cmd/message-share-agent/main.go`
  - 启动后台心跳循环
- Modify: `backend/internal/transfer/telemetry.go`
  - 新增进度节流门或相关 helper
- Modify: `backend/internal/api/http_server.go`
  - 本地上传改成流式 multipart 读取

### 测试

- Modify: `backend/internal/app/service_test.go`
  - 心跳恢复可达、连续失败降级、节流行为
- Modify: `backend/internal/protocol/peer_http_test.go`
  - 心跳协议、流式 multipart 解析
- Modify: `backend/internal/discovery/service_test.go`
  - `directReachabilityTTL` 与自然过期
- Modify: `backend/internal/transfer/telemetry_test.go`
  - 节流门测试

## 验收标准

满足以下条件视为本轮交付完成：

1. A/B 已配对后，即使 B 不主动发送用户消息，只要双方仍在局域网且 endpoint 有效，B 也会在心跳窗口内自动把 A 恢复为 `reachable=true`。
2. 对端关机或离网后，`reachable` 会在心跳失败阈值或直连 TTL 后自然回落，不会长期假阳性。
3. 大文件传输在同一局域网内吞吐高于当前基线，且中间事件总量明显下降。
4. 前端进度、速率、ETA 仍持续可见，且比当前更平稳。
5. 后端、前端、统一测试脚本和打包脚本全部通过。

## 风险

- 心跳会增加少量后台流量，但只对已配对设备启用，周期较长，成本可控。
- 若某些网络环境屏蔽直连但保留广播，`online` 与 `reachable` 仍会分离；这是正确的产品语义，不视为问题。
- 传输提速受磁盘、杀毒软件和 TLS 影响，不保证线性倍增，但流式解析和节流应能带来稳定可见收益。
