# 歷史候選審查

判定：changes_required。三項 blocker、一項 should-fix；全部是原始碼推論，未執行程式或重現執行期結果。

## 身分與邊界

- 工作副本：指定的 `1-control/case`，未讀其他實驗目錄、既有報告、ground truth 或目前主 checkout。
- `git rev-parse HEAD HEAD^{tree}`：`5cdb3e42365f7a2adc9d7c88a4e07938f3b8c498`、`e29c7b645d9b47cd7a20137f98efc65e675e3569`，兩者相符。
- 比較基準：`ea63672eef4d24f88dd5dea085a38f0a1d77d458`。`git diff --stat`：20 files changed, 4218 insertions(+), 17 deletions(-)。`git status --short` 無輸出。
- 行為規格來自候選 commit message，沒有把抽出的 round metadata 當成實際 gate 指令。
- 已讀變更來源：snippet domain/store/HTTP/API/UI、bridge、composer/detail、icon/capacity 差異；已追相關未變更連接：gate、receipts、SQLite 開啟、Cloud install/reader/writer、failure-text、toast、main 入口及部分測試。這不是所有檔案逐行或完整測試覆蓋的宣稱。
- 只執行 git/rg/sed 讀取與任務簽收/完成協定。沒有 build、test、瀏覽器、外部 API 或委派。

## 發現

### F1 — blocker：Cloud 缺少明確的不可用說明

- 位置：`web/console/src/session/Snippets.tsx:174`。
- 錯誤與後果：Cloud 的 GET 確實會得到 501 `cloud_not_carried`，但 sheet 直接呼叫舊 `failureSentence(failure, T.webRequestFailed)`。舊表沒有此 code；未知碼只會得到「請求失敗（cloud_not_carried）」而不是提交承諾的 Cloud 未支援說明。這會把預期的功能缺席呈現為一般故障，沒有告知可在哪裡操作。
- 證據鏈：`web/console/src/cloud/install.ts:31` 攔截同 origin `/v1/`；`web/console/src/cloud/relay-reader.ts:294` 回 501；`web/console/src/session/snippets-api.ts:47` 保存 flat code；`web/console/src/legacy/js/core/failure-text.js:19` 的完整 SENTENCES 表至 `:81` 無此碼，`:109-116` 落入 fallback；`web/console/public/strings/zh-Hant.json:599` 的 fallback 是「請求失敗」。既有 `web/console/src/overlays/toast.ts:39-41` 已用 `nextWord("cloudNotCarried")` 處理相同缺口，但此 sheet 沒走那條路。
- 具體修正：共用既有 Cloud 說明 resolver，或在此呼叫傳入該碼的專用 sentence，保留 code tag 並涵蓋英文與繁中。加入 sheet 的 501 refusal 驗收，斷言專用文字及沒有新增按鈕。
- 類型：source-inferred；信心高。畫面未實際執行。

### F2 — blocker：機器 token 分支在真實 gate 後不可達

- 位置：`internal/transport/http/snippets.go:256`。
- 錯誤與後果：handler 宣稱允許機器 token 或 Send 裝置，但 `machineScoped` 沒有 `/v1/snippets`。只攜帶正確 orchestrator token、沒有 paired-device 憑證的請求，在進入 handler 前便會被拒絕，不能使用宣稱的機器 token 寫入路徑。
- 證據鏈：`internal/transport/http/gate.go:251-260` 僅對 machineScoped 路徑驗證機器 token；`:322-343` 的允許名單沒有 snippets。`internal/domain/auth/auth.go:9` 明示 loopback 不是授權訊號。`internal/transport/http/snippets_test.go:52-74` 人工注入 access 後直接呼叫 handler，沒有通過 `Server.Handler()`，因此不能捕捉此連接缺口。
- 具體修正：依宣稱將精確 snippets 路徑納入 machine scope，保持 device Send 檢查；以完整 Handler 加上有效機器 token、無 device cookie 的整合測試覆蓋四種寫入及無效 token 拒絕。若產品本意不允許機器 token，應移除相關承諾與死分支，並明列契約變更。
- 類型：source-inferred；信心高。HTTP 回應未實際執行。

### F3 — blocker：回覆遺失後再按儲存會換鍵並重複新增

- 位置：`web/console/src/session/snippets-api.ts:95`。
- 錯誤與後果：每次 `createSnippet` 都呼叫 `writing` 產生新 Idempotency-Key。若第一筆 POST 已成功寫入、回應卻遺失，sheet 保留原稿並允許再按儲存，第二次 POST 使用新鍵；後端收據無法辨認這是同一次決定，會再新增一份。這違反「retry after a dropped connection must not leave a second copy」與 API 檔頭的明確宣稱。
- 證據鏈：`web/console/src/session/snippets-api.ts:92-106` 每次產生鍵；`:63-67` fetch 失敗丟 offline；`web/console/src/session/Snippets.tsx:255-263` 只顯示錯誤、解除 busy，沒有關閉 editor 或保留 request identity；`:382` 再次儲存重新呼叫 create；`internal/transport/http/snippets.go:278-280` 收據以提供的 key 區分請求。
- 具體修正：將鍵連同精確 method/path/body 保存在一次待確認的儲存操作內，結果不明時重送同一鍵及 payload；修改內容時建立新的操作，並處理原操作的未知結果。用「伺服器完成 POST 後丟失回覆，再按儲存」的 failure injection 驗收只留下單一 snippet。
- 類型：source-inferred、以回覆遺失為觸發條件；信心高。未執行 failure injection，未聲稱已發生重複。

### F4 — should-fix：StrictMode 的 effect 重播會讓讀取結果永久被忽略

- 位置：`web/console/src/session/Snippets.tsx:143-144`。
- 錯誤與後果：effect cleanup 將 alive 設成 false，但 effect setup 沒設回 true。入口有 StrictMode；在其開發模式 setup/cleanup/setup 檢查下，同一 ref 會保持 false，`:159` 與 `:169` 都直接返回，fresh sheet 的 loading 不會結束。Cloud 的專用拒絕也無法畫出。
- 證據：`web/console/src/main.tsx:63-73` 的兩個入口都包 StrictMode；`web/console/src/session/Snippets.tsx:143-169` 的 ref 生命週期與守衛。
- 具體修正：effect setup 設 alive.current=true，再回傳設 false 的 cleanup，或讓取消旗標屬於各次讀取 effect。以 StrictMode 掛載 sheet 驗收成功與失敗讀取都能離開 loading。
- 類型：source-inferred，限開發模式的 React effect 重播；信心高。未執行 React，不能據此聲稱正式 bundle 也卡住。

## 提交宣稱核對

| 宣稱 | 結果 | 證據與限制 |
| --- | --- | --- |
| 精確候選 commit/tree | passed | 實際執行 git rev-parse，兩者相符。 |
| 原始輸入以 UTF-8 bytes 驗證 | passed（source） | `internal/domain/snippet/snippet.go:145-184` 先量原字串，再由 HTTP snippetFields 正規化。未執行邊界測試。 |
| daemon 自己的 SQLite、舊 snippets 目錄不被本路徑寫入 | passed（source） | 新 schema/openSnippets 與 store 寫入都用現有 daemon DB；沒有執行磁碟遷移或全程檔案監測。 |
| 數量與位置在 transaction 內決定、限制拒絕而不淘汰 | passed（source） | store CreateSnippet 的 count/limit/position/insert 都在 s.write closure；SQLite DSN 設 immediate transaction。未執行競爭驗證。 |
| 10 writes / 10 minutes，重啟保留 | passed（source） | `internal/adapters/store/snippets.go:371-387` 在 transaction 內讀寫 snippet_writes。未執行重新開啟或時間邊界。 |
| 四種寫入可用 Send 或機器 token | failed（source） | F2。 |
| 收據持久化、UI 斷線重試不重複新增 | 部分 passed / failed（source） | handler 使用既有 receipted/store 收據；UI 未保留鍵，F3。未執行 restart。 |
| 兩個入口與點選只填 composer | passed（source） | Detail 的 header/menu 派發 open 事件；Snippets insert 只派發 compose append；Composer listener 呼叫 dictation 的 sink。未實際點選。 |
| registry → git common-dir → cwd 順序 | 部分 passed（source） | HTTP snippetProject 有此順序及雙側 spelling；本輪未完整追 git adapter 執行，因此不把 common-dir 的實際結果列為 passed。 |
| legacy byte-for-byte/原畫面完全相同/遷移報告齊備 | unverified | 沒有逐位元比對原 app 檔案、跑 UI 或檢閱未列入 diff 的報告；不可由 comments 直接認定。 |
| Cloud 無 snippet write、GET 501、sheet 明確說明且無 + | 部分 passed / failed（source） | reader/writer 無 snippets 路由，reader 501；UI error 會隱藏 +，但專用說明缺失，F1。dev StrictMode 另見 F4。 |
| 端到端與部署行為 | unverified | 任務禁止 build/test/browser/network；未建立 relay、未驗證任何部署。 |

## 專案約束、測試與簡化

- 凍結 clone 沒有根目錄 AGENTS.md（指定讀取回報不存在）；使用目前 briefing 與既有的專案指示，沒有讀取其他實驗來源補規格。
- 看過的 domain/store 測試有資料結果與拒絕碼斷言，並非單純源碼字串鏡像；完整 gate 與 UI 重試/Cloud 文案缺少本輪可見的對應覆蓋。所有測試均未執行，沒有證明新增測試在舊版會失敗。
- 聚焦簡化：F1 可沿用現有 `nextWord("cloudNotCarried")` 文案來源。未確認其他可安全刪除的結構；沒有為了行數提出改寫。
- 無額外 nit、沒有另列未確認的範圍外問題。產品意圖唯一待釐清之處是機器 token 是否真的要被支援；提交目前明文宣稱支援，因此按缺口評級。

## 角色來源與收尾

- 實際角色：`clawdline.persona.code-reviewer`，版本 `sha256:1d280ec7e3fd4459cd460a7011e197fbfbf7cf4e9a988f49bb91998a1a63aead`，固定 squad snapshot `OMITTED`。
- 已完整讀角色來源 `REDACTED_LOCAL_PATH`。
- 已讀並套用內含 two-axis-code-review 技能，版本 `sha256:1221272ac73f36a5323171093d7c8534cfebe90c82a93a0c8721ef5b41bb398d`。未呼叫 squad skill-event；任務將可寫範圍限制為正常完成檔案，並禁止網路，故沒有技能使用收據。
- Repository 修改、commit、worktree 清理、安裝及部署皆為 0；本任務不授權這些操作。公開或付費 API 請求數 0，費用 0。
- Fixed but not yet released (awaiting review)：Nothing。本輪只審查，沒有修正。
