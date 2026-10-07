import { useLayoutEffect, useRef, type MouseEvent } from "react"
import type { PageModule } from "./types.js"
import { bindPlan, type PlanPage } from "../legacy/plan-bridge.js"
import { useCloudAccount } from "../cloud/account-context.js"
import sectionMarkup from "./plan/section.html?raw"

/**
 * The Plan page: `section#plan` in the Swift app's `index.html`, drawn by its
 * `view/plan.js`.
 *
 * The fragment beside this file is that section's markup between its own tags
 * (index.html lines 960–1001), whitespace included, and the copied module
 * fills it. React owns the section element and nothing inside it.
 *
 * A hosted browser uses its signed-in Cloud account to read its real plan;
 * the local console keeps the copied module's `not_here` state.
 *
 * What the original's `main.js` and page registry do for this page is done
 * here: bind once, `enter` on arrival, `leave` on departure, the keyboard lands
 * on the heading (`focus: "plan-title"`), and the close button's
 * `data-page-to` goes where it says. Escape is App's, as for every page but the
 * list.
 *
 * The hosted checkout returns to `/billing/done`. This page recognizes that
 * arrival and lets the copied module wait for the entitlement webhook.
 */
function PlanPageView({ shown }: { shown: boolean }) {
  const account = useCloudAccount()
  const page = useRef<PlanPage | null>(null)
  const boundOrigin = useRef<string | null>(null)
  const entered = useRef(false)
  const checkoutReturn = useRef(location.pathname === "/billing/done")

  useLayoutEffect(() => {
    const origin = account?.apiOrigin ?? null
    if (!page.current || boundOrigin.current !== origin) {
      page.current?.leave()
      page.current = bindPlan(document, origin)
      boundOrigin.current = origin
      entered.current = false
    }
    if (!shown) {
      page.current?.leave()
      entered.current = false
      return
    }
    const returning = !!account && checkoutReturn.current
    if (returning) history.replaceState(history.state, "", "/#page=plan")
    if (entered.current) return
    // `enter` paints the page's furniture from the catalog, so on a cold start
    // at `#page=plan` it waits for the words, as the original's `paintLabels`
    // is written to.
    const arrive = () => {
      if (!shown || entered.current) return
      entered.current = true
      checkoutReturn.current = false
      void page.current?.enter({ returning })
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
  }, [shown, account?.apiOrigin])

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
