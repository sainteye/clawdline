# 25 筆待答提議，逐筆量過一次（2026-09-21）

使用者問：「這些東西真的是提議嗎？是不是一些不重要的東西？或是已經落地的東西？」

答案是第三種居多：**17 筆已經不成立、7 筆還成立、1 筆量不到**。它們不是不重要——
「通知重試沒有退避也沒有上限」「額度說 unknown 的時候要說出為什麼」都是具體缺陷——
而是**做掉它們的是別的工作，而提議不知道**。

系統的 sweep 只從帳面重新判定（有沒有變成工作項目、to-do 收掉了沒、規則還成不成立），
從來不問「這筆提議描述的狀況現在還成立嗎」。所以一件事被另一條線做完之後，
提議會繼續問下去，直到七天過期。

下表每一列的證據是在當天的樹上重新量的，不是引用。

# 25 筆 pending 提議的現況量測

量測基準是目前工作樹 `393cb7ca400d3ab532886f31e13c72c00e209220` 與目前執行中的 daemon；對 daemon 的查詢除 CHILD 簽收外只使用 `GET`，沒有改動 proposal 狀態、daemon 資料或程式碼。派工內容的清單段落遭 shell heredoc 文字取代，因此先從派工時留下的 `/tmp/p25.txt` 復原順序，再與 daemon 回傳的 25 筆 pending proposal 逐筆核對 id 與標題。

| id | 判定 | 證據 | 一句話 |
|---|---|---|---|
| `17c1468e` | `STILL TRUE` | `web/console/src/session/order.ts:58-62,85-94` 以 `row.activity` 排序；`web/console/src/session/List.tsx:311-341` 畫出的列只有標題、meta、task 與 state。live `GET /v1/sessions` 的列都有 activity。 | activity 資料存在也參與排序，但仍沒有畫在 session 列上。 |
| `b0bd6313` | `GONE` | `docs/cutover.md:1-6` 記錄 Swift app 已在 2026-09-19 停止；目前 Go route 在 `internal/transport/http/sessions.go:408-410` 寫入 `Activity`，contract 在 `internal/contract/zz_generated.go:4993-4997` 定義它，live `/v1/sessions` 也逐列回傳。 | 提議所指的舊 app 已不是服務來源，現行 daemon 已有 activity。 |
| `089add82` | `GONE` | 原交付 branch tip `ceb604a` 是目前 HEAD 的 ancestor（`git merge-base --is-ancestor … HEAD` 回 0）；live `/v1/diagnostics` 回傳該改動加入的 `screens.capture_slots` 與 `cache.terminal_screens`。 | 這份改動不只已合併，現在跑著的 daemon 也已帶著它。 |
| `998b970d` | `GONE` | `docs/cutover.md:266-269,281-282` 留有真 daemon 的一分鐘逾時實測；`internal/app/orchestrator/watch.go:446-451` 會落成 `StateTimeout`，當天一筆真 task 的紀錄也呈現 timeout 與對應摘要。 | A2 的逾時已由實際 daemon 與目前程式路徑證實。 |
| `8c1b8d7a` | `GONE` | `docs/cutover.md:283-295` 記錄真 daemon 的跨 root claims 衝突回 `409 workspace_busy`、同 root 回 200 加 `claims_overlap` 警告；目前實作在 `internal/app/orchestrator/dispatch.go:230-266`，守衛測試在 `internal/app/orchestrator/w1_test.go:492-507`。 | A3 已有跨 root 與同 root 的實測答案。 |
| `d849dd79` | `STILL TRUE` | `web/console/src/legacy/devices-bridge.ts:13-27` 明載 local page 缺 account list／local route，`:98-103` 的 local console 也沒有安裝 account source；該 bridge 的 Node 測試 15/15 通過。 | 本機 console 目前仍只列 serving machine，還沒有帳號機器清單。 |
| `65893a55` | `STILL TRUE` | `web/console/src/cloud/CloudGate.tsx:180-188,1178-1202` 只在 picker 標 `data-forgotten`；Devices source（`:570-585`）只設 `selectable:false`／`autoSelectable:false`，`web/console/src/legacy/js/view/devices.js:109-155` 沒有 forgotten 呈現。 | 「這個分頁已忘記」仍未顯示在裝置頁的裝置列上。 |
| `658a9133` | `STILL TRUE` | `rg -l '^async function jsonFetch' web/console/src` 目前找到 8 份定義，分散在 `door/api.ts` 與 board、documents、ledger、projects、schedules、start、timeline bridges，仍沒有共用 envelope reader。 | 數量已由十份變八份，但重複的核心問題仍在。 |
| `102ca0be` | `STILL TRUE` | `web/console/src/pages/settings/window/copy.ts:4-15` 仍宣告沿用 `Copy+Chinese.swift` 詞彙；目前量到 20 個 user-facing key 含 Mac／macOS，例如 `:143,146,148,161,201`。 | 原稱 18 句的數字已變成 20，但跨平台文案仍稱 Mac。 |
| `9b2ba61f` | `CANNOT TELL` | live health／cloud status 能證明真 daemon 已連真 relay 且有 iOS 裝置，但唯讀 GET 無法把一次完整操作歸因為手機發起。 | 要判定需真的用手機走完一次，或有逐命令的 sender receipt。 |
| `e552e002` | `GONE` | `docs/linux.md:3-9,55-69` 記錄 2026-09-20 在 AWS/SSM 的 Ubuntu 24.04 從頭實測與 dispatch success；當天一筆真 task 的 result 也明載 AWS Linux 實測。 | AWS Linux 的端到端驗證已有具體執行紀錄。 |
| `68a36ace` | `GONE` | `web/console/src/session/StatusLine.tsx:81,240-246` 已畫 `ctx …%`；`web/console/src/session/context.ts:31-39` 計算比例，live session `/info` 有 context 值，相關 Node 測試 5/5 通過。 | status line 現在已顯示 context 用量。 |
| `2afb24cf` | `GONE` | `internal/app/orchestrator/composer.go:156-181` 的 placeholders 包含 `Ask Codex to do anything`；live tasks 有大量成功 Codex task，本次 Codex child 也已進入 accepted／working。 | 新 daemon 現在能辨識 Codex composer 並派出工作。 |
| `34dc96c3` | `GONE` | `internal/adapters/terminal/farewell.go:50-63,162-172,223-318` 實作 assistant quit words、quit→TERM→KILL、重讀身分與安全停止；`internal/app/actions.go:527-602` 使用該關閉階梯並回具名錯誤。 | 關閉 session 已走目前 daemon 的安全關閉流程。 |
| `84cd449f` | `GONE` | `internal/adapters/limits/limits.go:24-31,103-122` 定義 `no_record`、`no_reading`、`unreadable`、`too_old`；`api/v1/orchestrator.schema.json:723-748` 與 `internal/transport/http/orchestrator.go:576-610` 都投影 `unknown_reason`。 | 額度 unknown 現在會附帶具體原因。 |
| `02aee496` | `GONE` | `internal/app/cloudops/ops.go:687-714,1295-1324` 註冊 `past-sessions` 與 schedule create/update/delete/run；live cloud commandset 也包含這五個操作。 | Cloud 詞彙表所缺的 past-sessions 與排程寫入已補齊。 |
| `96fdeb6d` | `GONE` | `internal/transport/http/snippets.go:21-27` 有 GET/POST/order/PATCH/DELETE 五條 route，`internal/transport/http/server.go:341-342` 已掛載；`web/console/src/session/Detail.tsx:16,25,138,181,261` 與 `Snippets.tsx:113-193` 已接 UI。 | 常用句的後端與 session UI 都已存在。 |
| `40116d8c` | `GONE` | tmux：`internal/adapters/terminal/reveal_darwin.go:43-51` 與 `internal/app/focus.go:27-49`；網址焦點：`shell/darwin/main.swift:450-452`、`shell/darwin/Browser.swift:417-428`；effort：`internal/adapters/projects/launch.go:194-199`、`internal/app/orchestrator/draft.go:79-107`。 | 提議列出的 tmux、網址框與 reasoning effort 三個缺口都有對應行為。 |
| `322cad32` | `GONE` | `internal/app/orchestrator/record.go:289-309` 定義 `AttemptLimit=8`、失敗 5→300 秒與已送達 2→30 分鐘兩條梯；`notice.go:484-498,532-552` 有 ACK 重試上限與 10 分鐘 hold 界線。 | 通知重試現在有退避與硬上限。 |
| `0d281ce8` | `GONE` | `internal/app/inventory_reading.go:88-100,180-220,271-272` 提供單一 InventoryReading、Recent/Fresh 與獨立 scan budget；`internal/app/screen_held.go:82-99` 有非同步 HeldScreens，`sessions_slow_scan_test.go:39` 守住慢掃描失名模式。 | 一次盤點已供多個讀者共用，慢畫面讀取也不再拖掉名稱。 |
| `26c5c89c` | `STILL TRUE` | `internal/adapters/process/ps_unix.go:102-116` 明載無 resume 時，從 startup 到 first message 仍沒有可見 id，回 `no_record`；`:140-152,174-195` 才在首訊息後用 open rollout 綁定。live 本 child 在首訊息後已是 `identity:"open_file"`。 | 永久認不出已修，但標題所指「剛開、尚未首訊息」的無身分窗口仍存在。 |
| `43475a0a` | `GONE` | `web/console/src/App.tsx:68-75` 已有 ledger drawer row；`pages/ledger.tsx:92-108` 與 `pages/timeline.tsx:16-23,107-123` 都有頁面，timeline 明載依設計從 work item 進入，實際入口在 `pages/work/Board.tsx:203-217`。 | 兩頁都已存在；timeline 不是缺頁，而是刻意不放 drawer。 |
| `403b8a4e` | `GONE` | `internal/app/orchestrator/effects.go:143-153,220-230` 以 `context.WithoutCancel` 加獨立 timeout 跑 effect；`remedy.go:85-97,106-116` 不再給假 route，`internal/transport/http/coordinator.go:159-166` 的 501 回 `implemented:false` 與真實替代路徑。 | effect 不再綁 client request context，succession 補救也不再指向必定 501 的路由。 |
| `f9512dfd` | `GONE` | `internal/app/orchestrator/linger.go:208-235` 由同一 `tabPolicyOf` 產生 CHILD.md 與 task API 政策；`internal/transport/http/orchestrator.go:1049-1073` 投影 `BrokerTab`，本 task 的 live GET 回完整 setting、value 與四種 ends。 | task API 現在直接帶完整分頁政策。 |
| `bb9ef9a4` | `STILL TRUE` | 主路徑已改為 opt-in upstream（`internal/config/config.go:25-39,74-95`），但 `internal/config/retiredapp_test.go:77-85` 仍明列兩個 `PENDING` 例外；`internal/adapters/swiftstore/file.go:14,19-20,66-67,78-80` 與 `internal/adapters/cloud/account.go:16-20` 仍用舊 app 正在執行／寫入的現在式敘述。 | 最重要的 upstream 規則已修，但 repo 自己仍列出兩處沿用舊 app 現況的殘留。 |

## 這 25 筆裡有幾筆是「已知答案卻還在問」

結果是 **17 筆**：`GONE` 17 筆、`STILL TRUE` 7 筆、`CANNOT TELL` 1 筆。這 17 筆已有目前程式、文件紀錄或 live daemon 回應證明前提消失／工作完成，卻仍維持 pending；其餘 7 筆仍有可指認的現況缺口，1 筆必須靠手機實測或 sender receipt 才能下判斷。

本輪只留下這份 `artifacts/report.md`；沒有修改 repository、daemon 資料或 proposal 狀態。

Fixed but not yet released (awaiting review): Nothing.
