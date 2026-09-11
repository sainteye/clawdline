# 平台可靠性基準（W0-C）

此文件由 `tools/measure-platform-reliability.py` 產生；CLA-296 Plan v4 的量測輸入。
`source_reference_commit`：`base:7a785fc15c34cdbfd2de8f6960a038f78c90132c+candidate-overlay:c588c071eec39a0796a9135c7ab814914d11be63f35a7153b60f370557bc2be5`；這表示指定 base 加上由 source manifest 封存的 candidate overlay，base commit 本身不含 overlay bytes。
來源範圍 SHA-256：`c588c071eec39a0796a9135c7ab814914d11be63f35a7153b60f370557bc2be5`。
工具 SHA-256：`35ee1089ae4652c4f2c6606a182927bacbe196262c244cad8758be263bf1944d`。

17 個非空來源檔、19 列。這是範圍摘要，並非整棵 commit-tree 驗證。
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

CloudInboundCommandQueue 是 pending command 的單一 owner；count、plaintext／charged bytes、finish、peak、invalid drop 與 typed refusal 都有明確讀值。replay cursor 只在 admission 後前進；容量拒絕終結該 sequence，不 fence 後繼 sequence。加密拒絕由有界且有 deadline 的單一 publication lane 發送。

- **source-derived**：
  - `Sources/CloudTransport.swift:219–246`；slice SHA-256 `f66e1167b44c8441d263827b8d939dad0b470b414a23278a677a0401ea20954d`。
  - `Sources/CloudTransport.swift:1170–1204`；slice SHA-256 `6ba6d231d4c9e42c257c3cdad892b06d0ecde882b1cb1c507ff04a461856fc8c`。
  - `Sources/CloudAppBridge.swift:714–722`；slice SHA-256 `21ba233e27062ca9cd21dd77dc3ae69407268b8ffddc0f76491e320f079d6980`。
  - `Sources/CloudAppBridge.swift:344–592`；slice SHA-256 `f7b72877e71c8225c5dec9a48386a44cac84144bb64041bf8096f0d71af059d3`。
  - `Sources/CloudAppBridge.swift:2022–2119`；slice SHA-256 `631e77630b11ef90a9cebc73233a2e4d4fc7e4b0941c07f8d1333faccd16feb3`。
  - `Sources/CloudAppBridge.swift:1391–1395`；slice SHA-256 `da5abf06113703618a42225a65ddbecc1392c2590013e9083554575fdefa0060`。
- **來源數值／屬性**：`{"capacity_refusal":"terminal_for_authenticated_sequence","defaultDeadlineMilliseconds":1000,"defaultMaximumChargedBytes":33554432,"defaultMaximumCount":8,"defaultMaximumOutstanding":8,"defaultMaximumPlaintextBytes":16777216,"idempotency_identity":"cloud:<sender>:<sequence>","typed_overload_code":"cloud_ingress_busy"}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：8／32 MiB 是 Plan-v4 source budget，尚未以 reference workload 證明適足；charged bytes 不是 RSS 或 sealed frame bytes。 同一 transport reconnect 可重讀 FIFO；process restart durability、ledger/spool production wiring 與 end-to-end human ACK 仍屬 W5/W6。
- **接手 owner**：W5-1 durability owner / W6 numeric-budget review。

## cloud-ready

readyGenerations 明定 bufferingNewest(1)，dropped 計數；與 commands 是不同流。

- **source-derived**：
  - `Sources/CloudTransport.swift:824–824`；slice SHA-256 `bbe2398002f751bf1f82dd2e82a09166dac984ffc7ca189f48c0d315cb166507`。
  - `Sources/CloudTransport.swift:980–988`；slice SHA-256 `6211c6dfa714182470fde62aec022d0ae20e9231cd1c202290828e8a2c0a4edd`。
- **來源數值／屬性**：`{"pending_generations":1}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：沒有執行 ready-generation 消費／丟棄測試；不能把此策略當成 command 可丟棄政策。
- **接手 owner**：R-1 transport reliability owner。

## http-admission

accept 直接 start/receive；這條 accept/receive 路徑沒有全域連線、aggregate body bytes 或 request-read deadline admission。response close grace 是另一階段。

- **source-derived**：
  - `Sources/RemoteServer.swift:558–563`；slice SHA-256 `809c25ceafa41dcf951f1a64aed7b8de55b5d7d97e2198ebd2e7ce86950a1c42`。
  - `Sources/RemoteServer.swift:568–621`；slice SHA-256 `a36633fb23f30d6079fca2bb082b6d35050af66525a3192f968ce1a6f5cb74b0`。
  - `Sources/RemoteServer.swift:5569–5569`；slice SHA-256 `f51d92d14db18adf58729af5350df5b81ae3ccfee46a076b7cbd7796bd31e283`。
- **來源數值／屬性**：`{"bodyLimit":20971520,"responseCloseGraceSeconds":30}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：NWListener/kernel 上限、同時連線數、記憶體峰值與 slowloris 行為未量測。 64 KiB 是未找到 header terminator 時的條件檢查，不能稱所有完整 header 的嚴格上限。
- **接手 owner**：R-2 HTTP reliability owner。

## sse-output

Stream 只保存 connection；openStream 登錄串流，授權檢查沒有容量條件；write 與 heartbeat 的 contentProcessed 忽略 error，沒有此層 outstanding-byte 計數、slow-consumer eviction 或待送 snapshot 合併。

- **source-derived**：
  - `Sources/RemoteServer.swift:5430–5435`；slice SHA-256 `aa83071c47cfb0303bcd1ac0652e35be5e485d5fc876cd63b03ccbe31da608ba`。
  - `Sources/RemoteServer.swift:5445–5492`；slice SHA-256 `0c9d26aef7affa2aa56381c1b4e02f643d6e0e810772abeb5f143fb69953292f`。
  - `Sources/RemoteServer.swift:734–742`；slice SHA-256 `7f6beca1780fb6921227aff809fa9a9aa1a78be02872dc41f3a825c1fde71452`。
  - `Sources/RemoteServer.swift:5495–5511`；slice SHA-256 `1ea4c0fb7c2d0ba698647d66baef5d0f91e8a66da781e98b17dd6a4a26d9482c`。
  - `Sources/RemoteServer.swift:5558–5567`；slice SHA-256 `3fef466150f294172022c48b80a119ce8d4c048ba552e0dd620fdce6b779ea95`。
- **來源數值／屬性**：`{"connection_cap":null,"outstanding_byte_cap":null}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未開 authenticated SSE 或使真實 consumer 停讀；連線／待送 bytes、kernel buffer 與 callback 延遲未知。 Network 可能有下層背壓，來源不能證明本層已受保護；publication 合併不是 local SSE 合併。
- **接手 owner**：R-2 HTTP reliability owner。

## sse-reconnect

openStream 送 hello 與當前 sessions/orchestrator 全量 snapshot；write 分配 event id。重連以當前狀態 realign，這些路徑沒有 Last-Event-ID replay。

- **source-derived**：
  - `Sources/RemoteServer.swift:5445–5492`；slice SHA-256 `0c9d26aef7affa2aa56381c1b4e02f643d6e0e810772abeb5f143fb69953292f`。
  - `Sources/RemoteServer.swift:5558–5567`；slice SHA-256 `3fef466150f294172022c48b80a119ce8d4c048ba552e0dd620fdce6b779ea95`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未測斷線重連、遺失事件與 viewer 觀察；snapshot 送出不等於 command effect 完成／ACK。
- **接手 owner**：R-2 HTTP reliability owner。

## terminal-queue

同一 serial terminal worker 在入列前計 total/per-channel outstanding；HTTP 容量拒絕為 429 busy，maintenance 為 503 restart_maintenance；nested inline 仍計新增 channel。

- **source-derived**：
  - `Sources/TerminalCommandScheduler.swift:59–116`；slice SHA-256 `1be76532d1e427d0f28d471102818ef7c6eb780c3065ad75110f3c8af248eeb0`。
  - `Sources/RemoteServer.swift:218–222`；slice SHA-256 `cba9be1ea79d0e721081d4be089af146435c807cbbace83a9be21d4918bd07c0`。
  - `Sources/RemoteServer.swift:3335–3413`；slice SHA-256 `1bc144ead388a794abd1e6bd79486b57de72aa07b181c88874b07f95a7855048`。
  - `Sources/Coordinator.swift:1471–1479`；slice SHA-256 `ec85bdb9cfc055b8bfec1e596f3a3ff56ca60ba955f1e4d41710927ef314709b`。
- **來源數值／屬性**：`{"terminalChannelDepth":2,"terminalDepth":8}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：count 是 queued+active，不是 byte cap；8/2 為現行常數，沒有量到負載適足性。 同 key terminalPending waiters 另外 append，沒有此路徑的 waiter count/byte cap；重試可增加連線債務。
- **接手 owner**：W2-1 application owner / R-1 / R-2。

## read-queues

slow reads/analytics、transcript、voice/planner 有獨立 admission；overflow 分別為 429 busy/usage_analytics_busy/transcript_busy，voice/planner 亦回 429 busy。列出的 depth 都是 request 數量；沒有把單一 body cap 當 aggregate queued-byte cap。

- **source-derived**：
  - `Sources/RemoteServer.swift:4365–4402`；slice SHA-256 `3c055438cd43c41fe9d256ec8a771c0b62e71bfc233b48bbf775bb9df3364b91`。
  - `Sources/RemoteServer.swift:4218–4227`；slice SHA-256 `b46c966611141380b7924027284015f38af80ca5fd2231c4cb8b9095a6332540`。
  - `Sources/RemoteServer.swift:3704–3704`；slice SHA-256 `303a1dbde79ba7cd428db85803635dba2e8ce2e855d8ac88384d27b1d483fbfe`。
  - `Sources/RemoteServer.swift:3967–3967`；slice SHA-256 `553b36a769437f0e44673f2311acfd0d6e462d4b97781827b7a1b7193f0961db`。
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

Cloud read 的 foreground/background 分別按 queued+active 計 4/16；超額回 429 cloud_read_busy，拒絕回覆交給共用的有界 publication lane。

- **source-derived**：
  - `Sources/CloudAppBridge.swift:1784–1854`；slice SHA-256 `b8e0d937f7496b43ffd5ebe1aa86b68c1a4f3ffe9431c3ec7e74eb24a471a16b`。
- **來源數值／屬性**：`{"background_requests":16,"foreground_requests":4}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：沒有此路徑的 aggregate byte ceiling；read worker 的輸入 bytes 與 runtime adequacy 未測。
- **接手 owner**：R-1 transport reliability owner。

## cloud-publication

CloudSnapshotPublicationQueue 合併 snapshot，保留 authoritative barrier；最多 3 個 pending（active work 另計）。CloudTransport pendingByChannel 按 channel 保留最新 envelope。

- **source-derived**：
  - `Sources/CloudAppBridge.swift:271–344`；slice SHA-256 `1518b0c7bc64a05e1e1215d811211bdbcb084cf57ee4555689b4bd35eca267b5`。
  - `Sources/CloudTransport.swift:788–788`；slice SHA-256 `a1c315f851b4a1e0cccced7450c0e4e9c70bd153128c2cf51138b006e2855774`。
  - `Sources/CloudTransport.swift:851–879`；slice SHA-256 `4014231e5c4ee060d62a78dabe1d95975ad88992c8e6db5aee55dea0c4b68b62`。
- **來源數值／屬性**：`{"maximumPending":3}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未量到 pending bytes；未證明 channel cardinality 有界；不能套用為 local SSE 或 command ingress 保證。
- **接手 owner**：R-1 / W5-1 durability owner。

## spool-component

CloudOutboundSpool 元件預設有 row/charged-byte caps，admission 比較 candidate commit 後數量並回 typed rowCap/byteCap scope。

- **source-derived**：
  - `Sources/CloudOutboundSpool.swift:142–174`；slice SHA-256 `b9b1f5047d38519160bc169bbc1ddc97550de096d381174a6ecd9832575684d3`。
  - `Sources/CloudOutboundSpool.swift:537–546`；slice SHA-256 `64e1da4a238cb78b40644726aa53fac96d8d0cf13d7e5701008fafc05461c32d`。
  - `Tests/CloudOutboundSpoolTests.swift:720–720`；slice SHA-256 `854d4326f390dc756344667570d5b49be0f08cb70931b0f66f6493010ec4d133`。
- **來源數值／屬性**：`{"fairnessRecipientByteCap":262144,"fairnessRecipientRowCap":20,"globalByteCap":16777216,"globalRowCap":2000,"recipientByteCap":2097152,"recipientRowCap":200}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：此元件數值不證明 production transport 接線或實際 occupancy；本工具沒有驗證接線。 charged bytes 不是 sealed envelope bytes 或 process RSS；runtime override 與負載 adequacy 未測。
- **接手 owner**：W5-1 cross-runtime durability owner。

## ledger-component

CloudCommandLedger 元件有 global/actor/fairness row caps，refuseCapacity 計數並丟出 idempotencyCapacity(scope:reason:)。

- **source-derived**：
  - `Sources/CloudCommandLedger.swift:266–266`；slice SHA-256 `e2d6d1920ba39bc01d597b05a2f09b5f381440b6c93540b4c48efe8f8674069d`。
  - `Sources/CloudCommandLedger.swift:366–383`；slice SHA-256 `ec2056b4011407531daa4ff7770f50560f21dece5b8d4ec7e6c3440b56805ff6`。
  - `Sources/CloudCommandLedger.swift:634–642`；slice SHA-256 `7aaa548e352679960fd255c1179207383af33cd9c8cd0ecf849c5c979fb15c04`。
- **來源數值／屬性**：`{"fairnessActorLimit":100,"fairnessReserveStart":9000,"globalHardLimit":10000,"normalActorLimit":1000}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：元件未在此工具證明 production 接線；未測 bytes、waiters 與 runtime override。 production idempotency 仍使用 sender/sequence；R-1 的 capacity refusal 終結該 sequence，process-restart durable identity 與 ledger 接線仍由 W5-1 定義。
- **接手 owner**：W5-1 cross-runtime durability owner。

## store-read-health

readStore 將 absent、ready、corrupt、unsupported-version 與 unreadable 分開；load 只發布完整 schema-v1，要求 tasks 並拒絕錯型、無法 decode 或重複 identity 的 row。non-authoritative health 保留既有記憶體但阻止 projection 與 route 使用。

- **source-derived**：
  - `Sources/OrchestratorPersistence.swift:70–102`；slice SHA-256 `0e67153e791fe0a72cb8676730d66548eb6d643fadc6fb81260a0f94bdc92a92`。
  - `Sources/Orchestrator.swift:10034–10219`；slice SHA-256 `7d4da82405435739f2703d955737b3e20ab3e9397df8ed92c2bfc6f71163bcc7`。
  - `Sources/OrchestratorStore.swift:791–791`；slice SHA-256 `74c88e090505f191185f5eee7940757b1adc9e66a50bc0d40a64314a54d0f8cb`。
- **來源數值／屬性**：`{"read_health_fence_in_load":true,"top_level_version_gate":true}`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：focused Swift fixture 覆蓋 absent/corrupt/partial/wrong-row/future/directory，但不是 live registry 或 power-loss 觀測。 corrupt whole store 與可 parse 但 invalid restart 子記錄是不同政策。
- **接手 owner**：W1-5 persistence owner (accepted source boundary)。

## store-overwrite-risk

save 以 storeSaveLock 序列化 snapshot/write，輸出 version 1 原子替換，並拒絕 non-authoritative read health。Readable rejected bytes 保留在 canonical path 且另作content-addressed quarantine；不存在才是 authoritative empty。

- **source-derived**：
  - `Sources/Orchestrator.swift:10223–10296`；slice SHA-256 `4b0fc9f543a83d11a6b13d4d508fbcc1541cb90a6d8cc94799fc7a6384434a98`。
  - `Sources/OrchestratorPersistence.swift:102–109`；slice SHA-256 `05268545ae0daa005e78519332887d7ce7929239fe9282bffcdd4524898948c8`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：沒有實際 ENOSPC/EROFS、rename/chmod failure、雙 writer 或 power-loss 測試。 Data.atomic 與 quarantine copy 不構成 fsync/power-loss durability receipt。
- **接手 owner**：W4-2 packaging / operational recovery owner。

## disk-failure-seams

save 的 serializer/write/chmod 失敗回 false；write 後 chmod 失敗可能已換檔，因此 false 不等於磁碟完全沒變。Root Assignment、startup recovery 與cleanup 現在 gate effect；storeSaveInterceptorForTesting 可攔截寫入。

- **source-derived**：
  - `Sources/Orchestrator.swift:10223–10296`；slice SHA-256 `4b0fc9f543a83d11a6b13d4d508fbcc1541cb90a6d8cc94799fc7a6384434a98`。
  - `Sources/OrchestratorPersistence.swift:286–320`；slice SHA-256 `850444b1a62bfd5bce49d47a0020daef8bf7171b8c88edd884a708029bb5e96e`。
  - `Sources/OrchestratorRegistry.swift:722–835`；slice SHA-256 `2836dc415e0e6db01e22d767304d031d89cc28736ab80e7ceff6490ab94992e0`。
  - `Tests/OrchestratorRecoveryTests.swift:1720–1720`；slice SHA-256 `afdb101b2116a038b7267f771c1bfd3f9189395c2b0a4184d0ae16244645a938`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：EACCES/ENOSPC/EROFS、partial write、rename、chmod、fsync/power-loss 與雙 writer 未實測。 direct dispatch 與一般 terminal briefing lane 的更廣 ordering 仍由 W2-1 接手。
- **接手 owner**：W2-1 terminal lane / W4-2 packaging owner。

## sequence-disk

CloudSequenceFile 有 unreadable/unwritable typed failure，load 檢查 v=1；nextSequence 的正常 reserve 分支嘗試先 persist ceiling 再發序號。來源控制流程顯示 reserved[sender] 先於 try persist() 更新，persist 失敗不還原 reserved 且 next 未前進；同一物件重試可略過 persist 並發出序號。若失敗未替換磁碟舊 ceiling，重啟後可能重用已發序號；production composition 持續提供同一 sequenceFile。

- **source-derived**：
  - `Sources/CloudBridgeLifecycle.swift:46–62`；slice SHA-256 `47836308de5c86ebe9b72e1e8358fa316e4489ac6451f62dcd91c7030110811a`。
  - `Sources/CloudBridgeLifecycle.swift:68–95`；slice SHA-256 `75bbed4f31decb5c51ea2250974f1fdfe933590aa72b770fe61c18249f803b74`。
  - `Sources/CloudBridgeLifecycle.swift:95–116`；slice SHA-256 `7be466daf8b4ad380045149f9cd38c1db521f329872b66bad598b27f717e6214`。
  - `Sources/CloudBridgeLifecycle.swift:536–536`；slice SHA-256 `950be1dccde1ede0456c171e515e6532af7aa6fac4e47e36c3468f6ac5d56049`。
  - `Sources/CloudAppBridge.swift:967–967`；slice SHA-256 `fb03a42f0f7170421077ef8cf7c0fc272abd355bb764e6df403f08af4c42cc1a`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：上述是來源推導風險；實際序號重用或資料損失未觀測，未執行 Swift failure injection。 寫入失敗後重試、斷電、備份還原與同 identity 雙 writer 的實測及 production 修復由 W5-1 接手。 不得把 registry、sequence、ledger 合成同一錯誤政策。
- **接手 owner**：W5-1 durability owner / security reviewer。

## restart-contract

restart receipt 以 instance 與 total/per-channel drain 決定 ready；spawning 阻擋，queued 依 secret 是否可恢復判斷，briefed 本身不阻擋。reconcile 要新且完整 inventory；超過 grace 可帶 unresolved IDs 完成，不能讀成全部已觀察。

- **source-derived**：
  - `Sources/SessionWatch.swift:1167–1190`；slice SHA-256 `64b42da82c87bb07f6939ce908f5be1f518826f31eb357f075f133785b038b49`。
  - `Sources/SessionWatch.swift:1192–1210`；slice SHA-256 `6cde2caba502ddeb6dfc97a392ea32be6e7991b163c9a7b190cbed08fa021e0d`。
  - `Sources/Coordinator.swift:1684–1747`；slice SHA-256 `0f892d8ca72a686e44ad5f83f1c1612f21b936cd9f97212f33483ce39a40ed28`。
  - `Tests/OrchestratorRecoveryTests.swift:1537–1537`；slice SHA-256 `f827616029d4f063b25655e9997ee192fb5c2732b7d3d1f542eebc4b137496a3`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：existing test source 不是已執行 receipt；未重啟此 Mac，daemon restart time/p95 與 VM reboot 都未知。 R-1/R-2 與 W4 disposable Ubuntu 須測 admission/effect/write/ACK 各故障點；terminal service 和 daemon 分別測。
- **接手 owner**：W4 Linux runtime owner / W1-5 persistence owner。

## restart-liveness

可選 probe 僅 GET loopback /v1/health，記錄同一 instance/build/protocol與單次 request 耗時；它不重啟、不讀 store、不證明 recovery 或 Cloud acceptance。

- **source-derived**：
  - `Sources/RemoteServer.swift:5435–5445`；slice SHA-256 `8e7fc3414149906a3b5cb277b25f5ec9ce9857903d27b670c9b1ef73c0387b1d`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：未量測 restart/reconcile time、terminal preservation 或 storage readiness；health 不回 schema/read-health。 installed build 與來源 checkout 的 commit 關係未驗證。
- **接手 owner**：W4 Linux runtime owner / root finalizer。

## filesystem-fixture

可選 probe 在指定 scratch 的私人目錄測 byte round-trip、corrupt JSON、missing read、ENOTDIR write 與 replace failure；這是 Python/host filesystem fixture，不是 Swift Orchestrator.load/save。

- **source-derived**：
  - `Sources/Orchestrator.swift:10283–10283`；slice SHA-256 `6df2ea3a6d16d84234b2ec21b002e4067370f5932d516041633e841882c83db4`。
- **executable-test-derived**：`{"observation_count":null,"status":"not-run"}`。
- **measured-runtime**：`{"observation_count":null,"status":"not-measured"}`。
- **unknown**：真實 store 的 disk-full、permission failure、atomic rename 與 power-loss durability 仍未知。
- **接手 owner**：W4-2 packaging / operational recovery owner。

## 預算批准所需的下一批輸入

| 後續節點 | 必須取得的觀測與決策 |
|---|---|
| W6（R-1 follow-through） | ingress/admission/accepted/executing/refusal-publication/ACK 各段 count、bytes、wait/work；burst + suspended consumer；terminal-sequence overload、reply-lane deadline/full-drop 與無 silent admitted-command loss。 |
| R-2 | connection/aggregate body/stream/outstanding-byte ceiling；慢 consumer 與 duplicate retry 下，同時追蹤 health、其他 lane、completion/error、eviction、snapshot coalescing 及 reconnect realign。 |
| W1-5 store-health | fresh/existing state × missing/EACCES/corrupt JSON/wrong row shape/unsupported version；load→save 原檔保留；ENOSPC/EROFS/rename/chmod/partial-write；各 effect 的 persist-before-effect/send 與 typed read-health。 |
| W4/W6 | disposable Ubuntu 的 daemon/terminal service restart 分離、VM reboot、非空 restore、同 process/boot identity、queue drain 與 reconciliation 耗時分布。 |

Plan v4 的 restart 60s p95、reboot 120s、RPO/RTO 是提案目標；本基準沒有實測或批准它們。
沒有新增全管線 count/byte budget：需 reference workload、樣本數、duration、環境、拒絕／丟棄與 peak debt，再由具名 owner/approver 決定。
health liveness、fixture 通過、source seal 相符，都不構成 restart、durability、Ubuntu 或 Cloud 發布驗收。

## 來源檔案 seal

| 檔案 | bytes | SHA-256 |
|---|---:|---|
| `Sources/CloudAppBridge.swift` | 110733 | `6c302234a33d414c4b40d845c1008b39b04f961cb9fa8efd2de5632cfb2fc1dc` |
| `Sources/CloudBridgeLifecycle.swift` | 25322 | `5030614169a6389f6189045e9cdf98f52ea8434beb3ce5ab0e9c03eec9f2eef8` |
| `Sources/CloudCommandLedger.swift` | 28891 | `a7a2161c8023710d79b36b1bf8dc452c1a52d6050045d61044df8f55bc3b83d4` |
| `Sources/CloudOutboundSpool.swift` | 42742 | `1653423609868c4903529194ca74344d755b560d453a65951edf06d1d855f2aa` |
| `Sources/CloudTransport.swift` | 53894 | `00ccd9952ec29abfc408c9cfc3904074e9776163070c40a9b0f014efe4350f09` |
| `Sources/Coordinator.swift` | 96509 | `9540466f054e1e686b2087f511901af49f3364b0164525cb369f7f1d8133d0a6` |
| `Sources/Orchestrator.swift` | 588316 | `f3e1ab68b2b75b9769ab2993afbec41430a90ea54c9de7fd6cba88d2af94cf72` |
| `Sources/OrchestratorPersistence.swift` | 19385 | `375628d34df7a2b7679b1e5519d2cfd81f92a3ba86df62b12ae95dfd43d9482c` |
| `Sources/OrchestratorRegistry.swift` | 71277 | `9af6a0fecd32f73afb57ccd8963a15c7ade878336c8e3c044d96d0254f2c4523` |
| `Sources/OrchestratorStore.swift` | 56885 | `a15c0d900f79a09406a7b4594fd77047ed9d62347a3d15f6bdb19eb6c6100af8` |
| `Sources/ReadingFreshness.swift` | 25220 | `4592b2a84c03d196707626df2876f3ef4e5274149addf361b7738a2888ed672f` |
| `Sources/RemoteServer.swift` | 331376 | `4fbed488debf5bf879c220ca08e3f003ed98e1b6f53136ece90e47a5c6fcaa38` |
| `Sources/SessionWatch.swift` | 68334 | `57271981d56e9a24ad2eda992392369563eda504c5f0e455b3554ea162badf21` |
| `Sources/TerminalCommandScheduler.swift` | 7866 | `81ca5981fee4900fca40edea3e050de7b88c66982d745e6f51b6f1c5ecce7e4f` |
| `Sources/TranscriptReadCoordinator.swift` | 5111 | `61c1adf7558d54bff495128b78450b849eb48dd69bcddd724148b72f855cd371` |
| `Tests/CloudOutboundSpoolTests.swift` | 66799 | `da5322716a38ed3732fd113afdf507587c11f033e0a30d32f91be227b34fa244` |
| `Tests/OrchestratorRecoveryTests.swift` | 111776 | `725f8b579b9980ee5a4de7b1f865a5882eed28d20f27b2949d0ab8e548aa047f` |
