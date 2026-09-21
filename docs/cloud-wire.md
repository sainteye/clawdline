# Cloud wire：clawdline-go 接上 app.clawdline.com 的規格

這份文件是**後續 child 的規格書**。目標是讓 Go 版能照現有的 relay 協定跟 `app.clawdline.com`
講話，而 hosted console 一行都不用改（`docs/remote.md` 的設計原則 2）。

舊 Swift app 的 Cloud 端是 18 個 `Sources/Cloud*.swift` 共 22,345 行加 `WebPush.swift` 858 行；
Go 版在這一波之前是 0 行。**這一波只做地基**：canonical JSON、envelope 的編解碼、金鑰、簽章、時鐘。
實際連線、配對、27 種操作、推播都不在這一波，但這份文件把它們的規格也寫下來，因為後面要照著做。

## 0. 怎麼讀這份文件，以及它的來源

| 來源 | 位置 | 權威性 |
|---|---|---|
| 協定散文 | Cloud 服務（the cloud service）的 `docs/PROTOCOL.md`（661 行；不公開，本文件是這一邊對它的公開規格） | **規範**（README.md:4-6 說在 cutover ADR 之前它仍是 normative prose authority） |
| 契約包 | Cloud 服務的 `contracts/cloud/v1/` | candidate，closed schema＋測試向量 |
| relay 實作 | Cloud 服務的 `relay/src/` | 已部署的讀取者，實際會拒絕什麼以它為準 |
| relay 說明 | Cloud 服務的 `relay/README.md` | 錯誤碼、frame、token claims 的原文表 |
| Swift 實作 | `~/code/clawdline/Sources/Cloud*.swift` | 已上線的 producer |
| 公開測試向量 | `~/code/clawdline/Tests/protocol-vectors.json`（186,557 bytes，SHA-256 `ca354b68…fe5a9`） | **known-answer**，已複製到 `internal/domain/cloud/testdata/` |

規則：

- 每一條規格底下都標了來源檔案與行號。**沒有標行號的段落是推論**，並且會寫「（推論）」。
- 行號是 2026-09-18 讀到的那一份。`~/code/clawdline` 當時在 commit `85cf6003`。
- 四份來源互相不完全一致，不一致的地方在 §11 列成表，每一項都說明這一版選了哪一邊。
- **這個 repo 全程只讀 `~/code/clawdline` 與 Cloud 服務的原始碼，不改它們。**

---

## 1. 這條線上有誰

```
  瀏覽器 / PWA (viewer)                        這台 Mac (machine)
        │                                            │
        │  WSS /v1/connect?role=viewer               │  WSS /v1/connect?role=machine
        ▼                                            ▼
   ┌─────────────────────── relay（Cloudflare Workers + AccountDO）──────────────────┐
   │  只看得到 envelope 的外殼：ch / class / seq / ts / key_id / sender / 位元組數      │
   │  ct 永遠是不透明的。沒有 store-and-forward。                                      │
   └──────────────────────────────┬─────────────────────────────────────────────────┘
                                  │ 只有 entitlements / 裝置 token / 用量
                    control plane（Fastify + MongoDB rs0）
```

- **內容金鑰在使用者硬體上**，cloud 只送密文（`plan.md` §1 第 2、3 條；PROTOCOL.md:34-46）。
- **broker 留在 Mac 上**：claims、serialize、depth、capacity、dispatch policy 全部由收到指令的那台
  Mac 自己判斷，遠端呼叫者「不是 superuser，只是門口多一個請求者」（PROTOCOL.md:255-290）。
- **多機是 Cloud 功能**；免費版不依賴 Cloud（`docs/remote.md` 設計原則 1）。

### Go 版要有自己的裝置身分

不沿用舊 app 的身分（`docs/remote.md` 設計原則 3）。兩個 daemon 共用一個 machine identity 會搶同一組
sequence 與重送視窗。`internal/adapters/cloudkeys` 因此拒絕 `~/.config/clawdline`，只寫
`CLAWDLINE_NEXT_DIR/cloud/`。

---

## 2. Envelope：relay 唯一看得到的東西

### 2.1 十個欄位，就這十個

```json
{"v":1,"ch":"s/mac-01/session-01","seq":412,"ts":1787740000000,
 "class":"stream","key_id":"ms-1","nonce":"<base64 12 bytes>",
 "ct":"<base64 ciphertext||tag>","sender":"device-vector-01",
 "sig":"<base64 64 bytes>"}
```

- 欄位集合**必須剛好是這十個**：多一個未知欄位或少一個都是拒絕
  （`relay/src/lib/envelope.ts:29-41` `ENVELOPE_FIELDS`；`Sources/CloudEnvelope.swift:76-85` 用
  dynamic key 比對 `received == expected`）。
- **wire 上的欄位順序不是協定的一部分**：瀏覽器的 `JSON.stringify` 寫的是 insertion order。
  Go 端的 `DecodeEnvelope` 因此不要求 canonical bytes。（推論，但由
  `relay/src/lib/envelope.ts` 只做 key-set 檢查佐證。）
- **但「這個 envelope 的 canonical bytes」有唯一定義**：RFC 8785 的成員順序，也就是
  `ch, class, ct, key_id, nonce, sender, seq, sig, ts, v`。契約包的兩份 fixture 就是用這個拼法
  釘住 byte_length 與 SHA-256（`contracts/cloud/v1/vectors.json` 的
  `control_response.request_envelope` / `response_envelope`）。Swift 端用
  `JSONEncoder` 的 `.sortedKeys` 產生同一串（`Sources/CloudEnvelope.swift:157-161`）。

| 欄位 | 型別 | 規則 | 來源 |
|---|---|---|---|
| `v` | integer | 必須是 `1` | CloudEnvelope.swift:52, 251 |
| `ch` | string | §2.2 的文法；整串 ≤ 300 bytes | CloudEnvelope.swift:274-288；channels.ts:60 |
| `seq` | integer | 0 ≤ seq ≤ 9007199254740991 | CloudEnvelope.swift:56, 252 |
| `ts` | integer | Unix epoch **毫秒**，同樣的安全整數上限 | CloudEnvelope.swift:253；contracts README:85-86 |
| `class` | string | `stream` \| `ctl` \| `dispatch` \| `ho` | channels.ts:23-24 |
| `key_id` | string | token 字母表，≤ 64 bytes | CloudEnvelope.swift:258-260 |
| `nonce` | string | canonical padded base64，解出來剛好 12 bytes | CloudEnvelope.swift:53, 264-267 |
| `ct` | string | canonical padded base64，非空；≤ 25,162,752 bytes | CloudEnvelope.swift:268-269；contracts README:99-104 |
| `sender` | string | token 字母表，≤ 128 bytes | CloudEnvelope.swift:261-263 |
| `sig` | string | canonical padded base64，解出來剛好 64 bytes | CloudEnvelope.swift:244-247 |

**token 字母表**：ASCII `0x21..0x7e`，**排除 `/` 與 `|`**，長度 1..max
（`Sources/CloudEnvelope.swift:298-303`；`relay/src/lib/channels.ts:81`）。排除那兩個字元不是潔癖：
`/` 是 channel 的分隔符，`|` 是簽章字串的分隔符，少了這條規則，兩個不同的 envelope 可以組出同一串
被簽的位元組（`relay/README.md` 的「What the relay refuses」第二條）。

**canonical padded base64**：長度是 4 的倍數、只有標準字母表與 `=`、decode 之後 re-encode 必須
一模一樣（`Sources/CloudEnvelope.swift:305-315`）。Go 的 `base64.StdEncoding` 會**略過換行**、
也不檢查尾端的未用位元，所以 `DecodeCanonicalBase64` 先自己檢字母表、再用 re-encode 比對
（`internal/domain/cloud/envelope.go`）。

### 2.2 Channel 文法與 class 對應

| prefix | 段數 | 允許的 class | 載什麼 |
|---|---|---|---|
| `s/<machine>/<session>` | 2 | `stream` | session 快照，**每次都是整份，永遠不是 diff** |
| `t/<machine>/<session>` | 2 | `stream` | transcript 片段，viewer 打開 session 才要 |
| `orch/<machine>` | 1 | `stream` | orchestrator 快照：tasks、schedules、waits |
| `ctl/<machine>` | 1 | `ctl`, `dispatch` | viewer → Mac 的指令 |
| `ctlr/<machine>/<viewer_device>` | 2 | `ctl` | Mac → 指定 viewer 的指令回應 |
| `ho/<account>/<handoff_id>` | 2 | `ho` | 交接包與交接收據 |
| `wh/` | — | **保留，一律拒絕** | §10 留給 v1 的 webhook-out |

來源：`PROTOCOL.md:48-66`、`contracts/cloud/v1/vocabularies.json`、
`relay/src/lib/channels.ts:1-53`、`Sources/CloudEnvelope.swift:274-296`。

規則是「class 必須是該 prefix 的允許集合的**成員**」，不是相等
（`relay/src/lib/channels.ts:12-15`）。`dispatch` 走的是指令通道，只是為了計費而可分辨
（PROTOCOL.md:40-43、63-66）。

**`%` 不會被解碼**：`t/mac-01/%E6%9C%83%E8%A9%B1` 這種 percent-encoded 的邏輯 Unicode 識別字
在 wire 上就是字面的 ASCII bytes，不可以 URL-decode 也不可以做 Unicode normalization
（`contracts/cloud/v1/README.md:88-90`；向量 `unicode-channel-id` 即此例）。

### 2.3 大小上限，三個不同的數字

| 數字 | 值 | 是什麼 |
|---|---|---|
| 25,162,752 | `floor((32 MiB − 4 KiB) × 3/4)` | **wire 上限**：base64 之後的 frame 還塞得進 Cloudflare 的 32 MiB WebSocket 訊息 |
| 16 MiB | `max_envelope_bytes` / `max_handoff_bytes` | **帳號上限**，每個 tier 都一樣，由 entitlements 回答 |
| 1 MiB | relay 的 `DEFAULT_LIMITS.maxCiphertextBytes` | relay 讀不到 entitlements 時的**退路** |

來源：`PROTOCOL.md:433-443`、`relay/src/lib/envelope.ts:56-62`、`contracts/cloud/v1/README.md:99-104`。
**不可以把 wire 上限當成 entitlement**（README:104 原文）。

Go 端把前兩個放成常數 `MaxCiphertextBytes` 與 `AccountEnvelopeBytes`，並在註解裡寫清楚後者是政策
而非 wire 規則。

---

## 3. Canonical JSON（RFC 8785 的安全整數子集）

這串位元組同時是三樣東西：簽章的輸入、pairing wire body 的唯一合法拼法、以及每一個
`charged_bytes` 的數字。**這裡錯一個 byte，另一端看到的是「簽章驗不過」，而且永遠安靜地錯下去**
（`Sources/CloudCanonicalJSON.swift:3-6`）。

### 3.1 序列化規則

1. 物件成員依 **UTF-16 code unit** 遞增排序（`CloudCanonicalJSON.swift:144`）。
   **這不是 Go 的 byte 順序**：U+FFFF 以上的 code point 在 UTF-16 是 surrogate pair（D800–DFFF），
   會排在 U+E000..U+FFFF **之前**，在 UTF-8 byte 順序則排在**之後**。Go 端要用 `utf16.Encode`
   比較（`internal/domain/cloud/canonicaljson.go` 的 `lessUTF16`）。
2. 陣列保持原順序。
3. 字串只跳脫必要的：`"`→`\"`、`\`→`\\`、U+0008/09/0A/0C/0D→`\b \t \n \f \r`、
   其餘 C0 控制字元→小寫 `\u00xx`。**`/` 不跳脫，非 ASCII 直接輸出原始 UTF-8**
   （`CloudCanonicalJSON.swift:157-183`）。
4. 沒有任何無意義空白。
5. 數字：**只接受整數**，範圍 `[-(2^53−1), 2^53−1]`。小數點或指數一律丟 typed error，即使
   數學上是整數（`CloudCanonicalJSON.swift:8-33, 469-515`）。負零序列化成 `0`
   （`contracts/cloud/v1/README.md:114-121`）。

為什麼把浮點數整個關掉：RFC 8785 §3.2.2.3 要求用 ECMAScript 的 `Number::toString`，Swift 與 Go 的
預設格式化都不等於它；一個「幾乎對」的 float serializer 不會大聲失敗，只會產生另一端永遠重現不了的
位元組（`CloudCanonicalJSON.swift:11-18`）。2^53 以上同理，只是在數線的另一端：node v24.1.0 實測
`9007199254740993` 會變成 `9007199254740992`（`CloudCanonicalJSON.swift:20-33`）。

### 3.2 嚴格解析（`parseStrict`）

輸入的位元組必須**就是**它自己的 canonical 編碼，另外拒絕：

| 拒絕 | 為什麼 | 來源 |
|---|---|---|
| 重複的成員名（比對未跳脫的原字串） | Foundation 的 `JSONSerialization` 會安靜地留最後一個 | CloudCanonicalJSON.swift:289-294 |
| 開頭的 BOM | | :195-197 |
| 頂層值之後還有位元組 | | :200-202 |
| 不合法的 UTF-8（含 overlong、編碼過的 surrogate、> U+10FFFF） | | :435-467 |
| 落單的 UTF-16 surrogate（`\uD800` 沒有配對） | RFC 8785 §3.2.2.2 note | :391-404 |
| 巢狀深度 > 256 | fail-closed sanity bound | :86-88, 525-530 |

**Go 端刻意分成兩個入口**：`Parse`（不要求 canonical，給 wire 上的 envelope 用）與 `ParseStrict`
（要求 canonical，給 pairing body、receipt、fixture 用）。舊 app 只有後者，因為它用
`JSONDecoder` 讀 envelope。（這是 Go 端的設計判斷，見 §11。）

### 3.3 記錄簽章的輸入（`signingInput`）

```
<domain 的 ASCII bytes> || 0x00 || canonical(body 去掉「頂層」的 sig 成員)
```

- 只去掉**頂層**的 `sig`；巢狀裡的 `sig` 是別人的 payload 資料，照樣被簽。
- domain 必須是可列印 ASCII（0x20–0x7E）。連控制字元都拒絕：domain 裡的 NUL 會跟分隔符撞在一起，
  讓兩組不同的 (domain, body) 共用同一個簽章輸入。

來源：`Sources/CloudCanonicalJSON.swift:209-237`。

**這跟 envelope 的簽章格式不是同一個東西**（§4.1 是 pipe 格式）。

### 3.4 `charged_bytes`

就是 canonical UTF-8 的長度。二進位欄位必須在進入記錄之前先變成 canonical 標準**帶 padding** 的
base64 字串（`CloudCanonicalJSON.swift:78-82, 239-245`）。

---

## 4. 簽章與加密：確切的演算法與參數

### 4.1 Ed25519 簽章

被簽的是這一串 UTF-8，**沒有前綴、後綴、BOM、NUL、換行或多餘空白**：

```
v|ch|seq|ts|class|key_id|nonce|ct
```

- 七個 ASCII `|`，順序就是上面那個。
- 整數用十進位純文字。
- `nonce` 與 `ct` 用的是**base64 的原始拼法**，不是解出來的 bytes、不是 base64url、也不是重新
  序列化過的 JSON。
- **`sender` 與 `sig` 不在簽章範圍內**。驗章之前要先把 `sender` 解析成本機釘住的公鑰；
  讓呼叫者自己挑一把金鑰會把這個綁定整個抹掉。

來源：`Sources/CloudEnvelope.swift:143-151, 209-221`；`contracts/cloud/v1/README.md:107-112`；
`relay/src/lib/envelope.ts:7-8`；exact bytes 在 `contracts/cloud/v1/fixtures/signing/*.bin`。

簽章長度 64 bytes。金鑰是**每台裝置自己的 authentication key**（Ed25519），不是 master secret
（PROTOCOL.md:38-39）。

### 4.2 AES-256-GCM 加密

| 參數 | 值 | 來源 |
|---|---|---|
| 演算法 | AES-256-GCM（D16，CryptoKit 與 WebCrypto 原生共有的那個 AEAD） | PROTOCOL.md:34-37 |
| 金鑰 | 32 bytes：帳號 master secret，或 ctlr 回應的 request-scoped reply key | PROTOCOL.md:34-37；vectors `control_response` |
| nonce | **12 bytes**，隨機 | CloudEnvelope.swift:53；relay/src/lib/envelope.ts:43 |
| tag | 16 bytes，**接在 ciphertext 後面**，兩者一起 base64 成 `ct` | CloudEnvelope.swift:192, 231-238 |
| AAD | **沒有**（envelope 不帶 AAD；pairing wrapper 才有，見 §8.3） | CloudEnvelope.swift:191 |

> **PROTOCOL.md §1 的範例寫「nonce: base64, 24 bytes」是錯的。** D16 的正文、Swift、PWA 與 relay
> 都是 12。契約包把這一項記成 `GAP-NONCE`（`contracts/cloud/v1/README.md:188`）。**照 12。**

結構檢查與開封檢查是兩層：wire 只要求 `ct` 非空；要開封另外需要至少 16 bytes（tag）並且 AEAD
驗證通過（`contracts/cloud/v1/README.md:99-101`，`GAP-CT-MIN`）。

### 4.3 兩種金鑰角色，不可以混用

| | Ed25519 device key | AES-256-GCM content key |
|---|---|---|
| 做什麼 | 簽 envelope、簽連線 challenge | 封 payload |
| 誰有 | 每台裝置各一把 | 整個帳號共用（master secret）；ctlr 回應另有一次性的 reply key |
| cloud 看得到嗎 | 只有公鑰（在 device token 的 `pk` claim 裡） | **永遠看不到** |
| 存在哪 | §5 的 KeyStore | 同上 |

`PROTOCOL.md:38-39` 原文：「Both key roles are blueprint §02; do not conflate them.」

### 4.4 `ctlr` 回應用的是請求帶來的金鑰

請求（viewer → Mac，走 `ctl/<machine>`，master secret 封）的 plaintext 裡帶著一把 32 bytes 的
reply key 與它的 `key_id`；回應（Mac → viewer，走 `ctlr/<machine>/<viewer_device>`）用那把 key 封，
**不用 master secret**。`key_id` 必須是 `rk-` 加 22 個 base64url 字元
（`contracts/cloud/v1/README.md:93-97`；`contracts/cloud/v1/schemas/envelope.schema.json` 的
第五個 `oneOf` 分支）。

向量 `control_response.response_open_results` 直接把這件事寫成兩個斷言：用 `ms-1` 開**失敗**、
用 `rk-…` 開**成功**。Go 端的 `TestControlResponseUsesTheRequestReplyKey` 照著跑。

> 這條的意義：拿得到帳號 master secret 的其他 viewer 也讀不到別人問出來的答案。

---

## 5. 金鑰的產生、儲存與指紋

### 5.1 產生

| | 規則 | 來源 |
|---|---|---|
| device key | 32 bytes 的 Ed25519 seed。CryptoKit 的 `Curve25519.Signing.PrivateKey.rawRepresentation` 就是這 32 bytes，Go 的 `ed25519.NewKeyFromSeed` 吃同樣的 32 bytes | CloudKeys.swift:358-372；**已由向量實測**（`TestDeviceKeyMatchesThePublishedSeed`） |
| master secret | 32 bytes 亂數 | CloudKeys.swift:393-405 |

### 5.2 指紋（pairing fingerprint）

`SHA-256(公鑰)` 的**前 10 bytes**，RFC 4648 base32（大寫、不補 `=`），每 4 個字一組用 `-` 連起來：

```
DTGF-MRHB-3IYP-RIER
```

來源：`CloudKeys.swift:380-385`（base32 在 :1766-1819）。**兩個獨立的 oracle**：向量
`pairing_handover.handover` 的 `machine_signing_key`/`machine_fingerprint`，與 `offer` 的
`viewer_signing_key`/`viewer_fingerprint`。Go 端兩個都測。

這是配對時兩個人互相念的短字串，**它本身不帶任何授權**。

### 5.3 recovery code

```
CLAWD1-<base32(secret || checksum) 每 5 字一組>
checksum = SHA-256("clawdline-recovery-v1" || secret)[:4]
```

來源：`CloudKeys.swift:394-396, 414-449`。base32 的尾端未用位元必須是 0，否則拒絕
（:1806-1808，原文：接受兩種拼法會讓有 checksum 的 recovery code 沒必要地變成非 canonical）。

> **沒有公開向量。** `internal/domain/cloud/keys_test.go` 裡的那一串是用一份**獨立的 Python
> 實作**（照 CloudKeys.swift 重寫）算出來的，不是跨 runtime 的既有向量。測試裡有寫明這個限制。

### 5.4 存在哪裡：這一版的取捨與接縫

舊 app 在 `CloudKeys.swift` 裡有 63 處直接呼叫 Keychain。Go 版**這一版用 0600 檔案**，放在
`CLAWDLINE_NEXT_DIR/cloud/`（`docs/remote.md` 設計原則 4）：

| 檔名 | 內容 |
|---|---|
| `device-ed25519-v1` | 32 bytes seed 的 base64，一行 |
| `account-master-secret-v1` | 32 bytes content key 的 base64，一行 |

檔名沿用舊 app 的 Keychain account 名稱（`CloudKeys.swift:655-656`），這樣兩邊的儲存可以放在一起
討論。

規矩照 `internal/adapters/devices`：目錄 0700、檔案 0600（每次寫都重設而不是相信建立時的值）、
寫入走同目錄的暫存檔＋fsync＋rename、開檔一律 `O_NOFOLLOW` 並在前後檢查是不是同一個 inode、
**拒絕 `~/.config/clawdline`**。

**最重要的一條**：檔案存在但讀不出來是**錯誤**，不是「還沒有」。上層的
`LoadOrCreateDeviceKey` 只有在確定「不存在」時才鑄新的；把讀取失敗當成不存在，會在一次
權限錯誤之後默默換掉身分，使用者看到的第一個症狀是「所有東西都解不開了」。

接縫是 `cloud.KeyStore`（5 個方法）。之後 macOS Keychain、Windows Credential Manager、
Linux Secret Service 各實作一份；沒有桌面環境的 Linux 仍然退回這個檔案版，並且要在設定頁寫明。

---

## 6. 時間與重送

### 6.1 三個時間規則

| 規則 | 值 | 誰執行 | 來源 |
|---|---|---|---|
| `ts` 離 relay 的時鐘太遠 → 拒絕 | `MAX_CLOCK_SKEW_MS` = 5 分鐘（雙向） | relay | relay/src/env.ts:86；envelope.ts:58-59 |
| 指令的效力期限 | `ts + 300 秒` | Mac | CloudAppBridge.swift:3530-3531 |
| 裝置 token 的壽命 | `exp - iat < MAX_TOKEN_LIFETIME_MS`（24 小時） | relay | relay/README.md 的 claims 表 |

`ts` 是 Unix epoch **毫秒**。

### 6.2 時鐘守衛（EpochGuard）

一台睡醒的筆電、一台從快照還原的 VM、一個手動改日期的使用者，都會讓這台機器相信一個過期的指令是新的。
所以時間不是直接讀的：

- 三個來源一起看：wall clock、**睡眠中仍會前進**的連續計數器、boot id
  （`CloudClock.swift:21-35`；:16-19 特別交代連續計數器不能在睡眠時停住，否則每次睡醒都會被讀成
  wall clock 往前跳）。
- 用一個 TLS 認證過的 server 時間做校準，然後必須有**完整 60 秒**三個時鐘互相吻合，admission 才打開
  （`CloudClock.swift:68-71` 的三個常數：5 分鐘 / 60 秒 / 2 秒）。
- 任何異常（wall 前跳、wall 回退、boot id 變了、連續計數器倒退、server sample 離太遠）都會關掉
  admission，**並且把校準丟掉**——之後只有新的 server sample 能恢復，光等時間不行
  （:209-225）。
- 從 ready 掉下來時，回傳值帶著一個**呼叫者必須做的清理**（`deleteReservedRow`）：在時間還可信時
  預留的東西，現在那個時鐘已經被推翻了（:51-56）。
- `acceptServerDate` 與 `offerServerDate` 是兩個入口：把每次 token renewal 都餵給前者，會在每次
  renewal 之後關閉 admission 整整一分鐘。**2026-09-13 實際發生過：每 240 秒有 60 秒的 Cloud 指令
  被拒絕**（:10-15）。

### 6.3 重送（replay）

每個 sender 一個滑動視窗，**同一組 `(sender, seq)` 最多被認領一次**
（`Sources/CloudEnvelope.swift:360-373`）：

| 情況 | 判定 |
|---|---|
| seq 高於目前最高 | 新的，接受 |
| seq 在視窗內（1024）且該位元未設 | 新的，接受 |
| seq 在視窗內且位元已設 | **replay** |
| seq 比視窗還舊 | **無法判定 → 照 replay 拒絕** |

- 視窗 1024、最多追蹤 4096 個 sender；超過就拒絕而不是淘汰別人的視窗，因為被淘汰的視窗
  再也無法判定（:375-378）。
- 「嚴格遞增」的舊寫法會在一台裝置開兩個分頁時拒絕編號較小的那個，因為每個分頁各自預留一段
  sequence（:369-372）。
- **認領是最終的**：認證通過就認領，之後即使因為 capacity 被拒絕也不還回去，所以同一個 envelope
  再送一次就是 replay（:372-373、`CloudTransport.swift:2449-2450`）。
- 這個視窗是 process 層級共用的：bridge 因為登出、重試或身分變更而重建時，新的 transport 不能
  從空視窗開始（`CloudEnvelope.swift:460-488`）。

另外一層是 command ledger 的 idempotency：`cloud:<sender>:<seq>`
（`CloudTransport.swift:618-620`）。capacity 拒絕對那個 sequence 是終局的，之後的請求會拿到新的
sequence，也就是新的身分。

**這一波沒有實作 replay 視窗與 ledger**（不在任務範圍）。下一波實作時照上表。

---

## 7. 連線、握手與 frame

### 7.1 連線

```
GET /v1/connect?role=machine|viewer
Authorization: Bearer <device token>
```

瀏覽器不能在 WebSocket 上設 header，所以改用
`Sec-WebSocket-Protocol: clawdline.v1, clawdline.token.<device token>`。
**query string 裡的 token 一律不接受**（會進 edge log、proxy log 與瀏覽器歷史）。
來源：`relay/README.md:56-70`。

握手兩步：

1. relay 送 challenge：
   `{"type":"challenge","v":1,"context":"clawdline-challenge-v1","account":…,"device":…,"challenge":"<base64 32 bytes>","expires_in_ms":15000}`
2. client 簽的**不是 challenge 本身**，而是

   ```
   clawdline-challenge-v1|<account>|<device_id>|<challenge>
   ```

   的 UTF-8，回 `{"type":"hello","sig":"<base64>"}`。光簽一個 nonce 是簽章 oracle：同一串位元組
   可以被拿去代表同一帳號的另一台裝置，或另一個 relay。
3. relay 回 `{"type":"ready",…}`，接著立刻送它記憶體裡還留著的 realign 快照。

來源：`relay/README.md:72-100`；Swift 端在 `Sources/CloudTransport.swift:1906-1950`，簽的字串在
:1923-1924。Swift 另外在這裡驗身分綁定：challenge 的 account/device 要等於本機的 binding，
且 `machineID == deviceID`、指紋要對得上（:1914-1921）。

### 7.2 Frame

| client → relay | |
|---|---|
| `{"type":"publish","envelope":{…}}` | 一個 envelope |
| `{"type":"subscribe","channels":["t/…"]}` | 打開 transcript 或交接包（每條連線最多 8 個訂閱） |
| `{"type":"unsubscribe","channels":[…]}` | |
| `{"type":"ping"}` | 不會叫醒 Durable Object |

| relay → client | |
|---|---|
| `{"type":"envelope","envelope":{…}}` | 一筆投遞 |
| `{"type":"envelope","realign":true,"envelope":{…}}` | 連線或訂閱時補的最後一份快照 |
| `{"type":"ack","ch":…,"seq":…,"fanout":N,"status":"delivered"\|"machine_offline"\|"viewer_offline"}` | |
| `{"type":"publish_error","ch":…,"seq":…,"code":…,"field":…}` | **只有在 ch 與 seq 通過文法之後**才會關聯 |
| `{"type":"subscriptions","channels":[…]}` | |
| `{"type":"error","code":"malformed_envelope"}` | 連線層級，不猜 ch 或 seq |
| `{"type":"error","code":…,"message":…}` | 連線／握手／訂閱失敗 |

來源：`relay/README.md:162-186`。

- `s/` 與 `orch/` 的快照不用訂閱就會送給每個 viewer；`t/` 與 `ho/` 要先 `subscribe`
  （:187-191）。
- **重連不需要 replay**：重連後的第一份快照就會把 viewer 對齊（PROTOCOL.md:58-61）。
- **沒有 store-and-forward**：`POST /v1/ingest` 會同步回答目標機器現在是不是連著，
  `delivered` 或 `machine_offline`（PROTOCOL.md:100-103）。

### 7.3 `POST /v1/ingest`

給握不住 socket 的呼叫者（CI）。Bearer device token，body 就是一個 envelope，回

```json
{"ok":true,"status":"delivered","ch":"ctl/desk","seq":1,"fanout":1}
```

「跟上面同樣的認證，減掉 socket」的意思是減掉 challenge：沒有連線可以綁，所以 envelope 自己的
簽章頂上，而且 `sender` 必須是 token 的裝置（`relay/README.md:220-229`）。

---

## 8. 身分、能力與配對

### 8.1 Device token 的 claims

`PROTOCOL.md:299-311` 釘住，`relay/README.md:252-281` 有完整表。重點：

| claim | 規則 |
|---|---|
| `sub` | 必須等於 `dev` |
| `dev` | 必須等於 envelope 的 `sender` |
| `mid` | `role=machine` 時必填，且 epoch-aware 的 machine token 要求 `mid == dev` |
| `pk` | base64 的 32 bytes Ed25519 公鑰 |
| `caps` | `read_sessions` \| `read_transcript` \| `send_prompt` \| `start_session`，**viewer token 必填** |
| `identity_epoch`、`key_epoch`、`capability_epoch`、`key_fingerprint`、`jwks_generation` | 五個一起有或一起沒有，部分提供直接拒絕 |

header 的 `alg` 必須是 `EdDSA` 或 `ES256`；`none` 與所有 HMAC 在看 payload 之前就被拒絕。

### 8.2 能力（capability）誰管

| capability | 誰執行 | 打開什麼 |
|---|---|---|
| `read_sessions` | relay，收的時候 | `s/` 與 `orch/` 快照 |
| `read_transcript` | relay，收的時候 | `t/` 與 `ho/` |
| `send_prompt` | relay，發與收；**Mac 自己的 admission 仍然另外算** | 發 `ctl/`（兩種 class）、收 `ctlr/` |
| `start_session` | **只有 Mac**（它在 `ct` 裡面） | relay 看不到的東西 |

來源：`PROTOCOL.md:322-343`。一份 receive policy 同時決定 subscribe、fan-out 與 realign，
被收窄的裝置會寫一行 `capability_refused`（只記 device、connection、channel kind、path 與需要的
capability，**不記 channel 名字**）。

**Machine 不受 capability 管**：它發自己的狀態、收寄給它的 `ctl/` 與依角色收 `ho/`。

### 8.3 配對（這一波不做，規格先寫下來）

嚴格身分配對是四階段 `offer → grant → activate → confirm`（`PROTOCOL.md:160-171`）。金鑰推導
（`Sources/CloudPairing.swift:481-558`）：

```
LP(x)  = uint16_be(len(x)) || x                                  # CloudPairing.swift:490-498
shared = X25519(自己的私鑰, 對方公鑰)                              # :499-513
         → 失敗、長度不是 32、或全部是 0 一律在 salt/HKDF 之前拒絕   # :533-534, 969-…
salt   = SHA-256( LP("clawdline-pair-salt-v1") || LP(pairing_nonce)
                  || LP(pairing_id 的 UTF-8) || LP(claim_nonce) )  # :538-543
prk    = HMAC-SHA256(key=salt, data=shared)                       # :544
info   = LP("clawdline-pair-v1") || LP(phase)                     # :546-548
phase_key = HMAC-SHA256(key=prk, data=info || 0x01)               # :549-550
```

也就是 HKDF-SHA256 的 extract＋一個 32 bytes 的 expand block。

grant wrapper 有七個成員，AAD 是 `v, phase, pairing_id, sender_device_id, ephemeral_key` 的
canonical JSON（`contracts/cloud/v1/README.md:125-128`）。`pairing_handover` 有八個成員，
**只有在配對的兩端才是明文**。claim nonce 與 QR pairing nonce 只以 SHA-256 摘要存放
（PROTOCOL.md:164-167）。

向量 `pairing_handover` 把 offer、handover、AAD、wrapper、phase_key 全部給了，所以下一波可以做
byte 對照。**低階（low-order）對方公鑰的負向向量**在
`contracts/cloud/v1/crypto-negative-vectors.json`：little-endian u=0 與 u=1，外加注入的全零／過短
結果，全部 `reject_before_hkdf`。

### 8.4 配對的承諾（照搬本機版）

碼只顯示在本機、不出現在回應／log／audit；最多猜 5 次、2 分鐘失效、一次一個、10 分鐘 3 次；
token 用 constant-time 比對、只存雜湊；寫入是另一個權限、預設關閉；本機也沒有例外。
來源：`~/code/clawdline/docs/remote.md` 的 How a device is paired，本 repo 的 `docs/remote.md`
設計原則 5 已經抄過一次。

---

## 9. 錯誤碼

### 9.1 relay 拒絕（連線層）

| code | close | HTTP | 什麼時候 |
|---|---|---|---|
| `bad_request` | 4400 | 400 | 二進位 frame；`role` 不是 machine/viewer |
| `unauthorized` | 4401 | 401 | 沒有／不能用／驗不過／過期的 device token；challenge 答錯 |
| `token_superseded` | 4401 | 401 | 帳號的 identity epoch 已經超過這個 token。**這台裝置沒有被撤銷**，換一個新 token 重連 |
| `forbidden` | 4403 | 403 | relay 知道已被撤銷的裝置；另一個角色的 token；別的帳號的 shard |
| `handshake_timeout` | 4408 | — | 沒在 `expires_in_ms` 內回答 challenge |
| `over_capacity` | 4429 | 429 | 方案的 machine 或 viewer 上限 |
| `rate_limited` | — | 429 | `/v1/ingest`：超過硬性月配額的 dispatch（socket 上是 `publish_error`） |
| `too_large` | — | 413 | `/v1/ingest`：body 超過 frame 上限（socket 上是 `malformed_envelope`） |
| `not_found` | — | 404 | 路由不存在 |
| `bad_gateway` | 1011 | 502 | 撤銷授權來源不可用，或 token 指的 identity generation 控制面還沒確認；可重試 |
| `internal` | 1011 | 500 | 開著的 socket 失去撤銷授權；沒設定控制面；物件答不出來 |
| `clock_skew` | — | 400 | **只有 `/v1/ingest`**：`ts` 超出 `MAX_CLOCK_SKEW_MS`（socket 上是 `publish_error`） |

來源：`relay/README.md:113-127`。**一個 relay 知道已撤銷的裝置，不管它的 token 寫哪個 epoch，
都是 `forbidden`；`token_superseded` 只會是沒被撤銷、但拿著 epoch 移動之前的 token 的裝置**
（:128-130）。

被拒絕的 WebSocket upgrade **仍然會被接受**，送剛好一個 `{"type":"error","code","message"}` frame
再用對應的 close number 關掉——不是 HTTP 401/403/429/502，因為瀏覽器的 WebSocket API 永遠不會把那些
交給頁面（PROTOCOL.md:87-92；relay/README.md:104-121）。

### 9.2 relay 的 publish 失敗

一旦 publish frame 的 channel 與非負 `seq` 通過文法，確定性的失敗（class 不符、簽章錯、ciphertext
大小、未知欄位）會用 `publish_error` 帶精確 metadata 回答；未知的 key 只會被表示成常數 `unknown`。
JSON 壞掉、不是物件、channel 或 sequence 壞掉，只會得到
`{"type":"error","code":"malformed_envelope"}`。**兩種形狀都不帶明文、request id 或英文訊息。**
可執行的權威是 `relay/src/lib/routing.ts` 的 `PUBLISH_ERROR_TABLE`（relay/README.md:203-218）。

### 9.3 relay 的 refusal log（欄位，不是碼）

`connect_refused` / `request_refused` / `socket_closed` / `frame_refused` / `publish_refused` /
`publish_undelivered`。`dev` 在 token 驗過之前是 `null`；`ch_kind` 是 channel 的種類，**不是它的
名字**；沒有任何一行帶 envelope 內容、token、金鑰或自由文字（relay/README.md:139-160）。

### 9.4 Mac 端丟棄入站 envelope 的理由

`Sources/CloudTransport.swift:395-410`：

`envelope_malformed`、`wrong_channel`、`unknown_sender`、`roster_unreadable`、`key_id_mismatch`、
`key_unreadable`、`bad_signature`、`decrypt_failed`、`replay`、`replay_window_full`。

`roster_unreadable` 與 `key_unreadable` 是刻意跟 `unknown_sender` 分開的：Keychain 讀失敗以前會變成
一個空的 roster，讀起來跟「這台裝置沒配對過」一模一樣（:399-404）。

### 9.5 Mac 端的入站容量拒絕

`Sources/CloudTransport.swift:679-704`：

| 原因 | 回給 viewer 的 code | HTTP 對應 |
|---|---|---|
| 佇列筆數上限（8） | `cloud_ingress_busy` | 429 |
| 佇列位元組上限（32 MiB） | `cloud_ingress_busy` | 429 |
| 單筆明文上限（16 MiB） | `command_too_large` | 413 |
| 已關閉 | `cloud_ingress_closed` | 503 |

單筆超過上限永遠不會塞得下，所以是終局的 `command_too_large`，而不是一個邀請對方重試的「忙碌」。

### 9.5.1 Mac 端的出站容量拒絕（2026-09-21）

入站滿了會回一句話，出站滿了以前只寫一行 log。答案已經算完、卻塞不進它要走的那條 channel 時：

| 請求是什麼 | 回給 viewer 的 code | layer | HTTP |
|---|---|---|---|
| 讀（沒有副作用，重試會成功） | `cloud_read_busy` | `mac_transport` | 429 |
| 指令（副作用已經發生，只有回執掉了） | `command_answer_undeliverable` | `mac_reply` | 503 |

兩個 code hosted console 本來就認得（`core/failure-text.js`），差別在於**發生過什麼**：讀可以重試，
指令不行——重試等於再跑一次。`detail` 只有讀那一條帶（`lane: "egress"`、`limit`、`retry_after`），
因為 copied client 的白名單沒有指令那一條，帶了也會被丟掉。

這句拒絕本身是從 spool 的保留額度進去的（`internal/adapters/cloud.spoolRefusalByteLimit`，每條
channel 同時只有一句）。一條塞滿的 channel 沒辦法用塞滿它的那條路說自己塞滿了，這是唯一的例外。

同一天順著這個問題做的稽核：**每一個決定「一筆答案可以多大」的地方，都要取兩個天花板裡小的那個**
——relay 的單封 ciphertext 上限，以及這台機器自己一條 channel 裝得下的量
（`cloud.spool_channel_bytes`）。`imageMaxEncodedBytes()` 原本只看前者，於是介於兩者之間的圖片會
通過門口、死在 spool。文件（2 MiB）與看板快照（1,000,000 bytes）本來就在下面，transcript 回應
（150 KiB）也是。

### 9.6 本機 broker 的 typed 拒絕

遠端派工會得到本機 orchestrator 原本就有的那組 typed error：`workspace_busy`、`over_capacity`、
`depth_exceeded`、`bad_task`（PROTOCOL.md:262-270）。

### 9.7 Cloud v2 儲存契約的錯誤碼（尚未使用）

`Sources/CloudV2Protocol.swift:25-64` 另有一組：`unsupported_schema`、`invalid_field`、
`invalid_digest`、`ciphertext_digest_mismatch`、`invalid_signature`、`aead_authentication_failed`、
`unauthorized_signer`、`unexpected_stream`、`key_epoch_conflict`、`command_deadline_invalid`、
`command_expired`、`dedupe_horizon_unsafe`、`repairable_gap`、`rollback_detected`、
`revision_regressed`、`writer_epoch_conflict`、`stream_fork`。

**這是 greenfield 的 v2 儲存契約，不是現在 relay 在跑的 v1 wire。**（推論：`CloudV2Protocol.swift`
的檔頭說它是「greenfield Cloud v2 storage contract」的 policy-independent primitives，而 §2 的
envelope 走的是 `CloudEnvelope.swift`。下一波要確認它有沒有被 `CloudTransport` 用到。）

---

## 10. 指令載荷（`ct` 裡面的東西）

cloud 看不到這一層。

### 10.1 請求

向量裡的那一筆（`vectors.json` 的 `control_response`，產生器在
`tools/generate-protocol-vectors.swift:330-334`）：

```json
{"deadline_at":1787817720000,"images":[],
 "reply":{"device":"viewer-device-01","key":"<base64 32 bytes>","key_id":"rk-zF3jN8rQ4Wm2pV6sT0uYxA"},
 "request_id":"018f2f7a-7d65-4aa8-8e01-11a8f4257ed1","session":"session-id",
 "text":"hello","type":"send","v":1}
```

### 10.2 回應（`execution_response`，九個欄位）

```json
{"code":"ok","completed_at":1787817600440,
 "request_id":"018f2f7a-7d65-4aa8-8e01-11a8f4257ed1","request_seq":411,
 "result":{"accepted":true},"retryable":false,"status":"ok",
 "type":"execution_response","v":1}
```

（`tools/generate-protocol-vectors.swift:348-350`。）

`vocabularies.json` 特別交代：`accepted:true` 仍然可能只是「准許進入後續工作」，不是效果證明；
relay 的 `delivered` 只證明 fan-out 送出去了，不證明機器執行或人看到。**六個 evidence domain
彼此不互相蘊含。**

### 10.3 27 種操作

`Sources/CloudAppBridge.swift` 的 `case "…"`。寫入類（:3107-3465）：
`send`、`answer`/`key`、`start`、`resume`、`end`、`focus`、`shell-kill`、`board-command`、
`timeline-command`、`schedule-create`/`schedule-update`、`schedule-delete`/`schedule-run`、
`snippet-create`/`snippet-update`、`snippet-delete`、`snippet-order`、
`schedule-webhook-bind-v1`、`push-subscribe`、`push-unsubscribe`、`push-test`、`voice`、
`diagnostics.report`、`diagnostics.events`、`dispatch`。

唯讀類（:3828-4090）：`transcript`、`info`、`agent`、`shell`、`skills`、`git`、`screen`、`image`、
`documents`、`document`、`board`、`board.items`、`timeline`、`places`、`project-worktrees`、
`project-worktree-lifecycle`(-refresh)、`past-sessions`、`schedules`、`snippets`、`schedule`、
`push-key`。

Go 版目前有對應本機功能的約 7 種（`docs/remote.md`）。**這一波（地基）一種都不做**；第三階段開始接，
逐項在 §10.5。**數字是量出來的，不要抄**：`cloudops.Vocabulary()` 與 `Implemented()` 都是從 catalog 推
出來的，2026-09-20 量到詞彙 37 個字、接上 28 個、其餘回具名的拒絕（8 個 `unknown_command`、
`dispatch` 回 `cloud_dispatch_unpinned`），`Divergences()` 5 筆。

### 10.4 派工就是 task.json

遠端派工的加密載荷帶的就是本機 orchestrator 已經在講的 wire format：**task.json 就是協定**。
收到的 Mac 用它自己的 claims、serialize、depth、capacity 與 dispatch policy 檢查，跟本機建立的任務
一模一樣，並在同一條通道上用同樣的 typed error 回答（PROTOCOL.md:262-270）。

### 10.5 Go 端怎麼接（2026-09-18 量到的）

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
| `screen` | read | `GET /v1/sessions/{id}/screen` | 409 `session_unknown` | 接上 |
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
| `start` | command | `POST /v1/places/{place}/start[/{assistant}[/{model}]]` | 沒實測（會真的開 session） | 接上，路由由 `start.go` 讀出 |
| `resume` | command | `POST /v1/places/{place}/resume/[{assistant}/]{past}` | 沒實測（同上） | 接上，路由由 `start.go` 讀出 |
| `voice` | command | `POST /v1/voice` | 400 `bad_request`（rate） | 接上 |
| `push-subscribe` | read-level command | `POST /v1/push/subscribe` | 200 / 400 `bad_request` | 接上（2026-09-20），**但每個 Cloud viewer 都是同一台裝置**（見下） |
| `push-unsubscribe` | read-level command | `POST /v1/push/unsubscribe` | 200 | 接上（2026-09-20） |
| `push-test` | read-level command | `POST /v1/push/test` | 沒實測（會真的對 `web.push.apple.com` 發 HTTPS） | 接上（2026-09-20），路由由 `push.go` 讀出 |
| `agent` | read | — | `GET …/agents/{id}` 405 | 回 `unknown_command` |
| `shell` | read | — | `GET …/shells/{id}` 405 | 回 `unknown_command` |
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

**三個「接上了但答案不一樣」的地方**（`cloudops.Divergences()` 會把它們列出來，接線的人在決定要對外
advertise 哪些字時讀得到）：

1. `board`：這個 daemon 的 `/v1/board` 是「從現在跑著的 session 推出來的 projects 快照」
   （`{schema_version, revision, projects, source, at}`），舊版是 `{"board": {items, revision, …}}`。
   兩邊回答的不是同一個問題。**在 Go 的 board 對齊之前，不要把 `board` 放進對外宣告的 `commands`**；
   要改成一律拒絕的話，`internal/app/cloudops/ops.go` 裡把 `board` 的 `route` 拿掉就會變
   `unknown_command`，是一行。
2. `end`：`expected_closeability_version` 有帶過去，`POST /v1/sessions/{id}/close` **沒有比對它**
   （`contract.CloseRequest` 只有 `force`）。route 自己的 obligation 檢查照跑，所以不是沒有守門，
   但 viewer 以為的「拿我看到的那一版做 compare-and-swap」在這裡不成立。要補要動
   `internal/transport/http`（另一條線的 claims），不在這一波。

3. `push-subscribe`：**每一個 Cloud viewer 在這裡都是同一台裝置。** in-process 的 Cloud 請求帶的是這台
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
   所以現在是空的，不發這個 key。

### 16.6 還沒做的（不可以當作通過）

- **配對沒做。** Go 版沒有機器半邊的 QR／四階段 handover，所以瀏覽器拿不到這台機器的 master
  secret。實測時是**用 devtools 把已完成配對會寫的那四筆直接種進 IndexedDB**
  （`clawdline.machine-master*` / `-sender` / `-binding`）。因此這一段證明的是傳輸與操作那一層，
  **不是配對那一層**。配對仍是下一波。
- `sessions.snapshot` 這個字沒有接（發佈器自己每 5 秒掃）。
- `t/` 上沒有主動推播；transcript 的即時更新在舊版是靠 row 上的 `transcript_signature` 變動來觸發，
  這個 daemon 還沒有算那個簽章，所以 console 要靠自己的重讀節奏。
- 正式環境（`relay.clawdline.com`／`api.clawdline.com`）一個位元組都沒連過。
- entitlements、推播、`ctlr/` 回覆軌、交接通道都沒動。

---

## 17. 第五階段：配對的機器半邊（2026-09-18 實測）

§8.3 當時寫「這一波不做，規格先寫下來」，§16.6 又記了一次「配對沒做，是用 devtools 種金鑰」。
這一節把那個洞補起來：**Go daemon 現在自己產生交接資料、自己封裝帳號金鑰、自己釘住瀏覽器的公鑰**，
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

`POST /v1/cloud/pairing` 回答的連結，fragment 裡帶著把帳號主金鑰交出去的一次性 secret。
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
- **`account-master-secret` 的輪替**沒做。輪替的是簽章金鑰；換內容金鑰要讓每一個 viewer 重新
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

`token_rotation` 與 `switched_off` 是事件不是失敗，對照為空。**重試政策一行都沒改**（仍是 Swift 的，§15.3、
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
