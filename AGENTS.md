# Message Share 仓库协作规范

## 1. 目标

本仓库采用 `OpenSpec + superpowers` 协作模式：

- `OpenSpec` 负责基于提案产出和管理正式变更工件。
- `superpowers` 负责以正式工件为输入开展探索、规划、开发与状态同步。

二者不是两套并行体系，而是“`OpenSpec` 管正式文档，`superpowers` 管执行过程”。

## 2. `OpenSpec` 职责

- `OpenSpec` 应根据提案，在 `openspec/changes/<change>/` 下产出 `proposal.md`、`design.md`、`specs/**/*.md`、`tasks.md` 等相关文件。
- 上述文件构成该 change 的正式文档与任务基线。
- 凡需求、设计、规格、任务发生正式变更，均应优先更新对应 `OpenSpec` 工件。

## 3. `superpowers` 职责

- `superpowers` 的 `brainstorming` 与 `writing-plans`必须以对应 `OpenSpec` 工件为输入，不得脱离正式工件另起一套范围或计划。
- `superpowers` 在进入开发前，必须检查并对齐开发计划与 `OpenSpec` 的 `tasks.md`。
- `superpowers` 在执行过程中，必须同步更新开发计划状态与 `OpenSpec` 任务状态，保证两边一致。

## 4. 对齐与同步规则

- `superpowers` 的开发计划应与 `OpenSpec` 的 `tasks.md` 在任务粒度、执行顺序和完成状态上保持一致。
- 若 `tasks.md` 有增删、拆分、合并、重排或状态变更，开发计划必须同步调整。
- 若开发计划有实质性变更，必须同时检查并更新 `OpenSpec` 的 `tasks.md`，不得出现一边已改、一边仍旧的情况。
- 若发现两边不一致，应先完成对齐，再继续开发、评审或验收。

## 5. 执行约束

- 无 `active OpenSpec change` 时，不允许进入正式编码实现。
- `OpenSpec` 的 `tasks.md` 是 `superpowers` 执行、跟踪和验收的直接依据。
- `brainstorming` 与 `writing-plans` 的输出，应持续回写并反映到 `OpenSpec` 工件与开发计划中。
- 当前活跃变更使用 `openspec list --json` 查看。
