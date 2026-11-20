import { useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "../words"
import { assignFloors, portalHelpers, projectOf, slotPoint, strollPoint, towerBots, towerSummary, waitingList, type TowerBot, type TowerMood } from "./tower-state"
import { ramenScene, sectionScene, towerFloors, type TowerScene } from "./tower-scene"
import { drawBot } from "./tower-draw"
import { TowerAllowCard, type TowerAllow } from "./TowerAllowCard"
import { allowOnce, readWaiting, AllowError, type AllowReason } from "./tower-allow"
import type { SourceClient } from "../client"
import { canWrite } from "../view-state"
import { placeFloor, placedFloors, readFloors, FloorError, type FloorDoc } from "./tower-floors"
import { TowerFloorsPanel, type FloorRow } from "./TowerFloorsPanel"
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
  /** The Bots' sources, to read and answer a waiting offer in place; without
   * them the waiting list is read-only. */
  sources?: readonly SourceClient[]
  current?: () => readonly SourceClient[]
  /** The roster is still loading: an empty tower is not yet known to be empty. */
  loading?: boolean
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
/** Helpers drawn on one portal at most, and where each stands around its
 * centre; more are counted on their Bot's card. Presentation, not a bound. */
const PortalHelpers = 6
const portalRing: readonly (readonly [number, number])[] = [[-46, 6], [46, 6], [0, -22], [-88, -14], [88, -14], [0, 30]]
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

export default function BotTower({ items, w, language, paused = false, highlight = null, projectName, onSelect, sources, current, loading = false }: BotTowerProps) {
  const [tick, setTick] = useState(() => Date.now())
  const reduced = useReducedMotion()
  const [visible, setVisible] = useState(true)
  const [pageShown, setPageShown] = useState(() => typeof document === "undefined" || !document.hidden)
  const sceneRef = useRef<HTMLDivElement>(null), canvasRef = useRef<HTMLCanvasElement>(null), viewRef = useRef<HTMLDivElement>(null)
  // On a phone the layout switch and floor map fold behind one floor button.
  const [panelOpen, setPanelOpen] = useState(false)
  // The waiting list opens from the HUD's waiting chip and closes once nobody waits.
  const [waitOpen, setWaitOpen] = useState(false)
  // A tapped Bot shows its card first; tapping it again opens the chat.
  const [picked, setPicked] = useState<string | null>(null)
  const cardRef = useRef<HTMLDivElement>(null)
  // The row showing its full request, and every offer already sent from here.
  const [shown, setShown] = useState<string | null>(null)
  const attempted = useRef(new Map<string, AllowReason | "allowed">())
  // Each offer is read and answered through its own Bot's source, never another.
  const sourceRef = useRef(sources); sourceRef.current = sources
  const allow = useMemo<TowerAllow | undefined>(() => !current ? undefined : {
    read(item, signal) {
      const source = sourceRef.current?.find(s => s.machineID === item.bot.key.machine_id)
      return source ? readWaiting(source, current, item, signal) : Promise.reject(new AllowError("revoked"))
    },
    send(waiting, signal) {
      const source = sourceRef.current?.find(s => s.machineID === waiting.scope.key.machine_id)
      return source ? allowOnce(source, current, waiting, signal) : Promise.reject(new AllowError("revoked"))
    },
  }, [current])
  const buttons = useRef(new Map<string, HTMLButtonElement>())
  // Item changes place Bots at once; the idle tiers also age once a minute.
  const now = useMemo(() => Date.now(), [items, tick])
  const [layout, setLayoutState] = useState<TowerLayout>(() => readPref(layoutKey) === "building" ? "building" : "floor")
  const [chosenFloor, setChosenFloor] = useState<number | null>(() => { const v = readPref(floorKey); if (v === "section") return Section; const n = Number(v); return v !== null && Number.isInteger(n) && n >= 0 && n < towerFloors.length ? n : null })
  const allBots = useMemo(() => towerBots(items, now, w as unknown as Record<string, string>), [items, now, w])
  const summary = towerSummary(allBots)
  const waiting = useMemo(() => waitingList(allBots), [allBots])
  const listOpen = waitOpen && waiting.length > 0
  // Each machine's placements, read from its own source. A machine that cannot
  // answer keeps the default rule for its Projects.
  const [docs, setDocs] = useState<ReadonlyMap<string, FloorDoc>>(() => new Map())
  const [floorsOpen, setFloorsOpen] = useState(false)
  const floorSources = (sources ?? []).filter(s => s.canTowerFloors?.() === true)
  const floorSourceKey = floorSources.map(s => `${s.machineID}:${s.authorizationRevision}`).join(",")
  const loadFloors = (source: SourceClient, signal: AbortSignal) => readFloors(source, current ?? (() => sourceRef.current ?? []), towerFloors.length, signal)
    .then(doc => { if (!signal.aborted) setDocs(m => new Map(m).set(doc.machine, doc)) })
  useEffect(() => {
    const c = new AbortController()
    // Re-read when the panel opens, so a choice starts from the machine's floors.
    for (const s of floorSources) void loadFloors(s, c.signal).catch(() => { /* The default rule stands. */ })
    return () => c.abort()
  }, [floorSourceKey, floorsOpen])
  const placed = useMemo(() => placedFloors(docs.values()), [docs])
  const floorOf = useMemo(() => assignFloors(items, towerFloors.length, placed), [items, placed])
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
    setPanelOpen(false)
    setChosenFloor(i); writePref(floorKey, i === Section ? "section" : String(i))
    if (i !== Section) { setSceneFloor(i); writePref(sceneKey, String(i)) }
  }
  // Few Bots share one floor; the building is a choice only when one floor would crowd.
  const canBuild = allBots.length >= TowerBuildingMinBots && new Set(allBots.map(b => projectOf(b.item))).size >= 2
  const building = canBuild && layout === "building"
  const setLayout = (next: TowerLayout) => {
    // Leaving the building for one floor keeps the floor being looked at.
    if (next === "floor" && building && floor !== Section) { setSceneFloor(floor); writePref(sceneKey, String(floor)) }
    setPanelOpen(false)
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
  // A page opened before a deployment still names that deployment's pictures,
  // which the host may already have removed (it answers them with its HTML).
  // The building fetches every floor's picture as soon as it shows, while the
  // names are most likely current, and a picture that still fails says so.
  // The host has also once answered a just-deployed file with that HTML, which
  // the browser may then keep for hours; one fresh request steps past it.
  const [broken, setBroken] = useState<readonly TowerScene["picture"][]>([])
  const [retried, setRetried] = useState<Readonly<Partial<Record<TowerScene["picture"], string>>>>({})
  const retriedRef = useRef(retried); retriedRef.current = retried
  const pictureSrc = (name: TowerScene["picture"]) => retried[name] ? `${pictures[name]}?retry=${retried[name]}` : pictures[name]
  // Only the fresh request's failure is final; a first request failing late changes nothing.
  const pictureFailed = (name: TowerScene["picture"], fresh: boolean) => {
    if (fresh) { setBroken(b => b.includes(name) ? b : [...b, name]); return }
    if (retriedRef.current[name]) return
    retriedRef.current = { ...retriedRef.current, [name]: String(Date.now()) }
    setRetried(retriedRef.current)
  }
  const preloaded = useRef<HTMLImageElement[]>([])
  useEffect(() => {
    if (!building || preloaded.current.length || typeof Image !== "function") return
    preloaded.current = (Object.keys(pictures) as TowerScene["picture"][]).map(name => {
      const img = new Image()
      img.decoding = "async"
      img.onerror = () => pictureFailed(name, false)
      img.src = pictures[name]
      return img
    })
  }, [building])

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
  const helpers = section ? [] : portalHelpers(bots, PortalHelpers)
  const portal = section ? undefined : (scene as TowerScene).portal
  const scenery = useRef({ bots, running, scene, locate, size, helpers, portal, picked })
  scenery.current = { bots, running, scene, locate, size, helpers, portal, picked }
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
    // Layout size, not getBoundingClientRect: the Bots window opens with a
    // scale-in transform, a transformed rect measured mid-animation is a few
    // percent of the real size, and a transform never fires the ResizeObserver,
    // so the canvas stayed tiny and its Bots were drawn as blocks.
    const fit = () => {
      dpr = Math.min(2, window.devicePixelRatio || 1)
      const w = Math.max(1, Math.round(el.clientWidth * dpr)), h = Math.max(1, Math.round(el.clientHeight * dpr))
      if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h }
    }
    drawRef.current = (t: number) => {
      const { bots, running, scene, locate, size, helpers, portal, picked } = scenery.current
      const k = canvas.width / scene.width, still = !running
      g.clearRect(0, 0, canvas.width, canvas.height)
      // Helpers stand on the portal, smaller and fainter than the Bots they work for.
      if (portal) helpers.forEach(({ bot: b }, i) => {
        const [dx, dy] = portalRing[i % portalRing.length]
        drawBot(g, { shape: b.item.bot.appearance?.shape ?? "blob", color: (b.item.bot.appearance?.color ?? "").toLowerCase(), mood: "work", seed: b.seed + i }, (portal[0] + dx) * k, (portal[1] + dy) * k, size * .58 * k, false, t, still, dpr, b.live ? .72 : .4)
      })
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
        if (picked === b.key && cardRef.current) {
          cardRef.current.style.left = `${at[0] / scene.width * 100}%`
          cardRef.current.style.top = `${(at[1] - size - 8) / scene.height * 100}%`
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
  useEffect(() => { if (!running) drawRef.current(performance.now()) }, [bots, slots, running, scene, picked])
  // A phone shows part of the floor: open where the Bots are, busy ones first.
  useEffect(() => {
    const view = viewRef.current
    if (!view || view.scrollWidth <= view.clientWidth + 1 || !bots.length) return
    // Busy Bots first; otherwise the one active most recently, so an all-idle
    // floor never opens on the empty middle between two far-apart Bots.
    const focus = bots.filter(b => b.mood === "wait" || b.mood === "error" || b.mood === "work")
    const recent = [...bots].sort((a, z) => (Date.parse(z.item.activity_at) || 0) - (Date.parse(a.item.activity_at) || 0))
    const pick = focus.length ? focus : recent.slice(0, 1)
    const xs = pick.map(b => locate(b, 0, true).at[0])
    const mid = xs.reduce((a, x) => a + x, 0) / xs.length
    view.scrollLeft = Math.max(0, mid / scene.width * view.scrollWidth - view.clientWidth / 2)
  }, [scene.picture])
  const sectionPct = (x: number, y: number) => ({ left: `${x / sectionScene.width * 100}%`, top: `${y / sectionScene.height * 100}%` })

  // HUD chips: each fact is its own chip, the count picked out as the glowing figure.
  type Chip = { key: string, tone: string, text: string }
  const chips: Chip[] = [{ key: "total", tone: "total", text: w.towerBots.replace("{count}", String(summary.total)) }]
  if (summary.wait) chips.push({ key: "wait", tone: "wait", text: w.towerWaiting.replace("{count}", String(summary.wait)) })
  if (summary.work) chips.push({ key: "work", tone: "work", text: w.towerWorking.replace("{count}", String(summary.work)) })
  if (summary.error) chips.push({ key: "error", tone: "error", text: w.towerErrors.replace("{count}", String(summary.error)) })
  if (summary.helpers) chips.push({ key: "helpers", tone: "work", text: w.towerHelpers.replace("{count}", String(summary.helpers)) })
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
      {summary.total ? chips.map(c => c.key === "wait"
        ? <button key={c.key} type="button" className="pa-hud-chip" data-tone={c.tone} aria-expanded={listOpen} aria-controls="pa-tower-waitlist" onClick={() => { setPanelOpen(false); setFloorsOpen(false); setWaitOpen(o => !o) }}>{figure(c.text)}<i className="pa-hud-caret" aria-hidden="true"/></button>
        : <span key={c.key} className="pa-hud-chip" data-tone={c.tone}>{c.key === "total" && <i className="pa-hud-live" aria-hidden="true"/>}{figure(c.text)}</span>) : <span className="pa-hud-chip" data-tone="idle">{loading ? w.loading : w.towerEmpty}</span>}
    </p>
  </div>
  // Read-only: each row says who waits, where, on what, and since when; it
  // opens that Bot's chat, or rides to its floor when the building shows another.
  const waitList = listOpen && <div id="pa-tower-waitlist" className="pa-tower-waitlist" role="region" aria-label={w.towerWaitList}
    onKeyDown={e => { if (e.key === "Escape") setWaitOpen(false) }}>
    <header><b>{w.towerWaitList}</b><button type="button" aria-label={w.towerWaitListClose} onClick={() => setWaitOpen(false)}>×</button></header>
    <ol>{waiting.map(b => {
      const at = floorOf.get(projectOf(b.item)) ?? 0, project = projectName(b.item) || w.unnamedProject, when = ago(b.item.activity_at, now, language)
      const elsewhere = building && at !== floor
      return <li key={b.key} data-bot-key={b.key}>
        <p><strong>{b.item.bot.name}</strong><span>{building ? `${floors[at].label} · ${project}` : project}</span></p>
        <p>{b.caption && <small>{b.caption}</small>}{when && <time dateTime={b.item.activity_at}>{when}</time>}</p>
        <div>
          {allow && <button type="button" aria-expanded={shown === b.key} onClick={() => setShown(s => s === b.key ? null : b.key)}>{w.towerAllowShow}</button>}
          {shown !== b.key && <button type="button" onClick={() => onSelect(b.item)}>{w.towerOpenChat}</button>}
          {elsewhere && <button type="button" aria-label={w.towerRideTo.replace("{floor}", floors[at].label)} onClick={() => { setWaitOpen(false); goFloor(at) }}>{w.towerRide}</button>}
        </div>
        {allow && shown === b.key && <TowerAllowCard key={b.key} item={b.item} allow={allow} attempted={attempted.current} w={w} onChat={() => onSelect(b.item)}/>}
      </li>
    })}</ol>
  </div>
  const layoutSwitch = <div className="pa-hud-switch" role="group" aria-label={w.towerLayout}>
    <button type="button" aria-pressed={!building} onClick={() => setLayout("floor")}>{w.towerLayoutFloor}</button>
    <button type="button" aria-pressed={building} onClick={() => setLayout("building")}>{w.towerLayoutBuilding}</button>
  </div>
  const floorRows: FloorRow[] = useMemo(() => {
    const seen = new Map<string, FloorRow>()
    for (const item of items) {
      const key = projectOf(item)
      if (seen.has(key)) continue
      const machine = item.bot.key.machine_id, doc = docs.get(machine), writable = items.some(i => i.bot.key.machine_id === machine && canWrite(i.source))
      const supported = floorSources.some(s => s.machineID === machine)
      seen.set(key, { key, machine, project: item.bot.project.project_id, name: projectName(item) || w.unnamedProject, floor: floorOf.get(key) ?? 0,
        placed: placed.has(key), ready: !!doc && writable,
        locked: !supported ? w.towerFloorsUnsupported : !writable ? w.towerFloorsLocked : !doc ? w.loading : undefined })
    }
    // By name, so a row never moves under the finger that just placed it.
    return [...seen.values()].sort((a, z) => a.name.localeCompare(z.name, language) || (a.key < z.key ? -1 : 1))
  }, [items, docs, placed, floorOf, floorSourceKey, w, language])
  const placeRow = async (row: FloorRow, floor: number) => {
    const source = sourceRef.current?.find(s => s.machineID === row.machine), doc = docs.get(row.machine)
    if (!source || !doc) throw new FloorError("refused", "pa_revoked")
    const live = current ?? (() => sourceRef.current ?? [])
    try {
      const next = await placeFloor(source, live, doc, row.project, floor, towerFloors.length, new AbortController().signal)
      setDocs(m => new Map(m).set(next.machine, next))
    } catch (error) {
      // After a stale or unknown outcome the machine's floors decide what shows.
      if (error instanceof FloorError && (error.outcome === "stale" || error.outcome === "unknown")) await loadFloors(source, new AbortController().signal).catch(() => undefined)
      throw error
    }
  }
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
    {floorSources.length > 0 && <button type="button" className="pa-elevator-edit" aria-expanded={floorsOpen} aria-controls="pa-tower-floorset"
      onClick={() => { setWaitOpen(false); setPanelOpen(false); setFloorsOpen(o => !o) }}>{w.towerFloorsEdit}</button>}
  </nav>

  // The tapped Bot's card: what it is doing now, and the way into its chat.
  const pickedBot = bots.find(b => b.key === picked)
  const pickedCard = pickedBot && (() => {
    const b = pickedBot, when = ago(b.item.activity_at, now, language), busy = b.mood === "work" || b.mood === "wait" || b.mood === "error"
    const line = (busy ? b.caption : b.item.latest_message).split("\n").find(l => l.trim()) ?? ""
    return <div ref={cardRef} className="pa-tower-card" data-mood={b.mood} role="dialog" aria-label={b.item.bot.name}
      onKeyDown={e => { if (e.key === "Escape") setPicked(null) }}>
      <header><strong>{b.item.bot.name}</strong><button type="button" aria-label={w.towerCardClose} onClick={() => setPicked(null)}>×</button></header>
      <p className="pa-tower-card-state"><i aria-hidden="true"/>{w[moodWord[b.mood]]}{when && <time dateTime={b.item.activity_at}>{when}</time>}</p>
      <p className="pa-tower-card-project">{projectName(b.item) || w.unnamedProject}</p>
      {line && <p className="pa-tower-card-line">{line}</p>}
      {b.work > 0 && <p className="pa-tower-card-meta">{w.towerCardHelpers.replace("{count}", String(b.work))}</p>}
      {b.unread && <p className="pa-tower-card-meta">{w.towerCardUnread.replace("{count}", String(b.item.unread_count))}</p>}
      {!b.live && <p className="pa-tower-card-meta">{w.towerStale}</p>}
      <button type="button" className="pa-tower-card-open" onClick={() => { setPicked(null); onSelect(b.item) }}>{w.towerOpenChat}</button>
    </div>
  })()
  return <section className="pa-tower" aria-label={w.towerView}>
    <div className="pa-tower-stage">
      <div className="pa-tower-pan">
        {hud}
        {canBuild && <button type="button" className="pa-tower-floorbtn" aria-expanded={panelOpen} aria-label={w.towerFloorsMenu} onClick={() => { setWaitOpen(false); setPanelOpen(o => !o) }}>
          <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2 5.5 8 2.5l6 3-6 3zM2 8.5l6 3 6-3M2 11.5l6 3 6-3"/></svg>
          <b>{!building ? w.towerLayoutFloor : section ? w.towerSection : floors[floor].label}</b>
        </button>}
        {canBuild && <div className="pa-tower-controls" data-open={panelOpen || undefined}>{layoutSwitch}{elevator}</div>}
        <div className="pa-tower-view" ref={viewRef}>
        <div className="pa-tower-scene" ref={sceneRef} data-stale={summary.stale || undefined}
          onClick={e => { if (picked && !(e.target as HTMLElement).closest(".pa-tower-bot,.pa-tower-card")) setPicked(null) }}>
          {!broken.includes(scene.picture) && <img key={`${scene.picture}:${retried[scene.picture] ?? ""}`} src={pictureSrc(scene.picture)} alt="" draggable={false} decoding="async"
            onError={() => pictureFailed(scene.picture, !!retried[scene.picture])}/>}
          {broken.includes(scene.picture) && <div className="pa-tower-fallback pa-tower-picture-missing" role="alert">
            <p>{w.towerPictureMissing}</p>
            <button type="button" className="pa-pill" onClick={() => location.reload()}>{w.towerPictureReload}</button>
          </div>}
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
              aria-expanded={picked === b.key} data-picked={picked === b.key || undefined}
              onClick={() => { if (picked === b.key) { setPicked(null); onSelect(b.item) } else setPicked(b.key) }}>
              <span className="pa-tower-tip" aria-hidden="true">
                <strong>{b.item.bot.name}</strong>
                <span>{[project, mood].filter(Boolean).join(" · ")}</span>
                {b.mood === "work" || b.mood === "wait" || b.mood === "error" ? b.caption && <small>{b.caption}</small> : when && <small>{when}</small>}
              </span>
            </button>
          })}
          {pickedCard}
        </div>
        </div>
        {waitList}
        {building && floorsOpen && <TowerFloorsPanel rows={floorRows} labels={towerFloors.map(f => f.label)} w={w} onPlace={placeRow} onClose={() => setFloorsOpen(false)}/>}
      </div>
    </div>
  </section>
}
