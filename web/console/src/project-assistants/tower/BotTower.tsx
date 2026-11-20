import { useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "../words"
import { slotPoint, strollPoint, towerBots, towerSummary, type TowerBot, type TowerMood } from "./tower-state"
import { ramenScene as scene } from "./tower-scene"
import { drawBot } from "./tower-draw"
import "./tower.css"

export interface BotTowerProps {
  items: readonly W.ProjectAssistantBotSummary[]
  w: Words
  language: string
  /** Something covers the tower (a group room); drawing stops. */
  paused?: boolean
  /** A Bot pointed at elsewhere (the roster); its tag shows as if hovered here. */
  highlight?: string | null
  projectName: (item: W.ProjectAssistantBotSummary) => string | undefined
  onSelect: (item: W.ProjectAssistantBotSummary) => void
}

// A URL rather than an import keeps test bundlers that have no image loader working; Vite still emits the file.
const ramen = new URL("./ramen.webp", import.meta.url).href
const BotSize = 74
const RecomputeMs = 60_000
const moodWord: Record<TowerMood, keyof Words> = {
  wait: "towerMoodWait", work: "towerMoodWork", error: "towerMoodError",
  recent: "towerMoodRecent", idle: "towerMoodIdle", sleep: "towerMoodSleep",
}

function ago(at: string, now: number, language: string) {
  const t = Date.parse(at)
  if (!Number.isFinite(t) || t <= 0) return ""
  const s = Math.max(0, (now - t) / 1000)
  const f = new Intl.RelativeTimeFormat(language, { numeric: "auto" })
  if (s < 60) return f.format(0, "minute")
  if (s < 3600) return f.format(-Math.floor(s / 60), "minute")
  if (s < 86400) return f.format(-Math.floor(s / 3600), "hour")
  return f.format(-Math.floor(s / 86400), "day")
}

function useReducedMotion() {
  const query = typeof matchMedia === "function" ? matchMedia("(prefers-reduced-motion: reduce)") : undefined
  const [reduced, setReduced] = useState(!!query?.matches)
  useEffect(() => {
    if (!query) return
    const change = () => setReduced(query.matches)
    query.addEventListener("change", change)
    return () => query.removeEventListener("change", change)
  }, [])
  return reduced
}

/** Feet position of a Bot at time `now`, in background-image pixels. */
function place(b: TowerBot, index: number, now: number, still: boolean): { at: readonly [number, number], moving: boolean } {
  if (b.mood === "idle") return strollPoint(scene.zones.lounge, b.seed, now, still)
  return { at: slotPoint(scene.zones[b.zone], index), moving: false }
}

export default function BotTower({ items, w, language, paused = false, highlight = null, projectName, onSelect }: BotTowerProps) {
  const [tick, setTick] = useState(() => Date.now())
  const reduced = useReducedMotion()
  const [visible, setVisible] = useState(true)
  const [pageShown, setPageShown] = useState(() => typeof document === "undefined" || !document.hidden)
  const sceneRef = useRef<HTMLDivElement>(null), canvasRef = useRef<HTMLCanvasElement>(null)
  const buttons = useRef(new Map<string, HTMLButtonElement>())
  // Item changes place Bots at once; the idle tiers also age once a minute.
  const now = useMemo(() => Date.now(), [items, tick])
  const bots = useMemo(() => towerBots(items, now, w as unknown as Record<string, string>), [items, now, w])
  const summary = towerSummary(bots)
  const slots = useMemo(() => {
    const seen = new Map<string, number>(), index = new Map<string, number>()
    for (const b of bots) {
      if (b.mood === "idle") continue
      const n = seen.get(b.zone) ?? 0
      index.set(b.key, n); seen.set(b.zone, n + 1)
    }
    return index
  }, [bots])
  const running = !paused && visible && pageShown && !reduced

  useEffect(() => {
    if (paused || !pageShown) return
    const id = setInterval(() => setTick(Date.now()), RecomputeMs)
    return () => clearInterval(id)
  }, [paused, pageShown])
  useEffect(() => {
    const change = () => setPageShown(!document.hidden)
    document.addEventListener("visibilitychange", change)
    return () => document.removeEventListener("visibilitychange", change)
  }, [])
  useEffect(() => {
    const el = sceneRef.current
    if (!el || typeof IntersectionObserver !== "function") return
    const io = new IntersectionObserver(entries => setVisible(entries.some(e => e.isIntersecting)))
    io.observe(el)
    return () => io.disconnect()
  }, [])

  // What to draw lives in a ref: a parent re-render (a roster hover, a new
  // roster frame) swaps the picture without restarting the loop or resizing
  // the canvas, since resizing clears it and showed as a flash of every Bot.
  const scenery = useRef({ bots, slots, running })
  scenery.current = { bots, slots, running }
  const drawRef = useRef<(t: number) => void>(() => {})
  useEffect(() => {
    const el = sceneRef.current, canvas = canvasRef.current
    if (!el || !canvas) return
    const g = canvas.getContext("2d")
    if (!g) return
    let dpr = 1
    const fit = () => {
      const r = el.getBoundingClientRect()
      dpr = Math.min(2, window.devicePixelRatio || 1)
      const w = Math.max(1, Math.round(r.width * dpr)), h = Math.max(1, Math.round(r.height * dpr))
      if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h }
    }
    drawRef.current = (t: number) => {
      const { bots, slots, running } = scenery.current
      const k = canvas.width / scene.width, still = !running
      g.clearRect(0, 0, canvas.width, canvas.height)
      const placed = bots.map(b => ({ b, ...place(b, slots.get(b.key) ?? 0, t, still) })).sort((a, z) => a.at[1] - z.at[1])
      for (const { b, at, moving } of placed) {
        drawBot(g, { shape: b.item.bot.appearance?.shape ?? "blob", color: (b.item.bot.appearance?.color ?? "").toLowerCase(), mood: b.mood, seed: b.seed, unread: b.unread }, at[0] * k, at[1] * k, BotSize * k, moving, t, still, dpr, b.live ? 1 : .55)
        const button = buttons.current.get(b.key)
        if (button) {
          button.style.left = `${(at[0] - BotSize / 2) / scene.width * 100}%`
          button.style.top = `${(at[1] - BotSize) / scene.height * 100}%`
        }
      }
    }
    fit()
    drawRef.current(performance.now())
    const ro = new ResizeObserver(() => { fit(); drawRef.current(performance.now()) })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  useEffect(() => {
    if (!running) { drawRef.current(performance.now()); return }
    let frame = 0
    const loop = (t: number) => { drawRef.current(t); frame = requestAnimationFrame(loop) }
    frame = requestAnimationFrame(loop)
    return () => cancelAnimationFrame(frame)
  }, [running])
  // A still picture (reduced motion, hidden) redraws when its Bots change.
  useEffect(() => { if (!running) drawRef.current(performance.now()) }, [bots, slots, running])

  const parts = [w.towerBots.replace("{count}", String(summary.total))]
  if (summary.wait) parts.push(w.towerWaiting.replace("{count}", String(summary.wait)))
  if (summary.work) parts.push(w.towerWorking.replace("{count}", String(summary.work)))
  if (summary.error) parts.push(w.towerErrors.replace("{count}", String(summary.error)))
  if (summary.unread) parts.push(w.towerUnread.replace("{count}", String(summary.unread)))
  if (summary.total && !summary.wait && !summary.work && !summary.error) parts.push(w.towerAllIdle)
  if (summary.latest) {
    const when = ago(summary.latest.item.activity_at, now, language)
    if (when) parts.push(w.towerLatest.replace("{time}", when).replace("{name}", summary.latest.item.bot.name))
  }

  return <section className="pa-tower" aria-label={w.towerView}>
    <p className="pa-tower-summary">
      {summary.total ? parts.join(" · ") : w.towerEmpty}
      {summary.stale && <span className="pa-tower-stale">{w.towerStale}</span>}
    </p>
    <div className="pa-tower-stage">
      <div className="pa-tower-pan">
        <div className="pa-tower-scene" ref={sceneRef} data-stale={summary.stale || undefined}>
          <img src={ramen} alt="" draggable={false} decoding="async"/>
          <canvas ref={canvasRef} aria-hidden="true"/>
          {bots.map(b => {
            const mood = b.unread ? `${w[moodWord[b.mood]]}, ${w.towerMoodUnread}` : w[moodWord[b.mood]], project = projectName(b.item), when = ago(b.item.activity_at, now, language)
            const label = w.towerBotLabel.replace("{name}", b.item.bot.name).replace("{state}", mood)
            return <button key={b.key} type="button" className="pa-tower-bot" data-bot-key={b.key} data-mood={b.mood} data-highlight={highlight === b.key || undefined} aria-label={label}
              ref={el => { if (el) buttons.current.set(b.key, el); else buttons.current.delete(b.key) }}
              style={{ width: `${BotSize / scene.width * 100}%`, height: `${(BotSize + 6) / scene.height * 100}%` }}
              onClick={() => onSelect(b.item)}>
              <span className="pa-tower-tip" aria-hidden="true">
                <strong>{b.item.bot.name}</strong>
                <span>{[project, mood].filter(Boolean).join(" · ")}</span>
                {b.mood === "work" || b.mood === "wait" || b.mood === "error" ? b.caption && <small>{b.caption}</small> : when && <small>{when}</small>}
              </span>
            </button>
          })}
        </div>
      </div>
    </div>
  </section>
}
