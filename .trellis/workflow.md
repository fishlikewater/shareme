# Message Share 开发工作流

> 本流程已按当前 `Wails + Go + React + SQLite` 仓库改写。模板内容若与仓库事实冲突，以 `README.md`、`AGENTS.md`、`.trellis/spec/` 与现有脚本为准。

---

## 目录

1. 快速开始
2. 工作流概览
3. 会话开始流程
4. 开发流程
5. 会话结束
6. 文件说明
7. 最佳实践

---

## 快速开始

### 步骤 0：初始化开发者身份（仅首次需要）

```powershell
python .\.trellis\scripts\get_developer.py
python .\.trellis\scripts\init_developer.py <your-name>
```

会创建：

- `.trellis/.developer`
- `.trellis/workspace/<your-name>/`

### 步骤 1：获取当前上下文

```powershell
python .\.trellis\scripts\get_context.py
python .\.trellis\scripts\task.py list
git status
git log --oneline -10
```

### 步骤 2：阅读项目规范

至少先读：

```powershell
Get-Content AGENTS.md
Get-Content .\.trellis\spec\frontend\index.md
Get-Content .\.trellis\spec\backend\index.md
Get-Content .\.trellis\spec\guides\index.md
```

### 步骤 3：按索引补读细分规范

不要只读固定子集，要以 `index.md` 中列出的文档为准。

---

## 工作流概览

### 核心原则

1. **先读后写**：动手前先理解上下文
2. **遵循规范**：编码前先读 `.trellis/spec/`
3. **规格先行**：行为变更先判断是否进入 OpenSpec
4. **计划后编码**：方案确认后显式执行 `superpowers:writing-plans`
5. **计划活文档**：`docs/superpowers/plans/*.md` 一旦进入执行期，必须同步回写勾选状态、验证结果、review 结论和阻塞信息
6. **增量推进**：一次只推进一个任务
7. **及时记录**：完成后立刻写入 session / journal
8. **控制规模**：单个 journal 文件不超过 `2000` 行
9. **技能泛化**：修改 `SKILL.md` 时保持可迁移，不写当前任务私货

### 职责边界

- `OpenSpec`：管理行为变更的提案、规格、设计与归档
- `superpowers:brainstorming`：补齐方案讨论
- `superpowers:writing-plans`：输出可执行开发计划
- `Trellis`：承接执行、上下文同步、journal、多 Agent 协作

### 目录结构

```text
.trellis/
|-- .developer
|-- scripts/
|-- spec/
|-- tasks/
|-- workspace/
+-- workflow.md

openspec/
|-- config.yaml
|-- changes/
+-- specs/
```

---

## 会话开始流程

### 步骤 1：获取上下文

```powershell
python .\.trellis\scripts\get_context.py
```

### 步骤 2：阅读规范

- 协作总规范：`AGENTS.md`
- 前端 / Web：`frontend/index.md`
- 后端 / 服务端：`backend/index.md`
- 通用思考：`guides/index.md`

### 步骤 3：判断变更级别

- `L0`：文档、测试、重构、脚本或协作流程调整；无用户可见行为、API、数据结构变化，可直接进入 Trellis。
- `L1`：单模块用户行为、错误语义、命令参数、UI 交互变化，先走 `OpenSpec + superpowers + Trellis`。
- `L2`：跨 Wails 桌面、localhost agent、前端 API、SQLite、传输协议、安全或架构边界变化，必须补齐 design 与 review。

### 步骤 4：创建或选择任务

```powershell
python .\.trellis\scripts\task.py list
python .\.trellis\scripts\task.py create "<title>" --slug <task-name>
```

---

## 开发流程

### 行为变更流程（L1 / L2）

```text
1. 创建 OpenSpec 变更
   --> openspec new change <slug>
   --> 初始会生成 openspec/changes/<slug>/.openspec.yaml

2. 使用 superpowers:brainstorming
   --> 补齐或新建 proposal.md
   --> 补齐或新建 specs/.../spec.md
   --> L2 补齐或新建 design.md

3. 校验 OpenSpec
   --> openspec validate --strict --type change <slug>

4. 使用 superpowers:writing-plans
   --> 输出 docs/superpowers/plans/YYYY-MM-DD-<slug>.md
   --> 计划进入执行期后视为活文档，步骤必须使用 checkbox 追踪

5. 回写 OpenSpec tasks
   --> 只保留高层里程碑，不再维护第二套细粒度状态

6. 创建或绑定 Trellis 任务
   --> 将 proposal / spec / design / plan 作为执行上下文

7. 执行实现
   --> 使用 subagent-driven-development 或 executing-plans
   --> 每个步骤完成并通过对应验证后，及时更新计划 checkbox 与当前执行状态

8. 审阅与门禁
   --> L1 / L2 均需完成测试、lint、review
   --> L2 额外要求多 Agent 审阅通过
   --> 将最新验证 / review 结论同步回写到计划与 OpenSpec tasks

9. 完成后归档
   --> 归档前确认计划、Trellis、OpenSpec 的状态一致
   --> openspec archive <slug>
```

### 非行为变更流程（L0）

```text
1. 创建或选择 Trellis 任务
2. 阅读相关规范
3. 给出简短计划与验证方式
4. 编码、自测、提交、记录 session
```

### 通用任务开发流程

```text
1. 判断任务类型
2. 创建或选择任务
3. 阅读规范并补充上下文
4. 实现与自测
5. 提交业务变更
6. 记录 session
```

### 代码质量检查

提交前至少确认：

- lint / format / build / test 按项目要求通过
- 相关规范已同步
- 需要更新的 API / DB / 跨层契约已同步

当前仓库常用验证命令：

```powershell
# 全量 Windows 门禁
powershell -ExecutionPolicy Bypass -File .\scripts\test.ps1

# 后端单元测试
Push-Location backend
go test -count=1 -p 1 ./...
Pop-Location

# 前端测试与构建
Push-Location frontend
npm test
npm run build
Pop-Location

# Windows 桌面/agent 构建与冒烟
powershell -ExecutionPolicy Bypass -File .\scripts\build-desktop.ps1 -Platform windows/amd64
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-desktop.ps1 -SkipBuild
powershell -ExecutionPolicy Bypass -File .\scripts\build-agent.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\smoke-agent.ps1
```

---

## 会话结束

### 记录 Session

```powershell
python .\.trellis\scripts\add_session.py `
  --title "Session Title" `
  --commit "abc1234" `
  --summary "Brief summary"
```

### 结束前检查

- 业务代码按当前会话约定已提交
- `.trellis` 元数据已记录
- 测试与门禁已通过
- 工作区状态清晰

---

## 文件说明

### `workspace/`

记录每个开发者 / Agent 的工作日志。

### `spec/`

沉淀项目开发规范，帮助后续 AI 和开发者保持一致实现。

### `tasks/`

跟踪当前任务及其上下文。

### `openspec/`

管理行为变更的 proposal、spec、design、tasks 和 archive。

---

## 最佳实践

### 应该做

- 开发前先读 `AGENTS.md`、`.trellis/workflow.md`、相关 spec
- 行为变更先补齐 OpenSpec，再写计划
- 让验证命令成为完成定义的一部分
- 修改或新增 `SKILL.md` 时，优先写场景规则、检查点和命令占位，避免写具体项目文件清单
- 追加 skill 内容时跟随原文件语言，避免在同一个 skill 中中英混写
- 把经验沉淀回 spec / guides

### 不应该做

- 跳过 spec 阅读直接编码
- 在多个无关任务之间来回切换
- 未验证就宣称完成
- 保留与当前项目无关的旧模板内容

---

## 新项目接入检查表

- [ ] `AGENTS.md` 已改成项目真实规则
- [ ] `.trellis/spec/` 已改成项目真实技术栈
- [x] `.trellis/config.yaml` 已保留 journal 配置，真实门禁命令写入本 workflow 与 spec
- [ ] `openspec/config.yaml` 已补项目上下文
- [ ] 技能文档与项目提交口径一致
- [ ] 已初始化开发者身份并完成一次 `get_context.py` 自检
