# Session 身分：生命週期、告知、守衛

> 問的是：**一個 session 的「它是哪一段對話」，今天是每一趟盤點重新猜出來的純函數。**
> 使用者 2026-09-20 看著一個認不出來的 Codex session 說：「我們可能不是要單純解決問題，
> 而是要去思考我們的整個設計是否有問題。」看完分析後拍板 A／B／C 三個方向全做。
>
> 判準沿用 [`docs/design-decisions.md`](design-decisions.md) §1：(a) 使用者說過的話 >
> (b) 有量測的 > (c) 使用量為 0 的不投資 > (d) 沒有相容包袱時選簡單的。
> 原則是 [`docs/design-guidelines.md`](design-guidelines.md)，DG 回答在 §7。
>
> **這份文件只有設計，沒有改任何程式。** 量測於 2026-09-20 16:50–17:40，在使用者這台 Mac 上，
> codex-cli `0.155.1`、Claude Code registry schema 如 §1.5。base commit `d76cd6b`。
>
> **隱私**：文中不出現任何對話內容、對話標題、專案名稱或檔案路徑實例。
> 出現的只有**欄位名、旗標值、筆數、時間差**，以及 repo 自己的路徑。

---

## 0. 三十秒版本

| | 現況 | 判斷 | 決定 |
|---|---|---|---|
| 身分怎麼來 | 三條路全部是**這一瞬間的純函數**：argv、開檔表、Claude 的 registry | 它沒有記憶，所以**不可能比這一瞬間更確定** | **I1** 生命週期 |
| 證實過的身分 | 不存在任何地方，下一趟重算 | 讀不到一次＝名字掉一次 | **I2** `session_identities` 表 |
| 什麼能推翻它 | 沒有定義（每趟都是新的） | 「讀不到」「沒有」「變了」被壓成同一件事 | **I3** 只有正面證據 |
| 自己開的 child | broker 記 `ChildTerminalID`，**不記 conversation** | 開的那一刻知道最多卻不記 | **I4** 讓 child 自己簽回來 |
| 人手開的 | 沒有任何認領動作 | `ambiguous` 只能永遠沒名字 | **I5** 卡片上的認領 |
| 廠商檔案形狀 | **零個守衛** | 改了只會表現成「session 沒名字」 | **I6** 會紅的守衛 |
| 此刻這台機器 | 21 個 assistant session，**3 個沒有 id，全部是 Codex**，其中 2 個是 `ambiguous` | 基線 | §6 的驗收數字 |

---

## 1. 現況（全部複驗過）

### 1.1 三條路，與它們真正的位置

| Binding | 誰能用 | 程式 | 是什麼 |
|---|---|---|---|
| `command_line` | 兩家 | `internal/adapters/process/ps_unix.go:31` 的 `resumeID`，用在 `Scan` `:87` | `--resume <id>` / `resume <id>` 在 argv 裡 |
| `open_file` | 只有 Codex | `bindCodex` **定義在 `ps_unix.go:116`**，在 `Scan` 的最後一行 `:94` 被呼叫 | 行程開著自己的 rollout，id 在檔名 |
| `registry` | 只有 Claude | `transcript/claude.go:44` `ClaudeRegistryByPID` → `transcript/host.go:50` | `~/.claude/sessions/<pid>.json` |

沒有答案時的三種具名 nothing（`internal/domain/session/identity.go`）：`no_record`、`unreadable`、`ambiguous`。
這一組是「剛開的 Codex 認不出來」那一輪的交付（commit `aa980b6`），**是好的**，這份設計只加東西、不動它的語意。

**task 敘述的一處小誤，先更正**：`bindCodex` 的**定義**在 `ps_unix.go:116`，`:94` 是呼叫點。
其餘敘述複驗全部成立。

### 1.2 「每一趟盤點重算」到底重算了什麼

- `app.Inventory.Read`（`internal/app/inventory.go:58`）每次都先 `Identity.Refresh()`（`:66`），
  而 `transcript.Host.Refresh`（`host.go:36`）**重讀整個 `~/.claude/sessions/` 與整份 `session_index.jsonl`**。
- `Scan` 的最後無條件呼叫 `bindCodex`；只要有一列「codex 且沒有 id 且有 pid」，就跑一次 `lsof`。
- **`Binding` 與 `ConversationID` 沒有被寫進任何一張表。** 我把 `internal/adapters/store/*.go` 的
  `CREATE TABLE` 全部列出來對過（events、schedules、broker_tasks、broker_notices、coordinator、
  leases、waits、outbox、request_receipts、proposals、work、board_items、backlog、moves、
  snippets、todos、texts、handover 四張……），**沒有一張存身分**。task 的敘述成立。
- `orchestrator.Record`（`internal/app/orchestrator/record.go:350-352`）只有
  `ChildTerminalID`／`ChildBackend`／`RootTerminalID`。**root 用 conversation id 定址
  （`Root.SessionID`），自己開的 child 只用 terminal。** 這個不對稱就是 §3 的整個題目。

### 1.3 此刻這台機器的實況（基線）

按真正的規則（argv → bindCodex → enrich）重算，21 個 assistant session：

| assistant | 列數 | 結果 |
|---|---|---|
| claude | 16 | **16 個 `registry`**（`~/.claude/sessions/` 裡 16 筆記錄，對應 16 個活著的 pid，0 筆殘留） |
| codex | 5 | 1 `command_line`、1 `open_file`、1 `no_record`、**2 `ambiguous`** |

**3 個沒有 id，全部是 Codex，其中 2 個是 `ambiguous`。**

`ambiguous` 的結構原因量到了，而且它不是「隨便哪兩個」：

| pid | 開著的 rollout | 其中 `thread_source: user`、沒有 `parent_thread_id` 的 |
|---|---|---|
| A | 6（1 user＋2 `guardian_review`＋3 `subagent`） | **恰好 1** |
| B | 0 | — |
| C | 2（1 user＋1 `guardian_review`） | **恰好 1** |
| D | 1（user） | **恰好 1** |
| E | 2（1 user＋1 `guardian_review`） | **恰好 1** |

**五個行程裡，每一個都恰好開著一個「沒有父執行緒」的 rollout。** 這不是 tie-break 的猜測，
是廠商檔案自己寫的正面事實（詳見 §1.5）。pid A 之所以今天有名字，只是因為它是 `codex resume` 開的，
argv 先贏——**如果它不是 resume 開的，它會和 C、E 一樣是 `ambiguous`**。所以五個 codex 裡
**有三個處在 ambiguous 的形狀**，只是其中一個被 argv 救了。

### 1.4 穩定性：使用者的量測複驗成立，但結論要換句話說

12 次取樣、間隔 20 秒（共 4 分鐘），對 21 列的 binding：**0 列改變**。
使用者說的「沒有抖動」成立。

但同一份取樣還量到另一件事：**21 列裡有 2 列在這 4 分鐘內出現或消失**（只出現在 4／12 與 5／12 次取樣裡）。
所以「身分穩定」與「列表穩定」不是同一句話——**穩定的是答案，不穩定的是有沒有人來問。**
這正是要記憶的理由：不是因為它會跳，是因為**它一旦讀不到就沒有第二個來源**。

### 1.5 廠商檔案今天的形狀（守衛要釘的東西）

`~/.codex/sessions/<yyyy>/<mm>/<dd>/rollout-<timestamp>-<id>.jsonl`，第一行是
`{"type":"session_meta","payload":{…}}`。今天（12 個檔）的 payload 欄位集合有三種：

| 出現次數 | 欄位集合 |
|---|---|
| 7 | `base_instructions, cli_version, context_window, cwd, git, history_mode, id, model_provider, originator, runtime_workspace_roots, session_id, source, thread_source, timestamp` |
| 3 | 同上 ＋ `multi_agent_version`, `parent_thread_id` |
| 2 | 同第一列但**沒有 `git`** |

三個不變量，12／12 成立：

1. **檔名裡的 id 永遠等於 `payload.id`。**
2. **主執行緒**：`id == session_id`，沒有 `parent_thread_id`，`thread_source: user`、`source: cli`。
3. **子執行緒**：`payload.session_id == payload.parent_thread_id`（＝父的 id），`id != session_id`，
   `thread_source` 是 `guardian_review`、`source` 是 `{"subagent":{"other":"guardian"}}`，
   而且父的 rollout 確實在磁碟上。

第一行大小：中位數 18,724 bytes（`base_instructions` 佔大部分）。
讀＋parse 一個檔的第一行：p50 **0.057 ms**、max 0.46 ms（暖快取，n=12×10）。

Claude 那邊，`~/.claude/sessions/<pid>.json` 今天有 **20 個欄位**：
`pid, sessionId, cwd, startedAt, procStart, version, peerProtocol, peerFeatures, kind, entrypoint,
pidDomain, tmux, messagingSocketPath, name, nameSource, nameSince, updatedAt, status, statusUpdatedAt,
bridgeSessionId`。
`internal/adapters/transcript/claude.go:25` 的 `ClaudeRegistry` **只讀其中 7 個**。
同一個目錄裡還有 16 個 `<pid>.<64位hex>.key` 檔——**同一個目錄的第二種檔名形狀**，
今天被 `strings.HasSuffix(e.Name(), ".json")` 濾掉了，但沒有任何東西盯著它變成第三種。

**沒讀的欄位裡有一個很重要：`procStart`。** 它就是 §2 要用的那把鎖。

### 1.6 一個複驗時查到、task 沒有的缺陷：`lsof` 其實沒有限定 pid

`openfiles_darwin.go:35` 的命令是

```
lsof -w -n -P -F pn -d ^txt,^cwd,^rtd -p <逗號清單>
```

`lsof` 的選擇條件預設是 **OR**，要 AND 必須加 `-a`。所以 `-d` 一旦出現，`-p` 就不再限縮：
它實際列出的是**整台機器**。實測（同一組 5 個 pid，各跑 5 次）：

| 形式 | p50 | max | 輸出 | 答案裡的 pid 區塊 |
|---|---|---|---|---|
| 現行（`-d …` `-p …`） | **0.179 s** | 0.207 s | **552 KB** | **628** |
| 加 `-a` | 0.095 s | 0.162 s | 11 KB | 5 |
| 只有 `-p`（沒有 `-d`） | **0.030 s** | 0.077 s | 14.7 KB | 5 |

**答案是對的**——`parseLsof` 用 `p<pid>` 分塊，`files[s.PID]` 取到的仍然只有那個 pid 的檔。
錯的是代價與範圍：每一趟有未命名 Codex 的盤點，都在讀全機每一個行程的描述子表，
比只問 5 個 pid 貴約 **6 倍**。`aa980b6` 那一輪的報告量到「單一 pid 約 25 ms」，
和這裡的 `-p` only 30 ms 吻合——**它量的不是實際送出去的那條命令**。

這一行在 `internal/adapters/process`，**此刻被另一個在途 task 握著**（就是下面 W1b 那一個），所以這份文件只記錄，不修。
它是 §5 W0 的一行修正，也是 §8 的交辦項目。

---

## 2. A — 身分要有生命週期

### 2.1 問題

今天的身分是 `f(這一瞬間的機器狀態)`。它的失敗形狀只有一種，但發生得很頻繁：

- `lsof` 讀不到（權限、逾時、context 被取消）→ 整台機器的 Codex 全部變 `unreadable`，名字全掉。
- Codex 把 rollout 的 fd 關掉（我們沒有任何保證它不會）→ `no_record`，名字掉。
- 多開了一個子執行緒 → `ambiguous`，名字掉。
- 使用者關掉又打開 iTerm、daemon 重開 → 從頭再猜一次。

**一個證實過的事實，被當成一個可以隨時忘記的觀察。** 這違反 DG-4 的分類：
「這個行程正在進行的是哪一段對話」是**事實**（它發生過、被證實過、不能重算出來），
不是可以重算的觀察。

### 2.2 選項

| | 做法 | 為什麼不選 |
|---|---|---|
| **A1** | 快取：記住上一趟的答案，N 秒內不重問 | 只省成本，不解決問題。N 秒之後一樣掉。而且「過期」是時間，時間不是證據（DG-7） |
| **A2** | 記憶體裡記住，永遠不主動忘 | daemon 一重開全沒了；而且沒有地方記「為什麼它被推翻」，事後查不出來 |
| **A3**（選） | **落盤的身分記錄，鍵是「這個行程」，只有正面證據能推翻** | 見下 |
| **A4** | 把身分寫進 assistant 自己的檔（例如在 rollout 旁邊放一個我們的檔） | 在別人的目錄裡寫東西；廠商一改就變垃圾；而且它解決不了「誰是誰」，只是換個地方猜 |

### 2.3 決定 I1／I2：狀態、鍵、與轉換

**鍵是「這個行程」，不是「這個分頁」，也不是「這個 pid」**：

```
(terminal_id, pid, proc_start) → (assistant, conversation_id, source)
```

`proc_start` 是 kernel 說的行程起始秒數。repo 裡已經有這個讀取器：
`internal/adapters/swiftstore/procstart_darwin.go` 的 `ProcessStart(pid)`，
以及既有的比對規則 `sameStart`（容差 5 秒，`records.go:293`）與
`Identity.Matches`（`records.go:319`，六個事實全中才算數）。
**這條規則不是新發明的，是把 Swift app 已經在用、這個 repo 已經照搬的那條，
從「比對 Swift 的舊紀錄」擴大到「比對我們自己證實過的身分」。**（DG-9：沿用有今天的證據。）

狀態只有三個，外加「沒有記錄」：

| 狀態 | 意思 | 進入條件 | 離開條件 |
|---|---|---|---|
| （無記錄） | 從來沒證實過 | — | 任一正面讀數 → `confirmed` |
| `confirmed` | 這一趟有正面讀數，而且和記錄一致 | 讀到相同 id | 見下表 |
| `remembered` | 這一趟沒有正面讀數，但**行程還是同一個** | `(pid, proc_start)` 仍然對得上，且這一趟沒有反證 | 見下表 |
| `retired` | 這個行程不在了，或換人了 | 見下表 | 永不再服務；保留供稽核 |

**轉換表——「讀不到」「沒有」「變了」是三個不同的字（DG-7）：**

| 這一趟發生的事 | 對記錄做什麼 | 對外答什麼 |
|---|---|---|
| 讀到了，id 相同 | 更新「本趟已證實」（記憶體） | `confirmed` |
| **讀不到**（`lsof` 失敗／registry 目錄讀不到） | **不動** | `remembered`，`binding` 仍是原來的來源 |
| **讀得到但這個 pid 沒有東西**（`no_record`） | **不動** | `remembered` |
| **讀到了，而且答案是另一個 id** | 改寫成新 id，舊的寫成 `superseded` 事件 | `confirmed`（新的） |
| **讀到了候選，而記錄的 id 不在候選裡**（含 `ambiguous`） | **推翻**：記錄 `retired(reason=not_among_candidates)` | 退回這一趟的答案（可能是 `ambiguous`） |
| `ProcessStart(pid)` 回零（kernel 說沒有這個 pid） | `retired(reason=process_gone)` | 這一列本來就不會出現 |
| `ProcessStart(pid)` 與記錄差 > 5 秒 | `retired(reason=pid_reused)` | 不服務舊 id |
| daemon 重開 | 不動（記錄在 SQLite） | 第一次看到這個 pid 時先驗 `proc_start`，不過就 `retired` |

**沒有時間過期。** 理由寫清楚：一個開著三天沒人動的 session，它的身分**沒有變得比較不真**。
時間不是證據；只有上面那四種正面事實能推翻它。代價在 §2.6。

### 2.4 存在哪裡

`internal/adapters/store`（`clawdline.sqlite3`，D02：一個 store）：

```sql
CREATE TABLE IF NOT EXISTS session_identities (
  terminal_id  TEXT    NOT NULL,
  pid          INTEGER NOT NULL,
  proc_start   INTEGER NOT NULL,
  assistant    TEXT    NOT NULL,
  conversation TEXT    NOT NULL,
  source       TEXT    NOT NULL,   -- command_line|open_file|registry|reported|claimed
  first_seen   INTEGER NOT NULL,
  retired_at   INTEGER NOT NULL DEFAULT 0,
  retired_why  TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (terminal_id, pid, proc_start)
);
```

**刻意沒有 `confirmed_at` 這一欄。** 「最後一次證實是什麼時候」是**觀察**，
每 10 秒改寫一次它就是 `broker-design.md` §2.2 那個 `executor.observed_at` 的缺陷重演
（315 秒 30 次整份重寫換 +90 B）。所以：

- 落盤的只有**事實**：建立、改變、退役。**一個身分在整個 session 生命期內只寫 1–2 次。**
- 「這一趟有沒有重新證實」活在記憶體裡，隨 daemon 的生命週期。
- wire 上給的是 `first_seen`（事實，可落盤）＋ `identity_state`（本次讀數的結論）。
  daemon 剛重開時每一列都是 `remembered`，直到各自被再證實一次——**這是誠實的，不是缺陷**。

**上限登記（DG-2，`internal/domain/capacity`）：**

| row | 類別 | 上限 | 到頂時 | 誰被告知 | 誰決定淘汰 |
|---|---|---|---|---|---|
| `store.session_identities` | `journal` | 5,000 列 | **淘汰最舊的已 `retired` 列**；若全部都是活的 → `refuse` ＋ `/v1/health` `capacity_exhausted` | diagnostics 永遠有；到頂時 health | 機器（只淘汰 retired），活列絕不淘汰 |

理由：一台機器上活著的 assistant session 是被機器本身限制住的（今天 21 個）。
5,000 列全是活的代表發生了別的事，那時候「拒絕」才是對的答案。

### 2.5 一個被留著的舊身分，什麼時候會變成**錯的**答案

這是這個方向**唯一的真風險**，正面回答，不說「應該不會」。四種：

**(1) 同一個行程換了對話。** Claude 的 `/clear`、Codex 的新 thread，pid 與 `proc_start` 都沒變，
conversation 變了。

- **Claude**：registry 每一趟都讀得到，下一趟就走「答案是另一個 id」→ 自動改正。
  最壞暴露 **1 個 tick**。
- **Codex**：**這是最危險的窗。** 新對話的 rollout 要等第一則訊息才存在，
  而舊 rollout 的 fd 可能還開著。如果沒有規矩，`remembered` 會一直答舊的，而且沒有任何東西反駁它。
  **所以上表最後一條規則是必要的，不是保險**：只要這一趟讀到了任何候選，
  而記錄的 id 不在候選裡，就推翻——哪怕結果是退回 `ambiguous`。
  **「沒有名字」比「穿著別人的名字」便宜得多**，這一句是 `identity.go` 已經寫下的判斷，這裡只是延伸它。
  殘餘窗口：舊 fd 還開著且新的還沒出現的那一段，記錄仍在候選裡，仍答舊的。
  **這一段是關不掉的**，除非 Codex 上游在啟動時就寫 rollout（`aa980b6` 那一輪的報告 §4 同一個結論）。

**(2) pid 重用。** 需要同一個 pid、同一個 tty、同一種 assistant、而且 `proc_start` 落在同一個
5 秒窗內。macOS 的 pid 空間要繞一圈才會重複。**殘餘風險非零，而且我不打算宣稱它是零。**
Claude 那邊有額外保險：registry 檔自己就帶 `procStart`，可以不靠 kernel 直接對；
Codex 那邊沒有等價物，只能靠 kernel。

**(3) terminal 重用。** 同一個 tty 換新 session → pid 一定不同 → 鍵對不上 → 不會服務舊記錄。**安全。**

**(4) daemon 重開後的第一瞬間。** 記錄還在，但還沒驗 `proc_start`。
**規則：任何一列在被服務之前，必須先通過 `proc_start` 驗證**；驗不過就 `retired`。
機器重開後所有 pid 都是新的，所以舊記錄會在第一次被看到時全部退役——**不需要開機清空**，
正確性由鍵本身保證，清空只是回收。

### 2.6 代價

1. **多了一張表、一個狀態機、一組轉換規則。** 這是這份設計最貴的一塊，也是 B 與 C 的地基。
2. **`remembered` 是一個新的、會出現在畫面上的字。** 使用者會看到「它記得，但這一趟沒再確認」。
   不把它藏起來是刻意的（DG-7）：藏起來就等於宣稱「現在確認過」。
3. **沒有時間過期 ⇒ 一個 `lsof` 永久失效的機器，會永遠答 `remembered`。**
   這是正確的（沒有任何東西推翻它），但它會讓一個真正壞掉的機器看起來只是「沒再確認」。
   **補償**：wire 上帶 `identity_state` 與 `first_seen`，`unreadable` 本身仍然照舊出現在
   diagnostics 的讀數裡，所以「這台機器讀不到開檔表」有它自己的說法，不靠身分這條線講。
4. **寫入次數**：每個 session 生命期 1–2 次，daemon 空轉時是 0（DG-4 的驗收：briefed 但沒有狀態變化時寫入接近 0）。

### 2.7 怎麼驗

| 實驗 | 做法 | 控制組（要看到它紅） |
|---|---|---|
| 讀不到不該掉名字 | 注入一個回 `(nil,false)` 的 `OpenFiles` 一趟，斷言列仍有 id 且 `identity_state=remembered` | 同樣注入跑在今天的程式上 → 名字變 `unreadable`，沒有 id |
| 換了對話要改正 | 餵一趟「唯一且不同」的 id，斷言一趟之內改掉，並留下 `superseded` 事件 | 拿掉「答案不同」那條轉換 → 測試紅 |
| 不在候選裡要推翻 | 記錄 id = X，這一趟候選是 {Y, Z}，斷言退回 `ambiguous`，**不**答 X | 拿掉這條 → 測試紅（而且它紅的就是「穿著別人的名字」那個形狀） |
| pid 重用 | 同 pid、`proc_start` 晚 6 秒，斷言 `retired(pid_reused)` 而不是服務舊 id | 把容差改成無限大 → 測試紅 |
| daemon 重開 | 寫入記錄、關掉、開起來、第一趟先驗 `proc_start` | 跳過驗證 → 機器重開的情境下會服務全部舊記錄，測試紅 |
| 寫入量 | 5 個 session 開著不動 120 秒，斷言 `session_identities` 的寫入次數 = 0 | 把 `confirmed_at` 加回表裡 → 立刻不是 0 |

---

## 3. B — 自己開的不要猜，手開的讓人認領

### 3.1 B-i：自己開的 child，開的時候拿得到什麼？（先查，再設計）

**broker 在開 child 的那一刻只拿到 terminal id。**
`dispatch.go:616` 記下 `ChildTerminalID` 與 `ChildBackend`，之後所有事情都靠
`sessionByTerminal`（`broker.go:805`）用 terminal 去掃描結果裡撈。**沒有 pid，沒有 conversation。**

**Codex 的 conversation id 是啟動時決定還是第一則訊息才有？**
`aa980b6` 那一輪已經量過，我複驗它的結論並補了幾個新事實：

- rollout 是**第一則訊息**才寫（七個樣本差 4–88 秒，當天三個 thread 完全沒有 rollout）。
- 沒有 `~/.claude/sessions/<pid>.json` 的等價物：`thread-writer-locks` 建完就關 fd、不帶 pid、
  而且一次建兩個；`state_5.sqlite` 的 `threads` 沒有 pid／tty 且要第一則訊息後才出現；
  status bar 上的 id 一有 thread name 就消失。

**但對「我們自己開的 child」而言，這個空窗其實已經很小了**——因為 **broker 自己就是打第一則訊息的人**
（`dispatch.go` 的 `brief()` 把 briefing 打進去）。訊息送出後 rollout 的 fd 在 **0.02 秒**內出現。
所以 B-i 的真問題不是空窗，是：**答案出現之後，我們從來沒有把它記下來，
於是這個 child 接下來一整輩子都要靠 `lsof` 重猜，而且會掉進 `ambiguous`**（§1.3 的 C 與 E 就是這個下場）。

**還有一條更強的路，而且 repo 已經在用了。**
`cmd/clawdline/session.go:28`：

```go
var conversationEnv = []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "CODEX_SESSION_ID"}
```

我在這個 session 裡實際確認：**`CLAUDE_CODE_SESSION_ID` 與 `CLAUDE_PID` 確實存在於
Claude Code 執行的指令的環境裡。** 也就是說 **child 自己知道自己是誰**，
而且它已經有一個**帶著 task secret 的、經過認證的**回報管道：`POST …/accepted`。

而且 `internal/adapters/projects/launch.go:115-120` 已經在啟動 child 時把這些變數
`env -u` 掉了——所以 child 看到的那個值**只可能是它自己的**，不可能是從父 session 繼承來的。
**這個保證已經上線了，不是這份設計要新建的。**

**Codex 那一半我不敢照抄 repo 的說法。** `session.go:23` 的註解說 Codex 以 `CODEX_THREAD_ID` 匯出，
但那行註解裡有日期的量測（2026-09-19）是 **Claude 那一半**的。我對已安裝的
`codex-cli 0.155.1` 原生 binary 抽 strings：`CODEX_THREAD_ID` 出現 **4 次**，其中一次
出現在一份看起來是**環境變數過濾清單**的字串裡（鄰居是 `SHELL`、`TMPDIR`、`LC_ALL`、
`*KEY*`、`*TOKEN*`、`CODEX_EXEC_SERVER_NOISE_AUTH_TOKEN`）。
**過濾清單既可能是「要傳進去」也可能是「要濾掉」，從字串看不出來。**
所以這是一個**具名的未知**，不是假設，實驗在 §5 W0①（一行：在一個 codex session 裡 `env | grep CODEX_`）。

### 3.2 決定 I4：child 自己簽回來，拿不到就退回掃描

**`POST /v1/orchestrator/tasks/<id>/accepted` 的 body 多兩個可選欄位**：

```json
{"conversation_id": "<CLAUDE_CODE_SESSION_ID 或 CODEX_THREAD_ID>", "pid": 12345}
```

broker 收到時：

1. 用 task secret 認證（已經有了）。
2. 檢查這個 task 的 `ChildTerminalID` 此刻的那一列是不是宣告的那種 assistant。
3. 以 `(terminal_id, pid, proc_start)` 寫一筆 `source: reported` 的身分記錄（§2.4 的表）。
4. **同時把它記進 `Record`**：`ChildConversationID`，補上 root／child 的不對稱。

拿不到就**不送這兩個欄位**，`accepted` 的語意一個字都不變（它現在就允許空 body）。
Codex child 若 W0① 證明拿不到，退回原本的路：broker 打了 briefing，0.02 秒後 fd 出現，
`open_file` 綁上，而 `ambiguous` 由 W1b 的結構規則解掉（§1.5 的不變量 2／3，守衛在 §4 的 C4–C6）。
**所以 B-i 沒有任何一條路是「猜」。**

**沒有選的兩條路，與為什麼：**

- **預先鑄一個 id，用 `codex resume <新 uuid>` 開**：那會是最強的 `command_line`，從 t=0 就有名字。
  但 binary 自己的字串說 `resume` 是恢復**既有**執行緒
  （`reserved thread ID cannot be used when resuming a thread`、
  `codex fork --worktree requires an explicit session ID`），看起來鑄不出來。
  **我沒有實際跑**（跑它會在使用者機器上留下一個游離的 codex session），所以這是 W0② 的實驗，
  **如果成立，它比 I4 更好，B-i 整段可以收掉。**
- **broker 開完之後自己去 `lsof` 那個 pid 記下來**：這只是把同一個猜測搬到更早的時間點，
  而且 `ambiguous` 一樣會發生。它不比 child 自己說更強，卻多一條路徑要維護（DG-5）。

### 3.3 決定 I5：人手開的，在卡片上認領

**候選從哪裡來——只從「關於這一列本身」的正面事實，永遠不從全機清單。** 兩個來源，依序：

1. **這個行程此刻開著的 transcript**（`ambiguous` 的那 2–6 個）。這是最好的來源：
   它們已經被 kernel 指認為「這個行程手上的檔」。
2. 當來源 1 是空的（`no_record`）：同一種 assistant、記錄**建立時間晚於這個行程的 `proc_start`**、
   且 `cwd` 與這一列相同的對話，取最近的 **N=5** 筆。

**而且兩個來源都要再減一次：已經被別的活著的列綁走的對話，不進候選。**
（這一條只有在 A 落地之後才做得到——這是 A 與 B 的相依。）

**每個候選給人看什麼（隱私）：**

| 給看 | 不給看 |
|---|---|
| 對話**自己已經有的名字**（Codex 的 `thread_name`、Claude 的 `customTitle`／`aiTitle`）——就是列表現在已經在畫的同一個字串 | 任何一則訊息、第一句話、prompt 預覽、diff |
| 開始時間、最後一次動的時間（`session.Activity`，已經有了） | 完整路徑 |
| 往返次數／紀錄大小 | 對話內文的任何摘要 |
| 專案：列表現在已經在畫的 icon ＋ basename | |
| Codex：是不是子執行緒（有 `parent_thread_id`）——**而且子執行緒預設不列為候選** | |

沒有名字的候選就顯示座標：「未命名 · 17:02 開始 · 12 次往返」。
**它是座標，不是內容**——和 `session.Coordinate` 的判斷一致。

**動作**：`POST /v1/sessions/{id}/identity`，走既有的 `sessionVerb`（`actions.go:33`）形狀，
需要 `maySend`（它改變一則訊息會被送到哪裡，是寫入不是讀取）。Body：

```json
{"conversation_id": "…", "candidates_fingerprint": "…", "request_id": "…"}
```

- `candidates_fingerprint` 照 `session.MenuFingerprint`（`fingerprint.go:32`）的辦法：
  **認領要指名它當時看到的那份候選清單**。清單變了就 `409 candidates_moved`，
  絕不讓一個舊畫面上的選擇落在新清單上。
- 冪等走既有的 `request_receipts`（D03），scope `identity`。

**認領之後算哪一種 Binding**：新的 `source: claimed`。
**不是**在 `session.Binding` 裡加第七個值——`Binding` 回答的是「怎麼拿到的」，
`identity_state` 回答的是「這一趟有沒有再確認」，兩個問題兩個欄位（DG-5）。
`claimed` 是 `Bound()` 為真的第四個來源。

**認錯了怎麼改**：同一條路再送一次別的候選就取代它；送 `conversation_id: ""` 是撤回
（`retired(reason=withdrawn)`），該列退回掃描說了算。每一次都是一筆事件，查得到。

**認領會不會被下一趟掃描蓋掉**——優先順序寫成一條，只有一份：

```
command_line  >  claimed  >  reported  >  registry / open_file  >  (remembered: 上面任一個，這一趟沒再確認)
```

但**排在後面不等於推翻不了**。§2.3 的轉換表對 `claimed` 一視同仁：
**如果這一趟讀到了唯一而明確的、不同的 id，`claimed` 會被 `superseded`，而且卡片上要說一句
「你認領的那一段，機器後來讀到了別的」。** 理由講白：

> 認領只在機器答不出來的地方有意義。一個能把認領釘在 kernel 說的事實之上的設計，
> 就是一個能讓某個 session 永遠穿著別人名字的設計。

代價是：使用者認錯之後，可能會看到自己的認領被機器改掉。**這是刻意的，而且會被告知。**

### 3.4 代價

- **`accepted` 的 body 長出兩個欄位**，contract 要改（`api/v1/orchestrator.schema.json`）。
  舊的 child（沒有帶欄位的）完全照舊——**這是可選的加法，不是協定變更**。
- **多一個人會按的動作**，就多一條要防的路：fingerprint、收據、`maySend`、
  以及「認領一個已經屬於別人的對話」要被拒絕（`409 conversation_taken`）。
- **候選清單會讓人看到「這台機器上還有哪些對話」的一小部分**。範圍已經收到
  「這個行程手上的檔」＋「這個行程開始後、同一個 cwd、最近 5 筆」，但它不是零。
  這是為了讓人分得出來所付的價，寫在這裡讓使用者能否決。

### 3.5 怎麼驗

| 實驗 | 控制組 |
|---|---|
| 派一個 Codex child、一個 Claude child，量「開分頁」到「列上有 id」的秒數，改前改後 | 改前的數字就是控制組（Codex 今天靠 `lsof`，而且可能停在 `ambiguous`） |
| 手開一個 Codex 進入 `ambiguous`，認領它，斷言列上有名字；**重開 daemon**，斷言還在 | 沒有 A 的表 → 重開就沒了，測試紅 |
| 認領之後餵一個不同的、唯一的 `command_line` 讀數，斷言認領被 `superseded` 且卡片上有那句話 | 把 `claimed` 做成不可推翻 → 這個測試紅，而且它紅的就是「永遠穿著別人名字」 |
| 拿一份舊的 `candidates_fingerprint` 送認領 → `409` | 不帶 fingerprint → 舊畫面的選擇會落在新清單上 |
| 候選清單裡不含已被別列綁走的對話 | 拿掉這個過濾 → 兩列可以認領同一段對話 |

---

## 4. C — 廠商檔案是不穩定介面，要有會紅的守衛

### 4.1 現況

**今天沒有任何東西盯著 `~/.codex` 與 `~/.claude` 的形狀。**
`openfiles_test.go`、`identity_test.go`、`lsof_darwin_test.go` 釘的是**我們自己的 parser**
（給這些 bytes，答這個答案）——它們**永遠不會**因為 Codex 改了檔案而變紅，因為那些 bytes 是我們寫的。

它壞掉時的症狀是：**session 沒有名字**。使用者會先發現，我們不會。
這正是 DG-8 的「passed ≠ never tested」。

repo 裡已經有兩個對的先例，形狀直接照抄：
- `tools/check-legacy-css.sh`：三個答案（0 相符／1 漂移／2 查不了），
  而且最後一行是 `checked nothing; a zero-file run is a failure, not a pass`。
- `tools/check-private/main.go:97`：`read no files; a run that read nothing is not a pass`。
- `internal/contract/namespace_test.go`：一張**必須逐項分類**的表，新東西沒分類就紅，
  舊東西消失了也紅。

### 4.2 決定 I6：釘什麼

不是釘「檔案長怎樣」，是釘**每一個我們的程式依賴的判斷所根據的那件事**。
每一條都寫出「它悄悄不成立的時候，症狀是什麼」——因為那才是守衛存在的理由。

| # | 斷言 | 依賴它的程式 | 它悄悄壞掉時的症狀 |
|---|---|---|---|
| C1 | rollout 路徑形如 `sessions/<yyyy>/<mm>/<dd>/rollout-<ts>-<id>.jsonl`，`.jsonl` 前最後 36 字是 id，前一個字是 `-` | `codexRolloutID`（`openfiles.go:69`） | 全部 Codex 變 `no_record` |
| C2 | 第一行是 `type == "session_meta"` 的 JSON，`payload` 至少有 `id, session_id, cwd, originator, source, thread_source, cli_version` | §4 的結構規則、候選清單 | 候選分不出主從 |
| C3 | `payload.id` **等於檔名裡的 id** | 同上（兩個來源交叉驗證） | 綁到別的 thread |
| C4 | 主執行緒：`id == session_id` 且**沒有** `parent_thread_id` | ambiguous 的結構解法 | `ambiguous` 回來，或更糟：綁到子執行緒 |
| C5 | 子執行緒：`parent_thread_id == session_id != id` | 同上 | 同上 |
| C6 | **「可以把一個 thread 標成不是它自己的」的欄位集合，恰好是 `{parent_thread_id, forked_from_id}`** | C4／C5 的完整性 | **出現第三種衍生執行緒時，它會被當成主執行緒** |
| C7 | 活著的 codex 行程持有自己 rollout 的 fd | `bindCodex` 全部 | 全部 Codex 變 `no_record` |
| C8 | `session_index.jsonl` 每列有 `id` 與 `thread_name` | `CodexNames`（`codex.go:13`） | Codex 列沒有名字（有 id 但只剩座標） |
| C9 | `~/.claude/sessions/<pid>.json` 存在於每個活著的 Claude session，帶 `pid, sessionId, cwd, tmux, status, name, version` | `ClaudeRegistry`（`claude.go:25`） | 全部 Claude 變 `no_record` |
| C10 | 該目錄的檔名形狀恰好是 `{<pid>.json, <pid>.<hex>.key}` | `ClaudeRegistryByPID` 的 `.json` 過濾 | 第三種形狀被誤讀成 session 記錄 |
| C11 | `status` 的值落在 `{busy, idle, waiting}` 裡 | `StateFromAssistantStatus`（`session.go:61`） | **新的狀態字被靜靜地壓成 `unknown`** |
| C12 | `CLAUDE_CODE_SESSION_ID` 存在於 Claude 執行的指令的環境中，且等於該 pid 記錄裡的 `sessionId` | I4（child 自己簽回來）、`clawdline session report` | child 報不出自己是誰，或報錯 |
| C13 | 〔W0① 之後〕Codex 對應的那一條 | I4 的 Codex 那一半 | 同上 |

**C6 是這張表裡最重要的一條，而且是複驗時才長出來的。**
從已安裝 binary 的 `SessionConfiguredEvent` 欄位表裡可以看到 `forked_from_id` 與
`parent_thread_id` 並存。今天的規則只認得 `parent_thread_id`；
**一個 fork 出來的 thread 會通過「沒有 parent」的檢查，被當成主執行緒。**
守衛要釘的不是「有沒有 `parent_thread_id`」，是**「能讓一個 thread 不是它自己」的那個集合有沒有變大**。
（這正是家規講的：寫下判斷所依據的基礎，不要只封住被抓到的那一種拼法。）

### 4.3 對什麼跑：兩層，而且兩層的差別是整件事的重點

| 層 | 跑什麼 | 在哪跑 | 它能發現什麼 | 它**不能**發現什麼 |
|---|---|---|---|---|
| **L1 fixture** | `internal/adapters/*/testdata` 裡我們寫的 bytes | `go test ./...`，每台機器、CI | 我們的 parser 改壞了 | **廠商改了**——因為那些 bytes 是我們寫的 |
| **L2 真機** | 已安裝 CLI 的**真實檔案** | `tools/check-vendor-shapes.sh`，release candidate 的檢查清單＋`clawdline doctor` | 廠商改了 | 沒裝 CLI 的機器上什麼都發現不了 |

**兩層要互相綁住**，照 `check-legacy-css.sh` 的兩個方向：

- L2 報「真實檔案出現了任何 fixture 都沒有涵蓋的形狀」；
- L2 也報「某個 fixture 的形狀在真實檔案裡**一個都找不到了**」——**這一個才抓得到移除**，
  而移除正是最安靜的那種改變。

**manifest**（照 `web/console/src/legacy/MANIFEST.json` 的先例）記下
每條斷言最後一次驗過的 CLI 版本。**版本動了而 manifest 沒動，是一個 finding（exit 1）**，
理由是「廠商出了新版而沒有人重驗」正是這個守衛存在的狀態。
代價：自動更新 CLI 的機器上它會常紅。**這是刻意的**；補償是守衛會把該改的那一行原封不動印出來，
而且同一次就把斷言重跑完，所以「改 manifest」是三十秒且安全的動作。

### 4.4 機器上沒裝那個 CLI 時

三個答案，第三個不是綠的：

| exit | 意思 | 什麼時候 |
|---|---|---|
| **0** | 通過 | 每一條能跑的都跑了、都過了，**而且至少跑了一條真實檔案的斷言** |
| **1** | 有發現 | 形狀變了／不見了／版本動了／`status` 出現沒對應的字／出現沒分類的檔名形狀 |
| **2** | **查不了** | CLI 沒裝、目錄讀不到、`lsof` 不能用 |

三條規則，每一條都對應一次真的事故：

1. **零斷言＝失敗，不是通過。** 一條真實檔案的斷言都沒跑成，就不准回 0
   （`check-legacy-css.sh` 與 `check-private` 的同一句話）。
2. **exit 2 不是綠的。** 任何把 2 當成成功的 CI，就是把缺陷重新引進來。
   `--require codex,claude` 讓「該有卻沒有」變成 exit 1；**這台 Mac 用 `--require`**，
   一台全新的 Linux box 用預設。
3. **沒有人看的檢查等於沒有檢查**（DG-1：「Nothing made anybody look」）。
   所以 `--record` 會把最後一次結果寫進 store 一列，`/v1/diagnostics` 顯示它**和它的年齡**——
   **舊的結果讀起來要像「舊的」，不能像「綠的」。**
   不進 `/v1/health`：廠商換了檔案的形狀不是這個 daemon 掛了，而 health 是講掛掉的。

### 4.5 怎麼驗守衛自己會紅（DG-8）

| 控制組 | 期望 |
|---|---|
| 把 fixture 裡的 `parent_thread_id` 改名成 `parentThreadId` | exit 1，而且訊息指名 **C4/C5/C6** |
| 在 fixture 裡加一個帶 `forked_from_id`、沒有 `parent_thread_id` 的 thread | exit 1，指名 **C6**（今天的規則會把它當主執行緒） |
| 把 Claude fixture 的 `status` 改成一個沒對應的字 | exit 1，指名 **C11**（今天是靜靜地變 `unknown`） |
| 把 `CODEX_HOME` 指到一個空目錄 | **exit 2**，訊息含「checked nothing」，**絕不是 0** |
| 同上但加 `--require codex` | **exit 1** |
| manifest 的版本停在舊版、機器上是新版 | exit 1，並印出該改的那一行 |

### 4.6 代價，與沒有選的三條路

**代價：**

1. **會常紅。** CLI 自動更新的機器上，版本一動就是一個 finding。這是刻意的（§4.3），
   代價是每次升版要花三十秒確認並改一行 manifest。
2. **L2 只在裝了 CLI 的機器上有意義。** CI 的 Linux box 永遠回 2，所以它在 CI 裡**不是一道關卡**，
   只是一筆記錄。真正的關卡在 release candidate 的檢查清單上，而那是在這台 Mac 上跑的。
3. **守衛讀的是使用者真實的對話檔。** 它只讀**第一行**、只取**欄位名**，
   任何輸出都不含 payload 的值（`cwd`、`git`、`base_instructions` 一律不印），
   而且它自己也要過 `tools/check-private.sh`。

**沒有選的三條路：**

| | 為什麼不選 |
|---|---|
| **只做 fixture（L1）** | 它永遠不會因為廠商改了而紅——那正是今天的狀態，等於什麼都沒加（DG-8：passed ≠ never tested） |
| **讓 daemon 定期跑守衛** | 那是一個新的背景迴圈，要回答 DG-1（停了誰知道）與 DG-2（累積什麼），成本比它解決的問題大。守衛是**一個檢查**，不是一個服務 |
| **把守衛的結果放進 `/v1/health`** | health 是講「這個 daemon 掛了」。廠商換了檔案形狀時 daemon 好得很，把它塗紅會稀釋 health 的意思（DG-5：一個概念一種拼法） |

---

## 5. 分波與相依

| 波 | 內容 | 相依 | 為什麼在這個位置 |
|---|---|---|---|
| **W0** | 三個一行的實驗＋一行修正：① codex 執行的指令的環境裡到底有沒有 `CODEX_THREAD_ID`；② `codex resume <全新 uuid>` 會建立還是報錯；③ `lsof` 加 `-a`（§1.6） | 無 | **①② 會改變 W2 的形狀**，而且各只要一分鐘。③ 是已量到的 6 倍代價，一行 |
| **W1** | A 的全部：`session_identities`、狀態機、`proc_start` 驗證、capacity 一列、wire 上的 `identity_state` 與 `first_seen` | 無 | **它是 B 與 C 的地基**：候選要排除已綁走的、C4 的「一個行程一個主執行緒」要有記憶才驗得出來 |
| **W1b** | ambiguous 的結構解法（§4 的 C4／C5／C6） | 與 W1 同一次交付 | **一個在途的 task 正在做這一條**（標題是「自己開的 Codex 認不出來：子執行緒讓身分變成 ambiguous」，宣告的寫入範圍是 `internal/adapters/process`、`internal/domain/session`、`internal/adapters/transcript`）。W1b 的工作是**讀它的交付、把 C6 補上**，不是重做 |
| **W2** | B-i：`accepted` 帶 `conversation_id`／`pid`，broker 記 `reported`，`Record` 補 `ChildConversationID` | W1（要有地方記）、W0①② | 自己開的比手開的常見，而且它不需要任何 UI |
| **W3** | B-ii：候選來源、`/v1/sessions/{id}/identity`、fingerprint、收據、console 一個動作 | W1、W2 | 要有 UI，最貴；而且候選過濾要 W1 的表 |
| **W4** | C：fixtures、`tools/check-vendor-shapes.sh`、manifest、diagnostics 一列 | 弱相依 W1b（它釘的斷言以 C4–C6 為主） | 可以和 W3 並行；放最後是因為 W1b 定下來之後再釘比較省 |

**W1 與 W4 之間沒有強相依，可以並行；W2、W3 都在 W1 後面，而且 W3 在 W2 後面。**
W1＋W1b 是一個 session 的量（一張表、一個狀態機、一組轉換、它們的測試），不要拆。

---

## 6. 怎麼證明它真的比現在好

**一個數字**：這台機器上，此刻有多少 assistant session 答不出 conversation id。

今天的基線（§1.3，2026-09-20 17:0x）：

```
21 列 = 16 claude(registry) + 5 codex(1 command_line, 1 open_file, 1 no_record, 2 ambiguous)
沒有 id 的：3 / 21 = 14%，全部是 Codex，其中 2 個是 ambiguous
「三個 codex 處在 ambiguous 的形狀」，其中一個被 argv 救掉
穩定性：12 次取樣 / 4 分鐘，21 列 0 列改變；但有 2 列在這 4 分鐘內出現或消失
```

**落地後用同一支取樣器、同一台機器、跑 24 小時，比同一個數字。** 預期：

- `ambiguous` 歸零（W1b 的結構規則）。
- `no_record` 只剩「開著但還沒有人打字」那一段，而且 Clawdline 開的 child 連這一段都沒有（W2）。
- `unreadable` 造成的掉名字歸零（W1 的 `remembered`）。
- 目標：**沒有 id 的列 ≤「剛開還沒打字的手開 Codex」的數量**，其餘全部有名字。

三個不能只看那個數字的補充驗收：

1. **「穿著別人名字」的次數必須是 0。** 這是 A 唯一的真風險。
   驗法是 §2.7 的「不在候選裡要推翻」那一條，加上 24 小時取樣裡
   **任何一列的 conversation 改變都要有一筆對應的 `superseded`／`retired` 事件**；
   有改變而沒有事件，就是一次沉默的換名，算失敗。
2. **寫入量**：24 小時之後 `session_identities` 的列數應該接近「這天開過的 session 數」，
   不是「這天盤點的次數」。差一個數量級就是 DG-4 破了。
3. **守衛真的會紅**：在 codex-cli 下一次升版時，`check-vendor-shapes.sh` 必須在
   **使用者發現 session 沒名字之前**先講話。這一條要等下一次升版才驗得到，
   在那之前只有 §4.5 的人造控制組。

---

## 7. Guidelines answer（`design-guidelines.md` §4）

1. **DG-1 停了誰知道**：守衛的結果寫進 store，diagnostics 顯示它**和年齡**；舊結果讀起來像舊的。
   身分本身沒有新的背景迴圈——刻意的，見 §4.4 的「沒選 daemon 定期跑」。
2. **DG-2 累積什麼**：`session_identities`，5,000 列，到頂淘汰已 `retired` 的、全活就拒絕＋health，
   diagnostics 永遠看得到。登記在 `internal/domain/capacity`。
3. **DG-3 代價的分母**：`lsof` 的 p50／max／位元組／pid 區塊數已量（§1.6）；
   身分記錄的寫入以**每個 session 1–2 次**為分母，不是每趟盤點；meta 行讀取 p50 0.057 ms。
4. **DG-4 哪些可以重算**：`confirmed_at` **刻意不落盤**（它是觀察）；落盤的只有建立／改變／退役三種事實，
   而且與它們的事件同一個交易。
5. **DG-5 新的名字**：`identity_state`（`confirmed|remembered`）與 `source`（多一個 `claimed`、一個 `reported`）
   是**兩個問題兩個欄位**，不是在 `Binding` 裡加第七個值。`conversation id` 只有一種拼法（`namespace_test.go` 已在守）。
6. **DG-6 讀的是執行用的那份**：身分的推翻與認領都在同一個 store 交易裡讀寫（D08 的 `mutate`）；
   `proc_start` 每次現問 kernel，不快取——`sessions.go:220` 的 `liveOf` 已經是這樣做的，理由也已經寫在那裡。
7. **DG-7 讀不到 ≠ 不存在**：§2.3 的轉換表就是這一條，四種「不動記錄」與四種「推翻」分開列。
   守衛的 exit 2 與 exit 0 是兩個字。
8. **DG-8 控制組**：§2.7、§3.5、§4.5 每一條都有「要看到它紅」的那一欄。
9. **DG-9 逐項繼承**：`sameStart`／`Identity.Matches`／`ProcessStart` 是既有規則的擴大使用，不是新發明；
   `session.Binding` 的六個值一個都不改語意。
10. **DG-10 人的位置**：只有一個新的人的決定——**認領**，在 session 卡片上，需要 `maySend`，
    預設是不認領（沒有名字），而且**機器不會替人按**。認領被機器推翻時，要告訴人。

---

## 8. 這份文件沒有做到的事

1. **W0 的三個實驗我沒有跑。** ① 需要一個 codex session；② 會在使用者機器上留下一個游離的
   codex thread；③ 那行程式在 `internal/adapters/process`，**W1b 那個在途 task 正握著**。
   ②如果成立，§3.2 的決定要換成更好的那一個。
2. **`lsof` 的 6 倍代價（§1.6）只被記錄，沒有修。** 同上，不是我的 claims。
3. **Windows 完全沒談。** `ps_windows.go` 與 `openfiles_windows.go` 今天回 `Complete:false` 與 `false`，
   在 A 之下它們會讓每一列都停在 `remembered` 或無記錄——**這是正確的行為，但沒有人驗過**。
4. **`ambiguous` 在真機上只在 Codex 的 guardian／subagent 形狀下重現過。**
   多個**主**執行緒同時被一個行程開著的情況，這台機器上沒有出現過，所以 C4 的
   「恰好一個」在那種情況下的行為只有紙上規則。
5. **候選清單的隱私邊界是我選的安全預設**（只給結構、不給內容；子執行緒預設不列），
   **使用者還沒有看過**。它會讓人看到「這台機器上還有哪些對話」的一小部分，這是可以被否決的。
6. **沒有量過這份設計自己的效益**——§6 的比較要等落地之後才跑得了。
