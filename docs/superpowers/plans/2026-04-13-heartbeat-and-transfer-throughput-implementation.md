# Heartbeat And Transfer Throughput Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让已配对设备通过后台心跳自动恢复和维持可达状态，同时通过流式 multipart 与事件节流提升文件传输速度和稳定性。

**Architecture:** 在现有 HTTP/TLS peer 协议上新增轻量心跳接口，由 agent 后台循环对已配对设备做健康探测，并把结果统一收敛到 discovery 的 `reachable` 语义。文件传输链路改成真正流式解析与大缓冲复制，同时对中间进度事件按时间/字节双阈值节流。

**Tech Stack:** Go、net/http、TLS、SQLite、React、Vite、Vitest

---

### Task 1: 心跳协议与可达状态闭环

**Files:**
- Modify: `backend/internal/protocol/peer_api.go`
- Modify: `backend/internal/protocol/peer_http.go`
- Modify: `backend/internal/app/service.go`
- Modify: `backend/internal/discovery/service.go`
- Test: `backend/internal/protocol/peer_http_test.go`
- Test: `backend/internal/app/service_test.go`
- Test: `backend/internal/discovery/service_test.go`

- [ ] **Step 1: 写失败测试，锁定“B 不发用户消息也能自动恢复可达”**

测试名建议：

```go
func TestHeartbeatMarksTrustedPeerReachableWithoutUserTraffic(t *testing.T) {}
func TestHeartbeatFailureThresholdMarksPeerUnreachable(t *testing.T) {}
func TestRegistryDirectReachabilityExpiresAfterTTL(t *testing.T) {}
```

- [ ] **Step 2: 运行定向测试确认红灯**

Run: `go test ./internal/app ./internal/discovery ./internal/protocol -run 'Heartbeat|Reachability'`

Expected: FAIL，提示缺少 heartbeat 协议或恢复逻辑。

- [ ] **Step 3: 新增 peer heartbeat 请求/响应与 handler**

实现内容：

```go
type HeartbeatRequest struct {
	SenderDeviceID string `json:"senderDeviceId"`
	SentAtRFC3339  string `json:"sentAt"`
	AgentTCPPort   int    `json:"agentTcpPort"`
}

type HeartbeatResponse struct {
	ResponderDeviceID   string `json:"responderDeviceId"`
	ResponderDeviceName string `json:"responderDeviceName"`
	AgentTCPPort        int    `json:"agentTcpPort"`
	ReceivedAtRFC3339   string `json:"receivedAt"`
}
```

- [ ] **Step 4: 在 RuntimeService 中落地心跳接收与后台心跳循环**

实现点：

```go
func (s *RuntimeService) AcceptHeartbeat(ctx context.Context, request protocol.HeartbeatRequest) (protocol.HeartbeatResponse, error)
func (s *RuntimeService) RunHeartbeatLoop(ctx context.Context)
```

- [ ] **Step 5: 保持 `reachable` 为 TTL 派生状态，不允许永久粘住**

关键规则：

```go
reachable = (online && lastKnownAddr != "") || directReachable
directReachable = lastKnownAddr != "" &&
	!lastDirectActiveAt.IsZero() &&
	now.Sub(lastDirectActiveAt) <= directReachabilityTTL
```

- [ ] **Step 6: 跑后端相关测试确认绿灯**

Run: `go test ./internal/app ./internal/discovery ./internal/protocol`

Expected: PASS

### Task 2: 流式 multipart 与复制缓冲优化

**Files:**
- Modify: `backend/internal/api/http_server.go`
- Modify: `backend/internal/protocol/peer_http.go`
- Modify: `backend/internal/app/service.go`
- Test: `backend/internal/api/http_server_test.go`
- Test: `backend/internal/protocol/peer_http_test.go`
- Test: `backend/internal/app/service_test.go`

- [ ] **Step 1: 写失败测试，锁定流式解析路径**

测试名建议：

```go
func TestHandleFileTransfersStreamsMultipartWithoutParseMultipartForm(t *testing.T) {}
func TestPeerHTTPServerStreamsIncomingFilePart(t *testing.T) {}
```

- [ ] **Step 2: 运行定向测试确认红灯**

Run: `go test ./internal/api ./internal/protocol -run 'StreamsMultipart|FileTransfers'`

Expected: FAIL

- [ ] **Step 3: 把本地 API 上传入口改为 `MultipartReader`**

约束：

- 不再调用 `ParseMultipartForm`
- 逐个读取字段
- 遇到文件 part 时直接把 reader 传给 `SendFile`

- [ ] **Step 4: 把 peer 接收入口也改为 `MultipartReader`**

约束：

- `AcceptIncomingFileTransfer` 直接消费文件流
- 保留现有 TLS 鉴权与字段校验

- [ ] **Step 5: 把发送端和接收端复制改为大缓冲**

实现点：

```go
var transferCopyBuffer = make([]byte, 256*1024)
io.CopyBuffer(dst, src, transferCopyBuffer)
```

- [ ] **Step 6: 跑后端测试确认绿灯**

Run: `go test ./internal/api ./internal/protocol ./internal/app`

Expected: PASS

### Task 3: 传输事件节流与前端稳定展示

**Files:**
- Modify: `backend/internal/transfer/telemetry.go`
- Modify: `backend/internal/app/service.go`
- Test: `backend/internal/transfer/telemetry_test.go`
- Test: `backend/internal/app/service_test.go`
- Modify: `frontend/src/components/FileMessageCard.tsx`
- Modify: `frontend/src/components/TransferStatusBanner.tsx`
- Test: `frontend/src/components/FileMessageCard.test.tsx`
- Test: `frontend/src/components/TransferStatusBanner.test.tsx`

- [ ] **Step 1: 写失败测试，锁定中间事件节流和最终事件直发**

测试名建议：

```go
func TestProgressGateSuppressesHighFrequencyIntermediateEvents(t *testing.T) {}
func TestProgressGateAlwaysPublishesTerminalState(t *testing.T) {}
```

- [ ] **Step 2: 运行后端定向测试确认红灯**

Run: `go test ./internal/transfer ./internal/app -run 'ProgressGate|Transfer'`

Expected: FAIL

- [ ] **Step 3: 在 telemetry 或 app 层新增双阈值节流门**

规则：

- `minPublishInterval = 120ms`
- `minPublishBytes = 256 * 1024`
- `done/failed` 立即发布

- [ ] **Step 4: 保持前端展示语义**

要求：

- 未完成态最多显示 `99%`
- 完成态才显示 `100%`
- 宽度仍用原始百分比
- 现有平滑动画保持

- [ ] **Step 5: 跑前后端相关测试**

Run: `go test ./internal/transfer ./internal/app`

Run: `npm test -- FileMessageCard.test.tsx TransferStatusBanner.test.tsx`

Expected: PASS

### Task 4: 启动集成、全量验证与交付复核

**Files:**
- Modify: `backend/cmd/message-share-agent/main.go`
- Verify: `scripts/test.ps1`
- Verify: `scripts/build-agent.ps1`

- [ ] **Step 1: 在 agent 启动流程中接入心跳循环**

要求：

- discovery runner 启动后开启 heartbeat loop
- 进程退出时能随 `ctx.Done()` 停止

- [ ] **Step 2: 跑后端全量测试**

Run: `go test ./...`

Expected: PASS

- [ ] **Step 3: 跑前端全量测试与构建**

Run: `npm test`

Run: `npm run build`

Expected: PASS

- [ ] **Step 4: 跑统一脚本与打包脚本**

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1`

Expected: PASS，并生成新的 `backend/message-share-agent.exe`

- [ ] **Step 5: 多 agent review**

要求：

- 后端 reviewer：审心跳保活与可达状态闭环
- 传输 reviewer：审流式上传、缓冲优化与节流
- 集成 reviewer：审功能是否闭环、是否可落地

## 自检

- 可达恢复不再依赖用户再次发送消息。
- 可达状态不会永久粘住，会因 TTL 或连续失败自然回落。
- 上传和接收都不再依赖 `ParseMultipartForm`。
- 传输过程中中间事件被节流，但最终状态不丢。
- 全量测试、构建、打包和多 agent review 都通过。
