# Agent Harbor Recover：用户问题记录与后续排查入口

日期：2026-11-16。状态：**开放，尚未修复或完成复现验收**。
代码基线：`c88c08880893a665eb9066473f87e922a1cafe19`。

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
2. 再覆盖工作区重启后的 Recover；比较恢复前后确切的 Conversation、Profile、连接实例和模型选择，避免恢复成新对话或其他对话。
3. 覆盖 hooks 无报告的正常运行；确定上游能可靠提供身份的路径，避免把缺失报告永久转换为无法恢复。
4. 核对正常退出后的状态是否及时更新、是否存在重复/过期 Herdr 观察，以及恢复按钮的各个前置条件。分别修复实际复现的根因，不预设所有失败同源。
5. 基本恢复稳定后，再讨论 Codex 内切换 Conversation 后的绑定同步和持续跟名。不要统一用全局“最近会话”代替每个窗格自己的已确认 ID。
6. 说明文字随功能一起整理，避免重复确认；验收必须包含真实恢复成功，不能仅以移除黄字为通过。

遵循 [测试隔离要求](../bridge/tests/README.md)；保留现有用户服务、凭据和历史。确需用户提供失败样本时，只采集必要元数据与错误文本。

## 另一个已讨论、未实施的问题：轮询开销

每秒观察 Herdr 的循环至少在初始提交 `154ec7d` 已存在，不是命名功能新加的定时器。当前每个活跃会话正常情况下每秒启动一次 `herdr pane get`，身份更新还会保存会话记录；写入路径含文件和目录同步。命名增加记录读取，成功后不再扫描标题索引。尚无 CPU、I/O 或耗电基准。

用户关心性能；可调查事件通知、连接复用、只在状态变化时落盘和缓存。先保证状态与恢复正确，避免在这份问题记录中将尚未测量的收益当成事实。
