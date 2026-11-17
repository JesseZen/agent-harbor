# UI 实机走查 — 2026-11-17

视觉结构的逐页对照与统一方案见 [ui-visual-review.md](ui-visual-review.md)。

## 实现状态（2026-11-17 更新）

本文后面的表格是 `0934148` 的问题记录，不是完成清单。下表以 `ui/flow-audit` 分支代码为准；这是未完成的 UI 阶段性成果，尚未合入 main。

| 状态 | 内容 |
| --- | --- |
| 已实现 | Harbor 操作按钮移除，改为固定底部快捷键；状态消息不替换底栏；快捷键按组换行；正文、列表按底栏实际高度留出空间；空间不够时显示窗口太小。 |
| 已实现 | 新建 Ctrl+S、Settings Ctrl+S 保存/Ctrl+R 恢复；目录 Ctrl 操作可发现；绑定原环境 Ctrl+Y；不可用操作显示 unavailable（完整原因仍有待统一）。 |
| 已实现 | Profile 默认实例改独立列表，修正 Tab 焦点与 Enter 保存目标；新建名称 Shift+Tab；长文档可滚到最后一行；Settings 删除每字段两行空白。 |
| 已实现 | 新建名称提示改为灰色输入框占位文字，输入后消失；会话重命名、Profile 新建/重命名与旧筛选编辑复用相同输入框；Profiles emoji 仅保留顶层 tab，正文 Profile 字段与页面标题去除重复 emoji。 |
| 已实现 | 参考 omp 18.22.0：搜索框空闲灰框、聚焦淡紫框，⌕ 与真实文本输入光标，灰色 placeholder；底栏统一 ⎋ / ↵ / ⇥ 符号；长列表和文档右侧自动显示比例滚动条，短内容隐藏。 |
| 已实现 | Accounts、Profiles、Settings、Connections、客户端空结果、Start/Resume 审阅移除重复操作说明；空状态统一灰色且不加 bullet。账号限制/路径进 Details，危险后果与连接披露仍保留。搜索输入时隐藏不生效的 Sessions 全局动作提示。 |
| 部分实现 | 表单/确认页的快捷键位置统一；Accounts/Profiles 正文层级已统一；其余标题和所有确认页的上下键语义尚未全部统一。首次启动短页不再误写箭头滚动；完整帮助仍未补齐。 |
| 部分实现 | 成功通知不再顶掉快捷键，已清理普通成功通知跨任务残留；不可用快捷键显示 unavailable，空状态与 NO_COLOR 活跃 tab 标记已补；其他错误原因和通知分类仍需继续检查。 |
| 已实现 | 确认取消统一回列表焦点；Accounts 调用 Profile 选择/取消回 Accounts，选择后重查当前环境；Settings 从调用页打开并返回；More 首项焦点及上下键；搜索先清空再离开输入；共享 footer 布局输出滚动上限，输入/缩放后 clamp。 |
| 已实现 | Profiles 使用现有字段/选中样式；Accounts 分开 Profile、登录状态、已存登录，明确空状态，路径与详细错误放 i Details；空列表 s 给出添加入口；Accounts/Profiles 的 ? 帮助可达。 |
| 部分实现 | Review 与 Working 使用用户动作名（不显示 query-resume）；窄屏当前/聚焦 tab 保留标题，其余 tab 只显示图标；目录删除列表后重复的完整路径/预览；普通成功通知离开任务清除，并使用普通成功样式。其他页标题、通知分类与紧凑列表仍需继续统一。 |
| 部分实现 | Profiles/Accounts 主层级、空账号、路径 Details、窄屏导航与目录重复路径已改；剩余标题/Details 全面统一、会话空 attention 行压缩尚未完成。 |
| 未实现 | 创建时 recovery setup 的行式交接/取消残留；同步 service 调用移到命令；终端颜色能力与 ASCII fallback。 |
| 仅方案，未落地 | 早先八张 Profiles/Accounts/Rename/Resume 视觉示意；其中保留按钮的部分已被用户“去掉按钮”决定替代。 |
| 尚未验证 | 真实账号登录/浏览器授权、完整外部 Magpie bind、Magpie 实际模型回合/恢复、断线重连、嵌套 tmux/SSH 异常、suspend/panic 清理。 |

当前实现的测试与截图说明见 [视觉报告的实现记录](ui-visual-review.md)。以下保留原始观察，避免把旧问题记录误当作新版本表现。


## omp 参考控件与全页减噪（2026-11-17）

本轮按用户提供的实际截图调整所有 Harbor 共享控件；参考本机安装的 omp 18.22.0 对应源码提交 `3e8cff3b7ce19a805c11a26a6dd1703066435851` 的 [ScrollView](https://github.com/can1357/oh-my-pi/blob/v18.22.0/packages/tui/src/components/scroll-view.ts)、[viewport](https://github.com/can1357/oh-my-pi/blob/v18.22.0/packages/tui/src/components/scroll-viewport.ts)、[按键格式化](https://github.com/can1357/oh-my-pi/blob/v18.22.0/packages/tui/src/key-hint-format.ts) 和 Settings selector。未引入 TypeScript 依赖；在既有 Go 组件内实现等价的显示规则，保留 Harbor 调色板。

搜索框共用 Bubbles 文本输入渲染，聚焦时光标落在实际编辑位置，包括空 placeholder 首字符；未聚焦不显示光标。按键显示符号与实际 dispatch 分开，Esc 仍保持清空、离开输入、返回的层级。自动滚动条为单列灰色轨道/现有 brand 色滑块；比例由实际总行数、可见行数与偏移计算，末尾可到达，独立于固定底栏。Profile 列表按实际底栏留出空间；小窗口 tab 保留全部导航目标，展开当前或聚焦项文字。

Accounts 总览只显示 Profile、登录状态、已存账号与一条空状态；技术限制、错误详情移入 Details。Profile emoji 仅位于顶层 tab。Settings 删除默认常驻说明；客户端无结果不再因 Enter 添加第二条说明；Start/Resume 删除复述标题的句子，Stop/Remove 等操作后果及跨提供方披露仍保留。

验证：最终 cnb 隔离全量 `GOWORK=off go test ./...` 通过（60.732 秒）；Profile 可见行数/输入态底栏调整的相关回归通过（8.025 秒）。真实 Herdr 0.9.5 / Codex 0.169.0 在外层 160×45、100×30、80×24、60×24 验证搜索空闲/聚焦/输入/清空、Accounts/Details、Settings、19 个 Profile 的滚动列表、客户端无结果、Connections、帮助长文档与缩放。单独进入 Start 审阅确认无重复正文，未执行启动、真实登录或模型调用。

本次 fixture 的 Herdr、tmux 和工作区进程已清理。证据保留在本地忽略目录 `ui-omp-20261117`，包含实际带色 ANSI、按键事件和仓库 Unicode 15 renderer 再渲染 PNG；不是设计稿或原生像素截图。前面的实现状态表以本节为最新视觉规则；以下续改记录是上一轮的验证历史。

## 续改实现记录（2026-11-17）

在 `ui/flow-audit` 保留前序未提交修改继续实现，未同步、改写或合入 main。代码集中在 `app_feedback.go` 的页面边界处理、`app_ui.go` 的共享滚动上限和底栏，以及 Accounts、Profiles、More、搜索等输入处理。新增 `app_flow_continuation_test.go` 覆盖取消后第一次 Enter、调用页返回、越界滚动、More 和搜索分层；原帮助测试改为有效滚动位置，避免要求恢复越界 offset。

运行使用 cnb 独立源码目录、HOME/XDG/Codex、tmux socket、项目和 Herdr，未登录账号或发模型请求。客户端 Codex 0.169.0，Herdr 0.9.5 本地候选。账号工具缺失的 unavailable 状态和空账号反馈均实际查看。四种外层尺寸为 160×45、100×30、80×24、60×24；以 Herdr 内部弹窗空间为准，80 列仍保留侧栏，60 列侧栏折叠。真实按键包括 Profile 创建/选择、Accounts 调用返回、Settings 取消返回、i Details、? 帮助、s 空账号结果、默认实例取消、客户端 unmatched 搜索的两层 Esc、文档上下滚动，以及 More 上下选择重命名、移除确认取消后第一次 Enter 打开 Start 审阅。没有确认启动该合成会话。

最终共享布局版本的隔离 `GOWORK=off go test ./...` 通过（88.493 秒）；最后两处 Profile 标记/子页图标和客户端空结果不可用提示调整后，相关回归再次通过（0.321 / 0.337 秒），并重建运行检查。真实长帮助在 100/80/60 列到末尾后第一次 Up 均立即移动；160 列该帮助可完整显示，无需滚动。fixture 进程已通过本次隔离 stop 清理。

带色 ANSI 和按键事件保留在本地忽略目录的 `ui-flow-continuation-20261117` 证据包；PNG 是仓库 Unicode 15 renderer 对终端捕获的再渲染，不是设计图或原生像素截图。renderer 新增 80 列识别和 `--rows`，防止 60×24 捕获按旧 60×20 高度裁掉底部。

仍未完成：recovery setup 行式交接及取消后的记录语义；所有同步 service 调用异步化；Magpie 确认与 Session 确认的导航全面统一；Settings 的独立 ? 帮助；ASCII fallback/终端颜色能力；完整真实账号、模型调用和精确恢复验收。恢复/停止的真实操作未执行，取消焦点由共享确认路径、状态测试和实际移除取消路径验证，不能当作真实恢复成功。

## 原始走查结论

本次结论：优先修焦点和返回路径，再统一快捷键、表单和确认页，最后压缩布局。上一轮只有合成渲染和模型测试，不能算完整实机验收；以下是补做的 cnb/tmux 实际结果。

## 实际环境和方法

- Harbor 基线 `0934148`，分支 `ui/flow-audit`；没有合入另一个会话的 main 修改。
- cnb Linux，独立 tmux socket、HOME/XDG/Codex/Profile、项目、Herdr 和 Magpie；标准发现端口由本次 fixture 独占，未连接已有用户服务。
- Herdr 0.9.5 本地恢复候选、原生 Codex 0.169.0、codex-auth 0.4.0。Magpie 最初使用旧 0.1.3528-harbor-dev，Manage 被 Profile 能力检查拒绝；只替换本次 fixture 的 Magpie 为已有 Profile isolation 候选后，实际进入上游管理页。相同版本字符串不能证明候选能力相同。
- 真实按键通过 tmux 发送；鼠标用 SGR 事件送入真实 Herdr。每步保存 `capture-pane` 文本和带色 ANSI，步骤号可对应证据。不是 fake service，也不是直接调用 UI Model。
- 实走外层终端 160×45、100×30、80×24、60×24。外层 100 列时弹窗内部只有约 60 列；80 列时侧栏仍在，内部更窄；60 列时 Herdr 隐藏侧栏，内部反而变宽。原先按整个终端宽度渲染，漏掉了这层真实约束。

## 优先修的交互问题

| 优先级 | 实际操作与证据步骤 | 实际结果 | 修法 |
| --- | --- | --- | --- |
| P1 | Profile 默认实例：鼠标选 managed（122），再进入，Tab 走到实例并 Enter（123–125） | 鼠标已保存 managed；再次键盘操作却保存 shared。Tab 到实例没有可见焦点，Enter 使用另一个方向键 cursor。 | 统一焦点、选中值和 mouse/keyboard activation；保存前显示具体 Profile 与实例。 |
| P1 | 恢复确认 → Details → Esc → Esc 回列表 → Enter（170–175） | 第一次 Enter 完全无反应；第二次才进入恢复确认。停止确认取消后也出现一次（157–161）。 | 退出子页时清掉遗留的 review focus，恢复到列表或原按钮。 |
| P1 | 多个 Profile，在外层 80×24 打开 Magpie default，再 PgDown（128–130） | 候选实例都在屏幕外，PgDown 无效。60×24 也复现（131）。 | 默认实例做独立 picker；按真实弹窗尺寸分配可滚动内容，固定操作区。 |
| P2 | Accounts → Choose Profile → 创建/选择 Audit A（51–58） | 选择结束跳到 Sessions，用户还得再按 a 回 Accounts。Settings 从 Accounts 打开后 Esc 也回 Sessions（63–65、94）。 | 保存调用页和局部状态，任务完成/取消回调用页；明确区分顶层导航与子任务。 |
| P2 | 新建名称输入 Shift+Tab（29–30），对比 Rename 输入 Shift+Tab（48–49） | 前者向前跳到 Apply，后者正确去 Cancel。同一个输入操作，反向 Tab 规则不同。 | 统一输入→Apply→Cancel 的双向循环，输入焦点也进入同一套 focus model。 |
| P2 | More 菜单 Down（44–46） | 没反应；Tab 首先跳到顶部 Sessions 标签。菜单无初始焦点，夹着不可见 list 焦点。 | 初始焦点放首个动作；上下键选菜单，Esc 回 More 按钮。 |
| P2 | 客户端搜索 unmatched → Esc → Enter（22–26），对比模型搜索（208–211） | 客户端 Esc 只离开输入、保留 unmatched；空列表仍有 Select，Enter 无反馈。模型/Settings 搜索第一次 Esc 会清空并保留输入焦点。 | 统一 Esc 分层；空结果显示原因，禁用 Select 并提供清空入口。 |
| P2 | Accounts 在 80×24 连续 Down 80 次，再 Up 一次（227–229） | 两帧完全相同。滚动状态超出正文末尾，反向滚动需要消耗积累的空步。 | 更新时就 clamp offset，resize 后也重算。 |
| P2 | 新建 → Create → recovery setup → 取消（36–43） | 离开弹窗、先创建 tab/record，再切换为 yes/skip/Enter 的行式提示；Esc/方向键变成输入字符。取消后留下 Unknown 会话和错误终端，列表又提供 Start session。 | 创建前说明交接与取消结果；setup 使用明确一致的取消约定，失败记录区分“未启动”与未知运行状态。 |
| P2 | 退出 Magpie 管理后进入 New（190–195） | 旧的 Magpie action completed 出现在新建表单，顶掉正常按键提示，还附带 Details。 | 按页面/动作管理通知，成功消息及时消失，不跨任务污染表单。 |

## 快捷键和焦点一致性

以下是实机按过的行为，不是从帮助文本抄出的设计。

| 页面 | ↑↓ / Tab | Enter / Esc | 帮助与问题 |
| --- | --- | --- | --- |
| 首次启动 | ↑↓ 选连接，Tab 走按钮 | Enter review/confirm；Esc 回上级 | footer 却写 ↑↓ scroll（2–5），操作语义不符。 |
| Sessions | ↑↓ 选会话；Tab 穿过导航、按钮、搜索 | Enter 打开/恢复；搜索 Esc 分层退出 | `?` 可用。`p` 能开 Profiles，但帮助漏掉 p；工作区快捷键另由 Herdr prefix 管。 |
| More | ↑↓ 不选动作；Tab 先去导航 | 快捷字母可执行；Esc 返回 | 不符合可见的垂直菜单形状。 |
| 项目选择 | ↑↓ 选目录，Tab 走控件；鼠标选取/滚轮已送入 | Enter 使用，Esc 返回表单 | 三排按钮没有展示各自 Ctrl 快捷键（234–236）；只能看代码才知道 Browse/Recursive/Known/Retry 的组合键。 |
| 客户端选择 | Tab 当作下一项，而非像目录页一样走控件 | Enter 选行；搜索 Esc 不清空 | Search/Select/Cancel 有可点击按钮，但键盘 focus 模型与其他 picker 不同。 |
| 新建名称 / Rename | Shift+Tab 方向不同 | 都用 Enter Apply，但按钮分别靠上、靠底 | 同一个编辑任务用了不同页面壳和焦点规则。 |
| 实例/模型选择 | 上下选，Tab 控件，PgUp/PgDown 滚动 | 搜索 Esc 先清空，再退出输入 | 空模型列表不解释为什么 Acknowledge & apply 不可用（199–200、209）。 |
| 会话 Stop/Resume/Remove 确认 | ↑↓ 在动作间移动 | Esc 取消；Resume 默认聚焦主动作，Stop/Remove 默认取消 | 这个默认焦点区别有理由，应保留；取消后的焦点残留必须修。 |
| Magpie 操作确认 | ↑↓ 滚正文，Tab 才移动按钮 | 默认 Cancel，再 Confirm；Esc 返回 | 与会话确认外观近似，但上下键含义、按钮顺序和位置不同（103–104 对比157–158）。 |
| Accounts | ↑↓ 滚正文，Tab 操作 | s 选择、o 退出登录状态；Esc 返回 | `?` 无响应（52）。无已存账号时 s 仍执行，结果只有 signed-out，缺少“没有可选登录”的说明（59–60）。 |
| Profiles | ↑↓ 选行，Tab 导航/按钮 | Enter 选择；d 进入默认实例 | 默认实例的 Tab 与 Enter 脱节；选中 Profile、当前 Profile、焦点三个状态容易混淆。 |
| Settings | ↑↓/Tab 控件；编辑时输入接管 | Enter 仅接受字段，另需 Save；Esc 取消 | `?` 无响应（66），i 才是 Details。搜索 Esc 先清空，这点比客户端 picker 更一致。 |

工作区快捷键实际验证：在 Settings 把 Harbor `prefix+a` 改为 `prefix+t`，先尝试切页被未保存保护挡住（91），保存后 Herdr 标签改为 `^B t`（93）；关闭弹窗，旧 `Ctrl+B a` 不再打开（95–97），新 `Ctrl+B t` 成功打开（98–99）。Settings 的字段接受、整页保存和导航防丢稿机制有效，不应在改版时丢掉。

建议统一交互约定：Tab/Shift+Tab 总是遍历控件；列表 ↑↓ 选行；文档 ↑↓ 滚动；所有确认页 ↑↓ 选动作，正文使用 PgUp/PgDown；Esc 依次清空搜索、离开输入、退一层；`?` 在非文本输入时始终打开本页帮助。Ctrl+C 保留退出，Ctrl+Z/suspend 另做生命周期验证。

## UI 不一致和视觉问题

1. **表单壳不统一。** 新建名称的 Apply/Cancel 在正文下，Rename/Profile 编辑在底部，Settings 又有 Accept field 和整页 Save。统一固定操作区，清楚区分字段草稿和持久保存。
2. **确认页不统一。** 会话确认按钮垂直靠上，Magpie 确认按钮水平贴底；主动作和 Cancel 顺序也不同。保留按风险决定的默认焦点，统一位置和按键语义。
3. **选中与聚焦混用。** Profile 默认实例是最严重实例；选行标记、按钮焦点和 active Profile 不是同一状态。每页必须明确哪一个是 Enter 的目标，且切页后可见。
4. **页面标题不统一。** `HARBOR`、`Harbor`、有/无锚点图标、`New session`/`Choose client`/`Review query-resume` 并列。`query-resume` 是内部动作名，不该当用户标题。Profile 页缺少与其他主页面一致的图标/上下文。
5. **搜索和提示不统一。** 客户端空搜索没有 no-results 文案；模型搜索亦缺解释；目录 Ctrl 操作不可发现；帮助入口分散在 ?、i、!、Details。成功通知跨页变成带 Details 的异常式呈现。
6. **真实弹窗空间浪费。** 目录页只有一个项目，却同时显示目录行、完整路径、再一次完整路径（234）。搜索框占三行，项目辅助按钮占两排以上；会话每条固定三行，即使第三行为空。80×24 外层下这种固定开销比第一轮全屏渲染更严重。
7. **噪声来自重复字段和说明，不是多层边框。** Harbor 内层只有搜索框边框，但真实环境还套了一层 Herdr popup。应压缩搜索框和重复说明，不要再加卡片边框。上游 Magpie 的长 footer 在弹窗内被裁切（185、187–189）；需要明确上游界面边界，不能假称全由 Harbor 控制。
8. **窄屏丢内容不是单纯变丑。** Profile 默认候选项不可见是功能阻断；长 Profile、project、model 文本被截，很多页没有直接可发现的查看全文入口。60 列侧栏折叠后重新排版也应纳入截图基线。

## 已实走和仍未覆盖

实际走过：首次启动选择/review/cancel；Sessions 空/运行/结束列表、搜索、More、Details、Rename、Stop/Remove 取消；新建目录/客户端/名称/Native-Magpie 切换、实例/模型空结果和非空选择/apply/cancel；Profile 创建/选择/default/大量数据；Accounts signed-out/选择已存登录的空结果/sign-out result；Settings 搜索、字段编辑、未保存导航保护、保存、快捷键重开；Magpie start、进入上游六个页面、q 返回；原生 Codex hook review、真实首轮、退出、同一 Conversation 恢复。

恢复成功的实机证据是 169–177：保存 reference 的确认、Details、取消和重进、Resume 后原来 `AH_LOCAL_FIRST` / `HELLO_FROM_ISOLATED_MOCK` 对话重新显示。这个过程用本地合成 provider，没有真实账号或用户对话。

仍未做：真实账号 Add login/浏览器授权、真实外部 Magpie bind 后的完整旅程、Magpie 连接下真实模型回合和恢复、连接中断/重连、tmux 嵌套/SSH 异常、Ctrl+Z/suspend/panic 清理、实际字体/主题的截图对比。没有把这些标成通过。带色 ANSI 能保留终端样式，但不是屏幕像素截图，也不能单凭它宣称颜色对比度或字体效果验收通过。

前一轮通过的全部 Go 测试只证明旧回归集通过，不证明这些新发现不存在。原始走查阶段没有修改产品 UI，也没有动 main；后续实现见顶部状态表。实机证据和探索脚本保留在本地忽略目录，下面附原来的静态检查记录供对照。

---

# UI flow audit — 2026-11-17

Baseline: `0934148`; isolated branch `ui/flow-audit`. This is an audit of the baseline, before UI fixes or the concurrent main bug fixes.

## Findings

| Priority | Flow / reproduction | Observed problem | Proposed change |
| --- | --- | --- | --- |
| P1 | Profiles → Magpie default → Tab to an instance → Enter | The focused `profile-instance:<id>` is not dispatched by the keyboard handler. Enter saves `profileDefaultCursor` instead. A synthetic reproduction focused `other` but saved the empty shared default and reported success. Mouse and keyboard can save different targets. | Unify keyboard and mouse activation; make focus and selected value explicit. |
| P1 | 30 Profiles → Magpie default at 60×24 | The Profile list and ordinary actions consume the viewport; all default-instance choices fall below the bottom. The page has no document scrolling/reveal mechanism for them. At 80×24 the longer default page can also exceed the viewport. | Open a dedicated instance picker, reserving its action row before allocating list height. |
| P2 | Accounts → repeatedly Down past the end → Up | Rendering clamps the offset but state does not: an offset of 500 becomes 499, leaving the displayed content unchanged. Similar unbounded offsets exist in onboarding, connection subdocuments and settings details. | Clamp stored offsets to the rendered content budget on every scroll/resize. |
| P2 | Sessions → More… → ↑/↓ | The menu shows a vertical action list but ignores arrows; only Tab or direct shortcuts work. `sessionsFocusIDs` also includes an invisible list target on this page. | Use a dedicated menu focus list and support arrows; restore focus to More when returning. |
| P2 | New session → client / instance / model selector → search with no results | Client and instance pickers leave Select enabled without a matching row, and show no explicit no-results explanation. The model picker disables Apply but does not render its reason in the body. | Show an empty-state message and a visible disabled reason; offer clear search/back. |
| P2 | NO_COLOR: top navigation and unavailable actions | The active tab is encoded only by background color; disabled buttons lose their visual distinction and their reasons are generally not shown next to the button. | Add an active-tab text marker and explicit unavailable text/reason. |
| P2 | Shrink to 40×10; many Profiles | No truthful minimum-size state. Profile Back can be clipped; content and actions continue to be silently truncated. | Define the minimum size from essential controls and display a resize message below it. |
| P2 | Profiles create/rename/select/default; client/model selection | Several service calls execute synchronously in key handlers, and the model selector calls `AuxiliaryAcknowledged` while rendering. Disk/locking latency can freeze input or redraw. | Move effects into commands with busy/error state and stale-result protection. This is a code-path risk, not a measured production stall. |
| P3 | Sessions / onboarding / directory picker / Connections | Too much fixed chrome and repeated explanatory text; important data and actions get less space than repeated hints. Exact counts below. | Reduce search to one line when inactive, collapse secondary actions, put extended explanations in Details. |
| P3 | Help, details, review and terminal compatibility | Sessions help omits `p Profiles` and the quit key; first-run footer says arrows scroll although they select choices. Detail pages foreground internal evidence labels. Page rendering always emits raw truecolor escapes when NO_COLOR is absent; no ASCII rendering option was found. | Make hints page-specific; show task information first and evidence on demand; respect detected color capability and provide ASCII fallback. |

Code entry points: `bridge/app_profiles.go` (default activation and layout), `bridge/app_accounts_ui.go` (scroll), `bridge/app_sessions_ui.go` (More and list shell), `bridge/app_new_ui.go` and `bridge/app_selector.go` (empty selectors and effects), `bridge/app_ui.go` / `bridge/app_style.go` (tabs, disabled buttons, painting), `bridge/app_secondary_ui.go` (first-run footer), `bridge/app_view.go` (help/details).

## Density and responsive behavior

- Border nesting depth is **one**, around search only. There is no excessive nested-panel problem. Replace the three-row search box with a one-row search field; do not add more panel borders.
- At 80×24, Sessions spends **6 rows on header/navigation/new/refresh/search and 3 on actions/hints: 9/24, or 37.5%** of height before displaying session data. Each session takes three rows even when its attention line is empty. That leaves five visible sessions.
- At 60×24 navigation wraps to two rows, leaving four session cards. New/Refresh and selection actions occupy another two rows. Two bottom hint lines repeat Enter/search/help; keep one contextual hint row.
- At 100 columns the right preview repeats the selected name and client/connection information already in the list. At 160 columns the same two-column arrangement simply gets wider. Prefer a short primary row with project/connection metadata and show evidence only in Details.
- At 60×24 onboarding uses six vertically stacked action rows plus one hint row; Previous/Next duplicate the arrow-key operation. Remove these two buttons and retain clear arrow hints.
- The directory picker has five rows before its status and three button rows before its results. With a single known directory the full path can appear twice, followed by a preview. Keep Use/Cancel visible; fold Browse/Recursive/Known/Retry into secondary controls and show the full selected path once.
- Connections repeats instance name/status in the row and selected-instance section, then adds a separate lifecycle block and a long repeated action hint. At 60×24 the bottom help is below the viewport. Keep ownership visible but shorten repeated explanations.
- Every session status has the same `●` prefix, which does not distinguish states. The useful state signal is its text; color reinforces it. Selection uses both `›` and a background; these serve monochrome access and need not be removed.

Current layout and suggested density direction:

```text
wide (>=90 columns)                 narrow (60 columns)
Harbor / Profile                   Harbor / Profile
[Sessions] Connections ...          [Sessions] ... (compact navigation)
New  Refresh  Search: ...           New  Refresh  Search: ...
> Name / state | Project            > Name / state
  project      | Connection          project / connection
  connection   | task summary      ...one pane; i opens full details
...            |                   ...
Open  Details  More                Open  Details  More
contextual keys / notice           contextual keys / notice
```

The current Sessions layout already switches to one pane below 90 columns; preserve that useful behavior. Standard 80×24 and 60-column layouts should keep the selected item and essential actions visible. The hard minimum is currently undefined and needs to be established while fixing the page budgets.

## Verification scope

The audit harness generated plain rendered frames for 25 states (150 frames) at six sizes: 160×45, 100×30, 80×24, 60×24, 60×20 and 40×10. Synthetic fixtures contain Unicode/CJK names and no credentials or real conversations. States cover onboarding, list/More, details/help/error, confirmation/evidence, project picker, new-session form/client/name/connection/instance/catalog, Connections/bind, Accounts/busy/result, and Profiles/default/create.

Model-event probes reproduced the wrong default target and Accounts overscroll. The complete isolated `GOWORK=off go test ./...` run passed (86.726s); a focused UI run also passed (53.353s). These are existing regressions tests, not assertions that the audit findings are fixed. A real PTY smoke using the synthetic model exercised an 80-to-60-column resize followed by `q` and Ctrl+C separately: both exited successfully, restored termios, left the alternate screen and showed the cursor. The harness answered terminal background/cursor queries before sending user keys; earlier attempts without that handshake were invalid and are not product failures. Frames and exploratory test code stay in local ignored audit material, not product code.

This does not establish real login, live upstream recovery, browser handoff, network-disconnect timing, mouse behavior inside actual Herdr/tmux, suspend/resume, or panic cleanup. Those require separate isolated real-runtime checks. Color contrast and emoji alignment in an actual terminal were not visually measured; plain frame checks cannot establish them.
