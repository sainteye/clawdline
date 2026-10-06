import { catalogFormat } from "../catalog.js"
import { catalogWord } from "../catalog.js"
import type { MachineUsage, SessionRow } from "@clawdline/contract"
import { useEffect, useMemo, useRef, useState } from "react"
import { RefusalError } from "@clawdline/core"
import {
  assignSlots,
  bytes,
  cpuLevel,
  memoryLevel,
  memoryPercent,
  memorySegments,
  remember,
  rows as usageRows,
  say,
  sparkPoints,
  swapLevel,
  swapPercent,
  verdict,
  type Level,
  type Sort,
} from "./model.js"
import { failureWords, readMachineUsage } from "./read.js"
import { Mark } from "../session/List.js"
import { nextWord } from "../next-strings.js"
import "./machine.css"

/** How often an open dashboard asks. The route answers a second ask inside 1.5 s from its last reading. */
const EVERY_MS = 3000
const HISTORY = 40
/** The sparkline spans at least this many readings, so a freshly opened one is a line, not a dot. */
const SPARK_MIN = 8

function chinese(): boolean {
  const lang = typeof document === "undefined" ? "" : document.documentElement.lang
  const nav = typeof navigator === "undefined" ? "" : navigator.language
  return /^zh/i.test(lang || nav || "")
}

/**
 * The machine dashboard, opened from the session counts beside the wordmark:
 * whether this machine is why everything is slow, and which session is using
 * it. Mounted only while open, so a closed dashboard asks nothing.
 */
export function MachineDashboard({ sessions, onClose, onClawdfather }: {
  sessions: readonly SessionRow[]
  onClose: () => void
  onClawdfather: (id: string | null) => void
}) {
  const zh = chinese()
  const [usage, setUsage] = useState<MachineUsage | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [sort, setSort] = useState<Sort>("memory")
  const [cpuHistory, setCpuHistory] = useState<number[]>([])
  const [memHistory, setMemHistory] = useState<number[]>([])
  const slotsRef = useRef<Map<string, number>>(new Map())
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    let alive = true
    let timer: ReturnType<typeof setTimeout> | undefined
    const tick = async () => {
      if (typeof document !== "undefined" && document.hidden) {
        timer = setTimeout(tick, EVERY_MS)
        return
      }
      try {
        const next = await readMachineUsage()
        if (!alive) return
        setUsage(next)
        setError(null)
        setCpuHistory((h) => remember(h, next.cpu_percent, HISTORY))
        setMemHistory((h) => remember(h, memoryPercent(next), HISTORY))
      } catch (err) {
        if (!alive) return
        setError(err)
        // A machine without a reader will not grow one while this is open.
        if (err instanceof RefusalError && err.code === "machine_usage_unsupported") return
      }
      if (alive) timer = setTimeout(tick, EVERY_MS)
    }
    void tick()
    return () => {
      alive = false
      if (timer) clearTimeout(timer)
    }
  }, [])

  useEffect(() => {
    closeRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation()
        onClose()
      }
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [onClose])

  const slots = useMemo(() => {
    if (!usage) return slotsRef.current
    const ids = usage.groups.filter((g) => g.kind === "session").map((g) => g.id)
    const next = assignSlots(ids, slotsRef.current)
    slotsRef.current = next
    return next
  }, [usage])

  const list = useMemo(() => (usage ? usageRows(usage, sessions, slots, sort, zh) : []), [usage, sessions, slots, sort, zh])
  const segments = useMemo(() => (usage ? memorySegments(usage, list, zh) : []), [usage, list, zh])
  const top = useMemo(() => {
    const most = Math.max(1, ...list.map((r) => (sort === "cpu" ? r.cpu : r.rss + r.swap)))
    return most
  }, [list, sort])

  const v = usage ? verdict(usage) : null
  const stale = !!usage && !!error
  const steward = sessions.find((row) => row.machine_scope || row.coordinator)

  return (
    <div className="overlay machine-overlay" onClick={onClose}>
      <div
        className="sheet machine-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="machine-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="machine-head">
          <div className="machine-heading">
            <h2 id="machine-title">{catalogWord("literal", "0a0017caec11")}</h2>
            <span className="machine-sub">
              {usage ? (
                <>
                  {catalogFormat("template", "7e8b4dfff169", [usage.cores, bytes(usage.memory_total_bytes)])}
                  <span className={`machine-live${stale ? " stale" : ""}`} aria-hidden="true" />
                  {stale ? (catalogWord("literal", "7361fddb7d5f")) : catalogWord("literal", "371d49dd8b8f")}
                </>
              ) : error ? null : catalogWord("literal", "f8533719e17a")}
            </span>
          </div>
          <button className="machine-close" type="button" ref={closeRef} onClick={onClose} aria-label={catalogWord("literal", "82860b805ea6")}>
            ×
          </button>
        </div>

        {!usage && error ? <p className="machine-verdict" data-level="warn">{failureWords(error, zh)}</p> : null}
        {!usage && !error ? <Skeleton /> : null}
        <button className="machine-clawdfather" type="button" onClick={() => onClawdfather(steward?.id ?? null)}>
          <span className="clawdfather-crown" aria-hidden="true" />
          <span>{steward ? (catalogWord("literal", "bb1756b831b9")) : nextWord("machineSessionStart")}</span>
        </button>

        {usage && v ? (
          <>
            <p className="machine-verdict" data-level={v.level} role="status" aria-live="polite">
              <LevelMark level={v.level} zh={zh} />
              <span>{say(v, true)}</span>
            </p>

            <div className="machine-gauges">
              <Gauge
                label="CPU"
                value={`${Math.round(usage.cpu_percent)}%`}
                percent={usage.cpu_percent}
                level={cpuLevel(usage)}
                sub={`load ${usage.load.map((n) => n.toFixed(1)).join(" · ")}`}
                history={cpuHistory}
                zh={zh}
              />
              <Gauge
                label={catalogWord("literal", "e962e81b9996")}
                value={`${Math.round(memoryPercent(usage))}%`}
                percent={memoryPercent(usage)}
                level={memoryLevel(usage)}
                sub={`${bytes(usage.memory_used_bytes)} / ${bytes(usage.memory_total_bytes)}`}
                history={memHistory}
                zh={zh}
              />
              <Gauge
                label="Swap"
                value={usage.swap_total_bytes > 0 ? `${Math.round(swapPercent(usage))}%` : "—"}
                percent={swapPercent(usage)}
                level={swapLevel(usage)}
                sub={usage.swap_total_bytes > 0 ? `${bytes(usage.swap_used_bytes)} / ${bytes(usage.swap_total_bytes)}` : catalogWord("literal", "2826e9975083")}
                zh={zh}
              />
            </div>

            {usage.pressure ? (
              <p className="machine-pressure">
                <span className="machine-label">{catalogWord("literal", "6965bbac3e3d")}</span>
                <Wait name="CPU" value={usage.pressure.cpu_some} />
                <Wait name={catalogWord("literal", "35c033c1dbfc")} value={usage.pressure.memory_some} />
                <Wait name={catalogWord("literal", "3f30b2c523df")} value={usage.pressure.io_some} />
              </p>
            ) : null}

            <section className="machine-section">
              <h3>{catalogWord("literal", "d173b3f0bd70")}</h3>
              <div className="machine-stack" role="img" aria-label={segments.map((s) => `${s.label} ${bytes(s.bytes)}`).join(", ")}>
                {segments.map((s) => (
                  <i
                    key={s.key}
                    style={{ width: `${s.percent}%`, background: s.color }}
                    title={`${s.label} · ${bytes(s.bytes)} · ${s.percent.toFixed(0)}%`}
                  />
                ))}
              </div>
              <div className="machine-scale">
                <span>{catalogFormat("template", "b8faa287adb9", [bytes(usage.memory_used_bytes)])}</span>
                <span>{catalogFormat("template", "ca37c38d230b", [bytes(usage.memory_available_bytes)])}</span>
              </div>
            </section>

            <section className="machine-section">
              <div className="machine-section-head">
                <h3>{catalogFormat("template", "82697b4e4f82", [list.filter((r) => r.kind === "session").length])}</h3>
                <div className="machine-sort" role="group" aria-label={catalogWord("literal", "bde9237d920d")}>
                  <button type="button" aria-pressed={sort === "memory"} onClick={() => setSort("memory")}>
                    {catalogWord("literal", "e962e81b9996")}
                  </button>
                  <button type="button" aria-pressed={sort === "cpu"} onClick={() => setSort("cpu")}>
                    CPU
                  </button>
                </div>
              </div>
              <ul className="machine-rows">
                {list.map((r) => {
                  const weight = sort === "cpu" ? r.cpu : r.rss + r.swap
                  return (
                    <li key={r.key} className="machine-row" data-kind={r.kind}>
                      {r.icon ? (
                        <span className="machine-dot machine-icon" aria-hidden="true">
                          <Mark icon={r.icon} cellPx={2} />
                        </span>
                      ) : (
                        <span className="machine-dot" style={{ background: r.color }} aria-hidden="true" />
                      )}
                      <div className="machine-who">
                        <span className="machine-title">{r.title}</span>
                        <span className="machine-detail">{r.detail}</span>
                      </div>
                      <div className="machine-amounts">
                        <span className="machine-mem">
                          {bytes(r.rss)}
                          {r.swap > 0 ? <em title={catalogWord("literal", "4f2ba7c98279")}> +{bytes(r.swap)} swap</em> : null}
                        </span>
                        <span className="machine-cpu">{r.cpu.toFixed(r.cpu < 10 ? 1 : 0)}{catalogWord("inline", "6ebb2cb97b4d")}</span>
                      </div>
                      <div className="machine-bar" aria-hidden="true">
                        <i style={{ width: `${Math.max(weight > 0 ? 1.5 : 0, (weight / top) * 100)}%`, background: r.color }} />
                      </div>
                    </li>
                  )
                })}
                {list.length === 0 ? <li className="machine-empty">{catalogWord("literal", "4420cacfb8e8")}</li> : null}
              </ul>
            </section>

            {usage.others.length > 0 ? (
              <details className="machine-others">
                <summary>{catalogWord("literal", "5d2f213c46e2")}</summary>
                <ul>
                  {usage.others.map((o) => (
                    <li key={o.name}>
                      <span className="machine-title">{o.name}</span>
                      <span className="machine-detail">{catalogFormat("template", "c4e5c8cf3d7b", [o.processes])}</span>
                      <span className="machine-mem">{bytes(o.rss_bytes)}</span>
                      <span className="machine-cpu">{o.cpu_percent.toFixed(o.cpu_percent < 10 ? 1 : 0)}{catalogWord("inline", "6ebb2cb97b4d")}</span>
                    </li>
                  ))}
                </ul>
              </details>
            ) : null}

            <p className="machine-note">
              {catalogFormat("template", "37d294810b2a", [(usage.interval_ms / 1000).toFixed(1), usage.cores])}
            </p>
          </>
        ) : null}
      </div>
    </div>
  )
}

function LevelMark({ level, zh }: { level: Level; zh: boolean }) {
  const word = level === "bad" ? (catalogWord("literal", "0547c3976b56")) : level === "warn" ? (catalogWord("literal", "de1f4ca207b0")) : catalogWord("literal", "96e69c58bf8b")
  const icon = level === "bad" ? "!" : level === "warn" ? "▲" : "✓"
  return (
    <span className="machine-level" data-level={level}>
      <b aria-hidden="true">{icon}</b>
      {word}
    </span>
  )
}

function Gauge({
  label,
  value,
  percent,
  level,
  sub,
  history,
  zh,
}: {
  label: string
  value: string
  percent: number
  level: Level
  sub: string
  history?: number[]
  zh: boolean
}) {
  const w = 120
  const h = 28
  return (
    <div className="machine-gauge" data-level={level}>
      <div className="machine-gauge-top">
        <span className="machine-label">{label}</span>
        <LevelMark level={level} zh={zh} />
      </div>
      <div className="machine-gauge-mid">
        <div className="machine-value">{value}</div>
        {history ? (
          // The last two minutes, filling from the right; the baseline is
          // drawn from the start so a short line reads as "so far".
          <svg className="machine-spark" viewBox={`0 0 ${w} ${h}`} aria-hidden="true" preserveAspectRatio="none">
            <line className="machine-spark-base" x1="0" y1={h - 0.5} x2={w} y2={h - 0.5} />
            {history.length > 1 ? <polyline points={sparkPoints(history, w, h, Math.max(SPARK_MIN, history.length))} /> : null}
          </svg>
        ) : null}
      </div>
      <div className="machine-meter" aria-hidden="true">
        <i style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />
      </div>
      <span className="machine-sub-line">{sub}</span>
    </div>
  )
}

function Wait({ name, value }: { name: string; value: number }) {
  const level: Level = value >= 20 ? "bad" : value >= 5 ? "warn" : "ok"
  return (
    <span className="machine-wait" data-level={level}>
      {name} <b>{value.toFixed(value < 10 ? 1 : 0)}%</b>
    </span>
  )
}

function Skeleton() {
  return (
    <div className="machine-skeleton" aria-hidden="true">
      <i />
      <div className="machine-gauges">
        <i />
        <i />
        <i />
      </div>
      <i />
      <i />
    </div>
  )
}
