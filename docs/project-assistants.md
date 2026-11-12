# Persistent project assistants: contract v1

This is PA-01's additive foundation, based on Clawdline
`8522f21290f848027f45ff2122a8bd527fb66883`. It installs no server, router,
runtime, provider process, feature switch, UI or production database table.
The zero value of `FeatureState` is hidden. The foundation being ready is not
product acceptance and does not make either native provider available.

The machine-readable dispatch input is [project-assistants-handoff.json](project-assistants-handoff.json).
The source and intentional differences are in [project-assistants-reuse.md](project-assistants-reuse.md).

## One data contract

Contract version: `project-assistants.v1`. Fixture version: `1`. Feature schema
version: `1`, independent of the host SQLite schema/user_version. The source is
`api/v1/project-assistants.schema.json`; the existing generator emits
`internal/contract/zz_generated.go` and `web/contract/src/generated.ts` together.
All wire names start with `ProjectAssistant`. Domain structs intentionally have
their own effect-free representation; PA-05 maps them to the generated wire.
There is no second hand-maintained UI Bot/Entry model.

Identity is `(machine_id, bot_id)`, never a name, role or terminal. Every Bot
has one persistent Conversation; turns and native ExecutionBindings are
separate. A source machine owns its SQLite facts and its monotonic event and
message sequences. A Cloud roster is a readable projection, not a second
authority or a merged memory store. Every collection is an array, never null.
Timestamps on the wire are RFC3339 UTC strings. Integers remain within the
JavaScript safe-integer range; PA-03/05 must refuse exhaustion instead of wrapping.

Project selection contains registered source-machine and Project IDs only.
The source resolver checks current registration and directory availability
before creation and before execution. `ProjectScope` retains the existing
canonical repository identity shared by linked worktrees, or the existing
non-repository place identity. Selecting a worktree does not create a separate
memory authority. A directory is an internal resolver result, never client cwd.
The requested Project ID must already be the canonical registered ID; clients
do not resolve arbitrary path aliases themselves.

The additive [registered Project catalog](project-assistants-targets.md) at
API level 58 supplies source-owned metadata through a dedicated paged endpoint.
It preserves the retired empty `/v1/projects` route. Its Agent options name
accepted configuration kinds, never native readiness. Project resolution still
runs before Create and execution; UI and Cloud consumption have separate
integration acceptance.

## Approved journey and fixed inputs

| Approved frame | Contract and fixed input/output | Owner |
| --- | --- | --- |
| BR-01 | Entry is always outline `blob`, `#e39567`; every activation opens the complete authorized roster with no selected Bot. Badge sums readable sources only; revocation removes their counts. | PA-04 component; PA-05 Session mounting |
| BR-02 | `ListRequest` / `ListResponse`, `BotSummary`, `SourceStatus`: name, appearance, Project/Agent, activity/time, unread cursor/count, pending permissions, version and freshness. Offline is stale cached data or an explicit unavailable source, never authoritative zero Bots. | PA-03 projections; PA-04 roster; PA-05 source clients |
| BR-03 | `Message`, `Turn`, `PermissionOffer`, `WorkLink`, typed events; `reply-explicit`, `reply-fallback`, `reply-empty` fixtures. User-visible messages and trace are separate. Phone Back goes to roster, then original Sessions. | PA-02 runtime; PA-04 chat; PA-05 authorization |
| BR-04 | The same source-keyed roster/chat data, left/right on desktop. First opening has an unselected prompt. Drafts and reading anchors are keyed by authenticated account plus machine/Bot and survive switching. Selecting/reading does not send. | PA-04 module; PA-05 route/focus host |
| BR-05 | `CreateSettings`: name, shape/color, Agent, source machine/registered Project, permission policy, fixed instructions plus captured role reference. `create-configuration-only`, linked-worktree, place and directory-gone fixtures. Stable client Bot ID plus idempotency key creates one Bot and zero executions. | PA-03 persistence; PA-04 form; PA-05 command |
| BR-06 | `Shape`: blob, squircle, wedge, pebble, tablet, hex, cloud, teardrop. Individual row/header share stored appearance; global entry never follows a Bot. Unknown imported appearance renders the fixed fallback without rewriting preserved data. | PA-04 SVG; PA-03 persistence |

N01's explicit fixed-entry rule supersedes older wording printed in the static
BR-06 image. The approved outline paths are supplied by the design's
`shape_path`, not a canvas-based shaded-dot renderer. PA-04 preserves that
provenance. No Bot microphone is initially shown: the existing voice bridge is
a single recorder and PA-05 has not supplied a safe ownership adapter. Opening,
closing, selecting and restoring focus never start or stop recording or work.

Create, list, search, get, history, mark-read and export are database/projection
operations: no process initialization, model turn or capability auto-install.
Native adapter unavailability does not prevent these operations. The first
send alone may create a native binding. Editing replaces only name, appearance
and instructions with `expected_version`; Agent/model/Project/policy switching,
archive and deletion are absent from v1. Instructions and role snapshots are
captured at the next idle turn. A running Turn's captured snapshot is immutable.

## Admission, delivery and retry

`Command` carries machine/Bot, a typed operation, idempotency key and a SHA-256
content hash. The source recomputes the hash from canonical validated JSON;
clients cannot assert it. For create, the stable opaque client Bot ID is also
the command subject. The receipt key is `(machine, Bot, operation, key)`, shared
across devices for the same authenticated principal. The principal is captured
by the transport and is not accepted from request JSON. Reusing the same key
with different content is `pa_idempotency_conflict`.

`accepted` is durable admission, `executing` means an external send may occur,
`delivered` means a durable visible reply exists, `observed` is an authenticated
device's acknowledgement of that receipt/event, and `read` is an independently
advanced visible-message cursor. Acceptance, a tool result or model text never
constitutes a human approval. `unknown` preserves uncertainty after a possibly
performed send; it is not queued, failed or safe to retry. `DecideReplay` returns
`inspect_unknown` for executing/unknown receipts and never launches them again.
An accepted/queued item may be recovered only when durable evidence proves it
has never crossed the executing/write boundary. Native RPC completion alone
does not establish visible-message delivery.

`Repository.Commit` is one compare-and-swap on the Bot aggregate version.
Facts, projections, events and the command receipt commit in one transaction
or none do. It increments the aggregate version even if a mutation changes
only messages/turns. A create expects aggregate version zero. All supplied
facts and their nested fences must belong to the same command subject. The
store rejects unknown fields/versions and inconsistent payloads, not just
malformed JSON. A receipt lookup checks key scope before comparing content.
No receipt needed to resolve an unknown effect or an in-window retry may be
evicted. Receipt retention and durable storage budgets are PA-03 operational
decisions requiring registration before implementation, not arbitrary defaults
invented by this foundation.

One Bot runs one turn at a time; other Bots are independent. At admission,
validate enabled state, current source authorization/freshness, Project
resolution, current fence, message/queue capacity and the expected memory
revision. An initial Conversation has `binding_id=""`, generation `0`;
`CheckSendFence` accepts only that exact initial identity. The first admission
atomically creates binding/generation `1`. All subsequent incoming events,
tools, permission replies and sends compare the full current fence. A new
binding requires a new ID and strictly newer generation, never a reused native
session under a changed number.

ACP incoming requests must remain independently serviceable while a prompt
RPC is pending. Stop cancels the active and queued turns; closing the view
does not call Stop. Permission answers bind offer, source, Project, Bot,
Conversation, binding, generation and active Turn. Pending/answered/expired/
cancelled/unknown are separate states. A repeated command returns its receipt
before revalidating an already answered offer. A new answer to an old offer
is refused. An allow option cannot widen the enforced read-only sandbox;
native mode changes and persistent escalation remain denied.

Within a turn, each `send_message` tool call is durably deduplicated by
binding/turn/call ID; identical text under different calls remains two
intentional messages. Once any explicit visible message exists, final model
text stays in trace. If none exists, promote nonblank final visible text once
under a deterministic `(turn, final)` receipt. With no explicit message and no
final text, finish truthfully with no reply and accessible trace. Cancellation
does not promote a late final response.

Read cursors are source-owned per Cloud principal and Conversation, shared
across its devices. `AdvanceRead` takes the maximum bounded by the last visible
message sequence. Receiving an event, rendering a hidden chat, trace updates
and permission cards do not mark messages read or add unread messages.

## Memory, forgetting and native admission

SQLite owns feature MemoryEntries, provenance, revisions and tombstones.
For Save, input `MemoryEntry.revision` equals `expected_revision`; the store
assigns the next revision on successful commit. The input cannot override the
source version. Markdown is an export/snapshot, not a second mutable authority. Existing
Project memory stays under its existing service. Save/Forget use optimistic
revision checks and commit their event/receipt atomically. A forgotten entry
or source cannot be saved back through a stale summary. Summary provenance
includes transitive sources; after a forget it must be rebuilt from retained
entries, never relabelled after merely removing a source ID.

Before assembling a turn, read `Memory.Current` / `Snapshot`. Immediately
before the one bounded process write, use `Memory.WithCurrent` to recheck
revision and provenance while serialized against Save/Forget for that Bot.
This is a per-Bot memory barrier, not a database transaction held across IO;
the callback must not reenter Memory or wait for the model. It is invoked at
most once. A write error may mean an unknown effect and must not trigger an
automatic callback retry. PA-02 chooses/registers the write deadline; PA-03
implements the barrier and persistence independently.

`CheckOpaqueResume` returns `resume_forbidden_after_forget` when the native
binding predates the forget fence. `DecideResume` allows `start_filtered` only
with a current, provenance-complete filtered snapshot and admitted scoped
capabilities. PA-02 safely terminates the old owned execution, creates a new
binding with a newer generation and sends only that snapshot. It must not
fork/replay the old native history or resend the whole visible chat. A queued
snapshot invalidated by a forget is refused before `session/prompt`.

Default native automatic memory is disabled for the feature process; proven
per-Bot/Project isolation is the only permitted alternative. `Capabilities`
contains actual CLI/adapter versions, scope and evidence; unknown/read-only-
unverified/memory-unverified modes refuse admission. These fields represent a
trusted local probe result, not client attestations. An actor, source status
or an installed CLI does not prove the sandbox. Native flags must be checked
against the exact installed CLI/adapter protocol; the legacy `-c sandbox_mode`
example has not passed the current CLI's argument parser in the supplied probe.

Forgetting applies to feature-owned memory and future assembled context, not
model training knowledge, independent external files, managed instructions or
other copies. If a permitted external source can reintroduce forgotten data,
disclose that limitation and deny any mode whose promised isolation cannot be
enforced. Do not alter global settings, credentials or Project instruction files.

## Bot execution authority

`AssistantExecutionActor` is a host-issued, server-resolved capability bound to
source, canonical Project, Bot, Conversation, binding/generation, operation
allowlist, expiration and revocation. It has no terminal/run/master credential.
PA-05 authenticates the opaque capability and consults current revocation for
every call; constructing the Go struct or sending its fields is not proof of
authenticity. Capabilities are kept out of list responses and model arguments.

The only operations are dispatch_work, read_work, propose_work and
publish_message. Even an issuer-provided operation list cannot add human Board
acceptance, completion, arbitrary terminal input, machine administration or
permission approval. `WorkBridge.Dispatch` uses the existing broker's exact
claims and isolated worktree checks. It persists the WorkLink and reports
accepted/executing/failed/completed/landed/unknown back to the original chat.
Task completion is not landing and result text is not a person's decision.

The PA-05 core seam allowlist is in the handoff. Preserve the existing Session
actor path. Never fabricate `via.run` or fall back to machine credentials when
the Bot actor is refused. A Bot without a human run may only create an allowed
proposal/link through the new narrow adapter; it cannot rewrite others' cards.

## Ports and source-specific UI client

Go ports are `Runtime`, `Repository`, `Memory`, `WorkBridge`, `EventFeed`, plus
the source `ProjectResolver` and opt-in `SchemaInstaller` seam in
`internal/app/projectassistant/ports.go`. This package imports only the domain,
context and database/sql. It does not import app/ports or projects, avoiding
the prior ports → projects → transcript → process → ports cycle.

PA-03 supplies `SchemaInstaller.Version/Install`. The test helper
`projectassistanttest.Bootstrap(ctx, installer)` opens a new caller-owned
in-memory SQLite DB and invokes that installer. No global `store/sqlite.go`
change is required to test the module. The installer touches feature-owned
tables/version only and preserves host `PRAGMA user_version` and unknown data.

PA-04 defines the single `SourceClient` interface in its claimed `client.ts`,
using generated request/response types. Its exact method map is in the handoff;
PA-05 implements it with a captured machine-specific relay client. Every
operation rechecks that capture's authorization revision. The global active-
machine singleton is not used after switching the UI target. The host supplies
all authorized clients; PA-04 federates source pages and keeps per-source
offline/error states rather than shrinking the roster to the selected machine.

## Capacities and failure ownership

| Register row | Maximum target | At full | Enforcement / measurement owner |
| --- | ---: | --- | --- |
| bots.name_runes | 128 Unicode code points | Refuse settings; preserve old Bot | PA-03 calls PA-01 validation; PA-05 measurement |
| bots.instruction_bytes | 16,384 UTF-8 bytes | Refuse new instructions; running snapshot unchanged | PA-03; PA-05 measurement |
| bots.message_bytes | 32,768 UTF-8 bytes | Refuse before acceptance; no prompt | PA-02/05; PA-05 measurement |
| bots.pending_turns | 16 waiting turns per Bot, excluding active | Refuse new admission, retain existing queue and receipts | PA-02; live count wired by PA-05 |
| bots.page_rows | 100 Bots/messages per page | Refuse oversized request; continue with cursor | PA-03/05 |
| bots.event_rows | 256 replay events per page | Refuse oversized request; expired cursor requires snapshot | PA-03/05 |

These are initial contract ceilings, not performance measurements. Runtime
overrides may lower them; integration validates against the effective lowered
value as well as these domain ceilings. All rows notify diagnostics and the
sender; no eviction silently loses a command. Before the module is installed,
HTTP readings explicitly report no allocation in this contract-only build;
this is not a native readiness verdict. PA-05 must replace them with actual
observations when installing the module, and report unknown if an installed
runtime cannot be measured. The two
register/HTTP snapshots and English/Traditional Chinese row help are kept in
sync. PA-02/03/05 must register their own process/RPC/event-buffer, durable
storage/receipt, polling and lifetime bounds with failure fixtures before
adding them. This foundation does not choose those unimplemented limits.

## Verification, disable and removal

Run the commands in the handoff. Policy fixtures execute against domain rules;
ACP and HTTP scenario fixtures are fixed input/output requirements for later
module implementations, not claims that an adapter or route was exercised.
Native prompts, production Cloud, browser product interactions, daemon restart,
paid API work and deployment are outside this foundation's verification.

Default-off, disabled-with-data and removal are different operations:

1. Never enabled: no entry and no module installer/process/subscription.
2. Enabled: source/Project opt-in admits metadata operations; native send still
   requires capability evidence.
3. Disabled with data: history/export entry remains read-only; reject create,
   send and permission answers, stop only owned processes/descendants and
   subscriptions/timers, and preserve unknown effects without retries.
4. Older daemon rollback: PA-03 tests a copy with the specified old binary,
   original Session/Board work, then upgrade; feature tables are preserved and
   host schema version is not raised by this module installer.
5. Full removal: delete the removable files and revert only enumerated core
   wiring. There is no Bots UI afterward. Preserve feature data. Prove original
   flows build/work, then restore an earlier export into an empty store and
   separately reinstall a compatible module to read preserved tables. A switch
   is not a removal rehearsal and data deletion is never implicit.

Exports include settings, conversations, messages, turns/bindings, permission
states, memory entries/tombstones/context, work links, events, receipts and read
cursors with contract/schema versions. PA-03 validates exact round trips and
rejects unknown future versions. PA-05 owns the final removal, authorization,
Cloud and cross-module failure-injection rehearsal; PA-04 returns for actual
desktop/phone interaction acceptance after modules are integrated.


## Module implementation checkpoints

The [runtime and ACP adapter](project-assistants-runtime.md) implement durable
queueing, fenced callbacks, permission lifecycle, filtered prompt context and
owned process cleanup. Scripted peer evidence and cross-compilation are distinct
from admission of a real native provider. The [Session host integration](project-assistants-host.md) connects the all-source roster and preserves the
existing voice owner; its synthetic browser matrix does not prove production
Cloud or device revocation behavior across an App unmount.

The application Service owns metadata operations, server-derived command hashes,
principal-scoped read projections and literal human memory entries. Its ports
have no startup effects: settings and history do not initialize a native process.
Before each new repository, memory or runtime effect, Service obtains current
authority again and requires the same principal, reader and authorization revision
as admission. It checks online freshness again for read/observed metadata. This
closes revocation or grant changes during Project resolution and other preparation;
it neither holds an external grant-store lock across the effect nor replaces
runtime execution fences or HTTP publication checks. A new request after grant
renewal keeps the original principal-derived idempotency hash. Replaying an
existing receipt still does not repeat the external effect.
The [Server composition](project-assistants-server.md) mounts the HTTP module
at API level 57 and supplies current gate authority, source identity, selected
registered Project resolution and an explicit local opt-in. Isolated Server
fixtures do not establish production Cloud, native or device acceptance. Merely
importing these modules does not enable the default-off feature or install tables.

The [broker bridge](project-assistants-broker.md) supplies a separate private Bot
capability, the existing isolated task admission path, and durable work-result
projections. It preserves the human approval boundary. Its current verified
module tests use a real temporary broker and fake worker launch; installing the
bridge in a production source host is a separate integration requirement.

Read cursors and observed receipts belong to the authenticated reader. Fresh,
readable retained history may acknowledge that reader's own view without granting
content, native, configuration or permission-answer writes. Offline, stale and
revoked sources cannot accept new acknowledgements. The transport must preserve
this distinction rather than requiring content-write authority for every POST.


The [SQLite implementation](project-assistants-store.md) preserves source facts,
reader cursors and unknown intents independently of the host schema. The
[lazy runtime host](project-assistants-runtime-host.md) composes this Store,
Service, Engine and Broker, and owns native entry lifetime and bounded durable
queue shutdown. Its real Store/Broker fixtures and fake providers prove module
behavior. The Server composition supplies settings and HTTP lifecycle wiring;
production Cloud, MCP, native admission and complete removal remain integration
acceptance work.


The [Cloud wire module](project-assistants-cloud-wire.md) carries complete Bot
requests and typed failures through the existing device-scoped bridge. Reader
acknowledgements remain read-level actions; content changes keep their write
gate. A whole archive that exceeds the existing plaintext ceiling is explicitly
refused rather than truncated. Transport module evidence is separate from
production Cloud rollout, long-history UI recovery and phone acceptance.
