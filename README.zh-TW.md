# Clawdline

[English](README.md)・[安全性](https://clawdline.com/security/)

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](go.mod)

**透過 Clawdline Cloud，在手機同一份清單查看多台機器的 Claude Code 和 Codex Session，點開就能回覆或留待辦。**

使用示意資料錄製的真實主控台連續操作：

<img src="docs/assets/cloud-sessions-demo-zh-Hant.gif" width="390" alt="真實手機主控台連續錄影：從多台機器的 Session 清單點開工作面板設計，查看 Codex 傳來的小張設計圖，展開待辦並送出「請把新增專案按鈕加大」，最後顯示已傳送。">

[Herdr](https://github.com/herdrdev/herdr/blob/master/docs/versions/0.9.1/website/src/content/docs/connecting-machines.mdx) 也能在 SSH 的 TUI 視窗列出多台機器的 Agent；[T3 Code](https://github.com/pingdotgg/t3code/blob/main/docs/user/remote-access.md) 可連接多個環境。Clawdline Cloud 則在手機瀏覽器將 Session 依機器列在同一頁，並直接開啟對話、待辦與圖片。

## 你可以做什麼

- **掌握 Session：** 看誰在工作、誰在等你；讀取對話、查看 Agent 傳來的圖片，用文字、語音或標註過的圖片回覆。[Session](docs/user/sessions.md)
- **讓工作接著走：** 留下[待辦](docs/user/board.md#session-to-dos)、回答 [Agent 便條紙](docs/user/sessions.md#attention-notes-from-agents)，並接收[通知](docs/user/notifications.md)。
- **操作終端機：** 在 Session 旁開啟即時[終端機](https://clawdline.com/docs/shells/#terminals)。
- **派工並追蹤交付：** [派給](docs/user/clawdfather-and-dispatch.md)另一個 Session、確認成果已合併，用選用的[看板](docs/user/board.md)追蹤較大的工作。
- **稍後執行：** 用 [Callback](docs/user/clawdfather-and-dispatch.md#callbacks-for-long-commands) 排入耗時命令，或用[排程](docs/user/schedules.md)啟動任務；Cloud Pro 可用 Webhook 觸發。
- **整理專案：** [登錄專案](docs/user/projects.md)、透過 [unify](docs/project-files.md#unify) 讓 Claude Code 和 Codex 共用規則與 skill，並查看[用量](docs/user/usage.md)。
- **掌握每台機器：** [Clawdline Cloud](docs/user/remote-access.md) 在手機彙整多台機器的 Session，透過端對端加密中繼連線。
- **手機直連機器：** ICE 可連通時，即時終端機與 Session 讀取改走 WebRTC 直連資料通道；直連失敗時仍可用加密 Relay。[Cloud 連線](docs/cloud.md)

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

開啟 Cloud 後，內容先在本機加密才送出。Cloud 只轉送密文，不持有解密金鑰；只有你的機器與你配對的瀏覽器能讀取。Cloud 仍看得到帳號、裝置與流量等中繼資料。

配對的瀏覽器可以傳送指示、操作你的終端，請只配對你信任的裝置。Claude Code 與 Codex 仍會依各自 AI 供應商的政策傳送請求。

[運作方式、資料流圖解與原始碼](https://clawdline.com/security/)・[AI 資安檢視比較](https://clawdline.com/zh-TW/ai-review/)

## 安裝

[![下載 Clawdline v2.1.3 macOS 版 — Apple Silicon、macOS 13 以上](docs/assets/download-macos-zh-TW.svg)](https://github.com/sainteye/clawdline/releases/download/v2.1.3/Clawdline-v2.1.3-macos-arm64.dmg)

Apple Silicon · macOS 13+ · [安裝教學](https://clawdline.com/docs/install/#mac-dmg)

使用 macOS 13 以上或 Linux 的命令列安裝器時，先安裝 Claude Code 或 Codex、受信任的 Python 3 與 OpenSSL 3 以上版本。macOS 內建的 LibreSSL 不足以驗證；請看[驗證工具需求](docs/user/install.md#before-you-start)。接著執行：

```sh
curl --proto '=https' --proto-redir '=https' -fsSL https://raw.githubusercontent.com/sainteye/clawdline/fcc9059142485514d54683adb769710abb96e1ab/install.sh | sh -s -- --version v2.1.3
```

安裝腳本會先用本機 OpenSSL，對照已審查腳本內固定的公鑰驗證 manifest 簽章，再核對已簽署的壓縮檔大小與 SHA-256；通過後才解壓縮、執行下載的程式。缺少驗證工具或檢查失敗都會停止安裝。你仍須信任安裝腳本與本機工具；驗證通過的程式才會啟動使用者服務並開啟主控台。你可以從主控台開 Session，或在 tmux 執行 `claude` 或 `codex`；需要時可用發行版內附的 tmux。[安裝與首次使用](docs/user/install.md)・[第一個 Session](docs/user/first-session.md)

這個指令同時固定**驗簽腳本**與 `v2.1.3` 發行版。腳本版本與安裝程式的版本是兩個獨立來源；已簽署的發行 manifest 會標明程式的原始碼版本。安裝器不會默默改裝最新發行版。[先審查再固定版本安裝](docs/user/install.md#review-and-pin-an-installation)說明操作方式與首次安裝的信任邊界。

## 支援範圍

- 本機功能免費，不需要 Clawdline 帳號；Cloud 有 Free 與 Pro 方案。
- Windows 可執行常駐服務與主控台，但目前不能列出 Session 或開終端機。[平台](docs/user/platforms.md)
- 1.0 起，macOS 與 Linux 的正式安裝版可直接更新。主控台支援九種語言；新文案尚無翻譯時會顯示英文。[更新方式](docs/updates.md)・[語系機制](docs/localization.md)

## 指南

[使用指南](docs/user/README.md)・[Agent 指南](docs/agent-contract.md)・[從原始碼建置](docs/user/install.md)・[疑難排解](docs/user/troubleshooting.md)・[開發文件](docs/README.md)・[UI 動畫編號表](docs/ui-motion-catalog.md)・[靜態 HTML 成果頁](docs/html-artifacts.md)

使用指南目前只有英文版。

## 授權

[MIT](LICENSE)
