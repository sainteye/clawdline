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

**Sequences survive a relaunch.** Both ends refuse an envelope whose sequence did not advance, so
a counter that restarts at zero does not merely repeat itself — it makes the Mac silent on every
channel until it has climbed back past whatever the viewer already saw. `CloudSequenceFile` writes
down a *ceiling* before handing out any number under it, so a crash skips a block and can never
repeat one. A file it cannot read refuses rather than restarting from zero.

**Commands go through the door they already went through.** `RemoteServerCloudCommandRouter`
converts a verified cloud command back into an in-process request, so authentication,
idempotency, menu safety, image validation and audit stay the local HTTP route's single
implementation. The write gate is the same one: a Mac with `remote_write` off refuses a cloud
command with `cloud_commands_disabled`, exactly as it refuses one from the browser on its own
network.

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

**Pairing.** Covered in its own section below.

**Reconnect.** `keepConnected` re-acquires the device token and restarts the socket with
exponential backoff and jitter, and treats a refusal as terminal for the same reason the Mac
does. Outbound sequences use the same reserve-ahead discipline as the Mac, in `localStorage`.

## Pairing, and the design decision inside it

**The four-phase protocol in `CloudPairing.swift` has no server.** That file describes an
`offer`/`grant`/`activate`/`confirm` handover whose wire form is "the complete request body of
the four phase-write APIs". The control plane exposes exactly three pairing routes — `start`,
`complete`, `claim` — and refuses a second `complete` with `already_completed`, so the account
has **one** ciphertext slot and it can be written once. `CloudAccountClient`'s
`startPairing`/`completePairing`/`claimPairing` already speak that three-call shape, and
`CloudPairingCryptographyProviding` already asks for one opaque blob rather than four. This is a
real gap between two designs in two repositories, and it is named here rather than papered over.

What ships is the **single-blob handover**, built out of `CloudPairing`'s own KDF, wrapper, AAD
and canonical-JSON primitives. No new cryptography was invented; two document shapes were, because
none were pinned:

- **`pairing_offer`** — eleven members, canonical JSON, carried as base64url. The viewer's
  `pairing_id` and one-time `claim_nonce` from `POST /v1/pairing/start`, its device id, its
  Ed25519 and X25519 public keys, its fingerprint, a pairing nonce, and an expiry no further away
  than ten minutes.
- **`pairing_handover`** — eight members. The account id, the machine id, the machine's Ed25519
  public key and fingerprint, the content-key id, and the content key.

**The direction, and why.** The blob the API can carry travels from the *sender* (`complete`) to
the *requester* (`start`, then `claim`), and the content key has to travel Mac → viewer. So the
**viewer is the requester**: it asks for the handle, shows the offer, and a person carries that
offer to the Mac. The Mac seals for exactly that offer, writes the one slot, and pins the viewer;
the viewer claims once and the record is destroyed. The property `PROTOCOL.md` §3 asks for
survives: an attacker holding the OAuth session can register a device and call `start`, but no Mac
ever seals for a `pairing_id` that was not physically handed to it.

Both sides pin the other's Ed25519 key out of band — the Mac from the fragment it was handed, the
viewer from inside the sealed blob — rather than from anything the cloud says. The Mac
additionally refuses a delivery whose echoed fingerprint disagrees with the fragment, which is the
one substitution a person comparing codes on two screens cannot see.

**The wire form is checked across three implementations.** `tools/generate-protocol-vectors.swift`
produces a complete handover into `Tests/protocol-vectors.json`;
`Tests/CloudLifecycleTests.swift` opens it with `CloudHandover`, and
`Tests/web-cloud-pairing.mjs` opens it with `cloud-pairing.js`. A drifted mirror is the worst kind
of broken here — nothing throws, pairing simply never completes for a real person — so the
agreement is measured rather than asserted in a comment.

**How a person does it.** On an iPhone, open `app.clawdline.com` in Safari, add Clawdline to the
Home Screen, close Safari, and continue from the installed app. That order is part of the security
model rather than presentation: Safari and the Home Screen app have isolated IndexedDB stores,
and neither the viewer's signing key nor the account content key is extractable. The Safari page
therefore stops before login and before `POST /v1/auth/session`; it does not consume a viewer slot
whose key could never move into the PWA.

In the installed app, sign in with GitHub. Then on the Mac use Settings → Cloud → *Pair a
Phone…*. The Mac creates a short-lived secret locally, sends only its SHA-256 to
`POST /v1/pairing/invitations/start`, and shows `https://app.clawdline.com/#pair=…` as a QR. The
PWA scans and decodes that QR locally; camera frames never leave the phone. The fragment is kept
only for this pairing and neither the app server nor the OAuth callback receives it. The PWA makes
its ordinary viewer offer, encrypts it with AES-GCM under the QR secret, and sends only the secret
hash and opaque bytes to `POST /v1/pairing/invitations/accept`. The Mac polls
`POST /v1/pairing/invitations/poll`, decrypts that offer locally, and the existing
`complete`/`claim` X25519 handover moves the account master secret Mac → viewer.

The acceptance seam after that poll is bounded too. `CloudPairingCompleter.production` loads the
restored identity, device signing key and master secret through one-shot `CloudKeychainReader`
awaits. A locked or non-answering load-or-create returns a visible pairing failure after ten
seconds; closing the sheet cancels the await immediately. The underlying synchronous Security call
may still finish, so the UI claims only that this pairing attempt stopped waiting—not that a read or
key creation was rolled back. The ordinary Settings identity restore uses the callback spelling and
still accepts a terminal identity that arrives after its timeout warning.

There are deliberately two user-visible checks. GitHub proves the phone and Mac belong to the
same Clawdline account; scanning the QR proves the phone is pairing with the Mac physically in
front of the person. Both are required because the Cloud service is only a ciphertext relay: it
never receives the QR secret, the plaintext viewer offer, or the account master secret. The old
long offer remains a protocol compatibility seam, not the primary UI.

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
build signs with decides whether macOS re-asks for those items after every rebuild. Three states
are possible and all three are now said out loud:

| what `security find-identity` finds | what `./build.sh` does |
|---|---|
| exactly one `Clawdline Local Development` in the explicit Keychain, Keychain unlocked | signs with it, scoped to that same Keychain |
| exactly one, **Keychain locked or not answering** | **fails, naming both repairs** |
| two or more with that name | fails; it will not choose by Keychain order |
| none, or the query fails | **fails**; ad-hoc requires `CLAWDLINE_SIGN_ADHOC=1` or `CLAWDLINE_SIGN_IDENTITY=-` |

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

- **Copy.** The hosted pairing screen borrows the local door's strings, and three lines in
  `index.html` are English in every language. The string table is served by
  `Sources/RemoteServer.swift`, which this change does not own; adding proper names to `T` and to
  `/v1/strings` is a follow-up that must touch that file.
- **Transcripts and push.** The viewer subscribes to `t/<machine>/<session>` on demand, which the
  relay supports, but the Mac does not yet publish transcript envelopes. Web push needs the Mac's
  VAPID keys and does not work through the relay, so the hosted console registers no service
  worker.
- **`/v1/strings` for the hosted console.** There is no such route on the control plane, so the
  fetch fails and the page falls back to English inside its two-second budget. It is a 404 in the
  console and nothing else.
- **Handoff over `ho/`.** The channel and the envelope class exist; nothing in this repository
  publishes or consumes one.
