/** The service-only performance read. No shared cache, SSR singleton, owner or account selector. */
export const PERFORMANCE_LIMITS = { bodyBytes: 1048576, points: 1440, routes: 32, deployments: 100, alerts: 30, refreshMs: 30000, timeoutMs: 6000 } as const
export type Metric = { count: number; errors: number; cancelled: number; slow: number; max_ms: number; p50_ms: number | null; p95_ms: number | null; p99_ms: number | null; error_pct: number | null }
export type PerformanceWindow = Metric & { from_ms: number; to_ms: number; coverage_seconds: number; expected_seconds: number; dropped: number }
export type PerformancePoint = Metric & { at: number; observed: boolean; coverage_seconds: number }
export type ServiceGroup = { service: string; cpu_pct: number | null; memory_bytes: number; processes: number; coverage: string; runtime_image: string | null }
export type HostPoint = { at: number; cpu_pct: number | null; memory_pct: number | null; available_bytes: number | null; total_bytes: number | null; cores: number; load: number; groups: ServiceGroup[]; error: string | null; group_error?: string }
export type Deployment = { id: string; start: number; end: number | null; version: string; status: "running" | "complete" | "failed" | "unknown" }
export type PerformanceBody = {
  schema: "clawdline.service-performance/1"; generated_at: number; from_ms: number; to_ms: number;
  status: "reporting" | "unknown"; host_fresh: boolean; api_fresh: boolean;
  source_at: number | null; received_at: number | null; latest: HostPoint | null;
  api: { boot: string; version: string | null; active: number; dropped: number } | null;
  slow_routes_from_ms: number; slow_routes_to_ms: number;
  current: PerformanceWindow; previous: PerformanceWindow; history: PerformancePoint[]; hosts: HostPoint[];
  routes: (Metric & { route: string })[]; deployments: Deployment[];
  alerts: { id: string; rule: string; transition: string; at: number; receipt: string | null; attempts: number }[];
  alerts_dropped: number; retention_floor: number | null; interval_ms: number; unsupported: string[]; method: "histogram_upper_bound";
}

const bad = (): never => { throw new Error("monitor_contract") }
const object = (v: unknown): Record<string, unknown> => v && typeof v === "object" && !Array.isArray(v) ? v as Record<string, unknown> : bad()
const n = (v: unknown): number => typeof v === "number" && Number.isFinite(v) && v >= 0 ? v : bad()
const maybe = (v: unknown) => v === null ? null : n(v)
const text = (v: unknown): string => typeof v === "string" && v.length <= 256 ? v : bad()
const boolean = (v: unknown) => typeof v === "boolean" ? v : bad()
const list = <T>(v: unknown, cap: number, parse: (v: unknown) => T): T[] => Array.isArray(v) && v.length <= cap ? v.map(parse) : bad()
const nullableText = (v: unknown) => v === null ? null : text(v)
function metric(v: unknown): Metric {
  const o = object(v)
  const out = { count: n(o.count), errors: n(o.errors), cancelled: n(o.cancelled), slow: n(o.slow), max_ms: n(o.max_ms), p50_ms: maybe(o.p50_ms), p95_ms: maybe(o.p95_ms), p99_ms: maybe(o.p99_ms), error_pct: maybe(o.error_pct) }
  if (out.errors > out.count || out.cancelled > out.count || out.slow > out.count || out.errors + out.cancelled > out.count || (out.count === 0 && (out.p95_ms !== null || out.error_pct !== null))) bad()
  return out
}
function windowMetric(v: unknown): PerformanceWindow {
  const o = object(v)
  return { ...metric(o), from_ms: n(o.from_ms), to_ms: n(o.to_ms), coverage_seconds: n(o.coverage_seconds), expected_seconds: n(o.expected_seconds), dropped: n(o.dropped) }
}
function host(v: unknown): HostPoint {
  const o = object(v)
  return { at: n(o.at), cpu_pct: maybe(o.cpu_pct), memory_pct: maybe(o.memory_pct), available_bytes: maybe(o.available_bytes), total_bytes: maybe(o.total_bytes), cores: n(o.cores), load: n(o.load), error: nullableText(o.error), ...(o.group_error === undefined ? {} : { group_error: text(o.group_error) }),
    groups: list(o.groups, 5, v => { const g = object(v); return { service: text(g.service), cpu_pct: maybe(g.cpu_pct), memory_bytes: n(g.memory_bytes), processes: n(g.processes), coverage: text(g.coverage), runtime_image: nullableText(g.runtime_image) } }) }
}
export function parsePerformance(v: unknown): PerformanceBody {
  const o = object(v)
  if (o.schema !== "clawdline.service-performance/1" || o.method !== "histogram_upper_bound" || !["reporting", "unknown"].includes(String(o.status))) bad()
  const current = windowMetric(o.current), previous = windowMetric(o.previous)
  const latest = o.latest === null ? null : host(o.latest)
  const hostFresh = boolean(o.host_fresh), apiFresh = boolean(o.api_fresh)
  if (o.status === "reporting" && (!hostFresh || !apiFresh || latest === null)) bad()
  let api: PerformanceBody["api"] = null
  if (o.api !== null) { const a = object(o.api); api = { boot: text(a.boot), version: nullableText(a.version), active: n(a.active), dropped: n(a.dropped) } }
  return { schema: "clawdline.service-performance/1", method: "histogram_upper_bound", generated_at: n(o.generated_at), from_ms: n(o.from_ms), to_ms: n(o.to_ms), status: o.status as PerformanceBody["status"], host_fresh: hostFresh, api_fresh: apiFresh, source_at: maybe(o.source_at), received_at: maybe(o.received_at), latest, api, current, previous, slow_routes_from_ms: n(o.slow_routes_from_ms), slow_routes_to_ms: n(o.slow_routes_to_ms),
    history: list(o.history, PERFORMANCE_LIMITS.points, v => { const p = object(v); return { ...metric(p), at: n(p.at), observed: boolean(p.observed), coverage_seconds: n(p.coverage_seconds) } }),
    hosts: list(o.hosts, PERFORMANCE_LIMITS.points, host),
    routes: list(o.routes, PERFORMANCE_LIMITS.routes, v => { const r = object(v); return { ...metric(r), route: text(r.route) } }),
    deployments: list(o.deployments, PERFORMANCE_LIMITS.deployments, v => { const d = object(v); if (!["running", "complete", "failed", "unknown"].includes(String(d.status))) bad(); return { id: text(d.id), start: n(d.start), end: maybe(d.end), version: text(d.version), status: d.status as Deployment["status"] } }),
    alerts: list(o.alerts, PERFORMANCE_LIMITS.alerts, v => { const a = object(v); return { id: text(a.id), rule: text(a.rule), transition: text(a.transition), at: n(a.at), receipt: nullableText(a.receipt), attempts: n(a.attempts) } }),
    alerts_dropped: n(o.alerts_dropped), retention_floor: maybe(o.retention_floor), interval_ms: n(o.interval_ms), unsupported: list(o.unsupported, 16, text) }
}

export async function readPerformance(get: typeof fetch, origin: string, hours: number, until: string | null, signal: AbortSignal): Promise<PerformanceBody> {
  const ctl = new AbortController()
  const abort = () => ctl.abort()
  signal.addEventListener("abort", abort, { once: true })
  if (signal.aborted) ctl.abort()
  const timer = setTimeout(abort, PERFORMANCE_LIMITS.timeoutMs)
  const q = new URLSearchParams({ hours: String(hours) })
  if (until) q.set("until", String(Date.parse(until)))
  try {
    const r = await get(`${origin}/su/v1/health/performance?${q}`, { credentials: "include", cache: "no-store", signal: ctl.signal })
    if (!r.ok) throw new Error(r.status === 401 || r.status === 403 || r.status === 404 ? "monitor_refused" : "monitor_unavailable")
    const chunks: Uint8Array[] = []
    let bytes = 0
    const reader = r.body?.getReader()
    if (!reader) throw new Error("monitor_contract")
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      bytes += value.byteLength
      if (bytes > PERFORMANCE_LIMITS.bodyBytes) { await reader.cancel(); throw new Error("monitor_bound") }
      chunks.push(value)
    }
    const data = new Uint8Array(bytes)
    let offset = 0
    for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.length }
    return parsePerformance(JSON.parse(new TextDecoder().decode(data)))
  } finally { clearTimeout(timer); signal.removeEventListener("abort", abort) }
}
