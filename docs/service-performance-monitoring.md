# Cloud service and owner-local API performance

Two separate records serve two separate permissions. The operator view at
`/admin/health/services?view=performance` reads stored Cloud service aggregates through
`GET /su/v1/health/performance`. The machine dashboard's **API performance** tab reads
`GET /v1/api-performance` from the selected paired daemon, locally or through its encrypted owner channel. Owner records stay on that machine;
they never enter the service monitor, connection-health upload or operator administration.
See [the local producer and storage contract](local-api-performance.md) for its permission and
histogram details. In the general console, **Manage → Machine monitoring** opens
machine selection followed by **Resources / API performance**. The selected
machine and paired account/device scope own the panel; changing them unmounts
its held observations. A machine without the read declares it unsupported.

The Admin sidebar has a separate **Service monitoring** destination, opening
the service performance view directly. It is distinct from **Connection health**
and does not grant access to owner machine records.

The existing M01–M06 stage, incident, trend and monitoring views remain available. The service
performance subview does not replace the connection-health lifecycle or owner-sealed evidence.
Admin remains independent of machine pairing and the relay. A monitoring failure cannot authorize
a retry, restart, discard, recovery claim or operation on a person's computer.

## Service observations

The Cloud service implementation has three independent layers: a bounded in-memory API producer,
a Linux background host sampler and separate SQLite storage, and authenticated read-only Admin
queries. The API producer reports route templates, complete response-hook wall durations, fixed
histograms, 5xx, client cancellation/499, counts, active work, boot and deployed source commit.
It never records request bodies, actual URLs, query values, addresses, headers or account identity.
API admission through response completion is measured; edge queue time and client receipt are not.
Intentional long-poll waits are part of that elapsed duration, including the response-time alert
threshold. A slow-route observation is not by itself evidence of slow CPU execution.

VM resources use CPU-counter differences, `MemAvailable`, core count, load and fixed managed
container cgroups. The first CPU sample is unknown. The cgroup identity is checked against the
inspected container and kernel PID start time; totals include child tasks. Memory is cgroup
accounting, rather than summed process RSS. Runtime image identity and source commit remain
different claims. No paired private computer is sampled by the service monitor.

Cloudflare host CPU/memory, relay request histograms, independent external availability and edge
queue timing are explicitly unsupported in this implementation. Existing relay stage observations
remain on the stage view; they are not relabeled as request percentiles or fleet coverage.

## Reading and truth rules

The four cards show host CPU, memory, API P95 and server error rate. Current and previous windows
each contain five complete minutes. Missing observations do not become zero latency or success;
an observed interval with zero completions has no percentile/error-rate denominator. Fewer than
100 requests, incomplete coverage or dropped observations prevents a change assessment.
Percentiles merge fixed histogram counts and show upper-bound estimates capped by observed max.
They are never averages of source percentiles.

History uses 1/6/24-hour and seven-day ranges, synchronized chart axes, readable values and
keyboard controls. Seven-day request history uses hourly summaries. Minute edges make daily
queries precise; the seven-day slow-route list begins at the first complete retained hour and
states its actual interval. Resource history selects retained samples within bounded bins; it is
not a claim to capture every short resource spike. Unknown deployment ends draw a start marker
without an invented duration. Markers establish timing, not a cause of a performance change.

Every read checks the current revocable Admin session and role and uses `no-store`. There is no
shared data cache or SSR data singleton. A changed window starts empty; a forbidden answer clears
held data. Hidden tabs stop and cancel polling. Returning focus revalidates before showing service
data; visible background refresh can retain a clearly historical result on failure. Local and
Cloud adapters never fall back to each other's data or to raw machine diagnostics.

## Alerts, budgets and rollout

New abnormal samples trigger durable alert transitions after three observations; recovery also
needs three new observations and lower thresholds. Unknown evidence breaks a streak and cannot
recover an alert. The durable outbox leases work, deduplicates transitions across restart and
limits delivery attempts. Monitoring notifications require a separate opt-in on an existing Admin
notification device; question notifications alone do not enable them. Every send checks device,
current role, subscription generation and opt-in again. Acceptance is a provider receipt, not a
human observation. A send followed by a crash before its receipt can repeat the same notification
tag; there is no exactly-once claim.

The content-free [service budgets](../api/v1/service-monitor-budget.json) are registered alongside
browser read guards in `internal/domain/capacity`. Remote deployment bounds do not imply this
daemon can observe remote occupancy. API requests do not wait on telemetry disk or network work.
The service store, including its side files, has separate disk and query limits; physical quota
failure leaves evidence unavailable instead of replacing it with a healthy empty response.
Minute summaries retain one day, hourly summaries seven days, and raw payloads are not retained.
Response histories admit at most 289 points including a clipped window edge; resource histories
have an independent 288-sample bound. The actual oldest retained host observation is displayed; retention is not continuous coverage.

The monitor uses existing infrastructure and no new paid telemetry API. Service installation,
the API rollout, relay (when changed), hosted console, local dev installation and public Release
have separate receipts. Source and local test success do not prove production enablement.

Implementation/acceptance is in progress. The initial shared-screen preview is development
fixture data; it does not establish deployment or production performance.
