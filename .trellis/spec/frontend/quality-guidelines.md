# 前端质量规范

## 完成定义

- 关键交互有 Vitest/Testing Library 覆盖。
- `npm test` 通过。
- 涉及构建或 Wails 资源时，`npm run build` 通过。
- 入口兼容性不倒退：桌面 Wails binding 与 localhost browser fallback 均保持可用。

## 检查项

- 测试：在 `frontend/` 执行 `npm test`。
- 构建：在 `frontend/` 执行 `npm run build`。
- 全量门禁：在仓库根执行 `powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`。

## 禁止模式

- 为通过测试删除关键断言。
- 用 `any` 绕过类型错误。
- 只测渲染快照，不测用户行为、事件合并或错误态。
