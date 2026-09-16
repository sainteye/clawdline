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
web/                      console（從舊 repo 複製）
shell/darwin|windows|linux
docs/                     本檔與 coordination.md
tools/                    守衛與生成腳本
```

**兩條硬規則**，由守衛強制：

- `domain` 不 import `adapters` 或 `transport`
- `transport` 不 import `adapters`

Go 的 package 邊界讓這是編譯期的事，不是掃字串。舊樹的 `Orchestrator ↔ RemoteServer`
循環相依在 Swift 的單一模組裡難以根除，正是因為缺這道邊界。

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
| **P4** | 剪斷代理 |
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
`%231` 因此不同——它接過一次 handoff，助理把自己叫作「Clawdline handoff …」，
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
