# Session closeability and Status vNext

Status: phases 1–3 shipped — the projection, the closure attestation, and the compare-and-swap on
close. Phase 4's targeted audit and phase 5's durable audit history are not built; the reasons why
are at the end. This extends existing Session status. It does not replace terminal state, the
`work_state` vocabulary, or the close route's `lost_if_closed` loss gate, all of which still apply
underneath it.

## The missing distinction

Clawdline already has `work_complete`, and it already has a fail-closed `lost_if_closed` check.
They answer narrower questions:

- `work_complete` proves that one process-bound child task has a machine-verified target landing.
- `lost_if_closed` sees live descendants and waits at the instant a close is requested.

Neither proves that a root Session's full responsibility graph is clear. A root can have direct
shared-tree work, a pending landing, an unacknowledged handoff, an external deployment or a user
decision without a child task. This is why idle correctly falls back to `unknown`, why `ready`
cannot mean closeable, and why ordinary roots almost never reach `work_complete`.

Three existing axes stay separate, and this adds a fourth:

- terminal `state`: what the pane is doing now;
- `work_state`: what action a person should take now;
- `owed`: a durable decision or obligation still owed;
- `closeability`: whether this Session can end.

## The projection

Every Session row on `GET /v1/sessions` and `GET /v1/orchestrator/sessions` carries the full
object. Bearings is intentionally reduced and uses the distinctly typed key
`closeability_state` plus `closeability_counts`; the same key never changes type across routes.

```json
{
  "closeability": {
    "state": "blocked",
    "reasons": [{"code": "pending_landing_owned", "kind": "obligation",
                 "subject_kind": "task", "subject_id": "…",
                 "mover": {"kind": "session", "self": true, "person_needed": false}}],
    "mover": {"kind": "session", "self": true, "person_needed": false},
    "observed_at": 1788005803,
    "session_generation": 63,
    "activity_generation": 42,
    "obligation_generation": 91,
    "version": "cl1_2f9a4c31d0be5a7788c1e6b04d3f9021",
    "provenance": ["broker"],
    "attestation_id": null,
    "source": {"provenance": "session_watch", "freshness": "current",
               "observed_at": 1788005790, "max_age_seconds": 45}
  }
}
```

Each value has one action:

- `blocked`: the broker sees a positive obligation; do not close.
- `needs_attestation`: broker blockers are clear, but local or external work is not broker
  observable; obtain an attestation about that exact Session.
- `safe`: broker blockers are clear and a fresh closure attestation is bound to the exact current
  process; the close button may proceed.
- `unknown`: evidence is stale, missing or ambiguous; refresh/audit and fail closed.

`ready` means able to accept work. `safe` means able to end. They are independent. Legacy
`work_complete` stays on the wire and still means task landing rather than root closeability.

### Doubt outranks the list

`unknown` outranks `blocked`, and that ordering is the point. A stale, missing or ambiguous source
does not merely add a row to the obligation list — it makes the list's *completeness* unknown, and
a reader handed an incomplete list reads it as a checklist. So the projection reports the doubt as
the headline and keeps the positive obligations it did see underneath it, rather than presenting a
confident short list of two things to do. Nothing doubtful can pass through `safe`.

### The reason vocabulary

Closed, and grouped by `kind`, because the three groups have three different next actions.

| kind | codes |
| --- | --- |
| `obligation` | `terminal_working`, `terminal_waiting_you`, `own_task_unfinished`, `live_descendant_task`, `task_without_result`, `pending_landing_owned`, `coordination_wait_owned`, `coordination_wait_waiting`, `open_handoff`, `completion_undelivered`, `owed_decision`, `dirty_isolated_worktree`, `touched_claims_without_closure` |
| `evidence` | `terminal_unreadable`, `session_inventory_stale`, `session_inventory_missing`, `session_identity_unbound`, `session_identity_ambiguous` |
| `attestation` | `attestation_missing`, `attestation_superseded` |

Any `evidence` reason forces `unknown`. Any `obligation` reason with no `evidence` reason forces
`blocked`. `attestation` reasons appear only when nothing else is outstanding.

Every reason names a `mover` — `session` (with `self: true` when it is this one), `person`, `task`
or `broker`. When every outstanding reason points at the same mover, the projection lifts it to
the top-level `mover` field, which is what a UI can put on one line. Several movers is reported as
`null`, because "three different people have to do three different things" is the answer and a
headline naming one of them would be a wrong one.

## Broker-owned evidence

Projected from the current registries rather than stored as a second truth, and from **records
only**: a worktree is dirty because a reading wrote that down, and a claim was touched because the
terminal-state audit said so at the time. Nothing here stats a filesystem or reads a screen at
projection time. A list response settles the obligation clock and copies the task, wait, handoff,
self-state and attestation records once; every row in that response projects from the same
immutable registry snapshot.

- terminal working, waiting or unreadable;
- a task this exact process is executing, still unfinished (`own_task_unfinished`);
- live descendant or attached task;
- a terminal task with no verified result and no summary — a child that died without reporting;
- pending landing and post-delivery worktree/claim closure belong to the dispatching root; an
  executor carries only `own_task_unfinished` while its task is live and is never told to land;
- coordination wait as owner or waiter;
- open handoff;
- an outbound completion delivery not yet acknowledged, dead-lettered included;
- a non-empty `owed` debt;
- a dirty isolated worktree, or claims the audit found touched, with no `landed`/`abandoned`
  landing decision recorded;
- stale (incomplete or older than 45 seconds) or missing Session inventory, an unbound process
  identity, or anything but exactly one inventory row resolving to this exact (assistant,
  conversation) pair.

### The two clocks

`activity_generation` is per terminal. It advances when a terminal is observed *entering* working
or waiting — a new turn — and not for a changed live line inside one turn. It is never reset when
the process in a terminal changes: a counter that is never reused cannot make an old attestation
match a new process by arithmetic, and the full identity comparison refuses that case anyway.

`obligation_generation` is machine-wide and advances only when the *content* of the obligation
evidence above changes, measured as a SHA-256 fingerprint over exactly the fields the projection
reads. It is settled at one choke point, the registry save. Bumping on every write instead would
invalidate an attestation with the very save that stored it, and would make the clock a
measurement of how busy the machine is rather than of whether anything owed changed.

Both are persisted with the registry, so a restart neither invents a tick nor loses one: the first
save after a restart recomputes the same fingerprint over the same records and finds it unchanged,
which is what lets an attestation written before the restart survive it.

### The CAS token

`version` is opaque. Clients compare it; they do not construct or parse it. It is a SHA-256 digest
over the exact process identity, both clocks, and the resulting state.

It deliberately does **not** cover the SessionWatch scan generation. That advances on its own every
few seconds, and a token nobody could hold still long enough to send would make every close a race
rather than proving anything. Scan freshness reaches the reader as `source.freshness`, the
inventory's own `source.observed_at`, and the published `source.max_age_seconds`. Incomplete,
missing, future-dated or older evidence becomes `unknown`, which no close accepts.

## Evidence the broker cannot observe

The broker cannot prove ownership of shared-tree hunks, unregistered local todos, repo-external
artifacts and deployments, user decisions not written to `owed`, or whether direct root work covers
the whole request. `POST /v1/orchestrator/sessions/:id/closure` is where that is answered — see
[`api.md`](api.md#post-v1orchestratorsessionsidclosure) for the request contract.

The route is authorized by the machine orchestrator token. Its record is bound to the exact
target terminal, assistant, PID/start and conversation, and to one observed turn through
`activity_generation` rather than through the race-prone requirement that SessionWatch display
`working` in the same millisecond. That binding names what the assertion is about; it does not
authenticate which human or process holding the machine token authored it. Operationally, ask the
target Session for its account before the credential holder posts it. The route rereads the real
terminal and inventory after recording, and only that broker merge may output `safe`.

## Closing

`POST /v1/sessions/:id/end` accepts `expected_closeability_version`, copied from the projection's
opaque token. The broker recomputes at the close:

- matching version and still `safe`: the close proceeds;
- any other state or a moved version: `409 close_not_proven` with the whole projection, reasons
  included;
- `accept_loss: true` remains the explicit human override for a positive `lost_if_closed` victim
  list, and nothing more. It cannot override stale or ambiguous evidence, `needs_attestation`,
  `unknown`, or a superseded attestation — those produce no victim list, and accepting a loss
  nobody can enumerate is not consent.

Omitting the field leaves every existing client exactly where it was. Consequently the proof
protects clients that opt in; an older client or script that omits it retains only the prior
`lost_if_closed` protection.

A closed Session disappears from the live inventory. There is deliberately no live `closed` state
and no permanent record of one; what remains is the bounded `session.end` audit row.

### Closing a Session on purpose

1. Read the row. `safe` and the close is one press with its `version` attached.
2. `blocked`: work the `reasons` list. Each names its subject id and the one thing that moves it —
   land or abandon the landing, release the wait, acknowledge the completion, clear the debt,
   let the child finish.
3. `needs_attestation`: ask that exact Session to account for its local work, then use the machine
   credential to `POST …/closure` with the row's `activity_generation`. The stored subject binding
   is exact; the HTTP credential does not prove who supplied the account.
4. `unknown`: do not close. Take a fresh reading; if it persists, the `reasons` say whether the
   inventory was incomplete, the identity unbound, or two rows are claiming one conversation.
5. Repeat the close with the version from the projection you just read. A refusal is not a retry
   loop: `close_not_proven` carries the current state, and that state is the next thing to work.

## Deep status audit

The current command is a prompt fan-out, not a durable broker audit. The projection above is the
first step of replacing it: `work_reason`, provenance and source freshness/generation are now on
Session rows and in Bearings, so an `unknown` row can be explained without waking anybody.

Not built, on purpose: idempotent targeted audits with a durable run record
(`running | complete | partial | expired`) and per-target state
(`pending | responded | unreachable | timeout | stale | contradiction`). The design note this page
carried says why, and it still holds — do not build a durable audit engine before the projection
can explain today's `unknown` rows. `audit_id` is accepted on the closure route so that the
correlation exists when the audit does. Audits collect evidence; they do not auto-dispatch,
auto-land, auto-close or decide for the user.

## UI

The Session row draws `work_state` and a separate closeability badge — safe to close, N
obligations remain, waiting for session attestation, closeability unknown. The close confirmation
lists the typed reasons with their subjects and movers beside the existing `lost_if_closed` list.

The browser re-decides the state from the frame rather than printing what it was sent: `safe` has
positive preconditions (a current reading, an attestation id, a version, no reason left standing,
and a row this page is not drawing as working, waiting or unreadable) and everything else falls to
`unknown`, which no close accepts. `unknown` carries no icon, for the same reason the work-state
vocabulary gives its absence none.

## Delivery phases

1. **Shipped.** Projection: typed reasons, provenance and generations on the read routes.
2. **Shipped.** Closure attestation and closeability computation.
3. **Shipped.** CAS-bound close route and UI badge.
4. Idempotent targeted deep audit.
5. Durable audit history only if operational evidence shows it is needed.
