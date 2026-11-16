# Bot browser tools

A Bot is not a terminal Session. `/v1/browser/commands` keeps requiring a live
Session (`browserCommandRoute` → `broker.LiveSession`), so a Bot's native
conversation answers `session_unresolved` there, before any browser admission.
A Bot instead reaches the same browser engine through its own turn's owned
MCP tools. This page records the backend contract; the person's view of a
Bot's tab is not built yet (see "Not done").

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

## Not done

- The person's view: `BrowserPanel` still needs a Session row. A Bot target
  should pass the Host-derived owner (resolved by the daemon from the Bot key,
  never trusted from the client), the profile and tab, through the existing
  device read/send gates for status, screenshot, control and audit
  (`Records(owner)`). Opening a tab for a Bot from the panel needs its own
  Host-verified branch in `/v1/browser/open`.
- New-site notices now name the Bot whose current conversation owns the tab
  (`Host.BrowserOwnerBot`, `browser.bot_new_site.*`) and never a terminal
  Session; a tap still opens the console's front page, because the broker's
  push carries only a Session destination. A Bot destination needs
  `internal/transport/http/push.go`, `internal/app/orchestrator` and a console
  hash route.
- Real Chrome and native Claude Code / Codex acceptance.
- The transparency page has no card describing the `browser_status` and
  `browser_command` tool text a Bot receives; only the source receipts were
  refreshed. A card needs its English and Taiwan Traditional Chinese copy.
