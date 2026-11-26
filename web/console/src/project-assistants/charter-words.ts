import { catalogFormat, catalogWord } from "../catalog"
import { charterRefusalCode } from "./charter-api"

export const charterWord = (key: string) => catalogWord("teamCharter", key)
export const charterFormat = (key: string, ...values: unknown[]) => catalogFormat("teamCharter", key, values)
export const charterNames = (names: readonly string[]) => names.join(charterWord("listSeparator"))

/** A refusal in words a person can act on. */
export function charterProblem(error: unknown, version?: number): string {
  switch (charterRefusalCode(error)) {
    case "version_conflict": return version ? charterFormat("conflictVersion", version) : charterWord("conflict")
    case "too_large": return charterWord("tooLarge")
    case "not_found": return charterWord("notFound")
    default: return charterWord("unknown")
  }
}

/** The roster subtitle for one Project's charter. */
export function charterRosterLine(summary: { version: number; bots: number; readers_current: number; pending_proposals: number; open_conflicts?: number }): string {
  // A disagreement a Bot reported needs the person before a proposal does.
  if ((summary.open_conflicts ?? 0) > 0) return charterFormat(summary.open_conflicts === 1 ? "rosterConflict" : "rosterConflicts", summary.open_conflicts)
  if (summary.pending_proposals > 0) return charterFormat(summary.pending_proposals === 1 ? "rosterProposal" : "rosterProposals", summary.pending_proposals)
  if (summary.readers_current >= summary.bots) return charterFormat("rosterAllCurrent", summary.version, summary.bots)
  return charterFormat("rosterSomeCurrent", summary.version, summary.readers_current, summary.bots)
}
