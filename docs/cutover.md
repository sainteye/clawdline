> **實作依據是 [`docs/design-decisions.md`](design-decisions.md)（2026-09-18 起）；本文保留為分析材料，與它衝突之處以它為準。**

# 舊 app 退役：盤點、判準、切換順序

> 使用者目標（2026-09-18 原話）：「舊的 app 退役，然後使用新的 app 來使用 https://app.clawdline.com/」
>
> 這份文件只調查、不改任何程式。它回答一個問題：**舊 app 關掉之後，會少掉什麼、誰要接手、按什麼順序。**
> 每一列的「新版現況」都標了來源檔案或當場量到的回應；沒量到的一律寫「未量」，不算通過。

量測時間 2026-09-18 08:00 前後。新版 repo 在 `e4c5608`，舊 app 在 `~/code/clawdline`（只讀）。

---

## 0. 三句話

1. **今天不能退役，而且卡住的不是畫面。** 畫面已經量到接近 1:1（`docs/replica.md`）。
   卡住的是 **broker**：這台機器上每一個 agent session 的派工、收工、交付與協調能力，
   整個住在舊 app 的行程裡。新 daemon 的對應物目前是一個骨架——有 claims 仲裁與 task 目錄，
   **沒有** child 簡報、task secret、逾時、通知、inflight、handoff、landing、Clawdfather。
2. **但也不需要一次退役。** 兩個 app 今天就同時在跑（舊 `:7717` pid 46747、新 `:7727` pid 53506），
   bundle id、設定目錄、埠、token、服務名全部分開（`docs/plan.md` §4）。所以正確的形狀是
   **一塊一塊搬、每一塊都可以退回去**，最後一塊才是 broker。
3. **有一個反向依賴要先講**：新版目前**唯讀舊 app 的 store** 來畫皇冠、task chip、協調等待、
   交付勾、session 標題與用量數字（`internal/adapters/swiftstore`）。所以「關掉舊 app」
   在今天不是零成本——它會讓**新版自己**掉東西。這條讀取按 `plan.md` §4 是過渡，
   要在退役前由新版自己的 store 取代。

---

## 1. 這張表怎麼讀

| 欄位 | 意思 |
|---|---|
| 舊 app 做什麼 | 只寫會被人或程式用到的行為，不寫內部結構 |
| 新版現況 | ✅ 有並量過／🔶 有但未量或形狀不同／❌ 沒有。後面加 🔧 表示**現在有 in-flight task 在做**（任務 id 寫在該節） |
| 差距 | 關掉舊 app 之後，具體少掉什麼 |
| 誰接手 | 新 daemon／Cloud／使用者自己／不接手（刻意放掉） |
| 波 | 建議的切換順序（見 §8） |

**規模參考**（`wc -l`，2026-09-18）：舊 app `Sources/` 179 檔 151,532 行；
其中 Orchestrator 家族 24,371、Cloud 22,345、Board 13,233、Remote 9,108、Usage 6,415、
Timeline 1,456、WebPush＋SmartNotification 1,318。
新版 Go＋前端：`internal/` 43,277、`web/console/src/` 23,291、`shell/` 4,990、`cmd/` 1,077。

---

## 2. broker：關掉舊 app＝關掉這台機器的派工能力

這是最大的一塊，也是唯一一塊「關掉就會讓別人正在做的事停下來」的。

### 2.1 現在有多少東西掛在上面（當場量到）

- `~/.config/clawdline/orchestrator.json`（6.4 MB）：**767 筆 task**（success 686、cancelled 31、
  failure 22、timeout 13、spawn_failed 11、**briefed 4 在跑**）、25 筆 root assignment、
  104 筆 session delivery、50 筆 closure attestation、971 筆 session activity。
- `~/.config/clawdline/landing-queue.json`（70 KB）：250 個 repository 的 landing path 紀錄。
- `~/.config/clawdline/coordinator.json`：Clawdfather＝一個 codex session（記的是它的 tmux pane id），generation 21。
- `~/.config/clawdline/owned-storage.jsonl`（228 KB）、`coordinator-successions.json`（5 KB）。
- 派工的隔離 worktree 在 `~/Library/Application Support/Clawdline/worktrees/`（6 個 repo、**434 MB**）。

**舊 app 的心跳**（`Orchestrator.swift:6407-6444`）：5 秒一次的 `beat` 推進每一筆活著的 task
（收 `result.json`、判逾時、偵測 spawn 失敗、關分頁、掃 landing、跑 completion pump、
推進 handoff 與 root assignment），60 秒一次的排程時鐘，6 小時一次的清理。
**這個心跳停了，正在跑的 child 寫完 `result.json` 也沒有人收。**

### 2.2 逐項

| 項目 | 舊 app 做什麼 | 新版現況 | 差距 | 誰接手 | 波 |
|---|---|---|---|---|---|
| 派工 `POST /v1/orchestrator/tasks` | 建 task、產 secret、開分頁、寫 `/tmp/.clawdline/<id>/task.json`＋`CHILD.md`、注入第一句話、記 claims、depth、capacity、timeout | 🔶🔧 `internal/app/dispatch.go`：claims 仲裁（實測會擋，`plan.md` §3.2）、寫 `task.json`、開 tmux、送一句 `"Your task directory is …"` | **沒有 CHILD.md、沒有 task secret、沒有 timeout、沒有 depth／capacity、沒有 worktree 隔離、沒有 dispatch-policy 注入**。今天派出去的 child 會收到一句話，不知道怎麼回報 | 新 daemon | 2 |
| 收工 | `beat` 每 5 秒讀 `result.json`、驗簽、記狀態、關分頁 | 🔶🔧 `POST /v1/orchestrator/tasks/{id}/settle` 要**別人來拉**；沒有時鐘會自己去收 | 沒人呼叫 settle 就永遠不會結案 | 新 daemon | 2 |
| `inventory` | 一個 repo 裡 live／unlanded／droppable 三態＋`inventory_generation`（兩條派工路由都拒絕沒帶它的 body） | ❌ | 派工前的重複工作偵測整個消失 | 新 daemon | 3 |
| `inflight` | child 用自己的 secret 讀「這個 repo 還有誰在做什麼」（含隔離分支與 head） | ❌ | child 無法自己查重 | 新 daemon | 3 |
| `messages` | session 之間送訊息（本文件的派工鏈就靠它） | ❌ | 跨 session 溝通消失 | 新 daemon | 3 |
| `notify` | child 用自己的 secret 推一句話給使用者（每 task 5 則、每台每小時 30 則） | ❌ | 沒有 agent 通知 | 新 daemon | 3 |
| `completions` / `completions/reconcile` | 交付回報的送達帳本與對帳 | ❌ | 卡片上的交付勾沒有來源 | 新 daemon | 3 |
| `landings` / `landing-queue` / `/advance` / `/order` | landing 收據、per-repo 佇列與順序（250 個 repo 的紀錄） | 🔶 `internal/app/landing.go` 有 `Lander.Land`（用 git ancestry 證明），**但沒有 HTTP 路由** | landing 無法從外面記錄 | 新 daemon | 3 |
| `handoffs` | 交接一整條工作線（REFERENCES／VERIFICATION／OPEN THREADS） | ❌ | 交接消失 | 新 daemon | 4 |
| `root-assignments` | 開一個獨立 Root／Feature Launch（25 筆紀錄） | ❌ | Feature 無法有獨立擁有者 | 新 daemon | 4 |
| `graphs` | 多節點派工圖 | ❌ | split-and-join 無法宣告 | 新 daemon | 4 |
| `detached-tasks` | `root.session_id: null` 的無人值守自動化 | ❌ | 排程的自動任務沒有載體 | 新 daemon | 3 |
| `coordinator`（Clawdfather） | 註冊、rebind、bearings、successions；機器層級的角色 | 🔶 `/v1/next/coordinator` 有 read／POST 與候選清單，實測 `registered:false` | 新 daemon 沒有接手這個角色；皇冠目前靠唯讀舊 store | 新 daemon | 3 |
| `schedules` 與 `schedule-webhooks` | 6 個排程檔（5 個啟用，全部是真的營運工作：文章發布、production 錯誤巡檢、a private venture對應、a private venture 巡檢）＋webhook bind／delivery | ✅🔧 2026-09-18（一次隔離交付，`docs/schedules.md`）：舊版檔案格式、六條路由、webhook 綁定帳本、匯入／匯出；時鐘在 7796 實測發射、重啟不連發、停機錯過的那一次補跑一次。派工本身仍是 Go 第一版（見 A1、A2） | **5 個每天在跑的營運排程會停**。遷移＝把 6 個 JSON 換成新格式並重新設定 | 新 daemon＋使用者確認 | 2 |
| `durable-reports/promotions` | 把 task 報告升級成不可變、可跨裝置讀的文件（`~/Library/Application Support/Clawdline/durable-reports/`） | ❌ | 報告只剩本機檔案 | 新 daemon | 5 |
| `storage` / `maintenance/restart` | store 健康度、重啟維護窗（讓 app 可以安全重啟而不殺掉 in-flight） | ❌ | 新 daemon 重啟沒有保護 | 新 daemon | 4 |
| `whoami` / `assistants` / `waits` | session 自我識別、可用助理、協調等待 | ❌ | 協調等待（畫面上的「等待中」）沒有來源 | 新 daemon | 3 |
| `verification-runs/reserve` | 編譯／測試的單一插槽鎖（四個 `swift-frontend` 曾把這台 Mac 重開機） | ❌ | **多個 agent 會同時開編譯** | 新 daemon | 3 |
| dispatch policy | `~/.config/clawdline/dispatch-policy.md`（15 KB）在每次派工時被讀進 child 的簡報 | ❌ | 家規不會傳給 child | 新 daemon | 2 |

**結論**：broker 不是「一條路由」，是 **20 條以上路由＋一個 5 秒心跳＋六份持久狀態**。
目前有一個 in-flight task 正在做第一波（「broker 第一波：派工到收工的完整迴圈」，
claims `internal/app/orchestrator`、`internal/adapters/store`、`taskdir`、`supervisor`、`api/v1`）。

---

## 3. 資料：誰擁有、退役後誰寫

| 檔案／目錄 | 大小 | 今天誰寫 | 新版今天怎麼用 | 退役後誰寫 | 遷移方式 |
|---|---|---|---|---|---|
| `~/.config/clawdline/orchestrator.json` | 6.4 MB | 舊 app | **唯讀**（`swiftstore`，依 mtime 快取；讀不到＝「未知」不是「空」） | 新 daemon 的 `clawdline.sqlite3` 事件流 | **不要轉檔**。把它當成凍結的歷史：新 daemon 從空的 store 開始，舊檔留著給 `swiftstore` 讀舊 task 的標題與交付紀錄，直到畫面不再需要 |
| `coordinator.json` | 3.8 KB | 舊 app | 唯讀（皇冠） | 新 daemon 的 `coordinator` 表 | 重新註冊一次即可，不轉檔 |
| `landing-queue.json` | 70 KB | 舊 app | 未讀 | 新 daemon | 250 個 repo 的 landing path 是有用的歷史；建議匯入成 read-only 參考，不要當權威 |
| `owned-storage.jsonl` | 228 KB | 舊 app | 未讀 | 新 daemon | 同上 |
| `project-board.json` | 4.1 MB | 舊 app | 未讀 | 新 daemon | **776 張卡、34 個專案、2,618 筆收據、revision 6343**。新版的 `/v1/board` 是另一種東西（見 §6） |
| `project-board-workflow.json`＋`project-board-history/` | 712 KB＋3.7 MB（555 筆） | 舊 app | 未讀 | 新 daemon | 同上 |
| `project-timeline.json` | 2.4 MB | 舊 app | 未讀 | 新 daemon | 時間軸整個沒有移植 |
| `schedules/*.json` | 6 檔 | 舊 app | 只讀 `schedule_id` 與 `title`（給用量頁） | 新 daemon | **要人工搬**：5 個啟用中的營運排程 |
| `snippets/*.json` | 5 檔 | 舊 app | 未讀 | 新 daemon | 小、可手動重建 |
| `usage.sqlite3` | 6.4 MB＋3 MB WAL | 舊 app | **唯讀副本**（複製主檔與 `-wal` 到自己的 `$TMPDIR`，最多每 5 秒一次） | 新 daemon 要有自己的帳本 | 舊檔＝歷史；退役後新 daemon 若沒有自己的帳本，用量頁會退回 transcript 計算（已實作） |
| `~/Library/Caches/com.tsunamiworks.clawdline/session-images/` | 86 項 | 舊 app | 唯讀（對話裡的 `<clawdline-image id>` 標記沒有第二份來源） | 新 daemon（自己的 `CLAWDLINE_NEXT_DIR/session-images`，已實作，先查自己再問舊的） | 舊圖留著，否則舊對話裡的圖變成一行文字 |
| `durable-reports/` | 44 KB | 舊 app | 未讀 | 新 daemon | 升級過的報告，量少 |
| `~/Library/Application Support/Clawdline/worktrees/` | **434 MB**（6 repo） | 舊 app（`ProjectWorktreeLifecycle`） | 未管理 | 新 daemon | **退役前要決定誰清**。今天沒人清＝硬碟慢慢長大 |
| `remote-audit.jsonl` | 6.4 MB | 舊 app | 新版有自己的（448 bytes） | 各自 | 不合併 |
| `secrets/`、`*-token`、`push.json`、`orchestrator-archive-key` | — | 舊 app | **一律不讀**（`plan.md` §4 規則） | 新版自己的 | **不要遷移。**重新產生、重新配對 |

**一條規則**：`~/.config/clawdline` 與 `~/.config/clawdline-next` 不共用任何一個檔案。
今天新版對前者只有 `O_RDONLY`，退役後應該連讀都不要——這條讀取是一個獨立 adapter
（`internal/adapters/swiftstore`），設計上就是要能整條拿掉。

**B1 之後（2026-09-19）**：新版**先用自己的 store**（broker 的 task、waits、`session.delivered`、handoff、
Feature Root、coordinator 表，以及自己從 transcript 算的用量），舊 store 只當「歷史補充」；
`CLAWDLINE_NEXT_LEGACY_STORE=off` 把整條舊讀取關掉——`orchestrator.json`、`coordinator.json`、`config.json`、
`schedules/`、`dispatch-policy.local.md`、`usage.sqlite3`、`project-board.json`＋`project-board-history/`
一個都不開，回應裡以 `disabled` 具名（task 列表 `store`、用量 `source.legacyLedger`、`/v1/board/tracks` 的
`sources.board/history.status`）。舊檔不存在時是 `absent`（已知沒有），不再是「未知」。
**不歸這個開關管**的只有 B3 的舊圖片快取（`session-images`、`drops`）與語音模型——那是歷史資產，不是狀態。

---

## 4. Cloud（app.clawdline.com）：做到哪、還缺什麼

使用者的目標句子裡就有這一段，所以這一節寫得比別節細。

### 4.1 已經做到的（都有實測紀錄，見 `docs/cloud-wire.md` §15–16）

| 層 | 狀態 | 證據 |
|---|---|---|
| Envelope、canonical JSON、簽章、加密、時鐘、重送 | ✅ | `internal/domain/cloud/`；對舊 app 的 186 KB known-answer 向量（SHA-256 `ca354b68…`）比對 |
| 傳輸（WSS `/v1/connect?role=machine`、退避重連、outbound spool） | ✅ | `internal/transport/cloud/`、`internal/adapters/cloud/`（共 14,605 行） |
| 27 種操作 | 🔶 **接上 17 種、10 種回具名的 `unknown_command`** | `internal/app/cloudops/`；逐項表在 `cloud-wire.md` §10.5 |
| 接進 `clawdline serve`、`cloud_enabled`／`cloud_commands` 兩個開關 | ✅ | `cmd/clawdline/main.go` 的 `startCloudLine` |
| 入站 roster（帳號裝置表、`caps` 實際生效） | ✅ | `internal/adapters/cloud/roster.go` |
| **端到端：hosted console 真的操作這台 Mac** | ✅ **但只對本機複本** | mongod 27117＋api 8180＋relay `wrangler dev` 8787＋自簽 TLS 8443；console 是 `tools/build-web-app.py` 的產出，**一個位元組都沒改**。量到：機器卡「已連上」、11 個 session 全列、transcript 展開、從 console 送訊息進可拋棄 session |

### 4.2 還缺什麼才能接正式環境

| 缺口 | 嚴重度 | 說明 |
|---|---|---|
| **配對（機器這一半）** 🔧 | **擋路** | Go 版沒有 QR／四階段 handover。端到端實測是用 devtools 把四筆 IndexedDB 直接種進去的，**證明的是傳輸與操作，不是配對**。有一個 in-flight task「Cloud 第五階段：配對（機器這一半）」正在做 |
| **帳號與裝置核准** | **擋路** | 核准發生在帳號那一端。把 Go 版配進使用者真正的 Cloud 帳號**會動到帳號**，也要在已信任的裝置上按核准——`docs/remote.md` 設計原則 3 明寫「要先問他」 |
| 正式環境從未連過 | 擋路 | `relay.clawdline.com`／`api.clawdline.com` **一個位元組都沒連過**。D1（2026-09-19）把機器這一邊準備到「只差使用者按一下」：操作手冊 `docs/cloud-cutover.md`，錯誤名稱與假扮正式端的測試見 `cloud-wire.md` §18 |
| 10 種操作沒有 | 中 | `agent`、`shell`、`skills`、`board.items`、`timeline`、`snippets`、`schedule`、`diagnostics.report`、`diagnostics.events`、`dispatch`。回 `unknown_command` 是誠實的——hosted console 會把它記進 `machineLacks` 不再問——但**那幾個按鈕在手機上就是不會動** |
| `dispatch` 刻意拒絕 | 中 | hosted console 只送 `{task}`，本機 broker 要 materialized 的 `task.json`＋id＋secret，沒有 pinned wire shape，所以回 `cloud_dispatch_unpinned` 409（照舊版）。**從手機派工＝沒有** |
| entitlements、推播、`ctlr/` 回覆軌、交接通道 | 中 | 都沒動 |
| `sessions.snapshot` 與刪除屏障 | 低 | `/v1/sessions` 的 `scan.complete` 一直是 false，所以不發清單標記，代價是**消失的 session 不會從 console 上消失** |
| `t/` 沒有主動推播 | 低 | transcript 即時更新在舊版靠 `transcript_signature` 變動觸發，Go 版還沒算那個簽章 |
| Cloud 狀態沒有進契約 | 低 | `/v1/cloud/status` 的形狀沒進 `api/v1/`，設定頁自己寫型別 |

**兩個 app 同時接同一個 Cloud 帳號要小心**：`docs/remote.md` 設計原則 3 說 Go 版要有自己的裝置身分，
否則兩個 daemon 會搶同一台 Mac 的序號與重送視窗。舊版 `docs/cloud.md` 另外記了一條：
同一個帳號上有多台 Mac 時，push 的控制平面會用 `cloud_machine_ambiguous` 拒絕。
**所以「新舊同時上 Cloud」的安全做法是：Go 版以「第二台機器」配對，不是接管同一個身分。**

> **本次量到的一個落差**：現在跑在 `:7727` 的那顆 binary（`~/code/clawdline-go/bin/clawdline`，
> 2026-09-18 06:47 建置）對 `/v1/cloud/status` 回 **501 not_implemented**，
> 而這條路由在 `e4c5608` 的原始碼裡是有註冊的（`43a585f` 併進來的）。
> 也就是**正在跑的 daemon 比 HEAD 舊**。驗收時要以重建後的 daemon 為準。

---

## 5. 手機：推播、PWA、service worker

**結論：新版今天完全沒有這一塊，而且它不只是「少一個路由」。**

### 5.1 舊 app 怎麼做

- **VAPID 金鑰**：P-256，第一次要用時才產生，存在 `~/.config/clawdline/push.json`（0600）的
  `vapid_private`。同一個檔案的 `subscriptions[]` 每台裝置一列（`endpoint`、`p256dh`、`auth`、
  `device`、`origin`、`created`）。（本次調查沒有讀這個檔，只從 `WebPush.swift:171-236` 讀出結構。）
- **直送，不經 Cloud**：Mac 自己用 RFC 8291 `aes128gcm` 封裝、RFC 8292 ES256 JWT 簽章，
  `URLSession` 直接 POST 到 `endpoint`（例如 `web.push.apple.com`）。404／410 才刪訂閱。
  Cloud 只是把 `push-key`／`push-subscribe`／`push-unsubscribe`／`push-test` 這四個**控制請求**轉進來。
- **誰會送**：session 停下來問問題（無條件，優先於所有偏好）、交付收據、扇出批次完成、
  agent `/notify`、排程失敗或逾時、部署完成。六個設定開關在 `docs/notifications.md:307-327`。
- **PWA 的檔案全部是 Swift 即時產生的**，不是 `Resources/web` 裡的靜態檔：
  `/sw.js`（`RemotePage.swift:1152`）、`/manifest.webmanifest`（`:1242`）、
  `/icon-{32,64,180,192,512}.png`、任意尺寸的 `/splash-<w>x<h>.png`、
  `/project-<size>-<packed>.png`（`RemoteIcon.swift`，URL 本身就是內容，快取一年）。
  這些全部在免 token 的開放清單上——**通知要畫圖示時，作業系統是用自己的身分去抓的**。
- **手機怎麼裝**：iOS 的 Safari 分頁沒有 Push API，所以必須「加到主畫面」再從那裡開。
  來源 origin 可以是 cloudflared 的網域，或 `https://app.clawdline.com`。
  **純區網 HTTP 不能註冊 service worker**（不是安全內容）。

### 5.2 新版現況

| 東西 | 新版 |
|---|---|
| `/v1/push/*` 四條路由 🔧 | ❌ 一條都沒有 |
| RFC 8291／8292 送出器 🔧 | ❌ 整個沒有（`grep vapid\|webpush\|p256dh\|aes128gcm` 零命中） |
| `/sw.js`、`/manifest.webmanifest`、圖示、splash | ❌ **閘門的開放清單裡有這些路徑，後面卻沒有 handler**（`gate.go:237-254` 的 `openPath` vs `page.go`）。當場量到：`/sw.js`、`/manifest.webmanifest`、`/icon-192.png`、`/favicon.ico` 四條在 `:7727` 全部 **404**，在 `:7717` 全部 **200**。`web/console/public/` 只有字串目錄 |
| 設定頁的通知區塊 | 🔶 刻意 `hidden`，並寫明「push 是裝置與 Cloud 的功能，這裡不做」 |
| cloudflared 監督 | ❌ 沒有 `RemoteTunnel` 的對應物，沒有任何程式寫 `cloudflared.yml` |
| 本機配對（六位數） | ✅ `internal/transport/http/auth.go`＋`shell/darwin/Pairing.swift` |
| Cloud 的四個 push 命令 | ❌ `cloud-wire.md:817` 把 WebPush 排在最後一波 |

有一個 in-flight task「推播：Web Push（手機通知）」正在做這一塊
（claims `internal/adapters/push`、`internal/transport/http/push.go`、`web/console/public`、`docs/push.md`）。

### 5.3 一個要先決定的事（我先選了安全的預設）

**要不要把舊的 VAPID 私鑰搬到新版？**

- 搬：所有手機不用重新訂閱。代價是**複製一個秘密**，違反 `plan.md` §4 的「不讀秘密」與
  `remote.md` 設計原則 3 的「Go 版要有自己的身分」。
- 不搬：新版自己產生金鑰，**每一台手機都要重新加到主畫面、重新允許通知**。
  一次性成本，之後兩邊互不干擾；而且如果 origin 也換了（tunnel 網域或改走 app.clawdline.com），
  訂閱本來就綁 origin，**再怎麼搬金鑰都還是要重訂**。

**預設選不搬**，理由是後面那一句：訂閱同時綁 origin 與金鑰，換 app 幾乎一定會換 origin。
若使用者想要「完全無感」，那才需要另外設計（同一個 origin＋搬金鑰），請在驗收時說。

---

## 6. 其他

| 項目 | 舊 app 做什麼 | 新版現況 | 關掉之後 | 誰接手 | 波 |
|---|---|---|---|---|---|
| **Claude Code Hook** | `~/.config/clawdline/hook.sh` 掛在 `~/.claude/settings.json` 的 **8 種事件、11 個 matcher**，每個 `timeout:5`。它**不連 7717**，只把每個 tty 的狀態寫成 `hooks/<ttysNNN>.json`；app 在行程內用目錄監看讀它 | ❌ 沒有 hook（`inventory.go:154` 明寫 "this daemon installs no hooks"）。殼的設定頁只**讀**得出有沒有裝，`supported:false`，且只認 `clawdline-next/hook.sh` 這個字串，兩個 app 看不到彼此的 | session 狀態的**快路徑**消失，退回 20 秒一次的畫面輪詢（舊 app 自己把這叫「明確的非降級退路」）。腳本會繼續寫沒有人讀的檔；`.tty-*` 備忘檔會留下沒人掃 | 新 daemon（裝自己的 hook）或不接手 | 4 |
| **skills** | `GET /v1/sessions/{id}/skills`：走 `.claude/skills` → `~/.claude/skills` → plugin，只回 metadata；Codex 的則從 rollout 檔解析 | ❌ 沒有這條路由（Cloud 的 `skills` 命令因此回 `unknown_command`） | 手機／瀏覽器 composer 的 `/` 斜線選單變空。**還是可以盲打**，助理端不受影響 | 新 daemon | 4 |
| **常用句（snippets）** | 5 個 JSON 檔；`GET/POST /v1/snippets`、`/order`、`PATCH`、`DELETE`；寫入要 write 開關＋`Idempotency-Key` | ❌ | 常用句面板整個沒有東西；檔案還在，但**沒有任何原生 UI**，只能手改 | 新 daemon | 5 |
| **時間軸（Timeline）** | `/v1/timeline`＋`project-timeline.json`（2.4 MB） | ❌ | 整個功能消失 | 新 daemon | 5 |
| **看板（Project Board）** | 19 欄的 `/v1/board`、`/v1/board/items/{item}`（＋history）、`/v1/board/projects/{p}/items`、workflow gaps；776 張卡 | 🔶 **同名不同物**：新版 `/v1/board` 回的是「從現在跑著的 session 推出來的專案清單」（`domain/board/board.go`），實測 200 但沒有卡片模型 | 卡片、歷史、workflow 全部消失。側欄的「看板」在新版根本還沒有頁 | 新 daemon | 5 |
| **驗證帳本** | `/v1/orchestrator/usage/verification-ledger`＋側欄的頁 | ❌ | 消失 | 新 daemon | 5 |
| **Onboarding** | `~/.config/clawdline/onboarding.json`（已完成，version 1）。**只管一件事**：啟動時要不要開 Home 視窗 | ❌ 未移植 | 已經跑完的機器上**什麼都不會壞**。只有重跑導覽會斷 | 不接手（可延後） | 6 |
| **選單列、⌥Space 快捷面板、瀏海島** | 狀態列 `✳`（等待時 `●`、在跑時數字、tooltip 列出等待中的 session）、狀態列選單（含更新通知、吉祥物、登入時啟動）、主選單、Carbon 全域熱鍵、`NotchIsland` | 🔶 選單列與熱鍵 ✅（`841b870`，用 System Events 逐字比對）；登入時啟動 ✅ 開關（預設關、**未實際打開測過**）；瀏海島 `shell/darwin/NotchIsland.swift` **寫了但未編譯未執行**；快捷面板目前用 webview 視窗代替（舊版 5,400 行原生） | 詳見左欄。這些全部是 app 行程裡的 AppKit，**跟 7717 無關**——它們在 app 關掉的那一刻一起消失 | 新 daemon 的殼 | 3 |
| **`clawdline://` URL scheme** | `toggle`、`push?test=1`、`settings`、`home`／`setup`、`hooks?install=`、`send?text=&target=`、`snapshot?…`、`filmstrip?…`——這是 Stream Deck／捷徑／shell 腳本的門 | ❌ 未移植 | **所有外部自動化的入口消失**。這一項在舊盤點裡最容易漏 | 新 daemon 的殼 | 4 |
| **配對核准的 alert** | 六位數配對碼**只在 Mac 螢幕上出現**，這就是它的全部安全性質 | ✅ `shell/darwin/Pairing.swift`（訂 `/v1/auth/pairings`），但**這台機器上沒有觸發過** | — | 新 daemon 的殼 | 3 |
| **更新檢查** | 每天一次查 GitHub，是已安裝的副本唯一得知新版的方式 | ❌ | 不會再知道有新版 | 新 daemon | 6 |
| **編譯插槽鎖** | `verification-runs/reserve`：`./test.sh`／`./build.sh` 各自佔一個插槽（四個 `swift-frontend` 曾把這台 Mac 重開機） | ❌ | **多個 agent 會同時開編譯** | 新 daemon | 3 |
| **worktree 生命週期** | 建立／回收隔離 worktree，`reclaimed-checkouts/` 保存未落地的位元組 | ❌ | 434 MB 的 worktree 沒有人回收 | 新 daemon | 4 |
| **Dock 圖示** | — | ✅ 與舊 app 逐位元組相同 | 兩個 app 同時跑會有兩個一樣的圖示與 `✳`——**這是忠實復刻的結果，不是 bug** | — | — |

---

## 7. 可以退役的判準

每一列都是可勾選的，而且寫了**怎麼驗證**。驗證方法一律是「發真的請求／做真的動作」，
不是「讀程式碼覺得有了」。沒驗過的一律當成沒過。

### A. broker（不過這一組就不能關舊 app）

- [ ] **A1 一輪派工可以全程走完新 daemon。**
      驗：用新 daemon 派一個真的 child（`POST :7727/v1/orchestrator/tasks`，帶 orchestrator token），
      child 收到有 CHILD.md 的簡報、寫 `result.json`，**在沒有人手動呼叫 settle 的情況下**
      task 在 5 分鐘內變成 `success`。
- [ ] **A2 逾時會自己發生。** 驗：派一個 `timeout_minutes: 1` 而且不寫 result 的 task，
      確認它自己變成 `timeout` 並關掉分頁。
- [ ] **A3 claims 仲裁會擋。** 驗：兩筆 claims 重疊的派工，第二筆回 `409 workspace_busy`，
      而且**在開任何東西之前**（`plan.md` §3.2 已經量過一次，換到真 daemon 再量一次）。
- [ ] **A4 child 可以用自己的 secret 做三件事**：`/progress`、`/notify`、`GET /inflight`。
      驗：三個請求各回 2xx，錯的 secret 回 403（用 `--fail-with-body` 看 exit code）。
- [ ] **A5 landing 有路由而且會拒絕假的。** 驗：`POST …/landing` 帶一個**不是** branch 祖先的 commit，
      回 `unverified_landing`；帶真的，回成功並關掉義務。
- [ ] **A6 inventory 有 `inventory_generation`，而且舊的 generation 會被拒。**
      驗：讀一次、故意用舊值派工、收到 `409 stale_inventory` 且回應帶回整份 inventory。
- [ ] **A7 Clawdfather 可以在新 daemon 上註冊並被畫出來。**
      驗：`POST /v1/next/coordinator` 註冊後，`registered:true`，清單上出現皇冠，
      **而且此時 `swiftstore` 是關掉的**（否則證明不了來源）。
- [ ] **A8 排程真的會在新 daemon 上發射。**（2026-09-18 在隔離的 7796 實測通過，證據在那次排程交付的 task artifacts，不在 repo 裡；
      規則已換成舊版的「時刻＋補跑窗」，`every 1h` 不再存在，驗法改成「建一個一分鐘後的排程」。在 7727 上仍待重建後驗） 驗：建一個 `every 1h` 的排程，
      確認 `first_seen` 規則生效（**第一次在一小時後**，不是立刻），
      到期時真的開了分頁，`/v1/diagnostics` 的 `due` 與 `fired` 對得起來。
      **驗法補一條（`design-decisions.md` D53，W4）**：排程的 run 走 broker——有自己的 secret、CHILD.md 與逾時；
      一個不寫 result 的 run 要在自己的 `timeout_minutes` 到時變成 `timeout`，並放行同一排程的下一次；它的 claims
      與 broker task 互相擋（`409 workspace_busy`，在開任何東西之前）。2026-09-18 在隔離的 7807（私有 tmux、假的
      `claude`）實測通過，證據在那次交付的 `artifacts/report.md`（本機 task 目錄，不在 repo 裡）；7727 仍待重建後驗。
- [ ] **A9 五個營運排程搬過去而且各跑成功一次。**（匯入與內容一致已用複本驗過 6/6；「各跑成功一次」要真的切換，見 `docs/schedules.md` 的順序）
      清單：文章發布、a private venture發布、dual production 錯誤巡檢、a private venture餐廳對應與 production 錯誤、
      a private venture 內容修正與對話異常巡檢。**這一項要使用者自己確認結果對**，不是看它有沒有開分頁。
- [ ] **A10 編譯插槽鎖存在。** 驗：同時要求兩次驗證，第二個排隊而不是同時開 `swift-frontend`。
- [ ] **A11 dispatch-policy 會進 child 的簡報。** 驗：派一個 child，請它把簡報裡的家規原文回報一段。

### B. 資料

- [ ] **B1 新 daemon 不再需要讀 `~/.config/clawdline`。**
      驗：把 `CLAWDLINE_SWIFT_DIR` 指到一個空目錄跑一次，畫面上皇冠、task chip、
      協調等待、交付勾、標題**仍然正確**（來源換成自己的 store）。**這是退役的硬門檻。**
      **驗法補一條（B1 實作）**：改用 `CLAWDLINE_NEXT_LEGACY_STORE=off` 啟動（它連
      `usage.sqlite3`、舊看板與家規 local 檔都不開，比指空目錄完整）；對照組是同一組「金絲雀」舊檔在
      開關 `on` 時要出現在回應裡、`off` 時一個都不能出現。2026-09-19 在隔離的 7815 實測：`off` 時
      session 清單、task 列表（`store: disabled`）、用量（`source.legacyLedger: disabled`）、看板、tracks、
      設定、專案都 200，daemon 沒開任何舊 store 檔（`lsof` 取樣）；新版自己記的交付回報畫成交付勾。
      **7727 仍待重建後驗**；皇冠、task chip、協調等待在 live daemon 上要有真的 broker 事實才看得到，
      單元測試已涵蓋投影，live 未量。「只有舊資料才有」的歷史清單記在那次 B1 實作交付的 `artifacts/report.md`（本機 task 目錄，不在 repo 裡）；要重列，就用上面同一組金絲雀舊檔，開關 `on` 與 `off` 各啟動一次，比對兩邊回應的差集。
- [ ] **B2 用量頁有自己的帳本。** 驗：同上情境下用量頁仍有數字，而且不是 transcript 退路
      （回應要說得出來源）。
      **B1 實作後**：用量的列**一律**是新版自己從 transcript 算的（它就是新版的帳本，不再是退路），
      舊 `usage.sqlite3` 只補「transcript 已經不在」的 conversation（同一個 conversation 只取一個來源），
      回應的 `source` 說出用了哪些；舊帳本讀不到時 `availability` 是 `partial / legacy_ledger_unreadable`。
      **沒做的**：新版 broker 沒記 child 的 conversation id，所以新版派出去的 task 在用量頁歸成
      `manual`（舊版派的仍由舊 store 歸屬）；transcript 被清掉後新版自己的歷史就沒了——要不要落盤見報告。
- [ ] **B3 舊圖片仍讀得到。** 驗：打開一則有 `<clawdline-image>` 標記的舊訊息，圖片出得來。
      （這一條**允許**繼續唯讀舊快取目錄——那是歷史資產，不是活狀態。）
- [ ] **B4 worktree 有人回收。** 驗：跑一次回收，確認已落地的殘留被移除、
      **未落地的先被保存成可驗證的 patch／branch**。
- [ ] **B5 舊資料已凍結並備份。** 驗：`~/.config/clawdline` 與
      `~/Library/Application Support/Clawdline/{project-board*.json,project-board-history,durable-reports,Observability}`
      有一份離線副本，而且**確認過新 daemon 不會寫進去**。

### C. 遠端與手機

- [ ] **C1 手機能連得到新 daemon。** 驗：在手機上開新 daemon 的頁面，六位數配對走完，
      清單出得來。（碼只在 Mac 螢幕上出現。）
- [ ] **C2 有一個安全 origin。** 驗：tunnel 或 Cloud 其中一條真的通，`https`，
      service worker 註冊得起來。**純區網 HTTP 不算。**
- [ ] **C3 推播真的會響。** 驗：`/v1/push/key` → 訂閱 → `/v1/push/test` 在手機上跳出通知，
      **而且點下去會開到那個 session**。
- [ ] **C4 該響的時候會響。** 驗：至少兩種真實觸發各一次——session 停下來問問題、任務交付。
- [ ] **C5 通知上有圖示。** 驗：`/icon-192.png` 與 `/manifest.webmanifest` 有人回答
      （今天閘門放行但**後面沒有 handler**）。
- [ ] **C6 PWA 裝得起來。** 驗：iOS 加到主畫面、從主畫面開、確認是 standalone。

### D. Cloud（app.clawdline.com）

- [ ] **D1 機器這一半的配對真的做過一次。** 驗：**不用 devtools 種 IndexedDB**，
      走真的 QR／四階段 handover。
- [ ] **D2 連過正式環境。** 驗：`relay.clawdline.com` 與 `api.clawdline.com` 各一次成功連線，
      `/v1/cloud/status` 說得出帳號、機器指紋與已登記的 viewer。
- [ ] **D3 以「第二台機器」配對，不是接管舊身分。** 驗：帳號的裝置表上舊 Mac 與新 Mac 各一列，
      推播沒有 `cloud_machine_ambiguous`。**前提是方案允許兩台 Mac**：免費方案 `max_machines` 是 1，
      第二台在核准頁就會被 `machine_limit_reached` 擋下（`docs/cloud-cutover.md` 步驟 1）；
      另外，兩台都配對到同一個瀏覽器時，hosted console 的推播設定**依設計**就會回 `cloud_machine_ambiguous`
      （舊 repo `docs/cloud.md:761, 1140-1144`：一台 Mac，否則 ambiguous），跟上面的驗法互相衝突——**驗法待重新決定**，
      D1 沒有替它選。
- [ ] **D4 hosted console 上 27 種操作的期待是誠實的。**
      驗：`descriptor` 的 `commands` 陣列＝`cloudops.Implemented()`，
      沒接的那幾種在 console 上是**不出現或明說不支援**，不是按了沒反應。
- [ ] **D5 session 清單會自己收斂。** 驗：關掉一個 session，console 上那一列**會消失**
      （今天 `scan.complete` 一直是 false，所以不會）。
- [ ] **D6 從手機派工這件事有結論。** 要嘛把 `dispatch` 的 wire shape 釘下來並實作，
      要嘛**明說不支援**。今天是 `cloud_dispatch_unpinned` 409。

### E. 原生殼與自動化

- [ ] **E1 選單列、⌥Space、⌘, 在打包後的 app 上真的動過。**（目前熱鍵錄製「只做過型別檢查」。）
- [ ] **E2 登入時啟動真的打開過一次並重開機驗證。**（目前預設關、未測。）
- [ ] **E3 瀏海島編譯得過而且跑得起來。**（目前**未編譯未執行**。）
- [ ] **E4 `clawdline://` 的替代品存在，或確認不需要。**
      這一項要使用者回答：有沒有 Stream Deck／捷徑／腳本在用舊的 URL scheme。
- [ ] **E5 hook 有結論。** 要嘛新版裝自己的 `clawdline-next/hook.sh`，
      要嘛**把舊的從 `~/.claude/settings.json` 移掉**（否則它會一直寫沒有人讀的檔）。
      移除用舊 app 自己的 `uninstall()`，它會保留別人的 hook。

### F. 畫面（`docs/replica.md` 已覆蓋大半）

- [ ] **F1 側欄七頁都有。** 今天缺**看板**與**驗證帳本**兩頁。
- [ ] **F2 失敗路徑比對過。**（`replica.md` 的 R2：stale 橫幅、503、429、看板讀不到的後備，
      **沒有任何獨立比對**。這是覆蓋缺口。）

---

## 8. 建議的切換順序

### 8.1 先講回退：兩個 app 能不能並存？

**能，而且現在就是這樣。**（2026-09-18 08:10 實測：舊 app pid 46747 佔 `:7717`、
新 daemon pid 53506 佔 `:7727`，兩邊 `/v1/health` 同時有回應。）分開的東西：

| | 舊 | 新 |
|---|---|---|
| bundle id | `com.tsunamiworks.clawdline` | 另一個 |
| 設定目錄 | `~/.config/clawdline` | `~/.config/clawdline-next` |
| 埠 | 7717 | 7727 |
| token 檔 | `orchestrator-token` 等 | 自己的 `local-token`／`orchestrator-token` |
| cookie 名 | `clawdline` | `clawdline-next`（**刻意不同**，否則同一個瀏覽器量不了兩邊） |
| store | JSON 檔群 | `clawdline.sqlite3` |
| hook 字串 | `clawdline/hook.sh` | `clawdline-next/hook.sh`（互相看不到） |

**但有三個地方會真的相撞，切換時要當成一次性的事**：

1. **Cloud 帳號身分**——同帳號多台 Mac 時推播會 `cloud_machine_ambiguous`。
   做法：新版以「第二台機器」配對，驗完再決定要不要撤掉舊的那一台。
2. **cloudflared 的網域**——手機的 PWA 與訂閱綁在 origin 上。
   同一個網域一次只能指向一個 port。切過去＝手機要重裝、重訂閱。
3. **唯讀的 `swiftstore`**——這是單向的，不會弄壞舊 app，但它是**新版對舊 app 的依賴**。
   判準 B1 就是在拆這條。

**回退的通則**：每一波都是「新的開起來、舊的不要關」。真的要退回去，就是把新 daemon 停掉
（`cloud_enabled` 設 false、停 `clawdline serve`），舊 app 一直在原地，它的狀態沒有被任何人改過。
**唯一不能靠這招退回去的是手機**——PWA 與訂閱換過 origin 就要再換一次。

### 8.2 波次

| 波 | 做什麼 | 完成的判準 | 怎麼退回去 |
|---|---|---|---|
| **0**（現在） | 盤點（本文件）。備份 `~/.config/clawdline` 與 `~/Library/Application Support/Clawdline/` 的資料檔 | B5 | — |
| **1** | **畫面切換**：日常改用新 app 的視窗看 session、送訊息、開新 session。舊 app 照常當 broker | F1 的前六頁已可用；每天用它，把不順的記下來 | 開回舊視窗，零成本 |
| **2** | **broker 第一波**（派工→收工完整迴圈、CHILD.md、secret、逾時、dispatch-policy）＋**排程搬家**＋**Cloud 配對（機器半邊）** | A1–A5、A8、A9、A11、D1 | 新派的用新的；已經在跑的舊 task 留在舊 broker 跑完。停新 daemon 即可 |
| **3** | **broker 協調面**（inventory、inflight、messages、notify、completions、waits、coordinator、detached、編譯插槽）＋**原生殼補齊**（配對 alert 實測、瀏海島編譯、登入時啟動）＋**Cloud 正式環境（唯讀）** | A6、A7、A10、D2、D3、E1–E3 | 同上。Cloud 那條把 `cloud_enabled` 關掉就斷 |
| **4** | **遠端與手機**：tunnel 或 Cloud 擇一當手機的 origin、push 四條路由＋送出器、`/sw.js` 與圖示、PWA 重裝；**broker 交接面**（handoffs、root-assignments、graphs、storage／maintenance）；URL scheme；hook 的結論；worktree 回收 | C1–C6、E4、E5、B4 | **這一波的手機部分退不回去**（origin 換了）。先在一台手機上試，確認再換第二台 |
| **5** | **B1／B2 拆掉對舊 store 的依賴**＋看板、時間軸、驗證帳本、常用句、durable reports | B1、B2、F1 全部 | 把 `swiftstore` 打開即可暫時退回 |
| **6** | **退役日**：停舊 app、從 `~/.claude/settings.json` 移除舊 hook、取消舊 app 的登入時啟動、把舊 Mac 從 Cloud 帳號撤掉（如果決定撤）、資料改標「歷史」 | 上面全部打勾 | 舊 app 重開即可——只要**沒有刪掉** `~/.config/clawdline` |
| **7** | 清理：刪 `.build`、回收 worktree、決定要不要保留 `~/.config/clawdline` | — | — |

### 8.3 先切哪一半？

**先切「看」，最後切「派」。**

看（清單、對話、送訊息、開 session、設定、用量、專案、文件）今天已經可用而且可逆——
每一次退回去的代價是「把視窗切回去」。派（broker）不可逆的部分不是程式，是**時間**：
一筆派錯或掉了的 task 是一個 agent 幾十分鐘的工作，而且它會靜靜地不見。
所以 broker 的每一項都要有**它自己的**驗證，不能靠「畫面看起來對」。

### 8.4 一個非技術的門檻

`~/.config/clawdline/dispatch-policy.md`（15 KB）今天會被複製進**每一個**可能再派工的 child 的簡報。
它寫的是這台機器的家規（要不要派、一個 task 多大、誰審、驗證預算、編譯插槽）。
**退役不只是搬程式，也要搬這份家規**，而且它裡面提到的路由（`/v1/orchestrator/inventory`
的 `409 stale_inventory`、`POST /v1/orchestrator/handoffs`、`root-assignments`、`detached-tasks`）
**必須真的存在**，否則家規會教 child 去打不存在的門。這一項掛在判準 A11 與 A6。

---

## 9. 這份盤點沒有做到的事

- **沒有量任何效能**：新 daemon 在 767 筆 task 規模下的表現完全沒有測過（今天它的 store 是空的）。
- **沒有跑 build 或測試**：依派工指示，本任務只調查。
- **`/v1/cloud/status` 的 501 沒有追下去**：判斷是「跑著的 binary 比 HEAD 舊」，
  但沒有重建 daemon 驗證（不可以重啟 7727）。
- **沒有讀任何 secret**：`push.json`、`secrets/`、各種 token 檔、`orchestrator-archive-key` 都沒開過。
  推播那一節的欄位結構是從 `WebPush.swift` 讀出來的，不是從檔案。
- **舊 app 的 108 個 route case 沒有逐條測**：路由清單是從 `RemoteServer.dispatch` 與
  `grep -rhoE '"/v1/[^"]+"' Sources/` 抽的，代表**字面路徑**，不是 handler 行為。
- **三個 in-flight task 會改變這份盤點**：broker 第一波、
  Web Push、Cloud 配對。它們落地後，§2、§4.2、§5.2 要重讀一次。
- **看板、時間軸、驗證帳本沒有深入**：只確認了資料規模與新版沒有對應物，
  沒有盤點它們的欄位語意。要移植時需要各自一份規格。

---

## 附錄：這份文件的數字從哪裡來

| 數字 | 怎麼量的 |
|---|---|
| 151,532 行 / 各子系統行數 | `find Sources -name '*.swift' -exec wc -l {} +`（2026-09-18，`~/code/clawdline` 工作樹） |
| 舊 app 130 個字面路徑 | `grep -rhoE '"/v1/[^"]+"' Sources/ \| sort -u` |
| 舊 app 108 個 route case | `RemoteServer.dispatch`（`RemoteServer.swift:928` 起）的 `case` 數；`HTTPRouteTable.swift:6` 的註解也是這個數字 |
| 新版 34 條註冊路由 | `grep -rhoE 'mux\.Handle(Func)?\("[^"]+"' internal/transport/http/` |
| 767 筆 task／25 root assignment／104 delivery | `python3` 讀 `~/.config/clawdline/orchestrator.json` 的 key 長度 |
| 776 張卡／34 專案／2,618 收據／revision 6343 | 同法讀 `~/Library/Application Support/Clawdline/project-board.json` |
| 6 個排程（5 啟用） | `~/.config/clawdline/schedules/*.json` 的 `title` 與 `enabled` |
| 434 MB worktree | `du -sh ~/Library/Application\ Support/Clawdline/worktrees` |
| 兩邊都活著 | `curl /v1/health` 對 `:7717` 與 `:7727`，加 `ps aux` |
| 新 daemon 的路由回應 | 帶 `~/.config/clawdline-next/local-token` 對 `:7727` 發 GET，只讀不寫 |
| hook 掛了 8 種事件 11 個 matcher | `~/.claude/settings.json` 與 `Sources/HookBridge.swift:94-110` 對照 |
| PWA 四條資產 404 vs 200 | `curl -o /dev/null -w '%{http_code}'` 對兩個埠各發一次 |

**這份文件裡每一個「舊 app 會怎樣」的句子，都來自原始碼或當場的請求；
每一個「新版沒有」的句子，都來自對新樹的 grep 或對 `:7727` 的實際請求。
凡是推論，句子裡會有「判斷」「多半」這種字。**
