import { localizedLiteralMap } from "../../catalog.js"
import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import type { SessionRow } from "@clawdline/contract"
import type { WorkV2Item } from "./api.js"

const CONDITIONS: Record<string, string> = localizedLiteralMap({
  blocked: "4fefeb9ea924",
  waiting_user: "edbff7828d02",
  owner_required: "83d7348506b6",
  owner_offline: "0b7448cc7d28",
  evidence_unknown: "e6067601215a",
  assignment_failed: "c2c72abc5152",
  assigned_unnotified: "d7ee58bbe444",
})

export function conditionWords(item: { condition: WorkV2Item["condition"]; project: { available: boolean } }): string {
  if (!item.project.available) return catalogWord("literal", "aa5b220d59fe")
  return item.condition ? CONDITIONS[item.condition] ?? catalogWord("literal", "4a8cf314ecbe") : catalogWord("literal", "3b46754552ca")
}

export function needsPerson(item: Pick<WorkV2Item, "condition" | "user_action" | "decision_id">, decisionCount: number): boolean {
  return !!item.condition || !!item.user_action || !!item.decision_id || decisionCount > 0
}

/**
 * What the person is asked to do next on a card. An Agent's waiting_user
 * points at a decision, and the card names that decision's question; its
 * answer buttons are in the item's detail. When the decision is not among the
 * open ones this read holds, the card says a question waits rather than
 * showing nothing. A free-text action (the daemon's own waits, or one written
 * before decisions) still reads as before.
 */
export function nextActionWords(
  item: Pick<WorkV2Item, "user_action" | "decision_id">,
  decisions: { id: string; question: string }[],
): string {
  if (item.decision_id) {
    const decision = decisions.find((row) => row.id === item.decision_id)
    return decision ? catalogFormat("template", "9aad7f980e59", [decision.question]) : catalogWord("literal", "712a6cdf60cf")
  }
  return item.user_action ? catalogFormat("template", "56db22485fa2", [item.user_action]) : ""
}

export function phaseStayWords(enteredAt: number | null | undefined, nowSeconds: number): string {
  if (!enteredAt) return catalogWord("literal", "e761602c4ba2")
  const minutes = Math.max(0, Math.floor((nowSeconds - enteredAt) / 60))
  if (minutes < 1) return catalogWord("literal", "3e9f7d914950")
  if (minutes < 60) return catalogFormat("template", "8ecdad22ecbb", [minutes])
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return catalogFormat("template", "26a0dc9801c8", [hours, minutes % 60 ? catalogFormat("template", "f7011fada0cd", [minutes % 60]) : ""])
  return catalogFormat("template", "f49a26639528", [Math.floor(hours / 24), hours % 24])
}

export function ownerOnlineWords(ownerSession: string | null, sessions: SessionRow[]): string {
  if (!ownerSession) return catalogWord("literal", "5d6f2c54d8ec")
  const owner = sessions.find((session) => session.sessionId === ownerSession)
  if (!owner) return catalogWord("literal", "0b7448cc7d28")
  return owner.source?.freshness === "current" && ["working", "waiting", "idle"].includes(owner.state)
    ? catalogWord("literal", "4734166d945d") : catalogWord("literal", "6d7e597d4591")
}

export function deploymentWords(item: Pick<WorkV2Item, "deployment_evidence" | "no_deployment_reason">): string {
  const evidence = item.deployment_evidence?.split(/\r?\n/).find((line) => line.trim())?.trim()
  if (evidence) return catalogFormat("template", "65519076782c", [evidence])
  const reason = item.no_deployment_reason?.split(/\r?\n/).find((line) => line.trim())?.trim()
  return reason ? catalogFormat("template", "292c86e4825b", [reason]) : catalogWord("literal", "dc8d685a64b5")
}
