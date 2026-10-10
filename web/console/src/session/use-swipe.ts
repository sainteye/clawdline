import { useEffect, useSyncExternalStore, type RefObject } from "react"
import { actionWidthOf, paintSwipe } from "./List.js"
import type { Swipes } from "./swipe.js"

export function useSwipeToEnd(scrollRef: RefObject<HTMLDivElement | null>, swipes: Swipes): string | null {
  const swiped = useSyncExternalStore(swipes.subscribe, swipes.openId, swipes.openId)
  useEffect(() => {
    const scroller = scrollRef.current
    if (!scroller) return
    let node: HTMLElement | null = null
    const rowAt = (target: EventTarget | null): HTMLElement | null => {
      const el = target instanceof Element ? target.closest<HTMLElement>("li.row") : null
      return el && scroller.contains(el) && el.querySelector(".swipe-end") ? el : null
    }
    /** The row is ready to be dragged before it is dragged; see above. */
    const arm = (el: HTMLElement) => {
      if (!el.dataset.swipe) el.dataset.swipe = "dragging"
    }
    const paint = (el: HTMLElement, id: string) => {
      paintSwipe(el, swipes.stateOf(id), swipes.offsetOf(id))
    }
    const start = (ev: TouchEvent) => {
      if (ev.touches.length !== 1) return
      const el = rowAt(ev.target)
      const id = el?.dataset.id ?? null
      const width = el ? actionWidthOf(el) : undefined
      const closed = swipes.begin(id, ev.touches[0].clientX, ev.touches[0].clientY, ev.timeStamp, width)
      if (closed) {
        for (const other of scroller.querySelectorAll<HTMLElement>("li.row[data-swipe]")) paintSwipe(other, "", 0)
        node = null
        return
      }
      node = el
      if (el) arm(el)
    }
    const move = (ev: TouchEvent) => {
      if (!node || ev.touches.length !== 1) return
      const axis = swipes.move(ev.touches[0].clientX, ev.touches[0].clientY, ev.timeStamp)
      if (axis === "list") {
        // The scroller's after all: put the row back exactly as it was, so a
        // scroll that started over a row leaves no trace of having been armed.
        paintSwipe(node, swipes.stateOf(node.dataset.id ?? ""), swipes.offsetOf(node.dataset.id ?? ""))
        node = null
        return
      }
      if (axis !== "row") return
      // The order is frozen by the list's own `touchstart` listener; this only
      // has to not fight it.
      paint(node, node.dataset.id ?? "")
    }
    const end = () => {
      const settled = swipes.end()
      const el = node
      node = null
      if (!settled || !el) return
      paint(el, settled.id)
    }
    scroller.addEventListener("touchstart", start, { passive: true })
    scroller.addEventListener("touchmove", move, { passive: true })
    scroller.addEventListener("touchend", end, { passive: true })
    scroller.addEventListener("touchcancel", end, { passive: true })
    return () => {
      scroller.removeEventListener("touchstart", start)
      scroller.removeEventListener("touchmove", move)
      scroller.removeEventListener("touchend", end)
      scroller.removeEventListener("touchcancel", end)
      swipes.closeOpen()
    }
  }, [scrollRef])
  return swiped
}
