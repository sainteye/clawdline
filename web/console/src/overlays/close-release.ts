import type { CloseReason } from "@clawdline/contract"

/**
 * What the close sheet decides from a closeability reading, kept apart from
 * the sheet so it can be tested without a page.
 */

type Reading = { state?: unknown; version?: unknown; reasons?: unknown } | null | undefined
type Row = { closeability?: Reading } | null | undefined

/** The reason a Board item nobody has started gives (docs/work-system.md). */
export const UNSTARTED_BOARD_ITEM = "board_item_unstarted"

function reasonsOf(reading: Reading): CloseReason[] {
  return reading && Array.isArray(reading.reasons) ? reading.reasons as CloseReason[] : []
}

/**
 * The reasons a `close_blocked` was refused for.
 *
 * Over Clawdline Cloud the refusal arrives without them: the copied
 * `cloud-failure.js` keeps only its own table's detail fields, and
 * `close_blocked` is not in it. The daemon answers a close against any reading
 * but the current one with `close_not_proven`, so a `close_blocked` against
 * `version` is blocked by exactly what the page's row lists at that version.
 * A row that has moved on, or reads neither blocked nor unknown, says nothing.
 *
 * `unknown` too: the daemon answers `close_blocked` to an unknown reading only
 * for a process it found in the process table but no terminal can reach, so
 * that a second, forced press is the one that signals it (`closeEvidenceDecision`).
 * Any other unknown reading is refused `closeability_unknown` and never gets here.
 */
export function refusedReasons(refused: readonly CloseReason[], row: Row, version: string | null): CloseReason[] {
  if (refused.length) return [...refused]
  const reading = row?.closeability
  if (!version || !reading || (reading.state !== "blocked" && reading.state !== "unknown") || reading.version !== version) return []
  return reasonsOf(reading)
}

/**
 * The Board items a close may take this Session off: every one when the only
 * things blocking it are unstarted Board items, and none otherwise — a started
 * item, a TODO or an unreadable reading keeps the close as it was.
 */
export function releasableItems(reading: Reading): string[] {
  if (!reading || reading.state !== "blocked") return []
  const reasons = reasonsOf(reading)
  if (!reasons.length) return []
  const ids: string[] = []
  for (const reason of reasons) {
    if (reason.kind !== "obligation" || reason.code !== UNSTARTED_BOARD_ITEM || !reason.subject_id) return []
    ids.push(reason.subject_id)
  }
  return ids
}

/** The only reasons a process with no reachable terminal is held for. */
const PROCESS_ONLY_EVIDENCE = new Set(["session_identity_unbound", "terminal_unreadable"])

/**
 * Whether a close is the recovery of a process found in the process table that
 * no terminal can reach: the daemon's `recoverableProcessOnly`, which lets a
 * forced press pinned to this version signal it. The sheet already says only
 * the process is reachable, so that press is the person's decision and is sent
 * forced, instead of being refused once to ask the same question again. Such a
 * process has no bound Session, so it has no Board items or TODOs to list.
 */
export function processRecovery(id: string, reading: Reading): boolean {
  if (!/^(tty|pts\/)/.test(id) || !reading || reading.state !== "unknown") return false
  if (typeof reading.version !== "string" || !reading.version) return false
  const reasons = reasonsOf(reading)
  return reasons.length > 0 && reasons.every((reason) => reason.kind === "evidence" && PROCESS_ONLY_EVIDENCE.has(reason.code))
}
