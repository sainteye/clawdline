import { useRef, type PointerEvent as ReactPointerEvent } from "react"

/** How far down, in CSS pixels, a released sheet closes; a quick flick closes
 * sooner. Gesture tuning, not a capacity bound. */
export const SheetCloseDistance = 100
const FlickDistance = 36, FlickSpeed = 0.6 // px per ms

/** Whether a sheet dragged down by `dy` pixels over `ms` closes when released. */
export function sheetShouldClose(dy: number, ms: number): boolean {
  if (!(dy > 0)) return false
  return dy >= SheetCloseDistance || (dy >= FlickDistance && ms > 0 && dy / ms >= FlickSpeed)
}

/** Where a drag may start: the grip, or the chat header's empty space, never
 * one of its controls. */
const startsDrag = (target: EventTarget | null) => {
  const el = target instanceof Element ? target : null
  if (!el) return false
  if (el.closest(".pa-sheet-grip")) return true
  return !!el.closest(".pa-chat-header") && !el.closest("button,a,input,textarea,select,[role='button']")
}

/**
 * Swipe a bottom sheet down to close it. The sheet follows the finger; released
 * far enough or fast enough it closes, otherwise it springs back. Only a
 * downward drag moves it, and nothing happens while `enabled` is false.
 */
export function useSheetSwipe(enabled: boolean, onClose: () => void) {
  const drag = useRef<{ id: number, y: number, at: number, dy: number } | null>(null)
  const settle = (el: HTMLElement) => { el.style.transition = ""; el.style.transform = "" }
  return {
    onPointerDown(e: ReactPointerEvent<HTMLElement>) {
      if (!enabled || !e.isPrimary || drag.current || !startsDrag(e.target)) return
      drag.current = { id: e.pointerId, y: e.clientY, at: e.timeStamp, dy: 0 }
      e.currentTarget.setPointerCapture?.(e.pointerId)
      e.currentTarget.style.transition = "none"
    },
    onPointerMove(e: ReactPointerEvent<HTMLElement>) {
      const d = drag.current
      if (!d || d.id !== e.pointerId) return
      d.dy = Math.max(0, e.clientY - d.y)
      e.currentTarget.style.transform = d.dy ? `translateY(${d.dy}px)` : ""
    },
    onPointerUp(e: ReactPointerEvent<HTMLElement>) {
      const d = drag.current
      if (!d || d.id !== e.pointerId) return
      drag.current = null
      const el = e.currentTarget
      if (sheetShouldClose(d.dy, e.timeStamp - d.at)) { settle(el); onClose() }
      else { el.style.transition = "transform .18s ease-out"; el.style.transform = ""; setTimeout(() => settle(el), 200) }
    },
    onPointerCancel(e: ReactPointerEvent<HTMLElement>) {
      if (drag.current?.id !== e.pointerId) return
      drag.current = null
      settle(e.currentTarget)
    },
  }
}
