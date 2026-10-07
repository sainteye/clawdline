<!-- clawdline-doc: kind=record audience=both -->
# Cloud wire implementation record (2026-09)

The [wire specification](../cloud-wire.md) contains the stable protocol sections.

## 2026-09 的規格前言

# Cloud wire：clawdline-go 接上 app.clawdline.com 的規格

這份文件是**後續 child 的規格書**。目標是讓 Go 版能照現有的 relay 協定跟 `app.clawdline.com`
講話，而 hosted console 一行都不用改（`docs/remote.md` 的設計原則 2）。

舊 Swift app 的 Cloud 端是 18 個 `Sources/Cloud*.swift` 共 22,345 行加 `WebPush.swift` 858 行；
Go 版在這一波之前是 0 行。**這一波只做地基**：canonical JSON、envelope 的編解碼、金鑰、簽章、時鐘。
實際連線、配對、27 種操作、推播都不在這一波，但這份文件把它們的規格也寫下來，因為後面要照著做。


## 9.5.2 Every refused read is told, and small answers keep a reserve (2026-09-25)

**Measured.** Every read a phone makes of the machine as a whole — the Board, a Session's to-dos,
and each reference image on them — answers on one channel, `t/<machine>/__clawdline_machine__`.
Board cards and to-do rows fetched the full normalized PNG for every thumbnail (one was 971,344
bytes, more as base64), so a few in flight held that channel at 3.2–4.0 MB of its 4 MiB. The daemon
log had 153 `did not fit its channel` lines between 2026-09-23 and 2026-09-25; the first 20 minutes
after the `operation=` logging logged 3, all `operation=work.v2.image`. And the refusal reserve
admitted **one** live refusal per channel: every carried read waits on its own read id, so the
second refused read on a channel got no answer at all and its browser waited out its timeout
(`did not fit its channel and the refusal did not either`).

Three rules replace that:

1. **Every refusal is admitted, up to a cap.** A refusal's payload names its own read id, so a
   second refusal on a channel is a different waiter's only answer, not the first one repeated.
   Up to `cloud.spool_refusals` (64) refusals may be live on one channel, each at most
   `spoolRefusalByteLimit` (4 KiB) — at most 256 KiB a channel. Only past the cap is a refusal
   dropped, and that drop is logged with the read's `operation=`, its sender and its sequence. The
   rule is the spool's and applies to every channel a refusal is published on, because every
   refusal this daemon publishes (`Service.tellChannelFull`) answers one specific request; the
   ingress `cloud_ingress_busy` answers (9.5) never used the reserve and are unchanged.
2. **Small answers are not starved by large ones.** On the machine-read reply channel only
   (`Outbound.Headroom`, set when the answer's session is `__clawdline_machine__`), an answer
   larger than `largeAnswerByteLimit` (256 KiB) is refused as `cloud_read_busy` 429 — deliverably,
   per rule 1 — when admitting it would leave less than `smallAnswerReserveLimit` (1 MiB) of the
   channel's 4 MiB free. An answer at or below the threshold may use the reserve. A large answer on
   an otherwise empty channel is always admitted, so a picture as large as
   `imageMaxEncodedBytes()` still has a way through. A Session's own transcript channel keeps its
   whole cap: it was sized for two of its largest answers (limits N22), which a reserve would halve.
3. **Cards ask for a thumbnail.** `work.v2.image` takes an optional `size: "thumb"` (the body is
   otherwise the fixed `{type, session, request, id}`; a page built before it sends no `size` and
   is answered the full image). It is routed to `GET /v1/work/v2/images/{id}?size=thumb`, which
   answers the stored image with its long edge at most 480 px (`artifacts.MaxThumbnailEdge`) as
   `image/jpeg`; `shapeImage` carries `image/png` and `image/jpeg`. A 1600 × 873 reference measured
   1,307,675 bytes as its normalized PNG and 13,199 bytes as its thumbnail. The console asks for
   the original only when the full-size viewer or the red pen opens it, keeps at most two
   reference-image reads in flight per page through Cloud, and retries `cloud_read_busy` after 1 s,
   2 s and 4 s before showing the failure (`web/console/src/pages/work/reference-images.ts`).

同一天順著這個問題做的稽核：**每一個決定「一筆答案可以多大」的地方，都要取兩個天花板裡小的那個**
——relay 的單封 ciphertext 上限，以及這台機器自己一條 channel 裝得下的量
（`cloud.spool_channel_bytes`）。`imageMaxEncodedBytes()` 原本只看前者，於是介於兩者之間的圖片會
通過門口、死在 spool。文件（2 MiB）與看板快照（1,000,000 bytes）本來就在下面，transcript 回應
（150 KiB）也是。


## 10.5 Go 端怎麼接（2026-09-18 量到的）

第三階段把 27 種操作接到這個 daemon 已有的本機路由上。**這一節只寫量到的事實**：路由存不存在是
2026-09-18 對 `http://127.0.0.1:7727` 用本機 token 發真的請求量的（寫入類只用不存在的 session id
`nope` 與會在找 session 之前就被擋下的 body，沒有碰任何真的 session）。

程式在 `internal/app/cloudops/`（詞彙、嚴格解碼、權限、路由對照、回應組裝）與
`internal/transport/cloud/`（傳輸接縫、假傳輸、in-process router）。`internal/adapters/cloud/`
是另一條線的，這一波沒有碰。

| 操作 | 類別 | 這個 daemon 的路由 | 量到的回應 | 狀態 |
|---|---|---|---|---|
| `transcript` | read | `GET /v1/transcript?session&limit` | 409 `session_unknown` | 接上（`priority` 收下不用） |
| `info` | read | `GET /v1/sessions/{id}/info` | 409 `session_unknown` | 接上（`parts` 兩半同一個 body） |
| `git` | read | `GET /v1/sessions/{id}/git` | 409 `session_unknown` | 接上 |
| `screen` | read | `GET /v1/sessions/{id}/screen` | 409 `session_unknown` | 接上（hosted console 2026-09-26 起也問；Cloud 上沒有 `screen` 修訂事件，tmux 也照 `on-demand` 每秒問一次） |
| `shell` | read | `GET /v1/sessions/{id}/shells/{shell}?bytes` | 404 `not_found`（transcript 沒宣告過的 id，`TestTheShellRouteServesAnnouncedCommandsOnly`） | 接上（2026-09-26；`bytes` 1 KiB–1 MiB，與本機 `sessions.shell_output_bytes` 同一個界線；`shell-kill` 仍沒有本機能力） |
| `image` | read | `GET /v1/artifacts/images/{id}` | 404 `artifact_not_found` | 接上（PNG → base64） |
| `documents` | read | `GET /v1/sessions/{id}/documents` | 404 `document_not_found` | 接上（清單去掉本機網址） |
| `document` | read | `GET /v1/sessions/{id}/documents/{scope}[/{task}]/{path}` | 404 `document_not_found` | 接上（只送 inert UTF-8） |
| `places` | read | `GET /v1/places` | 200 | 接上 |
| `schedules` | read | `GET /v1/orchestrator/schedules` | 200 | 接上 |
| `push-key` | read | `GET /v1/push/key` | 200（問它就是鑄它） | 接上（2026-09-20） |
| `board` | read | `GET /v1/board` | 200 | 接上，**但 body 形狀不同**（見下） |
| `send` | command | `POST /v1/sessions/{id}/send` | 400 `empty_text` | 接上 |
| `answer`（與別名 `key`） | command | `POST /v1/sessions/{id}/key` | 400 `bad_request` | 接上 |
| `end` | command | `POST /v1/sessions/{id}/close` | 409 `session_unknown` | 接上，**CAS 沒有比對**（見下） |
| `focus` | command | `POST /v1/sessions/{id}/focus` | 409 `session_unknown` | 接上 |
| `smart-title` | command | `POST /v1/sessions/{id}/smart-title` | 沒實測（會花一次模型呼叫） | 接上（2026-09-23；viewer request id 直接成為本機冪等鍵） |
| `start` | command | `POST /v1/places/{place}/start[/{assistant}[/{model}]]` | 沒實測（會真的開 session） | 接上，路由由 `start.go` 讀出 |
| `resume` | command | `POST /v1/places/{place}/resume/[{assistant}/]{past}` | 沒實測（同上） | 接上，路由由 `start.go` 讀出 |
| `voice` | command | `POST /v1/voice` | 400 `bad_request`（rate） | 接上 |
| `push-subscribe` | read-level command | `POST /v1/push/subscribe` | 200 / 400 `bad_request` | 接上（2026-09-20），**但每個 Cloud viewer 都是同一台裝置**（見下） |
| `push-unsubscribe` | read-level command | `POST /v1/push/unsubscribe` | 200 | 接上（2026-09-20） |
| `push-test` | read-level command | `POST /v1/push/test` | 沒實測（會真的對 `web.push.apple.com` 發 HTTPS） | 接上（2026-09-20），路由由 `push.go` 讀出 |
| `agent` | read | — | `GET …/agents/{id}` 405 | 回 `unknown_command` |
| `skills` | read | — | `GET …/skills` 405 | 回 `unknown_command` |
| `board.items` | read | — | 本機沒有卡片模型 | 回 `unknown_command` |
| `timeline` | read | — | `GET /v1/timeline` 501 | 回 `unknown_command` |
| `snippets` | read | — | `GET /v1/snippets` 501 | 回 `unknown_command` |
| `schedule` | read | — | `GET /v1/orchestrator/schedules/{id}` 501 | 回 `unknown_command` |
| `diagnostics.report` | read-level command | — | 沒有這條路由 | 回 `unknown_command` |
| `diagnostics.events` | read-level command | — | 沒有這條路由 | 回 `unknown_command` |
| `dispatch` | command | （有 `POST /v1/orchestrator/tasks`） | — | 回 `cloud_dispatch_unpinned` 409，照舊版 |

**為什麼「沒有本機能力」是回 `unknown_command`。** 舊 app 27 種全都有，所以它只在 `default:` 用這個碼。
但這正是 hosted console 會學的那一個：`net/cloud-client.js` 的 `_settleRead` 看到 `unknown_command`
就把這個字記進 `machineLacks`，之後不再問這台機器（`_unsupportedRefusal` 回
`cloud_feature_unavailable`）。所以回這個碼＝這台 Mac 老實說「我不會」，而不是沉默或超時。
唯一與舊版不同的地方：舊版那個分支是「連 body 形狀都不知道」，所以只能發 notice；這裡的字是**知道形狀的**，
所以先照它自己的規則解碼，再把拒絕發在這個 read 自己的 `(session, name)` 上，等的人才收得到。

**`dispatch` 照舊版拒絕。** 這個 daemon 有 `POST /v1/orchestrator/tasks`，但 hosted console 只送
`{task}`，本機 broker 要的是 materialized `task.json` 加 task id 與 secret，沒有任何 pinned wire shape
說這些怎麼帶、那個檔案可以寫到哪裡。舊版為此回 409 `cloud_dispatch_unpinned`，這裡一字不改。

**兩個「接上了但答案不一樣」的地方**（`cloudops.Divergences()` 會把它們列出來，接線的人在決定要對外
advertise 哪些字時讀得到）：

1. `board`：這個 daemon 的 `/v1/board` 是「從現在跑著的 session 推出來的 projects 快照」
   （`{schema_version, revision, projects, source, at}`），舊版是 `{"board": {items, revision, …}}`。
   兩邊回答的不是同一個問題。**在 Go 的 board 對齊之前，不要把 `board` 放進對外宣告的 `commands`**；
   要改成一律拒絕的話，`internal/app/cloudops/ops.go` 裡把 `board` 的 `route` 拿掉就會變
   `unknown_command`，是一行。
2. `push-subscribe`：**每一個 Cloud viewer 在這裡都是同一台裝置。** in-process 的 Cloud 請求帶的是這台
   機器自己的 local token（`internal/transport/cloud.LocalAuthorizer`），而 push store 一台裝置只留一列
   （`push.Store.Add` 以 endpoint 或 device 取代舊列），所以第二個用 Cloud 開通知的瀏覽器會把第一個
   擠掉——同一台 Mac 在自己網域上配對的兩支手機則是各留一列。存下來的 web-app origin 也因此是這個
   daemon 自己的 `http://127.0.0.1`，而那正是 iOS declarative notification 用來解析網址的那個 origin。
   要修的不是這四個字，而是「viewer 自己的身分要到得了路由」，這條 wire 上沒有任何字帶得了它。
   量在 `internal/transport/http` 的 `TestEveryCloudViewerIsTheSameDeviceHere`（2026-09-20）。

其餘兩個較小的：`transcript` 的 `priority` 收下不用（這裡只有一條 lane），`info` 的 `parts` 兩半回同一個
body（`summary` 在這裡其實是 full）。

**權限。** Cloud 來的指令屬於已配對的 viewer，照舊版分三層：read 不過寫入閘門（transcript 讀不進任何東西）；
`diagnostics.report`／`diagnostics.events`／`push-subscribe`／`push-unsubscribe`／`push-test` 是 read-level
command（遠端寫入關掉也能送，但仍要過 roster 與時鐘——問這台 Mac 有事時通知我，是「讀」的另一條路，
不是往誰的 session 裡打字）；
其餘 command 先過機器的寫入開關（關著回 403 `cloud_commands_disabled`），解碼後在「不可回頭的那一點」
重讀一次授權，依序回 `command_clock_uncertain` 503、`command_roster_unreadable` 503、`unknown_sender` 403、
`cloud_commands_disabled` 403。**預設全部拒絕**：`Bridge` 的零值不允許任何 command。

**`X-Clawdline-Actor: device`——只拿得走，給不了。** 這個 daemon 是用 in-process 的方式打自己的路由，
請求上蓋的是這台機器自己的憑證，所以有些路由會問「你是從哪道門進來的」，而 Cloud viewer 進來的時候
會穿著這台機器的身分。這台機器有兩個「我」：orchestrator token（開 `/v1/orchestrator/*`），以及
local token（shell 與自己的腳本拿的那把，verdict 上是 `Local`）。會對 `Local` 放行的路由，放行的就是
這台機器自己的手——`/v1/push/unsubscribe` 讓它移掉任何一台裝置的訂閱（腳本收自己的尾），`/v1/settings`
讓它改全域快捷鍵。所以帶了這個 header 的請求，兩道門一起關掉，**而且它一道也開不了**：它講不出裝置、
講不出能力、講不出 sender，帶了它只會變少。會寫入的 schedule 四個字與 push 三個字都帶它。
沒有這個 header 之前量到的：Cloud viewer 可以移掉手機自己配對時留下的那一列訂閱
（`TestACloudViewerDoesNotInheritThisMachinesOwnExemption`，2026-09-20）。

**回應形狀。** 這一版產生的是已上線 producer 的形狀，也就是 `t/<machine>/<session>` 上的
`{"read": <name>, "status": <http>, "body"|"error": …}`（`CloudAppBridge.publishJSONAnswer`、
`performRead`）。§10.2 那個九欄位的 `execution_response` 是契約包裡 `ctlr/` 回覆軌的形狀，Swift 沒有分支、
PWA 會拒絕（§11 的 `GAP-CTLR`），所以這一波不產生它。

**這個 daemon 的拒絕有兩種拼法，兩種都要讀。** gate 與 documents 回
`{"error":{"code","message"}}`（舊版形狀），其餘多數路由回 `{"error":"<code>","detail":"<句子>"}`。
只讀前者的話，`session_unknown` 會變成 `command_failed`，`close_blocked` 的 `reasons` 會整個掉。
兩種都正規化，`reasons` 這類同層欄位也一起帶過去。

**還沒接線。** `Bridge.Router` 的實作 `internal/transport/cloud.Router` 需要一個 `Authorize` 勾子，
把 gate 判得出來的憑證蓋在 in-process 請求上；沒有勾子時 gate 會回 `unauthorized`，這是刻意的預設。
誰接線誰決定「一個已配對的 cloud viewer 在這台機器上算什麼」，那個決定要寫下來，不是這個檔案去假設。

---

---

## 11. 已知落差，以及這一版選了哪一邊

契約包自己列了 8 個 gap（`contracts/cloud/v1/README.md:181-191`）。跟這一波有關的：

| gap | 內容 | **這一版怎麼做** |
|---|---|---|
| `GAP-NONCE` | PROTOCOL.md §1 範例寫 24 bytes，其他全部是 12 | 用 **12**。Go 常數 `NonceBytes = 12`，測試用向量對照 |
| `GAP-CTLR` | Swift 沒有 ctlr 分支；PWA 拒絕 ctlr；relay 接受 | Go **讀得懂** ctlr（validate 接受），但 `ProducibleChannel("ctlr/…")` 拒絕發送，直到 additive readers 部署。契約要求的順序是 readers → mirrors → consumers → producers（README:193-199） |
| `GAP-MASTER-KEY` | PROTOCOL.md §1 說 `ct` 一律用 master secret，沒寫 ctlr 例外 | 依契約與向量：ctlr 回應用 reply key，並且**必須拒絕**用 master secret 開它。已有測試 |
| `GAP-CT-MIN` | wire 只要求 `ct` 非空；開封另外要 ≥ 16 bytes | 兩層分開實作：`validateUnsigned` 只檢查非空與上限，`Open` 檢查 tag |
| `GAP-RAW` | 一般的 JSON parser 會在驗證之前就抹掉重複 key 與數字拼法 | 自己寫 parser，不用 `encoding/json` 讀 envelope |
| `GAP-PAIR-SIZE`、`GAP-RECEIPT`、`GAP-REPLY`、`GAP-MIRROR` | 配對大小上限、收據 body、請求／回應語意、mirror 摘要 | **這一波沒碰**。下一波做配對與指令時要先讀這四項 |

另外兩個 Go 端自己的判斷（本文件之前沒有來源，標明為推論）：

1. **`Parse` 與 `ParseStrict` 分開。** 舊 app 只有嚴格版，因為它用 `JSONDecoder` 讀 envelope；
   Go 端不想用 `encoding/json`（`GAP-RAW`），所以自己的 parser 要能同時服務「wire 上的
   envelope（順序任意）」與「pairing body（位元組固定）」。
2. **`ProducibleChannel` 這個函式是新的。** 它把「讀得懂」與「可以發」分成兩件事，讓 `GAP-CTLR`
   變成一個會編譯失敗的事實，而不是一段只能靠人記得的註解。

---

## 12. Go 端的對照表（這一波交付的）

| 這份文件的節 | Go 檔案 | 對照的向量／測試 |
|---|---|---|
| §3 canonical JSON | `internal/domain/cloud/canonicaljson.go` | `TestPublishedBodiesAreCanonical`（offer、handover、AAD、execution_response 四份 body 逐位元組相同）、`TestParseRefusals`、`TestMemberOrderIsUTF16NotBytes` |
| §2、§4.1、§4.2 envelope | `internal/domain/cloud/envelope.go` | `TestSealReproducesEveryPublishedEnvelope`（6 個向量的 `ct` 與 `sig` 逐位元組相同）、`TestCanonicalEnvelopeBytes`（byte_length 與 SHA-256）、`TestChannelGrammar` |
| §4.4 ctlr reply key | 同上 | `TestControlResponseUsesTheRequestReplyKey`（master secret 開不開得了，兩個結果都讀向量自己的宣告） |
| §5 金鑰與指紋 | `internal/domain/cloud/keys.go` | `TestDeviceKeyMatchesThePublishedSeed`、`TestFingerprintMatchesThePublishedPairingValues`（machine 與 viewer 兩個 oracle） |
| §10.3、§10.5 27 種操作 | `internal/app/cloudops/`、`internal/transport/cloud/` | `TestEveryOperationIsAnsweredAsItself`（整個詞彙各一筆真形狀的指令，斷言回哪個 channel 與打哪條路由或回哪個碼；表的筆數與 `Vocabulary()` 對不上就 fail，所以不用手抄數字）、`TestTheWriteSwitchIsOffUntilSomebodySaysOtherwise`、`TestTheAuthorityIsRereadAtThePointOfNoReturn` |
| §5.4 儲存 | `internal/adapters/cloudkeys/files.go` | `TestAnUnreadableSecretIsNeverAnAbsentOne`、`TestASymlinkIsNotAKeyFile`、`TestRefusesTheSwiftAppsDirectory` |
| §6.1、§6.2 時鐘 | `internal/domain/cloud/clock.go` | `TestAdmissionOpensOnlyAfterAWholeStableWindow`、`TestEachAnomalyClosesAdmissionAndOwesCleanup`、`TestOfferDoesNotRestartALiveWindow` |

**沒有 byte 對照的項目**（不可以當成通過）：

- recovery code（§5.3）：沒有公開向量，用獨立的 Python 實作當 oracle。
- EpochGuard（§6.2）：沒有向量，是行為移植加表格測試。
- replay 視窗（§6.3）、pairing（§8.3）、握手（§7.1）：**這一波沒有實作**。
- 指令載荷（§10）：第三階段實作了（§10.5）。**沒有 byte 對照**——`vectors.json` 只有
  `control_response` 那一筆，走的是 `ctlr/` 回覆軌（`GAP-CTLR`，還不產生），已上線 producer 的
  `{"read","status","body"}` 形狀沒有公開向量，所以這一層是照 `CloudAppBridge.swift` 移植加表格測試。

---

## 13. 下一波的順序（建議）

1. **replay 視窗＋command ledger 的 idempotency**（§6.3）。純邏輯，可以表格測試，而且是後面每一
   波的前提。
2. **配對**（§8.3）：X25519＋HKDF＋wrapper，有完整向量與負向向量，做得到 byte 對照。
3. **傳輸**（§7）：WebSocket、握手、重連、outbound spool、entitlements 快取。這一層要先有
   一個假的 relay 才測得動（§14）。
4. **27 種操作**（§10.3）分兩三波，照畫面效益排。
5. **推播**（`WebPush.swift` 858 行）最後。

每一波都要先讀這份文件，並且**先把 §11 的 gap 表看完**。

---

## 14. 本機測試用的 relay 與 api（調查結果）

見 `artifacts/report.md` 的「relay 與 api 能不能在這台 Mac 上跑起來」一節。摘要：兩邊的依賴都已經
裝好了（relay 有 `workerd-darwin-arm64` 與 wrangler，api 有 `mongodb-memory-server`，這台機器另外有
Homebrew 的 `mongod` 8.0），所以**不需要對外部署也不需要連正式環境**就能跑起來當測試用。

---

## 15. 第二階段實測到的事實（2026-09-18）

這一節只寫**實際跑出來的**：本機起了一份 relay（`wrangler dev`）與一份 api（Fastify + mongod
rs0），Go 端以一台新機器的身分註冊、握手、送 envelope、被斷線重連，並讓一個 viewer 把同一個
envelope 送兩次。每一條後面都標了看到它的地方。可重跑的方式寫在
`internal/adapters/cloud/live_test.go` 的檔頭。

### 15.1 本機環境：實際跑起來的指令

| 元件 | 指令 | 實測結果 |
|---|---|---|
| mongod | `mongod --replSet rs0 --dbpath … --port 27117 --bind_ip 127.0.0.1` 再 `rs.initiate` | `hello.setName=rs0`、`isWritablePrimary=true` |
| api | `MONGO_URI='mongodb://127.0.0.1:27117/?replicaSet=rs0&directConnection=true' HOST=127.0.0.1 PORT=8180 PUBLIC_URL=http://127.0.0.1:8180 node --experimental-strip-types src/index.ts` | `/readyz` → `{"ok":true,"mongo":"up"}` |
| relay | `wrangler dev --ip 127.0.0.1 --port 8787 --var API_BASE:http://127.0.0.1:8180 --var RELAY_SERVICE_TOKEN:dev-service-token` | `/v1/health` → `{"ok":true,"service":"clawdline-relay"}` |

三件第一階段沒寫、但**不知道就會卡住**的事：

1. **`API_BASE` 一定要覆寫**，第一階段的報告已經警告過；補充的是**`RELAY_SERVICE_TOKEN` 也一定
   要給**。relay 的 `#admitIdentity` 只有在 entitlements 抓成功並且提交過一次 revocation 之後
   才會把 `#revocationAuthority` 設成 `durable`，在那之前每一條 `/v1/connect` 都回
   `bad_gateway`。api 開發模式的 `SERVICE_TOKEN` 預設就是 `dev-service-token`。
2. **api 需要一顆外部的 replica set**，`npm run dev` 不會自己起 mongo，
   `mongodb-memory-server` 只有測試在用。
3. **`TOKEN_ISSUER` 預設等於 `API_BASE`**，而 api 的 `iss` 預設等於 `PUBLIC_URL`。兩邊不一致
   時 token 會被 relay 以 `unauthorized`（issuer is not ours）擋掉，所以起 api 時要把
   `PUBLIC_URL` 設成 relay 那個 `API_BASE`。

開發模式的登入是 stub OAuth：`GET /v1/auth/oauth/start` 直接 302 回 callback 並帶
`code=stub:demo`，把 `demo` 換成別的字串就是另一個帳號（free tier 一個帳號只能有一台機器，
所以每次重跑要換）。

### 15.2 這一階段實測到的行為

| 事實 | 怎麼看到的 |
|---|---|
| 握手就是 §7.1 寫的兩步，簽的字串是 `clawdline-challenge-v1\|<account>\|<device>\|<challenge>` | `TestLiveHandshakeAndDedupe` 的 step 1；challenge nonce 是標準 padded base64 的 32 bytes |
| `ready` 帶 `connected_at` 與 `token_expires_at`，兩個都是 epoch **毫秒** | 同上 |
| 一個 machine 送 `orch/<mid>` 會拿到 `ack status=delivered`，**`fanout=0`**（沒有 viewer 在聽也算 delivered） | step 2 |
| **relay 自己完全不做 (sender, seq) 去重** | 對本機 relay 連送三次逐位元組相同的 envelope，三次都拿到 `ack delivered`（`artifacts/step5-connect-publish.txt`）。relay 的 `PUBLISH_ERROR_TABLE` 裡也沒有 duplicate 這個碼 |
| 去重是**收端**的事：同一個 `(sender, seq)` 只認一次 | step 4：viewer 把同一份 envelope 送兩次，Mac 端 `inbound_total=1`、`inbound_dropped.replay=1` |
| outbound 這一側也只結一次：第二張收據是 `late_ignored`，不是錯誤也不是復活 | `artifacts/step5-connect-publish.txt` 的 `cloud receipt ignored … reason=already_settled` |
| 斷線之後照 §15.3 的階梯重連，序號**不重來** | `artifacts/step6-reconnect.txt`：relay 被殺掉後 305→458→987→1569→3807→6436 ms，回來之後 `generation=2`；重開的 process 從 seq 64 而不是 0 開始（fence） |
| 重連之後在途的 envelope 是**原封不動**重送 | `TestAnUnansweredEnvelopeIsResentByteForByte` 逐位元組比對重送前後 |

### 15.3 這一版照抄的常數（來源 `Sources/CloudTransport.swift:1711-1718`）

`initialBackoff` 250 ms、`maximumBackoff` 30 s、`backoffResetAfter` 30 s、`openingTimeout` 15 s、
`authenticationTimeout` 15 s、`receiveTimeout` 90 s、`keepaliveInterval` 30 s、`refreshAhead` 60 s。
jitter 是 `0.75 + unit*0.5`（±25%，對稱），而且**先睡再加倍**（:2112-2113），所以任何一次失敗之後
的第一次重試都是 250 ms 上下。退避只有在「剛死掉的那條連線活過 30 秒」時才歸零——不是「剛剛還有
流量」：一條接起來就被拒絕的連線很快，快不可以讀成健康。

### 15.4 刻意跟舊版不同的地方

| 項目 | 舊 Swift app | 這一版 | 為什麼 |
|---|---|---|---|
| outbound spool 的 row | 持久化在 `outbound-spool.json` | **記憶體** | 舊版開檔時本來就把每一個 row 都燒掉（reserved 沒封、sent 的單調時間跨 process 不能比、ready 的 `ts` 過了 relay 的 skew 窗），所以持久化的可觀察差別只有序號 |
| 序號 | 連號寫在 spool 檔裡 | **獨立的 fence 檔**（`outbound-sequence-v1.json`，一次前進 64） | 序號是唯一一個掉了會出事的東西：重開機從 0 開始，每個 viewer 的 replay 視窗都會安靜地拒收這台機器的全部快照 |
| command ledger | actor＋durable store＋fairness＋GC metrics | 記憶體、同樣的常數與狀態機 | 重開機會忘記 outcome，同一個 request_id 會再執行一次。這是真的缺口，不是疏漏 |
| ledger 的 GC | 只在 `reserve` 的 transaction 裡跑，而且 `retainedElapsedMilliseconds` 在正式版裡從來沒被推進過，所以 `expired` 分支實際上不會觸發 | 只用 `expiresAt`（wall clock）收，不要求單調時間的雙條件 | 舊版那個雙條件在正式版沒有推進者，等於 row 只會因為 revoke 或重開機消失。這一版少一個條件，會真的過期 |
| WebSocket | `URLSession` / NIO | 自己寫的 RFC 6455 client（`websocket.go`） | 這個 repo 沒有直接相依，client 半邊很小；permessage-deflate 不談判，server 若硬開就當錯誤 |
| 入站 roster | Keychain 的配對裝置表 | **還沒有**：`PublicKeyFor` 目前一定回 false | 配對是下一波（§8.3）。所以現在每一個入站 envelope 都會記成 `unknown_sender` 而不是被放行 |

### 15.5 這一階段**沒有**做的

27 種操作的內容、推播、配對、entitlements 快取、hosted console 的畫面、把 transport 接進 daemon
的 `serve`（現在只有 `clawdline cloud connect` 會開線）、`/v1/cloud/status` 這種 HTTP 路由
（daemon 還沒有一條活的線可以報告）。

## 16. 第四階段：接線與 hosted console 端到端（2026-09-18 實測）

第二階段把線接起來但沒人開它，第三階段把 27 種操作接到本機路由但沒有傳輸。這一階段把兩半接進
`clawdline serve`，並且**用 app.clawdline.com 的正式前端 bytes**（`tools/build-web-app.py` 的產出，
只是指向本機 api 與 relay）真的操作了這台 Mac。

### 16.1 接線長什麼樣

`cmd/clawdline/main.go` 的 `startCloudLine` 在 `serve` 起來之後開一條 `internal/transport/cloud`
的 `Link`：

- `Open` 先讀設定。`cloud_enabled` 不是 `true` 就**什麼都不開**——不開金鑰庫、不讀身分、不連線，
  `/v1/cloud/status` 回 `enabled:false, state:"off"`。設定壞掉（relay URL 打錯）是**拒絕**，
  daemon 照樣起來，理由寫在 status 的 `last_error`。
- `Run` 開兩條 goroutine：transport 顧 socket，service 顧回答。另外兩條小的：roster 定時重讀、
  快照發佈器。
- 兩個開關，不是一個：`cloud_enabled` 是線通不通，`cloud_commands` 是 viewer 能不能動這台 Mac。
  第二個**每次請求重讀**檔案，所以關掉的當下連在途的請求也一起擋。CLI 是 `clawdline cloud commands on|off`。

### 16.2 入站 roster（§15.4 那個缺口補上了）

`internal/adapters/cloud/roster.go` 用機器憑證讀 `GET /v1/devices`（控制平面的帳號裝置表），
把每一列的 `public_key` 釘成 `PublicKeyFor`。核准發生在帳號那一端，這台機器沒有第二票。
`revoked_at` 有值的不算；讀不到**不等於空的**（`roster_unreadable`，`Readable()` 分得出來）；
一列壞掉不會鎖住其他三列；讀失敗保留上一次的好答案。

裝置的 `caps` 也真的在用：`cloud_commands` 開著，還要這個 sender 的 roster 列上有 `send_prompt`
或 `start_session`，才會讓一個 command 走到不可回頭那一點。

### 16.3 狀態路由與設定頁

`GET /v1/cloud/status`，**只有這台 Mac 自己的 token 讀得到**（跟 `/v1/diagnostics` 同一條規則：
它會說出 relay、帳號、機器指紋與每一台已登記的 viewer）。沒有 token 是 401，orchestrator token
也是 401。React 設定頁的「遠端」分頁多了一張 Cloud 狀態卡，5 秒一次、只在那一頁開著時才問。

**刻意的缺口**：這個形狀**沒有**進 `api/v1/` 契約，因此設定頁是自己寫型別
（`web/console/src/pages/settings/cloud.ts`）。理由是當時另一個進行中的 task 同時 claim 了 `api/v1/`，
重生契約會改到 218 個型別的產出檔並跟它撞在一起。補契約是待辦。

### 16.4 端到端實測：hosted console 真的看得到這台 Mac

環境全部在本機，**沒有碰正式環境，也沒有碰使用者的 Cloud 帳號**：mongod 27117、
Cloud 服務的 API（`api/`）複本 8180、relay 的複本（`wrangler dev`）8787，前面一層自簽 TLS 的
Node 伺服器把三者收在同一個 origin `https://127.0.0.1:8443`（console 的 build 宣告強制
`https` 與 `wss`，`net/cloud-boot.js:86-91`）。console 是
`tools/build-web-app.py --app-origin/--api-origin/--relay-url` 指向本機的產出，**一個位元組都沒改**。

量到的：

| 步驟 | 結果 |
|---|---|
| `serve` 自己連上 relay | `cloud ready account=… machine=… generation=1`，不必再打 `cloud connect` |
| api／relay 重啟後 | `relay_unauthorized` → 退避 → `connects=3`，線自己回來 |
| console 的機器卡 | 「已連上」、`Mac 電腦 · clawdline-go-e2e` |
| console 的 session 清單 | 這台 Mac 的 11 個 session 全部列出來（id、assistant、cwd、tty、狀態） |
| 打開一個 session | transcript 在 console 裡展開，內容與本機一致 |
| 從 console 送訊息 | 送進自己開的可拋棄 session，終端機收到、assistant 開始跑，訊息回到 console 的 transcript |

### 16.5 這一階段學到的三件事（不接就看不到東西）

1. **機器清單不是 API 路由。** `cloud-client.js` 的 `machines()` 是純本地計算，來源是它解密過的
   `orch/` 與 `s/` envelope。只會回答、不會主動發佈的機器，在 console 上是**不存在**的。
2. **session 清單是機器自己發佈的快照**：`s/<machine>/<session>` 一列一個 session，
   `s/<machine>/__clawdline_inventory_v1__` 是清單標記（`{"inventory":{"version":1,"sessions":[…]}}`，
   `features` 放在**旁邊**不能放裡面，多一個 key 整包 `bad_payload`）。
   標記是**刪除屏障**：viewer 會丟掉它沒點名的每一列，所以只有 authoritative 的 scan 能發。
   這個 daemon 的 `/v1/sessions` 目前 `scan.complete` 一直是 false，所以標記現在不會發，
   清單靠 row 自己撐（實測有效），代價是消失的 session 不會被 prune。
3. **descriptor 要自己說 `commands`。** 沒有 `commands` 陣列時 console 用 `platform` 猜，
   猜不到的平台**一個字都不送**——不是送了被拒絕，是連請求都不發。所以
   `internal/transport/cloud/publish.go` 直接把 `cloudops.Implemented()` 放進去。
   同理 `features` 是算出來的不是抄的：`sessions.snapshot` 與 `board.items` 這個 daemon 都不會答，
   所以現在是空的，不發這個 key。（2026-09-25：兩個都答了，`features` 現在列出兩個，見 §16.6。）

### 16.6 還沒做的（不可以當作通過）

- **配對沒做。** Go 版沒有機器半邊的 QR／四階段 handover，所以瀏覽器拿不到這台機器的 content
  key。實測時是**用 devtools 把已完成配對會寫的那四筆直接種進 IndexedDB**
  （`clawdline.machine-master*` / `-sender` / `-binding`）。因此這一段證明的是傳輸與操作那一層，
  **不是配對那一層**。配對仍是下一波。
- `sessions.snapshot` 這個字沒有接（發佈器自己每 5 秒掃）。**2026-09-25 接上了**：很久沒開的頁面
  重新整理後 Session 列表不齊，原因是 relay 的 object 被 evict 後不重播任何 row，而沒變的 row 要等
  240 秒的 heartbeat 才重發；`Publisher.Seen` 只對「這條 socket 上沒聽過的裝置」重發，同一支手機
  幾小時後回來不算新裝置。copied client 本來就會在每次非接手的連線時問這個字，只是 marker 的
  `features` 沒列，所以從來沒問過。現在 `Publisher.Snapshot`（`internal/transport/cloud/publish.go`）
  回答它：清掉略過記憶、依序重發 descriptor、每一列（含來源讀不到而被保留的 id，用最後一次發出的
  那份 row）、清單標記，之後才回 `{"sessions":[ids],"complete":bool}`。每台機器每 5 秒最多一次，
  最多 64 個等待者，第 65 個回 `429 cloud_read_busy`；線路斷掉時等待者收到 `cloud_reconnecting`。
  它不經 service 的序列迴圈等待（`Link.runService`），所以不會讓其他請求排在它後面。
- `t/` 上沒有主動推播；transcript 的即時更新在舊版是靠 row 上的 `transcript_signature` 變動來觸發，
  這個 daemon 還沒有算那個簽章，所以 console 要靠自己的重讀節奏。
- 正式環境（`relay.clawdline.com`／`api.clawdline.com`）一個位元組都沒連過。
- entitlements、推播、`ctlr/` 回覆軌、交接通道都沒動。

---

## 17. 第五階段：配對的機器半邊（2026-09-18 實測）

§8.3 當時寫「這一波不做，規格先寫下來」，§16.6 又記了一次「配對沒做，是用 devtools 種金鑰」。
這一節把那個洞補起來：**Go daemon 現在自己產生交接資料、自己封裝這台機器的 content key、
自己釘住瀏覽器的公鑰**（`internal/transport/cloud/pairing.go:335-365`），
第四階段那三件事（看得到 session 清單、打得開對話、送得進訊息）在**沒有任何 devtools**
的前提下重做了一遍。

### 17.1 走的是哪一條：三呼叫的相容路徑，不是四階段

PROTOCOL.md 同時有兩條：

| | 路徑 | 這一版 |
|---|---|---|
| 相容 | `pairing/invitations/start` ＋ `/accept` ＋ `/poll`，然後 `pairing/start` ＋ `/complete` ＋ `/claim` | **做了** |
| 嚴格身分 | `pairing/identity/start` ＋ `/phases/:phase` ＋ `/poll`，四階段 `offer → grant → activate → confirm` | 沒做 |

選相容那條的理由只有一個，而且是可驗證的：**部署中的 hosted console 走的就是它**。
`Resources/web/app/js/net/cloud-boot.js:485-600` 只呼叫 `pairing/start`、`invitations/accept`、
`pairing/claim`；`cloud-pairing.js` 的 `openPairingHandover` 只接受 `phase == "grant"` 的
七成員 wrapper。做四階段等於做一個今天沒有對手的東西。四階段仍是之後的事，
它的向量（`pairing_handover`）已經是這一版逐位元對照的那一份。

### 17.2 一次配對，機器這一半

```
1. 機器抽 32 bytes secret  →  只把 SHA-256 給 cloud（invitations/start）
                           →  把 secret 放在 fragment 裡給人：
                              https://<app-origin>/#pair=<base64url(canonical JSON)>
2. 瀏覽器（已登入同一個帳號）讀 fragment → 跟 cloud 要 pairing_id 與 claim_nonce
                           → 造 offer（自己的 Ed25519 公鑰＋新的 X25519 公鑰）
                           → 用 QR secret 封起來丟回 invitations/accept
3. 機器 invitations/poll 拿到密文 → 用自己的 secret 解開 → 得到 offer fragment
4. 機器 X25519(自己的臨時私鑰, offer 的臨時公鑰) → HKDF → grant phase key
                           → 封 handover（account_id、machine_id、機器簽章公鑰＋指紋、
                             key_id、master_secret）→ pairing/complete
5. 機器比對 complete 回來的 fingerprint 與 offer 裡的 viewer_fingerprint
6. 相符才**釘住** viewer 的公鑰
7. 瀏覽器 pairing/claim 取走那一份，一次，記錄即毀
```

cloud 全程看到的是：一個雜湊、兩段它讀不懂的 bytes、兩個 device id。

**第 5 步不是顯示，是檢查。** 控制面在瀏覽器呼叫 `pairing/start` 的時候就記下了它的指紋；
如果它跟這台 Mac 剛剛封裝的那份 offer 不一致，那份 offer 就不是開啟這個 pairing 的那一份——
這正是「兩個螢幕比對指紋」的人看不到的那種替換。

**第 6 步在第 5 步之後，不在之前。** 釘住等於允許那個瀏覽器驅動這台 Mac；沒收到金鑰的瀏覽器
本來就發不出指令，所以先釘只會在每一次失敗的交付後面留下一個被釘住的 viewer。

### 17.3 信任的根從雲端搬回本機

第四階段的 `roster.go` 是讀 `GET /v1/devices` 拿 viewer 公鑰。那是**雲端告訴機器該相信誰**，
跟 PROTOCOL.md §3 講的相反。`internal/adapters/cloud/pinned.go` 是本機那一份：

```
paired-devices-v1.json  （0600，放在 cloudkeys 目錄，裡面只有公鑰、id、時間）
```

`Link.publicKeyFor` 的順序是三條，順序本身就是規格：

1. **這台 Mac 撤銷過的，一律拒絕**，其他任何來源都推翻不了它。
2. **釘住的優先**：那把公鑰是從這台 Mac 自己解開的 offer 裡拿出來的，雲端沒有看過明文，
   所以它無法替換。
3. **roster 是後備**，給在這個檔案存在之前就配對好的 viewer。它是比較弱的答案，
   所以設定頁會標出哪幾列是釘住的、哪幾列只是在帳號清單上。

**讀不出來的釘住檔案不是空的**：整個 daemon 在那個狀態下不接受任何 sender，因為「掉回去讀雲端」
正是這些釘子要防的那個替換。

實測（`artifacts/revoke.txt`）：撤銷之後，那個瀏覽器送出的訊息**沒有到**，daemon 記成
`inbound_dropped.unknown_sender`，而同一時間 `GET /v1/devices` 仍然把它列為 `revoked_at: null` ——
本機的撤銷贏過雲端的清單，這一句是量到的，不是設計意圖。

### 17.4 金鑰輪替會把線拉下來再接回去

relay 是拿 device token 裡的 `pk` 去驗每一個 envelope 的簽章。所以換簽章金鑰**不能**塞進正在
跑的 transport：那條連線的後半段會用一把 relay 剛剛停止接受的金鑰去簽。`Link.Run` 因此是一個
迴圈——輪替把內層 context 取消，`wire()` 重新建一次，線再上來。spool 跟著重建，這是故意的：
舊金鑰簽的 envelope 送出去也是被拒，還會白白花掉一個序號。

順序是「先問控制面現在的 epoch → 鑄新的 → CAS 交換 → 成功才寫檔案」。先寫檔案的話，
一次被拒的交換會留下一台持有帳號沒聽過的金鑰的機器，而那看起來跟被撤銷一模一樣。

**代價要先講清楚**：每一個釘住舊金鑰的瀏覽器都會停止能驗證這台 Mac，必須重新配對。所以
`POST /v1/cloud/keys/rotate` 沒有 `{"confirm": true}` 就拒絕，`GET` 會先回答「會弄壞哪幾個瀏覽器」，
CLI 也是先把那幾行印出來再問。實測（`artifacts/rotation.txt`）：
`AC4B-H7AK-M4O6-L3JT → 4JU3-XJLG-EGL6-BEG5`，key epoch 1→2，線在 18 秒內自己回來，
而那個瀏覽器的 console 說 `This browser cannot decrypt Sessions` / `未配對`——**代價也是量到的**。

### 17.5 五條路由，全部只認這台 Mac 自己的 token

| | |
|---|---|
| `GET/POST/DELETE /v1/cloud/pairing` | 讀、產生、停止等待 |
| `POST /v1/cloud/pairing/offer` | 桌機路徑：把瀏覽器畫面上那串配對碼貼進來 |
| `POST /v1/cloud/devices/revoke` | 把一個瀏覽器趕出這台 Mac |
| `GET/POST /v1/cloud/keys/rotate` | 先看代價，再換 |

`POST /v1/cloud/pairing` 回答的連結，fragment 裡帶著把這台機器的 content key 交出去的一次性 secret
（`internal/transport/cloud/pairing.go:143-176,335-361`）。
通道進來的手機或 Cloud viewer 如果構得到這條，就等於一個「能讀 session」的人可以自己鑄一個
完整配對的瀏覽器出來。所以是 `requireLocal`，跟 `/v1/cloud/status` 同一條規矩。

CLI 是 `clawdline cloud pair|devices|revoke|rotate`，**都是打 daemon 的這幾條路由**，
不是自己讀檔案：配對只有一份在途狀態，兩個行程各拿一份等於兩個碼在兩個螢幕上。

### 17.6 新設定鍵：`cloud_app_origin`

預設 `https://app.clawdline.com`（`cloud-onboarding.js` 的 `CLOUD_APP_ORIGIN`）。
它跟 api、relay 分開，因為配對連結是這台 Mac 唯一交到**人**手上的字串——他要在瀏覽器裡打開它，
所以它得指那個人會看到的站，不是後面的控制面。驗證比照 `cloud_api_base`，另外拒絕路徑、
query 與 fragment：那些要嘛會被丟掉，要嘛會把一次性 secret 帶到不該去的地方。

### 17.7 這一階段沒有做也沒有量的

- **四階段嚴格身分配對**沒做（§17.1）。`identity_epoch`／`capability_epoch`／`jwks_generation`
  這一組 claim 也還沒有進 Go 端的判斷。
- **正式環境**一個位元組都沒連過。
- **QR 圖**沒有畫。產生的是同樣內容的連結（`https://<app-origin>/#pair=<fragment>`），
  設定頁顯示它並提供複製；手機掃 QR 那條路徑靠的是同一個 fragment，所以畫圖是純顯示層的補完。
- **`account-master-secret` 的輪替**沒做。輪替的是簽章金鑰；換這台機器的 content key 要讓與它配對的 viewer 重新
  拿一次，PROTOCOL.md 自己也說 content-key rotation 是 lazy 的。
- **多台機器**沒測。一台 Mac、三次配對、兩把瀏覽器金鑰。
- **重開機後**沒測（spool 與 ledger 仍在記憶體，第二階段的已知缺口沒有變）。

## 18. D1：正式連線前的最後一步（2026-09-19）

操作手冊在 `docs/cloud-cutover.md`。這一節只寫程式這一邊的規格。**正式環境仍然一個位元組都沒連過**，
下面每一個「正式端會回什麼」都抄自 Cloud 服務的原始碼，不是量的。

### 18.1 失敗的名字：一個對照，七個類別

`FailureCode` 仍是封閉詞彙（§15 起），這一波加了：`api_<code>`／`api_http_<status>`（控制面的拒絕，
`APIError`）、`incompatible`、`no_identity`、`identity_other_environment`、`login_denied`、`login_expired`、
`login_timeout`、`invalid_token`、`switched_off`、`unreachable`、`tls_untrusted`、`connection_timeout`。
從對面抄來的字只收 ≤64 bytes 的 snake_case（`maxFailureCodeBytes`，已登記），其餘變成 `…_unrecognized`。

每個字屬於一個類別，**依「接下來該做什麼」分，不是依哪一端說不**（`internal/adapters/cloud/failure.go` 的
`KindOfCode`，唯一的一張對照表；CLI、狀態路由、設定頁都讀它）：

| 類別 | 代表字 | 這台 Mac 的行為 |
|---|---|---|
| `not_signed_in` | `no_identity`、`identity_other_environment`、`api_no_session`、`api_http_401` | 線不起來或停下 |
| `device_not_approved` | `login_*`、`relay_forbidden`、`relay_*revoked`、`closed_4403` | 停下（`IsTerminalAuthorization` 不變） |
| `entitlement` | `relay_over_capacity`、`relay_rate_limited`、`closed_4429`、`api_machine_limit_reached` | 退避重試，token 沿用 |
| `version_mismatch` | `incompatible`、`api_not_found`、`upgrade_refused_{400,404,405,410,415,426}`、`relay_bad_request` | 退避重試 |
| `relay_refused` | `relay_unauthorized`、`relay_token_superseded`、`identity_binding`、`upgrade_refused_{401,403}` | 退避重試；in-band `unauthorized` 每次重拿 token |
| `unavailable` | `unreachable`、`tls_untrusted`、`connection_failed`、`relay_bad_gateway`、`api_internal` | 退避重試 |
| `unknown` | 表裡沒有的 `api_` 字 | 照原樣顯示，不猜 |

`switched_off` 是事件不是失敗，對照為空。（`token_rotation` 已移除：2026-10-06 起 relay 不再因 device token
到期關掉已登入的 socket，只有撤銷會；machine 也不再於到期前一分鐘自己關 socket 換 token，`refreshAhead`
只剩「撥號時拿一張至少還有 60 s 的 token」這個用途。）**重試政策一行都沒改**（仍是 Swift 的，§15.3、
`CloudTransport.swift:2238-2240`）；doc.go 原本說 `bad_request` 會停線，與程式不符，已改成照程式寫。

版本不合原本不存在：握手的 `v≠1`、context 不對、ready 的 `v≠1`、控制面回非 JSON、poll 回沒寫過的狀態，
現在都包 `ErrIncompatible`。正式端的 api 與 relay **都沒有版本協商路由**（grep 過 `api/src`、`relay/src`），
所以版本不合只能從這些形狀推出來。

### 18.2 控制面的拒絕有型別了

`AccountClient.post` 對 ≥400 的回答讀 `{"error":{"code","message"}}`（`api/src/server.ts` 的 error handler）成 `APIError`；
讀不出來的（代理的 HTML、空的 404）仍是 `APIError`，只是沒有 code。401／403 經 `Unwrap` 仍是
`ErrUnauthorized`，所以 transport 停線的判斷不變。控制面對機器會回的碼（`routes/auth.ts`、`routes/tokens.ts`、
`routes/guards.ts`）：`bad_machine`、`bad_public_key`、`bad_field`、`no_session`（撤銷或不認得的機器憑證，
`machinePrincipal` 找不到就落到 `requireSession`）、`not_found`、`internal`。`machine_limit_reached` 只會出現在
**瀏覽器的核准頁**（`approveDeviceCode` → `assertMachineSlotAvailable`），Mac 那邊看到的是一直 pending 到過期。

### 18.3 登入的輪詢搬進 adapter

`AccountClient.WaitForApproval` 取代 CLI 裡的迴圈，照伺服器的節奏（`interval`、`slow_down` 的
`retry_after_seconds`），結果是具名的 `ErrLoginDenied`／`ErrLoginExpired`／`ErrLoginTimeout`。
`cloud login` 成功後**用一次**機器憑證換 device token（`verified …`），證明核准生效；不連 relay。

### 18.4 假扮正式端的測試夾具

`internal/adapters/cloud/production_test.go`：自己簽一張 CA，簽 `api.clawdline.com` 與 `relay.clawdline.com`
的憑證，起兩個本機 TLS 伺服器。**設定檔是空的**（所以走的是預設的正式端點），HTTP client 與 WebSocket
撥號器都換成只認這兩個 `host:443` 的撥號器，其他位址一律拒絕並記錄；每個測試收尾時斷言紀錄裡沒有別的位址，
`TestTheHarnessRefusesEveryOtherAddress` 是那個斷言會紅的對照。為此 `DialOptions` 加了 `NetDial`，
transport 的 `Options` 加了 `TLSConfig`／`NetDial`，daemon 裡都是 nil。

驗到的：預設設定、Host 與 SNI 都是正式名稱、TLS 驗證是開的（不信任的 CA → `tls_untrusted`，請求沒送出去），
以及登入 7 種結局、連線 8 種拒絕各自的名字與對線的效果。`standin_test.go` 是同一組回答的 loopback 版，
給人手動跑真的 CLI（它不是 TLS，TLS 那一半只在上面那個檔案裡）。

### 18.5 還沒有的

- 正式端的任何回答（所以表裡「正式端會回什麼」是讀原始碼的推論）。
- 帳號層刪除一台 Mac 的介面：api 有 `DELETE /v1/machines/:id`，但只收瀏覽器 session，hosted console 沒有按鈕。
- `cloud_enabled` 仍只在 daemon 啟動時讀；打開開關要重啟 daemon。
- 設定頁沒有直接畫 `last_error_kind`；它顯示的 `last_error` 字串以類別開頭，所以不改前端也看得到。

## 19. 配對是讀取能力，不是舊查詢的備忘錄（2026-09-21）

機器列曾經能同時持有兩個互相矛盾的事實：`viewerVerified` 記著這個瀏覽器已經驗過簽章、解開這台機器的
`orch/` 信封，`unpairedMachines` 卻還留著較早一次「當時找不到 pairing」的負面答案。
`cloud-client.js` 的 `_machineRows()` 原本讓後者先贏，於是同一列可以畫出剛解密得到的上次看到時間與
session 數，最後卻說「尚未與這個瀏覽器配對」。

這不是機器 roster 或 control plane 能回答的問題。`paired-devices-v1.json` 只證明機器收下瀏覽器公鑰；
control plane 保存的是帳號上的 machine route 與一次性的 pairing handover，不保存一個可供
`client.machines()` 查詢的「這個瀏覽器已配對這台機器」欄位。該欄位完全由瀏覽器本地推導：key store
查詢、失敗記憶，以及成功驗簽並解密的信封。hosted console 的 typed boundary 用 `viewerVerified` 校正
舊的負面記憶，並清掉該 client 的 pairing miss。

但是一個從沒解開過信封的瀏覽器還缺第一步：machine row 已經因先前的失敗被算成 `not_paired`，而舊的
校正只承認 `viewerVerified`，所以剛剛成功解開並持久化的 pairing handover 沒有進入列的計算。現在這份
經過帶外 offer、X25519 phase key 與 AEAD 驗證的 handover 也是 bootstrap capability；它一成功，該
machine id 就立刻校正成 `paired` 並可選，連線同時重建，讓 relay 重播的 `orch/` 信封接手成為後續證據。
沒有 handover 或解密證據的 account row 仍是 `not_paired`，不會因為帳號認得它或機器 roster 有一列就
猜成 paired。

瀏覽器先出碼的路徑另有一個不同的斷點。`clawdline cloud pair -offer …` 做完時，機器只把一次性的
handover 放進 control plane；瀏覽器還得拿著產生 offer 時那把 X25519 私鑰呼叫 `pairing/claim`，解開後
才會把 machine master key 與 sender key 寫進永久 key store。先前那把私鑰只活在等待卡的 Promise 裡，
關卡片或重載就遺失；所以機器可以印出 paired，而瀏覽器永遠收不到自己的那一半。

現在 offer **顯示以前**，待領取資料與 non-extractable X25519 `CryptoKey` 會先以 structured clone 寫進
IndexedDB。卡片關掉只停止卡片本身的等待，背景會繼續 claim；頁面重載後，登入 session 一恢復也會讀出
同一筆繼續 claim。成功後才刪除；具名的 terminal pairing 答案或過期會刪除；沒有型別的網路中斷保留到
下一次頁面／連線再試。畫面仍在等待時的 claim 也走同一個成功出口：記下 bootstrap capability 並自動
重連，不再要求人按「重新載入」才讓機器列得知結果。這沒有把私鑰變成可匯出的 bytes，也沒有讓 cloud
看到它；未持有 handover 所寫金鑰的登入裝置即使收到同一份 `orch/` ciphertext，也無法驗簽解密。
