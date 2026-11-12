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
and port. `clawdline browser launch` and the console's setup guide write it for Clawdline's own
Chromium (below). The launcher runs the stable `<CLAWDLINE_NEXT_DIR>/bin/clawdline` when it
exists, because the daemon's own executable lives in a versioned release directory an update
replaces; otherwise it runs the binary that wrote it.

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
| `chrome-setup` | read | `GET /v1/browser/setup` |
| `chrome-launch` | command | `POST /v1/browser/launch` |

## The Settings block

Settings, Sessions and access, has a **Browser** block
(`web/console/src/pages/settings/BrowserSettingsBlock.tsx`): whether the extension is connected
and which browser it is in, the two switches `browser_enabled` and `browser_notify_new_site`
(both off on a new machine), the emergency stop with its banner and Resume, and the audit list,
narrowed by Session, site and day. `GET/POST /v1/browser/settings` reads and writes only those two
switches; an agent's machine token is refused, and writing needs a device that may send.
`/v1/browser/audit` takes `conversation`, `site` (a registrable domain) and `since` (Unix seconds)
besides `limit`. Through Clawdline Cloud the block uses four more words:

| Word | Kind | Local route |
|---|---|---|
| `chrome-settings` | read | `GET /v1/browser/settings` |
| `chrome-settings-update` | command | `POST /v1/browser/settings` |
| `chrome-stop` | command | `POST /v1/browser/stop` |
| `chrome-audit` | read | `GET /v1/browser/audit` (always with `limit`) |

`web/console/src/pages/settings/browser-settings.e2e.ts` exercises the block against a stand-in
daemon at desktop and phone widths, in English and Chinese.

## Clawdline's own Chromium

`clawdline browser launch [--browser PATH]` needs nothing installed into the person's Chrome. It
writes the extension carried in the binary (`extension/embed.go`) to
`<CLAWDLINE_NEXT_DIR>/chrome-extension`, registers the native messaging host inside
`<CLAWDLINE_NEXT_DIR>/chrome-profile`, and starts Chromium (`CLAWDLINE_CHROMIUM`, else the newest
Playwright Chromium) on that profile with the extension loaded. Sign-ins made there stay in that
profile and are separate from the person's Chrome. It passes `--use-mock-keychain`, so its cookies
are not protected by the macOS keychain; it is meant for sites the person chooses to sign in to
there. The logic is `internal/adapters/browserlaunch`, which the daemon calls too.

## The setup guide

`web/console/src/session/BrowserSetup.tsx` opens from the Chrome panel's **Set up the browser**
button, which the panel shows when the extension is not connected or `browser_enabled` is off.
Any other screen opens it with `openBrowserSetup()` or the `clawdline-open-browser-setup` window
event. Its four steps: what browser use is for; **Open Clawdline's Chrome**
(`POST /v1/browser/launch`, a person who may send), after which it reads `GET /v1/browser/setup`
every 2 s and moves on by itself once the extension connects; signing in inside that Chrome; two
switches, both on, for `browser_enabled` and `browser_notify_new_site`, written only when the
person presses Finish. A completion summary follows the four steps.

`GET /v1/browser/setup` answers whether a Chromium was found and where, the profile directory,
whether a Chromium holds it (`SingletonLock`), whether the extension is connected, and both
settings. Without a Chromium the launch is refused with `browser_chromium_missing` and the guide
shows `npx playwright install chromium` to copy. After 30 s without a connection it lists the
likely causes and keeps waiting. Launching while Clawdline's Chrome is already open opens one more
window in it.

## What a new Session is told

When `browser_enabled` is on as a new Claude Code, Codex, Gemini or Grok Session or child starts,
`workflow.BrowserUseInstructions()` (`internal/domain/workflow/browser_use.go`) is appended to its
launch text, the same way the optional space-saving image review is: Claude's
`--append-system-prompt`, Codex's `developer_instructions`, and the first-input context of Gemini
and Grok (`internal/app/owned_context.go`). The choice is frozen as `browser_use` in the sealed
workflow snapshot (schema 2; the digest covers it, schema 1 refuses it), so a resumed Session keeps
what it was told and turning the setting on or off reaches only executions started afterwards.
The text grants nothing: the daemon still refuses every action while the setting is off. The same
rules are `clawdline guide browser`, and the transparency page lists them as `browser-use`.

## Testing

`TestBrowserBridgeEndToEnd` (internal/transport/http) drives a real Chromium through the whole
path when `CLAWDLINE_BROWSER_E2E_BIN` and `CLAWDLINE_BROWSER_E2E_CHROMIUM` are set. Branded Chrome
ignores `--load-extension`, so it needs Chromium. It uses a temporary profile and a test server,
never the person's daemon or Chrome. `web/console/src/session/browser-panel.e2e.ts` exercises the
panel, and `browser-install.e2e.ts` the setup guide, against a stand-in daemon at desktop and phone
widths, in English and Chinese. `internal/adapters/browserlaunch` is tested with a shell script
standing in for Chromium.

## Transcript actions and takeover Notes

The transcript interleaves recent browser actions with conversation turns. It reads stored audit
rows and thumbnails, without requesting new page frames. Secret input and failed actions remain
visible as steps without a picture; neither secret text nor a secret-field screenshot is retained.
The audit survives a restart; recent pictures are memory-only and bounded separately. A successfully saved emergency
stop also survives a restart. A failed control-state write returns `browser_state_write_failed`,
keeps agent access paused and shows a retry warning. A failed request is not a durable receipt;
keep the daemon running and retry saving the stop before restarting. Taking over removes queued agent input. A control revision rejects
late replies across takeover/hand-back or stop/resume, so old page text and frames never reach
the agent after a control change. An operation already sent to Chromium may have executed before
the takeover; refusing its reply does not establish that it was undone.

An agent that needs sign-in, CAPTCHA or a consequential confirmation creates a Note with
`kind: "browser"` and the usual title, summary, action and reason. Its **Take over** button opens
the target Session's live panel. Opening the panel does not grant control: the person presses
**Take over** there before input is sent. The agent waits for hand-back. Notes never send browser
input or accept a consequential action themselves.

From the phone, **Remind me when I return to the computer** stores a browser Note in the same
Session through `chrome-remind`. Its receipt is idempotent, it starts no browser and changes no
settings. The Note remains available on this computer until the person handles it.

The first connection uses Chrome native messaging's extension-id allowlist instead of a second
console approval dialog. Opening the setup guide alone changes no settings. Its fourth step
starts with both switches checked; only **Finish** writes the two narrow browser settings.


Hand-back also withdraws queued person mutations and rejects late input replies across control
changes; already dispatched CDP actions may have executed. Sensitive type actions retain only
character count and a closed refusal code: URL, target, error prose and thumbnail are omitted.
Page element labels never fall back to editable field values. This does not claim a hostile page
cannot echo a secret into later page content; subsequent explicitly requested reads and pictures
still show the page. Installing into a person's usual Chrome profile is deferred; this delivery
uses only Clawdline's dedicated Chromium profile.
