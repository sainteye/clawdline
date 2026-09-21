import { useLayoutEffect, useRef, type MouseEvent } from "react"
import type { PageModule } from "./types.js"
import { bindUsagePage, type UsagePortfolio } from "../legacy/usage-bridge.js"
import sectionMarkup from "./usage/section.html?raw"
import dialogMarkup from "./usage/dialog.html?raw"
import { translateUsage } from "./usage/zh-Hant.js"

/**
 * The Usage page: `section#usage-analytics` and `dialog#usage-detail` in the
 * Swift app's `index.html`, drawn by its `view/usage.js`.
 *
 * The two fragments beside this file are that markup as it stands between
 * the elements' own tags (index.html lines 723–811 and 815–816), whitespace
 * included, and the copied module fills them — so the rows, cards and
 * sentences are the original's own. `static.js` translates only the drawer
 * row, so this wrapper repaints the copied page after every draw when the
 * chosen language is Traditional Chinese. React owns the two outer elements
 * and nothing inside them.
 *
 * What the original's page registry does for this page is done here:
 * `enter` on arrival asks for the portfolio, `leave` on departure, the
 * keyboard lands on "Back to sessions" (`focus: "usage-close"`), and a
 * `data-page-to` control goes where it says (`core/pages.js`'s delegate).
 *
 * The drawer row, `usage-open`, stays as App draws it: hidden in the markup,
 * and shown only by a board answer carrying `enabled: false`
 * (`BoardControls.apply`). The Swift app answers `enabled: true` here and this
 * daemon answers without the field, so neither shows it; `#page=usage` is the
 * way in on both.
 */
function UsagePage({ shown }: { shown: boolean }) {
  const portfolio = useRef<UsagePortfolio | null>(null)
  const was = useRef(false)

  // Bound once, after the markup is in the document, as `main.js` binds it at
  // load. Guarded, because a second bind would add a second listener to every
  // control.
  useLayoutEffect(() => {
    if (!portfolio.current) portfolio.current = bindUsagePage(document)
    const roots = [document.getElementById("usage-analytics"), document.getElementById("usage-detail")].filter(
      (root): root is HTMLElement => !!root,
    )
    const repaint = () => {
      for (const root of roots) translateUsage(root, document.documentElement.lang)
    }
    const watch = new MutationObserver(repaint)
    for (const root of roots) {
      watch.observe(root, {
        subtree: true,
        childList: true,
        characterData: true,
        attributes: true,
        attributeFilter: ["aria-label", "title", "data-label"],
      })
    }
    repaint()
    return () => watch.disconnect()
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (shown) {
      portfolio.current?.enter()
      const page = document.getElementById("usage-analytics")
      const detail = document.getElementById("usage-detail")
      if (page) translateUsage(page, document.documentElement.lang)
      if (detail) translateUsage(detail, document.documentElement.lang)
      document.getElementById("usage-close")?.focus({ preventScroll: true })
    } else {
      portfolio.current?.leave()
    }
  }, [shown])

  return (
    <>
      <section
        className="usage-analytics page"
        id="usage-analytics"
        data-page-view="usage"
        aria-labelledby="usage-title"
        hidden={!shown}
        onClick={followPageLink}
        dangerouslySetInnerHTML={{ __html: sectionMarkup }}
      />
      <dialog
        className="usage-detail"
        id="usage-detail"
        aria-labelledby="usage-detail-title"
        dangerouslySetInnerHTML={{ __html: dialogMarkup }}
      />
    </>
  )
}

/**
 * `core/pages.js`'s delegate for the controls inside this page: "Back to
 * sessions" names its page in `data-page-to`. The move is the drawer row's,
 * so it is the move the drawer makes.
 */
function followPageLink(ev: MouseEvent<HTMLElement>): void {
  const node = (ev.target as Element).closest?.("[data-page-to]")
  if (!node || !ev.currentTarget.contains(node)) return
  const name = node.getAttribute("data-page-to")
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (!row || row.disabled) return
  ev.preventDefault()
  row.click()
}

export const page: PageModule = { id: "usage", Component: UsagePage }
