# Codex 上下文窗口、压缩与 Profile 配置

研究快照，2026-11-19。Agent Harbor `main` 基线为 `ad00eea`。运行验证使用 macOS arm64 的原生 Codex **0.169.0** 及当日最新的 **0.170.0**、隔离目录和回环模拟接口。 [Codex 0.169.0](https://github.com/openai/codex/releases/tag/rust-v0.169.0)，[Codex 0.170.0](https://github.com/openai/codex/releases/tag/rust-v0.170.0)。

## 结论

- **单个会话可以设置自己的 context 窗口和自动压缩阈值。** 建议 Harbor 在现有 Codex 启动命令上用 `-c model_context_window=…` 和 `-c model_auto_compact_token_limit=…`。保存到 Harbor 会话记录后，可在恢复同一对话时重新应用。已用 Codex App Server 验证启动和恢复时的窗口覆盖。原生交互式 CLI 的完整 Harbor 启动流程仍待验证。 [Codex CLI 配置覆盖](https://learn.chatgpt.com/docs/config-file/config-basic#configuration-precedence)，[Codex 对话参数](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/app-server-protocol/src/protocol/v2/thread.rs)，[Harbor 启动入口](../../bridge/launch.go)。
- **Profile 可以保存 Codex 默认值。** 在目标 Profile 的环境里启动短生命周期的 Codex App Server，通过 `config/read` 和 `config/batchWrite` 更新它的 `CODEX_HOME/config.toml`。已有两个 Profile 的隔离、版本冲突和批量写入证据。窗口默认值在新加载对话时生效。 [App Server 配置接口](https://learn.chatgpt.com/docs/app-server#config-rpc-examples-for-app-settings)，[Profile 服务契约](../profile-contract.md)。
- **Magpie 可以按供应商模型或路由组设置目录窗口。** 这适合给使用该 Magpie 实例的会话提供模型默认值和路由依据。单个会话要用不同窗口，可用 Codex 启动参数。最终供应商能接受多少 token 由实际模型决定。 [Magpie 模型窗口命令](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/docs/reference.md#L1672-L1735)，[路由组窗口覆盖](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/internal/provider/group.go#L1574-L1590)。

## 现有接入点

Harbor 会话记录保存项目目录、客户端、Profile 绑定、对话引用和连接意图。恢复前按完整绑定解析客户端环境，以保存的引用向 Herdr 的本地开发候选查询恢复命令，再由 Harbor 启动客户端。Harbor 可以在现有启动参数中传入显式设置，同时沿用已经确认的对话和连接。Herdr 恢复查询仍是本地补丁。 [会话记录](../../bridge/store.go)，[启动与恢复](../../bridge/launch.go)，[Herdr 集成与补丁状态](../herdr-integration.md)。

| 用户要改的东西 | 现有存储或工具 |
| --- | --- |
| Harbor Profile 的名称、关联环境、默认 Magpie 实例 | `profiles.json` 保存 Harbor 自己的元数据。CLI 已有 `profiles rename` 和 `profiles default`。默认 Magpie 实例用于后续新建会话。 [Profile 服务契约](../profile-contract.md)，[Profile 命令](../../bridge/profile_commands.go)。 |
| Profile 内 Codex 的默认设置 | 目标 `CODEX_HOME/config.toml`。隔离 Profile 有独立 Codex home、SQLite 和运行目录。Existing Profile 指向用户已经拥有且可能共享的环境。用 `ResolveProfileClient` 和 `WithProfileClient` 按捕获的绑定解析并锁定。 [Profile 实现](../../bridge/profiles.go)，[根目录与配置校验](../../bridge/profile_runtime.go)。 |
| 单个 Codex 进程或对话的启动覆盖值 | CLI `-c key=value`，或 App Server 对话创建、恢复的 `config` 参数。Codex 自身还可用 `--profile NAME` 叠加同一 home 里的 `NAME.config.toml`。Harbor 的部分 Magpie 会话已经用这种配置层保存每会话的模型选择。 [Codex 配置层](https://learn.chatgpt.com/docs/config-file/config-basic#configuration-precedence)，[现有 Magpie 会话配置层](../../bridge/magpie_codex.go)，[启动组合](../../bridge/launch.go)。 |
| 按项目目录共享的配置 | 可信项目的 `.codex/config.toml`。同一项目目录可被多个 Harbor Profile 使用，按项目保存可能影响该目录下的多个 Profile。 [Codex 配置优先级](https://learn.chatgpt.com/docs/config-file/config-basic#configuration-precedence)，[Harbor 产品说明](../spec.md)。 |

Codex 配置优先级中，CLI 覆盖值高于可信项目，可信项目高于 `--profile` 配置层和用户配置。托管要求另行约束允许的值。配置页面需要给出保存值和指定项目下的生效值，并显示覆盖来源。启动路径也要继续保留 Harbor 的隔离设置和刚确认的连接。 [Codex 配置优先级](https://learn.chatgpt.com/docs/config-file/config-basic#configuration-precedence)，[配置层读取协议](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L389-L410)，[Harbor 启动参数](../../bridge/launch.go)。

## 设置窗口与压缩阈值

`model_context_window` 是 Codex 按模型使用的上下文窗口配置，`model_auto_compact_token_limit` 控制自动压缩的起点。0.169.0 的模型配置将窗口覆盖值限制在目录的 `max_context_window` 内（如果目录提供），另按模型百分比预留空间。常规窗口计算会把自动压缩阈值限制在解析窗口的 90% 内。界面应分别显示填写值与实际可用窗口，读 `thread/tokenUsage/updated` 的 `modelContextWindow` 验证后者。 [Codex 配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)，[0.169.0 窗口覆盖逻辑](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/models-manager/src/model_info.rs#L19-L30)，[可用窗口和压缩计算](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/protocol/src/openai_models.rs#L567-L592)。

`model_post_turn_compact_threshold_percent` 是另一个可选的百分比设置。Codex 完成回复后，若占用达到可用窗口的指定比例，就可以压缩历史。取值 `0–100`，`0` 关闭这项回合结束检查，常规 token 阈值仍会生效。这里的百分比是触发时的窗口占用，压缩后保留多少由 Codex 决定。0.169.0 和 0.170.0 都支持此键，可用 CLI `-c` 按会话传入，也可写入 Profile 的 `config.toml`。 [Codex 配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)，[0.169.0 的配置字段](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/config/src/config_toml.rs#L176-L192)，[0.170.0 的百分比校验](https://github.com/openai/codex/blob/rust-v0.170.0/codex-rs/core/src/config/mod.rs#L3371-L3376)。

| 设置所在位置 | 作用范围 |
| --- | --- |
| **Magpie 供应商模型** | 实例内按 `provider/model` 设置的目录窗口与路由依据。`magpie model context` 和 `magpie model spec` 也可设置 `provider/*` 默认值。共享实例的 Profile 会共用这些值。供应商实际接收的容量由该模型及其服务决定。 [Magpie 模型配置](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/docs/reference.md#L1672-L1735)。 |
| **Magpie 路由组** | 按组决定对客户端声明的窗口，默认从成员中取最大的已知窗口，也可设成指定值或跟随最小成员。长请求仍需要选到有足够容量的成员。Harbor 的 API-only 路径用 Magpie 的 Codex 格式目录。 [路由组窗口推导](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/internal/provider/group.go#L1476-L1491)，[路由组窗口覆盖](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/internal/provider/group.go#L1574-L1590)，[Harbor 的目录桥接](../../bridge/magpie_codex.go)。 |
| **Codex 启动参数或对话参数** | `-c model_context_window=…` 给本次启动配置覆盖值，App Server `thread/start.config` 或 `thread/resume.config` 可以只覆盖一个对话。相同模型的两个对话可以有不同的 Codex 窗口，覆盖值仍受所用模型目录声明的上限约束。 [Codex 配置优先级](https://learn.chatgpt.com/docs/config-file/config-basic#configuration-precedence)，[Codex 窗口限制](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/models-manager/src/model_info.rs#L19-L30)。 |
| **Codex Profile 默认配置** | 目标 `CODEX_HOME/config.toml` 的窗口和压缩阈值作为该 Profile 的默认设置，已加载对话的行为见下方试验。完全沿用模型目录时，切换模型或路由组可能改变会话所用的窗口。窗口大小和压缩起点可分别设置。 [Magpie 的 Codex 目录窗口和压缩规则](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/docs/subsystems/agent-wiring.md#L26)，[Profile 作用域](../profile-contract.md)。 |

可以用 Codex 的 `/status` 查看当前 token 用量，用 `/statusline` 在底栏展示 context 统计。使用 App Server 时，可用 `thread/tokenUsage/updated` 读取已加载对话的可用窗口。要主动释放当前占用，可以用 Codex 的 `/compact` 压缩历史。Herdr 可以承载这些终端操作，窗口与自动压缩阈值由 Codex 启动配置和模型目录决定。 [Codex 状态与压缩命令](https://learn.chatgpt.com/docs/developer-commands?surface=cli#inspect-the-session-with-status)，[Codex 窗口配置](https://learn.chatgpt.com/docs/config-file/config-reference)，[Herdr 集成](../herdr-integration.md)。

Magpie 返回给客户端的目录会参与 Codex 的配置解析。Harbor 的 API-only 路径显式加载 Magpie 的目录，joined 路径从 Codex 的 OpenAI provider 入口连接 Magpie。窗口的端到端验证应分别覆盖这两条路径和混合窗口的路由组。 [API-only 目录加载](../../bridge/launch.go)，[joined 启动配置](../../bridge/codex_joined.go)，[Magpie Codex 窗口与压缩规则](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/docs/subsystems/agent-wiring.md#L26)。

使用 Codex 自己的 ChatGPT 登录时，原生连接从 OpenAI 获取模型目录。Harbor 的 joined 连接经过 Magpie 获取合并目录，但打包基线的官方窗口覆盖从 Magpie 已发现的 `codex` 供应商读取。上游测试在 Magpie 能发现该登录时覆盖了官方窗口。Harbor 的 caller-only 补丁关闭这项登录发现，因此仅在 Harbor 的 Codex Profile 登录时，不应依赖 `magpie model context codex/<model>` 来改官方窗口。推荐沿用官方目录默认值，或由 Codex 按会话传入 `model_context_window` 和压缩设置。会话内切换官方模型时，启动覆盖是该会话共用的值，按模型自动应用不同覆盖仍需单独设计。该判断来自当前两条路径的实现，待用打包版验证目录与窗口。 [Magpie 合并目录](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/internal/gateway/codex_backend.go#L2323-L2351)，[官方窗口来源](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/internal/provider/codex_models.go#L797-L814)，[官方窗口测试](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/internal/gateway/codex_native_context_test.go#L20-L32)，[Harbor caller-only 集成](../magpie-caller-integration.md)。

## 可参考的仓库

- **CC Switch** 有 Codex 窗口和压缩阈值的配置界面，也有每模型目录写入示例。它维护自己的客户端目录选择和多文件配置事务，适合借鉴界面和字段处理。直接交给它写会额外引入配置所有权和账号文件的协调问题。 [CC Switch 配置表单](https://github.com/farion1231/cc-switch/blob/b044f4b03190b8208e7e592d326609e7f2fbe097/src/components/providers/forms/CodexConfigSections.tsx#L177-L215)，[CC Switch Codex 写入与目录源码](https://github.com/farion1231/cc-switch/blob/b044f4b03190b8208e7e592d326609e7f2fbe097/src-tauri/src/codex_config.rs#L268-L282)，[模型目录窗口实现](https://github.com/farion1231/cc-switch/blob/b044f4b03190b8208e7e592d326609e7f2fbe097/src-tauri/src/codex_config.rs#L2830-L2866)。
- **ai-profiles** 的 macOS 启动包装器通过 `CODEX_HOME` 选择配置、凭证和会话，展示了相同的原生隔离入口。它拥有自己的目录和迁移逻辑。Harbor 已经能按稳定 Profile 绑定解析同一类入口。 [ai-profiles 支持的客户端与隔离](https://github.com/bartekczyz/ai-profiles/blob/6a9bb6bb65d36ee72e5ed7c1c8a225e3aa42215e/README.md#L35-L51)，[Harbor 环境解析](../../bridge/profiles.go)。

## 建议的接入方式

以下是能力接入建议。针对“新建单个会话时调参数”的需求，优先采用下一节的最小范围，Profile 编辑与更多压缩选项可按后续需求增加。

1. **单会话设置走现有 Codex 启动参数。** 用类型明确的可选字段保存窗口与压缩阈值，由 Harbor 在新建和恢复时传给 Codex。恢复时沿用已确认的对话 ID、Profile 和连接。界面展示用户填写的值与实际可用窗口，防止把模型目录上限或压缩起点理解成同一个数字。需要给正在运行的会话应用新值时，先确认它是否须重新加载，再提供相应的恢复操作。 [Harbor 启动与锁](../../bridge/launch.go)，[产品恢复说明](../spec.md)，[Codex 窗口计算](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/protocol/src/openai_models.rs#L567-L592)。
2. **Profile 默认值走短生命周期的配置 RPC。** 在拥有 Profile 的服务器上解析并锁定稳定绑定。先支持窗口与压缩阈值，使用 stdio App Server 读用户层版本，以 `expectedVersion` 批量保存，省略由调用者提供的 `filePath`。核对响应路径并回读对应字段，遇到版本冲突时请求刷新。已有共享环境需要明确显示受影响的范围。 [Harbor Profile 作用域](../../bridge/profiles.go)，[Profile 策略校验](../../bridge/profile_runtime.go)，[配置写入协议](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/app-server-protocol/src/protocol/v2/config.rs#L1175-L1214)。
3. **Magpie 继续负责模型和路由组容量。** 使用实例自己的模型或路由组配置设置目录默认值。端到端试验应核查 Codex 显示的窗口、压缩触发的 token 用量、Magpie 选择的成员和最后供应商接受的容量。 [Magpie 模型配置](https://github.com/yetone/magpie/blob/5426c43cca9b90be809d51edebc5c926929e9f6c/docs/reference.md#L1672-L1735)，[Harbor 验收指南](../../bridge/tests/README.md)。

## 单会话参数的长期维护风险与最小范围

**建议先提供少量可选的会话启动参数，首次使用时继承模型与 Profile 的配置，后续新建会话预填上次应用的选项。** 新建会话时显式填写 `model_context_window`，由 Harbor 传给 Codex。确有需求时再增加 `model_auto_compact_token_limit`，回合结束压缩百分比可后续补充。前文列出的可接入能力不代表第一版需要全部实现。这是基于已验证接口的实现建议，Harbor 产品入口尚未实现。

| 范围 | 长期维护影响 |
| --- | --- |
| 透传少量 Codex 官方配置键 | 相对较低，主要维护参数校验、配置优先级与 Codex 版本兼容。窗口解析和压缩继续由 Codex 执行。 |
| 保存覆盖值并在恢复时重新应用 | 增加会话状态维护，需要让旧记录缺省为继承配置，并覆盖新建、恢复及失败重试路径。 |
| 按模型自动匹配窗口和压缩参数 | 明显扩大范围，需要处理模型别名、目录更新和 Magpie 路由组成员变化。 |
| 同时开放 token 阈值和多种百分比策略 | 增加字段解释、版本适配和实际压缩行为的验证成本。百分比表示触发时机，压缩后保留多少由 Codex 决定。 |

上述维护判断依据当前启动、恢复与会话存储实现，以及 Codex 的配置解析和压缩行为。已有原生探针验证可复用，完整 Harbor 路径仍需验收。 [会话记录](../../bridge/store.go)，[启动与恢复](../../bridge/launch.go)，[Codex 配置优先级](https://learn.chatgpt.com/docs/config-file/config-basic#configuration-precedence)，[Codex 窗口与压缩计算](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/protocol/src/openai_models.rs#L567-L592)。

当前会话记录已经保存 Profile、对话和连接，但没有上下文覆盖字段或通用启动参数字段。可以复用现有存储与启动流程，增加类型明确的可选字段，在新建和恢复时应用同一份设置。具体改动量需要结合全部启动分支评估。只在首次启动传入、恢复后丢失，会让同一会话的行为发生难以察觉的变化。 [会话数据结构](../../bridge/store.go)，[参数组装和恢复命令](../../bridge/launch.go)。

现有存储已有会话文件锁与原子写入，恢复审阅也会校验整条会话记录，可以复用这些机制。启动参数还会复制给实验性的 App Server，新增覆盖值需同时进入客户端和该服务器的参数组装，避免两者采用不同设置。 [会话存储事务](../../bridge/store.go)，[恢复审阅校验](../../bridge/session_safety.go)，[客户端与 App Server 启动](../../bridge/launch.go)。

显式窗口值在同一会话切换模型时继续适用，并受实际模型能力及目录上限约束。默认继承模型配置可以沿用模型自己的窗口。若要求切换模型后自动重新匹配覆盖值，则需要额外设计。单会话调参本身可复用 Codex 的配置入口，官方模型和 Magpie 接入模型均需沿实际启动路径验证。为这一需求接管模型目录或迁移官方账号管理，会扩大实现与维护范围。 [Codex 窗口覆盖逻辑](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/models-manager/src/model_info.rs#L19-L30)，[Harbor 连接与账号边界](../spec.md)，[Magpie caller-only 集成](../magpie-caller-integration.md)。

## 推荐开放的单会话参数

**建议常用设置先开放窗口大小、执行权限、审批方式和推理强度四项。** 首次使用全部继承，后续预填上次应用的选项，只有显式设置的字段生成启动覆盖。Codex 原生已有专用 CLI 选项和通用 `-c key=value` 接口，Harbor 需要实现选择、保存和恢复应用。现有启动流程已经使用这些配置入口，但尚无上述通用编辑入口。 [Codex CLI 参数](https://learn.chatgpt.com/docs/cli/reference)，[单次运行配置](https://learn.chatgpt.com/docs/developer-settings)，[Harbor 启动实现](../../bridge/launch.go)，[会话记录](../../bridge/store.go)。

| 常用设置 | 原生参数或配置键 | 建议提供的选择 | 使用场景与说明 |
| --- | --- | --- | --- |
| 上下文窗口 | `-c model_context_window=128000` | 继承，或填写 token 数 | 为单个会话覆盖 Codex 的窗口计算。沿用前文的模型上限和实际可用窗口说明。 [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)。 |
| 执行权限 | `--sandbox`，配置键为 `sandbox_mode` | 继承、`read-only`、`workspace-write`、`danger-full-access` | 分别用于只读任务、工作区内开发和显式选择的完全访问。CLI 提供这三个原生取值，文件和网络边界由 Codex 执行。 [CLI 参数](https://learn.chatgpt.com/docs/cli/reference)，[沙箱与审批](https://learn.chatgpt.com/docs/agent-approvals-security)。 |
| 审批方式 | `--ask-for-approval`，配置键为 `approval_policy` | 继承、`on-request`、`never` | 控制是否在需要时提出审批。`never` 时沙箱边界仍然生效，执行权限需单独选择。若继承了自动审批审阅配置，审批请求可能由自动审阅处理。 [沙箱与审批](https://learn.chatgpt.com/docs/agent-approvals-security)。 |
| 推理强度 | `-c model_reasoning_effort='"high"'` | 继承，或当前模型公开支持的档位 | 按模型提供选择，例如 `medium`、`high`。可用档位依赖模型与客户端，避免给所有模型固定同一份选项。Harbor 已有模型及推理强度的读取逻辑可参考。 [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)，[现有模型选择](../../bridge/codex_selection.go)。 |

以下三项作为高级选项候选。自动压缩 token 阈值与额外可写目录可在有需求时加入，百分比继续后置。

| 高级设置 | 原生入口 | 建议处理 |
| --- | --- | --- |
| 自动压缩 token 阈值 | `-c model_auto_compact_token_limit=100000` | 默认继承。需要时填写 token 数，实际触发仍受 Codex 的窗口与阈值计算约束。 [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)，[窗口与压缩计算](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/protocol/src/openai_models.rs#L567-L592)。 |
| 额外可写目录 | 可重复的 `--add-dir /absolute/path`；配置键为 `sandbox_workspace_write.writable_roots` | 适合同时修改多个仓库。路径以会话所在服务器为准，保存为明确的路径列表。配置中的额外写入根目录作用于 `workspace-write` 模式。 [CLI 参数](https://learn.chatgpt.com/docs/cli/reference)，[配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)。 |
| 回合结束压缩百分比 | `-c model_post_turn_compact_threshold_percent=80` | 默认继承，后续再开放。`0–100` 表示触发比例，`0` 关闭这项检查，普通 token 压缩限制仍然适用。已有原生探针记录见下文。 [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)。 |

“继承”应保存为空值。显式填写的数字 `0` 需要保留原本语义，例如回合结束压缩检查的关闭值。恢复时重新应用用户保存的覆盖值，省略的设置继续按 Codex 的配置与恢复规则解析。CLI 原生选项和 `-c` 仍受托管要求约束。权限覆盖也需要遵守 Codex 的组合规则，例如 beta 的 `default_permissions` 配置不能与 `sandbox_mode` 或 `sandbox_workspace_write` 同时使用。 [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)，[单次运行与配置层](https://learn.chatgpt.com/docs/developer-settings)，[会话启动与恢复](../../bridge/launch.go)。

下面是原生 Codex 的组合示例，假定所选模型支持 `high`。它展示未来 Harbor 可以生成的命令，产品入口尚未实现。 [CLI 参数](https://learn.chatgpt.com/docs/cli/reference)，[推理强度配置](https://learn.chatgpt.com/docs/config-file/config-reference)。

```sh
codex \
  --sandbox workspace-write \
  --ask-for-approval on-request \
  -c model_context_window=128000 \
  -c model_reasoning_effort='"high"'
```

模型继续复用现有选择入口。连接地址、供应商和凭证存储由现有 Profile 与连接意图流程负责。建议用明确字段提供上述设置，开放任意启动参数会增加参数冲突、优先级和恢复行为的维护范围。 [产品边界](../spec.md)，[Profile 启动约束](../../bridge/profiles.go)，[现有参数组合](../../bridge/launch.go)。

## Options 页面与上次选项记忆

**用户要求 Options 记住上次选项。** 布局建议是在新建会话一级页的 `Model` 后、`Name` 前增加一行 `Options`，进入二级页调整参数。现有一级页有五个字段，直接加入六项参数会变成十一行。保留一个入口能减少新建时经过的字段，同时提供当前设置摘要。该方案尚未实现。 [现有新建页](../../bridge/app_new_ui.go)，[TUI 一致性规范](../agents/tui.md)。

以下为布局示意，采用现有单列字段与固定底栏，不增加装饰框。

```text
宽屏                                    60 列窄屏
Project      /Users/.../workspace    >   Project     .../workspace    >
Client       codex                  >   Client      codex            >
Connection   Magpie                 >   Connection  Magpie           >
Model        Selected model         >   Model       Selected model   >
Options      Inherited              >   Options     Inherited        >
Name         Automatic              >   Name        Automatic        >
```

一级页摘要显示本次草稿的设置。全部继承时显示 `Inherited`，有覆盖值时优先显示执行权限和审批，再按剩余宽度显示窗口与推理强度，例如 `Workspace write · On request · 128k`。80×24 与 60 列下保持单列，摘要截短，完整值在二级页查看。正文溢出时沿用共享滚动和底栏规则。 [字段布局与窄屏规则](../agents/tui.md)。

```text
Harbor > New session > Options

Sandbox          Inherited                 >
Approval         Inherited                 >
Reasoning        Inherited                 >
Context window   Inherited                 >
Auto compact     Inherited                 >
Writable dirs    Inherited                 >

^S Apply    ^R Reset to inherited    Esc Cancel
↑↓/jk fields    Enter edit
```

二级页直接列出常用设置与已开放的高级设置，保持同页编辑。百分比仍按前文建议后置。`Ctrl+S` 应用并返回新建页，`Esc` 放弃本页未应用的修改并返回，返回后焦点留在 `Options`。`Ctrl+R` 将本页选项重置为继承，应用后更新草稿与记忆。创建会话继续在一级页执行。以上为拟定交互，复用现有字段、编辑与快捷键控件。 [TUI 控件与交互规范](../agents/tui.md)。

除“记住上次选项”为用户明确要求，以下记忆范围与保存时机是本次建议，供实现时采用。

| 场景 | 建议行为 |
| --- | --- |
| 记忆范围 | 在拥有工作区的服务器上，按稳定的 Profile、客户端及其环境绑定分别保存。切换 Profile 或客户端时加载对应记忆。当前同一 Profile/client 的 Native 与 Magpie 新建会话共用这些选项。 |
| 首次使用 | 没有保存记录时全部继承。 |
| 应用选项 | `Apply` 保存完整选项，包括继承状态和已开放的高级项，同时更新本次新建草稿。保存后退出并重新打开 Harbor 仍可预填。 |
| 取消编辑或新建 | `Esc` 放弃本页未应用的修改。已应用的最近选项持续保留，取消随后的一次新建不会撤销它。 |
| 创建与恢复 | 新建会话复制当前草稿的选项到会话记录。已有会话恢复时使用自己的记录，上次新建选项的后续变化仅供下一次新建预填。 |
| 继承与显式值 | 保存用户的选择。继承状态保持为空，显式值按原值保存，例如窗口数值和有特殊语义的 `0`。 |
| 模型或环境变化 | 新建和恢复时校验已保存的值与当前客户端、模型及路径是否兼容。不兼容时显示具体字段供用户调整。 |

记忆功能可参考当前按完整 Profile 绑定记住连接选择的交互与隔离方式。Options 还需要持久化自己的完整选项，具体存储设计待实现时确定。会话保存仍可复用已有事务和恢复审阅机制。 [现有连接记忆](../../bridge/app_form.go)，[Profile 隔离回归](../../bridge/app_contract_test.go)，[会话存储](../../bridge/store.go)，[恢复审阅](../../bridge/session_safety.go)。

## 原生验证记录

探针使用临时 HOME、XDG 路径、Codex home/SQLite 目录、模拟密钥和只监听回环地址的 Responses 服务，通过 stdio 执行 App Server JSON-RPC。A/B 写入与同一对话恢复时的 12,000→24,000 窗口覆盖在 0.169.0、0.170.0 上都通过。0.170.0 测试显式使用 legacy 历史，0.169.0 也跑了默认模式，未核验默认模式实际选用的存储格式。100,000→200,000 的热加载测试只运行了 0.169.0。仓库原有 Profile 测试也会写入 100 万窗口和 90 万压缩阈值的配置，覆盖的是 A/B 对话、凭证和状态隔离。 [仓库 Profile 原生测试](../../bridge/profile_native_test.go)，[隔离验收指南](../../bridge/tests/README.md)。

| 验证 | 实际结果 |
| --- | --- |
| Profile 写配置 | `config/read` 返回用户层版本。省略 `filePath` 的 `config/value/write` 只写 A 的 `config.toml`，保留测试注释。过期版本被拒绝。新的版本可批量保存两个字段。B 的文件按字节相同，有效配置也相同。0.170.0 的空 Profile 试验中，读取返回版本且不落盘，带该版本保存时只创建了 A 的文件。 |
| 单对话启动与恢复覆盖 | `gpt-5.5` 下，默认窗口 100,000 时报告可用 95,000。用 `thread/start.config` 设置 150,000 时报告 142,500，底层文件仍为 100,000。另一轮中，重启后对同一 ID 用 `thread/resume.config` 把窗口从 12,000 改到 24,000，报告从 11,400 变为 22,800，对话 ID 保持相同。 |
| 写入后的已加载对话 | 用 `config/batchWrite` 保存窗口 200,000、压缩阈值 180,000 并启用 `reloadUserConfig`。已加载的两个对话仍报告 95,000 和 142,500，新对话报告 190,000。请求很短，没有触发实际自动压缩。 |
| 百分比触发压缩 | 原生 Codex 0.170.0 的 `codex exec` 用 `-c` 传入窗口 12,000、常规压缩阈值 10,000，以及回合结束压缩阈值 `40`、`70` 或 `0`。模拟服务让首轮报告 6,000 tokens，只有 `40` 触发一次压缩请求。三次运行各有独立的临时目录，`config.toml` 均保持字节一致。 |

完整 Harbor 交互式 CLI 的新建和恢复、两种 Magpie 连接与路由组、真实供应商下的压缩行为、Linux 和远端仍待验证。Harbor 的会话及 Profile 编辑入口仍待接入。

本次推荐参数另用原生 Codex 0.170.0 的 `--help` 与 `resume --help` 核对。两者都公开 `--sandbox`、`--ask-for-approval`、`--add-dir` 和 `-c`；审批的专用 CLI 选项公开 `on-request`、`never`。本次核对没有执行真实权限操作或验证这些参数在 Harbor 全部恢复路径中的效果。 [官方 CLI 参考](https://learn.chatgpt.com/docs/cli/reference)。
