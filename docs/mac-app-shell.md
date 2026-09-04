# Mac App 的外殼：三個候選，以及為什麼是第三個

**這是一份保留下來的設計，不是現況說明。** 它在 2026-09-04 由一個只讀的設計節點寫成，
使用者讀完之後選了範圍 **B**：做到 Project 頁為止，**Mac App 的主視窗這一輪不做**。
所以底下第 3 章的結論還沒有任何一行程式碼實現它——它被保留在這裡，是為了讓
「為什麼是 B′ 而不是 B」在半年後還答得出來。

**行號綁在 `f94e07aa`（2026-09-04）**，而這棵樹每天都在動。行號會失效；
它們在這裡的用途是讓每一個結論可以被回頭查核，不是給讀者拿去對照今天的檔案。
要重新量，`git show f94e07aa:<檔名>`。

**已經照這份文件做掉的：** 第一到第三刀（側邊欄與「哪一頁」）、第四刀（Project 的資料路由）、
第五刀（Project 頁的畫面）。**沒有做的：** 第六刀，Mac App 的主視窗——
它的追蹤條目是 `docs/backlog.yaml` 的 `B-MAC-APP-HAS-NO-WINDOW-OF-ITS-OWN`。

相關：[`docs/api.md`](api.md)（路由與授權）、[`docs/backlog.yaml`](backlog.yaml)。
加一頁要做的三件事寫在 `docs/web-pages.md`，它隨側邊欄那一刀一起進來——
**在那一刀落地之前這個檔案不存在**，所以這裡不連它。

---


> **這是一份設計，不是現況說明，也沒有動到 repo 裡任何一個檔案。**
> 所有行號量測自 `f94e07aa`（2026-09-04），工作區在開始與結束時 `git status` 都是乾淨的。
> 行號會隨著任何一刀下去而失效——它們在這裡的用途是讓每一個結論可以被回頭查核，
> 不是給讀者拿去對照未來的檔案。
>
> 交付任務 `9d487db4`。這份文件要能被反駁：底下每一個判斷都指出它依據的檔案:行號，
> 沒有出處的地方我會明說「我沒有驗到」。

---

## 0. 這份文件回答什麼

三個問題，順序是它們互相依賴的順序：

1. **Mac App 應該原生重寫一份，還是嵌入現有的網頁 UI？**（第 3 章）
2. **側邊欄＋頁面這個形狀，兩邊要怎麼同時長出來？**（第 4 章）
3. **狀態層要不要從 JSON 搬進 SQL？**（第 5 章）

第 6 章是 `stablyai/orca` 的對照，第 7 章是切法，第 8 章是要使用者拍板的問題，
第 9 章是我沒驗到的東西。

---

## 1. 先更正四個前提

交辦已經更正了兩個。我另外量到兩個不一樣的數字，都不影響結論，但寫下來比較誠實。

| # | 說法 | 我量到的 | 影響 |
| --- | --- | --- | --- |
| 1 | 「網頁版已經完成過了」 | **側邊欄還沒做。** `grep -ril sidebar Resources/web/ docs/ Sources/` 零命中；`docs/backlog.yaml` 1381 行裡也沒有 | 交辦已更正，成立 |
| 2 | 「整個前端沒有任何路由概念」 | **有，但只路由一件事。** `Resources/web/app/js/input/route.js:30` 的 `routeTo(hash)` 只認得 `#session=<id>`；`route.js:49` 綁了 `hashchange`；`main.js:296` 在 boot 裡呼叫它一次 | **重要**。不是從零長路由，是**在一個已存在、已被通知綁住的 hash 路由上加第二個維度**——而它現在的解析是 `/(?:^|[#&])session=([^&]*)/`，這代表 `#page=project&session=abc` 這種寫法今天就會被正確解出 session。第 4.1 節據此設計 |
| 3 | 「Usage 現在是 68 個元素的覆蓋層」 | **146 個元素、59 個帶 id、91 行**（`index.html:453`–543 那一段，用 HTMLParser 數的）；`main.js:200` 的繫結表另外只點名 50 個 id | 數量級一致，結論不變 |
| 4 | 「這台機器現在有 58 個 worktree」 | **三個不同的數字，而它們是三件不同的事**：`git worktree list` 說 37（不含主樹，`prunable` 為 0）；broker 的 slot 目錄 `~/Library/Application Support/Clawdline/worktrees/clawdline-3b9e26c1/` 有 36 個；`git branch --list 'clawdline/task/*'` 有 **85** 個 | **這是第 4.4 節整章的起點。** 「worktree」在這台機器上不是一個數字，是三個，而使用者想看的那個列表要選一個 |

還有一個交辦沒提、但我在讀 `Settings` 的時候撞到的：

> **Mac App 的 Settings 已經是「側邊欄＋頁面」了，只是它是橫的、而且住在自己的視窗裡。**
> `Sources/Settings.swift:403` 的 `makePanes()` 回傳六個 `SettingsPane`
> （general / bar / dictation / remote / orchestrator / hooks），
> `Settings.swift:1842` 的 `TabStrip` 是那條橫向頁籤，每個 pane 是左右兩欄。
> 所以「頁面切換」這個 idiom 在原生端不是新東西，它已經被寫過一次、也已經有它自己的
> 視覺語彙（`Settings.swift:1839` 的註解：「Two tab strips in one app that do not look alike
> are two tab strips somebody has to learn separately.」）。

---

## 2. 現況：兩邊各長什麼樣

### 2.1 網頁版

| 事實 | 數字 | 怎麼量的 |
| --- | --- | --- |
| JS | 21,848 行 / 41 個模組 | `wc -l Resources/web/app/js/*/*.js main.js` |
| CSS | 4,066 行 / 17 個檔 | `wc -l Resources/web/app/css/*.css` |
| `index.html` | 1,042 行 | `wc -l` |
| 頁面的字串 | **523 個 key** | `grep -oE '^\s*"[a-zA-Z][A-Za-z0-9]*":' Sources/RemotePage.swift \| wc -l` |
| 覆蓋層 / sheet | **10 個**（keys, settings, start, info, coordinator-controls, action-confirm, command, schedule-history, schedule-form, schedule-delete-confirm） | `grep -n 'class="overlay"' Resources/web/index.html` |
| 版面骨架 | `<main class="app" data-view="list" data-pane="on">`（`index.html:183`），CSS 是兩欄 grid `360px minmax(0,1fr)`（`app/css/shell.css:4`–11） | |
| `data-view` 的值域 | 只有 `list` \| `detail`，而且只在手機寬度下有意義（`app/js/input/keys.js:51`、`edges.js:130`） | |

**畫面現在的分區是「清單 / 明細」，不是「頁面」。** `data-pane="off"` 把明細欄整個隱藏
（`shell.css:10`–11），手機上 `data-view` 決定哪一欄在前面。沒有第三個維度。

**入口已經在了。** 左上角的 wordmark 是一顆按鈕：

```
index.html:158  <button class="brand" id="brand" ...><canvas id="brand-mark">…<b>clawdline</b></button>
input/settings.js:113  els.brand.addEventListener("click", function () { Settings.toggle(); });
```

而 `index.html:154`–157 的註解已經把理由寫死了：「Tapping a product mark to reach them is a
shape people already know, and it is the highest-affordance thing in this header」。
**使用者說的「左上角 logo 點開側邊欄」，是把這顆已經存在的按鈕從開 sheet 改成開側邊欄。**

### 2.2 Mac App

| 東西 | 檔案 | 是什麼 |
| --- | --- | --- |
| 主選單 | `main.swift:188`–254 | App / Window / Help 三個選單，⌘H = Home、⌘, = 編輯 config、⌘Q、⌘W、⌘M |
| 狀態列 | `main.swift:258`、`302` | `NSStatusItem` + 一個選單 |
| **Home 視窗** | `Onboarding.swift:814`–952 | 820×740 的 `NSWindow`，深色釘死，`NSScrollView` + 三張 `HomeRouteCard`（本機 / Cloudflare / Cloud），**全手排版面**（`layoutHome()`），不是 Auto Layout |
| **Settings 視窗** | `Settings.swift:52`、`403`、`1842` | 六個 pane + 橫向 `TabStrip`，每個 pane 兩欄 |
| **提示面板** | `Controller.swift:310`–312 | `NSPanel`，`[.borderless, .nonactivatingPanel]`，`.floating` level，`[.canJoinAllSpaces, .fullScreenAuxiliary]` |
| 瀏海 | `NotchIsland.swift`（1,145 行） | 常駐、在面板不在的時候還在（`main.swift:19`–21 的註解） |
| 原生 UI 合計 | **13,681 行** | Panel + Controller + Settings + Onboarding + NotchIsland + WindowChrome + StackLog + Mascot + AssistantLogo + SessionImagePreview + Markdown |

**App 已經是 `.regular`**（`main.swift:11`），有 Dock 圖示、有主選單。所以「一個固定、
隨時可以打開來看的頁面」不需要改 activation policy，只需要一個新的視窗——
而這個 app 今天**沒有任何一個常駐主視窗**：Home 是開場白，Settings 是設定，面板是彈出物。

**沒有 WebKit。** `build.sh:774` 的 `swiftc` 只連了
`AppKit Carbon ServiceManagement Speech AVFoundation Network`，
`grep -rn "WebKit\|WKWebView" Sources/` 零命中。
`Resources/Clawdline.entitlements` 也確認 **App Sandbox 沒開**（它的註解自己寫了理由）。

### 2.3 「終端跳出來」那條路，以及它會不會被撞到

這條路由四段組成，每一段都跟主視窗無關：

1. `HotKey.swift:14` —— Carbon `RegisterEventHotKey`，**刻意不用 NSEvent global monitor**，
   因為那需要輔助使用權限（`HotKey.swift:3`–5 的註解）。
2. `Controller.swift:616` `show()` —— 記下 `previousApp = NSWorkspace.shared.frontmostApplication`
   （`Controller.swift:622`），然後 `NSApp.activate(ignoringOtherApps: true)`（`:651`）。
3. `Controller.swift:700` `hide(returnFocus:)` —— 把焦點還回去。
4. `Targets.swift:1020` `reveal(_:activate:)` → `ITerm.reveal` / `Tmux.reveal`
   （`Tmux.swift:721`），讓終端跟著面板走。

**它跟一個新主視窗的三個真實衝突：**

| 衝突 | 出處 | 嚴重度 |
| --- | --- | --- |
| `NSApp.activate(ignoringOtherApps: true)` 會把 app 帶到前面。今天 app 沒有常駐視窗，所以只有面板浮起來；有了主視窗之後，**⌥Space 可能把整個主視窗拉到終端前面**，那正是這個 app「存在就是為了不做」的事（`Tmux.swift:733` 的原話：「would take somebody's keyboard away from what they were typing into, which is the failure this whole app exists not to commit」） | `Controller.swift:651` | **高，但可解**：主視窗設 `hidesOnDeactivate = false` 且不參與 activate 的 ordering，或改用 `NSApp.activate(ignoringOtherApps:)` 之後立刻 `panel.makeKeyAndOrderFront` 而不動其他視窗。要驗。 |
| `applicationShouldHandleReopen`（`main.swift:109`）現在的行為是「有可見視窗就只 activate，否則開 Home」。多了一個主視窗之後，點 Dock 圖示該開哪一個變成一個要決定的事 | `main.swift:109`–116、`Onboarding.swift:10` `HomeReopenPolicy` | 中，是純政策 |
| **鍵盤衝突是真的，而且 ⌘K 與 ⌘J 在兩邊剛好都不同義。** 面板手寫了 20 個 ⌘ 組合（`Panel.swift:698`–740）。兩邊逐鍵比對：<br>　⌘K —— 面板：開/關目標清單（`Panel.swift:714` `onToggleList`）／網頁：把焦點丟進 session 列表並選一列（`keys.js:42`–47）<br>　⌘J —— 面板：開/關輸出窗格（`Panel.swift:717` `onToggleOutput`）／網頁：開關明細欄，手機上是切 list↔detail（`keys.js:48`–53）<br>　⌘I —— 面板：無／網頁：開 session 的 info sheet（`keys.js:55`–61）<br>而 macOS 的主選單 key equivalent **會贏過視窗**——任何一個進主選單的 ⌘ 鍵會同時偷走兩邊的 | `Panel.swift:714`、`:717`、`keys.js:42`–61 | **高**。第 8 章列成待拍板 |

`Panel.swift:695`–697 的註解還說「This app is an accessory (no Dock icon, no menu bar)」——
**那句話對 `main.swift:11` 已經不成立了**（activation policy 是 `.regular`，而且
`installMainMenu()` 在 `main.swift:14` 就被呼叫）。這不是我的任務範圍，但它說明面板的鍵盤
處理是在「沒有主選單」的前提下寫的，而那個前提已經沒了。

---

## 3. 核心問題：原生重寫，還是嵌入網頁？

我找到**三個**候選，不是兩個。第三個是關鍵，而它之所以存在，是因為
`RemoteServer.swift:750`–753 這四行：

```swift
/// Every route that answers with a body and closes. Split out from the connection handling so
/// that a test can ask it a question without opening a socket.
func route(_ request: Request) -> Response {
    withCachePolicy(dispatch(request))
}
```

**這個 app 的 HTTP 路由層已經是一個純函式了**，它不需要 socket、不需要 port、
不需要 `Config.remote`。這改變了整個成本結構。

### 候選 A —— 原生重寫

用 AppKit 把側邊欄、Project 頁、Usage 頁、Settings 頁重畫一次。

### 候選 B —— `WKWebView` 指向 `http://127.0.0.1:<port>`

app 內嵌一個 web view，載入自己已經在服務的頁面。

### 候選 B′ —— `WKWebView` + `WKURLSchemeHandler`，**不開 socket**

自訂 scheme（例如 `clawdline-app://`）的 handler 直接呼叫
`RemotePage.page(for:)`（`RemotePage.swift:721`）、`RemotePage.asset(_:)`（`:814`）、
`RemoteServer.shared.route(_:)`（`RemoteServer.swift:752`），完全不經過網路。

### 三者的代價，逐項

#### 3.1 這個 app 的遠端開關是**兩個**，而且**預設都是關的**

```
Config.swift:293   var remote = false        // 監聽 socket
Config.swift:355   var remoteWrite = false   // 允許寫入
```

- `RemoteServer.apply()`（`:453`）讀 `Config.shared.remote`，是 false 就不 listen。
- `RemoteServer.swift:470` 綁 `.ipv4(.loopback)`，只綁 loopback。
- `RemoteServer.swift:3364`–3371 的 `writeGate`：**只要 `request.source` 是 `.http`
  而 `Config.remoteWrite` 是 false，每一個寫入路由都回 `403 write_disabled`**，
  跟裝置權限無關。
- `RemoteServer.swift:840` 的 `open` 白名單只有 6 條路徑 + `/app/` + 圖示；
  其餘一律需要配對裝置，否則 `401 unauthorized`。

**這對候選 B 是致命的三連擊：**

1. `remote` 是 false 的時候，`127.0.0.1:7717` 上沒有東西。web view 拿到的是 WebKit 的
   連線失敗頁。要讓視窗有東西，就得**替使用者把一個他刻意關著的監聽 socket 打開**——
   而 `RemoteServer.swift:61`–63 的註解把那個決定寫得很清楚：
   「a listening socket is the difference between a program on your machine and a service on
   your machine, and that difference should be a thing you did on purpose.」
2. `remoteWrite` 是 false 的時候，視窗是**唯讀的**。使用者說的「另一塊是 Clawdline 輸入
   指令的部分」在那個視窗裡會全部 403。要讓它能用，就得再替使用者打開第二個開關——
   而那個開關的說明文字自己寫著「it is off by default because typing into a session runs
   code on this Mac」（`RemoteServer.swift:3368`–3370）。
3. **token。** `RemoteAuth.localToken()`（`RemoteAuth.swift:389`）確實會鑄一個
   `caps: [.read, .send, .admin]` 的「This Mac」裝置（`:415`），而
   `approvedDevices`（`:100`）明確排除 `local`，所以 `syncWriteCapability()`（`:508`）
   不會把它降權。所以 token 這一關過得去——**但它過不了第 1 點的 `writeGate`**，
   因為那道閘是看 `request.source` 而不是看 caps。

> **結論：候選 B 要求把兩個安全開關的語意改掉，或替使用者打開它們。**
> 那不是一個 UI 決定，是一個安全決定，而且它是這個 repo 花了最多字寫理由的那一個
> （`docs/remote.md` 開頭 40 行）。**我不建議 B。**

**一個容易讓人誤判的事實，所以寫在這裡：這台機器上兩個開關現在都是開的。**
我讀了 `~/.config/clawdline/config.json`：`remote = true`、`remote_port = 7717`、
`remote_write = true`。**所以候選 B 在這台機器上今天就會動**——而那正是它危險的地方：
它會在開發者的機器上完美運作，然後在每一個全新安裝上開出一個錯誤頁與一整排 403。
設計要對兩種狀態都成立，而**預設狀態是 `false`/`false`**（`Config.swift:293`、`:355`）。

#### 3.2 候選 B′ 把上面三個問題一次解掉——但它有自己的洞

`Request.Source` 是一個**封閉的兩態列舉**：

```
RemoteServer.swift:5386  enum Source { case http; case verifiedCloud(sender: String) }
RemoteServer.swift:5397  /// There is deliberately no header spelling for this initializer.
                          /// Only the in-process CloudAppBridge can name a verified-cloud source.
```

Cloud 那條路已經示範過「一個 in-process 的來源可以繞過 `.http` 的閘」——
`permission(for:)`（`:2819`）對 `.verifiedCloud` 直接回 `[.read, .send]`，
`writeGate` 的 `if case .http` 對它不生效。

所以 B′ 要加的是**第三個 case**（例如 `.embeddedWindow`），語意是
「這個請求來自這台 Mac 上這個 app 自己畫的視窗，沒有經過任何 socket」。
它不需要 `Config.remote`、不需要 `Config.remoteWrite`、不需要 token 上線，
因為**能畫這個視窗的人已經在鍵盤前面了**——這跟面板打字進終端是同一個信任等級。

**但 B′ 的洞是這個：不是每一條路由都走 `route()`。**
`RemoteServer.swift:619`–660 的 `handle(_:on:)` 在進 `route` 之前攔掉了五族：

| 路由 | 行號 | 為什麼不走 route |
| --- | --- | --- |
| `GET /v1/events`（SSE） | `:622` | 「the one route that does not answer and close — and the one that carries everything」 |
| `POST /v1/voice` | `:633` | whisper 熱機 1.6 秒、冷機約 12 秒 |
| `POST /v1/intents` | `:643` | 模型一輪 3.2–5.1 秒、deadline 30 秒 |
| Usage analytics 讀取 | `:648` | 有自己的 bounded worker 與 admission budget |
| bounded slow reads（transcript / info / places） | `:657`+ | 「a request is not slow, it is *exclusive*」 |

**SSE 是其中最要緊的一條，因為它是整個頁面的活水**：
`app/js/net/live.js:112` `new EventSource("/v1/events")`。
一個 `WKURLSchemeHandler` 可以重複呼叫 `didReceive(data:)`，理論上能餵 SSE；
**但「自訂 scheme 上的 `EventSource` 在 WKWebView 裡能不能用」我沒有驗到**（見第 9 章）。
如果不能，B′ 就要在 `preload`/`WKScriptMessageHandler` 上接一條 `handlers.*` 的旁路——
那是真工作量，但它是**一個檔案的工作量**，因為 `main.js:76`–84 的 `bindTranscriptEvents`
和 `main.js:184` 的 `useApi(Live)` 已經把「傳輸」抽成一個可換的東西了
（`net/api.js` 的 `useApi()`，`main.js:18` 匯入；`mock` / `live` / `cloud` 三種傳輸已經共存）。

> **這一點值得單獨說：這個前端已經被設計成可以換傳輸。**
> `main.js:93` `chooseTransport({mock, origin, config})` 有三個答案，
> `main.js:100`/`133`/`184` 分別把 `idleClient` / cloud client / `Live` 塞進同一個 seam。
> **第四種傳輸（in-process bridge）是這個設計已經預留的位置，不是要撬開的東西。**

#### 3.3 逐項比較表

| 維度 | A 原生重寫 | B 指向 127.0.0.1 | **B′ in-process scheme** |
| --- | --- | --- | --- |
| **要寫多少** | 重畫 21,848 行 JS + 4,066 行 CSS 的等價物。今天全部原生 UI 才 13,681 行——這一刀等於**再寫一次比現有原生 UI 更大的東西** | 幾百行（一個視窗 + 一個 web view） | 一個 `WKURLSchemeHandler`、`Request.Source` 加一個 case、一條 SSE 或事件橋。我估 400–800 行，**加上把五族繞過 `route()` 的路由各接一條線** |
| **一致性（原生 vs 網頁控制項同窗）** | 完美 | 差：視窗裡是深色網頁（`index.html:20` `color-scheme: dark` 釘死），旁邊是原生標題列與選單。而且網頁刻意 `user-scalable=no`、`maximum-scale=1`（`index.html:19`），那是為手機做的取捨，在桌機視窗裡是純損失 | 同 B。**但可以只嵌「頁面」那一塊，側邊欄與視窗框架用原生畫**——見 3.4 |
| **離線 / server 沒起來** | 不存在這個狀態 | **視窗是 WebKit 的錯誤頁**。要嘛替使用者開 `Config.remote` | **不存在這個狀態**：沒有 socket 就沒有「起不起得來」。頁面的資料來源是同一個 process 的記憶體 |
| **鍵盤與選單** | 完全掌控 | 差：主選單的 key equivalent 會贏過 web view，⌘K/⌘J/⌘I 被吃掉，而面板同時也要這三個鍵中的兩個 | 同 B，但可以在 `WKWebView` 的 `performKeyEquivalent` 攔截，或乾脆**不把這三個鍵放進主選單**。仍是一個要拍板的取捨 |
| **14 種語言** | **這是 A 唯一真正的優勢，而且它比看起來小。** `Sources/Strings.swift:20` 的 `protocol Copy` 讓少一個字串是**編譯錯誤**（`Strings.swift:5`–8 的註解），`Copy+English.swift` 有 793 個成員 × 14 種語言。原生直接讀 `L.t.*` | 網頁走 `RemotePage.strings()`（`RemotePage.swift:38`）把 `Copy` 攤成 523 個 key 的 JSON，`tools/check-web-strings.py` 守住三個名字之間的一致 | 同 B。**字串沒有第二份，兩邊都從 `Copy` 出來**——差別只在網頁那邊多一張 523 行的手寫對應表，而那張表已經有守衛 |
| **測試** | **這是 A 最大的隱形代價**：41 個 `.mjs` 套件（其中 16 個列在 `test.sh:606`–627 的 sealed roster）全部作廢，要在 Swift 重寫。它們今天是零依賴的純 Node，手工 stub DOM（`Tests/web-schedules.mjs:1`–24），跑得極快 | 全部保留，一行不改 | 全部保留。**新增的測試面只有 scheme handler 與事件橋**，而 `route()` 本來就是為了「不開 socket 也能問它問題」而拆出來的（`RemoteServer.swift:750`），所以它天生可測 |
| **跟終端跳出來共存** | 面板不受影響（它是獨立的 `NSPanel`）。但 3.3 的 activate 問題兩案都一樣 | 同左 | 同左。**三個候選在這一點上沒有差別**——衝突來自「多了一個常駐視窗」，不是來自視窗裡裝什麼 |
| **建置成本** | 0（不加 framework） | `-framework WebKit` 一個字（`build.sh:774`）。App Sandbox 沒開（`Clawdline.entitlements`），不需要新 entitlement | 同 B |
| **Cloud 那一半** | **A 會產生第二套 UI**：手機/Cloud 走網頁，Mac 走原生，同一個功能兩份實作、兩次改、兩次忘記改 | 一份 | 一份 |
| **`RemoteServer.swift` 的行數天花板** | 不動 | 不動 | **要動**：`tools/check-architecture-boundaries.sh:184` `remote_server_ceiling=5570`，現況 5,565 行，只剩 **5 行**餘裕；而且 `:488` 有反向守衛（餘裕太多也紅）。加一個 `Source` case 就會撞到 | 

#### 3.4 我的建議：**B′，而且只嵌「頁面」，不嵌整個視窗**

理由三個，按份量排：

1. **候選 A 要求重寫的量，比這個 app 現有的全部原生 UI 還大**，而且會**永久製造兩套實作**
   （Mac 原生一套、手機/Cloud 網頁一套）。這個 repo 已經花了很大力氣讓字串只有一份
   （`Strings.swift:5`）、讓 icon 只有一份（`main.js:253`–263 的註解）、讓
   modulepreload 的清單只有一份（`RemotePage.swift:754`–757 的註解）。
   **原生重寫是把「只有一份」這個原則在最大的那一塊上放棄掉。**

2. **候選 B 要求動兩個安全開關的語意**，而 B′ 用一個已經被示範過的機制
   （`Request.Source` 的第三個 case）把同一件事做成不需要動任何開關。
   B 相對於 B′ 唯一的優勢是「少寫一個 scheme handler」，代價是「這個視窗需要你先打開
   遠端服務並允許遠端寫入」——那是一個沒有人會覺得合理的說明句。

3. **「只嵌頁面」把一致性的損失壓到最小。** 使用者描述的形狀本來就是兩塊：
   一塊是網頁版的功能與呈現，一塊是輸入指令。那條界線可以剛好落在原生與網頁之間：

```
┌─────────────────────────────────────────────────────┐
│ ● ● ●   Clawdline                        [原生標題列] │
├──────────┬──────────────────────────────────────────┤
│          │                                          │
│ [原生]   │            [WKWebView]                   │
│ 側邊欄   │            Sessions / Usage /            │
│          │            Project / Settings            │
│ ▣ 專案   │            —— 就是今天的網頁             │
│ ◷ Usage  │                                          │
│ ⚙ 設定   │                                          │
│          ├──────────────────────────────────────────┤
│          │  [原生] 指令輸入 —— 就是今天面板那顆卡片  │
└──────────┴──────────────────────────────────────────┘
```

側邊欄是原生的 → 它可以跟主選單、⌘1/⌘2/⌘3、以及 macOS 的 `NSSplitViewController`
慣例對齊，不會有「網頁做的假側邊欄」那種違和。
輸入那一塊是原生的 → **它就是 `Controller.swift` 已經寫好的那張卡片**
（`Panel.swift:5`–58 的 `Style`、`Controller.swift:310` 的 container/card/scrim），
只是換一個宿主：從 `NSPanel` 搬到主視窗的下半。
而中間那一大塊是網頁 → 21,848 行不用重寫，41 個測試套件不用重寫，Cloud 不用分岔。

**這個切法還有一個附帶好處**：它讓「終端跳出來」與「常駐視窗」共用同一個輸入元件，
而不是兩份。今天面板的輸入卡片已經被寫成一個可以被放進任何 `NSView` 的東西
（`Controller.swift:310`–340 建 container → cardHost → card），**沒有假設它的宿主是面板**。
我沒有逐行驗證這個獨立性（見第 9 章）。

---

## 4. 側邊欄＋頁面：兩邊同時長出來的形狀

這一章的每一節都是「網頁那半怎麼長」，因為在 B′ 之下**它就是 Mac App 那半**。

### 4.1 路由：加第二個維度，而不是換掉現有的

今天的 hash 路由（`input/route.js`）只認 `session=`，解析式是
`/(?:^|[#&])session=([^&]*)/`（`route.js:25`）——它用 `[#&]` 當前綴，
所以 **`#page=project&session=abc` 今天就會被正確解出 `abc`**。這代表：

> **加頁面路由不需要改 `sessionInHash`，只需要在 `routeTo` 裡多解一個 `page=`。**

而 `routeTo` 有兩個呼叫者要一起顧：
- `route.js:49` 的 `hashchange`
- `route.js:54`–66 的 service worker `{type:"navigate", url}`（推播點進來的路徑）

**建議的形狀**（一句話規格，不是實作）：

```
#page=<sessions|project|usage|settings>[&session=<id>]
```

- 沒有 `page=` → `sessions`，跟今天一模一樣。**舊的推播 URL `/#session=abc` 不會壞。**
- `page=` 決定 `<main>` 上的一個新屬性（`data-page`），跟現有的
  `data-view` / `data-pane` 平行，不取代它們。
- **`core/layout-diagnostics.js:415` 的 MutationObserver 現在監看
  `["class","style","hidden","data-view","data-pane"]`**——新屬性要加進去，
  否則版面診斷對新頁面是瞎的。這一行是我在讀的時候撞到的，很容易漏。

### 4.2 Usage：離「頁面」比想像中近很多

`app/js/view/usage.js:671`–687：

```js
function open() {
    elements.settings.hidden = true;
    elements["usage-analytics"].hidden = false;
    elements.app.hidden = true;              // ← 把整個 <main> 藏起來
    doc.body.style.overflow = "hidden";
    …
}
function close() {
    elements["usage-analytics"].hidden = true;
    elements.app.hidden = false;
    …
}
```

**Usage 今天已經是一個「蓋掉整個 app 的全螢幕檢視」了，不是浮在上面的 modal。**
它缺的是三樣：一個網址、上一頁/下一頁、以及共用的框架（現在它自己畫
`usage-close`「Back to sessions」按鈕，`index.html:458`）。

> **所以「Usage 真的改成頁面」的第一刀非常小**：把 `open()`/`close()` 改成寫 hash，
> 由路由器來切 `hidden`，並把那顆「Back to sessions」拿掉換成側邊欄的選取狀態。
> DOM 一個都不用搬。146 個元素原地不動。

它自己內部還有兩個頁籤（`usage-overview` / `usage-agent-work`，`view/usage.js:659`–668，
`role="tablist"`），那是**頁面裡的頁籤**，跟側邊欄是不同層級，不要混。

### 4.3 Settings：兩邊的 Settings 不是同一個東西

| | 網頁的 Settings | Mac 的 Settings |
| --- | --- | --- |
| 內容 | 3 個 block：通知、助理圖示、逐字稿排序，加一顆 Usage 按鈕、一行版本 | 6 個 pane：general / bar / dictation / remote / orchestrator / hooks |
| 出處 | `index.html:571`–612 | `Settings.swift:403` |
| 主體 | **這個瀏覽器、這台裝置**——`index.html:616`–618 的註解自己寫死了：「which is about this browser on this device」 | **這台 Mac** |

**這是一個真正的設計分岔，不是實作細節。** 側邊欄上一格叫「設定」的東西，
在手機上該是前者，在 Mac 主視窗上使用者八成期待後者（熱鍵、語言、遠端開關⋯⋯）。
三種答案：

1. 側邊欄的「設定」= 網頁那份，Mac 的六個 pane 維持在自己的視窗（⌘,）。
   **一致，但 Mac 使用者會覺得那個設定頁沒有東西。**
2. 側邊欄的「設定」在 Mac 上多長出 Mac 那六個 pane——但那需要把 6 個 pane 的控制項
   （`SwitchView`、`ChoicePopUp`、`ValueSlider`、`AppScopeView`、`DeviceChips`、
   `ScheduleFormView`⋯⋯共 11 個自訂 `NSView` 子類）搬進網頁，**這是一次不小的重寫**。
3. 側邊欄的「設定」是一個原生 pane（不是網頁），跟其他頁面並排。
   **在 B′ 的「只嵌頁面」形狀下這是可行的**——側邊欄本來就是原生的，某一格對應原生內容
   而不是 web view，只是切換時換掉右邊那塊的內容。

**我建議 3。** 它讓 `Settings.swift` 那 3,582 行一行都不用動，
而且它讓「側邊欄可以放原生頁面」這件事一開始就是設計的一部分，
不是之後為了塞 Settings 而破例。

### 4.4 Project 頁：資料已經存在，而且比想像中乾淨

使用者要的是「Project 底下是各 worktree 完成的 Feature」。我去查了資料在哪裡。

**先排除兩個直覺但錯的來源：**

- **`git worktree list` 不行。** 它給的是「現在磁碟上還在的」，37 個，而且大多是早就結束的
  任務。它不知道 Feature，也不知道有沒有落地。
- **`GET /v1/orchestrator/tasks` 只能看 30 天。**
  `Config.swift:513` `orchestratorTaskRecordLimit = 1350`、
  `Config.swift:526` `orchestratorTaskRecordRetentionDays = 30`。
  `docs/api.md:3583`–3585 自己說了：「`GET /v1/orchestrator/tasks` cannot answer a question
  about last month and never will」。

**對的來源是 usage ledger，而它從來不被掃。**
`~/Library/Application Support/Clawdline/Observability/usage.sqlite3`，
schema 在 `Sources/UsageLedger.swift:890`–921。我用唯讀連線實際查了：

```sql
-- 這個 join 今天就能跑
select e.value_label, count(distinct i.task_id)
from usage_intervals i
join usage_attribution_events e on e.interval_key = i.interval_key
where i.isolation = 'worktree'
  and e.dimension = 'feature'
  and e.decision  = 'accepted'
group by 1;
```

| 量 | 值 |
| --- | --- |
| `usage_intervals` 總列數 | 727 |
| `isolation='worktree'` 的列 | 240 |
| 其中 distinct `task_id` | **157** |
| 其中 `landing_state='landed'` 的 distinct task | **67** |
| **有 accepted feature 歸屬的 worktree task** | **85** |
| **它們構成幾個 Feature** | **19** |

**所以「Project → 各 worktree 完成的 Feature」今天是一張 19 列的表，不是 58 列。**
而且每一列都已經有：Feature 名稱、幾個 task、屬於哪個 project、產出量、落地狀態。

那 19 個 Feature 長這樣（前幾名）：

```
Clawdfather: machine coordinator          16 tasks
Clawdfather final integration             12 tasks   (跨 2 個 project)
App-side purpose onboarding root (…)      12 tasks
Keychain signing feature root 2a173c54     5 tasks
修復 Chat 對話載入延遲                       4 tasks
```

**缺的只有一樣：branch。** `usage_intervals` 有 `working_dir`、`isolation`、`task_id`、
`landing_state`，沒有 branch 欄位。但 branch 是**可推的**：
`OrchestratorDraft.worktreeBranch(for:)`（`OrchestratorDraft.swift:545`）
就是 `clawdline/task/<task-id>`，而 `working_dir` 對隔離任務就是
`worktreePath(project:taskID:)`（`:577`），結尾是 task UUID。
所以 `task_id` → branch 是一個純函式，不需要新欄位。

**這解釋了那三個數字為什麼不一樣，也給出使用者要拍板的東西**（第 8 章）：

| 「worktree」的三種意思 | 數量 | 保存多久 |
| --- | --- | --- |
| 磁碟上還在的 checkout | 37 | broker 掃掉就沒了 |
| `clawdline/task/*` 分支 | **85** | 永遠（除非手動刪） |
| ledger 裡做過事的隔離任務 | **157** | 永遠（nothing sweeps it） |
| ⤷ 其中完成過 Feature 的 | **85** | 永遠 |

### 4.5 那兩個 85 是巧合，而它們只重疊 53 個——這是 Q3 真正的難處

分支數與「完成過 Feature 的 task 數」都是 85，我一開始把它當巧合放過去。
後來我把 ledger 那 85 個 `task_id` 轉成 `clawdline/task/<id>`（用
`OrchestratorDraft.worktreeBranch(for:)`，`OrchestratorDraft.swift:545` 的規則），
跟 `git branch --list` 做**集合差**（`comm`，不是 `diff`）：

```
ledger 推出的分支名   85
git 實際有的分支      85
  ├─ 兩邊都有         53
  ├─ 只在 ledger      32   ← 分支已被刪，但那批工作的 Feature 歸屬還在
  └─ 只在 git         32   ← 分支還在，但 ledger 沒有給它 accepted Feature
```

再問「只在 git 的那 32 個落地了沒」（`git merge-base --is-ancestor <branch> main`）：

```
只在 git 的 32 個：  已併入 main 15  ／  未併入 17
兩邊都有的 53 個：   已併入 main 36  ／  未併入 17
```

**所以有 15 條分支，做的工作已經落地在 `main` 上，而 Feature 歸屬那一側完全看不到它。**
（原因不只一個：可能是 30 天以外、可能是 `no_unambiguous_accepted_head`——
`docs/api.md:3672` 說了 proposal-only、rejected、conflicting、absent 一律停在
`Unknown Feature`。我沒有逐個分類。）

> **這是 Q3 為什麼是使用者要拍板的事，而不是工程可以自己決定的事：**
> 沒有任何單一來源答得出「哪些 worktree 完成過 Feature」。
> 兩個來源給的數字一樣（85 vs 85），交集只有 53，**而且兩邊各有 32 個對方沒有的**。
> 這個頁面要顯示 53、85、117（聯集）還是 15+53（落地優先），是一個產品問題。

---

## 5. 狀態層：要不要搬進 SQL

### 5.1 這不是「引入 SQLite」，是「搬遷」——而且成本比想像低

`Sources/UsageLedger.swift:3` `import SQLite3`。**系統模組，`build.sh:774` 的連結行
一個字都不用加**（SQLite3 在 SDK 裡，不需要 `-lsqlite3`）。而且它已經開好了：

```
UsageLedger.swift:856  PRAGMA journal_mode=WAL;
UsageLedger.swift:857  PRAGMA synchronous=NORMAL;
UsageLedger.swift:858  PRAGMA busy_timeout=5000;
UsageLedger.swift:825  private let queue = DispatchQueue(label: "…usageledger")
```

WAL、5 秒 busy timeout、自己的序列佇列——**多行程並發已經被想過一次了。**
所以「要不要用 SQL」這個問題在這個 repo 已經被回答過「要」，範圍是 usage。
真正的問題是：**還有哪些狀態該過去。**

### 5.2 現況分類：三種性質，不能一概而論

我量了 `~/.config/clawdline/` 的全部：

| 檔案 | bytes | 性質 | 該不該搬 |
| --- | --- | --- | --- |
| `orchestrator.json` | **1,771,836** | 一個**有上限的集合**：216 個 task、29 個 delivery、263 個 activity、24 個 attestation、15 個 root assignment | **該搬。** 這是唯一一個「集合」，也是唯一一個因為容器限制而**遺忘**的 |
| `config.json` | 12,824 | **人會手改的文件**。`main.swift:353` 的選單有「編輯 config」、`:357` 有「重新載入」，`Settings.swift:2598` 還有一整個編輯器 | **不該搬。** 搬進 SQL 是把一個使用者能用 `vi` 修好的東西變成不能。這是退步 |
| `coordinator.json` | 2,939 | **單一紀錄的 compare-and-swap**。`generation` 是它的樂觀鎖，我讀到的現值是 **17**（跟 Clawdfather 說的 16→17 一致） | **不該搬（現在）。** 單筆 CAS 用整檔原子改寫是**正確**的做法，SQL 在這裡不會更好，只會多一個相依 |
| `coordinator-successions.json` | 3,929 | 小的 append-only 序列 | 中性。跟 coordinator 一起走 |
| `landing-queue.json` | 4,428 | 見 5.3 | 特殊，見下 |
| `remote.json` | 2,153 | 裝置與 token 雜湊，`0600` | 不該搬。它的安全屬性綁在檔案權限上 |
| `cloud-devices.json` 219 / `cloud-sequence.json` 69 / `push.json` 642 / `onboarding.json` 72 / `trusted-stacks.json` 294 | 小 | 不該搬。搬遷成本大於全部收益 |

**這三種性質的差別是：**
- **集合**（會長大、要查詢、要遺忘）→ SQL 贏。
- **文件**（人會讀、人會改、要能用文字編輯器修）→ JSON 贏，而且是壓倒性的。
- **單筆 CAS**（一個紀錄、一個世代號、要原子換）→ 平手，別動。

### 5.3 搬了具體得到什麼

#### (a) 那個 1350 可以拿掉——而且它今天正在說謊

`Config.swift:513` 的 `orchestratorTaskRecordLimit = 1350` 加上
`:526` 的 30 天保留，是一個**遺忘機制**，理由就是一個 JSON 檔不能無限長大。
它今天的後果 Clawdfather 已經量到（登記檔 38.6/天 vs ledger 45.0/天，
最忙的兩天整個不見）——而我從另一邊量到同一件事：

> `usage_intervals` 有 **157** 個 distinct 隔離任務、**727** 列 interval，
> 而 `orchestrator.json` 只剩 **216** 個 task。
> **兩份資料量同一批任務，一份被刪過，一份沒有。**

搬進 SQL 之後，那個上限不是「調大」，是**不需要**——因為換掉它的是 index 與
`WHERE finished_at < ?` 的分頁刪除，而不是「整檔要能載入記憶體」。

#### (b) landing-queue 會變成一句 SQL

`docs/api.md:3412`–3417 說得很清楚：「**There is no route that adds an entry, and that is
the point.** Membership is derived on every read from the task registry through the same
`workVisibility(state:landing:isolated:branchExists:branchMerged:)`」
（`Orchestrator.swift:5634`，消費者在 `OrchestratorLandingQueue.swift:165`）。

**「哪些交付躺著超過 24 小時」今天的成本 = 把 1.77 MB 讀進來、parse、然後掃全部 216 筆。**
搬進 SQL 之後那是：

```sql
select id, title, since from tasks
where landing_state = 'pending' and since < strftime('%s','now') - 86400
order by since;
```

而 `Orchestrator.swift` 那句自己的註解——
*「This is a dashboard, not a gate: reading or ignoring it changes no claim and blocks no
dispatch.」*——說明了為什麼沒人會經過它：**一個要花力氣才問得到的儀表板，等於沒有。**
把成本降到一句 SQL，不會自動讓人看它，但它是讓「開機時順手問一句」變得可能的前提。

#### (c) 第 4.4 節那個 join 就不用手寫了

Project → worktree → Feature 今天橫跨 SQLite（feature 歸屬）與 1.77 MB JSON（task 與
worktree），所以現在得手寫。兩邊都在 SQL 之後，它就是一句 join——
**而我在第 4.4 節已經證明那句 join 跑得起來**，只是現在只跑得了一半。

### 5.4 代價，包含一個我在讀的時候撞到的缺陷

#### (i) 現況其實不慢——所以不要用「效能」當理由

我實測了 `save()` 那條路的成本（用同一份 1.77 MB 的資料，Python 當代理）：

| 步驟 | 時間 |
| --- | --- |
| 序列化（pretty + sorted keys） | **5.0 ms** |
| 原子寫入 + fsync | **1.4 ms** |
| 讀取 + parse | 0.2 + 5.9 ms |

`Orchestrator.save()`（`Orchestrator.swift:10173`）在這個檔案裡有 **53 個呼叫點**，
沒有 debounce（`grep -n "debounce\|coalesc" Sources/Orchestrator.swift` 只有一處
無關的 `asyncAfter`）。所以每一次狀態變動都是約 **7 ms** 的同步工作。
**這不是效能問題。** 拿效能當搬遷理由會被實測打臉；真正的理由是**遺忘與查詢**。

#### (ii) 那 7 ms 裡有一半是白花的——`save()` 把檔案寫了兩次

```swift
Orchestrator.swift:10225  do {
Orchestrator.swift:10226      try FileManager.default.createDirectory(at: RemoteAuth.directory, …)
Orchestrator.swift:10228      try data.write(to: storeURL, options: .atomic)      // ← 第一次
          } catch { … }
Orchestrator.swift:10233  try? FileManager.default.createDirectory(at: storeURL.deletingLastPathComponent(), …)
Orchestrator.swift:10235  try? data.write(to: storeURL, options: .atomic)          // ← 第二次，同樣的 bytes
```

同一份 1.77 MB 被原子寫入兩次，每次 1.4 ms。**這是一個我在讀的時候發現的缺陷，
不是這次任務的範圍，我沒有修**（這是只讀任務）。它值得一張獨立的小工單，
而且它對 SQL 的討論有意義：**現況的實作品質，本身就是「先修現況還是先搬遷」這個
決定的一部分。**

#### (iii) 多行程：SQLite 的鎖跟 JSON 的失敗方式不一樣

今天這台機器上有五條線同時在寫狀態。兩種機制的失敗方式：

| | JSON 整檔原子改寫 | SQLite |
| --- | --- | --- |
| 失敗形態 | **last writer wins，安靜地覆蓋** | `SQLITE_BUSY`，**吵鬧地失敗** |
| 現有防護 | `Orchestrator.save()` 的 `storeSaveLock`（`:10174`）——**只保護同一個 process 內** | WAL + `busy_timeout=5000`（`UsageLedger.swift:856`–858） |
| 已知事故 | 記憶體裡記著「共用索引上的收據會過期」「git merge 會 stage 別人的檔案」——同一族問題 | Clawdfather 今天遇過一次 `sqlite: database is locked` 讓落地紅掉 |

**兩邊都會壞，但壞的方式不同，而 SQLite 那種比較好。**
「安靜地覆蓋掉另一個 process 剛寫的狀態」是這個 repo 已經被咬過好幾次的失敗類型；
`SQLITE_BUSY` 至少會叫。但 `busy_timeout=5000` 意味著**一次寫入最壞會卡主執行緒 5 秒**——
所以搬遷的同時，寫入必須進自己的序列佇列，就像 `UsageLedger.swift:825` 已經做的。

#### (iv) 其餘代價

- **遷移與回頭。** 需要一次性的 JSON → SQL 匯入，加上一段時間的雙寫或只讀回退。
  `OrchestratorStore.swift:15`–18 的註解警告過：「Every key string, default, legacy-shape
  branch and optional-versus-absent distinction is a compatibility contract with stores
  already on disk; tidying one is a data-loss bug, not a cleanup.」**這句話對 schema 設計
  是一個硬約束**：新 schema 必須能表達那些「有/沒有」的區別，不能用 `NULL` 一律代表。
- **備份。** 今天備份 `~/.config/clawdline/` 是 `cp -R`。SQLite 要用
  `sqlite3 .backup` 或 `VACUUM INTO`，否則備份到一個寫到一半的 WAL。
- **「使用者手改設定檔」這個能力。** 只在 `config.json` 上存在，而我建議 `config.json`
  不搬——所以這個代價是 **0**，前提是不搬錯東西。
- **測試。** `Tests/OrchestratorStoreTests.swift` 等一整族要改。這是真工作量。

### 5.5 跟「原生 vs 嵌入」的交互作用——Clawdfather 說對了，這是同一個決定的兩半

**如果 Mac App 要有一個 Project 頁顯示歷史，而那個歷史正在被 1350 刪掉，
那個頁面在第 31 天就是錯的。**

具體地說：第 4.4 節的 19 個 Feature 全部來自 usage ledger（沒有上限），
所以那張表現在是對的。但它缺的 branch、landing 狀態的細節、task 標題，
都得回 `orchestrator.json` 拿——而那份只剩 30 天。
**所以 Project 頁今天可以做，但它會有一個「30 天以前只有 Feature 名稱、
沒有 task 明細」的邊界**，而那個邊界不是設計選的，是一個 JSON 檔的容量限制選的。

> **這是我認為最值得使用者知道的一句話**：
> 側邊欄與 Project 頁不需要等 SQL 遷移就能做，
> **但那個頁面能顯示多久的歷史，是 SQL 那個決定決定的，不是 UI 決定的。**

### 5.6 我的建議：分三段，而且第一段不是遷移

1. **先修 `save()` 的重複寫入，並給它一個 debounce。**（小、獨立、立刻有收益）
2. **只把 `orchestrator.json` 的 `tasks` 集合搬進 SQLite**，其餘欄位（handoffs、waits、
   attestations⋯⋯）留在 JSON。理由：`tasks` 是唯一一個有 1350 上限的、也是唯一一個
   被查詢的。一次搬一個集合，可以逐段驗證。
3. **`config.json`、`coordinator.json`、`remote.json` 永遠不搬。** 寫進文件，
   免得下一個人「順手統一一下」。

---

## 6. 參考：`stablyai/orca`

**我連得出去，讀到了。** 以下每一條都指出我讀的是哪一頁；我沒有登入，
所以 GitHub 的程式碼搜尋（`/search?q=repo%3A…`）對我回的是登入牆，這一點在第 9 章。

### 6.1 它解決的問題跟我們是不是同一個？——**大部分是**

README 的原話（`github.com/stablyai/orca`，61.4k stars，MIT）：

- 「The AI Orchestrator for 100x builders.」
- 「**Fan one prompt across five agents, each in its own isolated git worktree** —
  compare the results and merge the winner.」
- 「**Monitor and steer your agents from your phone** — get notified when an agent finishes
  and send follow-ups from anywhere.」
- 「Ghostty-class terminals with WebGL rendering, infinite splits, and scrollback that
  survives restarts.」

**平行任務隔離在各自的 git worktree、從手機監看與追送、終端整合**——
這三件事跟 Clawdline 是同一個問題。它的 repo 結構也平行得有點驚人：
`cloud/`（手機 relay）、`skills/`、`skill-guides/`、`skill-stubs/`、
`AGENTS.md`、`CLAUDE.md`、`docs/`、`native/`、`mobile/`。

**不一樣的地方（前提差異，所以有些決定不能抄）：**

| | orca | Clawdline |
| --- | --- | --- |
| 平台 | 跨平台（macOS / Windows / Linux + iOS / Android 原生 app） | macOS 專屬 + PWA |
| 技術棧 | Electron + React 19 + TypeScript + Vite/rolldown | 純 swiftc + 零依賴，`Package.swift` 的註解自己寫「This package exists so an editor can understand the code. It is not how the app is built.」 |
| 終端 | 自己在 app 裡畫（`@xterm/xterm` + WebGL addon + `node-pty`） | **不畫終端。它驅動你已經在用的 iTerm2 / tmux** |
| 相依數 | `package.json` 有 24 個 dependencies、100+ devDependencies | 0 |

**最大的前提差異就是最後一列。** orca 的終端是它自己的（`node-pty` 開的 pty，
xterm.js 畫的），所以「app 是不是唯一入口」對它不是問題。
Clawdline 的整個存在理由是**不搶你的鍵盤**（`Tmux.swift:733`）。
**所以 orca 在視窗與焦點上的任何決定，我們都不能直接抄。**

### 6.2 它的哪一個決定我們可以直接抄？——**兩個，而其中一個是反過來的**

#### (a) 可以抄：UI 是網頁、殼是原生、終端是另一塊——這正是第 3.4 節那個形狀

orca 的架構是 Electron：`src/renderer`（React）住在一個原生殼裡，
`src/main` 是 Node 側，`src/preload` 是橋。它的視窗裡同時有
**嵌入的 Chromium 瀏覽器**（Design Mode：「Click any UI element in a real Chromium window
to send its HTML, CSS, and a cropped screenshot straight into your agent's prompt」）
與**終端**。

> **一個 61.4k star 的產品，在同一個問題上，選的是「網頁 UI 裝在原生殼裡」而不是原生重寫。**
> 這不是證明——Electron 是它的整個技術選擇，它沒有「原生重寫」這個選項——
> 但它至少說明「網頁 UI + 原生殼 + 終端在同一個視窗」是一個做得起來的形狀，
> 而不是一個要自己發明的東西。

#### (b) **反過來抄：它的自己狀態沒有進 SQL，而它有 SQL**

這是我讀完最意外、也最有用的一條。

`src/main/` 底下有 **`sqlite/`** 和 **`persistence/`** 兩個目錄，它們不是同一件事：

**`src/main/sqlite/` 只有四個檔**：`sync-database.ts`、`sync-database.test.ts`、
`sqlite-read-failure.ts`、`sqlite-read-failure.test.ts`。
`sync-database.ts` 是一層薄包裝，用 Node 內建的 `node:sqlite`
（`import type { DatabaseSync, StatementSync, SQLInputValue } from 'node:sqlite'`），
只轉手 `readOnly` 與 `timeout` 兩個選項，**沒有設 WAL、沒有設 busy_timeout、
沒有任何 pragma**。而 `sqlite-read-failure.ts` 的內容是把**唯讀開檔失敗**分成三類
（`'contended'` / `'wal-index-unavailable'` / `'unreadable'`），它自己的註解說：

> 「Contention is transient — retry later, never persist it as a permanent failure.
> A missing wal-index is structural — retrying forever cannot fix it.」

還提到 WSL 9p 網路磁碟上「an extra sync stat is exactly the hang the transcript gate
exists to prevent」。

**這整組東西的形狀是「唯讀地去讀別人的資料庫」，不是「這是我的狀態存放處」。**
（各家 agent CLI 把歷史存在 sqlite 裡，`src/main/` 底下有 `claude-usage`、`codex-usage`、
`opencode-usage` 這些目錄。）

**orca 自己的狀態在 `src/main/persistence/loading-store/`，53 個檔，而它是檔案：**

- `store.ts`、`primary-state-writes.ts`、`state-serialization-secret-handling.ts`
- `write-scheduling.ts` —— **debounce + 上限等待**：
  `SAVE_DEBOUNCE_MS = 1_000`、`SAVE_MAX_WAIT_MS = 5_000`
- `write-flush-barriers.ts` —— 關機時的 flush barrier，
  它的註解：「once the quit flush has snapshotted, a newly debounced write would fire during
  teardown with nothing awaiting it, and the process can exit mid-rename.
  The quit flush is the last write by construction.」
- `backup-recovery-rotation.ts` —— 備份輪替
- `loaded-cohort-migrations.ts`、`loaded-state-adaptation.ts`、`normalize-loaded-*.ts`
  —— 載入時的遷移與正規化
- `secret-sentinel-substitution.ts` —— 機密不落盤的哨兵替換

以及 `package.json` 裡的 `proper-lockfile@4.1.2` —— 跨行程檔案鎖。

> **這對第 5 章是一個直接的反例，而且是我認為最有價值的一條：**
> 最接近的同類產品，同樣的 worktree 平行問題，**沒有把自己的狀態搬進 SQL**。
> 它做的是把「JSON 檔案存放」做完整：debounce、合併寫入、關機 flush barrier、
> 備份輪替、載入時遷移、跨行程鎖。
>
> **而那份清單，正好就是 Clawdline 的 `Orchestrator.save()` 缺的全部東西**
> （53 個呼叫點、沒有 debounce、寫兩次、沒有備份輪替、沒有跨行程鎖）。

**這不推翻第 5 章的建議，但它改變了順序。** 我在 5.6 建議的三段裡，
第 1 段（修重複寫入 + debounce）原本是「順手做的小事」；
有了這條對照，它變成**應該先做、而且做完要重新評估第 2 段還需不需要**。
因為 orca 的存在證明：一個比 Clawdline 大的同類系統，靠把檔案存放做完整就撐住了。

**不能抄的地方**：orca 是 Electron，它的狀態存放跑在 Node 的事件迴圈上，
沒有 Clawdline 那個「主執行緒同時要畫 AppKit、跑 SessionWatch、答 HTTP」的約束。
`busy_timeout=5000` 對它不是問題，對我們是。所以它「不設 pragma」這個決定我們不能抄
——`UsageLedger.swift:856`–858 設了，那是對的。

---

## 7. 切法：第一刀，以及後面依賴它的

### 第一刀：**網頁端的頁面路由**（不碰 Swift，不碰 Mac App）

**做什麼**（一句話）：把 `input/route.js` 從「只認 `#session=`」擴成
「`#page=<name>[&session=<id>]`」，在 `<main>` 上加 `data-page`，
把 Usage 的 `open()`/`close()`（`view/usage.js:671`、`681`）與
Settings 的 `toggle()`（`input/settings.js:46`）改成寫 hash 而不是直接改 `hidden`。

**為什麼它可以獨立落地：**

- **它不需要任何 Swift 改動。** `RemotePage.page(for:)` 只做三件替換
  （`RemotePage.swift:727`–729），不認得 hash。伺服器不需要知道有頁面這件事。
- **它不改任何 DOM。** Usage 那 146 個元素原地不動；Settings 那個 overlay 原地不動。
- **它不會破壞推播。** `sessionInHash` 的正則（`route.js:25`）用 `[#&]` 當前綴，
  舊 URL `/#session=abc` 與新 URL `#page=x&session=abc` 都解得出來。
  這一點**必須寫成紅過的測試**才算數。
- **它可以在瀏覽器裡驗完。** `tools/web-serve.py` + `?mock=1` 就能跑，不用編譯 Swift、
  不用搶機器鎖。
- **它是純 `.mjs` 可測的。** 路由是一個純函式（hash 進、`{page, session}` 出），
  可以照 `Tests/web-schedules.mjs` 的形狀寫，零依賴。

**後面依賴它的：**

```
第一刀：頁面路由（純 web）
   ├─ 第二刀：側邊欄（純 web）—— 需要路由來決定選取狀態
   │     └─ 第三刀：Usage / Settings 真的變成頁面（純 web）
   ├─ 第四刀：Project 頁的資料路由（Swift）
   │     ├─ 新路由 GET /v1/projects/:id/features
   │     ├─ 需要 UsageLedger 的 join（第 4.4 節已證明跑得起來）
   │     └─ 需要動 RemoteServer.swift 的行數天花板（只剩 5 行餘裕）
   │           └─ 第五刀：Project 頁的畫面（純 web）—— 依賴第四刀
   └─ 第六刀：Mac App 的主視窗（Swift，B′）
         ├─ Request.Source 加第三個 case
         ├─ WKURLSchemeHandler
         ├─ 事件橋（SSE 或 WKScriptMessageHandler）
         └─ 依賴第一到第三刀已經落地——否則嵌進去的還是今天的形狀
```

**注意第六刀在最後，不是第一。** 因為 B′ 嵌的是網頁，所以網頁先長成那個形狀，
Mac App 才有東西可嵌。**這也是這個切法最大的好處：前五刀每一刀都對手機使用者有價值，
即使第六刀永遠不做。**

`Orchestrator.save()` 的重複寫入與 debounce（第 5.6 節第 1 段）跟這條線完全獨立，
可以任何時候插隊。

---

## 8. 要使用者拍板的事

**技術待辦與使用者決定是兩張清單。** 這一張是使用者的，每一題附代價與我的建議。
建議用選項介面一次問一題。

### Q1. Mac App 的主視窗，裡面裝什麼？

| 選項 | 代價 | |
| --- | --- | --- |
| **A. 原生重寫** | 重寫 21,848 行 JS + 4,066 行 CSS 的等價物（現有全部原生 UI 才 13,681 行）；作廢 41 個 `.mjs` 測試套件；**永久兩套實作**（Mac 原生 / 手機網頁） | |
| **B. 嵌入網頁，指向 127.0.0.1** | 幾百行。但要求使用者先打開 `remote`（監聽 socket）與 `remoteWrite`（允許遠端寫入）兩個刻意預設關閉的安全開關；server 沒起來時視窗是錯誤頁 | |
| **B′. 嵌入網頁，in-process 不開 socket** | 約 400–800 行 + 五族繞過 `route()` 的路由各接一條線；`Request.Source` 加一個 case（要動 `RemoteServer.swift` 的行數天花板）；SSE 能不能走自訂 scheme **我沒驗到** | **← 我建議這個** |

### Q2. 主視窗的側邊欄裡，「設定」是哪一個設定？

| 選項 | 代價 | |
| --- | --- | --- |
| A. 網頁那份（3 個 block） | 一致，零工作。但 Mac 使用者打開會覺得空 | |
| B. 把 Mac 的 6 個 pane 搬進網頁 | 要重寫 11 個自訂 `NSView` 子類的網頁版。大 | |
| **C. 側邊欄可以放原生頁面，「設定」是其中之一** | `Settings.swift` 3,582 行一行不動。代價是側邊欄一開始就要支援「有些頁是網頁、有些頁是原生」 | **← 我建議這個** |

### Q3. Project 頁的「worktree」，指哪一個？兩個來源交集只有 53

**先看數字**（第 4.5 節有完整量測）：磁碟上 37、git 分支 85、ledger 有 Feature 的 85，
**而兩個 85 的交集只有 53**，各有 32 個對方沒有；只在 git 那 32 個裡，**有 15 條已經併進 `main`**。

| 選項 | 頁面上會有幾列 | 代價 |
| --- | --- | --- |
| A. 只用 ledger（有 accepted Feature 的） | 85 tasks / **19 個 Feature** | 最乾淨、有名稱有產出。**漏掉 15 條已落地但沒拿到 Feature 歸屬的工作** |
| B. 只用 git 分支 | 85 列 | 永遠保存，但**沒有 Feature 名稱、沒有標題**，只有一串 UUID。基本上不可讀 |
| **C. ledger 為主，git 補「已落地但無歸屬」的一區** | 19 個 Feature + 一區 15 條標為「已落地，歸屬未定」 | 多一次 git 掃描與一個 `merge-base` 判斷。**它同時把歸屬的缺口變成畫面上看得見的東西** ← 我建議這個 |
| D. 只列磁碟上還在的（37） | 37 列，大多是死掉的任務 | 最便宜，最沒用 |

**這一題不是工程可以自己決定的**，因為四個答案畫出來是四種不同的產品。

### Q4. ⌘K / ⌘J / ⌘I 歸誰？

三個鍵在兩邊都有主人，而且**沒有一個是同義的**：

| 鍵 | 面板（`Panel.swift`） | 網頁（`keys.js`） |
| --- | --- | --- |
| ⌘K | 開/關目標清單（`:714`） | 焦點進 session 列表並選一列（`:42`–47） |
| ⌘J | 開/關輸出窗格（`:717`） | 開關明細欄；手機上切 list↔detail（`:48`–53） |
| ⌘I | 無 | 開 session 的 info sheet（`:55`–61） |

**macOS 的主選單 key equivalent 會贏過兩者**，所以第三種答案不是妥協，是最大的改動。

| 選項 | 代價 |
| --- | --- |
| A. 主視窗開著時歸網頁，面板開著時歸面板 | 使用者要記兩套。但這是 macOS 的常態（不同視窗不同鍵），而且**零改動** |
| **B. 主視窗用 ⌘1/⌘2/⌘3 切頁（標準 macOS 慣例），三個鍵各自留給原本的持有者** | 側邊欄要另外找快捷鍵——但 ⌘1/⌘2/⌘3 本來就是 macOS 切分頁的慣例，比 ⌘K 更對 ← 我微幅偏好這個，因為它不動面板也不動網頁 |
| C. 三個鍵一律進主選單，兩邊都改 | 最一致，最多改動，而且會**同時**改掉手機上的行為（`keys.js` 是共用的） |

### Q5. 狀態層：現在要走多遠？

| 選項 | 得到 | 代價 |
| --- | --- | --- |
| **A. 只修現況**：`save()` 的重複寫入、debounce、備份輪替 | orca 靠這個撐住了 61.4k star 的同類產品 | 小。**但那個 1350 還在，Project 頁的歷史還是 30 天** ← 建議先做這個 |
| B. A + 把 `tasks` 集合搬進 SQLite | 1350 可以拿掉；landing-queue 變一句 SQL；Project 頁的 join 完整 | 一次遷移 + 相容性契約（`OrchestratorStore.swift:15` 的警告）+ 測試 |
| C. 全部搬 | —— | **我不建議。** `config.json` 是人會手改的，`coordinator.json` 是單筆 CAS，搬了都是退步 |

### Q6. 這一輪的範圍到哪裡？

| 選項 | |
| --- | --- |
| A. 只做網頁端（第一到第三刀）——側邊欄、Usage 頁、Settings 頁 | 手機立刻受益，Mac App 不動 |
| **B. A + Project 頁（第四、五刀）** | 多一個 Swift 路由 + 一次 join ← 建議 |
| C. B + Mac App 主視窗（第六刀） | 一輪做完全部。大，而且第六刀依賴前五刀 |

---

## 9. 我沒有驗到的東西，以及要驗它需要什麼

**這一節是這份文件裡最重要的一節，因為它是這份文件唯一會出錯的地方。**

| # | 沒驗到什麼 | 為什麼沒驗 | 要驗它需要什麼 |
| --- | --- | --- | --- |
| 1 | **`EventSource` 能不能在 `WKWebView` 的自訂 scheme 上運作** | 需要編譯一個帶 WebKit 的 app，而 `./build.sh` 被交辦禁止 | 一個 30 行的獨立 Swift 檔：`WKURLSchemeHandler` 回一個 `text/event-stream`，頁面 `new EventSource("myscheme://events")`，看 `onmessage` 有沒有進來。**這是整個 B′ 最大的單一風險**，應該在第六刀之前先用這個實驗回答 |
| 2 | **ATS 會不會擋 `http://127.0.0.1`** | 同上 | 同一個實驗 app 載入 `http://127.0.0.1:7717/`。這只影響候選 B，而我不建議 B，所以優先度低 |
| 3 | **面板的輸入卡片能不能離開 `NSPanel`** | 我讀到 `Controller.swift:310`–340 建 container/cardHost/card 時沒有看到對宿主的假設，但 4,092 行我沒有逐行讀 | `grep -n "panel\." Sources/Controller.swift` 逐個看哪些是版面、哪些是視窗行為。純閱讀，不用編譯 |
| 4 | **`NSApp.activate(ignoringOtherApps: true)` 加上一個常駐主視窗會不會把主視窗拉到終端前面** | 需要跑起來的 app | 建一個 `.titled` 視窗，開著，按 ⌥Space，看終端前面是什麼。**這是「保留終端跳出來」這個需求的驗收條件本身**，必須在第六刀落地前驗 |
| ~~5~~ | ~~85 個分支與 85 個 task 是不是同一批~~ | **已驗，見第 4.5 節。** 用 `comm` 做集合差（不是 `diff`）：交集 53，各有 32 個對方沒有；只在 git 那 32 個裡 15 條已併入 `main` | —— |
| 6 | **orca 的 `SyncDatabase` 到底被誰用** | GitHub 程式碼搜尋要登入（我試了 `/search?q=repo%3Astablyai%2Forca+sqlite`，回的是登入牆） | `git clone` 之後 `grep -rn "SyncDatabase" src/`。我的推論（它是用來唯讀別人的 db）建立在 `sqlite-read-failure.ts` 的內容與 `src/main/` 底下有 `claude-usage`/`codex-usage`/`opencode-usage` 三個目錄——**這是推論，不是證據**。第 6.2(b) 的另一半（orca 自己的狀態是檔案）是**直接證據**，不受這條影響 |
| 7 | **Swift 的 `JSONSerialization` 序列化 1.77 MB 要多久** | 我量的是 Python 的 `json.dumps`（5.0 ms）當代理 | 一個獨立的 `swift` script，讀那個檔、`JSONSerialization.data(…prettyPrinted, .sortedKeys)`、計時。不需要編譯整個 app |
| ~~8~~ | ~~`Config.remote` 今天是開還是關~~ | **已驗**：`remote = true`、`remote_port = 7717`、`remote_write = true`。見 3.1 末段——這反而是候選 B 的陷阱 | —— |
| 9 | **那 15 條「已落地但無 Feature 歸屬」是為什麼沒歸屬** | 我只數了它們，沒有逐個分類（30 天以外？`no_unambiguous_accepted_head`？proposal-only？） | 對每一個 task_id 查 `usage_attribution_events` 的 `decision` 與 `usage_intervals` 的 `started_at`。純 SQL，五分鐘。**它會決定 Q3 選項 C 那一區該叫什麼名字** |

**另外兩個我知道但沒有量的東西：**

- 我沒有量「把 Usage 改成頁面」的實際改動行數。我說它「非常小」的依據是
  `view/usage.js:671`–687 那 17 行，以及「DOM 一個都不用搬」——但把 `open()`/`close()`
  改成走路由會牽動它的鍵盤處理（`view/usage.js:722`–725 的 Escape）與焦點管理
  （`:677`、`:686`）。那是十幾行，不是零行。
- 我沒有讀 `NotchIsland.swift`（1,145 行）。它是「終端跳出來」那一族的第五個消費者
  （`RemoteServer.swift:53`–57 的註解列了四個消費者，HTTP 是第五個），
  一個常駐主視窗會不會是第六個、以及那對 `SessionWatch` 的讀取頻率有沒有影響，
  我沒有查。

---

## 10. 我讀過的東西

**Swift 側**：`RemotePage.swift`（1,027）、`RemoteServer.swift`（5,565，重點在
`:449`–530 監聽、`:605`–660 路由前攔截、`:752` `route()`、`:830`–890 認證、
`:3346`–3386 `writeGate`、`:5386` `Request.Source`）、`RemoteAuth.swift`（`:100`、`:389`）、
`Config.swift`（`:293`、`:355`、`:498`、`:513`、`:526`）、`main.swift`（全）、
`Controller.swift`（`:300`–340、`:605`–760）、`Panel.swift`（`:1`–60、`:695`–760）、
`Settings.swift`（`:384`–430、`:1835`–1870）、`Onboarding.swift`（`:814`–960）、
`Strings.swift`（`:1`–50）、`Targets.swift`（symbols + `:1020`）、`Tmux.swift`（symbols + `:721`–780）、
`Orchestrator.swift`（`:10173`–10245）、`OrchestratorStore.swift`（`:1`–40）、
`OrchestratorDraft.swift`（worktree 段）、`UsageLedger.swift`（`:809`–930、`:990`–1000）、
`UsageFeatureAttribution.swift`（`:1`–80）、`WindowChrome.swift`（`:1`–50）、`HotKey.swift`（`:1`–40）。

**網頁側**：`index.html`（全 1,042 行）、`main.js`（全 335 行）、`input/route.js`（全）、
`input/settings.js`、`input/keys.js`、`view/usage.js`（`:539`–728）、`app/css/shell.css`。

**建置與測試**：`build.sh`（`:760`–840）、`Package.swift`、`test.sh`（`:474`–640）、
`tools/check-architecture-boundaries.sh`（`:184`–190、`:488`）、`tools/check-web-strings.py`、
`tools/check-web-ids.py`、`tools/build-web-app.py`（`:1`–40）、`Resources/Clawdline.entitlements`、
`Tests/web-schedules.mjs`。

**文件**：`docs/api.md`（`:435`、`:842`–940、`:3375`–3420、`:3581`–3700）、
`docs/remote.md`（`:1`–40）、`docs/interface.md`（`:1`–80）、`docs/web-split.md`（`:1`–100，
這份文件的體例是照它寫的）、`docs/backlog.yaml`（grep）。

**機器實測**：`git worktree list`、`git branch --list 'clawdline/task/*'`、
`~/Library/Application Support/Clawdline/worktrees/` 的目錄計數、
`~/.config/clawdline/*.json` 的大小與 `config.json` / `coordinator.json` 的內容、
`usage.sqlite3` 的**唯讀** SQL 查詢（`file:…?mode=ro`）、
ledger 推出的分支名與 `git branch` 的 `comm` 集合差、
那些分支對 `main` 的 `git merge-base --is-ancestor`、
`orchestrator.json` 的序列化/寫入計時（寫到 `/tmp` 的暫存目錄，沒有碰真的 store）。
**沒有執行 `./build.sh` 或 `./test.sh`，沒有啟動任何編譯器，也沒有寫入 repo 或
`~/.config/clawdline/` 底下任何檔案。**

**外部**：`github.com/stablyai/orca` 的 README、repo 根目錄、`src/`、`src/main/`、
`src/main/sqlite/`、`src/main/persistence/`、`src/main/persistence/loading-store/` 的列表，
以及 `package.json`、`src/main/sqlite/sync-database.ts`、`src/main/sqlite/sqlite-read-failure.ts`、
`src/main/persistence/loading-store/write-scheduling.ts` 的內容。

---

## 附錄 A：一句話總結，給只讀這一段的人

1. **不要原生重寫。** 那要重寫的量比現有全部原生 UI 還大，而且會永久製造兩套實作。
2. **也不要把 web view 指向 127.0.0.1。** 那要求使用者打開兩個刻意關著的安全開關。
3. **用 in-process 的自訂 scheme（B′），而且只嵌「頁面」那一塊**——
   側邊欄與指令輸入用原生，中間那一大塊是今天的網頁。
4. **側邊欄的第一刀在網頁端，不在 Mac App。** 前五刀每一刀都對手機使用者有價值，
   即使 Mac 主視窗永遠不做。
5. **Project 頁的資料現在就在**：usage ledger 裡有 85 個完成過 Feature 的隔離任務、
   構成 19 個 Feature，那句 join 我已經跑過了。**但沒有任何單一來源答得出這個問題**——
   ledger 推出的 85 個分支名與 git 實際的 85 個分支只重疊 53 個，
   而只在 git 那 32 個裡有 15 條已經併進 `main`。要顯示哪一個集合是使用者要拍板的（Q3）。
6. **SQL 先不要搬。** 最接近的同類產品（orca，61.4k star，同樣的 worktree 平行問題）
   沒有把自己的狀態搬進 SQL——它把檔案存放做完整了。
   **而那份「做完整」的清單，正好就是 `Orchestrator.save()` 缺的全部**：
   debounce、合併寫入、關機 flush barrier、備份輪替、跨行程鎖，
   以及不要**把同一份 1.77 MB 原子寫入兩次**（`Orchestrator.swift:10228` 與 `:10235`）。
7. **但要知道 SQL 與 UI 是同一個決定的兩半**：Project 頁能顯示多久的歷史，
   是那個 1350 決定的，不是 UI 決定的。
