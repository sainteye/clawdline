// @ts-expect-error -- Node runs work-milestones.test.ts against this source file.
import { localizedLiteralList } from "../../catalog.ts"
import type { WorkV2Phase } from "./api.js"

export const WORK_MILESTONES = localizedLiteralList(["b1b5c6f84f09", "56e232762104", "218220af5c8e", "1882d286da02", "ffa731746caa"])

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
  implementing: 0,
  verifying: 1,
  merging: 2,
  deploying: 3,
}

const STARTED = new Set<WorkV2Phase>(["implementing", "verifying", "merging", "deploying", "done"])

/** Progress begins with implementation; assignment is preparation, not work underway. */
export function workMilestonesShown(phase: WorkV2Phase): boolean {
  return STARTED.has(phase)
}

/** One lifecycle reading shared by Board cards and their owning Session. */
export function workMilestoneStates(phase: WorkV2Phase): WorkMilestoneState[] {
  const completed = COMPLETED[phase]
  const current = CURRENT[phase]
  return WORK_MILESTONES.map((_, index) => index < completed ? "done" : index === current ? "current" : "pending")
}

export type WorkMilestone = { label: (typeof WORK_MILESTONES)[number]; state: WorkMilestoneState }

/**
 * The milestones an item's own line has. With its captured verify gate off,
 * implementing goes straight to deploying, so 驗證 and 合併 are not shown as
 * empty slots — unless the item is standing in one of them, as an item that
 * walked the long line before the gate was off still may.
 */
export function workMilestones(phase: WorkV2Phase, verifyGate: boolean): WorkMilestone[] {
  const states = workMilestoneStates(phase)
  const full = verifyGate || phase === "verifying" || phase === "merging"
  return WORK_MILESTONES.map((label, index) => ({ label, state: states[index] }))
    .filter((_, index) => full || (index !== 1 && index !== 2))
}
