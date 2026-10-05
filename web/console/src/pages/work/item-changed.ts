/*
 * One page-wide signal that a Board item changed through this page.
 *
 * A Session's to-do fold opens its items in the Board's card. Completing one
 * there left the fold showing it as unfinished until its fifteen-second
 * refresh, a focus change, or a reload. The Board says so here after every
 * action the machine accepted, and the fold asks again at once.
 *
 * This file imports nothing, so a test can run it as it is.
 */

const WORK_ITEM_CHANGED = "clawdline:work-item-changed"

function pageTarget(): EventTarget | null {
  return typeof window === "undefined" ? null : window
}

/** Say that an item changed. Only after the machine accepted the change. */
export function announceWorkItemChanged(target: EventTarget | null = pageTarget()): void {
  target?.dispatchEvent(new Event(WORK_ITEM_CHANGED))
}

/** Run `changed` whenever an item changed through this page. Returns the stop. */
export function onWorkItemChanged(changed: () => void, target: EventTarget | null = pageTarget()): () => void {
  if (!target) return () => {}
  const receive = () => changed()
  target.addEventListener(WORK_ITEM_CHANGED, receive)
  return () => target.removeEventListener(WORK_ITEM_CHANGED, receive)
}
