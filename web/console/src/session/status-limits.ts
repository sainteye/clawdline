export interface StatusLimitWindow {
  name: string
  usedPercent?: number
}

export interface StatusLimitCell {
  name: string
  value: string
  level: "" | "ok" | "warn" | "bad"
  /** The daemon's sentence about the reading, for a pointer that can hover. */
  title?: string
}

/** The parts of the daemon's `limits` this row draws. */
export interface StatusLimitReading {
  windows?: readonly StatusLimitWindow[]
  /** Which kind of nothing, when no window was read. */
  unknownReason?: string
  /** The sentence naming the file the reading rests on. */
  detail?: string
  stale?: boolean
  /** A spent window whose turns are charged to the account's credits. */
  creditsInUse?: boolean
  creditBalance?: number
}

/** The words of the credits cell; without them no credits cell is drawn. */
export interface StatusCreditWords {
  name: string
  inUse: string
  balance: (balance: string) => string
  explain: string
}

/** A balance as a person reads money-like counts: whole units, grouped. */
export function creditBalanceText(balance: number | undefined, locale?: string): string | null {
  if (typeof balance !== "number" || !Number.isFinite(balance)) return null
  return Math.round(balance).toLocaleString(locale)
}

/**
 * The cell that says this Session's turns are being charged to credits. A
 * spent window alone reads as "this assistant has stopped"; Codex keeps
 * answering on a Pro account's credits, and a person who is not told spends
 * them without knowing.
 */
export function statusCreditCell(reading: StatusLimitReading | null | undefined, words: StatusCreditWords | undefined, locale?: string): StatusLimitCell | null {
  if (!reading?.creditsInUse || !words) return null
  const balance = creditBalanceText(reading.creditBalance, locale)
  return {
    name: words.name,
    value: balance ? `${words.inUse} · ${balance}` : words.inUse,
    level: "bad",
    title: balance ? `${words.explain} ${words.balance(balance)}` : words.explain,
  }
}

/**
 * The compact plan-window cells drawn at the right edge of the Status Line.
 *
 * Keep this independent of the provider: Codex currently reports only its 7d
 * window while Claude commonly reports both 5h and 7d. Every window the daemon
 * sends gets one cell; a missing or non-finite percentage is unknown rather
 * than a number that looks measured.
 */
export function statusLimitCells(windows: readonly StatusLimitWindow[], unknown: string): StatusLimitCell[] {
  return windows.map((window) => {
    const used = window.usedPercent
    const pct = typeof used === "number" && Number.isFinite(used)
      ? Math.max(0, Math.min(100, Math.round(used)))
      : null
    return {
      name: window.name,
      value: pct === null ? unknown : `${pct}%`,
      level: pct === null ? "" : pct >= 85 ? "bad" : pct >= 60 ? "warn" : "ok",
    }
  })
}

/**
 * Every cell at the right edge, including the one that says there is no reading.
 *
 * `windows: []` is not "nothing worth saying". The daemon tells four kinds of
 * nothing apart and sends the one it found as `unknownReason`, with a `detail`
 * naming the file it rests on — and Claude's 5h and 7d percentages reach a
 * machine only through the stdin of whatever `statusLine.command` names, so a
 * machine with no status line configured has no record at all and never will.
 * Drawn as zero cells, that is indistinguishable from an account with quota to
 * spare, which is how a fresh Linux install reads as "the usage is simply not
 * shown here". The status line's own wrapper makes the same point about
 * itself: a blank one looks exactly like one that was never configured. So a
 * reading that knows nothing says so, in a cell.
 *
 * A reading with no windows and no reason stays blank: that is a daemon that
 * has not answered yet, not a machine that cannot answer.
 */
export function statusLimitRow(
  reading: StatusLimitReading | null | undefined,
  words: { unknown: string; limits: string; stale?: string; source?: string; credits?: StatusCreditWords },
  locale?: string,
): StatusLimitCell[] {
  const windows = reading?.windows ?? []
  if (windows.length) {
    const cells = statusLimitCells(windows, words.unknown).map(cell => {
      if (reading?.stale && words.stale) cell.value += ` · ${words.stale}`
      if (words.source) cell.title = words.source
      return cell
    })
    const credits = statusCreditCell(reading, words.credits, locale)
    return credits ? [...cells, credits] : cells
  }
  if (!reading?.unknownReason) return []
  return [{ name: words.limits, value: words.unknown, level: "", title: reading.detail }]
}
