# 跨平台：原生殼留下什麼，web 拿走什麼

> 規則（使用者 2026-09-18）：「除了少數 Mac 才有的特色，應該要可以跨平台」。
> 畫面一律做在 React console 裡，原生殼只留「非原生做不到」的那幾件事：視窗、全域熱鍵、
> 選終端機分頁、剪貼簿、通知、瀏海。之後 Linux 與 Windows 換一個 webview 殼就能用同一份畫面。

## 瀏海島（一直看得到的等待數與一隻角色）

### 這個功能的本質不是「瀏海」

瀏海只是道具。macOS 的鏡頭挖孔本來就是純黑、本來就一直在那裡，所以把一個黑色視窗貼著它畫，
看起來就像挖孔長出了兩隻耳朵——左耳住著角色，右耳是狀態。**這個把戲只有 Mac 有。**

拿掉道具之後剩下的才是功能，有三件：

1. **一個不用切換視窗就看得到的等待數**——有幾個 session 在等你回答，或有幾個在跑。
2. **一隻角色**，它睡覺、起床、忙的時候動得比較快、跑完了會跳舞。這是情緒，不是資料；
   它說的每一件事選單列的 ✳ 都已經說過了（舊版原始碼自己就這樣寫：同一份讀數換一套戲服）。
3. **兩個按得到的目標**：角色＝這個 app（打開 console），數字＝那個 session（選起它的終端機分頁）。

所以移植到別的平台時要問的不是「Linux 的瀏海在哪裡」，而是**「這台機器上，哪裡是一直看得到、
放得下一張小圖和一個數字、而且按得下去的地方」**。三個平台的答案都不同，而且都不是視窗。

### Linux

| 做法 | 能做到什麼 | 限制 |
|---|---|---|
| **系統匣（StatusNotifierItem／AppIndicator，走 D-Bus）** | 一張自繪的圖示（角色可以畫進去）、tooltip、左右鍵選單；換圖就是換一張 pixmap，所以低張數的動畫做得出來 | GNOME 從 3.26 起沒有內建托盤，要裝 AppIndicator 擴充套件才看得到；KDE Plasma 原生支援。圖示大小約 22–24 px，角色只能是剪影等級 |
| **置頂小浮窗（layer-shell／DOCK 視窗）** | 最接近舊版：真的有一塊常駐的畫面，角色可以動、數字可以是字 | **X11 可以自己決定位置，Wayland 不行**——Wayland 客戶端沒有全域座標，要靠 `wlr-layer-shell` 才能把 surface 釘在螢幕邊緣。sway、Hyprland、KDE 有；**GNOME 的 Mutter 沒有實作 layer-shell**，那裡只有寫 gnome-shell 擴充套件一條路 |
| **工作列徽章（`com.canonical.Unity.LauncherEntry`，D-Bus）** | 啟動器圖示上一個數字徽章，KDE 與 Ubuntu Dock 都吃 | 只有數字，沒有角色；要有 `.desktop` 檔才認得出是誰；GNOME 原生同樣不吃 |

**建議：系統匣為主，浮窗為選配。** 理由是「一直看得到」這個條件：托盤在哪個桌面環境都**可能**沒有，
但它不需要決定自己的位置，所以不會在 Wayland 上直接做不出來；浮窗反過來，做得出來的時候最像舊版，
但在最普及的 GNOME Wayland 上是沒有解的，把它當主力等於在最大的一塊使用者上交白卷。
工作列徽章不當主力：它畫不出角色，本質三件事只做到一件。

實作順序：托盤圖示（角色縮成剪影＋等待數畫在右下角）→ tooltip 用同一組字串 → 左鍵開 console、
右鍵選單列出等待中的 session。**在 sway／Hyprland／KDE 上另外提供 layer-shell 版的浮窗**，
那裡才把角色畫成會動的。動畫張數要降：托盤是換 pixmap，60 fps 會變成 D-Bus 洪水，
狀態改變時換一次、跑完了放一段幾張的短動作就夠。

### Windows

| 做法 | 能做到什麼 | 限制 |
|---|---|---|
| **通知區域圖示（`Shell_NotifyIcon`）** | 每個 Windows 都有；圖示可以隨時換成新的 HICON，所以角色與數字都畫得進去；tooltip 內建 | Windows 10／11 預設把**新的托盤圖示收進溢位選單**，使用者要自己拖出來才會一直看得到。16×16（依 DPI 放大），一樣只能是剪影 |
| **工作列徽章／覆疊圖示（`ITaskbarList3::SetOverlayIcon`）** | 在 app 的工作列按鈕上疊一個小圖，數字可以畫進去 | **要有工作列按鈕才有地方疊**——視窗關起來就沒有了，而這個功能的重點正是視窗關著的時候。MSIX 打包後另有 `BadgeUpdateManager` 的數字徽章，但要有套件識別 |
| **置頂無邊框小視窗（`WS_EX_TOPMOST`＋`WS_EX_NOACTIVATE`＋`WS_EX_TOOLWINDOW`）** | Windows **允許** app 自己決定螢幕座標，所以舊版那種「釘在螢幕上緣的一小塊」在這裡做得出來，角色可以全速動 | 它會蓋到別人的東西；要自己處理多螢幕與 DPI 變更；要靠 `SHQueryUserNotificationState` 在全螢幕簡報與遊戲時收起來 |

**建議：通知區域圖示為主，置頂小視窗為選配。** 托盤被收進溢位選單是個設定問題，
使用者拖一次就解決；工作列徽章則是結構問題，視窗關著就不存在，直接不符合這個功能的前提。
置頂小視窗是三個平台裡唯一能把舊版原樣搬過去的做法，所以留著當選配——但不當預設，
因為「預設就蓋住別人畫面的一塊常駐視窗」是要使用者同意的事，不是安裝完就該有的事。

### 這一版只做 Mac，介面接在哪裡

`shell/darwin/NotchIsland.swift` 一個檔，`main.swift` 只多四行掛載。檔案裡分成三層，
**中間那層是跨平台的，兩頭不是**：

| 層 | 內容 | 換平台時 |
|---|---|---|
| 讀數 | `attach(to:open:)` 注入的 bridge script、`shellIsland` 訊息、`IslandSession` | **照抄**。WebKitGTK 的 API 就是 `window.webkit.messageHandlers.<name>.postMessage`，同一段 JS 不用改；WebView2 是 `window.chrome.webview.postMessage`，加一層三行的 shim |
| 狀態機 | `IslandMode`、`refresh()`（等待優先於慶祝、慶祝優先於進度；3.4 秒的慶祝；`pendingFinished` 只消費一次）、`ears(for:bar:)` 的寬度規則 | **照抄**，它只碰數字與字串 |
| 畫面與互動 | `Notch.rect/path/screen`、`IslandPanel`（覆寫 `constrainFrameRect` 才畫得到選單列上面）、`IslandView`、`IslandMascotView` | **整層換掉**。托盤版只需要「把 `IslandView` 畫的東西畫成一張 pixmap」，吉祥物的 pack 格式與取樣（`IslandMascotPack`）可以照抄 |

兩個動作是共通的，兩邊都已經是本機 daemon 的路由，不是平台 API：

- 角色被按 → 打開 console 視窗（殼自己的事）。
- 數字被按 → `POST /v1/sessions/{id}/focus`，和 console 的「在 Mac 上顯示」同一條路由，
  所以兩邊不可能選到不同的東西。Linux／Windows 的殼照送同一個請求，要不要真的把終端機叫起來
  由 daemon 那端決定。

**還沒決定、留給第二個平台到場時再決定的一件事**：狀態機要不要搬進 Go，讓 daemon 直接發一個
`island` 事件（誰在等、幾個在跑、誰剛跑完），三個殼都只負責畫與按。現在不搬，因為只有一個殼，
搬了只是把一份程式碼換個語言；等第二個殼出現、兩邊的 3.4 秒開始各寫一次的時候，就該搬。
