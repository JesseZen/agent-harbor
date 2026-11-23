# Herdr 里复用现成的系统监控

研究快照：2026-11-19。Harbor 的 Herdr 上游基线为 `ee5bfb5`。本轮核对了上游接口，并用 Harbor 随包的 Herdr 0.9.5 验证本地弹窗；没有修改产品代码或现有工作区。[Harbor 的 Herdr 基线与补丁](../herdr-integration.md#herdr-integration-and-local-patch)

## 结论

**可以直接复用现成的终端程序，不必改 Herdr 或把仪表盘塞进 Harbor 的 Sessions。** 第一版建议加一个独立的 **System monitor** 快捷键，按需在 Herdr 弹窗里运行目标机器已有的 `btm`（bottom）；`btop` 是更流行的替代项，更适合大弹窗或临时放大 pane。关掉监控回到原来的工作界面，不占常驻侧栏。只有确实需要经常瞄一眼时，再在标签栏右侧加可选短摘要，保留现有 Harbor 提示。[Herdr 基线：快捷键与弹窗](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/docs/next/website/src/content/docs/configuration.mdx#L165-L215) · [Harbor 当前弹窗和状态命令](../../bridge/herdr.go#L245-L280)

## 独立监控工具

| 候选 | 平台、指标与窄屏 | 许可与分发 |
| --- | --- | --- |
| **bottom（`btm`，约 14.2k stars）** | 官方支持 macOS Intel/Apple Silicon 和 Linux x86_64/arm64 等。CPU、RAM、swap 有走势；磁盘表可显示挂载点、已用/可用、`R/s`、`W/s`。`--basic --default_widget_type disk` 能同屏显示 CPU、RAM 和磁盘表，但也带网络等次要信息，不支持自定义布局。首选用普通模式的独立配置，做成 `cpu` → `mem` → `disk` 三行；要 I/O 趋势再加 `disk_io_graph`。Harbor 入口用 `--read_only`，不提供终止进程的操作。[项目](https://github.com/ClementTsang/bottom) · [平台](https://bottom.pages.dev/stable/support/official/) · [Basic](https://bottom.pages.dev/stable/usage/basic-mode/) · [布局](https://bottom.pages.dev/stable/configuration/config-file/layout/) · [磁盘列](https://bottom.pages.dev/stable/configuration/config-file/disk-table/) · [只读配置](https://github.com/ClementTsang/bottom/blob/0.15.0/sample_configs/default_config.toml#L116-L121) | [MIT](https://github.com/ClementTsang/bottom/blob/0.15.0/LICENSE)；上游有 [0.15.0 发布包](https://github.com/ClementTsang/bottom/releases/tag/0.15.0)，也可由用户自行安装。 |
| **btop（约 35.5k stars）** | 支持 macOS 和 Linux。内存框可显示磁盘已用/可用空间，`io_mode` 显示读写速率图。窄屏可用独立配置 `--config`，设 `shown_boxes = "cpu mem"`、`show_disks = true`，再按挂载点筛选；`--tty` 是字形/颜色降级，不会取消尺寸限制。v1.4.8 源码的最低 PTY 尺寸：CPU/内存/网络/进程四框为 **80×24**，仅 CPU/内存为 **60×18**（均不含 Herdr 边框），两种都装不进本轮的 60 列弹窗。[项目与配置](https://github.com/aristocratos/btop#configurability) · [尺寸算法](https://github.com/aristocratos/btop/blob/v1.4.8/src/btop_tools.cpp#L111-L138) · [各框尺寸](https://github.com/aristocratos/btop/blob/v1.4.8/src/btop_draw.cpp) | [Apache-2.0](https://github.com/aristocratos/btop/blob/v1.4.8/LICENSE)；[v1.4.8 发布页](https://github.com/aristocratos/btop/releases/tag/v1.4.8)有 Linux 预编译包，无 macOS 正式发布包；macOS 可用 [Homebrew](https://formulae.brew.sh/formula/btop)。 |

**别把容量、吞吐量、繁忙度混成一个“硬盘%”。** 容量属于某个文件系统（例如项目目录所在挂载点），I/O 速率来自设备计数器在采样间隔内的变化，不代表该项目的流量；没取到 I/O 不能显示成零。bottom 0.15.0 在 Linux/macOS 均接入磁盘 I/O 采集，但设备与挂载点的对应关系要按实机验收。btop v1.4.8 的 macOS I/O 活动条用读写字节量估算，并非设备忙碌时间；上游还有非 APFS / 整盘卷漏报 I/O 的[反馈](https://github.com/aristocratos/btop/issues/1895)。该版 btop 的 macOS 已用 RAM 是 active + wired，也不应要求和活动监视器逐项一致。[bottom 采集源码](https://github.com/ClementTsang/bottom/blob/0.15.0/src/collection/disks.rs#L35-L82) · [btop 的 macOS 计算](https://github.com/aristocratos/btop/blob/v1.4.8/src/osx/btop_collect.cpp#L1114-L1163)

## 三个现成的 Herdr 插件

| 插件 | 可借用的部分与接入风险 |
| --- | --- |
| [getpipher/herdr-sysmon](https://github.com/getpipher/herdr-sysmon)（9 stars，[MIT](https://github.com/getpipher/herdr-sysmon/blob/main/LICENSE)） | 最接近「CPU、RAM、剩余磁盘放侧栏」；发布命名为 `@getpipher/herdr-sysmon`，仓库与插件 ID 仍是 `getpipher`。0.1.1 的清单只支持 macOS，需 `jq`，安装说明要手动合并侧栏配置，启动钩子另起常驻轮询。它把同一份整机数字推给每个工作区；磁盘只读 home 所在卷的剩余空间，CPU 是首样本（源码注明为开机累计平均），RAM 页大小写死为 16 KiB，另会定期 `ping` 外部地址。可以参考排版，不建议原样带入 Harbor。[npm 包声明](https://github.com/getpipher/herdr-sysmon/blob/main/package.json#L1-L13) · [清单](https://github.com/getpipher/herdr-sysmon/blob/main/herdr-plugin.toml) · [采样](https://github.com/getpipher/herdr-sysmon/blob/main/scripts/metrics.sh#L6-L62) · [轮询](https://github.com/getpipher/herdr-sysmon/blob/main/scripts/poll.sh#L60-L101) |
| [thewtex/herdr-mem-cpu-load](https://github.com/thewtex/herdr-mem-cpu-load)（3 stars，[Apache-2.0](https://github.com/thewtex/herdr-mem-cpu-load/blob/main/LICENSE)） | 最适合借用**单行摘要**，macOS/Linux 可直接调用单次输出，不必装成插件；也有 `--watch` 的小弹窗。只有 CPU、RAM、load，没有磁盘。整插件安装会在未配置 Space 布局时写 Herdr 配置，启动钩子会跑 daemon；不建议直接安装到托管工作区。若复用单次输出，不加 tmux/ANSI 样式，命令超时须大于 CPU 采样窗口。[单行输出与平台](https://github.com/thewtex/herdr-mem-cpu-load#one-line-mode) · [0.1.2 清单](https://github.com/thewtex/herdr-mem-cpu-load/blob/main/herdr-plugin.toml#L11-L52) |
| [ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay](https://github.com/ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay)（33 stars，[MIT](https://github.com/ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay/blob/main/LICENSE)） | 最接近「工作区用量 + 打开独立监控」：终端插件在 macOS/Linux 按 pane 的进程树估算 CPU/RSS，不是项目配额，脱离进程树的任务可能漏算，共享内存也不是独占 RAM；整机磁盘容量只出现在标题/报告/仪表盘，不在工作区侧栏，侧栏标题显示整机数字要额外打 Herdr 补丁。它的监控入口本身就是打开 `btop`/`htop`/`top`。整插件安装要编译 Rust，默认后台跑采集且首次写入 `[ui.sidebar.spaces]` 的 `$usage` 行。值得参考入口做法，不必为了打开监控引入整套插件。[配置写入](https://github.com/ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay#zero-setup) · [磁盘](https://github.com/ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay#disk) · [进程归属](https://github.com/ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay#how-it-works) · [监控入口与后台钩子](https://github.com/ezcorp-org/herdr-pc-ram-and-cpu-usage-overlay/blob/main/herdr-plugin.toml#L29-L123) |

Stars 是检索时的 GitHub 快照，只反映关注度；这三个小插件都没有在 Harbor 的打包版中验收。Harbor 的 Settings 保存和在线升级都会拒绝不认识的生成配置。**不要把插件的配置片段直接追加到 Harbor 托管文件，也不要让插件替 Harbor 改它**；如果做成产品入口，应由 Harbor 的配置生成与迁移路径写入。[Settings 校验](../../bridge/settings.go#L153-L169) · [在线升级校验](../../bridge/popup_upgrade.go#L98-L102) · [配置所有权与回滚](../herdr-integration.md#live-popup-upgrade)

## 放哪、怎么落地

1. **首选：独立快捷键 + 按需弹窗。** Harbor 已用 `[[keys.command]]` 的 `type = "popup"` 装 Sessions / Magpie / Settings。新增独立入口前做键位冲突检查，名字进入 `prefix+?`，不要复用已有键。Herdr 弹窗是模态的，`Esc` 也交给应用；用应用自己的 `q` 退出。窄屏按弹窗实际 **内侧** PTY 宽高取舍：先放弃 I/O 列，保留 CPU、RAM 和磁盘容量；需要完整列时放大窗口或用临时放大 pane。若需要鼠标菜单，可用插件 action 打开 `placement = "popup"`，不需要后台采样插件。[Herdr 基线：快捷键](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/docs/next/website/src/content/docs/configuration.mdx#L190-L240) · [插件弹窗与单实例限制](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/docs/next/website/src/content/docs/plugins.mdx#L300-L340) · [Harbor 快捷键](../../bridge/settings.go#L18-L45)
2. **其次：标签栏右侧的可选短摘要。** `[ui].tab_bar_right` 已有 `_status`，可以在其旁放一个短时、低频且带超时的纯文本命令，例如 `CPU 24% · RAM 9/32G · / 82G free`。Herdr 只取成功输出的最后一行，窄屏可能隐藏整块右栏；它不是可点击的仪表盘入口，也不适合跑 `btm`/`btop`。不要在每个工作区侧栏重复同一份整机数字。[Herdr 基线：状态栏](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/docs/next/website/src/content/docs/configuration.mdx#L318-L336) · [现有 `_status`](../../bridge/herdr.go#L245-L263)
3. **以当前选中的 Herdr 服务器为准。** 远程工作区的终端和状态命令在服务器上执行，本机只画界面；远程所需程序也装在服务器，不依赖客户端 `PATH` 或复制本机自定义命令。显示机器名；若在容器里，只承诺进程可见的数据，不把它冒充宿主机、工作区配额或 Harbor 会话用量。断线或工具缺失时说明是哪台机器不可用，不能悄悄换成本机或展示成实时值。[Herdr 基线：远程访问](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/docs/next/website/src/content/docs/persistence-remote.mdx#L36-L63) · [机器切换与断线](https://herdr.dev/tui/docs/connecting-machines/#switch-between-machines) · [Harbor 远程边界](../remote-workspaces.md)

先做可选外部依赖，缺工具给安装提示，不自动下载、不要求 `sudo`，不改变 Harbor 启动条件。以后若要随包分发，保留对应许可证、版权和适用的 `NOTICE`，检查二进制所含依赖许可，并按 Harbor 的 macOS/Linux × arm64/amd64 四目标锁版本、核对资产和验收。[Harbor 分发约定](../../distribution/README.md#release-maintenance) · [Apache-2.0 §4](https://www.apache.org/licenses/LICENSE-2.0#redistribution) · [bottom 的 MIT 条款](https://github.com/ClementTsang/bottom/blob/0.15.0/LICENSE)

## macOS 实测

用校验过的官方 bottom 0.15.0（macOS arm64）、Harbor 随包的 Herdr 0.9.5，以及隔离的 HOME、XDG、Herdr 和 tmux socket。测试键为 `prefix+u`，`width` 和 `height` 均为 `95%`。命令为 `btm --read_only --rate 2s -C <独立配置路径>`；配置用上述单列布局，CPU 默认选平均线，磁盘列依次为 `Mount`、`Free`、`Total`、`R/s`、`W/s`。这些配置只用于夹具，不直接写进托管工作区。[上游样例配置](https://github.com/ClementTsang/bottom/blob/0.15.0/sample_configs/default_config.toml#L194-L219) · [Herdr 弹窗尺寸算法](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/src/popup_size.rs#L50-L89)

| 外层终端 | 弹窗内侧 PTY | 实际可见 |
| --- | --- | --- |
| 160×45 | 124×39 | CPU、RAM、磁盘容量、读写速率列 |
| 100×30 | 67×25 | 同上 |
| 80×24 | 48×19 | CPU、RAM、磁盘容量、读速率列；写速率列隐藏 |
| 60×24 | 54×18 | 同上；Herdr 已隐藏侧栏，实际比 80 列弹窗更宽 |

```text
宽屏                           60 列外层
CPU avg [trend]                CPU avg [trend]
RAM  [used / total]            RAM [used / total]
Mount   Free   Total  R/s W/s   Mount  Free  Total  R/s
```

不增加常驻侧栏行，也不在 Herdr 弹窗与工具的分区框之间再套 Harbor 页面。按 `q` 后弹窗消失，同一 pane ID 接受了新的 shell 命令。合成 `tab_bar_right` 标记在 160/100/80 列可见，60 列不可见；未接入真实摘要采样。亮色/无色、再小的窗口、Linux、SSH 断线、磁盘 I/O 数据准确性，以及 Harbor 的配置迁移、工具缺失提示和旧快捷键回归，都还需要验收。[Harbor TUI 验收约定](../agents/tui.md#regression-and-completion) · [隔离测试指南](../../bridge/tests/README.md)
