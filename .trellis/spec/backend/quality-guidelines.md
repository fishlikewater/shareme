# 后端质量规范

## 完成定义

- 关键路径有 Go 单元测试或 smoke 覆盖。
- 不破坏 Wails 桌面入口与 localhost agent 兼容入口。
- 不引入明显传输性能退化、数据损坏或安全绕过。
- 涉及跨平台构建时，说明已验证平台与未验证平台。

## 检查项

- 后端单元测试：在 `backend/` 执行 `go test -count=1 -p 1 ./...`。
- Windows 全量门禁：在仓库根执行 `powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`。
- 桌面构建：`powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64`。
- 桌面冒烟：`powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1 -SkipBuild`。
- agent 构建与冒烟：`scripts/build-agent.ps1`、`scripts/smoke-agent.ps1`。

## 禁止模式

- 修改生产代码但不补或不运行对应测试。
- 为通过测试降低断言质量、移除真实错误路径。
- 把 Windows 上的 Go 层交叉编译等同于 macOS/Linux 正式 Wails 产物验证。
