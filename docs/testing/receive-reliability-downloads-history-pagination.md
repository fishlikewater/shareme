# 接收可靠性、下载目录与历史分页验收说明

## 1. 验收范围

本轮验收覆盖以下交付项：

- 普通文件接收与极速文件接收都提交到统一的默认下载目录。
- 极速传输以接收端落盘确认 ACK 作为发送端推进 committed 进度的依据，避免“发送端快、接收端慢”时误判完成。
- 会话首屏仅返回最近 10 条消息；更早历史通过游标分页加载；实时消息、分页结果与周期性 bootstrap 刷新可以共存且不重复。

## 2. 自动化验收结果

以下结果均来自 2026-04-19 本轮 fresh 验证：

```powershell
Set-Location backend
$env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'
$env:GOTELEMETRY='off'
go test -count=1 -p 1 ./...
```

结果：

- 后端全量测试通过。
- 关键模块 `internal/app`、`internal/api`、`internal/store`、`internal/transfer` 全部为 `ok`。

```powershell
Set-Location frontend
$env:npm_config_cache='E:\Projects\IdeaProjects\person\message-share\.cache\npm'
npm test
```

结果：

- 前端 `10` 个测试文件全部通过。
- 前端总计 `45` 条用例全部通过。

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\test.ps1'
```

结果：

- 仓库统一测试脚本通过。
- 脚本内后端测试以 `go test -count=1 -p 1 ./...` 方式通过，无缓存命中。
- 脚本内前端 `10` 个测试文件、`45` 条用例全部通过。

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\build-desktop.ps1' -Platform 'windows/amd64'
```

结果：

- Wails 桌面构建通过。
- 生成交付文件 `backend/build/bin/message-share.exe`。

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\smoke-desktop.ps1' -SkipBuild
```

结果：

- `message-share.exe` 可完成桌面启动链路并初始化运行目录。
- 主界面完成加载后会写出 UI ready marker。
- `config.json`、`local-device.json`、`message-share.db` 会在显式覆盖目录下生成。

说明：

- `npm ci` 输出了 `5 moderate severity vulnerabilities` 的审计提示，但未阻断测试与构建；本轮变更未引入新的依赖治理范围。

## 3. 需求到测试映射

### 3.1 默认下载目录统一提交

后端覆盖：

- `backend/internal/app/service_test.go`
  - `TestAcceptIncomingFileTransferWritesFileAndPersistsDone`
- `backend/internal/app/accelerated_transfer_service_test.go`
  - `TestSendAcceleratedFileLoopbackIntegrationCompletesWithoutFallback`
  - `TestCompleteAcceleratedTransferCommitsIncomingFile`
- `backend/internal/config/config_test.go`
- `backend/internal/transfer/file_writer_test.go`
- `backend/internal/transfer/session_receiver_test.go`
- `backend/internal/transfer/accelerated_receiver_test.go`

覆盖结论：

- 普通文件接收会把最终文件提交到 `DefaultDownloadDir`。
- 极速 loopback 集成路径会把接收文件提交到接收端的 `DefaultDownloadDir`。
- 同名文件避让、系统下载目录解析失败回退与最终提交语义均有自动化覆盖。

### 3.2 极速接收确认闭环

后端覆盖：

- `backend/internal/transfer/accelerated_sender_test.go`
  - `TestAcceleratedSenderWaitsForReceiverAckBeforeCommitting`
  - `TestAcceleratedSenderFailsWhenReceiverAckTimesOut`
  - `TestAcceleratedSenderDoesNotTreatFullWindowAsReceiverBacklog`
- `backend/internal/transfer/accelerated_receiver_test.go`
  - `TestAcceleratedListenerRejectsUnregisteredSession`
- `backend/internal/app/accelerated_transfer_service_test.go`
  - `TestPrepareAcceleratedTransferIncludesBackpressureHints`
  - `TestDeleteIncomingAcceleratedSessionUnregistersListenerBinding`
  - `TestTakeIncomingAcceleratedSessionByTransferIDUnregistersListenerBinding`
  - `TestSendAcceleratedFileLoopbackIntegrationCompletesWithoutFallback`

覆盖结论：

- 发送端仅在收到接收端 ACK 后推进 committed 统计。
- ACK 超时会触发失败路径。
- 会话从应用层删除或被标准传输复用时，会同步从数据面 listener 解绑，避免迟到 lane 继续写入旧 session。
- “批次刚好打满接收窗口”不会再被误判为接收端积压，条带并发不会因此无端降档。
- prepare 阶段会下发接收端窗口与 ACK 超时提示，供发送端有界流水线使用。

### 3.3 最近 10 条窗口与历史分页

后端覆盖：

- `backend/internal/app/service_test.go`
  - `TestBootstrapReturnsRecentTenMessagesWithHistoryCursor`
  - `TestListMessageHistoryReturnsOlderMessagesForConversation`
- `backend/internal/store/sqlite_test.go`

前端覆盖：

- `frontend/src/App.test.tsx`
  - `only renders the newest ten bootstrap messages for the active conversation`
  - `loads older messages when scrolling near the top`
  - `keeps loaded history when a realtime message arrives`
  - `does not duplicate messages when paged history overlaps with realtime updates`
  - `preserves loaded history after periodic bootstrap refresh`
- `frontend/src/lib/desktop-api.test.ts`

覆盖结论：

- bootstrap 首屏只保留最近 10 条。
- 向上滚动可以按游标加载更早消息。
- 分页历史、实时事件与 3 秒轮询刷新可以同时存在，不会清空已加载历史，也不会生成重复消息。

## 4. 用户可见行为

- 接收文件后，最终文件落在系统下载目录；若系统下载目录不可用，则回退到 `~/.message-share/downloads`。
- 桌面会话首屏更聚焦，只显示最近 10 条，向上滚动再按需取历史。
- 新消息到达、历史分页加载、轮询刷新三者并行时，消息列表保持稳定，不闪烁、不重置、不重复。
