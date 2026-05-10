## Purpose

定义 `shareme-agent` 在无窗口模式下提供仅限本机访问的 localhost Web UI 兼容入口、API 能力、事件同步、文件发送与构建验证要求。

## Requirements

### Requirement: 系统必须为 `shareme-agent` 提供仅限本机访问的 localhost Web UI 兼容入口

系统 MUST 在 `shareme-agent` 启动成功后提供可由本机浏览器访问的 localhost Web UI。该入口 MUST 只监听回环地址，并 MUST 拒绝非 loopback 请求。系统 MUST 在启动日志中输出可访问的本地地址。

#### Scenario: agent 启动后本机浏览器可以访问兼容入口
- **WHEN** 用户启动 `shareme-agent`，且 runtime 与 localhost Web server 均成功启动
- **THEN** 系统必须输出本地访问地址，并允许本机浏览器成功加载 Web UI

#### Scenario: 非本机请求被拒绝
- **WHEN** 非 loopback 来源尝试访问兼容入口的页面、API 或事件流
- **THEN** 系统必须拒绝该请求，且不得向其暴露兼容入口内容

### Requirement: 系统必须让 localhost 兼容入口提供与桌面入口一致的核心 P2P 交互能力

系统 MUST 通过 localhost 兼容入口向浏览器提供与桌面入口一致的数据形状和核心能力，包括启动快照、设备发现、配对发起、配对确认、文本发送、最近消息展示、历史消息分页和传输状态展示。兼容入口 MUST 继续复用现有会话与传输语义，而不是定义独立浏览器专用语义。

#### Scenario: 浏览器启动时获取统一快照
- **WHEN** 浏览器 UI 通过 localhost 兼容入口完成初始化
- **THEN** 系统必须返回包含设备、配对、会话、传输状态和 `eventSeq` 的统一启动快照

#### Scenario: 浏览器可以完成核心交互闭环
- **WHEN** 用户在 localhost 浏览器 UI 中选择目标设备并执行配对、发送文本或滚动加载历史消息
- **THEN** 系统必须完成对应操作，并以与桌面入口一致的状态模型更新页面

### Requirement: 系统必须通过可续接的实时事件流保持浏览器 UI 与 agent 状态同步

系统 MUST 为 localhost 兼容入口提供带 `afterSeq` 语义的实时事件流。该事件流 MUST 先补发 `eventSeq` 大于游标的 backlog，再持续推送 live events。客户端断线后 MUST 能从最后已确认的 `eventSeq` 继续续接，且系统 MUST 忽略重复或倒退事件。

#### Scenario: 首次订阅时补发 backlog 并进入 live 模式
- **WHEN** 浏览器使用 `afterSeq` 连接事件流，且存在更晚的历史事件
- **THEN** 系统必须先发送 backlog 事件，再持续发送后续实时事件

#### Scenario: 断线重连后从最后序号续接
- **WHEN** 浏览器事件流短暂中断后使用最后已接收的 `eventSeq` 重新连接
- **THEN** 系统必须只补发缺失事件，不得重复推送已处理事件

### Requirement: 系统必须支持浏览器普通文件发送且不得强制先落本地临时文件

系统 MUST 允许 localhost 浏览器 UI 通过普通文件上传路径向本机 agent 发起文件发送。系统 MUST 将上传输入流直接交给现有文件发送链路，而不得要求先将整文件强制写入 agent 本地临时目录后再发往对端。

#### Scenario: 浏览器普通文件发送成功
- **WHEN** 用户在 localhost 浏览器 UI 中选择普通文件并发起发送
- **THEN** 系统必须接收该上传流、创建统一传输记录，并将文件发送给目标设备

#### Scenario: 浏览器上传中断时传输失败且不生成最终文件
- **WHEN** 普通文件发送过程中浏览器上传被中断或请求异常终止
- **THEN** 系统必须将该传输标记为失败，且不得生成被视为完成的最终发送结果

### Requirement: 系统必须允许 localhost 浏览器 UI 委托 agent 发起极速文件发送

系统 MUST 为 localhost 浏览器 UI 提供 agent 原生本地文件选择入口，并在平台支持本地 picker 时返回 `localFileId` 供极速发送使用。若当前平台不支持本地 picker，系统 MUST 返回明确的 unsupported 错误，而不得伪造成功结果。

#### Scenario: 支持平台成功注册本地文件并发起极速发送
- **WHEN** 用户在支持本地 picker 的平台上通过 localhost 浏览器 UI 选择“极速发送”，并完成文件选择
- **THEN** 系统必须返回 `localFileId`，并允许该引用继续用于极速文件发送

#### Scenario: 不支持平台明确返回 unsupported
- **WHEN** 用户在不支持本地 picker 的平台上通过 localhost 浏览器 UI 请求本地文件选择
- **THEN** 系统必须返回明确的 unsupported 错误，供前端做能力降级展示
