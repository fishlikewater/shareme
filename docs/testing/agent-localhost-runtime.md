# Agent Localhost Runtime 验证

本文档用于验证 `backend/cmd/shareme-agent` 这条“无窗口 runtime + localhost Web UI 兼容壳”入口。

## 验证目标

- `shareme-agent` 能正常启动并初始化运行目录
- `http://127.0.0.1:<port>/api/bootstrap` 可访问
- `http://127.0.0.1:<port>/` 可访问
- localhost 入口只作为本机浏览器兼容壳，不替代 Wails 桌面正式入口

## Windows

构建：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1
```

冒烟：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-agent.ps1
```

## macOS / Linux

构建：

```bash
./scripts/build-agent.sh
```

冒烟：

```bash
./scripts/smoke-agent.sh
```

## 手动检查项

1. 启动 `shareme-agent`
2. 打开启动日志中的 `http://127.0.0.1:<port>/`
3. 确认设备列表、配对、文字发送、普通文件发送、极速发送入口都能显示
4. 访问 `http://127.0.0.1:<port>/api/bootstrap`，确认返回 JSON 快照
5. 确认局域网其他机器无法直接访问该 localhost 入口
