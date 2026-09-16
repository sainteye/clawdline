# Clawdfather 協調：重新設計

本檔是對現行協調規則的檢視與改法。現行規則散在
`clawdline/docs/clawdline-protocol.html`、`Sources/Coordinator.swift`、
`Sources/Orchestrator.swift` 與安裝版 skill guide 裡；本檔只記**要改什麼、為什麼**。

---

## 1. 最根本的發現

**整個系統只有三個真正的時鐘**：`timeout_minutes`（自 `briefed` 起算）、
四分鐘的 brief 窗口加五次輸入嘗試、Root Assignment 的 240 秒。

**child 結束之後的每一個義務都沒有時鐘**——wait、pending landing、handoff 接收方、
離線的 coordinator、dead letter、看板指派提案、landing-queue 位置、零工池。

這不是疏忽，而是一條貫穿全局、而且正確的原則：

> **absence of evidence is never proof of death**

「這次讀取裡沒看到」這句話，對一個暫時失去輔助使用權限的終端機也成立。

但這條原則的**代價從未被記帳**。完整形狀是：

> 每一個沒有時鐘的轉換，都是設計選了 `unknown` 而不是選期限，
> 然後把由此產生的決定交給一個**從來沒有人告訴他要去看**的人。

實測：2026-09-06，這台 Mac 結束時有 26 筆落地沒人記錄、10 筆交付從頭重做，
而 `GET /v1/orchestrator/inflight` 整天都正確地列著每一筆。文件自己的結論是
**"Nothing made anybody look."**

---

## 2. 要保留的

檢視不是為了推翻。以下原樣帶走：

- **`mover` 的概念**（只是要推廣，見下）
- **closeability 的三分法**：obligation / evidence / attestation，
  且 evidence 類一律 fail closed 到 `unknown`——它們動搖的是「義務清單完不完整」
- **handoff 信封刻意不含 transcript、路徑、terminal id**
- **看板的 ownership 分割**，特別是「使用者可編輯的證據不得鑄造可信的驗證或落地」
- **六個分開的事實**：accepted、executed、result-verified、transport-delivered、
  observed、acknowledged——沒有任何 HTTP 200 能製造出最後兩個
- **succession 的 13 個分開時間戳**：崩潰不會把「開了一個分頁」壓縮成「所有權移轉了」
- **「absence of evidence is never proof of death」本身**。要加的是時鐘的**可見度**，
  不是時鐘的**裁決權**。

---

## 3. 核心模型：Obligation

現行系統裡 wait、pending landing、handoff receiver、succession、dead letter、
board assignment、landing-queue position 各有各的狀態、各自的停滯方式、各自的查法。

它們是同一個概念。

```go
type Obligation struct {
    Kind      Kind      // wait | landing | handoff | succession | assignment | deadletter | ...
    Subject   Ref       // 這是誰的
    Mover     Mover     // 誰能清掉：thisSession | otherSession(id) | person | task(id) | broker
    Age       Duration  // 一級欄位，可查詢、可排序
    Escalated bool      // 超過閾值後自動浮上來
    Evidence  Evidence  // 我怎麼知道的
}
```

一個型別、一套查詢、一套升級規則。

---

## 4. 八項改法

### 4.1 加時鐘，但不下死亡判決

現行的二選一是假的：超時判死會誤判活著的線，永遠 `unknown` 會靜靜卡住。

第三條路：**每個停滯義務有一個 escalation clock。時間到了不改變狀態，改變可見度。**
狀態仍是「沒人能證明它死了」，但那變成**會老化、會排序、會叫的事實**。

### 4.2 把 `mover` 推廣到每一個義務

`mover` 目前是整個系統**唯一**標註「誰能清掉這件事」的地方，而它只長在 closeability 上。
推到每個義務上之後，「哪些線卡住了、誰該動」就是**一個查詢**，不是一輪廣播。

（實測動機：2026-09-06 曾發 17 則訊息去問「你卡住了嗎」，換回 17 句「我沒停」。
能回答的欄位當時就在同一列。）

### 4.3 通道要宣告自己的送達語義

現行：`progress` 只進 task 紀錄，`notify` 推到手機，只有 `result.json` 會叫醒 root。
而 guide 說 progress 會叫醒 root、`api.md` 說不會——**兩份現行文件互相矛盾**。

改法：每個通道在 schema 宣告 `reaches` 與 `wakes`。新增型別化的 `ask_root`，
broker 保證送達；送不到就**在派工當下拒絕**，而不是讓 child 等一個不會來的回覆。

### 4.4 節點狀態要有第三種

現行複審節點只有 `done` / `failed`，所以 `changes_required`（正常結果）會擋住它自己的
修正節點。量到的 18 份判決沒有一份是 `safe_to_land`，等於幾乎每個 feature 都會撞。

改法：加 `completed_with_findings`，frontier 推導要能往下走。

### 4.5 不可變的東西由 broker 持有

現行 graph 每次續派都要原樣重送整份定義，改一個字就 `graph_definition_conflict`。

改法：建立一次拿 id，之後只送 `graph_id` + `node`。

### 4.6 呼叫者不該重算 broker 已知的事

三個同形狀的現行陷阱：claims 要相對於 worktree 而非 `project_dir`；
驗收要用 `worktree.base` 而非 `main`；landing 要填自己那顆 commit
（而 `rev-parse HEAD` 可能已是別人的）。

改法：這三件 broker 都知道，都由它算。

### 4.7 契約單一來源

見 §5：現行有六處文件互相矛盾，每一處都是「同一件事寫在兩個地方」。
改法：只有一份 schema，文件從它生成，守衛擋住漂移。

### 4.8 失敗原因型別化

現行只能靠讀 transcript（或完全查不到）的失敗：

- **權限提示**：「問過並核准」與「從來沒問」寫下來一模一樣，事後無法還原
- **帳號不支援的 model pin**：400 只出現在該助理的 rollout
- **省略 model**：繼承當下 `/model`，沒有紀錄留下
- **Codex worktree child 完成但不能 commit**：報 `failure`，但那其實是成功

改法：派工時把解析後的 model 寫進紀錄；child 結束時 broker 讀 provider transcript
抓 error code 轉成 typed reason；broker 自己不讀的欄位要嘛開始讀、要嘛刪掉。

---

## 5. 現行文件的六處矛盾（新契約必須裁決）

| 矛盾 | 兩邊各說 |
|---|---|
| `inventory_generation` | skill 說「門是關著的直到你讀」；protocol 頁面出現 0 次 |
| §20「每個 typed error」 | 少了 5 個 succession code |
| 複審者能不能修 | skill 說不行；protocol 說可以有界修 |
| 何時指定 model | skill 說總是指定 opus；protocol 說只在預設尺寸不對時 |
| `kind` 的值域 | 封閉四值 vs standing session 要第五個（同一份文件相隔兩節） |
| `linger: -1` 是 child 還是 root | protocol 說是獨立 Root 且可派工；`CHILD.md` 說「你是樹的底部」 |

最後一項最嚴重：**depth 是整個設計最根本的不變量，而它沒有被裁決。**

---

## 6. 兩個實質漏洞

1. **任何 root 可以解掉任何 task 的 claims**，而且審計行記不到呼叫者，
   因為 machine token 不識別身分。一個鎖，任何人都能開，開了還不留名。
2. **isolated dispatch 沒有任何守衛**。相對 claims 對 isolated task 只留警告。
   實測：2026-08-28 兩個 root 在六秒內派了同一個交付的修正，兩個都 isolated，
   什麼都沒擋，而 `/inflight` 對兩邊都是空的。

---

## 7. 對 P3 的意義

P3 不是把三萬九千行翻譯過去，而是實作**四個狀態機加一套共用的義務模型**。

四個狀態機各自保持獨立——現行文件反覆強調它們不是彼此，那是對的：

1. child task 生命週期（8 狀態，唯一有時鐘的）
2. root landing 義務（4 階，3 種憑證）
3. session 投影（`work_state`、`owed`、`closeability`，永不儲存成第二份真相）
4. coordinator 身分帳本

但**停滯這件事統一建模**（§3）。這樣「哪些事卡住了、誰該動、多久了」是一個查詢，
不是一輪廣播，也不是一個需要有人記得去看的頁面。
