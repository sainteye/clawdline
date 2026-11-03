<!-- clawdline-doc: kind=spec audience=agent -->
# Cloud storage resilience and protocol compatibility

Status: narrow image-draft mitigation with a deferred architecture recommendation,
2026-10-28. Only the composer image-persistence removal below is in scope for
implementation. The connection protocol, key vault, temporary pairing,
independent telemetry, migration and retirement below remain recommendations.
They are deferred and must not be implemented under this change.

The goal is to keep an already authenticated Cloud console usable when its
browser's IndexedDB stops answering, without weakening encryption, pairing,
revocation or replay protection. Compatibility is a staged migration with an
exit policy. Keeping two complete protocols forever is not the target.

## Current scope and the image-draft storage pitfall

Remove image-draft persistence from the Session composer. Pictures belong only
to the current editor; leaving that editor or reloading the page requires
selecting them again. Picking, previewing and sending a picture still use the
existing image path. Explicit failed-send recovery is separate from an unsent
composer draft. Text drafts keep their localStorage identity, lifecycle-boundary
saves and immediate deletion on send or clear.

The old `browserDraftStorage.read` fetched localStorage text and then awaited an
IndexedDB image read, even for an editor with no pictures. If IndexedDB failed,
the combined read rejected and hid the successfully read text. A regression
test with an injected IndexedDB open failure reproduced that loss; the revised
text-only store reads the text without touching IndexedDB. The composer no
longer opens, reads or writes `clawdline-composer-drafts`.

Separate database names are not complete fault isolation. Browsers manage
storage limits at the origin level, so optional image persistence can compete
with critical sequence/key storage for the same application's budget. See
[browser storage quotas](https://developer.mozilla.org/en-US/docs/Web/API/Storage_API/Storage_quotas_and_eviction_criteria).
This is a plausible coupling, not proof that saved images caused an observed
sequence transaction failure. The original IndexedDB transaction error was not
retained, so this change cannot establish that causal link or claim to eliminate
all `no_sequence_store` failures.

The lesson is to keep optional, potentially large draft content off a critical
authentication/transport dependency, and never combine independent draft reads
so that image storage failure discards valid text. Future persistence work must
measure its origin-wide footprint and exercise quota, transaction and background
failures alongside sequence/key operations before it is enabled.

Existing stored picture drafts are not read or restored, and are not deleted
automatically. This removal therefore stops new activity but does not reclaim
their old disk usage. A page already running the old JavaScript keeps its old
behavior until the person reloads it; this change does not forcibly reload an
active editor. Do not delete the key or sequence databases as cleanup.

The remainder of this document preserves the larger design for possible later
review. None of those mechanisms is required to land this narrow removal.

## Current dependency and evidence

IndexedDB belongs to the browser's local storage for the application origin.
The sequence database is separate from the key database. In public source
`b25a774b464181057e7a07e8fc0287dc67e3cb64`:

- `web/console/src/legacy/js/net/cloud-boot.js`, `durableSequence`, reserves
  sequence blocks in IndexedDB. Its transaction error/abort branch maps failures
  to `no_sequence_store` and retains the cached database handle.
- The same file's `CloudSession.machinePairing` reads pairing bindings and keys
  from IndexedDB during operations. Fixing sequence allocation alone therefore
  does not make active operations independent of IndexedDB.
- `web/console/src/legacy/js/net/cloud-client.js`, `_publishCommand`, needs both
  a sequence and a complete machine pairing before publishing a request.
- `internal/domain/cloud/replay.go` keys incoming replay state by sender and
  sequence. Its 1,024-number window is shared across transports. A claim is final.

An injected persistently failing transaction against that exact sequence
function failed three consecutive requests while opening the database only
once. This demonstrates the recovery gap; it does not identify the browser
engine's original failure. The generic error alone cannot distinguish quota,
I/O, an aborted transaction or another storage failure.

Existing Cloud wire behavior is described in [Cloud](cloud.md) and
[the terminal wire](cloud-terminal-wire.md). The new protocol below requires
its own reviewed wire contract before implementation; it cannot be introduced
by resetting the current counter in the browser.

## Intended experience and limits

| Situation | Required behavior after the relevant stage is delivered |
| --- | --- |
| A running page loses IndexedDB access | Keep verified keys in memory; recover storage independently; continue operations supported by the negotiated resilient protocol. |
| Storage remains unavailable | Use a visible temporary-storage state, without repeated blocking alerts, reload loops or loss of the current destination and draft. |
| A backgrounded page returns | Revalidate connection authority and key generation, reconnect if necessary, then retry safe reads within a bounded deadline. |
| The process exits and no key can be loaded | Keep sign-in and recovery reachable; offer explicit temporary pairing. Encrypted content remains unavailable until the key handover succeeds. |
| The machine is old | Preserve its supported legacy behavior. Do not claim that the new resilience guarantee applies to it. |
| A write's outcome is unknown | Keep that uncertainty and operation identity; query a receipt where supported. Do not silently send it again. |

Temporary pairing holds non-extractable keys only in memory, uses the existing
human verification boundary and has explicit expiry and device-quota accounting.
It must not silently create permanent devices after every storage exception.
Exiting that temporary browser process loses its keys; another handover may be
needed while persistent storage remains unavailable. A login cookie cannot
replace a missing cryptographic pairing. Offline machines and unavailable
networks remain separate failures.

## Delivery stages

1. **Bounded recovery and independent diagnosis.** Preserve typed storage error
   names and stages; close/invalidate failed handles on abort, error and unexpected
   close; make at most one automatic reopen attempt per failed operation, within
   its original deadline. Fence late callbacks and transaction completions so
   timed-out work cannot publish later. Never reuse a number whose commitment is
   uncertain. Failure then settles visibly instead of retrying indefinitely.
2. **Storage-independent active connections.** Introduce the negotiated connection
   protocol and an in-memory key vault. Storage failure no longer blocks every
   active read or write for a capable machine.
3. **Cold-start temporary access.** Add the explicitly verified, memory-only
   pairing flow and its cleanup. This is a separate capability and acceptance gate.

The first stage can improve recovery without changing the envelope protocol.
It does not by itself guarantee operation during a persistent IndexedDB outage.
Each stage gets its own evidence and can be delivered independently.

## Active connection mechanism

The legacy protocol keeps its existing sender counter and storage rules.
The proposed connection protocol uses the following separate state:

- The machine issues a fresh one-time challenge. An authenticated handshake binds
  the existing paired viewer, target machine, daemon incarnation, protocol version
  and a fresh per-page connection identity. It does not need a legacy sequence
  to bootstrap. Cookies or a relay claim alone never establish the pairing.
- Successful admission returns a short-lived lease with a receiver-issued ID.
  The authenticated handshake transcript fixes its identity and expiry. Exact
  handshake retries yield the same lease without extending its lifetime.
- Every request authenticates the lease identity, target and request counter.
  The counter lives in page memory. Replay state is keyed by viewer, machine
  incarnation and lease, isolated from the legacy sender window.
- The relay and direct carrier share the same receiver-side admission and replay
  state. Receiving the same request through two carriers can authorize it only
  once. Each tab and target machine has independent connection state.
- Expired, unknown, closed and previous-incarnation leases are refused. A packet
  cannot recreate a forgotten lease. Consumed challenges remain consumed for
  their validity period; capacity pressure must refuse admission rather than
  discard replay evidence to make an old request look new.

The receiver uses its own monotonic lifetime checks. Client wall clocks do not
grant authority. New concurrency, lifetime, memory and diagnostic budgets must
be registered in `internal/domain/capacity` and pressure-tested before release.

The in-memory key vault keeps a complete verified binding: account, device,
machine sender pin, content-key ID, epoch and non-extractable key handles.
Account changes, revocation, lease closure and incompatible epoch observations
invalidate the relevant state. Cached keys are not cached permission.

Key rotation must work during storage failure too. A new peer must explicitly
negotiate authenticated in-memory key adoption and distinguish its volatile
acknowledgment from a durable persistence receipt. It must never claim that a
key was saved when it exists only in memory. Existing peers' durable-rekey
requirements stay intact. Until this contract is implemented and tested, a
key rotation during a storage outage is an uncovered case, not a promised pass.

## Version coexistence rules

"Legacy" and "connection" below are design labels, not product release numbers
or an already assigned envelope version. The wire version, canonical signature
bytes, message grammar and capability identifier must be fixed together in the
reviewed browser, daemon and relay contracts.

| Browser | Target and routing support | Selected behavior |
| --- | --- | --- |
| Existing browser, including an already open tab | Upgraded receiver | Legacy protocol; existing pairing and counters remain usable. |
| Upgraded browser | Existing receiver, missing feature or explicit unsupported response | Legacy protocol when its prerequisites are healthy; preserve the existing typed failure otherwise. |
| Upgraded browser | Upgraded receivers, activation disabled | Legacy protocol. |
| Upgraded browser | Full support, activation enabled and fresh authenticated admission succeeds | Connection protocol for newly admitted operations on that target. |
| Any browser | A different target with a different capability set | Negotiate that target independently. |

Selection is per target and connection, never an account-wide switch inferred
from a version string. A cached descriptor may guide a probe but cannot prove
current support. Both authenticated capability evidence and a successful
admission receipt are required before publishing a new-format content request.
Unknown and timed-out probes do not authorize the new protocol.

Upgrade the receivers before enabling new senders. Receiver support includes
every routing or receipt component on the selected path. New formats stay on
their explicitly negotiated routes; a legacy parser must never see a new packet
and guess its meaning. Do not reuse a cryptographic context with different
signature rules under an ambiguous version label.

Preserve existing database names, schema, device identities, pairing pins and
legacy sequence ceilings throughout coexistence. No destructive migration or
mandatory account-wide re-pairing is part of activation. Connection counters
must not advance the legacy window or be written into its sequence store.
Feature activation in one tab must not poison a tab still using the old protocol.

## Operations during switching and rollback

Changing a flag affects new admissions. It does not rewrite the identity of an
operation already admitted. Each logical operation records its target, protocol,
lease/incarnation when applicable, operation ID and known delivery stage.
Queued work is bound to a connection generation; replacement fences the old
generation before admitting new publication.

Reads may reconnect and retry within the original deadline and generation
checks. Mutations cannot be resent on a replacement connection or another
protocol merely because an acknowledgment disappeared. Preserve and look up
the original receipt; when that cannot establish the outcome, expose uncertainty
and do not guess that the action failed.

A mutation receipt is not automatically an exactly-once execution guarantee.
Each supported mutation needs an operation-specific execution/receipt contract,
including crash boundaries and any external side effect. Unsupported recovery
cases must remain uncertain. The new path cannot advertise a general guarantee
based only on a ledger entry written after execution.

Rollback has distinct steps:

1. Disable new connection-protocol admissions for the affected cohort.
2. Keep the compatible receiver code available to finish or explicitly settle
   existing leases and outstanding receipts; preserve legacy support and data.
3. Let new operations use the legacy path only after its own prerequisites are
   verified and previous uncertain mutations remain fenced.
4. Verify old and new clients, settled operations, pairing and key-epoch behavior.

A backend image lacking the active protocol is not an immediate safe rollback
target. Retain a known compatible image capable of disabling activation while
draining its state. An emergency security revocation may close leases at once;
it still must not turn an unknown mutation into an automatic retry. Browser
asset rollback alone cannot retract JavaScript already running in old tabs.

## Coexistence lifecycle and retirement

Capability negotiation and explicit version refusal are permanent infrastructure.
The complete legacy execution path is transitional. Its lifecycle is
**available but disabled → controlled trial → new default with legacy support →
deprecated → retired**. The release owner records evidence before each advance.

The proposed support policy is at least 30 calendar days of notice, at least two
stable release versions providing the replacement browser/daemon path, and 14
consecutive days of complete migration observations ending at the retirement
review. All three requirements apply. These are proposed minimums for review,
not a scheduled shutdown or already approved product support promise. The release
owner records actual release identities, dates and collection coverage before
general activation and reviews each extension at least every 14 days.

Retirement requires all of the following:

- Publish the minimum supported browser/daemon protocol, notice date and earliest
  retirement date before removing support. Fix the calendar dates and the above
  support minimums before activation expands beyond controlled trials.
- Demonstrate the new path's acceptance matrix, security checks, cold-start
  recovery and rollback on the supported platforms.
- Measure legacy admissions with a denominator and a completeness/coverage signal
  over the 14-day observation period. Resolve all observed legacy admissions
  before retirement, or explicitly extend support for their affected population.
  Missing telemetry never
  counts as zero legacy users. Quiet traffic says nothing about long-offline users.
- Resolve observed legacy use by migration or a documented supported exception.
  Exceptions have an owner, reason and review date within 14 days; they cannot silently make
  coexistence permanent. The retirement owner records the outcome of each review.
- Stop admitting new legacy operations, then drain or explicitly settle existing
  leases, maximum validity periods, queued operations and receipt lookups. Do not
  drop an unresolved receipt merely because its transport is retired.
- Exercise return-after-long-absence and downgrade paths. A client below the
  announced support floor receives a typed update-required response and a usable
  update/recovery entry, not a blank page or a destructive pairing reset.
- Obtain explicit release acceptance for retirement and record which rollback
  image remains valid. Delete legacy implementation and its feature flag only
  after these gates, while retaining version detection and the update-required
  entry point.

Actual notice dates, release identities and observation ownership are launch
blockers, not permission to support an unbounded transition. No automatic
date-based deletion is allowed. A later implementation plan must name an owner
and record these values before general activation; extending support requires
a recorded reason and next review.

An indefinitely old binary cannot receive unlimited hosted compatibility while
the legacy protocol is also removed. After the announced floor takes effect,
updating is required for that Cloud connection. This does not stop the local
Agent. The user experience promise is a controlled, communicated transition,
not permanent support for every past version.

## Costs and new failure modes

| Cost or risk | Required control and remaining limitation |
| --- | --- |
| More code and a larger cross-version test matrix during migration | Keep one owner, one contract and a retirement checklist. Maintenance cost increases until the legacy path is removed. |
| Additional handshake and renewal work | Reuse admitted connections; bound concurrency, memory and rate. Measure first-read latency, request volume and background/foreground behavior. No latency improvement is claimed yet. |
| Lease expiry, stale capabilities or mixed deployment | Require fresh admission, generation fences, typed refusals and staged receiver-first rollout. An unavailable peer cannot be made available by a browser fallback. |
| Replay or duplicate mutations during protocol switching | Isolate replay state; share it across carriers; preserve operation identity and receipt uncertainty; inject failures around execution. |
| Cached keys outliving authority or a key rotation | Bind complete epochs, revalidate authority and negotiate volatile adoption explicitly. Do not reuse an older key merely because storage failed. |
| Persistent storage hides the first failure from the diagnostics path | Record content-free typed error names through independent authenticated HTTPS, with bounded memory buffering and explicit loss counts. Telemetry failure cannot block normal work. |
| Temporary keys disappear at process exit | Explain the temporary state once and provide explicit re-pairing. Seamless cold start under total key loss is outside this guarantee. |
| Coexistence never ends | Require the support floor, published dates, usage coverage, owned exceptions and retirement acceptance above. |

Memory caching is not a new backup of private keys. Keep non-extractability,
avoid secret material in logs or localStorage, and discard handles when their
authority ends. New telemetry must exclude content, keys and arbitrary engine
messages; retain only reviewed, allowlisted stages and typed error names.

## Acceptance and rollout evidence

Before controlled trials, run deterministic failure injection for unavailable,
read-only, full, aborted and hung IndexedDB; late commits; multiple tabs with
mixed protocols; different machines; reconnects and daemon restarts; cross-carrier
replays; storage loss during rotation/revocation; receipt loss before and after
execution; storage recovery; cold start; temporary pairing expiry/quota; and
rollback with outstanding work. Test a page refresh and a full PWA relaunch as
separate actions. Include a near-miss implementation that improperly reuses a
failed handle or mixes replay namespaces and prove the relevant tests reject it.

Start with explicitly selected test devices. Report actual sample counts,
browser/daemon versions, cohorts and observation windows for read/send success,
timeouts, first-read and completed-operation latency, recovery, pairing, lease
admission, duplicate execution and revocation. Privacy-safe identifiers must
distinguish device, tab, target and connection without storing content.

Write numerical acceptance thresholds, request budgets, the trial population
and observation duration before trials begin; evaluate against a comparable
legacy cohort. Security-invariant failures, duplicate execution or loss of a
valid existing pairing stop expansion immediately. Passing telemetry samples
do not prove that every unobserved client works. Expand only after reviewing
both functional outcomes and collection gaps, then keep the activation kill
switch through the announced rollback window.

Expected user benefits are fewer whole-console failures, less manual reopening,
preserved navigation and drafts, and clearer recovery actions. Demonstrating
these under induced failure and supported real-device scenarios is a release
requirement. This design has not yet established a production success rate,
latency gain or absence of regressions.
