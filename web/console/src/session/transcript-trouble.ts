import type { ReadFailureKind } from "../poll.js"
import type { TranscriptPage } from "@clawdline/contract"

/*
 * When the transcript pane says it could not read the conversation.
 *
 * Measured on 2026-09-26: 100 local transcript reads across 10 sessions all
 * answered, and a whole day of Cloud reads refused 15, every one a closed
 * session. The red line people kept seeing was a single poll that threw in the
 * browser — a relay timeout, a reconnect — drawn the moment it happened, over a
 * skeleton that was about to fill or above entries that were still right.
 *
 * Temporary Cloud refusals also arrive as RefusalError: reconnecting is not
 * the machine denying access. Those and unreadable records get the same
 * recovery window as unanswered reads. Permission and execution refusals
 * still stop immediately. A slow first read gets a polite status.
 * A conversation that has not started (`readiness.ts`) is not a failure.
 */

/** Unanswered reads in a row before the pane says so. */
export const TRANSCRIPT_QUIET_FAILURES = 3
/**
 * And how long they must have been failing. Longer than two ordinary polls
 * (`POLL_MS`, 4 s) so a relay reconnect, which settles in a few seconds, is
 * never drawn; short enough that a machine that really went away is said
 * within one breath of looking.
 */
export const TRANSCRIPT_QUIET_MS = 12_000
/** Time before a still-unresolved initial read or recovery gets a polite notice. */
export const TRANSCRIPT_SLOW_MS = 4_000

/** An unavailable page must not overwrite a previously readable conversation. */
export function availableTranscript(page: TranscriptPage): TranscriptPage {
  if (page.evidence === "none" && page.note) {
    throw Object.assign(new Error(page.note), { code: "transcript_record_unavailable" })
  }
  return page
}

function temporaryRefusal(failure: unknown): boolean {
  const { code, status } = (failure ?? {}) as { code?: string; status?: number }
  if (["offline", "cloud_reconnecting", "cloud_read_busy", "cloud_read_timeout", "cloud_read_settled",
    "read_reply_mismatch", "machine_stale", "machine_freshness_unknown", "machine_freshness_unavailable",
    "machine_not_reported"].includes(code ?? "")) return true
  // Service failures and admission pressure may recover; unsupported features,
  // identity changes and permission refusals require a person's next action.
  return status === 408 || status === 429 || status === 500 || status === 502 || status === 503 || status === 504
}

export interface TranscriptRead {
  /** A page has been read at least once. */
  hasData: boolean
  /** The last kind of failed read, or null when the last read succeeded. */
  failureKind: ReadFailureKind | null
  /** Failed reads in a row. */
  failures: number
  /** How long the reads have been failing. */
  failingForMs: number
  failure?: unknown
  /** Elapsed time awaiting the first page or recovering from failed reads. */
  waitingForMs?: number
  /** A person explicitly asked to retry an established failure. */
  retrying?: boolean
}

/**
 * - `loading`: the skeleton, nothing read yet.
 * - `entries`: what has been read, with nothing said about the miss.
 * - `recovering`: a polite slow-read status; held entries stay visible.
 * - `failure`: `webTranscriptFailed`, with a way to ask again.
 */
export type TranscriptShow = "loading" | "entries" | "recovering" | "failure"

export function transcriptShow(read: TranscriptRead): TranscriptShow {
  const answered = read.hasData ? "entries" : "loading"
  if (read.retrying) return "recovering"
  if (read.failureKind === null) return !read.hasData && (read.waitingForMs ?? 0) >= TRANSCRIPT_SLOW_MS ? "recovering" : answered
  if (read.failureKind === "refused" && !temporaryRefusal(read.failure)) return "failure"
  const quiet = read.failures < TRANSCRIPT_QUIET_FAILURES || read.failingForMs < TRANSCRIPT_QUIET_MS
  if (!quiet) return "failure"
  return (read.waitingForMs ?? 0) >= TRANSCRIPT_SLOW_MS ? "recovering" : answered
}
