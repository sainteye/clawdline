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
pressed, and the text typed (cut to 2,000 characters, with its full length kept). Text typed into
a password field is recorded only as its length. Takeover and hand-back are rows too. The audit
is `<CLAWDLINE_NEXT_DIR>/browser-audit.jsonl` (mode 0600): 2,000 rows are kept, the file is
rewritten to them when it reaches twice that, and the route reports how many were pruned and
whether the last write to disk failed.

When `browser_notify_new_site` is on, the first successful action of a Session on a site
(registrable domain) sends one notification through the agent-notification path.

## The live panel

The Session's ⋯ menu has **Chrome tab** (`web/console/src/session/BrowserPanel.tsx`). It reads
`/v1/browser/status` to find the Session's tab and asks `POST /v1/browser/person` for a screenshot
about once a second while it is open and the page visible, because Chrome stops streaming a tab
that is covered, minimized or behind another tab. **Take over** (`POST /v1/browser/control`) stops
the agent's commands to that tab; only then does a tap on the picture become a click at the same
point of the page, and the text box, Tab, Enter, Delete and scroll reach it. What the person types
is not recorded. **Hand back** returns the tab to the agent.

Through Clawdline Cloud the panel uses the same routes as four words of
`internal/domain/cloudvocab/vocabulary.json`, each end-to-end encrypted like every other word and
run on the machine as the viewer's device (`local.device`), so a phone needs read access to look
and send access to take over and act:

| Word | Kind | Local route |
|---|---|---|
| `chrome-tab-status` | read | `GET /v1/browser/status` |
| `chrome-tab-screenshot` | read | `GET /v1/browser/screenshot?tab=N&quality=Q` (asked at quality 45 through the relay) |
| `chrome-tab-input` | command | `POST /v1/browser/person` (click, type, key, scroll) |
| `chrome-tab-control` | command | `POST /v1/browser/control` |

## Clawdline's own Chromium

`clawdline browser launch [--browser PATH]` needs nothing installed into the person's Chrome. It
writes the extension carried in the binary (`extension/embed.go`) to
`<CLAWDLINE_NEXT_DIR>/chrome-extension`, registers the native messaging host inside
`<CLAWDLINE_NEXT_DIR>/chrome-profile`, and starts Chromium (`CLAWDLINE_CHROMIUM`, else the newest
Playwright Chromium) on that profile with the extension loaded. Sign-ins made there stay in that
profile and are separate from the person's Chrome. It passes `--use-mock-keychain`, so its cookies
are not protected by the macOS keychain; it is meant for sites the person chooses to sign in to
there.

## Testing

`TestBrowserBridgeEndToEnd` (internal/transport/http) drives a real Chromium through the whole
path when `CLAWDLINE_BROWSER_E2E_BIN` and `CLAWDLINE_BROWSER_E2E_CHROMIUM` are set. Branded Chrome
ignores `--load-extension`, so it needs Chromium. It uses a temporary profile and a test server,
never the person's daemon or Chrome. `web/console/src/session/browser-panel.e2e.ts` exercises the
panel against a stand-in daemon at desktop and phone widths, in English and Chinese.

## Not built yet

The install guide, the Settings page with the audit list and the stop switch, usage guidance in
new Sessions, typing a Secret Handoff into a password field, the per-Session notification mute,
and streaming instead of screenshots when the tab is drawn.
