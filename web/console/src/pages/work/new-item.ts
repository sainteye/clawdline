const OPEN_NEW_WORK_ITEM = "clawdline:open-new-work-item"

export type NewWorkItemDraft = {
  projectID?: string
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
