import type { UsageOverview, UsageOverviewDay } from "./api.js"

export type UsageMetric = "tokens" | "dispatches" | "duration"

export function dailyValue(day: UsageOverviewDay | undefined, metric: UsageMetric): number | null {
  if (!day) return null
  if (metric === "duration") return day.duration_samples > 0 && day.duration_median_seconds !== null ? day.duration_median_seconds / 60 : null
  return metric === "tokens" ? day.tokens : day.dispatches
}

/** Adjacent missing points split a path; they never acquire an invented baseline. */
export function lineSegments(values: readonly (number | null)[], maximum: number, width: number): string[] {
  const paths: string[] = []
  let path = ""
  values.forEach((value, index) => {
    if (value === null) { if (path) paths.push(path); path = ""; return }
    const x = 54 + index * (width - 78) / Math.max(1, values.length - 1)
    const y = 174 - value / maximum * 150
    path += `${path ? " L" : "M"}${x.toFixed(2)},${y.toFixed(2)}`
  })
  if (path) paths.push(path)
  return paths
}

export function comparisonComplete(data: UsageOverview): boolean {
  return data.coverage.state === "complete" && !data.coverage.truncated && data.daily.length === data.days
    && data.previous_daily.length === data.days
    && [...data.daily, ...data.previous_daily].every(day => day.coverage === "complete")
}

export function percentageChange(current: number | null, previous: number | null, complete: boolean): string | null {
  if (!complete || current === null || previous === null || previous === 0) return null
  const percent = (current - previous) / previous * 100
  return `${percent > 0 ? "+" : ""}${Math.round(percent)}%`
}

export function compactValue(value: number | null): string {
  if (value === null) return "—"
  if (value >= 1_000_000_000) return `${Number((value / 1_000_000_000).toFixed(1))}B`
  if (value >= 1_000_000) return `${Number((value / 1_000_000).toFixed(1))}M`
  if (value >= 1_000) return `${Number((value / 1_000).toFixed(1))}K`
  return Number(value.toFixed(1)).toLocaleString()
}

/** Interpret the daemon's calendar date in UTC, independently of the viewer's time zone. */
function utcDate(date: string): Date { return new Date(`${date}T00:00:00Z`) }
export function shortDate(date: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { month: "numeric", day: "numeric", timeZone: "UTC" }).format(utcDate(date))
}

/** Select the catalog's singular or general sample sentence for its actual locale. */
export function sampleLabel(count: number, locale: string, one: string, other: string): string {
  const phrase = new Intl.PluralRules(locale).select(count) === "one" ? one : other
  return phrase.replace("{count}", String(count))
}

export function selectedDay(index: number, key: string, count: number): number {
  if (key === "Home") return 0
  if (key === "End") return Math.max(0, count - 1)
  return Math.max(0, Math.min(count - 1, index + (key === "ArrowLeft" ? -1 : key === "ArrowRight" ? 1 : 0)))
}

/** A superseded request cannot publish success or failure, even when transport ignores abort. */
export class UsageRequestScope {
  private controller: AbortController | null = null
  begin(): AbortSignal { this.cancel(); this.controller = new AbortController(); return this.controller.signal }
  cancel(): void { this.controller?.abort(); this.controller = null }
  current(signal: AbortSignal): boolean { return signal === this.controller?.signal && !signal.aborted }
}

/** Show explicit localized calendar endpoints, including both years across a boundary. */
export function dateRange(first: string | undefined, last: string | undefined, locale: string): string {
  const format = new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric", timeZone: "UTC" })
  if (!first || !last) return first || last ? format.format(utcDate((first || last)!)) : ""
  if (first === last) return format.format(utcDate(first))
  return `${format.format(utcDate(first))} – ${format.format(utcDate(last))}`
}
