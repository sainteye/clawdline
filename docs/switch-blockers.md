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

Not done in that correction: a finished iTerm2 child's tab was not lingered-and-closed, and a
schedule's `close_tab` was parsed and stored but acted on nowhere. Both are the next section.

## Finished children's tabs, and a schedule's `close_tab`

Every finished tmux child was closed three minutes after it ended; no iTerm2 child ever was, so the
session list only grew. A linger was decided on a reading whose source for the tab answered
completely, and on a Mac with a window iTerm2 will not list, the iTerm2 source never does. And a
schedule's `close_tab` (`on_success`, `always`, `never`) was parsed, stored and answered, and
nothing did what it said.

**The policy is one table** (`tabPolicy`, `internal/app/orchestrator/linger.go`), and every end has
a row:

| Task | Ends in | Tab | Rule |
| --- | --- | --- | --- |
| unscheduled | success, failure | closed `orchestrator_child_linger` after it ends (default 180 s) | `child_linger` |
| unscheduled | timeout, cancelled | left open: the child may still be working | `unfinished_left_open` |
| unscheduled, `orchestrator_child_linger` < 0 | anything | left open | `child_linger_off` |
| scheduled, `close_tab: on_success` (the default) | success / anything else | closed as soon as it is at rest / left open | `schedule_on_success` |
| scheduled, `close_tab: always` | anything | closed as soon as it is at rest | `schedule_always` |
| scheduled, `close_tab: never` | anything | left open | `schedule_never` |
| any | `spawn_failed` | closed at once when the tab is there (D11) | `spawn_failed` |

It is readable in three places: CHILD.md states the rule for the task, per way of ending, before the
work starts; the settlement's event (`task.success`, `task.failure`, …) carries
`tab: {rule, close, after_seconds}`; and a close still owed is a `broker_lingers` row. A scheduled
run's record keeps its schedule's `close_tab` as `schedule_close_tab`, the Swift app's name, and a
respawn carries it.

**A close the rule asks for is still made only while the tab is the child's.** Both backends are
decided by one step (`lingerStepFor`):

- The tab is found by the id its terminal gave back: the tmux pane, closed only while it is one of
  the panes of the session named for the task (F6); the iTerm2 session id, which iTerm2 never gives
  to another session.
- *Absent* is decided only by a reading that saw terminals and whose source for that tab answered
  completely. An iTerm2 tab not seen past a window that will not list is waited for, not dropped.
- *Present* needs no complete answer — the tab was seen, by its own id. It is left open for good,
  with the reason in a `task.child.linger.left` event, when a different assistant runs in it, a
  different conversation is in it, or a turn began in it after it had been seen at rest (a person
  went on in the tab, a root sent it a message). Waiting is not counted as use — an assistant can put
  a notice on its own screen — and only holds the close back, as working and unknown do.
- A linger nothing decides within a day of its deadline is let go (`task.child.linger.expired`) and
  the tab left as it is.

**iTerm2 asks a person before it closes a tab with a job running in it.** Measured on this Mac: the
default profile's "Prompt Before Closing" is 2 (ask if there are jobs besides `rlogin`, `ssh`,
`slogin`, `telnet`), and the scripting `close` has no way round it. Three closes of a tab running
`sleep 300` each put "Close tab #N? This tab is running sleep." on the screen, held the Apple Event
past its ten-second limit (killed at 14.4 s with the looks after it), and left the tab open until
somebody answered. That is the whole story behind "a close that timed out closed the tab later". A
tab whose shell is at its prompt closed in 0.14–0.15 s and asked nothing; looking for a missing id
took 0.12–0.16 s. iTerm2's own `jobName` variable is not a reading to decide on: it still named
`java_home`, a command the shell's startup had finished, while `sleep` was in front.

So a child's iTerm2 tab is closed by a ladder (`CloseITermChild`,
`internal/adapters/terminal/iterm_close_darwin.go`), as the Swift app's `Targets.end` did:

1. find the session by id, and its tty (a reading, outside the effects' lock);
2. ask the kernel for every process on that tty and its foreground group;
3. if a job is in front — anything but the shell, which is the tty tree's root or `login`'s child —
   end it with `SIGTERM` to its group **only when every process of the group started at least a
   second before the task ended**: a command a person started in the tab afterwards is somebody
   else's, and the tab is left open with that reason;
4. wait up to five seconds for the group to be gone, as the kernel answers it; read the tty again,
   and leave the tab open if anything else has come to the front;
5. close the session by id.

A close that still goes unanswered is looked for again (three looks, two seconds apart) and is
`Unconfirmed` unless a walk that read every window finds it gone: recorded `unknown`, never counted
done, never retried. One iTerm2 close is made per beat pass, and none for a minute after one went
unanswered, so a busy iTerm2 holds the beat once rather than once per tab. The ten-second limit is
kept: a close with nothing running answers in 0.15 s, and one that ran past ten was waiting for a
person, which no longer limit cures.

Measured on an isolated daemon (its own port and state directory, tasks with no root, children in
iTerm2, `orchestrator_child_linger` 15 s):

| Task | Settled | Closed | Effect |
| --- | --- | --- | --- |
| unscheduled, success | 14:38:07 | 14:38:22 → 14:38:24, gone | `child.close` done, closed |
| unscheduled, failure | 14:39:20 | 14:39:37 → 14:39:39, gone | done, closed |
| scheduled `never`, success | 14:40:42 | left open (`schedule_never`) | none |
| scheduled `on_success`, success | 14:40:37 | 14:40:47 → 14:40:49, gone | done, closed |
| scheduled `on_success`, failure | 14:40:41 | left open (`schedule_on_success`) | none |
| never briefed (trust dialog), twice | 14:34:57 | at once, 1 s each | done, closed |

Each close ended a running `claude` — with its MCP servers, which share its process group — before
closing, and none put a question on the screen. Tests: `internal/app/orchestrator/tabclose_test.go`,
`internal/adapters/terminal/iterm_close_darwin_test.go`.
