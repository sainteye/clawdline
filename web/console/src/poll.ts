/*
 * One route read on an interval, without React.
 *
 * `usePoll` (`useFleet.ts`) is this plus a `useState`. The timing lives here so
 * that "a retry does not start a second read while one is out" and "a success
 * ends the run of failures" can be proved under `node --test`, which has no DOM
 * and no React renderer to drive a hook with.
 *
 * Nothing in this file is imported at run time, for the same reason.
 */

/** The browser's own answer: `document.hidden`, heard through `visibilitychange`. */
export function documentVisibility(): Visibility | undefined {
  if (typeof document === "undefined") return undefined
  return {
    hidden: () => document.hidden,
    subscribe(fn) {
      document.addEventListener("visibilitychange", fn)
      return () => document.removeEventListener("visibilitychange", fn)
    },
  }
}

/**
 * Why a read did not come back with a page.
 *
 * - `unanswered`: nobody answered — a `TransportError`, which over Cloud is also
 *   what a relay timeout, a reconnect or a starting socket becomes.
 * - `refused`: the machine answered and said no — a `RefusalError`, with a code.
 * - `unknown`: anything else thrown. It is not the machine's answer either, so a
 *   screen treats it as the unanswered kind rather than as a verdict.
 */
export type ReadFailureKind = "unanswered" | "refused" | "unknown"

export interface PollState<T> {
  data: T | null
  /** The failed read's message, for a technical disclosure. */
  error: string | null
  /** The failed read's thrown value, kept whole. */
  failure: unknown
  failureKind: ReadFailureKind | null
  /** Failed reads in a row since the last success; 0 when the last read succeeded. */
  failures: number
  /** When the first of those failed reads settled (ms since epoch), or null. */
  failingSince: number | null
  /** True until the first read settles either way, and never again. */
  pending: boolean
  /** A read is out right now. */
  reading: boolean
}

/**
 * Why a read is being made: the poller's own timer, the first read, a person's
 * retry, a `poke` (something said the answer may have changed), or the page
 * coming back into view. A reader may read differently for each — the
 * transcript asks only for what was appended on a poke and reads whole on the
 * timer — and one that does not care takes no argument.
 */
export type ReadReason = "start" | "timer" | "retry" | "poke" | "visible"

/**
 * Whether the page is on screen, and a way to hear when that changes. In a
 * browser this is `document.hidden` and `visibilitychange`
 * (`documentVisibility`); a poller with none reads whether or not anybody can
 * see the answer.
 */
export interface Visibility {
  hidden(): boolean
  subscribe(fn: () => void): () => void
}

/**
 * Doubling waits after failed reads: `firstMs`, twice that, and so on up to
 * `maxMs`. A success goes back to the interval.
 */
export interface Backoff {
  firstMs: number
  maxMs: number
}

export interface PollerOptions<T> {
  read: (why: ReadReason) => Promise<T>
  intervalMs: number
  /** Pause while hidden, read once on coming back. */
  visibility?: Visibility
  /** After a failed read, wait this instead of the interval. */
  backoff?: Backoff
  classify: (err: unknown) => ReadFailureKind
  onChange: (state: PollState<T>) => void
  now?: () => number
  setTimer?: (fn: () => void, ms: number) => unknown
  clearTimer?: (handle: unknown) => void
}

export function initialPollState<T>(): PollState<T> {
  return {
    data: null,
    error: null,
    failure: null,
    failureKind: null,
    failures: 0,
    failingSince: null,
    pending: true,
    reading: false,
  }
}

export class Poller<T> {
  private state: PollState<T> = initialPollState<T>()
  private timer: unknown = null
  private alive = false
  /** A tick came due while hidden; the read waits for the page to be seen. */
  private parked = false
  /** A poke arrived while a read was out: read again as soon as it settles. */
  private again = false
  private unwatch: (() => void) | null = null
  private readonly o: PollerOptions<T>
  private intervalMs: number
  private readonly now: () => number
  private readonly setTimer: (fn: () => void, ms: number) => unknown
  private readonly clearTimer: (handle: unknown) => void

  constructor(options: PollerOptions<T>) {
    this.o = options
    this.intervalMs = options.intervalMs
    this.now = options.now ?? Date.now
    this.setTimer = options.setTimer ?? ((fn, ms) => setTimeout(fn, ms))
    this.clearTimer = options.clearTimer ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  }

  get(): PollState<T> {
    return this.state
  }

  start(): void {
    this.alive = true
    const v = this.o.visibility
    if (v) this.unwatch = v.subscribe(() => this.visibilityChanged())
    if (v?.hidden()) {
      this.parked = true
      return
    }
    void this.tick("start")
  }

  stop(): void {
    this.alive = false
    this.parked = false
    this.again = false
    this.unwatch?.()
    this.unwatch = null
    this.cancelTimer()
  }

  /**
   * Something says the answer may have changed: read now, or, with a read
   * already out, once more as soon as it settles — the read that is out may
   * have been answered before the change it was poked for. While hidden, the
   * read waits for the page to be seen, like every other.
   */
  poke(): void {
    if (!this.alive) return
    if (this.state.reading) {
      this.again = true
      return
    }
    if (this.o.visibility?.hidden()) {
      this.parked = true
      this.cancelTimer()
      return
    }
    this.cancelTimer()
    void this.tick("poke")
  }

  /**
   * Ask now instead of at the next tick. The scheduled read is cancelled, so a
   * retry does not leave two timers behind it; a read already out is the one
   * the person asked for, and a second on top of it would only race it.
   */
  retry(): void {
    if (!this.alive || this.state.reading) return
    this.cancelTimer()
    void this.tick("retry")
  }

  /**
   * A new pace takes effect from the next wait, without a read of its own: a
   * page that starts following a send is poked by what changed, and a read on
   * every change of pace was a second read of the same answer. A scheduled
   * read is moved to the new pace, counted from now; one that came due while
   * hidden still waits for the page to be seen.
   */
  setIntervalMs(ms: number): void {
    if (ms === this.intervalMs) return
    this.intervalMs = ms
    if (!this.alive || this.timer === null) return
    this.cancelTimer()
    this.schedule()
  }

  private visibilityChanged(): void {
    if (!this.alive) return
    if (this.o.visibility?.hidden()) {
      // Nothing is read for a page nobody can see; the read that would have
      // come due is made when it is seen again.
      if (this.timer !== null) {
        this.cancelTimer()
        this.parked = true
      }
      return
    }
    if (!this.parked || this.state.reading) return
    this.parked = false
    void this.tick("visible")
  }

  private schedule(): void {
    const failures = this.state.failures
    const b = this.o.backoff
    const wait = b && failures > 0 ? Math.min(b.maxMs, b.firstMs * 2 ** Math.min(failures - 1, 30)) : this.intervalMs
    this.timer = this.setTimer(() => {
      this.timer = null
      if (this.o.visibility?.hidden()) {
        this.parked = true
        return
      }
      void this.tick("timer")
    }, wait)
  }

  private cancelTimer(): void {
    if (this.timer !== null) this.clearTimer(this.timer)
    this.timer = null
  }

  private set(next: Partial<PollState<T>>): void {
    this.state = { ...this.state, ...next }
    this.o.onChange(this.state)
  }

  private async tick(why: ReadReason): Promise<void> {
    this.timer = null
    this.again = false
    this.set({ reading: true })
    let settled: Partial<PollState<T>>
    try {
      const data = await this.o.read(why)
      settled = { data, error: null, failure: null, failureKind: null, failures: 0, failingSince: null }
    } catch (err) {
      settled = {
        error: err instanceof Error ? err.message : String(err),
        failure: err,
        failureKind: this.o.classify(err),
        failures: this.state.failures + 1,
        failingSince: this.state.failingSince ?? this.now(),
      }
    }
    if (!this.alive) return
    this.set({ ...settled, pending: false, reading: false })
    if (this.again) {
      this.again = false
      if (this.o.visibility?.hidden()) {
        this.parked = true
        return
      }
      void this.tick("poke")
      return
    }
    this.schedule()
  }
}
