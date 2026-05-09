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
