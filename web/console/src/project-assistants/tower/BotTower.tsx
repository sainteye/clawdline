import { useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "../words"
import { assignFloors, projectOf, slotPoint, strollPoint, towerBots, towerSummary, type TowerBot, type TowerMood } from "./tower-state"
import { ramenScene, sectionScene, towerFloors, type TowerScene } from "./tower-scene"
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

// URLs rather than imports keep test bundlers that have no image loader working; Vite still emits the files.
const pictures: Record<TowerScene["picture"], string> = {
  ramen: new URL("./ramen.webp", import.meta.url).href,
  skyline: new URL("./skyline.webp", import.meta.url).href,
  rain: new URL("./rain.webp", import.meta.url).href,
  platform: new URL("./platform.webp", import.meta.url).href,
  building: new URL("./building.webp", import.meta.url).href,
}
export type TowerLayout = "floor" | "building"
const layoutKey = "clawdline.bots.towerLayout", floorKey = "clawdline.bots.towerFloor", sceneKey = "clawdline.bots.towerScene"
const readPref = (key: string) => { try { return localStorage.getItem(key) } catch { return null } }
const writePref = (key: string, value: string) => { try { localStorage.setItem(key, value) } catch { /* This page keeps the choice only. */ } }
const BotSize = 74, SectionBotSize = 44, WalkMs = 1000
/** The cross-section is a view of its own, beside the four floors. */
const Section = -1
/** A building is offered only when one floor would be crowded: more Bots than
 * this, across at least two Projects. A presentation threshold, not a bound. */
export const TowerBuildingMinBots = 7
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
function place(scene: TowerScene, b: TowerBot, index: number, now: number, still: boolean): { at: readonly [number, number], moving: boolean } {
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
  const [layout, setLayoutState] = useState<TowerLayout>(() => readPref(layoutKey) === "building" ? "building" : "floor")
  const [chosenFloor, setChosenFloor] = useState<number | null>(() => { const v = readPref(floorKey); if (v === "section") return Section; const n = Number(v); return v !== null && Number.isInteger(n) && n >= 0 && n < towerFloors.length ? n : null })
  const allBots = useMemo(() => towerBots(items, now, w as unknown as Record<string, string>), [items, now, w])
  const summary = towerSummary(allBots)
  const floorOf = useMemo(() => assignFloors(items, towerFloors.length), [items])
  const floors = towerFloors.map((f, i) => {
    const here = allBots.filter(b => floorOf.get(projectOf(b.item)) === i)
    const names = [...new Set(here.map(b => projectName(b.item) || w.unnamedProject))]
    return { ...f, index: i, bots: here, names, wait: here.filter(b => b.mood === "wait").length }
  })
  // The chosen floor, or else the first floor someone waits on, or else the first lived-in floor.
  const floor = chosenFloor === Section ? Section : chosenFloor !== null && floors[chosenFloor].bots.length ? chosenFloor
    : (floors.find(f => f.wait)?.index ?? floors.find(f => f.bots.length)?.index ?? 0)
  // The last real floor visited also dresses the one-floor layout.
  const [sceneFloor, setSceneFloor] = useState<number | null>(() => { const v = readPref(sceneKey); const n = Number(v); return v !== null && Number.isInteger(n) && n >= 0 && n < towerFloors.length ? n : null })
  const goFloor = (i: number) => {
    setChosenFloor(i); writePref(floorKey, i === Section ? "section" : String(i))
    if (i !== Section) { setSceneFloor(i); writePref(sceneKey, String(i)) }
  }
  // Few Bots share one floor; the building is a choice only when one floor would crowd.
  const canBuild = allBots.length >= TowerBuildingMinBots && new Set(allBots.map(b => projectOf(b.item))).size >= 2
  const building = canBuild && layout === "building"
  const setLayout = (next: TowerLayout) => {
    // Leaving the building for one floor keeps the floor being looked at.
    if (next === "floor" && building && floor !== Section) { setSceneFloor(floor); writePref(sceneKey, String(floor)) }
    setLayoutState(next); writePref(layoutKey, next)
  }
  const list = new Intl.ListFormat(language, { style: "narrow", type: "unit" })
  const section = building && floor === Section
  const scene: { picture: TowerScene["picture"], width: number, height: number } = section ? sectionScene : building ? floors[floor].scene
    : (sceneFloor !== null ? towerFloors[sceneFloor].scene : ramenScene)
  const bots = section ? allBots : building ? floors[floor].bots : allBots
  const size = section ? SectionBotSize : BotSize
  // In the cross-section every Bot stands on its own floor's strip: busy ones at
  // desks, in the queue or the failure corner; everyone else at the rest spots.
  const sectionGroup = (b: TowerBot) => b.mood === "work" ? "desk" : b.mood === "wait" ? "queue" : b.mood === "error" ? "repair" : "rest"
  const slots = useMemo(() => {
    const seen = new Map<string, number>(), index = new Map<string, number>()
    for (const b of bots) {
      if (!section && b.mood === "idle") continue
      const group = section ? `${floorOf.get(projectOf(b.item))}:${sectionGroup(b)}` : b.zone
      const n = seen.get(group) ?? 0
      index.set(b.key, n); seen.set(group, n + 1)
    }
    return index
  }, [bots, section, floorOf])
  const locate = (b: TowerBot, t: number, still: boolean): { at: readonly [number, number], moving: boolean } => {
    if (!section) return place(scene as TowerScene, b, slots.get(b.key) ?? 0, t, still)
    const f = sectionScene.floors[floorOf.get(projectOf(b.item)) ?? 0]
    return { at: slotPoint(f[sectionGroup(b)], slots.get(b.key) ?? 0), moving: false }
  }
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
  const scenery = useRef({ bots, running, scene, locate, size })
  scenery.current = { bots, running, scene, locate, size }
  const drawRef = useRef<(t: number) => void>(() => {})
  // Where each Bot was last drawn and where it is heading: a change of spot
  // (allowed, finished, woke) walks there for WalkMs instead of jumping.
  const trail = useRef(new Map<string, { sig: string, from: readonly [number, number], to: readonly [number, number], start: number, drawn: readonly [number, number] }>())
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
      const { bots, running, scene, locate, size } = scenery.current
      const k = canvas.width / scene.width, still = !running
      g.clearRect(0, 0, canvas.width, canvas.height)
      const placed = bots.map(b => {
        const spot = locate(b, t, still), sig = `${scene.picture}:${b.mood === "idle" && scene.picture !== "building" ? "stroll" : `${spot.at[0]},${spot.at[1]}`}`
        const prev = trail.current.get(b.key)
        if (!prev || prev.sig.split(":")[0] !== scene.picture || still) {
          trail.current.set(b.key, { sig, from: spot.at, to: spot.at, start: t, drawn: spot.at })
          return { b, ...spot }
        }
        if (prev.sig !== sig) { prev.sig = sig; prev.from = prev.drawn; prev.start = t }
        const k = Math.min(1, (t - prev.start) / WalkMs)
        if (k >= 1 || prev.from === spot.at) { prev.drawn = spot.at; return { b, ...spot } }
        const e = k < .5 ? 2 * k * k : 1 - (-2 * k + 2) ** 2 / 2
        const at: readonly [number, number] = [prev.from[0] + (spot.at[0] - prev.from[0]) * e, prev.from[1] + (spot.at[1] - prev.from[1]) * e]
        prev.drawn = at
        return { b, at, moving: true }
      }).sort((a, z) => a.at[1] - z.at[1])
      for (const { b, at, moving } of placed) {
        drawBot(g, { shape: b.item.bot.appearance?.shape ?? "blob", color: (b.item.bot.appearance?.color ?? "").toLowerCase(), mood: b.mood, seed: b.seed, unread: b.unread }, at[0] * k, at[1] * k, size * k, moving, t, still, dpr, b.live ? 1 : .55)
        const button = buttons.current.get(b.key)
        if (button) {
          button.style.left = `${(at[0] - size / 2) / scene.width * 100}%`
          button.style.top = `${(at[1] - size) / scene.height * 100}%`
          button.style.width = `${size / scene.width * 100}%`
          button.style.height = `${(size + 6) / scene.height * 100}%`
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
  useEffect(() => { if (!running) drawRef.current(performance.now()) }, [bots, slots, running, scene])
  const sectionPct = (x: number, y: number) => ({ left: `${x / sectionScene.width * 100}%`, top: `${y / sectionScene.height * 100}%` })

  // HUD chips: each fact is its own chip, the count picked out as the glowing figure.
  type Chip = { key: string, tone: string, text: string }
  const chips: Chip[] = [{ key: "total", tone: "total", text: w.towerBots.replace("{count}", String(summary.total)) }]
  if (summary.wait) chips.push({ key: "wait", tone: "wait", text: w.towerWaiting.replace("{count}", String(summary.wait)) })
  if (summary.work) chips.push({ key: "work", tone: "work", text: w.towerWorking.replace("{count}", String(summary.work)) })
  if (summary.error) chips.push({ key: "error", tone: "error", text: w.towerErrors.replace("{count}", String(summary.error)) })
  if (summary.unread) chips.push({ key: "unread", tone: "unread", text: w.towerUnread.replace("{count}", String(summary.unread)) })
  if (summary.total && !summary.wait && !summary.work && !summary.error) chips.push({ key: "idle", tone: "idle", text: w.towerAllIdle })
  if (summary.latest) {
    const when = ago(summary.latest.item.activity_at, now, language)
    if (when) chips.push({ key: "latest", tone: "latest", text: w.towerLatest.replace("{time}", when).replace("{name}", summary.latest.item.bot.name) })
  }
  if (summary.stale) chips.push({ key: "stale", tone: "stale", text: w.towerStale })
  const figure = (text: string) => text.split(/(\d+)/).map((part, i) => i % 2 ? <b key={i}>{part}</b> : part)
  const hud = <div className="pa-tower-hud">
    <p className="pa-hud-facts" role="status">
      {summary.total ? chips.map(c => <span key={c.key} className="pa-hud-chip" data-tone={c.tone}>{c.key === "total" && <i className="pa-hud-live" aria-hidden="true"/>}{figure(c.text)}</span>) : <span className="pa-hud-chip" data-tone="idle">{w.towerEmpty}</span>}
    </p>
    {canBuild && <div className="pa-hud-switch" role="group" aria-label={w.towerLayout}>
      <button type="button" aria-pressed={!building} onClick={() => setLayout("floor")}>{w.towerLayoutFloor}</button>
      <button type="button" aria-pressed={building} onClick={() => setLayout("building")}>{w.towerLayoutBuilding}</button>
    </div>}
  </div>
  // The elevator is a small glowing wireframe of the building: one isometric
  // slab per floor, its Bot count beside it, a lit pixel where someone waits.
  const elevator = building && <nav className="pa-tower-elevator" aria-label={w.towerElevator}>
    <button type="button" className="pa-elevator-all" aria-pressed={section} aria-label={w.towerSection} title={w.towerSection} onClick={() => goFloor(Section)}>
      <svg viewBox="0 0 96 22" aria-hidden="true"><path d="M30 20V6l18-4 18 4v14M38 9h4M54 9h4M38 14h4M54 14h4"/></svg>
      <b>{allBots.length}</b>
    </button>
    <svg className="pa-elevator-shaft" viewBox="0 0 96 132" aria-hidden="true"><path d="M8 18V114M48 6V102M88 18V114"/></svg>
    {floors.map(f => {
      const detail = f.bots.length ? `${list.format(f.names)} · ${f.wait ? w.towerWaiting.replace("{count}", String(f.wait)) : w.towerBots.replace("{count}", String(f.bots.length))}` : w.towerFloorEmpty
      return <button key={f.label} type="button" aria-pressed={f.index === floor} data-wait={f.wait || undefined} disabled={!f.bots.length}
        aria-label={`${f.label}, ${detail}`} title={detail} onClick={() => goFloor(f.index)}>
        <svg viewBox="0 0 96 26" aria-hidden="true"><path d="M8 13 48 1 88 13 48 25Z"/><text x="48" y="16.5">{f.label}</text></svg>
        <b>{f.bots.length}</b>
      </button>
    })}
  </nav>

  return <section className="pa-tower" aria-label={w.towerView}>
    <div className="pa-tower-stage">
      <div className="pa-tower-pan">
        {hud}
        {elevator}
        <div className="pa-tower-scene" ref={sceneRef} data-stale={summary.stale || undefined}>
          <img key={scene.picture} src={pictures[scene.picture]} alt="" draggable={false} decoding="async"/>
          {building && <span key={`door-${floor}`} className="pa-tower-doors" aria-hidden="true"/>}
          <canvas ref={canvasRef} aria-hidden="true"/>
          {section && sectionScene.floors.map((f, i) => {
            const fl = floors[i], [y0, y1] = f.band
            const box = { ...sectionPct(sectionScene.left, y0), width: `${(sectionScene.right - sectionScene.left) / sectionScene.width * 100}%`, height: `${(y1 - y0) / sectionScene.height * 100}%` }
            return <div key={fl.label} className="pa-section-floor" data-wait={fl.wait || undefined} style={box}>
              <span className="pa-section-lamp" aria-hidden="true" style={{ left: `${(f.lamp[0] - sectionScene.left) / (sectionScene.right - sectionScene.left) * 100}%`, top: `${(f.lamp[1] - y0) / (y1 - y0) * 100}%` }}/>
              <button type="button" className="pa-section-plate" disabled={!fl.bots.length} onClick={() => goFloor(i)}
                aria-label={w.towerEnterFloor.replace("{floor}", fl.label)}>
                <b>{fl.label}</b><span>{fl.bots.length ? list.format(fl.names) : w.towerFloorEmpty}</span>
                {fl.bots.length > 0 && <small>{fl.wait ? w.towerWaiting.replace("{count}", String(fl.wait)) : w.towerBots.replace("{count}", String(fl.bots.length))}</small>}
              </button>
            </div>
          })}
          {bots.map(b => {
            const mood = b.unread ? `${w[moodWord[b.mood]]}, ${w.towerMoodUnread}` : w[moodWord[b.mood]], project = projectName(b.item), when = ago(b.item.activity_at, now, language)
            const label = w.towerBotLabel.replace("{name}", b.item.bot.name).replace("{state}", mood)
            return <button key={b.key} type="button" className="pa-tower-bot" data-bot-key={b.key} data-mood={b.mood} data-pinned={!section && (b.mood === "wait" || b.mood === "error") || undefined} data-highlight={highlight === b.key || undefined} aria-label={label}
              ref={el => { if (el) buttons.current.set(b.key, el); else buttons.current.delete(b.key) }}
              style={{ width: `${size / scene.width * 100}%`, height: `${(size + 6) / scene.height * 100}%` }}
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
