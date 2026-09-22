> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.

# ADR 0001 — 平台邊界、狀態擁有者與證據分級

狀態：W0-A 決策已由 W3-1/W3-2 建立正式 targets；W4-1 至 W4-3 建立 Linux runtime／service／task lifecycle，W5-1 candidate 再把 Cloud command ledger 與 outbound spool 接入 Mac／Linux composition。Cloud pairing、authority cutover 與外部 GCE 驗收仍未完成。
決策日期：2026-09-10。來源讀取時間：2026-09-10 13:19–14:10 UTC。
來源 commit：`6f4411f1365258d1e1b41b76c85dcaa1dcb6b88a`；tree：
`75314e95a17d901362c0d998477205606b9add5d`。本文行號只對這個版本成立。

## 決策與適用範圍

Local HTTP、Cloud relay 收到的輸入及 CLI 是 **inbound adapters**：解碼、認證後呼叫
Application commands／queries。terminal、process、filesystem、durable store、secrets、
clock/ID、event publication、optional notification 是 **outbound capability ports**：介面由
Core／Application 所需行為定義，Mac／Linux 實作向內依賴它們。
transport 不是 state owner，outbound port 也不是呼叫 Application 的入口。

這個方向沿用 CLA-296 Plan v4，約束後續 extraction；W0-A 只新增文件和原始碼盤點工具。
現有 Mac facade 必須繼續委派；每個 mutable fact、sequence epoch 與 admission lane 只能有
一個寫入擁有者。不得因新 adapter 再建一套 task registry、terminal queue 或事件序號。
對 ownership 的判斷必須讀宣告與寫入路徑，不能把下表變成另一套 runtime registry。

```text
inbound: local HTTP / Cloud relay input / CLI
                    ↓ command or query
            Application → Core transitions
                    ↓ invokes application-defined outbound ports
          terminal / process / persistence / secrets / publication
                    ↑ implemented by
              Mac or Linux composition
```

Core／Application 不得依賴 AppKit、Security、ServiceManagement、Speech、AVFoundation、
Carbon、iTerm 型別、HTTP server singleton 或平台 executable。Mac／Linux composition 是
依賴圖末端。不能用到處增加 `#if os(Linux)` 代替邊界，也不要求一個籠統 Infrastructure target。
此方向現由 `ClawdlineCore -> ClawdlineApplication -> Clawdline`／`ClawdlineLinux` 的 SwiftPM
target graph 實作並由 resolved-source/edge guard 固定。W4-1 在 Application 加入 project-root、
provider launch/menu 與 terminal lifecycle policy，並由 Linux composition 注入 tmux、procfs、
contained filesystem 與 protected-file secret adapters。既有 Mac facade 與 Linux composition
共同使用 Application 的 `TerminalCommandScheduler`，沒有第二套 admission owner。health 仍刻意
`ready=false`：未配置時是 `w4_runtime_not_configured`，配置並驗證 executable／Landlock／seccomp
後是 `w4_provider_authentication_not_proven`；它不把 fixture、尚未存在的 real-provider auth、
service supervision、listener 或 Cloud lifecycle 宣稱為 ready。

## 固定版本的現有依賴與所有權

| 邊界／資料 | 現有 owner 與來源 | 尚未完成的邊界 |
|---|---|---|
| build／package | `Package.swift` 定義 Core／Application、Mac／Linux executable products與 Linux runtime test target；~~`architecture guard` 讀 SwiftPM resolved sources／edges／products~~（**2026-09-16 更正：該守衛已從樹中移除且無替代品**，見 [architecture.md](../architecture.md)）；`build.sh` 在 machine lock 內編譯 `Clawdline` product，再以 digest-checked copy 組 bundle／sign／install | W4-1 的 macOS focused proof只回答 shared policy／Linux target 可編譯與平台中立 contracts；pinned Ubuntu job 才執行 non-root real-tmux lifecycle。service listener、restart reconciliation、package upgrade／rollback 與 Cloud lifecycle 仍分屬 W4-2/W4-3。Mac bundle wrapper 不是 Ubuntu package provenance |
| task／handoff／root assignment／Session facts | `Orchestrator.swift`:1287、1313–1333 的 collections 與 lock alias，`:10194` load、`:10293` save | facade 仍混合 admission、state 與 effects；不能宣称 Registry 已接手全部資料 |
| graph admission reservation、terminal title／role 與部分 label projection | `OrchestratorRegistry.swift`:30–64 的 private collections；`:198` transaction，`:221` held-lock door，共用原有 NSLock | `Transaction` 還可能逸出 closure；held-lock door 仍依呼叫者持鎖慣例，不是型別系統的完整同步保證。新工作繼續既有 extraction 次序 |
| orchestration disk codec | `OrchestratorStore.swift`:3–18、`:79` stored task，包括 legacy decode／missing-field 語意 | 它是 serializer，並未擁有 collections、load/save 排程或 disk-health policy；不能藉搬移改 corruption／version 行為 |
| HTTP／Cloud admission、terminal mutation | `RemoteServer.swift` 的 legacy facade 委派 `TerminalCommandScheduler.swift`；Linux lifecycle 也由同一 Application owner 實作包住 tmux preflight/effect；`CloudAppBridge.swift`:199–238 的 router 委派同一 server | `RemoteServerCloudCommandRouter` 仍有 concrete `RemoteServer`；不能把 `CloudCommandRouting` 協定誤認成已獨立的 portable Application module |
| Session observation／restart maintenance | `SessionWatch.swift`:113–114 發布 inventory；`:983` executor reconciliation、`:1154` restart phase | observation 與 task 持久事實分開；incomplete inventory 不推定 executor 消失。restart 細節見能力矩陣 |
| Board、usage、verification data | `ProjectBoardStore.swift`:467–468 自有 locks；`UsageLedger.swift`:885 自有 queue；`VerificationRunLedger.swift`:117–135 自有 store／serial queue | 是不同資料領域與 durability policy，不能合併成泛用 store，也不在 W0-A 修改範圍 |
| Cloud transport／content keys／序號 | `CloudTransport.swift` 驗證 paired key、解密、sequence gate，再送 inbound stream；W5-1 後 transport 只接受 Application spool 已封好的 exact publish-frame bytes 並回傳 authenticated channel／sequence receipt；`CloudKeys.swift` 為 key storage seam | transport 不擁有 outbound queue、sequence 或 reconnect bytes。protocol 的存在仍不代表 Linux crypto／networking 已相容；pairing／rotation 與 accepted authority cutover 屬 W5-2／root |
| command ledger／outbound spool | `CloudCommandLedger.swift`、`CloudOutboundSpool.swift`、`CloudDurableStores.swift` 現由 Application target 共用；Mac bridge 與 Linux daemon 在開始 lifecycle／admission 前開啟同一套 single-writer、versioned、descriptor-checked file authority。ledger 在 effect 前保存 reservation／in-progress、reply 前保存 normalized outcome；spool 保存全域 sequence、exact frame、sent 與 correlated settlement | W5-1 是 source candidate，未執行 W0-E cutover、未發 candidate version，也沒有證明 live Relay／GCE／disk-full／PID-1。Ubuntu composition 因缺 Cloud authentication 不開 publish door；外部 pairing 與接受證據仍屬 W5-2/W5-3/W6 |

`Orchestrator` ↔ `RemoteServer` 的依賴仍存在。盤點中的 basename crossings 是尋找呼叫路徑
的索引，並非 compiler dependency graph；檔案較小、import 較少或協定較多都不能證明 owner 已移動。
既有 `architecture-refactor.md` 與 architecture guard 仍掌管 extraction
與 ratchet，本 ADR 不重設其 ceiling、不授權跳過 W0 review 就開始 W1。

## 可重現的盤點契約

`工具` 只讀完整 Git commit/tree object ID 的 blob，
不讀 index／working overlay、不執行來源中的 Package.swift 或 shell。輸出包括完整 selected
file manifest、blob／content digest、line counts、imports、lock spellings 與 basename crossings。
它拒絕空或缺少任一 Swift partition、缺 build reference、非 regular source、不可讀／損壞 blob、
非 UTF-8、空檔與 NUL。stdout 在失敗時不會產生部分 inventory；stderr 是 typed JSON、exit 2。

| 證據種類 | 可說的事 | 不能推論 |
|---|---|---|
| Git／source bytes | named tree 有哪些檔案、其 mode/blob/digest、文字行數 | deployed build、編譯輸入完整性、runtime 支援 |
| lexical candidates | 哪些 import、basename token、lock spelling 出現在哪些行 | 符號解析、call graph、lock 是否持有、條件編譯生效；註解、字串及 inactive branch 可能被計入 |
| 人工 source reading | 上述 declaration、委派與 mutation seam 在固定版本的責任 | 未觀察路徑的正確性；本文件不是獨立 review receipt |
| compiler／focused runtime／exact-tree acceptance | 由相應命令、tree、環境、check count 與 receipt 限定其結論 | 不能由 lexical inventory 代發，也不能由 focused proof 升格成 full acceptance |

`generated baseline` 刻意固定於上述 tree；它不是
HEAD freshness badge。`--check <file>` 比較完整 bytes，樹、generator digest、格式任一變動皆會
拒絕舊輸出。JSON 是可解析的 evidence manifest；其中没有推測的 ownership 欄位、也沒有
手填 dependency override。機器報告不分析 web、history/churn、resources、外部 repo；build
references 只提供 bytes 身分，精確 compile artifact 的全輸入身分屬 W0-B。

## 後續依賴、驗證與回退

- W0-E 以 [ADR 0002](0002-cloud-authority-and-executor-trust.md) 的 authority 規則產出 contract candidate；須再等 W0-D。
- W0-F 依本方向與 W0-B 的工具證明 Ubuntu 24.04 amd64 非空 Core boundary；Foundation import 不等於 Linux compile receipt。
- W4-1 的 Ubuntu CI 編譯真實 `ClawdlineLinux -> ClawdlineApplication -> ClawdlineCore` product，
  再以 non-root uid/gid、真 tmux、Landlock/seccomp containment probes 執行
  create/send/observe/resize/enumerate/close；health 固定 `ready=false`，並區分
  `w4_runtime_not_configured` 與 `w4_provider_authentication_not_proven`，因此 compiled/configured
  adapter receipt 不會被升格成 provider、service 或 Cloud support。
- W0-C 負責 reliability 測量；它的數字未回來以前，Plan v4 的 latency／recovery 數字仍是待批准目標。
- CLA-296 root 收集獨立 review、合併 correction、exact candidate 驗證與 landing。W0-A 不建立 runtime、Cloud rollout 或新的 Board lifecycle。

聚焦驗證：`node Tests/platform-architecture-inventory.mjs`（TMPDIR 應指向該任務的私有 work
目錄）。新 guard 有空 inventory 紅燈證明及 mutation probes；沒有執行 Swift full suite。
回退只需移除本組新增 docs/tool/test，不會變更 Mac behavior、wire authority、schema 或部署。
