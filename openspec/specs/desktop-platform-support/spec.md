# desktop-platform-support Specification

## Purpose
TBD - created by archiving change upgrade-to-wails-cross-platform-runtime. Update Purpose after archive.
## Requirements
### Requirement: 系统必须在 Windows、macOS 和 Linux 上提供正式桌面运行支持

系统 MUST 将 Windows、macOS 和 Linux 视为正式支持的桌面平台。对于任一正式支持平台，系统 MUST 提供一致的核心能力闭环，包括应用启动、设备发现、配对、文本发送、文件选择以及文件收发。

#### Scenario: 任一正式支持平台都能完成主界面启动
- **WHEN** 用户在 Windows、macOS 或 Linux 上启动正式桌面构建
- **THEN** 系统必须能够完成应用初始化并显示可用主界面

#### Scenario: 任一正式支持平台都能完成本地文件选择与发送
- **WHEN** 用户在任一正式支持平台上选择本地文件并发起发送
- **THEN** 系统必须能够通过该平台的桌面运行时完成本地文件选择，并进入统一的发送流程

### Requirement: 系统必须提供面向三端的一致构建与分发语义

系统 MUST 提供面向 Windows、macOS 和 Linux 的正式桌面构建入口，并输出适用于目标平台的桌面产物。目标产物启动后 MUST 直接进入桌面应用体验，而不得要求额外手动启动本地 Web UI 服务。

#### Scenario: 目标平台构建产物可作为独立桌面应用启动
- **WHEN** 为任一正式支持平台生成发布构建
- **THEN** 构建产物必须能够作为该平台的独立桌面应用启动，并直接加载 shareme 主界面

#### Scenario: 三端构建共享一致的目录与运行语义
- **WHEN** 不同正式支持平台上的构建产物启动并初始化运行环境
- **THEN** 系统必须保持一致的用户根目录语义、桌面主入口语义与本地文件交互模型，仅允许最小必要的平台差异

### Requirement: 系统必须使用平台原生能力处理关键桌面交互

系统 MUST 在 Windows、macOS 和 Linux 上使用对应平台可用的原生桌面能力处理关键交互，包括用户主目录解析、本地文件选择以及桌面窗口承载，而不得依赖仅适用于单一平台的脚本式实现作为正式方案。

#### Scenario: 文件选择使用平台原生交互
- **WHEN** 用户在任一正式支持平台上点击选择文件
- **THEN** 系统必须调用该平台可用的原生文件选择交互，而不是依赖仅适用于 Windows 的脚本式对话框实现

#### Scenario: 用户目录解析遵循统一语义
- **WHEN** 系统在任一正式支持平台上初始化默认目录
- **THEN** 系统必须解析当前用户主目录，并在其下应用统一的 `.shareme` 目录语义
