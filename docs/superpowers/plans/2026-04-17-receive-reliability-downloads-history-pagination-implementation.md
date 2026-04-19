# Receive Reliability, Downloads, and History Pagination Implementation Plan

> For agentic workers: 执行本计划时，以 `openspec/changes/improve-receive-reliability-downloads-and-history-pagination/tasks.md` 为任务真源，并保持回写同步。

**目标**

- 修复极速传输中“发送端快、接收端慢时可能失败”的闭环问题。
- 让接收文件默认落到用户下载目录，并在系统目录不可用时稳定回退。
- 把会话首屏收敛为最近 10 条消息，其他历史通过向上滚动分页加载。
- 保证实时事件、周期性 bootstrap 与历史分页三者可以共存且不会回退、重置或重复。

**架构取舍**

- 后端沿用现有控制面 + 数据面设计，不引入新协议层。
- 极速传输以“接收端落盘确认 ACK”为发送端 committed 依据，而不是“数据已写入 socket”。
- 普通文件接收与极速文件接收复用统一的临时文件创建、最终提交与重名避让逻辑。
- 历史分页仅新增会话维度本地 API，不改现有实时事件流模型。

**技术栈**

- 后端：Go、net/http、raw TCP、SQLite
- 前端：React 18、TypeScript、Vite、Vitest
- 交付：embed 静态资源、Windows PowerShell 脚本

**Source of Truth**

- `openspec/changes/improve-receive-reliability-downloads-and-history-pagination/`

**当前状态**

- OpenSpec `tasks.md`：18/18 已完成
- 本计划：5 个任务组全部完成并完成 fresh 验证

## Task 1: 接收目录统一交付

- [x] Step 1: 先补目录解析、系统下载目录回退、同名文件避让的失败测试。
- [x] Step 2: 新增默认下载目录解析器，支持环境变量覆盖、系统下载目录优先、应用数据目录回退。
- [x] Step 3: 让 `FileWriter`、`SessionReceiver`、`AcceleratedReceiver` 复用统一的临时文件与最终提交逻辑。
- [x] Step 4: 跑 `backend/internal/config` 与 `backend/internal/transfer` 定向测试并通过。

## Task 2: 极速传输接收闭环

- [x] Step 1: 先补 ACK 等待、ACK 超时、prepare 背压提示的失败测试。
- [x] Step 2: 扩展 prepare 响应，下发 `MaxInFlightBytes` 与 `AckTimeoutMillis`。
- [x] Step 3: 接收端在对外暴露 ACK frame 前先完成本地 ACK 入账；发送端只有收到 ACK 后才推进 committed 进度。
- [x] Step 4: 发送端实现基于接收窗口的有界流水线，并把 ACK 延迟纳入并发升降档判断。
- [x] Step 5: 根据 review 反馈补齐 session unregister 闭环，避免应用层删除会话后数据面仍接收迟到 lane。
- [x] Step 6: 根据 review 反馈修正“批次刚好打满窗口就判定接收端积压”的误判，避免无端降档。
- [x] Step 7: 跑 `backend/internal/app` 与 `backend/internal/transfer` 回归测试并通过。

## Task 3: 历史分页数据层与本地 API

- [x] Step 1: 在消息存储层增加按会话分页查询、边界游标与最近 10 条窗口能力。
- [x] Step 2: 调整 bootstrap，使其只返回每个会话最近 10 条消息及历史边界。
- [x] Step 3: 新增 `GET /api/conversations/{id}/messages` 本地分页 API。
- [x] Step 4: 补齐后端分页测试，覆盖窗口、边界、无更多历史与去重。

## Task 4: Web UI 历史加载与状态一致性

- [x] Step 1: 扩展前端类型与 API 客户端，接入会话历史分页查询。
- [x] Step 2: 调整聊天面板，首屏只展示最近 10 条，并在滚动接近顶部时触发分页加载。
- [x] Step 3: 合并 bootstrap、实时事件与已加载历史，避免重复、重置和 stale bootstrap 回滚。
- [x] Step 4: 修复测试环境中的 fake timers 泄漏，保证刷新相关用例不会拖挂后续测试。
- [x] Step 5: 跑前端回归测试，覆盖历史分页、实时追加、刷新恢复与排序/文案用例。

## Task 5: 交付验收保护

- [x] Step 1: 对应 OpenSpec 5.1，增加集成测试，验证普通文件与极速文件都会提交到默认下载目录。
- [x] Step 2: 对应 OpenSpec 5.2，运行 fresh 自动化测试与交付脚本：`go test -count=1 -p 1 ./...`、`npm test`、`scripts/test.ps1`、`scripts/build-agent.ps1`、`scripts/smoke-agent.ps1`。
- [x] Step 3: 对应 OpenSpec 5.3，更新 `docs/testing/receive-reliability-downloads-history-pagination.md` 与本计划，记录默认下载目录策略、ACK/解绑语义、历史分页行为以及单 exe + 本地 Web UI 的 build/smoke 证据。

## 本轮新增回归点

- [x] 接收端应用层删除/复用 session 时，同步从 `AcceleratedListener` 注销绑定。
- [x] listener 对已注销 session 的迟到 lane 会拒绝接入。
- [x] 发送端不会因为“刚好用满接收窗口”而误判为接收端积压。
- [x] 刷新类前端测试在使用 fake timers 后会恢复为 real timers，避免污染后续用例。

## Fresh Verification Evidence

```powershell
Set-Location backend
$env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'
$env:GOTELEMETRY='off'
go test -count=1 -p 1 ./...
```

- 结果：通过

```powershell
Set-Location frontend
$env:npm_config_cache='E:\Projects\IdeaProjects\person\message-share\.cache\npm'
npm test
```

- 结果：9 个测试文件通过，54 条用例通过

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\test.ps1'
```

- 结果：通过

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\build-agent.ps1'
```

- 结果：通过，并生成 `backend/message-share-agent.exe`

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\smoke-agent.ps1'
```

- 结果：通过；agent 可启动，`/api/health`、`/api/bootstrap` 与 `/` 均返回成功响应
