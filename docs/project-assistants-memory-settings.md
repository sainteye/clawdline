# Bot memory layers and native runtime settings

Construction starts from `2282c91d0a88b66e25434abcefff2b75f122cb6c`.
This document separates source construction, scripted local checks and the
explicitly labelled installed-native qualifications below. None establishes
production Cloud, phone or public Release acceptance.

## Data ownership and reads

`GET /v1/project-assistants/{bot}/memory-view` is an authenticated API 79
read. All query fields are explicit: `machine_id`, `conversation_id`,
`source_epoch`, `layer`, `entry_id`, `revision`. A listing has empty values
for the final four fields. A detail names `bot` or `project`, the source's
entry ID, its layer revision and the listing's source epoch. There is no raw
path input. Unknown fields, duplicate fields and request bodies are refused.

`Service.ReadMemory` resolves the Bot's full canonical Project on the source.
The source epoch binds account/principal, reader, authorization revision,
machine, access ceiling, canonical Project and conversation. It is an opaque
correlation fence, not a credential. Authority, Project and both memory
revisions are checked again after serialization and before publishing bytes.
An epoch from another Bot or grant cannot open a detail. A concurrent edit
returns `pa_context_stale`; a forgotten detail returns `pa_not_found`.

The Bot layer remains the feature-owned SQLite memory authority, including
its provenance, CAS writes, transitive sources and tombstones. `MemoryBot`
extracts only the Bot header. `ReadMemory` projects just memory through SQLite
JSON queries and returns at most the existing 256-entry Bot cap, with a
128-character preview. Historical tombstones are represented by their real
count, not copied into the view. It does not export or decode chat, work, receipts or
historical bindings into Go. Full Bot details retain the existing 32 KiB
memory-entry bound. New saves record source-owned time; old entries say that
time was not recorded. The source kind is the recorded author attribution;
this format does not identify a human principal by name.

The Project layer reads `memory.Store`, the same service used by Project
Sessions and `clawdline memory`. It is never a second Bot-shaped store.
`Store.Browse` uses that service's write lock and computes a content revision,
so a same-length edit changes the revision. Its details retain the existing
64 KiB body bound. The view reports entry counts against the actual Project
and Bot entry caps, not invented token usage. Project modification time is
filesystem metadata; its format has no author, so the UI says author not
recorded. Missing storage is empty; an unreadable store fails the read and
never reports a successful empty layer. A missing integration is explicitly
unavailable. The Project view lists up to 1,152 active and pending entries. Archived Project
originals remain reachable through `clawdline memory dream` and `memory show`.
Out-of-band data beyond the live caps is marked truncated rather than presented as complete.

The console uses production `ProjectAssistantSourceClient`, not export, for
these reads. It refreshes an open panel when the current Bot memory revision
changes and offers an explicit refresh for Project service changes, which
have no Bot event stream. Detail responses are discarded after a layer switch,
unmount or source/grant change. Read-only sources can read details; only the
existing authorized Remember/Forget paths write Bot memory. Those paths keep
their exact uncertain command, CAS revision and explicit replay semantics.

Project shared memory is read-only in this view and is **not** added to Bot
model context. The existing Bot memory send/forget barrier therefore remains
the only assembled-memory authority. No shared entry is copied into SQLite,
and no stale shared summary can restore a forgotten Bot entry. Existing
external-copy limitations still apply. Idle dreaming remains separately
scheduled work; this feature creates no background model job, Dreams result
or global native-memory reader. Native automatic memory stays disabled under
the existing admission policy.

## Native settings and execution

`GET /v1/project-assistants/{bot}/configuration` takes explicit
`machine_id`, `conversation_id`, `agent`, `model` fields. The source resolves
the Project and rechecks the same authority at publication. A disabled feature
retains passive memory/history reads but refuses native configuration probes. An installed
provider creates a disposable ACP discovery session, with no prompt, owned
tools, native resume or ownership record. The existing installed integration
owns executable resolution, credentials and memory-isolation evidence. The
request cannot select a command, executable, environment variable or shell.
One Host owns at most one discovery process; concurrent requests are refused
with `pa_capacity_full` instead of building a waiting queue. Cleanup errors
remain errors.

The catalog comes from actual `session/new` results: ACP `configOptions`, or
legacy `models.availableModels`. The model is selected first through
`session/set_config_option` (legacy `session/set_model` only when that is the
advertised protocol). The returned model-specific options are then read.
Supported reasoning selectors and advertised `fast`/`fast-mode` controls are
shown with their native choices. Boolean fast mode is sent as a boolean;
select controls retain their exact native strings. Permission/mode options
cannot be smuggled through the model-options dictionary. No model names or
native flags are guessed. A backend without a selector reports that fact.

Human settings edits add optional `agent`, `runtime_settings` and `notify` to
both ordinary and team-settings paths. Omission preserves saved values for
old clients. An explicit empty model/options object restores Agent defaults.
Changing Agent clears its model options, mode and alternate-Agent chain
before applying any explicit replacement. Project identity remains fixed.
Model changes clear the form's model-specific options. Saved selections are
validated against a fresh native catalog and checked again before a real
prompt, because persistence is not capability proof. A native setter that
returns a different current selection is refused before any prompt is sent. Unknown fields are
refused; consoles refuse these writes to a source below API 79 before sending.

Changing Agent or runtime settings advances the immutable instruction/config
revision. A running turn keeps its captured settings. Its next turn starts a
new generation and cannot load an incompatible opaque native binding. An
alternate Agent receives its own defaults, not the primary Agent's model IDs.
Existing fallback and four permission choices retain their separate paths.

`notify=off` disables future successful main-task completion push intents and
the console completion sound for that Bot; omission retains the old enabled
default. Permission cards and their existing delivery remain independent.
This is a completion choice, not a claim that every activity alert is filtered.

The following account-profile and application-extension construction adds real
source-owned ports; see [Bot login profiles and selected application extensions](project-assistants-login-capabilities.md).
It preserves this memory/model path and distinguishes supported account/text-skill/
HTTP-connector behavior from the still-open API-key, selected-fallback and network
work. These selectors do not disable independently configured native tools.

## CodyNC comparison

Read-only reference: CodyNC `9f67eb09084db61b3cca9c84a11bc03df9de29b7`.

| Boundary | Reference | Clawdline construction |
| --- | --- | --- |
| Memory authority | `host/src/chat/memory/{api,mod,file_api,file_tools,tools,files}.rs`; `memory-store.ts` | Dedicated view/detail, real separate authorities, source revisions and guarded publication; no copied global layer |
| Memory UI | `MemoryView.tsx`, `MemoryFiles.tsx` | Actual source-scoped Project/Bot previews and full detail; recorded/unknown attribution and bounds |
| Model protocol | `host/src/agent/bot/launch.rs::select_model`; `agent/auth/models.rs` | Model first, then returned model-specific config; verified selections on discovery and execution |
| Fast mode | `host/src/agent/config.rs`, `bot/fast_mode.rs` | Only a current native select/boolean offer; no guessed flag or invented support |
| Editor | `BotEditorView.tsx`, `AgentModelPicker.tsx` | Primary Agent, model/options, completion notification, existing team switch; fixed Project and existing approvals |
| Login profile | `LoginProfileRow.tsx`; `host/src/hub/bots.rs` and `agent/login_profiles` | Default login remains in place; managed account slots now have guarded source selection and native lifecycle ports; API keys remain open |
| Plugins/network | CodyNC editor's plugin lists and `NetworkSwitches` | Selected application text skills and configured HTTP connectors are enforced separately; independent native tools remain inherited and true remote reachability remains open |
| Dreams/native memory | CodyNC memory opening and optional native-memory reader | Explicitly different policy: dreaming deferred; global native-memory reading not authorized |

## Compatibility, capacity and verification

Feature schema 13 protects terminal quota-attempt facts against older writers;
schema 12 added named topic-note protection and schema 6 introduced runtime
settings protection. The repeatable migration accepts schemas 1–12 and changes
feature metadata transactionally; old Bot JSON is not rewritten. New optional
zero fields are omitted, preserving
old archive hashes. Exports identify schema 13; restore still accepts supported
older archives. Pre-13 binaries refuse schema 13 before rewriting it. Rollback
therefore requires a compatible binary or an intentional backup procedure;
deleting data or lowering metadata is not a rollback step.

New registered bounds cover discovery slots, configuration fields/choices,
configuration value bytes and memory-preview characters. Existing page and
memory-body bounds are reused. Capacity tests preserve actual guard failures
before registration, regenerate the real register/reading snapshots, and read
durable per-Bot option counts/value bytes. Missing optional tables are known
zero; unreadable SQLite is unknown. Transient selector/preview bounds retain
no occupancy and are described as validation policies.

Verification entry points:

- `TestMemorySettingsStoredHTTP`: authenticated HTTP, Host, SQLite, Project
  service, actual local scripted ACP configuration and final reply; two Bots
  in the same Project, long detail, read-only writes and publication-time forget.
- `TestMemoryViewSeparatesProjectsMachinesAndAccounts` and
  `TestMemorySettingsRejectInventedConfigurationWithoutMutation`.
- `TestRuntimeAgentSettingsWaitForNextGeneration`, old-client/domain checks,
  bounded SQL detail/forget checks and same-size Project revision checks.
- `TestConfigurationDiscoveryRefusesConcurrentWaiters` and real capacity
  snapshots/durable readings, including unreadable storage.
- `TestMemorySettingsBrowser` plus
  `web/console/tools/project-assistants-memory-settings.e2e.mjs`: production
  components and SourceClient in English desktop and Taiwan Chinese narrow
  view; real HTTP/Host/SQLite, saved settings into scripted ACP, read-only UI,
  delayed layer/source rejection and pre-transport API downgrade refusal.

The browser fixture uses its own headless profile, closes its own tabs and
removes the profile/process afterward. All data and ACP replies are explicitly
synthetic. No installed vendor prompt, paid API, production daemon mutation,
dev installation, deployment or public Release is part of these checks.

## Cloud integration handoff

The public vocabulary, generated Go/TypeScript carry lists and SourceClient
map `project-assistants.memory-view` and `project-assistants.configuration`
as device-authenticated reads. Their exact query shapes match the local routes;
source minimum is API 79. The existing reply ceiling and typed refusal envelope
apply. Optional edit fields are added to both existing edit operations.

The original memory/settings handoff was integrated in
`6a6d4ca8ad8aa41a13108d003f9707d396ad11ca`: both runtime registrations,
query inventories, copied limits, carry inventories and the decoder golden
were completed. The following login/capability construction uses the complete
cloudops write scope and records its additional API 81 mirror requirements
in the linked document. Source construction still does not establish a
working production Cloud deployment or private-relay mirror acceptance.

## Live native context reports

The ACP client exposes an optional internal `NativeContextReader` through the
installed-session isolation wrapper. It retains only the current native
session's advertised `compact` command and reported token usage/window.
An unreported or malformed optional value stays unknown; a reported zero and
an explicit empty command list remain distinct from unknown. Reports can arrive
while idle or before the session reply, but are usable only after their native
ID matches that reply and under the client's complete execution fence.
Cancellation or connection failure makes the reading unusable.

The Host can now read that optional state through the owned-session wrapper
and an existing runtime actor. This read never calls the runtime factory,
allocates an actor or process slot, or opens/resumes a provider. The current
human reader, background source, canonical Project, complete execution fence,
active turn, native ID, binding and memory/instruction revisions are checked;
authority and binding are rechecked after the native cached read. An absent,
opening, busy, stopping, closing or cleanup-unknown owner remains unknown.
The owned wrapper also seals readings before native cleanup. Ordinary turns
still close their session when they finish, so this is not a persisted last
reading or an idle maintenance session. The internal Host/SQLite tests exercise
this composition, including a real Forget during the read; they do not establish
a production HTTP/UI feature or installed-binary recovery.

This live, in-process reading adds no HTTP operation, saved Bot field, API or
archive generation, model prompt or maintenance command. It neither infers a
model's window nor claims compaction support from a provider name. A dedicated
idle-only, fenced Host operation with durable intent, terminal and
unknown outcomes is still missing. No compression policy control is shown.
Scripted ACP pipe tests establish transport behavior, not installed-engine
compaction or production UI acceptance.

An optional native compaction port now reserves one idle client lifetime
under the complete execution fence and the adapter's actual command report.
It sends exactly one bare `/compact` text block through the adapter's ACP
maintenance convention, without chat instructions, memory, handoff or files.
The client advertises typed compaction notifications and requires a matching
start/terminal ID plus a qualified RPC reply. A lower token reading, missing
terminal, malformed reply, disconnection or external cancellation cannot
become success. Duplicate terminal notifications do not change the first fact.
The same pending result can be inspected again, but the client cannot send a
second maintenance attempt or accept a competing chat prompt. Native failed
or cancelled states do not assert that the transcript was unchanged.

This port is not wired to Host, HTTP or UI. Its caller must first own the idle
actor, check current account/Project/memory/binding authority and persist intent;
those durable admission, recovery and old-binary/archive boundaries remain
unimplemented. The scripted peer tests exercise protocol and cancellation,
not a vendor engine, real compaction effect or production acceptance.

### Controlled installed-native compaction qualification

On 2026-11-18, source `e66e63bc5b35d8b751de718fc340790790610599`
was exercised with installed Codex CLI `0.166.0` and ACP adapter `2.6.0`,
using an isolated Host/SQLite fixture and the existing signed-in native CLI.
Admission confirmed native automatic memory was disabled. The batch made
three provider Opens against one native conversation: a seed prompt, one
explicit bare `/compact`, and a recall prompt after compaction. The first
callback never started before its daemon stopped; independent absence of its
launch marker and private state confirmed zero native operations before the
batch was deliberately registered again.

The seed used the actual Host `send_message` tool once to return a random
synthetic marker. Maintenance resumed the same native ID under the complete
original fence, qualified the adapter's current command report, and made
exactly one compaction write. It returned a nonempty compaction ID and a
matched typed `completed` result with a successful RPC. The recall resumed
the same native ID, without an answer hint, chat handoff or injected memory
context, and used `send_message` once to return the correct marker. The
wrapper capped Opens and writes and withheld memory/dispatch authority.
Observed calls stayed within the cap, with no automatic retry or fresh-session
fallback.

Independent final-source checks found two succeeded Host chat turns, four
persisted messages, no active turn and zero Bot memory entries. Every native
close and Host cleanup succeeded. The Go test ran in 70.953 seconds; the
whole bounded batch took 72.603 seconds. These are one qualification's elapsed
times, not a comparison or an interaction-latency benchmark. Token readings
were not used to decide success or infer a compression ratio.

Actual native writes were two chat prompts and one compaction command;
provider-internal request counts and exact billing remain unknown. No direct
API key, account switch, production Bot/group write, deployment or daemon
restart was part of this batch. Task-private evidence preserves the full
opt-in source, fingerprints, native identity, qualified readings, actual tool
calls, source exports and result. Its fsynced private maintenance intent is
explicitly a test record, not a product Host admission/recovery receipt.

The source passed all five jobs in exact CI `44245454182`. This qualification
does not complete durable idle-actor admission, concurrent Forget/revocation,
maintenance recovery, old-binary/archive compatibility or a product control.
Those boundaries remain required before wiring Host, HTTP or UI.

## Named topic-note source foundation (API 89, schema 12)

A note is one Bot memory entry with `kind: note`, a title, a summary and a
body in `text`. Names reuse the existing name limit. Title, summary and body
share the existing message-byte budget; notes and short facts share the live
entry cap. The registered memory-preview budget coalesces summary indexes
without truncating the saved summary or body. No new capacity number is copied
from Codync. Named notes are excluded from automatic short-fact compaction.

Only name, ID and bounded summary enter assembled model context. The compact
native index query does not decode chat history or return note bodies.
`read_memory` accepts optional `entry_id` to read one complete stored entry
under the captured actor, full Project and current memory revision.
`search_memory` matches facts and note names/summaries, without searching
other Projects. Short-fact `edit_memory` does not treat a note summary as an
editable body. The owned `read_memory` index also returns the CAS revision
from the same SQLite snapshot. `edit_memory` accepts a separate `note` object
with title, summary, body and `expected_revision`. An `entry_id` selects one
existing note in this Bot; omission creates a deterministic new ID bound to
the captured turn and tool key. `forget: true` with an entry ID and revision
forgets its live body, index and recorded provenance. Short-fact and note
fields cannot be mixed, and no caller-selected Bot or Project is accepted.

A note edit hashes its literal request before reading current state. An exact
retry reads the original immutable memory event, including after a later edit
or forget; a changed payload with the same key conflicts. A revision conflict
requires a fresh index and a new intentional edit, while an unknown result
requires the original key and payload. The existing Host lifetime, active-turn,
current-grant and peer-request restrictions enclose this same tool path.
The Bot layer has an explicit named-note editor. It lists names and summaries,
then reads the full body only when that entry is opened. Creating a note or
editing a detail captures the displayed memory revision; refreshing the index
does not silently rebase an unsaved draft. A known conflict retains that draft.
A lost acknowledgement retains the original command and literal note fields,
locks changes to them and offers an explicit retry of that same request.
Switching between Details and Memory preserves the editor. Short-fact writing
and bulk forgetting remain available in collapsed sections. Project shared
memory remains read-only, and sources below API 89 expose no note-write action.

Human writes retain the existing authenticated memory route and revision CAS.
A new client refuses note metadata before transport to a source below API 89;
plain short-fact writes keep their earlier API tier. Memory command receipts
already use `(machine, Bot, operation, key)`; newly written memory event IDs
now use that same complete identity so two Bots can use equal intent keys
without a machine-wide event collision. Original receipts and event IDs are
not rewritten, and an original replay still returns its original revision.

Forgetting tombstones the note and erases its title, summary and body from
live memory storage. Index, detail and search cannot retrieve it or resurrect
its ID. Legacy short facts retain their forgotten historical entries; those
entries are excluded from live index, detail, search and assembled context.
This is not an erasure claim for every historical fact. Existing human history
and independently saved exports are separate;
no provider conversation file is deleted. Forget still invalidates unsafe
opaque resume and dependent routine progress. The Project shared-memory view
remains read-only and is not added to model context. Dreams and a full note
version-history browser remain deferred.

The migration from core schema 11 to 12 keeps existing Bot JSON/history bytes
unchanged. Store-level `Restore` and `RestoreArchive` tests cover supported
older short-fact archives; note-bearing archives cannot pretend to be schema
11. These source interfaces are not a shipped archive-import command. The Bot
CLI, HTTP routes and Cloud vocabulary expose export, but no Bot archive import
or restore entry point. Project import and archived Session restore operate on
different data. Newer metadata must never be lowered for rollback.
Focused SQLite, owned MCP and scripted Host tests cover these source rules.
Editor behavior has its own isolated browser entry point below, separate from
production acceptance. The installed-native evidence below qualifies a
controlled Codex path; it does not turn the scripted checks into native or
production acceptance for both agents.

### Isolated core11 compatibility qualification

On 2026-11-18, the unchanged binary from
`90c7c21fd45a8f1d2efb6aecc7f4692637d874e8` and an isolated prototype from
`05e9fb26ea4f712c9b1fe329941801d5df158b2b` exercised the same synthetic
core11 SQLite baseline. The prototype keeps the original aggregate format and
stores topic notes separately under topic format 1. It is not merged into this
implementation and is not a migration or rollback path for production core12.
The actual prototype binary's version and console stamp matched its source;
the original binary was unchanged throughout the comparison.

| Boundary | Actual observation | Qualification limit |
| --- | --- | --- |
| Same data across binaries | The prototype wrote a note, the original binary directly reopened the same database and wrote an ordinary fact, and the prototype reopened it again. Both successful writes, two messages and one completed turn survived. | No snapshot, archive restore or lowered schema marker replaced the direct reopen. |
| Empty database | The prototype initialized core11/topic1. | This does not permit installing it over core12. |
| Prototype format refusal | Unknown topic version, missing topic marker, missing notes table and an actual core12 database returned typed `pa_unsupported` before changing SQLite logical contents. | These are the prototype's refusals, not the original binary's error classification. |
| Original Forget and delete | Both returned success and removed the applicable live topic content. | Independent exports and provider transcripts remain separate. |
| Combined capacity | The original binary made 253 successful HTTP writes to reach 255 facts plus one note. Another write was blocked without changing facts or the note; the prototype returned `pa_capacity_full` at the same total. | The original binary returned `pa_outcome_unknown`, not a known capacity refusal. |
| Capacity control | A derived synthetic copy had only its external note row removed through SQL. The same original binary then successfully wrote the final fact through HTTP. | The control preparation was not a successful HTTP deletion receipt or a change to the primary positive baseline. |
| Original ID collision and unknown topic format | Writes were blocked and contents remained unchanged. | Both returned `pa_outcome_unknown`; clear legacy refusal semantics remain unqualified. |
| Export versions | The prototype's no-topic wire export reported 11; note and note-tombstone exports reported 12. The original binary's wire export reported 11 and omitted the external note. | Export headers and preserved SQLite data do not establish archive import, roundtrip or old-reader version refusal. |

An earlier capacity preparation omitted a source Project and was rejected
before reaching the capacity guard. That failed preparation is retained and
does not count as capacity evidence; the later HTTP-filled comparison above
is a separate run. The qualification used no provider prompt or production
data. Task-private source, binary fingerprints, HTTP receipts, SQLite logical
comparisons and both failed and successful preparations are preserved.

The original binary and the prototype have no archive-input entry point.
Therefore archive11 import and archive12 rejection by the unchanged binary
remain **unexercised: no original entry point**. A new Go helper, JSON parsing
or an unsupported-route response cannot stand in for that binary consuming an
archive. Existing Store-level roundtrip tests retain their source-level scope.
Updater installation/rollback and the separately designated core12 recovery
binary are also not qualified by this trial.

The published release tags inspected through `v2.19.0`
(`69d25d02c653e8ca2848ca1610f384279d72a6e7`) contain no Bot archive consumer.
The first supported archive-input version must be identified by its actual
entry point, source commit and binary fingerprint, then qualify legacy and
current archives, integrity, scope, tombstones, refusal and recovery through
that entry point. Validation alone is not a restore roundtrip. Readable format
limits and the default write generation must stay separate; opening an empty
database or failing an import must not stamp a future generation. Only after
the designated reader and recovery binaries are qualified may new-format
writers be considered. No future generation is allocated here, and durable
Host maintenance writers remain closed.

### Controlled installed-native qualification

On 2026-11-18, source `6a1de817e050f2cfc4d9b3b623a794fbec78f422`
was exercised with the actual installed adapter, Host-bound tools and isolated
SQLite data. A private qualification wrapper deliberately requested
`session/new`, removed the chat handoff and omitted injected context text from
the outgoing prompt. The standing instructions and recall question contained
no preference answer. The actual memory tools, actor fences, revisions and
store were unchanged. This isolates memory retrieval from conversation recall;
it is not acceptance of a product new-conversation control.

| Stage | Observed result | Boundary |
| --- | --- | --- |
| Codex writes | Native `edit_memory` appended a short fact and created a named note using the revisioned index | One successful native prompt |
| Codex retrieves | A different native session read the index, explicitly fetched the note body by ID and returned both synthetic preferences correctly | One successful native prompt, without an answer hint or replayed chat |
| Codex after Forget | Independent reads found no live index, detail, search result or assembled context; another different native session read empty memory and answered `MISSING` | One successful native prompt after a completed Forget |
| Claude before sign-in | The first prompt returned `provider_signed_out`, with no Host tool calls | Unknown Host turn, batch stopped |
| Claude after confirmed sign-in | The first prompt in a new isolated Bot returned `provider_usage_limit`, with no Host tool calls | Unknown Host turn, batch stopped; the three memory stages remain unqualified |

The three Codex turns succeeded and their active-turn slots were empty.
The two Claude attempts also ended with empty active-turn slots, but their
unknown results are not success. Across these batches there were five native
Open attempts and five prompt writes: three successful Codex prompt writes and two
Claude refusals. Provider-internal request counts and exact billing were not
available; these counts are not a zero-cost claim. There was no automatic
retry, account switch, direct API-key request or production Bot mutation.

The after-Forget Codex check reused a known completed synthetic export by
restoring it into a new empty schema-12 database. It did not lower metadata,
restore a production backup or replay either earlier prompt. The private
fixture initially reset its `host-N` ID producer and collided with imported
IDs before provider Open. A zero-model comparison reproduced that failure and
passed with a distinct new ID namespace. The product UUID generator was not
changed. An earlier overbroad assertion also incorrectly required legacy
short-fact tombstone text to be erased; the corrected assertions retained the
existing distinction while checking both live detail refusals. Original
failures, source fingerprints, exports, tool records and native IDs are retained
in task-private evidence outside the public repository.

The containing source `659d85205b524273ce66e4c3a9144e9c36385031`
passed all five jobs in CI run `44232976610`. This does not establish
production Cloud observation, installed-binary upgrade/recovery, Claude memory
qualification or long-body native behavior. The full body bounds and editor
conflict/lost-acknowledgement cases remain separately covered by isolated tests.

The revisioned native writer is covered by
`TestHostTopicNoteEditsKeepRevisionAndReplayIdentity`: a fixture drops the
first successful acknowledgement after the real SQLite save, then retries
the same intent. It checks one entry, payload mismatch, stale revision,
replacement, provenance-dependent forgetting and original receipt replay
without resurrection. Owned MCP tests exercise the strict note shape,
index revision and rejected identity overrides. These are scripted local
tools, not an installed model's willingness to select them; clean native
conversation and UI acceptance remain separate work.


This writer increment uses the existing API 89 and core/archive schema 12
shapes already present in its previous source, rather than installing the
separately isolated additive-topic candidate. It changes no table, schema
marker or stored entry fields. Actual previous/candidate binary tests must
still establish that notes written after the update survive rollback to that
previous schema-12 binary. This does not make schema-11 binaries able to open
a schema-12 store and does not authorize lowering metadata or restoring a
pre-update snapshot over newer successful writes.


## Topic-note editor verification

`TestTopicMemoryEditorBrowser` renders the production Bots parent, Chat and
SourceClient over an isolated authenticated HTTP/Host/SQLite fixture. Its
Chromium/WebKit matrix covers English desktop and Taiwan Chinese touch input,
authorised read-only access and an API-88 source. It edits no production Bot
and asserts zero execution turns. The scenario covers creation, preservation
across Details/Memory switching, an acknowledgement lost after a successful
save, exact replay, body-free indexes, explicit body reads, an actual concurrent
write and revision conflict, same-ID replacement, forgetting and a refused
read of the forgotten detail. These are scripted browser checks; clean native
recall, updater compatibility and production visual acceptance stay separate.

## Offline archive recovery entry point

`clawdline project-assistants archive restore` consumes the Store archive
`{data, manifest}`, not the console's wire export. The local operator must name
an input file, a new database file and the exact expected machine, Bot and full
Project identity:

```sh
clawdline project-assistants archive restore --input archive.json \
  --database /recovery/new-bot.sqlite3 --machine MACHINE --bot BOT \
  --project PROJECT --canonical CANONICAL --kind repository
clawdline project-assistants archive export \
  --database /recovery/new-bot.sqlite3 --machine MACHINE --bot BOT \
  --project PROJECT --canonical CANONICAL --kind repository > roundtrip.json
```

These commands operate offline under the local operator's filesystem authority.
The expected identity is an explicit scope check, not proof of a paired grant,
Project registration or a signature on the archive. They do not access the
running daemon, start a provider, resume a turn or install the recovered database
as live state. A recovered database contains only the feature's schema and is
for isolated recovery inspection; replacing a live database with it is not a
supported installation procedure.

The reader admits archive generations 1 through 13 independently of the Store's
default export generation. The input is capped at 32 MiB (`bots.archive_bytes`);
larger files, trailing JSON, unknown fields, unsupported versions, manifest
mismatch, integrity damage and foreign identity are refused. Existing Store
validation preserves history, receipts, memory provenance, notes and tombstones;
its explicit collaboration/live-group transfer refusals remain in force.

Full validation and restoration take place in a private in-memory database.
Only after success is a complete, synced SQLite file staged in the destination's
parent and published with an exclusive hard link. An existing target, symlink
or SQLite sidecar is refused. A competing publisher cannot overwrite a winner.
Failure before publication leaves no target or staging residue. An uncertain
directory sync after publication or a lost success receipt is reported as
`pa_outcome_unknown`; the complete target is preserved and must be inspected,
not automatically imported again. Platforms without directory fsync retain the
file sync and exclusive-publication guarantees, without claiming Unix directory
crash durability. Export opens SQLite in read-only mode, requires the existing
current schema and rechecks identity; it never installs or migrates a schema.

`TestBotArchiveCLIConsumesActualLegacyAndReexportsRestoredFacts` uses the real
schema-1 producer fixture with its original manifest. The current-format CLI
checks include live and forgotten notes, complete re-export equality, typed
refusals, existing targets/sidecars, cancellation and concurrent publication.
These source-level CLI checks do not qualify an installed binary, updater
rollback, production Cloud reading or the unchanged older binaries that have no
archive-input command. The subsequent independent binary qualification below
adds evidence for specific reader/data chains without qualifying updater rollback
or every historical binary. No API number or archive generation is allocated by
this entry point; durable Host maintenance writers remain closed.

### Independent archive binary qualification

The following original receipts qualify a schema-12 reader. They do not qualify
an installed schema-13 reader, a schema-13 signed rollback chain or production
maintenance writes; those require their own exact-binary evidence.

The fixed reader source is `a3b7b1decc08b2ec95b6406d245b615c736d1645`.
Its actual darwin/arm64 CLI is 46,073,506 bytes with SHA-256
`ba71240fe78eea5f94b0a42801d2a5f9ef765f3affcfb78feff455a30c47e5a7`.
An independent runner used immutable synthetic input copies and the real
`90c7c21fd45a8f1d2efb6aecc7f4692637d874e8` and
`7654eff35a9a238086d7e73fb3305707748b0dda` binaries, rather than modifying an
old binary to add an archive entry point. Its selected final chains establish:

- The frozen schema-1 producer archive restores with its original manifest;
  messages, turns, memories and receipts survive a complete re-export comparison.
- A real HTTP write through the old core-11 binary survives the candidate opening
  and migrating an isolated database copy, exporting/restoring it through the
  actual CLI and reopening it with the core-12 comparison binary. The offline
  export command itself still refuses a core-11 database; direct daemon opening
  is a distinct migration step, not a new offline-reader capability.
- Real core-12 live and forgotten topic notes survive archive restoration and
  reopening with the comparison binary. Forgotten notes are absent from the live
  index and return 404 on detail reads; title, summary and body are cleared.
  Source identifiers, scope and revision remain as tombstone provenance. A new
  command key cannot resurrect the forgotten ID, while a different new fact can
  be saved and subsequently read by the candidate.
- The old core-11 binary refuses a core-12 feature schema, with the complete Bot
  archive unchanged. This is a feature-data comparison, not a claim that the
  daemon left every SQLite service-schema or shared-memory byte unchanged.

These conclusions use the runner's final `old-pipeline-3`, `current-pipeline-5`,
`offline-legacy-seatbelt` and `offline-current-v3` chains. The retained matrix has
674 rows and 380 assertions, including 21 historical false assertions from
setup and corrected test assumptions. The runner records a failed assertion
without necessarily raising, so process exit zero or a successful task alone
cannot establish that every assertion passed. Failed history stays available;
it is not relabelled as green by the final chains. Postflight checked the 536
incoming files against their manifest and observed cleanup of all 29 launched
processes and listening ports.

That matrix had no independently verified binary for the designated
`61f3c1065807439c2968c63acf4b61b029df3c4c` reader. The available `7654eff35`
comparison is not its substitute; the subsequent exact-reader qualification
below addresses that separate gap. Actual updater installation, service switching and failed-update
rollback still need a matching signed update bundle and an isolated rollback
environment. Older binaries without an archive-input command keep that explicit
qualification gap; it is not repaired retroactively by this reader. No original
positive-control state was reopened, no production database was replaced, and
no native model or voice request was needed for these binary/data checks.

This qualification does not admit a new-format writer or complete durable Host
maintenance, Claude native-memory qualification, continuous calls, editable
handoff cards or managed dev servers. See [remaining runtime construction](project-assistants-runtime.md#remaining-codync-runtime-construction)
for those separate ownership and failure boundaries. Cloud document reading,
production UI, running dev version and public Release acceptance each need
their own evidence.


### Subsequent exact-reader qualification

A separate independent run built the unchanged
`61f3c1065807439c2968c63acf4b61b029df3c4c` source, tree
`ba8efab3a7b5ef479d29633f800e85dc81bf91da`, in a clean isolated checkout.
Its darwin/arm64 binary reports `v0.0.0-dev.61f3c1065-reader748`, an unmodified
embedded revision, 62,423,394 bytes and SHA-256
`1818fb3f85f7222ae89d0bc2817e23001a16432cee34f416eb622067d3d2d6a3`.
It is a qualification artifact, not an installed dev or public Release.

The actual product HTTP Get, Export and memory-view/detail routes read the
same real core-12 synthetic database restored by the archive reader. Bot,
conversation, memory, entries, receipts, messages, turns, bindings and context
match the source archive; Events match after the explicitly documented existing
nil-array/zero-cursor wire projection. Live notes remain readable, forgotten
notes are absent from the index and return 404, and tombstone source identifiers
remain while title/summary/body stay empty. All rows in the original 20 feature
tables remain equal. GroupInstaller creates five empty tables and its schema
marker table, so neither the database file nor the complete table set is claimed
unchanged.

The final batch used two isolated daemons and 10 local GETs, with 58 passing
assertions including source preservation and cleanup. Across all three rounds,
17 GETs and five startup attempts were retained, with no mutating HTTP, provider
or paid content-API operation. The initial sandbox failure and two earlier false
assertions remain in the evidence; final success does not relabel that history.
The binary and all 13 incoming files retained their hashes, all owned processes
were waited and their ports were observed closed.

This closes only the designated binary's data-reading/visibility qualification
for that synthetic copy. It does not add a nonexistent archive-input CLI to the
old binary or qualify updater installation, service switching, signed bundles,
failed-update rollback, production UI or a new-format writer. Those recovery
and destination boundaries remain separate, and Host maintenance writers stay
closed.

## Project memory pages

API 98 accepts a page number and size on the existing authenticated memory
view. Page zero loads the Bot memory layer. The console reads Project memory
when its tab opens, then requests one page at a time in the existing index order. The usual
page contains 25 previews and the live total and capacity. The store scans
frontmatter to exclude archived entries and reads only the selected bodies. A detail uses the same captured conversation,
Project, source epoch and page revision. Earlier daemons and existing API
clients retain the original request shape.
