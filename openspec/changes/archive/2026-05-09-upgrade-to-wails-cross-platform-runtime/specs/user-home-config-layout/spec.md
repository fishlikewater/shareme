## ADDED Requirements

### Requirement: 系统必须在用户主目录下初始化统一的 `.message-share` 根目录

系统 MUST 在当前用户主目录下使用 `.message-share` 作为默认配置与运行数据根目录。该根目录 MUST 统一承载配置文件、设备身份、数据库、回退下载目录、日志与临时文件，并在首次启动时自动创建缺失目录。

#### Scenario: 首次启动时自动创建标准目录布局
- **WHEN** 用户首次启动应用，且用户主目录下不存在 `.message-share`
- **THEN** 系统必须创建 `.message-share` 及其所需的配置、身份、数据库、下载回退、日志与临时目录布局

#### Scenario: 后续启动时继续复用同一根目录
- **WHEN** 用户再次启动应用
- **THEN** 系统必须继续使用同一 `.message-share` 根目录，而不得重新切换到其他默认目录

### Requirement: 系统必须将生成的配置文件保存到 `.message-share` 并保留用户修改

系统 MUST 将生成的主配置文件保存到 `.message-share/config.json`。系统在启动时 MUST 仅为缺失文件或缺失字段补齐默认值，而不得覆盖用户已经明确修改并保存的配置项，包括设备名称等用户可编辑内容。

#### Scenario: 首次启动生成默认配置文件
- **WHEN** `.message-share/config.json` 不存在
- **THEN** 系统必须在 `.message-share` 下生成默认配置文件

#### Scenario: 用户修改配置后重启仍被保留
- **WHEN** 用户修改 `.message-share/config.json` 中的设备名称等可编辑字段并重新启动应用
- **THEN** 系统必须继续使用用户保存的值，而不得在启动时将其重写为默认值

### Requirement: 系统必须安全迁移旧默认目录到新的用户主目录布局

系统 MUST 在检测到旧默认目录存在历史数据且新的 `.message-share` 目录尚未初始化时，执行一次性迁移。迁移 MUST 优先保留用户数据完整性；若新目录已经存在有效数据，系统 MUST 以新目录为准并避免覆盖。

#### Scenario: 旧默认目录存在历史数据时执行迁移
- **WHEN** 系统首次按新规则启动，`.message-share` 尚未初始化，且旧默认目录中存在配置、身份或数据库文件
- **THEN** 系统必须将可迁移数据导入 `.message-share`，并使用户继续看到原有设备身份和历史状态

#### Scenario: 新目录已经存在时不覆盖用户现有内容
- **WHEN** `.message-share` 已存在有效配置或数据，且旧默认目录中也存在历史文件
- **THEN** 系统必须以 `.message-share` 为准，并且不得用旧目录内容覆盖新目录中的用户修改
