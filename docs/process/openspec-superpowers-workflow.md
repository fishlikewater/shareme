# OpenSpec 与 superpowers 协作流程

## 1. 一句话原则

`OpenSpec 管需求和计划，superpowers 管执行和验收。`

如果把两者混在一起，最常见的问题是：

- 需求在聊天里改了，但规格没改
- superpowers 写了自己的计划，和 OpenSpec 的任务清单分叉
- 开发完成后，没有统一验收基线

这个流程文档就是为了解决这三个问题。

## 2. 角色分工

### 2.1 OpenSpec 负责什么

OpenSpec 是正式变更管理层，负责：

- 为什么做：`proposal.md`
- 怎么设计：`design.md`
- 系统必须做到什么：`specs/**/*.md`
- 具体要做哪些事：`tasks.md`

### 2.2 superpowers 负责什么

superpowers 是执行流程层，负责：

- 探索问题空间
- 制定和执行实现步骤
- 推动测试、调试、评审
- 组织多 agent 并行开发或 review
- 维护执行期计划状态，包括 checkbox、当前执行状态、验证结果、review 结论和阻塞信息

superpowers 不负责替代 OpenSpec 管理正式变更文档。

## 3. 标准时序

### 阶段 1：探索

适用技能：

- `brainstorming`
- `openspec-explore`

输出目标：

- 明确问题、边界、非目标、完成定义

结果要求：

- 如果需求进入正式开发，必须转入 OpenSpec change

### 阶段 2：建模

适用技能：

- `openspec-propose`

输出目标：

- 创建 `openspec/changes/<change>/proposal.md`
- 创建 `openspec/changes/<change>/design.md`
- 创建 `openspec/changes/<change>/specs/**/*.md`
- 创建 `openspec/changes/<change>/tasks.md`

结果要求：

- `tasks.md` 未就绪前，不进入正式实现

### 阶段 3：实施

适用技能：

- `test-driven-development`
- `subagent-driven-development`
- `writing-plans`

执行规则：

- 以 `tasks.md` 为唯一任务清单
- 以 `specs/**/*.md` 为验收基线
- 代码中途发现新约束或新范围，先回写 OpenSpec，再继续实现
- 如果使用 `docs/superpowers/plans/*.md` 承接实现步骤，该计划在执行期必须视为活文档
- 已完成且验证通过的步骤应及时从 `- [ ]` 改为 `- [x]`
- 未完成、被阻塞或尚未验证的步骤不得提前勾选，应在计划中补充原因或当前状态

### 阶段 4：调试

适用技能：

- `systematic-debugging`

执行规则：

- bug 修复不能只改代码，不改规格
- 如果 bug 暴露的是需求缺口或状态机缺口，要同步修订 OpenSpec

### 阶段 5：验收

适用技能：

- `verification-before-completion`
- `requesting-code-review`

执行规则：

- 先验证，再宣称完成
- review 结论必须回到 OpenSpec 语义上判断，而不是只看“代码像是对的”
- 宣称完成前，必须确认计划文档、OpenSpec `tasks.md`、Trellis 任务状态与实际验证证据一致

## 4. 仓库内落点约定

### 正式工件

- `openspec/changes/<change>/proposal.md`
- `openspec/changes/<change>/design.md`
- `openspec/changes/<change>/specs/**/*.md`
- `openspec/changes/<change>/tasks.md`

### 辅助工件

- `docs/superpowers/specs/`
- `docs/superpowers/plans/`

辅助工件只允许用于：

- 历史遗留
- 临时草稿
- 中间推演材料

辅助工件不能替代 OpenSpec 正式工件。

### 执行期计划文档

`docs/superpowers/plans/*.md` 不是正式需求来源，但只要某个计划进入执行期，就必须承担执行状态记录职责。

执行期计划至少应保持：

- 使用 checkbox 语法记录步骤状态
- 有最新的当前执行状态、验证结果、review 结论或阻塞说明
- 已完成步骤必须有对应实现或验证证据
- 未完成步骤不得因为代码局部可用而提前勾选
- 完成前必须核对计划状态、OpenSpec `tasks.md` 和 Trellis 任务状态不冲突

## 5. 对话里的推荐口令

### 5.1 新需求

可以直接说：

- “先探索，不要实现”
- “先用 OpenSpec 建 change”
- “把这次需求补齐 proposal/design/specs/tasks”

### 5.2 开始开发

可以直接说：

- “按这个 OpenSpec change 开始实施”
- “以 tasks.md 为准持续推进”
- “先不要另写一套计划文档”

### 5.3 中途改需求

可以直接说：

- “先更新 OpenSpec，再继续改代码”
- “把这个新约束写回 specs 和 design”

### 5.4 完成前

可以直接说：

- “按 specs 场景验收”
- “做多 agent review，通过后才算完成”

## 6. 当前仓库示例

当前已经按该模式纳管的变更：

- `modernize-workbench-ui`

对应目录：

- [proposal.md](../../openspec/changes/modernize-workbench-ui/proposal.md)
- [design.md](../../openspec/changes/modernize-workbench-ui/design.md)
- [tasks.md](../../openspec/changes/modernize-workbench-ui/tasks.md)

这就是后续“OpenSpec 管需求和计划，superpowers 负责实施”的基准示例。
