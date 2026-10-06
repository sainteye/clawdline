import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import type { WorkV2Item } from "./api.js"

/** The item's steps nobody has ticked, which a manual completion leaves open. */
export function openSteps(item: Pick<WorkV2Item, "steps">): number {
  return (item.steps ?? []).filter((step) => !step.done).length
}

/**
 * What the person reads before closing an item by hand. The daemon records
 * the open steps without refusing, so the count is said here, before the press.
 */
export function completeConfirmWords(item: Pick<WorkV2Item, "steps">): string {
  const open = openSteps(item)
  return open > 0 ? catalogFormat("template", "61504537731d", [open]) : catalogWord("literal", "ee952fba0441")
}
