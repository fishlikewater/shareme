# 后端目录结构规范

## 主要入口

- `backend/main.go`：Wails 桌面入口。
- `backend/app.go`：暴露给前端的桌面应用方法。
- `backend/cmd/message-share-agent/`：无窗口 agent 与 loopback Web UI 入口。
- `backend/wails.json`：Wails 前端构建、资源路径与绑定输出配置。

## 核心包边界

- `internal/app/`：应用服务编排，承接配对、消息、文件发送、历史分页与事件发布。
- `internal/runtime/`：运行时装配，连接配置、存储、发现、协议与桌面宿主。
- `internal/domain/`：领域模型与跨模块稳定数据结构。
- `internal/config/`：运行根目录、配置、下载目录、旧数据迁移。
- `internal/store/`：SQLite 持久化。
- `internal/discovery/`：局域网设备发现状态。
- `internal/session/`：配对、消息与会话状态。
- `internal/protocol/`：点对点 HTTP 协议、请求响应结构与调用方鉴权。
- `internal/transfer/`：普通文件传输、加速传输、并发策略与遥测。
- `internal/localfile/`：本地文件选择、租约与极速发送本地文件引用。
- `internal/localui/`：localhost agent API、上传、事件流。
- `internal/desktop/`：桌面事件桥接。
- `internal/frontendassets/`：前端资源选择。

## 变更规则

- 新能力优先放入既有边界；只有边界无法清楚表达时才新增包。
- `internal/app` 可编排，但不应吞掉协议、存储、传输包内部职责。
- Wails 桌面入口和 localhost agent 共享同一业务服务时，应通过接口或 runtime 装配复用，不复制业务逻辑。
- 包名保持小写、短名、语义稳定；测试文件与被测包邻近放置。
