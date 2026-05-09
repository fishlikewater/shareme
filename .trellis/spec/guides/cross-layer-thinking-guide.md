# 跨层思考指引

## 何时使用

- 修改 Wails binding、localhost HTTP/SSE API、Go snapshot、TypeScript 类型、事件格式。
- 修改配对、发现、消息、文件传输、历史分页、健康状态的状态流转。
- 修改 SQLite schema、运行数据目录、旧数据迁移。
- 修改安全边界：指纹校验、可信设备、loopback 限制、本地文件 lease。

## 检查顺序

1. 数据由哪个 Go 包产生？
2. 是否同时经过 Wails 与 localhost agent 两条入口？
3. `frontend/src/lib/types.ts`、API client、组件和测试是否同步？
4. 失败路径由谁恢复或提示？
5. 哪些 Go 测试、前端测试、smoke 覆盖该链路？

## 输出要求

- 列出受影响层：Go runtime、protocol/localui、frontend lib、UI component、SQLite。
- 列出契约字段和状态值。
- 列出错误路径与恢复策略。
- 列出实际验证命令。
