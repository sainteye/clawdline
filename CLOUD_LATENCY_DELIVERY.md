# Cloud outbound window 與 durable latency correction 交付封存

交付節點：`cloud-latency-correction`
基準 commit：`eaa20bbce576bcbbb9303563059912c3079fc99e`
基準 tree：`f8ad8d856cdb0f4e500b0cbe02776c35e062c13a`
封存複審：task `e253836b-1a9a-49b1-ad19-a32c93365213` 的
`CLOUD_LATENCY_REVIEW.md`。

## 修正後的行為與不變式

`CloudDurableOutboundComposition.enqueue` 只做 durable reserve、authenticated envelope seal
與 spool seal；成功後喚醒 lifecycle-owned 單一 drain worker。`s`／`orch` 是 latest-value
channel：同一完整 recipient 的舊 ready rows 會與新 reservation 在同一 durable commit 中
burn；`t`／control 保留每筆事件。所有 ready rows 在交給 socket 前，會以 sealed authenticated
envelope 的 `ts` 再判一次 240 秒 freshness（嚴格位於 Relay 預設 300 秒 skew 內）；過期 row
以既有 persisted `staleReadyAtRestart` raw value terminal burn（該歷史名稱現在同時涵蓋 live
pre-send），不靠 restart 或 30 秒 uncertain timeout；沿用 raw value 讓前一版 reader 在 rollback
後仍能解碼同一份 store。

sent window 的預設上限是 8 rows／4 MiB exact persisted-frame bytes。4 MiB 小於 32 MiB
durable JSON 檔案上限，即使計入 base64 與 row metadata 仍可先觸發 typed window refusal；測試以
真實 `CloudFileSpoolStore` 將 raw sent bytes 推到恰好 4 MiB、保持整檔低於 32 MiB，再證明多一
byte 會被 window 拒絕。容量、integrity 與 permissions 是可見的 persistent structural failure，
不進固定 1 Hz full-file re-encode；durable I/O／transport transient failure 才走最高 60 秒的
bounded exponential retry。

authenticated `publish_error {ch,seq,code,field}` 只結算相符 `(完整 channel, sequence)`。
`bad_request`、`too_large`、`forbidden`、`rate_limited` 與未知 code 是 terminal；
`clock_skew`、`unavailable`、`internal` 會先 terminally 清掉遭拒的 exact bytes，再從記憶體中解密
保留的 self-authenticated envelope，使用相同 plaintext/logical id 但新的 sequence、nonce 與
`ts` 重新 seal，絕不重播被拒 bytes。code／field 只以 closed typed labels 觀測，不輸出 channel
path、plaintext、ciphertext、account/device identity。malformed、uncorrelated `error`、未知 ack
status 與未知 authenticated frame 都被安全忽略／分類，不拆 socket；transport failure 本身才會
觸發 reconnect。

reconnect 的唯一 replay 路徑是一次 `sent` snapshot 的 `resendPersisted`，按 sequence 升序重送
仍屬 uncertain 的 exact persisted frames；terminal peer rejection 已從集合移除。receipt 仍只按
authenticated `(完整 channel, sequence)` 結算，duplicate、late、out-of-order 與 mismatch 保留
typed observation。global sequence、per-channel order、exact-frame idempotency、writer lock 與
terminal-frame cleanup 不變。

停止分成 fence/cancel、transport shutdown（關 socket）、join worker/deadline 三段；生產路徑先做
能解除 `URLSessionWebSocketTask.send` 懸置的 shutdown，再 join。排程入口與 deadline callback
都在 stopped 狀態 fail closed，因此 join 後不會再產生 deadline writer；stop 後 enqueue 回傳
typed permanent `CloudDurableOutboundError.stopped`。seal 失敗留下的 reservation 仍會取得自己的
maintenance wake，按 reserved freshness terminal burn，不再永遠卡住後續 row。

durable persistence 沒有新增 schema version 或 migration。`CloudDurableFile.commit` 仍採 write、
file fsync、rename、directory fsync 與既有 recovery；覆寫前僅以 descriptor metadata inspection
取代讀完整舊 payload，regular file、single link、uid、0600 mode、size、descriptor/path inode
一致與 live-writer lock invariant 均保留。

## 精確 changed-file manifest 與 overlay digest

- `Sources/CloudAppBridge.swift`
- `Sources/CloudDurableStores.swift`
- `Sources/CloudOutboundSpool.swift`
- `Sources/CloudTransport.swift`
- `Tests/CloudAppBridgeTests.swift`
- `Tests/CloudLifecycleTests.swift`
- `Tests/CloudOutboundSpoolTests.swift`
- `Tests/CloudTransportTests.swift`
- `docs/architecture-refactor.md`
- `docs/cloud.md`
- `docs/runtime-performance.md`
- `CLOUD_LATENCY_DELIVERY.md`

前十一個 implementation／test／doc 路徑的 binary full-index diff SHA-256 是
`0dd1a42319466ff85cdae0878a2330f7e79e0de6bcb5db9eac170afa956554ca`。計算方式：

```text
git diff --binary --full-index HEAD -- <上述前十一個明列路徑> | shasum -a 256
```

交付文件本身刻意排除，避免 digest 自我參照。這個 digest 只封存目前未提交 overlay；root 在
staging 前仍須重算並比對同一組路徑。

此 digest 包含 landing-root targeted acceptance 的 rollback correction：未新增
`staleBeforeSend` persisted enum case，live pre-send burn 改用舊版可解碼的
`staleReadyAtRestart`，並將 legacy-decoder 行為 guard 放入可直接選取的
`CloudOutboundSpool` suite。

## 封存 findings disposition

| ID | disposition | 精確處置 |
|---|---|---|
| S1 | fixed | typed、correlated `publish_error`；terminal／retry-new-attempt／unknown-terminal；authenticated 非致命 frame 不再拆線。現場 challenge identity 與 protocol deployment mismatch 均已由 sealed review 證據排除。 |
| S2 | fixed | 240 秒 authenticated sealed-`ts` pre-send policy；`s`／`orch` durable latest-value collapse；`t`／control stale terminal burn；live burn 沿用舊 reader 已知的 `staleReadyAtRestart` raw value。 |
| S3 | fixed | 4 MiB sent-frame cap 可在 32 MiB durable-file ceiling 前確實觸發，含真實 base64／metadata guard；capacity refusal 不重試。 |
| R1 | fixed | architecture、cloud、runtime 與本文件均改成 socket shutdown-before-join、無 post-stop scheduler 的真實語意。 |
| R2 | fixed | production-shaped uncancellable send 先由 transport shutdown 解鎖，再 join worker；測試 fixture 不再假設 task cancellation 足夠。 |
| R3 | fixed | composition 測試直接走 `requestDrain(reconnect: true)`／`resendPersisted` 生產路徑，涵蓋 uncertain replay 與 definitive rejection 排除。 |
| R4 | fixed | 移除 `sendNext(resendSent:)`、coalesce 舊入口、不可達 state branches 與過時註解；reconnect／latest-value 各只留一條實作。 |
| F1 | fixed | definitive rejection 先 durable terminal settle，永不進 reconnect replay；retryable rejection 使用新 attempt bytes。 |
| F2 | fixed | stop fence 同時守住 schedule entry、deadline callback 與 worker 尾端，取消並 join 既有 task 後不會再生成 writer。 |
| F3 | fixed | structural 與 transient failure 分型；structural 持久可見且不重試，transient bounded exponential retry。 |
| F4 | fixed | seal failure 也喚醒 maintenance owner；無 sent row 時仍依 stale-reservation deadline 自主終結。 |
| F5 | fixed | window observation 明確區分 process peak、current、wall-clock ready age 與 admission-refusal attempts，不再把跨時間/跨重試的數量叫成單次拒絕或 durable high-water。 |
| F6 | fixed | stopped enqueue 回傳 closed typed permanent `CloudDurableOutboundError.stopped`，而不是泛用 cancellation。 |

sealed review 另指出 inbound `reason=invalid` 診斷仍可能太粗；它不是上述 stable finding，也不屬
outbound durable row correlation。此項不在本 correction 擴張範圍內，交回
Clawdfather／runtime diagnostics owner；具體風險是 inbound command drop 仍可能只有 closed reason
而欠缺足以定位的非敏感細分。這不影響上述十三個 finding 全數 fixed 的結論。

## 驗證與代表性 red-before-green

驗證成本：2 個 static/source checks、4 個在 runner 執行測試前即變紅的 preflight/compile
correction invocations、1 個四套件 environment-inconclusive invocation、1 個最終三套件 green
invocation，共 8 次、263 秒（各工具 wait 的整秒合計）。沒有重跑已知 Keychain 環境缺口來換取
綠燈。

- baseline source guard 在修正前為紅：沒有 `case "publish_error"`；window cap 與 durable-file
  ceiling 同為 32 MiB；`CloudAppBridge.stop` 在 transport shutdown 前 join；stop 後 enqueue 丟
  非 typed `CancellationError`。這四項在目前 source assertions 全部反轉。
- `git diff --check`：通過。
- `bash tools/check-architecture-boundaries.sh`：通過；receipt 為
  `main=63, ceiling after lock (1571>1538), runners=53, groups=665, suite_files=67, held-lock doors=0, task-door control=124, direct lock=0, max suspension=80, parsed=6265`。
- 四套件 focused invocation 成功執行 CloudTransport、CloudAppBridge、CloudOutboundSpool，僅
  CloudLifecycle 的兩個既知 production Keychain `load-or-create` fixture 失敗；runner 原文同時報
  `CloudLifecycleTests: 2/149 failed` 與 `1 of 536 checks failed`，因此分類為
  `inconclusive_environment`，不當成 green，也不重試同一環境問題。
- 唯一累積 direct Cloud-focused green：
  `CLAWDLINE_TEST_PROFILE=infrastructure CLAWDLINE_SUITE_JOBS=1 ./test.sh --cloud-focused CloudTransport,CloudAppBridge,CloudOutboundSpool`，在 worktree snapshot 執行，exit 0。
  suite receipt 是 `CloudTransport:95, CloudAppBridge:199, CloudOutboundSpool:239`（明細合計
  533）；runner 另印 `537 checks passed`，並印出機器可解析的
  `CLAWDLINE_CLOUD_FOCUSED_TESTS_COMPLETE v=1 suite_count=3 suites=CloudTransport:95,CloudAppBridge:199,CloudOutboundSpool:239`。
  兩個原始數字均封存，未將投影 headline 改寫成套件明細。

測試涵蓋：live `s`／`orch` collapse 與 `t` retention、sealed-`ts` freshness、真實檔案 exact
4 MiB admission boundary、persistent structural failure 不重試、reservation autonomous wake、
production-shaped stop/unblock/join、post-stop enqueue/deadline fence、typed Relay dispositions、未知
authenticated frames 保持同一 socket、production reconnect replay、definitive refusal 排除、
retry-new-attempt 的新 sequence/nonce/ciphertext。implementer/correction 沒有執行 full suite、build、
restart、deploy、push、second review 或 commit。

Landing-root targeted confirmation（2026-09-13 Asia/Taipei）另做一次有效 mutation red 與最窄
green：在 disposable snapshot 暫時加入 `staleBeforeSend` 並讓 live pre-send 寫入該 raw value，
`./test.sh --cloud-focused CloudOutboundSpool` 於 legacy decoder 以 `dataCorrupted` 變紅（3 checks
attempted、exit 1）；還原後同一 direct suite 為 `CloudOutboundSpool:240`、runner 244 checks、exit 0。
此前一個只改 live source、但 assertion 仍只走 restart 的 probe 維持 green，因沒有回答 live-write
問題而被明確丟棄，未當成 red evidence。兩次有效 run 均使用隔離 worktree snapshot；第一次受限
sandbox 的 pre-cloud stop 屬環境 inconclusive，改以可寫入既有 harness cache/temp 的同一問題執行。

Landing-root 唯一一次 exact staged-tree release-train full 隨後完整到達十二個 Cloud suites，並在
12,726 attempted checks 中發現兩個 `CloudLifecycle` production pairing assertions 失敗。兩者的共同
原因是 fixture 使用預設 Keychain identity authority：已完成 enrollment 的開發機會直接讀取現有
protected identity，因而繞過該測試注入的 hanging `CloudKeys` store。這不是環境不確定，也沒有
重跑 full。root 將 production composition fixture 改為顯式空白的 in-memory protected authority，
保持 production factory、timeout 與 cancellation 路徑不變；原 full 是此缺口的 red evidence。
唯一最窄確認 `./test.sh --cloud-focused CloudLifecycle` 隨後為 `CloudLifecycle:149`、runner 153
checks、exit 0。最終 landing 採 sealed focused receipts、上述 exact-full 其餘 12,724 checks 與這個
direct correction receipt 的 composed evidence。

## Rollback

沒有持久化 schema version 變更；live stale burn 刻意沿用前一版 reader 已知的
`staleReadyAtRestart` raw value，且有 legacy-decoder focused guard，因此既有與新寫入的 spool rows
及 sealed frames 仍可由前一版 reader 讀取。
若 stable live sequence 顯示 backlog、ordering、failure isolation 或 latency 惡化，Clawdfather 應把
native 與 hosted console 一起回退到前一個 accepted commit；不得手動刪除 spool、ready/sent rows
或 writer-lock inode。source rollback 必須把本 manifest 的十二個路徑視為同一單位，避免只還原
transport／worker 卻留下互不相容的 spool、tests 或契約文件。

## 尚待 Clawdfather landing 與 live acceptance

1. root 對 exact staged candidate 執行 targeted acceptance、release-train gate、landing 與 release；
   本 child 不宣告 `SAFE TO LAND`，也不 commit。
2. 在同一 named build、instance、account 與 payload distribution 取穩定序列，不採 restart 後第一個
   點：連續記錄 spool ready rows/bytes、sent rows/bytes、whole-file size，以及 publication
   enqueue→seal→socket→receipt 的 p50/p95。接受條件是 ready/size 收斂、publish latency 有界，且
   不再出現一筆每 30 秒推進或 1 Hz full encode。
3. socket／Relay 序列必須證明 `clock_skew/ts` 或其他 `publish_error` 不再被分類為
   `unexpected_frame`／reconnect loop；terminal refusal 不 replay，retryable refusal 產生新 attempt，
   reconnect 僅 replay uncertain sent rows。
4. 同一序列檢查 global/per-channel ordering、duplicate/out-of-order/late/mismatch counters，以及
   closed code/field、structural failure、window admission refusal、live stale-before-send、latest-value
   replacement 等 fault metrics；量到未達標必須回報原始值。

**Fixed but not yet released (awaiting review)：本 manifest 的全部修正。**
