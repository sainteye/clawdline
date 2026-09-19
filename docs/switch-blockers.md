# 切換阻擋：派出去的 child 沒被 brief

新 daemon 在 iTerm2 開的 child 一直沒被 brief：分頁開了、`claude` 起來了、CHILD.md 也寫了，
composer 卻是空的，task 停在 `spawning`，最後以 `timeout` 結案。這裡是兩個缺陷和修法。

## 缺陷一：iTerm2 的腳本碰到一種視窗就整支丟例外

- 量到的：某個可見、有標題的 iTerm2 視窗，JXA 對它的 `tabs()`、`currentTab()`、`currentSession()`
  都回 `null`。清單、打字、按鍵、擷取畫面、reveal 五支腳本都直接讀 `tabs.length`，只要有這種視窗就
  `TypeError`。
- 後果：iterm 來源每次都讀失敗，inventory 只剩 ps 那一列，而 ps 用 tty（`ttys031`）當 id；
  `NewITermTab` 回的是 session GUID，所以 `Broker.brief` 用 GUID 在 inventory 裡永遠找不到它，
  90 秒後放棄，一個字都沒打。
- **不是把 spawn 改回傳 tty**：inventory 合併時本來就以 GUID 為準（`app.richer`），所有 iTerm2 動作
  （`Send`、`Type`、按鍵、擷取、reveal）也都用 GUID 找 session。改成 tty 只會讓 brief 找得到、打不進去。
- 修法：五支腳本共用一個 `itermEach` 走訪，讀不到 tabs／sessions 的視窗或分頁跳過並計數；清單照樣
  發布讀得到的列（id 是 GUID），但只要有跳過就標成 incomplete，不拿它證明任何東西不存在。
- 同一條路上的第二個洞：`ITerm.Send` 呼叫字典裡沒有的 `writeText`、沒送 Enter、也不讀腳本回的
  `sent:false`。改成照舊 app 的 `send`：bracketed paste、單獨一個 CR、再看 composer 補 CR；
  腳本回「沒做到」就是失敗（`itermCall`／`itermAnswer`）。

## 缺陷二：「從沒被 brief」這件已知的事實沒有結案

- 量到的：brief 放棄後，原因存在紀錄的 `spawn_error`（`GET /v1/orchestrator/tasks/<id>`；注意是
  snake_case，同一列的 `spawnedAt` 卻是 camelCase，console 的 task 列則根本沒有這個欄位）。
  4 分鐘的鐘只在「分頁的來源完整回答它不在」或「分頁卡在對話框」時判決；這台 Mac 的 iterm 來源永遠
  不完整，所以什麼都不判，一直等到 task 自己的 `timeout_minutes`，verdict 只寫「passed its timeout」。
- 修法：`brief` 分清楚「從沒打過」（`unbriefed`）和「打了但回錯誤」（按鍵可能已經送到）。前者時
  secret 已經不會再留在任何地方，沒有任何讀數能改變答案，所以派工當下就結成 `spawn_failed`，
  verdict 帶著 brief 自己的原因；tmux 的 pane 用 tmux 給的 pane id 關掉。紀錄多一個
  `unbriefed`，萬一這筆結案沒寫進去，beat 的 4 分鐘鐘看到它也會結案，不必等讀數。
- 後者維持原樣：留在 `spawning`，靠簽收或 timeout 決定。

測試：`internal/app/orchestrator/unbriefed_test.go`、`internal/adapters/terminal/iterm_scripts_darwin_test.go`
（在 node 的 `vm` 裡用假的 iTerm2 物件模型跑真正的腳本字串，沒有 node 就 skip）。

## Review of e54e338: what the correction changed

An independent review returned `changes_required` (three important, five minor). The correction:

- **F1, F2 — every failed typing is one of two things.** `brief` asks `nothingTyped`: a lane that
  never came free (`lane.Busy`), a refusal before the first byte (`terminal.Unsent`: session not
  found or not seen, terminal not running, nothing to type) or a write asked from inside a
  transaction. Those try again, and a wait that ends on them is `unbriefed` → `spawn_failed` with
  the refusal as its reason. Anything else may have landed and is **never typed again**; the
  record stays `spawning` for the beat. `Actions.Send` now carries those typed causes (it used to
  drop the terminal's error inside `send_failed`), and an iTerm2 script's `ok:false` is `Unsent`,
  because every effect script answers it before it writes. The old test that used "the lane timed
  out" as a typing that may have landed had it backwards: a lane is waited for before the first
  byte.
- **F3 — a screen that could not be read is not ready.** Only a daemon with no screen reader at
  all types without looking; a capture that failed this once is asked again next round.
- **F4** — the send script's second look recognises Claude Code's `❯` caret.
- **F5** — an unbriefed iTerm2 child is closed by the session id iTerm2 gave back
  (`CloseITermSession`, iterm.js's `close`: the session, never its tab). `ITerm.Close` is
  implemented with the same script and a 10-second limit; a timeout or `-1712` is a typed
  `Failure` with `Attention`.
- **F6** — the tmux close kills the proven pane, not the session: a window somebody added
  survives.
- **F7** — a script that did not find its session past an unreadable window says "not seen",
  not "gone".
- **F8** — osascript's stderr goes to this machine's log; refusals and `spawn_error` carry a
  sentence.

Measured on an isolated daemon with a private tmux server: a child in an untrusted repository
(trust dialog on screen) settled `spawn_failed` at 91 s with "the child is showing a dialog" in its
verdict, and its pane was closed; a minimal task in a trusted checkout was briefed at 20 s and
settled `success` at 26 s.

Not done here: a finished iTerm2 child's tab is still not lingered-and-closed (its source never
answers completely on a Mac with an unlistable window, and a linger decides only on one that
does), and a schedule's `close_tab` is parsed and stored but nothing acts on it yet.
