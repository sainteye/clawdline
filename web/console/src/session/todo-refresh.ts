/*
 * How a Session's to-do fold keeps itself current, and what its header says
 * while it does.
 *
 * On 2026-09-25 a phone showed "Session 待辦 載入中…" for minutes while the
 * machine answered the same read in 0.17 s. Two things hid it: the header said
 * "loading" whenever there was no page, so a failed read read as a slow one
 * forever, with its reason only inside the closed fold; and nothing asked
 * again, so rows a Session added never appeared until the page was reopened.
 * This file is the plain logic of both, kept out of the component so a test
 * can run it with a fake clock.
 */

import { RefusalError, TransportError } from "@clawdline/core/refusal"

/**
 * How often an open Session page asks for its to-dos again while the page is
 * visible. Over Clawdline Cloud every ask is one envelope each way across the
 * relay, so this is per open Session page and never faster; the transcript's
 * reuse window is the same fifteen seconds (`TRANSCRIPT_LINE_REREAD_MS`).
 */
export const TODO_REFRESH_MS = 15_000

/**
 * The open Session page's own pace for its to-dos and attention notes. It asks
 * at once when the page is seen again, focused, or a Board item changed
 * through it, and the attention notes also when the row's `attention_count`
 * moves (`Interventions.tsx`); between those, this is the safety read. A row
 * with no `attention_count` (an older daemon) keeps `TODO_REFRESH_MS` for its
 * notes. On a phone the two reads at fifteen seconds were 24 of a detail
 * page's 59 requests in three minutes.
 */
export const TODO_SAFETY_MS = 60_000

/**
 * What the fold's header shows.
 *
 * `loading` only while the first read has no answer at all; `failed` when
 * there is no page and the last read failed; `stale` when a page is shown and
 * the last refresh of it failed; `loaded` otherwise.
 */
export type TodoHeaderState = "loading" | "failed" | "stale" | "loaded"

export function todoHeaderState(hasPage: boolean, failed: boolean): TodoHeaderState {
  if (!hasPage) return failed ? "failed" : "loading"
  return failed ? "stale" : "loaded"
}

/**
 * One read at a time for one Session's to-dos.
 *
 * A refresh that finds a read in flight joins it. An ask that needs an answer
 * newer than the one in flight — after the person changed something — marks
 * it and gets exactly one more read when the current one ends, so there are
 * never two in flight and never an answer from before the change shown as
 * the last word.
 */
export class OneRead {
  private flight: Promise<void> | null = null
  private again = false
  private readonly read: () => Promise<void>

  constructor(read: () => Promise<void>) {
    this.read = read
  }

  get reading(): boolean {
    return this.flight !== null
  }

  ask(fresh = false): Promise<void> {
    if (this.flight) {
      if (fresh) this.again = true
      return this.flight
    }
    const flight = (async () => {
      try {
        do {
          this.again = false
          try {
            await this.read()
          } catch {
            // `read` reports its own failure; a throw here must not strand
            // the flight or skip the one more read somebody asked for.
          }
        } while (this.again)
      } finally {
        this.flight = null
      }
    })()
    this.flight = flight
    return flight
  }
}

/** The parts of the page a refresh schedule listens to, so a test can hand in its own. */
export interface RefreshEnvironment {
  setInterval(run: () => void, ms: number): unknown
  clearInterval(handle: unknown): void
  visible(): boolean
  onVisible(run: () => void): () => void
  onFocus(run: () => void): () => void
  /** A Board item changed through this page, such as one completed from this fold. */
  onWorkChanged(run: () => void): () => void
}

/**
 * The browser's own. The caller hands in the Board's change signal
 * (`onWorkItemChanged`), so this file stays free of imports a test cannot run.
 */
export function browserRefreshEnvironment(onWorkChanged: RefreshEnvironment["onWorkChanged"]): RefreshEnvironment {
  return {
    setInterval: (run, ms) => window.setInterval(run, ms),
    clearInterval: (handle) => window.clearInterval(handle as number),
    visible: () => document.visibilityState === "visible",
    onVisible: (run) => {
      const changed = () => { if (document.visibilityState === "visible") run() }
      document.addEventListener("visibilitychange", changed)
      return () => document.removeEventListener("visibilitychange", changed)
    },
    onFocus: (run) => {
      window.addEventListener("focus", run)
      return () => window.removeEventListener("focus", run)
    },
    onWorkChanged,
  }
}

/**
 * Ask `refresh` every `every` (`TODO_REFRESH_MS` unless given) while the page is visible, and at once
 * when it becomes visible again, its window is focused, or a Board item
 * changed through this page. A hidden page's ticks are skipped rather than
 * queued. Returns the stop.
 */
export function watchTodoRefresh(refresh: () => void, env: RefreshEnvironment, every = TODO_REFRESH_MS): () => void {
  const tick = env.setInterval(() => { if (env.visible()) refresh() }, every)
  const offVisible = env.onVisible(refresh)
  const offFocus = env.onFocus(refresh)
  const offChanged = env.onWorkChanged(refresh)
  return () => {
    env.clearInterval(tick)
    offVisible()
    offFocus()
    offChanged()
  }
}

/**
 * How long a read that failed transiently waits before its one retry. A phone
 * that comes back after a minute hidden rebuilds its Cloud connection, and the
 * first read in that window is refused while the rebuild finishes; a few
 * seconds later the same read answers.
 */
export const TRANSIENT_READ_RETRY_MS = 3_000

/**
 * A failure that is likely gone a few seconds later: the transport did not
 * deliver an answer (a Cloud connection rebuilding, a read that timed out),
 * or a Cloud read lane was full. Any other refusal is the machine's answer
 * and is shown at once.
 */
export function isTransientReadFailure(error: unknown): boolean {
  if (error instanceof TransportError) return true
  return error instanceof RefusalError && (error.code === "cloud_ingress_busy" || error.code === "cloud_read_busy")
}

/**
 * Read, and when that fails transiently, wait `TRANSIENT_READ_RETRY_MS` and
 * read once more; the second failure, or any other, is thrown as it came.
 */
export async function readWithOneRetry<T>(
  read: () => Promise<T>,
  wait: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
): Promise<T> {
  try {
    return await read()
  } catch (error) {
    if (!isTransientReadFailure(error)) throw error
  }
  await wait(TRANSIENT_READ_RETRY_MS)
  return read()
}

/**
 * The reason a read failed, for the header's title and label: the refusal's
 * code, or what the transport said and what it said underneath. Never the
 * page's own sentence, which is shown beside it.
 */
export function readFailureReason(error: unknown): string {
  if (error instanceof RefusalError) return error.code
  if (error instanceof TransportError) {
    const cause = error.cause instanceof Error ? error.cause.message : ""
    return cause ? `${error.message} (${cause})` : error.message
  }
  return error instanceof Error ? error.message : String(error)
}
