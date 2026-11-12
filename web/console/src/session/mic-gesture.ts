/**
 * The composer's microphone has three presses: a tap dictates, and a hold or
 * a double press places a voice copilot call about this Session.
 *
 * A tap starts dictation at once, as it always has; a second press inside
 * DOUBLE_MS takes that start back (`undo`) and calls instead, so dictation
 * never waits to find out whether a second press is coming. A hold places the
 * call when the finger lifts, because the lift is a user activation and a
 * timer is not: iOS keeps audio started from a timer silent.
 *
 * Nothing is imported here, so `node --test` loads it as it is.
 */

/** How long a press is held before it is a call rather than a tap. */
export const HOLD_MS = 500
/** How soon a second press must follow the first to be a double press. */
export const DOUBLE_MS = 350
/** How far a finger may drift before a hold stops being one. */
export const MOVE_PX = 10
/** How long after a hold's lift the click it produces is swallowed. */
export const SWALLOW_MS = 700

export interface MicActions {
  /**
   * A single press: dictation. True when it started a recording; a press that
   * stopped one cannot be the first of a double press, or the second press
   * would throw away what was just said.
   */
  dictate(): boolean
  /** Takes back the dictation the first of a double press started. */
  undo(): void
  /** Places the call. Runs inside a pointer or click event. */
  call(): void
  /** The hold has lasted long enough; the lift will call. */
  held?(): void
}

export interface MicClock {
  now(): number
  after(ms: number, run: () => void): unknown
  cancel(handle: unknown): void
}

export class MicGesture {
  private timer: unknown = null
  private holding = false
  private start: { x: number; y: number } | null = null
  private swallowUntil = 0
  private lastTap = 0

  private readonly act: MicActions
  private readonly clock: MicClock

  constructor(act: MicActions, clock: MicClock) {
    this.act = act
    this.clock = clock
  }

  down(x: number, y: number) {
    this.reset()
    this.start = { x, y }
    this.timer = this.clock.after(HOLD_MS, () => {
      this.timer = null
      this.holding = true
      this.act.held?.()
    })
  }

  move(x: number, y: number) {
    if (!this.start) return
    if (Math.hypot(x - this.start.x, y - this.start.y) > MOVE_PX) this.reset()
  }

  up() {
    const held = this.holding
    this.reset()
    if (!held) return
    this.swallowUntil = this.clock.now() + SWALLOW_MS
    this.lastTap = 0
    this.act.call()
  }

  /** The press was taken away (scroll, system gesture, pointer lost). */
  abort() {
    this.reset()
  }

  click() {
    const now = this.clock.now()
    if (now < this.swallowUntil) {
      this.swallowUntil = 0
      return
    }
    if (this.lastTap && now - this.lastTap <= DOUBLE_MS) {
      this.lastTap = 0
      this.act.undo()
      this.act.call()
      return
    }
    this.lastTap = this.act.dictate() ? now : 0
  }

  private reset() {
    if (this.timer !== null) this.clock.cancel(this.timer)
    this.timer = null
    this.holding = false
    this.start = null
  }
}
