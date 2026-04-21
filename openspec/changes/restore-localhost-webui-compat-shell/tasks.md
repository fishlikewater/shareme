## 1. 前端双宿主适配

- [ ] 1.1 新增 localhost `LocalApi` 客户端，并让默认工厂在 Wails bindings 与 loopback 浏览器之间自动选择宿主
- [ ] 1.2 保持 `AppShell` 和现有页面组件宿主无关，接入 localhost 普通文件发送与事件订阅
- [ ] 1.3 补齐前端单元测试，覆盖宿主选择、localhost endpoint 映射、SSE 去重与重连

## 2. `message-share-agent` localhost 兼容壳

- [ ] 2.1 提取共享前端静态资源选择逻辑与宿主无关业务门面，避免桌面和 localhost 重复包装
- [ ] 2.2 实现 loopback-only HTTP server、启动快照 API、配对/消息/历史消息 API、浏览器普通文件上传和 SSE 事件流
- [ ] 2.3 将 `message-share-agent` 启动链路接回为“runtime host + localhost Web UI”，并在启动日志打印本地访问地址

## 3. 极速文件发送委托与平台边界

- [ ] 3.1 为 localhost 兼容入口接入 agent 原生本地文件选择与 `localFileId` 注册流程
- [ ] 3.2 在不支持本地 picker 的平台上返回明确 unsupported 结果，并让前端做能力降级展示

## 4. 验证与文档

- [ ] 4.1 补齐后端测试，覆盖 loopback 限制、SSE backlog/续接、浏览器普通文件流式发送和 unsupported 分支
- [ ] 4.2 增加 `message-share-agent` localhost 兼容入口的 smoke / integration 验证，并确认桌面版未回归
- [ ] 4.3 更新相关构建与使用文档，并保持 OpenSpec tasks、实现计划和最终实现状态一致
