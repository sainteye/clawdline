# Clawdline

**Claude Code 與 Codex 的本機協調中心：看懂每個 Session 的狀態、跨助理分派工作、
獨立審查結果，最後交付已驗證的精確版本。**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/macOS-13%2B-black.svg)](#安裝)
[![Swift](https://img.shields.io/badge/Swift-5-orange.svg)](Sources)
[![Dependencies](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](#安裝)

[English](README.md) · 繁體中文

[網站](https://clawdline.com/) · [安裝](#安裝) ·
[Clawdfather](https://clawdline.com/clawdfather) ·
[價格](https://clawdline.com/pricing) · [安全性](https://clawdline.com/security) ·
[公開使用手冊](https://clawdline.com/docs) · [開源技術文件](#文件)

## Clawdfather 交付循環

多數 Agent 工具擅長處理一段對話，或是替你多開幾個 Worker。當工作變成多個真實的 Terminal
Session、Claude 與 Codex 同時在同一個 Project 裡修改檔案、結果需要另一個人獨立審查，最後還得確認
目標 Branch 確實包含審查過的內容時，只會「多開幾個 Agent」就不夠了。Clawdline 從這個位置開始。

**Clawdfather 是整台 Machine 上持續存在的協調角色。** 它掌握 Session、Task、Wait 與尚未落地的
結果，把一個目標拆成各自有人負責的工作，交給合適的助理，請沒有參與實作的 Session 進行審查，
再把完整 Finding Set 交給一輪範圍明確的修正。最後由 Root 保留整合責任，直到精確的候選版本已經
完成驗證並正式釋出。

<img src="docs/assets/clawdfather-loop.gif" width="760" alt="Clawdfather 交付循環：Root 保留計畫與落地責任，Claude 與 Codex 在宣告過寫入範圍的工作線上平行執行，另一個 Session 獨立審查，一輪修正關閉完整 Finding Set，最後驗證目標 Commit 並留下長期收據。">

```text
產品意圖
  → 計畫與落地負責人
  → Claims 與 Claude／Codex 平行派工
  → 獨立審查
  → 一輪範圍明確的修正
  → Root 負責落地
  → 精確版本驗證
  → 經授權的 Build、Restart 與線上收據
```

這套流程刻意不只是一顆 Spawn 按鈕：

- **已交付，不等於已審查。** Child Session 回傳 `success`，只代表答案已經送達。沒有參與實作的
  Reviewer 才能判斷它是否安全，並留下別人可以重現的證據。
- **已審查，不等於已落地。** `SAFE TO LAND` 代表可以開始整合，不代表工作已經完成。Root 必須逐檔
  Stage、讀完真正的 Staged Diff、記下目標 Commit，並持續負責到 Broker 能驗證結果確實落在目標版本。
- **測試通過必須說清楚測的是什麼。** Child 在 Working Tree 上的測試，只能證明它看到的 Overlay；
  Release 驗收會針對精確的候選版本執行，Build、Restart 與線上 Health 也各自留下收據。
- **平行工作有明確邊界。** Shared Tree 任務開始前會檢查宣告的寫入路徑；不是檔案的操作可以序列化；
  Wait、Release、Completion ACK 都是有型別的紀錄，而不是期待另一個 Session 剛好看見的一行文字。

目前這條流程由一個明確登記的 Clawdfather Session，搭配 Clawdline 已有的 Resume、Dispatch、Review、
Landing、Closeability 與 Verification 能力完成。型別化的決策與交付圖現在會跟著每個 task；broker
會驗證依賴、從耐久 receipt 推導當前 frontier，並透過 API 發布 control sheet。視覺化編輯器仍是下一步。
產品意圖、不可逆操作、費用、Credential、隱私與安全性等決策，仍然由人負責。

## Clawdline 不同在哪裡

| | |
| --- | --- |
| **協調你已經開啟的 Session** | 不需要 Wrapper、替代 Runtime 或特殊啟動方式。你手動開啟的 Claude Code、Codex，會和 Clawdline 派出的 Session 出現在同一份清單；關掉 App，原本的工作仍會繼續。 |
| **Claude 與 Codex 是同一層級的成員** | 兩邊都能把工作派給對方。每個 Child 都是真實、看得見的 Terminal Session，保留 Transcript、Status、待回答問題、Task Record 與 Usage；Mac 與手機看到的是同一套資訊。 |
| **交付關係不會隨對話消失** | Claim、Wait、Result、Review、Landing Record、Completion ACK 與 Closeability 都是長期證據。Tab 安靜下來，不會自動把未完成的責任變成完成。 |
| **Project 才是整理工作的單位** | Session、Task、Schedule、開發伺服器、Branch、Backlog、Health 與 Deploy 狀態會回到同一個 Project，而不是散落在 Terminal、CI 頁面與 Status Page。 |
| **本機優先，Cloud 只增加連線範圍** | 免費 Mac App 不需要帳號，也不會在任何助理內安裝東西。[Clawdline Cloud](https://clawdline.com/) 是選用的加密連線層；執行環境與 Content Key 仍留在你擁有的硬體上。 |

## Session 不是一排 Terminal Tab

<img src="docs/assets/fleet-wide.png" width="760" alt="Clawdline 在瀏覽器裡：這台 Mac 上的每一個 Claude Code 與 Codex session 收在同一份清單，被別的 session 派出去的那些縮排在它底下，選中的那個的逐字稿就在清單旁邊">

只開一個 coding agent 的日子撐不了多久。到了下午就變成五個，其中兩個還不是你開的——某個 session
覺得那份 diff 需要第二個人看，就把它派了出去。你手上的東西這時候已經不是一排終端機，而是一支有
形狀的艦隊：哪一個在跑、哪一個卡在一個問題上、每一個是哪一種助理，還有**哪一個是誰叫出來的**。
這些東西，終端機視窗一件都畫不出來。它給你的是分頁標題，而分頁標題是**任務**——兩個不同專案很
容易在做讀起來一模一樣的任務——而且 plugin 也動不到，因為 plugin 提供的是 command、agent、
hook、MCP server 與 skill，不是 TUI 版面。

<img src="docs/assets/tabs.gif" width="760" alt="一個終端機視窗裡開了十一個 session，一個一個切過去。每個分頁標題都從左邊被裁掉，裁到只剩括號裡的行程名稱，所以整排讀起來就是同樣那四個字元重複十一次——…n3)、…de)、…sh)——而唯一會講清楚自己在做什麼的，就是你正開著的那一個。">

十一個 session，在終端機裡就長這樣。每個標題都從左邊被裁掉，裁到只剩括號裡的行程名稱，所以整排
讀起來就是 `…n3)` `…de)` `…sh)` 重複十一次——其中還有一個是 Codex session，那排分頁也一樣說不
出來。唯一講得清楚自己在做什麼的，是你已經開著的那一個。想知道另外十個在幹嘛，只能一個一個切過
去，一次一個，而它們不會等你。

這不是終端機做錯了什麼：一個分頁就是一個行程，而一個行程本來就是它答應要顯示的全部。它只是不是
這些工作真正的形狀。**一支艦隊把形狀畫出來會長什麼樣，就是這一頁最上面那張圖**——不是給你更多
資訊，是把同樣的資訊排好，讓「哪一個在跑、哪一個卡住了、哪一個是別的 session 派出去的差事」變成
你**看得到**的東西，而不是要去翻出來的東西。

Clawdline 把這個形狀畫出來，也讓你在上面動手。按 <kbd>⌘</kbd><kbd>K</kbd>，每個 session 都是
一行，而那一行會說它在做什麼——**在跑、跑完了、還是在等你回答**——被某個 session 派出去的那些，
縮排在它底下。按 <kbd>⌥</kbd><kbd>Space</kbd>、打字，內容就進到你指定的那一個。

**它不會在 Claude Code 或 Codex 裡裝任何東西。** 沒有 hook、沒有 MCP server、不會包一層
`claude` 或 `codex` 指令、也不會動你的設定檔。它讀的是你的 session 本來就在畫的螢幕、本來就在
寫的紀錄——所以你一小時前自己開的那四個也在清單裡，不只是被派出去的那些。iTerm2 直接支援，
其餘終端機透過 tmux。

**Codex session 就在同一份清單裡**，條件一模一樣：看得到它在做什麼、讀得到它說過什麼、送得出
指令、也開得了新的一個——而且派工的兩端誰都可以是誰，Claude Code 的 session 可以把工作派給 Codex
的子 session，反過來也行。[這句話的完整內容 →](#codex-也在同一條輸入條裡)

沒有東西要搬、也沒有東西要復原。關掉它，你的環境就跟原本一模一樣。

## 交付循環周邊的功能

| | |
| --- | --- |
| **Clawdfather：規劃、派工、審查、修正、落地**<br><br>一個已登記的整台 Machine 協調角色，持續掌握 Session、Task、Wait 與 Landing 證據。它可以跨 Project 拆解工作，在某一個助理額度不足時改走其他路徑，要求獨立審查，並由 Root 保留整合責任。<br><br>[大型任務如何派工 →](docs/dispatching.md) · [驗證與審查 →](docs/verification-workflow.md) | <img src="docs/assets/clawdfather-loop.gif" width="380" alt="Clawdfather 協調有 Claims 的平行工作、獨立審查、修正與精確版本交付。"> |
| **整支艦隊，還有誰派了誰** `⌘K`<br><br>這台 Mac 上的每一個 Claude Code 與 Codex session 收在同一份清單——你自己開的那些，還有被某個 session 派出去的那些，縮排在叫它們出來的那個底下。一眼就看得到一排分頁講不出來的事：哪一個在等你，哪一個是別的 session 交代出去的差事。<br><br>[把工作派給另一個 session →](#把工作派給另一個-session) | <img src="docs/assets/fleet-phone.png" width="300" alt="手機上的 session 清單：每個母 session 底下都縮排著一個被派出去的子 session，一個跑 Claude Code、一個跑 Codex，在等你回答的那個用重點色挑出來"> |
| **哪一個 session 在等你** `⌘K`<br><br>正在跑的那個帶著 Claude Code 自己畫的那行字；螢幕上有問題沒人回答的那個會大聲說出來，因為那是唯一一種每過一秒都在賠錢的狀態。每一行還帶著它自己專案的像素圖示。<br><br>[每個狀態是怎麼判的 →](docs/interface.md#which-session-wants-you) | <img src="docs/assets/sessions-live.gif" width="380" alt="Session 清單，動起來：選取往下走，一個被回答了於是安靜下來、一個跑完、另一個開始發問"> |
| **把 session 讀回來** `⌘J`<br><br>不是終端機的截圖。Clawdline 讀的是那個 session 的逐字稿檔案，所以你拿到的是真正的訊息邊界、完整歷史、標題、有框線的表格與程式碼——跑完的工具呼叫各收成一行。`⌘F` 撐滿整個螢幕。<br><br>[那塊面板在做什麼 →](docs/interface.md#reading-a-session-back) | <img src="docs/assets/transcript.png" width="380" alt="逐字稿面板：標題、有框線的表格、程式碼區塊，是排版過的而不是刮畫面"> |
| **同一批 session，在手機上**<br><br>你的 Mac 開一個網頁，手機打開它，就讀得到每個 session 在做什麼、逐字稿也在裡面——第二個開關打開之後還可以打字進去。全新安裝是關著的、只綁 loopback、每一台裝置都要用只出現在 Mac 上的數字配對。要從外面連進來靠 `cloudflared`，那是你自己裝的程式。<br><br>[用瀏覽器，或用手機 →](#用瀏覽器或用手機) | <img src="docs/assets/web-wide.png" width="380" alt="同一個網頁在筆電上：左邊是 session 清單、在等你的那個用重點色標出來，右邊是它的逐字稿，下面是打字的框"> |
| **聽得懂中英夾雜的語音輸入**<br><br>講的時候字就出現，而且辨識器餵的是你自己的歷史紀錄，所以 `webhook`、`rebase` 被夾在中文句子裡講出來也活得下來。Claude Code 內建的 `/voice` 把聲音串到 Anthropic 的伺服器、需要 Claude.ai 帳號，而且[一種中文都不支援](docs/compatibility.md#claude-code-has-its-own-dictation-now)。再裝上 [Whisper](docs/whisper.md)——一行 `brew install` 加一個模型檔——停下來之後它會讀同一段錄音把整段換掉，一句話裡就裝得下兩種語言。<br><br>[你講話的時候它在做什麼 →](docs/interface.md#talk-instead-of-type) | <img src="docs/assets/voice.zh.gif" width="380" alt="對著輸入條說話：字邊講邊出現，停下來之後 Whisper 讀同一段錄音把它換掉"> |

**瀏海裡也會講。** 你的吉祥物住在鏡頭那塊：什麼都沒在跑的時候牠在那裡睡覺，有東西在跑就探出來，
有人在等你就講出是哪一個，長工作跑完就跳舞。螢幕沒有瀏海的話，它變成選單列下面的一顆藥丸。
`"notch": false` 整個關掉。[更多 →](docs/interface.md#the-notch)

<img src="docs/assets/island.gif" width="760" alt="選單列，還有切進去的那個瀏海：一個 session 在跑的時候吉祥物從鏡頭那塊探出來，變成三個的時候旁邊多一個數字，接著形狀往右邊長出去、用重點色講出在等你的是哪一個——最後長工作跑完，一個綠點和吉祥物在跳舞">

也有：

- **安全關閉必須有證據** —— Broker 會根據目前的 Process Identity、Task、Wait、Pending Landing、
  Handoff、Completion Delivery 與已宣告責任，判斷 Session 是被阻擋、需要 Attestation、可以安全關閉，
  或是證據不足。啟用自動關閉後仍使用 Compare-and-Swap Version；證據一旦改變，就會拒絕關閉。
  [契約 →](docs/session-closeability.md)
- **本機用量分析，不把未知假裝成零** —— 可以依 Model、Assistant、Origin、Project、日期、Coverage
  或 Task 篩選與分組。未知 Model 或帳務方案不會被硬算出價格；缺少來源的 Coverage 也不會和真正的零混在一起。
  [API →](docs/api.md#get-v1orchestratorusageanalytics-analyticscsv-analyticsjson)
- **開發用的那些伺服器，就在你已經在打字的地方** —— `⌘S` 列出每個專案的長跑行程、跑了多久、
  每個 port 都是連結，還有啟動／停止／重啟。**Clawdline 從來不自己開行程**，它跑的是你的 repo
  在 `.devstack.json` 裡宣告的指令。[格式 →](docs/devstack.md) · [怎麼導入 →](docs/devstack-adopting.md)
- **是哪個專案，不只是哪個任務** —— 輸入條會講出 repo、分支、未提交數量、進行中的部署、
  在另一個分頁跑著的那件長工作，還有 backlog 與那個專案自己的圖示與顏色；網頁與配對過的手機上
  再多一個 milestone。總共七種小檔案，一個專案有多少話要說就寫幾種。
  [格式 →](docs/project-status.md) · [七種都在這 →](docs/connect.md)
- **只要會跑上幾分鐘的事，都可以有一條進度條** —— 一輪測試、一次建置、一批資料匯入、一段長轉檔、
  你自己那支部署腳本：花時間的那個東西自己寫一個小檔案，輸入條就把它跑到哪裡畫出來，一個階段
  接一個階段，最後是一個勾或一個叉。它是一個協定，不是測試工具附帶的功能，所以隔壁分頁那個 AI
  也用得上——本來是四分鐘的沉默，現在看得到那件事走到哪裡了。
  [`clawdline-progress` 與格式 →](docs/project-status.md#a-long-local-operation-in-flight--run-pathjson)
- **終端機的分頁跟著走** —— 在清單裡移動，iTerm2 就跟著換分頁，而且不會跳到前景。輸入條瞄準的
  那個和你眼前那個分頁，不再是兩個不同的 session。
- **圖片與檔案** —— 視窗上任何地方都能丟檔案、也能貼圖。圖片進到 Claude Code 是 `[Image #3]`，
  跟你自己貼一張進去一模一樣；不是圖片的東西則以路徑送出。
  [怎麼做到的 →](docs/interface.md#dropping-in-a-file-or-an-image)
- **Claude Code 與 Codex 並排** —— 兩種都在同一份清單裡，用同一套方式判讀、收同樣的 prompt。
  只有清單裡兩種都有的時候，那一行才會標出自己是哪一種——因為在只跑其中一種的機器上，那個字會
  出現在每一行，等於什麼都沒分辨；而真的要標的時候，字旁邊還會掛上那一種的產品標誌。Codex 的
  session 也可以自己取名字。[這需要什麼 →](#codex-也在同一條輸入條裡)
- **這條 session 在做什麼，就叫它什麼** —— 一行的名字來自這段對話自己的紀錄：assistant 寫在
  transcript 或 thread metadata 裡的名字，或是這個分頁被派工時就帶著的任務標題。**不會是分頁
  標題**——那是終端機裡任何東西都能覆寫的東西，iTerm2 重開一次就讓十五行裡的十一行一起寫著
  `Default`。而上面兩種也都不見得是你會給它的名字。在 Session 資訊卡上按一下
  標題就能自己打：它排在所有自動名字前面，清空欄位就把自動的還給你。名字屬於**那一段對話**、
  不屬於那個分頁，所以同一個視窗裡開下一段對話，它又會用回自己的名字。Codex 那邊會立刻改到
  thread 上；Claude 則是在它閒著、而且沒有選單開著的時候才用 `/rename` 告訴它，忙的時候名字照樣
  在這裡生效——回應會說下游沒有跟著改，而不是暗示等一下會補上。
- **你的 skill，就在輸入框裡** —— 在 Claude Code 的 session 裡打一個 `/`，輸入條就列出那個工作
  目錄真的構得到的 skill，邊打邊篩：專案的、個人的、外掛的，照一個真的被打出來的指令會拿到的
  優先順序。只有名字跟描述，是從 `SKILL.md` 讀出來的——打開一份選單永遠不會去打開一個 skill 的
  內容。手機上是同一份清單。
- **記得你送過什麼** —— <kbd>↑</kbd> <kbd>↓</kbd> 翻自己送過的字，而同一批字就是語音辨識被告知
  要預期的詞。
- **按一下，不用再打一次** —— 在 session 標頭上按那個專案的像素圖示，就打開這個專案的常用句：
  你一天要打好幾次的那幾句，一句按一下。按下去是把字放進輸入框，送出仍然只有送出鍵做得到，
  所以手機在口袋裡誤觸，不會把 `commit, push, deploy` 送進錯的 session。東西存在 Mac 上，
  可以只給一個專案、也可以每個專案都有；手機走 relay 連進來一樣讀得到。
  [那個圖示會做什麼 →](docs/interface.md#the-mark-in-a-sessions-header) · [路由 →](docs/api.md#the-snippets-a-session-can-press)
- **換上你自己的吉祥物** —— 那隻角色就是一份 JSON：像素網格、色盤、七段動作。換一隻不必 fork
  任何東西。[格式 →](docs/mascots.md) · [圖庫 →](docs/gallery.md)
- **十四種語言** —— 英文、中文（繁體與簡體）、日文、韓文、西班牙文、葡萄牙文、法文、德文、
  俄文、義大利文、印地文、印尼文、土耳其文。介面跟著系統走，也可以在設定裡釘死一種。

> ### 把你自己的專案接上來
>
> 把這個 repo 的網址貼給你的 Claude Code agent，叫它把你的專案接上去。
> **[docs/connect.md](docs/connect.md) 就是寫給它看的**——一個專案能寫的七種檔案、格式是什麼、
> 以及怎麼驗自己有沒有做對。每一個整合都只是一個 Clawdline 會去讀的小 JSON 檔；它不會安裝任何
> 東西，也不會替你的專案增加任何相依。
>
> *「把這個專案接上 Clawdline —— https://github.com/sainteye/clawdline」* 這一句就夠了。

## 把工作派給另一個 session

你正在講話的那個 session，就是你正在等的那個；而有些事情——畫一張圖、跑一輪測試、看一份 diff——
其實不必在問它的那條對話裡做完。帶著 `clawdline` skill 的 session 把任務寫下來、請 app 去跑。
Clawdline 會開一個終端機分頁，把任務指定的那種助理啟動起來，把指示打進去，盯著子 session 的
回覆，把它花掉的算清楚，然後回頭跟發派的那個 session 說一聲。

<img src="docs/assets/dispatch.webp" width="760" alt="一個 session 把三張工作卡片交給另外三個，每一個各自跑去自己的機器上，最後一條線停在一支亮起來的手機">

**子 session 可以是任一種助理、任一個模型。** 同一個 Claude Code session 可以在同一口氣裡，把畫圖
交給一個 Codex 子 session、把 diff 交給一個 Claude Code 子 session，還可以指名機械性的活用
`haiku`、有人會照著做的判斷用 `opus`；app 需要知道的只有該起哪一支執行檔、
之後該讀哪一種螢幕，而這兩件它本來就知道——這正是一台 Mac 跑得起一支混編艦隊、中間卻不必架一層
框架的原因。而那張圖是真的畫出來的：Codex 內建影像模型，交回來的是一個 PNG，不是一段對圖的
描述。派工本身是一條純本機的 HTTP 路由，任何以你的身分在跑的程式都能要一個子 session；
而把任務寫下來的那個 skill [就在這個 repo 裡](skills/clawdline/)，出的是 Claude Code 版。

**一個你看不到的 agent 是背景工作；一個你看得到、答得了、也停得掉的，才是一個 session。**
所以這裡的子 session 不是佇列裡的一個 job id。它跟別的東西一樣是清單裡的一行，縮排在叫它出來的
那個底下，帶著自己的狀態、自己的逐字稿、自己的 token 數——在 Mac 上是這樣，在手機上也是，你可以
在那裡讀它在做什麼、回答它卡住的那個問題，或者把它結束掉。它花掉的東西按任務累計，算 token；
模型有公開價格的時候，也一起算成錢。

**派工有自己的一道門。** 它擋在一個只有本機行程讀得到的 `0600` 檔案後面，所以配對過的手機看得到
這些任務、卻永遠開不了新的——往一個 session 打字，跟再生出五個 session 來，本來就不是同一種
權限。而且**這棵樹只有一層**：一個 session 同時可以派五個，而**子 session 一個都不能再派**——它裡面
想並行的工作交給那個助理自己的 subagent，不佔分頁、不經過 broker。整台 Mac 同時二十個派出去的
終端機，已經是一個人盯得完的上限；沒有這個底，它就是一顆裝了語言模型的 fork bomb。

**派工的規矩是你自己改的檔案——如果這台機器有話要說，那就是兩份。**
`~/.config/clawdline/dispatch-policy.md` 是 base，旁邊選用的 `dispatch-policy.local.md` 寫的是
只有這裡成立的事；兩份每次派工都會重讀，合成之後進到**每一個**子 session 的指示裡，local 排在
最後，所以更具體的那些規則會贏：哪種工作給哪個助理、哪種工作值得用哪個
模型、一件任務該切多大、小事什麼時候該累積成一批而不是各派各的、整張圖希望長什麼形狀。出廠的
那一份就是這個 repo 裡的 [`Resources/dispatch-policy.md`](Resources/dispatch-policy.md)，所以你
可以先讀、先不同意，再決定要不要裝；裝上之後那份是你的，內容刪光就等於沒有規矩。每一件任務還會帶著
一份 `plan`，也就是它所屬的整張圖——葉節點知道自己的答案要餵給誰，才寫得出接得起來的東西，
而不是一篇沒人要的心得。

**[docs/clawdline-protocol.html](docs/clawdline-protocol.html)** 是整份協定寫成一頁，讀者是剛裝好
這個東西的人：一件任務怎麼派出去、claims 和 file wait 各自保證了什麼、為什麼 landing 是「誰派工誰
負責」、每一條承諾值多少。從 checkout 直接打開就能看，不需要網路。英文。
**[docs/orchestrator.md](docs/orchestrator.md)** 是同一份協定的參考手冊版：檔案格式、憑證、
整個生命週期，以及帶著 `curl` 紀錄的路由說明。
**[docs/dispatch-permissions.md](docs/dispatch-permissions.md)** 是會咬人的那一半：被派出去的
session 會在哪四個地方停下來問、其中哪兩道任何設定都到不了，以及那個讀起來像「放手去做」的
flag，為什麼在最便宜的模型上意思正好相反。

## 多個 session 共用一份工作區

派工會把一個清單畫不出來的問題放大。同一個 repo 裡的兩個 session，就是同一份工作區上的兩個寫入
者，而它們看不到彼此。一個把另一個還在打的半成品 stage 了進去；同一套測試同時跑兩輪，兩份執行檔
寫到同一個固定路徑上互相蓋掉。當下不會有任何東西報錯，你拿到的是一小時前還在、現在不見了的東西。

這兩個 session 誰都沒有足夠的資訊去阻止它。app 有——每一次派工都經過它，而它本來就知道別的任務
在哪個目錄工作、每一個宣告過自己會動到什麼。

**那個資料夾裡有別人的時候，它會講。** 派進一個「已經有另一個 root 的任務在做事」的目錄，任務照
樣會開，因為同一個 repo 裡同時開著兩個 session 是再平常不過的事，不是錯誤——但回覆裡會多一則點名
那個任務的警告，而對面那個 root 也會收到一行講你這個；兩邊都宣告了彼此錯開的 claims 時，誰也不會
被吵——精確的答案取代粗略的那個。先讓你看得到，才談要不要擋。

**任務可以先講出自己要寫哪些路徑，撞到的那一個在門口就被擋掉。** `claims` 是一串相對於專案的路徑
——`["Sources/Orchestrator.swift", "docs"]`——寫一個目錄，底下整棵都算。如果某個 app **認得出來的別的 root** 的
任務還活著，而它宣告過的路徑跟你的相等、包住你的、或落在你的底下，這次派工在分頁被開出來以前就
被拒絕；而值錢的是那個拒絕本身：是哪一個任務佔著、那是誰的、什麼時候開始的、每一條撞到的絕對
路徑，還有建議你等多久再來。同一個 root 底下的兩個任務允許重疊，只會收到警告——那張圖是那個 root
自己畫的，先後順序也可能是它自己排的。

**不是檔案的東西，也可以排隊。** 有些衝突根本沒有路徑可以宣告：這個 repo 的 `./test.sh` 會把測試
執行檔寫到一個固定位置，所以兩次跑起來就算共同的原始檔一個都沒碰到，照樣會蓋掉對方。`serialize`
就是拿來點名這種操作的——`["build"]`——要同一個名字的任務按建立時間排隊，而且要幾個名字就一次拿
齊，所以兩個任務不會因為順序交叉而互相卡死。

**它到哪裡為止，這裡直接講。** claims 是派工當下的一道門，不是檔案系統上的鎖：一個不照指示走的
子 session，照樣寫得到它沒宣告過的地方。這道門收掉的是那段空窗——兩個 session 都已經被交代完、
都已經在改，然後兩邊的人要為一件做到一半的事情喬出個結果。而且它仲裁的是**任務**：
你自己在分頁裡開的那個 session 從來沒有被派工過，上面這些完全看不到它——那就是下一節在講的事。

**另外，不同的工作線請放在並排的 checkout，不要把一個 repo 巢狀塞進另一個裡面。** 想把私有的
那一半放進公開 checkout 裡——外層 repo gitignore 掉的一個資料夾、自己帶一份 git 歷史——很誘人。
我們試過；上面這整套機制都看不見它。worktree 隔離複製的是被追蹤的檔案，被外層 ignore 的目錄
不在其中——所以一個被隔離進 worktree 的任務，抵達時身上偏偏沒有它要改的那個資料夾；外層 repo
的 branch 也永遠載不動巢狀裡的工作。claims 還是守得住路徑，但那是拿粗的工具做細的活。兩個並排
的 checkout 讓每個 repo 都有自己完整的 worktree、claims 跟 session，代價只是不能在同一個資料夾
裡同時改兩邊。

**commit 那一刻也有一道門。** claims 是派工當下的門；等到有人真的打下 commit，這個 repo 自己的
`pre-commit` 守衛會把 claims 讀回來，擋掉夾帶別的 session 活著的任務所宣告的路徑的 commit，也擋掉
去收尾一個別人手動解完衝突的 merge。它預設是關的，要自己打開——`sh tools/install-git-hooks.sh`。

[claims 怎麼算、什麼時候放掉、佇列怎麼排 →](docs/orchestrator.md#reserving-declared-write-paths-at-dispatch)
· [commit 守衛 →](docs/shared-tree-guard.md)

## agent 一進門就讀到的規矩

派工的那道門有 app 站著。你自己開的那個終端機分頁，門口沒有人站——而一份工作區裡大部分的 session
都是這一種。能傳到它們手上的，只有它們開始讀的時候樹裡本來就有的東西。所以規矩就寫在樹裡。

這個 repo 的操作規則在 [`AGENTS.md`](AGENTS.md)，不含實作細節的專案詞彙與文件指標則在
[`CONTEXT.md`](CONTEXT.md)。前者是一份共用工作區真正需要的規則，不是 coding style：你進來的時候
就已經沒 commit 的東西，全都是別人還沒做完的活；stage 要逐檔指名，永遠
不要 `git add -A`；commit 之前要讀 staged diff 本身，不能看 `--stat` 很乾淨就算了；被派去做事的
session 不自己 commit，改完交回去；不要跑 build，因為它會把使用者正在用的那個 app 換掉；還有，
任務會動到的每一條路徑都要寫進 `claims`。[`CLAUDE.md`](CLAUDE.md) 只有一行，指向那份檔案——兩種
助理找的是不同的檔名，而同一套規矩不該維護兩次。

**Clawdline 這兩個檔案一個都不讀。** 讀它們的是助理自己，在它們被打開的任何一個 repo 裡——這正是
這個做法值得抄過去、而不是「安裝」起來的原因：你自己專案根目錄下的兩個檔案，沒有相依，也沒有東西
要復原。該寫進去的，是一個新的 session 沒辦法從程式碼推出來的那些事：哪些改動不是它可以碰的、
要怎麼 stage、什麼東西絕對不能跑。兩個檔案都沒有的 repo 一樣照常運作，只是對下一個打開它的人
沒有話要說。

### 把 Clawdline 規矩放到每個 project 都讀得到的地方

一個 repo 的 `AGENTS.md` 或 `CLAUDE.md`，只會傳到在那個 repo 裡打開的 agent。如果這些 Clawdline
操作規則應該跟著 agent 跨 project 走，就在 `~/.codex/AGENTS.md` 放一個從
`<!-- clawdline rules: begin -->` 到 `<!-- clawdline rules: end -->` 的 block；Claude Code 則把
對應的 block 放進 `~/.claude/CLAUDE.md`。Canonical 文字在這個 repo 的 `AGENTS.md` 裡：
[localhost failure 規則](AGENTS.md#prove-a-localhost-failure-before-calling-clawdline-offline)與
[recurring stall 規則](AGENTS.md#repeated-communication-stalls-require-a-capacity-and-protocol-audit)；
下面的短版保留同一組要求。Project-local instructions 可以覆寫這些全域預設。Clawdline 與
`install.sh` 都不會修改這兩個全域檔案；加入或更新這個 block 是一個明確的 setup step。

那個 block 裡要留住這兩條：

- **先證明 localhost 真的失敗，才能說 Clawdline unavailable。** Restricted sandbox 連不上
  `http://127.0.0.1:7717`，不能證明 service down。先讀目前設定的 port，再到獲准連 loopback 的
  execution environment，用同一個最小、唯讀的 `GET /v1/health` request 複驗；只有這個 permitted
  request 仍然失敗，才能稱 service unavailable。這是 agent 操作規則，不是要求人類關掉 sandbox：
  需要額外 localhost 權限時，照 provider 正常的 approval flow 取得。也不能因為一次 Clawdline
  dispatch 失敗，就改用 provider-native child session，卻把它說成 Clawdline task。
- **反覆發生的通訊卡頓，要從頭到尾 audit。** Slow send、loading state、pending message 或 event loss
  一再出現時，只改 timeout 或 spinner 並不算結案。要追 connection 與 queue ownership、queue 與
  concurrency bounds、backpressure、synchronous external calls、retry amplification、idempotency 與
  delivery receipts、SSE revision 與 resume、stale snapshots，以及 failure isolation；並把
  `accepted`、`executed`、`delivered`、`observed`、`acknowledged` 分清楚，不能拿一次 HTTP response
  當成五種 state 全都成立。

## 安裝

從零開始到第一個 Session、Shell 與終端機設定、瀏覽器／手機、Clawdline Cloud、E2EE 與疑難排解，
請看完整的**[繁體中文公開使用手冊](https://clawdline.com/docs)**。先選你想怎麼安裝；兩條路裝到的是
同一個 Mac app。

### 交給 AI 安裝（建議）

把下面這段完整貼給已經在這台 Mac 上執行的 Claude Code 或 Codex：

```text
請幫我安裝 Clawdline。先閱讀 https://clawdline.com/docs/install 與
https://github.com/sainteye/clawdline/blob/main/install.sh，確認這台 Mac 符合需求，
說明你準備採用的安裝方式與會執行的指令，再完成安裝並驗證 Clawdline 能開啟。
不要變更我的 Claude Code、Codex 或專案設定。
```

AI 會依這台 Mac 的狀況選擇安裝方式；遇到 macOS 權限或需要你確認的系統動作時，仍會停下來請你操作。

### 自己手動安裝

以下選一種即可。第一次安裝建議用可先檢查內容的安裝腳本。

**用腳本抓最新版**

```sh
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh -o install.sh
less install.sh          # 先看過 Shell 即將執行的完整腳本
bash install.sh          # 或 bash install.sh ~/Applications
```

**手動** —— 從 [Releases](https://github.com/sainteye/clawdline/releases/latest) 下載 `.zip`，
解開丟進 `/Applications`。**如果下面那個判準說 `adhoc`**，才需要清一次隔離屬性：

```sh
xattr -dr com.apple.quarantine /Applications/Clawdline.app
```

**自己編** —— 沒有套件管理器、沒有相依套件，幾秒鐘：

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline && ./build.sh
open ~/Applications/Clawdline.app
```

> **你需不需要那行 `xattr`？** 問你下載到的那個 build，不要問這一頁——答案會隨版本改變，
> 而一頁文件不會：
>
> ```sh
> codesign --display --verbose=2 /Applications/Clawdline.app 2>&1 | grep -E 'Authority|Signature'
> ```
>
> 出現 `Authority=Developer ID Application: TsunamiWorks Co., Ltd.` 代表它有簽章也公證過：
> 開起來跟任何一個下載來的 app 一樣，那行 `xattr` 對你沒有作用。出現 `Signature=adhoc` 代表沒有，
> macOS 會拒絕開它，直到隔離屬性被拿掉。
>
> **v0.6.0 以及更早的每一版都是 ad-hoc**，所以今天那行是需要的。原因不是缺一個 Apple 帳號——
> 憑證是有的，`tools/release.sh` 整支就是繞著它寫的，`notarytool submit --wait`、stapled ticket、
> `spctl --assess` 三關少一關它就拒絕發布。只是還沒有任何一版走過那條路，
> 而第一個走過的版本會在上面回答 `Authority=`。`install.sh` 已經替你做這個判斷，
> 只有在答案是 `adhoc` 時才會去拿掉隔離屬性。
>
> 自己編出來的 app 從來沒被下載過，所以自己編完全不會碰到這件事。

第一次送出時，macOS 會問要不要讓 Clawdline 控制 iTerm2，按**好**——沒有這個權限它什麼都送不
出去。選單列的 ✳ → **開機時啟動** 可以讓它常駐。

### 開箱就會動的，跟你自己打開的

第一次跑起來，輸入條就是整個產品：session 已經在那裡了、熱鍵已經送得出去、
<kbd>⌘</kbd><kbd>J</kbd> 已經讀得回來。除此之外的每一樣都是關著的，等你自己去打開，順序隨你。

| | 在哪裡打開 | 對應的說明 |
| --- | --- | --- |
| **輸入條**——看得到、送得出、讀得回來 | 不用打開；macOS 只在第一次問一次 iTerm2 | — |
| **你自己專案的那一行**——伺服器、分支、圖示、部署、本機正在跑的那件長工作、backlog；網頁與配對過的手機上再多一個 milestone | repo 內外七種小 JSON 檔；把這個 repo 的網址貼給 agent，它會幫你寫 | [connect.md](docs/connect.md) |
| **那個網頁，在這台 Mac 或手機上** | 設定 → 遠端 →「讓瀏覽器或你的手機看得到你的 session」，再按「用瀏覽器打開」或「配對手機……」 | [remote.md](docs/remote.md) |
| **讓配對過的裝置打字** | 設定 → 遠端 →「讓配對過的裝置寫進 session」 | [remote.md](docs/remote.md) |
| **從外面連進來** | 設定 → 遠端 →「從任何地方連到這台 Mac」；它跑的 `cloudflared` 是你自己裝的那支 | [remote.md](docs/remote.md#the-tunnel) |
| **把工作派給另一個 session** | 同一個「讓瀏覽器⋯⋯」開關，然後把 [skill](skills/clawdline/) 那兩個檔案挑一個放進 `~/.claude/skills/clawdline/` | [orchestrator.md](docs/orchestrator.md#the-skill) · [dispatch-permissions.md](docs/dispatch-permissions.md) |
| **讓一個 session 說自己這輪做完了** | 接手 handoff 的話什麼都不用做——那句話在包裹裡。其他情況裝上面那個 skill，另外可以選擇性地在全域 `CLAUDE.md` 加[一行](docs/orchestrator.md#and-one-optional-line-in-your-global-claudemd) | [orchestrator.md](docs/orchestrator.md#the-skill) |
| **一秒內就知道有人在等你回答，而不是二十秒** | 設定 → Claude Code Hook →「安裝」 | [hooks.md](docs/hooks.md) |
| **一句話裡裝得下兩種語言** | 一行 `brew install`，加上它要讀的模型檔 | [whisper.md](docs/whisper.md) |

**派工跟那個網頁共用同一個本機開關，但用的不是同一把鑰匙。** 那個開關負責的是在 `127.0.0.1` 上
開一道門；替派工把這道門打開的，是你家目錄下一個 `0600` 的檔案，它會自己寫出來，配對過的裝置從來
沒拿到過，網頁也讀不到。只為了派工把開關打開，不會在你的網路上放任何東西：listener 只綁 loopback，
tunnel 是另一個開關，而且在配對過任何裝置之前它拒絕啟動。

**「共用一份工作區」這件事本身沒有開關。** 資料夾警告每一次派工都會做；`claims` 與 `serialize`
是任務自己填的欄位；`AGENTS.md` 是你 repo 裡的一個檔案，這個 app 從來不讀它。剩下真的要調的，是
一次能派幾個、每個又能再派幾個，以及子 session 走多遠才停下來問——在設定 → 遠端的 Agent tasks
那幾列，或[設定裡的 `orchestrator_*`](#設定)。

## 用法

在 iTerm2 裡按 <kbd>⌥</kbd><kbd>Space</kbd>，打字，按 <kbd>Enter</kbd>。

<img src="docs/assets/demo.gif" width="760" alt="按 ⌥Space、打字、按 Enter，訊息就進到 Claude Code，而終端機不必被叫到前景">

| 按鍵 | 做什麼 |
| --- | --- |
| <kbd>⌥</kbd><kbd>Space</kbd> | 叫出／收起輸入條 |
| <kbd>Enter</kbd> | 送到目前的目標 |
| <kbd>⇧</kbd><kbd>Enter</kbd> | 換行 |
| <kbd>Tab</kbd> / <kbd>⇧</kbd><kbd>Tab</kbd> | 下一個／上一個 session |
| <kbd>⌘</kbd><kbd>K</kbd> | 展開 session 清單 |
| <kbd>⌘</kbd><kbd>1</kbd>…<kbd>⌘</kbd><kbd>9</kbd> | 直接跳到第 N 個 |
| <kbd>↑</kbd> / <kbd>↓</kbd> | 輸入框空的時候翻歷史 |
| <kbd>⌘</kbd><kbd>J</kbd> | 把那個 session 讀回來 |
| <kbd>⌘</kbd><kbd>F</kbd> | 把它撐滿整個螢幕 |
| <kbd>⌘</kbd><kbd>R</kbd> | 最新的訊息放最上面 |
| <kbd>⌘</kbd><kbd>+</kbd> / <kbd>⌘</kbd><kbd>−</kbd> / <kbd>⌘</kbd><kbd>0</kbd> | 那塊的字級 |
| <kbd>⌘</kbd><kbd>S</kbd> | 這個專案的伺服器 |
| <kbd>⌘</kbd><kbd>L</kbd> | 用說的代替打字 |
| <kbd>⌘</kbd><kbd>M</kbd> / <kbd>⌘</kbd><kbd>D</kbd> | 切換吉祥物／叫牠跳舞 |
| <kbd>⌘</kbd><kbd>/</kbd> | 展開其餘的快速鍵 |
| 拖曳 或 <kbd>⌘</kbd><kbd>V</kbd> | 丟檔案、貼圖片 |
| <kbd>Esc</kbd> | 關掉 |

熱鍵只在你的終端機在前景時生效；在其他 app 裡，<kbd>⌥</kbd><kbd>Space</kbd> 還是你裝這個之前
的那顆鍵。把 `"scope_app"` 設成 `""` 就變全域。

**輸入條的下緣一定會寫出它瞄準的是誰。** 它不會盲送——一個不肯告訴你字會送去哪的輸入框，比沒有
輸入框更糟。[目標是怎麼選的 →](docs/interface.md#which-session-it-sends-to)

## 先選你要在哪裡操作

這裡有兩個目的。用電腦時走本機瀏覽器；用手機時，再選 Clawdline.com 或自己管理的
Cloudflare Tunnel。三條路最後看到的是同一台 Mac 上的同一批 Session。

```text
我要操作本機的 Clawdline
├─ 用這台電腦的瀏覽器 → 本機瀏覽器（不需要帳號）
└─ 用手機
   ├─ 透過 Clawdline.com → Clawdline Cloud（帳號 + E2EE；目前為 preview）
   └─ 使用自己的網址 → Cloudflare Tunnel（自己管理 Cloudflare 與網域）
```

### 目的 1：用這台電腦的瀏覽器操作

在**設定 → 遠端**打開「讓瀏覽器或你的手機看得到你的 session」，再按**用瀏覽器打開**。Clawdline
會替這個瀏覽器建立一個裝置憑證，開啟 `http://127.0.0.1:7717`，並自動登入。不需要 Clawdline
帳號、Cloudflare 或手機配對。

### 目的 2：用手機操作

手機有兩條互相獨立的連線方式：

- **透過 Clawdline.com**：登入 Clawdline Cloud，在 Mac 的**設定 → 遠端 → Clawdline Cloud**
  連接這台 Mac，再由已信任的裝置完成手機配對。這條路讓 Relay 只承載簽章過的密文；目前 Cloud
  帳號仍是 preview，尚未全面開放。完整流程見 [Clawdline Cloud](docs/cloud.md)。
- **透過自己的 Cloudflare Tunnel 網址**：先開啟本機瀏覽器並建立至少一個配對裝置，再安裝
  `cloudflared`，於**設定 → 遠端 → 從任何地方連到這台 Mac**選擇臨時網址或**我的自訂網域**。
  用手機開啟該 HTTPS 網址後，再完成手機配對。自訂網址、Tunnel 與 Cloudflare 帳號都由你管理；
  完整設定見 [Named Tunnel](docs/remote.md#named--your-own-domain)。

不論選哪一條手機路徑，「看得到」和「可以操作」仍是兩個權限。只需要監看時不要打開「讓配對過的
裝置寫進 session」；要從手機送訊息、開啟或結束 Session 時才打開它。

**那個頁面是同一支艦隊，不是刪減版。** 這台 Mac 看得到的每一個 session 都在上面，分組方式一樣
——被派出去的子 session 縮排在叫它出來的那個底下——每一個都帶著自己的逐字稿、自己的狀態、卡住
的那個問題，還有一個回答它的框。手機只要能打字，就也能開一個 session、結束一個 session；它唯一
做不到的是派工，那是[另一把這台 Mac 從來不往外送的憑證](#把工作派給另一個-session)。

**也可以接著上次那條。** *開一個 session* 上有一個勾選框，勾了之後專案清單會變成你在那個專案裡
談過的對話——用 Claude Code 取的名字排出來，打字就能篩——按下去的那一列是新分頁裡的
`claude --resume`，不是一段新的對話。**是「你談過的」**，這比硬碟上有的那些少：這個 app 自己派
出去做事的 session、還有 `claude -p` 的一次性執行，兩種都不列，而且各自是靠一個欄位判斷的，
不是從內容猜的。名字是從逐字稿讀出來的，不是編的；而正在被寫的那一條會
自己說出來，按它是跳到那個 session 而不是再開一個：同一份逐字稿上兩個行程是一份壞掉的紀錄，
不是第二個意見。

<img src="docs/assets/web.gif" width="300" alt="手機上的那個網頁：六個 session、各自帶著專案的圖示，在問你話的那個用重點色挑出來。換另一個 session 的逐字稿，兩個工具呼叫摺成一行，點開才展開；最後在下面的框打一句話送出去">

**全新安裝是關著的**，要你自己去打開——一個在聽的 socket，是「你機器上的一支程式」跟
「你機器上的一個服務」的差別。

照一個請求碰到它們的順序，中間擋著這些東西：

- **只綁 loopback。** 這個 listener 建立時就要求 local endpoint 是 loopback，所以你的網路上
  根本沒有一個介面可以找到它。出去的路是一條**往外撥**的 tunnel，不是一個坐在那裡等的 port。
- **`Host` 最先被檢查。** DNS rebinding 唯一改不掉的東西就是 `Host`，所以只要 `Host` 不是這台
  伺服器認得的名字，請求當場就被擋掉。
- **跨站請求一律不回**，依據是 `Sec-Fetch-Site`——網頁偽造不了的那個標頭。會改東西的請求還要
  再過一次 `Origin`。
- **其餘每一條路都要有裝置 token** —— 256 個隨機位元，只存 SHA-256、用固定時間比對。
  **loopback 沒有例外**：tunnel 一開，從別的國家用手機發的請求也是從 `127.0.0.1` 進來的。
- **配對需要你看得到螢幕。** 那六位數字出現在 Mac 上，從來不在發問者拿到的回應裡。五次機會、
  兩分鐘、同一時間只有一組。
- **讀跟寫是兩個開關。** 讀會交出 repo 名稱與任務標題；寫則是遠端執行程式碼，因為 Claude Code
  會跑 `bash`。
- **在配對過任何裝置之前，tunnel 拒絕啟動**，而且每一次配對、撤銷、送字都會追加寫進
  `~/.config/clawdline/remote-audit.jsonl`。

配對過的裝置還可以訂閱通知，在有 session 開始等你的時候震一下。訊息是對那台裝置加密封起來的，
裡面會標出 session 任務標題、專案和狀態。打開送字之後，它也可以在這台 Mac 已經工作過的目錄裡開一個
新的 session：用戶端從來不送路徑，只送一個 Mac 自己列出來的不透明 id。

打開送字之後，它還可以**關掉一個 session**——助理先走它自己的 `/quit` 或 `/exit`，它佔著的終端機
分頁跟著收掉，兩步併成一個動作，因為助理一離開，那個裸的 shell 就從清單上掉下去，頁面也就沒有東西
可以關了——也可以**把某個 session 的分頁叫到 Mac 最前面**，而頁面不需要知道它在哪裡。在頁面上打
`/`，開的是跟輸入條同一份 skill 選單，走的是同一條只給 metadata 的路由。

**裝了 [Whisper](docs/whisper.md) 之後，手機也可以用說的把字打進那個框。** 送出鍵旁邊那支麥克風
負責錄，你的 Mac 用它上面已經有的模型讀回來，字落在框裡，送出去之前你都還能改——引擎、語言、
詞彙表都跟輸入條自己的語音輸入同一套，聲音也不會離開你的 Mac。每一支手機本來就有一個藏在權限
提示後面的辨識器，用它的代價就是那句話本身。手機上不會邊講邊出字，因為 Whisper 讀的是錄完的
檔案而不是一條串流：你看的是碼表，不是字。它需要一個 https 網址（開 tunnel 就有），而且被關在
送字那個開關後面——只能讀的裝置，就算轉出一句話，也沒有地方可以放。

**[docs/remote.md](docs/remote.md)** 是完整的威脅模型，包含它**不**防什麼。
**[docs/api.md](docs/api.md)** 是腳本或外掛講話的那層 HTTP 介面：每個 session、每份逐字稿、
一條事件流，而 `curl` 就是唯一的 SDK。

## 它是怎麼運作的

**讀。** Clawdline 把每一個 iTerm2 session 與 tmux pane 列出來，拿每一個的 TTY 去對 `ps`，
留下真的在跑 `claude` 或 `codex` 的那些。狀態來自每個 session 自己的螢幕——有轉圈那行就是在跑、
選單上停著游標就是在等回答，而**讀不到的螢幕回報的是「不知道」而不是「閒置」**，因為把「不知道」
畫成「閒置」，是對別人的工作下一個很有信心的錯誤判斷。磁碟上有紀錄檔的時候，
<kbd>⌘</kbd><kbd>J</kbd> 那塊讀的是檔案而不是螢幕。

**寫。** 不是模擬鍵盤，也不是寫進終端機的 pty——現代 macOS 上你寫不進別的行程的 TTY。走的是
iTerm2 的 scripting 介面（tmux 則是 `load-buffer` ＋ `paste-buffer`），並且包在括號貼上裡：

```
ESC[200~ 你的文字，含換行 ESC[201~     ← 當成一次貼上，不是連按好幾次 Enter
CR                                     ← 再單獨送一個 Return 才送出
```

少了那層包裝，兩行的 prompt 會在第一行就自己送出去。另一個好處是終端機完全不必被叫到前景——
那正是這整個工具的重點。

**選配的 hook。** 人不在輸入條前面的時候，判讀每二十秒才做一次，所以一個權限對話框可能會在那裡
坐上一陣子沒人發現。**設定 → Claude Code Hook → 安裝** 會在 `~/.claude/settings.json` 放九組
matcher 設定，分屬八個事件；之後只要一輪對話開始、結束或需要回答，通知當下就到，判讀在**一秒內**
發生。一則通知只說「什麼時候該去看」，從來不說螢幕上寫了什麼，所以說了算的仍然是螢幕。把 hook
移除不會留下任何東西。[完整的約定 →](docs/hooks.md)

## Codex 也在同一條輸入條裡

Codex 的 session 跟 Claude Code 的在同一份清單裡，四件事都一樣：**看得到、讀得到、送得出去、
開得起來。** Codex 裡面同樣沒有安裝任何東西——一樣是讀它本來就在畫的、本來就在寫的。

| | |
| --- | --- |
| **看得到** | TTY 上在跑 `codex` 的就是一個 session，不管那是原生的執行檔，還是官方那層會再生一個原生行程的 Node 包裝。`codex exec`、`codex sandbox` 跟那幾個 server 是同一個執行檔在做你打不進去的事，所以它們不會進清單，而不是被當成一個可以派工作的地方。**「不進清單」是順著行程樹往下傳，不是橫著蓋掉整個 tty：** 現在的互動式 Codex 會在自己的 UI 旁邊開一個 `codex app-server`，而一個被排除的子行程，不該把生它出來的那個父行程一起拖下去。 |
| **讀得到** | <kbd>⌘</kbd><kbd>J</kbd> 讀的是 Codex 正在寫的 rollout——`~/.codex/sessions/YYYY/MM/DD/rollout-….jsonl`——並排成跟 Claude Code 逐字稿一樣的一段對話。**哪個檔屬於哪個 session 是事實，不是猜的：** Codex 的行程會一直開著自己那個 rollout，所以直接去問它就好，這正是同一個目錄裡兩個 session 不會互相顯示對方內容的原因。它派出去的 subagent 也會在同一個資料夾寫自己的檔，那些靠 Codex 寫在第一行的欄位分辨。 |
| **送得出去** | 同一套括號貼上、同一個單獨的 Return。螢幕上有問題在等的時候判讀方式也一樣——游標底下的編號選項——手機上按一個數字就能回答，這是拿真的對話框試出來的，不是假設的。 |
| **開得起來** | *開一個新的 session* 會依這台 Mac 實際裝了哪幾種來給選項，按下的那一列就開在那裡。從手機來的時候，要開哪一種是路徑上的一個**名字**——`POST /v1/places/:id/start/codex`——比對一份只有兩個值的清單，永遠不是一段會被送出去執行的指令。 |

有一件事要講清楚：**背景 agent 的數字只有 Claude Code 那邊有。** Codex 一樣會派 subagent 出去，
但 Clawdline 的計數來自一個只有 Claude Code 會寫的目錄，所以一個派了三個出去的 Codex session，
看起來就像在想一句話想很久。

Codex 用 `/quit` 結束，Claude Code 用 `/exit`，而且兩邊都不吃對方那個字——這就是「結束」得先知道
自己在跟誰講話的原因。如果你的 Codex 不在 `~/.codex`，在設定裡填 `codex_home`；從 Finder 啟動的
app 看不到你的 `CODEX_HOME`。

**可選的自動命名。** 在設定裡打開「自動命名新的 session」，Clawdline 會在第一則需求出現後，
請設定的小型 Codex 模型取一次標題。Codex 會直接使用；Claude Code 通常會自己寫名稱，只有第一輪
結束後仍然沒有名稱時，Clawdline 才會啟動 fallback。那個 helper run 是 ephemeral、使用 low
reasoning、工具全關，而且絕不蓋掉你或 Claude Code 已經取的名稱。預設關著，因為每次 fallback
都是真的 Codex turn，也會消耗 Codex 額度。

實際跑過並拿來用的版本是 **Codex 0.149.0** 與 **Claude Code 2.1.235**。兩邊的螢幕都不是承諾過的
介面：[讀了哪些東西、變了你會看到什麼 →](docs/compatibility.md)

## 其他終端機：把 Claude Code 跑在 tmux 裡

Terminal.app、Warp、Tabby、Ghostty、Alacritty、Kitty 全部都能用，只要 Claude Code 跑在 tmux 裡：

```sh
tmux new -s work
claude
```

設定就這樣，而且 **tmux 完全不需要任何 macOS 權限**——它是普通的子行程，不是跨 app 自動化。
如果你的終端機不是 iTerm2，記得把熱鍵範圍放寬，讓 <kbd>⌥</kbd><kbd>Space</kbd> 在那裡也生效：

```json
{ "scope_app": "com.apple.Terminal,com.googlecode.iterm2" }
```

**為什麼不直接支援那些終端機？** 因為它們收不到文字。Terminal.app 的 `do script` 回報成功，
但一個卡在 `read` 的程式一個位元組都沒收到；Warp 與 Tabby 連等價的介面都沒有。剩下的路只有模擬
鍵盤，那需要輔助使用權限——一個工作只是開一個輸入框的工具，去要「看見你每一次按鍵」的能力——
而且必須把終端機叫到前景，那正是這個工具要避開的事。

## 設定

選單列 ✳ → **設定⋯** 裡每個值得調的東西都有一個控制項，而且動一下就生效。底下還是
`~/.config/clawdline/config.json`，它仍然是真相、仍然可以手改。App 在跑的時候直接改那個檔沒問
題：它只寫回自己動過的東西。

**輸入條**

| 鍵 | 預設 | |
| --- | --- | --- |
| `hotkey` | `option+space` | cmd / option / control / shift ＋ 一個鍵 |
| `scope_app` | `com.googlecode.iterm2` | 逗號分隔多個；`""` ＝ 全域生效 |
| `terminal` | `auto` | 新 session 開在哪個終端機：`auto`、`iterm`、`tmux`——和熱鍵的生效範圍是兩回事 |
| `y_fraction` · `width` | `0.30` · `720` | 輸入條落在哪、有多寬 |
| `language` | `auto` | 或任一個標籤：`ja`、`pt`、`zh-Hant`⋯ |
| `mascot` · `notch` | `clawd` · `true` | 角色，以及要不要住在瀏海裡 |
| `follow_target` | `true` | 終端機的分頁跟著輸入條的目標走 |
| `tmux_path` | `""` | 空的 ＝ 去常見位置找 |
| `codex_auto_name` | `false` | 從第一則需求替新的 session 命名；Claude 只在自己的名稱缺席時才用 |
| `auto_name_assistant` | `codex` | `codex` 或 `claude`；由哪個已安裝的助理消耗命名 turn |
| `codex_auto_name_model` | `gpt-5.6-luna` | `auto_name_assistant` 是 `codex` 時使用的模型 |
| `codex_home` · `codex_path` | `""` | Codex home 或執行檔不在慣用位置時覆寫 |

**讀一個 session**

| 鍵 | 預設 | |
| --- | --- | --- |
| `output_mode` | `auto` | `auto` · `transcript` · `terminal` |
| `output_font` | `Menlo` | 配合你的終端機，不然方框字元會跑掉 |
| `output_height` · `output_size` | `340` · `11.5` | 那塊的高度與字級 |
| `output_newest_first` | `false` | <kbd>⌘</kbd><kbd>R</kbd> |
| `card_opacity` · `backdrop` | `0.55` · `0.5` | 玻璃與模糊；背景很亮的時候調高 |
| `reopen_on_return` | `true` | 切回終端機時自己回來 |

**語音、檔案、整合**

| 鍵 | 預設 | |
| --- | --- | --- |
| `voice_settle_seconds` | `1.8` | 多長的停頓算一句話結束；0 ＝ 關掉 |
| `voice_stop_seconds` | `4.0` | 多長的安靜算整段講完 |
| `voice_vocabulary` | `[]` | 辨識器不可能知道的專有名詞 |
| `voice_language` | `auto` | 釘住語言；`voice_engine` 與 Whisper 那幾個鍵在 [whisper.md](docs/whisper.md) |
| `send_images_as_paste` | `true` | 圖片以 `[Image #3]` 進去，而不是路徑 |
| `hooks` | `true` | 裝了 Claude Code hook 之後要不要相信它 |
| `session_registry` | `true` | 要不要相信每個 Claude Code session 自己寫下的狀態 |
| `on_state_change` | `[]` | session 換狀態時去跑你自己的程式——是 argv 陣列，不是一行 shell。[它會被告知什麼 →](docs/notifications.md) |
| `status_dir` · `icons_file` | `""` | 專案狀態檔與圖示登錄檔 |

**遠端**

| 鍵 | 預設 | |
| --- | --- | --- |
| `remote` · `remote_port` | `false` · `7717` | 開網頁介面；只綁 loopback |
| `remote_write` | `false` | 配對過的裝置能不能打字，還是只能讀 |
| `remote_tunnel` | `off` | `off` · `quick` · `named` |
| `remote_tunnel_name` · `remote_hostname` | `""` | named tunnel 兩個都必填 |
| `cloudflared_path` | `""` | 空的 ＝ 去套件管理器慣用的位置找 |
| `push_on_delivery` · `push_on_fanout` | `true` · `true` | session 回報交件了、一批派出去的任務全部回來了。`push_on_fanout` 會沿用已移除的 `push_on_finish` |
| `push_on_deploy` | `false` | deploy 結束時通知，成功失敗都算 |
| `smart_notifications` | `false` | 用 Haiku 把籠統的整批完成通知換成一句「剛完成了什麼」；交件通知不花額度，直接照抄 session 自己寫的那句 |
| `orchestrator_enabled` | `true` | 能不能讓一個 session 把工作派給另一個 |
| `orchestrator_max_children` | `5` | 一個 session 同時最多派幾個子 session，1–10 |
| `orchestrator_max_grandchildren` | — | 已經不再讀取。樹只有一層是程式碼的事實，不是這個檔案的；舊設定檔留著這個 key，沒有任何地方會看它 |
| `orchestrator_permission` | `full` | 子 session 走多遠才停下來問：`ask`／`edits`／`full`。這同時是上限，任務要不到比它更多 |
| `orchestrator_notify_root` | `true` | 做完之後往發派的 session 打一行字 |
| `orchestrator_child_linger` | `180` | 回報過的子 session，分頁再留幾秒；`0` 馬上關，`-1` 不關 |

## 權限與隱私

| 要什麼 | 為什麼 | 什麼時候 |
| --- | --- | --- |
| **自動化 → iTerm2** | 把文字放進 session 的唯一方式 | 第一次送出時問一次 |
| **麥克風 ＋ 語音辨識** | 語音輸入 | 只有你按下麥克風時 |
| *（沒有其他）* | 不要輔助使用、不要螢幕錄製 | — |

全域熱鍵刻意走 Carbon 的 `RegisterEventHotKey` 而不是 `NSEvent` monitor，就是為了**避開輔助使用
權限**——一個只是要開輸入框的工具，沒有理由拿到「看見你每一次按鍵」的能力。

**這裡有三件事會連網，而且三件都是你自己扳下去的開關。** 一件是遠端存取：全新安裝關著，打開之後
也只綁 loopback，真正把東西帶出這台機器的是 `cloudflared`——你自己裝的那支。另一件是語音輸入：
macOS 對你下載過的聽寫語言在本機辨識，沒下載的則把聲音送到 Apple，而現在是哪一種，會整段寫在輸入
條下緣、只要它還在聽就一直在。想要它永遠不出這台機器，到系統設定 › 鍵盤 › 聽寫把那個語言下載
下來。第三件是 Codex 自動命名：打開後，第一則需求會再送給設定的 Codex 模型一次，用來產生標題。

遠端存取與自動命名都關著、又沒去按麥克風的話，這裡完全不連網。歷史紀錄存在
`~/.config/clawdline/config.json`，不會去任何地方。

## 需求與限制

- **Apple silicon、macOS 13 以上。** build 只出 arm64，所以下載回來的 release 在 Intel Mac 上
  起不來。在那種機器上自己 build 只要改 `build.sh` 一個字，但沒測過。
- **iTerm2，其餘終端機需要 tmux。**
- **單向。** Claude 的回覆還是在終端機裡——<kbd>⌘</kbd><kbd>J</kbd> 讀得回來，但輸入條管的是你
  送出去的那半。那半本來就是往上捲的；這個工具修的是被釘在左下角的那一半。
- **兩邊的螢幕與紀錄格式都不是承諾過的介面。** 每個欄位進來時都是選填，認不得的一律跳過。
  [跑過哪些版本 →](docs/compatibility.md)
- **背景 agent 的數字只算 Claude Code。** 一個派了 subagent 出去的 Codex session，看起來就像在
  想一句話想很久。
- **共用工作區的仲裁擋在門口，不是擋在硬碟上。** 它認得的是被派出去的任務、以及它們宣告過的東西；
  你自己開的那個分頁，或是一個不照指示走的子 session，照樣寫得到任何地方。
  [為什麼還是值得有 →](#多個-session-共用一份工作區)

## 出事的時候

App 做的每一件事都寫進 `~/Library/Logs/Clawdline.log`。

- **按 <kbd>⌥</kbd><kbd>Space</kbd> 沒反應** —— 看 log 有沒有 `hotkey registered`。沒有就是被別
  的 app 佔走了，在設定裡換一個。
- **顯示「No Claude Code session found」** —— 多半是自動化權限被拒。跑
  `tccutil reset AppleEvents com.tsunamiworks.clawdline` 之後重開，讓它重新問一次。
- **送出失敗** —— 面板會自己跳回來，你打的字還在，下緣寫著原因。它不會吃掉你的字。

## 文件

**[繁體中文公開使用手冊](https://clawdline.com/docs)** 是安裝與使用 Clawdline 的正式操作入口。
下列頁面是開源技術契約與深入實作參考；原始碼、測試、skill 與整合仍會連到它們，因此繼續公開保留。

| | |
| --- | --- |
| [輸入條的細節](docs/interface.md) | session 清單、<kbd>⌘</kbd><kbd>J</kbd> 那塊、語音、檔案、瀏海 |
| [把工作派出去](docs/orchestrator.md) | 一個 session 派工給另一個：協定、憑證、生命週期 |
| [共用工作區的 commit 守衛](docs/shared-tree-guard.md) | 擋掉夾帶別人已 stage 檔案的 `pre-commit` hook、`sh tools/install-git-hooks.sh` 開了什麼，以及 `pre-commit` 看不到哪些事 |
| [排程的任務](docs/schedules.md) | 按本地時間派出的 task 模板、睡眠補課與收 tab 政策 |
| [交給新 session 接著做](docs/handoff.md) | 把整條工作的現況交給下一個 session 繼續 |
| [被派出去的 session 會在哪停](docs/dispatch-permissions.md) | 四道門依序是什麼，以及那個在最便宜的模型上意思相反的 flag |
| [把專案接上來](docs/connect.md) | 寫給 agent 看的：一個專案能寫的七種檔案依序走一遍，以及怎麼驗自己有沒有做對 |
| [開發環境](docs/devstack.md) · [怎麼導入](docs/devstack-adopting.md) | `.devstack.json`，以及導入的三種深度 |
| [專案狀態檔](docs/project-status.md) | 另外六種：圖示與顏色、部署、任何在本機長跑的工作、backlog、milestone、健康檢查 |
| [從別的地方連進來](docs/remote.md) · [API](docs/api.md) | 完整的威脅模型，以及那層 HTTP 介面 |
| [Clawdline Cloud](docs/cloud.md) | Mac 這端的橋接、雲端主控台、金鑰交接，以及哪些事還沒對真的帳號跑過 |
| [Hook](docs/hooks.md) | 那八個事件，以及為什麼說了算的仍然是螢幕 |
| [通知](docs/notifications.md) | 誰會聽到什麼，以及為什麼決定聽眾的是深度而不是音量 |
| [等待](docs/waiting.md) | 工作跑在哪條執行緒上，以及等一個子行程曾經怎麼弄壞這個 app |
| [被移到背景的對話](docs/background-conversations.md) | 那個不再寫自己檔案的分頁，以及改成讀什麼 |
| [Whisper](docs/whisper.md) | 一句話裡不只一種語言的時候 |
| [吉祥物格式](docs/mascots.md) · [圖庫](docs/gallery.md) | 格式，以及大家把 pack 貼在哪 |
| [版本](docs/compatibility.md) | 這東西跑過哪些 Claude Code 與 Codex 版本 |

## 參與開發

純 AppKit、沒有相依套件、除了 `swiftc` 沒有 build 系統。

```sh
./test.sh     # 9434 個檢查，是分鐘等級不是秒
./build.sh    # 編譯，原本有在跑的話會自己接回來
swift build   # 只是為了讓編輯器讀得懂程式碼
```

那個檢查數就是 `test.sh` 裡的封印，而整套跑多久是量出來的——在一台有寫出型號的機器上，
拆成四個階段——記在 [docs/suite-runtime.md](docs/suite-runtime.md)。兩個只要跟來源對不上，
`Tests/docs-suite-facts.mjs` 就會紅；這個區塊在 2026-09-04 以前寫的是「約兩秒」，
而它其實是分鐘等級。

[CONTRIBUTING.md](CONTRIBUTING.md) 是其餘的部分：東西放在哪、怎麼加一個語言或一隻吉祥物、
以及「第三種把字送出去的方式」會長什麼樣。十四種翻譯的修正都歡迎——其中沒有人以母語在用的那幾種
最需要。

## 出處說明

吉祥物是 Claude Code 裡那隻像素角色的同人畫，社群叫牠 **Clawd**。本專案與 Anthropic 無任何關聯，
未經其背書或授權。Claude 與 Claude Code 是 Anthropic 的商標。

把 AI agent 的即時動態放進 MacBook 的鏡頭那塊，是 [bistin](https://github.com/bistin) 的
[CLI Island](https://github.com/bistin/cc-island) 先做的；這裡的實作是自己寫的、做法也不一樣，
但那個點子是借來的，在此致謝。瀏海本身的形狀出自
[DynamicNotchKit](https://github.com/MrKai77/DynamicNotchKit)，經由
[boring.notch](https://github.com/TheBoredTeam/boring.notch)。

Glossary 與文件指標架構、明確的決策圖，以及由 receipt 推導 frontier 的做法，參考並感謝
[mattpocock/skills](https://github.com/mattpocock/skills)，特別是其中 writing-for-agents、
domain-modeling、wayfinder、to-tickets 與 code-review 的指引。Clawdline 的 protocol 與實作皆為
本專案自行完成；本專案與該 repository 無隸屬或背書關係。

[![在 Ko-fi 上支持 Clawdline](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/sainteye)

## 授權

[MIT](LICENSE)
