# 平台可靠性基準（W0-C）

此文件由 `tools/measure-platform-reliability.py` 產生；CLA-296 Plan v4 的量測輸入。
來源引用提交：`e23d2beae4b76bb565bef8ec18d6f7a57a8ca1aa`。
來源範圍 SHA-256：`2f3d6a302ab8a3bac5f141921ee1c6f5c69009500e74fc66984ddcc8f6490eee`。
工具 SHA-256：`f959cfb504d0f0d24fc8776688a057e7bbc94601f653b0a2d4302f943554ef11`。

15 個非空來源檔、19 列。這是範圍摘要，並非整棵 commit-tree 驗證。
整檔 seal 拒絕來源漂移；本工具不解析或編譯 Swift。更新 seal 前須重讀受影響行為。
未提供 probe 時保留 unknown；要求 probe 卻無資料、輸入缺失或 parse 失敗則非零退出。
existing test source 一律屬 source-derived；本工具未執行 Swift tests。數值是現行常數或該次觀測，沒有批准新預算。

重現靜態文件：`python3 tools/measure-platform-reliability.py --format markdown`。
核對文件：`python3 tools/measure-platform-reliability.py --check docs/generated/platform-reliability-baseline.md`。
聚焦測試：`node Tests/platform-reliability-characterization.mjs`。

選用 probe：`python3 tools/measure-platform-reliability.py --health-port 7717 --samples 3 --scratch <existing-private-directory> --format json`。
只讀 unauthenticated loopback health；不開 SSE、不做壓測、不讀真實 registry、不重啟。scratch fixture 會自行清除。
health 支援單一 Content-Length、chunked（含 extensions/trailers）或無長度的 close-delimited 回應；須收齊 framing 才記錄觀測。body 上限 16 KiB，chunk metadata 另以相同上限約束；皆為量測工具防護，非 production budget。
宣告或實收 body 超限為 health_payload_invalid；提前 EOF 為 health_response_incomplete；framing 衝突、不支援或 metadata 超限為 health_framing_invalid。失敗 exit 2，stdout 為空。

## cloud-ingress

commands 未指定 bufferingPolicy；acceptInbound 忽略 yield 回傳。bridge 逐筆 await consume，再 await commandRouter；worker 上限不等於 ingress 上限。

- **source-derived**：
  - `Sources/CloudTransport.swift:534–534`；slice SHA-256 `3181e2d4079b0797655422d52cd7477fb57ee07a00d7fd67fbe2bf884ca77247`。
  - `Sources/CloudTransport.swift:875–900`；slice SHA-256 `ce320174cc1cc0536c3ba07146563329008730bf1b7a27233fccc9850ed800ed`。
  - `Sources/CloudAppBridge.swift:431–439`；slice SHA-256 `21ba233e27062ca9cd21dd77dc3ae69407268b8ffddc0f76491e320f079d6980`。
  - `Sources/CloudAppBridge.swift:1063–1067`；slice SHA-256 `58504ef9ea6dd1627b87cd98de663707d38190e928e61b0d0e65ded4fca39c60`。
- **來源數值／屬性**：`{"explicit_byte_cap":null,"explicit_count_cap":null,"yield_result_handled":false}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：Swift 預設 unbounded 語意為來源推論，未執行 Swift；未量到實際 backlog/RSS、bytes、wait/work。 authenticated command 的穩定 retry identity、durable re-read 與 typed overload 尚須 R-1 設計。
- **接手 owner**：R-1 transport reliability owner。

## cloud-ready

readyGenerations 明定 bufferingNewest(1)，dropped 計數；與 commands 是不同流。

- **source-derived**：
  - `Sources/CloudTransport.swift:537–537`；slice SHA-256 `bbe2398002f751bf1f82dd2e82a09166dac984ffc7ca189f48c0d315cb166507`。
  - `Sources/CloudTransport.swift:685–693`；slice SHA-256 `6211c6dfa714182470fde62aec022d0ae20e9231cd1c202290828e8a2c0a4edd`。
- **來源數值／屬性**：`{"pending_generations":1}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：沒有執行 ready-generation 消費／丟棄測試；不能把此策略當成 command 可丟棄政策。
- **接手 owner**：R-1 transport reliability owner。

## http-admission

accept 直接 start/receive；這條 accept/receive 路徑沒有全域連線、aggregate body bytes 或 request-read deadline admission。response close grace 是另一階段。

- **source-derived**：
  - `Sources/RemoteServer.swift:609–614`；slice SHA-256 `809c25ceafa41dcf951f1a64aed7b8de55b5d7d97e2198ebd2e7ce86950a1c42`。
  - `Sources/RemoteServer.swift:619–672`；slice SHA-256 `a36633fb23f30d6079fca2bb082b6d35050af66525a3192f968ce1a6f5cb74b0`。
  - `Sources/RemoteServer.swift:5639–5639`；slice SHA-256 `f51d92d14db18adf58729af5350df5b81ae3ccfee46a076b7cbd7796bd31e283`。
- **來源數值／屬性**：`{"bodyLimit":20971520,"responseCloseGraceSeconds":30}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：NWListener/kernel 上限、同時連線數、記憶體峰值與 slowloris 行為未量測。 64 KiB 是未找到 header terminator 時的條件檢查，不能稱所有完整 header 的嚴格上限。
- **接手 owner**：R-2 HTTP reliability owner。

## sse-output

Stream 只保存 connection；openStream 登錄串流，授權檢查沒有容量條件；write 與 heartbeat 的 contentProcessed 忽略 error，沒有此層 outstanding-byte 計數、slow-consumer eviction 或待送 snapshot 合併。

- **source-derived**：
  - `Sources/RemoteServer.swift:5464–5469`；slice SHA-256 `aa83071c47cfb0303bcd1ac0652e35be5e485d5fc876cd63b03ccbe31da608ba`。
  - `Sources/RemoteServer.swift:5479–5527`；slice SHA-256 `ff07a45bf3f3c0c3ac3dea63dad3fa4448c9e5581e57aa017d6ff89f6be54ca1`。
  - `Sources/RemoteServer.swift:778–786`；slice SHA-256 `7f6beca1780fb6921227aff809fa9a9aa1a78be02872dc41f3a825c1fde71452`。
  - `Sources/RemoteServer.swift:5530–5546`；slice SHA-256 `1ea4c0fb7c2d0ba698647d66baef5d0f91e8a66da781e98b17dd6a4a26d9482c`。
  - `Sources/RemoteServer.swift:5628–5637`；slice SHA-256 `3fef466150f294172022c48b80a119ce8d4c048ba552e0dd620fdce6b779ea95`。
- **來源數值／屬性**：`{"connection_cap":null,"outstanding_byte_cap":null}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未開 authenticated SSE 或使真實 consumer 停讀；連線／待送 bytes、kernel buffer 與 callback 延遲未知。 Network 可能有下層背壓，來源不能證明本層已受保護；publication 合併不是 local SSE 合併。
- **接手 owner**：R-2 HTTP reliability owner。

## sse-reconnect

openStream 送 hello 與當前 sessions/orchestrator 全量 snapshot；write 分配 event id。重連以當前狀態 realign，這些路徑沒有 Last-Event-ID replay。

- **source-derived**：
  - `Sources/RemoteServer.swift:5479–5527`；slice SHA-256 `ff07a45bf3f3c0c3ac3dea63dad3fa4448c9e5581e57aa017d6ff89f6be54ca1`。
  - `Sources/RemoteServer.swift:5628–5637`；slice SHA-256 `3fef466150f294172022c48b80a119ce8d4c048ba552e0dd620fdce6b779ea95`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未測斷線重連、遺失事件與 viewer 觀察；snapshot 送出不等於 command effect 完成／ACK。
- **接手 owner**：R-2 HTTP reliability owner。

## terminal-queue

同一 serial terminal worker 在入列前計 total/per-channel outstanding；HTTP 容量拒絕為 429 busy，maintenance 為 503 restart_maintenance；nested inline 仍計新增 channel。

- **source-derived**：
  - `Sources/RemoteServer.swift:215–273`；slice SHA-256 `0719c79016208248b2d666ec43ad4ca63ffc63e2d8ec3f48d1dfda9278bdffc5`。
  - `Sources/RemoteServer.swift:3369–3447`；slice SHA-256 `1bc144ead388a794abd1e6bd79486b57de72aa07b181c88874b07f95a7855048`。
  - `Sources/Coordinator.swift:1474–1483`；slice SHA-256 `bdc06b1fe15fe64ebb3be0fdb456f6da1317ae33f0d84d033762237566ea8018`。
- **來源數值／屬性**：`{"terminalChannelDepth":2,"terminalDepth":8}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：count 是 queued+active，不是 byte cap；8/2 為現行常數，沒有量到負載適足性。 同 key terminalPending waiters 另外 append，沒有此路徑的 waiter count/byte cap；重試可增加連線債務。
- **接手 owner**：W2-1 application owner / R-1 / R-2。

## read-queues

slow reads/analytics、transcript、voice/planner 有獨立 admission；overflow 分別為 429 busy/usage_analytics_busy/transcript_busy，voice/planner 亦回 429 busy。列出的 depth 都是 request 數量；沒有把單一 body cap 當 aggregate queued-byte cap。

- **source-derived**：
  - `Sources/RemoteServer.swift:4399–4436`；slice SHA-256 `3c055438cd43c41fe9d256ec8a771c0b62e71bfc233b48bbf775bb9df3364b91`。
  - `Sources/RemoteServer.swift:4252–4261`；slice SHA-256 `b46c966611141380b7924027284015f38af80ca5fd2231c4cb8b9095a6332540`。
  - `Sources/RemoteServer.swift:3738–3738`；slice SHA-256 `303a1dbde79ba7cd428db85803635dba2e8ce2e855d8ac88384d27b1d483fbfe`。
  - `Sources/RemoteServer.swift:4001–4001`；slice SHA-256 `553b36a769437f0e44673f2311acfd0d6e462d4b97781827b7a1b7193f0961db`。
  - `Sources/TranscriptReadCoordinator.swift:12–34`；slice SHA-256 `061f580f9b5fea41396571a90799dde22c3c5001c446285d1eb683e20d2e2df4`。
- **來源數值／屬性**：`{"backgroundDepth":1,"depth":2,"planDepth":2,"readingDepth":8,"usageAnalyticsDepth":2,"voiceDepth":2}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未測 queue wait/work、current debt 或 bytes；count ceiling 不是延遲保證。
- **接手 owner**：R-2 HTTP reliability owner。

## coalesced-read-waiters

FreshReadings 對同 key 共用一次 refresh，但會 append 每個 waiter；因此 worker count cap 不能證明 parked replies 有界。

- **source-derived**：
  - `Sources/ReadingFreshness.swift:180–180`；slice SHA-256 `85a198925bf6c326fbbfb72b18f2c5c690e780b8797e11c3e84af4c3947d04a1`。
  - `Sources/ReadingFreshness.swift:200–220`；slice SHA-256 `49b625454d301621dd39989d7d0cf80c36a60e168f75ef1908fe7fabdcd5f7fa`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：waiter 數量、closure 持有 bytes、disconnect 後回收及重複 retry 負載未測。
- **接手 owner**：R-2 HTTP reliability owner。

## cloud-read-queues

Cloud read 的 foreground/background 分別按 queued+active 計 4/16；超額回 429 cloud_read_busy，拒絕回覆另以 Task publish。

- **source-derived**：
  - `Sources/CloudAppBridge.swift:1402–1447`；slice SHA-256 `4c151020946843293f0e2c64b9e811244279f326712e033fbf57fb762200ee81`。
- **來源數值／屬性**：`{"background_requests":16,"foreground_requests":4}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：沒有此路徑的 aggregate byte ceiling；拒絕回覆 Tasks 的數量／輸出債務未測。
- **接手 owner**：R-1 transport reliability owner。

## cloud-publication

CloudSnapshotPublicationQueue 合併 snapshot，保留 authoritative barrier；最多 3 個 pending（active work 另計）。CloudTransport pendingByChannel 按 channel 保留最新 envelope。

- **source-derived**：
  - `Sources/CloudAppBridge.swift:262–295`；slice SHA-256 `e560d6b2abecfc6c939fa8fedb6ae9bb31bad898e7ba55b8e75a2b1112ad3ca7`。
  - `Sources/CloudTransport.swift:502–502`；slice SHA-256 `a1c315f851b4a1e0cccced7450c0e4e9c70bd153128c2cf51138b006e2855774`。
  - `Sources/CloudTransport.swift:564–585`；slice SHA-256 `c3859b76765045cedbfce60d9d3a4caf2b0aca35ccf1edaf52da093b0fd78084`。
- **來源數值／屬性**：`{"maximumPending":3}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未量到 pending bytes；未證明 channel cardinality 有界；不能套用為 local SSE 或 command ingress 保證。
- **接手 owner**：R-1 / W5-1 durability owner。

## spool-component

CloudOutboundSpool 元件預設有 row/charged-byte caps，admission 比較 candidate commit 後數量並回 typed rowCap/byteCap scope。

- **source-derived**：
  - `Sources/CloudOutboundSpool.swift:139–171`；slice SHA-256 `b9b1f5047d38519160bc169bbc1ddc97550de096d381174a6ecd9832575684d3`。
  - `Sources/CloudOutboundSpool.swift:534–543`；slice SHA-256 `64e1da4a238cb78b40644726aa53fac96d8d0cf13d7e5701008fafc05461c32d`。
  - `Tests/CloudOutboundSpoolTests.swift:720–720`；slice SHA-256 `854d4326f390dc756344667570d5b49be0f08cb70931b0f66f6493010ec4d133`。
- **來源數值／屬性**：`{"fairnessRecipientByteCap":262144,"fairnessRecipientRowCap":20,"globalByteCap":16777216,"globalRowCap":2000,"recipientByteCap":2097152,"recipientRowCap":200}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：此元件數值不證明 production transport 接線或實際 occupancy；本工具沒有驗證接線。 charged bytes 不是 sealed envelope bytes 或 process RSS；runtime override 與負載 adequacy 未測。
- **接手 owner**：W5-1 cross-runtime durability owner。

## ledger-component

CloudCommandLedger 元件有 global/actor/fairness row caps，refuseCapacity 計數並丟出 idempotencyCapacity(scope:reason:)。

- **source-derived**：
  - `Sources/CloudCommandLedger.swift:263–263`；slice SHA-256 `e2d6d1920ba39bc01d597b05a2f09b5f381440b6c93540b4c48efe8f8674069d`。
  - `Sources/CloudCommandLedger.swift:363–380`；slice SHA-256 `ec2056b4011407531daa4ff7770f50560f21dece5b8d4ec7e6c3440b56805ff6`。
  - `Sources/CloudCommandLedger.swift:631–639`；slice SHA-256 `7aaa548e352679960fd255c1179207383af33cd9c8cd0ecf849c5c979fb15c04`。
- **來源數值／屬性**：`{"fairnessActorLimit":100,"fairnessReserveStart":9000,"globalHardLimit":10000,"normalActorLimit":1000}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：元件未在此工具證明 production 接線；未測 bytes、waiters 與 runtime override。 稽核指 production idempotency 仍使用 sender/sequence；R-1 應先定 stable retry identity。
- **接手 owner**：W5-1 cross-runtime durability owner。

## store-read-health

load 先設 loaded=true，unreadable/corrupt/non-object JSON 直接 return；既有記憶體不在此分支清除，但 fresh process 的空狀態沒有 read-health fence。load 未檢查頂層 version；tasks 缺漏／型別不符變 []，無法 decode 的 row 被 skip 後發布 found。

- **source-derived**：
  - `Sources/Orchestrator.swift:10158–10252`；slice SHA-256 `8cc4744ded3e0769690b309df2e40f06a9bd985ce218fb30998733c75b523204`。
  - `Sources/OrchestratorStore.swift:788–788`；slice SHA-256 `74c88e090505f191185f5eee7940757b1adc9e66a50bc0d40a64314a54d0f8cb`。
- **來源數值／屬性**：`{"read_health_fence_in_load":false,"top_level_version_gate":false}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：這是來源路徑推導，沒有對 live registry 執行 corrupt/unreadable/unsupported-version 注入。 未證明實際發生資料損失；corrupt whole store 與可 parse 但 invalid restart 子記錄是不同案例。
- **接手 owner**：W1-5 persistence owner (store-health)。

## store-overwrite-risk

save 以 storeSaveLock 序列化 snapshot/write，輸出 version 1 原子替換；沒有以先前 load health 阻止 save。fresh empty／部分 decode 後再 save 可能覆蓋原證據。read failure 後未改變記憶體，不能概括為每次 failure 都清空。

- **source-derived**：
  - `Sources/Orchestrator.swift:10256–10331`；slice SHA-256 `fa557e01e5e5837c6170cd65204491beecf0c0701cf1bc41d3ed0e78d1a509a8`。
  - `Sources/Orchestrator.swift:10158–10252`；slice SHA-256 `8cc4744ded3e0769690b309df2e40f06a9bd985ce218fb30998733c75b523204`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：authoritative-empty overwrite 為可達路徑風險；沒有實際 overwrite/restart 或 power-loss 測試。 後續要保留原檔、typed read-health、unsupported version policy、legacy compatibility，分離 best-effort/persist-before-effect。
- **接手 owner**：W1-5 persistence owner (store-health)。

## disk-failure-seams

save 的 serializer/write/chmod 失敗回 false；write 後 chmod 失敗可能已換檔，因此 false 不等於磁碟完全沒變。storeSaveInterceptorForTesting 可攔截寫入。completion/restart 測試已有指定 rollback seam，但本次不執行 Swift。

- **source-derived**：
  - `Sources/Orchestrator.swift:10256–10331`；slice SHA-256 `fa557e01e5e5837c6170cd65204491beecf0c0701cf1bc41d3ed0e78d1a509a8`。
  - `Tests/OrchestratorCompletionTests.swift:691–691`；slice SHA-256 `722a8705a2b47caccc42f514cb4c59808be09c627a2090544fdb78054a4b7d4b`。
  - `Tests/OrchestratorRecoveryTests.swift:1381–1381`；slice SHA-256 `afdb101b2116a038b7267f771c1bfd3f9189395c2b0a4184d0ae16244645a938`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：EACCES/ENOSPC/EROFS、partial write、rename、chmod、fsync/power-loss 與雙 writer 未實測。 .atomic 不是 power-loss durability receipt；每個 effect 的 persist ordering 仍須各自確認。
- **接手 owner**：W1-5 persistence owner / W4-2 packaging owner。

## sequence-disk

CloudSequenceFile 有 unreadable/unwritable typed failure，load 檢查 v=1；nextSequence 的正常 reserve 分支嘗試先 persist ceiling 再發序號。來源控制流程顯示 reserved[sender] 先於 try persist() 更新，persist 失敗不還原 reserved 且 next 未前進；同一物件重試可略過 persist 並發出序號。若失敗未替換磁碟舊 ceiling，重啟後可能重用已發序號；production composition 持續提供同一 sequenceFile。

- **source-derived**：
  - `Sources/CloudBridgeLifecycle.swift:43–59`；slice SHA-256 `47836308de5c86ebe9b72e1e8358fa316e4489ac6451f62dcd91c7030110811a`。
  - `Sources/CloudBridgeLifecycle.swift:65–92`；slice SHA-256 `75bbed4f31decb5c51ea2250974f1fdfe933590aa72b770fe61c18249f803b74`。
  - `Sources/CloudBridgeLifecycle.swift:92–113`；slice SHA-256 `7be466daf8b4ad380045149f9cd38c1db521f329872b66bad598b27f717e6214`。
  - `Sources/CloudBridgeLifecycle.swift:533–533`；slice SHA-256 `950be1dccde1ede0456c171e515e6532af7aa6fac4e47e36c3468f6ac5d56049`。
  - `Sources/CloudAppBridge.swift:674–674`；slice SHA-256 `fb03a42f0f7170421077ef8cf7c0fc272abd355bb764e6df403f08af4c42cc1a`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：上述是來源推導風險；實際序號重用或資料損失未觀測，未執行 Swift failure injection。 寫入失敗後重試、斷電、備份還原與同 identity 雙 writer 的實測及 production 修復由 W5-1 接手。 不得把 registry、sequence、ledger 合成同一錯誤政策。
- **接手 owner**：W5-1 durability owner / security reviewer。

## restart-contract

restart receipt 以 instance 與 total/per-channel drain 決定 ready；spawning 阻擋，queued 依 secret 是否可恢復判斷，briefed 本身不阻擋。reconcile 要新且完整 inventory；超過 grace 可帶 unresolved IDs 完成，不能讀成全部已觀察。

- **source-derived**：
  - `Sources/SessionWatch.swift:1164–1187`；slice SHA-256 `64b42da82c87bb07f6939ce908f5be1f518826f31eb357f075f133785b038b49`。
  - `Sources/SessionWatch.swift:1189–1207`；slice SHA-256 `6cde2caba502ddeb6dfc97a392ea32be6e7991b163c9a7b190cbed08fa021e0d`。
  - `Sources/Coordinator.swift:1672–1722`；slice SHA-256 `65eff53327059be92f382eeb4ed6beef6634bd261bdcce9834f288d512e78a48`。
  - `Tests/OrchestratorRecoveryTests.swift:1199–1199`；slice SHA-256 `f827616029d4f063b25655e9997ee192fb5c2732b7d3d1f542eebc4b137496a3`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：existing test source 不是已執行 receipt；未重啟此 Mac，daemon restart time/p95 與 VM reboot 都未知。 R-1/R-2 與 W4 disposable Ubuntu 須測 admission/effect/write/ACK 各故障點；terminal service 和 daemon 分別測。
- **接手 owner**：W4 Linux runtime owner / W1-5 persistence owner。

## restart-liveness

可選 probe 僅 GET loopback /v1/health，記錄同一 instance/build/protocol與單次 request 耗時；它不重啟、不讀 store、不證明 recovery 或 Cloud acceptance。

- **source-derived**：
  - `Sources/RemoteServer.swift:5469–5479`；slice SHA-256 `8e7fc3414149906a3b5cb277b25f5ec9ce9857903d27b670c9b1ef73c0387b1d`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未量測 restart/reconcile time、terminal preservation 或 storage readiness；health 不回 schema/read-health。 installed build 與來源 checkout 的 commit 關係未驗證。
- **接手 owner**：W4 Linux runtime owner / root finalizer。

## filesystem-fixture

可選 probe 在指定 scratch 的私人目錄測 byte round-trip、corrupt JSON、missing read、ENOTDIR write 與 replace failure；這是 Python/host filesystem fixture，不是 Swift Orchestrator.load/save。

- **source-derived**：
  - `Sources/Orchestrator.swift:10311–10311`；slice SHA-256 `6df2ea3a6d16d84234b2ec21b002e4067370f5932d516041633e841882c83db4`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：真實 store 的 durability、disk-full、permission failure 與原檔 quarantine 行為仍未知。
- **接手 owner**：W1-5 persistence owner。

## 預算批准所需的下一批輸入

| 後續節點 | 必須取得的觀測與決策 |
|---|---|
| R-1 | ingress/admission/accepted/executing/publication/ACK 各段 count、bytes、wait/work；burst + suspended consumer；穩定 retry identity、durable re-read、typed overflow 與無 silent command loss。 |
| R-2 | connection/aggregate body/stream/outstanding-byte ceiling；慢 consumer 與 duplicate retry 下，同時追蹤 health、其他 lane、completion/error、eviction、snapshot coalescing 及 reconnect realign。 |
| W1-5 store-health | fresh/existing state × missing/EACCES/corrupt JSON/wrong row shape/unsupported version；load→save 原檔保留；ENOSPC/EROFS/rename/chmod/partial-write；各 effect 的 persist-before-effect/send 與 typed read-health。 |
| W4/W6 | disposable Ubuntu 的 daemon/terminal service restart 分離、VM reboot、非空 restore、同 process/boot identity、queue drain 與 reconciliation 耗時分布。 |

Plan v4 的 restart 60s p95、reboot 120s、RPO/RTO 是提案目標；本基準沒有實測或批准它們。
沒有新增全管線 count/byte budget：需 reference workload、樣本數、duration、環境、拒絕／丟棄與 peak debt，再由具名 owner/approver 決定。
health liveness、fixture 通過、source seal 相符，都不構成 restart、durability、Ubuntu 或 Cloud 發布驗收。

## 來源檔案 seal

| 檔案 | bytes | SHA-256 |
|---|---:|---|
| `Sources/CloudAppBridge.swift` | 87080 | `853c4431771550175c6c485b9da648edb72ef1d24b350030402dee74785aaabc` |
| `Sources/CloudBridgeLifecycle.swift` | 25177 | `07f585fd60b4e99abe42d89ca72087c6339456cb844b088718c503cd9ef6b663` |
| `Sources/CloudCommandLedger.swift` | 28746 | `def726c029d4fb17e0d096c3187d87b2ea89fdc413e69b95bb02cf0a27862fcf` |
| `Sources/CloudOutboundSpool.swift` | 42597 | `aef545decd4c7c95b5c566cb60cb11455eaadf6b52f84c3707b76df165459485` |
| `Sources/CloudTransport.swift` | 41195 | `1996000707852cac1a71b2a9622cca7aea996e79d000d7d1ddc9f4175425465d` |
| `Sources/Coordinator.swift` | 93345 | `bf400471dce7fe381aea8608d5d96b8710e33035c0d98fbaea30b493040345ad` |
| `Sources/Orchestrator.swift` | 583213 | `e06cc0b6582ec1e5adb84ae0bd123895a659f570c4bd45737cd6364e3b9369ac` |
| `Sources/OrchestratorStore.swift` | 56740 | `ed31e8ceb7aab18aee23efdf8c3a20805e61a576f89673a4b79deeffd58bcfcc` |
| `Sources/ReadingFreshness.swift` | 25220 | `4592b2a84c03d196707626df2876f3ef4e5274149addf361b7738a2888ed672f` |
| `Sources/RemoteServer.swift` | 335005 | `e143790631dde9e4db0bcaa55f1022a2e363b21781e364e730504cdec27f0c10` |
| `Sources/SessionWatch.swift` | 68189 | `d1d47930535b2e79bea14d13d56482885adeff09fcaa0326b84f8a60701beb5c` |
| `Sources/TranscriptReadCoordinator.swift` | 5111 | `61c1adf7558d54bff495128b78450b849eb48dd69bcddd724148b72f855cd371` |
| `Tests/CloudOutboundSpoolTests.swift` | 66799 | `da5322716a38ed3732fd113afdf507587c11f033e0a30d32f91be227b34fa244` |
| `Tests/OrchestratorCompletionTests.swift` | 63864 | `d2258c60bd16287345470da810a3ec720c14886fe49da132ff6d92da6054699b` |
| `Tests/OrchestratorRecoveryTests.swift` | 91478 | `e57da0019aa0535c82eb7ce41d22188662b594062fd7deebaabc6bdb4ec420f7` |
