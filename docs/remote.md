# 遠端：免費版與 Cloud 版

2026-09-17 使用者問「做完之後可以替換 app.clawdline.com 連到它使用了嗎」，並提醒「我們還有分免費版和
Cloud 版本，裝置配對等等」。這份文件記下當時量到的差距與設計原則，後面的派工照這裡走。

## 三條路徑，Go 版目前只有第一條

| 路徑 | 版本 | 舊版靠什麼 | Go 版（2026-09-17，master ef77892） |
|---|---|---|---|
| 本機瀏覽器／Mac 視窗 | 免費 | RemoteServer（只綁 loopback） | 有；20 條路由（舊版 89 條）；Session 頁已接近 1:1 |
| 自己的 cloudflared tunnel＋手機配對 | 免費 | RemoteAuth 541、RemoteTunnel 742、RemotePage 1,267、RemoteQR 44 行，加上 RemoteServer（5,976 行）裡的閘門 | 沒有；配對核心正在做 |
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
送出佇列、交接、推播。接縫合約是 `~/code/clawdline-cloud/docs/PROTOCOL.md`。

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
   這樣閘門不會在其他 child 量 7727 的途中上線。
3. 配對的網頁入口（照抄 `door/door.js`、`door.css`）與原生殼的 Remote 設定。
4. tunnel：跑使用者自己安裝的 cloudflared，一律帶 `--config`，還沒配對任何裝置就拒絕啟動。
5. Cloud bridge：先讀 PROTOCOL.md，拆成傳輸與加密一波、27 種操作分兩三波，用假的 relay 測。
