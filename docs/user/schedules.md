# Scheduled tasks and webhooks

After this page you can save a task that Claude Code or Codex runs on its own — once, on chosen
days at a chosen time, or with no time at all, only when you or a webhook start it — see each
run's result, and, through Clawdline Cloud, start the same task from a webhook and wait for its
result. You can tell it worked from the schedule's history.

## Availability

Labels below follow the English interface; the console also supports other languages.

Everything below is free. Cloud features need sign-in and a paired machine.

| | Local (no account) | Clawdline Cloud |
| --- | --- | --- |
| Schedules on this machine's local clock | Yes | Yes |
| Choose which machine a schedule runs on | — | Yes, once the account has more than one machine |
| Schedule webhooks | No | Yes |

Schedules run on macOS and Linux, where the daemon can open a session in tmux. The daemon has to be
running when a schedule is due; see [platforms.md](platforms.md) for keeping it running.

## Create a schedule

1. Open the session list. Schedules are the **Schedules** fold at the bottom of it.
2. Press **+** (**New schedule**) in its heading.
3. Fill in the form:
   - **Title**
   - **No time — runs only by webhook or by hand** — tick it
     for a task the clock never starts; the two fields below disappear.
   - **At** — the time of day.
   - **On** — **Daily**, the days of the week, or **Every N days**. For an interval, enter
     **Every (days)** and **Starting on**. For example, every 3 days starting October 21 at
     09:00 runs on October 21, 24, 27, 30, then November 2 in the schedule’s time zone.
     **Run now** does not change those dates.
   - **Machine** — shown only when your Cloud account has more than one machine.
   - **Where** — the project the session opens in.
   - **With** — Claude Code or Codex.
   - **First message** — the instructions the session starts with.
4. Open **More** for the rest, all optional:
   - **Close the tab when it finishes** — **If it worked** (the default),
     **Always**, or **Leave it open**.
   - **Enabled** and **Tell me if it fails** — both on by default.
   - **If it was missed, catch up within (hours)** — default 6, from 0 to 168.
   - **Give up if it is not done in (minutes)** — default 30, from 1 to 240.
   - **Model**
   - **Permission** — **This machine's default**, **Ask me at every step**, **May edit files directly**, or **Full permission (never asks)**. Only a person can set this field; an agent that tries is refused.
5. Press **Create**. Nothing is scheduled until you do.

**Check:** the schedule is a row in the list. Click it to open its history, and press **Run now** to try it at once. A new session opens and starts working on the first message.
If this schedule already has work running, Clawdline first protects it from an accidental duplicate.
You can confirm **force run** to start another one while the earlier task keeps running. Project-claim,
session-capacity, disabled-dispatch, and one-time-schedule checks still apply to the new run.

## What happens when it is due

- The daemon checks once a minute and starts the latest occurrence that is due.
- If the daemon was not running at that time, the occurrence runs **once** when it comes back, as
  long as that is within the catch-up window. Missed occurrences are never replayed one by one.
  Outside the window the occurrence is recorded as missed, and you get a notification if **Tell me
  if it fails** is on ([notifications.md](notifications.md)).
- Each run is recorded in the schedule's history, with the session it ran in.
- A schedule with no time is never due. Its row reads **Runs only by webhook or by hand** instead of a next time; it runs when you press **Run now** or its
  webhook is called, and only while it is **Enabled**.

## Change, pause or delete

Click the row, then **Edit schedule**.

- To pause, turn **Enabled** off and press **Save**. The row reads **Disabled**.
- To delete, press **Delete**. There is no undo.
- With more than one machine on the account, changing **Machine** moves the schedule to that one.

## Let a session create a schedule

A session using the Clawdline skill ([clawdfather-and-dispatch.md](clawdfather-and-dispatch.md))
can create a one-time schedule on its own. A repeating schedule needs your explicit instruction,
sent to that session through Clawdline: a message typed straight into the terminal is not proof
that you asked, and the repeating schedule is refused.

## Schedule webhooks (Cloud)

To have GitHub or another service trigger a **Bot routine**, use [Bot Webhooks](bot-webhooks.md) · [繁體中文](bot-webhooks.zh-TW.md). The steps below start a scheduled task from the Session list.

A webhook starts a saved schedule's task from outside — a CI job, a form, another service — so an
event can open a full agent session on your machine instead of running a fixed shell command.

1. In the hosted console at app.clawdline.com, open the machine's session list and click the
   schedule's row.
2. Under **Cloud webhook**, press **Generate webhook**, then **Copy URL**. The URL is shown once;
   Clawdline does not show it again.
3. Call it with an HTTP `POST`:

   ```sh
   curl --request POST "$CLAWDLINE_SCHEDULE_WEBHOOK_URL" \
     --header 'Content-Type: application/json' \
     --header 'Idempotency-Key: request-001' \
     --data '{}'
   ```

- The body is not passed to the agent. A webhook cannot replace the schedule's saved first
  message; it only says "run it now". The body may be empty, `{}`, or
  `{"deliver_within_seconds": N}` (10 to 86400): if your machine has not taken the run within N
  seconds, it is void and never starts later. Without it the limit is 24 hours.
- Send an `Idempotency-Key`: the same key never starts a second run. Without one, every call is a
  new run.
- `202` means Clawdline Cloud durably accepted the request, not that the run finished. Its
  `delivery_id` names the run's delivery: `GET` the same URL followed by
  `/deliveries/<delivery_id>` to read whether it was taken, refused, finished — and how — or
  expired. The result is also in the schedule's history.
- To start it and wait for the answer from a script or another machine, use
  `clawdline webhook fire` (below).
- **Rotate URL** replaces the address; **Disable webhook** turns it off.
- Moving the schedule to another machine (changing **Machine**) moves its webhook too, and the URL stays
  the same, so whatever calls it needs no change. A disabled webhook is not moved; generate a new one
  on the other machine if you need one.

### Start it and wait: `clawdline webhook fire`

`clawdline webhook fire` calls the webhook and waits for the run to end, from any machine with the
`clawdline` binary — no daemon, no pairing. The URL is a secret, so it is never an argument: put it
in a file readable only by you, or in `CLAWDLINE_WEBHOOK_URL`.

```sh
clawdline webhook fire --url-file ~/.config/deploy-webhook-url
```

It asks for delivery within 60 seconds (`--deliver-within`), waits up to 60 minutes (`--timeout`),
prints each change on stderr and one final line on stdout, and exits `0` when the run succeeded,
`1` when it finished without success, `2` when it never reached your machine (for instance the
machine was off), `3` when it was refused (the schedule is disabled or already running, or the URL
was rotated or disabled), and `4` when it stopped waiting. `--no-wait` prints the delivery id and
returns at once. [schedules.md](../schedules.md) lists every final line.

## Troubleshooting

- **Nothing ran.** The daemon must be running at the time, or come back within the catch-up
  window. Check that the schedule is **Enabled**.
- **The generate button is unavailable.** Webhooks need Clawdline Cloud: sign in and pair the machine.
- The public guide for webhooks is at https://clawdline.com/docs/schedule-webhooks.

## Deeper

- [schedules.md](../schedules.md) — the stored format, the clock, catch-up, and moving schedules
  between machines (partly in Chinese).
- [verifications.md](../verifications.md) — a scheduled task that reads back whether a change held.
