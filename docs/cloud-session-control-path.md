> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.

# The session control path

This page is about one narrow thing: what happens between pressing **open a session** or **close
this session** in Clawdline Cloud and that actually being true on the Mac and visible in the
browser. It names seven mechanisms on that path, what each one costs when measured, and what
should replace it. It is a design assessment, not a landing receipt: nothing here has been
implemented.

Three neighbouring pages own the rest, and this one does not restate or compete with them:

- The **size** of a read and the resource model that replaces one-large-object responses belongs
  to [read-path-architecture.md](read-path-architecture.md).
- The **target Cloud data planes** — replicated status, transcript segments, a command job ledger,
  live queries — belong to [cloud-request-architecture-v2.md](cloud-request-architecture-v2.md).
  Its section 11 already lists the whole-file spool rewrite, the global outbound sequence, and the
  synchronous read lane as concepts to remove rather than optimise. This page adds two days of
  fresh measurement to that diagnosis and does not propose a parallel architecture for it.
- Per-service latency belongs to `runtime-performance.md`.

What is new here is the part neither page covers: the **failure semantics of a control message**,
and the two layers outside the Cloud protocol that this path also crosses — the Mac's terminal
inventory, and the browser's sequence allocator.

## How these numbers were taken

From the running app on the broker Mac. The log slice is the last 400,000 lines of
`~/Library/Logs/Clawdline.log`, which covers 2026-09-15 04:13 to 2026-09-16 11:46 — 31 hours. The
durable-store readings are from the live
`~/.config/clawdline/cloud-runtime/machine-v1-*/outbound-spool.json` at 2026-09-16 11:57, and the
counter readings from `~/Library/Logs/Clawdline/diagnostics/cloud-status.json`, whose window opens
at its own `counting_since` and is not the log's window.

Two cautions that changed conclusions while this page was being written, and will change them
again for the next reader:

- **`cloud: spool commit` is logged only above 250 ms** (`CloudBridgeLifecycle.swift:709` passes
  `spoolCommitObservationThresholdMilliseconds: 250`). Every commit statistic below is therefore a
  statistic about the slow tail, not about commits. "Median 405 ms" means the median of the 25,204
  commits that each took longer than 250 ms; the median commit is unmeasured.
- **The same file is written by the test suite.** `%nope%`, `CLOSURE-ROUTE`, `CLOSURE-PEER`,
  `BLOCKED-TERMINAL`, `FOCUS-TAB`, and the `cccccccc-dddd-4eee-…` identifiers are fixtures. Attributing
  a slow minute without first excluding them produces a conclusion about `./test.sh`.

Real browser-issued traffic is identified by `by cloud:web_<device>`. Over the 31 hours there were
**8 session starts, all 200**, and **13 session closes: 8 × 200, 4 × 502, 1 × 404**.

## What the path is today

```
browser ── envelope(seq) ──▶ relay ──▶ CloudTransport
                                          │
                                   claim(sender, seq)          CloudEnvelope.swift:396
                                          │ replay? ──────────▶ refuse, reply=notice   ✗ stops here
                                          ▼
                                   ledger.reserve(request_id)  CloudAppBridge.swift:3523
                                          │ cached? ─────────▶ replay the stored outcome
                                          ▼
                                   commandRouter.route
                                          ▼
                                   RemoteServer /end          RemoteServer.swift:2855
                                          ▼
                                   TerminalSafeClose.end      HostPorts.swift:766
                                          │ inventory complete? ─▶ refuse 502  ✗
                                          ▼
                                   /exit · TERM · KILL · close tab
                                          │
   ◀── spool.reserve → seal → sendNext → settle ──── 4 whole-file commits per message
   ◀── outbound window: 8 rows ───────────────────── CloudOutboundSpool.swift:214
```

The two `✗` exits are the ones a person sees as "it errored". The queueing that makes the rest
feel broken happens on the return leg, which every other Cloud message shares.

## Seven mechanisms

### 1. The viewer's sequence allocator hands the same number to two tabs

`durableSequence` (`Resources/web/app/js/net/cloud-boot.js:154`) reserves sequence numbers in
blocks of 64 from `localStorage`, keyed by `clawdline.viewer.sequence:<deviceID>`. Two tabs of the
same device share that key, and the allocator renews its block from **its own last value**
(`reserved = value + SEQUENCE_BLOCK`) rather than from what storage currently says is claimed.

Two instances therefore lock onto the same numbers and stay locked. Driving the real exported
function with one shared storage, 130 numbers each:

| arm | tab A | tab B | numbers issued by both |
|---|---|---|---:|
| today, two tabs, one device | 0..129 | 64..193 | **66 of 130** |
| block claimed in storage before its first number only | 0..129 | 64..193 | **66 of 130** |
| every block, first and renewal, read-and-claimed in storage | 0..257 | 64..321 | **0** |

The middle row is the correction that looks right and is not: moving the claim earlier fixes the
*first* block and leaves every renewal colliding. No suite goes red for either version, because
nothing currently exercises two allocator instances over one storage.

The Mac sees the consequence as replay. `cloud-status.json` reports **209 of 773 inbound envelopes
dropped (21%)**, all `code=replay`, all from one device, and the log carries **1,665** of them
between 2026-09-15 11:00 and 2026-09-16 11:46, at 68–148 per hour through the night. The refused
sequences arrive in runs of about 60 — one block — with each number refused two or three times:

```
seq=118272 highest=118272     seq=118274 highest=118274
seq=118273 highest=118273     seq=118274 highest=118274
seq=118272 highest=118273     seq=118275 highest=118275
```

`CloudSequenceTracker` is behaving exactly as designed; its 1,024-bit window and its
"a claim is final" rule are both correct. The defect is upstream, in who chose the number.

**Design.** Every block, first and renewal alike, is read from storage and claimed there before a
number out of it is handed out. The allocator's contract becomes "a number this origin has never
issued", which is what the transport already assumes. The test is the table above: two instances,
one storage, zero overlap, in both interleaved and sequential open order.

### 2. The transport's replay refusal answers before the ledger's receipt can

`routeDurably` (`CloudAppBridge.swift:3503`) already implements idempotent replay properly:
`ledger.reserve` returns `.cached(outcome)` for a request identity it has seen, and the stored
status, code and payload go back to the caller. That is invariant 14 of the v2 design, shipped.

It is unreachable for a retry, because `claim(sender:sequence:)` runs at authentication and
refuses a repeated `seq` before the request identity is ever read. The refusal carries
`reply=notice` and the sentence "The sender sequence is not strictly monotonic." A client that
retries by re-sending the same envelope can never collect the answer the ledger is holding for it.

**Design.** The two layers answer different questions and should say so:

- The transport's question is *have I already accepted these exact bytes* — an anti-replay
  control, correctly final.
- The ledger's question is *have I already performed this effect* — an idempotency control, whose
  whole purpose is to make a retry safe.

So a retry must be a **new envelope sequence carrying the same `request_id`**, and the replay
refusal must say that, in a typed field a client can act on, rather than in prose about
monotonicity. With mechanism 1 fixed, a correct client already produces a new sequence; the
refusal text is what stops a person or a future client from concluding the request was rejected.

### 3. Capacity refusal is reported as permanent corruption

`failureDisposition` (`CloudAppBridge.swift:1278`) maps every `CloudOutboundSpoolError` to
`(.integrity, retryable: false)`. But `refuseAdmission` (`CloudOutboundSpool.swift:720`) raises
that same error type for `.rowCap`, `.byteCap` and `.fairness` — backpressure, not damage. The
occupancy those caps compare includes terminal rows still inside their ten-minute tombstone
retention, which the code comments say plainly.

The log carries **1,174 `stage=durable_reserve failure=integrity retry=permanent`** events,
arriving in bursts (443 in the 2026-09-15 20:00 hour, 428 in the 22:00 hour, 179 in the 2026-09-16
10:00 hour). Each one is a message that was never sent and will never be retried.

**Design.** Split the refusal domain. Capacity and fairness become their own typed error mapping
to `(.capacity, retryable: true)` with a retry-after derived from the oldest tombstone that GC is
waiting on. Corruption keeps `.integrity`. This is v2 invariant 5 — "backpressure is local and
typed" — applied to the store that already computes the bound.

### 4. A close needs a complete iTerm inventory it usually cannot get

`TerminalSafeClose.end` calls `stableTerminal` (`HostPorts.swift:744`), which refuses on
`!snapshot.isComplete` with "The terminal inventory was incomplete; nothing was closed." An
incomplete inventory is the normal case, not the exception:

| process proof | count |
|---|---:|
| `assistant process scan already in flight; using prior complete evidence` | 212 |
| `iTerm2 needs attention on this Mac … 有一個對話框在等回答` | 17 |
| `terminal inventory was incomplete` | 9 |
| `assistant process scan timed out` | 9 |
| `session scan recovered; complete inventory accepted` | 258 |

The 17 attention events are spread across the whole period (2026-09-15 13:49 through 2026-09-16
10:02), so this is a recurring state, not one bad afternoon. Four browser closes refused with
`code=terminal_io_failed`, which is 31% of the real ones. Two of them were the same session one
minute apart, and the third press on it returned 404 — the session had gone by then, so the person
was told "error, error, no such session" about a close that eventually happened.

**Design.** The close needs two different proofs and currently demands one:

- *Is the thing I am about to end the thing I meant* — an exact-tty process observation. This
  needs no iTerm inventory at all.
- *May I take the tab* — this is the part that needs iTerm, because iTerm owns the tab.

Separating them makes an incomplete inventory downgrade the outcome instead of cancelling it: the
assistant is asked to exit, signalled if it will not, and the tab is left open with a typed
`tab_left_open` result. The session ends, the person is told exactly what is still on screen, and
`iterm_attention_required` — which `StartPoints.swift:437` already returns on the start path —
becomes available on the close path so the console can say "there is a dialog waiting on the Mac"
instead of "terminal I/O failed". Staged truth is v2 invariant 7; this is the same idea one layer
down, in the host.

### 5. Token renewal closes the socket

`CloudTransport` renews the device token `refreshAhead = 60` seconds before a 300-second TTL and
completes the renewal by closing its own socket and reconnecting
(`CloudTransport.swift:2089`). That is **every four minutes, 436 times** in the window, each one
failing whatever was in flight: **1,864 `durable outbound drain failed failure=transport`**.

The error case exists precisely because this was once misread as the relay dropping the Mac — the
comment at `CloudTransport.swift:48` records that diagnosis from 2026-09-03. So the reconnect is
understood; its cost to in-flight control messages is not accounted for anywhere. Those frames are
retryable and do come back, so this loses no data; it spends a retry round on whatever a person
happened to press in that window.

**Design.** Renewal should not be a disconnect. Either renew in band on the live socket and let
the relay rebind the credential, or overlap: establish the next connection, move the send cursor,
then close the old one. A 300-second credential lifetime is a Cloud-side choice and is not the
thing to change first — a five-minute token with seamless renewal is fine; a five-minute token
that costs a reconnect is a four-minute sawtooth under every press.

### 6. One read lane, one giant parse

`drainReads` (`CloudAppBridge.swift:4197`) is a `while` loop that awaits one `performRead` at a
time. The `limit` of 16 background and 4 foreground is a queue-depth bound, not a concurrency
level. Inside `performRead`, an `info.summary` or `info.full` calls
`SessionInfo.recordFacts` (`RemoteServer.swift:4567`), whose cache key is
`(path, assistant, size, modified, maxBytes, claudeCache)` — so every append to an active
transcript invalidates it and the next read re-reads and re-parses up to 8 MiB synchronously,
in front of everything queued behind it.

Measured over the window: **2,081 of 3,718 `record_facts` calls missed (56%)**. Parse time on a
miss was median 744 ms, p99 18,963 ms, maximum 55,764 ms. End-to-end read latency was median
379 ms, p99 20,205 ms, maximum 163,643 ms; queue time alone reached **206,954 ms**.

Parsing is not intrinsically slow. The same 3.2 MB transcript parsed in **187 ms at 11:44:55 and
30,102 ms at 11:39:15** — a 160× spread on one file at one size. What differs is what else the
machine was doing, which is mechanism 8 below.

Two provider records on this Mac were 2.19 GB and 961 MB at the time of sampling, both still being
appended to by live Codex sessions. There is nothing wrong with those sessions; a seven-day
conversation has a large record. The defect is a read path whose cost scales with it.

**Design.** Facts become an incremental projection rather than a tail re-parse: a per-record
cursor of `(byte_offset, accumulated_facts, revision)`, advanced by parsing only the bytes appended
since the cursor, and a read answers from the last projection with its `observed_at` attached. The
request path stops parsing entirely. `Transcript.tailData` already seeks, so an offset-based
reader is a small addition rather than a new subsystem. This is the local half of v2's status
plane (§4.1) and v2 invariant 3, and it can ship before any Cloud-side replication decision —
it changes only who parses, and when.

Separately, and independently useful: control messages must not queue behind data reads. A
reserved lane for command receipts and refusals is v2 invariant 17 — "bulk bytes cannot block
control" — at its smallest useful size.

### 7. One spool, whole-file, four times per message

Every outbound state transition — `reserve`, `seal`, `sendNext`, `settle` — ends in
`store.commit(working)`, and `CloudFileSpoolStore.persist` (`CloudDurableStores.swift:1117`)
encodes the entire state and replaces the file: one temp write with `fsync`, then up to three
directory `fsync`s around two renames.

At 2026-09-16 11:57 that file was **498,035 bytes holding 601 rows, every one of them `acked`**.
The live payload was zero. Terminal rows are retained for `tombstoneRetention = 600` seconds, so
ten minutes of finished traffic is rewritten four times per new message — and, per mechanism 3,
occupies the caps that refuse new admissions.

The window on top of it is 8 rows (`CloudOutboundSpool.swift:214`, commented "implementation
default pending W6 measurement"). The process counters at the end of the window read
`window_refusal_attempts=57`, `writes_started=5247`, `writes_completed=5206`,
`writes_failed=41`.

The commit cost is not the file: bucketed by row count, the slow-tail median runs 500 ms at
100–149 rows and 295 ms at 950–999 rows — flat to slightly inverted. A 498 KB write plus the same
four `fsync`s, measured directly on the same volume, is **median 0 ms, maximum 2 ms** over 40
samples. So the seconds are not I/O and not serialisation volume; they are waiting, which again
points at mechanism 8.

**Design.** v2 §11 already retires the whole-file rewrite, and this page does not propose a
different replacement. Two bounded things are worth doing before it:

- A settled row's durable obligation is anti-replay, not content. Keep
  `(seq, logical_id, tombstone_at)` and drop the logical record at settle, the way
  `sealedEnvelopeBytes` is already dropped there. The file goes from 498 KB to a few KB without
  changing any protocol.
- Measure the window rather than shipping "pending measurement" indefinitely, and let the refusal
  name which bound was reached — the counters to do it with are already published.

## What the machine adds, and what nothing does about it

Mechanisms 6 and 7 both ended at "the seconds are waiting, not work". At 2026-09-16 12:07 this Mac
was:

```
memory   23G used of 24G, 141M unused, compressor holding 9,842M
swap     16,719M used of 17,408M; 71,951,372 swapins / 81,063,612 swapouts since boot 13 days ago
load     19.12  29.99  34.38   (14 cores)
```

and `/usr/local/share/NHIICC/nhiicc` — a duplicate of the health-card reader daemon, started from
`tw.gov.nhi.nhiicc2019.plist` while the one from `nhiicc2023.plist` holds the port — had been
spinning at **340–460% CPU for four and a half days** on a 2 MB resident set. Two
LaunchDaemon plists point at the same executable through `/usr/local/share/NHIICC` and
`/usr/local/share/nhiicc`, which the case-insensitive filesystem resolves to one directory.

**That daemon is not the cause of anything on this page, and this section first said it was.**
It was booted out at 2026-09-16 12:20, which dropped the load averages to 12.21/17.23/22.21 and
returned 881 MB of swap — a real waste, correctly removed. The latency did not change, because it
had already recovered 35 minutes earlier:

| five-minute bucket | reads | sessions published | signatures | slowest read |
|---|---:|---:|---:|---:|
| 11:35 | 6 | 16 | 110 | 83,602 ms |
| 11:40 | 22 | 18 | 158 | 55,469 ms |
| 11:45 | 10 | 44 | 126 | **193 ms** |
| 12:10 | 10 | 156 | 140 | 78 ms |
| 12:20 | 8 | 59 | 175 | 64 ms |

Between 11:45 and 12:20 the daemon was still eating four cores and the slowest read was 193 ms, so
it is not sufficient to produce the delay. The calm buckets also carry *more* traffic than the slow
ones, which rules out the other easy explanation — this is not a quiet machine looking fast.

What the slow windows do correlate with is the test suite. Of the 85 five-minute buckets holding a
read over 10 seconds, **45 (53%) are within 30 minutes of a suite fixture line; of the 154 buckets
where every read finished under 2 seconds, 21 (14%) are**. Three of the four refused closes fall in
suite-adjacent buckets; the 2026-09-15 10:01 one does not. And 12:05 carries 99 HTTP requests with
a 55 ms slowest read, so it is not "a suite is running" but *which* suite — the whole-module Swift
compile is what takes the machine, and the `.mjs` web segment does not.

That leaves roughly half the slow buckets unexplained. The honest state is: one suspect measured
and excluded, one contributor measured, and no account of the rest.

The architectural gap is the same either way, and it is the reason the cause was hard to find at
all: **there is no point on this path where the executor notices it is degraded and says so.**
Every layer instead waits its own timeout, in series, and the browser gets a spinner. A person
watching a session take 90 seconds to appear cannot learn whether a suite, a daemon or a swap
storm is responsible — and neither could this page, until it compared buckets. Slow disk appears
in v2 §6 as a backpressure cause; a degraded executor does not appear anywhere as a state.

**Design.** A host-pressure reading — available memory, load, compressor share — with three
consequences, none of which is a retry:

1. Status replication coalesces harder rather than queueing deeper.
2. The projection's `observed_at` age travels with every answer, so a stale card can say how
   stale it is. v2 invariant 9 asks for exactly this, as "recovery is a normal path, not a
   spinner".
3. The console can say the executor is saturated. Today a person watching a session take
   90 seconds to appear has no way to learn that a whole-module compile has the machine — and
   neither has the person diagnosing it, which is how a four-core daemon that was innocent came to
   be written down here as the cause.

## A bounded sequence

None of the following needs the section 13 product gate, Cloud-side storage, or the v2 planes, and
none of it introduces a concept v2 removes. Ordered by measured cost per unit of change:

| # | Change | Layer | Evidence it fixes | Verified by |
|---|---|---|---|---|
| 1 | Claim every sequence block in storage before use | browser | 209/773 inbound drops | two instances, one storage, zero overlap |
| 2 | Typed capacity error, retryable, with retry-after | Mac | 1,174 permanent losses | failure injection at the cap boundary |
| 3 | Drop the logical record at settle | Mac | 498 KB × 4 per message | file size after a settle burst |
| 4 | Split close identity proof from tab authority | Mac | 4 of 13 closes refused | inventory-incomplete fixture ends the session |
| 5 | Reserved lane for receipts and refusals | Mac | 207 s queue behind one parse | a long read cannot delay a refusal |
| 6 | Incremental facts projection | Mac | 56% miss, 56 s parse | parse bytes per read independent of record size |
| 7 | Overlapping or in-band token renewal | Mac + relay | 436 reconnects, 1,864 drops | no in-flight failure across a renewal |
| 8 | Host-pressure state, surfaced not swallowed | Mac + web | the 160× parse spread | a saturated executor is visible in the console |

Items 1–4 are each small and independently landable. Items 5 and 6 are the ones that move the
percentiles, and both are the local half of something v2 specifies — doing them here does not
fork that design, but it does mean re-reading v2 §4.1 and §6 before starting, not after.

## What this page does not establish

- No measurement here was taken on an idle machine, and mechanisms 6 and 7 are contaminated by
  whatever else the machine was doing by an unknown factor. The 160× spread on one file bounds it
  from below; nothing bounds it from above. Re-take every number in a bucket with no suite within
  30 minutes, or a before/after of any change above is a comparison between two different machines.
- **The load attribution in this page was wrong once already.** The duplicated daemon was named as
  the amplifier on the strength of its CPU share alone, and the bucket comparison that excluded it
  was only run when removing it changed nothing. A resource hog that is genuinely wasting four
  cores is not thereby the cause of a specific latency; the test that separates them is a window
  where it is present and the symptom is absent.
- The correlation between commit slow-tail p95 and every in-app load signal was weak
  (|r| ≤ 0.23 per minute over 947 minutes against commit count, row count, signature count, read
  count and fixture count). Per-minute fixture count was the wrong shape for the suite question —
  fixtures appear in a few seconds of a run that occupies the machine for far longer, which is why
  the five-minute adjacency comparison above sees a 3.8× separation the correlation does not.
  Neither rules in a cause for the remaining half of the slow buckets.
- Mechanism 1's collision is proven in the allocator and its consequence is proven in the Mac's
  counters. The link between them — that this device's two tabs are what produced those
  sequences — is inference from the run-of-60 pattern, not an observation of that browser.
- Nothing here has been implemented, and none of it is a Cloud deployment claim.
