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

export interface PollerOptions<T> {
  read: () => Promise<T>
  intervalMs: number
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
    void this.tick()
  }

  stop(): void {
    this.alive = false
    this.cancelTimer()
  }

  /**
   * Ask now instead of at the next tick. The scheduled read is cancelled, so a
   * retry does not leave two timers behind it; a read already out is the one
   * the person asked for, and a second on top of it would only race it.
   */
  retry(): void {
    if (!this.alive || this.state.reading) return
    this.cancelTimer()
    void this.tick()
  }

  /**
   * A new pace starts with a read, as the hook's effect always did when its
   * interval changed: a page that starts following a send wants the next page
   * now, not after the old, slower tick.
   */
  setIntervalMs(ms: number): void {
    if (ms === this.intervalMs) return
    this.intervalMs = ms
    this.retry()
  }

  private cancelTimer(): void {
    if (this.timer !== null) this.clearTimer(this.timer)
    this.timer = null
  }

  private set(next: Partial<PollState<T>>): void {
    this.state = { ...this.state, ...next }
    this.o.onChange(this.state)
  }

  private async tick(): Promise<void> {
    this.timer = null
    this.set({ reading: true })
    let settled: Partial<PollState<T>>
    try {
      const data = await this.o.read()
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
    this.timer = this.setTimer(() => void this.tick(), this.intervalMs)
  }
}
