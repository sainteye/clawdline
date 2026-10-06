import { catalogFormat } from "../../catalog.js"
import type { WorkV2Item } from "./api.js"

/**
 * An Epic's owning Session may break it into Feature/Issue items that carry
 * the Epic's id as `parent_id`. The Board shows that structure both ways: the
 * Epic lists its children with how far they have got, and a child names the
 * Epic it belongs to.
 *
 * The family is read from a list that includes closed items. The Board's own
 * list under 「進行中」 leaves out everything done or cancelled, and counting
 * from it would show an Epic whose children are all done as 「0 / 0」.
 */

export type EpicFamilyItem = Pick<WorkV2Item, "id" | "kind" | "title" | "phase" | "owner_session" | "parent_id">

/** The items whose parent is `epicID`, in the order the list gives them. */
export function epicChildren<T extends Pick<WorkV2Item, "parent_id">>(items: readonly T[], epicID: string): T[] {
  if (!epicID) return []
  return items.filter((item) => item.parent_id === epicID)
}

export interface EpicProgress {
  /** Children in phase `done`. */
  done: number
  /** Children that still count toward the Epic: everything except cancelled. */
  total: number
  /** Cancelled children, shown apart and never part of `total`. */
  cancelled: number
}

/**
 * How far an Epic has got. A cancelled child is work that will not be done, so
 * it is neither finished nor outstanding: it leaves the denominator and is
 * counted on its own, and an Epic whose remaining children are all done reads
 * as complete.
 */
export function epicProgress(children: readonly Pick<WorkV2Item, "phase">[]): EpicProgress {
  let done = 0
  let cancelled = 0
  for (const child of children) {
    if (child.phase === "done") done++
    else if (child.phase === "cancelled") cancelled++
  }
  return { done, total: children.length - cancelled, cancelled }
}

export function epicProgressWords(progress: EpicProgress): string {
  const words = catalogFormat("template", "7e807e36364d", [progress.done, progress.total])
  return progress.cancelled ? catalogFormat("template", "d11ed4fee18a", [words, progress.cancelled]) : words
}

export interface EpicParent {
  id: string
  /** The Epic's title when it is in the list; absent when it is not. */
  title?: string
}

/** The Epic `item` belongs to, or null when it has none. */
export function epicParent(item: Pick<WorkV2Item, "parent_id">, items: readonly Pick<WorkV2Item, "id" | "title">[]): EpicParent | null {
  const id = item.parent_id
  if (!id) return null
  const parent = items.find((candidate) => candidate.id === id)
  return parent ? { id, title: parent.title } : { id }
}

/** How a parent that is not in the list is named: the first eight characters of its id. */
export function shortWorkID(id: string): string {
  return id.slice(0, 8)
}

/** Whether the family needs a list beyond what the Board already shows. */
export function needsFamilyList(items: readonly Pick<WorkV2Item, "kind" | "parent_id">[]): boolean {
  return items.some((item) => item.kind === "epic" || !!item.parent_id)
}

/**
 * At most this many parents outside the family list are read one by one per
 * Board load. The family list is one page, so on a large Project an Epic can
 * fall outside it; past this many, a child names its Epic by short id.
 */
export const MAX_PARENT_READS = 24

/** The parents `items` name that `known` does not hold, each once, at most `limit`. */
export function missingParentIDs(items: readonly Pick<WorkV2Item, "parent_id">[], known: readonly Pick<WorkV2Item, "id">[],
  limit = MAX_PARENT_READS): string[] {
  const have = new Set(known.map((item) => item.id))
  const missing: string[] = []
  for (const item of items) {
    const id = item.parent_id
    if (!id || have.has(id)) continue
    have.add(id)
    missing.push(id)
    if (missing.length >= limit) break
  }
  return missing
}

/**
 * Reads the parents the family list lacks and returns the ones that answered.
 * A parent whose read fails stays unnamed: the child shows its short id.
 */
export async function readMissingParents<T extends Pick<WorkV2Item, "id">>(items: readonly Pick<WorkV2Item, "parent_id">[],
  known: readonly Pick<WorkV2Item, "id">[], read: (id: string) => Promise<T>, limit = MAX_PARENT_READS): Promise<T[]> {
  const ids = missingParentIDs(items, known, limit)
  if (!ids.length) return []
  const answers = await Promise.allSettled(ids.map((id) => read(id)))
  return answers.flatMap((answer) => answer.status === "fulfilled" ? [answer.value] : [])
}
