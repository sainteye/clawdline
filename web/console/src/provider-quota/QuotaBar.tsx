import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react"
import type { ProviderQuotaRow, ProviderQuotaSnapshot, ProviderQuotaWindow } from "@clawdline/contract"
import { client } from "../client.js"
import { nextWord } from "../next-strings.js"
import { usePoll } from "../useFleet.js"
import { useMachineVersion } from "../machine/NeedsUpdate.js"
import { onMachineChanged } from "../cloud/machine-switch.js"
import * as L from "../legacy/bridge.js"
import { quotaCountdown, quotaStale, visibleQuotaRows, compactWindow, quotaCreditBalance, quotaRefreshResult } from "./model.js"
import { quotaVisible, subscribeQuotaVisible } from "./visibility.js"
import { toast } from "../overlays/toast.js"
import { requestStatusRefresh } from "../status-refresh.js"
import { lastQuotaReading, rememberQuotaReading } from "./last-reading.js"
import "./quota.css"

function Icon({ id }: { id: string }) {
  const html = L.assistantLogoHTML(id)
  if (html) return <span className="quota-logo" aria-hidden="true"
    dangerouslySetInnerHTML={{ __html: html }} />
  return <svg className="quota-logo" viewBox="0 0 24 24" fill="none" aria-hidden="true">
    <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="1.5" />
    <path d="m6 18 12-12M8 6l10 12" stroke="currentColor" strokeWidth="2" />
  </svg>
}
function WindowValue({ window: w, now, detail = false, mobile = false }: { window: ProviderQuotaWindow; now: number; detail?: boolean; mobile?: boolean }) {
  const reset = quotaCountdown(w.resets_at, now)
  return <span className="quota-window" data-urgent={w.used_percent >= 80 || undefined}>
    <span className="quota-window-name">{w.name === "monthly" ? nextWord("quotaMonthly") : w.name}</span>
    <progress aria-label={nextWord("quotaUsedWindow", { window: w.name })} max={100} value={w.used_percent} />
    <span>{mobile ? `${Math.round(w.used_percent)}%` : nextWord("quotaUsed", { percent: Math.round(w.used_percent) })}</span>
    {reset !== null && <span className="quota-reset">{reset === "" ? nextWord("quotaResetPending") : detail
      ? nextWord("quotaResetsIn", { time: reset }) : reset}</span>}
  </span>
}
function CreditsNote({ row }: { row: ProviderQuotaRow }) {
  const balance = quotaCreditBalance(row)
  return <p className="quota-credits-note"><b>{nextWord("quotaCreditsInUseLong")}</b>
    {balance && <> · {nextWord("quotaCreditBalance", { balance })}</>}<br />
    {nextWord("quotaCreditsExplain", { provider: row.label })}</p>
}
function problemWord(problem: string | undefined, hasReading = true): string {
  switch (problem) {
    case "no_reading": return nextWord("quotaNoReading")
    case "reauth_required": return nextWord("quotaReauth")
    case "not_signed_in": return nextWord("quotaNotSignedIn")
    case "rate_limited": return nextWord("quotaRateLimited")
    case "too_old": return nextWord("quotaStale")
    case undefined: case "": return ""
    default: return nextWord(hasReading ? "quotaUnavailable" : "quotaReadUnavailable")
  }
}
const mobileQuery = "(max-width: 899px)"
function subscribeMobile(fn: () => void) { const query = window.matchMedia(mobileQuery); query.addEventListener("change", fn); return () => query.removeEventListener("change", fn) }
const isMobile = () => window.matchMedia(mobileQuery).matches

export function QuotaBar({ machineName, offline = false, mobileListShown = false, quotaScope, inert, accessory }: {
  machineName?: string
  offline?: boolean
  mobileListShown?: boolean
  quotaScope?: string | null
  inert?: boolean
  accessory?: ReactNode
}) {
  const preference = useSyncExternalStore(subscribeQuotaVisible, quotaVisible)
  const mobile = useSyncExternalStore(subscribeMobile, isMobile)
  const version = useMachineVersion()
  const [revision, setRevision] = useState(0)
  useEffect(() => onMachineChanged(() => setRevision((n) => n + 1)), [])
  const shown = (mobile ? preference.mobile && mobileListShown : preference.desktop) &&
    (version.apiLevel === undefined || version.apiLevel >= 28)
  const footer = useRef<HTMLElement>(null)
  useLayoutEffect(() => {
    const el = footer.current
    if (!shown || !el) return
    const measure = () => document.documentElement.style.setProperty("--quota-footer-height", `${el.getBoundingClientRect().height}px`)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => { observer.disconnect(); document.documentElement.style.removeProperty("--quota-footer-height") }
  }, [shown])

  if (!shown) return null
  return <footer ref={footer} className="provider-quota-bar" inert={inert}>
    {accessory}
    <QuotaReading key={JSON.stringify([quotaScope, revision])} scope={quotaScope ?? ""} machineName={machineName} offline={offline} mobile={mobile} apiLevel={version.apiLevel} />
  </footer>
}

/**
 * How long the footer waits on one reading before it says the reading is
 * unavailable. A machine that does not answer is otherwise waited on for the
 * relay's own minute, twice, and the footer said "reading" for all of it. The
 * read stays out past this, and what it answers is still shown (`HeldReads`).
 */
const QUOTA_READ_DEADLINE_MS = 20_000

type ManualCheck = {
  before: ProviderQuotaSnapshot | null
  supported: boolean
  started: boolean
  /** The poller's run of failures when the check was pressed. */
  failuresBefore: number
}

function QuotaReading({ scope, machineName, offline, mobile, apiLevel }: { scope: string; machineName?: string; offline: boolean; mobile: boolean; apiLevel?: number }) {
  const offlineRef = useRef(offline); offlineRef.current = offline
  const dialog = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLButtonElement | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [feedback, setFeedback] = useState("")
  const [refreshing, setRefreshing] = useState(false)
  const manual = useRef<ManualCheck | null>(null)
  useEffect(() => () => { manual.current = null }, [])
  const read = useMemo(() => async () => {
    const check = manual.current
    const explicit = check !== null && !check.started
    if (check) check.started = true
    const finish = (message: string, bad = false) => {
      if (!check || manual.current !== check) return
      manual.current = null
      setFeedback(message)
      setRefreshing(false)
      if (!dialog.current?.open) toast(message, bad)
    }
    try {
      if (offlineRef.current) throw new Error("offline")
      const snapshot = await client.providerQuotas(explicit && check.supported)
      if (check && !snapshot.updating) {
        const result = quotaRefreshResult(check.before, snapshot)
        if (!check.supported) finish(nextWord("quotaRefreshOlder"))
        else if (result === "updated") finish(nextWord("quotaRefreshUpdated"))
        else if (result === "partial") finish(nextWord("quotaRefreshPartial"), true)
        else if (result === "failed") finish(nextWord("quotaRefreshFailed"), true)
        else finish(nextWord("quotaRefreshUnchanged"))
      } else if (check && manual.current === check && !check.before) {
        check.before = snapshot
      }
      return snapshot
    } catch (error) {
      finish(nextWord(offlineRef.current ? "quotaOfflineNoReading" : "quotaReadUnavailable"), true)
      throw error
    }
  }, [])
  const polling = usePoll<ProviderQuotaSnapshot>(read, refreshing ? 2_000 : 30_000, { deadlineMs: QUOTA_READ_DEADLINE_MS })
  // A wait ended by its deadline never reaches `read`'s own catch, so the
  // manual check is released here. One pressed while an earlier read was
  // still out waited on that read and was never handed to `read` at all; it
  // is released by the failure that came after the press.
  useEffect(() => {
    const check = manual.current
    if (!polling.error || polling.reading || !check) return
    if (!check.started && polling.failures <= check.failuresBefore) return
    manual.current = null
    const message = nextWord("quotaReadUnavailable")
    setFeedback(message)
    setRefreshing(false)
    if (!dialog.current?.open) toast(message, true)
  }, [polling.error, polling.reading, polling.failures])
  // A remount starts its poller empty; until it reads, the footer shows the
  // last reading this browser kept for the machine, marked outdated.
  if (polling.data) rememberQuotaReading(scope, polling.data)
  const saved = polling.data ? null : lastQuotaReading(scope)
  const rows = visibleQuotaRows((polling.data ?? saved)?.providers ?? [])
  const shown = rows
  const more = rows.length - shown.length
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  useEffect(() => { const timer = window.setInterval(() => setNow(Math.floor(Date.now() / 1000)), 60_000); return () => clearInterval(timer) }, [])
  useEffect(() => {
    if (expanded && dialog.current && !dialog.current.open) dialog.current.showModal()
    if (!expanded && dialog.current?.open) dialog.current.close()
  }, [expanded])
  const refresh = () => {
    if (manual.current) return
    if (offline) {
      toast(nextWord("quotaOfflineNoReading"), true)
      return
    }
    manual.current = { before: polling.data ?? saved, supported: apiLevel === undefined || apiLevel >= 115, started: false, failuresBefore: polling.failures }
    setFeedback("")
    setRefreshing(true)
    setNow(Math.floor(Date.now() / 1000))
    polling.poke()
  }
  const stale = (row: ProviderQuotaRow) => offline || !!polling.error || !!saved || quotaStale(row, now)
  const open = (button: HTMLButtonElement) => { opener.current = button; setExpanded(true) }
  const readingState = offline ? "offline" : polling.error ? "failed" : polling.pending ? "loading" : rows.length ? "ready" : "empty"
  return <>
    {machineName && <span className="quota-machine" title={machineName}>{machineName}</span>}
    <div className="quota-providers" data-reading-state={readingState} role="group" aria-label={nextWord("quotaTitle")}>
      {!rows.length && <span className="quota-empty" role="status">{nextWord(offline ? "quotaOfflineNoReading" : polling.error ? "quotaReadUnavailable" : polling.pending ? "quotaLoading" : "quotaEmpty")}</span>}
      {shown.map((row) => {
        const compact = compactWindow(row)
        const used = compact
          ? (mobile ? `${Math.round(compact.used_percent)}%` : nextWord("quotaUsed", { percent: Math.round(compact.used_percent) }))
          : (mobile ? "" : nextWord("quotaUnknown"))
        const reading = row.credits_in_use ? `${used} · ${nextWord("quotaCreditsInUseLong")}` : used
        const aria = reading ? `${row.label} · ${reading}` : row.label
        return <button key={row.id} className="quota-provider" type="button" data-provider={row.id}
          data-stale={stale(row) || undefined} data-credits={row.credits_in_use || undefined} aria-label={aria} aria-haspopup="dialog" aria-controls="provider-quota-detail"
          onClick={(e) => open(e.currentTarget)} title={`${row.label} · ${nextWord("quotaDetails")}`}>
          <Icon id={row.id} /><span className="quota-provider-name">{row.label}</span>
          <span className="quota-compact-window">{compact ? <WindowValue window={compact} now={now} mobile={mobile} /> : <span className="quota-unknown">{used}</span>}</span>
          {!mobile && row.credits_in_use && <span className="quota-credits">{nextWord("quotaCredits")}</span>}
          {!mobile && stale(row) && <span className="quota-stale">{offline ? nextWord("quotaOffline") : nextWord("quotaStale")}</span>}
        </button>
      })}
      {more > 0 && <button type="button" className="quota-more" aria-haspopup="dialog" aria-controls="provider-quota-detail"
        aria-label={nextWord("quotaMore", { count: more })} onClick={(e) => open(e.currentTarget)}>+{more}</button>}
    </div>
    <button className="quota-refresh" type="button" title={nextWord("quotaRefresh")} aria-label={nextWord("quotaRefresh")}
      aria-busy={refreshing} onClick={() => {
        requestStatusRefresh()
        refresh()
      }}><span aria-hidden="true">↻</span></button>
    <dialog ref={dialog} id="provider-quota-detail" className="quota-dialog" aria-labelledby="quota-dialog-title"
      onKeyDown={(event) => { if (event.key === "Escape") event.stopPropagation() }}
      onCancel={() => setExpanded(false)} onClose={() => { setExpanded(false); opener.current?.focus() }}
      onClick={(event) => {
        // A click on the backdrop targets the dialog itself, outside its box.
        if (event.target !== event.currentTarget) return
        const box = event.currentTarget.getBoundingClientRect()
        if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) setExpanded(false)
      }}>
      <div className="quota-dialog-heading">
        <h2 id="quota-dialog-title">{nextWord("quotaTitle")}</h2>
        <div className="quota-dialog-actions">
          <button type="button" className="chip quota-refresh-now" aria-disabled={refreshing || offline}
            aria-busy={refreshing} onClick={() => { if (!offline) refresh() }}>{nextWord(refreshing ? "quotaRefreshing" : "quotaRefreshNow")}</button>
          <button type="button" className="quota-dialog-close" onClick={() => setExpanded(false)} aria-label={nextWord("quotaClose")} autoFocus>×</button>
        </div>
        <p className="quota-caption quota-refresh-feedback" role="status">{feedback}</p>
      </div>
      <p className="quota-caption">{machineName || nextWord("quotaThisMachine")} · {nextWord("quotaAccountScope")}</p>
      {rows.map((row) => <section key={row.id} className="quota-detail-row">
        <h3><Icon id={row.id} />{row.label}{stale(row) && <span className="quota-stale">{nextWord("quotaStale")}</span>}</h3>
        {row.windows.length ? row.windows.map((w, i) => <WindowValue key={i} window={w} now={now} detail />) : <p className="quota-caption">{nextWord("quotaUnknown")}</p>}
        {row.credits_in_use && <CreditsNote row={row} />}
        {row.observed_at !== undefined && <p className="quota-caption">{nextWord("quotaUpdated", { time: new Date(row.observed_at * 1000).toLocaleString() })}</p>}
        <p className="quota-caption">{nextWord(row.source === "claude_statusline" ? "quotaSourceClaude" : row.source === "codex_rollout" ? "quotaSourceCodex" : row.source === "provider_cli" ? "quotaSourceCLI" : "quotaSourceAPI")}</p>
        {(offline || polling.error || row.problem) && <p className="quota-problem">{offline ? nextWord("quotaOffline") : polling.error ? nextWord("quotaUnavailable") : problemWord(row.problem, row.windows.length > 0)}
          {row.problem === "reauth_required" && <span> {nextWord("quotaRunCLI", { provider: row.label })}</span>}</p>}
      </section>)}
      <p className="quota-caption">{nextWord("quotaRefreshHint")}</p>
    </dialog>
  </>
}
