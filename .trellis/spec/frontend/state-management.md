# 前端状态管理规范

## 状态分类

- 运行快照：`BootstrapSnapshot` 返回的 health、peers、pairings、conversations、messages、transfers。
- 事件增量：`AgentEvent` 或 Wails 事件带来的 peer、pairing、message、transfer 更新。
- UI 临时状态：选中设备、当前输入、弹窗、发送中状态。

## 原则

- 后端快照与事件是事实源；前端只做合并、排序、展示。
- 同一实体用稳定 ID 合并：`deviceId`、`pairingId`、`messageId`、`transferId`。
- loading、error、empty 三态必须可区分。
- 事件乱序或重连后，应能通过重新 bootstrap 恢复一致状态。

## 不建议

- 桌面入口与 localhost 入口维护两套 UI 状态。
- 在多个组件中各自保存同一 transfer 进度。
- 用数组下标作为业务状态关联键。
