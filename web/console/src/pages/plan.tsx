import { useLayoutEffect, useRef, type MouseEvent } from "react"
import type { PageModule } from "./types.js"
import { bindPlan, type PlanPage } from "../legacy/plan-bridge.js"
import sectionMarkup from "./plan/section.html?raw"

/**
 * The Plan page: `section#plan` in the Swift app's `index.html`, drawn by its
 * `view/plan.js`.
 *
 * The fragment beside this file is that section's markup between its own tags
 * (index.html lines 960–1001), whitespace included, and the copied module
 * fills it. React owns the section element and nothing inside it.
 *
 * This is the page the Mac serves, so there is no billing client, and the
 * module draws the state it draws for exactly that: `not_here` — Free, the
 * sentence that plans are managed in the hosted console, and a link there.
 * No control on it can start a payment. See legacy/plan-bridge.ts.
 *
 * What the original's `main.js` and page registry do for this page is done
 * here: bind once, `enter` on arrival, `leave` on departure, the keyboard lands
 * on the heading (`focus: "plan-title"`), and the close button's
 * `data-page-to` goes where it says. Escape is App's, as for every page but the
 * list.
 *
 * Not carried over, because both are the hosted console's: the wait for a
 * checkout webhook (`returning`, which with no billing client ends in the same
 * `not_here` state) and holding the Cloud door back while the page is open.
 */
function PlanPageView({ shown }: { shown: boolean }) {
  const page = useRef<PlanPage | null>(null)
  const was = useRef(false)

  useLayoutEffect(() => {
    if (!page.current) page.current = bindPlan(document)
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    // `enter` paints the page's furniture from the catalog, so on a cold start
    // at `#page=plan` it waits for the words, as the original's `paintLabels`
    // is written to.
    const arrive = () => {
      if (!was.current) return
      void page.current?.enter()
      document.getElementById("plan-title")?.focus({ preventScroll: true })
    }
    const root = document.documentElement
    if (!root.classList.contains("booting")) {
      arrive()
      return
    }
    const watch = new MutationObserver(() => {
      if (root.classList.contains("booting")) return
      watch.disconnect()
      arrive()
    })
    watch.observe(root, { attributes: true, attributeFilter: ["class"] })
    return () => watch.disconnect()
  }, [shown])

  return (
    <section
      className="page page-plan"
      id="plan"
      data-page-view="plan"
      aria-labelledby="plan-title"
      hidden={!shown}
      onClick={followPageLink}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/** `core/pages.js`'s delegate for the close button, which names its page in `data-page-to`. */
function followPageLink(ev: MouseEvent<HTMLElement>): void {
  const node = (ev.target as Element).closest?.("[data-page-to]")
  if (!node || !ev.currentTarget.contains(node)) return
  const name = node.getAttribute("data-page-to")
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (!row || row.disabled) return
  ev.preventDefault()
  row.click()
}

export const page: PageModule = { id: "plan", Component: PlanPageView }
