# Provider failure metadata and pre-send Agent fallback

The original prerequisite slice preserves native ACP error classification.
That classification alone does not choose another Agent, change retry policy,
or prove that a failed prompt had no effects. The pre-send backend slice below
adds a separate authoritative guarantee and cleanup transition. A running
prompt that fails remains unknown under
the existing runtime policy, even when its error is classified as quota.

## Internal contract

`internal/app/projectassistant.ProviderFailure` is an application-only error
wrapper. `errors.As` exposes `Reason` and `RPCCode`; `Unwrap` preserves the domain
failure used by `pa.Code`. RPC errors still have `pa.Unsupported` and the safe
`rpc_<code>` field. The classification wrapper remains private; the pre-send
slice below adds optional fallback configuration and attempt facts to the
existing wire. An empty reason means
the RPC error has no recognized classification.

The adapter inspects only actual JSON-RPC error objects. Native error messages,
details and arbitrary data never enter the wrapper, error string, projection or
persistence. Ordinary final replies containing quota or authentication words
remain ordinary replies.

## ACP volume and response ordering

ACP incoming notifications, replay and text capture follow Codync's native
transport behavior: there is no local notification-count, line-size or captured
text-size ceiling. The former 256 queued-event disconnect and 1 MiB ACP/capture
rejections are removed. Reader-owned control responses resolve pending RPCs
directly so a sink waiting for a control answer cannot block its own response.
The ordered consumer still handles notifications, permissions, prompt completion
and the load-replay barrier in wire order. Historical load notifications are
drained while no turn is active, before a new prompt can receive them.

The consumer is started before initialization. Closing wakes its condition
variable, cancels its sink context, drops queued events and retains the existing
owned process cleanup protocol. Queue entries are released as consumed. The
existing RPC/permission ownership, write deadlines, source fences and one-write
prompt behavior remain. Final message/storage admission is unchanged; native
stream capture does not authorize a larger stored message.

Native `max_tokens` and `max_turn_requests` stops retain distinct typed
`prompt_max_tokens` / `prompt_max_turn_requests` fields instead of becoming a
local capacity error or ordinary success. The runtime's unknown barrier still
prevents automatic replay after these stops.

`TestACPBurstPreservesSlowSinkOrderAndPermission` injects 288 notifications with a
slow sink and separately exercises synchronous permission answers and a control
RPC awaited inside that sink. `TestACPLoadReplaysHistoryBeforeResponseWithoutCapture`
replays 1,024 historical notifications before the load response.
`TestACPNativeTextExceedsFormerLocalCaptureAndFrameCaps` checks both aggregate
stream text and a single frame above 1 MiB. Close and provider-stop tests cover
cancellation and native limits. These synthetic reproductions do not identify
the exact cause of any earlier production capacity failure whose cause was not
retained.

| Trusted input | Normalized reason |
| --- | --- |
| RPC code `-32000` | `signed_out` |
| `usage_limit_exceeded` | `usage_limit` |
| `billing_error`, `account_on_hold` | `out_of_credits` |
| `rate_limit`, `rate_limit_exceeded` | `rate_limited` |
| `overloaded`, `server_error`, `server_overloaded`, `internal_server_error`, `response_too_many_failed_attempts` | `overloaded` |
| `authentication_failed`, `oauth_org_not_allowed`, `unauthorized`, `auth` | `signed_out` |
| `model_not_found` | `model_missing` |

The first string among `data.errorKind`, `data.category`, and Codex's
`data.codexErrorInfo` wins before validation. Codex camelCase names are converted
to snake_case; object forms use their first lexically ordered key. Categories
must contain only lowercase ASCII letters and underscores and fit in
`bots.error_kind_bytes` (40 bytes). Invalid or unknown categories stay
unclassified; they do not fall through to a lower-priority category.

For the recognized rate/billing/account categories, Claude's usage-limit text in
the RPC message or details takes precedence over billing. This follows Codync's
`host/src/agent/acp.rs` (`rpc_error`, `codex_kind`) and
`host/src/agent/fallback/reason.rs` at
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`. Clawdline deliberately omits upstream's
uncategorized message-text fallback: quoting usage words in an unknown category
does not gain classification. Native process/setup/model/mode policy and error
conversions outside the RPC classification boundary remain unchanged.

## Verification boundary

`TestACPProviderFailureClassification` sends genuine JSON-RPC error frames from
the owned subprocess fixture into the production Client and PendingPrompt.Wait.
It checks errors.As, unchanged pa.Code/field semantics, sanitized error strings,
usage-before-billing precedence, native category variants and negative controls.
`TestACPReplyQuotaWordsAreOrdinaryText` covers ordinary successful output.

`TestACPProviderFailureRuntimeDoesNotReplay` crosses the actual ACP client,
SQLite repository and Runtime together. A prompt is recorded by the peer before
quota, quota followed by EOF, or direct EOF. Each case becomes TurnUnknown;
repeating the same submission and invoking recovery still yields one provider
Open and exactly one written prompt. Application tests also preserve unknown
codes and cleanup-error precedence through error wrappers.

These fixtures call no external provider or model and use no credentials. On a
machine without native process inventory, subprocess tests explicitly skip;
such a run is not native acceptance. Focused commands are:

```sh
go test ./internal/adapters/projectassistantacp -run 'TestACPProviderFailure|TestACPReplyQuota|TestACPErrorKind' -count=1 -v
go test ./internal/app/projectassistant -run 'TestProviderFailure' -count=1 -v
go test ./internal/domain/capacity -run 'TestEveryBoundIsRegistered|TestTheRegisterMatchesItsSnapshot' -count=1
go test ./internal/transport/http -run 'TestEveryRegisteredRowIsMeasured|TestCapacityMeasuresMatchTheirSnapshot' -count=1
```

Native qualification, complete native tool interception, UI and deployment
acceptance remain separate work. The runtime/store extension below adds
observed progress, per-Agent parking and quoted recap/catch-up. Typed quota alone
never authorizes replay; EOF, partial writes, cancellation and cleanup ambiguity
retain their existing unknown-effect barriers.

## Bot Agent fallback: initial pre-send backend slice

This section records the initial slice; its progress and fresh-session-only
behavior is extended by "Durable runtime progress and return to the primary"
below.

This is a backend implementation for configured, default-login Claude Code and
Codex identities. It is not acceptance of the complete fallback Board step, a
native-provider qualification, or a public feature release. Bots remain behind
the existing feature gates. No CLI, login, native memory setting, permission
policy, model or command is changed.

### Observable contract

Create and edit accept optional `fallback_agents`, using the existing `claude`
and `codex` enums. A new Bot defaults to no fallbacks. A Bot without a
fallback chain does not consult shared rest to block its primary. When every
configured Agent is resting, a new human turn still tries its primary, as
upstream `relay_turn` uses `next_step(now).unwrap_or(0)`. An already attempted
Agent is never retried inside the same logical turn. An omitted edit preserves
the saved chain; an explicit empty array clears it. The primary Agent cannot be
edited. Identities must be unique, including the primary, within Codync's
three-fallback ceiling. With only these two default-login enums available, at
most one alternate is currently valid. A fixed non-Auto mode refuses an
alternate until a no-freer stand-in mapping is qualified; this does not block
new primary input merely because the alternate mode is unqualified.

Each new visible message chooses the earliest untried, non-resting identity,
subject to those primary-start and alternate-mode rules.
Routines do not select an alternate from this chain. A main binding on another
Agent (or revoked binding) refuses the routine before Open; routine/fallback
interaction remains separately qualified by the routine owner.
The primary `Bot.Agent`, BotKey, full ProjectScope, ConversationID, accepted
message ID and logical Turn ID remain unchanged. `ExecutionBinding.Agent`,
`OpenRequest.Bot.Agent` and the fresh handoff identify the effective Agent;
`OpenRequest.PrimaryAgent` separately retains the configured primary. The
existing two-Agent router receives the effective configuration. Every attempt
has its own binding and increasing generation. One existing prompt deadline
and process slot cover the entire logical turn, including cleanup and switching.

A provider may return `PreSendRefusal` from **Open**, with an owned cleanup
handle, only when it can authoritatively guarantee no prompt or external
action occurred. The wrapped `ProviderFailure.Reason` is diagnostic metadata,
not that guarantee. The Host invalidates the old tools on an Open error; any
tool invocation or native event observed during Open contradicts a no-effect
claim and forces unknown. The runtime also rejects contradictory durable
permissions, work links, output, messages or trace facts. Absence of those
facts alone is never proof. This slice conservatively holds any permission
offer rather than transferring permission authority to another Agent.

The runtime appends a refusal fact and revokes the old binding, confirms Close,
then atomically appends confirmed cleanup and the shared rest. Only afterward
may it mint another fence and tools. It rechecks stop, feature/source access,
Project resolution, configuration, memory and the current binding before Open
and before Send. A configuration change between attempts refuses the switch.
Old events, tools and permission answers remain fenced out. Missing handles or
failed cleanup retain the original owner and process slot. A separate host-only
`OpenNotStarted` fact can reclaim an unused slot when provider Open was never
entered and setup registrations were reclaimed; it never permits fallback. Providers never
share an MCP lifetime. There is no same-Agent Send retry.

Turn `attempts` is an append-only sequence of immutable facts: ordinal, effective
Agent, full fence, stage, whether Send began, effect knowledge, cleanup knowledge
and normalized reason. A `sending` fact commits before the external write.
After Send, effects remain `unknown` even if an answer and successful Close are
observed; this implementation has no authoritative native side-effect ledger.
The final answer and one bilingual switch notice commit through the existing
terminal receipt. Explicit native messages still suppress duplicate final text.

| Boundary | May open an alternate? |
| --- | --- |
| Open returns authoritative pre-send refusal, owned handle and confirmed Close | Yes, only another configured eligible identity |
| A typed usage/category error without the explicit guarantee | No |
| Open returns no handle, cleanup fails, or any host tool was invoked | No; unknown owner/effect is retained |
| Send/Wait returns quota, EOF, timeout, partial-write or uncategorized error | No |
| Permission answer or Broker response is lost | No; existing unknown receipts remain authoritative |
| Crash after opening, refusal, cleanup, transition or sending fact | No automatic advancement or restart continuation |

Recovery marks interrupted configured-fallback turns unknown and does not use
the ordinary restart notice to send them again. A person's next explicit input
is a new logical turn, not replay of the interrupted input. Existing unconfigured
Bots keep their ordinary restart behavior. SQLite CAS, receipt uniqueness and
transactions own these transitions; a failed receipt insert rolls back the
attempt/rest change. Historical multi-Agent bindings require matching attempt
facts. Feature schema 2 protects these additions from older readers; installed
v1 data migrates atomically without rewriting history, and valid legacy archives
remain restorable. New archives declare v2 so old binaries refuse rather than
drop attempt facts. Unknown newer versions remain refused; see the
[storage migration and rollback boundary](project-assistants-store.md). The
machine-owned rest table is additive.

### Upstream reference and rests

The read-only reference was verified at Codync commit
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`. Citations are to that exact source:

- `host/src/agent/fallback/mod.rs:20-69,115-132`: primary plus at most three
  fallbacks, unique login identities and effective configuration.
- `host/src/agent/fallback/rest.rs:11-76`: durable shared-login rest; transient
  failures use 1, 5, 25 and 60 minutes. Backoff restarts one hour after the prior
  expiry. Usage with unknown reset rests one hour; no credits rests six hours;
  supported sign-in harnesses rest one hour. Claude and Codex are the supported
  harnesses represented by this slice. Model/mode absence creates no global rest.
- `host/src/agent/fallback/rest.rs:72-76`: upstream can use the latest exhausted
  window reset. This slice has no qualified reset telemetry and therefore uses
  the upstream unknown-reset duration. Usage percentage never selects an Agent.
- `host/src/agent/fallback/reason.rs:62-103`: typed failure categories. Existing
  Clawdline classification is reused unchanged; arbitrary chat/error text does
  not become an authoritative no-effect result.
- `host/src/agent/bot/fallback.rs:348-411,485-515`: bounded advancement without a
  generic retry, and primary eligibility on the next visible turn after rest.
- `host/src/agent/bot/fallback/park.rs:38-105`: upstream kills the old connection
  before taking a parked session. The initial slice used confirmed owned
  cleanup and a filtered fresh-session handoff; the runtime extension below
  restores each Agent's own confirmed-clean history.

Rests are keyed by machine plus Agent and survive Host/database reopen and Bot
deletion. They are separate from Bot memory/export. A successful answer clears
the answering identity's rest in the terminal transaction. Clock expiry changes
selection only on the next message, never mid-turn. The capacity register covers
the three-entry configuration ceiling and upstream rest durations up to six
hours. The HTTP capacity reader measures actual remaining durable rest without
constructing a native provider.

### Verification and remaining work

`TestProjectAssistantFallbackHTTPKeepsLogicalTurnAndDurableRest` uses the real
HTTP routes, Service, Engine, Host and fresh SQLite: known Claude Open refusal,
confirmed cleanup, one Codex Send, one final answer/notice, persisted rest across
Host/database reopen, then fake-clock expiry returning Claude on the next input.
It also restores a legitimate multi-Agent archive and refuses a newer export.
`TestProjectAssistantFallbackHTTPWithOwnedOSProcesses` repeats the pre-send
path using two owned helper OS processes under the production named budgets;
both PIDs are reaped and neither installed vendor Agent is constructed.

Unknown-path tests cover diagnostic-only errors, missing handles, failed cleanup,
Open tool effects and Send/Wait errors. SQLite failure injection covers refusal,
confirmed-cleanup receipt, alternate start and pre-Send persistence boundaries;
reopening and explicit recovery never restart those inputs. Configuration tests
cover read-only/foreign/stale writes and omitted versus explicit-empty edits.

Native adapters deliberately do **not** construct `PreSendRefusal` in this
change. Their existing typed quota errors therefore remain unknown after Send
and do not automatically switch. Installed native A/B/A, authoritative pre-send
qualification, complete native tool/effect interception, completed-action
verification, fixed-mode stand-ins, sign-in/reset telemetry,
fallback editing/status/switch-back UI and broader exhaustion UX remain separate
work. Codex native resume is qualified separately by the common-flow
[installed-agent acceptance](project-assistants-native-acceptance.md); that proof
does not qualify native fallback switching or mid-turn effects. No model recap
calls are made.

## Existing Bot settings

API72 sources expose an ordered **Alternate Agents** list in the existing Bot
settings, following Codync's `FallbacksRow`: numbered rows in try order, each
saying when it takes over ("takes the message when <previous Agent> cannot
start it before sending"), with Move up, Move down and Remove, and an
"Add alternate Agent" picker. The list starts empty when nothing is stored;
opening the form never chooses one. The picker never offers the primary or an
Agent already in the list and disappears at `MAX_FALLBACK_AGENTS` (3, the
source's registered `bots.fallback_agents`, not a new bound). With only the
shipped Claude Code and Codex identities, one alternate is currently possible;
the same list orders more once the source accepts more identities. A stored
identity this console does not recognize is shown by its raw name and kept
until removed. Reordering is a deliberate change and is sent; an unchanged
order is omitted. Saving configuration never opens a provider or sends a
prompt. Selecting an identity does not qualify an installed native adapter to
issue the pre-send guarantee described above.

Deliberate differences from Codync: no per-alternate model or login profile
(Clawdline alternates are default-login Agent identities only), no rest/"In use"
status rows and no switch-back button (status and switch-back are later work),
and moves are buttons rather than a menu so they stay reachable on phones.

`Bots` provides the selected immutable SourceClient through `FallbackSourceScope`;
the form never consults the globally selected machine. An absent optional
`canFallbacks` capability means unavailable. The production client checks API72
again at the effect boundary whenever `fallback_agents` is present, including
explicit clearing. Older-source metadata edits omit the field. Source authority,
read-only/disabled status, current grants and post-await revocation keep their
existing independent checks, including paired browser surfaces.

The form captures its original version and alternate configuration on opening.
Unchanged alternates are omitted from the edit, preserving configured data on
unrelated saves. Fixed modes retain the existing order (it can still be removed) but offer no new alternate;
Auto is required for the currently qualified alternate mapping. Stale and rejected
saves keep the form. An unknown save also locks its controls, retains the visible
selection and never generates an automatic retry. Reopening reads the actual
source before another deliberate edit.

The pinned Codync `9f67eb09084db61b3cca9c84a11bc03df9de29b7` implementation
was inspected directly: `apps/desktop/src/renderer/views/bots/Fallbacks.tsx:45-81`
keeps alternates in the draft until the ordinary form save, excludes the primary
login and starts empty; `BotEditorView.tsx:162-187` places these controls on the
existing Agent card. `host/src/agent/bot/fallback.rs:348-411` closes the prior
attempt and advances the same turn's original entries; `turn.rs:391-393`
checks queued user messages independently. This UI increment uses Clawdline's
narrower pre-send boundary, not upstream mid-turn replay or an unqualified
native-mode stand-in.

Focused helper and captured-client tests cover omission/clearing, both shipped
identities, fixed modes, API71, read-only and revoked authority, and late
revocation becoming an unknown write without retry. The optional
`TestProjectAssistantFallbackSettingsBrowser` starts a real authenticated Go TCP
source with empty SQLite and a scripted provider, then invokes the installed
Clawdline browser CLI through `project-assistants-fallback-settings.e2e.mjs`.
It never installs or probes a vendor executable and makes zero vendor/model/API
requests. Set `CLAWDLINE_FALLBACK_SETTINGS_E2E_DIR` to a task-owned evidence
directory and `BOTS_BROWSER_HOME` only for the existing browser broker's CLI.
The product fixture itself retains its empty isolated HOME and store.

The initial same-page acceptance exposed a pre-existing producer defect:
`bot_updated` events can carry a zero-scoped `read_cursor`; the strict browser
read validator rejects that event and retains its recovery barrier. The settings
fixture therefore uses full source reopens between configuration saves, and
after sending it waits on the source export before reopening to read the
alternate's answer. This does not establish passing same-page post-edit or
post-send event handling; the source projection needs separate integration
before that claim. The read validator and authority fence are not relaxed by
this increment.

The browser run drives the person's configured Chrome through `clawdline
browser` (the same broker the routines acceptance uses); set
`BOTS_BROWSER_CLAWDLINE_DIR` to that broker's configuration directory because
the Go fixture isolates HOME. It covers: empty list, add/save/reopen, remove,
unrelated save omitting the field, a real send that switches to Codex before
sending, idempotent receipt replay, Taiwan Traditional Chinese at phone width,
API71 (control disabled with its reason, field omitted), a read-only grant
(fieldset disabled with the read-only reason, no PATCH), a real expected-version
conflict keeping the form, an injected lost response locking the form without
another PATCH, and a fixed mode keeping the saved order without an Add picker.

## Durable runtime progress and return to the primary

This increment owns runtime and SQLite behavior. Contract generation, installed
Host interception, UI presentation, schema-version coordination and deployment
must be integrated before claiming complete product acceptance. It makes no
model recap requests and changes no native provider's fallback qualification.

`Turn.Attempts` remains an append-only ledger in the aggregate's SQLite
transaction, together with its command receipt and events. The normal lifecycle
is `opening -> opened -> sending -> sent -> answered -> closed`. `opened`
records a successful Open, `sending` commits before calling Send, `sent` means
Send returned a pending handle, and `answered` means Wait returned a result.
These local observations do not prove remote acknowledgement or completion of
external side effects. Cleanup is confirmed only after the owned Close succeeds.
Provider/permission/message events atomically record sticky `native_activity`
and `tool_activity` facts; dispatch journals tool activity with its existing
unknown-effect receipt before contacting the WorkBridge. The event IDs and work
links retain the underlying observations. A claimed tool completion remains a
provider report, never authority to repeat an effect.

`Engine.RecordToolActivity(ctx, fence, turnID, callID)` is an additive Host
integration boundary. A Host must await this journal before invoking **every**
NativeWork tool, including tools without visible output. Failure must propagate
without invoking the tool. Calls use a source-owned stable invocation identity;
repeating the journal is idempotent and conveys no replay permission. The
existing owned-provider lifetime check still seals calls on refusal/cleanup.
Until this boundary is connected in all Host callbacks, this increment proves
observed events and runtime dispatch, not comprehensive native tool interception.

The worker reloads the durable turn when it appends lifecycle facts, so a
concurrent tool or permission event cannot be overwritten by the older worker
snapshot. SQLite refuses removed facts, rewritten prefixes, regressing
observations, contradictory no-effect claims and foreign resume provenance.
All known Send/Wait activity and any unknown cleanup still prohibit automatic
fallback, even if a provider incorrectly returns `PreSendRefusal` afterward.
A crash or failed journal never advances to another attempt on recovery.

Historical main-slot execution bindings are durable per-Agent parking slots;
no second session table or model-generated session map is needed. On a switch,
the runtime considers only the newest binding for the configured identity in
the same Bot, full ProjectScope and Conversation. It requires confirmed cleanup
and either a successful turn or a proven no-send refusal. An unsafe newest
binding cannot resurrect older history. Each restored native session receives a
new execution binding and generation; the historical revoked fence stays
revoked. Native session IDs never move between Agent identities. The primary's
rest expiry is considered on the next explicit input, and the configured
ordered Agent chain fixes the primary/alternate mapping. Fixed non-Auto mode
stand-ins remain unqualified and refused.

A fresh replacement receives a recap through the existing provenance-checked
SessionHandoff. A parked session receives a catch-up of chat after the last
message it saw. The watermark is derived from successful turns on that exact
Agent/native-session pair, excluding an input refused before Send. The carry
also quotes terminal attempt facts, durable work links and one most-recent tool
report per selected source turn. Tool reports use the existing continuation
quote budget and explicitly retain `reported_text`/`reported_complete` wording.
Nothing parses those reports to decide whether to switch, resume or replay.
JSON quoting and a bilingual untrusted-data preamble separate this material
from the current explicit input. Existing continuation/quote bounds, forget
provenance, instruction revisions and native-memory policy remain authoritative;
forgetting or changing fixed instructions starts filtered history rather than
loading the parked opaque session.

Differences from the pinned CodyNC implementation are deliberate:

- `host/src/agent/bot/fallback/park.rs:38-105` uses best-effort KV parking and
  last-chat sequence keys. This runtime uses confirmed-clean historical SQLite
  bindings and derives the sequence from successful native-session ownership.
  A bookkeeping failure stops the attempt rather than merely logging a warning.
- `host/src/agent/bot/fallback.rs:226-309` distinguishes fresh recap from resumed
  catch-up and reads tool ledgers. This implementation does the same distinction
  with bounded, filtered source facts and quoted provider observations. It does
  not infer file changes or command success from tool titles, start a summary
  model or report unobserved filesystem effects as completed.
- `host/src/agent/fallback/carry/ledger.rs:58-90` summarizes file/command/server
  reports. Comprehensive native file/command/server effect verification is not
  provided by this increment.
- `host/src/chat/fallbacks.rs:1-56` implements self-managed fallback settings.
  This increment does not give Bots permission to edit their own fallback chain;
  the existing explicit settings and authority checks remain the mapping owner.
- `apps/desktop/src/renderer/views/thread/HandoffNotice.tsx:24-42` expands an
  Agent switch notice to show what was carried. Console parity needs the wire/UI
  integration listed in the task handoff, including a deliberate switch-back
  action if that action is exposed. No switch-back HTTP command is added here.

`TestFallbackLedgerSQLiteParksAndCatchesUpAcrossReopen` exercises scripted
A/B/A/B/A, reopening real SQLite between B and A, exact native IDs, increasing
fences and quoted catch-up. `TestFallbackLedgerSQLiteEveryWriteFailureStopsAndSurvivesRestart`
injects receipt failures after aggregate/event writes at start, binding, opened,
sending, sent, answered, cleanup, terminal, activity, held, refusal, refused
cleanup and alternate-start boundaries. It reopens SQLite and verifies that no
input is resent. `TestFallbackLedgerSQLiteCrashSnapshotDoesNotSendAgain` reopens
a real SQLite `VACUUM INTO` snapshot captured while a tool-active turn was still
running; recovery preserves its unknown effects and the older queued-message
barrier. Additional tests exercise journal idempotency, late no-send claims,
forgets, instruction changes and foreign/unsafe parking candidates. These are
scripted-provider results, not installed native qualification or Cloud/UI
acceptance. All external provider/model/API request counts for these tests are
zero.
