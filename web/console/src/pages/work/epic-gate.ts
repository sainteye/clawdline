import type { WorkV2Document, WorkV2Item } from "./api.js"
import { documentsNewestFirst } from "./completion-report-order.js"

/**
 * When an Epic captures planning on assignment, its owner writes a plan and
 * a Child Session reviews it before implementation. The daemon enforces the
 * captured mode; this checklist only shows that Epic's current state.
 */
export interface EpicGate {
  /** A `plan` document exists. */
  plan: boolean
  /** A `plan_review` exists that is at least as new as the latest `plan`. */
  review: boolean
  ready: boolean
}

export const EPIC_GATE_HINT = "本輪已啟用規劃 gate：Epic 要先寫計劃書、經 Child Session review，才能開始實作"

export function isEpic(item: Pick<WorkV2Item, "kind">): boolean {
  return item.kind === "epic"
}

/** The Board does not show an empty gate panel or ask a person to supply its contract. */
export function epicGateDetailShown(item: Pick<WorkV2Item, "kind" | "acceptance_criteria">): boolean {
  return isEpic(item) && !!item.acceptance_criteria.trim()
}

export function epicGate(documents: WorkV2Document[] | undefined): EpicGate {
  const plan = documentsNewestFirst(documents, (document) => document.role === "plan")[0]
  const review = !!plan && documentsNewestFirst(documents, (document) => document.role === "plan_review").some((r) => r.created_at >= plan.created_at)
  return { plan: !!plan, review, ready: !!plan && review }
}

const BEFORE_IMPLEMENTING = new Set<WorkV2Item["phase"]>(["created", "assigning", "assigned"])

/** Show the checklist only after this Epic has captured an enabled planning gate. */
export function epicGateShown(item: Pick<WorkV2Item, "kind" | "phase" | "closed_at" | "gate_snapshot_cycle" | "planning_gate">): boolean {
  return isEpic(item) && item.gate_snapshot_cycle > 0 && item.planning_gate && !item.closed_at && BEFORE_IMPLEMENTING.has(item.phase)
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
