# Persistent Bot work through the existing broker

This module is an opt-in dispatch boundary, not a second task system. A Bot is
not a Session, terminal, human run, schedule or detached automation. Without a
source authority and result sink the typed entry point refuses work. Mounting
HTTP/MCP tools, enabling the feature, native-provider acceptance, Cloud delivery
and release acceptance are separate host integration work.

## Host construction

Construct `orchestrator.NewAssistantAuthority(AssistantAuthorityOptions)` with
`Current func(context.Context, pa.BotKey) (AssistantCurrent, error)` and an
explicit `Clock func() time.Time`. The constructor returns
`(*AssistantAuthority, error)` and rejects a missing dependency.

`Current` must read the source machine's canonical registered Project, Bot,
Conversation, binding, active turn, feature/source access and dispatch policy.
It supplies `Fence`, `Source`, `Feature`, `ActiveTurnID`, `BindingRevoked`,
`Project` (a current `pa.ProjectResolution`), `DispatchAllowed`, `AllowedClaims`,
`Assistant`, `PermissionMode` and `TimeoutMinutes`. None of these may be decoded
from model arguments or copied from a client-provided absolute directory.
`AllowedClaims` is an explicit list of exact relative files authorized by host
policy. Native read approval and model prose do not establish this policy.

Assign the authority to `Broker.AssistantAuthority`. Construct
`projectassistant.NewWorkBridge(WorkBridgeOptions{Repository, Broker, Clock})`,
which returns `(*BrokerWorkBridge, error)`, then connect
`Broker.AssistantWorkChanged = bridge.Sync`. The callback has the exact type
`func(context.Context, orchestrator.AssistantWorkSnapshot) error`.

Only the host calls
`authority.Issue(ctx, fence, operations, expiresAt)` to obtain a
`pa.AssistantExecutionActor`. Use a deadline from the host's registered runtime
turn policy; this module introduces no default TTL. Dispatch actors need
`pa.DispatchWork` and `pa.ReadWork`. A history reader can receive only
`pa.ReadWork`, including when the feature is disabled but readable data remains.
An independently issued pure ReadWork actor can also read/reconcile after the
execution binding is revoked. Source read authorization, current fence, HMAC
and expiry still apply. A mixed dispatch/read credential retains execution
checks and cannot silently become a history-only credential after revocation
or feature disable; the host must issue a separate pure history reader.

The HMAC key exists only in the authority instance. The signed credential binds
the source/Bot/Project/conversation/binding/generation, operations and expiry.
For an actor with dispatch authority it also binds the source's active turn at
issue time, so an old turn's credential cannot authorize the next turn within
the same execution binding. Every operation rechecks the current source facts;
revoked execution bindings, revoked/unreadable sources, stale generations and
disabled writes fail closed under the separate read/execution rules above.
Restart invalidates old credentials. A new current actor can still read a
historical task from the same Bot/Project/conversation.

`CapabilityID` is private credential material. Do not serialize the actor into
prompts, task files, public API responses or logs. `AssistantOwner`, stored as
`Record.assistant_owner`, contains only the historical fence, turn, original
command and request digest. It grants no authority.

## Admission and ownership

`Broker.DispatchAssistant(ctx, pa.WorkRequest)` builds an in-memory draft from
host policy and enters the ordinary `Broker.Dispatch` pipeline. The internal
admission marker is unexported; JSON cannot select this door. Ordinary Session
dispatch still validates its root Session and Squad capability as before.
Replaying a Bot task through an ordinary dispatch request is refused.

The Bot door requires a nonempty exact file list, runs the existing broker
claims validator, and rejects traversal, absolute paths, globs, directory
claims, symlink components and files outside the host's allowed list. It
always selects `isolation: worktree`; the host chooses the canonical directory,
worker provider, permission ceiling and timeout. It retains inventory freshness,
rate admission, per-owner and machine child capacity, quota evidence, terminal
backpressure, durable task creation, worktree effects, ordinary worker launch,
collection and landing proofs. The existing per-owner child ceiling is also
applied per Bot; this adds no queue or new capacity constant.

Source authorization is checked again immediately before durable creation.
Once accepted, revocation prevents new work but does not erase the task or
claim that an already accepted external effect was cancelled. The Bot actor
has no landing, approve, claim, finish, Session-management or arbitrary-machine
operation. Board changes still need the existing human/run authority.

`AssistantTaskID(command)` derives a stable UUID from source machine, Bot,
operation and idempotency key. It excludes the content hash intentionally:
reusing the command key with a different hash must conflict. The broker stores
the original command and `AssistantRequestHash(request)`, which independently
covers the historical fence, turn, claims and brief. Neither digest includes
the credential. Identical commands return one accepted task; changed payloads
conflict even if the caller leaves its claimed content hash unchanged.

## Receipts and recovery

The ownership split is explicit:

| Fact | Owner |
|---|---|
| Original `WorkRequest.Command` receipt, initially unknown | Runtime |
| `bridge-prepare-<task ID>` receipt before the first broker call | WorkBridge |
| Accepted task, isolated checkout intent, spawn, result and landing | Existing broker |
| `bridge-work-...` receipt, WorkLink, work event and one result message | WorkBridge/source Repository |

The original Runtime receipt is never used to reject the first Bridge call and
is never overwritten by the Bridge. The separate preparation receipt prevents
an unknown Bridge invocation without a broker task from being replayed after a
crash. A broker refusal remains inspectable; a new attempt uses a new command
rather than silently retrying an unknown one.

An accepted task can outlive a failed or missing WorkLink transaction.
`bridge.Status(ctx, currentActor, taskID)` re-reads broker facts and reconstructs
the persisted link and any result event. It never creates, respawns, resumes or
lands a task. `Broker.AssistantStatus` is the sanitized read projection before
source persistence. After Runtime returns `pa_outcome_unknown`, the host can
derive the task ID from the authenticated original command and call `Status`;
the command's Runtime receipt resource is a turn ID, not a task ID. Missing task
evidence is not permission to dispatch again.

The initial Runtime implementation also writes a `runtime-work-...` WorkLink
projection after a successful Bridge return. Integration must reconcile that
overlap with the persisted-return contract; it must not make the Bridge
overwrite the original Runtime command receipt.

## Results and reading

Every broker record mutation calls the opt-in result sink after its store
transaction commits. `Sync` re-reads current broker state before projecting, so
a delayed callback cannot overwrite a later landing with an earlier state.
Its source Repository mutation atomically stores the WorkLink, receipt and
events. A terminal result creates exactly one stable `work-result-<task ID>`
message. Later landing updates the link without creating another message or
incrementing unread. Work state changes are not trace messages.

| Durable broker fact | WorkLink state |
|---|---|
| Queued or spawning, not yet signed by the worker | accepted |
| Worker briefing/acceptance established | executing |
| Success | completed |
| Failure, cancelled or spawn failure | failed |
| Timeout or an unrecognized state | unknown |
| Verified landed or incorporated receipt | landed |

Completed does not imply landed. A failed source transaction leaves the broker
facts intact, logs that projection is pending without private error contents,
and can be repaired by `Status`. The host must expose/reconcile that pending
delivery when mounting its transport. There is no automatic model resubmission
or background timer in this bridge.

Historical reads compare the task's source/Bot/Project/conversation, not the
old binding/generation against the new execution. The reading actor itself must
be current, unexpired and authorized. A task-only link has an empty
`BoardItemID`; no new task navigation URL is invented. The host can expose its
sanitized WorkLink/task ID or connect an existing Board ID when one exists.

## Source adaptation

The structural reference is Codync revision
`234219cef2d3f991f0f7225ba89e3c99cdd34177`, specifically
`host/src/chat/outbox.rs` (`call`/`deliver`),
`host/src/agent/bot/cmd.rs` (`Cmd::SendToUser`) and
`host/src/agent/bot/queue.rs` (`on_cmd`). Their host-owned Bot command/result
boundary and explicit visible-message delivery inform this Go boundary.
This implementation uses the Clawdline broker and transactional source facts;
it does not copy Electron IPC, the upstream host, unbounded channels or an
additional task store. No upstream source text was copied into this module.
The foundation's Apache-2.0 license, NOTICE and derivation record under
`third_party/codync/` remain unchanged.

## Verification and removal

`TestAssistantBroker*` crosses the actual broker entry using empty temporary
SQLite/task directories and real temporary Git repositories/worktrees, with
worker launch replaced before any provider/model call. It covers independent
ownership, durable-before-launch, accepted/executing/completed/landed states,
credential privacy, forged/expired/revoked/cross-scope actors, stale turns and
generations, exact claims, symlinks, per-Bot/machine capacity, terminal
backpressure, late revocation, restart recovery and ordinary Session/Squad
admission. `TestWorkBridge*` covers the Runtime receipt boundary, persisted
links, source-transaction failure, recovery, conflicting payloads, unknown
preparation and one visible result through landing. Source-store semantics
use a Repository port fake in these tests; final cross-module SQLite-store and
native/MCP/Cloud acceptance belong to host integration.

To remove this module, delete the four new Go files and this document, remove
the `AssistantAuthority`, `AssistantWorkChanged` and `assistantMu` fields plus
the post-commit callback from `broker.go`, the `AssistantOwner` field from
`record.go`, the private assistant draft wrapper/branch from `draft.go`, the
typed admission/replay/recheck/per-Bot-capacity additions from `dispatch.go`,
and the Bot-owned briefing branch from `brief.go`. Remove the host constructor
and callback wiring separately. Restore only those additions, preserving other
work in the files. No changes to `lines.go`, `notice.go` or `observe.go` were
needed: a Bot has no fabricated Session line/terminal notice, and durable task
facts plus the source callback/Status carry its result. Keep broker rows,
feature data tables and exports; removing code never deletes Bot data.
