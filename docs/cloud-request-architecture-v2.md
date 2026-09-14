# Cloud request architecture v2: reads are data, commands are jobs

Status: target design, derived from first principles. This document deliberately does not begin
with the current relay, spool, or request handlers. The current system appears only in the
diagnosis and migration sections, as evidence about failure modes and compatibility work.

## 1. The question this design answers

The product needs to let a person open Clawdline Cloud, see useful Session state, read a
conversation, and send work to a Mac. Those are three different operations:

1. A **status view** is a small, latest-value projection.
2. A **conversation view** is a paged, append-only history plus live deltas.
3. A **command** is an asynchronous effect that may require an intermittently connected Mac.

They do not need the same consistency, storage, scheduling, or failure semantics. A correct design
therefore gives them separate data planes. Ordinary reads never wait for a Mac, and background
replication never consumes the capacity reserved for an interactive command.

The decisive distinction is this:

- An optimisation asks how to make `info.summary` and `transcript` request/reply faster.
- A first-principles design asks why either is a request to the Mac at all.

## 2. Evidence from the 2026-09-14 incident

The installed 0.8.0 app was connected when sampled. The slow experience was not explained by one
offline interval or one slow endpoint. Parsing the Mac log by request stage produced these
distributions for 2026-09-14:

| Operation | Samples | Route p50 / p95 | Publish p50 / p95 | End-to-end p50 / p95 | Maximum |
|---|---:|---:|---:|---:|---:|
| `info.full` | 243 | 2,101 / 4,656 ms | 75 / 610 ms | 2,254 / 5,123 ms | 23,272 ms |
| `info.summary` | 288 | 1,297 / 5,712 ms | 97 / 526 ms | 1,455 / 7,166 ms | 83,316 ms |
| `transcript` | 371 | 177 / 1,340 ms | 172 / 1,214 ms | 407 / 3,058 ms | 26,567 ms |

The same log contained 199 outbound-window-full events, with a maximum ready age of 10,851 ms and
a 17,456 ms receipt wait. A representative trace showed an eleven-Session snapshot batch taking
39.4 seconds, followed by a batch taking 28.0 seconds. During it, individual durable spool commits
took 0.8--3.2 seconds and socket writes took 0.25--4.6 seconds. Foreground and background reads then
queued behind that work for five to eight seconds.

Two independent mechanisms compound the delay:

- `info.summary` computes facts from the active transcript. Its cache identity includes the
  append-only file's size and modification time, so every new event can cause another read and
  parse of up to 8 MiB.
- Cloud publication shares an actor and a global durable outbound spool with recurring Session
  snapshots. Each spool transition serialises and atomically replaces the whole file; the sampled
  file was about 514 KiB with 654 rows, all terminal. A periodic burst of nine to eleven changed
  Session envelopes included a roughly 723 KiB first frame and repeatedly filled the eight-row
  outbound window.

The relay confirms the architectural dependency: it routes encrypted envelopes and keeps only an
in-memory, bounded last-envelope cache. The cache is lost on Durable Object eviction, entries over
256 KiB are not retained, and no ordinary stream content is durably stored. A viewer that missed
state must therefore wait for a new full snapshot or ask the Mac to regenerate it.

These observations establish a coupling failure. They do not dictate the replacement design.

## 3. Product invariants

The architecture begins with invariants that remain true across implementations:

1. **Opening Cloud is useful without a live Mac.** The latest successfully replicated status and
   conversation history remain readable, with their age visible.
2. **A read cannot cause an effect on the Mac.** Reading status or history requires only read
   authority and never publishes a command.
3. **First content is bounded.** Initial status and conversation rendering do not scale with total
   transcript size, total Session history, or another Session's traffic.
4. **Ordering is scoped.** Events are ordered within one logical stream. Unrelated Sessions do not
   need a global sequence or a shared head-of-line.
5. **Backpressure is local and typed.** One hot Session, slow disk, or large transcript can delay
   only its bounded class or partition; overload is reported with a retry policy.
6. **Effects are idempotent.** A command may be transported more than once but its declared effect
   executes once for one idempotency identity.
7. **Truth is staged.** Accepted, leased, executed, result-persisted, delivered, observed, and
   acknowledged are different durable facts.
8. **Encryption remains end to end.** Cloud stores and indexes ciphertext plus the minimum routing,
   revision, size, expiry, and integrity metadata. It does not learn transcript or status content.
9. **Recovery is a normal path.** Resume cursors, gaps, stale projections, and offline machines are
   represented in the protocol, not collapsed into a spinner or timeout.
10. **Every slow operation can be reconstructed.** A privacy-safe trace joins browser, relay, and
    Mac stages, including the final meaningful paint.

## 4. The target architecture

```text
                         encrypted, durable Cloud substrate

  Mac projector  --->  Status projection store  --->  Browser status cache/view
       |                 latest value by Session          (no Mac RPC)
       |
       +----------->  Transcript segment store  --->  Browser pager + live tail
       |                 immutable paged chunks           (no Mac RPC)
       |
       <----------->  Command job ledger          --->  Browser job observer
                         lease + receipt states            (asynchronous effect)

  All three use independent partitions, quotas, cursors, and schedulers.
  A small manifest names current encrypted revisions; payload bytes are not in the manifest.
```

### 4.1 Status plane: encrypted latest-value projections

The Mac owns a local incremental Session projector. It consumes transcript events once and
maintains the fields the status line needs: activity state, current phase, attention/wait reason,
last assistant completion, token summary, and source revision. It does not re-read an 8 MiB tail
when a viewer appears.

For each Session it publishes a small encrypted record keyed by opaque account, machine, and
Session identifiers:

```text
StatusRecord {
  schema_version
  session_key
  source_revision
  generated_at
  ciphertext
  ciphertext_digest
}
```

Cloud durably replaces the record only when `source_revision` advances. A compact encrypted
machine manifest lists Session keys, current record revisions, tombstones, and manifest revision.
The browser fetches the manifest and only the missing records, then applies live deltas. A single
status update is O(one small record), not O(all active Sessions).

Staleness is part of the view. The browser can say "last synced 4 min ago" while rendering the
last valid record. An offline Mac changes freshness, not readability.

### 4.2 Transcript plane: immutable encrypted segments

A conversation is an append-only logical event stream, stored as bounded encrypted segments. A
segment contains a deterministic event range and is immutable after publication:

```text
TranscriptSegment {
  schema_version
  session_key
  first_event_revision
  last_event_revision
  previous_segment_digest
  ciphertext
  ciphertext_digest
}

TranscriptHead {
  session_key
  head_revision
  newest_segment_digest
  updated_at
  ciphertext_metadata
}
```

The relay/object store may index the opaque Session key, revisions, byte sizes, digests, and
retention time. Only authorised devices decrypt the segment. The hash chain detects omission or
reordering without exposing content.

The initial conversation request fetches the encrypted head and the newest one or two bounded
segments. Older history is cursor-paged. Live events arrive as small deltas and eventually become
an immutable segment. IndexedDB retains verified encrypted segments for fast reopen and offline
reading. Total history size never affects first paint.

The writer chooses a segment boundary using both byte and event ceilings. A provisional starting
point is 128 KiB compressed plaintext or 200 events, whichever comes first; measurement should set
the final constants. Oversized individual events become separately referenced blobs rather than
breaking the segment bound.

### 4.3 Command plane: durable asynchronous jobs

Only an operation that must affect the Mac is a command. Submitting one creates a durable job and
returns its identity immediately:

```text
CommandJob {
  request_id
  idempotency_key
  target_machine
  encrypted_request
  accepted_at
  expires_at
  state
  attempt
  lease_owner
  lease_until
  encrypted_result_ref
}
```

The browser receives `accepted` after Cloud has persisted the job, normally as HTTP 202 or its
WebSocket equivalent. A Mac leases the job, executes through a request-id ledger, and writes a
versioned receipt. The browser observes the job stream or fetches the job by id. Closing the tab,
reconnecting, or a 60-second network interval does not erase the outcome.

The state machine is explicit:

```text
accepted -> leased -> executing -> result_persisted -> delivered -> observed -> acknowledged
    |          |            |
    +------ expired/refused/failed/cancelled (typed terminal states)
```

Transport is at least once. The request ledger makes the effect exactly once where an effect can
be made idempotent; otherwise it records the narrow ambiguity instead of silently retrying. Lease
expiry allows another delivery attempt. Same idempotency key and same digest replay the existing
job; a different digest is a conflict.

## 5. Persistence on the Mac

The Mac uses a transactional append-oriented store such as SQLite in WAL mode, not a JSON array
that is rewritten and fsynced for every state transition. Separate tables hold:

- status heads and pending projection revisions;
- transcript segment metadata and upload state;
- command inbox, leases, effect ledger, and receipts;
- per-stream cursors and acknowledgements;
- an outbox with payload references rather than repeated payload copies.

Enqueue, lease, settle, and acknowledgement are bounded indexed transactions. Terminal rows are
compacted or expired in background chunks and are never scanned on an interactive write.
Ciphertext blobs may live in content-addressed files; the database transaction owns their
reference and publication state.

Durability policy is class-specific. An accepted command or effect receipt crosses a durable
commit before it is reported. Status coalescing may group a few milliseconds of adjacent changes.
Transcript segment upload state is resumable and need not block local transcript production.

## 6. Scheduling and backpressure

There is no global FIFO whose sequence number doubles as a scheduling decision. Each machine has
independent bounded queues for:

| Class | Reserved purpose | Admission and coalescing |
|---|---|---|
| Command receipts/control | Preserve effect truth | Reserved slots and bytes; never dropped |
| Interactive live deltas | Active status/conversation | Per-Session bound; replace older status |
| Status replication | Latest-value convergence | Coalesce by Session and revision |
| Transcript segments | History replication | Per-Session byte bound; resume by segment |
| Background repair | Reconciliation/compaction | Uses only surplus capacity |

A weighted fair scheduler or deficit round robin serves the classes, with reserved command and
interactive capacity. Large frames are chunked before admission, so one frame cannot monopolise a
socket write. Limits exist per account, machine, Session, class, and connection; the refusal names
which bound was reached and when retry is useful.

Ordering is `(stream_key, revision)`. Independent streams can progress concurrently. A missing
revision pauses only that stream and starts repair from its last durable cursor.

## 7. Relay and browser responsibilities

The relay is more than a live fan-out switch but remains content-blind. It provides:

- durable encrypted latest-value records for status heads and manifests;
- durable immutable encrypted transcript segments and heads;
- a durable encrypted command/job ledger with leasing;
- live subscriptions as an acceleration layer, not the source of truth;
- cursor-based resume and typed gap responses;
- partitioned quotas and admission independent of viewer presence.

The browser treats live messages as invalidations or deltas against durable state. On reconnect it
offers its last cursor. If the cursor is valid, Cloud sends the suffix; if not, Cloud returns
`cursor_expired` with the current encrypted head to refetch. A Durable Object eviction loses only
hot cache, never product data.

Read capability authorises status and transcript fetches. It does not imply `send_prompt` and does
not require publishing `ctl`. This lets a read-only phone work as a genuinely read-only device.

## 8. Observability is part of the protocol

Every user operation starts with a random, opaque `trace_id`. Content identifiers, plaintext,
paths, prompts, and ciphertext are excluded from telemetry. Each component emits structured stage
events with a stable vocabulary:

```text
trace_id, operation, plane, stage, outcome, error_code,
started_at, duration_ms, queue_ms, service_ms,
request_bytes, response_bytes, queue_items, queue_bytes,
stream_revision, retry_count, connection_generation, build
```

Required stages include:

- browser intent, local-cache result, fetch/subscribe start and finish, decrypt, reconcile,
  meaningful first paint, and final observation acknowledgement;
- relay admission, durable commit, queue/lease, object fetch, fan-out, cursor repair, and refusal;
- Mac receive/lease, local route, source I/O, parse/project, durable transaction, socket write,
  relay acknowledgement, and receipt settlement.

All failures and all operations beyond their SLO are retained. Successful fast traces may be
sampled, but histograms and counters are unsampled. Metrics include latency by plane/stage,
queue age/depth/bytes, per-class admission/refusal, segment sizes, status coalescing, cursor gaps,
lease expiry, duplicate suppression, stale-view age, and first-meaningful-paint latency.

The diagnostic logs added during this incident cover current-system blind spots: spool commit
duration/file population, outbound stored/terminal population, snapshot batch queue/work timing
with a batch id, status source read/parse/cache timing, and the durable-publication stage of each
read. They are safe and useful during migration, but they do not substitute for the cross-layer
trace above.

## 9. Service objectives and budgets

These are initial design targets to validate with production distributions, not claims about the
current system:

| User outcome | Target |
|---|---|
| Warm status first meaningful paint | p95 < 250 ms, p99 < 750 ms |
| Cold status first meaningful paint | p95 < 750 ms, independent of Mac connectivity |
| Cached conversation newest page | p95 < 300 ms |
| Cloud-fetched conversation newest page | p95 < 800 ms, p99 < 2 s |
| Command durable acceptance | p95 < 300 ms |
| Live delta after Cloud acceptance | p95 < 500 ms when viewer is connected |
| Cross-Session interference | one saturated Session adds < 100 ms p95 to another's status |

Each total has a budget for browser work, network, relay queue/service, object persistence/fetch,
decrypt, and paint. Alerting fires on budget exhaustion at the stage that owns it, not only on an
end-to-end timeout.

## 10. Failure isolation and mandatory proofs

The design is incomplete until these representative failures go red when their protection is
removed:

1. Mac offline: status and replicated conversation remain readable; command stays accepted or is
   typed `machine_offline`, according to its declared delivery policy.
2. Slow Mac fsync: command receipt commits remain bounded; browser reads are unaffected.
3. Hot Session publishing maximum transcript traffic: another Session's status stays within its
   latency budget.
4. Lost relay acknowledgement: publication resumes without duplicate state or effect.
5. Duplicate command delivery before and after Mac restart: one effect, replayed receipt.
6. Relay/DO eviction: durable heads restore service; no wait for a fresh full snapshot.
7. Expired cursor or missing segment: one stream repairs from a typed gap without resetting other
   streams.
8. Corrupt/truncated ciphertext: integrity failure is isolated to one record or segment and names
   the recoverable revision.
9. Browser crash after delivery but before observation: reopening finds the result and advances
   observed/acknowledged independently.
10. Partition at every state transition: the ledger never promotes accepted to executed, or
    delivered to observed, without the corresponding evidence.

Load tests must combine these failures with many Sessions; a fast single-path sample cannot prove
failure isolation.

## 11. What the target deliberately removes

The following are not concepts to optimise in v2:

- one RPC per status-line load;
- one whole-transcript response per conversation open;
- an active viewer as a prerequisite for retaining readable state;
- full Session snapshots after each periodic change;
- one global outbound sequence as both correctness order and scheduler;
- a whole-file JSON spool rewrite for enqueue, send, ACK, or cleanup;
- a synchronous read lane that waits through local computation and durable answer publication;
- a 60-second in-memory browser waiter as the command's source of truth;
- read behaviour coupled to the capability to publish a command;
- connection presence confused with durable delivery or human observation.

## 12. Migration without redefining the target around v1

Migration follows the target model; it does not preserve v1 abstractions inside v2:

1. **Measure v1.** Ship the incident instrumentation and establish stage distributions, byte
   volumes, and slow-trace exemplars.
2. **Introduce durable encrypted storage contracts.** Add versioned status, transcript segment,
   manifest, and command-job schemas plus quotas and retention. Keep v1 routing unchanged.
3. **Build the local projector and segmented outbox.** Backfill status heads and transcript
   segments from local canonical data, using per-stream revisions.
4. **Dual publish and shadow read.** Browser still renders v1 while verifying v2 completeness,
   digests, freshness, and latency without comparing different source revisions.
5. **Switch reads by plane.** Status first, then newest transcript page, then historical paging.
   Fall back only with a typed compatibility reason and telemetry.
6. **Switch commands to the job ledger.** Preserve request identities across both paths during the
   bounded compatibility window; never execute one logical command through both.
7. **Remove v1 read RPCs and global snapshot/spool machinery.** Do this only after Cloud clients
   outside the supported compatibility range have been refused or upgraded explicitly.

Each phase has a rollback reader for already-written data. No rollback requires deleting v2
records, and no cutover claim is accepted without failure injection and exact build/revision
evidence.

## 13. Decisions still requiring measured prototypes

The architecture does not depend on these choices, but implementation should settle them with
bounded experiments:

- Durable Object storage versus R2 plus a small indexed head store for encrypted segments.
- SQLite blob columns versus content-addressed files referenced by SQLite on the Mac.
- Exact segment byte/event ceilings and compression framing.
- Status-record granularity: one record per Session versus a small fixed-size shard.
- Retention policy and account byte quotas for transcript history.
- The scheduler's exact weights after measuring mixed interactive/background traffic.

The acceptance criterion for each experiment is an invariant and SLO above, not similarity to the
current implementation.
