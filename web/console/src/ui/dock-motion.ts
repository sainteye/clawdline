import { useEffect, useRef, type RefObject } from "react"

// Match the Bots window's expansion and fold, anchored to the actual opener.
export function useDockMotion(ref: RefObject<HTMLElement | null>) {
  const animation = useRef<Animation | null>(null)
  const closing = useRef(false)
  const origin = useRef({ x: window.innerWidth, y: window.innerHeight })
  useEffect(() => () => { animation.current?.cancel() }, [])
  const capture = (opener?: HTMLElement | null, fallbackX = window.innerWidth) => {
    const box = opener?.getBoundingClientRect()
    origin.current = box?.width && box.height ? { x: box.left + box.width / 2, y: box.top + box.height / 2 }
      : { x: fallbackX, y: window.innerHeight }
  }
  const play = (out: boolean, done?: () => void) => {
    const node = ref.current
    if (!node || matchMedia("(prefers-reduced-motion: reduce)").matches) { done?.(); return }
    const box = node.getBoundingClientRect()
    node.style.transformOrigin = `${origin.current.x - box.left}px ${origin.current.y - box.top}px`
    const folded = { opacity: 0, transform: "scale(.06)", borderRadius: "40px" }
    const expanded = { opacity: 1, transform: "none", borderRadius: getComputedStyle(node).borderRadius }
    animation.current?.cancel()
    const next = node.animate(out ? [expanded, folded] : [folded, expanded], {
      duration: out ? 200 : 220, easing: out ? "cubic-bezier(.4,0,.8,.4)" : "cubic-bezier(.2,.8,.2,1)", fill: "both",
    })
    animation.current = next
    next.onfinish = () => { if (out) done?.(); else next.cancel() }
  }
  return {
    capture,
    open: () => { closing.current = false; play(false) },
    close: (done: () => void) => {
      if (closing.current) return
      closing.current = true
      play(true, done)
    },
  }
}
