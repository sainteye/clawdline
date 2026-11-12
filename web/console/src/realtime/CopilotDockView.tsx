import { useCallback, useEffect, useRef, useState } from "react"
import type { PointerEvent as ReactPointerEvent } from "react"
import { nextWord } from "../next-strings.js"
import { realtimeCopilot, type RealtimeCopilotState } from "./realtime-copilot.js"
import { RealtimeCopilotModal } from "./RealtimeCopilotModal.js"
import { CopilotNotice } from "./CopilotNotice.js"
import { DevStackModal } from "./DevStackModal.js"
import { bubbleSide, copilotDock, firstPoint, isTap, restingPoint, type DockMode, type Insets, type Point } from "./copilot-dock.js"
import "./realtime.css"

const POSITION_KEY = "clawdline.copilot.bubble"

function readInsets(): Insets {
  // The safe-area insets are only readable through a style that uses them.
  const probe = document.createElement("div")
  probe.style.cssText = "position:fixed;visibility:hidden;pointer-events:none;" +
    "padding:env(safe-area-inset-top,0px) env(safe-area-inset-right,0px) env(safe-area-inset-bottom,0px) env(safe-area-inset-left,0px)"
  document.body.appendChild(probe)
  const s = getComputedStyle(probe)
  const insets = { top: parseFloat(s.paddingTop) || 0, right: parseFloat(s.paddingRight) || 0,
    bottom: parseFloat(s.paddingBottom) || 0, left: parseFloat(s.paddingLeft) || 0 }
  probe.remove()
  return insets
}

function viewport() {
  return { width: window.innerWidth, height: window.innerHeight }
}

function savedPoint(): Point | null {
  try {
    const raw = localStorage.getItem(POSITION_KEY)
    if (!raw) return null
    const p = JSON.parse(raw) as Partial<Point>
    return typeof p.x === "number" && typeof p.y === "number" ? { x: p.x, y: p.y } : null
  } catch {
    // refusal-ok: a browser that keeps no site data starts the bubble at its default place.
    return null
  }
}

function savePoint(p: Point) {
  try {
    localStorage.setItem(POSITION_KEY, JSON.stringify(p))
  } catch {
    // refusal-ok: the position is a convenience; the bubble still works without it.
  }
}

/**
 * The voice copilot's on-screen presence for the whole page: its sheet, or a
 * bubble that stays above every page while the call goes on. Mounted once at
 * the root, so opening a Session or another page never takes the call away.
 */
export function CopilotDockView() {
  const [mode, setMode] = useState<DockMode>(copilotDock.get())
  const [call, setCall] = useState<RealtimeCopilotState>(realtimeCopilot.getState())
  useEffect(() => copilotDock.subscribe(setMode), [])
  useEffect(() => realtimeCopilot.subscribe((s) => {
    setCall(s)
    if (s.status === "disconnected" || s.status === "error") copilotDock.callEnded()
  }), [])
  const inCall = call.status === "connected" || call.status === "connecting"
  return <>
    <RealtimeCopilotModal open={mode === "sheet"} onClose={() => copilotDock.close()}
      onMinimize={() => copilotDock.minimize(inCall)} />
    {mode === "bubble" && inCall ? <CopilotBubble call={call} /> : null}
    <DevStackModal />
  </>
}

function CopilotBubble({ call }: { call: RealtimeCopilotState }) {
  const [at, setAt] = useState<Point>(() => {
    const insets = readInsets()
    const saved = savedPoint()
    return saved ? restingPoint(saved, viewport(), insets) : firstPoint(viewport(), insets)
  })
  const [dragging, setDragging] = useState(false)
  const press = useRef<{ id: number; from: Point; offset: Point; moved: boolean } | null>(null)

  // A rotated phone or a resized window keeps the bubble on screen.
  useEffect(() => {
    const keep = () => setAt((p) => restingPoint(p, viewport(), readInsets()))
    window.addEventListener("resize", keep)
    return () => window.removeEventListener("resize", keep)
  }, [])

  const onDown = useCallback((e: ReactPointerEvent<HTMLButtonElement>) => {
    if (e.button !== 0) return
    e.currentTarget.setPointerCapture(e.pointerId)
    press.current = { id: e.pointerId, from: { x: e.clientX, y: e.clientY }, offset: { x: e.clientX - at.x, y: e.clientY - at.y }, moved: false }
  }, [at])

  const onMove = useCallback((e: ReactPointerEvent<HTMLButtonElement>) => {
    const p = press.current
    if (!p || p.id !== e.pointerId) return
    if (!p.moved && isTap(p.from, { x: e.clientX, y: e.clientY })) return
    if (!p.moved) { p.moved = true; setDragging(true) }
    setAt({ x: e.clientX - p.offset.x, y: e.clientY - p.offset.y })
  }, [])

  const onUp = useCallback((e: ReactPointerEvent<HTMLButtonElement>) => {
    const p = press.current
    if (!p || p.id !== e.pointerId) return
    press.current = null
    if (!p.moved) { copilotDock.open(); return }
    setDragging(false)
    setAt((cur) => {
      const rest = restingPoint(cur, viewport(), readInsets())
      savePoint(rest)
      return rest
    })
  }, [])

  const onCancel = useCallback(() => {
    press.current = null
    setDragging(false)
    setAt((cur) => restingPoint(cur, viewport(), readInsets()))
  }, [])

  const speaking = call.status === "connected" ? call.speakingState : "connecting"
  return <div className={`copilot-bubble side-${bubbleSide(at, viewport())}${dragging ? " dragging" : ""}`} style={{ transform: `translate3d(${at.x}px, ${at.y}px, 0)` }}>
    <button type="button" className={`copilot-bubble-body state-${speaking}`} aria-label={nextWord("copilotExpand")}
      title={nextWord("copilotExpand")} onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onCancel}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); copilotDock.open() } }}>
      <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
        <path d="M6.62 10.79a15.05 15.05 0 006.59 6.59l2.2-2.2a1 1 0 011.02-.24c1.12.37 2.33.57 3.57.57a1 1 0 011 1V20a1 1 0 01-1 1A17 17 0 013 4a1 1 0 011-1h3.5a1 1 0 011 1c0 1.25.2 2.45.57 3.57a1 1 0 01-.25 1.02l-2.2 2.2z"/>
      </svg>
    </button>
    <CopilotNotice notice={call.notice} className="copilot-notice copilot-bubble-notice" />
    <button type="button" className="copilot-bubble-hangup" aria-label={nextWord("copilotHangup")} title={nextWord("copilotHangup")}
      onClick={() => { realtimeCopilot.disconnect(); copilotDock.close() }}>
      <svg viewBox="0 0 12 12" aria-hidden="true" focusable="false"><path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" /></svg>
    </button>
  </div>
}
