# Content-key runtime cutover

**Status: daemon and hosted receiver enabled in source; dev acceptance pending.**
`LinkOptions.ContentKeyRuntime` still defaults to false for library callers. The
production daemon explicitly enables it behind update acceptance. This source
change is not evidence of a running dev installation or a published release.
The foundation record below describes the original inactive implementation;
its remaining-obligations list is historical.

## Update acceptance before Cloud activation

The daemon defers the entire Cloud link (including `Open`, pairing, local
revocation, viewer grants and signing-key rotation) while an installed update
is pending or its state cannot be read. Local HTTP and console health routes
remain available to the update supervisor. Initial activation and settings
reload share the same admission check; the existing updater settle poll resumes
Cloud in the same boot after acceptance. Cancellation prevents a deferred open.

This guard must precede producer activation: the first supervisor upgrading
from v2.0.0 runs **the old version's** `Finish`.
Changing only the new supervisor cannot close that first rollback window.
`Finish` removes pending before writing its healthy receipt, so disappearance
alone is insufficient. Admission requires the accepted receipt for a witnessed
candidate, matching candidate build stamp, and an installation whose current
release still matches the process's captured executable/build identity. A
rollback or a replaced service hard link cannot relabel a surviving candidate
as the accepted old process. Unreadable pending quarantined by `Finish` remains
closed through its failed receipt.

Before admission, the device-management routes have no pairing object and
return a typed refusal without touching pins, revocation journals or keys;
there is no legacy-revocation fallback. This is an update compatibility guard,
not an assertion of deployed future-content revocation. The production caller
opts into `LinkOptions.ContentKeyRuntime` only behind this guard.

## The problem it closes

`Relay.PublishTracked` used to read `r.Secret` and `r.KeyID` as two fields, and
the spool's drain wrote whatever row it took. A rotation landing between those
steps could seal under one key and label with another id, or let a row sealed
under the retired key be written after the rotation reported success, because
the drain had already taken the bytes (`Spool.SendNext`, `Spool.ResendCorrelated`).

## The pieces

| Piece | Where | What it does |
|---|---|---|
| `KeyEpoch` | `internal/domain/cloud/key_epoch.go` | Holds one `ContentKeyState` (epoch, id, key) behind an `RWMutex` fence. |
| `KeyEpoch.Use(fn)` | same | Runs `fn` with the state while the fence is held shared. A halted epoch answers `ErrKeyEpochHalted` and does not run `fn`. |
| `KeyEpoch.Current()` | same | A snapshot for opening an inbound envelope; halted answers `ErrKeyEpochHalted`. |
| `KeyEpoch.Cutover(rotate, retire)` | same | Closes the fence, waits for every running `Use`, calls `rotate(old)`, calls `retire(old, next)`, installs `next`, reopens. |
| `KeyEpoch.Recover(recover, retire)` | same | Reopens a halted epoch from what the durable store says now. |
| `ContentKeyState.Sendable(keyID)` | same | `rk-*` or the installed id; `""` (unlabelled) is not. |
| `Relay.Keys` | `internal/transport/cloud/relay.go` | When set, every master-key publish runs inside `Use`, from reservation through `LabelKey`, seal, `SealCorrelated` and `MirrorStatus`. |
| `Options.KeyEpoch` | `internal/adapters/cloud/transport.go` | When set on a machine: inbound opens only with the installed key and only when the envelope names the installed id; every drain write and reconnect re-send runs under the fence and re-checks the row. |
| `SpoolRow.KeyID`, `Spool.LabelKey` | `internal/adapters/cloud/spool.go` | Which key a row is sealed under, recorded while the row is still reserved. |
| `Spool.RetireKeys(keep)`, `Spool.RetireForCutover` | same | Burns every live row (reserved, ready, sent) whose id `keep` refuses, as `content_key_retired`. |
| `Spool.SendNextKeyed(keep)` | same | `SendNext` that burns a ready row under a refused id instead of handing it out. |

With neither `Relay.Keys` nor `Options.KeyEpoch` set, every call behaves as it
did before: `Secret`/`KeyID` seal, `ContentKey` opens, the drain is unfenced.
The viewer's `ContentKeyFor`, per-connection `rk-*` terminal keys and the peer
paths never go through `KeyEpoch`.

## Terminal ingress uses the installed content key

Terminal and shared-carrier requests on `termi/<machine>/<viewer>` use the same
installed content-key validation as Session requests on `ctl/` and `r/`, on both
the relay and direct carriers. The channel still binds its viewer segment to the
signed sender. Per-connection `rk-*` reply keys cannot authorize ingress, and
retired content keys remain refused after cutover. Library callers without a
`KeyEpoch` retain the legacy `ms-1` requirement for terminal requests.

The rotation regression now exercises all three request channels. Before the
fix, only the terminal canary failed after cutover: its channel precheck required
`ms-1` before the installed-key validator could run, silently dropping valid
requests and leaving the browser waiting for a receipt.

## The cutover, in order

1. `Cutover` takes the write lock. Go's `RWMutex` stops new `Use` callers once a
   writer waits, so no new publish or send starts.
2. It waits for every `Use` already running. Socket I/O has an
   `OpeningTimeout` deadline, but acquiring the socket write mutex and finishing
   callbacks (including `MirrorStatus`) have separate owners. The complete
   cutover wait has no deadline here; integrated carriers must establish and
   measure their callback and lock-wait bounds.
3. `rotate(old)` is the durable step, normally
   `ContentKeyStore.RotateContentKey(operationID, old.Epoch, nil)`. It runs
   under the lock and must not publish.
4. `retire(old, next)` is normally `Spool.RetireForCutover`. It burns, under the
   spool's own lock, every reserved, ready and sent row not labelled with
   `next`'s id or an `rk-*` id. A sent row is marked `transport_uncertain`. Each
   burned row is handed out by `TakeBurned`, so its waiter hears
   `SettleBurned` on the next drain tick. A late relay receipt for it is
   `late_ignored`. The sequence counter does not move back.
5. `next` is installed and the fence reopens.

Lock order is always `KeyEpoch`, then the spool. A function passed to `Use` must
not call `Use`, `Current`, `Cutover` or `Recover`. A `MirrorStatus` hook runs
inside `Use` and must not publish through the relay.

## Failure behaviour

- **Any failure in `rotate`, validation or `retire` halts the epoch.** The old
  state stays installed, and `Use` and `Current` answer `ErrKeyEpochHalted` (the
  cause is wrapped). Nothing is sealed, sent, re-sent or opened under the master
  key: the store may already have advanced its floor, so carrying on under the
  old key would mean sending under a retired key.
- Every adopted state must have the exact `ms-1` legacy ID or
  `mk-<epoch>-<SHA256(key)>` ID. A higher epoch cannot reuse an old ID. A rotation
  that answers an epoch that is not later or an invalid state is refused (`ErrKeyEpochRegress` / `ErrContentKeyState`) and halts.
- A halted epoch refuses `Cutover`. The next step is `Recover`, not a second
  rotation.
- `Recover(store.RecoverContentKey, spool.RetireForCutover)` adopts what the
  store holds. If it holds the installed epoch and key, the rotation never
  became durable and the epoch simply reopens. If it holds a later epoch, the
  old rows are retired first and then the new state is installed. If it holds an
  earlier epoch, or another key at the same epoch, recovery answers
  `ErrKeyEpochRegress` and stays halted. A failed retire leaves the epoch
  halted at the old state, so the next recovery retries it.
- While halted, the drain sends nothing and keeps ticking. A reconnect re-send
  stops at the first refused row. Rows still `sent` are settled by their attempt
  window if recovery does not come in time.
- An inbound envelope whose id is not the installed one is dropped as
  `decrypt_failed`, with `key=not_current` in the log line. No old master key is
  kept for opening ordinary requests.
- With `KeyEpoch` set, an unlabelled spool row is unknown, and unknown is not
  current: the drain burns it instead of writing it. Today the relay is the only
  production producer, and it labels every row. `cmd/clawdline`'s
  `publishProbe` does not, so it must not share a spool with a fenced transport.

## Bounds

No new bound is introduced. The fence holds no queue: waiting publishers are
goroutines owned by their callers. The socket I/O deadline does not bound
callback execution or mutex acquisition, so no whole-cutover latency bound is
claimed here. Retired rows become tombstones under the existing
`cloud.spool_receipts` cap and `TombstoneRetention`.

## Obligations of the next owner

These are not done, and the foundation is not complete until they are:

- **Construct and share one `KeyEpoch`** from
  `ContentKeyStore.LoadOrCreateContentKey`, and set it on both `Relay.Keys` and
  the machine transport's `Options.KeyEpoch`. Setting only one is not
  supported: a fenced transport would burn the unfenced relay's rows as unknown
  or stale.
- **Wire `Link.Revoke` (or its successor) to `Cutover`** once the Cloud scope
  carrier lands, with a stable operation ID per revocation so that a retried
  rotation resumes rather than mints again. On a halted epoch, run `Recover`
  before anything else, including at daemon start.
- **Distribute the new key to remaining viewers** through the separate rekey
  control frame owned by the crypto work (`domain/rekey.go`). This page adds no
  protocol for it.
- **Retire old frames the direct carrier queued itself.** `MirrorStatus`
  returning means the carrier accepted the bytes, not that they were delivered.
  A carrier that buffers an old-key frame must drop it at cutover; nothing here
  does that.
- **Viewer side.** `ContentKeyFor` still answers by `(sender, keyID)`. Accepting
  `mk-*` ids from a paired machine is the viewer's own change.
- Acceptance as a production rotation, including a real revocation run, has not
  been done.

## Independent review follow-up

The independent static review of `3fadf9a24` retained no confirmed exploitable
finding in the inactive foundation. Its trusted-callback ID concern is hardened
by validating the complete epoch-derived key ID on construction, cutover and
recovery. A higher epoch returning its previous ID was accepted before this
validation; the regression test now requires rejection and a halted epoch.
The whole-wait deadline claim has been corrected above. Inbound decryption still
takes a snapshot before dispatch: integration must establish a fresh active-pin
authorization point for in-flight requests. This outbound fence alone does not
prove immediate cancellation of all in-flight inbound work.


The realistic epoch fixtures exposed a compatibility gap: the full epoch/hash ID
is longer than the previous 64-byte envelope key-ID limit. The daemon's parser
now permits 128 bytes; this is an additive reader change, not a new ID format.
Browser and private relay readers must accept the same bound before production
rotation is enabled. The existing short IDs remain valid. The relay reader
upgrade and actual end-to-end acceptance remain pending.

## Opt-in runtime integration

`Link.wire` loads or recovers durable content state and constructs one
`KeyEpoch`, shared by `Relay.Keys` and machine `Options.KeyEpoch`. It resumes
pending revocation before constructing the publisher. Recovery errors refuse
startup, without a legacy-key fallback. Ordinary pairing hands over the current
durable key and actual ID. The pure control contract and full epoch/hash IDs
are unchanged.

`runtimeAuthority.mu` orders ordinary request handling, terminal handling,
pairing disclosure/final pin, revocation and raw rekey disclosure. Pairing saves
the generation before its invitation or pasted-offer operation starts. Revocation
invalidates that generation; stale unfinished pairing cannot clear the refusal.
A new explicit successful pairing can restore a pin. Pin persistence failures
are errors, including directory-sync failures.

Revocation enters the exclusive epoch fence before writing a fixed-size intent
in `cloudkeys.Files`, containing viewer ID, stable random operation ID and expected epoch. Under the exclusive
epoch fence it persists deny, rotates with that operation, retires old spool
rows and marks all direct peers closed/removes their mappings. Closing SCTP
also retires accepted-but-undelivered direct data. After reopening the fence,
it retires revoked-viewer terminal connections, marks the journal complete and
requests a full publisher restatement on the next normal pass. Errors inside
the epoch transaction halt publication, including intent creation failures before
or after rename. An unreadable existing journal or failed deny persistence while
resuming it also halts publication. A retry with no durable intent recovers the
current durable key before starting one; a readable intent retains its operation
ID. Neither path reopens the old epoch after an uncertain write.
Restart/retry resumes the exact intent,
even when the pin already says revoked; it never remints an already durable
operation. Missing or already-revoked devices without pending work return
unchanged. A new intent replaces only a completed intent. This is a single
daemon transaction owner, not an interprocess transaction lock.

`Transport.dispatchRawRekey` bounds raw workers at eight and refuses excess
with `ErrRekeyOverload`; slow handlers do not block the normal reader. The
4096-byte contract bound applies before copying. Link admission additionally
allows one offer per two seconds globally. There is no unbounded nonce set;
duplicate offers share the admission budget and can only receive the current
key. Raw answers never enter retained publication or the spool.

The answer handler uses configured account/machine context, an active completed local
pin with valid effective scope, its verified signature, a fresh roster
fetch and matching active roster key with `read_transcript`. Historical v2.0.0
strong pins with absent scope retain their existing operate ceiling; readonly stays readonly. Rekey does not write or migrate pins. Revoked,
pending, foreign-account, malformed-key and unknown-scope pins remain refused,
and roster membership alone never qualifies. Readonly qualifies;
`send_prompt` is unnecessary. It re-reads the pin immediately before sealing,
uses fresh `crypto/rand` ephemeral/nonce bytes, and sends the targeted signed raw
answer inside the authority sequence and epoch fence. Contract expiry and epoch
checks still apply. Routing metadata grants no authority.

Carrier master answers seal/send inside `Use`. Mirrored status already owns
the relay's fence and calls `sendWithinEpoch` without recursively entering it.
Other direct sends check the queued envelope's key ID inside `Use`. After
acquiring `sendMu`, the final direct send rechecks closure and the active local
pin. Cutover retirement avoids `terminalMu` while holding the epoch fence:
terminal publishers can themselves wait for that fence. Per-connection `rk-*`
keys remain separate; revoked-viewer terminal connections are retired too.

The complete cutover wait is **not latency bounded**. The epoch owner waits
for relay callbacks and direct `sendMu` owners. `OpeningTimeout` bounds socket
I/O after WebSocket `writeMu` acquisition, not mutex wait or callbacks. Pion
owns data-channel enqueue/backpressure and network delivery. There is no measured
whole-cutover bound or remote erasure of already delivered bytes.

The alternate `IdentityPairing` adapter now joins the same authority through
short guarded disclosure and confirm/pin sections, as described below. Its
compatibility epoch record no longer selects the runtime key or resets its floor;
disabled-mode behavior remains unchanged.

Synthetic tests cover real answer seal/open and future ciphertext, readonly B,
revoked A, restart and operation idempotence, journal persistence boundaries,
failed pin writes, stale pairing generations, concurrent answer/revoke, wrong
or missing pins, bounded slow raw callbacks and late direct frame rejection.
Task artifacts hold actual run results. The deployed receivers and synthetic cross-system recovery do not establish
production browser/phone acceptance or operational cutover latency. Complete
dev and release acceptance before claiming released future-content revocation;
never roll back durable floors as a compatibility workaround.


## Identity handover participates in runtime authority

When `ContentKeyRuntime` is enabled, `IdentityPairing.Begin` captures the same
revocation generation as ordinary pairing. `MachineIdentityPairing.Authorize`
enters the shared authority only to read the selected durable state, reserve the
local scope and seal/write the grant, or verify activation and write the attested
confirm plus local epoch and pin. Offer/activate polling and the person's code
comparison run outside that lock. A revoked generation cannot disclose again or
finish a pin, including a revoke of a viewer not yet pinned. Cancellation and
replacement serialize with these short critical sections; the signed QR's expiry
also bounds the adapter context. Network writes retain their existing request
deadlines; this does not claim an overall cutover latency bound.

`ContentState` selects key bytes and epoch together. The old
`ContentEpochStore` is a compatibility record for the diagnostics uploader,
not a second authority: a historical lower epoch may remain until a successful
confirmed handover records the current epoch. A future floor, same-epoch key
mismatch, malformed record, or different account fails closed without overwriting
that record. Local persistence failure never reports paired. The completed pin
uses the decoded offer's verified signing key and fingerprint, never a roster
lookup. New diagnostic viewers reserve readonly scope before disclosure; existing
ceilings are preserved. Pending reservations are not completed rekey pins.

### Browser data flow and capability boundary

`#pairid` → `pairThisViewer` → `openIdentityGrant` authenticates the exact QR,
offer, machine key and epoch, imports non-extractable AES-GCM/HKDF keys, and
produces activation key confirmation. Only `openIdentityConfirm` accepting that
exact activation allows `saveHandover` → `storeHealthHandover`. That existing
account-and-machine IndexedDB record feeds `heldMasterKeyFor` → `sealedKeyFor`
→ the diagnostic attachment reader. Its transaction now retains the content and
identity epoch floors, the authenticated machine signing key, and the full
ordinary-envelope content key ID (`ms-1` or `mk-<epoch>-<SHA256(raw key)>`). Equal
epoch key substitution and machine pin substitution are refused. An older record
without the additive pin/ID can be upgraded by a newly confirmed handover; no
account-wide fallback is introduced.

This diagnostic handover does not establish ordinary Session access. The existing
`handoverDoneWord` and `RelayAccess` explicitly retain that distinction, including
for a signed-in browser still on the ordinary pairing screen. Ordinary envelopes
continue to obtain their key and machine pin from `storePairingCryptoKeys` and
`adoptMachineRekey`; their per-machine binding and epoch floor are unchanged.
The AES-GCM key imported by identity handover is cryptographically usable with the
same full hash ID at later epochs, but the diagnostic page does not silently
adopt it into an ordinary relay binding or claim that Session access was granted.

The production daemon opts in behind update admission; library callers still
default to disabled. Synthetic tests exercise real signatures and AEAD. Running
dev, production browser/phone and signed-release acceptance remain separate
requirements.

## Receiver-first rollout status

On 2026-10-21 the relay receiver was deployed and its public health version and
schema fingerprint checked. The hosted receiver serves `a01fab0861110c84b74b8b0f4d02fe6474f01527`;
its served main bundle passed CloudGate and the English/Traditional Chinese catalogs
were complete. These deployment checks establish receiver availability, not a
production revocation experiment.

The hosted `newCloudSession` factory enables `ViewerRekey`. The production daemon
now enables `ContentKeyRuntime` behind the update-acceptance gate described above.
Library defaults remain false. The next acceptance is the running dev build and
controlled browser recovery, followed by the exact signed release. No public
release or phone acceptance is claimed by this source change.
