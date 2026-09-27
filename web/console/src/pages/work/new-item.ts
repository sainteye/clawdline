import type { WorkV2Item } from "./api.js"

const OPEN_NEW_WORK_ITEM = "clawdline:open-new-work-item"
const OPEN_WORK_ITEM = "clawdline:open-work-item"

export type NewWorkItemDraft = {
  projectID?: string
  project?: {
    id: string
    label: string
    path: string
    icon?: unknown
  }
  kind?: "feature" | "issue" | "epic" | "refactor" | "plan"
  title?: string
  description?: string
}

/** Open the person-owned Board-item flow without changing the page underneath it. */
export function openNewWorkItem(draft: NewWorkItemDraft = {}): void {
  window.dispatchEvent(new CustomEvent<NewWorkItemDraft>(OPEN_NEW_WORK_ITEM, { detail: draft }))
}

/** The Board page stays mounted while hidden, so it can own one shared modal. */
export function onOpenNewWorkItem(open: (draft: NewWorkItemDraft) => void): () => void {
  const receive = (event: Event) => open((event as CustomEvent<NewWorkItemDraft>).detail ?? {})
  window.addEventListener(OPEN_NEW_WORK_ITEM, receive)
  return () => window.removeEventListener(OPEN_NEW_WORK_ITEM, receive)
}

/** Open an existing Board item in the Board's own card, from anywhere the Board is not shown. */
export function openWorkItem(item: WorkV2Item): void {
  window.dispatchEvent(new CustomEvent<WorkV2Item>(OPEN_WORK_ITEM, { detail: item }))
}

export function onOpenWorkItem(open: (item: WorkV2Item) => void): () => void {
  const receive = (event: Event) => open((event as CustomEvent<WorkV2Item>).detail)
  window.addEventListener(OPEN_WORK_ITEM, receive)
  return () => window.removeEventListener(OPEN_WORK_ITEM, receive)
}
