# Clawdline Cloud, from this repository's side

The Mac app and the web console in this repository are one half of Clawdline Cloud. The other
half — the control plane at `api.clawdline.com`, the relay at `relay.clawdline.com`, and the
Terraform behind them — lives in a separate private repository and is deployed by its own
runbook. This page is about the half that ships here: what is wired, what a person can actually
do with it, how the hosted console is built, and what has never been run against a real account.

The protocol itself is not restated here. `PROTOCOL.md` in the cloud repository is the
authority for the envelope, the channels, the command-authentication rule and the pinned wire
shapes; where this page and that one disagree, that one is right and this one is a bug.

## The shape, in one paragraph

The Mac holds the account's **content key** (a 32-byte master secret in its Keychain) and an
Ed25519 **device key**. It publishes full session and orchestrator snapshots as sealed envelopes
on `s/<machine>/<session>` and `orch/<machine>`, and accepts commands on `ctl/<machine>` only
from a device whose public key it has pinned locally. A browser viewer holds the same content
key — obtained once, by pairing — plus its own non-extractable Ed25519 key. The cloud carries
ciphertext and routing metadata and can read neither the snapshots nor the commands.

On macOS those secrets currently live in the traditional login Keychain and are guarded by its
code-signing ACL. The store deliberately sets neither `kSecUseDataProtectionKeychain` nor
`kSecAttrAccessible`: local builds have no application-identifier/keychain-access-group
entitlement, so the protected namespace refuses them, and Apple documents the accessibility
attribute as applying on macOS only in that namespace (or for synchronizable items). Moving to the
Data Protection Keychain is one future migration that must first give every release and local
build a compatible entitlement and signing identity; an inert accessibility label is not that
guarantee.

## The Keychain is a door that can stop answering

Every `SecItem…` call is synchronous, and on a locked Keychain it does not return until somebody
answers a system dialog. That is a length no screen may wait for, so the store refuses to be
called anywhere it could freeze one, and the app never waits on it without a bound.

**Both directions are refused on the main thread, and none may open authentication UI.** `CloudKeychainStore` throws
`mainThreadReadForbidden` from `data(for:)` and `mainThreadWriteForbidden` from `set(_:for:)` and
`remove(_:)`, *before* it reaches Security. Reads were guarded first; writes are the same door
and had been left open. The refusal is a `Thread.isMainThread` test rather than a queue test on
purpose: a blocked main thread is what freezes AppKit, and it is the thread, not the queue, that
the Security call parks. Every production copy/update/add/delete dictionary also carries
`kSecUseAuthenticationUIFail`; a locked item is therefore a typed failure, never a system dialog
whose lifetime the app cannot bound.

**Everything else goes through one of two adapters.** `CloudKeychainReader` answers a read on its
own serial queue and returns the result to the main queue. `CloudKeychainWriter` does the same for
a mutation. Both are bounded and cancellable adapters. Callback-based identity restoration treats
timeout as observable **progress** and retains the eventual terminal result for reconciliation. A
retrying mutation likewise does not silence the first operation: either terminal deletion success
proves the shared credential is gone. One-shot pairing reads instead finish their await on timeout
or cancellation and detach the late answer, so closing the QR sheet or a stalled load-or-create
returns to Settings. In either spelling cancellation stops delivery only—never the synchronous
Security call, which has no cancellation and may still land.

**A cancelled sign-in cannot leave a credential behind.** Cancelling a `Task` is a request the
transport may decline, so a login that is cancelled or signed out of can still return a real
credential afterwards. Filtering that in the UI is too late: the write is already on its way.
`CloudAccountClient` therefore carries a `CloudCredentialGeneration`, captured when
`startDeviceLogin` opens the flow and compared **inside the persistence transaction**:

- `signOut()` and `invalidatePendingLogins()` bump the generation *before* taking the store's
  coordinator, so a poll blocked on that lock sees the new value whichever order the two arrive in;
- the pre-write check refuses the credential outright, so an abandoned secret is never written
  rather than written and deleted;
- the post-write check runs again after `SecItemAdd` returns and tries to remove what it just
  wrote, because sign-out can arrive inside that window and would otherwise find nothing to remove;
- every credential embeds its nonsecret validity epoch, while the minimum valid epoch is stored
  durably outside the Keychain. Cleanup failure can therefore leave stale bytes, but a new process
  still rejects them. Invalidation failure and stale-byte cleanup have separate visible retry
  owners; neither is suppressed by `Task` cancellation.

Sign-out is consequently a sequence of phases rather than a synchronous call. Synchronous
reservation first raises the process-local admission floor, so the bridge detaches immediately;
a timeout is shown as unknown/reconciling without reattaching it. A late deletion success moves
the model and store to signed out together. When durable invalidation succeeds but
physical deletion fails, the bridge stays detached and Settings owns an explicit stale-item cleanup
retry. Cancellation persistence is likewise observable, bounded and retryable.

## What is wired on the Mac

`CloudAccount`, `CloudKeys`, `CloudTransport`, `CloudEnvelope` and `CloudAppBridge` were all
here before this change and none of them had a caller: `CloudAppBridge` was never constructed,
and `RemoteServer.attachCloudBridge` was never called, so a Mac signed in to Clawdline Cloud
published nothing and accepted nothing. The missing wire is `Sources/CloudBridgeLifecycle.swift`.

**One bridge, owned in one place.** `CloudBridgeLifecycle.shared.apply()` runs at launch, on
every config change, and whenever the Cloud settings card signs this Mac in or out
(`CloudSettingsModel.onConnectionChange`). It is idempotent by design: applying it again with
the same account and machine leaves a live bridge alone rather than dropping the socket,
re-handshaking and republishing every snapshot. Only a changed identity replaces the bridge, and
signing out detaches it.

**A refusal is not an outage.** `CloudTransport` reconnects forever with capped backoff and
reports nothing after the first `connect()`, which is right for a flaky network and wrong for a
revoked machine — that one would knock on the relay every thirty seconds for as long as the app
is open. `POST /v1/tokens/device` answers `403 revoked` for exactly that case, so the token fetch
is where the two are told apart: `CloudAPIDeviceTokenProvider` now raises
`CloudTransportError.unauthorized` for 401 and 403, `CloudSupervisedDeviceTokenProvider` reports
it to the lifecycle, and the lifecycle brings the bridge down and leaves it down until the
identity changes or somebody presses retry.

**One durable spool owns outbound order.** Both ends refuse an envelope whose sequence did not
advance. `CloudOutboundSpool` therefore reserves one never-reused global sequence across channels,
persists the logical record, seals the exact publish-frame bytes, and persists `sent` before the
socket write. `CloudTransport` has no `pendingByChannel`, queue, or sequence allocator: while it is
not ready it returns `notConnected`, and the spool retains the only retryable bytes. Reconnect may
resend the exact in-window sent frame; restart burns sent uncertainty, and only a correlated
authenticated channel/sequence ack or refusal settles a live row. Unreadable or unsafe state never
restarts from zero. On the first Mac open, the former `cloud-sequence.json` is descriptor-checked
and its sender ceiling is durably applied as the spool's minimum next sequence before any publish;
the same schema-compatible file remains a live rollback fence. Before the new spool commits a
reservation at an old-image block boundary, it first raises the predecessor's ceiling. Rollback may
skip a block but cannot reuse a sequence already returned by the new image. Missing legacy state
means no prior floor, while invalid legacy state fails startup and is preserved/quarantined rather
than being read as zero.

**Commands go through the door they already went through.** `RemoteServerCloudCommandRouter`
converts a verified cloud command back into an in-process request, so authentication,
idempotency, menu safety, image validation and audit stay the local HTTP route's single
implementation. The write gate is the same one: a Mac with `remote_write` off refuses a cloud
command with `cloud_commands_disabled`, exactly as it refuses one from the browser on its own
network. If the command carried a safe bounded request identity, the Mac also publishes that 403
on the existing `action:<request>` channel; an identifiable malformed `shell-kill` does the same
with `400 malformed_command`. Neither refusal reaches the command router. A non-`ctl` envelope is
outside this reply contract and is rejected without minting an action-channel identity; the shipped
browser sends `shell-kill` only as `ctl`.

Before that router can cross the effect point, `CloudCommandLedger` durably reserves the
authenticated `(viewer sender, request id, request digest)` identity. Exact duplicates wait or
replay the durable normalized outcome; a digest mismatch fails closed. The effect begins only after
the in-progress transition is durable, and its normalized status/body is durable before any reply
is offered to the spool. A restart drops never-started reservations and treats recovered
in-progress work as explicitly unknown. Store-originated persist/fsync/rename/recovery failures
propagate through admission and startup; no convenience path catches them into an empty ledger.
The Mac calibrates its real epoch guard only from authenticated server time and re-reads epoch,
paired-device roster, and (for writes) `remoteWrite` after durable reservation at the effect point.
A revocation there durably releases the reservation and performs no effect. Live in-progress
duplicates join the current owner; store failures terminate all waiters with either a retry after
durable release or a typed uncertainty once an effect has started.

Durable outbound logical rows contain channel/identity, byte count, and a SHA-256 content identity,
not plaintext or a base64-equivalent payload copy; content exists only inside the encrypted sealed
frame. Production revalidates complete logical metadata and the sealed frame on load. Settlement
matches the exact authenticated wire channel plus sequence and creates its tombstone in the same
commit. A cancellable deadline wake releases a lost-ACK head without waiting for new traffic and
reschedules with bounded backoff after a transient durable-store failure;
receipt ingestion keeps at most 256 oldest observations and counts overflow. Authority directories
and files must be private (0700/0600), newly created directory entries are parent-fsynced, new
writes use one fixed candidate name, and startup scans at most 128 directory entries before either
descriptor-validating/removing at most 64 legacy `.writing-*` candidates or failing closed. A
validated server machine identity is injectively encoded rather than used
as a raw path. Linux has no accepted runtime metric label, so it suppresses spool metrics instead
of publishing `runtime=mac`.

**Authenticated command ingress is bounded before execution.** `CloudInboundCommandQueue` is the
single owner of the pending FIFO and its accounting. Production limits are eight retained commands
and 32 MiB of charged variable-width bytes; admission also enforces a 16 MiB post-decrypt plaintext
ceiling matching the Relay content budget. A charge is exactly `plaintext.count +
channel.utf8.count + sender.utf8.count`, not sealed frame bytes or process RSS. Its snapshot reports
all three configured limits, current and peak count and charge, admitted and delivered totals,
invalid drops, per-reason refusals, refused charged bytes and oldest pending wait. These are source
budgets and runtime counters for W6 measurement, not a claim that the values are load-tested.

The receive path authenticates and decrypts first, checks replay next, and advances the replay
cursor only after this owner admits the command. Count, aggregate charged-byte and single-command
overflow return `cloud_ingress_busy` (HTTP-equivalent status 429) through the existing encrypted
request-scoped command or read answer only when the plaintext has the same safe bounded identity
the bridge normally accepts. The refusal path applies the exact machine channel and remote-write
gates first, returning `wrong_machine` or `cloud_commands_disabled` instead of an impossible retry
instruction. No safe identity means no invented reply channel. Capacity refusal is terminal for
that authenticated sequence and does not fence a later sequence; after capacity drains, a new
request can be admitted. The existing browser and Relay do not retry the same envelope bytes, and
R-1 does not add such a protocol. Thus a refusal is not an admission, an admission is not execution,
encrypted publication is not relay ACK, and none of them asserts human observation.

The command FIFO survives WebSocket reconnect and token rotation for the life of the transport; it
is never coalesced with snapshots and an admitted row is never evicted for a newer command. The
bridge's deliberately serial consumer may suspend in routing while receive, ready-generation and
outbound publication actor turns remain independently schedulable. Refusal answers—including
preflight 403 and read-lane busy answers—enter one serial lane capped at eight outstanding items.
Offering never waits for publication; each item has a one-second deadline, and current/peak,
admitted, completed, timed-out, full-drop and cancellation totals expose its debt. A timed-out
publication is cancelled and remains charged until its task exits, so repeated stalls cannot create
unbounded work or block receive-loop ping handling. Shutdown is an explicit lifecycle boundary:
admission then returns typed `finished` without advancing replay or accepted metrics. W5 still owns
pairing/key rotation and external Cloud/GCE acceptance; W5-1 now owns the Mac/Ubuntu durable
command and outbound composition described above.

The public W0-E contract bytes remain a candidate pinned to commit
`38eb822575e3c309a776a9e3e2874c7062d8fb75`, package tree
`3ee391a4af73f9688510c19106d38ba325227051`, source SHA-256
`47c21a3d096813f940026943004791087ea89d628f6123942dd59c02f71a2f3d`, and package SHA-256
`6ccccea5f05b603fd9a583940f6f7a3fd5a8735ac6d5ce6ef7246620b59b7d6f`.
`authority=candidate` and `cutover_required=true` are executable gates: W5-1 does not perform
cutover, raise a floor, or emit an unaccepted candidate version. Existing accepted v1 envelopes
remain the production wire until a later authority decision.

**The `orch/` snapshot carries three things, and two of them were added because their absence
was invisible.** `RemoteServer.orchestratorSnapshot()` is the one body both publishers send — the
local `orchestrator` event and the cloud envelope — and it holds `tasks`, `schedules` and `app`.
`schedules` is there rather than behind a request because the viewer reads that list on a
one-minute lane and a request is a person waiting; measured on one Mac it is 453 bytes beside
1,056,958 for the task list next to it. `app` is `version`, `build` and `protocol` out of
`/v1/health` — nothing about permissions — and it is the only reading the "this page is behind"
banner can have out here, because on the direct path that comparison comes from asking health on
every reconnect and the relay's `ready` frame is the *relay's* and has never heard of a build.

**An empty list and no list are different answers.** `CloudClient.schedules()` resolves an empty
inventory and refuses an unknown one — `cloud_read_unavailable` before any snapshot has arrived,
`cloud_schedules_unpublished` for a Mac whose build predates the field — because
`net/schedules.js` draws whatever it is handed, so resolving `[]` would be the page asserting on
the Mac's behalf that there is nothing scheduled. A refusal draws nothing and keeps the last
truthful list; a real empty answer still draws.

**Who may drive this Mac is a local fact.** `CloudPairedDeviceStore` holds the pinned viewer keys
in `~/.config/clawdline/cloud-devices.json`, owner-readable only, scoped to one account.
`CloudLifecycleKeyProvider` reads it on **every** inbound command rather than caching it at
attach time, which is what makes unpinning a viewer take effect without restarting the app.

## What is wired in the browser

`Resources/web/app/js/net/cloud-client.js` and `cloud-crypto.js` were also already here and also
had no caller outside the tests. `net/cloud-boot.js` is the boot path.

**Which transport, and why it is not a hostname check.** The page has always had two transports
and one question deciding between them (`MOCK`). The third cannot be chosen by asking whether
this is localhost, because the Mac serves the same page through a Cloudflare tunnel on a hostname
that is not localhost and which must keep talking to the Mac. So a cloud console is a
**build-time declaration**: `tools/build-web-app.py` fills the `<!-- clawdline:cloud -->` slot in
`index.html` with the origins that build is for, using the same slot mechanism the Mac already
uses for the string table and the module preloads. A copy served by anything else keeps the
comment and keeps its old behaviour. The declaration is checked against `location.origin` before
it is believed, so a hosted bundle copied to another origin refuses rather than half-working.

**Sign-in creates a revocable device.** `POST /v1/auth/session` both registers the viewer device
and mints the cookie, so the Ed25519 key pair is generated first and its public half is sent with
the registration. That ordering is what makes the session revocable per device: revoking the
`web_devices` row invalidates the cookie that names it. The private key is non-extractable and
lives in IndexedDB; the page can sign with it and cannot read it. A cookie naming a device whose
key this browser no longer holds is treated as not this browser's device, and a fresh one is
registered rather than pretending.

**Sign-in is an explicit gate.** A signed-out hosted console stays on a Clawdline-owned explanation
with a **Continue with GitHub** button. The `sign_in` boot state never navigates on its own; only the
button starts the top-level OAuth round trip. That keeps the account boundary visible before the
PWA leaves for GitHub.

**A full viewer tier has a recovery door, not a retry loop.** `409 device_limit_reached` carries the
ordinary tier and exact limit and is terminal for connection backoff. While the short-lived login
ticket from that fresh OAuth round trip remains valid, the PWA can read a recovery-only list of
active viewer name, kind, creation time and last-seen time, choose one recognizable device, and
revoke exactly that row. No public key, capability or account identity is rendered. The mutation
requires the PWA Origin; the API bumps revocation state and audits the fresh-login recovery, so the
old device's cookie and relay access retain the same revocation guarantees. The ticket then retries
ordinary session creation through a per-account allocation fence—there is no concurrent extra slot
and no tier change. Viewer kind/name is a coarse platform label rather than a transmitted user-agent,
and authenticated boot refreshes last-seen time. If the recovery ticket expires, the screen returns
to the explicit GitHub button instead of retrying a guaranteed 401. When session creation succeeds
without an account key, boot continues to `pairing_required`, and an installed iPhone gets the QR
scanner.

Other terminal 4xx session conflicts also stop and show an explicit retry action. Network and 5xx
failures remain retryable, but the Cloud door names the failure and countdown instead of leaving the
session list's connecting skeleton as the only visible state.

**Capabilities are read back, not assumed.** `GET /v1/devices` is consulted every boot, which is
also where a revoked device finds out. Writes are enabled only if the row still carries
`send_prompt`.

**And `send_prompt` gates one read as well, which is the relay's rule rather than this app's.**
PROTOCOL §12 says publishing to `ctl/` needs that capability *in either class*, and a read has to
ask on `ctl/` because it is the only channel a viewer may publish on at all. So a device
downgraded to read-only can see every session row and cannot ask for the messages inside one. It
is told `cloud_read_needs_send_prompt` rather than left waiting; widening it is a relay decision.

**Pairing.** Covered in its own section below.

**Reconnect.** `keepConnected` re-acquires the device token and restarts the socket with
exponential backoff and jitter, and treats a refusal as terminal for the same reason the Mac
does. Outbound sequences use the same reserve-ahead discipline as the Mac, in `localStorage`.

## Pairing and executor identity

The normative W5-2 wire is the four-phase identity protocol. The machine starts it with
`POST /v1/pairing/identity/start`; phase writers use
`POST /v1/pairing/identity/phases/:phase`; readers poll
`POST /v1/pairing/identity/poll`. Viewer writes `offer` and `activate`; machine writes `grant` and
`confirm`, and each side reads the other side's phases. A phase write is exactly the canonical
two-member `{claim_nonce,blob}` object produced by `CloudPairing.encodePhaseWriteBody`. An exact
retry is `duplicate`; a different body for the same phase is refused. The machine pins the viewer
only after it observes the `confirm` receipt, so a lost grant/confirm response is replayable without
granting command authority early.

`CloudAccountClient` implements those exact route identities and strict response shapes.
`CloudIdentityPairingMachineHandover` connects them to the protected
`CloudExecutorIdentityAuthority`: start publishes the current machine signing identity and content
epoch; offer is bound to the QR's account/machine/nonces; the exact grant is durable before upload;
activate must acknowledge that grant digest; confirm acknowledges activate and is byte-stable on
retry. `CloudIdentityPairingWireContract.canonicalVector` is the exact 843-byte route-role vector
also pinned by the private API/Relay; its SHA-256 is
`2f79369d4ee866976c6da2a41358c1aab5321ab0e6054bf8a3afe16e215f69a9`.
`canonicalLifecycleVector` separately pins the exact body members, duplicate replay rule and
`pin_after=confirm_receipt`; its SHA-256 is
`79504ce608fd278cecdb2e26f62d6d1c7e915ef66f94a79cc12ff240a95e410e`.

Machine signing-key rotation is `POST /v1/machines/:id/identity/rotate` with exact canonical
`expected_key_epoch`, `public_key`, and `fingerprint` members. Viewer and machine revocation retain
the browser-session DELETE routes. Rotation/revocation receipts advance the identity/key,
capability, content-key, and JWKS transition windows enforced by the control plane and relay; the
public protected authority advances its matching generation/key/revocation state. Every initial
socket and reconnect re-reads the protected binding and calls `verifyReconnect` only after the
W5-1 ledger and spool opened. Stale account, machine/device, identity generation, key epoch, or
revocation epoch therefore blocks before the challenge is signed.

The former `start`/`complete`/`claim` single-blob methods remain source compatibility for an older
console finishing an already-started handover. They are not the normative production identity
contract and do not authorize a cutover or a reader-floor change. W0-E remains
`authority=candidate`, `cutover_required=true`; this source correction is not live Relay/GCE or
release evidence.

## Building and deploying the hosted console

```sh
tools/build-web-app.py --out dist/app-console
```

No CI, no network, no GitHub Actions. It prints a file count, the stamp, and the SHA-256 of its
own `SHA256SUMS`, and two runs of the same tree produce byte-identical output — which is the only
reason "is what I am about to upload the reviewed tree?" has an answer, since nobody keeps the
build log of a manual upload. `Tests/web-app-build.mjs` builds twice and compares every byte.

What it does is the static half of what `RemoteServer.page` does per request: stamps every
`/app/` URL with a content hash so those assets can be `immutable`, fills the module-preload and
cloud slots, drops the twenty `apple-touch-startup-image` links the Mac draws on demand, writes
`manifest.webmanifest` with icons copied byte-for-byte out of `Resources/Clawdline.icns`, and
writes `_headers` with a CSP whose inline-script hashes are computed from the exact scripts it
emitted — no `'unsafe-inline'`, and `connect-src` limited to the declared API and relay. The
bundled MIT-licensed QR decoder and worker are content-stamped with the rest of the app; its
license is shipped beside it.

Pages also carries a generated Traditional Chinese string catalog under the same immutable stamp.
`tools/export-hosted-strings.py` exports it from the Mac's existing `RemotePage.strings` response,
so the words remain authored in `Copy+Chinese.swift`; `navigator.languages` selects it for
`zh-Hant`, Taiwan, Hong Kong and Macau, while the English already in the document remains the
fallback. A string change participates in the build stamp exactly like a JavaScript change.

Upload `dist/app-console` as the Pages deployment for `app.clawdline.com` and follow §4 of the
cloud repository's `RUNBOOK-DEPLOY.md` for the DNS cutover. Deploy is owned by the operator, not
by anything in this repository.

## What has never been run against a real account

Everything below is proved against fakes, fixtures and cross-runtime vectors in this repository's
suite, and **nothing below has been run against `api.clawdline.com` or `relay.clawdline.com`**.
The cloud repository's `RUNBOOK-DEPLOY.md` §8 is the acceptance smoke, and until it has been run
with these clients, these are exactly the claims that rest on reading rather than on measurement:

- A real device-code login, a real machine registration, and a real five-minute device token.
- A real WSS handshake against the relay: the DO-issued challenge, the Ed25519 signature over
  `context|account|device|challenge`, and the `ready` frame.
- A real GitHub OAuth round trip and the login-ticket cookie that carries "who just signed in"
  from the callback into `POST /v1/auth/session`.
- A real pairing: `start`, the human carrying the fragment, `complete`, `claim`, and the record
  being destroyed after one claim.
- A published snapshot arriving at a viewer, and an allowed control arriving at the Mac.
- The usage flush and the metering counters that follow from any of the above.

## Signing a local build, and why it is a Keychain question

The Cloud secrets are guarded by the login Keychain's code-signing ACL, so the identity a local
build signs with decides whether macOS re-asks for those items after every rebuild.

**Two things in that ACL behave differently, and only one of them is stable.** The trusted-application
entry binds to the *designated requirement* — for a certificate-signed build, "signed by this
certificate with this bundle id" — which a rebuild satisfies unchanged. The
`ACLAuthorizationPartitionID` entry does not: macOS keys it to `teamid:<id>` when the signature
carries a Team ID, and falls back to `cdhash:<hash>` when it does not. A cdhash is the hash of the
binary, so it is new on every build. That is the whole of why "Always Allow" never sticks: the
requirement still matches, and the partition the key is asking about is one this build has never
been in.

A self-signed certificate cannot carry a Team ID; only Apple issues one. So `./build.sh` no longer
looks for a single name — it works down a preference order, and says which layer answered:

| what `security find-identity -v -p codesigning ~/Library/Keychains/login.keychain-db` holds (or whatever `CLAWDLINE_LOCAL_SIGN_KEYCHAIN` names, if it is set) | what `./build.sh` does |
|---|---|
| exactly one `Developer ID Application: …` | signs with it, and says why: it carries a Team ID, so macOS keys the items to that team rather than to this build — one authorisation should then reach the next rebuild, which is the one claim here still waiting on the acceptance below |
| two or more of them | prints both names, refuses to choose by Keychain order, and falls through to the row below |
| exactly one `Clawdline Local Development` | signs with it, and says what it costs: no Team ID, so macOS asks again after every rebuild |
| two or more with that name | fails; it will not choose by Keychain order |
| whichever was chosen, **Keychain locked or not answering** | **fails, naming both repairs and the identity it would have used** |
| neither layer, or the query fails | **fails**; ad-hoc requires `CLAWDLINE_SIGN_ADHOC=1` or `CLAWDLINE_SIGN_IDENTITY=-` |

`CLAWDLINE_SIGN_IDENTITY` and `CLAWDLINE_SIGN_ADHOC=1` sit above the whole order and are unchanged:
an explicit value is still consulted first and still wins exactly.

**Whichever layer answers, a local build is still a local build.** The chosen identity signs on the
same branch as before — `--keychain` scoped to the discovered path, `--identifier` the bundle id,
and no `--options runtime`, `--timestamp` or `--entitlements`. A development build needs no hardened
runtime, and a build that contacted Apple's timestamp server on every run would stop working on a
train. The release path (`CLAWDLINE_SIGN_IDENTITY` set explicitly) keeps all three.

**After signing, the build measures the answer instead of assuming it.** It reads `codesign -d -vv`
back off the bundle it has just written and prints one line: the identity and `TeamIdentifier=<id>`
when there is one, `TeamIdentifier=not set` and the reason the prompts will continue when there is
not, or "could not read TeamIdentifier" when the description failed or timed out. That last case
returns zero: the application is signed, and a report that could not be made is not a build that
failed.

**Changing signing identity costs one round of prompts.** The build that first uses a new identity
is a different signer to every ACL on the machine: approve `app.clawdline.cloud.keys` once for each
of the two items (`device-ed25519-v1` and `account-master-secret-v1`), and expect macOS to ask again
for the authorisations it remembers by code requirement — Automation for iTerm2 or Terminal,
Accessibility, and the machine credential in the login Keychain. That round is the price of the
change, not evidence that it did not work; the rebuild *after* it is what shows whether it did.

> **What is measured, by whom, and what rests on Apple's documentation.** Three categories, because
> a reader deciding whether to believe this needs to know which of them a line belongs to.
>
> *Re-runnable in ten seconds, by anyone, with no dialog:* `codesign -d -vvv
> ~/Applications/Clawdline.app` reports `Authority=Clawdline Local Development` and
> `TeamIdentifier=not set`, against `TeamIdentifier=H7V7XYVQ7D` and `EQHXZ8M8AV` for the Developer
> ID-signed applications beside it; a self-signed certificate whose OU is *shaped* like a team id
> still signs `TeamIdentifier=not set`, so the shape is not the mechanism; an ad-hoc signature
> prints the same `not set`. Both the delivery and its review measured these independently.
>
> *Observed during the diagnosis that opened this line, and deliberately not re-measured since:* the
> two Cloud items' `ACLAuthorizationPartitionID` held 20 `cdhash:` entries, one per build, none of
> them the installed application's. Reading a partition list needs `security dump-keychain`,
> `find-generic-password -g`, or Keychain Access — the first two open a system dialog, which is why
> neither the delivery nor the review was allowed to run them.
>
> *Measured last, because only a person could produce it:* that a Team ID makes macOS write
> `teamid:<id>` instead of another `cdhash:`, and that the build after it is not asked. On
> 2026-09-06 this Mac ran the acceptance end to end. Before: 20 `cdhash:` entries on each item, no
> team, and the app's own log repeating `cloud: Keychain identity read timed out` all evening — a
> dialog it had opened and could not cancel, because `SecItemCopyMatching` is synchronous and
> uncancellable, so the window stayed on screen after the app had stopped waiting for it. The
> Developer ID build was installed, the two items were approved once each, and the partition lists
> became `["apple-tool:", "apple-tool:", "teamid:83D62P566Q"]` and `["teamid:83D62P566Q"]`: one
> entry naming the certificate, not a twenty-first naming a build. Then a second build was made —
> `CDHash=c7271bd8…` where the approved one had been `c4d8a0ba…`, so a different binary by the only
> measure the Keychain uses — and it attached the Cloud bridge with no dialog, no password, and no
> new ACL entry. That is what the twenty approvals before it never bought.

Discovery runs as `security find-identity … "$CLAWDLINE_LOCAL_SIGN_KEYCHAIN"`; ambiguity is counted
only in that result, lock usability is read for that path by the injectable
`tools/keychain-status.swift` helper using `SecKeychainGetStatus` and `kSecUnlockStateStatus`, and
local `codesign` receives `--keychain` with the same path. The build neither reads nor changes the
user's Keychain search list. The locked row is the one that used to have no answer. `codesign` would find the key, stop on an
unlock dialog, and wait — possibly behind another window, on another Space, with the build looking
merely slow. `build.sh` now probes the Keychain first and refuses rather than waits.

**Clawdline never unlocks a Keychain and never learns its password.** The two escapes are the
person's: `security unlock-keychain`, or `CLAWDLINE_SIGN_ADHOC=1 ./build.sh`, which is the
documented ad-hoc contract — chosen rather than fallen into, consulting no identity at all. Ad-hoc
means a fresh code identity every rebuild, so macOS re-asks to authorise iTerm2 automation and the
Cloud Keychain items are re-authorised on first use.

**Nothing waits forever.** Every `security` and `codesign` call in `build.sh` and
`tools/setup-local-signing-identity.sh` runs under a watchdog (`CLAWDLINE_SIGN_QUERY_TIMEOUT`,
`CLAWDLINE_CODESIGN_TIMEOUT`), validated as positive integers before a child is launched. macOS
ships no `timeout(1)` and `/bin/bash` here is 3.2, so `wait -n` is unavailable. A marker plus a
typed side channel distinguishes watchdog timeout from a child's own exit 124. Every signing
branch, including explicit ad-hoc, preserves stderr/status and stops on nonzero. A timed-out
mutation reports state unknown and requires inspection/reconciliation; it never claims that
nothing changed or was imported.

**The partition list is the person's step, by design.** `security set-key-partition-list` is what
stops `codesign` asking for key access on every rebuild, and `man security` is explicit that it
requires the Keychain password (`-k`). Clawdline will not ask for that password, accept it in an
environment variable, or put it on an argument list. Omitting `-k` is not a documented interactive
prompt contract: `/usr/bin/security help set-key-partition-list` says the password is required.
The setup script therefore never runs this mutation. Its former `--set-partition-list` spelling
fails before discovery or mutation and points the person to Keychain Access or a manually reviewed
SecurityTool invocation.

> **What the tests prove, and what they do not.** `Tests/keychain-rebuild-focused.mjs` drives all
> of the above through fake `security` and `codesign` executables and a temporary keychain file.
> That is proof of the *script's* branching, messages and bounds. It is **not** proof against a
> real login Keychain: no test here unlocks, searches, reorders or mutates one. The lock helper is
> compiled/typechecked, while its branch tests inject a fake executable and temporary path; the
> evidence is the documented `SecKeychainGetStatus` unlock bit, not `show-keychain-info` output.

## What is deliberately not here yet

- **Other hosted locales.** Traditional Chinese is bundled from the same catalog as the Mac.
  Other non-English catalogs still fall back to the English document until they are exported and
  named in the build declaration.
- **Push is machine-scoped.** The hosted worker and subscription now use the one connected Mac's
  VAPID key through an encrypted request/reply. An account showing more than one Mac is refused
  with `cloud_machine_ambiguous` instead of registering a browser subscription against whichever
  machine happened to publish first.
- **The reads that cross.** A session's messages, both tiers of its Info, one
  background agent's conversation, one background command's output, its skills menu, its Git
  panel, its live screen and the pictures inside its transcript now cross. The viewer asks on
  `ctl/<machine>` and the Mac
  answers on `t/<machine>/<session>`, which is the channel the viewer had always subscribed to and
  nothing had ever published on. The route table, the bounds and the typed refusals are in
  [`docs/api.md`](api.md#the-reads-a-browser-on-the-cloud-path-may-ask-for).
  `cloud_read_unavailable` was the answer for the last four and is now the answer for nothing:
  when the Mac cannot serve one of them it says so in its own word — `not_found`, `not_a_repo`,
  `git_failed`, `429 busy` — which is what lets `git-panel.js` keep the branch it already had.
  A picture crosses as bytes rather than as a URL, because the console's own origin has no artifact
  route and this Mac is not reachable from it; the bound on one is the relay's 16 MiB envelope cap
  turned into 12,582,132 bytes of PNG, and a picture over it is drawn as a stated size rather than
  as the broken-image icon it used to be.
  Project and task Markdown/text documents use the same encrypted request/reply path. Their
  listing carries only explicit machine/session identity plus relative scope/task/path metadata;
  a selected document crosses as bounded base64 inside the encrypted envelope and is decoded only
  by the paired viewer. The Mac still sends the request through its existing document routes, so
  root containment, task ownership, symlink, extension and 2 MiB decisions have one authority.
  A share URL is `https://app.clawdline.com/#document=1&machine=…&session=…&scope=…&task=…&path=…`:
  the origin receives only `/`, because browsers do not send a fragment, and the fragment holds no
  credential or local root. The hosted page keeps that explicit locator while its relay is cold
  and retries after connection instead of selecting a matching bare session from the fleet. A
  Session action with the same bare id on two Macs fails as `cloud_session_ambiguous`; only one
  complete machine/session pair may enter the document page. Localhost, LAN, named-tunnel and Mock
  documents remain readable through their existing transport but cannot enable Share or Copy:
  only a real Cloud machine identity can produce the canonical `app.clawdline.com` address.
  A live screen answer preserves the Mac's actual backend, but Cloud presents a signalled tmux
  screen as `channel: "on-demand"` and asks again after one second. The direct path's `screen` SSE
  revision does not cross the relay; claiming it did would leave the first pending capture and all
  later changes waiting forever. Each capture remains a named answer delivered only to the tab
  that requested it.
  Machine-scoped Projects, place inventory and past-session reads now use one reserved transcript
  answer channel with a per-request name; opening and resuming a session returns its local route's
  body on that channel. The durable `orch/` snapshot is not reused for these one-off answers, so
  asking for Projects cannot replace the task/schedule inventory. Everything else the Web UI reads
  remains guarded by `typeof api.X === "function"` and draws no control at all.
  `CloudClient.schedules()` was the one earlier exception and is no longer a defect: the
  `orch/` snapshot now carries `schedules` beside `tasks`, and an unpublished field is refused
  rather than drawn as an empty list — the two paragraphs above **The `orch/` snapshot carries
  three things** say how, and why an empty inventory and a missing one had to be different answers.
  A row's full detail is a separate read, just as it is on the direct path; that is where the task's
  project directory comes from, after which the shared `places()` and schedule renderer supply the
  project label and mark. Create, update and delete use the same local schedule routes and return a
  named action answer instead of treating relay acceptance as completion.
- **A read an older Mac has never heard of ends in a timeout, not in a refusal.** A malformed or
  unknown read is answered to the bridge and published to nobody — before it is parsed there is
  neither a body to send nor a name to send it under — so a hosted console that knows a read the
  paired Mac does not will wait out its sixty seconds and see `cloud_read_timeout`. On a matched
  pair this is invisible, because the client never sends a read it cannot spell. It is a real
  minute across a version gap, and the thing that lets a console see the gap coming is the build
  stamp — which this path now carries, in the `orch/` snapshot's `app` rather than in
  `handlers.hello`. That is the only place it could come from out here: the comparison on the
  direct path is a health request on every reconnect, and the relay's `ready` frame is the
  *relay's* and has never heard of a Mac's build.
- **Handoff over `ho/`.** The channel and the envelope class exist; nothing in this repository
  publishes or consumes one.
