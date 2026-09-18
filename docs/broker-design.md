# broker 設計分析：哪些照搬、哪些重新設計

> 使用者原話（2026-09-18）：「請在這次順便去分析，這些東西是否有應該要 refactor 或是重新設計的地方」。
>
> 分析對象是 `docs/cutover.md` §2、§3 點名的那一塊：舊 app 的 broker。
> **這份文件只做分析與設計，不改實作程式。**
>
> 原則：**重寫不是翻譯。** 舊 app 自己的紀錄已經點名的缺陷不照搬；
> 但也不為了新而新——每一條「改設計」都附證據與代價。沒量到的一律寫「未量」，不算通過。

量測時間：2026-09-18 08:00–08:40。舊 app 在 `~/code/clawdline`，**全程只讀**；新版 repo 在 `6c57d59`。
兩個 daemon 都在跑：舊版 `:7717`（pid 46747）、新版 `:7727`。

---

## 0. 五句話

1. **舊 broker 的形狀是對的，錯在它底下那層儲存。** 派工→簡報→收工→通知→ACK→落地這條鏈，
   加上它的三個不變量（收據型別化、claims 先宣告、delivered ≠ landed），是 20 天、769 筆真實派工
   打磨出來的，**照搬**。有問題的是這條鏈底下的儲存：一份 6.75 MB 的 JSON，任何一處改動都整份重寫。
2. **這份檔案每 10.5 秒被整份重寫一次，而每次改到的只有一個觀察時鐘。** 在 315 秒的視窗裡，
   `orchestrator.json` 被重寫了 **30 次**，內容淨變化 **+90 bytes**。逐欄比對連續五次重寫，
   **四次只改了 briefed task 的 `executor/observed_at` 與 `executor/inventory_generation`**（§2.2）。
   也就是說，這台機器每 10 秒寫掉 6.75 MB，記下來的是「我又看了一次，還是同一個執行者」。
   根因只有兩行程式（`SessionWatch.swift:1033-1043`、`Orchestrator.swift:7466`）。
   **Go 版的第一條設計規則因此是：事實落盤，觀察不落盤**（§6.2）。
3. **舊 repo 在 9/04 自己做過一次這個分析，而且警告過「不要用效能當搬遷理由」。**
   當時檔案 1.77 MB、序列化 5 ms。14 天後長到 6.75 MB（3.8×），序列化成本同比例上升到數十毫秒，
   而且跑在主執行緒上。**那份分析的結論仍然成立**：搬遷的主要理由是「遺忘」與「查詢」，
   不是速度。本文在它之上補兩條理由——**交易邊界**與**跨行程安全**——並且在一件事上不同意它（§3）。
4. **第二嚴重的問題不在效能，而在卡住的時候沒有任何東西會叫。** 舊 app 的 `/v1/health` 裡
   **沒有一個欄位能說出心跳還活著**（當場量到，§2.6）。git 歷史裡有四次「主執行緒被同步呼叫卡住、
   app 看起來還活著、讀取全部停止」的事故，**每一次的修法都是拿掉那個阻塞呼叫，
   沒有一次加上偵測或自救**。Go 版要補的是一個會回報自己還活著的心跳（§6.6）。
5. **有三件事不要動**：完成通知的 durable notice＋退避＋dead letter（`f05ed2b3`）；
   landing 的雙臂證明，以及「只關閉自己證明得了的紀錄」（`2343c901`）；`result.json` 的
   preflight 加上兩次觀察的復原（`9d1676e9`）。這三件各自是一次真實事故換來的解法，
   **照搬，連理由一起搬**。另外，舊版 backlog 裡還有 **十一項到今天仍未修的缺陷**（§5），
   這些**不照搬**。

---

## 1. 判準

| 分級 | 意思 |
|---|---|
| **照搬** | 行為與理由一起搬。Go 的寫法可以不同，但**可觀察的結果、以及拒絕的形狀要一樣**。 |
| **小改** | 概念照搬，實作換成比較適合 Go／SQLite 的形狀；對外契約不變，或只加欄位。 |
| **重新設計** | 舊版的**決定本身**已經被它自己的紀錄推翻。要換的是資料模型或責任歸屬，不是語言。 |
| **不照搬** | 舊版到今天仍開著的缺陷。Go 版要換一個不會長出這個缺陷的形狀。 |

每一項都附上**證據**（檔案行號、commit sha 或當場量到的數字）、**代價**與**風險**。
沒有證據的項目不列入分級表。

---

## 2. 現況量測

### 2.1 持久狀態：派工說明寫六份，實際上是九份

| # | 檔案 | 大小 | 內容（實測的 key 與筆數） | 寫法 | 屬於 |
|---|---|---|---|---|---|
| 1 | `~/.config/clawdline/orchestrator.json` | **6,752,983 B** | `tasks` 769、`root_assignments` 25、`session_deliveries` 105、`closure_attestations` 50、`session_activity` 974、`session_self_states` 9、`handoffs` 0、`coordination_waits` 0、`restart` 1 | 整份重寫 | broker |
| 2 | `~/.config/clawdline/landing-queue.json` | 70,723 B | `landing_paths` **252**、`queues` **0** | 整份重寫 | broker |
| 3 | `~/.config/clawdline/coordinator.json` | 3,797 B | Clawdfather＝codex `%426`、generation 21、alias 14 筆 | 整份 CAS | broker |
| 4 | `~/.config/clawdline/coordinator-successions.json` | 5,158 B | `successions` 4 | 整份重寫 | broker |
| 5 | `~/.config/clawdline/owned-storage.jsonl` | 228,746 B | 657 行，全部是 `kind: scratchpad` | append | broker |
| 6 | `~/.config/clawdline/remote-audit.jsonl` | **5,724,449 B** | **36,010 行**，涵蓋 08-18 → 09-18（31 天），**沒有輪替** | append | broker 寫 86 處 |
| 7 | `~/Library/Application Support/Clawdline/project-board.json` | **4,361,498 B** | `items` 781、`receipts` 2,630、`taskItems` 752、`projects` 34、revision 6366 | 整份重寫 | Board（由 broker 餵） |
| 8 | `…/project-board-workflow.json`＋`project-board-history/` | 730,759 B＋2,724,094 B（558 檔） | `runs` 256、`receipts` 375 | 整份重寫＋append | Board |
| 9 | `~/.config/clawdline/project-timeline.json` | 2,495,417 B | `entries`／`events`／`eventReceipts` 各 **2000 筆（已到上限）**、`checkpoints` 13 | 整份重寫 | Timeline |

**第 6 份是原本的盤點漏掉的，而它是 broker 唯一的事後追查依據。**
`RemoteAuth.audit`（`Sources/RemoteAuth.swift:494-510`）每寫一筆就 open、seek 到檔尾、write、close，
完全沒有輪替；`recentAudit(limit: 200)`（`:518`）為了取最後 200 行，
**會用 `String(contentsOf:)` 把整個 5.7 MB 讀進來**。

**兩個數字要更正**（原盤點與派工說明都寫錯了，這裡以實測為準）：

- **`landing-queue.json` 存的不是「250 個 repository 的紀錄」。** 它的 `landing_paths` 是
  **task id → 宣告寫入路徑**，252 個 key 全部是 UUID（每筆 1–32 條路徑，中位數 6）。`queues` 是**空的**，
  因為佇列成員是**推導出來的，根本寫不進去**，只有座位順序會被存下來
  （`Sources/OrchestratorLandingQueue.swift:17-36`：「Membership is derived and cannot be written…
  Position is stored, and only position is stored」）。這個差別直接影響 Go 版的資料模型：
  **不要建一張 `landing_queue` 表**（§6.5）。
- **200 筆上限已經不是現況。** 現值是 `orchestrator_task_record_limit = 1350`（`Sources/Config.swift:531`），
  2026-09-03 由 `4eb97d86` 從寫死的 200 拆成三個獨立設定。200 那一版造成的損害有紀錄：
  **149 列 `no_durable_task_record`，永遠無法歸因**。
  舊 repo 的文件到今天還彼此矛盾：`docs/orchestrator.md:2496`、`:2612` 寫 200，
  `docs/api.md:2530`、`docs/runtime-performance.md:261` 寫 1350。

#### `tasks` 那 5.42 MB 裝了什麼

769 筆共 5,420,520 B，佔全檔的 80.3%。每列大小：min 1,024／p50 6,625／p90 10,375／max 25,169 B。
逐欄加總後，最大的六欄：

| 欄位 | 總 bytes | 佔 tasks |
|---|---|---|
| `summary` | 1,441,262 | 26.6% |
| `review` | 655,926 | 12.1% |
| `graph` | 400,963 | 7.4% |
| `executor` | 313,826 | 5.8% |
| `progress` | 268,163 | 4.9% |
| `completion_delivery` | 215,463 | 4.0% |

**前三名都是寫完就不再改的長文**：child 的結論、reviewer 的裁決、派工圖。
第四名 `executor` 是**每 10 秒都在變的觀察**（§2.2）。一份檔案同時裝著「永遠不變的長文」與
「每 10 秒變一次的時鐘」，而且兩者一起整份重寫——整個儲存問題可以濃縮成這一句。

狀態分布：success 687、cancelled 31、failure 22、timeout 13、spawn_failed 11、**briefed 5（正在跑）**。
建立時間從 08-29 到 09-18，共 20 天，平均每天約 38 筆。

#### 成長曲線

| 日期 | `orchestrator.json` | task 數 | 來源 |
|---|---|---|---|
| 2026-09-04 | 1,771,836 B | 216 | 舊 repo `docs/mac-app-shell.md` §5.2（`6fc7918f`） |
| 2026-09-18 | 6,752,983 B | 769 | 本文實測 |
| 上限 | 約 9.7 MB | 1350 | `Config.swift:519-531` 的預估（7,145 B × 1350） |

14 天長了 3.8 倍，大致跟 task 數成正比。**照目前的速率（每天約 40 筆），大約 10 月初會碰到 1350 或 30 天的上限而停止成長，
之後就開始遺忘。**

### 2.2 寫入頻率、寫入放大，以及每一次到底改了什麼

**頻率。** 每秒取樣一次 `stat` 的 mtime 與 size，連續 315 秒（`work/watch.sh`；期間機器上有 5 個 briefed task）：

| 檔案 | 重寫次數 | 平均間隔 | 期間內容淨變化 |
|---|---|---|---|
| `orchestrator.json`（6.75 MB） | **30** | **10.5 s** | **+90 B** |
| `remote-audit.jsonl` | 5 | 63 s | +655 B（append） |
| `cloud-sequence.json` | 8 | 39 s | 0 |
| `project-board-workflow.json` | 2 | 158 s | +1,410 B |
| `project-board.json`（4.36 MB） | 1 | 315 s | +2,255 B |

`orchestrator.json` 在這段時間寫了 **30 × 6.75 MB ≈ 202 MB**，存下的是 **90 bytes** 的變化。
以同樣的速率推算一天，約 54 GB。

**每一次改了什麼。** 連續抓了五次重寫前後的快照，逐個葉節點比對（task 以 id、其餘以 terminal_id 或 notice_id 對齊）：

| 重寫 | 間隔 | 改變的葉節點 |
|---|---|---|
| 1 | 10.0 s | 4× `/tasks[]/executor/inventory_generation`、4× `/tasks[]/executor/observed_at`、1× `/session_activity[]/class` |
| 2 | 2.3 s | 4× `…/inventory_generation`、4× `…/observed_at` |
| 3 | 7.6 s | 同上 |
| 4 | 20.1 s | 同上 |
| 5 | 20.0 s | 同上 |

**五次裡有四次，改的只是四個 briefed task「我又看了一次它的執行者」的時鐘。**
機制可以從程式碼逐行追出來：

1. `SessionWatch` 每讀一次畫面，`inventory.generation` 就加一。
2. `reconcileExecutor`（`Sources/SessionWatch.swift:1033-1035`）只在 **epoch 與 generation 都相同**時
   才沿用上一張收據；generation 一變，就算執行者**完全一樣**，也會回傳一張新的
   `ExecutorReceipt(status: .observed, observedAt: <新時間>, inventoryGeneration: <新值>)`（`:1036-1043`）。
3. `watch()` 的 `changed = receipt != task.executorReceipt || changed`（`Orchestrator.swift:7466`）
   因此每次都成立，接著 `beat()` 呼叫 `save()`。

**只要至少有一個 task 在 briefed，每一代 SessionWatch 讀數都會觸發一次 6.75 MB 的完整重寫。**
舊 repo 在**線路上**量過同一對葉節點：`docs/read-path-architecture.md:114-125` 發現連續的
`orchestrator` SSE 影格「differ in 2 to 3 leaves… `/at`, `/tasks/0/executor/observed_at`,
`/tasks/0/executor/inventory_generation`. **234 KB to advance a clock**」，換算每個閒置的觀看者
每分鐘約 3.3 MB。**本文補上的是磁碟這一側：同一對葉節點也在驅動持久層的重寫。**

**這個時鐘需要持久化嗎？（推論，未以實驗驗證）** 不需要。沿用收據的前提是 `inventoryEpoch` 相同
（`SessionWatch.swift:1033`），而 epoch 每個行程都不一樣，所以重啟之後，舊的 `observed_at` 本來就不會被拿來比。
判定執行者遺失，需要「同一個行程 epoch 內、兩次完整觀察相隔至少 1 分鐘」
（`docs/orchestrator.md:1243`）；重啟後也是重新計算（`:1353`）。**狀態沒變的收據，落盤後在重啟時派不上任何用場。**

**一次 save 的成本。** 這裡只有代理量測，**Swift 沒有量**：

| 量測 | 1.77 MB（舊 repo，09-04） | 6.75 MB（本文，09-18） |
|---|---|---|
| parse | 5.9 ms | 26.2 ms |
| 序列化（sorted＋pretty） | 5.0 ms | **30.8 ms** |
| 原子寫入＋fsync | 1.4 ms | 4.4 ms（中位數） |

兩欄都是 Python 代理。Swift 的參考值來自舊 repo 對 Board 的量測（`b6f4f555`）：
`JSONEncoder(.sortedKeys)` 編碼 4.03 MB 花了 38.9 ms，**佔一次 persist 的 93%**。`orchestrator.json` 用的是
`JSONSerialization`，不是 `JSONEncoder`，所以兩者無法直接換算。**結論只能說到數量級：
每 10 秒一次、數十毫秒、跑在主執行緒上。這樣不會卡死，但會造成肉眼看得到的頓挫，而且會隨保留的歷史線性變長。**
貴的是序列化，不是磁碟。

`Orchestrator.save()`（`Orchestrator.swift:10320`）的寫法讓這件事避不掉：它在 `withTaskRecords`
（**全域唯一的那把 NSLock**）裡把 769 列全部 `map` 成 `[String: Any]`，出鎖後才
`JSONSerialization.data(.prettyPrinted, .sortedKeys)`，**而呼叫它的 `beat()` 就在主執行緒上**。
`save()` 在這個檔案裡有 **56 個呼叫點**（09-04 時是 53 個），都沒有 debounce。

**跨 app 的連帶成本：** 新版 `internal/adapters/swiftstore/file.go:62-67` 依 stamp 快取，
用意是讓「decode 的成本跟 Swift app 寫入的頻率成正比，而不是跟觀看的頻率成正比」。
Swift app 每 10.5 秒寫一次，所以**在 swiftstore 還開著的期間，新 daemon 也跟著每 10.5 秒 decode 一次 6.75 MB**。
這筆成本會在 `cutover.md` 的判準 B1 完成時一起消失。

### 2.3 誰讀、誰寫

| 檔案 | 寫 | 讀（舊 app 內） | 讀（外部） |
|---|---|---|---|
| `orchestrator.json` | `Orchestrator.save()`（56 個呼叫點） | `load()`、`records()`、每一條 `/v1/orchestrator/*` | **新版 `swiftstore` 唯讀**（`plan.md` §4） |
| `landing-queue.json` | `OrchestratorLandingQueue.save` | 落地掃描、inflight、inventory | 無 |
| `coordinator.json` | `Coordinator` | 皇冠投影 | **新版唯讀** |
| `project-board.json` | `ProjectBoardStore.persist` | Board 路由；由 `ProjectBoardIntegration.observe` 餵入 | 無 |
| `project-timeline.json` | `ProjectTimelineStore` | `/v1/timeline` | 無 |
| `owned-storage.jsonl` | `OwnedStorage` | 回收前的所有權檢查 | 無 |
| `remote-audit.jsonl` | `RemoteAuth.audit`（`Orchestrator.swift` 內有 86 處） | `recentAudit` | `jq`（`docs/remote.md`） |

**派一次工會碰到幾個檔？至少五個**：`task.json`＋`CHILD.md`（task 目錄）、`orchestrator.json`（save）、
`landing-queue.json`（宣告寫入路徑）、`remote-audit.jsonl`（好幾筆）、`project-board.json`
（`ProjectBoardIntegration.observe`，`Orchestrator.swift:2926/3097/4000/8082`）。
**這五個檔之間沒有共同的交易邊界**，任何一步失敗時，其他幾個已經寫下去了。

**跨行程也沒有仲裁。** `storeSaveLock` 只在同一個行程內有效；兩個行程同時寫，結果是
「last writer wins，安靜地覆蓋」（舊 repo `docs/mac-app-shell.md:630-631`）。

### 2.4 鎖與並行模型

- **整個 registry 只由一把非遞迴的 `NSLock` 管著**（`OrchestratorRegistry.lock`，`Sources/OrchestratorRegistry.swift:53`）。
  檔頭自己記下了重構前的樣子：「one `NSLock`, about 160 bare `lock.lock()` sites, roughly nineteen
  `static var` collections behind them, and ten `…Locked()` functions whose contract was enforced by
  nothing but the suffix in their names」。W1-4 把入口收斂成四道 `with*Records`，
  但**同步原語刻意完全沒動**（`:21-25`：「No actor, no queue, no lock splitting」）。
- **所有 HTTP 請求都走同一條序列佇列**（`com.tsunamiworks.clawdline.remote`，`RemoteServer.swift:99`），
  其中有幾條路由會 `onMain` 同步跳回主佇列（`:84`、`:1123`、`:1929`、`:5218`）。
- **心跳跑在主執行緒上**：`Timer` 掛在 `RunLoop.main`（`Orchestrator.swift:6435`）。
- **終端機 I/O 走一條有界的 lane**（`enqueueTerminalCommand`）。這個做法是對的，而且是事故換來的
  （`23ae603a`：完成通知曾經繞過這條 lane，直接呼叫 `Targets.send`）。

**這個模型踩過的地雷，都有紀錄：**

1. **把「主佇列」和「主執行緒」當成同一件事。** 程式用 `Thread.isMainThread` 判斷「我是不是在主佇列上」，
   但 `dispatchMain()` 之後兩者就分開了，結果 `records()` 對自己正在跑的佇列做 `dispatch_sync`。
   **libdispatch 這時不是死鎖，而是 trap，而且不留任何訊息。**
   `b6d1939a`（2026-08-29）：「libdispatch detects that and traps, silently — which is why this cost
   a day: the failure erases its own evidence.」註記：這是**測試環境**才會發生的缺陷
   （NSApplication 會在主執行緒上消化主佇列），但它說明了「同步跨佇列」這個寫法本身的代價。
2. **同一個 beat 被重入。** `Process.waitUntilExit()` 會跑 run loop，所以 beat 往終端機打字時，
   五秒的 timer 會**在這次 walk 進行到一半時**再觸發一次。第二次 walk 拿的是舊的複本，發現 secret 已經用掉，
   就把一個正在工作、後來也順利完成的 task 標成失敗（`docs/waiting.md:28`、`CHANGELOG.md:1557`）。
   修法有三層：等待改用 `waitQuietly()`；狀態只能往前走；**重疊不阻擋，只計數**
   （`orchestrator.beat_overlap`）。計數器刻意留著，理由寫在原處：
   「a second cause would look exactly like the first one did, and silence is not evidence.」
3. **第 2 點的修法本身又造成死鎖。** 第一版把等待丟到 `DispatchQueue.global`，再用 semaphore 擋住呼叫端，
   而呼叫端 `SessionWatch.read` 本來就在那個池子裡。池子滿了以後，
   「that reading never finished and the flag saying one was in progress never cleared… **The app looked
   alive the whole time**」（`68c5a9a9`、`docs/waiting.md:46-53`）。
4. **副作用比持久化先發生。** 簡報的注入會先打進終端機，之後才 save。如果行程在這兩步之間崩潰，
   重新載入時這一列會是 `spawning`，啟動復原就把它記成 `spawn_failed`——但 child 其實已經收到那一行了
   （`docs/architecture-refactor.md:931`）。

### 2.5 心跳：每 5 秒做哪些事

`Orchestrator.start()`（`:6412-6445`）裝了三個 timer：**5 秒的 `beat`**、**60 秒的 `scheduleBeat`**、
**6 小時的 `cleanup`**。此外，`SessionWatch` 每次讀數有變化時，也會呼叫一次 `beat(fromTimer: false)`。

一次 `beat`（`:6605-6725`）依序做這些事：

1. 取 `SessionWatch` 的身分快照，重建 restart inventory。
2. **進入 registry 鎖**：修剪已關閉的 handoff 標題、認領 handoff 身分，算出 `liveIDs`／
   `liveHandoffs`／`liveRootAssignments`，再呼叫 `beginBeatLocked()`（偵測重疊）。
3. 執行 `sweepBatches()`、`scheduleLandingSweep()`、`scheduleCompletionPump()`。這三個**刻意放在
   `liveIDs` 早退之前**，因為待落地的紀錄會比任何一個 live task 活得更久。
4. 逐一處理 live task：
   - `spawning`：判兩個 deadline（4 分鐘內沒出現 prompt 就是 `spawn_failed`；attached 的 task 用它自己的 timeout），再排入 brief step。
   - `briefed`：`watch()`。**這一步產生了 §2.2 那個時鐘。**
   - 已結束的 task：回收 work／build 目錄，並收集要關掉的分頁。
5. 只要 `changed` 為真，就 `save()`，再 `broadcastOrchestrator()`。

**最壞情況：** 第 2 步在唯一那把鎖裡執行；第 5 步的 `save()` 在主執行緒上把 769 列轉成字典，
再把 6.6 MB 序列化。**這兩段之間沒有任何背壓**，只要 `liveIDs` 不是空的，每一代讀數都會再跑一次。

`watch()`（`:7423`）判定逾時只用這一行：

```swift
if let briefedAt = task.briefedAt,
   Date().timeIntervalSince(briefedAt) > Double(task.timeoutMinutes) * 60 { … finalize(.timeout) }
```

**用的是牆鐘，而且時間記在 record 上，所以重啟後判斷仍然正確**（`docs/orchestrator.md:1327`：
「A briefed task survives a restart as one logical task」）。但這個判斷只有 beat 跑到時才會發生；
app 關著的那段時間沒有 beat。重啟時設了 `restartGrace = 20`（`:6567`），讓第一次讀數有時間進來。
`spawning` 狀態**撐不過重啟**：明文的簡報 secret 只存在行程裡，所以一律 fail closed，改記成 `spawn_failed`
（`docs/orchestrator.md:1297-1300`、`:1346`）。

### 2.6 路由、熱路徑、可觀測性

- 舊 app 的 HTTP dispatch 共有 **110 個 route case**，其中 **58 個屬於 `/v1/orchestrator/*`**；
  字面路徑則有 48 條。說「20 幾條路由」低估了。
- **用 audit log 排出熱路徑**（36,010 筆，31 天）：

  | 事件 | 筆數 |
  |---|---|
  | `session.send` | 3,928 |
  | `orchestrator.worktree.kept` | **3,699** |
  | `orchestrator.message` | 3,638 |
  | `orchestrator.completion.attempt` | 3,112 |
  | `orchestrator.finish` | 1,402 |
  | `orchestrator.brief` | 1,314 |
  | `orchestrator.close` | 1,242 |
  | `orchestrator.progress` | 1,226 |

  **排第二的是一個沒有人在看的迴圈。** 那 3,699 筆 `worktree.kept` 只來自 **158 個**不同的 task，
  其中一個被記了 **188 次**；原因分布是 `dirty` 3,676、`unreadable` 23。每一次 cleanup（每 6 小時，
  外加每次啟動）都會重新發現同一批清不掉的 worktree，再重新記一次。
  這跟 `docs/orchestrator.md:2551-2554` 記錄的「任何 dirty 的 checkout 都保留」屬於同一類問題：當時量到 62 個 checkout、
  8,802 MB。修法（delta 保存好之後才移除）只處理了**已落地**的那一類。
  硬碟上的現況：`worktrees/` **517 MB**、`/tmp/.clawdline/` **1.0 GB**（53 個 task 目錄）。
- **最大的回應曾經是 `GET /v1/orchestrator/tasks`**：`a0376f5b`（2026-09-16）記錄
  「4,300,745 bytes for 716 records, unpaged — the largest response this server produces」。這一項已經修好。
- **可觀測性的現況（當場量到）：** `GET :7717/v1/health` 的 top-level key 只有
  `auth, authed, build, http_reliability, instance, ok, password, protocol, version, write`。
  **沒有 `orchestrator`，沒有 `beat`，沒有 `scheduler`，也沒有 `store`。**
  心跳停了，這條路由照樣回 `ok: true`。

---

## 3. 舊 repo 已經做過一次這個分析

`docs/mac-app-shell.md` §5「狀態層：要不要搬進 SQL」（2026-09-04，`6fc7918f`）是舊 repo 對同一個問題的
正式回答，有量測，也有逐檔的判斷。**本文的立場必須先跟它對齊，不能當它不存在。**

它的結論：

| 檔案 | 它的判斷 | 理由 |
|---|---|---|
| `orchestrator.json` | **該搬** | 唯一一個「集合」，也是唯一一個因為容器限制而**遺忘**的檔案 |
| `config.json` | **永遠不搬** | 人會用 `vi` 手改；搬進 SQL 是退步 |
| `coordinator.json` | **不該搬（現在）** | 單筆 CAS 用整檔原子改寫是**正確**的做法，換成 SQL 不會更好，只會多一個相依 |
| `remote.json` | 永遠不搬 | 它的安全性綁在檔案權限上 |
| `landing-queue.json` | 特殊 | 成員是推導出來的 |

它明確說「**現況其實不慢——所以不要用『效能』當理由**」（`:593`），真正的理由是**遺忘與查詢**。

**本文同意的部分：**

- **同意「不要用效能當主要理由」。** 就算長到 3.8 倍，§2.2 的成本也只是頓挫，還不到卡死。
  今天的數字讓效能成為**次要**理由，但主要理由仍然是遺忘（149 列不可歸因）與查詢
  （「哪些交付躺了超過 24 小時」目前要 parse 整份檔案）。
- **同意 `config.json` 永遠不搬。** 新版已經這樣做了（`internal/adapters/nextconfig`，寫 `~/.config/clawdline-next/config.json`）。
- **同意它對 `OrchestratorStore.swift:19-21` 那句警告的解讀**：「Every key string, default, legacy-shape
  branch and optional-versus-absent distinction is a compatibility contract… tidying one is a
  data-loss bug」。**新的 schema 必須分得出「有」和「沒有」，不能一律用 `NULL` 代表。**
  broker 第一波「整份 wire JSON 放進 `record` 欄」的做法，天生就滿足這一條。

**本文補上的兩條理由：**

1. **交易邊界。** §2.3 說過，派一次工會碰到五個檔，但它們沒有共同的交易。舊 repo 自己在 Board 那邊也說過同一句話
   （`b6f4f555`）：「a receipt and the state change it acknowledges must commit together or the
   idempotency guard lies in one direction or the other」。
2. **跨行程安全。** 舊 repo 也寫了：「last writer wins，安靜地覆蓋」對上 `SQLITE_BUSY`「吵鬧地失敗」，
   而「**兩邊都會壞，但壞的方式不同，而 SQLite 那種比較好**」（`mac-app-shell.md:630-636`）。
   新舊兩個 app 並存的這段時間，這一條比 09-04 那時更重要。

**本文不同意的一點：`coordinator.json` 要跟 task 放在同一個 store。**
它主張不搬的理由有兩個：「SQL 不會更好」與「多一個相依」。第二個理由在 Go 版不成立，因為 SQLite 已經是
**唯一的** store，而且新版 HEAD 已經有 `coordinator` 表（`internal/adapters/store/sqlite.go` 的 schema 裡，
含 `generation` 欄）。放進同一個 DB 的好處是：Clawdfather 換手時，可以跟它負責的 task 狀態放在**同一個交易**裡。
CAS 的語意照搬即可，寫成 `UPDATE coordinator SET … WHERE generation = ?`。

**它當時建議的第一步是「先修 `save()` 的重複寫入並加 debounce」**。重複寫入後來修掉了
（`docs/runtime-performance.md:263`），**debounce 到今天仍然沒有**（56 個呼叫點）。
本文在 §2.2 找到比 debounce 更根本的問題：寫入的觸發條件本身就不對。

---

## 4. 已修好的痛點：修法照搬

每一項的寫法是：**它是什麼 → 為什麼發生 → 舊版怎麼處理 → Go 版怎麼做**。
證據是 commit sha（共 1,724 筆，2026-08-15 → 09-17）或檔案行號。
（這段歷史裡**沒有任何一筆中文 commit 訊息**，所以下面的引文都是英文原文。）

### A. 儲存

**A1 整份 JSON 重寫——連設定檔的註解都把這筆帳寫出來了。**
`Sources/Config.swift:519-531` 解釋 1350 這個上限為什麼存在：「It exists because `orchestrator.json` is
serialised and rewritten whole on every write and projected whole by `Orchestrator/records()`,
**both on the main queue**, so a burst has to hit a ceiling somewhere.」舊版的處理方式是**加一道限流閥，
而不是修掉底層的問題**。→ **Go 版：重新設計（已經在做）。**

**A2 Board 也走過同一條路，量得還更細。**
`f48772db`：5,032,820 B 的文件裡，`items[].history` 佔 1,835,259 B（36.5%），「for a log no mutation reads」。
修法**刻意選了繞道**：歷史搬到 append-only 的 log，文件只保留 20 筆的視窗。
`6e043845`：「Twenty was measured wrong the first time — it is a **per-item** number」，於是再砍到 5 筆。
`85cf6003`：每個 envelope 都複製一份 project catalog（1.17 MB），1.23 s → 0.193 s。
`b6f4f555`：**拒絕**把 receipts／evidence 拆成分段檔，理由是數字（只省 16 ms），而且收據必須跟狀態一起提交。
→ **Go 版：「收據與它確認的狀態同一個交易」照搬；「log 不放在聚合裡」照搬。**

**A3 store 壞掉，不等於 store 是空的。**
`6b083838`＋`Sources/OrchestratorPersistence.swift`：格式錯、缺 `tasks`、版本不認得、是目錄或 symlink——
**這些都不能當成「空的 registry」處理**。被拒的 bytes 會以內容定址的方式複製到
`orchestrator-quarantine/`（0600），路由回 `503 orchestrator_store_unavailable`，快照直接**省略**
`tasks`／`schedules`，而不是告訴手機「工作都不見了」。修復**刻意不在行程內偵測**，交給人重啟。
身分也**永遠不靠重新產生一個來自我修復**（`docs/orchestrator.md:2484`）。
→ **照搬，一字不改。**

### B. 並行與死鎖

**B1 對自己所在的佇列同步派工，會靜默地 trap。** 見 §2.4 第 1 點。
→ Go 沒有這種 trap，但對應的錯誤是 **self-deadlock**：持有一把 `sync.Mutex` 時又去取同一把。
**設計上要禁止「持鎖時呼叫任何會做 I/O 的東西」。**

**B2 在自己所在的執行緒池上，等自己派出去的工作。** 見 §2.4 第 3 點（`68c5a9a9`）。
→ goroutine 很便宜，但「in-progress 旗標永遠不清除」這種形狀在 Go 一樣會出現。
**每一個這類閘門都要有逾時與過期，而且它存在了多久要能在 `/v1/diagnostics` 看到。**

**B3 終端機 I/O 只有一條 lane，沒有例外。** `23ae603a`。→ **照搬。**

**B4 「drained」曾經是假的。** `85ecdad5`：pump 佇列只是**轉手**，被拒的 admission 要 0.25 秒後才回來，
所以「drained」在 promotion 還在 lane 裡時就已經是 true。**這個 bug 在閒置的機器上測得過，在忙碌的機器上會紅。**
→ **測試不能拿「佇列空了」當作完成的證據，要看收據。**

**B5 副作用必須在交易落盤之後才發生。** 見 §2.4 第 4 點。後來的一波修改把五個 best-effort 的副作用
移到了「save 成功之後」（`6b083838`：通知、注入、刪除都「only after the owning transition is durably saved」）。
→ **照搬，這也是 `plan.md` §9 的 reactor 形狀。**

### C. 卡死，而且不會自救

**C1 主執行緒四次被同步呼叫卡住，四次都只是把那個呼叫拿掉。**

| sha | 日期 | 卡在哪 | 修法 |
|---|---|---|---|
| `68c5a9a9` | 08-25 | 執行緒池自我阻塞 | 等待改用獨立的執行緒 |
| `d5c61e9c` | 08-24 | `AudioUnitInitialize`（HAL 卡住）——「an app in that state keeps its window and answers nothing: not the bar, not the hotkey, not HTTP」 | 沒裝 tap 就不去碰那個屬性 |
| `4622b258` | 08-31 | 啟動時跳出的 Keychain 對話框 | 移出主執行緒 |
| `c5b43bc4` | 08-31 | 同一個缺陷藏在上一次修正的下面——「the third time in this feature that a guard could not fail」 | 邊界化：`CloudKeychainStore` 用具型別的 `mainThreadReadForbidden` 拒絕主執行緒讀取 |

→ **四次都沒有加上自救或偵測。這是舊 app 至今仍然存在的性質，不是已經修好的事故。**
值得照搬的是 `c5b43bc4` 的修法：**把危險的呼叫關進單一檔案，用具型別的錯誤拒絕錯誤的呼叫端**。

**C2 「我現在沒有答案」被當成「我沒有答案」送出去。** `413c1566`：「a full lane is `429 busy`, a sheet in
iTerm2 is `502 iterm_attention_required`, an incomplete inventory is `work_state: 'unknown'`. All three
mean *I do not have a current answer* and all three are delivered as *I have no answer*」；以及
「the app could not answer '**was it slow, or was it queued**' about itself — guessing which is which
is how a queue-shaped problem gets a timeout bolted onto it」。
→ **照搬。** 這跟這台機器家規裡「慢送出／pending 不退，不能只補 timeout」是同一句話。

**C3 自救的邊界，舊版是刻意畫的。** app **從不監看 root 是否死亡**：「'not in this reading' is a sentence
that is also true of a terminal that lost its accessibility permission for a moment, and the cost of
being wrong there is somebody's work killed mid-turn」（`docs/orchestrator.md:1274`）。
執行者不一致時，**從不自動 respawn 或重新連結**，它的 `mover` 是 `person`（`:1334`）。
→ **照搬。** 本文 §6.6 提的自救，**只限於 broker 自己的 goroutine**，不碰任何人的工作。

### D. 派工（spawn_failed）

**D1 spawn_failed 誤判了一個正在工作的 child，刪掉了它的 worktree 和交付分支。**
`72ed3e46`／`95f6a30b`（2026-08-28），`Orchestrator.swift:6866-6871`：`finalize` 把「`spawn_failed` 而且
`briefedAt == nil`」讀成「這裡什麼都沒發生過」，於是執行 `disposeWorktree(why: "empty", allowCommitted: false)`。
**同一個小時內有兩個姊妹 task 被誤判：`ffe1b91b` 在第 109 秒已經 commit，所以全身而退；
`2a995bd5` 還沒 commit，結果在它的分頁還在那個目錄裡工作時，checkout 和交付分支都沒了。**
根因不是 app 重啟，而是 iTerm2 早 68 秒啟動、帶著 root 的 `CLAUDE_CODE_*` 環境變數，每個新分頁都繼承了，
於是 transcript 檔從來沒被寫出來，只剩四分鐘的 deadline 會觸發。
修法**繞開了壞掉的那條通道**：progress note 是用 task secret 認證的，而 secret 只會從一個地方到達 child，
**所以只要收到一筆 note，就等於第二條通道的送達收據**（`briefingProvenByProgress`／`acceptProgressAsBriefing`，
`:6876-6901`）。`expireSpawningIfDue` 會在 deadline 觸發**之前**先收 `progress.json`。
→ **照搬，並把「跟 child 說過話的 task，永遠不走 empty 回收」寫成不變量。**

**D2 打進 tty 不算簡報成功；最多重打五次。** 只有助理自己的 transcript 裡出現這個 task 的第一句 user turn，
task 才會轉成 `briefed`；重打五次仍不成功就記為 `spawn_failed`（`docs/orchestrator.md:1216-1217`）。→ **照搬。**

**D3 spawn_failed 一度佔 206 次派工中的 34 次，而協定給的解法是叫 root 把整份 task.json 重寫一遍。**
`52d74c88`：「thirty-four rewrites by the most context-loaded session in the tree, each one a chance to
drop a field」。修法是 `POST /v1/orchestrator/tasks/:id/respawn`，**複製 `task.json` 而不是 record**；
每個原始 task 家族最多 2 次，**沿著整條鏈計算**；超過就回 `409 respawn_exhausted`。
→ **照搬。`cutover.md` §2.2 的清單沒列到這條路由，這裡補上。**

**D4 那個比率後來失真了，原因就是 200 筆的上限。** `8aec5025`：「both readings are rolling windows over
different populations… A reader comparing them would see a trend that does not exist.」
→ **任何從 registry 算出來的比率，都要跟它的統計視窗一起報。**

**D5 失敗的 spawn 會留下一個還活著的助理，而那正是下一次 spawn 失敗的原因。** `3e37e8ec`：「four dead tabs
were still running when this was found, and the next two spawns timed out for exactly the reason the
first four had」。→ **照搬：`spawn_failed` 必須把它開的東西關掉。**

**D6 權限模式選錯，事後看起來像一次什麼都沒做的逾時。** `--permission-mode auto` 在 Haiku 上會默默變成
`manual`（每一步都要人按）。舊版的詞彙裡**刻意沒有 `auto`**，只有三個值，而且每個都在三個模型上
從真實的狀態列讀回來驗證過（`docs/orchestrator.md:242-246`）。→ **照搬這三個值。**

### E. 收工：child 靜默失敗、結果沒有人收

**E1 完成通知曾經只是一次 send，而不是一筆記錄下來的事實。** `f05ed2b3`：「If the terminal was busy, the app
restarting, or the root's process replaced, the notice was gone and the only trace was a `result.json`
nobody was told about.」修法：durable notice 先寫進 store，才交給 pump；跨重啟使用同一個 `notice_id`；
有上限的退避（`min(5·2^(n-1), max)`，`Orchestrator.swift:8444`）；具型別的 dead letter；
**delivery／observation／ACK 三者分開記錄**；ACK 用 CAS 回滾它看到的那一次 transition。
→ **整條照搬。這是整份舊程式裡品質最好的一段。**

**E2 `result.json` 卡在 child 的 shell 裡。** `9d1676e9`：broker 只有在 marker 與 tmp bytes **在相隔至少 30 秒的
兩次觀察中完全相同**時，才可以代替 child 建立 `result.json`；**重啟就重新計時；mtime 或存在時間本身，
永遠不能作為復原的依據**；發布方式是 create-if-absent。→ **照搬。**

**E3 格式錯的收據曾經被靜默忽略。** `5fb90f86` 把 preflight validator 直接內嵌進每一份簡報。secret 錯誤時
**只忽略、記一次 log**：「a wrong secret in a task directory is either a bug or somebody poking,
and neither is a reason to finalize somebody's task」。
→ **照搬。** 新版已經有這個檔：`internal/app/orchestrator/result-preflight.js`（broker 第一波）。

**E4 兩份出貨的指南都說 progress note 會叫醒 root，實際上從來不會。** `825fc32e`：「a child sent two progress
notes, the second one asking its root a question outright, the root received neither, and the child
decided alone」。舊版只改了文件。
→ **小改**：progress 不主動往 root 的終端機打字（`6fe569f1` 說明了為什麼：那等於在 root 正在等回應的那一輪裡打斷它），
**但要進 SSE**，讓 root 的畫面看得到；真的要叫醒 root 的 child，就用 `/notify`。

**E5 child 分頁的 linger deadline 只存在記憶體裡。** `3a7adb8e`：「**Seventeen of the eighteen tabs left
standing** had a restart inside their three minutes」。修法是持久化 deadline、加 20 秒 grace，而且
「a reading with no terminals in it at all is no longer allowed to decide that」。
→ **照搬。這是 Go 版最容易再犯的一類錯：deadline 放在 `time.AfterFunc` 裡，重啟就沒了。**

**E6 判定執行者遺失，需要兩次觀察。** 要在同一個行程 epoch 內、有兩次完整的觀察、而且相隔至少 1 分鐘，
才能判定 `executor_missing`；重啟後的對帳上限是 120 秒，超過就記 `reconciliation_timed_out: true`
（`docs/orchestrator.md:1243`、`:1353`）。→ **照搬。** 這也是 §6.2 可以把觀察時鐘留在記憶體的原因。

### F. 保留與清掃

**F1 200 筆上限掃掉了用量分類器唯一的持久證據。** `4eb97d86`：「at this machine's measured 45 dispatches a
day it swept a task record after about five days… which is why **149 rows report
`no_durable_task_record` and can never be attributed**」。修法是拆成三個設定。
舊 repo 另一側的量測（`docs/mac-app-shell.md:558`）：「`usage_intervals` 有 **157** 個 distinct 隔離任務……
而 `orchestrator.json` 只剩 **216** 個 task。兩份資料量同一批任務，一份被刪過，一份沒有。」
→ **重新設計**：見 §6.1 的三層保留。

**F2 上限變成可設定的值之後，測那個上限的測試就再也碰不到它了。** `794d3a53`：「**the group would have gone on
passing while testing nothing**」。→ **照搬：上限必須可以注入，測試也要注入它。**

**F3 清除曾經移掉擁有者還活著的列；24 小時的清掃刪掉過一個還在工作的 Root Session 的目錄。**
`7f70d979`，以及 `docs/orchestrator.md:2506`：「On 2026-09-11 three finished tasks on this Mac still had
their recorded process running 7 to 21 hours after finishing」。修法：凡是任何一個上限可能移除的 terminal 列，
都要讀它的擁有者是否還活著；讀不到就保留。→ **照搬。**

**F4 在讀取路徑上做全 store 的 prune，而且是在全域鎖裡。** `af83249d`：「reads and decodes every record in the
store, **twice** — on every call… Under the global store lock」。→ **設計規則：清掃不掛在讀取路徑上。**

**F5 保留清單從來沒被讀到，結果移掉了 25 個 worktree。** `docs/landing.md:345`：「a list that was never read
prints exactly what a list of nothing-to-keep prints」。→ **照搬：「讀不到」必須是具型別的，不能等同於「空」。**

**F6 每一輪清掃都有上限，而且輪流推進。** 每一輪最多 64 項，用輪轉游標，確保排在很後面的項目也輪得到
（`docs/orchestrator.md:1658-1667`）。→ **照搬。**

### G. 落地紀錄

**G1 落地紀錄的鎖只會越鎖越緊，不會解開。** `Sources/OrchestratorLandingSweep.swift:3-11`：「On 2026-09-06 one root's card
read 「還有 21 項未了結」 — 12 pending landings and 9 touched-claim rows, all from twelve children that
had reached `success` months of machine-time earlier」；`docs/session-closeability.md:213`：「**A lock that
only ever tightens is not a lock, it is a ratchet.**」修法是**兩條彼此不能冒充的證明**：
- **Arm 1 ancestry**：必須先有完整乾淨的 worktree 收據，才能問「delivery head 有沒有被 `refs/heads/<target>` 包含」；
  驗證走的是跟 HTTP 路由**同一條**路徑，所以驗證欄位都是真的。
- **Arm 2 write-set containment**：適用於共用 checkout 的 task。**每一條 claim 都必須能正面解析**，而且在
  **相隔至少 5 分鐘的兩次讀數**裡都乾淨。它證明的命題窄很多，所以**完全不帶驗證欄位**。
  「兩次讀數」這條規則有實際的代價當證據：「four rows clean at 20:24 were dirty again at 20:39」。
  「正面解析」這條也有：用空格串起來的三個檔名，被當成一條不存在的路徑，`git status` 回「exit 0、沒有輸出」，
  跟乾淨的結果一模一樣，**結果兩次試跑都把三筆紀錄關掉了**（`docs/landing.md:393-395`）。
→ **整條照搬，包括「兩條 arm 在型別上不能互相冒充」這個分離。**

**G2 落地紀錄一度改不回來，而且更正的人還被告知成功了。** `Orchestrator.swift:5335-5350`：「**Durable is not the
same as unamendable, and the lie was never the immutability.**… a resend that disagrees *is* a
write, and the two honest answers to a write are 'applied' and 'refused'. Being told `ok` for
neither is how **a record on this machine came to name another task's commit permanently**」。
現在的做法是：更正必須通過第一次落地時的同一道閘門，舊值以 `corrected_from` 帶回，並寫進 audit。→ **照搬。**

**G3 只拿紀錄上寫的那個 ref 比對，絕不換成一個「更好」的。** 12 筆紀錄裡有 3 筆寫的分支落後 `main` 11 個 commit，
但它們的工作其實已經進了 `main`。「**Which branch a record should have named is a person's decision**」
（`docs/landing.md:432-439`）。→ **照搬。**

**G4 落地是 git 上的事實，不是 registry 裡的一個欄位。** `e924dd9a`：Projects 頁說 53 個「已交付未落地」，
git 說其中 24 個已經是 HEAD 的祖先、13 個分支已經不存在，而真的去問 git 的落地佇列當晚說是 17 個。
→ **照搬。** 新版 `internal/app/landing.go` 的 `Lander.Land` 已經用 ancestry 證明，方向正確。

**G5 「decode 得出來」和「算不算證據」曾經被當成同一個問題。** `10d460f6`。
→ 舊的 landed 列如果沒有驗證欄位，一律標成 `legacy_unverified`。

**G6 掃描的成本，以及為什麼它有自己的佇列。** 最多每 5 分鐘一次、每次不超過 20 筆，約 140 個子行程；
在自己的序列佇列上跑，從不在讀取路徑上，也從不在 registry 鎖裡（`docs/landing.md:414-429`）。→ **照搬這些上限。**

### H. 落地佇列（一個 repo 一個槽）

**H1 兩個 session 被放進同一個 `.git/index`。** `ede95dc8`：「Two branches are two refs and **one `.git/index`,
one staging area and one `HEAD`**」。同一個 commit 還修了另一個缺陷：`advance` 會把「落地槽是你的」
打進一個 child 還沒回來的 session。現在改回具型別的 `no_ready_candidate`。

**H2 readiness 讀的是理由標籤，而不是事實。** `1e385141`。

**H3 順序是推導出來的，從不儲存。** `ed9c6152`：「a stored order would have to bump `Order.generation`, which
**re-arms a slot notice nobody asked for**」。兩個方向的失敗處理刻意不對稱：git 完全答不出來的 repository
在排序時 **fail closed**（「ordering an unreadable one costs a landing」）。

**H4 `claims: []` 會被讀成一個承諾。** 隔離 task 的 claims 會被清空，於是「一個寫了 26 個檔的交付，讀起來跟
一個什麼都沒寫的 review 一模一樣」（`docs/landing.md:236-242`）。修法是另外保留 `landing_paths`。
→ **這就是 §2.1 那 252 筆的由來。H1–H4 全部照搬，而且照搬成「不建佇列表」。**

### I. 家規（dispatch policy）

**I1 「是複本不是 symlink」這個說法方向反了，而且真實損害有量過。** `bf86ddb7`：「a paragraph recording that codex
sandboxes here now reach the network was gone within the hour, **11,994 characters down to 11,667,
with nothing in `git status` and no error anywhere**」。修法是加一個**永遠不會被寫入**的
`dispatch-policy.local.md`，排在最後讀；超過長度上限時**切掉的是 base**。`ae97de92` 之後，這台機器上的
policy 一度是 symlink。**今天當場量到它是兩個內容相同的普通檔**（SHA-256 都是 `55b2c30f…`，mtime 差 2 分鐘）。
**另一個當場量到的風險：policy 現在是 15,091 字元，上限 16,000，已經用了 94%。**

**I2 `defaultPolicy` 曾經是一個過時的字串常數。** `8a537a54`：「**A machine could start life with rules nobody
had read for months.**」→ 見 §6.8。

### J. 等待者（編譯插槽，跟 broker 相鄰，但是同一類錯）

`b2f25048`：「a waiter whose process died at the head of the queue left an entry that could never be
granted… **while the lock itself was free**… **thirty-two of them became `queue_full` for the whole
machine**」。`24a33139`：「**One ordinary Ctrl-C turned the machine's compile slot into a permanent
roadblock**」。`15924b14`：「**A clock on the work is wrong; a clock on the proof of life is right.**」
`b2a64352`：renewal loop 只要讀錯一次就整個結束。
→ **這四句話就是 Go 版所有等待者的設計規格**（落地槽、驗證插槽、terminal lane、completion pump）。

---

## 5. 舊版到今天仍未修的缺陷——不照搬

以下每一項都在舊 repo 的 `docs/backlog.yaml` 裡（逐條核對過行號），或是在文件裡明寫「未解決」。
**Go 版不能照搬這些形狀**，否則等於把一份已知的 bug 清單一起帶過來。

| # | 缺陷 | 舊 repo 證據 | Go 版的做法 |
|---|---|---|---|
| O1 | **`pending` 同時代表兩件相反的事**：有人正在做，或執行者已經死了。「the obligation therefore looks healthy while it is dead」；有一條線在一筆 `pending` 後面卡了 14 小時 | `backlog.yaml:608` `B-PENDING-CANNOT-SEE-ITS-EXECUTOR`；`orchestrator.md:1289` | **重新設計**：拆成 `pending_live` 與 `pending_orphaned`，由執行者收據推導，不另外儲存 |
| O2 | **落地槽的 `advance` 找不到活著的持有者**：持有者用 conversation id 命名，卻用 terminal id 去查。「Not measured: whether… this route has ever delivered.」 | `backlog.yaml:1679` | **重新設計**：session 身分只用一個命名空間，由契約型別強制 |
| O3 | **重新切分落地的交付，會永遠留在佇列裡** | `backlog.yaml:898` | 小改：成員推導時，除了祖先關係，也接受 landing 紀錄的 `verified_commit` |
| O4 | **重啟後，格式錯的暫時身分會耗盡簡報重試次數** | `backlog.yaml:448`（degrades／hours） | 照 E6：身分不一致時交給人處理，不消耗重試次數 |
| O5 | **Root Assignment 被重複打進同一個 root 兩次，而且活著的 root 被記成 `failed/prompt_timeout`** | `cloud-error-transparency.md:344` | 小改：簡報注入以 receipt 做冪等鍵 |
| O6 | **只為了探測而送出的值，會永久佔住冪等槽** | `backlog.yaml:1459` | 小改：寫入路由加一個 `validate_only` 模式。舊文件也說「a receipt that can be edited afterwards is a worse thing」，所以**收據仍然不可變** |
| O7 | **落地 note 過長時被拒，卻沒說是哪個欄位** | `backlog.yaml:667` | 小改：具型別的拒絕帶上 `field` |
| O8 | **格式錯的 `verification` 被靜默略過，而不是投影成 invalid** | `backlog.yaml:752`；`verification-workflow.md:299` | 小改：投影成 `invalid` 並附上原因 |
| O9 | **編譯租約把看不到的 pid 當成已經不在**——fail open | `backlog.yaml:1646-1652` | 照 J：看不到＝`unknown`，不能當作「不在」 |
| O10 | **Graph 的 landing 節點只看 `state == .landed`，不看驗證欄位**。舊版選擇改文件來配合程式 | `backlog.yaml:1217-1222` | 小改：節點要求 Arm 1 的驗證欄位 |
| O11 | **只看首次安裝的設定，永遠不會更新到已經在跑的機器** | `backlog.yaml:639-648` | 見 §6.8 的投影 |

另外有兩項已經修好、但屬於同一族、值得寫進 Go 版不變量的：
**inflight 對 linked worktree 永遠回答「沒有人」**（`CHANGELOG.md:200`：所有 task 都掛在它被切出來的那個 repository 下，
所以列表永遠對不上），以及 **SSE 每次變動都送出整份任務列表**（`read-path-architecture.md:114-125`；Cloud 那條路徑
早就用 `publishOrchestratorIfChanged` 解決了，本機這條卻沒有）。

---

## 6. Go 版 broker 的設計

> 目前狀態：in-flight task `0ca0b8c0`（「broker 第一波：派工到收工的完整迴圈」，分支 head `29a5cf5`）已經寫了
> 6,074 行，包括 `internal/adapters/store/broker.go`、`internal/app/orchestrator/{broker,dispatch,brief,watch,lifecycle,inventory,messages,notice,record}.go`
> 與 `api/v1/orchestrator.schema.json`。**下面的設計在它已經做了選擇的地方，會寫「已選，理由是這個」；
> 在它還沒做到的地方，會寫「接下來要這樣做」。本文不動它的檔案。**

### 6.1 儲存：SQLite、三層保留、長文與熱狀態分開放

**決定：不再用一份大 JSON。** 這也是 `internal/adapters/store/sqlite.go` 原本的方向，理由寫在檔頭：
「the three writes that must happen together — the events, the projection they change, and the receipt
that says the command was accepted — are one transaction or they are a race」。

**交易邊界的規則：一個外部可以觀察到的事實，就是一個交易。**

| 指令 | 同一個交易裡要一起落盤的東西 |
|---|---|
| 派工被接受 | `broker_tasks` 新增一列（`queued`）＋ claims 佔用 ＋ events ＋ receipt（`Idempotency-Key`） |
| child 回報 progress | note ＋（如果還在 `spawning`）狀態轉成 `briefed` ＋ event |
| 收到 `result.json` | 狀態轉成 terminal ＋ durable notice ＋ events |
| ACK | notice 轉成 `acknowledged` ＋ event（CAS 只針對它看到的那一次 transition） |
| landing | landing 列＋證據欄位＋關閉 obligation＋event |
| Clawdfather 換手 | `coordinator` 列（`WHERE generation = ?`）＋它交接的 obligation＋event |

**刻意不放在同一個交易裡的**：開分頁、打字、刪 worktree、送推播。這些是 reactor 事後處理的副作用（B5），
**成功或失敗都要用另一個指令回報回來**。

**schema 的形狀（第一波已選，本文同意）**：一個 task 一列，`record` 欄放整份 wire JSON，
只把真正會被拿來過濾的欄位拉出來。理由在 `broker.go` 檔頭：「a column per protocol field makes every
protocol addition a migration on the machines that have been running longest」。**secret 只存 SHA-256，
而且獨立成一欄。**

**要補的：長文分開放。** 見 §2.1：`summary`＋`review`＋`graph`＋`progress` 佔 task 列的 **46%**，
而且寫完就不會再改。

```sql
CREATE TABLE broker_task_text (
  task_id TEXT NOT NULL,
  kind    TEXT NOT NULL,   -- summary | review | graph | plan | instructions
  at      INTEGER NOT NULL,
  body    TEXT NOT NULL,
  PRIMARY KEY (task_id, kind)
);
```

代價是 `GET …/tasks/:id` 要多一次 join。好處是熱路徑完全不會碰到這些長文——
這正是舊版 `eaa20bbc`（「four fields were 71% of that」）只能靠「換一種投影」繞過去的問題。

**要補的：`busy_timeout` 要明確決定。** 目前 `sqlite.go` 只設了 `journal_mode=WAL`、`synchronous=FULL`、
`foreign_keys=ON`，第一波的分支上也沒有 `busy_timeout`，所以預設是 0：只要另一個行程
（例如 CLI 子命令或備份）正在寫，就會立刻回 `SQLITE_BUSY`。
舊 repo 對這一點的提醒：「`busy_timeout=5000` 意味著一次寫入最壞會卡 5 秒——所以寫入必須放在自己的
序列佇列上」（`mac-app-shell.md:636`）。`SetMaxOpenConns(1)` 就是這條序列。**建議設 `busy_timeout=1000`，
並把 `SQLITE_BUSY` 轉成具型別的錯誤、計進 diagnostics。**`synchronous=FULL` 維持不變。

**要補的：備份。** 備份 WAL 模式的 DB 不能用 `cp -R`。要用 `VACUUM INTO`，並在 `cutover.md` 的判準 B5 寫明。

**三層保留（取代「一個上限配一把掃帚」）：**

| 層 | 保留什麼 | 建議值 | 依據 |
|---|---|---|---|
| L1 工作目錄 | `/tmp/.clawdline/<id>/`、worktree、build 產物 | 24 小時（沿用）＋擁有者還活著就保留（F3） | 實測 `/tmp/.clawdline` 1.0 GB、`worktrees/` 517 MB |
| L2 熱列 | `broker_tasks` 的 `record` | **不刪** | 每年 38×365×7 KB ≈ 97 MB |
| L3 長文 | `broker_task_text` | 90 天，可設定 | 這就是 F1 那 149 列被掃掉的東西 |

**這一項評為「重新設計」而不是「小改」，理由是：** 舊版的上限是**為了保護寫入路徑才存在的**
（`Config.swift:519-531` 明說了）。寫入路徑換掉之後，這個理由就不存在了。
`owned-storage.jsonl` 存在的理由也一樣（「The ledger is independent of `orchestrator.json`: the ordinary task registry keeps only its
newest 200 settled rows」，`docs/orchestrator.md:1568`），所以在 Go 版它可以變成一張表，
**但「未知就保留」這條規則要照搬**（`:1587`）。

**遷移與凍結：** 沿用 `cutover.md` §3 已經做的決定，**不轉檔**。

> **分級：重新設計。** 代價：大部分第一波已經付了；剩下的長文分表、三層保留、busy_timeout、備份，估一個 child 的工作量。
> 風險：中。**索引選錯了，在 769 列時看不出來，要到 13,000 列才會看出來。**

### 6.2 事實落盤，觀察不落盤

**這是 §2.2 直接推出的規則，也是本文最重要的一個新建議。**

| | 事實 | 觀察 |
|---|---|---|
| 例子 | 狀態轉換、收據、claims、landing、notice 狀態、`briefedAt` | `executor.observed_at`、`inventory_generation`、`lastSeenChild`、`session_activity.class` |
| 重算得出來嗎 | 不行 | 可以，下一次讀數就有 |
| 放在哪 | SQLite，同一個交易 | 記憶體 ＋ `/v1/diagnostics` ＋ SSE 的即時欄位 |
| 重啟之後 | 必須存在 | 重新讀一次即可（E6：跨 epoch 本來就不比對） |

**規則：** 只有 `status` 改變（`observed → pending → executor_missing`）才寫入；
`observed → observed`（generation 不同）只更新記憶體。

**SSE 也用同一條規則。** Go 版現在的 `internal/transport/http/events.go:123-137` 會把上游每一個 orchestrator delta
**都換成一份整份的列表**。在代理期間這可以接受（前 50 筆），但 broker 自己擁有這條串流之後，
要用 Cloud 那條路徑已經證明可行的做法：**計算投影 identity，跟上一次一樣就不送。**

> **分級：重新設計。** 代價：很小。風險：低。**效益是全文最高的之一**：預期可以把 §2.2 那 30 次重寫中的
> 29 次消掉（推論，Go 版尚未實測）。

### 6.3 並行：一個擁有者、一個 beat、所有 I/O 都在鎖外

**狀態的擁有者是 SQLite，不是記憶體裡的結構。**

**規則（每一條都對應一次事故）：**

1. **持鎖時不做 I/O。** 對應 B1、F4。**守衛要能真的執行**：鎖包在一個型別裡，它的方法只接受純函式，
   需要 I/O 的一律回傳 `[]effect`，讓呼叫端在鎖外執行。
2. **只有一個 beat，不可重入。** 第一波的 `Watch`／`Pass` 已經是單一 goroutine＋`time.Ticker`，**比舊版的
   Timer＋observer 雙觸發好**。保留 overlap 計數器的概念。
3. **終端機 I/O 只有一條有界的 lane，沒有例外。** admission 被拒是背壓，不是送達失敗。
4. **每一個 in-progress 旗標都要有過期時間，而且看得到它存在了多久。**
5. **不拿「佇列空了」當作完成的證據，要看收據。**

**一個請求的鎖範圍：** 讀的路徑直接 `SELECT`；寫的路徑用 `SetMaxOpenConns(1)`，**背壓就在這裡**。
所以交易裡不能有 I/O。**要量的數字是寫交易的 p99，而且要放進 `/v1/diagnostics`。**

> **分級：擁有權重新設計，規則照搬。** 風險：低。但規則 1 的守衛一定要真的寫出來，否則它會跟舊版那十個 `…Locked()` 後綴一樣，只是一個慣例。

### 6.4 逾時與重送

**逾時由 broker 的 beat 判定，判準記在 record 上、用牆鐘。** 照搬 `watch()`。**要補的三件事：**

1. **重啟 grace 照搬**，而且照搬 `3a7adb8e` 的「**空的 inventory 不可以判任何人死亡**」。
2. **重啟後補判的通知要合併。** app 關一整晚之後，開機的第一個 beat 會一次判死所有過期的 task。
   建議：重啟後第一輪的 finalize 合併成**一則**摘要通知，個別的 notice 仍然寫進 store。這是**小改**。
3. **`spawning` 的兩個 deadline 在 record 上判**，而且**開火前先收 `progress.json`**。

**完成通知的重送與 ACK 冪等：整條照搬 E1。** **Go 版要加的**：把 notice 放進獨立的表（`broker_notices`），
這樣「ACK 不能蓋掉同時發生的其他收據」這個危險就不存在了。**這是小改。**

**attached task 最多會用掉兩倍的 `timeout_minutes`**（`docs/orchestrator.md:669`），這是舊版接受並寫下的上限。
→ **照搬，並寫進契約的說明。**

> **分級：逾時照搬，加兩處小改；重送與 ACK 照搬（表結構小改）。**

### 6.5 落地紀錄

**可以修改嗎：可以，而且必須可以。** 規格寫成基礎，而不是形狀：

> **更正一筆落地紀錄，要通過它第一次落地時通過的同一道閘門。舊值以 `corrected_from` 保留，並寫進 audit。
> 已結算的狀態仍然不能變成另一個狀態。任何情況下都不能回一個實際上沒有發生的 ok。**

**誰來驗證：broker 自己，用 git，而且只關閉自己證明得了的紀錄。** 照搬 G1 的兩條 arm。
新版 `Lander.Land` 已經是 Arm 1 的形狀；**Arm 2 與它的穩定視窗還沒做**。

**跨 repo 佇列要保留，但不要建表。** 照搬 H1–H4。

```
members(repository) = 由 broker_tasks 推導（workVisibility ∈ {live_work, pending_landing}）
order(repository)   = coordinator 設的 root key 順序（這個要存），其餘標 unplaced 排在後面
turn(repository)    = 第一個 ready candidate（推導）
```

**同時修掉 §5 的 O1、O2、O3。**

**舊版自己寫下、到現在還開著的缺口：** 如果有人沒建 Clawdline task 就直接在 checkout 裡工作，broker 完全看不見他。
**Go 版一樣看不見。這一條要寫進文件，不要假裝已經解決了。**

> **分級：可修改性照搬；雙 arm 驗證照搬（Arm 2 待做）；佇列照搬「不建表」；`pending` 拆分與單一命名空間要重新設計。**
> 風險：中。**Arm 2 寫的是永久紀錄，而它證明的命題只在某一瞬間成立**，所以「兩次讀數」這條規則不能省。

### 6.6 可觀測性：壞掉的時候，什麼會發出警報

**規則一：巡邏本身要能被看見。** 新版 `plan.md` §3.2 已經為排程器寫下這條，**broker 的 beat 用同一條**：

```json
{"broker": {
  "beat": {"at": 1789690666, "tick_seconds": 5, "passes": 12043,
           "last_duration_ms": 8, "p99_duration_ms": 41, "overlaps": 0, "restarts": 0},
  "watched": 5, "settled": 0, "notes": 2, "timed_out": 0, "spawn_failed": 0,
  "notices": {"pending": 1, "delivered": 0, "dead_letter": 0, "oldest_pending_seconds": 34},
  "store": {"status": "ready", "write_p99_ms": 6, "busy": 0, "tasks": 769},
  "lane": {"depth": 0, "peak": 3, "refused": 0, "oldest_wait_seconds": 0},
  "landing": {"open_obligations": 4, "oldest_open_seconds": 91230, "orphaned": 0},
  "policy": {"bytes": 15091, "limit": 16000, "state": "near_limit"}
}}
```

**規則二：「現在沒有答案」要跟「沒有答案」分開。** 照搬 C2。

**規則三：該發出警報的三種狀況。舊版一種都沒有。**

| 條件 | 怎麼發出警報 |
|---|---|
| beat 超過 `3 × tick` 還沒跑完一輪 | `/v1/health` 的 `ok` 變成 false，並帶上 `reason: "broker_beat_stalled"` |
| 有 notice 進了 `dead_letter` | 發一則使用者通知 |
| store 不是 `ready` | 回 `503 orchestrator_store_unavailable`（照搬 A3） |

**規則四：自救只做一件事，而且只做證明得了安全的那件。** 照 C3，**不做**「自動重啟 daemon」，也不碰任何人的工作。
**要做的是：beat goroutine panic 時要被 recover，記一筆具型別的 event，再由 supervisor 以退避的方式重啟這個 goroutine，
並把重啟次數放進 diagnostics。** 一個會自己恢復、而且**會大聲說自己恢復過**的迴圈，跟一個安靜停住的迴圈，是完全不同的兩回事。

> **分級：重新設計（舊版沒有）。** 代價很小。**這一項的投資報酬率是全文最高的。**

### 6.7 跨平台：tmux／iTerm 以外怎麼開 child

接續 `docs/cross-platform.md` §4.1。

1. **`Launcher` 與 `TerminalHost` 是兩個 port。** 第一波已經分開了，**這是對的，要維持**。
2. **能力要有名字、要回報，不能默默失敗。** **派工路由必須在開任何東西之前，就能回答「這台機器能不能開 child」**
   （D5：失敗的 spawn 會自己越滾越大）。建議的拒絕是：`409 no_child_capability`。
3. **各種形態下 broker 的行為：**

   | 形態 | 開 child | 簡報注入 | 收工 |
   |---|---|---|---|
   | tmux（macOS／Linux） | `new-window` | `send-keys` | `result.json`＋beat |
   | iTerm2（macOS） | osascript | osascript | 同上 |
   | ConPTY（Windows，daemon 自己開） | `CreatePseudoConsole` | 寫入輸入 pipe | 同上 |
   | WSL（Windows，建議的預設） | 就是 Linux | 同上 | 同上 |
   | 既有 session（Windows 原生） | **不能** | 不能 | — |

   **關鍵觀察：`result.json` 這條收工路徑在四種形態上完全一樣**，因為它走的是檔案系統，不是終端機。
   所以在 Windows 上就算讀不到畫面，**收工仍然是完整的**。
4. **一個現在就要決定的事（先選了安全的預設）：在讀不到畫面的平台上，`spawn_failed` 的判準整個不啟用**，
   只留 progress-note 證明與 task 本身的 timeout。理由是 D1：誤判的代價是刪掉一個活著的 child 的工作，
   漏判的代價只是多等到 timeout。**這兩個代價不對稱。**

> **分級：小改。**

### 6.8 家規：唯一的來源在 repo，安裝目錄只是投影

照搬「兩層、切 base 不切 local」，**把舊版沒做完的那一半做完**：

- repo 裡的 policy **在每次啟動時投影到安裝目錄**：投影前先比對 SHA-256，不一樣才寫，並記下舊檔的 digest。
  這會同時修掉 O11，以及 `8a537a54` 的「一次壞掉的讀取造成的空檔會永遠留著」。
- `dispatch-policy.local.md` **永遠不寫入**。
- **接近上限時要讓人看得見**：超過 90% 就在 diagnostics 出現 `near_limit`；超過上限時**派工仍然成功**，
  但回應裡帶一個有名字的 `warnings` 條目。

> **分級：小改。**

### 6.9 Board 與 Timeline

§2.3 說過，Board 是由 broker 透過 `ProjectBoardIntegration.observe` 餵進去的，但它有自己的寫入路徑、自己的收據（2,630 筆）與 revision。
**方向**：在 Go 版，它們應該是 broker 事件流的**投影**，放在同一個 DB 裡。
**但本文沒有盤點它們的欄位語意**（`cutover.md` §9 也說它們各自需要一份規格），所以這一項**只定方向，不分級、不排波次**。

---

## 7. 分級總表

| # | 項目 | 分級 | 代價 | 風險 | 依據 |
|---|---|---|---|---|---|
| 1 | 整份 JSON 改成 SQLite：events＋投影＋收據同一個交易 | **重新設計** | 大部分已付（第一波） | 中（索引） | §2.2、§3、`Config.swift:519-531`、`b6f4f555` |
| 2 | **事實落盤，觀察不落盤**（`observed → observed` 不寫入） | **重新設計** | 很小 | 低 | §2.2 逐欄比對、`SessionWatch.swift:1033-1043`、`Orchestrator.swift:7466` |
| 3 | SSE 算投影 identity，沒變就不送 | **重新設計** | 小 | 低 | `read-path-architecture.md:114-125`、`events.go:123-137` |
| 4 | 長文分表 | **重新設計** | 小 | 低 | §2.1、`eaa20bbc` |
| 5 | 保留策略：從一個上限改成三層 | **重新設計** | 小 | 低 | `4eb97d86`（149 列）、`mac-app-shell.md:558` |
| 6 | `busy_timeout` 明確設定、BUSY 具型別化；用 `VACUUM INTO` 備份 | **小改** | 很小 | 低 | `mac-app-shell.md:630-636`；`sqlite.go` 目前沒有設 |
| 7 | `coordinator` 跟 task 同一個 DB，CAS 語意照搬 | **小改**（不同意舊文件） | 已付 | 低 | §3 |
| 8 | `config.json` 維持是檔案 | **照搬** | 0 | 低 | `mac-app-shell.md:535-540` |
| 9 | store 壞掉不等於空的 store | **照搬** | 小 | 低 | `6b083838` |
| 10 | 持鎖不做 I/O（守衛要真的寫出來） | **照搬（規則）** | 小 | 中 | `b6d1939a`、`af83249d` |
| 11 | 單一 beat、不可重入、重疊看得見 | **小改** | 小 | 低 | §2.4 第 2 點 |
| 12 | 副作用在交易落盤之後才發生 | **照搬** | 小 | 低 | `6b083838`、`architecture-refactor.md:931` |
| 13 | 終端機 I/O 只有一條有界 lane；被拒是背壓 | **照搬** | 小 | 低 | `23ae603a` |
| 14 | 逾時：牆鐘＋重啟 grace＋空 inventory 不判死 | **照搬** | 小 | 低 | `watch()`、`3a7adb8e` |
| 15 | 重啟後補判的通知合併成一則 | **小改** | 小 | 低 | 本文推論 |
| 16 | `spawning` 的 deadline 開火前先收 progress | **照搬** | 小 | 低 | `72ed3e46` |
| 17 | 跟 child 說過話的 task 永遠不走 empty 回收 | **照搬** | 小 | 低 | 同上 |
| 18 | 打進 tty 不算簡報成功；最多重打五次 | **照搬** | 小 | 低 | `orchestrator.md:1216-1217` |
| 19 | `spawn_failed` 要關掉它開的東西 | **照搬** | 小 | 低 | `3e37e8ec` |
| 20 | `POST …/tasks/:id/respawn` | **照搬** | 小 | 低 | `52d74c88` |
| 21 | 權限模式只有三個值，沒有 `auto` | **照搬** | 0 | 低 | `orchestrator.md:242-246` |
| 22 | 完成通知：durable notice＋退避＋dead letter＋CAS ACK | **照搬** | 中 | 低 | `f05ed2b3` |
| 23 | notice 放進獨立的表 | **小改** | 小 | 低 | 本文推論 |
| 24 | `result.json` preflight＋兩次觀察的復原 | **照搬** | 小 | 低 | `9d1676e9`、`5fb90f86` |
| 25 | progress 不打字但要進 SSE | **小改** | 小 | 中 | `825fc32e`、`6fe569f1` |
| 26 | linger deadline 持久化 | **照搬** | 小 | 低 | `3a7adb8e` |
| 27 | 判定執行者遺失需要兩次觀察 | **照搬** | 小 | 低 | `orchestrator.md:1243`、`:1353` |
| 28 | 清掃前先確認擁有者是否還活著；讀不到就保留 | **照搬** | 小 | 低 | `7f70d979`、`orchestrator.md:2506` |
| 29 | 清掃不掛在讀取路徑；每輪有上限、輪流推進 | **照搬** | 小 | 低 | `af83249d`、`orchestrator.md:1658-1667` |
| 30 | landing 可修改：同一道閘門＋`corrected_from` | **照搬** | 小 | 低 | `Orchestrator.swift:5335-5350` |
| 31 | landing 兩條 arm，在型別上不能互相冒充 | **照搬** | 中（Arm 2 待做） | 中 | `OrchestratorLandingSweep.swift:3-46` |
| 32 | 只拿紀錄上的 ref 比對 | **照搬** | 0 | 低 | `landing.md:432-439` |
| 33 | 舊 landed 列沒有證據時標成 `legacy_unverified` | **小改** | 小 | 低 | `10d460f6` |
| 34 | 落地佇列不建表 | **照搬** | 等於少做 | 低 | `ed9c6152`、§2.1 |
| 35 | `pending` 拆成 live／orphaned | **重新設計** | 小 | 低 | O1 |
| 36 | session 身分只用一個命名空間 | **重新設計** | 中 | 中 | O2、`orchestrator.md:1710` |
| 37 | 具型別拒絕附的補救指令，由契約產生 | **重新設計** | 中 | 低 | `c9e89ce6`、`0f588da7` |
| 38 | `validate_only`；收據仍然不可變 | **小改** | 小 | 低 | O6 |
| 39 | `/v1/diagnostics` 加 `broker` 區塊；beat 停了會發出警報 | **重新設計** | 小 | 低 | §2.6 |
| 40 | beat panic 時 recover＋重啟＋記次數；不碰任何人的工作 | **重新設計** | 小 | 低 | C1、C3 |
| 41 | 等待者的時鐘掛在「還活著的證明」上 | **照搬** | 小 | 中 | `15924b14`、`b2f25048` |
| 42 | 家規從 repo 投影，接近上限要看得見 | **小改** | 很低 | 低 | `bf86ddb7`、`8a537a54`、O11 |
| 43 | 跨平台：能力要有名字，派工前先回答 | **小改** | 低 | 低 | `cross-platform.md` §4.1 |
| 44 | 讀不到畫面的平台關掉 `spawn_failed` 判準 | **小改** | 低 | 低 | D1 |
| 45 | audit log 要輪替；`recentAudit` 不讀整個檔 | **小改** | 很低 | 低 | §2.1 |
| 46 | worktree 清不掉時，要讓人看見一次，而不是每 6 小時記一筆 | **重新設計** | 中 | 中 | §2.6 |
| 47 | inflight／inventory 以 repository 的 common-dir 為鍵 | **照搬** | 小 | 低 | `CHANGELOG.md:200` |

**沒有任何一項被評為「不做」。** `cutover.md` §2.2 漏掉的 `respawn`，本文已補上。

---

## 8. 建議的實作順序

每一波都遵守「**新的開起來，舊的不要關**」。

| 波 | 內容 | 完成的判準 | 依賴 |
|---|---|---|---|
| **B1**（進行中 `0ca0b8c0`） | 派工→簡報→收工的完整迴圈、task secret、CHILD.md、逾時、dispatch-policy 注入 | `cutover.md` A1–A5、A11 | — |
| **B2** | **先做可觀測性，再做功能**：diagnostics 的 `broker` 區塊、beat 停了會發出警報、panic 時 recover、store 健康度；**同時做「觀察不落盤」**（#2） | 殺掉 beat goroutine，30 秒內 `/v1/health` 能說出來；5 個 briefed task 在閒置時寫入次數接近 0 | B1 |
| **B3** | 完成通知（獨立的表）；respawn；progress 進 SSE；SSE 用 identity 去重 | 拔掉 root 的分頁再接回來，notice 仍然送達，而且只送一次 | B1、B2 |
| **B4** | 長文分表＋三層保留＋busy_timeout＋備份＋audit 輪替 | 熱路徑的查詢不碰 `broker_task_text`；13,000 列的 fixture 下，清單投影 < 50 ms | B1 |
| **B5** | landing：路由、兩條 arm、可修改性、`pending` 拆分、`legacy_unverified` | `cutover.md` A5；更正一次後看得到 `corrected_from` | B1、B4 |
| **B6** | 落地佇列（**不建表**）＋單一身分命名空間；inventory＋generation；inflight | `cutover.md` A6；兩個 target 分支不會同時拿到 turn | B5 |
| **B7** | 協調面：messages、notify、waits、coordinator（同一個 DB）、detached、編譯插槽 | `cutover.md` A7、A10 | B2、B3 |
| **B8** | 交接面；worktree 回收；補救指令由契約產生 | `cutover.md` B4 | B5 |
| **B9** | 跨平台：能力要有名字；Launcher；讀不到畫面時關掉判準 | `cross-platform.md` L3 | B1 |

**為什麼 B2 排在 B3 前面：** C1 那四次事故裡，有三次真正的代價都是「不知道它停了」，而 §2.6 量到舊版在心跳停止時仍然回報 `ok: true`。
**「觀察不落盤」也放進 B2**，因為它小，而且它決定了 B4 的表要長成什麼樣子。

---

## 9. 這份文件沒有做到的事

- **沒有跑 build，也沒有跑測試。**
- **序列化成本是用 Python 代理量的，不是 Swift。**
- **「觀察不落盤可以消掉 29/30 次寫入」是推論。** 逐欄比對是實測，但機制（跨 epoch 不比對）是讀程式碼得出的。
  這個 Go 版要在 B2 實測。
- **沒有量新 daemon 在 767 筆規模下的表現。**
- **沒有讀任何 token 或 secret。**唯一讀過的憑證是新版自己的 `local-token`，用來對 `:7727` 發唯讀的 GET。
- **沒有對 `:7717` 需要認證的路由發過請求。**
- **Board 與 Timeline 只量了規模與寫入頻率。**
- **`coordinator.json`、`owned-storage.jsonl`、`project-timeline.json` 在 git 歷史裡沒有事故紀錄。**
- **舊 app 的 58 個 orchestrator route case 沒有一條一條測。**
- **§5 的 O1–O11 是舊 repo 的 backlog 狀態，沒有一條一條重現。**
- **第一波的 child（`0ca0b8c0`）還在進行中。**本文讀了它的分支 head `29a5cf5`，但沒有動它的檔案。

### 9.1 B2／B3 落地後實測（2026-09-18 09:06–09:24，task `eb6b34eb`）

以下只記量到的事實。量法：獨立 daemon `:7797`、獨立 `CLAWDLINE_NEXT_DIR`、tick 5 秒；child 與 root 都是
`/tmp` 下可拋棄的 haiku session。證據檔在該 task 的 `artifacts/`。

- **§9 那條推論，Go 版量到了。** 5 個 briefed child、121 秒、25 次 pass：觀察的 generation 18 → 43、
  每個 child 的 `observed_at` 每一次 pass 都前進；同一段時間 store 的寫入交易 **0**、`total_changes()` **0**、
  events **0**，DB 與 WAL 檔的 mtime、大小都沒動。SSE 在這段時間送出的 `orchestrator` frame 也是 **0**。
- **心跳停了，`/v1/health` 說得出來。** 用 `CLAWDLINE_NEXT_BEAT_FAULT=stall@4` 讓第 4 次 pass 永不返回：
  pass 在 09:06:35 開始，`/v1/health` 在 09:06:46 變成 `{"ok":false,"reason":"broker_beat_stalled"}`（**11 秒**）。
  `panic@3` 與 `exit@3` 則被 supervisor 接住、1 秒後重啟，health 全程 `ok:true`，diagnostics 的 `restarts` 是 1，
  store 各有一筆 `broker.beat.panicked`／`broker.beat.exited`。
- **拔掉 root 的分頁再接回來，notice 只送一次。** root 分頁關掉後完成 child：4 次 `root_missing`（5／10／20／40 秒的梯子，
  中間重啟 daemon 一次，notice id 不變）；root 用 `claude --resume` 在新分頁 `%924` 回來後，下一次到期的嘗試打進 `%924`，
  root 的 transcript 裡帶這個 notice id 的 user turn **正好 1 則**；ACK 之後 90 秒內沒有再送。
  store 的 events：`task.completion.attempt` 4、`delivered` 1、`acknowledged` 1。
- **這台機器的 session 讀數從來不是 complete。** `/v1/next/sessions` 的 scan 是
  `{"complete":false,"notes":["iTerm2 apple event failed: exit status 1"]}`。所以依 E6「兩次完整觀察」判定的
  `executor_missing` 在這台機器上**不會成立**；實測時 4 個自行結束的 child 一直停在最後一次的 `observed`。
- **一個從沒有對話過的 Claude session 不能 `--resume`**（`No conversation found with session ID`），因為 transcript
  要等第一個 turn 才寫。拿來當 root 的可拋棄 session，要先讓它回一句話。

---

## 附錄：數字從哪裡來

| 數字 | 怎麼量的 |
|---|---|
| 九份持久檔的大小與筆數 | 用 `python3` 讀每個檔，逐個 key 計算 `len()` 與 `len(json.dumps(v).encode())` |
| `orchestrator.json` 平均每 10.5 秒重寫一次 | 每秒用 `stat -f '%m %z'` 取樣，共 315 秒 |
| **每次重寫改了哪些葉節點** | 連續抓五次重寫前後的快照，逐個葉節點比對 |
| 重寫的觸發機制 | 讀 `SessionWatch.swift:1021-1066` 的 `reconcileExecutor` 與 `Orchestrator.swift:7466` |
| save 的成本 | 用 Python 代理；**Swift 沒有量** |
| 09-04 的基準 | 舊 repo `docs/mac-app-shell.md:500-680`（`6fc7918f`） |
| task 逐欄 bytes | 對 769 列的每一欄做 `json.dumps` 後加總 |
| 110／58 個 route case | `grep -cE` 在 `RemoteServer.swift` 上計數 |
| 86 處 audit、56 個 save 呼叫點 | `grep -c` 在 `Orchestrator.swift` 上計數 |
| 熱路徑事件排名、3,699／158／188 | 對 `remote-audit.jsonl` 的 36,010 行逐行解析後分組 |
| `landing_paths` 的 key 是 task id | 252 個 key 全部符合 UUID 形狀 |
| 上限 1350 | 讀 `config.json` 對照 `Config.swift:505-545` |
| policy 用掉 94% | 15,091 對 `policyLimit` 16,000 |
| 舊 `/v1/health` 沒有 broker 欄位 | `curl :7717/v1/health`（這條不需要認證） |
| §5 的 O1–O11 | 在舊 repo 用 `grep -n` 對照 `docs/backlog.yaml`，逐條核對行號 |
| 1,724 筆 commit 與各 sha 的原文 | 在 `~/code/clawdline` 用 `git log`／`git show --no-patch`（只讀） |

**這份文件裡每一句「舊 app 會怎樣」，都來自原始碼、commit 原文、舊 repo 的文件，或當場的量測；
每一句「Go 版應該怎樣」，後面都附了一條可以用來反駁它的證據。
凡是推論而沒有量過的，句子裡都會寫「推論」、「建議」或「未量」。**
