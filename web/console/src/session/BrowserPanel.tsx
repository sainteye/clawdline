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
type Status = { enabled: boolean; status: { connected: boolean; stopped: boolean; tabs: TabInfo[] } }
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
        {shot && <div className="browser-address" title={shot.url}>{shot.url}</div>}
        {shotError ? <div className="screen-note err" role="alert">{said(shotError, "browserPanelReadFailed")}</div> : null}
        {shot && (
          <img
            className="browser-shot"
            data-person={person ? "true" : undefined}
            src={"data:image/jpeg;base64," + shot.data}
            alt={nextWord("browserPanelImageAlt", { title: shot.title || shot.url })}
            onClick={clickPicture}
          />
        )}
      </div>
    )
  }

  const ready = !!tab && !!status?.status.connected
  return (
    <section className="panel-view" id="browser-panel" aria-labelledby="browser-title" hidden={!open}>
      <div className="panel-view-head">
        <h2 id="browser-title">{nextWord("browserPanelTitle")}</h2>
        {ready && <span className="screen-channel">{nextWord("browserPanelSampled")}</span>}
        <button className="chip" type="button" ref={closeRef} onClick={() => onClose(true)}>{L.strings.webClose}</button>
      </div>
      <div className="scroller panel-view-body" aria-live="polite">{open ? body() : null}</div>
      {open && ready && (
        <div className="browser-controls">
          <div className="browser-who" data-person={person ? "true" : undefined}>
            <span aria-live="polite">{person ? nextWord("browserPanelPersonActs", { assistant: name }) : nextWord("browserPanelAgentActs", { assistant: name })}</span>
            <button type="button" className={person ? "chip" : "chip browser-primary"} disabled={busy} onClick={() => void control(!person)}>
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
