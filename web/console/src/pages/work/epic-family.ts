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
  const words = `${progress.done} / ${progress.total} 已完成`
  return progress.cancelled ? `${words} · ${progress.cancelled} 已取消` : words
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
