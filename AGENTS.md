# 协作约定

请与用户明确指令、`README.md`、`docs/process/`、`.trellis/workflow.md`、`.trellis/spec/` 和 `openspec/` 工件合并使用；如有冲突，以用户明确指令和当前项目事实为准。

## 0. 项目定制位

- 项目名称：`shareme`
- 项目形态：局域网点对点消息与文件传输桌面应用
- 正式入口：`Wails + Go + React` 桌面应用
- 兼容入口：`backend/cmd/shareme-agent` 提供仅限本机 loopback 访问的 localhost Web UI
- 后端技术栈：Go 1.25、Wails v2.12、SQLite、局域网发现、点对点 HTTP/加速传输
- 前端技术栈：React 18、TypeScript、Vite、Vitest、Testing Library
- 运行数据目录：默认 `~/.shareme`，可由 `SHAREME_DATA_DIR` 覆盖
- 主要开发命令：Windows 执行 `powershell -ExecutionPolicy Bypass -File .\scripts\dev-desktop.ps1`
- 主要测试命令：Windows 执行 `powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1`
- 后端聚焦测试：在 `backend/` 执行 `go test -count=1 -p 1 ./...`
- 前端聚焦测试：在 `frontend/` 执行 `npm test`；涉及构建时执行 `npm run build`
- 提交策略：未经用户明确要求，AI 不主动提交 git commit；`.trellis` 元数据优先由脚本维护
- 文档语言：中文
- Skill 语言：修改已有 `SKILL.md` 时，新增内容跟随原文件语言，不做中英混写

---

## 1. 编码前先思考

**不要想当然，不要掩饰困惑，要把假设和取舍摆到台面上。**

- 先明确写出自己的假设；不确定时就提问。
- 如果需求存在多种解释，先把几种理解列出来，不要静默替用户做决定。
- 如果存在更简单的做法，要主动指出。
- 需要时可以温和地提出异议，不盲从执行。
- 一旦发现信息不清、边界模糊或描述矛盾，先停下来，说明困惑点并澄清。
- 进入项目后先识别技术栈、目录边界、现有测试命令、OpenSpec active change 与 Trellis 当前任务。

## 2. 简单优先

**用最少的代码解决问题，不做投机式设计。**

- 不添加用户没有要求的功能。
- 不为一次性代码提前做抽象。
- 不加入未被要求的“灵活性”“可配置化”“通用化”。
- 不为明显不可能发生的场景堆砌错误处理。
- 如果写了很多代码，但更少代码能清楚解决问题，就应继续简化。
- 对本仓而言，Wails 桌面入口与 localhost agent 入口的差异应收敛在适配层，不复制业务逻辑。

## 3. 外科手术式改动

**只改必须改的地方，只清理由自己改动带来的问题。**

- 不顺手重构无关模块。
- 不为“顺便优化”扩大改动面。
- 尽量贴合项目现有结构、命名和风格。
- 只删除因为本次修改而成为孤儿的代码。
- 若工作区已有无关未跟踪或未提交文件，不要擅自移动、删除或回滚。
- 影响 Go snapshot、TypeScript 类型、Wails binding、localhost API、SQLite schema 或传输协议时，必须同步检查上下游。

## 4. 以目标驱动执行

**先定义成功标准，再循环验证直到目标达成。**

把任务改写成可验证目标：

- “修 bug” -> “先复现，再补回归验证，再修复”
- “加能力” -> “先明确输入输出，再实现，再验证”
- “重构” -> “先确认行为基线，再保证改前改后一致”

多步骤任务建议使用：

```text
1. [步骤] -> 验证：[检查项]
2. [步骤] -> 验证：[检查项]
3. [步骤] -> 验证：[检查项]
```

常用验证口径：

```powershell
# Windows 全量门禁
powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1

# 后端聚焦验证
Push-Location backend
go test -count=1 -p 1 ./...
Pop-Location

# 前端聚焦验证
Push-Location frontend
npm test
npm run build
Pop-Location
```

<!-- TRELLIS:START -->
# Trellis Instructions

以下说明供在本项目中工作的 AI 助手使用。

开始新会话时，使用 `/trellis:start`：
- 初始化开发者身份
- 理解当前项目上下文
- 阅读相关规范

使用 `@/.trellis/` 了解：
- 开发工作流（`workflow.md`）
- 项目结构规范（`spec/`）
- 开发者工作区（`workspace/`）

保留此管理块，使 `trellis update` 可刷新说明。

<!-- TRELLIS:END -->
