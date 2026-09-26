import type { ReadFailureKind } from "../poll.js"

/*
 * When the transcript pane says it could not read the conversation.
 *
 * Measured on 2026-09-26: 100 local transcript reads across 10 sessions all
 * answered, and a whole day of Cloud reads refused 15, every one a closed
 * session. The red line people kept seeing was a single poll that threw in the
 * browser — a relay timeout, a reconnect — drawn the moment it happened, over a
 * skeleton that was about to fill or above entries that were still right.
 *
 * So a read nobody answered is not news until it has kept happening: several
 * in a row, over a stretch longer than one slow read. A refusal, or a page
 * that says there is no record, is the machine's answer and is shown at once.
 * The "not started" branch (`readiness.ts`) is decided before this and is not
 * a failure at all.
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

export interface TranscriptRead {
  /** A page has been read at least once. */
  hasData: boolean
  /** The last kind of failed read, or null when the last read succeeded. */
  failureKind: ReadFailureKind | null
  /** Failed reads in a row. */
  failures: number
  /** How long the reads have been failing. */
  failingForMs: number
  /** The page read says `evidence: "none"` and carries a note. */
  noRecord: boolean
}

/**
 * - `loading`: the skeleton, nothing read yet.
 * - `entries`: what has been read, with nothing said about the miss.
 * - `failure`: `webTranscriptFailed`, with a way to ask again.
 */
export type TranscriptShow = "loading" | "entries" | "failure"

export function transcriptShow(read: TranscriptRead): TranscriptShow {
  const answered = read.hasData ? (read.noRecord ? "failure" : "entries") : "loading"
  if (read.failureKind === null) return answered
  if (read.failureKind === "refused") return "failure"
  const quiet = read.failures < TRANSCRIPT_QUIET_FAILURES || read.failingForMs < TRANSCRIPT_QUIET_MS
  return quiet ? answered : "failure"
}
