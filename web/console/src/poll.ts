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
  /**
   * How long the screen waits for one read. Past it the wait counts as failed
   * (`ReadDeadlineError`) and the next one is scheduled, so a read nobody
   * answers does not leave `pending` true for good, which a screen draws as
   * "loading". The read itself stays out: the next wait joins it instead of
   * starting another (`HeldReads`), and its answer is shown whenever it comes.
   */
  deadlineMs?: number
  classify: (err: unknown) => ReadFailureKind
  onChange: (state: PollState<T>) => void
  now?: () => number
  setTimer?: (fn: () => void, ms: number) => unknown
  clearTimer?: (handle: unknown) => void
  setDeadline?: (fn: () => void, ms: number) => unknown
  clearDeadline?: (handle: unknown) => void
}

/** A read that was still out at its deadline. Nobody answered: not a verdict. */
export class ReadDeadlineError extends Error {
  readonly code = "cloud_read_timeout"
  constructor(ms: number) {
    super("the read was not answered within " + ms + " ms")
    this.name = "ReadDeadlineError"
  }
}

/** How many reads of one target `HeldReads` lets be out at once. */
export const READS_OUT_PER_TARGET = 1

/**
 * How long a read may stay out before `HeldReads` stops counting on it. The
 * relay ends a read of its own accord well inside this; one still out after
 * it is taken for lost, so a read that never settles cannot hold its target
 * for good. The next ask starts a new read, and the lost one's answer, should
 * it ever come, is dropped as replaced.
 */
export const HELD_READ_LOST_MS = 180_000

interface HeldWait<T> {
  expired: boolean
  late: (value: T) => void
}

interface HeldRead<T> {
  promise: Promise<T>
  startedAt: number
  /** The newest wait on this read: the only one a late answer is given to. */
  waiter: HeldWait<T> | null
}

/**
 * Reads with a deadline on the waiting, not on the read.
 *
 * A read through the relay may take a minute or more, and nothing here can
 * cancel one. A deadline that rejected and dropped the late answer turned a
 * read that always takes 25 s into one that never arrives, and every retry
 * after it put another read on a machine that was already not answering. So:
 *
 * - one read per target is out at a time (`READS_OUT_PER_TARGET`); asking
 *   again while it is out waits on that same read, with a deadline of its own;
 * - a wait that passes its deadline rejects with `ReadDeadlineError`, which is
 *   what stops a screen saying "reading";
 * - when the read answers after that, the newest wait's `late` is called with
 *   the answer. No newer read of the target can have ended in between, because
 *   none was started. An older wait that a newer one replaced hears nothing.
 *
 * A read that fails after its deadline says nothing more: the wait already
 * failed. A read out longer than `HELD_READ_LOST_MS` is given up on: the next
 * ask starts another, and the old one's waits hear nothing from then on. Whether the answer is still for what is on screen is the caller's
 * to check in `late`; the target only says which reads are the same read.
 */
export class HeldReads<T> {
  private readonly out = new Map<string, HeldRead<T>>()
  private readonly deadlineMs: number
  private readonly setTimer: (fn: () => void, ms: number) => unknown
  private readonly clearTimer: (handle: unknown) => void
  private readonly now: () => number

  constructor(o: {
    deadlineMs: number
    now?: () => number
    setTimer?: (fn: () => void, ms: number) => unknown
    clearTimer?: (handle: unknown) => void
  }) {
    this.deadlineMs = o.deadlineMs
    this.now = o.now ?? Date.now
    this.setTimer = o.setTimer ?? ((fn, ms) => setTimeout(fn, ms))
    this.clearTimer = o.clearTimer ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  }

  /** Reads of `target` that have not settled: 0 or 1. */
  outstanding(target: string): number {
    return this.out.has(target) ? 1 : 0
  }

  read(target: string, start: () => Promise<T>, late: (value: T) => void): Promise<T> {
    let held = this.out.get(target)
    if (held && this.now() - held.startedAt >= HELD_READ_LOST_MS) {
      held.waiter = null
      this.out.delete(target)
      held = undefined
    }
    if (!held) {
      let promise: Promise<T>
      try {
        promise = Promise.resolve(start())
      } catch (error) {
        return Promise.reject(error)
      }
      const fresh: HeldRead<T> = { promise, startedAt: this.now(), waiter: null }
      held = fresh
      this.out.set(target, fresh)
      const done = () => { if (this.out.get(target) === fresh) this.out.delete(target) }
      promise.then(done, done)
    }
    const read = held
    const wait: HeldWait<T> = { expired: false, late }
    read.waiter = wait
    return new Promise<T>((resolve, reject) => {
      const timer = this.setTimer(() => {
        wait.expired = true
        reject(new ReadDeadlineError(this.deadlineMs))
      }, this.deadlineMs)
      read.promise.then(
        (value) => {
          if (!wait.expired) { this.clearTimer(timer); resolve(value); return }
          if (read.waiter === wait) wait.late(value)
        },
        (error) => {
          if (!wait.expired) { this.clearTimer(timer); reject(error) }
        },
      )
    })
  }
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
  /** With a deadline: the one read that may be out, and who waits on it. */
  private readonly held: HeldReads<T> | null

  constructor(options: PollerOptions<T>) {
    this.o = options
    this.intervalMs = options.intervalMs
    this.now = options.now ?? Date.now
    this.setTimer = options.setTimer ?? ((fn, ms) => setTimeout(fn, ms))
    this.clearTimer = options.clearTimer ?? ((handle) => clearTimeout(handle as ReturnType<typeof setTimeout>))
    // Real timers, not the pacing slot: `cancelTimer` must not end a deadline.
    this.held = options.deadlineMs
      ? new HeldReads<T>({ deadlineMs: options.deadlineMs, now: this.now, setTimer: options.setDeadline, clearTimer: options.clearDeadline })
      : null
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

  /**
   * The read answered after the wait on it had failed, and no later wait has
   * taken its place: the answer is shown and the failure it ends is cleared.
   * The next read stays where the failure scheduled it.
   */
  private arrivedLate(data: T): void {
    if (!this.alive) return
    this.set({ data, error: null, failure: null, failureKind: null, failures: 0, failingSince: null, pending: false })
  }

  private async tick(why: ReadReason): Promise<void> {
    this.timer = null
    this.again = false
    this.set({ reading: true })
    let settled: Partial<PollState<T>>
    try {
      const data = await (this.held
        ? this.held.read("", () => this.o.read(why), (value) => this.arrivedLate(value))
        : this.o.read(why))
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
