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
It distinguishes existing components from the product behavior they do not yet
provide. It is not a production UI or installed-binary acceptance receipt.

Templates (`project-assistants-templates.md`), message reactions, pinned/Project
roster sections and mark-all-read already have implementations. They must not
be rewritten or described as wholly missing because their Board step also
contains other features. Current source boundaries are:

| Behavior | Existing source | Remaining construction and verification |
| --- | --- | --- |
| Editable handoff card | `session_handoff.go` creates a source-owned, filtered opening for a fresh native session. | A persisted editable draft, Keep action and atomic adoption by the next input are missing. Preserve the original history and bind adoption to the Bot, Project, conversation/generation, memory and instruction revisions. Test concurrent edit/Keep/send, Forget, revocation, restart and lost acknowledgement without replaying unknown work. |
| Continuous Bot call | `Composer.tsx` appends recognized speech to the current unsent draft. General Session voice has a separate owner. | Bind call ID, input, replies and history to the chosen Bot/conversation; support speech playback, mute/interruption and explicit end. Test account/Bot changes, disconnect, pending permission and late results after end with synthetic audio before any live call. Dictation is not this lifecycle. |
| Bot-owned dev server | `internal/adapters/devstack` reads declarations and probes ports; the Project devstack HTTP route accepts GET only. | A separately owned command/process tree, approved command and directory, start/list/stop/log operations and durable outcomes are missing. Test turn completion, concurrent start/stop, deleted Bot, revoked source, Host restart and lost acknowledgement. Stop only a verified owned process generation; do not inherit provider credentials or expose arbitrary repository commands through the read route. |
| Durable native maintenance | Live context readings and the optional ACP compaction port retain the native session and execution fence. | Host-owned idle admission and persistent intent/terminal/unknown recovery remain missing. Do not infer completion from lower token usage or send maintenance as ordinary chat. Reader/recovery qualification must precede any new-format writer; no policy control should appear before the operation exists. |

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
