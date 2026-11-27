# Bot login profiles and selected application extensions

This construction extends `b3a182748e8bfd1b6c538a26d7f428d90143ff14`
(API 81). Its login-profile and extension operations are API 81 with feature
schema 7; its API-key login secret is API 82 with feature schema 8.
The folder/transport construction advances this to API 83/schema 9; see
[Selected Bot skill folders and connector transports](project-assistants-extensions.md). Integration
must choose a higher monotonic tier if another API change lands first.
The login construction includes account and encrypted API-key profiles and
selected fallback identities. Application text skills and configured HTTP
connectors retain the previous implementation. Folder assets, selected stdio and dedicated connector credentials are described in the extension construction.
Network reachability is described in [Bot network source boundary](project-assistants-network.md)
and [Cloud source composition](project-assistants-network-cloud.md). No deployment,
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
on a different Agent gets that Agent's own default login and settings. Each optional `fallback_steps` entry names an Agent and its own opaque login
profile. The same Agent may appear with distinct logins. Legacy
`fallback_agents` remains compatible; omitted or empty legacy edits preserve
selected steps, while conflicting nonempty representations are refused.

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
to install or execute a package. Selected folder skills now use the execution-owned asset materializer described in the extension construction. Selected contents are bounded
by the existing instruction-byte ceiling.

HTTP connector definitions are a source-owned `project-assistants/connectors.json`
file below application state. The private file contains `ID`, `Name`, `URL`, full
`Project`, `Agent`, and `Enabled`; unknown fields, symlinked definitions,
credential-bearing URLs and query strings are refused. The extension construction
also accepts `Executable`, `Arguments`, `CredentialHeader` and `CredentialEnv`
and validates them rather than refusing them. A definition is offered only for its exact source Project and Agent.
Reading this file installs or activates nothing. No connector is configured on
the person's machine by the task. The extension construction adds selected stdio and credential-backed HTTP/stdio adapters while preserving these HTTP definitions.

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

## Network reachability

CodyNC's `NetworkSwitches.tsx` means remote Bots on other computers in a joined
network, or Cloud assistants in the account. It does not mean Internet egress.
No network/Internet switch or effective-looking saved flag is added here; the
Bot network settings, typed Bot endpoint and Cloud-assistant principal were built
later and are described in [Bot network source boundary](project-assistants-network.md)
and [Cloud source composition](project-assistants-network-cloud.md).

The Session peer path is execution/Session based:
`domain/agenthandoff.Endpoint` requires a Session ID and execution generation;
`transport/http.Server.PeerExecute` admits that execution, stores a peer inbox
and delivers a broker notice to the Session. `PeerControlInput` grants also name
source/target Session generations. A Bot endpoint or Cloud-assistant principal
is never substituted into those calls. A Bot ID, local-team membership, a
browser bearer or an ordinary person's paired phone is not such a principal.

The Bot rail uses a separate typed `agenthandoff.BotEndpoint` bound to account,
machine, Project, Bot, conversation and generation, and a distinct
Cloud-assistant protocol; its remaining acceptance is listed in the network
pages. No existing peer crypto/grant policy or paired-person write semantics are
changed.

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
and operations below API 81 before transport. Operation decoding and registration
belong to the source daemon and hosted console. The private Relay validates the
signed encrypted envelope, account, channel and device capability; it forwards
the ciphertext without parsing Bot operation fields or secret values. New Bot
operations do not require another operation vocabulary in the Relay. Hosted
acceptance still requires a compatible source, console and an observed encrypted
round trip; this construction does not claim production deployment.

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

The folder-skill and stdio/authenticated connector extension is now documented in
the linked API-83 construction. Remote-Bot/Cloud-assistant reachability is documented
in the network pages linked above. This does not complete the parent Board step or
its later installed/Cloud acceptance card.

## API-key Secret Handoff and protected-file vault

API 82 adds the dedicated `project-assistants.login-secret` encrypted Cloud
command and local `POST .../login-secret` endpoint. Ordinary profile actions
accept `kind: api_key` on creation and `prepare-key` to obtain an expiring
source-owned target. `submit-key` is refused on the ordinary action route.
Only the dedicated person input carries `value`; there is no terminal Session,
human bearer handed to a Bot, shell import, environment dictionary or argv key.
A target binds current source/principal authority, full Project/Bot/conversation
fence, Agent, profile ID and revision. Targets expire after the existing
15-minute Secret Handoff period and disappear on source restart. Up to 128
unexpired targets may exist in one registry; the capacity panel reads their
actual count. The 16 KiB input ceiling reuses Secret Handoff's registered bound.

Persistent submission receipts hash only the nonsensitive operation and scope,
never the value, length or a content fingerprint. Replaying the same operation
returns its original acceptance even with a different or empty value; it cannot
compare or replace a key. The browser persists only that nonsensitive command
for explicit recovery and clears the dedicated textarea before awaiting IO.
The source rechecks authority before persistence and before publishing success.

`botprofilevault` uses AES-256-GCM, fresh nonces and source/Agent/profile AAD.
Its purpose-specific key is an owner-only file, independent from all Cloud
identity/content keys. This is protected-file storage, **not an OS or hardware
keychain**. Private directory components, regular-file identity checks, exclusive
key creation, synced temporary writes and atomic replacement protect its path.
An existing namespace with a missing/unreadable key, tampered ciphertext or
missing encrypted record is unknown/refusal; it never mints replacement
authority over existing state. SQLite stores only profile kind and lifecycle
metadata. No credential or credential fingerprint enters DB, export, receipts
or fixture diagnostic output. Unknown writes/cleanup retain bytes and holds.

The first supported allowlist is deliberately one key per supported Agent:
`ANTHROPIC_API_KEY` for Claude Code and `CODEX_API_KEY` for Codex. The installed
Codex binary contains both CODEX_API_KEY and OPENAI_API_KEY support; the
application selects CODEX_API_KEY explicitly. Each key profile retains its own
empty native configuration/history namespace. Capability/version discovery,
model catalog, memory-isolation probe and execution use the same selected
identity and its key. Saved means locally available, not provider-verified.
Native automatic memory stays disabled; idle dreaming remains deferred.

Rekeying and removal share the selection/hold lock and refuse all primary and
fallback references and native/probe holds. Changing selections advances
instruction/config revisions; captured native turns retain their selected
identity. Per-login rests, attempts, parked history and rest clearing use that
identity. Safe fallback still requires confirmed no send/effect and confirmed
cleanup; unknown/late outcomes remain barriers. A different Agent's default
login never inherits the primary profile or model settings.

Verification includes actual authenticated HTTP/Host/SQLite and local ACP key
execution, source/Host reopen, different-value/empty-value replay, expiration,
encrypted-at-rest inspection limited to owned fixture files, vault tamper/key
loss refusal, same-Agent/two-profile fallback and different-Agent default
isolation. Production UI/SourceClient is exercised in English desktop and
Taiwan Traditional Chinese narrow view. These synthetic checks make zero
vendor/model API calls and do not establish installed-native, Cloud, phone or
public Release acceptance. Root retains deployment and integration.

## Primary Agent changes and stored generations

Changing the primary Agent is a configuration change, not a Project move. The
store accepts the change only with a newer instruction/configuration snapshot;
Bot key, Project and conversation remain immutable. The next native turn gets
its own compatible binding. A real HTTP/Host/SQLite/local ACP regression covers
a completed Codex turn, saving Claude as primary, and a separate Claude binding
answering the next turn. This is scripted protocol evidence, not installed-model
acceptance.
The source records the revision that superseded each former primary binding.
This preserves old no-fallback histories and archive restores without inventing
attempt facts. Later native updates cannot alter or erase that stored provenance.
