import { useEffect, useRef, useState, type RefObject } from "react"
import { Swipes, swipes } from "./swipe.js"

/**
 * Pull to refresh, phones only (`input/edges.js`): with resistance, released
 * past 62px it reads again and holds a 34px pad until the answer is in. The
 * original reloads the page instead when it is stale; this page has no stale
 * notice to answer.
 */
export function usePullToRefresh(
  scrollRef: RefObject<HTMLDivElement | null>,
  padRef: RefObject<HTMLDivElement | null>,
  refresh: () => void | Promise<void>,
  swipe: Swipes = swipes,
): "pull" | "release" | "busy" {
  const [word, setWord] = useState<"pull" | "release" | "busy">("pull")
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh
  useEffect(() => {
    const scroller = scrollRef.current
    const pad = padRef.current
    if (!scroller || !pad) return
    const THRESHOLD = 62
    let startY = 0
    let pulling = false
    let distance = 0
    let busy = false
    let alive = true
    const start = (ev: TouchEvent) => {
      if (busy || scroller.scrollTop > 0 || ev.touches.length !== 1) return
      startY = ev.touches[0].clientY
      pulling = true
      distance = 0
      pad.classList.add("dragging")
    }
    const move = (ev: TouchEvent) => {
      if (!pulling) return
      // One gesture has one owner. The swipe decides the axis from the first
      // real movement, and a gesture it has taken is not also a pull: a
      // diagonal drag used to open the pad and the row at once.
      if (swipe.axis() === "row") {
        distance = 0
        pad.style.height = "0px"
        return
      }
      const raw = ev.touches[0].clientY - startY
      if (raw <= 0) {
        distance = 0
        pad.style.height = "0px"
        return
      }
      distance = Math.min(90, Math.pow(raw, 0.82))
      pad.style.height = distance + "px"
      setWord(distance >= THRESHOLD ? "release" : "pull")
    }
    const end = () => {
      if (!pulling) return
      pulling = false
      pad.classList.remove("dragging")
      if (distance >= THRESHOLD && !busy) {
        busy = true
        setWord("busy")
        pad.style.height = "34px"
        void Promise.resolve().then(() => refreshRef.current()).catch(() => undefined).then(() => {
          setTimeout(() => {
            if (!alive) return
            pad.style.height = "0px"
            setWord("pull")
            busy = false
          }, 260)
        })
      } else {
        pad.style.height = "0px"
      }
    }
    scroller.addEventListener("touchstart", start, { passive: true })
    scroller.addEventListener("touchmove", move, { passive: true })
    scroller.addEventListener("touchend", end, { passive: true })
    scroller.addEventListener("touchcancel", end, { passive: true })
    return () => {
      alive = false
      scroller.removeEventListener("touchstart", start)
      scroller.removeEventListener("touchmove", move)
      scroller.removeEventListener("touchend", end)
      scroller.removeEventListener("touchcancel", end)
    }
  }, [scrollRef, padRef, swipe])
  return word
}
