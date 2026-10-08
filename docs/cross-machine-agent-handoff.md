# Cross-machine Agent messages and Session handoff

The public daemon uses a dedicated machine peer rail. A Cloud account, viewer
pairing, or the account content key does not authorize Agent work between
machines. A person compares each machine's Ed25519 signing fingerprint on
that machine, starts a source offer, accepts it on the target, and pins the
signed transcript on both sides. The target then grants `message`, `handoff`,
or both for exact source and target Session executions. Either machine may
revoke the pair or grant. A failed Cloud revocation leaves the local denial in
force; a failed Cloud authorization read closes admission.

The private control plane contract is `docs/PEER-ROUTING.md` in the Cloud
service. It supplies machine-authenticated pair and grant APIs and the
`peer_publish` / `peer_envelope` / `peer_ack` WebSocket frames. The daemon
derives a pair-specific X25519/HKDF/AES-GCM key, signs the canonical request
and ciphertext with its machine Ed25519 key, and opens an incoming envelope
only with the locally pinned source key. The relay's `source_public_key` is a
lookup hint; it cannot create a Principal. `ctl/` and `ho/` remain viewer and
account channels and are never a fallback for peer work.

## Fixed target and authorization

Each request carries a stable ID, operation kind, grant ID, body SHA-256,
and both endpoints as `machine_id`, `session_id`, and a 32-character lowercase
hex `execution_generation`. The body is valid UTF-8 text so the durable JSON
inbox preserves the bytes covered by its digest. A source checks its own current execution before
signing. The target rechecks its local pair and grant, the Cloud grant, the
write switch and its current execution before claiming a durable receipt and
again before the inbox effect. A changed generation or revoked grant refuses
the work even if an older ciphertext is still valid.

The target stores received work in a structured peer inbox, not as a chat
message. `message` and `handoff` have separate grant bits and retain the
source and target identities. An Agent reads its target Session inbox with
`clawdline cloud peer inbox <session> <generation> [before-request-id]` or the fixed-target
Console detail. Reading through the Console is not treated as proof that an
Agent observed or acknowledged the work.

## Product entries

The Cloud Console's all-machine Session detail names a fixed target and asks
the person to choose a distinct current source Session. It offers pair start,
accept and source sync with fingerprint entry; target grant and source sync;
local-first revoke; message or handoff send; relay receipt check; and target
inbox read. The selected Console machine is never used as an implicit source
or destination. The same actions are available from `clawdline cloud peer`.
The CLI `send` requires the source and target IDs and generations on the
command line even though the grant stores them, so a changed choice cannot
silently retarget a request. `clawdline cloud peer outbox <request-id>` reads
the saved relay receipt; a successful socket write alone leaves relay
acceptance unknown until `peer_ack` arrives.
Run `clawdline cloud peer fingerprint` on each machine to compare its signing
fingerprint from that machine before entering the opposite fingerprint in the
pair flow.

The local HTTP routes are:

- `POST /v1/cloud/peer/control` for an explicit pair or grant action;
- `POST /v1/cloud/peer/send` for one fixed, content-digested request;
- `GET /v1/cloud/peer/outbox/:id` for source relay evidence;
- `GET /v1/cloud/peer/inbox?machine_id=&session_id=&execution_generation=&before=`
  for structured target work. Each page has at most one item, an optional
  `next_before` cursor, and a `body_base64` field so JSON escaping cannot
  exceed the Cloud frame bound. A cursor from another execution is refused.

The hosted viewer reaches those routes only through advertised `peer-control`,
`peer-send`, `peer-outbox` and `peer-inbox` Cloud operations on the named
machine. The content-bearing inbox read uses the separate `r/` rail, a fresh
`read_content_v1` descriptor, `read_transcript` authority, and a pinned
execution generation; it is refused on the ordinary `ctl/` read rail. The
viewer must already be paired with that machine and have send permission for
changes. That viewer permission is authority to ask the
machine to act; the peer Principal is established separately by the pinned
machine signature, exact grant, Cloud authorization and generation checks.

## Receipts and unknown outcomes

The receiver claims the daemon's persistent `request_receipts` record under
scope `cloud.agent_handoff` before the inbox effect. The actor is the
source/target machine and Session tuple; the request digest binds generations,
grant, operation and body digest. Exact repeats replay a completed receipt;
changed repeats conflict; orphaned claims report unknown and never rerun the
effect. The source stores its request and relay evidence in a separate local
outbox. `peer_ack` can say `delivered`, `machine_offline` or `unknown` about
relay socket delivery only. `machine_offline` does not queue work for later.

The current private wire has no target-machine-to-source execution receipt
frame. A source result therefore leaves target execution, Session delivery,
Agent observation and Agent acknowledgement `unknown`. The target's durable
receipt and inbox provide local execution and Session delivery evidence; no
Console inbox read fills Agent observation or acknowledgement. The inbox API
returns the saved target receipt beside each item, so a person can distinguish
machine execution and Session delivery from an Agent's later observation. A timeout or
unknown relay result must be checked by the original request ID, not retried
with a new ID.

## Verification boundary

The public tests cover request validation, E2EE bytes, signed pair pins,
local revocation, generation refusal and durable receipt replay. A production
claim also requires a deployed private peer rail and a two-machine end-to-end
check; a local test or a relay `delivered` ack cannot replace that check.

## Isolated two-machine acceptance run

Use two disposable machines or VMs with separate process namespaces, `A`
(source) and `B` (target), connected to a dedicated test Cloud account. Use
the candidate public daemon and private peer API/relay on both. Never copy a
machine state directory or signing key to the other machine, and do not use a
person's existing Sessions. Keep an absolute, private `CLAWDLINE_NEXT_DIR` on
each machine for the whole run, including daemon restarts; choose its path
once and record it with the test evidence. Set `CLAWDLINE_NEXT_PORT` to an
unused loopback port on each machine. Record each `serve` PID and stop only
that PID. The existing production daemon and port 7727 are outside this run.

1. On both machines, set those two environment variables and start one
   disposable `tmux new-session -d -s peer-e2e` pane. Run `clawdline sessions
   --json` against each isolated daemon after it starts. Continue only when
   both panes appear as current Sessions with the same visible `peer-e2e`
   name but separate machine and Session IDs and nonempty, 32-character
   lowercase `execution_generation` values. Keep each pane alive for the
   exchange. This checks that a shared display name does not select a target.
2. Run `clawdline cloud login --name peer-e2e-source --wait 10m` on A and
   `clawdline cloud login --name peer-e2e-target --wait 10m` on B, using their
   own persistent directories. **A person must approve each device code in
   the account browser.** Run `clawdline cloud on` and `clawdline cloud
   commands on` on both, restart only the two fixture daemons, and wait for
   `clawdline cloud status` to report connected. Record the two machine IDs.
   For Console checks, pair the test viewer to both machines and give it
   `send` and `read_transcript` as separate explicit permissions; the
   browser pairing and grant step also needs the person. Confirm each
   descriptor advertises `machine.read_content_v1` before reading an inbox.
3. Read `clawdline cloud peer fingerprint` on A and B at their own consoles.
   Compare each displayed value over that independent view, record both
   values, then on A run `clawdline cloud peer start <B-machine-id>
   <B-fingerprint>`. Record its pair ID. On B run `clawdline cloud peer
   accept <pair-id> <A-fingerprint>`; on A run `clawdline cloud peer sync
   <pair-id> <B-fingerprint>`. Refuse to continue if either side shows a
   different fingerprint or the signed transcript cannot be pinned.
4. On B run `clawdline cloud peer grant <pair-id> <A-session-id>
   <A-generation> <B-session-id> <B-generation> both`. Record the grant ID.
   On A run `clawdline cloud peer grant-sync <grant-id>`. Restart both fixture
   daemons without changing their state directories, verify that their
   fingerprints and Session generations are still current, and use the same
   pair and grant. This tests durable identity, pins and grants.
5. On A run `clawdline cloud peer send <grant-id> <A-session-id>
   <A-generation> <B-machine-id> <B-session-id> <B-generation> message
   fixture-message-1` and then the same command with `handoff
   fixture-handoff-1`. Record both request IDs. Read each with `clawdline
   cloud peer outbox <request-id>` on A; this reports relay acceptance and
   socket delivery only. Read `clawdline cloud peer inbox <B-session-id>
   <B-generation>` on B and follow any printed `before-request-id` command
   until both items appear. Check their exact source and target triples, kind,
   body and durable target receipt. The target receipt may prove machine
   execution and Session delivery; Agent observation and acknowledgement
   remain unknown. Repeat the read in the Console's fixed B Session detail
   using its `r/` rail, and confirm an unpaired or `read_transcript`-denied
   viewer cannot read it.
6. On B run `clawdline cloud peer revoke-grant <grant-id>`. Attempt another
   send from A with the old grant and a fresh body. It must be refused before
   a new B inbox item appears. Restore a fresh grant only if needed to test
   generation replacement; then end B's fixture pane and create another
   same-named pane. A request pinned to the former B Session or generation
   must be refused, without a new inbox item. A relay `delivered` status alone
   does not override either denial. Finally revoke the pair on B and A with
   `clawdline cloud peer revoke-pair <pair-id>` and verify that no old grant
   or ciphertext can reestablish admission.

Capture redacted machine IDs, Session IDs, generations, fingerprint comparison
result, pair/grant IDs, request IDs, both local receipts, typed refusals and
the Console read gate. Keep credentials, keys, message bodies from real work
and browser approval URLs out of the report. For cleanup, turn Cloud off in
both fixture configurations, stop only the recorded fixture daemon PIDs,
kill only the two `peer-e2e` tmux sessions, revoke both test machines and the
test viewer in the Cloud account, and remove the two dedicated state
directories only after the redacted evidence is saved. A browser approval
that was not completed is a blocked preflight, not a failed peer exchange.
