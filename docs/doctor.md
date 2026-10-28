# Doctor reports

Doctor joins the running daemon's version, platform, fixed `claude --version` and
`codex --version` probes, current quota observations and bounded local quota history.
The entry is **Settings → Status → Doctor**. `clawdline doctor --json` reads the same
authenticated `GET /v1/doctor` endpoint. The existing plain `doctor` output remains
available when diagnosing a daemon that cannot be reached.

The Cloud vocabulary carries this machine-scoped read as `doctor`, without the
remote-write gate. API level 32 advertises the endpoint. A previous machine cannot
run the check; an already downloaded report can still be imported at
`https://app.clawdline.com/help/doctor` without a live machine connection.

## Evidence, not an authentication guess

The quota reader records transitions whenever `/v1/provider-quotas` is read. A
transition contains only a known provider/source, typed problem, optional numeric
percentage, observation time and stale flag. Real zero survives JSON round trips;
unknown values are null. An unchanged observation does not write again. No
background provider polling, chat prompt, automatic token refresh or paid inference
request is introduced. Doctor itself uses `Inspect`, which neither reads provider
credentials nor initiates a metadata refresh.

Claude's source remains its status-line cache and Codex's source remains rollout
quota keys. The report cannot establish whether Claude's upstream usage request
returned 401, whether a credential changed after login, or whether the browser
actually rendered a footer. These are explicit evidence gaps, never inferred
authentication failures. A CLI version is the executable found on the daemon's
PATH, not proof of the executable used by every active Session.

Each fixed version probe has a three-second deadline and a four-KiB output cap.
Only a recognized version is exported; stderr, paths and unrecognized output are
discarded. Concurrent Doctor checks on one server are refused with `doctor_busy`.

## Local retention and failures

The daemon retains at most 24 quota transitions for three days in
`<state>/doctor/history.json`, with a 32-KiB file cap, private directory/file
permissions and atomic replacement. Coverage names retained history, the latest
recorded transition, expiration/eviction counts, unknown loss and read/write
failures. History is not continuous monitoring and cannot reconstruct incidents
before installation. A failed history write does not replace the quota answer.
An unreadable history file is never exported as arbitrary text.

The exported report is at most eight KiB. The browser records the hosted build
when known and validates the same field whitelist before preview, import and
submission. Credential values, account identifiers, terminal/transcript text,
provider response bodies, model names and private filesystem paths have no fields
in this contract. A problem description is voluntary user content, capped at
500 characters, with a reminder not to include secrets.

## Explicit private submission

Preview and download work without upload. A signed-in Cloud member can explicitly
consent to submit the report, optional screenshots and description through the existing private
`POST /v1/support/questions`, with request type `issue`. This is server-readable
account-service support content, separate from encrypted machine traffic and
public community discussions. It does not create a public topic or publish an
existing thread. The owner, support operators and restricted answering Agent can
read the report. No Cloud API schema or database migration is required.

Before POST the browser persists the account, exact body and idempotency key in
session storage. An ambiguous result freezes that submission and retries only the
same body/key, including after reload. Another signed-in account cannot replay it.
Only a valid private question receipt displays success and a link to the original
thread. There is no automatic upload or background retry. Storage failure prevents
POST instead of risking an unrepeatable action.

## Verification boundaries

Tests exercise missing/zero readings, strict import projection, persistence,
retention, corrupt history, disk failure, bounded CLI probes, credential-free
inspection, local HTTP and Cloud routing. Browser acceptance uses isolated sample
reports and support responses. It does not submit a real user's report or call an
inference provider. An affected user's installed build, status-line producer and
actual incident still require that user's report.

See [provider quota sources](provider-quotas.md), [Cloud support](cloud-support.md)
and the [user guide](user/doctor.md).
