/**
 * Attention belongs to the work that explains it. These small selectors keep
 * every question inside its item and every proposal inside its Project.
 */
export function decisionsForWorkItem<T extends { work_id: string | null }>(rows: T[], workID: string): T[] {
  return rows.filter((row) => row.work_id === workID)
}

/** Only a confirmed answer may remove a choice from the local Board snapshot. */
export async function confirmDecisionAnswer(answer: () => Promise<unknown>, confirmed: () => void): Promise<void> {
  await answer()
  confirmed()
}

export type DecisionAnswerStatus = { phase: "pending" | "confirmed" | "retry" | "rejected"; option: string; label: string; message?: string; workID: string; question: string }

/** Only an answer for the selected option proves this submission succeeded. */
export function matchingDecisionAnswer(decision: { state: string; answer?: string | null }, option: string): boolean {
  return decision.state === "answered" && decision.answer === option
}

export function withoutAnsweredDecision<T extends { id: string }>(rows: T[], decisionID: string): T[] {
  return rows.filter((row) => row.id !== decisionID)
}

export function withoutAnsweredWait<T extends { decision_id?: string; condition: string | null; user_action: string }>(item: T, decisionID: string): T {
  if (item.decision_id !== decisionID) return item
  return { ...item, decision_id: undefined, condition: item.condition === "waiting_user" ? null : item.condition, user_action: "" }
}

export function proposalsForProject<T extends { project_id: string }>(rows: T[], projectID: string): T[] {
  return projectID ? rows.filter((row) => row.project_id === projectID) : rows
}
