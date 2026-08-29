# Session closeability and Status vNext

Status: design. This extends existing Session status; it does not replace terminal state or the
close route's loss gate.

## The missing distinction

Clawdline already has `work_complete`, and it already has a fail-closed `lost_if_closed` check.
They answer narrower questions:

- `work_complete` proves that one process-bound child task has a machine-verified target landing.
- `lost_if_closed` sees live descendants and waits at the instant a close is requested.

Neither proves that a root Session's full responsibility graph is clear. A root can have direct
shared-tree work, a pending landing, an unacknowledged handoff, an external deployment or a user
decision without a child task. This is why idle correctly falls back to `unknown`, why `ready`
cannot mean closeable, and why ordinary roots almost never reach `work_complete`.

Keep three existing axes separate:

- terminal `state`: what the pane is doing now;
- `work_state`: what action a person should take now;
- `owed`: a durable decision or obligation still owed.

Add a fourth projection, `closeability`.

## Closed projection

```json
{
  "closeability": {
    "state": "blocked | needs_attestation | safe | unknown",
    "reasons": [{"code":"pending_landing_owned","subject_id":"…"}],
    "observed_at": 1788005803,
    "session_generation": 63,
    "activity_generation": 42,
    "obligation_generation": 91,
    "version": "opaque-cas-token",
    "provenance": ["broker", "self"],
    "attestation_id": "…"
  }
}
```

Each value has one action:

- `blocked`: the broker sees a positive obligation; do not close.
- `needs_attestation`: broker blockers are clear, but only the Session can account for local or
  external work; ask that exact Session.
- `safe`: broker blockers are clear and the exact current process supplied a fresh closure
  attestation; the close button may proceed.
- `unknown`: evidence is stale, missing or ambiguous; refresh/audit and fail closed.

`ready` means able to accept work. `safe` means able to end. They are independent. Keep legacy
`work_complete` on the wire, but label it as task landing rather than root closeability.

`version` is an opaque broker-issued CAS token covering the exact process identity plus the
current activity, obligation and Session-inventory generations. Clients compare it; they do not
construct or parse it. An attestation names the projected `activity_generation`, and a successful
merge produces a new `version`.

## Broker-owned evidence

Project these from current registries rather than storing a second truth:

- terminal working, waiting or unreadable;
- live descendant or attached task;
- terminal task without a result;
- root/executor pending landing;
- coordination wait as owner or waiter;
- open handoff;
- unacknowledged or dead-letter completion delivery;
- non-empty `owed`;
- touched claims or a dirty isolated worktree without landed/abandoned closure;
- stale Session inventory, missing root identity or duplicate exact match.

Increment `obligation_generation` whenever task, landing, wait, handoff, outbox or owed evidence
changes. Any new working/waiting turn or obligation invalidates a prior safe attestation.

## Evidence only the Session can provide

The broker cannot prove ownership of shared-tree hunks, unregistered local todos, repo-external
artifacts/deployments, user decisions not written to `owed`, or whether direct root work covers the
whole request. Add a process-bound route rather than overloading `/state`:

```http
POST /v1/orchestrator/sessions/:id/closure
{
  "status": "clear",
  "activity_generation": 42,
  "note": "all owned work landed; no local obligation",
  "audit_id": "optional"
}
```

Bind it to the exact terminal, assistant, PID/start and conversation. Use an activity generation or
turn nonce, not the race-prone requirement that SessionWatch display `working` in the same
millisecond. The receipt is self-attestation, not completion; only the broker merge may output
`safe`.

## Closing

Extend `POST /v1/sessions/:id/end` with `expected_closeability_version`, copied from the projection's
opaque `closeability.version`. Recompute at the close:

- matching version and still `safe`: close and return a typed finite-retention receipt;
- any other state: `409 close_not_proven` with exact reasons;
- preserve `accept_loss:true` only as an explicit human override after showing the positive
  `lost_if_closed` victim list. It cannot override stale or ambiguous evidence,
  `needs_attestation`, or `unknown`.

A closed Session disappears from the live inventory. Do not create a permanent live `closed`
state; retain only the bounded audit receipt.

## Deep status audit

The current command is a prompt fan-out, not a durable broker audit. Improve it in two steps.

First:

- expose `work_reason`, provenance and source freshness/generation in Session rows and Bearings;
- ask only `needs_attestation` and relevant `unknown` Sessions;
- give each audit an idempotent `audit_id`;
- accept structured closure/status receipts, with prose as explanation only.

Then add a durable run record:

- run: `running | complete | partial | expired`;
- target: `pending | responded | unreachable | timeout | stale | contradiction`;
- initial/final registry generations, deadline and receipt per target.

Audits collect evidence. They do not auto-dispatch, auto-land, auto-close or decide for the user.

## UI

Show `work_state` and a separate closeability badge:

- Safe to close
- N obligations remain
- Waiting for Session attestation
- Closeability unknown

The detail surface lists typed reasons and freshness. `unknown` never renders as safe, and `ready`
never implies safe.

## Minimum tests

- exhaustive four-state table;
- each blocker has a removal/mutation that makes its named check red;
- stale, missing and ambiguous sources never produce `safe`;
- a new task/landing/wait/handoff invalidates attestation through generation change;
- PID/start/conversation reuse invalidates attestation;
- a dispatch racing close makes the version compare fail;
- `ready != safe` and task `work_complete != root safe` in API and UI;
- repeated `audit_id` does not wake Sessions twice;
- timeout, unreachable and contradiction remain distinct;
- restart preserves generation/attestation consistently;
- loss override continues to show exact victims.

## Delivery phases

1. Projection only: typed reasons, provenance and generations on read routes.
2. Closure attestation and closeability computation.
3. CAS-bound close route and UI badge.
4. Idempotent targeted deep audit.
5. Durable audit history only if operational evidence shows it is needed.

Each phase is separately reviewable and keeps the current close gate. Do not build a durable audit
engine before the projection can explain today's `unknown` rows.
