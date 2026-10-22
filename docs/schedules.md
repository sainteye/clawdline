# 排程：建立、發射、遷移（2026-09-18）

退役判準 A8「排程真的會在新 daemon 上發射」、A9「五個營運排程搬過去而且各跑成功一次」（`cutover.md` §7）
的 Go 端。規格來源是舊 app：`~/code/clawdline/docs/schedules.md`、`Sources/Schedules.swift`、
`Sources/ScheduleService.swift`、`Sources/Orchestrator.swift`（排程那段）、`Sources/ScheduleWebhook.swift`、
`Resources/web/app/js/{view/schedules.js,net/schedules.js,input/schedule.js,input/schedule-history.js}`。

## 模型：就是舊版的檔案

The Go scheduler now uses the retired app's file format, with one backward-compatible extension:
`clawdline_schedule`, `schedule_id`, `title`, `when {at, days|on}` (or `{trigger_only: true}`, below),
`task {…}`, `enabled`, `close_tab`, `catch_up_hours`, `notify_on_failure`, `created_at`,
`when_changed_at`, `fired_at`, and the optional `time_zone`. A file copied from the retired app still
parses without translation.

`time_zone` is an IANA name such as `Asia/Taipei` and says which wall clock `when.at` names. The
console records the browser's zone when it creates a schedule, and moving a schedule between machines
carries that zone with it. A Linux host running in UTC therefore does not change what “09:00” means.
A legacy schedule with no field still uses the daemon's `time.Local`, so an upgrade does not silently
retime existing work; an older client saving a schedule that already has a zone cannot erase it. An
unknown name is a `400 bad_request`. The Go binary embeds the IANA database, so execution does not
depend on the target Linux or Windows installation carrying zoneinfo.

**存在哪裡**：新 daemon 自己的 `clawdline.sqlite3`（`CLAWDLINE_NEXT_DIR` 底下），表 `schedule_files`，
一列一個檔、`body` 逐位元組就是那個檔。**不寫 `~/.config/clawdline`**。檔案本身說不出的三件事放在旁邊：
`first_seen`（這支 daemon 第一次看見它）、`last_fire`（已經決定過的最近一次）、`last_missed`。
舊的間隔模型用的 `schedules` 表留著不讀（沒有真的資料寫進去過），讓舊 store 照樣開得起來。

## Calendar-day intervals

A repeating schedule may use `when: {"at":"09:00", "every_days":3, "start":"2026-10-21"}`
instead of `days` or `on`. The form API flattens these three keys beside `time_zone`.
`every_days` is a positive integer; `start` is an inclusive Gregorian date in the existing
1970–9999 date grammar. The rules are mutually exclusive, including `trigger_only`.
Occurrences are anchored to that date in the schedule's named time zone, retaining the local
wall-clock time across daylight-saving changes. Calculation jumps to the adjacent occurrence,
without scanning each intervening day. A next date beyond the supported calendar is absent.
Manual execution, failure, disabling and re-enabling do not move the anchor. Existing catch-up,
first-seen, deduplication and active-run decisions still apply to the latest occurrence only.
Changing the interval or anchor stamps `when_changed_at`, just like changing weekday or time.
List rows include `when` for interval schedules, so the console can display their cadence.
Older daemons reject these new keys; move targets must support them before accepting the copy.

## 路由（與舊版相同）

| 路由 | 誰可以 |
|---|---|
| `GET /v1/orchestrator/schedules`、`GET …/schedules/:id` | 已配對裝置（唯讀即可）、本機 orchestrator token |
| `POST …/schedules`、`PATCH …/:id`、`DELETE …/:id` | 可以 send 的裝置＋`Idempotency-Key`；本機 orchestrator token 可直接動「只跑一次」（`when.on`）的排程；a Session carrying `session_id` + `via.run` from the person's latest message to that conversation may relay that explicit instruction to create, change or delete a repeating schedule. **`permission_mode`（權限）只有人能設**：可以 send 的裝置（含每一筆經 Cloud 的寫入）與本機 console 可以設、改、拿掉；orchestrator token 與 `session_id`＋`via.run` 轉達只能保留檔案裡原本的值，設或改一律 `403 permission_needs_person` |
| `POST …/:id/run` | 可以 send 的裝置＋key；或 orchestrator token（不用 key） |
| `POST /v1/orchestrator/schedule-webhooks/bind` | 可以 send 的裝置（Cloud `schedule-webhook-bind-v1` 指令的本機那半，經 Cloud 進來時就是這個身分），或本機 orchestrator token |
| `GET /v1/orchestrator/schedule-exports` | 只有 orchestrator token（本 daemon 新增；每個排程檔原樣匯出） |

`gate.go` 只多一行：`/v1/orchestrator/schedules/…` 的寫入跟清單一樣走「兩扇門」，不再一律要 orchestrator token。
普通的「立即執行」在同排程已有 task 執行中時仍回 `409 schedule_active`。人看見這個狀態後可再確認一次，
以 `{"force":true}` 重送；舊 task 不會被取消，新 task 會另行嘗試派送。force 只略過同排程的 active 保護，
不略過 `orchestrator_enabled`、單次排程已用完、trigger-only 停用、claims 衝突或 broker 容量限制，並在 audit 記 `force=1`。

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

On each tick, each enabled schedule considers only the **latest** occurrence (`LatestFire`) in its
`time_zone`, or in the machine zone for a legacy file. It then asks, in order:

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

**`fired_at` 只擋時鐘，不擋人**：它的唯一用途是讓時鐘不把同一個日期跑第二次。一個排程能跑幾次沒有上限——
跑過的單次排程照樣可以按「立即執行」、照樣可以被 webhook 觸發（兩者都不再回 `409 schedule_spent`），
存檔時把日期改掉也照收，`fired_at` 跟著舊日期丟掉，新日期到了時鐘會再跑一次。
`TestAOneTimeScheduleThatRanCanRunAgain` 釘住這三件事。

**重啟**：`last_fire` 在 SQLite，所以重開後同一次不會再跑；停機期間錯過、而且這支 daemon 早就看過的那一次，
重開後第一跳補跑一次——永遠只有最近那一次，不會補一整批。

**第 4 條為什麼要有**：舊版把沒有 `created_at` 的檔當成「從很久以前就在」，在 6 小時補跑窗內，一個被複製、
還原或匯入的檔會在出現的那一分鐘就發射——正是 §3.2 第二次事故換個寫法。所以每一次都還要跟這支 daemon
第一次看見這一列的時間比；這個時間在**讀取時**蓋章，因為會出事的正是沒經過寫入路徑的那些列。

## Trigger-only schedules and `clawdline webhook fire` (2026-09-27)

The problem: an agent session on one machine must be able to start a fixed, pre-defined task on
another machine through Schedule Webhook v1, and must learn from the command it ran whether the task
succeeded, failed, was refused, or never reached the machine. A trigger that does not reach the
machine quickly must be void for good: when the target is off, a deploy must not run hours later
when it wakes. There is still no machine-to-machine channel, and none is added; the caller waits on
the result instead.

**A schedule may have no time.** `when` may be exactly `{"trigger_only": true}` — no `at`, `days` or
`on` beside it, and `false` is refused rather than read as "timed". The clock never fires it:
`LatestFire` and `NextFire` are zero for it, so the minute timer skips it before any of the seven
questions above, it is never missed, never caught up and never notified about, and it has no
`next_fire`. `Decide` answers `no_clock` if it is ever asked anyway. It runs by a manual Run or by
its webhook, and **only while `enabled` is true**: a disabled one refuses the webhook with
`schedule_disabled` as before, and — unlike a timed schedule, whose manual Run is a validation
press that ignores `enabled` — refuses a manual Run with `409 schedule_disabled` too, because for a
schedule without a clock the press is the run itself. Lists and details carry `trigger_only: true`.
The form body sends `trigger_only: true` instead of `at`/`days`/`on` (all three beside it is
`400 bad_request`); a save may turn a repeating schedule into a trigger-only one and back, and a
schedule that runs once stays once. The orchestrator token alone cannot make one: like a repeating
schedule it is a standing instruction, and it needs a person's device or the `session_id` + `via.run`
relay. `TestATriggerOnlyScheduleHasNoClock` (a week of moments, midnight included) and
`TestATriggerOnlyScheduleIsRunOnlyByHand` (a week of beats, then Run enabled and disabled) pin it.

**A delivery's deadline is absolute on this side too.** A trigger may carry
`{"deliver_within_seconds": N}` (10…86400, default 86400); Cloud refuses the version-1
`mac_durable_accepted` receipt with `409 delivery_expired` once `accepted_at + N` has passed. The
runtime (`internal/app/schedule_webhooks.go`) treats that answer as final: the journal row becomes
`expired`, one log line says so, no receipt is retried and nothing is dispatched. A row in `expired`
or `canceled` is left alone even if Cloud offers the same delivery again.
`TestAnExpiredDeliveryNeverReachesTheBroker` counts `DispatchScheduled` calls through a wrapper and
asserts zero across three polls, with `TestAnAcceptedDeliveryReachesTheBrokerOnce` as its control;
with the re-offer guard disabled it failed, sending receipt versions 2 and 3 for a delivery Cloud had
already declared expired. A `schedule_dispatch_refused` receipt carries a stable code, never text:
`schedule_not_found`, `schedule_disabled`, `schedule_active`,
`orchestrator_disabled`, `hook_unbound`, `binding_store_unavailable`, `outcome_unknown` or
`dispatch_failed`. A finished run sends `task_execution_terminal` with `success`, `failure`,
`timed_out` (the broker's `timeout`), `cancelled` or `spawn_failed`;
`TestEveryTerminalTaskStateHasAWebhookOutcome` fails if the broker grows a terminal state with no
outcome.

**What the caller gets.** The `202 clawdline.schedule_webhook.accepted.v1` answer names the delivery
(`delivery_id`, `deliver_by`), and
`GET /v1/schedule-webhook-trigger/<token>/deliveries/<delivery_id>` — authorised by the same token —
answers its `state` (`queued`, `leased`, `mac_accepted`, `dispatch_deferred`, `dispatch_accepted`,
`dispatch_refused`, `terminal`, `expired`, `canceled`), the terminal `outcome` and the refusal's
`refusal_code`. No task content, schedule id or machine id is in it.

`clawdline webhook fire` is that caller, in the same binary on every platform, with no daemon:

```
usage: clawdline webhook fire [--url-file path] [--deliver-within 60s] [--timeout 60m] [--no-wait]
  start a schedule through its webhook; no daemon needed. The URL is read from --url-file or CLAWDLINE_WEBHOOK_URL, never from an argument
  stdout, one line: success|failed|not_delivered|refused|gave_up delivery=<id|-> [state=<s>] [outcome=<o>] [refusal_code=<c>] [code=<c> status=<n>] [reason=unreachable]
  --no-wait prints only the delivery id. Exit: 0 success, 1 failed, 2 never reached the machine, 3 refused, 4 gave up waiting, 64 usage
```

- The URL is a credential: never a positional argument (refused with 64, not echoed), never
  printed. Transport errors are stripped of the URL and host, and every output line is passed
  through a redaction of the URL and any long path segment. Redirects are not followed (a
  redirected POST would become a GET and carry the token elsewhere). `https` only, except `http`
  to a loopback host.
- One random `Idempotency-Key` per invocation. Transport errors and 5xx are retried with that key,
  backing off, only while the `--deliver-within` deadline (default 60s, whole seconds 10…86400,
  sent as `deliver_within_seconds`) has not passed; then exit 2. Any other non-202 is not retried.
- After the 202 it polls the status every 3 s, growing by half to 10 s, until a final state or
  `--timeout` (default 60m, counted from the 202). Each state change is one stderr line
  (`clawdline webhook fire: delivery <id>: leased`). A 429, a 5xx or a transport error while
  polling keeps polling. Unknown states are treated as still in progress.
- Final lines, one per outcome:

| Exit | stdout |
|---|---|
| 0 | `success delivery=<id> state=terminal outcome=success` (`--no-wait`: just `<id>`) |
| 1 | `failed delivery=<id> state=terminal outcome=timed_out` (or failure, cancelled, spawn_failed) |
| 2 | `not_delivered delivery=<id> state=expired` (or canceled); `not_delivered delivery=- reason=unreachable`; `not_delivered delivery=<id> state=unknown code=not_found` |
| 3 | `refused delivery=<id> state=dispatch_refused refusal_code=schedule_disabled`; `refused delivery=- code=webhook_unavailable status=404` (the trigger's 429 and 400 alike) |
| 4 | `gave_up delivery=<id> state=dispatch_deferred` (the last state seen) |

Decisions the contract left open: a usage error is 64 (EX_USAGE), because 2 already means "never
reached the machine"; a `404 not_found` for a delivery Cloud has just accepted is 2 (it was dropped
before anyone refused it), while any other 4xx while polling is 3; a 202 with no readable
`delivery_id` is `failed … reason=unreadable_answer`, exit 1. Pinned by `cmd/clawdline/webhook_fire_test.go`
against an `httptest` server speaking the contract, every test also asserting that neither stdout
nor stderr contains the URL or the token.

## Moving schedules in from the Swift app (retired)

The Swift app's schedule files were moved into this daemon on 2026-09-19 with
`tools/migrate-schedules.py` and `POST /v1/orchestrator/schedule-imports`, behind the
`schedule_imports_enabled` switch in `config.json`. The Swift app is gone, so the tool, the route
and the switch were removed on 2026-10-17; a `schedule_imports_enabled` key left in `config.json` is
ignored, and the route answers `501 not_implemented`. `GET /v1/orchestrator/schedule-exports` stays.
The procedure and its measured output are in this file's history before that date.

## 與舊版刻意不同的地方

| 項目 | 舊版 | 這裡 | 為什麼 |
|---|---|---|---|
| 第一次看見之前的那一次 | 沒有 `created_at` 就可能補跑 | 不跑、也不算 missed | `plan.md` §3.2 |
| 已決定的那一次 | 記在記憶體，重啟後重新評估（可能再推播一次 missed） | 持久在 SQLite | 重啟不連發、不重複推播 |
| 存放 | `~/.config/clawdline/schedules/*.json`，檔案是權威、可手改 | 新 daemon 的 SQLite；要改用路由或匯入 | 兩個 app 不共用任何可寫檔（`plan.md` §4） |
| orchestrator token 的拒絕句 | 最後一句教人去手寫檔案路徑 | 拿掉那半句 | 這裡對應手寫檔的是匯入路由，預設關 |
| 第一次看見的時間 | — | 無條件進位到下一秒 | 存的是秒；09:00:00.4 看見的列存成 09:00:00 會讓 09:00 那次「不早於看見」而當場發射 |
| 開了分頁但第一句打不進去 | — | 算這個排程的一次 run（不記 `spawn_failed`），回應帶 `warnings` | 分頁真的在；記成沒開會讓它不算「還在跑」，也不會寫 `fired_at` |
| 一個排程的 run 還沒結束 | 由 broker 的心跳推進 | 同樣由 broker 的 beat 推進（收 `result.json`、跑逾時）；「還在跑嗎」直接讀 broker 那一列的 state | 一個不寫 result 的 run 在自己的逾時到時結束，放行下一次（D53）——**但分頁還在工作或在等人回答時不算**（2026-10-15，見下方「逾時不打斷還在工作的 run」） |
| webhook 綁定與投遞 | Cloud 指令在 app 內處理 | 同樣的綁定帳本；機器憑證啟用 hook，持久 claim／lease／receipt journal 再走既有 broker | 收到但尚未回 receipt 或已派工但尚未記 receipt 時重啟，都以同一 task id 重播，不重複開 Agent |

## Which machine a schedule is on, and moving it (2026-09-26)

A schedule lives on the machine that runs it: its row is in that machine's SQLite and that machine's
clock fires it. There is no shared schedule and no machine-to-machine channel, and none is added here.

**The list.** On an account with more than one selectable machine, the Cloud Schedules panel reads every
machine independently (`cloud/relay-reader.ts`, `pages/schedules.tsx`) and draws each answered group without
waiting for its peers, the header's first,
each headed by the machine's name and platform word as the header switcher says it. A machine that did not
answer is a named line, never an empty group: "its schedules could not be read" when the account hears from
it now, or when this page did not ask because its own Cloud line was reconnecting; "offline (last seen
HH:MM)" for a machine that stopped reporting in; "offline or unreadable" when neither is known
(`schedule-machines.ts` `silentMachineWord`). A list read asked while the page's line is paused or renewing
waits for it, as one schedule's read does, and answers `cloud_reconnecting` when it does not come back; each
failed list read reaches the machine's log as `cloud.read.failed word=schedules`. Every action on a row (history, Run now, Edit, Delete, the webhook
binding) is sent to the machine the row was listed on (`?machine=`, `relay-writer.ts` `namedMachine`).
A console with one machine — the daemon's own page, or an account with one machine — draws no machine
column and no machine field, and sends what it sent before.

A refresh preserves each machine's last rows until that machine answers, including with an empty list.
An `unanswered` entry is a failed read even when the list endpoint resolves successfully. A Session
answer permits one recovery read per failed machine per regular refresh cycle, including before the
first schedule answer; repeated Session updates cannot create an unbounded retry loop. Returning to a
visible page retries a failed reading immediately. Optional project-label reads from an older refresh
cannot overwrite a newer inventory reading.

**Creating.** The form has a Machine field, defaulting to the header's machine; the project list is the
chosen machine's `GET /v1/places`. The create is routed by the place id, which names its machine.

**Moving** (`cloud/schedule-move.ts`). Choosing another machine while editing makes Save a move. The browser
is the courier, in three writes, in this order:

1. the source is saved with `enabled: false` and every stored field (`model` is omitted, which leaves it as
   stored);
2. a copy is created on the target with every field the form holds — including its permission
   (`permission_mode`), sent as the form field — plus a one-time schedule's `on`, plus the source's template
   fields the form has no control for, in `template` (below);
3. the source is deleted.

**The fields the form does not show travel in `template`.** `POST /v1/orchestrator/schedules` takes an
optional `template` object (`ScheduleTemplate` in `api/v1/schedules.schema.json`) holding exactly the keys a
save already carries from the stored file — `claims`, `serialize`, `isolation`, `isolation_base`,
`deliverables`, `kind`, `plan`, `graph`, and `reasoning_effort` when the assistant is codex
(`createTemplate`, `internal/app/schedules.go`). They reach `build` as if a stored file carried them, so the
schedule parser reads each value, as it reads a file. Any other key is refused by name
(`unknown template field: …`). **`permission_mode` is refused in `template`** (`template_permission_mode`):
it is the form's own 「權限」 field, and travels as that form field, where who may set it is checked;
`template` is never a second door around that check. A `PATCH` naming `template` is refused — a save keeps the
stored fields itself. The Cloud `schedule-create` word, the relay writer and the copied client carry the
body whole; `TestACloudMoveCarriesTheTemplateToTheStoredFile` (`internal/transport/http`) reads the stored
file back after a Cloud create. The console sends `template` only when the source has such fields, so a
schedule without them still moves to an older target, and sends `permission_mode` only when the form has
one chosen. A target from before either answers `unknown field: template` or
`unknown field: permission_mode` (`refusedByOlderTarget`), which the page says as "Clawdline on <machine> is too old to take the settings
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
| target offline | the target is offline, or its project list could not be read, and asking again at Save failed too (below) |
| target / source outdated | that machine's Clawdline sends no `repo`: update it there |
| no origin | the project has no origin remote, so nothing names it on another machine |
| source unlisted | the project is gone from the source's list, so the disable cannot name it |
| no project | the target has no clone of that repository |
| webhook unknown | the source's binding could not be read, or a bound hook could not be read from Cloud (or Cloud answered another hook, or a state that does not move); "could not read" is not "unbound" |
| spent | a one-time schedule that already ran; a copy would arm it again |

**"Offline" is asked again at Save, never read from the page** (2026-09-26). The fleet row is the hosted
gate's machine list, which is not drawn again once a console is on screen, and the places are from when the
machine was picked: a machine that was restarting when the page opened stayed "offline" in both, and every
Save repeated a refusal telling the person to wait for what had already happened. So when either says the
target is offline, or the source's places could not be read, Save shows busy and asks again — that
machine's `GET /v1/places`, and which machines the Cloud client computes as reporting in now
(`askAgainBeforeRefusing`, `recheckSchedulePresence`). A machine that answers its places is online; the
refusal is said only when the fresh read also failed. What came back becomes the form's own state — the
machine chips and the project list — so the page stops showing the old reading.

**A bound webhook moves with the schedule and keeps its URL** (2026-09-26), so whoever calls it changes
nothing. Before (1) the page reads the hook from Cloud (`GET /v1/schedule-webhooks/:id`) and plans with
its `revision`. Between (2) and (3) two more steps run, in this order:

- (2a) Cloud moves the hook to the target: `POST /v1/schedule-webhooks/:id/move`
  `{"machine_id", "expected_revision"}` (the Cloud's PROTOCOL.md). The hook becomes `pending_binding` —
  paused: a call is answered as a pending hook is, and runs nothing — with the same URL and a new
  generation; every not-yet-receipted delivery of the old generation is cancelled, so the source can never
  claim one.
- (2b) the target binds it to the **copy's** id with the same `schedule-webhook-bind-v1` step a new hook
  takes, and activates it with its own machine credential; the page waits until Cloud shows it `active`.
  The move raised the hook's revision and Cloud activates only at the revision a hook is at, so this bind
  carries the optional `hook_revision` — the revision (2a) answered (the rollback's bind, the one the move
  back answered); a new hook's bind sends none and activates at 0. Until 2026-09-26 the daemon always
  activated at 0, and every moved hook was refused `stale_revision`, said as `503 temporarily_unavailable`
  (`TestAMovedWebhookActivatesAtTheRevisionItIsAt`). Cloud's own refusal of an activation is now answered as
  itself (`409 stale_revision`, `409 invalid_state`, `404 not_found`); only a Cloud that was not reached
  stays `503 temporarily_unavailable`.

So the hook is never active while pointing at a schedule that does not exist: it is paused from (2a) until
the target has bound the copy. Deleting the source in (3) drops the source's local binding in the same
transaction (`DeleteScheduleFile`, `internal/adapters/store/schedules.go`); a binding whose schedule row is
already gone no longer refuses the same hook when it is bound again later — rows daemons before this left
behind (`TestAWebhookBindingLeavesWithItsSchedule`). A claim for a moved hook cannot reach the source in the
meantime: Cloud claims only deliveries stamped with the claimant's `machine_id`
(the Cloud API's delivery claim).

When a step fails, the page says what state things are in, by name (`moveSchedule`, outcomes
`hook_move_failed` and `hook_bind_failed`):

- (2a) refused (stale revision, the hook disabled meanwhile, Cloud unreachable): the copy is deleted and the
  source switched back; the hook was never touched.
- (2b) fails — the target is offline, or its Clawdline is older than webhook binding (2026-09-22) and so
  never activates it; the two cannot be told apart from the browser, so the sentence names both: the hook
  is moved back to the source (its revision read again) and bound again to the source schedule (the source
  still holds that binding), then the copy is deleted and the source switched back on. If the hook could not
  come back, or came back and was not activated, the page says which machine it is on, that it is paused,
  and the one action that fixes it: disable it and generate a new one (the URL changes)
  (`scheduleMoveHookStranded`). A copy left on the target or a source left disabled is each said too.
  A target older than `hook_revision` refuses the bind (`malformed_command` from its Cloud bridge) and the
  reason is said as that machine needing an update (`scheduleMoveTargetTooOld`); the rollback runs as
  above, and a source as old refuses its bind too, which leaves the schedule back on the source, switched
  on, the copy deleted, and the hook on the source, paused (`scheduleMoveHookStranded`). `stale_revision`
  (the hook changed while it was moving) is said as `scheduleMoveHookStale`.
- (3) refused: as without a hook — the hook runs the copy on the target; the old copy is on the source,
  disabled.

A **disabled** hook bound to the schedule does not move and is not needed: the schedule moves without it,
the source's binding goes with the deleted source, and the success toast says a new one can be generated on
the target (`scheduleMoveWebhookLeftDisabled`). A successful move's toast says the webhook moved and its URL
is unchanged (`scheduleMoveWebhookMoved`).

A save that changes nothing about the machine keeps the relay's `cloud_schedule_machine_mismatch` refusal
for a body whose place is on another machine than the schedule: a move is three writes, not a save.

**Needs a redeploy.** Both the source and the target daemon must send `repo`; a machine running a build
from before 2026-09-26 is refused by name as outdated until it is updated. A schedule with hidden template fields also needs a target
that takes `template`, and a schedule with a permission a target that takes `permission_mode`; an older one
is refused by name as too old, after the source was switched back on. Moving a schedule with a bound webhook
needs the Cloud `move` route, and a target — and, for the rollback, a source — from 2026-09-26 or later
(`hook_revision` on the bind); the source daemon
drops the binding on delete from this change on — an older source leaves a stale row that the updated
daemon's bind replaces.

## The permission field

The form's 「權限」 (in More) is `task.permission_mode`: 這台機器的預設 (no key), 每一步都問我 (`ask`),
可以直接改檔案 (`edits`), 完全權限（不再詢問）(`full`), with a one-line warning under the last. Editing a
schedule shows its stored value and opens More when it has one. The route reads the field as it reads
`model`: a name sets it (the parser checks it), `""` takes it off, no key keeps what the file says; the
console sends the key only when a permission is chosen or a stored one is being taken off, so a daemon
older than the field still takes a schedule that never had one. Only a person sets it: the orchestrator
token and a session relaying a person's message may keep the stored value and nothing else
(`permissionRefusal`, `403 permission_needs_person`), so an agent cannot grant itself a full-permission
recurring wake-up. The create or save audit line carries `permission` and `permission_was` whenever it
changed (`TestTheSchedulePermissionFieldRoundTrips`, `TestAnAgentMayNotSetOrChangeASchedulesPermission`,
`internal/app/schedule_permission_test.go`).

## 還沒有的（依賴別的工作線）

2026-09-26 對照程式碼重看過這一節：

- ~~`close_tab`~~：linger 已經照排程的 `close_tab` 關分頁（`internal/app/orchestrator/linger.go`）。
- ~~Cloud 的 `schedule`／`schedule-create` 等指令回 `unknown_command`~~：`internal/app/cloudops/ops.go`
  已經有 `schedules`、`schedule-create`、`schedule-update` 等 op，Cloud 的排程面板就是走它們。
- 推播：missed 與派工被拒（`refused`）時會推播（`internal/app/scheduler.go`）；run 以
  failure／timeout／spawn_failed **結束**時的推播這次沒有查證，照舊列為未確認。
- 範本裡 broker 還不支援的欄位（`serialize`、`graph`）會在發射時被具名拒絕（`bad_task`）。
- ~~執行紀錄的 `terminal_id`／`session_id`、`finished_at`~~：2026-10-15 補上，見下方「執行紀錄可以接回去」。
- 搬移只能從表單做，一次一個；表單沒有「只跑一次」的日期欄位，所以只跑一次的排程搬移時帶著原本的 `on`，
  但無法在搬移時改它。

## 逾時不打斷還在工作的 run（2026-10-15）

排程的 run 沒有 root 在等它（D07），結束它的只有它自己的 `result.json`，或沒寫的話就是逾時。之前逾時只看牆上時鐘，
而且從**排進佇列**那一刻算：等機器等了 17 分鐘的 run 只剩 13 分鐘（`407f8814`，2026-09-28）；brief 又跟 child 說
「你有 30 分鐘」，child 就在第 30 分鐘匆忙收尾回報，接著 `on_success` 關掉分頁（2026-10-15 的 Augur 研究佇列，
回報時距離 deadline 只剩 3 秒）。規則改成：

- **排程 run 過了逾時，只要分頁還在、而且在工作（`working`）或在等人回答（`waiting`），就不結束**——
  只有它自己回報交付或失敗才結束（`scheduledRunStillWorking`，`internal/app/orchestrator/scheduled_run.go`）。
  分頁不見了，或閒置在提示字元，才照舊記成 `timeout`，放行下一次。一般派工的 task 不受影響。
- **逾時不關排程的分頁**：`close_tab: always` 指的是 child 自己回報的結束（成功或失敗），逾時一律留著（`tabPolicy`）。
- 排程 child 的 brief 不再說「你有 N 分鐘」，改成「預計 N 分鐘，不是 deadline，做完再回報」。

`TestAScheduledRunAtWorkIsNotEndedByItsClock`、`TestAScheduledBriefingSaysTheClockIsNotADeadline`。

## 執行紀錄可以接回去（2026-10-15）

`GET …/schedules/:id` 的每一筆 run 現在帶 `terminal_id`（分頁還在 Session 清單上時，Web 直接打開它）、`finished_at`，
以及結束後還能接回去時的 `session_id`。之前 Go daemon 沒記 child 的對話，所以歷史面板的每一列都按不下去。

對話有兩個來源：broker 在 run 還在跑時，從讀數裡 child 分頁綁到的對話記下 `child_conversation_id`
（`noteChildConversation`）；更早的 Claude run 則看 transcript 的第一則使用者訊息——那是 broker 打進分頁的
`You are a Clawdline CHILD agent for task <id>`（`projects.ChildFirstLinePrefix`），只有這一行算數，其他 session
只是「提到」這個 task 不算。只有已結束、在專案目錄本身跑（`isolation: none`）、transcript 還在磁碟上的 run 才給
`session_id`。`POST /v1/places/:id/resume/…` 在專案歷史找不到這個對話時，再問 `ScheduleBook.ScheduledConversation`：
同一個專案、同一個 assistant、已結束的排程 run 的對話才放行，接回去是一個普通的互動 Session。每次查詢最多開
256 個 transcript、每個讀前 64 KiB（`schedule.run_transcripts_probed`、`schedule.run_transcript_head`）。
Codex 的舊 run 不回補，只有新的 run 從讀數記下對話。

`TestAFinishedRunIsResumedByItsChildsConversation`、`TestAChildConversationIsFoundByItsFirstLine`。

## Incident investigation schedules

A trigger-only schedule with `task.kind: "health_investigation"` can receive a
frozen incident through its bound Cloud webhook. Permission remains the person's
choice; ordinary hooks never become investigation workers automatically. See
[Health investigations](health-investigations.md) for the binding, read-only
evidence, bounded result format and restart behavior.


## Antigravity model and effort preservation

The website schedule form retains an explicit native model and effort when a stored
schedule is reopened, saved or copied through its existing template path. The native
model catalog is unknown: only the native default and an existing model are offered.
The effort choices are `low`, `medium`, `high`, `xhigh` and `max`, with an empty value
meaning native default. Unknown saved values remain visible; they are not silently
replaced with another provider's choices. The backend still validates every save.

Switching providers keeps separate form choices for each provider. Switching back
restores that provider's draft. A backend save which changes provider does not inherit
the previous provider's model or effort when the request omits those fields. Explicit
empty strings clear them. A template with no provider identity is validated against
the provider named by its create request.

Antigravity schedule saving checks the selected machine's current inventory and the
shared start-admission helper. Missing, stale, refused or unsupported readiness does
not authorize a save. This client check never grants backend dispatch permission:
the existing draft permission ceiling and native launch refusals still apply. Saving
or choosing a model does not authorize paid calls or automatic paid fallback.

The default model settings are documented in [Defaults for new sessions](default-models.md).
