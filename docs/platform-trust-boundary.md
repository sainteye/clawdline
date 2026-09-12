# 平台信任邊界：Mac、Cloud 與 Ubuntu executor

W0-A 現況與契約基線，2026-09-10；source commit
`6f4411f1365258d1e1b41b76c85dcaa1dcb6b88a`、tree
`75314e95a17d901362c0d998477205606b9add5d`。讀取區間與 authority 規則見
[ADR 0001](adr/0001-platform-boundary-and-evidence.md)／[ADR 0002](adr/0002-cloud-authority-and-executor-trust.md)。
此文是 threat model 與 source evidence，不是安全稽核通過或 GCE 部署證明。

## 資產在哪裡、誰必須被信任

| 範圍 | 可見資產與權力 | 信任界線與限制 |
|---|---|---|
| 使用者的 PWA／瀏覽器 | 配對後持 content key、自己的 signing key，解密 Session／document；發出已授權 command | browser origin、供應的 JS、擴充套件及 endpoint OS 可影響明文。E2EE 不保護已被控制的 viewer；read permission 不自動授予 write／dispatch |
| Cloud API control plane | account/device identity、entitlement、schedule/control records、token/public-key/auth metadata | 接入控制与可用性依賴 API；它不需要 content key 才能維持這些控制資料。不能把 API 的 auth 決策當成 host effect completion |
| Relay encrypted data plane | envelope ciphertext、channel、sender、sequence、timestamp、key id、class／signature，以及連線與流量 metadata | relay 可拒絕、延遲或重送流量；E2EE 不隱藏流量 metadata，也不保證 availability。host 的 pinned key／wire validation／replay checks 是另一道邊界 |
| Mac executor | 明文專案、terminal、transcript、task artifacts；Cloud content/device keys 在 Keychain；provider CLI 使用自己的登入狀態 | 登入使用者、被授權 agent 與足夠權限的本機管理者屬 endpoint 信任範圍。Clawdline Cloud key store 不等於 provider credential store |
| Ubuntu/GCE executor（契約，尚未提供） | 與 Mac 同類的 plaintext/process/key 資產，durable disk 還包含 repo、task、registry、receipt | VM admin、可讀取其記憶體／磁碟的基礎設施管理權限、snapshot／backup operator 都在 endpoint threat model 內。GCP 不因 Relay 是 E2EE 就被排除在 plaintext 信任範圍外 |
| Codex／Claude 等 provider | agent 向 provider 提交的 prompt、context 及 provider auth 使用 | Clawdline E2EE 只涵蓋 viewer↔executor transport，不能聲稱涵蓋 provider 的處理或保留政策。W0-A external AI remains OFF；本次沒有另外呼叫外部模型服務 |
| Git／package／update 管線 | 執行碼、資源、相容版本及 migration 輸入 | package signature/digest/source SHA 證明來源範圍，不證明無漏洞；image rollback 不回復 schema。source inventory 是來源觀測，不能當 release provenance receipt |

上表 Cloud control-plane 部署分工來自 Plan v4 與本 repo `docs/cloud.md:3–19`，並未在本次
讀取 live API／Relay／GCP 設定。GCE 管理權限列是保守的 endpoint threat-model 假設，不是
對某個 IAM policy 的實測。GCE 的獨立部署證明由後续 W5-3 負責。

## 每個入口各自授權

| 入口／動作 | 固定來源與目前保證 | 接受證據尚需涵蓋 |
|---|---|---|
| local HTTP machine／paired device／task secret | [RemoteServer.swift](../Sources/RemoteServer.swift):972–1005 分開三種 authority；task secret 不是 machine token，自身 `/inflight` 是特定唯讀例外 | 不授權 child 讀 machine credential；不能憑同一 token 可讀就推論所有 mutation 也可用 |
| Cloud encrypted command/read | [CloudTransport.swift](../Sources/CloudTransport.swift) 先辨識 paired sender、驗證簽章／解密，再檢查 sequence；[CloudAppBridge.swift](../Sources/CloudAppBridge.swift) 保留 typed read vocabulary 與 server gate。W5-1 的 shared Application ledger 在 effect 前持久保存 request identity／digest／stage，outcome 在 reply 前 durable；shared spool 單獨擁有全域 sequence、exact frame、sent 與 authenticated correlated settlement | W0-E candidate bytes 已被 code pin 住但未 cutover／emit；W5-2 仍須證 pairing／rotation／revocation，W5-3/W6 仍須 live Relay/GCE、restart/restore 與容量證據。不能由 source candidate 或 in-memory receive tracker 推定部署安全 |
| Board read/write | [ProjectBoardHTTP.swift](../Sources/ProjectBoardHTTP.swift):96–105、117–182 分開 selectors、capabilities 與 write gate | Ubuntu read parity 必須測 project／Session 範圍與無權限拒絕；不改 Board consent、敘事或生命周期 |
| document read | [ProjectArtifact.swift](../Sources/ProjectArtifact.swift):227–245 解出 project／task artifact root，266–289 檢查相對路徑、resolved containment、file type／size | project artifacts symlink 可成新根；task artifacts 須在 task dir。`:140–150` 明載 hard-link 與 check/read 間 symlink race 不受完整保證，不可宣称抵擋惡意 local writer |
| task result | [Orchestrator.swift](../Sources/Orchestrator.swift):7905–7923 對 result secret 做 hash 比對 | 檔案到達與 broker 接受只是 delivery 各階段；不等於獨立 review、exact-tree acceptance、landing、release 或人已看到 |
| project writes | [OrchestratorDraft.swift](../Sources/OrchestratorDraft.swift):338–340、422–447 驗 project_dir 與 relative claims；[CONTEXT.md](../CONTEXT.md) 定義 claims | claims 是 admission reservation，沒有證明 OS 阻止所有 undeclared writes；Ubuntu 需另證 allowlisted root、symlink escape 與 enforcement |

## Ubuntu/GCE 的不可省略契約

第一個 profile 是 Ubuntu 24.04 LTS amd64、單一 user/account 每 VM、dedicated non-root
`clawdline`、`UMask=0077`。workspace／task／registry／receipt 存 durable `/var/lib/clawdline`，
不把 `/tmp` 當 authoritative state。HOME、cwd、env allowlist、PTY、uid/gid、process ownership
各有明確來源；provider token 不出現在 argv、log、URL 或 task artifact。agents 不以 root 跑。

daemon 不開 public port，只需 outbound TLS；運維採 Plan v4 指定的 OS Login/IAP、最小權限
service account、persistent disk、snapshot 與非空 restore drill。這是預定接受條件，沒有由
W0-A 建 VM、IAM、service unit 或 firewall rule。secret rotation/revocation、backup 存取與
restore 後避免舊 epoch／sequence 重用，必須和 release/migration identity 一起驗證。

`clawdline-tmux.service` 與 daemon service 分立：daemon restart 要保留 terminal；VM reboot
無法保留 process，未完成 effects 必須標為 `interrupted`／`unknown`，不能依 durable accepted
receipt 猜成 success。restore 不得讓舊 receipt、key identity、sequence 或 revocation epoch
倒退。Mac-only capability 需明確 typed rejection，不能靜默在另一台 Mac 執行。

W4-2 candidate 將這條界線落在兩個沒有 `PartOf` 關係的 unit：只有 foreground-supervised
`clawdline-tmux.service` 擁有 `/run/clawdline` 生命週期，daemon restart 不刪 tmux socket。
caller-pinned public key、provenance、signature、archive 先由 descriptor/no-follow 複製到
root-owned 0700 immutable staging；驗簽、digest、精確解壓全程只使用該組 bytes，且 signed
schema/protocol 必須等於 target binary 自報 contract。這證明被安裝 bytes 的來源／完整性，
不證明程式無漏洞。release image 回復不觸碰 `/var/lib/clawdline`；舊 image 讀不了目前 schema
時 fail closed。壞、未知或語意矛盾 state 先留下 fsynced recovery obligation 再 quarantine，
普通 tick/restart 不能把缺少 canonical file 誤升為空 store。

exact unit parse、package 與 private-root failure injection 不等於 production systemd-as-PID-1 或 GCE
隔離實測；本機亦沒有可交付的 Claude/Codex 真 credential receipt。因此 service health 可對
release/schema/reconciliation 成為 `serviceReady`，provider `ready` 仍維持 false 和
`w4_provider_authentication_not_proven`，不把 shell fixture 升格為外部 auth 證據。

W4-3 candidate 把 Linux task authority 固定在 `/var/lib/clawdline/tasks/authority.json`，並讓
既有 daemon ingress owner 單獨序列化 task/terminal effects；Board 只由該 authority 唯讀投影，
沒有第二個 Board writer。Task secret 只保存 digest，並在 command/task existence branch 前
constant-time 比對；result bytes 在 task root 單獨 fsync，authority 再發布 digest/count/receipt，
而 startup、cached replay、ack、close 都以 pinned descriptor 重新驗 bytes。Document content
與 list 從 exact project/Session/task identity 選 computed root，以 descriptor-relative
no-follow/single-link/bounds 走訪；list 不在驗 root 後退回 pathname enumeration。舊
`records/runtime-state.json` 僅作一次 migration source，在 atomic fence replacement 前一直保留
可讀舊 authority；package rollback 同時核對 service UID/private/regular/single-link/bound/recordKind，
failed health 不會先切回讀不懂最新 authority 的舊 image。這些界線不把 localhost identity 推成 Cloud identity，
也不證明可抵擋同一 service uid 的惡意 process。

W5-1 candidate 將 Cloud command ledger 與 outbound spool 放進共用 Application target，Mac
production 在 attach bridge 前、Linux daemon 在開 ingress admission 前開啟它們。檔案 store 以
0700 directory、0600 single-link regular file、owner／size／version checks、single-writer lock 及
file fsync → rename → directory fsync 的 atomic commit 更新；失敗時 actor snapshot 不超前於
durable bytes，invalid state 被保留或 quarantine，startup fail closed。正常 reconnect 只由
spool 重送同一筆 exact sent frame，transport 自身不排隊、不另取 sequence；ack 需同時符合
authenticated 完整 wire channel 與 sequence 才能 settlement。logical durable row 只保存
identity、byte count 與 payload digest，明文只存在 encrypted exact frame；0600/0700 權限、
parent-directory fsync、固定 candidate 名與 bounded orphan cleanup 都 fail closed；cleanup 超過
128 個 directory entries 或 64 個 legacy candidates 時保留證據並拒絕啟動。Mac command
在 effect point 重讀真實 epoch guard、paired roster 與 write gate；lost ACK 由 cancellable deadline
wake 自動 burn，durable-store 暫時失敗時以 bounded backoff 再排程，receipt lane 固定 256 並記錄
overflow。Linux composition 尚無 Cloud authentication，
故只有 durable readiness，沒有 publish 能力。Mac 的舊 `cloud-sequence.json` 也以同一 file
authority checks 讀取；新 spool 先 durable 套用 sender reserved ceiling，且每逢舊 image block
boundary 都先提升 schema-compatible predecessor ceiling，rollback 只會跳號、不會重用；不能把
invalid legacy state 當作 zero。這些 source failure-injection fixtures 不等於
實際 disk-full、live Relay、GCE 或 PID-1 證據。

W5-2 candidate 將 account bootstrap、Ed25519／content key、pairing pin、rotation、revocation 與
reconnect identity 收進單一 Application `CloudExecutorIdentityAuthority`。公開 client 的 normative
路徑是 identity start、offer/grant/activate/confirm phase write/poll 與 machine identity rotation；
viewer 寫 offer/activate、machine 寫 grant/confirm，confirm receipt 前不得 pin，reply-loss 重送相同
canonical bytes 只得 duplicate。三-call 路徑僅相容已開始的舊 handover。Mac 的 Keychain 與
Ubuntu 的 protected-file store 只是 adapter；兩邊讀寫同一 canonical、4 KiB bounded record，且
missing／locked／unreadable／corrupt／future／wrong-owner／linked／partial／migration failure 都不會
改走 memory 或空 identity。Offer 的 exact canonical bytes、claim nonce digest、viewer signing／
ephemeral key 與 fingerprint、expiry、machine fingerprint 及 sealed grant 在 delivery 前 durable；
第二 claimant、late/mismatched phase、低階 X25519、nonce reuse 與 fingerprint substitution 在 key
release 或 pin mutation 前拒絕。Rotation 和 revocation 各自前進明示 epoch/generation，舊 key id
留在 retired fence；reconnect 只有 exact account/machine/device/epochs 且 W5-1 ledger/spool 都已開啟
才可 resume。Mac live transport 與 effect-time roster 由此 owner 重讀，不再由舊 JSON roster 決定
production authorization；初次 enrollment 先 atomic rename＋directory fsync，讓舊 binary 找不到
authority pathname，再從 fence 匯入 protected state，成功後才刪 fence。Crash 從 fence 重試，降版
不會復活舊 viewer。每次 initial/reconnect 都在 W5-1 owners 開啟後驗 exact epochs；公私
byte-identical 公私 843-byte route-role vector SHA-256 為
`2f79369d4ee866976c6da2a41358c1aab5321ab0e6054bf8a3afe16e215f69a9`；另有 public lifecycle
extension `79504ce608fd278cecdb2e26f62d6d1c7e915ef66f94a79cc12ff240a95e410e` 固定 exact members、
duplicate replay 與 confirm receipt 後才 pin。
既有八欄 v1 handover 與 W0-E candidate bytes 不變，Linux emission 仍關閉。

## 還不能下的結論與責任

- **W0-E／W0-D**：W5-1 code pin 住 public candidate commit/tree/source/package digests，並在 `authority=candidate`、`cutover_required=true` 時拒絕 cutover／emit／reader-floor raise；正式 authority cutover、API／Relay／PWA consumer 與 release identity 仍待接受。W0-A 沒有批准 revocation fail-open／fail-closed policy 的變更。
- **W0-F／W3**：Application/Linux product graph 使用 Linux-only exact `swift-crypto` 4.5.2 並有 focused compile；這仍不是真 Ubuntu VM、network 或 provider receipt。
- **W0-C／W5／W6**：W5-1/W5-2 deterministic fixtures 涵蓋 durable store、duplicate/reconnect/ack loss/reorder、protected-state refusal、兩 claimant、pair/revoke/rotate/restart 與 Mac/Linux Application parity；private API/Relay epoch enforcement、真 disk-full、live Relay、overload、slow consumer、VM restart／備份／還原及 runtime confidentiality 仍待交付。Plan v4 budget 尚不能當本次測量值。
- **CLA-296 root**：取得獨立安全／架構 review，處置發現、驗證 exact tree、整合。此次交付不變更 production policy、不切 authority、不部署、不代替使用者同意。
