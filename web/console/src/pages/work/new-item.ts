const OPEN_NEW_WORK_ITEM = "clawdline:open-new-work-item"

/** Open the person-owned Board-item flow without changing the page underneath it. */
export function openNewWorkItem(): void {
  window.dispatchEvent(new Event(OPEN_NEW_WORK_ITEM))
}

/** The Board page stays mounted while hidden, so it can own one shared modal. */
export function onOpenNewWorkItem(open: () => void): () => void {
  window.addEventListener(OPEN_NEW_WORK_ITEM, open)
  return () => window.removeEventListener(OPEN_NEW_WORK_ITEM, open)
}
