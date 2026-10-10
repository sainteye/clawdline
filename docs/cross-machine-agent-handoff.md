# Cross-machine Agent messages and Session handoff

## Person-facing permission

The hosted Console's **Cross-machine access** page lists each unordered pair of
machines. One switch controls whether their current Sessions may exchange Agent
messages and hand off work in **both directions**. There is no Session picker,
operation-scope picker, grant ID, or fingerprint entry in this flow. Turning a
switch on asks the two online machines, already verified and writable by this
browser, to create two directed signed pairs. Each machine independently
reports its signing fingerprint through the verified viewer channel; the
other's reported fingerprint is checked against the Cloud machine roster and
included in its signed pair transcript. The Console marks a switch on only
after fresh status reads from both machines show both directions pinned at
both ends. A partial result is shown as incomplete. If either machine cannot
be read, the page shows an unknown state instead of an off switch and names
the machine and reason; it never infers revocation from missing status.

Turning a switch off revokes every pair between the two machines on both
local stores and in Cloud. If either machine is offline, unpaired with this
browser, unreadable, or write-disabled, the switch cannot be changed. The
page does not infer a negative authority from an unreadable machine. Local
pins may remain after another machine revokes the Cloud pair; send, relay and
receive authorization still fail closed on the Cloud revocation. Refresh
reconciles the visible local state. A partially completed setup can be
continued or cleared from the page.

A directed pair is the authority for either operation and any **current**
source and target Session on its named machines. It is not a general remote
shell or account-wide command permission. Each message still names the exact
source and target machine, Session, and execution generation, and both
daemons validate their own current execution. New and restarted Sessions work
without a new grant. The Cloud pair lasts at most ten years, expires on a
signing-key change, and can be revoked immediately; it never becomes trusted
only because two machines share an account. Legacy per-Session grants remain
readable for existing in-flight requests but the Console does not create new
ones.

## Trust and routing

The two machines sign and pin the pairing transcript with their Ed25519
identity keys. The source and target exchange X25519 keys and encrypt peer
content end to end with pair-specific HKDF/AES-GCM material. Cloud checks
machine-authenticated pair metadata and routes ciphertext; it cannot read the
Agent message or handoff body. The relay's `source_public_key` is only a
lookup hint: the receiver builds its principal from its locally pinned
signature key. Cloud authorization is checked at send, relay delivery, and
receiver admission, so a revoked pair cannot be reactivated by replaying old
ciphertext or a stale browser status. The receiver's local write switch and
current execution are separate requirements. Offline machines do not queue
mutating work for later.

A request's wire field remains `grant_id` for compatibility. New machine-level
requests put the directed **pair ID** there; the sender and receiver resolve it
as a locally pinned pair before attempting a legacy grant lookup. Cloud
resolves the same field as a pair ID when no legacy grant has that ID. The
signed request still binds operation kind, both complete endpoints, request
ID and body SHA-256. The receipt identity binds the complete request and its
body digest; resends cannot silently change the target or operation.

`peer-control` `status` returns each machine's local fingerprint and bounded
`pairs` and `grants` arrays. The current Console uses pairs for the switches;
old grants are returned for compatibility. `start`, `accept`, `sync`, and
`revoke_pair` remain machine-authenticated control actions. The Console
orchestrates them automatically for both directions after reading each
machine through its verified viewer channel. No signing key, private key,
message body or inbox content is present in the status reply. Pair IDs are
implementation details and are never required in the normal page.

The local HTTP routes are `POST /v1/cloud/peer/control`, `POST
/v1/cloud/peer/send`, `GET /v1/cloud/peer/outbox/:id`, and `GET
/v1/cloud/peer/inbox?machine_id=&session_id=&execution_generation=&before=`.
The hosted viewer reaches them through the matching advertised Cloud
operations on the explicitly named machine. Inbox content uses the separate
content-read rail with `read_transcript` authority and a pinned target
execution. Viewer pairing and send permission allow the person to ask a
machine to change peer access; the machine-to-machine principal is proved
separately by the signed pair and fresh Cloud authorization.

## Receipts and acceptance

The receiver claims a durable request receipt before the inbox effect, then
rechecks authority at the effect boundary. Exact repeats replay a completed
receipt; conflicting repeats are refused; orphaned claims remain unknown.
The source outbox distinguishes socket submission and relay delivery from
target execution. `peer_ack` can report `delivered`, `machine_offline`, or
`unknown` about relay delivery; none proves that a target Agent observed or
acknowledged the work. The target inbox and durable receipt provide local
execution and Session delivery evidence.

A two-machine production acceptance run needs two isolated machine identities
under a dedicated Cloud account, each with its own persistent state directory,
current Session and verified browser pairing. Turn on their connection in the
hosted page and confirm both machines report both directed pairs. Send a
message and a handoff from A to B, then reverse the direction using different
Sessions. Verify both target inboxes and their durable receipts. Replace a
target execution and check that an old targeted request is refused. Turn the
switch off, verify local and Cloud revocation, then attempt a fresh send in
both directions; neither may create an inbox item. Keep the fixture daemons
and port 7727 separate, stop only their recorded PIDs, and do not mistake a
relay `delivered` ack or a healthy API for completed target execution. The
private API and relay protocol is specified in the Cloud repository's
`docs/PEER-ROUTING.md`.
