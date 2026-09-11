# 平台能力矩陣：固定 Mac 現況與 Ubuntu MVP 接受契約

W0-A，2026-09-10。Mac source commit `6f4411f1365258d1e1b41b76c85dcaa1dcb6b88a`，tree
`75314e95a17d901362c0d998477205606b9add5d`；來源讀取時間 2026-09-10 13:19–14:10 UTC。
下表的「現況」是原始碼閱讀，**本次未啟動 provider、編譯 Swift、重啟 app 或驗證已部署版本**。
Ubuntu 每列都是 Plan v4 的 MVP 契約，狀態均為尚待實作／驗證；沒有因 Mac 存在相似功能就標成支援。

能力與 command/query 的方向見 [ADR 0001](adr/0001-platform-boundary-and-evidence.md)，
GCE 信任與 wire authority 見 [ADR 0002](adr/0002-cloud-authority-and-executor-trust.md)。

| 能力 | 固定 Mac 現況與證据 | Ubuntu 24.04 amd64 MVP 必須證明 | 後續責任／明確缺口 |
|---|---|---|---|
| install／package provenance | [Package.swift](../Package.swift):4–22 是 editor-only macOS executable；[build.sh](../build.sh):918–925 編譯 arm64 Apple target，`:966–979` 寫 version，`:1062` 可 ad-hoc sign、`:1102–1103` 有 Developer ID 路徑。不能稱每個 build 都有 release signature | versioned amd64 package 驗 signature／digest，記 source SHA、resources／schema／protocol identity；immutable release directory、atomic current link、可驗 rollback | W0-B 精確 compile 輸入；W0-F 非空 Linux probe；W3/W4-2 正式 package／upgrade。Mac signing 不是 Ubuntu provenance |
| provider install／auth | [Assistant.swift](../Sources/Assistant.swift):267–279 以既有 Claude／Codex CLI 啟動、沒有 provider token 參數；`:305–316` 只看 home 目錄以判 installed，不能證明 binary 或登入成功 | 以真實 Codex／Claude 完成啟動與交談，provider auth 經 secret boundary；token 不入 argv／log／URL／artifact，失效／撤銷可診斷 | W4-1；現有 CloudKeyStoring 僅是 Cloud key seam，不能拿來宣稱 provider provisioning 完成 |
| project registration／containment | [OrchestratorDraft.swift](../Sources/OrchestratorDraft.swift):338–340 驗可用 absolute directory，`:422–447` 拒 claims 的 absolute／`..`；claims 是 reservation，不是 filesystem lock | durable disk 上 allowlisted project roots；拒 traversal、symlink escape、undeclared writes；說明由 OS／sandbox／broker 哪層執行，每層有反例 | W4-1；目前沒有從 claims 推得全面 write isolation 的證據 |
| HOME／cwd／env | [Codex.swift](../Sources/Codex.swift):19–29 依 config、CODEX_HOME、user home 選路徑；[Assistant.swift](../Sources/Assistant.swift):203–235 清除 identity variables，保留其餘 login-shell env；[StartPoints.swift](../Sources/StartPoints.swift):112–119 quoted cd | non-root uid/gid、umask 0077、durable/isolated HOME、cwd、env allowlist 明列；provider secret 與子 process 不越界；拒繼承他人的 session identity | W4-1；Mac 是 env denylist，不是 Linux allowlist；隔離 HOME 不能破壞 provider 登入 |
| PTY／process ownership | [Tmux.swift](../Sources/Tmux.swift):562–620 透過 login shell、`-c cwd`、send-keys；`:296` 仍依 `ITerm.assistantPIDs()`；[ITerm.swift](../Sources/ITerm.swift):1221–1234、1326–1338 用 ps/tty/ppid/pgid | tmux socket 只屬服務使用者；PTY 與 child PID／start identity／process group 的 ownership 可追溯；使用 Linux procfs/POSIX 證据，避免誤殺重用 PID／他人 pane | W4-1；目前 tmux code 存在不代表已從 iTerm／Mac 工具分離 |
| terminal lifecycle | 同上；send-keys 成功只表示送達 tty，不等於 shell／agent 已執行；mutation admission seam 現由 [TerminalCommandScheduler.swift](../Sources/TerminalCommandScheduler.swift):60–114 擁有（W2-1 從 RemoteServer.swift 移出，depth 8／per-channel 2／nested-inline reservation 與 restart-maintenance rejection/drain 都在這個獨立檔），`RemoteServer` 保留原有 facade 名稱 | create、send、observe、interrupt、resize（支持時）、close、enumerate 的真實 tmux contract；共享一個 admission owner，完整區分 accepted/executed/delivered/observed/acknowledged | W4-1；terminal admission owner 已在 W2-1 獨立成檔，不因 Linux adapter 再建第二條 terminal lane |
| task／result／receipts | [Orchestrator.swift](../Sources/Orchestrator.swift):1260 task root 在 `/tmp/.clawdline`；`:7905–7923` ingests result 並比 secret hash；`:10194/10293` load/save | bounded dispatch、claim/admission、authenticated result、receipts 與 restart parity；authoritative task 在 durable `/var/lib/clawdline/tasks`；corrupt／unreadable 不當成空成功 | W4-3；`/tmp` 是目前 Mac 路徑，不能直接沿用為 Linux durability 契約 |
| Board／Session reads | [RemoteServer.swift](../Sources/RemoteServer.swift):972–1005 認證後入 route；[ProjectBoardHTTP.swift](../Sources/ProjectBoardHTTP.swift):96–182 分開 capability、read selectors 與 write gate；[CloudAppBridge.swift](../Sources/CloudAppBridge.swift):68–98 typed reads | 保持 project／Session scope、authorised read 與無權限拒絕；不能由 read 授予 dispatch/write，不能改 Board consent 或 lifecycle | W4-3；本任務只描述 seam，不編修 Board implementation／tests／原有 docs |
| document reads | [ProjectArtifact.swift](../Sources/ProjectArtifact.swift):227–245 處理 project/task artifact roots，266–289 檢相對 path、resolved containment、regular type、extension、size；140–150 明載 hard-link／TOCTOU 限制 | 同一 machine、Session、scope、task、relative path 對应同一授權檔；相同拒絕測試及 Cloud read parity；惡意 local writer 防線不得被默認已有 | W4-3；project artifact symlink 可成新根，task artifact 必須仍在 task dir；Cloud URL 身分不可用 localhost 或 `machine=this-mac` 代替 |
| Cloud pairing／secret／replay | [CloudKeys.swift](../Sources/CloudKeys.swift):456–483 為 Keychain seam；[CloudTransport.swift](../Sources/CloudTransport.swift):875–896 驗 paired sender、解密、sequence；[CloudAppBridge.swift](../Sources/CloudAppBridge.swift):199–238 共用 server gates | headless device-code approval、E2EE pins、protected secrets、rotation/revocation、reconnect/resume、duplicate-effect 保護；不暴露 credential | W0-E vectors，W5-1/W5-2 wiring／runtime；目前 ledger/spool 類型存在不能代替已接線證明 |
| Mac-only capability routing | AppKit/iTerm/Apple Events/voice/build code 存在；此 tree 對 `capability_unavailable` 的 Swift 文字搜尋無命中，僅是字串觀測 | discover 支援能力；Xcode、iTerm2、Apple Events、voice、Mac-only build 在 side effect 前回 typed `capability_unavailable`，或只依明文批准 policy 路由到指定 host | W2/W4-1；不靜默缺欄、不假裝成功、不擅自轉到另一台 Mac。具體 capability ID／wire schema 由後續相容契約固定 |
| daemon restart | [SessionWatch.swift](../Sources/SessionWatch.swift):1179–1184 要 ready 且 outstanding 全零，1189–1203 以 queued-secret recoverability 判 blocker；spawning 一律阻擋，queued sealed secret 能解且 hash 相符可恢復；1207–1233 replacement 先 reconcile | tmux service 與 daemon service 分立，restart daemon 保留 terminal；停 admission、drain／durable reconcile 後才恢復；receipt/identity/epoch 不倒退 | W4-2；不能照泛用「queued 一律 unsafe」覆蓋這個固定 tree 的實作；本次無 restart 測量 |
| VM reboot／unknown outcome | [SessionWatch.swift](../Sources/SessionWatch.swift):1015–1024 loss/restart grace 與 incomplete inventory 不判 lost；這是 Mac source policy | reboot 會遺失 process；所有 in-flight effects 明確 reconciliation 成 `interrupted`／`unknown` 或有實證的狀態，不能推定成功；durable `/var/lib` 恢復 | W4-2/W6；Plan v4 60s p95 restart／ready 後 120s reboot 分類是待 W0 evidence 批准的目標，不是目前量測或既有 grace 等價值 |
| upgrade／backup／restore | Mac build/sign 路徑不證明跨 schema backup/restore；本次沒讀 live snapshots | signed versioned upgrade、additive schema readers、atomic rollback；restore 非空 repos/task/evidence/secrets，驗相容與跨 record invariants，避免 sequence/epoch reuse | W4-2/W5-4/W6；RPO/RTO 須由 drill 證明，image rollback 不是 schema rollback |
| GCE operations／health | [docs/cloud.md](cloud.md):3–6 記 separate private API/relay；本次 source inventory 不包含 live health、IAM、服務部署 | 一 VM 一 account/user、persistent disk、no public daemon port、outbound TLS、OS Login/IAP、least-privilege service account；health 有 exact build/protocol/schema，結構化/redacted logs、disk/memory/queue/reconcile age | W0-D Cloud evidence；W5-3 獨立 GCE IaC/state；W6 故障驗證。Ubuntu executor 加入 plaintext 信任範圍，見 trust doc |

## 怎麼使用這張表

W0-F 的首個證據只應將「非空 Core 可以在指定 Ubuntu/toolchain compile」補實，不能一次把
全表升為支援。每個 runtime capability 的接受紀錄至少要包含 exact source tree、package／
build identity、OS/architecture、command、成功與拒絕情境、check count、duration、outcome。
restart／reboot／restore 需另列 interruption point 與 durable state，健康 endpoint 本身不足以證明。

技術後續：W0-E 等 W0-A/W0-D；W0-F 等 W0-A/W0-B；root 再進獨立 W0 review、correction、
exact verification、landing。本任務沒有待使用者決策；後續 policy／budget 的批准由 CLA-296
root 明確處理，不能把本表當成已取得批准。更完整的來源檔案與文字盤點見
[generated inventory](generated/platform-architecture-inventory.md)。
