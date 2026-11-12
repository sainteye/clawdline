/**
 * A press on the Session title's icon has two meanings: a tap does what the
 * icon always did (opens Snippets), and a hold places a voice copilot call
 * about this Session.
 *
 * The call is placed the moment the hold reaches HOLD_MS, while the finger is
 * still down: the person asked for it not to wait for the lift. A timer is not
 * a user activation, so iOS may keep that call's audio suspended or refuse its
 * microphone; the lift, which is one, then runs `lifted` to wake it. The click
 * a lift produces after a hold is swallowed, so the hold does not also open
 * Snippets.
 *
 * Nothing is imported here, so `node --test` loads it as it is.
 */

/** How long a press is held before it is a call rather than a tap. */
export const HOLD_MS = 500
/** How far a finger may drift before a hold stops being one. */
export const MOVE_PX = 10
/** How long after a hold's lift the click it produces is swallowed. */
export const SWALLOW_MS = 700

export interface HoldActions {
  /** Places the call, the moment the hold is long enough. */
  call(): void
  /** The finger lifted after a call was placed: a user activation to wake its audio in. */
  lifted?(): void
  /** The press stopped being a hold (lifted, slid away, taken by the system). */
  released?(): void
}

export interface HoldClock {
  now(): number
  after(ms: number, run: () => void): unknown
  cancel(handle: unknown): void
}

export class HoldGesture {
  private timer: unknown = null
  private holding = false
  private start: { x: number; y: number } | null = null
  private swallowUntil = 0
  private readonly act: HoldActions
  private readonly clock: HoldClock

  constructor(act: HoldActions, clock: HoldClock) {
    this.act = act
    this.clock = clock
  }

  down(x: number, y: number) {
    this.reset()
    this.start = { x, y }
    this.timer = this.clock.after(HOLD_MS, () => {
      this.timer = null
      this.holding = true
      this.act.call()
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
    this.act.lifted?.()
  }

  /** The press was taken away (scroll, system gesture, pointer lost). */
  abort() {
    this.reset()
  }

  /** Whether a click should do the icon's ordinary job; false right after a hold. */
  tap(): boolean {
    if (this.clock.now() < this.swallowUntil) {
      this.swallowUntil = 0
      return false
    }
    return true
  }

  private reset() {
    const was = this.holding || this.timer !== null
    if (this.timer !== null) this.clock.cancel(this.timer)
    this.timer = null
    this.holding = false
    this.start = null
    if (was) this.act.released?.()
  }
}
