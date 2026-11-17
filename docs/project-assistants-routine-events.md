# Routine events, follow-ups and safe retry

Source: CodyNC `9f67eb09084db61b3cca9c84a11bc03df9de29b7`,
`host/src/routines/{schedule,hooks,mod,follow_up,retry}.rs`. This document describes
the local implementation, not production, installed-agent or real GitHub acceptance.
Bots remain development-only and hidden in public Releases.

## Stored schedule and capability

`webhook` is an externally driven trigger. `event` adds `source`, `event` and
`filters`; these retain upstream exact-source, exact-event (or `*`), AND-filter
semantics. `messageContains` checks a substring of the top-level `text` string.
Other filters require equal top-level strings. A claimed source is data, never
an authenticated account identity. GitHub normalization uses `issues.opened`,
for example, rather than `issues` when the payload action is `opened`.

Hook triggers have zero next wake times and can be ORed with existing interval,
once and cron triggers. A routine with at least one hook gets a separate SQLite
capability row containing 32 cryptographically random bytes encoded as 64 hex
characters. Metadata edits retain it. Rotation replaces it transactionally;
removing all hook triggers, deleting the routine or deleting the Bot removes it.
Exports contain neither this table nor its keys. Restoring an exported hook
definition creates a new local key, so an archive cannot transport authority.

Feature schema 4 adds the capability table and delivery uniqueness index.
Installation from schemas 1–3 is additive and transactional, preserving old
aggregate, event, receipt, memory and routine bytes. Older binaries must refuse
the newer schema; the separately versioned groups schema stays at 1.

## Deliveries and human management

API level 75 adds `POST /hooks/routines/{id}`. This exact route retains the gate's
Host, path and cross-site checks but uses its own bearer or HMAC proof rather
than a device cookie or token. It accepts `Authorization: Bearer <key>` or
GitHub `X-Hub-Signature-256` HMAC-SHA256 over the original body bytes.

The original body is bounded at 64 KiB before decoding. Authentication precedes
event parsing. Unknown routine IDs and incorrect keys both return the same 401
`webhook_unauthorized` body. Paused routines return 409 `webhook_paused`; an active
or unresolved run returns 409 `webhook_busy` with `Retry-After: 1`. Authenticated
GitHub ping and nonmatching events return `{"ignored":true}` without a run.
Invalid bodies return 400, oversize bodies 413, unavailable/invalid internal
limits 500, and disabled sources 403. Aggregate races return 409
`webhook_changed`; uncertain persistence returns 409 `webhook_outcome_unknown`.
The sender must keep its delivery ID when inspecting or redelivering after an
uncertain response; a timeout never grants new execution authority.

An accepted `X-GitHub-Delivery` or `Idempotency-Key` identifies the first run for
that routine. The run has an indexed unique delivery ID. A separate durable
command receipt retains the first run ID even after published terminal run
bookkeeping is reclaimed. Run insertion and receipt persistence share one SQLite
transaction. Redelivery returns the first run ID, including an unknown run; it
does not enqueue or acknowledge that run's effects. Source, Project, enabled
state and the current key are checked before returning a delivery receipt.

The Host's existing timer discovers accepted runs, so no new provider, actor or
browser connection is needed for admission. It checks authority again before
execution. Deletion, pause, Project/source changes and forgetting keep their
refusal boundaries. Event runs bind the memory revision at admission; forgetting
cancels an unstarted stale event instead of putting its old payload in a prompt.
The event is double JSON quoted and explicitly labelled untrusted data, never
additional instructions or authority. Ordinary routines retain main/run/main
isolation and their separately quoted progress notes.

`POST /v1/project-assistants/{bot}/routines/{id}/hook` takes the existing edit
command, exact fence, routine ID, expected revision and a `rotate` boolean.
It returns a local relative hook URL and the key. Viewing the key requires current
write authority; read-only devices cannot retrieve it. Every currently authorized
writable paired device can manage it. Rotation uses the original edit receipt
and does not rotate again on replay. Publication revalidates human authority.
The Cloud vocabulary operation is `project-assistants.routine-hook`.

Native execution-bound tools are `routine_hook` and `rotate_routine_hook`.
They can only access their own Bot, exact Project and conversation. Keys are
returned only by these explicit operations, never by `list_routines`, progress
notes, ordinary API metadata or exports. Tool text tells the model to keep keys
out of published messages and progress notes.

Native event filters use upstream objects. The typed person/Cloud wire uses
`filters: [{key,value}]`, with duplicate keys refused; the transport maps these
rows losslessly to the same domain map. This preserves the repository's named,
closed wire schemas without broadening the contract generator to arbitrary maps.

## Follow-ups

`follow_up` takes `minutes`, `content` and a stable `idempotency_key` in the Bot's
own active main conversation. An isolated routine cannot set one. The reminder
must pass the existing retained-text checks and contain at most 500 estimated
tokens. The range is 1–43200 minutes, at most 10 reminders may wait per Bot, and
at most 24 continuations may follow one another without a human turn.

Follow-ups reuse the routine persistence and scheduler, with explicit
`follow_up` provenance, one once trigger, memory revision and chain count.
Their run source is `follow_up`. They resume the main session rather than
creating an isolated routine slot. Their note is quoted as an untrusted reminder
to inspect current state. A completed or unknown follow-up is consumed; it is
never replayed after restart. A follow-up created by a follow-up increments the
chain; one created during an ordinary human turn starts a new chain.

`list_routines` returns waiting follow-ups separately. `cancel_follow_up` cancels
one that has not begun. Stop drops due reminders, including wakes not yet handed
to the actor, while retaining future reminders. Forget conservatively withdraws
retained reminders; an execution from before forget cannot write a new reminder
using the newer memory revision. The source actor serializes all these changes.

## Retry and recovery

Automatic retries require the existing qualified `PreSendRefusal` guarantee,
no contradictory durable tool/message/permission evidence, and confirmed cleanup
of its returned session handle. Only then does an internal, non-wire proof allow
the same routine run and turn to become queued again. Neither an ordinary
`ProviderFailure`, `OpenNotStarted`, timeout, missing handle, nor a successful
Close alone proves that no effects happened. Unqualified adapters therefore do
not automatically retry failures. Fallback ledgers remain conservative and are
not converted into routine retry proof.

Safe retries wait 60, 300 and 900 seconds, keeping the original run ID, turn ID,
routine session slot and send idempotency key. `not_before` is a wake time, not
an execution timeout or deadline; sleeping beyond it does not discard the work.
After three retries a qualified failure is terminal. Never-started queued work and durably proved safe retry waits survive a planned
restart under the original run/turn/key. Unknown results remain a durable barrier;
started or unknown routine and follow-up runs are excluded from restart continuation.
No unknown run is moved to another agent, fresh session or replacement key.
Saving a completed result after a persistence failure still uses the existing
completion recovery path and never sends another native prompt.

Clawdline deliberately does not copy upstream's manual replay of a run that may
already have acted. A manual retry button must not be offered for unknown effects.
An explicit future task is a new decision, not evidence that an old effect failed.

## Remaining integration surfaces

The human console now exposes event source/name/filter and webhook triggers
alongside interval, once and cron. Event and key operations require source API75;
older sources retain their calendar controls and never receive the new fields.
Changing trigger kind clears incompatible fields. The editor and command-journal
reader both enforce the closed typed filter rows, registered UTF-8 byte limits
and duplicate-key refusal; source/backend validation remains authoritative.

Key management is an explicit action on a saved hook routine. Opening its panel
does not retrieve a key. Retrieve and confirmed replacement require current human
interactive write authority and the complete machine/Bot/Project/conversation
fence. A replacement saves the exact command before transport; an uncertain
response or reload offers only explicit replay with the original command ID,
expected revision and fence. The existing account/reader/Bot sessionStorage
journal remains in use, preserving older uncertain calendar commands. Journals
contain commands, never keys. Keys remain in the current mounted key panel;
closing, hiding, losing write authority or changing source/account/Bot/Project
clears or hides them. Late responses cannot publish into another source panel.
Keys are not automatically copied, logged, exported or embedded in URLs. The
returned relative path is labelled local, never a public Cloud address.

Future enabled follow-ups with a source-reported next wake appear separately.
History shows actual run source, delivery ID, safe retry count and not-before
time when present. Unknown effects have an explicit warning and no retry button.
A person can cancel a pending follow-up through the existing authenticated
routine-delete command. It carries the exact edit receipt, conversation fence
and expected definition revision; the source rechecks current authority. Once a
scheduler tick changes that revision, a stale pending-note action is refused.
This control cancels a future note; it does not stop an already started run.
An uncertain reply retains the original command across panel remounts and only
an explicit replay can resend those exact bytes. The replay button waits until
current source authority has loaded. The focused bilingual browser fixture
checks read-only refusal, one lost reply, exact replay and two SQLite tombstones
without opening a provider.

Focused construction evidence includes Node cases for closed trigger shapes,
exact uncertain-key replay/reload, source-scope changes, late-key rejection and
API75/read-only/hidden gates. `TestProjectAssistantRoutineControlsSourceClientTCP`
uses the production TypeScript SourceClient against authenticated TCP, the real
Host and SQLite: save a filtered event, retrieve/rotate/replay, admit/dedupe a
delivery and inspect a genuinely executed scripted-provider result. Provider
execution is scripted; native/model/paid API counts are zero. This is construction
verification, not browser, installed-app or production Cloud acceptance.

## Source enrollment and Cloud deliveries

The source bridge uses the separate `clawdline.bot_routine_hooks.v1` machine-
authenticated REST protocol. It adds no content to opaque WebSocket envelopes
or ordinary Cloud command lanes. The credential remains inside the Cloud
adapter; the receiver borrows neither a browser grant nor a local/device bearer.
Incoming webhook bodies and proofs are visible to Cloud. Bot chat, memory and
instructions do not travel through this bridge.

`PUT /v1/bot-routine-hooks` replaces the complete `{id,key,enabled}` set. The
source persists its canonical sorted set fingerprint and positive JS-safe
revision before networking. A lost response retries the same content/revision;
rotation, pause, removal and feature opt-out advance it. An unexpected higher
Cloud revision produces a typed conflict; it is never silently overwritten.
A fresh source without hooks or prior enrollment does not send empty sets.
Existing enrollment is withdrawn with an acknowledged empty replacement even
when Bots are disabled. Current account, machine and Project are checked before
publication and again after network IO.

Bridge metadata has an independent schema version 1, alongside feature schema
5. Installation is additive and transactional; future bridge versions refuse
before feature DDL. It never rewrites aggregate/message/event/receipt bytes.
Enrollment fingerprints, one pending forwarding/ack barrier per namespace and
leases stay outside public views and archives; existing capabilities remain in
their private hook table.

Claim returns a Cloud queue ID, a scoped routine, enrollment revision, original
proof headers, canonical base64 body and lease token. The source rejects oversized
or noncanonical bytes and duplicate/unsupported headers. It preserves exactly
the supplied `X-Delivery-Id`, which may differ from the queue ID. It invokes the
production `POST /hooks/routines/{id}` receiver with original bytes and proof;
the Host independently rechecks current authority, key, Project, feature, memory
and SQLite delivery receipt. A durable pre-forward barrier prevents replay after
an ambiguous local effect or storage failure. A recorded local HTTP response is
acknowledged with its original lease, actual status/code and actual run ID.
Lost ack responses retain the identical durable ack and inspect same-machine
metadata. Unknown local outcomes inspect only; no replacement event identity or
new native prompt is inferred. Cloud's busy backoff retains the same identity.

One cancellation-aware sequential worker owns enrollment/claim/ack. Cloud reload
cancels and awaits it before another generation starts. Normal idle polling is
5 seconds; failure backoff reaches 60 seconds. Each cycle and HTTP client have
a 10-second deadline. No Host actor or memory lock is held during Cloud IO.
Source bounds are registered for 1000 hooks, 512 KiB enrollment, 64 KiB original
event, 200-byte headers, 100-byte routine IDs, 128 KiB HTTP replies and 16 KiB
local replies. Cloud separately owns its 1000 retained deliveries, 60-second
lease, 72-hour TTL, 30–900-second busy retry, 512 ack receipts, 256 source rate
slots plus global slot, 60/1200 requests per minute and 5000 ms query deadline.
Source diagnostics do not invent live measurements of those remote limits.

The existing human routine-hook response optionally adds `public_url`. The
local URL/key work with older sources. A public HTTPS address is available only
when the active worker has acknowledged exactly the current hook/key/enabled
snapshot; configured, offline, unknown, disabled, canceled or stale enrollment
provides no ready public address. Publication and the console validate source
scope; URLs contain no key or credential. The key panel clears these results
with its existing authority and lifecycle rules. A configured address or a local
fixture is not evidence of production deployment or acceptance.

`TestBotRoutineHooksHTTPHostSQLiteExactBytesLostAckAndRedelivery` uses authenticated
human HTTP, real Host/SQLite, a local fake Cloud HTTP server and a scripted
provider. It exercises enrollment, HMAC over whitespace/exact large-number JSON,
original delivery identity, lost ack and durable same-ID redelivery. These are
construction fixtures, not production Cloud receipts. The fixture first creates
an actual scripted main-session binding, which the existing routine runtime
requires; admission itself can remain durably queued before that first binding.
The opt-in
`TestBotRoutineHooksRealControlPlaneOptIn` accepts an independently provisioned
local control plane through `CLAWDLINE_TEST_BOT_HOOKS_API`, `_ACCOUNT`, `_MACHINE`
and `_CREDENTIAL`. Its default skip means the cross-repository path was not run.
Root owns that run, private Cloud integration, secrets, deployment and acceptance.

Local scripted tests use real HTTP, Host lifecycle and SQLite. They cover
signature/bearer authentication, indistinguishable credential failures, filters,
redelivery, pause/busy/ping, rotation/deletion/export, memory forget, Project and
source refusals, transaction failure injection, unknown execution and main/run/main.
Engine tests cover main-session follow-ups, bounds/chain/cancellation/Stop and
qualified no-effect retry. These are not native-model or production receipts.

## Retained input-adapter contract

The previously callable input module remains independent of admission and
execution. Event spec limits are 200 name bytes, 20 filters, 200 key bytes and
4000 value bytes. Sources are `slack`, `github`, `origin`, `microsoftTeams`,
`linear`, `sentry`, `pagerduty` and `email`. Webhook input limits are 64 KiB
original body, 200 delivery-ID bytes, 200 GitHub-event-header bytes and at least
32 printable secret bytes. The last two are explicit Clawdline bounds where
upstream had none; generated keys contain 64 printable hex characters.

Delivery-header precedence remains `X-Delivery-Id`, `X-GitHub-Delivery`, then
`Idempotency-Key`. JSON numbers remain exact. A wrong bearer may still be
authenticated by a valid HMAC, as upstream. Diagnostic reasons never contain
body bytes, proof or secrets and are not returned to the sender.

The deliberate input differences are retained: non-object/non-JSON input and
invalid UTF-8 are refused, repeated proof/event/delivery headers are refused,
GitHub event headers must be nonempty printable bounded text, digests use strict
hexadecimal (no leading `+` byte pairs), and the key must meet the registered
minimum length. Empty bodies remain empty objects; authenticated ping is ignored
before JSON parsing. The transport maps adapter failures to `pa.Failure` before
application admission: scope denied/unauthorized, invalid/input, capacity/body
and unsupported/internal-limits, retaining the HTTP status families above.
