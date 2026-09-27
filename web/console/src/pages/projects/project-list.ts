export interface ProjectListWords {
  unknownBefore: string
  unknownAfter: string
  boardBefore: string
  boardAfter: string
}

/** Copy changes at the React seam because the copied Projects renderer is pinned. */
export function projectListWords(language: string): ProjectListWords {
  return /^zh/i.test(language) ? {
    unknownBefore: "進度尚未確認",
    unknownAfter: "進行中數量待更新",
    boardBefore: "個工作項目 · 查看進度 →",
    boardAfter: "個工作項目 · 開啟工作看板 →",
  } : {
    unknownBefore: "Activity unknown",
    unknownAfter: "In-progress count unavailable",
    boardBefore: "work items · View progress →",
    boardAfter: "work items · Open work board →",
  }
}

export function replaceSuffix(value: string, before: string, after: string): string {
  return value.endsWith(before) ? value.slice(0, -before.length) + after : value
}

/** Repeating the same unknown badge on every row adds no comparison value. */
export function activityUnknownScope(rows: number, unknown: number): "none" | "row" | "list" {
  if (unknown === 0) return "none"
  return rows > 0 && unknown === rows ? "list" : "row"
}

export function completeProjectSummary(coverage: unknown): boolean {
  return coverage === "complete"
    || (!!coverage && typeof coverage === "object" && (coverage as { status?: unknown }).status === "complete")
}

/** Counts from the retired catalog do not describe the Board the row opens. */
export function currentProjectSummary(project: Record<string, unknown>): Record<string, unknown> {
  if (completeProjectSummary(project.summaryCoverage)) return project
  const { activeItemCount: _active, itemCount: _items, ...withoutHistoricalCounts } = project
  return withoutHistoricalCounts
}
