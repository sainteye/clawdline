# Browser content-key refresh (hosted receiver enabled in source)

The browser implementation composes the existing [v1 control contract](content-key-rekey.md)
with the paired machine's durable state. The hosted receiver and daemon producer are enabled in source; the daemon
additionally waits for its update to be accepted before opening Cloud.
This does not establish deployed rotation or future-content revocation.

## Authority and storage

`CloudViewerSession.connect` passes the authenticated account/device, persistent non-extractable
Ed25519 signing key and corresponding public bytes. `machinePairing` supplies a directly paired
machine's pinned, non-extractable verification key. Account roster membership, an envelope's
identities, and `allowWrites` do not grant rekey authority. Legacy bindings remain on their
existing path and must use explicit pairing to become eligible. No persistent private key is
exported. The cryptographic helper accepts a verification `CryptoKey` in addition to raw public
bytes; canonical messages, signatures, domains and KDF bytes are unchanged.

`ViewerRekey.request` creates a fresh extractable X25519 key pair, random operation ID and
32-byte random nonce. Epoch 1 is initialized only for `ms-1`; other existing bindings must
carry an authenticated durable floor. A new explicit pairing records the epoch of its
authenticated handover key ID. The fresh ephemeral is extractable solely for the primitive's
public-key match. It is never persisted or sent as a private key.

`ViewerRekey.answer` reserves the one pending operation before its first asynchronous step.
The crypto helper verifies the exact signed offer, account/machine/viewer, hash, machine
signature, expiry, recipient and floor, then decrypts and verifies the epoch/key ID. The
IndexedDB `adoptMachineRekey` transaction compares the latest stored floor, key ID, pin
generation and machine identity; it writes the master key, key ID, epoch and last consumed
operation together. Only its completion may change client memory. Aborts change neither
durable state nor memory. Competing tabs cannot replace a higher epoch or a different key
at the same epoch. Explicit pairing and legacy migration also preserve existing floors.
The original account-level compatibility key is never replaced by refresh.

Explicit pairing completion and same-origin pairing notifications invalidate successful
machine-pairing caches as well as missing answers, and cancel that machine's pending
exchange. An in-flight older lookup cannot repopulate a superseded cache. A durable
revision or floor conflict also invalidates the cached tuple, so a lost notification
cannot keep later bounded exchanges tied to an old pin generation. The existing
cooldown remains in force; this adds no automatic retries.

The pending operation is local and reserved while adoption is in progress. Duplicates cannot
start a second adoption, the consumed operation is recorded in the binding, and restart or
disconnect loses the ephemeral and pending offer. An unsolicited answer after restart is
rejected even if its signature is valid. A transaction's authority check is synchronous inside
its readwrite request callback; a disconnect after that check may leave a valid durable commit
for the next boot, but cannot resume the disconnected client or adopt into its memory.

## Capacity and recovery

Each client holds one operation across all machines (`VIEWER_REKEY_PENDING_LIMIT = 1`), with
the contract's 120-second lifetime and a 120-second minimum offer interval
(`VIEWER_REKEY_COOLDOWN_MS`). It retains zero unknown envelopes and schedules zero retries.
New traffic after the interval may request another operation; expiry itself sends nothing.
The relay's `content_key_routed` receipt confirms routing only, never recipient adoption or
successful key delivery. It neither consumes nor completes the pending offer. Both clients
validate control types, fields, operation identifiers and the finite relay error-code set.
Only a valid `content_key_error` for `frame=content_key_offer` with the exact current operation
identifier, before expiry, cancels the browser's pending exchange. It surfaces the fixed
`cloud_reconnecting` transient failure without exposing relay strings. Unrelated, expired,
answer-side or malformed controls cannot cancel another operation. Cancellation invalidates
the adoption authority callback and prevents later in-memory adoption; the durable transaction
boundary described above still applies. Cancellation keeps the original minimum offer interval,
retains no envelope or action and initiates no retry. The machine recognizes routed/error
controls and logs only validated fixed codes; it retains and retries no answer and treats no
control receipt as successful key delivery.
Both browser-owned bounds are registered with English and Taiwan Traditional Chinese copy,
and daemon readings explicitly report that live browser usage cannot be measured.

The unknown-key receive path does not advance replay state. It only waits for local offer
construction, never for the network answer: raw `content_key_answer` travels through the
same `messageChain` without deadlock. Successful adoption re-subscribes the machine's held
channels, including terminal subscriptions, to obtain retained state. Idempotent outstanding
reads reissue their original requests under the adopted key; actions are never replayed.
Reads queued during refresh keep their existing deadline and waiter rather than adding a
second queue. A read-only client can rekey and subscribe without publishing a command.
Its existing passive Session-refresh deadline remains authoritative if the relay has no
retained state; a discarded envelope is never reported as delivered. There is no claim that
subscription alone forces a disconnected machine to publish new data.

## Enable switch and deployment order

`CloudViewerSession` and `CloudClient` accept `enableViewerRekey: true`; both default to false.
The hosted `newCloudSession` factory explicitly sets it to true. The daemon
production caller enables `ContentKeyRuntime` behind update acceptance. Relay and
hosted receivers have been deployed and checked; running dev, browser/phone and
signed-release acceptance remain pending. Source enablement is not an online
revocation acceptance claim. The client additionally requires the trusted public device key
and `persistMachineRekey` callback. Tests opt in explicitly with a synthetic relay and database.

1. Integrate the native/crypto/epoch source branch and the root's Go envelope key ID compatibility
   change. The existing durable `mk-<epoch>-<full SHA256 hex>` format stays intact.
2. Deploy additive raw offer/answer validation and routing in the private relay. Its
   `DEFAULT_LIMITS.maxKeyIdLength` must accept 128 bytes before any producer emits these IDs.
   The browser's named `MAX_CONTENT_KEY_ID_BYTES` and Go `MaxKeyIDBytes` must both be 128.
3. Deploy and accept the hosted viewer with its receiver switch on. The source now
   includes active-pin authorization, rotation/publication barriers, answer handling
   and fresh snapshot recovery. Keep the daemon producer off until these receivers
   are deployed; that receiver-first step was completed on 2026-10-21.
4. Accept both ends with read-only B, revoked A, cross-tab races, persisted floors, restart,
   dropped/duplicated frames and recovery. The daemon source now opts in after receiver deployment; complete running dev
   and browser acceptance before releasing it. Rollbacks must retain durable floors.

## Verification

`viewer-rekey.test.ts` uses real WebCrypto and the actual client/message chain, with only relay
transport and storage faked. `viewer-rekey-storage.test.ts` models serialized IndexedDB
transactions with isolated writes and abort rollback. Tests cover read-only adoption,
signature/context/recipient failures, exact pending operations, expiry, duplicate and unsolicited
answers, aborts, competing floors, pairing generations, legacy migration, account alias preservation,
read resumption, subscription recovery, bounds and full-length durable IDs. The fake database
does not establish real browser crash consistency or production relay support. Root owns final
integration, browser/daemon acceptance, deployment and enabling production.

Historical v2.0.0 strong browser bindings (`legacy:false`, `ms-1`, no epoch or
revision) infer the epoch-1 floor and durably adopt authenticated answers without
a migration. A fresh Session reads the adopted key after restart. `legacy:true`
bindings remain ineligible to emit offers. Historical daemon strong pins without
scope retain their existing effective operate ceiling; this does not promote
readonly, resurrect pending/revoked pins or authorize roster-only viewers.
