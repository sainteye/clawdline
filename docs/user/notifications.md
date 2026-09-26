# Notifications

After this page your phone or computer gets a notification when a session has been waiting for your
answer, when an agent tells you something, or when a schedule fails — and you can send yourself a
test to prove it arrives.

## How it works

Notifications are standard Web Push. The daemon on your machine sends them itself, over outbound
HTTPS to your browser's push service; no Clawdline account is needed. Each browser or phone that
should be told subscribes once.

## Before you start

- A browser can subscribe only on a secure page: `http://127.0.0.1:7727` on the machine itself,
  your own tunnel's HTTPS address, or the hosted console at app.clawdline.com
  ([remote-access.md](remote-access.md)). An address on your local network (`http://192.168…`)
  cannot.
- **iPhone and iPad:** iOS only notifies an app on the Home Screen. In Safari, use **Share → Add to
  Home Screen**, then open Clawdline from the Home Screen and subscribe there.

The console's interface is in Traditional Chinese, the only language it ships so far. Labels below
are given as they appear, with their meaning in parentheses.

## Turn them on

1. In the console, open **設定** (Settings) and find **通知** (notifications).
2. Press **通知我** (notify me) — "when a session is waiting for an answer" — and allow
   notifications when the browser asks.
3. Press **送一則測試** (send a test).

**Check:** the page says **送出去了——馬上就會到這台裝置。** (sent) and the test notification
arrives on this device.

## What you are told about

| When | Sent by |
| --- | --- |
| A session has been stopped on a question or a permission prompt for **10 minutes** without an answer | The daemon, once per stop, at most 6 an hour. A question answered sooner is never pushed |
| An agent calls `clawdline notify --title … --body …` | The session itself (title up to 80 characters, body up to 500) |
| A schedule's run fails, when **失敗時通知我** is on | The scheduler ([schedules.md](schedules.md)) |

Tapping a waiting notification opens that session. A newer notification about the same session
replaces the older one.

Agent notifications and the waiting-for-you notification follow one switch on the machine,
`orchestrator_agent_notify`, on by default and shown in the macOS app's settings window. When it
is off, `clawdline notify` is refused with
`agent_notify_disabled`.

## Turn them off

Press **停掉** (stop) under **通知** on the same device. To stop a browser from asking at all,
change the site's notification permission in the browser's own settings.

## Troubleshooting

| You see | Do this |
| --- | --- |
| **這個瀏覽器沒辦法顯示通知。** (this browser cannot show notifications) | The page is not secure, or this is iOS outside the Home Screen. See "Before you start" |
| **這個網站的通知被擋掉了。** (notifications are blocked for this site) | Allow notifications for the site in the browser's settings |
| The test says the machine has no subscription for this device | Turn notifications off and on again on that device |
| Nothing arrives, but the test works | The session was answered within ten minutes, or the hourly limit was reached |

## Deeper

- [push.md](../push.md) — the Web Push implementation, what never leaves the machine, retries and
  the ten-minute measurement (in Chinese).
