> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.

# The read path

This page is about how a client asks this app for state, and why the current answer is one very
large object. It names the four mechanisms that produce that object, what each one costs when
measured, and the resource model that replaces them. It is a design assessment, not a landing
receipt: nothing here has been implemented.

Latency of individual services is a different page, `runtime-performance.md`.
The transport file's own decomposition belongs to
`architecture-refactor.md`, whose Phase 2 is still gated; the routing
section below is a proposal *under* that document, not a parallel plan.

## How these numbers were taken

All readings are from the running app on the broker Mac, 2026-09-16, with a store holding 702
Board items and 716 orchestrator task records. Re-take them the same way before and after any
phase; a number quoted from this page without re-measuring is a number about a tree that no
longer exists.

```console
$ TOKEN=$(cat ~/.config/clawdline/remote-token)
$ curl -s -o board.json -w '%{size_download} %{time_starttransfer}\n' \
    -H "Authorization: Bearer $TOKEN" \
    'http://127.0.0.1:7717/v1/board?project=<id>'
$ curl -s -N --max-time 20 -H "Authorization: Bearer $TOKEN" \
    http://127.0.0.1:7717/v1/events -o events.raw
$ wc -c events.raw
$ awk '/^event: /{n=$2} /^data: /{print n, length($0)-6}' events.raw
```

Per-field bytes come from summing `tojson|length` over each item, grouped by key.

## What one read costs today

| request | bytes | contents |
|---|---:|---|
| `GET /v1/board?project=<id>` | 1,000,265 | 421 item cards + 33-project catalog; **58 further items omitted** |
| `GET /v1/board?project=<id>&item=<id>` | 58,270 | one 19,428-byte item — and the 35,551-byte catalog again |
| `GET /v1/board` | 36,646 | the catalog alone |
| `GET /v1/orchestrator/tasks` | 4,290,975 | 716 task records, fully expanded, unpaged |
| `GET /v1/projects` | 122,955 | |
| `GET /v1/sessions` | 24,396 | |

No response carries `ETag`, `Last-Modified` or `Content-Encoding`. Every one is
`Cache-Control: no-store`, and the Board page re-reads its full 1 MB every 15 seconds
(`Resources/web/app/js/view/board.js`, the `schedule(…, 15000)` in `later()`), replacing
`state.items` wholesale and rebuilding the whole list DOM from `clear(target)`.

### Where the 1 MB goes

Of 963,516 bytes of `board.items`:

| field | bytes | share |
|---|---:|---:|
| `progress` | 339,507 | 35.2% |
| `listSummary` | 141,377 | 14.7% |
| `usage` | 117,770 | 12.2% |
| `cardSummary` | 66,109 | 6.9% |
| `deliveryLanes` | 57,308 | 5.9% |
| `title` + `summary` | 42,170 | 4.4% |
| `id` + `projectId` + `owner` | 43,699 | 4.5% |

Inside `progress`: `evidenceCounts` 67,416, `coordinationContext` 38,737, `attemptCounts` 34,945,
`reason` 26,187, `sessionDelivery` 24,064, `measurement` 17,786, `basisCodes` 15,586.

`coordinationContext` and `sessionDelivery` together are 62,801 bytes and have **no reader
anywhere in `Resources/web/app/js`**. `sessionDelivery.summary` carries up to 623 characters of
delivery narrative per card. They are shipped four times a minute to be discarded.

## The four mechanisms

### 1. One grain

`/v1/board` is five different responses behind one query string — catalog, project, item, report,
collection — selected inside `ProjectBoardHTTP.PreparedRead.execute()`. Only one of the five is
narrow. The project response is defined as *everything in this project that fits in a megabyte*,
which is why 58 items currently do not exist as far as a viewer is concerned:
`board.truncation.reason` is `snapshot_byte_budget`.

A byte budget is what a system reaches for when it has no page size. It is not a bound on the
answer, it is a bound on the *truthfulness* of the answer, and it fails silently and unevenly:
the two largest items in the store are 181,148 and 172,540 bytes, so which 58 items vanish
depends on who wrote a long delivery summary that week.

The item read shows the same shape from the other side. 61% of a 58 KB "one item" response is the
project catalog the client already holds.

### 2. No conditional read

The store keeps a monotonic `revision`; the read model carries it; the client compares it
(`board.revision < state.revision` is already a guard in `load()`). Every piece of a conditional
protocol exists except the protocol. So a client that knows it is level still pays 1 MB to find
out, every 15 seconds, per viewer, and the Mac re-runs `JSONSerialization` over the same
dictionary tree to tell it so.

### 3. The stream is a snapshot pipe

`RemoteServer.broadcastOrchestrator()` serializes the whole task snapshot and writes it to every
connected stream on every task change, from 25 call sites. The comment above `write(event:)` states
the design intent plainly: *"the stream carries the entire list on every change rather than a
diff."*

Measured over 20 seconds, one idle client:

| event | frames | bytes each | total |
|---|---:|---:|---:|
| `orchestrator` | 4 | 234,234 | 936,936 |
| `sessions` | 7 | ~22,600 | ~137,000 |
| `transcript` | 11 | ~62 | ~680 |
| **total** | | | **1,098,140** |

Diffing consecutive frames leaf by leaf:

- consecutive `orchestrator` frames differ in **2 to 3 leaves**, and they are
  `/at`, `/tasks/0/executor/observed_at`, `/tasks/0/executor/inventory_generation`. 234 KB to
  advance a clock.
- alternating `sessions` frames differ in **exactly one leaf**, `/scan/completed/sequence`.

That is roughly 3.3 MB/min per idle viewer, and about 7.3 MB/min — 440 MB/hour — with the Board
page open.

The asymmetry worth noting: the **Cloud publish path already solves this**.
`CloudAppBridge.publishOrchestratorIfChanged` computes a projection identity, returns without
sending when it equals the last published one, and coalesces inside an interval. The local stream
has neither. The mechanism does not need inventing; it needs applying on the other side of the
same function.

### 4. Write amplification, and logs inside the aggregate

`~/Library/Application Support/Clawdline/project-board.json` is 5,032,820 bytes:

| branch | count | bytes |
|---|---:|---:|
| `items` | 702 | 4,479,833 |
| `receipts` | 2,292 | 639,991 |
| `taskItems` | 699 | 85,279 |
| everything else | | ~27,000 |

and inside those items: `history` 1,835,259, `evidence` 940,091, `links` 657,800,
`sessionDeliveries` 164,495, `obligations` 105,157, `catalogDisposition` 100,568, `spans` 92,809.

`ProjectBoardStore.persist` re-encodes that whole document with `JSONEncoder(.sortedKeys)`, writes
it atomically and `fsync`s it — **on every mutation**. 2.8 MB of that is append-only log that
nothing in the mutation touched. Two items alone (286 and 302 history entries) account for 353 KB
rewritten every time anything anywhere changes.

Then `ProjectBoardIntegration.materializeReadModel` rebuilds **all 33 project envelopes** for any
dirty reason, embeds the full 35.5 KB catalog into each one (≈1.17 MB of duplication held in the
cache), and calls `boundedSummaries`, which runs `JSONSerialization.data` on every card
individually just to measure its size.

## What the architecture should be

### A. One response, one resource

The rule that makes everything else hold: **a response carries exactly one resource, plus links to
reach the others.** No response embeds a sibling collection.

```
GET /v1/board/projects                      catalog only
GET /v1/board/projects/{id}                 header, counts, links
GET /v1/board/projects/{id}/items           paged cards
GET /v1/board/items/{id}                    detail — no catalog
GET /v1/board/items/{id}/history?cursor=    paged
GET /v1/board/items/{id}/evidence?cursor=   paged
GET /v1/board/items/{id}/reports/{report}
GET /v1/orchestrator/tasks?cursor=&state=   paged, projected
```

That one rule removes the 35.5 KB catalog from every item read and the 33× catalog duplication
from the cache, without any argument about which fields matter.

### B. Two projections, typed

The card and the detail are different objects and should be different Swift types, not two
different dictionary shapes produced by copying a `stableFields` string array.

- `ListCard` — what the list renders and filters on: the stable identity fields, `listSummary`,
  `cardSummary`, a `usage` subset, and the part of `progress` that has a reader.
- `ItemDetail` — everything else, read only when a card is opened.

**Corrected 2026-09-16.** The first draft of this page put a ≤600-byte target on a card and listed
`progress.{group,state,active,label,historical}` as the whole of what a card needs. Checking the
readers instead of guessing at them: `view/board.js` also reads `progress.basisCodes` (by
`includes` and by a `declared_.+_span` pattern), `progress.warningCodes` for its count,
`progress.evidenceCounts.landings`, `progress.attemptCounts.{active,succeeded}`, and the whole of
`progress.measurement` and `progress.lifecycleEstimate` for `epic` and `refactor` rows. Those stay.
What leaves is `coordinationContext` and `sessionDelivery`, which have no reader at all — 62,801
bytes, 6.5% of the payload.

So a card lands near **2,140 bytes**, not 600, and **the byte win in this phase is small.** The
saving that matters here is pagination: fifty cards instead of 421, and 58 items that stop being
invisible. A phase whose stated number was wrong by 3.5× is worth saying so rather than quietly
restating.

`coordinationContext` and `sessionDelivery` leave the card by construction.

Typing this is the part that stops the regression. A `struct ListCard: Encodable` makes
"what is in a card" a reviewable declaration; re-adding a 600-byte narrative becomes a visible
change to a type rather than a key surviving a dictionary merge.

### C. A revision protocol, used by every read

- responses carry `ETag: "board:<revision>"`;
- `If-None-Match` returns `304` with no body;
- `?since=<revision>` returns `{revision, changed:[…], removed:[…]}`;
- a `since` older than the retained change log returns `409 revision_too_old` and the client
  refetches, rather than being handed a half-generation — the same failure mode
  `catalog_search_revision_conflict` already handles for catalog search.

This needs one thing the store does not have yet: a per-item `revision: Int` stamped from the
existing counter. `updatedAt` is a float timestamp and cannot order a `since` query safely.

The 15-second poll then costs about 200 bytes on the polls where nothing changed, which is most of
them.

### D. The stream carries changes

In order of payoff:

1. **Identity dedup on the local stream**, reusing `cloudOrchestratorIdentity`. Write nothing when
   the projected identity is unchanged. On the measurement above this alone removes 936,936 of
   1,098,140 bytes in 20 idle seconds.
2. **Take the clock out of the state.** `at`, `observed_at` and `inventory_generation` must not be
   inside the identity. A clock tick is a `tick` frame or it is nothing.
3. **Delta frames.** `event: orchestrator` becomes `{revision, changed, removed}`; a client with no
   revision or too old a one gets one full snapshot on `hello` or on an explicit `resync`.

All three apply equally to `sessions`, where half the frames carry a single changed integer.

### E. The log out of the aggregate

**Corrected 2026-09-16, after reading the store rather than its field sizes.** The first draft of
this page said `history` *and* `evidence` were append-only logs living inside the aggregate. Only
one of them is.

`history` is: `StoredHistory` is `id`, `at`, `actor`, `kind`, `summary`, `sourceTaskId`, and
nothing in the store writes to an entry once it exists. `evidence` is not: `resolved`, `status`,
`eventAt` and `eventOrdinal` are all updated in place by the reconciliation paths. An append-only
file for evidence would need last-write-wins compaction on every read, which is a different and
riskier change than the one this phase describes.

So the move is:

```
project-board.json                           aggregate state, ≈3.2 MB
project-board-history/<itemId>.jsonl         append-only
```

Per-mutation write cost drops from a 4.8 MB re-encode plus `fsync` to ≈3.2 MB plus one append —
**1,835,259 bytes off every write, 36.5%**, not the 2.8 MB the first draft claimed.
`maximumHistory = 2000` stops being a cap that silently drops an item's oldest entry and becomes a
retention policy over a file, and `GET …/history?cursor=` becomes a bounded read of one file.

Ordering is part of the design: the log is appended **before** the aggregate is persisted, so a
half-failure leaves an extra entry rather than a lost one. Every entry carries its own `id` and
reads de-duplicate on it, which makes an extra line recoverable and a missing line not.

The general answer for the rest — evidence included — is **per-item segment files**: the aggregate
becomes an index and each item's heavy arrays live in a file rewritten only when that item changes,
which takes a single-item mutation from 4.8 MB to about 5 KB and works for mutable data too. It
also breaks the store's single-atomic-write property and needs a journal, so it is a phase of its
own and not a detail of this one.

### F. The read model gets grains

- per-project cache entries keyed by `(projectID, revision)`, invalidated per project rather than
  all 33 together;
- one card index `[itemID: ListCard]` plus a membership index `[projectID: [itemID]]`, instead of
  33 arrays each holding its own copy of the catalog;
- the catalog held once and linked, never copied into an envelope;
- **pre-serialized `Data` per resource**, so an unchanged poll costs a pointer rather than a
  re-serialization of the same tree.

`boundedSummaries` disappears. A typed card has a known bounded size; pagination replaces the byte
budget; the 58 silently-omitted items become a second page a client can actually ask for.

### G. A routing layer

`RemoteServer.dispatch` is a single **2,286-line** function with **108 route cases**, inside a
5,839-line file, and it is where path parsing, auth, entitlement and domain logic currently meet.
Roughly forty routes are matched by `hasPrefix`/`hasSuffix`/`dropFirst` string surgery on the raw
path.

```swift
protocol Resource {
    static var route: RoutePattern { get }        // "/v1/board/projects/{id}/items"
    static var access: Access { get }             // declared, not re-derived per branch
    func get(_ request: Request, _ path: PathParameters) throws -> Response
}
```

- one route table built once, with path parameters extracted before any handler runs;
- one file per resource domain — which is also what this repository's own rule about changing
  `RemoteServer` requires;
- access expressed as a property of the resource instead of the `if path.hasPrefix(…)` chain
  currently spread across lines 900–990.

This buys no bytes. It is what stops phases A–F from being undone one convenient special case at a
time, and it is the same work `architecture-refactor.md` has already scoped for this file.

## Phasing

| # | change | measured cost removed | risk |
|---:|---|---|---|
| 1 | ~~identity dedup on local SSE (`orchestrator`, `sessions`)~~ **landed** | ~937 KB per 20 s per viewer | low — the mechanism already exists on the Cloud path |
| 2 | ~~drop `coordinationContext` + `sessionDelivery` from cards~~ **landed** | 62.8 KB per board read | low — no reader exists |
| 2a | ~~a test over the card field set~~ **landed** (`Tests/ReadPathTests.swift`) | — | it asserts both directions, and asserts that it can go red |
| 3 | ~~`ETag` / `If-None-Match` → `304` on board reads~~ **landed on the new routes** | ~1 MB per unchanged poll | low — additive |
| 4 | ~~split `/v1/board` into project / items / item resources~~ **landed; no client yet** | 35.5 KB per item read; first paint 1 MB → ~150 KB | medium — wire change |
| 5 | ~~typed `ListCard` + pagination replacing the byte budget~~ **landed** | ends the silent loss of 58 items | medium |
| 6 | `?since=` deltas on reads; delta frames on the stream | near-zero idle traffic | medium |
| 7 | ~~`history` out of the aggregate~~ **landed as a window, not a move** | ~1.69 MB per durable write | the document keeps 20 entries; the log keeps the rest, so no schema change |
| 7a | per-item segment files (evidence included) | ~4.8 MB → ~5 KB per durable write | high — breaks single-atomic-write; needs a journal |
| 8 | route table, one file per resource | none directly | medium |

**Standing, 2026-09-16.** Phases 1 through 5 and 7 are landed and type-checked, the Board page
reads through the audience route on both transports, and all 49 web suites pass. What remains is
**6** (`?since=` deltas and delta frames on the stream — built in `SnapshotDelta`, not wired) and
**8** (adopting the route table across `dispatch`'s 108 cases).

Two phases turned out differently from the plan, and both are recorded where they landed rather
than quietly:

**5 needed no new type.** `ProjectBoardReadCache.Model.cards` keeps every card untruncated and the
paged resources read that. The field contract in `BoardCardContract` was the part that mattered.

**7 is a window, not a move.** Taking history out entirely means a schema migration over 706 items
of real durable state, and this session could not run the app to watch it happen. The document
keeps the most recent twenty entries and the log keeps everything — about 92% of the saving,
without a schema version, a reverse migration or a new failure mode. The complete move, and the
per-item segment files in 7a, remain available and are now smaller changes than they were.

## What this page does not establish

- These are one machine, one store, one day. The orchestrator frame was measured **idle**; under
  active dispatch it is larger and more frequent, so 234 KB is a floor.
- A phase's predicted saving is a prediction. Each phase re-takes the measurement above and
  records the pair, or its number does not count.
- Nothing here is authorization to change wire, persistence or concurrency semantics. Phases 4–8
  each need their own brief.
