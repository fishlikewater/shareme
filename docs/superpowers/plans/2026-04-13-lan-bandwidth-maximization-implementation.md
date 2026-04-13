# LAN Bandwidth Maximization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让局域网内文件传输在保持现有产品形态与稳定性的前提下，通过 HTTP/2、单流直通优化和大文件并行分片尽量逼近带宽上限。

**Architecture:** 保持 UDP 仅做发现、TLS/TCP 作为数据面；小文件继续走单流路径，但去掉本地 Web 上传的磁盘中转并显式启用 HTTP/2；大文件新增传输会话协议，由发送端按 chunk 并发上传，接收端按 offset 写入同一个临时文件，并通过轻量爬坡算法在 `2~8` 范围内自适应调整并发。

**Tech Stack:** Go、net/http、HTTP/2、TLS、SQLite、React、Vite、Vitest

---

### Task 1: 传输基线与单流直通优化

**Files:**
- Modify: `backend/cmd/message-share-agent/main.go`
- Modify: `backend/internal/api/http_server.go`
- Modify: `backend/internal/api/http_server_test.go`
- Modify: `backend/internal/protocol/peer_http.go`
- Modify: `backend/internal/protocol/peer_http_test.go`

- [ ] **Step 1: 写失败测试，锁定“本地上传不再经过完整临时文件中转”和“数据面 client 显式启用 HTTP/2”**

```go
func TestHandleFileTransfersStreamsBrowserUploadWithoutTempFileReplay(t *testing.T) {}
func TestPeerTransportClientForcesHTTP2WhenTLSConfigIsCustomized(t *testing.T) {}
```

- [ ] **Step 2: 运行定向测试确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/api ./internal/protocol -run 'StreamsBrowserUpload|ForcesHTTP2'"`

Expected: FAIL，提示仍在使用临时文件回放或 client transport 未显式开启 HTTP/2。

- [ ] **Step 3: 为 peer transport 抽出统一的高吞吐 client 配置**

```go
peerTransport := protocol.NewHTTPPeerTransport(protocol.HTTPPeerTransportOptions{
	Scheme: "https",
	ClientFactory: func(expectedFingerprint string) *http.Client {
		return &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:     security.NewClientTLSConfig(peerCertificate, expectedFingerprint),
				ForceAttemptHTTP2:   true,
				MaxIdleConns:        64,
				MaxIdleConnsPerHost: 32,
				MaxConnsPerHost:     16,
				IdleConnTimeout:     90 * time.Second,
				ReadBufferSize:      256 * 1024,
				WriteBufferSize:     256 * 1024,
			},
		}
	},
})
```

- [ ] **Step 4: 把本地 Web 上传入口改成真正流式直通，不再先完整落盘再回放**

```go
type uploadEnvelope struct {
	peerDeviceID string
	fileName     string
	fileSize     int64
	file         io.ReadCloser
}

func parseStreamingUpload(r *http.Request) (*uploadEnvelope, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	// 逐 part 读取字段，拿到 file part 后直接把 reader 包装给 SendFile。
}
```

- [ ] **Step 5: 跑定向测试确认绿灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/api ./internal/protocol -run 'StreamsBrowserUpload|ForcesHTTP2'"`

Expected: PASS

- [ ] **Step 6: 提交这一批基础链路改动**

```bash
rtk git add -- backend/cmd/message-share-agent/main.go backend/internal/api/http_server.go backend/internal/api/http_server_test.go backend/internal/protocol/peer_http.go backend/internal/protocol/peer_http_test.go
rtk git commit -m "feat: optimize single-stream LAN transport baseline"
```

### Task 2: 传输会话协议与接收端随机写入器

**Files:**
- Modify: `backend/internal/protocol/peer_api.go`
- Modify: `backend/internal/protocol/peer_http.go`
- Modify: `backend/internal/protocol/peer_http_test.go`
- Create: `backend/internal/transfer/session_types.go`
- Create: `backend/internal/transfer/session_receiver.go`
- Create: `backend/internal/transfer/session_receiver_test.go`
- Modify: `backend/internal/app/service.go`
- Modify: `backend/internal/app/service_test.go`

- [ ] **Step 1: 写失败测试，锁定会话创建、分片写入与最终提交**

```go
func TestPeerHTTPServerStartsTransferSession(t *testing.T) {}
func TestTransferSessionReceiverWritesPartAtOffset(t *testing.T) {}
func TestTransferSessionReceiverCompletesOnlyWhenAllPartsArrive(t *testing.T) {}
```

- [ ] **Step 2: 运行定向测试确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/protocol ./internal/transfer ./internal/app -run 'TransferSession|WritesPartAtOffset|CompletesOnlyWhenAllPartsArrive'"`

Expected: FAIL，提示缺少 session 类型、缺少 part handler 或缺少 offset 写入逻辑。

- [ ] **Step 3: 在 protocol 层新增传输会话协议对象**

```go
type TransferSessionStartRequest struct {
	TransferID     string `json:"transferId"`
	MessageID      string `json:"messageId"`
	SenderDeviceID string `json:"senderDeviceId"`
	FileName       string `json:"fileName"`
	FileSize       int64  `json:"fileSize"`
	FileSHA256     string `json:"fileSha256"`
	AgentTCPPort   int    `json:"agentTcpPort"`
}

type TransferSessionStartResponse struct {
	SessionID             string `json:"sessionId"`
	ChunkSize             int64  `json:"chunkSize"`
	InitialParallelism    int    `json:"initialParallelism"`
	MaxParallelism        int    `json:"maxParallelism"`
	AdaptivePolicyVersion string `json:"adaptivePolicyVersion"`
}
```

- [ ] **Step 4: 新增接收端 session writer，支持按 offset 写入同一个临时文件**

```go
type SessionReceiver struct {
	file       *os.File
	completed  map[int]CompletedPart
	totalParts int
	mu         sync.Mutex
}

func (r *SessionReceiver) WritePart(partIndex int, offset int64, content io.Reader) (int64, error) {
	section := io.NewOffsetWriter(r.file, offset)
	written, err := io.CopyBuffer(section, readerOnly{Reader: content}, make([]byte, 256*1024))
	// 记录 part 完成状态
	return written, err
}
```

- [ ] **Step 5: 在 peer HTTP server 中接入 start / part / complete 三类接口**

```go
mux.HandleFunc("/peer/transfers/session/start", ...)
mux.HandleFunc("/peer/transfers/session/part", ...)
mux.HandleFunc("/peer/transfers/session/complete", ...)
```

- [ ] **Step 6: 在 RuntimeService 中增加接收端会话入口，但暂不接入发送端**

```go
func (s *RuntimeService) StartIncomingTransferSession(ctx context.Context, req protocol.TransferSessionStartRequest) (protocol.TransferSessionStartResponse, error)
func (s *RuntimeService) AcceptIncomingTransferPart(ctx context.Context, req protocol.TransferPartRequest, content io.Reader) (protocol.TransferPartResponse, error)
func (s *RuntimeService) CompleteIncomingTransferSession(ctx context.Context, req protocol.TransferSessionCompleteRequest) (protocol.TransferSessionCompleteResponse, error)
```

- [ ] **Step 7: 跑定向测试确认绿灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/protocol ./internal/transfer ./internal/app -run 'TransferSession|WritesPartAtOffset|CompletesOnlyWhenAllPartsArrive'"`

Expected: PASS

- [ ] **Step 8: 提交接收端会话协议与写入器**

```bash
rtk git add -- backend/internal/protocol/peer_api.go backend/internal/protocol/peer_http.go backend/internal/protocol/peer_http_test.go backend/internal/transfer/session_types.go backend/internal/transfer/session_receiver.go backend/internal/transfer/session_receiver_test.go backend/internal/app/service.go backend/internal/app/service_test.go
rtk git commit -m "feat: add receiver-side chunk transfer sessions"
```

### Task 3: 发送端分片协调器与自适应并发

**Files:**
- Create: `backend/internal/transfer/session_sender.go`
- Create: `backend/internal/transfer/session_sender_test.go`
- Create: `backend/internal/transfer/adaptive_parallelism.go`
- Create: `backend/internal/transfer/adaptive_parallelism_test.go`
- Modify: `backend/internal/protocol/peer_http.go`
- Modify: `backend/internal/protocol/peer_http_test.go`
- Modify: `backend/internal/app/service.go`
- Modify: `backend/internal/app/service_test.go`

- [ ] **Step 1: 写失败测试，锁定“从 2 路开始并按吞吐爬坡调整并发”**

```go
func TestAdaptiveParallelismStartsWithTwoWorkers(t *testing.T) {}
func TestAdaptiveParallelismIncreasesWhenThroughputGrows(t *testing.T) {}
func TestAdaptiveParallelismFallsBackWhenRetriesSpike(t *testing.T) {}
func TestSessionSenderUploadsLargeFileAsMultipleParts(t *testing.T) {}
```

- [ ] **Step 2: 运行定向测试确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/protocol ./internal/app -run 'AdaptiveParallelism|SessionSenderUploadsLargeFileAsMultipleParts'"`

Expected: FAIL，提示缺少 adaptive controller、缺少 sender worker 或大文件仍走单流。

- [ ] **Step 3: 新增轻量爬坡式自适应并发控制器**

```go
type AdaptiveParallelism struct {
	current int
	min     int
	max     int
	previous WindowMetrics
}

func (a *AdaptiveParallelism) Observe(window WindowMetrics) int {
	if window.ThroughputGain(a.previous) > 0.08 && !window.RetryRateWorseThan(a.previous) {
		a.current++
	} else if window.ThroughputGain(a.previous) < 0.03 {
		// 平台期，保持不动
	} else if window.ThroughputDropsFrom(a.previous) || window.RetryRateWorseThan(a.previous) {
		a.current--
	}
	a.current = clamp(a.current, a.min, a.max)
	a.previous = window
	return a.current
}
```

- [ ] **Step 4: 实现发送端 session sender，把大文件切分为多个 chunk 并并发上传**

```go
type SessionSender struct {
	transport  SessionTransport
	telemetry  *Registry
	controller *AdaptiveParallelism
}

func (s *SessionSender) Send(ctx context.Context, file io.ReaderAt, totalSize int64, meta SessionMeta) error {
	// start session -> spawn workers -> upload part -> complete session
	return nil
}
```

- [ ] **Step 5: 在 RuntimeService.SendFile 中加入路径分流**

```go
const multipartThreshold = 64 << 20

if fileSize < multipartThreshold {
	return s.sendFileSingleStream(ctx, peer, ...)
}
return s.sendFileMultipartSession(ctx, peer, ...)
```

- [ ] **Step 6: 让聚合进度仍然只对外发布一个 transfer.updated**

```go
progress := AggregateChunkProgress(parts)
snapshot := domain.Transfer{
	TransferID:       transferID,
	BytesTransferred: progress.BytesTransferred,
	ProgressPercent:  progress.Percent,
	RateBytesPerSec:  progress.RateBytesPerSec,
}
```

- [ ] **Step 7: 跑定向测试确认绿灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/protocol ./internal/app -run 'AdaptiveParallelism|SessionSenderUploadsLargeFileAsMultipleParts'"`

Expected: PASS

- [ ] **Step 8: 提交发送端协调器与路径分流**

```bash
rtk git add -- backend/internal/transfer/session_sender.go backend/internal/transfer/session_sender_test.go backend/internal/transfer/adaptive_parallelism.go backend/internal/transfer/adaptive_parallelism_test.go backend/internal/protocol/peer_http.go backend/internal/protocol/peer_http_test.go backend/internal/app/service.go backend/internal/app/service_test.go
rtk git commit -m "feat: add adaptive multipart LAN file sender"
```

### Task 4: 进度聚合、前端语义与回归保护

**Files:**
- Modify: `backend/internal/transfer/telemetry.go`
- Modify: `backend/internal/transfer/telemetry_test.go`
- Modify: `backend/internal/app/service.go`
- Modify: `backend/internal/app/service_test.go`
- Modify: `frontend/src/components/FileMessageCard.tsx`
- Modify: `frontend/src/components/FileMessageCard.test.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.test.tsx`

- [ ] **Step 1: 写失败测试，锁定“并行分片下前端仍只看到单任务传输”**

```go
func TestMultipartTransferPublishesSingleAggregatedTransferSnapshot(t *testing.T) {}
func TestMultipartTransferTerminalEventIsAlwaysPublished(t *testing.T) {}
```

```tsx
it("在分片传输下仍展示单个文件卡片而不是多个 chunk", () => {})
it("进行中百分比仍不显示 100%，完成后才显示 100%", () => {})
```

- [ ] **Step 2: 运行定向测试确认红灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/app -run 'MultipartTransfer|AggregatedTransfer'"`  
Run: `rtk npm test -- FileMessageCard.test.tsx TransferStatusBanner.test.tsx`

Expected: FAIL，提示后端暴露了 chunk 级状态或前端语义与聚合模型不匹配。

- [ ] **Step 3: 让 telemetry 与节流门适配聚合进度**

```go
gate := transfer.NewProgressEventGate(120*time.Millisecond, 256*1024)
if gate.Allow(delta, now) {
	s.publishTransferEvent(aggregateTransfer)
}
if gate.Finish(now) {
	s.publishTransferEvent(aggregateTransfer)
}
```

- [ ] **Step 4: 前端保持单任务展示语义**

```tsx
const displayedPercent =
  transfer.state === "done" ? 100 : Math.min(99, Math.round(transfer.progressPercent));
```

- [ ] **Step 5: 跑定向测试确认绿灯**

Run: `rtk powershell -NoProfile -Command "go test ./internal/transfer ./internal/app -run 'MultipartTransfer|AggregatedTransfer'"`  
Run: `rtk npm test -- FileMessageCard.test.tsx TransferStatusBanner.test.tsx`

Expected: PASS

- [ ] **Step 6: 提交聚合进度与前端语义修正**

```bash
rtk git add -- backend/internal/transfer/telemetry.go backend/internal/transfer/telemetry_test.go backend/internal/app/service.go backend/internal/app/service_test.go frontend/src/components/FileMessageCard.tsx frontend/src/components/FileMessageCard.test.tsx frontend/src/components/TransferStatusBanner.tsx frontend/src/components/TransferStatusBanner.test.tsx
rtk git commit -m "feat: aggregate multipart transfer progress for UI"
```

### Task 5: 全量验证、带宽回归证据与交付复核

**Files:**
- Verify: `backend/...`
- Verify: `frontend/...`
- Verify: `scripts/test.ps1`
- Verify: `scripts/build-agent.ps1`
- Optional doc update: `docs/testing/`（如需补充局域网吞吐验证记录）

- [ ] **Step 1: 跑后端全量测试**

Run: `rtk go test ./...`

Expected: PASS，所有后端测试通过。

- [ ] **Step 2: 跑前端全量测试**

Run: `rtk npm test`

Expected: PASS，所有前端测试通过。

- [ ] **Step 3: 跑前端构建**

Run: `rtk npm run build`

Expected: PASS，`dist/` 正常产出。

- [ ] **Step 4: 跑统一测试脚本**

Run: `rtk powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`

Expected: PASS，后端与前端脚本链路通过。

- [ ] **Step 5: 跑打包脚本**

Run: `rtk powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1`

Expected: PASS，并生成 `backend/message-share-agent.exe`。

- [ ] **Step 6: 收集带宽验证证据**

```text
环境：
- 发送端设备型号 / 网卡 / 磁盘
- 接收端设备型号 / 网卡 / 磁盘
- 有线 / Wi-Fi

样本：
- 32 MB 文件：单流路径
- 256 MB 文件：并行分片路径
- 1 GB 文件：并行分片路径

记录：
- 峰值 MB/s
- 平均 MB/s
- 最终并发收敛值
```

- [ ] **Step 7: 发起多 agent review**

```text
Reviewer 1：审协议与接收端会话一致性
Reviewer 2：审发送端自适应并发与吞吐路径
Reviewer 3：审交付闭环与回归风险
```

- [ ] **Step 8: 提交最终收口**

```bash
rtk git add --all
rtk git commit -m "feat: maximize LAN transfer bandwidth with adaptive multipart sessions"
```

## 自检

- Spec 中的协议选型、分流阈值、自适应并发、单流直通优化都对应到至少一个任务。
- 计划中没有保留 “TBD / TODO / implement later” 之类占位符。
- 类型命名在任务间保持一致：`TransferSessionStartRequest`、`TransferPartRequest`、`AdaptiveParallelism`、`SessionSender`。
- 每个任务都能独立产出可验证增量，不依赖“大爆炸式”一次改完。
