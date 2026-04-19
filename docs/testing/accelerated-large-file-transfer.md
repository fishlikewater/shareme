# 极速大文件传输验证说明

## 1. 验证范围

本轮验证覆盖以下交付范围：

- Windows 本地文件桥接与 `LocalFileLease`
- 桌面 bridge：原生文件选择、普通文件发送与极速文件发送命令
- 高速控制面：prepare / complete / fallback
- 高速数据面：独立 TCP 端口、`transferToken` 鉴权、接收端完整性校验与原子提交
- Wails 桌面 UI：极速发送入口、已选本地文件信息层、进度 / 速率 / ETA / 回退态展示
- 单文件高速路径失败后的普通路径回退，且沿用同一 `transferId` / `messageId`

## 2. 自动化验收基线

以下命令作为本轮收口的 fresh 验证基线，结果以最近一次实际执行输出为准：

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\test.ps1'
```

结果：

- 后端 `go test -p 1 ./...` 通过
- 前端 `10` 个测试文件、`45` 条用例全量通过

```powershell
$env:npm_config_cache='E:\Projects\IdeaProjects\person\message-share\.cache\npm'; npm run build
```

结果：

- 前端生产构建通过

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\build-desktop.ps1' -Platform 'windows/amd64'
```

结果：

- Wails 桌面构建通过
- 生成交付文件：`backend/build/bin/message-share.exe`

```powershell
& 'E:\Projects\IdeaProjects\person\message-share\scripts\smoke-desktop.ps1' -SkipBuild
```

结果：

- 自动启动桌面产物并完成运行时初始化
- 主界面完成加载后写出 UI ready marker
- `config.json`、`local-device.json`、`message-share.db` 会在显式覆盖的运行目录下生成

## 3. 需求到测试的闭环映射

### 3.1 本地文件桥接

- `backend/internal/localfile/manager.go`
- `backend/internal/localfile/manager_test.go`
- `backend/internal/app/local_file_service_test.go`
- `backend/internal/desktop/bridge_test.go`
- `frontend/src/lib/desktop-api.test.ts`

覆盖点：

- 原生文件选择后仅返回安全 `localFileId`、展示名称、文件大小与极速资格，不暴露真实路径
- 桌面 bridge 只暴露命令结果与安全租约，不把真实路径泄露给前端
- `TestManagerResolveRejectsExpiredLease` 与 `TestManagerResolveRejectsChangedFile` 覆盖 lease 过期、文件删除、元数据变化或读取失效时拒绝继续发送
- 桌面 UI 调用宿主命令选择文件，并在极速发送时提交 `localFileId` 而不是浏览器文件对象

### 3.2 高速控制面、数据面与自适应 striping

- `backend/internal/app/accelerated_transfer_service_test.go`
- `backend/internal/protocol/peer_http_test.go`
- `backend/internal/transfer/accelerated_sender_test.go`
- `backend/internal/transfer/accelerated_receiver_test.go`
- `backend/internal/transfer/adaptive_parallelism_test.go`

覆盖点：

- prepare / complete 消息结构与接收端高速会话注册
- `transferToken` 鉴权与独立 TCP 数据端口连接校验
- `TestAcceleratedStripingControllerMovesAcrossDiscreteLevels` 覆盖离散档位 `1 -> 2 -> 4` 升档，以及 `senderBlocked` / `receiverBacklog` 触发的降档
- `TestAdaptiveParallelismIncreasesWhenThroughputGrows`、`TestAdaptiveParallelismFallsBackWhenRetriesSpike`、`TestAdaptiveParallelismKeepsCurrentOnEmptyWindowWithoutRetries` 覆盖吞吐增长升档、重试激增降档与空窗口稳定性
- `TestAcceleratedReceiverWritesFramesByOffsetAndCommitsAtomically` 覆盖分片写入、完整性校验与原子提交
- `TestAcceleratedReceiverRejectsChecksumMismatchWithoutCommit` 与 `TestAcceleratedReceiverRejectsMissingChecksumWithoutCommit` 覆盖摘要不匹配或缺失时不得提交
- `TestCompleteAcceleratedTransferRejectsMissingFileSHA256` 覆盖应用层拒绝空 `fileSha256`
- `TestSendAcceleratedFileUsesStandardPathWhenLeaseIsNotEligible` 覆盖不满足极速条件时不得建立极速会话，并改走普通文件路径
- `TestSendAcceleratedFileFallsBackWithSameTransferID` 覆盖 prepare 失败时沿用同一 `transferId` / `messageId` 回退普通传输
- `TestSendAcceleratedFileDoesNotFallbackOnCompleteFailure` 覆盖 complete 失败属于不可恢复错误，不得错误 fallback
- `TestSendAcceleratedFileSuccessfulPathKeepsUnifiedIDs` 覆盖合格文件成功路径沿用统一 `transferId` / `messageId`，并将 `sessionId` 传递到 sender 与 complete 阶段
- `TestSendAcceleratedFileLoopbackIntegrationCompletesWithoutFallback` 覆盖发送端、接收端与数据面的 loopback 集成闭环，且成功路径不触发普通传输 fallback

### 3.3 桌面 UI 与事件语义

- `frontend/src/App.test.tsx`
- `frontend/src/lib/desktop-api.test.ts`
- `frontend/src/components/FileMessageCard.test.tsx`
- `frontend/src/components/TransferStatusBanner.test.tsx`

覆盖点：

- “极速发送大文件”入口
- 已选本地文件展示与资格提示
- `TransferStatusBanner.test.tsx` 覆盖 preparing、传输中、速率、进度、ETA 与回退提示
- 高速发送失败与本地文件选择失败时透传后端错误正文
- 桌面事件通道订阅与关闭不会生成重复监听
- 高速回退后 UI 不生成重复传输记录

## 4. Windows 双机实机验收矩阵

本仓库内自动化验证覆盖了单机测试、构建链路与交付物运行态 smoke；双机局域网实机验收仍按以下矩阵执行：

### 4.1 环境

- 两台已接入同一局域网的 Windows 机器
- 双方均启动由 `scripts/build-desktop.ps1` 生成的 `message-share.exe`
- 双方均可打开桌面应用主界面

### 4.2 验收步骤

1. 在 A 机和 B 机完成配对，确认双方设备状态均为“已配对”。
2. 在 A 机桌面界面点击“极速发送大文件”。
3. 通过原生文件选择框选择一个超过极速阈值的大文件。
4. 确认 UI 出现“已选本地文件”和“满足极速条件”。
5. 点击“发送已选大文件”。
6. 在 A、B 两端观察传输状态：
   - 出现准备中 / 传输中状态
   - 进度条持续推进
   - 速率与 ETA 持续刷新
7. 传输完成后，在 B 机校验：
   - 文件落入下载目录
   - 文件大小正确
   - 可选：对比 SHA-256
8. 制造异常（如关闭高速监听或让 prepare 失败），确认：
   - UI 出现“准备回退普通传输”或“已回退普通传输”
   - 同一文件仅保留一条传输记录
   - 普通文件路径仍能完成收发

### 4.3 通过标准

- A 可发起单个大文件极速发送
- B 可完整接收并提交文件
- 高速失败时自动回退普通路径且无重复记录
- 桌面界面可见速率、进度、ETA 与回退提示

## 5. V1 限制

- 不包含离线消息
- 不包含跨网段穿透
- 不包含多端同步
- 不包含断点续传
- 本轮双机实机验收以 Windows 为主；桌面桥接与原生文件选择接口按 Wails 语义同时适配 Windows、macOS 与 Linux
