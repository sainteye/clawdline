# Dedicated Bot Cloud source composition

The source uses the frozen Bot v1 envelope, original signed pair, and independent
human-approved Bot consent. Session grants and viewer bearers confer no Bot
authority. The external assistant gateway remains unavailable.

The normal Server constructs its network service before enabling Host when real
stored machine identity and signing keys are readable. Its repository is lazy:
default-off startup creates no network tables, opens no provider and sends no
new Cloud requests. `Server.StartBotNetwork` binds the existing authenticated
machine Transport. Its lifecycle owner must stop and await the worker before
replacing socket ownership.

The normal Cloud Link retains Transport ownership and exposes its dedicated Bot
seam to `startCloudLine`. The daemon attaches the Bot worker to the same line
context, cancels and awaits it before replacing a link, and includes it in bounded
shutdown. A source that is off still attaches no Bot rail until ordinary opt-in.
This source wiring is not a production deployment receipt.

## Machine authority and local human management

`cloud.BotNetworkClient` uses the stored machine credential/API origin, refuses
redirects, bounds responses, and validates closed complete JSON, account, machine,
epoch, expiry and captures. Every admission, effect and publication calls the live
machine `/v1/bot-network/authorize` endpoint with its complete immutable header.
Positive decisions are never cached. `CloudAuthority` also rechecks current local
identity, signing key and both keys/signatures of the original pinned pair. Pair
private keys stay in the existing custody store and are selected by pair ID.

The first management path is local-owner-only:

```sh
clawdline project-assistants network-consent-sync CONSENT_ID
```

It sends `POST /v1/bot-network/consents/CONSENT_ID/sync`, with no body or query.
The source reads an already human-approved private consent, compares exact private
pair metadata with the independent local transcript, and copies only that record.
It cannot create consent, install a supplied arbitrary grant or pair a machine.
Machine/native tokens, LAN/read-only devices and Cloud forwarding are refused.
Identical reads are harmless. Newer API epochs may skip missed syncs, while stable
identities and retained revocation cannot move or resurrect. Source-consent
metadata remains insufficient authority without the live authorize decision.

This local route arrives at API **86**, after API 85 main-chat reactions.
The combined tree retains Bot schema 11 and the reaction operations unchanged.
No Cloud operation or source decoder is added; the old closed decoder stays intact.
The production consent-picker remains separate work.

## Dedicated socket, durable correlation and capacity

After the ordinary challenge, the source independently negotiates Bot v1. Only
the current negotiated connection generation may publish or enqueue Bot frames.
Reader callbacks copy into a separate bounded lane and make no API/native calls.
Full ingress seals Bot readiness and counts refusals without closing the ordinary
Session/viewer socket.

Before signing a target transport ACK, the source verifies the encrypted frame,
exact delivery metadata and current authority, then retains header/frame digests,
delivery ID and local connection identity in SQLite. Outgoing attempts are retained
before writes. Relay accepted/delivered facts never claim native completion,
stored result or Agent observation. Native execution still uses the existing
durable claim and rechecks authority at each effect/publication boundary.

Disconnect, ambiguous write, timeout and retained unknown effects never trigger
automatic resend. A new seal under a retained attempt ID conflicts. Correlated
reads/replies retain original captures after native generation advance. Fresh
directory observations use fresh metadata IDs and authenticated complete current
target captures without moving stable consent.

Only actual offered, writable Bot fences are advertised with short capability
leases. Changed captures are noticed on the local worker pass. Lease CAS revision
and pending write identity are durable. A live authenticated read can reconcile
an ambiguous write. If that remote evidence expires, the current private API hides
its revision; the source remains typed unavailable rather than guessing recovery.
A private recovery metadata extension requires separate Root review.

Transport receipt, pending-call, frame, endpoint, lease and refresh bounds are
registered. Authenticated capacity/diagnostic endpoints measure actual occupancy.
Unknown transport records are never evicted to admit new work.

## Verification and remaining acceptance

`TestBotCloudNormalSourceSpineAndRevocation` uses two ordinary Server/Host
constructions and the ordinary opt-in path, real TCP API calls, negotiated
WebSockets, signed ACKs, SQL and one durable Host effect with a correlated stored
reply after generation advance in both original pair orientations. Its default
provider and private API/Relay are explicitly simulated. This is source evidence,
not Fastify/Mongo/workerd acceptance.

Set `CLAWDLINE_BOT_NETWORK_NATIVE_PROOF=1` to run its owned
`TestBotCloudOwnedACP` subprocess through the real ACP adapter, with zero model
requests. The child's sandbox retained an execution/cleanup unknown. Root's first
native run reached the adapter but failed its identity check: the synthetic
initialize version did not match the fixture verifier. Only the fixture response
was corrected; production validation was preserved. Root's real ACP rerun passed
both original pair orientations with one durable effect, correlated stored reply,
signed ACKs and live revocation in each. These API/Relay endpoints remain simulated.
`TestBotCloudPrivateSourceSpine` also passed against the real local private API,
Mongo and workerd services for both signed-pair orientations. Each run admitted one
durable native ACP effect, read its original-capture stored reply, and enforced a
real human-consent revoke. Synthetic subscription data supplied the two-machine
entitlement; the unchanged Free admission correctly refused the second source in
the preceding setup run. No billing or external model API was called.

These local proofs do not cover every private-service failure injection or a
production rollout. Production deployments, installed-provider acceptance and the
external assistant gateway remain separate work. The Board's whole memory/settings
step remains incomplete.
