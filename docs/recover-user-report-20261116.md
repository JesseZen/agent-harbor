# Agent Harbor Recover：用户问题记录与后续排查入口

日期：2026-11-16。状态：**已实现修复候选；用户实机复验待完成，缺失身份的旧记录仍需显式关联**。
原问题代码基线：`c88c08880893a665eb9066473f87e922a1cafe19`。

## 重要
用户补充说`以下的 ctrl b q 退出再进 不是项目的问题, 而是用户运行的脚本包含了"$harbor" stop && "$harbor" open 的命令, 改成 "$harbor" open 就没问题了`. 现在的主要问题是会话的恢复问题, 黄字频繁出现的问题等.

## 用户明确报告的现象

- 用户所说的恢复是 **Agent Harbor 的 Recover**，不是 Codex 内的 `/resume`。
- 经常无法重新打开刚刚关闭的对话；进入恢复页面后，点击恢复也不能正常恢复。
- 正常运行的会话旁也持续出现大段黄色说明；恢复过程中经常要求阅读或确认长段文字。
- **这些 Recover 和黄色说明问题在引入自动命名之前就存在**，不能将其归因于本轮命名修改。
- 用户提供的截图显示：进程运行已确认，但 Conversation 身份未确认，官方身份集成未证实，最近一次观察为 `missing`。

截图中的诊断文字（产品自身生成，不含用户对话内容）：

> Conversation identity has not yet been observed; recovery requires review.
> The live terminal run is verified; conversation identity is unverified and recovery still requires review.
> Official identity integration is unproven; inspect Herdr integration status in this CLI configuration, enable/trust its hooks, then explicitly review recovery. Observation: missing; review recovery.

用户此前确认 #19/#20 的本机人工试用通过并要求关闭 issue，见
[验收记录](issues-19-20-acceptance.md)。该报告不覆盖、也不推翻本条后来明确报告的 Recover 失败。

## 补充实机报告：Ctrl+B、Q 退出后重进变空

用户在约 23:51–23:53 提供三张截图，并明确描述以下操作顺序：

1. 使用 `Ctrl+B`、`Q` 退出 Agent Harbor。
2. 重新进入 Agent Harbor，用户观察到“全变空了”。
3. 在 Sessions 页尝试恢复已有会话，仍然无法恢复，页面出现下面的诊断和拒绝文字。

这次属于**用户实机报告与截图证据**，尚未由 agent 独立复现。
“全变空”具体指终端内容、窗格布局还是其他显示，尚未区分；Sessions 页仍能显示目标记录和详情，不能据此断言持久化记录或 Codex 历史已删除。
同样，退出按键是否只断开客户端、是否导致后台退出或布局恢复异常，需要验证，不能直接当作完整工作区重启。

截图一显示原有的身份缺失警告，以及：

> Legacy conversation needs review: unassociated; inspect Herdr identity and use reassociate SESSION_ID REFERENCE_JSON REVIEW_REASON, then explicitly approve query recovery.

截图二的 Session details 可见以下字段（个人路径、名称和运行标识已省略）：

| 字段 | 截图显示 |
| --- | --- |
| 总体状态 | `Unknown` |
| Presence | `not_observed` |
| Runtime association | `unassociated` |
| CLI | `codex-cli 0.169.0` |
| Connection | `magpie` |
| Initial model | `@codex-default` |
| Instance | `managed` |
| Observed route | `not observed` |
| Conversation | `unknown` |
| ID source | `unknown` |
| Recovery | `needs_review` |
| Previous Herdr pane | 有保存的窗格引用 |
| Identity observation | `missing` |

截图三的 `Harbor · Review resume` 页面仍展示 `Resume conversation` 按钮，Conversation 为 `unknown`，并显示：

> query recovery requires an upstream-observed reference; legacy records require explicit review and reassociate before query recovery · Inspect evidence, retry or Cancel.

该拒绝文字对应 `queryReferenceCheck` 的缺失身份分支。它说明这次操作被缺失恢复引用挡住，但尚未验证退出前为何没有捕获身份，以及重进后为何变空。界面里的 `Legacy` 是程序标签，不足以证明这一定是旧版本记录或数据迁移问题。

原始截图含个人目录和会话名称，未提交仓库；这里保留必要的操作顺序、状态字段和错误文本。下一轮应优先用相同退出/重进路径建立复现，不只测试主动 stop 后恢复。

## 已核实的代码事实与排查线索

以下是代码检查结果，不是全部用户失败场景的根因结论：

1. `bridge/query_recovery.go` 的 `queryReferenceCheck` 要求满足保存的 Conversation 身份及观察/显式复核条件。没有身份报告时，能够验证运行进程并不等于允许恢复。
2. `bridge/identity_candidate.go` 从 Herdr 观察身份；新报告与保存的引用不一致时会标记冲突，不会自动把绑定改到另一段 Conversation。应区分用户主动切换与非预期目标替换，不能直接取消冲突保护。
3. `bridge/app_review_ui.go`、`bridge/app_view.go` 和 `bridge/app.go` 包含 Harbor 自己的恢复说明、记录确认和模型选择前置条件。截图中的黄色说明不能笼统归为 Codex 上游错误。
4. `bridge/launch.go` 默认给普通 Codex 启动加入 `--no-daemon`。用户在 Harbor 外看到的“断开界面，后台任务继续，退出时打印当前线程重连 ID”，与 Harbor 默认拓扑不同。不要直接假定当前已经具备那套任务重连体验。
5. `bridge/previous_pane.go`、`bridge/action_state.go` 和 `bridge/agent_visibility.go` 负责旧窗格、进程退出、目标关联及恢复前检查。刚关闭不能打开还可能涉及这些状态；本轮没有独立复现其具体失败。

已知能力和限制详见 [Herdr 集成](herdr-integration.md)、
[运行状态契约](runtime-state.md)、[Profile 契约](profile-contract.md)。

## 本轮已经完成的工作与边界

- `9657984`：一次性自动命名、Herdr 原生 pane/tab 标签及 sidebar 明暗层次。
- `c88c088`：自动命名不再硬性依赖 hooks 身份报告。校验 Harbor 当前进程后，通过 Herdr 终端标题匹配所属 Profile 的 Codex 标题索引；**只更新显示名称，不生成或改写恢复身份**。
- 后者的测试已覆盖真实 Codex 自行生成标题、hooks 关闭、初次取名不使用 `/rename`，以及后续手动改名。Linux 全量 Go 测试、定向 race、vet 和命名 UI 验收通过。
- 用户使用的 macOS arm64 程序已更新到 `c88c088`；一次待命名会话已补同步并核对运行进程、Conversation 和连接信息未改变。
- **没有实施 Recover 修复、持续跟名、自动跟随 `/resume` 改绑、后台服务拓扑调整或轮询性能优化。不要将命名验收当作 Recover 验收。**

## 用户希望的方向与需要验证的上游能力

优先复用 Codex 和 Herdr 的原生查询、恢复、重连和事件能力，Harbor 维护必要的窗格、Profile、Conversation、Connection Intent 对应关系，避免另写历史或恢复引擎。

调查起点：[Codex CLI](https://developers.openai.com/codex/cli/reference/)、
[Codex App Server](https://developers.openai.com/codex/app-server)。
官方存在 `thread/read`、`thread/resume` 等接口及生命周期通知；但服务中存在某个线程，不等于证明某个 TUI 窗格当前正在显示它。必须验证当前客户端版本、服务拓扑及窗格绑定的实际能力。

正常使用应易于重新连接或恢复。运行状态与恢复就绪程度分别显示；一般情况只需简短、可操作的说明，详细证据可放入详情。ID、环境、连接冲突等真实不确定性仍要保留，不能通过猜测或隐藏错误获得“恢复成功”。

## 下一位 agent 的优先工作

1. 用真实 Codex、Herdr 和受控 provider 在隔离环境复现：新建 → 完成一轮 → 正常退出 → 立即 Recover。保留准确拒绝点，并区分客户端退出、窗格关闭和界面断开。
2. 优先覆盖补充报告中的 `Ctrl+B`、`Q` 退出 → 重新进入 → 变空 → Sessions Recover 路径；先区分客户端断开与服务退出，再单独覆盖完整工作区重启。比较前后确切的 Conversation、Profile、连接实例和模型选择，避免恢复成新对话或其他对话。
3. 覆盖 hooks 无报告的正常运行；确定上游能可靠提供身份的路径，避免把缺失报告永久转换为无法恢复。
4. 核对正常退出后的状态是否及时更新、是否存在重复/过期 Herdr 观察，以及恢复按钮的各个前置条件。分别修复实际复现的根因，不预设所有失败同源。
5. 基本恢复稳定后，再讨论 Codex 内切换 Conversation 后的绑定同步和持续跟名。不要统一用全局“最近会话”代替每个窗格自己的已确认 ID。
6. 说明文字随功能一起整理，避免重复确认；验收必须包含真实恢复成功，不能仅以移除黄字为通过。

遵循 [测试隔离要求](../bridge/tests/README.md)；保留现有用户服务、凭据和历史。确需用户提供失败样本时，只采集必要元数据与错误文本。

## 另一个已讨论、未实施的问题：轮询开销

每秒观察 Herdr 的循环至少在初始提交 `154ec7d` 已存在，不是命名功能新加的定时器。当前每个活跃会话正常情况下每秒启动一次 `herdr pane get`，身份更新还会保存会话记录；写入路径含文件和目录同步。命名增加记录读取，成功后不再扫描标题索引。尚无 CPU、I/O 或耗电基准。

用户关心性能；可调查事件通知、连接复用、只在状态变化时落盘和缓存。先保证状态与恢复正确，避免在这份问题记录中将尚未测量的收益当成事实。

## 2026-11-17 隔离复现与诊断

以下为修复前的诊断记录；后续实现见文末。诊断阶段未修改产品代码或替换用户程序。测试为 Linux amd64、真实
Codex 0.169.0、Herdr 0.9.5 本地 query 候选、受控原生 provider；使用独立
HOME、Codex home、SQLite、Harbor 状态和 tmux socket，不使用用户凭据。
运行的 Harbor 为 `c88c088` 命名修复候选；当前 `f3737f9` 相比该提交没有
`bridge/` 代码变化。以下结果不代表 macOS、真实账号或 Magpie 恢复已验收。

### 已复现的失败路径

1. 不安装 Herdr Codex 集成、关闭 hooks，正常新建并完成一轮请求。
2. Harbor 显示 `Idle`、`running=true`，但没有 Conversation ID，
   `recovery=needs_review`，并显示用户截图中的两段身份警告。
3. 使用 Codex `/exit` 正常退出，等待 runner 写入进程结束；身份仍为空。
4. 公共 `resume --development-query` 命令拒绝：
   `query recovery requires an upstream-observed reference; legacy records require explicit review and reassociate before query recovery`。

另一次独立 UI 复现也通过：正常 `/exit` 后，从 Sessions 进入 Review resume，
Conversation 显示 unknown，Resume 按钮仍可操作；确认后出现同一条缺失
引用的拒绝文字。保留了列表页和拒绝页的终端捕获。

之前用 `stop-session` 也得到相同结果；正常 `/exit` 的独立复现说明这条
失败路径不依赖强制停止或用户曾经使用的 `stop && open` 脚本。

### 对照结果

在新的隔离环境安装 Herdr 官方 Codex 集成，完成 Codex hooks 信任后，
同样完成一轮、正常 `/exit`。Conversation ID 被捕获，退出后仍为
`observed`。无需手动 `bind` 或 `reassociate`，在 managed Herdr 窗格内
执行 query recovery 成功；第二轮受控 provider 请求同时包含第一轮和
第二轮标记，且保存的 Conversation ID 不变。

注意：Herdr `integration install codex` 本身会把 `features.hooks` 设为
`true`。仅在安装前写入 `hooks=false`，不能作为“已安装但未启用”的
对照；本轮没有把这种情况误记为 hooks 关闭仍能捕获身份。

### 代码确认的问题

- **身份采集缺少准备流程。** `candidateIdentityCapture` 读取 Herdr
  `pane.get.agent_session`，没有报告就不会生成恢复引用。
  `CreateProfile` 创建独立客户端目录，启动流程没有为该 Profile 安装或
  引导准备 Codex 身份集成。能启动、聊天和自动命名，并不表示具备恢复
  身份。已完成聊天却没有捕获 ID，是本次复现的直接阻塞点，不能通过
  放宽 `queryReferenceCheck` 或猜测最近对话修复。
- **恢复入口允许进入必败确认。** `appSessionInspected` 在已退出且
  Conversation 为空时仍选择 `resume`，`reviewActions` 不因缺少引用禁用
  Resume；实际执行才被 query 检查拒绝。`deriveSession` 还把这种新记录
  标成 Legacy，容易误导为迁移问题。
- **黄色说明重复且混入正常信息。** `deriveSession` 生成身份缺失原因，
  `agent_visibility.go` 再追加同义说明；Sessions 右栏把 `Reason`、
  `DiscoveryWarning`、`IdentityWarning` 全部展开并染黄。即便身份已经
  观察到，observer 仍会保存一段正常的恢复提醒，UI 同样显示为黄色。
- **安装诊断存在配置作用域疑点。** `candidateIdentityCapture` 调用
  `integration status` 时没有传入客户端的 Profile 环境，并以
  `HERDR_PLUGIN_ENTRYPOINT_ID` 而非 client 匹配状态行。通用 `session`
  入口和独立 Codex home 需要单独回归；这是静态代码发现，尚未当作本次
  身份缺失的实机根因。

### 修复顺序

先补齐按 Session 绑定的 Profile 准备、检查和引导身份集成的流程，并用
默认新建环境直接验证恢复，避免测试先手动补齐产品缺少的前置条件。
已有缺失 ID 的记录需要单独设计明确的身份选择，安装 hooks 不会自动
补回它们原来的 Conversation。随后让恢复入口提前说明具体缺项，移除
错误的 Legacy 标签，并把正常提醒、简短阻塞原因和详细证据分别呈现。
保留身份冲突、连接及 Profile 检查。最终验收仍须验证同一 Conversation
和连接的实际恢复，不能只检查黄字消失。

## 2026-11-17 修复候选与验证

用户随后授权修复。候选已实现：

- managed Codex 启动前，在 Session 绑定的 Profile 环境和项目目录检查
  官方身份集成。隔离 Profile 自动准备；已有环境或明确关闭 hooks 的
  配置先询问是否准备、跳过或取消。已准备好的环境不重复询问。
  使用 Herdr 官方安装命令，信任仍交由 Codex 原生界面确认。
- 缺少 Conversation 的已退出记录在进入恢复确认前即说明阻塞原因；
  确认页的按钮和键盘路径也保留缺失引用检查。新记录不再误标 Legacy。
- 列表仅展示一条简短问题说明，详细身份观察保留在详情。正常观察到
  身份或正常退出后保留身份，不再生成黄色提醒。
- race 验证发现并补修另一条状态漏洞：周期身份读取耗尽重试后，客户端
  恰好退出，原逻辑可能用最终空报告恢复 `ready`。现在这类未解决读取
  失败必须由新的有效身份报告解除。新增确定性用例先复现失败再验证修复。

已完成 Linux amd64 验证：

- 全量 Go 测试通过（最终一轮 90.231s）；定向恢复与 identity race 通过
  （130.865s），`go vet ./...` 通过。
- `query_recovery_real.py` 从未安装集成的环境开始，由 Harbor 准备并在
  Codex 中完成信任；完成一轮、正常 `/exit`、恢复相同 Conversation，
  第二轮实际请求包含原上下文，无需手工 `bind` 或 `reassociate`。
- `--skip-identity` 实机负向用例通过：明确跳过准备后仍能聊天，但不会
  推断 ID；正常退出后 UI 在恢复确认之前说明缺失身份。
- `app_acceptance.py --replacement --profiles --onboarding managed` 通过：
  A/B 与同 Profile 多 Conversation 并发、原生/API-only Magpie 连接、
  另一 Profile 被选中时恢复原对话，以及工作区/网关重启后的双向恢复。
  此用例不再手动为各 Profile 安装 hooks，而由 Harbor 首次启动准备。

真实客户端版本为 Codex 0.169.0、Herdr 0.9.5 本地 query 候选。Profile
验收使用 pinned Magpie 0.1.3528 加 caller-only 与 Profile isolation 两个
已有本地补丁；不是上游正式发布。本轮修正了验收前置条件：合成 OpenAI
API key 不是已确认的 ChatGPT 账号，API-only 路径明确保持未登录；不放宽
产品账号或网关隔离检查。真实账号授权及远程恢复仍未覆盖。

macOS arm64 原生隔离验收也已通过：同为 Codex 0.169.0、Herdr 0.9.5
本地 query 候选，未预装身份集成，由 Harbor 准备并经 Codex 信任后完成
聊天、正常 `/exit` 和同一 Conversation/上下文的真实恢复。该结果仅覆盖
原生恢复链路；完整 macOS A/B、Magpie 和真实账号矩阵仍单独保留。

修复没有补造旧记录从未保存的 Conversation ID，也没有替换用户当前程序、
停止现有服务或改动用户凭据及历史。已有缺失引用的记录仍须明确选择并
关联原对话，不能视为已自动修复。

后续用户授权替换当前程序：已备份旧二进制，并原子替换本机 macOS arm64
启动入口，校验候选 SHA-256 和 `--version` 成功。未重启服务，替换前运行的
相关进程均仍存在。新启动的面板和会话使用新版；已有 runner 不会热更新。

### 替换后补充：未发送第一条消息即停止 Harbor

用户在 2026-11-17 再次报告：新建 Magpie Codex 会话，直接 stop Harbor
再重新进入，看到缺失 Conversation ID 的提示。用户随后明确：已经选择
信任 hooks，但没有完成一轮聊天，正在另行尝试聊天后再停止的路径。

新增隔离实机对照（Linux、真实 Codex 0.169.0、同上 Herdr 和 Magpie
候选、API-only 连接）确认：

- 信任 hooks 但未发消息：stop 前就没有观察到 Conversation ID；客户端
  home 中没有持久化的 conversation JSONL 或名称索引。直接 stop/reopen
  后同样缺失引用。不是 stop 删除了已经保存的 ID；当前界面仍将其显示
  为身份未捕获，尚不能区别“未开始对话”和“聊天后身份采集失败”。
- 完成第一轮再直接 `stop` Harbor（不先 stop-session）：stop 前后均有
  同一个完整引用，未显示缺失 ID 提示。恢复前仍需显式检查并关闭 Herdr
  恢复出来的空闲 shell；随后通过 Harbor 恢复原 Magpie Conversation，
  第二轮实际请求保留第一轮上下文，Profile、实例、连接形式及 ID 均相同。

这一轮是明确的直接停服对照。此前 Profile A/B 重启验收先逐个停止客户端，
不能单独代替本轮证据。上述结果不代表已完成用户真实 caller 连接的复验。
不应仅凭缺失引用把已启动的记录静默重置为新对话；空白启动与丢失身份
需要进一步区分，或通过明确的新建选择处理。

### 完成首轮聊天后的恢复死循环与焦点（2026-11-17）

用户随后完成聊天并提供新截图：Conversation 已有完整 ID，点击 Resume
却返回 `conversation observation needs review`，只有 Inspect evidence 和
Cancel 可来回切换；失败及详情返回后焦点还会落到 Cancel。

本机只读核对发现，该记录的保存引用和观察引用一致、没有冲突候选，状态为
`observation_unavailable`，已有结束证据。旧 UI 的 query 不确定性确认与
保存引用复核分属两条路径，Resume 只完成前者，因而无法完成该页要求。
恢复确认默认 Cancel、方向键打开详情进一步加重了导航混乱。

本轮修复把无冲突保存引用的复核并入 Resume，一次确认原 ID、Profile 和
连接后继续恢复；Details 为可选证据。恢复默认聚焦 Resume，失败保留该焦点，
详情返回聚焦 Details，方向键直接导航动作。确认后发生连接故障时，UI 保留
该次实际写入的复核快照供原页重试；记录或运行目标变化仍拒绝旧确认。另修复
审计 JSON 序列化格式被误认成记录变化的问题。没有替换身份或放宽真实冲突、
缺失引用、活跃进程及连接校验。

本轮验证：

- Linux 全部 Go 测试通过（87.035s），恢复、保存引用及复核竞态测试通过，
  `go vet ./...` 通过；新增测试覆盖失败重试、后续编辑拒绝和真实冲突拒绝。
- Linux 与 macOS 均通过真实 Codex 0.169.0、Herdr 0.9.5 本地 query 候选、
  Magpie Profile 候选的隔离验收：完成首轮后直接停止/重启 Harbor，仅在
  fixture 自有状态重放最终观察不可用；鼠标一次点击 Resume 完成原引用复核，
  故意停止 fixture Magpie 后显示具体连接失败并保留 Resume 焦点；恢复 Magpie
  后同页重试，原 Conversation 的实际第二轮请求带有首轮上下文，Profile、
  ID、实例和连接形式相同，仅记录一次复核。
- macOS 原生连接的真实 Codex 正常退出、恢复同一 ID 与上下文也通过。

上述 Magpie 实测使用合成 API provider；用户真实 caller 账号路径仍须用户
复验，Herdr query 仍为本地开发候选，不能视为上游发布能力。

本轮已再次备份并原子替换本机启动程序，校验版本和二进制散列成功。
未停止用户服务、改写用户会话记录或代替用户接受复核；关闭旧面板重新打开
后才会使用新 UI。
