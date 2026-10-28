/** Development harness only. This module is reached exclusively through fixture.ts. */
import type { PerformanceBody, Metric } from "./performance"
export function performanceFixture(now: number, hours = 1, scenario = "normal", until = now): PerformanceBody {
  const empty = scenario === "empty", stale = scenario === "stale"
  const end = Math.floor(until/60000)*60000, start = end-hours*3600000, interval = hours === 168 ? 3600000 : Math.max(60000,hours*3600000/288)
  const metric = (i: number): Metric => ({ count: empty ? 0 : Math.round(180+60*Math.sin(i)), errors: 0, cancelled: empty ? 0 : 2, slow: empty ? 0 : 1, max_ms: empty ? 0 : 2500, p50_ms: empty ? null : 100, p95_ms: empty ? null : 500+Math.sin(i)*100, p99_ms: empty ? null : 1100+Math.cos(i)*200, error_pct: empty ? null : 0 })
  const history = Array.from({length: Math.ceil((end-start)/interval)}, (_,i) => ({ ...metric(i/9), at: start+i*interval, observed: true, coverage_seconds: interval/1000 }))
  const host = (at: number,i: number) => ({ at, cpu_pct: 24+Math.sin(i/12)*8, memory_pct: 47+Math.cos(i/14)*2, available_bytes: 2200000000, total_bytes: 4200000000, cores: 2, load: .6, error: null, groups: [
    { service: "api-blue", cpu_pct: 6.4, memory_bytes: 90000000, processes: 12, coverage: "cgroup_v2", runtime_image: "sha256:"+"a".repeat(64) },
    { service: "mongo", cpu_pct: 8.2, memory_bytes: 700000000, processes: 32, coverage: "cgroup_v2", runtime_image: "sha256:"+"b".repeat(64) },
    { service: "caddy", cpu_pct: 2.3, memory_bytes: 45000000, processes: 8, coverage: "cgroup_v2", runtime_image: "sha256:"+"c".repeat(64) },
  ] })
  const hosts = history.map((p,i) => host(p.at,i))
  return { schema: "clawdline.service-performance/1", generated_at: now, from_ms: start, to_ms: end, status: stale || empty ? "unknown" : "reporting", host_fresh: !stale && !empty, api_fresh: !stale,
    source_at: stale ? end-600000 : end, received_at: end, latest: empty ? null : host(stale ? end-600000 : end,15), api: { boot:"fixture-boot", version:"a".repeat(40), active: 2, dropped:0 },
    current: { ...metric(0), count: empty ? 0 : 1024, coverage_seconds: empty ? 0 : 300, expected_seconds:300, dropped:0, from_ms:end-300000,to_ms:end },
    previous: { ...metric(2), count:empty ? 0 : 978, coverage_seconds:empty ? 0 : 300, expected_seconds:300,dropped:0, from_ms:end-600000,to_ms:end-300000 },
    history: empty ? [] : history, hosts:empty ? [] : hosts, routes:empty ? [] : ["GET /v1/machines", "POST /v1/health/viewer-ingest", "GET /su/v1/health/overview"].map((route,i) => ({ route,...metric(i) })),
    slow_routes_from_ms:start,slow_routes_to_ms:end,
    deployments:empty ? [] : [{ id:"fixture-deploy",start:end-1800000,end:end-1740000,version:"a".repeat(40),status:"complete" }], alerts:[], alerts_dropped:0, retention_floor:start,interval_ms:interval,unsupported:["relay_host_cpu","relay_host_memory"],method:"histogram_upper_bound" }
}
