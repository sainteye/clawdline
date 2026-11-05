import type { WorkV2Step } from "./api.js"

export function stepState(step: Pick<WorkV2Step, "done" | "status" | "cancelled_at">): "todo" | "done" | "cancelled" {
  if (step.status === "cancelled" || step.cancelled_at != null) return "cancelled"
  return step.done ? "done" : "todo"
}

export function stepIsOpen(step: Pick<WorkV2Step, "done" | "status" | "cancelled_at">): boolean {
  return stepState(step) === "todo"
}
