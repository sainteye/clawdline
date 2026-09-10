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
| Cloud encrypted command/read | [CloudTransport.swift](../Sources/CloudTransport.swift):875–896 先辨識 paired sender、驗證簽章／解密，再檢查 sequence；[CloudAppBridge.swift](../Sources/CloudAppBridge.swift):68–98、199–238 保留 typed read vocabulary 與 server gate | W0-E 比對 wire/consumer vectors；W0-C 測量 buffering；W5-1/2 驗證 durable replay／duplicate effects、pairing rotation 與 reconnect，不能由 in-memory tracker 推定重啟安全 |
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

## 還不能下的結論與責任

- **W0-E／W0-D**：authority cutover、shared vectors、API／Relay／PWA consumer 與 release identity 尚待交付。W0-A 沒有批准 revocation fail-open／fail-closed policy 的變更。
- **W0-F／W3**：crypto、networking、Foundation portability 的 compiler／vector 證據待取得；目前 import 掃描不代表 Ubuntu 支援。
- **W0-C／W5／W6**：disk-full/corruption、overload、slow consumer、duplicate/reconnect、重啟／備份／還原及 redaction runtime 證據待交付；Plan v4 budget 尚不能當本次測量值。
- **CLA-296 root**：取得獨立安全／架構 review，處置發現、驗證 exact tree、整合。此次交付不變更 production policy、不切 authority、不部署、不代替使用者同意。
