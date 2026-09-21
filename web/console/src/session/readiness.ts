import type { SessionRow } from "@clawdline/contract"

/**
 * Whether this is a real session whose conversation has not begun yet.
 *
 * Codex exposes that fact through `identity` before it has a rollout; Claude
 * can already have an identity while its first transcript does not exist, so
 * the activity reason is the common path. Neither means the terminal screen
 * was unreadable. Kept as one pure seam because the list, detail header,
 * transcript and status line must all say the same thing.
 */
export function conversationNotStarted(row: SessionRow | null | undefined): boolean {
  return row?.identity === "no_record" || row?.activity?.unknown_reason === "no_record"
}
