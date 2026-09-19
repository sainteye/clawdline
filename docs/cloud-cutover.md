# Cloud 正式連線：操作手冊

> 把新版（clawdline-go，7727）接上 `app.clawdline.com` 的正式環境。**照順序做，一步做完再做下一步。**
> 會動到帳號的是步驟 1、2、4、5，**全部由使用者本人做**；步驟 0、3 是維護 daemon 的人（root session）做，
> 不碰帳號。截至 2026-09-19，這一版**一個位元組都沒送到正式環境過**，下面的「會看到什麼」
> 抄自正式端的原始碼（`~/code/clawdline-cloud`）與本機 stand-in 的實錄，不是正式端量來的。
> **前提**：7727 跑的 binary 已含這一波（`clawdline cloud preflight` 存在）；舊 binary 的錯誤訊息沒有下面的類別名。

## 先知道的三件事

1. **舊 app 的 Cloud 線不會被斷。** 新版以「第二台 Mac」加入帳號：自己的金鑰與身分放在
   `~/.config/clawdline-next/cloud/`，程式拒絕讀寫舊 app 的 `~/.config/clawdline`；兩台用不同的 machine id，
   序號互不相干；瀏覽器的內容金鑰按機器分開存（`clawdline.machine-master:<帳號>:<機器>`）。
   relay 在方案滿了時**拒絕新的那一條**，不會踢掉已經連著的舊 Mac（`relay/src/lib/entitlements.ts:197-205`）。
2. **免費方案只能登記 1 台 Mac。** 舊 app 已經占了那一格時，步驟 2 的核准頁會失敗，訊息是
   `free allows 1 machine(s)`（`machine_limit_reached`）——這是方案，不是故障。先做步驟 1。
3. **兩台 Mac 都配對到同一個瀏覽器以後**，hosted console 對「沒指定哪台 Mac」的請求（新的推播訂閱、
   不在 session 裡的語音）會回 `cloud_machine_ambiguous`（舊 repo `docs/cloud.md:758-761, 1140-1144`）。
   已經在舊 app 上的推播訂閱照舊（推播由 Mac 直送）。這是步驟 4 的代價，做之前要知道。

## 步驟

| # | 誰 | 做什麼 | 做了會發生什麼 | 怎麼回退 |
|---|---|---|---|---|
| 0 | root | `~/code/clawdline-go/bin/clawdline cloud preflight` | **不送任何網路請求**。每列 ok／warn／block；全部沒有 block 時最後一行 `next` 會說「the person's step」。有 block（例如還留著本機測試的 `cloud_api_base`）就先清掉 | 不必回退：唯一的寫入是建立空的 0700 金鑰目錄 |
| 1 | 使用者 | 瀏覽器開 `https://app.clawdline.com` →「方案」頁 → 看「Mac 數量」 | 什麼都不改。**1** 且舊 app 已登記 → 三選一：升級 Pro（5 台）、等舊 app 退役、撤掉舊 Mac（舊 app 會失去 Cloud）。這是你的決定，機器不替你選 | — |
| 2 | 使用者 | 在這台 Mac 的終端機打 `~/code/clawdline-go/bin/clawdline cloud login`。它印出 `code` 與 `approve at <網址>` → 在瀏覽器開那個網址 → 頁面問「Is this the same code?」，核對跟終端機一樣 → 按 **Continue with GitHub** → 看到「**<名稱> is linked.**」 | 帳號多一台 Mac。終端機接著印 `account`、`machine`、`saved`，最後一行 `verified the control plane issued a device token` 表示核准真的生效（只拿 token，不連 relay） | `clawdline cloud off`，刪 `~/.config/clawdline-next/cloud/machine-identity-v1.json`。**帳號那一列目前沒有按鈕可刪**（hosted console 沒有 machine 撤銷介面），它會留著占一格；**不要刪 `device-ed25519-v1`**——同一把金鑰重新 login 會沿用同一列，不再多占一格 |
| 3 | root | `clawdline cloud on`，然後重啟 7727 的 daemon（現在是 `~/code/clawdline-go` 下的 `./bin/clawdline serve`；開關只在 daemon 啟動時讀） | daemon 自己連上 relay。7727 設定頁「遠端」→「Cloud 狀態」顯示「連線：connected」；`bin/serve.log` 有 `cloud ready account=… machine=…` | `clawdline cloud off`，重啟 daemon。帳號與舊 app 都不受影響 |
| 4 | 使用者 | 7727 設定頁「遠端」→「配對瀏覽器」→ **產生配對連結** → 在**已登入同一帳號**的瀏覽器開那個連結 → 核對兩邊顯示的金鑰指紋一樣 | 那個瀏覽器拿到這台 Mac 的內容金鑰，看得到新 Mac 的 session；「配對瀏覽器」列出它，標「由這台 Mac 配對」。從此同一個瀏覽器看得到兩台 Mac（見上面第 3 點） | 設定頁那一列按「撤銷」，或 `clawdline cloud revoke <device-id>`：本機撤銷優先於帳號清單 |
| 5 | 使用者決定 | `clawdline cloud commands on` | 已配對的瀏覽器可以對這台 Mac 的 session 打字、開新 session。**預設關，不做這步也能看** | `clawdline cloud commands off`，下一個請求就生效，不必重啟 |

## 失敗時怎麼看

- **核准頁**（步驟 2）：失敗時標題是「The approval did not finish.」，下面一行是控制面的原話，例如 `free allows 1 machine(s)`；
  這時終端機那邊只會一直等到 `login_expired`／`login_timeout`——Mac 看不到核准頁為什麼失敗，所以兩邊都要看。
- **終端機**（步驟 2、`cloud connect`）：`clawdline: <類別> (<代碼>): <細節>`，下一行 `what to do` 是該做的事。
- **daemon**（步驟 3 以後）：設定頁「Cloud 狀態」卡的紅字就是 `last_error`，開頭是類別；
  `GET /v1/cloud/status`（本機 token）另有 `last_error_kind` 與 `last_close_kind`；log 是 `cloud reconnect waiting reason=<代碼>`。

| 類別 | 常見代碼 | 意思 | 怎麼辦 | 線會自己重試？ |
|---|---|---|---|---|
| `not_signed_in` 帳號沒登入 | `no_identity`、`identity_other_environment`、`api_no_session` | 這台 Mac 沒有身分、身分屬於別的環境（例如本機測試）、或控制面不認得它的憑證 | 重做步驟 2 | 否，停下 |
| `device_not_approved` 裝置未核准 | `login_denied`、`login_expired`、`login_timeout`、`relay_forbidden` | 核准被拒、code 過期、沒人按、或帳號撤銷了這台 Mac | 重做步驟 2；核准頁若寫 `allows 1 machine(s)` 就是下一列 | 否 |
| `entitlement` 方案不足 | 核准頁的 `machine_limit_reached`；連線的 `relay_over_capacity` | 方案的 Mac 數滿了，或同時連線數滿了 | 回步驟 1。舊 Mac 不受影響 | 是，每 ≤30 秒 |
| `version_mismatch` 版本不合 | `api_not_found`、`incompatible`、`upgrade_refused_426`、`relay_bad_request` | 對面不是這一版協定，或根本不是 Clawdline 端點 | 確認設定檔沒有殘留 `cloud_api_base`／`cloud_relay_url`（步驟 0 會擋），再更新 build | 是 |
| `relay_refused` relay 拒絕 | `relay_unauthorized`、`identity_binding`、`relay_handshake_timeout` | relay 不收這張 token：api 與 relay 是不同環境，或系統時鐘差太多 | 同上檢查設定；確認時鐘 | 是（`relay_unauthorized` 每次重拿一張 token） |
| `unavailable` 連不上 | `unreachable`、`tls_untrusted`、`connection_failed`、`relay_bad_gateway` | 沒人說不，是沒人回答 | 等；看網路、代理、時鐘 | 是 |

## 在本機重現每一種錯誤（不碰正式環境、不碰任何帳號）

- 自動：`go test ./internal/adapters/cloud -run 'Production|EveryWay|Untrusted|Harness|EveryFailureWord'`。
  測試在本機假扮 `api.clawdline.com` 與 `relay.clawdline.com`（只有測試信任的 CA、正式的主機名與 SNI），
  撥號器只接這兩個名字、其餘一律拒絕並記錄。
- 手動看真的 CLI 印什麼：照 `internal/adapters/cloud/standin_test.go` 檔頭的指令在 127.0.0.1 起 stand-in，
  對一個**拋棄式**的 `CLAWDLINE_NEXT_DIR` 跑 `cloud login`／`cloud connect`，用 `/__scenario?name=…` 切換 12 種回答。
