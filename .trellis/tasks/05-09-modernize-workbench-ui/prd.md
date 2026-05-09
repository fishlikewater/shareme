# Modernize Workbench UI

## Goal

将 Message Share 前端改造成现代清爽的工作台式 UI，优先优化设备切换、设备状态判断和主会话操作路径。

## Requirements

- 使用紧凑应用栏替代当前大 hero 展示区。
- 使用可折叠设备 Dock，展开态显示完整设备信息，收起态仍保留可识别状态。
- 会话主工作区集中展示当前设备上下文、消息流、发送入口、配对提示和不可达提示。
- 健康状态与传输状态以状态区域呈现，正常时克制，异常或活跃传输时提升可见性。
- 保持现有 `LocalApi`、Wails binding、localhost API、事件载荷、SQLite 和传输协议不变。
- 同一套 React UI 继续服务 Wails 桌面入口与 localhost Web UI。

## Acceptance Criteria

- [ ] 设备 Dock 可展开/收起，折叠不改变当前选中设备。
- [ ] 设备项的选中、配对、可达、可发送状态有文本和视觉双重表达。
- [ ] 未配对设备在主工作区显示信任建立路径，并阻止不可用发送。
- [ ] 不可达设备可查看历史消息，并阻止当前发送。
- [ ] 可发送设备保留文字、普通文件、大文件发送入口。
- [ ] 健康状态与活跃传输可在状态区域被识别。
- [ ] 375、768、1024、1440 宽度下无页面级横向滚动。
- [ ] `frontend npm test` 与 `frontend npm run build` 通过。

## Technical Notes

- OpenSpec change: `openspec/changes/modernize-workbench-ui/`
- Design: `docs/superpowers/specs/2026-05-09-modernize-workbench-ui-design.md`
- Implementation plan: `docs/superpowers/plans/2026-05-09-modernize-workbench-ui.md`
- Change level: L1 frontend-visible behavior/information architecture change.
