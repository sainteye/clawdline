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
| `POST /tasks/<id>/landing` | `pending`／`abandoned`：secret 或 token；`landed`／`incorporated`／`nothing_to_land`：只有 token | `lifecycle.go` |
| `POST /tasks/<id>/completion/ack` | orchestrator token | `lifecycle.go` |
| `GET /v1/orchestrator/inflight?project=` | 同 inventory | `lifecycle.go` |
| `POST /v1/orchestrator/messages` | orchestrator token＋`Idempotency-Key` | `messages.go` |
| `GET /v1/orchestrator/whoami?conversation_id=` | orchestrator token | `messages.go` |
| `POST /v1/orchestrator/sessions/<terminal>/complete` | orchestrator token | `messages.go` |
| `GET /v1/orchestrator/sessions/<conversation>/todos?state=outstanding\|closed\|all&cursor=` | orchestrator token | `todos.go`（T2：Session 待辦，只讀；terminal id 回 `409 session_id_is_terminal`） |
| 完成通知（`<clawdline-notice>`）與重送 | — | `notice.go`、`watch.go` |

**Session 待辦（T2，design-decisions D36、board-redesign §5.2）**：每次派工替 root 開一筆 `dispatch:<task id>`，
與 task 同一筆交易寫入；之後每一筆 broker 事實（結算、landing）在它自己的交易裡推進待辦——`landed`／`incorporated`／`nothing_to_land`／
`abandoned`／沒有落地義務的結束→`done`，寫不進待辦就連事實一起不寫。root 被讀數**確定**不在→`handed_off`，
回來→`open`，移交 24 小時仍確定不在→`dropped`；讀不到（unknown）什麼都不動。daemon 啟動的第一個 beat 補齊沒有待辦的
task、跟上還沒結束的待辦。不問人、不推播、不進看板；升級訊號（`cross_session`／`long_lived`／`repeated_failure`）只放在
回應裡給 T4 用。派工的 `task.json` 可帶 `work_id`（小寫 UUID），respawn 會沿用。

**A finished child's notice reaches a root that was busy (`notice.go`, 2026-09-25).** A root showed a question from 19:54
to 22:08 while the person was away; its child finished at 20:07. The pump rightly typed nothing into the menu
(`root_choosing`), but every ten minutes of holding spent an attempt, so the notice was a dead letter at 21:27 — eight
attempts, none of them made — and when the person answered, the root carried on not knowing. It found out two and a half
hours later by asking. Three things changed:

- **A menu is a hold, not an attempt.** While the root shows something waiting to be answered the notice waits for free,
  with `root_choosing` and when the hold began in its `last_error`, up to `maxChoosingHold` (12 hours). At that ceiling
  it goes to dead letter in one step, saying so, and the person is pushed. A busy lane or an occupied composer still
  spends an attempt after ten minutes (`maxNoticeHold`), and other failures spend one each, as before.
- **The pull path carries it.** A root's own `GET /v1/work/v2/agent/session-todos/<conversation>` — which the guide
  has it read at every turn boundary — answers `unacknowledged_completions`: each completion of that root nobody
  acknowledged, pending, delivered or dead, with task id, title, state, `result_path`, `notice_id`, `notice_state`,
  `last_error` and `ack_path`. A ledger that cannot be read says `unacknowledged_completions_unknown` rather than an
  empty list. `clawdline session report` prints them on stderr after the receipt, as it does open to-dos. An ACK
  (the route, `clawdline task ack`, or a landing the root records) takes it off both.
- **A dead letter is typed once more when its root reads idle.** Idle is the one moment a line costs nothing: no
  menu, no turn to interrupt, an empty composer. The beat types it there once (`task.completion.retyped`; the attempt
  count past the limit is the durable mark it was spent), the notice stays a dead letter — no second push — and it is
  not typed again. Only dead letters at most `maxRetypeAge` (24 hours) old qualify; older ones wait for a person's
  `reconcile`.

## Landing ledger 的字

Landing 是 task 的終止狀態以外、唯一回答「這份交付後來怎麼了」的帳本。它目前有五個字：

| 狀態 | 意思 | 誰可以寫 | 機器驗什麼 |
|---|---|---|---|
| `pending` | 還欠著一個處置 | task secret 或 orchestrator token | 只記義務；不宣稱 Git 現實 |
| `landed` | 這個 task 自己的 delivery commit 已在 target | 只有 orchestrator token | target 與 commit 都能解析；commit 在 target 上；delivery head 是 commit 的祖先；delivery 不是 dispatch base |
| `incorporated` | 這個 task 的非祖先 delivery 由另一個 task 的已驗證 landing 整合進 target | 只有 orchestrator token | 原 delivery 非空且可解析、不是 carrier commit 的祖先；`carrier_task` 是同一 repo 的可讀 task；它的 landing 是 `landed`；target 與 exact commit 都相同；`note` 留下語意判斷 |
| `nothing_to_land` | task 沒有寫出要落地的東西 | 只有 orchestrator token | branch 與 checkout 的既有證據都沒有顯示寫入 |
| `abandoned` | 這份交付不採用 | task secret 或 orchestrator token | 這是處置決定，不冒充 Git 證明 |

**`landed` the broker records by itself (`landing_detect.go`, 2026-09-25).** A landing used to be settled only when
somebody asked: a root that forgot, or a root refused `409 close_blocked` when it tried to close. In the root transcripts
read that day one root was refused 16 times, the person pressed close six times, settling took 48 calls, and the code
behind its 19 pending landings had long been merged; only the record was missing. So the beat asks git: on the first pass
and every 12th after it (about a minute at the 5-second tick), at most 16 pending landings a look (`landingDetectLimit`),
each look starting after the last task the previous one examined. It reads only the rows that still owe a landing (`store.BrokerTasksOwingLanding`, on the expression index `broker_tasks_landing`), so its cost is what is owed, not the history (G33). A task qualifies when it has finished, its landing is
still `pending`, and it has a delivery branch and base of its own. When its branch head is an ancestor of the target, the
broker records `landed` through **the route's own gate** (`land()`, with the orchestrator's credential), at the target's
current head, with a note saying the broker recorded it; whatever the route would refuse is not written. It does not call
the exported `Land()`: a landing the root records says the root read the delivery and closes its completion notice, and
the broker noticing a merge is not the root reading anything.

The target is the record's (D19). A pending landing opens without one, and the broker does **not** fill it with `HEAD`.
It names one only when git proves there is a single candidate: among the local branches whose history holds the delivery
head, leaving out the broker's own `clawdline/task/*` branches and every branch checked out in a linked worktree (an
integration still in progress), exactly one remains — and it is the branch the primary checkout has out, the line work
lands on; a parked branch nobody has checked out is not named even when it is the only holder. None, or two or more,
or one that is not the primary checkout's, is a root's decision. Left alone, and not
counted as failures: an empty delivery (head under its base), a cherry-picked one (not an ancestor), a branch that is
gone. A repository git cannot read, or that takes longer than 10 seconds for one task, costs that task this look and
nothing else; the log says it once per task per distinct reason. A second look changes nothing: a `landed` task is no
longer on the list.

`incorporated` 刻意不假裝 Git 會讀程式語意。它擋得住不存在的 carrier、尚未落地的 carrier、別的 repo、別的 target、
別的 commit、空 delivery，以及其實可以正常記成 `landed` 的祖先關係；它擋不住「整合者解衝突時漏掉一個行為」。後者不是
tree equality 能回答的問題：語意整合本來就會讓兩棵樹不同。判斷依據必須留在 `note`，code review 與測試仍負責內容正確性。
帳本證明的是那個判斷綁在**哪一份原 delivery**以及**哪一筆已驗證落地**上，不是讓人裸寫一句「應該有進去」。
若 carrier 的 landing 日後被更正，舊 landing 仍由 carrier 的 `corrected_from` 與 `landing.corrected` event 保留；
`incorporated` 自己也保存當時驗過的 carrier commit、target commit、原 delivery head 與 base，不會只剩一個會漂移的 task id。

目前看得到、但還沒有新增狀態的形狀：

- 一份 delivery 被拆到兩個以上的 landing commit；一個 `commit` 與一個 `carrier_task` 表達不了完整集合。
- delivery 後來被另一份工作刻意取代，舊內容不應仍被說成在 target；這跟 `abandoned` 的「沒有出貨」不同。
- delivery branch 已被清掉，但 target 上仍有內容證據；現在缺少可在 branch 消失後重建 delivery 身分的證明。

這次只加 `incorporated`：它對應已經發生、而且現有兩筆 task 與 Git commit 都能留下可檢查連結的形狀。其餘形狀需要不同的
證據模型，不能共用一個模糊的「差不多有落地」。

### 下一次要替 ledger 加一個字

1. 在 `internal/app/orchestrator/record.go` 加常數與該狀態專屬的 durable evidence；同步決定 replay／correction 的 `sameAs` 身分。
2. 在 `lifecycle.go` 關閉 request schema、認證與 transition；把證明放在 `landing.go`，所有 unknown 都拒絕，不降級成 false。
3. 逐一走過 projection：inventory 是否 settled、session 待辦與 board 是否關閉、graph、timeline、舊 task projection，以及 reclaim
   是否真的有足夠證據刪東西。這些讀者不一定該把新字當 `landed`；要各自說明。
4. 改 `api/v1/orchestrator.schema.json`，執行 `go run ./tools/contract-gen` 同時產生 Go 與 TypeScript contract；不要手改 generated file。
5. 在非 legacy 的 console 加人能看懂的字。`web/console/src/legacy/js/**` 與 copied string catalog 是 byte-for-byte 資產，不在這條路上改。
6. 測試要一正一反地釘住每一個 proof gate、wire evidence、derived closing 行為與 replay；最後跑 `contract-gen -check` 和 web checks。

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

## Quota evidence at dispatch (2026-09-21)

`POST /v1/orchestrator/tasks` reads the same five-second, file-only account readings as
`GET /v1/orchestrator/assistants`. The accepted task keeps that moment under `assistant_quota`:
the broker's `read_at`, both assistants' availability, provider observation time, age, freshness
line, stale flag, windows, and the reason for an unknown reading. This is historical evidence; a
later task read never replaces it with the accounts' current state.

A selected `low` or `exhausted` assistant produces a non-blocking response warning. When another
installed assistant has a better known availability, `assistant_quota_choice` names both readings,
including their numbers and observation times. A selected or installed assistant whose reading is
unknown produces `assistant_quota_unknown`; failure of the whole reader produces
`assistant_quota_unreadable` and is recorded with the task. A retry of an already accepted
dispatch rebuilds these warnings from the stored snapshot rather than measuring the accounts
again.

Quota is not an admission gate. An explicit assistant may have model, capability, or continuity
reasons that the quota reading cannot see, so every warning says that the dispatch continued. This
keeps the guide's contract — nothing refuses a dispatch for quota — true.

`assistant: "auto"` is deliberately not admitted. The providers expose different windows, and a
reading may be stale or unknown; “most remaining” therefore has no stable ordering without a new
policy for incomparable windows, ties, missing evidence, capability, and model requirements.
Silently turning any of those cases into one provider would recreate the missing-evidence defect.
The warning keeps the caller's explicit choice while putting the machine's contrary evidence at
the decision boundary.

## A child that stalls after its briefing (2026-09-25)

Measured on 2026-09-25: the broker typed a child its first line, the model answered a lone `<br>`
in one second and ended its turn, and the tab sat at an empty composer for forty minutes. No
`/accepted` came, so the task stayed `spawning`; the tab was there and quiet, which the spawn clock
(`spawnVerdict`) deliberately decides nothing about; and nothing told the root, which would have
learned at the 240-minute timeout. One line typed into the tab got it working at once.

The beat now watches for that shape and only that shape (`internal/app/orchestrator/stall.go`):

- **Who is watched.** A task still `spawning` whose briefing was typed — the tab is recorded and
  the task is not `unbriefed` — and which has not signed (`/accepted` or `accepted.json`). A
  signed progress note moves a task to `briefed`, so it is not watched either.
- **Idle, on positive evidence only.** Each pass reads the child's own screen. Idle means the tab
  is in the reading, the assistant's prompt is drawn, the composer is empty and nothing on the
  screen is a menu. A live working line, a dialog, a draft or a screen that could not be read
  starts the interval again. A child reading files draws a working line, so a slow child is never
  mistaken for one that stopped. The interval is kept in memory; a restart forgets it and only
  ever waits longer.
- **One nudge.** After `stallIdleLimit` (5 minutes) of unbroken idle readings the child is typed
  one line naming its `CHILD.md` path — never the secret, which is already in its context. It goes
  through `b.Type`, the same lane every typed line takes. The nudge is recorded on the task
  (`stall.nudged_at`, event `task.nudged`) **before** it is typed, so a daemon that restarts never
  types a second one; a typing that fails is recorded (`stall.nudge_error`,
  `task.nudge.failed`) and not retried.
- **Then the root is told.** If the child is still unsigned and idle `stallReportLimit`
  (5 minutes) after the nudge, the task ends `spawn_failed` with a verdict that says it stalled
  (`stall.reported_at`, events `task.stalled` then `task.spawn_failed`). The settlement opens the
  ordinary completion notice, typed with `"kind": "task_stalled"` instead of `task_finished`, whose
  line says to respawn it or dispatch again. It is listed in `GET /v1/orchestrator/completions` and in the root's
  `session-todos` `unacknowledged_completions` (with `kind`) until acknowledged. The child's tab
  is closed as every `spawn_failed` tab is.

**Why it ends the task instead of marking it and leaving it running.** There is no cancel route,
and `/respawn` only takes a `spawn_failed` task, so a task left `spawning` with a mark on it would
have given the root nothing to do but wait for the timeout. `spawn_failed` is also the true
reading: nothing the child did was ever signed. A new task state was not added because every
reader of the state — the contract, the console, the board and the to-do lines — would have had to
learn it; the verdict and the notice kind carry the difference instead. The cost is on the other
side: a child that wakes after it was reported finds its task over (`/accepted` answers
`not_live`), and its root's respawn is the one that counts.

A child showing a menu is not typed at by this path; the spawn clock already ends a tab holding a
dialog at four minutes, before the nudge's interval is up.

## What the protocol costs a child and a root (2026-09-26)

Measured on 2026-09-26 over a week of transcripts, cost-weighted with cache re-reads: a child spent
about 6% of its cost on protocol and a root about 10%. The parts cut, and where each went:

- **A child reads one file.** `CHILD.md` carries the task itself — title, kind, timeout, declared
  writes, deliverables and the instructions, fenced so their own headings and fences stay theirs
  (`brief.go`, `writeTask`). It was reading `task.json` after `CHILD.md`, 2.4 times a session on
  average (0.9% of its cost). `task.json` stays the daemon's source, rewritten from the admitted
  record at admission, and the briefing names it once as the copy the child need not read.
- **Signing is one command.** `clawdline task accept <task dir>` posts `/accepted` with the task
  secret from `CLAWDLINE_TASK_SECRET` or stdin — never argv, never printed — and leaves
  `accepted.json` when the daemon cannot be reached, as `task finish` does for its result. The
  curl recipe and the fallback paragraph (0.6%) are gone from the briefing; the route stays.
- **A root reads a compact view.** `clawdline task show <id>` prints state, verdict, the whole
  summary, leftover titles, the symbol count, verification, landing and checkout; `--json` is the
  daemon's whole answer. A root was reading the child's whole `result.json` (2.1%) and `task.json`
  (1.5%).
- **The completion notice is a pointer.** Its `body` says the task, how it ended, the facts that
  are this delivery's alone and the two commands; what to do about a leftover or a landing moved to
  the guide's §5 and §6. Every JSON key it carried is still there.

Sizes, from the same record on both sides (this change's own task, 3,072 bytes of instructions):

| | before | after |
|---|---|---|
| What a child reads to start | `CHILD.md` 9,061 + `task.json` 4,000 (read 2.4× on average) | `CHILD.md` 12,059 |
| Completion notice, plain success | 910 (body 337) | 825 (body 252) |
| Completion notice, empty branch and two leftovers | 1,753 (body 1,134) | 1,100 (body 513) |

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
