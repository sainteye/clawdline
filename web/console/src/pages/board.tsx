import { useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import { boardRegistration } from "./board-registration.js"
import { bindBoard, boardIntent, enterProjectBoard, refreshBoardMode, type BoardPage } from "../legacy/board-bridge.js"
import { openTimeline } from "../legacy/timeline-bridge.js"
import { ActionConfirm, Info, requestPage, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./board/section.html?raw"

/**
 * The Project Board page: `section#board` in the Swift app's `index.html`,
 * drawn by its `view/board.js`.
 *
 * The fragment beside this file is that section's markup between its own
 * tags, whitespace included, and the copied module fills it — the Project
 * cards, the overview, the groups, the cards and an item's detail are the
 * original's own DOM. React owns the section element and nothing inside it.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enterProjectBoard` on arrival (which is the Projects page when no Project
 * has been opened — the drawer's row for this page is hidden, as there),
 * `leave` on departure, the keyboard lands on the heading
 * (`focus: "board-title"`), a `#page=board&machine=…&project=…` link is opened
 * through `openLocator` (`bindBoardRoute`), and `BoardControls.refresh()` runs
 * once at start, as `boot` runs it.
 *
 * The Timeline tab is `main.js`'s, not the module's: it hands the Timeline
 * page the Project this one is showing and then changes page, exactly as there.
 * With no Project open it goes to the Projects page instead, as there.
 *
 * Escape: in the original, `input/keys.js` gives this page its turn after the
 * drawer and the keyboard card — `BoardControls.escape()`, which is the
 * module's `escape`: an item gives back its Project, a Project gives back the
 * Projects page. It is taken here, ahead of App's, when nothing App would close
 * first is open.
 */
function BoardPageView({ shown }: { shown: boolean }) {
  const page = useRef<BoardPage | null>(null)
  const was = useRef(false)
  // A Board link the address carried, waiting for the page to be shown.
  const wanted = useRef<ReturnType<typeof boardIntent>>(null)

  useLayoutEffect(() => {
    if (!page.current) page.current = bindBoard(document, { navigate, openLive })
    const timeline = document.getElementById("board-timeline-tab") as HTMLButtonElement | null
    const onTimeline = () => {
      const project = page.current?.state.projectId ?? null
      if (!project) {
        navigate("projects")
        return
      }
      openTimeline(project)
      navigate("timeline")
    }
    timeline?.addEventListener("click", onTimeline)
    // Read before App's own routing runs, which is after this.
    wanted.current = boardIntent(location.hash)
    // A link pasted while the page is already up opens at once, as the route
    // does there; otherwise it waits for the arrival App is about to make.
    const onHash = () => {
      const intent = boardIntent(location.hash)
      if (!intent) return
      if (was.current && page.current) void page.current.openLocator(intent.locator, intent.error)
      else wanted.current = intent
    }
    window.addEventListener("hashchange", onHash, true)
    // `boot`: the mode, once, whatever page is first.
    refreshBoardMode().catch(() => {})
    return () => {
      window.removeEventListener("hashchange", onHash, true)
      timeline?.removeEventListener("click", onTimeline)
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
      if (!was.current || !page.current) return
      const intent = wanted.current
      wanted.current = null
      if (intent) void page.current.openLocator(intent.locator, intent.error)
      void enterProjectBoard(page.current, navigate)
      document.getElementById("board-title")?.focus({ preventScroll: true })
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
      id="board"
      className="page board-page"
      data-page-view="board"
      hidden={!shown}
      aria-label="Project Board"
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
  )
}

/** `Pages.go(name)`: the page, with its address written. */
function navigate(name: string): void {
  requestPage({ page: name })
}

/**
 * `openSession` in `main.js`, for a Session the list already has: the sessions
 * page, and that row opened the way a press on it opens it.
 */
function openLive(id: string): void {
  requestPage({ page: "sessions" })
  for (const node of document.querySelectorAll<HTMLElement>("#rows > li.row")) {
    if (node.dataset.id === id) {
      node.click()
      return
    }
  }
}

export const page: PageModule = { ...boardRegistration, Component: BoardPageView }
