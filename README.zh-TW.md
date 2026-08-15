<div align="center">

# Clawdline

**把 Claude Code 的輸入行，放到眼睛的高度。**

一條浮在螢幕中上方、Spotlight 風格的輸入條。打完字按 Enter，內容直接進到 iTerm2 裡的
Claude Code session——全程不必把視線移到終端機。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/macOS-13%2B-black.svg)](#安裝)
[![Swift](https://img.shields.io/badge/Swift-5-orange.svg)](Sources)
[![Dependencies](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](#安裝)

[English](README.md) · 繁體中文

<img src="docs/assets/demo.gif" width="760" alt="按 ⌥Space、打字、按 Enter，訊息就進到 Claude Code">

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

```bash
git clone https://github.com/sainteye/clawdline.git
cd clawdline && ./build.sh
open ~/Applications/Clawdline.app
```

沒有套件管理器、沒有相依套件——幾個 Swift 檔加一個 JavaScript 檔，`swiftc` 直接編成
app bundle。需要 Xcode command line tools。

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

## 換上你自己的吉祥物

<div align="center">
<img src="docs/assets/dance.gif" width="520" alt="吉祥物在輸入條上跳舞">
</div>

輸入條上那隻是**資料，不是程式**。一份 JSON 檔裝著像素網格、色盤、每一個姿勢與每一段動畫，
所以換一隻完全不必 fork。

```
~/.config/clawdline/mascots/clawd.json
```

改完按 <kbd>⌥</kbd><kbd>Space</kbd> 就看得到，不用重新編譯。

設計上，做這件事的方式是交給 Claude Code。存一張參考圖，然後說：

> 這是參考圖：`~/Downloads/chiikawa.gif`
>
> 幫我做成 Clawdline 的 mascot pack。格式看 `docs/mascots.md`，
> `~/.config/clawdline/mascots/clawd.json` 是可以照抄的範例。網格不要超過 20×16，
> 腳要放在最下面那一列才站得住。五段動畫都要寫：`pop`、`idle`、`typing`、`dance`、`cheer`。
> 存成 `~/.config/clawdline/mascots/chiikawa.json`，並把設定檔指過去。
>
> 然後驗收你自己的成果：跑
> `open "clawdline://snapshot?path=/tmp/m.png&routine=dance&t=0.3"`，看那張 PNG，
> 哪裡不對就改，改到看起來像參考圖為止。

**最後那段才是關鍵。** `clawdline://snapshot` 會把任何一段動畫的某一格畫成 PNG，
而且**不需要螢幕錄製權限**，所以 agent 看得到自己畫了什麼、可以自己迭代。
沒看過就寫的像素圖，出來會是一團。

完整格式、五段動畫的觸發時機、以及這個尺寸下什麼畫得出來：**[docs/mascots.md](docs/mascots.md)**。

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
  "scope_app": "com.googlecode.iterm2",  // "" ＝ 全域生效
  "y_fraction": 0.30,                    // 輸入條上緣落在螢幕高度的幾成，0 ＝ 最上面
  "width": 720,
  "language": "auto",                    // auto | en | zh-Hant
  "mascot": "clawd"
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

## 限制

- **只支援 iTerm2。** Terminal.app、Warp、Tabby 沒有等價的「寫進這個 session」介面。
  要支援就得走模擬鍵盤，那需要輔助使用權限，為了這件事付那個代價不划算。
- **單向。** Claude 的回覆還是在終端機裡。不過那半本來就是往上捲的；
  這個工具修的是被釘在左下角的那一半。

## 出事的時候

App 做的每一件事都寫進 `~/Library/Logs/Clawdline.log`：熱鍵有沒有註冊成功、
面板有沒有顯示、每一次送出的結果。

- **按 ⌥Space 沒反應**——看 log 有沒有 `hotkey registered`。沒有就是被別的 app 佔走了，換一個。
- **顯示「No Claude Code session found」**——多半是自動化權限被拒。跑
  `tccutil reset AppleEvents dev.sainteye.clawdline` 之後重開，讓它重新問一次。
- **送出失敗**——面板會自己跳回來，你打的字還在，下緣寫著原因。它不會吃掉你的字。

## 參與開發

純 AppKit、沒有框架、除了 `swiftc` 沒有 build 系統。註解寫的是**為什麼是這樣**，
特別是那些「先試了顯而易見的做法然後失敗」的地方——那些才是有價值的部分，請保持這個習慣。

## 出處說明

吉祥物是 Claude Code 裡那隻像素角色的同人畫，社群叫牠 **Clawd**。
本專案與 Anthropic 無任何關聯，未經其背書或授權。Claude 與 Claude Code 是 Anthropic 的商標。

吉祥物之所以做成可替換的 JSON，正是為了讓你換成自己的——見 [docs/mascots.md](docs/mascots.md)。

## 授權

[MIT](LICENSE)
