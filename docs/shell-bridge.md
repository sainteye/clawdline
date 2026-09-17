# 殼與網頁之間的介面

> 這份文件給「之後要寫 Linux／Windows 殼的人」。每一節寫一個畫面：**殼負責什麼、網頁負責什麼、
> 兩邊怎麼講話**。寫 Linux 殼的時候，照著同一組訊息名字實作一次，同一份 web 就能用。
>
> 每個 child 只加自己那一節，不要改別人的。共同的規則放在下面「通則」。

## 通則

架構決定（使用者 2026-09-18）：**畫面一律做在 web（React console）裡，原生殼只保留「非原生做不到」
的部分。** 理由是 Linux 與 Windows 之後只要換一個 webview 殼就能用同一份畫面。

- **殼負責**：視窗（大小、層級、無邊框、跟著終端機出現與收起）、全域熱鍵的註冊與錄製、把終端機
  分頁選起來、剪貼簿、通知、瀏海。
- **web 負責**：版面、文字、狀態、清單、設定的每一列——以及**所有的資料與動作**，一律走本機
  daemon 既有的路由，不在殼裡另外實作。
- **講話的方式**：web → 殼是一個 `WKScriptMessageHandler`（`window.webkit.messageHandlers.<name>
  .postMessage({kind: …})`）；殼 → web 是 `window` 上的 `CustomEvent`，因為 message handler 沒有
  回傳值。WebKitGTK 的 `window.webkit.messageHandlers` 同名；WebView2 是
  `chrome.webview.postMessage`，包一層就好。
- **殼注入的東西只有「殼才知道的事實」**，不包括字詞。設定頁是例外，原因寫在那一節。

---

## 輸入條（bar）

- 殼：`shell/darwin/Bar.swift`（新檔；`main.swift` 只多了掛載的幾行）
- web：`web/console/src/bar/`，文件是 `web/console/bar.html`，daemon 以一般檔案供應（`/bar.html`）
- 契約的 TypeScript 那一半：`web/console/src/bar/shell.ts`

### 殼負責

| 項目 | 為什麼非殼不可 |
|---|---|
| 無邊框、置頂、跨 Space、不搶 app 焦點的視窗（`NSPanel` `.borderless` `.nonactivatingPanel`） | 網頁不能自己開視窗，也不能決定自己在哪一層 |
| 視窗後面的毛玻璃（`NSVisualEffectView` `.hudWindow` `.behindWindow`） | 模糊的是**桌面**，網頁看不到桌面 |
| 全域熱鍵（`HotKey.swift`）：在別的 app 裡按下去也算 | 網頁只收得到自己視窗裡的鍵 |
| 顯示、隱藏、失焦自動收起、跟著終端機回來 | 這些都是視窗狀態 |
| 編輯鍵（⌘X／⌘C／⌘V／⌘A／⌘Z／⇧⌘Z） | 這個視窗沒有選單列，WKWebView 要有人把 action 送給它 |
| 把本機 token 放進 cookie，然後載入頁面 | `bar.html` 不在 gate 的開放清單上，只有殼載得到 |

### web 負責

卡片本身、輸入框、session 清單的每一列（⌘1–⌘9、專案圖示、標題、助理圖示與名稱、選中的色條與底色）、
最底下的快捷鍵提示與左下角的專案圖示與名稱、鍵盤的每一個行為、送出、歷史紀錄——以及**所有資料**：
`GET /v1/sessions`（走 `/v1/events` 的 stream）、`POST /v1/sessions/<id>/send`、
`POST /v1/sessions/<id>/focus`。殼一個 byte 的 session 資料都不碰。

### web → 殼：`shellBar`

```js
window.webkit.messageHandlers.shellBar.postMessage({ kind: … })
```

| `kind` | 欄位 | 意思 | 殼要做的事 |
|---|---|---|---|
| `height` | `height`（整數 px） | 卡片現在這麼高 | 把視窗改成這麼高，**上緣不動**（`yFraction` 指的是卡片的上緣，開清單不能把正在打字的那一行推走）；上限是螢幕可視高度 |
| `hide` | — | 收起來（Esc、送出完成） | `orderOut`，並把焦點還給召喚前的那個 app |
| `ready` | — | 頁面第一次畫完了 | 之後可以放心顯示視窗 |

寬度**不**由 web 決定：寬度等於視窗在螢幕上的位置，那是殼的事（macOS 用 `Config.width` 的 720）。

### 殼 → web：`window` 上的 CustomEvent

| 事件 | detail | 什麼時候送 |
|---|---|---|
| `clawdline-bar-shown` | `{}` | 每一次召喚（包含頁面早就載好、只是視窗被 order out 的那種）。web 收到就把清單與快捷鍵回到「剛召喚」的樣子、放掉歷史游標、把游標放回輸入框。**輸入框裡的字不清掉**——召喚不是丟掉半句話的方法 |
| `clawdline-bar-hidden` | `{}` | 收起來的時候。web 收到就停掉 `/v1/events` 的 stream（見下） |
| `clawdline-bar-state` | `{follow: boolean, hotkey: string}` | 載完、召喚、設定變更時 | 

`follow`：殼說「選擇移動時要不要順便把終端機的分頁選起來」。**目前一律 false**，原因見
`docs/cross-platform.md`。`hotkey`：目前註冊到的組合，只拿來當提示文字。

### 一條容易漏掉的規則：stream 跟著視窗

輸入條的頁面是**一直載著**的（這樣按下熱鍵是「視窗出現」而不是「頁面開始跑」），但
`/v1/events` 的 stream 只在視窗在螢幕上的時候開著。理由寫在 `main.swift` 的 `fleetBridgeScript`
上面：每一條 stream 都會讓 daemon 自己讀一次這台機器（process table、tmux、screens），一條永遠
開著、一天只看幾秒的 stream，就是把那個工作量白白加倍。所以 `clawdline-bar-shown` 開、
`clawdline-bar-hidden` 關（`web/console/src/bar/fleet.ts`）。**寫別的殼的時候這兩個事件一定要送**，
不然不是畫面不會更新，就是 daemon 一直在做沒人看的工。

### 沒有殼的時候

`web/console/src/bar/shell.ts` 的每一個呼叫在瀏覽器裡都是 no-op 而且回 false，頁面照樣畫、照樣
送得出去。這是開發這一頁的方法，也是為什麼頁面自己的行為沒有任何一項躲在 `inShell()` 後面。
