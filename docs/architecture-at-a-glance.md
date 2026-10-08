# Clawdline 架構，一眼看懂

> 2026-10-09 的程式碼快照。這是一張產品地圖：**執行與工作事實在使用者的機器上；Cloud 管帳號與路由；瀏覽器是操作介面。** Cloud 為選用功能，預設關閉。

## 01｜三個位置，一條主線

```text
本機瀏覽器 ── HTTP /v1 + SSE ──┐
                               ├── Clawdline app（Go daemon）── 終端機、Claude／Codex、專案檔案
Cloud 瀏覽器 ── 加密訊息 ── Relay ┘              │
     │                                        ├── SQLite：工作與協調事實
     └── Cloud API：登入、裝置、方案與通知         └── 本機檔案：逐字稿來源、task JSON、文件、金鑰
```

本機與 Cloud 瀏覽器使用同一套 React console 原始碼，但編成不同版本。本機版由 daemon 供應；`app.clawdline.com` 供應 Cloud 版。Cloud 版的遠端內容請求經加密 relay 回到目標機器；Cloud API 只處理帳號與服務中繼資料。跨機器功能屬於 Cloud；免費開源版以單機為界。

## 02｜三種資料庫，三種職責

| 在哪裡 | 技術／形式 | 放什麼 | 不放什麼 |
|---|---|---|---|
| Clawdline app | 單一 `clawdline.sqlite3`，`modernc.org/sqlite` | Board／Backlog／待辦、任務與指派、排程、事件、收據、協調租約、部分文件文字與圖片索引 | 全部專案檔、Claude／Codex 原始逐字稿、Cloud 帳號與訂閱 |
| Clawdline Cloud API | MongoDB 文件集合 | 帳號與登入身分、機器／瀏覽器裝置、權限與撤銷、配對狀態、訂閱與額度、用量計數、Push 訂閱、webhook 設定與送達收據等中繼資料 | Session／Board 內容與可解密的內容金鑰 |
| Clawdline Cloud relay | 每帳號一個 SQLite-backed Durable Object；持久儲存其路由與計量中繼資料 | 裝置撤銷狀態、計量計數、用量待送批次、通道名稱等 | 持久保存的 Session／Board／終端機內容 |

**「DB」不是唯一儲存形式。** 本機 task 的 `task.json`／`result.json`、設定與金鑰檔、Markdown 文件和圖片位元組是檔案。Claude／Codex 的逐字稿由各自程式寫在本機，Clawdline 讀取。Cloud 瀏覽器把配對後的裝置與內容金鑰放在該瀏覽器的 IndexedDB；少量裝置／畫面偏好用 localStorage 或 sessionStorage。API 往返與加密信封是 JSON 協定，不等於「整個系統用 JSON 檔儲存」。

## 03｜瀏覽器走哪條路

| 行為 | 本機瀏覽器 | `app.clawdline.com` 瀏覽器 |
|---|---|---|
| 讀 Session、Board、待辦、排程與文件；送訊息或指令 | 直接向本機 daemon 的 `/v1` 發 HTTP 請求，並用 SSE 接事件 | 對目標機器加密後經 relay；機器驗證並執行，再加密回覆 |
| 登入、列出帳號機器、管理裝置／方案、配對與 Push 訂閱 | 一般不需要 Cloud | 向 Cloud API 發帳號／裝置中繼資料請求；配對金鑰交接只轉送密封資料 |
| 開啟即時終端機 | 直接連本機 daemon | 起始經 Cloud relay；條件允許時 WebRTC 資料通道升級為瀏覽器與機器直連，失敗則留在 relay |

Cloud 模式的指令不是在 Cloud 執行。目標機器離線時，不能把它當作已執行；Cloud 不代替機器存一份待執行的通用工作佇列。

## 04｜資料會「記得」在哪裡

```text
Cloud 持久記得    帳號、裝置、權限、訂閱、用量、通知／webhook 中繼資料
Cloud 暫時持有    傳輸中的密文；relay 記憶體中的最新加密快照（喚醒後可消失）
機器持久記得     工作項目、排程、任務／收據、Session 對應、設定與本機信任
機器檔案記得     文件、圖片、任務 JSON、金鑰；Claude／Codex 自己寫逐字稿
瀏覽器記得       本機 IndexedDB 金鑰、localStorage／sessionStorage 中的有限狀態
```

Cloud 能看到必要的路由中繼資料，例如帳號／裝置／機器識別、通道、連線與用量；內容信封中的 Session 文字、Board 文字與終端機輸入由端點加解密。relay 不把密文信封寫進持久儲存；它的最新快照快取僅在記憶體。配對交接可暫存不透明的密封資料，Cloud 不能據此解密工作內容。

## 05｜各功能實作占比

2026-10-09 量測公開 repo `79cdc084` 與 Cloud 預覽分支 `f4cff69` 的 Git 追蹤實作檔，共 **337,652 實體行／1,272 檔**。每檔依主要功能歸類一次；跨功能的接線與平台程式列為共用。含空行與註解，排除測試、產生碼、文件、工具、依賴、建置輸出與本架構頁。分類規則可由 [`tools/architecture-feature-size.py`](../tools/architecture-feature-size.py) 重跑。

| 功能 | 行數 | 占比 |
|---|---:|---:|
| Cloud 遠端與加密 | 62,970 | 18.6% |
| 共用框架與平台 | 58,750 | 17.4% |
| 派工與協調 | 32,746 | 9.7% |
| Session 閱讀／歷史 | 32,266 | 9.6% |
| 工作流程與提案 | 29,664 | 8.8% |
| Session 輸入／終端 | 26,747 | 7.9% |
| 專案、檔案與文件 | 25,630 | 7.6% |
| 設定、帳號與裝置 | 15,708 | 4.7% |
| 排程、通知與 webhook | 14,218 | 4.2% |
| 看板／Backlog／待辦 | 10,386 | 3.1% |
| 角色與 Squad | 10,294 | 3.0% |
| 用量、方案與更新 | 9,261 | 2.7% |
| 官網 | 9,012 | 2.7% |

角色／Squad 與派工 broker 分開；看板／待辦與 Work／Proposal 分開；Session 閱讀與輸入／終端分開。這是**程式碼份量，不是執行時間、重要性或開發成本**，也不包含 Claude、Codex、tmux、Cloudflare 或 MongoDB 本身的原始碼。

## 06｜加密與信任邊界（開發者圖一）

```text
瀏覽器 IndexedDB               Cloud API / Relay                 機器 owner-only 檔案
裝置私鑰＋配對內容金鑰 ── 密文、簽章、路由 metadata ── 裝置私鑰＋內容金鑰
         解密端                         不持內容金鑰                    加密／執行端
```

每台裝置用 Ed25519 身分金鑰簽名；Session／指令內容以目標機器的 AES-256-GCM 內容金鑰封裝。配對把該機器的金鑰交給已驗證的瀏覽器。Cloud 帳號身分不等於機器授權；機器仍檢查配對、撤銷與「允許 Cloud 指令」開關。

## 07｜遠端指令生命週期（開發者圖二）

```text
瀏覽器送出 → relay 接受／轉送 → 機器驗證 → 本機 handler 執行 → 密文回覆 → 瀏覽器觀察
                │                  │                    │
          不代表已執行       可拒絕／離線         收據區分結果與後續確認
```

`accepted`、`executed`、`delivered`、`observed`、`acknowledged` 是不同事實。Cloud 指令進入與本機請求相同的 handler 與授權門；relay 的轉送成功不等於終端機真的收到字、使用者真的看見答案，或工作已審查／合併。

## 08｜改一項功能，要看哪一層（開發者圖三）

```text
React 畫面（web/console）──────┐
                             ├→ HTTP transport／Cloud transport → app use case → domain rules
CLI（cmd/clawdline）───────────┘                                      │
                                                                    ↓
                                        adapters：SQLite、終端機、檔案、Cloud、平台能力
```

`api/v1/*.schema.json` 是本機 API 的共同契約，產生 Go 與 TypeScript 型別；`web/core` 放可重用的 API／stream 邏輯。macOS、Linux、Windows 的差異主要收在平台 adapter 與桌面殼。這是程式碼導覽圖，**不是嚴格的 import DAG**：目前 HTTP transport 也負責組裝，broker 也直接使用部分 adapter。

---

**證據與邊界**：本機實作以 `docs/architecture.md`、`docs/cloud.md`、`internal/adapters/store`、`internal/transport/{http,cloud}`、`web/console/src/cloud` 為準；Cloud 服務以其 `api/src/db/mongo`、`relay/src/account-do.ts`、`relay/src/lib/cache.ts` 為準。功能行數依 `tools/architecture-feature-size.py` 對兩個 checkout 的追蹤檔逐檔歸類。本文件核對的是 2026-10-09 的原始碼，不宣稱此時生產環境每個部署都與 checkout 同版。
