## ADDED Requirements

### Requirement: 系统必须允许 Web UI 通过本机 agent 发起原生文件选择

系统 MUST 提供仅对本机 loopback Web UI 可用的本地文件选择入口。该入口 MUST 由 agent 拉起操作系统原生文件选择能力，并在成功后返回安全文件引用而非真实路径。

#### Scenario: 成功选择本地文件
- **WHEN** 本机 Web UI 调用本地文件选择入口，且用户在原生文件对话框中确认单个文件
- **THEN** 系统必须返回 `localFileId`、展示名称、文件大小和是否满足极速条件

#### Scenario: 非本机调用被拒绝
- **WHEN** 非 loopback 页面或未通过本地来源校验的请求调用本地文件选择入口
- **THEN** 系统必须拒绝该请求并不得打开原生文件选择对话框

### Requirement: 系统必须使用 LocalFileLease 管理本地文件引用

系统 MUST 为每个已选择文件创建 `LocalFileLease`，并使用 `localFileId` 作为 Web UI 与应用层的唯一引用。`LocalFileLease` MUST 至少保存文件位置、展示名称、文件大小和有效期，并且不得向 Web UI 暴露真实路径。

#### Scenario: Web UI 仅收到安全引用
- **WHEN** 用户完成本地文件选择
- **THEN** 返回结果中必须包含 `localFileId`，且不得包含本地文件真实路径

#### Scenario: 过期 lease 不可继续发送
- **WHEN** 已创建的 `LocalFileLease` 超过有效期
- **THEN** 系统必须拒绝继续使用该 `localFileId` 发起极速发送，并要求重新选择文件

### Requirement: 系统必须在发送前重新校验本地文件状态

系统 MUST 在发起极速发送前校验 `localFileId` 对应文件仍存在且关键元数据未变化。若文件已删除、大小变化或无法访问，系统 MUST 拒绝发送并返回需要重新选择文件的错误。

#### Scenario: 文件未变化时允许发送
- **WHEN** `localFileId` 对应文件仍存在，大小和基础元数据与选择时一致
- **THEN** 系统必须允许该文件进入极速发送流程

#### Scenario: 文件变化时拒绝发送
- **WHEN** `localFileId` 对应文件不存在、大小变化或读取权限失效
- **THEN** 系统必须拒绝该发送请求并返回文件已失效错误
