# Agent Harbor 第二步规格与实施文档：会话绑定模型升级、Sessions 页面重构与统一资产管理（Skills / Plugins）

> 扩展管理章节记录 Milestone B 的原始设计。当前功能以[产品规格](spec.md)为准，实机发现和重做建议见[Extensions 交接记录](extensions-handoff.md)。

- **状态**：已通过 `/grill-me` 全量决策对齐，待获批后按 Milestone A → B 顺序实施
- **基线仓库**：`agent-harbor`（`main`，含第一步已验证的 23 项未提交改动）
- **实测底层前提**：Codex CLI `0.169.0`（及 `0.170.0`）、Herdr `0.9.5`（本地恢复查询候选）、Magpie `0.1.3528-harbor-dev`（含 `caller-only` + `profile-isolation` 补丁）、`codex-auth 0.4.0`

---

## 1. 核心设计决策总览

以下决策经 `/grill-me` 逐条敲定，是本规格的唯一事实来源（Single Source of Truth）：

| 编号 | 主题 | 已敲定决策 |
|---|---|---|
| D1 | 统一资产池 vs Profile 隔离 | 日常默认走**统一单资产池**（所有常用账号共用同一份会话历史、Skills、Plugins，`Accounts` 纯做凭证切换层）；多 Profile 保留为高级隔离能力，不破坏原有强隔离契约。 |
| D2 | 默认资产池落盘与单 Profile 降噪 | 沿用 `EnsureProfiles()` 规则：已有 `~/.codex` 数据或旧记录时默认直连 `Existing environment`（与外部 Codex 互通）；全新环境创建 Harbor 托管隔离 `Default`。**只有 1 个 Profile 时弱化 UI 存在感**，不引导用户靠新建 Profile 来切账号。 |
| D3 | Skills & Plugins 管理深度与跨 Profile 复用 | Harbor TUI 新增轻管理入口：查看已装 Skills/Plugins、按当前 Profile 开关启用/禁用、调外部 CLI 搜索/安装/更新/卸载，并支持**跨 Profile 一键同步 Plugins 清单**；Skills 采用单源共享（`~/.agents/skills/`）+ 按 Profile 开关（`[[skills.config]]`），适配未来多 CLI 生态。 |
| D4 | `skills` CLI 集成与打包边界 | **分层解耦 + 交互交接**：列表读取与按 Profile 开关用纯 Go / Codex App Server RPC 实现（零 Node 依赖）；搜索/安装/更新/卸载按优先级调用伴生二进制 → 本机 `skills` → `npx skills`，通过 `tea.ExecProcess` 交接给交互式 CLI 后刷新。预留 companion 路径以便后续按需捆绑，不破坏现有无 Node 安装器。 |
| D5 | Harbor Session ↔ Codex 对话语义 | Harbor Session 定义为**受管工作区窗口/连接槽位**。只要来自已核实的前台运行（同 `RunToken` / `Pane` / `SocketIdentity` / `ProcessGroup` / `Profile`），窗口内 `/resume`、`/agents`、`/new` 切到新对话并上报合法 hook 时，Harbor **自动跟随最新对话**为当前恢复目标（历史切换轨迹保留在 Details），不再标 `conflict`。 |
| D6 | 自动标题、双活保护、恢复确认与旧记录迁移 | 未手动改名（`AutoNameSync`）的会话随最新对话自动更新名称与 Herdr 标签；两**同时运行**窗口撞同 UUID 仍标冲突并阻断重复恢复；正常 `ready` 停止会话按 Enter 保留轻量一次确认页（去掉刺眼黄字）；升级前已存在的旧 `conflict` 记录保留一次显式二选一，不静默覆盖。 |
| D7 | Sessions 列表分组与扩展接口 | 复用 Accounts 页的灰色分组横条（`button` 底色）做物理视觉分区，底层抽象可扩展的 `sessionGrouper` 接口（为未来按目录、连接、Profile、本地/远端、自定义 Tag 分组预留）；本期内置默认分组：**Active（运行中受管） / Stopped（已停止历史） / External（外部窗口，如 `agy`）**，空分组自动隐藏。 |
| D8 | Sessions 列表行密度与视觉降噪 | 受管记录固定**紧凑 2 行**（彻底移除第 3 行黄字警报）：主行 = `> 名称` + Herdr 语义状态（可选简短状态码如 `Review` / `No ID` / `Conflict`）；副行 = 用克制淡色区分的 `项目名` `客户端` `连接`（仅多 Profile 时显示短 Profile 名）。外部窗口仅显示低调灰色 `External` 标签，不挂黄字告警。详细告警与证据统一收进宽屏右栏预览与 `i` Details。 |
| D9 | 顶层导航重排与 Profiles 归位 | 顶栏保持 5 个高频 Tab：**`🚢 Sessions` · `🧩 Extensions` · `🐦 Connections` · `👤 Accounts` · `⚙ Settings`**。`Profiles` 收进 `Settings`（同时保留快捷键 `p` 直达；仅存在多个 Profile 时在顶栏标题和 `Accounts` 显示当前 Profile）。 |
| D10 | `--no-daemon` 回退窗口切号边界 | **不做自动关窗重开**。保持当前诚实上报（Accounts 结果页区分已生效与未生效窗口，提示按需手动关闭再 Enter 恢复），不引入对补丁版 Codex 的强依赖。 |

---

## 2. 不可破坏的既有契约与第一步成果

实施过程中必须完整保留以下已验证边界，严禁回退或绕过：

1. **第一步已完成改动（保持完好）**：
   - `--remote` 会话权限覆盖走 `$CODEX_HOME/agent-harbor-<session-id>.config.toml`（带 `# Agent Harbor session <id>` 归属标记）+ `--profile`，项目 `.codex/config.toml` 或自定义 profile 冲突时回退 `--no-daemon`（`bridge/codex_options_runtime.go`、`bridge/launch.go`）。
   - 静默恢复（Codex 0.169+ 启动 `resume` 不重发 `SessionStart`）时保留已确认启动基线，不将 `observed` 标脏为 `missing`/`needs_review`（`bridge/identity_candidate.go`、`bridge/launch.go`）。
   - `historical_exit` / `confirmed_exit` 显示灰色 `Stopped`，`not_started` 显示灰色 `Not started`（`bridge/agent_visibility.go`、`bridge/app_style.go`）。
   - 冲突选择页未选行前 `recoveryCursor == -1`，Enter 不写入（`bridge/app_enter_recovery.go`）。
2. **Profile 隔离契约（`docs/profile-contract.md`）**：
   - 隔离 Profile 的 `Home` / `SQLite` / `Runtime` 根路径保持 canonical 非软链、模式 `0700`、互不重叠（`ErrProfileAlias`）；`validateProfilePolicy` 继续禁止重定向 `sqlite_home`、`log_dir` 或切离 `file` 凭证后端。
   - 所有 Profile 变更/配置读写继续走 `WithProfileClient` 跨进程锁，UI 异步结果继续过 `ValidateProfileResult(captured, visible)` 校验。
3. **Magpie 边界（`docs/magpie-caller-integration.md` + `docs/magpie-profile-isolation.patch`）**：
   - Magpie 继续只负责供应商配置、模型目录与网关路由；`MAGPIE_CODEX_CALLER_ONLY=1` 下继续禁止 Magpie 枚举 Codex 目录和写删登录文件。Harbor 与 Magpie 不互相抢夺客户端资产所有权。
4. **TUI 一致性契约（`docs/agents/tui.md`）**：
   - 复用 `uiPage`、统一深灰表面与调色板、`uiRowStyle`（键盘 `>` + 悬浮底色 + 已选色）、固定最多两行底栏快捷键、比例滚动条与四尺寸（160×45 / 100×30 / 80×24 / 60×24）适配。
   - **文档同步项**：随 D9 落地，更新 `docs/agents/tui.md` 中“Profile emoji 仅出现在顶层 tab”一条，改为反映 `Profiles` 移入 `Settings` 后的实际规则。

---

## 3. Milestone A：会话绑定模型升级、Sessions 页面重构与 `--no-daemon` 边界

### A1. Harbor Session ↔ Codex 对话绑定模型（工作区窗口槽位）

#### 1. 语义与状态机

Harbor Session 代表一个**绑定了固定 Profile、工作目录、客户端、连接意图与启动选项的受管窗口槽位**。在一个已核实的前台运行期内，用户在 Codex 内通过 `/resume`、`/agents`、`/new` 切换到另一条主对话并触发 Herdr 官方 hook 时，这是正常的窗口内工作流，而非错误冲突。

**自动跟随（Auto-Follow）必须同时满足全部安全前置条件**：
1. **运行归属已核实**：`s.RunToken == token`，`validContext == true`，`socketIdentity(socket) == socketID`，且 `!recordChanged`（pane/socket 未变）。
2. **基线干净**：`!baselineUnknown && !blocked`（排除启动前 pane 已残留未知旧 ID 或曾经出现传输/关联丢失的运行）。
3. **上报合法**：`ref != nil && ref.Source != "" && ref.Agent == s.CLI && ref.Kind != "" && ref.Value != ""`（对 Codex 需满足 `ref.Kind == "id" && validCodexID(ref.Value)`）。
4. **当前记录本身未处于历史未决冲突**：若升级前遗留记录已是 `Status == "conflict"` 或 `ReportedCodexID != ""` 且本次运行尚未经显式审阅解除，则保持原样，不自动覆盖。
5. **无双活并发冲突**：遍历 `sessions.json` 中其他记录 `other`（`other.ID != s.ID`），若存在另一个**正在运行或启动中**（`processGroupActive(other.ProcessGroup) || sessionLaunchPending(other)`）的同 Profile/CLI 记录已指向同一个 `ref`（`other.Conversation.Reference == *ref` 或 `other.CodexSessionID == ref.Value`）：
   - **禁止静默跟随**；保持原保存目标不变，将当前记录标为 `Status = "conflict"`、`Recovery = "needs_review"`、`ReportedCodexID = ref.Value`，并设置明确的双活冲突提示（如 `"Conversation is already active in another session."`）。

**满足以上 5 条时执行平滑切换**：
1. 若 `s.Conversation != nil && s.Conversation.Reference != *ref`：
   - 将原 `s.Conversation.Reference` 追加到 `s.Conversation.History`（见下节数据结构，按时间正序保留，单条记录上限 32 条，超出裁剪最旧项，保证 `sessions.json` 有界）。
   - 更新 `s.Conversation.Reference = *ref`、`s.Conversation.ObservedReference = ref`、`s.Conversation.ObservedAt = time.Now().UTC()`、`s.Conversation.RunToken = token`、`s.Conversation.Pane = pane`、`s.Conversation.SocketIdentity = socketID`、`s.Conversation.Status = "observed"`、`s.Conversation.Provenance = "herdr-pane-observation"`。
   - 对 Codex 同步更新 `s.CodexSessionID = ref.Value`、`s.ConversationIDSource = "herdr-hook"`。
   - 清空 `s.ReportedCodexID = ""`、`s.IdentityWarning = ""`、`s.CandidateIDs = nil`、`s.Candidates = nil`，保持 `s.Recovery = "ready"`。
2. 更新 `s.LastActivityAt = &now`，使最近活跃窗口排序自然前移。

#### 2. 数据结构变更（`bridge/query_recovery.go` & `bridge/store.go`）

保持 `session.Version == 3` 向后兼容（新增字段全部 `omitempty`，旧版 Harbor 忽略未知字段或平滑升级）：

```go
type conversationTransition struct {
	From     herdrIdentity `json:"from"`
	To       herdrIdentity `json:"to"`
	At       time.Time     `json:"at"`
	Source   string        `json:"source"`   // "herdr-hook" | "primary-selection" | "manual"
	RunToken string        `json:"run_token,omitempty"`
}

type conversationEvidence struct {
	PrimarySelection  *primarySelectionEvidence `json:"primary_selection,omitempty"`
	ObservedReference *herdrIdentity            `json:"observed_reference,omitempty"`
	Reference         herdrIdentity             `json:"reference"`
	Provenance        string                    `json:"provenance"`
	ObservedAt        time.Time                 `json:"observed_at"`
	Pane              string                    `json:"pane"`
	SocketIdentity    string                    `json:"socket_identity"`
	RunToken          string                    `json:"run_token"`
	Status            string                    `json:"status"`
	History           []conversationTransition  `json:"history,omitempty"`
}
```

- **与 `primary_selection.go` 的关系**：当用户启用实验性补丁 `AGENT_HARBOR_EXPERIMENTAL_CODEX_PRIMARY_SELECTION=1` 时，`savePrimarySelection` 同样追加一条 `Source: "primary-selection"` 的 `conversationTransition` 记录到 `Conversation.History`；并且 `observeAfterPrimarySelection` 遇到来自同核实运行的新 hook 时，也遵循相同的自动跟随规则，不再与 stock hook 路径割裂。
- **与 `agent_visibility.go` 的配合**：
  - `mergeHerdrAgents` 在 `verifiedForegroundRun` 命中时，若 `agent.Reference != nil` 且与 `s.Conversation.Reference` 不同：
    - 仅当 `s.Conversation.Status == "conflict"` 或触发双活冲突时才设置 `v.Health = "needs_review"`；
    - 正常切换过程中（observer 轮询 1 秒周期内的瞬态差），不向列表暴露假冲突告警，等 `candidateIdentityCapture` 落盘后 `owned` 分支直接接管。
- **与 `session_name.go`（自动标题同步）的配合**：
  - 当 `s.AutoNameSync == true` 且窗口内切换到了新对话 B 后，`captureAutomaticSessionName` 读取当前 `s.Conversation.Reference.Value` 在 `session_index.jsonl` 中对应的最新标题并同步到 `s.Name`、Herdr `tab.rename` 与 `pane.rename`。
  - 手动改过名（`AutoNameSync == false`）的窗口始终保持用户名称。
- **已知 stock Codex 0.169.0 限制（文档与 Details 透明说明）**：
  - 在窗口内 A → B 但**未发送任何新消息**即直接退出时，stock Codex 0.169.0 不触发任何 hook；Harbor 记录的最新已确认对话仍为 A。下次按 Enter 恢复时会打开 A（因历史共用同一 `CODEX_HOME`，B 仍在 `/resume` 列表中随时可切）。

#### 3. 遗留旧 `conflict` 记录迁移与恢复确认页

- **不静默覆盖旧记录**：升级前 `sessions.json` 中已存在 `Status == "conflict"` 或 `ReportedCodexID != ""` 的记录，读取时不自动清洗；用户下次按 Enter 时仍进入现有的 `Choose recovery target` 二选一页面（`recoveryCursor == -1` 门禁不变），用户明确选定并确认后恢复为干净 `Status: "reviewed"`, `Recovery: "ready"`。
- **正常 `ready` 停止会话按 Enter 恢复页降噪（`bridge/app_review_ui.go`、`bridge/app_enter_recovery.go`）**：
  - 保留轻量一次确认页，清晰列出：对话标题（从 `session_index.jsonl` 读取，回退显示短 ID）、Profile（多 Profile 时突出，单 Profile 正常字段展示）、Project、Client、Connection / Model。
  - 将 `bridge/app_review_ui.go:122` 原先的**黄字** `"Current login and other writers: unverified."` 降级为**低调灰字（`muted`）**说明行（如 `"Verify target and connection before resuming. Login and external writers are not verified."`），仅在真正存在错误/阻塞/模型缺失时才使用 `warn` 黄色。

---

### A2. Sessions 页面重构：分组抽象、信息层级与配色降噪

#### 1. 可扩展分组接口（`bridge/app_sessions_group.go`，新建）

抽象统一分组接口，解耦“数据排序分组”与“TUI 渲染”，为后续扩展工作目录、连接、Profile、本地/远端、用户 Tag 等分组维度预留清晰边界：

```go
type sessionGroupMode string

const (
	groupByStatus sessionGroupMode = "status" // 本期默认实现：Active / Stopped / External
	// 预留未来扩展（本期不暴露切换 UI，保持接口稳定）：
	// groupByProject    sessionGroupMode = "project"
	// groupByConnection sessionGroupMode = "connection"
	// groupByProfile    sessionGroupMode = "profile"
	// groupByHost       sessionGroupMode = "host"
	// groupByTag        sessionGroupMode = "tag"
)

type sessionGroup struct {
	ID       string        // 稳定分组 ID，如 "active", "stopped", "external"
	Title    string        // 显示标题，如 "Active", "Stopped", "External"
	Meta     string        // 右侧计数/摘要，如 "3"
	Sessions []sessionView // 组内已排序会话
}

type sessionGrouper interface {
	Mode() sessionGroupMode
	Group(views []sessionView) []sessionGroup
}
```

**本期默认分组器 `statusGrouper` 规则**：
1. **`Active`**：受管且当前运行中（`!s.External && (s.Running || s.Runtime.Presence == "observed")`），组内按最近活跃时间倒序（`Blocked` 可置顶或保持最近活跃序，确保列表不因频繁跳序导致误点——**规定：组内按最近活跃时间稳定排序，避免闪烁跳行**）。
2. **`Stopped`**：受管且未运行（`!s.External && !s.Running && s.Runtime.Presence != "observed"`，含 `Stopped`、`Not started`、`Unknown`），组内按 `LastExitAt` / `LastActivityAt` / `CreatedAt` 倒序。
3. **`External`**：未托管的外部 Herdr agent（`s.External`），如直接打开的 `agy` 或手工终端 `codex`，组内按发现序稳定排列。
4. **空组自动隐藏**：某个分组 `len(Sessions) == 0` 时不渲染横条。
5. **键盘与鼠标导航**：
   - 分组横条**不可聚焦**，↑↓ / j/k 仅在会话项之间移动，自动跨过分组横条；
   - 滚动视口计算（`sessionsWindow` / `sessionsLayout`）与鼠标命中区（`left.hit`）把分组横条计入精确行偏移，且顶部可见第一条会话若位于分组开头，优先把该组横条一并留在视口内；
   - 搜索过滤（`/`）命中结果后对过滤后的切片重新走 `Grouper.Group(...)`，同样隐藏无结果的空分组。

#### 2. 分组横条视觉样式（复用 Accounts 共享组件）

把 `bridge/app_accounts_ui.go:237` 的 `appendAccountClient` 提升为 `bridge/app_ui.go` 的共享方法 `(*uiPage).groupBand(title, meta string)`：
- 背景色：`harborPalette["button"]`（`#333742`）
- 左侧标题：`fg("text").Background(band).Bold(true)`，前留 1 格空格
- 右侧计数/说明：`fg("value").Background(band)`，右对齐留 1 格空格
- 窄屏自动截断右侧说明，保障组名完整可读；无色/ASCII 模式下退化为 `── Title (N) ──` 保持空间区隔。

#### 3. 修复状态语义色（`bridge/app_style.go`）

彻底修复现存 Bug（`statusRole()` 把 `Idle / Working / Done / Starting` 全部落入默认 `warn` 黄色）：

| 运行状态 / 标签 | 语义角色（`harborPalette`） | 视觉含义 |
|---|---|---|
| `Working`、`Running`、`Ready`、`Verified` | `ok`（柔和绿 `#99C793`） | 正在活跃工作 / 健康就绪 |
| `Idle`、`Done` | `brand`（淡青绿 `#80D8CF`）或 `label`（平静灰 `#9397A4`）——**推荐：`Idle`/`Done` 用 `label`（平静浅灰），`Working` 用 `ok`（绿），`Starting` 用 `title`（浅蓝）** | 活窗口待命与正在干活一眼区分，完全不刺眼 |
| `Starting` | `title`（浅蓝 `#94B9EF`） | 启动过渡态 |
| `Blocked` | `warn`（琥珀黄 `#DAB77E`） | 真正需要用户介入（如等待审批） |
| `Stopped`、`Exited`、`Not started`、`External` | `muted`（暗灰 `#777D8C`）或 `idle`（`#9397A4`） | 非活跃历史 / 外部进程，视觉后退 |
| `Unknown` | `muted`（暗灰 `#777D8C`，仅伴随 discovery 失败或真实阻塞时加短状态码） | 不再整片刷黄 |
| `Failed`、`Error` | `bad`（柔红 `#E3A09B`） | 明确错误 |

> **关键视觉原则**：全屏正常状态下，**不应出现大面积黄色**。黄色仅留给真正需要人工处理的 `Blocked` 状态和少量异常短状态码。

#### 4. 固定紧凑 2 行列表项 + 淡色元数据 + 短状态码

彻底取消 `sessionRowHeight` 在 2 行和 3 行之间来回跳动的机制，**每条列表项固定 2 行**（除 `External` 外部窗口若无副信息也保持统一 2 行节奏：主行名+状态，副行项目+`External`）：

```text
 Active                                                          2
> fix-session-binding                                  ● Working
  agent-harbor-workspace  codex  Native
  codex-auth-quota                                        ● Idle
  agent-harbor-workspace  codex  Magpie · claude-sonnet-4

 Stopped                                                         2
  legacy-conflict-test                        ● Stopped [Conflict]
  my-project  codex  Native
  old-task                                       ● Stopped [No ID]
  my-project  codex  Native

 External                                                        1
  agy                                                 ● Idle  External
  agent-harbor-workspace  agy
```

**逐元素规则**：
1. **第 1 行（主行）**：
   - 左侧：`row.marker()`（`> ` / `  `）+ 会话名称（`text`，选中时按 `uiRowStyle` 变 `brand`）。
   - 右侧状态区：
     - 状态点 + 简短状态词（`● Working` / `● Idle` / `● Stopped` 等），按上表语义色着色；
     - 仅当存在**真实需要用户注意的异常**时，在状态后追加一个紧凑短状态码（`sessionStatusBadge(s)`），取代原先的一整行长黄字：
       - `Conflict`（`warn`）：遗留冲突或双活 UUID 冲突
       - `No ID`（`warn`）：已启动/已停止但从未捕获到对话 ID
       - `Review`（`warn`）：Profile 未绑定、目录失效、网关离线/不匹配或其他恢复阻塞
       - `Offline`（`bad`）：Herdr discovery 异常
     - **外部窗口（`External`）**：严禁再生成黄色告警行或 `[Review]` 徽章！只在状态旁用低调 `muted` 显示 `External`，详细说明（`Unmanaged · Enter focuses this Herdr agent`）仅在右侧预览或 Details 中以灰色展示。
2. **第 2 行（副行，低饱和淡色区分各维度）**：
   - 缩进 2 格对齐主行名称，各元数据项之间用 2 格空白隔开（减少多余 `·` 符号噪音），使用克制的低饱和角色色区分：
     - **项目目录基名**（`filepath.Base(s.WorkingDirectory)`）：`key`（`#B4B7C2`，稍亮灰）
     - **客户端**（`codex` / `agy` 等）：`title` 降亮度或沿用 Herdr 侧栏一致的低饱和强调色（Codex 用 `title` `#94B9EF`、Claude 用 `warn` 柔和态或 `section` `#BEB7F2`、其他用 `value` `#9397A4`）
     - **连接方式**（`Native` / `Magpie · <model>`）：`muted`（`#777D8C`）
     - **Profile 短名**：**仅当 `len(m.profiles) > 1` 或该会话处于未绑定 legacy 状态时显示**（`section` `#BEB7F2` 柔和淡紫，且去掉冗长的 `· shared existing environment` 尾缀，仅显示 `p.Name`，例如 `Work` 或 `Default`；单 Profile 时完全不渲染！）。
   - 当悬浮（`row.hovered`）时，整 2 行保持一致的 `selBg` 背景底色且前景色清晰可读（满足 `docs/agents/tui.md`）。
3. **顶部标题栏降噪**：
   - 单 Profile（`len(m.profiles) <= 1`）：`⚓ Harbor`（不再拼 `· Profile: Existing environment · shared existing environment`）。
   - 多 Profile（`len(m.profiles) > 1`）：`⚓ Harbor · <ProfileName>`（精简短名，不拼 `· shared existing environment`）。
4. **右侧预览栏（`width >= 90`）与 `i` Details**：
   - 列表中被收起的完整告警文案（`sessionAttention(s)`）、完整项目路径、完整连接/模型信息、当前 Conversation ID 与历史切换轨迹（`Conversation.History`）全部清晰展示在右侧预览与 Details 中。
   - 右侧预览对外部窗口使用 `muted` 灰字说明，不再用 `warn` 黄色渲染。

---

### A3. 第一步遗留边界：`--no-daemon` 回退窗口与旧窗口切号

- **正式产品边界（写入 `docs/spec.md` 与帮助文案）**：
  - 带有权限覆盖且项目 `.codex/config.toml` 无冲突的窗口默认走私有 App Server（`--remote`），支持 `Accounts` 原地切号。
  - 若因项目 `.codex/config.toml` 冲突或自定义 profile 回退至 `--no-daemon`，或为无私有 App Server 的旧窗口，Harbor **不执行自动关闭重开**（避免在 stock Codex 下丢失未发送草稿或切错未发消息的对话）。
  - `Accounts` 切号结果页继续明确区分：
    1. Profile 保存的登录状态；
    2. 每个受管活窗口的结果（`switched` vs `not confirmed` + 明确提示“无私有 App Server，如需使用新登录请手动关闭该窗口后按 Enter 恢复”）。

---

## 4. Milestone B：导航重构、Profile 归位与 `🧩 Extensions` 统一资产管理

### B1. 顶层 5-Tab 导航与 Profiles 移入 Settings

#### 1. 顶层导航栏（`bridge/app_ui.go`）

`mainTabs` 调整为固定 5 项，保持窄屏（60/80 列）可容纳性：

```go
var mainTabs = []uiAction{
	{ID: "sessions", Label: "🚢 Sessions"},
	{ID: "extensions", Label: "🧩 Extensions"},
	{ID: "magpie", Label: "🐦 Connections"},
	{ID: "accounts", Label: "👤 Accounts"},
	{ID: "settings", Label: "⚙ Settings"},
}
```

- 顶层 `←→` / `h/l` 继续在 5 个 Tab 间切换；
- 快捷键直达：`e` 或 `x`？注意 `Sessions` 页已有 `e`（Rename）和 `x`（Remove），顶层跳转快捷键需避免冲突——建议保留 `←→/hl` 和鼠标点击进入 `Extensions`，全局数字/辅助键或在非编辑态可按 `g`（或通过 Tab 切换）；同时**保留全局 `p` 快捷键直达 Profiles 页面**（从 Sessions / Accounts / Extensions / Settings 均可直达，Esc 返回调用页）。

#### 2. `Profiles` 移入 `Settings`（`bridge/app_settings.go` & `bridge/app_profiles.go`）

- `Settings` 页面拆分为两个分区（复用 `groupBand` 分组横条）：
  1. **`Workspace shortcuts`**：现有的 Herdr 前缀与动作快捷键配置（保留全部编辑、冲突校验、草稿保存与热加载逻辑）。
  2. **`Environment & Profiles`**：
     - 入口行：`Profiles & isolated environments`（右侧显示当前状态摘要，如单 Profile 时显示 `Single environment (Default)` 或 `Single environment (Existing)`；多 Profile 时显示 `3 profiles · Active: Work`）。
     - 按 Enter 进入现有 `profile-list` 视图（完整保留新建隔离 Profile、重命名、选择默认 Profile、设置默认 Magpie 实例、编辑 `Codex defaults`、绑定 legacy 原环境、`Esc` 返回 Settings）。
- **单 Profile 降噪规则（跨全站一致）**：
  - `Sessions` 顶栏与列表副行：仅在 `len(profiles) > 1` 时显示 Profile 名。
  - `Accounts` 页面：单 Profile 时隐藏或弱化顶部的 `Profile:` 显眼行（仍保留 `p` 切换和 `i` Details 中的完整 Codex home 路径）；多 Profile 时顶部继续显示当前 `Profile` 字段。
  - `Extensions` 页面：同上，单 Profile 时聚焦 Skills / Plugins 内容，多 Profile 时顶部显示当前作用的 Profile，并露出“从其他 Profile 同步 Plugins”操作。

---

### B2. `🧩 Extensions` 页面：Skills 与 Plugins 统一管理架构

#### 1. 资产边界与多 CLI 可扩展抽象

为适配未来 Claude Code、OpenCode、Pi、Gemini CLI 等多客户端，定义客户端无关的扩展提供者接口（`bridge/extensions.go`，新建）：

```go
type extensionKind string

const (
	extSkill  extensionKind = "skill"
	extPlugin extensionKind = "plugin"
)

type extensionItem struct {
	Kind        extensionKind // skill | plugin
	ID          string        // 唯一键（skill: 规范路径或 name; plugin: plugin@marketplace）
	Name        string
	Description string
	Source      string        // "shared (~/.agents/skills)" | "profile ($CODEX_HOME/skills)" | "project" | "marketplace:<name>"
	Path        string        // 实际文件/目录路径（用于 Details 和 [[skills.config]]）
	Enabled     bool          // 当前 Profile 下是否启用
	Scope       string        // "global-shared" | "profile" | "project"
	Clients     []string      // 已关联/支持的客户端标签，如 ["codex"]
	DetailErr   string        // 单项加载警告（收进 Details）
}

type extensionSnapshot struct {
	Profile       profileBinding
	ProfileName   string
	CodexHome     string
	SkillsCLI     string // 解析到的外部 skills CLI 形态："companion" | "path" | "npx" | "unavailable"
	SkillsCLIPath string
	Skills        []extensionItem
	Plugins       []extensionItem
	Warnings      []string
}
```

#### 2. Skills 管理设计（共享源 + 按 Profile 开关 + `skills` CLI 交互交接）

1. **目录与发现（零 Node 依赖，纯 Go + Codex RPC）**：
   - **优先路径**：在 `WithProfileClient(ctx, req, profileInspect, ...)` 内复用 `withCodexConfigPipe` 调用 Codex App Server 的 `skills/list`（覆盖当前 Profile 实际生效的全部 skills 及其 `enabled` 状态与 `path`）。
   - **回退/补充扫描（纯 Go）**：若 App Server 不可用或针对未来非 Codex 客户端，直接扫描：
     - 全局共享源：`~/.agents/skills/*/SKILL.md`（解析 YAML frontmatter `name`、`description`）
     - Profile 本地源：`$CODEX_HOME/skills/*/SKILL.md`（按真实路径去重，识别指向 `~/.agents/skills/` 的软链，标记 `Source: "shared"`）
     - 并读取 `$CODEX_HOME/config.toml` 的 `[[skills.config]]` 表解析每个 `path` 的 `enabled` 开关状态。
2. **按 Profile 启用 / 禁用开关（零 Node 依赖）**：
   - 按 `Space` 或 `t`（Toggle）切换当前选中 Skill 的启用状态：
   - 在 `WithProfileClient(ctx, req, profileConfig, ...)` 锁内，优先通过 `withCodexConfigPipe` 调用 Codex `skills/config/write`（传入 `path` + `enabled`），写入当前 Profile 自己的 `$CODEX_HOME/config.toml`（不修改 `~/.agents/skills/` 源文件，因此**绝不影响其他 Profile**！）。
   - 写完回读确认生效，更新 UI 快照。
   - 若正在修改 `Existing environment`（共享的 `~/.codex/config.toml`），与现有 `Codex defaults` 一致，首次或变更前给出清晰范围提示（说明会写入 `~/.codex/config.toml`）。
3. **外部 `skills` CLI 解析与交互交接（`tea.ExecProcess`）**：
   - 解析顺序（不触碰网络，纯本地探测）：
     1. `AGENT_HARBOR_SKILLS_BIN` 环境变量指定的绝对路径（校验可执行）
     2. Harbor 伴生目录（companion dir，与 `codex-auth` 同级）下的 `skills`（为未来按需捆绑预留）
     3. Profile 恢复后的用户 `PATH` 中的 `skills`
     4. 用户 `PATH` 中的 `npx`（调用时使用 `npx --yes skills ...`）
     5. 均不可用时返回 `SkillsCLI: "unavailable"`，列表浏览与开关照常可用，触发安装/搜索/更新/卸载时弹出清晰引导（提示安装 Node.js 或 `npm i -g skills`）。
   - **执行环境与安全边界**：
     - 执行 `skills` CLI 时注入当前 Profile 的 `env.Environment`（含正确的 `CODEX_HOME` 和真实 `HOME`，**不泄露 Harbor 内部私有锁或 Magpie 密钥**）。
     - 通过 `tea.ExecProcess`（与 `accounts` 调 `codex-auth login`、`connections` 调 `magpie manage` 相同机制）将终端临时交接给：
       - **安装/搜索（`a` Add）**：支持在弹窗内输入仓库/关键词（如 `vercel-labs/agent-skills` 或留空进 `skills find`），执行 `skills add <source> -g -a codex`（默认全局装入 `~/.agents/skills/` 并软链到目标客户端，多 CLI 支持后可勾选目标 agents）。
       - **更新（`u` Update）**：执行 `skills update`（或针对单项）。
       - **卸载（`x` Remove）**：经 Harbor 确认页（明确提示“卸载全局共享源会影响共享 `~/.agents/skills` 的环境”或仅移除当前客户端链接）后执行 `skills remove <name> -g`。
     - 命令退出回到 Harbor 后，恢复鼠标捕获（`tea.EnableMouseAllMotion`）并自动重新加载 `extensionSnapshot`。

#### 3. Plugins 管理设计（按 Profile 独立 + 跨 Profile 清单同步）

1. **目录与发现**：
   - 在 `WithProfileClient(ctx, req, profileInspect, ...)` 内调用 `codex plugin list --json`（或复用 `withCodexConfigPipe` 的 `plugin/list` / `plugin/installed` RPC，在当前 Profile 环境下执行，确保只读该 Profile 的 `$CODEX_HOME/plugins` 与 marketplace 配置）。
2. **安装 / 卸载 / Marketplace 管理**：
   - 同样通过 `tea.ExecProcess` 在当前 Profile 的 `env.Environment` 下交接执行：
     - 安装：`codex plugin add <plugin>`
     - 卸载（经确认页）：`codex plugin remove <plugin>`
     - Marketplace 管理：`codex plugin marketplace ...`
   - 对已装插件提供 `Space` / `t` 启用/禁用（若当前 Codex 版本支持配置层开关，通过 `config/batchWrite` 或 `plugin/*` RPC 切换；若不支持则清晰标注只读，不猜测写配置文件）。
3. **跨 Profile 一键同步 Plugins 清单（核心新增能力）**：
   - **适用条件**：存在 >= 2 个 Profile 时，在 `Extensions` 页（以及新建隔离 Profile 完成后的可选提示步骤中）提供 **`y` Sync plugins from Profile…**。
   - **严格安全原则**：
     - **严禁软链 `$CODEX_HOME/plugins/` 或直接拷贝 `auth.json` / MCP OAuth 凭证**，防止跨 Profile 凭证泄露与缓存损坏。
     - 同步流程采用**声明式清单重放（Declarative Manifest Replay）**：
       1. 读取源 Profile 的已配置 marketplaces 与已安装 plugin ID 列表（只读锁，仅提取插件名与 marketplace 源，不读任何凭证/token/历史）。
       2. 与目标 Profile（当前 Profile）对比，计算增量差集：`待添加 Marketplaces` + `待安装 Plugins` + `已存在（跳过）`。
       3. 在确认页清晰列出：**源 Profile → 目标 Profile**、将新增的 marketplace 与 plugin 列表、以及明确披露“仅重新安装插件定义，不复制账号登录态、对话历史或 MCP OAuth 授权”。
       4. 用户确认后，在目标 Profile 的 `WithProfileClient(..., profileConfig, ...)` 环境下依次执行 `codex plugin marketplace add`（缺省项）与 `codex plugin add`（增量项），逐项记录成功/跳过/失败结果并在结果页汇总展示（单条失败不中断其余项，不回滚源 Profile）。

#### 4. `🧩 Extensions` 页面布局（严格遵守 `docs/agents/tui.md`）

```text
 ⚓ Harbor                                 (多 Profile 时: ⚓ Harbor · Work)
  Sessions  [🧩 Extensions]  Connections  Accounts  Settings
 ╭──────────────────────────────────────────────────────────────╮
 │ ⌕ Filter skills and plugins                                  │
 ╰──────────────────────────────────────────────────────────────╯
 Skills · shared (~/.agents/skills)                            4
> code-review                                            Enabled
  Review branch changes against standards and spec
  tdd                                                    Enabled
  Test-driven development red-green-refactor workflow
  legacy-helper                                         Disabled
  Disabled in this Profile

 Plugins · this Profile                                        2
  github-tools                                         Installed
  marketplace: official
  linear-sync                                          Installed
  marketplace: team

 ↵ Details  Space Toggle  a Add  u Update  x Remove  y Sync  r Refresh
 ↑↓/jk select  ←→/hl tabs  / search  ? help
```

- 复用 `groupBand` 分成 **`Skills`** 与 **`Plugins`** 两组；
- 每项固定 2 行：主行名称 + 状态（`Enabled`/`Installed` 用 `ok`，`Disabled` 用 `muted`），副行单行淡灰描述或来源；
- 宽屏（>= 90 列）支持右侧预览完整描述、真实路径（标明是否 symlink 回 `~/.agents/skills/`）、所属 Profile 开关来源；窄屏按 Enter 进 Details 查看；
- `Space` 切换开关（异步执行并显示轻提示），`a` 添加（先选 Skill 还是 Plugin，或随当前光标所在分组默认选中）、`u` 更新、`x` 移除（走统一确认壳）、`y` 从其他 Profile 同步 Plugins（仅多 Profile 时显示）。

---

## 5. 模块与文件改动清单

### Milestone A（核心体验）

| 文件 | 改动内容 |
|---|---|
| `bridge/query_recovery.go` | 为 `conversationEvidence` 增加 `History []conversationTransition`（`omitempty`）。 |
| `bridge/identity_candidate.go` | 重构 `update()`：满足 5 项安全前置条件且无双活冲突时，自动跟随新 hook 上报的合法对话、记录 `History`、保持 `observed`/`ready`；保留双活冲突与遗留旧冲突的拦截。 |
| `bridge/primary_selection.go` | 与新 `History` 结构统一，避免与自动跟随路径互相标冲突。 |
| `bridge/agent_visibility.go` | 收敛 `conversationAssociationWarning` 与 `mergeHerdrAgents`，移除外部窗口黄色告警文案（转为结构化低调说明）；避免合法切换瞬态触发假警告。 |
| `bridge/session_name.go` | 确认 `AutoNameSync` 在对话自动跟随切换到 B 后正确跟随 B 的标题更新并同步 Herdr tab/pane。 |
| `bridge/session_checks.go`、`bridge/enter_recovery.go` | 保留双活冲突检查与遗留 `conflict` 的显式二选一，精简恢复确认页文案。 |
| `bridge/app_style.go` | 修复 `statusRole()`，区分 `Working`(`ok`)、`Idle`/`Done`(`label`)、`Starting`(`title`)、`Blocked`(`warn`)、`Stopped`/`Not started`/`External`/`Unknown`(`muted`/`idle`)、`Failed`(`bad`)。 |
| `bridge/app_ui.go` | 抽离共享分组横条 `(*uiPage).groupBand(title, meta)`（供 `Accounts`、`Sessions`、`Settings`、`Extensions` 共用）。 |
| `bridge/app_sessions_group.go`（新） | 定义 `sessionGrouper` 接口与默认 `statusGrouper`（`Active` / `Stopped` / `External`，空组隐藏）。 |
| `bridge/app_sessions_ui.go` | 重构列表为分组横条 + 固定紧凑 2 行 + 淡色元数据 + 短状态码 + 单 Profile 降噪 + 跨组键盘/鼠标命中与滚动计算。 |
| `bridge/app_review_ui.go` | 正常恢复确认页去刺眼黄字（`Current login and other writers` 改 `muted`）。 |
| `bridge/recovery_integration.go` | 增加 `sessionStatusBadge(s)` 短状态码映射，将长句留给右栏预览与 Details。 |
| `docs/spec.md`、`docs/herdr-integration.md` | 更新 Session 槽位自动跟随语义与 `--no-daemon` 回退不自动重开边界。 |

### Milestone B（统一资产 & 导航重构）

| 文件 | 改动内容 |
|---|---|
| `bridge/app_ui.go`、`bridge/app.go` | 更新 `mainTabs` 为 5 项（引入 `extensions`，移除顶层 `profiles`），保留 `p` 快捷键和 `activateSessions` 路由。 |
| `bridge/app_settings.go` | 使用 `groupBand` 增加 `Environment & Profiles` 分区及进入 `profile-list` 的入口。 |
| `bridge/app_profiles.go`、`bridge/app_accounts_ui.go` | 单 Profile 时弱化 Profile 字段露出；从 Settings 进入 Profiles 时 `Esc` 正确返回 Settings。 |
| `bridge/extensions.go`（新） | 定义 `extensionItem`/`extensionSnapshot`、纯 Go `~/.agents/skills` + `$CODEX_HOME/skills` + `config.toml` 扫描器、`skills` CLI 解析器（`AGENT_HARBOR_SKILLS_BIN` → companion → `PATH` → `npx`）。 |
| `bridge/extensions_codex.go`（新） | 复用 `withCodexConfigPipe` 调用 Codex `skills/list`、`skills/config/write`、`plugin/list`；实现 `codex plugin` 查询与跨 Profile 声明式 Plugins 清单对比与同步执行。 |
| `bridge/app_extensions_ui.go`（新） | 实现 `🧩 Extensions` 主页、搜索过滤、Details、Toggle、`tea.ExecProcess` 交互交接（Add/Update/Remove）、跨 Profile Sync 选择与结果页。 |
| `bridge/service.go`、`bridge/service_fake.go` | 在 `applicationService` 接口上新增 Extensions 查询、开关与插件同步方法，并在 fake service 中补齐以便 UI 回归测试。 |
| `docs/agents/tui.md`、`docs/spec.md`、`docs/ui-flow-audit.md`、`docs/ui-visual-review.md` | 同步 TUI 契约（5 Tab 与 Profiles 归位）及实机验收记录。 |

---

## 6. 验收门禁与测试计划

所有运行时测试必须严格遵守 `bridge/tests/README.md` 与 `AGENTS.local.md`：使用独立临时 `HOME` / `XDG` / `CODEX_HOME` / tmux socket / 端口，绝不触碰用户现有服务与账号，测试结束后清理全部临时目录。

### Milestone A 验收标准

1. **单元测试与 `-race` 回归**：
   - `identity_candidate_test.go`：覆盖①同核实运行 A → B（`/resume` 或 `/new`）自动跟随且记录 `History`；②A → B → C 连续切换；③自动标题同步跟随 B 且手动改名保持不变；④两个同时运行的 Session 撞同一 UUID 触发 `conflict`；⑤静默恢复不标脏（第一步回归）；⑥旧 `sessions.json` 中已有 `conflict` 记录升级后仍需显式二选一，不静默覆盖。
   - `app_sessions_ui` / `app_contract_test.go`：覆盖 `statusGrouper` 三组划分与空组隐藏、键盘 ↑↓ 跨分组横条、鼠标点击命中偏移、搜索过滤后动态隐藏空组、单/多 Profile 副行与标题降噪、`statusRole()` 全状态色映射、`NO_COLOR` / `AGENT_HARBOR_ASCII=1`。
   - `go vet ./...` 与 `GOWORK=off go test -race ./...` 全部通过（注：记录并隔离已知无关失败项，不掩盖新回归）。
2. **隔离真机客户端验收（macOS arm64：Herdr 0.9.5 + Codex 0.169.0 + Magpie 0.1.3528-harbor-dev）**：
   - 场景 1（窗口内切对话）：Harbor 启动 Codex 对话 A → 在 Codex 内 `/agents` 或 `/resume` 切到对话 B 并发送一条消息 → Harbor 自动更新为 B、无黄色警告、自动标题更新 → 退出后按 Enter 恢复直接指向 B。
   - 场景 2（静默切对话边界）：A 切 B 不发消息直接 `/exit` → Harbor 保持已确认的 A、无假报错，按 Enter 可正常恢复 A。
   - 场景 3（四尺寸视觉证据）：在外层 `160×45`、`100×30`、`80×24`、`60×24`（含无色模式）捕获同时包含 Active（Working/Idle/Blocked）、Stopped（正常/Conflict/No ID）、External（模拟 `agy`）的真实 Herdr 弹窗 ANSI，经仓库 Unicode 15 渲染器生成视图并逐张核查排版、对齐、滚条与底栏。
   - 场景 4（`--no-daemon` 边界）：项目 `.codex/config.toml` 触发回退 `--no-daemon` 后在 `Accounts` 切号，确认不自动重开且结果页如实报告 `not confirmed`。

### Milestone B 验收标准

1. **单元测试与 `-race` 回归**：
   - 导航测试：5 Tab 左右切换、`p` 快捷键从各页进入 Profiles 并 `Esc` 返回原页、Settings 进入 Profiles 并返回、单 Profile 与多 Profile 露出切换。
   - Extensions 纯 Go 逻辑测试：扫描 `~/.agents/skills` 与 `$CODEX_HOME/skills`（含 symlink 去重）、解析 `SKILL.md` frontmatter、读写 `[[skills.config]]`、单 Profile 开关不污染另一隔离 Profile、`skills` CLI 探测优先级（`AGENT_HARBOR_SKILLS_BIN` → companion → `PATH` → `npx` → `unavailable`）、跨 Profile Plugin 清单 diff 计算与零凭证复制校验。
2. **隔离真机集成验收（macOS arm64：Herdr 0.9.5 + Codex 0.169.0 + 隔离模拟/真实 `skills` CLI）**：
   - 在两个隔离 Profile（A、B）之间验证：
     - `~/.agents/skills` 装入的 Skill 在 A 和 B 均可见；在 A 中禁用后 B 仍保持启用，A 的 `config.toml` 写入正确且通过 `validateProfilePolicy` 校验。
     - 通过 Harbor `Extensions` 触发 `skills` CLI 交互交接（使用隔离假 `skills` 或本地源）执行 Add/Update/Remove 后，正确回到 Harbor、恢复鼠标事件并刷新列表。
     - 在隔离环境中验证 `codex plugin list --json` 展示与 A → B 跨 Profile 同步 Plugins 清单（验证 B 仅同步插件定义，不拷贝 A 的 `auth.json` 或对话历史）。
   - 四尺寸（`160×45`、`100×30`、`80×24`、`60×24`）捕获 `Extensions`、新版 `Settings`（含 Profiles 入口）与单/多 Profile `Accounts` 的真实 ANSI 证据并完成 Unicode 15 视觉复核。
   - 更新 `docs/ui-flow-audit.md` 与 `docs/ui-visual-review.md` 状态表，明确区分已验证项与未覆盖平台（如 Linux / 远端 / 真实线上 marketplace 网络）。

---

## 7. 已知限制与后续演进预留

1. **stock Codex 0.169.0 不发消息切对话不可感知**：属于上游客户端行为，不作为本期阻塞；仓库内 `docs/codex-primary-selection.patch` 作为可选实验增强保留。
2. **未来更多 Sessions 分组维度**：Milestone A 落地 `sessionGrouper` 后，后续可在不改列表渲染壳的前提下平滑增加按工作目录、连接、Profile、本地/远端、用户 Tag 的分组切换快捷键。
3. **未来多 CLI 扩展**：`extensionItem` 与 `skills` CLI 的 `~/.agents/skills/` 模型已天然面向多客户端，后续接入 Claude Code / OpenCode / Pi / Gemini CLI 时只需实现对应客户端的 `extensionProvider` 适配层。
4. **`skills` CLI 可选伴生捆绑**：本规格预留 `AGENT_HARBOR_SKILLS_BIN` 与 companion 目录探测，未来若上游发布原生单文件二进制或决定单独打包分发，可直接无缝切换。
