# Scheduled tasks and webhooks

After this page you can save a task that Claude Code or Codex runs on its own — once, or on chosen
days at a chosen time — see each run's result, and, on Clawdline Cloud Pro, start the same task
from a webhook. You can tell it worked from the schedule's history.

## Availability

The console's interface is in Traditional Chinese, the only language it ships so far. Labels
below are given as they appear, with their meaning in parentheses.

| | Free (no account) | Clawdline Cloud Pro |
| --- | --- | --- |
| Schedules on this machine's local clock | Yes | Yes |
| Choose which machine a schedule runs on | — | Yes, once the account has more than one machine |
| Schedule webhooks | No | Yes |

Schedules run on macOS and Linux, where the daemon can open a session in tmux. The daemon has to be
running when a schedule is due; see [platforms.md](platforms.md) for keeping it running.

## Create a schedule

1. Open the session list. Schedules are the **排程** (Schedules) fold at the bottom of it.
2. Press **+** (**新增排程**, new schedule) in its heading.
3. Fill in the form:
   - **標題** (title)
   - **時間** (at) — the time of day.
   - **星期** (on) — **每天** (daily), or the days of the week.
   - **機器** (machine) — shown only when your Cloud account has more than one machine.
   - **專案** (where) — the project the session opens in.
   - **助理** (with) — Claude Code or Codex.
   - **第一則訊息** (first message) — the instructions the session starts with.
4. Open **更多** (more) for the rest, all optional:
   - **結束之後關掉分頁** (close the tab when it finishes) — **成功時** (if it worked, the default),
     **一律** (always), or **不要關** (leave it open).
   - **啟用** (enabled) and **失敗時通知我** (tell me if it fails) — both on by default.
   - **錯過了，幾小時內還要補跑** (catch-up window, hours) — default 6, from 0 to 168.
   - **跑多久沒完就放棄（分鐘）** (timeout, minutes) — default 30, from 1 to 240.
   - **模型** (model)
   - **權限** (permission) — **這台機器的預設** (this machine's default), **每一步都問我** (ask at
     every step), **可以直接改檔案** (may edit files directly), or **完全權限（不再詢問）** (full
     permission, never asks). Only a person can set this field; an agent that tries is refused.
5. Press **建立** (create). Nothing is scheduled until you do.

**Check:** the schedule is a row in the list. Click it to open its history, and press **立即執行**
(run now) to try it at once. A new session opens and starts working on the first message.

## What happens when it is due

- The daemon checks once a minute and starts the latest occurrence that is due.
- If the daemon was not running at that time, the occurrence runs **once** when it comes back, as
  long as that is within the catch-up window. Missed occurrences are never replayed one by one.
  Outside the window the occurrence is recorded as missed, and you get a notification if **Tell me
  if it fails** is on ([notifications.md](notifications.md)).
- Each run is recorded in the schedule's history, with the session it ran in.

## Change, pause or delete

Click the row, then **編輯排程** (edit schedule).

- To pause, turn **啟用** (enabled) off and press **儲存** (save). The row reads **停用** (disabled).
- To delete, press **刪除** (delete). There is no undo.
- With more than one machine on the account, changing **機器** moves the schedule to that one.

## Let a session create a schedule

A session using the Clawdline skill ([clawdfather-and-dispatch.md](clawdfather-and-dispatch.md))
can create a one-time schedule on its own. A repeating schedule needs your explicit instruction,
sent to that session through Clawdline: a message typed straight into the terminal is not proof
that you asked, and the repeating schedule is refused.

## Schedule webhooks (Cloud Pro)

A webhook starts a saved schedule's task from outside — a CI job, a form, another service — so an
event can open a full agent session on your machine instead of running a fixed shell command.

1. In the hosted console at app.clawdline.com, open the machine's session list and click the
   schedule's row.
2. Under **雲端 Webhook** (Cloud webhook), press **產生 Webhook** (generate), then **複製網址** (copy
   URL). The URL is shown once;
   Clawdline does not show it again.
3. Call it with an HTTP `POST`:

   ```sh
   curl --request POST "$CLAWDLINE_SCHEDULE_WEBHOOK_URL" \
     --header 'Content-Type: application/json' \
     --header 'Idempotency-Key: request-001' \
     --data '{}'
   ```

- The body is not passed to the agent. A webhook cannot replace the schedule's saved first
  message; it only says "run it now".
- Send an `Idempotency-Key`: the same key never starts a second run. Without one, every call is a
  new run.
- `202` means Clawdline Cloud durably accepted the request, not that the run finished. The result
  is in the schedule's history.
- **輪替網址** (rotate URL) replaces the address; **停用 Webhook** (disable) turns it off.

## Troubleshooting

- **Nothing ran.** The daemon must be running at the time, or come back within the catch-up
  window. Check that the schedule is **啟用** (enabled).
- **The generate button is unavailable.** Webhooks need the Pro plan.
- The public guide for webhooks is at https://clawdline.com/docs/schedule-webhooks.

## Deeper

- [schedules.md](../schedules.md) — the stored format, the clock, catch-up, and moving schedules
  between machines (partly in Chinese).
- [verifications.md](../verifications.md) — a scheduled task that reads back whether a change held.
