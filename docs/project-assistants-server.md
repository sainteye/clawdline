# Bot Server composition

This backend unit mounts the existing Bot HTTP module behind `Server.Handler`
and its real authentication gate at API level 57. It does not establish full
product, native-provider, MCP, Cloud deployment, create-form or mobile acceptance.
The [Host module](project-assistants-runtime-host.md) retains runtime ownership;
the [contract](project-assistants.md) defines Bot operations and wire values.

## One owner and an explicit opt-in

`Server.StartProjectAssistants(ctx)` reads `project_assistants_enabled` (a real
boolean, default false) and the existing Cloud IdentityStore under this Server's
state directory. It never enrolls a machine or uses an active Cloud connection,
hostname or client path as machine identity. No verified stored identity means
typed `pa_unpaired` and no Host. The existing private execution-machine seam is
available only to isolated fixtures and supplies no account-owner identity.

The Server retains one `projectassistanthost.Host` over
`store.ProjectAssistantDatabase()` and the existing Broker. New, hidden-feature
status, settings, creation and history construct no Engine or provider. A fresh
default-off database receives no feature tables. Startup calls durable Disable
before a later Enable, including for saved queues without any live actor.
Cleanup failures retain the original Host and its unknown/pending owners.

`Server.StopProjectAssistants(ctx)` seals new writes, then closes that Host under
its registered total cleanup deadline before a shared database may close. It
does not close the database or ordinary Broker. The executable calls Start after
authentication is ready and Stop in the shutdown path. Repeated cleanup can retry
the same owner; a timeout never proves a native process disappeared.

## Settings and observable state

- Full GET/POST `/v1/settings` carries `project_assistants_enabled` alongside the
  existing settings. Its response reflects the persisted preference; absent or
  malformed stored values are false.
- Narrow GET/POST `/v1/settings/project-assistants` carries exactly
  `{ "enabled": boolean }`. GET reports actual admitted Host Enabled, rather
  than equating a saved true with successful runtime startup.
- POST is local-owner-only through both the outer gate and fresh Bot witness.
  Paired send, readonly, Cloud and an orchestrator token do not grant this
  machine-global opt-in. Browser settings cannot raw-save this key.
- Both write paths use the same coordinator outside the settings file lock.
  Invalid/null/duplicate/extra or trailing JSON is refused before lifecycle IO.
  Full settings continue accepting the existing unrelated keys and null rules.
- Disable seals admission, persists false, then quiesces owned work and durable
  history. A cleanup error leaves false persisted and returns typed failure.
  Enable keeps source admission sealed during cleanup, installation and save.
  Save failure disables the Host and attempts a durable false compensation;
  ambiguous persistence returns `pa_outcome_unknown`, never a success receipt.

Bot `/status` distinguishes hidden default-off sources from history-only retained
data, and never invents native capabilities. Host Usage and the capacity endpoint
report unknown cleanup as `Known=false`; zero live actors alone is insufficient
evidence of durable cleanup. The narrow settings response intentionally contains
no additional cleanup or history fields.

## Trusted authority and independent source policy

`auth.Authority.Verify` mints a private, nonserialized witness without keeping
the bearer. Bot composition calls `Revalidate` on every authority read. It checks
the issuing Authority, exact device ID, token digest, creation identity, local
identity and approval. Capabilities intersect the original witness, the narrowed
gate verdict and current grants. A removed Local flag cannot return after Cloud
actor-device or Tailscale narrowing. Constructed Verdicts, token rotation,
revocation and same-ID replacements do not inherit a proof. Existing generic
authorization does not use this new proof as a broader exemption.

Cloud authority additionally requires `CloudViewerOf(ctx)`, the transport's
verified envelope sender, the freshly loaded account identity and a fresh
`PinnedStore.ProjectAssistantGrant`. No body/header principal, reader, machine
or Fence grants authority. Account-paired devices share the local account-owner
principal/cursor namespace; invited viewers have separate namespaces. The owner
namespace helper derives identity only and does not confer permission. Readonly
viewers can read and acknowledge their own cursors. Only operate viewers can
change content. Missing, pending, revoked or downgraded pins fail closed.

The Host Source callback is separate current machine policy and captures no HTTP
bearer or viewer. Runtime admission revalidates a request witness when one is
present. Actual background lifetimes recheck source identity and current opt-in,
without depending on the originating HTTP credential. Account/machine identity
replacement refuses operations and retains the old Host; it never migrates its
data or private closures to the replacement.

The existing HTTP module still performs its final publication check, strict
JSON/query/ID decoding and typed error envelope. Only exact POST Bot `/read` and
`/observed` routes pass the outer gate's readonly-metadata exception. Other
methods, nested/suffix matches, encoded action aliases, dot IDs and path
normalization aliases do not receive it. Origin and JSON checks still apply.
Effect checks and final publication checks are distinct; neither is an atomic
lock over another process replacing a Cloud pin file.

## Selected Project and native boundaries

Project resolution reads the existing explicit PlaceRegistry and matches exact
PlaceID on the source machine. It inspects only the selected directory, compares
the saved canonical ProjectKey with current `ResolveScope`/`CanonicalProjectKey`,
and preserves the selected working directory for linked worktrees. Repositories,
worktrees and non-repository places retain their tagged scopes. Missing,
ambiguous, deleted, wrong-machine or changed-canonical selections are typed
refusals. Remembering an old path is insufficient. This code does not enumerate
home/provider history, launch a terminal or change the deliberately empty legacy
`/v1/projects` endpoint.

The production Provider is the installed Claude route (`InstalledClaudeProvider`);
without an explicit `setup-claude` an initial Send returns typed
`adapter_not_found`. Since 2026-11-14 the first version follows CodyNC: the
native CLI keeps its own tools, works in the Bot's Project as the person and
asks before acting; the person may allow an action once, and a standing allow
rule is refused. There is no OS sandbox and no read-only claim. Native config,
history and memory live in a feature-owned directory per Bot, Project,
conversation and revision. The production DispatchPolicy is fixed source
policy (`projectAssistantDispatchPolicy`): any exact relative file inside the
Bot's repository Project, a Claude worker in `edits` mode, and a timeout from
`capacity.MaximumBotDispatchSeconds`. HTTP and model arguments cannot widen it,
and the Broker still applies its path, symlink, worktree, quota and landing
rules. Private fake-provider fixtures exercise lazy first Send, admission
revocation, cancellation and Close deadlines without establishing native
acceptance.

## Capacity, verification and removal

Actor/process occupancy comes from actual Host Usage, including unresolved
owners. Exposed SessionUsage counters report the largest per-session occupancy
for their per-session bound. Uninstalled/unconfigured sources allocate nothing;
installed durable queues, descendants, frames and IO durations that Usage does
not expose remain unknown. Input validators and request-owned pages are described
separately. Limits reuse the existing capacity actor/process/prompt/cleanup
constants, with no additional operational bound or timer loop.

Isolated tests start real Server.New/Handler instances with temporary state,
real SQLite, gate device files, synthetic Cloud identity/pins and no production
port. They verify default-off table absence, full/narrow settings, local-owner
restrictions, metadata and history-only reads/export, durable opt-out without an
Engine, cleanup failure/retry, provider absence/laziness, source identity changes,
fresh grants and registered Project scope. Task artifacts retain actual command
logs and limitations; compilation is not destination acceptance.

Removal: remove this composition file/test, Server field/routes, start/stop
calls, the new setting/schema fields and Bot-specific gate exception; regenerate
contracts/routes. Remove the witness and namespace helper only after their last
caller is gone. Restore the previous Bot capacity measure implementation only
when no runtime is installed. Root owns transparency/catalog/generated Agent
route-guide entries. Disable and prove cleanup first; retain original Sessions,
Board, schedules, shared database and Bot history. Targets catalog, frontend
API-level support and agent choices, Cloud words, MCP, native enforcement,
real Cloud/mobile acceptance, integration/landing, install, Release and rollback
remain separate root-owned work. No external/model API call was made.
