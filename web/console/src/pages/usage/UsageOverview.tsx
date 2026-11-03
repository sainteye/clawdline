import { ProjectPicker } from "../../ui/ProjectPicker.js"
import type { SessionRow } from "@clawdline/contract"
import { Mark } from "../../session/List.js"
import { readProjectPlaces, type ProjectPlace } from "../work/api.js"
import { UsageSkeleton } from "./UsageSkeleton.js"
import { usageProjectOptions } from "./project-options.js"
import { useEffect, useId, useMemo, useRef, useState } from "react"
import { currentCatalogTag } from "../../catalog.js"
import { onMachineChanged } from "../../cloud/machine-switch.js"
import { scheduleFleet } from "../../cloud/schedule-machines.js"
import { readUsageOverview, UsageReadError, type UsageOverview } from "./api.js"
import { compactValue, dateRange, comparisonComplete, dailyValue, lineSegments, percentageChange, sampleLabel, selectedDay, shortDate, UsageRequestScope, type UsageMetric } from "./model.js"
import { usageWord as w } from "./words.js"
import "./usage.css"

type ReadState = { state: "loading" } | { state: "error"; code: string } | { state: "ready"; data: UsageOverview }

function errorWord(code: string) {
  if (["machine_offline", "offline", "socket_error"].includes(code)) return w("usageOffline")
  if (["not_implemented", "cloud_feature_unavailable", "cloud_machine_unsupported", "cloud_not_carried"].includes(code)) return w("usageUnsupported")
  return w("usageFailed")
}

/** Required states: loading; failed/offline/old machine with retry; missing, partial, empty and complete data. */
export default function UsageOverviewPage({ shown }: { shown: boolean }) {
  const [days, setDays] = useState<7 | 28>(7)
  const [project, setProject] = useState("")
  const [options, setOptions] = useState<UsageOverview["project_options"]>([])
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [metric, setMetric] = useState<UsageMetric>("tokens")
  const [read, setRead] = useState<ReadState>({ state: "loading" })
  const [revision, setRevision] = useState(0)
  const scope = useRef(new UsageRequestScope())
  const initialRead = useRef(true)
  const title = useRef<HTMLHeadingElement>(null)
  const [visible, setVisible] = useState(() => !document.hidden)

  useEffect(() => {
    const changed = () => { initialRead.current = true; scope.current.cancel(); setRead({ state: "loading" }); setProject(""); setOptions([]); setPlaces([]); setRevision(n => n + 1) }
    return onMachineChanged(changed)
  }, [])
  useEffect(() => {
    if (!shown) return
    let current = true
    // refusal-ok: Project marks are optional decoration; usage options and filtering remain available with placeholder marks.
    void readProjectPlaces().then(page => { if (current) setPlaces(page.places) }).catch(() => { if (current) setPlaces([]) })
    return () => { current = false }
  }, [shown, revision])
  useEffect(() => {
    const changed = () => setVisible(!document.hidden)
    document.addEventListener("visibilitychange", changed)
    return () => document.removeEventListener("visibilitychange", changed)
  }, [])
  useEffect(() => { if (shown) title.current?.focus({ preventScroll: true }) }, [shown])
  useEffect(() => {
    if (!shown || !visible) { scope.current.cancel(); return }
    const request = scope.current
    const signal = request.begin()
    setRead({ state: "loading" })
    void readUsageOverview(days, project, signal).then(data => {
      if (request.current(signal)) { initialRead.current = false; setOptions(data.project_options); setRead({ state: "ready", data }) }
    }).catch(error => {
      if (request.current(signal)) { initialRead.current = false; setRead({ state: "error", code: error instanceof UsageReadError ? error.code : "read_failed" }) }
    })
    return () => request.cancel()
  }, [days, project, revision, shown, visible])

  const pickerOptions = useMemo(() => usageProjectOptions(options, places), [options, places])
  const data = read.state === "ready" ? read.data : null
  const fleet = scheduleFleet()
  const machine = fleet?.machines.find(m => m.id === fleet.current)?.name ?? w("usageThisMachine")
  return <section id="usage" className="page board-page usage-page" data-page-view="usage" hidden={!shown} aria-labelledby="usage-title">
    <div className="usage-wrap">
      <header className="usage-header"><div className="usage-heading"><div className="usage-title-block"><h1 id="usage-title" tabIndex={-1} ref={title}>{w("usageOverview")}</h1>
        <div className="usage-metadata">{data && <span>{dateRange(data.daily[0]?.date, data.daily.at(-1)?.date, currentCatalogTag())} · {data.timezone}</span>}<span className="usage-machine">{machine}</span></div>
      </div>
        <button className="usage-refresh" type="button" aria-label={w("usageRefresh")} title={w("usageRefresh")} onClick={() => { initialRead.current = false; setRevision(n => n + 1) }}>↻</button></div>
      <div className="usage-filters">
        <div className="usage-project-filter"><span className="usage-filter-label">{w("usageProject")}</span>
          <ProjectPicker places={pickerOptions} value={project}
            label={w("usageProject")} allLabel={w("usageAllProjects")} allowAll
            onChange={value => { if (value === project) return; initialRead.current = false; scope.current.cancel(); setRead({ state: "loading" }); setProject(value) }} />
        </div>
        <div className="usage-switch" role="group" aria-label={w("usagePeriod")}>{([7, 28] as const).map(period => <button key={period} type="button" aria-pressed={days === period} onClick={() => { if (days !== period) { initialRead.current = false; scope.current.cancel(); setRead({ state: "loading" }); setDays(period) } }}>{w("usageDays", { days: period })}</button>)}</div>
      </div></header>
      <div className="usage-body" aria-busy={read.state === "loading"}>
        {read.state === "loading" && (initialRead.current ? <UsageSkeleton /> : <div className="usage-state" role="status">{w("usageLoading")}</div>)}
        {read.state === "error" && <div className="usage-state" role="alert"><p>{errorWord(read.code)}</p><button type="button" onClick={() => setRevision(n => n + 1)}>{w("usageRetry")}</button></div>}
        {data && <>
          <div className="usage-totals">
            <Total label={w("usageTokens")} value={data.totals.tokens} previous={data.previous_totals.tokens} unit="Token" complete={comparisonComplete(data)} />
            <Total label={w("usageTasks")} value={data.totals.dispatches} previous={data.previous_totals.dispatches} unit={w("usageTaskUnit")} complete={!data.coverage.tasks_truncated} />
          </div>
          {data.coverage.state !== "complete" && <p className="usage-coverage" role="status">{w(data.coverage.state === "missing" ? "usageMissing" : "usagePartial")}</p>}
          {data.totals.tokens === 0 && data.totals.dispatches === 0 && data.coverage.state === "complete" && <p role="status" className="usage-coverage">{w("usageEmpty")}</p>}
          <div className="usage-chart-head"><div className="usage-switch" role="group" aria-label={w("usageOverview")}>
            <button type="button" aria-pressed={metric === "tokens"} onClick={() => setMetric("tokens")}>{w("usageTokens")}</button>
            <button type="button" aria-pressed={metric === "dispatches"} onClick={() => setMetric("dispatches")}>{w("usageTasks")}</button>
          </div><span>{metric === "tokens" ? "Token" : w("usageTaskUnit")}</span></div>
          <DailyChart key={metric} data={data} metric={metric} label={w(metric === "tokens" ? "usageTokens" : "usageTasks")} />
          <div className="usage-columns">
            <section><h2>{w("usageProjects")}</h2><div className="usage-projects">
              {data.projects.length === 0 && <p>{w(data.totals.tokens === null ? "usageMissing" : "usageEmpty")}</p>}
              {data.projects.map(row => {
                const icon = pickerOptions.find(option => option.id === row.id)?.icon
                return <div key={row.id} className="usage-project-row">
                  <span className="usage-project-mark" aria-hidden="true">{icon
                    ? <Mark icon={icon as SessionRow["icon"]} cellPx={3} />
                    : <span className="project-picker-placeholder">▦</span>}</span>
                  <span className="usage-project-name">{row.id === "unattributed" ? w("usageUnattributed") : row.name}</span>
                  <div className="usage-track" aria-hidden="true"><div style={{ width: `${row.tokens / Math.max(1, ...data.projects.map(p => p.tokens)) * 100}%` }} /></div>
                  <span>{compactValue(row.tokens)}</span>
                </div>
              })}
            </div></section>
            <Composition data={data} />
          </div>
          <section className="usage-duration"><div className="usage-chart-head"><div><h2>{w("usageDuration")}</h2><p>{w("usageDurationCaption", { count: data.totals.duration_samples })}</p><p>{w("usageDurationDefinition")}</p></div>
            <div className="usage-duration-total"><strong>{compactValue(data.totals.duration_samples === 0 || data.totals.duration_median_seconds === null ? null : data.totals.duration_median_seconds / 60)}</strong> {w("usageMinutes")}
              {data.previous_totals.duration_samples > 0 && data.previous_totals.duration_median_seconds !== null && <small>{w("usagePrevious")} {compactValue(data.previous_totals.duration_median_seconds / 60)} {w("usageMinutes")}</small>}
            </div></div><DailyChart data={data} metric="duration" label={w("usageDuration")} />
          </section>
          <footer className="usage-metadata"><span>{w("usageReadCoverage", { read: data.coverage.sessions_read, total: data.coverage.sessions_total })}</span><span>{w("usageTokenIncludesCache")}</span></footer>
        </>}
      </div>
    </div>
  </section>
}

function Total({ label, value, previous, unit, complete }: { label: string; value: number | null; previous: number | null; unit: string; complete: boolean }) {
  const delta = percentageChange(value, previous, complete)
  return <div className="usage-total"><div>{label}</div><div className="usage-number">{compactValue(value)}<small>{unit}</small></div>
    <div className="usage-prior">{w("usagePrevious")} {compactValue(previous)} {unit}{delta && <span>{delta}</span>}</div></div>
}

function Composition({ data }: { data: UsageOverview }) {
  const rows = [
    { label: w("usageCacheRead"), value: data.totals.parts.cache_read },
    { label: w("usageOutput"), value: data.totals.parts.output },
    { label: w("usageCacheWrite"), value: data.totals.parts.cache_write },
    { label: w("usageInput"), value: data.totals.parts.input },
  ]
  const total = rows.reduce((sum, row) => sum + row.value, 0)
  return <section><h2>{w("usageComposition")}</h2>
    {data.totals.tokens === null || total === 0 ? <p className="usage-coverage">{w(data.totals.tokens === null ? "usageMissing" : "usageEmpty")}</p> : <>
      <div className="usage-composition" aria-hidden="true">{rows.map((row, index) => <span key={row.label} data-part={index} style={{ width: `${row.value / total * 100}%` }} />)}</div>
      <dl className="usage-parts">{rows.map((row, index) => <div key={row.label}><dt><i data-part={index} aria-hidden="true" />{row.label}</dt><dd>{Math.round(row.value / total * 100)}% <small>{compactValue(row.value)}</small></dd></div>)}</dl>
    </>}
  </section>
}

/** A single keyboard stop with arrow navigation; pointer/touch selects the same daily pair. */
export function DailyChart({ data, metric, label }: { data: UsageOverview; metric: UsageMetric; label: string }) {
  const [index, setIndex] = useState(0)
  const id = useId()
  const host = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(700)
  useEffect(() => {
    const observer = new ResizeObserver(entries => setWidth(Math.max(280, entries[0]?.contentRect.width ?? 700)))
    if (host.current) observer.observe(host.current)
    return () => observer.disconnect()
  }, [])
  const values = data.daily.map(day => dailyValue(day, metric))
  const previous = data.previous_daily.map(day => dailyValue(day, metric))
  const max = Math.max(1, ...[...values, ...previous].filter((n): n is number => n !== null))
  const maximum = Math.ceil(max / 3) * 3
  const currentIndex = Math.min(index, Math.max(0, values.length - 1))
  const locale = currentCatalogTag()
  const samples = (count: number) => sampleLabel(count, locale, w("usageSampleOne"), w("usageSamples"))
  const unit = metric === "tokens" ? "Token" : w(metric === "duration" ? "usageMinutes" : "usageTaskUnit")
  const valueLabel = (value: number | null) => value === null ? w("usageNoValue") : `${value.toLocaleString(locale, { maximumFractionDigits: 2 })} ${unit}`
  const day = data.daily[currentIndex]
  const priorDay = data.previous_daily[currentIndex]
  const pair = `${w("usageCurrent")} ${day ? dateRange(day.date, day.date, locale) : ""}: ${valueLabel(values[currentIndex] ?? null)} · ${w("usagePrevious")} ${priorDay ? dateRange(priorDay.date, priorDay.date, locale) : ""}: ${valueLabel(previous[currentIndex] ?? null)}${metric === "duration" ? ` · ${samples(day?.duration_samples ?? 0)} / ${samples(priorDay?.duration_samples ?? 0)}` : ""}`
  return <div className="usage-chart" ref={host}>
    <div className="usage-legend"><span><i />{w("usageCurrent")}</span><span><i />{w("usagePrevious")}</span><span>{unit}</span></div>
    <div className="usage-chart-control" tabIndex={0} role="slider" aria-valuemin={1} aria-valuemax={Math.max(1, values.length)} aria-valuenow={currentIndex + 1} aria-valuetext={pair} aria-label={label} aria-describedby={`${id}-help ${id}-value`}
      onKeyDown={event => { if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) { event.preventDefault(); setIndex(selectedDay(currentIndex, event.key, values.length)) } }}
      onPointerDown={event => { event.currentTarget.focus({ preventScroll: true }); const rect = event.currentTarget.getBoundingClientRect(); setIndex(Math.max(0, Math.min(values.length - 1, Math.round(((event.clientX - rect.left) * width / rect.width - 54) / (width - 78) * (values.length - 1))))) }}
      onPointerMove={event => { if (event.pointerType === "mouse" || event.buttons) { const rect = event.currentTarget.getBoundingClientRect(); setIndex(Math.max(0, Math.min(values.length - 1, Math.round(((event.clientX - rect.left) * width / rect.width - 54) / (width - 78) * (values.length - 1))))) } }}>
      <svg viewBox={`0 0 ${width} 210`} aria-hidden="true">
        {[0, 1, 2, 3].map(tick => { const y = 174 - tick * 50; return <g key={tick}><line className="usage-grid" x1="54" x2={width - 24} y1={y} y2={y} /><text x="0" y={y + 4}>{compactValue(maximum * tick / 3)}</text></g> })}
        {data.daily.map((day, i) => (i === 0 || i === values.length - 1 || i % Math.ceil(values.length / (width < 500 ? 4 : 7)) === 0) && <text key={day.date} x={54 + i * (width - 78) / Math.max(1, values.length - 1)} y="202" textAnchor="middle">{shortDate(day.date, locale)}</text>)}
        {lineSegments(previous, maximum, width).map((d, i) => <path key={`p${i}`} d={d} className="usage-line usage-line-prior" />)}
        {lineSegments(values, maximum, width).map((d, i) => <path key={`c${i}`} d={d} className="usage-line" />)}
        {[{ series: previous, prior: true }, { series: values, prior: false }].map(({ series, prior }) => series.map((value, i) => value !== null && <circle key={`${prior}-${i}`} className={prior ? "usage-dot usage-dot-prior" : "usage-dot"} cx={54 + i * (width - 78) / Math.max(1, values.length - 1)} cy={174 - value / maximum * 150} r={i === currentIndex ? 4.5 : 2} />))}
        <line className="usage-cursor" x1={54 + currentIndex * (width - 78) / Math.max(1, values.length - 1)} x2={54 + currentIndex * (width - 78) / Math.max(1, values.length - 1)} y1="20" y2="178" />
      </svg>
    </div>
    <p id={`${id}-value`} className="usage-chart-value" aria-live="polite" aria-atomic="true">{pair}</p>
    <p id={`${id}-help`} className="usage-chart-help">{w("usageChartHelp")}</p>
  </div>
}
