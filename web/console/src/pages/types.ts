import type { ComponentType } from "react"

/**
 * A page this console can show, registered by dropping a file into pages/.
 *
 * App picks up every `pages/*.tsx` that exports `page`, so a new page is one new
 * file and no edit to App — which is what lets two pages be built at once
 * without two people editing the same routing table.
 *
 * `id` is the original's page name (`#page=<id>`, `data-page-to`), and a page
 * that registers here makes its drawer item enabled unless it explicitly has
 * no drawer entry.
 */
export interface PageModule {
  id: string
  /** False when this page has an address but is entered through another page. */
  drawer?: boolean
  /**
   * The lowest daemon route level (`api_level` in `/v1/health`,
   * api/v1/routes.json) this page's routes need. A machine that reports a
   * lower level gets the drawer entry disabled with the needs-update word
   * (`pageNeedsUpdate`); one that reports no level is offered the page, and
   * its 501 `not_implemented` speaks for it (docs/updates.md).
   */
  requiresApiLevel?: number
  /** The page's root element, as the original's `section.page`. */
  Component: ComponentType<{ shown: boolean }>
}
