# 審查結論

需要修改（changes_required）。這是凍結版本的原始碼審查；以下沒有任何已執行的執行期驗證主張。

## 身分與範圍

- BASE：792d5b884474289c12280f6c6168cd9c5ff81239
- HEAD：e490f91572aea31f14a997700989fd523a4d6677
- tree：f892be8ec6824c79cb00065eac6e13b5550128f0
- 已執行 git rev-parse、git diff --stat、git show、git diff 及限定 checkout 的 rg/sed；HEAD/tree 完全符合 briefing，並讀取改動來源檔 internal/transport/http/work_v2.go。
- 18 個改動檔，3997 additions、187 deletions。CROSS-SURFACE：A 涉及 HTTP/API、Console/Session UI、SQLite schema、文件及容量診斷；B 涉及人與 Agent 權限、破壞性 reset、收據及停止背景工作；C 非測試新增來源超過 800 行。Cloud wire 與 CLI 本身未改，但為必要相依面。
- 行為主張取自 candidate commit：「Work items now stay human-authored from creation through delivery」，內文主張人建立項目、獨立 person/Agent authority、Console 使用新模型、停用舊 sweep、reset 僅限本地且受計數確認。
- 實際角色來源：clawdline.persona.code-reviewer；版本 sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead；snapshot OMITTED。來源為本次 launch personas/code-reviewer.md。
- 已讀並套用 clawdline.skill.two-axis-code-review，版本 sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d；read/applied 收據成功。

## 已確認的原始碼缺陷

這裡的「確認」指控制流程／資料依賴可由來源推導；實際 UI、當機與競態結果均未執行。

1. **blocking — Console 切換 v2 後，Cloud 無對應讀寫通道。信心：高。**
   - 位置：web/console/src/pages/work.tsx:510；web/console/src/pages/work/api.ts:355；web/console/src/pages/work/api.ts:386。
   - 必要的未改連接：web/console/src/cloud/relay-reader.ts:685 僅有 v1 work routes，:745 的 default 回 cloud_not_carried；:480 對無 writer 的非 GET 也拒絕。internal/app/cloudops/ops.go:850 至 :919 只路由五個 v1 work 讀取；relay-writer 無 v2 mapping。
   - 後果：共用 Console 入口已無條件使用 WorkV2Page，Cloud 的 items/proposals/Session to-do 呼叫無法抵達新 HTTP handlers；WorkV2Page.tsx:36 的 Promise.all 因此不能完成新看板讀取，寫入亦無通道。這是來源推導，未以正式 Cloud 或瀏覽器重現。
   - 修正：補齊 Console relay、daemon vocabulary/route、命令能力與必要權限兩端，再切換入口；若本輪限定本地預覽，保留明確受控入口，不要使 Cloud 使用未承載的 v2 頁面。
   - 映射：Console 使用新模型且不破壞必要連接。INTENDED? 作者須確認是否刻意接受本輪 Cloud 看板與 Session 待辦失效；未因「implemented locally」而略過此缺口。

2. **blocking — 既有 Session 指派沒有 durable briefing outbox。信心：高。**
   - 位置：internal/transport/http/work_v2.go:540 至 :555。
   - Assign 先以 nil filer 提交 ownership，接著直接 actions.Send，最後另行 CompleteReceipt。兩次持久化與終端 I/O 分離，沒有同交易的待送效果；讀取 docs/work-system-v2.md:240 及 acceptance :100 的原子 outbox／重啟契約後，來源不符合該契約。
   - 後果：在 Assign 返回與 Send 之間停止 daemon，項目已指派但沒有可恢復的 briefing intent；在 Send 後、receipt 前停止也無 durable begun/settled effect 可判斷是否已輸入。一般 send error 雖嘗試設 assigned_unnotified，不能涵蓋程序停止。
   - 修正：在 ownership 交易中同時記錄結構化 outbox intent 與 operation identity，透過既有 broker effect 的 begun/unknown/settled 流程送出及恢復；加入契約指定的兩個停止點驗證。
   - 映射：人建立／指派到 delivery 的可靠連接、WS2-S02。未執行停止注入。

3. **blocking — 直接待辦先輸入終端，之後才拒絕已完成／已讀狀態。信心：高。**
   - 位置：internal/transport/http/work_v2.go:731 至 :756；internal/app/work_v2.go:766 至 :786。
   - send handler 用 includeCompleted=true 找 row，未先檢查 completed_at/read_at/sent_at，:748 已執行 Send，:753 才呼叫會拒絕 completed/read、對 sent 僅回舊 row 的 MarkDirectTodoSent。
   - 後果：以新 request key 對已完成或已讀待辦呼叫 Send，文字仍送出但 HTTP 隨後回拒絕；對已送但未讀 row 用另一 key 可再次輸入同文。UI 隱藏按鈕不能保護 API，也不能避免並行狀態變更。
   - 修正：在終端效果前以交易驗證並保留可送狀態，綁定每筆待辦的唯一 durable send effect，再更新收據；拒絕的請求不得造成 terminal effect。
   - 映射：WS2-T02 與人／Agent action 契約。這是順序推導，沒有實際送訊息。

4. **blocking — 破壞性 reset 的計數確認與刪除不在同一交易。信心：高（競態需執行驗證）。**
   - 位置：internal/transport/http/work_v2.go:994 至 :1019；internal/adapters/store/work_v2.go:849 至 :878。
   - handler 先讀 WorkV1Counts 並比對 token，再呼叫 ResetWorkV1；後者才開寫入交易並重新計數，卻不接受或比對預期 counts。v1 路由仍編譯且可寫（internal/transport/http/work.go:237 起）。
   - 後果：兩者之間新建立或改動的 v1 row 可被刪除，雖然它不在剛通過的確認計數裡；BEGIN IMMEDIATE 僅保護 reset 交易內，不能保護先前 token 檢查。
   - 修正：將 expected counts／confirmation 帶入 ResetWorkV1，於同一寫入交易內計數、比較、再刪除；不相符即 rollback。以同步屏障插入 v1 writer 驗證零刪除。
   - 映射：commit 的 count-confirmed reset 主張。實際競態與破壞性 reset 未執行。

5. **blocking — required 部署完成僅以任意非空字串通過。信心：高。**
   - 位置：internal/app/work_v2.go:465 至 :467；internal/domain/work/v2.go:273 至 :281；internal/transport/http/work_v2.go:839 至 :855。
   - HTTP 接收 deployment 字串，app 將 TrimSpace != 空字串轉成 hasDeployment，domain 對 required 僅檢查該布林值。沒有解析部署目標、成功結果或健康證據。
   - 後果：owner 在 deploying 時填 deployment="failed" 也符合完成條件，項目被關閉並移除 owner projection；不符合 docs/work-system-v2-acceptance.md:165 的 successful deployment and health evidence，也不能支持已交付的完成狀態。
   - 修正：以可驗證、綁定 item/cycle/target 的部署與 health evidence 判斷完成，拒絕 failed/unknown；保留 agent_decides 的明確不適用理由。
   - 映射：from creation through delivery、WS2-L04。沒有實際發部署或執行此請求。

## 未解意圖問題，並非僅憑意圖判為缺陷

6. **non_blocking / INTENDED? — 停用舊 Board sweep 是否也要停止等待通知？信心：高（呼叫連接），產品意圖待確認。**
   - 位置：internal/transport/http/work.go:77；internal/transport/http/waiting.go:84；internal/app/waiting.go:16。
   - StartWork 現在不做任何事；全 checkout rg 只找到 watchWaiting 的定義，waiting().Observe 的唯一呼叫也在該未啟動 loop。
   - 若非刻意退休此功能：Session 長時間停在問題上，不再由這個 watcher 記錄或送出提醒，與禁止 broker 自動建立 Board content 是不同功能。
   - 具體處理：作者確認是否退休等待提醒；若保留，從獨立啟動邊界啟動 watcher，維持原預算、去重與 outbox。沒有將這個未解意圖單獨當成 acceptance failed 的依據。

7. **non_blocking / INTENDED? — 新 UI 與 Agent 引導是否刻意分期交付？信心：高（可見來源），範圍待確認。**
   - 位置：web/console/src/pages/work/WorkV2.tsx:97 至 :114；web/console/src/session/Todos.tsx:73 起；docs/work-system-v2.md:226、:319、:368。
   - WorkCard 僅呈現卡片與指派，沒有 item detail/document/step/history 導覽；Session assigned row 沒有 item link。cmd/clawdline 的 v2/workV2 搜尋沒有對應命令，仍須核實另一路徑是否提供文件所述薄 CLI／turn 讀取引導。
   - 若宣稱整份 v2 已可用：人無法從這些新畫面查看保留的證據／文件／步驟，Agent 的標準入口也未證明接到新模型。
   - 具體處理：作者列明本輪可接受的部分交付，補 item detail/link 與已驗證的 Agent 入口，或保留切換前入口直到必要畫面可用。這是未解範圍問題，未作獨立 confirmed failure。

## 已核對為正確的連接（僅限來源）

- 新路由有掛載：server.go:330 → work.go:237 → workV2Route；新 Console API 使用相同 v2 路徑。
- 人的寫入透過 requirePersonWorkV2 拒絕 machineAuthed，並要求 maySend；maySend 檢查 Send capability（gate.go:246）。
- Agent 路由要求 machineAuthed；Advance 在 app/work_v2.go:447 要求 owner session 相符。共享 credential 的合作式身分限制已由 docs/work-system-v2.md:226 明文承認，未誤報為新增的 per-session sandbox。
- Store 的 Create/Edit 等 filer 可與項目／event 在同一 WorkV2Tx 完成；既有 Session assignment 是上述例外。
- planning kind 在 app Assign :257 被拒絕；domain AgentTransition 拒絕無 owner 與 terminal item。done 在同交易釋放 assignment、清除 owner。
- reset handler 確實先呼叫 requireLocal；auth.go:241 的判斷要求 Allowed && Local。ResetWorkV1 刪除列表不含 work_v2 與 broker tables；這不等於已跑保留性／rollback 驗證。
- StartWork 確實不再啟動 v1 sweep；這只能證明該啟動連接移除，不能證明所有 v1 API 都不會建立資料。
- 讀取了 domain、app、store 的新增測試內容；它們涵蓋基本 authority、kind/lifecycle、資料／mark read/reset。沒有執行，因此 baseline red、全套綠燈、瀏覽器與 failure injection 均未證明。
- 新增獨立 v2 模型有既有 normative design 支持；未找到能直接以較小替代保留所有 authority/evidence/failure 語意的現成 helper，不提出純行數簡化。

## 驗收狀態與限制

- commit 的人建立、分離權限、使用新 UI：部分來源可對應；必要 Cloud 連接失敗，不能整體 passed。
- count-confirmed reset：failed（來源推導的交易間隙）；local-only：來源支持，執行期 unverified。
- reliable assignment／delivery：failed（來源推導的 outbox、send 順序、部署證據缺口）。
- commit 所述隔離 daemon port、空 v1 counts、已建 Feature、icon 與「未刪 live rows」等歷史執行主張：unverified；未讀取其他實驗證據或採信 commit 自述為執行證據。
- 沒有執行 build/test/browser/network/dispatch/付費 API，符合此任務限制；verification.last=skipped。角色規則要求的測試 baseline red 與 required gates 無法在本輪限制下檢驗。
- 檢查的相依路徑包括：HTTP→app→store→domain；Console Board/Session→API→local handlers；Cloud reader/writer/catalog→本地路由；指派→terminal Send/receipt；reset→counts/SQL delete；daemon StartWork→waiting watcher；文件→現行實作／測試。
- 這不是 3997 行的逐行完整安全稽核；跨平台、真實瀏覽器、正式 Cloud、時序、程序重啟、既有資料 migration、其餘 failure-injection matrix 與新測試是否能先紅均仍 unverified。
- 無修改來源、無 commit、無部署；移除 worktree 0 個，保留指定 frozen checkout 供 root 驗證。Fixed but not yet released (awaiting review)：Nothing。

