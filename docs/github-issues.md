> **整套工作系統（看板項目、Session 待辦、Backlog、GitHub Issue）怎麼運作、哪些已經做到，一份講完在 [`docs/work-system.md`](work-system.md)。**

> **實作依據仍是 [`docs/design-decisions.md`](design-decisions.md)。** 本文是分析與建議；要採納其中任何一條，由 root 在那份文件加一列裁決（它 §1 的用法）。

# 看板與 Backlog 對照 GitHub Issue：哪些讓位、哪些留下、哪些設計拿來用

> **需求來源（使用者 2026-09-19 原話，逐字）**
>
> 「如果我們的看板系統、backlog 使用 GitHub Issue 來輔助，我們哪些應該要拿掉，直接移到 GitHub Issue？因為 GitHub Issue
> 直接提供了問題描述、解決方案是哪一個 PR、狀態追蹤等。或是我們也可以參考它的設計來改良我們的系統。所以重點是：
> 1. 哪些功能應該使用 GitHub Issue 來取代？ 2. 哪些功能我們應該留在我們系統中，然後參考 GitHub Issue」
>
> **本輪只出文件，不改實作。** 依據是 master `c0c545a`：三結構的 T2（Session 待辦）、T3（看板與 Backlog）、
> T4（提議、決定、摘要）都已落地，本文讀的是那份程式，不是設計稿。

---

## 0. 五句話

1. **GitHub Issue 能取代的是「一類內容」，不是我們的「機制」。** 該搬的是讀者在這台機器以外的東西：外部回報的缺陷與需求、
   開源產品對外承諾的規劃、「哪個 commit 解決了它」的公開紀錄、發版分組。三結構（看板、Session 待辦、Backlog）與 T4 的
   提議、決定、摘要，**沒有一項應該拿掉**。
2. **理由只有一個：我們每一條規則讀的都是本機 broker 的事實**（派工、結束、落地），GitHub 看不到這些事實。把狀態搬上 GitHub，
   等於把 D01「落地只有一份事實」變成兩份；舊版的 345 張「證據上已結束、生命週期沒動」的卡，就是兩份事實分岔的樣子。
3. **現實條件量過了（§1）。** 30 天內碰過的 17 個 GitHub repo 只有 2 個公開，而公開那一側幾乎全是 `sainteye/clawdline` 本身；
   Claude 的工作有 44.3%、Codex 有 27.6% 不在公開 repo 裡。公開 repo 目前 **0 個 issue、1 個 PR、約 1,680 個 commit 直接進 `main`**：
   PR 那一套今天用不上，但 **commit 訊息裡寫的 `Fixes #N`** 跟現在的落地流程相容。
4. **值得學的是三件事（§5）**：`closes #N` 把綁定寫在交付物本身（我們的 `work_id` 只存在派工紀錄裡，git 裡看不到）；
   `state_reason` 裡的 `duplicate`（我們的雙胞胎問題沒有出口）；每個 repo 唯一的短編號（舊版 `CLA-53` 在三個專案裡重複）。
   「兩態＋labels」只學外層的顯示，不學儲存；assignee 與 milestone 我們現在的做法比較強，不改。
5. **要整合的話，第一步是「只連結、不同步」，而且不呼叫 GitHub API（§6）**：人下指令把本機項目連到一個已經存在的 issue，
   落地時 root 在 commit 訊息寫 `Fixes owner/repo#N`，push 到預設分支時由 GitHub 自己關 issue。不需要 token，沒有速率限制，
   離線也照樣正確。任務書舉的「雙向同步」**不建議做**（§6.4）。

---

## 1. 現實條件：量到的，不是印象

### 1.1 工作發生在哪種目錄

**量法**：從 09-19 往回 30 天，Claude transcript 3,371 份、Codex session 1,181 份，每份只抽第一個 `cwd` 欄位。
每個目錄問本機 git：在不在 work tree 裡、有沒有 remote。GitHub repo 公不公開，用**不帶憑證**的
`GET api.github.com/repos/<owner>/<repo>` 判斷：200 是公開，404 是私有或不存在。已經刪掉的 Clawdline worktree，
依路徑裡的專案名對回主 repo。腳本只輸出加總數字，不輸出任何路徑或 repo 名（附錄）。

| 目錄類型 | Claude（1,223 份 \*） | Codex（1,181 份） |
|---|---:|---:|
| 公開的 GitHub repo | 681（55.7%） | 855（72.4%） |
| 私有的 GitHub repo | 257（21.0%） | 276（23.4%） |
| 是 git，但沒有 remote | 77（6.3%） | 11（0.9%） |
| 不是 git | 32（2.6%） | 29（2.5%） |
| 已刪除的暫存目錄 | 174（14.2%） | 10（0.8%） |
| 判定不了 | 2（0.2%） | 0 |
| **不在公開 repo 的合計** | **542（44.3%）** | **326（27.6%）** |

\* 另有一個不是 git 的暫存目錄，30 天內有 2,148 份（每天約 72 份），是自動化的 headless 執行，不算工作，所以不列入分母。

- **以 repo 數算**：17 個 GitHub repo，公開 2 個、私有 15 個。
- **公開那一側幾乎只有產品本身**：還在的目錄裡，落在公開 repo 的 Claude transcript 有 438 份，其中 437 份是 `sainteye/clawdline`；
  Codex 是 590 份全部都是。
- `clawdline-go` 目前沒有設定 remote（算在「是 git，但沒有 remote」這一列），之後要變成 `sainteye/clawdline` 的內容，
  換過去之後才會算進公開這一列。

**判定：使用者說的「很多工作發生在沒有 repo 或不公開的目錄」成立。** 以專案數算是絕大多數，以工作量算大約三到四成。
另一件同樣重要的事：**公開那一側實際上只有一個 repo，就是這個產品。** 所以「搬到 GitHub Issue」真正有意義的只有一個 repo。

### 1.2 公開 repo 今天怎麼被使用

09-19 不帶憑證查 `sainteye/clawdline`：issue **0**、PR **1**（沒有合併）、預設分支約 **1,680** 個 commit、Issues 與 Projects 開著、
Discussions 關著。工作流程是 child 在分支上交付，root 在本機把它落地到 `main` 再 push，**中間沒有 PR**。

影響：

- GitHub 的「PR 連到 issue、merge 時關閉」今天不會發生。
- 但 closing keyword 也可以寫在 **commit 訊息**裡，commit 進到預設分支時一樣會關 issue（GitHub Docs〈Closing an issue〉：
  「close issues automatically with keywords in pull requests and commit messages」）。這條路跟現在的落地流程相容。
- 關鍵字只在**預設分支**生效（〈Linking a pull request to an issue〉），所以本機的「落地到 `main`」和 GitHub 的「關閉」
  中間隔了一次 push。這兩件事不能當成同一件。

### 1.3 量級、速率、延遲

- **量級**：舊看板 787 張卡裡有 602 張（76.5%）其實屬於 Session 待辦，每天新增約 41 張；歷史紀錄 11 天 12,900 筆，
  **每天一千多筆**；批次對帳時最多一秒動到 88 張（`board-redesign.md` §1.3、§1.4、§7.2）。新版每一個派工都產生一筆待辦（`OriginDispatch`）。
- **GitHub 的上限**（docs.github.com，09-19 讀）：驗證過的使用者每小時 5,000 次；**會產生內容的請求每分鐘 80 次、每小時 500 次**；
  超過會回 403／429，要等 `retry-after`，沒有這個標頭就至少等一分鐘；大量寫入之間建議隔一秒；不帶憑證每小時 60 次；
  帶 ETag 的條件請求回 304 時不計入主要上限。
- **換算**：每天 41 張卡的「建立」碰不到上限，會碰到的是**狀態更新**。每天一千多筆歷史，只要其中一次批次對帳（一秒 88 張）
  變成 GitHub 寫入，就直接撞上每分鐘 80 次。
- **延遲與通知**：本機看板的讀取是一次 SQLite 查詢，GitHub 的讀取是一次網路往返。daemon 只聽 `127.0.0.1`，GitHub 的 webhook
  需要一個公開可達的端點，而 Cloud 線上的 `wh/` 前綴是保留的、一律拒絕（`cloud-wire.md` §10）。所以要從 GitHub 拿變化，
  今天只能輪詢，輪詢會吃掉速率預算。

### 1.4 三結構現在實際有什麼

| 結構 | 表 | 由誰寫 | 關鍵規則（程式） |
|---|---|---|---|
| **看板** | `work`、`board_items`（active／awaiting_closure／done／dropped，`owner NOT NULL`） | 人的指令＋規則 | `work.Derive` 每次讀取時從 broker 事實推導，不落盤（D04）；`SweepRules`：落地就結束、交付進入收尾佇列、3 天沒有新事實就回 Backlog（`internal/domain/work/board.go`） |
| **Backlog** | `backlog`（planned／dropped、`rank`、`start_on`） | 人＋規則 | 派工帶著它的 `work_id` 就移進看板；`start_on` 進入 7 天內就移進看板；永遠不自動刪除 |
| **Session 待辦** | `todos` | **只有 broker** | 派工時建立、落地時結束、owner 不在就移交，24 小時沒人接就放掉（`todos.go`） |
| **提議** | `proposals` | session 提、人答 | `GateProposal`：I1–I3 事實訊號，每回合最多 1 次、每天最多 3 次，人在場才在對話裡問，7 天沒回答就留在待辦（`proposals.go:343`） |
| **決定** | `decisions` | session 問、人答 | `NewDecision`：沒有安全預設就拒絕；只有擋住工作的決定才推播 |
| **摘要** | `digests` | 規則 | 每天一則、每週一則 |
| **移動紀錄** | `moves` | 以上全部 | append-only，trigger 擋掉 UPDATE 與 DELETE（`store/work.go:89-92`） |

---

## 2. 逐項對照

「GitHub 對應」包含 Issues、Projects、Milestones、labels、PR 連結、`closes #N`、Discussions。

| # | 職責 | 我們現在怎麼做 | GitHub 對應 | 能不能取代 | 取代之後失去什麼 | 判定 |
|---|---|---|---|---|---|---|
| 1 | **正在發生的工作**（看板「進行中」） | `Derive` 從 broker 事實推：有活著的 task 就是 active | open issue＋Projects 的 Status 欄 | **不能** | GitHub 看不到本機的 task 是不是還活著；Status 欄是人或 Action 寫進去的拷貝，會跟事實分岔；斷網時看不到 | **留** |
| 2 | **規劃中的工作**（Backlog） | `backlog` 表、`rank`、`start_on`；7 天內或派工時自動進看板 | open issue＋milestone＋Projects 的排序與 Iteration；Projects 的 draft issue 可以不屬於任何 repo | **部分** | 「日期到了」「派工了」這兩條自動承諾；私有與非 git 目錄的規劃沒地方放（draft issue 只存在 project 裡，沒有 PR 連結、沒有通知，也碰不到本機事實）；「永遠不自動刪除」的保證 | **分流**：開源產品對外承諾的規劃放 GitHub，其餘留在本機 |
| 3 | **Session 自己的待辦** | `todos`：每個派工一筆，機器寫、機器關 | task list、sub-issue | **不能** | 讀者是 session 不是人（D1）；量大到是雜訊；會撞上每分鐘 80 次的寫入上限；會把私有工作公開 | **留** |
| 4 | **提議**（值不值得讓人追蹤） | `GateProposal`：事實訊號、注意力預算、人不在場就不問 | 沒有對應，開 issue 本身就等於「已經追蹤」 | **不能** | 預算，以及「預設就是不問」 | **留**（可以多一個答案「開成 GitHub issue」，見 §6.5） |
| 5 | **等你決定** | `decisions`：選項、**必填的預設答案與期限**、只推播擋住工作的 | issue 或 Discussion 的留言 | **不能** | 期限、預設、推播預算；session 拿到的是結構化的答案，不是一段文字 | **留** |
| 6 | **停擺偵測** | `ruleStalled`：3 天沒有新事實就回 Backlog，寫 `stalled_3d` | 沒有內建（要自己寫 Action 或 stale bot） | **不能** | 「事實」是本機的派工與交付 | **留** |
| 7 | **收尾閘門** | `ruleLanded`：broker 的落地紀錄就是結束；交付了還沒落地就進收尾佇列；問過之後 7 天沒回答就是 `unconfirmed`；沒有任何指令能說「已落地」（`landingWords` 會具名拒絕） | `closes #N`：commit 或 PR 進入預設分支時關掉 issue | **部分**：只對「公開 repo、push 到預設分支」那一部分 | 本機落地和 push 是兩件事；非程式碼的交付、私有目錄、還沒 push 的，GitHub 都看不到 | **留**；另外把 `Fixes #N` 當成落地時順手寫下的對外綁定（§5.2、§6） |
| 8 | **自動狀態轉換的證據** | 每次移動寫一筆 `moves`，`evidence` 記是哪個 task、哪個時間 | issue timeline（closed by commit、cross-referenced） | **不能** | GitHub 只記在 GitHub 上發生的事 | **留** |
| 9 | **移動紀錄** | `moves`，只能新增 | issue timeline | 只對 GitHub 上的變更有效 | 本機規則的稽核 | **留** |
| 10 | **每日／每週摘要** | `digests`：完成了什麼、什麼停擺、自動移動、待確認、等收尾 | notifications、email | **不能** | 本機工作的摘要只有本機做得出來 | **留** |
| 11 | **人的指令** | 10 個：start、track、schedule、defer、accept、rework、drop、handover、untrack、rank | close／reopen、assign、milestone、label、Projects 欄位 | 只對已連結的項目有部分對應 | — | **留**；連結到 issue 的那一面，在 GitHub 上操作 |
| 12 | **工作身分** | `work.id`（UUID），派工帶 `work_id`（D36） | 每個 repo 唯一的 `#N` | 不能當主鍵：私有與非 git 的工作沒有 repo | — | **留**，外加一個選填的 GitHub 連結（§6） |
| 13 | **問題描述與驗收條件** | `title`、`acceptance`（選填） | issue body、issue form | **可以**（對外那一類） | 不必重寫一份 | 對外的放 GitHub，本機的留在本機 |
| 14 | **「是哪個 PR 或 commit 解決的」** | broker 的 `Landing{Repo, Target, Commit}` 是唯一事實（D01，`orchestrator/record.go:128`） | linked PR、closing commit，兩個方向都看得到 | **對外公開的那一份可以** | 本機的落地證明（D17 的 delivery head、base） | 真相留在本機；對外靠 commit 訊息裡的 `Fixes #N` |
| 15 | **外部回報的缺陷與需求** | **沒有**，本機沒有任何讓外人回報的入口 | Issues＋issue form | **應該由 GitHub 做** | — | **搬（新增）** |
| 16 | **發版分組** | 沒有 | milestone＋Releases | **應該由 GitHub 做** | — | **搬（本機不要做）** |
| 17 | **容量** | `work.open` 2,000、`proposals.open` 500、`decisions.open` 256、摘要 800 | 一個 project 最多 5 萬項 | — | — | 不構成搬家的理由（我們的上限是刻意的，DG-2） |

---

## 3. 應該直接用 GitHub Issue 的

**判準：讀者在這台機器以外的，就放 GitHub。** 讀者在這台機器上的（使用者自己、session），就留在本機。

| # | 什麼 | 放在哪裡 | 為什麼在 GitHub 比較好 |
|---|---|---|---|
| A | **外部回報的缺陷與功能需求**（使用者、外部貢獻者） | `sainteye/clawdline` 的 Issues，加上 bug／feature 兩個 issue form | 外人只看得到 GitHub；我們沒有、也不該做公開的收件入口（Cloud 是私人的）；搜尋、訂閱、重複標記、`closes` 都是現成的；**不用我們維護** |
| B | **開源產品對外承諾的規劃**（roadmap、「下一版會有」） | issue＋milestone | 有可以分享的連結；外部協作者看得到，也能認領（assignee）、在 PR 裡寫 `Fixes`；完成度由 GitHub 算。本機 Backlog **不再另收一份**，要開始做的時候，把本機項目連到那個 issue（§6） |
| C | **「哪個 commit 解決了它」的公開紀錄** | 落地 commit 的訊息裡寫 `Fixes #N` | 綁定寫在交付物本身，換了工具也還在；GitHub 自動兩個方向都顯示 |
| D | **發版分組與 release notes** | milestone、Releases | 這是公開的產品節奏，本機沒有讀者 |

### 3.1 那「應該拿掉」的是什麼

直接回答使用者的第一個問題：**已經落地的程式沒有一項要拿掉。** GitHub Issue 取代的是**用途**，不是結構：

1. **一條使用規則**：開源產品的對外需求與缺陷，不再開成本機的 Backlog 項目。這不用寫程式，寫進使用說明與 session 的簡報就好。
2. **兩個以後都不要做的功能**：公開的回報入口、本機的 milestone／發版分組（第 15、16 列）。
3. **連結到 GitHub issue 的項目，本機不再各自維護的兩件事**：Backlog 的 `rank`（排序以 GitHub 那邊為準），
   以及每週摘要裡「30 天沒人看，要留嗎？」的詢問（它在 GitHub 上已經有人看了）。這要等 §6 的連結存在之後才談得上，是一個小改動。

---

## 4. 必須留在本機的

使用者點名的六個現實條件，逐項對照（✗＝GitHub 在這一條上做不到，或會出錯）：

| 項目 | ① 沒有 repo 或不公開 | ② 每天幾百筆 | ③ 離線要正確 | ④ 注意力預算 | ⑤ 速率與延遲 | ⑥ 隱私 |
|---|:-:|:-:|:-:|:-:|:-:|:-:|
| Session 待辦 | ✗ | ✗ | ✗ | — | ✗ | ✗ |
| 看板（進行中、等收尾、本週排入、最近完成） | ✗ | — | ✗ | — | ✗ | ✗ |
| Backlog（私有與非 git 的規劃；所有項目的承諾規則） | ✗ | — | ✗ | — | — | ✗ |
| 提議 | ✗ | — | — | ✗ | — | ✗ |
| 等你決定 | ✗ | — | ✗ | ✗ | ✗ | ✗ |
| 停擺、收尾、摘要 | ✗ | — | ✗ | ✗ | — | ✗ |
| `moves` | ✗ | ✗ | ✗ | — | ✗ | ✗ |
| 落地的真相（broker `Landing`） | ✗ | — | ✗ | — | — | ✗ |

逐條的理由：

- **① 沒有 repo 或不公開**：Claude 44.3%、Codex 27.6% 的工作不在公開 repo；15／17 個 GitHub repo 是私有的（§1.1）。
  GitHub Issue 只能掛在 repo 上，非 git 目錄的工作沒有地方放。
- **② 每天幾百筆**：Session 待辦每個派工一筆，舊版歷史每天一千多筆。上了 GitHub 就是雜訊，而且它的讀者是 session（D1）。
- **③ 離線要正確**：看板的每個狀態都是從本機 SQLite 裡的 broker 事實推出來的（D04），斷網時照樣正確。
  如果狀態放在 GitHub，斷網的時候不是看不到，就是看到過時的拷貝。
- **④ 注意力預算**：提議每回合最多 1 次、每天最多 3 次；只有擋住工作的決定才推播（`DefaultProposalPolicy`，`proposals.go:291`）。
  GitHub 的通知不受我們的預算控制，外人的留言也會觸發。
- **⑤ 速率與延遲**：每分鐘 80 次的寫入上限，加上沒有 webhook 只能輪詢（§1.3）。
- **⑥ 隱私**：**公開 repo 裡的 issue 一律公開**，GitHub 沒有「公開 repo 裡的私有 issue」這種東西；issue 的刪除只有 admin 能做，
  而且是永久刪除，在那之前可能已經被快取或索引。本機工作的標題常常帶著客戶名、路徑或私人事務，這個 repo 自己的
  `tools/check-private.sh` 就是為了擋這些東西寫的。

---

## 5. 參考它的設計

每一項都寫：我們現在怎麼做、改成 GitHub 那樣的代價與好處、建議。

### 5.1 狀態只有 open／closed 兩態，外加 labels

- **我們現在**：看板存四個狀態，Backlog 兩個，再加上「在哪個結構」；`closed_reason` 有四種（landed、accepted、unconfirmed、dropped）；
  沒有 labels。
- **GitHub**：open／closed，加上 `state_reason`（`completed`、`not_planned`、`duplicate`、`reopened`），再加自由的 labels。
- **對照**：**`closed_reason` 本來就是我們的 `state_reason`**：landed 是「由事實確認的 completed」，accepted 是「由人確認的 completed」，
  dropped 對應 not_planned。unconfirmed 在 GitHub 沒有對應，因為 GitHub 從來不會自己關 issue。
- **把儲存縮成兩態的代價**：`board_items` 的 CHECK 約束（看板項目一定有 owner、關閉就一定有原因）會退回程式慣例。
  `board-redesign.md` §3.2 比較過「同一張表、用 kind 區分」，那正是舊版的做法，§1.3–1.4 的數字就是結果。
- **加自由 labels 的代價**：舊版的 `audience`／`catalogDisposition` 本質上就是 labels，787 張卡混在一起就是它的結果。
  我們的分面（`project`、`commitment`、在哪個結構）都有型別，有型別才擋得住。
- **建議**：學 `state_reason`（已經有了），**不學** labels，也不把儲存縮成兩態。API 與手機畫面可以加一層外層顯示：
  「開著／關了＋原因」，active 與 awaiting_closure 當成開著底下的分區。這只是顯示，沒有儲存成本。

### 5.2 `closes #N` 的自動關閉

- **我們現在**：`ruleLanded`：這一輪綁定的交付全部在 broker 紀錄上落地，項目就結束。綁定靠派工時帶的 `work_id`
  （D36，`orchestrator/draft.go` 的 `admitWorkID`），**只存在 broker 的 task 紀錄裡，git 裡看不到**。
- **GitHub**：綁定由寫這個變更的人，寫在變更本身（commit 訊息或 PR 描述）；關閉發生在程式碼進入預設分支的那一刻。
- **它兩個設計裡，「關閉時點＝進入預設分支」我們已經有了**，就是落地。**我們沒有的是「綁定寫在交付物上」。**
  它的好處是換了工具綁定也還在：將來的時間軸（D41）或任何人讀 git，都能知道這個 commit 是為了哪件事。
  而且綁定多了第二次機會：舊版派工只有 12.1% 綁到看板項目，派工時漏掉的，落地時寫 commit 的那一刻還能補上。
- **代價與一個限制**：
  - root 的落地步驟多一行 commit 訊息；落地的時候可以檢查「已連結的項目，落地 commit 沒有 `Fixes`」，只提醒，不擋。
  - **公開 repo 只寫 `Fixes owner/repo#N`**，那是本來就公開的資訊。**不要把 `work_id` 寫進公開 repo 的 commit**：這個 repo 的
    隱私守衛把真實的 task／session id 當成私人資料，`work_id` 是同一類東西。私有 repo 與沒有 remote 的 repo 可以選擇寫一行
    `Clawdline-Work: <work_id>` trailer。
  - GitHub 只在預設分支關閉；我們的 `defaultBranches` 是用名字判斷的（`main`／`master`，`proposals.go:62`），同一個落差兩邊都有。
- **建議**：做（§6.2 的一部分）。

### 5.3 PR 與 issue 的雙向連結

- **我們現在**：項目到 task 靠 `work_id`，task 到 commit 靠 `Landing{Repo, Target, Commit}`；項目頁列得出 task。
  **反方向（從一個 commit 找到它為了哪件事）不存在。**
- **GitHub**：cross-reference 事件兩個方向都自動出現。
- **建議**：項目頁從 broker 的落地紀錄推出「落地的 commit」清單。這是 D01 的純函數，不用新增任何儲存。反方向只靠 §5.2 的
  `Fixes`／trailer，不另外建索引。代價很小，好處是從一個 commit 就查得到它為什麼存在。

### 5.4 milestone 當作 Backlog 的分組

- **我們現在**：`rank` 決定順序，`start_on` 是每一項自己的日期，進入 7 天內就變成承諾、自動移進看板。沒有分組。
- **GitHub**：milestone 是一個有名字、有到期日的群組，完成度自動算（關了幾個／總共幾個）。**到期日是被動的，到了什麼也不會動。**
- **對照**：我們的 `start_on` 是主動的（會移動項目），milestone 是被動的。分組這個需求**沒有量過**：舊版有人把還沒動工的
  epic 登記成卡（`board-redesign.md` §1.7-3），但那些卡從來沒有開始過。
- **建議**：本機**不做** milestone。開源產品的發版分組用 GitHub 的 milestone（§3 D）。將來本機真的要分組，
  學 sub-issue（一個 parent，進度由子項推導），不要學 milestone：parent 自己也是一個工作項目，走同一套生命週期，
  不用另外發明一種東西。

### 5.5 issue template 當作建立時的判準

- **我們現在**：判準已經有了，而且在伺服器端：`POST /v1/work/items` 規定要有 `project` 與 `title`（`project_required`，
  專案永遠不從 session 的目錄猜）；提議要過 I1–I3 的事實訊號（`GateProposal`）；決定沒有安全預設就拒絕（`NewDecision`）。
  後面兩個其實就是「必填欄位」。
- **GitHub**：issue form（YAML）有必填欄位與下拉選單，判準在建立的當下就擺在建立者面前。
- **建議**：
  - 機器替人建立東西的兩個地方（提議、決定），判準已經用伺服器的拒絕碼做到了，而且比 template 嚴格。template 能擋住的，
    我們已經擋了；它擋不住的（agent 自己的判斷），我們本來就不讓它決定（D7）。**不用改。**
  - 人自己開的項目，判準就是人本身，不要加必填欄位增加摩擦。收尾佇列可以標出「沒有驗收條件」，讓人知道「收下」是主觀判斷。
  - 真正該用 template 的是 GitHub 那一側：`sainteye/clawdline` 的 bug／feature issue form（§3 A）。

### 5.6 assignee 當作責任人

- **我們現在**：看板上的 `owner NOT NULL`（root 的對話 id，或 `user`）；有 `handover` 指令；owner 不在了就標「無人負責」，時鐘照走；
  待辦移交之後 24 小時沒人接就放掉。
- **GitHub**：0 到 10 個 assignee，可以不填；沒人負責的 issue 很常見。assignee 是一個**會一直在的人**。
- **對照**：我們比較強：一定要有、而且只有一個。GitHub 的「可以不填」正是舊版 #3b（開始了沒人收尾）的來源。
  可以學的是反過來那一面：我們的 owner 是**一段對話**，對話會結束。這個產品只有一個使用者，所以「負責的人」永遠是使用者，
  對話只是執行者。
- **建議**：不改。已經有的「無人負責」標記就是這個差別的顯示。

### 5.7 額外一：`duplicate` 關閉原因（建議做，小）

- **問題**：同一件事開出兩張卡（`board-redesign.md` §1.7-1，至少 9 對）。現在唯一的出口是 `drop`，一丟掉，兩張之間的關係就沒了。
  U1 決定不遷移舊卡之後，雙胞胎只會從新項目產生（人開的項目、依工作線產生的提議），但一樣會發生。
- **學 GitHub**：多一個關閉原因 `duplicate`，帶 `duplicate_of: <work_id>`，加一個人的指令。摘要可以寫「已合併進 X」。
- **代價**：一個列舉值、一個欄位、一個指令與它的拒絕碼（例如指向自己或指向已關閉的項目）。

### 5.8 額外二：每個專案唯一的短編號（建議做，小）

- **問題**：`work.id` 是 UUID，沒辦法在對話裡講；舊版的 `CLA-53` 好講，但在三個專案裡重複（80 個 key、224 張卡，§1.7-6）。
  使用者的指令大多經由 session 轉達（U4），短編號剛好用得上：「把 #12 放回 Backlog」。
- **學 GitHub**：每個 repo 各自從 1 遞增的 `#N`。我們做成每個 `project_id` 一個計數器，加上 `UNIQUE(project_id, n)`；
  UUID 仍然是身分，`#N` 只是顯示與輸入用。

### 5.9 不學的一件：GitHub 從來不自己關 issue

GitHub 靠人與關鍵字關 issue，issue 可以開著好幾年。我們刻意不同：落地就自動結束，交付沒人回答就結束成 `unconfirmed`。
因為量到的結果是只靠人，十一天 787 張卡裡進到 `closed` 的是 0 張（`board-redesign.md` §1.4）。**這一條不學。**

---

## 6. 如果要整合

### 6.1 原則

- **每個欄位只有一個寫入者。** issue 的內容與開關由 GitHub 管；本機的生命週期由 broker 的事實與人的指令管。
- **連結存在本機，同時寫進 git。** 本機存 `owner/repo#N`；落地時寫進 commit 訊息。
- **GitHub 的狀態永遠不直接改本機的狀態。** 最多只是一個觀察，或者一個提議（DG-4、DG-10）。

### 6.2 最小可驗收的第一步：連結，不同步

| 內容 | 細節 |
|---|---|
| 新指令 `link`／`unlink` | 人（或人經由 session，U4）對一個工作項目下：`{"github": "owner/repo#N"}`。條件：項目的專案有 GitHub remote，而且 `owner/repo` 相同（讀本機的 `git remote`，不上網）；issue 由人自己在 GitHub 開好。一個項目一個連結；要換就先 `unlink`。寫一筆 `moves`（trigger `linked`／`unlinked`） |
| 寫入路徑不上網 | 這一步**沒有任何 GitHub 用戶端**：不讀 token、不呼叫 API。畫面顯示連結，狀態寫「GitHub 上的狀態：未查」，不假設它是開著的（DG-7） |
| 落地 | 已連結的項目落地到該 repo 的預設分支時，root 的落地指引要求 commit 訊息帶 `Fixes owner/repo#N`；push 之後由 GitHub 關 issue。落地閘門讀本機 git，檢查落地的 commit 範圍裡有沒有這個關鍵字，**沒有就提醒，不擋** |
| 已連結項目的 Backlog | 不再問「30 天沒人看，要留嗎？」，`rank` 也不用（§3.1 第 3 點） |

**驗收（每一條都要能失敗）：**

1. 連到 remote 不相符的 repo，回 `409 link_repo_mismatch`；專案沒有 GitHub remote，回 `409 no_github_remote`；格式不對，回 `400`。
2. 斷網（或者根本沒有網路介面）時，`link` 照樣成功，項目頁顯示「未查」，不是「開著」。
3. 這一步的程式裡沒有 `api.github.com`，也沒有讀 token 的路徑；用一個守衛測試寫死這一點。
4. 已連結項目的落地指引，恰好出現一次 `Fixes owner/repo#N`；沒有連結的項目不會出現。
5. `link`、`unlink` 各寫一筆 `moves`，重送同一個 Idempotency-Key 會重播原本的回應（D03）。
6. 離開這台機器的只有 `#N`，而且是經由 root 自己寫的 commit 訊息。

### 6.3 失敗模式

| 情況 | 會發生什麼 | 第一步怎麼處理 |
|---|---|---|
| **離線** | 寫入路徑不上網 | 不受影響；push 之後 GitHub 才關 issue |
| **速率限制** | 第一步不呼叫 API | 不適用。第二步（§6.5）用 outbox（D08），遇到 `retry-after` 就照等，寫入之間隔一秒，錯誤具型別（`github_rate_limited`），**不擋本機任何寫入** |
| **權限** | 第一步由 push 的人觸發關閉 | 不需要 token。跨 repo 用關鍵字關 issue 需要什麼權限，**本文沒有驗證**。第二步需要 fine-grained token（issues:write），放在 Keychain，agent 永遠讀不到 |
| **目標不是預設分支，或者 squash／rebase 把訊息改掉了** | GitHub 不關 issue；本機照樣因為落地而結束 | 項目顯示「已落地、GitHub 狀態未查」。第三步如果讀到 issue 還開著，最多在摘要裡寫一行，**不是一個狀態** |
| **外部 PR 在 GitHub 上把 issue 關了** | 本機沒有 task，項目還是 active | 照規則 3 天後回 Backlog。第三步可以把「GitHub 上已關閉（`not_planned`／`completed`）」變成一個提議：「要在本機放棄或收下嗎？」，**不自動** |
| **issue 被刪除或轉移** | 刪除只有 admin 能做，而且是永久的；轉移之後 repo 與編號都會變 | 第一步不讀，所以照樣顯示「未查」。第三步讀到 404／410 就標 `link_broken`（觀察，不是狀態） |
| **雙向衝突** | 每個欄位只有一個寫入者 | 不存在 |
| **誰是真相來源** | 見下表 | — |
| **隱私** | 第一步對外多出的只有一個 issue 編號 | 第二步（由 Clawdline 開 issue）會送出標題與內文：人一定要看到**要送出的原文**再確認；送出前跑一次跟 `tools/check-private.sh` 同一套的規則（`internal/domain/privacy`）；永遠不自動送 |

| 什麼 | 真相來源 |
|---|---|
| issue 的標題、內文、labels、milestone、開或關 | GitHub |
| 本機項目的生命週期、綁定的 task、在哪個結構 | Clawdline（broker 事實＋人的指令） |
| 落地 | broker 的 `Landing`（D01）；GitHub 上的 closed 只是它的**對外回聲**，不是證據 |
| 項目與 issue 的連結 | Clawdline 的 `moves`，加上 git 裡的 `Fixes` |

### 6.4 為什麼不建議「雙向同步」

任務書舉的例子是「只有被標成人要追蹤、而且專案有 GitHub remote 的項目，才雙向同步成 issue」。條件本身是對的，問題出在「雙向」與「同步」：

1. **本機的狀態變化上了 GitHub 就是公開的雜訊。** `stalled_3d` 回 Backlog、`schedule_missed`、`unconfirmed` 都是給使用者自己看的
   自我管理；同步出去之後，外部的關注者會看到 issue 一直改 label、被機器關掉，然後又重新打開。
2. **`unconfirmed` 會在沒有人的情況下關掉公開的 issue。** 對外人來說，這等於宣稱「做完了」，但本機的意思只是「沒人回答，我先清掉」。
3. **每個欄位有兩個寫入者**，就需要衝突規則，還需要 webhook（沒有公開端點，Cloud 的 `wh/` 也會拒絕）或輪詢（吃速率預算）。
4. **「做完了」會有兩個真相來源**，直接違反 D01。

### 6.5 之後的步驟（每一步開始前都要再問使用者）

| 步 | 內容 | 前提 |
|---|---|---|
| 2 | **人確認之後由 Clawdline 開 issue**：提議多一個答案「開成 GitHub issue」（只在專案有公開 GitHub remote 時出現），或者一個指令；先顯示要送出的原文，人確認後才寫進 outbox（D08），送出後把 `#N` 寫回，變成 §6.2 的連結 | 第一步用過一陣子；token 的存放（Keychain）先定案 |
| 3 | **唯讀讀回 GitHub 的狀態**：只讀已連結的 issue，帶 ETag（回 304 不計入上限），結果當成**觀察**顯示，或者變成提議（§6.3） | 第二步 |
| — | 雙向同步、Session 待辦上 GitHub、自動開 issue | **不做** |

---

## 7. 需要使用者拍板的（都已經選了安全的預設）

| # | 問題 | 預設 | 為什麼這是安全的那一邊 |
|---|---|---|---|
| G1 | 開源產品的對外需求與缺陷，從現在起改在 `sainteye/clawdline` 的 Issues 收（加兩個 issue form） | **是** | 不用寫程式；本機本來就沒有這個入口 |
| G2 | 開 Discussions | **不開** | 目前 0 個 issue，還沒有需要分流的討論 |
| G3 | 做 §6.2 的「連結，不同步」 | **做**，排在 T 線核心之後 | 不需要 token、不上網；錯了只是少一個連結 |
| G4 | 由 Clawdline 代開 issue（§6.5 第二步） | **不做**，等 G3 用過再說 | 對外公開幾乎不可逆 |
| G5 | §5.7 的 `duplicate` 與 §5.8 的短編號 | **做**（兩個都小） | 只增加，不改現有的語意 |
| G6 | 雙向同步 | **不做** | §6.4 |

---

## 8. 結論與建議順序

**一句話：GitHub Issue 只接手「要給外面的人看的」那一類內容（外部回報、開源產品的公開規劃、解決它的 commit、發版分組），
三結構與提議、決定、摘要全部留在本機，因為它們讀的是 GitHub 看不到的本機事實；我們向 GitHub 借三個設計，也就是寫在
commit 裡的綁定、`duplicate`、短編號，整合只做單向連結。**

建議順序：

1. **現在就能做，不用寫程式**：開源產品的對外需求與缺陷改開在 `sainteye/clawdline` 的 Issues，加 bug／feature 兩個 issue form；
   本機 Backlog 不再收這一類（G1）。
2. **參考設計，小改**：`duplicate` 關閉原因、每個專案的短編號、「開著／關了＋原因」的外層顯示（§5.1、§5.7、§5.8）。
   都是 T3 的延伸，可以跟 T6 前端一起做。
3. **整合第一步**：`link`／`unlink`、落地時寫 `Fixes owner/repo#N`、已連結項目的 Backlog 不再問、項目頁的「落地的 commit」清單
   （§6.2、§5.3）。
4. **之後再問**：人確認後由 Clawdline 開 issue（outbox），以及唯讀讀回 GitHub 的狀態（§6.5）。
5. **不做**：雙向同步、Session 待辦上 GitHub、自動開 issue、本機的 milestone。

---

## 9. 這份文件沒有做到的事

- **沒有改任何實作，也沒有跑 build 或測試。** 本文只新增這一份文件。
- **量測的限制**：
  - 只有 30 天。`cwd` 是 session 開始時的目錄，session 中途跑去別的目錄工作不會被算到。
  - 「一個自動化目錄」是用規則排除的（30 天內 ≥500 份、不是 git），沒有去確認它實際在做什麼。
  - 不帶憑證的 404 分不出「私有」與「不存在」。
  - 已刪除的 worktree 是用專案名對回主 repo 的；Claude 那一側有 2 份對不回去。
  - 數的是 session，不是 task，也不是看板項目。
  - 舊看板的數字（787、41／天、一秒 88 張）引用自 `board-redesign.md`，本文沒有重算。
- **GitHub 的規格**是 2026-09-19 從 docs.github.com 讀的：速率限制、關鍵字、關閉原因、sub-issue（每個 parent 最多 100 個、最多 8 層）、
  draft issue、刪除權限。跨 repo 用關鍵字關 issue 需要什麼權限，**沒有驗證**；也沒有實際對 API 試過任何一條。
- **沒有讀任何 token 或 secret。** 對 GitHub 的請求全部是不帶憑證的唯讀 GET（公開 repo 的計數，以及 repo 公不公開）；
  文件裡沒有任何私有 repo 的名字，也沒有任何本機路徑。

---

## 附錄：數字從哪裡來

| 數字 | 怎麼量的 |
|---|---|
| §1.1 的表 | `cwds.py`：30 天內每份 transcript 的第一個 `cwd`；`final.py`：分類、把刪掉的 worktree 對回主 repo、排除自動化目錄；`public_split.py`：公開那一側有多少是 `sainteye/clawdline`。腳本在本任務的 `artifacts/` |
| §1.2 | 不帶憑證的 `GET api.github.com/repos/sainteye/clawdline` 與 `search/issues?q=repo:sainteye/clawdline+is:issue`／`is:pr`；commit 數取自 `commits?per_page=1` 的 `Link` 標頭的最後一頁 |
| §1.3 的 GitHub 上限 | docs.github.com〈Rate limits for the REST API〉、〈Best practices for using the REST API〉 |
| `closes #N` 的條件 | docs.github.com〈Linking a pull request to an issue〉、〈Closing an issue〉 |
| `state_reason`、draft issue、sub-issue、刪除權限 | docs.github.com〈REST API endpoints for issues〉、〈Adding items to your project〉、〈Adding sub-issues〉、〈Deleting an issue〉 |
| 三結構的規則 | `internal/domain/work/{board,proposals,todos}.go`、`internal/adapters/store/{work,proposals,todos}.go`、`internal/transport/http/{work,proposals}.go`，master `c0c545a` |
