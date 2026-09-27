import type { WorkV2Document, WorkV2Item } from "./api.js"
import { documentsNewestFirst } from "./completion-report-order.js"

/**
 * An Epic may not start implementing until its owning Session has written a
 * plan and a Child Session has reviewed that plan: a `plan_review` written at
 * or after the latest `plan`. The daemon enforces this; the Board only says
 * where an Epic stands so nobody presses on an Epic that is not ready.
 */
export interface EpicGate {
  /** A `plan` document exists. */
  plan: boolean
  /** A `plan_review` exists that is at least as new as the latest `plan`. */
  review: boolean
  ready: boolean
}

export const EPIC_GATE_HINT = "Epic 要先寫計劃書、經 Child Session review，才能開始實作"

export function isEpic(item: Pick<WorkV2Item, "kind">): boolean {
  return item.kind === "epic"
}

export function epicGate(documents: WorkV2Document[] | undefined): EpicGate {
  const plan = documentsNewestFirst(documents, (document) => document.role === "plan")[0]
  const review = !!plan && documentsNewestFirst(documents, (document) => document.role === "plan_review").some((r) => r.created_at >= plan.created_at)
  return { plan: !!plan, review, ready: !!plan && review }
}

const BEFORE_IMPLEMENTING = new Set<WorkV2Item["phase"]>(["created", "assigning", "assigned"])

/** The checklist is shown on an open Epic that has not yet moved past `assigned`. */
export function epicGateShown(item: Pick<WorkV2Item, "kind" | "phase" | "closed_at">): boolean {
  return isEpic(item) && !item.closed_at && BEFORE_IMPLEMENTING.has(item.phase)
}

/**
 * The plan and its reviews, read in the order they belong together: each plan,
 * newest first, followed by the reviews written after it and before the next
 * newer plan. A review older than every plan comes last.
 */
export function epicPlanDocuments(documents: WorkV2Document[] | undefined): WorkV2Document[] {
  const plans = documentsNewestFirst(documents, (document) => document.role === "plan")
  const reviews = documentsNewestFirst(documents, (document) => document.role === "plan_review")
  const out: WorkV2Document[] = []
  const placed = new Set<string>()
  let newer = Infinity
  for (const plan of plans) {
    out.push(plan)
    for (const review of reviews) {
      if (review.created_at >= plan.created_at && review.created_at < newer && !placed.has(review.id)) {
        out.push(review); placed.add(review.id)
      }
    }
    newer = plan.created_at
  }
  for (const review of reviews) if (!placed.has(review.id)) out.push(review)
  return out
}
