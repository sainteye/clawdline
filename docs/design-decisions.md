> **The approved replacement work system is specified in [`work-system-v2.md`](work-system-v2.md),
> with its release gate in [`work-system-v2-acceptance.md`](work-system-v2-acceptance.md). The
> currently implemented v1 system remains described in [`work-system.md`](work-system.md) until
> cutover. The decision register remains authoritative where analysis documents disagree.**

# 設計決定：唯一的實作依據

> **從 2026-09-18 起，實作只照這一份。** `docs/broker-design.md`、`docs/broker-design-challenge.md`、`docs/board-design.md`、
> `docs/board-redesign.md`、`docs/timeline-design.md`、`docs/cutover.md`、`docs/limits.md` 保留為分析材料；與本文件衝突之處以本文件為準，
> **本文件沒寫到的，照原文件**（§3.6 列出仍然有效的範圍）。`docs/design-guidelines.md` 是**原則**，本文件是**實作決定**，兩者的關係見 §7。
>
> 依據：**master `4db8ddd`**。撰寫途中 master 前進了兩次，兩次都已重讀：broker B2＋B3（`e582a17`，程式）、
> `docs/design-guidelines.md`（`cde8510`）與 `docs/limits.md`（`ec4efdf`）。§5 的行號是 `cddea3f` 的，`4db8ddd` 只加了這兩份文件，程式行號不變。
>
> 本文件只做裁決與清單，**沒有改任何實作**，也沒有跑 build 或測試。

**使用者原話（逐字，取自 root session 的 transcript，時間是 UTC）**——這是判準 (a) 的全部來源：

| 時間 | 原話 |
|---|---|
| 09-17 00:30（摘要裡的原句） | 「我希望 mac 版有的功能都盡量 1:1 的對齊到，尤其是畫面的細節」 |
| 09-17 23:45 | 「接下來的目標就是舊的 app 退役，然後使用新的 app 來使用 https://app.clawdline.com/」 |
| 09-18 00:11 | 「請在這次順便去分析，這些東西是否有應該要 refactor 或是重新設計的地方」 |
| 09-18 00:13 | 「看板系統也一樣要去思考是否要重新設計或 refactor」 |
| 09-18 00:51 | 「所以我們沒有重新設計這看起來是 legacy 產物的東西嗎？ 也就是以第三方的角度來看是不合理的設計」 |
| 09-18 01:00 | 看板五點：「1. 使用者不知道是否建立…應該詢問 2. 太瑣碎的項目應該是給 session 看的 3. 看板項目的生命週期沒有定義清楚…a. 瑣碎無人管理 b. 開始之後沒人收尾、看不進度 c. 已經作完了但看板系統完全沒有更新 4. 要讓人類適當的參與、理解、給出指令 5. 不用人介入的項目…完全自動化建立、自動化追蹤、自動結束(類似 TODO)，並且不應該和給人看的項目混在一起」 |
| 09-18 01:02 | 「看板存在的兩個目的：1. 讓人類理解狀況 2. 讓 Session 不會忘記什麼要作」「規劃要作，但短期完全還沒有計畫開始要做的東西，應該乾脆純放在另外一種資料結構，類似 Backlog」 |
| 09-18 01:11–01:12 | 設計概規要涵蓋「觀測、記錄要有上限、效能、資料格式合理性」，而且「guide line 本身也必須要有一定的合理性…簡潔性…不能無限擴張」 |

---

## 0. 五句話

1. **使用者 00:51 那一問就是判準**：「照搬」必須用今天的資料撐住，撐不住的是歷史。所以 broker 這邊，第三方挑戰（challenge）的
   「應改／應廢除」**幾乎全部採納**；但它自己也同意的那一半——**不變量要留、只丟形狀**——同樣是裁決的一部分。
   `limits.md` 的上限規則也採納，只有「長文 90 天後搬到冷層」一處改成先不搬（D26）。
2. **看板照 `board-redesign.md` 做**（三種結構、同一個 SQLite、人的決定是證據），`board-design.md` 只在「787 張舊卡的 1:1 唯讀檢視」
   這個範圍內有效。理由是使用者 01:00、01:02 的原話，不是誰的量測比較多。
3. **一件事只有一份事實、一種拼法**：落地只看 broker 的 landing 紀錄；冪等只有一張收據表；派工只有 broker 一條路；
   寫入範圍只有一個不可變的 `declared_writes`。撰寫時還查到一個文件都沒點名的第二拼法：**排程走的是舊骨架 `app.Dispatcher`，
   它的 task 存在另一張表，跟 broker 的 claims 互相看不到**（D07）。
4. **已落地的程式不用退回**：B1＋B2＋B3 沒有一項要撤；要改或要刪的是 26 個有名有姓的缺口（§5 的 G01–G23、G33–G35），其中 8 個是 B1 重現了舊版事故的形狀
   （G01、G02、G03、G05、G06、G09、G17、G18），前五個加 G09 排在第一波。
5. **需要你拍板的 13 件事都已選了安全的預設**（§4）：沒有一件會在你醒來前改動你的資料或舊 app。**其中一件有期限**：
   舊 app 自己的 30 天規則會在 **2026-09-28** 起開始刪 task 紀錄（limits.md §2.4），要保留完整的凍結備份，得在那之前決定（U10）。

---

## 1. 判準與用法

**判準依序**（前面的贏後面的）：

- **(a)** 使用者親自說過的話。
- **(b)** 有量測證據的，贏過只有推理的。
- **(c)** 使用量為 0 的機制不值得保留，也不值得新投資。
- **(d)** 新版沒有舊 app 的相容包袱時，選簡單的那個。

**用法**：

- 派工時引用 **D 編號**（例：「做 W1：D05、D10、D11、D15、D16、D21、D22、D23」）。child 只照本文件，不要自己在幾份分析文件之間挑。
- 實作時發現本文件錯了，**寫進 report，由 root 改本文件**；不要自己改走另一份文件的做法。
- 表格裡的行號都是 `cddea3f` 的。

---

## 2. 分歧清單

只列**實質**分歧（做法或資料模型不同），措辭差異不列。「→」後是裁決的 D 編號（§3）。

| # | 題目 | 甲方：主張（證據） | 乙方：主張（證據） | → |
|---|---|---|---|---|
| X01 | 看板怎麼存 | **board-design C1**：整份 JSON 重寫照搬（`b6f4f555`：一次 42 ms、93% 是 encode；拆檔只省 16 ms 還要 journal） | **broker-design §6.9、challenge §5.1、board-redesign §3.2**：跟 broker 同一個 SQLite（`b6f4f555` 否決的是「手刻 journal 拆檔」，不是 SQLite；task 落地→待辦結束→看板結束要同一筆交易） | D02 |
| X02 | 看板的收據、歷史 log、id 當檔名、log 寫失敗 | **board-design C3–C6**：照搬（`ProjectBoardHistoryLog.swift` 的理由：多一行救得回、檔名要 UUID、log 失敗只計數） | **challenge §5.1**：全是 JSON 檔的後果，C5、C6 廢除；**board-redesign**：歷史是 `moves` 表 | D02、D04 |
| X03 | 看板有沒有人的指令 | **board-design B5**＋**challenge §5.1 B5**：狀態由證據推導，沒有手動狀態選單（舊 `project-board.md:30-34`） | **board-redesign §8、§10 #10**：人的「收下／放棄／排入」是證據（`closed` 0／787；使用者第 4 點） | D31 |
| X04 | 機器對帳要不要寫歷史 | **board-design B7**：小改，只在被記錄的事實改變時才寫 | **board-redesign D6、§3.2**：衍生狀態根本不存（4,124 筆對帳、75.5% 是 `planning⇄execution` 擺盪） | D04 |
| X05 | 看板／時間軸的讀 4／寫 8 佇列 | **board-design D5、timeline-design C7**：照搬（`architecture.md`「sound and should be kept」） | **challenge §5.1 D5**：那句話的前提是 Swift 只有一條序列 HTTP 佇列；4 與 8 找不到理由（`17f444b3` body 空白）；Go 版今天也沒做 | D06 |
| X06 | `progress` 狀態機放哪、吃什麼 | **board-design B1**：照搬，放在 `internal/adapters/board`，輸入舊 `StoredItem` | **challenge §5.1 B1、§6.3**：規則照搬，位置搬到 domain、輸入改 broker 事實（否則是落地事實的第二份拷貝） | D01、D33 |
| X07 | task 與看板項目怎麼綁 | **board-design §2.11**：`inferredSourceKey`＋`taskItems` 反查，標「跨文件待決」 | **board-redesign D8**：派工必帶 `work_id`（派工只有 12.1% 綁得到項目、近兩天 0／56） | D36 |
| X08 | 看板 API 的重新設計 | **board-design A7／A8**：五合一路由與位元組預算要重做，**兩套都要答**直到前端換掉 | **board-redesign**：新看板是另一套結構；**challenge A1**：舊 envelope 8 個欄位沒人讀 | D34 |
| X09 | store 壞掉 | **broker-design #9／A3**：「照搬，一字不改」（隔離區、503、省略 tasks） | **challenge #9**：原則要、隔離區是 JSON 產物（這台機器從沒建立過隔離目錄）；SQLite 真正會壞的是一列，B1 解不開就 `continue`、同 id 派工會 upsert 蓋掉 | D05 |
| X10 | 「持鎖不做 I/O」 | **broker-design #10、§6.3**：規則照搬，守衛是「鎖的方法只接受純函式」 | **challenge #10**：照字面，B1 正確的 `mutate`（`5feea4b`，三個 race 測試）就違規；改寫成「持寫入權時不做**外部** I/O」，交易取代 `writeMu` | D08 |
| X11 | 終端機寫入的 lane | **broker-design #13**：整台機器一條有界 lane | **challenge #13**：每個終端機一條＋全機 admission（child 自 9/07 那週起 100% 是 tmux；舊版 `Tmux.swift:664-665` 自己就是 per destination；`23ae603a` 修的是「同一個終端機」）；B1 一條都沒有 | D22 |
| X12 | 逾時與「重啟 grace」 | **broker-design §2.5、#14**：`restartGrace = 20` 讓第一次讀數進來再判逾時 | **challenge #14**：它只用在 linger（`Orchestrator.swift:6555-6590`），逾時判斷沒有 grace | D12 |
| X13 | 怎麼知道 child 收到簡報 | **broker-design #16、#18**：deadline 開火前先收 progress；打進 tty 不算、最多重打五次；**docs/broker.md**（B1）：分頁開始跑一個 turn 就升 `briefed` | **challenge #16、#18、§8①**：B1 自己的 `eefa318` 推翻了前者；31 天 `brief.progress` 2 次、第二次打入 1 次；「五」沒有任何紀錄；簡報明文叫 child 不要送心跳；改成用 secret 簽的 `accepted` 收據 | D10 |
| X14 | 讀不到畫面時怎麼判 `spawn_failed` | **broker-design #44**：讀不到畫面的平台整個關掉判準 | **challenge #14**：盤點完整度要當輸入，讀不到≠不在；**B2 落地的程式**（`watch.go:299`）：只要讀數裡看得到任何終端機就可以判，因為**這台 Mac 的盤點永遠不完整**（iTerm2 apple event 失敗，broker-design §9.1 實測） | D05、D11 |
| X15 | 什麼時候可以把 task 的 worktree 當空的回收 | **broker-design #17**：跟 child 說過話的 task 永遠不走 empty 回收 | **challenge #17**：「說過話」是代理訊號，child 照簡報保持安靜就保護不到；要系統正面回答：行程不在＋乾淨＋沒有 commit（家規：「unknown never authorises a removal」） | D13 |
| X16 | respawn | **broker-design #20**：照搬路由，並補進 cutover；**B3 已照此落地**（`respawn.go`） | **challenge #20**：31 天用 4 次、鏈上限從沒碰到，降級成派工的一個欄位 | D14 |
| X17 | `result.json` 的收尾 | **broker-design #24**：preflight＋30 秒兩次觀察照搬；**docs/broker.md** 把 30 秒窗列為「還沒做」 | **challenge #24**：驗證要；內嵌 8.7 KB 的 node 程式改 CLI；30 秒窗廢除（`result.recovered` 0 筆、找不到書面理由、marker 已綁 SHA-256） | D16 |
| X18 | 「完成」有幾條路 | **docs/broker.md**：`/complete` 帶 status＋summary 是一條正式路由，簡報也教 child 用它 | **challenge §6.1**：`result.json` 是唯一來源；`/complete` 先到時，`symbols`、`artifacts`、`verification`、`review` 都不會進紀錄 | D15 |
| X19 | 落地的證明 | **broker-design #31、§6.5**：兩條 arm 整條照搬，Arm 2 待做 | **challenge #31**：Arm 1 要綁定「這個 task 交付的東西」（B1 拿 `Worktree.Base` 報 landed 也會過）；Arm 2 31 天關閉 1 次、共用 checkout 的 98 筆 landed 全走 Arm 1，廢除 | D17 |
| X20 | 落地佇列 | **broker-design #34、§6.5**：不建表，但推導 members／order／turn；**cutover §2.2**：`/landing-queue`、`/advance`、`/order` 排在波 3 | **challenge #34**：`/advance` 31 天試 1 次、成功送達 0 次，`queues` 從沒存過；要的只是「一個 checkout 同時一個人落地」＝租約 | D20 |
| X21 | 「這個 task 寫哪裡」 | **broker-design H4**：清空 claims、另存 `landing_paths`；**B1 程式**：清空，什麼都沒存（`dispatch.go:216`） | **challenge §6.4**：一個不可變的 `declared_writes`＋`lease_scope`，宣告過的事實永遠不清空 | D21 |
| X22 | 冪等的拼法 | **board-design D4、timeline-design C5**：各自的 `expectedRevision`＋`requestId` 收據照搬；B1 各功能各一種 | **challenge §6.2**：九種活的拼法（其中 messages 的 `Idempotency-Key` 只檢查有沒有帶，start／resume／voice 只存記憶體 10 分鐘）；一張收據表 | D03 |
| X23 | 時間軸的冪等機器 | **timeline-design C1、C2**：`(producer, sourceID)`＋決定性 digest、批次內重複檢查照搬 | **challenge §5.2**：`ingest` 全 repo 沒有呼叫者、`requestReceipts` 0 筆，兩個生產者都在 daemon 裡；主鍵 `(project, sha)` 就夠 | D41 |
| X24 | 時間軸的寫入順序 | **timeline-design C3**：entry 先寫、checkpoint 後寫，照搬 | **challenge §5.2**：同一個交易就沒有先後 | D41 |
| X25 | 時間軸的 12 種狀態詞彙 | **timeline-design A2**：照搬 | **challenge §5.2**：2,000 筆只出現過 2 種 event；reconciler 的部署分支從沒被走過 → 延後 | D41 |
| X26 | 舊資料要不要匯入 | **cutover §3**：`landing-queue.json`、`owned-storage.jsonl` 建議匯入成唯讀參考 | **broker-design §6.1**：沿用「不轉檔」；**challenge #34**：佇列形狀不搬 | D50 |
| X27 | 落地驗收怎麼驗 | **cutover A5**：帶一個不是 branch 祖先的 commit 會被拒 | **challenge #31**：這個驗法測不出「拿 base commit 冒充」的洞 | D51 |
| X28 | 家規（dispatch policy）從哪來、超長怎麼切 | **broker-design §6.8、I1**：repo 是唯一來源、啟動時投影到安裝目錄；超長時切 base、local 永遠不切 | **B1 程式**（`orchestrator_wiring.go:86-106`）：直接讀舊 app `~/.config/clawdline` 的兩個檔、以**位元組**計、從尾端切（＝先切 local）；**plan.md §4** 的唯讀清單沒有這兩個檔；**cutover B1**：退役後不再讀 `~/.config/clawdline` | D23 |
| X29 | 派工有幾條路 | **docs/schedules.md**：排程走既有的 `Dispatcher.Dispatch`（過渡）；**cutover A8**：排程驗收只看有沒有開分頁 | **docs/broker.md**：broker 是派工；**plan.md §1 第 8 條**：claims 重疊要在開始前擋掉。**程式裡**：`app.Dispatcher` 查 `tasks` 表、broker 查 `broker_tasks` 表，**兩邊的 claims 互相看不到**；排程的 run 沒有 secret、沒有逾時（`schedules.md:108-110`） | D07 |
| X30 | 逾時從哪一刻算 | **broker-design §6.4**：舊版從 `briefedAt`；attached task 最多兩倍 timeout，「照搬並寫進契約」 | **B1 程式**：從 `CreatedAt`（`record.go:271`）；`attach_session` 一律拒絕（docs/broker.md） | D12 |
| X31 | 選單上的 root：通知延後算不算一次嘗試 | **challenge #22**：延後不計次，所以一直停在選單上的 root **永遠不會進 dead letter**（列為 B1 與舊版的差異） | **B3 落地的程式**（`notice.go:283`）：延後只記在記憶體、刻意不計次（背壓不是失敗） | D24 |
| X32 | 新畫面能不能有新字、新樣式 | **replica.md、wave8.md 共用前言**：1:1 復刻，字串只用 `zh-Hant.json` 既有的鍵 | **board-redesign §3.3、§3.5**：新看板三區、Backlog 分頁、Session 待辦面板，需要「Backlog」「Session 待辦」「等你決定」這些舊版沒有的詞 | D35 |
| X33 | 時間軸這一輪做不做 | **cutover §8.2**：波 5 與看板一起搬 | **timeline-design §6 #4、challenge §5.2**：等部署來源與預設過濾有答案再做 | D40 |
| X34 | 長文（summary、review 散文、graph、plan）的保留 | **broker-design §6.1 L3**：90 天，可設定（刪除） | **limits §4.3、§5**：90 天後留摘要、原文移到壓縮的冷層，不直接刪；review 的裁決、axes、finding metadata 是證據、留在 L2（F1：刪掉的證據歸因不回來；原文一年約 42 MB） | D26 |
| X35 | 可重算的 git 歷史滿了怎麼辦 | **timeline-design B1、§6 #3**：不自動刪，講出來、由人決定封存 | **limits §5、§6**：git 歷史 100% 可從 git 重算，自動淘汰、被引用的釘住；「等人決定」＝在沒人看的時候重演今天的凍結 | D41 |
| X36 | 冪等收據滿了怎麼辦 | **board-design B8**：照舊 FIFO 淘汰，把契約文件的句子改成「會淘汰」 | **limits §3.2、§5**：按**時間**過期；視窗內滿了拒絕新指令（429），視窗外的重送回 `receipt_expired`、不重新執行（FIFO 在大量寫入時會淘汰還在重試視窗內的收據；錯誤回應也佔一格） | D03 |
| X37 | 滿了之後誰被告知 | **board-design C1、§6 #5**：看板文件超過 8 MB 時在 `/v1/health` 講一次 | **limits §4.5**：數字只放 `/v1/diagnostics.capacity`；公開的 `/v1/health` 只在證據類或安全稽核類到頂時翻 `ok:false`，其餘走推播（獨立預算）與畫面 | D27 |

---

## 3. 決定表

欄位：**決定**／**理由**（括號是判準）／**被推翻的文件與段落**／**影響的已落地程式**（`cddea3f`）／**順序**（波次定義在 §6）。

### 3.1 共通：一份事實、一個 store、一種拼法

| ID | 決定 | 理由 | 被推翻 | 影響的已落地程式 | 順序 |
|---|---|---|---|---|---|
| D01 | **落地事實只有一份：broker 的 landing 紀錄。** 看板的 progress、Session 待辦的結束、（將來）時間軸的「已落地」、session 列的「還有 N 項未了結」都是它的純函數，不另存拷貝 | (b) 舊版量過三份答案互相矛盾（`e924dd9a`：53／24／17）；舊時間軸那份停在 09-17 02:06 早已分岔。(d) 新版還沒有第二份，現在定最便宜 | board-design B1（位置與輸入）；plan.md §4「項目寫入等 C1／C2」那句 | `internal/adapters/board/progress.go`（吃舊 `StoredItem`：舊卡檢視可留）；`internal/transport/http/sessions.go:308` `ownCloseReasons` 讀的是舊骨架的 `obligations` 表，**看不到 broker 的 pending landing** | W4（closeability）、T3（看板） |
| D02 | **一個 store：`clawdline.sqlite3`。** broker、看板三結構、coordinator、（將來）時間軸都在裡面；新 app 不寫任何 JSON 狀態檔（`config.json` 例外：人讀人改、沒有交易） | (a) 使用者要的「task 落地→待辦結束→看板結束」要同一筆交易（board-redesign §3.2）。(b) `b6f4f555` 的 42 ms 比的是手刻 journal，不是 SQLite。(d) | board-design C1、C3、C4、C5、C6、C7、C8、§1 表的「照搬（附條件）」、§6 #2；timeline-design B1–B7（失去對象） | `internal/adapters/board/settings.go` 寫 `CLAWDLINE_NEXT_DIR/project-board.json`（見 D37） | T3；settings 搬家見 D37 |
| D03 | **冪等只有一種：`receipts(scope, actor, key) → (request_digest, response, at)`，跟它確認的狀態變更同一個交易。** 同 key 同內容＝重播原回應；同 key 不同內容＝409。每個功能只宣告自己的 scope。**按時間過期、不按筆數**：視窗內（預設 24 小時，逐 scope 可設）滿了拒絕新指令（429＋`Retry-After`），視窗外的重送回 `receipt_expired`、**不重新執行**，過期後留 key＋digest 的精簡墓碑（limits §4.2）。完成通知的 `broker_notices` 不是這張（它是 outbox 狀態機，B3 的獨立表＋CAS 維持）；Cloud 封包的 replay window 是防重放、不是冪等，也不併 | (d) 九種拼法各自被測、各自出錯（challenge §6.2）；plan.md §1 第 7 條「收據持久且型別化」目前有兩種不持久。(b) FIFO 在大量寫入時淘汰視窗內的收據＝重送變重複執行；舊看板收據約 9/23–9/26 開始淘汰（limits §2.4） | board-design D4（語意保留、拼法改）、B8（保留 FIFO）；timeline-design C5；broker-design #38 的 `validate_only` 仍可做（收據仍不可變） | `store/sqlite.go:65` `receipts(command_id)`；`transport/http/orchestrator.go:400` messages 的 key 只檢查存在；start／resume／voice 的記憶體 10 分鐘（challenge §6.2，本文未重讀）；`adapters/board/settings.go` 的 JSON 收據；`domain/cloud/ledger.go` 無呼叫者（**不要啟用**，Cloud 指令要去重時用 scope=`cloud`） | W2 |
| D04 | **事實落盤、觀察不落盤，看板也一樣**：衍生的顯示狀態（排隊中／進行中／已落地…）每次讀取時推導，不寫回、不寫「對帳」歷史 | (b) B2 已實測：5 個 briefed child、121 秒，寫入 0（broker-design §9.1）；舊看板 4,124 筆對帳、75.5% 是擺盪 | board-design B7 | broker 已完成（`e582a17`） | 已完成；T3 遵守 |
| D05 | **讀不到≠空≠不在，而且「不在」要由負責它的那個來源正面回答。** ① DB 開不起來回 `503 orchestrator_store_unavailable`（B2 已做）。② **一列解不開＝那一列 `unreadable`**：列表與 inventory 要列出它，同 id 的派工回 409，不能當成沒有、不能被 upsert 蓋掉。③ 判斷「分頁不在」只看**那個分頁所屬 backend 的來源**是否完整（tmux 的 child 看 tmux 的讀數），不看全域完整度，也不接受「讀數非空」當依據。④ 不做隔離目錄 | (b) 家規 2026-09-11「unknown never authorises a removal」；B2 量到這台 Mac 的全域盤點**永遠**不完整，所以全域完整度會讓判定永不成立，而「非空即可」會在 tmux 讀失敗、iTerm 有讀數時誤判。(c) 隔離目錄在這台機器從沒被建立過 | broker-design #9／A3「一字不改」（隔離區部分）；B2 的 `alive ‖ rd.seen()` 規則 | `orchestrator/broker.go:200` 解不開就 `continue`；`dispatch.go:83` 讀錯當沒有 → `store/broker.go:125` `ON CONFLICT(id) DO UPDATE` 蓋掉；`watch.go:299` `alive ‖ rd.seen()`；`internal/app/inventory.go` 把各來源的 `Complete` AND 成一個（要保留每個來源的完整度） | W1 |
| D06 | **兩個原語，不再每個功能各做一套佇列：** ① **per-key 有界 lane**：同一資源序列化，全機一個 admission 上限，滿了回 429（背壓，不是失敗）；② **租約**：一個資源同時一個持有者，時鐘掛在「還活著的證明」上，讀不到就不放行 | (d) 舊版六套、新版六套上限各有拒絕碼（challenge §6.5）；(b) 等待者四次事故（`b2f25048`、`24a33139`、`15924b14`、`b2a64352`）就是租約的規格 | broker-design #13（整台一條）、#34／H1–H3；board-design D5；timeline-design C7 | B1 沒有 lane；`app/actions.go` 的 pasteboard 1＋4、`start.go` 的閘門、`schedules.go` 的 `ScheduleBook.mu`（都是 lane 形狀，**可留**，之後收斂到同一個原語） | W1（終端機 lane）、W5（租約） |
| D07 | **派工只有一條路：broker（`internal/app/orchestrator`）。** 排程以無 root 的 detached task 走 broker（有 secret、CHILD.md、逾時、result 收集）；舊骨架 `app.Dispatcher`、`app.Lander`、`tasks`／`obligations`／`receipts(command_id)` 表、CLI 的 `dispatch`／`settle`／`land`、`/v1/next/*` 影子路由退役 | (b) 讀程式確認：兩條路查兩張表，**排程的 task 與 broker 的 task 在 claims 仲裁裡互相看不到**，違反 plan.md §1 第 8 條；排程 run 沒有逾時，不寫 result 就永遠擋住下一次（`schedules.md:108-110` 自己寫了）。(d) | docs/schedules.md「派工走既有的 `Dispatcher.Dispatch`」（當時是過渡，現在定終點）；cutover A8、A9 的驗法 | `internal/app/dispatch.go:29`、`landing.go:24`、`schedules.go:805`、`scheduler.go`；`cmd/clawdline/main.go` 的 dispatch／settle／land；`transport/http/server.go` 的 `dispatcher` 與 `/v1/next/*`；`sessions.go:308` | W4 |
| D08 | **持有寫入權時不做外部 I/O（終端機、git、子行程、網路）；store 自己的 I/O 就是這把鎖要保護的東西。** 寫入權＝一個 `BEGIN IMMEDIATE` 交易（讀、判斷、寫在同一個交易裡），取代行程內的 `writeMu`；副作用在交易裡登記成 outbox 列，commit 之後才執行，結果再用另一筆指令記回來。守衛要能失敗（例：交易 callback 裡呼叫終端機會被拒），不能只靠命名 | (b) B1 的 `mutate`（`5feea4b`）靠「鎖內重讀」消掉四種寫回過去的 bug，照 broker-design 字面去「修」會讓它們回來。`writeMu` 只管得到一個行程，而 `SaveBrokerTask` 是無條件 upsert：CLI、備份、第二個 daemon 寫進來不會被擋也不會被發現。broker-design §3 搬 SQLite 的「跨行程安全」要到這一步才成立 | broker-design #10、§6.3 規則 1 的字面與守衛形狀；#12 維持但由本條實作 | `orchestrator/broker.go:292-293` `mutate`／`writeMu`；`store/broker.go:125`；`dispatch.go:365` worktree 在 `:232` 落盤之前建、分頁 id 開完才寫；push／relay／收養 `.ready` 先做後記（challenge #12） | W2 |
| D09 | **wire 上一個欄位一種拼法**：`api/v1/tasks.schema.json` 的四個 snake_case 舊名，Dashboard 改讀 camelCase 之後刪 | (d)；成本低但它就是「以畫面為由保留的後端形狀」 | — | `api/v1/tasks.schema.json:70` | 隨下一次動 Dashboard |

### 3.2 broker

| ID | 決定 | 理由 | 被推翻 | 影響的已落地程式 | 順序 |
|---|---|---|---|---|---|
| D10 | **簡報送達的證明＝child 用 task secret 簽的 `accepted`**（HTTP；沒有 loopback 時寫檔，形狀同 `progress.json`）；帶 secret 的 progress 也算。「分頁開始跑一個 turn」只當作**活著**的證據，不再把 `spawning` 升成 `briefed`。人看的那一句開場白保留（那是給人看的，不是收據）。不重打簡報 | (b) `eefa318` 量到 progress 當證明會把活的 child 判死；31 天 `brief.progress` 2 次、第二次打入 1 次；「五次」無任何紀錄；舊版自己已對 root 廢除重打（`fb54fb68`）。收據鏈（plan.md §1 第 7 條）的第一格今天是空的 | broker-design #16、#18 後半；docs/broker.md「打完 briefing 後分頁開始跑一個 turn，就升成 briefed」 | `watch.go:325` `proveBriefing`；`brief.go`（加一行）；`lifecycle.go`（新路由）；`taskdir`（`accepted` 檔）；`api/v1/orchestrator.schema.json` | W1 |
| D11 | **`spawn_failed` 只在兩種情況判**：沒有 `accepted`／progress，且（a）child 所屬 backend 的來源完整地回答「沒有這個分頁」，或（b）分頁卡在對話框。其餘一律等 task 自己的逾時。判了之後**關掉它開的東西**（tmux session），只在證明了的時候關 | 誤判的代價是刪掉活人的工作（D1 事故），漏判只是多等到逾時——代價不對稱（broker-design §6.7）。`3e37e8ec`：沒關的分頁造成下一次 spawn 失敗 | broker-design #44（「平台整個關掉判準」改成本條，不分平台）；#19 維持 | `watch.go` `runClocks`／`spawnVerdict`；`ports.Launcher` 沒有 Close，`clawdline-task-<id8>` 永遠不關 | W1 |
| D12 | **逾時：牆鐘、從 `created_at` 起算（B1 現行）、記在 record 上、沒有 grace。** `restartGrace` 與「一個終端機都沒有的讀數不能拿來判」屬於 linger（關分頁），不屬於逾時。attached 的「兩倍 timeout」不搬（`attach_session` 本來就拒絕）。契約說明寫清楚起算點 | (b) challenge 重讀 `Orchestrator.swift:6555-6590`：grace 只用在 `rearmLingers`。(d) 從建立算，預算是整個 task 的上界，最簡單 | broker-design §2.5、#14 的「重啟 grace 判逾時」；§6.4「attached 兩倍照搬並寫進契約」 | `record.go:271`（留）；契約說明 | W1（說明）；linger 在 W6 |
| D13 | **把 worktree／分支當空的回收，三件事都要系統正面回答**：擁有者行程不在、checkout 乾淨、分支沒有 base 以外的 commit。任一件讀不到就保留。「有沒有跟 child 說過話」不是依據 | (b) 家規 2026-09-11；child 照簡報保持安靜時，舊的代理訊號保護不到它 | broker-design #17 | B1 不回收（`RemoveWorktree` 無呼叫者）；`inventory.go:189` 附近把 `branch_empty`／`merged_and_clean` 標成 `dispose`，**不看行程** | W6 |
| D14 | **respawn：B3 已落地的 `POST …/tasks/:id/respawn` 保留為唯一拼法**（複製 `task.json`、新 id 與新 secret、整條鏈最多 2 次）。**不加** `respawn_of` 派工欄位，也不再為它排任何後續工作 | (c) 31 天用 4 次、鏈上限從沒碰到——不值得新投資，這一點 challenge 對。但它的「降級成欄位」建議的前提是「路由還沒做、代價 0」，B3 已經做完並有測試，現在拆掉也是投資。(d) 一種拼法＝已經存在的那一種 | challenge #20（前提不再成立）；broker-design #20 的優先度 | `respawn.go`（留） | 不排 |
| D15 | **完成只有一個來源：`result.json`**（由 beat 收）。`POST …/tasks/:id/complete` 改成不帶內容的「現在就來收」：驗 secret、觸發一次收集，**不從 summary 合成結果、不結算**。簡報不再教 child 帶 status／summary 呼叫它 | (b) 讀程式確認：`/complete` 通常比 5 秒的 beat 先到，`Settle` 用 summary 合成一個 Result，之後 beat 看到已結束就跳過，`symbols`、`artifacts`、`verification`、review 節點的型別化裁決全都不進紀錄（舊版會補、B1 不補）。(d) | docs/broker.md 路由表裡 `/complete` 的語意 | `lifecycle.go:79-92` `Complete`→`Settle(summary)`；`dispatch.go:604` 合成分支；`brief.go:226` | W1 |
| D16 | **`result.json` 收尾**：preflight 驗證保留；**30 秒兩次觀察窗不做**（marker 已綁那份 bytes 的 SHA-256）；收養 `.ready` 改 **create-if-absent**（不覆蓋已存在的 `result.json`）；內嵌的 node validator 改由 `<絕對路徑>/clawdline task finish <task_dir>` 取代（簡報給絕對路徑），CLI 落地前 JS 留著 | (c) `result.recovered` 31 天 0 筆；30 秒找不到書面理由。(d) Go 版本身就是 CLI；node 在 Linux／Windows 的 child 環境裡不保證（未量） | broker-design #24 的 30 秒窗與內嵌 JS；docs/broker.md 把 30 秒窗列為「還沒做」→ 改成「刻意不做」 | `taskdir/broker.go:213` `os.Rename`（會覆蓋）；`result-preflight.js:120` 註解仍說 broker 會等觀察窗；`brief.go` 內嵌 JS | W1（收養、註解）；W7（CLI） |
| D17 | **落地證明綁定交付（Arm 1）**：worktree task 在結算時記下 delivery head（分支當時的頭），`landed` 要證明這個 head 是 target 的祖先；共用 checkout 的 task 在派工時記下 base（當時的 HEAD），報的 commit 要是 target 的祖先、而且**不是** base 的祖先。拿 `Worktree.Base` 或任何派工前就有的 commit 報 landed 一律拒絕。**Arm 2（寫入集合兩次乾淨讀數）不做**：共用 checkout 由 root 報 commit，或走 `nothing_to_land` | (b) B1 的洞讀程式確認：`proveLanding` 只問「是不是祖先」，而 `Worktree.Head` 建立後從不更新（inventory 回的 head 永遠等於 base）。(c) Arm 2 31 天關閉 1 次，共用 checkout 的 98 筆 landed 全部帶 Arm 1 的驗證欄位 | broker-design #31、§6.5「兩條 arm 照搬（Arm 2 待做）」；cutover A5 的驗法（見 D51） | `lifecycle.go:276` `proveLanding`；`inventory.go:209` `row.Head = r.Worktree.Head`（永遠是 base） | W3 |
| D18 | **落地紀錄可更正但不可靜默覆寫**：更正走第一次落地時的同一道閘門，舊值以 `corrected_from` 保留並記 event；同狀態同 target、不同 commit 的重送是一次寫入，答案只有 applied 或 refused | 兩份文件一致（broker-design #30、challenge #30）；B1 重現了 G2 的事故形狀 | 無 | `lifecycle.go:262` `now.Landing = next` | W3 |
| D19 | **target 只看紀錄上的**：第一次 landing 時由 root 決定並記下；之前是「未定」。inventory 判斷 merged 用紀錄上的 target，**未定就是未知，不拿主 repo 的 `HEAD` 代替** | 兩份文件一致（broker-design #32：「Which branch a record should have named is a person's decision」）；B1 兩處違反 | 無 | `dispatch.go:612` 開 pending 時沒有 target；`inventory.go:239` 用 `"HEAD"` | W3 |
| D20 | **不做落地佇列**（`/landing-queue`、`/advance`、`/order`、slot notice 都不做）；「一個 checkout 同時只有一個人在落地」用 D06 的租約 | (c) `/advance` 31 天試 1 次、成功送達 0 次，`queues` 從沒存過，O2 的 bug 在舊 HEAD 仍在。它要防的併發集中在要退役的那個 repo（clawdline-go 目前 1 個 root、0 次） | broker-design #34 與 §6.5 的 members／order／turn；cutover §2.2 那一列 | 無（B1 沒做） | W5 |
| D21 | **寫入範圍＝一個不可變的 `declared_writes`（派工時宣告、永不清空）＋ `lease_scope`（`shared`｜`worktree`）。** 擋不擋別人是 `lease_scope` 的函數；落地的寫入集合就是 `declared_writes`；`claims: []` 與「沒宣告」要分得開（`claims_declared` 保留） | (b) B1 把 worktree task 的 claims 清空、沒存到任何地方——「一個寫了 26 個檔的交付，讀起來跟什麼都沒寫的 review 一樣」（H4）原樣重現；而 B1 的註解與 schema 都說有保留。(d) 舊版四個欄位兩個檔 | broker-design H4 的拼法（另存 `landing_paths`） | `dispatch.go:216`；`draft.go:212-213` 的註解；`orchestrator.schema.json:170` 的描述 | W1 |
| D22 | **終端機寫入：每個終端機一條序列 lane；全機一個 admission 上限，滿了回 429。** brief、完成通知、messages relay、一般 send、開 child 分頁全部走它；iTerm adapter 在自己裡面序列化 Apple events | (b) child 自 9/07 那週起 100% tmux；舊版 `Tmux.swift:664-665` 自己就是 per destination；`23ae603a` 修的不變量是「同一個終端機一次一個寫入者」 | broker-design #13 | `orchestrator_wiring.go:38` `Type`→`actions().Send`，沒有 lane；challenge 指出開 child 的 `NewTmuxSession` 繞過 `start.go` 的閘門（本文未重讀） | W1 |
| D23 | **家規**：① 超過 16,000 **字元**時切 base、local 永遠完整（照舊版 I1）；② 過渡期 broker 唯讀舊 app 的 `dispatch-policy.md`／`.local.md`，並把它們補進 plan.md §4 的唯讀清單；③ 退役前新 app 在自己的 repo 帶 base、啟動時投影到 `~/.config/clawdline-next/`（比對 SHA-256，不同才寫），local 永遠不寫（見 U8）；④ 超過 90% 在 diagnostics 出 `near_limit` | (b) 量到今天 base 15,091 B＋local 283 B＝15,376 B，**再多約 600 B，B1 就會先切掉 local**——與舊版的修法相反。cutover B1 是退役硬門檻 | broker-design §6.8 的時點（要等新 app 有自己的 base）；B1 的切法 | `orchestrator_wiring.go:86-106` `dispatchPolicy` | ①④ W1；③ W5 |
| D24 | **完成通知**：B3 的獨立表＋CAS 維持；選單造成的延後**不計入嘗試**（背壓不是失敗），但 notice 的年齡要在 diagnostics 看得到（B3 已有 `OldestPending`）；**進 dead letter 要發一則使用者通知**；broker 的 `/notify` 接上已落地的 Web Push | 延後計次會讓「root 正在等人回答」被判成送不到，那是錯的；但「永遠在等」必須看得見。push 已在 `0e0f145` 落地，`/notify` 卻仍一律 `not_subscribed` | challenge #22 把「永不 dead letter」當缺陷 → 判為可留、補可見度 | `notice.go:283` `deferNotice`（留）；`orchestrator_wiring.go` 沒有接 `Notify` | W5 |
| D25 | **儲存的補強照 broker-design＋limits**：`busy_timeout=1000`，`SQLITE_BUSY` 具型別並計入 diagnostics；備份用 `VACUUM INTO`；長文（summary、review 散文、graph、plan、instructions）分表，**熱路徑不碰、也不再每一輪解碼整張 `broker_tasks`**；summary 全文存、截斷只在畫面（limits N8）。保留：L1 工作區＝24 小時＋擁有者確認不在＋未知就保留（W6，D13）；L2 task 紀錄與裁決＝證據、不刪；L3 見 D26。稽核拆成**安全稽核**（8 MiB 一段輪替、分段不刪、0600）與**營運紀錄**（broker 生命週期已經在 store 的 `events`，不再寫進 audit）；daemon log 10 MiB × 5 輪替（limits §4.2） | broker-design 與 limits 一致；limits 補了數字與稽核拆分（舊 audit 80.8% 的位元組是 `orchestrator.*` 營運事件，安全事件不到 1%） | 無 | `store/sqlite.go:187` 只有 `SetMaxOpenConns(1)`，沒有 `busy_timeout`；`orchestrator/broker.go:184` `records` 每一輪讀出並解碼整張表（limits N1：SSE 訂閱者每 2 秒也一次） | W2；稽核與 log 見 C2 |
| D26 | **長文不刪、也先不搬**：留在 D25 的長文表裡，熱路徑不碰；review 的裁決、axes、finding metadata 是證據、留在 task 紀錄。不做 90 天刪除的清掃，也先不做冷層；等 `store.db` 的容量列（D27）進入 `warn` 才決定要不要啟用 limits §4.3 的冷層 | (b) limits 自己量的：長文每筆約 3.2 KB、一年原文約 42 MB，熱列一年約 97 MB（broker-design §6.1），同一個數量級；F1 證明刪掉的就歸因不回來。(d) 90 天刪除要一個清掃器，冷層要一種新的檔案格式與讀取工具；兩者都在解決一個在這個規模還不存在的問題，而容量列會在它存在之前叫 | broker-design §6.1 L3「90 天，可設定」（刪除）；limits §4.3 的冷層與 §8 #4 的預設（延後，不是否決） | 無（B1 沒有長文表） | W2（分表）；冷層不排 |
| D27 | **每一個有界的東西都登記在 `internal/domain/capacity`，滿了之後誰知道照 limits §4.5**：數字在 `/v1/diagnostics.capacity`；公開的 `/v1/health` 只在證據類或安全稽核類到頂（或讓位走到拒絕新派工）時翻 `ok:false, reason:"capacity_exhausted"`，與 `broker_beat_stalled` 共用 `reason` 列舉；推播只在 `critical`／`full` 與恢復時各一則、每列 24 小時最多一則、預算與 agent 的 30／小時分開；上限可注入且**覆寫只准調小**；測試不能解析到使用者的目錄（limits §4.8） | (a) 使用者：「你的時間軸已經滿了 <- 這本身就是一個設計問題」、01:11「記錄要有上限」。(b) 兩個 app 盤出 70 列上限，到頂會主動告訴人的只有 1 個（limits §0）；舊 `http_reliability` 是唯一做對的範本 | board-design C1 的「8 MB 時在 `/v1/health` 講一次」與 §6 #5；timeline-design B1 的「講出來」改由本列統一做 | 新版 `/v1/health`、`/v1/diagnostics` 沒有任何填充度欄位（limits N31）；`adapters/board/board.go` 的 `MaximumItems`／`MaximumProjects` 宣告了沒有引用（limits N25） | C1 起 |

### 3.3 看板

| ID | 決定 | 理由 | 被推翻 | 影響的已落地程式 | 順序 |
|---|---|---|---|---|---|
| D30 | **看板的設計＝`board-redesign.md`**：看板（人現在需要知道的）／Session 待辦（session 不能忘的，全自動建立、追蹤、結束）／Backlog（規劃要做、沒有開始的承諾），三張表、以 `work_id` 相連、每次移動寫一筆 `moves`、看板與 Backlog 的界線是「有沒有承諾」。`board-design.md` 只在「舊卡的 1:1 唯讀檢視」範圍內有效 | (a) 使用者 01:00 五點、01:02 兩個目的與 Backlog，逐條對得上。(b) board-redesign §1：人建立 0／787、`closed` 0／787、345 張證據上已結束但生命週期沒動 | board-design §1 表、§4 的 B5、B7、C1–C8、D5，§5 的順序，§6 #2 | 無衝突：舊卡檢視（`4ffd681`）保留 | T1–T6 |
| D31 | **人的決定是證據**：開始做、排入、放回 Backlog、收下、還要改、放棄、轉交、追蹤、不用追蹤、回答決定——寫進 `decisions`。狀態仍由證據推導，**沒有自由的狀態選單**；人的決定**不能覆蓋 broker 的事實**（不能把沒落地的標成已落地） | (a) 使用者第 4 點「要讓人類適當的參與、理解、給出指令」。(b) 只靠自動證據，十一天 `closed` 0 張 | board-design B5「沒有手動狀態選單」照搬；challenge §5.1 B5「維持」 | 無 | T3、T4 |
| D32 | **舊的 787 張卡**：照舊唯讀、1:1 檢視（已落地，留）；遷移成三結構是一次**批次提議**，你批准一次才套用（U1）；舊 store 一個字都不寫 | plan.md §4、board-design C9、board-redesign §7.4 一致 | 無 | `adapters/board/source.go`（留）；`pages/board.tsx`（留） | T5 |
| D33 | **`ProgressOf`／`ListSummaryOf` 的規則保留**（`delivered ≠ reviewed ≠ landed`），新看板用的那一份放 `internal/domain`、輸入是 broker 的事實（D01）；`internal/adapters/board/progress.go` 只服務舊卡檢視，舊卡退場時一起拿掉 | 規則是產品判斷（大家一致）；位置與輸入是舊 app 檔案格式的殘留（challenge §5.1） | board-design B1 的位置與輸入 | `adapters/board/progress.go`（可留） | T3 |
| D34 | **看板的讀取資源**：新看板用新的分頁資源（固定 page size，沒有前綴選擇器、沒有位元組預算）；舊的 19 欄 envelope 與五合一路由**只為舊卡檢視保留、不再投資**；8 個沒人讀的欄位（`mode`、`entitlement`、頂層 `updatedAt`、`available`、`snapshotBudgetBytes`、`automaticMutation`、`sourceIngestion`、`responsibilitySource`）在舊 console 不再當 oracle 時刪 | (d) 兩套 API 並存是舊版自己留下的債（board-design §2.10）；新看板本來就是另一套結構 | board-design A7、A8 的「兩套都要答直到前端換掉」 | `adapters/board/envelope.go`（留） | T3（新資源） |
| D35 | **新畫面不是復刻**（新看板三區、Backlog 分頁、session 詳情的待辦面板）：樣式用 `legacy/tokens.css` 的值，不發明新的樣式系統；字詞先找 `zh-Hant.json` 既有的鍵，找不到才新增，**新詞放新 app 自己的字串目錄**，不改照抄的 `zh-Hant.json`、不動 `MANIFEST.json` 守衛的檔 | (a) 9/17「1:1 對齊」針對的是既有畫面；9/18 的看板原話要的是新結構，新結構一定有舊版沒有的詞（board-redesign §3.5）。replica.md 的內嵌瀏覽器段已經用過同一條規則 | wave8.md 共用前言「字串只用 zh-Hant.json 已有的鍵」的適用範圍（只限復刻頁） | 無 | T6 |
| D36 | **不移植每則訊息的 workflow 信封與 `begin` 分類；派工帶 `work_id`**（沒帶就由 broker 依規則綁定或開一筆 Session 待辦，絕不在另一個專案另開人類卡）。task 與項目的綁定鍵＝`work_id`，取代 `inferredSourceKey` 與 `taskItems` 反查 | (b) 信封是 agent 自己開卡的來源（19.9% 的 begin 自判 `new_work`），也會污染畫面選單解析（62 行 log）；派工只有 12.1% 綁得到項目 | board-design §2.11「跨文件待決」→ 定案 | 無（新版沒有信封） | T2（派工欄位）、T3 |
| D37 | **看板設定**（`enabled`、`narrativeConsent`）從 `CLAWDLINE_NEXT_DIR/project-board.json` 搬進 SQLite，收據併入 D03；重播要回**收據上存的** revision，不是目前的 | D02、D03；challenge 指出 `settings.go` 重播回目前 revision | board-design C3（收據在同一份文件） | `adapters/board/settings.go`（搬之前可留） | W2 之後、T3 之前 |
| D38 | **刪掉沒有呼叫者的舊看板骨架**：`internal/domain/board` 的 `Decide`／`Command`，store 的 `board`、`board_commands` 表與 `BoardRevision`／`BoardSeen`／`ApplyBoardCommand` | (c) 非測試的呼叫者 0（`grep` 確認） | 無 | `domain/board/board.go:99`；`store/sqlite.go:121`、`:440` 附近 | W4 |
| D39 | **維持**：`entitlement` 常數、`narrativeConsent` 的欄位與開關（AI 外送延後）、抽屜「專案 · 看板」照舊版隱藏、舊卡從專案列進入——都只在舊卡檢視範圍。新看板的入口在 T6 再問你（U6） | 各文件一致 | 無 | 已落地（`4ffd681`） | — |

### 3.4 時間軸

| ID | 決定 | 理由 | 被推翻 | 影響的已落地程式 | 順序 |
|---|---|---|---|---|---|
| D40 | **這一輪不建時間軸**，等你回答「要不要接部署證據來源」與「預設過濾」（U7） | (c) `ingest` 0 呼叫、部署事件 0、`requestReceipts` 0；照搬等於搬一個預設看不到東西、而且會安靜停擺的功能（timeline-design §6 #4、challenge §5.2 一致） | cutover §8.2 把它排在波 5 | 無（新版沒有時間軸程式） | 等 U7 |
| D41 | **將來建的時候**：SQLite 投影；git 匯入以 `(project, sha)` 為主鍵、一頁一個交易；「已落地」讀 broker 事實（D01）。git 歷史是**觀察**類：滿了自動淘汰最舊的，**被看板引用的釘住**；落地與部署證據是**證據**類、自己的預算、永不自動淘汰（limits §4.2、§6）。**不搬** `(producer, sourceID)`＋digest、批次內重複檢查、entry 先於 checkpoint 的順序規則、`requestId` CAS（併入 D03）、讀 4／寫 8；12 種狀態詞彙等有部署生產者時再加。**照搬**：A1「commit／build／task 都不代表上線」、A3 封閉查詢參數、A4 `capacity` 公開、C4 先解析完整 SHA、D1 git 匯入唯讀有界 | (c)(d) 見 X23–X25。X35：timeline-design 避開自動刪除，怕的是「刪掉還被引用的」——這用釘住解決；等人決定＝在沒人看的時候再凍結一次 | timeline-design A2、B1、C1、C2、C3、C5、C7、§6 #3；B2–B7 失去對象 | 無 | 等 U7 |
| D42 | **舊時間軸的 2,000 筆：凍結成唯讀的歷史，不匯入、不摘要**（與 limits §6 一致）。新版的時間軸若建，git 歷史從 git 重新推導，落地從新版自己的紀錄推導 | (b) limits 量到：1,973 筆可從 git 逐字重算；27 筆 landing 裡 **10 筆是舊 app 測試寫進正式 store 的假資料**，另 17 筆在凍結的 `orchestrator.json` 有原始紀錄；部署證據 0 筆——沒有任何一筆是只存在那裡而且是真的 | 無 | 無 | — |

### 3.5 退役（cutover）

| ID | 決定 | 理由 | 被推翻 | 影響的已落地程式 | 順序 |
|---|---|---|---|---|---|
| D50 | **舊 broker 的資料不轉檔、不匯入**：`orchestrator.json`、`landing-queue.json`、`owned-storage.jsonl` 凍結＋離線備份（cutover B5），備份時記下每個檔的 sha256、大小與筆數（`project-timeline.json` 也一樣，limits §6）；新 broker 從空的開始，舊 task 留在舊 broker 跑完。**注意期限**：舊 app 自己的 30 天規則 2026-09-28 起會開始安靜地刪舊 task 紀錄（limits §2.4），「凍結」若要包含 09-28 以前的全部，備份要在那之前做（U10） | (d) 那 252 筆是舊 task 的寫入集合，新 broker 不管舊 task；新版沒有任何讀者 | cutover §3 對 `landing-queue.json`、`owned-storage.jsonl` 的「建議匯入成唯讀參考」 | 無 | cutover 波 0（U10） |
| D51 | **cutover A5 的驗法補一條**：拿 `Worktree.Base`（或任何派工前就存在的 commit）報 `landed` 要被拒 | 原驗法測不出 D17 的洞 | cutover A5 | — | W3 驗收 |
| D52 | cutover §2.2 的 `landing-queue`／`advance`／`order` 列改為「租約」（D20）；`respawn` 已由 B3 補（D14） | 見 D14、D20 | cutover §2.2 | — | — |
| D53 | **cutover A8／A9 的排程驗收要走 broker 路徑**（D07）：run 有 secret、有逾時、不寫 result 會自己結束並放行下一次 | 否則一個不寫 result 的 run 永遠擋住該排程 | cutover A8、A9 的驗法 | — | W4 驗收 |
| D54 | **cutover §8.2 的波次仍是大框**（先切「看」、最後切「派」）；broker 與看板的細部順序以本文件 §6 為準 | — | cutover §8.2 波 2–5 的細部內容 | — | — |
| D55 | **A drawing may retain the last complete reading for one failed terminal source for at most 120 seconds, and it must label every retained row with its age.** Retention is per source, so an iTerm2 failure does not age a current tmux row. `Fresh` decision callers never receive retained rows; a retained row cannot trigger waiting notifications or identify a task. At expiry the source becomes `missing`, not empty or current. The 120-second cache is registered as `cache.session_inventory` and may only be configured downward. | One iTerm2 listing already has a 10-second deadline, so the bound spans twelve consecutive deadlines and matches the existing screen-backoff ceiling. The 45-second closeability stale guard is shorter, so retained display state cannot authorise a close. Raising the Apple Event timeout is not justified by the available evidence: the production log records failure categories but not successful latency or contemporaneous load. | — | `internal/app/inventory_reading.go` previously replaced its held inventory after every incomplete pass; the console then rendered each process placeholder as a new unknown row. The header also counted only classified state buckets, so its first number could disagree with the number of rows shown. | This change |
| D56 | **A terminal tab or tmux pane is the sole existence authority for a Session.** Task records, identities, transcripts, Cloud rows and caches may enrich a row but may neither create one nor keep one alive. A complete enumeration by the row's owning terminal source removes an absent row. A source that did not answer makes no new existence decision, so the last complete row may be retained only under D55. A successful close is itself a positive terminal-backend answer: the daemon removes that row from its held readings immediately, and a Cloud viewer applies the successful `end` answer before any older retained row. Only a later current terminal enumeration may show that id again. The daemon decides all three facts; the console only applies their typed freshness and outcome. | This is the boundary that makes “do not forget on no answer” and “do forget on authoritative absence” compatible. At 12:57 on 2026-09-21, the close succeeded, an incomplete iTerm reading kept one unseen row for about ten seconds, and the next complete inventory moved from 11 ids to 10. The old row remained drawable during that interval even though the terminal backend had already answered the stronger fact. | — | The Cloud inventory was already a deletion barrier, but it only moved on the next publisher pass. A successful Cloud mutation also left no command line in `daemon.log`; state-changing Cloud answers now record operation, session, status, code, sender and sequence without body content. | This change |

### 3.6 Work system v2 (approved 2026-09-22)

The rows below supersede D30–D39 for the replacement. Those rows continue to describe the running
v1 implementation until cutover; they do not constrain v2.

| ID | Decision | Reason | Supersedes | Implementation status |
|---|---|---|---|---|
| D57 | **Only a person creates a work item.** Feature and Issue are executable; Epic, Refactor and Plan remain unassignable in Planning for v2.0. Agents and broker rules may create only previewable proposals. | The owner requires every Board item to have known human provenance; no more unknown automatically filed work. | D30, D31, D36 automatic placement/proposal paths | Approved, not implemented |
| D58 | **Assignment is a human action and the sole source of work ownership.** One item has at most one owning Session; one Session may own several items. Offline owners remain assigned until a person acts. | “Claimed” means the person deliberately gave the item to a new or existing Session, not that a dispatch or clock inferred responsibility. | D30 commitment rules and automatic handoff/drop | Approved, not implemented |
| D59 | **The owning Agent drives `assigned → implementing → verifying → merging → deploying → done`; the broker validates evidence without moving the phase on its own.** Blocked/waiting/offline/unknown are conditions, not phases. | The Agent performs implementation, verification, integration and deployment; the broker is the durable evidence and effect boundary. | D31/D33 derived automatic lifecycle and closure | Approved, not implemented |
| D60 | **The owning Agent may edit title/description, documents and item-local steps, and may drive the execution phase through `done`, but cannot change Project, kind, owner, deployment policy, cancellation, or reopening.** All writes are versioned and append an immutable event. | Agents need a local progress aid without acquiring human Board authority or overwriting concurrent edits. | D31's person-only content model | Approved, not implemented |
| D61 | **Session to-dos have two human-readable subjects:** one projected row per assigned nonterminal item, plus direct quick to-dos created with `+`. Direct to-do Send/Delete are person-only; owner Agent/person may complete; `sent_at` draws `✓`, Agent API read sets `read_at` and draws `✓✓`. Broker task obligations are nested execution detail. | Dispatch rows are not the same as human work, and a lightweight direct request must not silently become a Board item. | D30/D36 dispatch-derived top-level to-dos | Approved, not implemented |
| D62 | **An unsent direct to-do is pull-only.** No beat or idle detector may type it. Only a person's Send action creates immediate terminal-delivery intent; root guides ask Sessions to poll at turn boundaries. | An idle assistant has no autonomous turn, and background delivery would violate “Send only by the user.” | Any future automatic reminder interpretation | Approved, not implemented |
| D63 | **The broker remains execution/evidence infrastructure and loses Board authorship.** `work_id` links a task to an existing item; tasks, landings, leftovers and clocks create no item, assignment, proposal, placement or closure. | One source owns each fact: the person owns work/assignment, the Agent owns progress requests, and broker facts validate effects. | D30–D36 rule-created/moved work | Approved, not implemented |
| D64 | **Old Board content is permanently deleted at cutover, without content migration or backup.** The operation is explicit, local-only, dry-run/confirmation bound, deletes only proven Board targets, retains transcripts/tasks/landings/Git/worktrees/settings/credentials/Project icons, and proves old facts cannot regrow v1 rows. | The owner stated the old content has no value. Automatic destructive schema migration remains unsafe. | D32, D50 and U1/U9 only for Board content; broker-history retention remains | Approved, not implemented |
| D65 | **There is one authoritative Board.** Every card carries its Project icon from the current Project catalog; Project pages filter the same Board. The retired Project Board is removed from navigation and is not a live source. | Two Board surfaces and a legacy catalog source make an empty replacement indistinguishable from stale old work. | D32, D34, D39 legacy Board presentation | Approved, not implemented |
| D66 | **Assigning a Board item never interrupts a Session unless it is positively observed idle.** Assignment itself atomically creates the assigned-item to-do projection. A `working`, `waiting`, or `unknown` owner receives no terminal input and pulls `assigned_items` at its next turn boundary; an idle owner may receive one courtesy brief. | A Board assignment is next work, not an instruction to abandon the current turn. Unknown is not evidence of idleness, and the to-do projection already preserves the request durably. | Work-system v2 §8.1/§8.3 durable briefing intent | This change |
| D67 | **A direct to-do's `read_at` means only that the Session synchronized its queue, not that work started.** An open read row may be sent again by the person; a successful reminder replaces `sent_at` and clears the old `read_at` until the Session synchronizes again. Completion alone draws the green check, remains visible for confirmation, and is removed only by explicit Delete. | A turn-boundary poll can observe next work while the Session is still finishing its current turn. Treating that observation as acceptance hid Send and then removed the only conclusive completion state from the person's view. | D61's one-way read receipt presentation | This change |
| D68 | **A successful assignment turns two or more top-level Markdown list rows into item-local TODOs when the item has no steps; every TODO needs an owning-Session completion receipt before `done`.** One row is prose, nested rows stay nested, failed assignment seeds nothing, and reassignment never duplicates existing steps. The TODOs remain inside the parent item and never become Board items or Session assignments. | A person naturally groups several requested changes in one item. Preserving that grouping while making each requested result explicit prevents the parent from closing after only one visible part is finished. | D60's non-gating step semantics | This change |

### 3.7 沒有被推翻、照原文件執行的部分

裁決只動上面那些列。以下照原文件做，**不必再裁決**：

- **broker-design**：§7 的 #1（SQLite，已做）、#2、#3（已做）、#4、#5 的三層結構（數字見 D25，L3 見 D26）、#6、#7（coordinator 同一個 DB）、#8、#11（已做）、#12（由 D08 實作）、#15、#18 前半（打進 tty 不算送達）、#19、#21（契約補 enum，並註明「這是 08-25 的讀數」）、#22、#23（已做）、#25（已做）、#26、#27、#28、#29、#30、#32、#35、#36、#37、#39（已做）、#40（已做）、#41、#42、#43、#45、#46、#47；§4 的 A–J 各條「照搬」的不變量（形狀以本文件為準）；§5 O1–O11 的「不照搬」。#33 `legacy_unverified` 失去對象（D50：新 broker 沒有舊列）。
- **broker-design-challenge**：§3 標「維持」的 14 項；§4 的整張表（B1 沒搬的不變量）是 §5 待辦的來源。
- **board-design**：A1–A6、B2、B3、B4、B6、C9、D1、D3、D7、E1、E3、E4——**只在舊卡檢視範圍**。
- **board-redesign**：全文（D30），門檻與預設見 U1–U6。
- **timeline-design**：§2–§3 的量測；A1、A3、A4、C4、D1、E1、E3（等 U7）。
- **cutover**：§1–§7 的盤點與判準（A5、A8、A9 依 D51、D53 補驗法），§8.1 的回退規則。
- **limits**：§2 的盤點（70 列）與 §3 的分類；§4.1 登記表、§4.2 資料分類（長文那一列以 D26 為準）、§4.4 讓位順序、§4.5 告知規則、§4.6 水位與預測、
  §4.7 四層驗證、§4.8 測試不寫正式目錄；§6 舊時間軸；§7 的順序（對應本文件的 C 線，§6）。
- **design-guidelines**：全文（DG-1～DG-10 與 §4 的問句），是設計與 review 的原則；它跟本文件的關係見 §7。

---

## 4.0 已拍板（2026-09-19）

| 項目 | 使用者的決定 | 影響 |
|---|---|---|
| **U1** 787 張舊卡要不要遷成三結構 | **不遷移。** 原話：「不遷移」 | **T5 取消**，不做批次提議畫面。原本的唯讀凍結安排已由 U15 取代：切換時永久刪除舊看板內容 |
| **資料歸屬（B 組）**、**Cloud 正式連線（D 組）** | 照 root 的規劃做。原話：「2. 3. 照你規劃的做」 | B1「新 daemon 不再需要讀 ~/.config/clawdline」與正式環境連線依序進行；動到使用者帳號的那一步仍然先問 |
| **U14** 工作系統 v2 的預設 | **核准。** 原話：「這些預設若沒有問題……沒有問題，請做」 | D57–D63、D65 成為替代設計；先寫正式規格與驗收契約，不在本次改程式 |
| **U15** 舊看板內容 | **全部刪除，不遷移。** 原話：「舊的看板內容沒有意義，可以全部刪除」 | D64；切換時只刪已證明屬於舊看板的內容，不備份內容，broker／Git／Session 等非看板證據保留 |

## 4. 需要你拍板的（都已選安全的預設，你不決定就照預設走）

只收三種：不可逆、會改變你的工作流程、牽涉你的資料。

| # | 問題 | 為什麼要你決定 | 不決定時的預設 | 相關 |
|---|---|---|---|---|
| U1 | 舊的 787 張看板卡要不要遷成三結構 | 你的資料；第一次搬不該由機器決定 | **先不遷**。T1 做完唯讀的三軌投影後，給你一個批次提議畫面（看板 31／等收尾 66／Backlog 77／自動結束 260…），你逐列改、按一次「套用」才寫。在那之前新看板是空的，舊卡照舊唯讀 | D32、board-redesign §7 |
| U2 | 推翻舊版「看板沒有手動狀態」的原則 | 會改變你用看板的方式 | **推翻**，照你 01:00 的第 4 點：人可以下「收下／放棄／排入…」這些指令；但不能把沒落地的標成已落地 | D31 |
| U3 | 看板的時鐘與推播預算：停擺回 Backlog 3 天、值得問 24 小時、「短期」7 天、沒人回答的收尾與提議 7 天後的預設結果、只有擋住工作的決定才推播 | 決定你多常被打擾、東西多快離開你的看板 | **照 board-redesign §10 #2–#5**；全部是設定值，隨時可改（改門檻只會移動「進行中」與「等收尾」，不會動 Session 待辦與 Backlog，§7.3 量過） | D30 |
| U4 | 你在 session 裡說的話，算不算看板指令 | 改變你下指令的管道 | **算**，actor 記 `user_via_session:<run>`（run 證明訊息是你送的） | board-redesign §10 #7 |
| U5 | 不再在每則訊息裡夾 workflow 信封 | 切到新 app 後，session 不會再收到分類信封，agent 不會再自己開看板卡 | **不移植** | D36 |
| U6 | 新看板的入口放哪（抽屜「專案 · 看板」、側欄「看板」） | 畫面改變 | **舊卡檢視照舊版**（抽屜隱藏、從專案列進）；新看板到 T6 時再問 | D39 |
| U7 | 時間軸要不要接部署證據來源？預設過濾要不要改？ | 產品決定；沒有部署來源，時間軸只是 git 歷史瀏覽器 | **這一輪不建時間軸**；舊的 2,000 筆凍結唯讀 | D40–D42 |
| U8 | 家規（`dispatch-policy.md`）搬到新 app | 那是你的規則；base 目前 15 KB、local 是你自己的 283 B | **過渡期**新 broker 唯讀舊的兩個檔（現況，只修切法）。**退役前**把現在的 base 原文放進新 repo 當新 app 的 base；你的 local 由你自己複製到 `~/.config/clawdline-next/`——新 app 永遠不寫你的 local | D23 |
| U9 | 舊 broker 的資料不轉檔 | 你的資料（767 筆 task 的歷史） | **不轉檔、凍結、離線備份**；新 broker 從空的開始，舊畫面需要的舊 task 標題與交付勾仍由唯讀的 `swiftstore` 供應，直到 cutover B1 | D50 |
| U10 | **（有期限）舊 app 的下一面牆**：30 天規則 **2026-09-28** 起刪舊 task 紀錄；看板收據約 9/23–9/26 開始 FIFO 淘汰；看板卡片約 10 月中撞 2,000 張 | 舊 app 與你的資料；本任務依規定不能碰 `~/.config/clawdline` | **只回報、不動舊 app**（與 limits §8 #1 相同）。如果你要「凍結」包含 09-28 以前的全部 task 紀錄：在 09-27 前做 cutover B5 的離線備份，或把舊 app 的 `orchestrator_task_record_retention_days` 調大（一行設定）。備份檔含 `secret_hash` 等欄位，要放在只有你能讀的地方 | D50、limits §2.4 |
| U11 | 主畫面要不要有容量橫幅 | 會偏離 1:1，需要新字串 | **先不放在 1:1 的頁面**，放 Dashboard 的容量面板＋推播（limits §8 #3；該文件建議做） | D27、D35 |
| U12 | 長文要不要在 90 天後刪或搬走 | 刪了就回不來 | **不刪、也先不搬**，留在資料庫的長文表；等容量告警再決定 | D26 |
| U13 | 設計概規的上限（12 條）、編號（DG-n）與十條的取捨 | 那份文件自己寫明等你拍板（design-guidelines §5 #6） | **照現狀**：10 條、上限 12、新增要先說淘汰哪一條 | design-guidelines §1 |

---

## 5. 已落地的程式與決定的落差（待辦）

以 `cddea3f` 為準。「要改」＝跟決定衝突；「可留」＝不衝突或只是還沒做到。

| # | 位置 | 現況 | 決定 | 要改／可留 | 波 |
|---|---|---|---|---|---|
| G01 | `orchestrator/broker.go:200`（`records`） | 解不開的列直接 `continue`，beat、inventory、列表都看不到它 | D05 | **要改** | W1 |
| G02 | `orchestrator/dispatch.go:83`＋`store/broker.go:125` | 讀取失敗被當成「沒有這筆」，同 id 派工接著用 `ON CONFLICT DO UPDATE` 把那一列蓋掉 | D05 | **要改** | W1 |
| G03 | `orchestrator/watch.go:299` | `alive ‖ rd.seen()`：讀數裡看得到任何終端機就能判「分頁不在」 | D05、D11 | **要改**（per-source 完整度） | W1 |
| G04 | `orchestrator/watch.go:325`（`proveBriefing`） | 分頁開始跑一個 turn 就升 `briefed` | D10 | **要改** | W1 |
| G05 | `ports.Launcher`／`watch.go` | `spawn_failed` 不關 `clawdline-task-<id8>` | D11 | **要改** | W1 |
| G06 | `orchestrator/dispatch.go:216` | worktree task 的 claims 清空，沒有存到任何地方 | D21 | **要改** | W1 |
| G07 | `orchestrator/lifecycle.go:79-92`、`dispatch.go:604`、`brief.go:226` | `/complete` 用 summary 合成結果並結算；簡報教 child 用它 | D15 | **要改** | W1 |
| G08 | `taskdir/broker.go:213`（`AdoptReady`） | `os.Rename` 會覆蓋已存在的 `result.json` | D16 | **要改** | W1 |
| G09 | `transport/http/orchestrator_wiring.go:38` | 終端機寫入沒有 lane：brief、notice pump、relay 可以同時打進同一個終端機 | D22 | **要改** | W1 |
| G10 | `transport/http/orchestrator_wiring.go:86-106`（`dispatchPolicy`） | 以位元組計，超長從尾端切（先切 local）；讀舊 app 目錄但 plan.md §4 沒列 | D23 | **要改**（目前 15,376 B，未觸發） | W1 |
| G11 | `result-preflight.js:120` | 註解說 broker 會等一個觀察窗 | D16 | **要改**（只改註解） | W1 |
| G12 | `api/v1/orchestrator.schema.json:165` | `permission` 沒有 enum | broker-design #21 | 小改 | W1 |
| G13 | `orchestrator/broker.go:292`（`mutate`）＋`store/broker.go:125` | 行程內 `writeMu`＋無條件 upsert；跨行程不擋 | D08 | **要改**（B1 三個 race 測試的斷言不變） | W2 |
| G14 | `orchestrator/dispatch.go:365` vs `:232`；lifecycle／messages／watch 的 push、relay、收養 | 副作用先做、後記（worktree 在 `queued` 落盤前建） | D08（outbox） | **要改** | W2 |
| G15 | `store/sqlite.go:65`、`transport/http/orchestrator.go:400`、start／resume／voice | 收據各一種；messages 的 key 不存不重播；三條路由只存記憶體 | D03 | **要改** | W2 |
| G16 | `store/sqlite.go:187` | 沒有 `busy_timeout` | D25 | **要改** | W2 |
| G17 | `orchestrator/lifecycle.go:276`（`proveLanding`）、`inventory.go:209` | 只問祖先、不綁交付；`Worktree.Head` 永遠等於 base | D17 | **要改** | W3 |
| G18 | `orchestrator/lifecycle.go:262` | 同狀態同 target 不同 commit 直接覆寫 | D18 | **要改** | W3 |
| G19 | `orchestrator/dispatch.go:612`、`inventory.go:239` | pending landing 沒有 target；merged 用主 repo `HEAD` | D19 | **要改** | W3 |
| G20 | `internal/app/dispatch.go`、`landing.go`、`schedules.go:805`、`cmd/clawdline` 的 dispatch／settle／land、`server.go` 的 `/v1/next/*`、`sessions.go:308` | 第二條派工路徑與第二份 obligation，claims 與 broker 互相看不到；closeability 看不到 broker 的 pending landing | D07、D01 | **要改**（退役） | W4 |
| G21 | `transport/http/orchestrator_wiring.go`（沒有 `Notify`） | push 已落地（`0e0f145`），`/notify` 仍一律 `not_subscribed`；dead letter 不通知人 | D24 | **要改** | W5 |
| G22 | `orchestrator/inventory.go:189` 附近 | `dispose` 標記不看擁有者行程 | D13 | **要改**（目前沒有刪除路由，所以無害） | W6 |
| G23 | `domain/board/board.go:99`、`store/sqlite.go:121`、`:440` 附近 | `Decide`、`board_commands` 等沒有呼叫者 | D38 | **刪** | W4 |
| G24 | `orchestrator/notice.go:283`（`deferNotice`） | 延後只在記憶體、不計次 | D24 | 可留（年齡已可見：`broker_notices.go` 的 `OldestPending`） | — |
| G25 | `orchestrator/respawn.go` | 路由＋鏈上限 2 | D14 | 可留 | — |
| G26 | `orchestrator/record.go:271`（`Deadline`） | 從 `CreatedAt` 起算 | D12 | 可留（契約補說明） | W1 |
| G27 | `adapters/board/progress.go`、`source.go`、`envelope.go`、`web/console/src/pages/board.tsx` | 舊卡的 1:1 唯讀檢視 | D32、D33、D34 | 可留 | — |
| G28 | `adapters/board/settings.go` | JSON 檔＋收據；重播回目前的 revision | D37 | 可留到搬家 | W2 之後 |
| G29 | `domain/schedule`（`first_seen`、`ClaimScheduleFire`） | `first_seen` 在讀取時蓋章並落盤、觸發用 CAS | D04 | 可留（`first_seen` 是跨重啟要用的事實，不是觀察） | — |
| G30 | `app/schedules.go:601`、`:676`、`:699`（`ScheduleBook.mu`） | 跨派工持有一把行程內的鎖 | D06、D08 | 可留（它是 lane：序列化「決定＋開分頁」，不保護資料寫入）；W4 改走 broker 後由 broker 的 admission 背壓 | W4 |
| G31 | `api/v1/tasks.schema.json:70` | 同一欄位兩種拼法 | D09 | 可留到 Dashboard 改讀 | — |
| G32 | `domain/cloud/ledger.go` | 沒有非測試的呼叫者 | D03 | 可留，但**不要啟用**；Cloud 指令要去重時改用 receipts | — |
| G33 | `orchestrator/broker.go:184`（`records`）＋ SSE | 每一輪 beat、每個 SSE 訂閱者每 2 秒都讀出並解碼整張 `broker_tasks`，成本跟著全部歷史長（limits N1） | D25 | **要改**（熱路徑只碰活的列） | W2 |
| G34 | `orchestrator/watch.go`（`collectNote`） | `progress.json` 超過 300 字直接丟、不記（limits N5） | D05、D27 | **要改**（計數或具型別拒絕） | W1 |
| G35 | `adapters/board/board.go` 的 `MaximumItems`、`MaximumProjects` | 宣告了、沒有引用（limits N25） | D27、D32 | **刪**（舊卡唯讀，沒有寫入可限） | C1（守衛會先紅在這裡） |

容量面（log、稽核、task 目錄、裝置清單、推播訂閱、快取、請求大小、截斷旗標…）的完整落差是 `docs/limits.md` §2.3 的 N1–N31，
照 §6 的 C 線處理；上表只列跟本文件的決定直接相關的。

---

## 6. 實作順序（唯一）

broker 的波次以 **W** 編號，看板以 **T** 編號，容量（limits）以 **C** 編號，避免跟 broker-design 的 B1–B9 混淆。
**W1→W2→W3 同一個 package，一個接一個**；T1、C1 與 W 線的 claims 不重疊，可以現在平行（契約產出檔照慣例由 root 統一重生）。

| 波 | 內容 | 完成判準（要能失敗的測試或實際操作） | 依賴 | 大致的寫入範圍 |
|---|---|---|---|---|
| **W1 B1 的事故形狀** | D05、D10、D11、D12（說明）、D15、D16（收養、註解）、D21、D22、D23 ①④；G01–G12、G26、G34 | ① 一列解不開：列表看得到 `unreadable`，同 id 派工回 409；② 兩個寫入者打同一個終端機，重疊 0；③ tmux 來源完整且沒有該分頁→`spawn_failed` 並關掉 session；tmux 來源讀失敗→不判；④ 只有 turn 開始→仍 `spawning`；送 `accepted`→`briefed`；⑤ 先 `/complete` 再寫 `result.json`→紀錄帶著 result 的 `symbols`／`artifacts`／`review`；只 `/complete` 不寫檔→不結算；⑥ 已存在的 `result.json` 不被收養覆蓋；⑦ worktree task 的 `declared_writes` 保留並出現在落地寫入集合；⑧ 家規超長時 local 完整 | 無（B2＋B3 已落地） | `internal/app/orchestrator`、`internal/adapters/taskdir`、`internal/adapters/store/broker*.go`、`internal/transport/http/orchestrator*.go`、`internal/app/actions.go`、`internal/app/ports`、`internal/adapters/terminal`、`api/v1/orchestrator.schema.json` |
| **W2 儲存原語** | D03、D08、D25、D26（分表）；G13–G16、G33；（可順手）D37 | ① 第二個 store handle 同時寫同一列：一方拿到具型別的 conflict／BUSY，不會安靜覆蓋；② commit 後、副作用前殺掉行程：重啟後副作用恰好執行一次；③ 同 `(scope, actor, key)` 重送回原回應、內容不同回 409、視窗外回 `receipt_expired` 而不重新執行；④ 熱路徑查詢不碰長文表，beat 與 SSE 不再解碼整張表；⑤ B1 三個 race 測試斷言不變 | W1 | `internal/adapters/store`、`internal/app/orchestrator`、`internal/transport/http`（收據） |
| **W3 落地只有一份事實** | D17、D18、D19、D51；broker-design #35（`pending_live`／`pending_orphaned`）；G17–G19 | cutover A5（含 D51）：base commit 被拒、非祖先被拒、真交付通過；同 commit 重送 no-op、不同 commit 要嘛 `corrected_from` 要嘛拒絕；target 未定時 inventory 回未知 | W2 | `internal/app/orchestrator`、`api/v1` |
| **W4 一條派工路徑** | D07、D01（closeability）、D38、D53；G20、G23、G30 | 排程的 run 與 broker task 的 claims 互相擋（409 `workspace_busy`，在開任何東西之前）；run 有 secret／CHILD.md／逾時，不寫 result 會自己結束並放行下一次；`tasks`／`obligations` 表不再被寫；session 列的「還有 N 項未了結」包含 broker 的 pending landing | W3 | `internal/app/{dispatch,landing,scheduler,schedules}.go`、`cmd/clawdline`、`internal/transport/http/{server,sessions,dispatch,obligations}.go`、`internal/adapters/store/sqlite.go`、`internal/domain/board` |
| **W5 協調面** | broker-design B7（waits、coordinator 同 DB、detached、編譯插槽）；D06 租約（編譯插槽＝cutover A10、落地租約＝D20）；D24；D23 ③；broker-design #36 單一身分命名空間 | cutover A4、A7、A10；dead letter 時手機收到一則通知；兩個 root 同時落地同一個 checkout，第二個拿不到租約 | W2 | `internal/app/orchestrator`、`internal/adapters/store`、`internal/transport/http` |
| **W6 交接與回收** | broker-design B8（handoffs、root-assignments、graphs）；#26 linger 持久化（含 D12 的 grace 與「空讀數不判」）；D13 回收（含 limits N10、N11 的 task 目錄與 worktree 清掃，未落地的先存成 patch）；#37 補救指令由契約產生；#46 清不掉的 worktree 只講一次；G22 | cutover B4；linger 跨重啟仍關分頁；回收前三件事任一讀不到就保留 | W3、W5 | 同上＋`internal/adapters/git` |
| **W7 跨平台** | broker-design B9（#43 能力要有名字、`409 no_child_capability`）；D16 的 `clawdline task finish` | 沒有 node 的 child 也能完成回報；不能開 child 的平台在派工前就拒絕 | W1 | `cmd/clawdline`、`internal/app/orchestrator/brief.go`、`internal/adapters/terminal` |
| **T1 唯讀三軌投影** | board-redesign §9 第 1 步：`GET /v1/board/tracks?project=` 與 `clawdline board tracks` | 對 board-redesign 附錄的快照，數字與 §7.2 完全相同；每條規則一個會失敗的 fixture；**零寫入** | 無（可現在做） | 新 package（例如 `internal/domain/work`）、`internal/transport/http` 新檔＋`server.go` 一行、`cmd/clawdline`；**不動** `internal/adapters/board` 既有檔 |
| **T2 Session 待辦** | `todos` 表，由 broker 事實自動建立／結束／移交；派工帶 `work_id`（D36） | board-redesign §9 第 2 步的 failure injection | W3、W4 | `internal/app/orchestrator`、`internal/adapters/store`、`api/v1` |
| **T3 看板與 Backlog** | `work`、`board_items`、`backlog`、`moves`；D31、D33、D34、D37 | board-redesign §9 第 3 步 | T2 | 新表、`internal/domain/work`、新路由 |
| **T4 人的參與點** | `proposals`、`decisions`、待確認區、每日／每週摘要、§4.4 的拒絕碼 | 每個拒絕碼一個測試；`asked_inline` 與 `ask_true` 在 diagnostics 對得上 | T3、W5（推播） | 同上 |
| **T5 遷移** | 批次提議畫面，U1 批准後套用 | 重跑冪等；人改過的列不被覆蓋 | T1、T3、U1 | 同上 |
| **T6 前端** | 看板三區、Backlog 分頁、session 詳情的待辦面板（D35） | reviewer 對照 board-redesign §3.3 | T3、T4、U6 | `web/console/src/pages`、新字串目錄 |
| **C1 容量登記表第一步** | limits §7.1：`internal/domain/capacity`、四列（`audit.security` 改按大小輪替、`store.db`、`board.receipts`、`cloud.relay_queue`）、`/v1/diagnostics.capacity`、health 的 `capacity_exhausted`、`clawdline doctor capacity --drill`；D27；G35 | limits §7.1 的驗收（drill 在拋棄式目錄上走完 `ok→warn→critical→full→rotated=1`、正好一個通知意圖；登記表守衛上線第一天紅在 G35） | 無（可現在做） | `internal/domain/capacity`（新）、`internal/adapters/devices`、`internal/transport/http` 的 health／diagnostics、`api/v1/system.schema.json` |
| **C2 沒有上限的收口** | limits §7.2 波 2：daemon log 進 `CLAWDLINE_NEXT_DIR/logs/` 並輪替、稽核拆兩份、裝置清單與推播訂閱的上限要比讀取上限先叫（N13、N14）、記憶體快取 LRU、`result.json` 與缺上限的 request body（N9、N27）。**task 目錄與 worktree 的清掃不在這裡，併入 W6**（D13） | 各列在 C1 的登記表裡、drill 走得完 | C1 | 依項目 |
| **C3 安靜失敗的收口** | limits §7.2 波 3：Cloud 入站佇列改成拒絕並告訴送的人（N20）、screen bus 改覆蓋（N19）、`Store.Append` 錯誤計數與證據寫入失敗翻 health（N2）、transcript 與文件列表的截斷旗標並畫出來（N17、N28）、圖片與 drops 被引用就先告警（N15、N16）。**看板收據併入 W2（D03）、summary 全文併入 W2（D25）、`progress.json` 超長併入 W1（G34）** | 每一項「把一個安靜的失敗換成一個吵的」都有一個會紅的測試 | C1 | 依項目 |
| **C4 容量的通知與畫面** | 推播（獨立預算）、Dashboard 的容量面板、dead letter 與 spool 拒絕接上來；跟 W5 的 D24 一起做 | limits §4.7 第 4 層：縮小上限的拋棄式 daemon 走到 `capacity_exhausted` 並產生一筆通知意圖 | C1、W5 | 依項目 |
| 時間軸 | 不排 | — | U7 | — |

**跟 cutover 波次的對應**：cutover 波 2 ≈ W1＋W4 的排程部分；波 3 ≈ W2、W3、W5；波 4 ≈ W6；波 5 ≈ T 線；跨平台 ≈ W7；
C 線不屬於任何一波，隨時可做，但 C1 要在 cutover 波 6（退役日）之前完成——退役之後，舊 app 那些「滿了沒人知道」的牆（limits §2.4）就換成新版自己的了。
limits §7.2 的波 4（broker 保留）已經拆進 W2（D25、D26、G33），波 6（看板與時間軸移植時的分類預算）跟著 T 線與 D41 走，波 7（主畫面橫幅）等 U11。
cutover 的「新的開起來、舊的不要關」與回退規則照舊。

**單一 review 的建議**（照家規「durable state、concurrency、destructive effects 要一個獨立讀者」）：W1＋W2 合起來一次、W3 一次、W4 一次（它刪東西）。

---

## 7. 跟 `design-guidelines.md`、`limits.md` 的關係；以及已經過時的句子

這兩份在本文件撰寫途中落地（`cde8510`、`ec4efdf`），已全文讀過並併入：

- **`design-guidelines.md` 是原則，本文件是實作決定。** 十條沒有一條跟本文件的決定衝突：DG-4（只記事實、意圖先記）＝D04、D08；
  DG-5（一種拼法）＝D03、D07、D21；DG-6（讀執行用的那一份）＝D08 保留「寫入權內重讀」；DG-7（未知不是沒有）＝D05、D11、D13；
  DG-2（有界）＝D27；DG-9（逐項繼承）就是本文件；DG-10（人的位置）＝D10、D31 與 §4。以後兩者不一致時，**先照本文件做**，
  把不一致寫進 report，由 root 在本文件加一列裁決或提案修改概規（照它 §1 的程序）。
- **`limits.md` 的數字與做法直接採用**（D03 的時間過期、D25 的稽核與 log、D27 的告知規則、D41 的淘汰與釘住、D42 的凍結），
  **只有一處不採用**：長文 90 天後移到冷層（limits §4.3）改為「先不搬」（D26）。它的 §7 順序成為本文件的 C 線。
- **兩份的檔頭已經補上**（2026-09-19，root）：`design-guidelines.md` 的是英文，因為那份在同一天翻成了英文；`limits.md` 的是原文。當時交接寫的句子是：

  ```text
  limits.md 第一行：
  > **實作依據是 [`docs/design-decisions.md`](design-decisions.md)（2026-09-18 起）；本文保留為分析材料，與它衝突之處以它為準。**

  design-guidelines.md 第一行：
  > **實作上的具體決定在 [`docs/design-decisions.md`](design-decisions.md)；本文是原則，兩者不一致時先照那份做並回報。**
  ```

  `design-guidelines.md` 自訂了 280 行的上限，目前 273 行，加這兩行後 275 行，仍在上限內。

**已經過時、root 要順手改的句子**（本任務只加檔頭，不改這些檔的內容）：

| 檔 | 句子 | 為什麼過時 |
|---|---|---|
| `docs/plan.md` §4 | 「項目寫入等 `docs/board-design.md` 的 C1／C2 決定」 | D02、D30 已定 |
| `docs/plan.md` §4 | 唯讀清單沒有 `dispatch-policy.md`／`.local.md` | D23 ②：B1 在讀，要補進清單 |
| `docs/broker.md`「還沒做」 | 「`result.json.ready` 的收養沒有舊版的 30 秒觀察窗」列為待辦；`respawn` 列為還沒做 | D16：刻意不做；D14：B3 已做 |
| `docs/broker.md`「刻意與舊版不同」 | 「打完 briefing 後分頁開始跑一個 turn，就升成 `briefed`」 | D10 |
| `docs/schedules.md` | 「派工走既有的 `Dispatcher.Dispatch`」 | D07 |
| `docs/cutover.md` §6 | 看板「同名不同物：新版 `/v1/board` 回的是專案清單」 | `26c0152`／`4ffd681` 之後已是舊卡的 19 欄 envelope |
| `docs/plan.md` §3.2、`docs/cross-platform.md` | `scheduler` 在 `/v1/health` | 實際在 `/v1/diagnostics`（limits §2.3） |
| `docs/broker-design.md` §6.1 | L3 長文「90 天，可設定」 | D26：不刪、先不搬 |

---

## 8. 這份文件沒有做到的事（不算通過）

- **沒有跑 build、沒有跑測試、沒有改任何實作。** §5 的每一個「要改」都是讀 `cddea3f` 的程式得出的，**沒有重現**。
- 引用的使用量與容量（31 天的 audit、787 張卡、773 筆 task、70 列上限、09-28 的期限）來自 `broker-design-challenge.md`、`board-redesign.md`、
  `limits.md` 的量測，**本文沒有重算**。本文自己量的只有：家規兩個檔的大小（15,091 B＋283 B）、inflight 的三筆在跑 child、
  master 的前進（`3d443bf`→`cddea3f`→`4db8ddd`）。
- `start.go` 的閘門被 `NewTmuxSession` 繞過（challenge 讀的）、start／resume／voice 的收據只存記憶體（challenge 與 limits 各自讀到），**本文沒有重讀**。
- D26（長文先不搬）推翻了 broker-design 與 limits 各自的做法，依據是 limits 自己的量級估算（一年約 42 MB 原文）；**這個數字沒有在新版上量過**，新版的 store 幾乎是空的。
- 「per-source 完整度」（D05 ③）是本文的裁決，依據是 B2 實測「這台 Mac 的全域盤點永遠不完整」；**inventory 目前沒有分來源回報完整度**，這要在 W1 補。
- 沒有讀任何 token 或 secret；transcript 只抽了使用者自己打的訊息，並遮蔽任何像憑證的內容。
