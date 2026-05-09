# 思考指引

> 本目录沉淀跨 Wails 桌面、localhost agent、前端 UI、SQLite 与点对点传输之间最容易出错的判断点。

---

## 常见风险

- Wails 桌面入口与 localhost agent 只改其一。
- Go snapshot 字段、TypeScript 类型、UI 展示、测试断言不同步。
- 文件传输进度事件过频或丢失最终态。
- SQLite schema、旧数据迁移、查询实现只改一部分。
- 配对/指纹/loopback 安全边界被便利性改动绕过。

---

## 使用方式

- 写代码前先看 [编码前检查表](./pre-implementation-checklist.md)
- 涉及跨模块、异步链路、权限或状态流转时，看 [跨层思考指引](./cross-layer-thinking-guide.md)
- 涉及公共能力、常量、错误码、查询模板、校验逻辑时，看 [代码复用思考指引](./code-reuse-thinking-guide.md)

---

## 最小动作

修改前先搜索：

```bash
rg "关键字"
```
