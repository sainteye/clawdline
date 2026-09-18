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
| `POST …/schedules`、`PATCH …/:id`、`DELETE …/:id` | 可以 send 的裝置＋`Idempotency-Key`；或本機 orchestrator token，但**只能動「只跑一次」（`when.on`）的排程**（`MachineRefusal`，舊版原句） |
| `POST …/:id/run` | 可以 send 的裝置＋key；或 orchestrator token（不用 key） |
| `POST /v1/orchestrator/schedule-webhooks/bind` | 只有 orchestrator token（Cloud `schedule-webhook-bind-v1` 指令的本機那半） |
| `POST /v1/orchestrator/schedule-imports` | 只有 orchestrator token，**而且 `config.json` 要有 `"schedule_imports_enabled": true`**（預設關；本 daemon 新增，遷移用） |
| `GET /v1/orchestrator/schedule-exports` | 只有 orchestrator token（本 daemon 新增，遷移用） |

`gate.go` 只多一行：`/v1/orchestrator/schedules/…` 的寫入跟清單一樣走「兩扇門」，不再一律要 orchestrator token。

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

## 遷移：把舊排程搬過來

```sh
# 在要接手的機器上。<state> 是新 daemon 的 CLAWDLINE_NEXT_DIR（預設 ~/.config/clawdline-next）
cp -p ~/.config/clawdline/schedules/*.json /tmp/schedules-copy/      # 用複本；.bak 不會被帶到
# 在 <state>/config.json 加上 "schedule_imports_enabled": true（匯入用完就拿掉）
python3 migrate-schedules.py import /tmp/schedules-copy 7727 <state>  # 逐檔結果與下次執行時間
python3 migrate-schedules.py verify /tmp/schedules-copy 7727 <state>  # 三種比對都要 SAME
```

`migrate-schedules.py` 在這次任務的 artifacts 裡（它只讀來源目錄）。匯入的規則：

- 每個檔**逐位元組**存進去，檔名就是 id；讀不懂的檔也存，清單上顯示成 `invalid` 列（不會默默少一個）。
- 匯入那一刻就是它的 `first_seen`，而且最近一次已發生的時間記為已決定：舊 app 已經跑過（或錯過）的那一次，
  新 daemon 不會重跑，也不會說它 missed。下一次才是新 daemon 的。
- 同一個檔再匯一次回 `unchanged`；同 id 不同內容回 `exists`，不覆蓋。
- **為什麼要另外一個開關**：匯入收下重複排程與任意 `project_dir`，對新 daemon 來說就是舊版的「手寫檔案」那扇門，
  orchestrator token「只能動只跑一次的排程」的規則管不到它，而本機任何 agent session 都讀得到那個 token。所以預設關，
  遷移時由人打開、用完關掉。

**怎麼驗證內容一致**（`verify`，三種互相獨立）：

1. **位元組**：來源檔的 sha256 ＝ `GET /v1/orchestrator/schedule-exports` 裡那個檔的 sha256。
2. **解析結果**：新 daemon 自己讀出來的 `title`、`enabled`、`when`、`close_tab`、`catch_up_hours`、
   `notify_on_failure` 與整個 `task` 範本，跟來源檔相同（未寫的欄位比對舊版預設值）。
3. **時間**：新 daemon 的 `next_fire` ＝ 腳本自己從來源 `when` 用本地時間算出來的下一次。

另外可以拿舊 app 自己的答案對照：在已配對的 7717 分頁 `fetch('/v1/orchestrator/schedules')`，
逐列比 `next_fire`（2026-09-18 實測 6/6 相同）。

**真的切換時的順序**（要使用者決定，見報告）：兩邊不能同時啟用同一個排程，否則同一時刻會開兩個 session。
先在新 daemon 匯入，確認 verify 全部 SAME，再在舊 app 把那幾個排程停用（或停舊 app 的派工），最後讓新 daemon
的派工開著。

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
| webhook 綁定 | Cloud 指令在 app 內處理 | 同樣的帳本與規則，走本機路由 | 這個 daemon 的 Cloud 線以路由接本機能力 |

## 還沒有的（依賴別的工作線）

- ~~派工本身還是 Go 的第一版~~：W4 起排程走 broker，有 CHILD.md、task secret、`timeout_minutes`、`model`、
  `permission_mode`、worktree 隔離。**還沒有的**：`close_tab`（結束後關分頁屬於 linger，W6）；排程 run 以
  failure／timeout／spawn_failed 結束時的推播（舊版 `scheduleNotifyFailure`，要等 broker 的推播接上，W5／D24）；
  範本裡 broker 還不支援的欄位（`serialize`、`graph`、`reasoning_effort`）會在發射時被具名拒絕（`bad_task`），
  不再像舊骨架那樣安靜地忽略。
- **webhook 的啟用與投遞**：綁定寫進本機帳本後，要向 Cloud 啟用 hook；這個 daemon 沒有 Cloud 帳號用戶端，
  所以回舊版遇到「沒有機器憑證」時的 `401 no_machine_credential`（綁定本身仍留著，與舊版相同）。Cloud 送來的
  delivery（claim／lease／receipt）整段沒有做。
- **Cloud 的 `schedule`／`schedule-create` 等指令**：`internal/app/cloudops` 還回 `unknown_command`；
  路由已經有了，接上是 cloudops 那邊的事。
- **執行紀錄的 `terminal_id`／`session_id`、`finished_at`**：Go 的 task 記錄沒有這些，run 列只能是「沒有動作」。
