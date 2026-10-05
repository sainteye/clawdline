import { useEffect, useRef } from "react"
import * as L from "../legacy/bridge.js"
import type { ProjectPlace } from "../pages/work/api.js"

/** The terminal's own mark, below the project's mark in a card. */
export function TerminalGlyph() {
  return <span className="session-terminal-icon" aria-hidden="true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="m7 9 3 3-3 3m6 0h4" />
    </svg>
  </span>
}

export function TerminalMarks({ place }: { place?: ProjectPlace }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    if (canvas.current && place?.icon) L.drawIcon(canvas.current, place.icon as L.StartPlaceRow["icon"], 4)
  }, [place?.icon])
  return <span className="session-terminal-marks" aria-hidden="true">
    {Boolean(place?.icon) && <span className="session-terminal-project"><canvas ref={canvas} /></span>}
    <TerminalGlyph />
  </span>
}
