判定：changes_required；先處理 F1。這是原始碼審查，沒有執行應用、測試或瀏覽器驗證。

## 身分與範圍

BASE：ea63672eef4d24f88dd5dea085a38f0a1d77d458

HEAD：5cdb3e42365f7a2adc9d7c88a4e07938f3b8c498

Tree：e29c7b645d9b47cd7a20137f98efc65e675e3569

在指定 checkout 執行 git rev-parse HEAD HEAD^{tree}，兩值完全吻合；git diff --stat BASE..HEAD 顯示 20 檔、4218 additions、17 deletions。已讀取變更來源檔，符合工具檢查。未讀其他實驗目錄、先前報告、ground truth 或目前主 checkout。

採 CROSS-SURFACE：A 觸發，涉及 server/API、client/UI、persisted schema、auth integration、capacity diagnostics；B 觸發，新增需要讀寫權限的路由及刪除操作。亦有超過 800 行非測試新增程式碼，但 A/B 已足以決定模式。wire/relay、CLI、背景發布與 docs/translations 未在 diff 中修改，已沿必要相依路徑檢視，不能因此視作已完成整合。

角色來源：REDACTED_LOCAL_PATH；clawdline.persona.code-reviewer；version sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead；squad snapshot OMITTED。已完整讀取。角色列出的 two-axis-code-review skill 檔不在 brief 允許讀取的範圍，未讀取、未宣稱套用，也未寫入 skill-event 收據。

## Findings（4 項）

### F1 — blocking：宣稱支援的 orchestrator token 無法通過 snippets 外層閘門

分類：source-inferred confirmed；信心高；未執行 HTTP reproducer。

新增的 internal/transport/http/snippets.go:256 明確接受 machineAuthed 或 maySend，commit 也宣稱存在兩個寫入入口。但 internal/transport/http/gate.go:251 只有 machineScoped(p) 才驗證 orchestrator credential；gate.go:322 的完整 allowlist 不包含 /v1/snippets 或子路徑。只帶 orchestrator token 的請求於 gate.go:254–262 先回 401，永遠無法走進新增 handler 的 machineAuthed 分支。這不是 handler 能自行解決的拒絕；授權的 CLI／腳本寫入會失敗。

具體修正：將兩個 snippets path pattern 納入 machineScoped，或明確移除該 credential 契約並同步調整宣稱；補透過 Server.Handler 的權限整合案例，涵蓋正確 token、錯誤 token、read-only device 及四種寫入。現有 snippets_test.go:45–70 直接注入 access 並呼叫 handler，沒有覆蓋實際外層 gate，不能證明這項宣稱。

### F2 — should-fix / non_blocking：Cloud 缺席提示實際落到一般失敗文案

分類：source-inferred confirmed；信心高；實際畫面 unverified。

web/console/src/session/Snippets.tsx:174 將 cloud_not_carried 交給 failureSentence，fallback 是 T.webRequestFailed。未修改的 legacy/js/core/failure-text.js:19–84 的 SENTENCES 沒有 cloud_not_carried，detailedSentence 也不處理它；:113–116 因此使用一般 fallback。relay-reader.ts:293–295 確實回傳該碼。結果會保留技術碼，但沒有 commit 宣稱的「說明 hosted console 不提供此功能」之可理解句子。隱藏 ＋ 的條件存在，不等於缺席說明成立。

具體修正：為該碼加入明確的英文與臺灣繁中文案及對應映射，或在 snippets sheet 給它專屬 sentence；驗證該碼不再落到 webRequestFailed。不要直接顯示 relay 的英文 detail。

### F3 — should-fix / non_blocking：StrictMode effect 重跑後 alive 永久為 false

分類：plausible，source-supported；信心中高；development runtime unverified。

Snippets.tsx:143–144 初始化 alive=true，但 effect 只有 cleanup 將它改 false，setup 不恢復。main.tsx:64、:71 將應用包在 StrictMode。若目前 React 開發模式執行 setup→cleanup→setup，後續 refresh 的 success 及 failure 都被 Snippets.tsx:159、:169 丟棄，初始 loading 無法結束，last-message seed 也被 :337 擋住。這只推論適用於該 effect 重跑行為，不宣稱正式 bundle 已重現。

具體修正：effect setup 先設定 alive.current=true，再回傳 cleanup，或改成每次 effect 獨立取消／讀取世代。需要實際以 StrictMode 掛載並 resolve/reject 讀取，確認兩種答案都能離開 loading；本任務禁止 build/test，未執行。

### F4 — should-fix / non_blocking：寫入結果未知後重試無法沿用原冪等鍵

分類：source-inferred key-lifetime gap；「重複寫入」執行後果 plausible，信心高；斷線 runtime unverified。

snippets-api.ts:92–95 每次 writing() 都 mintKey；:105–112 每次 create/update 呼叫都重新執行 writing。Snippets.tsx:247–265 在失敗後保留編輯器，下一次儲存會再次呼叫 API，沒有保存第一次的 key。若第一次 create 已落地但回應遺失，再按儲存會以新 key 建立第二筆；後端的 durable receipts 無法辨識它是同一決定。這與 snippets-api.ts:8–10 宣稱 dropped connection retry 會沿用 key 不符。

具體修正：將 request body 和 key 保存到該次尚未確定結果的編輯決定中，重試相同內容沿用 key；內容變更才開新決定，或先查明結果並明確呈現 unknown。補「server 寫入成功、回應中斷、同一決定重試」的行為案例，確認只產生一筆。

## 已核對正確的來源連線

- 五個本機操作註冊在 server.go:258–259，分派到 snippets.go 的 list/create/order/update/delete；寫入 handler 均經 snippetWrite，檢查 send 與 Idempotency-Key。
- sqlite.go 在 Open 時建立 snippets schema；store/snippets.go 的 create、move、order、delete 與 rate brake 均放入 s.write transaction callback，沒有在 handler 先決定位置或數量。這是結構確認，並非已測試並行或重啟。
- domain/snippet/snippet.go 的 ParseFields 在 Normalized 前對原始 string 取 UTF-8 byte 長度，build 再檢查；ScopeAgrees 拒絕 global 攜帶 project；LimitRefusal 不執行 eviction。
- snippets.go:278 呼叫現有 receipted，receipts.go:64、:135 使用 store 的 ClaimReceipt/CompleteReceipt；沒有新增 process-local receipt map。持久性與 crash window 未實測。
- 專案判定從 snippetProject 到 Registry.MatchSpelled，再到 git.Toplevel；git/worktree.go:28–45 確實以 --git-common-dir 折回主 checkout。Registry.MatchSpelled 兩側使用同一 spelling，並提供 deterministic tie break。
- Detail 的 header 與 menu 都呼叫 requestSnippets；Snippets 的 insert→appendToComposer→Composer 的 COMPOSE_APPEND listener 接到既有 sink，所讀路徑沒有呼叫 send。
- CloudGate 安裝 readThroughRelay，install.ts 將本 origin /v1 fetch 轉給 RelayReader；reader 的 default 會以 501 cloud_not_carried 拒絕 snippets。sheet 有 error note 路徑，且 new button 於 error/loading 隱藏。僅確認分支，沒有瀏覽器驗收。
- diagnostics 的兩筆 measure 對到 SnippetCounts；capacity 新增 total/scope registry rows。測試來源涵蓋 byte bounds、scope、排序、上限及 rate brake；未執行，沒有 red/green 證據，亦沒有確認所有測試會於移除實作時正確失敗。

## INTENDED? author must confirm（不算已確認缺陷）

1. Cloud 讀取是否真的應繼續缺席？snippets-api.ts:103 新增 readSnippets，但未接 relay-reader switch。更深一層 internal/app/cloudops/ops.go:807–813 只有 snippets decode、沒有 route，cloudops.go:186 的 Implemented 排除它；cloudops_test.go:919 更要求它仍不可發布。legacy/js/net/cloud-client.js:3861–3916 已有 snippets snapshot/fresh-read consumer，但沒有接到新 store。本次沒有加入 background snapshot producer。若非刻意分階段，手機無法讀取新 daemon 已儲存的任何常用句。作者須決定是否補 reader seam、local route 及必要發布／fresh read；不把 commit 自述的缺席視為產品授權。

2. Cloud 四種寫入是否真的應缺席？legacy/js/net/cloud-client.js:3926–3942 已有 create/update/delete/order 的命令封裝；新 UI 的 SNIPPET_CONTROLS 卻全部 true，而 relay-writer 沒有相應 routes，Go cloudops 也沒有實作四個 mutation words。若非預期，遠端無法管理常用句。補接時須一併保留機器身分、send 權限及 durable request key，不能只加前端 methods。

3. read-only 裝置是否應看到所有寫入控制？Snippets.tsx:146 與 :442 硬編 readOnly=false，與 copied snippetActions 的 read-only 分支不同。若非預期，僅可讀的裝置仍可開啟編輯、刪除及新增，直到 server refusal 才知道不可寫；server-side send 檢查本身仍存在，這不是已確認的權限繞過。

## 驗收宣稱與結論

- commit 的「四個 write 可用 send 或 machine token」：failed（F1，來源證據）。
- commit 的「Cloud 缺席轉成說明句、無 ＋」：failed 部分為說明句（F2）；無 ＋ 的來源條件存在，實際畫面 unverified。
- commit 的「dropped connection retry 沿用 request key」：failed 於前端 key 保存契約（F4）；實際重複建立需失敗注入驗證。
- 本機資料儲存、byte bounds、scope、capacity、project resolution、append-only composer：來源結構相符；runtime acceptance 全部 unverified。
- 整體 acceptance「實作 commit 宣稱且不破壞必要連線」：failed，因 F1/F2/F4；Cloud 範圍意圖另外待作者確認，不以意圖問題單獨判失敗。

剩餘缺口：依 brief 未執行 tests/build/browser、real relay、並行、重啟、斷線或實際資料遷移。docs/cutover.md:230 與 docs/cloud-wire.md:706 仍描述本機沒有 snippets／GET 501；沒有新增所引用的 docs/snippets.md。這些是交付說明不完整的觀察，未加入額外 blocking finding。沒有新依賴或需要替換的獨立框架；本輪簡化檢視沒有可證明保留必要行為而能直接刪除的結構。

未修改 repository、未 commit、未建立或移除 worktree；移除數 0，保留指定實驗 checkout 供根任務驗證。Fixed but not yet released (awaiting review)：Nothing（本任務只有審查，未修正）。
