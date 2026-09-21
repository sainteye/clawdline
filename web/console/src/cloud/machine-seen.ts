import { nextWord } from "../next-strings.js"

/**
 * Last-seen evidence in the page's language.
 *
 * The timestamp is the newest authenticated machine envelope this browser
 * observed. `null` therefore means "this browser has never seen a report",
 * not "the machine is offline" and not a guessed age.
 */
export function machineSeenWord(
  at: number | null,
  now = Date.now(),
  locale = typeof document === "undefined" ? undefined : document.documentElement.lang || undefined,
): string {
  if (!at) return nextWord("cloudForgetAskUnknown")
  const seconds = Math.min(0, (at - now) / 1000)
  const absolute = Math.abs(seconds)
  const unit: Intl.RelativeTimeFormatUnit = absolute < 90 * 60 ? "minute" : absolute < 36 * 3600 ? "hour" : "day"
  const size = unit === "minute" ? 60 : unit === "hour" ? 3600 : 86400
  const value = Math.round(seconds / size)
  try {
    const relative = new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(value, unit)
    return nextWord("cloudMachineSeen", { time: relative })
  } catch {
    // refusal-ok: a browser refusing a locale is not a machine refusal.
    return nextWord("cloudMachineSeenAt", { time: new Date(at).toLocaleString() })
  }
}
