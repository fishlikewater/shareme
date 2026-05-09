# 后端异常处理规范

## 原则

- Go 内部错误用 `%w` 包装，保留调用链上下文。
- 跨 HTTP/agent/Wails 边界时，错误语义要稳定，不能只返回模糊的 `failed`。
- 安全相关错误不得泄露密钥、私钥、完整证书内容或本机隐私路径。
- 用户可见错误要可理解，日志/测试中保留排障线索。

## 当前边界

- 点对点协议鉴权错误集中在 `internal/protocol` 与 `internal/app` 调用方校验处。
- localhost agent 的 HTTP 错误在 `internal/localui` 转换。
- Wails 方法错误经 `backend/app.go` 暴露给前端。
- 配置、迁移、SQLite 打开错误必须带路径或操作上下文，但避免泄露敏感内容。

## 禁止

- 静默吞错后继续发布成功事件。
- 因兼容 localhost agent 而绕过 loopback、安全或配对校验。
- 在前端只根据字符串猜测底层错误而不维护契约。
