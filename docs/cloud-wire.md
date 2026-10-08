<!-- clawdline-doc: kind=spec audience=agent -->
# Cloud wire：機器與瀏覽器的公開協定

維護者可用這份規格查 envelope、canonical JSON、金鑰、配對與指令載荷。
[2026-09 的接線與實測紀錄](records/cloud-wire-implementation-2026-09.md)保留當時的來源與驗證結果；
目前的實作以 `internal/domain/cloud`、`internal/adapters/cloud` 與 `internal/app/cloudops` 為準。

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
- 四份來源互相不完全一致，不一致的地方在[實作紀錄 §11](records/cloud-wire-implementation-2026-09.md) 列成表，每一項都說明這一版選了哪一邊。
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
| `ss/<machine>/<session>` | 2 | `stream` | 不含內容的 Session 狀態列與清單標記；預設推送 |
| `s/<machine>/<session>` | 2 | `stream` | 可選發布的完整 session 快照，**每次都是整份，永遠不是 diff**；viewer 明確訂閱才接收 |
| `t/<machine>/<session>` | 2 | `stream` | transcript 片段，viewer 打開 session 才要 |
| `orch/<machine>` | 1 | `stream` | orchestrator 快照：tasks、schedules、waits |
| `ctl/<machine>` | 1 | `ctl`, `dispatch` | viewer → Mac 的指令 |
| `r/<machine>` | 1 | `ctl` | viewer → machine 的加密唯讀內容請求；僅 `info` 且 `parts=full` 或 `transcript` |
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
`JSONDecoder` 讀 envelope。（這是 Go 端的設計判斷，見[實作紀錄 §11](records/cloud-wire-implementation-2026-09.md)。）

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
| 金鑰 | 32 bytes：目標機器的 content key（wire 上仍稱 `master_secret`），或 `ctlr` 回應的 request-scoped reply key | `internal/transport/cloud/link.go:600-669`；`internal/transport/cloud/pairing.go:335-352`；vectors `control_response` |
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
| 誰有 | 每台裝置各一把 | 每台機器的本機 key store 各持自己的 content key；與該機器配對的 viewer 取得這一把；`ctlr` 回應另有一次性的 reply key |
| cloud 看得到嗎 | 只有公鑰（在 device token 的 `pk` claim 裡） | **永遠看不到** |
| 存在哪 | §5 的 KeyStore | 同上 |

`PROTOCOL.md:38-39` 原文：「Both key roles are blueprint §02; do not conflate them.」

Go 版在本機 store 缺少 content key 時自行產生，沒有從帳號載入共用 key；配對 handover 把**這台機器**的
key 交給 viewer，瀏覽器按 account ID ＋ machine ID 分開儲存、查找。舊瀏覽器的帳號層 key 仍有
相容讀取路徑，但新配對走機器層路徑；`master_secret` 與 `ms-1` 是沿用的 wire 名稱，不能用來推定
金鑰在整個帳號共用（`internal/domain/cloud/keys.go:302-324`；
`internal/transport/cloud/link.go:600-669,1074-1087`；`internal/transport/cloud/pairing.go:335-352`；
`web/console/src/legacy/js/net/cloud-boot.js:418-469,607-623`）。

### 4.4 `ctlr` 回應用的是請求帶來的金鑰

請求（viewer → Mac，走 `ctl/<machine>`，用該機器的 content key 封）的 plaintext 裡帶著一把 32 bytes 的
reply key 與它的 `key_id`；回應（Mac → viewer，走 `ctlr/<machine>/<viewer_device>`）用那把 key 封，
**不用該機器的 content key**。`key_id` 必須是 `rk-` 加 22 個 base64url 字元
（`internal/transport/cloud/link.go:642-669`；`internal/transport/cloud/relay.go:242-247`；
`contracts/cloud/v1/README.md:93-97`；`contracts/cloud/v1/schemas/envelope.schema.json` 的
第五個 `oneOf` 分支）。

向量 `control_response.response_open_results` 直接把這件事寫成兩個斷言：用 `ms-1` 開**失敗**、
用 `rk-…` 開**成功**。Go 端的 `TestControlResponseUsesTheRequestReplyKey` 照著跑。

> 這條的意義：拿得到該機器 content key 的其他 viewer 也讀不到別人問出來的答案。

---

## 5. 金鑰的產生、儲存與指紋

### 5.1 產生

| | 規則 | 來源 |
|---|---|---|
| device key | 32 bytes 的 Ed25519 seed。CryptoKit 的 `Curve25519.Signing.PrivateKey.rawRepresentation` 就是這 32 bytes，Go 的 `ed25519.NewKeyFromSeed` 吃同樣的 32 bytes | CloudKeys.swift:358-372；**已由向量實測**（`TestDeviceKeyMatchesThePublishedSeed`） |
| 這台機器的 content key（舊稱 master secret） | 32 bytes 亂數；本機 store 缺少時產生 | `internal/domain/cloud/keys.go:51-61,302-324` |

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
| `account-master-secret-v1` | 這個 daemon 本機的 32 bytes content key 的 base64，一行 |

檔名沿用舊 app 的 Keychain account 名稱（`CloudKeys.swift:655-656`；
`internal/adapters/cloudkeys/files.go:48-53`），是既有儲存格式
的名稱，**不是帳號共用範圍的宣告**。`LoadOrCreateMasterSecret` 只讀寫本機 store 的這一份；配對交出
這台機器的 key，瀏覽器則按 machine ID 儲存（`internal/domain/cloud/keys.go:302-324`；
`internal/transport/cloud/pairing.go:335-352`；`web/console/src/legacy/js/net/cloud-boot.js:607-623`）。

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
| `read_sessions` | relay，收的時候 | 預設接收 `ss/` 與 `orch/` 狀態快照 |
| `read_transcript` | relay，發 `r/` 與收內容時；machine 在 `r/` 回答前及封裝前再查 | 明確訂閱的完整 `s/`、`t/` 與 `ho/`，以及唯讀 `r/` 請求 |
| `send_prompt` | relay，發與收；**Mac 自己的 admission 仍然另外算** | 發 `ctl/`（兩種 class）、收 `ctlr/` |
| `start_session` | **只有 Mac**（它在 `ct` 裡面） | relay 看不到的東西 |

來源：`PROTOCOL.md:322-343`。一份 receive policy 同時決定 subscribe、fan-out 與 realign，
被收窄的裝置會寫一行 `capability_refused`（只記 device、connection、channel kind、path 與需要的
capability，**不記 channel 名字**）。

**Machine 不受 capability 管**：它發自己的狀態、收寄給它的 `ctl/` 與依角色收 `ho/`。

`orch/<machine>` 的 `machine.read_content_v1` 只在 machine 已接上唯讀 `r/` bridge 時為 `true`。缺欄視為舊版，不從 `ss/` 是否出現推論。`r/` 的 JSON body 只接受 `{"type":"info","machine_id":"<machine>","parts":"full","session":"<id>","expected_generation":"<32 lowercase hex>"}` 或 `{"type":"transcript","machine_id":"<machine>","session":"<id>","limit":1..1000,"expected_generation":"<32 lowercase hex>"}`；`machine_id` 必須與 `r/` channel 目標相同。機器從已驗簽並解密的 envelope 取得 `sender`、`seq` 與 `ch`；重新讀取 roster、signing key、本機撤銷及 `read_transcript` 能力，且在本機 pinned HTTP read 後、密封回答前再查一次。其他字回 `read_only_channel`；缺世代回 `execution_target_required`；能力不足回 `read_transcript_required`。成功與具安全回覆定位的拒絕仍走 `t/<machine>/<session>` `stream` envelope，payload 以 `read: "info.full"` 或 `"transcript"`、`status`、`body`／`error` 對應 waiter；錯誤 `seq` 對應原請求。Cloud viewer 應先訂閱精確的 `t/` channel，再發送 `r/`，並以選定的 machine、session、generation 驗證回覆後才顯示內容。

### 8.3 Session 狀態頻道的分階段契約

Go daemon 另外發布 `ss/<machine>/<session>`。每個 JSON 僅有 `machine_id`、`session_id`、可驗證時的 `execution_generation`、同批的 `snapshot_generation`、`assistant`、`backend`、`state`、`source`（`provenance`、`observed_at`、`freshness`）、`inventory_complete`、`projected_at`、`last_movement_at`、`no_progress_after_ms`、可判定時的 `no_movement`，以及有來源證據時的 `completed_unconfirmed`、`attention_required`、`close_blocked`、`failed_agent_count`。`completed_unconfirmed` 只在未讀 Board 交付已到 `done` 時為真；`deploying` 不算完成。`close_blocked` 在新鮮且狀態已知的關閉可行性讀值時帶 true/false；`failed_agent_count` 在完整且未截斷的 provider-native 子代理讀值時帶整數，包含 0。缺欄即未知，兩者都不宣稱整個 Session 工作失敗。`last_movement_at` 只表示助理自己的紀錄新增位元組，並非推論工作已進展；門檻由 `internal/domain/capacity` 登錄。不傳標題、對話、畫面、選單、Shell、Git、工作目錄或內容衍生摘要。來源不是 `current` 時省略執行世代與這些狀態事實；`scan.generation` 仍只是清單讀取序號，不能作為執行世代。

`waiting_for_reply` 是額外的正面布林訊號：只有當前 `waiting` 列真的帶選項，且 `screen_reading=read` 證實畫面選單，或 `screen_reading=unavailable`、`menu.source=transcript` 證實仍開啟的單題問句時才為 true。投影只帶布林，不帶問句與選項文字。只有 `state=waiting` 或 `work_person_needed` 不足以推論問句；缺欄表示無法分類。

狀態清單標記放在 `ss/<machine>/__clawdline_inventory_v1__`，其 JSON 是 `inventory:{version:1,sessions:[終端 ID]}`、`at`（Unix 秒）、`complete`（布林）及隨機 128-bit 的 `snapshot_generation`。每批狀態列都帶相同值，viewer 必須丟棄與標記不符的舊快取列；狀態集合改變或 heartbeat 時整批列先發、標記後發。舊 relay 拒絕 `ss/` 時，新頻道自行重試，不阻斷既有 `s/` 標記。狀態列的 `source.observed_at`、`projected_at` 與 `last_movement_at` 都是 Unix 秒，`no_progress_after_ms` 是毫秒。

私有 Cloud relay 的相容版 `b6d3740` 已部署，允許並預設推送 `ss/`，同時保留現有 `s/` 接收方式。`f7e309e` 已實作表中的目標接收規則，但尚未部署；Console 用 `ss/` 清單、按需訂閱 `s/` 詳情後，再啟用 `s/` 明確訂閱與 `read_transcript` 權限。權限收緊前 relay 仍依 `read_sessions` 接收完整 `s/`；不能把該 row 當成僅狀態資料。完整 `s/` 可選擇發布時仍帶 `execution_generation`，舊快取缺這欄時不得用於固定世代的內容讀取。

機器端世代以 Cloud machine id、終端 id 及隨機 128-bit 值固定；同一核心程序的 PID 與啟動時間持續可證時不變，確認消失再現或程序指紋變動時換新。`Server.AdmitExecutionTarget` 在新鮮的權威終端讀取後比對三者；`execution_target_required`、`execution_machine_mismatch`、`execution_target_missing`、`execution_source_unknown`、`execution_generation_changed`、`execution_check_unavailable` 均是拒絕，不可把來源未知當成空集合。呼叫端仍須另驗 viewer 能力與撤銷。沒有世代的舊版請求維持舊路徑行為，不具備固定目標保證。

新版 Cloud `transcript` 指令可加 `expected_generation`（保留原有 `session`、`limit` 及可選的 `priority`），`info` 指令可在 `session`、`parts` 旁加同一欄。Bridge 從已驗證的 `ctl/<machine>` 取得 machine id，轉成機器內部的兩個固定目標 header；HTTP 在內容讀取前後各驗一次，Bridge 在送出前再驗撤銷。舊版沒有世代的指令形狀仍按舊語意處理。

機器收到效果性請求時，傳輸層先用裝置簽章驗證身分，並以該機器的內容金鑰解密；執行前再核對同一把驗章公鑰仍在帳號名冊中。本機若有配對釘選，名冊公鑰也必須與釘選相同；本機撤銷優先於名冊。這讓在釘選檔建立前已完成配對、仍持有該機器內容金鑰的舊瀏覽器可以傳送，但只有帳號登入而沒有該機器金鑰的裝置仍無法送出可解密的請求。逐機 Cloud 命令開關另外在執行前檢查。

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

這句拒絕本身是從 spool 的保留額度進去的（`internal/adapters/cloud.spoolRefusalByteLimit`）。一條塞滿
的 channel 沒辦法用塞滿它的那條路說自己塞滿了，這是唯一的例外。

#### 9.5.2 Read refusal measurements

See the [dated read-refusal record](records/cloud-wire-implementation-2026-09.md#952-every-refused-read-is-told-and-small-answers-keep-a-reserve-2026-09-25).

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

### 10.3 Operations

See the [dated operation record](records/cloud-operations-2026-09.md).

For a live Session's `send`, `answer`/`key`, `interrupt`, `end`, or
`archive-session`, the Go machine now requires `request` (the caller's stable
Idempotency-Key) and `execution_generation` (the authoritative 32-digit
lowercase hex generation in the Session row). The machine checks the current
execution through `AdmitExecutionTarget` before performing an effect. A missing or
unprovable generation fails closed. An older machine without `session-receipt`
in its descriptor is not offered these durable operations by the console.

The SQLite request receipt uses `(viewer sender, machine, terminal Session,
execution_generation, operation, request)` as its identity. The canonical
request digest is a comparison field: repeating that identity with different
parameters returns `idempotency_key_reused`. Admission commits before the local
route runs. A completed answer is replayed; an orphaned admission is unknown
and is never run again. The read word `session-receipt` takes the same identity
plus a separate read correlation ID and reports `machine_execution` as
`missing`, `pending`, `completed`, `rejected`, or `unknown`. It deliberately
reports relay acceptance, relay delivery, viewer observation and viewer
acknowledgement as unknown: those are separate evidence domains, and the
machine's execution receipt cannot prove them. The console exposes this word
through `GET /v1/sessions/{id}/cloud-receipts/{request}` with `action` and
`execution_generation` query fields. A lost reply must be reconciled by this
read under the original request ID, never by minting another write ID.

### 10.4 派工就是 task.json

遠端派工的加密載荷帶的就是本機 orchestrator 已經在講的 wire format：**task.json 就是協定**。
收到的 Mac 用它自己的 claims、serialize、depth、capacity 與 dispatch policy 檢查，跟本機建立的任務
一模一樣，並在同一條通道上用同樣的 typed error 回答（PROTOCOL.md:262-270）。


The dated implementation mapping, differences, next-wave notes, and stage measurements are in the
[Cloud wire implementation record](records/cloud-wire-implementation-2026-09.md).
