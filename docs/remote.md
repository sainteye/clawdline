# 遠端：免費版與 Cloud 版

2026-09-17 使用者問「做完之後可以替換 app.clawdline.com 連到它使用了嗎」，並提醒「我們還有分免費版和
Cloud 版本，裝置配對等等」。這份文件記下當時量到的差距與設計原則，後面的派工照這裡走。

## 三條路徑，Go 版目前只有第一條

| 路徑 | 版本 | 舊版靠什麼 | Go 版（2026-09-17，master ef77892） |
|---|---|---|---|
| 本機瀏覽器／Mac 視窗 | 免費 | RemoteServer（只綁 loopback） | 有；20 條路由（舊版 89 條）；Session 頁已接近 1:1 |
| 自己的 cloudflared tunnel＋手機配對 | 免費 | RemoteAuth 541、RemoteTunnel 742、RemotePage 1,267、RemoteQR 44 行，加上 RemoteServer（5,976 行）裡的閘門 | 有（2026-09-19）：閘門與配對核心、網頁的配對頁、tunnel。見下方「配對頁與 tunnel」 |
| app.clawdline.com | Cloud | 18 個 `Cloud*.swift` 共 22,345 行＋WebPush 858 行；網頁端 `net/cloud-*.js` 共 7,796 行 | 0 行 |

量法：`wc -l ~/code/clawdline/Sources/Cloud*.swift` 等，在 `~/code/clawdline` 的工作樹上量；路由數是
`grep -rhoE '"/v1/[^"]+"' Sources/ | sort -u | wc -l`，是字面路徑的數量，不是 handler 的數量。

在配對與認證落地之前，Go daemon **沒有任何認證**：預設只綁 127.0.0.1，改綁其他位址只會印一行警告。
所以在那之前不能開 tunnel，也不能綁非 loopback。

### Cloud 這條還缺什麼

舊版 Cloud bridge 回應 27 種操作（`CloudAppBridge.swift`、`CloudV2Protocol.swift` 裡的 `case "…"`）。
Go 版有對應本機功能的約 7 種：send、end、info、transcript、dispatch，以及唯讀的 schedules、board。
其餘 20 種沒有：agent、answer、board.items、diagnostics.events、diagnostics.report、document、documents、
focus、git、image、places、resume、schedule、screen、shell、skills、snippets、start、timeline、voice。

底下還要一層傳輸：帳號、裝置配對與核准、金鑰、端對端加密的封包、canonical JSON、指令帳本（重送去重）、
送出佇列、交接、推播。接縫合約是 Cloud 服務（the cloud service）的協定文件（`PROTOCOL.md`，不公開）；這一邊照它寫下的公開規格是 `docs/cloud-wire.md`。

## 設計原則

1. **免費版不依賴 Cloud。** Cloud 是選配的 adapter，預設關閉。沒有帳號也要能完整使用單台 Mac：
   本機頁、自己的 tunnel、六位數配對。多機（挑機器、角色移交、跨機派工）只在 Cloud 路徑，
   照 2026-09-15 的決定；Cloud 只帶密文，不做需要明文的決策。
2. **hosted 網頁不用換。** Go 版只要照 PROTOCOL.md 跟 relay 溝通，現在的 app.clawdline.com 不必改就能連上。
   hosted 要不要換成 React 版，之後另外決定。
3. **Go 版要有自己的裝置身分。** 不沿用舊 app 的身分，否則兩個 daemon 會搶同一台 Mac 的序號與重送。
   測試時讓 Go 版以「第二台機器」配對，舊 app 照常運作。**把 Go 版配進使用者真正的 Cloud 帳號之前要先問他**：
   這會動到他的帳號，也要他在已信任的裝置上按核准。
4. **金鑰儲存要跨平台。** 舊版 `CloudKeys.swift` 有 63 處直接呼叫 Keychain。Go 版的配對核心這一版先用
   0600 檔案，留一個接縫；之後換成 macOS Keychain、Windows Credential Manager、Linux Secret Service，
   沒有桌面環境的 Linux 仍退回 0600 檔案，並在設定頁寫明。
5. **配對的承諾照搬**（`~/code/clawdline/docs/remote.md` 的 How a device is paired）：碼只顯示在本機，
   不出現在回應、log、audit；最多猜 5 次、2 分鐘失效、一次一個、10 分鐘 3 次；token 以 constant-time 比對、
   只存雜湊；寫入是另一個權限，預設關閉；本機也沒有例外；還沒有配對任何裝置之前 tunnel 不准啟動。
   沒有原生殼的平台（Linux、Windows），碼由 CLI 在終端印出。

## 順序

1. 設定、用量、專案三頁（已落地：c9f60c1、303c9e4、ef77892）。
2. 免費版配對與認證的 Go 核心，加上原生殼的最小接線（token、配對 alert）。在隔離的 worktree 裡做，
   這樣閘門不會在其他 child 量 7727 的途中上線。**已合進 master（`5cca6b1`）。** Codex 的安全審查
   （一次獨立的 review task）找到 1 high、4 medium、1 low，全部修正後才合併（`f2caa6a`）。下一次重建 7727 閘門就會生效，
   瀏覽器要用 `clawdline open` 取得 cookie（見 replica.md 的量測段落）。
   刻意的取捨：密碼錯誤（24 小時 10 次）與配對猜錯（24 小時 5 次）是全體共用的額度，連得到 port 的人可以讓
   密碼登入或新配對停一天（已配對的裝置不受影響）；額度只存在記憶體，重啟會歸零。
   配對猜錯次數跨配對累計，**與 Swift 不同**：Swift 每個配對各 5 次、換新配對就歸零，舊 app 仍有這個缺口。
   已知還沒做：React console 的配對畫面、Dashboard 的派工按鈕（會 403）、send 的 Idempotency-Key、
   公開 health 的版本、真實 cloudflared 與原生殼寫入的驗證。
3. 配對的網頁入口（照抄 `door/door.js`、`door.css`）與原生殼的 Remote 設定。**已做**，見下一節。
4. tunnel：跑使用者自己安裝的 cloudflared，一律帶 `--config`，還沒配對任何裝置就拒絕啟動。**已做**，見下一節。
5. Cloud bridge：先讀 PROTOCOL.md，拆成傳輸與加密一波、27 種操作分兩三波，用假的 relay 測。

## 配對頁與 tunnel（2026-09-19）

### 配對頁

- `web/console/src/legacy/js/door/door.js` 是舊版原檔的逐位元組拷貝，登記在 `MANIFEST.json`，
  `tools/check-legacy-css.sh` 會比對；`door.css` 早就在。React 的 `web/console/src/door/Door.tsx`
  不 import 它（它 import 整個舊頁面、在 import 時查 id），而是逐行照它寫，產出 `index.html` 那段門口相同的
  id、class、屬性，字是 `view/static.js` 貼進去的那些 `webDoor*` 鍵。
- **什麼時候出現**照舊版 `net/live.js` 的 `check`：開頁先讀公開的 `/v1/health`（不走快取），只有明確的
  `authed: false` 才顯示門口；health 讀不到不是權限問題，照常畫 console。為此公開的 health 多了兩個欄位，
  也是舊版就有的：`authed`（這個請求帶的憑證是否被放行，只關於發問者）與 `password`（有沒有密碼這扇門）。
  兩者都不含路徑、port 或工作內容，F-06 的性質不變；`TestHealthAndDiagnostics` 釘住完整的欄位集合。
- 還沒被放進來之前 console 完全不掛載（不讀清單、不開事件串流）。頁面開著時裝置被撤銷，門口在下一次檢查
  （回到前景、網路恢復、每 30 秒）時蓋在 console 上面；重新進來後整頁重載。
- 三個狀態與密碼那一路照舊。和舊版不同的一處：猜錯碼的拒絕在這裡叫 `wrong_code`（舊版叫 `forbidden`），
  而且帶 `tries_left`（這個 daemon 跨配對、24 小時計算），用完就結束這次配對。

### tunnel

`internal/adapters/tunnel` 是 `RemoteTunnel.swift` 的規則，`internal/transport/http/tunnel.go` 是接線。

- **跑使用者自己裝的 cloudflared**：`cloudflared_path`（只能手改 config.json，設定路由不收，因為它指名一支
  會被執行的程式）→ Homebrew／MacPorts 的位置 → PATH。
- **一律帶 `--config`**，指向 `CLAWDLINE_NEXT_DIR/cloudflared.yml`（0600，每次啟動前重寫）；
  查 named tunnel 憑證的 `cloudflared tunnel list` 也帶。從來不讀 `~/.cloudflared/config.yml`。
- **還沒有人被放進來就拒絕啟動**：判斷讀的是閘門自己的 authority（`IsConfigured`：一台非本機的已核准裝置，
  或設了密碼——與 Swift 相同），而且在每次可能改變答案的時候重讀：daemon 啟動、`/v1/settings` 寫了
  `remote`／`remote_tunnel`／`remote_hostname`、`/v1/auth/` 下的配對完成、密碼登入、登出與所有裝置路由。
  撤銷到沒有裝置時，正在跑的 tunnel 會被關掉。另外 `remote`（「讓瀏覽器或你的手機看得到你的 session」）
  關著也拒絕：Swift 的理由（本機 server 沒開）在這裡不成立，保留的理由是它是使用者對「可被這台機器以外連到」
  的同意。
- **兩種模式**：`quick`（`--url http://127.0.0.1:<port>`，位址從 banner 讀，等到第一條連線註冊才公布）與
  `named`（`run <remote_tunnel_name>`，位址是 `https://<remote_hostname>`，YAML 裡只有這一條 ingress
  加上 404）。所有來自設定的值在 YAML 裡都是雙引號字串，名稱與 hostname 另有格式檢查。
- **看得見**：`GET /v1/tunnel`（只有本機 token；quick tunnel 的位址本身就是通行證）、`clawdline tunnel`、
  設定視窗「遠端」分頁 hostname 下方的卡片（off／…／位址／原因，字照 `Settings.swift` 的 `refreshTunnel`）。
- **閘門的 hostname 跟著同一次讀取更新**，named tunnel 改了 hostname 不必重啟 daemon。
- **不留孤兒**：SIGINT／SIGTERM 時先收掉 cloudflared 再把訊號照原樣重拋；SIGKILL 接不到，所以子行程的 pid
  記在 `cloudflared.pid`，下次啟動時只有在該 pid 的命令列確實帶著這個目錄的 `--config` 時才停掉它，
  讀不到行程表就什麼都不做。
- 已知沒做：真的 cloudflared 連到 Cloudflare 的那一段沒有實測（驗收只到「被正確呼叫、參數正確」與真 binary
  的 `--help` 認得這些參數）；Windows 上不收孤兒（沒有 `ps`）；`quick` 位址會寫進 daemon log 一次（同 Swift）。
