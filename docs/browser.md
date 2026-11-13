# Browser use through the Clawdline Chrome extension

Agents (Claude Code, Codex, Gemini, Grok) use the selected Chrome profile through one path that Clawdline
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
pressed, and the length of text typed. All typing is private: no typed text, URL, target or
post-input thumbnail is retained for that action, including ordinary text fields. Chrome can
change focus or field type between inspection and insertion, and opaque frames and closed shadow
roots cannot be classified reliably. This deliberately uses the same private result for every
input instead of letting a stale focus snapshot authorize plaintext audit. The daemon applies
the same rule when an older worker still reports a non-password field. Takeover and hand-back
are rows too. The audit
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
| `chrome-prepare` | command | `POST /v1/browser/prepare` (own extension files only) |

## The Settings block

Settings, Sessions and access, has a **Browser** block
(`web/console/src/pages/settings/BrowserSettingsBlock.tsx`): whether the extension is connected
and which browser it is in, three mutually exclusive modes plus `browser_notify_new_site`
(off on a new machine), the emergency stop with its banner and Resume, and a View activity button. The audit is read only while its modal is open,
narrowed by Session, site and day. Closing or leaving Settings aborts pending reads
and stops its polling; opening Settings alone makes no audit request. `GET/POST /v1/browser/settings` reads and writes only the mode, its legacy enabled projection, and notification preference; an agent's machine token is refused, and writing needs a device that may send.
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

The phone panel fills the screen and starts at readable CSS-pixel size. **Fit**, **100%** and the
zoom buttons, a two-finger pinch and a drag change only the viewer. A drag or pinch never becomes
a page click, including at a pan boundary. A single tap takes over before clicking and is mapped through the
transformed image to the remote page's CSS coordinates. New screenshots keep that view; rotation
remeasures and clamps it. The single bottom row uses the app's shared `--indicator` safe margin,
not `env()` alone (the page intentionally has no `viewport-fit=cover`), and `--vvh`/`--vvt` keep
it above a raised keyboard. Keys, notifications and recent actions are in **Tools**.

Screenshots include visible editable rectangles and input kinds, never field values or labels.
A native input covers each editable rectangle and follows the image's zoom and pan.
The phone directly touches that input, preserving its native keyboard focus action rather
than relying only on programmatic focus of a hidden proxy. This happens before waiting for Cloud. Text waits for takeover and the remote field click to succeed. Chinese
IME composition sends only committed text; Backspace, Enter and Tab use the same ordered input
queue. It coalesces adjacent text, holds at most 32 edits and 16 KiB, and never retries an unknown
result. Handback cancels unsent input. For fields the geometry cannot describe, including some
cross-origin frames, **Tools → Keyboard** provides a native keyboard fallback.

After takeover, **Mobile site** changes this tab to a 390 × 720 mobile viewport, mobile user agent
and touch emulation; **Desktop site** clears those overrides. An agent can request the same with
`clawdline browser viewport mobile` or `desktop`, subject to the existing ownership, stop and
takeover fences. This reflows responsive pages without reloading or discarding form input.
Server-selected mobile pages may need a subsequent navigation; changing layout does not guarantee
that a site has a mobile design. The encrypted Cloud person-input route carries the same command.

`clawdline browser launch [--browser PATH]` needs nothing installed into the person's Chrome. It
writes the extension carried in the binary (`extension/embed.go`) to
`<CLAWDLINE_NEXT_DIR>/chrome-extension-<payload-sha256>`, registers the native messaging host inside
`<CLAWDLINE_NEXT_DIR>/chrome-profile`, and starts Chromium (`CLAWDLINE_CHROMIUM`, else the newest
Playwright browser, including its newer Chrome for Testing layouts) on that profile with the extension loaded. Installed revisions are compared numerically. Sign-ins made there stay in that
profile and are separate from the person's Chrome. It passes `--use-mock-keychain`, so its cookies
are not protected by the macOS keychain; it is meant for sites the person chooses to sign in to
there. The logic is `internal/adapters/browserlaunch`, which the daemon calls too.

Chrome can keep an unpacked extension's installed service worker across a full browser restart,
even after its files and manifest version changed. Each embedded payload therefore has its own
directory and extension identity. A restarted dedicated browser loads that identity and the
native-host allowlist names it alone. Retain an older payload directory until no browser uses it;
do not treat a connected old worker as proof that new browser commands are available.

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

`node --test web/console/tools/browser-worker.test.mjs` exercises the real worker with a local native-port
substitute. Setting `CLAWDLINE_BROWSER_E2E_CHROMIUM` also runs that worker inside a fresh Chrome
for Testing profile with actual `chrome.debugger` input. Synthetic fixtures cover ordinary text,
passwords, nested and closed shadow roots, same-origin and opaque frames, and a focus switch
between inspection and insertion. The native transport is substituted in this narrow regression;
the full bridge test remains responsible for audit-to-disk acceptance.

## Transcript actions and takeover Notes

The transcript interleaves meaningful page changes with conversation turns. Reads, screenshots,
release, close and control bookkeeping stay in the full activity audit without filling the
conversation. The transcript reads stored audit
rows and thumbnails, without requesting new page frames. Secret input and failed actions remain
visible as steps without a picture; neither secret text nor a secret-field screenshot is retained.
Clicking a captured picture opens the shared image lightbox, with zoom, touch controls and
focus returned to the picture when it closes. The preview keeps that action's captured image.
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
still show the page. This delivery does not install into a person's usual Chrome profile; its optional connection guide is available separately. Acceptance on this machine uses a dedicated profile.

### Closing a tab and disabling the channel

`clawdline browser close [--tab N]` closes only a tab owned by the calling
Session, and refuses while the person controls it. `release` relinquishes
ownership and leaves the page open. A late response from a closed tab cannot
restore its ownership or return old page data.

Settings → Sessions and access → Clawdline browser controls only the Clawdline
channel. Disabling it cancels queued agent commands and viewer input, and invalidates in-flight
results, including across a later off/on cycle. An operation already dispatched
to Chrome may have run; the setting cannot undo it. Agents' native browser tools
are unaffected. Re-enabling the channel does not clear an emergency stop.

Visible page addresses retain the complete hostname, including subdomains and
`www`. New-site notifications continue to group by registrable domain.

A close reserves its owned tab before dispatch. Takeover and further agent
commands return `browser_tab_closing` until the host confirms success or failure.
The reservation is persisted and survives a timeout or restart; a timeout is not
proof that Chrome did nothing. A late host answer settles it. Turning off browser
use cancels commands still queued, but a close already sent may finish; its tab
cannot be acknowledged as taken over in the meantime. No control receipt promises
to undo an already dispatched browser operation.

## Three browser modes

`browser_mode` is `native`, `user_chrome` or `isolated`. Native means no Clawdline
browser commands or new-session browser guidance; agents can use their own tools.
User Chrome uses the person's usual profile after a separate extension/native-host
connection. Isolated uses Clawdline's dedicated Chromium or Chrome for Testing.
An absent mode migrates only from the old boolean: true means isolated, false or
absent means native. An invalid explicit mode fails closed. `browser_enabled`
remains a derived runtime projection; conflicting writes are refused. Its raw persisted legacy boolean is true only for isolated mode, so an older daemon fails closed instead of interpreting user_chrome as permission for its old channel.

Changing actual Chrome profiles withdraws queued commands, fences old results and
viewer input, and relinquishes old tab ownership without closing the old pages.
Turning access off and back on in the same profile retains its tabs. Viewer
requests include `browser_mode`; old viewers without it address only isolated
Chrome. Hosts pin mode/profile in separate launchers and cannot consume commands
from another selected mode. The worker's hello cannot overwrite that binding.
A pending close must settle before changing actual profiles.

The user-Chrome setup is deliberately separate from choosing the setting:

```sh
clawdline browser prepare --mode user_chrome
```

The guide’s Prepare current extension button calls `POST /v1/browser/prepare`
(`chrome-prepare` through Cloud). This requires a sending person device, rejects
machine tokens and other selected modes, and accepts no filesystem path. It
returns the canonical extension folder, its ID, a connection command, and on
macOS a shell-quoted `open` command. The folder picker also supports
Command-Shift-G and pasting that path. The Terminal command above uses the same
preparation operation. Neither route registers a host or starts Chrome.

Preparation only writes the content-addressed extension folder inside Clawdline's state
directory and prints its extension ID and a connection command. The person loads
that folder with Load unpacked at `chrome://extensions`, then runs the printed
`install-host --mode user_chrome --extension-id ...` command and reloads the
extension. A nonstandard user-data directory can be named with `--chrome-dir`.
These installation steps are not performed by this delivery. Dedicated launch
continues to register only its own profile. Do not substitute a launch of the
person's usual Chrome for the separate connection flow.

Cloud carries the selected viewer mode, the three settings choices and responsive
layout through the encrypted chrome-* words. Folder preparation adds the
write-gated `chrome-prepare` word. Tests use temporary profiles and native-host manifests; a test labelled
user_chrome does not establish installation into the person's real Chrome.

Mode preconditions and config persistence share the bridge's command-admission
fence. A close cannot enter between a successful profile check and the saved
choice. A rejected close precondition never writes config. Control-state disk
failures after a saved choice are reported separately and pause access; the
settings UI reads back the actual saved choice instead of guessing it rolled
back. Viewer action callbacks, frames and keyboard lifetime are scoped by mode
and tab, so profiles reusing a tab number cannot reuse an old callback or image.

Setup transport readiness does not grant browser access. Close result receipts survive
service and host restarts without replaying the destructive operation. The protocol,
limits, failure boundaries and regression fixtures are described in
[browser setup and close result recovery](browser-reliability.md).


### Extension update boundary and proposed automatic flow

The application carries its current extension, but an already loaded unpacked
extension does not switch directories when the application updates. Preparation
publishes a complete revision directory atomically and verifies an existing one
without rewriting files a browser may be reading. It refuses changed or incomplete
files instead of silently replacing them. Old revision directories are retained.
A preparation receipt establishes file readiness, not a running worker version.

The current guide describes an explicit update: hand back control, prepare the
current files, and if the directory changed remove the old Clawdline extension,
load the new directory, and run its matching connection command. Reloading the
old directory alone does not update it. This delivery does not install or reload
the person's usual Chrome.

Automatic updating needs a separate lifecycle protocol before it can be enabled:
a stable approved installation identity or a verified identity transition; a
worker-reported embedded revision rather than a fetch of newer on-disk metadata;
a per-profile handback and admission fence that waits for pending commands and
uncertain close receipts; a durable update intent that survives daemon or worker
restart; and an observed new-worker handshake before success. Refusal or lost
acknowledgement must preserve the old installation and report the unresolved
state without blindly retrying. Dedicated and usual Chrome require separate
consent and receipts. This is a proposed design, not implemented automatic reload.
