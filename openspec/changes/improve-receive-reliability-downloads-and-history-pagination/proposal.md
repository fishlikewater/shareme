## Why

当前版本的大文件传输已经针对发送端做了提速，但接收端在落盘吞吐跟不上时仍可能被发送端压垮，最终表现为“发送端快、接收端慢、传输失败”。与此同时，接收文件默认没有稳定落到用户熟悉的“下载”目录，聊天页也会一次性加载全部历史消息，随着记录增多会拖慢首屏与交互体验。

这三个问题都已经影响到产品可交付性：文件传输要先保证接收闭环，再保证文件落点符合用户习惯，并把消息历史读取改成面向长会话可持续扩展的模式。

## What Changes

- 强化接收端文件接入能力，为极速文件传输补充显式背压、接收窗口控制、写盘确认与超时策略，避免发送端持续快推导致接收端失败。
- 统一定义接收文件的落点策略：默认写入当前操作系统用户的“下载”目录；若系统下载目录不可解析或不可写，则回退到现有应用数据目录下载区，并保持文件名去重与原子提交。
- 新增会话消息分页能力：会话首屏只返回最近 10 条消息，更早消息通过滚动触发分页加载，不再在聊天页默认灌入全部历史记录。
- 调整本地 API 与前端状态装配方式，使 Bootstrap 只承担首屏消息窗口，历史消息改由显式分页接口按会话拉取。

## Capabilities

### New Capabilities
- `download-directory-delivery`: 定义接收文件默认落到系统下载目录、失败回退目录、文件名冲突处理与最终提交语义。
- `message-history-pagination`: 定义会话首屏最近 10 条消息窗口、向前分页游标、滚动触发加载与消息列表一致性要求。

### Modified Capabilities
- `accelerated-large-file-transfer`: 调整极速文件传输对接收端背压、发送窗口、接收确认与失败条件的要求，保证发送端提速时不会因接收端慢而轻易失败。

## Impact

- 受影响后端模块：`backend/internal/transfer`、`backend/internal/app`、`backend/internal/api`、`backend/internal/store`、`backend/internal/config`
- 受影响前端模块：`frontend/src/AppShell.tsx`、`frontend/src/components/ChatPane.tsx`、`frontend/src/lib/api.ts` 及相关测试
- 受影响本地 API：Bootstrap 首屏消息装载语义、历史消息分页查询接口、文件接收目录配置与返回信息
- 受影响系统行为：极速传输稳定性、接收文件最终落点、长会话历史消息加载性能
