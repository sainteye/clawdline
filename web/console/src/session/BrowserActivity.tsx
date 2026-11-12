import { useEffect, useState, type ReactElement } from "react"
import { nextWord } from "../next-strings.js"
import * as L from "../legacy/bridge.js"
import "./browser-activity.css"

type Action = { id?: string; at: number; op: string; site?: string; target?: string; text_length?: number; password?: boolean; ok: boolean; error?: string; thumb: string }
const words: Record<string, Parameters<typeof nextWord>[0]> = {
  open: "browserOpOpen", read: "browserOpRead", click: "browserOpClick", type: "browserOpType", key: "browserOpKey",
  scroll: "browserOpScroll", screenshot: "browserOpScreenshot", takeover: "browserOpTakeover", handback: "browserOpHandback", release: "browserOpRelease",
}

/** Audit pictures already captured by agent actions; this never requests a frame. */
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
  const cards = [...actions].reverse().map((action, i) => ({ at: action.at, node: <article className="entry browser-action" key={"browser:" + conversation + ":" + (action.id || action.at + ":" + i)}>
    <div className="who"><span className="speaker">{nextWord("browserChip")}</span><time dateTime={new Date(action.at * 1000).toISOString()}>{new Date(action.at * 1000).toLocaleTimeString()}</time></div>
    <div className="body">
      <b>{words[action.op] ? nextWord(words[action.op]) : action.op}</b>
      <span>{action.op === "type" ? (action.password ? nextWord("browserAuditPassword", { count: action.text_length ?? 0 }) : "") : action.target || action.site}</span>
      {!action.ok && <p role="status">{action.error}</p>}
      {action.thumb && <img src={"data:image/jpeg;base64," + action.thumb} alt={nextWord("browserPanelImageAlt", { title: action.site || "" })} loading="lazy" />}
    </div>
  </article> }))
  if (error && actions.length) cards.push({ at: actions[0].at, node: <p key="browser-read-failed" className="screen-note err" role="status">{nextWord("browserPanelReadFailed", { reason: L.strings.webOffline })}</p> })
  return cards.sort((a, b) => a.at - b.at)
}
