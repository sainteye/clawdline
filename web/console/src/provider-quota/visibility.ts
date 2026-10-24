// @ts-expect-error -- Node's source tests import the TypeScript module directly.
import { QUOTA_VISIBLE_KEY, readQuotaVisible, saveQuotaVisible, type QuotaLayout } from "./model.ts"
const read = () => ({ desktop: readQuotaVisible(undefined, "desktop"), mobile: readQuotaVisible(undefined, "mobile") })
let current = read()
const listeners = new Set<() => void>()
export const quotaVisible = () => current
export function subscribeQuotaVisible(listener: () => void): () => void {
  listeners.add(listener)
  const storage = (event: StorageEvent) => {
    if (event.key === QUOTA_VISIBLE_KEY || event.key?.startsWith(`${QUOTA_VISIBLE_KEY}.`) || event.key === null) {
      current = read()
      for (const fn of listeners) fn()
    }
  }
  window.addEventListener("storage", storage)
  return () => { listeners.delete(listener); window.removeEventListener("storage", storage) }
}
export function setQuotaVisible(layout: QuotaLayout, on: boolean): boolean {
  current = { ...current, [layout]: on }
  const saved = saveQuotaVisible(on, undefined, layout)
  for (const fn of listeners) fn()
  return saved
}
