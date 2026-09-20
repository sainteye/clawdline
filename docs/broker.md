# Broker：Go 版接手派工（第一波）

> 目標：舊 app 退役。**寫這份文件的時候（2026-09-18）它還扮演這台機器的 broker**——每一個 agent session
> 的派工、落地、訊息、收據都走它的 `/v1/orchestrator/*`（`Sources/Orchestrator.swift` 10,745 行）。
> 這一波只做「一個 root 從派工到收工」那條路，做完就能自己跑一次完整迴圈。
>
> **2026-09-19 起舊 app 已經停掉，7717 沒有人在聽，派工全部走這個 daemon 的 7727。** 下面凡是寫成
> 現在式的「舊 app 在扮演 broker」都是當時的事實，留著是為了看清楚這一波要接手的是什麼。

## 這一波有什麼

| 路由 | 認證 | 程式 |
|---|---|---|
| `GET /v1/orchestrator/inventory?project=` | orchestrator token（或已配對裝置） | `orchestrator/inventory.go`、`wire.go` |
| `POST /v1/orchestrator/tasks` | orchestrator token | `dispatch.go`、`draft.go`、`brief.go` |
| `GET /v1/orchestrator/tasks`、`/tasks/<id>` | 同 inventory | `transport/http/tasks.go`（併入 console 的列表）、`orchestrator.go` |
| `POST /tasks/<id>/progress`、`/complete`、`/notify`、`GET /tasks/<id>/inflight` | 該 task 的 secret | `lifecycle.go` |
| `POST /tasks/<id>/landing` | `pending`／`abandoned`：secret 或 token；`landed`／`nothing_to_land`：只有 token | `lifecycle.go` |
| `POST /tasks/<id>/completion/ack` | orchestrator token | `lifecycle.go` |
| `GET /v1/orchestrator/inflight?project=` | 同 inventory | `lifecycle.go` |
| `POST /v1/orchestrator/messages` | orchestrator token＋`Idempotency-Key` | `messages.go` |
| `GET /v1/orchestrator/whoami?conversation_id=` | orchestrator token | `messages.go` |
| `POST /v1/orchestrator/sessions/<terminal>/complete` | orchestrator token | `messages.go` |
| `GET /v1/orchestrator/sessions/<conversation>/todos?state=outstanding\|closed\|all&cursor=` | orchestrator token | `todos.go`（T2：Session 待辦，只讀；terminal id 回 `409 session_id_is_terminal`） |
| 完成通知（`<clawdline-notice>`）與重送 | — | `notice.go`、`watch.go` |

**Session 待辦（T2，design-decisions D36、board-redesign §5.2）**：每次派工替 root 開一筆 `dispatch:<task id>`，
與 task 同一筆交易寫入；之後每一筆 broker 事實（結算、landing）在它自己的交易裡推進待辦——`landed`／`nothing_to_land`／
`abandoned`／沒有落地義務的結束→`done`，寫不進待辦就連事實一起不寫。root 被讀數**確定**不在→`handed_off`，
回來→`open`，移交 24 小時仍確定不在→`dropped`；讀不到（unknown）什麼都不動。daemon 啟動的第一個 beat 補齊沒有待辦的
task、跟上還沒結束的待辦。不問人、不推播、不進看板；升級訊號（`cross_session`／`long_lived`／`repeated_failure`）只放在
回應裡給 T4 用。派工的 `task.json` 可帶 `work_id`（小寫 UUID），respawn 會沿用。

錯誤碼與文字照舊版，包括信封：`{"error":{"code","message","request_id", …extra}}`，**extra 放在 `error` 裡面**。
`stale_inventory` 因此能把整份 inventory 帶回來，重送只要一次來回。

規格來源：舊版路由與 handler（唯讀），加上這台機器真正在用的形狀——本任務自己的 `task.json`、`CHILD.md`，
以及 2026-09-18 當時對 7717 的 `/inflight`（只用本任務自己的 secret）。那個 port 從 2026-09-19 起
沒有人在聽，同一條 `/inflight` 現在問 7727。

## 刻意與舊版不同的地方

| | 舊版 | Go 版 | 為什麼 |
|---|---|---|---|
| task 目錄 | `/tmp/.clawdline/<id>` | `<CLAWDLINE_NEXT_DIR>/tasks/<id>` | 兩個 broker 寫同一個 task 目錄，就是 plan.md §4 要避免的「兩個 app 搶同一份狀態」。inventory 多回一個 `task_root`，caller 才知道 task.json 要寫在哪 |
| worktree 位置 | `~/Library/Application Support/Clawdline/worktrees/<slug>/<id>` | `<CLAWDLINE_NEXT_DIR>/worktrees/<slug>/<id>` | 兩個 broker 共用一個 worktrees 根目錄會互相 prune。slug 規則照舊 |
| task 紀錄 | `~/.config/clawdline/orchestrator.json` | 自己的 SQLite `broker_tasks` | 新派的 task 完全存在新版自己的地方；舊 store 只讀、只用來畫舊 app 的 task |
| tmux 的 child | （舊版開 iTerm 分頁） | 每個 child 一個新的 detached session，名字 `clawdline-task-<id 前 8 碼>` | `new-window` 不指定 session 會落在 tmux 最後用過的那個——這台機器上是有人正在用的 `clawdline`（25 個視窗）。broker 不可以把 child 放到別人的鍵盤前 |
| 派工前 root 必須在 | 會解析 | 會解析（`root_unresolved`、`conversation_ambiguous`） | 一樣。root 不在，完成通知就沒有收件人 |
| 4 分鐘時鐘 | 沒有 progress note 就 `spawn_failed` | 分頁還活著就不判 `spawn_failed`；分頁不見或卡在對話框才判 | 見下面「實測抓到的三件事」第二條 |
| `serialize`、`graph`、`attach_session` | 支援 | **明確拒絕**（`bad_task`，說出欄位名） | 還沒做。默默忽略 `serialize` 會把該等待的 task 直接開起來 |
| `reasoning_effort` | 支援 | 支援（codex 限定，`high`／`xhigh`，接成 `--config model_reasoning_effort=…`） | 2026-09-20 補上。之前是具名拒絕，而那句拒絕請人「改用 Swift app」——那個 app 已經退役，等於沒有出路；同時 schedule 那邊早就會驗、會存這個欄位，存得下去卻發不出來 |

## 實測抓到的三件事（都有測試）

1. **游標不是 composer。** 第一次派的 child 開在新 checkout，Claude Code 畫出 workspace trust 對話框，
   broker 看到「有 assistant 在 tty 上」就打字加 Enter——那個 Enter 回答了對話框，而游標在「No, exit」。
   child 什麼都沒讀就死了。現在要看到**有框線的輸入列**才打字；沒有框線的 `❯` 是選單的反白，不打
   （`composer.go`，`TestADialogIsNeverTypedInto`）。規則看結構，不列舉對話框的字，所以沒見過的對話框也擋得住。
2. **安靜不是失敗。** 時鐘原本把「4 分鐘沒 progress note」當 `spawn_failed`，但 briefing 明說**不要**送心跳，
   於是正常工作的 child 被記成沒開起來（B1 第一次實跑就撞到一次）。現在問分頁本身：不見了、或卡在對話框，才是 spawn 失敗；
   活著就交給 task 自己的 timeout（`spawnVerdict`，`TestSilenceFromALiveChildIsNotASpawnFailure`）。
   另外，打完 briefing 後分頁開始跑一個 turn，就升成 `briefed`——比舊版讀 transcript 找 task 標記弱，註解裡寫明了。
3. **慢步驟不能把過去寫回去。** 通知 pump 打字要幾秒，打完把打字前的副本存回去，蓋掉中間到的 ACK；
   `/complete` 與 beat 收 result.json 可以各結算一次、發兩個 notice id；派工還在打 briefing 時把 `spawning`
   存回去蓋掉剛被證明的 `briefed`。現在所有修改走 `mutate`：鎖內重讀、以**當下**的紀錄決定
   （`race_test.go` 三個測試，另一個寫入者直接在慢步驟裡面發生，不靠時序運氣）。

## 還沒做（這一波刻意不做，或做不到）

- `detached-tasks`、`handoffs`、`root-assignments`、`respawn`、`landing-queue`、`graphs`、`waits`、`coordinator/*`。
- `/notify` 沒有推播：這個 daemon 還沒有 WebPush（另一個 task 在做）。所以一律 `409 not_subscribed`——
  是事實，不是 stub。`Broker.Notify` 是接縫。
- messages 的 `Idempotency-Key` **只檢查有沒有，不存也不重播**；`images` 不支援。
- 打字前不清 composer 裡的草稿（舊版 `TerminalComposerClear`）。實測看到：relay 的訊息把 child composer 裡一段
  未送出的草稿一起送了出去。
- `result.json.ready` 的收養沒有舊版的 30 秒觀察窗：marker 與 tmp 的 sha256 一致就收。
- whoami 只讀一次 inventory，沒有舊版的「兩次獨立讀取一致」。
- `sessions/<terminal>/complete` 不檢查 session 是否 `working`，也不去重（每次 `created:true`）。
- landing 的 correction（`corrected_from`）與 `namesSameCommit` 前綴比對沒做；已落地的 target 不同一律 `landing_conflict`。
- iTerm 分頁那條路有寫、**沒在這台機器上跑過**（驗收用 tmux，以免在使用者的 iTerm 開分頁）。
- 契約：inventory 本體是 map（原因見 `wire.go`），其他 body 都是 `api/v1/orchestrator.schema.json` 生成的型別。

## 自己跑一次

```bash
go build -o /tmp/x/clawdline ./cmd/clawdline
mkdir -p /tmp/x/dir && echo '{"terminal":"tmux"}' > /tmp/x/dir/config.json
CLAWDLINE_NEXT_PORT=7794 CLAWDLINE_NEXT_DIR=/tmp/x/dir CLAWDLINE_NEXT_STANDALONE=1 \
  CLAWDLINE_NEXT_OWN_SESSIONS=1 /tmp/x/clawdline serve
OT=$(cat /tmp/x/dir/orchestrator-token)
# 1. whoami → 自己的 conversation id；2. inventory → generation 與 task_root
# 3. 把 task.json 寫到 <task_root>/<id>/，再 POST /v1/orchestrator/tasks {task_id, secret, inventory_generation}
```

第一次在一個新 repo 用 worktree 隔離時，Claude Code 會問那個 repo 要不要信任（worktree 的信任跟著主 repo）。
那是人的決定，broker 不會替人按；先在那個 repo 開一次 claude 回答它。
