// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord, catalogWordLanguage, currentCatalogTag, resolveTag } from "../../catalog.ts"

export interface ProjectActivityPlace {
  boardProjectId?: string
  activeItemCount?: number
  summaryCoverage?: unknown
  activitySourcePartial?: boolean
  activityReadStatus?: string
}

export function projectActivityWords(place: ProjectActivityPlace): { text: string; tone: "unknown" | "active" | "idle" } | null {
  if (!place.boardProjectId) return null
  const count = place.activeItemCount
  if (!Number.isSafeInteger(count) || count === undefined || count < 0) {
    return { text: catalogWord("literal", "e47242d55799"), tone: "unknown" }
  }
  const n = new Intl.NumberFormat(currentCatalogTag()).format(count)
  const running = catalogWord("projects", "activityInProgress").replace("{n}", n)
  if (place.activityReadStatus !== "ready") {
    return { text: catalogWord("projects", "activityLastKnown").replace("{n}", running), tone: "unknown" }
  }
  if (place.activitySourcePartial || !completeProjectSummary(place.summaryCoverage)) {
    return { text: catalogWord("projects", "activityPartial").replace("{n}", running), tone: "unknown" }
  }
  return count === 0
    ? { text: catalogWord("projects", "activityNone"), tone: "idle" }
    : { text: running, tone: "active" }
}

export function projectActivityUsesEnglishFallback(place: ProjectActivityPlace): boolean {
  if (!place.boardProjectId) return false
  const count = place.activeItemCount
  const keys = !Number.isSafeInteger(count) || count === undefined || count < 0
    ? [["literal", "e47242d55799"]]
    : place.activityReadStatus !== "ready"
      ? [["projects", "activityInProgress"], ["projects", "activityLastKnown"]]
      : place.activitySourcePartial || !completeProjectSummary(place.summaryCoverage)
        ? [["projects", "activityInProgress"], ["projects", "activityPartial"]]
        : [["projects", count === 0 ? "activityNone" : "activityInProgress"]]
  return keys.some(([domain, key]) => catalogWordLanguage(domain, key) === "en" && currentCatalogTag() !== "en")
}

export function projectOpenLabel(name: string, activity: string | null, summarizeUnknown: boolean): string {
  const open = catalogWord("legacy", "webProjectOpenLabel").replace("{name}", name)
  return open + (activity && !summarizeUnknown ? `, ${activity}` : "")
}

const pinnedStatuses: ReadonlyArray<[string, string, string]> = [
  ["Preparing the project directory in the background…", "正在背景準備專案目錄…", "statusPreparing"],
  ["The project directory is temporarily unavailable; retrying shortly.", "專案目錄暫時無法取得，稍後自動重試。", "statusUnavailable"],
  ["Showing the last available directory while it updates.", "顯示上次可用的專案目錄，背景正在更新。", "statusRetained"],
  ["Board unavailable; showing standard Projects. Your setting has not changed.", "看板暫時無法讀取，目前顯示一般專案。設定未變更。", "statusBoardUnavailable"],
  ["Update failed; showing the last available project directory. ", "更新失敗，目前保留上次的專案目錄。", "statusUpdateFailed"],
]

/** Preserve a refusal code or following machine sentence while replacing the pinned prefix. */
export function localizePinnedProjectStatus(value: string): string {
  for (const [english, chinese, key] of pinnedStatuses) {
    for (const source of [english, chinese]) {
      if (value.startsWith(source)) return catalogWord("projects", key) + value.slice(source.length)
    }
  }
  return value
}
export interface ProjectListWords {
  unknownBefore: string
  unknownAfter: string
  boardBefore: string
  boardAfter: string
}

/** Copy changes at the React seam because the copied Projects renderer is pinned. */
export function projectListWords(language: string): ProjectListWords {
  const copiedChinese = /^zh/i.test(language)
  const requested = resolveTag(language)
  const alternate = requested !== currentCatalogTag()
  return {
    unknownBefore: copiedChinese ? "進度尚未確認" : "Activity unknown",
    unknownAfter: requested === "en" && alternate ? "In-progress count needs updating" : requested === "zh-Hant" && alternate
      ? "進行中數量待更新" : catalogWord("literal", "e47242d55799"),
    boardBefore: copiedChinese ? "個工作項目 · 查看進度 →" : "work items · View progress →",
    boardAfter: requested === "zh-Hant" && alternate ? "個工作項目 · 開啟工作看板 →" : catalogWord("literal", "28ce0b145abb"),
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
