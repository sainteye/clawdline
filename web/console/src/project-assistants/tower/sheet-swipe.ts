import { useRef, type PointerEvent as ReactPointerEvent } from "react"

/** How far, in CSS pixels, a released sheet must have moved to change; a
 * quick flick changes it sooner. Gesture tuning, not a capacity bound. */
export const SheetCloseDistance = 100
const FlickDistance = 36, FlickSpeed = 0.6 // px per ms

/** What a released drag does. A half sheet swiped up fills the screen and
 * swiped down closes; a full one swiped down returns to half. */
export type SheetRelease = "close" | "collapse" | "expand" | "stay"

const far = (d: number, ms: number) => d >= SheetCloseDistance || (d >= FlickDistance && ms > 0 && d / ms >= FlickSpeed)

/** `dy` is how far the sheet moved down (negative is up) over `ms`. */
export function sheetRelease(dy: number, ms: number, full: boolean): SheetRelease {
  if (!Number.isFinite(dy) || dy === 0) return "stay"
  if (dy > 0) return far(dy, ms) ? (full ? "collapse" : "close") : "stay"
  return !full && far(-dy, ms) ? "expand" : "stay"
}

/** Kept for the close rule's own tests: a half sheet released `dy` down. */
export function sheetShouldClose(dy: number, ms: number): boolean {
  return sheetRelease(dy, ms, false) === "close"
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
 * Swipe a bottom sheet: it follows the finger, and on release closes,
 * collapses, expands or springs back as sheetRelease says. An upward drag of a
 * full sheet and any drag while `enabled` is false do nothing.
 */
export function useSheetSwipe(enabled: boolean, full: boolean, act: (release: Exclude<SheetRelease, "stay">) => void) {
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
      // Down follows the finger; up, a half sheet rises a little to show it will grow.
      const raw = e.clientY - d.y
      d.dy = full ? Math.max(0, raw) : raw
      const shown = d.dy >= 0 ? d.dy : Math.max(d.dy, -60) * .5
      e.currentTarget.style.transform = shown ? `translateY(${shown}px)` : ""
    },
    onPointerUp(e: ReactPointerEvent<HTMLElement>) {
      const d = drag.current
      if (!d || d.id !== e.pointerId) return
      drag.current = null
      const el = e.currentTarget, release = sheetRelease(d.dy, e.timeStamp - d.at, full)
      if (release !== "stay") { settle(el); act(release) }
      else { el.style.transition = "transform .18s ease-out"; el.style.transform = ""; setTimeout(() => settle(el), 200) }
    },
    onPointerCancel(e: ReactPointerEvent<HTMLElement>) {
      if (drag.current?.id !== e.pointerId) return
      drag.current = null
      settle(e.currentTarget)
    },
  }
}
