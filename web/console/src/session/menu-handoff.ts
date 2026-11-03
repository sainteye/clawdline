/**
 * When a waiting question is handed to the Session's own Terminal.
 *
 * The registry says "waiting" as soon as the CLI starts to ask, and the
 * screen read that turns the dialog into buttons can land a snapshot or two
 * later. Switching on the first snapshot without options sent people to
 * Terminal for ordinary Claude Code and Codex questions that would have
 * become buttons a moment later. A question is handed off only when it has
 * stayed without options for `MENU_HANDOFF_GRACE_MS` — long enough for the
 * Waiting card's own reread loop (`menu-follow.ts`) to ask the machine to
 * read it again several times — and the newest row says the screen *was*
 * captured. A captured screen that still yields no options is the parser
 * genuinely not reading the dialog; a screen that could not be captured is a
 * read that has not happened yet, and Terminal would read the same nothing.
 */

/** How long a question may stay without options before it is called unreadable. */
export const MENU_HANDOFF_GRACE_MS = 6_000
/** The notice's countdown before it switches on its own. */
export const MENU_HANDOFF_COUNTDOWN_S = 5

export type UnreadMenu = { key: string; since: number }

/** The unread question this snapshot shows, keeping when it was first seen. */
export function unreadMenuSeen(previous: UnreadMenu | null, key: string, now: number): UnreadMenu | null {
  if (!key) return null
  return previous?.key === key ? previous : { key, since: now }
}

/**
 * Milliseconds until the handoff notice is due, or null when it never is for
 * this snapshot (no unread question, the screen was not read, or this
 * question already had its notice).
 */
export function menuHandoffDelay(unread: UnreadMenu | null, screen: string | null | undefined, offered: string, now: number): number | null {
  if (!unread || screen !== "read" || offered === unread.key) return null
  return Math.max(0, unread.since + MENU_HANDOFF_GRACE_MS - now)
}
