# Notifications

This page lists the notifications the current Go daemon can send. A browser can
also send a test notification to its own device.

## How it works

Notifications are standard Web Push. The daemon on your machine sends them itself, over outbound
HTTPS to your browser's push service; no Clawdline account is needed. Each browser or phone that
should be told subscribes once.

The switches under **When to notify me** belong to the selected machine. You
can change them from the hosted console even when the browser you are using
cannot receive notifications. That browser's permission and subscription are
controlled separately under **This device**. A paired device needs Send
permission to change the machine's switches.

## Before you start

- A browser can subscribe only on a secure page: `http://127.0.0.1:7727` on the machine itself,
  your own tunnel's HTTPS address, or the hosted console at app.clawdline.com
  ([remote-access.md](remote-access.md)). An address on your local network (`http://192.168…`)
  cannot.
- **iPhone and iPad:** iOS only notifies an app on the Home Screen. In Safari, use **Share → Add to
  Home Screen**, then open Clawdline from the Home Screen and subscribe there.

Labels below follow the English interface; the console also supports other languages.

## Turn them on

1. In the sidebar, open **Notifications**.
2. Press **Turn on notifications on this device** and allow
   notifications when the browser asks.
3. Press **Send a test**.

**Check:** the page says **Sent — it should arrive on this device in a moment.** The test
notification arrives on this device.

## What you are told about

| When | Condition and destination |
| --- | --- |
| A Session waits on a question or permission for **10 minutes** | `push_on_waiting`: once per stop, at most 6 per hour per machine; it opens that Session. When unset, this key inherits the older `orchestrator_agent_notify` choice. |
| A Session stops because its selected model is at capacity | `push_on_model_capacity`: enabled by default, independently of question and system-capacity notifications. Sends on the first current reading, once per stop, and opens that Session. Shares the waiting-notification budget of 6 per hour per machine. |
| An agent calls `clawdline notify --title … --body …` or a task's notification route | Subject to `orchestrator_agent_notify` and the agent notification limit; `--session` opens the named Session. |
| An agent creates an unsolicited human intervention Note | Subject to `orchestrator_agent_notify`; it opens the owning Session. |
| A blocking Board decision needs an answer | `push_on_board_decision`: once per pending decision, at most 30 per hour per machine; it opens the list. |
| A Board item enters `done` | `push_on_board_done`: one outbox notification for that completion; it opens the assigned Session while that Session is still present. |
| A schedule misses its run, dispatch is refused, or its file cannot be read | `push_on_schedule_failure`: missed and refused runs also require **Notify on failure** on the schedule. An invalid file is reported when its fingerprint changes. These open the list. |
| A capacity row crosses a notification threshold | `push_on_capacity`: the capacity monitor sends the recorded notice; it opens the list. |
| A task completion notice becomes a dead letter | `push_on_dead_letter`: once for that dead letter; it opens the root Session while present. |
| A Session records its delivery report | `push_on_delivery`: one push after the reporting Session finishes its final answer; it opens that Session while present. |
| All tasks in one dispatched batch finish | `push_on_fanout`: one push when the last task finishes; a batch has at least two tasks dispatched by the same Root while it has active children. It opens the Root Session while present. |
| Completion wording includes details | `smart_notifications` changes the two preceding notifications: the Session's own delivery summary or task titles appear in the body. It does not send an extra notification or call an external model. |
| A Cloud or running macOS app release passes its production checks | `push_on_cloud_release_success` and `push_on_app_release_success` are separate. Unset keys inherit the older `push_on_deploy` choice; a fresh installation has both off. Each opens the list. |
| An App install or Cloud production check fails | `push_on_release_failure` sends one notification for that failed release command and build. An unset key inherits `push_on_deploy`; a fresh installation has it off. It opens the list. |
| You press **Send a test** | Only the asking device receives it. It opens the selected Session when the machine can still verify it; otherwise it opens the list. |

Tapping a waiting notification opens that session. A newer notification about the same session
replaces the older one.

The `orchestrator_agent_notify` switch is on by default. When it is off,
`clawdline notify` is refused with `agent_notify_disabled`.

The former custom waiting rules are no longer evaluated. Their saved data is
retained for compatibility but has no effect on notifications.

## Turn them off

Press **Turn off notifications on this device** under **Notifications** on the same device. To stop a browser from asking at all,
change the site's notification permission in the browser's own settings.

## Troubleshooting

| You see | Do this |
| --- | --- |
| **This browser cannot show notifications.** | The page is not secure, or this is iOS outside the Home Screen. See "Before you start" |
| **Notifications are blocked for this site.** | Allow notifications for the site in the browser's settings |
| The test says the machine has no subscription for this device | Turn notifications off and on again on that device |
| Nothing arrives, but the test works | The session was answered within ten minutes, or the hourly limit was reached |

## Deeper

- [push.md](../push.md) — the Web Push implementation, what never leaves the machine, retries and
  the ten-minute measurement (in Chinese).

## Delivery reports wait for the final answer

`clawdline session report` records the receipt immediately. The agent calls it
before writing its final answer, so the receipt itself does not send a push.
The broker keeps the notification in its durable outbox until a fresh reading
taken after the report shows the same conversation idle. A working, waiting,
unknown, retained or pre-report reading cannot release it. There is no fixed
sleep that could expire while a long answer is still being printed; delivery
follows the next broker pass that observes idle.

Restart recovery uses the same condition for an unattempted notification. An
attempt interrupted before its outcome was recorded is marked unknown rather
than sent again. A replaced conversation or a Session proven gone closes the
pending notification without sending it. An unreadable or unbound Session
keeps it pending. Older intents without a conversation binding are closed
without sending. Fan-out and Board completion notifications retain their own
triggers.

The regression first failed with `pushed before the final answer was printed`
on the immediate-send implementation. It also holds a ten-minute working
answer, stale idle observations, unknown states, incomplete absence and a
restarted broker before allowing one fresh idle observation to release the
notification. Recovery of an attempted push and concurrent recovery cannot
send it twice.


## Admin new-question notifications

The Cloud help center has a separate per-device notification for Admins when a member submits a
new question. It does not use a machine Session notification rule. See [Questions and replies](help-center.md#admin-inbox-and-new-question-notifications) for setup and the authenticated question destination.
