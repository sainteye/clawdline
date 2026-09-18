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

### 閘門上線之後，量 :7727 要先認證

新核心照舊版的規則把每條路由都放在閘門後面，本機也沒有例外；不需要 token 的只有頁面本身、
`/assets/`、`/strings/`、圖示、`/v1/health`、`/v1/strings` 與 `/v1/auth/*`。**沒有任何開關或環境變數能關掉它。**

- **瀏覽器**：在跑 daemon 的那台機器上執行 `clawdline open`（`CLAWDLINE_NEXT_PORT`、`CLAWDLINE_NEXT_DIR`
  要跟 daemon 一致）。它替這個瀏覽器發一個自己的裝置「Browser on this Mac」，打開
  `http://127.0.0.1:7727/v1/auth/open#t=…`。token 在 fragment（瀏覽器不會送出、不會寫進 log），
  那一頁用 `/v1/auth/adopt` 換成 `clawdline-next` cookie，再轉到 `/`。之後同一個瀏覽器裡的量測照舊：
  measure2.js 在頁面裡 `fetch('/v1/…')`，同源的 760 寬 iframe 與 hidden 分頁 shim 都吃同一個 cookie。
  這個裝置只能讀；要量 send、interrupt 這類寫入，改用 `clawdline open --send`。
  量完在該分頁執行 `fetch('/v1/auth/logout', {method: 'POST'})`，cookie 清掉，裝置也一起撤銷。
  沒有瀏覽器可開時，`clawdline open --print` 只印網址（網址本身就是鑰匙，別貼到別處）。
- **cookie 名字刻意跟舊版不同**：舊版叫 `clawdline`，新版叫 `clawdline-next`。瀏覽器的 cookie 不分 port，
  同名會互相覆蓋，同一個視窗就沒辦法同時量 7717 與 7727。
- **curl／Node**：帶本機 token。
  `curl -H "Authorization: Bearer $(cat ~/.config/clawdline-next/local-token)" http://127.0.0.1:7727/v1/sessions`。
  `local-token`（0600）是 daemon 啟動時寫的，可以讀也可以 send、管理裝置——只給本機腳本用，
  不要貼進瀏覽器，也不要拿它對真的 session 試 send。
- **派工與 `/v1/orchestrator/*` 的寫入**：要 `-H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline-next/orchestrator-token)"`。
  瀏覽器裡的裝置一律 403，Dashboard 的派工按鈕也是（舊版同樣不讓配對的裝置派工）。
- 沒帶 token 的回應是舊版的原文：`401 {"error":{"code":"unauthorized","message":"This needs a paired device.",…}}`。

### 閘門看的那一份路徑（2026-09-18，reviewer 第四輪 F1）

**授權判斷與路由分派看同一個字串。** 舊版的 `request.path` 是原始的 request target，它的閘門與 switch
比對的是同一份；Go 版的閘門本來讀 `r.URL.Path`（net/http 已經解過碼），而 `http.ServeMux` 比對的是
`cleanPath(EscapedPath())`，於是 `/v1/sessions/..%2F..%2Fv1%2Fauth%2Fx/git` 在閘門眼裡是公開的
`/v1/auth/…`，在 mux 眼裡仍是一個 session id——**不帶任何 token 就跑進 handler**，而 `/git`、`/info`
在查表前會先盤點整台機器（ps、tmux、osascript）。同一類第二種拼法是 `…/documents/../../../../v1/health`：
閘門清乾淨之後看到公開的 `/v1/health`，`withDocuments`（在 mux 前面、比對原始路徑）仍然認得是文件路由。

修法（`internal/transport/http/gate.go`）：

- `routePath(r)`＝`cleanPath(r.URL.EscapedPath())`，**閘門與每一處用路徑做決定的地方都讀這一份**
  （`openPath`、`machineScoped`、`taskSecretRoute`、`writePolicy`、`authRoute`、session 的
  `{id}/{verb}`、`/v1/places/`、`/v1/projects/`、`/v1/artifacts/images/`、`/v1/orchestrator/tasks/`）。
  要名字的地方一律「先切段、再解碼一段」，never 整條路徑解完再切。
- `readablePath`：任何一段解碼後含 `/`、`\`、NUL，或本身是 `.`／`..`（含 `%2e%2e`、`%2F`、`%2f` 等拼法），
  在閘門最前面回舊版的 `400 {"error":{"code":"bad_request","message":"Could not read that request"}}`
  （`RemoteServer.response(for:)` 的 `.badRequest`）。舊版對同樣四個請求是 401，因為它沒有這條規則、
  單純過不了 token；新版多這一道，**這是刻意的差異**。
- 回歸測試：`gate_test.go` 的 `TestGateReadsAPathOneWay`、`TestReadablePath`（修正前 12 個拼法有 8 個直接 200）。

### 文件頁的到達方式（F2）

舊版 `main.js` 的 `navigate` 對文件頁固定 `Pages.go(name, {hash:false})`——**不寫位址**。Go 版原本反過來
寫 `location.hash="#page=documents"` 再靠 hashchange 繞回來，在手機寬度（<900）等於沒有實作：寫 hash 是
same-document navigation，會發 `popstate`，而 `popstate` 在這頁是手機的返回手勢
（`input/action-confirm.js`），於是 `closeDetail` 把剛寫進去的位址用 `replaceState` 換掉，接著才輪到
hashchange，讀到的是空的，就回到清單。現在改成 `requestPage({page, hash:false})`，App 直接 `Pages.go`，
760 與 1129 都會開，而且兩種寬度都不再改 hash——與舊版一致。

### 文件的 project root（F11）

`<session cwd>/artifacts` 是 symlink 時，舊版無條件跟隨（`ProjectArtifact.projectRoot`），這台機器也真的
這樣用（`~/code/clawdline/artifacts -> ../clawdline-cloud/artifacts`，文件頁 101 列）。所以**預設仍然跟隨**，
但加了一道地板：解析後的 root 不可以是家目錄、不可以包含 session 自己的工作目錄、不可以是檔案系統根
（`ln -s ~ artifacts` 那一條就是被這道擋掉的）。要完全不跟隨的機器，在自己的 `config.json` 寫
`documents_contain_project_root: true`。

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
| 裝置頁本機那台機器的 `platform` 寫死 `macos`（照舊版 `net/live.js`，在 `legacy/devices-bridge.ts` 的 `localMachines`） | 本頁不顯示這個欄位，所以畫面相同；但 Go 核心在 Linux／Windows 上跑時應換成實際的作業系統（連帶 `name`／`label` 的「這台 Mac」），待三平台時處理 |
| 多個 :7727 分頁會吃滿 Chrome 對同一 host:port 的 6 條連線 | child C 觀察到一個請求排隊 95 秒。**舊版也會**：裝置頁 child（3f2a0d24）量測時 7717 在 Chrome 裡排隊超過 5 秒，同一時間 curl 7 ms 就回來；是瀏覽器對同一 host:port 的連線上限，不是新版的退化 |

### 其他頁面（側欄）

| 頁 | 狀態 | 後端 |
|---|---|---|
| 設定（本機） | ✅ `c9f60c1` | `/v1/settings`，寫 `~/.config/clawdline-next/config.json`；看板區塊要等 `/v1/board` 帶 `board.enabled` |
| 用量 | ✅ `303c9e4`、`2df060b` | `/v1/orchestrator/usage/analytics*`、`project-worktrees`；數字讀舊 app 的 `usage.sqlite3` 副本（`plan.md` §4），沒有就退回 transcript；`portfolio.features` 未移植 |
| 專案 | ✅ `c3a5bb7`、`ef77892` | `/v1/places`、`/v1/projects`；每列進行中數量要等 Board 進度投影 |
| 裝置、方案（本機、沒有 Cloud） | ✅ `836344b` | 不需要；與舊版本機頁一樣是前端常數 |
| 看板、驗證帳本 | 未開始 | Board 是整個功能（舊版 `/v1/board` 有 19 個欄位，含 entitlement）；帳本是 `/v1/orchestrator/usage/verification-ledger` |

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

## Reviewer 第三輪（task 5ba0274e，對 master `836344b`）

第一次有獨立的眼睛看五個新頁面與 session 增量。1129 與 760 兩種寬度，同源 iframe，dpr 2.2，兩邊 BackCompat。

| 範圍 | 比了 | 差 |
|---|---|---|
| 裝置頁 | 1,510 項 ×2 寬 | 0 |
| 方案頁 | 2,560 項 ×2 寬 | 0 |
| 設定頁 | 3,965 項 | 33，全部已知（看板區塊、通知權限狀態） |
| 專案清單 | 21,569 項 | 400，全部已知（進行中數量） |
| 用量頁（1129） | 408,351 項 | 扣掉已知的 features 後 0；Load more、明細、Recent agent work 各 0 |
| 用量 API（同一秒） | 30 天 27 列、7 天 18 列 | 順序與數字全同，只差 `portfolio.features` |
| 行為 | 104＋21＋20 步 | 全部相同（Tab 順序由 DOM 推算，沒有真的按） |
| session 增量（%798、%712） | r、設定頁切換、助理圖示、`.limits` | 相同；`/info` 的 limits 只差 `readAtMs` |

新發現兩項，都是 minor：
- **R1**：`r` 不會重畫隱藏中的 `#settings-order` 文字，進設定頁時才更新。設定頁開著時 `r` 本來就不作用，
  使用者看不到舊字，所以不修。
- **R2**：失敗路徑（stale 橫幅、`503 usage_analytics_busy`、429、看板讀不到的後備、清單更新失敗）沒有任何
  獨立比對。這是覆蓋缺口，不是缺陷。

未比對：760 寬的 session 增量、真的按 Tab 或 Escape、失敗路徑。結論：**扣除已知清單，已量到的範圍可以宣稱 1:1。**

## 現在的狀態（2026-09-17 早上）

五個側欄頁面與狀態列的方案額度已落地（見上表）。免費版的配對與認證在分支 `free-auth`，
等 Codex 的安全審查（`docs/remote.md`）。以下是清晨時的記錄。


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
| 選單列（`main.swift` buildMenu） | 約 300 行 | ✅ `841b870`：文字與順序逐字對齊，用 System Events 與舊 app 比對 |
| 全域熱鍵（`HotKey.swift`） | 138 行 | ✅ `841b870`：設定讀 `~/.config/clawdline-next`，沒設定就不註冊 |
| 登入時啟動（`SMAppService`） | — | ✅ `841b870`：開關，預設關閉；測試未打開 |
| Dock 圖示 | — | ✅ `360c9fd`：與舊 app 逐位元組相同。兩個 app 同時跑會有兩個相同圖示與 ✳，這是忠實復刻的結果 |
| 原生「設定⋯」 | — | ✅ `c9f60c1`：⌘, 與選單列都打開 `#page=settings`；熱鍵錄製在殼裡。只做過型別檢查，殼沒有實際跑過 |
| 配對 alert、本機 token | — | ✅ `5cca6b1`（審查後合併）：只有「忽略」的 NSAlert、cookie 注入。2026-09-17 以 `de7c5da` 重新打包，對開著閘門的 7727 實測：殼帶著本機 token 載入，`booting=false elements-with-id=265 rows=8`。alert 沒有在這台機器上觸發過 |
| 內嵌瀏覽器（舊版沒有） | — | ✅ 見下一段：視窗上方一條原生列，本機 console 與任意網址兩個 WKWebView，cookie 分家 |
| 快捷面板（`Controller` + `Panel`） | 5,400 行 | 目前以 webview 視窗代替 |
| 原生設定（`Settings.swift`） | 3,822 行 | 未開始 |
| 導覽（`Onboarding.swift`） | 1,468 行 | 未開始 |
| 瀏海島（`NotchIsland.swift`） | 1,148 行 | 🔧 `shell/darwin/NotchIsland.swift`：兩耳、吉祥物（連 `Mascot.swift` 的取樣與繪製）、等待／在跑／跑完、tooltip、點角色開 console、點數字送 `/v1/sessions/{id}/focus`；`notch: false` 就整個不建立。**未編譯、未執行**，等 root 驗收時的那一次 build；跨平台對應見 `docs/cross-platform.md` |

`7bbdd1a` 之後，打包出來的 app 在 WKWebView 裡實際畫出即時清單（殼回報 `booting=false elements-with-id=79 rows=11`）。

### 內嵌瀏覽器（2026-09-18）

使用者要的是「在新 app 裡直接用 clawdline 的所有功能」，包含 Cloud。**舊 app 沒有這一面可抄**——它整個
沒有 WKWebView（`grep -rl WebKit Sources/` 只有 Onboarding／Settings／RemotePage／WebPush，都不是 web view），
所以這裡沒有「照抄」可言，只有「不要發明新的樣式系統與新的字」。

做法：**兩個 WKWebView，不是一個會導航的**。

| | console 那個（`Shell.web`，本來就有的） | web 那個（`ExternalWeb`，新的） |
|---|---|---|
| 可以去哪 | 只有 `http://127.0.0.1:<port>`；其他一律取消，交給系統瀏覽器（`NSWorkspace.shared.open`，舊 app 對所有連結的做法） | 任何 http／https；**console 的位址被拒絕**，那裡只能從旁邊的分頁按鈕去 |
| cookie | `WKWebsiteDataStore.default()`，本機 token 寫在這裡 | 自己的 store（macOS 14+ 用固定 UUID 的 `WKWebsiteDataStore(forIdentifier:)`，所以 Cloud 的登入活得過重開；13 退回 `.nonPersistent()`） |
| 麥克風 | 照舊 grant（`Microphone.swift`，只給 console 的 origin） | 一律 deny |
| 注入的 script | fleet bridge、settings 字詞 | 沒有 |

**一個會導航的 view 要自己記得什麼時候把 token cookie 放進去、什麼時候拿出來，而它要記對的東西是一組
憑證。**兩個 store 是同一條規則交給 WebKit 執行：token 從來沒寫進去的罐子，不管頁面做什麼都交不出來。
代價是兩邊不共用登入——對「本機 daemon」與「託管 console」來說本來就該如此。

上方那條列是原生的（一個頁面要再開第三個 web view 才畫得出來）。顏色與間距直接用 `legacy/tokens.css`
的值（`Browser.swift` 的 `Ink`，變數名一字不改）：`.conn` 的 pill 當按鈕、`.stale .go` 的 accent 當選中、
`.top` 的 14／12 間距、`.composer .box` 的圓角當網址框。字詞取自舊版 `Copy+Chinese.swift` 原本的屬性名：
`homeLocalTitle`（本機瀏覽器）、`homeCloudPreviewTitle`（Clawdline Cloud 預覽）、`webInfoRefresh`（重新整理）；
**上一頁／下一頁舊版整份字串目錄裡沒有任何對應的詞**，所以就是 `‹` `›` 兩個符號，不編字。

實測（打包後真的開 app，殼的 stdout 與 System Events）：console `rows=14`；切到 `app.clawdline.com` 載入成功
（未登入畫面）；`cookie-wall: separate-stores=true`、console store 有 `clawdline-next@127.0.0.1`、web store
`(none) token-cookies-here=0`；網址列、上一頁、`target=_blank`、外部頁面導向 console 被拒、console 的外連結
交給系統瀏覽器，都各驗過一次。詳見 task `03e6be52` 的 `artifacts/report.md`。

### 排程（2026-09-18，task `1f9ca362`）

清單 `details#schedules`、`#schedule-history`、`#schedule-form`、`#schedule-delete-confirm` 照
`view/schedules.js`、`net/schedules.js`、`input/schedule.js`、`input/schedule-history.js` 逐行移植到
`pages/schedules.tsx`（六個原檔照抄進 `legacy/js/`，守衛 72 檔相符）。對 7796 實際操作過：清單、執行紀錄、
編輯存檔、`+` 建立、刪除確認。**沒有和 7717 做畫面逐屬性比對**。

## 下一步建議

1. 唯讀 Swift store——✅ `b3959bf`：同一秒 13 列 260 欄只差 8 欄（皆與 store 無關）；task 列表 1,045 欄 0 差異；
   Clawdfather、協調等待、交付勾、標題、closeability（version 逐字相同）一致。adapter 只有一個 `O_RDONLY` 的開檔，
   讀 `orchestrator.json`、`coordinator.json` 與 `config.json` 的 `session_titles`。
   N5（一列的圖示）來自 Swift 記憶體裡永不失效的快取，store 沒有這個事實——**刻意不繼承**。
2. overlay：Session 資訊、確認框、鍵盤說明——✅ `9eb0a58`（鍵盤卡 3,241 項 0 差異、確認框 0 差異、行為 22 項相同）。
   接線 ✅ `fd90d22`：標題與選單開 Info 卡、關閉走確認框；暫代匯出收進 `legacy/overlay-bridge.ts`。
3. 頁面（`f0d6b90` 之後，`pages/*.tsx` 自己註冊，不再改 App）：專案頁 `%846`、用量頁 `%847`、設定頁（本機）`%848`。
   契約產出檔不給 child 認領，整合時由 root 統一重生。
4. 依畫面效益補後端：Session 資訊 sheet（`#info`）、開新 session（`#start`）、確認框（`#action-confirm`）、
   在 Mac 上顯示——這四個是詳情標頭與選單最常用的入口。
3. 側欄頁面：用量頁的後端最接近（已有 `/v1/orchestrator/usage` 與 transcript 用量）。

## 還沒做的（2026-09-17 早上）

- 原生：導覽（`Onboarding.swift` 1,468 行）、聽寫（`Voice.swift` 633 行）、瀏海島（`NotchIsland.swift` 1,148 行）、
  原生 Remote 設定。
- 頁面與 sheet：看板、驗證帳本、開新 session（`#start`）、`#command`、Git 面板、即時畫面、
  在 Mac 上顯示、常用句、附圖。
- 遠端：網頁的配對入口（`door.js`）、tunnel、Cloud bridge（`docs/remote.md`）。

## 額度

2026-09-17 深夜：Claude 7 天 85%、Codex 7 天 92%。所以今晚是**少量高價值**的派工，不做大量扇出。
2026-09-17 早上：Claude 7 天 95%（約 16 小時後重置），Codex 92%。只做修正，不開新功能。
