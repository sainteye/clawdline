// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat } from "./catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "./catalog.ts"
import type { BearingsSource, ScanSource, SessionRow } from "@clawdline/contract"
import type { ReadState } from "./read-state.js"

function ageWords(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const days = Math.floor(s / 86400)
  if (days >= 1) return catalogFormat("template", "9fd6ec6e8a4d", [days])
  const hours = Math.floor(s / 3600)
  if (hours >= 1) return catalogFormat("template", "4e22c21b2072", [hours])
  const minutes = Math.floor(s / 60)
  if (minutes >= 1) return catalogFormat("template", "4e237583c0c9", [minutes])
  return catalogFormat("template", "6d09f003ee65", [s])
}

// The list normally reads again every ten seconds (`Sessions.tsx`). One missed
// pass is ordinary repaint timing, not a warning worth adding to every row.
// Three missed passes are the first point where the age becomes useful context.
const retainedAgeNoteAfterSeconds = 30

// The daemon appends detail after this prefix as its inventory reader evolves.
// A refresh is a pending answer, not a terminal-source failure.
export function inventoryRefreshInProgress(notes: readonly string[] | undefined): boolean {
  return notes?.some((note) => note.startsWith("session inventory refresh is in progress;")) ?? false
}

/** The locale choice used by the session shell, without adding catalog keys. */
export function sessionReadingChinese(): boolean {
  const lang =
    (typeof document !== "undefined" ? document.documentElement.lang : "") ||
    (typeof navigator !== "undefined" ? navigator.language : "") ||
    ""
  return lang.toLowerCase().startsWith("zh")
}

/** An older retained row is an earlier fact, said quietly with its age. */
export function retainedStateWords(
  row: Pick<SessionRow, "source" | "state">,
  nowSeconds = Date.now() / 1000,
  chinese = sessionReadingChinese(),
): string | null {
  if (row.source?.freshness !== "unverified") return null
  const ageSeconds = Math.max(0, nowSeconds - row.source.observed_at)
  if (ageSeconds < retainedAgeNoteAfterSeconds) return null
  void chinese
  const age = ageWords(ageSeconds)
  if (row.state === "working") return catalogFormat("template", "19ebca4cf0e3", [age])
  if (row.state === "waiting") return catalogFormat("template", "08166fe2eeb1", [age])
  if (row.state === "idle") return catalogFormat("template", "d1a333c2a48d", [age])
  return catalogFormat("template", "1696f81987f7", [age])
}

/** One batch failure belongs above the list, not repeated as six row failures. */
export function batchReadingWords(
  source: BearingsSource | undefined,
  nowSeconds = Date.now() / 1000,
  chinese = sessionReadingChinese(),
): string | null {
  if (!source || source.freshness === "current") return null
  if (source.freshness === "unverified") {
    void chinese
    const age = ageWords(Math.max(0, nowSeconds - source.observed_at))
    return catalogFormat("template", "0e6efad65a6c", [age])
  }
  return catalogWord("literal", "ae1523fd382c")
}

/**
 * The concrete reason a terminal source did not finish, translated at the UI
 * boundary. Adapter diagnostics still cross the wire for evidence, but their
 * English subprocess text is never used as the sentence on a Chinese screen.
 */
export function scanFailureWords(
  notes: readonly string[] | undefined,
  sources: readonly ScanSource[] | undefined,
  chinese = sessionReadingChinese(),
): string | null {
  void chinese
  for (const note of notes ?? []) {
    if (note.startsWith("iTerm2 apple event failed:")) {
      return catalogWord("literal", "17ca248a65b6")
    }
    if (note.startsWith("iTerm2 answer was unreadable:")) {
      return catalogWord("literal", "46bdd1695d89")
    }
    const outside = /^tmux is at (.+), which is not on this daemon's PATH/.exec(note)
    if (outside) {
      return catalogFormat("template", "ad648d394b80", [outside[1]])
    }
    if (note.startsWith("tmux list-panes failed:")) {
      return catalogWord("literal", "ba53feed13b3")
    }
  }

  const openGap = (sources ?? []).flatMap((source) => source.gaps ?? []).find((gap) => !gap.sealed)
  if (openGap) {
    return catalogWord("literal", "2811b1d9d660")
  }
  if (inventoryRefreshInProgress(notes)) return null
  const incomplete = (sources ?? []).find((source) => !source.complete)
  if (incomplete) {
    return catalogFormat("template", "cdf164a88db5", [incomplete.source])
  }
  return null
}

/** The header's first number is always the list's total, never one state. */
export function totalSessionWords(total: number, chinese = sessionReadingChinese()): string {
  void chinese
  if (total === 0) return catalogWord("literal", "dee6ddd8236d")
  return catalogFormat("template", "df6b2970b6cb", [total])
}

/** The header count, derived without turning a missing snapshot into `[]`. */
export function sessionCountState(reading: {
  snapshot: { sessions: SessionRow[]; scan: { emptyAuthoritative: boolean } } | null
  loaded: boolean
  error: string | null
}): ReadState<SessionRow[]> {
  const rows = reading.snapshot?.sessions
  if (rows && rows.length > 0) return { phase: "ready", value: rows }
  if (reading.error) return { phase: "unanswered", error: reading.error }
  if (rows && reading.snapshot?.scan.emptyAuthoritative) return { phase: "empty_authoritative", value: rows }
  return { phase: "loading" }
}
