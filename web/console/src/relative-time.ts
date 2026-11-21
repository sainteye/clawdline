/** Show past activity in the same minute, hour, or day units across lists. */
export function lastActivityRelative(at: number, now: number, locale?: string): string {
  if (!Number.isFinite(at) || at <= 0) return ""
  // A producer clock ahead of this browser still means now.
  const seconds = Math.min(0, at - now / 1000)
  const elapsed = Math.abs(seconds)
  const unit: Intl.RelativeTimeFormatUnit = elapsed < 90 * 60 ? "minute" : elapsed < 36 * 3600 ? "hour" : "day"
  const size = unit === "minute" ? 60 : unit === "hour" ? 3600 : 86400
  const value = Math.round(seconds / size)
  try {
    return new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(value, unit)
  } catch {
    return new Intl.RelativeTimeFormat(undefined, { numeric: "auto" }).format(value, unit)
  }
}
