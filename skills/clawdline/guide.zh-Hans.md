# Clawdline 指南

本文供执行 **Clawdline Next** 的机器上的助理 Session（Claude Code 或 Codex）使用。
它只涵盖此守护进程目前提供的功能：下列每条路由都由产生本指南的版本注册；若有遗漏，测试会失败。
请执行 `clawdline guide` 重新打印，不要依赖副本；`clawdline guide zh-Hant` 会打印台湾繁体中文版（`zh-TW` 仍是别名）。
`clawdline guide` 会打印核心内容并列出其他部分；进行某项工作时，可打印对应部分
（例如 `clawdline guide zh-Hans dispatch`），也可用 `clawdline guide zh-Hans all` 打印全文。
任何打印内容（包括核心内容）都以 `guide-version: <sha256>` 开头；用 `--since <hash>` 执行相同命令时，
若内容未变，则只打印 `unchanged <hash>` 一行。
`clawdline guide zh-Hans refused <code>` 会打印说明该拒绝码的部分；若没有任何部分提及该码，则标准输出为空并以状态 1 结束。

在本指南中，**步骤**是工作项目检查清单中的一条，任务的 **writes** 是它可以修改的路径，
**assignment** 表示工作项目的负责人，**部分**是本指南中具名的一段内容。

## 0. 如果你从 Swift 应用程序认识 Clawdline，请先阅读这里

Swift 应用程序已于 2026-09-19 退役：它已停止运作，不再于登录时启动，端口 7717 也不再有回应。
其目录 `~/.config/clawdline` 仍在磁盘上，且仍会被读取（仅供读取），用于取得此守护进程从未保存的历史资料。
此守护进程不是旧应用程序的副本；以下五项差异特别容易造成误解：

1. **任务目录是 `<state dir>/tasks`，不是 `/tmp/.clawdline`。** `/tmp/.clawdline` 属于 Swift broker；
   如果两个 broker 把任务 ID 写进同一目录，就会在不易察觉的地方发生冲突。两者都不要写死：
   从清单（§3）读取 `task_root`，再在其下写入 `task.json`。
2. **没有工作流程信封，也没有需要调用的工作流程路由。** 消息不再携带看板分类，Session 也不会自行创建看板卡片。
   `POST /v1/orchestrator/sessions/<terminal>/workflow` 仍存在，只是为了避免旧辅助工具在轮次中途失败；
   它会回复 `workflow_retired`，不记录任何内容，新代码不应调用它。
3. **参与看板要透过提案与决定**（§10）：Session 提案，由人作答。没有任何需要 `begin` 或 `deliver` 的操作。
4. **入口不同。** 端口为 7727（或 `CLAWDLINE_NEXT_PORT`），状态存于 `~/.config/clawdline-next`
   （或 `CLAWDLINE_NEXT_DIR`）。绝不要读取 `~/.config/clawdline`：其中的令牌不属于此守护进程，会收到 `401 unauthorized`。
5. **Swift 应用程序曾有、此守护进程没有的功能：**持久报告晋升（回复
   `501 durable_report_promotion_unsupported`）、协调者继任（回复 `501 succession_unavailable`），
   以及任务简报中的 `serialize` 和 `attach_session` 栏位（各自以 `bad_task` 按名称拒绝）。
   `reasoning_effort` 仍受支持，但仅限 `codex` 任务，值为 `high` 或 `xhigh`。

## 1. Root 与 child

如果第一则消息写着 *「You are a Clawdline CHILD agent for task …」*，你就是 **child**。
消息所指的 `CHILD.md` 是你的工作规则：你不派发任务、不送出轮次收据，使用 `clawdline task accept` 签收，
并以 `clawdline task finish` 完成。读到这里即可停止。

否则，你就是 **root**：与人对话的一般 Session。以下内容供你使用。

如果消息写着 *「You are an independently owned Clawdline Feature Root …」*，或有看板工作项目指派给你，
接着打印 `clawdline guide zh-Hans feature-root`：它涵盖从读取项目到标记 `done` 的完整一般流程，
也会指出较少见情况应打印哪个部分。

## 2. 连接守护进程

**有现成命令时，请使用命令，不要自行拼装 curl。** 命令会在自身进程内读取凭证，
因此凭证不会出现在命令行、`ps`、命令输出或你的会话记录中。
未附凭证的自制 curl 会收到 `401 unauthorized`（「No valid credential came with this request …」）：
缺的是凭证，不是权限。请改用命令。

| 命令 | 用途 |
|---|---|
| `clawdline guide [lang]` | 本指南；不需要守护进程 |
| `clawdline session report --summary "…"` | 记录已完成的轮次（§7） |
| `clawdline session close [--dry-run] [--terminal id]` | 稽核并关闭已完成的 Session，绝不强制关闭（§2a） |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | 派发自己负责的 child（§4） |
| `clawdline item show\|steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | 读取并推进自己负责的看板项目（`clawdline guide zh-Hans feature-root`、§10） |
| `clawdline todo add\|list\|done` | 管理此 Session 自己的待办事项，仅在用户要求时使用（§10） |
| `clawdline heavy -- <command…>` | 在此机器唯一的编译时段中执行建置或测试套件（§11） |
| `clawdline send --to <terminal> "…"` | 将消息转送至另一个 Session（§8） |
| `clawdline notify --title "…" --body "…"` | 推送通知给用户（§9） |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | 在 Session 上方留下一则可采取行动的 Note（§9a） |
| `clawdline assistants` | 各助理帐户的剩余额度 |
| `clawdline landings` | 此机器上所有尚待落地的项目；`--work-id <item id>`：某看板项目的所有落地记录 |
| `clawdline leases [--json]` | 谁占用编译时段与各落地租约，以及谁在排队等候 |
| `clawdline sessions [--json]` | 可供发送消息、等待或交接指定的 Session，及其状态与任务 |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | 按类别查看 Session、child 任务或看板项目的耗用量；预设为自己 |
| `clawdline cloud pair [--offer <code>]` | 将一个 Cloud 浏览器与此机器配对 |
| `clawdline task show [--json] <task id>` | 简要显示一个 child 任务的状态、判定、摘要、遗留事项标题、验证、落地和工作目录（§5） |
| `clawdline task wait <task id>… [--timeout 9m] [--any]` | 等待 child 完成（全部，或以 `--any` 等待其中一个），依 `task show` 的格式显示每项结果并关闭通知。全部成功时结束码为 0、一项失败为 1、没有失败但有取消为 5、逾时为 3、任务无法读取为 4；优先顺序为 4、3、1、5（§5） |
| `clawdline callback --title "…" [--timeout 30m] [--work-id <item>] -- <command…>` | 由守护进程执行长时间命令（例如部署及检查、等待 CI）并立即返回；结束此轮次后，命令结束时会显示与已完成 child 相同类型的 `<clawdline-notice>`（§5a、`clawdline guide zh-Hans callback`） |
| `clawdline task cancel <task id> --reason "…"` | 停止误派的 child：关闭其分页，释放其写入范围与时段，保留已有提交的分支供你处理（§5） |
| `clawdline task ack <task id> <notice id>` | 手动关闭完成通知；很少需要，因为 `task show` 和 `task wait` 也会关闭通知（§5） |
| `clawdline task accept <task dir>` | child 签收简报；root 绝不执行 |
| `clawdline task finish <task dir>` | child 回报完成；root 绝不执行 |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | 在任何机器透过 Cloud webhook 启动调度并等待结果；结束码表示最终状态（「调度未来工作」）。不需要守护进程 |

没有明确指定指南语言时，CLI 依序采用命令前的 `--lang <tag>`、`CLAWDLINE_LANG`、
已保存的 `product_language`，最后采用英文。明确指定 `clawdline guide <tag>` 会覆盖该选择；
不支持的语码显示英文。`clawdline guide -list` 列出九个随程序提供的语码。
此偏好只改变供人阅读的 CLI 文字，不改变协议字段或 Agent 的语言。

上述编排命令（不包括 `webhook fire`）成功时会打印守护进程的 JSON；遭拒时会先打印
`refused, <status> <code>: <message>`，再逐行以 `key: value` 列出拒绝内容中的纯量，
最后列出补救方式，并以状态 1 结束。Cloud 命令有自己的易读成功及错误输出。`--port` 可覆盖端口。

`clawdline usage` 是令牌帐本（仓库中的 `docs/token-ledger.md`）：记录每个令牌用于
`board`、`protocol`、`rules`、`impl`、`delegate`、`harness`、`talk`、`compaction` 或 `other`。
不加参数时，它会读取由 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID` 指定的自身 Session。
输出先有一行标头（调用次数、最高上下文、费用），接着按费用列出每个类别的占比、令牌数与费用，
最后列出所有资料缺口；`--json` 则打印守护进程的原始回复。`rules` 明确标示为上限：
若保护检查与其他工作在同一个 shell 命令中执行，该命令的全部耗用量都会计入。
路由为 `GET /v1/usage/sessions/<conversation>`、`GET /v1/usage/tasks/<task id>` 和
`GET /v1/usage/items/<item id>`，可透过已配对装置或编排令牌读取。
若帐本尚未读取或已无法读取某 Session，会回复 `not_yet_read`、`transcript_missing` 或
`transcript_unreadable`，绝不回传空的总量；未知 ID 会得到 404 `unknown_session`、
`unknown_task` 或 `unknown_item`。可在 `/v1/diagnostics` 的 `usage` 查看帐本是否仍在读取。

**等待长时间命令。** `clawdline heavy`、`clawdline dispatch` 与长时间测试在等待时不会输出，
之后会自行结束。请以**一次长等待**等候，别每隔数秒查询一次：每次查询都是重新读取整份上下文的轮次。
一次令牌审查在十个项目中统计到 520 次这类轮次（7220 万令牌），大多发生于排队中的 `heavy` 执行。

- **Claude Code：**使用一次带长 `timeout`（最高 `600000` 毫秒）的 Bash 调用，或使用
  `run_in_background`，然后等到完成通知抵达。不要反复执行 `sleep` 和 `tail`。
- **Codex（codex-cli 0.157.1，code mode）：**在 `functions.exec` 单元格第一行放置
  `// @exec: {"yield_time_ms": 600000}`。`exec_command` 回传 Session ID 后，以空的 `chars`
  和 `yield_time_ms: 300000` 等待 `write_stdin`；若仍在执行，就在同一单元格内重复。
  实测使用 `600000` 时，外层单元格可保持开启 330 秒，空的 `write_stdin` 最多等待 300 秒。
  若外层单元格暂停，请以较长的 `yield_time_ms` 调用 `wait` 收取结果。

  等待 child 或建置时使用单一单元格。只替换命令，将 Session 轮询保留在单元格内，
  以免 `exec_command` 正常经过 30 秒回传时唤醒助理，重复发出相同等待：

  ```js
  // @exec: {"yield_time_ms": 600000}
  let r = await tools.exec_command({cmd: "clawdline task wait --timeout 9m TASK_ID", yield_time_ms: 30000});
  while (r.session_id) {
    r = await tools.write_stdin({session_id: r.session_id, chars: "", yield_time_ms: 300000});
  }
  text(r.output);
  text(`exit ${r.exit_code}`);
  ```

建置时，将命令改成 `tools/heavy.sh …`，并保留其结束码：75 表示建置尚未开始，等待编译时段或内存就已逾时。

`clawdline heavy` 最多等待 `--max-wait`（预设 30 分钟），之后会以状态 75 结束而不执行命令；
若所需等待时间超过工具允许值，请改为背景执行。

**使用 curl 访问编排路由。** 读取 `<state dir>/orchestrator-token`，并在
`X-Clawdline-Orchestrator` 标头送出。避免令牌出现在命令参数中：先设定
`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`，再使用
`-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`。
使用 `curl --fail-with-body`；每个带有 JSON 本文的 POST 还需加入
`-H 'Content-Type: application/json'`，否则会收到 `415 unsupported_media_type`。

### 配对 Cloud 浏览器

配对会改变谁能读取这部机器。已配对的浏览器可立即读取；若 Cloud `commands` 已开启，也能操控机器。
只有当用户明确要求配对该浏览器，或提供确切的配对命令或邀请码时，才执行配对命令。
配对不会开启命令功能；那是另一个独立设定。

支持两种配用户向：

1. **浏览器显示邀请码。** 在机器上执行它提供的确切命令：

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   保留单引号。邀请码是不透明、短效且仅能使用一次的密钥：不要解码、修改、保存，
   也不要在最终答复中重述。若已过期或被使用，请从浏览器取得新邀请码，不要重试或修改旧码。
2. **机器发出邀请。** 执行 `clawdline cloud pair`。它会打印一次性的
   `https://app.clawdline.com/#pair=…` 链接并等待。用户登录同一个 Clawdline Cloud 帐户，
   在要配对的浏览器中开启完整链接。把链接视同邀请码：不要公开或保留。

成功时会打印三行：`paired` 是浏览器装置 ID，`browser` 是浏览器指纹，`machine` 是机器指纹。
请比对浏览器指纹与浏览器上显示的值，以及机器指纹与此机器上显示的值。
不一致不算成功：立即以 `paired` 的 ID 执行 `clawdline cloud revoke <device-id>`，再回报差异。
`clawdline cloud devices` 会列出目前的浏览器及其本机信任状态，也可作为配对后的只读检查。

这些命令都透过正在执行的本机守护进程。若失败，回报其确切 stderr。
除非用户另行要求，否则不要开启 Cloud、登录、启用命令、轮换金钥或替换所提供的邀请码。

**位置。**

- 端口：`CLAWDLINE_NEXT_PORT`，否则为 **7727**。仅限回环：`http://127.0.0.1:<port>`。
- 状态目录：`CLAWDLINE_NEXT_DIR`，否则为 `$XDG_CONFIG_HOME/clawdline-next`，再否则为
  `~/.config/clawdline-next`（Windows 上为 `%APPDATA%\clawdline-next`）。
- `GET /v1/health` 不需要凭证，回复 `served_by: "clawdline-go"`。用它区分「未执行」与「遭拒」。

**凭证。** 共有三种，Session 使用第一种：

| 凭证 | 位置 | 送出方式 | 可访问范围 |
|---|---|---|---|
| 编排令牌 | `<state dir>/orchestrator-token` | 标头 `X-Clawdline-Orchestrator` | `/v1/orchestrator/`、`/v1/work/`、`/v1/board` 下的全部路由，以及 `GET /v1/places`、`POST /v1/artifacts/images` |
| 任务密钥 | root 派发时选定 | 标头 `X-Clawdline-Task-Secret` | child 自己在 `/v1/orchestrator/tasks/<id>/` 下的路由，以及 `POST /v1/orchestrator/proposals` |
| 装置令牌 | `<state dir>/local-token` 或已配对装置的令牌 | `Authorization: Bearer` | 控制台路由（`/v1/sessions/…`）；Session 不需要它 |

若把编排令牌当作 `Bearer` 送出，系统会用装置令牌规则比对并拒绝。
令牌错误或缺少时会收到 `401 unauthorized`，其消息会说明：令牌缺失、不属于此守护进程，或装置未配对。
`clawdline doctor` 会打印 CLI 读取的目录和端口。

**必须使用 curl 时**，不要让令牌出现在命令行中：

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

 - `--fail-with-body`：没有它，遭拒请求也会以状态 0 结束，看起来像成功。
- **每个带本文的 POST 都需加入 `-H 'Content-Type: application/json'`**，否则会收到
  `415 unsupported_media_type`。单用 `curl -d` 会送出表单类型。
- 除非路由规定更小的上限，本文大小上限为 2 MiB。
- tmux 终端机 ID（例如 `%47`）放入路径时，需当作单一路径区段逸出为 `%2547`。

**拒绝回复有两种格式。** 请依拒绝码分支，绝不要依消息文本分支：

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}`：闸门与 broker。
  `retry_after` 等额外栏位位于 `error` 中。
- `{"error":"<code>","detail":"…"}`：路由不存在、方法错误及部分读取请求。

此守护进程不负责的路由会收到 `501 not_implemented`，拒绝消息会指出路由。
在一般机器上，这就是最终回复。只有有人刻意用 `CLAWDLINE_NEXT_UPSTREAM_PORT`
在此程序后面设定另一个守护进程时，请求才会转送；若上游没有回应，
`502 upstream_unreachable` 会指出无回应的地址。两者都不是此守护进程提供的路由回复。
2026-09-19 以前，转送预设开启并指向端口 7717 的 Swift 应用程序，因此当时的笔记可能写着
不属于此程序的路由会抵达旧应用程序；现在不会。

### 设置 Project 的 Clawdline 显示信息

当用户要求你让目前工作的 Project 在 Clawdline 中清楚呈现时，请阅读本部分。
成果不只是「有几个文件」：Project 应有准确的名称和标记，长时间工作能回报进度，
而且 Clawdline 无需启动开发服务器，就能显示它们的状态。

先阅读此仓库的规则、README、部署与建置脚本，以及现有进程管理器设定。
保留 Project 已使用的命令。不要只为 Clawdline 添加第二套部署流程或进程监督器；
除非用户要求该操作变更，否则不要启动、停止、重新启动或部署任何东西。设定与实际部署是不同工作。

依序完成以下四项检查，只有确实不适用时才略过：

1. **Project。** 执行 `clawdline project list`。若清单中没有此工作目录，
   用 `clawdline project add <absolute-root>` 加入仓库根目录，再重新列出。
   这只记录 Session 可以从哪里开始，不会修改仓库。
2. **名称与图示。** 未设定图示时，Clawdline 会产生稳定的图示。
   若用户希望指定名称或像素标记，请保留 `~/.claude/project-icons.json` 的其他所有项目，
   只编辑包含此 Project 的最长路径对应项目。格式见 Clawdline 仓库中的
   `docs/project-status.md`；Projects 页面也能复制已解析的现有图示，无需手动编辑 JSON。
   全域用户文件不属于仓库；若原要求尚未授权修改它，请先展示确切的拟议项目。
3. **部署与长时间工作。** Clawdline 只读取状态收据，绝不执行部署。对 GitHub 仓库而言，
   部署收据位于 `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`，其中 owner 和 repo 来自 `origin`。
   已知执行状态的产生者会以原子方式写入 `state`（`running`、`ok`、`fail` 或 `none`）、
   `label`、`url`、`started_at`，以及实测的 `typical_seconds`。
   对本机建置、测试、汇入或部署命令，若有辅助工具，请使用
   `clawdline-progress run --label <label> -- <command>`；否则依 `docs/project-status.md`
   实现 `run-<path>.json` 契约。绝不要杜撰耗时；测得之前先省略。
   若产生进程遭终止，不得留下永久显示为执行中的状态。
4. **开发服务器。** 在最近的可部署根目录添加或更新 `.devstack.json`。
   Go 守护进程目前会读取声明的 `processes`，探测其回环 `port` 或开启其 `url`；
   它**不会**从浏览器执行 `status`、`up`、`down`、`restart` 或 `logs` 命令。
   请优先使用最小且准确的 Tier 0 文件，例如：

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   没有稳定端口或 URL 的进程，不应凭猜测加入文件。验证开发环境声明时，不要探测或重启正式环境。

逐层验证每项变更：`clawdline project list` 列出此工作目录；每个 JSON 文件均可解析；
仓库针对已修改脚本的测试通过；`GET /v1/devstacks` 将已声明的服务器显示为执行中、已停止或未知，
而不是默默省略；Project 中的 Session 显示新的进度或部署收据。
若某次读取无法执行、内容格式错误或资料过旧，请指出哪一项并保留未知状态，绝不要把缺失回报成成功。
最后列出已设定内容、刻意判定不适用的项目，以及在 git 之外修改的任何用户文件。

**统一 Claude 与 Codex 的规则和技能**（`/clawdline unify`）。Codex 读取 `AGENTS.md`
和 `.agents/skills/<name>/`；Claude 读取 `CLAUDE.md`（只有在 `CLAUDE.md` 不存在或含有
`@AGENTS.md` 一行时才读取 `AGENTS.md`）及 `.claude/skills/<name>/`。
若 Project 的规则存于 `AGENTS.md`，`CLAUDE.md` 不存在或汇入它，且所有技能均存于
`.agents/skills/<name>/`，并由 `.claude/skills/<name>` 以相对链接指向，则为已统一。
当用户要求统一，或调用 `/clawdline unify` 时：

1. 在 Session 的 Project（git 顶层目录）执行 `clawdline project unify`；若 Project 未列出，
   先用 `clawdline project add` 加入。此命令不修改内容。
   以用户的语言展示 Claude 和 Codex 目前及之后各自会读取的内容、每列技能、每句操作说明与每项冲突，
   包括 Codex 看不到的 `CLAUDE.md` 行。
2. 只有在用户于本对话中亲自发送消息批准该计划后，才执行 `clawdline project unify --apply`。
   此命令会送出你展示的版本；若磁盘内容已变，则回复 `plan_changed`，不套用任何变更。
   这时应重新打印计划，再次询问。
3. 显示 `clawdline project unify --check` 的结果（结束码 0 表示已统一、1 表示偏移、3 表示未知）。
   它不会提交任何内容；请说明哪些文件已变更，以便用户或某个 Session 提交。

没有用户的消息授权时，不要自行编辑 `AGENTS.md`、`CLAUDE.md` 或技能来解决冲突：
两个目录中的技能不同、链接指向别处，或 Codex 看不到某些规则，都应由用户决定。
路由为 `GET /v1/projects/{place}/unify`（计划）与带 `{"version"}` 及
`Idempotency-Key` 的 `POST /v1/projects/{place}/unify`；拒绝码为 `plan_changed`、
`plan_unknown`（Project 一部分无法读取，因此没有变更）及 `name_taken`
（统一操作要创建的名称已存在；不覆盖任何内容）。

## 2a. Feature Root 的一般流程

以下是一般 Feature Root（负责一个看板项目的 Session）应依序执行的完整流程。
每个步骤都是命令：命令会携带凭证，自行拼装 curl 调用同一路由会遭拒。
较少见的工作可用 `clawdline guide <part>` 查阅；本部分末尾列有指引。

**1. 读取项目。** `clawdline item show <item id>` 会打印种类、阶段、验收标准、已记录的关卡、
验收版本（`acceptance vN`）、Feature 的「Needs independent review」开关、步骤及每份文档的内文。
这就是你的工作依据。`clawdline item show <item id> --doc <doc id>` 只打印一份文档的内文，
便于导入文件；`clawdline item steps <item id>` 会列出相同记录，但不含文档内文。
每次写入项目后都会显示 `wrote …; item <id> is at version N`、简短项目摘要，以及 `item show` 提示。
请用 `item show` 读取完整验收标准、步骤和文档。除非传入 `--expected-version`，
写入会依项目的目前版本执行。

若你的 ASSIGNMENT.md 有 **HANDOFF** 标题，表示你接手另一个 Session 进行到一半的项目
（开始实现后、完成前重新指派）。规划前先读取该标题指出的交接包。
守护进程根据自身记录及 git 创建交接包，无需询问前一位负责人；其中包含与项目绑定的任务及其结果、
尚未落地的提交、各工作目录未提交变更的修补档及其 sha256、在原基底恢复修补档的 `git apply` 命令、
前一位负责人的最后消息（或无法读取的原因）、目前阶段与未完成步骤。
修补档放在 ASSIGNMENT.md 旁，工作目录消失后仍会保留。请从那里继续，不要从头开始。

**2. 为 Session 命名。** 若此 Session 是为该项目开启的，在读取目标与范围后执行一次
`clawdline item name <item id> "<task name>"`。这会重命名 Session，不会重命名项目。

**3. 实现前。**

- 若已开启记录规划，但没有验收标准，请用
  `clawdline item acceptance <item id> --body-file acceptance.md` 写入可观察的标准。
- 若「Needs independent review」已勾选，或项目为 Epic，先依 `clawdline guide zh-Hans epic` 中的审查计划流程执行。
  未勾选时，不需要计划或审查 child。
- 多阶段工作尚无步骤时，用 `clawdline item step-add <item id> "first" "second" …`
  创建两至八个可逐一验证的步骤；单一变更无需步骤。
- 然后执行 `clawdline item phase <item id> implementing`。

**4. 预设在此 Session 工作。** 自行调查、实现、验证并落地 Feature。
只有具体需求使独立 Session 有用时才派发：真正可独立并行的工作、不同工具或权限，
或必要的独立审查。派发前说明原因；一般调查或实现本身不构成理由。

需要派发时，请把综合判断、集成与落地留在此处：

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` 将 child 绑定到项目，使其落地可计入该项目。若一个 child 处理多个项目，
  请重复此参数：第一个是 child 的主要项目，而落地会计入每个项目。
- 标题最多 60 个字符，应以一行说明完成后有何变化。任何冒号（`:` 或 `：`）都会遭拒，
  因为它把观察与解释接在一起；以「the user」作主词，或以代码格式的识别符开头，也会遭拒。
  这些情况都会回复 `bad_task`，并在 `title: …` 中说明原因。
- 简报必须能独立阅读。请纳入已验证的事实，逐一附上 `file:line` 或证明它的命令，
  让 child 不必重新发现。
- 调查或 Explore child 的简报还需写明停止条件（回答哪个问题即可结束任务）及轮次上限。
- 只读工作使用 `--claims ""`。所有参数与拒绝码见 `clawdline guide zh-Hans dispatch`。

**5. Child 完成时，**输入框会出现一行 `<clawdline-notice>`。执行
`clawdline task show <task id>`，再集成交付内容；读取该任务会关闭通知，不需另行 ACK。
**派发后，结束你的轮次**：通知会唤醒你；若一直让轮次开着等待，每次轮询都会重新读取整份上下文。
只有在没有其他工作且必须阻塞等待时，才执行 `clawdline task wait <task id>…`
（预设 `--timeout 9m`；`--any` 表示等第一个完成的任务）。
集成使用工作目录的 child 时，**合并其分支**到目标分支。
**合并会在几分钟内自行记录落地**，不要手动提交落地记录。
`clawdline landings` 会列出尚待落地的任务。以 `--claims ""` 派发且没有写入的 child，
broker 会记录为 `nothing_to_land`。其他情况使用 `clawdline task land <task id> <state>`
（见 `clawdline guide zh-Hans landing`）。

**6. 完成报告。** 若查明原因需要大量调查，请撰写报告；直接且可观察的修复无需报告。
在标记 `done` 前加入：项目完成后会解除指派，届时写入报告会得到 `409 not_item_owner`。

```sh
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

请用 Markdown 写给回报问题的人，且不要包含私人资料。

**7. 完成项目。** 每个步骤验证后，用 `clawdline item step-done <item id> <step id>` 标记完成。
直接工作应从可抛弃的工作目录提交并推送；若使用 child，请合并其分支。
落地后，一个命令即可将项目设为 `done`。child 的落地记录会提供提交、目标及远端信息；
直接工作则需明确指定：

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --commit <sha> --target main --remote origin \
  --deployment "what went live, where, which version"      # omit landing flags for a landed child
```

也可以一次推进一个阶段：

```sh
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> deploying --no-landing-reason "why there is no code"    # work with no code
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

依项目的部署政策，设定 `done` 时需带上 `--deployment` 或 `--no-deployment-reason`。
若 `clawdline item steps <item id>` 打印关卡行，该项目就需遵循该行指出的较长流程。

**等待部署。** 部署执行或传播期间，不要让轮次保持开启。
将部署与检查作为同一个 callback 启动，结束轮次，收到通知后再完成项目：

```sh
clawdline callback --title "The hosted console serves <sha>" --work-id <item id> --timeout 20m -- \
  sh -c './deploy.sh <sha> && tools/wait-hosted-console.sh <sha>'
# … the notice: clawdline task show <callback id>, then
clawdline item finish <item id> … --deployment "what went live, where, which version (callback <callback id>)"
```

**8. 回报轮次：**`clawdline session report --summary "…"`（§7）。

**9. 保持负责此项目的 Session 开启。** 完成或取消看板项目会解除指派，
不会结束原本负责它的 Session。执行 `session report` 后，保留 Session 供后续工作使用。
不要只因项目达到 `done` 或 `cancelled` 就执行 `clawdline session close`。
用户之后可以明确要求关闭 Session。broker 会依 `clawdline guide child` 的 child 分页规则，
在 agent 派发的 child 任务结束后另行关闭其分页。

**遭拒时。** `version_conflict`：重新执行相同命令，它会重读版本。
`steps_incomplete`：仍有步骤未完成。其他拒绝码：先看 §12，再看其对应部分。

**较少见的工作，各有对应部分：**`clawdline guide zh-Hans board` 说明提案、决定、待办事项、
重新开启已完成项目、等待用户、关卡及各阶段的拒绝码；`clawdline guide zh-Hans epic` 说明计划、
计划审查、Epic 子项目与 persona；`clawdline guide zh-Hans landing` 说明手动落地、交接
（包括长时间 Root 的里程碑交接）及 Root 指派；`clawdline guide zh-Hans running` 说明停滞的 child、
遗留事项与重新产生任务。

## 3. 派发前，先读取现有工作

其他 Session 可能已在执行同一工作，而共享工作目录看不出来：
未合并分支上的已完成交付不会出现在 `git status`。请先查阅。

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- 回复包含 `generation`、`task_root` 及四个清单：`live`、`unlanded`、`droppable`、
  `unreadable`。每列都有守护进程可接受的 `do`。指定 `claims` 时，每个执行中项目也会显示
  与哪些写入范围 `overlaps`。
- **派发时必须提供 `generation`**（§4）。它是依各列封存栏位产生的 16 位十六进位字符串；
  列开始、结束或更改写入范围时都会变动。
- **`task_root` 是放置 `task.json` 的位置。** 这是此守护进程自己的栏位；
  Swift broker 因将 `/tmp/.clawdline` 写死，所以没有此栏位。
- 若 `project` 不是 Git 仓库内的绝对路径，会收到 `400 bad_request`。

另有两项值得读取一次：

- `GET /v1/orchestrator/inflight?project=…`：仓库内所有尚未完成的工作、负责人及其声明的写入范围。
- `clawdline assistants`：各助理的 `availability`（`ok`、`low`、`exhausted`、`unknown`）、
  `windows`、`stale`、`resets_at`。先读取，再选择派发给谁；系统不会因配额拒绝派发。

**是否真的需要派发？** 可拆成独立部分的工作，并行通常更快。
若每一步都依赖前一步，拆开反而更慢，因为每次交接都会中断连续性。
诊断、比自身简报还短的工作，以及有人正在等待的工作，请留在自己的 Session。
此机器的工作规则位于 `<state dir>/dispatch-policy.md`（以及用户的
`dispatch-policy.local.md`）；每个 child 都会在简报中收到这些规则。

## 4. 派发自己负责的 child

自己负责的 child 是你麾下范围明确的任务。**综合判断、集成与落地仍由你负责。**

**一个命令即可完成以下四个步骤**，简报可从标准输入或文件提供：

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--persona <id>] [--work-id uuid …] [--task-id uuid] [--label "…"] [--project-dir D] < brief.md     # or --instructions-file brief.md
```

此命令会产生 ID 与密钥，从清单读取 `generation` 和 `task_root`，写入 `task.json`，
再送出任务；若遇到一次 `stale_inventory`，会重读清单并重送一次。
它先打印 `dispatched <id> <state> [worktree <path>]`，接着逐行列出警告：
守护进程的警告，以及写入范围与你重叠的每个执行中任务。
`--json` 则打印守护进程的回复。遭拒时，stderr 会显示
`refused, <status> <code>: <message>`，接着逐行列出额外信息与补救方式，
并以状态 1 结束；本部分末尾的表格解释各拒绝码。
root 是你的对话，取自 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID`，否则由
`--conversation` 指定；除非 `--assistant` 另有指定，child 使用与你相同的助理；
除非 `--project-dir` 另有指定，Project 为目前目录的 git 顶层。
`--claims ""` 声明不写入任何内容的 child。守护进程开启工作目录与 child 分页时不会输出；
这是直到 child 存在才回复的单次请求，因此等待一次即可（§2「等待长时间命令」）。
密钥绝不出现在 argv、`task.json` 或命令输出中；令牌则和其他精简命令一样由命令自行读取。

若审查可能需要重试，第一次调用前先选定小写 UUID，每次尝试都以 `--task-id` 指定。
命令会在 `task.json` 旁私下保存原始派发意图，因此即使守护进程已重写简报，
相同的重试仍可重新送出。broker 会回传原任务 ID 及 `(replayed)`；
若简报有变，会在本机遭拒。命令逾时或输出遗失时，先查询
`GET /v1/orchestrator/tasks/<id>`，再判断是否失败。
若任务不存在，可用相同 ID 和简报重试。明确拒绝表示任务未创建；修正原因后也可重试。

`--persona <id>` 以内建 persona 启动 child（记录于 `task.json` 的 `persona`）；
若此版本没有该 ID，会在本机遭拒。任何种类（包括 `plan_review`）都不会预设使用 persona；
想使用时请自行指定 `code-reviewer`。`GET /v1/personas` 列出此版本提供的 ID；
persona 的说明见 §10 的 Epic 部分（`clawdline guide zh-Hans epic`）。

若调用端没有这个二进位程序，以下是命令所执行的步骤：

**1. 选择 ID 与密钥。**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 characters, lowercase
SECRET=$(openssl rand -hex 32)          # 64 lowercase hex
```

密钥由你透过 POST 本文送给守护进程，再由守护进程在 child 分页输入的一行中交给 child。
它不在 `task.json` 或派发回复中，你之后也不需要它。
（重新产生任务的回复是唯一带密钥的回复：它会带有副本的新密钥。）

**2. 读取清单**（§3），取得 `generation` 和 `task_root`。

**3. 写入 `<task_root>/<TASK_ID>/task.json`。** 守护进程从此文件读取简报，而不是从请求读取。
接纳任务时，它会验证内容，依接纳结果重写 `task.json`，并根据相同记录创建 child 的
`CHILD.md`，包括标题、指示、写入范围、交付物、种类与逾时。
因此 child 读到的是已验证的任务，且不需要读 `task.json`。

| 栏位 | 规则 |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | 与上一步相同的 ID |
| `assistant` | `claude` 或 `codex` |
| `project_dir` | 现有目录的绝对路径 |
| `title` | 显示在画面上：最多 60 个字符的一行，说明完成后有何变化。冒号（`:` 或 `：`）、以「the user」作主词，或以代码格式识别符开头，都会以 `bad_task`（`title: …`）拒绝 |
| `instructions` | 必填，最多 16 KiB。必须能独立阅读：child 不知道其他背景。纳入已验证的事实，每项附上 `file:line` 或命令；调查型 child 还需停止条件与轮次上限 |
| `claims` | **必填**：child 最多可写入 32 个相对路径。`[]` 表示不写入任何内容，并会收到警告（`claims_missing`） |
| `isolation` | `none`（预设），或 `worktree`（在自己分支上的私人工作目录） |
| `permission_mode` | `ask`、`edits` 或 `full` |
| `timeout_minutes` | 1–240，预设 30 |
| `kind`、`deliverables`、`model` | 选填；`model` 须符合 `[a-z0-9._-]`，最多 64 个字符 |
| `work_id` | 选填，所服务看板项目的 UUID |
| `persona` | 选填，内建 persona ID（`GET /v1/personas`）；预设不指定 |
| `auto_compact_window` | 选填，仅适用 Claude：child 开始压缩时的上下文令牌数（50000–1000000），或设为 `null` 表示不压缩。省略时遵循机器的 `claude_auto_compact_window`；除非用户设定，否则此功能关闭。此栏位适合比较执行结果，不适合日常简报：压缩可能遗失细节 |
| `root` | **必填**：`{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "project_dir": "<the same absolute repository path as project_dir>", "label": "…"}`。有角色范围的 root 需要 `root.project_dir`，以便守护进程验证其 Project 范围 |

**派发前，确认 child 的工作接口与启动模式能使用它必需的每个工具。**
简报应列出必要工具，root 则需确认选定接口确实提供它们。
Codex CLI child 不会只因某个权限参数，就取得 ChatGPT 桌面应用程序内建的 `@Browser`。
进行 UI、无障碍或响应式版面审查时，请派给真正具有 Browser/Computer Use 的接口；
或指定能达到同等验收结果的本机浏览器工具，例如 Playwright/Chrome CDP，并确认已安装。
若该接口可能要求应用程序、来源或 GUI 访问权，派发时使用 `--permission-mode ask`：
Codex 的 `full` 指非互动式 shell 启动（`--ask-for-approval never`），并不代表拥有所有工具；
若根本没有创建访问请求，Auto-review 也无从审查。
child 启动任务时需实际使用每个必要工具，而不只是检查命令名称。
若有工具不可用，应立即回报确切缺口，由 root 恢复访问或重新派发。
不能因 root 选了不兼容的工作接口，就把依赖工具的验收检查留作未验证而结束。

**`root.session_id` 是你的对话 ID，绝不是终端机 ID。** Claude Code 透过
`CLAUDE_CODE_SESSION_ID` 汇出它，Codex 则透过 `CODEX_THREAD_ID`。
守护进程用它把 child 归到你名下，并在 child 完成时通知你。
要确认该 ID 指向此分页，可调用 `GET /v1/orchestrator/whoami?conversation_id=<id>`，
回复会包含 `terminal_id`。

**4. 使用编排令牌派发：**

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

请透过标准输入送出本文（`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`），
让密钥不出现在 argv 中。回复为 `{ok, task, warnings?}`。
请读取 `warnings`：`claims_overlap`、`claims_missing`、`claims_ignored_for_worktree`、
`dirty_worktree_base`，以及 `work_not_placed`（指定项目尚无法移到看板上；
看板下一轮扫描会处理）。重送相同 ID 会回复已存储的任务并带有 `replayed: true`，因此可以安全重试。

即使分页无法开启，也会收到状态 200，但 `task.state: "spawn_failed"`。
使用编排令牌调用 `POST /v1/orchestrator/tasks/<id>/respawn` 可开启带新密钥的副本；
每个原始任务最多可重试两次。

派错 child（简报、范围错误或同一工作派发两次）时，不要让它占着时段与写入范围，
一直等到完成或逾时：立即以 `clawdline task cancel <id> --reason "…"` 停止（§5）。

**可能遇到的拒绝码**，依检查顺序排列：

| 状态 | 拒绝码 | 处理方式 |
|---|---|---|
| 409 | `task_unreadable` | 已存储此 ID 的任务，但无法读取；不要以相同 ID 重送 |
| 422 | `bad_task` | 消息会指出栏位。包括「No readable task.json under …」；请检查 `task_root` |
| 422 | `claims_required` | 加入 `claims` |
| 422 | `root_session_required`、`root_assistant_required` | 加入 `root.session_id` 和 `root.assistant` |
| 403 | `session_scope_mismatch` | 检查 `root.project_dir` 是否存在，且与 root Session 的 Project 及角色快照相符。修正简报或 CLI；不要要求用户更改 Project 设定 |
| 422 | `detached_route_required` | 你送出了 `root.poll_only`；这属于脱离式自动化（§6） |
| **409** | **`stale_inventory`** | `generation` 缺失或过旧。错误内含完整的目前清单：读取清单，重新判断，再以其中的 `generation` 重送 |
| 422 | `work_not_found`、`work_other_project`、`work_closed` | 指定的 `work_id` 不是项目、属于其他 Project，或已关闭 |
| 422 | `also_work_not_found` | `also_work_ids` 中某 ID 不是看板项目；在 `work_id` 之后以相同方式检查 |
| 503 | `store_unavailable` | 无法读取看板来检查指定项目；没有启动任何工作，请重送 |
| 409 | `graph_*` | 任务图的接纳规则（`graph` 栏位） |
| 409 | `no_child_capability` | 此平台无法开启 child；`missing` 会指出缺少什幺 |
| 429 | `squad_launch_capacity` | 太多 persona 启动仍在等待其 Session；稍后重试 |
| 429 | `rate_limited` | 十分钟内派发次数过多 |
| 422 / 409 | `root_unresolved`、`conversation_ambiguous` | 对话 ID 找不到执行中的 Session，或对应不只一个。请修正，不要改成脱离式任务 |
| 403 | `session_actor_required` | 带角色开启的 root，必须从该 Session 使用自己的 squad 能力派发 |
| 403 | `session_scope_mismatch` | 此处也可能出现：root Session 的 Project 与角色快照不符 |
| 503 | `squad_policy_unavailable` | 无法读取角色指派设定；没有启动任何工作 |
| 409 | `persona_disabled_for_auto_assignment` | 目标 Project 已关闭该 persona 的自动指派 |
| 429 | `over_capacity` | 你的 child 时段（预设 5）或机器时段已满；查看 `retry_after` |
| 409 | `workspace_busy` | 另一个 root 的写入范围重叠；错误会指出阻挡的任务 |
| 409 | `worktree_unavailable` | 无法创建私人工作目录 |
| 429 | `terminal_busy` | 所有终端机写入信道都忙碌；`retry_after: 5` |

## 5. 执行期间与完成后

child 透过 `clawdline task accept` 签收简报（送出 `/accepted` 或留下 `accepted.json`）；
计划变更时可以送一则进度笔记（`/progress`），最多推送五则通知（`/notify`）；
最后写入 `result.json` 并执行 `clawdline task finish`。你不调用这些路由。

- `clawdline task show <id>`：显示一个任务及其状态（`GET /v1/orchestrator/tasks/<id>`）。
  `GET /v1/orchestrator/tasks` 会列出任务（`?state=`、`?limit=`；最多 500 个）。
- **任务完成时，守护进程会在你的输入框输入一行 `<clawdline-notice>`。**
  其 `body` 是一个短句，包含任务、结束方式、此交付独有的事实（停滞、已释放的写入范围、
  分支、遗留事项数量），以及唯一需执行的命令 `clawdline task show <id>`。
  该命令行印任务后会关闭通知。通知 JSON（版本 3）含 `task`、`state`、`notice_id`，
  `outstanding`、`leftovers`、`claims_released` 仅在有意义时出现；结果由 `task show` 打印。
  若通知行未能输入，会按 5 至 300 秒的间隔重试。它已显示在画面上、但你尚未读取任务时，
  不会再次输入完整内容；改以 2 至 30 分钟的间隔输入简短 `task_reminder`，提示同一命令。
  最多输入八次，之后放弃。显示选单时不会输入；选单也不消耗这八次机会：
  通知最多等待 12 小时，选单消失后再输入。`task show` 会送出：

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  对已完成任务，`clawdline task show <id>` 和 `clawdline task wait <id>…` 会在打印后送出 ACK；
  `clawdline task ack <id> <notice_id>` 可手动送出并打印一行。再次 ACK 会回复 `changed: false`。
  未确认的通知列于 `GET /v1/orchestrator/completions`；
  `POST /v1/orchestrator/completions/reconcile` 会重新启用它们。
  已放弃的通知在你的 Session 下次闲置时，还会再输入一次。
- **即使没看见通知行，你仍能得知结果。** 每个轮次边界都应读取自己的
  `GET /v1/work/v2/agent/session-todos/<conversation id>`；其中的
  `unacknowledged_completions` 列出已完成但你尚未确认的每个 child，包含 `task_id`、
  `title`、`state`、`kind`、`result_path`、`notice_id`、`ack_path`，并说明通知仍在等待或已放弃。
  `clawdline session report` 会在收据后打印这些任务。逐一执行 `clawdline task show <id>`，
  然后集成；读取即为 ACK，会从两个清单移除通知。
  `task show` 会完整打印摘要并计算省略项目；`--json` 显示守护进程的完整回复，
  包括 symbols 和 artifacts。只有信息不足时才自行读取 `result.json`。
- **列出遗留事项的交付**（child 说明尚未完成的工作）本身不会改变任何内容。
  `task show` 会列出其标题。要提交其中一项供用户决定，请送出
  `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`；
  用户可选择追踪、稍后处理（Backlog）或不处理。在用户作答前，内容不会进入看板。
- **child 在收到简报后停住，会先受到提醒，再回报为停滞。**
  若简报已输入，但 child 尚未签收且画面闲置五分钟（提示符已出现、输入框空白、
  没有选单或工作中的文本），守护进程会输入一行指出其 `CHILD.md`，绝不含密钥。
  若再过五分钟仍未签收且闲置，任务以 `spawn_failed` 结束，判定会说明停滞，
  你会收到 `"kind": "task_stalled"` 而非 `task_finished` 的通知。
  请重新产生任务（`POST /v1/orchestrator/tasks/<id>/respawn`）或再次派发，然后确认通知。
  正在工作、显示选单或已签收的 child 不会收到这种输入提醒。
- **取消误派的 child**（简报或范围错误、重复任务）：执行
  `clawdline task cancel <id> --reason "wrong brief"`
  （`POST /v1/orchestrator/tasks/<id>/cancel`、`{"reason":"…"}`）。
  原因必填，最多 500 字节。任务以 `cancelled` 结束，原因成为判定；
  分页会关闭，写入范围与 child 时段会释放，你会收到一则含取消原因的通知。
  **提交不会被丢弃：**已提交的 child 会保留分支与工作目录，落地仍待处理，
  附注会说明其中的提交数；`task show` 与 `clawdline landings` 都会显示。
  可合并其中需要的内容，或用 `clawdline task land <id> abandoned` 记录为放弃。
  只有派发任务的 root Session，或透过控制台操作的用户可以取消；其他调用者会收到
  `403 not_task_root`。以角色开启的 Session 须送出自己的能力凭证
  （`session_actor_required`；命令会代你处理）。
  已结束任务会回复 `409 task_already_terminal` 及其 `state`；重复执行同一取消命令，
  则会回复相同成功结果并带 `replayed: true`。
  `clawdline task wait` 等待的任务若遭取消，会以状态 5 结束；
  其他任务则以完成、失败或逾时结束。
- **child 完成不代表代码已落地。** 在你集成前，它的工作仍留在共享目录或自己的分支上。

## 5a. 等待长时间命令：callback

部署、CI 执行、发行传播或长时间建置：凡是答案要「稍后」才知道的工作，
都不要在目前轮次等待，也不要在之后的轮次轮询。把它交给守护进程：

```sh
clawdline callback --title "CI is green on <sha>" --timeout 45m -- gh run watch <run id> --exit-status
```

它会打印 `callback <id> briefed` 并返回。**结束你的轮次。** 命令结束时，
守护进程会输入 `<clawdline-notice>`，其内文为
`callback <first 8 of id> finished: success (exit 0 after 6m) — run clawdline task show <id>`；
`task show` 会打印结束状态、结束码及输出末尾几行，并像 child 任务一样关闭通知。

- 这是属于你的无分页任务：`clawdline task cancel <id> --reason "…"` 会停止命令的整个进程群组；
  超过 `--timeout`（1 分钟至 4 小时，预设 30 分钟）时会停止并结算为 `timeout`。
  执行中的 callback 与执行中的 child 一样，会阻止 Session 关闭。
- 命令会按参数逐字执行，不透过 shell；若需要 shell，请写 `sh -c '…'`。
  它在目前目录执行（用 `--dir` 指定其他目录），只继承环境中的 PATH、HOME、locale、
  USER、SHELL、TMPDIR、TERM，绝不继承凭证。需要凭证的命令应从自己的文件读取。
- 输出位于任务目录的 `output.log`，结束后保留七天。
- 命令只执行一次。若守护进程途中重启，会重新接手该命令；
  若命令在没有守护进程监看时结束，且没有留下结束状态，会以结果未知的 `failure` 结算，
  **不会**再次执行。若安全，请自行重新启动。
- 若启动结果不明（CLI 无法连上守护进程），可使用 `--task-id <the id it printed>` 重试：
  相同 ID 绝不会启动两次。
- callback 不占用 child 时段。每个 Session 最多同时执行 8 个，每部机器最多 16 个；
  超额会收到 `429 callback_capacity` 与 `retry_after`。
  派发任务不得将自身声明为 `kind callback`（`bad_task`）。Windows 会回复
  `501 no_callback_capability`。

## 6. 落地与另外三种工作

**合并 child 分支后，无需再做任何事**：broker 会自行记录 `landed`（见下文）。
若 child 声明不写入（`--claims ""`）、自行完成，且分支与工作目录都没有留下变更，
broker 会在输入通知前将其记录为 `nothing_to_land`。
需要手动记录落地的情况是上述两者未涵盖的内容，例如 cherry-pick、`incorporated` 交付、
`nothing_to_land` 或 `abandoned`。使用编排令牌执行一个命令即可：

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

其对应路由如下；脚本可以用 `X-Clawdline-Orchestrator` 标头调用：

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- 仅接受这些键，以及会被接受但不使用的 `delivery`；其他键都会遭拒。
  `pending` 和 `abandoned` 可使用任务密钥或编排令牌；`landed`、`incorporated` 与
  `nothing_to_land` 只能使用编排令牌。
- `landed` 需要 `target` 和 `commit`；`incorporated` 需要 `target`、`commit`，
  以及其已验证落地承载此次交付的另一个任务 `carrier_task`。
  守护进程会在 Git 中**验证两种情况**；否则会回复 `409 unverified_landing`，
  `reason` 为以下其中一项：
  `commit_unresolved`, `target_unresolved`, `not_on_target`, `base_unknown`, `predates_dispatch`,
  `delivery_unknown`、`nothing_delivered`、`not_the_delivery`，以及 `incorporated` 对应的
  `carrier_required`, `carrier_is_delivery`, `carrier_unresolved`, `carrier_not_landed`,
  `carrier_repository_mismatch`, `carrier_target_mismatch`, `carrier_commit_mismatch`,
  `delivery_is_ancestor`.
- 若任务确实有写入，`nothing_to_land` 会遭 `409 wrote_to_repository` 拒绝。
- 已结算的落地不可更改：会收到 `409 invalid_transition`；值不同时为 `409 landing_conflict`。
- **合并会自行留下记录。** 已完成任务的分支合并进目标后，broker 会在几分钟内经过相同 Git 检查，
  自行以目标分支的 HEAD 提交记录 `landed`。若记录中没有目标，只有当主要工作目录的分支
  是唯一承载交付的分支时，才会指定它。cherry-pick、`incorporated` 交付和
  `nothing_to_land` 仍需由你记录。
- **完成通知会指出任务结束时的分支状态**，并以命令形式要求一项操作。
  *分支上没有提交*：落地须由该分支证明，现状下无法记录已落地；
  工作目录尚在时，请在其中的该分支提交，或执行 `clawdline task land <id> abandoned`。
  *分支上已有提交*：合并分支至目标；合并会记录落地。
  *无法读取*：检查分支，再记录。
  *写入共享工作目录*：用承载该工作至目标的提交执行 `clawdline task land <id> landed`，
  或记录为 `abandoned`。
  *没有写入，broker 已记录 nothing_to_land*：只剩确认通知。

`clawdline landings`（`GET /v1/orchestrator/landings`）列出机器上所有待落地项目，
每项都有 `ownership.status`。`unknown` 不代表「无人负责」，而是无法读取证据。
`503 landings_incomplete` 表示部分列无法读取；系统不会用较短清单取代完整结果。

`clawdline landings --work-id <item id>`（`GET /v1/orchestrator/landings?work_id=<item id>`）
则列出某看板项目**已记录**的所有落地：`{"work_id", "landings": [...], "at"}`。
每列含 `id` 和 `source`：`task`（已绑定 child 的 landed 或 incorporated 记录，ID 即任务 ID）、
`root`（`item phase deploying --commit` 写入的记录），或 `phase_event`
（旧版守护进程保存在项目历史中的副本）。
若已绑定任务的记录无法读取，仍会保留一列 `state: "unknown"`，绝不省略。
项目读取结果也包含与 `landings` 相同的列。不存在的项目会收到 `404 work_not_found`。

**两个 root 要落地到同一工作目录时，**请先取得落地租约（§11）。

另外三种工作各有自己的路由。选哪一种是工作边界，不只是细节：

| 种类 | 路由 | 用途 |
|---|---|---|
| **Handoff** | `POST /v1/orchestrator/handoffs` | 将现有工作及其完整状态交给新的 Session |
| **Root assignment** | `POST /v1/orchestrator/root-assignments` | 为新功能创建独立负责的 Root |
| **Detached automation** | `POST /v1/orchestrator/detached-tasks` | 无需向任何人回报的无人值守工作 |

**交接。** 先写入 `<state dir>/handoffs/<handoff_id>/handoff.md`
（清单路由会回复 `package_root`）。文件应有三个标题：**REFERENCES**（接手者必读的所有资料）、
**VERIFICATION**（接手者继续前需根据资料回答的问题），以及 **OPEN THREADS**（应从哪里接续）。
然后以封闭格式的本文送出：

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

接手者会收到指示，读取文件、逐一查看参照资料、回答验证问题，再继续工作。
交接开启时，系统会捕捉送出者在该 Project 中尚未关闭的看板项目。
接手者取得对话 ID，且其第一笔对话记录被观察到后，这些项目的有效指派与负责人
会在单一交易中转给接手者。交接失败时，负责权仍归送出者。
已关闭、已转给他人或正由另一途径指派的项目保持原状。
处于 verifying 或 merging 的验证关卡项目会退回 implementing，让新负责人重新验证。
交接行输入后，你会收到一则 `handoff_receipt` 通知；该通知本身不证明看板转移已完成。
拒绝码包括 `bad_task`（含缺少或空白的 `handoff.md`）、`sender_not_found`、
`sender_ambiguous`、`rate_limited`、`terminal_busy`；
若你担任机器协调者，还有 `succession_required`。此守护进程不支持继任（`501`），
因此该 Session 无法交接。

**里程碑交接。** 长时间执行的 Root 到达里程碑时，可用
`clawdline handoff --summary summary.md` 交接，避免后续工作每次调用都重读先前全部内容。
摘要必须恰好有五个 `## ` 标题：Goal、Verified decisions、Blockers、Evidence
（应提供可开启的路径、提交、ID 或 `clawdline` 命令，而非其内容）、Next step；
最多 6 KiB，不含凭证或对话文本。
`--check` 可在不开启任何内容的情况下列出所有问题，守护进程会以
`bad_milestone_summary` 拒绝相同问题。
守护进程会在旁边写入 `obligations.md`：你的看板项目会转移（包括尚待用户作答的决定）；
执行中的 child、未确认通知与待落地项目仍归你负责。
因此交接后请继续确认通知并落地，待 `clawdline session close` 显示 `safe` 时再关闭。
是否交接由你决定：`clawdline usage --compare-handoff` 会显示它在此机器上的收益，
但系统绝不强制交接。

**Root 指派。** `Idempotency-Key` 标头必须等于 `request_id`：

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

每个 assignment 栏位为 1–8192 字节，总计最多 32 KiB。
守护进程自行写入简报并开启 Session。它**没有 parent、密钥、逾时、结果或落地**：
完成时不通知任何人，因为它不向任何人回报。
拒绝码：`bad_root_assignment`、`idempotency_mismatch`、`request_conflict`、`rate_limited`。
绝不要用 child、脱离式任务或交接来假造 Root 指派。

**脱离式自动化。** 与派发（§4）类似：先在 `task_root` 下写入 `task.json`，
再送出 `{"task_id", "secret", "inventory_generation"}`；
但简报的 root 必须为 `{"session_id": null, "poll_only": true}`，
否则会收到 `detached_task_required`。没有人会收到通知；请轮询
`GET /v1/orchestrator/tasks/<id>` 并读取 `result.json`。
它绝不是 Root，也不是 Feature 负责人。

### 安排后续工作

Clawdline Next 自行管理调度工作。不要使用已退役应用程序、`cron` 或一连串脱离式任务。
用 `GET /v1/orchestrator/schedules` 读取清单；用
`GET /v1/orchestrator/schedules/<id>` 读取单一调度的完整内容。
`GET /v1/places` 提供写入时要指定的 `place_id`。

一次性调度（`on`）可直接使用编排令牌创建。重复调度（`days`）是持续有效的指示，
需有用户在此 Session 中的明确指示：

1. 读取 `GET /v1/orchestrator/sessions/<conversation>/run`。
   它是用户透过 Clawdline 发送消息到此 Session 时产生的近期执行记录。
2. 调用 `POST /v1/orchestrator/schedules`，提供 `Idempotency-Key`、一般调度本文，
   再加上该对话与执行记录：

```json
{"title":"Morning sweep","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"Inspect the overnight failures and report actionable findings.",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

当用户明确要求变更时，同一证明也可授权
`PATCH /v1/orchestrator/schedules/<id>`（送出完整调度本文），以及
`DELETE /v1/orchestrator/schedules/<id>`（以 JSON 本文送出两个证明栏位）。
`POST /v1/orchestrator/schedules/<id>/run` 会立即执行一次。
回报成功前，请重新读取已创建或修改的调度。

缺少 `via` 时，编排令牌仍只能创建一次性的 `on` 调度。
虚构、过期或属于其他 Session 的执行记录，会分别收到 `run_unknown`、`run_expired` 或
`run_other_session`；格式错误的证明则会收到 `invalid_user_authorization`。
若用户直接在终端机输入消息，就没有执行记录：请他们透过 Clawdline 送出指示。
不要把一次执行记录当作用户未要求工作的一般授权。
这项证明使转送可供稽核，不会把机器共用的编排令牌变成 Session 专属凭证。

调度也可以不设定时间：送出 `"trigger_only": true`，不送 `at`、`days` 和 `on`。
时钟绝不会触发它；只有 `…/run` 或 webhook 能触发，且调度须为 `enabled`。
它和重复调度一样是持续有效的指示，需要相同证明。

**在另一部机器启动任务并取得结果。** 机器之间没有直接信道。
请在目标机器创建仅由触发器启动的调度，绑定 Cloud webhook，并将 URL 存储于调用端可读的位置。
此 URL 是凭证：应放在只有你能读取的文件，或 `CLAWDLINE_WEBHOOK_URL`，绝不要放进命令行参数。
然后在调用端机器执行：

```sh
clawdline webhook fire --url-file <path>
```

它会送出 `{"deliver_within_seconds": 60}`（`--deliver-within`），因此目标机器若已关机，
不会稍后再执行；它追踪交付状态，直到结束或超过 `--timeout`（60 分钟），
并在 stderr 打印变化、在 stdout 打印最后一行。
结束码 `0` 表示成功；`1` 表示已结束但未成功（failure、timed_out、cancelled、spawn_failed）；
`2` 表示从未到达机器（expired、canceled、unreachable）；
`3` 表示遭拒（dispatch_refused 及其拒绝码、URL 不可用、速率限制）；
`4` 表示停止等待，并保留最后观察到的状态。
`--no-wait` 在收到 `202` 后打印交付 ID 并返回。
请原样回报结束码和最后一行：`2` 代表没有执行，不代表执行失败。

若调度任务读取某个待验收项目所需的资料（侧边栏的「验收」，
`docs/verifications.md`），应以自己的任务密钥把读取结果写为该记录上的笔记，
而不是使用本不该持有的编排令牌：

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<readout>"}'
```

只有当记录的 `schedule_id` 与启动此任务的调度相符时才会写入，否则收到
`schedule_mismatch`。笔记署名为 `task:<task id>`；非调度启动的任务会收到
`not_scheduled`。使用 `clawdline verify list` 查询记录 ID。

## 7. 报告自己已完成的轮次

当你的轮次确实完成（工作已完成、已验证，适用时也已提交），请在最终答复前
把以下命令作为最后一项操作：

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

它会在你的 Session 列上显示勾号：**已交付，等待核可**。
其效力弱于落地，不要求审查。只有当守护进程判定 Session 闲置，且终端机仍保持同一对话时才会显示；
工作中、等待中或画面无法读取的状态优先于此标记。

- **只适用于已完成的轮次。** 部分工作、诊断、阻碍或向用户提问时都不要使用。
  child 绝不送出（`409 child_session`）。
- 命令从 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID` 找出你的对话
  （否则由 `--conversation` 指定），向 `GET /v1/orchestrator/whoami` 查询终端机，
  并将 `{"summary"}` 送到 `POST /v1/orchestrator/sessions/<terminal>/complete`。
- 摘要须为 1–500 个字符。每次调用都是新收据，以最新收据为准。
- 回复还包含 `open_todos`：此 Session 已送出或读取、但尚未完成的直接待办事项，
  依时间由旧到新，最多 20 项；更多时会标示 `open_todos_truncated`。
  命令会在收据后把它们打印到 stderr，每行一个 ID 与文本。
  已完成的项目请逐一以 `clawdline todo done <id>` 勾除。
  无论如何都会记录收据，结束状态也不变；`open_todos_unknown: true` 表示无法读取，
  并非没有未完成项目。
- 拒绝码：`conversation_id_malformed`（不是小写 UUID）、`conversation_not_found`、
  `conversation_ambiguous`、`registry_stale`、`session_not_found`、`session_unbound`、
  `child_session`。请如实回报拒绝结果；聊天中的一句话不算收据。

**留一份状态报告给用户。** 若此轮次更改了 git Project 的文件，
请提供一个页面，让用户查看工作状态，并阅读此轮次添加或修改的每个文件。
它是本机 HTML 文件：不会上传，也不会载入外部内容。

```sh
clawdline report --repo <project> --status status.md [--notes notes.txt] [--lang zh-TW] [--open] <commit>…
```

- 按时间由旧到新列出**此轮次自己的提交**。每个提交会分别读取，
  因此夹在中间的其他 Session 提交不会混入；绝不要传入范围。
- `status.md`：可选的 `# Title`，其下一行也可选；之后每张卡片用一个 `## ` 标题，
  以 ✅、🟡 或 ❌ 开头，并附简短的 Markdown 内文。
- `--notes`：每行一个 `path: sentence`，显示在对应文件上方。
  可重复使用 `--pin` 将文件置顶；若此轮次修改 `CLAUDE.md` 或 `AGENTS.md`，它们会自动置顶。
  `--exclude` 会排除路径并注明。`--at` 指定显示内容的版本（预设 `HEAD`）。
- 报告存于每个仓库之外的 `<state dir>/reports/<date>-<id>/report.html`，
  并打印两个地址：**先是终端机可开启的 `file://` 地址，接着是此机器守护进程提供的
  `http://127.0.0.1:<port>/reports/<id>`**。最终答复中请列出两者：控制台无法开启
  `file://` 地址，只会显示文本；`http://` 地址则会变成链接。
  该地址仅供此机器上已登录其控制台的浏览器开启；手机或 Cloud 检视者会收到
  `report_not_over_cloud`、`report_local_only` 拒绝。
- `--out` 可改写到另一个文件或目录，此时只提供 `file://` 地址。
  stderr 会说明省略或缩短的内容。`--open` 也会在此机器的浏览器开启文件。

## 8. 与另一个 Session 对话

**查找。** `GET /v1/orchestrator/sessions` 是通讯录：列出每个 Session 的
`id`（终端机 ID）、`label`、`assistant`、`cwd`、`state`、`work_state`，
以及执行中 child 的 `taskId`。

**传送。**

```sh
clawdline send --to <terminal id> "text"        # or text on stdin
```

这会以 `{from_session, to_session, text}` 和 `Idempotency-Key` 调用
`POST /v1/orchestrator/messages`。守护进程会把消息放在指出你为来源的
`<clawdline-message>` 信封内，输入到接收方的输入框。

- `to_session` 是**终端机 ID**：消息会送到指定分页，而非跟随对话。
  `from_session` 是你的终端机或对话 ID（命令会代你填入）。
- 只支持文本，最多 100,000 个字符。没有 `images` 栏位；若送出会被默默丢弃。
- `ok` 表示字节已到达输入框，不代表有人读取。
- 转送可能耗时数十秒：守护进程在输入前会读取机器上的每个 Session
  （2026-09-19 实测某台 Mac 每次约 30 秒）。命令最多等待两分钟。
  不要提早中断：若转送途中被切断，内容可能已输入但未记录，
  使用相同键时便会收到 `409 request_in_progress`。
- 命令会先打印 `Idempotency-Key`。若调用途中失败，请用 `--key <that key>` 重试：
  相同键与本文只会输入一次。相同键配不同本文会收到 `409 idempotency_key_reused`。
- 拒绝码：`source_not_found`、`target_not_found`、`same_session`、
  `target_busy`（接收方显示选单，没有输入内容）、`terminal_busy`、`delivery_failed`。

**向用户展示图片。** 不要贴本机路径：手机上无法开启。

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- 只能使用编排令牌。可提供一至六个本机文件；每个都须为一般文件、最多 12 MiB，
  每边最多 12,000 像素。PNG、JPEG 和 GIF 会直接读取；其他格式在 macOS 上透过 `sips` 处理。
- 回复列出 `artifacts`，每项都有 `<clawdline-image id="…">` 之类的 `marker`。
  **请把标记放入答复**；控制台会在标记位置显示图片。图片保留 24 小时。

## 9. 通知用户

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

这会调用 `POST /v1/orchestrator/notify`。只在用户正等待某件事时使用：
推送之所以有价值，是因为它很少发送。指定 `--session <terminal>` 后，点按通知会开启该 Session。

- `409 agent_notify_disabled`：用户已关闭 agent 通知。这不是你的错；不要重试。
- `409 not_subscribed`：没有装置订阅推送。
- `429 rate_limited`：整部机器每小时最多 30 则，与所有 child 的通知共用。
- `502 push_failed`：推送服务拒绝；错误内含 `sent` 和 `failed`。

## 9a. 留下需要人工介入的 Note

当长时间执行的 Agent 有一件具体事项需用户阅读、执行或决定，而一般聊天消息可能淹没在消息流中时，请使用 Note。
Note 会留在目标 Session 收合的待处理区，直到用户将它移为已处理前都会显示红点。
你可以继续独立工作；用户可在自然的停顿点回来处理。
Note 不是进度日志、私人提醒、通知，也不是看板决定的授权。
同一请求应避免重复创建 Note。

**在聊天中要求用户作选择前，**先创建一则 `answer` Note，包含实际问题、决定所需的取舍，
以及两至四则完整的建议答复。点按按钮会把答复当作对话消息送出，因此每个 `draft`
都应能独立、明确地表达选择。创建后，在聊天中简短提示即可。
不要把创建 Note 或将 Note 标记为已处理当作用户的答案；
请等待对话消息（点按送出的答复或用户自行输入的消息）到达后才行动。
若创建失败，请说明并直接提问。Note 应留给需要人工判断的决定，
而不是 Agent 可自行处理的例行选择。

用 JSON 本文文件创建 Note。不指定 `--target` 时，CLI 会透过 `whoami`
解析这个执行中 Root 自己的终端机 ID。若要送至其他 Session，
请用通讯录（`clawdline guide zh-Hans send`）中的执行中**终端机 ID**作为 `--target`。
`--from` 预设为环境中的此 Root 对话 ID。CLI 在不把机器凭证放入命令行的情况下读取它，
注入来源与目标 ID，并打印守护进程的持久 Note ID。
若结果不明，请重用已打印的 `--key`。

```json
{"kind":"answer","title":"Choose a date","summary":"One release date needs your choice.","action":"Choose a date when you have a moment.","reason":"Only you can choose it.","options":[{"label":"Tuesday","draft":"Tuesday works for me."},{"label":"Wednesday","draft":"Wednesday works for me."}]}
```

```sh
clawdline note create --body-file note.json
# For another Session: clawdline note create --target <terminal-id> --body-file note.json
```

`kind` 可为 `read`、`answer`、`action` 或 `report`；`title`、`summary`、`action`、`reason` 必填。
`answer` 可提供两至四个选项。每个 `draft` 是按钮上显示的建议答复。
用户点按后，控制台会立即把答复作为对话消息送到 Note 所属的 Session，
随后附上一行含 Note ID、标题和动作的上下文，让接收的 Session 知道用户回答了哪个请求；
按钮上不会显示该上下文。只有送出成功，Note 才会移至最近已处理。
若送出失败，Note 仍待处理，待处理控制项会显示答复未送出。
`detail` 可放较长文本。`document_url` 可链接真实且可读的 Cloud 文档；
发布前请验证文档路由和文件。请依到达的对话消息行动，而不是依 Note 状态：
用户手动标记已处理的 Note 不会送出任何消息。
若工作确实被答案阻塞，请记录等待用户的状态，并发送一次现有的待处理通知。
单靠可见 Note 不会推送，也不会唤醒 Agent。

## 10. 看板

看板有三种结构：看板项目、Backlog，以及各 Session 自己的待办清单；
**由用户决定哪些内容进入看板**。只有用户透过 Clawdline 传来的消息明确要求时，
Session 才能创建看板项目；否则应提出提案。Session 绝不主动创建卡片。
唯一例外是 Epic 负责人：Epic 计划经审查后，可将其拆分为 Feature 和 Issue 项目，
再指派给 Session（`clawdline guide zh-Hans epic`）。

**TODO / 待办 / 土度 若与看板项目一起提及，指的是该项目的步骤。**
请用 `--step` 加在项目上，**不要**再用 `clawdline todo add` 创建一份。
`clawdline todo add` 仅适用于用户要求你在此 Session 自己的待办清单中追踪、
且没有看板项目的工作。

**用户要求创建看板项目时。** 只有其消息透过 Clawdline 送达、因此带有 run，
并明确要求创建项目时，才自行创建：

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "first step" --step "second step" …   [--description-file f | description on stdin] [--assign-self]
```

范例：用户写道 *「创建一个整理发布说明的看板项目，TODO：起草说明、检查链接、发布。」*
只需执行以下一个命令：

```
echo "Clean up the release notes before the next release." | \
  clawdline item add --project <place id> --kind feature --title "Clean up the release notes" \
  --step "Draft the notes" --step "Check the links" --step "Publish"
```

 - `item add` 会读取此对话的最新 run（`GET /v1/orchestrator/sessions/<conversation>/run`），
  除非使用 `--run` 指定。它在送出前打印 Idempotency-Key（`--key` 可重试相同写入），
  并打印新项目及每个步骤的 ID。对应请求为带有
  `{"session_id", "via": {"run"}, "project_id", "kind", "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`
  的 `POST /v1/work/v2/agent/items`；成功时回复 `201` 与
  `{"item", "assigned", "assignment_state"}`。
- Feature、Issue 或 Epic 预设为**未指派**，连同步骤一起列在看板上同类型的未指派区。
  步骤依 `--step` 的顺序排列；未提供参数时，则取描述中两行以上的顶层 Markdown 清单。
  用户常会要求你先整理供以后处理的工作；创建项目并不代表由你负责。
  此时 `assignment_state` 为 `not_requested`。
- **只有用户消息要求此 Session 立即执行工作时**（例如「为这件事建项目并完成」），
  才加上 `--assign-self`（`"assign": {"mode": "self"}`）。
  项目会在同一次写入中进入 `assigned` 并**指派给你**。终端机不会再输入通知，因为是你要求的。
  依序处理步骤，逐一验证后标记完成（`clawdline item steps <item id>`、
  `clawdline item step-done <item id> <step id>`；若工作中发现需要新步骤，
  用 `clawdline item step-add` 加入），并与其他已指派项目一样用
  `clawdline item phase` 推进阶段（见下文）。
  以此方式接手的 Epic 须先遵循下文的 Epic 流程，之后才能实现。
  若用户稍后要求你接手原先创建为未指派的项目，请用 `clawdline item claim`（见下文）。
  Refactor 是改变内部结构但不改变外部行为的可执行工作：会被指派、包含步骤，
  并遵循 Feature 的阶段、关卡与审查开关。
  Plan 无论是否指定 `--assign-self`，都会以未指派状态创建于 Planning，
  且不接受步骤（`planning_has_no_steps`）。
- 已注册的 Clawdfather 是可执行 Project 工作的例外：它绝不负责或编辑 Project 代码。
  当用户消息明确要求添加项目时，它可使用
  `clawdline item add --project <place id> --kind feature --title "…" --assign-new`
  （或 `--assign-terminal <id>`）先创建项目，再委派给 Project Session。
  `assignment_state` 会显示已记录 Project 负责人（`assigned`）、新 Session 需先完成首次对话
  （`awaiting_user`）、指派失败（`failed` 与 `assignment_error`），或未要求指派
  （`not_requested`）。在后两种情况下，用户可从看板指派。
  若委派中断后重播且回复 `pending`，会指出原始项目；再次指派前先检查看板。
  没有明确消息时，请提出项目提案并等待接受。
- 用户会在卡片上看到「Created by the Session from your message at HH:MM」标记，
  并附其原话引述。
- 下列拒绝都不会写入：`run_unknown`（未指定 run 或未产生 run）、
  `run_expired`（超过一天）、`run_other_session`（消息送到另一个 Session）、
  `session_not_found`、`child_session`（child 透过 `result.json` 回报）、
  `project_not_found`、`project_mismatch`（一般可执行项目须位于你工作的 Project）、
  `coordinator_required`（机器 Session 没有有效角色）、`machine_delegation_required`
  （一般 Session 要求只有机器层级才能做的合并指派）、`invalid_assignment`
  （`assign` 格式错误，或 Clawdfather 要求 `self`）、`too_many_steps`（超过 128 个）、
  `run_items_exhausted`（一则消息最多支持五个项目）。
- **没有 run**：用户直接在终端机输入，`item add` 会回复 `no_run` 或 `run_unknown`。
  请改提案（见下文），并告知用户在看板的 Agent proposals 中接受。

绝不要主动创建看板项目，也不要创建多个项目来规划尚未确定的工作。

**认领用户指定的看板项目。** 若用户透过 Clawdline 发送消息要求你接手看板上的特定项目，
例如 *「接手发布说明项目」* 或 *「认领 <item id>」*，
请用一个命令认领：

```
clawdline item claim <item id>
```

- 除非用 `--run` 指定，`item claim` 会读取此对话的最新 run，并读取项目版本；
  它会在送出前打印 Idempotency-Key（`--key` 可重试相同写入），完成后再打印项目。
  对应请求为带 `{"expected_version", "session_id", "via": {"run"}}` 的
  `POST /v1/work/v2/agent/items/<id>/claim`；它只会把项目指派给**你，也就是消息的接收 Session**，
  不会指定其他 Session 或终端机。
- 此后项目与用户从看板直接指派给你完全相同：你成为负责人，项目进入 `assigned`；
  若原本没有步骤，会从描述中的清单产生。终端机不会输入通知。
  请依下文处理一般已指派项目。
- 用户会在卡片上看到「Claimed by the Session from your message at HH:MM」标记，
  并附其原话引述。
- 下列拒绝都不会写入：`run_unknown`、`run_expired`、`run_other_session`、
  `session_not_found`、`child_session`（与 `item add` 相同）；`work_not_found`；
  `project_mismatch`（项目位于你不工作的 Project）；`item_assigned`
  （项目已有 Session，或正在为其开启 Session；只有用户可将项目在 Session 间移动）；
  `item_terminal`（已完成或取消）；`planning_not_assignable`
  （Plan 保持在 Planning；Epic 或 Refactor 可以认领）；
  `version_conflict`（项目已变更，请重新执行命令）；`run_claims_exhausted`
  （一则消息最多支持五次认领）。
- **没有 run** 会收到 `no_run` 或 `run_unknown`：保留项目供用户指派。

**将用户指定的看板项目指派给新 Session。**
若用户透过 Clawdline 发送消息，要求你把一个特定、未指派的 Feature 或 Issue
交给新 Session，例如 *「为 <item id> 开启一个安全事务 Session」*，请用一个命令指派：

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- 若项目不是 Epic 的子项目，`item assign` 会像 `item claim` 一样读取此对话的最新 run，
  除非用 `--run` 指定。对应请求为带
  `{"expected_version", "session_id", "mode": "new_session", "assistant"?, "model"?, "persona"?, "via": {"run"}}`
  的 `POST /v1/work/v2/agent/items/<id>/assign`；它会开启与用户自行选择「New Session」相同的新 Session。
- 卡片会显示「Assigned by a Session from your message at HH:MM」，并引述其原话，
  同时列出新 Session 使用的 persona。
- 下列拒绝都不会写入：`item claim` 的拒绝码
  （`run_unknown`、`run_expired`、`run_other_session`、`session_not_found`、`child_session`、
  `project_mismatch`、`item_assigned`、`item_terminal`、`version_conflict`，
  以及 `run_claims_exhausted`；`item claim` 和 `item assign` 共用一则消息的五次额度）；
  `kind_person_assigns`（仅限 Feature 或 Issue）；`new_session_only`（要自己接手，请认领）；
  `unknown_persona`；`persona_disabled_for_auto_assignment`
  （该 Project 已关闭此角色的自动指派）。

绝不要主动认领或指派项目；只处理用户消息指定的项目。
也不要使用用户专用的 `POST /v1/work/v2/items/<id>/assign`，Session 调用它会收到
`session_cannot_create_item`。Epic 负责人也可用 `clawdline item assign`
指派 Epic 自己的子项目（`clawdline guide zh-Hans epic`）。

**为看板项目开启的新 Session 命名。** 读取目标和范围后，选择描述实际任务的短名称，
并执行 `clawdline item name <item id> "<task name>"`。
这只会修改一次 Session 名称，不会更改看板项目标题或启动新模型轮次。
只有该新 Session 目前的负责人可以执行。再次送出相同名称是安全的；
不同名称会遭拒，但用户仍可手动设定 Session 标题。
指派给现有 Session 的看板项目不会更改该 Session 名称。

**提出看板项目。** 看板的 **Agent proposals** 伫列由以下路由接收：

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` 是 `GET /v1/places` 某列的 `id`。
- 必须指定一个来源，且须属于你：此 Session 负责的看板项目，或此 Session 自己的待办事项
  （`proposal_source_required`、`proposal_source_invalid`）。
  若提案源自用户提出的事情，请引用相应待办事项：流程是用户提出要求、
  你用 `clawdline todo add`（见下文）记录，再依该待办 ID 提案。
- 提案应以用户能直接理解的白话撰写：`title` 指出可察觉的成果，`description` 说明会变更什幺，
  `reason` 说明为何现在值得做，`suggested_acceptance` 指出完成后可观察到什幺。
  四者都必填。不要以未解释的缩写、内部识别符、代码路径或实现术语作为主要说明。
  看板起初显示标题、来源与原因；展开 **Explain / 详细说明** 才显示变更内容与完成后的样貌。
- **提案中要包含步骤时，**请在 `description` 中写两行以上的顶层 Markdown 清单。
  用户接受并指派项目后，每行都会成为一个 `steps`（见下文）。
- `201` 表示已创建待处理提案。用户在看板的 Agent proposals 伫列中接受、编辑或拒绝前，
  它不会成为看板项目。拒绝码：`invalid_proposal`、`proposal_too_large`、
  `project_not_found`、`proposals_full`。

**旧版提案路由。** `POST /v1/orchestrator/proposals`
（在对话中提问后使用 `…/<id>/asked`）仍受支持：child 可用任务密钥和 `task_id`
提交遗留事项；root 的工作线提案也可在这里取得立即询问或暂缓的 `instructions`。
它的列出现在旧版「to confirm」区域，**不在** v2 看板的 Agent proposals 伫列；
因此不能用它把项目放到看板上供用户决定。

**请用户作决定。**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "work_id": "<the Board item this is about>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

项目须保持开启，且由此 Session 负责；看板会以其卡片作为问题的上下文
（`decision_source_required`、`decision_source_not_found`、`decision_source_invalid`、
`decision_source_closed`）。选项须有两至四个；`default` 必须是其中之一，
并在无人作答时生效（预设七天后；也可用 `due_in_minutes` 指定 60–10080 分钟）。
只有 `blocking` 决定会推送。用 `GET /v1/orchestrator/decisions/<id>` 读取答案。

**由用户作答；Session 只转达其原话。** 提案、决定和看板项目都在 `/v1/work/…` 下作答。
Session 在那里写入时，须指定承载用户消息的 run：`"via": {"run": "<id>"}`；
缺少时会收到 `403 session_cannot_decide`。可从
`GET /v1/orchestrator/sessions/<conversation>/run` 读取最新 run；
虚构、过期、属于其他 Session 或早于提问的 run，会以对应拒绝码拒绝。
用户若直接在终端机输入，就没有 run；请他们透过 Clawdline 或控制台作答。

**你尚待收取的 child。** `GET /v1/orchestrator/sessions/<conversation id>/todos`
依对话 ID 查询，而不是终端机 ID（否则收到 `409 session_id_is_terminal`）。
broker 会依任务事实创建和关闭这些项目；你不需写入。

每个轮次边界、声明自己闲置前，还应读取
`GET /v1/work/v2/agent/session-todos/<conversation id>`。
其中 `assigned_items` 是用户指派给此 Session 的看板项目；`recent_items` 是此 Session 最近完成的项目；
`direct_todos` 是快速请求；`unacknowledged_completions` 是已完成但尚未收到你 ACK 的 child（§5）。
这次读取可让你工作期间收到的指派先等候，不打断目前轮次。
完成目前轮次后，将已指派项目当作下一项自己负责的工作，
并以 `clawdline item show <id>` 读取完整记录，包括文档内文。

**用户送出的待办事项。** 若消息最后一行是
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)`，
它就是用户从 Clawdline 送给此 Session 的 `direct_todos` 之一；
该行上方的文本是请求内容。完成工作并验证后，请在回报轮次**之前**用该 ID 执行
`clawdline todo done <id>`；否则虽然工作已完成，用户的清单仍显示未完成。
未完成的待办事项应保持开启。`clawdline session report` 会在 stderr 列出
所有送到此 Session、但尚未勾除的待办事项（§7）。

**用户要求时，管理自己的待办事项。** 只有用户明确要求此 Session 将工作记录为
Clawdline 待办事项，或交付多个项目的清单并要求在那里追踪时，
才写入此 Session 自己的清单：

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` 对应带 `{"todos": [{"text": "…"}, …]}` 的
`POST /v1/work/v2/agent/session-todos/<conversation id>`，并附上命令先打印的
Idempotency-Key（`--key` 可重试相同写入）。每次调用可在最多 96 KiB 的请求本文中，
加入 1–20 列、每列最多 8 KiB；要幺全部加入，要幺全部不加入。
若会让此 Session 的未完成待办事项超过 500 项，整份清单会遭拒（`direct_todos_full`）。
成功时回复 `201`，并依输入顺串行出各列。
对话必须是此守护进程认识的执行中 Session（`conversation_id_malformed`、
`session_not_found`）；Clawdline child 会遭拒（`child_session`），
仍应透过 `result.json` 回报。

绝不要主动添加待办事项，也不要用它规划尚未确定的工作。
每列都应在验证完成后，才以 `clawdline todo done <id>` 标记。
用户会看到这些列标示为 Session 添加；只有用户可送出或删除它们。
它们不是看板项目，绝不出现在看板上。
待办事项是目前 Session 的工作清单；看板项目是用户希望在看板追踪的工作。
用户要求后者时，请使用 `clawdline item add`（见上文），把清单写成项目的
`--step`，不要再另外添加待办事项。

若用户的意思明确表示你刚完成的项目其实尚未完成，请自行修正看板；
不要让它留在 Recently Done、创建替代项目，或要求用户重开。
先重读项目的目前版本，再使用：

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "The concrete behavior or acceptance claim that remains unfinished"}
```

只有在用户明确指向你刚完成的项目时才使用此路由。
它只接受由同一 Session 完成、且最终指派已解除的 `done` 工作；
不能撤销用户的取消操作，也不能接手另一个 Session 的完成项目。
它会保留先前证据，从 `implementing` 开始新周期，恢复此 Session 为负责人，
并把原因记录在不可变更的项目历史中。原因最多 8 KiB。
含义不明的后续消息不足以授权变更看板项目。

若负责的 Agent 需要用户执行操作或作选择，应创建决定并等待。
先为此项目开启决定（调用 `POST /v1/orchestrator/decisions`，带上项目的
`work_id`、两至四个选项、`default` 与期限；若要求用户执行操作，选项可为
`{"id": "done", "label": "I've done it"}` 和 `{"id": "cannot", "label": "I can't"}`），
再透过机器验证的路由，让项目指向该决定：

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

决定必须存在（否则 `decision_not_found`）、属于此 Session（`decision_other_session`）、
与此项目相关（`decision_other_item`），且仍开启（`decision_not_open`）。
若在其他 `condition` 下指定 `decision_id`，会收到 `decision_requires_waiting_user`；
没有决定的 `waiting_user` 会收到 `waiting_user_requires_decision`。
用户可在看板卡片或「Waiting on you」作答；作答后，或期限到达而预设选项生效时，
守护进程会在同次写入中清除项目的 `waiting_user`，记录答案，
并在此 Session 闲置时输入所选选项的 ID 和标签。
若你自行停止等待，请透过同一路由将 `condition` 设为空字符串（或其他条件）；
决定会变成 `withdrawn` 并离开「Waiting on you」。
项目解除指派、重新指派、取消或完成时也一样。
对已撤回决定作答会收到 `decision_withdrawn`。

**已记录的规划与验证关卡。** `planning_gate` 预设开启，`verify_gate` 预设关闭；
`clawdline setting get|set planning_gate|verify_gate` 接受 `on/off` 或 `true/false`。
每个执行周期首次成功指派时，两项设定值便会冻结。
重新指派或之后修改全域设定，不会影响该周期。
已记录为规划开启的 Epic 或 Feature，在进入 implementing 前需有验收标准。
Epic 还需计划与独立审查；Feature 只有在用户勾选「Needs independent review」时才需要（见下文）。
Issue 不受规划关卡限制。规划关闭时，也会略过 Epic 的强制规划。
两者都开启，表示先规划、再独立验证；只有规划开启，仍需一般合并验证；
只有验证开启，会略过规划，但仍检查确切候选版本；两者都关闭则使用一般生命周期。
用户不必在看板填写验收标准。若受关卡约束的项目送达时没有标准，
负责的 Session 可在指派后、进入受关卡约束的阶段前，
用 `clawdline item acceptance <item id> --body-file <file>` 写入可观察的标准。
空白契约只能由负责的 Session 填写一次。
若用户透过 Clawdline 明确要求此 Root 修改该项目的验收标准，
请将完整替代 Markdown 写入文件，并执行
`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`。
项目版本是 `clawdline item show <id>` 打印的 `item version N` 行
（`item steps` 只打印验收版本）；消息 run 可从
`GET /v1/orchestrator/sessions/<conversation>/run` 读取。
保留的消息摘录须明确要求变更验收标准；禁止、讨论或单纯提问都不是授权。
若此 Root 恰好只负责一个未关闭项目，消息可透过对话上下文指向该项目；
否则必须用 ID 或标题明确指定。run 须晚于目前验收版本。
明确拒绝表示没有变更。结果不明时，使用相同的 `--key`、`--run`、
`--expected-version` 及文件字节重试。用户也可直接编辑。
在 merging 前变更验收标准，会使旧 PASS 与覆盖决定失效；开始 merging 后便会锁定。

若已记录为验证开启，请从干净、已注册的工作目录，对其已提交候选版本执行
`clawdline item phase <id> verifying`。CLI 会送出目前分支与完整 HEAD；
守护进程会检查 Project、周期基底、文件树与验收摘要。
脱离式只读 Codex 检查者对 Issue 使用 `code-reviewer`，对 Epic 使用 `reality-checker`；
对含参考图片或设计文档的 Feature 使用 `evidence-collector`，其他 Feature 则使用
`reality-checker`。其明确判定为 `PASS`、`FAIL` 或 `NEEDS_WORK`；
未验证的陈述会说明原因，绝不授权合并。
结果缺失或格式错误属技术故障，最多重试一次，之后升级处理。
Epic 最终端到端测试须等待所有子项目结束，并将受影响组件集成成单一可执行候选版本。
先执行聚焦的子项目测试与集成冒烟测试；不要对模拟 UI、分离的分支或未完成 API
派发最终浏览器或多帐户端到端测试。
派发前，须证明所选工作者能使用已授权浏览器或同等本机自动化工具开启目标 URL，
且具备必要的测试帐户、测试资料与来源权限。简报中应指出该途径；
单靠 `--permission-mode full` 不代表有浏览器访问权。
工具预检失败时，先解决问题再重试，不要让另一位验证者再次撞上相同阻碍；
预检失败不算一次端到端测试。
每个 Epic 规划一轮完整端到端测试，而不是每个子项目或修订各跑一轮。
修正缺陷后只重跑受影响情境。只有验收范围或集成边界发生实质变化时，
才重跑完整测试，并记录原因。
`verifying → merging` 需要对确切候选版本及验收标准的有效 PASS，
或附明确理由的覆盖决定；单一句验证说明不足以放行。
连续三次 FAIL 会先升级给执行中的上层 Epic 负责人；若其不可用，再升级给用户。
技术故障则另行升级。只有指定的上层负责人使用
`POST /v1/work/v2/agent/items/<id>/gate-decision`；用户使用
`POST /v1/work/v2/items/<id>/gate-decision`。Agent 绝不可使用用户的路由。
看板分别标示 AI、人工与技术覆盖决定，绝不将它们标示为检查者 PASS。
保留的详细资料达到容量上限时，用户先下载
`GET /v1/work/v2/items/<id>/gate-export`，验证 manifest 摘要，
再带该摘要与项目版本确认 `POST /v1/work/v2/items/<id>/gate-purge`。
清除只会移除符合条件的已关闭细节；汇总、最新事实与稽核记录仍保留。

**推进阶段。** 负责的 Session 自行推进其项目的执行阶段；其他人不代为操作，
轮次收据或条件解除也不会推进。阶段不是 `…/edit` 的栏位（`phase_not_editable`）。
只有对应工作确实发生后，才执行各阶段转换：

```
clawdline item phase <item id> implementing                  # when you start
clawdline item phase <item id> verifying                     # the change exists; now check it
clawdline item phase <item id> merging --verification "what was run and what it showed"
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin --landing-project <place id>
clawdline item phase <item id> deploying --no-landing-reason "why there is no code to land"
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

命令会读取项目版本、打印 Idempotency-Key（`--key` 可重试相同写入），再打印项目。
对应请求为：

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "no_landing_reason"?: "…", "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 一次推进一个阶段：`assigned → implementing → verifying → merging → deploying → done`。
  可从 `verifying` 退回 `implementing`；从 `merging` 退回 `implementing` 或 `verifying`。
  若项目已记录的验证关卡为关闭，也可依落地证据（见下文）从 `implementing`
  直接进入 `deploying`，`verification` 可省略；受关卡约束的项目会收到
  `verification_gate_on`，须走完整流程。其他情况不可跳过阶段；只有从
  `deploying` 才能进入 `done`。
- 进入 `merging` 需要 `verification`。进入 `deploying` 需要落地证据：
  此项目的 broker child 已落地，或由 `landing` 指定守护进程能同时在 Project 本机
  `target` 分支和 `refs/remotes/<remote>/<target>` 找到的提交；请先推送。
  若工作落地在另一个仓库（例如后端项目的变更由前端提交承载），
  以 `landing.project`（`--landing-project`）指定 `GET /v1/places` 中该 Project 的 ID，
  系统便会改在该处查询提交。Project 目录内的巢状仓库（`cloud/`）在这里是独立 Project。
  此证据只会写成一次 broker 的 **root 落地记录**；
  再次记录相同项目、仓库、提交与目标，仍是同一记录。
  项目历史以 `landing_id` 指向它；`clawdline landings --work-id <item id>` 也会列出。
  进入 `deploying` 前，守护进程会立即查询 git，确认已绑定 child 的分支是否已合并；
  因此刚完成的合并不必等待 broker 下一轮检查。
  无代码工作应使用 `no_landing_reason`（`--no-landing-reason`）取代落地证据；
  若同时带 `landing`，会收到 `invalid_landing_evidence`；
  若已绑定 child 仍欠落地，会收到指明任务的 `landing_owed`；
  若 child 已落地，会收到 `landing_recorded`。
  进入 `done` 需要 `deployment` 或 `no_deployment_reason`，依项目的
  `deployment_policy` 决定：`required` 只接受 `deployment`，`not_required` 只接受
  `no_deployment_reason`，`agent_decides` 两者皆可。所有步骤须先完成。
- `done` 会解除你的指派，并把项目移到 Session 的最近完成列。
  如需完成报告，请在此之前加入（见下文）。
- 拒绝码：`invalid_transition`（不是下一阶段或缺少证据）、`steps_incomplete`、
  `not_item_owner`、`item_unassigned`、`item_terminal`（用户重开项目）、
  `evidence_unknown`、`direct_landing_not_applicable`、`invalid_landing_evidence`、
  `landing_project_not_found`、`landing_commit_unresolved`、`landing_target_unresolved`、
  `landing_not_on_target`、`landing_remote_unresolved`、`landing_not_published`、
  `landing_owed`、`landing_recorded`、`landings_full`（项目最多保留 64 笔 root 落地）、
  `version_conflict`。遇版本冲突请重读再送。
  因缺少落地遭拒时，回复末尾会列出 broker 刚查到的情况，例如分支尚未合并，
  或仓库无法读取。

**以一个命令完成。** 工作落地后，`clawdline item finish <item id>`
可在单一交易中，将项目从 `implementing`、`verifying`、`merging` 或 `deploying`
一路推进至 `done`：

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?}, "no_landing_reason"?: "…",
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 每一步都与相应的 `item phase` 操作相同，经过相同关卡，并各自写入
  `item.phase_changed`。任一步遭拒，整个操作便遭拒，不写入任何内容。
- 落地信息会自动读取，而不是要求你逐项输入：对受关卡约束的项目，提交是关卡授权的候选版本；
  否则是已绑定 child 的落地提交。目标是其落地记录指定的分支，远端是该分支追踪的远端。
  你提供的栏位具有优先权，结果会像 `item phase deploying` 一样对照 git 验证。
  已记录为验证开启的关卡仍需 PASS：受关卡约束的项目从 `implementing` 执行 finish
  会收到 `verification_candidate_required`。请先从候选工作目录用 `item phase`
  进入 `verifying`，等待 PASS，再完成。
- 若已绑定 child 的分支刚合并，finish 本身会记录其落地：
  守护进程会先查询 git 再读取落地记录。因此合并后立即收到 `landing_required`，
  表示分支并不在目标上；拒绝消息会说明 broker 查到的情况。
- 无代码工作使用 `clawdline item finish <item id> --no-landing-reason "…" --no-deployment-reason "…"`，
  规则与 `item phase` 相同。
- 已在 `done` 的项目会按现状回复，不写入任何内容；重复观察相同落地不会再推进。
- 额外拒绝码包括 `verification_required`、`landing_required`、`deployment_required`
  （指出缺少哪一步的证据）、`landing_target_unknown`、`landing_remote_unknown`、
  `landing_remote_unreadable`、`landing_ambiguous`（请用参数明确指定）、
  `landing_owed`、`landing_recorded`，以及 `finish_not_started`
  （仍停在 `implementing` 之前）。

已指派项目可包含 `steps`。成功指派时，可从描述中两行以上的顶层 Markdown 清单产生步骤；
以 `clawdline item add` 创建的项目则带有其 `--step` 列。
每个步骤是该项目检查清单中的一条，不是另一个看板项目。
步骤验证后，用 `clawdline item step-done <item id> <step id>` 标记；
它会带 `{"session_id"}` 送至
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete`。
在 Agent 路由中，`expected_version` 可省略：省略时按目前版本写入；
指定 `--expected-version` 时则会比对，过旧会收到 `version_conflict`。
只要还有未完成步骤，进入 `done` 就会收到 `steps_incomplete`；
Clawdline 不会只因父项目阶段推进就自动勾除步骤。

**将自己负责的项目拆成步骤。** 若你负责的项目没有步骤，而工作分多阶段
（有几项可分别验证的变更，或涉及系统多个部分），请在实现前自行拆成有序步骤：
两至八个具体且各自可验证的步骤。单一、直接的变更**不需要**步骤；
不要为了凑清单而加一条。若工作后来比预期大，再加入必要步骤。

```
clawdline item step-add <item id> "Wire the route" "Cover it with a test" "Say it in the guide"
```

标题可作为参数，或从标准输入逐行读取非空白内容。
命令在写入每个标题前都会重读项目并打印该次 Idempotency-Key，
之后打印简短收据及 `item show` 提示。每个标题都会发出一次仅限负责人的请求：

```
POST /v1/work/v2/agent/items/<id>/steps     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

其中 `"position"` 应比最后一个现有步骤的位置大一，因为步骤按位置排序。
之后逐一验证，再用 `clawdline item step-done` 标记完成。
这不属于禁止对看板项目和待办事项「自行发起」的情况：项目已由你负责，
步骤只是向用户展示受托工作的各阶段。

若解决问题或事件需要大量调查，才能找到根因或从看似可行的方案中辨认真正修复方式，
请在项目进入 `done` 前加入用户易读的完成报告。
直接且可观察的简单修正不需要报告。
把事件经过、根因、变更内容、验证方式与任何剩余边界写入文件，然后执行：

```
clawdline item doc <item id> --role completion_report --title "Completion report" --body-file report.md
```

命令会代你调用 `POST /v1/work/v2/agent/items/<id>/documents`
（§10 的 Epic 部分，即 `clawdline guide zh-Hans epic`，列有各栏位）。
若自行拼装 curl 调用该路由，却没有命令所读取的凭证，会收到 `401 unauthorized`。
本文为 Markdown，最多 64 KiB。请写给回报问题的人，而不是输出原始除错日志，
并避免私人资料。有效负责人须在项目进入终止状态前加入报告；
遇版本冲突后请重读。完成报告是有来源的叙述，绝不取代验证、落地或部署证据。
若有报告，它会留在已关闭的看板项目上，也能从 Session 的 Recently Done 列直接开启。

`/v1/board` 是 Swift 应用程序旧卡片的只读路由。
落地是 broker 的事实：不可手动标记项目已落地（`422 landing_is_broker_fact`）。

### Epic 与 Feature：实现前遵循用户的审查开关

在执行周期中已记录为规划开启的 Epic，需要计划与独立审查。
Feature 带有用户的 **Needs independent review** 开关（项目的 `review_required`；
`clawdline item steps <id>` 会打印）。只有用户可在看板设定；你不能设定，
也不自行判断 Feature 的风险。要求进入 `implementing` 时，守护进程会读取该开关。

- **未勾选**（预设）：写入 Feature 的简短验收标准、实现并执行聚焦测试。
  不要撰写供审查的计划、派发 `plan_review` child，或记录风险评估。
- **已勾选**，以及所有规划开启的 Epic：使用经审查的计划流程。

若你认为未勾选的 Feature 值得审查，请告知用户，让用户勾选；
Agent 没有自行要求守护进程审查的途径。

1. 仔细规划，并把计划写入项目：
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. 派发只读 child，要求它严格审查计划，找出缺漏、错误和风险：
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. 等待 child 完成。以 `--work-id` 派发且成功完成的审查 child，
   会自行在项目上将审查收据记录为 `plan_review` 文档；
   请检查 `GET /v1/work/v2/items/<id>` 的 `.documents`。
   只有文档不存在时（例如派发 child 时未指定 `--work-id`），才手动记录：
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   对同一任务再次执行命令没有副作用：会回复已存在的文档。
   若要向用户简短说明计划如何依审查结果调整，应另写一份 `other` 文档，而不是第二份审查。
   若 Feature 计划在审查后变更，且变更仍在先前审查的风险边界内，
   请在修订计划之后加入标题为 `Review boundary assessment` 的 `other` 文档，
   内含 JSON `{"new_risk_boundary":false,"reason":"..."}`。
   若产生新边界或边界不确定，则需重新进行聚焦审查。
   Epic 原有的最多两次审查限制仍适用。
4. 以 `clawdline item step-add <item id> …` 将工作拆分成步骤。
5. 最后才执行 `clawdline item phase <item id> implementing`。

验证应规划为连续流程。每个实现 child 用聚焦测试检查自己的代码；
Epic 负责人集成受影响组件，再执行最小但有用的跨组件冒烟测试。
只有集成候选版本可运作后，才派发真正的端到端验证，以及适用的独立 UX／产品审查。
派发前检查验证者的浏览器途径、目标 URL、测试帐户、测试资料和权限。
不要一再派发只读验证任务来发现或回避缺少浏览器的问题：
先修复访问权，或选择同等的本机浏览器工具。
预检失败不算端到端测试。对每个 Epic 的稳定候选版本规划一轮完整端到端测试，
而不是对每个 child 或修订各跑一轮；聚焦修复后只重跑受影响路径。
只有验收或集成发生实质变化时才重跑整轮，并记录原因。

`clawdline item doc` 会读取项目版本和最后文档位置、打印 Idempotency-Key
（`--key` 可重试相同写入），再打印项目。内文来自 `--body-file` 或标准输入。
对应请求为带
`{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`
的 `POST /v1/work/v2/agent/items/<id>/documents`；角色包括 `spec`、`design`、`test`、
`deploy`、`completion_report`、`other`、`plan`、`plan_review`。

修改文档时，以相同的 `--role` 和 `--title` 再写一次：守护进程会替换内文、参照与位置，
保留 ID，版本加一，并记录 `document.revised`。
CLI 会显示 `added … at v1` 或 `revised … to vN`，`clawdline item show`
会打印每份文档的 `vN`。重送相同文本不会更改内容，只会回复现有文档，
因此可安全重试。旧文本不会保留；若要保留两版，请用不同标题。

- `plan`、`plan_review` 和审查边界不会就地修改：每次写入都创建新文档，
  因为规划关卡会依序读取，且审查会指明其读取的计划。
- 每个项目最多保留 32 份文档，`completion_report` 不计入上限，
  即使一般文档已满仍可加入。每个项目只保留一份 `completion_report`；
  不论新标题是什幺，再次写入都会修改原报告并采用新标题。
- 第 33 份一般文档会收到 `documents_full`，不写入任何内容；
  消息会指出可改用的 `clawdline item doc` 命令，以修改现有文档。

- `plan` 和 `plan_review` 只适用于 Epic 或 Feature；其他种类会收到
  `document_role_not_applicable`。
- `plan_review` 的 `reference` 是审查计划的 Clawdline child 任务 ID。
  守护进程只在该任务存在（否则 `plan_review_task_unknown`）、由项目负责的 Session 派发
  （`plan_review_task_not_owned`）、若指定工作线则属于此项目
  （`plan_review_task_other_item`）、种类为 `plan_review`
  （`plan_review_task_wrong_kind`）、以 `success` 结束
  （`plan_review_task_unfinished`），且派发时间不早于最新计划
  （`plan_review_task_stale`）时接受。前面没有计划的审查会收到 `epic_plan_required`。
  审查 child 自动创建的文档也须通过相同检查；同一任务的重复写入具有幂等性。
- 规划开启的 Epic 没有经审查计划时，执行 `clawdline item phase <item id> implementing`
  会收到 `epic_plan_required` 或 `epic_plan_review_required`。
  规划开启、且用户勾选「Needs independent review」的 Feature 也一样，
  会收到 `feature_plan_required` 或 `feature_plan_review_required`；
  未勾选时只需验收标准。已勾选 Feature 的计划若在审查后修改，
  还需提出边界未变的证据或再审查。规划关闭的 Epic 可直接进入 implementing。
- 关卡会读取最新 `plan_review` 任务的审查收据。
  每项发现的 `severity` 为 `blocking` 或 `non_blocking`
  （旧范本的 `important` 和 `minor` 均视为非阻挡）。
  没有发现时判定为 `safe_to_land`；全为 `non_blocking` 时为
  `proceed_with_findings`；有任一 `blocking` 时为 `changes_required`。
  若最新审查含阻挡项，仍已指派的项目无法执行 `item phase implementing`，
  也无法以 `--work-id` 派发其他任务，唯 `--kind plan_review` 例外；
  会收到 `epic_plan_review_blocking` 或 `feature_plan_review_blocking`。
  拒绝消息会列出阻挡发现与下一步命令：先修订计划，再派发新审查。
  只有非阻挡发现，或旧收据中未标示严重程度的发现，才允许继续。
  Epic 最多审查两次：若第二次仍有阻挡项，修订计划以回应发现，
  修订计划即可进入 implementing；不要派发第三次审查。

**将 Epic 拆成子项目并分派。** 这是「Session 只能依用户消息创建看板项目」
及「只有用户可指派项目」的唯一例外：用户已将 Epic 指派给你，
这就是拆分它的授权。经审查计划使 Epic 进入 `implementing` 后，
若部分工作更适合其他 Session，就在其下创建 Feature 或 Issue 项目并指派：

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | description on stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- 终端机 ID 可从 Session 通讯录 `GET /v1/orchestrator/sessions`
  （`clawdline guide zh-Hans send`）查询；接手 Session 必须在 Epic 的 Project 工作。
  你可将子项目指派给自己；`--assign-new` 会以指明 Epic 的 Root Assignment 开启新 Session。
  不指定 `--assign` 参数时，子项目保持未指派，等待用户处理。
- `item child` 会读取 Epic 版本、打印 Idempotency-Key（`--key` 可重试相同写入），
  再打印子项目。对应请求为带
  `{"expected_version", "session_id", "kind", "title", "description", "steps"?, "deployment_policy"?, "assign"?: {"mode": "existing_session", "terminal_id"} | {"mode": "new_session", "assistant"?, "model"?, "persona"?}}`
  的 `POST /v1/work/v2/agent/items/<epic id>/children`；成功时回复
  `201` 与 `{"item", "assigned", "assignment_error"?: {"code", "message"}}`。
  子项目位于 Epic 的 Project，带有指向 Epic 的 `parent_id`，
  卡片会显示由 Epic 负责的 Session 创建。
  步骤来自你的 `--step` 列；未提供时，在指派后从描述的清单产生。
- 系统先创建子项目，再进行指派。若指派失败，子项目**仍存在，且未指派**；
  回复包含 `assignment_error` 与指派拒绝码（`session_unavailable`、
  `project_mismatch`、`assignment_failed` 等），命令以状态 1 结束。
  请用 `item assign` 再次指派，或留给用户处理。
- `item assign` 对应带
  `{"expected_version", "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`
  的 `POST /v1/work/v2/agent/items/<child id>/assign`；
  它会将你负责的 Epic 下仍开启的子项目移给另一个 Session，
  效果与用户自行指派相同。
- 下列拒绝都不会写入：`not_epic_owner`（你不是 Epic 负责人）、
  `parent_not_epic`（父项目不是 Epic）、`epic_not_planned`
  （Epic 尚未进入 `implementing`；子项目应根据已审查计划创建）、
  `item_terminal`（Epic 已结束）、`child_kind_not_allowed`（只允许 `feature` 或 `issue`）、
  `epic_children_full`（Epic 最多保留 32 个子项目，无论开启或关闭）、
  `not_epic_child`（对非 Epic 子项目执行 `item assign`；应由用户指派，
  除非其消息要求你代办，见 `clawdline guide zh-Hans board`）、`invalid_assignment`、
  `version_conflict`、`persona_not_applicable`（422：为现有 Session 指定 persona），
  以及 `unknown_persona`（400：目录中没有该 ID）。
- **persona** 是新 Session 启动时使用的角色：在整段对话期间，
  系统提示词会加入文本，使它依该角色的方式工作。
  仅适用于新 Session（`--assign-new`、`--new`、`dispatch`）；
  现有 Session 保留启动时的角色。预设不指定。
  persona 绝不覆盖 `CLAUDE.md`／`AGENTS.md`、简报、`CHILD.md` 或本协议。
  `GET /v1/personas` 列出角色；每个角色的 `teams` 列出它所属的所有团队，
  一个角色可属于多个团队。ID 包括：
  - `architect`：规划 Epic；
  - `backend`：守护进程、API 或存储功能；
  - `frontend`：控制台或手机版面功能；
  - `minimal-change`：以最小且有效的修正处理 Issue；
  - `code-reviewer`：审查及 `plan_review` child；
  - `reality-checker`：验证时先看证据，再说「可运作」；
  - `security`：涉及权限、配对或 Cloud 的工作；
  - `technical-writer`：文档与指南；
  - 营销，适用于博客、网站或文档仓库：`seo`（页面与中继资料）、
    `content-writer`（在文件中起草文章）、`ai-search`（可供 AI 答案引擎引用的页面）、
    `social-media`、`instagram`、`email`（电子报）、`growth`（可量测实验）、
    `pr`（公告）。
  - 产品、品质与营运：`product-manager`、`sprint-prioritizer`、`feedback-synthesizer`、
    `trend-researcher`、`ux-researcher`；`test-automation`、`accessibility`、`performance`、
    `api-tester`、`evidence-collector`（依搜集的证据逐项判定 PASS 或 FAIL）；
    `sre`、`devops`、`incident-commander`、`finops`、`secrets`。
  - 设计与商务：`ui-designer`（符合 Project 设计系统的画面）、`ux-architect`
    （流程与版面结构）、`brand-guardian`（品牌一致性）、`ui-finish-gate`
    （发布前的视觉检查）、`image-prompt`（图片生成提示词）、`pricing`、
    `customer-success`、`support`（回复草稿）、`analytics`（根据真实资料作答）、
    `devrel`（可执行范例）、`privacy`（个人资料检查；非法律建议）。
  - `zero-review-lead`：负责从零重新审视既有功能或流程的审查 Epic；
    规划角色视角，带同一份事实资料包派发只读审查 child，
    再依其证据形成目标设计；其技能是 `zero-based-review`。
- **若 Epic 改变人们实际使用的体验，请加入独立 UX／产品审查。**
  在计划中判断 Epic 是否改变人机接口、使用流程或产品政策。
  若是，合并前至少派发一个只读专家 child；针对版面、互动与端到端产品流程，
  预设使用 `ux-architect`：

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  简报应指明集成候选版本，要求桌面及最小支持手机尺寸的证据、键盘与萤幕阅读器行为、
  死路、产品适配性、严重程度和具体建议。证据不可取得时，须**标记未验证并说明原因**。
  若主要风险是政策和范围，而不是版面，请改用 `product-manager`；
  若另做一轮发布前视觉检查具有实质价值，则加入 `ui-finish-gate`。
  解决所有阻挡发现，并在 Epic 的验证证据或完成报告中记录任务 ID、判定与处置。
  若没有对人可见的影响，请在计划中说明原因，不要增加审查进程。
  记录本次审查涵盖的范围。一般而言，对集成后的 Epic，每个相关专家只派发一次；
  不要把 UX、品牌、安全等角色当检查清单逐一派发。
  小幅文案、间距、测试或范围内发现的修正，请自行用聚焦检查完成。
  只有后续变更实质改变使用流程、产品政策、品牌方向、安全边界，
  或超出已记录范围的其他风险时，才重新派发；
  请指出改变的边界，且只请求对应专家。
  此审查绝不取代已记录的计划关卡，或验证关卡对确切候选版本的检查者 PASS。
- **每个 child 合并后，你仍须负责 Epic。** 立即重读该 child 的项目和
  `clawdline item steps <child id>`，确认每个步骤均已完成。
  合并不会关闭 child，`merging` 也不是可长期停留的状态。
  child 的负责 Session 须完成剩余步骤，为已可从本机目标及 `origin/main` 取得的确切提交
  记录落地收据，再依其部署政策提供部署证据或无需部署的理由，推进 `deploying` → `done`。
  若 child 由你负责，请自行执行；若由另一个 Session 负责，请向它跟进，
  或使用授权的子项目重新指派途径；不要冒充其负责人（`not_item_owner`）。
  如有 broker 完成通知，请 ACK；然后分类工作目录残留，只移除已证实与落地内容一致、
  或任务暂存的资料。未落地、混合或未知的字节应保留给下一位负责人。
  所有 child 均达 `done` 或 `cancelled` 前，不可声明父 Epic 完成；
  `epic_children_open` 会指出剩余数量。不要创建 Epic 子项目以外的看板项目。
- **已完成的 child 不会关闭其独立 Feature Root Session。**
  对 Epic 以 `--assign-new` 开启的每个 Root，使用
  `clawdline session close --dry-run --terminal <id>` 读取 `closeability`；
  只有确实由此 Epic 开启的 Root 才会回复 `closing as epic_owner`。
  不要依标签或终端机位置推测所有权，也不要把 `clawdline session report` 当作关闭。
  child 到达 `done` 后，请其 Root 负责人稽核自己的任务、落地、通知、待办与工作目录，
  再完成关闭报告。证明只能透过目前守护进程支持的路由取得；
  退役 Swift 应用程序的关闭路由不算。
  若该路由或受保护的关闭操作不可用，请记录产品阻碍与下一位负责人，保留 Session。
  只有身分和工作都验证通过，且 `closeability.state=safe` 时，
  才能用 `clawdline session close --terminal <id>` 结束它。
  关闭前它会重读清单；成功关闭后再次执行会收到 `session_not_found`。
  不要用 `clawdline close <terminal id>` 绕过保护。
  对 `blocked` 状态，请与指出的处理者跟进。
  对 `unknown`（包括 `terminal_unreadable`），保留 Session，
  记录缺少的证据与下一位负责人；不要强制关闭、封存或宣称已清理。
  声明 Epic 协调完成前，请逐一列出每个 Root 的关闭结果或具名阻碍。
  看板 `done` 不取代这份清单。

## 11. 协调

**机器协调者（「Clawdfather」）。** 它在 Project 之外、由守护进程管理的机器工作空间中，
回报 Session 状态并管理受支持的机器操作。它绝不编辑 Project 原始码，包括 Clawdline。
用户明确要求工程工作时，先用 `clawdline item add --project … --assign-new`
创建 Project 看板项目，再委派给 Project Session。没有该要求时，请提出项目供用户接受。
已指派的 Project 负责人处理 child 派发、验证与落地。
工作空间是组织边界，不是文件系统沙箱。新绑定须从该工作空间创建；
既有绑定仍可读取。从控制台独立的 Clawdfather 操作开启 Session，
再注册其对话 ID。产品边界见 `docs/clawdfather-role.md`。
在该新 Session 中执行 `clawdline coordinator bind`，注册它，
或在证实前任者已离线时重新绑定。
命令会读取自己的对话 ID，拒绝替换仍在线上或无法读取的持有者。
`GET /v1/orchestrator/coordinator` 可检查角色；
`/coordinator/bearings` 提供机器概况：执行中任务、待落地项目、开启的等待、
无法送达的消息、已占用租约，以及状态为 `unknown` 的项目。
带 `{"session_id": "<conversation id>"}` 的 `POST …/coordinator/register` 可取得角色；
绑定 Session 离线后，`POST …/coordinator/rebind` 可透过
`expected_coordinator_id`、`expected_generation` 转移角色。
继任会收到 `501 succession_unavailable`。

**文件等待。** wait 表示「负责人完成这些路径后通知我」。
它是记录和消息，不是锁，也不是文件监视器。

- `POST /v1/orchestrator/waits`：
  `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`
  （Session ID 为对话 ID）。负责人会在输入框收到一次通知。
- 负责人结束等待：以 `{"owner_session_id", "commit"?, "note"?}` 调用
  `POST /v1/orchestrator/waits/<id>/release`；每位等待者都会收到通知。
  计时器不会自动解除等待。
- 等待者退出：以 `{"waiter_session_id"}` 调用 `POST …/waits/<id>/cancel`。
- `409 owner_busy` 和 `502 request_delivery_failed` 表示**等待已记录**，
  但负责人尚未收到通知。`502 release_incomplete` 列出仍待通知的人；请重新送出解除请求。

**租约。** 有两种资源：`heavy_compile`（机器唯一的编译时段）和 `landing`
（每个工作目录一个）。

**建置或测试套件请透过 `clawdline heavy -- <command>` 执行**，不要直接执行。
它会排队等候 `heavy_compile`，直到机器有足够内存（可用量至少四分之一、上限 1 GB，
且内存停滞不超过 10%），再以较低优先顺序执行命令。
在 Linux 上，内存不足时它也会成为内核优先终止的进程。
执行期间会续租，结束后释放，并保留命令本身的结束码。
若守护进程不存在或收到无法识别的拒绝，它不会拒绝建置：
仍会执行命令，但在 stderr 说明。
若取得时段与内存前就超过 `--max-wait`（预设 30 分钟），
它会放弃排队，不执行命令，并以 **75** 结束；
此码不会与命令自身的失败混淆，稍后可重试。
等待期间只会在开始与结束时各打印一行，中间不输出；请用一次长等待等候
（§2「等待长时间命令」）。在 `heavy` 内调用 `heavy` 会直接执行。
`--min-available 1500M` 可要求更多内存；`--no-slot` 只检查内存。
若仓库提供 `tools/heavy.sh <command>`，它会代你找到可执行文件。

- `POST /v1/orchestrator/leases`：
  `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`。
  其中 `checkout` 仅用于落地租约。
  回复 `granted`，或带 `position` 和 `retry_after_seconds` 的 `queued`。
  排队请求应用相同 `request_id` 再次询问。
- 使用 `{"request_id", "resource", "checkout"}` 调用
  `POST …/leases/renew | release | cancel`。持有者透过 `/renew` 续租
  （以自己的 `request_id` 再调用 `POST /v1/orchestrator/leases` 也会续租）。
- 须在 60 秒内续租，否则系统会视为租约已失效；`409 lease_lost` 即表示如此。
  排队者达 32 个时会收到 `429 queue_full`。

**图表**（`GET /v1/orchestrator/graphs`）是依已派发任务的 `graph` 栏位计算的只读检视。
**回收**（`/v1/orchestrator/reclaim`）会扫描已完成的工作目录；
除非本文指定 `{"dry_run": false}`，否则 POST 只会试执行。

## 12. 遭拒时如何处理

- 依 `error.code`（或扁平格式中的 `error`）分支；消息文本是给人阅读的。
- `retry_after` 表示容量限制：等待指定时间，再送出相同请求。
- `409 stale_write`、`503 orchestrator_store_busy`：存储系统忙碌，
  可以安全地重送相同请求。
- 任何地方出现 `unknown`（所有权、存活状态、来源），都表示守护进程无法读取。
  它不代表「不存在」；不可据此删除或声明任何内容已失效。
- 若预期中的路由回复 `404 not_found` 或 `501`，表示此守护进程没有该路由。
  请如实说明，不要改用 Swift 应用程序路由或提供者原生 subagent 代替。
