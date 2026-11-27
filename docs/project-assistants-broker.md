# Persistent Bot work through the existing broker

This module is an opt-in dispatch boundary, not a second task system. A Bot is
not a Session, terminal, human run, schedule or detached automation. Without a
source authority and result sink the typed entry point refuses work; a
dispatch that continues on completion also needs the completion callback. Mounting
HTTP/MCP tools, enabling the feature, native-provider acceptance, Cloud delivery
and release acceptance are separate host integration work.

## Host construction

Construct `orchestrator.NewAssistantAuthority(AssistantAuthorityOptions)` with
`Current func(context.Context, pa.BotKey) (AssistantCurrent, error)`, an
explicit `Clock func() time.Time` and `ResolveProject func(context.Context,
pa.ProjectSelection) (pa.ProjectResolution, error)`. The constructor returns
`(*AssistantAuthority, error)` and rejects a missing dependency.

`Current` must read the source machine's canonical registered Project, Bot,
Conversation, binding, active turn, feature/source access and dispatch policy.
It supplies `Fence`, `Source`, `Feature`, `BotName`, `BotAppearance`, `BotAgent`,
`ActiveTurnID`, `BindingRevoked`, `Project` (a current `pa.ProjectResolution`),
`DispatchAllowed`, `ProjectDispatchAllowed`, `ProjectDispatchAskable`,
`ApprovedProjects`, `AllowedClaims`, `AnyProjectFile`, `Assistant`,
`TimeoutMinutes`, `SharedSettings`, `DispatchAgents`, `Resolved` and `WorkOwner`;
the broker itself sets `BotRequestID`. None of these may be decoded
from model arguments or copied from a client-provided absolute directory.
`AllowedClaims` is an explicit list of exact relative files authorized by host
policy; `AnyProjectFile` instead admits any exact relative file inside the
Project, and the production dispatch policy sets it. Native read approval and
model prose do not establish this policy.

Assign the authority to `Broker.AssistantAuthority`. Construct
`projectassistant.NewWorkBridge(WorkBridgeOptions{Repository, Broker, Clock})`,
which returns `(*BrokerWorkBridge, error)`, then connect
`Broker.AssistantWorkChanged = bridge.Sync`. The callback has the exact type
`func(context.Context, orchestrator.AssistantWorkSnapshot) error`. A dispatch
with `ContinueOnCompletion` also requires `Broker.AssistantWorkCompleted`, of the
same type; without it the request is refused as `work_result_sink`.

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
`Record.assistant_owner`, contains only public provenance: the historical
fence, Bot name and appearance, turn, original command, request digest, target
Project, persona, `ContinueOnCompletion` and peer-dispatch attribution. It
grants no authority.

## Admission and ownership

`Broker.DispatchAssistant(ctx, pa.WorkRequest)` builds an in-memory draft from
host policy and enters the ordinary `Broker.Dispatch` pipeline. The internal
admission marker is unexported; JSON cannot select this door. Ordinary Session
dispatch still validates its root Session and Squad capability as before.
Replaying a Bot task through an ordinary dispatch request is refused.

The Bot door runs the existing broker claims validator and rejects traversal,
absolute paths, globs, directory claims, symlink components and, unless
`AnyProjectFile` is set, files outside the host's allowed list. An explicitly targeted investigation or a dispatch
with a completion callback may declare an empty list for read-only work. It
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
operation. It can hand whole ownership to an independent Root (see "Opening a
Root" below); the Root, not the Bot, then integrates. Board changes still need the existing human/run authority.

`AssistantTaskID(command)` derives a stable UUID from source machine, Bot,
operation and idempotency key. It excludes the content hash intentionally:
reusing the command key with a different hash must conflict. The broker stores
the original command and `AssistantRequestHash(request)`, which independently
covers the historical fence, turn, claims and brief. Neither digest includes
the credential. Identical commands return one accepted task; changed payloads
conflict even if the caller leaves its claimed content hash unchanged.

## Opening a Root

Decided 2026-11-25 (the person, in the COO Bot's chat). An owned child cannot
integrate: its CHILD.md forbids push, merge and worktree commands, and the Bot
actor has no landing operation, so nobody on a Bot's line could legitimately
land work. A Bot therefore chooses per dispatch between a **child** (unchanged:
bounded work that returns a branch for review) and a **Root** (integration,
release, or a whole feature handed off end to end).

- **Tool shape.** `dispatch_work` takes `mode: "child" | "root"`, default
  child, rather than a second tool: the destination, persona, agent rules and
  idempotency key are the same, and a second tool would duplicate them. The
  mode travels in `pa.DispatchChoice.Mode` (omitted when empty, so every
  earlier request hash is unchanged). `claims` may be empty for a Root, and
  `continue_on_completion` is refused with it. `Broker.DispatchAssistant`
  refuses a non-empty mode, so a Root never silently becomes a child.
- **Idempotency.** `Broker.DispatchAssistantRoot` runs the same
  `checkAssistantRequest` source authority as a child, then calls the
  ordinary `OpenRootAssignment` with `request_id = AssistantTaskID(command)`.
  A retry replays that Root Assignment receipt and opens nothing; the same key
  with different content is `pa_idempotency_conflict`, because the request
  digest covers the `bot_owner` record including `AssistantRequestHash`.
- **Authority.** The host admits mode root only on a turn the person started
  (`directUserTurn`) or, for a request card, a handoff-woken turn of the
  card's holder working on that card (`botRequestDispatchScope`); anything
  else is `pa_scope_denied` field `dispatch_root_origin`. A thread worker
  answering another Bot cannot open one. The destination rules are the
  child's: the Bot's own Project directly, another Project only through the
  existing permission card. No approval card is added for the Bot's own
  Project; the person sees the new Root Session open, as for a child.
- **Ownership record.** `RootAssignment.BotOwner` (`RootBotOwner`) records the
  Bot's fence, turn, command, request hash, display name, and optional
  request card and Board item. It is provenance, not parentage: the record
  still has no task, secret, timeout, result or landing. The bridge persists
  `AssistantRootLink(a)` as a work link in the Bot's chat, so the chat strip
  shows the Root like other work; its `task_id` is the Root Assignment id, and
  `work_status` does not follow it.
- **Delivery notice.** `ReportSessionDeliveryWithEvidence` queues
  `bot.root.delivered` in the same append as the delivery receipt when the
  reporting terminal is an open, Bot-opened Root whose Bot has not been told.
  The handler calls `Broker.AssistantRootDelivered`; the host's
  `rootDelivered` writes the Root's summary as the link's result message and
  reuses the ordinary work-completion wake (`WakeWorkCompletion`, keyed on the
  Root id, so a replay queues no second turn), honouring the Bot's work review
  setting. On success the row records `bot_owner.delivered_at`; later reports
  from the same Root queue nothing, and a queued duplicate finishes as
  `bot_already_told`. It fires once per Root, not once per report.
- **The Root's brief.** The ordinary five-section Root Assignment brief
  (objective = title, scope = brief, constraints, references, acceptance) plus
  an `OPENED BY A BOT` section: who opened it, that it is nobody's child and
  works under the ordinary Root rules, and that `clawdline session report` is
  what tells the Bot.
- **Concurrency.** Several Roots may push `main`. The existing protection is
  enough for now: ordinary pushes only (never forced), the repository's
  pre-push hook, and re-fetch/merge on rejection. A merge queue is deferred
  until rejected pushes are observed to cost real time.
- **Review.** Review verdicts a Bot recorded stay displayed on its request
  card and are named in the Root's constraints; they are not enforced, as in
  the request card design.

Not built here: the roster's per-Bot count of open Roots and a request-card
row for a Root (the chat strip link covers the opening Bot); a merge queue.

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

After a successful Bridge return the Runtime writes no WorkLink of its own:
`Engine.finishDispatch` reads back the link the Bridge persisted. The Bridge
must not overwrite the original Runtime command receipt.

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
and can be repaired by `Status`. The host reconciles the result before delivering a completion callback. A Bot
whose work review setting is off only gets the terminal result published to
its chat and stays idle, unless the dispatch requested the callback.

### Waiting for dispatched work

Since 2026-11-23 every dispatched task is reviewed by default: the Bot's
`work_review` setting (Bot settings, "Take over dispatched work"; omitted means
on, readable and writable through the local API, Cloud and the phone) queues
one review turn for each task. Turned off, the result is only published.
`dispatch_work` with `continue_on_completion: true` still requests the turn
explicitly, whatever the setting, when answering the current request depends on
the worker's result. For a long build, test or
external wait, the brief gives the isolated work Session the command and
asks it to use the existing [command callback](broker.md#callbacks-waiting-without-a-turn-2026-10-06).
Once the dispatch is accepted, the Bot ends its turn. The same native turn
may read a running task's status once. A repeat returns
`work_completion_pending`; receipt reconciliation by the original dispatch
key and later human status questions remain available.

Worker settlement records a `bot.work.completed` outbox effect in the same
transaction. Every terminal state — success, failure, timeout, cancellation and
`spawn_failed` — takes this one path: the effect reconciles the result and,
when the Bot reviews work or the dispatch requested it, queues at most one
review turn with a reference to the existing result message. The first moment
a Bot-owned task waits for a person (`waiting_for_person`, its briefing held at
a dialog) records `bot.work.waiting`, run right after the commit; it queues the
same single review turn without a quote, telling the Bot to tell the user what
the worker needs. There is no second wake path: both effects go through
`WakeWorkCompletion`, and its `runtime-work-completion-<taskID>` send receipt
is checked for existence, not content, so a task reviewed while waiting is not
reviewed again when it settles, and replay or restart queues nothing.

The review prompt says it is a report on dispatched work, not a new
instruction from the user, and names the task ID, title and state; the result
summary is the quoted message. The turn is a host notice and carries no user
authorization. A review turn that dispatches again extends a chain; at the
routine follow-up limit (`bots.follow_up_chain`, 24) the host writes one notice
in the chat instead of a turn, under the same receipt. The Bot reply uses the ordinary conversation
queue. The task ID keys its send receipt, so a retry of callback delivery
does not launch the worker or queue another Bot turn. The broker's existing
outbox recovery handles interrupted delivery. An unstarted follow-up survives a
daemon restart; an interrupted model prompt is never sent again automatically.
A conversation reset, forgotten source or cancelled originating turn prevents
the quoted result from starting a follow-up.

At the assistant boundary, the broker's known `over_capacity` refusal becomes
`pa_capacity_full` with the original retry-after metadata. Its raw diagnostic
sentence is not transported as assistant product copy. This mapping does not
turn a durable unknown preparation into permission to retry: subsequent calls
with that same intent remain unknown unless existing broker facts are recovered.
Localized presentation and the production HTTP/tool envelope remain host work.

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
