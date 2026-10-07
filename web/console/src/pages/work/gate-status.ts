import { localizedLiteralMap } from "../../catalog.js"
import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import type { WorkGateCompactRead, WorkGateRoundSummary } from "@clawdline/contract"

export const AUTHORITY: Record<string, string> = localizedLiteralMap({
  pass: "5418f86ba3ea",
  ai_override: "a5ef9ebb1095",
  person_override: "f6eb09d61a5d",
  technical_ai_override: "28d16292ace1",
  technical_person_override: "9d2f7c1f283b",
})

/** Names both immutable switch values so a compact card never hides which gate was captured. */
export function gateSnapshotText(cycle: number, planning: boolean, verification: boolean): string {
  if (cycle === 0) return catalogWord("literal", "731141a1b815")
  return catalogFormat("template", "92dfded7689f", [planning ? catalogWord("literal", "09b410f51b19") : catalogWord("literal", "862364ba6811"), verification ? catalogWord("literal", "09b410f51b19") : catalogWord("literal", "862364ba6811")])
}

export function gateStatus(gate?: WorkGateCompactRead): string {
  if (!gate || gate.gate_snapshot_cycle === 0) return catalogWord("literal", "b2a1c6bc47a4")
  const authority = gate.current_authorization
  if (authority) return AUTHORITY[authority.kind] ?? catalogWord("literal", "a8c1da282b1b")
  const escalation = gate.escalation
  if (escalation && escalation.state !== "resolved") {
    return escalation.state === "waiting_user" ? catalogWord("literal", "7314e95c4cbb") : catalogWord("literal", "e4c7f6e1f371")
  }
  if (!gate.verify_gate) return gate.planning_gate ? catalogWord("literal", "ce0c89b0dbf3") : catalogWord("literal", "d85a2ae65148")
  const round = gate.latest_round
  if (!round) return catalogWord("literal", "d1e4a07bc098")
  if (round.state === "complete" && round.verdict === "PASS") return catalogWord("literal", "e7f7f95a735c")
  return roundStatus(round)
}

export function roundStatus(round: WorkGateRoundSummary): string {
  if (round.state === "stale") return catalogWord("literal", "80916f71df75")
  if (round.state === "technical_failure") return catalogWord("literal", "ee97a405ed90")
  if (round.state !== "complete") return ({ queued: catalogWord("literal", "16c749fb7cd0"), dispatching: catalogWord("literal", "d9715e0f7ae6"), running: catalogWord("literal", "9d7cb46b6e3a") } as Record<string, string>)[round.state] ?? catalogWord("literal", "1c2daefcb904")
  if (round.verdict === "PASS") return catalogWord("literal", "583482996f34")
  if (round.verdict === "FAIL") return catalogWord("literal", "4f214c6fa008")
  if (round.verdict === "NEEDS_WORK") return catalogWord("literal", "3ad19c8935c2")
  return catalogWord("literal", "4ed38be26870")
}
