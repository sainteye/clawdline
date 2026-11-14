import type { SessionRow } from "@clawdline/contract"
import { type FormEvent, useCallback, useEffect, useRef, useState } from "react"
import { followsRelay } from "../client.js"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { browserCall as call } from "./browser-call.js"
import { openBrowserSetup } from "./BrowserSetup.js"
import { BrowserViewport } from "./BrowserViewport.js"
import { BrowserComposer, type BrowserKeyboardHandle, type PageField } from "./BrowserComposer.js"
import type { InputReceipt } from "./browser-keyboard.js"
import { webAddress } from "./browser-address.js"
import "./browser-panel.css"

/**
 * The Session's Chrome tab, in the transcript's space (docs/browser.md).
 *
 * **It shows screenshots, and says so.** Chrome stops streaming a tab that is
 * covered, minimized or behind another tab, which is how the agent's window
 * usually is while the person watches from elsewhere
 * (docs/experiments/chrome-extension-probe-2026-11-12.md), so the panel asks
 * for a screenshot about once a second while it is open and the page visible.
 * Nothing is asked for while it is closed.
 *
 * **Input needs a takeover.** Clicking the picture, typing and the keys work
 * only after Take over, which the daemon records and which stops the agent's
 * commands to that tab until Hand back. What the person types is not recorded.
 *
 * Through Clawdline Cloud the same four routes are carried as the
 * chrome-tab-* words (internal/domain/cloudvocab/vocabulary.json), end to end
 * encrypted like every other word, so the phone sees and acts on the tab too.
 */

type TabInfo = { browser_mode?: string; tab: number; conversation: string; person: boolean; current: boolean }
type Status = { mode?: string; enabled: boolean; person_open?: string; status: { connected: boolean; stopped: boolean; state_error?: boolean; tabs: TabInfo[]; muted?: string[] } }
type Activity = { at: number; op: string; site?: string; url?: string; target?: string; text_length?: number; password?: boolean; ok: boolean; thumb: string }

const OP_WORD: Record<string, Parameters<typeof nextWord>[0]> = {
  open: "browserOpOpen", read: "browserOpRead", click: "browserOpClick", type: "browserOpType", key: "browserOpKey",
  scroll: "browserOpScroll", screenshot: "browserOpScreenshot", takeover: "browserOpTakeover", handback: "browserOpHandback", release: "browserOpRelease",
  viewport: "browserOpViewport",
  close: "browserOpClose",
}
type Shot = { data: string; width: number; height: number; url: string; title: string; layout?: "mobile" | "desktop"; editables?: { x: number; y: number; width: number; height: number; kind: string }[] }

const SHOT_MS = 1000
const STATUS_MS = 3000

/** A refusal in the console's own words, with its code kept (refusals/scan.ts). */
function said(error: unknown, lead: "browserPanelReadFailed" | "browserPanelActFailed"): string {
  return nextWord(lead, { reason: L.failureSentence(error, { fallback: (error as Error)?.message || "" }) })
}

/** An address as a browser's bar shows it: the host first, the rest dimmed. */
function address(raw: string | undefined): { host: string; rest: string; secure: boolean } {
  if (!raw) return { host: "", rest: "", secure: false }
  try {
    const u = new URL(raw)
    const rest = (u.pathname === "/" ? "" : u.pathname) + u.search
    return { host: u.host, rest: decodeURIComponent(rest), secure: u.protocol === "https:" }
  } catch {
    return { host: raw, rest: "", secure: false }
  }
}

function assistantName(row: SessionRow | null): string {
  switch (row?.assistant) {
    case "codex": return "Codex"
    case "claude": return "Claude"
    case "antigravity": return "Gemini"
    case "grok": return "Grok"
  }
  return "Agent"
}

export function BrowserPanel({ row, open, onClose }: { row: SessionRow | null; open: boolean; onClose: (restore: boolean) => void }) {
  const relay = followsRelay()
  const conversation = row?.sessionId ?? ""
  const [status, setStatus] = useState<Status | null>(null)
  const [statusError, setStatusError] = useState<unknown>(null)
  const [shotRecord, setShotRecord] = useState<{ value: Shot; tab: number; mode?: string; scope: string } | null>(null)
  const [shotError, setShotError] = useState<unknown>(null)
  const [actError, setActError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [controlBusy, setControlBusy] = useState(false)
  const [toolsOpen, setToolsOpen] = useState(false)
  const [reminded, setReminded] = useState(false)
  const [activity, setActivity] = useState<Activity[]>([])
  const [statusTick, setStatusTick] = useState(0)
  const closeRef = useRef<HTMLButtonElement>(null)
  const nativeKeyboard = useRef<BrowserKeyboardHandle>(null)
  const controlRevision = useRef(0)
  const controlPending = useRef(false)

  const mine = status?.status.tabs.filter((t) => t.conversation === conversation && (!t.browser_mode || t.browser_mode === status?.mode)) ?? []
  const tab = mine.find((t) => t.current) ?? mine[mine.length - 1] ?? null
  const tabID = tab?.tab ?? 0
  const browserMode = status?.mode
  const scopeKey = (row?.id ?? "") + ":" + conversation + ":" + (browserMode ?? "legacy") + ":" + tabID
  const scope = useRef(scopeKey)
  scope.current = scopeKey
  const shot = shotRecord?.scope === scopeKey && shotRecord.tab === tabID && shotRecord.mode === browserMode ? shotRecord.value : null
  const otherDebugger = (shotError as { code?: string } | null)?.code === "browser_other_debugger"
  const person = !!tab?.person && status?.enabled !== false && !otherDebugger
  const name = assistantName(row)

  useEffect(() => {
    if (!open) return
    let alive = true
    const read = () => {
      const revision = controlRevision.current
      return call<Status>("GET", "/v1/browser/status?conversation=" + encodeURIComponent(conversation)).then(
        (s) => { if (alive && revision === controlRevision.current && !controlPending.current) { setStatus(s); setStatusError(null) } },
        (e: unknown) => { if (alive && revision === controlRevision.current && !controlPending.current) setStatusError(e) },
      )
    }
    read()
    const timer = window.setInterval(() => { if (!document.hidden) read() }, STATUS_MS)
    return () => { alive = false; window.clearInterval(timer) }
  }, [open, relay, conversation, statusTick])

  useEffect(() => {
    setReminded(false)
    setShotRecord(null)
    setShotError(null)
    if (!open || !tabID) return
    let alive = true
    let inFlight = false
    const read = () => {
      if (inFlight || document.hidden) return
      inFlight = true
      // Through Clawdline Cloud the picture travels encrypted through the
      // relay, so it is asked for at a lower quality there.
      call<{ value: Shot }>("GET", "/v1/browser/screenshot?tab=" + tabID + "&quality=" + (relay ? "45" : "55") + (browserMode ? "&browser_mode=" + encodeURIComponent(browserMode) : "")).then(
        (r) => { if (alive) { setShotRecord({ value: r.value, tab: tabID, mode: browserMode, scope: scopeKey }); setShotError(null) } },
        (e: unknown) => { if (alive) setShotError(e) },
      ).finally(() => { inFlight = false })
    }
    read()
    const timer = window.setInterval(read, SHOT_MS)
    return () => { alive = false; window.clearInterval(timer) }
  }, [open, relay, tabID, browserMode, scopeKey])

  // What the agent did, with the picture of the page after each action.
  useEffect(() => {
    setActivity([])
    if (!open || !conversation || !tabID) return
    let alive = true
    const read = () => {
      if (document.hidden) return
      call<{ rows: Activity[] }>("GET", "/v1/browser/activity?conversation=" + encodeURIComponent(conversation)).then(
        (r) => { if (alive) setActivity(r.rows ?? []) },
        () => {},
      )
    }
    read()
    const timer = window.setInterval(read, STATUS_MS)
    return () => { alive = false; window.clearInterval(timer) }
  }, [open, conversation, tabID])

  useEffect(() => { if (open) closeRef.current?.focus({ preventScroll: true }) }, [open])
  useEffect(() => {
    if (controlPending.current) { controlRevision.current++; controlPending.current = false }
    setToolsOpen(false); setBusy(false); setControlBusy(false); setActError(null)
    nativeKeyboard.current?.close()
  }, [open, scopeKey])

  const act = useCallback(async (op: string, args: Record<string, unknown>): Promise<InputReceipt> => {
    if (!tabID || scope.current !== scopeKey) return { ok: false }
    setBusy(true)
    setActError(null)
    try {
      const result = await call<{ value?: { editable?: string | null } }>("POST", "/v1/browser/person", { op, tab: tabID, args, ...(browserMode ? {browser_mode:browserMode} : {}) })
      return scope.current === scopeKey ? { ok: true, editable: result.value?.editable } : { ok: false }
    } catch (e) {
      if (scope.current === scopeKey && op !== "type") setActError(e)
      return { ok: false }
    } finally {
      if (scope.current === scopeKey) setBusy(false)
    }
  }, [tabID, browserMode, scopeKey])

  // The person's own read of the focused field, for checking what was sent.
  // It neither marks the panel busy nor reports an error: an unreadable field
  // only means nothing is compared.
  const readField = useCallback(async (): Promise<PageField | null> => {
    if (!tabID || scope.current !== scopeKey) return null
    try {
      const result = await call<{ value?: PageField }>("POST", "/v1/browser/person", { op: "field", tab: tabID, args: {}, ...(browserMode ? {browser_mode:browserMode} : {}) })
      return scope.current === scopeKey ? result.value ?? null : null
    } catch { return null }
  }, [tabID, browserMode, scopeKey])

  const muted = !!status?.status.muted?.includes(conversation)
  const mute = async (on: boolean) => {
    setBusy(true)
    setActError(null)
    try {
      await call("POST", "/v1/browser/mute", { conversation, muted: on })
      setStatus((s) => s && { ...s, status: { ...s.status, muted: on ? [...(s.status.muted ?? []), conversation] : (s.status.muted ?? []).filter((c) => c !== conversation) } })
    } catch (e) {
      setActError(e)
    } finally {
      setBusy(false)
    }
  }

  const control = async (on: boolean): Promise<boolean> => {
    if (!tabID || scope.current !== scopeKey) return false
    if (!on) nativeKeyboard.current?.close()
    controlRevision.current++; controlPending.current = true
    setControlBusy(true)
    setActError(null)
    try {
      await call("POST", "/v1/browser/control", { tab: tabID, person: on, ...(browserMode ? {browser_mode:browserMode} : {}) })
      if (scope.current !== scopeKey) return false
      setStatus((s) => s && { ...s, status: { ...s.status, tabs: s.status.tabs.map((t) => t.tab === tabID ? { ...t, person: on } : t) } })
      return true
    } catch (e) {
      if (scope.current === scopeKey) setActError(e)
      return false
    } finally {
      if (scope.current === scopeKey) { controlRevision.current++; controlPending.current = false; setControlBusy(false) }
    }
  }

  const ensureControl = () => person ? Promise.resolve(true) : control(true)
  const tap = async (x: number, y: number): Promise<InputReceipt> => await ensureControl() ? act("click", { x, y }) : { ok: false }
  const changeLayout = async (mode: string) => {
    nativeKeyboard.current?.close()
    if (await ensureControl()) await act("viewport", { mode })
  }

  const activityList = () => (
        <details className="browser-activity">
          <summary>{nextWord("browserPanelActivity")}</summary>
          {activity.length === 0 ? <p className="browser-activity-empty">{nextWord("browserPanelActivityEmpty")}</p> : (
            <ol>
              {activity.map((row, i) => (
                <li key={row.at + ":" + i}>
                  {row.thumb && <img src={"data:image/jpeg;base64," + row.thumb} alt="" />}
                  <span className="browser-activity-words">
                    <b>{OP_WORD[row.op] ? nextWord(OP_WORD[row.op]) : row.op}</b>
                    <span>{row.op === "type" ? (row.password ? "••••" : "") : row.op === "viewport" && (row.target === "mobile" || row.target === "desktop") ? nextWord(row.target === "mobile" ? "browserPanelMobile" : "browserPanelDesktop") : row.target || address(row.url).host}</span>
                    <time dateTime={new Date(row.at * 1000).toISOString()}>{new Date(row.at * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</time>
                  </span>
                </li>
              ))}
            </ol>
          )}
        </details>
  )

  const body = () => {
    if (statusError) return <div className="screen-note err" role="alert">{said(statusError, "browserPanelReadFailed")}</div>
    if (!status) return <div className="screen-note" role="status">…</div>
    // A Session without a tab can be given one by the person whenever some
    // Chrome is connected, even when the agent's own selection is native
    // tools or its switch is off (the daemon's person_open).
    if (!tab && status.person_open && !status.status.stopped) {
      return <BrowserOpenCard conversation={conversation} assistant={name} onOpened={() => setStatusTick((n) => n + 1)} />
    }
    // Not connected, or browser use switched off: one card and one main
    // action. Setup happens only on the computer, so through Cloud the main
    // action is the reminder and the setup steps are secondary.
    if (!status.status.connected || !status.enabled) {
      const off = status.status.connected
      const remind = async () => {
        setBusy(true); setActError(null)
        try { await call("POST", "/v1/browser/reminder", { conversation }); setReminded(true) }
        catch (e) { setActError(e) } finally { setBusy(false) }
      }
      return (
        <div className="browser-card-wrap">
          <div className="browser-card" role="status">
            <span className="browser-card-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" /><path d="M9.5 8.5l5 5M14.5 8.5l-5 5" /></svg>
            </span>
            <h3>{nextWord(off ? "browserCardDisabledTitle" : "browserCardNotConnectedTitle")}</h3>
            <p>{nextWord(off ? "browserCardDisabledBody" : relay ? "browserCardNotConnectedBody" : "browserCardNotConnectedLocal")}</p>
            {relay ? <>
              <button type="button" className="browser-card-primary" disabled={busy || reminded} onClick={() => void remind()}>{nextWord(reminded ? "browserReminded" : "browserRemind")}</button>
              <button type="button" className="browser-card-secondary" onClick={() => openBrowserSetup()}>{nextWord("browserCardSetupSteps")}</button>
            </> : <button type="button" className="browser-card-primary" onClick={() => openBrowserSetup()}>{nextWord("browserSetupOpen")}</button>}
            {actError ? <p className="browser-card-error" role="alert">{said(actError, "browserPanelActFailed")}</p> : null}
          </div>
          {relay && !off && <p className="browser-card-hint">{nextWord("browserCardNotConnectedHint")}</p>}
        </div>
      )
    }
    if (!tab) return <BrowserOpenCard conversation={conversation} assistant={name} onOpened={() => setStatusTick((n) => n + 1)} />
    return (
      <div className="browser-stage">
        {status.status.state_error && <div className="screen-note err" role="alert">{nextWord("browserStateUnsaved")}</div>}
        {status.status.stopped && <div className="screen-note" role="status">{nextWord("browserPanelStopped")}</div>}
        {shotError ? <div className="screen-note err" role="alert">{(shotError as { code?: string })?.code === "browser_other_debugger" ? nextWord("browserPanelOtherDebugger") : said(shotError, "browserPanelReadFailed")}</div> : null}
        {shot && !otherDebugger && (
          <BrowserViewport key={scopeKey + ":" + (shot.layout || "desktop")} shot={shot} canClick={!busy && !controlBusy} person={person} onTap={tap}
            onKeyboard={(kind, ready) => nativeKeyboard.current?.open(kind, ready)} onFocusKeyboard={() => nativeKeyboard.current?.open()} onCloseKeyboard={() => nativeKeyboard.current?.close()}
            layoutControl={<label className="browser-mode"><span className="sr-only">{nextWord("browserPanelLayout")}</span>
              <select aria-label={nextWord("browserPanelLayout")} value={shot.layout || "desktop"} disabled={busy || controlBusy} onChange={e => void changeLayout(e.target.value)}>
                <option value="mobile">{nextWord("browserPanelMobile")}</option><option value="desktop">{nextWord("browserPanelDesktop")}</option>
              </select></label>} />
        )}

      </div>
    )
  }

  const ready = !!tab && !!status?.status.connected && !!status.enabled
  return (
    <section className="panel-view" id="browser-panel" aria-labelledby="browser-title" hidden={!open}>
      {/* One address bar instead of a title row: the page's address, how it is
          shown (a picture about once a second), and the way out. */}
      <div className="browser-bar">
        <h2 id="browser-title" className="sr-only">{nextWord("browserPanelTitle")}</h2>
        <div className="browser-url" title={shot?.url || ""}>
          {address(shot?.url).secure
            ? <svg className="browser-lock" viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="7" width="9" height="6.5" rx="1.5" /><path d="M5.5 7V5.2a2.5 2.5 0 0 1 5 0V7" /></svg>
            : <svg className="browser-lock" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.5" /><path d="M2.5 8h11M8 2.5c1.7 1.6 2.5 3.4 2.5 5.5S9.7 11.9 8 13.5M8 2.5C6.3 4.1 5.5 5.9 5.5 8s.8 3.9 2.5 5.5" /></svg>}
          <span className="browser-host">{address(shot?.url).host || nextWord("browserPanelTitle")}</span>
          <span className="browser-path">{address(shot?.url).rest}</span>
        </div>
        {ready && <span className="browser-live" title={nextWord("browserPanelSampled")}><i aria-hidden="true" />{nextWord("browserPanelSampled")}</span>}
        <button className="browser-close" type="button" ref={closeRef} aria-label={L.strings.webClose} title={L.strings.webClose} onClick={() => onClose(true)}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
        </button>
      </div>
      <div className="scroller panel-view-body" aria-live="polite">{open ? body() : null}</div>
      {open && !!tabID && (
        <div className="browser-controls">
          <BrowserComposer key={scopeKey} keyboardRef={nativeKeyboard} person={person}
            available={ready && !otherDebugger && !controlBusy && !busy}
            onPrepare={ensureControl} onSend={(text, replace) => act("type", replace ? { text, replace: true } : { text })}
            onDelete={() => void act("key", { key: "Backspace" })} onReadField={readField} />
          <div className="browser-who" data-person={person ? "true" : undefined}>
            <span aria-live="polite">{person ? nextWord("browserPanelPersonActs", { assistant: name }) : nextWord("browserPanelTapToTake")}</span>
            <button type="button" className={person ? "browser-take" : "browser-take browser-primary"} disabled={controlBusy || otherDebugger} onClick={() => void control(!person)}>
              {person ? nextWord("browserPanelHandBackShort") : nextWord("browserPanelTakeOverShort")}
            </button>
            <button type="button" className="browser-tools-toggle" aria-expanded={toolsOpen} aria-controls="browser-tools" onClick={() => setToolsOpen(v => !v)}>{nextWord("browserPanelTools")}</button>
          </div>
          <div id="browser-tools" className="browser-tools" hidden={!toolsOpen}>
            <div className="browser-tools-head"><b>{nextWord("browserPanelTools")}</b><button type="button" onClick={() => setToolsOpen(false)} aria-label={L.strings.webClose}>×</button></div>
            <div className="browser-keys">
              <button type="button" className="browser-keyboard-open" disabled={busy || controlBusy || otherDebugger} onClick={() => { nativeKeyboard.current?.open(); setToolsOpen(false) }}>{nextWord("browserPanelKeyboard")}</button>
              <button type="button" disabled={!person || busy} onClick={() => void act("key", { key: "Tab" })}>Tab</button>
              <button type="button" disabled={!person || busy} onClick={() => void act("key", { key: "Enter" })}>Enter</button>
              <button type="button" disabled={!person || busy} onClick={() => void act("scroll", { direction: "up" })}>{nextWord("browserPanelScrollUp")}</button>
              <button type="button" disabled={!person || busy} onClick={() => void act("scroll", { direction: "down" })}>{nextWord("browserPanelScrollDown")}</button>
            </div>
            <button type="button" className="browser-notify" aria-pressed={!muted} disabled={busy} onClick={() => void mute(!muted)}>{nextWord(muted ? "browserPanelNotifyOff" : "browserPanelNotifyOn")}</button>
            {activityList()}
          </div>
          {actError ? <div className="screen-note err" role="alert">{said(actError, "browserPanelActFailed")}</div> : null}
        </div>
      )}
    </section>
  )
}

/**
 * A Session without a tab: the person may open one themselves. The daemon
 * gives the new tab to this Session already taken over by the person, so the
 * agent waits until they hand it back (POST /v1/browser/open; through Cloud,
 * chrome-tab-open).
 */
function BrowserOpenCard({ conversation, assistant, onOpened }: { conversation: string; assistant: string; onOpened: () => void }) {
  const [text, setText] = useState("")
  const [opening, setOpening] = useState(false)
  const [error, setError] = useState("")
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (opening) return
    const url = webAddress(text)
    if (!url) { setError(nextWord("browserOpenBadAddress")); return }
    setOpening(true); setError("")
    try {
      await call("POST", "/v1/browser/open", { conversation, url })
      onOpened()
    } catch (e) {
      setError(nextWord("browserOpenFailed", { reason: L.failureSentence(e, { fallback: (e as Error)?.message || "" }) }))
    } finally {
      setOpening(false)
    }
  }
  return (
    <div className="browser-card-wrap">
      <form className="browser-card" onSubmit={(e) => void submit(e)}>
        <h3>{nextWord("browserCardNoTabTitle")}</h3>
        <p>{nextWord("browserCardNoTabBody", { assistant })}</p>
        <label className="browser-card-label" htmlFor="browser-open-url">{nextWord("browserOpenLabel")}</label>
        <input id="browser-open-url" className="browser-card-input" type="text" inputMode="url" enterKeyHint="go"
          autoCapitalize="off" autoCorrect="off" autoComplete="url" spellCheck={false}
          placeholder={nextWord("browserOpenPlaceholder")} value={text} disabled={opening}
          aria-invalid={error ? true : undefined} aria-describedby={error ? "browser-open-error" : undefined}
          onChange={(e) => { setText(e.target.value); if (error) setError("") }} />
        <button type="submit" className="browser-card-primary" disabled={opening || !text.trim()}>{nextWord(opening ? "browserOpenOpening" : "browserOpenSubmit")}</button>
        {error ? <p id="browser-open-error" className="browser-card-error" role="alert">{error}</p> : null}
      </form>
      <p className="browser-card-hint">{nextWord("browserOpenNote", { assistant })}</p>
    </div>
  )
}

/**
 * The header's sign that this Session is using a Chrome tab. It reads the
 * bridge's status every 15 seconds while the page is visible and the panel is
 * closed, and shows nothing when the Session has no tab.
 */
export function useBrowserTab(row: SessionRow | null, hidden: boolean): boolean {
  const conversation = row?.sessionId ?? ""
  const [has, setHas] = useState(false)
  useEffect(() => {
    setHas(false)
    if (!conversation || hidden) return
    let alive = true
    const read = () => {
      if (document.hidden) return
      call<Status>("GET", "/v1/browser/status?conversation=" + encodeURIComponent(conversation)).then(
        (s) => { if (alive) setHas(!!s.status?.connected && s.status.tabs.some((t) => t.conversation === conversation)) },
        () => { if (alive) setHas(false) },
      )
    }
    read()
    const timer = window.setInterval(read, 15_000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [conversation, hidden])
  return has && !hidden
}

export function BrowserChip({ onOpen }: { onOpen: () => void }) {
  return (
    <button type="button" className="browser-chip" title={nextWord("browserChipTip")} aria-label={nextWord("browserChipTip")} onClick={onOpen}>
      <i aria-hidden="true" /><span className="browser-chip-name">{nextWord("browserChip")}</span>
      <span className="header-menu-label">{nextWord("browserPanelMenu")}</span>
    </button>
  )
}
