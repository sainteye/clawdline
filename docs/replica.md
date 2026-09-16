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
| 清單列的 working 狀態（旋轉動畫） | 🔧 派工中 | child C |
| detail-head（返回、身分、在 Mac 上顯示、⋯ 選單） | ✅ 標記與字串對齊 | root |
| transcript（#tx） | 🔧 派工中 | child A |
| composer（form#composer） | 🔧 派工中 | child B |
| status-line（footer） | 🔧 派工中 | child B |

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

## 額度

2026-09-17 深夜：Claude 7 天 85%、Codex 7 天 92%。所以今晚是**少量高價值**的派工，不做大量扇出。
