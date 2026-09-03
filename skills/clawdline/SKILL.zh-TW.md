---
name: clawdline
description: |
  使用 Clawdline 派 bounded child（目前 Root 保留綜合、整合與 landing）、把既有工作線完整 handoff
  給下一個 session，或傳送 message、report、status、finding、coordination note
  給另一個 live session。觸發語句包括「派任務」「開 child」「叫 Codex review」「使用 Clawdline Handoff」
  「交接給下一個 session」及等義英文。
  Handoff 會移轉 sender 的 REFERENCES、VERIFICATION、OPEN THREADS。poll-only detached task 只供
  無人值守 automation，絕不是 Root 或 Major Feature owner。Root Assignment / Feature Launch 會
  啟動獨立 ordinary Root；不得用 child、detached automation 或假 handoff 冒充。這條對話能直接做、provider-native
  subagent 的檢索、或只查 session inventory 時不要使用。若目前 session 是 Clawdline child，改依
  CHILD.md。
---

# Clawdline

**這個檔案是 discovery stub，不是使用指南。** 完整、與版本吻合的參考文件隨 Clawdline app bundle
一起出貨；刻意不寫在這裡，這樣它就不可能跟真正替你派工的那個 build 對不上。

## 做任何事之前，先載入指南

先解析出 reader，之後每一步都重複使用同一個：

- 若環境變數 `CLAWDLINE_SKILL_READER` 有值，用它。
- 否則用 `/Applications/Clawdline.app/Contents/Resources/clawdline-skill.sh`。
- 否則用 `$HOME/Applications/Clawdline.app/Contents/Resources/clawdline-skill.sh`。
- 否則，在這個 repository 的 checkout 裡，用 `Resources/clawdline-skill.sh`。

以下的 `READER` 代表你解析出來的路徑。執行前請直接代換；不要建立 shell 變數，也不要照字面執行
`READER`。

```
READER get clawdline.zh-TW    # 繁體中文
READER get clawdline          # English
READER list                   # 這個 build 帶了哪些主題
```

它會印出**這台機器上實際安裝的那個 build** 的完整指南：派工六步驟、handoff、detached automation、
Root Assignment、`claims` 與 `serialize`、landing 收尾、file wait、排程與 coordinator 路由。
**先讀它，再執行你需要的指令。**

這是一次本機檔案讀取，**不需要 Clawdline 正在執行**，所以在 SSH、無頭環境、app 關著的時候答案都一樣。

**不要憑記憶或這份 stub 的快取猜測路由、欄位或旗標。** 它們會隨版本改變，而這個檔案已經刻意不再列出它們。

若你選定的 reader 無法執行，回報它的確切錯誤並停下。不要改用另一個路徑：不同路徑可能描述的是另一個
build，而不是真正替你派工的那一個。

## 找不到任何 reader 時

只有在上面每個路徑都不存在時才適用，那表示這台機器沒有安裝 Clawdline——直接說出來，不要自己發明路由。
一個唯讀呼叫就能分辨它是不是只是「沒找到」：

```
PORT=$(jq -r '.remote_port // 7717' ~/.config/clawdline/config.json 2>/dev/null || echo 7717)
curl -s "http://127.0.0.1:$PORT/v1/health"
```

若它有回應，代表 app 正在跑、只是這份 stub 找不到它的 bundle。把兩件事都回報並停下，不要憑記憶派工。

## 這份 stub 唯一直接寫明的事

哪一條路由承載哪一種工作，是**角色邊界**而不是路由細節，所以它跟指南裡一樣寫在這裡。
這是每一個 Clawdline 表面都帶著的同一份封閉契約。

<!-- clawdline-dispatch-role-contract:v1 -->

- **Owned child.** `POST /v1/orchestrator/tasks` creates a bounded child only when Clawdfather
  retains synthesis, integration, and landing.
- **Handoff.** `POST /v1/orchestrator/handoffs` is continuation or transfer of an existing work
  line; the receiver must walk the sender's complete REFERENCES, answer VERIFICATION, and continue
  from OPEN THREADS.
- **Detached automation.** `POST /v1/orchestrator/detached-tasks` is the only public route that
  accepts `root.session_id: null` with `root.poll_only: true`; ordinary
  `POST /v1/orchestrator/tasks` refuses poll-only. It is only unattended automation, never a Root
  or Major Feature owner.
- **Root Assignment / Feature Launch.** `POST /v1/orchestrator/root-assignments` opens an
  ordinary independent Root and briefs only objective, scope, constraints, relevant references,
  and acceptance. Its durable machine-auth record and UI classification carry no child, handoff,
  detached, timeout, secret, result, parent, or landing lineage.

<!-- /clawdline-dispatch-role-contract:v1 -->
