import { useEffect, useRef, useState, type ReactElement } from "react"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import { openImagePreview } from "../legacy/images-bridge.js"
import "./browser-activity.css"

type Action = { id?: string; at: number; op: string; site?: string; url?: string; target?: string; text_length?: number; password?: boolean; ok: boolean; error?: string; thumb: string }
const words: Record<string, Parameters<typeof nextWord>[0]> = {
  open: "browserOpOpen", read: "browserOpRead", click: "browserOpClick", type: "browserOpType", key: "browserOpKey",
  scroll: "browserOpScroll", screenshot: "browserOpScreenshot", takeover: "browserOpTakeover", handback: "browserOpHandback", release: "browserOpRelease",
  viewport: "browserOpViewport",
}

// Reads, screenshots and control bookkeeping remain in the complete audit.
// The conversation shows changes to the page instead of a second debug log.
const quietOperations = new Set(["read", "screenshot", "release", "close", "takeover", "handback"])
const hostOf = (action: Action) => { try { return new URL(action.url || "").host || action.site } catch { return action.site } }

function ActionPicture({ thumb, title }: { thumb: string; title: string }) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [preview, setPreview] = useState(false)
  const source = "data:image/jpeg;base64," + thumb
  useEffect(() => {
    if (preview && trigger.current) return openImagePreview(trigger.current, source, "clawdline-browser.jpg", () => setPreview(false))
  }, [preview, source])
  return <button type="button" ref={trigger} className="browser-action-picture" aria-label={L.strings.webImagePreview} onClick={() => setPreview(true)}>
    <img src={source} alt={nextWord("browserPanelImageAlt", { title })} loading="lazy" />
  </button>
}

/** Audit pictures already captured by page actions; this never requests a frame. */
const reasons = {browser_disabled:"browserAuditWhyDisabled",browser_wrong_mode:"browserAuditWhyMode",browser_tab_closing:"browserAuditWhyClosing",browser_stopped:"browserAuditWhyStopped",browser_person_controlling:"browserAuditWhyPerson",browser_timeout:"browserAuditWhyTimeout"} as const

export function useBrowserActivity(conversation: string | undefined): { at: number; node: ReactElement }[] {
  const [actions, setActions] = useState<Action[]>([])
  const [error, setError] = useState<unknown>(null)
  useEffect(() => {
    setActions([]); setError(null)
    if (!conversation) return
    const abort = new AbortController()
    let pending = false
    const read = async () => {
      if (pending || document.hidden) return
      pending = true
      try {
        const res = await fetch("/v1/browser/activity?conversation=" + encodeURIComponent(conversation), { cache: "no-store", signal: abort.signal })
        if (!res.ok) throw new Error(String(res.status))
        const result = await res.json() as { rows: Action[] }
        if (!abort.signal.aborted) { setActions(result.rows ?? []); setError(null) }
      } catch (e) { if (!abort.signal.aborted) setError(e) }
      finally { pending = false }
    }
    void read()
    // Follow the same visible-page cadence as the browser panel's activity list.
    const timer = window.setInterval(() => void read(), 3000)
    return () => { abort.abort(); window.clearInterval(timer) }
  }, [conversation])
  const cards = actions.filter(action => !quietOperations.has(action.op)).reverse().map((action, i) => ({ at: action.at, node: <article className="entry browser-action" key={"browser:" + conversation + ":" + (action.id || action.at + ":" + i)}>
    <div className="who"><span className="speaker">{nextWord("browserChip")}</span><time dateTime={new Date(action.at * 1000).toISOString()}>{new Date(action.at * 1000).toLocaleTimeString()}</time></div>
    <div className="body">
      <b>{words[action.op] ? nextWord(words[action.op]) : action.op}</b>
      <span>{action.op === "type" ? (action.password ? nextWord("browserAuditPrivateInput", { count: action.text_length ?? 0 }) : "") : action.op === "viewport" && (action.target === "mobile" || action.target === "desktop") ? nextWord(action.target === "mobile" ? "browserPanelMobile" : "browserPanelDesktop") : action.target || hostOf(action)}</span>
      {!action.ok && <p role="status">{action.error && Object.hasOwn(reasons,action.error) ? nextWord(reasons[action.error as keyof typeof reasons]) : action.error}</p>}
      {action.thumb && <ActionPicture thumb={action.thumb} title={action.site || ""} />}
    </div>
  </article> }))
  if (error && cards.length) cards.push({ at: actions[0].at, node: <p key="browser-read-failed" className="screen-note err" role="status">{nextWord("browserPanelReadFailed", { reason: L.strings.webOffline })}</p> })
  return cards.sort((a, b) => a.at - b.at)
}
