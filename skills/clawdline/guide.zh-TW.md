# Clawdline 使用指南

這是 `clawdline guide` 的繁體中文版。兩者內容有出入時，以英文版（`clawdline guide`）為準。

給跑在裝了 **Clawdline Next** 的機器上的助理 session 看，Claude Code 或 Codex 都一樣。這份指南只寫
這個 daemon 今天有提供的東西，其他一概不寫：下面每一條路由都是印出這份指南的那個 build 註冊的，
少了一條，就會有測試失敗。要看請重新執行 `clawdline guide zh-TW`（英文版是 `clawdline guide`），
不要相信手上的副本。`clawdline guide zh-TW` 只印每個 session 都需要的核心，並列出其他部分；做到某一
部分的工作時再印那一部分（`clawdline guide zh-TW dispatch`），要全文用 `clawdline guide zh-TW all`。

## 0. 如果你是從 Swift app 學會 Clawdline 的，先讀這段

Swift app 已於 2026-09-19 退役：它被停掉、取消了登入時啟動，port 7717 沒有人在聽。它的目錄
`~/.config/clawdline` 還在，而且還會被讀——只讀不寫——裡面是這個 daemon 自己從來沒有的歷史。
這個 daemon 不是它的複製品，最容易踩空的是下面五個差異：

1. **Task 目錄是 `<state dir>/tasks`，不是 `/tmp/.clawdline`。** `/tmp/.clawdline` 當初歸 Swift
   broker 管；兩個 broker 往同一個目錄寫 task id，會在沒人看的地方撞在一起。兩個都不要寫死：從
   inventory（§3）讀 `task_root`，把 `task.json` 寫在它底下。
2. **沒有 workflow envelope，也沒有 workflow 路由可以呼叫。** 訊息不再帶看板分類，session 也從不
   自己開看板卡片。`POST /v1/orchestrator/sessions/<terminal>/workflow` 還在，只是為了讓舊的 helper
   不會在 turn 中途失敗：它回 `workflow_retired`，什麼都不記錄，新的程式碼不可以呼叫它。
3. **參與看板要透過 proposal 和 decision**（§10）：session 提案，人來回答。沒有什麼要 `begin` 或
   `deliver` 的。
4. **換了一扇門。** Port 7727（或 `CLAWDLINE_NEXT_PORT`），狀態放在 `~/.config/clawdline-next`
   （或 `CLAWDLINE_NEXT_DIR`）。絕對不要讀 `~/.config/clawdline`：那裡的 token 不是這個 daemon 的，
   會被 `401 unauthorized` 拒絕。
5. **Swift app 有、這個 daemon 沒有的東西：** durable report 升級（回
   `501 durable_report_promotion_unsupported`）、coordinator succession（回
   `501 succession_unavailable`）、task 的取消路由，以及 brief 欄位 `serialize`、
   `attach_session`（兩個都會被點名拒絕，code 是 `bad_task`）。`reasoning_effort` 有支援：
   `high` 或 `xhigh`，而且只在 `codex` 的 task 上。

## 1. Root 還是 child

如果你的第一則訊息寫著 *"You are a Clawdline CHILD agent for task …"*，你就是 **child**。訊息裡指定的
`CHILD.md` 管你：你不派工，不送 turn receipt，用 `clawdline task accept` 簽收，最後用
`clawdline task finish` 收尾。讀到這裡就可以停了。

否則你是 **root**：一個有人正在跟你對話的一般 session。後面的內容都是寫給你的。

## 2. 連上 daemon

**有指令可以用，就用指令，不要自己組 curl。** 指令在自己的 process 裡讀憑證，所以憑證不會出現在命令列、
`ps`、指令輸出，也不會出現在你的 transcript 裡。

| 指令 | 做什麼 |
|---|---|
| `clawdline guide [zh-TW]` | 這份指南。不需要 daemon |
| `clawdline session report --summary "…"` | 記錄你已經完成的 turn（§7） |
| `clawdline send --to <terminal> "…"` | 把一則訊息轉進另一個 session（§8） |
| `clawdline notify --title "…" --body "…"` | 推播一則通知給使用者（§9） |
| `clawdline assistants` | 每個助理的帳號還剩多少額度 |
| `clawdline landings` | 這台機器上所有還欠著的 landing |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | 一個 session、child task 或 Board item 花了多少 token，依類別分；預設是你自己 |
| `clawdline cloud pair [--offer <code>]` | 把一個 Cloud 瀏覽器與這台機器配對 |
| `clawdline task show [--json] <task id>` | 精簡地看一個 child task：狀態、verdict、summary、leftover 標題、驗證、landing、checkout（§5） |
| `clawdline task ack <task id> <notice id>` | ACK 一則 child 完成通知（§5） |
| `clawdline task accept <task dir>` | child 簽收 briefing。root 永遠不執行它 |
| `clawdline task finish <task dir>` | child 的完成動作。root 永遠不執行它 |

上面那些 orchestration 指令成功時會印出 daemon 回的 JSON；被拒絕時印出
`refused, <status> <code>: <message>`，exit code 是 1。Cloud 指令另有自己給人看的成功與錯誤輸出。
`--port` 可以覆寫 port。

`clawdline usage` 讀的是 token 帳本（repository 裡的 `docs/token-ledger.md`）：每個 token 花在什麼上面——
`board`、`protocol`、`rules`、`impl`、`delegate`、`harness`、`talk`、`compaction`、`other`。
不帶旗標時讀你自己的 session，由 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID` 指名。輸出是一行標頭
（呼叫次數、最大 context、費用），接著每個類別一行、依費用排序——占比、token、費用——最後列出每個缺口；
`--json` 印出 daemon 的原始回答。`rules` 是上限，輸出也會這樣標：守衛和其他工作寫在同一條 shell
指令裡時，整條指令都算給它。路由是 `GET /v1/usage/sessions/<conversation>`、
`GET /v1/usage/tasks/<task id>` 和 `GET /v1/usage/items/<item id>`，用配對過的裝置或 orchestrator
token 讀。帳本還沒讀到、或已經讀不到的 session 會回它的原因：`not_yet_read`、`transcript_missing`
或 `transcript_unreadable`，絕不回一個空的總數；沒人認得的 id 回 404 `unknown_session`、
`unknown_task` 或 `unknown_item`。帳本是否還在讀，看 `/v1/diagnostics` 裡的 `usage`。

### 配對一個 Cloud 瀏覽器

配對會改變誰能讀這台機器。配對過的瀏覽器立刻就能讀；Cloud 的 `commands` 開啟時，它還能操作這台
機器。只有在人明確要求配對該瀏覽器，或直接給了完整配對指令或 offer 時，才執行配對指令。配對本身
不會開啟 commands；那是另一個獨立設定。

支援兩個方向：

1. **瀏覽器顯示 offer。** 在機器上照原樣執行它給的整行指令：

   ```sh
   clawdline cloud pair -offer '<code>'
   ```

   單引號要保留。offer 是不透明、短效且只能使用一次的秘密；不要解碼、修改、保存，也不要在最後答覆
   中重貼。若已失效或用過，請瀏覽器產生新的 offer，不要重試或自行改內容。
2. **由機器建立邀請。** 執行 `clawdline cloud pair`。它會印出一個一次性的
   `https://app.clawdline.com/#pair=…` 連結並等待。使用者要在想配對的瀏覽器裡、登入同一個 Clawdline
   Cloud 帳號後，完整開啟該連結。連結與 offer 一樣：不要公開或保留。

成功時會印三行：`paired` 是瀏覽器 device id，`browser` 是 browser fingerprint（瀏覽器指紋），
`machine` 是 machine fingerprint（機器指紋）。把 browser fingerprint 與瀏覽器顯示的值比對，也把
machine fingerprint 與該機器顯示的值比對。對不上就不算成功：立刻用 `paired` 的 id 執行
`clawdline cloud revoke <device-id>`，再回報指紋不符。`clawdline cloud devices` 會列出目前的瀏覽器
roster 與本機信任狀態；配對後要唯讀複查，也用這條。

這些指令都透過本機正在執行的 daemon。失敗時回報 stderr 原文。除非使用者另外要求，否則不要順手
開啟 Cloud、登入、開啟 commands、rotate key，也不要替換使用者給的 offer。

**東西在哪裡。**

- Port：`CLAWDLINE_NEXT_PORT`，沒設就是 **7727**。只聽 loopback：`http://127.0.0.1:<port>`。
- 狀態目錄：`CLAWDLINE_NEXT_DIR`，沒設就是 `$XDG_CONFIG_HOME/clawdline-next`，再沒有就是
  `~/.config/clawdline-next`（Windows 上是 `%APPDATA%\clawdline-next`）。
- `GET /v1/health` 不需要憑證，會回 `served_by: "clawdline-go"`。用它分辨「沒在跑」和「被拒絕」。

**憑證。** 一共三種，session 用第一種：

| 憑證 | 在哪裡 | 怎麼送 | 開得了什麼 |
|---|---|---|---|
| Orchestrator token | `<state dir>/orchestrator-token` | header `X-Clawdline-Orchestrator` | `/v1/orchestrator/`、`/v1/work/`、`/v1/board` 底下的全部路由，以及 `GET /v1/places`、`POST /v1/artifacts/images` |
| Task secret | root 派工時自己選 | header `X-Clawdline-Task-Secret` | child 自己在 `/v1/orchestrator/tasks/<id>/` 底下的路由，以及 `POST /v1/orchestrator/proposals` |
| Device token | `<state dir>/local-token`，或配對過的裝置自己的 token | `Authorization: Bearer` | console 的路由（`/v1/sessions/…`）。session 用不到 |

把 orchestrator token 當成 `Bearer` 送，它會被拿去跟裝置比對，然後被拒絕。token 錯誤或沒帶時回
`401 unauthorized`「This needs a paired device.」——字面上講的是裝置，真正的原因是 token。

**非用 curl 不可時**，別讓 token 出現在命令列上：

```sh
DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"
PORT="${CLAWDLINE_NEXT_PORT:-7727}"
auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")"; }
curl --fail-with-body -sS -H @<(auth) "http://127.0.0.1:$PORT/v1/orchestrator/inventory?project=$PWD"
```

- `--fail-with-body`：少了它，被拒絕時 exit code 還是 0，看起來像成功。
- **每一個帶 body 的 POST 都要加 `-H 'Content-Type: application/json'`**，否則會被
  `415 unsupported_media_type` 拒絕。只用 `curl -d` 的話，送出去的是 form 型別。
- Body 上限 2 MiB，除非該路由規定得更小。
- 像 `%47` 這種 tmux terminal id，放進路徑時要當成一個 segment 跳脫：`%2547`。

**拒絕有兩種形狀。** 看 code 決定怎麼處理，永遠不要看那句話：

- `{"error":{"code":"…","message":"…","request_id":"…", …extras}}`——gate 和 broker 用這種。
  `retry_after` 這類額外欄位放在 `error` 裡面。
- `{"error":"<code>","detail":"…"}`——找不到路由、HTTP method 錯誤，以及部分讀取用這種。

不歸這個 daemon 管的路由會回 `501 not_implemented`，而且拒絕訊息會說出那條路由的名字——
一般機器上就是這個答案。只有在有人刻意用 `CLAWDLINE_NEXT_UPSTREAM_PORT` 在它後面擺了另一個
daemon 時才會轉送出去，轉不到則回 `502 upstream_unreachable`，並說出打不通的位址。兩種都不是
這個 daemon 給的答案。2026-09-19 之前轉送是預設開著、且指向 7717 的 Swift app，所以當時寫的筆記
會說沒接管的路由會跑到那個 app——現在不會。

## 3. 派工之前：先讀已經存在的東西

別的 session 可能已經在做你要做的事，而且從共用的 working tree 上看不出來：一份已經完成、放在還沒
合併的 branch 上的交付，不會出現在任何 `git status` 裡。先讀。

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- 回 `generation`、`task_root` 和四個清單：`live`、`unlanded`、`droppable`、`unreadable`。每一列都帶著
  一個 daemon 會接受的 `do`。帶了 `claims` 時，每一筆 live 列都會標出它 `overlaps` 什麼。
- **派工一定要帶 `generation`**（§4）。它是由各列 sealed 欄位算出來的 16 個 hex 字元；只要有一列開始、
  結束或改了 claims，它就會變。
- **`task_root` 就是 `task.json` 要放的地方。** 這是這個 daemon 自己的欄位；Swift broker 沒有，因為它
  把 `/tmp/.clawdline` 寫死了。
- `project` 不是某個 Git repository 裡的絕對路徑時，回 `400 bad_request`。

另外值得讀一次的：

- `GET /v1/orchestrator/inflight?project=…`——repository 裡每一條還沒結束的工作線，由誰負責、claim 了
  什麼。
- `clawdline assistants`——每個助理的 `availability`（`ok`、`low`、`exhausted`、`unknown`）、
  `windows`、`stale`、`resets_at`。讀完再決定派給誰；不會有任何東西因為額度而拒絕派工。

**到底該不該派出去？** 能拆成獨立幾塊的工作，平行做比較快。每一步都依賴上一步的鏈，拆開反而更糟，
因為每次交接都會把鏈切斷。診斷、比寫 briefing 還小的工作，以及有人正在等的事，都留在自己的 session
做。這台機器的規矩寫在 `<state dir>/dispatch-policy.md`（還有使用者自己的
`dispatch-policy.local.md`）；每個 child 的 briefing 裡都會附上它們。

## 4. 派出一個 owned child

owned child 是掛在你底下、範圍有限的 task。**彙整、整合和 landing 都還是你的事。**

**下面四個步驟，一個指令就做完**，brief 從 stdin 或檔案讀：

```sh
clawdline dispatch --title "…" --claims a.go,b.go [--isolation worktree] [--assistant codex] \
  [--permission-mode ask|edits|full] [--timeout 90] [--kind k] [--deliverable p] [--model m] \
  [--work-id uuid] [--label "…"] [--project-dir D] < brief.md     # 或 --instructions-file brief.md
```

它會產生 id 和 secret、讀 inventory 拿 `generation` 和 `task_root`、寫 `task.json`、送出 task；遇到
一次 `stale_inventory` 會重讀 inventory、再送一次。輸出是 `dispatched <id> <state> [worktree <path>]`，
接著每個警告一行——daemon 給的，以及每個 claims 和你重疊的 live task。`--json` 改成印 daemon 的原始
回答。被拒時在 stderr 印 `refused, <status> <code>: <message>` 並以 1 結束；每個 code 的意思見本節最後
的表。root 是你的對話，取自 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID`，否則用 `--conversation`；
child 的 assistant 預設跟你一樣，`--assistant` 可以改；project 預設是目前目錄的 git top-level，
`--project-dir` 可以改。`--claims ""` 表示這個 child 什麼都不寫。secret 不會出現在 argv、`task.json` 或
輸出裡，token 的讀法跟其他 thin command 一樣。

它做的步驟如下，給沒有這個 binary 的呼叫者照著做：

**1. 選一個 id 和一個 secret。**

```sh
TASK_ID=$(uuidgen | tr 'A-Z' 'a-z')     # 36 個字元，小寫
SECRET=$(openssl rand -hex 32)          # 64 個小寫 hex
```

secret 由你放在 POST body 交給 daemon，再由 daemon 打進 child 的那一行交給 child。它不在
`task.json` 裡，也不在派工的回應裡，之後你也用不到它。（唯一會帶 secret 的回應是 respawn：它回的是副本的新
secret。）

**2. 讀 inventory**（§3），拿到 `generation` 和 `task_root`。

**3. 寫 `<task_root>/<TASK_ID>/task.json`。** daemon 從這個檔案讀 brief，不是從 request 讀。收件時它先
驗證，再用收下的內容重寫 `task.json`，並從同一筆紀錄寫出 child 的 `CHILD.md`——標題、instructions、
claims、deliverables、kind 和 timeout 都在裡面——所以 child 讀到的 task 就是通過驗證的那一份，它不必再讀
`task.json`。

| 欄位 | 規則 |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | 同一個 id |
| `assistant` | `claude` 或 `codex` |
| `project_dir` | 一個已經存在的目錄的絕對路徑 |
| `title` | 顯示在畫面上；超過 200 字元會被截掉 |
| `instructions` | 必填，最多 16 KiB。內容必須自己就講得清楚：除此之外 child 什麼都不知道 |
| `claims` | **必填**：最多 32 個 child 可以寫入的相對路徑。`[]` 表示它什麼都不寫，派工會帶一個警告（`claims_missing`） |
| `isolation` | `none`（預設），或 `worktree`：在自己的 branch 上開一份私有 checkout |
| `permission_mode` | `ask`、`edits` 或 `full` |
| `timeout_minutes` | 1–240，預設 30 |
| `kind`、`deliverables`、`model` | 選填；`model` 只能用 `[a-z0-9._-]`，最多 64 字元 |
| `work_id` | 選填，這個 task 所服務的看板項目的 UUID |
| `auto_compact_window` | 選填，只限 Claude：child 在 context 到多少 token（50000–1000000）時壓縮，或 `null` 表示不壓縮。不寫就跟著這台機器的 `claude_auto_compact_window`，使用者沒設就是關閉。用來比較兩次執行，不是每份 brief 都要寫：壓縮可能漏掉細節 |
| `root` | **必填**：`{"session_id": "<your conversation id>", "assistant": "claude"\|"codex", "label": "…"}` |

**`root.session_id` 是你的 conversation id，絕對不是 terminal id。** Claude Code 把它 export 成
`CLAUDE_CODE_SESSION_ID`，Codex 則是 `CODEX_THREAD_ID`。daemon 靠它把 child 歸到你底下，並在 child
結束時通知你。要確認它指的就是這個分頁：`GET /v1/orchestrator/whoami?conversation_id=<id>` 會回
`terminal_id`。

**4. 派工**，帶 orchestrator token：

```
POST /v1/orchestrator/tasks
{"task_id": "…", "secret": "…", "inventory_generation": "…"}
```

body 從 stdin 送（`jq -n … | curl --data-binary @- -H 'Content-Type: application/json' …`），secret
才不會出現在 argv 裡。
回應是 `{ok, task, warnings?}`。要讀 `warnings`：`claims_overlap`、`claims_missing`、
`claims_ignored_for_worktree`、`dirty_worktree_base`。同一個 id 再 POST 一次，會回之前存下的 task 並帶
`replayed: true`，所以重試是安全的。

分頁開不起來時仍然回 200，只是 `task.state: "spawn_failed"`。
`POST /v1/orchestrator/tasks/<id>/respawn`（orchestrator token）會用新的 secret 開一份副本，每個原始
task 最多兩次。

**你會遇到的拒絕**，照檢查的順序排：

| 狀態 | Code | 怎麼處理 |
|---|---|---|
| 422 | `bad_task` | 訊息會點出是哪個欄位。「No readable task.json under …」也是這一種——檢查 `task_root` |
| 422 | `claims_required` | 補上 `claims` |
| 422 | `root_session_required`、`root_assistant_required` | 補上 `root.session_id` 和 `root.assistant` |
| 422 | `detached_route_required` | 你送了 `root.poll_only`；那是 detached automation（§6） |
| **409** | **`stale_inventory`** | 你的 `generation` 沒帶或過期了。錯誤裡附著目前完整的 inventory：讀它、重新判斷，再用它的 `generation` 重送 |
| 409 | `graph_*` | task-graph 的准入規則（`graph` 欄位） |
| 409 | `no_child_capability` | 這個平台開不了 child；`missing` 會說缺什麼 |
| 429 | `rate_limited` | 十分鐘內派工次數太多 |
| 422 / 409 | `root_unresolved`、`conversation_ambiguous` | 你的 conversation id 對不到任何活著的 session，或對到不只一個。把它修好；不要改走 detached |
| 429 | `over_capacity` | 你的 child 名額（預設 5 個）或整台機器的名額滿了；看 `retry_after` |
| 409 | `workspace_busy` | 另一個 root 的 claims 跟你重疊；錯誤會點出擋住你的那個 task |
| 409 | `worktree_unavailable` | 私有 checkout 建不起來 |
| 429 | `terminal_busy` | 所有 terminal 寫入通道都在忙；`retry_after: 5` |

## 5. 執行中，以及結束時

child 會用 `clawdline task accept` 簽收 briefing（它會送 `/accepted`，連不到時留下 `accepted.json`），
計畫改變時可以送一則進度說明（`/progress`），最多可以推五則通知（`/notify`），最後寫好 `result.json`、
執行 `clawdline task finish` 收尾。這些路由你不用呼叫。

- `GET /v1/orchestrator/tasks/<id>`——單一 task 和它的狀態。`GET /v1/orchestrator/tasks` 列出全部
  （`?state=`、`?limit=` 最多 500）。
- **child 結束時，daemon 會在你的輸入框打一行 `<clawdline-notice>`。** 它的 `body` 只有一句：哪個 task、
  怎麼結束、只屬於這次交付的事實（stalled、claims 已釋放、它的 branch、幾個 leftover），以及要執行的兩個
  指令——先 `clawdline task show <id>`，再 `clawdline task ack <id> <notice_id>`。JSON 仍帶著 `state`、
  `result_path`、`outstanding`、`leftovers`、`notice_id` 和 `ack_path`。它照 5→300 秒的階梯重試，一共八次，直到你 ACK 為止——而且你正在顯示選單時，它絕不
  打字。選單不會用掉那八次：那一行會等，最多等 12 小時，選單一消失就打進去：

  ```
  POST /v1/orchestrator/tasks/<id>/completion/ack   {"notice_id": "…"}
  ```

  `clawdline task ack <id> <notice_id>` 會送出它，並印一行結果。第二次 ACK 會回 `changed: false`。還沒 ACK 的通知列在 `GET /v1/orchestrator/completions`；
  `POST /v1/orchestrator/completions/reconcile` 會把它們重新排上。已經放棄的通知，會在你的 session 下一次
  閒置時再打一次。
- **就算你沒看到那一行，也會知道。** 你每個 turn 邊界都會讀的
  `GET /v1/work/v2/agent/session-todos/<conversation id>` 會列出 `unacknowledged_completions`：每一個已經
  結束、你還沒 ACK 的 child，附 `task_id`、`title`、`state`、`kind`、`result_path`、`notice_id` 和 `ack_path`，不管它的
  通知還在重送還是已經放棄。`clawdline session report` 在收據之後也會印出來。每一筆都先執行
  `clawdline task show <id>`、整合，然後 ACK；ACK 之後兩邊都不會再列。`task show` 會完整印出 summary，
  省略的部分只給數量；`--json` 是 daemon 的完整回應，包括 symbols 與 artifacts。只有不夠用時才去讀
  `result.json` 本身。
- **交付裡列了 leftovers**（child 說它沒做的事），本身不會觸發任何事。`task show` 會列出它們的標題。要把
  其中一項提給使用者，送
  `POST /v1/orchestrator/proposals {"session_id":"<yours>","task_id":"<id>","leftover":"<its title>"}`；
  使用者會回答 track、later（Backlog）或 no，在他回答之前，什麼都不會進到他的 board。
- **child 收到 briefing 就停住，會先被推一下，再回報給你。** briefing 已經打進去、child 卻還沒簽收，而且
  它的畫面連續 5 分鐘都是閒置（提示字元在、輸入框是空的、沒有選單、沒有正在跑的那一行），daemon 會在它的
  分頁打一行字，指出它的 `CHILD.md`（絕不再打一次 secret）。再過 5 分鐘仍沒簽收、仍閒置，task 就以
  `spawn_failed` 結束，verdict 寫明它 stalled，你收到的通知 `kind` 是 `task_stalled`，不是 `task_finished`。
  respawn 它（`POST /v1/orchestrator/tasks/<id>/respawn`）或重新派一次，然後 ACK。正在工作、正在顯示
  選單、或已經簽收的 child，絕不會被打字。
- **沒有取消路由**。task 只會以完成、失敗或逾時結束。
- **child 結束，不等於程式碼已經 landing。** 在你整合之前，它的成果還放在共用的 working tree 或它自己的
  branch 上。

## 6. Landing，以及另外三種工作

帶著 claims 的 child 一回來，**就記下 landing 義務**：

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "note": "…"}
```

- 只收這幾個 key，外加 `delivery`（收下但不使用）；其他 key 一律拒絕。`pending` 和 `abandoned` 接受
  task secret 或 orchestrator token；`landed` 和 `nothing_to_land` 只接受 orchestrator token。
- **合併會自己記帳。** 已結束的 task 分支一旦合併進 target，broker 會在幾分鐘內用同一道 Git 查證
  自己記成 `landed`，commit 是 target 當下的 head。紀錄上沒有 target 時，只有主 checkout 的 branch
  是唯一含有這份交付的 branch 才會自己命名。cherry-pick、`incorporated` 與 `nothing_to_land` 仍由你記。
- **完成通知會寫出 task 結束當下 branch 的狀態**，每一種只要求一件事。*branch 上什麼都沒 commit*：landing
  要從那個 branch 證明，所以照現況永遠不可能記成 landed——趁 checkout 還在磁碟上，在它裡面、那個 branch
  上 commit；sweep 把 checkout 收走之後，只剩 `abandoned` 和 `nothing_to_land` 可記。*branch 上有
  commit*：把那個 branch 合併進 target，再用帶著它的 commit 記 landing。*讀不到*：不知道有沒有 commit——
  先去看 branch 再記。*寫進共用 checkout*：用把那份工作帶上 target 的 commit 記 landing，或記
  `abandoned`。
- `landed` 需要 `target` 和 `commit`，而且 daemon **會去 Git 裡查證**；查不過就回
  `409 unverified_landing` 並附上 `reason`（`target_unresolved`、`commit_unresolved`、
  `not_on_target`、`predates_dispatch`、`nothing_delivered`、`not_the_delivery`、…）。
- task 其實有寫入 repository 時，`nothing_to_land` 會被 `409 wrote_to_repository` 拒絕。
- 已經定案的 landing 不能再改：回 `409 invalid_transition`；要改成不同的值時回
  `409 landing_conflict`。

`clawdline landings`（`GET /v1/orchestrator/landings`）是整台機器上所有 pending 的 landing，每一筆都帶
`ownership.status`。`unknown` 不代表「沒人負責」：它的意思是證據讀不到。`503 landings_incomplete` 表示
有些列讀不到，而且不會拿一份比較短的清單來頂替。

**兩個 root 要 landing 到同一份 checkout 時**，先取得 landing lease（§11）。

另外三種工作各有自己的路由。選哪一條是邊界問題，不是細節：

| 種類 | 路由 | 是什麼 |
|---|---|---|
| **Handoff** | `POST /v1/orchestrator/handoffs` | 把一條既有的工作線連同完整狀態，交給一個新的 session |
| **Root assignment** | `POST /v1/orchestrator/root-assignments` | 為新功能開一個獨立負責的新 Root |
| **Detached automation** | `POST /v1/orchestrator/detached-tasks` | 沒人看著、也沒有回報對象的工作 |

**Handoff。** 先寫 `<state dir>/handoffs/<handoff_id>/handoff.md`（列表路由會回 `package_root`）。它應該
有三段：**REFERENCES**（接手者必須讀的所有東西）、**VERIFICATION**（接手者繼續之前，要從那些來源回答
的問題）和 **OPEN THREADS**（從哪裡接著做）。然後 POST，body 是封閉的（只能有這些 key）：

```
{"handoff_id": "<uuid>", "from_session": "<your conversation id>", "coordinator_plain_handoff": true,
 "project_dir": "/abs", "assistant": "claude"|"codex", "model": "…", "title": "…"}
```

接手者會被告知：讀那個檔案、逐一看過它的 references、回答它的 verification 問題，然後繼續做。對方接手時
你會收到一則 `handoff_receipt` 通知。拒絕：`bad_task`（`handoff.md` 不存在或是空的也算）、
`sender_not_found`、`sender_ambiguous`、`rate_limited`、`terminal_busy`，以及你持有整台機器的
coordinator 角色時的 `succession_required`——這個 daemon 沒有 succession（`501`），所以那個 session
沒辦法 handoff。

**Root assignment。** `Idempotency-Key` header 必須等於 `request_id`：

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

每個 assignment 欄位 1–8192 bytes，加起來最多 32 KiB。daemon 自己寫 brief、自己開 session。它**沒有
parent、secret、timeout、result，也沒有 landing**：它結束時不會通知任何人，因為它不向任何人負責。
拒絕：`bad_root_assignment`、`idempotency_mismatch`、`request_conflict`、`rate_limited`。絕對不要用
child、detached task 或 handoff 假裝成 root assignment。

**Detached automation。** 跟派工（§4）一樣——`task.json` 放在 `task_root` 底下，再送
`{"task_id", "secret", "inventory_generation"}`——但 brief 的 root 必須是
`{"session_id": null, "poll_only": true}`，否則會被 `detached_task_required` 拒絕。不會通知任何人；
自己 poll `GET /v1/orchestrator/tasks/<id>`，再讀 `result.json`。它永遠不是 Root，也不是功能的 owner。

### 安排未來工作

排程工作由 Clawdline Next 自己負責。不要使用退役 app、`cron` 或一串 detached task 代替。
用 `GET /v1/orchestrator/schedules` 讀清單；用 `GET /v1/orchestrator/schedules/<id>` 讀完整內容。
寫入需要的 `place_id` 由 `GET /v1/places` 取得。

只跑一次的排程（`on`）可直接使用 orchestrator token 建立。重複排程（`days`）是一份長期指示，必須帶著
使用者在這個 Session 裡明確給的指示：

1. 讀 `GET /v1/orchestrator/sessions/<conversation>/run`。它是使用者透過 Clawdline 把訊息送給這個
   Session 時簽發的近期 run。
2. 帶 `Idempotency-Key` 呼叫 `POST /v1/orchestrator/schedules`；一般 schedule body 之外，再加上該
   conversation 與 run：

```json
{"title":"早晨巡檢","at":"09:00","days":"daily","place_id":"<place id>",
 "assistant":"codex","instructions":"檢查昨夜錯誤，回報可執行的發現。",
 "session_id":"<conversation id>","via":{"run":"<run id>"}}
```

使用者明確要求變更時，同一份證據也能授權 `PATCH /v1/orchestrator/schedules/<id>`（送完整 schedule
body）與 `DELETE /v1/orchestrator/schedules/<id>`（以 JSON body 送出兩個證據欄位）。
`POST /v1/orchestrator/schedules/<id>/run` 會立即執行一次。回報成功前，要把建立或修改後的排程讀回來。

沒有 `via` 時，orchestrator token 仍只能操作一次性的 `on` 排程。捏造、過期或屬於其他 Session 的 run
分別回 `run_unknown`、`run_expired`、`run_other_session`；授權格式錯誤回
`invalid_user_authorization`。使用者若直接在 terminal 打字，就沒有 run：請他透過 Clawdline 送出這項
指示。絕對不要把一次 run 當成使用者沒有要求之工作的概括授權。這份證據讓代轉行為可以稽核；它不會把
整台機器共用的 orchestrator token 變成某個 Session 專屬的憑證。

排程啟動的 task 若是替某件「等待驗收」的事讀資料（側欄的「驗收」，見 docs/verifications.md），用它自己的
task secret 把讀數寫成那筆紀錄上的一則紀錄——不用 orchestrator token，它本來就不該拿著：

```sh
curl -sS -X POST "http://127.0.0.1:$PORT/v1/orchestrator/tasks/$TASK_ID/verification-note" \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: readout-$TASK_ID" -d '{"verification":"<record id>","text":"<讀數>"}'
```

只寫得進 `schedule_id` 正是啟動這個 task 的排程的那筆紀錄（否則回 `schedule_mismatch`），署名為
`task:<task id>`；不是排程啟動的 task 會被拒絕為 `not_scheduled`。紀錄 id 用 `clawdline verify list` 查。

## 7. 回報你自己完成的 turn

這一輪真的做完了——工作做完、驗證過、該 commit 的也 commit 了——就在最後回答之前，把這一步當成最後一個
動作：

```sh
clawdline session report --summary "One concrete sentence about what was delivered."
```

它會在你的 session 那一列畫一個勾：**已交付，等待驗收**。它比 landing 弱，也不代表有人 review 過。只有
在 daemon 判斷 session 是閒置時才會顯示——工作中、等待中、讀不到畫面，都會蓋過它——而且只在那個
terminal 還掛著同一個 conversation 時顯示。

- **只用在完成的 turn。** 做一半、只做了診斷、被擋住、正在反問使用者，都不要送。child 絕對不送
  （`409 child_session`）。
- 指令從 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID` 找出你的 conversation（都沒有就用
  `--conversation`），向 `GET /v1/orchestrator/whoami` 問出 terminal，再把 `{"summary"}` 送到
  `POST /v1/orchestrator/sessions/<terminal>/complete`。
- summary 長度 1–500 字元。每呼叫一次就是一張新的收據；以最新的為準。
- 回應裡還有 `open_todos`：這個 Session 已送出或已讀、但還沒完成的 direct to-do，由舊到新，最多
  20 筆（更多時 `open_todos_truncated` 為 true）。指令會在收據之後把它們印到 stderr，一行一個 id
  和文字。做完的每一筆都用 `clawdline todo done <id>` 勾掉。收據無論如何都會記錄，離開碼也不變；
  `open_todos_unknown: true` 表示讀不到，不代表沒有未完成的。
- 拒絕：`conversation_id_malformed`（不是小寫的 UUID）、`conversation_not_found`、
  `conversation_ambiguous`、`registry_stale`、`session_not_found`、`session_unbound`、
  `child_session`。被拒絕就照實回報；在聊天裡寫一句話不算收據。

## 8. 跟另一個 session 說話

**找到它。** `GET /v1/orchestrator/sessions` 是通訊錄：每個 session 的 `id`（也就是它的 terminal id）、
`label`、`assistant`、`cwd`、`state`、`work_state`，活著的 child 還有 `taskId`。

**送出。**

```sh
clawdline send --to <terminal id> "text"        # 或從 stdin 送文字
```

這就是 `POST /v1/orchestrator/messages`，帶 `{from_session, to_session, text}` 和一個
`Idempotency-Key`。daemon 會把它打進收件者的輸入框，外面包一層標明你是來源的 `<clawdline-message>`
envelope。

- `to_session` 是 **terminal id**：訊息跟著你指定的那個分頁走，不是跟著 conversation 走。
  `from_session` 是你的 terminal id 或 conversation id（指令會幫你填）。
- 只能送文字，最多 100,000 字元。沒有 `images` 欄位：送了也會被默默丟掉。
- `ok` 的意思是位元組送到了某個輸入框，不代表有人讀了。
- 可能要幾十秒：daemon 打字之前會先讀過整台機器的 session（2026-09-19 在一台 Mac 上量到每次 relay 約
  30 秒）。指令最多等兩分鐘，不要中途打斷——relay 在半路被切斷，可能已經打進去卻沒記下來，之後用同一把
  key 重送只會回 `409 request_in_progress`。
- 指令會先印出它的 `Idempotency-Key`。如果呼叫在半路失敗，用 `--key <that key>` 再執行一次：同一個 key
  加同一個 body 只會打一次。同一個 key 配上不同的 body，會得到 `409 idempotency_key_reused`。
- 拒絕：`source_not_found`、`target_not_found`、`same_session`、`target_busy`（收件者正在顯示選單；
  什麼都沒打進去）、`terminal_busy`、`delivery_failed`。

**給使用者看一張圖。** 不要貼本機路徑：在手機上點了什麼都打不開。

```
POST /v1/artifacts/images    {"images": [{"path": "/absolute/path.png"}]}
```

- 只接受 orchestrator token。一到六個本機檔案；每一個都必須是一般檔案，最多 12 MiB、每邊最多
  12,000 px。PNG、JPEG、GIF 直接讀；其他格式在 macOS 上會先交給 `sips` 轉。
- 回應會列出 `artifacts`，每一個都有一個 `marker`，例如 `<clawdline-image id="…">`。**把 marker 放進
  你的回覆裡**；console 會在 marker 的位置顯示圖片。圖片保留 24 小時。

## 9. 通知使用者

```sh
clawdline notify --title "At most 80 characters" --body "At most 500 characters"
```

也就是 `POST /v1/orchestrator/notify`。只用在使用者正在等的事情上：推播的價值就在於它很少出現。
`--session <terminal>` 讓使用者點通知時直接打開那個 session。

- `409 agent_notify_disabled`：使用者關掉了 agent 通知。不是你的錯；不要重試。
- `409 not_subscribed`：沒有任何裝置訂閱推播。
- `429 rate_limited`：整台機器每小時 30 則，跟所有 child 的通知共用。
- `502 push_failed`：push service 拒絕了；`sent` 和 `failed` 在錯誤裡。

## 10. 看板

看板有三種結構——看板項目、Backlog，以及每個 session 自己的待辦清單——而且**上面放什麼由人決定**。
只有在使用者透過 Clawdline 送來的訊息親口要求時，session 才自己建立看板項目；其他時候只能提案。
從不主動建卡片。

**TODO／待辦／土度跟看板項目一起講，指的就是那個項目的 steps。** 用 `--step` 放到項目上。**不要**
再用 `clawdline todo add` 寫一次。`clawdline todo add` 只用在使用者要你把一份清單記成這個 Session
自己的待辦、而且沒有看板項目的時候。

**使用者要你建立看板項目時。** 只有在他的訊息——透過 Clawdline 送來，所以有 run——明確要求時，
才由你自己建立：

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "第一步" --step "第二步" …   [--description-file f | description 從 stdin]
```

範例。使用者寫：「開一個看板項目整理 release notes，TODO：起草、檢查連結、發佈。」這就是一條指令，
別的都不做：

```
echo "下次 release 前把 release notes 整理好。" | \
  clawdline item add --project <place id> --kind feature --title "整理 release notes" \
  --step "起草" --step "檢查連結" --step "發佈"
```

- `item add` 會讀這個對話最新的 run（`GET /v1/orchestrator/sessions/<conversation>/run`），除非用
  `--run` 指定；送出前先印出 Idempotency-Key（用 `--key` 重送同一筆寫入），成功後印出建立的項目和
  每個 step 的 id。它就是 `POST /v1/work/v2/agent/items`，body 是 `{"session_id", "via": {"run"},
  "project_id", "kind", "title", "description", "deployment_policy"?, "steps"?: ["…"]}`。
- Feature 或 Issue 建好時**已經指派給你**，phase 是 `assigned`，並帶著 steps：依序是那些 `--step`，
  沒給的話，就是 description 裡兩列以上的頂層 Markdown 清單。不會有任何字打進你的 terminal——是你自己
  要的。照順序做，每一步確認完成後就勾掉（`clawdline item steps <item id>`、`clawdline item step-done
  <item id> <step id>`），並像任何已指派項目一樣用 `clawdline item phase` 推進 phase（見下文）。Epic、Refactor、Plan 會以未指派狀態建在
  規劃區，不帶 steps（`planning_has_no_steps`）。
- 使用者會看到卡片上寫著「Session 依你 HH:MM 的訊息建立」，並引用他的原話。
- 拒絕，每一種都什麼都不寫：`run_unknown`（沒指名 run，或沒有這個 run）、`run_expired`（超過一天）、
  `run_other_session`（那是傳給別的 Session 的訊息）、`session_not_found`、`child_session`（child 用
  `result.json` 回報）、`project_not_found`、`project_mismatch`（可執行的項目必須在你工作的 Project
  裡）、`too_many_steps`（超過 128）、`run_items_exhausted`（一則訊息最多撐五個項目）。
- **沒有 run**——使用者是直接在 terminal 打字，所以 `item add` 回 `no_run` 或 `run_unknown`：改走
  提案（見下文），並告訴使用者到看板的 Agent 提案裡接受它。

絕對不要主動建立看板項目，也不要一次建好幾個來規劃推測性的工作。

**提議一個看板項目。** 看板上的 **Agent 提案**佇列只由這一條路由餵進去：

```
POST /v1/work/v2/agent/proposals     (Idempotency-Key required)
{"project_id": "<place id>", "kind": "feature" | "issue" | "epic" | "refactor" | "plan",
 "title": "…", "description": "…", "reason": "why this is worth doing",
 "suggested_acceptance": "what would count as done", "session_id": "<your conversation id>",
 "source_work_id": "<uuid>" or "source_todo_id": "<uuid>"}
```

- `project_id` 是 `GET /v1/places` 某一列的 `id`。
- 一定要有一個來源，而且必須是你自己的：這個 Session 負責的看板項目，或這個 Session 自己的待辦
  （`proposal_source_required`、`proposal_source_invalid`）。從使用者要求而來的提案，要引用它來自
  的那筆待辦——所以路徑是：使用者提出要求、你用 `clawdline todo add` 記下來（見下文）、再以那筆待辦
  的 id 提案。
- **要提一個帶 TODO 清單的項目**，就在 `description` 裡寫兩列以上的頂層 Markdown 清單。使用者接受並
  指派這個項目時，每一列都會變成它的一個 `steps`（見下文）。
- `201` 會回傳這筆待決提案。使用者在看板的 Agent 提案佇列裡接受、編輯或拒絕；在那之前它不會變成
  看板項目。拒絕：`invalid_proposal`、`proposal_too_large`、`project_not_found`、`proposals_full`。

**較舊的提案路由。** `POST /v1/orchestrator/proposals`（在對話裡問過之後再呼叫 `…/<id>/asked`）
仍然有效：child 用自己的 task secret 和 `task_id` 在這裡登記 leftover，root 的工作線提案也在這裡拿到
「現在問還是先擱著」的 `instructions`。它的列出現在舊的「待確認」區，**不在** v2 看板的 Agent 提案
佇列裡，所以這不是把項目放到使用者看板上的方法。

**請使用者做決定。**

```
POST /v1/orchestrator/decisions     (Idempotency-Key required)
{"session_id": "…", "project": "<project>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

二到四個選項；`default` 必須是其中之一，也就是沒人回答時會採用的選項（7 天後，除非 `due_in_minutes`
指定 60–10080）。只有 `blocking` 的 decision 會推播。用 `GET /v1/orchestrator/decisions/<id>` 讀答案。

**回答的是使用者；Session 只能代轉他說的話。** proposal、decision 和看板項目都在 `/v1/work/…`
底下回答。Session 要寫進去，必須指名帶著使用者那句話的 run：`"via": {"run": "<id>"}`，沒帶就被
拒絕（`403 session_cannot_decide`）。用 `GET /v1/orchestrator/sessions/<conversation>/run` 讀最新的 run；
捏造、過期、屬於其他 Session 或早於問題的 run 都會具名拒絕。使用者直接在 terminal 打的字沒有 run，
請他透過 Clawdline 回答，或由他自己在 console 操作。

**你的待辦清單。** `GET /v1/orchestrator/sessions/<conversation id>/todos`——用 conversation id 指名，
不是 terminal id（否則回 `409 session_id_is_terminal`）。這些項目由 broker 根據 task 的事實開啟和關閉；
你不需要寫任何東西。

每個 turn 的邊界、宣告自己閒置之前，也要讀
`GET /v1/work/v2/agent/session-todos/<conversation id>`。其中的 `assigned_items` 是使用者交給這個
Session 的看板項目，`recent_items` 是這個 Session 最近完成的項目，`direct_todos` 是快速交辦，`unacknowledged_completions` 是你還沒 ACK 就已經結束的 child（第 5 節）。這條
pull 路徑讓工作中收到的分派先等著，不會打斷目前的 turn。完成目前的 turn 之後，把 assigned item 當成
下一件自己負責的工作，並從 `GET /v1/work/v2/items/<id>` 讀取完整內容。

**使用者送來的待辦。** 訊息最後一行如果是
`(Clawdline to-do <id>. When it is done: clawdline todo done <id>)`，那就是使用者從 Clawdline
送來的一筆 `direct_todos`；那一行上面的文字才是交辦內容。把事情做完，驗證確實完成之後，**在回報這個
turn 之前**先執行 `clawdline todo done <id>`（用那一行的 id）——否則工作做完了，那一列在使用者的清單上
還是開著。還沒做完的就讓它開著。`clawdline session report` 會在 stderr 列出所有送給這個 Session、
還沒勾掉的待辦（§7）。

**使用者要求時，寫你自己的待辦。** 只有在使用者明確要求這個 Session 把工作記成 Clawdline 待辦——
或交給它一份多項清單並說要在那裡追蹤——才寫進這個 Session 自己的清單：

```
clawdline todo add "first item" "second item" …     (or one item per non-empty stdin line)
clawdline todo list
clawdline todo done <to-do id>
```

`todo add` 就是 `POST /v1/work/v2/agent/session-todos/<conversation id>`，body 是
`{"todos": [{"text": "…"}, …]}`，並先印出它使用的 Idempotency-Key（用 `--key` 重送同一筆寫入）。
一次呼叫帶 1–20 列、每列最多 8 KiB，整個請求在 96 KiB 以內，全部寫入或全部不寫；會讓這個 Session
的未完成待辦超過 500 筆的清單會整批被拒（`direct_todos_full`）。成功回 `201` 和依原順序排列的各列。
conversation 必須是這個 daemon 認得的 live Session（`conversation_id_malformed`、
`session_not_found`）；Clawdline child 會被拒（`child_session`），它照樣用 `result.json` 回報。

絕對不要自己主動這樣做，也不要拿來規劃推測性的工作。每一列在確認做完之後才用
`clawdline todo done <id>` 完成。使用者會看到這些列標示為 Session 建立，而且只有使用者能傳送或刪除
它們。它們不是看板項目，也不會出現在看板上。待辦是目前這個 Session 裡的一串雜事；看板項目是使用者要在
看板上追蹤的工作——他要的是這個時，用 `clawdline item add`（見上文），清單以項目的 `--step` 放進去，
絕不同時再寫成待辦。

如果使用者的意思很清楚：你剛完成的那個項目其實還沒做完，就由你自己修正看板；不要讓它繼續留在
「最近完成」、另開替代項目，或要求使用者替你重開。先重讀項目取得目前版本，再呼叫：

```
POST /v1/work/v2/agent/items/<id>/reopen     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "reason": "仍未完成的具體行為或驗收主張"}
```

只有在對方明確指向你剛完成的項目時才使用。這條路由只接受最後由同一個 Session 釋放 assignment 的
`done` 項目；它不能推翻使用者的取消，也不能拿走另一個 Session 的完成項目。成功時會保留先前證據、以
新 cycle 回到 `implementing`、恢復這個 Session 為 owner，並把原因寫入不可變的項目歷史。原因最多
8 KiB。語意含糊的追問不構成修改看板的授權。

負責項目的 Agent 要等待使用者動作時，使用 machine-authenticated route：

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "user_action": "請使用者完成的單一具體動作"}
```

`user_action` 最多 8 KiB，而且只屬於 `waiting_user`；缺少具體動作或在其他 condition 寫入都會被具名拒絕。
不再等待時，用同一路由把 `condition` 設成空字串，daemon 會一起清掉 `user_action`，避免看板留下過期要求。

**推進 phase。** 負責項目的 Session 自己把項目推過執行階段；別人不會替你推，turn receipt 或清掉
condition 也不會。phase 不是 `…/edit` 的欄位（`phase_not_editable`）。每一步所說的事真的發生了，
才執行那一步：

```
clawdline item phase <item id> implementing                  # 開始動手時
clawdline item phase <item id> verifying                     # 改動已經在了，開始檢查
clawdline item phase <item id> merging --verification "跑了什麼、結果是什麼"
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin --landing-project <place id>
clawdline item phase <item id> done --deployment "上線了什麼、在哪裡、哪個版本"
clawdline item phase <item id> done --no-deployment-reason "為什麼不需要部署"
```

指令會先讀項目的 version，印出 Idempotency-Key（用 `--key` 重送同一筆寫入），成功後印出項目。它就是

```
POST /v1/work/v2/agent/items/<id>/phase     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "next": "<phase>",
 "verification"?: "…", "landing"?: {"commit", "target", "remote", "project"?},
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 一次走一步：`assigned → implementing → verifying → merging → deploying → done`。`verifying` 可以
  退回 `implementing`；`merging` 可以退回 `implementing` 或 `verifying`。不能跳過任何一步，`done`
  只能從 `deploying` 進入。
- `merging` 要帶 `verification`。`deploying` 要有 landing：這個項目的 broker child 已經 land，或用
  `landing` 指名一個 commit，daemon 要在 Project 的本機 `target` branch 和
  `refs/remotes/<remote>/<target>` 上都找得到它——先 push。工作落在別的 repository 時（後端項目、
  改動卻是前端的 commit），用 `landing.project`（`--landing-project`）指名那個 Project 在
  `GET /v1/places` 的 id，daemon 就改在那裡找 commit，收據會記下是哪個 repository。`done` 要帶 `deployment` 或
  `no_deployment_reason`，由項目的 `deployment_policy` 決定（`required` 只收 `deployment`，
  `not_required` 只收 `no_deployment_reason`，`agent_decides` 兩者皆可）。所有 step 都要先完成。
- `done` 會釋放你的 assignment，項目移到這個 Session 的「最近完成」。需要結案報告時（見下文）
  要在這之前加上。
- 拒絕：`invalid_transition`（不是下一個 phase，或缺它要的證據）、`steps_incomplete`、
  `not_item_owner`、`item_unassigned`、`item_terminal`（要由人重開）、`evidence_unknown`、
  `direct_landing_not_applicable`、`invalid_landing_evidence`、`landing_project_not_found`、
  `landing_commit_unresolved`、
  `landing_target_unresolved`、`landing_not_on_target`、`landing_remote_unresolved`、
  `landing_not_published`，以及 `version_conflict`：重讀後再送。

已指派的項目可能帶有 `steps`。成功指派時，description 裡兩個以上的頂層 Markdown 列點可以自動成為
steps，用 `clawdline item add` 建立的項目則帶著它的 `--step`；每一列都是父項目裡的 TODO，不是另一張看板項目。確認完成一列後，以 machine authentication 和
Idempotency-Key 呼叫 `POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete`，body 是
`{"expected_version": <item version>, "session_id": "<你的 conversation id>"}`。版本衝突時先重讀。
只要還有任何 step 未完成，`done` 轉換就會以 `steps_incomplete` 拒絕；父項目的 phase 前進不會偷偷把
step 勾成完成。

如果 issue 或 incident 必須經過深入調查，才找出 root cause（根因），或必須排除多個看似合理的解法才
確認真正修正，請在把項目推進 `done` 之前加入一份給使用者閱讀的結案報告。直接觀察就能確認的直觀修正
不需要報告。使用帶 machine authentication 與 Idempotency-Key 的文件路由：

```
POST /v1/work/v2/agent/items/<id>/documents     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "role": "completion_report", "title": "結案報告",
 "body": "發生了什麼、root cause、修改內容、驗證方式，以及仍存在的邊界"}
```

body 是 Markdown，最多 64 KiB。寫給提出問題的人讀，不要貼成原始 debug log，也不要放入私密資料。只有
尚未結案的 active owner 能加入；版本衝突時先重讀。結案報告是具名敘述，不取代驗證、landing 或部署
證據；有報告時，它會留在已關閉的看板項目，並可從 Session 的「最近完成」列直接打開。

`/v1/board` 是 Swift app 的舊卡片，唯讀。landing 是 broker 的事實：項目永遠不會被人手動標成已 landing
（`422 landing_is_broker_fact`）。

## 11. 協調

**整台機器的 coordinator（"Clawdfather"）。** `GET /v1/orchestrator/coordinator` 查看這個角色；
`/coordinator/bearings` 是整台機器的概況（進行中的 task、pending 的 landing、還開著的 wait、dead letter、
被持有的 lease，以及哪些是 `unknown`）。帶 `{"session_id": "<conversation id>"}` 呼叫
`POST …/coordinator/register` 就取得這個角色；綁定的 session 離線之後，`POST …/coordinator/rebind` 會把
角色移走（`expected_coordinator_id`、`expected_generation`）。Succession 回
`501 succession_unavailable`。

**檔案 wait。** wait 的意思是「owner 處理完這些路徑時告訴我」。它是一筆紀錄加一則訊息，不是鎖，也不是
檔案監看。

- `POST /v1/orchestrator/waits`——
  `{"repository", "paths", "owner_session_id", "waiter_session_id", "reason", "release_condition"}`
  （session id 都是 conversation id）。owner 會在自己的輸入框被告知一次。
- 由 owner 結束它：`POST /v1/orchestrator/waits/<id>/release`，帶
  `{"owner_session_id", "commit"?, "note"?}`；每個 waiter 都會被告知。沒有任何計時器會自動 release
  wait。
- waiter 自己退出：`POST …/waits/<id>/cancel`，帶 `{"waiter_session_id"}`。
- `409 owner_busy` 和 `502 request_delivery_failed` 表示 **wait 已經記下來了**，只是還沒通知到 owner。
  `502 release_incomplete` 會列出還有誰沒通知到：再送一次 release。

**Lease。** 兩種資源：`heavy_compile`（整台機器唯一的重度編譯名額）和 `landing`（每份 checkout 一個）。

- `POST /v1/orchestrator/leases`——
  `{"request_id": "<uuid>", "resource", "checkout" (landing only), "holder", "reason", "session_id", "pid"}`。
  回 `granted`，或回 `queued` 並附上 `position` 和 `retry_after_seconds`。排隊中的請求用同一個
  `request_id` 再問一次。
- `POST …/leases/renew | release | cancel`，帶 `{"request_id", "resource", "checkout"}`。持有者用
  `/renew` 續約（用自己的 `request_id` 再打一次 `POST /v1/orchestrator/leases` 也算續約）。
- 60 秒內要續約，否則 lease 會被視為已經不在了。`409 lease_lost` 就表示它真的不在了。排隊的 waiter 到
  32 個時回 `429 queue_full`。

**Graph**（`GET /v1/orchestrator/graphs`）是從已派出 task 的 `graph` 欄位算出來的唯讀檢視。
**Reclaim**（`/v1/orchestrator/reclaim`）會清掉已經結束的 checkout；POST 預設只是 dry run，除非 body 寫明
`{"dry_run": false}`。

## 12. 被拒絕時怎麼辦

- 看 `error.code`（扁平形狀則是 `error`）決定怎麼處理。message 是寫給人看的。
- 有 `retry_after` 就是容量方面的回答：等那麼久，再送同一個 request。
- `409 stale_write`、`503 orchestrator_store_busy`：store 當下在忙；原封不動再送一次是安全的。
- 任何地方出現 `unknown`——ownership、存活狀態、來源——都表示 daemon 讀不到。它不等於「不存在」，也不能
  據此刪掉任何東西，或宣告任何東西已經死了。
- 你預期存在的路由如果回 `404 not_found` 或 `501`，就是這個 daemon 沒有它。直接講明；不要退回去用
  Swift app 的路由，也不要改用 provider 原生的 subagent 代替。
