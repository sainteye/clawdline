# Persistent assistant runtime and ACP transport

This is an additive module, default off and not wired to the daemon by this
change. Module tests are not final five-card acceptance. No native provider,
paid model, installation, deployment or release is authorized by this module.

## Constructors and ownership

`projectassistant.NewRuntime(RuntimeOptions) (*Engine, error)` implements the
frozen `Runtime` port. Options require `Repository`, `Memory`, `WorkBridge`,
`ProjectResolver`, `Provider`, `Clock func() time.Time`, `ID func() string`,
`Authorize func(context.Context, pa.BotKey) (pa.FeatureState, pa.SourceStatus,
error)` and explicit `RuntimeLimits`. Construction performs no process or model
work. The host supplies collision-resistant IDs and current local authorization.
`Authorize` must resolve the source machine's current grant and opt-in state;
client-provided source status alone is insufficient. The host persists opt-out
before calling `Disable` and creates a fresh runtime if it enables that Project
again. Disable's local safety latch deliberately cannot be cleared by a send.

The only store synchronization boundary is `Repository.Commit`'s atomic CAS.
Every runtime mutation supplies the current Bot version, an incremented Bot,
facts, receipt and matching event payloads. The store allocates durable event
sequences. The runtime allocates message sequences and updates the conversation
in the same mutation. The store and memory implementation must use their shared
per-Bot exclusion rule; no SQLite transaction may span external IO.

`Engine.Recover(ctx, key)` is an explicit startup operation. Interrupted running
or waiting turns become unknown; recovery does not resend them. The host must
enumerate persisted Bots on startup and call recovery, including after an
acceptance committed immediately before a daemon crash. A transient store error
halts the worker instead of silently losing its durable queue; the host may
retry recovery after resolving the store failure, never a possibly sent prompt.

`Engine.Publish(ctx, ProtocolEvent)` is the owned chat-tool seam. The host must
authenticate the tool's server-held fence before exposing it through MCP/HTTP.
`Engine.DispatchWork(ctx, pa.WorkRequest)` verifies the actor, current turn,
source and fence, records an unknown receipt before external IO, then calls the
existing WorkBridge. It never launches an unclaimed repo writer. A repeated
dispatch returns an inspectable unknown result instead of repeating broker IO.
The broker's own claims/arbitration rules remain authoritative.

`projectassistantacp.NewProvider(ProviderOptions) (*Provider, error)` requires
explicit ACP limits and accepts trusted `Configurations map[pa.Agent]Configuration`.
Configuration contains an absolute executable, argument vector and exact
environment. There is no inherited environment by default, shell command,
registry resolver, npx, download, provider fallback or login change.
`Verify(ctx, bot, config)` is a trusted local evidence check: it must return the
exact CLI/adapter versions and native memory isolation for this Bot/Project.
Read-only enforcement is reported, not required: since 2026-11-14 native tools
act after the person's per-action approval, as in CodyNC. It must not call a model. The host owns
those probes and evidence lifetimes. A configuration without Verify is refused.

`StartProcess(ctx, ProcessOptions)` and `NewClient(process, limits, fence, sink,
readOnlyPermission)` expose the lower-level transport for isolated fixtures.
They do not establish native read-only or memory isolation by themselves.

## Queue, snapshots and fences

Submit checks current source access, content, capabilities and queue capacity,
then atomically persists a user message, queued turn and accepted receipt before
returning. First send mints the initial binding; metadata reads never launch an
engine. Each active Bot has one worker and one turn at a time. Process slots are
global to the Engine, so other Bots can run up to the configured concurrency.
Waiting for a slot is cancellable and already durable. Idle actors are reclaimed.

Admission captures ownership, not instructions. Queued-to-running is the single
allowed rebind: immutable key/Project/conversation/message/idempotency identity
stays fixed while the latest instructions and memory revision are captured and
a fresh process generation/binding is minted. The admission user message retains
its original provenance fence; new assistant events use the running fence.
All incoming messages, permissions and work are checked against authoritative
current source, Project, binding, generation and active turn. Stale events fail.

Opaque resume requires advertised load support and unchanged forget and
instruction revisions. Since 2026-11-14, as in Codync (host/src/chat/memory
change_notice), memory that only gained or changed entries keeps the native
session: the next prompt starts with a `<memory_update>` carrying all of the
filtered memory as it is now. A Bot changes its own memory through the
read_memory, search_memory and edit_memory tools of its owned MCP endpoint,
only during its current turn; a delete forgets the entry. Forgetting, changed
instructions or unsupported resume starts a new binding with the current
provenance-complete filtered context.
Displayed chat history is never automatically sent back to the model. The
runtime's `Memory.WithCurrent` callback contains context validation and one
bounded Send write, not the model wait or session setup. A stale context aborts
without retrying that callback.

An explicitly selected quote is resolved from one repository Export snapshot,
including its exact source turn, before session setup and outside WithCurrent.
It must belong to the same Bot/Project/conversation. Missing provenance, a
tombstoned message/source ID, and an old or unknown source revision after a
forget fence are refused. Quote text uses MaxMessageBytes and is never silently
truncated. PromptRequest.Quote carries a separate PromptQuote block; persisted
user Text is unchanged. The existing memory revision fence then protects the
final write against forgetting between preparation and send. This explicit
reference does not authorize replaying an unknown prompt.

## ACP and visible replies

The client implements initialize, session/new, advertised session/load,
session/prompt and session/cancel using newline-delimited JSON-RPC 2.0. Protocol
version 1 is required. A bounded reader and a separate ordered incoming consumer
remain active while a prompt caller waits. Permission RPCs are persisted without
waiting for the prompt; answers write directly through their own pending native
request ID. No persistent allow/reject policy is accepted. Allow-once also needs
the trusted `ReadOnlyPermission` classifier for the actual tool payload; labels
and native option names alone confer no authority. AutoRead selects only a
verified read-only allow-once option through the same durable Answer path.

ACP `session/update` agent message chunks become trace and bounded partial text.
The owned `clawdline/send_message` RPC supports explicit visible replies in the
scripted transport. Real provider integration must install an authenticated
owned MCP chat server using Engine.Publish. The bounded transport and host
composition are described in [Owned Bot tools over MCP](project-assistants-owned-mcp.md);
arbitrary native adapters are not assumed to implement the scripted extension. Unknown filesystem,
terminal and MCP client requests are rejected. Native in-process tools still
require a separately verified sandbox at admission.

Incoming frames and prompt completion share one ordered consumer: explicit
messages persist before completion can choose final-text fallback. Stable
per-turn message IDs deduplicate repeated explicit calls. Explicit messages
suppress fallback; otherwise final text or partial agent text is promoted once.
No-text completion records a completed no-visible-reply trace. Cancellation and
unknown prompt effects do not promote partial text as a successful reply.

An external write can succeed before a receipt is finalized. The runtime records
unknown before native permission responses and broker dispatches, so a crash
does not authorize repeating them. Initial send receipts remain acceptance
receipts; current Turn/Permission/Work facts establish subsequent status.
Turn termination atomically finishes its remaining pending permission offers
with the turn, active ID, receipt and typed events: unknown turns retain unknown
permission effects; known termination cancels offers or expires already elapsed
ones. Resolved/unknown offers are preserved. This is a durable state transition,
not an additional native answer, and removes stale pending counts after stop.

## Bounds and overload behavior

There are no operational silent defaults. PA-05 owns the capacity register,
measurement wiring, translated notices and its selected integration values.

| Required field | Integration candidate | At the bound |
| --- | ---: | --- |
| RuntimeLimits.MaxBots | 64 | Refuse a new active actor before durable acceptance; idle actors are reclaimed. This is not the saved Bot count. |
| RuntimeLimits.MaxProcesses | 4 | Keep accepted turns durably queued; wait cancellably. |
| RuntimeLimits.PromptTimeout | 10 minutes | Stop waiting; retain unknown external effect, never replay. |
| RuntimeLimits.CleanupTimeout | 5 seconds | Bound cleanup; surface unknown if ownership/termination cannot be established. |
| Limits.RPCPending | 32 | Refuse additional outgoing RPCs with typed capacity. |
| Limits.IncomingPermissions | 8 | Fail the transport on overload; retain unknown effect. |
| Native ACP notifications | No fixed local count ceiling | Ordered delivery follows Codync; load replay and prompt completion retain ordering barriers. |
| Limits.FrameBytes | 1 MiB | Owned MCP HTTP inputs and adapter metadata only; native ACP input/output lines have no local byte ceiling. |
| Limits.CaptureBytes | 1 MiB | Existing process-identity census budget only; native reply capture has no local byte ceiling. |
| Limits.StderrBytes | 64 KiB | Retain the first bounded bytes; continue draining without unbounded memory. |
| Limits.OwnedDescendants | 256 | Stop and surface typed capacity/unknown if the owned process census exceeds its budget. |
| Limits.WriteTimeout | 2 seconds | A lock/deadline/partial-write failure is typed unknown; do not retry the frame. |
| Limits.CleanupTimeout | 5 seconds | Bound native child cleanup and inventory commands. |
| Limits.RPCTimeout | 120 seconds | Expire control RPC waits and native permission offers; unknown external effects remain inspectable. A prompt response instead waits on the runtime turn context, whose existing `RuntimeLimits.PromptTimeout` is 10 minutes. Stop, shutdown and revocation still cancel that context without replay. |
| Limits.DescendantPoll | 50 milliseconds | Periodically capture descendant ownership; it is not a guarantee against unobserved fast reparenting. |

Pending turns and person-visible message size use the existing domain limits
(`MaxPendingTurns`, `MaxMessageBytes`), not a second local definition. Durations
are standard `time.Duration`. Prompt completion uses the runtime turn context
directly, so the shorter control RPC deadline cannot end a still-running turn.
`Engine.Usage` and `Client.Usage` expose counts for host measurements without
contents. A busy actor returns `Known=false` instead of delaying diagnostics.
Process census output alone uses CaptureBytes (registered as process_census_bytes). Native ACP text and notification handling do not use that budget.

## Process containment and honest limits

Unix launches a new process group, closes stdin for graceful exit, surveys only
PID/parent/group/state/start time, and retains exact observed descendant identities across
setsid and parent exit. Cleanup signals only matching identities and the owned
group while the root identity still matches. If process inventory is denied,
the direct `os.Process` handle is still stopped/reaped, but descendant cleanup
returns `execution_effect_unknown`. No process is stopped by name or pattern.
Initial ownership observation is synchronous before StartProcess returns and
before Wait can reap a fast-exiting child. Even an already exited child retains
its unreaped identity during that observation. A missing initial receipt refuses
startup, kills/reaps the direct child and preserves unknown descendant effects.
Signal failures use ProcessSignalFailure with a stable domain code, phase and
original errno. Only a fresh complete census proving no live group member can
classify a denied group signal as already stopped; live or unknown state remains
ExecutionEffectUnknown. Fixture cleanup also verifies its captured birth
fingerprint before signaling a PID that might since have been reused.
One remaining cleanup deadline covers the survey join, every census, graceful
wait and final reaping wait. Closing cancels an in-flight survey before replacing
it with that deadline. A completed reaping receipt wins over a deadline that is
also ready; an unproved/live process or unknown inventory remains unknown.
The self test-peer uses GORACE atexit_sleep_ms=0 to remove the race runtime's
test-only exit delay; detection and its failing exit code remain enabled.

A polling census cannot prove absence of a child that forks, escapes and reparents
between samples. Native admission therefore still needs a trusted supervisor
or separately proved containment boundary. The fixture proves cleanup only for
observed owned descendants; it does not turn polling into a security boundary.
Windows and unsupported platforms explicitly refuse startup until an audited
supervisor can establish containment before execution. No taskkill fallback is
presented as equivalent evidence.

The supplied provider capability research found no installed native ACP adapter.
This paragraph records the state before 2026-11-14. Claude native resume has
since passed a real cross-process proof on the supplied adapter with Claude Code 2.1.323; read-only
enforcement is no longer pursued for the first version, which follows CodyNC.
OAuth graceful exit and real model cancellation remain unproved. Those modes
must stay unavailable. Scripted replies and subscription login observations are
not native capability evidence. This work makes zero usage-priced API requests.

## Upstream adaptation and verification

Pinned Codync revision: `234219cef2d3f991f0f7225ba89e3c99cdd34177`.
Preserve `third_party/codync/LICENSE`, `NOTICE` and `DERIVATION.md`.

| Upstream source | Adapted behavior | Deliberate Clawdline difference |
| --- | --- | --- |
| host/src/agent/acp.rs | Separate pending RPCs and incoming calls/notifications | Bounded queues, typed overload, ordered completion, trusted executable only. |
| host/src/agent/bot/queue.rs | Serial next-turn processing and stop | Durable acceptance, unknown effects never automatically resumed/replayed. |
| host/src/agent/bot/session.rs | Native session separated from displayed conversation | Every process generation is fenced; unsafe opaque history uses filtered new binding. |
| host/src/agent/bot/turn.rs | Immutable turn snapshots and final reply handling | Source/Project grant checks and memory revision fence before bounded stdin write. |
| host/src/chat/outbox.rs | Explicit send_message primary reply and final-text fallback | Durable per-turn deduplication; Cloud document delivery belongs to the host's owned tools. |

Behavior tests cover lazy start, durable failure before acceptance, serial queue,
queue/actor capacity, instruction/memory snapshots, permission interleaving,
persistent-allow denial, explicit/fallback deduplication, cancellation, stale
generation and source revocation, restart without replay, stalled/partial stdin,
RPC timeout/exit/oversized output, stderr cap, and observed setsid descendants.
The test executable is the free scripted peer. `testdata/peer.go` is an additional
standalone host rehearsal peer and never calls a model.

### Interrupted turns explain their outcome

The runtime retains allowlisted interruption categories instead of reducing every
failure to the field `execution`: a turn deadline, cancellation, transport exit,
response/step limits and normalized provider refusals. The ACP pending reply
preserves its context error alongside `execution_effect_unknown`, so a timeout
remains distinguishable from cancellation. A planned restart keeps its existing
recovery marker. These categories are diagnostics, never no-effect evidence.

The bounded SQLite read view and HTTP/Cloud snapshot carry an optional
`failure_field`. Its SQL projection admits only known categories; arbitrary or
large stored failure narratives stay outside mandatory state. Live turn events
retain the same field when reduced to a summary. Older sources without it still
show the generic uncertainty explanation.

Chat presents the cause in English and Taiwan Traditional Chinese, followed by
instructions to inspect results and send follow-up instructions when effects are
unknown. It does not resubmit the original prompt or clear unknown receipts.
This follows the visible failure-notice behavior in
[Codync's turn handler at 9b471897](https://github.com/leepokai/Codync/blob/9b47189739216b405416b334b9ada5b5b28176c1/host/src/agent/bot/turn.rs).
Clawdline retains its own effect-state and cleanup barriers; this change adds no
automatic health-check hook or recovery retry. Persisted older events that already
lost their cause cannot establish why a particular turn stopped.

Run the two package tests and race tests, then the repository commit gate.
When sandbox policy refuses `/bin/ps`, transport/setsid tests skip before
launching their fixtures, and a separate admission test asserts typed unknown
startup refusal and direct-child reaping. Normal-environment descendant
acceptance remains explicitly pending until the host runs those tests.
Cross-compile the adapter tests for Windows without claiming Windows execution.
Quote cases cover actual scripted-peer stdin, a separate unchanged user message,
missing/foreign message or source turn, tombstones, old/zero/unknown provenance
after forgetting, oversized content and unknown-effect idempotency.
PA-05 must repeat key cases with PA-03's real SQLite adapter and then perform
cross-module and actual UI acceptance on its pinned integrated candidate.


A fresh post-grace census that leaves no known owned identity does not require
another census for each signal: no PID or process group remains authorized for
such a signal. Close skips those redundant signal inventories and still waits
for the direct child's reap receipt within the same original cleanup deadline.
An earlier or fresh inventory failure remains typed execution-effect-unknown;
an empty ownership map does not erase that failure. Observed escaped descendants
keep the original birth-identity checks and TERM/KILL path. A controlled reaped
child fixture makes any third census exhaust the existing deadline, proving the
old false-unknown result and the corrected two-observation result. Separate
unknown-preservation, original transport, and real observed-setsid cleanup
tests cover the unchanged failure and ownership boundaries. This does not admit
a native provider or establish cleanup of an unobserved escaped descendant.

## Fresh native sessions receive a source-owned handoff

A turn that cannot safely resume an opaque native session now constructs its
opening inside the final `Memory.WithCurrent` boundary. `SessionHandoff` binds
the Bot, Project, conversation, newly minted binding/generation, Agent and exact
turn instruction/role snapshot to that filtered memory revision. It also carries
read-only unfinished turn/work references and a stable message bookmark. It
copies no implicit transcript into the prompt and does not retry an unknown
intent. Old-generation persisted work remains descriptive conversation data.

`PromptRequest.Handoff` is additive to the internal provider port. The ACP
client binds it to the exact prompt fence/instructions and freshly checked
context, then sends that opening and the user's new input through the existing
`session/prompt` path. A safely resumed session retains its existing prompt path.
The existing frame/capture limits still refuse oversized data without silently
truncating it; this change introduces no additional bound or public operation.

The source is pinned Codync's `host/src/chat/handoff.rs` and
`host/src/agent/bot/compaction.rs`: summaries, open work and a history bookmark
cross a fresh-session boundary. Automatic compaction/rollover, a new editable
handoff card and transcript excerpt copying are outside this first-version data
unit. Filtered SQLite memory, rather than opaque provider context, is the source
for the new opening after forgetting. This does not establish real Claude/Codex
adapter admission or claim that a native provider erased its external memory.

## A reviewed handoff can open a fresh Bot conversation

The Bot information panel offers **Prepare a fresh conversation** after a
native conversation exists and the source is idle. The card starts from the
same filtered history selection used by the fresh native opening. People can
review that text, save an edit, discard an unsaved edit, jump to original
history, or keep the current conversation. A preparation, edit, or Keep has no
native effect. Keep leaves the existing binding eligible for ordinary resume.

The source persists a draft and its edit receipts in source-local SQLite
tables. Those tables stay outside the portable v12 Bot archive and its
canonical digest. The draft belongs to the exact Bot, Project, conversation,
binding generation, memory revision and instruction revision. The HTTP and
Cloud Pass routes first appear at API 93. Each human send must name the
displayed draft revision while a current draft exists. Input admission,
the user message, its receipt and adoption commit together. A stale revision
fails without admitting an input. Source receipts let a client inspect an
uncertain response without starting another turn.

The admitted turn opens the existing fresh-session path. At its final memory
and authorization boundary, the source claims the summary once and clears the
stored text. The Agent receives it as quoted history alongside the actual new
input. Forget withdraws an unclaimed draft and clears its text in the memory
transaction. An adopted input with no current grant fails closed. After a
restart a saved draft remains available. An adopted turn that has lost its
in-process grant is never reauthorized from its receipt, and an unknown
handoff turn keeps the native outcome unknown.

The source regression uses real SQLite, the normal HTTP handler and the
verified Cloud Pass seam with a scripted Provider. The browser regression
uses the real Bot chat with desktop and mobile Chromium viewports. With web
dependencies prepared and existing Playwright and Chromium installed, run:

```sh
go test ./internal/adapters/projectassistantstore -run '^TestHandoffCard'
go test ./internal/transport/http -run '^TestHandoffCardSource'
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
PLAYWRIGHT_BROWSERS_PATH=/path/to/browser-cache \
CLAWDLINE_HANDOFF_CARD_ARTIFACTS=/path/to/evidence \
go test ./internal/transport/http -run '^TestHandoffCardBrowser$' -count=1
```

## Resuming after a restart

Since 2026-11-14, as in Codync (host/src/agent/bot/mod.rs RESUME_PROMPT,
STALE_RESUME_MS), a turn that a daemon restart cut off is still recorded as
unknown, and when its message is less than an hour old and nothing else is
queued, the runtime sends the agent Codync's resume prompt as a notice
message. The notice acknowledges the unknown turn (its receipt stays
unknown) and runs in the same native session when it can be loaded; the
prompt tells the agent to finish without repeating a completed action. An
older interrupted turn, or one with queued messages behind it, waits for the
person's next message as before.

## Darwin lifecycle preservation acceptance

[CI run 43752755054](https://github.com/sainteye/clawdline/actions/runs/43752755054) failed `TestHostLazySendDisablePreservesUnknownAndExistingHostWork` while the real SQLite cleanup used the fixture's 100 ms deadline. The functional case now uses the registered 5,000 ms production cleanup budget. Its checks still require the interrupted turn to remain unknown, the queued turn to become cancelled, and the existing host work to survive. Dedicated short-deadline and failed-cleanup cases retain their original limits and verify that unresolved resources prevent re-enable. The focused preservation, late-open and failure-injection cases passed locally; the exact repaired main still requires CI acceptance.


## Remaining Codync runtime construction

This is a source audit of Clawdline `8938346924d7c4a057a20c5c64953d988d500d57`
against the pinned Codync `9f67eb09084db61b3cca9c84a11bc03df9de29b7`.
The table tracks subsequent source construction as well as that original
baseline; the API 92 server and API 93 handoff rows describe the current
candidate, not the older 893 build. It is not a production UI or
installed-binary acceptance receipt.

Templates (`project-assistants-templates.md`), message reactions, pinned/Project
roster sections and mark-all-read already have implementations. They must not
be rewritten or described as wholly missing because their Board step also
contains other features. Current source boundaries are:

| Behavior | Existing source | Remaining construction and verification |
| --- | --- | --- |
| Editable handoff card | API 93 stores an editable source-owned draft, Keep action and atomic adoption by the next input. SQLite and synthetic HTTP/browser checks cover stale edits, lost replies and API 90–92 fallback. Pending draft reads block calls; only an explicit pre-admission handoff refusal releases the call journal. | Installed and production Cloud acceptance remain separate. Original history stays readable. A fresh handoff can retain the physical history directory, so its read-only maintenance gate must still run before deciding whether to resume. |
| Continuous Bot call | `BotCall.tsx` binds call IDs, input and replies to the chosen source/Bot/conversation. Synthetic Chromium checks cover playback, mute/interruption, explicit end, permission waits, source changes and unknown results without replay. Existing draft dictation remains available. | Installed and production Cloud acceptance remain pending. Synthetic audio does not qualify microphone recognition or voice quality. On-device recognition requires an installed language pack, and playback requires a local voice; there is no automatic remote speech fallback. |
| Bot-owned dev server | The API 92 construction below composes the source-owned registry, exact command approval, native start/list/stop/log tools and known-handle cleanup. Captured Cloud routing adds two reads and four known-handle controls. Clean Host restarts consume exact terminal checkpoints under current standing approval. The existing Project devstack route stays read/probe only. | Paired viewer/workerd/source acceptance and physical crash/boot recovery remain pending. Selected-chat observation and title refresh are implemented below; installed and Cloud acceptance remain separate. Current evidence uses local synthetic processes and real SQLite/MCP, not an installed vendor, Cloud UI or updater qualification. |
| Durable native maintenance | Isolated qualification owns intent/terminal/unknown receipts. Read-only detached-history gates survive disable/enable and protect pending or unknown histories before fresh handoff and resume. | Production provider, verified idle admission, retention and trusted rollback qualification remain separate construction. Do not infer completion from lower token usage or send maintenance as ordinary chat. The production writer stays disabled; reader/recovery qualification must precede any writer release. |

The relevant pinned upstream sources are `host/src/chat/handoff.rs`,
`docs/features/voice-call.md`, `host/tests/calls_e2e.rs` and
`docs/features/dev-servers.md`. Their data ownership and observable behavior
are the comparison, rather than their constants, platform assumptions or exact
UI implementation. In particular, a dev server is independent of an Agent turn
but still runs code with the source machine's authority; a port probe does not
establish an owned process or a safe command-start operation. The retired Swift
DevStack commands are not current Go capabilities.

Construction and destination acceptance remain separate. A completed source
unit does not complete a grouped step while the other behaviors in that group
are absent. The archive reader's independent qualification and remaining recovery
gaps are recorded in [memory and settings](project-assistants-memory-settings.md#independent-archive-binary-qualification).
No new API level, archive generation or maintenance writer is enabled by this
audit.


### Source-owned development servers (API 92 construction)

One Host owns a source-local server registry independently of its Engine and
native executions. Enabling the already opt-in Bot feature explicitly installs
the local catalog and opens that registry; metadata inspection starts nothing.
The source launcher snapshots only basic source environment values, excludes
provider/profile credentials and runtime startup hooks, and resolves an existing
canonical starting directory under the current full Project. This directory
check is not a shell/filesystem/network sandbox. Approved commands run with the
source user's files and network authority, as the pinned upstream specification
describes. Windows source launch remains unavailable.

The existing owned native MCP endpoint advertises `start_dev_server`,
`list_dev_servers`, `stop_dev_server` and `dev_server_logs` only to a current
source-bound execution with the service available. Start accepts command,
optional name/folder and an explicit idempotency key. The other tools use known
IDs; no tool accepts Bot/Project identity, environment, PID or approval. Host
source/canonical Project authority is checked before effects and again before
content is returned. The actual MCP body write and flush also cross the Host's
current memory/publication fence, under the existing write deadline; waiting for
human review or peer consumption never holds that lock. Source start reviews
use the actual native execution deadline rather than the shorter ordinary RPC
budget. A refused last publication returns only a typed code, and a failed or
partial transport write is never followed by another response frame.
Mutation keys cache the exact argument hash, no-op and
uncertain results for that execution; changing a payload conflicts. A cached
old stop cannot stop a later restarted attempt. Accepted is distinct from
running. Valid list/log calls also record source tool activity.

Each definition binds Bot, full Project, command, directory and the source
Forget boundary. A label does not change command authority or start another
copy. Ordinary memory saves do not invalidate resources. Model listings and
logs exclude definitions from a different Forget epoch, and publication checks
run under the existing memory fence. Historical definitions and exact rules
remain visible/revocable to the person through the human rules API. The
console's Codex source-rule entry requires API 92; Claude's separate native
Project rules retain their existing scope. Matching Cloud transport qualification
remains separate from this source construction.

The source API also exposes typed human list/log reads and start/restart/stop/remove
controls for existing IDs under `/v1/project-assistants/{bot}/dev-servers`.
Controls carry the displayed fingerprint, attempt and revision; they accept no
command, directory, environment or PID. The source checks the same paired human
principal and current write authority at admission, including when an exact
standing rule exists. Read-only devices may read history but cannot mutate it.
Human history remains readable after native completion or Host disable; this
does not expose forgotten history to the model. An old Forget provenance cannot
be silently transferred to a new execution.

The registry compares the complete displayed baseline under its operation lock,
both before review and at final start admission. A completed intervening attempt
invalidates an old control. A start/restart key derives a stable attempt identity
from the trusted principal, machine, Bot, server, action and idempotency key.
Replaying that same attempt cannot launch another process. Restart first stops
the observed attempt and may subsequently fail; it is not an atomic external
stop-and-start transaction. A refusal after the stop reports an unknown overall
outcome and requires inspection. Stale stop/remove controls refuse and require a fresh
read; uncertain responses are never automatically retried with a new key. These
receipts remain source-local and add no portable Bot operations.

Bot details show the server section before the ordinary editor, and only after
a source read finds servers. Each quiet row opens its first eligible source-provided address, or the available
log tail when no address is available. Copy/address choices, logs and process
controls live behind the existing anchored menu, visible on desktop hover
or keyboard focus and directly available on touch. Logs use a native modal,
show executable text as plain text, and retain the incomplete-tail warning.
Source status/permissions still gate every operation; read-only devices retain
logs. A write locks further controls until a fresh source read succeeds. A lost
reply displays an inspection request and never automatically resends the write.
The full source/principal/Bot/Project key remounts this view when its owner changes.
Current chat event frontiers trigger reads, with demand during a slow read
coalesced into another read rather than cancelling the first. Closing details
removes its portalled menu and log sheet. No address is guessed from log text;
background observation and chat-title summaries remain separate construction.

The existing rules/remove client already has Cloud relay and vocabulary entries;
its request flag means content access, not local-only transport. The new server
controls still need their own Cloud carriage, private bridge and UI. Source HTTP
and client tests do not qualify those destinations.

The command handler requests the existing source-owned permission card, with
complete structured scope and all four choices. The human card explicitly
states that source file/network authority is outside the Agent terminal sandbox,
in English and Taiwan Traditional Chinese. A human selection is persisted
before being delivered, never sent as a vendor permission RPC. Always allow
and Never then persist an exact versioned local rule before returning; uncertain
rule storage releases no launch check. Revocation retains its version so a stale
card cannot recreate a grant. It affects future starts; stopping an existing
server is a separate action. The retained Host launch check keeps no native
review callback and does not expire at the end of a native turn. Source policy
revocation and Forget stop known owned processes through Host reconciliation;
they do not automatically restart when policy becomes available again. One old
forgotten definition cannot stop a new current-epoch definition for the same Bot.

Definitions persist before human review, without an accepted attempt or process.
Accepted intent, the current-attempt pointer and source-wide process budget
commit atomically. The attempt ledger compares complete immutable identity and
revision; the stable storage key does not contain mutable Project/approval
payload. Lost acknowledgements remain unknown and are never automatically
replayed. There are at most 64 live definitions, four source-owned attempts,
8 KiB per complete spec, a 64 KiB output tail per run and 32 mutation receipts
per native caller, all in the existing capacity register. Unknown owners retain
occupancy. The HTTP capacity projection reports actual admitted definition and
attempt counts; retained log/caller buffer occupancy remains explicitly unknown.
A restored historical output tail is marked incomplete rather than complete
and empty.

The catalog borrows the Host's existing SQLite writer. Installation is explicit;
inspection refuses an old/unknown format without writing. Known local catalog
version 1 upgrades atomically to 2, adding one optional rule per definition;
old definitions retain epoch zero and no approval. Malformed/partial schemas,
dangling current pointers and unowned live receipts are refused without repair
or downstamping. Removing a terminal definition clears command/rule text and
retains an ID-only tombstone, so late controls cannot address a reused ID.
These local tables change neither portable Bot core/archive 12 nor SQLite's
host user_version. The maintenance writer stays closed.

The command adapter now has an optional read-only TCP observer. It pins the
root before the direct child can be reaped: Darwin uses kernel birth plus the
exec-stable unique process ID; Linux retains a pidfd and kernel start ticks.
Each observation brackets the listener read with fresh, complete rooted parent
and kernel-identity snapshots. A reused PID, changed ancestry, missing root,
incomplete inventory or unsupported platform returns an unknown/unavailable
reading, never guessed addresses. Darwin queries lsof with an explicit PID
intersection and TCP LISTEN selector. Linux joins only the owned processes'
socket descriptors to TCP tables in the source host's network namespace, then
rechecks matching descriptors. Other network namespaces remain unsupported. No command text,
file contents, argv or environment is returned.

OS probes share the existing census byte, descendant and cleanup-time budgets.
At most four source TCP observations run at once across the process; a busy read
refuses without queuing another probe, and the existing capacity override may
only lower this admission limit. The capacity view reports the actual active
count. The new bound's unregistered guard was exercised red before registration.
A controlled PID-only comparison disclosed a replaced child's bind; the full
identity comparison refuses it. Actual Darwin Go HTTP peers cover exclusion of
an unrelated listener, exec without losing ownership and port closure on cleanup.
Linux implementation and tests still require their platform CI; a Darwin test
is not Linux runtime evidence.

The later source-response and selected-chat sections below carry this optional
observer through server lists and title/details. Cloud carriage and a Host
background change stream remain separate. Ordinary ACP peers
retain no observer. It does not replace the existing cleanup supervisor, persist
kernel facts, adopt an escaped/crashed owner or authorize kill/restart by PID.
Physical crash/boot recovery remains separate construction. Clean-restart
checkpoints are described below; portable Bot/archive 12 and the maintenance
writer are unchanged.

The registry owns foreground process handles through the existing ACP physical
supervisor, without creating another native provider. It drains combined output
into the bounded tail and retains an acquired handle even when launch returns
late or fails. Direct-child exit is not descendant cleanup: the exact existing
process owner still closes its verified tree. Native turn completion, cancellation
or replacement cannot close that separate Host owner. Host disable/close stops
known resources; Bot deletion stops them and removes their executable definitions.
Unknown cleanup is retained and blocks Host replacement. Reopened active receipts
are unknown observations, never proof of a live/gone process or permission to
adopt, kill by bare PID or run a command again.

Tests use real temporary SQLite, a synthetic Go ACP subprocess following its
actual owned HTTP MCP descriptor, stored human approval cards and controlled
process handles. They cover all four tools, duplicate starts, payload conflicts,
old stop after restart, Forget filtering, deletion, revocation and unknown cleanup.
A separately launched real Go HTTP peer survives native turn completion and its
port closes on Host disable. A controlled whole-Bot reconciliation counterexample
stops a fresh context's server; the per-definition fix refuses that regression.
These are local synthetic checks with zero vendor/model requests. They do not
qualify an installed vendor, production Cloud/UI, physical recovery after Host
crash, machine reboot or updater rollback. The details component has isolated
desktop/mobile browser evidence, not installed or production Cloud acceptance.
The later port-response and selected-chat sections implement owned port reads.
The captured Cloud mapping below completes the public operation vocabulary.

### Clean Host restart checkpoints

A planned Host close stops each owned tree before saving its restart checkpoint.
Only an accepted/running attempt with a successful cleanup and a known stopped
receipt qualifies. A natural exit, explicit stop, feature disable, failure or
unknown cleanup does not. Repeating close cannot renew an intent. Opening the
registry remains inspection only and never adopts a process.

The optional source-local `bot_devserver_restarts` table retains one checkpoint
per bounded definition: the OS boot identity's SHA-256, exact current attempt
and terminal revision. Installation is explicit and rejects unknown table shapes.
It neither changes Bot/archive 12 nor grants authority. Existing catalog writers
can ignore this table; an intervening attempt/revision invalidates the checkpoint
before consumption. This source compatibility property is not a new historical
binary or updater qualification.

Enable preserves the native-history barrier and establishes current background
authority before consuming checkpoints in one transaction. A failed or uncertain
consume returns no executable work. A different or unreadable OS boot identity
discards the intent and leaves the server stopped. No deferred retry waits for a
later grant. Matching boot identity alone is insufficient: the source, Bot,
canonical Project, Forget boundary and current exact permanent rule are checked
again, including immediately before launch. AllowOnce does not become a standing
grant, and restoration never creates a new approval card or Agent provider.
An admitted restoration has a new attempt identity and the usual source-owned
process/cleanup journal; response uncertainty does not replay the old attempt.

Temporary SQLite reopen and Host fixtures cover clean restart, single consumption,
intervening writers, failed transactions, denied/one-time approval, revocation,
Project replacement, Forget, stopped/exited work and unknown cleanup. A Darwin/
Linux test also runs a real source-owned shell command, proves cleanup through the
existing supervisor, restarts the same definition and cleans the second tree.
Boot-change tests inject the OS identity; they do not reboot the machine. This
does not yet persist kernel birth facts or clean up survivors after an unplanned
Host crash. Such active ownerless receipts remain unknown and reserve capacity.

### Captured Cloud server controls

`assistantRelayRequest` maps the existing API 92 routes to
`project-assistants.dev-servers`, `dev-server-logs`, and the four
`dev-server-start`, `dev-server-restart`, `dev-server-stop` and
`dev-server-remove` actions. Reads retain the captured machine/Bot key and
known server ID. Actions retain the complete key, server ID, fingerprint,
attempt, receipt revision and stable idempotency key. A closed body rejects
new commands, environment variables, PIDs or client-supplied approval.

The SourceClient still requires API 92 or later, the source's explicit fresh
support signal, and current paired read/write authority. The existing encrypted
connection checks its advertised words and preserves the write reference.
An authenticated person's explicit known-handle start is the existing source
Allow-once path; it cannot override a saved source deny. Source CAS, Forget
and revocation checks remain in the Host. An uncertain transport response
does not trigger another control call.

The public Bridge/Router and real paired HTTP/Host/SQLite tests cover readable
metadata, read-only write refusal, stale CAS, saved deny and revocation without
opening a native provider. They do not exercise a production Relay. A paired
browser through real workerd to the source, including Forget and lost replies,
remains a distinct acceptance step. The private Relay's generic encrypted
carriage does not by itself establish that six-operation acceptance.

Source list/start/stop responses now carry `ports_known` and `ports`, bound to
that exact definition, attempt and receipt revision. The optional process
observer reads current owned TCP listeners; a second Owner/Registry check
rejects a result if stop, restart, removal or owner replacement occurred during
the OS read. No registry or memory lock spans that read. Known stopped/failed
attempts report an empty set; unsupported, lost, denied or changing ownership
remains unknown. These observations are point-in-time reads, not durable
process ownership or restart authority.

Each source-derived port includes its literal bind, a local HTTP candidate and
remote HTTP candidates. Loopback has no remote candidate. Wildcard binds use
current source interface addresses of the same address family, excluding
loopback and link-local addresses; clients supply neither hosts nor URLs. IPv6
literals use brackets. These are TCP observations, not HTTP probes or a promise
of another device's reachability. Remote clients must not open the local-only
candidate. Source responses retain every server identity; if optional listener
details exceed the existing native-frame budget, all port observations become
unknown rather than a partial list being described as complete. The details
view opens and copies only these source candidates. Local transport plus a
loopback console origin is required for a local-only candidate: LAN access can
also use the local transport, so its account key alone does not prove the
browser is on the source machine. Cloud, LAN and unknown origins use only
remote candidates. Unknown write results lock navigation/copy as well as process
changes until a fresh read; logs remain available. Clipboard failures are shown
without claiming a successful copy. The selected-chat observation below shares
these readings with the title; this source change does not establish Cloud
carriage or installed UI acceptance.


### Selected-chat server observation

The selected visible chat now owns one observation shared by its title and
details. It reuses the existing chat read pulse, including a new read-view head
with the same conversation frontier; server changes need not create a chat
message. Opening details explicitly refreshes that same owner rather than
creating a second poller. There is no new timer, global observer map or Host
change stream. A quiet title prefers the first running server with a known
port, otherwise its name; `+n` counts the other running servers, not listeners.
Starting, stopping, failed and unknown readings remain distinct. Stale or
unconfirmed readings do not supply a title.

One request slot serializes reads and controls, retaining at most one following
read demand. An abort does not release a non-cooperative request's slot until
its promise settles. Hidden pages pause; resuming requests a new source read.
The owner is bound to the source object, machine, grant revision and full
Bot/Project scope. Scope changes or revocation discard old bodies before they
can enter the new title, list or log dialog. Read failures retain stale rows
rather than imply an empty authoritative set. Unknown controls retain an
inspection lock until an explicit read; neither refresh pulses nor visibility
changes replay a write. Request keys are created only after local admission.

Controlled-promise tests cover coalescing, read/write exclusion, non-cooperative
abort, unknown outcomes, read-only authority and late results after revocation
or closure. Removing the post-read authority check produced a failing private-row
disclosure counterexample; restoring it passes. Isolated desktop English and
mobile Taiwan Traditional Chinese browsers exercise the actual Chat and details
components against a synthetic source: details-closed changes update the title
while the chat frontier remains unchanged, a held read does not fan out under
chat refresh, switching Bots excludes its late body, and revocation hides the
server rows and title. The chat read-view is scripted while server reads use
the fixture HTTP transport. These checks use no formal daemon, provider/model
request or private conversation; they do not qualify a full installed App,
production Cloud transport, server reachability or crash/reboot recovery.


The server candidate's minimum API level is 92 after coordinating publication
order with the group-reaction and message-board owners. API 90 supplies group
reactions and published API 91 supplies message-board replies. The console must
not infer source server tools or exact source-command rules from either level;
all server reads, log reads and controls require 92 before transport. The
candidate integrates both lower-level implementations from main and regenerates
the shared contract outputs. A private higher-level build alone does not
establish that its features have been deployed.


A published API 91 source can provide message-board replies without server
tools. Server support therefore also requires an explicit optional
`dev_servers_supported` source-status signal, fresh readable authority and API
92 or newer. A missing or false signal never enables a server read or control,
even on a higher API level. Read-only readers may inspect existing servers;
controls still require the current paired-person write grant.
