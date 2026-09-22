> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.

# Cloud request architecture v2: reads are data, commands are jobs

Status: target design, derived from first principles. This document deliberately does not begin
with the current relay, spool, or request handlers. The current system appears only in the
diagnosis and migration sections, as evidence about failure modes and compatibility work.

The executable product sequence is [`cloud-mvp-plan.md`](cloud-mvp-plan.md). The MVP deliberately
does **not** implement the general replication design below. It uses one Cloud coordination store,
one exact-target durable inbox, and machine-owned execution state. This document remains a
first-principles analysis of failure semantics and a possible post-MVP direction; it is not an
implementation dependency or permission to add durable transcript, blob replication, automatic
routing, or general offline commands to the MVP.

## 1. The question this design answers

The product needs to let a person open Clawdline Cloud, see useful Session state, read a
conversation, inspect bounded facts that exist only on an executor, and send work to that
executor. Those are four different operations:

1. A **status view** is a small, latest-value projection.
2. A **conversation view** is a paged, append-only history plus live deltas.
3. A **command** is an asynchronous effect that may require an intermittently connected Mac.
4. A **live query** is an effect-free, bounded read whose source exists only on an online Mac or
   Linux/GCE executor.

They do not need the same consistency, storage, scheduling, or failure semantics. A correct design
therefore gives them separate data planes. Replicated reads never wait for an executor. A live
query fails immediately and explicitly when its executor is offline; it never masquerades as a
durable read or command. Background replication never consumes capacity reserved for an
interactive command or live query.

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

1. **Offline usefulness is policy-bounded.** When an injected replication policy explicitly
   enables a plane and scope, its latest successfully replicated status and conversation history
   remain readable without a live executor, with their age and completeness visible. Replication
   is default-disabled until the product decision gate in section 13 is closed; an excluded scope
   returns `not_replicated`, never an empty success.
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
11. **Stored-object authenticity is end-to-end.** Every durable object has a canonical signed
    header whose AEAD associated data binds schema version, account, machine, signer device and
    signing-key identity, plane, stream, optional command identity and precondition,
    CAS-granted writer epoch and generation, projector version, revision range, previous digest,
    key epoch, coarse generated time, and ciphertext digest. A hash chain proves ordering only
    when its signed head and local verified high-water also verify. Verification takes the
    requested stream identity and resolves only that machine/plane's authorised writer key;
    roster membership or possession of a different machine's writer key is insufficient.
12. **Key capability is cryptographic.** Account recovery material wraps per-plane, per-key-epoch
    data keys to currently authorised device public keys. The executor chooses the write epoch
    from its pinned local key ring and rejects a Cloud grant that names another epoch. Revocation
    rotates future writes. Data
    already decrypted or cached on an offline device cannot be recalled, and retention is the
    maximum continuing exposure window.
13. **The executor remains command authority.** Target, sender capability, idempotency identity,
    expiry, and precondition are signed end-to-end and rechecked against the executor's pinned
    roster and guarded clock. Cloud admission and deduplication are advisory only.
14. **An effect never silently replays.** Once `effect_started` is durable, redelivery can replay a
    receipt or return `effect_unknown`; it cannot execute the effect again. Cancellation is a
    separate command and can be `cancel_too_late`.
15. **Revisions are durable before publication.** A canonical source record obtains its revision
    in local transactional storage before any delta, tail, segment, or head is published. Source
    replacement or incompatible projection opens a new stream epoch and explicitly supersedes or
    resets the old one.
16. **Writer authority is fenced.** Ordering is by CAS-granted `(writer_epoch,
    projector_version, revision)`; `writer_generation` identifies the immutable grant within that
    epoch. Renewing a grant opens a new writer epoch rather than changing generation in place. An executor reads Cloud's durable
    high-water before writing; rollback, regression, an ungranted epoch, and a fork are typed
    failures rather than silent last-write-wins.
17. **Bulk bytes cannot block control.** Segments and blobs bypass control Durable Objects and the
    control socket. Command ledger, status/index control, live queries, and bulk transfer have
    separate partitions, connections, admission budgets, and failure domains.
18. **Every operation is classified exactly once.** A closed catalog assigns every bridge read to
    replicated status, replicated transcript, replicated blob, effect-free live query, command,
    or explicit unsupported. Adding an unclassified read is a compile/test failure.

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
Session identifiers. Every stored object uses the same signed-header contract; `first_revision`
and `last_revision` are equal for a latest-value status record:

```text
StatusRecord {
  header: {
    schema_version, account, machine, signer_device_id, signing_key_id,
    plane = status, stream_key,
    writer_epoch, writer_generation, projector_version,
    first_revision, last_revision, previous_digest,
    key_epoch, coarse_generated_at, ciphertext_digest
  }
  aead { nonce, ciphertext, tag }
  writer_signature
}
```

The header's RFC 8785 canonical bytes are both executable AES-GCM associated data and the
domain-separated Ed25519 signing body. The encrypted body's digest is computed before the GCM tag;
the tag authenticates that body against the header, while the signature authenticates both to a
reader without the plane key. Verification is called with the requested `(account, machine,
plane, stream_key)` and resolves only a writer key authorised for that exact identity. A roster
viewer or another Mac's writer key is rejected even when its Ed25519 signature is valid. Cloud durably replaces the record only when its complete fenced ordering key
advances and its grant matches the stream. A compact signed encrypted machine manifest contains
membership only: Session keys, bounded-retention tombstones, and current writer epoch. It changes
when a Session enters or leaves membership, not for every status revision. Cloud's content-blind
`stream_key -> high-water` index answers `GET since=cursor`; a cursor older than its retained
tombstones returns `cursor_expired`. A status checkpoint is therefore O(one small record), not
O(all active Sessions).

Live status deltas are in-memory acceleration and have a distinct budget from durable checkpoints.
Each account and Session has a maximum durable checkpoint frequency; semantic state transitions
and idle flushes force a checkpoint within the freshness objective. Staleness is derived from
three separate facts: the signed coarse `generated_at`, a signed machine-liveness checkpoint, and
relay-observed connection state. Browser display corrects wall-clock skew with relay server time;
an idle Session is not reported as an offline executor.

### 4.2 Transcript plane: immutable encrypted segments

A conversation is an append-only logical event stream, stored as bounded encrypted segments. A
segment contains a deterministic event range and is immutable after publication:

```text
TranscriptSegment {
  header: StoredObjectHeader(plane = transcript_segment,
    first_revision, last_revision, previous_digest, projector_version, ...)
  ciphertext
  writer_signature
}

TranscriptHead {
  header: StoredObjectHeader(plane = transcript_head, ...)
  ciphertext
  writer_signature
}
```

The relay/object store may index the opaque Session key, revisions, byte sizes, digests, and
retention time. Only authorised devices decrypt the segment. The hash chain detects omission or
reordering without exposing content.

The canonical event is a source-record identity and its original record bytes, not today's parsed
projection. Its identity includes the source file generation plus record UUID or durable byte
offset. Rendering, sidechain filtering, and other interpretation are versioned projections. A
parser-incompatible change, compaction, rewind, resumed replacement file, or snapshot restore
opens a new stream epoch and emits `stream_reset` or `supersedes`; it never reuses a
`(writer_epoch, revision)` position.

The initial conversation request fetches the encrypted signed head and newest one or two bounded
segments. Older history is cursor-paged. A history boundary explicitly returns `not_replicated` or
`retention_expired`; asking an online executor to backfill is a separately authorised command.
Live events are provisional until covered by either an immutable segment or a bounded,
latest-value signed `TranscriptTail`. A time/event coalescing ceiling and an idle deadline persist
the tail, so an executor going offline does not strand the newest events only in fan-out memory.
IndexedDB retains size-bounded verified encrypted segments for fast reopen and offline reading.
Revocation or logout erases its key and ciphertext stores; remote erasure of an already-offline
device is impossible and must be disclosed. Total history size never affects first paint.

The writer chooses a segment boundary using both byte and event ceilings. A provisional starting
point is 128 KiB or 200 events, whichever comes first; measurement should set final constants.
Compression is per event or an equivalent construction that cannot place attacker-controlled and
secret plaintext in one visible compression context. Ciphertext is padded to fixed size classes.
Oversized individual events become separately referenced, padded blobs rather than breaking the
segment bound.

### 4.3 Command plane: durable asynchronous jobs

Only an operation that must affect the Mac is a command. Submitting one creates a durable job and
returns its identity immediately:

```text
CommandJob {
  request: SignedStoredObject(plane = command_request,
    stream_key = digest(device_id, idempotency_key),
    command = { sender_device_id, idempotency_key, opaque_target_stream_key,
                command_type, quantized_not_after, full_observed_position_and_digest })
  accepted_at
  lease_generation
  receipts: SignedStoredObject(plane = command_receipt, same stream_key and command binding,
    chained receipt revision and previous_digest)
}
```

The browser receives `accepted` after Cloud has persisted the job, normally as HTTP 202 or its
WebSocket equivalent. A Mac leases the job, executes through a request-id ledger, and writes a
versioned executor-signed receipt. The browser renders effect truth only from a verified receipt;
Cloud-visible receipt state is scheduling metadata, not proof of an effect. The browser observes the job stream or fetches the job by id. Closing the tab,
reconnecting, or a 60-second network interval does not erase the outcome.

The ledger is a set of append-only facts, not one state field that conflates three observers:

```text
Cloud facts:       accepted, lease_generation, delivery_attempt
Mac receipts:      mac_durable_accepted, effect_started,
                   effect_completed | effect_failed | effect_unknown
Viewer facts:      observed(viewer_id), acknowledged(viewer_id)
Refusal outcomes:  expired, refused, precondition_failed,
                   cancelled_before_lease | cancel_too_late
```

The device creates and durably stores its idempotency key before first encryption. Browser retries
reuse the identical ciphertext bytes from IndexedDB; Cloud ciphertext dedupe is advisory. The
executor is the sole dedupe authority for `(device_id, idempotency_key)`, verifies signed target,
capability, `not_after`, and the full observed position using its pinned roster and guarded clock,
and rejects expiry before guarded now or `not_after > guarded_now + maximum_job_life` with distinct
typed outcomes. `not_after` uses the same 60-second quantum as stored-object generation time.
Startup refuses a configuration whose
dedupe retention is shorter than maximum job life plus allowed skew. A partial Cloud index write cannot create a
new executor identity. Transport is at least once, but after durable `effect_started` a lease
expiry permits only receipt replay or `effect_unknown`, never a repeated effect.

Ordering is per `(machine, session)` FIFO lease for commands that share a Session, in addition to
mandatory preconditions containing `(stream_key, writer_epoch, writer_generation,
projector_version, last_revision, ciphertext_digest)` or the prompt id the sender observed. Each command
type declares its delivery policy. Interactive answers have a short `not_after` and default to no
store-and-forward; whether other commands persist while offline remains behind the section 13
product decision gate.

### 4.4 Live-query plane and the closed read catalog

The rows below classify the **current machine bridge**. The MVP's canonical Cloud Board and
Schedule APIs are separate product resources, not a reinterpretation of these legacy reads.
Machine observations remain machine-authored facts after those resources exist.

A live query has read capability, an effect-free implementation, a bounded independent queue and
deadline, and no command-ledger entry. It is available only while its target executor is online;
otherwise admission immediately returns `machine_offline`. Results are neither durable replicated
content nor a fallback that silently publishes a command.

The protocol catalog is closed and is the source of `CloudAppBridge.readTypes`:

| Current bridge read type | v2 classification | Reason |
|---|---|---|
| `info` | replicated status | bounded Session projection |
| `transcript` | replicated transcript | immutable source-record stream plus tail |
| `image` | replicated blob | content-addressed transcript attachment |
| `agent`, `shell`, `skills`, `git`, `screen` | live query | bounded Session-local observation |
| `documents`, `document` | live query | scoped local document inventory/content |
| `board`, `timeline`, `places`, `project-worktrees`, `past-sessions` | live query | bounded machine-local query |
| `schedules`, `snippets`, `schedule`, `push-key` | live query | effect-free machine-local read |
| `project-worktree-lifecycle` | live query | reads an already cached lifecycle snapshot and never runs Git |
| `project-worktree-lifecycle-refresh` | command | performs a local observation, runs processes, and mutates the lifecycle cache |

The command-classified compatibility spelling stays explicitly classified during migration,
reads the executor's pinned roster, guarded clock, and remote-write gate at admission and again
immediately before its process-running cache mutation. It deliberately does not enter the command
ledger because the refresh is an idempotent observation; it is not admitted to the v2 effect-free
live-query lane. The catalog classification is an enforced admission check, not
descriptive metadata. A future removal must change the catalog to
`unsupported` before removing its bridge parser. Any newly added read type without exactly one
catalog row fails the focused protocol suite. In addition, `orch/` machine snapshots are
replicated status; `ho/` handoff payloads are target-addressed bulk transfer; and `ctlr/` command
answers are command receipts. None may fall into a global compatibility spool.

### 4.5 Typed availability and integrity gaps

Successful empty data is never used to represent an availability or history gap. The common typed
surface includes:

- `machine_offline`: an online-only live query or no-store-and-forward command has no executor;
- `not_replicated`: injected policy did not enable this plane/scope, or history was never backfilled;
- `retention_expired`: the requested history existed but is older than the retained boundary;
- `cursor_expired`: the change cursor or tombstone evidence is no longer retained, with a current
  signed head from which to restart;
- `repairable_gap`: an immutable segment or receipt reader missed a predecessor and must fetch the
  missing range before advancing;
- `replication_quota_exhausted`: the account/stream history write or byte budget is exhausted.

Quota exhaustion sheds transcript history and bulk backfill first. It does not consume or disable
reserved status and command capacity. Integrity failures are distinct: `rollback_detected`,
`revision_regressed`, `writer_epoch_conflict`, and `stream_fork` preserve the last verified object
and name the rejected stream position.

## 5. Persistence on the executor

The Mac and Linux/GCE executor use the same transactional append-oriented contract, implemented
with SQLite in WAL mode (including an explicit linked SQLite choice for the self-contained Linux
binary), not a JSON array rewritten and fsynced for every transition. Separate tables hold:

- status heads and pending projection revisions;
- transcript segment metadata and upload state;
- command inbox, leases, effect ledger, and receipts;
- per-stream cursors and acknowledgements;
- an outbox with payload references rather than repeated payload copies.

A revision allocation, canonical source identity, writer grant, and pending publication enter one
transaction before bytes can reach a live or durable transport. At startup the executor reads the
Cloud high-water and reconciles it with local state. A local regression returns
`revision_regressed` and requires a new CAS writer epoch; restoring a VM/Mac snapshot, cloning a
machine identity, or observing two writers can never continue under the old grant.

Enqueue, lease, settle, and acknowledgement are bounded indexed transactions. Terminal rows are
compacted or expired in background chunks and are never scanned on an interactive write.
Ciphertext blobs may live in content-addressed files; the database transaction owns their
reference and publication state.

Durability policy is class-specific. An accepted command or effect receipt crosses a durable
commit before it is reported. Receipt queue rows are a bounded view of durable truth: the number
of outstanding receipts is bounded by command admission, and they are not dropped from the ledger
when an in-memory queue is full. Live status coalescing and durable checkpoint coalescing have
separate ceilings. Transcript segment upload state is resumable and does not block local
transcript production.

## 6. Scheduling and backpressure

There is no global FIFO whose sequence number doubles as a scheduling decision. Each machine has
independent bounded queues for:

| Class | Reserved purpose | Admission and coalescing |
|---|---|---|
| Command receipts/control | Preserve effect truth | Reserved slots and bytes; never dropped |
| Interactive live deltas | Active status/conversation | Per-Session bound; replace older status |
| Live queries | Online-only, effect-free reads | Per-machine and per-query deadline; fail offline |
| Status replication | Latest-value convergence | Coalesce by Session and revision |
| Transcript segments | History replication | Per-Session byte bound; resume by segment |
| Targeted bulk transfer | Handoff and large blobs | Explicit target; chunked data connection |
| Background repair | Reconciliation/compaction | Uses only surplus capacity |

A weighted fair scheduler or deficit round robin serves the classes, with reserved command and
interactive capacity. Large frames are chunked before admission, so one frame cannot monopolise a
socket write. Limits exist per account, machine, Session, class, and connection; the refusal names
which bound was reached and when retry is useful.

Ordering is `(stream_key, writer_epoch, projector_version, revision)`. A writer generation is
fixed within its epoch; lease renewal allocates a higher writer epoch, so a same-epoch generation
change is a typed conflict rather than an ambiguous in-place ordering change.
Independent streams progress concurrently. A missing revision pauses only that stream and starts
repair from its last durable cursor.

Topology enforces the scheduling promise. A Worker authorises content-addressed segment/blob PUTs
after the index atomically reserves byte and write quota; the bulk bytes then flow directly to R2,
not through an AccountDO event loop. Durable Objects contain only bounded heads, indexes, grants,
and ledgers. The command ledger is isolated from transcript indexes (at least per account/machine,
or equivalently stronger sharding), and the executor's control connection for commands, receipts,
status, and live-query control is separate from HTTP bulk upload/download. R2 failure cannot block
command acceptance; command-DO failure cannot corrupt already verified R2 objects.

Every account has explicit persistent-write and stored-byte budgets. A per-Session checkpoint rate
bound prevents high-frequency status from turning into unbounded durable rows. The budgets and
storage topology are contract constraints, not prototype details, because cross-Session latency,
privacy exposure, and cost depend on them.

## 7. Relay and browser responsibilities

The relay is more than a live fan-out switch but remains content-blind. It provides:

- durable encrypted latest-value records for status heads and manifests;
- durable immutable encrypted transcript segments and heads;
- a durable encrypted command/job ledger with leasing;
- live subscriptions as an acceleration layer, not the source of truth;
- cursor-based resume and typed gap responses;
- partitioned quotas and admission independent of viewer presence.

Before accepting a writer, Cloud performs a compare-and-swap over the durable stream high-water
and returns a grant containing writer epoch, immutable lease generation, projector version, and an
echo of the executor's proposed key epoch. Before encryption, the executor compares that echo with
its locally pinned per-plane key ring; a mismatch is `key_epoch_conflict`. Every stored object's
signed header must match that grant. Durable high-water includes key epoch and rejects a decrease.
A stale lease, ungranted higher epoch,
same position with another digest, or old epoch is rejected with the corresponding typed failure.
Within one transcript-segment epoch, the next immutable range begins at exactly
`previous.last_revision + 1`; latest-value status, head, and tail checkpoints may coalesce skipped
intermediate revisions, while a writer CAS still binds the previous accepted digest. Reader
checkpoint validation is deliberately different: a latest-value reader may miss an intermediate
checkpoint and advance by verified identity and monotonic position without possessing its immediate
predecessor. A chained transcript or receipt gap returns `repairable_gap`, not the permanent
`stream_fork` integrity verdict. Thus a generic “greater than” comparison cannot hide a missing
transcript range. The browser independently persists its highest verified position keyed by the
requested stream, passes that expected identity into signature and read validation, retains a
per-plane revoked-key floor, and refuses a lower or substituted response even if Cloud serves it.

The browser treats live messages as invalidations or deltas against durable state. On reconnect it
offers its last cursor. If the cursor is valid, Cloud sends the suffix; if not, Cloud returns
`cursor_expired` with the current encrypted head to refetch. A Durable Object eviction loses only
hot cache, never product data.

Read capability authorises status and transcript fetches. It does not imply `send_prompt` and does
not require publishing `ctl`. This lets a read-only phone work as a genuinely read-only device.
Per-plane/per-epoch data keys are wrapped only to roster devices with that plane's capability.
Revocation rotates the affected plane for new writes. New devices receive only the retained epoch
key ring their capability and product policy permit; deleting an epoch/session key is the
crypto-shredding boundary for stored ciphertext.

## 8. Observability is part of the protocol

Every user operation starts with a random, opaque `trace_id`. The trace id is Cloud-visible
metadata because it crosses browser, relay, and executor, but it is randomly generated per
operation and is not a content identifier. Plaintext, paths, prompts, ciphertext, stable content
identifiers, and unhashed local stream names are excluded from telemetry. Each component emits
structured stage events with a stable vocabulary:

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

The v2 durable metadata disclosure is explicit and must be copied verbatim into the public
protocol and security policy before storage ships: opaque account, machine, and stream keys;
plane; signer device id and signing key id; writer/key/projector epochs and lease generations;
first/last revision; previous and ciphertext digests; padded ciphertext size class; 60-second
quantized generated, updated, and expiry times; manifest membership and retained tombstones; and
operation-scoped `trace_id`. A command additionally exposes sender device id, idempotency key,
opaque target Session stream key, command type, quantized `not_after`, the full precondition
(account, machine, plane, stream key, writer epoch/generation, projector version, last revision,
and ciphertext digest), accepted time, delivery attempt, and receipt state. This metadata can
reveal activity timing, approximate event counts, Session lifecycle, device correlation, command
kind and command attempts even without plaintext. Transcript size is padded, and
retention/deletion applies to metadata indexes as well as ciphertext.

Browser and executor stage events flow to a separately access-controlled telemetry sink through
bounded batches; relay stages are recorded at the serving component. Product policy must set sink
retention and operator access before launch. An opaque stream key may appear for within-stream
correlation; a local Session id or path may not.

The diagnostic logs added during this incident cover current-system blind spots: spool commit
duration/file population, outbound stored/terminal population, snapshot batch queue/work timing
with a batch id, status source read/parse/cache timing, and the durable-publication stage of each
read. They are safe and useful during migration, but they do not substitute for the cross-layer
trace above.

## 9. Service objectives and budgets

These are initial design targets to validate with production distributions, not claims about the
current system:

| User outcome | Target | Required dependencies and budget boundary |
|---|---|---|
| Warm status first meaningful paint | p95 < 250 ms, p99 < 750 ms | browser cache, key availability, decrypt, paint |
| Cold status first meaningful paint | p95 < 750 ms | DNS/TLS, control-plane token or cached valid token, JWKS availability, status DO/index, object fetch, decrypt, paint; independent of Mac connectivity only |
| Cached conversation newest page | p95 < 300 ms | IndexedDB, key availability, verification, projection, paint |
| Cloud-fetched conversation newest page | p95 < 800 ms, p99 < 2 s | index/head, R2, decrypt/verify/project/paint |
| Command durable acceptance | p95 < 300 ms | auth/JWKS and isolated command-ledger commit; no executor dependency |
| Accepted command to lease | p95 < 500 ms while executor is connected | command DO, control socket, executor admission |
| Lease to first durable Mac receipt | p95 < 500 ms excluding declared command execution | control socket and executor SQLite fsync |
| Executor status event to Cloud durable checkpoint | p95 < 2 s, p99 < 5 s | projector, local transaction, control index/store; measured before Cloud acceptance too |
| Sealed transcript tail to Cloud durable head | p95 < 5 s, p99 < 15 s | local transaction, bulk PUT, index publish |
| Live delta after Cloud acceptance | p95 < 500 ms when viewer is connected | fan-out and browser path |
| Cross-Session interference | one saturated Session adds < 100 ms p95 to another's status or command acceptance | isolated DOs, connections, quotas, scheduler |

Each total has a budget for browser work, network, relay queue/service, object persistence/fetch,
decrypt, and paint. Alerting fires on budget exhaustion at the stage that owns it, not only on an
end-to-end timeout.

Availability, durability, freshness, and lag receive separate measured objectives and error
budgets before launch. At minimum, command-ledger durability, replicated-read monthly
availability, checkpoint-age distribution by plane, and writer backlog age are reported
independently. A cold-DO JWKS miss, control-plane outage, R2 outage, and executor outage consume
different budgets and return different typed errors; “independent of Mac” never means independent
of those other dependencies.

## 10. Failure isolation and mandatory proofs

The design is incomplete until these representative failures go red when their named protection
is removed. Every executable proof records the exact guard, comparison, transaction, partition,
or capability check whose deletion makes the test fail; a presence-only assertion is not a proof.

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
11. Rotate and revoke a key epoch: authorised retained objects remain readable, revoked devices
    cannot read new writes, a Cloud grant cannot select an older local key, and removing either
    the pinned-epoch comparison or read-side revoked-key floor makes the proof red.
12. Store substitution, replay, and rollback: mutate every signed identity/order field using a
    resolver that still returns the same key, exchange authentic objects across plane/stream on
    first and later reads, serve lower high-water, and reuse a revision; canonical signature,
    expected-stream binding, authorised-writer lookup, and high-water guards reject each case.
13. Lease expiry after durable `effect_started`: redelivery returns the receipt or
    `effect_unknown`; removing the no-reexecute ledger guard produces a second effect.
14. Two commands for one Session lease out of order or against a changed signed status position:
    FIFO lease/full-position precondition returns `precondition_failed`; a same numeric revision
    in a new writer epoch does not satisfy it. Removing either guard exposes the stale effect.
15. Compaction, parser upgrade, source replacement, and rewind: a new stream/projector epoch is
    required, and deleting the epoch comparison produces a fork. An executor going offline with
    an unsealed tail still leaves the last forced tail checkpoint readable.
16. Restore the local DB or VM snapshot, clone the machine identity, and connect two writers: CAS
    fencing returns `revision_regressed` or `writer_epoch_conflict`; each grant field, epoch-advance
    generation/digest rule, same-epoch generation, projector direction, and stream identity has a
    negative test. Removing the grant match accepts a stale writer.
17. Saturate one Session's R2 segment uploads while another submits commands and status: isolated
    DOs and connections preserve their SLO. Running bulk bytes through either control path makes
    the test exceed budget.
18. Exhaust the account history write/byte budget: history returns
    `replication_quota_exhausted` while status and commands continue; removing class-reserved quota
    causes collateral refusal.
19. Stop the control plane or deny a cold DO fresh JWKS: cached-valid and cold-auth paths report
    their specified availability outcome without accepting an unverifiable token.
20. Make R2 unavailable while DOs work, then make command/status DOs unavailable while R2 works:
    failures remain plane-local and already verified content is neither lost nor promoted.
21. Exercise quantized `not_after` immediately inside and outside maximum job life and below
    guarded now: the executor guarded clock, not Cloud wall time, decides expiry with distinct
    `command_deadline_invalid` and `command_expired` outcomes; overflow is rejected, and startup
    also rejects dedupe retention shorter than maximum job life plus allowed skew.
22. Downgrade during dual command compatibility: the version gate or dual-written dedupe ledger
    prevents an already-started v2 effect from executing through v1.
23. Give an old browser a new schema: it returns typed `unsupported_schema` and retains its last
    verified high-water instead of interpreting unknown fields.
24. Disable replication, omit backfill, expire retention, and expire a cursor: each returns its
    distinct typed gap and none renders an empty history as complete.
25. Classify the complete bridge read catalog: each read has exactly one row, and the mutating
    `project-worktree-lifecycle-refresh` spelling cannot enter the effect-free live-query lane.
26. Sign a machine stream with a valid viewer key and another Mac's valid writer key; both fail
    authorised-writer lookup. Forge an `effect_completed` command receipt with the sender key;
    only the target executor writer can produce effect truth.
27. Admit lifecycle refresh, revoke its roster/clock/gate authority while it waits, then release
    the lane; the router remains untouched. Under ingress pressure only the synchronous write-gate
    refusal retains precedence over a retryable capacity answer; an admitted refresh still performs
    the complete asynchronous roster, guarded-clock, and gate checks at both effect boundaries.

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
- a synchronous read lane for data that already has a replicated v2 representation;
- a 60-second in-memory browser waiter as the command's source of truth;
- read behaviour coupled to the capability to publish a command;
- connection presence confused with durable delivery or human observation.

## 12. Migration without redefining the target around v1

Migration follows the target model; it does not preserve v1 abstractions inside v2:

1. **Measure v1.** Ship the incident instrumentation and establish stage distributions, byte
   volumes, and slow-trace exemplars.
2. **Introduce policy-independent contracts, default-disabled.** Add signed versioned status,
   transcript tail/segment/head, membership manifest, writer-grant, and command-fact schemas plus
   injected replication policy. Do not store content until the section 13 product gate sets
   retention, consent, deletion, metadata, and store-and-forward policy. Keep v1 routing unchanged.
3. **Build the local projector and segmented outbox.** Backfill status heads and transcript
   segments from canonical source records, using transactional per-stream revisions. From its
   first release, the v2 outbox is a separate actor, store, scheduler, and connection; backfill
   uses only surplus background-repair capacity. Apply the same contract to Mac and Linux/GCE
   executors, including snapshot-restore fencing.
4. **Dual publish and shadow read.** Browser still renders v1 while verifying v2 completeness,
   digests, freshness, and latency without comparing different source revisions. Dual publication
   never passes through the v1 spool/control connection.
5. **Switch reads by plane.** Status first, then newest transcript page, then historical paging.
   Fall back only with a typed compatibility reason and telemetry.
6. **Switch commands to the job ledger.** Preserve request identities across both paths during the
   bounded compatibility window; never execute one logical command through both. Either dual-write
   compatible dedupe facts or refuse app downgrade across this boundary with a typed version gate.
7. **Remove only mapped v1 reads.** Remove a v1 read RPC only after its catalog entry has a shipped
   v2 replacement and Cloud clients outside the supported compatibility range have been refused or
   upgraded explicitly. Live queries remain bounded executor reads; command/unsupported entries
   are never silently removed as “reads.” Global snapshot/spool machinery leaves only after
   `orch/`, target-addressed `ho/`, and `ctlr/` have their declared classes.

Each phase has a rollback reader for already-written data. Rollback normally preserves valid v2
records, but opt-out, deletion/crypto-shredding, a bad immutable projection, and a superseded stream
are explicit exceptions with auditable deletion or supersession. No cutover claim is accepted
without failure injection and exact build/revision evidence.

## 13. Product decision gate and measured prototypes

Durable Cloud content and offline command delivery remain disabled until the product owner records
one decision covering all of the following. Every technical signing entry requires an explicitly
injected replication policy and has no permissive default; `disabled` returns `not_replicated`
rather than guessing.

- opt-in/opt-out default and per-project/per-Session exclusions, including NDA-sensitive work;
- retention by tier for status, transcript, blobs, metadata, keys, and tombstones;
- deletion timing and crypto-shredding semantics, including backups and the impossibility of
  remotely clearing an offline device that already cached data;
- the complete Cloud-visible metadata disclosure and timestamp/size-padding policy;
- command-type store-and-forward defaults, maximum job lifetime, and dedupe retention.

Closing this gate requires simultaneous updates to `DECISIONS.md`, `PROTOCOL.md` security and
metadata sections, and the public `security.astro` wording. No prototype may silently decide these
product promises.

The following implementation constants still require bounded measurement inside the fixed
topology and policy constraints:

- SQLite blob columns versus content-addressed files referenced by SQLite on the Mac.
- Exact segment byte/event ceilings and compression framing.
- Status-record granularity: one record per Session versus a small fixed-size shard.
- The scheduler's exact weights after measuring mixed interactive/background traffic.
- Durable checkpoint frequency and padding size classes within the policy's hard write/byte budget.

The substrate topology is not optional: bulk content goes directly to R2 after quota reservation;
bounded heads/indexes/grants/ledgers use isolated Durable Objects; command and transcript index
work do not share one account event loop; and executor control and bulk connections are separate.
The acceptance criterion for each remaining experiment is an invariant and SLO above, not
similarity to the current implementation.
