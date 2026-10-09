import { useMemo, useState } from "react"
import { buildAttentionOverview, filterAttention, targetKey, type AttentionFilter, type AttentionKind, type Freshness, type MachineStatusSnapshot, type SessionState, type SessionTarget } from "./attention-model.js"
import { attentionKindWord, attentionWords, formatAttentionReport, freshnessWord, machineGap, nextStep, type AttentionLocale } from "./attention-report.js"
import "./attention.css"

export type AttentionReading =
  | { phase: "loading" }
  | { phase: "error" }
  | { phase: "ready"; machines: readonly MachineStatusSnapshot[]; observedNow: number }

const ui = {
  en: { loading: "Reading machine status…", error: "Machine status could not be read.", retry: "Retry",
    machine: "Machine", machineID: "Machine ID", sessionID: "Session ID", platform: "Platform", state: "Session state", freshness: "Freshness", kind: "Attention type",
    all: "All", known: "known items", unknown: "machines with incomplete status", empty: "No matching known items.", filters: "Filter attention items",
    report: "Show manager report", hide: "Hide manager report", copy: "Copy report", copying: "Copying…", copied: "Report copied.", copyFailed: "Could not copy the report. Select the text below to copy it.",
    open: "Open Session", observed: "Observed", generation: "Snapshot generation", gap: "Gap", next: "Next step", unavailable: "Unavailable", source: "Source", coverage: "Machine coverage", evidence: "Evidence and gaps" },
  "zh-Hant-TW": { loading: "正在讀取機器狀態…", error: "無法讀取機器狀態。", retry: "重試",
    machine: "機器", machineID: "機器 ID", sessionID: "Session ID", platform: "平台", state: "Session 狀態", freshness: "新鮮度", kind: "注意類別",
    all: "全部", known: "個已知待處理項目", unknown: "台機器資料不完整", empty: "沒有符合篩選條件的已知項目。", filters: "篩選待處理項目",
    report: "顯示管理者報告", hide: "隱藏管理者報告", copy: "複製報告", copying: "複製中…", copied: "報告已複製。", copyFailed: "無法複製報告；可選取下方文字複製。",
    open: "開啟 Session", observed: "觀察時間", generation: "快照世代", gap: "資料缺口", next: "下一步", unavailable: "無法使用", source: "來源", coverage: "機器涵蓋範圍", evidence: "依據與缺口" },
} as const

const kinds: AttentionKind[] = ["reply", "blocked", "failed", "no_progress", "unconfirmed", "offline"]
const states: SessionState[] = ["working", "waiting", "blocked", "failed", "completed", "idle", "unknown"]
const freshnesses: Freshness[] = ["current", "stale", "offline", "unknown"]

/** Embeddable next to the combined Session list once its ss/ adapter is ready. */
export function AttentionOverview({ reading, locale, onOpen, onRetry }: {
  reading: AttentionReading
  locale: AttentionLocale
  onOpen?: (target: SessionTarget) => void
  onRetry?: () => void
}) {
  const t = ui[locale]
  const w = attentionWords(locale)
  const [filter, setFilter] = useState<AttentionFilter>({})
  const [showReport, setShowReport] = useState(false)
  const [feedback, setFeedback] = useState("")
  const [copying, setCopying] = useState(false)
  const overview = useMemo(() => reading.phase === "ready" ? buildAttentionOverview(reading.machines, reading.observedNow) : null, [reading])
  const matches = overview ? filterAttention(overview.entries, filter) : []
  const report = overview && reading.phase === "ready" ? formatAttentionReport(overview, reading.observedNow, locale) : ""
  const options = (values: readonly string[], selected: string | undefined, change: (value: string) => void, word: (value: string) => string) => (
    <select value={selected ?? ""} onChange={(event) => change(event.target.value)}>
      <option value="">{t.all}</option>
      {values.map((value) => <option key={value} value={value}>{word(value)}</option>)}
    </select>
  )
  return <section className="attention-overview" aria-labelledby="attention-heading">
    <h2 id="attention-heading">{w.title}</h2>
    {reading.phase === "loading" && <p className="attention-loading" role="status">{t.loading}</p>}
    {reading.phase === "error" && <div role="alert"><p>{t.error}</p>{onRetry && <button type="button" onClick={onRetry}>{t.retry}</button>}</div>}
    {overview && <>
      <p className="attention-summary" role="status">{overview.entries.length} {t.known} · {overview.unknownMachines} {t.unknown}</p>
      {!overview.complete && <p className="attention-gap" role="note">{w.incomplete}</p>}
      <details className="attention-coverage"><summary>{t.coverage} · {overview.machines.length}</summary>
        <ul>{overview.machines.map((machine) => <li key={machine.machine_id}>
          <strong>{machine.name}</strong> · <code>{machine.machine_id}</code> · {machine.platform} · {freshnessWord(machine, locale)}
          <small>{t.source}: ss/{machine.machine_id} · {t.observed}: {machine.observedAt === null ? w.unknown : new Date(machine.observedAt).toLocaleString(locale)} · {t.generation}: {machine.snapshotGeneration ?? w.unknown}</small>
          <small>{w.completeness}: {machine.completeness === "complete" ? w.complete : machine.completeness === "partial" ? w.partial : w.unknownComplete} · {t.gap}: {machineGap(machine, locale)}</small>
        </li>)}</ul>
      </details>
      <details className="attention-filter-disclosure"><summary>{t.filters}</summary>
      <div className="attention-filters">
        <label>{t.machine}{options(overview.machines.map((m) => m.machine_id), filter.machine, (machine) => setFilter((f) => ({ ...f, machine })), (id) => `${overview.machines.find((m) => m.machine_id === id)?.name ?? id} · ${id}`)}</label>
        <label>{t.platform}{options([...new Set(overview.machines.map((m) => m.platform))], filter.platform, (platform) => setFilter((f) => ({ ...f, platform })), (value) => value)}</label>
        <label>{t.state}{options(states, filter.state, (state) => setFilter((f) => ({ ...f, state: state as SessionState })), (value) => value)}</label>
        <label>{t.freshness}{options(freshnesses, filter.freshness, (freshness) => setFilter((f) => ({ ...f, freshness: freshness as Freshness })), (value) => value === "stale" ? w.stale : value === "offline" ? w.offline : value === "unknown" ? w.unknownFresh : w.current)}</label>
        <label>{t.kind}{options(kinds, filter.kind, (kind) => setFilter((f) => ({ ...f, kind: kind as AttentionKind })), (value) => attentionKindWord(value as AttentionKind, locale))}</label>
      </div>
      </details>
      {matches.length === 0 && <p>{overview.entries.length === 0 ? w.noItems : t.empty}</p>}
      <ul className="attention-items">
        {matches.map((entry) => <li key={`${entry.kind}:${entry.session ? targetKey(entry.session.target) : entry.machine.machine_id}`}>
          <strong>{attentionKindWord(entry.kind, locale)}</strong>
          <span>{entry.machine.name} · {entry.machine.platform}</span>
          {entry.session && <span>{entry.session.title || entry.session.target.session_id}</span>}
          {entry.session && onOpen && entry.machine.access === "readable" && entry.evidence === "current" && <button type="button" onClick={() => onOpen(entry.session!.target)}>{t.open}</button>}
          <details className="attention-item-evidence"><summary>{t.evidence}</summary>
            <small>{t.machineID}: {entry.machine.machine_id}{entry.session && ` · ${t.sessionID}: ${entry.session.target.session_id}`}</small>
            <small>{t.source}: ss/{entry.machine.machine_id} · {t.observed}: {entry.machine.observedAt === null ? w.unknown : new Date(entry.machine.observedAt).toLocaleString(locale)} · {t.generation}: {entry.machine.snapshotGeneration ?? w.unknown} · {entry.evidence === "retained" ? w.stale : entry.evidence === "unknown" ? w.unknownFresh : freshnessWord(entry.machine, locale)}</small>
            <small>{t.gap}: {machineGap(entry.machine, locale)} · {t.next}: {nextStep(entry.machine, entry.session, locale)}</small>
          </details>
        </li>)}
      </ul>
      <div className="attention-report-controls">
        <button type="button" aria-expanded={showReport} onClick={() => setShowReport((shown) => !shown)}>{showReport ? t.hide : t.report}</button>
      </div>
      {showReport && <div className="attention-report">
        <h3>{w.report}</h3>
        <button type="button" disabled={copying} onClick={async () => {
          setCopying(true)
          setFeedback("")
          try { await navigator.clipboard.writeText(report); setFeedback(t.copied) }
          catch { setFeedback(t.copyFailed) }
          finally { setCopying(false) }
        }}>{copying ? t.copying : t.copy}</button>
        <p role="status">{feedback}</p>
        <textarea readOnly aria-label={w.report} value={report} />
      </div>}
    </>}
  </section>
}
