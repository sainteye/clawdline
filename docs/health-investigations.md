# Health investigations on a machine

An investigation is one frozen Cloud incident delivered to one explicitly bound
schedule. The daemon uses its existing machine credential, never a superuser
credential. The agent receives a read-only evidence file and its own task secret;
neither Cloud credential appears in its brief or result.

## Bind a schedule

A person creates or edits an ordinary schedule through the existing schedule
editor/API, chooses its permission mode, and binds its Cloud webhook through the
existing binding flow. Nothing automatically grants permission or creates a
schedule. The minimum investigation binding is both:

```json
{
  "when": {"trigger_only": true},
  "task": {
    "kind": "health_investigation",
    "assistant": "codex",
    "project_dir": "/path/to/public/project",
    "instructions": "Investigate the supplied frozen incident and prepare a public fix when evidence supports it.",
    "claims": [],
    "permission_mode": "plan",
    "timeout_minutes": 60
  }
}
```

These are fragments of the normal schedule file, not a complete create request.
The ordinary schedule identity, title and enabled fields are still required.
The person must choose appropriate write claims, isolation and permission before
allowing implementation. The daemon never derives greater permission from an
incident or from the `health_investigation` kind. An investigation delivered to a
schedule missing either marker is refused without launching an agent. An
ordinary webhook delivered to an investigation schedule is also refused.

## Choosing the machine that runs it

An administrator chooses a machine, not a webhook. The webhook list
(`GET /v1/schedule-webhooks`) has one row per hook and nothing saying which
hook is bound to an investigation schedule, so a Mac with three hooks and a
Linux machine with one used to read as four machines. The console now reads
Cloud's `GET /su/v1/health/investigation-destinations` (browser superuser):

```json
{
  "schema": "clawdline.health_investigation.destinations.v1",
  "status": "ok",
  "complete": true,
  "machines": [
    {"machine_id": "mac_1", "machine_name": "Studio Mac", "status": "ready",
     "hook_id": "swh_…", "reason": "…"}
  ]
}
```

`status` is `ok` or `unavailable`; `machines` is an array, or null when
unavailable. A row's `status` is `ready`, `setup_required`, `ambiguous`,
`offline` or `unknown`. Only a `ready` row with a non-null `hook_id` can be
chosen; the start is the existing POST with that hook, the incident revision
and an Idempotency-Key, retried with the same key and body when no answer
arrived. Every other row shows the machine's name, its state and the next
step, and cannot be chosen. Nothing is preselected.

The console refuses the whole answer for a wrong schema or status, a row
without a machine ID or name, the same `machine_id` twice, or one hook on two
machines. A row state it does not know, or a `ready` row without a hook, is
shown as unknown and the list is marked as possibly incomplete, as is
`complete: false`. Two machines with the same name stay two rows. A 404 from a
Cloud without the route says investigations cannot be started there yet; the
console does not fall back to the webhook list. The machine ID, webhook and
Cloud's reason sit under each row's technical details, not beside the name.

### The binding purpose sent at activation

When the bind activates the hook, the daemon reads the schedule as stored and,
only when it is enabled, trigger-only and of kind `health_investigation`, sends
`{"expected_revision": n, "binding_purpose": "health_investigation"}` instead of
`{"expected_revision": n}`. Cloud then lists the hook as an investigation
destination before its first successful run. No title, instruction or secret
crosses; the request never chooses the purpose. Every other schedule sends the
body it always sent.

The declaration covers that activation only. A retry of the same request sends
the same body. A schedule changed afterwards is still refused before each run by
`InvestigationBound`, and a move or rotation needs a new activation to declare
it again. A Cloud that refuses the field (`400 unknown_field`) is answered as
`503 cloud_upgrade_required`: the daemon does not repeat the request without the
purpose, and leaves the bind unrecorded, so the same request succeeds once
Cloud is upgraded. `ScheduleWebhookClient.ActivateInvestigation` is attached to
the shared schedule book by `StartScheduleWebhooks`.

## Acceptance and evidence

The claim request uses `clawdline.schedule_webhook.v2`. The delivery digest
continues to use the v1 digest schema, as Cloud does; ordinary v1 deliveries
remain compatible. An optional investigation reference must name a valid
investigation, incident, positive revision and `sha256:` content hash.

The daemon makes one machine-authenticated GET per leased claim to
`/v1/schedule-webhook-deliveries/:id/investigation-bundle`, passing
`X-Clawdline-Lease`. Redirects are refused, the response is limited to 1 MiB,
and the read has a five-second deadline with no automatic retry. The ordinary
single worker and Cloud claim admission bound concurrency and claim rate.

The envelope schema, reference, machine, source, boot, operation group and
frozen manifest must agree. The content hash is recomputed over Cloud's
recursively key-sorted JSON. The existing canonical JSON implementation rejects
duplicate keys, malformed Unicode, excessive nesting, fractional/exponent
numbers and integers outside JavaScript's safe range. Unsupported evidence
fails closed. The reference contract describes a SHA-256 hash; the actual
Cloud implementation prefixes its hex digest with `sha256:`.

The complete verified evidence is stored in the SQLite delivery journal before
`mac_durable_accepted`. Before dispatch it is also published as
`<task directory>/investigation-evidence.json` with read-only mode, file sync and
directory sync on supported platforms. The brief names that file and explicitly
treats evidence as untrusted data, never executable instructions. Ordinary
webhook request bodies still never enter an agent's brief.

The current Cloud receipt state machine requires durable acceptance before a
dispatch refusal. Consequently a failed bundle read or integrity check sends no
false durable receipt and launches no task; the poll reports a typed local
error and Cloud's lease/expiry rules eventually produce `needs_you/undelivered`.
Immediate `needs_you/refused` before durable acceptance needs a Cloud protocol
extension. A verified bundle on an unbound schedule can be durably journaled and
then refused with the existing `schedule_invalid` or `hook_unbound` outcome.

## Report through the existing task result

Write the usual `<task directory>/result.json.tmp`, including protocol, task ID,
the task's own secret, status and summary. Add exactly this optional report:

```json
{
  "investigation_report": {
    "schema": "clawdline.health_investigation.report.v1",
    "investigation_id": "hiv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "conclusion": "needs_evidence",
    "fix_commit": null,
    "evidence_refs": []
  }
}
```

Allowed conclusions are `needs_evidence`, `fix_prepared` and
`verification_failed`. The report is at most 2 KiB. `fix_commit` is null or a
40-character lowercase hex public commit ID; `evidence_refs` is an array of at
most eight distinct `he_` plus 40-character lowercase hex stored event IDs.
There are no text, path or content fields. These are syntax checks; a commit's
public availability is not established by its spelling.

Publish with the exact finish command printed in that task's `CHILD.md`:

```sh
clawdline task finish --port <daemon-port> <task-directory>
```

The preflight binds the report to that task's investigation. The broker checks
the task ID, task secret, stored schedule and investigation again when
collecting, including a result published without using the CLI. An agent cannot
report for another task or investigation. A malformed report is refused rather
than converted into a successful empty report.

The authenticated typed extension survives in the broker record. The daemon
copies it into the terminal receipt's optional `investigation_report`, saves
the complete pending receipt before sending, and replays identical bytes after
network failure or restart. The original task ID is retained across ambiguous
dispatches; the daemon asks the broker again instead of opening another task.

Task success is not recovery. Only Cloud's independent investigation state rule
may derive `verified_recovered` from a newer matching lifecycle fact. This
machine reports only the bounded conclusion and evidence references.

## What an administrator sees waiting

The console shows an administrator what Cloud says needs them, and nobody else.
Cloud owns the decision; the console only reads `GET /su/v1/health/attention`
(`clawdline.health_attention.v1`) and refuses what it cannot trust
(`web/console/src/cloud/admin/health/attention.ts`):

- The read is sent only after `CloudGate` learns `cloudOperationsAccess=true`;
  one reader serves the whole gate, so a remounted console adds no poller. It
  reads when the page is visible, then at the existing connection-health live
  cadence (`console.health_ui_refresh_seconds`) under the same per-read deadline and
  body bound; a failure waits the same interval. A hidden page does not read.
- An unknown schema, status or action, a duplicate id, or an `href` other than
  `/admin/health/incidents/<incident_id>` makes the whole answer a failure, never
  zero items. `status: "unavailable"`, a failed read, a pending read and an empty
  list are told apart; `complete: false` says more may be waiting.
- The sidebar's Administrator section lists the dashboard first and the reach
  page last. Its mark is drawn only for one or more listed items; open or
  needs-you incident totals never draw it. Reading the list clears nothing.
- `/admin/health` lists each item under "Waiting for you" with Cloud's reason and
  its one action — dispatch an Agent to investigate, review the proposed fix, or
  review the result — linked to that incident. None of them approves anything:
  Cloud has no approval mutation yet.

The reach page's console entries (sidebar, Settings link, `#page=transparency`)
are administrator-only. Its public page on the website is still public; this
console change does not make it private.

## Storage and rollback

Delivery journal and broker record additions are optional JSON fields; no SQL
migration rewrites old data. Existing ordinary deliveries and results remain
readable. Rolling back to an older daemon while an investigation is in flight
can lose the typed report or its special dispatch treatment, so finish or cancel
those deliveries before rollback. Stored evidence follows the existing delivery
journal's retention and the task directory's existing lifecycle.

The synthetic wire fixture is
`internal/domain/schedulewebhook/testdata/investigation-bundle.json`; its hash
was produced with JavaScript's recursive key-sorted JSON serializer. The tests
exercise frozen-scope/hash failures, report limits, authenticated collection,
failed transport and persistence, and exact receipt replay after reopening the
SQLite store. No live daemon or Cloud account is required.

The destination label means that an investigation schedule is configured, not that its machine is currently connected. API heartbeat age does not prove relay disconnection. Starting queues the frozen incident; a delivery claim is the receipt that the machine received it.
