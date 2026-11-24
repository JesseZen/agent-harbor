# Extensions 交接记录

Harbor 的 Extensions 首页管理 Skills。`b` 打开 Plugins 占位页，页内 `m` 打开 Marketplaces 占位页，`Esc` 逐级返回。两个占位页只注册导航动作。原来的插件查询、安装、移除、市场预设和跨 Profile 同步都已从 Harbor 源码移除。用户已有的 Codex 插件、市场配置和缓存由 Codex 管理。Herdr 的 Harbor 插件注册属于启动和终端集成。 [产品规格](spec.md)

## 旧实现与遇到的问题

| 功能 | Milestone B / B.1 怎么实现 | 实机发现 |
|---|---|---|
| Skills 列表 | B 在同一次 Extensions 加载里扫描共享目录和 Profile 目录、启动 Codex App Server、读取插件和市场。B.1 先渲染本地快照，再补 Codex Skills。 | B 的慢查询把本地列表一起挡住。两条共享目录记录只能说明这次扫到了两个 Skill。Codex 内置、项目和其他生效范围需要有效的 RPC 查询与工作目录。 |
| Skills 搜索和安装 | B 用 `tea.ExecProcess` 接 `skills find`。`a` 留空进搜索，非空直接交给 `skills add`。B.1 把目录搜索改为 JSON，Harbor 自己渲染结果，安装仍调用 Skills CLI。 | 关键词被误送给 `add`，错误随子进程退出快速闪过。交还终端后回到 Extensions，搜索上下文消失。Discover 输入框还被全局 Vim 导航提前转换了 `hjkl`。 |
| Plugins | B 通过 Profile 环境调用 `codex plugin list --json`，把已装项混在 Extensions 首页，并提供手填安装、卸载、`y` 跨 Profile 同步。B.1 增加 `list --available --json`，把可安装项作为另一组塞回首页。 | 空数组用例没有触发非空 JSON 的兼容问题。`r` 后出现插件查询错误，页面仍显示 0，让人误以为查到了空目录。 |
| Marketplaces | B 读取市场给同步流程使用。B.1 增加市场页面、Git/local 增删升级，以及 `openai-primary-runtime`、`openai-bundled` 两条合成预设。尝试从市场回到首页并定位可安装插件。 | 用户在显示为「官方市场」的列表按 Enter 没看到可安装目录。显示市场名称、Codex 实际发现的市场、当前凭证可访问的插件目录是三件事。用户从选市场到装插件的流程没有走通。 |

Milestone B 的原始代码可用 `git show a80e1d2:bridge/extensions_codex.go` 和 `git show a80e1d2:bridge/app_extensions_ui.go` 查看。B.1 的历史验证主要覆盖了解析和合成数据下的 UI。它没有验证从真实市场进入目录、安装后在同一个 Profile 中看到插件的完整路径。 [原始实施规格](step2-session-assets-ui-spec.md) · [历史走查](ui-flow-audit.md)

### 非空 JSON 兼容问题

前序实测使用 Codex `0.169.0`。插件行的关键字段是 `pluginId` 和 `marketplaceName`。`source` 与 `marketplaceSource` 可以是对象，如 `{"sourceType":"git","source":"https://example.test/team.git"}`。市场行也可能有嵌套的 `source`，其 `sourceType` 可区分 Git、local 与 bundled。旧解析预期 `id`、`marketplace` 和字符串来源，遇到实机行会丢来源或整批解析失败。以下片段是按当时的非空字段整理的合成样例，外层结构需要按目标 Codex 版本重新抓取。

```json
{
  "pluginId": "reviewer",
  "marketplaceName": "team",
  "source": {"sourceType": "git", "source": "https://example.test/team.git"},
  "marketplaceSource": {"sourceType": "git", "source": "https://example.test/team.git"}
}
```

前序实测中，远程市场返回 401 时，CLI 在 stderr 告警，stdout 仍返回 Git/local 市场的目录。调用层应分别收集 stdout、stderr 和退出状态。解析错误、远程部分不可用、真实空目录需要不同的状态。安装应使用目标版本的 CLI 确认过的标识，测试两个市场有同名插件的情况。保留 `pluginId` 原值和市场身份，展示用名称单独处理。 [Codex CLI 源码](https://github.com/openai/codex)

这次用原生 Codex `0.172.0` 在隔离环境重验了本地市场。市场清单放在 `.agents/plugins/marketplace.json`，插件清单放在插件目录的 `.codex-plugin/plugin.json`。`plugin list --available --json` 顶层同时返回 `installed` 和 `available`，`available` 中的完整 `pluginId` 是 `reviewer@harbor-fixture`，可直接传给 `plugin add`。`plugin marketplace list --json` 顶层是 `marketplaces`，市场行的来源位于 `marketplaceSource`。CLI 的市场添加、安装、回读、卸载和移除市场都通过。本地与内置市场的探测要根据各自的源字段处理。 [OpenAI 市场清单样例](https://github.com/openai/plugins/blob/main/.agents/plugins/marketplace.json)

## 当前 Skills 路径

- 本地扫描读取 `~/.agents/skills/*/SKILL.md` 和 `$CODEX_HOME/skills/*/SKILL.md`，用规范化路径给软链去重，从当前 Profile 的 `[[skills.config]]` 读取启用状态。首页拿到这份结果就可操作，异步合并 `skills/list`。RPC 失败时仍保有本地列表。`Space` 在 Profile 配置锁内优先走 `skills/config/write`，回退路径保留 TOML 中的其他内容。 [本地扫描](../bridge/extensions.go) · [Codex 服务](../bridge/extensions_codex.go)
- 已读快照按 Profile 保留在当前弹窗模型 30 秒。返回时可先显示旧数据，`r` 和写操作完成后重新加载。关掉 Herdr 弹窗会销毁这个进程内缓存。重开仍要做本地扫描，但首屏无需等待 Codex RPC 或目录网络请求。先分别量本地扫描、RPC、在线目录和子进程耗时，再决定是否需要跨进程缓存。 [页面和快照](../bridge/app_extensions_ui.go)
- `d` 聚焦 Discover，`a` 输入关键词或仓库来源。关键词走 `https://skills.sh/api/search?q=...&limit=50`，仓库路径或 URL 走 `skills add`。上游 `skills find` 是面向终端的人机命令，其 `find.ts` 自己用同一个 JSON 入口。上游公开了 `skills ls --json` 和 `skills add ... --json -y`。目前 Harbor 直接用目录 JSON 响应，依赖 `skills`、`id`、`source`、`skillId`、`name` 和 `installs` 字段。把它当作上游兼容面，保留非空响应、空结果、HTTP 失败及字段解析回归。 [Skills CLI 文档](https://github.com/vercel-labs/skills#readme) · [上游搜索实现](https://github.com/vercel-labs/skills/blob/main/src/find.ts)
- Discover 用 Enter 提交搜索，方向键或 `jk` 选结果，`a` 安装。CLI 退出后仍在 Discover，结果留在列表，完成或失败在页面上反馈。直接来源安装、更新和卸载走 Skills CLI。CLI 解析顺序为显式 `AGENT_HARBOR_SKILLS_BIN`、伴生目录、`PATH` 上的 `skills`、`npx --yes skills`。输入焦点应在全局方向键转换之前判断。 [快捷键与输入路由](../bridge/app_feedback.go) · [Skills 接入](../bridge/extensions_codex.go)
- `skills add <source> --skill <id> -g -a codex --json -y` 的 `-g` 面向共享安装。做真实安装验收时，分开检查共享存储、CLI 实际写入的 Codex 链接以及两个 Profile 各自的启用状态。目录命中和子进程退出状态各自有用，持久安装以回读为准。 [Skills 全局安装说明](https://github.com/vercel-labs/skills#readme)

## 建议的插件重做顺序

1. **走通一个市场、一个插件。** 在独立 worktree 和临时 Profile 中创建一个带有非空目录的本地市场。对目标 Codex 检查 `plugin list --help`、`plugin add --help`、`plugin marketplace --help`，留存脱敏的已装和可用 JSON。先走完 `Extensions → Plugins → 可安装目录 → 选插件 → 安装结果 → 已装列表`。从 Marketplaces 进入时，所选市场直接成为目录过滤条件，`Esc` 回到原市场行。用同一个确定的 Profile 校验安装前后的结果。
2. **每页按需加载。** Skills 首页只查 Skills。Plugins 页面打开后立即展示自己的缓存，各自读取已装与可安装列表。市场页打开后才列市场。为远程目录、Git/local 目录和已装列表保留各自的可用性。按 Profile 和查询范围缓存，丢弃过期或跨 Profile 的异步消息。已装或市场发生变更时，刷新受影响的目录和状态。
3. **官方市场来自 Codex。** 用户想开箱就能找到默认官方目录，优先用当前版本、当前 Profile 的真实有效市场和可安装列表。验证无预配置的新 Profile 和登录态不同的 Profile。如果该版本需要登录或明确启用官方目录，就在市场行展示实际状态和对应动作。OpenAI 维护的 `openai/plugins` 市场清单声明了 `openai-curated`。如果需要 Git 来源预设，可从这里验证可访问性与版本兼容，也要确认与 Codex 内置或登录后的远程目录是否重复。只读标记和可升级能力来自 Codex 的源类型或版本接口。Git/local 自定义市场保留从来源添加、Git 升级、确认后移除的独立流程。 [OpenAI 插件库](https://github.com/openai/plugins)
4. **把外部工具错误留在页面上。** 有机器接口的查询和非交互操作放异步命令，stdout 供解析，失败时显示脱敏诊断或可重试状态。需要 Codex 登录、交互批准或外部输入时才交接终端。安装期间锁定提交目标，成功后回读，失败保留原过滤条件和选择。复用 `uiPage`、共享 footer、文本输入与列表控件。 [TUI 契约](agents/tui.md)
5. **最后再做跨 Profile 同步。** 先取源和目标的已装 ID 与用户 Git/local 市场定义，预览增量，确认后按市场、插件的次序重放。确认时锁定并重新解析目标 Profile，重试时重新计算已有项，逐项报告成功和失败。跳过内置市场的添加步骤，目标已识别对应市场时再处理其插件定义。只重放安装来源和标识，账号、历史、OAuth、插件缓存和软链接都留在原来的环境。 [Profile 契约](profile-contract.md)

## 验证与接手

- **本轮 Harbor 验证。** macOS arm64 上用 Herdr `0.9.5`、Codex `0.172.0` 和独立 tmux 实跑了外层 160×45、100×30、80×24、60×24。Plugins 和 Marketplaces 双向返回正常，旧操作键无插件动作，测试 Profile 配置字节一致。Skills 读取了共享和 Profile 目录，Codex 补充项也进入列表。`hjklHJKL` 保留为输入，查找、安装交接、失败信息、刷新和重开弹窗都走过，Sessions 和 Connections 导航回归通过。13 张 ANSI 终端图用 Unicode 15 重渲染。搜索和安装交接分别用了本地 HTTP 与 CLI 桩，实际线上 Skills 安装和 Linux/远端弹窗仍待验证。
- 定向 Go 竞态测试和 `go vet` 通过。隔离全量竞态测试与原始 `main` 基线 `a80e1d2` 都复现了 `TestRecoverSavedReferenceApprovalSurvivesFailedLaunch` 和 `TestStatusDoesNotWaitForUnresponsiveGateway`。普通全量测试复现了前一项。
- 测试中先覆盖非空多市场 JSON、同名插件、旧字段和对象来源、stderr 401 与成功 stdout 并存、账号受限、版本不支持、加载过期、安装失败后重试、切 Profile 后迟到结果。跑一次真实 `add → list → remove → list`，随后用真实 Herdr/tmux 从键盘和鼠标走通主路径。记录外层及弹窗实际尺寸、命令版本、实际生效的 Profile 与配置副作用。 [隔离验收](../bridge/tests/README.md)
- 当前本机确认 Codex `0.172.0` 的原生命令帮助仍有 `plugin list/add/remove` 和 `plugin marketplace list/add/upgrade/remove`。`skills.sh` 的公开目录请求返回了上述非空字段。当前仓库的 UI 只有插件占位，真实插件管理留待新实现验收。
- 新 agent 从包含这次清理的基线开独立 worktree，沿 Plugins 占位入口接入功能。源码入口在 `bridge/app_extensions_ui.go`，Skills 服务在 `bridge/extensions_codex.go`，两者是否共享服务接口等到实际插件契约确定后再决定。建议先读 `tui-design`，遇到版本兼容或状态回归时用 `diagnosing-bugs` 按最小可复现路径验证。
