# 前端开发规范

> 适用于 `frontend/` 下 React UI、Wails 绑定客户端、localhost agent 客户端与样式代码。

---

## 当前基线

- 前端形态：同一套 React UI 同时服务 Wails 桌面入口与 loopback localhost Web UI。
- 技术栈：React 18、TypeScript、Vite、Vitest、Testing Library。
- 契约来源：`frontend/src/lib/types.ts`、`desktop-api.ts`、`localhost-api.ts` 与 Go 侧 snapshot / API 结构共同约束。

---

## 文档索引

| 文档 | 用途 |
|------|------|
| [目录结构](./directory-structure.md) | 说明模块划分和目录边界 |
| [组件规范](./component-guidelines.md) | 说明组件职责、组合与交互边界 |
| [Hook 规范](./hook-guidelines.md) | 说明复用逻辑与副作用边界 |
| [状态管理](./state-management.md) | 说明服务端状态、客户端状态和权限状态 |
| [质量规范](./quality-guidelines.md) | 说明前端门禁 |
| [类型安全](./type-safety.md) | 说明共享类型与契约同步原则 |

---

**文档语言**：中文。
