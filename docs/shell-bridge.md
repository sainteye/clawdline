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

---

# 設定視窗那一節（原本是另一份同名文件，合併時併進來）

## 誰負責什麼

| | 負責的東西 |
|---|---|
| **web（React console）** | 版面、文字、狀態、清單、設定的每一列。設定的讀寫一律走 daemon 的 `GET`／`POST /v1/settings`。 |
| **原生殼** | 視窗（大小、層級、無邊框、跟著終端機出現與收起）、全域熱鍵的註冊與錄製、把終端機分頁選起來、剪貼簿、通知、瀏海、檔案選取器、在檔案總管裡顯示某個檔。 |

**殼不送句子。** 一個讀數過來的時候是事實（`{"kind":"noModel"}`），不是中文；
字詞在 web 那邊（`web/console/src/pages/settings/window/copy.ts`，逐字抄自舊 app 的
`Sources/Copy+Chinese.swift`）。這樣下一個平台的殼不用再抄一次字串。

唯一的例外是**原生視窗標題列**：它在頁面載入之前就要畫出來，所以
`shell/darwin/Copy.swift` 留了一個 `settingsTitle`。

## 目前有哪些視窗

| 視窗 | 網址 | 殼的程式 | web 的程式 |
|---|---|---|---|
| 主控台（session 清單等七頁） | `/` | `shell/darwin/main.swift` | `web/console/src/App.tsx` |
| **Clawdline 設定** | `/settings.html` | `shell/darwin/SettingsWindow.swift` | `web/console/src/pages/settings/window/` |

設定視窗是**獨立的 HTML 進入點**，不是主控台的一個分頁。理由：它是一個設定視窗，
session 清單、抽屜、event stream 和二十九份樣式表沒有理由被載進去。
`web/console/vite.config.ts` 的 `build.rollupOptions.input` 列了這兩個文件。

兩個文件都在認證閘門後面。殼在載入前把 daemon 的 local token 當成 `clawdline-next`
cookie 塞進 WKWebView 的 cookie store（`shell/darwin/Pairing.swift` 的 `LocalToken.install`），
所以視窗跟其他 client 走同一道門。**`POST /v1/settings` 只接受這台機器自己的 token**
（`internal/transport/http/gate.go` 的 `writePolicy`）——配對過的瀏覽器裝置就算能 send 也是 403，
因為熱鍵是一個全域鍵盤攔截。

## 設定視窗的橋：訊息格式

兩邊的型別在 `web/console/src/pages/settings/window/bridge.ts`，那是這份文件的權威版本。

### 傳輸方式

頁面找傳輸的順序是：

1. `window.__clawdlineShell.post`（function）——**非 macOS 的殼設這個就好**；
2. `window.webkit.messageHandlers.shellSettingsWindow`（macOS 的 `WKScriptMessageHandler`）。

兩個都沒有 = 沒有殼。**這時視窗照樣要畫得出來**：每一列只要是檔案裡的值都讀得到也寫得進，
只有真的需要機器的那幾項會明講「這一項要在 Clawdline app 裡才能改」，不會假裝。
在還沒寫殼的那一天，這就是 Linux 上看到的樣子。

message handler 沒有回傳值，所以**殼的每一個回答都是一個 window event**。

### 頁面 → 殼

| 訊息 | 意思 |
|---|---|
| `{"kind":"state"}` | 把現在的原生讀數送過來 |
| `{"kind":"record"}` | 開始聽下一個按鍵組合 |
| `{"kind":"stopRecording"}` | 不聽了，把原本的組合裝回去 |
| `{"kind":"changed"}` | 頁面已經寫好檔案了；重讀並重新套用 |
| `{"kind":"chooseApp"}` | 開一個只選 app 的檔案選取器 |
| `{"kind":"reveal","what":"config"\|"hooks"}` | 在檔案總管裡把那個檔選起來 |
| `{"kind":"hooks","install":true\|false}` | 裝／移除 Claude Code hook |
| `{"kind":"close"}` | 關掉這個視窗 |

殼要檢查訊息真的來自它自己載入的那一頁（macOS 版比對 `frameInfo` 的 host 與 port）。
順著連結跑到別處的頁面不是這一頁。

### 殼 → 頁面（CustomEvent）

| event | `detail` |
|---|---|
| `clawdline-settings-state` | `ShellState`，見下表 |
| `clawdline-settings-hotkey` | `{"spec":"cmd+shift+k","display":"⌘⇧K"}` 或 `{"cancelled":true}` |
| `clawdline-settings-app` | `{"id":"com.apple.Terminal"}` 或 `{"cancelled":true}` |

`ShellState`：

| 欄位 | 內容 |
|---|---|
| `hotkey` | 現在生效的組合。**檔案沒寫時是預設值 `option+space`**（2026-09-19 起；舊 app 退役之前這裡是空的，因為那時候 ⌥Space 是它的） |
| `isDefault` | 這個組合是不是預設來的，而不是使用者自己設的 |
| `display` | 同一個組合，用這個平台的寫法（macOS 是 `⌘⇧K`） |
| `registered` | 現在真的註冊著沒有 |
| `failed` | 設了組合但註冊不起來。**句子不要送**，web 用 `hotkeyFailedTitle()` 自己組 |
| `trouble` | 註冊不起來是哪一種：`system`（macOS 自己佔著那組鍵）／`unreadable`（設定讀不到）／`refused`（別的東西拒絕了）。空字串＝沒有麻煩 |
| `legacy` | 舊版 app 正開著而且用同一組鍵。**兩個 process 註冊同一組 Carbon 快速鍵都會被告知成功**（macOS 15.6 實測），按一下會開兩個輸入框，所以這一格存在是為了說出來，不是為了讓出 |
| `scopeApp` | 生效範圍的 `scope_app`，含殼自己的預設；空字串 = 所有 app |
| `apps` | `scope_app` 裡每一個 id 的 `{id,name,icon?,unresolved?}`，順序同 `scope_app` |
| `runningApps` | 現在開著、而且還不在範圍裡的 app，同樣的形狀 |
| `mascots` | 這台機器有的吉祥物包名稱 |
| `fonts` | 這台機器的等寬字型家族 |
| `dictation` | `{"kind":"ready","model":"…"}`／`{"kind":"noBinary"}`／`{"kind":"noModel"}` |
| `configPath` | 設定檔在哪，家目錄寫成 `~` |
| `hooks` | `{"supported","installed","heard","path"}` |
| `platform` | `darwin`／`linux`／`windows`。只給報告用，不要拿來分支行為 |

`icon` 是 16pt 的圖示壓成 PNG 的 data URL（macOS 版畫成 32px 以配 Retina）。
一份清單幾 KB 的 JSON，比每個圖示開一條路由便宜。

`unresolved` 是「這台機器上沒有裝這個 bundle id 的 app」。**那一列還是要畫，而且顯示 id 本身**：
把別人手寫在自己 config 裡的一行悄悄丟掉，比顯示一個樸素的字串糟糕。

### 一次改動的完整流程

1. 頁面 `POST /v1/settings`，只送有改的那幾個 key；
2. daemon 原子地把它 merge 進 `~/.config/clawdline-next/config.json`，回一份完整快照；
3. 頁面拿快照更新畫面，然後送 `{"kind":"changed"}`；
4. 殼重讀檔案，重新註冊熱鍵、重建選單，然後送一份新的 `clawdline-settings-state`。

**控制項顯示的是回來的東西，不是送出去的東西。** 這是舊 app 的
`apply()` → `clawdlineConfigChanged` 那條路，一字不差。

錄熱鍵多一步：殼在聽的時候會先把原本的組合放掉（不然按下去的那一刻會觸發正在等它的那個東西），
等頁面寫完說 `changed` 才裝回去；寫失敗的話頁面送 `stopRecording`，關視窗也一樣。

## `/v1/settings` 能改哪些 key

權威清單在 `internal/adapters/nextconfig/nextconfig.go` 的 `Settables`，
schema 在 `api/v1/settings.schema.json`。**要在設定視窗多一列，先在那張表加一行。**
不在表上的 key 一律 `400 bad_request`，這就是 schema 的 `additionalProperties: false`。

key 的拼法、值的範圍都跟舊 app 的 `Config` 一樣，所以一行從哪個檔案抄到哪個檔案都是同一個意思。
`on_state_change` 只讀不寫：它是一串 argv，不是一行命令列，一個文字框只會招來那種
把「路徑裡有空格」拆成兩個參數的錯誤。

## 這個 build 還沒接上的東西

設定視窗畫得出來、寫得進去，但下面這些**目前沒有人在讀那個值**，或是刻意沒做：

- **輸入條那一頁的每一列**（位置、寬度、不透明度、閱讀面板）——clawdline-go 還沒有輸入條。
  值寫得進檔案，之後輸入條做好就會讀到。
- **語音輸入的兩個秒數與引擎**——同上，還沒有聽寫。
- **遠端那一頁的開關與通道**——`remote`、`remote_write`、四個推播開關、`remote_tunnel`、
  `remote_hostname` 都寫得進檔案；tunnel 本身、已配對裝置清單、Cloud 登入卡片沒有後端，所以沒有畫。
- **派工那一頁的「派工的規矩」卡片與「排程」清單**——需要 policy 檔與排程後端，沒有畫。
- **Claude Code Hook 的安裝鍵是關著的**（`hooks.supported` 為 `false`）。
  讀的那半是真的：它會去 `~/.claude/settings.json` 找命令列裡含 `clawdline-next/hook.sh` 的項目，
  也就是**這個 app 自己的**項目。舊 app 找的是 `clawdline/hook.sh`，兩個字串互不包含，
  所以兩個 app 看不到也刪不掉對方的設定——這是任何一方可以動那個檔之前必須先成立的性質。
  沒有打開是因為：clawdline-go 沒有 hook 腳本、沒有 notes 目錄、也沒有任何東西會去讀那些 notes。
  裝下去只會讓 Claude Code 每一輪多跑八個沒人看的命令，代價是改了別人的設定檔。
  要打開的話要做三件事：一支寫 note 的腳本放進 `~/.config/clawdline-next/hook.sh`、
  一個讀 notes 的 adapter、以及 `SettingsWindow.swift` 裡 `hooksReading()` 的 `supported` 改成真的
  （install/uninstall 照舊 app `Sources/HookBridge.swift` 的 `adding`／`removing`，
  只動自己的項目，並在第一次寫入前留一份備份）。

## 要做一個新平台的殼，最少要做什麼

1. 開一個 webview 視窗，載 `http://127.0.0.1:7727/settings.html`，標題是「Clawdline 設定」，深色背景。
2. 載入前把 `<state dir>/local-token` 當成 `clawdline-next` cookie 放進去。
3. 在 document-start 注入 `window.__clawdlineShell = { post: <把物件送回原生的函式>, settingsWindow: { platform: "linux" } }`。
4. 收上面那八種訊息，用 `window.dispatchEvent(new CustomEvent(...))` 回三種答案。
5. `ShellState` 裡答不出來的欄位就給空的：`fonts: []` 會讓字型下拉只剩檔案裡那一個值，
   `hooks.supported: false` 會讓那顆按鈕維持關著。**不要編。**
