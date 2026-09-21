import { useLayoutEffect, useRef } from "react"
import type { PageModule } from "./types.js"
import { bindProjects, type ProjectsPage } from "../legacy/projects-bridge.js"
import {
  registerRetiredBoardProjectFallback,
  type RetiredBoardProject,
} from "../legacy/board-bridge.js"
import { ActionConfirm, Info, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./projects/section.html?raw"
import { nextWord, type NextWord } from "../next-strings.js"

/**
 * The Projects page: `section#projects` in the Swift app's `index.html`,
 * drawn by its `view/projects.js` and `view/worktrees.js`.
 *
 * The fragment beside this file is that section's markup between its own
 * tags, whitespace included, and the copied modules fill it — the rows, the
 * Project's identity, the worktree lifecycle cards, the delivered block and
 * its groups are the original's own DOM. React owns the section element and
 * nothing inside it, and never re-renders inside it.
 *
 * What the original's `main.js` does for this page is done here: bind once,
 * `enter` on arrival (the list, every time), `leave` on departure, the
 * keyboard lands on the heading (`focus: "projects-title"`), and the app's
 * static words are painted for the chosen language.
 *
 * Escape: in the original, `input/keys.js` gives this page its turn after the
 * drawer and the keyboard card, and a Project open inside the page is given
 * back before the page is left. App's key handler leaves the page directly, so
 * the page's turn is taken here, ahead of it, and only when nothing App would
 * close first is open.
 */
function ProjectsPageView({ shown }: { shown: boolean }) {
  const page = useRef<(ProjectsPage & { openProject(project: RetiredBoardProject): Promise<void> }) | null>(null)
  const was = useRef(false)
  const painted = useRef(false)

  useLayoutEffect(() => {
    // The copied Projects view still calls its old `openBoard(place)` seam for
    // rows joined to the frozen catalog. Keep the copy byte-for-byte, but open
    // that same repository in Projects' own detail after discarding the old
    // association. Nothing on this path reads an old card.
    const bound =
      page.current ??
      (bindProjects(document, navigate) as ProjectsPage & {
        openProject(project: RetiredBoardProject): Promise<void>
      })
    page.current = bound
    return registerRetiredBoardProjectFallback((old) => {
      const { boardProjectId: _retired, ...project } = old
      void bound.openProject(project)
    })
  }, [])

  useLayoutEffect(() => {
    if (shown === was.current) return
    was.current = shown
    if (!shown) {
      page.current?.leave()
      return
    }
    // The language arrives in App's effect, which runs after this one on a cold
    // start at `#page=projects`; the page is still covered (`booting`) until
    // then, so arrival waits for them as the original's does.
    const arrive = () => {
      if (!was.current) return
      // Static copy is painted once; the Board answer may rewrite the lede after.
      if (!painted.current) {
        painted.current = true
        paintNextWords(document.getElementById("projects"))
      }
      void page.current?.enter()
      document.getElementById("projects-title")?.focus({ preventScroll: true })
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
      if (page.current?.state.view !== "detail") return
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
      className="page projects"
      id="projects"
      data-page-view="projects"
      aria-labelledby="projects-title"
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

export const page: PageModule = { id: "projects", Component: ProjectsPageView }

function paintNextWords(root: ParentNode | null): void {
  if (!root) return
  for (const node of root.querySelectorAll<HTMLElement>("[data-next-word]")) {
    const key = node.dataset.nextWord as NextWord | undefined
    if (!key) continue
    const value = nextWord(key)
    if (node.textContent !== value) node.textContent = value
  }
}
