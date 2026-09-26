# 排程：建立、發射、遷移（2026-09-18）

退役判準 A8「排程真的會在新 daemon 上發射」、A9「五個營運排程搬過去而且各跑成功一次」（`cutover.md` §7）
的 Go 端。規格來源是舊 app：`~/code/clawdline/docs/schedules.md`、`Sources/Schedules.swift`、
`Sources/ScheduleService.swift`、`Sources/Orchestrator.swift`（排程那段）、`Sources/ScheduleWebhook.swift`、
`Resources/web/app/js/{view/schedules.js,net/schedules.js,input/schedule.js,input/schedule-history.js}`。

## 模型：就是舊版的檔案

先前 Go 版的排程是 `every 1h`／`at 09:00` 的間隔模型，舊 app 沒有這種東西。現在整個換成舊版的檔案格式，
欄位一個不改：`clawdline_schedule`、`schedule_id`、`title`、`when {at, days|on}`、`task {…}`、`enabled`、
`close_tab`、`catch_up_hours`、`notify_on_failure`、`created_at`、`when_changed_at`、`fired_at`。
解析器（`internal/domain/schedule`）的每一句拒絕都是舊版 parser 的原句，所以舊檔搬過來是**複製**，不是翻譯。

**存在哪裡**：新 daemon 自己的 `clawdline.sqlite3`（`CLAWDLINE_NEXT_DIR` 底下），表 `schedule_files`，
一列一個檔、`body` 逐位元組就是那個檔。**不寫 `~/.config/clawdline`**。檔案本身說不出的三件事放在旁邊：
`first_seen`（這支 daemon 第一次看見它）、`last_fire`（已經決定過的最近一次）、`last_missed`。
舊的間隔模型用的 `schedules` 表留著不讀（沒有真的資料寫進去過），讓舊 store 照樣開得起來。

## 路由（與舊版相同）

| 路由 | 誰可以 |
|---|---|
| `GET /v1/orchestrator/schedules`、`GET …/schedules/:id` | 已配對裝置（唯讀即可）、本機 orchestrator token |
| `POST …/schedules`、`PATCH …/:id`、`DELETE …/:id` | 可以 send 的裝置＋`Idempotency-Key`；本機 orchestrator token 可直接動「只跑一次」（`when.on`）的排程；a Session carrying `session_id` + `via.run` from the person's latest message to that conversation may relay that explicit instruction to create, change or delete a repeating schedule |
| `POST …/:id/run` | 可以 send 的裝置＋key；或 orchestrator token（不用 key） |
| `POST /v1/orchestrator/schedule-webhooks/bind` | 只有 orchestrator token（Cloud `schedule-webhook-bind-v1` 指令的本機那半） |
| `POST /v1/orchestrator/schedule-imports` | 只有 orchestrator token，**而且 `config.json` 要有 `"schedule_imports_enabled": true`**（預設關；本 daemon 新增，遷移用） |
| `GET /v1/orchestrator/schedule-exports` | 只有 orchestrator token（本 daemon 新增，遷移用） |

`gate.go` 只多一行：`/v1/orchestrator/schedules/…` 的寫入跟清單一樣走「兩扇門」，不再一律要 orchestrator token。

### A Session relays a person's recurring instruction

A repeating schedule cannot be created with the machine-wide orchestrator token alone. Otherwise
any local automation could quietly give itself a permanent wake-up. When a person explicitly asks
a Session in Clawdline to arrange recurring work, the daemon has issued a run for that message. The
Session first reads `GET /v1/orchestrator/sessions/<conversation id>/run`, then sends these two
fields with the ordinary schedule form body:

```json
{
  "session_id": "<conversation id>",
  "via": { "run": "<run id>" }
}
```

The run must still be inside its 24-hour window and must belong to the conversation named by
`session_id`; otherwise the route returns `run_unknown`, `run_expired`, or `run_other_session`.
A missing field, wrong type, or extra key under `via` returns `invalid_user_authorization`. After
verification the proof is removed from the schedule body and retained in the schedule audit as
`actor=user_via_session:<run>` and `session=<conversation>`. The raw request bytes remain the
`Idempotency-Key` receipt digest, so the same key cannot be reused with different evidence.
This is an auditable relay of the person's instruction, not a session-scoped credential: the
orchestrator token remains machine-wide.

The orchestrator token may also read `GET /v1/places` to obtain the form's `place_id`. This opens
only the list, not Session-starting routes such as `/v1/places/:id/start`.

## 時鐘：一分鐘一跳，只問最近那一次

每一跳對每個啟用中的排程，只看**最近一次**應該發生的時間（`LatestFire`），依序問：

1. `fired_at` 有值 → 已用掉（只跑一次的排程）。
2. 比 `created_at` 早 → 那時還沒有這個排程。
3. 比 `when_changed_at` 早 → 那一次是改時間「變出來」的。
4. **比這支 daemon 第一次看見它還早 → 不歸我管**（`plan.md` §3.2 的規則，舊版沒有；見下）。
5. 已經決定過（`last_fire`，持久）或手動執行在它之後 → 不動。
6. 同一排程還有 task 沒結束 → 記為 handled、audit `skipped active`。
7. 在 `catch_up_hours` 窗內 → **跑**；窗外 → 記為 missed、audit、推播（`notify_on_failure`）。

跑之前會**先用 compare-and-set 把這一次寫成已決定**（`ClaimScheduleFire`），也先把這次 run 連到排程，再派工：
派到一半 daemon 死掉，重開不會再派一次；兩個 daemon 指向同一個 state、或計時器與手動執行撞在一起，只有一方拿得到。
只有 `over_capacity` 會把它還回去，讓下一分鐘在窗內重試（舊版規則）。手動執行同理：在某次之後按下的那一次先佔住它，
被拒就還回去。存檔與刪除也走同一條派工 lane（`ScheduleBook.mu`），所以不會在「計時器最後看一眼」與「開分頁」之間把
排程改期或刪掉，只跑一次的排程的 `fired_at` 也只會蓋在仍然指向那一次的檔上。**派工走 broker（W4，`design-decisions.md` D07）**：
排程的 `task` 範本加上 broker 自己的三個鍵（`clawdline_protocol`、`task_id`、`root: {session_id: null, label: <排程標題>}`）
寫成 task.json，交給 `Broker.DispatchScheduled`，跟任何派工走同一道門——同一個 claims 仲裁（排程的 run 與 broker task
互相擋，`409 workspace_busy`，在開任何東西之前）、同一份紀錄、CHILD.md、自己的 secret、`timeout_minutes`（範本沒寫就 30）、
同一個 beat 收 `result.json`。與一般派工只差兩處（照舊版 `dispatch(taskID:secret:schedule:)`）：沒有 root，範本可以不寫 claims
（記成「未宣告」並警告，不當成「什麼都不寫」）。`over_capacity` 與 `terminal_busy`（滿了）把這一次還給時鐘，下一分鐘在窗內重試；
其他拒絕照舊用掉這一次。只跑一次的排程在 session 真的開起來後寫 `fired_at` 進 body。

**重啟**：`last_fire` 在 SQLite，所以重開後同一次不會再跑；停機期間錯過、而且這支 daemon 早就看過的那一次，
重開後第一跳補跑一次——永遠只有最近那一次，不會補一整批。

**第 4 條為什麼要有**：舊版把沒有 `created_at` 的檔當成「從很久以前就在」，在 6 小時補跑窗內，一個被複製、
還原或匯入的檔會在出現的那一分鐘就發射——正是 §3.2 第二次事故換個寫法。所以每一次都還要跟這支 daemon
第一次看見這一列的時間比；這個時間在**讀取時**蓋章，因為會出事的正是沒經過寫入路徑的那些列。

## Migration: moving the old schedules over

```sh
# On the machine taking over. <port> and <state> are the new daemon's port and its CLAWDLINE_NEXT_DIR
# (7727 and ~/.config/clawdline-next unless it was started otherwise). Neither has a default.
mkdir -p /tmp/schedules-copy
cp -p ~/.config/clawdline/schedules/*.json /tmp/schedules-copy/   # a copy; *.json.bak is not taken
# add "schedule_imports_enabled": true to <state>/config.json, and take it out again afterwards
python3 tools/migrate-schedules.py import /tmp/schedules-copy <port> <state>   # each file's result and next firing
python3 tools/migrate-schedules.py verify /tmp/schedules-copy <port> <state>   # all three comparisons must say SAME
```

`tools/migrate-schedules.py` needs Python 3.9 or later and nothing outside its standard library. It
reads the source directory and writes nothing, there or anywhere else. Before anything is sent it
refuses:

- **a source that is not a copy**: a directory that is, or resolves into, `~/.config/clawdline` — in
  any letter case and through any symbolic link, because it is compared by device and inode as well as
  by spelling — and a file in the copy that is a symbolic link, a hard link, not a plain file, or not
  UTF-8 text (a JSON string carries text, so such bytes would arrive changed);
- **a target that is not the new daemon of `<state>`**: `/v1/health` on `<port>` must answer
  `"served_by": "clawdline-go"` before the token leaves the script (the Swift app on 7717 does not), and
  the token is `<state>/orchestrator-token`; a daemon that refuses it keeps its state somewhere else.

A refusal from the daemon is printed in the daemon's own words. Without the switch, for example:

```
migrate-schedules: The daemon refused the import: 403 schedule_imports_disabled: Importing schedule files is switched off. Set "schedule_imports_enabled": true in this daemon's config.json for the migration, and take it out again afterwards.
```

The exit status is 0 when every file is in and the same, 1 when something differs or needs a person
(`exists`, `invalid`, `refused`, a comparison that is not SAME), and 2 when it could not be done or
checked. Titles are never printed, so the output can be pasted into a report.

The import's rules (the daemon's, `ScheduleBook.Import`):

- Every file is stored **byte for byte**, its name being its id. A file that does not parse is stored
  too and listed as an `invalid` row with the parser's sentence, so a migration never loses one without
  saying so; a name that is not `<lower-case UUID>.json` is `refused` and not stored.
- The moment of import is the row's `first_seen`, and its latest occurrence at or before that moment is
  recorded as decided: what the Swift app already ran (or missed) is not run again here and is not
  announced as missed. The next occurrence is the new daemon's.
- The same file again is `unchanged`; the same id with different bytes is `exists` and is not
  overwritten.
- **Why a separate switch**: an import accepts repeating schedules and any `project_dir`, which makes it
  what a hand-written file was for the Swift app — the door the orchestrator token's once-only rule does
  not cover, and every agent session on this machine can read that token. So it is off by default; a
  person turns it on for the migration and off afterwards.

**How `verify` shows the content is the same** — three independent comparisons:

1. **Bytes**: the source file's sha256 = the sha256 of that file in
   `GET /v1/orchestrator/schedule-exports`.
2. **Parsed**: what the daemon's own parser read — `title`, `enabled`, `when`, `close_tab`,
   `catch_up_hours`, `notify_on_failure` and the whole `task` template, from
   `GET /v1/orchestrator/schedules/:id` — equals the source file, with the Swift app's defaults
   (`on_success`, `6`, `true`) for a field the file leaves out. `when.days` is compared as a set, as the
   parser keeps it, and `true` never counts as `1`.
3. **Time**: the daemon's `next_fire` = the next firing the script computes from the source `when` in
   this machine's local time. Run it in the daemon's time zone (`TZ`), or this comparison says DIFFERENT
   for a reason that is not the file.

A file the daemon lists as `invalid` is `INVALID` under parsed, and one it does not hold is `MISSING`.
Files the daemon holds that the source does not have are named and not compared.

Measured on 2026-09-19 with copies of this machine's five schedule files, against a daemon of its own
(its own `CLAWDLINE_NEXT_DIR` and port, and `"orchestrator_enabled": false`, so nothing could fire).
Ids and paths below are replaced with fixtures; the rest is the output as printed.

```
$ python3 tools/migrate-schedules.py import /tmp/schedules-copy 7841 /tmp/state
import  /tmp/schedules-copy -> 127.0.0.1:7841 (/tmp/state)
  c6000001-0000-4000-8000-000000000001.json  imported    next 2026-09-21 09:40 Mon
  c6000002-0000-4000-8000-000000000002.json  imported    next 2026-09-20 12:10 Sun
  c6000003-0000-4000-8000-000000000003.json  imported    next 2026-09-22 10:00 Tue
  c6000004-0000-4000-8000-000000000004.json  imported    next 2026-09-22 10:20 Tue
  c6000005-0000-4000-8000-000000000005.json  imported    next 2026-09-20 09:15 Sun
5 files: 5 imported
next: verify with the same arguments, then take "schedule_imports_enabled" out of /tmp/state/config.json again.

$ python3 tools/migrate-schedules.py import /tmp/schedules-copy 7841 /tmp/state    # the same again
  ...
5 files: 5 unchanged

$ python3 tools/migrate-schedules.py verify /tmp/schedules-copy 7841 /tmp/state
verify  /tmp/schedules-copy against 127.0.0.1:7841 (/tmp/state)
  file                                       bytes      parsed     next_fire
  c6000001-0000-4000-8000-000000000001.json  SAME       SAME       SAME      2026-09-21 09:40 Mon
  c6000002-0000-4000-8000-000000000002.json  SAME       SAME       SAME      2026-09-20 12:10 Sun
  c6000003-0000-4000-8000-000000000003.json  SAME       SAME       SAME      2026-09-22 10:00 Tue
  c6000004-0000-4000-8000-000000000004.json  SAME       SAME       SAME      2026-09-22 10:20 Tue
  c6000005-0000-4000-8000-000000000005.json  SAME       SAME       SAME      2026-09-20 09:15 Sun
5 files: bytes 5/5 SAME, parsed 5/5 SAME, next_fire 5/5 SAME
```

It was seen to go red first. A copy with one byte changed in each of two files — a minute in `when.at`,
a letter in `task.instructions` — against the same daemon (exit 1):

```
  c6000001-0000-4000-8000-000000000001.json  DIFFERENT  DIFFERENT  DIFFERENT 2026-09-21 09:40 Mon
  c6000002-0000-4000-8000-000000000002.json  DIFFERENT  DIFFERENT  SAME      2026-09-20 12:10 Sun
  ...
  c6000001-0000-4000-8000-000000000001.json  bytes: source sha256 …, stored …
  c6000001-0000-4000-8000-000000000001.json  parsed: differs in when
  c6000001-0000-4000-8000-000000000001.json  next_fire: daemon 2026-09-21 09:40 Mon, computed here 2026-09-21 09:41 Mon
  c6000002-0000-4000-8000-000000000002.json  bytes: source sha256 …, stored …
  c6000002-0000-4000-8000-000000000002.json  parsed: differs in task.instructions
5 files: bytes 3/5 SAME, parsed 3/5 SAME, next_fire 4/5 SAME
```

Importing that copy answered `exists` for the two and left the stored files as they were (verify of the
good copy stayed 5/5 afterwards). A truncated file and one with `"at": "25:00"` were imported as
`invalid` rows — `The file does not contain a JSON object.` and `when.at must be HH:MM in local time` —
and are in the list; verify reports them `SAME  INVALID`.

The Swift app's own answer was a fourth comparison, and it is how this was checked on 2026-09-18:
in a paired 7717 tab, `fetch('/v1/orchestrator/schedules')` and compare `next_fire` row by row
(6/6 the same). That tab cannot be opened any more — the Swift app was stopped on 2026-09-19 and
nothing answers 7717 — so the first three comparisons are the whole check now.

**The order for the real switch** was the user's decision, and it was carried out on 2026-09-19:
the two apps must never both have the same schedule enabled, or the same moment opens two sessions.
Import into the new daemon first and see verify say SAME everywhere; then disable those schedules in
the Swift app (or stop its dispatch); last, leave the new daemon's dispatch on. Stopping the app did
the middle step for every schedule at once, so an import today lands on a machine where nothing else
fires.

## 與舊版刻意不同的地方

| 項目 | 舊版 | 這裡 | 為什麼 |
|---|---|---|---|
| 第一次看見之前的那一次 | 沒有 `created_at` 就可能補跑 | 不跑、也不算 missed | `plan.md` §3.2 |
| 已決定的那一次 | 記在記憶體，重啟後重新評估（可能再推播一次 missed） | 持久在 SQLite | 重啟不連發、不重複推播 |
| 存放 | `~/.config/clawdline/schedules/*.json`，檔案是權威、可手改 | 新 daemon 的 SQLite；要改用路由或匯入 | 兩個 app 不共用任何可寫檔（`plan.md` §4） |
| orchestrator token 的拒絕句 | 最後一句教人去手寫檔案路徑 | 拿掉那半句 | 這裡對應手寫檔的是匯入路由，預設關 |
| 第一次看見的時間 | — | 無條件進位到下一秒 | 存的是秒；09:00:00.4 看見的列存成 09:00:00 會讓 09:00 那次「不早於看見」而當場發射 |
| 開了分頁但第一句打不進去 | — | 算這個排程的一次 run（不記 `spawn_failed`），回應帶 `warnings` | 分頁真的在；記成沒開會讓它不算「還在跑」，也不會寫 `fired_at` |
| 一個排程的 run 還沒結束 | 由 broker 的心跳推進 | 同樣由 broker 的 beat 推進（收 `result.json`、跑逾時）；「還在跑嗎」直接讀 broker 那一列的 state | 一個不寫 result 的 run 在自己的逾時到時結束，放行下一次（D53） |
| webhook 綁定與投遞 | Cloud 指令在 app 內處理 | 同樣的綁定帳本；機器憑證啟用 hook，持久 claim／lease／receipt journal 再走既有 broker | 收到但尚未回 receipt 或已派工但尚未記 receipt 時重啟，都以同一 task id 重播，不重複開 Agent |

## Which machine a schedule is on, and moving it (2026-09-26)

A schedule lives on the machine that runs it: its row is in that machine's SQLite and that machine's
clock fires it. There is no shared schedule and no machine-to-machine channel, and none is added here.

**The list.** On an account with more than one selectable machine, the Cloud Schedules panel reads every
machine (`cloud/relay-reader.ts`, `_freshSchedules`) and draws one group per machine, the header's first,
each headed by the machine's name and platform word as the header switcher says it. A machine that did not
answer is a named line — "offline (last seen HH:MM)" when a last-seen time is known, "offline or unreadable"
otherwise — never an empty group. Every action on a row (history, Run now, Edit, Delete, the webhook
binding) is sent to the machine the row was listed on (`?machine=`, `relay-writer.ts` `namedMachine`).
A console with one machine — the daemon's own page, or an account with one machine — draws no machine
column and no machine field, and sends what it sent before.

**Creating.** The form has a Machine field, defaulting to the header's machine; the project list is the
chosen machine's `GET /v1/places`. The create is routed by the place id, which names its machine.

**Moving** (`cloud/schedule-move.ts`). Choosing another machine while editing makes Save a move. The browser
is the courier, in three writes, in this order:

1. the source is saved with `enabled: false` and every stored field (`model` is omitted, which leaves it as
   stored);
2. a copy is created on the target with every field the form holds, plus a one-time schedule's `on`, plus
   the source's template fields the form has no control for, in `template` (below);
3. the source is deleted.

**The fields the form does not show travel in `template`.** `POST /v1/orchestrator/schedules` takes an
optional `template` object (`ScheduleTemplate` in `api/v1/schedules.schema.json`) holding exactly the keys a
save already carries from the stored file — `claims`, `serialize`, `isolation`, `isolation_base`,
`deliverables`, `kind`, `plan`, `graph`, and `reasoning_effort` when the assistant is codex
(`createTemplate`, `internal/app/schedules.go`). They reach `build` as if a stored file carried them, so the
schedule parser reads each value, as it reads a file. Any other key is refused by name
(`unknown template field: …`). **`permission_mode` is refused** (`template_permission_mode`): any device
that may send could otherwise make a schedule that runs with more than the form can grant, so a schedule
carrying a permission setting is still not moved. A `PATCH` naming `template` is refused — a save keeps the
stored fields itself. The Cloud `schedule-create` word, the relay writer and the copied client carry the
body whole; `TestACloudMoveCarriesTheTemplateToTheStoredFile` (`internal/transport/http`) reads the stored
file back after a Cloud create. The console sends `template` only when the source has such fields, so a
schedule without them still moves to an older target; a target from before this change answers
`unknown field: template`, which the page says as "Clawdline on <machine> is too old to take the settings
this schedule carries" (`scheduleMoveTargetTooOld`), and the move is never retried without the fields.

**The project path in the first message is rewritten.** When the copy's instructions name the source
schedule's `project_dir` as a whole path — the exact string, not the tail of a longer path and not followed
by a character that continues a name, so `/a/dual` is not found inside `/a/dual-astro` or `/a/dual.git` —
it is replaced by the target place's path (`retargetInstructions`); nothing else in the text changes. The
success toast says so: 「第一則訊息裡的路徑已換成 <path>」 (`scheduleMovePathRewritten`).

At no instant are two enabled copies stored. If (2) is refused the source is saved back to its previous
`enabled`, and the page shows the target's refusal; if that restore is also refused the page says the
source is there, disabled. If (3) is refused the page says the old copy is still on the source machine,
disabled. The target does not run an occurrence from before the copy existed: a created schedule is first
seen and created at that instant — `TestAScheduleCreatedByAMoveDoesNotRunThePastOccurrence`
(`internal/app/schedule_move_test.go`) creates one at 09:00:30 with a six-hour catch-up window and measures
0 due that beat and 1 the next day.

The project is found on the target by the repository it clones, not by its path: a place id is a digest of
a path on one machine. `GET /v1/places` (and the Cloud `places` op, which returns the same body) carries
`repo` on every place — `host/owner/name` from the checkout's `[remote "origin"] url`, read from the git
config file directly and bounded at 64 KiB (`places.git_config_bytes`, `docs/limits.md`), or `""` when
there is no portable origin. `repo` is always present so an older daemon is recognisable by its absence.

Everything that can refuse is asked before (1), so a refusal changes nothing. Each is a sentence naming
what to act on (`next-strings.ts`, `scheduleMove*`):

| Refusal | When |
|---|---|
| target offline | the target is offline, or its project list could not be read |
| target / source outdated | that machine's Clawdline sends no `repo`: update it there |
| no origin | the project has no origin remote, so nothing names it on another machine |
| source unlisted | the project is gone from the source's list, so the disable cannot name it |
| no project | the target has no clone of that repository |
| webhook bound / unknown | a Cloud webhook is bound to the schedule id (`schedule-webhook-bind-v1`), or the binding could not be read; the copy gets a new id, so the hook would call a schedule that is gone |
| spent | a one-time schedule that already ran; a copy would arm it again |
| unformed fields | the template carries a permission setting (`permission_mode`); a create may not set it, so the move refuses rather than dropping it. Every other hidden field travels in `template` |

A save that changes nothing about the machine keeps the relay's `cloud_schedule_machine_mismatch` refusal
for a body whose place is on another machine than the schedule: a move is three writes, not a save.

**Needs a redeploy.** Both the source and the target daemon must send `repo`; a machine running a build
from before 2026-09-26 is refused by name as outdated until it is updated. A schedule with hidden template fields also needs a target
that takes `template`; an older one is refused by name as too old, after the source was switched back on.

## 還沒有的（依賴別的工作線）

2026-09-26 對照程式碼重看過這一節：

- ~~`close_tab`~~：linger 已經照排程的 `close_tab` 關分頁（`internal/app/orchestrator/linger.go`）。
- ~~Cloud 的 `schedule`／`schedule-create` 等指令回 `unknown_command`~~：`internal/app/cloudops/ops.go`
  已經有 `schedules`、`schedule-create`、`schedule-update` 等 op，Cloud 的排程面板就是走它們。
- 推播：missed 與派工被拒（`refused`）時會推播（`internal/app/scheduler.go`）；run 以
  failure／timeout／spawn_failed **結束**時的推播這次沒有查證，照舊列為未確認。
- 範本裡 broker 還不支援的欄位（`serialize`、`graph`）會在發射時被具名拒絕（`bad_task`）。
- **執行紀錄的 `terminal_id`／`session_id`、`finished_at`**：這次沒有查證，照舊列著。
- 搬移只能從表單做，一次一個；表單沒有「只跑一次」的日期欄位，所以只跑一次的排程搬移時帶著原本的 `on`，
  但無法在搬移時改它。
