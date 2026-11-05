# 凍結候選審查

結論：needs rework／changes_required。以下 8 項均為原始碼推論，沒有執行期重現；沒有將未執行的測試列為通過。

## 身分與範圍

- checkout：`CASE_DIR`
- 實際 `git rev-parse HEAD HEAD^{tree}`：`e490f91572aea31f14a997700989fd523a4d6677`、`f892be8ec6824c79cb00065eac6e13b5550128f0`，符合指定值。
- `git diff --stat 792d5b884474289c12280f6c6168cd9c5ff81239..e490f91572aea31f14a997700989fd523a4d6677`：18 files changed, 3997 insertions(+), 187 deletions(-)。`git status --short` 沒有輸出。
- 行為主張取自 candidate commit：人建立的 Project 項目持續經過指派與交付，Console 顯示圖示和 Session 責任，v1 reset 維持本機限定及筆數確認。
- 實際角色來源：`REDACTED_LOCAL_PATH`；definition `clawdline.persona.code-reviewer`，version `sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead`；snapshot `OMITTED`。
- 未讀角色列出的外部 skill 檔案：本任務只允許 clone、自己的 CHILD.md 與角色檔，未宣稱 skill 已套用或已記錄收據。
- 範圍限制：未建置、測試、瀏覽網路、呼叫付費 API、派工或修改 repository；只執行必要的 accept/finish 協定及寫完成資料。協定偏差：有兩次唯讀搜尋管線使用 `head -100` 截短輸出，超出 git/rg/sed 工具白名單；沒有因此讀取其他資料來源。

## 發現

### F1 — blocker：執行項目永遠不會出現在看板

證據：`web/console/src/pages/work/WorkV2.tsx:79` 只渲染 `item.area === "execution"`，`:100` 的指派控制也使用相同判斷。實際 wire 在 `internal/transport/http/work_v2.go:139` 直接採 `i.Area()`；`internal/domain/work/v2.go:209-218` 對未指派項目回傳 `unassigned`，對已指派項目回傳實際 phase，完全沒有 `execution`。`web/console/src/pages/work/api.ts:314` 的型別把錯誤假設固定成 execution/planning。

後果：建立 Feature/Issue 後，開啟中的卡片不在任何欄位，無法從這個頁面指派；即使透過 API 指派，卡片仍不會顯示，圖示和責任也無法在看板驗收。修正：統一 Area 的 wire/type/分欄契約，涵蓋 unassigned 與每個執行 phase，並以真實 API 回應驗證建立至指派的畫面。信心：高。

### F2 — blocker：v2 項目接不到既有派工與 landing 證據

證據：`internal/app/work_v2.go:124` 透過 `CreateItem` 建立 v2 列，`internal/adapters/store/work_v2.go:249` 寫入 `work_v2_items`。未修改的 `internal/app/orchestrator/dispatch.go:164` 仍呼叫 `checkNamedWork`，而 `internal/app/orchestrator/lines.go:175` 只讀 `b.Store.WorkItem`；`internal/adapters/store/work.go:425` 是 v1 工作表讀取。新階段 API 的 `internal/app/work_v2.go:450-465` 又要求 broker task 綁定該 v2 id 的 landed facts；`internal/adapters/store/work_v2.go:207-209` 用 task record 的 work_id 連接。

後果：正常新建 v2 id 不存在於 v1，child dispatch 明確指定該 id 時會在派工前遭拒；省略 id 又不會綁回 v2 項目，後續正常 merge 證據無法讓項目前往 deploying。修正：讓 broker 的命名驗證、歸屬檢查及提交流程以 v2 為準，並驗證指定 v2 id 派工、landing、前進階段的完整連線。信心：高；未宣稱所有其他綁定入口都不可繞行。

### F3 — blocker：Agent 寫入沒有重新確認 live Session 身分

證據：`internal/transport/http/work_v2.go:782` 只檢查共用 machine credential，`:853` 把 request body 的 session_id 直接交給 Advance；edit 路徑 `:435-449` 也直接使用 body session_id。`internal/app/work_v2.go:447` 只比對儲存的 owner 字串。未修改的 `internal/transport/http/gate.go:253` 的 machineAuthed 僅為布林值，不解析 Session。`docs/work-system-v2.md:226-230` 明訂共用 credential 是合作式邊界，但仍要求 server 對 live identity 和 active assignment 重新驗證；acceptance WS2-A02 要求 stale reading 回 owner_identity_unknown。

後果：已消失或無法辨識的 owner Session 仍可憑過期 id 推進、編輯；身分未知不會按約定停止寫入。這不是要求把既定合作式模型改成防惡意偽造的安全沙箱。修正：在 Agent 操作前解析完整、最新的 live identity 並對 active assignment 檢查；未知時回 typed refusal。信心：高。

### F4 — blocker：指派提交與簡報傳送之間沒有可恢復的 durable effect

證據：`internal/transport/http/work_v2.go:540-546` 先 Assign（filer=nil）提交 owner，再直接 `actions().Send`；`:553-555` 到傳送後才 CompleteReceipt。新 Session 路徑 `:570-581` 的 pending assignment 和外部 root 請求也分開，隨機 requestID 未存入該 pending row，`:608` 才完成本地 assignment。`docs/work-system-v2-acceptance.md` WS2-S02 要求 assignment 和 durable briefing effect 在 crash/restart 後仍能精確結算。

後果：程序在 owner 提交後、Send 前退出，既有 Session 留下已指派但未收到簡報的工作，沒有待補發 outbox；在 Send 後、receipt 前退出則沒有可回放的完成答案。新 Session 的 crash 可留下無法由本地 intent 關聯恢復的 assigning 狀態。修正：在 assignment 交易中保存穩定外部操作 id、receipt 和 durable effect，交由既有 outbox 執行與復原，對傳送已開始但結果未知的狀態禁止盲目重送。信心：高；crash 後精確錯誤碼未執行驗證。

### F5 — blocker：確認筆數後仍可能刪除未經確認的新資料

證據：`internal/transport/http/work_v2.go:994` 在 handler 先讀 counts，`:1015` 比對 confirmation，`:1019` 才呼叫另一個交易 ResetWorkV1。`internal/adapters/store/work_v2.go:849-874` 在刪除交易中重新讀筆數，卻沒有接收或比較核准的 counts/hash。v1 路由仍存在（`internal/transport/http/work.go:234` 後的 switch），broker 的 v1 todo producer 也仍存在（`internal/app/orchestrator/todos.go:67-82`）。

後果：有寫入發生在 handler 計數之後、reset 取得寫鎖之前時，新的列也會被 DELETE，而舊 confirmation 仍成功；違反 commit 的 count-confirmed 破壞性操作主張。修正：將預期 counts/hash 傳入 reset，在同一 BEGIN IMMEDIATE 交易中確認全部筆數，任何差異都 rollback 並要求重新預覽。信心：高；具體競態排程為 source-inferred。

### F6 — blocker：取消中的新 Session 指派可以晚到並重新掛上 owner

證據：`internal/app/work_v2.go:330-338` 的 FinishAssignment 只確認 pending row 狀態，不確認 item terminal/cycle 或 intent 是否仍有效；`:363-364` 成功時直接設定 active assignment 與 owner。`Cancel` 的 `:518-528` 只釋放 ActiveAssignment 並把 item 設 cancelled，沒有終止 assigning row。HTTP `internal/transport/http/work_v2.go:581-608` 在外部開啟 Session 的等待期間，允許另一個請求取消該 item。

後果：取消完成後，較晚返回的開 Session 結果仍把 owner 掛到 cancelled item；若期間又 reopen，舊 cycle 的結果還可能認領新 cycle，背離人為取消與重開的權限。修正：把 pending intent 綁定 cycle 與有效狀態；取消時撤銷 intent，FinishAssignment 在交易內拒絕已失效或跨 cycle 的完成，安全處理已開啟的外部 Session。信心：高。

### F7 — blocker：停用舊 sweep 同時切斷等待回答通知

證據：變更後 `internal/transport/http/work.go:77-78` 的 StartWork 為空；base diff 顯示移除了 `go s.watchWaiting(ctx, tick)`。在凍結 clone 對 cmd/internal 搜尋 StartWork/watchWaiting，只有 `cmd/clawdline/main.go:162` 呼叫 StartWork，`internal/transport/http/waiting.go:84` 定義 watchWaiting，沒有其他啟動點。`:89` 的 Observe 是此 watcher 對 Session 等待狀態產生作用的入口。

後果：daemon 啟動後不再週期觀察 Session 等待回答，原本獨立於 Board 內容的等待通知功能停止；停用 Board 自動產生內容不需要取消這個連線。修正：把 waiting watcher 移到獨立的 daemon lifecycle 啟動點，保留其開關、取消與錯誤處理，再停用 v1 sweep。信心：高；實際推播未測。

### F8 — blocker：任意非空文字就能充當成功驗證或部署證據

證據：`internal/app/work_v2.go:465-466` 只用 TrimSpace 是否非空產生 hasVerification/hasDeployment；`internal/domain/work/v2.go:270-280` 直接使用這些布林值允許進入 merging 或 done。`internal/transport/http/work_v2.go:839-854` 接受任意 verification/deployment 字串，沒有成功狀態、health、Project、目標或 cycle 的驗證。`docs/work-system-v2.md:168-181` 要求 successful verification、valid deployment evidence，deployment failure 留在 deploying；acceptance WS2-L04 明訂成功部署與 health。

後果：verification="failed" 仍可讓 verifying→merging，required 部署時 deployment="failed" 仍可讓 deploying→done 並釋放 owner，失敗或未知可能被顯示為已完成。修正：以結構化成功狀態與可驗證的 evidence reference 取代非空判斷，驗證其 Project/item/cycle/target 及 health；保留失敗記錄但禁止成功前進。信心：高；不推定 Agent 會惡意輸入。

## 已核對正確與驗收狀態

| 主張／檢查 | 狀態 | 證據與界線 |
| --- | --- | --- |
| 指定 commit/tree、diff 身分 | passed | 實際 git 輸出如上；已讀 changed source |
| 人的建立入口拒絕 machine credential | passed（靜態） | `internal/transport/http/work_v2.go:171-180` 明確拒絕 machine 並要求 maySend；不是 HTTP 實測 |
| Planning 種類不進入 assignment | passed（靜態） | `internal/app/work_v2.go:256-257` 的 planning guard；沒有執行測試 |
| v2 正常寫入使用版本比對與 append-only events | passed（靜態、有限範圍） | `internal/adapters/store/work_v2.go:263-284` 的 version 條件與 append event；store test 的相關斷言已閱讀，未證明它們執行或可紅 |
| reset 的 local token 限制 | passed（靜態） | handler `:991` requireLocal，`internal/transport/http/auth.go:241-252` 檢查 Allowed/Local；未做 HTTP 測試 |
| Console 顯示完整執行卡與責任 | failed | F1 |
| 工作經正常派工到可驗證交付 | failed | F2、F3、F4、F6、F8 |
| reset 對精確確認筆數進行刪除 | failed | F5 |
| 既有必要連線不受影響 | failed | F7 |
| commit 宣稱的 isolated daemon、built Console、sweep interval、catalog icon 實測 | unverified | 任務禁止 build/test/runtime/network；commit 自述不視為本次執行證據 |
| 未刪 live rows、未 release 的歷史操作主張 | unverified | 凍結原始碼無法證明作者當時執行了什麼 |
| 新測試會先紅後綠與完整失敗注入 | unverified | 只讀測試；任務禁止執行，不以缺證據算通過 |

沒有額外的 intent 問題，也沒有列出範圍外問題。簡化檢視：不建議另造平行派工器；F4 可使用既有 durable outbox，但其替代接線仍需實作及失敗注入，沒有聲稱已驗證可直接替換。

本次沒有修改程式、commit、安裝、部署、發版或建立 worktree；移除 worktree 0 個。Fixed but not yet released (awaiting review)：Nothing。
