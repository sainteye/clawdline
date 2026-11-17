# Bot network source boundary

The normal Host prerequisite, dedicated machine API/socket adapter, local consent
sync ingress, API 86 dependency and remaining acceptance boundary are described
in [Cloud source composition](project-assistants-network-cloud.md). The frozen
wire and authority rules below remain unchanged.

This source construction extends API 83 and Bot schema 9. Bot schema 10 adds
an account-scoped, source-owned preference table; it does not rewrite historical
Bot aggregates, events, command receipts or archive data. Public Release still
hides Bots. This document does not establish production Cloud, installed-native,
phone or public Release acceptance.

## Versioned source protocol

The Bot-only rail is independent from the closed Session peer protocol. Its
composition adapter accepts `POST /v1/bot-network/exchange` with
`{header,ciphertext,nonce,key_id,signature}`. `header` contains integer `version:1`,
`request_id`, `correlation_id`, `kind`, `source`, `target`, `pair_id`,
`authority_id`, integer `authority_epoch`, and lowercase SHA-256 `body_digest` of
the exact plaintext bytes. Each endpoint contains `account_id`, `machine_id`,
`project_id`, immutable `bot_id`, `conversation_id`, and the actual nonnegative
integer stored `generation`. A new unbound conversation's real generation is
zero. No Session identity is fabricated. Names, handles and paths are not
authority. Both endpoint accounts must match.

Kinds `directory` and `message` have an empty correlation ID. `read` and `reply`
require a nonempty correlation ID different from their request ID. HTTP replies
swap source/target, use kind `reply`, request ID `lowercase_hex(SHA256("bot-network-response-v1\n" + original_request_id))`, and
correlate the original request ID. Callers use short generated request IDs so
that this prefix remains within the existing 128-byte segment policy. The
source caller verifies the complete response header before decrypting it.
`message` plaintext is `{"text":"..."}` under the existing collaboration
request limit. Unknown HTTP fields/trailing JSON are refused.

Encryption reuses the existing pinned signed pair transcript, X25519/HKDF-SHA256
`PairKey` and AES-256-GCM primitive, with separate Bot-only domains. Let `H` be
the canonical JSON header bytes from `cloud.CanonicalBytes`. The exact Ed25519
input is `clawdline-bot-network-v1\nH\nnonce\nkey_id\nciphertext`. GCM AAD is
`clawdline-bot-network-aad-v1\nH\nkey_id`. Nonce is fresh random 12 bytes;
ciphertext includes its 16-byte tag. Standard padded base64 is canonical. Body
and frame limits reuse the 512 KiB peer body policy. Existing Session and
discovery signature domains cannot open these envelopes.

`Store.PutGrant` stores separate Bot consent with account, stable source/target
Bot identities, epoch, expiry, revocation, complete signed pair transcript and
locally compared fingerprint. It verifies that transcript against this machine
and fingerprint before persistence. The source repeatedly reads current local
consent at send, admission, effect and publication. A machine pair alone does
not grant Bot authority. This local store does not perform private Cloud
authorization; production composition still needs independently authenticated,
fresh private paired-account authorization and revocation propagation.

Grant identity also pins both actual conversation IDs. Native generations may
advance under current execution checks; deleting and recreating a Bot with the
same Bot ID and a different conversation cannot inherit or move an old consent.

## Dedicated assistant principal

The separate adapter accepts `POST /v1/bot-assistant/call` with `{call,body}`;
`body` is a UTF-8 string preserving exact narrow-operation JSON bytes. `call`
contains integer `version:1`, `request_id`, `correlation_id`, `kind`, full
`target` endpoint, `grant_id`, integer `grant_epoch`, `key_id`, `body_digest`,
Unix-second `issued_at` and `expires_at`, and standard-base64 `signature`.
Signing input is `clawdline-bot-assistant-call-v1\n` followed by canonical JSON
of the complete call with `signature` set to the empty string, without a final
newline. The call window must contain current source time and fit entirely
inside the grant lifetime.

The source's independent human policy pins the dedicated gateway key ID/public
key and global opt-in with CAS versioning. A separate durable consent pins the
actual assistant principal, account/machine/Project, explicit Bot allowlist,
list/message/read permissions, epoch, revocation and lifetime. These must match
the policy key pin. The verifier derives opaque `VerifiedAssistant` only after
checking the dedicated signature and current policy/consent. Human viewer
bearers, browser cookies, roster membership and a claimed JSON principal do not
authorize it. A Bot setting cannot install trust, create consent or change global
opt-in. Management write ports are absent from all Bot and assistant tools;
production management composition must authenticate the human independently.

Allowed assistant operations are only `directory`, `message`, and correlated
`read`. Directory publication is filtered again by verified account, machine,
Project and Bot allowlist. There is no remote shell or assistant setting/memory
management operation. Current Host feature/write/target/reachability facts are
checked with fresh policy/grant reads before effect and publication. Native
`NetworkWork.Assistant` captures the actual principal, original signed call and
exact body bytes rather than inventing a requester Bot or Session. Peer text
retains its untrusted-work boundary and native permission policy.

Assistant receipts are durable before native submission and bind authenticated
principal and grant as well as account/machine/Project/target Bot. Full immutable
call digest includes epochs and conversation capture. Fixed-ID replay reads the
original receipt; changed bytes conflict; an orphaned claim remains unknown and
does not execute twice. Delayed reads must match the exact principal, grant
epoch and captured target. `Read` records source publication only and cannot
claim assistant observation or acknowledgement. Actual SQLite occupancy includes
terminal, revoked and unknown records; errors remain unknown readings.

These handlers are source composition adapters. Normal daemon bootstrap does
not install a gateway, mount these raw rails or fill `Network`,
`NetworkTransport` and `Assistants` automatically. Default source capabilities
remain unavailable/setup and nil services refuse typed unavailable. The private
service still requires closed Bot frame parsers, fresh dedicated Bot account/pair
authorization endpoints, authenticated opaque Relay routing and a genuine
external-assistant OAuth/MCP gateway with independent consent. Existing human
viewer OAuth is not that gateway. Source fixture success does not establish
production Cloud support.

Rerunnable proof includes `TestAssistantSignedHTTPActualNativeACPAndStoredReply`:
dedicated signed HTTP through AssistantService/Host/SQLite to an actual owned
native ACP fixture, one native execution under fixed-operation replay, exact
whitespace-containing signed bytes, and a durable correlated reply read. The
controlled source HTTP/SQLite tests separately inject grant revocation and
global opt-in changes before effect and publication. Initial sandbox unknown
native startup/cleanup and the permission-enabled passing harness are retained
separately. All identities and keys are synthetic; external API/model requests
are zero. The frozen downstream contract is retained as the task artifact
`CLOUD-NETWORK-CONTRACT.md` for Root's private implementation unit.

## Per-Bot admission preferences

`offered_to_network` permits discovery and messaging by Bots on authorized other
computers. `reachable_by_assistants` permits discovery and messaging by dedicated
cloud-assistant principals with matching account consent and Bot scope. These
are separate preferences, not Internet or firewall permissions. Neither switch
creates an account grant, machine pair, assistant consent, gateway trust or
global assistant opt-in.

Both preferences default to false for an existing or newly created Bot. Settings
are keyed by account, machine and immutable Bot ID, and also pin its canonical
Project identity. A different account receives its own default-off preference.
A rename never reassigns the identity. Old archives contain no network authority;
restoring them starts with default-off preferences in a new source namespace.
Schemas 1–9 migrate through additive DDL without rewriting their historical
aggregate bytes. Archive formats 8 and 9 remain accepted after the schema bump.

Human management reads a `NetworkSettingsView` containing saved preferences and
current source capabilities. Saving checks the actual stored Bot version, the
complete current conversation/binding/generation fence and the independent
settings revision. Optional omitted fields preserve their existing values; an
empty patch preserves the revision too. The source must independently authorize
the authenticated human before persistence and recheck authority before publishing
the result. Bot tools cannot change either preference or global assistant opt-in.

Capability states are `ready`, `setup_required` and `unavailable`, with an optional
stable diagnostic reason. Only current authenticated infrastructure may produce
`ready`; a remembered preference or an account roster entry cannot. Enabling a
preference without its ready capability returns `pa_unsupported` with field
`network_peer_setup` or `network_assistant_setup`. Disabling a preference is allowed
even after infrastructure becomes unavailable. Saved preferences may remain in
storage while inactive; they confer no delivery authority.

The dedicated settings component shows switches only for ready source capabilities
and otherwise explains the setup/unavailable state. A late save response cannot
replace a different Bot/account's settings, and an unconfirmed save requires a
refresh before another attempt. Global assistant account consent belongs to
separate authenticated human management, never to this per-Bot settings form.

Focused source evidence is provided by
`TestNetworkPreferencesNeverEnableMissingInfrastructure`,
`TestNetworkSettingsKeepAccountScopeCASAndHistoricalBytes` and
`TestNetworkMigrationPreservesSchemaNineAggregateAndArchive`. They exercise
actual SQLite migration, account isolation, stale-write refusal, omission
preservation, retained aggregate bytes, restart reads and legacy archive restore.
They do not prove a production Cloud gateway exists.

## Transport and gateway integration

The retained network receipt namespace has a registered ceiling of 64 records
per source machine, including terminal and unknown effects. At the ceiling,
new requests receive `pa_capacity_full` rather than erasing replay evidence.
The capacity endpoint queries the actual SQLite table and reports the largest
per-machine namespace. An absent optional table is known zero; unreadable storage
is unknown. It does not infer zero from an unconfigured runtime. The bound was
exercised red before registration, and the register/HTTP measure snapshots are
generated from their real implementations.

Dedicated assistant grants and receipts each have an independent registered
64-record ceiling per account and source machine. Revoked grants and unknown
receipts remain counted. A full namespace refuses a new identity or effect,
but does not prevent revocation of an existing grant or exact receipt replay.
Their capacity readings query actual retained SQLite rows and take the largest
account/machine namespace; they never sum unrelated account namespaces.

Machine-to-machine Bot grants likewise retain at most 64 identities per source
machine. New IDs are refused at the ceiling; a newer epoch can still revoke an
existing grant. Admission count and write share one SQLite transaction. The
capacity endpoint reads the largest actual machine namespace, including revoked
identities, and marks unreadable storage unknown.

The source Bot wire, its principal boundaries, durable receipt lifecycle and
private service integration contract are documented by the integrating task.
The private Cloud API, Relay and dedicated assistant gateway require their own
implementation and acceptance; ordinary encrypted Bot management operations use
the existing source/console vocabulary and opaque Relay forwarding.

## Human settings transport

API 84 adds authenticated `GET` and `POST
/v1/project-assistants/{bot_id}/network-settings`. Reads name exactly one source
machine and current conversation. Writes carry the generated command, full
conversation fence, Bot version, settings revision and a closed patch. A patch
uses optional objects such as `{"offered_to_network":{"enabled":false}}` so
both generated Go and TypeScript clients preserve omission separately from an
explicit false value. The source derives the account and Project; neither may
be chosen by the request. The read result includes the current fence and Bot
version for a subsequent compare-and-swap. The two ordinary Cloud operations
are `project-assistants.network-settings` and
`project-assistants.network-settings-save`; they do not carry network principal
authority and require no private Relay operation vocabulary.

Authentication still identifies the human manager independently of the source
account. A Host with no configured network uses an `unconfigured:<machine>`
preference namespace and reports unavailable capabilities. That namespace is
never migrated or promoted to account authority. Enabling either preference
requires its actual configured capability; a harmless false preference may be
saved without enabling infrastructure. The source rechecks human authorization,
account, Project, capabilities and settings revision before publishing replies.
The settings UI renders no enabled switch for an unavailable or incomplete
source. These preferences do not change global assistant consent.

`TestNetworkSettingsCrossAuthenticatedHTTPHostAndSQLite` exercises generated Go
requests through authenticated HTTP, Host and SQLite, including explicit false,
omission, stale revisions, absent authentication, read-only authority and a
refused enable on an unconfigured source. `TestNetworkSettingsBrowser` uses the
production settings components and SourceClient at English desktop and Taiwan
Chinese narrow widths; it also checks downgrade and delayed-response revocation.
Its fixtures configure no real account or grant and make zero provider/model
requests. Chromium's original sandbox refusal and the isolated permission-enabled
pass are retained separately. The harness closes owned targets and its browser,
then removes its temporary profile.

## Appendix: frozen downstream integration contract

# Bot network and external-assistant source contract, version 1

This contract describes the source implementation in the task worktree. The
private Cloud API, Relay framing and assistant gateway sections below are
required downstream work, not claims that those services already support it.
The public Session peer protocol is unchanged. A Session machine pair alone
never authorizes a Bot call, and a human Cloud viewer token is never an assistant
principal.

The HTTP handlers are composition adapters, not an instruction to mount a new
rail on an existing daemon. Source `Host.Options.Network`, `NetworkTransport`
and `Assistants` require independently configured composition. They remain nil
in the existing normal daemon bootstrap; this task does not enable or install
that infrastructure. Per-Bot settings therefore truthfully report unavailable
or setup state until real composition and authenticated service support exist.
A nil handler/service returns typed unavailable. The local test explicitly
composes its own isolated source instances and authority.

## Implemented source wire

The Go definitions in `internal/domain/agenthandoff/bot_wire.go` are normative.
`version` is integer `1`. The source HTTP transport is
`POST /v1/bot-network/exchange`, JSON `{header,ciphertext,nonce,key_id,signature}`.
It has no bearer-auth fallback. `Cache-Control: no-store` applies to all replies.
Unknown fields and trailing JSON values are refused by the HTTP decoder.

The complete header, including fields whose values are empty, is:

```json
{"version":1,"request_id":"request-1","correlation_id":"","kind":"message","source":{"account_id":"account-1","machine_id":"machine-a","project_id":"project-a","bot_id":"bot-a","conversation_id":"conversation-a","generation":1},"target":{"account_id":"account-1","machine_id":"machine-b","project_id":"project-b","bot_id":"bot-b","conversation_id":"conversation-b","generation":0},"pair_id":"pair-ab","authority_id":"bot-consent-ab","authority_epoch":1,"body_digest":"<lowercase SHA-256 of exact UTF-8 plaintext bytes>"}
```

There is no Session ID or execution-generation string. Bot IDs and Project IDs
are actual stored stable identities. Conversation generation is the source's
captured nonnegative integer; zero is an actual stored unbound conversation,
not a fabricated native execution. Account IDs at both endpoints must match.
Display names, handles, filesystem paths and caller-provided usernames do not
route work or establish authority.

Kinds are `directory`, `message`, `read`, `reply`. Requests of the first two
kinds have an empty correlation ID. `read` and `reply` require a nonempty
correlation ID different from their own request ID. In the source HTTP adapter,
a response swaps the original endpoints, uses kind `reply`, request ID
`lowercase_hex(SHA256("bot-network-response-v1\n" + original_request_id))` and correlation ID `<original request_id>`.
The source caller verifies that complete response header before opening it.
Request IDs must leave room for this prefix within the existing 128-byte
segment policy; callers should generate UUIDs rather than maximum-length IDs.
`message` plaintext is exactly `{"text":"..."}`; native request text is limited
by the existing 32,000-byte collaboration request budget. The source reply body
contains `receipt`, `bots` or a typed `failure`.

`directory` can structurally name a target Project with empty target Bot and
conversation and generation zero. The corresponding correlated directory reply
can structurally omit the source Bot capture. This is syntax only: the current
Host implementation requires an actual scoped target Bot before admission.
No parser or successful decryption bypasses Host state or consent checks.

### Encryption and signatures

The implementation reuses the existing `agenthandoff.PairKey`, signed pair
transcript and X25519/HKDF-SHA256/AES-256-GCM code. The pinned pair's direction
does not change for responses; the local source/target side is inferred from
the actual sending endpoint's machine ID. Both directions are tested.

Pair derivation is unchanged:

* Salt: SHA-256 of `clawdline-peer-kdf-v1\n<pair_id>\n<original source machine>\n<original target machine>`.
* HKDF info: `clawdline-peer-aes-gcm-v1\n<source X25519 public base64>\n<target X25519 public base64>`.
* Key ID: `peer-` plus the first 32 lowercase hex digits of SHA-256 of
  `clawdline-peer-key-id-v1\n<pair_id>\n<source X25519 public base64>\n<target X25519 public base64>`.

Let `H` be the canonical JSON bytes produced by `cloud.CanonicalBytes` over
the entire header, with lexicographically ordered object keys. The signature
input is exactly:

```text
clawdline-bot-network-v1\nH\nnonce\nkey_id\nciphertext
```

GCM additional authenticated data is exactly
`clawdline-bot-network-aad-v1\nH\nkey_id`. Nonce is fresh random 12 bytes;
ciphertext includes the 16-byte GCM tag. Nonce, ciphertext, signature and public
keys use canonical padded standard base64. Ed25519 signatures are 64 bytes.
Plaintext is nonempty valid UTF-8, at most the existing 512 KiB peer body budget.
Decoded ciphertext is at most that budget plus the GCM tag. The local HTTP body
limit reuses twice that peer budget to account for base64/framing. Neither the
Session signature domain nor the Session discovery domain can open a Bot frame.

### Dedicated source consent and receipts

`botnetwork.Grant` is a separate durable source record containing ID, account,
epoch, actual source and target Bot identities, the complete signed machine pair
transcript, locally compared peer fingerprint, expiry and revocation. It is not
an existing Session grant. Local provisioning is a management port outside
network dispatch and all native Bot tools. Fresh checks are required at source
send, target admission, native effect, result publication and source read.

Both endpoint conversation IDs are immutable grant identity, independently of
native generation changes. Deleting/recreating the same Bot ID with a different
conversation cannot inherit an old grant, and increasing its epoch cannot move
that grant to the replacement conversation. The source authorizer and durable
grant update both enforce this distinction.

`Store.PutGrant` verifies the complete signed pair transcript against this local
machine and its locally compared peer fingerprint before storing it. The source
reads retained grants and checks current local revocation/epoch/expiry at the
boundaries above. The controlled fixture provisions both local source stores
with synthetic independent Bot consent and increments the target epoch when
revoking. This is not a live private-account authorization call. Production
composition must additionally implement the fresh private paired-account checks
specified below and propagate revocation into the source's durable authority;
the local store does not contact or impersonate that unbuilt service.

The source must also establish current feature/source write authority, canonical
Project membership, stored Bot identity, conversation capture, nonhidden state,
current offer switch and native permission policy. Deleted/forgotten/stale targets
fail through the actual source store/Host. Peer text is untrusted peer work and
cannot become a human message, persistent-memory approval or remote shell.

The durable receipt actor is JSON of account, source machine/Project/Bot and
target machine/Project/Bot. Request ID is the durable key; digest is SHA-256 of
the entire canonical immutable header. Captured generations and consent epochs
stay in that digest, while the actor stays stable, so changed-content or changed-
capture reuse conflicts instead of executing again. A claim is stored before
native submission. An orphaned claim remains unknown and exact replay returns it
without another effect. Retained receipts, including terminal/unknown records,
consume the registered network budget. Source failures never justify automatic
effect replay.

Receipt states distinguish `unknown`, `refused`, `accepted`,
`source_executing`, `reply_stored`, and `failed`. `read=true` means that the
source published a correlated reply through a read request. It never means an
Agent observed or acknowledged it. Relay acceptance/delivery are separate facts
that the downstream service must not collapse into source execution.

## Implemented external-assistant source boundary

The Go definitions in `bot_assistant.go` and
`internal/app/botnetwork/assistant.go` are normative. Dedicated source ingress is
`POST /v1/bot-assistant/call`, JSON `{call,body}`; `body` is a UTF-8 string
containing exact operation JSON bytes. No Authorization header, cookie, viewer
token, machine roster membership, supplied `kind=user` field or supplied
principal object establishes assistant authority. Unknown fields/trailing JSON
are refused. Unconfigured service or missing gateway trust returns HTTP 503 with
`{"error":"bot_assistant_unavailable"}`.

The complete signed `call` shape includes empty correlation/signature fields:

```json
{"version":1,"request_id":"assistant-request-1","correlation_id":"","kind":"message","target":{"account_id":"account-1","machine_id":"machine-b","project_id":"project-b","bot_id":"bot-b","conversation_id":"conversation-b","generation":0},"grant_id":"assistant-consent-1","grant_epoch":1,"key_id":"gateway-key-1","body_digest":"<SHA-256>","issued_at":1800000000,"expires_at":1800000060,"signature":"<standard base64>"}
```

Time values are Unix seconds. The source checks current time within the call
window and requires the entire call window inside the stored grant lifetime.
No additional source-side lifetime constant was added. The gateway must choose
short explicit call windows and never silently refresh or retry unknown work.

Assistant signature input is exactly `clawdline-bot-assistant-call-v1\n` followed
by canonical JSON of the complete call with `signature` replaced by the empty
string. There is no newline after that JSON. The key is a dedicated Ed25519
gateway key, independent of all human OAuth/browser signing keys. There is no
assistant encryption invented here: transport from gateway to source must use
its separately authenticated encrypted source channel, with the dedicated
signature retained end to end. The gateway must never borrow a human viewer
channel/bearer to carry an assistant call.

Source policy stores account, machine, CAS version, independent global human
opt-in, gateway key ID and pinned gateway public key. A separately retained
grant stores ID/epoch, key ID/public key, stable authenticated principal ID,
account/machine/Project, explicit Bot-ID allowlist, independent list/message/read
permissions, revocation and lifetime. Policy and grant key pins must match.
Absent policy/key/grant is unavailable, not an empty allowed scope. Saving a
Bot preference changes none of these records. `PutAssistantPolicy` and
`PutAssistantGrant` are local management ports absent from assistant dispatch and
native tools; production management composition must authenticate the human.
They enforce CAS and immutable grant principal/Project identity. Revocation
retains a newer epoch rather than deleting replay evidence.

The verifier returns an opaque `VerifiedAssistant` derived only from that
signature and source-owned grant. Dispatcher and durable receipt store use this
principal; no fictional Bot endpoint or Session is minted for an assistant.
The Host supplies current feature/write/current-target/reachable-switch facts.
An assistant must pass global opt-in and the target Bot's
`reachable_by_assistants` switch; the peer offer switch alone is insufficient.

Allowed tools are `directory`, `message`, `read`. No shell, arbitrary native tool,
memory write, setting update, grant creation, opt-in update, Session handoff or
assistant-origin `reply` tool exists. Directory results are filtered again by
verified account, machine, Project and explicit Bot allowlist before publication.
Message text uses the collaboration request limit. Read must correlate the exact
stored principal/grant epoch/target capture; an unrelated principal or delayed
old capture cannot read its reply. The assistant receipt lane additionally binds
authenticated principal ID and grant ID. A valid signature does not skip a
fresh source check before effect/publication.

Assistant grant rows and retained receipt rows each reuse the existing 64-row
collaboration namespace budget, scoped to account/machine. Bot allowlists reuse
the existing page-row bound. `AssistantUsage` reads actual SQLite occupancy;
storage errors are unknown, never fabricated zero measurements. All synthetic
authority in tests is independently generated and installed only in isolated
state. No real account, key, active consent or external model request is used.

## Required private Cloud API contract

These endpoints do not exist in the inspected private service and are required
downstream additions. Keep existing `/v1/peer/*` Session shapes closed. Do not
add Bot endpoints to their Session parser or repin historical contracts merely
to carry ordinary encrypted Bot management operations.

1. `POST /v1/bot-network/authorize`: authenticated machine principal only;
   body `{header}` with the exact BotHeader above. Caller must be a named endpoint
   machine. The server derives account from machine authentication, requires both
   endpoint machines in that same active account, an active directed signed pair,
   an independent current Bot consent ID/epoch scoped to the exact stable Bot
   identities, current Bot capability on both sources, and nonrevoked/unexpired
   policy. It returns only `{authorized:true,account_id,pair_id,authority_id,authority_epoch,expires_at}`.
   `expires_at` is Unix seconds. Typed denials return `{authorized:false,reason}`
   and no authority object. A roster row is insufficient.
2. `POST /internal/v1/bot-network-authorize/:account`: private service
   authentication only; same body/decision as above. Relay supplies its
   authenticated account context; body account claims cannot select another
   account. This is fresh authorization for admission/publication, not a cached
   permanent grant. Unknown API results refuse/retain unknown effects.
3. Human management must establish/revoke Bot consent and assistant global
   consent through its actual authenticated management flow, with stable IDs and
   increasing epochs/versions. Bot tools and assistant OAuth principals cannot
   access this management route. The source's paired transcript/fingerprint and
local consent must also remain pinned; an API response is not a replacement
for local trust. The exact human management ingress remains Root-owned work
   and must be specified in its private implementation before production use.

Authorization failure reasons must distinguish invalid shape, wrong account,
nonparticipant, pair inactive/revoked/expired, consent missing/revoked/stale,
capability unavailable, source offline/unavailable and transient unknown state.
Existing source `pa_*` errors remain typed source failures; do not convert them
into success or assert a relay delivery from a failed request.

## Required Relay framing and assistant gateway

Add separate closed frame kinds, not a permissive extension of Session frames:

* Machine outbound `bot_network_publish`: fields `type`, `header`, `ciphertext`,
  `nonce`, `key_id`, `signature`. The five non-type fields map exactly to the
  source BotEnvelope. Relay authenticates the sending machine and requires its
  identity equal to header source, checks canonical base64/size/signature against
  the current registered machine signing key, and obtains the fresh dedicated
  Bot authorization decision above. It forwards opaque encrypted bytes only to
  the named target source in the same account as `bot_network_envelope` with the
  same other fields. Responses are independent reverse-direction Bot frames.
* Preserve transport facts as correlated relay accepted/delivered receipts using
  original request ID, header digest and sender/recipient machine identities.
  They must never claim native execution, stored reply or Agent observation.
* Relay must validate before decoding excessive base64: reuse the 512 KiB
  plaintext plus 16-byte ciphertext bound, canonical 12-byte nonce, 64-byte
  signature and existing segment limits. Closed version handling must refuse an
  unsupported version before forwarding. Bound pending frames and durable reads;
  do not amplify unknown source effects with retry loops.

The dedicated external-assistant gateway is a new OAuth/MCP service, not the
private service's existing human viewer login. It must authenticate the actual
external assistant client and account subject, derive a stable principal ID,
obtain explicit human account/source/Bot/tool consent, issue independently scoped
gateway signatures, enforce revocation and epoch checks, and use a genuine
assistant-specific authenticated source channel. Map its MCP tools to the exact
`directory`, `message`, `read` call kinds and retain original request IDs for
explicit recovery. No assistant may toggle global opt-in or broaden its scopes.

The gateway's source channel carries the unchanged `{call,body}` object. It
must authenticate the source machine and keep results scoped to the captured
assistant subject and account. Gateway/Relay error handling must distinguish
not accepted, source accepted, native executed, result stored and result read.
A broken connection after acceptance is unknown; it is not permission to mint a
new request ID. Source evidence alone does not prove that OAuth, MCP, private
authorization, production routing or account consent UI is deployed.

Old peers do not advertise this capability and receive typed unsupported or
unavailable, never a synthetic Session route or enabled working switch. Existing
signed Session peer traffic and all historical Session golden vectors remain
unchanged. The public Release's existing Bots visibility policy is unchanged.

## Verification entry points and limits

`TestNetworkOwnedACPRealHTTPStoresAndCorrelatedReply` and
`TestNetworkOwnedACPReversePairRealHTTPStoresAndCorrelatedReply` also cross the
actual Host, SQLite, HTTP and native ACP path with both original pairing
orientations. Signer selection follows the authenticated sender machine, not
which machine originally offered the pair. Each path retains one target effect.

`TestBotWireUsesDedicatedDomainAndBothPairDirections`,
`TestBotWireBindsEveryRoutingAndAuthorityField` and
`TestBotWireReceiptLaneSurvivesCapturedAuthorityChanges` cover encryption,
domain separation and immutable correlation. `TestAssistantPrincipalRequiresDedicatedPinnedSignatureAndCurrentConsent`
and `TestAssistantSignatureCannotAuthorizeOtherToolsOrReplyCaptures` cover
dedicated signature/scope/time/epoch admission. Adapter tests
`TestAssistantHTTPUsesStoredDedicatedAuthorityAndDurableCorrelatedReceipts`,
`TestAssistantHTTPRevocationBeforeEffectAndPublicationWithholdsReply` and
`TestAssistantHTTPUnavailableAndHumanPolicyVersionAreIndependent` exercise real
loopback HTTP and SQLite, fixed-operation replay, scoped reads/directory,
revocation before effect and publication, policy CAS and unavailable defaults.
Those adapter tests use a controlled Host fixture. Separately,
`TestAssistantSignedHTTPActualNativeACPAndStoredReply` crosses the actual
dedicated signed HTTP handler, AssistantService, source NetworkHost, SQLite and
an owned native ACP process. It retains the actual external principal and exact
whitespace-containing signed body bytes in `NetworkWork.Assistant`, proves
fixed-operation replay starts one native turn, and reads a stored correlated
reply. The normal sandbox produced unknown native startup/cleanup; that refusal
is retained in `assistant-native-source-refusal.log`. The permission-enabled
owned harness passed in `assistant-native-source-escalated.log` with one native
execution and one retained receipt. It does not prove production gateway/OAuth,
private account authorization or deployed Cloud routing. There are zero
external provider/model/API requests here.

## Native route discovery and source composition

The execution-owned `network_bot` tool first accepts `kind: routes` without any
model-supplied requester identity. The Host derives the running Bot endpoint
from its captured fence. The service reads that exact source's retained grants,
rechecks each current grant, and sends a signed encrypted `directory` request
to its configured transport. Only currently admitted remote directory entries
become returned `{authority_id,target}` routes. This local tool operation is not
a fifth wire kind, does not fabricate a Session, and does not expose a model-
chosen URL. Subsequent directory/message/read calls use those returned captures.

The native source test discovers its target through this actual path; no target
environment value is read by the requester. Disabled, hidden, deleted, stale or
revoked targets fail their independent receiving checks. A directory failure is
omitted from the available route list rather than presented as a live Bot.

Composition can allocate a `*projectassistanthost.NetworkHost` first, construct
the peer and assistant services with that pointer, construct the Host with those
services in Options, assign its Host pointer, and only then enable the Host.
For assistant-only composition, supply NetworkAccount explicitly. Supply the
independent current NetworkCapabilities callback only after infrastructure trust
and readiness have actually been established. No normal daemon startup performs
this configuration in this source-only construction.

Source capability management, global assistant policy and dedicated grant writes
remain separate local management ports. Their callers must authenticate the
human manager. Neither raw principal transport nor native tools exposes these
management writes. The normal source remains unavailable until Root wires its
real configured/private service composition.

## Required live authorization port and ordered receipts

`botnetwork.Options.Authority` is mandatory and implements
`CheckBotAuthority(context.Context, agenthandoff.BotHeader) error`.
`Service.New` refuses a missing authorizer. Every source/target admission,
native effect and publication passes the exact immutable header through that
port as well as the local retained grant checks. A receiving encrypted request
first verifies the pinned source signature and GCM tag before invoking this
possibly remote authorization port. The isolated tests explicitly use
`Store.CheckBotAuthority`, which reads actual locally provisioned consent;
that controlled local-account stand-in does not implement production Cloud
account membership, live pair status or the private authorization endpoints.
Root must supply the real private API adapter at this port in production.

Both receipt stores publish with one atomic SQLite UPDATE. A stored terminal
reply/failed/refused outcome, or an unknown outcome with failure evidence,
cannot be overwritten by an earlier read that completed later. Executing does
not regress to accepted. The transport read/publication fact merges by logical
OR and never becomes an Agent-observed or acknowledged assertion. The actual
SQLite regression first reproduced stale-read overwrites, then passed after
the atomic guard. No unknown native effect is automatically retried.

## Directory bootstrap and captures

The crypto grammar reserves a scope-only directory endpoint, but the implemented
scoped grant/Host dispatcher currently requires a complete actual target capture
for every remote directory call. The native `routes` tool obtains these from
its durable, conversation-pinned grants and probes the receiver; it does not
perform unrestricted Project-wide discovery. Private clients must preserve the
returned Bot and conversation fields rather than using the reserved empty form.

A dedicated assistant gateway likewise needs actual target captures before its
first call. In the pending authenticated human consent flow, management obtains
the selected Bot's actual source fence (the existing authenticated settings
response includes it) and sends `target_hints: BotEndpoint[]` alongside the
requested machine/Project/Bot scope. These hints are routing data, never
principal or grant authority. The gateway independently derives OAuth principal,
account and allowed Bot scopes from its real consent records, then signs only
matching target calls. Source verification still checks stored identity, current
Project/conversation/generation and its independently installed dedicated grant.
Missing/stale hints must produce a typed unavailable/stale result and require an
authenticated management/source refresh; the gateway may not invent generation
or use a human viewer bearer for a Bot call. Implementing that consent/hint
ingress and its refresh is part of the explicitly pending private gateway unit.
