# SQL Bot read views

`projectassistantstore.NewReadViews(db *sql.DB) (*ViewStore, error)` implements
`app/projectassistant.ReadViews`. The host lends its existing query-only reader;
the adapter neither opens nor closes a database, changes pragmas, installs
schema/indexes, mutates facts, nor invokes a provider. The old Repository,
EventFeed and Export contracts remain separate. See the shared
[read-view contract](project-assistants-read-views.md) for transport and client
responsibilities.

## Ownership and transaction

The constructor retains the borrowed pool and generates a private random HMAC
key and public source epoch. There is no registry, cache, expiry timer or retry.
Trusted composition supplies `AuthorizationBinding`, `ByteLimit` and
`CursorByteLimit`; absent/invalid budgets and ceilings above the existing Cloud
plaintext/cursor bounds are rejected. Domain request validation runs first.

Each `Read` starts one read transaction. Its first SELECT obtains stored Bot
identity/Project, Conversation/current fence, Bot version, memory revision and
the **machine** maximum event sequence. Every later selected fact, count and
reference belongs to that same SQLite snapshot. All rows are closed and the
transaction ends before returning; no SQL lifetime crosses serialization or
other IO. A missing Bot/conversation, malformed required shape, mismatched
identity, missing current binding or broken selected collection relation is an
error, not an authoritative empty result. Legacy null/missing collections and
empty arrays/maps remain valid; null members inside populated collections do
not silently disappear.

The aggregate outer properties are PascalCase; domain properties are
snake_case. Queries explicitly CAST the persisted BLOB to TEXT. JSON functions,
MATERIALIZED CTEs and window functions operate on the existing tables; there is
no Go aggregate unmarshal, `load`, Export, Repository.Bot or Conversation call
on the read path.

Subsequent statements materialize the stored header scalars and collection
types once before iterating members. Fence predicates still compare every
required key, Project, conversation and execution component, but use those
scalar columns rather than extracting each header field from the complete
aggregate for each member. The aggregate and returned member JSON remain TEXT;
raw-byte accounting and complete-object decoding are unchanged. This is a
statement-local SQL projection, not a retained cache or schema change.

## Complete control state and paged facts

Snapshot contains the complete stored Bot, every counted accepted/queued/
running/waiting-permission summary across generations, their user messages and
the last terminal current-fence turn summary. The existing16 counted-turn bound
uses a sentinel and rejects overflow. Unknown turns are an exact separate
cross-generation count:17 unknown plus16 counted turns is valid. Captured turn
instructions and failure narratives never enter a compact turn summary.

Head includes exact current-fence pending-offer and latest-per-turn trace
counts by default. An API 113 client can set `defer_trace_count` on chat
reads to skip the complete trace aggregation and validation until the user
opens the trace collection. In those reads, the trace count field is zero
even if traces exist. Traces mode still returns the exact count and validates
the stored trace chunks. Permissions and traces have separate summary pages
and signed detail references. The chat requests 20 rows on entry and uses
the signed cursor for earlier messages. Detail returns one complete stored offer (including every
option) or trace; it does not invent tool/question text or authorize an answer.
Trace references identify the selected latest event; superseded references
expire. Runtime permission/fence checks remain authoritative.

Snapshot/history merge messages and WorkLinks into a single descending
`(anchor_message_seq, kind, stable_ID COLLATE BINARY)` order. Work anchors come
from Turn.MessageID to Message.Seq. There is one total page allowance, with a
sentinel and no skipped ties. History is LIVE across requests, not a retained
snapshot. MarkRead/version changes and new generations do not expire historical
positions; durable old-generation message/work facts remain readable. Later
events reconcile work facts using the observation frontier.

Events check stored source identity without loading the aggregate. They return
at most the existing256 rows plus an internal sentinel, reject ahead/old-epoch
cursors and advance only to the last included event. An empty page preserves
the requested sequence. Nested message/turn/permission/work fences, memory,
Bot/read/source scopes and capabilities are checked independently of the outer
envelope; trace ownership and event binding use scalar same-transaction SQL
checks. Only the initial snapshot head may start client replay. Page heads never
advance an existing replay cursor.

## Two separate byte boundaries

The trusted `ByteLimit` is one shared SQL-to-Go **raw-byte prefix budget**, not
an allowance for each row. The first SELECT CASE-checks the combined metadata
bytes before exposing any metadata JSON to Go. Pending-summary/user-message
bytes and the last terminal summary consume the remaining mandatory budget.
Candidate timeline/event/permission-summary/trace-summary queries use an
ordered SQL `sum(length(...)) OVER (...)` and CASE to expose only the fitting
prefix. Event row IDs are charged too, including a corrupt ID that differs from
its JSON payload. Full detail similarly checks its raw size against the
remaining budget before crossing into Go.

Consequently the sum of returned source JSON texts is at most `ByteLimit`,
bounded by the existing8MiB ceiling, plus small internal failure metadata.
Selected ID columns duplicate IDs already inside their bounded JSON; parsed Go
objects, temporary strings and signed references add overhead. This is not a
claim that total Go allocations or RSS equal8MiB. There are at most100 (events:
256) candidate objects, each continuation/detail token is at most the trusted
cursor budget (never above4096 UTF-8 bytes), and no growing retained history.

Since schema 17 a page reads its rows through the member index: the timeline
starts at the page's floor (below), and the head, pending and last-turn
statements look members up by ID or position. SQLite may still materialize the
Bot's turn sets for the own-chat filter and order the page's candidates; that
engine work is bounded by the Bot's turns and the page, not by its message
history, and it is not bounded by the returned JSON budget.

The source budget does not estimate HTTP/Cloud escaping or envelope overhead.
After the transaction, the transport measures actual complete generated JSON
and its actual Cloud correlation envelope against the existing ceiling. The
adapter supplies `AfterCursor`, `CursorFailure` and `Failure` per candidate;
none are wire fields. An oversized next object yields a payload-free failure
sentinel, so a legal earlier prefix can be published with honest continuation.
The oversized object is not skipped. A first oversized object has no publishable
prefix and must become typed capacity failure.

Each cursor prefix is judged independently. An unencodable intermediate position
does not invalidate a later fitting prefix. If packing really must stop at a
position with no legal continuation, it must refuse instead of pretending the
page ended. Mandatory data is never sliced to make a success response fit.

## Cursor isolation

HMAC domain separation binds the reader epoch, trusted authorization digest,
stored Bot/Project/conversation, collection and position. Permission/trace
collection cursors and detail references additionally bind the complete current
execution fence. History deliberately does not bind Bot.Version. Tokens are
strictly decoded, checked in UTF-8 bytes and never truncated. Rebuilding a
reader expires old references even when a fresh restore has identical version
and sequence values. Same-process replacement of a DB file without rebuilding
the reader is unsupported and is not detected by an invented heuristic.

## Verification boundary

The adapter tests use synthetic identities and `t.TempDir` databases, actual
Store Commit/Restore and the existing modernc SQLite driver. They cover legacy
null/empty data, malformed required metadata and members, missing relationships,
source/authority/epoch/cursor isolation, UTF-8/lowered budgets, counted/unknown
control separation, all-once600-message/101-work traversal,101 offers each with
101 options, current-fence details, MarkRead and generation changes, shared raw
prefix budgeting, giant trace/captured-instruction data, nested event scopes and
borrowed-pool ownership. A test-only SQL driver barrier pauses the actual first
SELECT after its first row: a second WAL handle commits old-generation work and
message events603/604 while the reader still reports frontier602 owned by a
different Bot. Event resume reads exactly those later facts.

These are module tests, not Host/HTTP/Cloud/native/phone/removal acceptance.
The integrating root owns exact-tree full-gate and deployed-surface evidence.
There are zero product-model or paid external API requests in this module work.
Removal consists of detaching the optional ReadViews composition and deleting
the three adapter sources/two tests/this document; no schema or stored-data
rollback is necessary.

### Long-history source deadline

Initial/recovery snapshot reads exposed a failure on a legal long conversation.
The independent Store regression fixture writes 9,001 contiguous messages
(5,209,366 JSON bytes), 600 durable source events and another Bot's creation
event, which owns machine frontier 602. It uses real Store Commit and ViewStore,
synthetic fixed-length scopes, the existing 100-row page and the original
60-second read context. Fixture creation took 130ms in the failing run; the
head took 76ms, then the first fact-shape SQL statement exhausted the context.
Snapshot returned `context deadline exceeded` after 60.010s. This is separate
from the transport fixtures with different scope lengths and JSON byte counts.

The saved CPU profile contains 53.33s sampled CPU, including 52.83s cumulative
under SQLite's JSON extraction, 52.73s under its JSON cache search and 51.82s
flat in the underlying byte comparison. EXPLAIN QUERY PLAN and the captured SQL
retain the aggregate lookup and member scans; materializing scalars removes
the repeated large-TEXT header lookups from the per-member predicates. The
profile does not establish a bound on SQLite CPU, memory or parse work.

With those statement-local scalars, the same fixture and deadline returned a
100-row snapshot in 2.414s, its 100-row history continuation in 2.121s and an
empty events page in 344ms. Every returned message equals the complete stored
object, the continuation starts at message 8,901, frontier remains 602, the
initial events are not replayed, an ahead cursor is still typed `cursor_expired`,
and no transaction remains borrowed after Read. The regression also checks a
100-row events prefix against complete stored source-event objects. A separate
SQL probe records each statement and its duration; its snapshot shape/relation checks took
382/376ms. These are local synthetic source measurements with substantial
margin inside the existing deadline, not complete Cloud latency or a production
ceiling. Real usage frequency has not been measured for this unreleased feature.

The retained implementation adds one scalar-projection boundary and a behavior
regression test; it keeps the existing shape, fence, relationship and byte-budget
checks. Malformed facts, nested foreign events, missing event bindings and
Trace-to-Turn ownership remain refusals. The bounded transport/Bridge deadline
guard and exact response-envelope verification belong to the integration owner.

### Read-path performance follow-up (2026-11-20)

The statement still materializes the scalar projection `a`. SQLite can inline
its inner Bot lookup, eliminating a second temporary copy of the aggregate.
The HTTP packer still checks each legal prefix against the same local and Cloud
byte limits. It counts the bytes of the already escaped JSON items and measures
the fixed Cloud wrapper with the existing serializer, then assembles the
selected page once. The cost is exact byte accounting in the packer, covered by
the Cloud serializer's exact-fit and one-byte-overflow tests. There are no new
dependencies, indexes, migrations, or cache lifetimes.

The timed path is Cloud Bridge → Router → HTTP → SQLite → serialized reply.
The source baseline was commit `775a8e8eb`. On an Apple
M4 Pro (24 GB, darwin/arm64, Go 1.27.1), each fixture ran five times per binary,
in alternating baseline/candidate order under shared machine load. P95 is the
maximum of five samples. Snapshot opens a conversation, history loads the next
older page, and events resumes at the current frontier with no unseen events.

| Synthetic fixture and read | Baseline median / P95 (ms) | Candidate median / P95 (ms) |
| --- | ---: | ---: |
| 9,001 short messages, snapshot | 1560 / 3995 | 1106 / 1204 |
| 9,001 short messages, history | 1375 / 1596 | 1157 / 1224 |
| 9,001 short messages, events | 338 / 455 | 280 / 374 |
| 200 rich messages, snapshot | 602 / 609 | 346 / 635 |
| 200 rich messages, history | 626 / 665 | 326 / 417 |
| 200 rich messages, events | 220 / 257 | 168 / 296 |

The fixtures read 100 messages per page. The rich fixture uses about 8 KB of
plain text per message, with Traditional Chinese and HTML-escaped characters.
The baseline profile placed 4.53 seconds of sampled CPU under the SQLite view
reader on the long fixture, with temporary B-tree writes visible in the stack.
On the rich fixture, 340 ms of sampled CPU were under repeated page packing.
The rich snapshot and empty resume tails varied. These samples do not establish
a tail improvement for those cases. The owner-local monitor exposed no
attributed Bot route in the sampled 24-hour interval, so real route frequency
is unknown. The fixtures exclude relay/network transit, browser rendering and
provider execution. Both snapshot fixtures met the existing 60-second budget.

Reproduce with the same test files on both commits. Build the baseline with
`go test -overlay=/path/to/baseline-overlay.json -c -o /tmp/bot-before.test ./internal/transport/http`,
mapping only `internal/adapters/projectassistantstore/read_view_query.go` and
`internal/transport/http/project_assistants_view.go` to copies from `775a8e8eb`.
Build the candidate with `go test -c -o /tmp/bot-after.test ./internal/transport/http`.
Run both from `internal/transport/http`, alternating order and repeating five times:

```sh
"$BOT_TEST_BINARY" -test.v \
  -test.run '^TestProjectAssistantViewServer(LongHistoryFitsExistingCloudReadDeadline|RichHistory)$'
```

### Reuse event validation inside a read transaction

An event page can contain many updates with the same persisted binding or
trace turn. Verify a binding once per `(binding ID, generation)` and a trace
turn once per `(binding ID, generation, turn ID)`. Each result belongs to the
current SQLite transaction and page. The per-page row limit bounds both maps.
All event envelopes, nested facts, foreign-source checks and publication
authority are checked as before. Different bindings and generations still
require independent checks, and no result survives the read transaction.

`TestProjectAssistantViewServerBoundEventReplay` uses a 9,001-message Bot,
100 source events referencing one real stored binding, and a later machine
frontier from another Bot. It asserts that every event keeps its sequence and
binding, with no phantom continuation. Through the in-process Cloud Bridge,
100-event replay fell from **7,391 to 988 ms** median and from **19,776 to
1,575 ms** nearest-rank P95. Five alternating runs per binary on an Apple
M4 Pro (24 GB, darwin/arm64, Go 1.27.1) under shared machine load supplied the
samples. The preceding integrated source was `641717fab`. An empty event page
and the conversation snapshot and history checks showed variable times. Those
operations do not reuse validation across a page of events. They establish
no performance gain for these cases or for relay/browser transit.

To repeat, build both trees with this same test fixture, using
`go test -c -o /tmp/bot-replay.test ./internal/transport/http`. For the baseline,
use `-overlay` with `read_view.go` and `route_stats.go` from `641717fab`.
Invoke both binaries five times, in alternating order, from
`internal/transport/http`:

```sh
"$BOT_TEST_BINARY" -test.v -test.run '^TestProjectAssistantViewServerBoundEventReplay$'
```

Use the `mode=events after_seq=1` completion for the backlog read. A separate
trial that slimmed snapshot metadata and timeline relation SQL did not produce
a consistent improvement on both complete-page fixtures and was reverted.

### Validate history without materializing the timeline (2026-11-23)

Opening a direct Bot chat requests a snapshot; scrolling back requests a history
page. Both validate persisted facts before returning up to 100 visible rows.
On the existing 9,001-message store fixture, five instrumented reads spent a
median 414 ms in `viewValidateFacts` (p95 454 ms). The timeline read itself
took a median 158 ms (p95 183 ms). The validation query rendered each object
as JSON text for repeated field lookups and materialized full message bodies
to check relationships.

The shape checks now read SQLite's binary JSON values with `jsonb_each`.
Relationship checks materialize IDs, sequences and fence fields. Both still
inspect all persisted rows in the same transaction. The change uses the
existing SQLite dependency and adds no schema, cache or provider work.
The SQL maintains a scalar projection specifically for validation, while the
timeline retains its payload and artifact-title projection. The corruption
fixture also checks duplicate IDs, duplicate positions and missing turn
messages with a one-row page.

#### Cloud Bridge comparison

Baseline `889075cd4`, Apple M4 Pro (24 GB, darwin/arm64, Go 1.27.1). Each
variant ran 12 times per fixture under shared machine load. One round used
each binary sequentially with a new database per fixture. The six permutations
of the three variant orders were run twice. Reported p95 is nearest-rank (the
maximum of 12 samples). Times are milliseconds for Bridge → Router → HTTP →
SQLite → serialized reply. They exclude network transit, browser rendering
and provider calls.

| Fixture and read | Baseline median / p95 | Binary JSON shape only | Binary JSON and scalar relationships |
| --- | ---: | ---: | ---: |
| 9,001 short messages, snapshot | 801 / 988 | 721 / 1217 | 608 / 786 |
| 9,001 short messages, history | 740 / 992 | 657 / 1485 | 580 / 943 |
| 9,001 short messages, empty event resume | 185 / 211 | 211 / 297 | 203 / 365 |
| 200 rich messages, snapshot | 231 / 296 | 209 / 287 | 205 / 328 |
| 200 rich messages, history | 217 / 311 | 192 / 251 | 195 / 278 |
| 200 rich messages, empty event resume | 70 / 111 | 70 / 151 | 72 / 138 |

The scalar-only trial moved median validation from 414 to 388 ms in five
instrumented reads, with overlapping samples. The binary-JSON-only candidate
cut long-history medians but had the slowest long-history tails. The combined
change cut median source time by 193 ms on first open and 160 ms on the older
page and improved both long-history p95s. The rich snapshot p95 increased.
Event resume skips this validation and serves as a control. Its spread shows
that p95s on this shared machine are sensitive to load, so the measurements
support the long-history median comparison more strongly than the tails.
The existing 60-second reader deadline was met in every Bridge run. Neither
the Bridge sample nor the existing route aggregate measures how often people
open a history this long or how long a phone waits before the first message
appears.

Build each comparison revision from its checkout, using the existing fixtures:

```sh
go test -c -o "$BOT_TEST_BINARY" ./internal/transport/http
```

Then run from `internal/transport/http` in the order described above:

```sh
"$BOT_TEST_BINARY" -test.v \
  -test.run '^TestProjectAssistantViewServer(LongHistoryFitsExistingCloudReadDeadline|RichHistory)$'
```

Compare the `Cloud Bridge request` elapsed lines per mode. For the store
fixture used to locate the bottleneck, the unchanged timing test is:

```sh
go test -run '^TestReadViewLongHistoryWithinSourceDeadline$' -count=15 -v ./internal/adapters/projectassistantstore
```

#### Chrome click to visible message

The same two source variants served a 9,001-message synthetic Bot through the
normal Cloud request decoder, authenticated HTTP gate, Host and SQLite. The
browser used the same production-mode Bots shell, list and Chat components
for both variants, with a loopback adapter in place of the encrypted relay.
The fixture sent the normal automatic read acknowledgement and checked that
each recorded click issued exactly one successful snapshot. Neither source
started a provider.

Chrome 157.0.8103.80 ran on the same M4 Pro. Each viewport used a fresh tab
per trial, one unrecorded warm-up per variant, and 12 pairs with alternating
order. Actual mouse or touch input opened Bots and selected the roster row.
Timing starts on the trusted roster click and ends on the frame after the
latest message first intersects the chat viewport. Reported p95 is the
maximum of 12 samples.

| Chrome viewport | Before median / p95 (ms) | After median / p95 (ms) | Faster pairs |
| --- | ---: | ---: | ---: |
| Desktop, 1280 × 800 | 887 / 2168 | 803 / 1558 | 8 of 12 |
| Mobile emulation, 390 × 844 | 907 / 1977 | 671 / 2330 | 11 of 12 |

The mobile outlier was already present in the snapshot request (2281 ms);
the next-frame browser observation was 2330 ms. This run establishes a
lower median on both tested viewports, with stronger paired evidence at
the mobile size. It does not establish a mobile tail improvement or
production network latency.

The local browser harness combines the server fixture from
`project_assistants_view_server_test.go`, the synthetic relay used by
`project_assistants_roster_entry_browser_test.go`, and the production host
fixture `web/console/tools/fixtures/project-assistants-message-menu-host.tsx`.
The task's retained `browser-source-{before,after}.json` Go overlays,
`browser-ui.mjs`, `browser-runner.mjs`, and JSON samples reproduce the run:

```sh
go test -overlay="$EVIDENCE/browser-source-$variant.json" -c \
  -o "$EVIDENCE/browser-source-$variant.test" ./internal/transport/http
# From internal/transport/http, start one fixture per variant.
CHAT_READ_SOURCE_READY="$EVIDENCE/browser-$variant.origin" \
  "$EVIDENCE/browser-source-$variant.test" -test.run '^TestTaskBrowserChatReadSource$' -test.v
# From the checkout root, after both .origin files have been written.
node "$EVIDENCE/browser-ui.mjs" "$PWD" "$EVIDENCE"
node "$EVIDENCE/browser-runner.mjs" "$EVIDENCE" "$CHROME"
```

The Go overlay keys are absolute paths in the checkout being tested.
Write the matching `.origin.done` files to finish both source fixtures.

### Reads check the rows they return (2026-11-27)

On the owner's machine, daemon `bot_view` traces from 24–26 November (344
Cloud reads) showed `sqlite_read` taking nearly all Bridge time, a median
of 208–454 ms per day and up to 8.1 s, with every statement slowing at once
under load. Local snapshot reads scaled with the Bot's whole stored history,
not with the page: 0.55 s for a 4.7 MB aggregate (1,138 messages), 0.33–0.44 s
for 2.9 MB, about 10 ms for 40 KB. Five browser traces matched to daemon
traces spent 0.44–2.34 s in the daemon and a further 0.75–1.3 s moving
156–172 KB of ciphertext through the relay.

Every persisted write already passes `validateAggregate`: message positions,
IDs, kinds and fences, turn, permission, work link and binding relations, and
the conversation's last sequence. `persist` is the only writer of the Bot
row. Snapshot and history no longer repeat that walk over the whole history;
they still check the shape and fence of each row they return, and the head's
metadata. A row that no writer of this store can produce, outside the page,
no longer stops the newest page; `TestReadViewChecksReturnedRowsOnly` keeps
that boundary and `TestRestoreRefusesBrokenFactRelations` keeps the write-side
refusals. On the 9,001-message store fixture
(`TestReadViewLongHistoryWithinSourceDeadline`, three runs each on an Apple
M4 Pro under shared load) snapshot fell from 680–685 ms to 396–430 ms and
history from 562–649 ms to 379–424 ms; the empty event resume, which never
ran the walk, stayed at 185–205 ms.

API 125 adds `exchange_excerpts` to snapshot and history. Exchange notices
then carry at most `MaxExchangeExcerptRunes` characters of text, request and
reply. The chat shows a 160-character summary and sends the flag; the
exchange panel reads without it and gets the full text. In the owner's
largest pages, exchange text was 37–67% of the JSON for exchange-heavy Bots.
The chat's write preflight reads only the head and fence, so it asks for one
row instead of a hundred.

### Member rows (schema 17, 2026-11-27)

Schema 17 stores each Bot's messages, turns, bindings, permissions and work
links one member per row (`project_assistant_bot_facts`, see
`docs/project-assistants-store.md`). The read statements define `fact_messages`
and its siblings with json_each's key, value and type columns, so their
predicates are unchanged. The timeline first finds its floor: the position of
the page-size-plus-one-th own-chat message below the cursor. Those messages
are candidates, so every row of the page is at or above the floor and no older
message is read. The chat head takes the newest own-chat row from the top of
the index, and pending and last-turn lookups join messages by ID.

On the 9,001-message store fixture snapshot fell from 396–430 ms (commit
`fc1d01a3f`) to 7–8 ms, history to 7 ms and an empty event resume from about
200 ms to 2–4 ms. On a copy of the owner's database (254 Bots, the chat's
20-row opening read, five runs per Bot, medians), against `b057cb528`:

| Bot (stored history) | Before (ms) | After (ms) |
| --- | ---: | ---: |
| 4.7 MB, 1,138 messages | 563 | 36 |
| 2.9 MB, 793 messages | 362 | 21 |
| 1.7 MB, 451 messages | 129 | 16 |
| 0.67 MB, 196 messages | 76 | 10 |
| 40 KB, 17 messages | 7.4 | 3.9 |

The Bots list (`ReadRoster`, 17 Bots) fell from 206–215 ms to 62–63 ms; its
unread count now reads only messages after the reader's position. A read
cursor write on the largest Bot fell from 65 to 40 ms median. The migration
took 2.1–5.1 s once on that copy. Exports of all 254 Bots and the roster frame
were byte-identical before and after it, apart from the roster's random epoch
and cursor signature. These are source timings on an Apple M4 Pro under shared
load; they exclude the relay and the browser.
