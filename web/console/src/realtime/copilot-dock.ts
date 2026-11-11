/**
 * Where the voice copilot is on screen: closed, its full sheet, or a small
 * bubble the person drags aside while the call goes on.
 *
 * The call itself is `realtimeCopilot`, one per page; this is only its
 * presentation, held outside any page so the bubble stays while the person
 * opens Sessions, scrolls or switches pages. Nothing is imported here, so
 * `node --test` loads it as it is.
 */

export type DockMode = 'closed' | 'sheet' | 'bubble'

type Listener = (mode: DockMode) => void

export class CopilotDock {
  private mode: DockMode = 'closed'
  private listeners = new Set<Listener>()

  get(): DockMode {
    return this.mode
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener)
    listener(this.mode)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private set(mode: DockMode) {
    if (mode === this.mode) return
    this.mode = mode
    for (const l of this.listeners) l(mode)
  }

  /** The toolbar's button and a press on the bubble both open the sheet. */
  open() {
    this.set('sheet')
  }

  /** Shrinks the sheet to the bubble; only a live call has a bubble. */
  minimize(inCall: boolean) {
    this.set(inCall ? 'bubble' : 'closed')
  }

  close() {
    this.set('closed')
  }

  /** A call that ends while shrunk leaves nothing on screen to point at it. */
  callEnded() {
    if (this.mode === 'bubble') this.set('closed')
  }
}

export const copilotDock = new CopilotDock()

export interface Insets {
  top: number
  right: number
  bottom: number
  left: number
}

export interface Point {
  x: number
  y: number
}

/** The bubble's diameter and its distance from the screen's edges, in CSS px. */
export const BUBBLE_SIZE = 60
export const BUBBLE_MARGIN = 12

/**
 * Where a released bubble comes to rest: against the nearer side, like a
 * messaging app's call bubble, and never under the phone's bars.
 */
export function restingPoint(at: Point, viewport: { width: number; height: number }, insets: Insets): Point {
  const minX = insets.left + BUBBLE_MARGIN
  const maxX = Math.max(minX, viewport.width - insets.right - BUBBLE_MARGIN - BUBBLE_SIZE)
  const minY = insets.top + BUBBLE_MARGIN
  const maxY = Math.max(minY, viewport.height - insets.bottom - BUBBLE_MARGIN - BUBBLE_SIZE)
  const centre = at.x + BUBBLE_SIZE / 2
  return {
    x: centre < viewport.width / 2 ? minX : maxX,
    y: Math.min(maxY, Math.max(minY, at.y)),
  }
}

/** Where the bubble first appears: the right side, a third of the way down. */
export function firstPoint(viewport: { width: number; height: number }, insets: Insets): Point {
  return restingPoint({ x: viewport.width, y: Math.round(viewport.height / 3) }, viewport, insets)
}

/** A press that moved less than this is a tap that opens the sheet, not a drag. */
export const TAP_SLOP = 8

export function isTap(from: Point, to: Point): boolean {
  return Math.hypot(to.x - from.x, to.y - from.y) < TAP_SLOP
}
