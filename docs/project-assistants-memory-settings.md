# Bot memory layers and native runtime settings

Construction starts from `2282c91d0a88b66e25434abcefff2b75f122cb6c`.
This document describes source construction and scripted local verification,
not installed-native, production Cloud, phone or public Release acceptance.

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
unavailable. The Project view returns up to its existing 1,024-entry cap.
Every entry permitted by the two stores is therefore reachable. Out-of-band
data beyond those caps is marked truncated rather than presented as complete.

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

Feature schema 6 protects these settings against older writers. The repeatable
migration accepts schemas 1–5 and changes feature metadata transactionally;
old Bot JSON is not rewritten. New optional zero fields are omitted, preserving
old archive hashes. Exports identify schema 6; restore still accepts supported
older archives. Older binaries refuse schema 6 before rewriting it. Rollback
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
