# 1:1 復刻：做法、進度、下一步

> 目標（使用者原話，2026-09-17）：「mac 版有的功能都盡量 1:1 的對齊到，尤其是畫面的細節」。
> 做法：派工＋reviewer。限制：不要把時間浪費在 test 與 build。

## 為什麼「照抄」而不是「重寫」

第一次重寫畫面時，我從截圖推論樣式，結果發明了舊版沒有的 class 名稱、編了字串目錄裡沒有的中文、
把 `tint()` 的目標色與比例都猜錯。讀原始碼比從結果推論可靠，所以現在的做法是：

| 層 | 做法 | 位置 | 守衛 |
|---|---|---|---|
| 樣式 | 12 個 CSS **逐位元組複製** | `web/console/src/legacy/*.css` | `tools/check-legacy-css.sh` |
| 狀態投影、字串、像素 | 14 個 JS **逐位元組複製**（derive / i18n / pixels / selection…） | `web/console/src/legacy/js/` | 同上 |
| 字串目錄 | 778 鍵的 `zh-Hant.json` **複製**，daemon 寫進 `<!-- clawdline:strings -->` | `web/console/public/strings/` | 同上 |
| 專案圖示 | Go 移植 `ProjectIcon.swift`，**321/321 逐格相同** | `internal/domain/icon/` | 對 `/v1/projects` 實測 |
| React | 只負責產生**同樣的 DOM 與 class**，文字與投影一律經 `legacy/bridge.ts` | `web/console/src/` | reviewer |

守衛有三態：0 相同、1 漂移、2 無法判定（來源不在）。**漂移時的正確動作是去看來源改了什麼、有意識地重抄，
絕不是改 manifest。**

## 驗收的方法

不靠眼睛。兩邊（`:7717` 舊版、`:7727` 新版）在**同一個瀏覽器視窗、同一個縮放**下跑同一段 JS，
比對 `getComputedStyle` 與 `getBoundingClientRect`。這個方法已經抓到過：

- 列高 219 vs 86：`.state` 外面多包了一層沒 class 的 span，破壞 flex（改成跟原本一樣直接設 innerHTML）
- 列高 219 vs 86：`canvas.spin` 沒被畫，停在 canvas 預設的 300×150（原本的註解正好警告這件事）
- 「機器 · %801」vs「Mac 電腦 · 這台 Mac」：wire 上本來就沒有 machine，是舊版前端補的

## 進度

### Session 清單頁（主畫面）

| 區塊 | 狀態 | 負責 |
|---|---|---|
| header.top（品牌、counts、conn） | ✅ 標記與字串對齊 | root |
| nav.sidebar（七頁＋Dashboard） | ✅ 標記對齊；未有後端的頁面 disabled | root |
| 清單列 li.row（圖示、標題色、機器、路徑、tty、assistant logo） | ✅ 7/8 列高度相同 | root |
| 清單列的 working 狀態（旋轉動畫） | ✅ 8 列 × 96 項零差異（fixture） | child C `55a0f16` |
| 清單列的進度行 `line` | ✅ 解析規則照移植，與舊版同一分頁抽出同一行 | root `25a57ae` |
| detail-head（返回、身分、在 Mac 上顯示、⋯ 選單） | ✅ 標記與字串對齊 | root |
| transcript（#tx） | 🔧 派工中（02:29 斷線，03:20 喚回） | child A |
| composer（form#composer） | ✅ 無 session 時 17/17 元素零差異 | child B `cd9a673` |
| status-line（footer） | ✅ 5/5 元素零差異；ctx/files/deploy/limits 沒有資料來源 | child B `cd9a673` |

### 已知、刻意延後

| 項目 | 為什麼延後 |
|---|---|
| task chip 與子任務縮排（`S.tasks`） | 舊版要 `{id,title,state,created,finishedAt,child.terminalId,root.terminalId}`；Go store 沒有 title、child 分頁、完成時間，要改 store 結構。兩個 app 的 store 刻意分開，所以這台機器上只會對 clawdline-go 自己派的任務顯示，畫面效益低 |
| ⋯ 選單的「在 Mac 上顯示／Session 資訊／即時畫面／Git」 | 各自需要後端路由，目前 disabled 而非移除 |
| composer 的附圖、語音、skill menu | 同上 |
| 多個 :7727 分頁會吃滿 Chrome 對同一 host:port 的 6 條連線 | child C 觀察到一個請求排隊 95 秒。舊版架構相同，是否也會發生尚未比對 |

### 其他頁面（側欄）

全部需要後端，舊版 83 條路由裡新核心沒有的部分。排在主畫面驗收之後。

| 頁 | 舊版檔案 | 後端 |
|---|---|---|
| 用量 | `usage.css` 294 行 | `/v1/orchestrator/usage/analytics*`（部分已有 `/v1/orchestrator/usage`） |
| 專案 | `projects.css` 184 行 | `/v1/projects`、`/v1/places` |
| 驗證帳本 | `ledger.css` 164 行 | `/v1/orchestrator/usage/verification-ledger` |
| 裝置、方案、設定 | `devices.css` / `plan.css` / `sheets.css` | 多數屬雲端版（2026-09-15 已拍板不在本機版） |

### Dashboard

原本那個監控面板保留成側欄的一個選項（使用者 2026-09-17 同意），樣式全部限定在 `.dashboard-page` 底下，
不會漏到復刻的頁面上。

## Reviewer 第一輪（2026-09-17 03:28–03:39，task ee7741a5）

同一分頁、同一視窗（1180×772，dpr 2.2，兩邊 BackCompat），34,401 個屬性逐節點比對。
結論：**還不能宣稱 1:1**，新發現 41 項。報告原文在 `/tmp/.clawdline/ee7741a5-…/artifacts/report.md`。

已經 0 差異（扣除已知）的：對話區四種訊息、composer（已開／空狀態）、一般 idle 列、working 列本體、主格線、`#conn`、status-line 空狀態。

| 編號 | 問題 | 處理 |
|---|---|---|
| #1–3、#9、#16–17 樣式 | 只抄了 12 個 CSS，抽屜的規則在 `pages.css` | root `0b5a74f`：29 個全抄，守衛 46 檔 |
| #4–5、#7–8、#10–15、#32 | 抽屜項目、篩選列按鈕、清單外框、brand logo、counts、#conn title | ✅ `2b1cfbb`：header、清單窗格對齊；其餘只剩刻意 disabled 與資料差異 |
| #25–37 | 詳情標頭與 ⋯ 選單 | ✅ `ca9f93a`、`f1f61f1`：三種狀態零 rect 差異 |
| #6 與 transcript 資料 | 打開沒捲到底；工具 subject 全是「…」 | ✅ `14d93b7`：兩個 session wire 逐筆零差異，另 7 抽 6 完全相同 |
| #21、#22、#38、#39 | 背景 shells、標題來源、status-line 模型名與花費 | ✅ `bb8a8a4`：shells 開關實測一致；標題 10 列中 7 列一致（其餘 3 列的標題來自 Swift store）；模型名與花費同一 session 兩邊相同 |
| #16–20 | Clawdfather 皇冠與 chip、coordination-wait、交付勾 | **需要使用者決定**：資料在 Swift app 的 store，兩個 app 的 store 依設計分開。要不要讓 Go 唯讀 `~/.config/clawdline`，是架構決定 |
| #41 | 所有 overlay（Session 資訊、開新 session、確認框、語音…） | 之後的波次 |

## Reviewer 第二輪（task 618bd064，對 HEAD `bb8a8a4`）

同一分頁 1022×739、dpr 2.2、兩邊 BackCompat；760 寬用同源 iframe 量。報告原文在
`/tmp/.clawdline/618bd064-…/artifacts/report.md`。**結論仍是還不能宣稱 1:1。**

第一輪 41 項：**已消失 26**、部分 4（#4、#9、#10、#22）、仍在 10（其中 7 項屬已知，非已知的 #23、#24、#40 都是 subtle）、#32 未重量。

已對齊：抽屜、header、brand、清單外框、每一列、列的 hover／focus、transcript 137 則逐則相同、捲到底、詳情標頭、⋯ 選單、狀態列的模型名與花費。

新發現：

| # | 嚴重度 | 內容 | 處理 |
|---|---|---|---|
| N1 | **blocking** | 760 寬點列後看不到對話：舊版 `data-view`（手機）與 `data-pane`（桌面）是兩個屬性，新版合成一個，兩組 CSS 都不成立 | ✅ `e339955` |
| N2 | visible | 桌面版第一份清單到達時不會自動打開第一列 | ✅ `e339955` |
| N3 | visible | 沒開 session 時少了 home hero | ✅ `e339955` |
| N4 | visible | 沒有 ↑↓／Enter／Escape／`/`，選取與打開綁在一起，沒有凍結排序 | ✅ `e339955`、`5f442f5` |
| N5 | visible | `%832` 的圖示與標題色不同 | 多半與 #22 同源（舊版依 Swift store 的 task 決定），歸入等你決定的那一項 |
| N6 | subtle | 沒有 `#page=` 路由，Dashboard 會卸載 `main#app` | ✅ `e339955` |
| N7 | subtle | 幾個看不見的屬性 | 暫不處理 |

未比對（不算通過）：排程列（新 daemon 0 筆）、unknown／waiting 列、骨架時序、失敗與斷線情境。

## 窄範圍 reviewer（task a5f1b0fc，對 HEAD `5f442f5`）

只驗 N1–N4、N6。**結論：全部可以關閉。** 760 寬點列／返回／瀏覽器上一頁／`history.state`、桌面 ⌘J、
自動打開、home hero（兩種寬度各 620 項，只差背景圖網址的 origin 與打包工具省略的等價漸層 stop）、
鍵盤 class 順序、`#page=` 路由與焦點，兩邊一致。

未比對（不算通過）：resize 轉換、觸控、帶 `#page=` 的冷啟動、g／G、篩選框內的 Escape、凍結排序的實際效果。
兩邊的 `hero-orchestration-v4-task-clinic.webp` 都回 404——舊版本身的缺陷，照樣繼承。

## 現在的狀態（2026-09-17 清晨）

**Session 清單頁**：兩輪 reviewer 加一輪窄驗收之後，除了下面兩類，已量到的部分與舊版一致。

1. **使用者已決定（2026-09-17 早上）：Go 版唯讀 Swift store。** Clawdfather 皇冠與 chip、coordination-wait、
   交付勾（#16–20）、3 列標題（#22）、`%832` 的圖示（N5）、task chip 與縮排，交給 child `%843`。規則見 `plan.md` §4。
2. **沒有後端，刻意 disabled**：在 Mac 上顯示、Session 資訊、即時畫面、文件、我傳出的訊息、常用句、Git、
   附圖、語音、⌘I 等 sheet、所有 overlay（#41）、status-line 的 ctx／files／deploy／limits、task chip 與縮排、
   `#conn` 的版本、帶圖片標記的訊息、側欄的裝置／專案／方案／設定頁。

**其他頁面**：還沒開始。

## macOS 原生殼

webview 之外，舊 app 的原生面：

| 舊版 | 規模 | 狀態 |
|---|---|---|
| 選單列（`main.swift` buildMenu） | 約 300 行 | child `%845` |
| 全域熱鍵（`HotKey.swift`） | 138 行 | child `%845`：設定讀 `~/.config/clawdline-next`，不與舊 app 搶同一組 |
| 登入時啟動（`SMAppService`） | — | child `%845`：預設關閉，測試不打開 |
| 快捷面板（`Controller` + `Panel`） | 5,400 行 | 目前以 webview 視窗代替 |
| 原生設定（`Settings.swift`） | 3,822 行 | 未開始 |
| 導覽（`Onboarding.swift`） | 1,468 行 | 未開始 |
| 瀏海島（`NotchIsland.swift`） | 1,148 行 | 裝飾，最後 |

`7bbdd1a` 之後，打包出來的 app 在 WKWebView 裡實際畫出即時清單（殼回報 `booting=false elements-with-id=79 rows=11`）。

## 下一步建議

1. 唯讀 Swift store（`%843` 進行中）。
2. overlay：Session 資訊、確認框、鍵盤說明——✅ `9eb0a58`（鍵盤卡 3,241 項 0 差異、確認框 0 差異、行為 22 項相同）。
   **待 root 接線（等 `%843` 放開檔案）**：`Detail.tsx` 改用 `requestInfo`／`requestConfirm`／`useClosingId`、刪掉兩段式關閉
   （逐行說明在 P1 報告的「需要 root 接上的 Detail.tsx」一節）；`overlays/legacy.ts` 的暫代匯出搬進 `bridge.ts`。
3. 依畫面效益補後端：Session 資訊 sheet（`#info`）、開新 session（`#start`）、確認框（`#action-confirm`）、
   在 Mac 上顯示——這四個是詳情標頭與選單最常用的入口。
3. 側欄頁面：用量頁的後端最接近（已有 `/v1/orchestrator/usage` 與 transcript 用量）。

## 額度

2026-09-17 深夜：Claude 7 天 85%、Codex 7 天 92%。所以今晚是**少量高價值**的派工，不做大量扇出。
