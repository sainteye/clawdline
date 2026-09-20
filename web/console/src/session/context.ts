import type { SessionInfoContext } from "@clawdline/contract"

/**
 * What the status line's `.item.context` cell says, decided apart from the
 * markup that draws it — the `identityHTML` half of the original's
 * `status-line.js` that has anything to get wrong.
 *
 * `null` is a cell that is not drawn at all. That is the whole point of it:
 * a session nobody could take a reading for — a Claude model this build has
 * no window for, whose status line has written no cache, or a session with no
 * record yet — is unknown, and a permanent green `ctx 0%` is the reading that
 * would be believed.
 */
export type ContextCell = {
  /** Rounded and clamped, as the original rounds it. */
  percent: number
  /** `ok`, `warn` or `bad`; `legacy/status-line.css` colours `data-level` from it. */
  level: "ok" | "warn" | "bad"
  /** The tooltip. It names both sides only when the daemon sent both. */
  title: string
}

/**
 * `tokens` is the word the string catalogue has for it
 * (`webInfoTokens`); the original writes `ctx` itself, and so does this.
 *
 * The daemon withholds `windowTokens` for a window it guessed, so
 * `162,277 / 1,000,000 tokens` is shown only where it is a measurement. The
 * percentage beside it is read as the approximation it is either way.
 */
export function contextCell(at: SessionInfoContext | undefined | null, tokens: string): ContextCell | null {
  if (!at || typeof at.usedPercent !== "number" || !Number.isFinite(at.usedPercent)) return null
  const percent = Math.max(0, Math.min(100, Math.round(at.usedPercent)))
  const level = percent >= 85 ? "bad" : percent >= 60 ? "warn" : "ok"
  let title = `ctx ${percent}%`
  if (typeof at.usedTokens === "number" && typeof at.windowTokens === "number") {
    title += ` (${at.usedTokens.toLocaleString()} / ${at.windowTokens.toLocaleString()} ${tokens})`
  }
  return { percent, level, title }
}
