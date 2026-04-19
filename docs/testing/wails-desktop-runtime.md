# Wails Desktop Runtime 验证记录

## 默认目录

- 配置文件：`~/.message-share/config.json`
- 设备身份：`~/.message-share/local-device.json`
- 数据库：`~/.message-share/message-share.db`
- 日志目录：`~/.message-share/logs`
- 临时目录：`~/.message-share/tmp`
- 回退下载目录：`~/.message-share/downloads`

## Windows

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1 -SkipBuild
```

## macOS

```bash
./scripts/build-desktop.sh darwin/universal
./scripts/smoke-desktop.sh darwin/universal
```

## Linux

```bash
./scripts/build-desktop.sh linux/amd64
./scripts/smoke-desktop.sh linux/amd64
```

## 2026-04-19 Fresh 验证结果

以下结果来自 2026-04-19 当前工作区的最新实跑：

```powershell
Set-Location backend
$env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'
$env:GOTELEMETRY='off'
go test -count=1 -p 1 ./...
```

结果：

- 后端全量测试通过。

```powershell
Set-Location ..\frontend
$env:npm_config_cache='E:\Projects\IdeaProjects\person\message-share\.cache\npm'
npm test
npm run build
```

结果：

- 前端 `10` 个测试文件、`45` 条用例通过。
- 前端生产构建通过，并输出到 `backend/frontend/dist`。

```powershell
Set-Location ..\backend
$env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'
$env:GOTELEMETRY='off'
go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -clean -platform windows/amd64
```

结果：

- Wails `windows/amd64` 构建通过。
- 生成交付文件 `backend/build/bin/message-share.exe`。

```powershell
Set-Location ..
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1 -SkipBuild
powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1
```

结果：

- Windows smoke 通过，输出 `Desktop smoke passed: app started, runtime dir initialized, and main UI reported ready.`。
- `scripts/test.ps1` 通过；该脚本当前汇总的是 Windows 路径上的后端测试、前端测试、Wails Windows 构建、Windows smoke，以及 `linux/amd64`、`darwin/amd64`、`darwin/arm64` 的静态编译校验。
- `scripts/test.ps1` 在执行后端 `go test ./...` 与跨平台 `go build .` 前，会确保 `backend/frontend/index.html` 这份编译兜底页存在，避免在前端 `dist` 尚未生成时因 embed 资源缺失而导致 fresh 验证失真。

```powershell
Set-Location .\backend
$env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'
$env:GOTELEMETRY='off'
$env:GOOS='linux'
$env:GOARCH='amd64'
go test -c -o 'E:\Projects\IdeaProjects\person\message-share\.tmp\cross-build\config-linux-amd64.test' ./internal/config
go build -o 'E:\Projects\IdeaProjects\person\message-share\.tmp\cross-build\message-share-linux-amd64' .

$env:GOOS='darwin'
$env:GOARCH='amd64'
go test -c -o 'E:\Projects\IdeaProjects\person\message-share\.tmp\cross-build\config-darwin-amd64.test' ./internal/config
go build -o 'E:\Projects\IdeaProjects\person\message-share\.tmp\cross-build\message-share-darwin-amd64' .

$env:GOARCH='arm64'
go test -c -o 'E:\Projects\IdeaProjects\person\message-share\.tmp\cross-build\config-darwin-arm64.test' ./internal/config
go build -o 'E:\Projects\IdeaProjects\person\message-share\.tmp\cross-build\message-share-darwin-arm64' .
```

结果：

- 上述 `linux/amd64`、`darwin/amd64`、`darwin/arm64` 的静态编译校验均通过，说明当前 Go 宿主与配置路径逻辑在目标平台代码层可编译。

```powershell
Set-Location .\backend
$env:GOCACHE='E:\Projects\IdeaProjects\person\message-share\.cache\go-build'
$env:GOTELEMETRY='off'
go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -clean -platform linux/amd64
go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build -clean -platform darwin/amd64
```

结果：

- Wails CLI 在 Windows 主机上返回 `Crosscompiling to Linux not currently supported.` 与 `Crosscompiling to Mac not currently supported.`。
- 因此，Linux / macOS 的 Wails 正式桌面产物构建与 smoke 仍需在对应目标平台，或具备受支持交叉构建能力的环境中执行上面的分平台命令完成实机验证。

## 验收口径

- Wails 构建成功并生成对应平台产物。
- `smoke-desktop` 会从非源码目录启动桌面产物，并以 `config.json` 与 `MESSAGE_SHARE_UI_READY_MARKER` 同时落地作为通过条件。
- `smoke-desktop` 会为每次验证注入隔离端口，并校验 UI ready marker 中记录的实际运行时端口与注入值一致，避免被本机常驻实例或历史进程占用默认端口而干扰结果。
- 桌面应用主界面完成加载后，前端会调用宿主 `UiReady()`，由后端写出包含 ready 时间与实际端口信息的 UI ready marker。
- `smoke-desktop` 在 marker 出现后还会执行一个短暂稳定窗口检查，避免“刚 ready 就立刻退出”的瞬时假阳性。
- 运行目录会在用户主目录下或显式覆盖目录下初始化 `config.json`。
- 当前 Windows 环境的 `scripts/test.ps1` 已纳入 Linux / macOS 静态编译校验，但无法替代目标平台上的 Wails 真机构建与 smoke。
