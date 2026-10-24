# Connection health administration

Status: product plan, 2026-10-20. No monitoring implementation or production rollout is implied.

The product should let an operator open one page, see which user operation is blocked, slow or
failing, establish when it began, then give an agent enough evidence to find the last progressing
program stage in the exact affected build. This is the acceptance target, not a log-search page.

The accompanying [panel design](admin-connection-health-design.md) and
[interactive preview](admin-connection-health-preview.html) describe the interface. Preview values
are illustrative fixtures, never measurements. Thresholds and budgets below are proposed defaults
until pressure tests establish their cost and coverage.

## Scope and product boundaries

The first production version covers the browser, Cloud API, relay Worker and account execution,
relay sockets, the machine daemon, its application request lanes, the direct carrier, and the
monitoring system itself. Include read, send/control and status-subscription paths separately.
A healthy direct read cannot establish healthy command delivery over the relay.

This is a read-only inspection product. It does not retry commands, restart machines, discard
queues, change pairing, infer why a machine is offline, or add sleep prevention. Historical
observations are not offline command delivery: no queued user control payloads enter telemetry.
Normal work remains independent of monitoring availability and telemetry budgets.

The single-machine open-source product gets local health, journal, watchdog, query and evidence
export. Cloud adds remote history, independent service observations and the cross-source view.
Service-operator administration and an account owner's paired-machine diagnostics are separate
permissions, even when the same person holds both. Existing business administration stays intact.

## What exists, and what it does not establish

The initial source review used the product tree at `d2ed0c063`; locations below must be refreshed
when implementation starts. They describe source behavior, not proof of the deployed incident cause.

| Existing evidence | Source | Limit |
| --- | --- | --- |
| Admin loads independently of machine CloudGate and relay | `web/console/src/cloud/admin/AdminGate.tsx:16` | Preserve this failure independence. |
| Operations overview, accounts, usage and promotions | `web/console/src/cloud/admin/api.ts:62`; `words.ts:17` | No connection-health or incident contract. |
| Per-browser direct-attempt history | `web/console/src/cloud/direct-history.ts:13`; `pages/devices/AccountMachines.tsx:293` | Browser-local, 24 hours; not durable machine/Cloud history. |
| Carrier state | `web/console/src/cloud/carrier-state.ts:4` | Read carrier; command/status paths may differ. |
| Socket state and lifetime counters | `internal/adapters/cloud/status.go:27` | Connected is not application progress; no persisted stage timeline. |
| Synchronous socket read and frame handler | `internal/adapters/cloud/transport.go:445` | A stuck callback can prevent the next read, outside the next socket deadline. |
| Sender lookup, verification and callback | `internal/adapters/cloud/transport.go:675` | Needs separate active phases before each dependency. |
| Shared WebSocket writer lock | `internal/adapters/cloud/websocket.go:292` | Write deadline is set after taking the lock; lock wait needs separate observation. |
| Eight read slots and bounded command lane | `internal/transport/cloud/link.go:1082` | Occupancy alone does not name each holder or its age/stage. |
| Spool capacity, receipts and loss readings | `internal/adapters/cloud/spool.go:956` | Mutex-backed normal snapshot cannot be the watchdog's only evidence. |
| Route duration samples and totals | `internal/transport/http/route_stats.go:18` | Completed-request samples omit a request that never finishes; recent samples are not windowed SLO data. |
| Local diagnostics and Cloud status | `internal/transport/http/server.go:722`; `cloud.go:3` | Raw local-only endpoints must not simply be opened to remote viewers. |
| Content-free direct attempt trace | `internal/transport/cloud/direct_trace.go:3` | Useful correlation model; not an end-to-end request history. |

The Cloud source review found an existing superuser operations overview, metadata-only bounded
refusal logging, process/repository health checks and deployment-request progress semantics.
None provides durable end-to-end connection health. Relay fanout currently means a socket send
was accepted, not that a viewer decrypted/applied the response. Cloud service implementation
references remain in its private engineering record; this public plan publishes the cross-service
contract, not private repository paths or customer identifiers.

The incident motivating this work had an observed receipt gap followed by recovery without a
recorded daemon restart. That establishes a gap in observations; it does not identify whether the
reader, a callback, writer, network or relay was responsible. Do not label a fixture as a replay
of a proven root cause.

## Information architecture

Keep stable screen IDs in review and future feedback.

| ID | Screen | Main question and required content |
| --- | --- | --- |
| M01 | Connection health | Who is affected now, where, since when, and how trustworthy is the evidence? Active incidents, path map, request success/error/timeout counts, slow operations, oldest pending work, source freshness. |
| M02 | Machine connections | Presence, daemon build/boot, each viewer and carrier, read/control/status paths, queue holders, replacement/reconnect chain and shared timeline. |
| M03 | Cloud services | API/dependencies, relay admission/account stages/socket writes, instance/build, saturation and independent probe viewpoint. |
| M04 | Request trace | The exact logical operation, attempts, last proven stage, current stage age, queue/lock/handler/write/receipt timing and build-specific code location. |
| M05 | Incident investigation | Impact, interval, evidence, supported hypotheses, missing evidence, recovery proof and a frozen investigation bundle. |
| M06 | Monitoring status | Producer/ingest/query freshness, gaps, dropped/suppressed records, upload backlog, clock uncertainty, quotas, retention floor and schema compatibility. |

Proposed paths are `/admin/health` plus machine, service, trace, incident and telemetry subroutes.
Owner routes provide only account-scoped views; linking from Devices never grants superuser rights.
Every route restores its time window, scope and selected incident using opaque identifiers, with
no credentials or content in URLs. Unauthorized deep links return a typed refusal without a data flash.

Desktop starts with current user impact and an always-visible data-quality strip, then the path
map and synchronized timeline. Mobile starts with the same impact and freshness facts, problem
cards and a compact path map; waterfall stages become vertical. Charts have readable tables,
keyboard access, visible focus and text/icon status alternatives. No required hover interaction.

All live interface copy ships in English and Taiwan Traditional Chinese together. Use existing
admin visual tokens. Example mode is visibly labeled on every view and every export; it cannot
be confused with production data or real incident evidence.

## Health rules are evidence predicates

Show two dimensions: operational outcome and evidence quality. Do not compress both into a
single uptime score or a global green socket badge.

| Outcome | Evidence required | What must remain distinct |
| --- | --- | --- |
| Healthy | Fresh observations for the named path and operation; recent completed work meets its target. | Zero traffic is not 100% success. Socket-open only proves transport state. |
| Slow | A completed operation exceeds its class target, or a valid window percentile exceeds the rule with enough samples. | Show sample count, window, timeout count and incomplete operations separately. |
| Blocked | Identified in-flight phase exceeds its declared deadline/no-progress rule. | Full queue alone means saturation; show age and progress before claiming a stall. |
| Error | An attributed typed failure/refusal for a named operation or stage. | Successful relay fallback can mean direct degradation, not total operation failure. |
| Offline | An authenticated machine presence contract expired; retain the source and expiry evidence. | A direct socket close only establishes that path unavailable/unknown. Do not infer sleep, power or network cause. |
| Unknown | Evidence missing, stale, contradictory or unsupported. | Never fill gaps with zero or the previous green status. |

Evidence quality is `complete`, `partial`, `stale`, `not_collected`, `unsupported`, or `forbidden`.
No activity in a freshly observed interval is `no_requests`, not missing data. Stale values remain
visible with their as-of time and an explicit unknown-current-state treatment. Recovery requires
new progress/success evidence; quiet errors alone leave recovery unverified. Render the exact rule
version, triggering stage/deadline and evidence age in detail.

Latency charts use windowed histograms/counters with denominators. Do not average percentiles
across machines, exclude timeouts silently, or count retries as distinct logical user operations.
Show attempt counts separately. If sampling cannot establish an affected count, label a lower
bound or sampled population. Empty latency is unavailable, never zero milliseconds.

## Correlated signal contract

Publish versioned schemas before implementations. Required common metadata:

- Schema/event version, event ID, producer kind, source instance/boot ID and increasing producer sequence.
- Opaque tenant/machine/viewer scope derived from authenticated identity; component and stable location code.
- Logical trace ID, attempt ID, span/parent ID, connection ID/generation and superseded connection ID.
- Source UTC timestamp, source monotonic elapsed duration, collector received UTC, clock offset estimate/uncertainty.
- Build commit, platform, instrumentation version, stage/outcome/code, carrier and route/operation enum.
- Current/maximum queue rows/bytes, oldest age, stage start/last progress, declared deadline and active slots.
- Sampling decision, suppression/drop counts, retained sequence range, truncation and coverage gaps.

IDs are correlation, not authority or idempotency keys. Retry retains the logical request identity
but creates a distinct attempt; never blindly resend a mutation because its answer was lost.
High-cardinality IDs belong in bounded events, not metric labels. Metric dimensions use finite
component/operation/carrier/outcome enums. Sensitive route/query/path/channel values are not labels.

The lifecycle vocabulary distinguishes client issued, Cloud accepted, lane queued, handler
started/completed, response sealed, spool queued, writer-lock waiting, socket-write started/ended,
relay socket accepted/forwarded, endpoint received/decrypted, UI applied and explicit acknowledgment.
Only an actual receipt creates each state. A browser applying data is not human acknowledgment.
A direct operation has no invented relay span. State transitions remain meaningful across mixed versions.

Measure local durations with monotonic clocks. Causal IDs/sequences establish cross-process order;
clock calibration gives an uncertainty interval, not exact one-way network time. Display uncertainty
bands and source/collector times. Restarts and wall-clock jumps cannot create negative elapsed time
or silently join unrelated boots. Late evidence revises an incident with provenance; it must not
pretend it was available during the outage.

## Instrumentation and independent progress capture

### Daemon

Instrument before and after frame read, public-key/roster lookup, verify/decrypt, inbound callback,
read/command admission, handler work, bridge dependency, spool reservation/seal/settlement,
writer-lock acquisition, socket write and receipt callbacks. Instrument direct setup/replace/refuse/
close, carrier chunk assembly and SSE cursor/resume/gap. Use existing content-free ICE diagnostics;
never record SDP, addresses, credentials or user payloads.

Each risky finite operation publishes a bounded immutable active-stage snapshot via atomics before
entering the dependency. Include holder, correlation, start, last progress and deadline. Every
completion updates a histogram/counter; bounded normal traces are sampled, failures and slow/stall
transitions get reserved evidence capacity with explicit overflow counts. Reserve a finite set of active slots for reader, writer, drain, keepalive and watchdog progress;
business operations cannot exhaust them. Business-slot overflow records interval, operation class
and counts, changes trace coverage to partial, and forbids exact per-operation claims for those
unrecorded spans. No instrumentation caller waits for telemetry disk/network I/O or allocates an
unbounded queue.

A dedicated watchdog reads only these atomic snapshots and its own timer. It must not call the
normal logger, live Status/Spool snapshot or business handler, or acquire the lock it is diagnosing.
When progress exceeds a stage rule, mark the incident atomically and emit a bounded emergency
record. A separate capture worker can take one capped stack snapshot; the watchdog never waits
for capture, disk persistence or upload. Limit capture to one in-flight job, a 1 MiB raw buffer,
a 512 KiB sanitized export and a proposed 2-second completion budget. If the runtime cannot cancel
a stuck capture, abandon waiting and mark it unavailable; do not spawn another worker until it
finishes. Measure capture CPU/pause behavior before enabling it. Freeze a bounded pre/post window
and record recovery duration independently of capture success. Raw stacks stay
local; exports sanitize function/frame locations and any paths/values. Stack collection and full
profiling are distinct: block/mutex profiling is a bounded opt-in diagnostic session, not always-on.

**Stall capture (implemented).** The capture worker above is built as approved: one worker, one
capture in flight, a 1 MiB raw buffer that stays in memory and is never exported or persisted, a
512 KiB sanitized capture, a 2-second budget after which the worker reads unavailable and no
replacement starts, and at most four captures a minute. Its measured cost (100 goroutines: 5.7 ms
per capture, at most 1.3 ms stop-the-world; 5,000 goroutines: 36.6 ms, at most 29 ms) is small
for something that runs only after a 15-second stall, so it is on by default and
`CLAWDLINE_STALL_CAPTURE=off` turns it off. The local export joins each capture to the location
map's function, the exact `file:line` in this build and the stall's recovery; the owner receives
the same join and the stalled goroutine's frames as a later sealed revision. The contract,
sanitizing rules and the full measurement are in `docs/admin-connection-health-contract.md` §4.2.

This watchdog cannot survive total process suspension, scheduler starvation or host failure. Cloud
presence and application probes supply another viewpoint; any separately shipped supervisor must
have an explicit cross-platform lifecycle. A private helper on one developer machine is not the product.

### Cloud and viewer

API records admission, authentication, handler/dependency stages and completion; distinguish Mongo,
service fetch and response-write waits. Relay records outer Worker admission, account-handler
entry/exit, storage/dependency waits, socket lifetime and fanout/refusal outcome. Outer response
completion must be distinct from handler return, including storage output-gate delays. A service
that responds to liveness can still have a stuck account-specific data path.

Viewer records connection/reconnect/backoff, carrier replacement, logical request phases,
decrypt/apply exceptions, snapshot freshness and SSE gaps. Its local diagnostic buffer survives a
navigation/reload only under an explicit bounded retention contract. It can show its own current
connectivity even when upload fails. Existing diagnostics.events remains useful but cannot be the
only route: the relay may be the component that is failing.

### Monitoring must not follow the failed path

Use a dedicated bounded HTTPS ingest path, separate connection/client, queue and deadline from the
business relay and writer lock. Cloud queries return stored observations without synchronously
contacting the selected machine or blocked account handler. Machine offline therefore still yields
Cloud facts and the last owner-encrypted snapshot, each with its real age.

Cloud API failure cannot be diagnosed solely by that API. Keep the admin shell independent of
machine CloudGate; show a failed query, local browser probe viewpoint and last confirmed data.
Independent external-to-process probes should persist bounded outcomes outside the failed process.
Same-host probes do not prove host/network availability. A fully independent Cloud probe/storage
fault domain is an explicit production milestone with cost/lifecycle acceptance, not an implied
promise that the dashboard is always reachable.

## Storage, retention and budgets

Local structured journal uses a separate bounded append worker and segments, checksummed framing,
boot markers and a documented crash-loss window. Do not share application DB writer locks or
require normal per-event fsync. Publish write errors, disk-full/read-only state, dropped counts and
actual retention cutoff. An emergency reserve is bounded too; if disk is unavailable, memory and
independent Cloud evidence state their durability limits.

Cloud stores separate latest summaries, routine events, minute aggregates and incident bundles.
Freeze selected incident evidence so raw TTL pruning cannot invalidate the bundle. Export has a
manifest/hash, immutable evidence revision, source coverage, expiry and later-evidence provenance.
Security audit has its own retention and cannot be silently sampled/expired as routine telemetry.

Proposed limits below must be registered, exposed and pressure-tested before release. Time or byte
cap, whichever comes first, determines retention; the UI must not promise seven days if storage
retained less. The exact Cloud schema/index overhead and compressed/uncompressed limits need measurement.

| Resource | Proposed starting bound | At the bound |
| --- | --- | --- |
| Local ring | 4,096 records / 4 MiB | Evict oldest routine data; count loss. |
| Active operations | 128 bounded slots | Count overflow; keep aggregate occupancy and never allocate freely. |
| Collector queue | 1,024 records / 2 MiB | Shed normal detail first; reserve bounded anomaly records. |
| Local detail | 7 days / 100 MiB; 8 MiB segments | Prune sealed oldest; show actual floor. |
| Minute aggregates | 30 days / 20 MiB locally | Compact and prune oldest. |
| Captured incidents | 20, 10 MiB each, 100 MiB total | Eviction policy exposes a tombstone; active/pinned quota is finite. |
| Watchdog | 1-second cadence, per-stage operation deadlines | Record scheduling gaps; exclude healthy long-lived streams. |
| Initial finite-operation warning/block | 5 / 15 seconds, overridden by operation contract | Explicit proposed heuristic, not a measured SLO or proof of deadlock. |
| Stack evidence | One capture per transition; 60-second cooldown; 512 KiB export cap | Mark truncation; no capture loop. |
| Normal background export | 1 MiB/machine/hour, maximum 32 KiB batch | Delay/drop optional detail; preserve loss counters. |
| Incident export | 5 MiB/incident, maximum 4/day/machine; shared tenant caps apply | Keep local capture and metadata marker; no budget expansion. |
| Cloud routine detail | 24 hours, within 50 MiB/machine and 500 MiB/account | Show quota/retention floor; finite fair allocation. |
| Cloud incident evidence / aggregates | 7 days / 30 days, within the same effective quota | Separate accounting and explicit expiry. |
| Query | 200 events / 1 MiB/page, 5-second deadline; max 24-hour raw range | Cursor/partial status with gaps; aggregates for wider windows. |
| Active-dashboard probe | 60 seconds, one outstanding per machine | Bounded timeout, no overlapping retries. |

Within the normal 1 MiB/hour envelope, reserve 64 KiB/hour and at most 120 metadata requests/hour
for heartbeat, health transitions and explicit loss markers. Ordinary traces cannot consume this
reserve. This reserve is still bounded: once exhausted, local evidence remains and remote state
becomes stale/unknown; do not promise timely remote fault classification. Proposed active-source
snapshots are every 30 seconds, upload flush at most 15 seconds, dashboard polling every 15 seconds,
with bounded jitter; P0 must reconcile rates, response caching and total tenant/global caps.

Ingest additionally needs an event-body maximum, bounded tenant/global bytes per day, finite metric
label cardinality, total outstanding upload bytes, retry attempts/time budget and query concurrency.
Finalize these in the capacity contract using fleet load measurements; missing global bounds block
production rather than implicitly selecting infinity. No new paid observability vendor or LLM
analysis is required. Existing infrastructure still incurs storage/egress/write costs. Publish cost
arithmetic (bytes × sources × retention × replica/index factor, requests and probe frequency) and
hard budget controls; do not silently enable recurring external monitoring or expand spending.

## Authentication, privacy and availability

Cloud operator sees minimum operational metadata across services and authorized tenant scopes.
Account owner sees only their machines, and private daemon diagnostics require separate paired
owner authorization/decryption. Superuser status never grants content keys, machine tokens, raw
stacks, message bodies, document names or session contents. The default is content-free metadata.
Owner-encrypted retained attachments require explicit opt-in, bounded retention, revocation semantics
and deletion handling; encrypted bytes are still sensitive and subject to scope and audit.

Server derives tenant/device from the principal; reject spoofed scope, guessed IDs, invalid schemas and stale/revoked credentials. Exact retries
of a batch/event return the prior durable receipt without rewriting it; reuse of an ID with
different content returns `idempotency_conflict`. Recheck current principal authority on retries. Correlation metadata is authenticated and validated.
Detailed cross-account inspection and evidence export are audited; if required audit cannot persist,
fail that sensitive operation with a typed reason. Routine dashboard reads can use a bounded audit
summary; security-event audit must not disappear because telemetry sampling is full.

New contracts expose sanitized evidence, not existing local-only diagnostics verbatim. The agent
gets a narrow local read route/CLI, or an explicitly authorized account export; never borrow a browser
credential or bypass pairing. Clear decrypted caches on sign-out, revocation and account changes.
No fallback to LAN/tunnels or broad local access is implied.

## Query and agent investigation contract

Proposed Cloud operator reads: overview, source/machine timeline, bounded events, trace, incidents
and collector health under `/su/v1/health/`. Owner reads have separate tenant-bound routes. Local
`clawdline health` commands provide status/events/trace/incidents/export with explicit `--since`,
`--until`, source, machine/carrier and JSON output. These commands do not exist yet.

Dedicated ingest accepts authenticated versioned bounded batches, deduplicates event IDs and returns
accepted/rejected counts plus a durable ingest receipt. Accepted is not persisted until that receipt
says so; producer retains only its bounded retry window and exposes loss. API failure, invalid
schema, unsupported version, retention expiry, quota, forbidden and partial coverage are typed.

Every query returns generated/as-of timestamps, source freshness, coverage/sequence ranges,
retention floor, sampling/drop/truncation, continuation cursor, schema/rule/build versions and clock
uncertainty. A timeout/unsupported producer cannot become an empty successful event list.

An incident evidence bundle contains:

1. Manifest, incident/revision, exact scope/window, provenance, schema and file hashes.
2. Facts and last-proven lifecycle stage; separate hypotheses and unverified recovery.
3. Source boot/build, stable location codes mapped to exact-commit function/source frames.
4. Correlated bounded spans/events/queue holders, pre/post metrics and typed failures.
5. Clock uncertainty, missing intervals, retention cutoffs, suppression/drop/truncation and redactions.
6. How to query the next page or obtain missing owner-authorized evidence; no automatic repair.

A future agent starts with the incident summary, chooses the failing edge, reads the last-progress
stage, resolves the location map for that build, then checks contrary evidence before proposing a
cause. The UI may identify a blocked stage without declaring it the root cause. A structured bundle
must be useful without screenshots, huge log pastes or access to the person's phone.

## Incident workflow and alerting

Detection groups correlated symptoms by scope/component and time, not just text similarity.
Stages: active, recovering, recovered-awaiting-review, closed. Keep first observed failure, last
success, last progress, recovery evidence and residual in-flight work. Operator acknowledgment and
technical recovery are separate. Incident pinning has a hard count/byte/expiry policy.

Problem lists rank observed affected logical operations/machines/viewers, duration and evidence
quality. Keep the selected incident/time window stable during live refresh; new problems do not
move keyboard focus or replace the investigation. Sampling limits affected-count claims.

Application probes are explicitly read-only isolated synthetic operations, never user mutations.
Record probe issue, application handler response and viewer apply separately. Show exactly which
read/control/status edges the probe exercises; a ping or successful read leaves unexercised control
execution unverified. Probe traces are separately labeled and excluded from user success/error
denominators. One browser probe proves that browser viewpoint, not all users.

Default dashboard is passive. Optional incident alerts need owner-selected scope, rule version,
threshold, deduplication, escalation and recovery semantics; do not enable noisy automatic pushes
as part of adding a chart. Reconnect storm and upstream outage group related incidents; telemetry
quota does not suppress the visible fact that observations are incomplete. Never automatically
retry a user mutation based on an incident or a lost receipt.

### Reading an incident: people first, agents and engineers one disclosure away

An incident revision (M05) is read in this order:

1. **What happened** (`incident-summary.ts`): a plain summary built only from the revision's own
   typed fields — what was observed at the time, what the frozen evidence can and cannot say now,
   what is missing, and the next place to look. It names no event or stage codes.
2. **Agent investigation** (`investigation-view.tsx`), one card: the newest investigation's state
   (*Investigating*, *Needs you*, *Verified recovered*), the Cloud's reason in a fixed sentence with
   its next step, then the agent's enumerated report (conclusion, fix commit, cited evidence).
   Earlier investigations fold away; ids, hashes and delivery codes are under its own disclosure.
3. **Recovery evidence and human review**, side by side. Acknowledgement stays an explicit
   two-step act and never marks recovery.
4. **Evidence records**: one card per frozen record, with the event and stage in words, the
   producer's own measured time when it sent one, and Cloud's receive time. Every exact field —
   raw event and stage names, the full build commit, record and event ids, digest, producer wall
   clock — is under the card's *Technical record* disclosure.
5. **Context** (neighbouring records) and **Technical details for engineers and agents**
   (revision hashes and sizes, retention, the proven/active stages as stored, sequence coverage,
   the manifest) are disclosures; the audited export stays at the end. Nothing is removed, and the
   exported bytes are unchanged.

The summary's limits are deliberate. An *active* incident is described as "no ending or recovery
recorded, current status unknown", never as stuck now: a frozen revision cannot observe the
present, and a missing ending may be a missing record. It gives no cause, no percentage and no
threshold value; the only duration is the producer's own `elapsed_ms`, and the only age is the
read's `as_of` minus the newest receive time, both Cloud's clock. A completed stage such as
`api_dependency_completed` means downstream work returned; when no final stage or recovery follows,
the summary says no final response was recorded, not that the dependency was slow. Slow and stall
are the producer's threshold observations at that moment, and a `pending` result is not final.
Old records that carry no request route cannot be joined to service logs from this page, so the
next step for such an operation is a log search around the last receive time, stated as such.

### Handing an incident to an agent

The operator detail carries `investigations` from the Cloud's private investigation contract
(v1). The page reads it as three cases and never folds them together: no field at all is an older
Cloud (*not supported*, nothing may start), `{status: "unavailable"}` is storage Cloud could not
read (whether one is running is unknown, so nothing may start), and `{status: "ok", items}` is the
list. An investigation row whose state, reason, report conclusion or recovery basis this page does
not know keeps that value as unknown and is still shown; it never refuses the incident, and an open
row the page cannot place blocks a start because it may hold the one live slot.

*Hand to an agent…* lists the operator's own active schedule webhooks from
`GET /v1/schedule-webhooks` (`clawdline.schedule_webhook.list.v1`). That list has no field saying
which hook is bound to an investigation schedule, so the page says so and asks the operator to
confirm the binding; a machine without it refuses the delivery and the investigation reads
*Needs you / refused*. The page creates no webhook and changes no schedule. The start is
`POST /su/v1/health/incidents/:id/investigations` with exactly `hook_id` and
`expected_incident_revision` (the latest revision; `deliver_within_seconds` is left to Cloud's
default) and an `Idempotency-Key` the page keeps until a definite answer. A lost answer, an
untyped 5xx or an unreadable 202 keeps the key and body and offers *Send the same request again*;
Cloud's typed refusals spend it. `investigation_open` and `incident_revision_moved` reload the
incident so the live investigation or newer revision is shown. Task success is never recovery,
and human review stays separate: acknowledgement changes no investigation.

## Delivery plan and ownership

This plan preserves the complete scope; phases determine order, not which requested surfaces disappear.

| Phase | Owner roles | Deliverable and exit evidence |
| --- | --- | --- |
| P0 Contract and baseline | Architect, SRE, Cloud/privacy, product | Source inventory, final stage/health/permission schemas, producer coverage matrix, capacity/budget registration and failure fixtures. Measure present overhead first. |
| P1 Evidence foundation | Daemon/backend, Cloud backend, SRE | Atomic active phases, bounded local journal/watchdog, API/relay/viewer progress, independent ingest/query and clock/build identity. Inject stalls and prove retained evidence. |
| P2 Production monitor | Claude Code UI designer, frontend/backend, UX/accessibility | All M01–M06 with real sources, mobile, timelines/waterfalls, unknown/stale states, owner/admin boundaries, durable incidents and agent query/export. No demo-only acceptance. |
| P3 Operational acceptance | Independent SRE/test/performance, incident commander | Failure matrix, load/soak/budget/privacy/access tests; an uninformed agent locates injected faults using only bundles; production acceptance and rollout/rollback runbook. |
| P4 Operational refinement | Product/SRE | Calibrated SLO, optional deduplicated alerts, incident/deployment comparison and long-term trends. Never silently introduce automated repair. |

P0–P3 are the first production acceptance boundary. Deploy producers/contracts before consuming UI;
old producers render unsupported/partial instead of blocking the page. Rollout local fixtures →
isolated synthetic account → owner's opt-in machine → small cohort → default bounded metadata.
Owner private diagnostic retention stays opt-in. Independent global probes require an explicit host,
lifecycle and cost decision before production claims. Stage feature flags per producer/ingest/UI;
turning monitoring off must not interrupt sessions or erase preserved incident evidence. Rollback
keeps compatible old event readers and marks unknown fields/unsupported schemas explicitly.

The root owns synthesis, integration and release evidence. Claude Code owns panel design. SRE owns
health predicates and failure injection; backend owns collectors/contracts; security/privacy owns
scope and redaction; UX/accessibility owns the human investigation path. Do not substitute multiple
reviews of the same facts for a measured integrated candidate.

## Production acceptance matrix

| Fault injection | Required observation and proof |
| --- | --- |
| Handler never returns | Socket may remain connected; show blocked handler, age, holder and exact-build location; watchdog and stored queries continue. |
| Writer mutex held vs socket write held | Distinct lock-wait/write stages, durations and independent evidence; no shared-lock watchdog failure. |
| Relay receipts disappear after successful execution | Executed remains proven, receipt/viewer observation unknown; no blind resend; identify oldest unacknowledged sequence. |
| Relay account/storage output gate stalls | Worker admission, account entry/return and outer completion separate; healthy global API cannot hide account failure. |
| API dependency slow/down | Dependency and query/ingest failure visible; static shell/independent vantage does not report green. |
| Read slots saturated, large image refused | Occupants/oldest age/capacity and typed 429; no image content; saturation distinct from stuck work. |
| Direct replacement/reconnect storm | Per-viewer attempts, supersession links, limit refusal, fallback and actual operation result preserved. |
| Zero subscribers/socket send throws | Socket acceptance/fanout/refusal evidence; never claim endpoint observed. |
| Viewer receives but cannot decrypt/apply; SSE gap | Last proven receiver stage, typed failure or missing apply evidence; sequence/resume/snapshot gaps explicit. |
| One viewer succeeds and another fails | Scope isolation; global averages cannot hide the affected viewer. |
| Machine offline, no traffic, old producer | Offline/no_requests/unsupported visibly distinct, with last known Cloud evidence. |
| Clock jump, process restart, duplicate/reordered/late events | Boot/sequence/cause correlation remains valid; no fabricated latency; durable history and revision provenance. |
| Active business span slots exhausted | Critical loop slots still progress; overflow interval/class/count and partial trace coverage visible; agent names missing evidence rather than fabricating a stage. |
| Collector queue/disk/store full or stalled | Nonblocking business traffic, bounded drops and stale coverage; watchdog does not share failed sink/lock. |
| Sign-out, revocation, cross-tenant ID guesses, export expiry | Server-side refusal, no cached plaintext flash, audit and least privilege. |
| Multiple incidents, low traffic, live refresh | Impact ordering and filters stay stable; selected row/time does not jump; stopped errors without a new success remain recovery-unverified. |
| Desktop/mobile keyboard/readers | First-screen problem recognition and find-incident → select-time → inspect-stage → export flow, without hover. |
| Monitoring enabled vs disabled under idle/load/failure storm | Measured CPU/memory/IO/latency/egress, bounded labels/queues/retries/retention; register actual capacity. |

For each experiment retain injected condition, exact builds, expected/actual stage, UI evidence,
versioned bundle, measured detect/recover times and observer cost. Initial proposed UX targets:
recognize the affected path within 30 seconds; identify the last progressing stage within two
minutes. Initial detection target: after a per-stage deadline is crossed, mark the local stall within
two watchdog periods plus the separately measured persistence/UI delay. Remote visibility adds
the configured collection, upload and query intervals with explicit network delay bounds; this
target applies only while reserved metadata budget and ingest are available. Otherwise remote
collection becomes stale/unknown and must not imply a timely root-cause observation.
These are acceptance targets to test, not performance already demonstrated.

Final acceptance includes a withheld fault experiment: an agent without the injector's explanation
receives only the dashboard/export. It must identify the fault domain and implementation stage, or
precisely name the missing evidence. When evidence is insufficient, name the next missing instrumentation point and a reproducible
verification step. A plausible guess is not a pass. Historical selection is labeled historical,
not current, and pauses live movement until the person explicitly returns to now.

## Local real-adapter acceptance (2026-10-21)

An isolated test binary over source commit `c4926432a` exercised the real WebSocket `Conn`,
Runtime, system monotonic clock, watchdog, journal, runtime stack collector and production
sanitizer. Its only added source was a task-owned test harness; it was not a released daemon.
The two blocked exports were handed, without injector code or expected answers, to an independent
investigator. Source SHA-256 values for the adapter, location map, capture worker and runtime
matched that commit. The investigator correctly distinguished both waits:

| Observation | Exact-build join | What the export established |
| --- | --- | --- |
| `writer_lock_waiting`, no progress | `internal/adapters/cloud/websocket.go:316`, `Conn.writeFrame` | A captured goroutine was waiting in `sync.Mutex.Lock`; it did not establish the holder or a deadlock. |
| `socket_write_started`, one progress step | `internal/adapters/cloud/websocket.go:353`, `Conn.writeFrame` | A captured goroutine was in `net.(*pipe).write`; it did not establish a production TCP failure, peer cause or byte count. |

Each arm first established the actual tracker phase and runtime frame. Detection was about
15.1 seconds in each single local observation. Twenty local probe/export pairs remained callable
while each fault was held. After release, findings cleared in about 99 ms and the harness observed
successful writes. Those successful return values were separate harness assertions: the shared
blocked exports showed `still_blocked`, and the later export reported
`released_outcome_unknown`. Neither export proves successful delivery. These measurements are
not a production latency target, overhead benchmark, persistence/restart acceptance or an
explanation of a historical connection outage.

### Remaining attribution and the next instrumentation points

The following are explicit design points for a later evidence-contract revision, not fields that
this acceptance claims are implemented:

- Bind writer phases to a process-local opaque connection resource and an instrumented holder
  operation. At `Conn.writeFrame`, record lock acquisition/release and the holder's current child
  stage independently of the writer lock. An uninstrumented holder remains unknown. Validate with
  multiple connections and multiple writers; stack-function matches alone cannot select a holder.
- At each underlying `Write` return, retain a bounded outcome distinguishing header and payload,
  returned byte count, typed error, deadline expiry and local close/cancellation. Keep addresses,
  payloads and peer identity out of public metadata; any private detail follows owner encryption.
  A return still does not prove peer observation or viewer application.
- Bind the real operation's terminal outcome to capture recovery through an independent bounded
  journal path. Distinguish phase release, successful local return, transport receipt and viewer
  application. If a terminal transition was dropped or never collected, preserve
  `released_outcome_unknown`; never upgrade it from silence.

Any implementation must version and validate the affected export/upload contracts together,
register genuinely new bounds, preserve nonblocking observation, and retain failure-injection
proof. The local experiment establishes useful stage attribution while leaving these deeper
causal and recovery claims unavailable.
