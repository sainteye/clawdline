# Clawdline

[English](README.md)

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

**Claude Code 與 Codex 的本機控制中心。一次看見所有 Session、知道哪一個正在等你，
也讓 Agent 能彼此交接工作，而不會遺失交付紀錄。**

Clawdline 由 Go daemon 與 React 控制台組成。它看得到你本來就在 tmux（Mac 上也包括 iTerm2）
裡跑的 Claude Code 與 Codex Session，不需要 wrapper、不裝 hook，讓你從瀏覽器或手機閱讀、回答、
啟動和停止它們。Agent 與程式碼都留在你的機器上；選用的 Clawdline Cloud 只是一條端對端加密的
路徑，讓其他裝置連回這台機器。

## 它能做什麼

<p align="center">
  <img src="docs/assets/sessions-live.gif" width="760" alt="Clawdline 即時更新多個 Claude Code 與 Codex Session 的狀態。">
</p>

- **Session 與注意力。** 每個 Session 都標出所屬 Project、Agent 與狀態：正在工作、在等你、
  閒置，或讀不出來——讀不出來就照實說，不會猜成閒置。打開就能讀真正的對話紀錄、回答問題、
  傳文字或畫過重點的圖片、用說的輸入、停下目前這一輪，或安全地關掉。
- **用手機操作。** 同一套控制台在任何瀏覽器都能用。可以透過 SSH 轉發、你自己的 cloudflared
  tunnel 或 Clawdline Cloud 連回來；「讀取」和「操作」是兩個分開的權限。問題等了十分鐘沒人回，
  會推播通知你。
- **排程與 Webhook。** 把 Claude Code 或 Codex 的任務存起來，照本機時鐘執行，有補跑、逾時與
  失敗通知。Cloud Pro 還能用 Webhook 從任何事件啟動同一個任務。
- **看板與驗收。** 把工作放上 Project 的看板、指派給 Session，由 Agent 帶著證據推進實作、驗證、
  合併與部署。只能事後判斷的改動，放進「等待驗收」清單。
- **Clawdfather 與派工。** Session 可以把範圍明確的工作派給子 Session、收回結果，或把整條工作
  交接給新的 Session；交付的工作是否真的落地，由 Clawdline 記錄。也可以指定一個 Session 當
  整台機器的協調者 **Clawdfather**。
- **跨機器的 Project。** 讓第二台機器取得與第一台相同的 Project 名稱、圖示和沒進 git 的 skill，
  以 git origin 對應。

<p align="center">
  <img src="docs/assets/fleet-phone.png" width="390" alt="手機上的 Clawdline，顯示不同 Project 中正在工作、等待中與子 Session 的狀態。">
</p>

## 和其他工具相比

[T3 Code](https://github.com/pingdotgg/t3code) 是完整的 Agent 操作介面，有 Web、桌面與手機 App，
支援更多 Agent 與原生版本控制流程。[Herdr](https://github.com/herdrdev/herdr) 是以終端機為主的
Agent 執行環境與 multiplexer，支援的 Agent 很廣。[Orca](https://github.com/stablyai/orca) 是完整的
Agent Development Environment，有 worktree、編輯器、diff review 與各種整合。

Clawdline 不是 IDE，也不取代 Claude Code 或 Codex。它是環繞你既有 Session 的控制平面，核心是
Web／手機控制、broker 派工、落地證據、排程、Webhook 與 Clawdfather 協調。當困難的地方不再是
「開一個 Agent」，而是「長時間可靠地管理多個 Agent」，就是它適合的位置。

## 安裝

在 macOS 或 Linux 從原始碼建置。你需要 Go 1.25 以上、Node.js 與 npm、tmux，以及 Claude Code
或 Codex。

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline
(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline

CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

在另一個終端機：

```sh
./bin/clawdline doctor      # 版本、port、狀態目錄
./bin/clawdline open        # 讓這個瀏覽器登入；加 --send 才能在 Session 裡打字
```

接著在 tmux 裡執行 `claude` 或 `codex`，Session 就會出現在清單上。每一步與確認方法，請看
[Install and first run](docs/user/install.md)。

## 幾點說明

- Clawdline 目前是 **pre-1.0**，還沒有 release 下載，之後還會變動。
- 控制台介面目前是**繁體中文**，這是它唯一附帶的語言。
- **Windows** 可以建置並執行 daemon 與控制台，但還不能列出或控制 Session。Linux 可以在
  `systemd --user` 服務下無介面執行；macOS 另有選用的原生 App。
- 自己機器上的一切都免費、不需要帳號。**Clawdline Cloud**（多台機器、遠端配對、排程 Webhook）
  是選用的，預設關閉，目前仍是預覽版。

## 文件

下面的詳細說明頁是英文。中文的使用說明在 https://clawdline.com/docs/ 。

開始使用

- [安裝與第一次執行](docs/user/install.md)
- [macOS、Linux 與 Windows](docs/user/platforms.md)：原生 App、`systemd --user` 服務、Windows
  目前能做什麼
- [疑難排解](docs/user/troubleshooting.md)

日常使用

- [查看、回答、啟動、停止與關閉 Session](docs/user/sessions.md)
- [鍵盤快捷鍵](docs/user/keyboard-shortcuts.md)
- [通知](docs/user/notifications.md)
- [從手機或另一台機器遠端存取](docs/user/remote-access.md)：SSH、自己的 tunnel、Clawdline Cloud

執行工作

- [排程與 Webhook](docs/user/schedules.md)
- [看板、Session 待辦、現在與驗收](docs/user/board.md)
- [Clawdfather、派工、落地與交接](docs/user/clawdfather-and-dispatch.md)
- [Project，以及把它們帶到另一台機器](docs/user/projects.md)
- [Token 用量、Agent 額度與容量](docs/user/usage.md)

要在它上面開發？從 [docs/architecture.md](docs/architecture.md)、[AGENTS.md](AGENTS.md) 開始，
所有設計文件列在 [docs/README.md](docs/README.md)。

## 授權

[MIT](LICENSE)
