import type { SessionRow } from "@clawdline/contract"
import type { WorkV2Item } from "./api.js"

const CONDITIONS: Record<string, string> = {
  blocked: "工作受阻",
  waiting_user: "等待你回應",
  owner_required: "尚待指派",
  owner_offline: "負責 Session 未在線",
  evidence_unknown: "進度證據待確認",
  assignment_failed: "指派失敗",
  assigned_unnotified: "尚未通知負責 Session",
}

export function conditionWords(item: { condition: WorkV2Item["condition"]; project: { available: boolean } }): string {
  if (!item.project.available) return "專案目前無法使用"
  return item.condition ? CONDITIONS[item.condition] ?? "狀態待確認" : "正常"
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
    return decision ? `等你決定：${decision.question}` : "等你回答一個問題"
  }
  return item.user_action ? `下一步：${item.user_action}` : ""
}

export function phaseStayWords(enteredAt: number | null | undefined, nowSeconds: number): string {
  if (!enteredAt) return "停留時間不明"
  const minutes = Math.max(0, Math.floor((nowSeconds - enteredAt) / 60))
  if (minutes < 1) return "停留不到 1 分鐘"
  if (minutes < 60) return `停留 ${minutes} 分鐘`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `停留 ${hours} 小時${minutes % 60 ? ` ${minutes % 60} 分鐘` : ""}`
  return `停留 ${Math.floor(hours / 24)} 天 ${hours % 24} 小時`
}

export function ownerOnlineWords(ownerSession: string | null, sessions: SessionRow[]): string {
  if (!ownerSession) return "尚無負責 Session"
  const owner = sessions.find((session) => session.sessionId === ownerSession)
  if (!owner) return "負責 Session 未在線"
  return owner.source?.freshness === "current" && ["working", "waiting", "idle"].includes(owner.state)
    ? "負責 Session 在線" : "負責 Session 在線狀態不明"
}

export function deploymentWords(item: Pick<WorkV2Item, "deployment_evidence" | "no_deployment_reason">): string {
  const evidence = item.deployment_evidence?.split(/\r?\n/).find((line) => line.trim())?.trim()
  if (evidence) return `部署證據：${evidence}`
  const reason = item.no_deployment_reason?.split(/\r?\n/).find((line) => line.trim())?.trim()
  return reason ? `未部署：${reason}` : "尚無部署說明"
}
