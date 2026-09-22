<!-- retired-app-record: 與舊 app 並行重建的計畫，量於 2026-09-16 起；該 app 已於 2026-09-19 停用，7717 無人監聽 -->

> **主體與時間：** 這份文件記錄的是 **與舊 app 並行重建的計畫**，量於 **2026-09-16 起**。
> **舊 Swift app 已於 2026-09-19 停掉、取消登入時啟動，7717 現在沒有人在聽。**
> 文中寫成現在式的「舊 app 還在跑」「7717」都是**量測當下**的事實，刻意保留，
> 用來對照還有哪些功能要 migrate、當初怎麼實作——**不要照著它設定今天的 daemon**。

# clawdline-go — 計畫

一份**平行重建**：與現有的 Swift 版 Clawdline 同功能、但核心以 Go 寫成、介面統一為既有的 web
console、且從第一天就同時支援 macOS / Windows / Linux。

**現有的 `~/code/clawdline` 全程只被讀，不被改。** 兩者可以同時安裝、同時執行。

---

## 1. 不可動的十件事

架構全部重來，但這些是產品之所以是它的原因。設計必須滿足它們。

1. **不搶你的鍵盤** — 協調你已經開著的 session，而不是取代它
2. **協調權留在本機** — cloud 只送密文、不持有任何 task 狀態
3. **內容金鑰在使用者硬體上** — AES-GCM 與 Ed25519，因為那是零依賴的 runtime 交集
4. **多機是雲端功能** — 開源版是單機
5. **只建模在線／離線**，不建模睡眠
6. **delivered ≠ reviewed ≠ landed**
7. **收據是持久且型別化的** — accepted → executed → delivered → observed → acknowledged
8. **claims 先宣告寫入路徑**，重疊要在開始前擋掉
9. **Claude 與 Codex 對等**，任一方可派另一方
10. **沒有 store-and-forward** — 機器離線要大聲說

---

## 2. 一句話

一個 Go 二進位，同時是 daemon 和 CLI；執行永遠在本機；所有 client 消費同一份 HTTP＋SSE 契約。

```
  外框（每平台一個薄殼）
    macOS  : Swift + WKWebView + 選單列 / 全域熱鍵 / 拖放
    Windows: WebView2 + 系統匣
    Linux  : WebKitGTK，或直接用瀏覽器
         |  http://127.0.0.1:7727
  console（沿用現有那份，一行不改）
         |  同一份 /v1 契約
  Go daemon
    ├ HTTP + SSE server
    ├ 終端機層（attached / owned）
    ├ transcript 讀取（~/.claude、~/.codex）
    ├ orchestrator 狀態機
    └ 狀態儲存（單一事件流）
```

---

## 3. Package 佈局

```
cmd/clawdline/            單一 binary：serve / status / pair / task / doctor

internal/
  domain/                 純邏輯，不 import 任何 I/O
    session/              身分、狀態、證據
    task/                 claims、waits、landing、receipts、obligation
    board/                專案看板
  app/
    ports/                TerminalHost / ProcessHost / FileSystem /
                          Store / Secrets / Clock / Publisher
  adapters/               每個 port 的實作，依平台分檔
    terminal/             iterm_darwin.go  tmux.go
                          conpty_windows.go  pty_unix.go
    process/              ps_darwin.go  procfs_linux.go  win32_windows.go
    store/                sqlite.go（modernc.org/sqlite，純 Go，不開 cgo）
    supervisor/           砍整棵行程樹、detach
  transport/http/         inbound：路由、認證、SSE、代理
  contract/               由 schema 生成的型別（唯一真相）

api/v1/*.schema.json      契約本體
web/                      前端，三個 npm workspace（見 §3.1）
  contract/               生成的 TS 型別；零 import
  core/                   client、refusal、fleet store；無 DOM、無 React
  console/                React DOM 的畫面
shell/darwin|windows|linux
docs/                     本檔與 coordination.md
tools/
  contract-gen/           讀 api/v1 生 Go 與 TS
  package-macos.sh        打包
```

**兩條硬規則**，由守衛強制：

- `domain` 不 import `adapters` 或 `transport`
- `transport` 不 import `adapters`

Go 的 package 邊界讓這是編譯期的事，不是掃字串。舊樹的 `Orchestrator ↔ RemoteServer`
循環相依在 Swift 的單一模組裡難以根除，正是因為缺這道邊界。

---

## 3.1 前端：React，以及將來的 React Native

**決定（2026-09-17）**：web console 用 React 重寫；手機 app 若要做，用 React Native，
**但兩邊不共用畫面層，只共用畫面以下的全部**。

### 為什麼不是 React Native Web 一套打通

看板是密集的桌面畫面——八列 session、每列四個欄位、右欄四塊面板。
RN 的 primitive（`View` / `Text`）做這種版面要付出的代價，遠大於「手機和桌面共用一份 JSX」
省下來的。而手機上要的本來就不是同一個版面：那邊要的是「哪幾件事需要我」，
不是一張表。**同一份版面在兩個裝置上都只能是妥協。**

### 所以切在哪裡

| 層 | 內容 | 瀏覽器 | React Native |
|---|---|---|---|
| `web/contract` | 生成的型別 | ✓ | ✓ |
| `web/core` | client、refusal、fleet store、排序規則 | ✓ | ✓ |
| 畫面 | React DOM / RN | 各自寫 | 各自寫 |

`core` 的硬規則：**不 import DOM，不 import React，不 import 任何 bundler 專屬語法。**
它跟 host 要的東西只有兩樣，而且都寫在型別裡：

- `fetch` — 瀏覽器和 RN 都有，所以 client 不需要平台分支
- `StreamTransport` — **這是兩邊唯一真正不同的地方**。瀏覽器有 `EventSource`，
  RN 沒有，RN 的替代品（fetch stream、websocket、輪詢）形狀又各不相同。
  所以 core 只宣告形狀，由 host 供應；`nativeEventSourceTransport()` 在沒有
  `EventSource` 時回 `undefined` 而不是丟例外，讓 host 可以在啟動時直接判斷。

輪詢版的 transport 是**具名的東西，不是安靜的退路**：用輪詢餵的畫面比串流晚，
而且會漏掉在兩次讀取之間開了又關的狀態。host 落到這條路上時應該講得出來。

React 端要另外寫的只有 `useFleet.ts`——九行 `useSyncExternalStore`。
**規則住在 store 裡，不住在 hook 裡**，否則手機 app 要重寫一次
「哪個快照可以覆蓋哪個快照」。

### console 不再是借來的

原本 `tools/package-macos.sh` 從 `~/code/clawdline/Resources/web` 複製 51,671 行過來。
那讓這個 app **沒有自己的前端**，而且在這台機器以外的地方建不起來。
現在打包會自己 build，`CLAWDLINE_WEB_SOURCE` 仍然可以指回舊的那份——
那是拿來在同一支 daemon 上比對兩個 console 用的，不是出貨路徑。

---

## 3.2 排程：兩次事故的共同形狀

同一件事咬了兩次，兩次都在測試中開出真的 assistant session。

| | 間隔 | 觸發 | 當時的修法 |
|---|---|---|---|
| 第一次 | 3 秒 | 十八秒開十個 session | `MinInterval = 1m`，在 parse 時拒絕 |
| 第二次 | 1 小時（完全合法） | store 裡一列 `last_run=0`，下一跳就開 | —— |

**速率從來不是問題的形狀，第一次發射才是。**
`Due()` 舊的寫法把「沒跑過」當成「現在就該跑」，理由是「不然第一次會在沒人選的時間跑」。
那個理由對**一個人剛建立的排程**成立，但它分不出「剛建立」和「出現在 store 裡」——
還原的備份、複製過來的、手寫進去的，全都會在時鐘一跳時一起發射。

現在的規則：**間隔從「上次跑」與「這支 daemon 第一次看見它」兩者的較晚者起算。**
「第一次看見」在**讀取時**蓋章而不是寫入時，因為會出事的那些列正是沒經過寫入的那些。

一個人現在建立 `every 1h`，第一次跑在一小時後——那正好是他選的時間，
比「立刻跑」更符合原本那個理由。

**2026-09-18：模型換成舊版的檔案（時刻＋補跑窗），這條規則原樣保留**，成為決策的第 4 關：比這支 daemon
第一次看見那一列還早的那一次，不跑也不算 missed。見 `docs/schedules.md`。

### 這條規則有人看著它

- `internal/domain/schedule/schedule_test.go`：本 repo 的第一個測試，
  三個 case 對應事故走過的三格，換回舊行為三個都紅。
- `/v1/health` 的 `scheduler`：**巡邏本身要可觀察**。
  「巡過但沒派工」和「排程器停了」在外面看起來都是安靜，所以時鐘要像掃描一樣報告自己。
  `due` 跟 `fired` 分開記，因為「沒東西到期」和「有東西到期但派工被拒」不是同一件事。

### 實測（2026-09-17，兩組）

| | `first_seen` | `considered` | `due` | `fired` | 新分頁 |
|---|---|---|---|---|---|
| 處理組 | 就是現在 | 3 | **0** | 0 | 0 |
| 對照組 | 兩小時前 | 3 | **1** | 0 | 0 |

對照組的 `fired=0` 是 dispatch 拒絕的：`workspace_busy: task task-1 already claims [Sources/]`，
**而且是在開任何東西之前拒絕的**——順帶現場驗到了 claims 仲裁的順序。

---

## 4. 與舊 app 的共存規則

**必須全部分開**，否則兩個 app 會搶同一份狀態、同一個埠、同一個 token，
而使用者正在用舊的工作。

| | 現有 | 本專案 |
|---|---|---|
| bundle id | 現有的 | 另一個 |
| 設定目錄 | `~/.config/clawdline` | `~/.config/clawdline-next` |
| 埠 | 7717 | 7727 |
| token 檔 | `orchestrator-token` | 自己的 |
| 服務名 | 現有的 | 另一個 |

另外：**第一版不安裝 Claude hook**，避免與舊 app 互相覆寫。
`~/.claude` 與 `~/.codex` 的 transcript 兩邊都讀，那是唯讀，不衝突。

### 例外：唯讀 Swift app 的 store（2026-09-17，使用者決定）

分開的是**寫入**。為了讓畫面 1:1，本專案**唯讀** `~/.config/clawdline`：Clawdfather 是誰
（`coordinator.json`）、task 列表與標題、交付紀錄、session 自報的狀態、root assignment
（`orchestrator.json`）。這些是舊 app 自己的事實，新 app 沒有另一份來源；不讀，清單上就少了
皇冠、task chip、協調等待與交付勾，標題也會不同。

讀的東西（全部在 `internal/adapters/swiftstore`）：
- `~/.config/clawdline/orchestrator.json`、`coordinator.json`：上面那些事實。
- `~/.config/clawdline/config.json`：只解 `session_titles`，以及方案額度要的 `status_dir`、`codex_home`、
  `assistant_quota_low_threshold` 三個鍵（`QuotaConfig`）。
- **圖片** `~/Library/Caches/com.tsunamiworks.clawdline/session-images/`（`CLAWDLINE_SESSION_IMAGE_DIR` 可改）：
  舊 app 存過的圖片與它們的 metadata（`SessionImageMarker`／`SessionImageArtifact`）。舊 app 在跑的那段時間寫進
  `~/.claude`／`~/.codex` 的對話裡有 `<clawdline-image id="…">` 標記，本專案沒有第二份來源；不讀，那些訊息就只剩一行
  原始標記文字。做法（`images.go`）：`O_RDONLY` 開 `<id>.json` 與 `<id>.png`，比對 byteCount，過期或 `deletedAt`
  就回「過期」。**舊 app 讀到過期記錄時會順手寫墓碑，本讀取器不寫**——過期是答案，不是要改的狀態。
  自己存的圖在自己的 `CLAWDLINE_NEXT_DIR/session-images`，先查自己的、查不到才問舊的。
- **看板** `~/Library/Application Support/Clawdline/project-board.json`（`CLAWDLINE_BOARD_STORE` 可改；2026-09-18）：
  舊 app 的 776 張卡，`/v1/board` 唯讀顯示（`internal/adapters/board/source.go` 的 `Legacy`）。`O_RDONLY` 讀整份、
  依 size＋mtime 快取，讀壞或版本不認得就沿用上一次好的讀數並把 `readState.status` 標成 `stale`，沒有就回 `error`，
  **不回空看板**。這一條放在 `internal/adapters/board` 而不是 `swiftstore`，因為它是那個 task 的認領範圍；
  整條拿掉的方式相同。新版自己的看板設定寫在 `CLAWDLINE_NEXT_DIR/project-board.json`（0600），只有 board 級的
  `enabled`／`narrativeConsent`；項目寫入等 `docs/board-design.md` 的 C1／C2 決定。
- **家規** `~/.config/clawdline/dispatch-policy.md` 與 `dispatch-policy.local.md`（broker，2026-09-18 補登，
  `docs/design-decisions.md` D23 ②）：每次派工時各讀一次，貼進 child 的 CHILD.md；`/v1/diagnostics` 的
  `broker.policy` 也讀同兩個檔算字數。放在 `internal/transport/http/orchestrator_wiring.go` 的 `dispatchPolicy`，
  組合規則在 `internal/app/orchestrator/policy.go`：以**字元**計、上限 16,000，超過時只切 base（段落邊界），
  local 永遠完整。讀不到就當空的（家規是給 child 的建議，缺了不是錯）。退役前改由新 app 自己的 base 投影（D23 ③，W5）。

規則：
- **只讀。** 不寫、不 rename、不建立或觸碰 `.lock`。
- **不讀秘密。** `secrets/`、各種 token 檔、`orchestrator-archive-key`、`push.json`，以及紀錄裡的
  `secret_hash` 一律不讀、不轉出。
- **讀不到是「未知」，不是「空」。** 舊 app 隨時在改寫這些檔；半截的 JSON 要沿用上一次成功的讀數，
  並依 mtime 快取（`orchestrator.json` 有 6 MB）。
- **這是過渡。** 等本專案自己擁有派工與交付紀錄時，這條讀取要能整條拿掉；所以它是一個獨立的
  adapter（`internal/adapters/swiftstore`），不是散在各處的路徑。

---

## 5. 代理式接管

新 daemon 從第一天就跑在 7727，**沒實作的路由轉發給舊的 7717**。

```
console ──► 新 daemon (7727)
              ├ 已接管：自己答
              └ 未接管：轉發 ──► 舊 app (7717)
```

- 第一天就能用：console 完整運作
- 舊的沒被動：新的只是它的 client，唯讀讀它的 token
- 可逐塊驗收：同一個請求打兩邊、比對回應
- 隨時可退回：某塊出問題就轉回代理

過渡期舊 app 要開著。這是開發鷹架，不是出貨行為；P4 是明確的剪斷點。

---

## 6. 里程碑

`/v1/events` 送 `sessions` 與 `orchestrator` 兩種整份快照，所以照這兩半切。
**整塊整塊接管，不要逐條路由**，否則快照會由兩個來源拼出來。

| | 內容 |
|---|---|
| **P0** | 骨架、契約、殼、全代理。三平台 CI 同時開 |
| **P1** | `domain/session` ＋ adapters ＋ `/v1/sessions` ＋ sessions 快照 |
| **P2** | 終端機控制：send / open / interrupt / close |
| **P3** | `domain/task` ＋ board ＋ 排程 ＋ Clawdfather 協調（見 coordination.md） |
| **P4** | 剪斷代理 ✅ |

### P4 達成：console 完全不需要舊 app（實測）

`CLAWDLINE_NEXT_STANDALONE=1` 讓未實作的路由回 typed 的 `not_implemented` 而不是代理出去。
把 console 載進來之後，**它自己說出還缺什麼**——只有四條：

```
GET /v1/strings
GET /v1/board
GET /v1/orchestrator/tasks
GET /v1/orchestrator/schedules
```

其中三條早就實作了，只是掛在 `/v1/next/` 下面驗證用。接上真名之後，
console 完全由新核心供應，未實作請求數為零。

`/v1/strings` 回空目錄：這個 daemon 還沒有在地化目錄，console 會用它內建的英文。
**那是看得見、說得出原因的降級，不是看起來像故障的缺口。**

### 實證：單獨接管一條路由是看不見的（P1 量到）

`/v1/sessions` 被接管之後，畫面完全沒有變化。原因是 `/v1/events` 仍在代理，
而它送的是**整份 sessions 快照**——初始 fetch 用了新核心的資料，第一個串流影格
就把它蓋掉了。判定方法：新核心的 payload 沒有 `closeability`，而畫面上有。

所以「整塊整塊接管」不是偏好而是**必要條件**：一份資料與送它的串流必須一起搬。
`/v1/sessions` 與 `/v1/events` 的 `sessions` 影格是同一塊。

### 接管 sessions 之後，畫面上還缺什麼（P1 實測）

開啟 `CLAWDLINE_NEXT_OWN_SESSIONS=1` 後 console 由新核心供資料，八張卡正確。剩下：

1. **標頭的「N 個在跑」計數消失**。`work_state` 需要 broker 投影，而契約規定
   `ready` 要有正面證據，閒置的助理沒有就是 `unknown`。這是 P3 的工作。
2. **`closeability` 整塊沒有**，所以列上不再出現「還有 N 項未了結」。
3. 列上的「狀態未知」**與舊 app 相同**，不是退化。

### 已知落差：新核心的 `/v1/sessions` 沒有認證

舊 app 對這條路由要求已配對的裝置，新核心目前誰都能讀。在只綁 loopback 的
開發階段可以接受，但**接管開關預設關閉**，而且逐路由 scope 是 P3 的工作項目
（見 coordination.md §4.7）。
| **P5** | Windows／Linux 殼與安裝檔 |

**驗收門檻是 P3**：能派工才算數。

---

## 7. 三平台

不是「先做 Mac 再移植」，而是從 P0 就把差異關進 port。

| 能力 | macOS | Windows | Linux |
|---|---|---|---|
| 遙控既有終端機 | osascript → iTerm2、tmux | tmux（若有） | tmux |
| 自己開終端機 | openpty | ConPTY | openpty |
| 行程盤點 | `ps` | Win32 | procfs |
| 秘密儲存 | Keychain | DPAPI | 0600 檔案 |
| 服務註冊 | launchd | SCM | systemd |

Windows 沒有 tmux，所以 Windows 使用者只有「自己開的 session」那一半。
**這要在產品層面先講清楚，不要等使用者問。**

三平台 CI 從 P0 就必須綠。Windows job 一開始只需要跑得過「daemon 起得來、
`/v1/health` 有回應」，但它從第一天就要是綠的。

---

## 8. 刻意與舊版不同之處

拿舊 app 當 oracle 做差異測試時，**這些欄位會紅，而那是預期的紅**：

- payload 多了 `evidence` 與 capability 欄位
- 權限從全域寫入開關變成逐路由 scope
- 狀態從十個 JSON 檔變成一條事件流
- 任務目錄從 `/tmp` 變成平台解析的 durable 路徑
- `protocolVersion` 從顯示用變成真的能力協商

### 標籤的兩層優先序（P1 觀察到）

1. **Clawdline 自己保存的標題**（舊 app 存在 `config.json` 的 `session_titles[]`）
2. 助理自己的標題：Claude 的 `ai-title`、Codex 的 `session_index.jsonl` 的 `thread_name`

新 app 是全新安裝，第一層是空的，所以它顯示第二層。比對舊 app 時
有一個 session 因此不同——它接過一次 handoff，助理把自己叫作「Clawdline handoff …」，
而舊 app 記著原本那條工作線的名字。**規則相同，覆寫資料不同。**

---

## 9. 狀態：一條事件流

```
command → decider（純函式，不做 I/O）→ events
        → events + 投影 + receipt 在同一個 transaction 落盤
        → 之後才改記憶體、才通知訂閱者
        → 副作用在 reactor 裡事後做
```

給你三件事：「ack 只代表意圖已落盤」變成型別上的事實；不變量可以寫成表格測試；
崩潰復原就是 replay。

---

## 10. 可重用元件與整併

**共用的依據是「這是同一個概念」，不是「這兩段程式碼長得像」。**

一開始就共用：

- `Obligation`（wait / landing / handoff / succession / assignment / dead-letter 的共同模型）
- 契約型別：一份 schema 生 Go 與 TS，mock 也從它生
- read model 在 server 算，client 只渲染
- `TerminalHost` 一個介面四個實作

不共用、只共用測試向量：**加密**（Go 與瀏覽器無法共用實作，共用 `Contracts/Cloud/v1/` 那類向量）。

機制（否則規則會腐爛）：一份**概念登記表**，每個概念指名唯一擁有者，
守衛檢查「出現第二份實作」就紅，而且那個守衛自己要能證明會紅。

反面：同一段邏輯**第三次出現才抽**，除非它一開始就明顯是同一個概念。

---

## 11. 驗證方式（刻意先不寫測試）

**開發期的目標是「外在功能吻合」與撰寫效率，不是覆蓋率。**

- **先不寫單元測試。** 重構／重建期間「跑測試 → 修一個 → 再跑」是最慢的路。
- **驗證靠實際執行**：打開 console 看畫面、對同一個請求打 7717 與 7727 比對回應。
  舊 app 就是 oracle，不需要先寫斷言。
- **盡量減少 build 與測試的次數**：整段寫完再編譯一次，而不是每個檔案編一次。
- **等外在行為對了、形狀穩下來，再補測試**，而且優先補那些「會復發的缺陷類型」。

補測試的時候才適用下列設計（先寫進來，免得屆時又要重來）：

- `domain` 全純函式 → 表格測試、`t.Parallel()`、毫秒級
- adapters 用 fake port 測
- 整合測試用假時鐘，不等真實 deadline
- 所有上限可注入
- 舊 repo 的 `web-*.mjs` 可直接對新 daemon 跑（測協定形狀，不綁語言）

---

## P5 進度（實測）

**六個平台的執行檔可從一台機器產出**，`CGO_ENABLED=0`：

| 目標 | 大小 |
|---|---|
| darwin/arm64 | 11.0 MB |
| darwin/amd64 | 11.6 MB |
| linux/amd64 | 11.5 MB |
| linux/arm64 | 10.9 MB |
| windows/amd64 | 11.6 MB |
| windows/arm64 | 10.8 MB |

**Linux 那顆已在真的 Linux 容器裡跑起來**：`doctor` 正確解析 `/root/.config/clawdline-next`、
SQLite 開得起來、`serve` 綁得上埠、`/v1/health` 從容器內部答得出來。

過程中補上 `CLAWDLINE_NEXT_HOST`：預設仍是 loopback，因為一個握有終端機與憑證的
daemon 不該意外變得可連。綁得更寬時會在 log 裡說一次。

**macOS 那半完成並實測**：Swift 的 WKWebView 殼會啟動捆在 bundle 裡的 daemon
（不是 PATH 上那顆——殼跟它不同建置的 daemon 講話，是沒人重現得了的 bug），
載入 console 後回報 `title=clawdline, elements-with-id=469`。
`tools/package-macos.sh` 產出 15 MB 的 `.app` 與 6.9 MB 的 `.dmg`，掛載後可執行。

**process supervisor 完成並以對照組實測**：沒有行程群組時砍直接子行程留下 3 個孤兒，
有群組時砍整個群組留下 0 個。Windows 版誠實拒絕（需要 Job Object）。

**尚未完成**：Windows 的 WebView2 殼與系統匣、MSI／winget、ConPTY、Job Object。
那幾項在這台機器上無法驗證，而寫沒辦法驗的程式正是這個專案一路避免的事。
