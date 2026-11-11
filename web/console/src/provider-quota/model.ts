import type { ProviderQuotaRow, ProviderQuotaWindow } from "@clawdline/contract"

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
/**
 * Grok may retain a proven missing reading or an expired saved sign-in, which the details explain
 * with the command that renews it; an initial provider slot is not evidence.
 */
export function visibleQuotaRows(rows: readonly ProviderQuotaRow[]): ProviderQuotaRow[] {
  return rows.flatMap((row) => {
    if (row.id === "grok" && row.problem === "not_signed_in") return []
    const windows = row.windows.filter((w) => Number.isFinite(w.used_percent) && w.used_percent >= 0 && w.used_percent <= 100)
    const observed = typeof row.observed_at === "number" && Number.isFinite(row.observed_at) && row.observed_at > 0
    return windows.length || row.id === "grok" && (row.problem === "no_reading" || row.problem === "reauth_required" || observed) ? [{ ...row, windows }] : []
  })
}
export function tightestWindow(row: ProviderQuotaRow) {
  return row.windows.reduce<ProviderQuotaWindow | undefined>((a, b) => !a || b.used_percent > a.used_percent ? b : a, undefined)
}
/** The all-models weekly window leads: it is the reading a person plans a week around. */
const weekly = (name: string) => name === "7d"
/**
 * The single window the footer shows; the other windows open with the provider's details: the all-models weekly window for every
 * provider, so a model-specific bucket such as Gemini's "Argon 7d" or a busy 5h window does not
 * stand in for it. Without a 7d window, the tightest window is shown.
 */
export function compactWindow(row: ProviderQuotaRow) {
  return row.windows.find((w) => weekly(w.name)) ?? tightestWindow(row)
}
export function compactQuotaRows(rows: readonly ProviderQuotaRow[], count: number): ProviderQuotaRow[] {
  return [...rows].sort((a, b) => {
    const left = compactWindow(a), right = compactWindow(b)
    return left && right ? right.used_percent - left.used_percent : left ? -1 : right ? 1 : 0
  }).slice(0, count)
}
/** The credits balance while credits are in use, in whole units grouped for reading; null otherwise. */
export function quotaCreditBalance(row: ProviderQuotaRow, locale?: string): string | null {
  const b = row.credit_balance
  if (!row.credits_in_use || typeof b !== "number" || !Number.isFinite(b)) return null
  return Math.round(b).toLocaleString(locale)
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
