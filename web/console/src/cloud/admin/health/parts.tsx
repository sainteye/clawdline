import { StatusIcon } from "../../../ui/StatusIcon.js"
/* Pieces the six connection-health screens share: status words with glyphs,
 * tables that become cards at phone width, the shared timeline and its table. */
import { useId, useState, type ReactNode } from "react"
import type { Incident, Lane, Outcome, Quality, SpanState } from "./types"
import { placeInstant, placeLane, latencyView, rateView } from "./model"
import { healthClock, healthDuration, healthMs, type HealthSay, type HealthWord } from "./words"
import type { HealthTarget } from "./model"
import type { Latency, Ratio } from "./types"

/** How a screen moves. The root wires it to the admin router; `href` makes every route a real link. */
export type HealthNav = { href: (t: HealthTarget) => string; go: (t: HealthTarget) => void }

export type ScreenProps = { say: HealthSay; now: number; nav: HealthNav }

/** Severity is always a glyph plus a word, never colour alone. */
const GLYPH: Record<SpanState, string> = {
  blocked: "■", offline: "○", error: "✕", slow: "▲", unknown: "?", no_requests: "–", healthy: "✓", not_retained: "…", forbidden: "—",
}

export function stateWord(say: HealthSay, s: SpanState): string {
  if (s === "not_retained") return say("span_not_retained")
  if (s === "forbidden") return say("span_forbidden")
  return say(`outcome_${s}` as HealthWord)
}

export function OutcomeBadge({ outcome, say }: { outcome: Outcome; say: HealthSay }) {
  return (
    <span className={`adh-badge adh-o-${outcome}`}>
      <span aria-hidden="true" className="adh-glyph">{outcome === "slow" ? <StatusIcon name="clock" /> : GLYPH[outcome]}</span>
      {say(`outcome_${outcome}` as HealthWord)}
    </span>
  )
}

export function QualityChip({ quality, say }: { quality: Quality; say: HealthSay }) {
  return <span className={`adh-chip adh-q-${quality}`}>{say(`quality_${quality}` as HealthWord)}</span>
}

export function Unknown({ children }: { children: ReactNode }) {
  return <span className="adm-unknown">{children}</span>
}

export function Panel({ title, children, actions, id }: { title: string; children: ReactNode; actions?: ReactNode; id?: string }) {
  const auto = useId()
  const hid = id ?? auto
  return (
    <section className="adm-panel adh-panel" aria-labelledby={hid}>
      <div className="adh-panel-head">
        <h2 className="adm-h3" id={hid}>{title}</h2>
        {actions ? <div className="adh-panel-actions">{actions}</div> : null}
      </div>
      {children}
    </section>
  )
}

export type Column = { label: string; num?: boolean }
export type Row = { key: string; head: ReactNode; cells: ReactNode[] }

/** A real table; below 560 px each row becomes a card whose cells carry their own labels. */
export function HTable({ caption, columns, rows }: { caption: string; columns: Column[]; rows: Row[] }) {
  return (
    <div className="adm-table-wrap">
      <table className="adm-table adh-cards">
        <caption className="adm-visually-hidden">{caption}</caption>
        <thead>
          <tr>{columns.map((c, i) => <th key={i} scope="col" className={c.num ? "num" : undefined}>{c.label}</th>)}</tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.key}>
              <th scope="row" data-label={columns[0]?.label}>{r.head}</th>
              {r.cells.map((cell, i) => (
                <td key={i} data-label={columns[i + 1]?.label} className={columns[i + 1]?.num ? "num" : undefined}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function LatencyText({ latency, say }: { latency: Latency | null; say: HealthSay }) {
  const v = latencyView(latency)
  if (v.kind === "unknown") return <Unknown>{say("unknown")}</Unknown>
  return <>{healthMs(say, v.p50)}</>
}

export function latencyCells(latency: Latency | null, say: HealthSay): ReactNode[] {
  const v = latencyView(latency)
  if (v.kind === "unknown") return [<Unknown key="a">{say("unknown")}</Unknown>, <Unknown key="b">{say("unknown")}</Unknown>, <Unknown key="c">{say("unknown")}</Unknown>, latency === null ? <Unknown key="d">{say("unknown")}</Unknown> : String(latency.samples)]
  return [
    healthMs(say, v.p50),
    healthMs(say, v.p95),
    v.tooFewForP99 ? <Unknown key="c">{say("tooFew", { n: v.samples })}</Unknown> : healthMs(say, v.p99),
    String(v.samples),
  ]
}

export function RateText({ ratio, say }: { ratio: Ratio | null; say: HealthSay }) {
  const v = rateView(ratio)
  if (v.kind === "unknown") return <Unknown>{say("unknown")}</Unknown>
  if (v.kind === "no_requests") return <Unknown>{say("outcome_no_requests")}</Unknown>
  return <>{`${v.percent < 10 ? v.percent.toFixed(1) : Math.round(v.percent)}%`}</>
}

export function Age({ seconds, say }: { seconds: number | null; say: HealthSay }) {
  return seconds === null ? <Unknown>{say("unknown")}</Unknown> : <>{healthDuration(say, seconds)}</>
}

export function ageSince(iso: string | null, now: number): number | null {
  return iso === null ? null : Math.max(0, Math.round((now - Date.parse(iso)) / 1000))
}

export function Crumb({ say, nav }: { say: HealthSay; nav: HealthNav }) {
  const home: HealthTarget = { screen: "M01" }
  return (
    <a className="adm-crumb adh-crumb" href={nav.href(home)} onClick={(e) => linkClick(e, () => nav.go(home))}>
      <span aria-hidden="true">← </span>{say("crumbHome")}
    </a>
  )
}

/** Router links: an ordinary click navigates in place, a modified click keeps the browser's behaviour. */
export function linkClick(e: React.MouseEvent, go: () => void) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
  e.preventDefault()
  go()
}

export function NavLink({ to, nav, children, className }: { to: HealthTarget; nav: HealthNav; children: ReactNode; className?: string }) {
  return <a className={className ?? "adh-link"} href={nav.href(to)} onClick={(e) => linkClick(e, () => nav.go(to))}>{children}</a>
}

/* ---- the shared timeline -------------------------------------------------- */

export type TimelineLabel = (lane: Lane) => string

/** One track per lane over the window. States differ by height and pattern, not colour alone;
 *  time without an observation is drawn as unknown. "Show as table" swaps in the same data. */
export function Timeline({ title, lanes, window, label, incidents, say, nav }: {
  title: string; lanes: Lane[]; window: { start: string; end: string }; label: TimelineLabel
  incidents: Incident[]; say: HealthSay; nav: HealthNav
}) {
  const [asTable, setAsTable] = useState(false)
  const toggle = (
    <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" aria-pressed={asTable} onClick={() => setAsTable((v) => !v)}>
      {asTable ? say("showChart") : say("showTable")}
    </button>
  )
  const spanText = (state: SpanState, covered: boolean) => (covered ? stateWord(say, state) : `${stateWord(say, "unknown")} (${say("laneUncovered")})`)
  return (
    <Panel title={title} actions={lanes.length ? toggle : undefined}>
      <p className="adm-fine adh-axis-note">{healthClock(window.start)}–{healthClock(window.end)} UTC</p>
      {lanes.length === 0 ? (
        <p className="adm-fine">{say("notVisible")}</p>
      ) : asTable ? (
        <HTable
          caption={title}
          columns={[{ label: say("tableLane") }, { label: say("tableFrom") }, { label: say("tableTo") }, { label: say("tableState") }]}
          rows={lanes.flatMap((lane) => [
            ...placeLane(lane, window).map((p, i) => ({
              key: `${lane.segment}-${i}`,
              head: label(lane),
              cells: [`${healthClock(p.start)} UTC`, `${healthClock(p.end)} UTC`, spanText(p.state, p.covered)],
            })),
            ...lane.ticks.filter((t) => placeInstant(t.at, window) !== null).map((t, i) => ({
              key: `${lane.segment}-t${i}`,
              head: label(lane),
              cells: [`${healthClock(t.at)} UTC`, "", `${say("events")}: ${t.label}`],
            })),
          ])}
        />
      ) : (
        <div className="adh-lanes">
          {incidents.map((inc) => {
            const left = placeInstant(inc.started_at, window) ?? (Date.parse(inc.started_at) < Date.parse(window.start) ? 0 : null)
            if (left === null) return null
            const right = inc.ended_at === null ? 100 : placeInstant(inc.ended_at, window) ?? 100
            return (
              <div key={inc.id} className="adh-band-row">
                <span className="adh-lane-name" aria-hidden="true" />
                <div className="adh-band-track">
                  <span className="adh-band" style={{ left: `${left}%`, width: `${Math.max(0.5, right - left)}%` }} aria-hidden="true" />
                  <a className="adh-marker" style={{ left: `${left}%` }} href={nav.href({ screen: "M05", incident: inc.id })}
                    onClick={(e) => linkClick(e, () => nav.go({ screen: "M05", incident: inc.id }))}>
                    <span aria-hidden="true">{GLYPH[inc.outcome]} </span>{say("incidentMarker", { id: inc.id })}
                  </a>
                </div>
              </div>
            )
          })}
          {lanes.map((lane) => {
            const placed = placeLane(lane, window)
            const sentence = placed.map((p) => `${healthClock(p.start)}–${healthClock(p.end)} ${spanText(p.state, p.covered)}`).join("; ")
            return (
              <div key={lane.segment} className="adh-lane">
                <span className="adh-lane-name">{label(lane)}</span>
                <div className="adh-track" role="img" aria-label={`${label(lane)}: ${sentence}`}>
                  {placed.map((p, i) => (
                    <span key={i} className={`adh-span adh-s-${p.state}${p.covered ? "" : " adh-uncovered"}`} style={{ left: `${p.left}%`, width: `${p.width}%` }} />
                  ))}
                  {lane.ticks.map((t, i) => {
                    const at = placeInstant(t.at, window)
                    return at === null ? null : <span key={`t${i}`} className="adh-tick" style={{ left: `${at}%` }} title={t.label} />
                  })}
                </div>
              </div>
            )
          })}
          <Legend say={say} />
        </div>
      )}
    </Panel>
  )
}

function Legend({ say }: { say: HealthSay }) {
  const states: SpanState[] = ["healthy", "slow", "blocked", "error", "offline", "unknown", "no_requests", "not_retained", "forbidden"]
  return (
    <ul className="adh-legend" aria-hidden="true">
      {states.map((s) => (
        <li key={s}><span className={`adh-key adh-s-${s}`} />{stateWord(say, s)}</li>
      ))}
    </ul>
  )
}
