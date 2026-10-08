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

## 05｜程式碼規模：實作行數

下列是**程式碼份量，不是執行時間、重要性或複雜度**。2026-10-09 量測公開 repo `934f62a6` 與 Cloud 服務 repo `3f1d1a62` 的 Git 追蹤實作原始檔，共 **338,049 實體行／1,276 檔**；含空行與註解，排除測試、產生碼、文件、套件依賴及建置輸出。每個檔案只歸到一組；`legacy` 目錄仍在現行 console 中，計入。

| 模組（依程式碼路徑分組） | 行數 | 占比 |
|---|---:|---:|
| Console ＋ web core | 103,911 | 30.7% |
| 本機協調與領域規則 | 61,392 | 18.2% |
| 本機平台／外部程式接點 | 53,346 | 15.8% |
| 本機 HTTP ＋ SSE | 31,262 | 9.2% |
| 機器端 Cloud 協定 | 21,741 | 6.4% |
| 本機 SQLite | 16,145 | 4.8% |
| CLI 與啟動接線 | 14,694 | 4.3% |
| 官網 | 11,802 | 3.5% |
| Cloud API ＋ MongoDB | 10,940 | 3.2% |
| 桌面殼與其他命令 | 6,478 | 1.9% |
| Cloud relay | 6,338 | 1.9% |

```text
Console           ███████████████████████████████ 30.7
協調與規則         ██████████████████              18.2
本機接點           ████████████████                15.8
HTTP／SSE         █████████                       9.2
機器端 Cloud      ██████                          6.4
其餘六組           ████████████████████            19.6
```

百分比各自四捨五入，合計可能差 0.1%。這張圖包含公開 Clawdline repo 與 Cloud 服務 repo 的實作，**不包含 Claude、Codex、tmux 或 Cloudflare／MongoDB 本身的程式碼**。

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

**證據與邊界**：本機實作以 `docs/architecture.md`、`docs/cloud.md`、`internal/adapters/store`、`internal/transport/{http,cloud}`、`web/console/src/cloud` 為準；Cloud 服務以其 `api/src/db/mongo`、`relay/src/account-do.ts`、`relay/src/lib/cache.ts` 為準。行數依上述路徑對兩個 checkout 的追蹤檔逐檔歸類。本文件核對的是 2026-10-09 的原始碼，不宣稱此時生產環境每個部署都與 checkout 同版。
