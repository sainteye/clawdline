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
}

/** The browser's own. */
export function browserRefreshEnvironment(): RefreshEnvironment {
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
  }
}

/**
 * Ask `refresh` every `TODO_REFRESH_MS` while the page is visible, and at once
 * when it becomes visible again or its window is focused. A hidden page's
 * ticks are skipped rather than queued. Returns the stop.
 */
export function watchTodoRefresh(refresh: () => void, env: RefreshEnvironment = browserRefreshEnvironment()): () => void {
  const tick = env.setInterval(() => { if (env.visible()) refresh() }, TODO_REFRESH_MS)
  const offVisible = env.onVisible(refresh)
  const offFocus = env.onFocus(refresh)
  return () => {
    env.clearInterval(tick)
    offVisible()
    offFocus()
  }
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
