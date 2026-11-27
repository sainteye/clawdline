# Persistent Bot storage

`internal/adapters/projectassistantstore` implements the frozen Repository,
Memory, EventFeed and SchemaInstaller ports. It is an additive module: importing
it does not open a database, install tables, start a model, register a route or
enable the feature. The host remains responsible for opt-in, authenticated
principals, authorization and fresh Project/source resolution.

## Construction and ownership

```go
err := (projectassistantstore.Installer{}).Install(ctx, db)
if err != nil { return err }
featureStore := projectassistantstore.New(db)
// Pass this SAME instance as Repository, Memory and EventFeed.
```

`Installer{}.Version()` returns the feature schema version. Installation uses a
transaction and creates only `project_assistant_*` tables/indexes. It does not
touch the host's `PRAGMA user_version`, configure its connections, open its live
store or delete existing data. Repeated installation preserves the feature's
facts; unknown schema versions are rejected. The caller owns and closes `db`.
The SQLite driver is supplied by the host, rather than registered by this module.

The feature schema is now **17**. Install accepts any installed version from 1
to 16 and migrates it in the same transaction; version-specific steps, such as
the version 8 login-kind backfill and the version 17 split of Bot members into
rows, run only for older markers. The rest of this paragraph records the 1 to 2
upgrade. The HTTP contract remains
`project-assistants.v1`, with the existing integer `schema_version` field. An
installed schema 1 upgrades to 2 in the same transaction as additive feature
DDL and roster backfill. The migration changes the feature version marker;
it does not decode or rewrite aggregate JSON, messages, events, receipts,
memory/tombstones, permissions, work links, read cursors or routine records.
Existing host tables and `PRAGMA user_version` remain unchanged. Version 0 and
unknown newer versions are refused before DDL or backfill. Failure to update or
verify the version marker rolls back the entire transaction, including DDL.

Host construction migrates an already installed source on its primary database
before exposing the read service, even when the source is disabled. It does not
construct a provider, start a runtime, resume a turn or enable routines. A fresh
default-off source creates no feature tables. After construction, status and
authorized reads still require the current schema; legacy or future metadata
cannot silently bypass that check.

`New(db *sql.DB) *Store` performs no implicit migrations. `WithReader(reader)`
borrows a separate reader pool for independent projections and must be set
before the Store is shared. The exported `CompletionIntent` and `DecisionIntent`
hooks add database writes only to the same Bot transaction. Share one Store
per database. Its Bot mutex serializes Commit, Save, Forget, MarkRead and Restore
against `WithCurrent`. Separate Store instances or processes do not share this
mutex. SQLite still provides transaction/CAS integrity, but the caller must not
use separate writers while a `WithCurrent` callback is sending context.

## Atomic facts and durable receipts

The Bot table stores one explicit aggregate: configuration, conversation, memory
entries/revision and reader cursors. Since schema 17 its turns, bindings,
messages, permission offers and work links are stored one member per row in
`project_assistant_bot_facts`, keyed by the member's identity (a message by its
ID, with its position in `ord`). Events and command receipts have separate
durable tables. A Commit validates the resulting aggregate, allocates source-local durable event
sequences and commits all facts/projections/events/receipts together. The SQLite
CAS also checks the stored version. A failed constraint or final receipt write
rolls everything back, including event allocation.

The module validates machine, canonical Project, Bot, conversation, binding,
generation, turn/message relations and nested event payloads. It does not turn
client-provided Project scopes into authorization. Host Project memory remains
with the existing service; the store never reads or edits those external files.
Memory source records preserve the host-supplied provenance and scope; the host
must authenticate their origin before calling Save.

Commands use the frozen command identity/hash rule. The host supplies a SHA-256
hash of its canonical validated command. The store validates the hash format and
compares it with the immutable stored receipt; it cannot reconstruct a transport
command from a generic Mutation. Same identity/hash returns the exact previous
receipt before CAS. A different hash conflicts. Accepted, executing, unknown,
delivered, observed and read are distinct. Receipts and events are not evicted
while their Bot exists; `DeleteBot` removes the Bot's member rows, events,
receipts, handoff cards and receipts, reactions and routine records in one
transaction and keeps a minimal roster tombstone. Unknown effects never become
permission to replay. Observe records a separate
observation receipt; it refuses unknown/executing targets and does not rewrite
the original receipt. Resolving unknown effects requires a future explicit
reconciliation operation, not retrying the original command.

A WorkLink is a projection, separate from that immutable command receipt.
The trusted broker bridge may commit later verified landing facts or a Board
binding with a new projection command, including after WorkUnknown. Existing
Bot/Project/conversation/fence/turn/task ownership and aggregate CAS still apply.
The store does not authenticate a broker record itself; that is the bridge's
responsibility before Commit. Repeating the original dispatch still returns its
unchanged unknown receipt, and repeating a projection creates no duplicate event.
The SQLite behavior tests cover these boundaries without claiming that the
complete broker-to-store host has already been installed or exercised.

An existing Bot's Project and conversation cannot change. An Agent change is
accepted only with a newer instruction revision, and it marks the old Agent's
primary bindings as superseded. Instruction revisions never move backwards. Queued turns can have zero
instruction/memory snapshots. Exactly at queued-to-running they capture the
latest Bot instructions and memory revision and may bind the current generation.
Their admission user message retains its original fence. Other messages,
permissions and work links require the turn's complete fence. Once running, turn
snapshots are immutable. A changed binding ID requires a newer generation.

The aggregate is deliberately direct, with no automatic pruning or separate
projection cache. Mutations still decode the whole aggregate and validate it;
`persist` then writes the Bot row and only the member rows whose bytes changed.
Schema 17 made that change after real use justified it: on 26 November one
owner's largest Bot held 4.7 MB of history in its row, and opening its chat
took 0.36–0.56 s in SQLite alone. The migration moves each member unchanged and
removes the five collections from the row; a missing or repeated member identity
fails the whole migration and leaves the database as it was. A row that still
holds a collection inline is refused by every reader rather than read as empty.
`docs/project-assistants-read-store.md` has the measurements.

## Lists, reads and replay

Lists use stable `(machine, bot)` ordering, case-insensitive name search and an
opaque query-bound keyset cursor. Histories return newest-first message pages.
Event pages return oldest-first durable events after a cursor. Page limits are
the existing domain limits. Process traces are the one retention rule (below).
An invalid/future event cursor and malformed, mismatched or missing list anchor
return `pa_cursor_expired` so the caller can refresh a snapshot.

`ListForReader(ctx, query, readerID)` is an additional public method. `readerID`
comes from the authenticated host principal, never the client. A principal's
cursor is monotonic and shared across that principal's devices. Distinct
principals never consult each other's cursors. The frozen `List` method has no
reader argument and returns a reader-neutral projection: an empty ReaderID and
zero unread count mean no unread projection was requested, not that all messages
were read. The host must use ListForReader for authenticated unread counts and
populate source status/freshness from its own authority. Trace, permission and
work events do not increment visible message sequences or unread counts.

MarkRead stores its event and receipt atomically. Replaying it returns the
original resulting cursor even if later commands advanced the principal further.
Unknown Bots/conversations/turns/bindings/offers return typed `pa_not_found`.
SQL/infrastructure errors propagate and are never reported as success.

## Process traces

A turn's process — streamed reply and thought chunks, tool calls and tool
activity, plans — is shown while the turn runs. It is not kept after: the
commit that stops a turn being its Bot's active turn (succeeded, failed,
cancelled, unknown, or replaced) deletes that turn's `trace_updated` events
and the `send` receipts of the events it published (`runtime-event-<turn>-…`).
Two categories stay, because a reader needs them later:
`bot_request` (a delegated request's reply) and `no_visible_reply`
(`pa.RetainedTraceCategories`). The machine's newest event is never deleted,
so no reader's event frontier moves backwards. Messages, turns, permissions,
work links, memory events and the person's own send receipts are unaffected.

Nothing can use what is deleted. An incoming event for a turn that is not
active is refused before its receipt is consulted, so a runtime receipt can
only deduplicate a retry while its turn runs. The chat shows a past turn's
messages; its process disclosure says the steps are no longer stored.

The roster journal (`project_assistant_roster_changes`) keeps one row per Bot.
Each append first deletes the Bot's earlier rows. Every reader takes either
the newest row per Bot or the rows after a frontier, and reads the Bot's
current summary for each, so a frontier from before the deleted rows still
sees the Bot through its newest row.

Before this rule each streamed chunk wrote an event, a receipt and a roster
row that were never deleted. On 2026-11-26 one machine's `clawdline.sqlite3`
passed its 1 GiB limit (`store.db`, `capacity_exhausted` on `/v1/health`)
with 271,244 trace events, 277,723 `send` receipts and 200,118 roster rows
holding 47 MB of text. `store.Open` clears what such stores left behind with
the same rules, `store.bot_process_prune_batch` rows per statement, for at most
`store.bot_process_open_seconds` so a restart stays inside the updater's
health wait; the maintenance clock (`MaintainEvery`, which also runs at start)
deletes the rest in the background. An Open that finds nothing left gives the
space back with one VACUUM when at least `store.bot_process_vacuum_free_bytes`
are free (`internal/adapters/store/bot_process_retention.go`).

## Memory and forgetting

Creation with only Bot and Conversation initializes memory at revision 1, as
required by the domain's CheckContext/Forget rules. Mutation.Memory can only
assert the current revision; changing memory must use Save/Forget so provenance
and tombstones cannot be bypassed. Save performs memory CAS and advances both
memory and Bot versions. An entry's incoming revision is the expected current
revision; its stored revision becomes the new revision. Summaries and handoff
notes are ordinary provenance-bearing entries rather than another authority.

Forget monotonically records source IDs and the forgotten-through revision,
marks every dependent entry forgotten and emits its receipt/event in the same
transaction. A forgotten entry/source cannot be restored by a later Save. The
context is rebuilt in stable entry/source order from only active, valid entries.
Readable transcripts and historical events remain available to people; Snapshot
does not replay them into model input. Old opaque native bindings are rejected by
the existing domain resume rule after forgetting.

`WithCurrent(ctx, key, revision, callback)` checks the current revision and complete
provenance while holding the Bot mutex, then invokes the callback at most once.
It holds no SQL transaction or connection during the callback. The callback may
perform the one bounded stdin write specified by the port, not a model turn or a
reentrant call to this Store's mutation methods. A callback error is propagated
without retry because the external effect may be unknown. Forget/Save wait until
that call returns; a stale revision never invokes the callback.

This does not prove native-provider forgetting, native memory isolation or
read-only tool enforcement. The module never edits native configuration, Project
memory or third-party copies. Runtime must use the filtered snapshot and an
appropriate new binding when opaque resume cannot exclude forgotten sources.

## Export, integrity and restore

The Repository Export includes versions, Bot, conversation, messages, turns,
bindings, offers, work links, memory entries/tombstones, filtered context, events,
all receipts including unknown effects, and all principal cursors. Export reads
a consistent transaction snapshot. No Markdown file becomes writable authority.

Additional public APIs are:

```go
store.Restore(ctx, data)                 // data is projectassistant.Export
store.ExportArchive(ctx, key)           // (Archive, error)
store.RestoreArchive(ctx, archive)      // error
```

`Archive{Data, Manifest}` includes a SHA-256 of the canonical JSON-encoded Export
and the contract/schema versions. It detects transfer corruption; it is not a
signature and grants no authorization. Use Archive for persisted transfers.
Bare Restore is available for the frozen typed port and validates scope,
relations, duplicates, receipts, event order and the reconstructed context.
Both paths reject unknown newer versions and insert atomically without replacing
an existing Bot. Sequence collisions reject the entire restore. The supported
recovery target is an empty, compatible feature database; this is not a merge or
conflict-resolution tool for independently edited databases.

Empty optional fallback configurations are omitted from aggregate JSON,
including nested event Bot snapshots. A fixture produced by the actual v1
types, installer and exporter at `7f4f1db83` verifies the original canonical
archive hash; recreating a v1 fixture with current types would miss this
compatibility boundary. The fixture contains synthetic facts only.

New exports and integrity manifests declare the current schema
(`pa.SchemaVersion`). Restore accepts every schema from 1 to the current one
through the same relationship, complete-context, command-hash and receipt
validations. An export older than schema 12 is refused if any memory entry
carries topic-note fields, and an export carrying group membership must declare
the current `GroupSchemaVersion`. An archive's manifest and data schema versions must agree;
its contract and integrity hash must also pass. Unknown versions, damaged
hashes, overwritten Bots and sequence collisions refuse the entire restore.
Restoring a valid v1 export preserves fact and receipt identities, including
unknown effects and principal read state; the next export declares the
current schema. It does
not infer command identities from old plain drafts or grant replay permission.

Schema 2 is a forward-data boundary for additive Bot fallback configuration,
attempt ledgers and bindings for different Agents. Schema-1 daemons use equality
checks and intentionally refuse schema-2 feature tables and archives rather
than misread those facts. Never change the marker back to 1 or clear attempts
to make an old binary accept them. Recovery uses a compatible version with a
preserved archive or installer snapshot. Returning to a pre-upgrade snapshot
must also account for any later facts; this migration provides no automatic
format downgrade. The historical lifecycle rehearsal below describes its named
v1 revision and does not establish v2 downgrade compatibility.

This version/migration design belongs to Clawdline. It is not a claim that
Codync uses the same SQLite schema or migration mechanism.

## Upstream derivation and deliberate differences

The reference revision is Codync
`234219cef2d3f991f0f7225ba89e3c99cdd34177`:

| Source | Adopted behavior | Go module difference |
| --- | --- | --- |
| `host/src/store/model.rs` | Stable Bot identity/settings and roster representation; distinct transcript entry kinds | Frozen Go Bot/Turn/Binding types and source-machine/Project authority; no upstream accounts or relay |
| `host/crates/memory/src/lib.rs` | Caller-owned SQLite connection and atomic memory changes; no processes/models in memory module | Caller-owned Go `*sql.DB`; no Markdown authority or background workers |
| `docs/features/context-and-memory.md` | Provenance, instruction capture boundaries, revision CAS, durable forgetting and separation of transcript from context | Strict per-Bot/Project scope, monotonic tombstones without automatic expiry, filtered context; no dreaming/shared layer/native configuration edits |

This is a behavior/architecture port, not a literal Rust copy. Existing
`third_party/codync` Apache-2.0/NOTICE/derivation records remain intact. The store
does not independently implement provider context compaction or claim the
upstream's native memory switches work in Clawdline.

## Verification boundaries

The package tests exercise empty-DB/repeated installation, host table operations
and user_version preservation, real database reopen, CAS races, late SQL failure
rollback, scope/relationship injection, queued snapshot capture, principal read
isolation, pagination, observation receipts, forget/callback serialization,
immutable callback effects, source resurrection denial, full Archive roundtrip,
future versions, integrity damage and late restore collisions. Run:

```sh
go test -race -count=1 ./internal/adapters/projectassistantstore
tools/check.sh
```

These are module tests. A specified older executable doing original Session,
Board and schedule work on an isolated copy, actual feature source removal and
rebuild, UI/Cloud acceptance and native provider behavior remain integration
responsibilities. Simulated host-table operations do not establish those claims.

The schema-2 compatibility tests additionally compare actual SQLite values,
opaque aggregate bytes, schema objects and sequence allocation across v1
migration and database close/reopen. Metadata `ABORT` and `IGNORE` trigger
injections verify that failed upgrades leave v1 facts and missing indexes
unchanged. Legacy archive tests retain unknown receipts, forgetting and read
state while emitting v2, and check matching versions, corruption and existing
relationship/collision refusals for both accepted versions. Disabled Host tests
verify readable history with no provider construction and strict later status
checks. These do not qualify the fallback runtime or native continuation.

## Local rollback, removal and reinstall rehearsal

On 2026-11-13 the integrating root completed the five separate lifecycle
checks using synthetic data and real local executable artifacts. The tested
feature commit was `414eff0410557e351126bc02047445afa9b8e7ed`, tree
`8dd21619cedc47ce5c1bff3ecbc47fb303ec159a`. The specified pre-feature executable
was built from `96af39ebce0e5bb55942cfe9025384cc863a7c8e`.

The removal checkout physically deleted 145 feature files and restored 59
individually reviewed wiring files. It retained the unrelated atomic-argument
fixture fix. The resulting tree was
`f73ed5b8fd14117708b546a4baf10914f5a63ba1`; its executable was built and run.
The [handoff removal inventory](project-assistants-handoff.json) partitions the
new files and lists the wiring. That inventory is revision-specific: do not
blanket restore a baseline over later or foreign work.

The same lifecycle sequence established:

1. The specified old executable could read/forget synthetic archived Session
   metadata, edit/read back a Board item, and save/read back a disabled schedule
   on a data copy. The schedule did not execute. Upgrading the same copy to the
   feature executable preserved the complete Bot export.
2. The source-removed executable could perform those original operations.
   Its Bot status API returned 501; the console document returned 200.
   Restoring the compatible feature executable over that data made the stored
   Bot readable again.
3. Disabled feature sources remained `history_only`; a real HTTP send request
   was rejected with `pa_disabled`. The full HTTP export remained available.
4. ExportArchive/RestoreArchive restored all facts into an empty compatible
   database. A newly started feature executable read the restored data over
   HTTP. Explicit enable switched the source to `interactive`; disable restored
   `history_only` without submitting a model turn.
5. Original Project memory and the synthetic Project instruction file had
   unchanged file hashes. All five owned daemon PIDs and listening ports were
   independently checked absent after the exercise.

Full archive data compared equal after rollback, upgrade, source removal,
reinstallation and empty-database restore. The canonical export hash was
`2592d72864d2bb38513f9b307bd60749e15eb6557f277aeabed94267248b80bd`:
120 messages, two turns, two bindings, three permission offers, one unknown
work link, two memory entries, 609 events, five receipts and one read cursor.
The forgotten-source tombstone, unknown effects and cursor at message 120
survived. Three real HTTP exports also compared equal. Checking table existence
alone was not used as preservation evidence.

The OS sandbox denied personal-home reads/writes, external network access and
other executables. The old executable's mandatory legacy icon path stayed
unreadable but returned ENOENT to select its default icon. The existing
`CLAWDLINE_NEXT_PLACES_FIXTURE` selected only the temporary synthetic Project;
this bypassed no data, authority or feature-capability gate. No native CLI/model
Session, production Cloud flow or physical phone was tested in this rehearsal.
Native provider/tool containment remains a separate acceptance obligation.
Private raw logs, complete exports and executable hashes stay with the owning
Board evidence; they are not copied into this public repository.

## Fresh-session handoff data

The runtime reconstructs a `SessionHandoff` from the existing SQLite Bot,
conversation, turn, work and memory facts. There is no second handoff database,
Markdown authority, summary cache or automatic summary generation. The opening
carries the exact execution identity and immutable instruction/role snapshot,
current provenance-complete memory text, unfinished turn/work references and a
message-ID/sequence bookmark. A reopen reconstructs those fields from the same
facts rather than treating an old serialized opening as current authority.

Only `Memory.Snapshot`/`WithCurrent` supplies summary text. Remembered summaries
and handoff notes remain normal source-bearing memory entries, so Forget removes
them from the next opening. Neither readable transcript text nor an opaque native
summary repairs missing provenance. The bookmark and unfinished IDs preserve
navigation and state, not requests or authority to replay unknown work.

This follows the pinned Codync `host/src/chat/handoff.rs` division between a
handoff's summary, open work and history bookmark. The first-version difference
is deliberate: no automatic rollover schedule, last-four-message transcript copy,
background compaction or model-generated draft is added. Clawdline's SQLite
memory/provenance rules remain authoritative. Native provider acceptance and
production Cloud behavior are separate from this data path.
