/* Task-owned example answers for the connection-health fixture harness and the
 * module tests. Every id starts with `fixture`, nothing here came from Cloud or
 * describes a real machine, and it is reachable from the page only in a
 * development build with VITE_ADMIN_HEALTH_FIXTURE set (AdminGate.tsx). It
 * answers in memory: no request leaves the browser.
 *
 * Scenarios: `normal` (also `1`), `partial`, `errors`, `empty`, `slow`,
 * `signed_out`, `forbidden`, `service`; for incidents `incidents_disabled`
 * (the Cloud has them turned off), `audit_fail` (every export answers
 * `503 audit_unavailable`) and `tampered` (an export whose bundle no longer
 * matches its hash). Incident bundles carry real canonical sha256 hashes.
 * Trends (`aggregates`): `normal` holds a daemon with a restart, a stall day, loss and absent
 * buckets, a relay with stage times and a viewer with errors; `partial` is scan-capped with a
 * refused day and dropped cells; `errors` answers `503 health_store_unavailable`; `rollups_off`
 * is a Cloud with rollups turned off (`aggregates.minute: unsupported`). `many_incidents` adds 124
 * closed list rows (ids `hi_f2…`, list only: opening one answers 404) so the incident list pages by
 * the Cloud's cursor, which is bound to the window like the real one.
 * Independent probes (`probes`, Cloud cb153af contract §17.4): `normal` holds a declared-separate
 * host with a current 2xx and a current timeout, a same-fault-domain host whose spool replayed an
 * old observation and reported loss, a stale target, and a registered host that never reported;
 * `partial` is scan-capped (counts are floors, loss unknown) with an observation dated after the
 * read; `empty` is probing off (no host registered); `probes_silent` is a registered host that has
 * sent nothing; `errors` answers `503 health_store_unavailable`. */

export const FIXTURE_SCENARIOS = ["normal", "partial", "errors", "empty", "slow", "signed_out", "forbidden", "service", "incidents_disabled", "audit_fail", "tampered", "rollups_off", "many_incidents", "probes_silent", "investigations_old", "investigations_unavailable"] as const
export type FixtureScenario = (typeof FIXTURE_SCENARIOS)[number]

const MAC_A = "fixture_mac_a"
const MAC_B = "fixture_mac_b"
// Source ids keep the Cloud's shape (`hs_` + 32 hex) so the source filter can name them.
const SRC_A = "hs_f100000000000000000000000000000a"
const SRC_B = "hs_f100000000000000000000000000000b"
const SRC_RELAY = "hs_f100000000000000000000000000000c"
const SRC_RELAY_DO = "hs_f100000000000000000000000000000d"
const SRC_API = "hs_f100000000000000000000000000000e"
const ACCOUNT = "fixture_account"
const BOOT_A = "fixture-boot-a"
const BOOT_B = "fixture-boot-b"
const BUILD = "f1x7e0000000"

type Row = Record<string, unknown>

function envelope(now: number, quality: string, extra: Row, coverage: Row | null): Row {
  const iso = new Date(now).toISOString()
  return {
    schema_version: 1, generated_at: iso, as_of: iso, scope: { kind: "operator", account: null },
    outcome: "unknown", outcome_reason: "no_presence_contract", rule_version: null,
    evidence_quality: quality, partial_reason: null, next_cursor: null, clock_uncertainty_ms: null,
    clock: { basis: "server_received_at", order: "producer_seq", producer_wall: "display_only" },
    owner_diagnostics: "unsupported",
    coverage: { retention_floor: new Date(now - 86_400_000).toISOString(), truncated: false, ...(coverage ?? { sequence: "not_computed", sequence_reason: null, seq_ranges: null, gaps: null, dropped: null }) },
    source_coverage: { daemon: "collected", relay_worker: "service_scope", relay_do: "service_scope", api: "service_scope", viewer: "unsupported", external_probe: "unsupported" },
    ...extra,
  }
}

function events(now: number, scenario: FixtureScenario): Row[] {
  const rows: Row[] = []
  // 240 daemon rows on machine A with a hole at 41–42, so paging and gaps both show.
  for (let seq = 1; seq <= 242; seq++) {
    if (seq === 41 || seq === 42) continue
    const at = now - (243 - seq) * 9_000
    const stall = seq === 230 || seq === 231
    rows.push({
      id: `he_fixture_a_${seq}`, account: ACCOUNT, machine_id: MAC_A, source_id: SRC_A, producer_instance: "daemon",
      schema: "clawdline.connection-health.record/1", producer_seq: seq, event_id: `${BOOT_A}-${seq}`, boot_id: BOOT_A,
      batch_id: `${BOOT_A}-b${Math.ceil(seq / 20)}`, build_commit: BUILD,
      event: seq === 1 ? "boot" : seq === 230 ? "slow_detected" : seq === 231 ? "stall_detected" : seq === 238 ? "recovered" : "scheduling_gap",
      wall: new Date(at - 1_500).toISOString(), mono_ns: seq * 9_000_000_000, source: "daemon",
      stage: stall || seq === 238 ? "handler_started" : null, class: stall || seq === 238 ? "read" : null, kind: stall ? "finite" : null,
      phase_id: stall || seq === 238 ? 7 : null, correlation: stall || seq === 238 ? "fixture-trace-1" : null,
      count: 1, rule: stall ? "watchdog/1" : null, received_at: new Date(at).toISOString(), retain_until: new Date(at + 86_400_000).toISOString(),
    })
  }
  // Machine B: an earlier boot, then a new one whose seq 7 arrives after seq 8 and whose clocks step back at seq 10.
  if (scenario === "normal" || scenario === "partial") {
    for (const [boot, seq, ago, mono, wallAgo, event] of [
      ["fixture-boot-b0", 1, 3_300_000, 1, 3_300_500, "boot"], ["fixture-boot-b0", 2, 3_240_000, 60, 3_240_500, "scheduling_gap"],
      [BOOT_B, 1, 2_400_000, 1, 2_400_500, "boot"], [BOOT_B, 6, 2_100_000, 300, 2_100_500, "slow_detected"],
      [BOOT_B, 8, 1_980_000, 420, 1_980_500, "scheduling_gap"], [BOOT_B, 7, 1_950_000, 400, 1_950_500, "scheduling_gap"],
      [BOOT_B, 10, 1_860_000, 380, 1_990_000, "scheduling_gap"], [BOOT_B, 12, 1_800_000, 600, 1_800_500, "scheduling_gap"],
      [BOOT_B, 13, 1_790_000, 610, 1_790_500, "transitions_lost"], [BOOT_B, 14, 1_780_000, 620, 1_780_500, "state_after_loss"],
    ] as const) {
      const at = now - ago
      rows.push({
        id: `he_fixture_b_${boot}_${seq}`, account: ACCOUNT, machine_id: MAC_B, source_id: SRC_B, producer_instance: "daemon",
        schema: "clawdline.connection-health.record/1", producer_seq: seq, event_id: `${boot}-${seq}`, boot_id: boot, batch_id: null, build_commit: null,
        event, wall: new Date(now - wallAgo).toISOString(), mono_ns: mono * 1_000_000_000, source: "daemon", stage: null, class: null, kind: null,
        phase_id: null, correlation: null, count: 1, rule: null, received_at: new Date(at).toISOString(), retain_until: new Date(at + 86_400_000).toISOString(),
        // The watchdog could not tell what happened across the loss: unverified, not recovered.
        ...(event === "state_after_loss" ? { state: "unverified_after_gap" } : {}),
      })
    }
  }
  if (scenario === "service" || scenario === "normal") {
    const stages = [
      ["relay", "relay-worker", SRC_RELAY, 1, "worker_admitted", "ok", 2, 12, null],
      ["relay", "relay-worker", SRC_RELAY, 2, "account_dispatch_started", "pending", 8_400, 1, "fixture-relay-op"],
      ["relay", "relay-worker", SRC_RELAY, 3, "account_dispatch_returned", "ok", 120, 11, null],
      ["relay", "relay-worker", SRC_RELAY, 4, "outer_settled", "ok", 35, 12, null],
      ["relay", "relay-do", SRC_RELAY_DO, 1, "do_entered", "ok", 1, 11, null],
      ["relay", "relay-do", SRC_RELAY_DO, 2, "storage_started", "ok", 260, 11, null],
      ["relay", "relay-do", SRC_RELAY_DO, 3, "fanout_sent", "zero_target", 0, 11, null],
      ["cloud_api", "api", SRC_API, 1, "api_admitted", "ok", 1, 40, null],
      ["cloud_api", "api", SRC_API, 2, "api_dependency_started", "ok", 640, 40, null],
      ["cloud_api", "api", SRC_API, 3, "api_response_completed", "error", 700, 2, "fixture-api-op"],
    ] as const
    stages.forEach(([source, instance, sourceId, i, stage, outcome, elapsed, count, correlation], n) => {
      const at = now - (60 - n * 2) * 1_000
      const boot = `fixture-${instance}-boot`
      rows.push({
        id: `he_fixture_${instance}_${i}`, account: source === "relay" ? "svc:relay" : "svc:api", machine_id: source === "relay" ? "svc_relay" : "svc_api", source_id: sourceId, producer_instance: instance,
        schema: "clawdline.connection-health.record/2", producer_seq: i, event_id: `${boot}-${i}`, boot_id: boot,
        batch_id: `${boot}-b1`, build_commit: null, event: outcome === "pending" ? "stall_detected" : "stage_completed",
        wall: new Date(at).toISOString(), mono_ns: i * 1_000_000, source, stage, class: null, kind: null, phase_id: null,
        correlation, count, rule: null, outcome, elapsed_ms: elapsed, received_at: new Date(at).toISOString(), retain_until: new Date(at + 86_400_000).toISOString(),
      })
    })
  }
  return rows.sort((a, b) => String(b.received_at).localeCompare(String(a.received_at)))
}

const coverageOf = () => ({
  sequence: "exact", sequence_reason: null,
  seq_ranges: [
    { source_id: SRC_A, machine_id: MAC_A, boot_id: BOOT_A, from: 1, to: 40 },
    { source_id: SRC_A, machine_id: MAC_A, boot_id: BOOT_A, from: 43, to: 242 },
  ],
  gaps: [{ source_id: SRC_A, machine_id: MAC_A, boot_id: BOOT_A, from: 41, to: 42, count: 2, cause: "unexplained" }],
  gap_edges: "not_inferred", dropped: { transitions_lost: 0, overflow: 0 },
})

function sources(now: number): Row[] {
  const boot = (id: string, gen: number, last: number) => ({
    boot_id: id, server_generation: gen, build_commit: BUILD, first_received_at: new Date(now - 3_600_000).toISOString(),
    last_received_at: new Date(last).toISOString(), last_wall_at: new Date(last - 1_500).toISOString(), last_seq: 242, events_accepted: 240, events_rejected: 0,
  })
  return [
    {
      source_id: SRC_A, account: ACCOUNT, machine_id: MAC_A, producer_instance: "daemon", source: "daemon", boot_id: BOOT_A,
      boot_basis: "latest_receipt", boot_order: "single_boot", build_commit: BUILD, build_provenance: "producer_declared",
      first_seen_at: new Date(now - 3_600_000).toISOString(), last_received_at: new Date(now - 9_000).toISOString(),
      last_wall_at: new Date(now - 10_500).toISOString(), last_receipt_age_ms: 9_000, freshness: "fresh", expected_every_ms: null,
      last_seq: 242, events_accepted: 240, events_rejected: 0, clock_offset_ms: 1_500, boots: [boot(BOOT_A, 1, now - 9_000)], boots_evicted: 0,
      retain_until: new Date(now + 86_400_000).toISOString(),
    },
    {
      source_id: SRC_B, account: ACCOUNT, machine_id: MAC_B, producer_instance: "daemon", source: "daemon", boot_id: BOOT_B,
      boot_basis: "latest_receipt", boot_order: "unverified", build_commit: null, build_provenance: "producer_declared",
      first_seen_at: new Date(now - 7_200_000).toISOString(), last_received_at: new Date(now - 1_800_000).toISOString(),
      last_wall_at: null, last_receipt_age_ms: 1_800_000, freshness: "stale", expected_every_ms: null,
      last_seq: 12, events_accepted: 12, events_rejected: 1, clock_offset_ms: null,
      boots: [boot(BOOT_B, 2, now - 1_800_000), boot("fixture-boot-b0", 1, now - 5_000_000)], boots_evicted: 0,
      retain_until: new Date(now + 84_600_000).toISOString(),
    },
  ]
}

function groups(now: number): Row[] {
  return [{
    account: ACCOUNT, machine_id: MAC_A, source_id: SRC_A, boot_id: BOOT_A, phase_id: 7, correlation: "fixture-trace-1",
    group_outcome: "recovered", group_quality: "complete", group_reason: null, last_event: "recovered", last_stage: "handler_started",
    class: "read", first_seq: 230, last_seq: 238, events: 3, dropped: null, last_received_at: new Date(now - 45_000).toISOString(),
    evidence_age_ms: 45_000, last_wall: new Date(now - 46_500).toISOString(), basis: { time: "server_received_at", order: "producer_seq", rule_version: "cloud-derive/1" },
  }, {
    account: "svc:relay", machine_id: "svc_relay", source_id: SRC_RELAY, boot_id: "fixture-relay-boot", phase_id: null, correlation: "fixture-relay-op",
    group_outcome: "stall_detected", group_quality: "partial", group_reason: "sequence_gap_after", last_event: "stall_detected", last_stage: "account_dispatch_started",
    class: null, first_seq: 1, last_seq: 1, events: 1, dropped: null, last_received_at: new Date(now - 39_000).toISOString(),
    evidence_age_ms: 39_000, last_wall: new Date(now - 39_000).toISOString(), basis: { time: "server_received_at", order: "producer_seq", rule_version: "cloud-derive/1" },
  }]
}

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
const refusal = (status: number, code: string) => json(status, { error: { code, message: code } })

/* ---- trends: the shape of `GET /su/v1/health/aggregates` (Cloud 13e7387, contract §15.2) ---- */

const ROLLUP_BUCKETS: Record<string, { ms: number; max: number }> = {
  "1m": { ms: 60_000, max: 6 * 3_600_000 }, "5m": { ms: 300_000, max: 24 * 3_600_000 },
  "1h": { ms: 3_600_000, max: 7 * 86_400_000 }, "1d": { ms: 86_400_000, max: 30 * 86_400_000 },
}
const ELAPSED_BOUNDS = [10, 50, 100, 250, 500, 1_000, 2_500, 5_000, 10_000, 30_000, 60_000]

function rollCell(event: string, stage: string | null, cls: string | null, outcome: string | null, error: string | null, records: number, count: number | null, elapsedMs: number | null): Row {
  const buckets = ELAPSED_BOUNDS.map(() => 0).concat([0])
  if (elapsedMs !== null) buckets[ELAPSED_BOUNDS.findIndex((b) => elapsedMs <= b) === -1 ? ELAPSED_BOUNDS.length : ELAPSED_BOUNDS.findIndex((b) => elapsedMs <= b)] = records
  return {
    event, stage, class: cls, outcome, error, records, count_total: count === null ? 0 : count * records, count_is_floor: count === null, records_without_count: count === null ? records : 0,
    elapsed: elapsedMs === null ? null : { records, max_ms: elapsedMs, bounds_ms: ELAPSED_BOUNDS, buckets, basis: "per_record_reported_elapsed" },
  }
}

/** Which buckets of a series hold a rollup: deterministic in the bucket index, with holes. */
function rollPoints(kind: "daemon" | "relay" | "viewer", from: number, to: number, ms: number, now: number): Row[] {
  const out: Row[] = []
  const n = Math.round((to - from) / ms)
  for (let i = 0; i < n; i++) {
    const start = from + i * ms
    if (start > now) break
    // Holes: every seventh daemon bucket (none of the last six, which hold the signals) and every
    // fifth viewer bucket were never rolled up: unknown.
    if (kind === "daemon" && i % 7 === 5 && i < n - 6) continue
    if (kind === "viewer" && i % 5 === 1) continue
    const late = i >= Math.floor(n * 0.6)
    const cells: Row[] = []
    if (kind === "daemon") {
      cells.push(rollCell("scheduling_gap", null, null, null, null, 4 + (i % 3), 1, null))
      if (i === n - 4) cells.push(rollCell("stall_detected", "handler_started", "read", null, null, 1, null, null), rollCell("slow_detected", "handler_started", "read", null, null, 1, null, null))
    } else if (kind === "relay") {
      cells.push(rollCell("stage_completed", "account_dispatch_started", null, "ok", null, 3, 40, 120 + (i % 4) * 90))
      cells.push(rollCell("stage_completed", "fanout_sent", null, "ok", null, 3, 40, 8))
      if (i === n - 2) cells.push(rollCell("stage_failed", "storage_started", null, "timeout", null, 1, 1, 5_200))
    } else {
      cells.push(rollCell("viewer_stage_completed", "viewer_request_issued", null, "ok", null, 2, 12, 340))
      if (i % 4 === 2) cells.push(rollCell("viewer_stage_failed", "viewer_sse_resume", null, "error", "sse_gap", 1, 1, 2_100))
      if (i === n - 3) cells.push(rollCell("viewer_stage_failed", "viewer_connect", null, "error", "auth_refused", 1, null, null))
    }
    const boots = kind === "daemon" ? [late ? BOOT_B : BOOT_A] : kind === "relay" ? ["fixture-relay-boot"] : ["fixture-viewer-boot"]
    out.push({
      start: new Date(start).toISOString(), boots: boots.length, boot_ids: boots, boot_ids_truncated: false,
      batches: 1 + (i % 2), records: cells.reduce((a, c) => a + (c.records as number), 0), duplicates_refused: kind === "daemon" && i === 5 ? 1 : 0,
      producer_wall_behind: kind === "daemon" && i === n - 6 ? 2 : 0, producer_wall_ahead: 0, loss_reported: kind === "daemon" && i === n - 5 ? 6 : 0, cells_dropped: 0,
      cells: cells.sort((a, b) => `${a.event}|${a.stage}`.localeCompare(`${b.event}|${b.stage}`)),
    })
  }
  return out
}

function aggregatesAnswer(scenario: FixtureScenario, q: URLSearchParams, now: number): Response {
  if (scenario === "errors") return refusal(503, "health_store_unavailable")
  for (const k of q.keys()) if (!["from", "to", "bucket", "machine_id", "source", "source_id", "event", "stage", "class", "outcome", "error", "account"].includes(k)) return refusal(400, "unknown_field")
  const bucket = q.get("bucket") ?? "1h"
  const spec = ROLLUP_BUCKETS[bucket]
  if (!spec) return refusal(400, "invalid_field")
  // The Cloud's own quantization (§15.2): from floors, to ceils and is at most the current bucket.
  const nowEnd = Math.ceil((now + 1) / spec.ms) * spec.ms
  const to = Math.min(Math.ceil((Date.parse(q.get("to") ?? "") || now) / spec.ms) * spec.ms, nowEnd)
  const from = Math.floor((Date.parse(q.get("from") ?? "") || to - spec.max) / spec.ms) * spec.ms
  if (from >= to) return refusal(400, "invalid_window")
  if (to - from > spec.max) return refusal(400, "window_too_wide")
  const floor = (Math.floor((now - 30 * 86_400_000 - 3_600_000) / 3_600_000) + 1) * 3_600_000
  const off = scenario === "rollups_off" || scenario === "empty"
  const partial = scenario === "partial"
  const all = off ? [] : [
    { source_id: SRC_A, machine_id: MAC_A, source: "daemon", producer_instance: "daemon", denominator: "unknown", kind: "daemon" as const, loss: { storage_lost_records: 6, store_gaps: 1, excluded: 0 } },
    { source_id: SRC_RELAY, machine_id: "svc_relay", source: "relay", producer_instance: "relay_worker", denominator: "service_reported_operations", kind: "relay" as const, loss: { storage_lost_records: 0, store_gaps: 0, excluded: 0 } },
    { source_id: "hs_f100000000000000000000000000000f", machine_id: MAC_B, source: "viewer", producer_instance: "viewer", denominator: "viewer_reported_stage_ends", kind: "viewer" as const, loss: { storage_lost_records: 0, store_gaps: 0, excluded: 0 } },
  ]
  const wanted = all.filter((x) => (!q.get("machine_id") || x.machine_id === q.get("machine_id")) && (!q.get("source") || x.source === q.get("source")) && (!q.get("source_id") || x.source_id === q.get("source_id")))
  const clipped = from < floor
  const coveredFrom = partial ? from + Math.floor((to - from) / spec.ms / 3) * spec.ms : clipped ? floor : from
  const series = wanted.map((x) => ({
    account: x.source === "daemon" || x.source === "viewer" ? ACCOUNT : "svc:relay", source_id: x.source_id, machine_id: x.machine_id, source: x.source, producer_instance: x.producer_instance, denominator: x.denominator,
    producer_reported_loss: { ...x.loss, attribution: "first_received_minute", provenance: "producer_declared", counters: "monotonic" },
    points: rollPoints(x.kind, from, to, spec.ms, now).filter((p) => Date.parse(p.start as string) >= (clipped ? floor : from)),
  }))
  const reason = partial ? "scan_cap" : clipped ? "retention_floor" : series.some((s) => s.points.some((p) => (p.loss_reported as number) > 0)) ? "producer_reported_loss" : null
  const quality = series.length === 0 ? "not_collected" : reason !== null ? "partial" : "complete"
  return json(200, {
    ...envelope(now, quality, {
      window: { from: new Date(from).toISOString(), to: new Date(to).toISOString(), bucket, bucket_ms: spec.ms, basis: "server_received_at" },
      partial_reason: reason,
      source_coverage: { daemon: series.length ? "collected" : "not_collected", relay_worker: "service_scope", relay_do: "service_scope", api: "service_scope", viewer: "unsupported", external_probe: "unsupported" },
      aggregates: off && scenario === "rollups_off" ? { minute: "unsupported", retention_days: null } : { minute: "collected", retention_days: 30, rule_version: "minute-rollup/1", route: "aggregates" },
      series, series_total: series.length,
    }, {
      truncated: partial, retention_floor: new Date(floor).toISOString(), retention_clipped: clipped, covered_from: new Date(coveredFrom).toISOString(),
      sequence: "not_computed", sequence_reason: "aggregates_hold_counts_only", seq_ranges: null, gaps: null, dropped: null, absent_bucket: "unknown",
      refusals: { rows: partial ? [{ account: ACCOUNT, day: new Date(Math.floor((to - spec.ms) / 86_400_000) * 86_400_000).toISOString(), reason: "account_bytes", batches: 3 }] : [], truncated: false },
      cells_dropped: partial ? 4 : 0, cell_filter: "none",
    }),
    rule_version: "minute-rollup/1",
    semantics: { counts: "accepted_event_records", distinct_operations: "not_aggregated", elapsed: "per_record_reported_elapsed_not_end_to_end", boots: "never_merged_in_storage", clock: "server_received_at_minute" },
  })
}

/* ---- independent probes: the shape of `GET /su/v1/health/probes` (Cloud cb153af, §17.4) ---- */

const PROBE_NOT_PROVEN = ["application_handler", "viewer_apply", "account_health", "dns_phase", "tls_phase"]
const PROBE_FRESH_MS = 600_000

function probeTarget(now: number, o: { host: string; target: string; stage: "probe_api_readiness" | "probe_relay_reachability"; viewpoint: string; independence: string; event: "probe_completed" | "probe_failed"; outcome: string; http: string | null; receivedAgo: number; observedAgo: number; results: number; failures: number; skipped: number; floor: boolean }): Row {
  const fresh = o.receivedAgo <= PROBE_FRESH_MS && o.observedAgo >= 0 && o.observedAgo <= PROBE_FRESH_MS
  return {
    producer_instance: o.host, target_id: o.target, stage: o.stage, viewpoint: o.viewpoint, independence: o.independence,
    independence_basis: "operator_declared_unverified",
    latest_result: {
      event: o.event, target_id: o.target, viewpoint: o.viewpoint, independence: o.independence, outcome: o.outcome, http_class: o.http,
      elapsed_ms: o.outcome === "timeout" ? 5_000 : 48, dns_phase: "unknown", tls_phase: "unknown",
      wall: new Date(now - o.observedAgo).toISOString(), received_at: new Date(now - o.receivedAgo).toISOString(),
    },
    evidence_age_ms: o.receivedAgo, observation_age_ms: o.observedAgo, observation_clock: "producer_wall_unverified",
    freshness: fresh ? "fresh" : "stale",
    status: !fresh ? "unknown" : o.event === "probe_completed" ? "answered_2xx" : o.outcome,
    proves: o.stage === "probe_api_readiness" ? "api_readiness_route_answered_from_viewpoint" : "relay_health_route_answered_from_viewpoint",
    does_not_prove: PROBE_NOT_PROVEN,
    results_in_window: o.results, failures_in_window: o.failures, overlaps_skipped_in_window: o.skipped, counts_are_floor: o.floor,
  }
}

function probeHost(now: number, instance: string, viewpoint: string, ago: number | null, loss: Row | null): Row {
  return {
    producer_instance: instance, registered: true, registered_viewpoint: viewpoint,
    last_received_at: ago === null ? null : new Date(now - ago).toISOString(), last_receipt_age_ms: ago,
    freshness: ago === null ? "missing" : ago <= PROBE_FRESH_MS ? "fresh" : "stale",
    status: ago !== null && ago <= PROBE_FRESH_MS ? "reporting" : "unknown", producer_reported_loss: loss,
  }
}

function probesAnswer(scenario: FixtureScenario, now: number): Response {
  if (scenario === "errors") return refusal(503, "health_store_unavailable")
  const off = scenario === "empty"
  const silent = scenario === "probes_silent"
  const capped = scenario === "partial"
  const none = { spool_dropped: 0, spool_expired: 0, upload_rejected: 0 }
  const hosts = off ? [] : silent ? [probeHost(now, "fixture_probe_a", "fixture_vp_separate", null, none)] : [
    probeHost(now, "fixture_probe_a", "fixture_vp_separate", 40_000, capped ? null : none),
    probeHost(now, "fixture_probe_b", "fixture_vp_shared", 90_000, capped ? null : { spool_dropped: 3, spool_expired: 0, upload_rejected: 1 }),
    probeHost(now, "fixture_probe_c", "fixture_vp_spare", null, capped ? null : none),
  ]
  const A = { host: "fixture_probe_a", viewpoint: "fixture_vp_separate", independence: "declared_separate", floor: capped }
  const B = { host: "fixture_probe_b", viewpoint: "fixture_vp_shared", independence: "same_fault_domain", floor: capped }
  const targets = off || silent ? [] : [
    probeTarget(now, { ...A, target: "fixture_api", stage: "probe_api_readiness", event: "probe_completed", outcome: "ok", http: "2xx", receivedAgo: 40_000, observedAgo: 41_000, results: 1_380, failures: 2, skipped: 1 }),
    probeTarget(now, { ...A, target: "fixture_relay", stage: "probe_relay_reachability", event: "probe_failed", outcome: "timeout", http: null, receivedAgo: 40_000, observedAgo: 41_000, results: 1_380, failures: 17, skipped: 0 }),
    probeTarget(now, { ...B, target: "fixture_api", stage: "probe_api_readiness", event: "probe_completed", outcome: "ok", http: "2xx", receivedAgo: 90_000, observedAgo: 2_400_000, results: 610, failures: 0, skipped: 4 }),
    probeTarget(now, { ...B, target: "fixture_relay", stage: "probe_relay_reachability", event: "probe_failed", outcome: "http_error", http: "5xx", receivedAgo: 3_600_000, observedAgo: 3_601_000, results: 604, failures: 31, skipped: 0 }),
    ...(capped ? [probeTarget(now, { ...A, target: "fixture_clock", stage: "probe_api_readiness", event: "probe_completed", outcome: "ok", http: "2xx", receivedAgo: 30_000, observedAgo: -120_000, results: 12, failures: 0, skipped: 0 })] : []),
  ]
  const coverage = off ? "unsupported" : "service_scope"
  const quality = off ? "unsupported" : silent ? "not_collected" : "partial"
  return json(200, {
    ...envelope(now, quality, {
      window: { from: new Date(now - 86_400_000).toISOString(), to: new Date(now).toISOString() },
      partial_reason: off || silent ? null : capped ? "scan_cap" : "producer_reported_loss",
      probe: { schema: "clawdline.external-probe.record/1", fresh_within_ms: PROBE_FRESH_MS, registered_hosts: hosts.length, unsupported: ["target_registry", "dns_tls_phases", "independence_verification", "account_attribution"] },
      hosts, targets,
    }, null),
    coverage: { retention_floor: new Date(now - 86_400_000).toISOString(), truncated: capped, sequence: "not_computed", sequence_reason: null, seq_ranges: null, gaps: null, dropped: null },
    source_coverage: { daemon: "not_collected", relay_worker: "service_scope", relay_do: "service_scope", api: "service_scope", viewer: "unsupported", external_probe: coverage },
  })
}

/** Answers one URL from the fixture. Exported for the module tests. */
export async function fixtureAnswer(scenario: FixtureScenario, url: string, now: number, method = "GET", ivRequest?: { key: string | null; body: string }): Promise<Response> {
  if (scenario === "signed_out") return refusal(401, "unauthorized")
  if (scenario === "forbidden") return refusal(404, "not_found")
  const u = new URL(url, "https://fixture.invalid")
  const path = u.pathname.replace(/^.*\/su\/v1\/health\//, "")
  if (path === "investigation-destinations") return destinationsAnswer()
  const q = u.searchParams
  const empty = scenario === "empty"
  if (path === "aggregates") return aggregatesAnswer(scenario, q, now)
  if (path === "probes") return probesAnswer(scenario, now)
  if (path === "overview") {
    const all = empty ? [] : events(now, scenario)
    const by: Record<string, number> = {}
    for (const e of all) by[String(e.event)] = (by[String(e.event)] ?? 0) + 1
    return json(200, envelope(now, empty ? "not_collected" : scenario === "partial" ? "partial" : "complete", {
      window: { from: q.get("from"), to: q.get("to") }, partial_reason: scenario === "partial" ? "count_cap" : null,
      events: { total: empty ? null : all.length, total_is_floor: scenario === "partial", by_event: empty || scenario === "partial" ? null : by },
      sources: { listed: empty ? 0 : 2, more: false, fresh: empty ? null : 1, stale: empty ? null : 1 },
      incidents: { coverage: scenario === "incidents_disabled" || empty ? "disabled" : "collected" },
    }, empty ? { sequence: "not_collected", sequence_reason: null, seq_ranges: null, gaps: null, dropped: null } : coverageOf()))
  }
  if (path === "outcomes") {
    if (scenario === "errors") return refusal(503, "health_store_unavailable")
    return json(200, { ...envelope(now, empty ? "not_collected" : "partial", {
      window: { from: q.get("from"), to: q.get("to") }, partial_reason: empty ? null : "sequence_coverage",
      groups: empty ? null : groups(now).filter((g) => !q.get("machine_id") || g.machine_id === q.get("machine_id")), ungrouped: empty ? null : { boot: 1, scheduling_gap: 233 },
    }, empty ? null : coverageOf()), rule_version: "cloud-derive/1" })
  }
  if (path === "sources") {
    const rows = empty ? [] : sources(now).filter((s) => !q.get("machine_id") || s.machine_id === q.get("machine_id"))
    return json(200, envelope(now, rows.length ? "complete" : "not_collected", { machines: null, sources: rows }, null))
  }
  if (path === "events" || path.startsWith("trace/")) {
    if (scenario === "errors" && path === "events") return refusal(429, "health_busy")
    const correlation = path.startsWith("trace/") ? decodeURIComponent(path.slice(6)) : null
    const source = q.get("source")
    const sourceId = q.get("source_id")
    // The Cloud's own validation of the filter (contract §3).
    if (source !== null && !["daemon", "cloud_api", "relay", "viewer", "collector"].includes(source)) return refusal(400, "invalid_field")
    if (sourceId !== null && !/^hs_[0-9a-f]{32}$/.test(sourceId)) return refusal(400, "invalid_field")
    let rows = empty ? [] : events(now, scenario)
    if (correlation) rows = rows.filter((e) => e.correlation === correlation)
    if (q.get("machine_id")) rows = rows.filter((e) => e.machine_id === q.get("machine_id"))
    if (source !== null) rows = rows.filter((e) => e.source === source)
    if (sourceId !== null) rows = rows.filter((e) => e.source_id === sourceId)
    const limit = Math.min(200, Number(q.get("limit") ?? 100))
    // The cursor binds the filter and window, as the Cloud's does: another filter's cursor is refused.
    const bind = [path, q.get("from"), q.get("to"), q.get("machine_id"), source, sourceId].join("|")
    let offset = 0
    if (q.get("cursor")) {
      const [at, bound] = atob(q.get("cursor")!.replace(/-/g, "+").replace(/_/g, "/")).split("\n")
      if (bound !== bind) return refusal(400, "bad_cursor")
      offset = Number(at)
    }
    const page = rows.slice(offset, offset + limit)
    const more = offset + limit < rows.length
    const cursor = more ? btoa(`${offset + limit}\n${bind}`).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") : null
    return json(200, envelope(now, empty ? "not_collected" : more ? "partial" : "complete", {
      window: { from: q.get("from"), to: q.get("to") }, partial_reason: more ? "row_limit" : null, next_cursor: cursor, events: page,
    }, empty ? null : coverageOf()))
  }
  if (path === "incidents" || path.startsWith("incidents/")) return incidentAnswer(scenario, path, q, now, method, ivRequest)
  return refusal(404, "not_found")
}

/* ---- incidents (contract §11) ------------------------------------------------ */

const INC = {
  active: "hi_f1000000000000000000000000000a01",
  loss: "hi_f1000000000000000000000000000a02",
  recovering: "hi_f1000000000000000000000000000a06",
  review: "hi_f1000000000000000000000000000a03",
  closed: "hi_f1000000000000000000000000000a04",
  expired: "hi_f1000000000000000000000000000a05",
} as const
export const FIXTURE_INCIDENTS = INC

async function sha256(text: string): Promise<string> {
  const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text))
  return Array.from(new Uint8Array(d), (b) => b.toString(16).padStart(2, "0")).join("")
}
function canonical(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canonical).join(",")}]`
  if (v && typeof v === "object") return `{${Object.keys(v).sort().map((k) => `${JSON.stringify(k)}:${canonical((v as Row)[k])}`).join(",")}}`
  return JSON.stringify(v)
}
const hashOf = async (v: unknown) => `sha256:${await sha256(canonical(v))}`

type Spec = {
  id: string; machine: string; source: string; sourceId: string; instance: string; boot: string
  group: Row; trigger: "stall" | "slow" | "loss"; openedAgo: number
  /** Per revision: the facts it holds (seq, event, stage, outcome) and its state. */
  revisions: { facts: [number, string, string | null][]; state: string; recovery: Row; reason: "opened" | "new_evidence" | "acknowledged"; gaps: Row[]; exactness: "exact" | "unverified"; closure?: string; ackSeq?: number }[]
}

const none = { observed: false, event_key: null, event: null, received_at: null, unverified_reason: null }

function specs(): Spec[] {
  const op = (correlation: string | null, phase: number | null) => ({ rule: "incident-group/1", kind: "operation", correlation, phase_id: phase })
  return [
    { id: INC.active, machine: "svc_relay", source: "relay", sourceId: SRC_RELAY, instance: "relay-worker", boot: "fixture-relay-worker-boot", group: op("fixture-relay-op", null), trigger: "stall", openedAgo: 40_000,
      revisions: [{ facts: [[2, "stall_detected", "account_dispatch_started"]], state: "active", recovery: none, reason: "opened", gaps: [], exactness: "exact" }] },
    { id: INC.loss, machine: MAC_B, source: "daemon", sourceId: SRC_B, instance: "daemon", boot: BOOT_B, group: { rule: "incident-group/1", kind: "loss", correlation: null, phase_id: null }, trigger: "loss", openedAgo: 1_790_000,
      revisions: [{ facts: [[13, "transitions_lost", null], [14, "state_after_loss", null]], state: "active", recovery: none, reason: "opened", gaps: [{ from: 11, to: 11, count: 1, cause: "unexplained" }], exactness: "exact" }] },
    { id: INC.recovering, machine: "svc_api", source: "cloud_api", sourceId: SRC_API, instance: "api", boot: "fixture-api-boot", group: op("fixture-api-op", null), trigger: "slow", openedAgo: 50_000,
      revisions: [
        { facts: [[2, "slow_detected", "api_dependency_started"]], state: "active", recovery: none, reason: "opened", gaps: [], exactness: "exact" },
        { facts: [[2, "slow_detected", "api_dependency_started"], [3, "stage_failed", "api_response_completed"]], state: "recovering", recovery: { observed: false, event: "stage_failed", unverified_reason: "ended_failed" }, reason: "new_evidence", gaps: [], exactness: "exact" },
      ] },
    { id: INC.review, machine: MAC_A, source: "daemon", sourceId: SRC_A, instance: "daemon", boot: BOOT_A, group: op("fixture-trace-1", 7), trigger: "stall", openedAgo: 120_000,
      revisions: [
        { facts: [[230, "slow_detected", "handler_started"], [231, "stall_detected", "handler_started"]], state: "active", recovery: none, reason: "opened", gaps: [], exactness: "exact" },
        { facts: [[230, "slow_detected", "handler_started"], [231, "stall_detected", "handler_started"], [238, "recovered", "handler_started"]], state: "recovered_awaiting_review", recovery: { observed: true, event: "recovered" }, reason: "new_evidence", gaps: [], exactness: "exact" },
      ] },
    { id: INC.closed, machine: MAC_B, source: "daemon", sourceId: SRC_B, instance: "daemon", boot: BOOT_B, group: op(null, 3), trigger: "slow", openedAgo: 2_100_000,
      revisions: [
        { facts: [[6, "slow_detected", "socket_write"]], state: "active", recovery: none, reason: "opened", gaps: [], exactness: "exact" },
        { facts: [[6, "slow_detected", "socket_write"], [10, "released_after_stall", "socket_write"]], state: "recovering", recovery: { observed: false, event: "released_after_stall", unverified_reason: "released_outcome_unknown" }, reason: "new_evidence", gaps: [{ from: 9, to: 9, count: 1, cause: "unexplained" }], exactness: "exact" },
        { facts: [[6, "slow_detected", "socket_write"], [10, "released_after_stall", "socket_write"]], state: "closed", recovery: { observed: false, event: "released_after_stall", unverified_reason: "released_outcome_unknown" }, reason: "acknowledged", gaps: [{ from: 9, to: 9, count: 1, cause: "unexplained" }], exactness: "exact", closure: "acknowledged_without_observed_recovery", ackSeq: 10 },
      ] },
    { id: INC.expired, machine: MAC_A, source: "daemon", sourceId: SRC_A, instance: "daemon", boot: BOOT_A, group: op(null, 2), trigger: "slow", openedAgo: 6.9 * 86_400_000,
      revisions: [{ facts: [[5, "slow_detected", "handler_started"]], state: "active", recovery: none, reason: "opened", gaps: [], exactness: "exact" }] },
  ]
}

/** Incident evidence is frozen: its times come from the first incident answer, not each refresh. */
let incidentEpoch: number | null = null

/** Acknowledgements made in this tab, so the harness shows the new closing revision. */
const acknowledgedHere = new Set<string>()

function withAck(spec: Spec): Spec {
  if (!acknowledgedHere.has(spec.id)) return spec
  const last = spec.revisions[spec.revisions.length - 1]
  if (last.state === "closed") return spec
  const through = Math.max(...last.facts.map((f) => f[0]))
  return { ...spec, revisions: [...spec.revisions, { ...last, state: "closed", reason: "acknowledged", closure: (last.recovery as { observed?: boolean }).observed ? "acknowledged_after_observed_recovery" : "acknowledged_without_observed_recovery", ackSeq: through }] }
}

async function bundleOf(spec: Spec, n: number, now: number): Promise<{ bundle: Row; hash: string; bytes: number; created: string }> {
  const opened = now - spec.openedAgo
  const rv = spec.revisions[n - 1]
  const at = (seq: number) => new Date(opened + seq * 500).toISOString()
  const factRow = ([seq, event, stage]: [number, string, string | null]) => ({
    id: `he_${spec.id.slice(3, 11)}${String(seq).padStart(32, "0")}`, schema: "clawdline.connection-health.record/1", event_id: `${spec.boot}-${seq}`, producer_seq: seq,
    boot_id: spec.boot, batch_id: null, event, source: spec.source, stage, class: stage ? "read" : null, kind: event === "stall_detected" || event === "slow_detected" ? "finite" : null,
    phase_id: spec.group.phase_id, correlation: spec.group.correlation, count: 1, rule: event.endsWith("_detected") ? "watchdog/1" : null,
    outcome: null, elapsed_ms: null, state: event === "state_after_loss" ? "unverified_after_gap" : null, build_commit: spec.source === "daemon" ? BUILD : null,
    mono_ns: seq * 1_000_000, wall: new Date(opened + seq * 500 - 1_500).toISOString(), received_at: at(seq), digest: `sha256:${"0".repeat(63)}${seq % 10}`,
  })
  const facts = rv.facts.map(factRow)
  const prev = n > 1 ? await bundleOf(spec, n - 1, now) : null
  const prevFacts = new Set(n > 1 ? spec.revisions[n - 2].facts.map((f) => f[0]) : [])
  const added = rv.facts.filter((f) => !prevFacts.has(f[0])).map((f) => ({ id: factRow(f).id, producer_seq: f[0], received_at: at(f[0]) }))
  const seqs = rv.facts.map((f) => f[0]).sort((a, b) => a - b)
  const recovery = rv.recovery === none ? none : { ...none, ...rv.recovery, event_key: facts[facts.length - 1].id, received_at: facts[facts.length - 1].received_at }
  const created = rv.reason === "acknowledged" ? new Date(now - 5_000).toISOString() : facts[facts.length - 1].received_at
  const bundle = {
    manifest: {
      schema: "clawdline.health-incident-bundle/1", incident_id: spec.id, revision: n,
      rule_versions: { grouping: "incident-group/1", state: "incident-state/1" },
      scope: { machine_id: spec.machine, source_id: spec.sourceId, producer_instance: spec.instance, source: spec.source, boot_id: spec.boot, group: spec.group },
      window: { from: facts[0].received_at, to: facts[facts.length - 1].received_at, basis: "server_received_at" },
      build: { commits: spec.source === "daemon" ? [BUILD] : [], provenance: "producer_declared" },
      sources: [{ source_id: spec.sourceId, source: spec.source, producer_instance: spec.instance, boot_id: spec.boot, boot_order: spec.machine === MAC_B ? "unverified" : "single_boot" }],
      sequence: { exactness: rv.exactness, reason: null, present: seqs.map((x) => ({ from: x, to: x })), gaps: rv.gaps, gap_edges: "not_inferred" },
      dropped: { transitions_lost: spec.trigger === "loss" ? 1 : 0, overflow: 0, facts_refused_by_bound: 0 },
      retention: { raw_retain_until_earliest: new Date(opened + 86_400_000).toISOString(), bundle_retain_until: new Date(opened + 7 * 86_400_000).toISOString(), frozen: "facts_and_context_copied" },
      clock: { uncertainty_ms: null, basis: "server_received_at", order: "producer_seq", producer_wall: "display_only" },
      redaction: { class: "content_free_metadata", owner_diagnostics: "refused_at_ingest", absent_fields: ["location", "age_ns", "deadline_ns"], payloads_paths_names: "never_accepted" },
      provenance: { reason: rv.reason, previous_revision: n > 1 ? n - 1 : null, previous_content_hash: prev?.hash ?? null, added: rv.reason === "acknowledged" ? [] : added, late: false, revised_at: created },
    },
    summary: {
      state: rv.state, trigger: spec.trigger, opened_at: new Date(opened).toISOString(), recovery,
      acknowledgement: rv.ackSeq === undefined ? null : { at: created, through_seq: rv.ackSeq },
      ...(rv.closure ? { closure: rv.closure } : {}),
      stages: {
        last_proven: rv.state === "recovered_awaiting_review" ? { stage: "handler_started", event_key: facts[facts.length - 1].id, basis: "settled_record" } : null,
        active: rv.state === "active" && facts.some((f) => f.stage) ? { stage: facts.filter((f) => f.stage).pop()!.stage, event_key: facts[facts.length - 1].id, basis: "producer_watchdog_record" } : null,
        next: "unknown", root_cause: "not_derived", hypotheses: [],
      },
    },
    facts,
    context: [factRow([1, "boot", null])],
    next: { missing_evidence: rv.gaps.length > 0 ? "sequence_gaps_listed" : "none_listed", owner_diagnostics: "unsupported", automatic_repair: "never" },
  }
  const text = canonical(bundle)
  return { bundle, hash: `sha256:${await sha256(text)}`, bytes: new TextEncoder().encode(text).length, created }
}

async function incidentRow(spec: Spec, now: number): Promise<Row> {
  const n = spec.revisions.length
  const latest = await bundleOf(spec, n, now)
  const opened = now - spec.openedAgo
  const retain = spec.id === INC.expired ? now - 60_000 : opened + 7 * 86_400_000
  const expired = retain <= now
  return {
    id: spec.id, account: spec.source === "relay" ? "svc:relay" : spec.source === "cloud_api" ? "svc:api" : ACCOUNT, machine_id: spec.machine, source: spec.source, source_id: spec.sourceId,
    producer_instance: spec.instance, boot_id: spec.boot, group: spec.group, trigger: spec.trigger, state: expired ? null : spec.revisions[n - 1].state,
    expired, revision: n, content_hash: latest.hash, opened_at: new Date(opened).toISOString(), updated_at: latest.created,
    acknowledged: spec.revisions[n - 1].ackSeq !== undefined, facts: expired ? null : spec.revisions[n - 1].facts.length, facts_refused: 0,
    bytes: latest.bytes, retain_until: new Date(retain).toISOString(),
  }
}

function incidentEnvelope(now: number, extra: Row): Row {
  const iso = new Date(now).toISOString()
  return {
    schema_version: 1, generated_at: iso, as_of: iso, scope: { kind: "operator", account: null }, outcome: "unknown", outcome_reason: "no_presence_contract",
    rule_version: "incident-group/1", state_rule: "incident-state/1", clock_uncertainty_ms: null,
    clock: { basis: "server_received_at", order: "producer_seq", producer_wall: "display_only" }, owner_diagnostics: "unsupported",
    aggregates: { minute: "unsupported", retention_days: null }, evidence_quality: "complete", partial_reason: null, next_cursor: null, ...extra,
  }
}

/* ---- agent investigations (the Cloud's investigation contract v1) ----------- */

const HOOK_A = "swh_fixture00000000000000000000a1"
const HOOK_B = "swh_fixture00000000000000000000b2"
const FIX_COMMIT = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

/** Where an investigation can run: one row per machine, two ready, the others each saying what they need. */
function destinationsAnswer(): Response {
  const row = (machine_id: string, machine_name: string, status: string, hook_id: string | null, reason: string) => ({ machine_id, machine_name, status, hook_id, reason })
  return json(200, { schema: "clawdline.health_investigation.destinations.v1", status: "ok", complete: true, machines: [
    row(MAC_A, "Investigation machine A", "ready", HOOK_A, "single_investigation_hook"),
    row("fixture_mac_b", "Investigation machine B", "ready", HOOK_B, "single_investigation_hook"),
    row("fixture_mac_c", "A machine whose name is long enough to wrap on a phone screen", "ambiguous", null, "multiple_investigation_hooks"),
    row("fixture_linux_d", "Build server", "setup_required", null, "no_investigation_hook"),
    row("fixture_mac_e", "Laptop", "offline", null, "machine_offline"),
  ] })
}

type IvSpec = { state: string; reason: string | null; delivery: string | null; task: string | null; ago: number; closed?: boolean; report?: { conclusion: string; fix_commit: string | null; evidence_refs: string[] }; recovery?: boolean }
/** Investigations per incident, newest first; the active one has a live investigation. */
const IV_SPECS: Record<string, IvSpec[]> = {
  [INC.active]: [{ state: "investigating", reason: "running", delivery: "schedule_dispatch_accepted", task: null, ago: 600_000 }],
  [INC.recovering]: [{ state: "needs_you", reason: "recovery_unverified", delivery: "task_execution_terminal", task: "success", ago: 5_400_000, closed: true, report: { conclusion: "fix_prepared", fix_commit: FIX_COMMIT, evidence_refs: [] } }],
  [INC.review]: [
    { state: "verified_recovered", reason: null, delivery: "task_execution_terminal", task: "success", ago: 7_200_000, closed: true, report: { conclusion: "fix_prepared", fix_commit: FIX_COMMIT, evidence_refs: ["he_f3a9c1d27b6e4f0a8c5d2e1b9f7a6c3d4e5f6a7b"] }, recovery: true },
    { state: "needs_you", reason: "report_needs_evidence", delivery: "task_execution_terminal", task: "success", ago: 14_400_000, closed: true, report: { conclusion: "needs_evidence", fix_commit: null, evidence_refs: [] } },
    { state: "needs_you", reason: "task_not_successful", delivery: "task_execution_terminal", task: "failure", ago: 21_600_000, closed: true },
  ],
  [INC.closed]: [{ state: "needs_you", reason: "report_verification_failed", delivery: "task_execution_terminal", task: "success", ago: 9_000_000, closed: true, report: { conclusion: "verification_failed", fix_commit: FIX_COMMIT, evidence_refs: [] } }],
}
/** Investigations started in this tab, by incident, and the Idempotency-Keys that started them. */
const startedHere = new Map<string, { key: string; body: string; row: Row }>()

function ivRow(incident: Row, i: number, v: IvSpec, now: number): Row {
  const started = now - v.ago
  const n = String(i + 1).padStart(2, "0")
  return {
    investigation_id: `hiv_fixture${String(incident.id).slice(-3)}${n}`, incident_id: incident.id, incident_revision: incident.revision, incident_content_hash: String(incident.content_hash).replace(/^sha256:/, ""),
    hook_id: HOOK_A, machine_id: MAC_A, delivery_id: `swd_fixture${String(incident.id).slice(-6)}${n}`,
    started_at: new Date(started).toISOString(), deadline_at: new Date(started + 4 * 3_600_000 + 120_000).toISOString(), closed_at: v.closed ? new Date(started + 1_800_000).toISOString() : null,
    state: v.state, reason: v.reason, delivery_state: v.delivery, task_terminal_state: v.task, report: v.report ? { schema: "clawdline.health_investigation.report.v1", ...v.report } : null,
    recovery: v.recovery ? { event_key: "he_f3a9c1d27b6e4f0a8c5d2e1b9f7a6c3d4e5f6a7b", producer_seq: 42, received_at: new Date(started + 1_500_000).toISOString(), basis: "evidence_ref" } : null,
  }
}

function investigationsOf(scenario: FixtureScenario, incident: Row, now: number): Row | undefined {
  if (scenario === "investigations_old") return undefined
  if (scenario === "investigations_unavailable") return { status: "unavailable", items: null }
  const here = startedHere.get(String(incident.id))
  const items = (IV_SPECS[String(incident.id)] ?? []).map((v, i) => ivRow(incident, i, v, now))
  return { status: "ok", state_rule: "investigation-state/1", items: here ? [here.row, ...items] : items }
}

/** POST …/investigations: the same key and body replay; a live one or a moved revision refuses. */
async function startAnswer(scenario: FixtureScenario, incident: Row, key: string | null, body: string, now: number): Promise<Response> {
  if (scenario === "investigations_unavailable") return refusal(503, "health_store_unavailable")
  if (key === null) return refusal(400, "idempotency_key_required")
  const here = startedHere.get(String(incident.id))
  if (here && here.key === key) return here.body === body ? json(202, { schema: "clawdline.health_investigation.started.v1", investigation: here.row, replayed: true }) : refusal(409, "idempotency_conflict")
  const input = JSON.parse(body) as { hook_id?: string; expected_incident_revision?: number }
  if (input.expected_incident_revision !== incident.revision) return refusal(409, "incident_revision_moved")
  if (here || (IV_SPECS[String(incident.id)] ?? []).some((v) => !v.closed)) return refusal(409, "investigation_open")
  if (input.hook_id !== HOOK_A && input.hook_id !== HOOK_B) return refusal(409, "hook_unavailable")
  const row = { ...ivRow(incident, 9, { state: "investigating", reason: "queued_unclaimed", delivery: "queued", task: null, ago: 0 }, now), hook_id: input.hook_id }
  startedHere.set(String(incident.id), { key, body, row })
  return json(202, { schema: "clawdline.health_investigation.started.v1", investigation: row, replayed: false })
}

async function incidentAnswer(scenario: FixtureScenario, path: string, q: URLSearchParams, asked: number, method: string, ivRequest?: { key: string | null; body: string }): Promise<Response> {
  incidentEpoch ??= asked
  const now = incidentEpoch
  const disabled = scenario === "incidents_disabled" || scenario === "empty"
  const all = disabled ? [] : specs().map(withAck)
  if (path === "incidents") {
    const from = Date.parse(q.get("from") ?? "") || now - 7 * 86_400_000
    const to = Date.parse(q.get("to") ?? "") || now
    let rows: Row[] = await Promise.all(all.filter((x) => now - x.openedAgo >= from && now - x.openedAgo <= to).sort((a, b) => a.openedAgo - b.openedAgo).map((x) => incidentRow(x, now)))
    let nextCursor: string | null = null
    if (scenario === "many_incidents" && rows.length > 0) {
      // A closed, unexpired row is the template: an expired one carries no state or facts.
      const base = rows.find((r) => r.state === "closed" && r.expired === false) ?? rows[0]
      for (let i = 0; i < 124; i++) {
        const opened = now - 6 * 3_600_000 - i * 1_200_000
        if (opened < from || opened > to) continue
        rows.push({ ...base, id: `hi_f2${String(i).padStart(30, "0")}`, state: "closed", expired: false, acknowledged: true, opened_at: new Date(opened).toISOString(), updated_at: new Date(opened + 60_000).toISOString() })
      }
      rows.sort((a, b) => Date.parse(String(b.opened_at)) - Date.parse(String(a.opened_at)))
      // The Cloud's cursor binds the reader, filter and window; this one binds the window.
      const bind = `${from}|${to}`
      const limit = Math.min(200, Number(q.get("limit") ?? 50))
      let offset = 0
      const cursor = q.get("cursor")
      if (cursor !== null) {
        const [at, bound] = atob(cursor.replace(/-/g, "+").replace(/_/g, "/")).split("\n")
        if (bound !== bind || !/^[0-9]+$/.test(at)) return refusal(400, "bad_cursor")
        offset = Number(at)
      }
      const more = offset + limit < rows.length
      nextCursor = more ? btoa(`${offset + limit}\n${bind}`).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") : null
      rows = rows.slice(offset, offset + limit)
    }
    return json(200, incidentEnvelope(now, {
      next_cursor: nextCursor,
      window: { from: new Date(from).toISOString(), to: new Date(to).toISOString(), basis: "opened_at" },
      evidence_quality: disabled ? "not_collected" : "complete",
      source_coverage: { incidents: disabled ? "disabled" : "collected", bundles: disabled ? "disabled" : "collected", exports: "supported" },
      incidents: rows,
      refusals: { rows: disabled ? [] : [{ account: ACCOUNT, reason: "tenant_incident_cap", window_start: new Date(now - 3_600_000).toISOString(), count: 2 }], truncated: false },
    }))
  }
  const [, rawId, tail] = path.split("/")
  const spec = all.find((x) => x.id === rawId)
  if (!spec) return refusal(404, "incident_not_found")
  if (tail === "acknowledge") {
    if (method !== "POST") return refusal(404, "not_found")
    const before = spec.revisions[spec.revisions.length - 1].state === "closed"
    acknowledgedHere.add(spec.id)
    return json(200, incidentEnvelope(now, { revised: !before, incident: await incidentRow(withAck(spec), now) }))
  }
  const row = await incidentRow(spec, now)
  if (tail === "investigations") {
    if (method !== "POST") return refusal(404, "not_found")
    if (row.expired) return refusal(410, "incident_expired")
    return startAnswer(scenario, row, ivRequest?.key ?? null, ivRequest?.body ?? "", now)
  }
  if (row.expired) return json(410, { error: { code: "incident_expired", message: "incident_expired", details: { expired_at: row.retain_until } } })
  const raw = q.get("revision")
  const n = raw === null ? spec.revisions.length : Number(raw)
  if (!Number.isSafeInteger(n) || n < 1 || n > 16) return refusal(400, "invalid_field")
  if (n > spec.revisions.length) return refusal(404, "revision_not_found")
  const b = await bundleOf(spec, n, now)
  if (tail === "export") {
    if (scenario === "audit_fail") return refusal(503, "audit_unavailable")
    const bundle = scenario === "tampered" ? { ...b.bundle, next: { ...(b.bundle.next as Row), automatic_repair: "never", missing_evidence: "none_listed_after_edit" } } : b.bundle
    return new Response(JSON.stringify({
      schema_version: 1,
      export: { incident_id: spec.id, revision: n, latest_revision: spec.revisions.length, content_hash: b.hash, hash_basis: "sha256 over key-sorted JSON of `bundle`", exported_at: new Date(now).toISOString(), expires_at: row.retain_until, scope: { kind: "operator", account: row.account } },
      bundle,
    }), { status: 200, headers: { "content-type": "application/json; charset=utf-8", "content-disposition": `attachment; filename="${spec.id}.r${n}.json"` } })
  }
  return json(200, incidentEnvelope(now, {
    incident: { ...row, revisions: Array.from({ length: spec.revisions.length }, (_, i) => i + 1) },
    revision: { number: n, created_at: b.created, content_hash: b.hash, bytes: b.bytes },
    bundle: b.bundle,
    investigations: investigationsOf(scenario, row, now),
  }))
}

/** A fetch that answers from the fixture, honouring the caller's abort signal. */
export function fixtureFetch(name: string): typeof fetch {
  const scenario: FixtureScenario = (FIXTURE_SCENARIOS as readonly string[]).includes(name) ? (name as FixtureScenario) : "normal"
  return ((input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
    const signal = init?.signal
    const delay = scenario === "slow" ? 8_000 : 120
    const timer = setTimeout(() => resolve(fixtureAnswer(scenario, String(input), Date.now(), init?.method ?? "GET", { key: new Headers(init?.headers).get("idempotency-key"), body: typeof init?.body === "string" ? init.body : "" })), delay)
    signal?.addEventListener("abort", () => { clearTimeout(timer); reject(new DOMException("aborted", "AbortError")) }, { once: true })
  })) as typeof fetch
}
