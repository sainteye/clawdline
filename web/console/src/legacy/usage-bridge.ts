// The Usage page's way into its copied module.
//
// `js/view/usage.js` is the Swift app's Project Portfolio, byte for byte. It is
// bound here exactly as that app's `main.js` binds it: one table of the page's
// elements by id, and the two drawing helpers passed in rather than imported,
// because the module deliberately imports nothing of the page's own.
import { bindUsagePortfolio as bindUsagePortfolioOriginal } from "./js/view/usage.js"
import { drawIcon } from "./js/core/pixels.js"
import { tint } from "./js/core/util.js"

/** What `bindUsagePortfolio` hands back: the page's arrival and departure, and its reads. */
export interface UsagePortfolio {
  load(cursor: string | null, append: boolean): void
  render(data: unknown, append: boolean): void
  selectView(view: "overview" | "agent_work"): void
  enter(): void
  leave(): void
}

/** `main.js`'s element table for `bindUsagePortfolio`, in its order. */
export const USAGE_ELEMENT_IDS = [
  "usage-analytics",
  "usage-close", "usage-overview",
  "usage-agent-work", "usage-controls",
  "usage-range", "usage-from",
  "usage-to", "usage-timezone",
  "usage-refresh", "usage-meta",
  "usage-availability", "usage-status",
  "usage-overview-panel",
  "usage-agent-work-panel",
  "usage-measured", "usage-output-change",
  "usage-run-count", "usage-scheduled-output",
  "usage-scheduled-runs", "usage-coverage-kpi",
  "usage-unknown-count", "usage-project-count",
  "usage-project-list", "usage-project-detail",
  "usage-project-detail-title",
  "usage-project-rank", "usage-project-summary",
  "usage-project-trend", "usage-project-mix",
  "usage-project-lineage", "usage-project-recent",
  "usage-insights", "usage-schedule-body",
  "usage-unknown-schedule", "usage-feature-body",
  "usage-feature-summary",
  "usage-feature-count", "usage-feature-fold",
  "usage-unknown-feature",
  "usage-coverage-panel", "usage-coverage-list",
  "usage-export-csv", "usage-export-json",
  "usage-agent-list", "usage-more",
  "usage-detail", "usage-detail-list",
  "usage-detail-close",
] as const

/**
 * Bind the page once its markup is in the document. Every id must be there:
 * the module reads them without asking, as it does in the original.
 */
export function bindUsagePage(doc: Document): UsagePortfolio {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of USAGE_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  return (bindUsagePortfolioOriginal as (
    elements: Record<string, HTMLElement | null>,
    environment: { drawIcon: unknown; tint: unknown },
  ) => UsagePortfolio)(elements, { drawIcon, tint })
}
