import { useEffect, useLayoutEffect, useRef, type RefObject } from "react"

const EASE = "cubic-bezier(.2, .7, .2, 1)"

/**
 * FLIP for Board cards, the same idea as the Session list's
 * (`Sessions.tsx`, `useListReorderAnimation`) spread over two dimensions and
 * over every region: a card that changes place — within its grid, or into
 * another region when its phase moves — starts where it was and travels to
 * where it is; a card that was not there before fades in.
 *
 * Positions are untransformed layout offsets measured against the Board
 * itself, so a still-running animation or the page's scroll position never
 * feeds into the next move. An unchanged order records its layout and leaves
 * everything alone.
 */
export function useBoardMotion(rootRef: RefObject<HTMLElement | null>): void {
  const previous = useRef<{ order: string[]; places: Map<string, { x: number; y: number }> } | null>(null)
  const animations = useRef(new Map<string, Animation>())

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root) return
    const nodes = [...root.querySelectorAll<HTMLElement>(".work-v2-card[data-work-id]")].filter((node) => node.offsetParent !== null)
    const order = nodes.map((node) => node.dataset.workId ?? "")
    const places = new Map(nodes.map((node) => [node.dataset.workId ?? "", layoutPlace(node, root)]))
    const before = previous.current
    previous.current = { order, places }

    const sameOrder = before?.order.length === order.length && before.order.every((id, index) => id === order[index])
    if (!before || sameOrder || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return

    for (const node of nodes) {
      const id = node.dataset.workId ?? ""
      const from = before.places.get(id)
      const to = places.get(id)!
      let frames: Keyframe[]
      let duration: number
      if (!from) {
        frames = [{ opacity: 0, transform: "translateY(-8px) scale(.98)" }, { opacity: 1, transform: "none" }]
        duration = 280
      } else {
        const dx = from.x - to.x
        const dy = from.y - to.y
        if (Math.abs(dx) < 1 && Math.abs(dy) < 1) continue
        frames = [{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }]
        duration = 360
      }
      animations.current.get(id)?.cancel()
      const animation = node.animate(frames, { duration, easing: EASE })
      animations.current.set(id, animation)
      animation.onfinish = animation.oncancel = () => {
        if (animations.current.get(id) === animation) animations.current.delete(id)
      }
    }
  })

  useEffect(() => () => {
    for (const animation of animations.current.values()) animation.cancel()
    animations.current.clear()
  }, [])
}

/**
 * Where a card sits inside the Board, ignoring any transform on the way:
 * both measured along their offset-parent chains, then subtracted.
 */
function layoutPlace(node: HTMLElement, root: HTMLElement): { x: number; y: number } {
  const card = pageOffset(node)
  const board = pageOffset(root)
  return { x: card.x - board.x, y: card.y - board.y }
}

function pageOffset(node: HTMLElement): { x: number; y: number } {
  let x = 0
  let y = 0
  for (let at: HTMLElement | null = node; at; at = at.offsetParent as HTMLElement | null) {
    x += at.offsetLeft
    y += at.offsetTop
  }
  return { x, y }
}
