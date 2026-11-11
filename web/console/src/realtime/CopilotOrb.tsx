import React, { useEffect, useRef, useState } from "react"
import { nextWord } from "../next-strings.js"
import { createOrbRenderer, ORB_PHASE_PERIOD, type OrbRenderer } from "./copilot-orb-gl.js"
import { QUIET, easeLook, frameDue, orbLook, settled, smoothLevel, type OrbLevels, type OrbMode } from "./copilot-orb.js"

interface Props {
  mode: OrbMode
  /** How loud each side of the call is right now; absent outside a call. */
  levels?: () => OrbLevels
}

/**
 * The voice copilot's blue cloud orb. WebGL draws it when it can; otherwise
 * layered CSS clouds stand in. Either way it is drawn at most ORB_MAX_FPS
 * times a second, not at all while the page is hidden, and stops once a
 * still state has settled.
 */
export function CopilotOrb({ mode, levels }: Props) {
  const wrap = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [drawn, setDrawn] = useState<"webgl" | "css" | null>(null)
  const modeNow = useRef(mode)
  modeNow.current = mode
  const levelsNow = useRef(levels)
  levelsNow.current = levels
  const wake = useRef<() => void>(() => undefined)

  useEffect(() => { wake.current() }, [mode, levels])

  useEffect(() => {
    const box = wrap.current
    if (!box) return
    let renderer: OrbRenderer | null = canvas.current ? createOrbRenderer(canvas.current) : null
    setDrawn(renderer ? "webgl" : "css")
    const motion = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-reduced-motion: reduce)") : null
    let still = !!motion?.matches
    let heard: OrbLevels = QUIET
    let look = orbLook(modeNow.current, QUIET, still)
    let phase = Math.random() * ORB_PHASE_PERIOD
    let frame = 0
    let last = 0

    const tick = (now: number) => {
      frame = 0
      if (document.hidden) return
      if (last && !frameDue(last, now)) { frame = requestAnimationFrame(tick); return }
      const dt = last ? Math.min((now - last) / 1000, 0.1) : 0
      last = now
      const raw = levelsNow.current?.() ?? QUIET
      heard = { input: smoothLevel(heard.input, raw.input, dt), output: smoothLevel(heard.output, raw.output, dt) }
      const target = orbLook(modeNow.current, heard, still)
      look = easeLook(look, target, dt)
      phase = (phase + look.speed * dt) % ORB_PHASE_PERIOD
      if (renderer) renderer.draw(look, phase)
      box.style.setProperty("--orb-swell", look.swell.toFixed(3))
      box.style.setProperty("--orb-glow", look.glow.toFixed(3))
      box.style.setProperty("--orb-bright", look.bright.toFixed(3))
      const listening = !!levelsNow.current && modeNow.current !== "idle"
      if (listening || !settled(look, target)) frame = requestAnimationFrame(tick)
    }
    const start = () => {
      if (frame || document.hidden) return
      last = 0
      frame = requestAnimationFrame(tick)
    }
    const stop = () => {
      if (frame) cancelAnimationFrame(frame)
      frame = 0
    }
    const onVisibility = () => { if (document.hidden) stop(); else start() }
    const onMotion = () => { still = !!motion?.matches; start() }
    const onLost = (e: Event) => {
      e.preventDefault()
      renderer = null
      setDrawn("css")
    }
    wake.current = start
    document.addEventListener("visibilitychange", onVisibility)
    motion?.addEventListener?.("change", onMotion)
    canvas.current?.addEventListener("webglcontextlost", onLost)
    const drawnOn = canvas.current
    start()
    return () => {
      stop()
      wake.current = () => undefined
      document.removeEventListener("visibilitychange", onVisibility)
      motion?.removeEventListener?.("change", onMotion)
      drawnOn?.removeEventListener("webglcontextlost", onLost)
      renderer?.dispose()
    }
  }, [])

  return <div ref={wrap} className={`realtime-orb orb-${mode}${drawn === "css" ? " orb-css" : ""}`} role="img" aria-label={nextWord("copilotOrb")}>
    <canvas ref={canvas} className="orb-canvas" aria-hidden="true" />
    {drawn === "css" ? <div className="orb-css-stage" aria-hidden="true">
      <div className="orb-halo" />
      <div className="orb-ball">
        <div className="orb-cloud orb-cloud-1" />
        <div className="orb-cloud orb-cloud-2" />
        <div className="orb-cloud orb-cloud-3" />
        <div className="orb-gloss" />
      </div>
    </div> : null}
  </div>
}
