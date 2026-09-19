# 工作系統：看板項目、Session 待辦、Backlog、GitHub Issue

> 這一份講完整套工作系統：四個物件各在什麼情況下用、怎麼開始、怎麼推進、怎麼結束、達成什麼，
> 以及哪些已經在跑、哪些只是設計。依據是本 repo `13d08ea` 的程式，加上 2026-09-19 對執行中的 daemon
> 做的唯讀查詢（§8）。實作決定的依據仍是 [`design-decisions.md`](design-decisions.md)（D30–D39、U1–U6），
> 兩份不一致時照 §10 處理。背景材料：[`board-redesign.md`](board-redesign.md)（設計與量測）、
> [`github-issues.md`](github-issues.md)（GitHub 對照）、[`board-design.md`](board-design.md)（舊卡）、
> [`limits.md`](limits.md)（上限）。

## 0. 這套系統在解什麼問題

舊版只有一種東西：看板卡。同一張卡同時是人要看的進度、也是 session 的備忘，結果兩邊都壞掉。
`board-redesign.md` §1 量過舊版的 787 張卡：人建立的 0 張、走到 `closed` 的 0 張、345 張在證據上已經做完
但狀態沒動、三分之二是 broker 自己的流水帳，派工只有 12.1% 綁得到卡。

這套系統只為兩個目的存在（使用者 2026-09-18 定的）：

1. **讓人看懂現在的狀況。** 你打開看板，只看到「有承諾」的事；每一件都有負責人、有時鐘、會自己結束。
2. **讓 session 不會忘記要做什麼。** session 欠的東西由事實建立、由事實關閉，不靠 session 記得呼叫任何路由。

做法：把「一件事」分給四個物件，**每個物件只有一種讀者、一套生命週期**。物件之間只靠 `work_id` 相連，
每一次搬動寫一筆只能新增的 `moves`。看板上的狀態每次讀取時從 broker 的事實推出來，不另存一份。

## 1. 全貌：一張圖，和「這件事放哪裡」

```text
  outside readers              YOU (a person)  <----(q)---- a session's decision
        |                         +---+-----------------+
      (g1)                       (p)                   (p)
        v                         v                     v
 +--------------+   (g2)   +--------------+  (b)  +-----------+
 | GITHUB ISSUE |<- - - - -| BOARD ITEM   |------>| BACKLOG   |--(x)--> dropped
 +--------------+          | committed,   |<------| planned,  |
        ^                  | owned, now   |  (a)  | no commit |
        |                  +--------------+       +-----------+
      (g3)                   |   ^   |   ^              ^
        |                   (e) (w) (u) (t)            (l)
        |                    v   |   |   |              |
  landing commit          closed |   |  +----------------------+
  "Fixes owner/repo#N"           |   |  | PROPOSAL             |
                                 |   |  | ("to confirm")       |
                                 |   |  +----------------------+
                                 |   |      ^          |
                                 |   |     (s)        (n)
                                 |   |      |          |
                                 |   v      |          v
                          +---------------------------------------+
                          | SESSION TO-DO   (one per dispatch)    |--(k)--> done / dropped
                          +---------------------------------------+
                                            ^
                                           (d)
                                            |
                                      root session
```

四個物件：BOARD ITEM＝看板項目、SESSION TO-DO＝Session 待辦、BACKLOG＝Backlog、GITHUB ISSUE。PROPOSAL
（提議）不是物件，是待辦通往看板與 Backlog 的唯一一道門。(g2)、(g3) 兩條今天只是設計（§6）。

- (d) root 派工，broker 在同一筆交易裡開一筆待辦（TD-2）。
- (s) 這條工作線值得你追（派了 child、待辦超過 24 小時、有外部效果），session 或規則提議（PT-1、PT-3）。
- (t)／(l)／(n) 你回答「追蹤」上看板、「之後」進 Backlog、「不用」留在待辦；7 天沒回答等於「不用」（PT-4）。
- (p) 你自己新增，放看板或 Backlog（BD-2、BL-2）。
- (w) 派工帶這一項的 `work_id`：那個 task 的進行、交付、落地都算這一項的事實（BD-4）。
- (a) Backlog 有了承諾：派工帶它的 `work_id`、日期進入 7 天內、你按 start（BL-6）。
- (b) 看板 3 天沒有新事實、排定的日期錯過、你按 defer（BD-13）。(u) 你按 untrack，只剩待辦（BD-13）。
- (e) 看板項目結束：落地、收下、未確認、放棄（BD-9～BD-12）。(x) Backlog 只有你能丟（BL-7）。
- (k) 待辦由落地紀錄關掉，或沒人接 24 小時後放掉（TD-9、TD-10）。
- (q) session 需要你拍板：發一個「決定」，掛在它的工作上（PT-5）。
- (g1) 外面的人開 issue（GH-2）；(g2) 本機項目連到 issue，只存連結（GH-4）；(g3) 落地 commit 寫
  `Fixes owner/repo#N`，push 到預設分支後由 GitHub 關 issue（GH-6）。

**這件事放哪裡**：由上往下問，第一個「是」就是答案。

| 先問 | 是的話放 | 你要做的 |
|---|---|---|
| 讀的人在這台機器以外？（外部回報的缺陷、開源產品對外的規劃） | GitHub Issue | 在 GitHub 開 issue（§6） |
| 只是 session 做事的一步，你不需要知道？ | Session 待辦 | 什麼都不用做：派工時 broker 自己開（§4） |
| 要你拍板才能繼續？ | 不是項目，是一個「決定」 | 等 session 發決定，在 console 回答（PT-5） |
| 你要現在看到進度？（已經派出去、你自己接了、7 天內要開始） | 看板項目 | console 的「工作」頁新增，或 `POST /v1/work/items` 帶 `"place":"board"`（§3） |
| 要做，但還沒有開始的承諾？ | Backlog | 同上，帶 `"place":"backlog"`；有日期就排 `schedule`（§5） |

**它會怎麼結束**：看板項目在 broker 記下落地時自己結束；交付了沒落地的，等你收下，問過你一次之後 7 天
沒回就以「未確認」結束；3 天沒有新事實就退回 Backlog。Session 待辦由落地紀錄關掉。Backlog 只有你能丟掉，
有了承諾就自己上看板。GitHub Issue 由 GitHub 自己關，本機不以它為準。

不在這四個物件裡的：`/v1/board`（console 的「專案 · 看板」）是舊 app 的 787 張卡，唯讀、不遷移（U1），不再投資（D34）。

## 2. 怎麼讀這份文件，以及四個物件共用的規則

- **編號**：`BD` 看板項目、`TD` Session 待辦、`BL` Backlog、`GH` GitHub Issue、`PT` 物件之間的路、`CM` 共用。
  要改哪一條，說它的編號就好。
- **狀態**：每條規則後面一個標記，只有三種：已實作（寫出程式在哪，`internal/` 底下的檔名與符號，其他目錄寫全路徑）、
  部分（寫出缺什麼）、只是設計。缺口的細節在 §9。
- **誰**：「你」是人，用本機瀏覽器或已配對、能送出的裝置，`moves` 的 actor 記 `user`；「root」是持 orchestrator
  token 的 session（放在 `X-Clawdline-Orchestrator` 標頭）；「child」是持自己 task secret 的 session；
  「broker」「rule」是 daemon 自己，依事實或時鐘動作。

**CM-1** 每一個寫入都帶 `Idempotency-Key`：沒帶回 400 `idempotency_key_required`；同 key 同內容重播原回應，不同內容回 409 `idempotency_key_reused`，過了視窗回 409 `receipt_expired`，不重做（D03）。〔已實作 `transport/http/work.go` `claimOnce`〕

**CM-2** session 不能自己動你的看板：用 orchestrator token 寫 `/v1/work/*` 一律 403 `session_cannot_decide`，除非帶 `{"via":{"run":"…"}}` 代轉你說的話，actor 記 `user_via_session:<run>`。〔部分：run 已有發放者並會驗證（`app/runs.go` `Runs`、`transport/http/runs.go` `relayRun`）：人經 `POST /v1/sessions/{id}/send` 送出訊息時發，session 用 `GET /v1/orchestrator/sessions/<對話 id>/run` 取；沒發過的 403 `run_unknown`、超過一天 403 `run_expired`、拿別的 session 的 run 答提議或決定 403 `run_other_session`、比問題還早的訊息 403 `run_before_question`；直接在 terminal 打的字沒有 run。缺的是分不出哪個 session 在代轉（缺口 11）〕

**CM-3** 每一次搬動、每一個指令都寫一筆 `moves`（trigger、actor、依據的事實），資料庫 trigger 擋掉 UPDATE 與 DELETE；用 `GET /v1/work/items/{id}/moves` 讀。〔已實作 `adapters/store/work.go`〕

**CM-4** 一件工作同時只在一個地方：看板或 Backlog 二選一，資料庫 trigger 擋住兩處同時存在（409 `work_in_two_places`）；兩處都不在時只剩它的待辦。〔已實作 `adapters/store/work.go` `work_one_place_board`、`work_one_place_backlog`〕

## 3. 看板項目

讀者是你。狀態：`active`（進行中）→ `awaiting_closure`（交付了、等收尾）→ `done`（`landed`／`accepted`／
`unconfirmed`），或 `dropped`。看板分四區：等你決定（等收尾的交付）、進行中、本週排入、最近完成（7 天內結束的）。

| 什麼情況下用 | 怎麼開始 | 怎麼推進 | 怎麼結束 | 達成什麼 |
|---|---|---|---|---|
| **BD-1** 你要現在看到它的進度，而且它有承諾：有派工綁著、你自己接了、7 天內要開始、或交付在等你收。承諾記在 `commitment` 欄（`dispatch`／`assigned`／`scheduled`）。〔部分：`decision`、`delivered` 兩個值沒有程式會寫（缺口 8）〕 | **BD-2** 你在 console「工作」頁新增，或 `POST /v1/work/items` `{"place":"board","title":…,"project":…}`；`owner` 省略就是你自己。缺欄位回 400 `project_required`／`invalid_title`；未結束的項目滿 2,000 回 507 `work_full`。在 session 裡說「追蹤這個」走 CM-2。〔已實作 `app/board_work.go` `Create`〕<br>從 Backlog 上來見 BL-6，從提議來見下一列。 | **BD-4** root 派工時在 `POST /v1/orchestrator/tasks` 帶這一項的 `work_id`：那個 task 的進行、交付、落地就算這一項的事實；格式不對回 422 `bad_task`。〔已實作 `domain/work/lines.go` `LineOf`、`app/orchestrator/lines.go` `bindLine`：沒帶的由 broker 在接受派工時依規則綁定（respawn 沿用原本的、同一個 graph 同一條、否則以 task id 開一條，review／test 這類步驟不綁），record 記 `work_from`；帶了還沒有項目的 id 照收，那是一條還沒上板的線〕<br>**BD-5** 狀態每次讀取時推導、不存：有 task 在跑是 `active`；交付了沒落地是 `awaiting_closure`；全部落地是 `done`。〔已實作 `domain/work/board.go` `Derive`〕<br>**BD-6** sweep 每 15 秒（`CLAWDLINE_NEXT_WORK_TICK`）依序套 8 條自動規則，有變化才寫一筆 `moves`；連續 3 個 tick 沒跑完，`/v1/health` 回 `ok:false`、`reason:"work_sweep_stalled"`。〔已實作 `app/board_work.go` `Run`、`Stalled`，規則在 `SweepRules`〕<br>**BD-7** 你的指令 `POST /v1/work/items/{id}` `{"op":…}`：start、track、schedule、defer、accept、rework、drop、handover、untrack、rank。帶 `expected_version` 而項目已經變了，回 409 `version_conflict` 並附上目前的樣子；綁的 task 讀不到，回 409 `facts_unknown`；`land`、`complete`、`close` 這類字回 422 `landing_is_broker_fact`；多帶不認得的欄位回 400 `invalid_command`。〔已實作 `domain/work/board.go` `Decide`、`ParseOp`，`transport/http/work.go` `readWorkBody`〕<br>**BD-8** 有「決定」在等你時，3 天的時鐘停住；交付等收尾滿 3 天，每日摘要問你一次（寫一筆 `closure_asked`）。〔部分：摘要只存不送（缺口 3）；時鐘停住已實作 `Derive` 讀的 `DecisionSince`〕 | **BD-9** 落地：這一輪綁的 task 全部是 `landed` 或 `nothing_to_land` → `done`（`landed`），actor 是 `broker`。〔已實作 `ruleLanded`〕<br>**BD-10** 收下：等收尾時你按 accept → `done`（`accepted`，證據記 `landed:false`）；還沒交付就按，回 409 `not_awaiting_closure`。〔已實作 `Decide`〕<br>**BD-11** 沒人回：問過之後 7 天 → `done`（`unconfirmed`）。只清掉看板，不宣稱落地，未落地的交付仍在 broker 的 inventory 裡。〔已實作 `ruleUnconfirmed`〕<br>**BD-12** 放棄：只有你能 drop → `dropped`。〔已實作 `Decide`〕<br>**BD-13** 離開看板但沒結束：沒有 task 在跑、3 天沒有新事實 → 回 Backlog（`stalled_3d`）；排定日期過了一整天還沒派工 → 回 Backlog（`schedule_missed`）；你按 defer → 回 Backlog；你按 untrack → 只剩待辦。〔已實作 `ruleStalled`、`ruleScheduleMissed`、`Decide`〕<br>**BD-14** 結束之後又有新 task 綁上 → 重開成 `active`（`redispatched`）。〔已實作 `ruleRedispatched`〕<br>**BD-15** owner 那段對話不在了 → 標「無人負責」、寫進摘要。〔只是設計（缺口 7）〕<br>**BD-16** 同一件事開了兩項：以 `duplicate` 結束並帶 `duplicate_of`；每一項另有專案內唯一的短編號 `#N`。〔只是設計（G5，缺口 10）〕 | 看板上的每一項都有承諾、負責人和下一個時鐘（讀取時的 `derived.stall_at`、`derived.closure_due_at`）；做完的靠 broker 的落地紀錄自己離開，不再出現「證據上做完、看板沒動」。驗法：`GET /v1/work/board` 的每一列都有 `owner` 與 `commitment`（資料庫 CHECK 擋住空值），`derived.reason` 說得出它在這一區的理由。 |
| 你沒開，但 session 在做的一條工作線值得你追：它派了 child、它的待辦開超過 24 小時、或它有外部效果 | **BD-3** 經 PT-1 或 PT-3 提議，你在「待確認」答 `track`：項目上看板，owner 是提議的 root，這條線已經派出去的 task 都算這一輪。〔已實作 `app/proposals.go` `Answer`、`domain/work/proposals.go` `Placing`；提議的 task 若還不在線上，提議與回答後由 broker 綁上（`BindWork`），項目才跟得到它的落地〕 | 同上一列 | 同上一列 | 只在規則說值得的時候問你；問不問由伺服器決定，不由 agent 自己判斷 |

**容量**：`work.open` 上限 2,000（看板未結束的加上 Backlog 的 `planned`），到頂時新增回 507 `work_full`，
只有你能清（收下、放棄或丟掉）；登記在 `internal/domain/capacity`。

## 4. Session 待辦

讀者是 session（root，或之後接手的人），不是你。狀態：`open`、`handed_off`（還欠著，owner 確定不在）、
`done`、`dropped`。

| 什麼情況下用 | 怎麼開始 | 怎麼推進 | 怎麼結束 | 達成什麼 |
|---|---|---|---|---|
| **TD-1** 每一個有 root 的派工都有一筆，你不用做任何事。沒有 root 的 detached task 與排程不產生待辦。〔已實作 `domain/work/todos.go` `DispatchTodo`〕 | **TD-2** broker 在接受派工的同一筆交易裡開一筆 `dispatch:<task id>`，意思是 root 的「收結果、落地」。沒有任何路由能手動建立。〔已實作 `app/orchestrator/todos.go`〕<br>**TD-3** 其他來源：`result.json` 與交付的 `remaining`、obligation。表的 CHECK 留了這幾個值。〔只是設計：沒有生產者（缺口 4）〕 | **TD-4** task 的每一個新事實，在記錄它的同一筆交易裡套到待辦上；待辦寫不進去，那個事實也一起不寫。〔已實作 `Follow`、`applyTodo`〕<br>**TD-5** broker 的 beat 讀 owner 在不在：確定不在 → `handed_off`；回來了 → `open`；讀不到 → 不動（DG-7）。〔已實作 `tendTodos`、`Tend`〕<br>**TD-6** 讀：root 用 `GET /v1/orchestrator/sessions/<conversation id>/todos?state=outstanding\|closed\|all`（給 terminal id 回 409 `session_id_is_terminal`）；你在 session 詳情的「待辦」面板讀（`GET /v1/sessions/<列 id>/todos`，對話 id 還不知道時回 409 `conversation_unknown`）。〔已實作 `transport/http/todos.go`、`web/console/src/session/Todos.tsx`〕<br>**TD-7** 每筆帶升級訊號 `cross_session`、`long_lived`、`repeated_failure`。〔部分：`cross_session` 看有沒有看板或 Backlog 項目接住這條線（`Escalation` 的 `held`）；`repeated_failure` 沒有人接（缺口 5）〕<br>**TD-8** 在 session 容易忘的時刻把未完成的待辦帶回去：派工簡報、handoff 的 OPEN THREADS、完成通知。〔只是設計（缺口 6）〕 | **TD-9** 落地紀錄說 `landed`／`nothing_to_land`／`abandoned`，或 task 結束時不欠落地（`nothing_owed`）→ `done`；之後又開了落地義務 → 重開。〔已實作 `owed`、`Follow`〕<br>**TD-10** `handed_off` 滿 24 小時、owner 仍確定不在 → `dropped`；task 和它欠的落地仍在 broker 的 inventory，只是不再提醒。〔部分：放掉已實作 `Tend`；沒有接手的人，`handed_to` 沒有寫入者（缺口 6）〕 | session 不用記得呼叫任何東西（舊版 43.6% 的 run 連 `deliver` 都漏了）；欠的事由事實開、由事實關；你的看板上看不到它們。驗法：`GET …/todos?state=outstanding` 的每一筆，對應的 task 都還在跑、或還欠著落地。 |

**容量**：沒有登記（缺口 9）。每個派工一列、永遠保留；熱路徑只讀還欠著的（`OutstandingTodos`）。

## 5. Backlog

讀者是做規劃時的你。狀態：`planned`、`dropped`。沒有 owner 欄，也沒有時鐘會把它刪掉。

| 什麼情況下用 | 怎麼開始 | 怎麼推進 | 怎麼結束 | 達成什麼 |
|---|---|---|---|---|
| **BL-1** 決定要做，但還沒有開始的承諾：沒有派工、沒人接、沒有 7 天內的日期。〔已實作：`backlog` 表沒有 owner，放進來就不在看板上（CM-4）〕 | **BL-2** 你新增 `POST /v1/work/items` `{"place":"backlog"}`，可以帶 `start_on`、`rank`；帶 `owner` 回 400 `owner_on_board_only`。〔已實作 `app/board_work.go` `Create`〕<br>**BL-3** 提議答 `later`。〔已實作 `Placing`〕<br>看板退回來的見 BD-13。 | **BL-4** 排順序 `{"op":"rank","rank":N}`（0 是不排，排在最後）；排日期 `{"op":"schedule","start_on":"YYYY-MM-DD"}`，日期在 7 天內（含已經過去的）就直接上看板。不是 `planned` 回 409 `wrong_place`。〔已實作 `Decide`〕<br>**BL-5** 每週摘要列出 30 天沒被看過的項目（rank、schedule 會重設這個鐘），問「要留嗎」；同一項 30 天內只問一次，不回答就是留。〔部分：列出已實作 `app/proposals.go` `weekly`；摘要只存不送（缺口 3）〕 | **BL-6** 上看板：放進 Backlog 之後有派工帶它的 `work_id` → `dispatched`（owner 是那個 root）；`start_on` 進入 7 天內 → `start_on_within_7d`（已經過去的日期不會自己上去）；你按 start（可以指定 owner）。〔已實作 `ruleDispatched`、`ruleStartSoon`、`Decide`〕<br>**BL-7** 丟掉：只有你能 drop；機器永遠不刪 Backlog，滿了只拒絕新增。〔已實作 `Decide`、`adapters/store/work.go` `WorkOpenLimit`〕 | 規劃留得住，又不假裝在進行；看板與 Backlog 的界線是一個事實（有沒有承諾），不是形容詞。驗法：`GET /v1/work/backlog` 的每一項 `owner` 都是 null、`commitment` 都是 null。 |

**容量**：和看板共用 `work.open`（§3）。

## 6. GitHub Issue

讀者在這台機器以外。本機不存 issue，只在 GH-4 做了之後存「項目連到哪一個 issue」。

| 什麼情況下用 | 怎麼開始 | 怎麼推進 | 怎麼結束 | 達成什麼 |
|---|---|---|---|---|
| **GH-1** 外部回報的缺陷與需求、開源產品對外的規劃、「哪個 commit 解決了它」、發版分組。這台機器上的工作（私有 repo、沒有 repo 的目錄、待辦、提議、決定）一律不上 GitHub：公開 repo 的 issue 一律公開。〔已實作：程式裡沒有任何 GitHub 用戶端，本機資料出不去〕 | **GH-2** 外面的人在 `sainteye/clawdline` 用 bug、feature 兩個 issue form 開 issue。〔只是設計：repo 沒有 `.github/ISSUE_TEMPLATE`（G1，缺口 10）〕<br>**GH-3** Clawdline 不代開 issue。〔已實作：沒有這條路（G4）〕 | **GH-4** 你對本機項目下 `link` `{"github":"owner/repo#N"}`：`owner/repo` 要和專案的 git remote 相同，否則回 409 `link_repo_mismatch`，專案沒有 GitHub remote 回 409 `no_github_remote`；寫一筆 `moves`，不呼叫 GitHub API、不讀 token；畫面顯示「GitHub 上的狀態：未查」。〔只是設計（G3，缺口 10）〕<br>**GH-5** 連結之後，本機不再維護它的 `rank`，每週摘要不再問「要留嗎」。〔只是設計（缺口 10）〕 | **GH-6** root 落地已連結的項目時，commit 訊息寫 `Fixes owner/repo#N`；push 到預設分支後 GitHub 自己關 issue；落地時沒寫只提醒、不擋。〔只是設計（缺口 10）〕<br>**GH-7** 本機項目照樣只看 broker 的落地紀錄結束；GitHub 上的 closed 只是對外的回聲，不改本機任何狀態。〔已實作：本機規則沒有讀 GitHub 的路〕 | 外面的人有公開的入口與紀錄；本機規則在離線、私有、沒有 repo 時照樣正確；離開這台機器的只有 `#N`。驗法：`internal`、`cmd` 底下搜不到 `api.github.com`。 |

使用者 2026-09-19 的拍板（編號是 `github-issues.md` §7 的 G1–G6，不是 `design-decisions.md` §5 的 G01–G35）：

| # | 內容 | 拍板 | 今天 |
|---|---|---|---|
| G1 | 對外的需求與缺陷改在 `sainteye/clawdline` 的 Issues 收，加 bug、feature 兩個 issue form | 做，併進 repo 替換的那一步 | 還沒做 |
| G2 | 開 Discussions | 不開 | 關著 |
| G3 | 連結、不同步（GH-4～GH-6） | 做，排在 T 線之後 | 還沒做 |
| G4 | 由 Clawdline 代開 issue | 不做 | 沒有這條路 |
| G5 | `duplicate` 關閉原因、每個專案的短編號 `#N`（BD-16） | 做 | 還沒做 |
| G6 | 雙向同步 | 不做 | 沒有這條路 |

## 7. 物件之間的路：提議、決定、摘要

你只在三個地方被拉進來：提議、決定、摘要。其他一律不打擾你（D31、DG-10）。

**PT-1** 提議（待辦 → 看板或 Backlog）：root 用 orchestrator token `POST /v1/orchestrator/proposals`，帶 `work_id` 或 `task_id`；child 用自己的 task secret 與 `task_id`，記在 root 名下、不問人。伺服器從事實算 I1 派了 child、I2 待辦超過 24 小時、I3 外部效果（session 宣告 `deploy`／`publish`／`push_default_branch`／`spend`／`email`，或 broker 看到落地在 `main`／`master`）。拒絕時什麼都不記：422 `proposal_below_threshold`、409 `proposal_duplicate`、409 `proposal_already_tracked`、409 `not_the_root`、429 `proposals_full`、400 `unknown_effect`。〔已實作 `domain/work/proposals.go` `GateProposal`、`app/proposals.go` `Propose`〕

**PT-2** 在對話裡問不問，看回應的 `ask`：你 30 分鐘內透過這個 daemon 送過訊息給那個 session、它這一輪還沒問過、今天全機問不到 3 次，才是 `ask:true`，並附上要問的那一句；問完回報 `POST /v1/orchestrator/proposals/{id}/asked`。其他一律進「待確認」、不推播。daemon 重啟後當作你不在。〔已實作 `GateProposal`、`app/proposals.go` `Heard`、`ReportAsked`〕

**PT-3** 規則自己提：sweep 對「派工帶了 `work_id`、但還沒有項目」的工作線提議，只進待確認、不在對話裡問。〔已實作 `app/proposals.go` `RuleProposals`：每個有 root 的派工都在一條線上；待辦還欠著、第一個派工滿 30 分鐘（`ProposalPolicy.RuleAfter`，先讓 root 自己提、自己在對話裡問）才提，已結束的不提〕

**PT-4** 回答提議：你 `POST /v1/work/proposals/{id}` `{"answer":"track"|"later"|"no"}`；7 天沒回答變成 `expired`，工作留在待辦、不上你的看板；過期之後仍然可以回答。〔已實作 `app/proposals.go` `Answer`、`domain/work/proposals.go` `ExpireProposal`〕

**PT-5** 決定：root `POST /v1/orchestrator/decisions`，2 到 4 個選項，必填 `default`（沒有回 400 `decision_default_required`），期限 60 到 10,080 分鐘（預設 7 天），到期就採用預設（`defaulted`）。只有 `blocking:true` 的推播一次，推播的結果另記一筆（`sent`／`not_subscribed`／`over_budget`／`failed`／`unknown`）。你在 console 或 `POST /v1/work/decisions/{id}` 回答；掛在看板項目上時，它的 3 天時鐘停住。〔已實作 `NewDecision`、`OpenDecision`、`PushDecision`〕

**PT-6** 摘要：每天一則（前一天完成的、落地的、停擺的、自動搬動的、待確認的、等收尾的、這次要問的收尾），每週一則（30 天沒看過的 Backlog）。存在 `digests` 表，用 `GET /v1/work/digests` 讀，console 工作頁會顯示。〔部分：只存不送（缺口 3）〕

**PT-7** 量 agent 有沒有照伺服器說的做：`/v1/diagnostics` 的 `proposals.ask_true`、`asked_inline`、`asked_inline_unprompted`。〔已實作 `transport/http/proposals.go` `proposalDiagnostics`〕

**PT-8** 訊息不再夾 workflow 信封，session 不自己開卡；舊 helper 呼叫 `POST /v1/orchestrator/sessions/<terminal>/workflow` 得到 200 與「已退役」，什麼都不記。〔已實作 `transport/http/workflow.go`（D36、U5）〕

**容量**：`proposals.open` 500、`decisions.open` 256，到頂拒絕新的（429）；`work.digests` 800，淘汰最舊的。

## 8. 今天真的長怎樣（2026-09-19，執行中的 daemon，只做唯讀 GET）

| 路由 | 看到的 |
|---|---|
| `/v1/health` | `ok:true` |
| `/v1/work/board` | 四區都是 0；`sweep` 每 15 秒一次、已跑 132 次、`stalled:false` |
| `/v1/work/backlog` | `planned` 10、`dropped` 0；10 項全部由 root 代轉建立（actor `user_via_session:<run>`），沒有 rank、沒有 start_on、綁定的 task 0 |
| `/v1/work/proposals`（pending／answered／expired） | 全部 0 |
| `/v1/work/decisions`（open／answered） | 全部 0 |
| `/v1/work/digests` | 2 則（daily、weekly），每一欄都是 0 |
| `/v1/orchestrator/sessions/<root>/todos?state=all`（派這份文件的 root） | 14 筆：`open` 7（都是 `dispatched`、都帶 `cross_session`），`done` 7（`landed` 3、`nothing_owed` 4）；帶 `work_id` 的 0 筆 |
| `/v1/diagnostics` | `work.open` 10／2,000、`proposals.open` 0／500、`decisions.open` 0／256、`work.digests` 2／800；沒有待辦的容量列；`proposals.ask_true` 0、`ask_false` 0 |

**怎麼讀**：規則都在跑（sweep 活著、摘要照時寫），但**看板到今天沒有接到任何一筆派工**。派工不帶 `work_id`，
看板就收不到它的事實；規則提議只看帶 `work_id` 的工作線；也沒有 session 主動提議過。所以「session 做的事 →
提議 → 看板」這條路今天是斷的（缺口 1），而 Backlog 目前唯一的來源是 root 代轉（缺口 2）。

## 9. 缺口

每一條寫：缺什麼、影響哪幾條、下一步誰決定。補上的那一個 commit 把它從這裡刪掉（§10）。

3. **摘要只存不送**（BD-8、BL-5、PT-6）。「問你一次」只寫進 `digests` 表；你沒打開 console 工作頁就等於沒被問，
   7 天後照樣以 `unconfirmed` 結束。`board-redesign.md` §8 設計的是每天一則訊息。
4. **待辦只有派工一種來源**（TD-3）。`result.json` 沒有 `remaining`，交付收據只有一句話，obligation 表正在退役（D07）。
5. **`repeated_failure` 沒有人接**（TD-7）。`board-redesign.md` §6 要它變成「卡住：重試／換方法／放棄」的決定或提議。
6. **待辦移交沒有接手的人，也不會被帶回 session**（TD-8、TD-10）。`handed_to` 沒有寫入者；`/v1/orchestrator/handoffs`
   存在但不移動待辦；`app/orchestrator/brief.go` 與完成通知都不帶待辦。
7. **看板項目沒有「無人負責」**（BD-15）。owner 的對話消失之後，項目照舊顯示那個 owner。
8. **`commitment` 有兩個值沒有人寫**（BD-1）。`decision`、`delivered` 在 CHECK 裡，程式只寫 `dispatch`、`assigned`、`scheduled`。
9. **待辦沒有容量登記**（DG-2）。`internal/domain/capacity` 沒有 `todos` 的列，而它每個派工多一列、永遠保留。
10. **GitHub 這一側都還沒做**（GH-2、GH-4～GH-6、BD-16）：issue form（G1，repo 替換那一步）、`link`／`unlink` 與
    `Fixes` 提醒（G3，T 線之後）、`duplicate` 與短編號（G5）。
11. **run 證明人說過話，證明不了是哪個 session 在代轉**（CM-2）。orchestrator token 是整台機器共用的（`local-token` 同一個使用者的程序也都讀得到），
    伺服器分不出呼叫的是哪個 session：任何 session 都能讀別的 session 的 run 並拿來代轉看板指令；`run_other_session` 只擋得住誤用。
    要分得出來得給每個 session 自己的憑證，由使用者決定要不要做。

## 10. 怎麼修改這份文件

**誰可以改**

- 改規則（新增、刪除、改門檻、改誰有資格做）：使用者拍板。任何 session 都可以提案，在 report 的 finding 或 PR 裡
  寫「BD-n 改成……，理由……」。使用者不在時預設不改，提案留在 report。
- 改狀態標記：程式落地的那一個 commit 裡，由落地的 root 改，不用拍板；同一個 commit 把 §9 對應的缺口刪掉。
  發現標錯了也一樣，寫進 report，由 root 改。
- §8 是 2026-09-19 的量測，只在重新量過時整段換掉，並寫上新的日期。
- 這份和 `design-decisions.md` 不一致時，先照 `design-decisions.md` 做，由 root 在同一個 commit 修正這份，
  或在那份加一列新的裁決。

**改了之後同一個 commit 要一起改的**

| 改的是 | 一起改 |
|---|---|
| 任何規則 | `design-decisions.md` 加一列裁決，或改它現有的 D、U 列：它仍是實作依據 |
| BD、BL、CM | `internal/domain/work/board.go`（`SweepRules`、`Decide`）與 `board_test.go` 裡一個先紅後綠的 fixture（DG-8）；`internal/app/board_work.go`；`internal/transport/http/work.go` 檔頭的路由與權限；`internal/adapters/store/work.go` 的 CHECK 與 trigger |
| TD | `internal/domain/work/todos.go`、`internal/app/orchestrator/todos.go`、`internal/adapters/store/todos.go` 的 CHECK，各自的測試 |
| PT | `internal/domain/work/proposals.go`、`internal/app/proposals.go`、`internal/transport/http/proposals.go` 檔頭 |
| 時鐘與門檻（3 天、7 天、24 小時、30 天、30 分鐘） | `DefaultPolicy`、`DefaultProposalPolicy`、`DefaultDecisionPolicy`、`DefaultDigestPolicy`、`DefaultStall`、`UnownedGrace`、`LongLived` |
| 上限 | `internal/domain/capacity` 的登記列、`internal/adapters/store` 的常數、`limits.md` |
| session 能做什麼 | `skills/clawdline/guide.md` 與 `guide.zh-TW.md` 的 §10 |
| 畫面上的字 | `web/console/src/pages/work/words.ts` |
| GH | `github-issues.md` §7 的拍板列 |

**上限**（照 `design-guidelines.md` §1：有上限、滿了要說淘汰哪一條、由使用者決定）

- 全文最多 400 行。
- 物件固定四個。要加第五個，先說它取代或併入哪一個，由使用者拍板。
- 每個物件最多 18 條規則，CM 與 PT 各最多 10 條，§9 最多 12 個缺口。
- 到頂時，新增一條的同一個修改要指名淘汰或合併哪一條；說不出來就不加。
- 缺口補上就從 §9 刪掉，不留「已修好」的歷史，歷史在 git 裡。
- 檢查：第一行印行數；第二行在「規則數」和「狀態標記數」不相等的列印出列號，什麼都不印才算過。

```sh
f=docs/work-system.md; wc -l < "$f"
awk '{ r = gsub(/\*\*(BD|TD|BL|GH|PT|CM)-[0-9]+\*\*/, "&"); s = gsub(/〔(已實作|部分|只是設計)/, "&"); if (r != s) print NR ": " r " rules, " s " tags" }' "$f"
```
