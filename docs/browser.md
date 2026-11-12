# Browser use through the Clawdline Chrome extension

Agents (Claude Code, Codex, Gemini, Grok) use the person's Chrome through one path that Clawdline
owns, so every assistant gets the same commands, the person can watch and take over, and every
action is recorded. The design canvas and the person's decisions are recorded on the board item
"Chrome extension integration"; the measurements behind them are in
`docs/experiments/chrome-extension-probe-2026-11-12.md`.

## The path

```
clawdline browser <op>          an agent, machine token, from a live Session
        │  POST /v1/browser/commands
        ▼
daemon  internal/app/browserbridge  (tab ownership, takeover, stop, audit)
        ▲  POST /v1/browser/host/poll   (long poll, 25 s)
        │  POST /v1/browser/host/results
clawdline browser-host          started by Chrome through native messaging
        │  stdin/stdout, 4-byte length + JSON
        ▼
extension/chrome                chrome.debugger (CDP 1.3) in a separate "Clawdline" window
```

**Why native messaging and a long poll.** The daemon never accepts a WebSocket
(`internal/adapters/cloud/websocket.go` explains why), and an extension cannot read the machine
token. Chrome starts the host only for the extension id named in the host manifest's
`allowed_origins`, so no other program can pose as the extension, and the extension holds no
secret and opens no network connection. Measured on 2026-11-12: Chromium reads the host manifest
from `<user-data-dir>/NativeMessagingHosts/`, so registering it touches only that profile, and the
host inherits Chrome's environment.

`clawdline browser install-host --extension-id <id> --chrome-dir <user-data-dir>` writes that
manifest and a launcher (`<CLAWDLINE_NEXT_DIR>/browser-host.sh`) that pins this daemon's directory
and port. The console's install guide will run it with the person's consent.

## Commands

`open <url>`, `tabs`, `read`, `click <label>` or `click --x N --y N`, `type <text>`, `key <name>`,
`scroll <down|up>`, `screenshot [--out FILE]`, `release`, `stop`, `audit`, `status`.

- `open` creates the Session's tab in the agent window; later commands go to the Session's
  current tab unless `--tab` names another of its own tabs.
- `read` returns the page text (up to 200 KiB) and up to 300 visible elements with their labels;
  `click` takes one of those labels.
- `type` uses `Input.insertText`, so Chinese and other scripts need no input method.

## What the bridge enforces

| Situation | Refusal |
|---|---|
| `browser_enabled` is off (the default) | `browser_disabled` |
| No host has polled in 60 s | `browser_extension_missing`; the agent continues another way |
| Another Session's tab | `browser_tab_other_session` |
| The person has taken the tab over | `browser_person_controlling`; no command, screenshot or read reaches the tab |
| The person, or an agent, pressed stop | `browser_stopped`; only a person may resume |
| 32 commands already waiting | `browser_queue_full` |
| No answer in 30 s | `browser_timeout`; the command may or may not have run |

Every command, refused or not, is an audit row: time, Session, op, tab, URL, site, the element
pressed, and the text typed. Text typed into a password field is recorded only as its length.
The audit keeps 2,000 rows in memory and reports how many were pruned; it does not yet survive a
daemon restart.

When `browser_notify_new_site` is on, the first successful action of a Session on a site
(registrable domain) sends one notification through the agent-notification path.

## Testing

`TestBrowserBridgeEndToEnd` (internal/transport/http) drives a real Chromium through the whole
path when `CLAWDLINE_BROWSER_E2E_BIN` and `CLAWDLINE_BROWSER_E2E_CHROMIUM` are set. Branded Chrome
ignores `--load-extension`, so it needs Chromium. It uses a temporary profile and a test server,
never the person's daemon or Chrome.

## Not built yet

The console's live panel, takeover and hand-back buttons, the install guide, the Settings page,
the audit list, typing a Secret Handoff into a password field, persistence of the audit, and the
Cloud channel for the phone.
