# Owned Bot tools over MCP

A Bot's ACP session receives its captured fixed instructions and one private
loopback MCP endpoint. The endpoint calls the existing Host functions for
`send_message`, `dispatch_work`, `work_status` and `message_work`. It does not create another
actor registry, broker, receipt store or model provider. This follows the pinned
Codync host-owned chat-tool boundary; the Go host retains Clawdline's existing
Project, claims, worktree and durable-result authority.

## Execution and authority

`Host` puts its private `NativeTools` functions in the internal `OpenRequest`.
The field is excluded from JSON. Every function captures the actual execution
fence and active turn, checks the current source/feature/revocation state, and
uses the existing engine and broker bridge. The model supplies operation data,
never a Bot/Project identity, actor, human approval or machine credential.

During a direct user turn, the designated Lead can set `project` on
`dispatch_work` to a unique registered repository folder name or absolute
registered path on this machine. The Host resolves it through the same
Project registry used by Bot setup. The Broker rechecks the canonical
destination before recording the task, checks claims relative to that
repository, and starts a new isolated Session there. Investigations can
declare an empty write set with `claims: []` and describe the investigation
scope in the brief. The work receipt and result belong to the source Bot
conversation. The destination is included in the durable task provenance
and idempotency hash.

Any other Bot with a dispatch policy (allowed, timeout above zero) may set
`project` too, but it first asks the person. The Host shows a
`dispatch_project` permission card in that Bot's chat naming the Bot, the
destination folder, the Session title and the claims, with three choices:
allow this time, always allow this Bot → Project, or decline. No Bot
permission mode answers this card automatically. The turn waits on the card;
an expired or abandoned card never dispatches. The card ID is derived from
the turn and the model's idempotency key, so a resent call reads the same
card's stored answer instead of asking again: allowed reaches the normal
receipt replay, declined stays `pa_declined`, expired stays `pa_expired`.

Always allow saves one exact Bot-to-Project rule in
`project_assistant_dispatch_rules` before the dispatch it allows. The rule is
listed and removed on the existing saved-permission-rules settings surface
(`GET`/`DELETE` of the Bot's rules), as
`clawdline-dispatch-project:<project id> <folder>`; at most
`capacity.MaximumBotDispatchRules` are kept. Only the person's answer or a
saved rule puts a destination in `AssistantCurrent.ApprovedProjects`; the
Broker still rechecks the destination and claims before recording the task.

A card can be raised only on a turn the person started by talking to the Bot.
Turns from another Bot, a room, the Network or a routine are refused with
field `dispatch_project_direct`, telling the Bot to ask the person to talk to
it directly. Saved rules deliberately do not apply to those turns either.
The lead keeps its direct route and is never asked.

Any Bot that may dispatch can also set `persona` on `dispatch_work`, with or
without `project`, to start the work Session with a Session persona
(`docs/personas.md`). The MCP layer only carries the id: the Broker admits it
with the same `admitPersona` rule as every other dispatch, so a built-in id
from `persona.IDs()` or a namespaced custom id from the Squad page is
accepted, and anything else is refused as `pa_invalid` with field `persona`
before any preparation receipt is written. That refusal, and the tool's
description, list the built-in ids generated from `persona.IDs()`, so neither
can drift from the catalog. The persona is recorded in the task's
`AssistantOwner`, in the request hash and in the Host's content hash, so one
idempotency key reused with another persona is `pa_idempotency_conflict`. An
empty persona is omitted from both hashes, which keeps receipts written before
the field existed replayable. The dispatch reply echoes `persona`;
`work_status` does not show it yet.

The ACP provider creates an HTTP MCP capability only after native capability
admission. Its random bearer is independent of the daemon's credential. It is
restricted to a literal loopback address with an explicit port, one execution
fence and one listener. Metadata/capability reads do not start a listener or
native process. Production still has no configured, admitted native provider.

The transport follows the JSON-response profile of
[MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports).
It binds to `127.0.0.1`, checks the exact Host and bearer, rejects browser Origin
headers, and exposes no CORS access. GET has no SSE stream and returns 405.
Initialization and its initialized notification precede tools. Subsequent
requests carry the negotiated protocol revision. There is no resumable HTTP
session or server-initiated stream to restore after the execution ends.

The [MCP tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)
have closed argument schemas. Mutating calls require an explicit durable
`idempotency_key`, independent of the JSON-RPC request ID. The host's existing
receipt conflict/replay rules remain authoritative; this transport does not
retry an unknown dispatch. Failed tool results expose a stable error code,
not native stderr, a bearer or arbitrary exception text. Duplicate JSON keys,
invalid Unicode, unsafe numbers and excessive nesting are rejected through the
existing Cloud JSON parser. This narrow profile accepts string/integer RPC IDs.

## Messaging dispatched work

`message_work` lets a Bot correct or add to a work Session it dispatched while
that Session is still running. The model supplies only `task_id`, `text` and
`idempotency_key`; it never names a terminal, sender or identity. The Host
binds the execution's signed actor, which needs the dispatch operation, and
`Broker.AssistantMessageWork` reads the task record to decide the rest:

- The task's `AssistantOwner` must match this Bot, Project and conversation,
  otherwise `pa_scope_denied`/`work_owner`.
- The task must be briefed (the worker signed for its briefing) and its
  terminal must still be a live assistant Session. A queued or spawning task
  answers `pa_offline`/`work_not_running`; a finished, failed or cancelled task
  answers `pa_expired`/`work_finished`.
- Text is at most `bots.work_message_bytes` (8,192 UTF-8 bytes), refused
  before acceptance with `pa_capacity_full`/`text`.

Delivery is the same durable outbox effect as `clawdline send`: the intent is
recorded under a receipt, then one `<clawdline-message>` envelope line is typed
into the worker's composer at once. A worker that is busy reads it at its next
opportunity, as with any Session message. A worker showing a menu, or a
terminal another writer holds, is refused rather than typed into
(`work_session_menu`, `work_session_busy`), and the key is released so the same
key can be sent again. The envelope's source is the Bot's name with
"(dispatcher)" and its own agent, and the body begins
"來自 <Bot>（派工者）／From <Bot> (dispatcher):" followed by a note that it is
quoted guidance, not the person's authorization. It grants the worker no new
permission; deployment, pushing, release and scope changes still need the
person or the worker's briefing.

The receipt is scoped to the owning Bot conversation. The same key and text
return the first answer again (`replayed`), even after the task has ended, so
an uncertain call is resolved under its original key; the same key with other
text is `pa_idempotency_conflict`. The answer separates `stage: accepted`
(durable, delivery not settled: ask again under the same key) from
`stage: delivered` (typed into the composer). Neither means the worker read or
followed it; its next turn and `work_status` are that evidence.

### While answering another Bot

A Bot answers another Bot's `message_bot` in a private thread worker
(`thread-<task>`), whose Bot key and conversation differ from the chat that
dispatched the Bot's work. Until 2026-11-23 that turn could not reach the work:
`work_status` compared the worker's own conversation with the task's owner and
answered `pa_scope_denied`/`work_owner`, and the worker had no `message_work`
at all (`pa_disabled`/`native_work`). A request such as "add this spec to the
task you are running" then stopped at the hand-off.

On a peer-request turn the Host now resolves the worker to the Bot that
durably owns it (`project_assistant_background_tasks`) and passes that Bot's
current conversation to the broker as `AssistantCurrent.WorkOwner`. The broker
accepts a task when its `AssistantOwner` matches either the actor's own
conversation or that resolved owner on the same machine and Project. The owner
always comes from the task record and the background-task row, never from the
model. The actor itself is still the worker's, signed for its live peer turn,
so the capability ends with that turn.

- `work_status` and `message_work` reach work this same Bot dispatched.
- Work another Bot dispatched is refused as
  `pa_scope_denied`/`work_dispatched_by_another_bot`: ask that Bot instead.
- `message_work` text is prefixed with
  "轉述 <requesting Bot> 的請求／relaying a request from <requesting Bot>:", after
  the broker's dispatcher header, so the worker reads a peer's relayed request,
  not the person's authorization. A request from another machine names
  "another Bot". The prefix counts toward the 8,192-byte limit.
- A background thread that is not answering another Bot still has no reach
  into its owner's work; `message_work` there answers
  `pa_scope_denied`/`message_work_from_main_chat`.

Other tools blocked on peer-request turns (`dispatch_work`, `send_message`,
routines, outputs) are unchanged.

## Direct Lead Bot controls

A designated Lead responding to a person in its own chat receives `manage_bots`.
It can list and operate Bots across registered Projects and personal workspaces on
this source. It uses the same application operations as the console, under a
separate Lead principal and the current live turn. It has no reusable human or
machine credential. The Host rechecks the role and turn at admission and through
the service authorization checks. The tool exposes a fixed list of Bot operations,
never arbitrary network or daemon requests.

Use an empty path with `GET` for the roster and `GET /<bot-id>` for a Bot's
settings and version. `PATCH /<bot-id>` takes a `settings` object with
`expected_version` and the fields to change, such as `name`. It preserves the
current name, instructions and appearance when omitted. Pass a stable
`idempotency_key` for mutations and use the latest target version or fence.
Creation accepts an exact registered `folder` name or absolute path. The tool
also exposes visibility, deletion, work and queue controls,
schedules, memory, group management and existing development server controls.
A person's direct request can be performed without a second team proposal.
Legacy team proposals remain available when the person asks for a proposal.

Delegate with `message_bot`. During a direct user turn the Lead can discover
and contact hidden Bots too. The recipient still runs with its own authority.
Use `dispatch_work` for an isolated repository Session. Peer, routine, room,
voice-greeting and background work do not get direct Lead controls. Credential
entry and native permission answers stay on the person's own surface.

## When a dispatch's outcome is unknown

The broker derives a work task's ID from the Bot identity and the
`dispatch_work` idempotency key, so the key alone can find the task again.
After `pa_outcome_unknown` the Bot never invents a new key:

- When the reply carries `task_id` (the broker accepted the task but the link
  was not persisted), it follows that task with `work_status(task_id)`.
- Otherwise it calls `work_status(idempotency_key)`. The Host takes the Bot
  identity and conversation from its authenticated fence, never from the
  model, and answers one of three states from the same receipt rows and broker
  record that dispatch itself uses:
  - `accepted`: `task_id` and the same `work` fields `work_status` returns.
  - `not_accepted` with `safe_to_resend_same_key: true`: the Engine receipt
    exists but the bridge never prepared a broker call. Resending the same key
    and content creates the task once.
  - `undetermined` with a `reason`: `broker_admission_unknown` (the broker was
    asked and holds no task; it refused or the call was cut off, and the
    bridge never replays that), `dispatch_in_progress`, `broker_unreadable` or
    `receipt_unreadable`. A resend is not known to be safe.
- A key this Bot never dispatched, and a task of this Bot owned by another
  conversation or Project, both answer the same `pa_not_found`
  (`work_receipt`). Another Bot's key lives in another Bot's namespace and can
  never address that Bot's work. A cross-Project dispatch whose permission
  card is unanswered or declined never wrote a receipt, so it also answers
  `pa_not_found`.

A resend with the same key and content in the same turn is handed back to the
work bridge instead of being refused outright: the bridge returns the accepted
task from broker facts, refuses an unknown admission, and only runs a dispatch
that never reached the broker. A different turn changes the content hash and
still answers `pa_idempotency_conflict`; use the lookup instead.

## Native session settings

Before creating/loading a session, the client checks ACP protocol 1, the exact
verified adapter package/version, and the needed system-prompt/HTTP-MCP
capabilities. Both new and loaded sessions receive the same captured settings.

For Claude ACP 0.106.0, the request uses the pinned Codync-style append preset
with dynamic sections excluded, an empty built-in tool list, empty setting
sources, strict MCP configuration and memory/dream/hooks disabled in request
settings. For Codex ACP 2.6.0, it uses the adapter's typed append-only text
`systemPrompt`. These are launch requests, not proof that native settings,
filesystem restrictions or memory isolation took effect. Independently verified
native admission remains required; no full-privilege fallback is added.

## Capacity and cleanup

The existing per-session `Limits.RPCPending` is one shared budget for outbound
ACP requests and accepted MCP connections. Headers and body reads therefore
cannot allocate an unbounded number of native handlers. Idle connections have
bounded header/read deadlines; keep-alive is disabled. A full budget refuses
new MCP connections with a typed `pa_capacity_full` HTTP 503 after a bounded
header read, without decoding a body or executing a tool. ACP calls use the same budget and occupancy reading.
A native message-capable configuration needs at least two slots so a pending
prompt can coexist with its tool request.

MCP bodies/responses use `Limits.FrameBytes`; HTTP headers use that server
setting plus Go's standard parser overhead. Existing write/RPC deadlines and
the existing bounded runtime process slots also bound listeners. No new
numeric resource policy is introduced.

Closing seals tool admission and cancels tool contexts before process cleanup.
A handler retains its shared slot until it exits, including when a connection
has been closed. A cancellation-ignoring callback produces unknown cleanup;
its handle remains attached to the engine until explicit cleanup succeeds.
Partial provider initialization likewise returns a cleanup handle when
resources cannot be proved stopped. It never converts an unknown close to an
invented successful resource release.

## Acceptance scope

The local behavior checks use an owned Go ACP peer and synthetic Project data.
They can exercise the actual HTTP MCP endpoint, Host, SQLite repository,
claims/worktree broker and durable result projection without a vendor/model
request. They do not establish Claude/Codex native sandbox or memory admission,
production Cloud, a physical phone, a dev installation or a release.

The separate authorized offline adapter initialization allowance remains
consumed at one attempt per provider. These tests do not invoke those installed
adapters and do not reset or expand that allowance.

For removal, remove `mcp.go`, `session_configuration.go` and their paired tests,
the Host MCP integration test, this page, and the paired wiring in
`capabilities.go`, `client.go`, Host `provider.go`, app `protocol.go` and the
capacity source/measurement/copy changes. The earlier physical-removal rehearsal
is evidence for its recorded revision only; adding this unit requires updating
any later complete removal inventory.

## Permission lifetime

Cancellation seals permission admission before writing native cancellation
frames. Answer and cancellation writes are serialized, offers retain their
captured turn, and late permission requests receive a cancelled outcome. A
native prompt response seals the same permission lifetime. A cancelled client
cannot be reused for another prompt or promote a late successful result;
the runtime opens a new execution-owned client for its next turn.
