> **實作依據是 [`docs/design-decisions.md`](design-decisions.md)（2026-09-18 起）；本文保留為分析材料，與它衝突之處以它為準。**

# 時間軸（Project Timeline）：照搬什麼、改什麼、重做什麼

> 問的是：舊版時間軸的設計值不值得原封不動搬進 clawdline-go。
> 判準與 `docs/board-design.md` 相同：**重寫不是翻譯。** 被點名過的缺陷不照搬，也不為了新而新。
>
> 量測：2026-09-18 09:00–10:15，對**正在跑的舊 app**（7717）與它此刻的資料檔。
> **本輪只出文件，不實作。**

---

## 0. 先講最重要的一件事：它已經滿了，而且安靜地停了

```
-rw-r--r--@ 1 sainteye staff 2495417  9 17 02:06  ~/.config/clawdline/project-timeline.json
```

| 量 | 值 | 上限 | 使用率 |
|---|---:|---:|---:|
| `entries` | **2,000** | 2,000 | **100%** |
| `events` | 2,000 | 20,000 | 10% |
| `eventReceipts` | 2,000 | 4,096 | 49% |
| `requestReceipts` | 0 | 4,096 | 0% |
| `checkpoints` | 12 | 128 | 9% |
| 位元組 | 2,495,417 | 8,388,608 | 29.8% |

`entries` 滿了。而上限的實作是**拒絕寫入，不是淘汰舊的**：

```swift
// Sources/ProjectTimelineStore.swift:293-298
let addsEntry = !state.entries.contains { $0.id == entry.id }
guard state.entries.count + (addsEntry ? 1 : 0) <= Self.maximumEntries,
      state.events.count + newEvents.count <= Self.maximumEvents else {
    return MutationOutcome(status: "capacity", accepted: 0, dropped: events.count,
                           persisted: true, reason: "timeline_capacity_reached")
}
```

`grep -i "retention|evict|prune|compact|trim"` 在全部七個時間軸檔案裡，**只命中字串處理用的 `trimmingCharacters`**。
沒有淘汰、沒有保留視窗、沒有壓實。

結果，量到的：

- 12 個專案 checkpoint 裡 **10 個是 `historyStatus: "capacity"`**，`timeline_capacity_reached` 出現 11 次。
- 而 `ProjectTimelineIntegration.swift:58` 看到 `"capacity"` 就**整個專案跳過不再嘗試**：
  ```swift
  if store.historyCheckpoint(projectID: project)?.historyStatus != "capacity" {
      importPage(path, store: store)
  ```
- 最後一次成功寫入是 **2026-09-17 02:06:58**（`updatedAt` 與 mtime 一致）。
  同目錄的 `orchestrator.json` 是 2026-09-18 08:15。**時間軸的 store 凍了一天以上。**
- 最新的 event 停在 **2026-09-10**，而這段期間 broker 一直有 landing。

**舊 repo 的任何文件、CHANGELOG 或 commit 訊息都沒有記錄「它後來真的滿了並停止」。**
`CHANGELOG.md` 裡 "timeline" 出現 **0 次**。這是本文件新發現的。

---

## 1. 三十秒版本

| | 現況 | 判斷 |
|---|---|---|
| 「commit 不等於上線」這個產品立場 | 整個功能存在的理由 | **照搬** |
| 12 種狀態詞彙與證據導向的卡片 | 好的 | **照搬** |
| 事件的冪等鍵（`producer`＋`sourceID`＋digest） | 修過一次決定性 bug，現在是對的 | **照搬** |
| 「entry 先寫、checkpoint 後寫」的順序 | 有測試守著，理由寫得清楚 | **照搬** |
| 上限＝拒絕而非淘汰 | 已經讓功能安靜停擺一天以上 | **重新設計** |
| 整份 2.4 MB 文件每一個 commit 重寫一次 | 匯入 2,000 個 commit 就是 2,000 次全檔重寫 | **重新設計** |
| offset 分頁 | 背景刷新會位移，前端只能去重、救不回被跳過的 | **小改** |
| 預設過濾把**全部** 2,000 筆都濾掉 | 每個專案的預設頁都是空的 | **重新設計**（是資料問題不是 UI 問題） |
| 檔案權限 `0644` | 同目錄其他檔都是 `0600` | **小改**（安全缺陷） |
| 存在 `~/.config/clawdline`，看板卻在 `Application Support` | 不一致 | **小改** |

---

## 2. 量到的現況

### 2.1 規模

Swift **7 檔、1,456 行**：

| 檔 | 行 |
|---|---:|
| `ProjectTimelineStore.swift` | 486 |
| `ProjectTimelineModel.swift` | 258 |
| `ProjectTimelineReadCache.swift` | 203 |
| `ProjectTimelineIntegration.swift` | 182 |
| `ProjectTimelineGitImporter.swift` | 155 |
| `ProjectTimelineHTTP.swift` | 121 |
| `ProjectTimelineRequestCoordinator.swift` | 51 |

前端：`view/timeline.js` **296 行**、`css/timeline.css` **17 行**（全 app 最小的 CSS，對照 `board.css` 228）、
`index.html:597-614` 共 18 行標記。

**對照組：看板是 13,233 行 Swift＋2,199 行 JS。時間軸是它的九分之一。**

### 2.2 儲存格式

路徑（`ProjectTimelineStore.swift:95-99`）：`~/.config/clawdline/project-timeline.json`。
**`~/Library/Application Support/Clawdline/` 底下沒有任何時間軸資料**——那裡是看板的地盤。

單一文件，每次變更整份重編碼重寫（`:388-402`）：

```swift
let data = try JSONEncoder().encode(draft)
guard data.count <= Self.maximumStoreBytes else { ... }
try data.write(to: url, options: .atomic)
```

top-level（`StoredState`，`:37-48`）：
`schemaVersion`、`revision`、`enabled`、`updatedAt`、`entries[]`、`events[]`、
`requestReceipts[]`、`eventReceipts[]`、`checkpoints[]`、`coverageIssues[]`。

實測各欄位重量：

| 欄位 | 位元組 | 列數 |
|---|---:|---:|
| `entries` | 1,221,578 | 2,000 |
| `events` | 795,125 | 2,000 |
| `eventReceipts` | 466,550 | 2,000 |
| `checkpoints` | 4,948 | 13 |
| `coverageIssues` | 57 | 2 |

`schemaVersion = 1`，而且**沒有 migration**：版本不符時整份丟棄並回 503（`:81-87`）。

### 2.3 2.4 MB 是怎麼長出來的

event 的 `kind` 分布：

| kind | 筆數 | 佔比 |
|---|---:|---:|
| `git_history_observed` | **1,973** | 98.7% |
| `landed_to_git` | 27 | 1.4% |
| deploy／availability／rollback／operation | **0** | 0% |

也就是說：**2.4 MB 幾乎全部是 git commit 的匯入**，不是部署證據。

匯入規則（`ProjectTimelineGitImporter.swift:6-8`）：最多 8 個 repo、每 repo 每頁 40 個 commit、8 秒逾時。
背景回填每 **60 秒**輪一個 repo（`Integration.swift:50-65`）。

event 的時間範圍是 **2025-11-04 → 2026-09-10**——十個月的 git 歷史。
每天筆數從個位數到 2026-09-05 的 185 筆。它是往回挖歷史，不是記錄當下。

### 2.4 寫入頻率與觸發點

五個，窮舉：

| # | 觸發 | 寫入量 |
|---|---|---|
| 1 | app 啟動時 `prepare()` → 每個 Start Point 匯入 ≤40 commit | **每個 commit 一次全檔重寫**（`GitImporter:99-100`），再加每 repo 一次 checkpoint 寫入 |
| 2 | 背景回填，每 60 秒一個 repo | 同上 |
| 3 | broker landing 驗證通過（`Orchestrator.swift:2927` → `observeBrokerRecord`） | 一次 |
| 4 | `POST /v1/timeline {operation:"set_enabled"}`（設定頁開關） | 一次，**即使值沒變也會寫**（`Store:167-172` 無條件動 `updatedAt` 並附收據） |
| 5 | `POST /v1/timeline {operation:"ingest"}` | **全 repo 沒有任何呼叫者** |

**觸發 5 是死的。** `grep -rn '"ingest"' Sources/ Resources/ tools/ Tests/` 只命中 handler 與一個測試。
`deploy_succeeded`／`availability_verified` 這些 kind 只存在於驗證器與 reconciler 裡，沒有生產者。
舊版自己記著為什麼：

> `docs/project-timeline.md:71-73` — Deployment and availability evidence must be supplied by a
> trusted local producer. **Automatic GitStory integration is deferred** until its API, owner,
> authorization and retention contract are defined.

**寫入放大**：匯入約 2,000 個 commit＝約 2,000 次獨立的全檔重編碼＋原子寫，而文件一路長到 2.4 MB。
`ingest` 在 `GitImporter:81-109` 的迴圈裡**逐 commit 呼叫，沒有批次**。

### 2.5 讀取路徑與查詢語法

handler：`ProjectTimelineHTTP.admit`（`:46-100`）。GET 的參數是封閉集合，未知或重複的 key 一律
`400 bad_timeline_query`：

| 參數 | 驗證 | 拒絕碼 |
|---|---|---|
| `project` | ≤200 B，非空，精確比對 | — |
| `entry` | ≤200 B，detail 還要和 `project` 相符 | — |
| `cursor` | `0 ≤ v ≤ 2000` | `invalid_timeline_cursor` |
| `environment` | `production｜staging｜preview｜development｜all`，**預設 `production`** | `invalid_timeline_environment` |
| `category` | `deploy｜server｜architecture｜feature｜operation` | `invalid_timeline_category` |
| `upcoming` | `true｜1｜false｜0`，預設 `false` | `invalid_timeline_upcoming` |

回應單一 key `timeline`，含 `entries[]`、`coverage{status,reasons[]}`、`checkpoints[]`、
`capacity{entryCount,entryLimit,eventCount,eventLimit,byteLimit}`、`selected|null`、`nextCursor|null`、
`filters{...}`、`status`、`viewer`。清單列會被壓縮（拿掉 `events` 與 `projections`），
只有 `selected` 帶完整 event。

**分頁是 offset，不是 keyset**（`ReadCache:137-145`，`pageSize = 40`）。
offset 索引的是每次請求重新推導的清單，背景刷新一發生每個 offset 就位移。
伺服器不處理，**前端用 id 去重擋著**（`timeline.js:256-259`）——
那會藏住重複，但**救不回被跳過的**。

讀取從不碰 git（`ReadCache.swift:3`）。佇列：讀 4／寫 8，超過回 `429` 帶 `Retry-After: 1`。

### 2.6 UI 上它是做什麼的

進入點：**專案 → 看板 → 「Timeline」分頁**，或設定頁的「Open timeline」。
單欄、最大 760 px，手機優先。控制項：Environment 下拉、Category 下拉、
「Include upcoming / Git history」核取方塊。

卡片：分類圖示、**12 種狀態詞彙**的 pill（Available／Limited rollout／Partially available／
Deployed · awaiting availability／In Git／Deployment failed／Rolled back／…／Availability unknown）、
標題、兩行摘要、看板項目 pill、revision 數、時間。

它要回答的是：**「這個東西真的在線上了嗎，憑哪張收據？」**

> `docs/project-timeline.md:5` — A commit, build, successful task, or narrative summary
> **never means a feature is live.**

**這個立場是整個功能的價值，也是最該照搬的東西。**

### 2.7 但使用者今天看到的是空的

全部 2,000 筆 entry 的 `requiredTargets` 都是 `[]`，而且只有 `git_history_observed`(1,973) 或
`landed_to_git`(27) 兩種 event，所以 reconciler 對每一筆都回 `"landed_to_git"`。
而預設過濾正好把這三種濾掉（`ReadCache:133`）：

```swift
if !query.includeUpcoming && ["upcoming", "landed_to_git", "unknown"].contains(status) { return false }
```

**所以每個專案的預設頁都是空的**，畫的是 `timeline.js:185` 的退路文案：
「No deployment or availability records match these filters.」

我自己量的也一致：`GET /v1/timeline` 回 6 KB、`?project=…` 回 2 KB，兩者都 `entries: []`，
`coverage.status: "partial"`、`reasons: ["github_remote_unavailable", "timeline_capacity_reached"]`。
舊版自己的 `docs/acceptance.md:30` 記著 1,561 B／0.017 s，並把它列為「fine」——
**那個 fine 是對著一個空結果量的。**

2,000 筆裡只有 **12 筆（0.6%）**有任何看板關聯。

### 2.8 耦合

| 對象 | 方向 | 讀／寫 | 證據 |
|---|---|---|---|
| 看板：`projectID(path)`、`projectPresentations` | 時間軸 → 看板 | **讀** | `Integration.swift:21, 57, 68, 91`。專案身分是跟看板借的 |
| 看板：`ProjectBoardHTTP.viewer(...)` | 時間軸 → 看板 | **讀** | `HTTP.swift:49-50`。**整個授權判斷都是看板的** |
| 看板項目 | 時間軸 → 看板 | **只存 ID** | `entry.boardItemIDs`，前端畫成可點的 pill |
| `project-board.json` | — | **都沒有** | 不讀也不寫 |
| Orchestrator | orchestrator → 時間軸 | **推送** | `Orchestrator.swift:2927`。時間軸從不開 `orchestrator.json` |
| orchestrator token | 時間軸 → orchestrator | **讀** | `RemoteServer.swift:842, 982, 4450` |
| git | 時間軸 → git | **唯讀子行程** | `GitImporter:123-140`，8 秒逾時、256 KiB 輸出上限 |
| 部署系統 | — | **不存在** | 沒有任何生產者寫 deploy／availability 證據 |

**時間軸是一個純 sink：它讀 git、讀看板的身分與授權，寫一個檔。沒有任何東西依賴它。**
這對重寫是好消息——它可以獨立換掉。

### 2.9 懸空引用的方向

entry 指向看板項目（`boardItemIDs`），看板**從不**指向 entry。
entry 不可變也永不清掃，所以**一個被刪掉的看板項目會留下一顆點了沒反應的 pill**。
原始碼對此**沒有任何處理**：`timeline.js:156` 無條件呼叫 `environment.openBoard(...)`。

唯一的刪除是 checkpoint 的取代（`Store:219-222`），而它帶一個 `|| $0.projectID == nil` 子句：
**一個專案的掃描會刪掉該 repo 底下所有 legacy 的 nil-project checkpoint，包含屬於別的專案的**。
影響範圍有界（128 筆），但那是真的跨專案刪除。

### 2.10 權限缺陷

`:397` 的寫入沒帶 `posixPermissions`，實際落盤是 `-rw-r--r--`（**全域可讀**）。
同目錄的 `orchestrator.json`、`config.json`、`landing-queue.json` 全都是 `-rw-------`，
其他 store 也都明確 chmod 0600（`Coordinator.swift:902`、`CloudStatus.swift:506`、
`CloudDurableStores.swift:196`）。**時間軸是唯一的例外。**

---

## 3. 舊紀錄裡被點名過的事

**歷史很短**：9 個 commit 動過時間軸檔案，全部在 **2026-09-10 到 2026-09-13**，之後沒有了。
`AGENTS.md`、`CLAUDE.md`、`CONTEXT.md`、`CONTRIBUTING.md`、`DECISIONS.md` 都沒有提到時間軸。

### 3.1 修過的缺陷

| commit | 日期 | 內容 |
|---|---|---|
| `584783ca` | 09-10 | **冪等鍵的決定性 bug**。`observedAt` 用牆鐘，導致同一個 commit 每次重掃都產出不同 digest，同一組 `(producer, sourceID)` 會撞 `timeline_event_conflict` 而被拒。改成用 `commit.committedAt` |
| `82bc767d` | 09-10 | 兩個缺陷：(a) 批次內重複身分沒檢查，**一個 POST 可以用同一組 `(producer, sourceID)` 塞兩份不同內容且兩份都會落盤**；(b) `operation_applied` **完全沒有驗證**，`operation_verified` 只檢查 `verification != nil` |
| `d6911bd1` | 09-13 | Cloud 重連時 `cloud_reconnecting` 沒有被任何舊拼法匹配到，**pending 的 requestId 被丟掉**——重試就變成對著可能已改變的 revision 送一個全新指令 |
| `d4a06d20`／`e560bdd3` | 09-15 | `_onlyMachine` 讓**任何雙機帳號完全打不開時間軸** |

### 3.2 守住了的設計

| 內容 | 證據 |
|---|---|
| entry 先寫、checkpoint 後寫；中斷只會重放同一筆，不會跳過 | `docs/project-timeline.md:80-83`、`GitImporter:101-108`、`Tests/ProjectTimelineTests.swift:482` |
| 每次掃描先解析出完整 SHA 再交給 git，**併發 checkout 無法掉包** | `docs/project-timeline.md:78-79`、`GitImporter:55-66`，測試 `:496-509`「HEAD race cannot mark incomplete A complete」 |
| 讀取永不觸發 git、transcript 或 AI | `ReadCache.swift:3` |

### 3.3 上限＝拒絕，是刻意的，而且有文件

> `docs/project-timeline.md:82-83` — The first rejected entry stops a page;
> **capacity never evicts retained history.**
>
> `:96-97` — A capacity stop remains paused in the background;
> **no automatic deletion or retention expansion is performed.**

測試 `Tests/ProjectTimelineTests.swift:484`：「capacity does not delete older retained entries」。
前端也有對應文案（`timeline.js:137`）：「Git backfill paused at storage capacity；
retained history was not removed.」

**這個決定本身是對的**——自動清掃會掃掉還被引用的東西。
問題是它只做了一半：**拒絕之後沒有任何人被通知，而且回填會永久放棄那個專案。**

### 3.4 本文件新發現、舊紀錄沒提過的

1. **store 真的滿了並停止寫入**（§0）。沒有任何 commit、doc 或 CHANGELOG 記錄這件事。
2. **預設過濾濾掉 100% 的資料**（§2.7）。舊版的驗收把「1,561 B／0.017 s」記成通過，那是對空結果量的。
3. **檔案是 0644**（§2.10）。
4. **`ingest` 操作沒有呼叫者**（§2.4），所以整個部署證據的鏈路從來沒有被走過。

---

## 4. 逐項判斷

### A. 產品立場與 wire

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| A1 | 「commit／build／成功的 task／AI 摘要都不代表上線」 | **照搬** | `project-timeline.md:5`。這是功能存在的理由 | 無 |
| A2 | 12 種狀態詞彙與證據導向卡片 | **照搬** | 詞彙本身區分得很細且誠實 | 無 |
| A3 | 查詢語法（封閉參數集、逐項 typed 拒絕碼） | **照搬** | 已經是新版房規的形狀 | 無 |
| A4 | `capacity{...}` 公開在回應裡 | **照搬** | 容量要看得見 | 無 |
| A5 | offset 分頁 | **小改** | §2.5，背景刷新會位移，前端只能去重 | 低。改 keyset（以 `(effectiveAt, id)` 排序）。**看板的 `read-path-architecture.md:211-212` 也提過缺一個 per-item revision，同一件事** |
| A6 | 預設 `environment=production`、`upcoming=false` | **重新設計** | §2.7，今天濾掉 100% | 低。這是**資料問題**：在沒有部署證據生產者之前，預設不該把全部資料藏起來 |

### B. 儲存與成長

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| B1 | 上限＝拒絕而非淘汰 | **重新設計** | §0。功能已經安靜停擺一天以上 | 中。**不是改成自動刪除**——那正是他們正確避開的。改成：(a) 容量到達時在 `/v1/health` 與 UI 講出來、(b) 回填不永久放棄、(c) 提供明確的「封存舊區間」動作由人決定 |
| B2 | 每個 commit 一次全檔重寫（匯入 2,000 個＝2,000 次） | **重新設計** | §2.4。與看板不同：看板是 42 ms／次、一天 580 次；**時間軸是在一個迴圈裡連續做** | 中。批次：一頁 40 個 commit 一次寫入。這一項單獨就把匯入寫入量降 40 倍 |
| B3 | 三個並行的 2,000／20,000／4,096 上限 | **小改** | `entries` 滿了，`events` 才 10%。上限之間不成比例 | 低。用位元組與時間視窗當主要界線，筆數當次要 |
| B4 | 檔案權限 0644 | **小改**（但是安全缺陷） | §2.10，全目錄唯一例外 | 無。0600 |
| B5 | 存在 `~/.config/clawdline` | **小改** | 與看板不一致 | 低。新版統一 `CLAWDLINE_NEXT_DIR` |
| B6 | `schemaVersion` 不符就整份丟棄回 503 | **小改** | 沒有 migration 路徑，而且從沒寫過一個 | 低。至少保留原檔並具名拒絕，不要讓「丟棄」是唯一結果 |
| B7 | checkpoint 取代時的 `|| projectID == nil` 跨專案刪除 | **小改** | §2.9 | 低。刪除條件要含 `projectID` 相等 |

### C. 冪等與併發

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| C1 | 事件冪等鍵用 `(producer, sourceID)` ＋內容 digest，且 digest 必須是**決定性的** | **照搬** | §3.1 的 `584783ca`，牆鐘進 digest 是真的踩過的坑 | 無 |
| C2 | 批次內重複身分要檢查 | **照搬** | §3.1 的 `82bc767d` | 無 |
| C3 | entry 先寫、checkpoint 後寫 | **照搬** | §3.2，不對稱是順序的理由 | 無 |
| C4 | 掃描先解析完整 SHA | **照搬** | §3.2，有測試守著 | 無 |
| C5 | `expectedRevision` CAS ＋ requestId 重放 | **照搬** | 與看板同一套 | 無 |
| C6 | 重連時保留 pending requestId | **照搬** | §3.1 的 `d6911bd1` | 無 |
| C7 | 讀 4／寫 8 的有界佇列，429＋`Retry-After` | **照搬** | 與看板同一套 | 無 |

### D. 資料來源

| # | 項目 | 判斷 | 理由 | 代價／風險 |
|---|---|---|---|---|
| D1 | git 匯入唯讀、有界（8 repo × 40 commit × 8 s） | **照搬** | 正確的界線 | 無 |
| D2 | 每 60 秒回填一個 repo | **小改** | 與 B2 一起：批次之後這個節奏可以放慢 | 低 |
| D3 | `ingest` 操作沒有生產者 | **重新設計**（或明確刪掉） | §2.4。整條部署證據鏈從未被走過，而它是這個功能的全部價值 | 中。**這是產品決定**：要嘛接一個真的部署來源，要嘛承認時間軸目前只是 git 歷史瀏覽器並照那樣命名 |
| D4 | entry → 看板項目的單向 ID 引用會懸空 | **小改** | §2.9 | 低。讀取時解析不到就把 pill 畫成非互動，不要開一個打不開的東西 |

### E. 前端

| # | 項目 | 判斷 | 理由 |
|---|---|---|---|
| E1 | `view/timeline.js`（296 行）與 `timeline.css`（17 行） | **照搬**（逐位元組） | 與看板同一套復刻做法 |
| E2 | 前端用 id 去重來擋分頁位移 | **小改** | §2.5。A5 修好之後這段可以拿掉；**在那之前不要拿掉** |
| E3 | 容量到達的文案 | **照搬**，但要**加上「還有多少空間」** | §3.3 的文案是對的，只是說得太晚 |

---

## 5. 建議順序

1. **所有「照搬」項**（A1–A4、C1–C7、D1、E1）— 形狀與規則先對。
2. **B4（0600）** — 一行，安全缺陷，先修。
3. **B7、D4** — 兩個小的正確性修正。
4. **B2（批次寫入）** — 單獨就把匯入寫入量降 40 倍，風險低。
5. **B1（容量的處理）＋ E3** — 讓「滿了」變成看得見的事，而不是安靜停擺。
6. **A5＋E2** — keyset 分頁，然後才拿掉前端去重。
7. **A6＋D3** — 需要使用者拍板，見 §6。

**時間軸整體的規模（1,456 行 Swift）比看板小九倍，而且沒有任何東西依賴它**，
所以它是三者裡最適合「先重新設計再實作」的一塊。

---

## 6. 需要使用者拍板的

| # | 問題 | 我的建議 | 為什麼 |
|---|---|---|---|
| 1 | 時間軸要不要接一個真的部署證據來源？ | **要決定，不要繼續擱置** | §2.4：`ingest` 沒有呼叫者、0 筆部署事件。沒有它，時間軸就只是一個 git 歷史瀏覽器，而它的預設過濾又正好把 git 歷史藏起來（§2.7），等於一個空畫面 |
| 2 | 在沒有部署來源之前，預設過濾要不要改？ | **要**。預設不該濾掉 100% 的資料 | 使用者今天打開每個專案都是空的。這不是「誠實」，是沒有訊息 |
| 3 | 2,000 筆滿了之後怎麼辦？ | **講出來＋讓人決定封存**，不要自動刪 | 自動刪會刪掉還被引用的；他們避開這個坑是對的，只是忘了講 |
| 4 | 時間軸要不要在這一波做？ | **建議不要**，等 1 與 2 有答案 | 現在實作等於把一個「預設看不到東西、而且會停止記錄」的功能原樣搬過去 |

---

## 7. 這份文件沒有量到的

- **多機／多使用者**：所有數字來自一台機器。
- **`app.clawdline.com` 上的時間軸**：沒有量過（舊版也沒有）。
- **部署證據路徑**：從未在真實資料上發生過，所以 reconciler 的五個部署分支
  （`Model:161-220`）從未被走過，也就從未被觀察過。
- **重新設計後的成本**：B1、B2、A5 的收益是推論，沒有原型量測。
- **`ProjectTimelineWorkflow` 之類的鄰居**：時間軸沒有這種東西，這一欄留白是因為真的沒有。
- **`docs/project-timeline.md` 自己的完整內容**：本文件只引用了與判斷相關的段落。
