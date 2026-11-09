# Temporary API contention stays retryable

This candidate addresses a user-visible failure before microsecond savings: Gemini close attempts
reported an internal error during exclusive Session metadata work, and independent Cloud health
readers could exceed their own principal's two-slot limit. It does not implement a Cloud rollout,
raise the native single-writer limit, or claim zero downtime.

## Native Session and Cloud action receipts

The application baseline is `44fdc88182e98146f9e09a22cdeb2f3131cd3c49`. Its modern Gemini quota
implementation already uses an independent native CLI usage process, without an ACP model prompt.
That upstream correction is reused. Unknown ACP implementations remain unsupported; this candidate
does not probe a live model, expand the legacy allowlist, or change login/configuration.

Only a typed Session `busy` error whose delivery is `not_written` becomes the existing `busy`
refusal (HTTP 429). An uncertain delivery remains `close_failed` (500), with its receipt retained.
The direct HTTP receipt already releases 429 reservations. The outer Cloud `end` receipt now
releases precisely `429 busy`, so a later explicit retry with the same request ID can run. Other
429 codes, uncertain effects, authorization, execution-generation checks and completed results
retain their existing receipt behavior. No automatic close retry is added.

The regression evidence covers the application classification, the HTTP route with a real SQLite
receipt store, and the Cloud bridge with a real receipt store and controlled local-route replies.
The application classification and outer Cloud same-key regression failed before their respective
fixes. Near misses cover uncertain delivery and an unrelated Cloud 429, both of which must remain
recorded. Successful retries are replayed without a second close effect. Live Gemini close,
phone/relay execution and an installed candidate remain unverified.

## Shared health reads and bounded backpressure

`healthGet` shares two active fetch admissions per Cloud origin within a tab across stored-page
reads, attention, incident exports and investigation destinations. It holds at most eight waiting
reads in FIFO order. An aborted queued read never reaches fetch; overflow is typed `rate_limited`.
An active fetch retains admission until its underlying promise settles, including after a page's
deadline or navigation. Admission ends at fetch completion (response headers), not completion of
client-side body parsing. No response or credential is cached. Mutating POSTs retain their explicit
idempotency behavior and are not queued or retried by this scheduler.

The separate Cloud candidate raises `HEALTH_LIMITS.principalQueryConcurrency` from two to four;
process concurrency remains 16. This accommodates two tabs/devices with two active reads each.
A fifth read by one principal or a seventeenth process read still receives `429 health_busy` and
`Retry-After: 1`. The request deadline, Mongo query/scan budgets, body/page limits and ingestion
limits are unchanged. Timed-out underlying server work keeps its slot until it settles.

The frontend remains compatible with the previous two-slot API. Numeric and HTTP-date
`Retry-After` values are retained up to 60 seconds. Automatic attention and stored-page refreshes
wait at least that hint; they do not immediately retry a failed read. Existing request/overall
budgets include queue wait. New frontend bounds have capacity rows, HTTP measurement wiring and
both snapshots, bilingual row explanations and generated capacity guides; the registration guard first failed on the two missing rows. The existing parallel-read explanation now describes shared tab admission rather than equating it to the server limit.

Cost: a small in-tab queue and cancellation/cooldown state, plus up to twice the per-principal
share of the unchanged global server budget. Many tabs can still receive typed backpressure.
The unrelated performance-monitor reader has its own server pool and is unchanged. Deployment
order can be frontend first, but multi-device overlap needs the Cloud API candidate too.

The registered Gemini context and restart explanations were reviewed in both languages after the
error helper changed their source file. Their instruction behavior and original excerpts are
unchanged; the source fingerprint and existing shutdown citation follow the reviewed source.

## Repeated controlled measurement

Node v24.1.0, darwin/arm64. Three warmups per arm, then 20 measured rounds with alternating order.
Each round starts three stored-page reads and attention together against a synthetic two-slot
server. Each admitted read performs a fixed 20 ms wait and returns the existing non-private
fixture. There is no network, database, account, model, production load or external API cost.

| Metric | Baseline | Candidate |
| --- | ---: | ---: |
| Requests | 80 | 80 |
| Successful reads | 60 | 80 |
| Refused reads | 20 | 0 |
| Rounds with all four results | 0 / 20 | 20 / 20 |
| Peak server work | 2 | 2 |
| Batch settled median | 44.45 ms | 44.53 ms |
| Batch settled P95 | 49.14 ms | 50.85 ms |

There is no demonstrated latency improvement: the settled baseline includes a refusal, not four
successful results, and the tiny timing difference is not the reason to keep this fix. The result
shows the contention counterexample is removed without extra requests or higher single-tab server
concurrency. Production refusal frequency, user wait saved, database cost and tail latency are
not established by this synthetic workload.

Reproduce from the repository root (the temporary archive contains only this source subtree):

```sh
before=$(mktemp -d /tmp/health-before.XXXXXX)
git archive 44fdc88182e98146f9e09a22cdeb2f3131cd3c49 web/console/src/cloud/admin/health | tar -x -C "$before"
node --experimental-strip-types web/console/tools/measure-health-admission.mjs "$before" "$PWD" 20
node --experimental-strip-types --test web/console/src/cloud/admin/health/read-admission.test.ts
```

The harness prints every sample and the summary. The actual task also retained command, exit-code,
lock/version and raw red/green receipts privately. The Cloud runtime regression holds four real
Fastify-injected operator requests, checks the fifth's typed 429, lets all four HTTP deadlines
expire, verifies their slots remain held, releases underlying work, then observes a successful read.
This uses lockfile Fastify 5.12.7 and Vitest 3.2.7 with an in-memory repository; it is not Mongo or
production acceptance. Browser, commit-gate and publication outcomes are separate delivery evidence.
