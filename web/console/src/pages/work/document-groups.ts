import type { WorkV2Document } from "./api.js"

/** The stable reading order for roles the daemon knows today. */
const DOCUMENT_ROLE_ORDER = [
  "plan", "plan_review", "spec", "design", "test", "deploy", "completion_report", "other",
] as const

const ROLE_RANK = new Map<string, number>(DOCUMENT_ROLE_ORDER.map((role, index) => [role, index]))

export interface WorkDocumentGroup {
  role: string
  documents: WorkV2Document[]
}

/**
 * Give every document exactly one role group. Known roles keep the Board's
 * reading order; future roles follow them instead of disappearing.
 */
export function groupDocumentsByRole(documents: WorkV2Document[] | undefined): WorkDocumentGroup[] {
  const grouped = new Map<string, WorkV2Document[]>()
  for (const document of documents ?? []) {
    const role = String(document.role)
    grouped.set(role, [...(grouped.get(role) ?? []), document])
  }
  return [...grouped].map(([role, entries]) => ({
    role,
    documents: entries.sort((left, right) => right.created_at - left.created_at || right.id.localeCompare(left.id)),
  })).sort((left, right) => {
    const leftRank = ROLE_RANK.get(left.role) ?? DOCUMENT_ROLE_ORDER.length
    const rightRank = ROLE_RANK.get(right.role) ?? DOCUMENT_ROLE_ORDER.length
    return leftRank - rightRank || left.role.localeCompare(right.role)
  })
}
