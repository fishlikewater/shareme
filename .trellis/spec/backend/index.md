# 后端开发规范

> 适用于 `backend/` 下 Go、Wails runtime、localhost agent、SQLite、局域网协议与文件传输代码。

---

## 当前基线

- 架构形态：单仓库桌面应用后端，正式入口为 Wails runtime，兼容入口为 headless localhost agent。
- 运行模块：`backend/main.go`、`backend/app.go`、`backend/internal/**`、`backend/cmd/shareme-agent`。
- 技术栈：Go 1.25、Wails v2.12、SQLite（`modernc.org/sqlite`）、标准库 HTTP、局域网发现与文件传输模块。
- 规范来源：以仓库内已落地代码、配置、测试和迁移脚本为准

---

## 阅读顺序

| 文档 | 用途 |
|------|------|
| [目录结构](./directory-structure.md) | 模块划分、包结构、命名方式 |
| [数据库规范](./database-guidelines.md) | 实体、迁移、查询、事务 |
| [异常处理](./error-handling.md) | 错误码、异常边界、统一返回 |
| [日志规范](./logging-guidelines.md) | 结构化日志、敏感信息、链路标识 |
| [质量规范](./quality-guidelines.md) | 测试、lint、门禁、禁止模式 |

---

## 使用原则

- 优先遵循仓库现有模式，不新增与现有风格冲突的写法
- 先查已有公共模块、公共库或基础设施封装
- 涉及跨模块、异步链路、权限边界时，同时阅读 `guides/` 下的思考指引

---

**文档语言**：中文。
