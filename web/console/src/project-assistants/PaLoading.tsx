import { useEffect, useState } from "react"
import "../loading-skeleton.css"
import { showAfter, SkeletonDelayMs } from "./loading"

/** True once `waiting` has lasted `delay`; false again as soon as it stops. */
export function useShowAfter(waiting: boolean, delay = SkeletonDelayMs): boolean {
  const [shown, setShown] = useState(false)
  useEffect(() => {
    const gate = showAfter(delay, setShown)
    gate.set(waiting)
    return gate.stop
  }, [waiting, delay])
  return waiting && shown
}

/** Alternating reply and own-message shapes; only the label is readable, to assistive technology. */
const chatShapes: readonly { mine: boolean; widths: readonly number[] }[] = [
  { mine: false, widths: [82, 64, 46] },
  { mine: true, widths: [100, 58] },
  { mine: false, widths: [74, 52] },
  { mine: true, widths: [100] },
]

export function ChatSkeleton({ label }: { label: string }) {
  return <div className="pa-skeleton pa-chat-skeleton" role="status" data-loading-id="bots-chat">
    <span className="loading-skeleton-label">{label}</span>
    <div aria-hidden="true">{chatShapes.map((shape, index) => <div className={`pa-skeleton-bubble${shape.mine ? " pa-skeleton-mine" : ""}`} key={index}>
      {shape.widths.map((width, line) => <i className="page-skeleton-line" key={line} style={{ width: `${width}%` }} />)}
    </div>)}</div>
  </div>
}
