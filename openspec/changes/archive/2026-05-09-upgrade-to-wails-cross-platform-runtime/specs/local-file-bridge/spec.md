## MODIFIED Requirements

### Requirement: 系统必须允许桌面 UI 通过桌面宿主发起原生文件选择

系统 MUST 提供仅对当前桌面应用上下文可用的本地文件选择入口。该入口 MUST 由 Wails 桌面宿主拉起操作系统原生文件选择能力，并在成功后返回安全文件引用而非真实路径；正式桌面运行时 MUST 不再要求通过 loopback Web UI 访问该能力。

#### Scenario: 成功选择本地文件
- **WHEN** 当前桌面窗口中的 UI 调用本地文件选择入口，且用户在原生文件对话框中确认单个文件
- **THEN** 系统必须返回 `localFileId`、展示名称、文件大小和是否满足极速条件

#### Scenario: 正式桌面运行时不再暴露 loopback 选择主入口
- **WHEN** 正式桌面运行时启动
- **THEN** 系统必须通过桌面桥接提供本地文件选择能力，并且不得要求用户或前端访问 loopback HTTP 文件选择入口

### Requirement: 系统必须使用 LocalFileLease 管理本地文件引用

系统 MUST 为每个已选择文件创建 `LocalFileLease`，并使用 `localFileId` 作为桌面 UI 与应用层的唯一引用。`LocalFileLease` MUST 至少保存文件位置、展示名称、文件大小和有效期，并且不得向桌面 UI 暴露真实路径。

#### Scenario: 桌面 UI 仅收到安全引用
- **WHEN** 用户完成本地文件选择
- **THEN** 返回结果中必须包含 `localFileId`，且不得包含本地文件真实路径

#### Scenario: 过期 lease 不可继续发送
- **WHEN** 已创建的 `LocalFileLease` 超过有效期
- **THEN** 系统必须拒绝继续使用该 `localFileId` 发起极速发送，并要求重新选择文件

### Requirement: 系统必须在发送前重新校验本地文件状态

系统 MUST 在发起基于 `localFileId` 的发送前校验对应文件仍存在且关键元数据未变化。若文件已删除、大小变化或无法访问，系统 MUST 拒绝发送并返回需要重新选择文件的错误。

#### Scenario: 文件未变化时允许发送
- **WHEN** `localFileId` 对应文件仍存在，大小和基础元数据与选择时一致
- **THEN** 系统必须允许该文件进入极速发送流程

#### Scenario: 文件变化时拒绝发送
- **WHEN** `localFileId` 对应文件不存在、大小变化或读取权限失效
- **THEN** 系统必须拒绝该发送请求并返回文件已失效错误
