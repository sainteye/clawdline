/**
 * A working session's clock, drawn here rather than sent.
 *
 * The line a working session draws carries the assistant's own clock —
 * "Thinking… (1m 12s · ↑ 3k tokens)" — and the clock moves every second, so a
 * daemon that compared lines published every screen refresh as a change. It
 * now leaves the clock out of the comparison and sends `working_since`, the
 * moment the turn began; this page puts the clock back into the line it was
 * sent. A row without `working_since` (an older daemon) is drawn as sent.
 */

/**
 * Where the clock is in a line, as `session.ElapsedSpan` finds it in the
 * daemon (internal/domain/session/activity.go): inside the first parenthesis
 * that holds one, numbers each followed at once by `h`, `m` or `s`, ending at
 * the seconds. Indices are UTF-16, as `slice` takes them.
 */
export function elapsedSpan(text: string): { start: number; end: number; seconds: number } | null {
  const chars = Array.from(text)
  const offset = (i: number) => chars.slice(0, i).join("").length
  const digit = (c: string | undefined) => c !== undefined && c >= "0" && c <= "9"
  for (let i = 0; i < chars.length; i++) {
    if (chars[i] !== "(") continue
    let total = 0
    let first = -1
    let j = i + 1
    while (j < chars.length && chars[j] !== ")") {
      if (!digit(chars[j])) {
        j++
        continue
      }
      const from = j
      while (j < chars.length && digit(chars[j])) j++
      if (j >= chars.length) break
      const value = Number(chars.slice(from, j).join(""))
      const unit = chars[j]
      if (unit === "h" || unit === "m") {
        total += value * (unit === "h" ? 3600 : 60)
        if (first < 0) first = from
      } else if (unit === "s") {
        if (first < 0) first = from
        return { start: offset(first), end: offset(j + 1), seconds: total + value }
      }
      j++
    }
  }
  return null
}

/** Seconds in the providers' own spelling: `12s`, `1m 12s`, `1h 2m 3s`. */
export function elapsedWords(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const rest = s % 60
  if (h > 0) return `${h}h ${m}m ${rest}s`
  if (m > 0) return `${m}m ${rest}s`
  return `${rest}s`
}

/**
 * The line with its clock read from `since` at `nowSeconds`. A line with no
 * clock, or no `since`, is the line as sent.
 */
export function liveWorkingLine(line: string, since: number | undefined, nowSeconds: number): string {
  if (typeof since !== "number" || !Number.isFinite(since) || since <= 0) return line
  const span = elapsedSpan(line)
  if (!span) return line
  return line.slice(0, span.start) + elapsedWords(nowSeconds - since) + line.slice(span.end)
}

/**
 * Keeps the clock in one element's text moving, once a second, while the
 * page is visible. Answers the function that stops it.
 */
export function runWorkingClock(element: HTMLElement, line: string, since: number): () => void {
  let timer: ReturnType<typeof setInterval> | undefined
  const paint = () => {
    const text = liveWorkingLine(line, since, Date.now() / 1000)
    if (element.textContent !== text) element.textContent = text
  }
  const start = () => {
    if (timer !== undefined) return
    paint()
    timer = setInterval(paint, 1000)
  }
  const stop = () => {
    if (timer === undefined) return
    clearInterval(timer)
    timer = undefined
  }
  const visibility = () => (document.visibilityState === "hidden" ? stop() : start())
  document.addEventListener("visibilitychange", visibility)
  visibility()
  return () => {
    stop()
    document.removeEventListener("visibilitychange", visibility)
  }
}
