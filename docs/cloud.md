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

**How a person does it.** On the phone: open `app.clawdline.com`, sign in with GitHub, and the
console shows a pairing code and this browser's fingerprint. On the Mac: Settings → Cloud →
*Pair a Phone…*, paste the code, and compare the two fingerprints. The browser polls
`POST /v1/pairing/claim`, whose `202 pairing_pending` is the ordinary waiting state rather than a
failure, and connects as soon as the slot is written.

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
emitted — no `'unsafe-inline'`, and `connect-src` limited to the declared API and relay.

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

## What is deliberately not here yet

- **Copy.** The hosted pairing screen borrows the local door's strings, and three lines in
  `index.html` are English in every language. The string table is served by
  `Sources/RemoteServer.swift`, which this change does not own; adding proper names to `T` and to
  `/v1/strings` is a follow-up that must touch that file.
- **A scanner.** The offer is generated in the browser, so the code to scan is on the phone and
  the reader would have to be the Mac. The settings sheet takes a paste; a camera or a QR image
  is a later improvement, not a protocol change.
- **Transcripts and push.** The viewer subscribes to `t/<machine>/<session>` on demand, which the
  relay supports, but the Mac does not yet publish transcript envelopes. Web push needs the Mac's
  VAPID keys and does not work through the relay, so the hosted console registers no service
  worker.
- **`/v1/strings` for the hosted console.** There is no such route on the control plane, so the
  fetch fails and the page falls back to English inside its two-second budget. It is a 404 in the
  console and nothing else.
- **Handoff over `ho/`.** The channel and the envelope class exist; nothing in this repository
  publishes or consumes one.
