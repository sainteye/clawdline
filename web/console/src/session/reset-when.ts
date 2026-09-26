/**
 * When a plan window resets, as a person reads a moment still ahead.
 *
 * The info card used to hand `resetsAt` to `clockOf`, which reads a moment
 * already past: a future moment has a negative age, which is under a minute,
 * so every window said it reset "just now". A 7d window also needs its day,
 * since "04:00" alone reads as today.
 *
 * - under an hour ahead: minutes from now;
 * - later the same local day: the clock time;
 * - any other day: weekday, date and clock time;
 * - already past (a stale reading): the clock time and date, never "just now".
 */
export function resetWhen(
  unix: number,
  nowUnix: number,
  lang: string,
  inMinutes: (n: number) => string,
): string {
  const at = new Date(unix * 1000)
  const now = new Date(nowUnix * 1000)
  const ahead = unix - nowUnix
  if (ahead > 0 && ahead < 3600) return inMinutes(Math.max(1, Math.ceil(ahead / 60)))
  const clock = pad(at.getHours()) + ":" + pad(at.getMinutes())
  const sameDay = at.getFullYear() === now.getFullYear() && at.getMonth() === now.getMonth() &&
    at.getDate() === now.getDate()
  if (sameDay) return clock
  const day = at.toLocaleDateString(lang, { weekday: "short", month: "numeric", day: "numeric" })
  return day + " " + clock
}

function pad(n: number): string {
  return (n < 10 ? "0" : "") + n
}
