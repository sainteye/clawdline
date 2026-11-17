# Bot browser tools

A Bot is not a terminal Session. `/v1/browser/commands` keeps requiring a live
Session (`browserCommandRoute` → `broker.LiveSession`), so a Bot's native
conversation answers `session_unresolved` there, before any browser admission.
A Bot instead reaches the same browser engine through its own turn's owned
MCP tools. This page records the backend contract; the person's view of a
Bot's tab uses the shared browser panel described below.

## Path

```text
native CLI → existing owned HTTP MCP (ACP session descriptor)
  → browser_status / browser_command
  → app.BrowserTools on the Host's NativeWork
  → bound(): current Fence, active turn, deadline, source, feature, lifetime
  → projectassistanthost.Options.Browser (composition callback)
  → browserbridge.Profiles.DoOwned / SelectedStatus
  → the same Bridge queue, native host and extension
```

No new route, token, BotID API, shell bridge or external browser protocol was
added. The callback is set in `internal/transport/http/project_assistants_host.go`
and implemented in `project_assistants_browser.go`.

## Frozen types

- `app.BrowserTools` (optional, like `OutputTools`):
  `BrowserStatus(ctx) (BrowserStatus, error)`,
  `BrowserCommand(ctx, BrowserCommand) (BrowserResult, error)`.
- `app.BrowserCommand{Op, Args json.RawMessage, Mode}`. `Mode` may only name a
  profile automatic selection already allows; there is no `explicit` field.
- `app.BrowserStatus{Mode, Connected, Stopped, Tabs []BrowserTab}`, where
  `BrowserTab{Tab, Mode, Current, Person}` lists only the owner's tabs.
- `app.BrowserFailure{Code, Message}` carries the engine's refusal code
  (`browser_stopped`, `browser_person_controlling`, `browser_setup_required`,
  `browser_native_tools`, `browser_session_busy`, `browser_tab_closing`,
  `browser_timeout`, `browser_cancelled`, …) or `browser_page_error` with the
  extension's message.
- `projectassistanthost.BrowserAccess{Status, Command}` takes the owner from
  Host only.

## Owner

`projectassistanthost.BrowserOwner(fence, forgottenThrough)` is
`"bot-" + hex(sha256(...))` (68 characters, within the engine's 128) over
length-prefixed fields: a version tag, machine, Bot, the complete Project scope
(machine, Project, canonical ID, kind), conversation and forget epoch.
Generation, binding, native session and turn are excluded, so a resumed or
restarted turn keeps its tabs; another conversation, Project, Bot, machine or a
forget does not reach them. The owner is a key, not a credential: authority
comes from each call's `bound()` check.

First version: only the person's main chat lane. A routine `SessionSlot` and
Bot-to-Bot or host-notice turns answer `pa_scope_denied`.

## Engine rules for an owner

`Profiles.DoOwned` uses the same policy, saved choice, stop, person control,
queue, epochs and audit as `DoSession`, plus:

- every non-open operation needs a tab this owner opened in the selected
  profile (`browser_tab_other_session` otherwise, including unowned isolated
  tabs the Session path still allows);
- `tabs` is refused; `browser_status` lists the owner's tabs instead;
- a caller cancelled before admission enqueues nothing, and `Poll` never hands
  out a command whose caller was already cancelled (`browser_cancelled`);
- once a host took a command, cancellation answers `browser_timeout`: the effect
  may have happened, and nothing is replayed. A click Chrome already received
  cannot be withdrawn.

Ending the turn cancels the bound context (and the Host lifetime seal), which
withdraws a still-queued command. The seal is not redundant: the runtime
closes the provider session before it cancels the turn context, and ACP Close
waits for the native process to exit. During that cleanup only the seal
withdraws a command still waiting for the host
(`TestHostSealWithdrawsAQueuedCommandDuringNativeCleanup` fails without it).

A remembered browser choice never falls through to another browser after a
possibly executed action: turning the remembered profile off answers
`browser_disabled`, not a switch to the next automatic one. Fallback and
`browser_native_tools` / `browser_setup_required` apply to a Bot conversation
before its first admitted command, as they do to a Session.

## MCP

`browser_status` takes no arguments. `browser_command` takes `op` (open, read,
click, type, key, scroll, screenshot, viewport, release, close), optional `args`
(the CLI's JSON arguments unchanged) and optional `browser_mode`. Unknown
fields are `pa_invalid`. A screenshot answers a text caption plus a standard MCP
`image` content (`image/jpeg`, base64 as the extension produced it). A result
that would not fit the existing owned-MCP frame answers
`browser_result_too_large`; nothing is truncated and no limit was raised.

## Verification

`TestBotBrowserRunsEndToEndThroughACPAndOwnedMCP`
(`internal/transport/http/project_assistants_browser_test.go`) is one path:
the production ACP provider launches a scripted subprocess with the formal ACP
handshake; `session/new` carries the Host-owned HTTP MCP descriptor; the
process initializes MCP over TCP, lists the tools and calls `browser_status`
and `browser_command`; each command passes the Host binding checks, the
production Profiles and Bridge, and a scripted native host's Poll and Deliver;
the reply is read back from SQLite. It covers open, read, click, type, read,
screenshot (a decodable JPEG image block) and close, each with a command id
and an audit row under the Host-derived owner; the typed codes
`browser_page_error` (with its message), `browser_disabled`,
`browser_person_controlling`, `browser_stopped`, `browser_tab_closing`,
`browser_setup_required`, `browser_native_tools` and
`browser_tab_other_session` with no host admission; a finished turn's replayed
descriptor admitting nothing; and the CLI route's `session_unresolved` for the
Bot conversation. The scripted subprocess and host are not a model or Chrome.

`TestAnOwnedLateResultCannotCrossAPersonsABA` covers a late owned read or
screenshot across takeover/handback and stop/resume.

## The person's Bot chat

The chat's More menu opens the shared `BrowserPanel` with an explicit Bot
target. The Session target retains its existing transport. A Bot uses its
captured `SourceClient`, for both local HTTP and Cloud `chrome-*` words. No
selected-machine singleton chooses its source. Every browser request carries
`bot_id` and the Bot's current `conversation`, and carries neither an owner nor
a machine ID. The daemon resolves the owner and filters status, screenshots,
activity and the complete audit. Browser control, opening and muting use the
same device and Bot write gates as the backend.

The chat header also reuses the Session's Chrome chip when targeted status
contains an owned tab in the selected profile. It reads status immediately,
every 15 seconds while visible with the panel closed, and when the page becomes
visible again. Requests are serialized, cancelled on scope changes, and pinned
to the Bot's source and current conversation. Read-only devices get the same
entry; sources below API 74 send no browser status request.

Assistant replies with a `browser_status` or `browser_command` tool trace in
their own turn show an **Open browser** action. Tool details are read serially
from the existing bounded trace collection without opening the trace disclosure;
thoughts, reply text, unrelated tools, and other turns do not add the action.
Observed live browser traces remain attached to that turn while the chat is
mounted. Automatic detail failures do not lock the chat or replay browser actions.
The trace reader exposes the latest record per turn/category: on reopening,
only the retained tool record can establish this action; earlier tool calls
replaced by a later non-browser tool are not recoverable from that summary.
Older trace pages follow the existing More action. The reply entry remains
available after a tab is released and opens the existing empty state. Both
entries use the same panel selection of the current owned tab (or last owned
tab), takeover, handback, and write gates. Closing restores focus to the reply opener when available, otherwise the More menu.

A source below API 74 is refused before any targeted request is sent: older
sources ignore unknown GET fields and could return a machine-wide view.
Read-only devices can inspect the picture and audit; write controls are
disabled with their reason. Unpaired, unavailable and old sources keep their
reason. The browser setup of another machine is never opened from the Bot
panel. `pa_stale_version` freezes the panel, reloads Bot identity and requires
another explicit open; it never replays the old action. A lost write result
keeps its original outcome and is never automatically sent again. The modal
restores its menu focus on close, contains keyboard focus, and supports Escape.

The `MachineNotifyBot` helper carries a Bot destination through the existing agent notice
switch, hourly allowance and notification ledger. The tap fragment contains
`bot`, `conversation` and, when known by the daemon, `machine`. The fragment
selects a chat when Bots is mounted, never a browser owner: the current Bot still supplies the
conversation for each panel call. An ambiguous machine selection is not
resolved by falling through to the active machine. Session notifications keep
their existing destination.

`bot-browser.test.ts` exercises API gating, local and Cloud target fields,
read-only and revoked grants, stale outcomes, and the Cloud source capture.
`push_bot_test.go` checks destination construction, the shared switch/ledger,
and the new-site tap. It also executes the actual TypeScript SourceClient
and passes all eight resulting requests through the real Go Cloud decoder,
including a negative audit request without its required limit. `TestBotChatBrowserPanelE2E` is an explicit browser test
against an empty task-owned Go daemon on a temporary TCP port and a scripted
native host. It launches a separate headless Chrome profile and the production
Bots/Chat/BrowserPanel components. It covers local and mapped Cloud traffic,
English and Taiwan Traditional Chinese, desktop and phone sizes, read-only and
old sources, takeover/handback, mute, audit, opening a first tab, and stale/lost
write responses. It also checks the owned-tab header chip, the reply action
loaded from durable tool details, and a reply opening the empty state after no
tab remains. `TestBotBrowserReplyEntryFacts` verifies that the fixture's trace
and reply are linked in SQLite without a provider call. The Cloud fixture maps words into real daemon routes; it does
not prove production relay encryption or pairing. The screenshot is a synthetic
JPEG from the scripted host, not real Chrome automation or a physical phone.

Run the UI fixture explicitly from the repository root:

```sh
BOTS_BROWSER_UI_E2E=1 BOTS_ARTIFACTS=<task-artifacts> \
  go test -v ./internal/transport/http -run '^TestBotChatBrowserPanelE2E$' \
  -count=1 -timeout=3m
```

The fixture discovers an existing Playwright Chromium headless shell from
`PLAYWRIGHT_BROWSERS_PATH` (or the OS account's standard browser cache), with an
explicit `CHROME` executable override. It installs nothing and keeps its
profile in task-owned temporary storage. The Go test passes the cache path
separately because this package deliberately replaces HOME with an empty one.
The normal-environment matrix passed all six panel modes, the first-tab open,
and the stale/lost response cases, with six screenshots and no model requests.
A physical device, an agent driving actual Chrome, and production Cloud still
have separate acceptance below.

## Remaining integration and acceptance

- Real Chrome and native Claude Code / Codex acceptance, and production Cloud.
- The transparency page has no card describing the `browser_status` and
  `browser_command` tool text a Bot receives; only the source receipts were
  refreshed. A card needs its English and Taiwan Traditional Chinese copy.


## Leaving the person's browser panel

Closing the Bot browser panel, pressing Escape or unmounting it while returning
to another view now hands its human-controlled tabs back automatically. The
panel captures the exact source, Bot and conversation; handback first waits for
an in-flight takeover or open, then reads fresh ownership, returns only that
owner's human-controlled tabs in their exact Chrome profiles, and reads again
to confirm the handback. It leaves the tabs open for the Bot.

Explicit close waits for confirmation. A refusal keeps the panel open with its
code; a forced view exit reports failure through the existing toast. An unknown
handback is never blindly repeated: another explicit exit first reads current
ownership. Changing source authority cannot retarget the old request. Pending
panel reads are cancelled after the handback settles, while React StrictMode's
effect replay on a still-connected panel does not relinquish control. This is
view-exit behavior, not a guarantee that a network request survives terminating
the entire browser or losing connectivity.

`browser-handback.test.ts` covers profile/tab isolation, pending takeover,
unknown outcomes and malformed ownership. `bot-feedback.e2e.mjs` uses the actual
Bot panel in Chromium/WebKit, in English and Traditional Chinese, and checks
close, Escape, unmount and leaving during takeover. Its source and Chrome facts
are synthetic; it does not drive the person's real tabs.
