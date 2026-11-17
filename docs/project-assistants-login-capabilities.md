# Bot login profiles and selected application extensions

This construction starts from `f5da4df1c328c29e6f65b5e805db25ed61de4281`
(API 80). Its compatibility tier is API 81 and feature schema 7. Integration
must choose a higher monotonic tier if another API change lands first.
The construction is **partial**: account profiles, application text skills
and configured HTTP connectors are implemented. API-key profiles, selected
fallback profiles and network reachability remain open. No deployment,
installed-native account acceptance or public Release is claimed here.

## Account identity and native launch

An omitted/empty Bot login selection uses the existing native login in place.
A managed account has an opaque random ID, Agent, human name, revision and
typed sign-in state. The source account/machine namespace owns its SQLite
record. Neither HTTP nor a model can supply a directory, executable, environment
dictionary or token. Source account changes cannot resolve another account's
managed IDs. Missing, removed, wrong-Agent and uncertain profiles fail before
native launch; no refusal silently selects the default login.

`botloginprofiles.Registry` owns slots below the source's private state, outside
the canonical Project. It rejects symlinks and non-private managed directory
components. A profile action also resolves the full Project through the existing
service authority. A repository or ordinary place containing state cannot host
a selected managed profile. Slot names never come from a client path.

Claude receives `CLAUDE_CONFIG_DIR`; Codex receives `CODEX_HOME`. `HOME` stays
the person's home. Every native launch uses an explicit environment; no shell
environment is imported and no process-global environment is changed. Account
slots are empty native configurations: global auth, settings, skill and session
folders are never cloned or linked. Native Project instructions still follow
the existing provider behavior. No account identity is inferred by reading tokens.

Installed CLI help was inspected for the native protocols: Claude's `auth login`,
`auth status --json`, `auth logout`, and Codex's `login`, `login status`, `logout`.
Production sign-in starts only after a person's explicit action. Its browser
callback belongs to the CLI. The application discards native login output and
never renders callback URLs, codes, internal paths or credentials. Browser or
terminal fallback that requires manual code entry has not had real-account
acceptance; the local protocol fixture is not that evidence.

Source-owned profile environments cover executable capability/version probes,
model discovery, Codex's effective memories check and real ACP execution. A
managed Codex account must report its own memories feature disabled. Claude
keeps the existing explicit automatic-memory/dreaming suppression. A default
account's memory receipt does not qualify a selected account.

Selecting another profile advances the Bot's configuration/instruction revision.
An active turn retains its captured Bot and profile. The next generation gets
the new profile and cannot load the old account's opaque native history. Native
owner identities include profile identity while retaining legacy default-history
bytes. Attempt facts, bindings and rests also retain profile identity. A fallback
on a different Agent gets that Agent's own default login and settings. This slice
does not expose a selected fallback-profile chain or same-Agent account rotation.

## Commands, sign-in and cleanup

`GET /v1/project-assistants/{bot}/login-profiles` takes explicit `machine_id`,
`conversation_id` and `agent`. `POST .../login-profile` takes the existing
authenticated command, full execution fence, Bot version, profile revision and
one of `create`, `rename`, `start`, `cancel`, `check`, `remove`. Request bodies
are closed shapes. Source authority is checked at admission, again before the
native effect, and before publication. The initial no-binding fence is accepted
only when it exactly matches the source's initial conversation.

The source commits the command digest, acceptance snapshot and profile mutation
before native sign-in. Replaying a lost start response returns that original
snapshot; it never starts another login. Acceptance is not sign-in success.
GET reports subsequent `accepted`, `running`, `signed_in`, `signed_out` or
`unknown` facts. Only native status establishes signed-in state. Native login,
status and sign-out use owned process census/cleanup and wait for shutdown.
Feature disable suspends owned logins; Host close joins their finalizers.

Browser recovery persists the original nonsensitive command under account,
authorization revision, machine, full Project and conversation. Explicit
recovery validates its scope; mounting, refreshing and source changes never
automatically replay it. Stale asynchronous responses cannot publish into a
different sheet/source. Read-only paired devices may list profiles; profile
actions retain the ordinary write gate.

Reauthentication refuses any persisted Bot selection or active native/probe hold.
To sign into a different account, first deselect the profile from every Bot (or
create a new profile). Deselecting and reselecting advances each Bot’s instruction
revision, so a replacement account cannot resume its earlier opaque history.
An existing start receipt still replays without launching another login. Native
probes and execution refuse a signed-out managed profile. The settings sheet
waits for signed-in state before exploring its model catalog, and refreshes
discovery when sign-in completes.

Profile selection, sign-in and removal share the registry lock. Removal refuses current
Bot references, active native/probe/login holds and uncertain effects. Execution
holds are durable: restart cannot mistake lost cleanup for an unused account.
Restarted accepted/running logins become unknown, and unknown credentials remain
preserved. Successful removal first revokes the identity, then runs native
sign-out and removes its owned slot. A sign-out or cleanup failure preserves
bytes and a `cleanup_unknown` record instead of claiming deletion succeeded.
Automatic reconciliation of uncertain owners is intentionally unavailable.

## Application-managed extensions

`GET .../extensions` reads the actual application skill catalog and configured
HTTP connector definitions through the source. `extensions` on both ordinary
and team settings is optional; omission preserves it for older clients, while
explicit empty skill/connector arrays deselect. The edit carries only stable
IDs and fingerprints. Source validation checks fresh definitions before save,
before native launch and during publication. A changed or revoked fingerprint
requires a fresh deliberate selection.

Production skill inventory uses the existing Squad catalog, including installed
custom definitions. Supported text-only skill contents are added only to the
selected Bot's assembled system context. Metadata is task data, never permission
to install or execute a package. Folder skills with assets are excluded until
their immutable asset-materialization port exists. Selected contents are bounded
by the existing instruction-byte ceiling.

HTTP connector definitions are a source-owned `project-assistants/connectors.json`
file below application state. The private file contains `ID`, `Name`, `URL`, full
`Project`, `Agent`, and `Enabled`; unknown fields, symlinked definitions,
credential-bearing URLs, query strings and environment/command fields are
refused. A definition is offered only for its exact source Project and Agent.
Reading this file installs or activates nothing. No connector is configured on
the person's machine by the task. This port currently supports unauthenticated
HTTP endpoints; stdio and secret-backed connector adapters remain open.

The native session receives only selected connectors in actual ACP `mcpServers`,
beside mandatory Host tools. The endpoint is an execution-owned loopback proxy
on the existing MCP listener with its existing process/RPC budgets. A native
request must present that execution's bearer and selected connector index. Every
call rechecks the current Host turn, source authority and fingerprint, and
journals activity before the backend effect. The proxy rechecks before publishing
bytes, strips the execution bearer from backend requests, refuses redirects and
uses bounded requests/replies. Revocation before a call prevents it; revocation
after an external effect refuses the reply with an unknown-outcome error and
does not retry. Selected skill removal also blocks subsequent prompts and native
event publication; it cannot erase guidance already read by an active process.

These selectors govern **application-managed additions**. They do not claim to
disable independent native Project/global tools, hooks or MCP configuration.
The bilingual UI states that boundary. The visual community plugin system and
opt-in toolbox are separate and are never treated as native connector catalogs.

## Network reachability remains construction work

CodyNC's `NetworkSwitches.tsx` means remote Bots on other computers in a joined
network, or Cloud assistants in the account. It does not mean Internet egress.
No network/Internet switch or effective-looking saved flag is added here.

The current authenticated peer path is execution/Session based:
`domain/agenthandoff.Endpoint` requires a Session ID and execution generation;
`transport/http.Server.PeerExecute` admits that execution, stores a peer inbox
and delivers a broker notice to the Session. `PeerControlInput` grants also name
source/target Session generations. There is no Bot endpoint or Cloud-assistant
principal to substitute into those calls. A Bot ID, local-team membership, a
browser bearer or an ordinary person's paired phone is not such a principal.

The remaining boundary is a typed Bot execution endpoint bound to account,
machine, full Project and current Bot generation, its publication/admission
policy on the existing signed/encrypted peer transport, and a distinct
Cloud-assistant issuer/service. It still needs the two isolated real-source
transport proof requested by the task. No existing peer crypto/grant policy or
paired-person write semantics are changed. This open requirement is not
cancelled or marked complete.

The pinned CodyNC Git tree contains the login-profile modules, editor,
fallback identity, launch/skill/connector integration and both referenced
`host/crates/accounts` and `host/crates/network` source trees. A sparse reference
checkout may omit their working files; inspect the pinned Git objects with
`git show` or `git archive` before declaring a reference missing. The current
supported subset above is not a complete upstream crate/protocol comparison;
the remaining implementation must also use those fixed reference sources.

## Compatibility, bounds and verification

API 81 adds four carried words with real runtime registration:
`project-assistants.login-profiles`, `login-profile`, `extensions` and
`configuration-profile`. The last retains the configuration route but includes
an explicit draft `login_profile`; old configuration requests without the key
retain their existing saved-profile behavior. SourceClient refuses new fields
and operations below API 81 before transport. Private Cloud relay mirrors must
carry these four shapes and the new optional edit/team-settings fields before
hosted acceptance; this construction does not edit or deploy that repository.

Feature schema 7 adds source-owned profiles/holds and protects Bot selection
fields from older writers. Schemas 1–6 migrate transactionally without rewriting
historical Bot/event/receipt/archive bytes. New zero fields are omitted from old
hash inputs. Registered limits cover 32 managed profiles per source account and
machine and 32 selected application extensions per Bot. Native login output,
connector definitions, connector RPCs and context reuse existing registered
byte/process policies. Durable capacity readings query actual SQLite rows;
unreadable storage is unknown, never an invented zero. Red-before-registration
logs and the real register/reading golden snapshots are retained.

Verification entry points include:

- `TestLoginProfilesStoredHTTPAndNativeIsolation`: actual authenticated
  HTTP/Host/SQLite, two local native sign-ins, exact lost-response replay,
  two managed/default ACP executions, busy removal and wrong/missing identity.
- `TestProfileAuthorityBusyRemovalAndRestartKeepUnknownBytes` and
  `TestProfileSlotsRejectSymlinksAndProjectOverlap`.
- `TestRuntimeAgentSettingsWaitForNextGeneration`, native account-history
  separation and selected-home Codex memory-policy probes.
- `TestIndividualExtensionsCrossStoredHTTPAndActualACP`: real selected skill
  context, actual connector HTTP call through the owned proxy, deselection and
  revoked-definition save rejection.
- `TestConnectorProxyChecksCurrentAuthorityBeforeEffectAndPublication`:
  actual local backend, wrong bearer/index, pre-effect revocation and late
  publication refusal. Inventory tests also reject wrong Project/Agent and
  stale skill fingerprints.
- `TestLoginProfilesBrowser` and the task-owned browser harness: production
  components/SourceClient in English desktop and Taiwan Chinese narrow view,
  real source commands, two local native sign-ins, saved selections and ACP
  turns, read-only UI, API downgrade and late-source rejection.

The browser owns and closes its targets, process and temporary profile. All
native accounts, definitions, replies and HTTP connector content are synthetic.
No real credentials, vendor/model prompt, paid API, global configuration copy,
dev install, deployment or public publication is part of these checks. Initial
sandbox denials of `/bin/ps` are retained separately from permission-enabled
native/browser passes. The decoder golden audit retains 276,750 previous inputs
with zero removals or changed decisions; added probes are distinct new inputs.

Remaining construction is explicit: API-key Secret Handoff/vault integration,
selected fallback login identities, folder-skill and stdio/authenticated
connector support, and true remote-Bot/Cloud-assistant reachability. These
limitations do not complete the parent Board step or its later acceptance card.
