# Connection health: what the daemon sends after an identity handover

This page is the daemon side of two Cloud uploads that run beside the relay, not through it:
owner-sealed diagnostics (Cloud contract §13) and the producer-reported loss totals of the metadata
uploader (§12.1). The pairing that records a content-key epoch is
`internal/adapters/cloud/health_pairing.go`; this page starts where its record exists.

## Owner-sealed diagnostics (`cmd/clawdline/health.go`)

`startConnectionHealth` builds one `healthsealed.Worker` per boot (`newSealedDiagnostics`) and runs
it on its own goroutine for the daemon's lifetime.

**Consent.** Off by default. A seal or a send happens only while both
`cloud_health_owner_retention` (`healthsealed.SettingKey`) is a JSON `true` and `cloud_enabled` is
on. The metadata switch `cloud_health_upload` never permits it. Both are re-read from the settings
file before every seal and every send, so withdrawing either stops the next request; the journal
writer only reads the worker's cached opt-in (`Worker.Enabled`), never a file.

**Keys and epochs** (`sealedKeys`). For each seal the daemon reads the identity
(`machine-identity-v1.json`), the master secret once (`cloudkeys.Files.MasterSecret`) and asks the
content-key epoch record (`content-key-epoch-v1.json`, `ContentEpochStore.Load`) about that very
secret: the record must name the same key id and account. Anything else — no record, another
account, another secret — is `key_handover_required`, returned before any request is made. No epoch
is assumed. Only then is `GET /v1/machines` asked for this machine's current `identity_epoch`, on
the sealed sender's own client. The master secret is cleared after the key is derived, and the
derived key after sealing; a send never holds the secret.

**Independent of the relay.** The sender has its own HTTPS transport (`sealedClient`: one
connection, 5 s dial and TLS, 10 s per request, no redirects so the bearer goes nowhere else). It
reads only stamped journal records (`sealedFeed`, attached to the watchdog's writer beside the
durable store, and alone when the store cannot open), so a stuck relay socket, writer lock or spool
does not hold it.

**What is sealed** (`sealedJob`). Records that say where the machine was stuck — `slow_detected`,
`stall_detected`, `released_stalled`, and `state_after_loss` in `slow` or `blocked` — become one
attachment per phase (`subject_kind: incident`, `subject_id: phase-<id>`): location, age, deadline,
build, stage, correlation, observed time, and the watchdog's dropped-transition count as coverage.
`sealedFeed` also subscribes to the stall capture worker (`rt.Capture.OnPublish`): a published
capture, and later the stall's recovery (`recovered` or `released_outcome_unknown`), each become a
`/2` revision of the same attachment (`SnapshotSchemaV2`, carrying `Capture` — a bounded excerpt,
never the raw dump — and `Recovery`). The feed remembers at most 32 stalled phases
(`maxOwnerIncidents`, `cloud.health_owner_incidents`); a /2 snapshot holds at most 24 frames and 8
sources (`cloud.health_sealed_snapshot_frames`, `cloud.health_sealed_snapshot_sources`).
Nothing else is in the whitelist.

**Lifecycle and failures.** The exact request body is written before the first send. A revision in
flight at shutdown stays pending; the next boot's worker resends it byte for byte on start and every
minute (`Worker.Run`), so a lost reply replays as `duplicate`. A pending revision whose identity
epoch has rotated is refused locally and never sent. Stop waits for the worker within the
connection-health stop deadline and names it if it is late.

| Case | What the status says |
| --- | --- |
| opt-in off | `owner_diagnostics.coverage: off` |
| no recorded epoch / wrong key | `failed`, `key_handover_required`, no request |
| no identity or secret; machine revoked or without an `identity_epoch` | `failed`, `key_unavailable` |
| `GET /v1/machines` unauthorized | `failed`, `auth_refused` |
| Cloud unreachable, 5xx | `failed`, `upstream_unavailable`; the body stays pending |
| jobs dropped at the queue bound or refused | `partial` with the counts |
| receipted | `active` |

`owner_diagnostics` is part of `upload` in `GET /v1/connection-health/status`; the
`cloud.health_sealed_*` capacity rows read the same status (queue depth now, local records as last
counted on the worker's goroutine, unmeasured before the first count).

## Producer-reported loss (`internal/adapters/healthupload`)

Store gaps, storage loss and exclusions are lifetime totals of one progress file. The progress file
now carries a `counter_epoch` (`ue-<24 hex>`), minted with its first write and never changed; a lost
or moved-aside file starts new totals under a new epoch.

The totals travel only in **metadata-only batches** (`events: []`, a `local_loss` block, batch id
`ll-<epoch>-<n>`, the uploader's own boot id), built when the settled totals moved past the last such
batch and there is nothing else to send. Event batches never carry the block: their bodies must stay
what the same records rebuild after a lost progress file, or the rebuilt batch would be
`409 idempotency_conflict` instead of a replay. The block is written into the stored body once and
resent byte for byte. A metadata-only batch that is refused or abandoned is not rebuilt until the
totals move again, so it cannot spend the hour's requests in a loop.

Gap and exclusion reasons are Cloud's allowlists exactly. An exclusion reason Cloud does not list is
sent as `unlisted`; a gap reason it does not list, or a value past 2^53−1, makes the whole block
unsendable rather than a block that would read the missing key as 0.

## Not done here

- The local identity route and console comparison below now write the content-key epoch after confirmed handover.
- `cloud_health_owner_retention` is set in Settings → Remote → **Owner diagnostics in Cloud**
  (`cloud/health-owner/OwnerRetentionBlock.tsx`) or with `clawdline setting`; see §8.
- The owner reader is wired below; real-browser acceptance remains pending.
## Product handover and browser health lifecycle

How a browser gets the key that opens a machine's owner-sealed diagnostics, and when the hosted
page records its own connection health. The cryptography is in
`internal/domain/cloud/health_pairing.go` and `web/console/src/legacy/js/net/cloud-pairing.js`; the Cloud contract
is Cloud's connection-health contract §13.9 (summarized in
`docs/admin-connection-health-contract.md` §9b). This page is the product path around them.

## 1. Why a second handover

The legacy pairing (`#pair=`, `internal/transport/cloud/pairing.go` `Pairing`) leaves the master
secret in the browser only as a non-extractable AES-GCM key, with no content-key epoch. That key
cannot be HKDF input and its epoch cannot be guessed, so a browser paired that way answers
`key_handover_required` for sealed diagnostics. It must take this handover once. Nothing extracts
the old key and nothing assigns it an epoch. The legacy pairing is unchanged and still drives the
relay.

## 2. The person's path

1. On the machine: Settings → Remote → **Diagnostics key** → **Show link**
   (`cloud/health-owner/IdentityHandoverBlock.tsx`). The daemon starts a pairing with Cloud and
   answers a `#pairid=<base64url QR>` link (`IdentityPairingFragmentName`), drawn as a QR.
2. On the browser, signed in to the same account, open the link. The hosted page takes the
   fragment out of the address at once (`takeIdentityPairing`), keeps it in this tab's session
   storage through a sign-in, and shows six digits and the machine key
   (`IdentityHandoverDialog.tsx`). **Codes match** sends the offer; **They differ** sends nothing.
3. The machine's Settings window now shows the same six digits and the browser key. **Codes
   match** there is the only approval: `POST /v1/cloud/pairing/identity/decision` with
   `{"pairing_id","approve":true}`. There is no automatic approval and no Cloud-reachable route.
4. Grant, activate and the machine's attested confirm follow without input. The machine records
   the epoch only from Cloud's grant receipt (`ContentEpochStore`, `content-key-epoch-v1.json`
   beside the key files).
5. The browser keeps the result only after verifying the confirm: one IndexedDB put of
   `{v, accountID, machineID, machineFingerprint, contentKeyEpoch, identityEpoch, contentKeyID,
   machineSigningKey, pairedAtMs, hkdfKey, masterKey}` under
   `clawdline.health-handover:<account>:<encodeURIComponent(machine)>`, both keys
   non-extractable (`storeHealthHandover`, `saveHandover`).

## 3. Local routes (this machine's own token only)

| Route | Answer |
| --- | --- |
| `GET /v1/cloud/pairing/identity` | the run's state |
| `POST /v1/cloud/pairing/identity` | starts a run (replacing one in flight) and answers once Cloud issued the QR |
| `DELETE /v1/cloud/pairing/identity` | cancels; Cloud's row is left to expire |
| `POST /v1/cloud/pairing/identity/decision` | the code decision; `approve` is required |

States: `idle`, `waiting_viewer` (link, `expires_at`), `confirm_code` (`code`,
`viewer_fingerprint`), `handing_over`, `paired` (`viewer_device_id`, `content_key_epoch`),
`denied`, `failed` (`error`: `expired`, `grant_not_recorded`, `no_master_secret`,
`transcript_mismatch`, `cancelled`, `unavailable`, or `cloud_<Cloud code>`), `cancelled`. A
remote reader gets 403; a decision for another pairing id gets 404 `unknown_pairing`; a decision
outside `confirm_code`, including a repeated one, gets 409 `pairing_not_waiting`; a malformed body
gets 400. Bounds: `cloud.identity_pairing_decision_bytes` (1 KiB),
`cloud.identity_pairing_run_seconds` (660 s), `cloud.identity_pairing_poll_attempts` (600).

## 4. The reader's authority

`CloudGate.tsx` installs the owner-diagnostics source from the session at each call: account,
device, the device's Ed25519 private key, and `tokenIdentity` — the `key_epoch` and
`identity_epoch` the last `/v1/tokens/device` mint answered, held as `deviceKeyEpoch` and
`identityEpoch` (`tokenIdentityOf`; null until a
token was minted, never 0). `heldMasterKey(machine)` reads only the handover record
(`heldMasterKeyFor`); no record is `{ key: null, contentKeyEpoch: null }`.

What stops or clears it:

- Sign-in screen (sign-out, expired session): the handover in flight stops and opened plaintext
  is cleared. Records stay: they are keyed by account and open nothing for another one.
- Another account: the run stops at its next await (`stale`); a record written after the stop is
  deleted again.
- Revocation of this browser: the run stops, plaintext is cleared, and the account's records are
  deleted (`forgetHandovers`).
- An identity-epoch rotation (any device of the account revoked): Cloud voids the grant, the next
  token mint names the new epoch, and sealed reads are refused until the browser takes the
  handover again. Nothing in the browser assumes the old grant still holds.

## 5. The browser's health producer

Off by default. It runs only in a hosted build made with **`VITE_VIEWER_HEALTH=on`**
(`VIEWER_HEALTH_BUILD_FLAG`), only while the page is signed in to the Cloud, attributed to the
machine being read (`ViewerHealthLifecycle`, `health-viewer/lifecycle.ts`). It stops on the
sign-in screen, revocation, another account (a fresh producer, not the old buffer) and unmount.
No account setting exists for it today; a build flag is the one explicit opt-in the current
architecture has, and no deployed page records anything unless its build sets it. What it records
and what it cannot record (`viewer_sse_gap`, the direct carrier) is in
`docs/admin-connection-health-contract.md` §11.

## 6. Proof

- `internal/transport/http/cloud_identity_pairing_test.go`: local-only routes, required decision,
  cancel, approve and deny.
- `web/console/src/cloud/health-owner/handover.test.ts`: the persistence seam — kept only after
  confirm, reload into an HKDF key for the right epoch only, nothing after denial, stop, account
  switch during the write, or revocation; the `#pairid=` fragment leaves the address.
- The route experiment (`TestIdentityPairingRoutesEndToEnd`, skipped unless its fixture runs it)
  drives the local routes against Cloud's real routes with the product's browser modules: machine
  denial keeps nothing; approval persists once; reload then sealed open; sign-out clears
  plaintext; rotation needs a new handover; revocation deletes records.

- `web/console/src/cloud/health-owner/pairing-screen.test.ts`: a fresh device on the pairing
  screen takes the link and keeps only its `clawdline.health-handover:` record; no device key or
  another account runs nothing and keeps the link; denial, sign-out during the comparison, and an
  account change during the write keep nothing; reload, a new content-key epoch, and revocation.
- `web/console/src/cloud/health-owner/retention.test.ts`: the opt-in's status lines (§8), and
  the poller under a hung settings read, an unreachable network, overlapping ticks, unmount with a
  late answer, a read outstanding while the switch is written, and a failed write.
- `internal/domain/capacity` `TestOwnerRetentionBoundsMatchTheBlock`: the two registered rows
  equal `OWNER_RETENTION_LIMITS`.
- `web/console/src/health-viewer/terminal-direct-health.test.ts`: a terminal's own direct
  negotiation (§9).

Not proven here: a real browser's IndexedDB, `CloudGate`'s wiring of the pairing screen in a
rendered page, the visual review of both sheets and the Settings block, the rendered
`OwnerRetentionBlock` itself (its poller is tested; the React wiring is not), and a real
`RTCPeerConnection` on the terminal path.

## 7. A browser that never took the legacy pairing

Signing in registers this browser as a device with its Ed25519 key before any relay pairing exists
(`cloud-boot.js` `ensureSession`); `connect` stops at `pairing_required` only because no relay
master key is held. That device can sign an identity offer, so `CloudGate` runs
`watchIdentityPairing` for the account of either the console or the pairing screen
(`handoverAccount`, kept through a reconnect's "checking" and cleared on sign-in and revocation).
`handoverSigner` admits only the session's own device, for the account the page is signed in to.

Two capabilities, said apart (`RelayAccess`): the handover leaves a diagnostics key and nothing the
relay reads, so the finished sheet on the pairing screen says the browser can open this machine's
diagnostics and that reading Sessions still needs **Pair browser** (`handoverDoneRelayNeeded`). It
grants no account membership: the link runs only for a page already signed in to the machine's
account, and a link for another account fails `wrong_account`. Sealed reads from such a browser also
need a minted device token for their epochs (`tokenIdentity`); until the relay connection mints
one the reader answers unknown epochs rather than guessing.

## 8. The owner-retention opt-in in Settings

Settings → Remote, under the diagnostics-key block: one switch for `cloud_health_owner_retention`
(`POST /v1/settings`, this machine's own token only). Default off; it is consent for sealed owner
evidence and never touches `cloud_health_upload`. The daemon re-reads the file before every seal
and send, so writing `false` cancels the next one. The lines under it come from
`GET /v1/connection-health/status` → `upload.status.owner_diagnostics` (`senderReading`,
`retentionLines`).

Both reads run through `createRetentionPoller` (`retention.ts`), with the bounds in
`OWNER_RETENTION_LIMITS`, registered as `console.owner_retention_poll_seconds` (5 s) and
`console.owner_retention_request_seconds` (5 s):

- one refresh at a time, both reads together. A 5 s tick that finds the last refresh still out is
  skipped, not queued; an explicit re-read (after a write) waits for the slot and coalesces;
- the whole refresh has a 5 s deadline. Past it both reads are aborted (`readSettings` takes the
  `AbortSignal`, and its own 10 s deadline now covers the body too) and whatever had not answered
  is shown as unknown (`timeout`). A read that ignores its abort keeps the slot until it settles,
  so a hung daemon costs one outstanding request, not one per tick;
- a settings read that fails makes the switch unknown (`ownerRetentionSettingUnknown`, the switch
  disabled), never the last value kept as if current;
- a switch write aborts the read in flight and makes it obsolete; no tick reads until the write
  answers, so a read from before the write cannot overwrite the written value. A failed write is
  unknown (`write_unconfirmed`) until the next read. Writes are not aborted on unmount; their late
  answer is not shown;
- unmount aborts the read in flight and shows nothing that answers later.

| Status | Shown |
| --- | --- |
| route unreadable or not a status | unknown, with the reason; never "off" or "0 sent" |
| no `owner_diagnostics` | this daemon runs no sender |
| off, nothing ever kept | nothing is sealed or sent |
| off, something receipted or kept | nothing new is sent; Cloud's copy is **not** deleted and stays until Cloud's own retention ends |
| on: `waiting` / `active` / `partial` | nothing yet / receipted count / receipted, dropped and refused counts |
| on: `failed` `key_handover_required` | take the diagnostics key first |
| on: other `failed` | the daemon's code and Cloud's code |
| local records | count and size, or "not counted yet" while `attachments` is null |

There is no account-level Cloud setting for this or for the browser producer: the browser
producer stays behind the explicit `VITE_VIEWER_HEALTH=on` build flag, and an account toggle would
need a Cloud setting this repository does not own.

## 9. A terminal's own direct negotiation

A terminal that opens the direct path itself (`TerminalChannelTransport.prepareDirect`, then
terminal-session's `direct_offer`) records the same stage a Session read's negotiation does in
`direct-carrier.ts`: one `viewer_carrier_replace`, class `transport`, under its own random
correlation, with the words of `directCarrierHealthError` (`health-viewer/direct-words.ts`, shared
by both):

| What happened | Recorded |
| --- | --- |
| the machine's answer applied and the channel opened | completed `ok` |
| ICE found no path, or the channel never opened | failed `network_unreachable` / `carrier_closed` |
| the offer could not be made or sealed | failed, the link's or the error's word |
| the offer was released before an answer was applied (`direct_offer` refused, timed out or stale) | completed `aborted`: the transport cannot tell which, so it does not guess a failure word |
| a channel the terminal still holds stops carrying after it opened | failed, its own correlation |
| a channel adopted by the page's carrier stops carrying | failed once, by the carrier's `lost` |
| released, replaced by the terminal's next negotiation, or borrowed from an open carrier | nothing |

No connection id, key id, SDP or sealed offer reaches a record; recording is synchronous and never
awaited, and an observer that throws is caught by `viewer-health-hook.js`. The relay request that
carries `direct_offer` is not given a stage here.

What this does not give: the Cloud's §14 health wire has no carrier dimension, so a terminal's
negotiation and a Session read's arrive as the same stage and class and cannot be told apart in
Cloud analytics, and neither is marked as direct rather than relay. Carrier-split analytics are unsupported until Cloud's
§14 parser accepts a carrier field; nothing in this repository claims them.
`docs/admin-connection-health-contract.md` §11 still says a terminal's `direct_offer` is not
recorded as a negotiation; that line predates this section and is outside this change.
