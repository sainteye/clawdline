# 跨平台：哪些行為在 Linux／Windows 沒有對應

> 使用者 2026-09-18 的要求：「除了少數 Mac 才有的特色，應該要可以跨平台」。做法是畫面一律做在
> web，原生殼只留「非原生做不到」的部分（介面見 `docs/shell-bridge.md`）。
>
> 這份文件記的是**剩下的那些做不到**：每一項寫「限制是什麼、建議怎麼呈現」。**做不到不可以默默
> 失敗**——沒有對應的東西要回一個具名的「不支援」，或是在畫面上明講，不可以看起來像壞掉。
>
> 每個 child 只加自己那一節。

---

## 輸入條（快速面板）

macOS 的這一半在 `shell/darwin/Bar.swift`，web 的那一半在 `web/console/src/bar/`。

### 1. 全域熱鍵（⌥Space）

**限制是什麼。** macOS 用 Carbon 的 `RegisterEventHotKey`（`shell/darwin/HotKey.swift`），不需要
輔助使用權限，整個系統一個組合只有一個擁有者。

- **Linux**：X11 下是 `XGrabKey`，抓得到；**Wayland 抓不到**。Wayland 刻意不讓一般程式監看別的
  視窗的鍵盤，合法的路是桌面環境自己的設定（GNOME 的 `org.gnome.settings-daemon.plugins.
  media-keys.custom-keybindings`、KDE 的 `kglobalaccel`），或是 `xdg-desktop-portal` 的
  GlobalShortcuts portal（相對新，不是每個桌面都有）。
- **Windows**：`RegisterHotKey` 抓得到，但被 UIPI 擋在提權視窗之外，而且 `VK_SPACE` 加 `MOD_ALT`
  會跟系統選單打架。

**建議怎麼呈現。** 殼在啟動時就知道自己抓不抓得到：抓得到就照舊；抓不到的時候**不要靜靜地不動**，
而是（a）寫進 log 說明是哪一種抓不到，（b）在設定頁的快速鍵那一列說「這個桌面要由系統設定，指令是
`clawdline-next://toggle`」，並且（c）一定要留一條不靠熱鍵也打得開的路——macOS 是選單列的
「打開輸入框」，Linux／Windows 就是 tray 選單那一項加上那個 URL scheme。`main.swift` 現在
`applyConfiguredHotKey(alertOnFailure:)` 就是這個形狀的 macOS 版（註冊不起來會說一句「多半是被別的
軟體佔走了」，而不是假裝註冊好了）。

### 2. 視窗層級與「不搶焦點」

**限制是什麼。** macOS 的 `NSPanel` 有 `.nonactivatingPanel`：視窗拿到鍵盤，但**應用程式沒有被
啟動**，所以底下那個 app 看起來還是「前景」。這是整個輸入條成立的前提。

- **Linux／X11**：`_NET_WM_WINDOW_TYPE_DOCK` 或 `override-redirect` 可以做到置頂不被管理，但焦點
  的模型不一樣，多半得自己 `XSetInputFocus`，而且各家 WM 行為不同。
- **Linux／Wayland**：要靠 `wlr-layer-shell`（wlroots 系：sway、Hyprland 有）。**GNOME 沒有**
  layer-shell，一般程式做不出「浮在所有東西上面的面板」。
- **Windows**：`WS_EX_TOPMOST` 加 `WS_EX_NOACTIVATE` 很接近，但 `NOACTIVATE` 的視窗要拿到鍵盤得繞
  `AttachThreadInput`，而且會踩到前景視窗鎖（`SetForegroundWindow` 的限制）。

**建議怎麼呈現。** 做不到「不搶焦點」的平台，就做成**會搶焦點的一般視窗**，並且在收起來的時候把
焦點還回去（`Bar.swift` 的 `hide(returnFocus:)` 已經是這個行為）。不要為了模擬而用 always-on-top
的無邊框視窗卻不處理焦點——那會變成打字打到一半鍵盤跑掉。GNOME／Wayland 上老實一點：開一般視窗，
並在設定頁說明這個桌面沒有浮動面板。

### 3. 毛玻璃

**限制是什麼。** 卡片後面的模糊是**桌面的模糊**，網頁看不到桌面，所以那一層一定在殼裡
（macOS：`NSVisualEffectView`）。Windows 有 `DwmSetWindowAttribute` 的 Mica／Acrylic（Win11）；
Linux 要靠合成器的 blur-behind（KDE 有 `KWindowEffects`，GNOME 沒有）。

**建議怎麼呈現。** 沒有毛玻璃的時候**什麼都不用做**：`bar.css` 的 scrim 是一層 55% 的深色，殼後面
是黑的話，畫出來就是一張比較實的卡片，不會破版。這是唯一一個「退化得剛好」的項目。

### 4. 跟著終端機出現與收起

**限制是什麼。** macOS 用 `NSWorkspace.didActivateApplicationNotification` 知道「現在前景是哪個
app」，而且拿得到 bundle id（`com.googlecode.iterm2`）。

- **Linux／X11**：`_NET_ACTIVE_WINDOW` 加 `WM_CLASS` 做得到。**Wayland 拿不到**——沒有一般程式讀
  得到「現在誰在前景」的 API，這是刻意的。
- **Windows**：`SetWinEventHook(EVENT_SYSTEM_FOREGROUND)` 加上 process 的 image name 做得到。

**建議怎麼呈現。** 拿不到前景 app 的平台，就**把這個功能整個關掉**，不要用「視窗失去焦點」去猜
（失焦包含切到瀏覽器、切到 Slack，全部重新出現會非常吵）。設定頁那一列（`settingsReopen`
「輸入條跟著終端機出現和收起」）在這種平台上要 disabled 並說明原因，而不是開著卻沒作用。

### 5. 把選到的 session 在終端機裡選起來（Reveal／follow）

**這一項在 macOS 上也還沒打開，原因不是平台。** 舊版 `Controller.follow(_:)` 呼叫的是
`Targets.reveal(target, activate: false)`，註解寫得很清楚：「**Without activating.** Bringing
iTerm2 forward on every Tab press would hand it the keyboard, which is the one thing this whole
application exists to avoid doing.」

新 daemon 的 `POST /v1/sessions/<id>/focus`（`internal/app/focus.go`）呼叫的是
`h.Reveal(ctx, s, true)`——**一律 activate**，而且路由沒有參數可以要它不要。在輸入條裡按 ⇥ 就會把
終端機叫到前面，鍵盤立刻從輸入框跑掉，輸入條自己也會因為失焦而收起來。

所以：web 那一半（`Bar.tsx` 的 `follow`）已經接好，但由殼送過來的 `clawdline-bar-state.follow`
**一律是 false**。要打開，需要 `/focus` 多一個 `activate` 欄位（預設 true，維持相容），
`app.Actions.Focus` 把它傳進 `Reveal`；那是一行的改動，但會動到別的 child 剛落地的檔案，所以留給
root 或下一輪。

**跨平台的部分**：tmux 的 `select-pane`／`select-window` 三個平台都有（Windows 在 WSL 裡）；
iTerm2 只有 macOS；Windows Terminal 沒有可以選分頁的自動化介面。`internal/adapters/terminal` 已經
有 `terminal.Unsupported` 這個具名錯誤，沒有對應實作的平台回它，不要默默成功。

### 6. 剪貼簿與編輯鍵

**限制是什麼。** 這個視窗沒有選單列，WKWebView 只有在有人把 `cut:`／`copy:`／`paste:` 送給它的
時候才會動（`Bar.swift` 的 `performKeyEquivalent`，抄 `ConsoleWindow` 的做法）。

- **Linux（WebKitGTK）／Windows（WebView2）**：webview 內建的鍵盤處理本來就會處理 Ctrl+C／V／X／A／Z，
  不需要這一段。

**建議怎麼呈現。** 這是 macOS 要補、別的平台不用補的一項。修飾鍵也不同：web 那一半
（`Bar.tsx` 的 `onKey`）已經寫成 `ev.metaKey || ev.ctrlKey`，所以 ⌘K 與 Ctrl+K 是同一件事，
提示列上的 `⌘K` 字樣是唯一還寫死 macOS 的地方——之後要換成依平台顯示 `⌘`／`Ctrl`，
字詞要去 `Copy+*.swift` 找，不要自己造。

### 7. 送出的歷史紀錄放在哪裡

**限制是什麼。** 舊版把 60 筆歷史寫進 `~/.config/clawdline/config.json`（`Config.shared.history`）。
輸入條現在是一個網頁，網頁把使用者的訊息寫進設定檔不合適（設定頁會把設定檔顯示出來），所以它放在這個
origin 的 `localStorage`（`web/console/src/bar/history.ts`）。

**後果，而且是真的。** 換一個 webview、清掉網站資料、或是用無痕視窗，歷史就沒了；舊版的歷史連重裝
都還在。三個平台都一樣，所以這不是平台差異而是這一版的差異——**寫在這裡是因為它會被誤認成 bug**。
要修的話是 daemon 開一條自己的路由來存，不是讓網頁去寫設定檔。

### 8. 還沒接上的（不是平台限制，是這一輪沒做）

畫面上畫著、但目前不會動的東西，列在這裡以免被當成跨平台問題：

| 項目 | 現況 | 要接的話 |
|---|---|---|
| 麥克風（⌘L `語音`） | 按鈕照畫、`disabled`，`title` 是舊版的 `hintVoice` | `POST /v1/voice` 已經有（第八波 B），錄音那一段在 `legacy/voice-bridge.ts`，但它跟 composer 的 DOM 綁在一起，要拆出來 |
| ⌘J `看輸出` | 提示列上有，按了沒事 | 舊版是 `Controller.refreshOutput` 那一整套輸出面板，需要 `/v1/transcript` 與一個新的面板 |
| ⌘M `換角色`、⌘S `伺服器` | 同上 | 吉祥物這個 app 目前不畫；dev stack 沒有後端路由 |
| ⌘F `全螢幕`、⌘R `反序`、⌘+ `字級` | 同上 | 這三個都只對輸出面板有意義，會跟 ⌘J 一起 |
| 拖拉檔案進卡片 | `bar.css` 有 `[data-drop="on"]` 的邊框規則，沒有人設定它 | 舊版是 `DropTargetView` + `Drop.swift`；跨平台要各自的 drag 型別 |
| 列上的「N 個在背景」 | 不畫 | wire 上沒有這個欄位（舊版讀 Swift app 的 subagent 檔） |
| 列上的協調等待 | 不畫 | 同上，讀的是舊 app 的 orchestrator store |

### 9. 刻意跟舊版不一樣的兩個初始狀態

`Controller.show()` 開起來是 `listMode = .none`、`keysShown = false`：一行輸入框、一列寫著
`⌘/ 快速鍵` 的提示，沒有別的。這一版**開起來清單是開的、快捷鍵是攤開的**——那是使用者給的那張畫面，
也是舊版按 ⌘K 與 ⌘/ 之後的樣子。

理由：舊版的「收起來」狀態還有地方可去（⌘J 會在它上面開輸出面板），這一版沒有面板（見上一節），
清單再收起來就只剩一行字的卡片。兩個鍵都照樣 toggle，要改回去是 `Bar.tsx` 最上面兩個常數。
