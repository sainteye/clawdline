export interface StatusLimitWindow {
  name: string
  usedPercent?: number
}

export interface StatusLimitCell {
  name: string
  value: string
  level: "" | "ok" | "warn" | "bad"
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
