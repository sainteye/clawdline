# Clawdline

🌐 **[Clawdline 官網首頁](https://clawdline.com/)** — 了解 Clawdline 如何集中管理 Claude Code 和 Codex Session、下載 App，並查看安裝指南與安全性說明。

[English](README.md)・[安全性](https://clawdline.com/security/)

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

**透過 Clawdline Cloud，在手機同一份清單查看多台機器的 Claude Code 和 Codex Session，點開就能回覆或留待辦。**

![Clawdline 將 Claude Code 與 Codex Session 集中在同一處，讓你掌握進度、回覆並留待辦；本機瀏覽器在你的機器上使用，選用 Cloud 可從手機及多台機器接續工作。](docs/assets/readme-overview-zh-Hant.png)

## 與 Herdr、T3 Code 的差異

- 🖥️ **Herdr：Agent 的終端工作區。** 在持續運行的終端窗格中執行 Agent，並在同一個 TUI 管理本機與 SSH 工作區。[Herdr 介紹](https://github.com/herdrdev/herdr#readme)・[多機器連線](https://herdr.dev/docs/connecting-machines/)
- 💬 **T3 Code：操作程式開發 Agent 的 App。** 透過桌機、網頁與手機 App 操作 Agent，支援本機與遠端環境。[T3 Code 介紹](https://github.com/pingdotgg/t3code#readme)・[遠端存取](https://github.com/pingdotgg/t3code/blob/main/docs/user/remote-access.md)
- 🔁 **Clawdline：協調 Session，追蹤工作交付。** 在瀏覽器集中管理已使用的 Claude Code 和 Codex Session；留下待辦、回答 Agent 便條並派工，選用看板追蹤驗證、合併與部署；清楚區分「Agent 回報完成」與「已驗證合併」。選用 Cloud 後，也能從手機同一份清單掌握多台機器。[Session](docs/user/sessions.md)・[派工](docs/user/clawdfather-and-dispatch.md)・[看板](docs/user/board.md)

## 你可以做什麼

### 掌握 Agent 的工作

- 🤖 **所有 Session 集中在一處** — 看哪些 Claude Code 和 Codex Session 正在工作、哪些在等你，點開對話就能跟上進度。[Session](docs/user/sessions.md)
- 💬 **用文字、語音或圖片回覆** — 口述訊息、附上截圖，或先圈出要改的地方再送出；Agent 傳來的圖片也能直接在對話中打開。[訊息與圖片](docs/user/sessions.md#answer-and-send)
- 🖥️ **Session 旁的即時終端機** — 直接在主控台執行命令，不必切換到另一個終端機程式。[終端機](https://clawdline.com/docs/shells/#terminals)
- 📌 **可以直接回應的 Agent 便條紙** — 問題、建議回覆與待你檢視的內容留在對話上方，回來時就能接著處理。[關注便條](docs/user/sessions.md#attention-notes-from-agents)
- 🔔 **工作需要你時收到通知** — 自行開啟 Session 等待、交付、看板決策與排程失敗等通知，點開即可回到相關工作。[通知](docs/user/notifications.md)
- 📊 **Token 用量與帳號額度** — 查看 Session 的 context 與 Token 用量、任務和看板的 Token 帳單；有可用讀值時，也能查看供應商額度與重設時間。[用量](docs/user/usage.md)

### 把需求推進到交付

- ☑️ **不打斷當前工作的待辦** — 把下一件事留在 Session 佇列，讓 Agent 在檢查工作時接手。[Session 待辦](docs/user/board.md#session-to-dos)
- 🗂️ **從想法追到部署的看板** — 追蹤提案、規劃、實作、驗證、合併與部署；將工作指派給 Session，也看得見哪些地方需要你決定。看板可自行選擇是否啟用。[看板](docs/user/board.md)
- 🤝 **派給另一個 Session 協作** — 派出範圍明確的任務、收回結果，並核對變更是否真的合併。[派工](docs/user/clawdfather-and-dispatch.md)
- 📖 **用 Playbook 重複啟動工作** — 儲存審查等常用任務的完整啟動訊息；Agent 也能透過 `clawdline playbook list|create|edit|delete` 管理。[Playbook API 與 CLI 指南](docs/playbooks.md#agent-api)・`clawdline guide project`
- 🧩 **共用規則與 skill** — 透過 unify，讓 Claude Code 和 Codex 都能使用專案指示與 skill。[Unify](docs/project-files.md#unify)
- 📁 **一起管理專案與 worktree** — 登錄專案、在正確的目錄啟動 Session，並查看 worktree、已交付的工作與已合併的變更。[專案](docs/user/projects.md)

### 離開桌前，工作繼續

- 📱 **手機同一份清單掌握多台機器（Cloud）** — 在配對的瀏覽器開啟不同機器的 Session、讀取對話、回覆與留待辦。Cloud 是選用功能。[遠端存取](docs/user/remote-access.md)
- 🌐 **Agent 跨機器接力（Cloud・dev）** — 雙向配對後，Agent 可探索遠端 Project 與 Session、派出任務並互傳訊息；新增的探索與 Session 喚醒功能目前提供於 dev 版本。[跨機器協作](docs/remote-agent-work.md)
- ⚡ **手機直連你的機器（Cloud）** — ICE 可直連時，即時終端機與 Session 讀取走 WebRTC；無法直連時，改走端對端加密 Relay。[Cloud 連線](docs/cloud.md)
- ⏳ **耗時命令完成後再接續** — 把命令交給 Callback，結果就緒時通知 Session 回來處理。[Callback](docs/user/clawdfather-and-dispatch.md#callbacks-for-long-commands)
- 🗓️ **排程任務與 Webhook 觸發** — 指定時間或定期啟動工作；Cloud Pro 也能讓外部服務透過 Webhook 觸發排程。[排程](docs/user/schedules.md)
- 💡 **問答、Issues 與 Ideas 社群（Cloud）** — 閱讀討論、詢問使用方式、回報問題與提出功能建議。任何人都能閱讀，登入後可參與；回覆與實際交付進度分開追蹤。[社群指南](docs/user/help-center.zh-TW.md)・[公開邊界](docs/cloud-support.md)

Clawdline 的常駐服務與主控台在你的機器上執行，管理你已使用的 Claude Code 和 Codex Session。Cloud 是選用功能；中繼只傳送密文。[運作方式](docs/architecture.md)

## 跨機器 Session 協作（Cloud）

讓 Mac 上的 Codex 與 Linux 上的 Claude Code 接力工作。雙向配對後，Agent 可探索遠端 Project 與 Session、把任務派到指定專案、與既有 Session 互傳訊息，並在結果回來時接續工作；不必由你複製指示與回覆。

![跨機器 Session 協作：探索、派工與雙向訊息、結果通知；Cloud 只轉送密文](docs/assets/cross-machine-sessions-zh-Hant.svg)

兩台機器都需在線並授權互通；工作在目標機器執行，程式碼與內容金鑰留在各自機器上。配對可撤銷，派工完成不代表變更已合併。**目前提供於 dev 版本**；正式版的一行安裝尚未包含這次新增的探索與 Session 喚醒功能。[使用方式與權限界線](docs/remote-agent-work.md)

## 手機直連你的機器（Cloud）

手機完成配對後，Clawdline 會嘗試與 Mac 或 Linux 機器建立 WebRTC 直連資料通道，讓即時終端機與 Session 讀取少經過一次 Relay 轉送，操作反應可以更快。Cloud 仍負責連線協商及其他指令；ICE 無法直連時，讀取會改走端對端加密的 Relay。

![手機與 Mac 或 Linux 機器直連傳送終端機及 Session 讀取；Clawdline Cloud 負責加密協商與備援轉送](docs/assets/phone-direct-zh-Hant.svg)

可以在機器的「設定 → 遠端」或執行 `clawdline cloud direct off` 關閉直連。AWS Linux 可把 ICE 固定在單一 UDP 埠；請看 [Cloud 設定說明](docs/cloud.md#aws-linux-direct-connection)與[完整 Linux 步驟](docs/linux.md#let-a-cloud-browser-connect-directly-to-ec2)。固定埠設定目前提供於開發版本。

## 安全性與你的資料

Clawdline 在你自己的機器上讀取 Claude Code 與 Codex 對話、終端與專案檔案。本機使用不需要帳號，Cloud 預設關閉，由你決定是否開啟。

![機器與已配對瀏覽器負責加解密，Clawdline Cloud 轉送密文且不持有內容金鑰，仍可見帳號、裝置與流量中繼資料。](docs/assets/security-data-flow-zh-Hant.svg)

開啟 Cloud 後，內容先在本機加密才送出。Cloud 只轉送密文，不持有解密金鑰；只有你的機器與你配對的瀏覽器能讀取。Cloud 仍看得到帳號、裝置與流量等中繼資料。

配對的瀏覽器可以傳送指示、操作你的終端，請只配對你信任的裝置。Claude Code 與 Codex 仍會依各自 AI 供應商的政策傳送請求。

[運作方式、資料流圖解與原始碼](https://clawdline.com/security/)・[AI 資安檢視比較](https://clawdline.com/zh-TW/ai-review/)

## 安裝

[![下載 Clawdline v2.8.0 macOS 版 — Apple Silicon、macOS 13 以上](docs/assets/download-macos-zh-TW.svg)](https://github.com/sainteye/clawdline/releases/download/v2.8.0/Clawdline-v2.8.0-macos-arm64.dmg)

Apple Silicon · macOS 13+ · [安裝教學](https://clawdline.com/docs/install/#mac-dmg)

使用 macOS 13 以上或 Linux 的命令列安裝器時，先安裝 Claude Code 或 Codex、受信任的 Python 3 與 OpenSSL 3 以上版本。macOS 內建的 LibreSSL 不足以驗證；請看[驗證工具需求](docs/user/install.md#before-you-start)。接著執行：

```sh
curl -fsSL https://clawdline.com/install.sh | sh
```

安裝腳本會先用本機 OpenSSL，對照已審查腳本內固定的公鑰驗證 manifest 簽章，再核對已簽署的壓縮檔大小與 SHA-256；通過後才解壓縮、執行下載的程式。缺少驗證工具或檢查失敗都會停止安裝。你仍須信任安裝腳本與本機工具；驗證通過的程式才會啟動使用者服務並開啟主控台。你可以從主控台開 Session，或在 tmux 執行 `claude` 或 `codex`；需要時可用發行版內附的 tmux。[安裝與首次使用](docs/user/install.md)・[第一個 Session](docs/user/first-session.md)

短網址指向經審查的驗簽腳本，並安裝最新穩定版。腳本版本與安裝程式的版本是兩個獨立來源；已簽署的發行 manifest 會標明程式的原始碼版本。入口會隨經審查的安裝器更新。[先審查再固定版本安裝](docs/user/install.md#review-and-pin-an-installation)保留固定腳本與發行版的替代方式，並說明首次安裝的信任邊界。

## 支援範圍

- 本機功能免費，不需要 Clawdline 帳號；Cloud 有 Free 與 Pro 方案。
- Windows 可執行常駐服務與主控台，但目前不能列出 Session 或開終端機。[平台](docs/user/platforms.md)
- 1.0 起，macOS 與 Linux 的正式安裝版可直接更新。主控台支援九種語言；新文案尚無翻譯時會顯示英文。[更新方式](docs/updates.md)・[語系機制](docs/localization.md)

## 指南

[使用指南](docs/user/README.md)・[Agent 指南](docs/agent-contract.md)・[從原始碼建置](docs/user/install.md)・[疑難排解](docs/user/troubleshooting.md)・[開發文件](docs/README.md)・[UI 動畫編號表](docs/ui-motion-catalog.md)・[靜態 HTML 成果頁](docs/html-artifacts.md)

使用指南目前只有英文版。

## 授權

[MIT](LICENSE)
