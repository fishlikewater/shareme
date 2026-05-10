# 后端数据库规范

## 当前事实

- 数据库：SQLite，驱动为 `modernc.org/sqlite`。
- 打开入口：`backend/internal/store/sqlite.go` 的 `Open`。
- 默认路径：`~/.shareme/shareme.db`，由 `config.ResolveLayout` 间接确定。
- 初始化方式：`Open` 中执行 `create table if not exists` 与幂等 `alter table`。
- 现有表：`local_device`、`trusted_peers`、`conversations`、`messages`、`transfers`。
- 旧运行数据迁移：`backend/internal/config/migration.go`，迁移配置、身份文件与数据库文件；旧目录与旧数据库名只作为迁移来源。

## 变更规则

- 新增表、字段、索引时，同步更新 `sqlite.go`、相关查询方法和 `sqlite_test.go`。
- 变更运行数据布局时，同步检查 `config/layout.go`、`config/migration.go` 与相关测试。
- schema 变更必须幂等，旧数据库重复启动不能失败。
- 需要补历史数据时，优先写可重复执行的 SQL 或 Go 迁移逻辑，并覆盖旧数据场景测试。
- 业务层不得直接拼接不受控 SQL；查询封装留在 `internal/store`。
