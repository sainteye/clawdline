import type { ProviderQuotaRow } from "@clawdline/contract"

export type QuotaLayout = "desktop" | "mobile"
export const quotaVisibilityKey = (layout: QuotaLayout) => `${QUOTA_VISIBLE_KEY}.${layout}`
export const QUOTA_VISIBLE_KEY = "clawdline.provider-quota-visible"
type ReadStore = Pick<Storage, "getItem">
type WriteStore = Pick<Storage, "setItem">
export function quotaStorage(): Storage | null {
  try { return typeof localStorage === "undefined" ? null : localStorage } catch { return null }
}
export function readQuotaVisible(store: ReadStore | null = quotaStorage(), layout: QuotaLayout = "desktop"): boolean {
  try { return (store?.getItem(quotaVisibilityKey(layout)) ?? store?.getItem(QUOTA_VISIBLE_KEY)) !== "false" } catch { return true }
}
export function saveQuotaVisible(on: boolean, store: WriteStore | null = quotaStorage(), layout: QuotaLayout = "desktop"): boolean {
  try { if (!store) return false; store.setItem(quotaVisibilityKey(layout), String(on)); return true } catch { return false }
}
/** Unknown is not zero. A provider enters the footer only after supplying a number. */
export function visibleQuotaRows(rows: readonly ProviderQuotaRow[]): ProviderQuotaRow[] {
  return rows.flatMap((row) => {
    const windows = row.windows.filter((w) => Number.isFinite(w.used_percent) && w.used_percent >= 0 && w.used_percent <= 100)
    return windows.length ? [{ ...row, windows }] : []
  })
}
export function tightestWindow(row: ProviderQuotaRow) {
  return row.windows.reduce((a, b) => b.used_percent > a.used_percent ? b : a)
}
/**
 * In compact / mobile layouts, select the window to display for a provider.
 * For Gemini, default to all models' weekly limit ("7d") rather than a model-specific
 * bucket like "Argon 7d". For other providers (or when "7d" is absent), choose the tightest window.
 */
export function compactWindow(row: ProviderQuotaRow) {
  if (row.id === "gemini") {
    const allModels7d = row.windows.find((w) => w.name === "7d")
    if (allModels7d) return allModels7d
  }
  return tightestWindow(row)
}
export function compactQuotaRows(rows: readonly ProviderQuotaRow[], count: number): ProviderQuotaRow[] {
  return [...rows].sort((a, b) => compactWindow(b).used_percent - compactWindow(a).used_percent).slice(0, count)
}
export function quotaStale(row: ProviderQuotaRow, now: number): boolean {
  return row.stale || row.windows.some((w) => w.resets_at !== undefined && w.resets_at <= now)
}
export function quotaCountdown(reset: number | undefined, now: number): string | null {
  if (reset === undefined || !Number.isFinite(reset)) return null
  if (reset <= now) return ""
  const minutes = Math.ceil((reset - now) / 60)
  const days = Math.floor(minutes / 1440), hours = Math.floor(minutes % 1440 / 60), mins = minutes % 60
  return days ? `${days}d ${hours}h` : hours ? `${hours}h ${mins}m` : `${mins}m`
}
