import type { WorkV2Phase } from "./api.js"

export const WORK_MILESTONES = ["實作", "驗證", "Commit / Merge", "部署", "完成"] as const

export type WorkMilestoneState = "done" | "current" | "pending"

const COMPLETED: Record<WorkV2Phase, number> = {
  created: 0,
  assigning: 0,
  assigned: 0,
  implementing: 0,
  verifying: 1,
  merging: 2,
  deploying: 3,
  done: WORK_MILESTONES.length,
  cancelled: 0,
}

const CURRENT: Partial<Record<WorkV2Phase, number>> = {
  created: 0,
  assigning: 0,
  assigned: 0,
  implementing: 0,
  verifying: 1,
  merging: 2,
  deploying: 3,
}

/** One lifecycle reading shared by Board cards and their owning Session. */
export function workMilestoneStates(phase: WorkV2Phase): WorkMilestoneState[] {
  const completed = COMPLETED[phase]
  const current = CURRENT[phase]
  return WORK_MILESTONES.map((_, index) => index < completed ? "done" : index === current ? "current" : "pending")
}
