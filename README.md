# Clawdline

**Claude Code 與 Codex 的本機控制中心。一次看見所有 Session、知道哪一個正在等你，
也讓 Agent 能彼此交接工作，而不會遺失交付紀錄。**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

Clawdline 由 Go daemon 與 React 控制台組成。Agent 與程式碼都留在你的機器上執行；
選用的 Cloud 連線只負責提供一條加密路徑，讓其他裝置連回這台機器。

## 為什麼要使用 Clawdline？

一個程式開發 Agent 很容易掌握；但當多個 Agent 分散在多個程式庫，終端機分頁很快就會
失控：你會忘記 Session 屬於哪個 Project、錯過權限詢問，也很難確認派出去的工作究竟只是
做完了，還是真的通過檢查並合併。

Clawdline 把這些彼此獨立的終端機，整理成一套看得見、管得到的系統。

### 以 Project 為核心的 Session

每個 Claude Code 或 Codex Session 都會顯示所屬 Project、終端機、Agent 與目前工作。
你自行在 tmux 啟動的 Session 也會出現；Clawdline 不要求你透過 wrapper 啟動，也不會在
Claude Code 或 Codex 裡安裝 hook。

### Session 狀態回報與管理

直接看出哪些 Session 正在工作、等待你、閒置、完成，或目前無法判讀。打開 Session 就能閱讀
真正的對話紀錄、回答問題、補充指令、中斷或關閉。證據不足時會明確顯示 `unknown`，不會把它
猜成 idle。

<p align="center">
  <img src="docs/assets/sessions-live.gif" width="760" alt="Clawdline 即時更新多個 Claude Code 與 Codex Session 的狀態。">
</p>

### 用手機操作 Codex 與 Claude Code

同一套控制台可以在瀏覽器中使用。啟用選用的 Clawdline Cloud 後，你能從手機查看進度、
閱讀對話紀錄、接收需要注意的通知並回覆 Session，而工作仍在自己的機器上執行。讀取與送出
指令是兩個獨立權限。

<p align="center">
  <img src="docs/assets/fleet-phone.png" width="390" alt="手機上的 Clawdline，顯示不同 Project 中正在工作、等待中與子 Session 的狀態。">
</p>

### Scheduled Task，由 Agent 執行

把 Claude Code 或 Codex 的任務存起來，設定單次或週期性排程。排程任務與互動式派工共用同一套
broker、隔離、逾時、狀態與結果紀錄。daemon 離線期間若錯過執行時間，也只會補跑最近一次符合
條件的工作，不會一次重播所有錯過的排程。

### 用 Webhook 執行複雜任務

把 Cloud webhook 綁定到已儲存的 Agent 任務。事件送達時，Clawdline 可以在你的機器上啟動完整
Agent Session，而不只是執行寫死的 shell command；持久化的 claim 與 receipt 會避免同一個事件
悄悄開出重複工作。

### Clawdfather 統合、協調其他 Sessions

指定一個執行中的 Session 成為 **Clawdfather**，負責整台機器的協調工作。它可以檢視所有 Session、
把範圍明確的任務交給 Claude Code 或 Codex 子 Session、等待結果、把整條工作移交給另一個 Session，
並清楚區分「已交付」、「已審查」與「已合併」。

## 和其他 Project 相比

[T3 Code](https://github.com/pingdotgg/t3code) 是完整的 Agent 執行環境操作介面，提供 Web、桌面與
手機 App，也支援比 Clawdline 更多的 Agent 與原生版本控制工作流程。若你要的是功能豐富、
跨裝置、接近完整開發工作區的體驗，T3 Code 很合適；Clawdline 則更專注於管理你已經在 tmux 或
iTerm2 中啟動的 Claude Code／Codex Session，以及它們的狀態、排程與持久協調紀錄。

[Herdr](https://github.com/herdrdev/herdr) 是以真實終端機 pane 為核心的 Agent 執行環境與
multiplexer，能偵測許多不同的程式開發 Agent、彙整工作區狀態，並快速跳到需要注意的 pane。
如果你想要以終端機為主的多 Agent 工作區與廣泛的 Agent 支援，Herdr 的定位更直接；Clawdline
的重點則是 Web／手機控制、broker 派工、landing 證據、Scheduled Task、Webhook 與 Clawdfather。

[Orca](https://github.com/stablyai/orca) 是完整的 Agent Development Environment，主打隔離的
git worktree、內建 terminal、editor、diff review、瀏覽器、GitHub／Linear 整合與手機 companion。
如果你想在一個應用程式裡建立、比較並合併多個 Agent 的成果，Orca 涵蓋得更廣；Clawdline 不是
IDE，而是環繞既有 Session 運作的控制平面，並把長時間執行的排程、Webhook 與 Agent 對 Agent
協調放在核心。

Clawdline 不取代 IDE，也不取代 Claude Code 或 Codex。當困難的地方不再是「開一個 Agent」，
而是「長時間可靠地管理多個 Agent」，就是它適合的位置。

## 安裝

Clawdline 目前仍是 pre-1.0，尚未提供 release 下載。請在 macOS 或 Linux 從原始碼建置。
你需要 Go 1.25 以上、Node.js 與 npm、tmux，以及 Claude Code 或 Codex。Windows 版本可以完成
build，但目前還不能在 Windows 上探索或控制 Session。

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline

(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline
```

啟動 daemon：

```sh
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

接著在另一個終端機執行：

```sh
./bin/clawdline doctor
./bin/clawdline open
```

在 tmux 裡啟動 Claude Code 或 Codex，Session 就會出現在控制台：

```sh
tmux new -s work
cd /path/to/your/project
claude  # 或：codex
```

也可以在尚未啟動 Agent 前，明確把既有目錄加入「開一個 Session」清單；這個動作不會呼叫模型：

```sh
./bin/clawdline project add /path/to/project /path/to/another-project
./bin/clawdline project list
```

在 macOS 上也可以建置原生外殼：

```sh
tools/package-macos.sh          # dist/Clawdline Next.app
tools/package-macos.sh --dmg    # 同時建立 disk image
```

配對、Cloud 設定、診斷與疑難排解，請繼續閱讀[開始使用指南](docs/getting-started.md)。

[系統架構](docs/architecture.md) · [遠端存取](docs/remote.md) ·
[所有文件](docs/README.md) · [MIT License](LICENSE)
