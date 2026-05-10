# 编码前检查表

## 最小检查

- 是否已判定 `L0/L1/L2`？
- 若为 `L1/L2`，是否已有 active OpenSpec change 与 `tasks.md`？
- 是否读过 `AGENTS.md`、`.trellis/workflow.md` 与相关 `.trellis/spec/`？
- 是否搜索过既有 Go/React 实现、测试与 OpenSpec 历史？
- 是否知道本次最小验证命令？

## shareme 特别检查

- 是否影响 Wails 桌面入口、localhost agent，或两者兼有？
- 是否影响 `~/.shareme` 数据布局、`SHAREME_DATA_DIR` 覆盖行为或旧数据迁移？
- 是否影响配对、指纹、可信设备、loopback 安全边界？
- 是否影响大文件/极速发送路径与进度事件？

## 如果答案是否

先补齐事实与规格，不靠猜测推进。
