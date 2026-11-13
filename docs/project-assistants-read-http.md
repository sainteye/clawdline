# Bounded Bot reads through the source

The additive operation is `GET /v1/project-assistants/{bot}/view` at API59.
Existing API58 metadata routes and the previous16 service methods remain.
This page describes the source implementation and local integration evidence;
production Cloud encryption/relay, native providers, phone acceptance and feature
removal remain separate acceptance work.

## Interfaces and ownership

`ServiceOptions.ReadViews` is the optional application port. Trusted composition
also supplies `ReadViewByteLimit` from `cloudops.ReadPlaintextBytesLimit`; neither
this budget nor the cursor budget/authorization binding is a request field.
A missing port/budget returns typed unsupported for View alone.

`Service.View(context.Context, pa.ReadViewRequest) (ReadViewResult, error)` returns
`ReadViewResult{View ReadView, Source pa.SourceStatus}` with a private captured
authority. `ReadViewResult.Revalidate(context.Context) error` must succeed after
complete serialization and immediately before publishing. Its zero value refuses.
The binding hashes current trusted principal, reader, authorization revision and
source identity/read/write/fresh/access ceiling, excluding descriptive timestamps.
Each query first checks fresh readable authority, then checks the same authority
after SQL returns. Publication repeats that check after packing. Revocation,
downgrade, account/reader change, expiry and Host shutdown discard all content.

`Store.ProjectAssistantReader() *sql.DB` lends the existing query-only reader
pool separately from the single writer. Host owns its lifetime and existing pool
bounds; the feature neither opens nor closes a database or changes pragmas/DSN.
`Host.Options.Reader` and the optional controlled `ReadViews` composition preserve
old callers. Fresh default-off composition installs no feature schema/provider.
Installed history can be read while disabled without creating a Runtime/provider.

## One strict HTTP and Cloud request

The existing authenticated handler accepts exactly one value for each query key:
`machine_id`, `conversation_id`, `mode`, `source_epoch`, `cursor`, `after_seq`,
`page_size`. GET bodies, unknown/duplicate/missing keys, fractional/negative numeric
fields and mode-incompatible fields are refused. Bot IDs retain the existing
single-segment rule; display names are not reserved. Current machine/principal
identity comes from the existing gate witness revalidation, not the request.

`project-assistants.view` is the17th Bot Cloud word and a read operation. The
single vocabulary generates closed required decoding and captured carry metadata.
The nested key supplies the escaped Bot path and machine query; all empty epoch/
cursor values and zero after_seq survive. A read does not require content-write
permission or the remote command switch. Source mode/page validation remains
authoritative. No second schema, plugin or Export fallback is introduced.

The six modes and their source epoch/cursor semantics are defined in
[the read contract](project-assistants-read-views.md). Events request up to256 rows,
other pages up to100, detail exactly1. Only snapshot includes mandatory snapshot
state and replaces the initial event cursor with the machine event frontier.
Events advance to the last included event, retaining the request position on an
empty page; neither history heads nor other Bots' frontiers skip later events.

## Complete-object packing

The adapter finishes its short SQL transaction before transport conversion.
Transport encodes each complete generated item once, then measures complete
generated envelopes for candidate prefixes. HTTP includes its newline; Cloud uses
the actual payload serializer with `read:` plus128 '<' characters, the worst legal
existing correlation name. Both complete bodies must fit the existing8MiB limit.

Mandatory head/source/snapshot are never sliced. Each item has exactly one payload
matching kind; collections are non-null arrays. Internal AfterCursor, CursorFailure
and Failure never appear on the wire. A valid preceding prefix may carry an honest
continuation before an oversized next object, without skipping it. An oversized
first item, mandatory object or detail returns typed capacity_full with no partial
successful object. An intermediate oversized cursor does not prevent a later
fitting complete prefix. Once cumulative encoded rows alone exceed8MiB, later
prefixes cannot fit and packing stops. The final same-authority check occurs after
these full-byte measurements, on the sole successful View publication branch.

## Verification boundary

Direct Service tests change principal, reader, grant revision, source identity,
readability, write ceiling, feature state and freshness before/during reads and
before publication. HTTP tests exercise its actual successful publication branch,
strict query/body parsing, whole-row/error prefix selection, cursor rescue,
snapshot/detail refusal, event positions and the exact8MiB one-byte boundary.
The borrowed-pool test verifies SQL write refusal, a stable read snapshot during a
concurrent committed writer, and visibility after that transaction ends.

Actual local Server/gate/Service/SQL and Cloud Bridge/Router/Server/SQL tests also
verify snapshot frontier602 after600 source events, a different Bot owning that
frontier, message sequence650, older history, old epoch/ahead refusal, disabled
history and zero provider calls. Current-generation2 pages retain generation1
work and an exact old unknown-turn barrier; a late old-generation work event
still resumes after the original frontier. A stored indivisible WorkLink above
8MiB yields a valid newest-row prefix and typed capacity refusal on continuation.
Eight post-pack authority/lifecycle barriers discard encoded content and assert
that the SQL reader is no longer held at publication.

The9001-message regression first failed on Store candidate f81c9907d: an existing
60-second request budget ended in typed outcome_unknown after60.004s. The prior
235.31s whole-test duration is not a request latency measurement, and shrinking
the separate sequence-disambiguation fixture to650 rows was not its fix.
After committed SQL correction45d4921, the permanent real Bridge/Router/Server/
Store regression passed with separate unchanged60-second budgets: snapshot
748.234ms, history704.958ms, events182.643ms in the normal local run. Fixture
build11.141ms/write62.343ms were recorded separately. The race run also passed.
These establish this synthetic deadline case, not production Cloud/network
latency or a general O(page) SQLite CPU/memory guarantee.

The original16 methods have an explicitly named API58 Legacy16 captured-request
compatibility test, preserving every unique method and complete field mapping.
New View Go vocabulary/Bridge/real Server checks remain required at API59.
Actual17th TS SourceClient.view and strict readView helper joint acceptance is
separately pending under the integrating root after both modules commit; this
module does not claim that joint check or substitute generic echo for View.

The recorded decoder baseline contains220164 inputs across246 generated words.
Regeneration contains222491 inputs across247 words:1614 probes for View and713
additional existing-word probes. Of those,692 contain the new mode/source_epoch
top-level keys; the precise origin of the remaining21 probes is not yet attributed.
Typed comparisons retaining exact JSON number lexemes found zero missing old
inputs and zero changed old outputs. The complete253-word carried catalog includes
the new View word. These are local decoder/serialization facts, not evidence of a
live encrypted production Cloud request or native execution.


## Captured console request integration

The backend vocabulary and its captured console parser are delivered together.
The parser carries all seven view query fields, including empty strings and
zeroes, and rejects bodies, duplicate or extra fields, unknown modes and unsafe
numeric encodings before transport. Six-mode relay tests retain the complete
request envelope. The source remains authoritative for cursor and mode-state
validation. The original API58 Legacy16 capture remains unchanged; the full
vocabulary test continues to exercise every carried operation, including View.
The later UI module adds SourceClient.view and strict response validation; this
transport test does not establish joint Go-to-TypeScript or production Cloud
acceptance.
