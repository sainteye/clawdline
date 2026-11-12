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

`New(db *sql.DB) *Store` has no options or implicit migrations. Share one Store
per database. Its Bot mutex serializes Commit, Save, Forget, MarkRead and Restore
against `WithCurrent`. Separate Store instances or processes do not share this
mutex. SQLite still provides transaction/CAS integrity, but the caller must not
use separate writers while a `WithCurrent` callback is sending context.

## Atomic facts and durable receipts

The Bot table stores one explicit aggregate: configuration, conversation, turns,
bindings, messages, permission offers, work links, memory entries/revision and
reader cursors. Events and command receipts have separate durable tables. A
Commit validates the resulting aggregate, allocates source-local durable event
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
delivered, observed and read are distinct. There is no receipt/event eviction;
unknown effects never become permission to replay. Observe records a separate
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

Existing Bot source, Project, conversation, Agent and permission policy cannot
change. Instruction revisions never move backwards. Queued turns can have zero
instruction/memory snapshots. Exactly at queued-to-running they capture the
latest Bot instructions and memory revision and may bind the current generation.
Their admission user message retains its original fence. Other messages,
permissions and work links require the turn's complete fence. Once running, turn
snapshots are immutable. A changed binding ID requires a newer generation.

The aggregate is deliberately direct, with no automatic pruning or separate
projection cache. Mutations decode and rewrite the Bot's aggregate, so cost grows
with retained history. No latency/capacity benefit or large-history performance
claim has been measured. A normalized representation can replace it under a
future schema migration if real usage justifies that work.

## Lists, reads and replay

Lists use stable `(machine, bot)` ordering, case-insensitive name search and an
opaque query-bound keyset cursor. Histories return newest-first message pages.
Event pages return oldest-first durable events after a cursor. Page limits are
the existing domain limits; this module adds no retention or summary bounds.
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
