import { useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import { bindTimeline, requestedTimeline, timelineReturn, type TimelinePage } from "../legacy/timeline-bridge.js"
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
 * reached from something that already has a Project, which is what hands it
 * the Project through `openTimeline`.
 *
 * **Where it is reached from changed** (work-system-review §5.2, W6). The old
 * Project Board was a frozen snapshot presented as a live page. It has been
 * removed, and a work item now opens this page (`pages/work/Board.tsx`), which
 * is where "the history of this one thing" belongs.
 *
 * `boardItemIds` belong to the old store and have no proved mapping to work
 * ids. The copied renderer still emits their pills, so this host removes them
 * as they are drawn instead of turning an unknown relation into a link.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter(project)` on arrival, `leave` on departure, and the keyboard lands on
 * the heading. Escape is the module's own: an open entry gives itself back, and
 * the page gives itself back to the page that opened it.
 */
function TimelinePageView({ shown }: { shown: boolean }) {
  const page = useRef<TimelinePage | null>(null)
  const was = useRef(false)

  useLayoutEffect(() => {
    const root = document.getElementById("timeline")
    root?.querySelector("#timeline-board-tab")?.remove()
    const discardUnmappedPills = () => {
      for (const pill of root?.querySelectorAll(".timeline-board-pill, .timeline-board-more") ?? []) pill.remove()
    }
    discardUnmappedPills()
    const observer = root ? new MutationObserver(discardUnmappedPills) : null
    if (root && observer) observer.observe(root, { childList: true, subtree: true })
    if (!page.current) {
      // In the copied module this callback means "back to Board". With that
      // page retired it returns to the page that supplied the Project. Pill
      // calls never survive the observer above.
      page.current = bindTimeline(document, () => navigate(timelineReturn()))
    }
    return () => observer?.disconnect()
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
        navigate(timelineReturn())
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
