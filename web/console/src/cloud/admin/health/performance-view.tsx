import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { ConnectionHealthProps } from "./ConnectionHealth"
import { healthHref } from "./route"
import { healthWords, type HealthSay } from "./words"
import { MonitoringNotifications } from "./performance-notifications"
import { PERFORMANCE_LIMITS, readPerformance, type PerformanceBody, type PerformancePoint } from "./performance"
import "./performance.css"

const number = (v: number | null | undefined, unit = "") => v == null ? "—" : `${v.toLocaleString(undefined, { maximumFractionDigits: 1 })}${unit}`
const at = (v: number | null | undefined) => v == null ? "—" : new Date(v).toLocaleString(undefined, { hour12: false })
const ms = (v: number | null | undefined) => v == null ? "—" : v < 1000 ? number(v, " ms") : number(v / 1000, " s")
type Held = { reader: typeof fetch; key: string; body: PerformanceBody | null; failed: boolean; loading: boolean }

export function PerformanceScreen({ route, go, apiOrigin, get, fixture, onRefused, locale }: ConnectionHealthProps) {
  const say = useMemo(() => healthWords(locale), [locale])
  const hours = route.screen === "M03" ? route.hours ?? 1 : 1
  const until = route.scope.until
  // No account selector is sent. A different route/get/origin owns a fresh state and abort fence.
  const key = `${apiOrigin}|${hours}|${until ?? "live"}`
  const [held, setHeld] = useState<Held>({ reader: get, key, body: null, failed: false, loading: true })
  const [revision, setRevision] = useState(0)
  const ctl = useRef<AbortController | null>(null)
  const refresh = useCallback(() => setRevision(v => v + 1), [])
  useEffect(() => {
    let alive = true
    const run = () => {
      if (typeof document !== "undefined" && document.hidden || ctl.current) return
      const current = new AbortController()
      ctl.current = current
      setHeld(v => ({ reader: get, key, body: v.reader === get && v.key === key ? v.body : null, failed: v.reader === get && v.key === key && v.failed, loading: true }))
      void readPerformance(get, apiOrigin, hours, until, current.signal).then(body => {
        if (alive && !current.signal.aborted) setHeld({ reader: get, key, body, failed: false, loading: false })
      }).catch(error => {
        if (!alive || current.signal.aborted) return
        if (error instanceof Error && error.message === "monitor_refused") {
          setHeld({ reader: get, key, body: null, failed: true, loading: false })
          onRefused("forbidden")
        } else setHeld(v => ({ reader: get, key, body: v.reader === get && v.key === key ? v.body : null, failed: true, loading: false }))
      }).finally(() => { if (ctl.current === current) ctl.current = null })
    }
    const visibility = () => {
      if (document.hidden) { ctl.current?.abort(); ctl.current = null; setHeld({ reader: get, key, body: null, failed: false, loading: true }) }
      else run()
    }
    run()
    const timer = until === null ? setInterval(run, PERFORMANCE_LIMITS.refreshMs) : null
    const revalidate = () => { ctl.current?.abort(); ctl.current = null; setHeld({ reader: get, key, body: null, failed: false, loading: true }); run() }
    document.addEventListener("visibilitychange", visibility)
    window.addEventListener("focus", revalidate)
    window.addEventListener("storage", revalidate)
    return () => { alive = false; ctl.current?.abort(); ctl.current = null; if (timer) clearInterval(timer); document.removeEventListener("visibilitychange", visibility); window.removeEventListener("focus", revalidate); window.removeEventListener("storage", revalidate) }
  }, [get, apiOrigin, hours, until, key, revision, onRefused])
  const state = held.reader === get && held.key === key ? held : { reader: get, key, body: null, failed: false, loading: true }
  const data = state.body
  const move = (h: 1 | 6 | 24 | 168, end: string | null) => go(healthHref({ screen: "M03", performance: true, hours: h }, { ...route.scope, account: null, until: end }))
  return <div className="adh pf">
    {fixture ? <p className="adh-example" role="note">{say("st_fixtureBanner")}</p> : null}
    <header className="adh-head"><h1 className="adh-title">{say("pf_title")}</h1><p className="adm-sub">{say("pf_lead")}</p></header>
    <nav className="adh-switch" aria-label={say("tr_viewLabel")}>
      <a className="adh-switch-btn" href={healthHref({ screen: "M03" }, route.scope)} onClick={e => { e.preventDefault(); go(healthHref({ screen: "M03" }, route.scope)) }}>{say("pf_stage")}</a>
      <a className="adh-switch-btn" aria-current="page" href={healthHref(route, route.scope)}>{say("pf_view")}</a>
    </nav>
    <div className="pf-toolbar">
      <fieldset className="adh-control"><legend>{say("pf_range")}</legend><div className="pf-ranges">{([1, 6, 24, 168] as const).map(h => <button type="button" key={h} className="adm-btn adm-btn-small" aria-pressed={hours === h} onClick={() => move(h, until)}>{say(`pf_${h}`)}</button>)}</div></fieldset>
      <div className="pf-timebuttons"><button type="button" className="adm-btn adm-btn-small" onClick={() => move(hours, new Date((until ? Date.parse(until) : Date.now()) - hours * 3600000).toISOString())}>{say("pf_before")}</button>
        <button type="button" className="adm-btn adm-btn-small" disabled={until === null} onClick={() => { const next = Date.parse(until!) + hours * 3600000; move(hours, next >= Date.now() ? null : new Date(next).toISOString()) }}>{say("pf_after")}</button>
        {until ? <button type="button" className="adm-btn adm-btn-small" onClick={() => move(hours, null)}>{say("pf_now")}</button> : <button type="button" className="adm-btn adm-btn-small" onClick={() => move(hours, new Date().toISOString())}>{say("pause")}</button>}
        <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" disabled={state.loading} onClick={refresh}>{say(state.loading ? "refreshing" : "refresh")}</button></div>
    </div>
    {state.failed ? <p className="adm-stale" role="alert">{say(data ? "pf_refreshFailed" : "pf_failed")}</p> : null}
    {data ? <>
      <p className={`pf-quality ${state.failed || data.status === "unknown" ? "pf-warning" : ""}`} role="status"><span aria-hidden="true">{state.failed || data.status === "unknown" ? "◇" : "◉"}</span> {say(until !== null ? "pf_asof" : state.failed || data.status === "unknown" ? "pf_unknown" : "pf_reporting")}<span>{at(data.latest?.at)}</span></p>
      <div className="pf-cards">
        <Card label={say("pf_cpu")} glyph="◴" value={number(data.latest?.cpu_pct, "%")} note={`${number(data.latest?.cores)} · ${say("pf_cores")}`} stale={state.failed || !data.host_fresh} say={say} />
        <Card label={say("pf_memory")} glyph="▥" value={number(data.latest?.memory_pct, "%")} note={`${say("pf_available")} ${number(data.latest?.available_bytes == null ? null : data.latest.available_bytes / 1073741824, " GiB")}`} stale={state.failed || !data.host_fresh} say={say} />
        <Card label={say("pf_latency")} glyph="◷" value={ms(data.current.p95_ms)} note={`P95 · ${number(data.current.count)} ${say("pf_count")}`} stale={state.failed || !data.api_fresh || data.current.coverage_seconds < 299} say={say} />
        <Card label={say("pf_errors")} glyph="△" value={number(data.current.error_pct, "%")} note={`${number(data.current.errors)} / ${number(data.current.count)} · 5xx`} stale={state.failed || !data.api_fresh || data.current.coverage_seconds < 299} say={say} />
      </div>
      <p className="adm-fine">{say("pf_elapsedNote")}</p>
      <section className="adm-panel"><h2>{say("pf_compare")}</h2><p className="adm-fine">{at(data.current.from_ms)} – {at(data.current.to_ms)}</p>
        <div className="pf-comparison">{([
          [say("pf_latency") + " · P95", ms(data.current.p95_ms), ms(data.previous.p95_ms)],
          [say("pf_count"), number(data.current.count), number(data.previous.count)],
          [say("pf_errors"), number(data.current.errors), number(data.previous.errors)],
          [say("pf_cancelled"), number(data.current.cancelled), number(data.previous.cancelled)],
          [say("pf_coverage"), `${number(data.current.coverage_seconds)} / 300 s`, `${number(data.previous.coverage_seconds)} / 300 s`],
        ]).map(([label, a, b]) => <div key={label}><strong>{label}</strong><span>{say("pf_current")} <b>{a}</b></span><span>{say("pf_previous")} <b>{b}</b></span></div>)}</div>
        {data.current.count < 100 || data.previous.count < 100 || data.current.coverage_seconds < 299 || data.previous.coverage_seconds < 299 || data.current.dropped || data.previous.dropped ? <p className="adm-fine pf-warning">{say("pf_low")}</p> : null}
      </section>
      <History data={data} say={say} />
      <section className="adm-panel"><h2>{say("pf_routes")}</h2><p className="adm-fine">{at(data.slow_routes_from_ms)} – {at(data.slow_routes_to_ms)}</p><div className="pf-route-list">{data.routes.length ? data.routes.map(r => <article key={r.route} className="pf-route"><code>{r.route}</code><dl><dt>{say("pf_count")}</dt><dd>{number(r.count)}</dd><dt>P95 / P99</dt><dd>{ms(r.p95_ms)} / {ms(r.p99_ms)}</dd><dt>≥ 2 s / 5xx / 499</dt><dd>{number(r.slow)} / {number(r.errors)} / {number(r.cancelled)}</dd></dl></article>) : <p>{say(data.api_fresh ? "pf_noTraffic" : "pf_missing")}</p>}</div></section>
      <section className="adm-panel"><h2>{say("pf_groups")}</h2><div className="pf-route-list">{data.latest?.groups.length ? data.latest.groups.map(g => <article className="pf-route" key={g.service}><h3>{g.service}</h3><dl><dt>CPU</dt><dd>{number(g.cpu_pct, "%")}</dd><dt>{say("pf_memory")}</dt><dd>{number(g.memory_bytes / 1048576, " MiB")}</dd><dt>{say("pf_processes")}</dt><dd>{number(g.processes)}</dd></dl><details><summary>{say("pf_runtimeImage")}</summary><code>{g.runtime_image ?? say("pf_noImage")}</code></details></article>) : <p>{say("pf_missing")}</p>}</div><p className="adm-fine">{say("pf_groupNote")}</p>{data.latest?.group_error ? <p className="pf-warning">{say("pf_missing")} · {data.latest.group_error}</p> : null}</section>
      <section className="adm-panel"><h2>{say("pf_markers")}</h2><p className="adm-fine">{say("pf_markerNote")}</p>{data.deployments.length ? data.deployments.map(d => <article key={d.id} className="pf-deployment"><strong>{say(d.status === "complete" ? "pf_complete" : d.status === "failed" ? "pf_failedDeploy" : d.status === "running" ? "pf_running" : "pf_unknownDeploy")}</strong><code>{d.version}</code><span>{at(d.start)} → {at(d.end)}</span></article>) : <p>{say("pf_none")}</p>}</section>
      <section className="adm-panel"><h2>{say("pf_alerts")}</h2><p className="adm-fine">{say("pf_alertNote")}</p>{data.alerts_dropped > 0 ? <p className="pf-warning">{say("pf_alertLoss")} {number(data.alerts_dropped)}</p> : null}{data.alerts.length ? data.alerts.map(a => <p key={a.id}>{say(("pf_rule_" + a.rule) as import("./words").HealthWord)} · {say(a.transition === "recovered" ? "pf_recovered" : "pf_opened")} · {at(a.at)} · {say(("pf_receipt_" + (a.receipt ?? "pending")) as import("./words").HealthWord)}</p>) : <p>{say("pf_none")}</p>}</section>
      <MonitoringNotifications get={get} origin={apiOrigin} say={say} />
      <details className="adm-panel"><summary>{say("pf_quality")}</summary><dl className="pf-facts"><dt>{say("pf_apiSource")}</dt><dd>{at(data.source_at)} / {at(data.received_at)}</dd><dt>{say("pf_version")}</dt><dd><code>{data.api?.version ?? "—"}</code></dd><dt>{say("pf_active")}</dt><dd>{number(data.api?.active)}</dd><dt>{say("pf_dropped")}</dt><dd>{number(data.current.dropped)}</dd><dt>{say("pf_floor")}</dt><dd>{at(data.retention_floor)}</dd></dl><p className="adm-fine">{say("pf_histogram")}</p><p className="adm-fine">{say("pf_unsupported")}</p></details>
    </> : state.loading ? <div className="adm-skeleton" aria-busy="true"><p role="status">{say("pf_loading")}</p><div className="pf-cards">{[0, 1, 2, 3].map(v => <div key={v} className="adm-tile adm-shimmer" />)}</div><div className="adm-panel adm-shimmer adm-shimmer-tall" /></div> : null}
  </div>
}

function Card({ label, glyph, value, note, stale, say }: { label: string; glyph: string; value: string; note: string; stale: boolean; say: HealthSay }) {
  return <section className={`adm-tile pf-card ${stale ? "pf-card-stale" : ""}`}><div><span aria-hidden="true" className="pf-icon">{glyph}</span><h2>{label}</h2></div><strong>{value}</strong><p>{note}</p>{stale ? <small>{say("pf_stale")}</small> : null}</section>
}

function History({ data, say }: { data: PerformanceBody; say: HealthSay }) {
  const [selected, setSelected] = useState<number | null>(null)
  const points = data.history
  const index = Math.max(0, Math.min(selected ?? points.length - 1, points.length - 1))
  const point = points[index]
  const [view, setView] = useState<[number, number] | null>(null)
  const start = view?.[0] ?? data.from_ms, end = view?.[1] ?? data.to_ms
  const x = (t: number) => 42 + (t - start) / Math.max(1, end-start) * 900
  const series = (get: (p: PerformancePoint) => number | null, color: string, max: number, independent: boolean) => {
    let path = "", open = false
    for (const p of points) {
      if (p.at < start || p.at > end) continue
      const v = p.observed || independent ? get(p) : null
      if (v === null) { open = false; continue }
      path += `${open ? "L" : "M"}${x(p.at).toFixed(2)},${(148-v / Math.max(1, max)*120).toFixed(2)} `; open = true
    }
    return <path d={path} stroke={color} strokeWidth="2" fill="none" vectorEffect="non-scaling-stroke" />
  }
  const chart = (title: string, values: [((p: PerformancePoint) => number | null), string][], max: number, unit: string, independent = false) => <div className="pf-chart"><h3>{title}</h3><svg viewBox="0 0 960 185" role="img" aria-label={title}><title>{title}</title>
    {[0, .5, 1].map(v => <g key={v}><line x1="42" x2="942" y1={148-v*120} y2={148-v*120} stroke="var(--adm-line, #45454c)" strokeDasharray="4 5" /><text x="0" y={152-v*120}>{number(max*v)}{unit}</text></g>)}
    {data.deployments.filter(d => d.start <= end && (d.end === null || d.end >= start)).map(d => <g key={d.id}>{d.end !== null ? <rect x={x(Math.max(start,d.start))} y="18" width={Math.max(1, x(Math.min(end,d.end))-x(Math.max(start,d.start)))} height="130" fill="#7691a8" opacity=".15" /> : null}{d.start >= start ? <line x1={x(d.start)} x2={x(d.start)} y1="18" y2="148" stroke="#7691a8" strokeDasharray="4 3" /> : null}</g>)}
    {values.map(([get,color]) => <g key={color}>{series(get,color,max,independent)}</g>)}
    {point && point.at >= start && point.at <= end ? <line x1={x(point.at)} x2={x(point.at)} y1="18" y2="148" stroke="currentColor" opacity=".35" /> : null}
    <text x="42" y="178">{new Date(start).toLocaleTimeString([], { hour:"2-digit", minute:"2-digit" })}</text><text x="942" y="178" textAnchor="end">{new Date(end).toLocaleTimeString([], { hour:"2-digit", minute:"2-digit" })}</text>
  </svg></div>
  const resources = useMemo(() => {
    const map = new Map<number, { cpu: number | null; memory: number | null }>()
    for (const h of data.hosts) map.set(Math.floor(h.at / data.interval_ms), { cpu: h.cpu_pct, memory: h.memory_pct })
    return map
  }, [data.hosts, data.interval_ms])
  const resource = (p: PerformancePoint, key: "cpu" | "memory") => resources.get(Math.floor(p.at/data.interval_ms))?.[key] ?? null
  return <section className="adm-panel pf-history"><h2>{say("pf_history")}</h2><p className="adm-fine">{at(start)} – {at(end)} · <span className="pf-deploy-legend">▧ {say("pf_deployment")}</span></p>
    {chart(say("pf_latency"), [[p => p.p95_ms, "#dc9461"], [p => p.p99_ms, "#cf6e64"]], Math.max(1000, ...points.map(p => p.p99_ms ?? 0)), " ms")}
    <p className="pf-legend"><span>P95</span><span>P99</span></p>
    {chart(say("pf_requests"), [[p => p.count, "#a8a8b2"]], Math.max(1, ...points.map(p => p.count)), "")}
    {chart(say("pf_resources"), [[p => resource(p, "cpu"), "#dc9461"], [p => resource(p,"memory"), "#a8a8b2" ]], 100, "%", true)}
    <div className="pf-readout"><label>{say("pf_select")}<input aria-label={say("pf_select")} type="range" min="0" max={Math.max(0,points.length-1)} value={index} onChange={e => setSelected(Number(e.target.value))} /></label><p><time>{at(point?.at)}</time> · P95 {ms(point?.p95_ms)} · P99 {ms(point?.p99_ms)} · {say("pf_count")} {point?.observed ? number(point.count) : "—"}</p></div>
    {data.deployments.length ? <div className="pf-focus">{data.deployments.slice(0, 10).map(d => <button key={d.id} type="button" className="adm-btn adm-btn-small" onClick={() => setView([Math.max(data.from_ms,d.start-300000),Math.min(data.to_ms,(d.end??d.start)+300000)])}>{say("pf_focus")} · {d.version.slice(0,7)}</button>)}{view ? <button className="adm-btn adm-btn-small" type="button" onClick={() => setView(null)}>{say("pf_now")}</button> : null}</div> : null}
    <details><summary>{say("pf_table")}</summary><ol className="pf-values">{points.map(p => <li key={p.at}><time>{at(p.at)}</time><span>P95 {ms(p.p95_ms)} · P99 {ms(p.p99_ms)} · {p.observed ? number(p.count) : say("pf_missing")}</span></li>)}</ol></details>
  </section>
}
