import type { ComponentType } from "react"

/**
 * A page this console can show, registered by dropping a file into pages/.
 *
 * App picks up every `pages/*.tsx` that exports `page`, so a new page is one new
 * file and no edit to App — which is what lets two pages be built at once
 * without two people editing the same routing table.
 *
 * `id` is the original's page name (`#page=<id>`, `data-page-to`), and a page
 * that registers here is the one that makes its drawer item enabled.
 */
export interface PageModule {
  id: string
  /** The page's root element, as the original's `section.page`. */
  Component: ComponentType<{ shown: boolean }>
}
