# Runtime performance

This page is about the latency of the running Clawdline app: Session inventory, Session Info,
transcript reads and the Cloud path back to the Mac. Build and test capacity is a different
problem, documented in [machine-resource-scheduling.md](machine-resource-scheduling.md) and
[suite-runtime.md](suite-runtime.md).

## Baseline measured on 2026-09-07

The measurements below were taken on the 24 GiB Mac that runs the Clawdline broker. They compare
the same service through loopback and through Clawdline Cloud. A Cloud number includes tunnel and
network latency; it is not evidence that the Mac spent that time doing work.

| request | observed wall time |
|---|---:|
| local `/v1/health` | about 1 ms |
| Cloud `/v1/health` | 0.58–0.88 s |
| local `/v1/sessions` | 0.43–1.03 s |
| Cloud `/v1/sessions` | 0.89–1.79 s |
| cold local Session Info, 3.4 MiB provider record | 1.185 s |
| cold local Session Info, 39.7 MiB provider record | 10.294 s |
| cold local Session Info, 82 MiB provider record | 20.187 s |

The nearly linear cold-Info curve identified a local file-processing cost. Before the correction,
one request loaded the whole provider record and then the usage readers loaded it again; several
fields subsequently split and parsed those bytes independently. The fixed Cloud health cost is a
separate floor. It should be subtracted before attributing Cloud latency to the Mac.

The Mac was not under active swap pressure during these readings, and the Clawdline process held
roughly 100–140 MiB. Memory pressure was therefore not the primary cause. iTerm2 CPU was sometimes
high while several terminals were painting output, which can add contention and Apple-event
latency, but it did not explain the provider-record size curve.

## Follow-up measured on 2026-09-08

The optimized build was measured again against the same running Codex Session. Local Session-list
and Info reads were already substantially faster than the 2026-09-07 baseline. Transcript remained
the dominant local read until one repeated full-output scan was removed.

| request | observed wall time |
|---|---:|
| local `/v1/health` | 0.8–1.1 ms |
| local `/v1/sessions` | 9.8–44.3 ms |
| warm local Session Info summary | 0.8–3.0 ms |
| local transcript, 200 entries, before output-scan correction | 1.22–2.06 s; median about 1.45 s |
| same transcript after correction | 0.30–0.53 s; median about 0.405 s |

The transcript correction is about 3.6 times faster at the median even though the observed rollout
grew from 21.7 MB to 23.6 MB between the two readings. Sampling had attributed most stacks to
`Codex.outcome`: it split each command's complete output merely to retain the first non-empty line.
Some individual output fields were 95–111 KB. The reader now stops at the first useful newline.
A five-second sample after the correction found no `Collection.split` below `Codex.outcome`;
remaining transcript time was primarily JSON decoding and semantic entry construction.

Public Cloud requests were also timed independently so their connection cost would not be charged
to the Mac:

| public endpoint | new connection | reused connection |
|---|---:|---:|
| hosted `BUILD.json` | 0.46–0.47 s normally | 0.16 s |
| API `/healthz` | 0.50–0.52 s normally | 0.21 s |
| API `/readyz` | 0.50–0.52 s normally | 0.21–0.22 s |
| API billing tiers | 0.50–0.52 s | 0.21 s |
| relay health | not measured cold | 0.14–0.15 s |

The production API topology is a proxied Cloudflare A record followed by one `t4g.medium` EC2
instance in `us-east-1a`; Caddy terminates origin TLS and proxies to Fastify, while MongoDB runs on
the same Compose network. A read-only SSM benchmark from that instance measured Fastify health at
1.03–1.35 ms, readiness including a Mongo ping at 1.68–2.04 ms, and Caddy plus local TLS at
6.38–7.52 ms. Caddy negotiated HTTP/2. CloudWatch's preceding six hours showed CPU averaging 6.5%
and peaking at 7.53%, a full 576 CPU credits, no surplus-credit use and no failed status checks.
The instance size, CPU credits, local database and origin reverse proxy are therefore not the
material source of this request's roughly 0.21-second warm latency.

The edge observed for these requests was San Jose. A warm hosted or relay request took about
145–160 ms, while a warm API request took about 211–220 ms. The additional roughly 65–70 ms is
consistent with the API continuing from the Cloudflare edge to the AWS origin in `us-east-1`.
Moving or replicating the origin closer to Taiwan could lower that floor, but it is a regional
architecture and data-placement decision rather than a Mac CPU optimization. Cold TLS setup adds
about another 290 ms before application work.

The Cloudflare zone currently has HTTP/2, HTTP/3 and Brotli enabled. Zero-RTT and Early Hints are
off. Tiered Cache is off and not editable on the present plan. The available token could read
ordinary zone settings but received an authentication refusal for Argo Smart Routing and the
origin HTTP/2 stream setting, so their current values remain unknown rather than being inferred as
off. Caddy proves that the origin itself supports HTTP/2.

The most promising Cloud-side experiment is paid Argo Smart Routing, measured specifically against
the 65–70 ms edge-to-origin seam before it is retained. It can choose a better Cloudflare backbone
path but cannot erase the physical SJC-to-Northern-Virginia distance. For a primarily Taiwanese
audience, placing or replicating the control plane in AWS `ap-east-2` (Taipei) should have a larger
latency effect; this requires a deliberate data residency, migration and multi-region consistency
design because the current EC2, EBS and Mongo resources are regional and single-instance. Merely
resizing the current EC2 instance is not supported by these measurements.

The hosted page currently advertises 79 module preloads and 21 stylesheets during startup. A cached
reload observed through browser automation completed in about 0.38 s, while a newly created browser
surface took about 6.95 s including browser startup and automation overhead. Treat the latter as a
user-path observation, not a network-only benchmark. Reducing the roughly 100 startup resource hints
is a future bundling/code-splitting candidate, but removing preloads alone could replace parallel
fetches with a module waterfall and is not itself a safe optimization.

Enabling Early Hints is a lower-risk cold-page experiment for those existing static resource hints;
it does not accelerate authenticated API or relay traffic. Zero-RTT would affect only resumed TLS
connections and requires a replay-safety audit of every admitted request, so it is not recommended
as a first response to the measured latency.

Authenticated production inventory and transcript timings were not collected in this follow-up:
the local Keychain credential seam returned no usable credential, and a fresh browser profile was
not paired. The public timings therefore establish transport floors; they do not claim the latency
of an authenticated end-to-end Session read.

## Cloud reconnects are credential rotation, not an empty inventory

The production viewer device token normally lives for 300 seconds. The relay closes a socket when
that credential expires, so waiting for the close before minting the next token creates a visible
control-plane and WebSocket round trip every five minutes. The hosted client renews 30 seconds
early: the authenticated socket already serving the page remains active while its replacement
mints a token and completes the signed relay challenge. The replacement is not installed as the
page's API merely because its WebSocket object exists; it becomes usable only after `ready`.

Relay realignment is one retained `s/<machine>/<session>` channel at a time, in no inventory order.
It is not an atomic account-wide Session list. A replacement client therefore starts with the same
viewer's last-known-good Session rows and applies the realigned envelopes to that map. An explicit
tombstone still removes a closed Session. Without that distinction, the first retained row after a
reconnect looked like the complete inventory and the phone closed whichever conversation had not
replayed yet, returning the reader to the list.

The authoritative membership frame is wire version 1 on
`s/<machine>/__clawdline_inventory_v1__`, with exactly `{"inventory":{"version":1,
"sessions":[...]}}` and at most 512 unique, nonempty ids. The reserved id cannot itself appear in
the list. Retained frames remain signed and encrypted, but their sender sequence is aligned per
channel because relay delivery order is arbitrary; non-realignment traffic retains sender-wide
replay protection. Session rows and inventories also retain their signed sequence as freshness:
an older inventory cannot prune a newer row, and a row older than an inventory that excludes it
cannot resurrect a closed Session. The inventory scopes membership only to the verified machine;
it never trusts a title or tty and never prunes another Mac.

This inventory version says which Session ids exist, not whether transcript bytes changed. That
signal is the Cloud-only row field `transcript_signature` (docs/cloud.md): a row is republished when
its transcript signature changes, at most one pass per second, and never for an unchanged one.
Opening a conversation always makes a fresh foreground Cloud transcript read with no conditional
revision, while automatic refresh remains background and follows the existing transcript demand
signals. A typed transcript `not_found` removes only that exact `(machine, Session)` row.

The task registry held about 400 records. That means 400 retained rows, not 400 filesystem scans.
The defect was that Session-list projection repeatedly fingerprinted and sorted the whole registry,
then selected one Session's obligations by walking it again. A compact rendering of that fact must
not be described as “400 scans.”

## What an open console costs the device holding it

Measured on 2026-09-15 against the hosted console, the phone reading it overheated: 472 transcript
reads in fourteen hours (38.9 MB), 223 of them byte-identical to the read before and 134 less than
five seconds after it. The browser's own share of that — separate from the Mac's publication rate
and the Cloud transport's socket — is held to these rules:

- **A transcript is read only when it can have changed.** A Cloud row carrying
  `transcript_signature` (equal to the `signature` of that session's transcript read answer) is
  transcript demand when the signature or the row's `state` changes, and not when `line` or `label`
  does. The state edge is read at once because the Mac debounces the signature and a turn that has
  just ended is the moment a reader waits for. A row whose signature already names the transcript
  on screen, read in the state it is in now, is settled without a read. A row without the field (an
  older Mac, a Linux machine, the local page) reads at once on a state or label change, and a
  `line`-only change costs at most one read per 15 seconds (`TRANSCRIPT_LINE_REREAD_MS`); a held
  line is read when its interval is up even if nothing else arrives. The local page's
  `transcript-revision` file events still read immediately. The policy is
  `createTranscriptRefetchPolicy` in `session/transcript-requests.js`.
- **An unchanged answer is not redrawn.** A transcript answer whose signature is the one on screen
  redraws only when an optimistic echo was retired, an error line or wait has to come down, or the
  session's working state (which decides the live sweep on the last tool run) changed.
- **A burst of frames is one draw.** Session and orchestrator frames update state at once and draw
  in the next animation frame, once however many arrived (`net/handlers.js`). A row whose drawn
  inputs are unchanged is not refilled, rows already in order are not moved, and a list in which
  nothing changed is not measured for FLIP.
- **Layout probes are coalesced.** `Diagnostics.note` is covered by at most one probe per
  `LAYOUT_PROBE_INTERVAL_MS` (1 s), the first note after a quiet second in the next frame and the
  rest of a burst by one trailing probe. Probes run only where they can be read: on a phone, where
  an incident can be recorded, or while the layout panel is open.
- **A hidden page runs no clock that does work.** The spinner clock, the status line's minute read,
  the transcript time relabel, the terminal lease and poll, the background-command beat and the
  Devices reload stop their timers while the page is hidden and catch up once when it is visible
  (`core/visibility.js`); a status-line reading that came due or a turn that ended while hidden is
  read once on return. The spinner clock draws only canvases still in the document and not the
  list's while a phone has the list behind a transcript. The remaining bare intervals — the voice
  recording limit, the pairing door's countdown and the local transport's reconnect label — are
  named in `Tests/web-transcript-requests.mjs`, which refuses any other.
- **Infinite animations move layers.** The live-run sweep and the skeleton sheen animate
  `transform` rather than `background-position`, and a transcript redraw sets
  `--live-sweep-delay` so the sweep continues from its current phase instead of restarting.

To measure a recurrence from the Mac's side, join `cloud: read received id=<id> read=transcript
sender=web_<device> session=<id>` with `cloud: read routed id=<id> … bytes=<n>` in
`~/Library/Logs/Clawdline.log` and count reads per device, identical consecutive byte counts per
session, and the gaps between them.

## Runtime invariants

Performance here is kept by structural bounds rather than by a timing threshold that happens to
pass on one fast machine.

- Session Info reads one newline-aligned provider-record tail, capped at 8 MiB. Summary and full
  Info share one signature-keyed parse. Request cost no longer grows with the lifetime size of the
  conversation.
- Codex writes cumulative token counters, so its newest counter remains an exact total in a tail.
  Claude writes per-turn deltas. If a Claude record is larger than the bound, cumulative `usage`
  is omitted rather than publishing a partial sum as the whole session. Current context and the
  status-line cached cost remain available when their source facts exist.
- Parsed provider facts use a 24-entry LRU cache. Slow-reading results use a 128-entry LRU cache.
  Transcript title, custom-title and weak-title caches each retain at most 512 entries. A long-lived
  process therefore cannot retain one entry for every key or historical transcript it has ever
  seen.
- Transcript reads have one serial interactive worker and one serial background worker, with a
  combined bound of two and a background bound of one. A person opening a Session can therefore
  parse concurrently with one agent/refresh read, while each class remains bounded and ordered.
  Cloud carries this intent explicitly; a stale hosted tab with no field defaults to foreground,
  while newer automatic callers explicitly remain background. The Cloud bridge separates those
  classes before local routing too, with a four-read foreground bound and a sixteen-read background
  bound. The limit+1 request bypasses the saturated worker and receives an encrypted typed
  `cloud_read_busy` 429 on its own Session channel; capacity returns as soon as that lane drains.
- Cloud Session snapshots are deduplicated on what a viewer reads, so a watcher tick publishes only
  rows that changed outside the three freshness-only closeability paths (`observed_at`,
  `session_generation`, `source.observed_at`). An authoritative inventory of at most 512 exact
  Session ids is encrypted on a reserved machine-scoped Session channel after each changed scan, on
  reconnect, and after three minutes without any Session-channel frame, which keeps an idle Mac
  inside the hosted console's five-minute machine window. It repairs retained relay rows across a
  Mac restart without trusting labels or terminal names.
- Durable Cloud publication returns each producer after reserve, encryption and durable spool seal;
  one lifecycle-owned worker performs globally ordered socket writes independently. Already-sent
  rows may await correlated receipts while later ready rows fill a hard row/byte window (currently
  8 rows and 4 MiB, labelled `implementation_default_pending_w6`). Current/process-local-peak window
  debt, ready wall-clock age, receipt wait, exact frame bytes, socket work/failure and window-refusal
  attempts are separate observations. `s`/`orch` ready backlog is latest-value coalesced and every
  channel's authenticated sealed timestamp is checked immediately before send. A stalled socket owns
  one worker; stop closes its transport before joining it, so Foundation's non-cancellation-aware
  WebSocket send cannot hold bridge replacement.
- Every transcript admission/refusal/completion records its lane, source, target, queue debt,
  queue time, parse time, total local time, status and answer size. Cloud additionally records
  receive-to-route and encrypted-publication timing, without recording transcript text.
- Session closeability uses an immutable read-side index. Settled historical tasks are absent from
  root and parent hot buckets; an exact child identity still retains all matching rows so duplicate
  identity evidence fails closed. The index rebuilds only after a closeability-relevant registry
  fingerprint changes, not for every poll or event publication.
- The orchestrator store still has a deliberate retention valve (1,350 ordinary task rows and 30
  days by default). It is serialized and atomically replaced as one file on mutation. It is written
  once per save, not twice.

The last item remains an O(retained history) mutation cost. The read path is indexed, but the
durable registry is still one JSON document rather than a journal or database. If save latency
becomes visible near the configured ceiling, measure serialization and atomic replacement
separately before choosing a storage migration.

The Cloud outbound spool is also still an atomically replaced JSON authority. Its commit path no
longer rereads the complete old payload before replacement; it checks the same descriptor metadata
and pathname inode instead. Live evidence on a 4.13 MiB/1,674-row spool measured the full
read/decode/encode/write/fsync commit at about 8 ms, while producer publication was p50 44,493 ms
and p95 126,135 ms. That comparison rules persistence out as the primary tens-of-seconds cause;
the corrected scheduling seam is the producer's former await of socket drain.

## How to measure a recurrence

Hold the subject still: compare the same endpoint, Session id and provider record through each
surface. Do not compare the first task with usage on disk to a different task in an HTTP response.

1. Time local `/v1/health`. A slow health request indicates queue or process-wide contention; a
   fast one establishes the service floor.
2. Time the same health request through Cloud. The difference is the tunnel/network floor for that
   moment.
3. Time local and Cloud `/v1/sessions` with the same inventory. If the local form grows with task
   retention, inspect index invalidation and registry mutation frequency.
4. Choose one Session id and record its provider-record byte size. Time
   `/v1/sessions/:id/info?parts=summary` locally, then the full form. Repeat only after changing the
   file signature or restarting the app if the question is cold-read cost; otherwise the shared
   cache is intentionally part of the result.
5. Time `/v1/sessions/:id/transcript` separately. It has its own bounded read and should not be
   blamed for an Info delay without a same-id measurement.
6. Record Clawdline RSS, CPU, macOS memory-pressure state and iTerm2 CPU at the same timestamps.
   RSS alone cannot establish memory pressure, and aggregate CPU alone cannot identify the route
   doing the work.

Use authentication headers without printing their values into logs or documentation. Report HTTP
status beside wall time: a fast typed refusal and a slow successful read are different outcomes.

## Long-run acceptance

A soak test should keep one app process alive while it observes more unique Session and Info keys
than every cache capacity, appends to a large provider record, and advances the task registry toward
its retention limit. Sample RSS and the same local endpoints at fixed intervals. Acceptance means:

- cache entry counts plateau at their documented bounds;
- cold Session Info remains a function of the 8 MiB window, not total provider-record size;
- unchanged Session polls do not rebuild the closeability index;
- settled unrelated task history does not increase one Session's candidate count;
- local health remains near the service floor while slow reads are in flight; and
- Cloud minus local latency is reported separately, not charged to CPU or memory on the Mac.

If RSS still grows after the bounded caches plateau, take a heap graph before raising a cache limit.
If CPU grows while inputs are unchanged, sample the process and identify repeated work; elapsed
runtime by itself is correlation, not a cause.
