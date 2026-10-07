/*
 * The Archive page's rules (docs/session-archive.md), without the page.
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is;
 * the words come in as a function, as `session/restore-offer.ts` takes them.
 */
import type { ArchivedSession } from "@clawdline/contract"

/** The name an archived entry goes by: the title its row showed, else its folder's label, else the folder. */
export function archivedName(s: Pick<ArchivedSession, "title" | "place_label" | "cwd">): string {
  return s.title.trim() || s.place_label.trim() || s.cwd
}

/**
 * When it was archived, twice: how long ago, and the date and time.
 *
 * The relative part alone goes vague past a day ("3 days ago") and the date
 * alone makes the reader do arithmetic, so the entry says both. `timeZone` is
 * for the tests; the page reads the browser's own.
 */
export function archivedWhen(
  at: number,
  nowMs: number,
  locale?: string,
  timeZone?: string,
): { relative: string; absolute: string } {
  if (!Number.isFinite(at) || at <= 0) return { relative: "", absolute: "" }
  // A clock a little behind the machine's still means "now", not the future.
  const seconds = Math.min(0, at - nowMs / 1000)
  const absolute = Math.abs(seconds)
  const unit: Intl.RelativeTimeFormatUnit = absolute < 90 * 60 ? "minute" : absolute < 36 * 3600 ? "hour" : "day"
  const size = unit === "minute" ? 60 : unit === "hour" ? 3600 : 86400
  let relative: string
  try {
    relative = new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(Math.round(seconds / size), unit)
  } catch {
    // refusal-ok: a browser refusing a locale is not an archive refusal.
    relative = ""
  }
  return { relative, absolute: stamp(at, timeZone) }
}

/** `2026-09-24 10:05`, in the reader's own time zone unless told another. */
function stamp(at: number, timeZone?: string): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23",
  }).formatToParts(new Date(at * 1000))
  const part = (type: string) => parts.find((p) => p.type === type)?.value ?? ""
  return `${part("year")}-${part("month")}-${part("day")} ${part("hour")}:${part("minute")}`
}

/** What became of one entry's Restore press. */
export type Outcome =
  | { kind: "restoring" }
  | { kind: "failed"; code?: string; message?: string; messageLang?: string }

export type ArchiveEntryWord =
  | "archiveRestoring"
  | "archiveFailedNotArchived"
  | "archiveFailedAlreadyOpen"
  | "restoreFailedPlaceUnavailable"
  | "restoreFailedNotFound"
  | "restoreFailedOpen"
  | "restoreFailedBusy"
  | "restoreFailedUnknown"
  | "restoreFailedSaid"

/** Each `RestoreArchivedCode` and its sentence; the four it shares with the reboot restore are that restore's. */
const CODE_WORD: Record<string, ArchiveEntryWord> = {
  not_archived: "archiveFailedNotArchived",
  already_open: "archiveFailedAlreadyOpen",
  place_unavailable: "restoreFailedPlaceUnavailable",
  conversation_not_found: "restoreFailedNotFound",
  open_failed: "restoreFailedOpen",
  over_capacity: "restoreFailedBusy",
}

/**
 * The line under an entry after a press, or "" before one.
 *
 * `open_failed` is the one whose reason is the terminal's own, so its message
 * follows the sentence; a code this bundle does not know says the machine's
 * message, or that it did not open.
 */
export function entryLine(
  outcome: Outcome | undefined,
  word: (key: ArchiveEntryWord, holes?: Record<string, string>) => string,
): string {
  if (!outcome) return ""
  if (outcome.kind === "restoring") return word("archiveRestoring")
  const key = outcome.code ? CODE_WORD[outcome.code] : undefined
  if (!key) return outcome.message ? word("restoreFailedSaid", { why: outcome.message }) : word("restoreFailedUnknown")
  const said = word(key)
  return outcome.code === "open_failed" && outcome.message ? `${said} ${outcome.message}` : said
}

/** The list once one conversation has reopened: only an entry that opened leaves it. */
export function withoutRestored<T extends { conversation_id: string }>(list: readonly T[], conversation: string): T[] {
  return list.filter((s) => s.conversation_id !== conversation)
}
