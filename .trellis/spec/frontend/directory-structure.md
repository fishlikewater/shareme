# 前端目录结构规范

## 当前结构

- `frontend/src/main.tsx`：React 入口。
- `frontend/src/App.tsx`、`AppShell.tsx`：应用组合与主壳。
- `frontend/src/pages/`：页面级视图，如发现页、设置页。
- `frontend/src/components/`：业务组件，如聊天、设备列表、文件消息卡、传输状态。
- `frontend/src/lib/`：本地 API 抽象、Wails client、localhost client、共享类型。
- `frontend/src/styles.css`：全局样式。
- `frontend/src/test/`：测试初始化。

## 变更规则

- UI 不应直接判断运行入口细节；入口差异留在 `lib/api.ts`、`desktop-api.ts`、`localhost-api.ts`。
- 新增后端字段时，先更新 `types.ts`，再同步组件与测试。
- 组件测试与组件邻近，API 测试与 `lib` 文件邻近。
- 不为一次性 UI 过早抽象通用组件；重复出现且语义稳定后再抽。
