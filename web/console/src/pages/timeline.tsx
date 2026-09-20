import { useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import { openBoard } from "../legacy/board-bridge.js"
import { bindTimeline, requestedTimeline, type TimelinePage } from "../legacy/timeline-bridge.js"
import { ActionConfirm, Info, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./timeline/section.html?raw"

/**
 * The Project Timeline: `section#timeline` in the Swift app's `index.html`,
 * drawn by its `view/timeline.js`.
 *
 * The fragment beside this file is that section's markup between its own tags,
 * whitespace included, and the copied module fills it — the date groups, the
 * cards, the work-item pills, the evidence list and the filters are the
 * original's own DOM.
 *
 * It is not a drawer row, there or here: a timeline is one Project's, so it is
 * reached from that Project's board (`board-timeline-tab`), which is what hands
 * it the Project through `openTimeline`.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter(project)` on arrival, `leave` on departure, and the keyboard lands on
 * the heading. Escape is the module's own: an open entry gives itself back, and
 * the page gives itself back to the board.
 */
function TimelinePageView({ shown }: { shown: boolean }) {
  const page = useRef<TimelinePage | null>(null)
  const was = useRef(false)

  useLayoutEffect(() => {
    if (!page.current) {
      page.current = bindTimeline(document, (project, item) => {
        if (!openBoard(project, item)) navigate("projects")
      })
    }
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    const arrive = () => {
      if (!was.current) return
      const project = requestedTimeline()
      // A timeline with no Project is not a page, it is a question nobody
      // asked. The Projects page is where the Project is chosen, so that is
      // where a reader who arrived here by address is sent.
      if (!project) {
        navigate("projects")
        return
      }
      void page.current?.enter(project)
      document.getElementById("timeline-title")?.focus({ preventScroll: true })
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

  useLayoutEffect(() => {
    if (!shown) return
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape" || ev.metaKey || ev.ctrlKey) return
      if (ActionConfirm.isOpen() || Info.isOpen() || overlayShown("keys")) return
      if (!(document.getElementById("sidebar")?.hidden ?? true)) return
      if (document.querySelector("dialog[open]")) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      page.current?.escape()
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [shown])

  return (
    <section
      className="page timeline-page"
      id="timeline"
      data-page-view="timeline"
      aria-label="Project Timeline"
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/** The page router, as the drawer moves: its row for that page, clicked. */
function navigate(name: string): void {
  const row = document.querySelector<HTMLButtonElement>(`#sidebar [data-page-to="${name}"]`)
  if (row && !row.disabled) row.click()
}

export const page: PageModule = { id: "timeline", Component: TimelinePageView }
