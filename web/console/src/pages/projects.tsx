import { catalogFormat, catalogWord, catalogWordLanguage, currentCatalogTag } from "../catalog.js"
import { createPortal } from "react-dom"
import { ProjectTools } from "./projects/ProjectTools.js"
import type { ProjectSetupHandle } from "./projects/ProjectSetup.js"
import { activityUnknownScope, localizePinnedProjectStatus, projectActivityUsesEnglishFallback, projectActivityWords, projectListWords, projectOpenLabel, replaceSuffix } from "./projects/project-list.js"
import { decoratePinnedWorktree } from "./projects/pinned-worktree-copy.js"
import { useLayoutEffect, useRef, useState } from "react"
import type { PageModule } from "./types.js"
import { bindProjects, latestProjectReadRefusal, latestWorktreeReadRefusal, worktreeSourceRow, type ProjectsPage } from "../legacy/projects-bridge.js"
import { machineWording } from "../legacy/machine-copy.js"
import {
  registerRetiredBoardProjectFallback,
  type RetiredBoardProject,
} from "../legacy/board-bridge.js"
import { ActionConfirm, Info, requestPage, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./projects/section.html?raw"
import { nextWord, type NextWord } from "../next-strings.js"
import { workPageHash } from "../page-route.js"
import { openTerminalPage } from "./terminal/navigate.js"
import { showProjectRefusalDetail } from "./projects/refusal-detail.js"

type ProjectTarget = RetiredBoardProject & {
  id?: string
  label?: string
  path?: string
  activeItemCount?: number
  summaryCoverage?: unknown
  activitySourcePartial?: boolean
  activityReadStatus?: string
}
type BoundProjects = ProjectsPage & {
  state: ProjectsPage["state"] & { places?: ProjectTarget[] | null }
  openProject(project: RetiredBoardProject): Promise<void>
}

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
  const [iconHost, setIconHost] = useState<HTMLElement | null>(null)
  useLayoutEffect(() => { setIconHost(document.getElementById("project-icon-copy-host")) }, [])
  const page = useRef<BoundProjects | null>(null)
  const setup = useRef<ProjectSetupHandle>(null)
  const was = useRef(false)
  const painted = useRef(false)

  useLayoutEffect(() => {
    const root = document.getElementById("project-worktree-lifecycle")
    if (!root) return
    const adapt = () => {
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const value = node.textContent ?? ""
        if (!/The Mac reports|until the Mac|Refreshing the Mac observation|Mac 回報|Mac 取得|重新觀測 Mac/.test(value)) continue
        const wording = machineWording(value, document.documentElement.lang)
        if (wording !== value) node.textContent = wording
      }
    }
    adapt()
    const observer = new MutationObserver(adapt)
    observer.observe(root, { childList: true, characterData: true, subtree: true })
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    // The copied Projects view still calls its old `openBoard(place)` seam for
    // rows joined to the frozen catalog. Keep the copy byte-for-byte, but send
    // that repository to its scoped work page. Nothing here reads an old card.
    const bound = page.current ?? (bindProjects(document, navigate) as BoundProjects)
    page.current = bound
    const openWork = (project: ProjectTarget) => {
      const path = typeof project.path === "string" ? project.path.trim() : ""
      if (!path) {
        const { boardProjectId: _retired, ...ordinary } = project
        void bound.openProject(ordinary)
        return
      }
      const address = workPageHash(path, "projects")
      try {
        history.replaceState(history.state, "", address)
      } catch {
        location.hash = address
      }
      requestPage({ page: "work", hash: false })
    }
    const unregister = registerRetiredBoardProjectFallback(openWork)
    // The copied renderer owns its click listener and cannot be changed. Its
    // rows do expose their place id, and the bound page retains the exact
    // place answer, so capture the ordinary (non-retired-Board) rows here and
    // give every Project the same route into its work.
    const rows = document.getElementById("projects-rows")
    const onProject = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row[data-place-id]")
      if (!button || !rows?.contains(button)) return
      const project = bound.state.places?.find((place) => place.id === button.dataset.placeId)
      if (!project?.path) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      openWork(project)
    }
    rows?.addEventListener("click", onProject, true)
    return () => {
      rows?.removeEventListener("click", onProject, true)
      unregister()
    }
  }, [])

  useLayoutEffect(() => {
    const lifecycle = document.getElementById("project-worktree-lifecycle")
    if (!lifecycle) return
    const decorate = () => {
      decoratePinnedWorktree(lifecycle, worktreeSourceRow)
      showProjectRefusalDetail(lifecycle.querySelector<HTMLElement>("#project-worktree-status"), latestWorktreeReadRefusal())
    }
    const observer = new MutationObserver(decorate)
    observer.observe(lifecycle, { childList: true, subtree: true })
    decorate()
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    const rows = document.getElementById("projects-rows")
    const help = document.getElementById("projects-activity-help")
    const status = document.getElementById("projects-status")
    if (!rows || !help || !status) return
    const decorate = () => {
      const words = projectListWords(document.documentElement.lang || navigator.language || "")
      const unknownActivities: HTMLElement[] = []
      for (const button of rows.querySelectorAll<HTMLButtonElement>("button.project-row[data-place-id]")) {
        const place = page.current?.state.places?.find((candidate) => candidate.id === button.dataset.placeId)
        if (!place) continue
        const activity = projectActivityWords(place)
        const badge = button.querySelector<HTMLElement>(".project-row-activity")
        if (badge && activity) {
          if (badge.textContent !== activity.text) badge.textContent = activity.text
          badge.className = `project-row-activity is-${activity.tone}`
          if (projectActivityUsesEnglishFallback(place)) badge.lang = "en"
          if (activity.tone === "unknown") unknownActivities.push(badge)
        }
        const name = place.label || place.path || ""
        const label = projectOpenLabel(name, activity?.text ?? null, false)
        if (button.getAttribute("aria-label") !== label) button.setAttribute("aria-label", label)
        if (projectActivityUsesEnglishFallback(place) || (catalogWordLanguage("legacy", "webProjectOpenLabel") === "en" && currentCatalogTag() !== "en")) button.lang = "en"
      }
      const rowCount = rows.querySelectorAll("button.project-row[data-place-id]").length
      const scope = activityUnknownScope(rowCount, unknownActivities.length)
      for (const activity of unknownActivities) {
        activity.hidden = scope === "list"
        const button = activity.closest<HTMLButtonElement>("button.project-row")
        if (!button) continue
        if (scope === "list") {
          const current = button.getAttribute("aria-label") ?? ""
          const next = projectOpenLabel(button.querySelector(".project-row-name")?.textContent ?? "", activity.textContent, true)
          if (next !== current) button.setAttribute("aria-label", next)
        }
        button.setAttribute("aria-describedby", help.id)
      }
      for (const project of rows.querySelectorAll<HTMLButtonElement>("button.project-row[data-place-id]")) {
        const lines = project.querySelectorAll<HTMLElement>(".project-row-path")
        if (lines.length < 2) continue // The first line is the user's actual path.
        const line = lines[lines.length - 1]
        const value = line.textContent ?? ""
        const next = replaceSuffix(value, words.boardBefore, words.boardAfter)
        if (next !== value) line.textContent = next
        if (catalogWordLanguage("literal", "28ce0b145abb") === "en" && currentCatalogTag() !== "en") line.lang = "en"
      }
      for (const project of rows.querySelectorAll<HTMLButtonElement>(".project-row[data-place-id]")) {
        const wrapper = project.closest<HTMLElement>(".project-row-wrap")
        const worktrees = wrapper?.querySelector<HTMLButtonElement>(".project-row-worktrees")
        if (worktrees) {
          const word = catalogWord("next", "projectsWorktrees")
          const name = wrapper?.querySelector(".project-row-name")?.textContent || ""
          worktrees.title = word
          worktrees.setAttribute("aria-label", `${word}: ${name}`)
          if (catalogWordLanguage("next", "projectsWorktrees") === "en" && currentCatalogTag() !== "en") worktrees.lang = "en"
        }
        if (!wrapper || !project || wrapper.querySelector(".project-row-settings")) continue
        const gear = document.createElement("button")
        gear.type = "button"
        gear.className = "project-row-settings"
        gear.dataset.placeId = project.dataset.placeId
        gear.title = catalogWord("literal", "00186c9a0293")
        gear.setAttribute("aria-label", catalogFormat("template", "78b00d86df37", [wrapper.querySelector(".project-row-name")?.textContent || catalogWord("literal", "fb67023ade0c")]))
        gear.setAttribute("aria-haspopup", "dialog")
        gear.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <path d="M10.09 4.54h3.82l.54 1.66 1.35.77 1.71-.35 1.9 3.3-1.16 1.3v1.56l1.16 1.3-1.9 3.3-1.71-.35-1.35.77-.54 1.66h-3.82l-.54-1.66-1.35-.77-1.71.35-1.9-3.3 1.16-1.3v-1.56l-1.16-1.3 1.9-3.3 1.71.35 1.35-.77Z"/>
          <circle cx="12" cy="12" r="2.5"/>
        </svg>`
        wrapper.classList.add("has-settings")
        wrapper.appendChild(gear)
        // 終端: this project's terminals, locally or through the selected Cloud machine.
        const name = wrapper.querySelector(".project-row-name")?.textContent || nextWord("terminalEntry")
        const terminal = document.createElement("button")
        terminal.type = "button"
        terminal.className = "project-row-terminal"
        terminal.dataset.placeId = project.dataset.placeId
        terminal.title = nextWord("terminalEntry")
        terminal.setAttribute("aria-label", nextWord("terminalEntryFor", { project: name }))
        terminal.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="m7.5 10 3 2.5-3 2.5M12.5 15h4"/>
        </svg>`
        wrapper.classList.add("has-terminal")
        wrapper.appendChild(terminal)
      }
      help.hidden = scope === "none"
      const localizedStatus = localizePinnedProjectStatus(status.textContent ?? "")
      if (localizedStatus !== status.textContent) status.textContent = localizedStatus
      showProjectRefusalDetail(status, latestProjectReadRefusal())
    }
    const onSettings = (ev: Event) => {
      const gear = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-settings[data-place-id]")
      if (!gear || !rows.contains(gear)) return
      const project = page.current?.state.places?.find(place => place.id === gear.dataset.placeId)
      if (project?.path) setup.current?.open(project.path, gear)
    }
    const onTerminal = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-terminal[data-place-id]")
      if (!button || !rows.contains(button)) return
      // Every project's terminals are in the Session page's terminal list,
      // each row named by its project; there is no per-project page any more.
      const project = page.current?.state.places?.find((place) => place.id === button.dataset.placeId)
      if (project?.path) openTerminalPage(project.path)
    }
    rows.addEventListener("click", onSettings)
    rows.addEventListener("click", onTerminal)
    const observer = new MutationObserver(decorate)
    observer.observe(rows, { childList: true, subtree: true })
    observer.observe(status, { childList: true })
    decorate()
    return () => {
      observer.disconnect()
      rows.removeEventListener("click", onSettings)
      rows.removeEventListener("click", onTerminal)
    }
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
    <>
    <section
      className="page projects"
      id="projects"
      data-page-view="projects"
      aria-labelledby="projects-title"
      hidden={!shown}
      dangerouslySetInnerHTML={{ __html: sectionMarkup }}
    />
    {iconHost && createPortal(<>
      <ProjectTools shown={shown} changed={() => { void page.current?.enter() }} setupRef={setup} />
    </>, iconHost)}
    </>
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
