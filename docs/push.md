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

退役盤點在 2026-09-18 實測到 7727 上 `/sw.js`、`/manifest.webmanifest`、`/icon-192.png`、
`/favicon.ico` 全部 404；
同一天（2026-09-18）對還在跑的舊 app（7717）量，四條全部 200：**閘門的公開清單早就放行這些路徑，
只是後面沒有東西回應**。（舊 app 已於 2026-09-19 停用，右邊那一欄現在沒有東西可以再量。）
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

## 有人在等你回答（2026-09-21）

在這之前，每一則「有人在等你」的推播都是**等的那一方自己送的**：child 的 `/notify`、root 的
`/v1/orchestrator/notify`、blocking decision 開立時的那一則。會忘記的就是「送」這個動作——
2026-09-21 三個短命的配對連結只有一個被推出去。所以這裡加了一條**機器自己說**的：

> **一個 session 停在問你的問題上，十分鐘沒有人回答，推一則；同一次停頓只推這一則。**

**機器怎麼知道它在等。** 讀數本來就知道：Claude 自己的狀態檔寫 `waiting`，或畫面上畫著選單
（`AskUserQuestion`、權限詢問）。列表上畫成 🙋、`work_state: waiting_you` 的就是它。
`internal/app/waiting.go` 跟 board sweep 同一個時鐘（15 秒），取的是手上最近一次、15 秒內的
讀數（`InventoryReading.Within`）——broker 的 beat 每 5 秒本來就會讀一次，所以它不自己另外掃。
這是舊版唯一一條由狀態變化觸發的推播（`StateHook.swift:341-360`），差在下面兩點。

| | 值 | 為什麼 |
|---|---|---|
| 門檻 `maxUnseenWait` | 10 分鐘 | 舊版一進 `waiting` 就推。實測一半的問題兩分鐘內就有人答，那一則推到的是正在看的人。見下面的分布：十分鐘是回答跑完的地方，再往後幾乎都是很久。 |
| 一次停頓一則 | 寫進 store | 停頓從第一個看到 `waiting` 的讀數，到第一個看到它在做別的事的讀數。決定（`pushed`、`over_budget`、`silent`）寫成 `session.waiting` event；`pushed` 跟推播的 outbox effect 在同一個 transaction（D08）。重啟後讀回來，不重新決定。outbox 的 recovery 對開始了沒結束的推播記 `unknown`、不重送。 |
| 預算 `waitingPushHourLimit` | 6／小時 | 跟 agent 的 30、decision 的 30 分開算。一週裡單日最多 3 則；這個數是給「一整波 child 卡在同一個權限詢問」那種下午用的，那是一件事，不該是十則。超過的記 `over_budget`，之後也不補推。 |
| 派出去的 child | 帶「時限還剩 N 分鐘」 | 那個分頁沒有人在看，它的 timeout 在倒數（`StateHook.swift:278-287`）。task 已經結束的分頁不推（記 `silent`）。 |
| 開關 | `orchestrator_agent_notify` | 目前唯一一個管「session 在做什麼」的推播開關；關掉時停頓不決定，重新打開時還在等的會推。 |
| 點開 | 那個 session | `terminal` 帶在推播上；tag 是 `waiting-<terminal>`，同一個 session 的新一則蓋掉舊的。 |

**只有讀數能結束一次停頓。** `unknown` 不是「沒在等」；讀數看不到那個終端機（例如 iTerm2 沒回應）
時，列表上少了那一列也不是它不在了。沒有人看到結束的停頓（daemon 當時沒在跑）就當作已經推過：
推兩次比讓他在列表上看到更糟（DG-7）。

### 不推的，以及為什麼

- **提案（proposal）。** 它有安全預設（留在 to-do），不擋任何人。最近一天 38 筆，**0 筆被人回答**，
  28 筆後來被機器自己撤回（問題自己消失了）。若規則是「pending 超過 30 分鐘就推」會是 37 則，
  一小時是 33 則，四小時也還有 14 則——幾乎全是會自己解決的事。它們留在「待確認」與每日摘要。
- **非 blocking 的 decision。** 它自己說不擋工作。blocking 的在開立時已經推過一次（`PushDecision`），
  不推第二次。附帶一個事實：`decisions` 表**從來沒有過一筆**——agent 要人拍板時沒有走這條路。
- **用文字問完就結束 turn 的 session。** 畫面上它是 `idle`，跟做完了分不出來，機器看不到。
  要被看見，就用 `AskUserQuestion` 問（那是 `waiting`，這條規則接得到），或開 decision。

### 期限

機器只知道**自己**開的東西的期限：它自己的 Cloud 配對邀請（`internal/transport/cloud/pairing.go`，
本機上限 10 分鐘，control plane 實際給 3 分鐘——2026-09-19 本機 log 的五個邀請每一個都剛好 3:00 過期）。
2026-09-21 過期的三個連結是**另一台機器**開的，這台 daemon 看不到。

而且三分鐘本身就不是推播救得回來的：下面的分布裡 16／45（36%）的問題超過三分鐘才有人答，那還是
多半有人在旁邊的情況；十分鐘以上沒人答的 11 次全部都會錯過。所以這一類要改的是**順序**：先把人叫到
（一則不帶碼的 notify，或 `AskUserQuestion`——後者現在會被推），他回了再產生碼。

由送的一方帶上期限（例如 `/notify` 帶 `expires_at`，讓機器拒絕已經過期的、並在窗口比人回應時間短時
回一個警告）是對的方向，但那是 `api/v1` 的 contract 變更，這一版沒有做。

### 2026-09-21 的量測

| 來源 | 窗 | 數字 |
|---|---|---|
| Claude transcript 的 `AskUserQuestion`（tool_use → tool_result 的間隔） | 7 天 | 45 次；≥1 分 33、≥2 分 20、≥3 分 16、≥5 分 14、**≥10 分 11**、≥15 分 9、≥30 分 5、≥60 分 2；中位數 107 秒、p75 462 秒 |
| 同上，≥10 分鐘的逐日 | 7 天 | 0、3、3、0、1、0、1、3 → **這條規則一週 11 則，一天最多 3 則** |
| `proposals` | 最早一筆起約 26 小時 | 38 筆；0 answered、28 withdrawn、10 pending |
| `decisions` | 全部 | 0 筆 |
| `daemon.log` 的 `push:` 行 | 09-18 22:44 起約 2.5 天 | 9 則：容量 1、dead letter 3、child `/notify` 4、root `/notify` 1 |

**沒量到的：** 權限詢問與 Codex 的核准等待——transcript 裡沒有「停在詢問上」的紀錄，所以 11 是下限。
量測腳本只讀時間戳、tool 名稱與 id，不讀內容。

## 經 Cloud 送出的推播（2026-09-26）

**要解的問題。** 手機的瀏覽器一個 service worker 只有一份推播訂閱，而那份訂閱綁在一把 VAPID
金鑰上。原本那把是某一台機器自己的，所以同一個 Cloud 帳號上的 Linux 機器永遠通知不到這支手機
（量過：那台 daemon 的 `push/` 裡只有它自己的金鑰，沒有任何訂閱），帳號上只要出現第二台會推播的
機器，console 的 `_pushMachine` 就回 `cloud_machine_ambiguous`。決定是：**由 Clawdline Cloud 送
Web Push**，手機只訂閱一次，帳號上每台機器都叫得到它，不管其他機器有沒有在線。

**Cloud 不能越過的線。** Cloud 只持有帳號的 VAPID 金鑰對與每份訂閱的 **endpoint**，從來拿不到
`p256dh`、`auth` 或任何明文。每則訊息還是由機器自己封（RFC 8291 aes128gcm，就是原本的 `Build` ＋
`Body`），Cloud 只轉送密文；它沒有 `auth`，所以造不出手機解得開的通知。`p256dh`／`auth`／origin
由瀏覽器經既有的端對端通道（`push-subscribe` 這個 relay word）交給每一台機器，不經過 Cloud。

### daemon 這一半

- `push-subscribe` 這個字可以在 `subscription` 旁邊多帶一個 `cloud_subscription_id`（UUID）。
  `internal/app/cloudops` 把它併進交給 `/v1/push/subscribe` 的同一份文件；`push.FromBrowser` 檢查
  形狀，存成 `Subscription.CloudID`，寫進 `subscriptions.json` 的 `cloud_subscription_id` 欄位。
  物件裡面也帶一份的，視為同一個問題兩個答案，拒絕。
- 有 `CloudID` 的那一列：`Build` ＋ `Body` 跟原本一模一樣（Apple 的 declarative envelope 仍然看
  endpoint 的 host 與存下來的 origin 決定），然後不 POST 到 endpoint，而是用機器憑證
  `POST <api_origin>/v1/push/send`（`internal/adapters/cloud/push_send.go`，跟
  `schedule_webhooks.go` 同一條呼叫 Cloud API 的路）。憑證與 API origin 從 Cloud link 拿
  （`Link.PushClient`），http 層在送的那一刻透過 `pushCourier` 取得。
- Cloud 的回答在 `send.go` 的 `forward` 變成判決，規則跟直送的一樣：2xx 算送達；**410
  `subscription_gone` 在本機刪掉那一列**；429 照 `retry_after` 重試（超過 60 秒的上限就放棄，不排隊）；
  502 裡的 `push_status` 是 0 或可重試的就重試，其餘不重試；其他 5xx 重試；其餘（包括
  `404 unknown_subscription`、401、403）**原封不動留著**——只有 Cloud 的 410 算「已經不在」的證據。
- 沒有 `CloudID` 的列照舊直送，用這台機器自己的 VAPID 金鑰（免費版／本機 console 的路徑，沒變）。
  整個 fan-out 裡只要沒有直送的列，就**不會產生**這台機器自己的金鑰。
- 沒登入 Cloud、又握著 Cloud 訂閱的機器：那一列算送不出去（`Failed`），**不會**改用自己的金鑰
  （endpoint 綁的是 Cloud 的金鑰，push service 會拒）。`/v1/diagnostics` 的 `push.subscriptions`
  那一列會寫出「N 份經 Clawdline Cloud 的訂閱送不出去：這台機器沒登入 Cloud」。

### console 這一半

`web/console/src/cloud/cloud-push.ts`，由 `CloudGate.tsx` 在選定機器時裝上；`push/push.ts` 看得到它
就走 Cloud 路徑，看不到（本機 console）就跟以前一模一樣。`legacy/js/net/cloud-client.js` 是
`MANIFEST.json` 釘住的拷貝，沒有動。

- **開**：`GET {api}/v1/push/key`（`credentials: "include"`）→ 瀏覽器目前的訂閱如果是用別把金鑰
  做的，先 `unsubscribe()` → 用 Cloud 的金鑰 `pushManager.subscribe` →
  `POST {api}/v1/push/subscriptions {endpoint}`（帶 `Idempotency-Key`）拿到 id → 把
  `{subscription, cloud_subscription_id}` **並行**送給每一台已配對、有 `push-subscribe` 的機器
  （fan-out，不再嚴格挑一台，所以兩台不再是 `cloud_machine_ambiguous`）。畫面照機器名稱說哪幾台
  收下了、哪幾台沒收到和原因；本來就沒有推播的機器（`unknown_command` 等）不列。
- **之後每次開頁**：這個瀏覽器在 localStorage 的 `clawdline.push.cloud` 記著 Cloud 的 id、金鑰、
  每台機器回的本機 row id。名單上缺的、有推播的機器（之後才加入的、上次離線的）這時補送。用
  機器 id 比對，這是改動最小的讀法：不需要新的 relay word，重送對 daemon 是冪等的（同一個
  endpoint 或同一台裝置只留一列）。
- **關**：瀏覽器 unsubscribe → 對記錄裡每台機器送 `push-unsubscribe`（各自的 row id）→
  `DELETE {api}/v1/push/subscriptions/:id`。沒通知到的照名稱說出來。

### 驗證（2026-09-26，在 task worktree 裡跑）

- `internal/adapters/push/cloud_send_test.go`：對著一個照合約實作的假 Cloud API——Cloud 訂閱經 API
  送出、endpoint 一次都沒被直接 POST、瀏覽器的私鑰解得開 Cloud 收到的密文、Cloud 收到的 body 沒有
  `p256dh`／`auth`／`endpoint`；410 在本機刪掉；429／502／404 各自重試或留著；本機訂閱仍然直送並
  帶 `vapid t=`；`*.push.apple.com` 的 Cloud 訂閱仍然是 `application/notification+json`；沒有 Cloud
  時送不出去也不退回自己的金鑰、不產生金鑰。暫時拿掉 `forward` 的分流，其中五個測試變紅，還原後
  回綠。
- `internal/transport/http` 的 `TestACloudSubscriptionIsSentThroughCloudEndToEnd`：Cloud viewer
  經 relay word 帶 `cloud_subscription_id` 訂閱，沒登記 Cloud link 時 `PushSend` 算失敗、endpoint
  沒被打到；登記之後經假 API 送達。
- `internal/app/cloudops` 的 `TestACloudSubscriptionCarriesItsCloudIDToTheRoute`。
- `web/console/src/cloud/cloud-push.test.ts`：兩台機器一台離線（點名）、金鑰不同時重新訂閱、
  下次開頁只補送缺的那台、關閉時點名沒通知到的、沒裝 seam 時不碰 Cloud。

**沒量到的**：真的 Cloud API（另一個 repository 平行在做）、真的手機收到經 Cloud 送出的通知。
Cloud console 上的「測試通知」按鈕沒改，帳號上有兩台以上會推播的機器時仍然會回
`cloud_machine_ambiguous`。

## 還沒接的線

- **session 開始等待時的通知接上了（上一節），但只有這一條。** 舊版另外在 `DeployWatch.swift`、
  `Orchestrator.swift`、`SmartNotification.swift` 呼叫 `WebPush.send`，那三條在 Go 版還沒有對應的
  觀察點。現在會送出通知的：設定頁的測試按鈕、容量告警、dead letter、child 與 root 的 `/notify`、
  blocking decision、排程失敗，以及上一節這一條。
- **機器自己的推播沒有自己的開關。** 等待推播沿用 `orchestrator_agent_notify`；要分開需要一個新的
  設定鍵（`nextconfig`、contract、設定頁三處）。
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
