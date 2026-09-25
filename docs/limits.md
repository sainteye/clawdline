<!-- retired-app-record: 新舊兩版的上限盤點，量於 2026-09-18 09:10–09:40；該 app 已於 2026-09-19 停用，7717 無人監聽 -->

> **主體與時間：** 這份文件記錄的是 **新舊兩版的上限盤點**，量於 **2026-09-18 09:10–09:40**。
> **舊 Swift app 已於 2026-09-19 停掉、取消登入時啟動，7717 現在沒有人在聽。**
> 文中寫成現在式的「舊 app 還在跑」「7717」都是**量測當下**的事實，刻意保留，
> 用來對照還有哪些功能要 migrate、當初怎麼實作——**不要照著它設定今天的 daemon**。

> **整套工作系統（看板項目、Session 待辦、Backlog、GitHub Issue）怎麼運作、哪些已經做到，一份講完在 [`docs/work-system.md`](work-system.md)。**

# 上限與保留策略：滿了之後誰會知道

> **實作依據是 [`docs/design-decisions.md`](design-decisions.md)（2026-09-18 起）；本文保留為分析材料，與它衝突之處以它為準。**

> 使用者原話（2026-09-18）：「你的時間軸已經滿了 <- 這本身就是一個設計問題」。
>
> 起因是 `docs/timeline-design.md` §0：舊 app 的時間軸 entries 到了 2,000／2,000，**上限的行為是拒絕新寫入而不是淘汰**，
> 2026-09-17 02:06 之後什麼都沒再記錄，沒有任何東西發出警報，畫面上也看不出來。
> 使用者的意思不只是「把上限調大」，而是**一類缺陷**：系統裡每一個有界的東西，滿了之後會發生什麼、誰會知道。
>
> 量測：2026-09-18 09:10–09:40，對**正在跑的**舊 app（7717）、新版（7727）與它們此刻的資料檔。
> 舊 app 的原始碼在 `~/code/clawdline`（HEAD `85cf6003`），新版在本 repo `26c0152`。**全程唯讀。**
> **本文只出文件，不改實作。** 沒量到的一律寫「未量」，不算通過。

---

## 0. 五句話

1. **「滿了就安靜拒絕」不是時間軸一個地方的事。** 兩個 app 合起來盤出 70 列（§2）。舊 app 裡**只有一個**上限滿了會主動告訴使用者
   （看板 workflow 的 toast）；新版的 `/v1/health` 與 `/v1/diagnostics` 裡**沒有任何一個**填充度欄位。
2. **下一面牆已經排好日期了**（§2.4）：看板收據約 5–8 天後開始 FIFO 淘汰、orchestrator 的 task 紀錄 **2026-09-28** 起開始被
   30 天規則安靜刪除、看板卡片約一個月後撞上 2,000 張的拒絕上限。舊 app 的主 log 已經 **313 MB**，沒有輪替，一天最多長 62 MB。
3. **缺陷分三類，另有一個橫跨三類的問題**（§3）：(a) 該淘汰卻拒絕、(b) 該拒絕卻無聲截斷或淘汰、(c) 根本沒有上限；
   而**就算行為對了，大多也沒有人知道**。舊 app 自己有一個做對的範本：`/v1/health.http_reliability` 把每一個 HTTP 上限跟它的
   `current／peak／refused／evicted` 放在一起。新版要做的，就是把這個形狀推廣到**所有**儲存。
4. **統一規則是「類別決定行為」**（§4）：證據（落地紀錄、交付與驗證收據、安全稽核、task 本體）**永不淘汰**，到頂只能拒絕並大聲說；
   只用來擋重送的冪等收據**按時間**過期，不按筆數；可重算的觀察**可以淘汰**；長文**摘要後移到冷層**；log **輪替**。每一個上限登記在一張表裡，
   沒登記的上限本身就是缺陷，而且要能**故意塞滿、看到它叫**。
5. **舊 app 那個滿掉的時間軸：凍結成唯讀的歷史，不搬、也不摘要。**（§6）2,000 筆裡 1,973 筆可以從 git 逐字重算，
   27 筆 landing 裡有 **10 筆是測試假資料**（本文新發現：舊 app 的測試寫進了使用者正式的 store），剩下 17 筆在凍結的
   `orchestrator.json` 裡有原始紀錄；部署證據 0 筆。搬過來只會把假資料當成證據一起搬。

---

## 1. 三十秒版本

| | 舊 app | 新版（`26c0152`） |
|---|---|---|
| 盤到的列（§2.2、§2.3） | 39 | 31 |
| 到頂時會主動通知使用者的 | 1（看板 workflow toast） | 0 |
| health／diagnostics 裡有填充度的 | HTTP 入口層有（`http_reliability`），儲存層 0 | 0（唯一的丟棄計數在 `/v1/cloud/status`） |
| (a) 該淘汰卻拒絕 | 8 | 2 |
| (b) 該拒絕卻無聲截斷或淘汰 | 10 | 11 |
| (c) 沒有上限 | 8 | 11 |
| 行為對、但沒人知道 | 5 | 2 |
| 行為對、也有人知道 | 8（含範本 O26） | 3 |
| 其他 | — | 2（N23 寫好沒接線、N31 health 本身） |
| 已經滿了 | 時間軸 entries（100%）、看板 workflow runs（100%，靠 retire 運轉） | — |
| 最近的牆 | 看板收據 5–8 天、task 紀錄 30 天規則 10 天 | 新 store 幾乎是空的（events 17 列），牆在「沒有上限」那一側 |

分類的判準、每一列的證據與實測值在 §2、§3；一列同時有兩種問題時（N19、N27）按主要的那一類計。**新版的「空」不是好消息**：SQLite 裡沒有任何一句 `DELETE FROM`，task 目錄與 worktree
沒有清掃，稽核檔沒有輪替——它不會撞牆，是因為它還沒開始長。

---

## 2. 盤點：每一個上限

### 2.1 怎麼量的

- **原始碼**：兩個唯讀 subagent 各掃一邊（舊 app 的 `Sources/` 與 `Resources/web/`、新版的 `internal/`、`cmd/`、`web/`），
  每一列帶 file:line。其中最重要的 9 條我親自重讀過（附錄 A 列出哪 9 條）。
- **實測**：直接 `stat`／`wc`／`jq` 資料檔；SQLite 一律**先複製到本 task 的 `work/` 再開**（`usage.sqlite3` 與新版的
  `clawdline.sqlite3`，連同 `-wal`，不含 `-shm`），不碰原檔；兩個 daemon 的 `/v1/health` 與新版 `/v1/diagnostics`（本機 token）。
- **不讀的**：任何 token、`secrets/`、`push.json`（只記大小）、其他 task 的 `/tmp/.clawdline/<id>/` 內容（只記目錄數與 `du`）。
- 「類」欄：**a／b／c** 見 §3；**✓** 行為與類別相符；**✓?** 行為對但沒人知道；**範本** 是值得照抄的做法。

### 2.2 舊 app

**儲存與派工**

| # | 東西 | 上限（file:line） | 到頂時 | 誰會知道 | 永久消失？ | 現在（實測） | 類 |
|---|---|---|---|---|---|---|---|
| O1 | 時間軸 entries | 2,000（`ProjectTimelineStore.swift:7`） | **拒絕**（`:294-297`）；背景回填看到 `capacity` 就整個專案跳過（`ProjectTimelineIntegration.swift:58`） | 只有打開時間軸頁才看得到的被動文案（`timeline.js:137,149`）；log、audit、health 都沒有；broker 的 landing 觀察**連回傳值都不看**（`ProjectTimelineIntegration.swift:115`） | **是**：凍結後已有 **47 筆已驗證的 landing** 沒進時間軸 | **2,000／2,000（100%），自 09-17 02:06:58 凍結約 31 小時** | a |
| O2 | 時間軸 events／檔案大小 | 20,000（`:8`）／8 MiB（`:10`） | 拒絕；**載入時超限整個 store 回 503**（`:77-86`） | 錯誤碼 | 否 | 2,000（10%）／2,495,417 B（29.8%） | a |
| O3 | 時間軸 `requestReceipts` | 4,096（`:9`） | 拒絕且不過期；檢查在所有指令之前（`:157-158`），**到頂後連 `set_enabled` 都 507** | 錯誤碼；`capacity` 物件不報它 | 否，但指令 API 永久失效 | 0 | a |
| O4 | 時間軸 checkpoints | 128（`:85, :223-225`） | 拒絕 | `historySourceIssues`＋頁面文案 | 游標會掉 | 13（10 個 `capacity`、2 個 `complete`） | a |
| O5 | orchestrator task 紀錄 | 1,350 筆（`Config.swift:531`）＋ settled 滿 30 天（`:545`） | **淘汰**（`OrchestratorTaskShape.swift:904-911`）；只保護「已結束且擁有者還活著」與待落地的列；數量那條規則**不檢查是否已結束**（`:903-905`，實務上碰不到，潛在） | **沒人**：`removeTasks` 與刪目錄都沒有 audit、沒有 log（`Orchestrator.swift:10504, 10531`） | **是**：這是用量分類唯一的持久證據（F1：149 列永遠無法歸因） | 776／1,350（57%）；最舊一列 19.8 天 → **2026-09-28 起 30 天規則開始刪**。每天約 36 筆，穩態約 1,080 列，所以真正生效的是天數，不是筆數 | b |
| O6 | `session_deliveries` | 最新 200 筆（`Orchestrator.swift:10495`） | 淘汰，**不看 settled**，未結清的交付也會被刪 | 沒人 | 是 | 105／200（53%） | b |
| O7 | progress notes | 5 則、每則 300 字（`:5926-5929`） | 淘汰最舊 | 沒人 | 是 | — | ✓（進度類，淘汰是對的） |
| O8 | 完成通知重送 | 8 次（`:1326`）→ `dead_letter` | 轉 dead letter | **只在 record 的 `dead_letter_at` 欄位**；沒有 audit、log、health | 通知沒送到 | — | ✓? |
| O9 | agent notify | 每 task 5、每機 30／小時（`:4907`、`OrchestratorRegistry.swift:109`） | 拒絕 429 `notify_limit`／`rate_limited` | 呼叫端＋audit | 否 | — | ✓ |
| O10 | `session_activity`、`closure_attestations`、`landing_paths`、`coordinator-successions` | 無（`OrchestratorEventPublisher.swift:90-95`、`OrchestratorLandingQueue.swift:483`、`Coordinator.swift:1415-1419`） | 只增不減；`landing_paths` 在 task 紀錄被刪之後還留著 | — | — | 990／50／260／4 | c |
| O11 | `owned-storage.jsonl` | 30 天且路徑已不存在才壓實（`OwnedStorage.swift:256-257`） | **只要一行壞掉，壓實就永遠拒絕**（`:254-255`），而呼叫端 `_ = OwnedStorage.compact()` 忽略回傳（`Orchestrator.swift:10537`） | 沒人 | 否 | 666 行／231,201 B | c |
| O12 | `durable-reports/` | 500 份／256 MiB（`DurableReportStore.swift:17-22`） | 拒絕 409 `durable_report_quota_reached`；**沒有刪除 API**；載入超過 500 份整個 store 不可用（`:386, :403`） | 錯誤碼 | 否，但之後永遠寫不進去 | 44 KB | a |
| O13 | `usage.sqlite3` | 無保留（唯一的 DELETE 是去重，`UsageLedger.swift:1027`） | 只增不減 | — | — | 7.0 MB＋4.0 MB WAL；`usage_intervals` 2,183 列（08-27 起） | c（帳本是證據，不該刪；缺的是空間警報） |

**看板**

| # | 東西 | 上限（file:line） | 到頂時 | 誰會知道 | 永久消失？ | 現在（實測） | 類 |
|---|---|---|---|---|---|---|---|
| O14 | 卡片／專案 | 2,000／200（`ProjectBoardStore.swift:50-51`） | **拒絕**：手動建立 409 `item_capacity_reached`；自動匯入 refused 並記進 `ingestionCoverage.droppedCount`（`:1494-1517`） | 只在 board envelope 的欄位 | 新卡進不來 | 793／2,000（39.7%），近 7 天每天 41 張 → **約 29 天後（10 月中）撞牆**；專案 34／200 | a |
| O15 | 收據 `(actor, requestId)` | 4,096（`:52`） | **FIFO 淘汰**（`:6660-6664`）；**錯誤回應也佔一格**（`:6573-6577`） | envelope 的 `idempotencyRetention.evictedReceipts`；但契約文件寫「never evicts」（`board-design.md` B8） | 失去重送保護 | 2,639／4,096（64%）；每個 revision 0.32–0.54 筆、每天約 580 個 revision → **推估 5–8 天後（約 9/23–9/26）開始淘汰** | b |
| O16 | 檔案大小 | 32 MiB（`:57`） | 拒絕 409；**載入超限整個看板 503**（`:6151-6153`） | 錯誤碼 | 否，但之後寫入全失敗 | 4,393,721 B（13.1%） | a（遠） |
| O17 | 每卡內嵌歷史＋`project-board-history/*.jsonl` | 內嵌 5（`:5683`）；每項 log 保留 2,000、超過 3,000 壓實（`ProjectBoardHistoryLog.swift:50-54, 160-164`） | 淘汰；**log 寫失敗被 `try?` 吞掉**（`ProjectBoardStore.swift:5702`），之後內嵌視窗一裁就永久消失，`historyDroppedCount` 卻照樣累加，看起來跟正常存進 log 一樣 | UI 算得出 evicted 數，但分不出「在 log 裡」還是「真的掉了」 | 部分 | 568 檔、12,915 行、單項最多 624 行（壓實門檻的 21%） | b |
| O18 | workflow | runs 256、events／run 64、receipts 512、outbox 512（`ProjectBoardWorkflow.swift:69-75`） | runs 先 retire，不行才拒絕；receipts FIFO；outbox 滿了 429 | **全 app 唯一會跳 toast 的上限**，一頁只跳一次（`board-workflow-status.js:28-33`） | 部分 | **runs 256／256（100%，靠 retire 運轉）**；receipts 376／512（73%）；outbox 0 | ✓ |
| O19 | 回應大小 | 1,000,000 B／500 張（`:53, :58`）；HTTP 2 MiB | 截斷並帶 `truncation.reason`、`itemsOmittedCount` | 欄位＋UI 一句 | 否 | — | ✓ |
| O20 | 讀／寫佇列 | 4／8（`ProjectBoardRequestCoordinator.swift:24-26`） | 429＋`Retry-After: 1` | 呼叫端 | 否 | — | ✓ |

**log 與稽核**

| # | 東西 | 上限 | 到頂時 | 誰會知道 | 永久消失？ | 現在（實測） | 類 |
|---|---|---|---|---|---|---|---|
| O21 | `~/Library/Logs/Clawdline.log` | **無**（`Log.swift:11-31`：開檔、seek 到尾、寫、關） | 只增不減；第一次建立用預設 umask，**實際是 0644** | — | — | **313,624,555 B**、1,744,691 行，自 08-31 起；每天 0.6→**61.8 MB**（09-15 高峰），近三天 21–38 MB；**77% 是 `cloud:`**，其中 `durable outbound stage=drain_observation` 一天寫 3.6 萬行 | c |
| O22 | `remote-audit.jsonl` | **無**（`RemoteAuth.swift:494-510`） | 只增不減；**沒有鎖、沒有 `O_APPEND`**（先 seek 再寫，多佇列可能互相覆蓋）；寫失敗 `try?` 吞掉——而這個函式的註解自稱「a security control and not bookkeeping」 | 沒人 | 寫失敗時是 | 5,745,166 B、36,130 行、31 天；**80.8% 的位元組是 `orchestrator.*` 營運事件**，裝置／配對／推播這類安全事件合計不到 1% | c |
| O23 | `orchestrator.worktree.kept` | 無去重 | 同一個 dirty worktree **每 6 小時重寫一次** | audit 本身 | — | 3,699 筆來自 158 個 task，單一 task 188 次；佔 audit 位元組 11.5% | c |
| O24 | `cloud-viewer-events.jsonl` | 4 MiB，輪替成 `.1`（`DiagnosticReport.swift:214-226`） | 輪替，最多兩檔 | — | 最舊的 | 1.4 MB | ✓（**全 app 唯一會輪替的 log**） |
| O25 | `hooks/notification-census.log` | 400 行 → 留最新 200（`hook.sh`） | 淘汰 | — | 是（診斷用，可以） | 367／400 | ✓ |

**遠端、Cloud 與 HTTP**

| # | 東西 | 上限 | 到頂時 | 誰會知道 | 永久消失？ | 現在（實測） | 類 |
|---|---|---|---|---|---|---|---|
| O26 | SSE／HTTP 入口 | 連線 128、stream 16、每 stream 4 MiB、合計 16 MiB（`HTTPReliability.swift:94-104`） | 新 stream 503 `sse_capacity`；慢消費者**斷線**，重連拿完整快照；同 channel 快照合併 | **`/v1/health.http_reliability` 的 limits＋metrics**，外加 log | 不會（重連補快照） | streams peak 8／16、connections peak 12／128、`streams.evicted` 1、`write_error_evictions` 1 | **範本** |
| O27 | 送出去重 | 10 分鐘，只在記憶體（`RemoteServer.swift:3680-3693`） | 過期淘汰；**重啟就清空** | 沒人 | 重啟後 client 重試可能打兩次字 | — | b |
| O28 | Cloud command ledger | 24 小時；全域 10,000、每 actor 1,000（`CloudCommandLedger.swift:294-304`），`permitsEvictionAtCapacity=false` | 拒絕 429 `command_ledger_capacity`（不淘汰視窗內的） | 計數器 `ledger_capacity_refusals_total` 存在，**production 沒有任何地方讀** | 指令被拒 | 620／10,000（6%），全部 `completed`，`expiresAt − createdAt` 正好 24 小時 | ✓? |
| O29 | Cloud outbound spool | 全域 2,000 列／16 MiB、每收件者 200 列／2 MiB、發送窗 8 列（`CloudOutboundSpool.swift:183-218`） | 拒絕（不踢掉還在用的列）；但 `failureDisposition` 把所有 spool 錯誤歸成 `.integrity`、**不重試**（`CloudAppBridge.swift:1294`） | 只有 log | 發布失敗 | 977／2,000（49%）；本行程 `window_refusal_attempts` 1,319、`writes_failed` 268 | ✓?（行為對、歸類錯） |
| O30 | Cloud replay window | 每 sender 1,024 序號；**sender 4,096 個，行程存活期間不回收**（`CloudEnvelope.swift:375-402`） | 拒絕新 sender 直到重啟 | `cloud-status.json` 的 dropped 計數與最近 20 筆 | 是 | 未量（記憶體） | a |
| O31 | Web Push 訂閱 | 無總數上限；同 endpoint／device 只留一筆（`WebPush.swift:271-280`） | 只有 404／410 會刪；**其他失效狀態永遠留著** | log＋audit | — | `push.json` 652 B（含秘密，只記大小） | c |
| O32 | Web Push payload | `maxPayload`（`:122`） | 先拿掉 icon（有 log），再**二分法靜默截短 body**（`:596-606`） | 部分 | 部分 | — | b |
| O33 | 請求與訊息 | body 20 MiB、訊息 100,000 字、plan 4,096（`RemoteServer.swift:650, 2015`） | 大多拒絕；**child summary 在 2,000 字處靜默截斷**（`Orchestrator.swift:7992`）；`result.json` 超過 2 MiB **當作不存在**（`:7987`）；手機送圖時解碼失敗的圖靜默跳過（`RemoteServer.swift:4263-4266`） | 大多只有錯誤碼 | summary 會被截掉 | — | b |
| O34 | 家規 `dispatch-policy.md` | 16,000 字（`Orchestrator.swift:9303`） | 在段落處截斷，**並在簡報裡說**（`:9325-9335`）；local 規則先花預算（`:9338-9340`） | **只有讀簡報的 child 知道**；改檔的人沒有任何提示 | child 看不到被切的規則 | 15,037＋283 字＝**約 96%** | ✓?（錯的人知道） |

**讀取、圖片與暫存**

| # | 東西 | 上限 | 到頂時 | 誰會知道 | 永久消失？ | 現在（實測） | 類 |
|---|---|---|---|---|---|---|---|
| O35 | transcript 讀取 | 檔尾 8 MiB（`TranscriptRevisionWatch.swift:412`）；回應 150 KiB（`:445`） | 截斷；150 KiB 那層帶 `truncation`，**web client 完全不讀**；**8 MiB 之前的內容連旗標都沒有** | 沒人 | 否（原檔在），但看不到 | 最大的 Codex transcript **2,238,643,505 B**（8 MiB 只看得到 0.37%）；最大的 Claude 67.6 MB；`~/.codex/sessions` 12 GB、`~/.claude/projects` 2.7 GB | b |
| O36 | session 圖片 | 24 小時、64 張、64 MiB、單張 12 MiB、墓碑 7 天、metadata 512（`SessionImageArtifact.swift:165-175`） | **淘汰最舊的，即使 transcript 還在引用**（`:555-561`） | 讀的人事後看到「Image expired」 | 是 | 13 張 PNG、71 筆 metadata（14%）、3.0 MB | b |
| O37 | Drops（手機上傳的 PNG） | 40（`Drop.swift:193-202`） | 淘汰；**這些路徑已經打進 agent 的 prompt 裡** | log | 是 | 未量 | b |
| O38 | worktree（dirty 就保留） | 無（`OrchestratorDraft.swift:1075-1136`） | 只增不減 | 重複的 audit（O23） | — | **508 MB**（6 個 repo）；`reclaimed-checkouts` 74 個、5.3 MB（30 天，會刪，有 audit） | c |
| O39 | task 目錄 `/tmp/.clawdline/<id>` | settle 後 24 小時＋擁有者活著就留（`Config.swift:516`）；每輪最多 64 個 | 淘汰；**刪除時沒有 audit**（`Orchestrator.swift:10531`） | 沒人 | 是（含 `work/`） | 61 個、160 MB（只量 metadata） | ✓? |

HTTP 入口層以外，舊 app 沒有任何 GET diagnostics route（只有 `POST /v1/diagnostics/report`）；`/v1/health` 的 key 是
`ok, version, build, instance, protocol, write, auth, password, authed, http_reliability`（`RemoteServer.swift:1066-1109`），
**沒有任何儲存層的填充度**。能看到填充度的只有三處：時間軸 GET 的 `capacity`（只有 entries／events／bytes，不含收據）、
看板 envelope 的 `idempotencyRetention`＋`ingestionCoverage`、`GET /v1/orchestrator/storage`。

### 2.3 新版（`26c0152`）

新版是全新安裝：`~/.config/clawdline-next` 總共 528 KB；`clawdline.sqlite3` 的副本裡 `events` 17 列、`receipts` 5 列、`tasks` 0 列；
`remote-audit.jsonl` 448 B；`tasks/` 0 個目錄。**所以「離上限多遠」在新版幾乎都是「還沒開始長」，要看的是它會不會長、長了誰知道。**

| # | 東西 | 上限（file:line） | 到頂時 | 誰會知道 | 永久消失？ | 類 |
|---|---|---|---|---|---|---|
| N1 | SQLite 全部資料表（`events`、`receipts`、`tasks`、`obligations`、`board_commands`、`broker_*`） | **無**；全 repo 沒有一句 `DELETE FROM`；`events` 的註解明寫「never edited and never deleted」（`store/sqlite.go:33-35`） | 只增不減；broker 每 5 秒、每個 SSE 訂閱者每 2 秒都會讀出並解碼整張 `broker_tasks`（`orchestrator/broker.go:161-176`），成本跟著全部歷史長 | 只有 CLI `clawdline doctor` 印筆數（`cmd/clawdline/main.go:162-168`） | — | c |
| N2 | 事件寫入失敗 | — | `Store.Append` 不記 log（`sqlite.go:238-243`），呼叫端一律 `_ =` 丟掉；`app/actions.go:414-416` 的註解說「logged by the store」，**不是真的** | 沒人 | **是**（磁碟滿就是它） | b |
| N3 | broker 解不開的 record | — | `continue` 跳過（`broker.go:169-173`） | 沒人 | 看不見 | b |
| N4 | 派工容量 | 每 root 5、每機 20；10 分鐘速率（`broker.go:94-127`） | 429 `over_capacity`／`rate_limited` | 呼叫端 | 否 | ✓ |
| N5 | progress notes | 300 字；讀最新 5（`broker.go:100-107`） | HTTP 400；但**從 `progress.json` 來的超長筆記直接丟、不記 log**（`watch.go:103-105`） | 檔案那條沒人 | 部分 | b |
| N6 | notify | 5／task、30／小時（`broker.go:103-109`） | 429 | 呼叫端 | 否 | ✓ |
| N7 | 完成通知重送 | 8 次（`record.go:151-175`） | 轉 dead letter，寫 event `task.completion.dead_letter` | **console 沒畫、health 沒有**；`KindDeadLetter` obligation 有定義（`domain/task/obligation.go:23`）但從未建立 | 是 | ✓? |
| N8 | summary／title／kind／label | 2,000／60／40／120（`taskdir/finish.go`、`domain/work/proposals.go`、`orchestrator/draft.go`） | summary／kind／label 仍依各入口處理；title 超長或符合已量到的壞形狀會帶教學訊息拒絕，不再截斷 | 寫入者 | 否 | ✓ |
| N9 | `result.json` | **無大小上限**（`taskdir/broker.go:162-176` 直接 `os.ReadFile`） | — | — | — | c |
| N10 | task 目錄 `<Dir>/tasks/<id>` | **無清掃**（`taskdir.go:21`） | 只增不減 | — | — | c |
| N11 | worktree `<Dir>/worktrees/…` | **無**；`RemoveWorktree`（`git/worktree.go:56-60`）沒有任何呼叫端 | 只增不減 | — | — | c |
| N12 | `remote-audit.jsonl` | **無輪替**；`O_APPEND`＋0600（比舊版好，`devices/files.go:513-543`）；欄位值 256 B **靜默截斷**（`:75-77`） | 只增不減；寫失敗只記 log | log | 部分 | c |
| N13 | 裝置清單 `remote.json` | 裝置數**無上限**，每次密碼登入新增一台（`auth/authority.go:718`）；讀取上限 4 MiB（`devices/files.go:70`） | **超過 4 MiB 時 auth 在啟動時整個失敗**（`gate.go:120-124`） | log | 服務中斷 | c（滿了是全面停擺） |
| N14 | push 訂閱 | 無數量上限（`push/store.go:225-237`）；檔案讀取上限 1 MiB | 超過 1 MiB → `ErrUnreadable`，**所有推播都失敗** | log；notify 回 502 | — | c |
| N15 | 圖片 | 照搬舊版 policy（`artifacts/artifact.go:59-70`） | 淘汰最舊，**不記 log**（`store.go:365-372`）；讀者拿到 410 `artifact_expired` | 讀的人事後才知道 | 是 | b |
| N16 | Drops | 40（`drops.go:33-34`） | 淘汰，有 log | log | 是 | b |
| N17 | transcript | 8 MiB 尾端、150 KiB 回應（`transcript/turns.go:36-39`、`http/transcript.go`） | 同舊版；React `Transcript.tsx` 不讀 `truncation` | 沒人 | 否 | b |
| N18 | 記憶體快取（transcript titles） | 256 筆 LRU（`transcript/claude.go`、`transcript/lru.go`） | 淘汰最久未使用的讀數並計數 | diagnostics、notice | 是 | ✓ |
| N19 | SSE（自己供應時） | 訂閱者數**無上限**；screen bus 每訂閱 chan 16，**滿了走 `default` 直接丟、沒有計數**（`http/screen.go:189-197`）；沒有 Last-Event-ID | 丟 | 沒人 | 下次變動才補回 | b（訂閱者無上限那一半是 c） |
| N20 | Cloud 入站佇列 | 64（`cloud/relay.go:42`） | **丟最舊的請求**，記 log | `/v1/cloud/status.queue_dropped`；UI 有型別（`cloud.ts:77`）**但沒畫** | 是 | b |
| N21 | Cloud replay window | 照搬（`cloud/replay.go:11-17`） | 拒絕新 sender | `inbound_dropped{reason}`，設定頁有警示點（`SettingsWindow.tsx:632-640`） | 是 | a |
| N22 | Cloud spool | 四個登記列：`cloud.spool`（2,000）、`cloud.spool_bytes`（16 MiB）、`cloud.spool_channel_bytes`（4 MiB，單一 channel）、`cloud.spool_receipts`（4,096 筆墓碑） | 拒絕；**而且會回一句具名的拒絕**給等在那條 channel 上的人（`cloud_read_busy` / `command_answer_undeliverable`，9.5.1），不是只記 log | log、diagnostics、notice、**等的人本人** | 是 | ✓ |
| N23 | Cloud command ledger | 照搬（`domain/cloud/ledger.go:49-66`） | **寫好了但沒接上**（`NewLedger` 沒有正式呼叫端）；註解承認重啟後會重複執行（`:19-23`） | — | — | （未接線） |
| N24 | 看板收據 | 4,096（`board/board.go:56`） | FIFO；淘汰計數只存在檔案裡（`settings.go:205-208`），不上 wire | 沒人 | 是 | b |
| N25 | 看板卡片／專案 | `MaximumItems = 2_000`、`MaximumProjects = 200`（`board/board.go:53-54`） | **宣告了，整個 codebase 沒有任何引用**——跟舊時間軸同一個數字，這次是名義上的上限 | — | — | c |
| N26 | 看板快照與檔案 | 500 張／1 MB；檔案 32 MiB（`board.go:39-50`） | 快照截斷帶 `truncation`（✓）；檔案超限時載入回 503、整個看板不可用（同 O16） | 欄位 | 否 | a（遠） |
| N27 | request body | 多數路由有；**`dispatch`、`orchestrator` 八條、`schedules`、`coordinator`、`actions` 沒有**（`server.go:434-438` 只設了 `ReadHeaderTimeout`）；auth body 超過 64 KiB **被截斷後當成 `{}`**（`auth.go:66-78`） | — | — | — | c（auth 截斷那一半是 b） |
| N28 | 文件列表 | 200 列、走訪 4,000（`documents.go:366-394`） | **靜默截斷，沒有旗標** | 沒人 | 否 | b |
| N29 | daemon log | Go 標準 `log` 寫 stderr，全 repo 沒有 `SetOutput`；打包的殼沒有轉向輸出（`shell/darwin/main.swift:300-322`） | 目前由啟動者導到 `/tmp/clawdline-next.log`（491 B，實測 `lsof`）；打包後去哪裡未驗證 | — | 可能遺失 | c |
| N30 | scheduler | `MinInterval = 1m`（`schedule.go:54-60`） | 400；錯過的 tick 合併 | `/v1/diagnostics.scheduler` | 否 | ✓ |
| N31 | health／diagnostics | — | `GET /v1/health` 只有 `at, ok, served_by`；`GET /v1/diagnostics` 是 `at, dir, ok, port, served_by, upstream, scheduler{…}`（實測 7727）。**沒有任何填充度、容量或丟棄數**；唯一的丟棄計數在 `/v1/cloud/status` | — | — | — |
| N32 | Session inventory cache | **120 seconds**, registered as `cache.session_inventory`; overrides may only lower it | The last complete rows for one failed terminal source expire and that source becomes `missing`. A terminal-confirmed close suppresses any older in-flight or retained copy inside the same window and leaves sooner when a complete terminal enumeration takes authority back. Action and decision callers bypass retained observations entirely. | `/v1/diagnostics.capacity` reports age and expiry count; the session list labels retained rows and the batch once | No; this is a reproducible observation cache | ✓ |
| N33 | Spoken-intent planner | **4 KiB** per sentence, **2** queued turns, **30 seconds per CLI attempt**, and **130 seconds** for a hosted-console request, registered as `intent.request_bytes`, `intent.planner_queue`, `intent.planner_seconds`, and `intent.cloud_wait_seconds` | An oversized sentence is refused before a model reads it; a third queued request receives 429 `busy`; a timed-out or unusable turn receives 502 `plan_failed`. Claude and the Codex fallback each get one bounded attempt. The Cloud deadline covers one 60-second turn ahead, one 60-second turn of its own, and 10 seconds of relay overhead. | The sender receives a typed refusal; all four rows are present in `/v1/diagnostics.capacity`; audit logs contain timing and outcome but never the sentence. | No; the route creates only an editable draft and starts nothing. | ✓ |
| N34 | Manual session titles | **16 KiB** per request, **200 characters** per normalized title, **200 rows**, and **90 days**, registered as `session.title_request_bytes`, `session.title_characters`, `session.title_rows`, and `session.title_age` | Oversized input is refused by name. A successful write prunes expired rows, then evicts the oldest row if the retained set is full. Clearing a title removes its row and reveals the next label rung. | The sender receives a typed refusal; all four rows are present in `/v1/diagnostics.capacity`, and retained-row pressure is eligible for a notice. | Yes; an expired or oldest manual label can disappear, while the assistant/task/fallback label remains. | ✓ |
| N35 | Requested user action on a Board item | **8 KiB** per item write, registered as `work.item_user_action_bytes` | The owning Agent receives `user_action_too_large`; existing item state is unchanged. The text is accepted only with `waiting_user` and is cleared with that condition. | The sender receives a typed refusal; the row is present in `/v1/diagnostics.capacity`. | No | ✓ |
| N36 | Completion-correction reason on a Board item | **8 KiB** per correction, registered as `work.completion_reason_bytes` | The completing Agent receives `reason_too_large`; the terminal item, released assignment and event history remain unchanged. | The sender receives a typed refusal; the row is present in `/v1/diagnostics.capacity`. | No | ✓ |
| N37 | Linux release health gate | **30 seconds**, registered as `deploy.health_seconds` | The unprivileged deploy refuses the new release, restores the previous atomic selector, and restarts that release. | The command reports the failed deployment and rollback; the row is present in `/v1/diagnostics.capacity`. | No; the new release is not kept active without health and exact-build evidence. | ✓ |
| N38 | A Session's own to-dos | **20 rows** per call, registered as `session.todo_batch_rows`; each row under the existing 8 KiB (`session.direct_todo_bytes`), the call under the 96 KiB work request body (`work.request_body_bytes`, which `clawdline todo add` also applies to stdin), and the Session under its 500 open rows (`session.direct_todos`) | The whole call is refused by name (`too_many_todos`, `todo_too_large`, `todo_text_required`, `direct_todos_full`) and no row is written; a batch is never cut to the rows that fit. | The sender receives a typed refusal; the row is present in `/v1/diagnostics.capacity`. | No | ✓ |

另外兩件跟「誰會知道」直接相關的：`plan.md` §3.2 與 `cross-platform.md` 還寫 `scheduler` 在 `/v1/health`，實際已經在
`/v1/diagnostics`；`git/changes.go:80-82` 的註解說「every file read goes through an `io.LimitReader`」，至少 6 處不是
（`result.json`、`task.json`、`board/settings.go:61`、`nextconfig.go:119`、`pinned.go:277`、`identity.go:76`）。

### 2.4 最近的牆（舊 app，今天還在用的那一個）

| 什麼 | 何時 | 到時的樣子 | 依據 |
|---|---|---|---|
| 時間軸 entries | **已撞**（09-17 02:06:58） | 一切新紀錄被拒，含 47 筆已驗證的 landing | O1，實測 |
| 看板收據 4,096 | **約 5–8 天**（9/23–9/26） | 開始 FIFO；重試可能變成重複執行，而且畫面不會說 | O15；外推（收據沒有時間戳，用 revision 比例換算） |
| task 紀錄 30 天 | **2026-09-28** | 最舊的 task 紀錄每 6 小時被安靜刪掉一批；用量頁上那些任務變成「無法歸因」 | O5；最舊一列 created 08-29 13:12，settled 時間未逐列量 |
| 看板卡片 2,000 | **約 29 天**（10 月中） | 新卡拒絕，自動匯入的任務不再出現在看板上 | O14；外推，近 7 天每天 41 張 |
| 家規 16,000 字 | **已用約 96%** | 再加幾百字，主檔的最後一段規則就會被切掉（local 那份先花預算，不會被切），只有 child 看得到那句說明 | O34 |
| `Clawdline.log` | 沒有牆，**是磁碟** | 近三天每天 21–38 MB，高峰 62 MB；目前 313 MB | O21 |

**這張表裡的每一面牆，今天都沒有任何東西會在撞上時告訴使用者。**這些都在舊 app，本 task 不能改它；root 要不要先把它們轉告使用者，
見 §8 第 1 項。

---

## 3. 哪些是缺陷

判準只有一個問題：**這筆資料是什麼類別，而它在到頂時的行為，是不是這個類別唯一正確的行為。**
類別見 §4.2；這裡先用結果分組。

### 3.1 (a) 該淘汰卻拒絕

| 舊 | 新 | 為什麼該淘汰 |
|---|---|---|
| O1 時間軸 entries | —（未移植） | 98.7% 是 git 匯入的**觀察**，可以從 git 逐字重算（§6）；用同一個計數器擋住證據（landing）與觀察（git 歷史），觀察一滿，證據也跟著進不來 |
| O2 時間軸 events／bytes、O4 checkpoints | — | 同上 |
| O3 時間軸 `requestReceipts` | — | 冪等收據只需要活過重試視窗；滿了就讓整個指令 API 永久失效，是把「保護」變成「停機」 |
| O12 durable-reports | — | 報告是敘事，可以移到冷層；沒有刪除 API，滿了就永遠滿 |
| O14 看板卡片 | （N25 是名義上的上限，實際沒有執行，計在 c） | 已結案的卡是溫資料，該讓位給新卡，而不是讓新卡進不來 |
| O16 看板檔案 | N26 | 遠，但同一形狀：載入超限就整個看板 503 |
| O30 Cloud replay window | N21 | 閒置超過封包最長壽命的 sender，它的序號窗已經不可能再被重放利用，可以安全淘汰；不淘汰就要等重啟 |

**原則：**拒絕只適合「不能淘汰」的東西（§4.2 的證據類）。可以重算、可以摘要、已經結案的東西佔住空間，讓新的東西進不來，
這不叫保護，是**把遺忘的成本轉嫁給未來所有的寫入**。**而拒絕無論如何都要大聲**：拒絕的那一刻就是警報的那一刻。

### 3.2 (b) 該拒絕卻無聲截斷、覆蓋或淘汰

| 舊 | 新 | 丟的是什麼 | 應該怎樣 |
|---|---|---|---|
| O5 task 紀錄 1,350／30 天 | —（第一波不刪，但也沒警報） | **證據**：交付事實、用量歸因（F1：149 列） | 不刪（broker-design §6.1 L2）；空間吃緊時拒絕新派工並大聲說 |
| O6 `session_deliveries` 200 | — | **未結清**的交付義務 | 未結清的永不淘汰；已結清的才是溫資料 |
| O15 看板收據 FIFO | N24 | 冪等保護；錯誤回應也佔一格 | **按時間**過期（視窗外），視窗內滿了就拒絕新指令（429＋`Retry-After`），不淘汰視窗內的 |
| O17 看板歷史 log 寫失敗被吞 | — | 歷史事實 | 寫失敗是具型別的結果，計入 diagnostics；內嵌視窗只裁已經確認寫進 log 的 |
| O27 送出去重只在記憶體 | （`start.go` 的冪等快取同樣只在記憶體） | 重啟後重送會打兩次字 | 冪等收據落盤（舊 Board 已經這樣做），視窗內跨重啟有效 |
| O32 推播 body 二分截短 | —（新版有 log） | 通知內容 | 截短可以，但要在內容裡標記「…」並計數 |
| O33 summary 2,000 字截斷、`result.json` 超過 2 MiB 當作不存在 | N8、N9 | **child 的交付敘事**；「做完了」被讀成「沒做完」 | 全文存進 `broker_task_text`，截斷只發生在畫面；超大的 `result.json` 回具型別的拒絕 `result_too_large`，不當作不存在 |
| O35 transcript 8 MiB 尾端無旗標、`truncation` 沒人讀 | N17 | 使用者看不到舊對話，而畫面讀起來像完整的 | 截斷一律帶旗標，前端一律畫出「更早的內容沒有載入」 |
| O36 圖片淘汰仍被引用的 | N15 | **使用者的輸入** | 被 transcript 引用的圖以位元組預算保留，預算到頂時先告警再淘汰最舊的，淘汰要計數 |
| O37 Drops 40 | N16 | 已經寫進 prompt 的檔案 | 同上 |
| — | N2 事件寫入失敗被丟掉 | **事實**，磁碟滿時全部 | 寫入失敗是具型別的錯誤；`store.write_errors` 進 diagnostics；證據類寫不進去時 health `ok:false` |
| — | N3 broker record 解不開就跳過 | task 從畫面上消失 | 計數並以 `unreadable` 列出現，不是消失 |
| — | N5 `progress.json` 超長直接丟 | 進度 | 截斷並計數，或回寫一個具型別的拒絕 |
| — | N19 screen bus 滿了丟 | 最新狀態 | **最新值 channel 要覆蓋，不是丟**——配對 watcher 的 `offer`（`authority.go:760`）已經是對的寫法 |
| — | N20 Cloud 入站佇列丟最舊 | **指令** | 拒絕新的並告訴送的人（busy＋重試），不丟已經收下的 |
| — | N27 auth body 截斷後當成 `{}`；N28 文件列表無旗標 | 請求內容／列表 | 拒絕 413；截斷一律帶旗標 |

**原則：****用一個吵的失敗換一個安靜的失敗**（`board-design.md` §2.8 引用的 `80173fb1`，這份考古裡最該照搬的一句話）。
截斷只能發生在**呈現**，不能發生在**儲存**；任何截斷、覆蓋、淘汰都要留下「掉了多少」的計數，而且讀的人要看得到。

### 3.3 (c) 根本沒有上限

| 舊 | 新 | 會長到哪裡 |
|---|---|---|
| O21 `Clawdline.log` 313 MB | N29（stderr，去向未定） | 磁碟 |
| O22 `remote-audit.jsonl`、O23 kept 重複寫 | N12 | 磁碟；舊版的 `recentAudit(limit: 200)` 為了取最後 200 行會讀整個檔（雖然現在沒有呼叫者） |
| O10 四個集合、O11 `owned-storage` | N1 SQLite 全部資料表 | 解碼成本跟著全部歷史長（N1 的每 2 秒全表解碼） |
| O13 `usage.sqlite3` | — | 帳本本來就不該刪，缺的是空間警報 |
| O31 推播訂閱 | N14 | 新版：超過 1 MiB 時**所有推播都失敗** |
| O38 worktree 508 MB | N10、N11 | 磁碟；新版沒有任何清掃 |
| — | N13 裝置清單 | **超過 4 MiB 時 auth 在啟動時整個失敗**——一個沒有上限的東西，最後撞上的是另一個東西的上限 |
| — | N9、N27 | 單一請求的大小 |

**原則：**「沒有上限」本身不是缺陷——**證據就不該有筆數上限**。缺陷是**沒有人知道它在長**。所以每一個沒有上限的東西，
都要換成下面兩者之一：(1) 有明確的上限與到頂行為（log 輪替、快取 LRU、請求大小）；(2) 刻意不設筆數上限，但以位元組與磁碟空間
登記、有成長速率與「預計多久會到」的預測，會叫（證據類）。N13 說明了第三種情形：**沒有上限的東西，會在別人的上限上爆炸**，
所以讀取上限（4 MiB、1 MiB）也要登記，而且要比寫入端先叫。

### 3.4 橫跨三類：行為對了，但沒有人知道

O8／N7 dead letter、O28 ledger 的拒絕計數沒人讀、O34 家規截斷只有 child 知道、
O39 task 目錄刪除沒有 audit——**這些都是對的行為**，缺的是「誰會知道」。

反過來，舊 app 唯一做對的地方是 O26：`http_reliability` 在 `/v1/health` 裡把 `limits.streams: 16` 和
`metrics.streams{current, peak, refused, evicted}` 放在一起。今天當場讀得到 `streams.evicted = 1`。**它的 HTTP 層滿了看得見，
它的儲存層滿了看不見；差別不在技術，而在於前者有一張表，後者沒有。**

**原則：**一個上限＝一個數值＋一個到頂行為＋一個計數器＋一個會被告知的人。四個缺一個，就是缺陷。

### 3.5 順帶發現：舊 app 的測試寫進了使用者正式的時間軸

時間軸 27 筆 `landed_to_git` 裡，**10 筆的 task id 是測試用的假值**（`10101010-2020-3030-4040-505050505050`、
`30303030-4040-5050-6060-707070707070`，還有一個是 6、7、8、9、a 依序各自重複排成的 id…），標題是
「a record naming the wrong commit」「landing state machine」「legacy pending row」，`effectiveAt` 是 `10`、`20`、`30`、`5300`
（也就是 1970 年），觀察時間都是 2026-09-10 13:39 前後。這些 id 在舊 repo 的
`Tests/LandingCurrencyTests.swift`、`OrchestratorLandingTests.swift` 等檔裡找得到。

機制：`ProjectTimelineIntegration.observeBrokerRecord` 用的是 `storeForTesting ?? .shared`，而 `.shared` 的路徑寫死在
`~/.config/clawdline/project-timeline.json`（`ProjectTimelineStore.swift:11, 95-99`）。landing 測試走到
`Orchestrator.recordLandingInLedger`（`:2924-2928`）時沒有設 `storeForTesting`，就寫進了使用者的正式 store。

**這跟容量有關**：測試吃掉了正式 store 的容量，而且把假資料混進了「證據」那一欄。新版的對應規則見 §4.8。

---

## 4. 新版的統一規則

### 4.1 規則零：每一個有界的東西都要登記；沒登記的上限就是缺陷

新版把 O26 的形狀推廣到所有東西：`internal/domain/capacity` 一張登記表（刻意不叫 `limits`，那個名字已經是方案額度的讀取器，
`internal/adapters/limits`）。每一列：

| 欄位 | 意思 |
|---|---|
| `name` | 穩定的鍵：`audit.security`、`log.daemon`、`store.db`、`broker.task_text`、`board.receipts`、`sse.subscribers`、`cloud.relay_queue`… |
| `class` | 資料類別（§4.2），**決定到頂時准許的行為** |
| `unit`／`limit` | 筆數、位元組或天數，至少一個；**可注入，而且覆寫只准調小**（§4.7） |
| `warn_at` | 預設 80%；可以逐列覆寫（例如看板檔案在 8 MiB 就該說，見 §5） |
| `at_limit` | `refuse`／`evict_oldest`／`expire`／`rotate`／`summarize`／`coalesce`／`disconnect`，必須是該類別准許的 |
| `measure()` | 回傳 `used`、`oldest_at`；**要便宜**（`stat`、有索引的 `COUNT`，不讀整個檔——`recentAudit` 讀 5.7 MB 取 200 行就是反例）；**量不到回 `unknown`，不回 0**（broker-design F5） |
| 計數器 | `refused`、`evicted`、`expired`、`rotated`、`dropped`、`write_errors`、`last_action_at`；行程內累計，重啟歸零並附 `counting_since` |
| `owner` | 誰會被告知（§4.5） |

登記表本身有兩道守衛：一個測試走過每一列，檢查 `at_limit` 是該類別准許的、`measure` 存在、`limit` 可注入；另一道掃 `internal/`
裡的 `max…`／`…Limit`／`Maximum…` 常數與有界 channel，沒有對應登記就紅。**第二道守衛上線的第一天就應該紅在 N25**
（`MaximumItems` 宣告了卻沒人用），這正好是它會紅的證明（`plan.md` §10：「那個守衛自己要能證明會紅」）。

背景工作樹新增兩列。`sessions.agent_rows` 是每個 session 最多帶到畫面的 provider agent 數，限制 6，較舊列留在 provider 的原始紀錄並以
`agents_reading.truncated` 明說省略數；`cache.background_agents` 有三張可重建的 LRU：immutable metadata（Claude sidecar 與 Codex rollout head 共用）、
transcript tail、完成通知 cursor，每張最多 256。穩定的一次掃描仍會 `stat` 近期 Claude agent 以判斷是否有變，但快取命中不再開 transcript；Codex 則用已知的 thread id
命中快取，不在每次 beat 重走 rollout 目錄。讀不到來源時讀數是 `unknown`，不把它畫成 0。

`places.registered` 是 `CLAWDLINE_NEXT_DIR/places.json` 中由人明確保留的 Project 目錄，限制 512 筆。
它是人的選擇（`evidence`），所以滿了拒絕新增，不自動淘汰；只有 `clawdline project remove` 會移除。
diagnostics 每次直接數這個最多 512 列的檔案，壞掉或讀不到回 `unknown`，不把既有 Project 說成零個。

Work v2 的看板項目與直接 Session 待辦參考圖片是使用者輸入，在其主體存續期間不可自動淘汰，因此以 evidence 列登記並在滿載時拒絕：
`work.images_per_item` 6 張、`work.image_bytes` 每張正規化 PNG 5 MiB、
`work.image_bytes_per_item` 15 MiB、`work.image_bytes_total` 512 MiB；另有 buffer 列
`work.image_request_body_bytes` 18 MiB（保留加密 Cloud envelope 的膨脹空間），同時約束本機 HTTP 與 Cloud 子文件。Diagnostics 分別量
最滿主體（項目或直接待辦）的張數／位元組、最大單張及全庫位元組；不會為新圖片刪除既有參考資料。

### 4.2 資料分類：什麼絕不能丟、什麼可以摘要後丟、什麼可以直接丟

| 類別 | 例子 | 可以丟嗎 | 到頂時 |
|---|---|---|---|
| **證據** `evidence` | landing 紀錄與它的更正（`corrected_from`）；交付、驗證收據；review 的裁決、axes 與 finding metadata；closure attestation；task 紀錄本體（L2）；未結清的 obligation；`result.json` 的結構化欄位 | **絕不** | **拒絕新寫入**，並立刻 health `ok:false`。上限不用筆數，**用位元組與磁碟空間**，設在正常使用下碰不到的地方，所以到頂＝真的出事了 |
| **安全稽核** `security_audit` | 配對、裝置、密碼、權限；遠端寫入（`session.send`、`key`、`interrupt`、`close`、shell、上傳） | **絕不**（可以輪替成分段檔，分段不刪） | 輪替；磁碟吃緊時照證據處理 |
| **冪等收據** `idempotency` | 看板 `(actor, requestId)`、`Idempotency-Key`、Cloud command ledger、送出去重 | 過了重試視窗可以 | **按時間過期**；視窗內滿了就**拒絕新指令**（429＋`Retry-After`），不淘汰視窗內的；視窗外的重送回具型別的 `receipt_expired`，**不重新執行**。要分得出「過期的重送」與「新指令」，request id 必須帶時間（UUIDv7 或附 `issued_at`）；做不到的，過期時只留 key＋digest 的精簡墓碑（每筆幾十位元組） |
| **使用者輸入** `user_input` | 上傳的圖、drops | 不再被引用時可以 | 位元組預算；被引用的先告警再淘汰最舊的，淘汰要計數、要讓讀的人看到原因 |
| **長文** `narrative` | child summary、review 的散文、graph、plan、durable report | 可以**摘要後**移走 | 90 天後在 DB 留摘要（前 280 字＋sha256＋位元組數），原文移到冷層（§4.3），**不直接刪** |
| **進度** `progress` | progress notes、session activity | 可以摘要後丟 | 視窗＋`droppedCount`（照舊版的 5 則） |
| **觀察** `observation` | executor `observed_at`、SSE 影格、git 歷史匯入、看板機器對帳 | 可以，**可重算** | 最好不落盤（broker-design §6.2）；落盤的淘汰最舊，但**被引用的釘住** |
| **營運紀錄** `journal` | `orchestrator.*` 生命週期、`worktree.kept`、`completion.attempt` | 可以 | 只記**狀態轉換**，不記重複；broker 的生命週期本來就在 store 的 `events` 裡，不再重寫一份到 audit |
| **工作區** `work` | task 目錄、worktree、build 產物 | 擁有者確認不在、內容已保存後 | 時間＋擁有者檢查，**未知就保留**（F3、F5）；保留的數量與位元組進 diagnostics |
| **快取** `cache` | transcript titles、project readings | 可以 | LRU |
| **診斷 log** `diagnostic_log` | daemon log | 可以 | 按大小輪替（10 MiB × 5），0600 |
| **即時緩衝** `buffer` | SSE 每訂閱者緩衝、Cloud 入站佇列、終端機 lane | **丟了就要讓對方知道** | 最新值 channel `coalesce`；指令佇列 `refuse`（告訴送的人）；慢消費者 `disconnect` 後重拿快照（照 O26）。**絕不默默丟中間的一段** |

**稽核要拆成兩份。**舊 audit 的位元組有 80.8% 是 `orchestrator.*`——那是營運紀錄，而且在新版已經是 store 裡的事件，不需要再寫一份；
安全事件不到 1%，遠端寫入（`session.*`、`voice.*`、`place.*`…）約 18%。拆開之後，「稽核永不丟」每年只要十幾 MB，
而不是跟著 broker 的每一次心跳一起長。

### 4.3 分層：熱／溫／冷——要，但層由類別決定，不由年齡決定

| 層 | 放什麼 | 在哪 | 怎麼讀 | 界線 |
|---|---|---|---|---|
| **熱** | 活著的 task、未結清的 obligation、視窗內的冪等收據、最近的觀察 | SQLite 主表＋記憶體 | 熱路徑直接讀 | 工作集大小；分頁 |
| **溫** | 已結案的 task 紀錄（L2）、90 天內的長文（L3）、看板已結案的卡與每項歷史、安全稽核的當前分段 | 同一個 SQLite，但**熱路徑不碰**（第一步就要讓 N1 的「每 2 秒解碼全表」只碰熱的） | 分頁、按需 | DB 位元組預算（預設 1 GiB 告警）＋磁碟剩餘 |
| **冷** | 長文原文（摘要之後）、稽核的舊分段、舊 app 凍結下來的檔 | DB 之外、壓縮、append-only 的分段檔，`CLAWDLINE_NEXT_DIR/archive/` | 只有明確的工具（CLI、設定頁的「匯出」）會讀 | **只有人能刪**，而刪除本身寫進安全稽核 |

量級：長文佔舊 task 列的 46%（broker-design §2.1），2.49 MB／769 筆＝每筆約 3.2 KB，每天 36 筆一年約 42 MB 原文，壓縮後約十 MB。
所以「冷層不刪」付得起。

### 4.4 到頂時的行為，以及空間吃緊時的讓位順序

§4.2 的最後一欄就是到頂行為。空間（磁碟或 DB 預算）吃緊時，依序讓位，**每一步都記一筆事件、進 diagnostics**：

1. 丟快取
2. 輪替並刪除超出預算的**診斷 log** 分段
3. 提前把溫層長文摘要進冷層
4. 拒絕**可選**的寫入：git 歷史匯入、觀察、營運紀錄
5. 拒絕新派工與新指令：具型別的 `507 storage_exhausted`，health `ok:false`
6. **永遠不做**：淘汰證據、安全稽核、視窗內的冪等收據

### 4.5 誰會被告知

| 管道 | 什麼時候 | 內容 |
|---|---|---|
| `/v1/diagnostics.capacity`（本機 token） | 永遠 | 每一列的完整讀數（下面的形狀） |
| `/v1/health`（公開） | **只在證據或安全稽核類到頂，或讓位走到第 5 步時** | `ok:false`＋`reason:"capacity_exhausted"`；**名字與數字不上公開的 health**，只在 diagnostics。warn／critical 不翻 `ok`，避免「一直是紅的」 |
| 推播（既有的 `internal/adapters/push`） | 進入 `critical` 或 `full` 時；回到正常時一則「已恢復」 | 一則一句話：什麼、多滿、哪天會到、要做什麼。每列 24 小時最多一則；**預算與 agent 的 30／小時分開**，不跟 agent 搶 |
| 畫面 | 任一列不是 `ok` 時 | Dashboard（Go 自己的頁，不在 1:1 範圍內）一個「容量」面板。主畫面橫幅見 §8 第 3 項 |
| store 的 `events` | 每次狀態轉換、每一批淘汰／過期／輪替 | `capacity.state{name, from, to}`、`capacity.evicted{name, count, oldest_at}`；**一批一筆，不是一項一筆**（O23 的反例） |
| `clawdline doctor` | 手動 | 印整張表，給沒有瀏覽器的 Linux |

`/v1/diagnostics.capacity` 的形狀：

```json
{"capacity": {
  "counting_since": 1789694351,
  "beat": {"at": 1789694400, "tick_seconds": 60},
  "entries": [
    {"name": "audit.security", "class": "security_audit", "unit": "bytes",
     "used": 412300, "limit": 8388608, "ratio": 0.049, "state": "ok", "at_limit": "rotate",
     "rotated": 0, "refused": 0, "evicted": 0, "oldest_at": 1789600000,
     "growth_per_day": 31000, "projected_full_at": 1802000000},
    {"name": "board.receipts", "class": "idempotency", "unit": "rows",
     "used": 2639, "limit": 4096, "window_seconds": 86400, "state": "ok", "at_limit": "refuse",
     "expired": 0, "refused": 0},
    {"name": "store.db", "class": "evidence", "unit": "bytes",
     "used": 69632, "limit": 1073741824, "disk_free_bytes": 180000000000, "state": "ok",
     "at_limit": "refuse", "write_errors": 0}
  ]
}}
```

### 4.6 水位與預測

- `ok` < 80% ≤ `warn` < 95% ≤ `critical` < 100% ≤ `full`；**另外有 `unknown`**（量不到），它不是 `ok`。
- **遲滯**：往下要比門檻低 5 個百分點才回去，不然在 95% 附近會一直通知。
- **預測**：用最近 7 天的成長算 `projected_full_at`。**距離到頂不到 14 天，就算比例還低也升到 `warn`**——撞牆前兩週說，
  而不是撞牆那一刻說。§2.4 那張表就是這條規則今天會產出的東西。
- 量測用一個自己的 beat（60 秒，跟 scheduler 同一個節奏），而且**這個 beat 本身也要可觀察**（`plan.md` §3.2 的規則：
  「巡過但沒事」和「巡邏停了」在外面看起來都是安靜）。

### 4.7 怎麼證明它會叫

**要能故意把某個東西塞滿，然後看到警報。**分四層，前兩層每次都跑，後兩層驗收時跑一次：

1. **domain 表格測試（純函式）**：每個類別注入 `limit = 20`，依序寫到第 16（80%）、19（95%）、20（100%）、21 筆，斷言：
   狀態 `ok → warn → critical → full`、第 21 筆觸發該類別的到頂行為（拒絕／過期／輪替／覆蓋）且計數器加一、
   **每次往上跨越只產出一個通知意圖**、在第 18／19 筆之間來回不會再通知（遲滯）。
2. **登記表守衛**：§4.1 的兩道；第二道要附一個會紅的樣本（未登記的常數），證明它不是擺設。
3. **`clawdline doctor capacity --drill <name>`**：只在一個**拋棄式的** `CLAWDLINE_NEXT_DIR` 上執行（偵測到它是使用者正在用的那個
   目錄、或有 daemon 在用它，就拒絕），**走真的寫入路徑**把指定的東西寫到上限＋1，然後印出 diagnostics 那一列與產出的通知意圖。
   只有在「狀態真的走到 `full`、到頂行為真的發生、每次跨越正好一則通知」時才 exit 0。這一層不需要瀏覽器，CI 可以跑。
4. **端到端一次**：在拋棄式 port 上起一個 daemon，以 `CLAWDLINE_NEXT_CAPACITY=audit.security=4KiB,store.db=<目前大小＋32KiB>`
   縮小上限，做幾次會寫稽核的動作（例如 `clawdline open --print` 發一個裝置、再登出；兩者是否真的寫稽核，驗收時先確認），
   看 `/v1/diagnostics` 出現 `rotated ≥ 1`；再寫到 `store.db` 超過上限，看 `/v1/health` 變 `ok:false, reason:"capacity_exhausted"`，
   並看到一筆 `capacity.notify` 事件（拋棄式 daemon 沒有推播訂閱，所以驗的是通知意圖，不是手機）。

**覆寫只准調小**，而且覆寫中的列在 diagnostics 標 `overridden: true`、log 記一行——防止有人（包括我們自己）把上限調大來讓警報閉嘴。

**現成的滿載樣本**：舊時間軸 `project-timeline.json` 就是一份真實世界的 100% 樣本、看板檔是 64% 的收據樣本。
等新版有時間軸或看板收據的讀取器時，拿它們的**副本**當 fixture，應該直接看到 `full`／`warn`。

### 4.8 測試不能寫進正式的目錄

§3.5 的教訓寫成規則：**預設路徑在測試裡不可以解析到使用者的目錄。**新版的做法：所有 store 的預設路徑只從
`CLAWDLINE_NEXT_DIR` 解析；測試的 helper 在 `testing.Testing()` 為真、而目錄解析結果落在 `$HOME` 底下時直接 panic。
這條守衛也是一個上限——它限制的是「誰可以吃正式 store 的容量」。

---

## 5. 與既有文件的一致與分歧

| 文件 | 那邊怎麼說 | 本文 | 一致？理由 |
|---|---|---|---|
| `broker-design.md` §6.1 L1 | 工作目錄 24 小時＋擁有者活著就保留 | 同（§4.2 `work`），另加：保留的數量與位元組進 diagnostics、重複的 kept 只記一次 | **一致**。補充：新版目前**連 L1 都沒有**（N10、N11） |
| `broker-design.md` §6.1 L2 | 熱列不刪（每年約 97 MB） | 同（證據類） | **一致**。補充：「不刪」必須搭配 `store.db` 的位元組告警，否則就是另一個「沒人知道它在長」 |
| `broker-design.md` §6.1 L3 | 長文 90 天，可設定（刪除） | 90 天後**留摘要、原文移到冷層**，不直接刪；review 的裁決、axes 與 finding metadata 是**證據**，留在 L2，只有散文進 L3 | **分歧**。理由：summary 是人唯一讀得懂的「這個 child 做了什麼」，冷層一年約十 MB 付得起（§4.3）；F1 的教訓是「刪掉的證據再也歸因不回來」，把裁決跟散文一起刪會重演 |
| `broker-design.md` §6.2 | 事實落盤、觀察不落盤 | 同（§4.2 `observation`） | **一致** |
| `broker-design.md` §6.6 | `broker{beat, notices, store, lane, landing}`；beat 停了 health `ok:false` | `capacity` 放在 `broker` 旁邊；health 的 `reason` 共用一個列舉（`broker_beat_stalled`、`capacity_exhausted`）；`broker.store` 管寫入健康，`capacity` 管填充度，不重複 | **一致**。注意：進行中的 broker B2＋B3 那個 task（認領 `api/v1`、`internal/adapters/store`）正在做這一塊，§7 第一步要排在它之後 |
| `timeline-design.md` B1、§6 第 3 項 | 「不是改成自動刪除」；容量到了講出來，**由人決定封存** | **可重算的 git 歷史**（且沒有被看板引用的）自動淘汰；被引用的 12 筆釘住；landing／部署證據**永不自動淘汰**、用自己的預算；人決定的封存只留給證據 | **分歧**。理由：他們避開自動刪除，是怕「刪掉還被引用的」——這用釘住解決。git 歷史 100% 可從 git 重算（`ProjectTimelineGitImporter.swift:86-88`：標題＝commit subject，摘要與分類是常數）。**要人決定才能讓位，等於在沒人看的時候重演今天的凍結**，而使用者正在睡覺 |
| `timeline-design.md` B3 | 用位元組與時間視窗當主要界線 | 同，再加**分類預算**：觀察滿了不能擠掉證據 | **一致** |
| `timeline-design.md` §0 表 | `eventReceipts` 上限 4,096、`checkpoints` 12 | `eventReceipts` **沒有自己的上限**（4,096 是 `requestReceipts`，`ProjectTimelineStore.swift:9, 157`），它跟著 events 受 20,000 限制；checkpoints 現在 13 | **更正**（數字的小誤植，結論不變） |
| `timeline-design.md` §2.1 | 27 筆 `landed_to_git` | 其中 10 筆是測試假資料（§3.5） | **補充**（新發現） |
| `board-design.md` C1、§6 第 5 項 | 文件超過 8 MB 時在 `/v1/health` 講一次；只說，不自動清 | `board.store` 以 `warn_at = 8 MiB` 登記；數字放 `/v1/diagnostics`，不上公開的 health | **小分歧**。理由：新版公開的 health 刻意只有三個欄位；照 §4.5，只有證據類到頂才翻公開的 `ok` |
| `board-design.md` B8 | 收據會 FIFO 淘汰；**把契約文件的句子改成**「收據會淘汰，且 `evictedReceipts` 會說」 | **改行為**：按時間過期、視窗內滿了拒絕新指令 | **分歧**。理由：FIFO 按筆數，在大量寫入時會淘汰**還在重試視窗內**的收據，重送就變成重複執行；而且錯誤回應也佔一格。舊 app 5–8 天內就會開始淘汰（§2.4） |
| `board-design.md` C2 | `spans` 比照 `history` 做視窗＋`droppedCount` | 同（§4.2 `progress`／`observation`） | **一致** |
| `board-design.md`（未提） | — | 卡片 2,000 張是**拒絕**型上限，舊 app 約一個月後撞到；新版宣告了但沒用（N25） | **補充**（新發現） |
| `cutover.md` §3 | `orchestrator.json` 不轉檔、當成凍結的歷史；`remote-audit` 各自、不合併；時間軸「整個沒有移植」 | 時間軸同樣凍結、不轉檔（§6）；新版的稽核拆成兩份（§4.2） | **一致**。另外 B5「舊資料凍結並備份」應把 `project-timeline.json` 的 sha256 與筆數一起記下 |
| `plan.md` §3.2 | `/v1/health` 的 `scheduler` | 實際在 `/v1/diagnostics` | **文件過時**，照實際 |
| `plan.md` §11 | 所有上限可注入 | 同，另加「覆寫只准調小」 | **一致**。現況：新版除了 `orchestrator_max_children` 以外沒有任何上限可設定 |

---

## 6. 舊 app 那個已經滿掉的時間軸：新版接手時怎麼處理那 2,000 筆

**一句話：凍結成唯讀的歷史——原檔原樣留著、不搬進新版、也不做摘要；新版的時間軸從空的開始，git 歷史從 git 重新推導，
landing 從新版自己的 landing 紀錄推導。**

理由，全部是量的：

| 那 2,000 筆裡是什麼 | 筆數 | 能不能從別處得到 | 所以 |
|---|---:|---|---|
| `git_history_observed` | 1,973（98.7%） | **能，逐字**：標題＝commit subject，摘要是常數句「Read-only Git history evidence…」，分類固定 `feature`、標籤固定 `["git"]`（`ProjectTimelineGitImporter.swift:86-88`）；`sourceID` 就是 `github:<repo>:<sha>` | 搬＝複製一份 git 已經有的東西 |
| `landed_to_git`，task 在 `orchestrator.json` | 17 | **能**：原始 landing 紀錄在 `orchestrator.json`（也凍結，`cutover.md` §3） | 從那邊推導，不從時間軸 |
| `landed_to_git`，測試假資料 | **10** | 不需要 | **搬過來就是把假資料當成證據** |
| 部署／可用性證據 | 0 | — | 沒有任何獨有的東西 |
| 有看板關聯的 entry | 12 | 看板那邊有項目 id | 新版若要保留這個連結，從看板側重建 |

也就是說，**這 2,000 筆裡沒有任何一筆是只存在於這個檔案裡、而且是真的。**

不選另外兩個選項的理由：

- **原樣搬**：10 筆假 landing 會變成新版的「證據」；而且 2,000 筆會一進來就佔滿新版時間軸的觀察預算，等於把「已經滿了」一起搬過去。
- **摘要**：摘要的價值在於原文會消失，但這裡的原文就在 git 裡，永遠不會消失。

具體做法：

1. **不寫它。**新版對 `~/.config/clawdline` 只有 `O_RDONLY`（`plan.md` §4），連它的 0644 權限（`timeline-design.md` B4）也不由新版修。
2. `cutover.md` 的 B5 備份時，記下它的 sha256、大小（2,495,417 B）與 `revision`（2491），這樣之後說「凍結」是可驗證的。
3. 新版的時間軸（如果做，`timeline-design.md` §6 第 4 項建議先不做）用 §4 的規則：git 歷史是 `observation` 類、可淘汰、被引用的釘住；
   landing 是 `evidence` 類、自己的預算、永不自動淘汰。
4. 凍結之後遺漏的那 **47 筆已驗證 landing**（09-17 02:06 之後、`landed_at` 晚於凍結時間、全部 `state: landed`）仍在
   `orchestrator.json` 裡，**但那份檔案 30 天後會開始刪**（O5）。如果新版需要切換前的 landing 歷史，要在 2026-10 中之前從它讀出來；
   這是一個有期限的決定，列在 §8。

---

## 7. 最小可驗收的第一步，與實作順序

### 7.1 第一步：一張登記表、一個 diagnostics 區塊、一個會叫的例子

**範圍**（一個 child、一個隔離 worktree；排在 broker B2＋B3 那個 task 落地之後，因為它認領了 `api/v1` 與 `internal/adapters/store`）：

1. `internal/domain/capacity`：`Entry`、`Class`、`State`、`Reading`、類別→准許行為的表、水位＋遲滯＋預測的純函式狀態機。
2. 登記四列**今天就存在**的東西：
   - `audit.security`：`remote-audit.jsonl`（N12，c）→ **改成按大小輪替**（8 MiB 一段、分段不刪、0600）。這是第一步唯一的行為改動，
     因為它最小、最獨立、而且是 (c)。
   - `store.db`：DB 檔大小＋磁碟剩餘（證據類，會翻 health）。只量，不改行為。
   - `board.receipts`（N24，b）：先把現有的淘汰計數**搬上 wire**；行為（按時間過期）留到第三步。
   - `cloud.relay_queue`（N20，b）：現有的 `queue_dropped` 接進來。行為留到第三步。
3. `/v1/diagnostics.capacity`（§4.5 的形狀）與契約；`/v1/health` 在證據類 `full` 時 `ok:false, reason:"capacity_exhausted"`。
4. §4.7 的第 1、2、3 層：表格測試、登記表守衛（附會紅的樣本；**上線第一天就該紅在 N25**，處理方式是登記它或刪掉它）、
   `clawdline doctor capacity --drill audit.security`。

**驗收**（一次 build、一次測試，照家規）：

- `go test ./internal/domain/capacity ./internal/adapters/devices` 綠，而且新測試在「輪替被拿掉」的版本上會紅（讀它，或在那裡跑一次）。
- 在拋棄式目錄上 `clawdline doctor capacity --drill audit.security` exit 0，印出 `ok → warn → critical → full → rotated=1`，正好一個通知意圖。
- 拋棄式 daemon 的 `GET /v1/diagnostics` 出現四列，`store.db` 的 `used` 等於檔案實際大小；`board.receipts` 的 `used` 等於 fixture 裡的收據數（新版看板 store 若與舊檔同格式，可直接拿舊檔的**副本**當 fixture，應讀出 2,639／4,096；格式是否相同本文沒有驗證）。
- `go run ./tools/contract-gen -check` 通過。

### 7.2 之後的順序

| 波 | 內容 | 為什麼排這裡 |
|---|---|---|
| 2 | **(c) 收口**：daemon log 寫進 `CLAWDLINE_NEXT_DIR/logs/`、10 MiB × 5 輪替、0600（N29）；稽核拆成安全與營運兩份（§4.2）；task 目錄與 worktree 的清掃（N10、N11，照 L1 與 `cutover.md` B4：未落地的先存成 patch）；裝置清單與推播訂閱的上限要比讀取上限先叫（N13、N14）；記憶體快取 LRU（N18）；`result.json` 與缺上限的 request body（N9、N27） | 都是「還沒開始長」的東西，現在改最便宜；N13 是會讓 auth 整個起不來的那一個 |
| 3 | **(b) 收口**：看板收據按時間過期＋`receipt_expired`（N24）；Cloud 入站佇列改成拒絕並告訴送的人（N20）；screen bus 改覆蓋（N19）；`Store.Append` 的錯誤計數與證據寫入失敗翻 health（N2）；summary 全文存、只在畫面截斷（N8）；`progress.json` 超長計數（N5）；文件列表與 transcript 的截斷旗標，前端畫出來（N17、N28）；圖片與 drops 的「被引用就先告警」（N15、N16） | 每一項都是把一個安靜的失敗換成一個吵的 |
| 4 | **broker 保留**（跟 broker 那條線一起）：L3 長文 90 天→摘要＋冷層；營運紀錄只記轉換；熱路徑不再解碼全表（N1） | 需要 broker 的 store 形狀先穩定 |
| 5 | **通知與畫面**：容量的推播（獨立預算）、Dashboard 的「容量」面板、dead letter 與 spool 拒絕接上來（N7、N22） | 推播已經落地（`fb745a7`），但通知要有東西可說，所以排在登記表長滿之後 |
| 6 | **看板與時間軸移植時**：分類預算、git 歷史可淘汰且釘住被引用的、已結案的卡讓位給新卡（取代 2,000 張的拒絕）、`MaximumItems` 要嘛真的執行、要嘛刪掉 | 跟著那兩個功能的移植走 |
| 7 | 主畫面的橫幅 | 等 §8 第 3 項的決定 |

---

## 8. 需要使用者拍板的

> 使用者在睡覺。以下都選了安全的預設，而且都可以推翻。

| # | 問題 | 我選的預設 | 為什麼 |
|---|---|---|---|
| 1 | **舊 app 的下一面牆（§2.4）要不要現在處理？** 最快的是看板收據 5–8 天後開始淘汰、task 紀錄 09-28 起被 30 天規則刪。舊 app 有設定鍵 `orchestrator_task_record_retention_days`（`Config.swift:545`）可以調大 | **只回報，不動**：本 task 不能碰舊 app 與 `~/.config/clawdline` | 調舊 app 的設定是使用者自己的事；新版的設計不依賴它。若使用者想延後 F1 那類遺失，把那個值調大是一行設定、零風險 |
| 2 | 舊時間軸的 2,000 筆怎麼處理 | **凍結、不搬、不摘要**（§6） | 沒有任何一筆是只存在那裡而且是真的；10 筆是測試假資料 |
| 3 | 主畫面要不要有容量橫幅？ | **先不放在 1:1 的頁面**；放 Dashboard＋推播 | 橫幅需要 `zh-Hant.json` 沒有的新字串，是刻意偏離 1:1。但使用者的原話正是「畫面上看不出來」——**我建議做**，只是這要使用者決定 |
| 4 | 長文 90 天之後：刪、還是移到冷層？ | **移到冷層**（與 broker-design §6.1 分歧，見 §5） | 冷層一年約十 MB；刪了就回不來 |
| 5 | 可重算的 git 歷史：自動淘汰、還是等人決定？ | **自動淘汰，被引用的釘住**（與 timeline-design 分歧，見 §5） | 等人決定＝今天的凍結 |
| 6 | 切換前那 47 筆 landing 要不要趁 `orchestrator.json` 開始刪之前讀出來？ | **先不讀**；列為有期限的決定（2026-10 中以前） | 新版的時間軸還沒決定要不要做 |

---

## 9. 這份文件沒有量到的

- **舊 app 的 task 紀錄逐列的 settled 時間**：用 created 推 30 天規則的生效日（09-28），settled 通常晚幾小時，實際日期可能晚一天。
- **看板收據與卡片的到頂日期是外推**：收據沒有時間戳，用 revision 比例換算；卡片用近 7 天的速率線性外推。
- **Cloud replay window 的 sender 數、Drops 的現況**：在記憶體或沒量。
- **推播訂閱數**：`push.json` 含秘密，只記了大小（652 B），沒讀內容。
- **新版打包後 daemon 的 stderr 去哪裡**：沒有實際從 Finder 啟動驗證。
- **新版的成長速率**：新版 store 幾乎是空的，§4.6 的預測在新版上還沒有資料可算；§2.4 的日期全部是舊 app 的。
- **§4 的設計沒有原型**：分類、讓位順序、水位都是設計，沒有量過成本；`measure()` 夠便宜這件事要在第一步量。
- **`/tmp/.clawdline` 其他 task 目錄的內容**：照規則只量了目錄數與 `du`。
- **Windows／Linux**：路徑、磁碟剩餘的讀法、log 目錄的慣例都沒有在那兩個平台上看過。

---

## 附錄 A：數字從哪裡來

**原始碼的逐列盤點**由兩個唯讀 subagent 完成（舊 app：`Sources/`＋`Resources/web/`；新版：`internal/`、`cmd/`、`web/`），
兩邊都完整回來了。其中以下 9 條我親自重讀原始碼確認：

1. 時間軸 entries 的拒絕與 `requestReceipts` 的檢查位置（`ProjectTimelineStore.swift:7-10, 157-158, 293-298`）
2. `eventReceipts` 沒有自己的上限（同檔 `:45, 276, 303`）
3. task 紀錄的數量規則不檢查是否已結束（`OrchestratorTaskShape.swift:871-912`）
4. `Log.swift` 沒有輪替與權限（全檔）
5. `RemoteAuth.audit` 沒有鎖、沒有 `O_APPEND`、寫失敗被吞（`RemoteAuth.swift:490-510`）
6. spool 錯誤一律歸成 integrity 不重試（`CloudAppBridge.swift:1290-1296`）
7. 家規截斷與 local 規則的預算順序（`Orchestrator.swift:9296-9340`）
8. 時間軸測試寫進正式 store 的路徑（`ProjectTimelineIntegration.swift:9, 19, 78-80`；`ProjectTimelineStore.swift:11, 95-99`；`Orchestrator.swift:2924-2928`）
9. 新版：`MaximumItems` 沒有引用、沒有 `DELETE FROM`、`Store.Append` 不記錯誤、screen bus 滿了丟、audit 用 `O_APPEND`＋0600

**實測**（2026-09-18 09:10–09:40）：

| 數字 | 怎麼量 |
|---|---|
| 時間軸各欄筆數、凍結時間、47 筆遺漏的 landing、10 筆假資料 | `python3` 讀 `project-timeline.json` 與 `orchestrator.json`（只讀 `id`、`landing`、`created` 等欄，不讀 `secret_hash`）；假資料的 id 在舊 repo `Tests/` 裡 `grep` 到 |
| orchestrator 各集合、最舊一列、每天筆數 | 同上 |
| 看板卡片、收據、revision、歷史檔 | `project-board.json`、`project-board-history/` |
| `remote-audit.jsonl` 行數、天數、事件前綴的位元組佔比 | 逐行 `json.loads` |
| `Clawdline.log` 大小、每日成長、各 tag 佔比 | `wc`、`awk` 依行首日期與第三欄分組 |
| Cloud ledger 與 spool 筆數、狀態、保留時間 | `cloud-runtime/*/command-ledger.json`、`outbound-spool.json`（只讀狀態與時間欄）；spool 的累計計數取自 log 最後一行 `drain_observation` |
| `usage.sqlite3`、新版 `clawdline.sqlite3` | 主檔＋`-wal` 複製到本 task 的 `work/` 後 `sqlite3` 計數 |
| 圖片、worktree、task 目錄、reclaimed checkouts | `ls`、`du` |
| transcript 規模 | `du`、`find -size`、最大檔的 `ls -l` |
| 舊 `/v1/health` 的 `http_reliability` | `curl :7717/v1/health`（公開） |
| 新 `/v1/health`、`/v1/diagnostics` | `curl :7727`，diagnostics 帶 `~/.config/clawdline-next/local-token` |
| 新版 daemon 的 log 去向 | `lsof -p <pid>` |
| 家規字數 | Python `len(str)`（與 Swift 的 `String.count` 在這份以英文為主的檔案上一致） |

### Copied project marks

`icons.saved` retains at most 512 explicitly copied marks without automatic eviction;
`icons.side` rejects grids above 64 rows or columns; `icons.request_bytes` rejects a copy body
above 96 KiB. A full registry still permits replacing an existing mark. See
[project-icons.md](project-icons.md) for resolution, transfer and conflict behavior.
