# Message Share 仓库协作规范

## 1. 宗旨

本仓库行 `OpenSpec + superpowers` 协作之法。`superpowers` 主其事，`OpenSpec` 定其文；二者相承，不得并立为二源。

## 2. 分职

- `superpowers` 司探索、澄清、整理需求、起草 `specs`、编制 `plan`、执行开发、评审与验收。
- `OpenSpec` 据 `superpowers` 所成 `specs` 与 `plan`，建正式 `proposal.md`、`design.md`、`specs/**/*.md`、`tasks.md`，并总而治之。
- `docs/superpowers/specs/` 与 `docs/superpowers/plans/` 可为工作稿；正式工件皆归 `openspec/changes/<change>/`。

## 3. 同步

- `OpenSpec` 之 `tasks.md` 与 `superpowers` 之开发 `plan`，须一一相应，次序相同，语义一致。
- 凡任务有增删、拆并、改序，`task` 与 `plan` 须同时更新，不得一新一旧。
- `superpowers` 执行之时，须同步回写 `task` 进度与 `plan` 进度，使文与实常相符。

## 4. 执行

- 无 `active OpenSpec change`，不得入正式编码；活跃变更可用 `openspec list --json` 查之。
- `OpenSpec` 所生 `tasks.md`，乃 `superpowers` 执行之据；开发、排期、拆解，皆不得离此自立。
- 若范围、方案或次序有变，先修 `OpenSpec`，次同步 `plan`，后改代码。

## 5. 评审与验收

- 评审与验收，唯 `tasks.md` 为源；`specs` 定其意，`plan` 述其法，皆不得越而代之。
- `superpowers` 于评审与验收时，须逐项对照 `tasks.md` 核验，并同步回写结果。
- 任务未结、进度未同步、结论未回写者，不得称完成。
