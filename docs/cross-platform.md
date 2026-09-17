# 跨平台盤點：每一個功能在 Linux 與 Windows 怎麼做

> 使用者原話（2026-09-17）：「除了少數 Mac 才有的特色，應該要可以跨平台」「你可以重新思考這些
> 東西要怎麼跨平台使用，如果真的不行的，要告訴我限制和討論如何呈現使用」。
>
> **這是決策文件，不是實作。** 這一份的作用是讓你在看完總表之後，知道哪些事在別的作業系統上
> 是「照做就好」、哪些是「做得到但換一種互動」、哪些是「這台真的沒有，要講出來」。

## 0. 這份文件怎麼共同編輯

這份文件由多個 child 分節寫，**骨架與總表由骨架擁有者維護，其他人只動自己的節**。規則三條：

1. **總表（§3）只有一個作者。** 你不要直接改總表的列。你的結論若與總表不同，就在**你自己的節**裡
   寫一行 `**修正總表**：<列名> → <新結論>`，root 合併時統一改。理由：markdown 表格改一列，
   兩個人同時改就是整段衝突，而衝突解到最後總會有人的字被吃掉。
2. **每一節用 anchor 包起來，anchor 內是你的，anchor 外不要碰。**

   ```markdown
   <!-- section:<你的 id> owner:task-xxxxxxxx -->
   ### 4.14 <你的題目>
   …你的內容…
   <!-- /section:<你的 id> -->
   ```

   **新增一節就接在 §4 的尾巴**，節號用下一個沒人用的數字，不要插在別人中間——插入會讓後面
   每一節的號碼都動，等於改了別人的檔案。已經用掉的 anchor id（不要重複）：

   `sessions`、`screen`、`clipboard`、`whisper`、`secrets`、`notifications`、`autostart`、
   `hotkey`、`notch`、`shell`、`filesystem`、`swiftstore`、`fidelity`。

   已知還有別的 task 在動同一個題目：**瀏海島是 task `a0d852b4`**（它認領
   `shell/darwin/NotchIsland.swift` 與 `docs/replica.md`），§4.9 只寫決策骨架，細節以它為準。
3. **狀態標記統一四種**，寫在每一節開頭一行：

   | 標記 | 意思 |
   |---|---|
   | `已實作` | 這個平台上已經有程式在跑這條路 |
   | `可實作` | 做法確定、元件存在，只是還沒寫 |
   | `降級` | 做不到原本那件事，換一種互動；代價要寫出來 |
   | `不支援` | 這台機器沒有這個東西，要具名地講出來 |

**這份文件的所有 Linux／Windows 判斷都是讀原始碼與既有知識得出的，沒有在真的 Linux 或 Windows
機器上量過。** 使用者授權之後會開 AWS 的機器實測，§7 就是那份最小檢查清單。凡是我自己不確定的，
都收在 §8，不要把它們當成已知。

---

## 1. 一句話

**Go daemon 本身幾乎是跨平台的；不跨平台的是它伸出去碰的那五樣東西——終端機、剪貼簿、鍵盤、
秘密儲存、桌面。** 六個平台的執行檔已經可以從一台機器產出（`plan.md` §P5），Linux 那顆也已經在
真的容器裡起得來；剩下的問題全部集中在 adapters 與原生殼。

三個平台上這個產品的形態不一樣，這是**產品層面要先講清楚的第一件事**：

| | macOS | Linux | Windows |
|---|---|---|---|
| **遙控你已經開著的 session** | ✅ tmux ＋ iTerm2 | ✅ tmux（沒 tmux 就只能看見，不能控） | ❌ 原生沒有（要 WSL 或 MSYS2 的 tmux） |
| **自己開、自己養的 session** | ✅ tmux | ✅ tmux／自有 pty | ⚠️ 要 ConPTY，尚未實作 |
| **桌面整合（熱鍵、匣、開機啟動）** | ✅ 原生殼 | ⚠️ X11 完整、Wayland 受限、無桌面則無 | ⚠️ 要寫 WebView2 殼 |
| **console 網頁本身** | ✅ | ✅ | ✅ |

**Windows 使用者只有「自己開的 session」那一半**（`plan.md` §7 已經寫下這句），這份文件把那半句
展開成一張可以照著做的表。

---

## 2. 兩條硬規則先限制了選項

在挑任何做法之前，這個專案有兩條規則會先砍掉一半的候選：

### 2.1 `CGO_ENABLED=0`

一台機器產出六個平台的執行檔（`plan.md` §P5）靠的就是這個。**它砍掉的是所有要連 C 函式庫的路**：

| 想做的事 | 需要 cgo 的做法 | 純 Go 的替代 |
|---|---|---|
| macOS Keychain | Security.framework、`go-keychain` | 子行程 `/usr/bin/security`，或交給 Swift 殼 |
| Linux 剪貼簿 | `golang.design/x/clipboard`（X11 lib） | 子行程 `wl-copy`／`xclip`／`xsel` |
| Linux 全域熱鍵 | Xlib `XGrabKey` | `xgb`（純 Go 的 X11 protocol client）或 D-Bus portal |
| Linux 殼 | WebKitGTK（gotk3） | **不做殼**，用系統瀏覽器 |
| Windows 殼 | `webview/webview_go` | `go-webview2`（純 Go COM） |
| Windows 行程／DPAPI／Job Object | — | `golang.org/x/sys/windows` 全部有 |

**結論：Windows 那一整欄都可以純 Go 做；Linux 的桌面整合要靠子行程與 D-Bus；macOS 的原生面
留在 Swift 殼裡不動。** 這條規則並沒有擋住任何真正要做的事，但它決定了做法。

### 2.2 「不搶你的鍵盤」

`plan.md` §1 的第一條。它在跨平台上有兩個具體後果，而且兩個都是**拒絕一條看似可行的路**：

- **Linux 不用 `TIOCSTI`。** 往別人的 tty 塞字元在技術上做得到，但那正是一個 keylogger 等級的
  能力，而且新一點的核心預設關掉它（`dev.tty.legacy_tiocsti`）。沒有 tmux 就是沒有——
  「看得見、動不了」是誠實的答案，偷塞鍵不是。
- **Linux 不用 `/dev/input` 抓全域熱鍵。** 同一個理由：那要進 `input` 群組，等於可以讀你打的
  每一個字。舊版 `HotKey.swift` 選 Carbon 而不選 `NSEvent` global monitor，寫的就是這個理由
  （「一個開視窗的工具不該能讀你按的每一個鍵」）。Wayland 上沒有正統做法時，正確的答案是降級，
  不是換一條更有權力的路。

---

## 3. 總表

難度：**S** ＝ 半天以內；**M** ＝ 一到三天；**L** ＝ 一週以上；**X** ＝ 要先做一個新元件。
「現況」欄是 macOS 上的 clawdline-go（master `d40545e`）。

### 3.1 終端機與 session

| 功能 | 靠什麼 | 現況 | Linux | Windows | 做不到時怎麼呈現 | 難度 |
|---|---|---|---|---|---|---|
| 盤點既有 session（tmux） | `tmux list-panes` | ✅ `terminal/tmux.go` | ✅ 同一條路，零修改 | ❌ 原生無 tmux；WSL／MSYS2 才有 | Windows：清單只列 owned session，並在頁面上說明「這台沒有 tmux」 | S |
| 盤點既有 session（iTerm2） | osascript／JXA | ✅ `iterm_darwin.go` | ❌ 沒有 iTerm2 | ❌ Windows Terminal **沒有任何遙控 API** | 具名不支援（`hosts_other.go` 已經只回 tmux） | — |
| 盤點行程 | `/bin/ps -ax -o tty,pid,pgid,tpgid,command` | ✅ `process/ps_unix.go`（darwin\|\|linux） | ✅ 可用；建議改讀 `/proc`（精簡容器沒有 `ps`） | ⚠️ `process/ps_windows.go` 目前誠實回 `Complete:false` | Windows：`Complete:false` ＋ 「這台的行程盤點還沒實作」 | M |
| 自己開 session | `tmux new-session -d` | ✅ | ✅ | ⚠️ 要 ConPTY（§4.1） | — | L |
| 送訊息／送鍵／中斷／關閉 | `send-keys`（`-l`、`-H`）、`C-c`、`kill-pane` | ✅ | ✅ | ⚠️ ConPTY：直接寫輸入 pipe（`\x03` 就是中斷） | — | M |
| 讀畫面（attached） | `capture-pane -p -e -J` | ✅ | ✅ | ❌ 沒有 tmux 就沒有 | 「這個 session 的畫面讀不到」，而不是空白畫面 | — |
| 讀畫面（owned） | — | — | 自有 pty ＋ VT 解析器 | ConPTY ＋ VT 解析器（**同一個元件**） | — | X |
| 即時畫面推送 | `pipe-pane` → FIFO，可讀性就是訊號 | ✅ `signal_unix.go` | ✅ 同一條路 | ❌ 沒有 mkfifo；`signal_other.go` 回 nil | 已經正確：畫面標成 `on-demand`，不假裝 4ms | — |
| 即時畫面推送（owned） | — | — | pty 輸出本身就是訊號 | ConPTY 輸出 pipe 本身就是訊號 | — | S（做完 ConPTY 就有） |
| 在畫面上顯示（focus／reveal） | `select-pane` ＋ iTerm2 activate | ✅ | ⚠️ 只做得到 tmux 內的 select；**Wayland 沒有 client 主動 raise 視窗的協定** | ⚠️ 同左；`SetForegroundWindow` 有前景鎖 | 已經正確：`ok` 只承諾「選到了」，不承諾視窗在你面前 | S |
| 砍整棵行程樹 | `Setpgid` ＋ `killpg` | ✅ `supervisor_unix.go` | ✅ | ⚠️ 要 Job Object；現在誠實拒絕啟動 | 已經正確：寧可不啟動，也不要讓呼叫者以為能取消 | M |

### 3.2 圖片、剪貼簿、語音

| 功能 | 靠什麼 | 現況 | Linux | Windows | 做不到時怎麼呈現 | 難度 |
|---|---|---|---|---|---|---|
| 瀏覽器端附圖／貼上／拖拉 | HTML5，全在前端 | ✅ | ✅ 零修改 | ✅ 零修改 | — | — |
| 圖片解碼（PNG／JPEG／GIF） | Go 標準庫 | ✅ | ✅ | ✅ | — | — |
| 圖片解碼（HEIC／TIFF／WebP／BMP） | `/usr/bin/sips` | ✅ `decode_darwin.go` | ❌ `ErrDecoderUnavailable` | ❌ 同左 | 已經正確：回 `unsupported_image`，不是靜默接受 | S（純 Go webp／bmp 解碼器可補） |
| 把圖交給終端裡的 assistant | 借用 macOS pasteboard 再還原 | ✅ `pasteboard_darwin.go` | ❌ 見 §4.3，**建議不做** | ❌ 同左 | 已經正確：`ErrPasteboardUnsupported` → 改交路徑 | — |
| 語音錄音 | 瀏覽器 MediaRecorder → 16 kHz PCM | ✅ | ✅ 需 secure context | ✅ 同左 | 非 localhost 的 http 來源：麥克風按鈕要說「這個網址不能錄音」 | S |
| 語音轉文字 | `whisper-cli` ＋ `ggml-*.bin` | ✅ | ✅ 搜尋路徑已寫好 | ✅ 搜尋路徑已寫好 | 已經正確：`ErrNoBinary` 與 `ErrNoModel` 分開 | S（缺的是安裝指引，§4.4） |

### 3.3 秘密、權限、儲存

| 功能 | 靠什麼 | 現況 | Linux | Windows | 做不到時怎麼呈現 | 難度 |
|---|---|---|---|---|---|---|
| 裝置與 token 儲存 | 0700 目錄 ＋ 0600 檔 ＋ atomic rename | ✅ `devices/`、`cloudkeys/` | ✅ 原生正確 | ❌ **Windows 沒有 0600**，`os.Chmod` 只改唯讀位元 | 見 §4.5：要 NTFS ACL ＋ DPAPI，這是目前唯一的 blocking 安全落差 | M |
| 秘密進系統 keystore | — | ❌ 全部是檔案 | Secret Service（D-Bus）；headless 沒有 | DPAPI／Credential Manager（純 Go 可做） | 設定頁明說「這台用的是檔案」，不要假裝 | M |
| 拒絕 symlink | `O_NOFOLLOW` ＋ Lstat ＋ same-file | ✅ | ✅ | ⚠️ `nofollow_other.go` 只剩 Lstat；還有 junction／ADS／短名 | §4.11 | M |
| 事件儲存 | modernc.org/sqlite（純 Go） | ✅ | ✅ | ✅ 但 WAL 不可放網路磁碟 | 狀態目錄若在 UNC／對映磁碟機，啟動時就拒絕並說明 | S |
| 狀態目錄位置 | `config.Dir()` | ✅ | `$XDG_CONFIG_HOME` → `~/.config` | `%APPDATA%` | — | — |
| 文件路由的路徑穿越防護 | 副檔名白名單 ＋ 根目錄先算定 | ✅ `documents/` | ✅ | ⚠️ 要補 Windows 專屬檢查（§4.11） | — | M |

### 3.4 桌面整合

| 功能 | 靠什麼 | 現況 | Linux | Windows | 做不到時怎麼呈現 | 難度 |
|---|---|---|---|---|---|---|
| 原生視窗（webview） | WKWebView | ✅ `shell/darwin/` | **建議不做殼**，用系統瀏覽器 | WebView2 ＋ `go-webview2` | Linux：`clawdline open` 已經是完整答案 | L（Windows） |
| 選單列／系統匣 | NSStatusItem | ✅ | StatusNotifierItem（D-Bus）；**GNOME 預設沒有匣** | `Shell_NotifyIcon` | GNOME：退回瀏覽器分頁的徽章（§4.9） | M |
| 全域熱鍵 | Carbon `RegisterEventHotKey` | ✅ `HotKey.swift` | X11 可；**Wayland 基本上不行**（§4.8） | `RegisterHotKey`（user32） | Wayland：設定頁換成「由你的桌面設定一個捷徑指向 `clawdline panel`」 | M |
| 登入時啟動 | `SMAppService` | ✅（預設關） | `systemd --user` ＋ `loginctl enable-linger`；或 XDG autostart | `HKCU\...\Run` | 無桌面且無 systemd：印出手動指令 | M |
| 本機桌面通知 | **舊版沒有**（實測：`Sources/` 找不到 `UNUserNotificationCenter`） | — | `org.freedesktop.Notifications` | Toast（需 AUMID） | 見 §4.6：通知有三個出口，不是一個開關 | M |
| 推播到手機 | Web Push（RFC 8291 ＋ VAPID） | ❌ 未移植 | ✅ 天生跨平台 | ✅ 天生跨平台 | — | L |
| 瀏海島 | MacBook 的實體瀏海 | ❌ 未移植（task `a0d852b4` 進行中） | 無瀏海 | 無瀏海 | 抽象成 `Presence` port，見 §4.9 | M |
| 拖放到原生視窗 | AppKit | ❌ 未移植 | — | — | 瀏覽器的 HTML5 拖放已經覆蓋 | — |
| Dock／工作列圖示 | `.icns` | ✅ | `.desktop` ＋ PNG | `.ico` | — | S |

### 3.5 遠端與互通

| 功能 | 靠什麼 | 現況 | Linux | Windows | 做不到時怎麼呈現 | 難度 |
|---|---|---|---|---|---|---|
| 本機認證閘門 | 檔案 token ＋ cookie | ✅ | ✅ | ⚠️ 同 §3.3 的權限問題 | — | — |
| `clawdline open` 開瀏覽器 | `open`／`xdg-open`／`rundll32` | ✅ 三平台已寫 | ✅ headless 無效 → `--print` | ✅ | headless：`--print` 印網址（已實作） | — |
| 六位數配對 | CLI 印出 | ✅ | ✅ | ✅ | 無殼平台本來就走 CLI（`remote.md` §5 已寫） | — |
| tunnel | 使用者自備 cloudflared | ❌ 未做 | ✅ 有官方二進位 | ✅ 有官方二進位 | — | M |
| Cloud bridge | PROTOCOL.md | ❌ 未做 | ✅ 純網路 | ✅ 純網路 | — | L |
| 讀舊 Swift app 的 store | `~/.config/clawdline` | ✅ `swiftstore/` | 天生為空（那台沒有舊 app） | 天生為空 | 用量頁退回 transcript（已實作）；皇冠／交付勾就是沒有 | — |
| 用量帳本 | `~/Library/.../usage.sqlite3` | ✅ | ❌ `ObservabilityDir()` 只在 darwin 回值 | ❌ 同左 | 已經正確：退回 transcript 計算 | — |
| 讀 transcript ／方案額度 | `~/.claude`、`~/.codex` | ✅ | ✅ 同樣路徑 | ✅ `%USERPROFILE%` | — | — |
| 排程 | 純 Go | ✅ | ✅ | ✅ | — | — |

---

## 4. 逐項

<!-- section:sessions owner:task-1d861805 -->
### 4.1 Windows 上沒有 tmux，session 怎麼開與讀

`降級`（WSL 模式為 `可實作`）

這是整份文件裡最大的一題，因為 `plan.md` §1 的第一條——「協調你已經開著的 session，而不是取代
它」——在 Windows 上**沒有辦法完整成立**。理由不是懶，是 Windows 根本沒有這個產品需要的那個東西：

- **Windows 原生沒有 tmux。** tmux 要 POSIX pty。MSYS2／Cygwin 有 port，但那個 pty 是模擬的，
  而且只控制得到同一個 MSYS 環境裡跑的行程；Windows 原生的 console 應用在它底下行為不同。
- **Windows Terminal 不能被遙控。** 它有 `wt.exe` 命令列可以**開**新分頁（`wt -w 0 new-tab`），
  但**沒有任何公開 API 可以對一個已經存在的分頁送鍵或讀畫面**。所以 Windows Terminal 只能當
  「開啟器」，不能當 `TerminalHost`。
- **Windows 沒有 tty 與前景行程群組。** `ps_unix.go` 靠 `pgid == tpgid` 判斷「這個終端機現在
  正在跑的是哪一個行程」，Windows 上這個概念不存在。一個 console 可以有多個 attached 行程，
  而且沒有「前景群組」。

所以 Windows 要分三種形態講，**建議把三種都做，並在設定頁明白標示現在是哪一種**：

#### (a) WSL 模式 —— 建議的預設

**整個 daemon 用 Linux 版二進位跑在 WSL2 裡**，一切照 Linux 走：tmux 有、pty 有、procfs 有、
FIFO 有。Windows 那邊只有瀏覽器（或將來的 WebView2 殼）連 `http://127.0.0.1:7727`——WSL2 的
localhost 轉發讓這件事不需要任何設定。

- **代價 1**：專案檔案要放在 WSL 的檔案系統裡（`/home/...`）才有正常的 I/O 速度與 inotify；
  放在 `/mnt/c/...` 兩者都會壞。
- **代價 2**：用 Windows 原生工具鏈（MSVC、.NET）的專案不適用。
- **代價 3**：`~/.claude`、`~/.codex` 是 WSL 裡那一份，不是 Windows 那一份。使用者若兩邊都跑過
  assistant，會看到兩套歷史。**這一點要在頁面上講明**，不然他會以為 session 消失了。
- 這條路的工作量幾乎是零（已經有 linux/amd64 的二進位，而且已在容器裡跑過），**所以它應該是
  Windows 上第一個能用的形態**，而不是等 ConPTY 做完。

#### (b) 原生 Windows ＋ owned session —— ConPTY

daemon 自己用 **ConPTY**（`CreatePseudoConsole`，Windows 10 1809＋）開一個 pseudo console，把
assistant 跑在裡面。daemon 就是這個 pty 的 host，所以：

| 要做的事 | tmux 上怎麼做 | ConPTY 上怎麼做 |
|---|---|---|
| 送訊息 | `send-keys -l <text>` ＋ `Enter` | 往輸入 pipe 寫 UTF-8 ＋ `\r` |
| 送一個原始按鍵（選單用） | `send-keys -H <hex>` | 往輸入 pipe 寫那一個 byte |
| 中斷 | `send-keys C-c` | 往輸入 pipe 寫 `\x03` |
| 關閉 | `kill-pane` | 關掉 pty ＋ 砍 Job Object |
| 改大小 | tmux 自己管 | `ResizePseudoConsole` |
| 讀畫面 | `capture-pane -p -e -J` | **見下一節：自己解析 VT 流** |

純 Go 可做：`golang.org/x/sys/windows` 有全部的 syscall，或直接用 `github.com/UserExistsError/conpty`
（MIT，純 Go）。**不需要 cgo。**

- **這是 Windows 上唯一能「完整」的路**，但它只涵蓋 daemon 自己開的 session。
- **代價**：使用者自己在 Windows Terminal 裡手動開的那個 claude，daemon 讀不到也送不進去。
  產品上要接受：**Windows 上要用 Clawdline，就從 Clawdline 開 session。**

#### (c) 原生 Windows ＋ 既有 session 的「只看得見」

即使不能控，也應該看得見。`CreateToolhelp32Snapshot`（純 Go）可以列出 pid／ppid／執行檔名；
命令列（`classify()` 與 `resumeID` 都要它）則要 WMI（`Win32_Process.CommandLine`，可用純 Go 的
COM 綁定）或 `NtQueryInformationProcess` 讀 PEB。

- **呈現**：這些 session 出現在清單上，圖示與標題照舊，但**狀態明白標成「偵測到，這台無法遙控」**，
  送出框停用並附上理由。這比讓它們從清單上消失好——使用者知道那個 session 在跑，看不到它才叫故障。
- 這正好對應 `ps_windows.go` 現在的 `Complete:false`：**不能看，和看到空的，是兩件事**，那個檔案
  已經把這條規則寫下來了。

**修正總表**：無。

<!-- /section:sessions -->

<!-- section:screen owner:task-1d861805 -->
### 4.2 螢幕內容怎麼讀（ConPTY 的緩衝區）

`可實作`（需要一個新元件）

讀畫面有兩條完全不同的路，而分界線不是作業系統，**是這個 session 是誰開的**：

| | attached（別人開的） | owned（daemon 自己開的） |
|---|---|---|
| macOS | tmux `capture-pane`；iTerm2 有 `Capture` | pty ＋ VT 解析器 |
| Linux | tmux `capture-pane` | pty ＋ VT 解析器 |
| Windows | **沒有** | ConPTY ＋ VT 解析器 |

也就是說，**「owned session 的讀畫面」在三個平台上是同一個元件**，值得先寫它。

#### ConPTY 給你的不是緩衝區，是一條 VT 流

這一點很容易誤解，所以寫清楚：`CreatePseudoConsole` 給你一對 pipe。你寫進去的是鍵盤輸入；
你**讀出來的是已經渲染好的 VT escape sequence 流**——游標移動、清行、顏色、重繪，全都在裡面。
它不是一個你可以隨時去 dump 的二維字元陣列。

所以要有「畫面」，daemon 必須自己維護一個終端機狀態機：把那條流餵進去，狀態機裡永遠有一份
當下的螢幕，隨時可以輸出成跟 `capture-pane -p -e -J` 一樣的東西。

**這件事舊版評估過，而且拒絕過。** `LiveScreen.swift` 的表格裡，方案 B 是「`pipe-pane` ＋ VT
emulator」，被否決的理由是：

> 一個中途加入的讀取者，25 行裡有 13–14 行是錯的，而且在五個加入點裡有兩個**永遠不收斂**——
> Claude Code 只重畫有變的行，你到達之前畫的那一行，對你來說永遠是空的。

**這個理由在 owned session 上不成立**，而這是關鍵的轉折：owned session 的 daemon 從**第一個
byte** 就在讀，根本沒有「中途加入」這回事。所以舊版拒絕 B 的那個理由，在這裡剛好反過來——
B 是唯一可行的，而且是正確的。舊版量到 B 的 emulator 「以 100.00% 的 cell 與顏色精確度重現了
三條真實的流，零個未實作的序列」，349 行 Swift，所以規模是可控的。

Go 的候選：`github.com/hinshun/vt10x`、`github.com/charmbracelet/x/vt`，或照舊版那 349 行自己寫。
**建議自己寫或包一層**，理由跟舊版一樣：這個東西要跟著 Claude Code 的重繪方式走，而那不是
一個函式庫會替你維護的東西。

#### 評估過但不採用：`ReadConsoleOutput`

Windows 有 `AttachConsole(pid)` ＋ `ReadConsoleOutput` 可以直接讀一個 console 的字元緩衝區。
不採用，三個理由：

1. **一個行程同時只能 attach 一個 console。** daemon 要輪流 attach／detach，而 attach 會接管
   自己的 stdio。
2. 對 ConPTY 托管的 pseudo console 適用性有限。
3. 它讀的是那個 console 的緩衝區，而我們自己就是 pty host——繞遠路去讀自己已經有的東西。

#### 即時性

- macOS／Linux 的 attached：`pipe-pane` → FIFO，FIFO 的可讀性就是訊號（實測 0.014 ms）。已實作。
- **owned（三平台）**：pty／ConPTY 的輸出 pipe 本身就是訊號——有 byte 讀出來就是畫面動了。
  比 FIFO 那一套更簡單，因為不需要第二個管道。
- Windows 的 attached：沒有，`signal_other.go` 回 nil，畫面標成 `on-demand`。**這是對的**，
  不要改成一個永遠不會醒的 stub。

#### 一個要記得設的旗標

ConPTY 底下，Windows console 的輸出 code page 預設不是 UTF-8（繁體中文機器上是 CP950）。
要 `SetConsoleOutputCP(CP_UTF8)` ／ `SetConsoleCP(CP_UTF8)`，否則中文全是亂碼。
這一條在真機上第一次跑就會撞到。

**修正總表**：無。

<!-- /section:screen -->

<!-- section:clipboard owner:task-1d861805 -->
### 4.3 剪貼簿與圖片：Linux／Windows 建議「不做借用」

`不支援`（建議維持現況）

現在把圖交給終端裡的 assistant 的做法是：**借用系統剪貼簿 → 放圖 → 送 Ctrl-V → 還原**
（`pasteboard_darwin.go`，對應舊版 `Targets.send(_ pieces:to:)`）。Claude Code 收到 Ctrl-V 時去讀
剪貼簿，畫面上顯示 `[Image #1]`。

**在 Linux 上這條路建議不要做**，理由是 X11 與 Wayland 的剪貼簿所有權模型：

- X11 的 selection **不是一塊記憶體，是一個「誰現在擁有它」的宣告**。擁有者必須活著，才能在
  別人貼上時把資料交出去。`xclip` 因此會 fork 一個常駐行程。
- Wayland 更嚴：**設定剪貼簿要有 focus**。`wl-copy` 靠開一個隱形 surface 繞過，而且同樣必須
  保持存活。
- 這表示「借用再還原」在 Linux 上不是兩次呼叫，是**兩次交接所有權**，而且中間那段時間
  daemon 必須持有使用者的剪貼簿。一旦 daemon 在那段時間掛掉，使用者原本複製的東西就沒了。
  這跟「不搶你的鍵盤」是同一類的傷害。

**建議的呈現**：Linux 與 Windows 上，送圖就是**把檔案路徑交給 assistant**（現在
`ErrPasteboardUnsupported` 已經是這個行為）。

- **代價**：Claude Code 在那些機器上收到的是一行路徑，不是 `[Image #1]`。它仍然讀得到圖
  （它有讀檔工具），但對話裡的呈現不一樣，而且要多一次工具呼叫。
- **要講給使用者聽的**：在附圖的提示行寫一句「這台機器會把圖片以路徑交給 assistant」，
  而不是讓他自己從結果推論。

**Windows** 技術上比較容易（`OpenClipboard`／`SetClipboardData` 是一次呼叫，資料交給系統保管，
不需要常駐），所以將來若真的要做，**先做 Windows**。但同樣的問題還在：借用期間使用者複製的
東西會被蓋掉。現行的 `change count` 保護（別人複製過就不還原）在 Windows 上有對應的
`GetClipboardSequenceNumber`，所以可以照抄。

#### 圖片解碼

`sips` 只有 macOS 有。Linux／Windows 上 HEIC、TIFF、WebP、BMP 目前一律 `unsupported_image`。

- 補救成本最低的是 **WebP 與 BMP**：純 Go 有 `golang.org/x/image/webp`（只支援解碼，夠用）
  與 `golang.org/x/image/bmp`。**建議直接補上，三平台一起受益**——現在連 macOS 都是繞去叫 `sips`。
- HEIC 沒有純 Go 解碼器，維持 `unsupported_image`。呈現：錯誤訊息要說「這台機器讀不了 HEIC，
  請先轉成 PNG 或 JPEG」，不要只說「不支援的圖片」。

**修正總表**：無。

<!-- /section:clipboard -->

<!-- section:whisper owner:task-1d861805 -->
### 4.4 whisper-cli 的安裝方式

`已實作`（搜尋路徑）／`可實作`（安裝指引）

`internal/adapters/whisper` 的搜尋路徑三個平台都已經寫好了，而且 `ErrNoBinary` 與 `ErrNoModel`
是分開的兩個錯誤——`whisper.go` 的檔頭已經說明為什麼要分開（「只告訴他『關著』，他會去檢查
一件他已經做過的事」）。**缺的不是程式，是指引。**

| | 執行檔怎麼來 | 搜尋路徑（已寫好） | 模型怎麼來 | 模型路徑（已寫好） |
|---|---|---|---|---|
| **macOS** | `brew install whisper-cpp` | `/opt/homebrew/bin`、`/usr/local/bin` | `download-ggml-model.sh`，或沿用舊 app 下載過的 | `~/.cache/whisper`、`~/Library/Application Support/Clawdline/models` |
| **Linux** | **沒有主流發行版套件**。實務上是自己編：`git clone whisper.cpp && cmake -B build && cmake --build build -j` | `/usr/local/bin`、`/usr/bin`、`/opt/whisper.cpp/build/bin`、`~/.local/bin`、`~/whisper.cpp/build/bin` | 同上 | `~/.cache/whisper`、`~/.local/share/whisper`、`/usr/share/whisper`、`/opt/whisper.cpp/models` |
| **Windows** | 官方 release 有預編 zip，解壓到 `%LOCALAPPDATA%\whisper.cpp\` | `%LOCALAPPDATA%\whisper.cpp[\bin]`、`%ProgramFiles%\whisper.cpp[\bin]` | 同上 | `%LOCALAPPDATA%\whisper`、`%LOCALAPPDATA%\whisper.cpp\models` |

三件要做的事：

1. **設定頁把路徑印出來。** 「沒找到 whisper-cli」旁邊直接列出**這台機器實際搜尋過的目錄**，
   以及這個平台的安裝指令，附一個複製按鈕。使用者最常犯的錯是裝了但裝在別的地方。
2. **明講不吃哪一個。** pip／uv 裝的 `openai-whisper` 是另一個工具、另一組 CLI 參數，**不相容**。
   Linux 使用者很容易裝到那個然後以為壞了。錯誤訊息要點名。
3. **模型可以代下載，執行檔不行。** 建議加一個 `clawdline voice install-model <name>`，把
   `ggml-<name>.bin` 下載到 `~/.cache/whisper`（三平台都是第一個搜尋位置）。執行檔要編譯，
   不代勞——那是一個會失敗在一百種地方的事，而失敗的錯誤訊息會變成我們的責任。

**Linux 上還有一個前提**：瀏覽器要錄音，頁面必須是 secure context。`http://127.0.0.1:7727`
算（localhost 是例外），但**從區網 IP 連進來的 `http://192.168.x.x:7727` 不算**，麥克風會被
瀏覽器拒絕。這不是 Linux 專屬，但 Linux 上「在另一台機器開瀏覽器連過去」是常見用法，所以
要在 `#mic` 停用時說出真正的理由。

**修正總表**：無。

<!-- /section:whisper -->

<!-- section:secrets owner:task-1d861805 -->
### 4.5 金鑰儲存：Keychain／Credential Manager／Secret Service／檔案

`已實作`（檔案）／`可實作`（keystore）／**Windows 有一個 blocking 落差**

現況：`internal/adapters/cloudkeys`（device Ed25519 seed、account master secret）與
`internal/adapters/devices`（local token、machine token、裝置雜湊、audit log）**全部是檔案**，
0700 目錄 ＋ 0600 檔 ＋ 寫入走同目錄暫存檔再 rename ＋ 開檔一律 `O_NOFOLLOW`。接縫是
`cloud.KeyStore`。`remote.md` §4 已經記下這是刻意的第一版選擇。

#### 先講那個 blocking 的落差

**Windows 上「0600」是一句空話。** `os.Chmod` 在 Windows 只改唯讀位元，不設 ACL。所以
`cloudkeys` 與 `devices` 兩個 package 檔頭寫的那條規則——「這個檔案是這個人的，別人讀不到」
——在 Windows 上目前**不成立**。同一台機器上的另一個使用者帳號讀得到 local token，而 local token
可以 send、可以管理裝置。

兩件事一起做才補得起來，**而且建議兩件都做，不要二選一**：

1. **NTFS ACL**：建檔時用 `SetNamedSecurityInfo`／`CreateFile` 帶一個只含目前使用者 SID 的 DACL，
   並關掉繼承。純 Go 可做（`golang.org/x/sys/windows`）。
2. **DPAPI**：`CryptProtectData`（user scope）把內容加密之後才寫檔。即使檔案被複製走，
   換一個帳號也解不開。純 Go 可做。

第 2 項同時就是 Windows 的 keystore 答案，所以它不是額外的工作，是**把 §3.3 那一列和這一列
合併成同一件事**。

#### 四個平台的 keystore

| | 做法 | 純 Go？ | 限制 |
|---|---|---|---|
| **macOS** | Keychain | ❌ 要 cgo（Security.framework） | 替代：子行程 `/usr/bin/security`（但密碼會出現在命令列參數），或**由 Swift 殼代管**——殼本來就連得到 Security.framework |
| **Windows** | DPAPI（user scope）＋ 檔案；或 Credential Manager | ✅ | Credential Manager 單筆上限 2560 bytes，對 32-byte key 綽綽有餘 |
| **Linux（有桌面）** | Secret Service（D-Bus `org.freedesktop.secrets`，gnome-keyring／KWallet） | ✅（`godbus/dbus`） | **要有 desktop session 且 keyring 已解鎖**。SSH 進去的 session 通常沒有 |
| **Linux（headless／容器）** | 0600 檔案 | ✅ | 就是現況 |

**macOS 的建議**：不要為了 Keychain 開 cgo——那會拆掉「一台機器產六個平台」這件事。
要嘛維持檔案，要嘛讓 Swift 殼在啟動時從 Keychain 讀出來、交給 daemon。後者比較對，因為殼本來
就已經在做同一件事（它把本機 token 注入 WKWebView 的 cookie）。

#### 三條規則，寫進設計

1. **檔案是地板，keystore 是加值。** 啟動時偵測，能用就用，用不了就用檔案。
   **絕不因為 keystore 不在就拒絕啟動**——headless Linux 是正常的部署形態，不是壞掉的桌面。
2. **降級要說出來，而且要說在兩個地方**：設定頁的一行字，以及 `/v1/health` 的一個欄位
   （讓 CLI 與監控也看得到）。「秘密存在哪裡」是使用者有權知道的事。
3. **讀不到 ≠ 不存在。** 這是 `cloudkeys` 檔頭已經寫下的規則，換 keystore 時特別危險：
   使用者換了桌面環境、keyring 沒解鎖、DPAPI 因為帳號密碼被管理員重設而失效——這些情況下
   **必須停下來報錯，不可以 mint 一把新的**。mint 新的會讓他所有已配對的裝置靜默失效，
   而他第一個察覺的症狀會是「什麼都解不開了」。

#### 遷移

一旦某個平台開始用 keystore，就要有從檔案搬過去的路徑，而且**搬完要把檔案刪掉並記一筆 audit**。
反方向（keystore → 檔案）不要自動做：那是降級，要使用者明確同意。

**修正總表**：`3.3 / 裝置與 token 儲存 / Windows` 這一格的難度應為 **M**，且屬於 **blocking**——
Windows 版在補上 ACL ＋ DPAPI 之前不應該對外發布。

<!-- /section:secrets -->

<!-- section:notifications owner:task-1d861805 -->
### 4.6 通知

`可實作`

先講一個實測到、而且對跨平台是好消息的事實：

> **舊 Swift app 沒有使用 macOS 本機通知中心。** `grep -rn "UNUserNotificationCenter|NSUserNotification|display notification" Sources/`
> 在舊 app 的 `Sources/` 底下**零個結果**。它唯一會主動離開這台機器的東西是 **Web Push**
> （`WebPush.swift`，RFC 8291 ＋ VAPID，加密後交給 Apple／Mozilla／Google 的 push service）。

也就是說，**「通知」這個功能天生就是跨平台的**，因為訂閱在瀏覽器端，daemon 只需要能對外發
HTTPS。Linux server、Windows、macOS 都一樣。

所以通知不該是設定頁上一個開關，而是**三個出口，各自有狀態**：

| 出口 | 需要什麼 | macOS | Linux | Windows |
|---|---|---|---|---|
| **1. Web Push 到已配對的裝置**（主要路徑） | 瀏覽器訂閱 ＋ daemon 能對外連線 | ✅ | ✅ | ✅ |
| **2. 本機桌面通知**（有殼、有桌面時的加值） | 平台 API | `UNUserNotificationCenter`（要 signed bundle，殼已經是 `.app`） | `org.freedesktop.Notifications`（D-Bus，幾乎每個桌面都有；headless 沒有） | Toast（`Windows.UI.Notifications`；**要 AUMID，而且開始功能表要有捷徑才會顯示**） |
| **3. console 頁面本身**（永遠有） | 無 | ✅ | ✅ | ✅ |

出口 3 是地板：分頁標題的徽章、favicon、header 的 counts。它不需要任何權限、任何平台 API，
而且在使用者正看著頁面的時候它其實是最好的那個。

#### 三個要寫進呈現的限制

1. **Service Worker 要 secure context。** 瀏覽器要訂閱 Web Push，頁面必須是 https 或 localhost。
   `http://127.0.0.1:7727` 可以；**`http://192.168.x.x:7727` 不可以**。所以「從另一台機器連進來
   的人收不到推播」——除非走 tunnel 的 https。這一條要在設定頁的通知區塊直說，不然使用者會以為
   是 bug。
2. **Windows Toast 的 AUMID。** 沒有註冊 AppUserModelID 的行程發出的 toast 不會顯示，而且
   **不會報錯**。這是 Windows 上最容易「以為做完了其實沒有」的一項，要在真機驗證清單裡。
3. **headless Linux 沒有出口 2。** 這是正常狀態，不是故障。設定頁列「本機桌面通知：這台機器
   沒有桌面工作階段」。

#### 建議的順序

Web Push（出口 1）本來就要為 Cloud 做，做完三個平台一起有。出口 2 是三份各自的小工，
建議排在原生殼之後，因為它們都要殼提供身分（bundle／AUMID／`.desktop` 名稱）。

**修正總表**：無。

<!-- /section:notifications -->

<!-- section:autostart owner:task-1d861805 -->
### 4.7 開機自動啟動

`已實作`（macOS）／`可實作`（Linux、Windows）

macOS 已經有：`SMAppService.mainApp`，一個開關，預設關閉（`replica.md` 的原生殼表）。

#### Windows

四個候選，**建議選第一個**：

| 做法 | 純 Go | 優點 | 缺點 |
|---|---|---|---|
| **`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`** | ✅ `x/sys/windows/registry` | 一行登錄值；使用者自己在工作管理員的「開機」分頁看得到也關得掉 | 登入後才跑；部分防毒會多看一眼 |
| Startup 資料夾（`shell:startup` 放 `.lnk`） | ⚠️ 要 COM 產生捷徑 | 使用者看得懂 | 麻煩，沒有好處 |
| Task Scheduler（`schtasks /create /sc onlogon`） | ✅（子行程） | 可延遲啟動、可自動重啟 | 企業環境常鎖住；XML 難維護 |
| Windows Service（SCM） | ✅ | 開機就跑，不用登入 | **不要做**——見下 |

**不要做成 Windows Service。** 服務跑在 Session 0，與使用者的桌面工作階段隔離：它看不到使用者
的終端機、拿不到使用者的 `%USERPROFILE%\.claude`、也不能開視窗。這個 daemon 的工作**就是**
使用者那個工作階段裡的事。把它做成服務會得到一個技術上在跑、產品上什麼都做不到的東西。

#### Linux

兩條，**建議都支援並自動偵測**：

| 情形 | 做法 |
|---|---|
| 有桌面 | XDG autostart：`~/.config/autostart/clawdline-next.desktop`。主流桌面全支援 |
| 無桌面／server | `systemd --user`：`~/.config/systemd/user/clawdline-next.service` ＋ `systemctl --user enable --now` |

**`loginctl enable-linger $USER` 這一步一定要寫出來。** 沒有 linger，systemd 預設在使用者最後
一個工作階段結束時殺掉他所有的行程（`KillUserProcesses`）——也就是說，SSH 登出，daemon 就沒了。
這是 AWS 上第一個會撞到的坑，而且症狀（「我明明 enable 了」）跟設定錯誤長得一模一樣。

#### 統一的介面

建議做一個 `clawdline autostart enable|disable|status` 子命令，把三個平台的差異關在裡面，
設定頁只呼叫它、只顯示它回報的狀態。理由：**設定頁上那個開關要能誠實地回答「現在是開還是關」**，
而三個平台的「怎麼問」完全不同；把它放在一個地方，設定頁就不用知道 registry 長什麼樣子。

無桌面、無 systemd 的機器（極簡容器）：`status` 回「這台機器沒有可用的自動啟動機制」，
並印出手動的指令。

**修正總表**：無。

<!-- /section:autostart -->

<!-- section:hotkey owner:task-1d861805 -->
### 4.8 全域熱鍵，以及 Wayland 的限制

`已實作`（macOS）／`可實作`（Windows、X11）／`降級`（Wayland）

#### macOS（現況）

Carbon 的 `RegisterEventHotKey`。`HotKey.swift` 的檔頭寫明為什麼不用 `NSEvent` 的 global
monitor：那要輔助使用權限，而**一個開視窗的工具不該能讀你按的每一個鍵**。這條理由在後面
每一個平台都要再用一次。

#### Windows

`RegisterHotKey`（user32），純 Go 可呼叫。簡單、不需要任何權限。兩個要處理的事實：

- **同一組合被別的程式先註冊，`RegisterHotKey` 會失敗。** 要具名回報（「`Ctrl+Shift+K` 已經被
  這台機器上的另一個程式佔用」），不要靜默失敗然後讓使用者按了沒反應。
- 需要一個訊息迴圈來收 `WM_HOTKEY`，所以它住在殼裡，不住在 daemon 裡。

#### Linux — X11

`XGrabKey`。純 Go 有 `github.com/BurntSushi/xgb`（X11 protocol 的純 Go client，不需要 Xlib）。
可行，難度 M。同樣要處理「已被佔用」（`BadAccess`）。

#### Linux — Wayland：**設計上就不允許**

這是要講給使用者聽的核心事實：**Wayland 的設計裡，一般的 client 不能攔截全域按鍵。**
鍵盤事件只有 compositor 看得到，沒有 focus 的視窗什麼都收不到。這不是缺一個函式庫，
是一個刻意的安全邊界——而且它的理由跟 `HotKey.swift` 拒絕 `NSEvent` monitor 的理由一模一樣。

三條路：

1. **XDG Desktop Portal 的 `org.freedesktop.portal.GlobalShortcuts`**（唯一的「正統」做法）。
   - 支援度不齊：KDE Plasma 有實作；GNOME 長期沒有（**要在真機用 `busctl introspect` 確認，
     見 §8**）；wlroots 系（Sway、Hyprland）靠 `xdg-desktop-portal-wlr`，覆蓋不完整。
   - 即使有，**綁哪一個鍵是 compositor 決定的，不是 app**，而且第一次註冊會彈一個系統對話框
     要使用者確認。所以設定頁上那個「錄製熱鍵」的 UI 在這條路上是不成立的。
2. **叫使用者在自己的桌面設定一個捷徑。** GNOME「設定 → 鍵盤 → 自訂捷徑」、KDE「系統設定 →
   捷徑」，指向一個命令。**這條在每一個桌面都能用、不需要任何權限、不需要 portal**，
   而且使用者完全掌握綁什麼鍵。
3. `libinput`／`/dev/input` 直接讀。**拒絕**——那要進 `input` 群組，等同 keylogger。

**建議的降級呈現**（安全的預設，不等使用者拍板）：

- 偵測 `$WAYLAND_DISPLAY` 或 `$XDG_SESSION_TYPE=wayland`。
- 設定頁的熱鍵欄位**換掉，不是停用**：改成一段說明「你的桌面（Wayland）由 compositor 管理
  快捷鍵」＋ 一個可複製的命令列 ＋ 兩三個主流桌面的設定路徑。
- 為此需要一個 `clawdline panel` 子命令：向 daemon 要求把 console 叫出來（開瀏覽器或叫殼的
  視窗）。**這個子命令三個平台都有用**，不只 Wayland——它同時是「沒有殼的 Linux」的那個入口。
- 若 D-Bus 上查得到 `GlobalShortcuts` portal，才額外顯示「用系統對話框設定」的按鈕。

**代價**：Wayland 使用者要多做一次設定，而且那次設定在我們的 UI 外面。**這是誠實的代價**，
比一個按了沒反應的錄製框好，也比要求他把 daemon 加進 `input` 群組好太多。

#### 順帶：「把視窗叫到前面」在 Wayland 也不行

同一個邊界的另一面。X11 有 `_NET_ACTIVE_WINDOW`（`wmctrl`／`xdotool`）；**Wayland 沒有讓 client
主動 raise 自己的協定**（`xdg-activation` 需要一個由使用者互動產生的 token，daemon 沒有）。
所以 Linux 上的「在畫面上顯示」只做得到 tmux 內的 `select-pane`／`select-window`——這正好
就是 `reveal_other.go` 現在的行為，而且它的註解已經把這件事說對了：
**`ok` 是「選到了」，不是「它在你面前」。**

**修正總表**：無。

<!-- /section:hotkey -->

<!-- section:notch owner:task-1d861805 -->
### 4.9 瀏海的替代

`不支援`（硬體）／`可實作`（抽象成 Presence）

> **歸屬**：瀏海島本身正由 task `a0d852b4`（「瀏海島（Mac 原生）與它的跨平台對應」）實作，
> 它認領 `shell/darwin/NotchIsland.swift` 與 `docs/replica.md`。這一節只寫決策層的骨架；
> 實作細節以那一個 task 的結論為準，兩邊若不一致，以它為準並回來改這一節。

先把事實擺正：**瀏海只有有瀏海的 MacBook 有。** 外接螢幕、iMac、Mac mini、Mac Studio 全都沒有。
所以這不是「Mac 有、別人沒有」，是「某些 Mac 有」——它從一開始就需要一個後備，而那個後備
正好就是其他平台要的東西。舊版自己也說它是裝飾（`NotchIsland.swift`：「這一個是玩的」）。

#### 它到底在傳達什麼

拆開看，瀏海島只講三件事，而且三件都不是只有瀏海能講：

1. 現在有幾件事在跑；
2. 哪一個 session 在等你；
3. 一件長工做完了（那個舞）。

所以正確的做法**不是替瀏海找一個像瀏海的東西，是把這三件事抽成一個 port**——
暫名 `Presence`——然後每個平台各給一個實作：

| 實作 | 平台 | 怎麼呈現 |
|---|---|---|
| **通用 web 徽章（地板，永遠有）** | 全部 | 分頁標題前綴（`(2) Clawdline`）、動態 favicon、header 的 counts。**不需要任何原生 API** |
| 瀏海島 | 有瀏海的 MacBook | 現況 |
| 選單列標記 | macOS | 已有（`NSStatusItem`） |
| 工作列 overlay icon ＋ 進度 | Windows | `ITaskbarList3::SetOverlayIcon`（角標）＋ `SetProgressState/Value`（**跑東西時工作列按鈕變一條進度，這其實比瀏海更貼近「一眼看到在跑」**） |
| 系統匣圖示 | Windows | `Shell_NotifyIcon`，換圖示表示狀態 |
| StatusNotifierItem | Linux（KDE 與大部分桌面） | D-Bus，純 Go 可做 |

#### GNOME 的坑要先講

**GNOME 預設沒有系統匣。** 要靠使用者自己裝 AppIndicator 擴充。所以 Linux 上不能把系統匣當
成必然存在的東西——這正是「通用 web 徽章」必須是地板的理由：它在 GNOME、在 headless ＋
遠端瀏覽器、在任何地方都成立。

#### 建議

- **先做地板那一個**（分頁標題 ＋ favicon 徽章）。它是純前端，三平台一起有，而且它同時補上
  「使用者把 console 開在另一台機器的瀏覽器裡」這個情形——那是瀏海永遠碰不到的。
- Windows 的工作列進度排在 WebView2 殼之後，它很便宜而且效果好。
- Linux 的 StatusNotifierItem 排最後：要有殼，而 Linux 建議不做殼（§4.10）。

**代價**：Mac 之外沒有「那個好玩的東西」。這是真的，而且應該直說——瀏海島是這個 app 的個性，
不是功能，而個性沒辦法移植到一塊沒有瀏海的螢幕上。能移植的是它講的那三件事。

**修正總表**：無。

<!-- /section:notch -->

<!-- section:shell owner:task-1d861805 -->
### 4.10 原生殼與內嵌瀏覽器

`已實作`（macOS）／`可實作`（Windows）／`降級`（Linux：不做殼）

#### Linux：建議不做殼

WebKitGTK 要 cgo，而 cgo 會拆掉「一台機器產六個平台」。`plan.md` §3 本來就寫「WebKitGTK，
**或直接用瀏覽器**」——建議就是後者，而且 `clawdline open` 已經把它做完了：發一個唯讀裝置、
用 fragment 帶 token、`/v1/auth/adopt` 換成 cookie、轉到 `/`。

**代價要列清楚，這是一整排功能不存在**：

| 沒有殼就沒有 | 替代 |
|---|---|
| 全域熱鍵 | §4.8 的 compositor 捷徑 ＋ `clawdline panel` |
| 系統匣 | §4.9 的 web 徽章 |
| 本機桌面通知 | `notify-send` 可以由 daemon 直接發（D-Bus，不需要殼）——**這一項其實不需要殼** |
| 登入時啟動的 UI | `clawdline autostart` CLI（§4.7） |
| 內嵌瀏覽器（Cloud 預覽） | 使用者自己開一個分頁。**Linux 上這反而更乾淨**：cookie 本來就分家 |
| 麥克風權限的代為授權 | 瀏覽器自己會問，而且問得比較清楚 |

**結論：Linux 上「沒有殼」不是缺陷，是一個可以接受的形態**，只要上面這張表在設定頁上看得到。
真的要殼再說，而那時候第一個該做的是 systemd user service ＋ 瀏覽器，不是 GTK。

#### Windows：WebView2

`go-webview2`（純 Go，走 COM）而不是 `webview/webview_go`（要 cgo）。

- Runtime：Windows 11 內建；Windows 10 要裝 Evergreen Runtime。**安裝檔要偵測並代裝**，
  否則第一次啟動就是一個空白視窗。
- **兩個 webview、cookie 分家**（`replica.md` 的「內嵌瀏覽器」那一段）在 WebView2 上做得到：
  用兩個 `CoreWebView2Environment`，各自不同的 `userDataFolder`。這條規則的價值在 Windows 上
  一樣成立——本機 token 從來沒寫進去的那個罐子，頁面做什麼都交不出來。
- 上方那條原生列（網址框、上一頁、分頁按鈕）在 Windows 要重畫一次。顏色與間距同樣從
  `legacy/tokens.css` 取值，字詞同樣只能用舊版字串目錄裡已有的鍵。

#### macOS：不動

Swift 殼保持現況。它是唯一一個有 Keychain、瀏海、Carbon 熱鍵、`SMAppService` 的殼，
這些本來就是「少數 Mac 才有的特色」。

**修正總表**：無。

<!-- /section:shell -->

<!-- section:filesystem owner:task-1d861805 -->
### 4.11 儲存、路徑、檔案系統

`已實作`／`可實作`

#### SQLite

`modernc.org/sqlite` 是純 Go，三平台可用。兩個限制要寫成啟動時的檢查：

- **狀態目錄不能在網路磁碟。** SQLite 的 WAL 在 SMB／NFS 上的鎖定不可靠。Windows 上使用者的
  `%APPDATA%` 在企業環境可能是重導向到網路磁碟（folder redirection）的。**建議啟動時判斷
  磁碟型別，是網路磁碟就拒絕並說明**——這比事後查一個偶發的資料損毀便宜太多。
- Windows 的檔案鎖定語意與 POSIX 不同（`LockFileEx` 是強制鎖，POSIX 是勸告鎖）。
  同一個檔案被兩個 daemon 開著，Windows 上的症狀會跟 macOS 不同。

#### Windows 的路徑陷阱（`documents` 路由要補的檢查）

`internal/adapters/documents` 的檔頭已經把邊界寫得很清楚：「從裝置來的名字只能**在**一個
這段程式算出來的根目錄裡挑」。那套規則在 Windows 上要加幾條，因為 Win32 會在 Go 看到名字
**之後**再改寫它：

| 陷阱 | 為什麼危險 |
|---|---|
| `\` 與 `/` 都是分隔符 | 只擋 `/..` 擋不到 `\..` |
| ADS（`notes.md:hidden`） | 副檔名白名單看到的是 `.md`，實際開的是另一條資料流 |
| 8.3 短名（`PROGRA~1`） | 一個目錄有兩個名字，白名單比對會漏 |
| 尾隨的空白與點（`x.md.`、`x.md `） | Win32 開檔時會被剝掉，比對的名字和開的檔案不同 |
| 保留裝置名（`CON`、`NUL`、`COM1`…） | 開起來不是檔案 |
| 大小寫不敏感 | 比對要用不敏感的方式，否則白名單可以被繞過 |
| `\\?\` 與 UNC 前綴 | 繞過正規化 |
| reparse point（symlink／junction／hardlink） | `nofollow_other.go` 現在只剩 Lstat ＋ same-file 檢查 |

**建議做法**：Windows 上不要只做字串檢查。開檔之後用 `GetFinalPathNameByHandle` 拿到**核心
認定的真實路徑**，再確認它在根目錄底下。那是唯一不會被上面任何一條繞過的檢查，因為它問的是
已經開好的那個 handle。

#### 路徑長度

Windows 預設 `MAX_PATH` 260。worktree ＋ task id 的路徑很長（`…\worktrees\clawdline-go-xxxxxxxx\<uuid>\…`
就已經接近）。Go 的 `os` 對絕對路徑會自動補 `\\?\`，但不是所有情形，**要在真機驗證**（§7）。

#### 行尾與編碼

- transcript、contract、console 全部 UTF-8，不受影響。
- ConPTY 底下要 `SetConsoleOutputCP(CP_UTF8)`（§4.2）。
- git 的 `core.autocrlf` 在 Windows 上預設 true，`internal/adapters/git` 的 diff 解析要確認
  不會因為 `\r` 而錯位。

**修正總表**：無。

<!-- /section:filesystem -->

<!-- section:swiftstore owner:task-1d861805 -->
### 4.12 與舊 Swift app 互通：只有 macOS 有，而那是對的

`已實作`

`internal/adapters/swiftstore` 唯讀舊 app 的 `~/.config/clawdline`（Clawdfather 是誰、task 列表、
交付紀錄、session 標題、圖片、用量帳本）。**Linux 與 Windows 上舊 app 不存在，所以這一整塊
天生為空**，而且程式已經正確處理：

- `ObservabilityDirIn()` 在非 darwin 回 `""` → 用量頁退回 transcript 計算（已實作）。
- `ProcessStart()` 在非 darwin 回零時間 → 不會對上任何記錄的身分（`procstart_other.go` 的註解
  已經說明了）。
- 其餘讀取找不到檔案 → 依規則是「未知」，不是「空」。

**要注意的一個呈現問題**：在 Mac 上「皇冠、task chip、協調等待、交付勾」有，在 Linux／Windows
上沒有。使用者若在兩台機器之間切換，會以為 Linux 那台壞了。**建議**：這些元素的來源是舊 app
的 store，而那個 store 按 `plan.md` §4 是**過渡**——等 clawdline-go 自己擁有派工與交付紀錄，
這些欄位就會由自己的 store 供應，三平台一致。在那之前，Linux／Windows 上不要留下一個空位，
就是沒有那個元素（現況即如此）。

**修正總表**：無。

<!-- /section:swiftstore -->

<!-- section:fidelity owner:task-1d861805 -->
### 4.13 一個要先講清楚的驗收問題：跨平台不量 1:1

`降級`（驗收標準）

`replica.md` 的驗收方法是**同一個瀏覽器視窗、同一個縮放，比對 `getComputedStyle` 與
`getBoundingClientRect`**。這個方法在 Linux 與 Windows 上**一定會全紅**，而且不是因為程式錯了：

- 那台機器沒有 SF Pro／`-apple-system`，字型會 fallback 到別的字體；
- 字寬一變，每一個 `getBoundingClientRect` 都變；
- Linux 的字型渲染（freetype hinting、subpixel）與 macOS 不同；
- Windows 的 DPI 縮放是另一套。

**使用者在睡覺，所以這裡選一個安全的預設並寫下來，等他醒來可以推翻**：

> 跨平台的驗收標準是**結構與字串相同**，不是像素相同。也就是：同一份 DOM、同一組 class、
> 同一批字串鍵、同一批 `id`，以及**每一個「這台做不到」的地方都在畫面上具名地說出來**。
> 像素級的 1:1 只在 macOS 對舊版量。

理由：1:1 復刻的對象是**舊的 Swift app**，而舊 app 只有 macOS。在一台沒有舊 app 的機器上，
沒有可以比對的 oracle——那裡要驗的是別的東西。

**修正總表**：無。

<!-- /section:fidelity -->

---

## 5. 「這台做不到」要怎麼呈現：四條規則

這是整份文件裡最該被當成規範的一段。現有的程式其實已經在守它了（`terminal/errors.go` 的
`Unsupported`、`ps_windows.go` 的 `Complete:false`、`signal_other.go` 回 nil 而不是 stub、
`pasteboard_other.go` 的 `ErrPasteboardUnsupported`、`whisper` 把「沒裝」與「沒模型」分開），
把它們寫成四條，後面的人照著做：

1. **具名地拒絕，不要靜默。** 每一個平台差異都回一個有型別的錯誤，錯誤裡說**這台機器**為什麼
   做不到。`terminal.Unsupported{Op:…}` 是既有的範本。
2. **「不知道」和「沒有」是兩件事，不可以拼成同一個答案。**
   `ps_windows.go` 回 `Complete:false` 而不是空清單，是這條規則最好的示範：空清單是在宣稱
   「沒有東西在跑」，而它沒有資格這樣宣稱。
3. **停用，不要移除。** 一個看得見但停用、而且說得出理由的按鈕，比一個消失的按鈕好。
   使用者對照 Mac 上的截圖時，看到「這台機器沒有 iTerm2」會懂；看到那一項不見了，只會困惑。
   （`replica.md` 對沒有後端的項目已經是這個做法。）
4. **降級要在兩個地方講**：使用者看得到的畫面上（設定頁 ／ 那個功能的旁邊），以及
   `/v1/health` 的欄位（讓 CLI、監控與寫程式的人也拿得到）。
   建議 `/v1/health` 加一塊 `capabilities`，列出這台機器上每一個平台相關的能力與它的狀態，
   內容就是 §3 那張總表在**這一台**上的答案。設定頁直接渲染它，不要自己再判斷一次。

---

## 6. 難度與建議順序

分三梯。每一梯的驗收都是 §7 的對應層。

### 第一梯：讓 Linux 真的能用（估 1–2 週）

Linux 是離「可用」最近的，因為 tmux、pty、FIFO、procfs 全都有。

| # | 項目 | 難度 | 為什麼排這裡 |
|---|---|---|---|
| 1 | `/v1/health` 的 `capabilities` 區塊 ＋ 設定頁渲染它 | S | 後面每一項降級都要靠它呈現，先做它，後面每一項就只是填一格 |
| 2 | `clawdline autostart` ＋ systemd user unit ＋ linger 的說明 | M | 沒有它，daemon 一登出就死，其他全都白做 |
| 3 | procfs 版的行程盤點（取代 `/bin/ps`） | S | 精簡容器沒有 `ps`；`/proc` 也更準 |
| 4 | Secret Service ＋ 檔案退路 ＋ 降級的呈現 | M | 為 Cloud 鋪路；沒有桌面就誠實用檔案 |
| 5 | `clawdline panel` ＋ Wayland 熱鍵的降級 UI | M | Wayland 是現在 Linux 桌面的預設 |
| 6 | whisper 的安裝指引與路徑印出 ＋ `install-model` | S | 純文案 ＋ 一個下載，效益高 |
| 7 | 通用 web 徽章（分頁標題 ＋ favicon） | S | 三平台一起受益，而且它是 Presence 的地板 |
| 8 | 本機桌面通知（`org.freedesktop.Notifications`） | S | 不需要殼，D-Bus 直接發 |

### 第二梯：讓 Windows 能用（估 3–5 週）

| # | 項目 | 難度 | 備註 |
|---|---|---|---|
| 1 | **WSL 模式的文件與偵測** | S | **工作量幾乎為零，卻直接讓 Windows 使用者有完整功能。應該第一個做。** |
| 2 | NTFS ACL ＋ DPAPI | M | **blocking**：在這之前 Windows 版不該對外發布（§4.5） |
| 3 | ConPTY ＋ VT 解析器（owned session） | X | 最大的一塊。做完，Linux 的 owned session 也一起有 |
| 4 | Job Object（`supervisor_windows.go`） | M | 跟 3 綁在一起：不能砍的樹就不該開 |
| 5 | Win32／WMI 行程盤點（只看得見，不能控） | M | 讓既有 session 至少出現在清單上 |
| 6 | `documents` 路由的 Windows 路徑檢查 | M | 安全項，跟 2 一起審 |
| 7 | WebView2 殼（含兩個 environment 的 cookie 分家） | L | |
| 8 | `RegisterHotKey`、`HKCU\Run`、Toast、工作列徽章 | M | 都要先有殼 |

### 第三梯：加值

- 純 Go 的 WebP／BMP 解碼（三平台一起，順便讓 macOS 少叫一次 `sips`）。
- Linux StatusNotifierItem（要先有殼，所以可能永遠不做）。
- Windows 剪貼簿借用（若真的要做，先做 Windows，Linux 不做，§4.3）。
- macOS Keychain（若決定做，走 Swift 殼代管，不開 cgo）。

---

## 7. 要驗證什麼：AWS 的 Linux 與 Windows 機器上的最小檢查

**這一節是給實測用的。** 每一項寫成「跑什麼 → 看到什麼才算過」。沒跑到的項目不可以當成通過；
跑了但結果不同，記下來比宣稱通過有價值。

建議機型：Linux 一台 **有桌面**（GNOME on Wayland）＋ 一台 **headless**（Amazon Linux／Ubuntu
Server，SSH 進去）；Windows 一台 **Windows Server 2022 或 Windows 11**（要有桌面，因為殼與
Toast 都需要）。**Linux 兩台是必要的，不是保險**——headless 與桌面的差別正好落在熱鍵、
keystore、通知、autostart 這四項上，而那四項是這份文件裡最多不確定的地方。

### L0 — 它活不活得下來（兩台都要）

| 檢查 | 過的條件 |
|---|---|
| `clawdline doctor` | 正確解析狀態目錄（Linux `~/.config/clawdline-next`、Windows `%APPDATA%\clawdline-next`） |
| `clawdline serve` | 綁得上 127.0.0.1:7727，log 沒有 panic |
| `GET /v1/health` | 200，`scheduler` 的 `considered`／`due`／`fired` 有值 |
| SQLite | 狀態目錄裡建得出 db；**殺掉 daemon 再起來，事件還在** |
| 重開機 | daemon 自己回來（做完 §4.7 之後） |
| 長路徑（Windows） | 在一個接近 260 字元的路徑底下跑 `serve`，不炸 |
| 網路磁碟（Windows，可選） | `%APPDATA%` 被重導向時，啟動要拒絕並說明（做完檢查之後） |

### L1 — 認證閘門（兩台都要）

| 檢查 | 過的條件 |
|---|---|
| 不帶 token 打 `/v1/sessions` | `401 unauthorized`，訊息是舊版原文 |
| 帶 `local-token` | 200 |
| `clawdline open` | 有桌面：真的開起瀏覽器並進到 `/`。headless：`--print` 印出網址 |
| `fetch('/v1/auth/logout')` | cookie 清掉，裝置也撤銷 |
| **檔案權限** | Linux：`stat -c %a ~/.config/clawdline-next/local-token` ＝ `600`，目錄 `700`。**Windows：`icacls` 只列出目前使用者**（做完 §4.5 之前這一項會紅，那個紅是預期的，要記下來） |
| symlink | 把 `local-token` 換成一個 symlink，daemon 要拒絕，不是跟著走 |

### L2 — Session（Linux 為主）

| 檢查 | 過的條件 |
|---|---|
| `tmux new-session -d` 後盤點 | `/v1/sessions` 列出那個 pane，tty／cwd／assistant 正確 |
| 沒有 tmux server 時盤點 | 空清單且 `Complete:true`（**這是「權威的空」，跟讀不到不同**） |
| tmux 不存在時盤點 | note 寫「tmux is not installed」，`Complete:true` |
| 開一個**可拋棄的** claude session，送一句話 | 畫面上出現那句話 |
| 讀畫面 | `/screen` 回得到內容，中文不是亂碼 |
| 送鍵（`/key`） | 用 AskUserQuestion 問一個無害問題，按選項，選到的是按的那一個 |
| 中斷、關閉 | pane 真的停 ／ 真的消失 |
| focus | 回 `ok`，而且**文件與 UI 沒有宣稱視窗會跳到前面**（Wayland 上不會） |
| 砍整棵樹 | 對照組：有 `Setpgid` 時孤兒 0 個 |
| **Windows** | WSL 模式：上面每一項在 WSL 裡重跑一遍。原生模式：owned session 的開／送／讀／中斷／關（做完 ConPTY 之後） |
| **Windows 的既有 session** | 手動在 Windows Terminal 開一個 claude，清單上要**出現**並標示「無法遙控」（做完 §4.1(c) 之後） |

### L3 — 每一個降級都看得見（兩台都要，這一層最重要）

對每一項：**打對應的 API，看它回的是具名錯誤；打開頁面，看那個地方有沒有說出理由。**

| 功能 | Linux 應有的答案 | Windows 應有的答案 |
|---|---|---|
| 送圖給 assistant | `ErrPasteboardUnsupported` → 改交路徑，且畫面上說了 | 同左 |
| HEIC 圖片 | `unsupported_image`，訊息說得出是哪一種格式 | 同左 |
| iTerm2 相關 | 不出現在 backend 清單 | 同左 |
| 即時畫面 | tmux 有 → `live`；無 tmux → `on-demand` | `on-demand`，且沒有假裝 |
| 行程盤點 | 完整 | `Complete:false` ＋ note |
| supervisor | 正常 | `supervisor_unsupported`（在補上 Job Object 之前） |
| 舊 app 的 store | 皇冠／交付勾就是沒有，不留空位 | 同左 |
| 用量頁 | 退回 transcript，頁面上說明了數字的來源不同 | 同左 |
| 全域熱鍵 | Wayland：設定頁顯示 compositor 捷徑的說明，不是一個錄不到的框 | `RegisterHotKey` 被佔用時要具名 |
| 秘密儲存 | 設定頁說出「keyring」還是「檔案」 | 說出「DPAPI」還是「檔案」 |
| 通知 | 三個出口各自的狀態都列出來 | 同左，且 Toast 真的看得到（AUMID 的坑） |

### L4 — 選配元件（兩台都要）

| 檢查 | 過的條件 |
|---|---|
| whisper 三態 | 沒裝 → `ErrNoBinary`；裝了沒模型 → `ErrNoModel`；都有 → 真的轉錄出已知內容 |
| 錄音的 secure context | 從 `127.0.0.1` 可以；從區網 IP 要說出真正的理由 |
| git 面板 | 在一個**可拋棄的** repo 上看得到變更；Windows 上 `\r` 不會讓 diff 錯位 |
| cloudflared | 沒裝時具名拒絕；裝了能起（可選） |

### L5 — 前端（兩台都要）

| 檢查 | 過的條件 |
|---|---|
| console 載入 | 該平台的預設瀏覽器裡，`booting=false`、清單畫得出來 |
| SSE | 放著十分鐘不斷線；斷了會重連 |
| 版面 | **不量像素**（§4.13）。量的是：同一組 `id` 都在、沒有溢出、沒有水平捲軸、760 寬與桌面寬都能用 |
| 字型 | fallback 之後中文還讀得出來，沒有方塊 |

### 記錄方式

每一項記「跑了／沒跑／結果」三態，**沒跑的要留在清單上**。這份文件的價值有一半在於：
下一次有人問「Windows 到底能不能用」時，答案是一張有勾有叉有空格的表，不是一句印象。

---

## 8. 我不確定、要在真機確認的事

這一節是誠實的邊界。以下每一條都影響上面的建議，但**我沒有在真的機器上驗過**：

1. **GNOME 到底有沒有實作 `org.freedesktop.portal.GlobalShortcuts`。** 我的認知是長期沒有，
   但這會變。驗法：在那台桌面 Linux 上跑
   `busctl --user introspect org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop | grep -i GlobalShortcuts`。
   有的話，§4.8 的建議要加上 portal 那條路。
2. **whisper.cpp 在 scoop／winget 上有沒有套件。** §4.4 的 Windows 那一列寫的是「官方 release
   的預編 zip」，如果有套件管理員的版本，指引要改。
3. **Go 在 Windows 上對超過 `MAX_PATH` 的處理涵蓋到哪。** Go 會對絕對路徑自動補 `\\?\`，
   但不是每一條路徑操作都會。要在接近 260 字元的路徑底下實跑（L0）。
4. **`modernc.org/sqlite` 在 Windows 上的 WAL 行為。** 純 Go 應該沒問題，但沒跑過。
5. **ConPTY 的輸出流在 Claude Code 的 TUI 底下實際長什麼樣。** VT 解析器要跟著它的重繪方式走，
   而那要真的接上去看一次才知道成本。舊版在 tmux 上量過（349 行、100% 精確度），ConPTY 不一定相同。
6. **WSL2 的 localhost 轉發在使用者的網路設定下是否穩定。** 有些企業 VPN 會擋。
7. **Windows Toast 在沒有開始功能表捷徑時到底會不會顯示。** 我的認知是不會且不報錯，要實測。
8. **Linux 桌面上 Secret Service 在 SSH ＋ `DISPLAY` 轉發的情形下會不會卡住等解鎖。** 會卡住的話，
   要有 timeout 而不是讓啟動吊死。

---

## 附：這份文件依據的原始碼位置

| 主題 | 檔案 |
|---|---|
| 平台分檔的全貌 | `grep -rn '^//go:build' internal/ shell/` |
| 終端機 backend 清單 | `internal/adapters/terminal/hosts_darwin.go`、`hosts_other.go` |
| tmux | `internal/adapters/terminal/tmux.go`、`screen.go`、`keys.go`、`type.go`、`launch.go` |
| iTerm2 | `internal/adapters/terminal/iterm_*.go` |
| 即時畫面訊號 | `internal/adapters/terminal/signal_unix.go`、`signal_other.go` |
| 在畫面上顯示 | `internal/adapters/terminal/reveal.go`、`reveal_darwin.go`、`reveal_other.go` |
| 行程盤點 | `internal/adapters/process/ps_unix.go`、`ps_windows.go` |
| 行程樹 | `internal/adapters/supervisor/supervisor_unix.go`、`supervisor_windows.go` |
| 剪貼簿與圖片解碼 | `internal/adapters/artifacts/pasteboard_*.go`、`decode_*.go` |
| 語音 | `internal/adapters/whisper/whisper.go`（`binaryDirs()`、`modelDirs()`） |
| 秘密 | `internal/adapters/cloudkeys/`、`internal/adapters/devices/`、`nofollow_*.go` |
| 路徑與狀態目錄 | `internal/config/config.go` |
| 文件路由的邊界 | `internal/adapters/documents/documents.go` |
| 舊 app 的 store | `internal/adapters/swiftstore/`（`usagedb.go` 的 `ObservabilityDirIn`、`procstart_*.go`） |
| 開瀏覽器 | `cmd/clawdline/auth.go` 的 `openURL` |
| macOS 原生殼 | `shell/darwin/`（`HotKey.swift`、`main.swift` 的 `SMAppService`、`Browser.swift`） |
| 舊 Swift app（唯讀參考） | `~/code/clawdline/Sources/`：`LiveScreen.swift`、`WebPush.swift`、`NotchIsland.swift`、`Whisper.swift`、`CloudKeys.swift`、`Targets.swift` |
