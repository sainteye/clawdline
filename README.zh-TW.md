# Clawdline

[English](README.md)

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

**你已經在跑的 Claude Code 與 Codex Session，都在這裡管理。**

Clawdline 是在 macOS 與 Linux 上執行的本機 daemon，附網頁主控台。它不需要 wrapper 或 hook，
就能找到你在 tmux（Mac 上也包括 iTerm2）裡的 Session，告訴你哪一個在等你，也讓一個 Session
把工作交給另一個，並證明結果真的合併進去了。Agent 與程式碼都留在你的機器上；選用的
Clawdline Cloud 透過端對端加密的中繼，讓手機或其他機器連回來。

<p align="center">
  <img src="docs/assets/sessions-live.gif" width="760" alt="Clawdline 即時更新多個 Claude Code 與 Codex Session 的狀態。">
</p>

## 它能做什麼

**基本功能：每天都會用到**

- **看見每一個 Session。** 每一列都標出專案、助理與狀態：工作中、在等你、閒置，或讀不出來
  ——讀不出來就照實說，不會猜成閒置。可以讀真正的對話記錄、回答問題、傳文字或畫過重點的圖片、
  用說的輸入、開新 Session、停下目前這一輪、安全關閉，或先封存、之後再叫回來。
  [Session](docs/user/sessions.md)
- **即時終端機。** 在 Session 清單旁打開終端機直接輸入；在這台機器上可以，從 app.clawdline.com
  也可以（那個瀏覽器要先有 Cloud 終端權限），機器連得到時會自動改走直連。
  [終端機](https://clawdline.com/docs/shells/#terminals)
- **用手機操作，並收到通知。** 同一套主控台在任何瀏覽器都能用，「讀取」與「傳送」是兩個分開的
  權限；可以走 SSH 轉發、你自己免費的 cloudflared tunnel，或 Cloud。Session 等了十分鐘沒人回、
  Agent 呼叫你，或排程失敗時，會用 Web Push 通知你；點通知就打開那個 Session。
  [遠端連線](docs/user/remote-access.md)・[通知](docs/user/notifications.md)
- **用量一眼看懂。** 每個 Session 的 context、各 Session 與看板項目花了多少 token，以及每個
  助理的方案額度還剩多少。[用量](docs/user/usage.md)

**協調工作：Clawdline 和其他工具不同的地方**

- **派工，並留下合併證據。** Session 可以把範圍明確的工作派給子 Session、收回結果，或把整條
  工作線交接給新的 Session。寫入範圍（claims）會擋住彼此重疊的修改；Clawdline 記錄的是工作
  有沒有真的進到分支，而不只是子 Session 說它做完了。[派工與合併確認](docs/user/clawdfather-and-dispatch.md)
- **一路追到交付的看板。** 把 Feature、Issue 或 Epic 指派給 Session，跟著它經過實作、驗證、
  合併與部署，每個階段都要附證據。規劃預設開啟：Feature 與 Epic 開工前要先有計畫，並通過獨立
  審查。需要你決定的問題會直接出現在卡片上。看板本身預設關閉，要先在設定裡開啟。
  [看板](docs/user/board.md)
- **角色。** 用內建角色開 Session，例如架構師、審查者、技術寫手，各自帶著自己的手冊與 skill。
  [角色](docs/personas.md)
- **排程與 Webhook。** 把任務存起來，讓 Session 依本機時鐘或手動執行，有補跑、逾時與失敗通知。
  Cloud Pro 還能用 Webhook 啟動。[排程](docs/user/schedules.md)

**專案與多台機器**

- **跨機器的專案。** 以 git origin 對應，讓第二台機器拿到相同的專案名稱、圖示和沒進
  git 的 skill，有沒有 Cloud 都可以。[專案](docs/user/projects.md)
- **Claude 和 Codex 共用同一套規則與 skill。** `clawdline project unify` 會列出怎麼讓同一個
  專案的 Claude Code 與 Codex Session 讀到同一份 `AGENTS.md` 和同一組 skill；你確認套用後才
  會改檔案，而且不會替你 commit。[Unify](docs/project-files.md#unify)
- **選用的加密 Cloud。** 透過 Clawdline Cloud 的端對端加密中繼，配對手機或多台機器。預設關閉，
  目前是預覽版。[遠端連線](docs/user/remote-access.md)

<p align="center">
  <img src="docs/assets/fleet-phone.png" width="390" alt="手機上的 Clawdline，顯示不同專案中正在工作、等待中與子 Session 的狀態。">
</p>

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
./bin/clawdline open        # 讓這個瀏覽器登入，讀取並傳送到 Session
```

在 tmux 裡執行 `claude` 或 `codex`，Session 就會出現在清單上。每一步與確認方法都在
[安裝與第一次執行](docs/user/install.md)。`./bin/clawdline update` 會告訴你這台機器是否落後最新
版本（[更新](docs/updates.md)）。

## 限制

- **1.0 之前**，要從原始碼建置，還沒有安裝檔。
- 主控台目前只有**繁體中文**介面。
- **Windows** 能跑 daemon 與主控台，但還不能列出 Session，也不能開終端機。Linux 用
  `systemd --user` 在背景執行；macOS 另有選用的原生 App（[平台](docs/user/platforms.md)）。
- Claude 的 **5h／7d 方案百分比**需要一個會寫入 `~/.claude/statusline-cache/rate-limits.json`
  的 Claude Code status line（[用量](docs/user/usage.md)）。
- 在你自己機器上的功能全部免費，也不需要帳號。Cloud 有 Free 與 Pro 兩種方案。

## 和其他工具相比

[T3 Code](https://github.com/pingdotgg/t3code) 是完整的操作介面，有桌面與手機 App 和原生版本控制
流程。[Herdr](https://github.com/herdrdev/herdr) 是以終端機為主的 Agent multiplexer。
[Orca](https://github.com/stablyai/orca) 是附編輯器與 diff review 的 Agent 開發環境。Clawdline 不是
IDE，也不取代 Claude Code 或 Codex：它管理你本來就會開的 Session——派工、合併證據、看板與排程
——適合「難的不是開一個 Agent，而是長時間可靠地帶好幾個 Agent」的時候。

## 文件

- 開始：[安裝與第一次執行](docs/user/install.md)・[平台](docs/user/platforms.md)・
  [疑難排解](docs/user/troubleshooting.md)
- 基本功能：[Session](docs/user/sessions.md)・[快捷鍵](docs/user/keyboard-shortcuts.md)・
  [遠端連線](docs/user/remote-access.md)・[通知](docs/user/notifications.md)・[用量](docs/user/usage.md)
- 協調工作：[派工與合併確認](docs/user/clawdfather-and-dispatch.md)・[看板](docs/user/board.md)・
  [排程](docs/user/schedules.md)
- 專案與多台機器：[專案](docs/user/projects.md)・[更新](docs/updates.md)
- 網站指南：[clawdline.com/docs](https://clawdline.com/docs/)
- 開發者：[architecture.md](docs/architecture.md)、[AGENTS.md](AGENTS.md)、
  [docs/README.md](docs/README.md)

使用指南目前只有英文版。

## 授權

[MIT](LICENSE)
