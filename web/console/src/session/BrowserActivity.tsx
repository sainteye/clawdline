import { useEffect, useMemo, useRef, useState, type ReactElement } from "react"
import { currentCatalogTag } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { inlineImageDimensions } from "../legacy/js/view/document-html-images.js"
import * as L from "../legacy/bridge.js"
import { openImagePreview } from "../legacy/images-bridge.js"
import { documentVisibility, Poller, type PollState } from "../poll.js"
import { browserCall, type BrowserCall } from "./browser-call.js"
import "./browser-activity.css"

type Action = { id?: string; at: number; op: string; site?: string; url?: string; target?: string; text_length?: number; password?: boolean; ok: boolean; error?: string; thumb: string }
type ActivityListener = (state: PollState<Action[]>) => void
type ActivityFeed = { poller: Poller<Action[]>; listeners: Set<ActivityListener> }
const activeFeeds = new WeakMap<BrowserCall, Map<string, ActivityFeed>>()

/** A transcript and its Chrome panel read the same conversation through the same caller. */
export function useBrowserActivityFeed(conversation: string | undefined, call = browserCall, scope = "") {
  const key = conversation ? JSON.stringify([scope, conversation]) : null
  const [reading, setReading] = useState<{ key: string; call: BrowserCall; state: PollState<Action[]> } | null>(null)
  useEffect(() => {
    if (!key || !conversation) return
    const feeds = activeFeeds.get(call) ?? new Map<string, ActivityFeed>()
    activeFeeds.set(call, feeds)
    let feed = feeds.get(key)
    if (!feed) {
      const listeners = new Set<ActivityListener>()
      const poller = new Poller<Action[]>({
        read: async () => (await call<{ rows: Action[] }>("GET",
          "/v1/browser/activity?conversation=" + encodeURIComponent(conversation))).rows ?? [],
        onChange: (state) => { for (const listener of listeners) listener(state) },
        intervalMs: 3000,
        visibility: documentVisibility(),
        classify: () => "unknown",
      })
      feed = { poller, listeners }
      feeds.set(key, feed)
    }
    const { poller, listeners } = feed
    const listener: ActivityListener = (state) => setReading({ key, call, state })
    listeners.add(listener)
    listener(poller.get())
    if (listeners.size === 1) poller.start()
    return () => {
      listeners.delete(listener)
      if (listeners.size === 0) {
        poller.stop()
        feeds.delete(key)
      }
    }
  }, [key, conversation, call])
  return reading && reading.key === key && reading.call === call ? reading.state : null
}
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
  const size = useMemo(() => inlineImageDimensions(source), [source])
  useEffect(() => {
    if (preview && trigger.current) return openImagePreview(trigger.current, source, "clawdline-browser.jpg", () => setPreview(false))
  }, [preview, source])
  return <button type="button" ref={trigger} className="browser-action-picture" style={{ width: size?.width }} aria-label={L.strings.webImagePreview} onClick={() => setPreview(true)}>
    <img src={source} alt={nextWord("browserPanelImageAlt", { title })} width={size?.width} height={size?.height} loading="lazy" />
  </button>
}

/** Audit pictures already captured by page actions; this never requests a frame. */
const reasons = {browser_disabled:"browserAuditWhyDisabled",browser_wrong_mode:"browserAuditWhyMode",browser_tab_closing:"browserAuditWhyClosing",browser_stopped:"browserAuditWhyStopped",browser_person_controlling:"browserAuditWhyPerson",browser_timeout:"browserAuditWhyTimeout"} as const

export function useBrowserActivity(conversation: string | undefined, scope: string): { at: number; node: ReactElement }[] {
  const reading = useBrowserActivityFeed(conversation, browserCall, scope)
  const data = reading?.data
  const failed = !!reading?.error
  const locale = currentCatalogTag()
  return useMemo(() => {
    const actions = data ?? []
    const cards = actions.filter(action => !quietOperations.has(action.op)).reverse().map((action, i) => ({ at: action.at, node: <article className="entry browser-action" key={"browser:" + conversation + ":" + (action.id || action.at + ":" + i)}>
      <div className="who"><span className="speaker">{nextWord("browserChip")}</span><time dateTime={new Date(action.at * 1000).toISOString()}>{new Date(action.at * 1000).toLocaleTimeString()}</time></div>
      <div className="body">
        <b>{words[action.op] ? nextWord(words[action.op]) : action.op}</b>
        <span>{action.op === "type" ? (action.password ? nextWord("browserAuditPrivateInput", { count: action.text_length ?? 0 }) : "") : action.op === "viewport" && (action.target === "mobile" || action.target === "desktop") ? nextWord(action.target === "mobile" ? "browserPanelMobile" : "browserPanelDesktop") : action.target || hostOf(action)}</span>
        {!action.ok && <p role="status">{action.error && Object.hasOwn(reasons,action.error) ? nextWord(reasons[action.error as keyof typeof reasons]) : action.error}</p>}
        {action.thumb && <ActionPicture thumb={action.thumb} title={action.site || ""} />}
      </div>
    </article> }))
    if (failed && cards.length) cards.push({ at: actions[0].at, node: <p key="browser-read-failed" className="screen-note err" role="status">{nextWord("browserPanelReadFailed", { reason: L.strings.webOffline })}</p> })
    return cards.sort((a, b) => a.at - b.at)
  }, [conversation, data, failed, locale])
}
