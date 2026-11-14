import { useEffect, useState } from "react"
import { nextWord } from "../next-strings.js"
import { finishFirstTour, firstTourDone, placePresence, sessionsStarted, useFirstRun } from "./first-run.js"
import { Start } from "./Start.js"
import "./first-run.css"

type Box = { left: number; top: number; width: number; height: number }
const CARD_WIDTH = 300
const GAP = 14
const EDGE = 16

/** Where the card goes: beside the target when it fits, otherwise below or above it. */
function cardAt(target: Box, cardHeight: number): { left: number; top: number } {
  const vw = window.innerWidth
  const vh = window.innerHeight
  const width = Math.min(CARD_WIDTH, vw - EDGE * 2)
  if (target.left + target.width + GAP + width <= vw - EDGE) {
    return { left: target.left + target.width + GAP, top: Math.max(EDGE, Math.min(target.top, vh - cardHeight - EDGE)) }
  }
  const left = Math.max(EDGE, Math.min(target.left, vw - width - EDGE))
  if (target.top + target.height + GAP + cardHeight <= vh - EDGE) return { left, top: target.top + target.height + GAP }
  return { left, top: Math.max(EDGE, target.top - GAP - cardHeight) }
}

/**
 * The one-time, two-step tour for a person who has a Project and has never
 * started a Session: where the + is, then that a Project press starts one.
 * It follows the real elements, lets presses through to them, and is never
 * shown again once skipped, finished, or made moot by a first start.
 */
export function FirstTour({ sessionCount, shown }: { sessionCount: number | null; shown: boolean }) {
  useFirstRun()
  const eligible = shown && sessionCount === 0 && placePresence() === "some" && sessionsStarted() === 0 && !firstTourDone()
  const [step, setStep] = useState<0 | 1>(0)
  const [target, setTarget] = useState<Box | null>(null)

  useEffect(() => {
    if (!eligible) return
    let frame = 0
    const follow = () => {
      const open = Start.isOpen()
      const next: 0 | 1 = open ? 1 : 0
      setStep((s) => (s === next ? s : next))
      const node = document.getElementById(open ? "start-list" : "start-go")
      const rect = node && node.getClientRects().length ? node.getBoundingClientRect() : null
      setTarget((old) => {
        if (!rect) return null
        const box = { left: rect.left, top: rect.top, width: rect.width, height: rect.height }
        return old && old.left === box.left && old.top === box.top && old.width === box.width && old.height === box.height ? old : box
      })
      frame = requestAnimationFrame(follow)
    }
    frame = requestAnimationFrame(follow)
    return () => cancelAnimationFrame(frame)
  }, [eligible])

  if (!eligible || !target) return null
  const pad = 5
  const cardHeight = 170
  const at = cardAt(target, cardHeight)
  const first = step === 0
  return (
    <div className="first-tour" role="dialog" aria-modal="false" aria-labelledby="first-tour-title">
      <div className="first-tour-ring" style={{ left: target.left - pad, top: target.top - pad, width: target.width + pad * 2, height: target.height + pad * 2 }} />
      <div className="first-tour-card" style={{ left: at.left, top: at.top }}>
        <span className="first-tour-step">{nextWord("tourStep", { step: step + 1, count: 2 })}</span>
        <h2 id="first-tour-title">{nextWord(first ? "tourStartTitle" : "tourProjectTitle")}</h2>
        <p>{nextWord(first ? "tourStartBody" : "tourProjectBody")}</p>
        <div className="first-tour-foot">
          <span className="first-tour-dots" aria-hidden="true"><i data-on={first ? "" : undefined} /><i data-on={first ? undefined : ""} /></span>
          <span className="first-tour-actions">
            {first && <button className="chip" type="button" onClick={() => finishFirstTour()}>{nextWord("tourSkip")}</button>}
            <button className="chip home-hero-primary" type="button" onClick={() => first ? Start.open() : finishFirstTour()}>
              {nextWord(first ? "tourNext" : "tourDone")}
            </button>
          </span>
        </div>
      </div>
    </div>
  )
}
