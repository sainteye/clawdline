import { ActionProgress } from "../ui/ActionProgress.js"
import { catalogFormat, catalogWord, catalogWordLanguage, currentCatalogTag } from "../catalog.js"
import { createPortal } from "react-dom"
import { showModalWithoutChoice } from "../modal-focus.js"
import { ProjectTools } from "./projects/ProjectTools.js"
import { ProjectFolderPicker } from "./projects/ProjectFolderPicker.js"
import type { ProjectSetupHandle } from "./projects/ProjectSetup.js"
import { localizePinnedProjectStatus, projectOpenLabel } from "./projects/project-list.js"
import { decoratePinnedWorktree } from "./projects/pinned-worktree-copy.js"
import { useLayoutEffect, useRef, useState } from "react"
import type { PageModule } from "./types.js"
import { bindProjects, latestProjectReadRefusal, latestWorktreeReadRefusal, removeWorktree, worktreeSourceRow, type ProjectsPage } from "../legacy/projects-bridge.js"
import { machineWording } from "../legacy/machine-copy.js"
import { ActionConfirm, Info, requestPage, shown as overlayShown } from "../overlays/index.js"
import sectionMarkup from "./projects/section.html?raw"
import { nextWord, type NextWord } from "../next-strings.js"
import { workPageHash } from "../page-route.js"
import { showProjectRefusalDetail } from "./projects/refusal-detail.js"
import { removeProjectPlace } from "./work/api.js"
import { failureSentence, strings } from "../legacy/bridge.js"
import { arriveAtDetail, detailInHash, enterDetail, leaveDetail } from "../page-detail.js"
import "../page-bar.css"
import "../page-skeleton.css"
import "./projects/project-skeleton.css"

/** An opened Project is `#page=projects&id=<place id>` (page-detail.ts). */
const PROJECTS_ADDRESS = {
  enter: (id: string) => enterDetail("projects", id),
  leave: () => leaveDetail("projects"),
}

type ProjectTarget = {
  id: string
  projectId?: string
  label?: string
  path?: string
  activeItemCount?: number
}
type BoundProjects = ProjectsPage & {
  state: ProjectsPage["state"] & { places?: ProjectTarget[] | null; place?: ProjectTarget | null }
  openLifecycle(project: ProjectTarget): Promise<void>
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
  const [toolsHost, setToolsHost] = useState<HTMLElement | null>(null)
  useLayoutEffect(() => { setToolsHost(document.getElementById("project-tools-host")) }, [])
  // The copied renderer says it is reading in a status line; the rows it is
  // about to draw are shown in its place, beside its list, without touching it.
  const [skeletonHost, setSkeletonHost] = useState<HTMLElement | null>(null)
  const [listReading, setListReading] = useState(false)
  useLayoutEffect(() => {
    const rows = document.getElementById("projects-rows")
    if (!rows) return
    const host = document.createElement("div")
    rows.after(host)
    setSkeletonHost(host)
    return () => { host.remove(); setSkeletonHost(null) }
  }, [])
  useLayoutEffect(() => {
    const view = document.getElementById("projects-list-view")
    if (listReading) view?.setAttribute("data-reading", "")
    else view?.removeAttribute("data-reading")
  }, [listReading])
  const page = useRef<BoundProjects | null>(null)
  const setup = useRef<ProjectSetupHandle>(null)
  const was = useRef(false)
  const painted = useRef(false)
  const [removeTarget, setRemoveTarget] = useState<ProjectTarget | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [removeBusy, setRemoveBusy] = useState(false)
  const [removeError, setRemoveError] = useState("")
  const removeDialog = useRef<HTMLDialogElement>(null)
  const [worktreeTarget, setWorktreeTarget] = useState<{ id: string; path: string; project: ProjectTarget; atRisk: boolean } | null>(null)
  // Ids being deleted. Closing the dialog stops the wait, not the deletion, so
  // a press outlives the dialog that made it.
  const [worktreeRemoving, setWorktreeRemoving] = useState<readonly string[]>([])
  const [worktreeError, setWorktreeError] = useState("")
  const worktreeDialog = useRef<HTMLDialogElement>(null)
  const worktreeShown = useRef<string | null>(null)
  worktreeShown.current = worktreeTarget?.id ?? null
  const worktreeBusy = !!worktreeTarget && worktreeRemoving.includes(worktreeTarget.id)

  useLayoutEffect(() => {
    const button = document.getElementById("projects-add")
    if (!button) return
    button.textContent = catalogWord("projects", "addButton")
    const open = () => setAddOpen(true)
    button.addEventListener("click", open)
    return () => button.removeEventListener("click", open)
  }, [])

  useLayoutEffect(() => {
    const dialog = removeDialog.current
    if (!dialog) return
    if (removeTarget) showModalWithoutChoice(dialog)
    if (!removeTarget && dialog.open) dialog.close()
  }, [removeTarget])

  async function confirmRemove() {
    if (!removeTarget?.path || removeBusy) return
    setRemoveBusy(true)
    setRemoveError("")
    try {
      await removeProjectPlace({ id: removeTarget.id, label: removeTarget.label || removeTarget.path,
        path: removeTarget.path })
      setRemoveTarget(null)
      void page.current?.enter()
    } catch (error) {
      setRemoveError(failureSentence(error, nextWord("projectsRemoveFailed")))
    } finally {
      setRemoveBusy(false)
    }
  }

  useLayoutEffect(() => {
    const dialog = worktreeDialog.current
    if (!dialog) return
    if (worktreeTarget) showModalWithoutChoice(dialog)
    if (!worktreeTarget && dialog.open) dialog.close()
  }, [worktreeTarget])

  async function confirmWorktreeRemove() {
    const target = worktreeTarget
    if (!target || worktreeBusy) return
    setWorktreeRemoving(ids => [...ids, target.id])
    setWorktreeError("")
    try {
      await removeWorktree(target.project.projectId || target.project.id, target.id)
      if (worktreeShown.current === target.id) setWorktreeTarget(null)
      await page.current?.openLifecycle(target.project)
    } catch (error) {
      // A person who closed the dialog stopped waiting; the list still
      // answers whether the checkout is gone.
      if (worktreeShown.current === target.id) {
        setWorktreeError(failureSentence(error, nextWord("worktreeRemoveFailed")))
      } else {
        void page.current?.openLifecycle(target.project)
      }
    } finally {
      setWorktreeRemoving(ids => ids.filter(id => id !== target.id))
    }
  }

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
    const bound = page.current ?? (bindProjects(document, navigate, PROJECTS_ADDRESS) as BoundProjects)
    page.current = bound
    const openWork = (project: ProjectTarget) => {
      const path = typeof project.path === "string" ? project.path.trim() : ""
      if (!path) return
      const address = workPageHash(path, "projects")
      try {
        history.replaceState(history.state, "", address)
      } catch {
        location.hash = address
      }
      requestPage({ page: "work", hash: false })
    }
    // The copied renderer owns its click listener; capture the row so its
    // primary action opens the same settings dialog as the gear.
    const rows = document.getElementById("projects-rows")
    const onProject = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row[data-place-id]")
      if (!button || !rows?.contains(button)) return
      const project = bound.state.places?.find((place) => place.id === button.dataset.placeId)
      if (!project?.path) return
      ev.preventDefault()
      ev.stopImmediatePropagation()
      setup.current?.open(project.path, button)
    }
    const onWork = () => {
      const project = bound.state.place
      if (project) openWork(project)
    }
    const work = document.getElementById("project-open-work")
    rows?.addEventListener("click", onProject, true)
    work?.addEventListener("click", onWork)
    return () => {
      rows?.removeEventListener("click", onProject, true)
      work?.removeEventListener("click", onWork)
    }
  }, [])

  useLayoutEffect(() => {
    const lifecycle = document.getElementById("project-worktree-lifecycle")
    if (!lifecycle) return
    const decorate = () => {
      decoratePinnedWorktree(lifecycle, worktreeSourceRow)
      showProjectRefusalDetail(lifecycle.querySelector<HTMLElement>("#project-worktree-status"), latestWorktreeReadRefusal())
      for (const card of lifecycle.querySelectorAll<HTMLElement>(".worktree-card[data-worktree-id]")) {
        const existing = card.querySelector(".worktree-remove")
        const id = card.dataset.worktreeId || ""
        const row = worktreeSourceRow(id)
        if (!row?.path || row.cleanup?.blockers?.some((blocker) => blocker.code === "main_worktree")) {
          existing?.remove()
          continue
        }
        if (existing) continue
        const button = document.createElement("button")
        button.type = "button"
        button.className = "worktree-remove"
        button.dataset.worktreeId = id
        button.textContent = nextWord("worktreeRemoveButton")
        card.appendChild(button)
      }
    }
    const onRemove = (event: Event) => {
      const button = (event.target as Element | null)?.closest<HTMLButtonElement>("button.worktree-remove[data-worktree-id]")
      if (!button || !lifecycle.contains(button)) return
      const project = page.current?.state.place
      const row = worktreeSourceRow(button.dataset.worktreeId || "")
      if (!project || !row?.worktreeId || !row.path) return
      const clean = row.status?.complete === true && row.status.staged === 0
        && row.status.modified === 0 && row.status.untracked === 0
      // The person decides; the dialog only says what deleting would lose.
      const atRisk = !(clean && row.classifications?.includes("landed_identical_residue") === true)
      setWorktreeError("")
      setWorktreeTarget({ id: row.worktreeId, path: row.path, project, atRisk })
    }
    const observer = new MutationObserver(decorate)
    observer.observe(lifecycle, { childList: true, subtree: true })
    lifecycle.addEventListener("click", onRemove)
    decorate()
    return () => { observer.disconnect(); lifecycle.removeEventListener("click", onRemove) }
  }, [])

  useLayoutEffect(() => {
    const rows = document.getElementById("projects-rows")
    const status = document.getElementById("projects-status")
    if (!rows || !status) return
    const decorate = () => {
      for (const button of rows.querySelectorAll<HTMLButtonElement>("button.project-row[data-place-id]")) {
        const place = page.current?.state.places?.find((candidate) => candidate.id === button.dataset.placeId)
        if (!place) continue
        if (!Number.isSafeInteger(place.activeItemCount) || (place.activeItemCount ?? -1) < 0) {
          button.querySelector(".project-row-activity")?.remove()
        }
        const name = place.label || place.path || ""
        const label = projectOpenLabel(name, null, false)
        if (button.getAttribute("aria-label") !== label) button.setAttribute("aria-label", label)
        if (catalogWordLanguage("legacy", "webProjectOpenLabel") === "en" && currentCatalogTag() !== "en") button.lang = "en"
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
        const name = wrapper.querySelector(".project-row-name")?.textContent || catalogWord("literal", "fb67023ade0c")
        const remove = document.createElement("button")
        remove.type = "button"
        remove.className = "project-row-remove"
        remove.dataset.placeId = project.dataset.placeId
        remove.title = nextWord("projectsRemove")
        remove.setAttribute("aria-label", nextWord("projectsRemoveFor", { project: name }))
        remove.setAttribute("aria-haspopup", "dialog")
        remove.innerHTML = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M4 7h16M9 7V4h6v3m3 0-1 13H7L6 7m4 4v5m4-5v5"/></svg>`
        wrapper.classList.add("has-remove")
        wrapper.appendChild(remove)
      }
      const localizedStatus = localizePinnedProjectStatus(status.textContent ?? "")
      if (localizedStatus !== status.textContent) status.textContent = localizedStatus
      setListReading(!rows.childElementCount && status.textContent === strings.webProjectsLoading)
      showProjectRefusalDetail(status, latestProjectReadRefusal())
    }
    const onSettings = (ev: Event) => {
      const gear = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-settings[data-place-id]")
      if (!gear || !rows.contains(gear)) return
      const project = page.current?.state.places?.find(place => place.id === gear.dataset.placeId)
      if (project?.path) setup.current?.open(project.path, gear)
    }
    const onRemove = (ev: Event) => {
      const button = (ev.target as Element | null)?.closest<HTMLButtonElement>("button.project-row-remove[data-place-id]")
      if (!button || !rows.contains(button)) return
      const project = page.current?.state.places?.find(place => place.id === button.dataset.placeId)
      if (project?.path) { setRemoveError(""); setRemoveTarget(project) }
    }
    rows.addEventListener("click", onSettings)
    rows.addEventListener("click", onRemove)
    const observer = new MutationObserver(decorate)
    observer.observe(rows, { childList: true, subtree: true })
    observer.observe(status, { childList: true })
    decorate()
    return () => {
      observer.disconnect()
      rows.removeEventListener("click", onSettings)
      rows.removeEventListener("click", onRemove)
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
      const add = document.getElementById("projects-add")
      if (add) add.textContent = catalogWord("projects", "addButton")
      // An address that names a Project (a reload, a link) opens it once the
      // list is read, with the list put under it as one Back step.
      const wanted = detailInHash("projects", location.hash)
      if (wanted) arriveAtDetail("projects", wanted)
      void page.current?.enter(wanted)
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

  // Back, Forward and a typed address move the open Project the way the
  // page's own back control does.
  useLayoutEffect(() => {
    if (!shown) return
    const follow = () => {
      if (!/(?:^|[#&])page=projects(?:&|$)/.test(location.hash)) return
      void page.current?.follow(detailInHash("projects", location.hash))
    }
    window.addEventListener("popstate", follow)
    window.addEventListener("hashchange", follow)
    return () => {
      window.removeEventListener("popstate", follow)
      window.removeEventListener("hashchange", follow)
    }
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
    {skeletonHost && listReading && createPortal(<ProjectsSkeleton label={strings.webProjectsLoading} />, skeletonHost)}
    {toolsHost && createPortal(<>
      <ProjectTools shown={shown} changed={() => { void page.current?.enter() }} setupRef={setup} />
    </>, toolsHost)}
    <ProjectFolderPicker open={addOpen} onClose={() => setAddOpen(false)} onAdded={() => { void page.current?.enter() }} />
    <dialog className="project-remove-dialog" ref={removeDialog}
      onCancel={(event) => { if (removeBusy) event.preventDefault() }}
      onClose={() => { if (!removeBusy) setRemoveTarget(null) }}>
      <h2>{nextWord("projectsRemoveTitle")}</h2>
      <p>{nextWord("projectsRemoveDescription", { project: removeTarget?.label || removeTarget?.path || "" })}</p>
      {removeError && <p className="project-remove-error" role="alert">{removeError}</p>}
      <div className="project-remove-actions">
        <button type="button" disabled={removeBusy} onClick={() => setRemoveTarget(null)}>{nextWord("projectsRemoveCancel")}</button>
        <button type="button" disabled={removeBusy} aria-busy={removeBusy} onClick={() => void confirmRemove()}><ActionProgress busy={removeBusy}>{removeBusy ? nextWord("projectsRemoveBusy") : nextWord("projectsRemoveConfirm")}</ActionProgress></button>
      </div>
    </dialog>
    <dialog className="project-remove-dialog" ref={worktreeDialog}
      onClose={() => setWorktreeTarget(null)}>
      <h2>{nextWord("worktreeRemoveTitle")}</h2>
      <p>{nextWord("worktreeRemoveDescription", { path: worktreeTarget?.path || "" })}</p>
      {worktreeTarget?.atRisk && <p className="project-remove-error" role="status">{nextWord("worktreeRemoveRisk")}</p>}
      {worktreeBusy && <p className="worktree-remove-waiting" role="status">{nextWord("worktreeRemoveWaiting")}</p>}
      {worktreeError && <p className="project-remove-error" role="alert">{worktreeError}</p>}
      <div className="project-remove-actions">
        <button type="button" onClick={() => setWorktreeTarget(null)}>{worktreeBusy ? nextWord("worktreeRemoveClose") : nextWord("projectsRemoveCancel")}</button>
        <button type="button" disabled={worktreeBusy} aria-busy={worktreeBusy} onClick={() => void confirmWorktreeRemove()}>
          {worktreeBusy && <span className="worktree-remove-spinner" aria-hidden="true" />}
          {worktreeBusy ? nextWord("worktreeRemoveBusy") : nextWord("worktreeRemoveConfirm")}
        </button>
      </div>
    </dialog>
    </>
  )
}

/** The first read: Project rows in the list's own geometry. */
function ProjectsSkeleton({ label }: { label: string }) {
  return <div className="page-skeleton" role="status" aria-label={label}>
    <ol className="projects-rows" aria-hidden="true">{[0, 1, 2, 3, 4].map((row) => <li className="project-row-wrap" key={row}>
      <div className="project-row project-skeleton-row"><span className="page-skeleton-line project-skeleton-mark" />
        <span className="project-row-text"><span className="page-skeleton-line project-skeleton-name" /><span className="page-skeleton-line project-skeleton-path" /></span></div>
    </li>)}</ol>
  </div>
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
