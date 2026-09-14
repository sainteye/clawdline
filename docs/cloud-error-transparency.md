> **這是一份設計，不是現況說明。** 撰寫於 2026-09-13。量測對象：本 repo commit `517480fb` 的工作樹、
> `~/Library/Logs/Clawdline.log`（08-31 起，659,523 行）、私有 repo `~/code/clawdline-cloud` 的 `bdb5b84`。
> 實作一開始，行號就會漂移；行號的用途是記下每一條路徑當時在哪裡決定，不是給讀者對照現在的檔案。
> 拍板結果記在 §8；§3–§7 裡寫著「建議」的地方，§8 已經定案。

# Cloud 錯誤與認證透明化

讀者：要拍板這份設計的人，以及之後實作、除錯 Clawdline Cloud（`app.clawdline.com` ↔ relay ↔ Mac）的 agent。

## 0. 要解決的問題

一筆 Cloud 指令從手機走到 Mac，中間要經過七層（§1）。**每一層都有拒絕時什麼都不說的路徑**；
就算有說，使用者多半只看到一句英文，或一句「這個開不起來。」。除錯的人只能拿時間戳去 Mac log 對。

今天量到的三個例子（量法在最後一欄）：

| 事件 | 使用者看到 | Mac log 寫了什麼 | 量法 |
|---|---|---|---|
| 09-12 21:25 到 09-13 12:18，Mac 丟掉 790 筆入站信封 | 沒反應 | `CloudTransport dropped inbound envelope reason=invalid count=N`，沒有 sender、seq、key_id | `grep -a -c 'dropped inbound envelope reason=invalid'` ＝ 790 行。`count=` 是**每條連線各自累計、重連就歸零**的數字，把它加總會得到 258,086，是錯的讀法 |
| 09-13 14:56–15:01，6 筆 `command_clock_uncertain` | 語音顯示 `The command durability boundary is unavailable.` | `cloud: command refused 503 command_clock_uncertain`，沒有指令種類、sender、request | 6 筆都落在 `reconnect waiting reason=token_rotation` 之後 2–42 秒 |
| 09-13 15:06–15:09，13 筆 `reason=replay` | 沒反應 | 同第一列的格式 | 同一台 device 在同一個 Chrome profile 開了第二個分頁 |

**目標：** 任何一次「按了沒反應／卡住／被拒」，畫面上都說得出「哪一層、什麼原因、哪一筆」；Mac 上也有一個固定位置，
讀得到同一筆、同一個代號。

## 1. 一筆指令走過的七層

| # | `layer` | 在哪裡 | 會怎麼失敗 |
|---|---|---|---|
| 1 | `browser` | `Resources/web/app/js/net/cloud-client.js` | 還沒連上、沒有裝置金鑰、序號儲存壞掉、同步 throw |
| 2 | `relay` | `~/code/clawdline-cloud/relay` | token 過期、被撤銷或被取代；簽章不符；容量；Mac 不在線（`machine_offline`） |
| 3 | `mac_transport` | `CloudTransport.acceptInbound` | 信封格式、sender 未配對、key_id 不符、簽章、解密、replay、入站佇列滿 |
| 4 | `mac_preflight` | `CloudAppBridge.consume`／`serveRead` | 寫入開關關著、指令格式錯、未知指令、dispatch 未釘選 |
| 5 | `mac_ledger` | `routeDurably` → `CloudCommandLedger` | 時鐘不確定、期限已過、冪等衝突、容量、roster 讀不到 |
| 6 | `mac_route` | `RemoteServer` 的 route、`CloudLocalRoute` | route 自己的錯（`not_found`、`busy`、`terminal_io_failed`…） |
| 7 | `mac_reply` | `publishJSONAnswer` → 出站 spool | 已經執行了，但回覆送不出去，或在 spool 裡過期 |

任務書點名的四層（relay、入站丟棄、command ledger、router）是第 2、3、5、6 層。盤點時發現第 1、4、7 層也一樣會靜默，所以一起納入。

## 2. 錯誤契約

### 2.1 同一筆指令的代號：`ref`

三邊都看得到的只有信封外層的 `(sender, seq)`：

- relay 看得到。它的 `ack` 與 `publish_error` 本來就是用 `(ch, seq)` 對應請求。
- Mac 在**解密之前**就看得到。所以連 key_id 不符、解不開的那一筆，也能指名。
- 瀏覽器是配號的人。

`request`（瀏覽器用 `crypto.randomUUID()` 產生，放在密文裡）只有 Mac 解密後才看得到，relay 永遠看不到。

**契約：**

- 跨層代號 `ref` ＝ `sender` ＋ `seq`。畫面顯示成 `f052dcb8·1234`，也就是 sender 去掉 `web_` 前綴後的前 8 碼、一個 `·`、再接 seq。
- `request` 是第二把鍵。Mac 解密之後的每一行 log、每一個回覆都帶著它；最近指令紀錄（§4.1）同時記 `seq` 與 `request`。
- **不在信封外層加新的 request id。** 信封固定 10 個鍵、受簽章涵蓋，relay 與 Mac 都嚴格驗形狀，加欄位等於協定改版。
  而 `(sender, seq)` 本來就應該唯一；會重複的時候（撞號），那正是要被指名的錯誤（§6.3）。

### 2.2 一個拒絕帶什麼

瀏覽器內部把所有失敗統一成一個形狀 `CloudFailure`。Mac 回覆裡的 `error` 物件、relay 的 frame，都先正規化成它：

```json
{
  "layer": "mac_ledger",
  "code": "command_clock_uncertain",
  "ref": { "sender": "web_f052dcb8-…", "seq": 1234, "request": "3bff2f2b-…" },
  "status": 503,
  "retryable": true,
  "detail": { "reason": "stability_period_incomplete", "clears_in_ms": 41000 }
}
```

- `code`：1–64 字元的小寫 snake_case，保持穩定。**畫面上的文字只由 `code` 決定**，永遠不顯示 `message`。
- `layer`：§1 的七個值之一，是封閉集合。
- `detail`：每個 code 各自列出允許的欄位（白名單），其他一律丟掉。不放路徑、標題或逐字稿。
- `retryable`：由瀏覽器依 code 查表決定，不照伺服器說的。沿用 relay `publishErrorDisposition` 的原則：
  未知代碼一律當成終止，未來的伺服器不能讓舊的 client 陷入重試。

Mac 回覆的 `error` 物件新增三個欄位：`layer`、`seq`、`detail`（`request` 已經在 `read: "action:<request>"` 裡）。
`message` 仍然保留，給還沒更新的舊 console 用；新版 console 不讀它。

### 2.3 Mac log 只用一種格式

所有 Cloud 拒絕與丟棄都改寫成同一種行：

```
cloud: refusal layer=mac_ledger code=command_clock_uncertain sender=web_f052dcb8-… seq=1234 request=3bff2f2b-… type=voice session=%25690 status=503 reply=published detail.reason=stability_period_incomplete
```

- 解密前就丟掉的那種，沒有 `request`、`type`、`session`，這三欄寫 `-`；另外多出 `key_id=`／`expected_key_id=` 或 `highest_seq=`。
- `reply=` 記錄 sender 有沒有收到東西：`published`（收到回覆）｜`notice`（只進狀態摘要，見 §4.2）｜`silent:<原因>`。
  **只要還看得到 `silent:` 的行，就代表那條路這份設計還沒蓋到。**
- 這一行取代今天的 `cloud: command refused <status> <code>`、`CloudTransport dropped inbound envelope reason=… count=…`
  與 `cloud: command ingress refused …`。`count=` 拿掉，計數改由狀態快照提供（§4.1），而且會寫明從什麼時候開始算。

### 2.4 一筆指令的步驟

任務書要求分清楚 accepted、executed、delivered、observed、acknowledged。對一筆 Cloud 指令來說，每一步由誰、憑什麼證明：

| 步驟 | 誰看得到 | 證據 |
|---|---|---|
| `sealed` | 瀏覽器 | 配到 seq、簽好信封、寫進 socket |
| `relayed`／`machine_offline` | 瀏覽器 | relay `ack` 的 `status` 與 `fanout` |
| `accepted`／`dropped` | Mac | 解密、序號、入站佇列都通過；或是丟棄的原因 |
| `executed` | Mac | 帳本 row 變成 `completed`，帶 outcome |
| `delivered`／`undeliverable` | Mac | 回覆在出站 spool 收到 relay 的 receipt；或是送不出去、過期 |
| `observed` | 瀏覽器 | 回覆對上了等待中的請求 |
| `acknowledged` | 瀏覽器 | 結果或錯誤已經畫在畫面上 |

`relayed` 只證明 relay 把信封寫進了 Mac 的 socket。`clawdline-cloud/contracts/cloud/v1/vocabularies.json` 的
`relay_fanout` 明寫它不證明 Mac 收下了。所以一筆指令 `relayed` 之後 60 秒沒有回覆，畫面要說「Mac 沒有回應」，
並附上狀態快照裡同一個 `ref` 走到哪一步（如果有的話）。

## 3. 靜默路徑盤點與處置

欄位說明：**今天**是使用者現在看到什麼，**之後**是新的呈現方式，**測試**對應 §7 的編號。

### 3.1 瀏覽器（`browser`）

| ID | 路徑（出處） | 今天 | 之後 | 測試 |
|---|---|---|---|---|
| B1 | relay 回 `ack status=machine_offline`，但 `cloud-client.js:376-378` 只 `_emit`，沒人等 | 60 秒後才出現「Mac 沒有回應這個讀取」 | 立刻失敗：`relay · machine_offline`，「Mac 目前不在線上」 | T-B1 |
| B2 | relay 回 `publish_error`，`cloud-client.js:383` 沒有這個分支，落到 `bad_frame` | 60 秒後逾時，code 遺失 | 用 `(ch, seq)` 對上請求，立刻失敗：`relay · <code>`，帶 `field` | T-B2 |
| B3 | relay 送 `error` frame 後關閉連線；`onclose` 從來沒讀 close code（`cloud-client.js:292-312`） | 連線狀態變 offline，原因不明 | close code 與原因記進連線狀態；之後的請求失敗時帶 `relay · <code>` | T-B3 |
| B4 | transport 同步 throw：大約 20 個公開方法會同步丟例外（`_place`、`_onlyMachine`、`_sessionIdentity`、`_read`、`_send`、`subscribe` 等），其中至少 12 個 UI 呼叫點沒有接住 | 只出現在瀏覽器 console；`sending`、`pressing` 這類旗標卡住不放 | 契約：CloudClient 的每個公開方法一律回 Promise，不准同步 throw。守衛會列舉 prototype 上的全部方法逐一檢查（§5） | T-B4 |
| B5 | UI 直接顯示原始 `e.message`（`composer.js`、`detail-actions.js`、`voice.js` 的 `complain()`、`shell-panel.js`、`agent.js`、`snippets.js` 等約 20 處）；或是把 code 丟掉、顯示通用句（`start.js` 的 `why()`、`git-panel.js`、`terminal.js`） | 一句英文，或「這個開不起來。」 | 共用 `describeFailure(error)`：已知 code 顯示在地化句子，未知 code 顯示「沒有成功（`code` · `ref`）」。錯誤那一行可以點，點下去打開 §4.3 的狀態 sheet | T-B5 |
| B6 | `readAnswer` 只保留 `code`、`message`、`status`（`cloud-client.js:156-171`），`reason`、`app`、`lost`、`retry_after` 都被丟掉 | Cloud 上分不出 `no_model` 與 `no_binary`，`{app}` 也填不進句子 | 依 code 的白名單保留 `detail` | T-B6 |
| B7 | token 輪替時，`keepConnected` 會 retire 舊 client，所有還在飛的讀取一起變成 `offline`（`cloud-boot.js:731-734`） | 英文「the cloud connection dropped」 | 新代碼 `cloud_reconnecting`（`browser` 層、可重試），在地化 | T-B7 |
| B8 | 診斷面板的「Send to Mac」在 Cloud 上是 `fetch` 到 hosted console 自己的網域（`layout-diagnostics.js:428`） | 報告送不到 Mac | 改走 Cloud 指令 `diagnostics.report`，Mac 端沿用 `DiagnosticReport.save` 與固定檔案 | T-B8 |
| B9 | 收到的 Mac 信封 `key_id`，和這個瀏覽器送指令用的 `this.keyID`（`cloud-client.js:209`）不一樣 | 這種狀態下兩端都不會說 | 瀏覽器自己就偵測得到：狀態顯示「金鑰不一致：這個瀏覽器用 `ms-1` 送出，Mac 目前是 `ms-2`」，並提供既有的配對修復入口 | T-B9 |

### 3.2 Relay（`relay`）

這一節的程式在私有 repo `~/code/clawdline-cloud`，這一輪納入（§8 決定 3）；要不要部署仍是使用者的決定。

| ID | 路徑 | 今天 | 之後 | 測試 |
|---|---|---|---|---|
| R1 | 所有拒絕都不寫 log：`publish_error`、`malformed_envelope`、關閉連線、handshake 失敗、`machine_offline`。設計文件 `artifacts/2026-08-27-ctl-response-seam.md:2202` 寫說會記，程式裡沒有 | 營運端看不到任何拒絕 | 結構化的 `console.error({at:"publish_refused", code, field, ch_kind, seq, dev})`，每條連線、每個 code 限流 | T-R1 |
| R2 | WebSocket 升級前的拒絕（401、403、429、502）只存在 HTTP 回應裡，瀏覽器的 WebSocket API 讀不到 | 一律顯示成 offline | 在 Worker 先接受升級，送出 `{"type":"error","code":…}`，再用 44xx close，讓瀏覽器讀得到 | T-R2 |
| R3 | 帳號的 identity epoch 前進時（例如別台裝置被撤銷），既有 socket 被關閉的訊息寫「this device has been revoked」（`account-do.ts:526,1519`） | 誤以為自己被撤銷 | 新代碼 `token_superseded`，意思是重連就好 | T-R3 |
| R4 | `/v1/ingest` 的 `clock_skew` 不在 `errors.ts` 的狀態表裡，所以回 500 | — | 補進表裡，回 400 | T-R4 |
| R5 | `publish_error code=internal` 實際上會發生（`account-do.ts:753,763`），表上卻標成 `forward`（尚未發出） | — | 表與測試改成 reachable | T-R4 |

### 3.3 Mac 入站（`mac_transport`）

| ID | 路徑 | 今天 | 之後 | 測試 |
|---|---|---|---|---|
| M1 | `reason=invalid` 一個字蓋掉大約十種原因：信封形狀、channel、key_id 不符（`CloudBridgeLifecycle.swift:53-58` 丟出 `unexpectedFrame("unknown-key")`）、解密失敗…（原因表在 `CloudTransport.swift` 的 `reason(for:)`） | log 一行，沒有 sender、seq、key_id；sender 什麼都收不到 | 細分成 `envelope_malformed`、`wrong_channel`、`key_id_mismatch`、`decrypt_failed`；照 §2.3 格式寫 log；記進狀態快照的丟棄紀錄（§4.1），並以 notice 送出（§4.2） | T-M1 |
| M2 | `unknown_sender`、`bad_signature`。另外，Keychain 讀取失敗時 roster 會退回空的 `[:]`（`CloudBridgeLifecycle.swift:66-69`），看起來就跟未配對一模一樣 | 同 M1 | 同 M1，並記錄 roster 當下讀不讀得到 | T-M2 |
| M3 | `replay`（`CloudTransport.swift:1580`）；序號追蹤只存在記憶體裡 | 沒反應 | `replay` 帶 `seq` 與 `highest_seq`，送 notice。瀏覽器看到自己配的 seq 被判 replay 時，畫面說「這台裝置在別的分頁也開著」 | T-M3 |
| M4 | 入站佇列拒絕（`count_cap`、`charged_byte_cap`、`plaintext_cap`）只有在 `commandRefusalReply` 找得到回覆位置時才會回。`answer`、`key`、沒帶 `request` 的 `send`、`dispatch` 全部靜默。`plaintext_cap` 是永久性的，訊息卻說「稍後再試」。`wrong_machine` 的回覆發到了錯的 channel | 多半沒反應 | 瀏覽器的所有指令一律帶 `request`（`answer`、`key`、`send` 補上），每一筆就都有回覆位置。`plaintext_cap` 改成不可重試的 `command_too_large`。仍然找不到回覆位置的，改送 notice | T-M4 |
| M5 | 拒絕回覆的 lane 塞滿（`dropped_full`）或逾時（`timed_out`）。本機 log：11 次 `timed_out`，7 次 `completed` | 沒反應 | 計數記進狀態快照；逾時或被丟的那一筆改送 notice | T-M5 |

### 3.4 Mac 預檢（`mac_preflight`）

| ID | 路徑 | 今天 | 之後 | 測試 |
|---|---|---|---|---|
| P1 | `wrong_machine`、`malformed_command`（`shell-kill` 除外）、`cloud_dispatch_unpinned`、`unknown_command`、`malformed_read`、`unserializable_read`（`CloudAppBridge.swift:1749-2098`、`:2324`、`:2761`） | 沒反應，60 秒後逾時 | 有 `request` 就回覆，沒有就送 notice；一律帶 `layer=mac_preflight` | T-P1 |
| P2 | `cloud_commands_disabled` 在解析指令之前就判定（`:1768`） | 有時候有回覆，有時候沒有 | 同 P1，固定回覆 | T-P1 |

### 3.5 帳本（`mac_ledger`）

| ID | 路徑 | 今天 | 之後 | 測試 |
|---|---|---|---|---|
| L1 | 所有帳本拒絕的回覆訊息都是同一句 `The command durability boundary is unavailable.`（`CloudAppBridge.swift:2201-2210`） | 語音直接顯示這句英文 | 回覆帶 `layer`、`code`、`detail`，畫面文字由 code 決定 | T-L1 |
| L2 | `command_clock_uncertain`：時鐘守衛的原因在 `CloudBridgeLifecycle.swift:147-150` 被丟掉。**這個窗口本身由 task 7dddfed6 修**，這裡只負責讓它看得見 | 同 L1 | `detail.reason`（例如 `stability_period_incomplete`）加上 `clears_in_ms`；畫面顯示「Mac 正在確認時間，約 N 秒後再試」 | T-L2 |
| L3 | `command_gate_unavailable` 一個代碼同時代表「roster 讀不到」和「寫入開關關著」（`CloudBridgeLifecycle.swift:592-598`） | 分不出是哪一個 | 拆成 `command_roster_unreadable` 與 `command_writes_disabled` | T-L3 |
| L4 | 帳本 row 不記指令種類、seq、session，也不記回覆有沒有送到；效果發生前的拒絕會把 reserved row 直接刪掉（`CloudCommandLedger.swift:715-726`），不留痕跡 | 事後查不到 | 另外在記憶體裡放一個「最近 N 筆指令」的 ring（§4.1），**不改帳本的持久格式** | T-L4 |

### 3.6 Route 與回覆（`mac_route`、`mac_reply`）

| ID | 路徑 | 今天 | 之後 | 測試 |
|---|---|---|---|---|
| F1 | route 錯誤：`remote:` 那行有 path 與 sender，但沒有 request；`busy` 路徑連 `remote:` 行都不寫 | 看得到 code，但 message 是英文 | 回覆帶 `layer=mac_route`；log 用 §2.3 格式 | T-F1 |
| G1 | 回覆發佈失敗，產生 `command_answer_undeliverable` 或 `read_answer_undeliverable`（本機 log 合計 170 次）。**這時效果已經執行了**，瀏覽器卻等到逾時 | 逾時，看起來像沒做 | 最近指令紀錄標成 `undeliverable`。瀏覽器逾時前先讀狀態；如果同一個 ref 已經 `executed`，就顯示「Mac 已經執行，但回覆沒送到」 | T-G1 |
| G2 | 已經進 spool 的回覆，超過 240 秒的 ready 期限或 30 秒的 receipt 期限就被燒掉，不寫 log；spool 的 metrics sink 是 no-op（`CloudDurableStores.swift:1157-1170`） | 同 G1 | 計數與最近 N 筆都記進狀態快照 | T-G1 |

## 4. 診斷讀取

### 4.1 Mac 端的 Cloud 狀態快照

新增一個 actor（新檔 `Sources/CloudStatus.swift`），把今天散在各處、在 production 裡**完全沒有人讀**的狀態收在一起：
入站佇列、拒絕 lane、帳本、出站 window 的 metrics，時鐘守衛狀態，token 到期時間，transport 狀態。

```json
{
  "clawdline_cloud_status": 1,
  "generated_at": "2026-09-13T07:01:05Z",
  "counting_since": "2026-09-13T06:40:12Z",
  "bridge": "attached",
  "transport": { "state": "connected", "connected_since": "…", "last_close": "token_rotation" },
  "token": { "expires_at": "…", "next_rotation_at": "…" },
  "clock_guard": { "state": "uncertain", "reason": "stability_period_incomplete", "since": "…", "clears_at": "…" },
  "identity": { "key_id": "ms-2", "roster_readable": true, "devices": [{ "device": "web_f052dcb8-…", "label": "iPhone" }] },
  "inbound": { "accepted": 812, "dropped": { "key_id_mismatch": 790, "replay": 13 }, "recent_drops": ["最多 20 筆"] },
  "commands": ["最多 50 筆：ref、type、各步驟時間、refusal{layer,code}"],
  "reply": { "undeliverable": 2, "expired_ready": 0, "expired_receipt": 0, "lane_dropped": 0 }
}
```

- 計數跟著 process 的生命週期，**不會因為重連歸零**，並用 `counting_since` 說明是從何時開始算的。
- 不存 transcript、標題或路徑。

### 4.2 三種讀者、三條路

| 讀者 | 路 | 為什麼這樣走 |
|---|---|---|
| 在 Mac 上除錯的 agent | 固定檔案 `~/Library/Logs/Clawdline/diagnostics/cloud-status.json`，有變化時寫入，最多每 2 秒一次 | 和 [`diagnostics.md`](diagnostics.md) 同一個目錄、同樣是「路徑固定」的原則；app 的 HTTP 掛了也讀得到；不必動 `RemoteServer.swift` |
| 手機，指令還送得進去時 | Cloud 讀取 `cloud.status`，在機器層 pseudo-session 上回覆完整快照 | 按一下就拿到最近 50 筆指令各自走到哪一步 |
| 手機，指令送不進去時（key_id 不符、replay…） | **notice**：把摘要（計數、最近 10 筆丟棄、時鐘守衛、token）併進 Mac 已經在發佈、而且手機本來就收得到的機器層快照。有丟棄或拒絕時合併起來重發，最多每 5 秒一次 | 送不進去的時候，任何「請求／回覆」都沒用，只能靠 Mac 主動發。relay 會快取最後一份，所以新開的分頁一連上就看得到 |

notice 併進機器層快照（§8 決定 1）。

### 4.3 手機上怎麼呈現

- **錯誤那一行本身可以點。** 點下去打開「Cloud 狀態」sheet，自動捲到同一個 `ref`：這一筆走到哪一步、在哪一層被拒、代碼、原因。
- **設定頁新增一列「Cloud 狀態」**，不必再連點五下版本號。
- **「Send to Mac」在 Cloud 上可以用了**（B8）。報告裡一併附上瀏覽器端的最近指令 ring（只有 seq、步驟、code，沒有內容）。
- sheet 的資料全部來自 §4.1 的快照與瀏覽器自己的 ring。**使用者要做的事，上限是按一下。**

## 5. 前端呈現的兩條規則

1. **畫面上的文字只由 `code` 決定。**
   - 新增共用模組 `Resources/web/app/js/core/failure-text.js`，提供 `describeFailure(error) → { text, code, ref, layer, retryable }`。
   - 未知代碼顯示「沒有成功（`code` · `ref`）」。
   - 守衛：靜態掃描 `js/input`、`js/view`、`js/session`，toast 與錯誤文字不准直接用 `.message`。
     現有 `worktrees.js`、`documents.js` 那種 `code: message` 的寫法，也一併改走這個函式。
2. **transport 不准同步 throw。**
   - CloudClient 的公開方法一律是 `async`，或回傳 Promise。
   - 守衛列舉 `CloudClient.prototype` 上的全部公開方法，在「沒有機器」「沒有 session」「socket 關著」三種狀態下各呼叫一次，
     斷言：沒有同步例外、回傳值是 thenable、reject 時帶 typed code。
   - 現有四處測試**把同步 throw 當成預期行為**釘住了，要一起改：`client.test.mjs:1261-1267`、`client.test.mjs:2008-2011`、
     `Tests/web-board-transport.mjs:60`、`Tests/web-timeline.mjs:90-93`。
   - task ae4e7f05 正在修 `_place` 的同步 throw。這條規則要等它落地之後，再把它的做法推廣到全部方法，
     避免兩個 worktree 同時改 `cloud-client.js`。

## 6. 認證相關

| 情境 | 今天誰知道 | 之後怎麼看得見 |
|---|---|---|
| 6.1 配對：sender 不在 roster 裡 | 只有 Mac log 的 `unknown_sender`，而且沒寫是哪個 sender | notice 帶 sender。瀏覽器發現那是自己的 device 時，顯示「這台 Mac 不認得這個瀏覽器」，並附配對入口 |
| 6.2 金鑰漂移：key_id 不符 | 沒有人 | 兩端都偵測：瀏覽器在本機比對收到的 key_id（B9）；Mac 的 notice 帶 `key_id` 與 `expected_key_id`（M1） |
| 6.3 同一 device 多分頁撞號 | 沒有人 | 瀏覽器在 notice 裡看到自己的 seq 被判 replay，就顯示「這台裝置在別的分頁也開著」；同時用 `BroadcastChannel` 偵測同一 device 的其他分頁並提示。撞號本身由滑動窗口修掉（§8 決定 2） |
| 6.4 device token 過期、被取代、被撤銷 | 只有 relay 知道（它關閉連線，但瀏覽器沒讀 close code） | 讀 close code（B3）；relay 分開 `token_superseded` 與 `revoked`（R3）；升級前的拒絕也讀得到（R2） |
| 6.5 roster 讀不到（Keychain） | 看起來跟未配對一樣 | 狀態快照顯示 `roster_readable:false`；代碼 `command_roster_unreadable`（L3） |

### 6.3 為什麼撞號今天一定會發生

瀏覽器的序號配置器（`cloud-boot.js` 的 `durableSequence`）每次向 `localStorage` 預留 64 個號碼。
Mac 的 `CloudSequenceTracker` 則要求**同一個 sender 的 seq 嚴格遞增**。

兩個分頁共用同一個 sender：分頁 A 預留了 100–163，分頁 B 接著預留 164–227。
只要 B 先送出 164，A 之後送的 101 就會被判成 replay。每次 token 輪替都會重建配置器、重新讀天花板，兩個分頁會互相跳過對方。
所以只要開兩個分頁，序號較低的那個，大部分指令都會被丟掉。

## 7. Failure-injection 測試清單

每一條都要**先在修正前的樹上跑紅，再在修正後跑綠**（[`verification-workflow.md`](verification-workflow.md) 的 guard 規則）。
前端用 node 測試（`client.test.mjs` 已經有假 socket）；Mac 用 `CloudTransportFakes.swift` 的假 relay；relay 用 `relay/test-workers/relay.test.ts`。

| 編號 | 注入什麼 | 斷言看得到什麼 |
|---|---|---|
| T-B1 | 假 relay 對 publish 回 `ack status=machine_offline` | 請求在一個 tick 內 reject：`layer=relay code=machine_offline`，ref 的 seq 等於送出的 seq；不是等 60 秒 |
| T-B2 | 假 relay 回 `publish_error code=forbidden field=capability` | 同 T-B1，另有 `detail.field=capability` |
| T-B3 | 假 relay 送 error frame，再以 4401 關閉 | 連線狀態記下 `unauthorized`；之後的請求 reject 時帶這個 code |
| T-B4 | 在三種壞狀態下呼叫全部公開方法 | 沒有同步例外、每個都回 thenable、reject 有 typed code。把其中一個方法故意改回同步 throw，守衛必須變紅 |
| T-B5 | 注入未知代碼 `zz_unknown` 與一個已知代碼 | 畫面文字含 `zz_unknown` 與 ref，不含 message 原文；在程式裡插入一行 `toast(e.message)`，靜態守衛必須變紅 |
| T-B6 | Mac 回 `no_whisper`，帶 `reason=no_model` | 顯示 NoModel 那一句 |
| T-B7 | 請求還在飛時觸發 token 輪替 | reject `cloud_reconnecting`，文字已在地化 |
| T-B8 | Cloud 模式下按 Send to Mac | 送出的是 `diagnostics.report` 指令，不是 fetch 到頁面自己的網域 |
| T-B9 | 收到 key_id `ms-2` 的 session 快照，而自己用 `ms-1` 送 | 狀態出現 `key_id_drift`，兩個 id 都在 |
| T-V1 | 未知 sender、同 key_id 不同 master、`validateEnvelope` 失敗、IndexedDB 拒絕、storage 寫入丟錯、Mac 回 `unknown_command`／`viewer_events_malformed`、50 筆連發、重新載入、UI handler 丟出帶標題的 V8 例外與 WebKit 句型、48 欄與非 ASCII 列、頁面被殺前未寫入、兩個分頁、被取代的裝置 log；per-machine pairing：未配對的第二台、配對 key_id 不符、paired sender 不符、`machine_key_incomplete`、pairing store 拒絕、legacy 綁定成功、legacy 綁定寫入失敗、已配對的 Linux executor、換對象後的 `unknown_command`、已配對 Mac 金鑰漂移、legacy 瀏覽器綁定前金鑰漂移、renewal；Mac：重送同一 batch、輪替後重送、id 紀錄上限、頁面封出的最壞批次 | 每筆一列，`stage` 與原始例外正確，引擎訊息不進列（只留名稱與 `error_class`）；配對欄位正確；legacy 綁定成功不留列、寫入失敗仍套用且留一列；列裡沒有 nonce／ct／sig／明文／金鑰；列 ≤ 48 欄且 ≤ 2,048 UTF-8 位元組、無孤立 surrogate；連發只留有界列數且丟棄數精確；未寫入的列以 `dropped_unflushed` 計入；兩個分頁不互蓋；`unknown_command` 時列留在手機且不重試，內容被拒時丟棄並計數、後面的列照送；一台的封鎖不擋下一台；未配對的第二台不開門、Mac 照常、store 只問一次；真正的金鑰漂移仍開門；Linux executor 永遠不是送達對象；批次用 Mac 的 pairing 封裝；收據對上 batch 才刪；Mac 對同一 batch 只寫一次（§11.9，`web-cloud-failures.mjs` 的 `viewer events ·`、`CloudTransparencyTests.swift` 的 `diagnostics.events` 與 `page→Mac contract`） |
| T-R1 | workerd 測試送一個簽章錯誤的 publish | log 出現 `publish_refused code=forbidden field=sig` |
| T-R2 | 用過期 token 連線 | 瀏覽器收到 error frame 與 4401，不是沒有代碼的 1006 |
| T-R3 | 撤銷另一台裝置，讓 epoch 前進 | 既有 socket 關閉時帶 `token_superseded` |
| T-R4 | ingest 送過期的 ts；製造 revocation race | 回 400 `clock_skew`；`internal` 在表上標成 reachable |
| T-M1 | 假 relay 送 key_id `ms-1`，Mac 目前是 `ms-2` | log 一行 `layer=mac_transport code=key_id_mismatch sender=… seq=… key_id=ms-1 expected_key_id=ms-2 reply=notice`；狀態快照 `dropped.key_id_mismatch` 加 1、`recent_drops` 有這一筆；notice 有發出去 |
| T-M2 | 未配對的 sender；簽章錯誤；roster 讀取丟錯 | 三個不同的 code；roster 讀取失敗時 `roster_readable:false` |
| T-M3 | 同一個 sender 先送 seq 10，再送 seq 5 | `replay` 帶 `highest_seq=10`；notice 有發出去 |
| T-M4 | 塞滿入站佇列後送一個 `key`（今天沒帶 request 的那種） | 瀏覽器收到 `cloud_ingress_busy` 回覆，不是逾時 |
| T-M5 | 塞滿拒絕回覆 lane | `lane_dropped` 加 1；notice 有發出去 |
| T-P1 | 未知指令、格式錯誤、寫入開關關著 | 各自回覆，帶 `layer=mac_preflight` |
| T-L1 | 任何一種帳本拒絕 | 回覆有 `layer`、`code`、`detail`；瀏覽器不顯示 message |
| T-L2 | 讓時鐘守衛變成 uncertain | `detail.reason` 與 `clears_in_ms` 出現在回覆與畫面上 |
| T-L3 | roster 讀不到；寫入開關關著 | 兩個不同的 code |
| T-L4 | 送三筆指令：一筆成功、一筆被帳本拒絕、一筆回覆失敗 | 狀態快照的 `commands` 三筆，步驟各自正確 |
| T-F1 | route 回 `busy` | 回覆帶 `layer=mac_route`；log 有 §2.3 格式那一行 |
| T-G1 | spool 發佈丟錯；receipt 逾時 | `undeliverable`、`expired_receipt` 各加 1；瀏覽器的逾時訊息變成「已經執行，但回覆沒送到」 |

### 7.1 在 hosted console 上的驗收（第二階段）

任務書要求的四種情境。每一種都要能在畫面或狀態 sheet 上說出層級、代碼與 ref，
而且在 `cloud-status.json` 裡找得到同一個 ref。

| 情境 | 怎麼製造 | 畫面應該說 |
|---|---|---|
| 時鐘穩定窗口 | 用測試開關強制讓時鐘守衛在 `stability_period_incomplete`（或另一個目前的七種守衛原因） | `mac_ledger · command_clock_uncertain · ref`，原因與 Mac 當下守衛值一致 |
| replay | 同一個 Chrome profile 開第二個分頁，先在新分頁送一筆，再回到舊分頁送一筆 | `mac_transport · replay · ref`，「這台裝置在別的分頁也開著」 |
| 金鑰不符 | 只在測試分頁的記憶體裡把 `keyID` 改成舊值再送出，不動 Keychain | 本機顯示 `key_id_drift`，Mac notice 顯示 `key_id_mismatch`，兩個 key_id 都在 |
| Mac 離線 | 把 Clawdline 的 Cloud 開關關掉後送出 | 立刻顯示 `relay · machine_offline · ref` |

製造 replay 會弄亂使用者自己正在用的分頁。驗收前先說一聲，結束後請他重新整理那個分頁。

## 8. 決定（2026-09-13 使用者以選項介面拍板）

四題都選了建議選項。以下是拍板的結果，以及當時有、但沒被選的替代方案（留作記錄）。

1. **Mac 的 notice 併進機器層快照。** 加密、同帳號的所有 viewer 都收得到、不改 relay；摘要上限約 8 KiB，
   有丟棄或拒絕時合併起來重發，最多每 5 秒一次。
   - 沒選：relay 的逐裝置通道 `ctlr/<machine>/<device>`（兩端都沒實作，要先審 reply key 設計並部署 relay）；
     只寫 Mac 端檔案（手機上看不到被丟掉的指令）。
2. **多分頁撞號要修掉。** Mac 的 replay 檢查改成滑動窗口：記住最高值以下 1,024 個已經收過的 seq，
   沒收過的照樣接受、收過的照樣拒絕；仍受 300 秒信封期限與帳本 request 冪等保護。
   這是在改一條指令安全規則，那一件交付後要派一次獨立複審。
   - 沒選：只讓它看得見。
3. **這一輪包含 relay 的 R1–R5。** 由 child 在 `~/code/clawdline-cloud` 實作並跑 workerd 測試；
   **部署 relay 之前另外問使用者。**
   - 沒選：這一輪不碰 relay，升級前的拒絕仍顯示成 offline。
4. **一般畫面上顯示代碼與 ref**，用小字放在句子後面，整行可以點開狀態 sheet。
   - 沒選：只在 Cloud 狀態 sheet 裡看得到。

## 9. 排程與相依

- **先等兩件落地：** task ae4e7f05（`cloud-client.js`、`start.js`、`i18n.js`）與 task 7dddfed6
  （`CloudBridgeLifecycle.swift`、`CloudClock.swift`、`docs/cloud.md`）。在那之前，這條線只動**新檔**與不重疊的檔案。
- **切成四件，每件都能獨立落地：**
  1. **前端錯誤呈現**：B1–B7、B9、`failure-text.js`、transport 契約守衛。等 ae4e7f05。
  2. **Mac 錯誤契約與狀態快照**：M1–M5、P1–P2、L1–L4、F1、G1–G2、`CloudStatus.swift`、`cloud-status.json`。
     等 7dddfed6；會動到 `CloudTransport.swift`、`CloudAppBridge.swift`、`CloudCommandLedger.swift`。
  3. **手機的狀態 sheet**：`cloud.status` 讀取、notice 的呈現、Cloud 上的 `diagnostics.report`。依賴 1 與 2。
  4. **Relay**：R1–R5，在 `~/code/clawdline-cloud`。不依賴另外兩件，可以先做。
- **不動 `Sources/RemoteServer.swift`。** 狀態走檔案與 Cloud 讀取，不加 HTTP route。
  機器層快照是在 `RemoteServer.swift` 組好之後交給 `CloudAppBridge.publishOrchestrator`，notice 的摘要要併在 bridge 裡。
  如果之後要加 `GET /v1/cloud/status`，那一件要把 ceiling 與 governance 表一起算進範圍。
- **編譯：** Mac 那件需要一次完整 Swift 編譯，排在 `./test.sh` 的機器鎖後面。重建 app 前一定先告知使用者。
- **部署（全部是使用者的決定）：** 重建 Mac app、部署 hosted console 到 Pages、部署 relay。
  相容性：新 console 配舊 Mac、舊 console 配新 Mac 都要能跑，所以只新增欄位，不改既有欄位。

## 10. 順帶發現、不在這一輪範圍

| 發現 | 出處 | 建議 |
|---|---|---|
| Root Assignment 的收據比對失敗，broker 對同一個 root 把任務書打了兩次，還把活著的 root 記成 `failed/prompt_timeout` | `~/.config/clawdline/orchestrator.json` 裡記著 `inject_attempts: 2`；`Sources/Orchestrator.swift` 的 `rootAssignmentTranscriptReceipt` | 另開一件（已列給使用者） |
| 帳本 row 要過期，必須 `retainedElapsedMilliseconds` 往前走，但沒有任何 production 呼叫會推進它 | `CloudCommandLedger.swift:772-774`；磁碟上 13 筆全部是 0 | **待驗證。** 磁碟上只有 13 筆、橫跨 48 分鐘，log 卻有 842 次 Cloud POST，看起來 store 會被重置。要先確認實務上會不會真的累積到每台 1,000 筆的上限 |
| Onboarding 讀的是舊的 roster store，而 production 遷移完成後會刪掉它 | `Onboarding.swift:1403`、`CloudBridgeLifecycle.swift:541` | 待驗證（目前只從程式推論） |
| relay 每條連線最多 8 個訂閱，但瀏覽器的訂閱清單只增不減 | relay README；`cloud-client.js` | 待驗證 |

## 11. 線上格式（實作兩端的唯一依據）

前端、Mac、relay 分別由不同的人實作。兩端碰到彼此的地方**只以這一節為準**；與前面各節有出入時，這一節對。
改這一節要同時通知三端的實作者。

### 11.1 Mac 回覆裡的 `error`

沿用今天 `t/<machine>/<session>` 上的 `{"read":<name>,"status":<int>,"error":{…}}`，`error` 物件：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `code` | string | 既有，snake_case，1–64 字元 |
| `message` | string | 既有，英文；只留給舊 console |
| `layer` | string | **新增**。`mac_transport`、`mac_preflight`、`mac_ledger`、`mac_route`、`mac_reply` 之一 |
| `seq` | integer | **新增**。被回覆的那筆指令的信封 seq |
| `detail` | object | **新增**，選填。只放該 code 白名單內的欄位（§11.6） |

今天已經放在 `error` 最上層的 `reason`、`app`、`lost`、`retry_after`、`lane`、`limit` 維持原位，不搬進 `detail`；
瀏覽器正規化時，把白名單內的最上層欄位一併複製進 `CloudFailure.detail`。

舊 Mac 的回覆沒有 `layer`：瀏覽器記成 `layer:"mac"`（只為相容而存在的第八個值），畫面照常由 `code` 決定。

### 11.2 notice：`orch/<machine>` 快照裡的 `cloud_status`

Mac 發佈的 `orch/<machine>` payload 物件多一個最上層鍵 `cloud_status`，序列化後 ≤ 8 KiB：

```json
"cloud_status": {
  "v": 1,
  "generated_at_ms": 1789290000000,
  "counting_since_ms": 1789280000000,
  "clock_guard": { "state": "ready", "reason": null, "clears_at_ms": null },
  "token_expires_at_ms": 1789290240000,
  "key_id": "ms-2",
  "roster_readable": true,
  "dropped": { "key_id_mismatch": 790, "replay": 13 },
  "recent_drops": [
    { "at_ms": 1789289990000, "sender": "web_…", "seq": 1234, "code": "key_id_mismatch",
      "key_id": "ms-1", "expected_key_id": "ms-2", "highest_seq": null }
  ],
  "recent_notices": [
    { "at_ms": 1789289990000, "sender": "web_…", "seq": 1235, "request": null,
      "layer": "mac_preflight", "code": "malformed_command" }
  ]
}
```

- `clock_guard.state`：`ready` 或 `uncertain`。`reason` 是 snake_case 或 `null`。
- `recent_drops`：解密前或序號層的丟棄，最多 10 筆，新的在前。沒有的欄位寫 `null`，不省略。
- `recent_notices`：解密後、但找不到回覆位置而改走 notice 的拒絕，最多 10 筆，新的在前。
- **發佈時機**：bridge 保留最近一次收到的 orchestrator payload；`cloud_status` 有變化時，把它併進那份 payload 重發，
  合併後最多每 5 秒一次。bridge 從沒收過 orchestrator payload 時，發佈只含 `cloud_status` 的物件。
  `RemoteServer.swift` 不改。
- **能力訊號**：瀏覽器看到任何一台 Mac 的 `cloud_status.v >= 1`，才對那台 Mac 啟用依賴新 Mac 行為的功能（§11.4）。

### 11.3 Cloud 讀取 `cloud.status`

- 請求：沿用 `_machineRequest(machine, "cloud.status", {}, "read")`，也就是 session `__clawdline_machine__`、
  payload `{"type":"cloud.status","session":"__clawdline_machine__","request":<uuid>}`。
- 回覆：`t/<machine>/__clawdline_machine__` 上的 `{"read":"read:<request>","status":200,"body":<§4.1 完整快照>}`。
- §4.1 快照的 `commands` 每一筆：`{"sender","seq","request"|null,"type"|null,"session"|null,
  "accepted_at_ms"|null,"executed_at_ms"|null,"outcome"|null,"delivered_at_ms"|null,
  "undeliverable":null|"<snake>","refusal":null|{"layer","code"}}`。最多 50 筆，新的在前。
- `undeliverable` 的封閉詞彙是 `command_answer_undeliverable`、
  `read_answer_undeliverable`、`refusal_undeliverable`、`peer_rejected`、
  `receipt_expired`、`ready_expired`；瀏覽器對這一組都說回覆未抵達。

### 11.4 指令一律帶 `request`

- Mac：**每一種指令都接受選填的 `request`（小寫 UUID）**，包括 `answer`、`key`、`send`、`dispatch`；不因為多了這個鍵就回 `malformed_command`。
- 瀏覽器：`answer`、`key` **只在那台 Mac 已經送出 `cloud_status.v >= 1` 之後**才補上 `request`；`send` 本來就一律帶。
  capable Mac 的 `answer`／`key` 在 relay ack 後仍等待 `action:<request>`，由 Mac 的成功或拒絕結案；relay 的
  `machine_offline`／`publish_error` 仍可立即拒絕。舊 Mac 照舊走 ack-only，避免精確欄位檢查把新欄位當 malformed。

### 11.5 Cloud 指令 `diagnostics.report`

- 請求：`_machineRequest(machine, "diagnostics.report", {"report": <object>}, "action")`。
- Mac：用 `DiagnosticReport.save` 寫同一組固定檔案，`written_by` 是信封的 sender。
  回覆 `action:<request>`，body 與 `POST /v1/diagnostics/report` 成功時相同；錯誤碼與那條 route 相同（`report_not_json`、`report_too_large`、`report_write_failed`），
  `layer` 為 `mac_route`。
- 瀏覽器：只有已宣告 `cloud_status.v >= 1` 的 Mac 才收到這個新指令；舊 Mac 立即回本機的版本不相容提示。送出前先檢查序列化後的大小，超過 relay 對 `ctl` 類別的上限（見 `clawdline-cloud/docs/DECISIONS.md` D17 與 relay 的 per-class cap）
  或 `DiagnosticReport.maxBytes` 中較小的那個，就在本機以 `browser · report_too_large` 失敗，不送出。

### 11.6 `detail` 白名單

| code | 允許的 `detail` 欄位 |
|---|---|
| `command_clock_uncertain` | `reason`、`clears_in_ms` |
| `key_id_mismatch` | `key_id`、`expected_key_id` |
| `replay` | `highest_seq` |
| `cloud_ingress_busy`、`cloud_read_busy` | `retry_after`、`lane`、`limit` |
| `no_whisper` | `reason` |
| `terminal_closed` | `app` |
| `would_lose_work` | `lost` |
| `publish_error` 來的任何 code | `field` |
| 其他 | 無 |

新增 code 或欄位時，同時更新這張表與兩端的白名單。

### 11.7 Relay

- `ack`、`publish_error` 的形狀不變；瀏覽器以 `(ch, seq)` 對應請求。
- 新 code `token_superseded`：identity epoch 被取代時使用，close code 4401，瀏覽器視為可重連。
- 升級前的拒絕改成：接受升級 → 送 `{"type":"error","code":<code>,"message":<english>}` → 以 `errors.ts` 的 `WS_CLOSE` 對應碼關閉。
  瀏覽器記下最後一個 error frame 的 `code` 與 close event 的 `code`。
- 新增的 log 只寫 `code`、`field`、channel 種類、`seq`、device id；不寫信封內容。

### 11.8 Mac 的 replay 窗口

- 每個 sender 記住目前最高的 seq，與最高值以下 1,024 個位置裡「已經接受過」的集合。
- 接受：seq 大於最高值；或 seq 在窗口內且沒被接受過。
- 拒絕（`replay`，`detail.highest_seq`）：seq 在窗口內且已被接受過；或 seq 比窗口更舊。
- **判斷依據只有一條：同一個 `(sender, seq)` 在這個 process 裡最多被接受一次；說不準的一律拒絕。**
  既有的 300 秒信封期限與帳本 request 冪等保持不變，不能拿它們來取代這一條。

### 11.9 Cloud 指令 `diagnostics.events`

瀏覽器收件失敗與解密門的自動紀錄，不需要任何按鍵。怎麼讀、欄位、每個 `stage` 與它會不會開門、H1a／H1b／H2–H4 對照：[`diagnostics.md`](diagnostics.md#the-viewer-events-file)。
H1 在 per-machine pairing 之後分成兩種：H1a 是本瀏覽器**沒有配對**的第二台機器，
H1b 是**有配對**但配對不完整（`machine_key_incomplete`，只標在該機器的修復狀態，不開帳號門）或 key_id 不同（`pairing_key_id` 的 `unknown_key`）的機器。
H1a 現在是那台機器自己的狀態：envelope 路由到一台本瀏覽器沒有 pairing、也沒有該 sender pin 的機器時，丟 `machine_not_paired`（`detail.machine`），
`cloudSessionAccessProblem` 不分類它，所以不開整個帳號的解密門；`CloudClient.machineAccess(machine)` 保留 `not_paired` 狀態（renewal 後仍在），
給「為選定機器配對」的介面使用。`machine_key_incomplete` 同樣只留在該機器，等使用者真的對它操作時再由 `machine_pairing_required` 顯示配對門；真正的金鑰漂移（含尚未綁定前帳號金鑰就漂移的 legacy 瀏覽器）仍開解密門。

- 請求：`{"type":"diagnostics.events","session":"__clawdline_machine__","request":<uuid>,"batch":<object>}`，`ctl` 類別，
  read-level（和 `diagnostics.report` 一樣不需要遠端寫入開關）。
- 對象：本瀏覽器**有配對**的機器（它的 `orch/<machine>` 快照是經由本瀏覽器對那台機器的 pairing 打開的——精確 pairing，或驗章與解密成功後綁定的 legacy pin）、
  descriptor 沒有標成非 Mac 平台、並送出過 `cloud_status.v >= 1`（只有 Mac 會送）。Linux executor 不實作這個指令（收到會直接丟掉、不回覆），
  以平台與能力兩個獨立事實排除。封裝和其他指令一樣走 `_outboundMachinePairing`，配對已不在時在本機以 `machine_pairing_required` 拒絕、照退避重試。
  **不用 `_onlyMachine`**；兩台有能力的 Mac → 本機 `cloud_machine_ambiguous`，列留在手機。
- `batch`：`{v:1, batch_id, created_at_ms, device, tab, web_build, rows:[{n, at_ms, event, tab, data}], completeness:{n_from, n_to, rows,
  dropped_rate_limited, dropped_overflow, dropped_refused, dropped_unflushed, storage_errors, counting_since_ms, rate_limited:[{key, event, dropped, first_at_ms, last_at_ms, sample_n}], limits}}`，
  鍵集合精確比對；`data` 是一層的 scalar 或短陣列，最多 48 欄，不認得任何事件名稱。列的 `tab` 是記下那一列的分頁。
  算式：`rows + dropped_overflow + dropped_unflushed = n_to - n_from + 1`；`dropped_refused` 是先前被 Mac 以內容拒絕而丟掉的那一批的列數，不在本批範圍內。
  頁面端以 UTF-8 位元組量：每列 ≤ 2,048、每批 ≤ 240 KiB，字串不在 surrogate pair 中間截斷。頁面實際封出的最壞情況批次存在
  `Tests/cloud-viewer-events-contract-batches.json`，node 逐位元組比對、Swift 以真正的 `CloudViewerEventLog.problem(in:)` 與 `append` 驗證。
  例外訊息只保留本程式碼自己寫死的句子（`OWN_ERROR_MESSAGES` 精確比對），其餘只留 `error_name` 與 `error_class`。
- Mac：`CloudViewerEventLog.append` 在 `~/Library/Logs/Clawdline/diagnostics/cloud-viewer-events.jsonl` 追加一行（0600，超過 4 MiB 先輪替成 `.1`），
  `device` 是信封的 sender；`Clawdline.log` 寫一行只有計數的紀錄。成功回 `action:<request>`，body `{ok, batch_id, rows, path, line_bytes, file_bytes, rotated, duplicate}`。
  Mac 在 `cloud-viewer-events.batches` 記最近 2,048 個 `<device>\t<batch_id>`（獨立檔案，重啟與 `.1` 輪替後都還在）；
  認得的 batch 回 `duplicate: true` 與該批列數、不再追加。id 在那一行 fsync 之後才記，所以兩者之間當機仍可能寫兩次，但不會少寫。
  拒絕碼：`viewer_events_empty`（400）、`viewer_events_malformed`（400，訊息只寫欄位路徑、不寫值）、`viewer_events_too_large`（413，上限 256 KiB）、
  `viewer_events_write_failed`（500），`layer` 為 `mac_route`。
- 瀏覽器：同一批用同一個 `request`、同樣的位元組重送；24 小時內 Mac 帳本回既有結果，之後由上面的 batch id 紀錄認出，都不會寫兩次。收據的 `batch_id` 與 `rows` 相符才刪。
  最終拒絕分兩種：**拒絕這批內容**（`viewer_events_malformed`、`viewer_events_too_large`、`viewer_events_empty`、`malformed_command`，或 `mac_route` 層的任何最終拒絕）
  → 丟掉這一批，下一批以 `dropped_refused` 計入並附一列 `viewer_events.batch.refused` 寫明拒絕碼、batch 與 Mac 指出的欄位路徑，後面的列不會被它卡住；
  **拒絕這台 Mac 或本裝置**（其他 4xx，408、429 除外，如 `unknown_command`；或本裝置沒有 `send_prompt`）→ 保留這一批，封鎖到 Mac build 或頁面 build 改變（最多一天），期間不重試。
  其他失敗照退避重試（至少 60 秒、每次加倍到 30 分鐘、連續被丟的批次同樣加倍、每日最多 48 次）。
  有 Web Locks 時同一台裝置只有持有鎖的分頁寫 `localStorage` 與送出，其他分頁的列留在記憶體、拿到鎖時重新編號併入；
  寫入是合併的（最多每秒一次、頁面隱藏時、送達相關動作立即），每筆失敗只寫幾個位元組的 `:journal`，被殺掉前沒寫入的列由下一頁以 `dropped_unflushed` 計入。
  拿到鎖時也移除同帳號下沒有分頁持有的其他裝置 log，並記一列 `viewer_events.log.pruned`。
