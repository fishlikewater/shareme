# 前端类型安全规范

## 当前契约文件

- `frontend/src/lib/types.ts`：前端展示层使用的 snapshot、event、状态联合类型。
- `frontend/src/lib/api.ts`：统一 `LocalApi` 接口。
- `frontend/src/lib/desktop-api.ts`：Wails binding 适配。
- `frontend/src/lib/localhost-api.ts`：loopback HTTP/SSE 适配。
- Go 侧 snapshot 类型主要在 `backend/internal/app/service.go`。

## 规则

- 新增或改名字段时，必须同步 Go snapshot、`types.ts`、两个 API client 与相关测试。
- 不用 `any` 掩盖契约不确定性；外部事件 payload 可先是 `Record<string, unknown>`，落到组件前要收窄。
- 状态字符串扩展时，保留 `LooseString` 兼容未知值，但 UI 必须有默认展示路径。
- 组件不得直接拼接协议字段名来绕开 `LocalApi`。

## Scenario: 拖拽文件发送跨入口契约

### 1. Scope / Trigger

- Trigger：消息输入区支持外部文件拖拽发送，必须同时覆盖 localhost Web UI 的浏览器 `File` 与 Wails 桌面入口的文件路径回调。
- 范围：`ChatPane` 只负责把输入事件转成语义化回调；`AppShell` 负责当前 peer、忙碌态和快照更新；`LocalApi` 与 Go bridge 负责入口差异。

### 2. Signatures

```ts
// frontend/src/lib/api.ts
sendFile(peerDeviceId: string, file?: File): Promise<TransferSnapshot>
sendFilePath?(peerDeviceId: string, path: string): Promise<TransferSnapshot>
```

```go
// backend/app.go
func (a *DesktopApp) SendFilePath(peerDeviceID string, path string) (app.TransferSnapshot, error)

// backend/internal/desktop/bridge.go
func (b *Bridge) SendFilePath(ctx context.Context, peerDeviceID string, path string) (app.TransferSnapshot, error)
```

### 3. Contracts

- localhost/browser：`sendFile(peerDeviceId, file)` 跳过文件选择器，向 `/api/peers/{peerDeviceId}/transfers/browser-upload` 发送 `FormData`，字段为 `fileSize` 与 `file`。
- localhost/browser：`sendFile(peerDeviceId)` 仍调用浏览器文件选择器，再走同一 multipart endpoint。
- Wails/desktop：`options.App.DragAndDrop.EnableFileDrop = true`，前端通过 `window.runtime.OnFileDrop(callback, true)` 取得路径；可投放目标必须暴露 CSS `--wails-drop-target: drop`。
- Wails/desktop：`sendFilePath(peerDeviceId, path)` 调用 `DesktopApp.SendFilePath`，后端直接打开该路径并沿现有 `RuntimeService.SendFile(ctx, peer, fileName, fileSize, reader)` 发送。

### 4. Validation & Error Matrix

| Case | Expected |
|------|----------|
| `file` 存在 | 不打开 picker，直接上传该 `File` |
| `file` 为空 | 走原浏览器/桌面选择文件流程 |
| `path` 为空或空白 | Go bridge 返回 `localfile.ErrPickerCancelled` |
| `sendFilePath` 不存在 | 不注册 Wails path drop；localhost 仍用浏览器 `File` drop |
| `sendingFile` / `pickingLocalFile` / `sendingAcceleratedFile` 为真 | 拖拽路径发送直接忽略，避免并发文件发送 |

### 5. Good/Base/Bad Cases

- Good：localhost 文本框 `drop` 事件提供 `DataTransfer.files[0]`，`ChatPane` 调用 `onSendFile(file)`，`AppShell` 调用 `LocalApi.sendFile(peer, file)`。
- Base：点击“选择文件”仍调用 `onSendFile()`，不改变旧的 picker 行为。
- Bad：展示组件直接调用 `fetch`、`window.go` 或自行拼接 `/api/peers/...`。

### 6. Tests Required

- `frontend/src/components/ChatPane.test.tsx`：断言输入框 drop `File` 后调用 `onSendFile(file)`。
- `frontend/src/App.test.tsx`：断言浏览器 `File` drop 与 Wails `OnFileDrop` path 都能落到当前 peer 并更新消息。
- `frontend/src/lib/localhost-api.test.ts`：断言传入 `File` 时不调用 picker，multipart 含 `fileSize` 与 `file`。
- `frontend/src/lib/desktop-api.test.ts`：断言 `sendFilePath(peer, path)` 调用 Wails binding。
- `backend/internal/desktop/bridge_test.go`：断言 `SendFilePath` 不打开 dialog，直接读取目标路径并流式发送。

### 7. Wrong vs Correct

#### Wrong

```ts
// 展示组件绕过统一适配层，直接发 localhost endpoint。
await fetch(`/api/peers/${peerId}/transfers/browser-upload`, { method: "POST", body: form })
```

#### Correct

```ts
// 展示组件只表达用户意图，由 AppShell/API 适配入口差异。
await onSendFile(file)
await resolvedApi.sendFile(selectedPeer.deviceId, file)
```
