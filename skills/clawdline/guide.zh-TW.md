# Clawdline 使用指南

這是 `clawdline guide` 的繁體中文版。兩者內容有出入時，以英文版（`clawdline guide`）為準。

給跑在裝了 **Clawdline Next** 的機器上的助理 session 看，Claude Code 或 Codex 都一樣。這份指南只寫
這個 daemon 今天有提供的東西，其他一概不寫：下面每一條路由都是印出這份指南的那個 build 註冊的，
少了一條，就會有測試失敗。要看請重新執行 `clawdline guide zh-TW`（英文版是 `clawdline guide`），
不要相信手上的副本。`clawdline guide zh-TW` 只印每個 session 都需要的核心，並列出其他部分；做到某一
部分的工作時再印那一部分（`clawdline guide zh-TW dispatch`），要全文用 `clawdline guide zh-TW all`。
印出的內容（核心也一樣）第一行是 `guide-version: <sha256>`；同一個指令加上 `--since <hash>` 再執行，
內容沒變時只印一行 `unchanged <hash>`。`clawdline guide zh-TW refused <code>` 印出說明該拒絕碼的
部分；沒有任何部分提到它時，exit 1，stdout 不印任何東西。

這份指南裡，項目清單的一列叫 **step**，task 可以改的路徑叫它的 **writes**，項目歸誰負責叫
**指派**，這份指南可單獨印出的一段叫 **part**。

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

如果訊息寫著 *"You are an independently owned Clawdline Feature Root …"*，或有看板項目指派給你，接著印出
`clawdline guide zh-TW feature-root`：它是從讀項目到 `done` 的完整一般流程，比較少見的工作也會指出該印哪個部分。

## 2. 連上 daemon

**有指令可以用，就用指令，不要自己組 curl。** 指令在自己的 process 裡讀憑證，所以憑證不會出現在命令列、
`ps`、指令輸出，也不會出現在你的 transcript 裡。自己組的 curl 沒帶這份憑證，會回 `401 unauthorized`
（"This needs a paired device."）：缺的是憑證，不是權限。改用指令。

| 指令 | 做什麼 |
|---|---|
| `clawdline guide [zh-TW]` | 這份指南。不需要 daemon |
| `clawdline session report --summary "…"` | 記錄你已經完成的 turn（§7） |
| `clawdline dispatch --title "…" --claims a,b < brief.md` | 派出一個 owned child（§4） |
| `clawdline item steps\|name\|phase\|step-add\|step-done\|doc\|acceptance <item id> …` | 讀取並推進你負責的看板項目（`clawdline guide zh-TW feature-root`、§10） |
| `clawdline todo add\|list\|done` | 這個 Session 自己的待辦，只在使用者要求時（§10） |
| `clawdline heavy -- <command…>` | 在機器唯一的編譯槽裡跑 build 或測試（§11） |
| `clawdline send --to <terminal> "…"` | 把一則訊息轉進另一個 session（§8） |
| `clawdline notify --title "…" --body "…"` | 推播一則通知給使用者（§9） |
| `clawdline note create --body-file <JSON> [--target <terminal>]` | 在 Session 上方留下需要人處理的便條（§9a） |
| `clawdline assistants` | 每個助理的帳號還剩多少額度 |
| `clawdline landings` | 這台機器上所有還欠著的 landing |
| `clawdline usage [--session <c> \| --task <id> \| --item <id>]` | 一個 session、child task 或 Board item 花了多少 token，依類別分；預設是你自己 |
| `clawdline cloud pair [--offer <code>]` | 把一個 Cloud 瀏覽器與這台機器配對 |
| `clawdline task show [--json] <task id>` | 精簡地看一個 child task：狀態、verdict、summary、leftover 標題、驗證、landing、checkout（§5） |
| `clawdline task ack <task id> <notice id>` | ACK 一則 child 完成通知（§5） |
| `clawdline task accept <task dir>` | child 簽收 briefing。root 永遠不執行它 |
| `clawdline task finish <task dir>` | child 的完成動作。root 永遠不執行它 |
| `clawdline webhook fire [--url-file <path>] [--deliver-within 60s] [--timeout 60m] [--no-wait]` | 從任何一台機器透過 Cloud webhook 啟動一個排程，並等它的結果；exit code 說明它怎麼結束（「排程」一節）。不需要 daemon |

上面那些 orchestration 指令（`webhook fire` 除外）成功時會印出 daemon 回的 JSON；被拒絕時印出
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

**用 curl 呼叫 orchestrator 路由。** 從 `<state dir>/orchestrator-token` 讀取憑證，放進
`X-Clawdline-Orchestrator` header。避免把憑證放在指令參數：使用
`DIR="${CLAWDLINE_NEXT_DIR:-$HOME/.config/clawdline-next}"`，再使用
`-H @<(printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$DIR/orchestrator-token")")`。
使用 `curl --fail-with-body`；帶 JSON body 的 POST 還要加
`-H 'Content-Type: application/json'`（否則回 `415 unsupported_media_type`）。

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

### 設定 Project 在 Clawdline 裡的顯示

使用者要你把目前工作的 Project 在 Clawdline 裡整理清楚時，讀這一段。完成不是「有幾個檔案」；完成是
這個 Project 有正確的名稱與圖示、長時間工作能回報進度，而且 Clawdline 能看見開發伺服器、卻不會替它
啟動伺服器。

先讀這個 repository 的指示、README、deploy/build scripts 與現有 process manager 設定。沿用 Project
已經使用的指令。不要只為 Clawdline 另加一條 deploy 路徑或另一個 supervisor；除非使用者明確要求實際
操作，否則不要啟動、停止、重啟或部署任何東西。顯示設定和真的部署是兩件不同的工作。

依序檢查以下四項；真的不適用時才略過：

1. **Project。** 執行 `clawdline project list`。清單沒有這個 checkout 時，用
   `clawdline project add <absolute-root>` 加入 repository root，再列一次。這只記錄能從哪裡開 Session，
   不會改 repository。
2. **名稱與圖示。** 沒有設定時，Clawdline 會從路徑產生一個穩定圖示。使用者想指定名稱或像素圖示時，
   保留 `~/.claude/project-icons.json` 裡的其他每一列，只修改這個 Project 最長相符路徑的那一列。格式在
   Clawdline repository 的 `docs/project-status.md`；Projects 頁也能複製已解析好的圖示，不必手改 JSON。
   全域使用者檔案不是 repository 內容；若原要求沒有授權修改，先把準備寫入的完整 entry 給使用者看。
3. **Deploy 與長時間工作。** Clawdline 只讀狀態收據，從不執行 deploy。GitHub repository 的 deploy 收據
   是 `~/.claude/statusline-cache/ghrun-<owner>-<repo>.json`，owner 與 repo 取自 `origin`。已經知道 run
   狀態的 producer 要用 atomic write 寫入 `state`（`running`、`ok`、`fail` 或 `none`）、`label`、`url`、
   `started_at`，以及實測過的 `typical_seconds`。本機 build、test、import 或 deploy command 可在 helper
   存在時用 `clawdline-progress run --label <label> -- <command>` 包住，否則依
   `docs/project-status.md` 實作 `run-<path>.json` contract。時間沒有量過就省略，不要猜；producer 被中止
   時也不能永遠留下 running。
4. **開發伺服器。** 在最近的 deployable root 新增或更新 `.devstack.json`。目前 Go daemon 只讀
   `processes` 並探測它們的 loopback `port`，或開啟 `url`；它不會從 browser 執行 `status`、`up`、
   `down`、`restart` 或 `logs`。優先寫最小而正確的 Tier 0，例如：

   ```json
   {"version":1,"name":"myapp","processes":[{"name":"api","port":8002},{"name":"web","port":3001}]}
   ```

   沒有固定 port 或 URL 的 process 不要猜進去。驗證開發環境宣告時，不要探測或重啟 production。

每一層分開驗證：`clawdline project list` 要列出 checkout；每個 JSON 都能 parse；改過的 script 要通過
repository 自己的 tests；`GET /v1/devstacks` 要把宣告的 server 顯示成 running、stopped 或 unknown，而
不是靜默省略；Project 裡的 Session 要能看到新鮮的 progress/deploy 收據。讀不到、格式錯誤或過期時，說清楚
是哪一項並保留 unknown，絕不能把「沒有答案」回報成成功。最後列出完成的設定、刻意判定不適用的項目，
以及 git 以外被修改的使用者檔案。

## 2a. Feature Root 的一般流程

一般的 Feature Root——負責一張看板項目的 Session——要執行的就是下面這些，依序。每一步都是指令：指令
帶著憑證，自己組 curl 打同一條路由會被拒絕。比較少見的工作只差一個 `clawdline guide <part>`；指引在
最後。

**1. 讀項目。** `clawdline item steps <item id>` 印出它的種類、phase、驗收標準、本輪擷取的 gate、
驗收版本（`acceptance vN`）、Feature 另有使用者的「需要獨立審查」勾選，以及 steps。你就從這份紀錄開始工作。

**2. 替 Session 命名**（如果它是為這個項目開的）：讀完 objective 和 scope 之後執行一次
`clawdline item name <item id> "<task name>"`。改的是 Session 名稱，不是項目標題。

**3. 開始實作之前。**

- 本輪擷取了 planning 但沒有驗收標準：用 `clawdline item acceptance <item id> --body-file acceptance.md`
  寫下可觀察的標準。
- 勾了「需要獨立審查」，或它是 Epic：先走 `clawdline guide epic` 裡的審查計畫流程。沒勾：不寫計畫、
  不派審查 child。
- 工作分好幾段又沒有 steps：`clawdline item step-add <item id> "first" "second" …`（二到八個能各自
  驗證的 step；單一修改不需要）。
- 然後 `clawdline item phase <item id> implementing`。

**4. 委派，調查也算。** 實作交給 child，調查也一樣——找原因，或是讀到足以給使用者選項。你留下的是
結論，不是整份檔案內容；整合、合併與 landing 仍是你的。

```sh
clawdline dispatch --title "…" --claims a.go,b.go --isolation worktree --work-id <item id> < brief.md
```

- `--work-id` 把 child 綁到項目上，它的 landing 就算是項目的。一個 child 做好幾張項目時就重複這個
  旗標：第一張是 child 的工作線，landing 對每一張都算數。
- 標題是一行、最多 60 字元，說完成後什麼會不一樣。任何冒號（`:` 或 `：`）都會被拒，因為它把觀察和
  解釋接在一起；以「使用者」或 "the user" 當主詞、或用 code 格式的識別字開頭，也會被拒。每一種都回
  `bad_task`，並以 `title: …` 說是哪一種。
- brief 要自己就講得清楚。把你已經查證過的事實寫進去，每一條附上 `file:line` 或顯示它的指令，child
  就不必重新找一次。
- 調查或 Explore child 的 brief 還要寫明停止條件——回答了就結束任務的那個問題——以及 turn 上限。
- 只讀的工作用 `--claims ""`。所有旗標和拒絕代碼在 `clawdline guide zh-TW dispatch`。

**5. child 結束時**，你的輸入框會被打進一行 `<clawdline-notice>`。執行 `clawdline task show <task id>`，
整合交付，然後 `clawdline task ack <task id> <notice id>`。worktree child 的整合方式是**把它的 branch
merge** 進 target。**merge 會自己記下 landing**，幾分鐘內：不要手動送 landing。`clawdline landings`
列出還欠著的。用 `--claims ""` 派出、什麼都沒寫的 child，broker 會自己記 `nothing_to_land`。其他情況用
`clawdline task land <task id> <state>`（`clawdline guide landing`）。

**6. 結案報告**：找出原因需要深入調查時（直接觀察就確認的修正不需要）。在 `done` 之前加入：項目一旦
`done` 就沒有持有者，這時送報告會回 `409 not_item_owner`。

```sh
clawdline item doc <item id> --role completion_report --title "結案報告" --body-file report.md
```

寫給提出問題的人讀，用 Markdown，不放私密資料。

**7. 完成項目。** 每個 step 確認完成後用 `clawdline item step-done <item id> <step id>` 勾掉。child 的
branch merge 之後，一個指令就把項目從目前的 phase 走到 `done`；commit、target 和 remote 從已記錄的
landing 讀，你只要寫說明：

```sh
clawdline item finish <item id> --verification "what was run and what it showed" \
  --deployment "what went live, where, which version"      # 或 --no-deployment-reason "…"
```

也可以一次推進一個 phase：

```sh
clawdline item phase <item id> verifying
clawdline item phase <item id> merging --verification "what was run and what it showed"
clawdline item phase <item id> deploying        # a landed --work-id child is the evidence
clawdline item phase <item id> deploying --commit <sha> --target main --remote origin   # otherwise; push first
clawdline item phase <item id> done --deployment "what went live, where, which version"
clawdline item phase <item id> done --no-deployment-reason "why nothing needs deploying"
```

`done` 依項目的部署政策帶 `--deployment` 或 `--no-deployment-reason`。本輪擷取了 verification 時，
`verifying → merging` 需要 checker 的 PASS：進入 `verifying` 之前先讀 `clawdline guide zh-TW board` 的
「本輪擷取的規劃與驗證 gate」。

**8. 回報這個 turn**：最後一個動作是 `clawdline session report --summary "…"`（§7）。

**被拒絕時。** `version_conflict`：同一個指令再跑一次，它會重讀版本。`steps_incomplete`：還有 step
沒勾。其他代碼：先看 §12，再看涵蓋它的那個部分。

**比較少見的工作，各一個部分：** `clawdline guide zh-TW board`——提案、決策、待辦、重開已完成項目、
等待使用者、gate 與所有 phase 拒絕；`clawdline guide zh-TW epic`——計畫、計畫審查、Epic 的子項目、
persona；`clawdline guide zh-TW landing`——手動 landing、交接、Root 指派；
`clawdline guide zh-TW running`——卡住的 child、leftover、respawn。

## 3. 派工之前：先讀已經存在的東西

別的 session 可能已經在做你要做的事，而且從共用的 working tree 上看不出來：一份已經完成、放在還沒
合併的 branch 上的交付，不會出現在任何 `git status` 裡。先讀。

```
GET /v1/orchestrator/inventory?project=<absolute repo path>[&claims=a,b]
```

- 回 `generation`、`task_root` 和四個清單：`live`、`unlanded`、`droppable`、`unreadable`。每一列都帶著
  一個 daemon 會接受的 `do`。帶了 `claims` 時，每一筆 live 列都會標出它 `overlaps` 什麼。
- **派工一定要帶 `generation`**（§4）。它是由各列 sealed 欄位算出來的 16 個 hex 字元；只要有一列開始、
  結束或改了 writes，它就會變。
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
  [--persona <id>] [--work-id uuid …] [--label "…"] [--project-dir D] < brief.md     # 或 --instructions-file brief.md
```

它會產生 id 和 secret、讀 inventory 拿 `generation` 和 `task_root`、寫 `task.json`、送出 task；遇到
一次 `stale_inventory` 會重讀 inventory、再送一次。輸出是 `dispatched <id> <state> [worktree <path>]`，
接著每個警告一行——daemon 給的，以及每個 writes 和你重疊的 live task。`--json` 改成印 daemon 的原始
回答。被拒時在 stderr 印 `refused, <status> <code>: <message>` 並以 1 結束；每個 code 的意思見本節最後
的表。root 是你的對話，取自 `CLAUDE_CODE_SESSION_ID` 或 `CODEX_THREAD_ID`，否則用 `--conversation`；
child 的 assistant 預設跟你一樣，`--assistant` 可以改；project 預設是目前目錄的 git top-level，
`--project-dir` 可以改。`--claims ""` 表示這個 child 什麼都不寫。secret 不會出現在 argv、`task.json` 或
輸出裡，token 的讀法跟其他 thin command 一樣。

`--persona <id>` 讓 child 以內建角色（persona）開啟（`task.json` 的 `persona`）；這個版本沒有的 id 會在
本機就被拒絕。任何 kind 都不會自動套用角色，`plan_review` 也一樣：要審查員就自己寫 `code-reviewer`。
這個 build 有哪些 id 看 `GET /v1/personas`；角色是什麼，見 §10 的 Epic 段落（`clawdline guide epic`）。

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
writes、deliverables、kind 和 timeout 都在裡面——所以 child 讀到的 task 就是通過驗證的那一份，它不必再讀
`task.json`。

| 欄位 | 規則 |
|---|---|
| `clawdline_protocol` | `1` |
| `task_id` | 同一個 id |
| `assistant` | `claude` 或 `codex` |
| `project_dir` | 一個已經存在的目錄的絕對路徑 |
| `title` | 顯示在畫面上：一行、最多 60 字元，說完成後什麼會不一樣。冒號（`:` 或 `：`）、以 "the user" 當主詞、或用 code 格式的識別字開頭，會以 `bad_task`（`title: …`）拒絕 |
| `instructions` | 必填，最多 16 KiB。內容必須自己就講得清楚：除此之外 child 什麼都不知道。寫進你已經查證過的事實，每一條附 `file:line` 或指令；調查 child 還要有停止條件與 turn 上限 |
| `claims` | **必填**：最多 32 個 child 可以寫入的相對路徑。`[]` 表示它什麼都不寫，派工會帶一個警告（`claims_missing`） |
| `isolation` | `none`（預設），或 `worktree`：在自己的 branch 上開一份私有 checkout |
| `permission_mode` | `ask`、`edits` 或 `full` |
| `timeout_minutes` | 1–240，預設 30 |
| `kind`、`deliverables`、`model` | 選填；`model` 只能用 `[a-z0-9._-]`，最多 64 字元 |
| `work_id` | 選填，這個 task 所服務的看板項目的 UUID |
| `persona` | 選填，內建角色的 id（`GET /v1/personas`）；預設沒有 |
| `auto_compact_window` | 選填，只限 Claude：child 在 context 到多少 token（50000–1000000）時壓縮，或 `null` 表示不壓縮。不寫就跟著這台機器的 `claude_auto_compact_window`，使用者沒設就是關閉。用來比較兩次執行，不是每份 brief 都要寫：壓縮可能漏掉細節 |
| `root` | **必填**：`{"session_id": "<你的 conversation id>", "assistant": "claude"\|"codex", "project_dir": "<與外層 project_dir 相同的絕對 repository 路徑>", "label": "…"}`。有角色範圍的 root 需要 `root.project_dir`，daemon 才能驗證 Project 範圍。 |

**派工前，要讓 worker surface 與啟動模式符合 child 必須使用的每一項工具。** brief 必須列出必要工具，
root 也必須證明選到的 surface 真的提供它們。Codex CLI child 不會因為調整 permission flag，就得到
ChatGPT desktop app 內建的 `@Browser`。UI、無障礙或 responsive-layout review 必須派到真正有
Browser／Computer Use 的 surface，或明確指定 Playwright／Chrome CDP 等等同驗收能力的本機 browser harness，
並先證明它已安裝。若該 surface 可能要求 App、origin 或 GUI 權限，就用 `--permission-mode ask`：Codex
`full` 是 non-interactive shell 啟動（`--ask-for-approval never`），不是所有工具；沒有建立 request，
Auto-review 就無從審核。child 開始時要真的操作每項必要工具，不能只檢查 command name；缺少任何一項就
立即回報具體缺口，由 root 補回權限或重派。不得因 root 選錯 worker，就把依賴工具的驗收標成 unverified 後結束。

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
使用 `clawdline dispatch --task-id <uuid>` 重送同一筆派工時，保留原本的 id 與內容。
回應是 `{ok, task, warnings?}`。要讀 `warnings`：`claims_overlap`、`claims_missing`、
`claims_ignored_for_worktree`、`dirty_worktree_base`，以及 `work_not_placed`（指定的項目還沒能移上看板；
看板的定期掃描會在一個 tick 內補上）。同一個 id 再 POST 一次，會回之前存下的 task 並帶
`replayed: true`，所以重試是安全的。

分頁開不起來時仍然回 200，只是 `task.state: "spawn_failed"`。
`POST /v1/orchestrator/tasks/<id>/respawn`（orchestrator token）會用新的 secret 開一份副本，每個原始
task 最多兩次。

**你會遇到的拒絕**，照檢查的順序排：

| 狀態 | Code | 怎麼處理 |
|---|---|---|
| 409 | `task_unreadable` | 這個 id 的 task 存在但讀不出來；不要用同一個 id 重送 |
| 422 | `bad_task` | 訊息會點出是哪個欄位。「No readable task.json under …」也是這一種——檢查 `task_root` |
| 422 | `claims_required` | 補上 `claims` |
| 422 | `root_session_required`、`root_assistant_required` | 補上 `root.session_id` 和 `root.assistant` |
| 403 | `session_scope_mismatch` | 檢查 `root.project_dir` 是否存在，且與根 Session 的 Project 及角色快照相符。修正 brief 或 CLI；不要請使用者改 Project 設定。 |
| 422 | `detached_route_required` | 你送了 `root.poll_only`；那是 detached automation（§6） |
| **409** | **`stale_inventory`** | 你的 `generation` 沒帶或過期了。錯誤裡附著目前完整的 inventory：讀它、重新判斷，再用它的 `generation` 重送 |
| 422 | `work_not_found`、`work_other_project`、`work_closed` | 你指定的 `work_id` 不存在、屬於別的 Project，或已經關閉 |
| 422 | `also_work_not_found` | `also_work_ids` 裡有 id 不是看板項目；之後還會像 `work_id` 一樣逐一檢查 |
| 503 | `store_unavailable` | 讀不到看板，無法檢查指定的項目；什麼都還沒開始，再送一次 |
| 409 | `graph_*` | task-graph 的准入規則（`graph` 欄位） |
| 409 | `no_child_capability` | 這個平台開不了 child；`missing` 會說缺什麼 |
| 429 | `squad_launch_capacity` | 還在等 Session 身分的角色啟動太多；稍後再試 |
| 429 | `rate_limited` | 十分鐘內派工次數太多 |
| 422 / 409 | `root_unresolved`、`conversation_ambiguous` | 你的 conversation id 對不到任何活著的 session，或對到不只一個。把它修好；不要改走 detached |
| 403 | `session_actor_required` | 以角色開啟的 root 必須從自己的 Session、帶它的 squad capability 派工 |
| 403 | `session_scope_mismatch` | 這裡也會檢查：根 Session 的 Project 與它的角色快照不符 |
| 503 | `squad_policy_unavailable` | 讀不到角色指派設定；什麼都還沒開始 |
| 409 | `persona_disabled_for_auto_assignment` | 目標 Project 關閉了這個 persona 的自動指派 |
| 429 | `over_capacity` | 你的 child 名額（預設 5 個）或整台機器的名額滿了；看 `retry_after` |
| 409 | `workspace_busy` | 另一個 root 的 writes 跟你重疊；錯誤會點出擋住你的那個 task |
| 409 | `worktree_unavailable` | 私有 checkout 建不起來 |
| 429 | `terminal_busy` | 所有 terminal 寫入通道都在忙；`retry_after: 5` |

## 5. 執行中，以及結束時

child 會用 `clawdline task accept` 簽收 briefing（它會送 `/accepted`，連不到時留下 `accepted.json`），
計畫改變時可以送一則進度說明（`/progress`），最多可以推五則通知（`/notify`），最後寫好 `result.json`、
執行 `clawdline task finish` 收尾。這些路由你不用呼叫。

- `clawdline task show <id>`——單一 task 和它的狀態（`GET /v1/orchestrator/tasks/<id>`）。`GET /v1/orchestrator/tasks` 列出全部
  （`?state=`、`?limit=` 最多 500）。
- **child 結束時，daemon 會在你的輸入框打一行 `<clawdline-notice>`。** 它的 `body` 只有一句：哪個 task、
  怎麼結束、只屬於這次交付的事實（stalled、writes 已釋放、它的 branch、幾個 leftover），以及要執行的兩個
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

**merge 了 child 的 branch 之後，什麼都不用再做**：broker 會自己記下 `landed`（見下）。宣告不寫入
（`--claims ""`）、自己結束、branch 和 checkout 都沒留下東西的 child，broker 會在打完成通知之前自己記
`nothing_to_land`。手動記的 landing 是給這兩者都涵蓋不到的情況——cherry-pick、`incorporated` 的交付、
`nothing_to_land`、`abandoned`——而且是一行指令，帶 orchestrator token 送出：

```
clawdline task land <task id> <landed|incorporated|abandoned|nothing_to_land|pending> \
  [--target <branch>] [--commit <sha>] [--carrier-task <task id>] [--note "…"]
```

它就是下面這條 route；script 可以帶 `X-Clawdline-Orchestrator` header 直接呼叫：

```
POST /v1/orchestrator/tasks/<id>/landing
{"state": "pending" | "landed" | "incorporated" | "abandoned" | "nothing_to_land", "target": "<ref>", "commit": "<sha>", "carrier_task": "<task id>", "note": "…"}
```

- 只收這幾個 key，外加 `delivery`（收下但不使用）；其他 key 一律拒絕。`pending` 和 `abandoned` 接受
  task secret 或 orchestrator token；`landed`、`incorporated` 和 `nothing_to_land` 只接受
  orchestrator token。
- **合併會自己記帳。** 已結束的 task 分支一旦合併進 target，broker 會在幾分鐘內用同一道 Git 查證
  自己記成 `landed`，commit 是 target 當下的 head。紀錄上沒有 target 時，只有主 checkout 的 branch
  是唯一含有這份交付的 branch 才會自己命名。cherry-pick、`incorporated` 與 `nothing_to_land` 仍由你記。
- **完成通知會寫出 task 結束當下 branch 的狀態**，每一種只要求一件事，而且寫成指令。*branch 上什麼都沒
  commit*：landing 要從那個 branch 證明，所以照現況永遠不可能記成 landed——趁 checkout 還在磁碟上，在它
  裡面、那個 branch 上 commit，或執行 `clawdline task land <id> abandoned`。*branch 上有 commit*：把那個
  branch 合併進 target；merge 會自己記 landing。*讀不到*：先去看 branch 再記。*寫進共用 checkout*：
  `clawdline task land <id> landed` 帶上把那份工作帶上 target 的 commit，或記 `abandoned`。*它什麼都沒寫，
  broker 已記 nothing_to_land*：只剩 ACK。
- `landed` 需要 `target` 和 `commit`；`incorporated` 需要 `target`、`commit` 和 `carrier_task`（以自己已查證的
  landing 帶上這份交付的另一個 task）。兩者 daemon 都**會去 Git 裡查證**；查不過就回 `409 unverified_landing`，
  `reason` 是下列之一：`commit_unresolved`、`target_unresolved`、`not_on_target`、`base_unknown`、
  `predates_dispatch`、`delivery_unknown`、`nothing_delivered`、`not_the_delivery`，以及 `incorporated` 專有的
  `carrier_required`、`carrier_is_delivery`、`carrier_unresolved`、`carrier_not_landed`、
  `carrier_repository_mismatch`、`carrier_target_mismatch`、`carrier_commit_mismatch`、
  `delivery_is_ancestor`。
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
| **Root 指派** | `POST /v1/orchestrator/root-assignments` | 為新功能開一個獨立負責的新 Root |
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

**Root 指派。** `Idempotency-Key` header 必須等於 `request_id`：

```
{"request_id": "<uuid>", "assistant": "claude"|"codex", "model": "…", "project_dir": "/abs", "label": "…",
 "assignment": {"objective": "…", "scope": "…", "constraints": "…", "relevant_references": "…", "acceptance": "…"}}
```

每個指派欄位 1–8192 bytes，加起來最多 32 KiB。daemon 自己寫 brief、自己開 session。它**沒有
parent、secret、timeout、result，也沒有 landing**：它結束時不會通知任何人，因為它不向任何人負責。
拒絕：`bad_root_assignment`、`idempotency_mismatch`、`request_conflict`、`rate_limited`。絕對不要用
child、detached task 或 handoff 假裝成 Root 指派。

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

排程可以不設時間：用 `"trigger_only": true` 取代 `at`、`days` 與 `on`。時鐘永遠不會執行它；它只由
`…/run` 或它的 webhook 啟動，而且只在 `enabled` 時。它和重複排程一樣是長期指示，需要同一份證據。

**在另一台機器啟動任務，並知道它怎麼結束。** 機器之間沒有直接的通道。把任務做成目標機器上的
trigger-only 排程，替它綁一個 Cloud webhook，把網址存在呼叫端讀得到的地方（它是憑證：只有你能讀的
檔案，或 `CLAWDLINE_WEBHOOK_URL`；絕不放在命令列參數）。然後在呼叫端的機器上：

```sh
clawdline webhook fire --url-file <path>
```

它會送出 `{"deliver_within_seconds": 60}`（`--deliver-within`），所以關機中的目標之後不會補跑；它追蹤
這筆 delivery 的狀態直到結束或超過 `--timeout`（60m），變化印在 stderr，最後一行印在 stdout。Exit
`0` 成功 · `1` 結束但沒成功（failure、timed_out、cancelled、spawn_failed）· `2` 沒送到機器（expired、
canceled、連不到）· `3` 被拒絕（dispatch_refused 與它的 code、網址已不可用、速率限制）· `4` 不等了，
附上最後看到的狀態。`--no-wait` 在 `202` 之後印出 delivery id 就返回。回報時照實寫 exit code 與最後一行：
`2` 代表什麼都沒跑，不是失敗。

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

## 9a. 留下需要人處理的便條

長時間工作的 Agent 有一件具體的事需要使用者閱讀、執行或決定，而一般對話訊息容易被後續內容淹沒時，使用便條。便條留在目標 Session 預設收合的「關注」區，未處理時顯示紅點；Agent 可以繼續做不依賴該答覆的工作，使用者可在告一段落時回來看。便條不是進度日誌、Agent 私人提醒、推播通知，也不是看板決策的授權。同一件事不要重複貼便條。

**要在對話中請使用者選擇之前**，先建立一張 `answer` 便條，寫清楚實際問題、決策所需的取捨，以及二至四個完整的建議回覆。點一下按鈕就會把回覆當成對話訊息送出，所以每個 `draft` 單獨閱讀也要清楚。建立後，在對話中簡短提示即可。建立便條或便條被標記為已處理，都不是使用者的答覆；收到對話訊息（點選送出的回覆或使用者自己打的）後，才能執行依賴該決定的動作。建立失敗時，說明失敗並直接在對話中提問。只有需要人判斷的決定才用便條；Agent 能自行決定的例行選擇不用。

把便條內容寫成 JSON 檔。沒有 `--target` 時，CLI 透過 `whoami` 找到本存活 Root 的 terminal id。若目標是另一個 Session，才用通訊錄中的存活目標 **terminal id**（`clawdline guide zh-TW send`）指定 `--target`。`--from` 預設由環境取得本存活 Root 的 conversation id。CLI 自行讀取機器憑證並補上來源、目標身分，不會把憑證放進命令列；成功時印出持久便條 id。結果不確定而需重試時，沿用印出的 `--key`。

```json
{"kind":"answer","title":"請選擇日期","summary":"發布日期需要你決定。","action":"方便時請選一個日期。","reason":"只有你能決定日期。","options":[{"label":"週二","draft":"週二可以。"},{"label":"週三","draft":"週三可以。"}]}
```

```sh
clawdline note create --body-file note.json
# 要寫給另一個 Session：clawdline note create --target <terminal-id> --body-file note.json
```

`kind` 可為 `read`、`answer`、`action` 或 `report`；`title`、`summary`、`action`、`reason` 必填。`answer` 可提供二至四個選項，每個選項的 `draft` 是按鈕顯示的建議回覆。使用者點一下，Console 就把它當成對話訊息直接送到便條所在的 Session，後面附上便條 ID、標題和待回覆事項，讓接收的 Session 知道使用者回答的是哪張便條；按鈕不顯示這段脈絡。送出成功後，便條才會移到最近已處理。送出失敗時，便條維持待處理，關注按鈕會說明回覆沒有送出。較長內容放 `detail`；`document_url` 可指向真正可讀的 Cloud 文件，建立便條前先驗證文件路徑與檔案。依實際收到的對話訊息行事，不看便條狀態：使用者手動標記已處理的便條，並沒有送出任何訊息。若工作確實卡在答覆上，另記錄等待使用者的狀態，並按既有規則發送一次關注通知。便條本身不推播，也不喚醒 Agent。

## 10. 看板

看板有三種結構——看板項目、Backlog，以及每個 session 自己的待辦清單——而且**上面放什麼由人決定**。
只有在使用者透過 Clawdline 送來的訊息親口要求時，session 才自己建立看板項目；其他時候只能提案。
從不主動建卡片。唯一的例外是 Epic 的負責 Session：Epic 的計畫審查過之後，它可以把 Epic 拆成 Feature、
Issue 項目並指派給其他 Session（`clawdline guide epic`）。

**TODO／待辦／土度跟看板項目一起講，指的就是那個項目的 steps。** 用 `--step` 放到項目上。**不要**
再用 `clawdline todo add` 寫一次。`clawdline todo add` 只用在使用者要你把一份清單記成這個 Session
自己的待辦、而且沒有看板項目的時候。

**使用者要你建立看板項目時。** 只有在他的訊息——透過 Clawdline 送來，所以有 run——明確要求時，
才由你自己建立：

```
clawdline item add --project <place id> --kind feature|issue|epic|refactor|plan --title "…" \
  --step "第一步" --step "第二步" …   [--description-file f | description 從 stdin] [--assign-self]
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
  "project_id", "kind", "title", "description", "deployment_policy"?, "steps"?: ["…"], "assign"?: {"mode": "self"} | {"mode": "existing_session", "terminal_id": "…"} | {"mode": "new_session", "assistant"?: "…"}}`，
  回 `201` 和 `{"item", "assigned", "assignment_state"}`。
- Feature、Issue、Epic 預設建好時**不指派給任何人**，放在看板上同類未指派項目所在的地方，並帶著 steps：
  依序是那些 `--step`，沒給的話，就是 description 裡兩列以上的頂層 Markdown 清單。使用者常常只是要你
  先把工作寫下來、之後再做；建立項目不代表項目歸你。`assignment_state` 是 `not_requested`。
- **只有使用者的訊息要這個 Session 現在就去做**（「開個項目然後把它做掉」）時，才加 `--assign-self`
  （`"assign": {"mode": "self"}`）。這樣項目建好時就**指派給你**，phase 是 `assigned`，在同一筆寫入裡
  完成。不會有任何字打進你的 terminal——是你自己要的。照順序做，每一步確認完成後就勾掉
  （`clawdline item steps <item id>`、`clawdline item step-done <item id> <step id>`；做下去發現還少一步，就用
  `clawdline item step-add` 補上），並像任何已指派項目一樣用 `clawdline item phase` 推進 phase（見下文）。
  這樣接下的 Epic，進 implementing 之前要先走完 Epic 流程（見下文）。使用者之後才要你接下你先前建好、
  未指派的項目，就用 `clawdline item claim`（見下文）。Refactor、Plan 不論有沒有 `--assign-self`，
  都以未指派狀態建在規劃區，不帶 steps（`planning_has_no_steps`）。
- 已登記的 Clawdfather 是工程項目的例外：它不能擁有或修改 Project 程式碼。使用者的訊息明確要求新項目時，
  可以用 `clawdline item add --project <place id> --kind feature --title "…" --assign-new`（或
  `--assign-terminal <id>`）先建立項目，再交給 Project Session。若指派失敗，項目保留為未指派，
  回應會說明 `assignment_error`，使用者可從看板指派；沒有明確要求時先提案並等待接受。
- 使用者會看到卡片上寫著「Session 依你 HH:MM 的訊息建立」，並引用他的原話。
- 拒絕，每一種都什麼都不寫：`run_unknown`（沒指名 run，或沒有這個 run）、`run_expired`（超過一天）、
  `run_other_session`（那是傳給別的 Session 的訊息）、`session_not_found`、`child_session`（child 用
  `result.json` 回報）、`project_not_found`、`project_mismatch`（一般可執行項目必須在你工作的 Project
  裡）、`too_many_steps`（超過 128）、`run_items_exhausted`（一則訊息最多撐五個項目）。
- **沒有 run**——使用者是直接在 terminal 打字，所以 `item add` 回 `no_run` 或 `run_unknown`：改走
  提案（見下文），並告訴使用者到看板的 Agent 提案裡接受它。

絕對不要主動建立看板項目，也不要一次建好幾個來規劃推測性的工作。

**認領使用者指給你的看板項目。** 使用者透過 Clawdline 傳來的訊息要你接下看板上某個已經存在的項目——
「把 release notes 那個項目接走」「認領 <item id>」——就認領它，一條指令：

```
clawdline item claim <item id>
```

- `item claim` 會讀這個對話最新的 run，除非用 `--run` 指定；接著讀項目拿到 version，送出前先印出
  Idempotency-Key（用 `--key` 重送同一筆寫入），成功後印出項目。它就是
  `POST /v1/work/v2/agent/items/<id>/claim`，body 是 `{"expected_version", "session_id", "via": {"run"}}`，
  而且只會把項目指派給**你，也就是那則訊息送達的 Session**——裡面沒有任何欄位能指名別的 Session 或
  terminal。
- 之後這個項目看起來就跟使用者從看板把它指派給你一模一樣：你是 owner，phase 變成 `assigned`，原本沒有
  steps 的話會從 description 的清單補上。不會有任何字打進你的 terminal。照任何已指派項目的做法做（見下文）。
- 使用者會看到卡片上寫著「Session 依你 HH:MM 的訊息認領」，並引用他的原話。
- 拒絕，每一種都什麼都不寫：`run_unknown`、`run_expired`、`run_other_session`、`session_not_found`、
  `child_session`（同 `item add`）；`work_not_found`；`project_mismatch`（項目在你沒在工作的 Project）；
  `item_assigned`（已經有 Session，或正在為它開一個——只有使用者能把項目從一個 Session 移到另一個）；
  `item_terminal`（已完成或已取消）；`planning_not_assignable`（Refactor、Plan 留在規劃區；Epic 可以認領）；
  `version_conflict`（項目變了，重跑一次指令）；`run_claims_exhausted`（一則訊息最多撐五次認領）。
- **沒有 run** 會回 `no_run` 或 `run_unknown`：把項目留給使用者指派。

**把看板項目指派給使用者要的新 Session。** 使用者透過 Clawdline 傳來的訊息要你把某個尚未指派的 Feature
或 Issue 交給一個新 Session——「幫 <item id> 開一個 security Session」——就指派它，一條指令：

```
clawdline item assign <item id> --new [--assistant claude|codex] [--model m] [--persona <id>]
```

- 項目不是任何 Epic 的子項目時，`item assign` 會跟 `item claim` 一樣讀這個對話最新的 run，除非用 `--run`
  指定。它就是 `POST /v1/work/v2/agent/items/<id>/assign`，body 是 `{"expected_version", "session_id",
  "mode": "new_session", "assistant"?, "model"?, "persona"?, "via": {"run"}}`，開出來的新 Session 跟使用者
  自己在看板選「新 Session」開的一樣。
- 卡片上會寫「Session 依你 HH:MM 的訊息指派」，引用他的原話，並標出新 Session 帶的角色。
- 拒絕，每一種都什麼都不寫：`item claim` 的那些（`run_unknown`、`run_expired`、`run_other_session`、
  `session_not_found`、`child_session`、`project_mismatch`、`item_assigned`、`item_terminal`、
  `version_conflict`，以及 `run_claims_exhausted`：認領和指派共用一則訊息的五次）；`kind_person_assigns`
  （只能是 Feature 或 Issue）；`new_session_only`（要自己接就用認領）；`unknown_persona`；
  `persona_disabled_for_auto_assignment`（這個角色在該 Project 關掉了自動指派）。

絕對不要主動認領或指派項目——只處理使用者訊息指名的那一個——也不要用使用者的
`POST /v1/work/v2/items/<id>/assign`，那條會拒絕 Session（`session_cannot_create_item`）。Epic 的負責
Session 也用 `clawdline item assign` 指派那個 Epic 自己的子項目（`clawdline guide epic`）。

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
- 提案要用一般人能直接理解的白話（plain language）：`title` 說可感受到的結果，`description` 說會改什麼，`reason`
  說為什麼現在值得做，`suggested_acceptance` 說完成後可以觀察到什麼。四欄都必填；不要拿未解釋的
  縮寫、內部識別碼、程式路徑或實作術語當成主要說明。看板會先顯示標題、來源與原因；使用者按
  **Explain／詳細說明**後才展開「會改什麼」與「完成後會看到什麼」。
- **要提一個帶 steps 的項目**，就在 `description` 裡寫兩列以上的頂層 Markdown 清單。使用者接受並
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
{"session_id": "…", "work_id": "<這個問題所屬的看板項目>", "question": "…", "options": [{"id": "a", "label": "…"}, …],
 "default": "a", "blocking": true, "due_in_minutes": 1440}
```

項目必須仍開啟，而且由這個 Session 負責；看板會把問題放在那張項目卡裡，讓人看得到脈絡
（`decision_source_required`、`decision_source_not_found`、`decision_source_invalid`、
`decision_source_closed`）。二到四個選項；`default` 必須是其中之一，也就是沒人回答時會採用的選項
（7 天後，除非 `due_in_minutes` 指定 60–10080）。只有 `blocking` 的 decision 會推播。用
`GET /v1/orchestrator/decisions/<id>` 讀答案。

**回答的是使用者；Session 只能代轉他說的話。** proposal、decision 和看板項目都在 `/v1/work/…`
底下回答。Session 要寫進去，必須指名帶著使用者那句話的 run：`"via": {"run": "<id>"}`，沒帶就被
拒絕（`403 session_cannot_decide`）。用 `GET /v1/orchestrator/sessions/<conversation>/run` 讀最新的 run；
捏造、過期、屬於其他 Session 或早於問題的 run 都會具名拒絕。使用者直接在 terminal 打的字沒有 run，
請他透過 Clawdline 回答，或由他自己在 console 操作。

**你待收的 child。** `GET /v1/orchestrator/sessions/<conversation id>/todos`——用 conversation id 指名，
不是 terminal id（否則回 `409 session_id_is_terminal`）。這些項目由 broker 根據 task 的事實開啟和關閉；
你不需要寫任何東西。

每個 turn 的邊界、宣告自己閒置之前，也要讀
`GET /v1/work/v2/agent/session-todos/<conversation id>`。其中的 `assigned_items` 是使用者交給這個
Session 的看板項目，`recent_items` 是這個 Session 最近完成的項目，`direct_todos` 是快速交辦，`unacknowledged_completions` 是你還沒 ACK 就已經結束的 child（第 5 節）。這條
pull 路徑讓工作中收到的指派先等著，不會打斷目前的 turn。完成目前的 turn 之後，把 assigned item 當成
下一件自己負責的工作，並用 `clawdline item steps <id>` 讀取完整內容（路由是 `GET /v1/work/v2/items/<id>`）。

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

只有在對方明確指向你剛完成的項目時才使用。這條路由只接受最後由同一個 Session 釋放指派的
`done` 項目；它不能推翻使用者的取消，也不能拿走另一個 Session 的完成項目。成功時會保留先前證據、以
新 cycle 回到 `implementing`、恢復這個 Session 為 owner，並把原因寫入不可變的項目歷史。原因最多
8 KiB。語意含糊的追問不構成修改看板的授權。

負責項目的 Agent 需要使用者動手或做選擇時，就開一個 decision 並等它。先開一個關於這個項目的 decision
（`POST /v1/orchestrator/decisions`，帶這個項目的 `work_id`、二到四個選項、`default` 和期限；要使用者動手時，
選項可以是 `{"id": "done", "label": "我做好了"}` 和 `{"id": "cannot", "label": "我沒辦法"}`），再用
machine-authenticated route 讓項目指向它：

```
PATCH /v1/work/v2/agent/items/<id>/edit     (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>",
 "condition": "waiting_user", "decision_id": "<the decision's id>"}
```

decision 必須存在（`decision_not_found`）、是這個 Session 開的（`decision_other_session`）、關於這個項目
（`decision_other_item`），而且還沒結束（`decision_not_open`）；在其他 condition 帶 `decision_id` 會得到
`decision_requires_waiting_user`。沒有 decision 的 `waiting_user` 會以 `waiting_user_requires_decision` 拒絕。
使用者在看板卡片或「等你決定」回答；decision 被回答，或到期由 default 生效時，daemon 會在同一筆寫入清掉項目的
`waiting_user`、把答案記在項目上，並在這個 Session 閒置時把所選選項的 id 與 label 打進來。要自己停止等待，就用
同一路由把 `condition` 設成空字串（或其他 condition）；decision 隨即變成 `withdrawn`、離開「等你決定」，項目被
釋放、改派、取消或完成時也一樣。回答已 withdrawn 的 decision 會以 `decision_withdrawn` 拒絕。

**本輪擷取的規劃與驗證 gate。** `planning_gate` 預設開、`verify_gate` 預設關；
`clawdline setting get|set planning_gate|verify_gate` 接受 `on/off` 或 `true/false`。每輪第一次成功
指派時固定兩個值；同輪改派或之後改全域設定都不影響本輪。規劃 gate 開啟的 Epic 與 Feature 在進入實作前
須有驗收條件。Epic 仍須計畫與獨立審查；Feature 只有在使用者勾選「需要獨立審查」時才需要（見下文）。Issue 不受規劃 gate 約束。規劃關閉時，
Epic 也跳過強制計劃。兩者皆開會先規劃再獨立驗證；只開規劃沿用一般 Merge 驗證；只開驗證會跳過規劃，
但仍檢查固定候選提交；兩者皆關走一般流程。使用者不必在看板填寫驗收條件。受 gate 約束的項目若尚無
驗收條件，負責 Session 在指派後、跨過 gate 前，用 `clawdline item acceptance <item id> --body-file <file>`
寫入可觀察的 Markdown 條件。若人明確要求修改已寫入的驗收條件，用
`clawdline item acceptance-revise <id> --run <message run> --expected-version <item version> --body-file <file>`；
項目版本是 `GET /v1/work/v2/items/<id>` 回應裡的 `.item.version`（`item steps` 只印驗收版本）；訊息的 run 從
`GET /v1/orchestrator/sessions/<conversation>/run` 讀。只根據該訊息提出的修改內容重寫完整文件。
負責 Session 只能首次補上空白內容；後續一般修訂由使用者處理，或由 Epic
owner 依有理由的驗證升級決定修訂。進入 Merge 前改動驗收會使舊 PASS 與覆核失效；進入 Merge 後即鎖定。

本輪驗證 gate 開啟時，先在已 commit、乾淨且登記過的 worktree 執行
`clawdline item phase <id> verifying`；CLI 送出目前 branch 與完整 HEAD，daemon 核對 Project、
本輪起點、Git tree 及驗收 digest。獨立唯讀 Codex checker 對 Issue 使用 `code-reviewer`、對 Epic
使用 `reality-checker`；有參考圖片或設計文件的 Feature 使用 `evidence-collector`，其他 Feature
使用 `reality-checker`。具型別的結論只有 `PASS`、`FAIL`、`NEEDS_WORK`；無法驗證的主張須說明原因，
不能授權 Merge。缺少或格式錯誤的結果是技術失敗，僅有一次有界重試，之後升級處理。Epic 的最後端到端
驗證要等所有子項目結束，且受影響的元件已整合成可執行的候選版本。先由各子項目做針對性測試，Epic
負責人做跨元件基本檢查；不要拿假資料畫面、未整合分支或未完成的 API 派出瀏覽器／多帳號端到端驗證。
派工前要確認驗證員能用已授權的瀏覽器或等效本機工具打開目標網址，並有測試帳號、資料及來源權限；
brief 要寫明使用途徑。`--permission-mode full` 本身不會賦予瀏覽器權限。工具預檢失敗時先解決存取問題，
不要把另一位驗證員派進同一個阻礙；預檢失敗不算開始端到端驗證。每個 Epic 原則上只安排一輪完整端到端驗證，
不要對每個子項目或每次修訂各做一輪。修正缺陷後只重驗受影響情境；只有驗收範圍或整合邊界有重大變動，
才記錄理由並重做完整一輪。`verifying → merging` 須有對應候選提交及驗收的有效 PASS，或記錄原因的明確
覆核；一段自行撰寫的驗證敘述沒有授權力。連續三次 FAIL 先交給仍在線的上層 Epic owner，該 owner
不可用時交給使用者；技術失敗另行升級。只有指定的上層 owner 使用
`POST /v1/work/v2/agent/items/<id>/gate-decision`；使用者使用
`POST /v1/work/v2/items/<id>/gate-decision`，Agent 不可代用使用者路由。看板分別標出 AI、使用者和
技術覆核，絕不當作 checker PASS。詳情容量額滿時，使用者先下載
`GET /v1/work/v2/items/<id>/gate-export` 並核對 manifest digest，再以該 digest 和項目版本確認
`POST /v1/work/v2/items/<id>/gate-purge`。只清除符合條件的已結束詳情；總計、最新事實及稽核仍保留。

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
- `done` 會釋放你的指派，項目移到這個 Session 的「最近完成」。需要結案報告時（見下文）
  要在這之前加上。
- 拒絕：`invalid_transition`（不是下一個 phase，或缺它要的證據）、`steps_incomplete`、
  `not_item_owner`、`item_unassigned`、`item_terminal`（要由人重開）、`evidence_unknown`、
  `direct_landing_not_applicable`、`invalid_landing_evidence`、`landing_project_not_found`、
  `landing_commit_unresolved`、
  `landing_target_unresolved`、`landing_not_on_target`、`landing_remote_unresolved`、
  `landing_not_published`，以及 `version_conflict`：重讀後再送。

**一個指令收尾。** 工作 landing 之後，`clawdline item finish <item id>` 在同一個交易裡把項目從
`implementing`、`verifying`、`merging` 或 `deploying` 走到 `done`：

```
POST /v1/work/v2/agent/items/<id>/finish    (Idempotency-Key required)
{"expected_version": <version>, "session_id": "<conversation id>", "verification"?: "…",
 "landing"?: {"commit"?, "target"?, "remote"?, "project"?},
 "deployment"?: "…", "no_deployment_reason"?: "…"}
```

- 每一步都是 `item phase` 會走的那一步，過同樣的 gate，各寫一筆 `item.phase_changed`；任何一步被拒，
  整個都拒，什麼都不寫。
- landing 是讀出來的，不是打出來的：有 gate 的項目，commit 是 gate 授權的 candidate；沒有的話是綁定
  child landing 的 commit；target 是那些 landing 記的 branch；remote 是那個 branch 追蹤的 remote。你給的
  欄位優先，結果照 `item phase deploying` 的方式向 git 驗證。本輪擷取的 verification gate 仍然要 PASS：
  有 gate 的項目從 `implementing` 收尾會被拒 `verification_candidate_required`，所以先在 candidate
  worktree 用 `item phase` 進 `verifying`，等 PASS，再收尾。
- 已經 `done` 的項目照原樣回覆、什麼都不寫，所以同一個 landing 看到兩次也不會再動。
- 拒絕代碼另有 `verification_required`、`landing_required`、`deployment_required`（那一步缺的說明）、
  `landing_target_unknown`、`landing_remote_unknown`、`landing_remote_unreadable`、
  `landing_ambiguous`（用旗標指名）以及 `finish_not_started`（還在 `implementing` 之前）。

已指派的項目可能帶有 `steps`。成功指派時，description 裡兩個以上的頂層 Markdown 列點可以自動成為
steps，用 `clawdline item add` 建立的項目則帶著它的 `--step`；每一列都是父項目清單上的一個 step，不是另一張看板項目。確認完成一列後，用
`clawdline item step-done <item id> <step id>` 勾掉；它會讀版本，送出
`POST /v1/work/v2/agent/items/<item-id>/steps/<step-id>/complete` 與 `{"expected_version", "session_id"}`。版本衝突時再跑一次。
只要還有任何 step 未完成，`done` 轉換就會以 `steps_incomplete` 拒絕；父項目的 phase 前進不會偷偷把
step 勾成完成。

**自己把項目拆成 steps。** 你負責的項目還沒有 steps、而工作要分階段做——好幾個要各自驗證的改動，或
動到系統裡不只一個部分——就在動手實作之前，自己把它拆成有順序的 steps：兩到八個具體、各自能驗證的
步驟。單純一次就改完的事**不要**拆 steps，也不要為了有清單而湊數。做下去發現比想像的大，再補上那一步。

```
clawdline item step-add <item id> "接上 route" "補一個測試" "寫進 guide"
```

標題直接當參數給，或從 stdin 一行一個（空行略過）。指令每加一個標題前都會重讀項目，寫入前先印出
Idempotency-Key，最後印出項目和它所有的 steps。每個標題就是一次只有 owner 能送的請求：

```
POST /v1/work/v2/agent/items/<id>/steps     （必須帶 Idempotency-Key）
{"expected_version": <version>, "session_id": "<conversation id>", "title": "…", "position": <n>}
```

`"position"` 取現有最後一個 step 的下一號，因為 steps 是照 position 排序的。之後每一步確認完成，就用
`clawdline item step-done` 勾掉。這不是看板項目和待辦禁止的「主動建立」：項目本來就是你的，steps 是
讓使用者看到你手上這件工作分成哪幾段。

如果 issue 或 incident 必須經過深入調查，才找出 root cause（根因），或必須排除多個看似合理的解法才
確認真正修正，請在把項目推進 `done` 之前加入一份給使用者閱讀的結案報告。直接觀察就能確認的直觀修正
不需要報告。把發生了什麼、root cause、修改內容、驗證方式，以及仍存在的邊界寫進一個檔案，然後：

```
clawdline item doc <item id> --role completion_report --title "結案報告" --body-file report.md
```

它替你送出 `POST /v1/work/v2/agent/items/<id>/documents`（欄位列在本節的 Epic 部分，
`clawdline guide zh-TW epic`）。自己組的 curl 沒帶指令讀的憑證，打這條路由會回 `401 unauthorized`。body 是 Markdown，最多 64 KiB。寫給提出問題的人讀，不要貼成原始 debug log，也不要放入私密資料。只有
尚未結案的 active owner 能加入；版本衝突時先重讀。結案報告是具名敘述，不取代驗證、landing 或部署
證據；有報告時，它會留在已關閉的看板項目，並可從 Session 的「最近完成」列直接打開。

`/v1/board` 是 Swift app 的舊卡片，唯讀。landing 是 broker 的事實：項目永遠不會被人手動標成已 landing
（`422 landing_is_broker_fact`）。

### Epic 與 Feature：照使用者的「需要獨立審查」勾選決定

本輪規劃 gate 開啟的 Epic 仍須計畫及獨立審查。Feature 上有使用者設定的「需要獨立審查」勾選框
（項目欄位 `review_required`，`clawdline item steps <id>` 會印出來）。只有使用者能在看板上設定；你不能改，
也不要自己判斷這個 Feature 的風險。daemon 在你要求進入 `implementing` 時才讀這個值。

- **沒勾**（預設）：寫好簡短可觀察的驗收條件，實作後做針對性測試即可。不要為審查寫計畫、
  不要派 `plan_review` child，也不要記錄風險判讀。
- **有勾**，以及所有規劃 gate 開啟的 Epic：走以下計畫審查流程。

如果你覺得沒勾的 Feature 應該審查，直接跟使用者說，由使用者決定要不要勾；Agent 沒有向 daemon 要求審查的途徑。

1. 仔細規劃，把計畫寫到項目上：
   ```
   clawdline item doc <item id> --role plan --title "Plan" --body-file plan.md
   ```
2. 派一個唯讀 child，brief 是嚴格審查這份計畫——缺了什麼、哪裡錯、哪裡有風險：
   ```
   clawdline dispatch --kind plan_review --work-id <item id> --claims "" …
   ```
3. 它以 success 結束後，記下它的審查，寫給使用者看——它發現了什麼、計畫因此改了什麼：
   ```
   clawdline item doc <item id> --role plan_review --title "Plan review" --reference <task id> --body-file review.md
   ```
   Feature 的計畫在審查後修訂時，若仍在原審查的風險邊界內，於修訂計畫之後新增標題為
   `Review boundary assessment` 的 `other` 文件，JSON 寫
   `{"new_risk_boundary":false,"reason":"具體說明"}`。跨越新的或無法確認的邊界才派一次針對性複審。
   Epic 保留原有最多兩次強制審查的規則。
4. 用 `clawdline item step-add <item id> …` 把工作拆成 steps。
5. 這些都做完，才 `clawdline item phase <item id> implementing`。

計畫須排出驗證順序：各實作子項目先做自己的針對性測試；Epic 負責人整合受影響的元件並完成最小的
跨元件基本檢查；整合後的候選版本可正常運作，才派真正的端到端驗證與必要的獨立 UX／產品審查。
派工前確認驗證員的瀏覽器使用途徑、目標網址、測試帳號、資料與權限。瀏覽器不可用時先排除問題或改用
等效的本機瀏覽器測試工具，不要反覆派只會碰到同一障礙的唯讀驗證任務。預檢失敗不算一次驗證。
每個 Epic 的穩定版本原則上只做一輪完整端到端驗證；小修正只重驗受影響的流程。只有驗收範圍或
整合邊界有重大變動，才記錄理由並重做完整一輪。

`clawdline item doc` 會先讀項目的版本和最後一份文件的位置，送出前印出 Idempotency-Key（`--key`
重試同一筆寫入），做完印出項目。內容來自 `--body-file` 或 stdin。它就是
`POST /v1/work/v2/agent/items/<id>/documents`，body 是
`{"expected_version", "session_id", "role", "title", "body", "reference", "position"}`；role 有
`spec`、`design`、`test`、`deploy`、`completion_report`、`other`、`plan`、`plan_review`。

- `plan` 和 `plan_review` 只用在 Epic 或 Feature（其他種類回 `document_role_not_applicable`）。
- `plan_review` 的 `reference` 是審查這份計畫的 Clawdline child 的 task id。daemon 只在這些條件都成立
  時接受：task 存在（`plan_review_task_unknown`）、是項目的 owner Session 派的
  （`plan_review_task_not_owned`）、有綁 line 的話就是這個項目（`plan_review_task_other_item`）、kind 是
  `plan_review`（`plan_review_task_wrong_kind`）、以 `success` 結束（`plan_review_task_unfinished`）、
  派出時間不早於最新的 plan（`plan_review_task_stale`）。還沒有 plan 就送審查，會被
  `epic_plan_required` 拒絕。
- 本輪規劃 gate 開啟的 Epic 缺少已審查計畫時不能進入實作（`epic_plan_required` 或
  `epic_plan_review_required`）；使用者勾了「需要獨立審查」的 Feature 也一樣
  （`feature_plan_required` 或 `feature_plan_review_required`），沒勾的只需要驗收條件。有勾的 Feature
  修訂計畫後，還需要未跨新邊界的紀錄或一次新審查。規劃關閉的 Epic 可直接進入實作。

**把 Epic 拆成子項目，再指派出去。** 這是「session 只在使用者訊息要求時才建立看板項目」和「只有使用者
能指派項目」的唯一例外：使用者把 Epic 指派給你，這就是拆分它的授權。等審查過的計畫讓 Epic 進入
`implementing` 之後，如果其中某些部分交給其他 Session 做比較好，就在它底下建立 Feature 或 Issue 項目並指派：

```
clawdline item child <epic id> --kind feature|issue --title "…" [--step "…"]… \
  [--description-file f | 描述從 stdin] [--deploy policy] \
  [--assign-terminal <terminal id> | --assign-new [--assistant claude|codex] [--model m] [--persona <id>]]
clawdline item assign <child id> (--terminal <terminal id> | --new [--assistant a] [--model m] [--persona <id>])
```

- terminal id 在 session 通訊錄 `GET /v1/orchestrator/sessions` 裡（`clawdline guide send`）；那個
  Session 必須在 Epic 的 Project 裡工作。也可以把子項目指派給自己；`--assign-new` 會開一個新 Session，
  它的 Root 指派會寫明所屬的 Epic。沒給 `--assign` 旗標，子項目就留在未指派，等使用者指派。
- `item child` 會先讀 Epic 的版本，送出前印出 Idempotency-Key（`--key` 重試同一筆寫入），做完印出子項目。
  它就是 `POST /v1/work/v2/agent/items/<epic id>/children`，body 是 `{"expected_version", "session_id",
  "kind", "title", "description", "steps"?, "deployment_policy"?, "assign"?: {"mode": "existing_session",
  "terminal_id"} | {"mode": "new_session", "assistant"?, "model"?, "persona"?}}`，回 `201` 與
  `{"item", "assigned", "assignment_error"?: {"code", "message"}}`。子項目在 Epic 的 Project 裡，帶著
  `parent_id`（那個 Epic），卡片上會寫明是 Epic 的負責 Session 建立的。steps 是你給的 `--step`；沒給的話，
  指派時從描述的清單產生。
- 子項目先建立、再指派。指派失敗時子項目**會留下來、維持未指派**，回應裡的 `assignment_error` 帶著指派的
  錯誤碼（`session_unavailable`、`project_mismatch`、`assignment_failed`……），指令以 1 結束：用
  `item assign` 再指派一次，或留給使用者。
- `item assign` 就是 `POST /v1/work/v2/agent/items/<child id>/assign`，body 是 `{"expected_version",
  "session_id", "mode", "terminal_id"? | "assistant"?, "model"?, "persona"?}`；它把你 Epic 底下一個未結束的子項目
  移給另一個 Session，跟使用者自己選的指派是同一種。
- 拒絕，都不會寫入任何東西：`not_epic_owner`（你不是這個 Epic 的負責 Session）、`parent_not_epic`
  （上層不是 Epic）、`epic_not_planned`（Epic 還沒進 `implementing`：子項目要出自審查過的計畫）、
  `item_terminal`（Epic 已結束）、`child_kind_not_allowed`（只能是 `feature` 或 `issue`）、
  `epic_children_full`（一個 Epic 最多 32 個子項目，含已結束的）、`not_epic_child`（`item assign`
  的對象不是任何 Epic 的子項目——那要由使用者指派，除非他的訊息要你指派：`clawdline guide board`）、`invalid_assignment`、`version_conflict`、
  `persona_not_applicable`（422：既有 Session 帶了角色）、`unknown_persona`（400：清單裡沒有這個 id）。
- **角色（persona）**是新 Session 開啟時帶著的一個角色：加進它 system prompt 的一段文字，讓它整段對話都照
  那個角色的方式做事。只用在新 Session（`--assign-new`、`--new`、`dispatch`）；既有 Session 維持開啟時的
  角色。預設沒有。角色絕不凌駕 `CLAUDE.md`／`AGENTS.md`、brief、`CHILD.md` 或這份協定。
  `GET /v1/personas` 列出全部；這些 id（每筆的 `teams` 列出它所屬的每個團隊，同一個角色可以在好幾個團隊）：
  - `architect`——規劃 Epic；
  - `backend`——daemon、API 或 store 的 Feature；
  - `frontend`——console 或手機版面的 Feature；
  - `minimal-change`——Issue：站得住的最小修正；
  - `code-reviewer`——審查與 `plan_review` 類的 child；
  - `reality-checker`——驗證：先有證據才說「會動」；
  - `security`——動到權限、配對或 Cloud 的工作；
  - `technical-writer`——文件與 guide；
  - 行銷團隊，用在部落格、網站或文件 repository：`seo`（頁面與 metadata）、`content-writer`（在檔案裡寫文章）、
    `ai-search`（讓 AI 搜尋引用的頁面）、`social-media`、`instagram`、`email`（電子報）、`growth`（可量測的
    實驗）與 `pr`（對外公告）。
  - 產品、品質與維運團隊：`product-manager`、`sprint-prioritizer`、`feedback-synthesizer`、`trend-researcher`、
    `ux-researcher`；`test-automation`、`accessibility`、`performance`、`api-tester`、`evidence-collector`
    （依證據逐項判 PASS／FAIL）；`sre`、`devops`、`incident-commander`、`finops` 與 `secrets`。
  - 設計與商業營運團隊：`ui-designer`（照專案設計系統做畫面）、`ux-architect`（流程與版面結構）、
    `brand-guardian`（品牌一致性）、`ui-finish-gate`（上線前的畫面把關）、`image-prompt`（生圖提示詞）、
    `pricing`、`customer-success`、`support`（回覆草稿）、`analytics`（用真實資料回答）、`devrel`（跑得起來的
    範例）與 `privacy`（個資檢查；不是法律意見）。
  - `zero-review-lead`——負責從零重新檢驗既有功能或流程的審查 Epic：規劃各角色審查面向，以同一份事實包派唯讀
    審查 child，再把證據整理成目標設計；附帶的 skill 是 `zero-based-review`。
- **Epic 會改變人看得到的體驗時，必須增加獨立 UX／產品審查。** 計畫要先判斷是否改到人看得到的介面、
  使用者旅程或產品政策。若有，進到 `merging` 前至少派一個唯讀專家 child；版面、互動與端到端產品流程預設用
  `ux-architect`：

  ```
  clawdline dispatch --kind review --work-id <epic id> --claims "" --persona ux-architect --permission-mode ask …
  ```

  brief 要指名整合後的 candidate，要求桌面與最小支援手機寬度、鍵盤與螢幕閱讀器、流程死路、產品合理性、
  嚴重度及具體修正建議的證據；無法取得證據時必須 **mark unverified and say why**。主要風險是政策與範圍而非
  版面時可改派 `product-manager`；另外需要獨立上線前視覺把關時再加 `ui-finish-gate`。所有阻擋 finding 都要
  處理，並把 task id、verdict 與處置寫進 Epic 的驗證證據或結案報告。若完全沒有面向人的影響，就在計畫寫明
  原因，不要增加審查儀式。記下這次審查涵蓋的範圍；整合後的 Epic 通常只對真正相關的專家各派一次，
  不要把 UX、品牌、安全等角色當成固定清單反覆派工。文案、間距、測試或已審範圍內 finding 的小修正，
  由主理人做針對性檢查並自行收尾。只有後續變更明顯改動使用者旅程、產品政策、品牌方向、安全邊界，
  或產生先前審查未涵蓋的重大風險，才說明新增的邊界並只重派對應專家。這份專家審查不能取代本輪
  planning gate 或 verification gate 對確切 candidate 的 checker PASS。
- **每個子項目合併後，Epic 仍由你負責追到結案。** 立即重讀該子項目與 `clawdline item steps <child id>`，
  確認所有步驟已完成；不能停在 `merging`。由子項目的負責 Session 以已同時在本機目標分支與
  `origin/main` 的確切 commit 附上 landing receipt，再依 deployment policy 附部署證據或不需部署理由，
  依序推進 `deploying` → `done`。若子項目由你持有，就自己完成；若由其他 Session 持有，就立即追辦或依
  授權改派，不能冒用其身分（`not_item_owner`）。有 broker completion 通知時接著 ACK，分類 worktree
  殘留，只清除已證明與 landing 相同（landed-identical）或屬於本任務的暫存內容；未 landing、混合或未知的內容保留並指明
  下一位負責者。所有子項目都 `done` 或 `cancelled` 後才可宣告上層 Epic 完成；否則
  `epic_children_open` 會寫明還有幾個未結束。除了 Epic 的子項目，不要建立其他看板項目。
- **子項目完成不等於獨立 Feature Root 的 Session 已關閉。** 對每個由 Epic `--assign-new` 開出的
  Root，用 `GET /v1/sessions` 的 `epic_parent.epic_id` 核對所屬 Epic、Session 身分及
  `closeability`；不要只靠標題或終端機排列猜測，也不要把 `clawdline session report` 當成關閉。
  子項目 `done` 後立即請該 Root 負責人清點自己的未完成 task、landing、通知、待辦和工作樹，依
  `clawdline` 關閉流程完成報告。只有目前的 daemon 提供正式入口時才能建立關閉證明；已退役 Swift
  的路由不算可用入口。若證明或受保護的關閉操作尚未實作，記錄產品阻礙與下一位負責者，並保留
  Session。只有 `closeability.state=safe`、身分與
  工作均可核對時，才經支援的 Session 關閉操作結束它，再重讀清單確認已消失；不要用
  `clawdline close <terminal id>` 繞過判定。`blocked` 就追具名責任人完成義務，`unknown`
  （例如 `terminal_unreadable`）就保留 Session、記錄缺少的證據與下一位處理者；不可強制關閉、
  封存或宣稱清完。宣告 Epic 收尾前，逐一列出這些 Session 的關閉結果或具名阻礙；
  Board 的 `done` 不會代替這項清點。

## 11. 協調

**整台機器的 coordinator（"Clawdfather"）。** 它在 daemon 管理的獨立機器工作區處理 Session 報告與受控機器作業，絕不修改任何 Project 的程式碼，包含 Clawdline。使用者明確要求工程工作時，先用 `clawdline item add --project … --assign-new` 建立目標 Project 的看板項目，再交給該 Project 的 Session；沒有建立項目的明確要求時，先送提案給使用者決定。接手的看板負責人負責拆解、派工、驗證與落地。工作區是工作流程邊界，不是檔案系統沙盒。新綁定必須來自該工作區，既有綁定仍可讀取。請從控制台的 Clawdfather 入口開啟 Session，再以對話 ID 登記角色。產品邊界見 `docs/clawdfather-role.md`。
在新 Session 裡執行 `clawdline coordinator bind`，用自己的對話 ID 登記；只有舊綁定已被證實離線時才會換綁，在線或讀不到狀態都會拒絕。
`GET /v1/orchestrator/coordinator` 查看這個角色；
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

**編譯或跑測試套件時，用 `clawdline heavy -- <指令>` 包起來，不要直接跑。** 它會先排 `heavy_compile`，等機器有足夠的可用記憶體（機器的四分之一，最多 1 GB，且記憶體等待不超過 10%），再以較低優先權執行；在 Linux 上，記憶體真的不夠時，核心會先砍它而不是互動中的 session。執行期間續租，結束後釋放，並保留指令本身的 exit code。它從不拒絕執行：daemon 沒回應、被拒絕、或超過 `--max-wait`（預設 30 分鐘）都會照跑，並在 stderr 說明。`heavy` 裡面再呼叫 `heavy` 會直接執行。`--min-available 1500M` 可要求更多記憶體，`--no-slot` 只檢查記憶體。repository 裡有 `tools/heavy.sh` 的話，用 `tools/heavy.sh <指令>` 就會自動找到執行檔。

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
