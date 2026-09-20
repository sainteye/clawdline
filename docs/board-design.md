<!-- retired-app-record: 舊 app 的看板，量於 2026-09-18 08:20–10:05；該 app 已於 2026-09-19 停用，7717 無人監聽 -->

> **主體與時間：** 這份文件記錄的是 **舊 app 的看板**，量於 **2026-09-18 08:20–10:05**。
> **舊 Swift app 已於 2026-09-19 停掉、取消登入時啟動，7717 現在沒有人在聽。**
> 文中寫成現在式的「舊 app 還在跑」「7717」都是**量測當下**的事實，刻意保留，
> 用來對照還有哪些功能要 migrate、當初怎麼實作——**不要照著它設定今天的 daemon**。

> **整套工作系統（看板項目、Session 待辦、Backlog、GitHub Issue）怎麼運作、哪些已經做到，一份講完在 [`docs/work-system.md`](work-system.md)。**

> **實作依據是 [`docs/design-decisions.md`](design-decisions.md)（2026-09-18 起）；本文保留為分析材料，與它衝突之處以它為準。**

# 看板（Project Board）：照搬什麼、改什麼、重做什麼

> 問的是：舊版看板的設計值不值得原封不動搬進 clawdline-go。
> 判準：**重寫不是翻譯。** 被舊紀錄自己點名過的缺陷不照搬；但也不為了新而新。
>
> 量測：2026-09-18 08:20–10:05，對**正在跑的舊 app**（7717）與它此刻的資料檔與 log。
> 腳本在 `artifacts/`（`measure.mjs`、`peek2.mjs`、`schema.mjs`）。

---

## 0. 先講最重要的一件事：這個設計只有十天大

`git log` 顯示看板檔案被 **51 個 commit** 動過，全部落在 **2026-09-08 到 2026-09-17**。
而其中 20 個有完整 commit body 的，**18 個是 9/16–9/17 那兩天**——也就是說：

**舊版看板不是累積多年的技術債，它是一個十天前才寫出來、並且在最後兩天被自己狠狠量過一輪的設計。**

那兩天留下了一份 341 行的自我評估 `docs/read-path-architecture.md`，裡面已經：

- 量出 `GET /v1/board?project=` 是 1,000,265 B，其中 **58 張卡因為位元組預算而任何請求都拿不到**（後來重量是 481 張裡的 62 張）
- 量出 `progress.coordinationContext` 與 `sessionDelivery` 是 62,801 B／次、**前端沒有任何讀者**，「一分鐘送四次只為了被丟掉」
- 量出 `persist` 每次重寫整份文件，並且**算過搬走 receipts 與 evidence 只省 16 ms，明確決定不做**
- 兩次公開更正自己的估算（卡片大小估錯 3.5 倍；per-item 歷史筆數算錯，20 其實是 ×708）

所以這份文件的工作不是「找出舊版哪裡爛」，而是**分辨哪些是已經修好的、哪些是他們知道但沒修的、哪些是他們的結論在 Go 上不成立的**。

---

## 1. 三十秒版本

| | 現況 | 判斷 |
|---|---|---|
| wire 契約（19 欄）與卡片允許清單 | 用代價換來的結論 | **照搬** |
| `progress` 狀態機、`listSummary` 分組 | 產品判斷，跑在十天真實資料上 | **照搬** |
| `entitlement` | 寫死常數，前端沒人讀 | **照搬**（別發明免費／Cloud 分支） |
| 每項目 append-only 歷史 `*.jsonl` | 設計裡最乾淨的一手 | **照搬** |
| CAS＋`(actor,requestId)` 收據 | 正確的併發模型 | **照搬** |
| 兩條獨立的 Board 佇列（讀／寫） | 舊版架構評估自己說「sound and should be kept」 | **照搬** |
| 整份文件重寫（4.36 MB／次） | 他們量過：42 ms，其中 93% 是 encode。**不是瓶頸** | **照搬**（附條件，見 C1） |
| 每次寫入替全部項目算 **detail** | 他們知道，卡在鎖的紀律上沒修 | **小改**（Go 沒有那個鎖問題） |
| 一半的持久寫入是機器自己對帳 | 沒被點名過 | **小改** |
| 一個路由五種問題＋前綴選擇器 | 他們補了第二套 API，舊的沒拆 | **重新設計** |
| 位元組預算當分頁用 | 他們自己的話：「a bound on the *truthfulness* of the answer」 | **重新設計** |

---

## 2. 量到的現況

### 2.1 資料放在哪、多大

| 檔 | 位置 | 大小 | 形狀 |
|---|---|---|---|
| `project-board.json` | `~/Library/Application Support/Clawdline/` | **4,361,498 B** | 單一文件，每次寫入整份重寫 |
| `project-board-history/<item>.jsonl` | 同上 | 3.7 MB、**558 檔、12,872 行** | 每項目一個 append-only log |
| `project-board-workflow.json` | 同上 | 730,759 B | 單一文件（`runs` 256、`receipts` 375） |
| `project-timeline.json` | **`~/.config/clawdline/`** | 2,495,417 B | 位置與看板不同，見 `timeline-design.md` |

`project-board.json` 內部（`schemaVersion: 2`、`revision: 6366`、`eventSequence: 3779`）：

| 欄位 | 位元組 | 佔比 |
|---|---|---|
| `items`（781 張卡） | 3,502,368 | **80.3%** |
| `receipts`（2,630 筆，上限 4,096，已淘汰 0） | 737,578 | **16.9%** |
| 四張反查索引（`taskItems` 752／`graphItems` 126／`graphNodeItems` 61／`explicitGraphItems` 15） | 115,557 | 2.6% |
| `projects`（34 個） | 2,962 | 0.07% |

卡片大小：平均 4,483 B、中位數 3,425 B、**最大 115,594 B**（CLA-296）。
大的原因是項目內嵌無上限子陣列：該卡 `links` 93、`spans` 63、`evidence` 56。

### 2.2 寫入頻率

- 最早的卡建立於 **2026-09-08**；量測時 `revision = 6366` → **十一天 6,366 次寫入，約 580 次／天**。
- 每次走 `persist()`：整份 `JSONEncoder(.sortedKeys)` → 32 MiB 上限檢查 → `.atomic` 寫 → `chmod 0600` → `fsync`。

**但重寫本身不是瓶頸，而且這一點他們量過。** commit `b6f4f555`（2026-09-17 00:19）：

> One persist is 42 ms of the 1,230: 38.9 to encode 4.03 MB with sortedKeys, 2.9 to write it
> atomically, 0.7 more to fsync. Ninety-three percent is the encode.

而且他們據此**明確決定不拆**：

> So moving receipts (685 KB) and evidence (942 KB) into segment files saves about 16 ms — and it
> needs a journal… **Not worth it, and the reason is a number rather than a preference.**

我原本準備把「整份重寫」列為重新設計。**這個結論站不住**，因為它是用量測反駁的，不是用偏好。見 C1。

### 2.3 一半的持久寫入是機器在對帳

12,872 筆歷史紀錄的 `kind` 分布（前十四名）：

| kind | 筆數 | 誰做的 |
|---|---|---|
| `automatic_state_reconciled` | **4,114** | 機器重算生命週期投影 |
| `task_reattributed` | **1,566** | 機器重掛 task |
| `catalog_reconciled` | **1,080** | 機器重新分類 audience／role |
| `task_linked` | 759 | broker |
| `span_started` | 734 | session |
| `item_created` | 641 | 人或 agent |
| `obligation_added` | 631 | agent |
| `verification_summary_linked` | 581 | broker |
| `checklist_updated` | 580 | agent |
| `session_relation_confirmed` | 436 | 機器 |
| `session_delivery_recorded` | 385 | session |
| `link_added` | 330 | agent |
| `span_ended` | 308 | session |
| `trusted_evidence_recorded` | 194 | root |

前三名合計 **6,760 筆＝52.5%**。**這一項在舊 repo 的任何文件、commit 或 log 裡都沒有被點名過**——
它是這份文件新發現的。見 B7。

### 2.4 寫入耗時：修正之後還剩什麼

舊版在 2026-09-17 00:19（`b6f4f555`）補上了自我量測，02:11（`85cf6003`）修掉最大的一項：

> **Hold the catalog once: a Board write goes from 1.23 s to 0.20 s** —
> 33 個專案 envelope 各帶一份 35 KB 的 catalog＝每次指令複製 1.17 MB 的字典。

從 log 抽出全部 499 行 `board: materialize`（**這行只在超過 200 ms 時才寫**，所以下面是慢尾巴的統計，不是平均值）：

| 期間 | 被記錄的次數 | 平均 | 破 1 秒 | 最慢 |
|---|---|---|---|---|
| 修正前（09-17 00:16–02:11） | 20 | 448 ms | — | — |
| **修正後（09-17 02:11 之後）** | **479** | 503 ms | **45** | **3,834 ms** |
| 其中今天（09-18） | 170 | 501 ms | 16 | 3,834 ms |

**那個 commit 拿掉的是其中一個成因，不是全部。** 修正後三十二小時內仍有 479 次物化超過 200 ms、
45 次破一秒。他們自己也知道還剩什麼，`85cf6003` 的 body：

> the build reads `state` under the store's lock, so a lazy closure needs either a snapshot of that
> state or a lock discipline, and **a deadlock in the read path is a worse outcome than 112 ms**.

剩下的 112 ms 是：**每次寫入替全部 711 個項目算 `detail`，而讀者一次只會打開一張。**
他們沒修的理由是 Swift actor 的鎖紀律風險。見 D2——**這個理由在 Go 上不成立。**

### 2.5 讀取：熱路徑其實很快

同一分頁、同一秒，連打兩次：

| 請求 | 回應大小 | 時間 |
|---|---|---|
| `GET /v1/board` | 37 KB | 69 ms → 21 ms |
| `GET /v1/board?project=…clawdline` | **1,048 KB** | 42 ms → 42 ms |
| `GET /v1/board?project=…clawdline-go` | 119 KB | 24 ms → 22 ms |
| `GET /v1/board?project=…clawdline&audience=human&limit=30` | 102 KB | 22 ms → 23 ms |
| `GET /v1/sessions`（對照組） | 25 KB | 40 ms → 33 ms |

`ProjectBoardReadCache` 是有效的。問題不是「讀很慢」，是**「讀很大」與「寫的尾巴很長」**。

一個專案完整讀取 1,048 KB／497 張卡，`truncated: false`；前端第一頁只畫 30 列
（`view/board.js` 的 `FIRST_PAGE = 30`）。**送 1 MB 畫 30 列。**
舊版已經做了 `audience=human&limit=30` 這條路（102 KB），但 `docs/acceptance.md:228-233` 記著：

> **No reading has been taken through `app.clawdline.com`.** … `boardItems` does not appear in the
> bundle it is serving today, so the Board there is still taking the old path.

也就是**手機上實際走的仍是 1 MB 那條**。

### 2.6 Cloud／手機路徑上，看板是會排隊的那一個

log 裡 1,250 筆 `kind=board queue_ms=`：

```
平均 2,634 ms    p90 2,164 ms    最慢 229,110 ms（3 分 49 秒）
```

這正是使用者全域規則裡寫過的形狀：「慢送出、長時間 loading」。
在這裡它有具體來源——看板讀取與看板寫入共用序列化路徑，而寫入的尾巴長到 3.8 秒。

### 2.7 併發與鎖

- 兩條**獨立的**有界佇列：`commandDepth = 8`（`.utility`）、`readDepth = 4`（`.userInitiated`），
  兩條都不佔用 message／SSE 的 admission（`ProjectBoardRequestCoordinator.swift:3-9`）。
- `docs/architecture.md:252-256` **明確肯定這個設計**：「That design is sound and should be kept.」
- 跨行程沒有檔案鎖，靠「只有舊 app 會寫」這個前提；寫入是 `.atomic`，所以不會讀到半截。
- 冪等：`(actor, requestId)` 收據＋`expectedRevision` 的 compare-and-swap；收據在同一份文件裡，
  **就是為了和它確認的狀態變更一起原子落盤**（`b6f4f555`）。
- 一條寫下來的跨 owner 規則：`ProjectBoardIntegration.swift:228-229`
  「never call back into Orchestrator while holding the board store's lock」。

### 2.8 卡片契約，以及一次刻意的反轉

`Sources/BoardCardContract.swift` 是允許清單，理由寫在檔頭：`progress` 整包複製上卡片，
62,801 B／次沒有讀者，而且**是靠一次量測才被發現的**。

更值得帶走的是 commit `80173fb1`（2026-09-16）——它**刻意反轉了一條既有契約**
（原本要求未宣告的 `progress` 欄位必須通過清單投影，為了向前相容）：

> The forward compatibility is worth less than it looks: for a field to matter the Mac must send it
> and the page must read it, and both live in this repository and change together — while copying
> progress whole was measured at 62,801 bytes a read in fields with no reader anywhere, accumulating
> silently for as long as nobody measured a payload. **The trade is a loud failure for a silent one.**

**「用一個吵的失敗換一個安靜的失敗」是這份考古裡最該照搬的一句話。**

### 2.9 entitlement：免費版與 Cloud 版根本沒有分

任務書問「免費版與 Cloud 版的差別（entitlement）照舊版怎麼判斷」。答案是：**舊版不判斷。**

```swift
// Sources/ProjectBoardStore.swift:4432
private static let entitlement: [String: Any] = [
    "state": "free_preview", "label": "Currently free",
]
```

同一個字面值出現三次（`ProjectBoardStore.swift:4432`、`ProjectBoardIntegration.swift:617`、
`net/board-mock.js:86`），沒有一處讀 `CloudAccount`、訂閱或 `Config`；
**前端也沒有任何一行讀 `board.entitlement`**。而且它是寫進產品文件的：

> `docs/project-board.md:14-16` — It is **enabled by default and currently free**. The entitlement
> returned by the API is `free_preview`; **this is not a subscription check** or a promise about
> future pricing.

付費區分活在 `/v1/entitlements`（方案頁），從來沒進過看板 envelope。
**正確的復刻是照抄常數，不是發明分支。** 見 B3。

### 2.10 一個路由，五個問題

`Sources/BoardResources.swift:10-17` 自己講：

> `GET /v1/board?project=<id>` is five different responses behind one query string — catalog,
> project, item, report, collection — chosen inside one handler. Only one of the five is narrow…
> **61% of "one item" was a sibling collection.**

分辨方式是把選擇器塞進 `item` 參數的字串前綴：`session:`、`collection:<item>:<kind>:<offset>`、
`catalog:<rev>:<offset>:<query>`、`report:<item>:<report>`。

後來補的 `/v1/board/projects`、`/v1/board/projects/{p}/items`、`/v1/board/items/{id}`、
`/v1/board/items/{id}/history` 是**同一個問題的第二個答案**，而舊路由留著沒拆。
補的理由是位元組預算會說謊：

> `docs/read-path-architecture.md:80-83` — A byte budget is what a system reaches for when it has no
> page size. **It is not a bound on the answer, it is a bound on the *truthfulness* of the answer,
> and it fails silently and unevenly**: the two largest items in the store are 181,148 and 172,540
> bytes, so which 58 items vanish depends on who wrote a long delivery summary that week.

### 2.11 與 orchestrator、broker、時間軸的耦合

> 同一時間有另一個 child 正在寫 `docs/broker-design.md`。以下只指出耦合面，**沒有改動任何相關檔案**。

| 方向 | 內容 | 性質 |
|---|---|---|
| broker → 看板 | `Orchestrator.swift:2926/3097/4000/6429/8082` 呼叫 `ProjectBoardIntegration.observe(...)`；broker 把 task 事實推進看板 | 推送 |
| broker → 看板（同步） | `OrchestratorSessionLanding.swift:328/333/343` 的 `observeRootLanding(...)` **回傳值直接進 landing 回應的 body** | **最緊的一處耦合** |
| 看板 → broker | 讀 `Orchestrator.SessionDelivery`、`boardRootLandingSnapshot()`、`ledgerBackfillRecords()`、`closeabilityInventoryMaxAge` | 唯讀，記憶體 API |
| 看板 → `orchestrator.json` | **沒有。** `grep "orchestrator\.json" Sources/` 在看板檔案裡零命中 | 無 |
| 看板 → SessionWatch | `responsibility` 每次讀取時貼上；`SessionWatch` 對看板零引用 | 唯讀，單向 |
| 時間軸 → 看板 | 時間軸借用看板的 `projectID(path)`、`projectPresentations`、以及**整個 `ProjectBoardHTTP.viewer(...)` 授權判斷** | 時間軸依賴看板，不是反過來 |
| 看板 → 時間軸 | **沒有。** 看板原始碼對 `ProjectTimeline` 零引用 | 無 |
| 看板 → session 關閉 | `RemoteServer.swift:1886/2236/4838` 用 `sessionAccountability(...)` 決定 session 能不能關 | 看板狀態會擋關閉 |

**好消息：看板不碰 `orchestrator.json`，它吃的是 broker 投影進來的事實。這個方向是對的，照搬。**

需要和 broker 那份文件一起決定的只有一件：**task → 看板項目的綁定鍵**
（`inferredSourceKey: "project-task-v1:<sha256>"`、752 筆 `taskItems` 反查表、歷史裡 1,566 筆
`task_reattributed` 代表這個綁定改過很多次）。標為「跨文件待決」，本文件不單方面定案。

---

## 3. 舊紀錄裡被點名過的事

### 3.1 已經被量測點名、並且已修

| 事故 | 證據 | 怎麼修的 | 對新版的意義 |
|---|---|---|---|
| 卡片帶 62,801 B／次沒人讀的欄位 | `BoardCardContract.swift` 檔頭、`read-path-architecture.md:51-68` | 允許清單，並反轉向前相容契約（`80173fb1`） | **照搬清單與那條反轉的理由** |
| 62／481 張卡任何請求都拿不到 | `a734ed8c`、`BoardResources.swift:21-28` | 補分頁資源；單一路由保持不變 | 修補有效，**兩套 API 並存是要收的債** |
| 歷史佔每次寫入 36.5% | `f48772db`、`ProjectBoardHistoryLog.swift:7-12` | 移到 `project-board-history/*.jsonl`，項目內只留 5 筆 | **照搬**（含「先寫 log 再寫聚合」的順序） |
| per-item 歷史筆數算錯（20 其實是 ×708＝1.6 MB） | `6e043845` 自承 | 20 → 5，再省 1.2 MB | 照搬結論 |
| 一次寫入重建 33 個專案 envelope、複製 1.17 MB catalog | `85cf6003` | catalog 只持有一份 | 照搬 |
| 第一畫面 1,051,337 B | `c5ba0f8c` | 降到 106,510 B（9.9×） | 照搬 |
| truncation 的歸因會說謊 | `ProjectBoardHTTP.swift:161-173` | audience 切片時重算 | 照搬這條誠實規則 |
| 五個地方各自拼 `path == "/v1/board"`，加路由時漏掉一個 | `c7165e00` | 集中成 `isBoardRoute` | 照搬（Go 的 mux 本來就集中） |

### 3.2 被點名、**還沒修**

| 問題 | 證據 | 他們為什麼沒修 |
|---|---|---|
| 每次寫入替全部項目算 detail（剩下的 112 ms） | `85cf6003` body | 怕 read path 死鎖 |
| Cloud 看板讀取排隊到 229 秒 | log 1,250 筆 | 沒有紀錄提過 |
| 手機上走的仍是 1 MB 舊路徑 | `acceptance.md:228-233` | 前端 bundle 沒換 |
| 收據佔 17% 且會 FIFO 淘汰，但契約文件寫「never evicts」 | `ProjectBoardStore.swift:6660-6666` vs `project-board-contract.md:12` | 文件句子比程式寬 |
| 單張卡可以長到 115 KB | 本文件量的 | 沒有紀錄提過 |

### 3.3 本文件新發現、舊紀錄沒提過的

1. **52.5% 的持久寫入是機器對帳**（§2.3）。
2. **修正後仍有 479 次物化超過 200 ms、45 次破 1 秒**（§2.4）——舊版量的是「一次寫入 1.23 s → 0.20 s」，沒有量分布。
3. **看板的多輪難題從未寫進 `docs/hard-problems.md`**：那份檔案只有一則 2026-09-06/07 的通知調查，看板十天裡至少有四個夠格的候選（位元組預算、卡片大小估錯 3.5 倍、20 vs 5 的算術、五個驗收失敗裡三個是量測工具自己錯）。
4. **2026-09-08 到 09-15 的建置期，12 個 `fix(board):` commit 全部沒有 body**——缺陷名稱在 subject 裡，原因沒有任何地方寫。

---

## 4. 逐項判斷

分類：**照搬**＝規則與形狀原樣移植。**小改**＝保留意圖，換掉實作細節，不改對外契約。
**重新設計**＝做法本身是問題來源；**本輪不實作，等使用者看過**。

### A. 對外契約（wire）

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| A1 | `board` 的 19 個欄位 | **照搬** | console 是同一份；`schemaVersion !== 1` 直接丟 `board_response_incomplete` | 無 |
| A2 | `BoardCardContract` 允許清單＋`80173fb1` 的反轉 | **照搬** | §2.8，量出來的結論 | 無 |
| A3 | `mode` ＝ `enabled ? "board" : "standard"` | **照搬** | 純函數，兩個值 | 無 |
| A4 | `viewer` 的 `canWrite`／`canManage` | **照搬** | 與新版逐路由閘門一致 | 無 |
| A5 | typed refusal，含「指令存了但投影太大」要帶 `commandApplied: true` | **照搬** | `project-board-contract.md:456`：投影失敗不可以假裝成成功的關閉快照 | 無 |
| A6 | `truncated`／`truncation` 必須描述**這一份**清單 | **照搬** | §3.1 | 無 |
| A7 | 一個路由五種問題（`item=collection:…` 前綴選擇器） | **重新設計** | §2.10，他們自己補了第二套 | 中。前端會打舊拼法，新版**兩套都要答**直到前端換掉 |
| A8 | 一個專案回 1,048 KB 畫 30 列 | **重新設計** | §2.5、§2.6。而且手機上走的就是這條 | 中。舊版已有 `audience/cursor/limit` 可直接當預設 |

### B. 產品規則

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| B1 | `progress` 狀態機（約 250 行決策鏈） | **照搬** | 編碼的是「什麼算交付、什麼算落地」；`delivered ≠ reviewed ≠ landed` 是 plan.md 第 6 條不可動的事 | 低。逐條移植，可用舊版回應當 oracle 逐卡比對 |
| B2 | `listSummary.group` 顯示分組（含 waiting＋planning 的修正） | **照搬** | 同上 | 低 |
| B3 | `entitlement` 常數 | **照搬** | §2.9 | 無。**不要發明分支** |
| B4 | `responsibility` 是請求時資料、永不進持久 store | **照搬** | `ProjectBoardIntegration.swift:325-326`：不完整的盤點永遠不能變成 `not_live` 證據 | 無 |
| B5 | 狀態由**證據**推導、不存成意見；沒有手動狀態選單 | **照搬** | `project-board.md:30-34` | 無 |
| B6 | `narrativeConsent`（provider-scoped、預設關、撤銷推進 epoch） | **照搬**（欄位與開關）；**AI 外送本身延後** | 設定頁第二個開關要它；送標題給 AI 是產品決定 | 低 |
| B7 | `automatic_state_reconciled` 寫進持久歷史 | **小改** | §2.3，4,114 筆／32%，重算投影沒產生新事實 | 低。只在推導改變了**被記錄的**事實時才寫歷史 |
| B8 | 收據會 FIFO 淘汰，但契約文件寫「never evicts」 | **小改** | §3.2 | 無。把句子改成「報表版本不淘汰、收據會，且 `evictedReceipts` 會說」 |

### C. 儲存

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| C1 | 每次寫入重寫整份文件 | **照搬**，但**加一道成長守衛** | §2.2：42 ms／次，93% 是 encode；拆開只省 16 ms 且需要 journal。**他們的數字是對的，不推翻。** 但那是 4 MB 的 42 ms——32 MiB 上限在目前速度（十一天 781 張卡）下約四到五個月會碰到，而 encode 時間是線性的 | 低。Go 版沿用單一文件＋原子寫；**另外加一條：當文件超過 8 MB 時在 `/v1/health` 講一次**，不要等撞牆 |
| C2 | 卡片內嵌無上限子陣列（`links`／`spans`／`evidence`） | **小改** | §2.1，單卡 115 KB。舊版已對 `history` 做過視窗（5 筆） | 低。對 `spans` 比照 `history` 做視窗＋`droppedCount`；`evidence` 不能動（會就地更新，`ProjectBoardHistoryLog.swift:16-23` 講過為什麼） |
| C3 | 收據放在同一份文件 | **照搬** | §2.7：就是為了和狀態變更一起原子落盤。拆開會讓冪等保證在某個方向說謊 | 無 |
| C4 | 每項目 append-only `*.jsonl`，log 先寫、聚合後寫 | **照搬** | `ProjectBoardHistoryLog.swift:25-32`：多一行可以救，少一行救不回；不對稱才是順序的理由 | 無 |
| C5 | 項目 id 當檔名，非 UUID 一律拒絕而不是消毒 | **照搬** | 同上 `:71-76`：消毒過的檔名會安靜地把兩個項目併成一個 log | 無 |
| C6 | log 寫失敗不讓指令失敗，但計入 `historyDroppedCount` | **照搬** | 同上 `:5693-5696` | 無 |
| C7 | 四張反查索引存在文件裡 | **小改** | 115 KB／2.6%，而且是可重建的衍生資料 | 低。索引是索引，不是狀態 |
| C8 | 資料在 `Application Support`，時間軸卻在 `~/.config` | **小改** | §2.1 | 低。新版統一在 `CLAWDLINE_NEXT_DIR` |
| C9 | 舊資料（776 張卡）的讀取 | **照搬 plan.md §4** | 唯讀、不 rename、不建 lock、讀不到是「未知」不是「空」 | 無 |

### D. 讀取與併發

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| D1 | 物化讀取模型（寫時備好、讀時便宜） | **照搬** | §2.5，21–42 ms 是它換來的；`architecture.md:277-285` 把它和 `SessionWatch` 並列為「要抄的兩個例子」 | 無 |
| D2 | 物化時**連 detail 一起全量算** | **小改** | §2.4。他們沒修的理由是 Swift actor 的死鎖風險——**Go 版讀的是不可變快照，沒有那個風險** | 低。這是 Go 版少數「同樣的修法在這裡更便宜」的地方 |
| D3 | 指令回應前同步重建（不可以把新 revision 配上舊 bytes） | **照搬**（承諾），**小改**（做法） | `b6f4f555`：「is right to — but rebuilding everything is the wrong way to keep that promise」 | 低。只重建被改的那一項＋它的專案 |
| D4 | `expectedRevision` CAS ＋ `(actor, requestId)` 收據 | **照搬** | 新版 `internal/domain/board.Decide` 已是這個形狀 | 無 |
| D5 | 兩條獨立有界佇列（讀 4／寫 8），不佔 SSE admission | **照搬** | `architecture.md:252-256` 明說 sound and should be kept | 無 |
| D6 | Cloud 看板讀取排隊 229 秒 | **重新設計**（＝A8 的後果） | §2.6 | 中。分頁化之後這條自然縮短；**不要只加 timeout** |
| D7 | 跨 owner 規則：持有看板鎖時不得回呼 broker | **照搬** | `ProjectBoardIntegration.swift:228-229` | 無 |

### E. 前端

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| E1 | `view/board.js`（2,199 行）與 `board.css` | **照搬**（逐位元組） | 復刻專案既定做法；且看板頁**一個 i18n key 都沒用**（`zh-Hant.json` 778 鍵裡零個含 board），全是內嵌雙語字面值 | 無 |
| E2 | 抽屜的 `#nav-board`「Projects · Board」 | **照搬＝保持隱藏** | 舊版 `BoardControls.apply()` **無條件** `nav-board.hidden = true`；真正入口是專案頁與 `#page=board&machine=…&project=…` | 無。**但這可能不是使用者要的**，見 §6 |
| E3 | 設定頁看板區塊（`#settings-board`） | **照搬** | 只需要 `board.enabled`／`revision`／`viewer.canManage`／`narrativeConsent`，全是 board 級事實 | 無 |
| E4 | 前端輪詢：loading 時 2 s、否則 15 s，隱藏分頁不輪詢 | **照搬**（規則），**小改**（間隔留待 A8 之後再評估） | 15 s × 1 MB 是 §2.6 的來源之一，但分頁化後就便宜了 | 低 |

---

## 5. 建議順序

1. **全部「照搬」項** — 先讓新版有一個形狀正確、讀得到 776 張卡的看板。**本輪只做這些。**
2. **B7、B8、C2、C7、D2、D3** — 六個低風險「小改」，不改對外契約，可同一波做。
   其中 **D2＋D3 最值得先做**：它是舊版明確想做而受限於 Swift 的那一項。
3. **A7＋A8＋D6** — 路由與分頁重新設計。要動 `board.js`，排在復刻穩定之後。
4. **C1 的成長守衛** — 單獨一件小事，但要在 32 MiB 之前發生。
5. **B6（AI 摘要外送）** — 產品決定，等使用者。
6. **跨文件待決：task → 項目的綁定鍵** — 與 `docs/broker-design.md` 一起決定。

---

## 6. 需要使用者拍板的

> 使用者在睡覺，以下都選了安全的預設繼續做，但都可以推翻。

| # | 問題 | 我選的安全預設 | 為什麼 |
|---|---|---|---|
| 1 | 抽屜那一列「專案 · 看板」要不要真的出現？ | **照舊版隱藏**，看板從專案頁與 `#page=board` 進 | 任務書寫「抽屜裡的『專案 · 看板』」，但舊版實際行為是永遠隱藏。復刻的定義是行為相同；讓它出現是**改設計**，不是復刻 |
| 2 | 新版自己建立的看板項目要存哪？ | **本輪不實作項目寫入**，只做 board 級的 `set_enabled`／`set_ai_consent` | C1／C2 還是待決；先寫死就是把待商榷的設計鎖進程式 |
| 3 | 舊版 776 張卡在新版是唯讀還是可編輯？ | **唯讀** | 兩個 app 都在跑；新版沒有權利寫舊 app 的檔（plan.md §4） |
| 4 | 要不要現在把 `entitlement` 接上 Cloud 訂閱？ | **不要**，照搬常數 | §2.9。舊版沒有這個區分，發明一個是憑空的產品決定 |
| 5 | 32 MiB 的成長守衛要不要現在做？ | **要**，但只做「說一聲」，不做自動清掃 | 自動清掃會掃掉還被引用的東西，那正是時間軸踩到的坑（見 `timeline-design.md`） |

---

## 7. 這份文件沒有量到的

誠實列出，**不算通過**：

- **多機／多使用者**：所有數字來自一台機器、一個使用者、十一天。
- **`app.clawdline.com` 上的實際讀取**：舊版自己也沒量過（`acceptance.md:228-233`），本文件同樣沒有。
- **32 MiB 上限的真實到達時間**：線性外推，不是量的。
- **小改／重新設計後的成本**：D2、A8 的收益是推論，沒有做過原型量測。
- **`ProjectBoardWorkflow`（116,786 B Swift、730 KB 資料）**：只量了大小與列數，沒讀規則。它是看板的鄰居，不是看板本身。
- **`ProjectBoardNarrative`**：舊 repo 裡沒有任何關於它的事故或量測紀錄，本文件也沒量。
- **錯誤與降級路徑**：`board_unavailable`、`board_response_too_large`、`board_snapshot_too_large`
  在 log 裡出現 **0 次**——那些路徑從未在這台機器上發生過，也就從未被觀察過。
- **`docs/project-board-sync-and-session-accountability.md` 描述的 session accountability 是未驗證的程式**：
  commit `3a4ebb60` 自承「as delivered, not as reviewed… it has not been built or run here」。
  本文件沒有把它當成已生效的行為。
