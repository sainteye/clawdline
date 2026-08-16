<div align="center">

# Clawdline

**把 Claude Code 的輸入行，放到眼睛的高度。**

一條浮在螢幕中上方、Spotlight 風格的輸入條。打完字按 Enter，內容直接進到 Claude Code
session；按 <kbd>⌘</kbd><kbd>J</kbd>，那個 session 就在同一個地方讀得回來——是**排版過的**，
不是刮畫面。從此不必再盯著終端機的角落。iTerm2 直接支援，其餘終端機透過 tmux。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/macOS-13%2B-black.svg)](#安裝)
[![Swift](https://img.shields.io/badge/Swift-5-orange.svg)](Sources)
[![Dependencies](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](#安裝)

[English](README.md) · 繁體中文

<img src="docs/assets/demo.gif" width="760" alt="按 ⌥Space、打字、按 Enter，訊息就進到 Claude Code">

<img src="docs/assets/transcript.png" width="760" alt="同一條輸入條展開逐字稿：標題、有框線的表格、程式碼區塊，是排版過的而不是刮畫面">

</div>

---

## 為什麼有這個東西

Claude Code 的輸入框畫在終端機的最下緣，而終端機通常是滿版的。於是那個你一天要看幾百次的
東西，長期釘在螢幕的**左下角**——離視線落點最遠的那個位置。

這件事沒有設定可以改。輸入框固定在畫面底部，plugin 也動不到：plugin 提供的是 command、
agent、hook、MCP server 與 skill，不是 TUI 版面。

所以 Clawdline 換一個方向：不動終端機，另外給你一個打字的地方。它出現在你指定的高度，
接住你的字，送進你剛才在用的那個 session，然後把焦點還給你原本的 app。視線不必移動。

## 安裝

四種擇一，結果都是同一個 app，挑你最信得過的那條。

**Homebrew**

```bash
brew install --cask sainteye/tap/clawdline
xattr -dr com.apple.quarantine /Applications/Clawdline.app
```

**用腳本抓最新版**

```bash
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh -o install.sh
less install.sh          # 四十行，值得花十秒看一遍
bash install.sh          # 或 bash install.sh ~/Applications
```

**手動** —— 從 [Releases](https://github.com/sainteye/clawdline/releases/latest) 下載 `.zip`，
解開丟進 `/Applications`，然後：

```bash
xattr -dr com.apple.quarantine /Applications/Clawdline.app
```

**自己編** —— 沒有套件管理器、沒有相依套件，幾秒鐘：

```bash
git clone https://github.com/sainteye/clawdline.git
cd clawdline && ./build.sh
open ~/Applications/Clawdline.app
```

<details>
<summary>那行 <code>xattr</code> 是幹嘛的，以及為什麼自己編就不用</summary>

release 的 build 有 ad-hoc 簽章但**沒有公證**——公證需要付費的 Apple Developer 帳號。
macOS 會把從網路下載的東西加上隔離屬性，並拒絕開啟沒公證的副本，所以那個屬性得拿掉，
不然就要走系統設定 › 隱私權與安全性 › 強制打開。

自己編出來的 app 從來沒被下載過，所以不會被隔離。如果你不想憑信任執行陌生人給的二進位檔，
那就是該選的那條：整個 build 就是 `swiftc` 跑過幾個你讀得完的檔案。

</details>

第一次送出時，macOS 會問要不要讓 Clawdline 控制 iTerm2，按**好**；沒有這個權限它什麼都送不出去。
選單列的 ✳ → **開機時啟動** 可以讓它常駐。

## 用法

在 iTerm2 裡按 <kbd>⌥</kbd><kbd>Space</kbd>，打字，按 <kbd>Enter</kbd>。

| 按鍵 | 做什麼 |
|---|---|
| <kbd>⌥</kbd><kbd>Space</kbd> | 叫出／收起輸入條 |
| <kbd>Enter</kbd> | 送到目前的目標分頁 |
| <kbd>⇧</kbd><kbd>Enter</kbd> | 換行（輸入條會跟著長高） |
| <kbd>Tab</kbd> / <kbd>⇧</kbd><kbd>Tab</kbd> | 下一個／上一個 Claude Code 分頁 |
| <kbd>⌘</kbd><kbd>K</kbd> | 展開 session 清單 |
| <kbd>⌘</kbd><kbd>1</kbd>…<kbd>⌘</kbd><kbd>9</kbd> | 直接跳到第 N 個 |
| <kbd>↑</kbd> / <kbd>↓</kbd> | 輸入框空的時候翻歷史 |
| <kbd>⌘</kbd><kbd>J</kbd> | 看那個分頁現在說了什麼 |
| <kbd>⌘</kbd><kbd>F</kbd> | 把它撐滿整個螢幕 |
| <kbd>⌘</kbd><kbd>R</kbd> | 最新的訊息放最上面，而不是最下面 |
| <kbd>⌘</kbd><kbd>+</kbd> / <kbd>⌘</kbd><kbd>−</kbd> / <kbd>⌘</kbd><kbd>0</kbd> | 那塊的字級，會記住 |
| <kbd>⌘</kbd><kbd>M</kbd> | 瀏覽／切換吉祥物 |
| <kbd>⌘</kbd><kbd>D</kbd> | 叫吉祥物跳舞 |
| <kbd>Esc</kbd> | 關掉 |

<kbd>⌘</kbd><kbd>A</kbd> <kbd>⌘</kbd><kbd>C</kbd> <kbd>⌘</kbd><kbd>V</kbd> <kbd>⌘</kbd><kbd>X</kbd>
<kbd>⌘</kbd><kbd>Z</kbd> 都照你想的運作。

**熱鍵只在 iTerm2 在前景時生效。** 在其他 app 裡，<kbd>⌥</kbd><kbd>Space</kbd> 還是你裝這個
之前的那顆鍵。把 `"scope_app"` 設成 `""` 就變全域。

### 它會送到哪個分頁

Clawdline 列出所有 iTerm2 session，用 `ps` 比對每個的 TTY，留下真正在跑 `claude` 的那些，
預設選你最後停留的那一個。

輸入條下緣一直寫著目標是誰。**它不做盲送**——一個不肯告訴你字會跑去哪的輸入框，比沒有更糟。

### 看那個分頁在說什麼

<kbd>⌘</kbd><kbd>J</kbd> 會在輸入框下面展開一塊，顯示那個 session 現在的畫面，約一秒更新一次，
按 <kbd>Tab</kbd> 換分頁時也跟著換。展開時整個螢幕背景會模糊——讀一段輸出跟丟一行指令
是兩種模式。裡面的字可以選取，畢竟把錯誤訊息複製出來是能看到它的主要理由。

**只有你原本就停在「新的會出現的那一端」時才自動跟著捲**：往上翻在讀東西的時候被拉走，比不跟還糟。
而且終端機沒有變動時 capture 出來的字串一模一樣，那一輪會整個跳過，不會在你眼前重排。

能讀到的時候，那塊顯示的是**對話**而不是畫面。Claude Code 會邊跑邊把每個 session 寫進
`~/.claude/projects/<專案>/<session>.jsonl`，那份檔案有畫面只能暗示的結構：誰說的、
說了什麼、跑了哪些工具。讀它的好處是有真正的訊息邊界、看得到完整歷史而不只是一屏，
而且可以用**排版**而不是截圖來呈現——說話者有標籤、內文用比例字、工具呼叫退到頁邊的等寬字裡。

<kbd>⌘</kbd><kbd>F</kbd> 把它變成螢幕的大小——不是 macOS 的全螢幕（那個會把視窗丟到自己的
Space，對一個蓋在工作上的面板來說正好相反）。它就是一次縮放，有動畫，吉祥物也有一段對應的動作。

<div align="center">
<img src="docs/assets/fullscreen.png" width="860" alt="⌘F：同一塊撐滿螢幕，跑完的工具呼叫各收成一行">
</div>

<kbd>⌘</kbd><kbd>R</kbd> 把整份倒過來，最新的訊息在最上面。自動跟隨也跟著換邊
（往上捲到頂而不是往下到底），而且會記住。**只有逐字稿會翻**：終端機畫面是一格一格的
截圖，把它的行倒過來會讓一句折行的話變成由下往上讀。

那個 session 正在跑的時候，面板上方會有一行寫著它在做什麼——
`Finagling… (5m 52s · ↓ 18.6k tokens)`。**這一行是從終端機刮下來的**，即使其他內容都來自檔案：
因為它從來不會被寫進檔案。逐字稿記的是「已經存在的訊息」，而這是畫在畫面上、又被擦掉的東西。

工具呼叫會折疊起來。一則回答底下常常壓著三十行路徑與 shell，而那不是你回頭要看的東西——
所以每一組跑完的工具收成一行，寫著花了幾個動作、用了哪些工具，點一下就展開。
**還在跑的那一組永遠不折**：那一組正好是會變的部分。

Claude 寫出來的是 Markdown，所以那塊會把它渲染出來：標題、清單、**有框線的表格**、引用、
強調、程式碼。表格留著直線交給等寬字型是行不通的——中日文字的字形來自 fallback，
它的字寬不保證剛好是等寬字的兩倍，所以在原始碼裡對齊的直線，畫到畫面上每一行都落在不同位置。
認不得的東西一律當純文字放行，因為那是唯一真正要緊的失敗方式：**畫面上多一個星號只是難看，
被剖析器吃掉一句話才是 bug。**

要找到對的那一份要三步，因為沒有任何一筆紀錄帶著 tty：用工作目錄找到專案資料夾、
用分頁標題比對 transcript 記下的 `aiTitle`、平手時取最近寫入的那個。
**那個格式沒有文件而且會變**，所以每個欄位進來時都是可有可無，認不得的一律跳過。

沒有 transcript 的時候（純 shell、非 Claude 的 pane）就退回刮終端機畫面。
iTerm2 只交得出**目前可見的畫面**，它的 scripting 沒有 scrollback；tmux 則是可見畫面加 200 行歷史。
把 `output_mode` 設成 `terminal` 或 `transcript` 可以固定用哪一種。

**在那條退路上，顏色只有走 tmux 才有。** `capture-pane -e` 會保留跳脫序列，那些會被解析成
真的顏色。iTerm2 的 scripting 回傳的是純字串——它告訴得了你「ANSI 紅是哪個紅」，
但告訴不了你「哪幾個字是紅的」，所以那條路拿到的是無色的文字。
這一段跟逐字稿無關：那邊的顏色來自**文字的意思**，不是終端機畫了什麼。

`output_font` 設成你終端機用的那個字型。預設是 Menlo；用方框字元拼出來的狀態列，
在字寬不同的字型下會整排跑掉——那就是它看起來壞掉的原因。

## 換上你自己的吉祥物

輸入條上那隻是**資料，不是程式**。一份 JSON 檔裝著像素網格、色盤、每一個姿勢與每一段動畫，
所以換一隻完全不必 fork。

```
~/.config/clawdline/mascots/clawd.json
```

改完按 <kbd>⌥</kbd><kbd>Space</kbd> 就看得到，不用重新編譯。

### 瀏覽與快速切換

<div align="center">
<img src="docs/assets/picker.png" width="620" alt="吉祥物挑選器，列出 clawd 與 mochi">
</div>

<kbd>⌘</kbd><kbd>M</kbd> 列出你有的每一份 pack，上下鍵**邊移動邊預覽**——清單還開著，
輸入條上那隻就已經換了，所以你是用看的挑，不是用讀名字挑。
<kbd>⌘</kbd><kbd>1</kbd>–<kbd>⌘</kbd><kbd>9</kbd> 直接跳，選單列的 ✳ 也有同一份清單。

內建兩隻，更多在 [**docs/gallery.md**](docs/gallery.md)：

<div align="center">
<img src="docs/assets/dance.gif" width="420" alt="clawd">
<img src="docs/assets/mochi-dance.gif" width="420" alt="mochi">
</div>

### 自己做一隻

做這件事的方式是交給 Claude Code。存一張參考圖，然後說：

> 這是參考圖：`~/Downloads/my-character.gif`
>
> 幫我做成 Clawdline 的 mascot pack。格式看 `docs/mascots.md`，
> `~/.config/clawdline/mascots/clawd.json` 是可以照抄的範例。網格不要超過 20×16，
> 腳要放在最下面那一列才站得住。六段動畫都要寫：`pop`、`idle`、`typing`、`dance`、`cheer`、`stretch`。
> 存成 `~/.config/clawdline/mascots/my-character.json`，並把設定檔指過去。
>
> 然後驗收你自己的成果：跑
> `open "clawdline://snapshot?path=/tmp/m.png&routine=dance&t=0.3"`，看那張 PNG，
> 哪裡不對就改，改到看起來像參考圖為止。

**最後那段才是關鍵。** `clawdline://snapshot` 會把任何一段動畫的某一格畫成 PNG，
而且**不需要螢幕錄製權限**，所以 agent 看得到自己畫了什麼、可以自己迭代。
沒看過就寫的像素圖，出來會是一團。

完整格式、六段動畫的觸發時機、以及這個尺寸下什麼畫得出來：**[docs/mascots.md](docs/mascots.md)**。
pack 是純資料——一堆字元、顏色與數字——執行不了任何東西，最壞就是載入失敗並說明原因。
`tools/validate-pack.py` 可以驗，CI 在每個 PR 上都會跑。

## 它是怎麼把字送進去的

不是模擬鍵盤，也不是寫進終端機的 pty——現代 macOS 上你寫不進別的行程的 TTY。
走的是 iTerm2 的 scripting 介面，並且包在括號貼上（bracketed paste）裡：

```
ESC[200~ 你的文字，含換行 ESC[201~     ← 當成一次貼上，不是連按好幾次 Enter
CR                                     ← 再單獨送一個 Return 才送出
```

少了那層包裝，兩行的 prompt 會在第一行就自己送出去。另一個好處是
**終端機完全不必被叫到前景**——那正是這整個工具的重點。

## 設定

`~/.config/clawdline/config.json`。選單列 ✳ → **重新載入設定** 套用。

```jsonc
{
  "hotkey": "option+space",              // cmd / option / control / shift ＋ 一個鍵
  "scope_app": "com.googlecode.iterm2",  // 逗號分隔多個；"" ＝ 全域生效
  "y_fraction": 0.30,                    // 輸入條上緣落在螢幕高度的幾成，0 ＝ 最上面
  "width": 720,
  "language": "auto",                    // auto | en | zh-Hant
  "mascot": "clawd",
  "tmux_path": ""                        // 空的 ＝ 去常見位置找,
  "output_height": 340                    // ⌘J 那塊的高度，80–900
  "output_font": "Menlo",                // 配合你的終端機，不然方框字元會跑掉
  "output_mode": "auto",                 // auto | transcript | terminal
  "output_size": 11.5,                   // ⌘+ / ⌘− 會直接改這個值
  "output_newest_first": false,          // ⌘R：最新的在最上面
  "backdrop": 0.5,                       // ⌘J 的背景模糊，0 ＝ 不要
}
```

加一個語言＝在 [`Sources/Strings.swift`](Sources/Strings.swift) 寫一個 struct、
在 catalog 加一行。編譯器會拒絕編譯少了字串的語言，所以翻譯不可能安靜地只做一半。歡迎 PR。

## 權限與隱私

| 要什麼 | 為什麼 |
|---|---|
| **自動化 → iTerm2** | 把文字放進 session 的唯一方式。只在第一次送出時問一次。 |
| *（沒有其他）* | 不要輔助使用、不要螢幕錄製、不連網。 |

全域熱鍵刻意走 Carbon 的 `RegisterEventHotKey` 而不是 `NSEvent` monitor，
就是為了**避開輔助使用權限**——一個只是要開輸入框的工具，沒有理由拿到「看見你每一次按鍵」的能力。

Clawdline 只跟你自己機器上的 iTerm2 說話。歷史紀錄存在
`~/.config/clawdline/config.json`，不會去任何地方。

## 其他終端機：把 Claude Code 跑在 tmux 裡

iTerm2 直接支援。其餘全部——Terminal.app、Warp、Tabby、Ghostty、Alacritty、Kitty——
只要 Claude Code 跑在 **tmux** 裡就能用：

```bash
tmux new -s work
claude
```

設定就這樣。Clawdline 會把 tmux 的 pane 跟 iTerm2 的 session 列在同一份清單，
用同樣的方式認出哪些在跑 `claude`，並透過 `load-buffer` ＋ `paste-buffer`
套同一層括號貼上送進去。而且 **tmux 完全不需要任何 macOS 權限**——
它是普通的子行程，不是跨 app 自動化。

如果你的終端機不是 iTerm2，記得把熱鍵範圍放寬：

```jsonc
{ "scope_app": "com.apple.Terminal,com.googlecode.iterm2" }
```

<details>
<summary>為什麼不直接支援那些終端機？</summary>

因為它們收不到文字。Terminal.app 有 `do script`，聽起來應該可以——實測不行：
一個在它的分頁裡卡在 `read` 的程式，從頭到尾一個位元組都沒收到，而那個呼叫回報成功。
Warp 與 Tabby 連等價的介面都沒有。

剩下的路只有模擬鍵盤，那需要輔助使用權限——一個工作只是開一個輸入框的工具，
去要「看見你每一次按鍵」的能力——而且必須把終端機叫到前景，那正是這個工具要避開的事。
tmux 兩個代價都不必付，結果一樣。

</details>

## 限制

- **單向。** Claude 的回覆還是在終端機裡。不過那半本來就是往上捲的；
  這個工具修的是被釘在左下角的那一半。
- **非 iTerm2 的終端機需要 tmux**，見上一節。

## 出事的時候

App 做的每一件事都寫進 `~/Library/Logs/Clawdline.log`：熱鍵有沒有註冊成功、
面板有沒有顯示、每一次送出的結果。

- **按 ⌥Space 沒反應**——看 log 有沒有 `hotkey registered`。沒有就是被別的 app 佔走了，換一個。
- **顯示「No Claude Code session found」**——多半是自動化權限被拒。跑
  `tccutil reset AppleEvents dev.sainteye.clawdline` 之後重開，讓它重新問一次。
- **送出失敗**——面板會自己跳回來，你打的字還在，下緣寫著原因。它不會吃掉你的字。

## 參與開發

純 AppKit、沒有框架、除了 `swiftc` 沒有 build 系統。

```bash
./test.sh     # 84 個檢查，約兩秒
./build.sh    # 編譯，原本有在跑的話會自己接回來
```

測試涵蓋的是「改動會安靜弄壞」的那幾塊：pack 解碼與驗證、keyframe 取樣、顏色解析、
熱鍵字串，以及決定「字會送去哪」的兩個解析器（`ps` 輸出與 `tmux list-panes`）。
需要畫面上有視窗才能測的東西刻意沒寫——**跑不了 CI 的測試就是沒有人會跑的測試**。

註解寫的是**為什麼是這樣**，特別是那些「先試了顯而易見的做法然後失敗」的地方，
那些才是有價值的部分，請保持這個習慣。

## 出處說明

吉祥物是 Claude Code 裡那隻像素角色的同人畫，社群叫牠 **Clawd**。
本專案與 Anthropic 無任何關聯，未經其背書或授權。Claude 與 Claude Code 是 Anthropic 的商標。

吉祥物之所以做成可替換的 JSON，正是為了讓你換成自己的——見 [docs/mascots.md](docs/mascots.md)。

## 授權

[MIT](LICENSE)
