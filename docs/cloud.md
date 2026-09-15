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
resend the exact in-window sent frames in sequence order. A successful durable seal wakes one
independently owned drain worker and returns to the producer; it does not await that frame's socket
write or ACK. The worker writes ready rows in global sequence order until either the row or exact
sealed-frame-byte window is full, and a correlated receipt replenishes one slot. The current
implementation defaults are 8 sent rows and 4 MiB, explicitly
`implementation_default_pending_w6`, not approved budgets. A lower reserved/ready row is never
skipped. Restart burns sent uncertainty, and only a correlated
authenticated channel/sequence ack or refusal settles a live row. Unreadable or unsafe state never
restarts from zero. On the first Mac open, the former `cloud-sequence.json` is descriptor-checked
and its sender ceiling is durably applied as the spool's minimum next sequence before any publish;
the same schema-compatible file remains a live rollback fence. Before the new spool commits a
reservation at an old-image block boundary, it first raises the predecessor's ceiling. Rollback may
skip a block but cannot reuse a sequence already returned by the new image. Missing legacy state
means no prior floor, while invalid legacy state fails startup and is preserved/quarantined rather
than being read as zero.

**A Session row goes out when something a viewer reads has changed.** Each
`s/<machine>/<session>` payload is `{"session": <row>, "at": <scan seconds>, "scan": {…}}`, where
the row is the one `/v1/sessions` serializes. `CloudAppBridge` compares each row with the one it
last published for that Session with three freshness-only paths removed —
`closeability.observed_at`, `closeability.session_generation` and
`closeability.source.observed_at` — and skips it when nothing else differs. Those three move on
every SessionWatch reading; no reader in `Resources/web/app/js` uses them (its closeability gate
reads `state`, `reasons`, `mover`, `attestation_id`, `version` and `source.freshness`, which still
count). Measured in this Mac's log on 2026-09-15 from 04:00 to 06:00, with nobody working and before
this rule: 195 Session publications an hour, every one of them republishing every row, because
`closeability.observed_at` is the projection's own clock. A row that does differ is still published whole, freshness values
included. `work_since`, `agents[].at`, `shells[].at` and the coordination `createdAt` clocks are
evidence instants that move only with what they describe, so they count like any other field.
**Every three minutes the rows go out again.** Because an idle Mac may now publish nothing, and the
relay forgets every row whenever its object is evicted, a scan that finds three minutes gone since
the last pass that sent every row and the inventory — a forced scan at transport-ready, an earlier
refresh, or a `sessions.snapshot` pass — sends every published row it would have skipped and then
the inventory marker (`CloudAppBridge.sessionPresenceIntervalMilliseconds`). Rows that changed in
between do not restart that clock. This is what brings the rows back to a viewer that cannot ask for
them (a device without `send_prompt`), to one whose request failed, and to one whose renewal took
over a socket that had stopped hearing; it also keeps the machine current, since the hosted console
calls a machine current for five minutes after its newest `s/` or `orch/` envelope. The check runs
with each scan rather than on a timer of its own, and an incomplete scan runs it too: it sends the
rows it names, every other still-published row from the newest copy the Mac holds, and an inventory
naming every Session whose row is still published — a list that can keep a viewer's row but never
drop one, since nothing has been tombstoned since the last complete scan. Measured on this Mac on
2026-09-15 (sealed frame sizes in `Clawdline.log`, `stage=socket_write frame_bytes`): nine rows came
to 27,681 bytes and the inventory to 529, so about 31 KB a pass for ten Sessions — about 0.63 MB an
hour for an idle Mac, sent once to the relay and once to each connected viewer, against about 9.4 MB
an hour when every scan re-sent every row. The marker carries `features` beside `inventory`, never
inside it: an older page refuses an unknown key in the inventory object.

**`transcript_signature` is a Cloud-only row field.** When present it is a non-empty opaque string
equal to the `signature` the Mac's `transcript` read answer returns for that Session's transcript
file at that moment; a viewer re-reads the transcript when it or the row's `state` changes, and not
otherwise — the state edge buys one read because the signature is debounced and a turn that has just
ended is when a reader is waiting (`createTranscriptRefetchPolicy`). It is
absent, never empty, when the signature is unknown (no transcript file, or the watch has not
reported yet), and absence is also what a Mac older than this field, or a Linux executor, sends.
Local `/v1/sessions` and SSE rows never carry it: transcript bytes must not repaint local
session-list consumers (`Sources/TranscriptRevisionWatch.swift`). The signature comes from
`CloudTranscriptSignatureWatch`, a second instance of the fd-event transcript watch whose only
demand is a running bridge: every Session publication names the published Sessions to it, and
stopping the bridge ends every watch, so nothing is watched while Cloud is disabled. Resolving a
Session to its file happens off the publication path and only again when the row's assistant, tty,
conversation id or working directory changes, a watch ends, the folder holding a watched file has
changed since that file was chosen (a conversation that goes on in a new file adds it there, which
no row field shows when the row has no conversation id; asking costs one `stat` per watched file
per publication), or a minute has passed. A watch reports its file once writes pause for 90 ms, and
at least once a second while they do not. A signature change republishes that row even when nothing
else changed, in at most one pass per second: the first change publishes at once, and everything
that changes during the following second rides one later pass. An unchanged signature never
republishes anything.

**A page that reconnects asks for the rows.** The relay replays each channel's last envelope from
memory (`relay/src/lib/cache.ts`), that memory dies with the account's object, and an idle account's
object is evicted while its sockets stay open (`relay/src/account-do.ts`) — so after an eviction a
viewer that connects is replayed nothing, and an unchanged row is never published again on its own.
Correctness does not depend on how often that happens:

- **Wire.** The viewer sends `{"type":"sessions.snapshot","session":"__clawdline_machine__","request":"<id>"}`
  on `ctl/<machine>`, with `"orchestrator": true` added only when the page holds no `orch/` snapshot
  of that machine from any of its clients — a page back from the background keeps the one it had. The
  Mac re-sends what a transport-ready force sends — every current row (signature included) on its own
  `s/` channel and the inventory, preceded by its last `orch/` snapshot with `cloud_status` when a
  request in the pass asked for it and may have it now — then answers `read:<id>` on
  `t/<machine>/__clawdline_machine__` with `{"sessions":[ids],"complete":bool}`. The `orch/` snapshot
  is opt-in and held to a floor because it can be hundreds of kilobytes (this Mac's log on 2026-09-15
  shows outbound frames of about 300 and 740 KB repeated), the relay never keeps an entry larger than
  256 KB for replay, and the relay fans every `s/` and `orch/` envelope out to every viewer socket on
  the account, phones included. So a request that arrived before the Mac's last `orch/` publication
  of any kind was already sent that one and needs nothing; the others get it at once only when that
  publication is more than a minute old (`CloudAppBridge.orchestratorResendFloorMilliseconds`).
  Inside the minute the rows and the answer go out at once and the `orch/` snapshot is owed: it is
  re-sent when the minute has passed, unless a publication in between already reached every page
  that asked. Everything goes through the ordinary publication lanes, so the rows land under the
  sequence guards every row already has. It is read-level: the remote-write switch does not gate it.
- **Bound.** However many viewers ask, one Mac sends its rows at most once per five seconds
  (`CloudAppBridge.sessionSnapshotIntervalMilliseconds`); a request that arrives inside the window, or
  while a pass runs, is answered by the next pass. Sixty-four requests may wait for one pass; the next
  is refused `cloud_read_busy`.
- **Who asks, and when.** A Mac lists the word in `features` — in its `cloud_status` digest and beside
  its inventory — and the page remembers that list with the machine's descriptor in `localStorage`,
  so a relaunched page can ask before any frame arrives. A client decides for each such machine once:
  when its socket becomes ready without having taken over from a still-live socket (a first
  connection, a return from a quiesced page, a reconnect after a drop), or when a machine it has not
  decided for first lists the word. It lets the relay's replay land first (750 ms): a machine whose
  inventory and every listed row arrived on this socket, and whose `orch/` snapshot the page holds,
  is current and is not asked at all. A token renewal is make-before-break, and it is taken as one
  only with proof that the old socket still hears: as the new client starts connecting it sends
  `{"type":"ping"}` on the old socket, which the relay answers with `pong` even from a hibernating
  object, and only when the old socket has received a frame since then does the new client take
  over what it heard during the handshake and ask nothing. A socket left half-open by a network
  change still reads `ready` and hears nothing, so its successor recovers the rows like any
  reconnection.
- **A device that cannot ask waits for the refresh pass.** A device without `send_prompt` cannot
  publish on `ctl/` at all (`relay/src/account-do.ts`). For each machine that lists the word it
  shows the machine as sending (`scan.recovering`, never an empty account) until this socket has
  heard that machine's inventory and a row or tombstone for every id it lists — what the Mac's pass
  every three minutes sends — and after four minutes (`SESSION_REFRESH_WAIT_MS`: the interval, a
  scan and slack) it has failed as `cloud_sessions_incomplete`. A renewal carries the wait over with
  its deadline, and what the old socket heard counts.
- **Older machines.** A Mac that lists no `features`, and a Linux executor, are never sent the word;
  the page behaves as it did before. A cached list that turns out wrong meets `unknown_command`, which
  ends the attempt quietly and is remembered like any other word the machine lacks.
- **What the list says meanwhile.** While a machine's rows are on the way the Session list's
  `scan.emptyAuthoritative` is false and `scan.recovering` names it: the list says it is waiting
  instead of "no sessions", and the open conversation is not closed by an empty frame. A retryable
  failure is asked once more after five seconds; then `scan.failures` names the machine and the code,
  which the list and its counts say in words (`core/failure-text.js`). Rows the answer named that
  have not arrived three seconds after it are `cloud_sessions_incomplete`, and so is a device that
  cannot ask whose four-minute wait ran out. A failure clears once that machine's inventory is whole
  again — which the next refresh pass brings.
- **The open transcript follows.** Recovered rows reach the list like any row, so a changed
  `transcript_signature` or `state` makes exactly one transcript read and an unchanged one none.

`Tests/web-cloud-fleet.mjs` (the `B1`, `F1`, `F3` and `F4` checks) models an idle Mac behind a relay
with nothing to replay — a cold viewer, one that never saw the Mac, a page back from quiesce after a
row changed, a renewal, a renewal over a half-open socket, a Mac that does not answer, an answer
whose rows never arrive, a read-only device before and after the refresh pass, and a page back from
the background that holds the `orch/` snapshot — and `Tests/CloudSessionRowTests.swift` holds the
Mac's refresh pass, snapshot pass, window, waiter bound, the owed `orch/` re-send, a pass that waits
out its window while a scan tombstones a row, and `stop()` in the middle of a pass.

Audited against the same assumption — *the relay's replay brings a (re)connecting page up to date*:
`s/` rows and the inventory (fixed above); the `orch/` snapshot, which carries the descriptor, task
list, schedules, snippets and `cloud_status` and is published only when a record or a notice changes
(re-sent by the same pass); `t/` read answers, including pictures and transcripts (not dependent:
a replayed answer settles only the request it names, and every read is asked fresh); the Schedules
strip and places (not dependent: the strip reads on return and places are asked).

**Known limitation: a Linux executor's rows do not come back after an eviction.** This predates the
skip rule on the Mac and is the same on `main`. The Linux executor
(`Packages/ClawdlineLinux/LinuxDurableCloudRuntime.swift`) publishes its rows only when their digest
changes, lists no `features`, answers no `sessions.snapshot` and has no refresh pass — both are
Mac-only. Its 240-second descriptor heartbeat keeps the machine current but not its rows, so a viewer
that connects after the relay lost them sees none of that machine's Sessions until one of them
changes, and because the machine lists no `features` the page does not know to wait: its list reads
as an ordinary one, empty if nothing else has rows (the `B1` check "…an older Mac is never asked"
pins exactly that for a machine without `features`). Closing it needs a new executor command, the
browser's `commands` gating for it, and its own Linux package tests.

**Commands go through the door they already went through.** `RemoteServerCloudCommandRouter`
converts a verified cloud command back into an in-process request, so authentication,
idempotency, menu safety, image validation and audit stay the local HTTP route's single
implementation. The write gate is the same one: a Mac with `remote_write` off refuses a cloud
command with `cloud_commands_disabled`, exactly as it refuses one from the browser on its own
network. If the command carried a safe bounded request identity, the Mac also publishes that 403
on the existing `action:<request>` channel, and every other preflight refusal — `malformed_command`,
`unknown_command`, `cloud_dispatch_unpinned`, a malformed request-scoped read — does the same.
None of them reaches the command router. A Session command answers on its own Session's channel; a
machine command, and any type this Mac does not know, answers only on `__clawdline_machine__`, so a
malformed body can never name a Session channel. A non-`ctl` envelope is outside this reply
contract except `dispatch`, which answers on the machine channel when it names a request. Every
command type accepts an optional lowercase `request` (design §11.4): `answer`, `key` and a `send`
that carries one are answered on `action:<request>`, and older pages that send none are unchanged.

Before that router can cross the effect point, `CloudCommandLedger` durably reserves the
authenticated `(viewer sender, request id, request digest)` identity. Exact duplicates wait or
replay the durable normalized outcome; a digest mismatch fails closed. The effect begins only after
the in-progress transition is durable, and its normalized status/body is durable before any reply
is offered to the spool. A restart drops never-started reservations and treats recovered
in-progress work as explicitly unknown. Store-originated persist/fsync/rename/recovery failures
propagate through admission and startup; no convenience path catches them into an empty ledger.
The Mac calibrates its real epoch guard only from authenticated server time and re-reads epoch,
paired-device roster, and (for writes) `remoteWrite` after durable reservation at the effect point.
Every device-token fetch carries that server time, and the token rotates every four minutes, so a
fetch only offers it: the sample establishes calibration at startup or after an anomaly invalidated
the last one, and otherwise leaves a ready guard or a running 60-second window alone while still
observing wall rollback, forward jump, boot-id change, continuous reversal and a sample more than
five minutes off. The guard's continuous clock is `CLOCK_MONOTONIC_RAW`, which counts through system
sleep, so waking is not read as a wall jump; the ledger's own continuous deadlines stay on
`DispatchTime` and never meet the guard's readings.
A revocation there durably releases the reservation and performs no effect. Live in-progress
duplicates join the current owner; store failures terminate all waiters with either a retry after
durable release or a typed uncertainty once an effect has started.

Durable outbound logical rows contain channel/identity, byte count, and a SHA-256 content identity,
not plaintext or a base64-equivalent payload copy; content exists only inside the encrypted sealed
frame. Production revalidates complete logical metadata and the sealed frame on load. Settlement
matches the exact authenticated wire channel plus sequence and creates its tombstone in the same
commit. Authenticated `publish_error` is decoded as closed code/field labels and never disconnects
the socket: terminal and unknown codes reject only their exact row, while clock-skew/unavailable/
internal responses terminally reject the old exact bytes and create a newly sealed sequence from
in-memory decryption. Uncorrelated error and unknown authenticated frames are ignored and observed,
not promoted into reconnect reasons. Before socket selection, the signed envelope `ts` must be no
older than 240 seconds, leaving one minute inside Relay's 300-second default. This applies explicitly
to transcript and control rows; `s` and `orch` are latest-value lanes whose older never-sent ready
values are removed in the same admission/live-normalization commit. Never-sent stale rows likewise
have zero terminal retention because no peer can ACK them. Sent and restart-uncertain rows keep
durable tombstones so a late authenticated receipt remains correlatable.

A cancellable maintenance wake burns every expired sent attempt and stale failed reservation without
waiting for new traffic. Transient durable I/O uses bounded exponential delay; structural capacity,
integrity and permission failures become a visible typed persistent state and are not re-encoded at
1 Hz. Stop/detach fences new scheduling, closes the transport to release a production WebSocket send,
then joins the sole drain and deadline tasks. Identity-free observations split
producer wait, reserve, envelope sealing, spool sealing, sent-state persistence, socket write and
receipt wait, and include logical/frame bytes plus ready/window current, process-local peak and
refusal-attempt debt; ready age is wall-clock age, not worker duration.
receipt ingestion keeps at most 256 oldest observations and counts overflow. Authority directories
and files must be private (0700/0600), newly created directory entries are parent-fsynced, new
writes use one fixed candidate name, and startup scans at most 128 directory entries before either
descriptor-validating/removing at most 64 legacy `.writing-*` candidates or failing closed. A
validated server machine identity is injectively encoded rather than used
as a raw path. Linux has no accepted runtime metric label, so it suppresses spool metrics instead
of publishing `runtime=mac`. Atomic spool replacement still writes and fsyncs the complete next
candidate; its pre-replacement safety check now inspects descriptor metadata instead of rereading
all current payload bytes, while preserving the regular-file, single-link, uid, mode, size and inode
checks. A measured 4.13 MiB/1,674-row commit was about 8 ms, so this is adjacent amplification
cleanup, not the explanation for the observed 44-second median producer stall.

**Authenticated command ingress is bounded before execution.** `CloudInboundCommandQueue` is the
single owner of the pending FIFO and its accounting. Production limits are eight retained commands
and 32 MiB of charged variable-width bytes; admission also enforces a 16 MiB post-decrypt plaintext
ceiling matching the Relay content budget. A charge is exactly `plaintext.count +
channel.utf8.count + sender.utf8.count`, not sealed frame bytes or process RSS. Its snapshot reports
all three configured limits, current and peak count and charge, admitted and delivered totals,
invalid drops, per-reason refusals, refused charged bytes and oldest pending wait. These are source
budgets and runtime counters for W6 measurement, not a claim that the values are load-tested.

The receive path authenticates and decrypts first and claims the envelope's `(sender, seq)` in the
replay window next; only then does this owner admit or refuse it. A claim is never given back, so a
capacity refusal is terminal for that authenticated sequence and the same envelope sent again is a
`replay`. Count and aggregate charged-byte overflow return `cloud_ingress_busy` (HTTP-equivalent
status 429, `detail.lane`, `detail.limit`, `detail.retry_after`); a single plaintext over the
ceiling returns the terminal `command_too_large` (413), because the same bytes will never fit. Both
reach the existing encrypted request-scoped command or read answer when the plaintext has the same
safe bounded identity the bridge normally accepts. The refusal path applies the exact machine
channel and remote-write gates first, returning `wrong_machine` or `cloud_commands_disabled`
instead of an impossible retry instruction. No safe identity means no invented reply channel: the
refusal is announced as a notice instead (below). A refused sequence does not fence a later one;
after capacity drains, a new request can be admitted. The existing browser and Relay do not retry
the same envelope bytes, and R-1 does not add such a protocol. Thus a refusal is not an admission,
an admission is not execution, encrypted publication is not relay ACK, and none of them asserts
human observation.

The command FIFO survives WebSocket reconnect and token rotation for the life of the transport; it
is never coalesced with snapshots and an admitted row is never evicted for a newer command. The
bridge's deliberately serial consumer may suspend in routing while receive, ready-generation and
outbound publication actor turns remain independently schedulable. Refusal answers—including
preflight 403 and read-lane busy answers—enter one serial lane capped at eight outstanding items.
Offering never waits for publication; each item has a one-second deadline, and current/peak,
admitted, completed, timed-out, full-drop and cancellation totals expose its debt. A timed-out
publication is cancelled and remains charged until its task exits, so repeated stalls cannot create
unbounded work or block receive-loop ping handling. Shutdown is an explicit lifecycle boundary:
admission then returns typed `finished` without advancing replay or accepted metrics. W5-4 wires
this same transport, spool and ledger into the public Linux daemon: one
`LinuxRelayRuntimeOwner` refreshes protected identity at every ready generation, replays exact sent
frames, settles ACK/`publish_error`, and hands `ctl/<machine>` plaintext to the existing
`LinuxDaemonIngressOwner`. Sender plus relay sequence becomes the durable command idempotency key;
timestamp, roster, revocation and the explicit `cloudCommandsEnabled` write gate are checked again
before any task/artifact mutation and immediately before an effect. Cancellation or policy failure
removes a never-started reservation and performs no host effect. One daemon-lifetime supervisor
strongly retains this sole owner and projects its typed state into health; explicit 401/403 or
revocation responses terminate reconnect and drive the same awaited stop path. The signed Linux
template defaults the write gate closed, and install must name `--cloud-commands-enabled true` to
create or preserve an enabled alpha configuration. Live Relay/GCE and real provider acceptance
remain external gates.

Linux discovery uses the same retained encrypted channel grammar as the Mac. Each row is a stable
`s/<machine>/<percent-encoded-session>` payload, and the complete-set marker is
`s/<machine>/__clawdline_inventory_v1__`. A complete scan publishes changed rows, explicit
`{"deleted":true}` tombstones for previously published ids that disappeared, then the authoritative
marker; an incomplete or over-limit scan publishes nothing. Retained channels may replay in any
order, so the browser realigns each channel independently and uses the marker sequence only as the
deletion barrier for older rows. Every authenticated ready generation replays the complete current
set. A ten-second local observation supplies the bounded refresh after host-side create, send or
close even when the effect did not originate in the browser.

The browser operations accepted by the Linux machine channel are the closed `info`, `places`,
`screen`, `send`, `start`, and `transcript` set. `start` names a configured place id, never a path; an empty assistant means
`claude`, otherwise it is `claude` or `codex`, and a nonempty model is exactly `haiku`, `sonnet` or
`opus`. The versioned durable request carries that model through the existing serialized write
gate and idempotency ledger. Success echoes place, bounded cwd, assistant and model; malformed,
unknown, revoked and gate-refused requests answer the same opaque request id with a typed refusal.
Any other word on the machine reply session is refused as `unknown_command` (400) through the same
gates, answered on `action:<request>` as the Mac's own refusal of an unknown word is; before that
refusal it was dropped without a reply. The descriptor advertises the exact six-word set.
For Linux `send`, only a successful body carries `optimistic_settlement:"action_receipt"`: the
correlated action receipt settles the browser pending indicator because terminal/screen projection
does not guarantee a later user-role transcript row. Refusals never carry that signal. `info`
uses the Mac-compatible full/summary request and reply names but returns only truthful Linux
metadata: the durable task-terminal edge must own the row and the durable create/`taskCreate` receipt's
boot/PID start-token incarnation must still match the live provider process. Missing or changed
incarnation evidence is `503 session_identity_incomplete`, never guessed from reusable `%N`, title,
cwd or tty. Only an exact configured cwd is returned; unavailable quota, usage, Git, model and
deploy facts are omitted or empty. A malformed Info request is `400 malformed_read`.

A newly installed Linux executor has an explicit enrollment step before its first daemon start:
run `ClawdlineLinux cloud-login --config /etc/clawdline/daemon.json` as the configured service
user, approve the printed one-time code at the printed Clawdline URL, and then start the daemon.
The command's first JSON line contains only the public authorization invitation; its final line is
a nonsecret protected-identity receipt. Device codes, machine credentials, signing keys, and
content secrets are never emitted. Re-running it for the same protected identity is idempotent,
while a credential/identity mismatch is a typed failure and never an implicit repair.

The public W0-E contract bytes remain a candidate pinned to commit
`38eb822575e3c309a776a9e3e2874c7062d8fb75`, package tree
`3ee391a4af73f9688510c19106d38ba325227051`, source SHA-256
`47c21a3d096813f940026943004791087ea89d628f6123942dd59c02f71a2f3d`, and package SHA-256
`6ccccea5f05b603fd9a583940f6f7a3fd5a8735ac6d5ce6ef7246620b59b7d6f`.
`authority=candidate` and `cutover_required=true` are executable gates: W5-4 does not perform
cutover, raise a floor, or emit an unaccepted candidate version. Existing accepted v1 envelopes
remain the production wire until a later authority decision.

The 2026-09-13 live calibration is residual cost, not a current deadlock: after GC the observed
spool was about 0.197 MB with `ready=0`, `staleReadyAtRestart=0`, and no durable-reserve failures.
Socket and spool medians were about 202 ms and 323 ms, while changed Session writes remained
11.9–19.5 s. The bounded-row correction prevents repeated latest-value replacement from retaining
one tombstone per update for 600 seconds; it does not claim to explain or resolve that Session-write
latency.

**Every refusal names its layer, its code and the command it refused.** Design
[`cloud-error-transparency.md`](cloud-error-transparency.md) §11 is the wire contract; this is what
the Mac does with it. A reference is the envelope's `(sender, seq)`, which exists before
decryption, plus the plaintext's `request`, `type` and `session` once they can be read safely.

- **Dropped before admission** (`mac_transport`). `CloudTransport` reports each drop to one owner
  with its own code — `envelope_malformed`, `wrong_channel`, `unknown_sender`, `roster_unreadable`
  (the roster read failed, which used to look exactly like an unpaired device), `key_id_mismatch`
  with both key ids, `key_unreadable`, `bad_signature`, `decrypt_failed`, `replay` with
  `highest_seq`, and `replay_window_full`. These replaced a single `reason=invalid`.
- **Refused before routing** (`mac_preflight`), **by the ledger** (`mac_ledger`), **by the route**
  (`mac_route`) and **after execution** (`mac_reply`). A published answer's `error` object gains
  `layer`, `seq` and a `detail` limited to the §11.6 whitelist; `message` stays for old consoles.
  Ledger refusals each carry their own words and code: `command_clock_uncertain` with
  `detail.reason` (the clock guard's own reason, such as `awaiting_server_time` or
  `stability_period_incomplete`) and `detail.clears_in_ms` when a window is counting down;
  `command_roster_unreadable`, `unknown_sender` and `command_writes_disabled` in place of the one
  `command_gate_unavailable` that could not say which.
- **One log line.** Every one of them is written once as
  `cloud: refusal layer=… code=… sender=… seq=… request=… type=… session=… status=… reply=…`,
  with `key_id=`/`expected_key_id=`/`highest_seq=`/`roster_readable=`/`detail.*=` where they apply.
  `reply=` is `published`, `notice`, or `silent:<why>`. The remaining `silent:` lines are an
  admission refused during transport shutdown (`silent:shutdown`), a transport composed without a
  drop or refusal owner (the Linux daemon: `silent:no_status_owner`, `silent:no_reply_owner`), and a
  command dequeued after its bridge stopped (`silent:bridge_stopped`). The old
  `cloud: command refused …`, `CloudTransport dropped inbound envelope … count=…` and
  `cloud: command ingress refused …` lines are gone; counts live in the status snapshot.

**`CloudStatus` is the one place that state is kept** (`Sources/CloudStatus.swift`). Transport state
and token expiry, the clock guard's state, reason and countdown, the key id, roster readability and
paired device ids, drop counts by code with the 20 newest drops, the 50 newest commands with each
Mac-side step (`accepted_at_ms`, `executed_at_ms`, `outcome`, `delivered_at_ms` from the relay
receipt for the reply's spool row, `undeliverable` — `command_answer_undeliverable`,
`read_answer_undeliverable`, `refusal_undeliverable`, `peer_rejected`, `receipt_expired`,
`ready_expired` — and `refusal`), the 20 newest notices, and
reply-side counts (`undeliverable`, `expired_ready`, `expired_receipt`, `lane_dropped`). Counts last
for the process, not the connection, and `counting_since` says when they started. It is a lock, not
an actor, because the transport records drops inline from its receive loop. Reads are not recorded
as command rows unless they are refused, so a polling transcript cannot push commands out of the
ring. It holds codes, ids, sequences, key ids, counts and times, and never a command body, prompt,
transcript text, title or path. Three readers share it: the fixed file
`~/Library/Logs/Clawdline/diagnostics/cloud-status.json` (see [`diagnostics.md`](diagnostics.md)),
the `cloud.status` read on `__clawdline_machine__`, and the notice.

**The notice is how a phone hears about a command it could not get in.** When a drop or an
unanswerable refusal happens, the bridge splices a `cloud_status` digest (§11.2: counts, clock guard,
token expiry, key id, roster readability, the ten newest drops and notices) into the newest
`orch/<machine>` payload it received and republishes it, at most once every five seconds; the
orchestrator snapshot's own bytes are kept and the digest is inserted before its closing brace.
Serialized it is at most 8 KiB, enforced by trimming the two recent lists. Before any orchestrator
payload has arrived it publishes an object holding only `cloud_status`. **`orch/<machine>` is
published whether or not the orchestrator is enabled**: `RemoteServer.cloudTransportBecameReady`
publishes `orchestratorSnapshot()` on every ready generation — every connect and every token
rotation — and that snapshot always carries `snippets`, `at`, `app` and a display-only `machine`
descriptor (`name`, `platform`, and an optional infrastructure `provider`), with `tasks` and
`schedules` only when the store is authoritative. Bridges composed without a status owner (the test
fixtures) record into a private one and publish no notice.

The current Mac producer publishes only `name` and `platform`. The viewer accepts `provider` as a
forward-compatible display field only after a runtime's production metadata model supplies an
explicit bounded value; it never derives AWS from a hostname, machine id, or Linux alone. The
separate Linux runtime correction owns that producer/config contract.

The platform prefix and local-machine fallback are served locale copy, not protocol values. The
opaque id and metadata remain unchanged when the viewer translates `This Mac`, `Mac`, `Linux`, or
`Linux / AWS`; localized presentation can therefore never change command routing.

The descriptor is not routing authority. Its enclosing authenticated `orch/<machine>` channel is
the exact machine identity used by every read and start command. Hosted Session rows may label an
explicit macOS host as Mac or an explicit Linux/AWS host as Linux / AWS; an older snapshot without
that evidence shows its opaque machine id and does not guess from an id prefix. When an account has
more than one machine, the New Session sheet requires one exact machine before it reads that
machine's Projects and assistants. A one-machine account keeps the original direct fast path only
while that machine's newest authenticated orchestrator or Session envelope watermark is no more
than five minutes old. Retained older/unknown routes remain visible as offline or status out of
date: they are never auto-selected, but an explicit press may issue a bounded Project probe because
the opaque authenticated route remains known. A fresh Session row/inventory envelope refreshes the
open picker even when an idle machine has no task change. The start reply is watched as the exact `(machine, session)`
pair, so equal terminal ids from two machines cannot open or wedge the wrong row.

**`diagnostics.report` is the Cloud door to the report files.** The command (§11.5) is read-level,
like the HTTP route: it is not behind `remote_write`. `RemoteServerCloudCommandRouter` answers it
with `CloudDiagnosticsReportRoute`, which calls the same `DiagnosticReport.save`, audits
`diagnostics.report`, records the envelope sender as `written_by` and answers the route's own body
or its refusal codes with `layer=mac_route`.

**The replay window accepts the same `(sender, seq)` at most once per process.** Each sender has a
highest sequence and a 1,024-position bitmap below it (design §11.8), so a second tab of the same
device, whose sequences arrive out of order, is no longer refused. Anything older than the window
cannot be decided and is refused as `replay`. The window belongs to the process, not the transport:
`CloudTransport.production` shares `CloudInboundReplayWindow.process`, so a bridge rebuilt after
sign-out, retry or an identity change cannot accept an envelope its predecessor already accepted. A
new sender beyond 4,096 tracked is refused rather than tracked by evicting another. The 300-second
envelope deadline and the ledger's request idempotency are separate owners and are not what makes
this safe.

**The `orch/` snapshot carries machine-scoped state plus display metadata.**
`machine` is the bounded human-facing name and platform described above. It is encrypted with the
rest of the snapshot and never replaces the channel id in an authorization or routing decision.
The other fields include three things, two of which were added because their absence
was invisible. `RemoteServer.orchestratorSnapshot()` is the one body both publishers send — the
local `orchestrator` event and the cloud envelope — and it holds `tasks`, `schedules` and `app`.
`schedules` is there rather than behind a request because the viewer reads that list on a
one-minute lane and a request is a person waiting; measured on one Mac it is 453 bytes beside
1,056,958 for the task list next to it. `app` is `version`, `build` and `protocol` out of
`/v1/health` — nothing about permissions — and it is the only reading the "this page is behind"
banner can have out here, because on the direct path that comparison comes from asking health on
every reconnect and the relay's `ready` frame is the *relay's* and has never heard of a build.

Commit `eaa20bbc` reduced only this Cloud `orchestratorSnapshot()` projection. It did not change the
local authenticated `GET /v1/orchestrator/tasks` route: that endpoint intentionally still returns
the complete `Orchestrator.records()` representation (about 3.69 MiB in the cited live reading).
The projected Cloud bytes and the complete local endpoint are different contracts, not before/after
measurements of one surface.

**An empty list and no list are different answers.** `CloudClient.schedules()` resolves an empty
inventory and refuses an unknown one — `cloud_read_unavailable` before any snapshot has arrived,
`cloud_schedules_unpublished` for a Mac whose build predates the field — because
`net/schedules.js` draws whatever it is handed, so resolving `[]` would be the page asserting on
the Mac's behalf that there is nothing scheduled. A refusal draws nothing and keeps the last
truthful list; a real empty answer still draws. An account whose every machine cannot have
schedules — only Linux executors — resolves an empty list, because that is a fact.

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

**Which machine a request goes to.** An account can hold a Mac and an enrolled Linux executor, and
the executor answers `info`, `places`, `screen`, `send`, `start`, and `transcript`.
`CloudClient._machineImplements(machine, word)` is
the one answer to "can this machine answer this word" — from, strongest first, the machine's own
`unknown_command` to that word, a descriptor `commands` list, a `linux` or other non-Mac platform,
and evidence of a Mac (a `macos`/`darwin` descriptor, or `cloud_status`) — and every route uses it.
A machine whose descriptor has not arrived answers `places` and `start`, which every platform
implements. A word a machine refused is not sent to it again until it publishes a different app
build, its first descriptor, or a different `commands` list:

- A request that names its machine — a Session, a Project route, a schedule, a Board link, an
  explicit machine — goes to that machine and nowhere else, and is refused before the wire as
  `cloud_machine_unsupported` (`cloud_feature_unavailable` for a Mac that refused the word) when that
  machine is known not to implement the word.
- A request with no machine picks with the rules dictation's `voiceHost` was written with: the one
  capable machine; else the one with a current inventory; else a typed refusal —
  `cloud_machine_ambiguous`; `machine_pairing_required` when the only machines that could provide
  the feature are ones this browser is not paired with; or `cloud_machine_unsupported` when no
  machine on the account has the feature. For any other word, a machine whose descriptor has not
  arrived counts only when no machine is evidently a Mac and none is known to answer. Push is strict: one Mac or `cloud_machine_ambiguous`, never the
  fresher of two, because the key, the subscription and its removal must reach the same Mac.
- A read that spans machines (`places`, `schedules({ fresh })`, `snippets()` with no Session) asks
  only capable machines, each on its own under its read timeout. One that refuses, fails or stays
  silent is named in `unanswered` beside the others' rows instead of discarding them; one that
  cannot have the feature contributes nothing; one not asked because its descriptor has not
  arrived is named in `unconfirmed`. The call rejects only when every machine asked failed. A caller
  never reads such a list as complete: the Projects lists (Projects, "say what to start", the
  schedule form and its history) say in one line which machine did not answer, the schedules strip
  keeps what answered and asks only the silent machines again — each on its own, at most once every
  two minutes, and never a machine this browser is not paired with — and a Board conversation whose
  machine did not answer fails with that machine's failure rather than "the Project is not there".
  Rows a Mac already published stay after it refuses the read that refreshes them.
- A machine that refuses a word it does not implement cannot know whether the page waits on
  `read:<request>` or `action:<request>`; both the Mac and the Linux executor answer `action:`, and
  a refusal — never a body — on the machine reply channel settles either spelling of the same
  request id.

`Tests/web-cloud-fleet.mjs` drives a Mac + Linux account with the executor silent on every other
word: Mac-only features still answer with the Mac's data within a bound, while Linux Session
Info/transcript/screen/send and machine places/start route to the Linux executor that advertised them.

**And `send_prompt` gates one read as well, which is the relay's rule rather than this app's.**
PROTOCOL §12 says publishing to `ctl/` needs that capability *in either class*, and a read has to
ask on `ctl/` because it is the only channel a viewer may publish on at all. So a device
downgraded to read-only can see every session row — after a reconnection, within the Mac's
three-minute refresh pass, since it cannot ask for the rows either (*A device that cannot ask waits
for the refresh pass*) — and cannot ask for the messages inside one. It is told
`cloud_read_needs_send_prompt` rather than left waiting; widening it is a relay decision.

**Pairing.** Covered in its own section below.

**Reconnect.** `keepConnected` re-acquires the device token and restarts the socket with
exponential backoff and jitter, and treats a refusal as terminal for the same reason the Mac
does. Outbound sequences use the same reserve-ahead discipline as the Mac, in `localStorage`.

- **No reconnect runs without a wait.** A socket that closes waits at least the initial backoff
  (250 ms, jittered) before the next connect, and the wait doubles to 30 s. It returns to the initial
  wait only after a connection has stayed up for 60 s, so a relay that accepts and then closes is met
  with a growing wait rather than a loop. Sixteen attempts with no connection staying up 60 s between
  them end in `terminal_error` with `reason: "retries_exhausted"`, and the door offers the press that
  starts again. None of these counts means "in a row": only a connection that stayed up 60 s, or a
  parked loop starting again, clears them. A loop out of attempts holds no socket and no timer, but
  it keeps listening to the page: shown, restored from the back-forward cache or back online, it
  starts over with its counts cleared — except after 4403, below.
- **Relay closes are classified.** 4400/4413 (this page's protocol) stop at once, before or after
  `ready`. 4403 (`forbidden`) is a revoked device, and also every device of an account whose
  revocation set the relay has saturated, which clears later and closes with the same code and words
  (`relay/src/lib/entitlements.ts`). A device that really is revoked does not reach the relay again:
  its next attempt asks the API for its session first, the API refuses a cookie naming a revoked
  device with 401, and the loop ends at `sign_in` (a revocation landing between that request and the
  device list or token mint is refused there as `revoked`). So a 4403 waits the longest backoff and
  the loop parks after three of them. Because a 4403 that no retry changes would otherwise cost three
  refused attempts every time the page comes back, a loop parked by 4403 starts again on a return
  with one attempt, and after three such parks with no connection staying up 60 s in between a return
  no longer starts it at all (`MAXIMUM_FORBIDDEN_PARKS`): it lets go of the page, and only the door's
  press or a reload tries again — at most 3 + 1 + 1 refused attempts per loop. A saturated revocation
  set that clears is met by the next return, and a connection that stays up gives the parks back.
  4429 (`rate_limited`, `over_capacity`) waits the longest backoff and counts against a bound of its
  own, forty attempts, rather than the sixteen a dropping network gets. Everything else retries.
- **Renewal is timed on the relay's clock.** The relay's `ready` frame carries `connected_at` and
  `token_expires_at`; their difference is the token's lifetime as a duration, so a phone whose clock
  runs ahead does not renew at once. Without them the API's `expires_at` is read against this
  device's clock. Either way the renewal never follows its connect sooner than 60 s, and the
  renewal timer is cleared when its connection ends for any other reason. The replacement takes over
  from the old socket only if that socket answers a relay ping sent as the replacement starts
  connecting (*Who asks, and when*); one that does not is replaced all the same, and its successor
  recovers the rows.
- **A hidden page does no periodic work.** Hidden for 60 s, the socket is retired
  (`cloud_reconnecting` for any read still waiting) and nothing connects, renews or retries.
  A person's own `action:` still waiting — a send, a keypress — holds the socket for up to another
  60 s. Visible again, a `pageshow` from the back-forward cache, `online`, or `api.revalidate("visible")`
  from `main.js` connects at once. The Session rows the page already had are carried into the new
  client, and the relay replays whatever its memory still holds, which after an eviction is nothing:
  the new client asks each machine for its rows (*A page that reconnects asks for the rows*), so a row
  that changed while the page was away comes back without waiting for it to change again; a device
  that cannot ask shows the machine as sending until the Mac's next refresh pass, at most three
  minutes away. A page shown
  after a hide longer than the grace whose socket still says `ready` — a phone that froze the page
  before its timer ran — gets a replacement socket the same way a token renewal does. Until
  `main.js` installs that replacement, the page still holds the retired client: a read or command
  asked of it then — a notification tap opening its Session — waits, within the read bound, and runs
  on the client that resumed from it. While the page is still hidden — a notification tap posts its
  `navigate` before `focus()` shows the page — the read waits up to five seconds for the page to be
  shown, then the rest of its read bound; with no connection on its way (still hidden after that,
  the loop has ended, or 4403 parked it for good) it is refused as before. A page shown after being frozen past the grace
  replaces its socket like a renewal, but is not taken as one: nothing proves that socket heard what
  was published meanwhile, so the replacement asks for the rows and says `connecting` first. While
  `navigator.onLine` is `false` nothing connects until `online`. The schedules strip stops its minute
  lane while hidden and reads once on return if it missed a tick.
- **Closing the socket rather than only pausing the lanes** is deliberate: an open socket keeps
  receiving every Mac publication, and each one is verified, decrypted and applied in the
  background. The cost is one reconnect, with its realignment, when a page returns after more than
  a minute away.

**What a client does once, not per envelope or per read.**

- A sender key the key store answered is kept for that client, and so is "no key" and a pairing
  the store holds only part of (`machine_key_incomplete`). A verification that fails under a
  remembered key forgets it; a pairing completed in this page (`forgetMachinePairingAnswer`), or in
  another tab of this browser (a `paired` message on the tab channel), forgets the negative answers;
  a token renewal starts a client that asks again.
- The machine descriptor table is written to `localStorage` only when a descriptor or build changed.
- A `t/` channel already held on the socket is not subscribed again. The relay holds at most eight
  subscriptions per connection and refuses a larger frame whole, so a channel no read is waiting on
  is let go when the next subscription is made and it has been idle for two minutes, or when that
  subscription needs its room — there is no timer, so an idle channel stays held, and still receives
  other devices' answers on it, until something else is subscribed. When all eight are held by reads
  still waiting, a read that needs a ninth is refused at once as `browser · cloud_read_busy`
  (retryable, nothing published) instead of being sent in a frame the relay would refuse whole.
- Subscribing makes the relay replay the channel's last envelope with `realign: true`. That replay
  settles only a request it names — `read:<request>`, `action:<request>` — or `image.<id>`. A
  replayed `transcript`, `info` or `git` answer may be minutes old and another device's, and it used
  to settle the read that caused the subscribe before the Mac was asked.
- `machine_offline` from the relay is remembered per machine for 5 s, doubling while it stays
  offline, to at most 5 minutes. Within that window a read, action or fan-out read aimed at the machine
  is refused here with the same `relay · machine_offline`, retryable, with no sequence spent and no
  `ref`. Any live envelope from the machine, or a `delivered` ack to it, ends the window at once; a
  realigned (retained) envelope does not.
- A picture is kept in memory by machine and artifact id after it is read (at most 64 pictures and
  32 MiB; a picture over 8 MiB is not kept). An id names one set of bytes for as long as it exists
  (`SessionImageArtifactStore` mints a fresh UUID per import), so drawing a transcript again reads
  no picture twice. Refusals are not kept.

`Tests/web-cloud-boot.mjs` drives the reconnect loop with a fake clock, document and navigator.
`Tests/web-cloud-fleet.mjs` and `Tests/web-cloud-failures.mjs` drive the client-side memory
through the relay-in-a-box suites, and `Tests/web-schedules.mjs` drives the strip's cache and lane.

**Every failure names its layer, code and ref** ([`cloud-error-transparency.md`](cloud-error-transparency.md)
§11 is the wire contract). `net/cloud-failure.js` turns every rejection into one shape —
`{layer, code, ref: {sender, seq, request}, status, retryable, detail}` — whatever said it: a Mac
reply `error` (a Mac that names no layer is recorded as `mac`), a relay `ack` with
`machine_offline` or a `publish_error` (matched to the request by `(ch, seq)` and settled at once),
the relay `error` frame and the WebSocket close code (both kept in connection diagnostics; a
non-closing frame never renames a later network close), or this page itself (`browser`). `detail` keeps only the
§11.6 whitelist, and `retryable` comes from a table here — an unknown code is terminal. A token
renewal retires reads as `cloud_reconnecting`; an already-written keypress is instead
`delivery_unconfirmed` and is not retryable. No public `CloudClient` method throws
synchronously; `Tests/web-cloud-failures.mjs` calls all of them in three broken states.

**The words come from the code.** Every UI error site goes through `core/failure-text.js`:
a known code gets its sentence, an unknown one the screen's fallback, and each line ends in
`code · ref` (`f052dcb8·1234`). No screen shows an error's `message`; the same suite scans
`js/input`, `js/view` and `js/session` for it. A failure line opens the Cloud status sheet.

**What the Mac says when it cannot answer.** A Mac that publishes `cloud_status` in its
`orch/<machine>` snapshot (§11.2) is marked capable. A `recent_drops` or `recent_notices` entry
naming this device and a sequence still waiting settles that request in the Mac's own layer and
code; a `replay` of a sequence this tab sent says the device is open in another tab. Only a capable
Mac is sent `request` on `answer` and `key` (§11.4); `send` already requires one on every supported
Mac. For a capable Mac the browser keeps waiting after the relay ack and settles a keypress only
from the Mac's `action:<request>` result, so a preflight refusal is visible. Older Macs retain the
ack-only answer/key path because their exact field check would reject the extra request. A capable
Mac is also asked `cloud.status` when a read times out (so an executed command whose reply was lost
says so), and asked for the status sheet and diagnostics report.

**This browser checks its own key id.** Every Mac envelope's `key_id` is compared with the key this
browser seals with; a difference is `key_id_drift` with both ids on the status sheet, whose repair
button raises the same encryption door a key error does. Other tabs of the same device are found
over `BroadcastChannel`.

**The Cloud status sheet** (`input/cloud-status.js`, data in `view/cloud-status.js`) opens from any
failure line — scrolled to that ref — and from the Settings row that appears only in Cloud mode. It
joins the browser's bounded trail of its last 50 commands (sequence, type, Mac, step times, refusal;
never contents) with each capable Mac's `cloud.status` snapshot, read once when the sheet opens,
plus the Mac's clock guard, token expiry, key id and drop counts. A failed read is a typed line on
the sheet. `?mock=1&cloud-status=line|open` draws a fixed failure and sheet for layout checks.

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
fallback. A string change participates in the build stamp exactly like a JavaScript change. The
catalog is one key per line, sorted, and `tools/check-web-strings.py` holds it to what a Mac sends;
a branch, which cannot export from a build of its own source, adds the key by hand with the
`TraditionalChinese` literal the guard prints.

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
  VAPID key through an encrypted request/reply. A Linux executor on the same account does not count;
  an account showing more than one Mac is refused with `cloud_machine_ambiguous` instead of
  registering a browser subscription against whichever machine happened to publish first. Turning
  notifications off removes this browser's subscription either way and says so when the Mac could
  not be told.
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
