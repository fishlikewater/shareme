# 局域网传输反馈与可达状态修正设计文档

## 1. 背景

当前 `message-share` 已经具备局域网设备发现、配对、文字发送、文件发送和本地 Web UI，但在用户体验和状态语义上还存在两个明显缺口：

- 文件传输在 UI 中只有结果态，没有过程态。用户看不到实时进度、传输速率、已传/总量和预计剩余时间，发送大文件时反馈不充分。
- 设备可达状态目前和真实链路状态存在分叉。典型场景是 A、B 已配对，B 休眠后重新开机，A 端显示 B 已配对且可达，B 端却显示 A 已配对但不可达；与此同时，A 发给 B 的消息 B 实际可以收到。这说明“收到广播”“主动请求失败”“收到入站流量成功”三类证据没有被统一建模。

本次设计目标是在不把 V1 过度重构的前提下，一次性补齐这两块体验短板，并保持未来支持断点续传、群聊、公告板广播时的扩展空间。

## 2. 本次设计目标

### 2.1 体验目标

- 文件发送和文件接收两侧都能实时显示传输进度。
- 顶部有一个“当前传输中”的精简状态条，聊天区文件消息卡有完整细节。
- 文件卡展示百分比、速率、已传/总量、预计剩余时间。
- 页面整体视觉风格升级为“温润层叠工作台”，强调层次感、简洁和完成度。

### 2.2 状态语义目标

- `reachable` 不再只由“最近是否收到广播”单独决定。
- 经过认证的入站直连流量也应被视为“对端最近可达”的证据。
- 休眠唤醒、广播短暂丢失、直连成功但发现状态未及时刷新等场景下，UI 状态应尽量接近真实链路状态。

## 3. 用户已确认的设计决策

本次设计基于以下已确认选择：

- 文件传输反馈范围：发送端和接收端都显示实时进度。
- 交互层级：文件消息卡里显示详细进度，同时页面顶部增加“当前传输中”的精简状态条。
- 指标内容：显示速率、百分比、已传/总量、预计剩余时间。
- 视觉方向：采用 `A. 温润层叠工作台`。
- 状态语义修正：把“入站成功应恢复可达状态”并入本次范围。

## 4. 范围与非目标

### 4.1 本次范围

- 扩展文件传输 telemetry 数据模型和事件流。
- 为发送和接收都补齐实时进度采样。
- 重做当前 Web UI 的视觉层次和文件传输呈现。
- 修正设备 `online/reachable` 的语义与恢复逻辑。
- 增补后端与前端测试，覆盖上述行为。

### 4.2 非目标

- 断点续传。
- 离线消息。
- 跨网段穿透。
- 群聊、公告板广播。
- 传输协议重写为完整分块确认协议。

## 5. 设计原则

### 5.1 V1 不过度设计

本次不重写文件协议，只在现有 `io.Reader / io.Writer` 传输链路上增加采样与事件推送。SQLite 继续持久化最终结果和必要中间状态，不引入复杂任务调度器。

### 5.2 状态以代理为单一真相源

传输速率、进度和 ETA 不由前端自行推算，统一以后端采样结果为准。前端只负责展示和轻量格式化，避免浏览器前后台切换导致数值漂移。

### 5.3 发现状态与直连状态分离

`online` 表示“最近是否收到该设备的发现广播”；`reachable` 表示“最近是否有可用的直连证据”。两者相关，但不再强行等同。

## 6. 用户体验设计

## 6.1 整体视觉方向

延续现有暖色基底，但把界面拆成更明确的三层：

- 背景层：暖米色纸感背景，叠加轻微径向光斑，保留温度。
- 分区层：左侧设备区使用更深的墨绿渐变，右侧工作区保持浅暖半透明面板，形成明确主次。
- 状态层：健康状态、当前传输状态、文件消息状态都用独立卡片和轻微玻璃感强调，但避免强烈霓虹或重工业监控台风格。

视觉目标是“看起来像一个完成度高的本地工具”，而不是普通后台列表页。

## 6.2 页面结构

页面结构保持当前信息架构，不做大拆：

- Hero 区：保留产品标题、本机设备信息和统计卡。
- 健康状态区：继续展示本地代理和发现状态。
- 新增活跃传输总览条：位于健康状态区之后、主工作区之前。
- 左侧设备区：保留设备列表，但层次更鲜明。
- 右侧工作区：聊天区、文件卡、配对区继续共存。

## 6.3 活跃传输总览条

顶部总览条负责回答“现在发生了什么”，设计要求如下：

- 默认突出展示 1 个主任务。
- 如果有多个活跃任务，附加显示“另有 N 个任务进行中”。
- 指标包含：
  - 当前状态：发送中 / 接收中 / 校验中
  - 百分比
  - 当前速率
  - 已传 / 总量
  - 预计剩余时间
- 完成后自动收起，不长期占位。
- 失败时短暂保留错误态，再降级回普通文件卡错误提示。

## 6.4 聊天区文件消息卡

文件消息卡从“文件名 + 一行状态文案”升级为完整状态卡：

- 标题区：文件名、方向、状态徽标、时间。
- 主进度区：一条进度条，颜色随方向变化。
- 指标区：四个信息块或一行组合指标
  - 百分比
  - 速率
  - 已传 / 总量
  - ETA
- 完成态：保留最终文件大小、方向和成功状态，不再显示动态速率。
- 失败态：保留失败状态和已传进度，便于判断失败时机。

发送和接收保持相同布局，只调整强调色：

- 发送：偏蓝青
- 接收：偏青绿

## 6.5 可达状态文案

由于 `online` 与 `reachable` 不再强行等同，设备列表文案需同步调整：

- `online=true, reachable=true`：已在线，可直传
- `online=false, reachable=true`：最近直连成功，可尝试发送
- `online=true, reachable=false`：已发现，但直连异常
- `online=false, reachable=false`：当前不可达

这样可以避免“实际刚收到消息，却仍显示不可达”的误导。

## 7. 后端设计

## 7.1 传输 telemetry 模型

现有 `TransferSnapshot` 仅包含：

- `transferId`
- `messageId`
- `fileName`
- `fileSize`
- `state`
- `createdAt`

本次扩展为包含运行态 telemetry 字段：

```go
type TransferSnapshot struct {
    TransferID        string
    MessageID         string
    FileName          string
    FileSize          int64
    State             string
    CreatedAt         string
    Direction         string
    BytesTransferred  int64
    ProgressPercent   float64
    RateBytesPerSec   float64
    EtaSeconds        *int64
    Active            bool
}
```

说明：

- `Direction` 由关联消息推导后写入 snapshot，便于前端直接渲染。
- `BytesTransferred`、`ProgressPercent`、`RateBytesPerSec`、`EtaSeconds` 属于运行态字段。
- `Active` 用于区分正在进行中的任务和历史完成任务。

## 7.2 活动传输注册表

新增内存态 `transfer registry`，负责维护正在进行中的任务：

- 发送开始时注册任务。
- 接收开始时注册任务。
- 每 250ms 左右采样并更新一次。
- 完成、失败、取消后写入最终状态并清理活动态。

注册表职责：

- 为 `transfer.updated` 事件提供实时 payload。
- 为 `Bootstrap()` 提供页面刷新后的活动传输恢复数据。
- 避免把高频速率采样全部落库。

## 7.3 telemetry 采样策略

发送端：

- 在 `io.Reader` 外包一层计数器 reader。
- 每次读到字节后累计 `bytesTransferred`。
- 通过“最近时间窗口内的增量字节”计算平滑速率。

接收端：

- 在写文件链路中包一层计数器 writer 或 `io.TeeReader` 统计器。
- 每次写入成功后更新 `bytesTransferred`。

ETA 计算：

- 当速率样本不足或速率接近 0 时，`EtaSeconds=nil`，前端显示“正在估算”。
- 一旦速率稳定，`eta = (fileSize - bytesTransferred) / rateBytesPerSec`。
- 速率采用平滑值，不直接用瞬时值，避免 ETA 抖动。

## 7.4 发送链路调整

当前发送文件是“传完后再保存消息和 transfer”。本次调整为：

1. 先 `EnsureConversation`
2. 先创建文件消息和 transfer 初始态
3. 将 transfer 状态置为 `sending`
4. 开始实际流传输并持续发布 `transfer.updated`
5. 传输结束后再写最终 `done/failed`

这样前端能在文件传输开始的第一时间看到消息卡，而不是等全部结束后才出现。

## 7.5 接收链路调整

当前接收文件是“写完整个文件后再保存消息和 transfer”。本次调整为：

1. 收到请求并完成鉴权后，立即创建 incoming message 与 `receiving` transfer
2. 开始写入临时文件，并持续更新 telemetry
3. 写入完成后切换到 `verified/done`
4. 提交文件并推送最终 `transfer.updated`

这样接收方也能看到实时过程，而不仅是最终结果。

## 7.6 可达状态语义修正

### 当前问题

当前 `Registry.Upsert()` 会在收到广播时直接把 peer 标记为 `online=true, reachable=true`；发送失败时又会把 peer 标为不可达；但入站文字、文件、配对确认成功不会恢复可达状态。

结果是：

- 广播和直连成功没有统一证据链；
- 某些设备在“实际能通信”的情况下仍显示 `reachable=false`。

### 新语义

新增两个时间维度：

- `LastSeenAt`：最近收到发现广播的时间
- `LastDirectActiveAt`：最近发生成功直连活动的时间

状态定义：

- `online = now - LastSeenAt <= discoveryTTL`
- `reachable = (online && lastKnownAddr != "") || now - LastDirectActiveAt <= directReachabilityTTL`

建议时间窗：

- `discoveryTTL = 6s`
- `directReachabilityTTL = 15s`

### 触发规则

以下事件会更新 `LastDirectActiveAt` 并触发 `peer.updated`：

- 成功发起或完成配对
- 成功发送文字
- 成功发送文件
- 成功接收文字
- 成功接收文件
- 成功处理配对确认

以下事件会把 `reachable` 显式置为 `false`：

- 主动发文字失败
- 主动发文件失败
- 主动确认配对失败
- 主动发起配对失败

广播仍负责更新：

- `LastSeenAt`
- `LastKnownAddr`
- `online`

这样可达性就不再只依赖发现广播。

## 7.7 Registry API 调整

为避免把发现逻辑和直连逻辑混在一起，`discovery.Registry` 新增或调整以下能力：

- `UpsertAnnouncement(...)`
- `MarkDirectActive(deviceID string, seenAt time.Time)`
- `MarkReachable(deviceID string, reachable bool)`
- `EnsurePeer(deviceID string, deviceName string)` 或等价能力

其中 `MarkDirectActive` 是关键补充，用于把经过认证的入站成功事件也纳入 reachability 证据。

## 7.8 持久化策略

SQLite 继续负责：

- 文件消息
- transfer 基础记录
- 最终状态

不把高频 telemetry 每次采样都落库，只落：

- 初始记录
- 重要状态转换（如 `receiving -> done`, `sending -> failed`）

必要时可为 transfer 表补以下字段：

- `bytes_transferred`
- `updated_at`

是否必须落这两个字段，以实现阶段最小变更为准；如不落库，也必须保证活动注册表可支持页面刷新后的短时恢复。

## 8. 前端设计

## 8.1 类型与状态

前端 `TransferSnapshot` 类型同步扩展 telemetry 字段，`BootstrapSnapshot` 和事件回放逻辑都要支持它们。

前端不自行推算速率与 ETA，只做：

- 百分比格式化
- 文件大小格式化
- ETA 文案格式化

## 8.2 新增派生选择器

`AppShell` 需要新增：

- `activeTransfers`：从 transfers 中筛选 `queued/sending/receiving/verified`
- `primaryTransfer`：选择一个最重要的任务用于顶部状态条
- `messageByTransfer` / `peerByTransfer`：把 transfer 映射回消息与设备

这样顶部总览条不需要额外接口。

## 8.3 组件调整

建议新增组件：

- `TransferStatusBanner`
- `FileMessageCard`

`ChatPane` 中：

- 文本消息继续用轻量消息卡
- 文件消息改用 `FileMessageCard`

`AppShell` 中：

- 在健康状态与主布局之间插入 `TransferStatusBanner`

## 8.4 页面刷新与恢复

页面刷新后：

- `bootstrap` 应带回数据库历史 transfer
- 如果有活动 transfer，则把内存注册表里的实时字段合并进去

这样刷新页面后，正在进行中的接收/发送不会直接消失。

## 9. 测试设计

## 9.1 后端测试

新增或调整测试覆盖以下行为：

- 发送文件时会创建初始 transfer 并持续发布 telemetry
- 接收文件时会在写入过程中发布 telemetry
- `TransferSnapshot` 正确计算 `bytesTransferred / progressPercent / rateBytesPerSec / etaSeconds`
- 页面刷新时 `Bootstrap()` 能合并活动传输
- 发送失败会把 peer 标为不可达
- 收到入站文字后会恢复 peer 可达状态
- 收到入站文件后会恢复 peer 可达状态
- 收到配对确认后会恢复 peer 可达状态

## 9.2 前端测试

新增或调整测试覆盖以下行为：

- 顶部总览条能展示活跃任务
- 文件消息卡能展示百分比、速率、已传/总量、ETA
- `transfer.updated` 事件能驱动界面增量更新
- ETA 为 `null` 时显示“正在估算”
- 完成态与失败态文案正确
- `online/reachable` 新语义下的设备文案正确

## 9.3 验收标准

满足以下条件时，本次设计可视为落地成功：

1. 发送文件时，发送端 UI 可实时看到进度、速率、已传/总量和 ETA
2. 接收文件时，接收端 UI 可实时看到进度、速率、已传/总量和 ETA
3. 顶部总览条能正确展示当前活跃传输
4. 页面刷新后，仍能恢复正在进行中的传输态
5. 设备休眠唤醒、广播暂未刷新时，只要发生经过认证的入站成功流量，对端就能在合理时间窗内恢复 `reachable=true`
6. 所有相关后端和前端测试通过

## 10. 风险与约束

### 10.1 当前协议仍为流式传输

本次不做完整分块确认协议，因此 telemetry 只解决“可见性”，不解决断点续传。

### 10.2 ETA 天生是估算值

即使使用平滑速率，局域网环境中的速率仍可能突变，因此 ETA 需要以“估算”而非“精确倒计时”的语义呈现。

### 10.3 reachable 仍是近似状态

`reachable=true` 表示“最近存在成功证据”，不等价于“此刻 100% 能成功发起下一次传输”。但这一定义会比当前实现明显更接近真实用户感知。

## 11. 结论

本次推荐落地方案是：

- 以现有传输链路为基础，引入轻量级实时 telemetry 注册表和事件推送；
- 以前后端统一的实时 transfer snapshot 驱动 UI；
- 在不重写协议的前提下补齐文件传输过程反馈；
- 把设备 `reachable` 从“仅凭发现广播判断”修正为“发现广播 + 直连成功证据”联合判断；
- 同时采用“温润层叠工作台”视觉方向重做当前 Web UI，使产品在保持简洁的同时具备更强层次感和更明确的任务反馈。
