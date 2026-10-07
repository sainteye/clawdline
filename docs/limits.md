<!-- clawdline-doc: kind=spec audience=agent -->
# Capacity and retention rules

For maintainers, this page collects the capacity rules and the N inventory rows cited by code.
The [dated 2026-09-18 inventory](records/limits-2026-09-18.md) preserves the full measurements,
retired-app comparison, analysis, and uncited rows. The implementation register is
`internal/domain/capacity`; read it for the enforced current values.

## 4. 新版的統一規則

### 4.1 規則零：每一個有界的東西都要登記；沒登記的上限就是缺陷

新版把 O26 的形狀推廣到所有東西：`internal/domain/capacity` 一張登記表（刻意不叫 `limits`，那個名字已經是方案額度的讀取器，
`internal/adapters/limits`）。每一列：

| 欄位 | 意思 |
|---|---|
| `name` | 穩定的鍵：`audit.security`、`log.daemon`、`store.db`、`broker.task_text`、`board.receipts`、`sse.subscribers`、`cloud.relay_queue`… |
| `class` | 資料類別（§4.2），**決定到頂時准許的行為** |
| `unit`／`limit` | 筆數、位元組或天數，至少一個；**可注入，而且覆寫只准調小**（§4.7） |
| `warn_at` | 預設 80%；可以逐列覆寫（例如看板檔案在 8 MiB 就該說，見[歷史分析 §5](records/limits-2026-09-18.md)） |
| `at_limit` | `refuse`／`evict_oldest`／`expire`／`rotate`／`summarize`／`coalesce`／`disconnect`，必須是該類別准許的 |
| `measure()` | 回傳 `used`、`oldest_at`；**要便宜**（`stat`、有索引的 `COUNT`，不讀整個檔——`recentAudit` 讀 5.7 MB 取 200 行就是反例）；**量不到回 `unknown`，不回 0**（broker-design F5） |
| 計數器 | `refused`、`evicted`、`expired`、`rotated`、`dropped`、`write_errors`、`last_action_at`；行程內累計，重啟歸零並附 `counting_since` |
| `owner` | 誰會被告知（§4.5） |

登記表有兩道守衛：一個檢查每列的類別、量測與上限；另一個掃描有界常數和 channel，要求對應登記。
詳細的首次盤點與反例見[日期紀錄](records/limits-2026-09-18.md)。

背景工作樹新增兩列。`sessions.agent_rows` 是每個 session 最多帶到畫面的 provider agent 數，限制 6，較舊列留在 provider 的原始紀錄並以
`agents_reading.truncated` 明說省略數；`cache.background_agents` 有三張可重建的 LRU：immutable metadata（Claude sidecar 與 Codex rollout head 共用）、
transcript tail、完成通知 cursor，每張最多 256。穩定的一次掃描仍會 `stat` 近期 Claude agent 以判斷是否有變，但快取命中不再開 transcript；Codex 則用已知的 thread id
命中快取，不在每次 beat 重走 rollout 目錄。讀不到來源時讀數是 `unknown`，不把它畫成 0。

`places.registered` 是 `CLAWDLINE_NEXT_DIR/places.json` 中由人明確保留的 Project 目錄，限制 512 筆。
它是人的選擇（`evidence`），所以滿了拒絕新增，不自動淘汰；只有 `clawdline project remove` 會移除。
diagnostics 每次直接數這個最多 512 列的檔案，壞掉或讀不到回 `unknown`，不把既有 Project 說成零個。

`places.git_config_bytes` 是讀一個 Project 的 git config 來回答 `repo`（`GET /v1/places`）時最多讀的位元組，64 KiB。
超過就不解析，這個 place 的 `repo` 是 `""`（等同沒有 origin），不拿截斷的前半段猜；它不累積，所以沒有淘汰，只在 place 讀取時量。

Work v2 的看板項目與直接 Session 待辦參考圖片是使用者輸入，在其主體存續期間不可自動淘汰，因此以 evidence 列登記並在滿載時拒絕：
`work.images_per_item` 6 張、`work.image_bytes` 每張正規化 PNG 5 MiB、
`work.image_bytes_per_item` 15 MiB、`work.image_bytes_total` 512 MiB；另有 buffer 列
`work.image_request_body_bytes` 18 MiB（保留加密 Cloud envelope 的膨脹空間），同時約束本機 HTTP 與 Cloud 子文件。Diagnostics 分別量
最滿主體（項目或直接待辦）的張數／位元組、最大單張及全庫位元組；不會為新圖片刪除既有參考資料。
Reference-image bytes are files under `reference-images/`; the rows keep metadata, sha256 and byte counts (N48).

`work.documents_per_item` measures the fullest item that can still receive
documents. Its diagnostic note identifies writable items at the 32-document
limit and closed items that retain 32 documents. Closed items do not make the
machine's health `capacity_exhausted`: their documents remain evidence, but
no new document can be added to a closed item. A writable item at the limit
still refuses a new ordinary document with `documents_full`; its one
`completion_report` has a separate slot. Revise an existing document when
updating the same material, or split genuinely distinct work into another
item. Neither action silently removes retained evidence.

### 4.2 資料分類：什麼絕不能丟、什麼可以摘要後丟、什麼可以直接丟

| 類別 | 例子 | 可以丟嗎 | 到頂時 |
|---|---|---|---|
| **證據** `evidence` | landing 紀錄與它的更正（`corrected_from`）；交付、驗證收據；review 的裁決、axes 與 finding metadata；closure attestation；task 紀錄本體（L2）；未結清的 obligation；`result.json` 的結構化欄位 | **絕不** | **拒絕新寫入**，並立刻 health `ok:false`。上限不用筆數，**用位元組與磁碟空間**，設在正常使用下碰不到的地方，所以到頂＝真的出事了 |
| **安全稽核** `security_audit` | 配對、裝置、密碼、權限；遠端寫入（`session.send`、`key`、`interrupt`、`close`、shell、上傳） | **絕不**（可以輪替成分段檔，分段不刪） | 輪替；磁碟吃緊時照證據處理 |
| **冪等收據** `idempotency` | 看板 `(actor, requestId)`、`Idempotency-Key`、Cloud command ledger、送出去重 | 過了重試視窗可以 | **按時間過期**；視窗內滿了就**拒絕新指令**（429＋`Retry-After`），不淘汰視窗內的；視窗外的重送回具型別的 `receipt_expired`，**不重新執行**。要分得出「過期的重送」與「新指令」，request id 必須帶時間（UUIDv7 或附 `issued_at`）；做不到的，過期時只留 key＋digest 的精簡墓碑（每筆幾十位元組） |
| **使用者輸入** `user_input` | 上傳的圖、drops | 不再被引用時可以 | 位元組預算；被引用的先告警再淘汰最舊的，淘汰要計數、要讓讀的人看到原因 |
| **長文** `narrative` | child summary、review 的散文、graph、plan、durable report | 可以**摘要後**移走 | 90 天後在 DB 留摘要（前 280 字＋sha256＋位元組數），原文移到冷層（§4.3），**不直接刪** |
| **進度** `progress` | progress notes、session activity | 可以摘要後丟 | 視窗＋`droppedCount`（照舊版的 5 則） |
| **觀察** `observation` | executor `observed_at`、SSE 影格、git 歷史匯入、看板機器對帳 | 可以，**可重算** | 最好不落盤（broker-design §6.2）；落盤的淘汰最舊，但**被引用的釘住** |
| **營運紀錄** `journal` | `orchestrator.*` 生命週期、`worktree.kept`、`completion.attempt` | 可以 | 只記**狀態轉換**，不記重複；broker 的生命週期本來就在 store 的 `events` 裡，不再重寫一份到 audit |
| **工作區** `work` | task 目錄、worktree、build 產物 | 擁有者確認不在、內容已保存後 | 時間＋擁有者檢查，**未知就保留**（F3、F5）；保留的數量與位元組進 diagnostics |
| **快取** `cache` | transcript titles、project readings | 可以 | LRU |
| **診斷 log** `diagnostic_log` | daemon log | 可以 | 按大小輪替（10 MiB × 5），0600 |
| **即時緩衝** `buffer` | SSE 每訂閱者緩衝、Cloud 入站佇列、終端機 lane | **丟了就要讓對方知道** | 最新值 channel `coalesce`；指令佇列 `refuse`（告訴送的人）；慢消費者 `disconnect` 後重拿快照（照 O26）。**絕不默默丟中間的一段** |

**稽核要拆成安全事件與營運事件。**安全事件不刪；營運事件按狀態轉換留存。
量測與理由見[日期紀錄](records/limits-2026-09-18.md)。

### 4.3 分層：熱／溫／冷——要，但層由類別決定，不由年齡決定

| 層 | 放什麼 | 在哪 | 怎麼讀 | 界線 |
|---|---|---|---|---|
| **熱** | 活著的 task、未結清的 obligation、視窗內的冪等收據、最近的觀察 | SQLite 主表＋記憶體 | 熱路徑直接讀 | 工作集大小；分頁 |
| **溫** | 已結案的 task 紀錄（L2）、90 天內的長文（L3）、看板已結案的卡與每項歷史、安全稽核的當前分段 | 同一個 SQLite，但**熱路徑不碰**（第一步就要讓 N1 的「每 2 秒解碼全表」只碰熱的） | 分頁、按需 | DB 位元組預算（預設 1 GiB 告警）＋磁碟剩餘 |
| **冷** | 長文原文（摘要之後）、稽核的舊分段、舊 app 凍結下來的檔 | DB 之外、壓縮、append-only 的分段檔，`CLAWDLINE_NEXT_DIR/archive/` | 只有明確的工具（CLI、設定頁的「匯出」）會讀 | **只有人能刪**，而刪除本身寫進安全稽核 |

### 4.4 到頂時的行為，以及空間吃緊時的讓位順序

§4.2 的最後一欄就是到頂行為。空間（磁碟或 DB 預算）吃緊時，依序讓位，**每一步都記一筆事件、進 diagnostics**：

1. 丟快取
2. 輪替並刪除超出預算的**診斷 log** 分段
3. 提前把溫層長文摘要進冷層
4. 拒絕**可選**的寫入：git 歷史匯入、觀察、營運紀錄
5. 拒絕新派工與新指令：具型別的 `507 storage_exhausted`，health `ok:false`
6. **永遠不做**：淘汰證據、安全稽核、視窗內的冪等收據

### 4.5 誰會被告知

| 管道 | 什麼時候 | 內容 |
|---|---|---|
| `/v1/diagnostics.capacity`（本機 token） | 永遠 | 每一列的完整讀數（下面的形狀） |
| `/v1/health`（公開） | **只在證據或安全稽核類到頂，或讓位走到第 5 步時** | `ok:false`＋`reason:"capacity_exhausted"`；**名字與數字不上公開的 health**，只在 diagnostics。warn／critical 不翻 `ok`，避免「一直是紅的」 |
| 推播（既有的 `internal/adapters/push`） | 進入 `critical` 或 `full` 時；回到正常時一則「已恢復」 | 一則一句話：什麼、多滿、哪天會到、要做什麼。每列 24 小時最多一則；**預算與 agent 的 30／小時分開**，不跟 agent 搶 |
| 畫面 | 任一列不是 `ok` 時 | 設定頁的「容量」區塊（`CapacityBlock`，Go 自己的區塊，不在 1:1 範圍內；經 Clawdline Cloud 的 `capacity` 讀取，手機也看得到）。每則容量推播都指向它。主畫面橫幅見[歷史分析 §8](records/limits-2026-09-18.md) 第 3 項 |
| store 的 `events` | 每次狀態轉換、每一批淘汰／過期／輪替 | `capacity.state{name, from, to}`、`capacity.evicted{name, count, oldest_at}`；**一批一筆，不是一項一筆**（O23 的反例） |
| `clawdline doctor` | 手動 | 印整張表，給沒有瀏覽器的 Linux |

The dated [diagnostics payload example](records/limits-2026-09-18.md) is kept with the source inventory.

### 4.6 水位與預測

- `ok` < 80% ≤ `warn` < 95% ≤ `critical` < 100% ≤ `full`；**另外有 `unknown`**（量不到），它不是 `ok`。
- **遲滯**：往下要比門檻低 5 個百分點才回去，不然在 95% 附近會一直通知。
- **預測**：用最近 7 天的成長算 `projected_full_at`。**距離到頂不到 14 天，就算比例還低也升到 `warn`**——撞牆前兩週說，
  而不是撞牆那一刻說。[§2.4](records/limits-2026-09-18.md) 那張表就是這條規則今天會產出的東西。
- 量測用一個自己的 beat（60 秒，跟 scheduler 同一個節奏），而且**這個 beat 本身也要可觀察**（`plan.md` §3.2 的規則：
  「巡過但沒事」和「巡邏停了」在外面看起來都是安靜）。

### 4.7 怎麼證明它會叫

**要能故意把某個東西塞滿，然後看到警報。**分四層，前兩層每次都跑，後兩層驗收時跑一次：

1. **domain 表格測試（純函式）**：每個類別注入 `limit = 20`，依序寫到第 16（80%）、19（95%）、20（100%）、21 筆，斷言：
   狀態 `ok → warn → critical → full`、第 21 筆觸發該類別的到頂行為（拒絕／過期／輪替／覆蓋）且計數器加一、
   **每次往上跨越只產出一個通知意圖**、在第 18／19 筆之間來回不會再通知（遲滯）。
2. **登記表守衛**：§4.1 的兩道；第二道要附一個會紅的樣本（未登記的常數），證明它不是擺設。
3. **`clawdline doctor capacity --drill <name>`**：只在一個**拋棄式的** `CLAWDLINE_NEXT_DIR` 上執行（偵測到它是使用者正在用的那個
   目錄、或有 daemon 在用它，就拒絕），**走真的寫入路徑**把指定的東西寫到上限＋1，然後印出 diagnostics 那一列與產出的通知意圖。
   只有在「狀態真的走到 `full`、到頂行為真的發生、每次跨越正好一則通知」時才 exit 0。這一層不需要瀏覽器，CI 可以跑。
4. **端到端一次**：在拋棄式 port 上起一個 daemon，以 `CLAWDLINE_NEXT_CAPACITY=audit.security=4KiB,store.db=<目前大小＋32KiB>`
   縮小上限，做幾次會寫稽核的動作（例如 `clawdline open --print` 發一個裝置、再登出；兩者是否真的寫稽核，驗收時先確認），
   看 `/v1/diagnostics` 出現 `rotated ≥ 1`；再寫到 `store.db` 超過上限，看 `/v1/health` 變 `ok:false, reason:"capacity_exhausted"`，
   並看到一筆 `capacity.notify` 事件（拋棄式 daemon 沒有推播訂閱，所以驗的是通知意圖，不是手機）。

**覆寫只准調小**，而且覆寫中的列在 diagnostics 標 `overridden: true`、log 記一行——防止有人（包括我們自己）把上限調大來讓警報閉嘴。

The test-isolation rule (§4.8) remains in the [dated source record](records/limits-2026-09-18.md).

## Code-cited inventory rows

These rows retain their original N identifiers and dated wording. Verify a current value against
`internal/domain/capacity` before changing behavior. The [complete inventory](records/limits-2026-09-18.md)
contains every other row and its measurement context.

| # | Thing | Limit (file:line) | At limit | Who knows | Lost? | Class |
|---|---|---|---|---|---|---|
| N1 | SQLite 全部資料表（`events`、`receipts`、`tasks`、`obligations`、`board_commands`、`broker_*`） | **無**；全 repo 沒有一句 `DELETE FROM`；`events` 的註解明寫「never edited and never deleted」（`store/sqlite.go:33-35`） | 只增不減；broker 每 5 秒、每個 SSE 訂閱者每 2 秒都會讀出並解碼整張 `broker_tasks`（`orchestrator/broker.go:161-176`），成本跟著全部歷史長 | 只有 CLI `clawdline doctor` 印筆數（`cmd/clawdline/main.go:162-168`） | — | c |
| N2 | 事件寫入失敗 | — | `Store.Append` 不記 log（`sqlite.go:238-243`），呼叫端一律 `_ =` 丟掉；`app/actions.go:414-416` 的註解說「logged by the store」，**不是真的** | 沒人 | **是**（磁碟滿就是它） | b |
| N5 | progress notes | 300 字；讀最新 5（`broker.go:100-107`） | HTTP 400；但**從 `progress.json` 來的超長筆記直接丟、不記 log**（`watch.go:103-105`） | 檔案那條沒人 | 部分 | b |
| N6 | notify | 5／task、30／小時（`broker.go:103-109`） | 429 | 呼叫端 | 否 | ✓ |
| N7 | 完成通知重送 | 8 次（`record.go:151-175`）. Since 2026-09-25 the holds have ceilings of their own: a busy lane or an occupied composer spends an attempt after **10 minutes** (`maxNoticeHold`), while a root showing something waiting to be answered holds for free for up to **12 hours** (`maxChoosingHold`); a dead letter at most **24 hours** old is typed once more when its root next reads idle (`maxRetypeAge`). All three are `parameter` lines on the capacity baseline, not rows: nothing accumulates | 轉 dead letter，寫 event `task.completion.dead_letter`; at the menu ceiling in one step, with `root_choosing` and the ceiling in its `last_error`. The idle re-type is one recorded attempt past the limit (`task.completion.retyped`), never a second push | **console 沒畫、health 沒有**；`KindDeadLetter` obligation 有定義（`domain/task/obligation.go:23`）但從未建立. A dead letter is pushed once; every unacknowledged notice — pending or dead — is on `GET /v1/orchestrator/completions` and on its root's own `GET /v1/work/v2/agent/session-todos/<conversation>` (`unacknowledged_completions`) until it is acknowledged | 是 | ✓? |
| N8 | summary／title／kind／label | 2,000／60／40／120（`taskdir/finish.go`、`domain/work/proposals.go`、`orchestrator/draft.go`） | summary／kind／label 仍依各入口處理；title 超長或符合已量到的壞形狀會帶教學訊息拒絕，不再截斷 | 寫入者 | 否 | ✓ |
| N9 | `result.json` | **無大小上限**（`taskdir/broker.go:162-176` 直接 `os.ReadFile`） | — | — | — | c |
| N10 | task 目錄 `<Dir>/tasks/<id>` | **無清掃**（`taskdir.go:21`） | 只增不減 | — | — | c |
| N11 | worktree `<Dir>/worktrees/…` | **無**；`RemoveWorktree`（`git/worktree.go:56-60`）沒有任何呼叫端 | 只增不減 | — | — | c |
| N12 | `remote-audit.jsonl` | **無輪替**；`O_APPEND`＋0600（比舊版好，`devices/files.go:513-543`）；欄位值 256 B **靜默截斷**（`:75-77`） | 只增不減；寫失敗只記 log | log | 部分 | c |
| N13 | 裝置清單 `remote.json` | 裝置數**無上限**，每次密碼登入新增一台（`auth/authority.go:718`）；讀取上限 4 MiB（`devices/files.go:70`） | **超過 4 MiB 時 auth 在啟動時整個失敗**（`gate.go:120-124`） | log | 服務中斷 | c（滿了是全面停擺） |
| N14 | push 訂閱 | 無數量上限（`push/store.go:225-237`）；檔案讀取上限 1 MiB | 超過 1 MiB → `ErrUnreadable`，**所有推播都失敗** | log；notify 回 502 | — | c |
| N15 | 圖片 | 照搬舊版 policy（`artifacts/artifact.go:59-70`）. Since 2026-09-26 every picture the daemon stores — drops, this store, and Board and to-do reference images — has a long edge of at most 1,600 px (`artifacts.MaxLongEdge`, the console's `LONG_EDGE`); a larger one is scaled down, not refused. A photograph is stored as JPEG (quality 90) and anything else as 8-bit PNG: a 146 KB phone JPEG had been a 1.19 MB 16-bit PNG | 淘汰最舊，**不記 log**（`store.go:365-372`）；讀者拿到 410 `artifact_expired` | 讀的人事後才知道 | 是 | b |
| N16 | Drops | 兩個登記列：`artifacts.drops`（**256 MiB**，量位元組；`MaxDropsBytes`，另有來源 `DropsAgeLimit`）與 `artifacts.drops_young`（**1 張**：過去 24 小時內被位元組上限刪掉、當時還不到 24 小時的圖；`DropsYoungLimit`）。保留規則：超過 **7 天**的檔案刪掉，但**最新 40 張**（`DropsKeep`，是地板不是上限，所以不是登記列）不因年齡被刪；目錄超過 256 MiB 時不論年齡從最舊的刪起，只有剛寫入的那張不刪。2026-09-26 起不再「只留最新 40 張」：舊規則在每天都傳圖的人身上天天 40／40，每次都推「容量已滿」卻沒有任何事可做，而且一個下午傳 40 張就會刪掉幾分鐘前才打進 prompt 的檔案（`drops.go`） | 過期與超量都刪最舊的，有計數（expired／evicted）與 log；每次量測（capacity beat）也會先清掉過期的，一週沒人傳圖的快取不會一直讀成滿 | `artifacts.drops`：diagnostics、log，**不推播**——它滿代表位元組上限正在刪檔，這是快取在運作。`artifacts.drops_young`：diagnostics、log、**notice（推播）**，句子說哪些圖被提早刪掉、assistant 回頭讀會讀不到、需要時重新貼；過了一天沒有再發生只記下、不推「已恢復」 | 是 | ✓ |
| N17 | transcript | 8 MiB 尾端、150 KiB 回應（`transcript/turns.go:36-39`、`http/transcript.go`） | 同舊版；React `Transcript.tsx` 不讀 `truncation` | 沒人 | 否 | b |
| N18 | 記憶體快取（transcript titles） | 256 筆 LRU（`transcript/claude.go`、`transcript/lru.go`） | 淘汰最久未使用的讀數並計數 | diagnostics、notice | 是 | ✓ |
| N19 | SSE（自己供應時） | 訂閱者數**無上限**；screen bus 每訂閱 chan 16，**滿了走 `default` 直接丟、沒有計數**（`http/screen.go:189-197`）；沒有 Last-Event-ID | 丟 | 沒人 | 下次變動才補回 | b（訂閱者無上限那一半是 c） |
| N20 | Cloud 入站佇列 | 64（`cloud/relay.go:42`） | **丟最舊的請求**，記 log | `/v1/cloud/status.queue_dropped`；UI 有型別（`cloud.ts:77`）**但沒畫** | 是 | b |
| N21 | Cloud replay window | 照搬（`cloud/replay.go:11-17`） | 拒絕新 sender | `inbound_dropped{reason}`，設定頁有警示點（`SettingsWindow.tsx:632-640`） | 是 | a |
| N22 | Cloud spool | 五個登記列：`cloud.spool`（2,000）、`cloud.spool_bytes`（16 MiB）、`cloud.spool_channel_bytes`（4 MiB，單一 channel）、`cloud.spool_receipts`（4,096 筆墓碑）、`cloud.spool_refusals`（64 句，單一 channel，每句至多 4 KiB）。Since 2026-09-25 the machine-read reply channel also keeps a 1 MiB reserve for answers of at most 256 KiB (`largeAnswerByteLimit`, `smallAnswerReserveLimit`, on the admission baseline), and reference-image thumbnails are `cache.image_thumbs` (8 MiB, oldest let go) with a 480 px long edge (`artifacts.MaxThumbnailEdge`) | 拒絕；**而且會回一句具名的拒絕**給等在那條 channel 上的人（`cloud_read_busy` / `command_answer_undeliverable`，cloud-wire 9.5.1）。Every refused read gets its own refusal up to `cloud.spool_refusals`; past it the drop is logged with `operation=`. A large answer that would eat the reserve is refused `cloud_read_busy` (cloud-wire 9.5.2) | log、diagnostics、notice、**等的人本人** | 是 | ✓ |
| N23 | Cloud command ledger | 照搬（`domain/cloud/ledger.go:49-66`） | **寫好了但沒接上**（`NewLedger` 沒有正式呼叫端）；註解承認重啟後會重複執行（`:19-23`） | — | — | （未接線） |
| N24 | 看板收據 | 4,096（`board/board.go:56`） | FIFO；淘汰計數只存在檔案裡（`settings.go:205-208`），不上 wire | 沒人 | 是 | b |
| N25 | 看板卡片／專案 | `MaximumItems = 2_000`、`MaximumProjects = 200`（`board/board.go:53-54`） | **宣告了，整個 codebase 沒有任何引用**——跟舊時間軸同一個數字，這次是名義上的上限 | — | — | c |
| N27 | request body | 多數路由有；**`dispatch`、`orchestrator` 八條、`schedules`、`coordinator`、`actions` 沒有**（`server.go:434-438` 只設了 `ReadHeaderTimeout`）；auth body 超過 64 KiB **被截斷後當成 `{}`**（`auth.go:66-78`） | — | — | — | c（auth 截斷那一半是 b） |
| N28 | 文件列表 | 200 列、走訪 4,000（`documents.go:366-394`） | **靜默截斷，沒有旗標** | 沒人 | 否 | b |
| N29 | daemon log | Go 標準 `log` 寫 stderr，全 repo 沒有 `SetOutput`；打包的殼沒有轉向輸出（`shell/darwin/main.swift:300-322`） | 目前由啟動者導到 `/tmp/clawdline-next.log`（491 B，實測 `lsof`）；打包後去哪裡未驗證 | — | 可能遺失 | c |
| N42 | Token ledger reading (`transcript.LedgerState.Feed`, docs/token-ledger.md) | **64 MiB** per pass and **8 MiB** per line: `ledgerFeedLimit` and `ledgerLineLimit` in `transcript/ledger.go`, `read` lines on the capacity baseline, not rows, because a pass holds one line at a time and keeps only totals | A pass stops at the first line boundary past 64 MiB and says there is more; the next pass starts from the stored offset. A line past 8 MiB is skipped, 8 MiB at a time and across passes if need be, and counted (`overlong`); a line that does not decode is counted (`undecodable`); neither stops the pass. A file that shrank below the offset or whose first 4 KiB changed is read again from zero (`restarts`) | The counts in the session's ledger state | No: a skipped line's tokens are still in the next call's measured context; only which category they belong to is lost | ✓ |
| N43 | Token ledger passes (`app.UsageLedger.Pass`, docs/token-ledger.md) | **32 transcripts** fed per pass, a pass a minute, and a look-back window of **7 days**: `usagePassLimit`, `usageWindowLimit` and `usageEvery` in `app/usage.go`; `parameter` lines on the capacity baseline, not rows, because a pass holds one transcript at a time and the table keeps one row of totals per transcript | The rest of the due transcripts wait for the next pass, which starts after the last one fed, so one that stays due (unreadable, or longer than one Feed) cannot starve the others. A transcript last written before the window keeps its stored totals and is not visited until it is written again. One that cannot be read is skipped for that pass (`transcript_unreadable`), one that is gone keeps its totals (`transcript_missing`); neither stops the pass | The daemon log: once when a pass meets the limit, until a pass no longer does, and once per transcript and reason, by conversation id; the reason is on the transcript's row | No: a transcript past the limit only waits; one outside the window keeps what was read | ✓ |
| N44 | Compaction comparison (`app.UsageLedger.CompareCompaction`, `GET /v1/usage/compare-compaction`, docs/token-ledger.md "Did compacting early pay") | **500 child tasks** per answer, newest first; **50 excluded tasks** named; a group needs **5 tasks** before a share or rate is shown; a `since` of at most **3650 days**: `compareTaskLimit`, `compareExcludedLimit`, `compareTooFewLimit` and `compareSinceDaysLimit` in `app/usage_compare.go`; `page`, `parameter` and `admission` lines on the capacity baseline, not rows, because the answer is computed on each ask and nothing is kept | Past 500 the answer covers the newest and says `truncated`; past 50 the excluded count stays whole and `excluded_truncated` says the names are not all there; a group under 5 tasks says `too_few` and shows its counts with every percentage null; a longer `since` is a 400 `bad_request` | The answer itself; `clawdline usage --compare-compaction` prints each flag as a line | No: nothing is stored, and a narrower `since` reads the rest | ✓ |
| N45 | Things waiting to be verified (`app.Verifications`, `/v1/verifications`, docs/verifications.md) | **200 records** open and closed together; **12 criteria** and **200 notes** a record; a title of **200** characters, a why of **4000**, a criterion of **500**, a note of **8000**, a closing reason of **2000**, a session name of **200**: `verificationTotalLimit` … `verificationAuthorLimit` in `app/verify.go`, and a Cloud sub-document of **64 KiB** (`verificationCloudBodyLimit`); `admission` lines on the capacity baseline, not rows, because each is refused at the door and nothing is kept past it | The 201st record is a 409 `verification_limit_reached`, the 201st note a 409 `notes_limit_reached`, anything longer a 400 `bad_request` naming the field; nothing is evicted — every record is something a person asked to be reminded of, and only a person deletes one | The refusal, in the answer to whoever asked | No: deleting a closed record makes room | ✓ |
| N48 | Board and to-do reference-image files (`store.ReferenceImagesDir`, `<state>/reference-images/<id>.png\|.jpg`) | The bytes are held by `work.image_bytes_total` (512 MiB, counted as the rows' `byte_count`, each proved against its file's sha256 on read). Three parameters on the baseline: **32 MiB** of pictures per step of the move out of SQLite (`ReferenceImageBatchLimit`), a file no row names is removed once **1 hour** old (`ReferenceImageOrphanAgeLimit`), swept at every Open and every **6 hours** (`ReferenceImageSweepIntervalLimit`) | A file the sweep removes is one no row names: its picture was deleted, or its row never committed; the count is logged and kept (`Store.ReferenceImageFileStats`). A row whose file is gone or differs is refused 410 `image_file_missing` / `image_file_mismatch`, logged, never served empty | The reader of the picture gets the named refusal; the daemon log carries each refusal and each sweep that removed something | No: only unnamed files are removed | ✓ |
| N49 | Sessions a reboot took away (`app.SessionRestore`, `store.RecordBoot`, `/v1/sessions/restorable`, docs/session-restore.md) | **200 conversations** recorded per boot, **2 boots** kept (this one and the one before), **20 conversations** per restore, a recorded `last_seen` at most **300 seconds** stale while the open set is unchanged, the boot's own `last_seen` heartbeat at most **60 seconds** stale while complete readings arrive, and a **180-second** grace before the previous boot's last sight inside which a conversation that went is still offered, registered as `sessions.restore_rows`, `sessions.restore_boots`, `sessions.restore_batch`, `sessions.restore_seen_age`, `sessions.restore_heartbeat` and `sessions.restore_grace` (`restoreRowsLimit` … `restoreGraceLimit` in `app/session_restore.go`) | Past 200 the conversations that moved longest ago are not recorded, and past 200 rows the rows that went longest ago are deleted first; both are counted as evicted. A third boot deletes the oldest boot and its rows at the next write. A restore naming more than 20 is refused whole with `restore_batch_too_large` and opens nothing. An unchanged set is rewritten only once its `last_seen` is 300 seconds old; between rewrites the boot's `last_seen` alone is moved once it is 60 seconds old. A row that went more than 180 seconds before the previous boot was last seen expires from the offer (its row stays until its boot goes). | `/v1/diagnostics.capacity` carries all six rows, the evicted count on `sessions.restore_rows`; the sender of an oversized restore receives the typed refusal | Rows past 200 and boots older than the previous one cannot be offered back; a session closed outside Clawdline within 180 seconds of a restart is offered and can be dismissed; nothing a person wrote is lost | ✓ |
| N50 | One background command's output (`GET /v1/sessions/{id}/shells/{shell}`, the Shell panel; Cloud word `shell`) | **1 MiB** of the output file's end per read, **64 KiB** when the reader names no window, never less than **1 KiB**, registered as `sessions.shell_output_bytes` (`transcript.MaxShellOutput`); the Cloud word's `bytes` field is pinned to the same 1 KiB–1 MiB | A larger ask is lowered to the limit and a smaller one raised to the floor; the answer starts at the first line boundary inside the window and says `truncated`. The file itself is Claude Code's and is never cut or copied. | The answer's `truncated`; the row is present in `/v1/diagnostics.capacity`. | No: the earlier output stays in the file on the machine. | ✓ |
| N59 | Ordinary shells a person opens on this machine's own tmux server (`internal/adapters/terminal/owned`, `ports.OwnedTerminals`; the routes are N60) | **8 terminals** per machine (`terminal.count`, `terminal.MaxTerminals`); **4 KiB** per keystroke batch (`terminal.input_bytes`, `MaxInputBytes`); **1 MiB** per paste (`terminal.paste_bytes`, `MaxPasteBytes`); **2,000 lines** per history read (`terminal.history_lines`, `MaxHistoryLines`) | The ninth open is refused `terminals_full` and nothing is closed for it; an oversized batch or paste is refused `input_too_large` before a byte is typed; a larger history ask is lowered to 2,000 lines. The server itself keeps 5,000 lines of history per terminal (`history-limit` in its generated configuration). | The caller, in the refusal; the rows are present in `/v1/diagnostics.capacity`. | No: a refused open or input changes nothing, and older history stays in the server. | ✓ |
| N60 | The terminal routes (`/v1/terminals*`, `internal/app/terminals`, `internal/transport/http/terminals.go`) and the terminal grants (`terminal-grants.json`, `internal/adapters/devices/grants.go`) | **16** terminal inputs in flight per machine in the terminals' own lane (`terminal.lane`, `terminals.LaneLimit`); **8** streams per terminal (`terminal.viewers`, `MaxViewers`) and **16** per machine (`terminal.streams`, `MaxStreams`); a control lease lasts **30 s** without a renewal, renewed every 10 s (`terminal.lease_seconds`, `MaxLeaseSeconds`); the grants file is read up to **256 KiB** (`terminal.grants_bytes`, `devices.MaxGrantsBytes`); a terminal request body is at most **6 MiB + 4 KiB** (`terminal.body_bytes`, `terminalBodyLimit`: a 1 MiB paste after JSON escaping) | A full lane refuses the input `terminal_busy` at once and types nothing — the Agent Sessions' lanes are separate and unaffected; a ninth viewer or a seventeenth stream is refused `terminal_viewers_full`; a lease not renewed for 30 s lapses and its next input is `lease_expired`; a larger grants file is unreadable, so it grants nobody (terminals only); a larger body is refused before it is read. | The caller, in the refusal; `/v1/diagnostics.capacity` measures the lane and the streams, and `/v1/diagnostics.terminals` says whether the grants file could be read and why not. | No: leases live in memory and a lapsed one only stops typing; a refused viewer or input changes nothing; an unreadable grants file is left for a person to repair. | ✓ |
| N64 | iTerm2 stall diagnosis (`internal/adapters/terminal/stall.go`, `stall_darwin.go`; wired in `internal/transport/http/server.go`) | Triggered by **5** timed-out iTerm2 Apple Events (killed at the run's own limit, or `-1712`) within **2 minutes** — an episode runs 8-11 a minute, while 19 of the 29 failure clusters logged 2026-09-23..10-02 cleared with at most 4. Holds the newest **32** failures (`iterm.stall_failures`) and the last **512 bytes** of osascript's stderr per failure (`iterm.stall_said_bytes`); writes at most one diagnosis per **30 minutes** (`iterm.stall_cooldown_seconds`); keeps the newest **20** files (`iterm.stall_diagnoses`); each step at most **30 s** (`iterm.stall_step_seconds`; the probe 3 s, `lsappinfo`/`sysctl`/`ps` 5 s) and **512 KiB** of output (`iterm.stall_section_bytes`). One diagnosis runs at a time, in its own goroutine. | Past 32 the oldest failure is let go (each is already a log line); a run inside the cooldown, or while a diagnosis is running, starts nothing; after a write the oldest files past 20 are removed; a step past its deadline is abandoned and recorded `timed out`, a failed step is recorded `failed` and the file goes on; output past 512 KiB is not written and the section says how many bytes. | The log: one `iterm: osascript failed: kind=… session=… elapsed=… reason=…` line per failure and one `iterm: stall diagnosis written: <path>` per file; the rows are in `/v1/diagnostics.capacity`. | Yes, on purpose: older failures and diagnoses go. Nothing a person wrote is touched. | ✓ |
| N73 | Work-unit cursors (`store.AddWorkCursors`, `app.WorkUnitRecorder`, `GET /v1/usage/work-units`, docs/token-ledger.md "One unit of work") | **50 000 cursor rows** kept (`usage.work_cursor_rows`, `store.WorkCursorRowLimit`); **1024 edges** waiting for the cursor worker (`usage.work_cursor_queue`, `app.workCursorQueueLimit`); **500 units** per answer, most recent first (`usage.work_units_per_answer`, `app.workUnitAnswerLimit`); 64 behind cursors settled per pass | Past 50 000 rows the oldest go in the write that adds one (a journal: the ledger's own rows keep every session's cumulative totals). An edge posted to a full queue is not read: it is written as a `cursor_missing` marker before the queue drains, and past twice the limit it is counted and logged. Past 500 units the answer says `truncated` | `/v1/diagnostics.capacity` for all three; the daemon log once when the queue fills; the answer for the units | No: the queue only delays a cursor, and a narrower `since` reads the rest | ✓ |
| N74 | Work samples and the before/after report (`app.UsageLedger.WorkSamplesBetween`, `GET /v1/usage/work-samples`, `clawdline usage --freeze-baseline`/`--work-report`, docs/token-ledger.md "Did a change make one unit of work cheaper") | **2000 child tasks** per answer, newest first (`workSampleLimit` in `app/usage_report.go`); a group needs **20 completed comparable units** in each period before it is judged (`workReportMinUnits`); a frozen baseline file is read to **16 MiB** (`workBaselineReadLimit` in `cmd/clawdline/usage_report.go`); `page`, `parameter` and `read` lines on the capacity baseline, not rows, because the answer is computed on each ask and the file is the person's | Past 2000 the answer covers the newest and says `truncated`, and the report prints that line; a group under 20 says `insufficient_evidence` with its counts; a larger file is refused by name | The answer and the report | No: nothing is stored by the daemon, and a narrower range reads the rest | ✓ |
| N75 | A root cancelling a child it dispatched by mistake (`clawdline task cancel`, `POST /v1/orchestrator/tasks/<id>/cancel`, `internal/app/orchestrator/cancel.go`) | **500** bytes of reason after its whitespace is collapsed (`CancelReasonLimit`); **200** characters of Idempotency-Key (`cancelKeyLimit`); both on the capacity baseline, like N72 | A longer reason is refused `422 reason_too_long` (the CLI refuses it first, exit 2), an empty one `400 reason_required`; a longer key `400 bad_idempotency_key`. Nothing is cut and nothing is settled. | The sender, in the typed refusal | No: the task is left as it was | ✓ |
| N76 | Milestone handoffs and their comparison (`orchestrator.CheckMilestoneSummary`, `POST /v1/orchestrator/handoffs` with `milestone`, `clawdline handoff`; `app.UsageLedger.CompareHandoff`, `GET /v1/usage/compare-handoff`; docs/handoff.md "Milestone handoffs", docs/token-ledger.md "Did handing over pay") | A milestone summary is at most **6 KiB** (`MilestoneSummaryLimit`), with no quoted or fenced block over **12 lines** (`milestoneVerbatimLines`); obligations.md lists at most **40 rows** per kind (`milestoneObligationRows`); a comparison reads at most **300 finished items**, newest first (`handoffItemLimit`), and computes a saving only when each compared group has **20 items** (`handoffComparableLimit`); `admission`, `page` and `parameter` lines on the capacity baseline, because nothing is kept | A longer summary, or one with a long block, is a 422 `bad_milestone_summary` naming each problem, and nothing is opened; past 40 rows obligations.md says how many more; past 300 items the answer says `truncated`; a group under 20 items makes the verdict `insufficient_evidence` with a null saving | The refusal's `problems`; `clawdline handoff --check`; the comparison's `truncated`, `too_few` and `verdict` | No: nothing is stored beyond the handoff record and its package | ✓ |
| N77 | A turn's status report (`clawdline report`, `internal/adapters/turnreport`, the guide's "report" part) | A code file's whole text up to **70 000 bytes** (`fullTextLimit`), a Markdown file's up to **1 MiB** (`markdownLimit`); one commit's diff of one file up to **256 KiB** (`diffLimit`); **12 MiB** of text and diffs in one report (`reportLimit`); **500 commits** per report (`commitsLimit`); the daemon answers a kept report of at most **48 MiB** at `/reports/<id>` (`servedLimit`); `read`, `page` and `admission` lines on the capacity baseline, because the report is a file on the person's machine and nothing is kept by the daemon | A larger file carries only its diffs and says so; a longer diff is cut and says so; past 12 MiB the largest whole texts go first, then the largest diffs, each named in the report and on stderr; more than 500 commits is refused before anything is read; a larger kept page is not found | The report itself and the command's stderr | No: the person names fewer commits or reads the file in the repository | ✓ |
