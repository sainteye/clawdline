/*
 * Swiping a row left on a phone, and what the swipe is allowed to do.
 *
 * The Swift app never had this: its `Resources/web/js/` binds no `touchstart`
 * at all. What it did leave is the look — `.row[data-swipe]`, `--swipe-x`,
 * `--swipe-button-x` and `.swipe-end` are in `legacy/responsive.css`, copied
 * byte for byte with the rest — so the appearance is the original's and only
 * the gesture is new. Nothing here draws anything; `Sessions.tsx` does, against
 * those same custom properties.
 *
 * **The swipe does not close anything.** It uncovers a control, and the control
 * opens the confirmation every other close in this console goes through. The
 * close is a ladder that types the assistant's own exit word, waits for it to
 * leave, then SIGTERM, then SIGKILL (`internal/adapters/terminal/farewell.go`),
 * and a gesture is the wrong amount of intent for the top of that ladder. It is
 * the shape the machine question takes (`cloud/CloudGate.tsx`): a dialog that
 * names what it would act on, with the safe answer under the focus.
 *
 * Three things this had to be written around, all of them already on the same
 * scroller (`Sessions.tsx`):
 *
 * - **Pull to refresh.** Vertical, from the top, `{passive: true}`.
 * - **The order hold.** A finger on the list freezes the sort, so a row does
 *   not move out from under it.
 * - **A passive listener cannot `preventDefault`**, so a horizontal drag cannot
 *   be stopped from scrolling that way. It does not have to be: the copied
 *   `.row[data-swipe]` carries `touch-action: pan-y`, which tells the browser
 *   the row pans vertically and nothing else, so the browser never takes a
 *   horizontal drag for a scroll and hands it to the page. That is why every
 *   listener here stays passive and the attribute is put on the row before the
 *   first move is read (`Sessions.tsx`, `arm`).
 *
 * The two gestures are told apart the usual way: whichever axis the first real
 * movement is on owns the rest of that gesture, and the other one is told to
 * keep out of it (`Axis`). Below `AXIS_SLOP` neither owns it, because a finger
 * that has barely moved has not said anything yet.
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */

/** The uncovered width: `--swipe-action-w` in `legacy/responsive.css`. */
export const ACTION_WIDTH = 126

/** How far a finger travels before the gesture says which axis it is. */
export const AXIS_SLOP = 10

/** Past this much of the action, letting go leaves it open. */
export const OPEN_AT = 0.45

/** A flick decides whichever way it is going, however short: px per ms. */
export const FLICK = 0.45

/** How much of a pull past the action's own width is given back, so the edge is felt. */
export const STRETCH = 0.32

/** Which gesture this is. `list` is the scroller's — pull to refresh, or a plain scroll. */
export type Axis = "undecided" | "row" | "list"

/** What a row is doing, as `data-swipe` spells it. `""` is no attribute at all. */
export type SwipeState = "" | "dragging" | "open"

/**
 * How far left the contents have moved for a finger that has travelled `dx`
 * from where it went down, with `base` already uncovered.
 *
 * Rightwards past closed is nothing: there is no action on that side, and a row
 * that slides right would uncover the edge of the list. Past the action's own
 * width the row gives a little and then stops, which is the difference between
 * an edge and a wall.
 */
export function offsetFor(base: number, dx: number): number {
  const raw = base - dx
  if (raw <= 0) return 0
  if (raw <= ACTION_WIDTH) return raw
  return ACTION_WIDTH + Math.pow(raw - ACTION_WIDTH, STRETCH * 2) * STRETCH
}

/**
 * Where the row settles when the finger leaves: open, or closed.
 *
 * A flick is answered by its direction whatever the distance — that is what a
 * flick is for — and anything slower is answered by how far it got.
 */
export function settleOpen(offset: number, velocity: number): boolean {
  if (velocity <= -FLICK) return true
  if (velocity >= FLICK) return false
  return offset >= ACTION_WIDTH * OPEN_AT
}

/** A closeability as the copied projection answers it (`view/derive.js`). */
export interface Projected {
  state: string
  reasons: readonly { kind: string; code: string }[]
}

/** The action the uncovered control names, in the reader's language. */
export interface RevealWords {
  end: string
}

/**
 * What the uncovered control says for one row.
 *
 * Pressing this control always opens the close confirmation; it never performs
 * the close. Its label therefore stays the action in all four states. The row's
 * state line already says whether closing is safe, blocked, awaiting an
 * attestation, or unknown, and the confirmation names the complete reasons.
 */
export interface Reveal {
  /** `data-closeability` on the control. */
  state: "safe" | "blocked" | "needs_attestation" | "unknown"
  word: string
}

export function revealFor(projected: Projected, words: RevealWords): Reveal {
  const state = projected.state
  if (state === "safe" || state === "blocked" || state === "needs_attestation") return { state, word: words.end }
  return { state: "unknown", word: words.end }
}

interface Drag {
  id: string
  x0: number
  y0: number
  /** What was already uncovered when the finger went down. */
  base: number
  axis: Axis
  offset: number
  /** The last move, for the flick. */
  at: number
  x: number
  velocity: number
  /** This gesture has done something a press must not be read as. */
  moved: boolean
}

/**
 * Every row's swipe, and the one that is open.
 *
 * One row is open at a time: two uncovered actions on one screen is two
 * questions nobody asked. A finger that goes down anywhere while one is open
 * closes it, and that press does nothing else — the same rule a sheet follows,
 * and the reason `tookThePress` exists.
 */
export class Swipes {
  private drag: Drag | null = null
  private opened: string | null = null
  private swallow = false
  private watchers = new Set<() => void>()

  /** The row whose action is uncovered, or null. Handed to React as it is. */
  openId = (): string | null => this.opened

  /** What `data-swipe` should say for one row right now. */
  stateOf(id: string): SwipeState {
    if (this.drag && this.drag.id === id && this.drag.axis === "row") return "dragging"
    return this.opened === id ? "open" : ""
  }

  /** How far left that row's contents are, in px. */
  offsetOf(id: string): number {
    if (this.drag && this.drag.id === id && this.drag.axis === "row") return this.drag.offset
    return this.opened === id ? ACTION_WIDTH : 0
  }

  /** Which axis the gesture in progress belongs to. */
  axis(): Axis {
    return this.drag ? this.drag.axis : "undecided"
  }

  /** The row being dragged, once the gesture has said it is horizontal. */
  draggingId(): string | null {
    return this.drag && this.drag.axis === "row" ? this.drag.id : null
  }

  /**
   * A finger went down on `id` (or on nothing, when it went down off a row).
   *
   * Returns true when this press was spent closing what was open, so the press
   * opens no session and starts no drag.
   */
  begin(id: string | null, x: number, y: number, now: number): boolean {
    const closing = this.opened !== null && this.opened !== id
    if (closing) {
      this.opened = null
      this.drag = null
      this.swallow = true
      this.tell()
      return true
    }
    this.swallow = false
    if (!id) {
      this.drag = null
      return false
    }
    this.drag = {
      id,
      x0: x,
      y0: y,
      base: this.opened === id ? ACTION_WIDTH : 0,
      axis: "undecided",
      offset: this.opened === id ? ACTION_WIDTH : 0,
      at: now,
      x,
      velocity: 0,
      moved: false,
    }
    return false
  }

  /**
   * The finger moved. The axis is decided once, by the first movement past
   * `AXIS_SLOP`, and never revisited — a gesture that changes its mind
   * mid-stroke is one the reader did not make.
   */
  move(x: number, y: number, now: number): Axis {
    const drag = this.drag
    if (!drag) return "undecided"
    const dx = x - drag.x0
    const dy = y - drag.y0
    if (drag.axis === "undecided") {
      if (Math.abs(dx) < AXIS_SLOP && Math.abs(dy) < AXIS_SLOP) return "undecided"
      // A row that is already open owns a horizontal gesture whichever way it
      // goes; a closed one only answers a pull to the left.
      const sideways = Math.abs(dx) > Math.abs(dy) && (drag.base > 0 || dx < 0)
      drag.axis = sideways ? "row" : "list"
      if (drag.axis === "list") return "list"
    }
    if (drag.axis !== "row") return drag.axis
    const gap = Math.max(1, now - drag.at)
    drag.velocity = (x - drag.x) / gap
    drag.at = now
    drag.x = x
    drag.offset = offsetFor(drag.base, dx)
    drag.moved = true
    return "row"
  }

  /** The finger left. Returns the row it settled on and whether it stays open. */
  end(): { id: string; open: boolean } | null {
    const drag = this.drag
    this.drag = null
    if (!drag || drag.axis !== "row") return null
    const open = settleOpen(drag.offset, drag.velocity)
    const was = this.opened
    this.opened = open ? drag.id : null
    // A gesture that moved the row is not a press, however it ended.
    if (drag.moved) this.swallow = true
    if (was !== this.opened) this.tell()
    return { id: drag.id, open }
  }

  /** Put it back, without a finger: the row went away, or a sheet took over. */
  closeOpen(): void {
    if (this.opened === null && this.drag === null) return
    this.opened = null
    this.drag = null
    this.tell()
  }

  /**
   * Whether the press that is arriving now was spent on the swipe.
   *
   * Asked once and answered once: a press is one event, and a second reader of
   * the same answer would be a second thing to keep right.
   */
  tookThePress(): boolean {
    const took = this.swallow
    this.swallow = false
    return took
  }

  /** React's seam: `useSyncExternalStore(swipes.subscribe, swipes.openId)`. */
  subscribe = (watch: () => void): (() => void) => {
    this.watchers.add(watch)
    return () => {
      this.watchers.delete(watch)
    }
  }

  private tell(): void {
    for (const watch of this.watchers) watch()
  }
}

/** The page's one set of swipes: the list is one list, and one row is open in it. */
export const swipes = new Swipes()
