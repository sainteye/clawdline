# Clawdline Cloud 多機設計：語音、專案、Session、Orchestrator

> **狀態：設計稿，等 Sean 拍板。** 這份文件不改任何程式碼。
> 修訂 2026-09-15：只處理離線，不區分離線原因（依 Sean 指示）。
> 量測基準：`clawdline` 在 `a0be4680`、`clawdline-cloud` 在 `6a10552a`，2026-09-15 讀取。
> 行號都是這兩個 commit 上的；外部資料的網址與讀取結果在附錄 B。

## 1. 一頁摘要

### 要解決的問題

一個 Cloud 帳號底下開始有好幾台機器：一台以上的 Mac，加上 Linux executor。今天很多功能都默默假設「帳號裡只有一台 Mac，而且它在線」。所以那台 Mac 一離線，手機上有一半的功能不是轉圈等 60 秒到 6 分鐘，就是直接說「有不只一台機器，所以什麼都沒送」。另一台在線的機器幫不上忙，因為那些功能只住在 Mac 上。

### 三個問題的答案

**Q1：多台機器時，語音和專案怎麼管、機器怎麼呈現才好用？**
機器是畫面上的一級物件：名字、平台、狀態點（在線／離線／未配對），離線時加上「離線（最後在線 23:10）」。每個功能都寫出「由哪台提供」，預設是**自動**（依偏好順序挑在線的那台），也可以**釘選**。麥克風在你開口前就寫出「這次由哪台的 Whisper 轉寫」。專案一個一列（用 git remote 當身分），列上用小徽章標出哪幾台有這個專案，離線的變灰；開 Session 時預選「上次用、而且在線」的那台並講出來。詳見 5.1 節、5.2 節。

**Q2：負責語音的機器離線時，另一台還能工作、還能開 Session 嗎？**
**今天：只能做一半。** Linux 上已經在跑的 Session 會繼續跑，手機也能在 Linux 上開新 Session；但手機**沒辦法在 Linux Session 裡打字或看畫面**（跟 Mac 在不在線無關，是 Linux executor 只實作了兩個指令），而語音、推播、常用句、排程、Board、Timeline 全都只在 Mac 上。
**設計後：可以。** relay 依機器每 30 秒一次的 ping 判斷連線還活不活著，網頁再用新鮮度窗口保底；手機立刻換到在線的機器，或在送出前說「釘選的那台離線」，不再等逾時。機器為什麼離線，Clawdline 不猜也不管。Linux Session 補上打字與畫面，推播改成每台機器都能發。詳見第 2 節、4.3 節與第 6 節。

**Q3：整個 Orchestrator 該用哪台？能動態調整嗎？**
**新工作的去處可以動態決定，跑到一半的工作永遠不搬。** 每台機器的 broker 仍是自己的最終仲裁（claims、worktree、secret、鎖都是本機檔案系統上的事實）。派工的那一方——手機，或 root 所在的機器——每次派工時依規則挑一台「在線、有這個專案、有容量」的機器，你可以按專案釘選。**不設一台固定的「總指揮」**，所以任何一台離線都不會卡住別台。子任務的結果由執行的機器保管，root 的機器回到在線再交付；交付物是推到 remote 的分支。詳見 5.8 節。

**免費開源版：什麼都不變。** 一台 Mac、本機頁面與 tunnel，看不到任何新東西。詳見 3.1 節。

### 和討論時的方向不一樣的地方

1. **手機內建辨識不當預設退路**：iOS 主畫面 app 用不了，繁中音訊很可能送 Apple，也和 `docs/whisper.md` 的立場衝突（5.1 節）。
2. **不設固定的協調者機器**：由派工方就地挑、broker 仲裁；協調者會離線，反而變成單點（5.8 節）。
3. **帳號資料的範圍收窄**：只有常用句與偏好這類小清單有主機；Board、Timeline、排程、任務是各機器的事實，在手機彙整（4.6 節）。
4. **加密副本先放瀏覽器**：Cloud 持久保存是可選項，而且要改的是 PROTOCOL §2，不是 D8（4.6 節、8.3 節）。
5. **推播不需要 Cloud 轉送**：改成帳號共用的衍生金鑰，每台機器直送（5.6 節）。
6. **機器之間互通要改 relay**：今天 relay 明文不允許機器對機器發指令（8.5 節）。

### 要你拍板的事

第 8 節有九題，每題都有建議選項。最先需要的三題，都會寫進階段 1 的 brief：

1. 8.1「先做哪一段」（建議：階段 1，離線感知與快速換手）
2. 8.2「手機內建辨識要不要當退路」（建議：不做；原因是 iOS 主畫面 app 用不了，而且繁中音訊很可能送到 Apple）
3. 8.6「釘選的機器離線時怎麼辦」（建議：送出前拒絕，附「這次改用 X」一鍵，不偷偷換）

---

## 2. 現況：機器在線、離線時，手機上實際會怎樣

### 2.1 手機今天怎麼知道一台機器還在

**它其實不知道，只能猜。**

- **relay 眼中的「在線」＝有一條完成握手的 WebSocket。** `relay/src/account-do.ts:1830-1838` 只是列出目前的 socket；ping 由 Cloudflare 自動回 pong、不喚醒 Durable Object（`account-do.ts:336`），relay 自己沒有逾時判斷。機器那頭斷網、斷電或整台停住時，沒有人送出關閉，這條 socket 就變成「半開」並繼續被當成在線，直到底層 TCP 發現它死了——要多久，沒有文件寫（實驗 E4）。
- **relay 不會主動告訴瀏覽器某台斷線。** socket 關閉時 relay 只結算計量並回應關閉（`account-do.ts:1033-1042`）；`relay/src` 裡送出的訊框只有 `challenge`、`ready`、`subscriptions`、`publish`、`publish_error`、`ack`、`pong`、`error` 這幾種，沒有「機器離線」。
- **relay 能給的唯一即時訊號是「送指令時沒有人收」。** 指令送出後 relay 回 `ack`，`status` 是 `delivered` 或 `machine_offline`（`relay/src/lib/routing.ts:189-193`、`account-do.ts:897-903`）。網頁收到 `machine_offline` 會立刻報錯（`Resources/web/app/js/net/cloud-client.js:2405-2406`）。但半開的 socket 會得到 `delivered`，那只代表 relay 寫進了 socket。
- **網頁端的「在線」其實是「5 分鐘內看過它的訊息」。** `MACHINE_INVENTORY_FRESH_MS = 5 * 60 * 1000`（`cloud-client.js:48`），據此把機器標成 `current`／`stale`／`unknown`（`cloud-client.js:1277-1291`），而且只在呼叫 `_machineRows()` 時用當下時間算一次（`cloud-client.js:1262-1263`）。程式碼自己也寫明「這是最後一個看到的訊息，不是 presence lease」（`cloud-client.js:307-309`）。Mac 每 4 分鐘換一次 token 並重發快照（`Sources/CloudBridgeLifecycle.swift:160`、`docs/cloud.md:333-338`），Linux 每 240 秒發一次描述（`Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:307`），所以在線的機器會一直是 `current`。
- **逾時就是答案。** 一般讀取 `READ_TIMEOUT_MS = 60000`、語音 `VOICE_TIMEOUT_MS = 6 * 60 * 1000`（`cloud-client.js:46-47`），逾時回 `cloud_read_timeout`（`cloud-client.js:2307-2311`）。

### 2.2 逐項行為

「Mac 離線」這欄假設帳號裡還有一台在線的 Linux executor。

| 功能 | Mac 在線 | Mac 離線 |
|---|---|---|
| 語音輸入 | 送到 Mac 的 Whisper | relay 已收到關閉：立刻 `machine_offline`；半開：等到 6 分鐘逾時 |
| 開新 Session | 選 Mac 或 Linux 都行 | 在 Linux 上可以開 |
| Linux Session 打字、看畫面 | **做不到** | **做不到** |
| Mac Session | 正常 | 看得到最後狀態；操作等逾時或立刻 `machine_offline` |
| 常用句 | 只有 Mac Session 旁有 | 頂多剩手機記憶體裡的舊清單，改不了 |
| 排程 | Mac 上觸發 | 手機看不出會不會觸發；列表重新整理失敗 |
| Board、Timeline | 從 Session 開正常；專案頁在多機帳號被拒 | 讀寫都等逾時或失敗 |
| 推播 | Mac + Linux 帳號訂閱會被拒 | Linux 不會推；Mac 推不推得出去要看它還連不連得到推播服務 |
| 從手機派工 | 被拒 | 被拒 |

**證據：**

- **語音**只找 Mac：解析器排除宣告成非 Mac 平台的機器（`cloud-client.js:1330-1336`）；使用者選過的機器「不管多舊都照用」（`cloud-client.js:1312`、`:1347`）；逾時 6 分鐘（`cloud-client.js:2839-2840`）。Linux 沒有 Whisper 也沒有 `voice` 指令（`docs/whisper.md:258-260`）。
- **開新 Session**：Start sheet 只有在「只有一台機器、而且它是 current」時才自動選，否則要你按一台（`Resources/web/app/js/input/start.js:678`）。Linux 實作了 `start`（`LinuxDurableCloudRuntime.swift:534`）。
- **Linux Session 打字與看畫面做不到**：Linux executor 只接受 `places` 和 `start` 兩種瀏覽器指令，其他一律在 `LinuxDurableCloudRuntime.swift:674-681` 變成 `malformed_command`，而且只有 `places`／`start` 會回拒絕（`:769-775`），所以 `send`、`screen`、`transcript` 送過去**沒有任何回應**，手機等 60 秒後顯示 `cloud_read_timeout`。按鍵更糟：對不認得的機器，網頁收到 relay 的 `ack` 就當作送達（`cloud-client.js:2761-2762`），畫面說送出了，其實被丟掉。Linux 自己的原生介面其實會送字（`Packages/ClawdlineLinux/LinuxDaemonIngress.swift:927-932`），只是網頁從來不發那種格式。
- **常用句**住在 Mac 的快照裡（`Sources/OrchestratorPersistence.swift:465`），畫面只顯示目前 Session 那台的（`Resources/web/app/js/input/snippets.js:277-279`）。
- **排程**在擁有它的 Mac 上觸發（clawdline-cloud `docs/PROTOCOL.md:208-215`）。重新整理用 `Promise.all` 同時問每台機器（`cloud-client.js:2621`），一台不回整個失敗。
- **Board、Timeline、診斷、推播**在沒有指定機器時走 `_onlyMachine`，帳號裡超過一台就回 `cloud_machine_ambiguous`（`cloud-client.js:1399-1410`；呼叫點 `:1899`、`:1921`、`:1515`、`:2807`）。從 Session 或連結打開時會帶機器（`Resources/web/app/js/main.js:656-657`、`:711-715`），所以那條路正常。專案頁不帶機器呼叫 `transport.board()`（`Resources/web/app/js/view/projects.js:511`），只有 `board_` 或 503 錯誤才退回（`:534`），所以多機帳號會直接看到錯誤。
- **推播**由 Mac 直接送到 Apple／Google 的推播服務（`Sources/WebPush.swift:770-807`），金鑰與訂閱存在 Mac 的 `push.json`（`WebPush.swift:171`）。Linux 沒有推播（Linux 套件搜 webpush／vapid／notif 0 筆，計畫也明文排除，`docs/ubuntu-headless-runtime-plan.md:30`）。
- **派工**：Mac 收到 Cloud 的 `dispatch` 一律回 `cloud_dispatch_unpinned`，理由是還沒有固定的 wire shape（`Sources/CloudAppBridge.swift:2485-2495`）。relay 也不允許機器對機器發指令（`relay/src/lib/routing.ts:48-51`、`:69-73`）。

**手機分不出機器為什麼離線**：可能整台停了，也可能只是斷網、上面的 Session 還在跑。所以這份設計只有在線與離線兩種狀態，畫面上也不替離線的機器宣稱「已停止」或「已暫停」（3.4 節）。

### 2.3 正在進行、還沒落地的修正

`clawdline/task/ed5f1c24-…` 分支（head `77e7104d`，未合併）正在：用一個「這台機器實作了哪些指令」的判斷取代平台猜測、把語音那套挑機器規則推廣到所有沒指定機器的功能、讓多機讀取一台一台隔離、讓 Linux 對沒實作的指令立刻回 `unknown_command`。這份設計把它當起點，但不依賴它的細節。它**還沒處理**的一點正是這份文件要解的：**被選中的機器「不管多舊都照用」**，所以離線的 Mac 仍會讓你等到逾時。

---

## 3. 設計原則與不變的前提

### 3.1 免費版單 Mac：什麼都不變

免費版的網頁走本機 transport：`main.js` 在 `chooseTransport` 決定用本機或 Cloud（`Resources/web/app/js/main.js:270-274`），本機那條固定只有一台機器 `LOCAL_MACHINE = "this-mac"`（`Resources/web/app/js/net/client.js:9`，`Resources/web/app/js/net/live.js:30`）。

這份設計的所有網頁改動都在 `net/cloud-client.js` 與 Cloud 專用畫面；relay 的改動只影響 Cloud 連線。**免費版使用者看不到任何新東西**：沒有機器狀態點、沒有「由哪台提供」、沒有主機角色。每個階段的驗收都包含「本機頁面測試不變」，並加一條守衛證明本機頁不讀多機狀態模型。

只有一台 Mac 的 Cloud 帳號會看到一樣東西：Mac 的狀態點與「離線（最後在線 23:10）」。其他多機介面在只有一台機器時全部隱藏。

### 3.2 Cloud 只帶密文

README 的「Local first; Cloud adds reach」是承諾，不是這次要重談的東西：

- Cloud 不做需要明文的決定。挑機器、帳號資料的權威、派工仲裁，都在使用者自己的某台機器或瀏覽器上。
- 語音音訊不送任何雲端轉寫 API。
- 新增的東西若會讓 Cloud 多看到 metadata，逐項寫出來（3.3 節）。

### 3.3 對照既有決定

「遵守」表示這份設計不需要改那條；「修訂」都列成第 8 節的決策，不預設通過。

| 決定 | 這份設計 | 說明 |
|---|---|---|
| D1 全內容 E2EE | 遵守 | 能力清單、帳號資料副本都在 `ct` 密文裡。Cloud 多知道的：連線時間（本來就在公開清單上）；階段 1 的連線存活判斷只用 relay 本來就收得到的 ping 時間；階段 4 起「哪台機器送給哪台」（仍是 envelope 欄位）；選 8.3 選項二時的密文快照大小與保存時間 |
| D8 不靜默遺失；store-and-forward 延後 | 遵守 | 不需要 Cloud 端排隊，理由見下 |
| D9 Orchestration 權威在機器本地 | 精神遵守，文字需修訂 | 仲裁仍在每台機器的 broker；但「留在 Mac」要改成「留在使用者的機器」才能讓 Linux 接工作（8.9 節） |
| D10 排程維持 D1 | 遵守 | 排程仍在擁有它的機器上觸發，Cloud 沒有明文排程 |
| PROTOCOL §2 頻道 | 階段 1–3 遵守；可選修訂 | 「envelope 內容永不持久化」只有在選 8.3 節選項二時才要改 |
| PROTOCOL §4 資料面 | 階段 4 修訂 | 機器對機器指令、機器讀別台 `orch/`（8.5 節） |
| PROTOCOL §7 排程 | 遵守 | 加上合併檢視與「這台離線，可能不會觸發」的標示 |
| PROTOCOL §11 跨機編排 | 遵守並補完 | §11 寫的「另一台 Mac 派工」在 relay v0.4 做不到，要靠 §4 修訂 |

**為什麼 D8 的 store-and-forward 仍然不需要。** 這份設計裡四個「對方離線」的情境都不必讓 Cloud 替你排隊：

1. **讀帳號資料**：要的是「最後狀態」，不是「指令佇列」，屬於 §2 的快照語意；第一步由瀏覽器自己保存（4.6 節）。
2. **主機離線時編輯**：編輯存在這支手機的草稿裡，主機回到在線再送，Cloud 不經手。
3. **子任務完成時 root 的機器離線**：結果由執行的機器保管，root 的機器回來再拉（5.8 節）。
4. **語音**：挑在線的機器；釘選的離線就在送出前說清楚。

只有一種需求真的要佇列：「把工作指定給一台離線的機器，等它回來自己開始」。那是 D8 所說的市場缺口，這份設計不做，留給以後單獨決定。D18 的 webhook outbox（clawdline-cloud `docs/DECISIONS.md:180-199`）證明 Cloud API 可以安全地保管不透明的意圖，真要做時是現成的形狀。

### 3.4 設計原則

1. **說出來，不要讓人等。** 機器狀態在按下按鈕之前就顯示；確定送不到的東西在送出前就拒絕，並附 typed error。
2. **跑中的東西不搬。** Session 釘在它的機器上；任務一旦被某台 broker 接受就留在那台。
3. **自動可以，靜默換手不行。** 釘選的機器離線時要問或要說，不能偷偷換成另一台。
4. **每份資料只有一個寫入者。** 真正屬於帳號、可編輯的小清單有一台主機；Board、排程、任務這些「某台機器上發生的事實」由各自的機器擁有，手機端彙整。
5. **免費版的路徑不因為多機變複雜。**
6. **只處理離線。** Clawdline 不區分離線的原因，也不負責讓機器保持在線。睡眠、斷網、關機、當機，在手機端都是同一件事：那台機器不回應。要不要讓機器一直在線、怎麼做，是使用者自己的設定。

---

## 4. 機器與能力模型

### 4.1 三層

- **機器**：身分與狀態。身分是 relay 認證過的 `machine_id`；名字只是顯示用（`Sources/OrchestratorPersistence.swift:481-489` 明寫名字不能拿來路由）。
- **能力**：這台機器能做什麼，由機器自己宣告。
- **帳號資料**：不應該跟著一台機器離線就消失的小清單。

### 4.2 狀態怎麼判定

手機對每台機器只算一個可達性狀態：**在線**或**離線**。「未配對」是授權問題，和可達性分開顯示。

| 狀態 | 什麼時候 | 畫面 |
|---|---|---|
| 在線 | 最近一次訊息在新鮮度內，而且之後沒有離線的證據 | 綠點 |
| 離線 | relay 對它回了 `machine_offline`、送前探測失敗，或最近一次訊息超過新鮮度 | 空心點＋「離線（最後在線 23:10）」 |
| 未配對 | 這個瀏覽器沒和它配對 | 鎖頭＋配對說明 |

瀏覽器剛連上的 60 秒內，還沒收到快照的機器顯示轉圈，不先判成離線（沿用 `MACHINE_INVENTORY_SYNC_MS`，`cloud-client.js:52`）。

規則細節：

- **不猜原因。** 離線的畫面只寫最後在線時間，不寫可能的原因，也不宣稱上面的 Session 已經停止。
- **新的證據勝過舊的。** relay 回 `machine_offline` 或探測失敗，勝過還在新鮮度內的舊訊息；之後只要再收到那台的任何訊息，就回到在線。機器重新連上時會重發 `orch/` 快照（`docs/cloud.md:333-338`），所以不必等新鮮度窗口。
- **狀態點要自己變。** 新鮮度是時間的函數，網頁要定時重算（例如每 15 秒），不能等下一次操作才發現某台已經過期。今天只在呼叫時算一次（`cloud-client.js:1262-1263`）。
- **在線但久沒消息就先探測。** 最近一次訊息超過 60 秒、還在新鮮度內的機器仍算在線，但要用它之前先送 10 秒的 `cloud.status` 探測（4.3 節）。

### 4.3 離線怎麼知道、多快

三個訊號，都和離線的原因無關：

| 訊號 | 涵蓋的情況 | 從離線到可判定 | 手機什麼時候看到 |
|---|---|---|---|
| relay 收到 socket 關閉 | 機器那頭的程式結束：結束 App、程序被殺或當掉，作業系統會替它關掉 TCP 連線 | relay 一收到關閉就回應關閉（`account-do.ts:1033-1042`），之後不再算成連線；預期是秒級，實際延遲由 E4 量 | 下一個送往那台的指令（包括探測），`ack` 就是 `machine_offline` |
| relay 判斷半開連線（階段 1） | 沒有人送出關閉：斷網、斷電、整台停住 | ping 每 30 秒一次、門檻 75 秒；離線發生在兩次 ping 之間，所以是離線後 **45–75 秒** | 同上，下一個指令 |
| 網頁新鮮度窗口 | 全部 | 在線的機器最多約 4 分鐘發一次訊息（Mac 換 token 時重發快照、Linux 240 秒重發描述），窗口 5 分鐘，所以是離線後 **1–5 分鐘** | 不必送指令，網頁重算時 |

**手機最先看到哪一個**：relay 不會主動推送離線（2.1 節），所以 relay 的兩個訊號都要手機送出指令才看得到。

- **手機正要用那台機器時**（按麥克風、開 Session、送字、派工），最先看到的是 relay：程式結束的情況立刻知道，半開連線在離線後 45–75 秒知道。為了不讓真正的請求去撞半開連線，最近一次訊息超過 60 秒時先送 10 秒的 `cloud.status` 探測（今天已有 `STATUS_PROBE_TIMEOUT_MS`，`cloud-client.js:123`，用在讀取逾時之後）：relay 已經判定就立刻失敗；relay 還沒判定的半開連線，10 秒沒回應也算失敗。所以沒有階段 1 的 relay 改動時，探測是最快的訊號，10 秒。
- **手機只是開著、什麼都沒送時**，最先看到的是新鮮度窗口：離線後 1–5 分鐘，加上一次重算間隔。

**relay 怎麼判斷半開連線（階段 1）。** Mac 每 30 秒送一次應用層 ping `{"type":"ping"}`（`Sources/CloudTransport.swift:1644`、`:2482-2498`、`:2500-2512`）。Linux executor 用同一個 `CloudTransport.production`，沒有改間隔（`Sources/CloudTransport.swift:1614-1629`、`Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:158-161`），所以也是 30 秒。relay 對這個固定字串用 Cloudflare 的自動回覆回 pong，不喚醒 Durable Object（`account-do.ts:181-182`、`:336`）。Cloudflare 提供 `getWebSocketAutoResponseTimestamp`，可以查到每條 socket 最後一次自動回覆的時間。relay 在路由指令時，把最後一次自動回覆超過 75 秒（30 秒的 2.5 倍）的機器 socket 當作不在，回 `machine_offline` 並關掉它。這不改 `machine_offline` 的定義（「目前沒有連線」），只是讓 relay 不再把死掉的 socket 算成連線；它是一條新的收窄規則，要補進 relay 的 narrowing 清單。

### 4.4 能力怎麼宣告

每台機器在自己的描述裡列出能力。ed5f1c24 分支正在加 `commands`（線上的指令字，例如 Linux 的 `["places", "start"]`），那是**路由**用的；這裡再加一層給**人**看的能力：

| 能力 id | 意思 | 今天誰有 |
|---|---|---|
| `sessions.run` | 能開、能跑 Session | Mac、Linux |
| `sessions.interact` | 能打字、看畫面、讀訊息 | Mac |
| `voice.transcribe` | 有可用的 Whisper | 裝了 Whisper 的 Mac |
| `snippets` | 常用句 | Mac |
| `schedules` | 能觸發排程 | Mac |
| `board` | Board 與 Timeline | Mac |
| `push.send` | 能發 Web Push | Mac |
| `dispatch.accept` | broker 能接派工 | 目前沒有（Cloud 派工被拒） |

- 能力由機器依自己的實際狀況宣告。例如 Whisper 沒裝好時不宣告 `voice.transcribe`，手機就不會送音訊過去再收到 `no_whisper`（`Sources/CloudLocalRoute.swift:303-311`）。
- 宣告在 `orch/<machine>` 快照裡，是密文；Cloud 看不到誰有什麼能力。
- 舊版機器沒有這個欄位時，沿用 ed5f1c24 的判斷（指令清單、平台、`cloud_status`）。

### 4.5 誰負責什麼：自動或釘選

每個「沒指定機器」的功能都有一個提供者規則：

- **自動**：一份偏好順序，挑第一台**在線且有這個能力**的。偏好順序預設是「上次用過的在前」。
- **釘選**：指定一台。釘選的那台離線時，**在送出前**回 typed error，附上「這次改用 X」的一鍵選項；**不會偷偷改用別台**（8.6 節）。

這解掉 ed5f1c24 留下的矛盾：今天「使用者選的機器不管多舊都照用」（`cloud-client.js:1312`）。改成：選擇仍然有效，但**離線**不再用「等逾時」表達，而是立刻說出來。離線是從新鮮度判斷、relay 還沒確認的，拒絕畫面上可以按「重新檢查」送一次 10 秒探測，有回應就回到在線並照送。

`cloud_machine_offline` 這類「（新）」code 是網頁依狀態模型在送出前拒絕；`machine_offline` 是 relay 對已經送出的指令回的。兩者都表示離線，前者多帶最後在線時間與可改用的機器。

偏好存哪裡：

- **語音**：沿用今天的做法，存在瀏覽器（`cloud-client.js:54-56`），手機和筆電可以用不同的 Mac 轉寫（`docs/whisper.md:266-267`）。
- **派工、帳號資料主機**：必須每個派工方都一致，存在帳號資料裡（4.6 節）。

### 4.6 帳號資料與主機

**問題**：常用句這種清單，你希望每台機器、每個 Session 都一樣，而且不會因為某台 Mac 離線就消失。

**哪些算帳號資料**（範圍刻意收窄）：

- 常用句
- 各功能的偏好順序與釘選
- 派工的專案釘選
- 專案身分的手動覆寫（5.2 節）

**哪些不算**：Board、Timeline、排程、任務、Session。它們是「某台機器上發生的事」，由那台擁有，手機彙整成一個畫面。這和 PROTOCOL §11「fleet dashboard 是客戶端彙整每台機器的 `orch/` 鏡像」一致（clawdline-cloud `docs/PROTOCOL.md:271-274`）。

**主機**：帳號資料只有一個寫入者，是你的某一台機器，稱為「帳號資料主機」。其他機器與瀏覽器持有加密副本。

- **主機在線**：編輯送到主機，主機發新版本，其他人更新副本。
- **主機離線**：手機顯示最後版本並標時間（「mac-mini 離線，這是 23:10 的版本」）；編輯存成這支手機上的草稿，標「等 mac-mini 回到在線再送出」，回來後帶版本號送出，衝突時列出兩版讓你選。
- **換主機**：預設手動移交（舊主機在線時交棒）；舊主機一直不回來時可強制接手，舊主機回來發現版本號比自己新，就自動變成副本，並把它離線期間沒同步的編輯列成衝突（8.4 節）。

**最後版本存在哪裡**（8.3 節）：

- **選項一（建議，第一步）：瀏覽器自己保存。** 每台機器最後一份 `orch/` 快照存在瀏覽器（今天已經有機器描述的本機快取 `MACHINE_DESCRIPTOR_CACHE`，`cloud-client.js:53`）。Cloud 不用改；缺點是換一支新手機、或清掉網站資料時，主機離線就看不到。
- **選項二：Cloud 另存加密的最後快照。** relay 今天只在記憶體保存最後一份，Durable Object 休眠被清掉就沒了（`relay/src/lib/cache.ts:5-8`）。改成持久化要修訂 PROTOCOL §2「envelope 內容永不持久化」。Cloud 會多保存：密文本身、頻道名稱、大小、更新時間與保留期限；看不到內容。藍圖本來就設想過「雲端那份加密 history vault」（clawdline-cloud `docs/2026-08-25-cloud-blueprint.html:332`），方向相容。

**機器之間怎麼同步副本**：relay 今天規定機器只收 `ctl` 與 `ho`，不收別台的 `orch/`（`relay/src/lib/routing.ts:159-163`），機器也不能發 `ctl`（`:69-73`）。所以機器間同步需要 8.5 節的決定；在那之前，副本只存在瀏覽器，而主機以外的機器用不到帳號資料。

---

## 5. 逐項設計

每一項都照同一個順序寫：在線／離線時的行為、畫面、資料權威與 Cloud 看得到什麼、失敗時的 typed error。標「（新）」的 error code 是提案。

### 5.1 語音輸入

**解決什麼**：開口前就知道這次會不會成功、由誰轉寫；轉寫的那台離線時不用等 6 分鐘。

**轉寫引擎順序**：

1. 釘選或偏好順序中，第一台**在線**且有 `voice.transcribe` 的 Mac。
2. 另一台在線、有 Whisper 的 Mac。
3. 以後若 Linux 裝了 Whisper，同樣條件的 Linux（今天的 Linux 計畫明文排除語音，`docs/ubuntu-headless-runtime-plan.md:30`）。
4. **沒有在線的轉寫機器時，不錄音、直接說**，並提示「可以用鍵盤上的聽寫」。

**手機內建辨識為什麼不當預設退路**（和討論方向不同，詳見 8.2 節）：

- **產品已經回答過這題。** `docs/whisper.md:230-235` 寫明：手機本來就有辨識器，代價是句子交給寫它的人，而 Clawdline 的回答是不用。
- **iOS 主畫面 app 裡用不了。** Safari 分頁從 iOS 14.5 起有 `webkitSpeechRecognition`，但 WebKit bug 225298 裡 Apple 工程師明寫「SafariViewController 與加到主畫面的 web app 目前不能用」，狀態 RESOLVED LATER，之後沒有修好的紀錄。你在手機上用的正是主畫面 app。
- **繁中音訊很可能送 Apple。** WebKit 只在語言支援裝置端辨識時才要求裝置端處理；Apple 的功能供應表裡「裝置端聽寫」清單有中國大陸華語、**沒有台灣華語**。網頁無法指定也無法得知走哪條。
- **Chrome 預設送 Google。** 只有桌面版 Chrome 139 起能要求 `processLocally`，Android 沒有。
- **鍵盤聽寫是系統的功能，不是我們送的。** Apple 說「能打字的地方就能聽寫」；主畫面 app 的輸入框理論上也可以，但 Apple 沒有明文寫（實驗 E1）。提示它，是把「音訊交給 Apple」這個選擇留給使用者自己做。

**在線／離線**：

- **轉寫機在線**：照今天的流程，送 16 kHz PCM 到那台（`docs/whisper.md:258-267`）。若那台最後一則訊息已超過 60 秒（可能是半開連線），按下麥克風的同時送一個 10 秒的 `cloud.status` 探測，**錄音不必等它**；停止錄音時探測已經失敗，那台就算離線，直接給「改用」選項，不送出那個可能等 6 分鐘的請求。
- **轉寫機離線**：自動模式改用下一台在線的並寫出來；釘選模式在錄音前就顯示，錄完不送，給「這次改用 MacBook」一鍵。relay 已經回過 `machine_offline` 就不再探測；只是超過新鮮度、relay 還沒確認時，錄音的同時探測 10 秒，有回應就回到在線並照送。

**畫面**（手機寬度，麥克風上方一行小字）：

```
Whisper · mac-mini
```

```
mac-mini 離線 → 改用 MacBook
```

```
釘選的 mac-mini 離線（最後在線 23:10）
[這次改用 MacBook]
```

```
沒有在線的 Mac 能轉寫
可以用鍵盤上的聽寫
```

**資料權威與 Cloud**：音訊只以密文送到被選中的那台，Cloud 看到的是一個送往 `ctl/<machine>` 的較大 envelope，和今天一樣。偏好存在瀏覽器。

**Typed error**：

- `cloud_voice_host_offline`（新）：釘選的轉寫機離線，附 `last_seen_at` 與可改用的 `candidates`。
- `cloud_voice_host_unavailable`：沒有能轉寫的機器（今天已有，`cloud-client.js:1353-1355`）。
- `cloud_voice_host_ambiguous`：今天有，改用偏好順序後應該不再出現，保留給「兩台都在線、都沒有偏好」。
- `machine_offline`：relay 說不在。
- `no_whisper`：Mac 沒裝好（`Sources/CloudLocalRoute.swift:303-311`），宣告能力後只剩競態時才會出現。
- `cloud_read_timeout`：真的沒有回應。

### 5.2 專案

**解決什麼**：同一個 repo 在兩台機器上各有一份 checkout，手機上應該是「一個專案、兩台可以做」，而不是兩列不相干的資料夾；而且要提醒「只有這台有的 commit」。

**專案身分**：

- 用正規化的 git remote。Mac 已經有現成的：`ProjectTimelineRemote.repositoryID` 把 GitHub 的 remote 正規化成 `github:owner/repo`（`Sources/ProjectTimelineModel.swift:106-123`）；沒有 GitHub remote 時退回 `local:<路徑雜湊>`（`Sources/ProjectTimelineGitImporter.swift:142-149`），也就是每台各自一個。
- 提案：推廣到任何 host（`git:<host>/<owner>/<repo>`），沒有 remote 的專案**永遠不自動合併**。
- 好處是 Timeline 已經用 `repositoryID + ":" + commit.sha` 當 key（`ProjectTimelineGitImporter.swift:82`），兩台機器匯入同一個 repo 時天然去重。

**`places` 回應要多帶的欄位**（都在密文裡）：`repository`、`branch`、`ahead`（還沒 push 的 commit 數）、`dirty`、`last_used_at`。

**在線／離線**：

- 每台機器各自回報自己的 checkout；手機以 `repository` 合併。
- 某台離線時，它的徽章變灰，資料是最後一次看到的並標時間。
- 開新 Session 預選「上次在這個專案用過、而且在線」的那台，並寫出來；若那台離線，改選下一台並說原因。
- **未 push 警告**：只有一台的 `ahead > 0`、而其他台也有這個專案時，列上顯示「mac-mini 有 3 個還沒 push 的 commit」。那台離線時標「（23:10 的資料）」。

**畫面**（專案頁一列）：

```
clawdline
● mac-mini   ○ linux-aws
mac-mini 有 3 個未 push
[開 Session：mac-mini]
```

- `●` 在線、`○` 離線；按徽章可以改機器。
- Session 列今天已經有機器標籤（`Resources/web/app/js/view/list.js:546-550`），專案列今天沒有。

**資料權威與 Cloud**：每台機器是自己 checkout 的權威；合併在手機上做。手動合併或拆開兩個專案的覆寫是帳號資料（4.6 節）。Cloud 看不到專案名稱或 remote。

**Typed error**：

- `project_checkout_missing`（新）：指定的機器沒有這個專案。
- `cloud_machine_offline`（新）：指定的機器離線，附 `last_seen_at`。
- `place_not_found`：今天 Linux 已有（`LinuxDurableCloudRuntime.swift:682-684`）。
- `cloud_machine_unsupported`：ed5f1c24 分支加的，機器沒有這個能力。

### 5.3 Session

**解決什麼**：每個 Session 都清楚是哪台機器的；那台離線時看得到最後狀態，而不是點下去等 60 秒。

**原則**：Session 釘在它的機器上，**不做 Session 搬移**。要換機器繼續，走 handoff（PROTOCOL §6 設計的跨機 handoff）；這份設計不把它排進階段。

**在線／離線**：

- **在線**：照常。Linux Session 在階段 2 補上打字、按鍵、看畫面、讀訊息。
- **離線**：列表保留最後狀態，整列變灰並標「mac-mini 離線 · 最後在線 23:10」；輸入框停用，寫出原因；點進去顯示最後一次讀到的內容（若這支手機讀過）。畫面不說 Session 停了，因為那台可能只是斷網，Session 還在跑。
- **開新 Session**：見 5.2 節的預選規則。

**畫面**：Session 列沿用今天的機器標籤，加狀態點。多機帳號可以依機器分組，也可以依專案分組（沿用專案身分）。

**資料權威與 Cloud**：Session 狀態的權威是它的機器（`s/<machine>/<session>` 快照）；Cloud 看到頻道名稱與大小，和今天一樣。

**Typed error**：

- `cloud_machine_offline`（新）：對離線的機器操作，送出前就拒絕。
- `cloud_machine_unsupported`：機器沒有 `sessions.interact`。**階段 2 之前**，這要取代今天 Linux 的「沒有回應」，按鍵也不能再顯示「已送出」。
- `machine_offline`、`cloud_read_timeout`：保底。

### 5.4 常用句與排程

**常用句——解決什麼**：每台機器、每個 Session 都用同一份，而且主機離線時還看得到。

- **在線**：常用句清單由帳號資料主機擁有。插入常用句是瀏覽器把文字放進輸入框，所以 Linux Session 也能用，Linux 不必自己實作常用句。
- **主機離線**：顯示最後版本並標時間；新增或修改存成草稿（4.6 節）。
- **畫面**：常用句面板頂端一行「來自 mac-mini · 23:10」；草稿有「等 mac-mini 回到在線再送出」標籤。
- **遷移**：今天每台 Mac 各有一份（`Sources/Snippets.swift:8`）。第一次設定主機時，把各台的清單合併去重，讓你確認一次。
- **Cloud**：只看到密文。
- **Typed error**：`cloud_account_hub_offline`（新，編輯被存成草稿時的狀態，不算失敗）、`account_data_conflict`（新，回到在線送出時版本衝突）。

**排程——解決什麼**：看得到帳號裡所有排程，而且知道哪些可能因為機器離線而不會觸發。

- **原則不變**：排程在擁有它的機器上觸發，app 關著就不觸發（PROTOCOL §7、D10）。
- **在線**：合併列表，每筆標機器。
- **擁有者離線**：該筆標「可能不會觸發：mac-mini 離線（最後在線 23:10）」。手機分不出那台是整台停了，還是只是斷網、排程照樣在跑，所以只說「可能」。那段時間若真的沒跑，機器回來後依既有的補跑規則處理（`catch_up_hours` 預設 6，`docs/schedules.md:95`）。
- **畫面**：列表每筆左邊一個機器徽章與狀態點，擁有者離線的整筆變灰；上方可依機器篩選。
- **一台不回不能拖垮全部**：今天的 `Promise.all`（`cloud-client.js:2621`）改成逐台隔離，ed5f1c24 分支正在做。
- **搬排程到別台**：明確操作「在 linux-aws 建一份、停用 mac-mini 那份」。Linux 今天沒有排程引擎，所以要等 Linux 有 `schedules` 能力才開放。
- **Cloud**：只看到密文；D18 的 webhook 另有規則，不受影響。
- **Typed error**：`schedule_owner_offline`（新，對離線機器上的排程按「立刻執行」）、`cloud_machine_unsupported`（目標機器沒有排程能力）。

### 5.5 Board 與 Timeline

**解決什麼**：多機帳號不再看到「有不只一台機器，所以什麼都沒送」。

- **權威**：Board 是各機器 broker 事實的投影（`Sources/ProjectBoardStore.swift:10-12`），Timeline 是「Mac 擁有的 append-only 權威」（`Sources/ProjectTimelineStore.swift:4`）。兩者都留在各自的機器上。
- **在線**：手機以專案身分合併各台的 Board 與 Timeline；每張卡、每筆紀錄標來源機器。Timeline 靠 `repositoryID:sha` 去重（5.2 節）。
- **離線**：該台的卡片保留最後狀態並變灰，標最後在線時間；對它的 `board-command` 在送出前拒絕。
- **畫面**：卡片右上角一個小機器徽章；專案頁頂端一行「資料來自 mac-mini（在線）、linux-aws（離線，23:10 的快照）」。
- **沒指定機器時**：不再用 `_onlyMachine`（`cloud-client.js:1399-1410`）拒絕，而是合併所有有 `board` 能力、在線的機器；離線的用最後快照補上並標時間。
- **Cloud**：只看到密文。
- **Typed error**：`cloud_machine_offline`（新，寫入離線機器上的卡片）、`cloud_machine_unsupported`、`machine_offline`。

### 5.6 推播

**解決什麼**：不管哪台機器上的 Session 在等你，手機都收得到；Mac 離線時，Linux 上的事也照推。

**先說一個限制**：瀏覽器的一個 service worker 註冊**只能有一組推播訂閱**，而且綁一把 `applicationServerKey`；用不同的 key 再訂閱會被拒絕（Push API 規格 §3.4.1 與 `subscribe()` 步驟 11.6.3）。所以「每台機器各有一把 VAPID 金鑰、手機分別訂閱」**做不到**。今天 Mac 每台各自產生一把隨機金鑰（`Sources/WebPush.swift:246-258`），這正是 `_onlyMachine("notifications")` 必須拒絕多機帳號的根本原因。

**提案**：

- **帳號共用一把 VAPID 金鑰**，由每台能推播的機器從 account master secret 用 HKDF 衍生（固定標籤，隨 `key_id` 輪替）。每台機器算出同一把，不需要在機器之間傳私鑰，也不需要主機。
- 瀏覽器照舊只訂閱一次，然後把**同一個訂閱登記到每台有 `push.send` 的機器**。
- 每台只推**自己的**事件（自己的 Session 在等、自己的派工完成）。同一件事用推播 `topic` 去重。
- Linux 實作 Web Push（RFC 8291 加密＋VAPID 簽章）。

**在線／離線**：在線的機器照推自己的事件。離線的機器推不推得出去，手機不依賴：它若只是連不到 relay、還連得到推播服務，仍會推；整台停了就不會。**Cloud 不代發推播**：藍圖寫明 Web Push 由機器直送、不經雲端（clawdline-cloud `docs/2026-08-25-cloud-blueprint.html:342`）；讓 Cloud 代發「機器離線」需要 Cloud 知道推播端點，這是新的 metadata，不建議。

**畫面**：通知設定頁列出「會推播的機器：mac-mini、linux-aws」，離線的標灰；訂閱登記失敗的機器寫出原因。

**資料權威與 Cloud**：訂閱清單每台機器各存一份；Cloud 什麼都沒多看到，推播服務（Apple／Google）看到的和今天一樣。

**遷移代價**：從隨機金鑰換成衍生金鑰，所有既有訂閱要重新訂閱一次（`WebPush.swift:253-254` 已經寫明換金鑰會讓訂閱全部失效）。被撤銷、但曾拿過 master secret 的裝置能算出這把金鑰；若它也錄下了過去的密文，就可能拿到訂閱端點並推送假通知。這和藍圖寫的撤銷語意（撤銷後仍解得開舊密文）是同一類風險，所以撤銷裝置時要輪替 `key_id`，金鑰與訂閱一起換新。

**Typed error**：

- `push_machine_unregistered`（新）：某台機器沒有登記到這個訂閱，附機器與原因。
- `push_key_mismatch`（新）：瀏覽器的訂閱綁的是舊金鑰，需要重新訂閱。
- 今天的 `push_timeout` 系列不變。

### 5.7 Devices 頁

**解決什麼**：一眼看出每台機器是誰、在不在線、能做什麼、擔任什麼角色，並在這裡改設定。

**一張機器卡**（手機寬度，由上到下）：

```
mac-mini  Mac · a1b2
○ 離線（最後在線 23:10）
Session 語音 常用句 推播
角色：語音輸入、帳號資料主機
[重新檢查] [設為語音輸入]
```

- **名字與識別**：名字＋平台＋短 id（名字相同時才顯示短 id，沿用今天的做法）。
- **狀態**：4.2 節的在線、離線、未配對之一；離線加最後在線時間。今天 Devices 只依 5 分鐘新鮮度分成三種：在線、離線或過期、未知（`Resources/web/app/js/view/devices.js:39-40`）。
- **重新檢查**：離線的機器才有，送一次 10 秒探測（4.3 節）。
- **能力**：4.4 節的能力，用短標籤；沒有的不顯示。
- **角色**：語音輸入（今天已有，`devices.js:136-142`）、帳號資料主機、各功能的釘選。
- **排序**：在線的在前，然後離線、未配對。
- **偏好順序**：在「設定 › 多台機器」裡，每個能力一個清單，可以上下移動或釘選。

**資料權威與 Cloud**：卡片資料都來自各台機器的密文快照；偏好來自瀏覽器或帳號資料。

**Typed error**：沿用各功能自己的 code；設定主機時若新主機離線，回 `cloud_machine_offline` 或 `machine_offline`。

### 5.8 Orchestrator、派工與落地

**解決什麼**：從手機或任一台機器派出的新工作，自動落在在線、合適的機器上；某台離線時，別台照常工作；子任務的結果不因為 root 的機器離線而遺失。

**今天**：

- broker 只在 Mac 上（`Sources/OrchestratorStore.swift`、`Sources/Orchestrator.swift`），任務清單經 `orch/` 快照鏡像到手機（`Sources/OrchestratorPersistence.swift:471-473`）。
- Cloud 派工被拒：`cloud_dispatch_unpinned`（`Sources/CloudAppBridge.swift:2485-2495`）。
- relay 沒有機器對機器的指令通道（`relay/src/lib/routing.ts:48-51`）。
- Linux 沒有 broker（Linux 套件搜 orchestrat／broker 都是 0 筆）。

**設計**：

1. **門留在每台機器（D9）。** 每台有 `dispatch.accept` 的機器跑自己的 broker；claims、serialize、depth、capacity、dispatch policy 照本機規則檢查，回同樣的 typed error：`workspace_busy`、`over_capacity`、`depth_exceeded`、`bad_task`（clawdline-cloud `docs/PROTOCOL.md:263-270`）。
2. **挑機器是派工方的事，每次派工重算。** 候選：有 `dispatch.accept`、在線、有這個專案的 checkout、專案有兩邊都到得了的 remote、快照顯示有容量。排序依序是：這個 graph 的釘選 → 這個專案的釘選 → 和 root 同一台 → 上次在這個專案用過 → 最閒。被挑中的 broker 仍可以拒絕；拒絕就換下一台，試完候選為止。以 task id 當冪等鍵，不會重複開 Session。
3. **沒有固定的協調者。** 派工方（手機，或 root 所在的機器）本來就看得到整個 fleet 的 `orch/` 鏡像，就地排序即可；最終仲裁在每台 broker。和討論方向「由一台協調者機器看全局」不同，理由是協調者本身會離線，成為所有派工的單點；而 broker 已經是仲裁者，多一層協調者沒有增加正確性。
4. **跑中的工作不搬。** 那台離線時，任務顯示「mac-mini 離線（最後在線 23:10）」，不寫「暫停」：那台可能只是斷網、任務還在跑，也可能整台停了。機器離線超過 `timeout_minutes` 又回來時，broker 怎麼算逾時，目前行為未知（實驗 E7）。
5. **跨機子任務的結果**：
   - 執行機把 `result.json` 耐久保存。
   - 先試著送回 root 的機器；對方 `machine_offline` 就留在自己的 outbox。
   - root 的機器回到在線後主動向各台拉「我派出去的任務有沒有結果」。
   - 以 task id＋結果雜湊冪等（今天的完成檔流程已經算 `result_sha256`）。
   - 不需要 Cloud 排隊（D8）。
6. **交付與落地**：
   - 子任務在執行機的 worktree 上 commit，推到 `clawdline/task/<id>` 分支。
   - root 所在的機器 fetch 後照 `docs/landing.md` 流程落地，落地仍歸 root。
   - base commit 必須在 remote 上。不在時（例如 root 那台還沒 push），派工在送出前就拒絕，避免子任務在錯的基底上工作。
7. **Linux 接工作**：Linux 需要一個精簡版 broker：接 task.json、在 tmux 開 Session、寫結果、推分支。這需要修訂 D9 的文字（8.9 節）。
8. **從手機派工**：先固定 Cloud 派工的 wire shape：task.json、task id、secret 怎麼放在密文 payload 裡、檔案寫在執行機的哪裡，才能拿掉 `cloud_dispatch_unpinned`。

**Q3 的直接回答**：用哪台＝派工當下在線、有專案、有容量、依釘選與使用紀錄排第一的那台。可以動態調整：每次派工都重算；你隨時可以改專案或 graph 的釘選。已經接受的工作不會因為調整而搬家。

**在線／離線**：

- **root 的機器離線**：已經派到別台的子任務照跑；結果等它回來。root Session 本身可能還在跑（只是斷網），也可能停了。
- **子任務的機器離線**：root 看到「mac-mini 離線」；不會自動在別台重跑，因為 worktree 與 claims 在那台上，而且它可能還在跑。
- **手機派工時目標離線**：挑下一台；釘選的那台離線則送出前拒絕，附「這次改用」選項。

**畫面**：派工確認列寫「將在 linux-aws 執行（mac-mini 離線；linux-aws 有此專案且閒置）」，可以點開換機器；任務列加機器徽章，執行機離線時標「mac-mini 離線」。

**資料權威與 Cloud**：任務狀態的權威是執行它的 broker；task.json、claims、結果都是密文。Cloud 看到 `class=dispatch` 的 envelope 數（今天已在公開的 metadata 清單上，clawdline-cloud `docs/PROTOCOL.md:40-43`），以及機器對機器的 envelope 流向（階段 4 新增：哪台送給哪台、多大、何時）。

**Typed error**：

- 既有：`workspace_busy`、`over_capacity`、`depth_exceeded`、`bad_task`、`machine_offline`、`cloud_dispatch_unpinned`（階段 5 之前）。
- `dispatch_no_capable_machine`（新）：沒有在線、有專案、有容量的機器，附每台被排除的原因。
- `dispatch_remote_required`（新）：專案沒有兩台都到得了的 remote。
- `dispatch_base_unreachable`（新）：base commit 不在 remote 上。
- `cloud_machine_offline`（新）：釘選的執行機離線。
- `dispatch_result_pending`（新，狀態不是失敗）：結果在執行機上，等 root 的機器回到在線。

---

## 6. 分階段計畫

大小是粗估：**S** 半個 session 以內；**M** 一個完整的 feature session；**L** 兩到三個 session 加一次獨立複審；**XL** 多個 feature 加一次 relay 部署。API（控制面）在所有階段都不用改。

### 階段 1：離線感知與快速換手

- **範圍**：
  - relay：路由指令時判斷半開連線（4.3 節），最後一次 ping 自動回覆超過 75 秒的機器 socket 回 `machine_offline` 並關閉；補進 relay 的 narrowing 清單。
  - 網頁的在線／離線狀態模型（4.2 節），定時重算；最近一次訊息超過 60 秒的機器，用之前先探測。
  - 語音與（ed5f1c24 落地後的）通用挑機器規則跳過離線的機器；釘選的機器離線時送出前拒絕，附「這次改用 X」一鍵。
  - 離線機器的 Session、常用句、排程、Board 顯示最後已知狀態，唯讀。
  - 麥克風寫出這次由哪台轉寫。
  - 瀏覽器保存每台機器的最後快照（8.3 節選項一）。
- **動到**：relay（一次部署）、網頁。Mac app 與 Linux executor 不用改，兩者本來就每 30 秒 ping。
- **大小**：網頁 M、relay S–M，合計約 L。比原本分開的兩段少了整塊 Mac 工作。relay 改的是所有指令的路由判斷，所以兩塊合起來做一次獨立複審。
- **可以分開上線的部分**：
  - **relay 那塊可以單獨先上。** 網頁今天收到 `machine_offline` 就立刻報錯（`cloud-client.js:2405-2406`），所以只上 relay，半開連線上的指令就從「等 60 秒或 6 分鐘」變成「離線 45–75 秒後立刻被拒」，舊網頁不用改。
  - **網頁那塊也可以單獨上。** 沒有 relay 改動時，半開連線靠 10 秒探測與 5 分鐘新鮮度窗口發現；慢一點，但不再等 6 分鐘。
  - 通用挑機器規則要等 ed5f1c24 落地；語音部分不用等。
- **驗收**（每種故障各注入一次，Mac 與 Linux 各一輪）：
  - **結束 App**、**殺掉程序**：手機下一個指令（或探測）立刻得到 `machine_offline`，狀態點變成離線。
  - **切斷機器的網路**（程序還在）：有 relay 改動時，下一個指令在 90 秒內得到 `machine_offline`；只有網頁改動時，探測在 10 秒內失敗並給改用選項；手機什麼都不送時，狀態點在 5 分鐘加一次重算間隔內變成離線。
  - **半開連線**（relay 測試裡讓 client 停止 ping 但不關 socket）：超過 75 秒後的指令得到 `machine_offline`、socket 被關閉；75 秒內的照常 `delivered`；測試證明 ping 仍不喚醒休眠中的 Durable Object。
  - 釘選的機器離線時，按麥克風前就顯示，送出在 2 秒內被拒，不再等 6 分鐘。
  - 機器重新連上 relay 後，手機收到它的第一份快照就回到在線，不等新鮮度窗口。
  - 免費版本機頁面的測試不變；新守衛證明本機頁不讀多機狀態模型。
- **依賴**：relay 那塊先做實驗 E9；E4 的量測當網頁那塊的基準。
- **可否回退**：可以。relay 回滾版本就回到今天的判斷；網頁狀態模型可以整段 revert。

### 階段 2：Linux Session 能用、專案一列

- **範圍**：
  - Linux 實作 `send`、`answer`／`key`、`screen`、`transcript`（原生介面已有送字與觀察）。
  - Mac 與 Linux 的 `places` 多帶 `repository`、`branch`、`ahead`、`dirty`、`last_used_at`；專案身分推廣到非 GitHub remote（8.7 節）。
  - 網頁的專案一列、機器徽章、開 Session 預選、未 push 警告。
- **動到**：Linux executor、Mac app、網頁。
- **大小**：L。
- **驗收**：
  - Mac 離線時（結束 App 或切斷網路），手機能在 Linux Session 打字、按鍵、看到畫面與訊息。
  - 對不支援的機器按鍵不再顯示「已送出」。
  - 同一個 GitHub repo 在兩台機器上只出現一列。
  - 未 push 警告在 fixture 上紅綠各證一次。
- **依賴**：ed5f1c24（Linux 對未實作的指令明確拒絕）。
- **可否回退**：可以，Linux 從描述拿掉某個指令，就恢復「明確拒絕」。

### 階段 3：推播不綁一台 Mac

- **範圍**：帳號共用的衍生 VAPID 金鑰；瀏覽器把訂閱登記到每台能推播的機器；Linux 實作 Web Push；移除 `_onlyMachine("notifications")`。
- **動到**：Mac app、Linux executor、網頁。
- **大小**：L。
- **驗收**：
  - Mac 離線時，Linux Session 等待輸入會推到 iPhone 主畫面 app。
  - 金鑰更換後，舊訂閱會被偵測並重新訂閱。
  - 兩台同時推同一件事，手機只出現一則。
- **依賴**：階段 2（Linux 知道自己的 Session 在等）；8.8 節。
- **可否回退**：可以，但回退也要再重新訂閱一次。

### 階段 4：機器之間的通道與帳號資料

- **範圍**：
  - relay 允許機器對機器發 `ctl`、允許機器讀別台的 `orch/`（修訂 PROTOCOL §4），但仍不能讀 `s/`、`t/`。
  - 機器之間互相釘選裝置金鑰，由已經信任兩邊的瀏覽器引介。
  - 帳號資料主機、副本、草稿與衝突；主機移交。
  - 可選：Cloud 持久保存加密的最後快照（修訂 PROTOCOL §2）。
- **動到**：relay、Mac app、Linux executor、網頁。
- **大小**：XL。
- **驗收**：
  - 主機離線時，手機仍看到常用句最後版本並標時間；草稿在主機回到在線後送出且不重複。
  - 強制接手後，舊主機回來會自動變成副本並列出衝突。
  - relay 測試證明機器仍讀不到 `s/`、`t/`。
- **依賴**：8.3 節、8.4 節、8.5 節。
- **可否回退**：relay 規則可以關；帳號資料可退回各機器各自一份。

### 階段 5：跨機 Orchestrator

- **範圍**：
  - 固定 Cloud 派工的 wire shape，拿掉 `cloud_dispatch_unpinned`。
  - 派工方挑機器；Linux 精簡版 broker 接工作。
  - 執行機保管結果、root 回到在線後交付；交付是推到 remote 的分支，落地機器 fetch。
- **動到**：Mac app（broker）、Linux executor、網頁。
- **大小**：XL。
- **驗收**：
  - root 在 MacBook，子任務派到 linux-aws；MacBook 離線 30 分鐘（切斷網路）期間子任務完成；MacBook 回到在線後 1 分鐘內收到結果並能 fetch 分支落地。
  - 同一個 task id 重送不會開第二個 Session。
  - base 沒 push 時，派工在送出前被拒。
- **依賴**：階段 4；8.9 節（含 D9 文字修訂）。
- **可否回退**：可以，派工退回「只派到同一台」。

---

## 7. 需要實驗才能回答的未知數

每項都是 root 可以派出去的小實驗，不是給 Sean 的決策。E3、E5 量的是這一版已經拿掉的機制，所以刪除；其餘保留原編號，方便對照先前的討論。

### E1 iOS 上的語音辨識

- **不知道**：`webkitSpeechRecognition` 在 iOS 26／27 的 Safari 分頁與主畫面 app 裡能不能用、`zh-TW` 能不能辨識、飛航模式下能不能動；鍵盤聽寫在主畫面 app 的輸入框能不能用。
- **最小實驗**：在 Pages preview 放一頁 30 行的測試頁。iPhone 上分別用 Safari 分頁與加到主畫面各開一次，記錄 `'webkitSpeechRecognition' in window`、`onerror` 的代碼、飛航模式下的結果；再到「設定 › 一般 › 鍵盤」看聽寫是否標示裝置端處理。Sean 只需要點兩下。

### E2 Linux Session 打字（程式碼已回答：不行）

- **已知**：程式碼顯示不行（2.2 節）。
- **最小實驗**：真帳號上從 hosted console 對 Linux Session 送一句，確認 60 秒後出現 `cloud_read_timeout`、daemon log 有 `malformed_command`；再按一個鍵，確認畫面顯示已送出而 Session 沒收到。用來當階段 2 的紅燈基準；要在 ed5f1c24 上線前做，它會改變這個行為。

### E4 離線的機器在 relay 上「還算連著」多久

- **不知道**：沒有階段 1 的 relay 改動時，各種離線要多久才讓指令得到 `machine_offline`；程式結束時 relay 是不是真的立刻知道。
- **最小實驗**：手機每 10 秒送一次 `cloud.status`；機器那頭依序結束 App、`kill -9` 程序、切斷網路（有線與 Wi-Fi 各一次），記錄每種情況第一次 `machine_offline` 的時間。Mac 與 Linux 各做一輪。

### E6 relay 記憶體裡的最後快照多久被清掉

- **最小實驗**：機器離線後，隔 10、30、60 分鐘各開一個新的瀏覽器，數 realign 收到幾個 envelope。答案決定 8.3 節選項一夠不夠。

### E7 機器離線超過逾時時間，broker 怎麼算

- **最小實驗**：派一個 `timeout_minutes` 很短的 child，讓執行它的機器離線超過逾時時間後回來（切斷網路一次、結束 App 一次），看任務變成 timed out、繼續，還是其他狀態。

### E8 iOS 主畫面 app 換 VAPID 金鑰後重新訂閱

- **最小實驗**：舊訂閱 `unsubscribe`，用新 key `subscribe`，由兩台機器各推一則相同 `topic` 的通知，確認都收得到而且不重複。

### E9 `getWebSocketAutoResponseTimestamp` 在休眠下可不可靠

- **最小實驗**：workerd／miniflare 測試裡讓 client 定期 ping、再停止，讀時間戳；部署到 staging 後實際切斷一次網路確認。

---

## 8. 給 Sean 的決策清單

每題的建議選項排第一並標「建議」。「下一步」寫的是拍板後誰去做。

### 8.1 先做哪一段？

- **A（建議）階段 1：離線感知與快速換手。** 直接解決「機器離線時手機一直轉圈」，不用改協定。代價：約 L（網頁 M、relay S–M），需要一次 relay 部署；relay 那塊可以先上。
- **B 先做階段 2：Linux Session 能用。** 讓 Linux 真的能接手工作，但手機仍然要等逾時才知道 Mac 離線了。代價：L。
- **C 先做跨機派工。** 價值最高、風險最高，依賴階段 4 的協定修訂。代價：XL＋XL。
- **下一步**：root 派一個 feature child 做階段 1；E4、E9 先做。

### 8.2 手機內建語音辨識要不要當退路？

- **A（建議）不做。** 沒有在線的 Mac 時直接說，並提示「可以用鍵盤上的聽寫（由 Apple 處理）」。理由：`docs/whisper.md` 已經回答過這題；iOS 主畫面 app 用不了 Web Speech；繁中不在 Apple 裝置端聽寫清單上。代價：Mac 全都離線時沒有 app 內的語音輸入。
- **B 在 Safari 分頁提供明確 opt-in 的 Web Speech，每次標示「音訊交給 Apple／Google」。** 主畫面 app 仍然沒有。代價：一條只有部分使用者用得到的路徑，還要改寫 `docs/whisper.md` 的立場。
- **C 自動退回手機辨識。** 最方便，但音訊會在使用者沒意識到時交給第三方，和產品立場衝突。
- **下一步**：選 A 由 root 把提示文案寫進階段 1 的 brief；選 B 則 root 先派 E1。

### 8.3 主機離線時，帳號資料怎麼讀？

- **A（建議）瀏覽器自己保存最後快照。** Cloud 不用改。代價：新手機或清掉網站資料時，主機離線就看不到。
- **B 另外讓 Cloud 保存加密的最後快照。** 任何裝置都看得到。代價：修訂 PROTOCOL §2「envelope 內容永不持久化」，Cloud 多存密文、頻道名稱、大小、更新時間與保留期限；relay 要改、要部署。
- **C 不做。** 主機離線就看不到。
- **下一步**：選 A 由 root 寫進階段 1 的 brief；選 B 則 root 先派 E6 看記憶體快取的實際壽命，再起草 PROTOCOL 修訂。

### 8.4 主機角色怎麼換？

- **A（建議）手動移交；舊主機一直不回來時可以強制接手，舊主機回來自動變成副本並列出衝突。** 代價：強制接手時可能有少量編輯要你手動選。
- **B 依偏好順序自動換。** 不用操作，但兩台都以為自己是主機的時間窗會變多，衝突也更頻繁。
- **C 固定一台，不能換。** 最簡單；那台長期不在時，帳號資料就只能讀。
- **下一步**：root 寫進階段 4 的 brief。

### 8.5 機器之間怎麼互通？

- **A（建議）relay 加機器對機器的指令通道，並允許機器讀別台的 `orch/`。** 修訂 PROTOCOL §4 與 `routing.ts:48-51`「v0.4 沒有機器對機器的 ctl」。代價：relay 改動、測試、部署；Cloud 多看到「哪台送給哪台」的流向。
- **B 每台機器也用 viewer 身分互相配對。** 不改 relay。代價：N 台要配對 N×(N−1) 次、佔用 viewer 裝置名額，而且機器會拿到比需要更多的讀取權（`s/` 也讀得到）。
- **C 不做跨機協調。** 帳號資料只在瀏覽器；跨機派工只能從手機發。
- **下一步**：root 起草 PROTOCOL §4 修訂，在 clawdline-cloud repo 裡派一次複審。

### 8.6 釘選的機器離線時怎麼辦？

- **A（建議）送出前拒絕，並給「這次改用 X」的一鍵選項。** 代價：多按一下。
- **B 自動改用下一台，事後告訴你。** 少一步，但違反「釘選就是釘選」；語音可能被不同設定的 Mac 轉寫。
- **C 照今天，等到逾時。** 不用開發，但就是今天「按了等 6 分鐘」的問題。
- **下一步**：root 寫進階段 1 的 brief。

### 8.7 專案身分怎麼定？

- **A（建議）正規化 git remote；沒有 remote 的專案各機分開，永不自動合併。** 代價：同一個專案若兩台設了不同 remote（例如 fork），會變兩列，要手動合併。
- **B 只靠手動合併。** 最精確，但每個專案都要設定一次。
- **C 用資料夾名稱。** 會把不相干的同名資料夾合在一起。
- **下一步**：root 寫進階段 2 的 brief。

### 8.8 推播金鑰怎麼管？

- **A（建議）從 master secret 衍生帳號共用的 VAPID 金鑰。** 不用在機器間傳私鑰、不用主機。代價：遷移時所有訂閱重新訂閱一次；輪替綁在 `key_id` 上。
- **B 主機持有金鑰，複製給其他機器。** 依賴階段 4 的通道，主機不在時新機器拿不到。
- **C 維持「一個帳號只有一台 Mac 推播」。** 不用做，但 Mac 離線時 Linux 的事沒人推。
- **下一步**：root 先派 E8，再寫進階段 3 的 brief。

### 8.9 Orchestrator 怎麼挑機器？

- **A（建議）新工作由派工方每次依規則自動挑、可按專案或 graph 釘選；跑中的不搬；不設固定協調者；D9 的「留在 Mac」改成「留在使用者的機器（Mac 或 Linux executor）」。** 代價：Linux 要有精簡版 broker；要固定派工 wire shape；D9 文字修訂。
- **B 一律手動挑機器，D9 不改。** 簡單、可預測，但 Mac 離線時要你自己想起來換。Linux 不接工作。
- **C 固定一台協調者機器替大家挑。** 看得到全局，但協調者離線時所有自動派工都停。
- **下一步**：root 先派 E7，再寫進階段 5 的 brief。

---

## 9. 附錄

### A. 引用的檔案與行號

`clawdline` 在 `a0be46809fc41e65abfece483aac5f115bdbcb4e`：

- `Sources/CloudBridgeLifecycle.swift:160`：device token 每 4 分鐘輪替。
- `Sources/CloudTransport.swift:1614-1629`：`CloudTransport.production` 不改 keepalive 間隔，Mac 與 Linux 共用。
- `Sources/CloudTransport.swift:1644`、`:2482-2498`：每 30 秒 keepalive。
- `Sources/CloudTransport.swift:2500-2512`：keepalive 送的是 `{"type":"ping"}`。
- `Sources/OrchestratorPersistence.swift:465-474`：`orch/` 快照內容（常用句、任務、排程）。
- `Sources/OrchestratorPersistence.swift:481-489`：機器描述只供顯示。
- `Sources/CloudAppBridge.swift:2485-2495`：Cloud `dispatch` 回 `cloud_dispatch_unpinned`。
- `Sources/CloudLocalRoute.swift:303-311`：`no_whisper`。
- `Sources/WebPush.swift:171`：`push.json`。
- `Sources/WebPush.swift:246-258`：每台 Mac 自己產生 VAPID 金鑰；換金鑰讓訂閱失效。
- `Sources/WebPush.swift:770-807`：推播由 Mac 直送推播服務。
- `Sources/Snippets.swift:8`：常用句。
- `Sources/ProjectBoardStore.swift:10-12`：Board 的權威。
- `Sources/ProjectTimelineStore.swift:4`：Timeline 的權威。
- `Sources/ProjectTimelineModel.swift:106-123`：`repositoryID` 正規化 GitHub remote。
- `Sources/ProjectTimelineGitImporter.swift:82`：Timeline 以 `repositoryID:sha` 為 key。
- `Sources/ProjectTimelineGitImporter.swift:142-149`：沒有 GitHub remote 時用路徑雜湊。
- `Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:158-161`：Linux 用 `CloudTransport.production` 連 relay。
- `Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:307`：描述每 240 秒重發。
- `Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:534`：`start`。
- `Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:674-684`：只接受 `places`／`start`，其餘 `malformed_command`；`place_not_found`。
- `Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift:769-775`：只有 `places`／`start` 會回拒絕。
- `Packages/ClawdlineLinux/LinuxDaemonIngress.swift:927-932`：原生介面的送字。
- `Resources/web/app/js/net/cloud-client.js:46-48`：`READ_TIMEOUT_MS`、`VOICE_TIMEOUT_MS`、`MACHINE_INVENTORY_FRESH_MS`。
- `Resources/web/app/js/net/cloud-client.js:52-56`：同步窗、描述快取、語音偏好。
- `Resources/web/app/js/net/cloud-client.js:123`：`STATUS_PROBE_TIMEOUT_MS`（10 秒）。
- `Resources/web/app/js/net/cloud-client.js:307-309`：「不是 presence lease」。
- `Resources/web/app/js/net/cloud-client.js:1262-1263`：`_machineRows()` 在呼叫當下取時間。
- `Resources/web/app/js/net/cloud-client.js:1277-1291`：`current`／`stale`／`unknown`。
- `Resources/web/app/js/net/cloud-client.js:1312`、`:1330-1361`：`voiceHost` 解析器，「不管多舊」。
- `Resources/web/app/js/net/cloud-client.js:1399-1410`：`_onlyMachine` 與 `cloud_machine_ambiguous`。
- `Resources/web/app/js/net/cloud-client.js:1515`、`:1899`、`:1921`、`:2807`：診斷、Board、Timeline、推播走 `_onlyMachine`。
- `Resources/web/app/js/net/cloud-client.js:2307-2311`：`cloud_read_timeout`。
- `Resources/web/app/js/net/cloud-client.js:2405-2406`：relay `machine_offline` 立刻報錯。
- `Resources/web/app/js/net/cloud-client.js:2621`：排程重新整理的 `Promise.all`。
- `Resources/web/app/js/net/cloud-client.js:2761-2762`：不認得的機器，按鍵以 relay `ack` 當送達。
- `Resources/web/app/js/net/cloud-client.js:2839-2840`：語音用 6 分鐘逾時。
- `Resources/web/app/js/net/client.js:9`、`Resources/web/app/js/net/live.js:30`、`Resources/web/app/js/main.js:270-274`：免費版本機 transport。
- `Resources/web/app/js/input/start.js:678`：Start sheet 只在單機時自動選。
- `Resources/web/app/js/input/snippets.js:277-279`：常用句依 Session 的機器過濾。
- `Resources/web/app/js/view/projects.js:511`、`:534`：專案頁不帶機器呼叫 Board。
- `Resources/web/app/js/main.js:656-657`、`:711-715`：從 Session 或連結開 Board、Timeline 時會帶機器。
- `Resources/web/app/js/view/list.js:546-550`：Session 列的機器標籤。
- `Resources/web/app/js/view/devices.js:39-40`、`:136-142`：Devices 的連線狀態與語音按鈕。
- `docs/cloud.md:333-338`：`orch/` 在每次連線與 token 輪替時重發。
- `docs/whisper.md:230-235`：不用手機辨識器的理由。
- `docs/whisper.md:258-267`：hosted console 的語音由一台 Mac 轉寫。
- `docs/schedules.md:95`：`catch_up_hours`。
- `docs/ubuntu-headless-runtime-plan.md:30`：Linux MVP 排除語音與通知。

`clawdline-cloud` 在 `6a10552a3b0c83bed93b2409e85adde6566c9ec2`（私有 repo，只列位置，不附連結）：

- `docs/DECISIONS.md:56-61`（D8）、`:63-68`（D9）、`:70-74`（D10）、`:180-199`（D18 outbox）。
- `docs/2026-08-25-cloud-blueprint.html:185-187`（D1）、`:332`（加密 history vault）、`:342`（Web Push 由機器直送）。
- `docs/PROTOCOL.md:40-46`（metadata 邊界）、`:48-66`（§2）、`:58-61`（記憶體保存最後一份、不持久化）、`:79-111`（§4）、`:100-103`（`machine_offline`、無 store-and-forward）、`:206-216`（§7）、`:255-290`（§11）、`:263-270`（跨機派工的門與 typed error）、`:271-274`（fleet dashboard 客戶端彙整）。
- `relay/src/lib/routing.ts:48-51`、`:69-73`：沒有機器對機器的 `ctl`。
- `relay/src/lib/routing.ts:159-163`：機器只收 `ctl`、`ho`。
- `relay/src/lib/routing.ts:189-193`：`delivered`／`machine_offline`。
- `relay/src/lib/cache.ts:1-12`：最後一份只在記憶體。
- `relay/src/account-do.ts:181-182`：`PING_FRAME`／`PONG_FRAME`。
- `relay/src/account-do.ts:336`：ping 自動回覆。
- `relay/src/account-do.ts:897-903`：`ack` 帶 `status`。
- `relay/src/account-do.ts:1033-1042`：socket 關閉時只結算並回應關閉，不通知瀏覽器。
- `relay/src/account-do.ts:1830-1838`：在線＝目前的 socket。

進行中、未合併：`clawdline/task/ed5f1c24-ad13-4f6d-bb9a-74f471df09f5`，head `77e7104d`。

### B. 外部來源

2026-09-15 讀取。

**iOS／瀏覽器語音辨識**

- WebKit bug 225298（主畫面 app 不能用，RESOLVED LATER）：https://bugs.webkit.org/show_bug.cgi?id=225298
- WebKit blog，Safari 14.1 加入語音辨識：https://webkit.org/blog/11648/new-webkit-features-in-safari-14-1/
- MDN browser-compat-data：https://raw.githubusercontent.com/mdn/browser-compat-data/main/api/SpeechRecognition.json
- caniuse（「Not available in SafariViewController and web apps added to Home Screen」）：https://caniuse.com/speech-recognition
- WebKit 原始碼，支援裝置端時要求裝置端辨識：https://github.com/WebKit/WebKit/blob/main/Source/WebCore/Modules/speech/cocoa/WebSpeechRecognizerTask.mm
- Apple `requiresOnDeviceRecognition`：https://developer.apple.com/documentation/speech/sfspeechrecognitionrequest/requiresondevicerecognition
- Apple 功能供應表（裝置端聽寫清單沒有台灣華語）：https://www.apple.com/ios/feature-availability/
- Apple 聽寫與隱私：https://www.apple.com/legal/privacy/data/en/ask-siri-dictation/
- iPhone 使用手冊，聽寫：https://support.apple.com/guide/iphone/dictate-text-iph2c0651d2/ios
- Chromium，音訊送 Google：https://github.com/chromium/chromium/blob/main/content/browser/speech/network_speech_recognition_engine_impl.cc
- Chrome 139（`processLocally`）：https://developer.chrome.com/blog/new-in-chrome-139
- Web Speech API 規格草案：https://webaudio.github.io/web-speech-api/

**推播**

- Push API（一個註冊一組訂閱；key 不同會 `InvalidStateError`）：https://www.w3.org/TR/push-api/

**WebSocket 與 Cloudflare**

- RFC 6455（ping 可驗證對端仍在）：https://www.rfc-editor.org/rfc/rfc6455
- RFC 1122（TCP keep-alive 預設關、間隔至少兩小時）：https://www.rfc-editor.org/rfc/rfc1122
- Durable Objects WebSocket（自動回覆不中斷休眠）：https://developers.cloudflare.com/durable-objects/best-practices/websockets/
- Durable Objects state API（`getWebSocketAutoResponseTimestamp`）：https://developers.cloudflare.com/durable-objects/api/state/

### C. 名詞表

- **機器**：帳號底下能跑 Session 的電腦，今天是 Mac 或 Linux executor；身分是 relay 認證的 `machine_id`。
- **在線／離線**：手機對一台機器算出的唯一可達性狀態，依 relay 的回覆、送前探測與新鮮度窗口判斷；不區分離線的原因。
- **新鮮度窗口**：網頁把「多久內看過它的訊息」當成在線的門檻，今天是 5 分鐘。
- **viewer**：只看與下指令的裝置，例如手機、筆電瀏覽器。
- **relay**：Cloudflare 上轉送密文的服務，每個帳號一個 Durable Object。
- **envelope**：relay 看到的唯一形狀；內容在 `ct` 密文裡。
- **`orch/<machine>`**：機器發佈的整份狀態快照（任務、排程、常用句、描述）。
- **`ctl/<machine>`**：送給機器的指令頻道。
- **`machine_offline`**：relay 說「這台目前沒有連線」。
- **半開連線**：一邊已經不在（例如斷網或斷電），另一邊還以為連著。
- **能力**：機器自己宣告能做的事，例如 `voice.transcribe`。
- **提供者規則**：沒指定機器時由誰做，自動或釘選。
- **帳號資料主機（主機）**：帳號層級可編輯小清單的唯一寫入者。
- **派工方**：發出新工作的一方，手機或 root 所在的機器。
- **broker**：每台機器上的任務仲裁者（claims、容量、depth、policy）。
- **VAPID**：Web Push 用來證明「是誰發的」的簽章金鑰。
