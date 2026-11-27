# Remote Agent work

## User outcome

An Agent in one Clawdline Session can assign bounded work to another paired
machine, exchange task messages, and receive a completion notice in its own
Session. The person does not operate the hosted Console to relay instructions
or results. The disk inventory on 2026-10-16 exposed the missing return path:
the source Agent opened a Linux Session through Chrome, sent the request, and
later read that Session manually after it had finished.

## Boundary

This is an owned remote task, not an independent Root assignment, a browser
command, or a general remote shell. The source Session retains responsibility
for the final answer. The target machine resolves its own Project and starts
its own Session. A source filesystem path is never interpreted on the target.
Only paired machines with current, reciprocal signed authority may exchange
task effects. Cloud routes ciphertext and cannot decide from task content.
The free single-machine path does not acquire a fleet dependency.

## Identity and admission

The source daemon creates an opaque task ID and stable request ID and records
its own machine, Session, execution generation, target machine and selected
target Project ID. The target daemon resolves that Project ID against its local
registered places. It validates the signed pair, Cloud revocation, its local
write switch, capability, Project availability, capacity and the source
execution before it admits a start. It claims a durable receipt before opening
one target Session. A repeat with identical bytes returns the same task and
target Session; a repeat with changed bytes is refused. If a claim was made but
the effect cannot be proved, the outcome is unknown and is not run again.

The task ID is a lowercase UUID. The encrypted body is a closed JSON object
with `assistant`, `title`, and `instructions`; it cannot supply a target path.
The target writes a private brief under its own state directory, resolves the
Project locally, and records the target execution before accepting a finish.

The target records its terminal ID, conversation ID and execution generation.
Every subsequent message names the task ID and both current executions. A
replaced source or target execution refuses an old request. Machine pairing
is the transport authority; task ownership and scope are separately checked
at every effect. Revoking the pair stops new effects, including a completion
message, without deleting the local task record or its result.

## Protocol

The machine peer rail gains versioned `task.start`, `task.message`, and
`task.finish` operations with a stable task ID, operation request ID, signed
source and target identities, and body digest. `task.start` targets a machine
and local Project before a target Session exists. It therefore cannot be
encoded as today's Session-targeted `message` or `handoff` kind. Its receiver
has a separate admission path and receipt scope. `task.message` and
`task.finish` use the exact Sessions and generations created by the start.

The source CLI exposes `clawdline remote machines`, `clawdline remote dispatch
--pair <id> --machine <id> --project <id> --assistant <name> --title <title>
--instructions-file <file> [--task-id <uuid> --request-id <uuid>]` (the two IDs
together repeat an earlier start), `clawdline remote show <task-id>`,
`clawdline remote send <task-id>`, `clawdline remote retry <task-id>` (resends the
stored start frame under the same task and request IDs), and `clawdline remote ack <task-id>`.
`remote machines` lists the active outgoing pairs; it does not report whether a
target is online and always answers `"presence": "unknown"`.
The target CLI uses `clawdline remote finish <task-id>` to publish one result.
The CLI reads local credentials, pair and execution identities; it does not
ask the Agent to compose wire headers or use a browser. The target Agent gets
a task briefing with its scope, stop condition and reporting command. Its
finish command records a bounded result in the target daemon before sending
a result pointer back to the source. The source reads that result through the
encrypted peer channel and verifies task and execution identity before using
it. Message bodies and results stay out of Cloud metadata.

## Evidence and wake-up

The task projection distinguishes `created`, `submitted`, `relay_delivered`,
`target_admitted`, `session_opened`, `agent_running`, `target_finished`,
`source_notified`, `source_observed`, and `source_acknowledged`. Failure,
cancellation, offline and unknown are typed states with the last proven rung.
Relay delivery never proves target execution or Agent observation.

The source daemon stores completion evidence and queues one durable notice to
the owning Session, using the same wake-up mechanism as a local child. The
notice contains the task ID and a `remote show` instruction, not an unverified
summary. The Agent reads the task once, then reports to the person. A lost
notice is retried from the durable outbox; a read receipt suppresses duplicates.
An offline source keeps the target result pending for reconciliation when it
returns. No machine silently moves work to another machine or blindly repeats
an operation whose outcome is unknown.

## Acceptance

Run two isolated current daemons with distinct machine identities and an
established signed pair. From the source Agent's terminal, dispatch a read-only
disk inventory to the target Project. End the source turn. Confirm that one
target Session opens, finishes, and wakes the source without Chrome; the source
reads the result and reports it to the person. Repeat the same request, change
its body under the same ID, disconnect either machine, replace either Session
execution, revoke the pair and drop each receipt in turn. Verify the exact
state and that none of these cases creates a duplicate target Session or an
unearned completion claim.


## Paired-machine discovery

`clawdline remote inspect --machine <id>` (also `remote projects` and `remote sessions`)
reads the target's current Project catalog, Session projection and installed assistants.
The Project IDs come from the exact catalog used by remote dispatch; no remembered target
path or previous task is required. The reply also identifies the selected directed pair
and the requesting Session execution. An omitted dispatch pair is selected only when one
active outgoing pair matches; ambiguity is a typed refusal.

Discovery has its own signed and encrypted `peer_discovery_publish` / `peer_discovery_envelope`
request/reply domain. It uses the existing machine pair in both directions. Cloud checks
the pair afresh before relaying; both daemons compare live pair metadata with their local
pins. Replies must match a pending random request and its exact source execution. Query
bodies and inventories remain encrypted; no viewer credential is created or required.
The response body uses the existing peer body limit. Pending queries share the peer ingress
bound, and each retains at most one reply. Duplicates are ignored and timeout releases the
pending slot. Offline relay acknowledgments end a query without claiming target execution.
Unknown capability or a lost response yields a timeout, never an empty successful inventory.

The legacy peer CLI now accepts a machine pair as well as a Session grant. A received peer
message is persisted before the broker delivers its idempotent wake to the exact current
execution. The wake contains the request identity and `remote inbox <request-id>`, which
checks the current execution before returning that exact message. Broker delivery failure
remains unknown rather than earning an Agent observation or acknowledgment. This closes the
previous gap where paired machines had no way to discover a dispatch Project and an inbox
write could leave the intended Agent unaware of it.
