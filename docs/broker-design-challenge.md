<!-- retired-app-record: 舊 app 設計的第三方覆審，量於 2026-09-18；該 app 已於 2026-09-19 停用，7717 無人監聽 -->

> **主體與時間：** 這份文件記錄的是 **舊 app 設計的第三方覆審**，量於 **2026-09-18**。
> **舊 Swift app 已於 2026-09-19 停掉、取消登入時啟動，7717 現在沒有人在聽。**
> 文中寫成現在式的「舊 app 還在跑」「7717」都是**量測當下**的事實，刻意保留，
> 用來對照還有哪些功能要 migrate、當初怎麼實作——**不要照著它設定今天的 daemon**。

> **實作依據是 [`docs/design-decisions.md`](design-decisions.md)（2026-09-18 起）；本文保留為分析材料，與它衝突之處以它為準。**

# 第三方挑戰：「照搬」的那些項目，有多少只是歷史

> 使用者原話（2026-09-18）：「所以我們沒有重新設計這看起來是 legacy 產物的東西嗎？也就是以第三方的角度來看是不合理的設計」
>
> 本文的角色是**唱反調的第三方**：當作從來沒看過這個產品，只看現在的設計與資料。
> 「一直都這樣」「先前的分析已經說過」「為了跟舊版對齊」都不算理由，除非今天的資料還撐得住它。
>
> 範圍：`docs/broker-design.md` §7 標「照搬」的項目，以及 `docs/board-design.md`、`docs/timeline-design.md` 標照搬的項目。
> **只寫文件，不改實作程式。** 跟既有文件意見不同的地方直接寫出分歧與雙方證據，不為了一致而軟化。

量測時間：2026-09-18 08:55–09:15。repo 在 `26c0152`；broker 第一波（B1）在 `d5a6ca3` 落地，本文讀的就是它。
舊 app（`~/code/clawdline`、`~/.config/clawdline`）**全程唯讀**。讀 `orchestrator.json` 前，先把所有名稱含
`secret`／`token`／`key` 的欄位去掉才處理；`remote-audit.jsonl` 只用 `event` 名稱、時間，以及四個細節欄位（`attempt`、`arm`、`result`、`generation`）。沒有讀任何 token 或 secret。
新版只用自己的 `local-token` 發過兩個唯讀 GET。

---

## 0. 結論先講（六句話）

1. **照搬的項目是 24 項，不是 25。** 逐項重驗後：**維持照搬 14、應改 8、應廢除 2。**
   應改的 8 項多半是「目的是對的，形狀是歷史」：要搬的是目的，不是舊版當年達成目的的那個形狀。
2. **看起來像 legacy 產物的東西，幾乎都能追到三個根。**
   (a) **拿一份 JSON 檔當 store**：隔離區、「先寫 log 再寫聚合」、`landing_paths` 另存成一個檔、看板整份重寫。
   (b) **iTerm2／AppleScript 與讀 transcript 的年代**：整台機器只有一條終端機 lane、重打五次、四分鐘時鐘＋拿 progress 當收據。
   (c) **同一件事有兩三種拼法**：完成有三條路、冪等至少九種、「落地了沒」有三份答案、寫入範圍分散在四個欄位。
3. **最硬的證據是使用量。** 31 天裡：
   - respawn 用了 4 次，最後一次是 9/09。
   - 落地佇列的 advance 試了 1 次，而且失敗，成功送達 0 次。
   - 用 progress 證明簡報送達 2 次；第二次打入簡報 1 次。
   - `result.json` 救回成功 0 次。
   - Arm 2 關閉 1 次。
   - **iTerm 分頁的 child 從 9/07 那週起是 0。**
4. **反方向的證據一樣硬：有好幾項照搬 B1 沒有搬，而 B1 在那些地方原樣重現了舊版的事故形狀。**
   - G2：落地紀錄被安靜覆寫。
   - H4：`claims: []` 讀起來像一個承諾。
   - 讀不到被當成不在。
   - 三個寫入者同時往同一個終端機打字。

   這幾項是**必要**，不是歷史（§4）。
5. **最大的分歧不在 broker，在看板。** `board-design.md` C1「整份文件重寫照搬」與 `broker-design.md` §6.9「看板是同一個 DB 裡的投影」
   互相矛盾，兩份文件都沒有指出對方。而 C1 引用的量測（`b6f4f555`）比較的是「手刻 journal 拆檔」，不是 SQLite。**本文站 §6.9。**
6. **如果只做三件事**（§8）：
   ① 簡報送達改用具型別的 `accepted` 收據；
   ② 完成只有一條路，收據只有一張表；
   ③ 落地只有一份事實：不搬舊佇列與 Arm 2、Arm 1 綁定到交付的 head、看板與時間軸改成投影。

   **三件都不需要退回 B1；B1 的 7 個測試沒有一個要改斷言**（§9）。

---

## 1. 方法

對每一項問三個問題，每一題都要有證據，不能只有直覺：

| 問題 | 去哪裡量 |
|---|---|
| 它解決的問題**今天還存在嗎**？ | 舊 app 的 `orchestrator.json`（773 筆 task，08-29→09-18）、`remote-audit.jsonl`（36,093 行，08-18→09-18）、`project-timeline.json`、本 task 自己的 `CHILD.md` |
| **如果今天從零開始**，會這樣做嗎？不會的話，合理的做法是什麼？ | 舊 repo 的原始碼與 commit 原文；新版 B1 的原始碼 |
| 它的存在是**必要**還是**歷史**？ | 上面兩者的交集。「歷史」的定義是：當初的理由在今天的資料或今天的平台上已經不成立 |

結論只有三種：

| 結論 | 意思 |
|---|---|
| **維持照搬** | 問題還在，而且從零開始也會這樣做 |
| **應改** | 問題還在，但舊版的形狀是某個已經不在的前提造成的。要留的是目的 |
| **應廢除** | 問題已經不在，或者這個機制從沒真的運作過 |

每一項另外寫出**改的代價**、**不改的代價**，以及 **B1 今天實際怎麼做**（B1 在 `d5a6ca3` 落地，是本文唯一會被改動影響的程式）。

**兩個子代理**幫我讀了程式：一個讀舊 repo（17 題），一個讀 B1 對 24 項的實作。兩個都完整回報了。
其中要當作證據的行號，我自己重讀過附錄 B 列的那些，全部與回報一致。

---

## 2. 三個前提先更正

### 2.1 照搬是 24 項

`broker-design.md` §7 的分級表逐列數下來是**照搬 24、小改 12、重新設計 11，合計 47**。派工說明寫「照搬 25、小改 12、重新設計 11」，
加起來是 48，跟表格對不上。如果第 25 項指的是 §6.3「擁有權重新設計、規則照搬」另外算一次，那它就是 #10，本文已經涵蓋。

照搬的 24 項：#8、9、10、12、13、14、16、17、18、19、20、21、22、24、26、27、28、29、30、31、32、34、41、47。

### 2.2 分級的時間點比 B1 早

`broker-design.md`（`89e3914`，08:41:43）寫明它讀的是 B1 分支 head `29a5cf5`（08:10）。
但 B1 在落地前又多了兩個 commit：

- `eefa318`（08:29）：「**Four minutes of silence is not a spawn failure.**」B1 在第一次真的跑的時候量到：
  拿 progress note 當 child 還活著的證明，會把一個活著、在工作的 child 記成 `spawn_failed`（B1 第一次實跑時派出的一個 child），因為簡報明文叫 child 不要送心跳。
- `5feea4b`（08:32）：「**One way to change a task record, so a slow step cannot write back the past**」。

所以 **#16 與 #18 被評為照搬時，落地的程式已經用自己的量測推翻了它們**。這不是誰的錯，是順序的問題；
但它正好是使用者問的那一類：「先前的分析說過」被當成理由，而先前的分析沒看到最新的證據。

### 2.3 有幾項「連理由一起搬」，但理由根本不存在

`broker-design.md` §1 對照搬的定義是「**行為與理由一起搬**」。以下幾項找遍 commit 與文件都找不到理由：

| 項目 | 找了哪裡 | 結果 |
|---|---|---|
| 簡報「最多重打**五**次」 | `762e09ce`（夾帶進來的）、`57dbae4f` | 五這個數字沒有任何紀錄 |
| `result.json` 救回要「**30 秒**內兩次觀察」 | `9d1676e9` 的 body 是空的；`tools/validate-task-result.mjs:124-127`、`docs/orchestrator.md:1070-1075` | 只說「age 不是同意」，沒說為什麼 SHA-256 綁定之後還要等 |
| 看板與時間軸的佇列深度「讀 **4**、寫 **8**」 | `17f444b3` 的 body 是空的 | 沒有 |
| `store` 隔離區的五種壞法 | `6b083838` 的 body 是空的 | 只有 `architecture-refactor.md` 一句「instead of treating every read failure as an empty registry」 |

理由不存在的東西，沒辦法「連理由一起搬」。

### 2.4 派工說明點名的五類，各落在哪裡

| 類別 | 落在哪幾項 |
|---|---|
| 把「先前的分析已經說過」當成理由，沒有重新驗證 | 看板 C1（`b6f4f555` 比的是手刻 journal）、看板 D5（「sound and should be kept」是在 Swift 單一序列佇列的前提下說的）、#14（`restartGrace` 誤讀）、#16／#18（沒看到 B1 的 `eefa318`）、#8（「vi」這個理由過時）、#21（08-25 的讀數）、「25 項」這個數字 |
| 為了跟一個即將退役的 app 對齊而保留 | #13（整台一條 lane 是 iTerm 的形狀）、#34（它要防的併發落地集中在舊 app 自己的 repo）、#24（嵌入 node 程式，而不是用 Go 的 CLI）、看板 B1（推導綁在舊 `StoredItem` 上）、看板 A1／B3（8 個沒人讀的欄位，含 `entitlement`） |
| 同一件事有兩三種拼法 | §6：完成、冪等、落地事實、寫入範圍、佇列與租約、wire 欄位 |
| 以畫面 1:1 為由保留的後端設計 | §6.6（`tasks.schema.json` 的雙拼法）、看板 A1 的 19 個欄位、看板 A7 的舊前綴選擇器（`board-design.md` 已評為重新設計，但「前端換掉之前兩套都要答」）、`swiftstore` 每 10.5 秒 decode 一次 6.75 MB（`broker-design.md` §2.2 已指出，`plan.md` §4 的使用者決定，屬於過渡） |
| 使用者點名的「看起來是 legacy 產物」 | §0 第 2 點的三個根：JSON 檔當 store、iTerm／transcript 的年代、多種拼法 |

---

## 3. broker 的 24 項，逐項

### 3.0 總表

| # | 項目 | 結論 | 一句話 |
|---|---|---|---|
| 8 | `config.json` 維持是檔案 | **維持** | 對；但舊理由（「人用 vi 改」）有一半過時 |
| 9 | store 壞掉不等於空的 store | **應改** | 原則必要；隔離區是 JSON 的產物，SQLite 真正會壞的是「一列」，B1 已經踩到 |
| 10 | 持鎖不做 I/O | **應改** | 字面規則跟 B1 正確的 `mutate` 衝突；要改寫成「持寫入權時不做外部 I/O」，並讓交易取代 mutex |
| 12 | 副作用在交易落盤之後 | **維持** | 必要；B1 只做了一半 |
| 13 | 終端機 I/O 只有一條有界 lane | **應改** | 「一個終端機一次一個寫入者」必要；「整台機器一條」是 iTerm 的歷史。B1 目前連一條都沒有 |
| 14 | 逾時：牆鐘＋重啟 grace＋空 inventory 不判死 | **維持**（要拆開） | 牆鐘必要；「重啟 grace」是誤讀，它只用在 linger |
| 16 | `spawning` 的 deadline 開火前先收 progress | **應廢除** | iTerm 年代的繞道；31 天觸發 2 次；B1 已經推翻 |
| 17 | 跟 child 說過話的 task 永遠不走 empty 回收 | **應改** | 從代理訊號換成依據：行程不在＋乾淨＋沒有 commit |
| 18 | 打進 tty 不算簡報成功；最多重打五次 | **應改** | 前半必要；「重打五次」31 天發生 1 次，失敗方向是重複簡報 |
| 19 | `spawn_failed` 要關掉它開的東西 | **維持** | 必要；B1 沒做 |
| 20 | `POST …/respawn` | **應改** | 降級成派工的一個欄位，排最後；31 天用 4 次，上限從沒被碰到 |
| 21 | 權限模式只有三個值 | **維持** | 成本 0；但證據是 08-25 的讀數，要重驗 |
| 22 | 完成通知：durable＋退避＋dead letter＋ACK | **維持** | 24 項裡資料最強的一項 |
| 24 | `result.json` preflight＋兩次觀察的復原 | **應改** | 驗證維持；內嵌 node 程式改成 CLI；30 秒窗口廢除 |
| 26 | linger deadline 持久化 | **維持** | 必要；而 #14 的兩條規則其實屬於這裡 |
| 27 | 判定執行者遺失需要兩次觀察 | **維持** | 必要 |
| 28 | 清掃前確認擁有者是否還活著 | **維持** | 必要 |
| 29 | 清掃不掛在讀取路徑；每輪有上限、輪流推進 | **維持** | 我原本以為是 JSON 的產物，查證後不是 |
| 30 | landing 可修改：同一道閘門＋`corrected_from` | **維持** | 必要；B1 已經重現了 G2 |
| 31 | landing 兩條 arm | **應改** | Arm 1 維持並補上綁定；Arm 2 廢除（31 天關閉 1 次） |
| 32 | 只拿紀錄上的 ref 比對 | **維持** | 必要；B1 有兩處沒照做 |
| 34 | 落地佇列不建表 | **應廢除** | 「不建表」對，但整個佇列形狀都不該搬：它從沒成功送達過一次 |
| 41 | 等待者的時鐘掛在「還活著的證明」上 | **維持** | 必要；而且應該成為 #34 的替代品 |
| 47 | inflight／inventory 以 common-dir 為鍵 | **維持** | 必要 |

---

### #8 `config.json` 維持是檔案 — **維持照搬**

- **還存在嗎**：存在。`Config.swift:876-887`（`4bbd4f94`）把不認得的 key 原樣保留，手改不會被 app 吃掉；新版 `nextconfig` 也是檔案。
- **從零開始**：會這樣做。設定是少量、人讀人改、跟任何交易都沒關係的資料。
- **必要或歷史**：必要。**但舊的理由有一半過時。** `mac-app-shell.md:535` 說「選單有『編輯 config』，人會用 `vi` 修」；
  那個選單在 `d33535fe`（08-18）之後打開的是 GUI 設定視窗（`main.swift:517-519`），不是檔案。
  站得住的理由是 `4bbd4f94` 的「未知 key 原樣保留」，以及仍然存在的「Reload config」（`main.swift:382`）。
- **改的代價**：0（只改理由的文字）。**不改的代價**：0。
- **B1**：照做（`nextconfig.go:38`）。讀檔失敗會默默退回預設值；對設定檔來說可以接受。

### #9 store 壞掉不等於空的 store — **應改：原則照搬，隔離區廢除，改去處理 SQLite 真正會壞的地方**

- **還存在嗎**：原則要防的問題還在，而且 B1 已經踩到了，只是換了位置。
  舊版處理的壞法全部是**單一 JSON 文件**才會有的（`OrchestratorPersistence.swift:72-103`、`Orchestrator.swift:10166-10240`）：
  不是一般檔案、不是 JSON object、`version` 不是整數或不是 1、缺 `tasks`、列重複。
  這台機器上 `~/.config/clawdline/orchestrator-quarantine/` **從來沒被建立過**（當場 `ls`），也就是說這套機制一次都沒觸發過。
- **SQLite 版本會怎麼壞**：B1 把整份 wire JSON 放在 `record` 欄（`broker-design.md` §6.1「已選」）。**壞掉的單位因此從檔案變成一列。**
  - `broker.go:168-173`：解不開的列直接 `continue`。beat、inventory、列表都看不到它，一個跑到一半的 child 會變成沒有人收。
  - `dispatch.go:72`：任何讀取錯誤都被當成「沒有這筆」，接著 `store/broker.go:106` 的 `ON CONFLICT(id) DO UPDATE` 把那一列蓋掉。
  - 執行中的讀取錯誤回 `500 internal`（`transport/http/orchestrator.go:472`），不是舊版的 `503 orchestrator_store_unavailable`；
    B1 的其他子系統其實已經有 `store_unavailable`（`auth.go:35`、`gate.go:195`）。
- **從零開始**：
  - DB 開不起來，回具型別的 503。
  - 一列解不開，那一列就是「讀不到」：列表與 inventory 要列出它，同 id 的派工要拒絕，**不能當成沒有**。
- **必要或歷史**：原則必要；隔離區、五種 JSON 壞法是歷史。
- **改的代價**：小。一個列層級的 `unreadable` 狀態、派工的拒絕、一個 503。
- **不改的代價**：照搬的話，隔離區會變成一段永遠不會執行的程式；而真正會發生的壞法（一列解不開）沒有人處理。

### #10 持鎖不做 I/O — **應改：字面跟 B1 正確的做法衝突，要改寫**

- **還存在嗎**：這條規則要防的兩件事，都是「記憶體裡的 registry 加一把全域鎖」的產物：
  `b6d1939a` 是在主佇列上對自己 `dispatch_sync`；`af83249d` 是在全域鎖裡把整個 store prune 兩次。
- **B1 怎麼做**：`mutate`（`broker.go:234-246`）**持著 `writeMu` 做 SQLite 的 SELECT 與 COMMIT**；`synchronous=FULL`，所以會 fsync。
  照字面，它違規。**但它是對的**：`5feea4b` 靠它消掉四種「慢步驟把過去寫回去」，而且有三個 failure-injection 測試守著。
  如果 B2／B3 有人照 §6.3 的字面去「修」它，那四個 bug 會回來。
- **從零開始**：寫入權就是交易。把讀、改、寫整段放在一個 `BEGIN IMMEDIATE` 裡，`writeMu` 就不需要了。
  **`broker-design.md` §3 用來說服大家搬到 SQLite 的「跨行程安全」，要到這一步才真的成立。**
  現在的 `writeMu` 只管得到同一個行程，而 `SaveBrokerTask` 是無條件的 upsert（`store/broker.go:102-109`）：
  另一個行程（CLI、備份、第二個 daemon）寫進來，不會被擋，也不會被發現。
  另外，`SetMaxOpenConns(1)`（`sqlite.go:184`）等於第二把隱性的鎖，而且目前沒設 `busy_timeout`。
- **改寫後的規則**：**持有寫入權（交易或鎖）時，不做外部 I/O：終端機、git、子行程、網路。store 自己的 I/O 就是這把鎖要保護的東西。**
- **必要或歷史**：意圖必要，字面是歷史。
- **改的代價**：小到中。store 要提供「在同一個交易裡讀＋寫」的 API。B1 的三個 race 測試直接呼叫 `mutate`，斷言不用改。
- **不改的代價**：留下一條照字面執行就會製造 bug 的規則；兩把鎖都不跨行程。

### #12 副作用在交易落盤之後 — **維持照搬（必要；B1 只做了一半）**

- **還存在嗎**：存在。這是 outbox 的形狀，跟語言無關。
  事故在 `architecture-refactor.md:931`：先打進終端機才 save，中間當掉就被記成 `spawn_failed`，但 child 其實已經收到了。
- **B1**：`29a5cf5`「Recording precedes effect」做到了 `queued` 先落盤（`dispatch.go:216`）。但：
  - `git worktree add` 發生在落盤之前（`:184`）。
  - `spawning` 與分頁 id 要等開完分頁、打完簡報才寫（`:240-249`）。中間當掉，紀錄會停在 `queued`、沒有分頁 id，
    **spawn 時鐘永遠不會啟動**，只剩 task 的 timeout。
  - push（`lifecycle.go:354`→`:363`）、relay（`messages.go:117`→`:121`）、收養 `.ready`（`watch.go:137`→`:157`）也都是先做後記。
- **改的代價**：小。每個副作用之前先寫一筆「要做」。**不改的代價**：上面那種孤兒。

### #13 終端機 I/O 只有一條有界 lane — **應改：「一個終端機一次一個寫入者」照搬；「整台機器一條」是 iTerm 的歷史**

- **還存在嗎**：「整台一條」的理由寫在原處（`RemoteServer.swift:235-237`）：
  > iTerm2 serializes Apple events itself; doing the same here keeps that queue visible and, crucially, keeps a modal sheet from occupying the HTTP/SSE queue.

  這兩個前提在 Go 版都不存在：
  - Go 的 HTTP 不是一條序列佇列。
  - child **自 9/07 那週起 100% 是 tmux**。8/24 那週是 iTerm 99／100；8/31 那週 iTerm 82、tmux 174；9/07 那週 tmux 297／298；9/14 那週 tmux 118／118。

  舊版自己的 tmux adapter 就寫著相反的話：「Terminal commands are serialized per destination so two different panes may be written concurrently」（`Tmux.swift:664-665`）。
- **`23ae603a` 真正修的不變量**是：完成通知繞過 lane，跟別的指令同時打進**同一個**終端機。那是「每個目的地一個寫入者」，不是「整台一個」。
  舊版的 lane 其實也已經有 per-terminal 的深度（`channelDepth = 2`，`TerminalCommandScheduler.swift:22-23`），只是執行時仍然一次只跑一個。
- **從零開始**：
  - 每個終端機一條序列 lane。
  - 全機一個 admission 上限，滿了回 `429 busy`，當作背壓。
  - iTerm adapter 在自己裡面把 Apple events 序列化。
- **B1**：**完全沒有 lane**（`orchestrator_wiring.go:33-35` 直接呼叫 `actions().Send`）。
  brief、notice pump、relay 三個呼叫端可以同時打進同一個終端機，**這正是 `23ae603a` 的形狀**。
  開 child 的 `NewTmuxSession` 也繞過了開 session 用的有界閘門（`start.go:62-63`）。
- **改的代價**：小。**不改的代價**：
  - 照字面搬：一個卡 15 秒的 iTerm AppleScript（`TerminalCommandScheduler.swift:19-21` 自己寫的數字）會讓每一個 tmux 通知一起等。
  - 什麼都不搬：B1 現在的交錯打字。

### #14 逾時：牆鐘＋重啟 grace＋空 inventory 不判死 — **維持照搬（牆鐘），但這一列把三個不同的機制黏在一起，要拆開**

- **牆鐘**：必要。app 關著的時候 child 仍然在跑，預算應該照真實時間算（`Orchestrator.swift:7444-7450`）。
- **「重啟 grace」是誤讀。** `broker-design.md` §2.5 說 `restartGrace = 20`「讓第一次讀數有時間進來」再判逾時。
  但它只用在 `rearmLingers`（`Orchestrator.swift:6567-6590`，重新掛上 linger 的 deadline），**逾時判斷沒有任何 grace**。這條屬於 #26。
- **「空 inventory 不判死」** 來自 `3a7adb8e`，也是 linger／關分頁的規則，同樣屬於 #26。
- **但它背後的原則（讀不到是未知，不是不在），B1 在別處違反了**：
  - `orchestrator_wiring.go:31` 只回傳 `Sessions`，把 inventory 的 `Complete` 丟掉了（對照 `app/inventory.go:54-55`）。
  - `watch.go:219-230` 找不到分頁就是 `alive=false`，接著判 `spawn_failed`。

  一次不完整的盤點，就能把一個正在開的 child 判死。
- 另外，B1 從 `CreatedAt` 起算（`record.go:264`），舊版從 `briefedAt`。不算錯，但契約要寫清楚。
- **改的代價**：文件拆開是 0；`spawnVerdict` 多一個「未知」輸入是小。
- **不改的代價**：做 B2 的人會去找一個不存在的「逾時 grace」來照搬；B1 的誤判留著。

### #16 `spawning` 的 deadline 開火前先收 progress — **應廢除（作為設計項目）：B1 已經用自己的量測推翻了它**

- **還存在嗎**：它是 `72ed3e46`／`95f6a30b`（08-28）的繞道。那次的根因是 iTerm2 繼承了 root 的 `CLAUDE_CODE_*` 環境變數，
  transcript 根本沒被寫出來，只剩四分鐘時鐘會開火。**iTerm 分頁的 child 自 9/07 起是 0。**
- **用量**：`orchestrator.brief.progress` 在 31 天裡觸發 **2 次**（09-11、09-15）。
- **根本的矛盾**：簡報明文要 child **不要**送心跳（本 task 的 `CHILD.md`：「do not send heartbeat status」）。
  拿一個被禁止送出的訊號當「活著」的證明，結果就是 B1 的 `eefa318` 量到的：一個活著、在工作的 child 被記成 `spawn_failed`。
- **從零開始**：簡報送達的證明應該是**一張具型別的收據**，不是從沉默或旁證推論。
  舊版的簡報已經要 child 第一句逐字說「收到 Clawdline 派來的任務：…」，但**沒有任何程式讀這一句**：
  它在 `OrchestratorChildBrief.swift:41-46` 產生，Sources／Resources／Tests 裡沒有任何消費者。
  它存在的原因是「an assistant answers the line before it opens the file」（`939d17d0`），也就是只給人看。
  把它換成一個用 task secret 簽的 `accepted`（HTTP；沒有 loopback 時用跟 `progress.json` 一樣的檔案退路），
  就是 `plan.md` §1 第 7 條那條收據鏈（accepted → executed → delivered → observed → acknowledged）的**第一格**。
  今天這一格是空的。
- **B1**：progress 仍然可以把 `spawning` 升成 `briefed`（`lifecycle.go:50-53`、`watch.go:106-111`）。這不是錯，順序也無害；
  要廢除的是「**把它當成設計依據**」。
- **改的代價**：小。一條路由或一個檔、簡報多一行、`spawnVerdict` 多一個輸入。
- **不改的代價**：四分鐘時鐘靠的訊號，是簡報叫 child 不要送的東西。

### #17 跟 child 說過話的 task 永遠不走 empty 回收 — **應改：從「代理訊號」換成「依據」**

- **還存在嗎**：D1 的損害是刪掉一個**行程還在、還沒 commit** 的 worktree。「說過話」只是代理訊號：
  child 如果照簡報保持安靜，這條不變量就保護不到它。
- **家規自己寫了正確的形狀**（本 task `CHILD.md`，2026-09-11 的教訓）：
  「a process is gone only when the system answers that there is no such process; **unknown never authorises a removal**」。
- **從零開始**：要移除一個 worktree，下面三件事都必須由系統正面回答：
  - 擁有者的行程不在；
  - checkout 是乾淨的；
  - 分支上沒有 base 以外的 commit。

  「有沒有說過話」不需要。舊版的 `orchestrator.worktree.kept` 有 3,699 筆（原因是 `dirty` 的 3,676 筆），說明「乾淨」這一條已經在擋了。
- **B1**：不回收任何 worktree（`git/worktree.go:57` 的 `RemoveWorktree` 沒有呼叫者）。
  但 inventory 會把 `branch_empty`／`merged_and_clean` 標成 `dispose`（`inventory.go:250-263`），**不看行程**。
  把 #14 的缺口接上去，就是：一次不完整的盤點 → `spawn_failed` → 0 commit、checkout 乾淨 → 出現在 droppable。
  **這正是 D1 那條鏈，只差最後一步刪除。**
- **改的代價**：小。B8 寫回收時用依據，不用代理訊號。**不改的代價**：B8 會建在一個會被沉默繞過的條件上。

### #18 打進 tty 不算簡報成功；最多重打五次 — **應改：前半維持，後半廢除**

- **前半必要。** `eefa318` 的對話框事件是現成的證據：Return 回答了 workspace trust，游標停在「No, exit」。
  「打進去了」跟「收到了」是兩回事。B1 照做，打完仍然是 `spawning`。
- **後半「最多重打五次」**：
  - 用量：audit 的 `orchestrator.brief.inject` 依 `attempt` 欄位分組，第一次 1,291 筆，**第二次 1 筆**（09-07 的一個 task），第三次以上 **0**。
  - 「為什麼是五」找不到任何紀錄（§2.3）。
  - 重打失敗的方向是**重複簡報**。`fb54fb68`（09-13）是 Root Assignment 被打進同一個 root 兩次，修法原文是
    「no composer state, missing receipt or elapsed time licenses a second send」。**舊版自己已經對 root 廢除了重打。**
- **B1**：不重打，改成「等到畫出有框線的輸入列才打」（`dispatch.go:514`，最多 90 秒）。這比重打好。
- **改的代價**：0。**不改的代價**：搬一個 31 天只觸發一次、而且一出錯就是重複簡報的計數器。

### #19 `spawn_failed` 要關掉它開的東西 — **維持照搬（必要）**

- `3e37e8ec`：「four dead tabs were still running … the next two spawns timed out for exactly the reason the first four had」。
  這是擁有權規則：誰開的誰關，跟語言無關。
- **B1**：沒做。`ports.Launcher` 沒有關閉的方法；`clawdline-task-<id8>` 這個 tmux session、worktree 和分支都留著。
- 注意，只在**證明了**的時候才關：`choosing` 時關；分頁已經不見就沒有東西可關；盤點不完整時不關（見 #14）。
- **改的代價**：小。**不改的代價**：B1 每一次 `spawn_failed` 都留下一個活著的助理。

### #20 `POST …/tasks/:id/respawn` — **應改：降級成派工的一個欄位，排最後**

- **問題還在嗎**：大多不在了。`52d74c88`（08-28）那時，`spawn_failed` 佔 206 次派工中的 34 次（16.5%，其中 33 次是 Codex）；
  今天是 773 筆裡 11 筆（1.4%）。
- **用量**：audit 31 天 **4 次**，最後一次是 09-09。registry 裡 3 筆有 `respawn_of`，`respawn_generation ≥ 2` 的是 **0**，
  所以「沿著整條鏈算、最多兩次」這個上限從來沒被碰到過。
- **從零開始**：值得留的只有 D3 那一半：**複製 `task.json`，不要讓 root 重寫一遍**。
  這可以做成派工的一個欄位（`respawn_of: <id>`，由 broker 複製檔案並檢查鏈長），不需要一條獨立路由。
- **B1**：沒做。
- **改的代價**：0（不做這條路由）；未來加一個欄位就夠了。
- **不改的代價**：一條路由＋整條鏈的計數＋測試，服務 0.4% 的派工。
  `broker-design.md` 說 `cutover.md` §2.2「漏掉」它而特別補上。**從用量看，漏掉它不算缺陷。**

### #21 權限模式只有三個值 — **維持照搬（成本 0；但證據過期，要重驗）**

- 三個值都在用：`full` 658、`edits` 75、`ask` 40。
- 「沒有 `auto`」的證據，是 2026-08-25 一個下午主要在 Haiku 上讀回來的（`5733f8f0` → `55d8e0c7` → `190983d2`）。
  Claude Code 與 Codex 之後都改過版；**`auto` 今天在 Haiku 上是否仍然會退成 `manual`，沒有量**。
- **改的代價**：0。**不改的代價**：0。只要在契約說明裡寫明「這是 08-25 的讀數」，下一個想加 `auto` 的人就知道要先重量。
- **B1**：照做（`draft.go:132-138`）；schema 沒有 enum（`orchestrator.schema.json:165`），可以補。

### #22 完成通知：durable notice＋退避＋dead letter＋ACK — **維持照搬（必要；24 項裡資料最強的一項）**

- 699 筆通知的嘗試次數：1 次 56、2 次 226、3 次 224、4 次 122、5 次以上 70。
  `completion.deferred` 545 次，dead letter 9 筆，ACK 690 筆。**root 忙碌是常態，不是例外。**
- **B1**：durable、退避（`record.go:170`，5 秒起、上限 300 秒）、dead letter（8 次）都有。但有兩個地方不一樣：
  - ACK 比對的是 notice id，不是 revision CAS。防止被覆寫靠的是 `writeMu`，所以也有 #10 的跨行程問題。
  - 一直停在選單上的 root 會一直被延後，而延後不計次（`notice.go:198-202`），所以**永遠不會進 dead letter**。

### #24 `result.json` preflight＋兩次觀察的復原 — **應改：拆成三件事**

1. **格式驗證：維持。** 在 `5fb90f86` 之前，格式錯的收據會被靜默忽略。
2. **把 8,676 bytes 的 base64 node 程式塞進每一份簡報：應改。**
   - 本 task 的 `CHILD.md` 共 42,190 B：validator 佔 8,676 B（20.6%），「Reporting」一節佔 13,256 B（31%）。
   - 塞進簡報的理由（`OrchestratorChildBrief.swift:56-57`）是讓任何 repo 裡的 child 都能跑。
     但 Go 版是**一個同時是 daemon 與 CLI 的二進位**（`plan.md` §2），這台機器上一定有它。
   - 從零開始：`<絕對路徑>/clawdline task finish <task_dir>`，做同樣的驗證＋原子 rename。
     這樣也拿掉了一個相依：**child 的環境裡必須有 `node`**。在 Linux／Windows 這不保證（未量）。
   - B1 原樣嵌入了同一支 JS（`brief.go:20-21`、`:203-204`）。
3. **30 秒兩次觀察的窗口：應廢除。**
   - 找不到書面理由（§2.3）。marker 已經綁了被驗證那份 bytes 的 SHA-256，broker 也驗 secret。
   - B1 自己的註解把道理說完了：「Age is not consent, which is why the marker carries a hash and not a timestamp」（`watch.go:134-135`）。
   - 用量：`orchestrator.result.recovered` **0 筆**。（`orchestrator.result` 另有 8 筆，全部是 `bad_secret` 的拒絕，跟救回無關。）
   - B1 已經不做這個窗口（`watch.go:136-137`），但 `result-preflight.js:119-120` 的註解仍然說 broker 會等。
     `docs/broker.md` 也把它列成「還沒做」。**兩處文字都要改成「刻意不做」。**
- **改的代價**：小。一個 CLI 子命令＋簡報改兩段。
- **不改的代價**：每一個 child 多讀 8.7 KB；沒有 node 的機器上，child 沒辦法照簡報完成。
- **B1 的小缺口**：收養用的是 `os.Rename`（`taskdir/broker.go:213`），會覆蓋既有的檔，不是 create-if-absent。

### #26 linger deadline 持久化 — **維持照搬（必要；#14 的兩條規則其實屬於這裡）**

- `3a7adb8e`：「Seventeen of the eighteen tabs left standing had a restart inside their three minutes」。
  這是「deadline 要落盤」的通則，Go 的 `time.AfterFunc` 一樣會犯。舊 store 裡有 51 筆 task 帶著 `close_at`。
- **B1**：沒有 linger（`orchestrator_child_linger` 只出現在設定鍵表）。加上 #19，**B1 的 child tmux session 目前永遠不會被關掉**。
- `restartGrace = 20` 與「沒有任何終端機的讀數不能拿來判」這兩條，應該從 #14 移到這一項。

### #27 判定執行者遺失需要兩次觀察 — **維持照搬（必要）**

- 觀察有雜訊：audit 裡 `identity.mismatch` 5、`identity.agreement` 840、`identity.foreign` 988。
  單次讀數就判死，代價是有人的工作被中斷（C3）。
- **B1**：沒有 `executor_missing` 的概念。最接近的是 `spawnVerdict`，它只看一次（見 #14）。

### #28 清掃前確認擁有者是否還活著；讀不到就保留 — **維持照搬（必要）**

- `docs/orchestrator.md:2506`：2026-09-11 有三個已結束的 task，行程在結束後 7–21 小時仍在跑（`7f70d979`）。
- **B1**：沒有清掃。這條要在 B4／B8 寫清掃時一起寫進去。

### #29 清掃不掛在讀取路徑；每輪有上限、輪流推進 — **維持照搬（我原本以為是 JSON 的產物，查證後不是）**

- 每輪 64 筆的輪轉游標掃的是**檔案系統**：owned scratch root 的目錄列表（`OwnedStorage.swift:858-886`）。
  上限是為了控制 `lsof` 的次數（`5a65bfdb`：「at most 1 + ⌈64 ÷ 16⌉ `lsof` runs a pass」）；
  輪轉是為了讓一個一直刪不掉的項目，不要每一輪都排在第一個。這兩個理由在 Go 版一樣成立。
- 「不掛在讀取路徑」也必要，而且 B1 的讀取路徑已經有同一類的成本：
  - inventory 每次讀全表，每個已結束的 worktree task 最多跑 4 個 git（`inventory.go:235-245`）。
  - 每次派工都跑一次完整的 inventory（`dispatch.go:299`）。

  在 773 筆的規模沒問題；**到幾千筆時就是 `af83249d` 的形狀（推論，未量）**。

### #30 landing 可修改：同一道閘門＋`corrected_from` — **維持照搬（必要；B1 已經重現了它要防的事故）**

- **用量**：`orchestrator.landing.corrected` 18 次（09-07 → 09-12）。不常用，但出事的代價是永久的：
  「a record on this machine came to name another task's commit permanently」（`Orchestrator.swift:5335-5350`）。
- **B1**（`lifecycle.go:189-242`）：同樣的狀態、同樣的 target、不同的 commit，重送時會通過 `proveLanding`，然後**直接被蓋掉**
  （`now.Landing = next`）。舊的 commit 沒有保留，event 也只記 `{state, task}`（`broker.go:198`）。**這就是 G2。**
- **改的代價**：小（`corrected_from`＋audit；在 SQLite 裡就是多一筆 event）。**不改的代價**：G2 會再發生一次。

### #31 landing 兩條 arm，在型別上不能互相冒充 — **應改：Arm 1 維持並補上綁定；Arm 2 廢除**

- **Arm 1 必要，但 B1 的版本少了一半。** `proveLanding`（`lifecycle.go:255-275`）只問「這個 commit 是不是 target 的祖先」，
  **不問它是不是這個 task 交付的東西**。`Worktree.Base` 本身就是 target 的祖先，拿它來報 landed 會通過。
  舊版的 Arm 1 要先有「完整乾淨的 worktree 收據」，才去問 ancestry。
  `cutover.md` A5 的驗法是「帶一個**不是** branch 祖先的 commit 會被拒」，**這個驗法測不出這個洞**。
- **Arm 2**（共用 checkout 的寫入集合，相隔 5 分鐘兩次乾淨）：
  - audit 裡 sweep 關閉的紀錄：ancestry 11 筆，write_set **1 筆**（09-10）。`orchestrator.landing.sweep` 最後一次出現是 09-11。
  - 共用 checkout 而且有 claims 的 143 筆 task 裡，98 筆 landed，**全部帶驗證欄位**（`verification_origin: local_target_branch`）。
    也就是 root 報 commit、用 ancestry 證明：**共用 checkout 的 task 實際上也走 Arm 1。**
- **從零開始**：Arm 2 是「用兩個瞬間的乾淨讀數，寫一筆永久紀錄」，`broker-design.md` 自己也評為中風險。
  沒有 Arm 2，共用 checkout 的 task 就由 root 報 commit（Arm 1），或走 `nothing_to_land`（B1 已經有 `nothingToLandRefusal`）。
- **改的代價**：Arm 1 的綁定是小；Arm 2 是 0（不做）。
- **不改的代價**：Arm 1 的洞會讓 A5 驗收通過一個可以被冒充的證明；Arm 2 則是用一整套穩定窗口，換一個月一次的自動關閉。

### #32 只拿紀錄上的 ref 比對 — **維持照搬（必要；B1 有兩處沒照做）**

- `docs/landing.md:432-439`：「Which branch a record should have named is a person's decision」。
- **B1**：
  - pending landing 開出來時沒有 target（`dispatch.go:596`），所以第一次 landed 用的 target，是呼叫端當下說的。
  - inventory 判斷 merged 用的是主 repo 的 `HEAD`（`inventory.go:239`），不是 target。

  **兩處都要改成紀錄上的 target。**

### #34 落地佇列不建表 — **應廢除：「不建表」是對的，但整個佇列的形狀都不該搬**

- **用量**：
  - `/advance` 在 audit 裡只有 **1 筆**（09-12 01:20，`delivery_failed`），**成功送達 0 次**。
  - `landing-queue.json` 的 `queues` 是 `{}`，從來沒存過 `notified_digest`。
  - `backlog.yaml:1678-1709` 自己寫著「Not measured: whether this route has ever delivered」。**現在量到了：沒有。**
  - O2 的 bug 在舊 HEAD 仍然在：`OrchestratorLandingQueue.swift:274` 用 conversation id 命名，`RemoteServer.swift:2403-2407` 用 terminal id 查。
- **它要防的問題存在嗎**：存在，但集中在要退役的那個 repo。
  - `~/code/clawdline`：20 天內 64 個 root，有 66 次 landed 發生在另一個 root 的 landed 之後 30 分鐘內。
  - `~/code/clawdline-go`：目前 1 個 root，0 次。
- **從零開始**：H1 的不變量是「一個 checkout 同一時間只有一個人在落地」。這是一把**租約**，跟編譯插槽（J、`cutover.md` A10）是同一個概念。
  用同一個租約原語（還活著的證明＋逾時＋讀不到就不放行，#41），不要另外建一條帶 slot notice、order、turn 的佇列。
- H4（`claims: []` 讀起來像一個承諾）的不變量要留，但換一種拼法，見 §6.4。
- **改的代價**：0（B6 還沒做）。
- **不改的代價**：搬一個從沒成功送達過的功能，連同它在 09-12 補的三個排序修正（`ed9c6152`、`ede95dc8`、`1e385141`）。

### #41 等待者的時鐘掛在「還活著的證明」上 — **維持照搬（必要）**

- 證據：`b2f25048`（32 個 `queue_full`）、`24a33139`（一次 Ctrl-C 就讓編譯插槽變成永久路障）、`15924b14`。
  家規也記著：四個 `swift-frontend` 同時跑，讓這台 Mac 重開機過。
- **B1**：還沒有等待者。這條會成為 #34 改寫之後的共用原語。

### #47 inflight／inventory 以 repository 的 common-dir 為鍵 — **維持照搬（必要）**

- 773 筆裡有 374 筆是 worktree 隔離。本 task 就是在 worktree 裡用 `/inflight` 查到另外三筆在跑的工作。
- **B1**：鍵是從 common-dir 推出來的主 worktree 路徑（`git/worktree.go:29-43`），比對用前綴（`inventory.go:182`），
  所以巢狀在 repo 路徑底下的另一個 repo 也會被算進來。這是一個小缺口。

---

## 4. 反方向：照搬是對的，但 B1 沒有搬

這一節回答另一半問題：**這些項目是必要，不是歷史，證據在哪？** 最硬的證據是：B1 沒有搬它們，然後在那些地方重現了舊版的事故形狀。

| 照搬項 | 舊版的事故 | B1 今天的樣子（讀程式得出，**未重現**） |
|---|---|---|
| #30 landing 可修改 | G2：一筆紀錄永久指向另一個 task 的 commit，而且更正的人被告知成功 | 同狀態同 target 的重送直接覆寫 commit，不留舊值（`lifecycle.go:242`） |
| #31 Arm 1 | G1：landing 要有完整的 worktree 收據才問 ancestry | 任何 target 的祖先都能 landed，包括 `Worktree.Base`（`lifecycle.go:255-275`） |
| H4（在 #34 裡） | 「一個寫了 26 個檔的交付，讀起來跟一個什麼都沒寫的 review 一模一樣」 | worktree task 的 claims 被清空（`dispatch.go:191-200`），而且沒有存到任何地方。註解（`draft.go:212-213`、`dispatch.go:591-594`）與 schema（`orchestrator.schema.json:170`）都說保留成 landing write set，程式沒有 |
| #14／F5 讀不到≠不在 | F5：「a list that was never read prints exactly what a list of nothing-to-keep prints」 | 不完整的盤點 → `alive=false` → `spawn_failed`（`orchestrator_wiring.go:31`、`watch.go:219-230`） |
| #13 一個終端機一個寫入者 | `23ae603a`：完成通知繞過 lane 直接打字 | 沒有 lane；三個呼叫端可以同時打進同一個終端機 |
| #9 store 壞掉≠空 | A3 | 解不開的列在 beat 裡消失，同 id 的派工會把它蓋掉（`broker.go:168-173`、`dispatch.go:72`） |
| #19、#26 | `3e37e8ec`、`3a7adb8e` | child 的 tmux session 永遠不關 |

**這張表的意思是：本文的「應改／應廢除」不是在說「舊版的東西都可以丟」。**
舊版每一次事故換來的是一個**不變量**；要丟的是它當年為了守住那個不變量而長出來的**形狀**。B1 目前的問題，大多是連不變量一起丟了。

---

## 5. 看板與時間軸裡標照搬的項目

### 5.1 看板（`board-design.md`，照搬 27 項）

分歧集中在一條鏈上：**C1 → C3 → C4 → C5 → C6，再加上 D5 與 B1**。其他照搬項大多站得住。

#### C1「每次寫入重寫整份文件」照搬 — **應改：它的理由搬不到 Go**

看板文件的理由是 `b6f4f555` 的量測：一次 persist 42 ms，93% 是 encode；把 receipts 與 evidence 拆成分段檔只省 16 ms。
**但那個 commit 比較的對象不是 SQLite。** 原文：

> So moving receipts (685 KB) and evidence (942 KB) into segment files saves about 16 ms — **and it needs a journal**,
> because a receipt and the state change it acknowledges have to commit together or the idempotency guard lies in one direction or the other.
> Not worth it, and the reason is a number rather than a preference.

它否決的是「自己手刻一個 journal 來拆檔」。在 Go 版，journal 已經有了。

- `broker-design.md` §6.1 把 broker 搬進 SQLite 的理由（事件、投影、收據同一個交易；跨行程安全；只有一個 store），逐條套到看板上都成立。
- 而且看板比 broker 更需要，因為**看板是由 broker 餵的**：`Orchestrator.swift:2926/3097/4000/6429/8082` 呼叫 `observe(...)`，
  `observeRootLanding` 的回傳值還直接進 landing 回應的 body。broker 在 SQLite、看板在一份 JSON，
  等於把 `broker-design.md` §2.3 要消滅的「派一次工碰到五個檔、沒有共同交易」原樣留了一格。
- **兩份文件在這裡直接矛盾。** `broker-design.md` §6.9 說看板與時間軸「應該是 broker 事件流的投影，放在同一個 DB 裡」；
  `board-design.md` C1 說「照搬整份重寫」。兩份文件都沒有提到對方。**本文站 §6.9 那邊。**
- 還有一個反例：舊 repo 自己在 09-04 說「**不要用效能當理由**」（`mac-app-shell.md:593`，`broker-design.md` §3 引用並同意）。
  C1 用「42 ms 不是瓶頸」當作不搬的理由，一樣是在拿效能當理由，只是方向相反。

| | |
|---|---|
| 結論 | **應改**：看板項目寫入（Go 版目前刻意沒實作，`plan.md` §4）要實作時，直接寫進同一個 SQLite，不寫 `project-board.json` |
| 改的代價 | 小。項目寫入本來就還沒做，要改的是 `board-design.md` 的 C1–C6 與 `plan.md` §4 的一句話 |
| 不改的代價 | 看板與 broker 之間沒有交易邊界；C3–C6 四條規則要跟著搬，而它們全是在補「兩個檔不能一起提交」 |

#### C3–C6：全是「JSON 檔」的後果

| # | 照搬的規則 | 在 SQLite 裡 | 結論 |
|---|---|---|---|
| C3 | 收據放在同一份文件，為了跟狀態一起原子落盤 | 收據是同一個交易裡的另一張表：目的達成，形狀消失 | **應改**（目的照搬，形狀不搬） |
| C4 | 每個項目一個 append-only `*.jsonl`，**先寫 log 再寫聚合**（「多一行救得回，少一行救不回」） | 歷史是一張表，跟聚合在同一個交易；沒有先後可言 | **應改**：「歷史不放在聚合裡」照搬；「先 log 後聚合」這個順序規則廢除 |
| C5 | 項目 id 當檔名，非 UUID 一律拒絕、不消毒 | 沒有檔名 | **應廢除**（前提消失）；「id 必須是 UUID」這條驗證可以留 |
| C6 | log 寫失敗不讓指令失敗，只計 `historyDroppedCount` | 同一個交易：歷史寫不進去，整個指令就沒有發生 | **應廢除**。「指令成功但歷史少一行」這種狀態在新設計裡不存在，也不該存在 |

#### D5「讀 4／寫 8 兩條獨立有界佇列」照搬 — **應改（不要搬）**

- 它存在的理由是 Swift 的 HTTP 只有一條序列佇列：`RemoteServer.swift:99` 的 `…clawdline.remote`；`:3773-3774`
  「Everything else here is read, decided and answered on one serial queue」。
  看板需要自己的佇列，才能「neither lane occupies message/SSE admission」（`ProjectBoardRequestCoordinator.swift:3-9`）。
- `docs/architecture.md:250-255` 的「That design is sound and should be kept」，是在那條序列佇列的前提下說的。
  同一份文件 `:271-277` 說：「The difference is not concurrency, it is **when the work happened**」。
  它肯定的是物化（D1），不是佇列。
- 4 與 8 這兩個數字找不到理由（`17f444b3` 的 body 是空的）。
- Go 的 `net/http` 每個請求一個 goroutine，看板讀的是不可變快照（`board-design.md` 自己在 D2 就這樣說）。
  **Go 版的看板今天沒有這兩條佇列**（`internal/adapters/board` 只有大小上限），而且不需要。
  寫入的背壓在 SQLite 的單一寫入者那裡；讀取如果要限流，應該是全域 HTTP 的一個上限，不是每個功能各做一套（§6.5）。
- **改的代價**：0。**不改的代價**：新增一套只在 Swift 有意義的 admission。

#### B1「`progress` 狀態機」照搬 — **維持規則，應改位置與輸入**

- 規則本身是產品判斷（`delivered ≠ reviewed ≠ landed`），照搬是對的。
- 但 Go 版把它放在 `internal/adapters/board/progress.go`（1,044 行），輸入是舊 app 的 `StoredItem` 形狀；
  它判斷「landed」看的是存在看板項目裡的 evidence（`progress.go:729-730`「Broker ancestry confirms the latest observed delivery landed」）。
  這是 broker 落地事實的**第二份拷貝**，而且綁在一個要退役的 app 的檔案格式上（見 §6.3）。
- **應改**：搬到 `internal/domain`，輸入改成 broker 的事實。這也是 `cutover.md` 判準 B1（新 daemon 不再讀 `~/.config/clawdline`）
  在看板這一塊的前提。

#### 其他照搬項

| # | 項目 | 結論 | 一句理由 |
|---|---|---|---|
| A1 | 19 個欄位的 envelope | **維持**，但標記 8 個欄位退役時刪 | 舊前端只讀 11 個（`board.js:1854-1926`、`board-settings.js:13-49`）。沒人讀的 8 個是 `mode`、`entitlement`、頂層 `updatedAt`、`available`、`snapshotBudgetBytes`、`automaticMutation`、`sourceIngestion`、`responsibilitySource`。現在留著成本是 0；舊 console 不再當 oracle 的那天刪掉 |
| A2 | 卡片允許清單＋「用吵的失敗換安靜的失敗」 | 維持 | 量出來的：每次 62,801 B 沒有讀者 |
| A3–A4 | `mode`、`viewer` | 維持 | 純函數 |
| A5 | 指令存了但投影太大時，帶 `commandApplied: true` | 維持 | 誠實的部分成功 |
| A6 | `truncated` 必須描述這一份清單 | 維持 | A8 做完之後自然消失 |
| B2 | `listSummary` 分組 | 維持 | 產品規則 |
| B3 | `entitlement` 常數 | **維持，但它是純粹的 wire 殘留** | 舊前端**沒有任何一行**讀 `board.entitlement`，只有 `net/board-mock.js:86` 寫入它；付費判斷在 `/v1/entitlements`。併入 A1 的退役清單 |
| B4 | `responsibility` 是請求時的資料，永不落盤 | 維持 | 跟 broker 的「觀察不落盤」是同一條規則，而且看板比 broker 早想到 |
| B5 | 狀態由證據推導，沒有手動選單 | 維持 | 產品規則 |
| B6 | `narrativeConsent` | 維持 | 欄位與開關；外送延後 |
| C9 | 唯讀舊 app 的 776 張卡 | 維持（過渡） | `plan.md` §4 的使用者決定；但它是 B1 progress 綁在舊形狀上的原因 |
| D1 | 物化讀取模型 | 維持 | 21–42 ms 是它換來的 |
| D3 | 「新 revision 不配舊 bytes」這個承諾 | 維持 | 正確性 |
| D4 | `expectedRevision` CAS＋`(actor, requestId)` 收據 | **語意維持，拼法應改** | 見 §6.2。另外，Go 版重播時回的是「目前的」revision，不是收據上存的那一個（`adapters/board/settings.go:163`） |
| D7 | 持看板鎖時不回呼 broker | 維持（併入 broker #10 的改寫） | 在同一個 DB 裡，它變成「交易裡不呼叫別的擁有者」 |
| E1 | `board.js` 逐位元組照抄 | 維持 | 使用者定的復刻做法 |
| E2 | 抽屜的「專案 · 看板」永遠隱藏 | 維持（前端 1:1）；**但這是一個永遠不會出現的元素** | 舊版 `BoardControls.apply()` 無條件 `hidden = true`。看板文件自己也寫了「可能不是使用者要的」 |
| E3–E4 | 設定頁區塊、輪詢規則 | 維持 | — |

### 5.2 時間軸（`timeline-design.md`，照搬 14 項）

**這一份最容易判斷，因為它的資料替它回答了。** 當場量到（唯讀）：

- `project-timeline.json` 的 2,000 筆 event 全部來自兩個**內部**生產者：`local_git_history` 1,973、`orchestrator_landing` 27。
- `requestReceipts` 是 **0 筆**。
- `ingest` 在整個舊 repo 只出現在 handler 本身（`ProjectTimelineStore.swift:173`、`ProjectTimelineHTTP.swift:95`），沒有任何呼叫者。
- 檔案停在 2026-09-17 02:06:58。
- **Go 版沒有任何時間軸程式**（`grep -rli timeline internal/` 只命中 cloudops 的操作名稱）。

| # | 項目 | 結論 | 證據 | 改的代價 | 不改的代價 |
|---|---|---|---|---|---|
| A1 | 「commit／build／task 都不代表上線」 | 維持 | 這是功能存在的理由 | — | — |
| A2 | 12 種狀態詞彙 | **應改：延後** | 2,000 筆只有兩種 event；reconciler 的五個部署分支從沒被走過（`timeline-design.md` §7） | 0（不做） | 搬 10 個沒有生產者、從沒被觀察過的狀態 |
| A3 | 封閉的查詢參數＋具型別的拒絕 | 維持 | 房規的形狀 | — | — |
| A4 | `capacity{}` 公開在回應裡 | 維持 | 容量要看得見 | — | — |
| C1 | `(producer, sourceID)`＋決定性 digest | **應廢除（目前）** | 兩個生產者都在 daemon 裡；`584783ca` 那個牆鐘 bug 出在 git 匯入器自己身上。內部匯入器只需要 `(project, sha)` 當主鍵 | 0 | 為一個沒有呼叫者的公開 API 搬一套衝突偵測 |
| C2 | 批次內重複身分檢查 | **應廢除（目前）** | 同上；它守的是 `POST ingest` 的批次 | 0 | 同上 |
| C3 | entry 先寫、checkpoint 後寫 | **應改** | 同一個交易就沒有先後；它守的是「兩次 persist 之間當掉」 | 0 | — |
| C4 | 先解析出完整 SHA 再交給 git | 維持 | 有測試守著的 checkout 同時切換的競爭（`:496-509`），跟儲存無關 | — | — |
| C5 | `expectedRevision` CAS＋requestId 重放 | **應改：併入統一收據** | `requestReceipts` 0 筆；唯一的寫入是 `set_enabled` 一個布林值 | 小 | 多一種冪等拼法 |
| C6 | Cloud 重連時保留 pending requestId | 維持，但**它屬於 Cloud 指令帳本，不屬於時間軸** | `d6911bd1` 修的是 client 端 | — | — |
| C7 | 讀 4／寫 8 有界佇列 | **應改（不要搬）** | 同看板 D5 | 0 | 同看板 D5 |
| D1 | git 匯入唯讀、有界 | 維持 | 界線正確 | — | — |
| E1 | `timeline.js` 照抄 | 維持（等 A6／D3 拍板） | `timeline-design.md` §6 第 4 題已經建議這一波不做 | — | — |
| E3 | 容量文案 | 維持 | — | — | — |

---

## 6. 跨文件：同一件事的多種拼法

這一節是使用者那句「看起來是 legacy 產物」最直接的答案。**每一種多出來的拼法，都是某一次事故的修補留下的，而沒有人回頭把舊的那一種拿掉。**

### 6.1 「完成」有三條路

| 路 | 舊版用量（31 天） | B1 |
|---|---|---|
| beat 收 `result.json` | 716 筆 success／failure 裡 664 筆有 `result_verified_at` | `watch.go:130-164` |
| 收養 `result.json.ready` | 救回成功 0 次 | `watch.go:132-147` |
| `POST /tasks/:id/complete`（帶 status＋summary） | **量不到**：成功的呼叫舊版不寫 audit（`Orchestrator.swift:4820` 只記 secret 錯誤）。代理量是上面剩下的 52 筆，這是上限，還包含欄位出現之前的舊列 | `lifecycle.go:68-81` |

**B1 在這裡會掉資料。** `/complete` 通常比 5 秒一次的 beat 先到；`Settle` 用 summary 合成一個 Result（`dispatch.go:583-589`），
之後 beat 看到紀錄已經是終態就跳過（`watch.go:67-69`）。於是 `result.json` 裡的 `symbols`、`artifacts`、`verification`、
**`review`（review 節點的型別化裁決）** 都不會進紀錄。舊版在 HTTP 先到時，會用 `result.json` 補上空的欄位（`Orchestrator.swift:8048-8058`）；B1 沒有。
B1 的簡報仍然在教 child 用它（`brief.go:220-229`）。`5feea4b` 修的「兩條路各結算一次」，是同一個根的另一個症狀。

**從零開始**：`result.json` 是唯一的完成來源。`/complete` 要嘛刪掉，要嘛降成不帶內容的「現在就來收」。

### 6.2 冪等至少有九種活的拼法，外加兩種死碼

B1／新版目前的冪等與收據機制（子代理 B 列出；#1、#3、#4 的行號我自己重讀過）：

| # | 機制 | 存了什麼 | 會重播嗎 |
|---|---|---|---|
| 1 | 派工以 task_id 去重（`dispatch.go:72-73`） | 不另存；回「目前的」record | 回目前狀態；**在驗 secret 之前**，而且檢查與寫入不是原子的（讀程式推論，未重現） |
| 2 | messages 的 `Idempotency-Key`（`orchestrator.go:366-369`） | **什麼都不存** | 不會：只檢查有沒有帶 |
| 3 | notice id（`dispatch.go:598-605`） | 在 record 裡 | ACK 比對 |
| 4 | Settle 只一次（`dispatch.go:575-577`） | — | 拒絕，不重播 |
| 5 | progress note `UNIQUE(task_id, note)`（`store/broker.go:49`） | 句子本身 | 只看存不存在 |
| 6 | 通用的 `store.Commit` 收據表（`sqlite.go:62-66`、`:254-257`） | at／seq | 不含回應內容 |
| 7 | 看板設定 `(actor, requestId)`＋digest（`adapters/board/settings.go:157-168`） | 存在 `project-board.json` | 回目前 revision |
| 8 | start／resume／voice 的 `Idempotency-Key`（`start.go:45-63`、`voice.go:94-120`） | **記憶體，10 分鐘** | 會；重啟就全部遺失 |
| 9 | Cloud replay window（`domain/cloud/replay.go:17`） | `(sender, seq)` | 只看存不存在 |
| 死碼 | `domain/board.Decide`＋`board_commands`（`sqlite.go:118-123`、`:393-444`） | — | 沒有非測試的呼叫者 |
| 死碼 | Cloud command ledger（`domain/cloud/ledger.go:210-281`） | 記憶體 24 小時 | `NewLedger` 沒有非測試的呼叫者 |

`plan.md` §1 第 7 條說「收據是持久且型別化的」。上面九種裡，#2 是假的，#8 不持久，其他七種各有各的形狀。
舊版的看板收據（`(actor, requestId)`）與時間軸收據（`(producer, sourceID)`＋digest）再加上去，就是十一種。

**從零開始**：一張收據表，`(scope, actor, key) → (request digest, response, at)`，跟它確認的狀態變更寫在同一個交易裡。
每個功能只宣告自己的 scope。看板 D4、時間軸 C5、messages、start／resume／voice 全部用它。

### 6.3 「落地了沒」有三份答案

舊版自己量過這件事的代價。`e924dd9a`（09-06）：
> The Projects page called 53 of this repository's worktrees "delivered, not landed" while git said 24 of those branches were
> already ancestors of HEAD and 13 no longer existed. … and the landing queue, which does ask git, said 17 the same night.

今天的三份拷貝：

1. broker 的 landing 紀錄。
2. 看板項目裡的 evidence：`trusted_evidence_recorded` 194 筆；Go 版在 `progress.go:729-730` 讀它。
3. 時間軸的 `landed_to_git` 27 筆。這一份**停在 09-17 02:06**，之後的 landing 全都不在裡面。三份拷貝已經在分岔。

**從零開始**：broker 的 landing 紀錄是唯一的事實。看板的 progress 與時間軸都是這個事實的純函數，
放在 `internal/domain`，讀同一個 DB。這就是 `broker-design.md` §6.9 的方向；本文只是補上「為什麼現在就要決定」：
Go 版的看板已經寫了 1,044 行推導，而且綁在舊形狀上。

### 6.4 「這個 task 寫哪裡」散在四個欄位、兩個檔

- 舊 store 的欄位：`claims`（738 筆）、`claim_keys`（143）、`untouched_claims`（62）、`claims_declared`，
  以及另一個檔 `landing-queue.json` 的 `landing_paths`（252）。
- 本 task 在 `/inflight` 裡看到的樣子：`"claims": []`、`"landing_paths": [...]`、`"claims_declared": true`，三個一起出現。
- 分成兩份的原因是 `OrchestratorDraft.swift:1766-1767`：隔離的 task 要把 claims 清空，才不會擋住共用樹上的工作。
- **B1 只剩一個欄位，清空之後什麼都沒留**（§4）。

**從零開始**：一個不可變的 `declared_writes`，加上一個 `lease_scope`（`shared` 或 `worktree`）。**宣告過的事實永遠不清空**；
「擋不擋別人」是 `lease_scope` 的函數，不是把資料刪掉來表達。

### 6.5 有界佇列、lane、租約：每個功能各一套

| 舊版 | 新版 |
|---|---|
| 終端機 lane（全機 8、每終端 2）、看板讀 4／寫 8、時間軸讀 4／寫 8、落地槽、編譯插槽、landing sweep 自己的佇列 | 開 session 1＋4、pasteboard 1＋4、worktree lifecycle 4、`codex app-server` 2、notice 每輪 8、SQLite 單連線 |

每一套都有自己的上限、自己的拒絕碼、自己的「滿了怎麼辦」。**從零開始只需要兩個原語**：

- **有界的 per-key lane**：同一個資源序列化，全機一個 admission 上限。終端機、git 操作都是它。
- **租約**：一個資源同時一個持有者，時鐘掛在還活著的證明上（#41）。編譯插槽、落地（#34）都是它。

### 6.6 wire 上為了複製來的畫面保留的第二種拼法

`api/v1/tasks.schema.json:70` 自己寫著：camelCase 那組是舊版的清單投影，給複製來的 console 讀；
`task_id`、`project_dir`、`created_at`、`claims` 這四個 snake_case 是「this daemon's older names, kept beside them because the Dashboard panel still reads them」。
**同一個欄位兩種拼法，只因為兩個前端各讀一種。** 成本很低，但它就是「以畫面為由保留的後端形狀」。
Dashboard 改讀 camelCase 之後應該刪掉。

---

## 7. 跟既有文件的分歧（不軟化）

| 文件 | 它說 | 本文說 | 誰的證據比較新 |
|---|---|---|---|
| 派工說明 | 照搬 25、小改 12、重新設計 11 | 表格是 24／12／11，合計 47 | `broker-design.md` §7 本身 |
| `broker-design.md` #10、§6.3 | 持鎖不做 I/O | 字面會讓 B1 正確的 `mutate` 違規；改寫成「持寫入權時不做**外部** I/O」，並讓交易取代 `writeMu` | `broker.go:234-246`、`5feea4b` |
| `broker-design.md` #13 | 一條有界 lane，照搬 | 一個終端機一條；全機只做 admission | `RemoteServer.swift:235-237`、`Tmux.swift:664-665`、child 自 9/07 起 100% tmux |
| `broker-design.md` §2.5、#14 | `restartGrace = 20` 是判逾時前的等待 | 它只用在 linger | `Orchestrator.swift:6555-6590` |
| `broker-design.md` #16、#18 | 照搬 `72ed3e46` 的繞道與重打五次 | 廢除：B1 自己在 08:29 推翻了；31 天各觸發 2 次與 1 次 | `eefa318`、audit |
| `broker-design.md` #20 | `cutover.md` 漏掉 respawn，要補 | 31 天用 4 次、上限沒碰過；降級併入派工 | audit、registry |
| `broker-design.md` #24；`docs/broker.md`「還沒做」 | 30 秒兩次觀察要照搬／是待辦 | 廢除：沒有書面理由、救回成功 0 次，B1 自己的註解已經說明為什麼不需要 | `watch.go:134-135`、audit |
| `broker-design.md` #31 | 兩條 arm 整條照搬 | Arm 2 廢除（關閉 1 次）；Arm 1 要補「綁定交付」 | audit、`lifecycle.go:255-275` |
| `broker-design.md` #34 | 照搬 H1–H4，不建表 | 整個佇列形狀不搬：成功送達 0 次。改用租約 | audit、`landing-queue.json` |
| `board-design.md` C1 | 整份重寫照搬，因為 42 ms 不是瓶頸 | 應改：`b6f4f555` 比的是手刻 journal；`broker-design.md` §6.9 要的是同一個 DB | `b6f4f555` 原文 |
| `board-design.md` C3–C6 | 照搬 | 全是 JSON 檔的後果；C5、C6 廢除 | — |
| `board-design.md` D5 | 讀 4／寫 8 照搬，「sound and should be kept」 | 那句話的前提是 Swift 的單一序列 HTTP 佇列；Go 版今天也沒做 | `RemoteServer.swift:3773-3774`、`architecture.md:271-277` |
| `board-design.md` B1 | progress 狀態機照搬 | 規則照搬；位置（adapters）與輸入（舊 `StoredItem`）應改 | `progress.go` |
| `timeline-design.md` C1、C2、C5 | 照搬 | 為沒有外部生產者、0 筆 request 收據的 API 搬冪等機器；廢除或併入 | 當場量的 `project-timeline.json` |
| `mac-app-shell.md:535`（`broker-design.md` #8 引用） | `config.json` 因為人會用 vi 改 | 結論對，理由過時：選單自 08-18 起開 GUI | `d33535fe` |

---

## 8. 如果只做三件事

排序的依據是：（能防止的損害 × 現在就會碰到的機率）÷ 代價，再考慮「越晚越貴」。

### 第一：簡報送達改用具型別的 `accepted` 收據

- **涵蓋**：廢除 #16；廢除 #18 的重打；#17 從代理訊號換成依據；把 #14「讀不到≠不在」補進 `spawnVerdict`。
- **為什麼排第一**：
  - 它是 B1 自己承認最弱的一環：`watch.go:246-252`「it is weaker than the Swift app's … It can be fooled by a child that was already working on something else」。
  - 它也是舊版唯一一類「刪掉活人工作」的事故（D1）的入口。
  - 它便宜，而且收據鏈（`plan.md` §1 第 7 條）的第一格今天是空的。
- **做法**：child 的第一個動作從「逐字說一句話」改成一個用 secret 簽的 `accepted`（沒有 loopback 就寫檔，跟 `progress.json` 一樣）。
  `spawnVerdict` 的輸入從「活著／選單」變成「收據／活著／選單／盤點完整度」，只有在盤點完整而且系統正面回答分頁不在時，才判 `spawn_failed`。
- **代價**：小。**不做的代價**：B1 的 `briefed` 靠「分頁開始跑一個 turn」，`spawn_failed` 靠一次可能不完整的盤點。

### 第二：完成只有一條路，收據只有一張表

- **涵蓋**：§6.1、§6.2；看板 D4、時間軸 C5、messages 那個假的 `Idempotency-Key`。
- **為什麼排第二**：B1 今天就會掉資料（review 的裁決，§6.1）。而且在跑的 broker B2＋B3 那個 task 正在做「notice 放進獨立的表」，
  如果收據表不先定，下一波就多一種拼法。
- **代價**：中。**不做的代價**：每一波都多一種冪等，每一種都要各自被測、各自出錯。

### 第三：落地只有一份事實

- **涵蓋**：
  - #31：Arm 1 綁定交付的 head；Arm 2 不搬。
  - #34：舊佇列不搬，改用租約。
  - #30：`corrected_from`。
  - §6.4：claims 不清空。
  - 看板 C1–C6 與 B1 progress 改成同一個 DB 裡的投影。
  - 時間軸 C1／C2／C5 不搬。
- **為什麼排第三**：損害目前還是潛在的，因為 B5／B6 還沒做。但它是資料模型的決定，越晚越貴。
  而且 B1 的 `proveLanding` 有個洞，會讓 `cutover.md` A5 的驗收誤判通過。
- **代價**：大部分是「不做」，所以是 0；Arm 1 綁定、`corrected_from`、claims 保留加起來是小。
- **不做的代價**：看板第一個項目寫入落地的那天，Go 版就有兩份會分岔的落地事實。

**第四、第五名（便宜到應該順手做）**：#13 的 per-terminal lane（B1 現在可能交錯打字）；#10 的規則改寫，以及用交易取代 `writeMu`。

---

## 9. 這些改動會不會破壞已經落地的 B1

**結論：不會。** 沒有一項需要退回 B1；B1 的 7 個測試沒有一個要改斷言，只有一個要多一個輸入欄。兩項會改變 B1 的行為，而且都是變嚴。

B1 的 7 個測試：`TestAnAckThatLandsMidTypingIsNotUndone`、`TestATaskIsSettledOnce`、`TestAProvenBriefingSurvivesTheDispatchThatWasStillTyping`、
`TestADialogIsNeverTypedInto`、`TestAFramedCaretIsAComposer`、`TestAStartingSessionIsNeitherReadyNorChoosing`、`TestSilenceFromALiveChildIsNotASpawnFailure`。

| 改動 | 碰到的 B1 檔案 | 行為 | 測試 |
|---|---|---|---|
| ① `accepted` 收據 | `brief.go`、`watch.go`、`lifecycle.go`、HTTP 路由 | 新增；progress 仍可升 `briefed` | 不受影響 |
| ① `spawnVerdict` 讀不到≠不在 | `watch.go`、`orchestrator_wiring.go:31` | **變嚴**：盤點不完整時不判 | `TestSilence…` 的表格多一欄，斷言不變 |
| ② `/complete` 不再結算 | `lifecycle.go:68-81`、`brief.go:220-229` | **改變**：只觸發收集 | `TestATaskIsSettledOnce` 直接呼叫 `Settle`，不受影響 |
| ② 收據表 | `store/sqlite.go`、`messages`、`adapters/board/settings.go` | 新增；看板設定（兩個欄位）搬進 DB 是選配 | 不受影響 |
| ③ Arm 1 綁定交付 | `lifecycle.go:255-275` | **變嚴**：`Worktree.Base` 之類的 commit 會被拒 | B1 沒有 landing 的測試，**要新增一個** |
| ③ `corrected_from`、claims 保留 | `lifecycle.go`、`dispatch.go:191-200`、`record.go` | 新增欄位 | 不受影響 |
| ③ 不搬 Arm 2、佇列、看板 JSON、時間軸 | — | 不做 | — |
| #13 lane | `internal/app/actions.go` 或 TerminalHost adapter | 新增 | 不受影響 |
| #10 交易取代 `writeMu` | `broker.go:234-246`、`store` | 內部 | 三個 race 測試直接呼叫 `mutate`，斷言不變 |
| #24 CLI | `brief.go`、`cmd/clawdline` | 新增；JS 留著當過渡 | 不受影響 |

另外三件事：

- **現在是改 B1 最便宜的時候。** 這台機器上的派工目前全部走舊 app 的 7717：本 task 與在跑的三個 child，task 目錄都在 `/tmp/.clawdline`，
  而 B1 的 task 目錄是 `<CLAWDLINE_NEXT_DIR>/tasks`。B1 只在測試 daemon 上跑過（`docs/broker.md`「自己跑一次」、`eefa318`）。
  也就是說，B1 還沒有任何一筆需要遷移的真實紀錄。
- **與在跑的 child 的衝突**：①②③ 與 #10、#13 都會碰到 `internal/app/orchestrator`、`internal/adapters/store`、`api/v1`，
  也就是在跑的 broker B2＋B3 那個 task 的 claims。**本文沒有動它們；這些改動應該排在 B2＋B3 落地之後，或交給它的下一波。**
  另外兩個在跑的 task（看板頁的前端、排程）不受影響。
- **`cutover.md` 的判準 B1**（新 daemon 不再讀 `~/.config/clawdline`）還沒落地，不受影響。第三件事裡「看板 progress 改讀 broker 的事實」是它在看板這一塊的前提。

---

## 10. 這份文件沒有量到的（不算通過）

- **沒有跑 build，也沒有跑測試。** 這是分析任務。
- **§4 列的 B1 缺陷都是讀程式得出的，沒有重現**：交錯打字、用 base commit 冒充 landed、`/complete` 掉欄位、盤點不完整判 `spawn_failed`、
  同一個 id 同時派工、解不開的列被 upsert 蓋掉。每一項都附了行號，可以直接拿去寫失敗注入測試。
- `auto` 權限今天在 Haiku 上的行為，沒量。
- Linux／Windows 上 child 的環境裡有沒有 `node`，沒量。
- 30 秒窗口：找不到書面理由。我能想到的唯一用途是「child 驗證完又改變主意、正要重寫」，這是推測，沒有驗證。
- 「兩個 root 在同一個 repo 同時落地」用的是代理量：30 分鐘內兩個不同 root 的 `landed_at`。它不是「同一個 checkout 同時操作」的直接量測。
- audit log 是活的檔案，量測期間仍在長，所以不同時間點讀到的筆數會差幾筆。細節欄位因事件而異；本文只用了其中四種：
  `brief.inject` 的 `attempt`、`landing.sweep` 的 `arm`、`landing-queue.advance` 的 `result`、`respawn` 的 `generation`，四種我都自己重算過。
- **`POST /complete` 的使用量量不到**：舊版成功的呼叫不寫 audit。§6.1 的論點建立在 B1 會掉資料這個機制上，不靠用量。
- 所有數字來自一台機器；registry 只保留 08-29 之後的 773 筆，更早的 respawn 與 spawn_failed 看不到。
- B1 的 `CHILD.md` 大小是子代理 B 用模板估的（不含家規約 16.7 KB，這台機器加上家規約 32 KB），沒有實際產出一份來量。
- 看板 19 個欄位的清單是子代理 A 從程式重建的，舊 repo 沒有一份現成的清單。

---

## 附錄 A：數字從哪裡來

| 數字 | 怎麼量的 |
|---|---|
| 照搬 24 項 | 對 `broker-design.md` §7 的表格逐列計數 |
| 773 筆 task 的狀態、隔離、後端、權限、respawn、landing、通知次數 | `python3` 讀 `orchestrator.json`，**先遞迴刪除名稱含 secret／token／key 的欄位**，再計數 |
| 每週 iTerm／tmux 分布 | 同上，以 `created` 的週一分組 |
| 同時落地 66／0 | 同上，以 `repository_common_dir` 分組，數「30 分鐘內有另一個 root 的 `landed_at`」 |
| audit 事件次數 | 逐行解析 `remote-audit.jsonl`，依 `event` 名稱計數；`attempt`／`arm`／`result`／`generation` 四個細節欄位另外分組 |
| `orchestrator-quarantine/` 不存在 | `ls ~/.config/clawdline/` |
| `CHILD.md` 42,190 B、validator 8,676 B、各節大小 | 對本 task 的 `CHILD.md` 以標題切段計 bytes |
| 時間軸的生產者與 `requestReceipts` 0 | `python3` 讀 `project-timeline.json` |
| `ingest` 沒有呼叫者 | `grep -rn '"ingest"' Sources/ Resources/ tools/` |
| `board.entitlement` 沒有讀者 | `grep -rn entitlement Resources/web` |
| B1 的行為 | 讀 `internal/app/orchestrator/*.go`、`internal/adapters/store/broker.go`、`internal/transport/http/orchestrator_wiring.go` |
| 新版 `/v1/health` 與 task 列表 | 用新版自己的 `local-token` 發兩個 GET |

## 附錄 B：自己重讀過的行號

子代理的回報裡，要拿來當證據的地方，我自己重讀了以下這些，全部與回報一致：

- 舊 repo：
  - `TerminalCommandScheduler.swift:17-24`
  - `RemoteServer.swift:235-237`
  - `Tmux.swift:664-665`
  - `OrchestratorChildIdentity.swift:131`、`:324`
  - `Orchestrator.swift:6555-6592`（`restartGrace`）
  - `OrchestratorResultFinalizer.swift:55`、`:124`
  - `b6f4f555` 與 `e924dd9a` 的 commit 原文
- B1：
  - `dispatch.go:66-80`、`:186-202`、`:566-606`
  - `lifecycle.go:64-82`、`:180-276`
  - `broker.go:158-192`、`:226-248`
  - `store/broker.go:100-110`
  - `watch.go:60-70`、`:128-140`、`:200-262`
  - `orchestrator_wiring.go:25-35`
  - `brief.go:220-231`
  - `draft.go:210-214`
  - `race_test.go:80-140`、`composer_test.go:60-100`
