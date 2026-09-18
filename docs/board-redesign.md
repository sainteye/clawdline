# 看板重新設計：看板、Session 待辦、Backlog 三種結構與它們的生命週期

> **需求來源（使用者 2026-09-18 原話，逐字）**
>
> 「看板最大的問題有幾個：
> 1. 使用者不知道是否建立 (我在想流程是否要去判斷這個是否重要到使用者應該追蹤，如果是的話應該詢問)
> 2. 太瑣碎的項目應該是給 session 看的
> 3. 看板項目的生命週期沒有定義清楚，造成會有
>   a. 瑣碎無人管理、無人在乎的項目
>   b. 項目開始之後沒人收尾、看不進度
>   c. 項目已經作完了，但是看板系統完全沒有更新
> 4. 我認為應該是在我們的流程中，要讓人類適當的參與、理解、給出指令，這樣才是完整的看板系統
> 5. 如果判斷不用人介入的項目，則應該要完全自動化建立、自動化追蹤、自動結束(類似 TODO)，並且不應該和給人看的項目混在一起」
>
> 同日補充（逐字）：
> 「看板存在的兩個目的：
> 1. 讓人類理解狀況
> 2. 讓 Session 不會忘記什麼要作」
> 「我認為規劃要作，但短期完全還沒有計畫開始要做的東西，應該乾脆純放在另外一種資料結構，類似 Backlog 或是其他名稱」
>
> 下文用 **#1、#2、#3a、#3b、#3c、#4、#5** 指上面五點，**目的一／目的二**指兩個目的，**#BK** 指 Backlog 那一段。
> 每個設計決定都標出它解決的是哪一條。
>
> **本輪只出文件，不改實作。** 看板頁前端的照搬由另一個 child（`%905`）在做，本文不動它的檔案。

量測對象：舊 app 的看板在 **2026-09-18 01:02 UTC（台灣 09:02）** 的唯讀快照（`revision 6403`，**787 張卡**——任務書寫的 776 是較早的數字，這十一天平均每天多約 40 張），
加上同一時刻的 `project-board-history/*.jsonl`（564 檔、12,900 筆）、`project-board-workflow.json`（workflow 日誌）、
`~/.config/clawdline/orchestrator.json` 裡 774 個 task 的狀態與落地欄位（只抽 id／狀態／落地，不讀 `secret_hash`），
以及 `:7727/v1/sessions` 的 14 個 live session。每張卡的 progress 是用本 repo 已移植的 `ProgressOf`／`ListSummaryOf`
（`internal/adapters/board/progress.go`）對快照逐張算的。腳本與輸出見附錄。

---

## 0. 五句話

1. **他說的五點全部成立，而且比他說的更嚴重。** 787 張卡**沒有一張能證明是人寫的**（網頁 UI 根本沒有任何改卡片的按鈕）；
   有歷史紀錄的 564 張裡 **52.8% 建立之後再也沒有任何非機器的更新**；舊版的生命週期 `… → integrated → closed`
   在十一天、787 張卡上**進到 `closed` 的是 0 張**。
2. **壞的不是「事實進不來」，而是「生命週期收不了尾」。** 610 張連著 broker task 的卡，卡上的 task 狀態與 broker 的真實狀態
   **0 筆不一致**；但 345 張（43.8%）在證據上已經結束，存下來的生命週期還停在 `execution`／`backlog`。
   收尾的閘門要一種幾乎沒人寫的證據（驗證紀錄），所以永遠過不了。
3. **一個結構硬扛兩個目的，正是他點名的病。** 同一張卡同時是 session 的備忘（checklist、obligation、宣告的 span 都是 session 寫的）
   和人要看的進度；結果人看到的是 session 的筆記，session 的筆記又被人的生命週期規則綁住。
   而且同一件工作常常**兩張卡互不相連**：派工只有 **12.1%** 綁到看板項目（最近兩天 56 次派工 **0 次**），
   192 張人類卡只有 31 張（16%）連得到任何派工——**人類卡看不到進度，是因為進度根本沒接到它身上。**
4. **設計：三種結構，各自一個讀者、一台狀態機、一張表。** 看板＝人現在需要知道的事；Session 待辦＝session 不能忘的事，
   全自動建立／追蹤／結束；Backlog＝規劃要做但沒有「開始的承諾」的事。三者用同一個工作身分（`work_id`）相連，
   移動是一筆有觸發條件、有執行者、寫進 `moves` 紀錄的交易。看板與 Backlog 的界線是**有沒有承諾**（有派工、有人接、
   排了七天內的開始日、在等人決定、或已交付待收尾），是可以從事實算出來的，不是形容詞。
5. **把現有 787 張依這套規則分：Session 待辦 602（76.5%）、看板 108（13.7%）、Backlog 77（9.8%）。**
   只看給人看的 185 張：**Backlog 77（41.6%）、等人收尾 66（35.7%）、真的正在發生 31（16.8%）、已結束 11（5.9%）**。
   也就是說，**人類看板上每五張卡只有不到一張是「現在」**。最小可驗收的第一步是把這個分法做成唯讀投影，
   讓使用者先對著自己的資料確認規則，再動任何寫入（§9）。

---

## 1. 他說的對不對：用數字逐條回答

### 1.1 量法與定義

| 名詞 | 定義 | 為什麼這樣定 |
|---|---|---|
| **人寫的** | 歷史紀錄的 `actor` 是人操作的裝置，且那個寫入在網頁 UI 上有對應的操作 | 舊版的 actor 規則（`ProjectBoardHTTP.swift:260-268`）：本機憑證一律記成 `machine`，配對裝置記成裝置 id |
| **非機器的更新** | 排除 `item_created`、`automatic_state_reconciled`、`catalog_reconciled`、`task_reattributed`、`session_relation_confirmed` 這五種機器對帳 | 這五種是重算，不是新事實（`board-design.md` §2.3 已量出它們佔 52.5%） |
| **建立後的更新** | 在 `item_created` **60 秒之後**的非機器事件 | 建立當下同一批寫入（連結 session、宣告 span、綁 task）是建立的一部分，不是進度 |
| **閒置** | 快照時間減去最後一筆非機器事件 | `updatedAt` 會被機器對帳一直推後（見 §1.4），不能當成「有人在動」 |
| **開始過** | 有 broker 派工、或有 session 交付紀錄、或有證據、或有勾掉的 checklist | **宣告的 span 不算**：workflow 的 `begin_template` 把 `phase` 預設成 `"output"`，登記一個項目就會同時宣告「正在產出」（見 §1.7） |

### 1.2 #1「使用者不知道是否建立」——**成立，而且是結構性的**

- **人沒有建立的管道。** 舊版網頁對看板的寫入只有 `set_enabled` 與 `set_ai_consent`（`input/board-settings.js:85,102`），
  產品文件也寫明「There is no browser New button, edit form or manual status selector」（舊 repo `docs/project-board.md:30-31`）。
- **12,900 筆歷史紀錄是誰寫的：**

  | actor | 筆數 | 佔比 | 是什麼 |
  |---|---:|---:|---|
  | `board` | 4,771 | 37.0% | store 自己（建立、自動對帳） |
  | `broker` | 2,944 | 22.8% | 派工事實 |
  | `workflow:<provider>:<session>` | 2,870 | 22.2% | session 在對話裡寫的（codex 2,136、claude 734） |
  | `machine` | 1,851 | 14.3% | 本機憑證，也就是 root／agent 用 curl 寫的 |
  | `root_attestation`／`root_report` | 268 | 2.1% | root 的驗證聲明與完成報告 |
  | 配對裝置 | 196 | 1.5% | **只有一個裝置、只碰過 7 張卡**，內容是 agent 口吻（例：「使用者已於 Session 明確批准指定 payload＋destination；S3 put-object 成功…」），而網頁沒有對應的按鈕——判為 agent 透過裝置 token 寫入 |

  **可以證明是人寫的：0／787。**
- **誰建立的：** broker 從派工自動推出的執行紀錄 **514 張（65.3%）**；agent 明確建立 273 張，其中可追到 session 對話的 99、
  root 用機器憑證或同秒由 broker 綁定的 59、只有建立事件的 25、建立時還沒有歷史檔的 89、不明 1。**人建立：0。**
- **agent 在人不知道的情況下開卡的機制：** 舊版每一則經 Clawdline 送出的訊息都夾帶一個 workflow 信封，要 session 先把這則訊息分類成
  `existing_item｜new_work｜question｜clarification`。保留中的 256 次 run 裡，236 次有 `begin`，其中 **47 次（19.9%）被 agent 自己判成
  `new_work`，當場開一張新卡，沒有問人**。
- **有沒有問過人：** 360 筆 obligation 裡記成 `actorKind: "user"`（要人決定）的只有 **7 筆（1.9%）、4 張卡**，5 筆到現在還開著；
  277 筆根本沒記 actorKind。workflow 的交付裡 `waiting_user` 15 次／256。

> **判定：成立。** 嚴重度：高。人不只「不知道有沒有建立」，是**根本沒有參與建立**；唯一的詢問管道（`actorKind:user`）十天用了 7 次。

### 1.3 #2「太瑣碎的項目應該給 session 看」——**成立**

| 量 | 數字 |
|---|---|
| broker 自動推出的單一 task 執行紀錄，且沒有 checklist | **496／787（63.0%）** |
| 建立後（60 秒以後）**沒有任何**非機器更新 | **298／564（52.8%）**；再加只有一筆的 124 張，**≤1 筆的共 422 張（74.8%）** |
| 從建立到最後一筆非機器事件的時間 | **<10 分鐘 312 張（55.3%）**、10 分–1 小時 135（23.9%）、1–6 小時 45、6–24 小時 45、1–3 天 25、3–7 天 2、≥7 天 **0** |
| 同一份分布的百分位 | p50 **0**（全部在建立那一刻寫完）、p75 0.76 小時、p90 14.9 小時、p95 23.6 小時 |
| 只有 ≤1 筆更新而且一小時內就結束 | 107 張，**全部是 agent 類** |
| 每天新增 | 09-08 初次匯入 379 張；之後十天 408 張，**平均每天約 41 張** |

舊版在 09-15 已經做過一次逐筆稽核（由一個 codex session 執行，當時 465 張，現在帶 `catalogDisposition` 的有 474 張），
把卡分成 `human`／`agent`／`archive`，並把 agent 列收進預設收合的「Agent execution details」。但那只是**呈現上的過濾**：它們仍在同一份文件、同一套生命週期、同一個 2,000 張的容量裡，
計數與「無人收尾」照樣發生在它們身上（見 §1.4 的 260 張）。

> **判定：成立。** 嚴重度：高。看板上三分之二是 broker 自己的帳，半數寫完那一刻就再也沒動過。

### 1.4 #3「生命週期沒有定義清楚」——**三種壞法都量得到**

**#3a 瑣碎無人管理、無人在乎：**

- 298 張建立後零更新（§1.3），其中 agent 240、archive 42、human 16。
- 依 §7 的規則，**260 張不給人看的卡（agent／archive 253、協調紀錄 7）沒有結束也不會再有人動**：delivered 186、blocked 54、
  unknown 17、execution 3。它們在舊版會永遠停在「未結束」。
- **機器在假裝它們有在動**：`updatedAt − createdAt` 的中位數是 **108 小時**，但真正的最後一筆事件中位數在建立的那一秒。
  **362 張卡的 `updatedAt` 比最後一筆真正的事件晚一天以上**——是機器對帳在碰它們。
  4,124 筆 `automatic_state_reconciled` 裡 **3,115 筆（75.5%）是 `planning ⇄ execution` 來回擺盪**，集中在 209 張卡
  （208 張是自動建立的 task 紀錄）、143 個不同的秒數裡（最多一秒 88 張），是批次對帳的雜訊。

**#3b 開始之後沒人收尾、看不到進度：**

- **session 開了頭不收尾**：workflow 日誌共 528 次 run（保留 256＋已淘汰 272）。**158 次（29.9%）連 `begin` 都沒有，
  230 次（43.6%）有 `begin` 沒有 `deliver`。**
- **看不到進度**：派工只有 **94／774（12.1%）** 帶 `work_item_id` 綁到看板項目；09-15 之後 6／99（6.1%）；**09-17、09-18 兩天 0／56**。
  192 張人類卡裡只有 **31 張（16.1%）** 連著任何 broker task。其餘的進度走在另一張 agent 卡上，兩張互不相連（§1.7）。
- **未結束的 181 張人類卡裡，98 張（54.1%）超過 3 天沒有任何非機器更新**，26 張超過 7 天。
- **「進行中」是假的**：投影成 active 的 15 張卡裡，**7 張（46.7%）的唯一依據是一段宣告中的 span，而宣告它的 session 已經不在了**
  （其中 5 張是人類卡）。14 段未結束的 span 有 11 段屬於已經不存在的 session。

**#3c 做完了但看板沒更新：**

| 量 | 數字 | 說明 |
|---|---:|---|
| 存下來的生命週期進到 `closed` | **0／787** | `integrated` 1 張、`verified` 8 張 |
| 投影已結束（landed／settled／verified／canceled）但存的狀態還是 `execution`／`backlog` | **345（43.8%）** | 人看到的標記與系統存的生命週期說兩件事 |
| broker 說每個 task 都已落地（`landed`／`nothing_to_land`），卡卻顯示未完成 | **16** | blocked 12、delivered 2、correction 1、execution 1 |
| 投影 `delivered` 的卡 | 262 | 其中 **207（79.0%）閒置 ≥3 天**；task 的落地欄位：**169 張完全沒有落地紀錄**、64 張只有 session 自稱交付、25 張 abandoned |
| 卡上的 task 狀態 vs broker 的真實狀態 | **0 筆不一致**（610 張有 task 連結，610 張的 task 都查得到） | **事實有進來；壞的是收尾** |

為什麼永遠收不了尾：舊版的自動對帳（`ProjectBoardStore.swift:3440-3505`）要 `closed` 必須同時有 owner、沒有未解的阻擋 finding、
**當前 checklist 每一列都有證據、有驗證證據**、有交付（落地）證據、並通過 `closureIsSafe`。驗證證據只能由 root 用 `record_evidence`
寫入，十一天只寫了 194 筆。**閘門是對的形狀，但它要的證據在實際流程裡幾乎不會產生，所以等於沒有出口。**

> **判定：三種壞法全部成立。** 嚴重度：高。尤其 #3c：人看的是 progress 投影，系統存的是另一個永遠不前進的狀態，
> 兩者之間沒有任何東西會讓它們收斂。

### 1.5 #4、#5「人要適當參與；不用人的要全自動、不混在一起」——**現況兩頭都沒做到**

- **人能下的指令：** 看板上 0 個（只有開關）。人的意志只能透過「跟 session 講話，再由 agent 代寫」進來，而那條路 43.6% 沒有收尾。
- **自動的部分沒有自動結束：** 260 張 agent 卡停在未結束（§1.4），因為結束的條件跟人類卡是同一套。
- **混在一起：** 同一份文件、同一個容量、同一套生命週期；分開的只有 UI 的預設收合。

### 1.6 總表

| 他說的 | 判定 | 最能說明的數字 |
|---|---|---|
| #1 不知道是否建立 | **成立** | 人建立 0／787；agent 自己判 `new_work` 開卡 19.9% 的 begin；問人的 obligation 十天 7 筆 |
| #2 瑣碎應給 session | **成立** | 自動執行紀錄 63.0%；建立後零更新 52.8%；55.3% 十分鐘內寫完 |
| #3a 無人管理 | **成立** | 260 張不給人看的卡永遠未結束；362 張被機器「更新」但沒有真事件 |
| #3b 沒人收尾、看不到進度 | **成立** | 43.6% run 沒 deliver；派工綁定 12.1%（近兩天 0%）；active 的 46.7% 是幽靈 |
| #3c 做完沒更新 | **成立** | `closed` 0 張；345 張投影已結束但生命週期沒動；卡上 task 狀態 0 筆不一致 |
| #4 人適當參與 | **成立（缺）** | 看板上人能下的指令 0 個 |
| #5 自動軌道獨立 | **成立（缺）** | agent 與人類卡同表、同生命週期、同容量 |

### 1.7 量測時順帶發現、舊紀錄沒提過的

1. **同一件工作兩張卡。** 例：`CLA-495`／`496`／`497`（「broker／看板／時間軸的設計分析」）在 `clawdline` 專案、人類卡、**顯示從未開始**；
   實際派出去的執行紀錄（如 `CLA-48`「broker 設計分析」）在 `clawdline-go` 專案、agent 卡、**已落地**。
   本任務自己也是：`CLA-498`（人類卡，`clawdline`）與 `CLA-53`（執行紀錄，`clawdline-go`）。同專案、標題相近、六小時內的配對至少 9 對
   （跨專案的沒算，是下限）。
2. **專案歸屬跟著 session 的 cwd，不跟著工作。** root 在 `~/code/clawdline` 裡開、做的是 `clawdline-go` 的事，它開的卡就記在 `clawdline`。
3. **`begin_template` 的 `phase` 預設 `"output"`**，所以「登記一個還沒動工的 epic」會同時宣告「正在產出」（`CLA-493`／`494` 都是這樣），
   span 因此不能當作「開始了」的證據。
4. **每則訊息的 workflow 信封會污染畫面解析。** 舊 app log 裡有 62 行 `choosing (unnumbered): options=17 — 請用中文 ⏐ <clawdline-workflow …`，
   是畫面上的選單解析把信封內容當成選項。
5. **assistant 內建的待辦工具在這台機器上幾乎沒人用**：近 10 天 400 份 Claude transcript 用 `TodoWrite` 的 0 份；
   300 份 Codex session 用 `update_plan` 的 16 份（5%）。所以 Session 待辦**不能**靠投影它們（§3.1 的決定來自這個數字）。
6. **item key 不唯一**：`CLA-53` 同時是 `clawdline`、`clawdline-cloud`、`clawdline-go` 三個專案裡的三張卡；
   全部有 80 個 key 重複、涉及 224 張卡。新設計的身分是 `work.id`，key 只是顯示用。

### 1.8 量不到的（不算通過）

- **卡片有沒有被人打開過：量不到。** 舊版不記單張卡的讀取；`remote-audit.jsonl` 不記看板讀取，log 裡 Cloud 的
  `kind=board` 讀取只記「讀了看板」不記哪一張。
- **workflow 那 112 張有 session 對話痕跡的卡，是不是真的有人在場：** `/send` 也可能是 root 轉送，所以 112 只是「人可能在場」的上限。
- **停擺門檻的依據只有十天**：「沉寂超過 3 天之後再也沒有恢復」是在這十天內觀察到的（§5.5），更長的沉寂沒有機會被觀察。
- **progress 是用 Go 移植版算的**，不是 Swift 原版；兩者是否逐卡一致，本文沒有重新比對（`board-design.md` 的 B1 說逐條移植）。
- `orchestrator.json` 與 session 清單是在快照後幾分鐘內讀的；`:7727/v1/sessions` 看到的 session 可能比舊 app 少，因此「session 已不在」可能略為高估。

---

## 2. 設計原則（每一條都指回問題）

| # | 原則 | 解的是 |
|---|---|---|
| D1 | **一個結構一個讀者。** 看板的讀者是人、Session 待辦的讀者是 session、Backlog 的讀者是做規劃的人。沒有任何一筆資料同時寫給兩種讀者 | 目的一 vs 目的二、#2、#5 |
| D2 | **給人看的東西，出現之前人一定知道。** 進看板只有三條路：人自己說、人確認了提議、或符合「承諾」規則的自動移動——而自動移動一定出現在每日摘要裡 | #1、#4 |
| D3 | **每一筆未結束的東西都有 owner 與時鐘。** 時鐘到了走一條寫好的轉換，從來不是「安靜地停著」 | #3a、#3b |
| D4 | **收尾用實際會產生的證據。** code 的收尾證據就是 broker 的落地紀錄（已證明可靠：0 筆不一致）；「驗證過」是徽章，不是閘門 | #3c |
| D5 | **人只在少數幾個點被拉進來，而且有預算。** 會擋住工作的決定才推播；其餘進每日摘要或待確認區 | #4 |
| D6 | **事實落盤，觀察不落盤**（沿用 `broker-design.md` §6.2）。session 宣告的 phase 是觀察，不能讓卡變成「進行中」 | #3b（幽靈進行中）、#3a（機器擺盪） |
| D7 | **規則決定能不能問，agent 只提供事實，人決定要不要追蹤。** agent 的自評不足以把東西放上看板 | #1 |
| D8 | **派工必須帶工作身分。** 沒帶就由 broker 依規則綁定或開一筆 Session 待辦，絕不在另一個專案另開一張人類卡 | #3b（看不到進度）、§1.7 的雙胞胎 |

---

## 3. 三種結構

### 3.1 各自是什麼

| | **看板**（Board） | **Session 待辦**（Todo） | **Backlog** |
|---|---|---|---|
| 目的 | **目的一**：人理解現在的狀況 | **目的二**：session 不會忘記要做什麼 | 規劃要做、但沒有開始的承諾 |
| 讀者 | 人 | session（它自己、它的 root、接手它的 session） | 做規劃的人 |
| 回答的問題 | 「現在有哪些事在發生、哪些在等我？」 | 「我（這個 session）還欠什麼？」 | 「之後要做什麼、先後順序？」 |
| 誰建立 | 人；或人確認的提議；或 Backlog／待辦依規則移入（一定進摘要） | **自動**：broker 派工、child 的 `result.json`、session 交付的 `remaining`、obligation | 人；或提議選「之後」；或看板停擺移回 |
| 誰結束 | 證據（落地）自動結束；或人「收下／放棄」 | **自動**：對應的 task 結束並落地、obligation 解決、交付涵蓋它；session 消失時移交給 root | 只有人（丟掉），或被移進看板 |
| 必填 | owner、目標、時鐘（下一筆證據的期限） | owner session、來源（哪個 task／交付） | 無（排序、預計開始日選填） |
| 數量級（依 §7 換算現況） | 進行中 31、等收尾 66 | 602（其中 live 5） | 77 |

**Session 待辦從哪裡自動長出來：** 不是 assistant 的 `TodoWrite`／`update_plan`（§1.7-5：幾乎沒人用），而是**broker 已經可靠產生的事實**：
一個派工＝root 的一筆「收結果、落地」待辦；child 的 `result.json` 與 session `deliver` 裡的 `remaining`＝各自 owner 的待辦；
obligation 依 `actorKind` 分流（`user` 的變成看板上的「等你決定」，其他的變成待辦）。
這條路不需要 session 多呼叫任何 API——量到 43.6% 的 run 連 `deliver` 都會漏，**任何「請 agent 記得呼叫」的設計都會漏同樣的比例**。

### 3.2 資料上怎麼分：三張表＋一個工作身分＋一條移動紀錄

比較過三種做法：

| 做法 | 好處 | 壞處 | 判斷 |
|---|---|---|---|
| **同一張表、不同 `kind`**（＝舊版的 `audience`） | 移動只改一個欄位 | 每個查詢都要記得過濾；容量、計數、生命週期共用；**舊版就是這樣，§1.3–1.4 的數字就是結果** | 不要 |
| **同一張表、不同狀態機** | 狀態機分開了 | 不變量（看板必須有 owner 與時鐘、Backlog 不需要）只能靠程式慣例，schema 擋不住；還是會一起算 | 不要 |
| **不同表** | 不變量由 schema 保證（`NOT NULL`、`CHECK`）；查詢**不可能**混到另一種；容量與保留策略各自定 | 移動要一筆交易；需要一個跨表的身分 | **採用** |

**決定（D1、#5、#BK）：**

```sql
-- 工作的身分：不管現在在哪個結構，id 不變，連結（task、文件、報告）掛在這裡
CREATE TABLE work (
  id          TEXT PRIMARY KEY,           -- uuid
  project_id  TEXT NOT NULL,              -- 工作的專案，不是 session 的 cwd（§1.7-2）
  title       TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  created_by  TEXT NOT NULL               -- user | user_via_session:<run> | root:<session> | broker
);

CREATE TABLE board_items (                -- 看板：人現在需要知道的
  work_id          TEXT PRIMARY KEY REFERENCES work(id),
  state            TEXT NOT NULL CHECK (state IN ('active','awaiting_closure','done','dropped')),
  owner            TEXT NOT NULL,         -- root session 或 "user"
  commitment       TEXT NOT NULL,         -- dispatch | assigned | scheduled | decision | delivered
  evidence_due_at  INTEGER,               -- 時鐘：active／awaiting_closure 必填
  acceptance       TEXT,                  -- 人看的驗收條件（少數幾條），不是 session 的步驟清單
  closed_reason    TEXT                   -- landed | accepted | unconfirmed | dropped
);

CREATE TABLE backlog (                    -- Backlog：規劃要做、沒有開始的承諾
  work_id      TEXT PRIMARY KEY REFERENCES work(id),
  state        TEXT NOT NULL CHECK (state IN ('planned','dropped')),
  rank         INTEGER,
  start_on     TEXT,                      -- 預計開始日；進入七天內即觸發移入看板
  reviewed_at  INTEGER                    -- 人最後一次看過它
);

CREATE TABLE todos (                      -- Session 待辦：session 不能忘的
  id            TEXT PRIMARY KEY,
  work_id       TEXT REFERENCES work(id), -- 可空：純 session 的雜事沒有人類工作
  owner_session TEXT NOT NULL,
  origin        TEXT NOT NULL,            -- dispatch | result_remaining | deliver_remaining | obligation
  task_id       TEXT,
  state         TEXT NOT NULL CHECK (state IN ('open','done','dropped','handed_off')),
  escalated_at  INTEGER
);

CREATE TABLE moves (                      -- 三個結構之間每一次移動，append-only
  seq      INTEGER PRIMARY KEY,
  work_id  TEXT NOT NULL,
  from_s   TEXT NOT NULL,                 -- todo | proposal | board | backlog | none
  to_s     TEXT NOT NULL,
  trigger  TEXT NOT NULL,                 -- §6 的觸發碼
  actor    TEXT NOT NULL,
  evidence TEXT NOT NULL,                 -- JSON：哪一筆事實觸發了它
  at       INTEGER NOT NULL
);
```

另外兩張小表：`proposals`（§4 的提議與它的答案）、`decisions`（等人決定的事）。

**放在哪：** 與 broker 同一個 SQLite（`broker-design.md` §6.1、§6.9 的方向）。理由是 #3c：舊版的看板是另一份文件，靠 broker 推送
`observe(...)` 更新，收尾條件又是另一套——兩個 store 之間只要有一處不收斂就永遠不收斂。同一個 DB 裡，
「task 落地」→「待辦結束」→「看板項目結束」可以是**同一筆交易**（`broker-design.md` §6.1 的規則：一個外部看得到的事實就是一筆交易）。
這一點與 `board-design.md` C1「照搬整份文件重寫」不同：C1 是對**照搬舊模型**的判斷，本文換了模型，換模型之後舊的理由（42 ms、不值得拆）
不再是決定因素。舊版 787 張卡的唯讀讀取（`internal/adapters/board/source.go` 的 `Legacy`）**不變**。

**衍生的顯示狀態不存。** 卡片上「排隊中／進行中／審查中／已交付／已落地」照舊由 `ProgressOf` 從證據推出（`board-design.md` B1、B5 照搬），
但**不寫回 store**，所以不會再有 4,124 筆對帳紀錄與 75.5% 的擺盪（D6、#3a）。存下來的只有四個生命週期狀態與人的決定。

### 3.3 畫面上怎麼分

- **看板頁**只放 `board_items`，由上到下：
  1. **等你決定**：`decisions` 與 `awaiting_closure`（收尾佇列）。每張只有一到三個按鈕。
  2. **進行中**：`active`。卡上顯示目標、owner、衍生狀態、**Session 待辦的計數**（「3 個待辦、1 個卡住」，不列內容）、
     最後一筆證據的時間、時鐘（「3 天內沒有新證據會回 Backlog」）。
  3. **本週排入**：Backlog 中 `start_on` 已進入七天內但還沒派工的（它們已經算承諾，見 §6）。
  4. **最近完成**：七天內 `done`，預設收合。
- 看板標題旁一個**待確認**徽章（`proposals` 的數量），點開是提議清單。
- **Backlog** 是看板頁的另一個分頁：排序清單、依專案篩選、每列有「排入」與「開始做」。
- **Session 待辦不出現在看板頁。** 它在 session 詳情的一個面板裡，並提供給 session 本身讀（API 與簡報，見 §5.2）。
  這是 #5「不應該和給人看的項目混在一起」的字面實現：**不是收合，是不在同一頁**。

> 目前的看板頁前端是 `%905` 在做的舊版照搬，給 787 張舊卡的唯讀檢視用。本設計的畫面是新 app 自己的看板，排在照搬落地之後（§9）。

### 3.4 兩個目的不打架：誰寫什麼

舊版一張卡上同時有：目標（人的）、checklist（session 的計畫，但被當成人的驗收條件）、obligation（有的要人決定、多數是 session 自己的）、
span（session 的宣告，卻讓卡變成「進行中」）、交付（session 的自述，卻是人看的完成標記）。**每一種欄位都是兩個讀者共用，兩邊都被對方綁住。**

| 資訊 | 舊版放在 | 新設計放在 | 誰寫 | 誰讀 |
|---|---|---|---|---|
| 目標、owner、驗收條件（少數） | 卡 | `board_items` | 人（或人確認的提議） | 人；session 唯讀地拿來知道「為什麼」 |
| 步驟清單、剩餘工作 | 卡的 checklist／obligation | `todos` | 自動（broker 事實） | session |
| 要人決定的事 | 卡的 obligation（`actorKind:user`，7 筆） | `decisions` | session 提出、人回答 | 人 |
| 「我正在做 X」 | 卡的 span | **不落盤**（記憶體與 SSE 的即時欄位） | session | 人（即時畫面） |
| 交付、落地 | 卡的 evidence／sessionDeliveries | broker 的 task 與落地紀錄 | broker | 兩邊都讀，推導出各自的狀態 |

**規則：session 永遠不寫看板項目的狀態；人永遠不需要編輯待辦。** 看板項目的狀態由它連著的待辦與 task 的事實推導，加上人的決定；
待辦的狀態由 broker 事實推導。兩者只透過 `work_id` 相連，**同一筆資料不會同時為兩個讀者存在**（D1）。

### 3.5 名稱：就叫 **Backlog**

API 用 `backlog`，中文介面也寫 **Backlog**，副標「已規劃，尚未排入」。理由：

1. **這是使用者自己用的字**，而且業界意思剛好吻合：有排序、規劃要做、不在進行中。
2. **自己造的中文詞都會撞到這個 app 已經在用的詞**：「待辦」是 Session 那一軌；「規劃」是 progress 的 `planning` 階段；
   「待排」和「待辦」只差一個字，在手機上一眼分不出來。
3. **舊版的 `backlog` 是看板項目的一個狀態**（151 張），新設計把它**拿掉**，讓這個字只有一個意思：一個結構，不是一個狀態。

另外兩個名稱：**看板**（`board`）、**Session 待辦**（`todo`）。

---

## 4. 建立：什麼值得讓人追蹤、誰判斷、什麼時候問、怎麼防呆（#1、#4、D2、D7）

### 4.1 預設：什麼都進 Session 待辦，不問

session 做的每一件事都先是待辦。**不問、不上看板。** 這一條本身就擋掉 §1.2 那 47 次「agent 自己判 `new_work` 開卡」。

### 4.2 什麼情況「值得問」：用規則判，agent 只提供事實

| 訊號 | 從哪裡來（事實，不是自評） | 為什麼是這條線 |
|---|---|---|
| **I1 跨 session**：這條工作派出了 child，或做了 handoff | broker | 一件事需要第二個 session，就已經不是一個回合的雜事 |
| **I2 活得久**：待辦開著超過 **24 小時** | 時間戳記 | 量到的 p95 是 23.6 小時（§1.3）：只有最長的 5% 會超過 |
| **I3 有外部效果**：部署、發布、推到預設分支、花錢、對外寄信 | session 用**封閉詞彙**宣告，或 broker 觀察到落地到預設分支 | 這些是人事後會問「那個上線了嗎」的東西 |
| I4 要人決定 | obligation `actorKind:user` | **不走提議**，直接變成「等你決定」（§5.1） |
| I5 人自己說要追蹤（「追蹤這個」「放上看板」） | 人送出的訊息（workflow run 證明是人送的） | **不問**，直接建立，actor 記 `user_via_session:<run>` |

**規則：** 符合 I1、I2、I3 任一條，而且還沒綁到看板項目 → 產生一個**提議**。I4、I5 不產生提議（一個直接變決定、一個直接建立）。

**永遠不提議的：** child 自己的 task（它的 root 擁有那條工作）；review／test／correction 這類附屬嘗試（它們是某件事的步驟）；
`question`／`clarification`；排程的例行工作（61 個有 `schedule_id` 的 task）。

**誰判斷：規則決定「能不能問」，agent 只提供 I1–I3 的事實，人決定「要不要追蹤」。** agent 自評不足以上看板——
§1.2 的 47 張就是 agent 自評的結果。

### 4.3 什麼時候問、用哪個管道

| 情況 | 管道 | 樣子 |
|---|---|---|
| 人在場（這個 root session 過去 30 分鐘內收過人送出的訊息） | **session 裡**，附在 agent 這一回合的交付訊息尾端，**不阻塞** | 「要不要把『X』放上看板追蹤？ 追蹤／之後（Backlog）／不用」——人回覆或在看板上按 |
| 人不在場 | **看板的待確認區**，不推播 | 待確認徽章＋每日摘要裡一行「有 N 個提議」 |
| 要人決定而且擋住工作（I4） | **推播**（現有 notify，每 task 5 則、每小時 30 則的上限內） | 只有這一種會推播 |

量級估計：派工以「root × 專案 × 天」分組，09-08 以來每天中位數 5 條、最多 17 條工作線。已經綁定的工作線不會再被問，所以
實際詢問數會低於這個上限（**未量**，是上限估計）。「要人決定」十天 7 筆，約每天 0.7 次，推播預算綽綽有餘。

### 4.4 防呆：不該問的時候絕對不要問

防呆放在**唯一的入口**：提議只能透過 `POST /v1/orchestrator/proposals` 建立，**是否要在 session 裡問，是伺服器的回答，不是 agent 的決定**。
agent 的簡報只寫一句：「只有當回應是 `ask: true` 時才在對話裡問，用回應裡的句子。」

| 拒絕碼 | 條件 |
|---|---|
| `proposal_below_threshold` | 不符合 I1–I3 |
| `proposal_from_child` | 呼叫者是 child；child 的提議改記在 root 的待確認區 |
| `proposal_duplicate` | 同一個 `work_id` 已經被提議過；被拒絕過的只在**有新的訊號**時可以再提一次，而且只能一次 |
| `proposal_already_tracked` | 已經綁在看板項目上：直接綁，不問 |
| `proposal_budget_exhausted` | 這個 session 這一回合已經問過一次，或今天已經問滿 3 次 → 改進待確認區 |
| （不是拒絕）`ask: false, reason: human_absent` | 人不在場 → 進待確認區 |

**沒有人回答的提議，7 天後過期，結果是「留在 Session 待辦」**（不追蹤）。這是安全的預設：沒回答就不把東西塞進人的看板；
它仍然在待辦裡被自動追蹤、自動結束，而過期會出現在摘要裡一次。

每一筆提議與它的答案寫進 `proposals`；「伺服器說 `ask:false` 但對話裡還是問了」這件事無法完全擋住（agent 可以打任何字），
所以**要量**：`/v1/diagnostics` 記 `proposals.asked_inline` 與 `proposals.ask_true`，兩者對不上就是簡報沒被遵守。

---

## 5. 生命週期

### 5.1 看板項目

```
          人說／人確認提議／Backlog 依規則移入
                         │
                         ▼
   ┌──────────────── active ─────────────────┐
   │   (有 owner、有時鐘 evidence_due_at)     │
   │                                         │
   │ 所有綁定的執行都結束、至少一個交付、      │  新的派工／新的阻擋 finding
   │ 但沒有自動收尾的證據                     │◄──────────────────────┐
   ▼                                         │                       │
awaiting_closure ──(落地證據到了)──────────► done ──────(reopen)─────┘
   │   │                                     ▲
   │   └──(人：收下)─────────────────────────┤
   │                                         │
   └──(問過一次，7 天沒回答)──► done（closed_reason = unconfirmed，標「交付未確認」）

active ──(3 天沒有新證據、也沒有交付)──► 移回 Backlog（寫 moves，進摘要）
active／awaiting_closure ──(人：放棄)──► dropped
```

| 轉換 | 觸發 | 由誰 | 證據 |
|---|---|---|---|
| →`active` | 人建立、人確認提議、Backlog 依 §6 移入 | 人／規則 | `moves` 一筆 |
| `active`→`done` | 綁定的 task 全部 `landed` 或 `nothing_to_land`，且沒有未解的阻擋 obligation／finding | **規則（自動）** | broker 落地紀錄（D4） |
| `active`→`awaiting_closure` | 綁定的執行都已結束、至少一個交付，但沒有上一條的證據（非 code、session 自稱交付、落地還沒發生） | 規則 | task 終態＋交付 |
| `awaiting_closure`→`done` | 落地證據到了 | 規則 | broker |
| `awaiting_closure`→`done` | 人按「收下」（＝舊版 `accept_artifact` 的一般化，記成人的決定，是事實不是意見） | **人** | `decisions` 一筆 |
| `awaiting_closure`→`done(unconfirmed)` | 進入 3 天後在摘要問一次，再 7 天沒回答 | 規則 | 時鐘；卡上永遠標「交付未確認」 |
| `awaiting_closure`→`active` | 人按「還要改」 | 人 | 新開一筆待辦給 owner |
| `active`→Backlog | **3 天**沒有新的非機器證據、沒有交付、沒有等人的決定 | 規則 | 時鐘；`moves` 記 `stalled_3d`，進摘要 |
| `done`→`active` | 新的派工綁上來、或新的阻擋 finding（沿用舊版「新範圍重開」） | 規則 | broker／finding |
| →`dropped` | 人按「放棄」 | **只有人** | `decisions` |
| owner 不在了 | owner session 不存在且沒有接手者 | 規則 | 卡上標「無人負責」、進摘要；時鐘照走 |

**時鐘看的「證據」是什麼：** 綁在這個 `work_id` 上的任何事實——待辦的建立或結束、派工與它的終態、落地、session 的交付回報、
人的決定。session 的宣告（「我正在做」）是觀察，不算（D6）。root 自己直接在做、沒有派工的工作，靠它每一輪的交付回報推進時鐘；
回報漏了、被移回 Backlog，代價是一個動作：下一筆綁上來的事實（或人按「開始做」）就會把它移回看板（§6）。
**移回 Backlog 從來不刪東西，只是把「承諾」這件事說實話。**

**為什麼 `done(unconfirmed)` 可以安全地自動結束：** 它只是把人的看板清乾淨，**不是**宣稱落地。未落地的交付在 broker 自己的 inventory
（`unlanded`）裡仍然是一筆義務，那裡有 owner 與落地佇列；看板不需要用「永遠不關」來記住它。卡上保留「交付未確認」標記，任何新證據都會重開。

### 5.2 Session 待辦

```
open ──(對應 task 結束且落地／obligation 解決／交付涵蓋)──► done
open ──(owner session 結束)──► handed_off（移給 root；root 也不在 → 24 小時後 dropped，進摘要一行）
open ──(符合 §6 升級條件)──► 保持 open，另外在看板產生決定或提議
```

| 轉換 | 觸發 | 由誰 |
|---|---|---|
| 建立 | 派工被接受（root 的「收結果、落地」待辦）；`result.json`／`deliver` 的 `remaining`；非 user 的 obligation | broker（自動） |
| →`done` | task 終態＋落地（`landed`／`nothing_to_land`）；obligation 解決；後續的 `deliver` 涵蓋 | broker（自動） |
| →`done`（未落地） | task 終態但 `abandoned`，或 broker 的落地義務由人或 root 關掉 | broker（自動） |
| →`handed_off` | owner session 結束 | 規則：移給它的 root；`handoff` 則移給接手者 |
| →`dropped` | 移交後 24 小時仍無 owner | 規則，進摘要 |

**session 怎麼「不忘記」（目的二）：** 待辦在三個 session 容易忘的時刻被送回給它：
(1) 派工的簡報與 handoff 的 OPEN THREADS 帶上它的未完成待辦；(2) child 完成通知（已有的 durable notice）帶上 root 那一筆「收結果、落地」待辦；
(3) `GET /v1/orchestrator/sessions/:id/todos`（機器憑證）隨時可讀。**不再在每一則人送出的訊息裡夾信封**（§1.7-4；也是 43.6% 漏 deliver 的來源）。

### 5.3 Backlog

```
planned ──(§6 的任一移入觸發)──► 移入看板
planned ──(人：丟掉)──► dropped
planned，30 天沒人看過 ──► 每週摘要問一次「要留嗎？」；預設保留
```

**Backlog 永遠不自動刪除。** 人的規劃被機器清掉是最糟的錯誤（`board-design.md` §6-5、`timeline-design.md` §3.3 的同一條理由）。

### 5.4 3a、3b、3c 各自被哪一條規則擋住

| 壞法 | 現況的機制 | 擋住它的規則 |
|---|---|---|
| **#3a 瑣碎無人管理** | 自動執行紀錄與人類卡同表同生命週期，結束條件用人類那一套，所以永遠不結束；機器對帳持續「更新」它們 | (1) 瑣碎的東西根本不是看板項目，是待辦（§3）；(2) 待辦由 broker 事實**自動結束**，owner 消失就移交或丟棄（§5.2）；(3) 衍生狀態不落盤，機器不會再「碰」它們（§3.2） |
| **#3b 開始後沒人收尾、看不到進度** | 派工不綁看板項目（12.1%）；session 的 begin 不收尾（43.6%）；宣告的 span 讓死掉的 session 看起來還在做 | (1) **D8**：派工必須帶 `work_id`，沒帶就由 broker 綁或開待辦，絕不在別的專案另開人類卡；(2) 每個看板項目有 owner 與**時鐘**，3 天沒證據就回 Backlog、owner 消失就標「無人負責」；(3) 宣告不落盤（D6），「進行中」只由 broker 的活著的嘗試推出 |
| **#3c 做完了看板沒更新** | 收尾閘門要驗證紀錄，實際幾乎不產生，`closed` 0 張 | (1) **D4**：落地證據就足以自動結束；驗證是徽章；(2) 非 code 或沒落地的交付進收尾佇列，**問一次、有期限、有預設**；(3) task 落地→待辦結束→看板結束在同一個 DB、同一筆交易（§3.2） |

### 5.5 門檻怎麼來的

| 門檻 | 值 | 依據 |
|---|---|---|
| 看板停擺回 Backlog | **3 天** | 全部 12,900 筆事件裡，沉寂超過 1 天之後又恢復的只有 11 次，**超過 3 天之後恢復的 0 次**；已完成的 281 張卡，過程中最長的沉寂 p99 是 30.8 小時。三天沒動，在這份資料裡等於不會再動（限制見 §1.8） |
| 值得問（I2） | **24 小時** | 活得最久的 5%（p95 23.6 小時） |
| 看板的「短期」 | **7 天** | `start_on` 在七天內＝本週排入；配合每週一次的 Backlog 摘要 |
| 收尾佇列的預設結果 | 進入 3 天後問一次，再 7 天 | 給人一個完整的週末；之後結束但標「未確認」 |
| 提議過期 | 7 天 | 同上 |

門檻敏感度見 §7.3：**改門檻只會在「看板·進行中」與「看板·等收尾」之間移動，不會改變 Session 待辦與「從未開始」的 Backlog。**

---

## 6. 三者之間的移動規則

每一次移動是一筆交易，寫一筆 `moves`（`trigger`、`actor`、`evidence`），而且**自動的移動一定出現在每日摘要**（D2）。

| 從 → 到 | 觸發 | 由誰 | 證據 |
|---|---|---|---|
| **Backlog → 看板** | 有人派工並帶這個 `work_id` | root（派工時） | broker task 被接受 |
| | 人在 session 裡說「開始做 X」，session 的 begin 綁到這個既有項目 | 人（經 session） | 人送出的 run＋綁定 |
| | 人在看板按「開始做」（指派給某個 root）或「排入」並設 `start_on` | 人 | `decisions` |
| | `start_on` 進入七天內 | 規則 | 日期 |
| | 某個 session 接受了指派（`assignment_decision: accepted`） | session | workflow 收據 |
| **看板 → Backlog** | `active` 且 3 天沒有新的非機器證據、沒有交付、沒有等人的決定 | 規則 | 時鐘，`stalled_3d` |
| | `start_on` 過了 1 天仍沒有派工 | 規則 | 日期，`schedule_missed` |
| | 人按「放回 Backlog」 | 人 | `decisions` |
| | **不會**：已交付的（那是收尾佇列，不是回到規劃） | — | — |
| **Session 待辦 → 看板（升級）** | 需要人決定而且擋住工作（obligation `actorKind:user`、`waiting_user`） | session 提出，規則放上 | 產生「等你決定」，掛在它的看板項目上；沒有看板項目就只推播與留在 session |
| | 同一筆待辦的嘗試失敗第二次（兩次 `failure`／`timeout`／`spawn_failed`） | 規則 | broker 終態；有看板項目就掛上「卡住了：重試／換做法／放棄」的決定，沒有就產生提議 |
| | 符合 §4.2 的 I1–I3 | 規則 | 產生**提議**（不是直接上看板） |
| | owner session 結束、移交後 24 小時仍無人接 | 規則 | 摘要一行：「N 筆待辦沒有人接：轉給誰／丟掉」 |
| **看板 → Session 待辦（降級）** | 人對提議或項目按「不用追蹤」 | 人 | `decisions`；工作回到只有待辦 |
| **提議 → 看板／Backlog／待辦** | 人選「追蹤」／「之後」／「不用」；7 天沒回答 → 待辦 | 人／規則 | `proposals` |

---

## 7. 現有 787 張卡落在哪裡

### 7.1 規則（依序，先符合先決定）

```
輸入：audience（舊版 catalogDisposition，沒有的依 inferredSourceKey 推）、type、
      progress（Go ProgressOf）、未解的 user 決定、最後一筆非機器事件、是否開始過（§1.1）

S  audience ≠ human，或 type = coordination        → Session 待辦
     已結束（completed／canceled）                  → S.done
     active 且不是幽靈（§1.4）                      → S.live
     其餘                                           → S.autoclose（遷移時自動結束）
B  audience = human 且已結束                        → 看板·已結束（B.done）
B  有未解的 user 決定，或 active 且不是幽靈         → 看板·進行中（B.now）
K  從未開始過（沒有派工、交付、證據、勾掉的 checklist）→ Backlog（K.never_started）
B  開始過，閒置 ≤ 3 天                              → 看板·進行中（B.now）
B  開始過，閒置 > 3 天，投影是 delivered／verified／
     blocked／correction／review_testing            → 看板·等收尾（B.closure）
K  其餘（開始過、閒置 > 3 天、沒有交付）             → Backlog（K.stalled）
```

coordination 放進 Session 待辦，是因為它們是 session 之間的協調紀錄（例：「通知 Clawdfather：/git 等路由佔住 RemoteServer 共用 queue」），
舊版自己也規定它們沒有交付生命週期。

### 7.2 結果（門檻 3 天）

| 結構 | 細分 | 張數 | 佔全部 | 投影狀態組成 |
|---|---|---:|---:|---|
| **Session 待辦** | S.done（已結束） | 337 | 42.8% | landed 304、canceled 19、settled 14 |
| | S.autoclose（遷移時自動結束） | 260 | 33.0% | delivered 186、blocked 54、unknown 17、execution 3 |
| | S.live（進行中） | 5 | 0.6% | 目前這幾個 child 的執行紀錄 |
| | **小計** | **602** | **76.5%** | |
| **看板** | B.now（真的正在發生） | 31 | 3.9% | delivered 24、execution 3、unknown 2、blocked 1、correction 1 |
| | B.closure（等人收尾） | 66 | 8.4% | delivered 52、verified 7、correction 3、blocked 2、review_testing 2 |
| | B.done（已結束） | 11 | 1.4% | landed 8、canceled 3 |
| | **小計** | **108** | **13.7%** | |
| **Backlog** | K.never_started | 60 | 7.6% | planning 53、unknown 4、execution 3 |
| | K.stalled | 17 | 2.2% | planning 12、unknown 5 |
| | **小計** | **77** | **9.8%** | |
| | **合計** | **787** | 100% | |

**只看給人看的卡（舊版 `audience=human` 的 192 張扣掉 7 張 coordination＝185 張）：**

| | 張數 | 佔人類卡 |
|---|---:|---:|
| 其實是 **Backlog** | **77** | **41.6%** |
| 等人收尾 | 66 | 35.7% |
| 真的正在發生 | 31 | 16.8% |
| 已結束 | 11 | 5.9% |

**看板現在有 41.6% 其實是 Backlog；「現在」只有 16.8%。** 等收尾的 66 張，正是 #3c 的具體清單（例：「自動追蹤名單上線」
「錯誤修復提案管理頁：commit、push、部署」，session 都說交付了，看板一直沒關）。

### 7.3 門檻敏感度

| 停擺門檻 | B.now | B.closure | K.stalled | K.never_started | S（合計） | B.done |
|---|---:|---:|---:|---:|---:|---:|
| 1 天 | 14 | 82 | 18 | 60 | 602 | 11 |
| **3 天** | **31** | **66** | **17** | **60** | **602** | **11** |
| 7 天 | 97 | 17 | 0 | 60 | 602 | 11 |

**Session 待辦 602 與「從未開始」60 在三種門檻下完全不動**；門檻只決定人類卡裡「進行中」與「等收尾／停擺」的切法。

### 7.4 誰來判、判錯了怎麼救

- **誰判：規則（§7.1），人批准一次。** 分類器唯讀地跑在舊資料上（舊 store 一個字都不寫，`plan.md` §4），產出每一列的歸屬與
  **理由碼**（例：`audience_agent`、`never_started`、`idle_gt_3d+delivered`）。遷移是一個**批次提議**：使用者在一個畫面上看到
  「看板 31／等收尾 66／Backlog 77／自動結束 260／封存 348」，可以逐列改，按一次「套用」。**沒有批准之前，新看板是空的，舊卡照舊唯讀顯示。**
- **先看最可能錯的：** 列表依信心排序，低信心的排最前面——
  1. **雙胞胎**：人類卡顯示「從未開始」，但同一件事的執行紀錄已落地（§1.7-1，至少 9 對，跨專案的另計）。這些依規則會進 Backlog，實際上應該是
     「已結束」。遷移前先跑一次綁定比對：同一個 root session、六小時內、task 的 `work_item_id` 或 graph 相符的，列為「可能是同一件事」讓人確認。
     **不用標題自動合併**（舊版的規則，理由不變：標題不是身分）。
  2. **靠門檻決定的**：`K.stalled` 與 `B.closure`，因為它們隨門檻移動（§7.3）。
  3. **span 被模板預設成 output 的**：已用「開始過」的嚴格定義排除（§1.1），列出來給人看。
- **判錯了怎麼救：** 每一列遷移都寫 `moves`（`trigger: migration`，含規則版本與理由碼）。任何一列都能用一個動作移到另一個結構；
  舊卡永遠讀得到（`migrated_from` 指回舊 id）。分類器是純函數、輸入唯讀，**整批重跑是冪等的**：改了規則重跑，只會移動人還沒手動改過的列
  （人改過的以人的為準，`moves.actor = user`）。
- **遷移自動結束的 260 張 S.autoclose**：只是讓它們離開「未結束」，broker 對未落地交付的紀錄不受影響。

---

## 8. 人類參與點：少而準（#4、D5）

| 階段 | 什麼時候把人拉進來 | 人看到什麼 | 人能下的指令 | 管道 | 預算 |
|---|---|---|---|---|---|
| **建立** | 提議符合 I1–I3，且人在場或到了摘要時間 | 一句話的目標＋為什麼被提議（I1／I2／I3） | 追蹤／之後（Backlog）／不用 | 對話尾端（在場）或待確認區 | 每 session 每回合 ≤1，每天推到人面前 ≤3 |
| **派工** | 不拉人（派工是 root 的事）；Backlog 項目被派工時在摘要告知一行 | — | —（人可以事先在 Backlog 按「開始做」） | 摘要 | — |
| **進行中** | **只有擋住工作的決定** | 問題、選項、誰在等 | 回答 | **推播**＋看板「等你決定」 | 沿用 notify 上限（每 task 5、每小時 30） |
| **交付** | 沒有自動收尾證據的交付，在 3 天後 | 交付摘要、輸出連結、還差什麼 | 收下／還要改／放回 Backlog／放棄 | 看板收尾佇列＋摘要一行 | 批次處理（一次一個畫面） |
| **落地** | 不拉人 | 摘要一行「N 件已落地」 | — | 摘要 | — |
| **定期** | 每天一次 | 完成了什麼、什麼停擺回了 Backlog、有幾個提議、有幾件等收尾、有沒有無人負責的 | 從摘要直接跳到對應的清單 | 每日摘要（一則） | 1 則／天 |
| **規劃** | 每週一次 | Backlog 裡 30 天沒人看的 | 留著／丟掉／排入 | 每週摘要 | 1 則／週 |

**人能在看板上下的指令（完整清單）：** 開始做、排入、放回 Backlog、收下、還要改、放棄、轉交、追蹤這個、不用追蹤、回答決定。
在 session 裡用自然語言說的，由 root 轉成同一組指令，actor 記 `user_via_session:<run>`（run 證明是人送出的訊息）。

這是**刻意推翻舊版的一條原則**：舊版 `project-board.md:30-34` 是「沒有手動狀態選單，狀態全由證據推導」。本設計保留「狀態由證據推導」，
但加上**人的決定也是一種證據**（收下、放棄、排入），因為量到的結果是：只靠自動證據，收尾永遠不會發生（#3c）。
人的決定不能覆蓋 broker 的事實（例如不能把沒落地的東西標成已落地），只能決定「我接受這個結果」或「不做了」。

---

## 9. 實作順序與最小可驗收的第一步

前提：看板頁的舊版照搬（`%905`）先落地，給舊卡一個唯讀、1:1 的檢視。本設計是新 app 自己的看板，排在它後面。

| 步 | 內容 | 完成的判準 | 依賴 |
|---|---|---|---|
| **1（最小可驗收）** | **唯讀的三軌投影**：`GET /v1/board/tracks?project=`，把舊的 787 張依 §7.1 分成三軌，每列帶理由碼；同一份規則的 CLI `clawdline board tracks` | 對本文附錄的快照，數字與 §7.2 完全相同（602／108／77 與各細項）；每一條規則有一個會失敗的 fixture 測試；**零寫入** | 無 |
| 2 | Session 待辦：`todos` 表，由 Go broker 的事實自動建立／結束／移交 | failure injection：child 死掉、root 死掉、落地比結果先到、`result.json` 重送兩次 → 待辦都收斂且只一筆 | broker B3（通知） |
| 3 | 看板與 Backlog：`work`、`board_items`、`backlog`、`moves`；承諾規則、3 天時鐘、落地自動結束、收尾佇列 | fixture 重現 §5.4 的三種壞法，各自被對應規則處理 | broker B5（落地） |
| 4 | 人類參與點：`proposals`、`decisions`、待確認區、每日／每週摘要、§4.4 的拒絕碼 | 每個拒絕碼一個測試；`asked_inline` 與 `ask_true` 在 diagnostics 對得上 | 3 |
| 5 | 遷移：批次提議畫面、人批准一次、`moves` 記錄 | 重跑冪等；人改過的列不被覆蓋 | 1、3 |
| 6 | 前端：看板頁三區、Backlog 分頁、session 詳情的待辦面板 | reviewer 對照本文 §3.3 | `%905` 落地、3、4 |

**為什麼第一步是唯讀投影：** 它是最便宜的一步，卻能讓使用者**對著自己的資料**判斷規則對不對（「這 77 張真的是 Backlog 嗎？」），
在任何寫入、任何畫面改動之前。規則如果錯了，改的是一個純函數，不是一份遷移過的資料。

**明確不移植的：** 每則訊息的 workflow 信封與 `begin` 分類（§1.7-3、4，#1 的來源）；`automatic_state_reconciled` 寫進持久歷史（D6）；
以 session cwd 決定卡片的專案（§1.7-2）。

---

## 10. 需要使用者拍板的（都已選安全的預設繼續做）

| # | 問題 | 預設 | 為什麼這是安全的一邊 |
|---|---|---|---|
| 1 | Backlog 的名稱 | **Backlog**（中英同字，副標「已規劃，尚未排入」） | 是你自己用的字；中文候選都撞到既有用詞（§3.5） |
| 2 | 停擺回 Backlog 的門檻 | **3 天** | 資料裡沉寂 3 天後恢復的 0 次；改門檻只影響人類卡的切法（§7.3） |
| 3 | 「短期」的長度 | **7 天** | 配合每週一次的 Backlog 摘要 |
| 4 | 沒人回答的提議 | **7 天後留在 Session 待辦（不追蹤）** | 不回答就不塞進你的看板；它仍被自動追蹤與結束 |
| 5 | 沒人回答的收尾 | **問一次，再 7 天後結束並標「交付未確認」** | 清掉看板但不宣稱落地；broker 的未落地紀錄不受影響；任何新證據都會重開 |
| 6 | 舊的 787 張怎麼遷 | **批次提議，你批准一次才套用**；之前新看板是空的 | 第一次搬資料不該由機器自己決定 |
| 7 | 在 session 裡說的話算不算人的指令 | **算**，actor 記 `user_via_session:<run>` | 你大部分的指令本來就在對話裡；run 證明訊息是人送的 |
| 8 | 每則訊息夾帶 workflow 信封 | **不移植** | 它是 agent 自己開卡的來源，也是 43.6% 沒收尾的那條路 |
| 9 | 卡片的專案 | **建立時明確指定**；沒指定才用 session 的 cwd，並標 `project_inferred` | 避免 root 在 A 專案目錄裡替 B 專案開卡（§1.7-2） |
| 10 | 推翻舊版「沒有手動狀態」的原則 | **推翻**：人的「收下／放棄／排入」是證據 | 只靠自動證據，`closed` 十一天 0 張（§8） |

---

## 11. 這份文件沒有做到的事

- **沒有改任何實作，也沒有跑 repo 的 build 或測試。** 為了逐張算 progress，把 `internal/adapters/board` 複製到任務的暫存目錄、
  加一支 30 行的量測程式編譯執行；repo 本身沒有動。
- **沒有量卡片的讀取**（舊版不記），所以「沒有被任何人打開過」這一項**量不到**。
- 「人在場」的定義（30 分鐘內收過人送出的訊息）與提議的每日數量，**沒有在真實流程上試過**；§4.3 的數量是上限估計。
- 所有數字來自一台機器、十一天。停擺門檻的依據受這個視窗限制（§1.8）。
- 雙胞胎只量到同專案的（至少 9 對）；跨專案的只舉了例子，沒有全數統計。
- 沒有讀任何 token 或 secret；`orchestrator.json` 只抽 task 的 id、狀態、落地欄位，沒有讀 `secret_hash`。唯一讀過的憑證是新版自己的
  `local-token`，用來對 `:7727` 發唯讀的 GET。

---

## 附錄：數字從哪裡來

腳本與輸出都在任務 `9f996840` 的 `artifacts/`（`measure.py`、`classify3.py`、`boardmeasure.go`、`metrics.json`、`metrics2.json`、
`classify3.json`、`twins.json`），快照與中間檔在 `snapshot.tar.gz`（0600）。重現：解開 tarball，放入兩支腳本，
`python3 measure.py && python3 classify3.py`——§7.2 與 §7.3 的數字會原樣出現（已實際重跑一次確認）。§9 第一步的驗收就是拿同一份快照對這些數字。

| 數字 | 怎麼量的 |
|---|---|
| 787 張卡、revision 6403 | 2026-09-18 01:02:40 UTC 以 `cp` 複製 `project-board.json`、`project-board-history/`、`project-board-workflow.json` 到暫存目錄，之後只讀副本 |
| 每張卡的 progress、group、audience | `boardmeasure.go`：對副本呼叫 `ProgressOf`／`ListSummaryOf`（本 repo `internal/adapters/board/progress.go`） |
| actor 分布、建立者、建立後更新 | 逐行解析 564 個 history 檔；actor 規則讀 `ProjectBoardHTTP.swift:260-268`、`:419-432` |
| 網頁沒有改卡片的操作 | `grep` 舊版 `Resources/web/app/js`：`boardCommand` 只被 `input/board-settings.js` 用於 `set_enabled`／`set_ai_consent` |
| 收尾閘門 | 讀 `ProjectBoardStore.swift:3440-3505` |
| 4,124 筆自動對帳、3,115 筆擺盪 | 從 history 的 `automatic_state_reconciled` 摘要解析「from X to Y」 |
| task 狀態、落地、`work_item_id` | 從 `~/.config/clawdline/orchestrator.json` 抽 774 個 task 的 id／state／landing／work_item_id（不含 `secret_hash`） |
| 卡上 task 狀態 vs 真實狀態 0 筆不一致 | 卡上 `links[kind=task, source=broker].attemptState` 對 task 的 `state` |
| live session | `GET :7727/v1/sessions`（本機 token，唯讀），14 個 |
| workflow 528 次 run、漏 begin／deliver | `project-board-workflow.json` 的 `runs[].missingFollowUp`（保留 256）＋`retiredGaps`（已淘汰 272） |
| 沉寂後恢復次數 | 每張卡相鄰兩筆非機器事件的間隔 |
| `TodoWrite`／`update_plan` 使用率 | `grep -l` 近 10 天的 `~/.claude/projects/**/*.jsonl`（前 400 份）與 `~/.codex/sessions/**/*.jsonl`（前 300 份） |
| 工作線數量 | `orchestrator.json` 的 task 依（root session, project_dir, 日）分組，排除 61 個排程 task |
| 三軌分類與敏感度 | `classify3.py`（§7.1 的規則），門檻 1／3／7 天各跑一次 |
