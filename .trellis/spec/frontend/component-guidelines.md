# 前端组件规范

## 原则

- 组件职责单一，props 表达业务语义。
- Wails 与 localhost API 差异不得渗入展示组件。
- 复杂状态优先在 `AppShell` 或页面层收敛，再传给子组件。

## 当前重点

- `DeviceList` 负责设备与配对入口展示。
- `ChatPane` 负责会话消息、文本发送、文件发送入口。
- `FileMessageCard` 负责文件消息与传输态展示。
- `TransferStatusBanner`、`HealthBanner` 负责全局运行状态提示。

## 不建议

- 在展示组件中直接调用 `window.go` 或 `fetch`。
- 组件内复制后端状态枚举字符串且不更新 `types.ts`。
- 为局部场景创建泛化不足的“通用”组件。
