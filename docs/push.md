# 推播：手機收得到通知

> 舊 app 退役之後，手機要照樣收到通知。這份文件是 Go 版 Web Push 的做法、與舊版的差異，
> 以及 2026-09-18 的本機實測結果。
>
> 相關：`docs/cross-platform.md` §4.6（通知有三個出口）、`docs/replica.md`（復刻的方法與閘門）。

## 為什麼只有 Web Push

跨平台盤點已經量過一件對這條線很重要的事：**舊 Swift app 根本沒有用 macOS 本機通知中心**
（`grep -rn "UNUserNotificationCenter|NSUserNotification" Sources/` 零筆）。它唯一會主動離開
那台機器的東西就是 Web Push。所以「通知」這個功能**天生跨平台**——訂閱在瀏覽器端，daemon
只要能對外發 HTTPS，macOS、Linux、Windows 一模一樣。

這也是為什麼這裡沒有「本機桌面通知」：舊版沒有，復刻就沒有。`cross-platform.md` §4.6 把它
列為之後的加值項，不是這一波的缺口。

## 三個 RFC，以及它們釘死的數字

| | 是什麼 | 在哪裡 |
|---|---|---|
| RFC 8291 | 用瀏覽器的公鑰與 `auth` 秘密把內容封起來，push service 讀不到 | `internal/adapters/push/encrypt.go` |
| RFC 8188 | `aes128gcm` 的那一筆 record：`salt(16) ‖ rs(4) ‖ idlen(1) ‖ keyid(65)` | 同上 |
| RFC 8292 | VAPID：ES256 的 JWT ＋ 驗它的公鑰 | `internal/adapters/push/vapid.go` |
| RFC 8030 | `TTL`、`Urgency`、`Topic`，以及 404/410 的意思 | `internal/adapters/push/send.go` |

數字照舊版（`Sources/WebPush.swift`）一字不改：record size 4096、payload 上限 3993、
TTL 3600 秒、`Urgency: high`、token 壽命 12 小時、`sub` 是
`https://github.com/sainteye/clawdline`。每一個為什麼是這個值，註解寫在 `push.go` 常數旁邊。

## 兩個東西不會離開這台機器

**VAPID 私鑰是身分，不是 session。** push service 會把訂閱綁在當初訂閱用的 application
server key 上；換一把新的，所有既有訂閱就開始失敗——而且是**失敗在 push service 那一端**，
回一個沒人在看的 403，使用者體感就是「手機忽然不響了」。所以私鑰只產生一次，0600 寫進
`$CLAWDLINE_NEXT_DIR/push/vapid-p256-v1`，之後每次開機讀回來。

**訂閱是一個 endpoint。** 那是「這台機器每次 session 狀態變化就會從自己的網路裡面 POST
過去」的網址，所以未經檢查的 endpoint 等於把一個 request-forgery 的把手交給任何拿得到
token 的人。`FromBrowser` 只收 https、要有真的 host、`p256dh` 必須是 65 octets 且開頭
`0x04`、`auth` 必須是 16 octets。**刻意不是廠商白名單**：Apple、Mozilla、Google 與自架的
都要能用，要守的性質是「不是明文、不是會打到本機的 scheme」，不是「我認得這家」。

### 檔案

```
$CLAWDLINE_NEXT_DIR/push/            0700
  vapid-p256-v1                      0600  32 octets 的私鑰純量，base64 一行
  subscriptions.json                 0600  瀏覽器交出來的那幾列
```

規矩照 `internal/adapters/cloudkeys`：目錄 0700、檔案 0600（**每次寫都設一次**，因為
atomic write 換掉的檔案不會繼承舊的 mode）、先寫暫存檔再 rename、開檔一律
`O_NOFOLLOW` 且前後各檢查一次 inode、**絕不碰 `~/.config/clawdline`**。

**分成兩個檔是刻意的，舊版是一個。** 舊版把 VAPID 私鑰跟所有訂閱放在同一個 `push.json`，
所以一份壞掉的訂閱清單會把身分一起帶走，每一支配對過的手機就靜靜不再響。這裡金鑰自己一個
檔：訂閱清單壞掉只賠上訂閱。

**「讀不到」永遠不等於「沒有」。** 這是 cloudkeys 那條最重要的規則。但它有一個刻意的例外，
跟舊版同一個理由：金鑰檔**存在、而且內容不是一把金鑰**時會重新產生一把，並且用最大聲的方式
寫進 log——因為那些訂閱在那一刻就已經全部死了（簽不出它們認得的金鑰），永遠拒絕只會讓這台
機器再也通知不了任何人。至於**讀不到**（symlink、I/O 錯誤、檔案大得離譜）一律是 refusal，
絕不重新產生：那可能只是一時的故障，而重新產生會丟掉一個還在的身分。

## 路由

四條，全部在閘門後面，全部是 read-level。

| | | |
|---|---|---|
| `GET /v1/push/key` | application server key（base64url 的未壓縮公鑰點） | 問它就是產生它的時機 |
| `POST /v1/push/subscribe` | 瀏覽器的 `PushSubscription.toJSON()`，原樣上傳 | 回 `{ok, id}` |
| `POST /v1/push/test` | 送一則測試通知給**發問的那台裝置** | 沒訂閱回 `409 not_subscribed` |
| `POST /v1/push/unsubscribe` | 用 id 收回一列 | 只收得回自己那台裝置的 |

**訂閱刻意不走 write gate。** 那道閘是「往別人的 session 裡打字」，而「有 session 在等你的
時候告訴我」剛好是它的相反：是讀的那一半從另一條路來。一支只配對成唯讀的手機，正是這個功能
存在的理由。

**測試按鈕只會響發問的那一台。** 一個爆炸半徑比被測物還大的測試，會教人不敢按它，而那正好
是測試按鈕的反面。`session_id` 是選填的，它把一盞燈變成一個迴圈：沒有它，按鈕只回答了半個
問題（*通知到了嗎*），而真正會壞的是另外半個（*點下去會不會回到我的 session*）。daemon 會
拿那個 id 去對自己正在看的 session 比對，對不上就退回清單，所以送一個過期的 id 是安全的。

**一個瀏覽器收不回另一個瀏覽器的訂閱。** id 不是秘密——它是這台機器給那一列的名字，而且本來
就會回給產生它的那一頁。擋住它變成把手的是：只有擁有那一列的裝置（或這台機器自己的 token）
才移除得掉。

## 退避與重試——**這一段是刻意跟舊版不同**

舊版送一次就結束：2xx 算送到，404／410 把訂閱丟掉，其他一律原封不動、寫一行 log。
這裡多了一層有界的重試，因為筆電上真正會發生的失敗就是那一種——push service 回 503、
或者 Wi-Fi 換手把一個 request 吃掉，那都不是「這個訂閱死了」的證據，而舊版對它的答案是
一則永遠不會到的通知加一行沒人讀的 log。

要避免的是重試風暴，所以它被三面綁住：

- **次數**：`DefaultAttempts = 3`（一次加兩次重試）。
- **間隔**：1 秒起、指數成長、上限 8 秒，**full jitter**（同一秒回來的重試是第二個 request，
  不是第二次機會）。`Retry-After` 有給就照它的，秒數與 HTTP-date 兩種拼法都讀。
- **總預算**：一個訂閱的所有等待加起來 30 秒；`Retry-After` 超過 60 秒直接放棄，因為那則訊息
  在重試落地前就過期了。

分類（`send.go`）：

| 回應 | 動作 |
|---|---|
| 2xx | 送到 |
| 404、410 | **丟掉這個訂閱**（RFC 8030 §7.3：resource 不在了，而且不會回來） |
| 408、429、500、502、503、504、連線錯誤 | 重試 |
| 其他（400、401、403、413…） | 原封不動。比漏掉一則通知更糟的，是因為某家服務下午不順就把人默默退訂 |

**每一次 attempt 都重新封一次。** 重試不是把同一段 ciphertext 再 POST 一遍：salt 與 ephemeral
key pair 都是每則訊息、每個訂閱、**每次嘗試**重新產生的，所以 one-key-one-nonce 這條規則對
push service 可能只收到一半的那些 bytes 也成立。

## 前端

| | |
|---|---|
| `web/console/src/push/api.ts` | 四個呼叫。訂閱**原樣**上傳——那是瀏覽器自己的物件，這一端改過的形狀都是把 credential 弄錯的機會 |
| `web/console/src/push/push.ts` | `input/push.js` ＋ `input/settings.js` 的通知那一半，**移植**而非照抄 |
| `web/console/public/sw.js` | service worker，**逐位元組照抄**自 `RemotePage.serviceWorker()` |
| `web/console/src/Sessions.tsx` | 清單頁底下那一條 `#notify` |
| `web/console/src/pages/settings.tsx` | 設定頁的「通知」區塊 |

移植而不是照抄，理由跟 `legacy/settings-bridge.ts` 寫的一樣：`input/*.js` 在載入時就把
listener 綁到它自己查出來的元素上，而這裡的元素是 React 的。每一條規則都還是它們的；
改掉的只是「畫」變成一個 subscription。

**四種狀態只有一種是「開」，所以畫面要說是哪一種。** 按下去卻什麼也沒發生是四種裡最糟的
那一種——那正是「使用者拒絕過權限」從頁面裡面看起來的樣子，而它的解藥在瀏覽器自己的設定裡，
所以那是一句話而不是一個開關。

**iOS 只有從主畫面打開才有。** 不是「比較難用」——Safari 分頁裡那個 API 根本不存在，所以沒有
按鈕可按、事後也沒有東西可以解釋。那一句話本身就是這個功能的全部，直到使用者讀過它為止。

**這個 console 還多一種舊版頁面不必說的情形**：service worker 需要 secure context，
`http://127.0.0.1:7727` 是，`http://192.168.x.x:7727` 不是。所以從網路上另一台機器連進來的
瀏覽器在這裡答 `unsupported`——正確，而且是這一頁上沒有人能修的理由。
（`cross-platform.md` §4.6 限制 1。要讓它可用，走 tunnel 的 https。）

## PWA 的外殼

退役盤點實測到 7727 上 `/sw.js`、`/manifest.webmanifest`、`/icon-192.png`、`/favicon.ico`
全部 404，而 7717 全部 200：**閘門的公開清單早就放行這些路徑，只是後面沒有東西回應**。
一個瀏覽器是在頁面自己的憑證之外、在它還不知道誰是誰之前就去要 favicon 與 manifest 的，
所以把它們放在閘門後面等於一個沒有圖示的主畫面。現在補齊：

| 路徑 | 怎麼來的 |
|---|---|
| `/sw.js` | 逐位元組照抄 `RemotePage.serviceWorker()`，`tools/extract-service-worker.py` 抽的，`--check` 驗得出漂移 |
| `/manifest.webmanifest` | 488 bytes，照抄 |
| `/icon-192.png`、`/icon-512.png`、`/favicon.ico` | 照抄。這三個裡面有一個圓角方塊，AppKit 那一條曲線的 rasteriser 不是移植能對到像素的東西，所以直接拿 bytes |
| `/splash-<寬>x<高>.png` | **移植**（`internal/domain/icon/app.go`）。二十種幾何、每年秋天還會長，而且塞進 repo 是 1.1MB 的 PNG |

四個照抄的檔在 `web/console/public/`，由 `page` 送出去；它們的來源記在
`web/console/src/legacy/MANIFEST.json` 的 `inlined` 區塊——`tools/check-legacy-css.sh` 檢查
不到這些（它比對 `Resources/web` 底下的檔案，而這些是一個函式畫出來的）。

啟動圖是這裡唯一畫的，而且非畫不可。它是平底色上的一堆軸對齊矩形，所以 `icon.Splash` 是忠實
移植而不是近似：座標會落在半個像素上（1179 寬、cell 19 → 起點 x=437.5），而軸對齊矩形對一個
像素的覆蓋率就是兩個重疊長度的乘積，所以那是答案本身，不是它的近似。實測見下。

`index.html` 補上舊版那一整組 head：`apple-mobile-web-app-capable`、標題、狀態列樣式、
`theme-color`、manifest link，以及二十個 `apple-touch-startup-image`。
**viewport 也改回舊版那一行**（`maximum-scale=1,user-scalable=no`，**沒有** `viewport-fit=cover`）：
console 原本寫了 `viewport-fit=cover`，而它抄來的那份 stylesheet 在 `legacy/tokens.css` 與
`legacy/composer.css` 裡白紙黑字寫著「這一頁沒有任何 safe-area inset」。舊版 index.html 的註解
記了實測：加上 `cover` 之後，iPhone 15 Pro 上加到主畫面的視窗每一頁底下都會多出 59 點的死區
（那正好是**上**方的 inset）。`viewport` meta 在桌面瀏覽器不生效，所以這個改動不影響已經量過的
1:1 比對。

## 還沒接的線

- **沒有東西會在 session 開始等待時自動送通知。** sender 與路由都在，`Server.PushSend` 就是那個
  接縫（`internal/transport/http/push.go`），但舊版在 `StateHook.swift`、`DeployWatch.swift`、
  `Orchestrator.swift`、`SmartNotification.swift` 四個地方呼叫它，那四條線在 Go 版還沒有對應的
  觀察點。現在唯一會送出通知的是設定頁那顆測試按鈕。
- **通知上的專案圖示。** 舊版帶 `icon: /project-<size>-<packed>.png`，那是一條動態路由
  （`RemoteIcon.project`）。`Notification.Icon` 欄位已經在，路由還沒有。
- **撤銷裝置不會連帶清掉它的訂閱。** `Store.RemoveDevice` 寫好了也測了，但 `/v1/auth/devices/{id}/revoke`
  還沒呼叫它（那個 handler 不在這次可改的檔裡）。舊版在 `RemoteAuth.swift:349` 做這件事。
- **iPad 沒有啟動圖**，跟舊版一樣：那二十個 media query 都是 iPhone 的。

## 2026-09-18 的實測

自己的 port（7731）、自己的 `CLAWDLINE_NEXT_DIR`、一個全新的 Chrome profile，**沒有碰**
使用者的瀏覽器，也沒有碰舊 app 的訂閱資料。

1. `clawdline open --print` 發一個 browser 裝置，開 console。`isSecureContext=true`，
   `Notification.permission=granted`（用 CDP 的 `Browser.grantPermissions` 事先給的，
   因為權限視窗是瀏覽器的 chrome，這裡沒有人可以按）。
2. 按清單頁底下的「通知我」→ service worker 註冊（scope `http://127.0.0.1:7731/`、active）
   → `GET /v1/push/key` → `pushManager.subscribe` 拿到 **`fcm.googleapis.com`** 的真 endpoint
   → `POST /v1/push/subscribe` 存成一列。
3. 按設定頁的「送一則測試」→ daemon log：`push: Clawdline → 1 sent, 0 failed`。
4. 回到頁面問瀏覽器 `registration.getNotifications()`：

   ```json
   [{"title":"Clawdline","body":"這是一則測試通知，該接的都接好了。",
     "tag":"clawdline","data":{"url":"/"}}]
   ```

   **這一筆才是證據**：bytes 離開這台機器、到了 Google 的 push service、回到瀏覽器、解密、
   叫醒 service worker、變成一則通知。

檔案權限實測：`push/` 是 `drwx------`，兩個檔都是 `-rw-------`。
稽核記了 `push.subscribe`（含 `host: fcm.googleapis.com`）與 `push.test`。

閘門實測：沒帶 token 的 `/v1/push/key` 是 401；未知的 `/v1/push/nope` 是 404；
沒訂閱的 `/v1/push/test` 是 `409 not_subscribed`；`endpoint` 是 `http://` 的訂閱是
`400 bad_request`；`application/x-www-form-urlencoded` 的寫入是 `415`。

PWA 外殼實測（7731）：

| 路徑 | 狀態 | Content-Type |
|---|---|---|
| `/sw.js` | 200（3651 bytes，與 Swift 抽出來的一模一樣） | `text/javascript; charset=utf-8` |
| `/manifest.webmanifest` | 200（488 bytes） | `application/manifest+json; charset=utf-8` |
| `/icon-192.png`、`/icon-512.png` | 200（4770、13710 bytes） | `image/png` |
| `/favicon.ico` | 200（1831 bytes） | `image/x-icon` |
| `/splash-1179x2556.png`、`/splash-2556x1179.png` | 200 | `image/png` |

啟動圖與舊版逐像素比對（Go 解碼兩張 PNG 全幅掃過）：

| 幾何 | 不同的像素 | 最大單通道差 |
|---|---|---|
| 2556x1179 | 0 / 3,013,524 | 0 |
| 750x1334 | 0 / 1,000,500 | 0 |
| 1179x2556 | 5,049 / 3,013,524（0.17%） | 3 |
| 1320x2868 | 2,856 / 3,785,760（0.075%） | 2 |

兩種幾何與 AppKit 完全相同，另外兩種只差在 cell 落在半像素上的那一圈邊，差 2–3/255，
肉眼看不出來。**沒有比對過的：另外十六種幾何。**

## 沒量到的

- 真的 iPhone。整條線是在 Chrome 上驗的，**Safari 與 Declarative Web Push 那一條路徑
  （`application/notification+json`、`navigate`）只有單元測試，沒有真機**。
- `Retry-After`、404/410 掉訂閱、429/503 重試，都是對 `httptest` 的假 push service 驗的，
  不是對真的服務。
- 加到主畫面之後的樣子：manifest 與二十個 startup image 的路由都回 200 了，但**沒有真的在
  iPhone 上加過一次**。上面那個 59 點死區的實測是舊版 index.html 的註解記的，不是這一波量的。
