# Clawdline Cloud MVP 計劃書

> **狀態：可開始實作的最小計劃，2026-09-15。**
> 目標只有一個：MacBook 關機後，使用者仍可從 Cloud Board 把工作派給自己的 Linux，
> 並由 Linux 完成工作。本文不是已完成能力清單，也不把未來完整 replication 系統塞進 MVP。

## 1. 完成的畫面

Cloud 帳號配對一台 Mac、一台 Linux 與一個可信 viewer。Mac 完全關閉時，使用者仍能：

1. 在 `app.clawdline.com` 讀寫同一份 Board、Project 資訊與必要設定；
2. 看見 Mac／Linux 的能力、在線狀態與最後驗證時間；
3. 明確選 Linux，建立 Session、送訊息、派 Task；
4. 由 Linux root dispatch、review、landing，並以 GitHub commit SHA 交付程式碼；
5. 讓指定 Linux 的 Schedule 與既有 Schedule Webhook signal 執行一次且不重複。

同一 release candidate 必須證明本機個人版只使用一台本機，不需要 Clawdline 帳號，且不會
連到 Clawdline Cloud。Provider、GitHub 等工作本身需要的網路不在這個保證內。

## 2. 從零開始後的最小架構

```text
Hosted UI / Mac UI
        |
        v
Cloud control plane ─── durable inbox ───> selected Linux executor
        |                                      |
        | one canonical Board                  | checkout, worktree,
        | Project/config, Schedule             | Session, secret, process
        |                                      |
        +──────────── signed receipts <────────+
                                               |
                                               v
                                      GitHub commit SHA
```

只有三個權威：

- **Cloud 是協調權威**：保存一份 account-scoped Board、Project/config、Schedule、machine directory、
  durable inbox 與 execution receipt。各裝置是 client/cache，不是第二份可寫資料庫。
- **選定的 machine 是執行權威**：保存自己的 repo path、worktree、credential、Session、process、
  claim、effect ledger 與完整 log。Cloud 不能直接執行 shell，也不保存這些資料。
- **GitHub 是程式碼交付權威**：跨機器交付使用 remote 可取得的完整 commit SHA；Board 文字、
  branch 名稱或「已 push」不能代替它。

這個 MVP **不建立通用 replication framework**。它不做 CRDT、多主機 Board 合併、帳號資料主機、
machine-to-machine mesh、repo/worktree 複製、自動 failover、任意 offline command、完整 transcript
storage 或第二個 Cloud storage service。Machine 改派是新的、明確的人類操作；已接受的工作不會
自動搬到另一台機器。

### 2.1 一份資料，不代表所有資料都上 Cloud

| 資料 | 唯一權威 | 其他位置可以保存什麼 |
|---|---|---|
| 帳號、方案、machine directory | Cloud | 登入與 directory cache |
| Board、Project 名稱／repo、account／Project 設定 | Cloud | 有版本的 verified cache、未同步 draft |
| Project 在某台機器的本機 path | 該 machine | Cloud 只存 opaque Project id 與「已 mapping」狀態 |
| Schedule definition、target、enabled | Cloud | target machine 的已驗證 cache |
| 待執行工作與 bounded receipt | Cloud | target machine 的 inbox/effect ledger |
| credential、repo、worktree、process、完整 Session | 該 machine | Cloud 不複製；只存 bounded status/receipt |
| 程式碼交付 | GitHub commit SHA | machine checkout/cache |

Cloud mode 只有 Cloud writer；本機資料庫只是 cache/outbox。首次啟用會先 shadow compare、一次 import，
再停止舊 local writer，不能 dual-write。退出 Cloud mode 時則先停止 Cloud write、匯出最新 snapshot，
再啟用 local writer。

### 2.2 一個簡單的 request／job 模型

同步 API 只做驗證與 durable admission，成功寫入後立即回覆；任何 machine I/O、Git fetch、Session
建立或工作執行都在 admission 之後非同步進行。所有 mutation 使用：

```text
request_id + actor_id + normalized_body  // idempotency
object_id + expected_revision            // optimistic concurrency
target_machine_id + target_generation    // exact target and fencing
not_after                                // bounded offline lifetime
```

Cloud 使用現有主要資料庫中的明確 collection/table 與唯一索引，不為每種資料建立一個新 service。
Board、Schedule 與 inbox 可以有各自的 revision/queue，但共用一個 control-plane deployment。
Executor 拉取或接收通知後，先 durable 寫入本機 effect ledger，再執行；同一 idempotency key 不產生
第二個 effect。若 crash 發生在外部 effect 後、receipt 前，回 `effect_unknown`，不自動重跑並假裝
exactly-once。

產品 UI 至少區分：

```text
accepted -> executor_durable -> effect_started
         -> effect_completed | effect_failed | effect_unknown
```

`delivered` 只表示 transport，不表示 executor 已保存或完成。

## 3. 最小安全模型

Cloud 被信任維持服務與排序，但不被信任閱讀敏感內容或偽造使用者／machine 簽章；MVP 不宣稱能
偵測一個惡意 Cloud 對全新裝置隱藏舊 record。敏感 Board、Project/config、Schedule 與 command
payload 以 account data key 加密。Cloud 可見 metadata 僅限 routing、quota、freshness 與診斷所需欄位。

Linux enrollment 時，以 out-of-band QR／fingerprint pin account admin root。之後 human admin 可簽發
有期限、可撤銷、能力受限的 viewer grant；Cloud login 或 directory membership 本身不構成執行權。
每個 effect 都由 Linux 在執行當下驗證 signer、grant capability、target、generation、expiry 與 revoke
epoch。Hosting process 持有較強 key，也不能把其權限借給同 uid agent。

### 3.1 封閉的 principal × operation 表

| Principal | 可以做 | Admission verifier | Effect-time verifier | 撤銷後 |
|---|---|---|---|---|
| human admin viewer | enrollment、grant/revoke、data-key rotation | Cloud 驗簽與 admin chain | Linux 驗 pinned admin root 與 epoch | 新 mutation/effect 全拒；舊 data 依 key rotation policy |
| human writer viewer | 依 grant 寫 Board／Project/config | Cloud 驗 writer grant、scope、CAS | 無 effect；只形成 desired facts | 新寫入拒絕 |
| human `command_writer` | 對明確 target 建立 Session／dispatch/cancel | Cloud 驗 command grant、target、expiry | Linux 再驗完整 grant chain 與 capability | 未開始 command 拒絕；已開始回真實 outcome |
| human `schedule_writer` | 建立／修改／enable Linux Schedule | Cloud 驗 schedule grant、CAS | Linux firing 前再驗 definition digest、target/generation/epoch | 新 occurrence 不可執行 |
| bounded agent | 建立／更新 Board item、checklist、scope、progress、report；提交有 provenance 的 evidence | Board adapter 驗 agent grant 與白名單 | 沒有 effect capability | 新寫入拒絕 |
| executor daemon | claim 自己的 inbox、寫 capability/status/execution receipt | Cloud 驗 machine credential 與 target | 本機 effect ledger + 上述 caller grant | lease/credential 失效後不可接新工作 |
| Cloud control plane | durable admission、CAS、routing、retention | schema、quota、auth | **不能授權或執行 effect** | 不適用 |
| recovery credential | 解開 account data key | 本機 passkey/recovery flow | **不能簽 grant、Schedule、command 或 receipt** | rotation 後舊 credential 不得解新 epoch |

Board desired facts 永遠不是 command。Cloud sequencer、lease、occurrence claim、帳號密碼或 executor
key 都不能代替 human command/schedule grant。

### 3.2 A1 recovery 決定

2026-09-15 使用者選定 **A1**：Cloud 可以在帳號下保存 recovery capsule，但只保存密文。

- Migration 產生獨立 asymmetric recovery key；既有 `CLAWD1` 只解開舊資料，不能包裝新的 key
  epoch。Cloud 保存 recovery public key、由它包裝的 account data key，以及以 platform passkey PRF
  派生 key 加密的 recovery private-seed capsule；任何一項都不是 private seed 明文。
- 使用者登入後可以看到「有 recovery 可用」，但不能只靠帳號密碼讀出 recovery code 或解密資料。
  解鎖必須在本機使用另一個可同步的 platform passkey，加上 biometric/device unlock。平台不支援
  可攜 PRF 時，退回一次顯示、由使用者自行保存的 offline recovery code；不能改成用帳號密碼解鎖。
- Recovery principal 只有 decrypt 能力，不能新增 admin、修改設定、簽 Schedule／command 或改
  executor roster。Recovery passkey/code 可以 rotate；撤銷裝置後的新 data-key epoch 不再包給舊裝置。
- 若使用者同時遺失 recovery passkey/code 與所有已授權 viewer，帳號密碼本身不能恢復內容。這是
  E2EE 的必要代價，不建立 Clawdline 後門。

### 3.3 Schedule 與 Webhook 的唯一一次規則

Schedule 明文時間只在可信 viewer 與 target executor 解密。Target machine 本機算 occurrence，執行前
必須取得 Cloud 簽發、短效、符合最新 target generation 的 firing lease；lease 只證明 freshness，
不能取代 `schedule_writer` 授權。

Cloud 主資料庫用唯一索引保存 occurrence claim：recurring key 是
`HMAC(account_dedupe_key, schedule_id || occurrence_slot)`；one-shot key 固定為
`HMAC(account_dedupe_key, schedule_id || "once")`，所以改時間或改 target 也不能讓已用過的一次性
Schedule 再執行。Claim 只負責 account-wide 去重，不授權 effect；Linux 仍做 effect-time 驗證。
`created_at`、`when_changed_at` 由 Cloud acceptance receipt 指派，不接受 client 任意回填。

既有 D18 Webhook 只延伸 target machine/generation：維持 opaque token、empty／`{}` body、bounded
outbox 與既有 retention；不新增 provider payload 或 generic webhook framework。

## 4. Request latency 與事後診斷是基礎能力

Cloud MVP 不允許 status、transcript 或單一慢 machine 阻塞整個 UI：

- boot 只抓 bounded Board/machine projection；transcript、screen、history 依需求且分頁載入；
- status freshness 使用小型 heartbeat，不因碼錶文字或 `observed_at` 更新而重發整份 Session；
- response 使用 revision/signature/ETag；同 signature 不重抓、不重畫；背景頁停止 polling；
- 每個 queue 有容量、deadline、backpressure 與 typed overload；不同 machine、account 與 request class
  有 failure isolation；不在 HTTP request 內等待 relay 或 executor；
- retry 必須 exponential backoff + jitter，所有 mutation 有 idempotency key，SSE 用 cursor resume；
- payload 有明確 byte/item/page 上限，查詢有對應 index，慢 query 不准無界掃描。

每筆 request／job 使用同一 `trace_id` 串起下列 structured log 與 metric；值不得包含 secret、明文
Project 名稱、Board 內容、Schedule template、transcript 或 webhook token：

```text
request_kind, account_hash, machine_id, trace_id, request_id,
accepted_at, queue_wait_ms, handler_ms, db_ms, relay_wait_ms,
payload_in_bytes, payload_out_bytes, item_count, cache_result,
retry_count, state, typed_error, executor_durable_at, effect_finished_at
```

Dashboard 至少顯示各 request kind 的 rate、p50/p95/p99、queue depth/reject、DB latency、payload bytes、
retry、SSE resume 與 `accepted -> executor_durable -> effect` latency。Failure injection 必須涵蓋慢 DB、
滿 queue、離線／half-open machine、relay restart、duplicate mutation、stale cursor 與超大 transcript。
事故後應能只靠 `trace_id` 回答時間花在 browser、Cloud queue、DB、relay、executor 或 effect 哪一段。

## 5. 四個 Milestone

```text
M0 contract + observability
  -> M1 Cloud Board/Project + machine presence
       -> M2 Linux durable dispatch
            -> M3 Schedule/Webhook + integrated rollout
```

每個 Milestone 都要有一個 integration root、代表性 red proof、獨立 review、exact-tree verification、
commit 與 landing receipt；文件或 child success 不等於 released。

### M0 — 鎖定 contract 與量測基線

**做什麼**：

- 更新 `DECISIONS.md`／`PROTOCOL.md`：Cloud coordination、account grants、A1 recovery、Schedule
  authority 與 D18 target extension；來源是 conversation
  `01a09d1a-4faf-7d43-94fa-b33b5e66642b` 的訊息
  `msg_01a0a37b-2115-7422-82e8-e9616f457003`（2026-09-15T05:12:30.741Z）；
- 建立最小 schema、CAS/idempotency/target-generation unique indexes、principal capability catalog、
  typed errors 與 migration/rollback fixture；
- 先補第 4 節 trace/log/metric 與 dashboard，量出 Board boot、status、transcript、dispatch 的基線；
- staging durable content write 維持關閉，只跑 schema、crypto、authorization 與 failure-injection tests。

**退出條件**：Cloud login 不能偽造 writer；agent 不能產生 effect；revoked/wrong target/stale revision/
duplicate body 被 typed refuse；每種慢 request 可由 trace 定位；contract 與 red vectors reviewed/landed。

### M1 — 單一 Cloud Board／Project 與 machine presence

**做什麼**：用現有 Cloud API/deployment 與主要資料庫落實 account/Project/config、Board、machine
directory/presence；使用一個 Cloud adapter、一次 import、verified cache 與 rollback export。Status API
只回 compact projection；Session/transcript 保持 machine-owned on-demand read。

**退出條件**：Mac 關機時 viewer 可讀寫 Board/Project/config、看見 Linux freshness；兩個 stale CAS
writer 不會互蓋；慢/offline Mac 不阻塞 Linux 或 Board；本機個人版對 Cloud host request 為零。

### M2 — Linux durable dispatch

**做什麼**：補齊 Linux root/broker 能力；使用同一 Cloud durable inbox 保存 exact-target command，
Linux 以 local effect ledger 去重並回 signed receipt。整合既有 Linux Session read/send，不複製
transcript。Git task admission 驗 remote 可取得的 base SHA，landing 回完整 commit SHA/ancestry receipt。

**退出條件**：Mac 關機後可選 Linux，建立 Session、派 child、review、landing；duplicate command
不開第二個 Session/effect；offline、capability missing、workspace busy、base SHA 不可達都在 deadline
內 typed refuse。Cloud 接受 command 後不等待 Linux 才回 HTTP。

### M3 — Schedule／Webhook 與整合 rollout

**做什麼**：Cloud 保存 encrypted Schedule definition；Linux 本機算時間，以 fresh lease、最新 target
generation、effect-time grant 與唯一 occurrence claim 執行。既有 local Schedule 先 shadow compare、
一次 import、停止舊 scheduler 後再切換。D18 只增加 Linux target/generation。

**退出條件**：Mac 關機時 Schedule 與 D18 signal 各執行一次；restart、重送、改派、one-shot 改時間、
舊 target 從 cache 恢復都不 duplicate；crash ambiguity 顯示 `effect_unknown`。最後用同一 exact release
candidate 驗證 Cloud flow、本機個人版零 Cloud traffic、Mac app、Linux artifact、hosted console 與
Cloud backend build stamp，再按 allowlist rollout。

## 6. Rollout 前的四個 policy 開關

它們不改變上面的架構，也不阻擋 M0/M1 在 staging 實作。MVP 先用下列保守預設；產品 owner 可在
production allowlist 開啟前修改：

| Policy | MVP 預設 |
|---|---|
| opt-in/out | **明確 opt-in**；本機個人版與既有帳號不自動上傳 |
| retention/delete | Board/Project/Schedule 留到使用者刪除；terminal command payload 7 天、receipt 30 天；刪除後 30 天內完成 backup expiry/crypto-shred |
| metadata disclosure | 公開列出 Cloud 看得到 routing id、時間、大小、狀態、能力與粗粒度 request kind；看不到內容明文 |
| offline command | 只允許 M2 dispatch、M3 Schedule 與 D18；預設 24 小時到期，不包含一般 send/answer/語音 command |

任何擴張若不是第 1 節必要條件，先留在後續 backlog；不能以「既然已有 queue／encryption」為由
把通用同步、任意 payload、auto-routing 或高可用架構順手帶進 MVP。
