import type { SessionRow } from "@clawdline/contract"
import { useCallback, useEffect, useRef, useState, type FormEvent, type MouseEvent } from "react"
import { followsRelay } from "../client.js"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
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

type TabInfo = { tab: number; conversation: string; person: boolean; current: boolean }
type Status = { enabled: boolean; status: { connected: boolean; stopped: boolean; tabs: TabInfo[]; muted?: string[] } }
type Activity = { at: number; op: string; site?: string; url?: string; target?: string; text_length?: number; password?: boolean; ok: boolean; thumb: string }

const OP_WORD: Record<string, Parameters<typeof nextWord>[0]> = {
  open: "browserOpOpen", read: "browserOpRead", click: "browserOpClick", type: "browserOpType", key: "browserOpKey",
  scroll: "browserOpScroll", screenshot: "browserOpScreenshot", takeover: "browserOpTakeover", handback: "browserOpHandback", release: "browserOpRelease",
}
type Shot = { data: string; width: number; height: number; url: string; title: string }

const SHOT_MS = 1000
const STATUS_MS = 3000

async function call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new Error(L.strings.webOffline)
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    // The daemon's refusal envelope, kept whole so the formatter can name it.
    const raw = data?.error
    const code = typeof raw === "string" ? raw : typeof raw?.code === "string" ? raw.code : "http_" + res.status
    const refusal = new Error(typeof data?.detail === "string" ? data.detail : typeof raw?.message === "string" ? raw.message : code) as Error & { code: string }
    refusal.name = "RefusalError"
    refusal.code = code
    throw refusal
  }
  return data as T
}

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
    return { host: u.host.replace(/^www\./, ""), rest: decodeURIComponent(rest), secure: u.protocol === "https:" }
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
  const [shot, setShot] = useState<Shot | null>(null)
  const [shotError, setShotError] = useState<unknown>(null)
  const [actError, setActError] = useState<unknown>(null)
  const [text, setText] = useState("")
  const [busy, setBusy] = useState(false)
  const [activity, setActivity] = useState<Activity[]>([])
  const closeRef = useRef<HTMLButtonElement>(null)

  const mine = status?.status.tabs.filter((t) => t.conversation === conversation) ?? []
  const tab = mine.find((t) => t.current) ?? mine[mine.length - 1] ?? null
  const tabID = tab?.tab ?? 0
  const person = !!tab?.person
  const name = assistantName(row)

  useEffect(() => {
    if (!open) return
    let alive = true
    const read = () => call<Status>("GET", "/v1/browser/status").then(
      (s) => { if (alive) { setStatus(s); setStatusError(null) } },
      (e: unknown) => { if (alive) setStatusError(e) },
    )
    read()
    const timer = window.setInterval(() => { if (!document.hidden) read() }, STATUS_MS)
    return () => { alive = false; window.clearInterval(timer) }
  }, [open, relay, conversation])

  useEffect(() => {
    setShot(null)
    setShotError(null)
    if (!open || !tabID) return
    let alive = true
    let inFlight = false
    const read = () => {
      if (inFlight || document.hidden) return
      inFlight = true
      // Through Clawdline Cloud the picture travels encrypted through the
      // relay, so it is asked for at a lower quality there.
      call<{ value: Shot }>("GET", "/v1/browser/screenshot?tab=" + tabID + "&quality=" + (relay ? "45" : "55")).then(
        (r) => { if (alive) { setShot(r.value); setShotError(null) } },
        (e: unknown) => { if (alive) setShotError(e) },
      ).finally(() => { inFlight = false })
    }
    read()
    const timer = window.setInterval(read, SHOT_MS)
    return () => { alive = false; window.clearInterval(timer) }
  }, [open, relay, tabID])

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

  const act = useCallback(async (op: string, args: Record<string, unknown>) => {
    if (!tabID) return
    setBusy(true)
    setActError(null)
    try {
      await call("POST", "/v1/browser/person", { op, tab: tabID, args })
    } catch (e) {
      setActError(e)
    } finally {
      setBusy(false)
    }
  }, [tabID])

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

  const control = async (on: boolean) => {
    if (!tabID) return
    setBusy(true)
    setActError(null)
    try {
      await call("POST", "/v1/browser/control", { tab: tabID, person: on })
      setStatus((s) => s && { ...s, status: { ...s.status, tabs: s.status.tabs.map((t) => t.tab === tabID ? { ...t, person: on } : t) } })
    } catch (e) {
      setActError(e)
    } finally {
      setBusy(false)
    }
  }

  const clickPicture = (e: MouseEvent<HTMLImageElement>) => {
    if (!person || !shot) return
    const box = e.currentTarget.getBoundingClientRect()
    const x = Math.round(((e.clientX - box.left) / box.width) * shot.width)
    const y = Math.round(((e.clientY - box.top) / box.height) * shot.height)
    void act("click", { x, y })
  }

  const send = (e: FormEvent) => {
    e.preventDefault()
    if (!text) return
    void act("type", { text }).then(() => setText(""))
  }

  const body = () => {
    if (statusError) return <div className="screen-note err" role="alert">{said(statusError, "browserPanelReadFailed")}</div>
    if (!status) return <div className="screen-note" role="status">…</div>
    if (!status.status.connected) return <div className="screen-note" role="status">{nextWord("browserPanelNotConnected")}</div>
    if (!tab) return <div className="screen-note" role="status">{nextWord("browserPanelNoTab")}</div>
    return (
      <div className="browser-stage">
        {status.status.stopped && <div className="screen-note" role="status">{nextWord("browserPanelStopped")}</div>}
        {shotError ? <div className="screen-note err" role="alert">{(shotError as { code?: string })?.code === "browser_other_debugger" ? nextWord("browserPanelOtherDebugger") : said(shotError, "browserPanelReadFailed")}</div> : null}
        {shot && (
          <img
            className="browser-shot"
            data-person={person ? "true" : undefined}
            src={"data:image/jpeg;base64," + shot.data}
            alt={nextWord("browserPanelImageAlt", { title: shot.title || shot.url })}
            onClick={clickPicture}
          />
        )}
        <section className="browser-activity" aria-labelledby="browser-activity-title">
          <h3 id="browser-activity-title">{nextWord("browserPanelActivity")}</h3>
          {activity.length === 0 ? <p className="browser-activity-empty">{nextWord("browserPanelActivityEmpty")}</p> : (
            <ol>
              {activity.map((row, i) => (
                <li key={row.at + ":" + i}>
                  <img src={"data:image/jpeg;base64," + row.thumb} alt="" />
                  <span className="browser-activity-words">
                    <b>{OP_WORD[row.op] ? nextWord(OP_WORD[row.op]) : row.op}</b>
                    <span>{row.op === "type" ? (row.password ? "••••" : "") : row.target || address(row.url).host}</span>
                    <time dateTime={new Date(row.at * 1000).toISOString()}>{new Date(row.at * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</time>
                  </span>
                </li>
              ))}
            </ol>
          )}
        </section>
      </div>
    )
  }

  const ready = !!tab && !!status?.status.connected
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
      {open && ready && (
        <div className="browser-controls">
          <div className="browser-who" data-person={person ? "true" : undefined}>
            <span aria-live="polite">{person ? nextWord("browserPanelPersonActs", { assistant: name }) : nextWord("browserPanelAgentActs", { assistant: name })}</span>
            <button type="button" className="chip browser-notify" aria-pressed={!muted} disabled={busy} onClick={() => void mute(!muted)}>
              {muted ? nextWord("browserPanelNotifyOff") : nextWord("browserPanelNotifyOn")}
            </button>
            <button type="button" className={person ? "chip browser-take" : "chip browser-take browser-primary"} disabled={busy} onClick={() => void control(!person)}>
              {person ? nextWord("browserPanelHandBack", { assistant: name }) : nextWord("browserPanelTakeOver")}
            </button>
          </div>
          {person && (
            <>
              <form className="browser-type" onSubmit={send}>
                <label className="sr-only" htmlFor="browser-text">{nextWord("browserPanelTypeLabel")}</label>
                <input id="browser-text" value={text} autoComplete="off" placeholder={nextWord("browserPanelTypePlaceholder")} onChange={(e) => setText(e.target.value)} />
                <button type="submit" className="chip browser-primary" disabled={busy || !text}>{nextWord("browserPanelSend")}</button>
              </form>
              <div className="browser-keys">
                <button type="button" className="chip" disabled={busy} onClick={() => void act("key", { key: "Tab" })}>Tab</button>
                <button type="button" className="chip" disabled={busy} onClick={() => void act("key", { key: "Enter" })}>Enter</button>
                <button type="button" className="chip" disabled={busy} onClick={() => void act("key", { key: "Backspace" })}>{nextWord("browserPanelKeyBackspace")}</button>
                <button type="button" className="chip" disabled={busy} onClick={() => void act("scroll", { direction: "up" })}>{nextWord("browserPanelScrollUp")}</button>
                <button type="button" className="chip" disabled={busy} onClick={() => void act("scroll", { direction: "down" })}>{nextWord("browserPanelScrollDown")}</button>
              </div>
            </>
          )}
          {actError ? <div className="screen-note err" role="alert">{said(actError, "browserPanelActFailed")}</div> : null}
        </div>
      )}
    </section>
  )
}

/**
 * The header's sign that this Session is using a Chrome tab. It reads the
 * bridge's status every 15 seconds while the page is visible and the panel is
 * closed, and shows nothing when the Session has no tab.
 */
export function BrowserChip({ row, hidden, onOpen }: { row: SessionRow | null; hidden: boolean; onOpen: () => void }) {
  const conversation = row?.sessionId ?? ""
  const [has, setHas] = useState(false)
  useEffect(() => {
    setHas(false)
    if (!conversation || hidden) return
    let alive = true
    const read = () => {
      if (document.hidden) return
      call<Status>("GET", "/v1/browser/status").then(
        (s) => { if (alive) setHas(!!s.status?.connected && s.status.tabs.some((t) => t.conversation === conversation)) },
        () => { if (alive) setHas(false) },
      )
    }
    read()
    const timer = window.setInterval(read, 15_000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [conversation, hidden])
  if (!has || hidden) return null
  return (
    <button type="button" className="browser-chip" title={nextWord("browserChipTip")} aria-label={nextWord("browserChipTip")} onClick={onOpen}>
      <i aria-hidden="true" />{nextWord("browserChip")}
    </button>
  )
}
