import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react"
import type { ProviderQuotaRow, ProviderQuotaSnapshot, ProviderQuotaWindow } from "@clawdline/contract"
import { client } from "../client.js"
import { nextWord } from "../next-strings.js"
import { usePoll } from "../useFleet.js"
import { useMachineVersion } from "../machine/NeedsUpdate.js"
import { onMachineChanged } from "../cloud/machine-switch.js"
import * as L from "../legacy/bridge.js"
import { quotaCountdown, quotaStale, visibleQuotaRows, tightestWindow, compactQuotaRows } from "./model.js"
import { quotaVisible, subscribeQuotaVisible } from "./visibility.js"
import "./quota.css"

function Icon({ id }: { id: string }) {
  if (id === "claude" || id === "codex") return <span className="quota-logo" aria-hidden="true"
    dangerouslySetInnerHTML={{ __html: L.assistantLogoHTML(id) }} />
  return <svg className="quota-logo" viewBox="0 0 24 24" fill="none" aria-hidden="true">
    {id === "gemini" ? <path fill="currentColor" d="M12 1C10.5 8.5 8.5 10.5 1 12c7.5 1.5 9.5 3.5 11 11 1.5-7.5 3.5-9.5 11-11C15.5 10.5 13.5 8.5 12 1Z" /> : <>
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="1.5" />
      <path d="m6 18 12-12M8 6l10 12" stroke="currentColor" strokeWidth="2" /></>}
  </svg>
}
function WindowValue({ window: w, now, detail = false }: { window: ProviderQuotaWindow; now: number; detail?: boolean }) {
  const reset = quotaCountdown(w.resets_at, now)
  return <span className="quota-window" data-urgent={w.used_percent >= 80 || undefined}>
    <span className="quota-window-name">{w.name === "monthly" ? nextWord("quotaMonthly") : w.name}</span>
    <progress aria-label={nextWord("quotaUsedWindow", { window: w.name })} max={100} value={w.used_percent} />
    <span>{nextWord("quotaUsed", { percent: Math.round(w.used_percent) })}</span>
    {reset !== null && <span className="quota-reset">{reset === "" ? nextWord("quotaResetPending") : detail
      ? nextWord("quotaResetsIn", { time: reset }) : reset}</span>}
  </span>
}
function problemWord(problem: string | undefined): string {
  switch (problem) {
    case "reauth_required": return nextWord("quotaReauth")
    case "not_signed_in": return nextWord("quotaNotSignedIn")
    case "rate_limited": return nextWord("quotaRateLimited")
    case "too_old": return nextWord("quotaStale")
    case undefined: case "": return ""
    default: return nextWord("quotaUnavailable")
  }
}
const mobileQuery = "(max-width: 899px)"
function subscribeMobile(fn: () => void) { const query = window.matchMedia(mobileQuery); query.addEventListener("change", fn); return () => query.removeEventListener("change", fn) }
const isMobile = () => window.matchMedia(mobileQuery).matches

export function QuotaBar({ machineName, offline = false, mobileListShown = false }: { machineName?: string; offline?: boolean; mobileListShown?: boolean }) {
  const preference = useSyncExternalStore(subscribeQuotaVisible, quotaVisible)
  const mobile = useSyncExternalStore(subscribeMobile, isMobile)
  const version = useMachineVersion()
  const [revision, setRevision] = useState(0)
  useEffect(() => onMachineChanged(() => setRevision((n) => n + 1)), [])
  if (!(mobile ? preference.mobile : preference.desktop) || (mobile && !mobileListShown) || (version.apiLevel !== undefined && version.apiLevel < 28)) return null
  return <QuotaReading key={revision} machineName={machineName} offline={offline} />
}
function QuotaReading({ machineName, offline }: { machineName?: string; offline: boolean }) {
  const offlineRef = useRef(offline); offlineRef.current = offline
  const read = useMemo(() => async () => {
    if (offlineRef.current) throw new Error("offline")
    return client.providerQuotas()
  }, [])
  const polling = usePoll<ProviderQuotaSnapshot>(read, 30_000)
  const rows = visibleQuotaRows(polling.data?.providers ?? [])
  const mobile = useSyncExternalStore(subscribeMobile, isMobile)
  const shown = mobile ? compactQuotaRows(rows, 2) : rows
  const more = rows.length - shown.length
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  useEffect(() => { const timer = window.setInterval(() => setNow(Math.floor(Date.now() / 1000)), 60_000); return () => clearInterval(timer) }, [])
  const footer = useRef<HTMLElement>(null)
  const dialog = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLButtonElement | null>(null)
  const [expanded, setExpanded] = useState(false)
  useLayoutEffect(() => {
    if (!rows.length || !footer.current) return
    const el = footer.current
    const measure = () => document.documentElement.style.setProperty("--quota-footer-height", `${el.getBoundingClientRect().height}px`)
    measure(); const observer = new ResizeObserver(measure); observer.observe(el)
    return () => { observer.disconnect(); document.documentElement.style.removeProperty("--quota-footer-height") }
  }, [rows.length > 0])
  useEffect(() => {
    if (expanded && dialog.current && !dialog.current.open) dialog.current.showModal()
    if (!expanded && dialog.current?.open) dialog.current.close()
  }, [expanded])
  if (!rows.length) return null
  const stale = (row: ProviderQuotaRow) => offline || !!polling.error || quotaStale(row, now)
  const open = (button: HTMLButtonElement) => { opener.current = button; setExpanded(true) }
  return <>
    <footer ref={footer} className="provider-quota-bar" aria-label={nextWord("quotaTitle")}>
      {machineName && <span className="quota-machine" title={machineName}>{machineName}</span>}
      <div className="quota-providers">
        {shown.map((row) => <button key={row.id} className="quota-provider" type="button" data-provider={row.id}
          data-stale={stale(row) || undefined} aria-label={`${row.label} · ${nextWord("quotaUsed", { percent: Math.round(tightestWindow(row).used_percent) })}`} aria-haspopup="dialog" aria-controls="provider-quota-detail"
          onClick={(e) => open(e.currentTarget)} title={`${row.label} · ${nextWord("quotaDetails")}`}>
          <Icon id={row.id} /><span className="quota-provider-name">{row.label}</span>
          <span className="quota-full-windows">{(row.id === "claude" || row.id === "codex" ? row.windows : [tightestWindow(row)]).map((w, i) => <Fragment key={i}>
            {i > 0 && <span className="quota-window-separator" aria-hidden="true">·</span>}<WindowValue window={w} now={now} />
          </Fragment>)}</span>
          <span className="quota-compact-window"><WindowValue window={tightestWindow(row)} now={now} /></span>
          {stale(row) && <span className="quota-stale">{offline ? nextWord("quotaOffline") : nextWord("quotaStale")}</span>}
        </button>)}
        {more > 0 && <button type="button" className="quota-more" aria-haspopup="dialog" aria-controls="provider-quota-detail"
          aria-label={nextWord("quotaMore", { count: more })} onClick={(e) => open(e.currentTarget)}>+{more}</button>}
      </div>
      <button className="quota-refresh" type="button" title={nextWord("quotaRefresh")} aria-label={nextWord("quotaRefresh")}
        disabled={polling.pending || offline} onClick={polling.poke}>↻</button>
    </footer>
    <dialog ref={dialog} id="provider-quota-detail" className="quota-dialog" aria-labelledby="quota-dialog-title"
      onCancel={() => setExpanded(false)} onClose={() => { setExpanded(false); opener.current?.focus() }}>
      <div className="quota-dialog-heading"><h2 id="quota-dialog-title">{nextWord("quotaTitle")}</h2>
        <button type="button" onClick={() => setExpanded(false)} aria-label={nextWord("quotaClose")}>×</button></div>
      <p className="quota-caption">{machineName || nextWord("quotaThisMachine")} · {nextWord("quotaAccountScope")}</p>
      {rows.map((row) => <section key={row.id} className="quota-detail-row">
        <h3><Icon id={row.id} />{row.label}{stale(row) && <span className="quota-stale">{nextWord("quotaStale")}</span>}</h3>
        {row.windows.map((w, i) => <WindowValue key={i} window={w} now={now} detail />)}
        {row.observed_at !== undefined && <p className="quota-caption">{nextWord("quotaUpdated", { time: new Date(row.observed_at * 1000).toLocaleString() })}</p>}
        <p className="quota-caption">{nextWord(row.source === "claude_statusline" ? "quotaSourceClaude" : row.source === "codex_rollout" ? "quotaSourceCodex" : "quotaSourceAPI")}</p>
        {(offline || polling.error || row.problem) && <p className="quota-problem">{offline ? nextWord("quotaOffline") : polling.error ? nextWord("quotaUnavailable") : problemWord(row.problem)}
          {row.problem === "reauth_required" && <span> {nextWord("quotaRunCLI", { provider: row.label })}</span>}</p>}
      </section>)}
      <p className="quota-caption">{nextWord("quotaRefreshHint")}</p>
      <button type="button" className="chip" disabled={polling.pending || offline} onClick={polling.poke}>{nextWord("quotaRefresh")}</button>
    </dialog>
  </>
}
