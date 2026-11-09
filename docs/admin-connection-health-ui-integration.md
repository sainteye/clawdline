# Connection health: UI integration

Status: 2026-10-21. `/admin/health` is a page of the hosted admin shell. It reads the Cloud's
stored operator evidence (`GET {api}/su/v1/health/…`, private contract
`docs/connection-health-contract.md` in the Cloud repository, §3, §3a, §10, §11 and §12) through an
explicit adapter, and draws M01–M06 and the evidence view from what those reads return. It is
**not** the complete monitor of [admin-connection-health.md](admin-connection-health.md): the Cloud
has no presence contract, no viewer producer, no probes and no owner-sealed diagnostics yet, and
every screen says so where it would otherwise have shown them. Durable incidents, their immutable
revisions, the audited export and the operator acknowledgement (Cloud commit `fac588c`) are
read and drawn. The Cloud API that serves these reads is committed, not deployed; nothing here has
called production.
The design is [admin-connection-health-design.md](admin-connection-health-design.md).

## Addresses

All under the existing admin shell (`AdminGate.tsx`), beside the business pages, signed in with the
same revocable cookie session and independent of the machine `CloudGate`. A failed business
overview does not hide this page; a refused health read does not break the business pages.

| Address | Screen | Reads (at most two in flight) |
| --- | --- | --- |
| `/admin/health` | M01 overview | `overview`, `outcomes`, `incidents` (open ones link to M05) |
| `/admin/health/machines` | M02 machines | `sources`, `outcomes` |
| `/admin/health/machines/<machine_id>` | M02 one machine | `sources?machine_id`, `outcomes?machine_id`, `events?machine_id` |
| `/admin/health/services[?source=…&source_id=hs_…]` | M03 Cloud services | `overview`, `sources`, `events?source&source_id` |
| `/admin/health/trace/<correlation>` | M04 trace | `trace/<correlation>`, `outcomes`, `incidents` |
| `/admin/health/incidents` | M05 incident list | `incidents` (7 days by `opened_at`, 50 rows), `outcomes` |
| `/admin/health/incidents/<hi_…>[?revision=n]` | M05 one revision | `incidents/<id>?revision=n`; export and acknowledge on demand |
| `/admin/health/monitoring` | M06 monitoring | `overview`, `sources`, `outcomes` |
| `/admin/health/evidence[/<correlation>]` | evidence record | `outcomes`, `events` or `trace/<correlation>`, `sources` |
| `/admin/health?trend=<range>` | M01 trends | `aggregates`, `overview`, `incidents` (7 days ending where the trend ends) |
| `/admin/health/services?trend=<range>[&source=…&source_id=hs_…]` | M03 trends | `aggregates?source&source_id`, `overview`, `sources` |
| `/admin/health/monitoring?trend=<range>` | M06 trends | `aggregates`, `overview`, `sources` |

Query: `window` (900, 3600, 21600 or 86400 s; default 3600), `until` (a past instant: paused),
`account` (an operator filter, never authority), and on M01/M03/M06 `trend` (`6h`, `24h`, `7d`,
`30d`) with an optional `machine_id` that narrows the trend read. A malformed value falls back to live/default; ids
must match the Cloud's opaque-id rule. Refresh, a shared link, Back and Forward land on the same
screen and scope (`health/route.ts`). A screen change moves focus to the page heading; a refresh
or a scope change does not.

## Files

| File | What it is |
| --- | --- |
| `health/stored.ts` | The adapter: v1 envelope, overview, events (record/1 and record/2, with `state` only on `state_after_loss`), sources, outcomes, the incident list, one incident revision and its bundle, read field by field. Refuses `schema_version ≠ 1`, an overall `outcome` other than `unknown`, any list past the Cloud's own bound, a `root_cause` other than `not_derived`, any hypothesis, and a revision chain that is not 1..n. `stageRole` names service stages that wait on downstream. |
| `health/incident-export.ts` | The audited export: read under 1 MiB (reading stops past it), checked against the incident and revision asked for, its bundle re-hashed with WebCrypto over the Cloud's key-sorted JSON; and the acknowledgement POST. |
| `health/incident-screens.tsx` | M05 list and revision view, the export panel and the two-step acknowledgement. |
| `health/cloud.ts` | The loader: `planFor` (fixed reads per screen), `runReads` (concurrency 2, per-read deadline, overall budget, typed failures, refusal cancels everything), `storedReducer` (stale-per-read, refusal clears), cursor paging with a frozen window. `HEALTH_UI_LIMITS`. |
| `health/route.ts` | Addresses and scope. |
| `health/stored-screens.tsx` | The screens. |
| `health/timeline.ts` | Pure mapping from stored reads to charts: the window axis and its bands, per-boot lanes of receipt marks, sequence coverage, source freshness, service source × stage, one operation's ordered stages, the weakest-quality summary and the problem list. Tested by `health/timeline.test.ts`. |
| `health/timeline-view.tsx` | The charts that draw those shapes, each with a chart/table switch, the problem summary and quality strip every screen opens with, and one "requested, not collected yet" panel per screen. |
| `health/stored-export.ts` | The evidence record: listed fields only, ≤ 512 KiB, `not_an_incident_bundle: true`, `incident_bundles: not_included_use_incident_export`. |
| `health/useStored.ts` | One load at a time; 15 s live refresh; cancelled on route or scope change. |
| `health/ConnectionHealth.tsx` | Page controls and whole-page states. Loaded lazily by `AdminGate.tsx`. |
| `health/aggregates.ts` | The trend adapter and chart model (below). |
| `health/trend-view.tsx` | The trend view: lanes, buckets to investigate, stage times, viewer errors, coverage. |
| `health/fixture.ts` | Development-only example answers (see below). |
| `health/stored.test.ts` | Module tests (mocked fetch; run by `npm test`). |
| `health/{types,parse,model,source,useHealth,screens,parts}.ts(x)` | The earlier draft snapshot design (`v0-draft`). `parts.tsx` is shared; the rest is the full-product rendering target and is not reachable from a route until producers and the Cloud can fill it. |

Copy is in `health/words.ts` (English and Taiwan Traditional Chinese); the page reads it from the
public catalogs as `health.<key>` (`tools/check-source-catalog.mjs` keeps them in step). Bounds are
registered as `console.health_ui_*` (`internal/domain/capacity/register_connection_health_ui.go`).

The [2026-11-09 admission correction](performance/api-admission-corrections-2026-11-09.md) makes
the two active read slots shared per Cloud origin within a tab, including attention, audited
exports and investigation destinations, with eight queued reads and abort removal. Queue wait is
inside the existing page deadline/budget. Stored-page and attention refreshes honor a bounded
`Retry-After` (up to 60 seconds). There is no automatic retry of mutations. The separate Cloud
candidate allows four reads per principal and keeps the process-wide ceiling at 16; publication
and production behavior require their own verification.

## Truth rules the code enforces

- The overall verdict is always "unknown — no presence contract". Evidence age (`fresh`/`stale`) is
  shown as evidence age and never as online or offline.
- `null` is unknown, never zero; a count past its cap is shown as "at least n"; a missing per-event
  split is "unknown", not zeros. No group is not success.
- Group states are the Cloud's `cloud-derive/1` words with their `group_quality`/`group_reason`;
  no incident, root cause or locked function is derived from them.
- A read that fails says why in its own panel; the reads that answered stay on screen. A failed
  refresh keeps that read's last answer marked stale with its time. A first load where every read
  failed shows no figures and each reason.
- 401, and 404 `not_found` from the operator surface, end the load, abort the other reads and clear
  every figure before the shell signs in again or says access is refused. There is no owner scope
  on this page: superuser does not grant it, and no owner read is made.
- Producer wall clocks are shown as "display only" and never subtracted; no latency is computed.
  Service `elapsed_ms` is shown as the service's own per-stage time, with stages that wait on
  downstream (`account_dispatch_started`, `storage_started`, `api_dependency_started`) labelled so,
  and `fanout_sent` labelled "handed to socket only". `build_commit` is labelled producer-declared.
- Owner diagnostics, relay/viewer account-scoped stages, end-to-end latency, queues, minute
  aggregates and probes are labelled unsupported with the reason.

## Incidents (M05, and links from M01 and M04)

- The list is the Cloud's, by `opened_at` over the 7 days ending where the page's window ends. An
  expired row shows its id and expiry and no state; `facts: null` is unknown. With incidents
  turned off (`source_coverage.incidents: disabled`) the page says none are being opened, never
  "no incidents". Refusal counters are shown, because an absent incident may be a refused one.
- A revision view separates **what the records prove** (frozen facts, last proven stage, the
  stage still active), **what is missing** (sequence gaps, producer loss counts, facts refused by
  the bound, owner-private fields never collected), **cause** (always "not derived", no
  hypotheses) and **context**. Recovery evidence and human review are two panels: `closed` reads
  "closed by review" with its closure, never healthy, and states never move with time.
- Hash, size, written time, retention of the bundle and of the raw rows, the previous revision
  with its hash, and late evidence are shown. Revisions are links (`?revision=n`, Back/Forward
  restore them); an older one says it is not the latest. `410 incident_expired` shows its
  expiry and nothing from before; `404 revision_not_found` offers the latest.
- **Export** fetches `…/incidents/<id>/export?revision=n`, reads at most 1 MiB, refuses any key
  beyond `schema_version`/`export`/`bundle`, checks the incident id and revision, parses the
  bundle, and recomputes `content_hash` as sha256 over the key-sorted JSON of `bundle` with
  WebCrypto. Only then are the exact received bytes offered for download (`<id>.r<n>.json`) or
  copy. `503 audit_unavailable`, a mismatch or an oversized body keeps nothing. The bytes live in
  the panel of that incident, revision and account; changing any of them unmounts it.
- **Acknowledge** is a button, then a confirmation that says what revision it writes and that it
  does not mean recovery; one `POST …/acknowledge` with the page's cookie, the browser supplying
  the console's `Origin` (`403 bad_origin` is shown as such). No read ever acknowledges.
- 401 and the operator surface's 404 from an export or acknowledgement clear the page as a read's
  would. Owner diagnostics stay unsupported; nothing here grants owner scope.
- M01's incident tile and panel list the open incidents on the first page (active, recovering,
  awaiting review), linking to each. M04 lists the incidents whose group names its correlation
  (first page only, said so) and the producer-declared builds per source and boot in its records.
- The stage note now reads: a completed socket write means only that the local socket
  implementation accepted the bytes; it does not prove delivery or viewer receipt.

## What the charts draw, and what they refuse to

Every screen opens with **What to look at first** (loss, stall, slow and released groups, stale
sources, missing sequence records and failed reads, worst first, with the newest one's machine,
stage and evidence age) and the **weakest evidence** strip: the overall quality is the weakest of
the reads the screen relies on, a failed or stale read or a partial group caps it at partial, and
each read's own quality is listed. It never says more than "only what these reads returned".

- **Time lanes** (M01/M02/M04/M05 groups, M02 machine and M04 records, M03 service records): one
  lane per source and boot, ordered by the Cloud's boot generation (unknown last). Each record or
  group is a glyph at its server receive time. The track between marks is drawn "not observed",
  never healthy. Records outside the query window are counted at the edge ("n earlier/later"), not
  drawn. Bands mark time before the retention floor, time after the read's `as_of`, rows older
  than the oldest held row of a list with more pages ("not loaded"), and listed sequence gaps
  placed between the receipts of their nearest held neighbours (otherwise counted "cannot be
  placed in time"). A record received after a higher sequence of the same boot is marked late;
  a producer monotonic or wall clock that goes backwards against its own sequence is counted per
  boot. Producer clocks are never compared with receive time or across hosts. Clock uncertainty
  is shown, or "unknown".
- **Sources** (M02, M06): each source's newest receipt on the window (fresh or stale glyph, age,
  declared cadence) and each boot from its first to its last receipt as a dotted bracket that
  says not every moment between was observed.
- **Sequence coverage** (M01, M06): received ranges and listed gaps in producer-sequence space,
  per source and boot, with retention floor and drop counters beside it.
- **Service stages** (M03): record/2 rows on the loaded page as source × stage in contract order,
  a bar for each stage's own largest `elapsed_ms` (downstream waits striped, socket handoff
  dotted; never summed, never end-to-end), outcome counts, and one bottleneck: a still-pending
  stage, else a failed one, else the largest own time, with its evidence age from `as_of`. The
  source and source-id selectors are the Cloud's own `source`/`source_id` filter: a change is a
  new address, so held pages are dropped and paging restarts under a cursor bound to that filter.
  The line under them says whether every record of the filtered window is loaded or how many of
  how many pages are held.
- **One operation** (M04): stored records per source and boot in producer order, with sequence
  holes as steps and late arrivals marked. Separate sources are not merged into one order and no
  waterfall or end-to-end time is drawn.
- Unsupported parts (latency, presence, relay/viewer stages, spans, probes) share one
  "requested, not collected yet" panel per screen with their exact limitation.

## Trends (M01, M03, M06): the Cloud's minute rollups

Private contract §15 (Cloud commit `13e7387`), `GET {api}/su/v1/health/aggregates`.
"Raw records (24 h)" and "Trends (up to 30 days)" are two views of the same screen; the trend
view **replaces** the screen's raw reads, so a load is still at most three reads, two in flight,
under the same deadlines and budget — never a fourth read. Each range asks for the only bucket
the Cloud allows for it: `6h` → `1m`, `24h` → `5m`, `7d` → `1h`, `30d` → `1d` (30 daily buckets,
never the raw events read and never a client-side scan). The window is aligned to the bucket and
exactly the Cloud's widest, so its own floor/ceil cannot turn it into `window_too_wide`. The
range is part of the address (a new query, a new chart, a history entry); live refresh and
pause behave as on the raw view. M03's `source`/`source_id` and the trend `machine_id` are sent
as the Cloud's own filters; `account` stays a filter.

The adapter (`parseAggregates`) refuses: a `rule_version` other than `minute-rollup/1`, any
`semantics` value other than the five §15.2 states, `absent_bucket` other than `unknown`, an
overall outcome other than `unknown`, a `bucket_ms` that is not its bucket's, a point that is not
bucket-aligned, inside the window and ascending, a `count_is_floor` that disagrees with
`records_without_count`, a `boots` count that disagrees with its ids, a viewer error outside the
viewer enum, and anything past the Cloud's bounds (50 series, 360 points, 64 cells, 8 boot ids,
200 refusal rows) — as `contract_schema`/`contract_shape`/`contract_bound`, never truncated.

What the view draws, from `trendLanes`, `viewerErrors` and `stageElapsed`:

- One lane per source (`HEALTH_UI_LIMITS.trendLanes`, 12, drawn; the table lists every one), one
  slot per bucket. A held bucket is an orange bar of **accepted event records**; a bucket with a
  stall, slow, failed stage or loss is darker with a mark. A bucket the Cloud did not list is a
  warm-gray "unknown" block, never a zero bar; before the retention floor and before
  `covered_from` (scan cap) are their own darker unknowns; held buckets older than `covered_from`
  are drawn as floors. After `as_of` is blank.
- A bucket whose boot set differs from the previous held one, or that holds several boots, is a
  restart: a dashed break, counted per lane. Producer clocks are only counted
  (`producer_wall_behind/ahead`); time is the Cloud's receipt minute.
- Reported operations (`count_total` of stage-end cells) are shown only for relay/API and viewer
  denominators, "at least" when any record had no count. The daemon's denominator is unknown, so
  it has no operations total and no ratio anywhere; no ratio is computed for any source.
- **Each stage's own reported time** (M03, M06): per source × stage the largest `max_ms` and the
  per-record histogram. Never summed, never end-to-end.
- **Viewer errors**: records per viewer error class over held buckets, with the reported count
  ("at least" when floored).
- **Buckets to investigate** (`trendSignals`, 20, newest first; the rest counted): each links to
  M03's raw records for that source and source id pinned at the bucket's end (only while raw
  records are within their 24 h), and to the incident list pinned there, both in the same account
  scope.
- **What these trends cannot say**: absent = unknown, the clock basis, what the counts are, and
  every `partial_reason`, retention clip, `covered_from`, series beyond 50, dropped cells and
  refused days the answer listed. `aggregates.minute: unsupported` shows "unsupported" and no
  chart. `503 health_store_unavailable` (rollups not migrated) is the trend panel's own failure;
  401/404 still clear the page.

## Bounds (`HEALTH_UI_LIMITS`, registered)

Per read 6 s; per load 12 s; 2 reads in flight (the Cloud's per-principal admission); 3 reads per
load; 1.5 MiB per body (refused unread above); 200 rows per page; 5 pages held (older paging only
while paused, because the Cloud's cursor hashes the window); 512 KiB evidence record; 64-character
ids; 50 incidents per list page over 7 days, 5 list pages held (250 incidents, paused only); 1 MiB
incident export; 12 trend lanes drawn; 20 trend buckets listed to investigate. A parity test
(`internal/domain/capacity/connection_health_ui_test.go`) fails when a `HEALTH_UI_LIMITS` key has
no row or a row disagrees. Revisions (16), facts (200) and
context (20) per revision are the Cloud's bounds, refused past them as `contract_bound`. These are this page's choices against the Cloud's documented bounds, not measurements of
production.

## Development harness

A development build only, with an explicit flag; a production build drops the branch and the module
(checked: `grep fixture_mac_a` over a production build with the flag set finds nothing).

```sh
cd web/console
VITE_HOSTED_CONSOLE='{"v":1,"app_origin":"http://127.0.0.1:5274","api_origin":"http://127.0.0.1:5274","relay_url":"ws://127.0.0.1:5274/v1/connect"}' \
VITE_ADMIN_HEALTH_FIXTURE=normal npx vite --host 127.0.0.1 --port 5274 --strictPort
# open http://127.0.0.1:5274/admin/health
```

Scenarios: `normal`, `partial`, `errors` (outcomes 503, events 429, aggregates 503), `empty`, `slow` (every read
times out), `signed_out`, `forbidden`, `service`, `incidents_disabled`, `audit_fail` (every export
503 `audit_unavailable`), `tampered` (an export whose bundle no longer matches its hash),
`rollups_off` (aggregates `minute: unsupported`), `many_incidents` (124 extra closed list rows, so
the list pages by cursor; opening one of them answers 404). The aggregates answer is in the shape
Cloud `13e7387` serves (§15.2) and applies its own quantization and `window_too_wide`; `normal`
holds a daemon with absent buckets, a restart, a stall/slow bucket, loss and a clock-skew count, a
relay with stage times and a failed stage, and a viewer with `sse_gap` and `auth_refused`;
`partial` is scan-capped with a refused day and dropped cells.

Trend addresses for review: `/admin/health?trend=30d`, `?trend=24h`, `?trend=6h`,
`/admin/health/services?trend=7d&source=relay`, `/admin/health/monitoring?trend=24h`,
`/admin/health?trend=30d&until=<two days ago>` (retention clip), `?trend=30d&machine_id=fixture_mac_a`,
with `normal`, `partial`, `errors`, `empty` and `rollups_off`; incident paging:
`VITE_ADMIN_HEALTH_FIXTURE=many_incidents`, `/admin/health/incidents?until=<a past instant>`. `normal`
holds one incident per state with real canonical hashes: `…a01` active (relay), `…a02` loss
(active), `…a06` recovering (failed stage), `…a03` recovery observed awaiting review (two
revisions), `…a04` closed by review without observed recovery (three revisions; r2 recovering),
`…a05` expired (410). Ids are `hi_f1000000000000000000000000000a0N`. Acknowledging in the harness
writes a closing revision for that tab only. The events read honours `source`/`source_id` and
refuses a cursor from another filter. The harness skips the session, answers in memory
(no request leaves the browser), and shows a fixture banner on every view; the evidence record
carries `fixture: true`. The other admin pages say they need a real session.

## Bundle

Production build, hosted declaration, measured on 2026-10-20 before and after the incident
screens: the lazily loaded `ConnectionHealth` chunk 130.61 kB → 182.92 kB JS (gzip 38.89 →
53.86) and 15.79 → 16.09 kB CSS; `AdminGate` 117.32 → 118.12 kB (gzip 35.08 → 35.36). About half
of the growth is the English and Chinese copy tables in `words.ts`. A production build with
`VITE_ADMIN_HEALTH_FIXTURE=normal` contains none of `fixture_mac_a`, `hi_f1000000`, `hs_f1000000`.

## Not verified, and what the root still owes

- **The charts were not rendered in a browser by the task that drew them.** Owed by
  the root: `VITE_ADMIN_HEALTH_FIXTURE=normal|partial|errors|empty|service` (command above), each
  of `/admin/health`, `/machines`, `/machines/fixture_mac_b` (two boots, a late record, clocks
  stepping back), `/services` (stage bars, bottleneck, source selector), `/trace/fixture-trace-1`,
  `/monitoring`, at desktop and 390 px; Tab through the chart/table switches and the source
  selector. The admin shell is dark-only (`legacy/tokens.css`), so there is no light theme to check.
- **No browser rendered these screens in this task.** Owed: desktop and 390 px, light and dark,
  keyboard-only pass (open M01 → a group → its trace → evidence → copy), screen-reader names,
  focus after route changes, for each fixture scenario, then against a deployed Cloud.
- **The incident screens were not rendered in a browser by the task that built them.** Owed by
  the root, with `VITE_ADMIN_HEALTH_FIXTURE=normal` unless named: `/admin/health` (incident tile
  and open-incident panel), `/admin/health/incidents`, each of
  `/admin/health/incidents/hi_f1000000000000000000000000000a0N` for N = 1, 2, 3, 4, 6, and
  `…a04?revision=1`/`?revision=2` with Back/Forward between them, `…a05` (expired 410),
  `…a03?revision=9` (missing revision), export then download/copy on `…a03`, acknowledge on
  `…a01` (confirm, then the closing revision appears), `audit_fail` and `tampered` exports on
  `…a03`, `incidents_disabled` on `/admin/health/incidents` and `/admin/health`,
  `/admin/health/trace/fixture-trace-1` (incident link and builds),
  `/admin/health/services?source=relay&until=<a past instant>` and `…?source=daemon&source_id=hs_f100000000000000000000000000000a&until=…`
  (load older until "all records … loaded"), `signed_out`/`forbidden` on an incident. Desktop and
  390 px; keyboard only through the revision links, export, download, copy, acknowledge and cancel.
- No production read: the Cloud reads are not deployed. Field names follow the committed Cloud
  branches (2a4ecc78 `4a40b35`, 8de84319 `1414a55`, 35ba675a `fac588c`); a later Cloud change needs
  the adapter updated.
- The incident list loads older pages only while paused (the Cloud's cursor hashes the window);
  live, it says to pause. M01's open-incident tile and M04's incident links still come from the
  first page only.
- Root rendered the actual trend components at desktop and 390 px with explicit fixture data;
  keyboard range changes, table switching and historical source links worked without horizontal
  overflow. Independent local Mongo/Fastify reads from Cloud `cb153af` exercised the four windows
  and viewer filters. The original all-source response containing `external_probe` failed the
  parser; after the probe merge, both original captured all-source and probe-only responses pass.
  Production reads and rollout acceptance remain pending.
- M06's `view=probes` now consumes `/su/v1/health/probes` as one parameterless service-scope read.
  It explicitly ignores historical/account filters; pause stops automatic refresh, while manual
  refresh reads latest evidence. Root rendered missing, delayed, stale, clock-ahead and
  same-fault-domain fixture evidence at desktop and 390 px. Parser/type and local module tests
  passed. A local archived Cloud `cb153af` probe-route response (two hosts, two targets) also
  passed the shipped parser; ten API probe tests passed. Production host enablement remains
  unverified and disabled.
- The fixture's acknowledgement is per tab and in memory; the real route's `Origin` check was not
  exercised against a Cloud.
- Still unavailable: a machine presence contract, measured clock uncertainty, account-attributed
  service evidence, sequence gaps over 30 days (rollups hold counts only), and distinct operations
  over 30 days. Owner-encrypted diagnostics have a separate paired-owner flow; the operator page
  does not gain access to that plaintext. The independent probe code is default off and no
  production probe host has been enabled.
