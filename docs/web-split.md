> **這是一份規劃，不是現況說明。** 撰寫於 2026-08-24，所有行號與行數都量測自
> `Resources/web/index.html` 在 commit `0199775` 時的內容（9889 行）。拆檔一旦開始，
> 這裡的行號就會失效——它們的用途是把每一刀切在哪裡記錄下來，供拆檔期間對照與事後追溯，
> 不是給讀者拿去對照當前的檔案。實際進度見 git log 裡 `split/` 開頭的分支。

# Resources/web/index.html 拆檔方案

一份規劃文件。**沒有動到 repo 裡任何檔案**——底下所有行號都是對著今天工作區的
`Resources/web/index.html` 重新數出來的，不是沿用先前的概數。

## 0. 現況與這份規劃的兩條定理

| 事實 | 數字 | 怎麼量的 |
| --- | --- | --- |
| `Resources/web/index.html` | 9889 行 / 507,070 bytes | `wc -l`, `ls -l` |
| head + 開場 inline script | 1–97 | `<script>` 在 80，`</script>` 在 96 |
| `<style>` … `</style>` | 98 … 2200（規則本體 99–2199，2101 行） | `grep -n '^<style'` |
| HTML body | 2202–2610（409 行） | `<header class="top">` 在 2202 |
| `<script>` … `</script>` | 2611 … 9889（JS 本體 2613–9887，7275 行） | 整段包在一個 IIFE 裡 |
| 近 14 天的 churn | 34 個 commit，+11,352 / −1,463，平均 +334 / −43 | `git log --since="14 days ago" --numstat` |

拆檔的動機是 merge 衝突面積，不是效能，也不是「單檔很醜」。34 個 commit 平均每次改 334 行，
這些改動散在 CSS、body、JS 三個互不相干的區域，但 git 看到的是同一個檔案。

這份規劃從頭到尾靠兩條定理撐著，兩條都可以驗證：

> **定理一（CSS）**：把 99–2199 切成 N 段、在 `<head>` 裡照**原本的先後順序** `<link>` 進來，
> 串接後的 cascade 與今天逐位元組相同。順序一改，就不再等價——第 7 節有一組實際會壞掉的例子。

> **定理二（JS）**：今天整份 JS 是單一 IIFE、由上而下執行一次，所以**任何在「執行期」（module
> body 跑的時候）就讀取的跨模組名字，一定宣告在讀它的那一行之前**。往後指的引用只可能發生在
> 函式體內，也就是 boot 之後才跑。實際掃過的結果：整份檔案在 top-level 執行的跨模組讀取只有
> `els`、`ASK_MARK`、`uuid`、`storedBool`、`drawIcon`、`MOCK*` / `reduced` / `phone` 這幾個，
> **全部落在 leaf 層**。所以第 2 節列出的 24 組循環相依，在 ES module 下全部是安全的。

---

## 1. 目標檔案樹

磁碟佈局刻意等於 URL 佈局，理由見第 4、5 節（伺服器只要一條路由，dev server 不必改寫路徑）。

```
Resources/web/
  index.html                    ~520 行   （head + body markup + 12 個 <link> + 1 個 <script type="module">）
  hero-orchestration*.webp                （不動，維持自己的路由）
  app/
    css/    12 個檔，2101 行
    js/     36 個檔，7275 行
```

### 1.1 CSS（12 檔 / 2101 行）

`<head>` 裡的 `<link>` 必須**照這個順序**排（定理一）。所有區間都是連續的，沒有搬動任何一行。

| 檔案 | 來源行 | 行數 | 裝什麼 |
| --- | --- | ---: | --- |
| `app/css/tokens.css` | 99–171 | 73 | `:root` 變數 + 一組 `(pointer: coarse)` 的 token 覆寫。**純 design token** |
| `app/css/base.css` | 172–227 | 56 | reset、`html/body`、`.booting`、`.scroller`、`.mono`、`:focus-visible`、`::selection` |
| `app/css/header.css` | 228–334 | 107 | `.top`、`.brand`、`.counts`、`.conn`、`.stale`、`.starting` |
| `app/css/shell.css` | 335–430 | 96 | `.app` / `.pane` 版面 + `.filter-row` + `.notify` |
| `app/css/list.css` | 431–647 | 217 | `.row` 全家（含 `waiting/unknown/finished` 狀態、`.task-chip`、`.kid`）、`.empty`、`.ptr`、`.skel*` |
| `app/css/detail.css` | 648–860 | 213 | `.detail-head`、`.agent-head`、`.session-actions`、`.git-panel`、`.chip` |
| `app/css/transcript.css` | 861–1188 | 328 | `.tx`、`.home-hero`、`.entry` + Markdown、`.entry.folded` / `.toolline`、`.askbox` |
| `app/css/composer.css` | 1189–1569 | 381 | `.composer` 全家、`.skill-menu`、`.shot`、`.send`、`.live`、`.agents`、`.why` |
| `app/css/status-line.css` | 1570–1653 | 84 | `.status-line`（含它自己的 `max-width: 1080px` 查詢） |
| `app/css/sheets.css` | 1654–1928 | 275 | `.overlay` / `.sheet` 底盤、`.confirm-sheet`、`.sheet .place`（Start）、`.info-sheet` |
| `app/css/door.css` | 1929–2020 | 92 | `.door`、`.digits`、`.toast` |
| `app/css/responsive.css` | 2021–2199 | 179 | `(max-width: 899px)`、`(pointer: coarse)`、`(prefers-reduced-motion)` 三個查詢 |

12 個檔案是「衝突面積」與「請求數」之間的取捨點。想再細切的話，下面這些邊界都已經確認過是
乾淨的（前面的註解區塊跟著規則走）：

- `detail.css` → 648–774（detail/agent head + session actions）、775–820（git panel）、821–860（`.chip`）
- `transcript.css` → 861–1029（容器 + hero + entry + Markdown）、1030–1147（folded / toolrow）、1148–1188（ask 卡片）
- `sheets.css` → 1654–1723（overlay/sheet/confirm）、1724–1780（Start 的 find/places）、1781–1928（info sheet）
- `responsive.css` → 2021–2165（phone）、2166–2183（coarse pointer）、2184–2199（reduced motion）

`composer.css` 是唯一切不乾淨的一塊：`.composer .waiting`（1368–1376）、`.live`（1384–1405）、
`.stale .shut`（1406–1411，**這幾行其實屬於 header**）、`.live canvas`（1419–1424）、
`.composer .waiting .title`（1426–1441）是交錯的。要拆得先把同一組的規則挪在一起——那是一次
**會改變順序**的編輯，不能混在第一階段做。同樣被放錯地方的還有 `.row .agents-chip`（1549–1560，
屬於 list）。都記在第 6 節的收尾階段。

### 1.2 JS（36 檔 / 7275 行）

| 檔案 | 來源行 | 行數 | 裝什麼 |
| --- | --- | ---: | --- |
| **core（leaf 層，不 import 上層任何東西）** | | | |
| `app/js/core/env.js` | 2622–2709 | 88 | `params`、`MOCK`、`MOCK_WRITE/FLAKY/DOOR`、`reduced`、`phone()`、`atMac()`、`hasKeyboard()`、`trackVisibleViewport` |
| `app/js/core/esc.js` | 3154–3158（從 util 抽出） | ~8 | 只有 `esc()`。抽出來的理由見 2.3 |
| `app/js/core/i18n.js` | 2710–3055 | 346 | `T`（英文預設值）、`applyStrings`、`fill`、`words` |
| `app/js/core/state.js` | 3056–3122 | 67 | `storedBool` / `storeBool`、`S`、`optimisticBySession` |
| `app/js/core/dom.js` | 3123–3149 | 27 | `els` 與那份 id 清單 |
| `app/js/core/util.js` | 3150–3232（扣掉 esc） | 78 | `ASK_MARK`、`shortPath`、`clockOf`、`tint`、`uuid`、`toast` |
| `app/js/core/pixels.js` | 3233–3366 | 134 | `dpr`、`drawIcon`、`ASSISTANT_LOGOS`、`assistantLogo/Name`、`SPIN`、`drawSpinner`、所有 spinner 把手 + 那顆共用時鐘 |
| **net（傳輸層）** | | | |
| `app/js/net/api.js` | 新檔 | ~14 | `export let api` + `useApi()`。**不 import 任何東西**，見 2.2 |
| `app/js/net/build.js` | 3367–3432 | 66 | `Build`（版本落後的橫幅） |
| `app/js/net/handlers.js` | 3433–3487 | 55 | `handlers`：`sessions` / `tasks` / `hello` / `conn` |
| `app/js/net/fetch.js` | 3488–3557 | 70 | `jsonFetch`、`post`、`adoptToken`、`stripFragment` |
| `app/js/net/live.js` | 3558–3801 | 244 | `Live`（真的那個 API） |
| `app/js/net/mock.js` | 3802–4455 | 654 | `Mock`（fixtures + 假串流）。**最大的單一檔** |
| **door** | | | |
| `app/js/door/door.js` | 4458–4718 | 261 | `Door`、`suggestName`、六格數字輸入 |
| **view（畫面）** | | | |
| `app/js/view/derive.js` | 4719–4893 | 175 | `RANK`/`rankOf`、凍結排序、`ordered`、task 衍生、`grouped`、`byId`、`revisionOf` |
| `app/js/view/list.js` | 4894–5349 | 456 | `onSessions`、`render`、`renderConn/Counts`、`renderList`、FLIP 進出場、`buildRow`、`fillRow` |
| `app/js/view/transcript.js` | 5350–5819 | 470 | `renderDetailHead`、`renderTranscript`、ask 解析、tool row、摺疊、`entryHTML` |
| `app/js/view/markdown.js` | 5820–6142 | 323 | `richText` 一整套（表格、清單、inline、`safeHref`） |
| `app/js/view/composer.js` | 6143–6435 | 293 | `renderComposer`、`renderWaiting`、`menuHTML`、`renderAgents` |
| `app/js/view/static.js` | 6436–6576 | 141 | `paintStatic`：把 markup 裡的英文字換成 `T` |
| `app/js/view/waits.js` | 6577–6787 | 211 | `Waiting`、`Waits`、`Optimistic`、兩個骨架 |
| **session** | | | |
| `app/js/session/open.js` | 6788–6903 | 116 | `loadTranscript`、`atBottom/toBottom`、`openSession`、`closeDetail` |
| `app/js/session/agent.js` | 6904–7068 | 165 | 背景 agent 的開關與繪製、`select`、`move` |
| **input（互動與各張 sheet）** | | | |
| `app/js/input/keys.js` | 7069–7173 | 105 | `typing`、全域 keydown、`toggleOrder` |
| `app/js/input/swipe.js` | 7174–7305 | 132 | `SwipeRows` |
| `app/js/input/detail-actions.js` | 7306–7505 | 200 | Refresh chip、back、agents 列、conn、`SessionActions` |
| `app/js/input/git-panel.js` | 7506–7618 | 113 | `GitPanel` |
| `app/js/input/action-confirm.js` | 7619–7790 | 172 | `ActionConfirm` |
| `app/js/input/route.js` | 7791–7852 | 62 | hash 路由 + service worker 的 `message` |
| `app/js/input/settings.js` | 7853–7996 | 144 | `Settings` |
| `app/js/input/start.js` | 7997–8380 | 384 | `Start`（開新 session 的那張表） |
| `app/js/input/status-line.js` | 8381–8576 | 196 | `SessionFacts` + `StatusLine` |
| `app/js/input/info.js` | 8577–9015 | 439 | `Info`（session 資訊卡） |
| `app/js/input/push.js` | 9016–9213 | 198 | `Push` |
| `app/js/input/shots.js` | 9214–9404 | 191 | `Shots` + 貼上/拖放圖片 |
| `app/js/input/composer.js` | 9405–9689 | 285 | `rawMsgText`…`insertText`、`SkillPicker`、`submit` |
| `app/js/input/edges.js` | 9690–9807 | 118 | 鍵盤上方那條、下拉重整、layout 變動 |
| **入口** | | | |
| `app/js/main.js` | 2613–2621 + 9808–9887 | ~95 | 專案 mark、`boot`、`/v1/strings`、時間戳的 `setInterval` |

行數相加（CSS 2101 + JS 7275）= 9376，其餘 513 行是 head（97）、body markup（409）與
`<style>`/`<script>` 標籤本身。

`index.html` 拆完剩下的樣子：

```html
<meta charset="utf-8">
…（1–79，viewport、apple-touch-startup-image 那 20 條，一行不動）…
<script>…（80–96 的開場 script，維持 inline、維持 classic，理由見第 7 節）…</script>

<link rel="stylesheet" href="/app/css/tokens.css">
… 依序 12 條 …
<link rel="stylesheet" href="/app/css/responsive.css">

<link rel="modulepreload" href="/app/js/core/dom.js">
… （選配，見第 7 節「請求數」）…

…（2202–2610 的 body markup，一行不動）…

<script type="module" src="/app/js/main.js"></script>
```

---

## 2. 模組相依圖

下面這張圖是**掃出來的**，不是猜的：把 JS 區段依 1.2 的邊界切開，逐行剝掉區塊註解、行註解與字串
字面值之後，比對每個 top-level 名字的宣告位置與引用位置。

### 2.1 分層

```
                        ┌──────────────┐
                        │   main.js    │  entry：useApi() → boot()
                        └──────┬───────┘
                               │
        ┌──────────────────────┴───────────────────────┐
        │                                              │
   ┌────▼─────┐   ┌──────────┐                   ┌─────▼──────┐
   │ net/live │──▶│ net/     │                   │ view/static│
   │ net/mock │   │ handlers │                   └─────┬──────┘
   └────┬─────┘   └────┬─────┘                         │
        │              │                               │
        │              ▼                               │
        │   ╔══════════════════════════════════════════▼════════════╗
        │   ║  中層：view/* · session/* · input/* · door/*           ║
        │   ║  24 組互相呼叫的循環（見 2.4）。全部是「函式呼叫」層級 ║
        │   ╚═══════════════════════════╤═══════════════════════════╝
        │                               │
   ┌────▼───────────────────────────────▼─────────────────────────────┐
   │ leaf 層（誰都可以 import，它們誰都不 import）                     │
   │ core/env · core/esc · core/i18n · core/state · core/dom          │
   │ core/util · core/pixels · net/api · net/build · net/fetch        │
   └──────────────────────────────────────────────────────────────────┘
```

`net/fetch.js` 只用到 `core/env`（`MOCK`）與 `core/i18n`（`T`）；`net/build.js` 只用到
`core/dom`。所以整個 net 層除了 `live`/`mock` 之外都在 leaf 裡。

### 2.2 四個到處被用的全域，在 ES module 下怎麼擺

| 全域 | 用了幾次 | 擺哪裡 | 為什麼不會變成循環 |
| --- | ---: | --- | --- |
| `S`（狀態） | 257 | `core/state.js`，`export const S = { … }` | 它是純資料，不 import 任何人。全檔都是 `S.foo = …` 這種**改屬性**，物件 identity 從頭到尾不變，所以 `const` 沒問題，也不需要 setter |
| `els`（DOM 快取） | 404 | `core/dom.js`，`export const els = {}` + 那份 forEach | 同樣不 import 任何人。`document.getElementById` 在 module body 就跑——**這是安全的**，因為 `type="module"` 天生 deferred，body 一定解析完了 |
| `T` / `applyStrings` | 316 | `core/i18n.js` | 只 import `core/esc.js`（`words()` 需要 `esc`）。見 2.3 |
| `api` | 34 | `core/…` 不行，要一個**空 leaf**：`net/api.js` | 見下 |

`api` 是唯一需要動到寫法的：今天是

```js
var api = MOCK ? Mock : Live;      // 4456
```

照抄成 module 的話，`net/api.js` 得 import `live.js` 與 `mock.js`，而 `live.js → handlers.js →
view/* → input/start.js → api.js`——`api.js` 會落進一個很大的循環裡。ESM 解得開，但那要靠
「沒有人在 module body 讀 `api`」這個當下成立、以後未必成立的性質。

改成把 `api` 的**繫結**與它的**內容**分開，`net/api.js` 就變成一個什麼都不 import 的 leaf：

```js
// app/js/net/api.js
//
// 這頁只有一個 API，它不是 Live 就是 Mock，而決定是哪一個的那件事（`MOCK`）跟需要它的三十幾個
// 呼叫點之間，隔著整個 render 層。所以這裡只放繫結，內容由 main.js 在任何人有機會呼叫它之前裝進來
// ——`export let` 是活的繫結，import 端拿到的一定是裝好之後的那個。
export let api = null;

export function useApi(impl) { api = impl; }
```

```js
// app/js/main.js 的第一件事
import { MOCK } from "./core/env.js";
import { Live } from "./net/live.js";
import { Mock } from "./net/mock.js";
import { api, useApi } from "./net/api.js";

useApi(MOCK ? Mock : Live);
```

其餘 33 個呼叫點一個字都不用改：`import { api } from "…/net/api.js"` 之後，`api.focus(…)`、
`typeof api.places !== "function"` 全部照舊。**注意**：不能改成 `export default` 一個固定物件再
轉發方法——`Mock` 少了 `pushKey` / `places` / `startPlace` 這些，程式裡有六處是用
`typeof api.X !== "function"` 在判斷「這個模式有沒有這件事」，固定物件會讓那些判斷全部變成 true。

### 2.3 建議順手拆掉的兩組循環（都是 leaf 層的，值得拆）

其他 22 組留著沒關係（見 2.4），但這兩組會讓 leaf 層不再是 leaf，進而讓**求值順序**變得要推理：

1. **`core/i18n.js` ↔ `core/util.js`**
   `words()`（i18n）要 `esc`；`clockOf()`（util）要 `T` 和 `fill`。
   → 把 `esc()`（3154–3158，5 行）抽成 `core/esc.js`。之後 `i18n → esc`、`util → esc + i18n + dom`，沒有循環。

2. **`core/pixels.js` ↔ `view/composer.js`**
   pixels 那顆共用時鐘（3355–3366）會讀 `liveSpin`，而 `liveSpin` 宣告在 6235（`view/composer.js`）。
   → 把 `var liveSpin = null;` 搬到 pixels.js，跟 `bandSpin` / `startSpin` / `confirmSpin` 放在一起。
   它本來就是同一類東西（「掛在同一顆時鐘上的 spinner 把手」），這行註解就寫在旁邊。

### 2.4 剩下 22 組循環：為什麼不用管

掃出來的雙向邊（只列中層的）：

```
view/list ↔ view/derive        view/list ↔ view/transcript     view/list ↔ view/composer
view/list ↔ view/waits         view/list ↔ session/agent       view/list ↔ session/open
view/list ↔ input/detail-actions               view/list ↔ input/start
view/transcript ↔ view/waits   view/transcript ↔ session/agent view/transcript ↔ input/detail-actions
view/composer ↔ input/composer view/composer ↔ input/shots     view/waits ↔ input/action-confirm
view/waits ↔ input/start       session/open ↔ session/agent    session/open ↔ input/composer
session/open ↔ input/detail-actions            session/open ↔ input/action-confirm
session/agent ↔ input/composer input/keys ↔ input/settings     input/settings ↔ input/push
input/detail-actions ↔ input/action-confirm
```

這些全部是**函式互相呼叫**，而且只發生在事件觸發或 boot 之後。ES module 在循環裡對兩種 export
的行為不同：

- `export function foo() {}` — **hoisted**，在任何 module body 開始跑之前繫結就已經初始化好，循環裡永遠安全。
- `export const/let X = …` — 在自己的 module body 跑到那一行之前處於 TDZ；別人在 module body 讀它會丟 `ReferenceError`。

所以規則只有兩條：

> **（a）跨模組的函式一律用 `export function name() {}`，不要用 `export const name = () => {}`。**
> 今天所有跨模組的函式本來就是 `function` 宣告，照抄即可。
>
> **（b）沒有模組可以在 module body（不含函式體）讀另一個模組的 `const`/`let` 型 export。**

(b) 目前完全成立。實際掃過 JS 區段全部 column-0 的可執行語句，跨模組的執行期讀取只有：

| 讀什麼 | 在哪讀 | 來自 |
| --- | --- | --- |
| `els[...]` | 約 60 處 `addEventListener` 綁定 | `core/dom.js`（leaf） |
| `ASK_MARK`、`uuid` | `Mock` 的 fixtures（3802–4455，IIFE 在求值時就跑） | `core/util.js`（leaf） |
| `storedBool` | `S` 的字面值裡（3099） | 同一個檔（`core/state.js`） |
| `reduced` / `phone` / `MOCK*` | 那顆 spinner 時鐘、`trackVisibleViewport`、`digits`、`pullToRefresh` | `core/env.js`（leaf） |
| `drawIcon`、`els` | `main.js` 的 9822–9823 | leaf，而且 main 是最後求值的 |

全部指向 leaf 層。做完 2.3 的兩個搬動之後，leaf 層之間也沒有循環，求值順序完全確定。

---

## 3. CSS 怎麼切

### 3.1 實際的分群狀況

`<style>` 裡本來就有 14 條 `/* ===== */` 的分隔線，位置是：99、172、228、335、350、431、600、
648、1189、1570、1654、2021、2166、2184。第 1 節的 12 個檔就是照這些線併出來的——每一刀都落在
「空行 + 註解區塊開頭」上，沒有一行被切開。

三類東西實際上是這樣分佈的：

- **design token**：只有 `tokens.css`（99–171）。裡面是 `:root` 的一組變數（顏色、字體、
  `--ease`、`--vvh`）加上一個 `(pointer: coarse) and (max-width: 899px)` 的覆寫。這是唯一一個
  其他檔案全部依賴、自己不依賴任何人的檔案，所以它一定排第一。
- **版面**：`base.css`（reset、`html/body`、`.scroller`）與 `shell.css`（`.app` 的兩欄 grid、
  `.pane`）。加起來 152 行。
- **元件**：其餘 9 個檔，每個對應畫面上一塊看得見的東西。

有兩個真正的**共用元件**藏在元件檔裡，要知道它們在哪：

- `.chip`（821–860）——detail header 的兩顆按鈕、`.sheet .block .row .chip`、`.confirm-sheet`
  的兩顆、`.info-sheet .buttons` 都在用。它現在排在 `detail.css` 尾巴，而所有用它的 sheet 規則
  都排在後面（1654+），所以 sheet 那邊的覆寫照樣生效。**不要**把它移到 `sheets.css` 後面。
- `.overlay` / `.sheet` 底盤（1657–1707）——`sheets.css` 開頭那一段，settings / keys / start /
  info / confirm 五張表共用。

### 3.2 為什麼不能重排：一個真的會壞的例子

```
345:  .app[data-pane="off"] .pane-detail { display: none; }     ← shell 段
2029: .app[data-pane="off"] .pane-detail { display: flex; }     ← phone 段（@media 內）
```

兩條選擇器**完全一樣**，specificity 都是 (0,2,1)。`@media` 不加 specificity，所以在手機寬度下
決定勝負的只有先後順序。`responsive.css` 排在 `shell.css` 後面 → `flex` 贏 → 手機上詳情面板顯示
得出來。順序一顛倒，手機版的對話畫面就整個不見了。

同一類的還有 `.home-hero::before`（882 vs 2134）、`.tx-scroll.home`（873 vs 2129）。

所以第一階段的規則就一條：**`<link>` 的順序 = 今天由上而下的順序**。這讓第一階段成為一個
「純搬移」的改動——可以用機械的方式驗證（見 6.1）。

### 3.3 要不要把 phone 覆寫分散到各元件檔？

先不要。分散之後，每個元件檔自帶一個 `@media (max-width: 899px)`，同檔內順序天然正確；但跨檔的
同 specificity 覆寫（例如 phone 段的 `.pane-detail` 覆寫的是 `detail.css` 的 `.pane-detail`）就
變成要靠 `<link>` 順序，而那正是我們想避免推理的東西。而且 phone 段裡有一整塊**不是覆寫**的東西
——`.row[data-swipe]` / `.row > .swipe-end`（2100–2127），那是只在手機上存在的滑動列元件，本來
就該獨立。先把它們留在 `responsive.css`，等第一階段落地、有東西可以比對之後再說。

---

## 4. `Sources/RemoteServer.swift` 要改什麼

三個地方：auth 的 open set、一個新的 route case、一個新的私有方法。`page()` 完全不用動——
`index.html` 還在原地。

### 4.1 URL 佈局的決定

用 `/app/` 一個前綴，磁碟上就放在 `Resources/web/app/`。理由：

- auth 只要多一行、route 只要多一個 case。
- 之後要加字型、圖、任何頁面自己的檔案，都在同一個前綴底下，不用再回來改 auth。
- **磁碟路徑 = URL 路徑**，所以 `tools/web-serve.py --root Resources/web` 不必改寫任何路徑就能
  當 dev server 用（第 5 節）。

CSS 裡的 `url()` 都是絕對路徑（884、2150 都是 `url("/hero-…webp")`），所以把 CSS 搬進
`/app/css/` **不會**弄壞背景圖。

### 4.2 auth 的 open set（約 :262）

`/app/*` 必須是開放的，理由跟 `/`、`/index.html`、`/v1/strings` 一模一樣：**畫不出來的門不是門**。
這些檔案對所有人都相同，不含任何 session、repository、路徑或憑證，而且已經在公開的 repo 裡。

```swift
        let open: Set<String> = ["/", "/index.html", "/manifest.webmanifest", "/hero-orchestration.webp",
                                 "/v1/health", "/v1/strings"]
        let pairing = request.path.hasPrefix("/v1/auth/")
+       // 這頁自己的樣式與模組，理由跟這頁本身一樣：一扇畫不出來的門不是門。它們對每個人都是同一份
+       // 檔案，不指名任何 session、repository 或路徑，而且早就在一個公開的 repo 裡——唯一改變的是
+       // 它們現在放在 `index.html` 旁邊而不是裡面。
+       let shell = request.path.hasPrefix("/app/")
         let icon = request.path == "/sw.js"
             || (request.path.hasPrefix("/splash-") && request.path.hasSuffix(".png"))
             …
-       if !open.contains(request.path), !pairing, !icon, !orchestratorAuthed, !taskSecretRoute {
+       if !open.contains(request.path), !pairing, !shell, !icon, !orchestratorAuthed, !taskSecretRoute {
```

### 4.3 route case

放在 `("GET", "/hero-orchestration.webp")` 那個 case 之後、`("GET", "/sw.js")` 之前（約 :813），
維持「靜態資產排在一起」的排版：

```swift
        case ("GET", let path) where path.hasPrefix("/app/"):
            return asset(String(path.dropFirst("/app/".count)))
```

### 4.4 `asset(_:)`

擺在 `page()` 旁邊（約 :1870）。

**路徑穿越怎麼擋**：`Request.init`（:2136）**從來不做 percent-decode**——`path` 就是 client 放在
線上的原字串。所以正確的作法是白名單而不是黑名單：不必去認 `%2e%2e%2f`、`%252e`、`..%2f` 這些
寫法，因為 `%` 根本不在允許的字母表裡。

```swift
    /// `Resources/web/app` 底下的一個檔，並且只有那底下的。
    ///
    /// **`request.path` 從來不會被 percent-decode** —— 見 `Request.init` —— 所以到得了這裡的
    /// 就是 client 放上線的那串原字。這讓安全的規則是一份白名單而不是一份黑名單：沒有
    /// `%2e%2e%2f` 要認，因為 `%` 根本不在下面這個字母表裡。一個 segment 只能是字母、數字、
    /// `.`、`-`、`_`；segment 之間用 `/`；不准有空的 segment，不准有 `.` 或 `..`；副檔名必須是
    /// 這個 app 知道怎麼標記的那幾種。其他一律在組出路徑之前就 404。
    private func asset(_ name: String) -> Response {
        let types = ["css": "text/css; charset=utf-8",
                     "js": "text/javascript; charset=utf-8"]
        // 用一份明確的字元集合，而不是 `isLetter` / `isNumber`：那兩個問的是 Unicode 的分類，
        // 而這裡要問的是「這是不是一個我們自己放進 bundle 的檔名」。順帶讓它在 CI 那套比較舊的
        // Swift 上編得過。
        let allowed = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_")

        let parts = name.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        let plain = { (part: String) -> Bool in
            if part.isEmpty || part == "." || part == ".." { return false }
            return part.allSatisfy { allowed.contains($0) }
        }
        guard parts.count >= 1, parts.count <= 4, parts.allSatisfy(plain),
              let file = parts.last,
              let dot = file.lastIndex(of: "."),
              let type = types[String(file[file.index(after: dot)...])] else {
            return .error(404, "not_found", "No such file")
        }

        guard let root = Bundle.main.resourceURL?
                .appendingPathComponent("web", isDirectory: true)
                .appendingPathComponent("app", isDirectory: true) else {
            return .error(404, "not_found", "The web interface is not in this build")
        }
        var url = root
        for part in parts { url.appendPathComponent(part) }
        // 束帶之外再加一條吊帶。上面那份白名單已經讓這件事不可能發生；它在這裡，是為了讓哪天有人
        // 把字母表放寬的時候，真正被讀走的檔案仍然在它被解析出來的那個目錄底下。
        guard url.standardizedFileURL.path.hasPrefix(root.standardizedFileURL.path + "/"),
              let data = try? Data(contentsOf: url) else {
            return .error(404, "not_found", "No such file")
        }
        return Response(status: 200, headers: ["Content-Type": type], body: data)
    }
```

**Content-Type 怎麼決定**：查表，不猜。`text/javascript; charset=utf-8` 是 module script 唯一
穩妥的標法（`application/javascript` 也可以，但 `text/javascript` 是現行標準指定的）。這件事不能
出錯——瀏覽器對 module 的 MIME 檢查是嚴格的，標錯就是整頁不執行、而且錯誤訊息不會說是 MIME 的事。
另外 `Response.wire`（:2199）已經無條件送 `X-Content-Type-Options: nosniff`，所以沒有回頭路。

**Cache-Control**：不用寫。`route()`（:242）對任何沒有自己設 `Cache-Control` 的回應一律補
`no-store`，這正是我們要的——新檔案不會有任何一台裝置卡在舊版本上。代價見 7.4。

**`build.sh` 不用改**：第 37 行是 `cp -R Resources/web`，整棵樹一起進 bundle。

### 4.5 順帶發現（跟拆檔無關，但同一個檔）

CSS 指向 `url("/hero-orchestration-v4-task-clinic.webp")`（884、2150），但
`RemoteServer.swift` 只有 `/hero-orchestration.webp` 一條路由（:803），open set（:262）裡也只有
那一個。這在 `1253aa7 feat: give the homepage hero a task clinic` 之後已經是 committed 的狀態，
所以現在從 app 開這一頁，首頁那張 hero 是 404 —— 只在 `file://` / dev server 那邊看得到，因為
那邊是直接從磁碟送的。誰收尾 hero，路由與 open set 要跟著加。這是既有的落差，不是拆檔造成的，
但拆檔的 `/app/` 路由（4.4）順手就能把它一起收掉。

---

## 5. `file://` mock 模式的損失與替代

### 5.1 損失是真的

ES module 在 `file://` 下會被擋——module script 是用 CORS 抓的，`file://` 的 origin 是 opaque。
實測（headless Chrome、`--dump-dom`，兩個檔的最小重現）：不加旗標時 module 沒有執行，`<title>`
停在初始值。

這件事的代價不只是「開發時不方便」：

- `tools/shoot-assets.sh:112` 的 `WEB_PAGE="file://$PWD/Resources/web/index.html?write=1"`——
  README 的 `web` 與 `web-wide` 兩張圖就是從這裡拍的。
- `shoot-assets.sh:155–161` 的 `needs_app()` 特地把 `web` / `web-wide` 排除在「需要 app 在跑」
  之外，整段設計是「不需要 app、不需要配對、不需要 session，一份 checkout 加一個 Chrome 就能重拍」。
- 頁面本身的 boot（9860）用 `location.protocol === "file:"` 判斷要不要去要 `/v1/strings`。

### 5.2 `tools/web-serve.py` 能不能直接拿來用

**能，而且幾乎不用改。** 它是 `SimpleHTTPRequestHandler` + 一個把 `/v1/`、`/icon-`、`/splash-`、
`/favicon.ico`、`/manifest.webmanifest` 轉給 app 的 proxy。指令：

```bash
./tools/web-serve.py --root Resources/web --port 7788
# 然後開 http://127.0.0.1:7788/?mock=1&write=1
```

會發生的事：

| 請求 | 結果 |
| --- | --- |
| `/` | 從磁碟送 `Resources/web/index.html` |
| `/app/css/*.css`、`/app/js/**/*.js` | 從磁碟送。磁碟佈局 = URL 佈局，所以不必改寫路徑 |
| `/manifest.webmanifest` | proxy 給 app；app 沒開就 502（頁面只會少一個 manifest，不影響任何功能） |
| `/v1/strings` | proxy。app 沒開 → 502 → `res.ok` 為 false → `boot(null)` → 英文，正是 mock 想要的 |
| `/hero-…webp` | **不在 PASS 清單裡**，會從磁碟送（檔案就在 `Resources/web/`）。剛好對 |
| Mock 的 API | 完全不出門，`?mock=1` 讓 `api = Mock` |

`end_headers()` 已經無條件送 `Cache-Control: no-store` 與 `Service-Worker-Allowed: /`，開發時剛好。

### 5.3 要改的兩處（都很小，都建議做）

**(a) 明確指定 `.js` / `.css` 的 MIME。** 這台機器上 Python 3.14 的 `mimetypes` 給
`.js → text/javascript`、`.css → text/css`，是對的；但那份對照表會讀 `/etc/apache2/mime.types`
之類的系統檔，換一台機器不保證。而 `SimpleHTTPRequestHandler` 認不出來時會退回
`application/octet-stream`，那會讓整頁的 module 全部拒絕執行。兩行的保險：

```python
class Handler(http.server.SimpleHTTPRequestHandler):
    extensions_map = {**http.server.SimpleHTTPRequestHandler.extensions_map,
                      ".js": "text/javascript", ".mjs": "text/javascript", ".css": "text/css"}
    upstream = "127.0.0.1:7717"
    …
```

**(b) 換成 `ThreadingHTTPServer`。** 現在是 `socketserver.TCPServer`，單執行緒。今天只送幾個檔
沒差，拆完之後一次要送 49 個，會排成一條隊。

```python
-    socketserver.TCPServer.allow_reuse_address = True
-    with socketserver.TCPServer(("127.0.0.1", args.port), Rooted) as httpd:
+    http.server.ThreadingHTTPServer.allow_reuse_address = True
+    with http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Rooted) as httpd:
```

（`ThreadingHTTPServer` 的 `protocol_version` 也是 `HTTP/1.1`，順便有 keep-alive。）

### 5.4 `shoot-assets.sh` 兩張圖怎麼辦

兩條路，建議走第一條：

**(1) 起 dev server（推薦）。** 把 `WEB_PAGE` 換成 `http://127.0.0.1:7789/?write=1&mock=1`，
在 `run_if web` 之前起一個 `web-serve.py --root Resources/web --port 7789`，拍完 kill——
跟 `web-push` 那段（279–292）現成的寫法一模一樣，抄過來就好。用另一個 port 是為了不跟 `web-push`
的 7788 打架。`needs_app()` 不用改：這台 server 沒接到 app 一樣能跑，mock 不出門。

**(2) 給 Chrome 加旗標。** `tools/shoot-web.js:154–164` 的 args 陣列加一個
`"--allow-file-access-from-files"`。**實測有效**——同一組最小重現檔，不加旗標
`<title>` 停在 `pending`（module 被擋），加了之後變成 `MODULE-LOADED-OK`。一行就好。

代價：這兩張圖從此綁在一個 Chrome 旗標上（那是個 debug 旗標，Chrome 沒有承諾要一直留著），
而且對「直接把檔案拖進瀏覽器看一眼」的人一點幫助也沒有——它救的是拍照腳本，不是開發迴圈。
`shoot-assets.sh` 那段「一份 checkout 加一個 Chrome 就能重拍」的承諾勉強還在，只是多了一個
但書。所以還是建議 (1)；(2) 是「這禮拜不想動 shoot-assets.sh」時的止血。

不論走哪條，`shoot-assets.sh` 那段「Shot from a **`file://` copy of the page**」的註解（252–261）
要一起改：它現在說的那件事會不再成立。

### 5.5 三十秒的重現法

```bash
mkdir -p /tmp/modtest && cd /tmp/modtest
echo 'export const hello = "OK";' > dep.js
echo 'import { hello } from "./dep.js"; document.title = hello;' > main.js
printf '<title>pending</title><script type="module" src="./main.js"></script>' > index.html
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
  --user-data-dir=/tmp/modtest/p --virtual-time-budget=3000 --dump-dom \
  "file:///tmp/modtest/index.html" | grep -o '<title>[^<]*</title>'
# <title>pending</title>  = 被擋
# <title>OK</title>       = 載到了
```

---

## 6. 分幾步做

每一階段都是一個可以獨立 commit、獨立回退的改動。

### 階段 1 — CSS（風險最低，先做）

跟 JS 完全解耦，而且可以機械地驗證。

1. 建 `Resources/web/app/css/`，把 99–2199 依 1.1 的 12 個區間**逐行搬過去**，一個字不改。
2. `index.html` 的 `<style>…</style>` 換成 12 條 `<link>`，順序照表。
3. `RemoteServer.swift`：4.2 的 auth 一行 + 4.3 的 route case + 4.4 的 `asset(_:)`。
   **必須跟第 1、2 步同一個 commit**——新的 HTML 配舊的 server 就是一頁沒有樣式的白字。

**驗證：**

```bash
# (a) 逐位元組等價：把 12 個檔串起來，要跟原本的 99–2199 一模一樣
cat app/css/{tokens,base,header,shell,list,detail,transcript,composer,status-line,sheets,door,responsive}.css \
  | diff - <(git show HEAD:Resources/web/index.html | sed -n '99,2199p') && echo IDENTICAL

# (b) 路徑穿越擋住了沒（app 要在跑）
for p in "/app/../index.html" "/app/%2e%2e/index.html" "/app/css/../../index.html" "/app/js/main.js"; do
  echo -n "$p → "; curl -s -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:7717$p"
done   # 前三個要 404，最後一個階段 1 時還不存在也是 404

# (c) Content-Type
curl -sI http://127.0.0.1:7717/app/css/tokens.css | grep -i content-type

# (d) 眼睛：./tools/web-serve.py --root Resources/web --port 7789，開 ?mock=1 逐頁看過
# (e) ./test.sh（Swift 側；CSS 沒有測試涵蓋，(a) 就是它的測試）
```

第 (a) 步是這個階段的重點：`IDENTICAL` 成立，就等於證明了定理一，第 3.2 節那個 `.pane-detail`
的陷阱不可能踩到。

### 階段 2 — 伺服器與工具（可以跟階段 1 合併，但分開比較好回退）

1. `tools/web-serve.py` 的兩處（5.3）。
2. `tools/shoot-assets.sh` 的 `WEB_PAGE` 與那段註解（5.4）。
3. `tools/shoot-web.js` 視 5.4 的選擇而定。

**驗證：** `./tools/shoot-assets.sh web` 能跑出 `web.gif`，而且圖裡的畫面跟上一版一致。

### 階段 3 — JS 的 leaf 層（8 個檔，2.1 的最底層）

`core/env`、`core/esc`、`core/i18n`、`core/state`、`core/dom`、`core/util`、`core/pixels`、
`net/api`，加上 2.3 的兩個小搬動（`esc` 抽出、`liveSpin` 搬進 pixels）。

其餘的 JS 先原封不動留在 `index.html` 的 `<script>` 裡？**不行**——classic script 不能 `import`。
所以階段 3 只能是「把整份 JS 一次搬成 module」的一部分。務實的作法是把階段 3、4 合成一次改動，
但**在同一支 branch 上分成兩個 commit**：先建 leaf 檔並讓 `main.js` import 它們，再切中層。

### 階段 4 — JS 的其餘 28 個檔

依 1.2 的表逐段搬。每個檔的頭尾都已經確認落在空行或註解區塊的邊界上。

搬的時候三件事：

1. 每個檔頂端補 `import`，跨模組的函式一律 `export function`（2.4 的規則 a）。
2. `index.html` 的 `<script>` 換成 `<script type="module" src="/app/js/main.js"></script>`。
   **head 裡 80–96 的那段 inline script 維持原樣、維持 classic**（見 7.1）。
3. `main.js` 的最前面呼叫 `useApi(MOCK ? Mock : Live)`。

**驗證：**

```bash
# (a) 語法：整份當成 module 解析得過（這件事今天就已經成立，實測過）
for f in Resources/web/app/js/**/*.js; do node --check "$f" || echo "FAIL $f"; done

# (b) 沒有漏掉的名字：用 dev server 開 ?mock=1，console 一條 ReferenceError 都不能有
./tools/web-serve.py --root Resources/web --port 7789

# (c) 走完 mock 的劇本：?mock=1&write=1 手動點過一輪，重點是
#     開 session / 開 agent / 摺疊 tool run / 送一則訊息 / 回答一個問題 /
#     Settings / Start / Info / Git changes / 結束 session 的確認 / ?door=1 / ?flaky=1
# (d) 自動化：node tools/shoot-web.js --url "http://127.0.0.1:7789/?write=1&mock=1" \
#            --script web --dir /tmp/webshoot --fps 16
#     這支劇本本來就會等頁面而不是等時鐘，跑得完就代表 render 迴圈是活的
# (e) 真的連上 app：./build.sh（會關掉再開啟 app，先講一聲），手機或瀏覽器實際開一次
# (f) ./test.sh
```

### 階段 5 — 收尾（獨立的一個 commit，因為它**會改變順序**）

把 3.1 / 1.1 記下來那幾處放錯地方的 CSS 挪回去：`.stale .shut`（1406–1411 → header）、
`.row .agents-chip`（1549–1560 → list）、`.detail-head .who/.name/.sub/.tools`（821–827 → detail
的前半）。同一個 commit 裡把 `composer.css` 拆成 composer / live / agents 三個檔。

這一步**不能**用階段 1 的 `diff` 驗證（順序變了），只能靠眼睛與截圖比對。所以它獨立成一個
commit，出事的時候回退代價最小。

---

## 7. 風險清單

### 7.1 head 裡那段 inline script 不能變成 module（**最容易踩，後果最明顯**）

80–96 那段做兩件事：拿掉 `file://` 下沒東西可指的 manifest link，以及
`document.documentElement.className = "booting"`。而 `base.css` 裡有

```css
html.booting body { visibility: hidden; }
```

它是「words 到齊之前不畫任何東西」這個設計的另一半。classic inline script 在解析當下就跑；
`type="module"` 一律 deferred。改成 module，`booting` 會在 body 解析完之後才掛上——中間那段時間
使用者會看到一整頁英文 furniture，正是那段註解在防的事。

**做法：這段維持原樣、維持 inline、維持 classic。** 12 條 `<link>` 排在它後面。

順帶一提，`html.booting body { visibility: hidden }` 一定要留在 `<head>` 的 stylesheet 裡——
`<head>` 的 stylesheet 是 render-blocking，所以即使拆成獨立檔，也不會有先畫再藏的閃爍。

### 7.2 script 執行順序：module 是 deferred

今天的 `<script>` 在 body 最後（2611），本來就是 DOM 解析完才跑，所以 `els` 的
`getElementById` 掃描不受影響。真正變的是三件事：

- **`window.onerror` 之前的錯誤**：module 的錯誤時機不同，但這頁沒有裝 `onerror`，無差別。
- **同一個 target、同一個 phase 的 listener 順序**：由「哪個模組先求值」決定，不再是原始碼順序。
  掃過所有 document/window 層級的 listener：`keydown` 有兩個（7077 bubbling、7774 **capture**）。
  capture 的一定先跑，跟註冊順序無關，所以這一對是安全的。`click`（7276，在 SwipeRows 裡）、
  `pointerdown`（7768）、`touchstart`（7272）、`paste`（9361）、`dragover`/`drop`（9382–9383）
  各只有一個。**目前沒有真的靠註冊順序的地方**——但這是拆完之後最難自己發現的一類 regression，
  以後新增 document 層級的 listener 要記得。
- **boot 的等待變長**：boot 現在要等整張 module graph 抓完。頁面在那之前是 `visibility: hidden`
  的 `#0e0e11`，讀起來像 app 的底色而不是白畫面，而且 home screen 有那 20 張啟動圖蓋著。
  9873 那個 `setTimeout(boot, 2000)` 的保險是在 `main.js` 求值**之後**才開始算的，救不了這一段。

### 7.3 module graph 的深度會乘上 RTT

瀏覽器要先拿到 `main.js`、解析完才知道要抓 `./core/dom.js`，再解析完才知道下一層。所以成本是
**深度 × RTT**，不只是檔案數。這份規劃的圖最深是 3 層（`main → input/* → core/*`）。

`<head>` 加一排 `<link rel="modulepreload">` 可以把所有模組壓平成第一層同時發出。Safari 17+
與 Chrome 都支援，不支援的瀏覽器會忽略它，不會壞。缺點是那排 link 要跟著檔案清單維護。
建議：階段 4 落地之後先量，超過感覺得出來才加。

### 7.4 請求數 × `Connection: close` × 單一 queue

`RemoteServer` 是手寫的 HTTP/1.1，`Response.wire`（:2196）對每個回應都送 `Connection: close`，
而且所有連線都在同一條 serial `DispatchQueue`（:46）上處理。拆完之後一次頁面載入從 1 個請求
變成 49 個（1 + 12 + 36），每個都是一條新的 TCP 連線。

在 Mac 本機（localhost、`Data(contentsOf:)` 讀幾 KB）這是可以忽略的。透過 cloudflared 從手機
連進來就不一定——每條連線都要付 RTT。

而且因為 `route()` 補的是 `no-store`，**每次載入都會重付一次**。這是正確的選擇（第一次做對比
事後追一台卡在舊版本的手機容易太多，`sw.js` 那段長註解講的就是那次），但代價要講清楚。

如果之後量出來真的痛，最小的解法是給 `/app/*` 一個 ETag：這些檔案只在 app 重建時才變，所以
ETag 可以直接用 build stamp（`Build.stamp` 已經有 `build|version|protocol` 這個字串），配
`Cache-Control: no-cache` + `If-None-Match` → 304。body 變成零，來回還是要一趟，但省掉 payload。
**不建議**用內容 hash 的檔名——那需要一個產生檔案的步驟，正是這份規劃排除掉的東西。

### 7.5 `sw.js` 與快取：比想像中安全

三件事查證過：

1. **service worker 只有在使用者開了通知才會註冊**（9186，在 `Push` 裡）。沒開通知的人根本沒有 worker。
2. worker 的 `fetch` handler 只攔 `event.request.mode === "navigate"`（RemoteServer.swift:1907）。
   **CSS 與 module 的請求完全不經過它**，直接走網路 + `no-store`。
3. worker 對 navigation 是 `fetch(url, { cache: "reload" })`，永遠拿新的 HTML。

所以拆檔不會製造新的 service worker 快取問題。

### 7.6 手機上裝成 home screen app 的舊快取

真正的殘留風險是 `sw.js` 註解裡描述的那一個：某台裝置在 `route()` 補上 `no-store` **之前**就把
`index.html` 存進了 Safari 的 heuristic cache。

好消息是這件事在拆檔之後**不會變糟，也不會出現混合狀態**：

- 舊的 `index.html` 是自給自足的（CSS 和 JS 都在裡面），它根本不知道 `/app/` 存在，繼續正常運作，
  只是收不到更新——跟今天一模一樣。
- 新的 `index.html` 一定跟 `/app/` 一起送達（同一個 bundle、同一個 `cp -R`）。

唯一能做出「新 HTML + 沒有 `/app/` 路由」這種組合的方法，是把 HTML 的改動與 `RemoteServer.swift`
的改動分成兩個 commit、然後在中間 build 一次。**所以階段 1 的三步必須在同一個 commit 裡。**

那台真的卡住的裝置，脫困的路徑沒有變：開通知（裝上 worker）→ worker `claim` → 再 reload 一次。

### 7.7 strict mode

module 一律 strict。已經驗過：

- 整份 JS 加上 `"use strict";` 之後 `node --check` 通過。
- 當成 module 解析（`node --check x.mjs`）也通過。
- 掃過所有頂層賦值，**沒有隱式全域**（那六個看起來像的——`distance`、`matched`、`offset`、
  `pulling`、`startOffset`、`wrong`——都是 `var a = 0, b = 0` 這種多宣告子形式，是我的正規表示式
  只抓到第一個名字）。
- 頂層 `var`/`function` 沒有重複的名字。（這點很重要：同名的兩個函式分到兩個模組，會從「後面蓋
  掉前面」悄悄變成「兩個各自獨立的東西」。）

### 7.8 一堆小事

- **`hidden` 屬性 vs `display` 規則。** CSS 裡有五、六處特地寫了 `.foo[hidden] { display: none; }`，
  因為前面設了 `display: flex` 會蓋掉 `hidden`。這些成對的規則在同一個檔裡，不會被拆散。
- **`?mock=1` 之外，`file://` 這個判斷還在兩個地方。** `MOCK` 的定義（2627）與 boot 的分支
  （9860）。dev server 之後兩處都走 `?mock=1` 這條路徑，行為一致，但 boot 會多發一次
  `/v1/strings`——app 沒開就是一次 502。要不要順手在 boot 加上 `if (MOCK) return boot(null)`
  是個可以分開討論的小決定，不影響拆檔。
- **`Origin` 檢查沒有影響。** `dispatch` 只對非 GET 檢查 Origin（:290），`/app/*` 全是 GET。
- **打包沒有影響。** `build.sh:37` 是 `cp -R Resources/web`，新目錄自動跟著進 bundle。
  `.gitignore` 不用改。
- **測試沒有影響。** `Tests/main.swift` 沒有任何一處讀 `index.html`；`grep` 全 repo 只有
  `RemoteServer.swift`（:262、:779、:1863）與 `Strings.swift`（:297 的一句註解）提到它。

---

## 附註：實測記錄

- ES module 在 `file://` 被擋：headless Chrome + `--dump-dom`，`<title>` 停在 `pending`。**已驗證。**
- `--allow-file-access-from-files` 救得回來：同一組檔案、同一個指令，加了旗標之後
  `<title>MODULE-LOADED-OK</title>`。**已驗證。**（headless Chrome 這兩趟各花了一分多鐘才回，
  不是卡住，是慢。）
- 整份 JS 以 module + strict 解析：`node --check` 通過。**已驗證。**
- Python 3.14 的 `mimetypes`：`.js → text/javascript`、`.css → text/css`。**已驗證**（但別的機器不保證，見 5.3）。
