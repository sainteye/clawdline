import { localizedLiteralMap } from "../../catalog.js"
import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { useCallback, useEffect, useImperativeHandle, useRef, useState, type Ref } from "react"
import { createPortal } from "react-dom"
import type { Icon } from "@clawdline/contract"
import { failureSentence } from "../../legacy/bridge.js"
import { Command } from "../../session/Command.js"
import { Mark } from "../../session/List.js"
import { readProjectPlaces, type ProjectPlace } from "../work/api.js"
import { BEFORE_PAGE_CHANGE } from "../../overlays/index.js"
import { ProjectFiles } from "./ProjectFiles.js"
import { ProjectUnify } from "./ProjectUnify.js"
import { ProjectExplorer } from "./ProjectExplorer.js"
import {
  projectSetupCapabilities,
  projectSetupInstructions,
  projectSetupProgress,
  type SetupTone,
} from "./project-setup.js"
import "./icon-copy.css"

const toneWords: Record<SetupTone, string> = localizedLiteralMap({
  ready: "83799bb51f05",
  missing: "54c58892d432",
  attention: "f81fb7eed946",
  "not-applicable": "fa463019faec",
  unknown: "944f88e4f77b",
})

function ProjectReadiness({ place, select }: { place: ProjectPlace; select(place: ProjectPlace): void }) {
  const capabilities = projectSetupCapabilities(place)
  const progress = projectSetupProgress(place)
  const needsAttention = capabilities.some(row => row.tone === "attention")
  const incomplete = capabilities.some(row => row.applicable && !row.complete)
  const action = needsAttention ? catalogWord("literal", "0a98c973ad81") : incomplete ? catalogWord("literal", "7ef8b05c48f0") : catalogWord("literal", "1b6d43720612")
  return <li className="project-readiness-card">
    <div className="project-readiness-identity">
      <span className="project-readiness-mark" aria-hidden="true">
        {place.icon ? <Mark icon={place.icon as Icon} cellPx={3} /> : null}
      </span>
      <span>
        <strong>{place.label}</strong>
        <span className="project-readiness-path">{place.path}</span>
      </span>
      <span className="project-readiness-score" aria-label={progress ? catalogFormat("template", "e50de8226de9", [progress.total, progress.complete]) : catalogWord("literal", "738cc5207e7d")}>
        {progress ? `${progress.complete}/${progress.total}` : "—"}
      </span>
    </div>
    <ul className="project-readiness-capabilities">
      {capabilities.map(row => <li key={row.key} className={`project-readiness-capability is-${row.tone}`}>
        <span className="project-readiness-dot" aria-hidden="true" />
        <span>
          <strong>{row.label}</strong>
          <span>{row.detail}</span>
        </span>
        <span className="project-readiness-state">{toneWords[row.tone]}</span>
      </li>)}
    </ul>
    <button className="project-readiness-action" type="button" onClick={() => select(place)}>{action}</button>
  </li>
}

/**
 * The Projects page keeps only a small readiness summary in its main flow. The
 * complete, independently scrolling audit opens on demand so a long list never
 * pushes the ordinary Project rows below a second copy of every Project. A
 * press from that audit enters the ordinary command draft, where the person
 * still reviews assistant, model and instructions before any Session exists.
 */
export type ProjectSetupHandle = { open(path: string, trigger: HTMLButtonElement): void }

export function ProjectSetup({ shown, ref }: { shown: boolean; ref?: Ref<ProjectSetupHandle> }) {
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const dialog = useRef<HTMLDialogElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const restoreTrigger = useRef(true)
  const pendingSelection = useRef<ProjectPlace | null>(null)
  const scopedTrigger = useRef<HTMLButtonElement | null>(null)
  const [projectPath, setProjectPath] = useState<string | null>(null)
  const [view, setView] = useState<"settings" | "files">("settings")
  const filesState = useRef({ dirty: false, busy: false })
  const [closeNotice, setCloseNotice] = useState("")
  const updateFilesState = useCallback((state: { dirty: boolean; busy: boolean }) => { filesState.current = state }, [])
  const mayLeave = useCallback(() => {
    if (!dialog.current?.open) return true
    if (filesState.current.busy) { setCloseNotice(catalogWord("literal", "167e00adca12")); return false }
    if (filesState.current.dirty && !window.confirm(catalogWord("literal", "1167bc91fa0c"))) {
      dialog.current?.querySelector<HTMLTextAreaElement>(".project-files-editor textarea")?.focus()
      return false
    }
    setCloseNotice("")
    return true
  }, [])
  const requestClose = useCallback(() => { if (mayLeave()) dialog.current?.close() }, [mayLeave])

  useEffect(() => {
    const beforePageChange = (event: Event) => { if (dialog.current?.open && !mayLeave()) event.preventDefault() }
    document.addEventListener(BEFORE_PAGE_CHANGE, beforePageChange)
    return () => document.removeEventListener(BEFORE_PAGE_CHANGE, beforePageChange)
  }, [mayLeave])

  const load = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const answer = await readProjectPlaces()
      setPlaces(answer.places)
    } catch (reason) {
      setPlaces([])
      setError(failureSentence(reason, catalogWord("literal", "df0d05d31c79")))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!shown) {
      restoreTrigger.current = false
      requestClose()
      return
    }
    restoreTrigger.current = true
    void load()
  }, [shown, load, requestClose])

  useImperativeHandle(ref, () => ({
    open(path, button) {
      scopedTrigger.current = button
      setCloseNotice("")
      setProjectPath(path)
      setView("settings")
      void load()
      dialog.current?.showModal()
      dialog.current?.focus({ preventScroll: true })
    },
  }), [load])

  const complete = places.filter(place => {
    const progress = projectSetupProgress(place)
    return progress && progress.complete === progress.total
  }).length
  const visiblePlaces = projectPath ? places.filter(place => place.path === projectPath) : places

  const select = (place: ProjectPlace) => {
    if (!mayLeave()) return
    pendingSelection.current = place
    restoreTrigger.current = false
    dialog.current?.close()
  }

  const summary = loading
    ? catalogWord("literal", "77fe4c2aa7e2")
    : error
      ? catalogWord("literal", "a95e9e07a847")
      : places.length > 0
        ? catalogFormat("template", "54ab1dddbd2b", [complete, places.length])
        : catalogWord("literal", "1cfce0908f3d")

  return <><section className="project-setup" hidden={!shown} aria-labelledby="project-setup-launcher-title">
    <div className="project-setup-launcher-copy">
      <p className="project-setup-eyebrow">{catalogWord("inline", "c6325b98fd0c")}</p>
      <h2 id="project-setup-launcher-title">{catalogWord("inline", "7a181eee912c")}</h2>
      <p className={error ? "is-error" : ""} role={loading ? "status" : undefined}>{summary}</p>
    </div>
    <button
      className="project-setup-open"
      type="button"
      ref={trigger}
      aria-haspopup="dialog"
      onClick={() => {
        dialog.current?.showModal()
        dialog.current?.focus({ preventScroll: true })
      }}
    >{catalogWord("inline", "4ad26221870f")}</button>
  </section>
    {createPortal(<dialog
      className="project-setup-dialog"
      data-view={view}
      ref={dialog}
      tabIndex={-1}
      aria-labelledby="project-setup-title"
      onCancel={event => { event.preventDefault(); requestClose() }}
      onClose={() => {
        const place = pendingSelection.current
        pendingSelection.current = null
        filesState.current = { dirty: false, busy: false }
        setCloseNotice("")
        setProjectPath(null)
        if (place) {
          restoreTrigger.current = true
          scopedTrigger.current = null
          void Command.reviewProjectSetup(place.id, projectSetupInstructions(place))
          return
        }
        const opener = scopedTrigger.current
        const liveOpener = opener?.isConnected ? opener : Array.from(document.querySelectorAll<HTMLButtonElement>(".project-row-settings"))
          .find(button => button.dataset.placeId === opener?.dataset.placeId)
        if (restoreTrigger.current) (liveOpener || trigger.current)?.focus({ preventScroll: true })
        scopedTrigger.current = null
        restoreTrigger.current = true
      }}
    >
      <div className="project-setup-dialog-heading">
        <div>
          <p className="project-setup-eyebrow">{catalogWord("inline", "c6325b98fd0c")}</p>
          <h2 id="project-setup-title">{projectPath ? `${visiblePlaces[0]?.label || catalogWord("literal", "faec0867e1cc")} · ${view === "files" ? catalogWord("literal", "f207dc293b4f") : catalogWord("literal", "91c1ae775198")}` : catalogWord("literal", "6a6633400d3e")}</h2>
        </div>
        <button className="project-setup-close" type="button" aria-label={catalogWord("inline", "dfc889eaa026")} onClick={requestClose}>{catalogWord("inline", "c7fdddf79eaa")}</button>
      </div>
      <div className="project-setup-dialog-body">
        {closeNotice && <p role="status">{closeNotice}</p>}
        {projectPath && <div className="project-setup-views" aria-label={catalogWord("inline", "a2d62aa7e4df")}>
          <button type="button" aria-pressed={view === "settings"} onClick={() => setView("settings")}>{catalogWord("inline", "c57e45ecd648")}</button>
          <button type="button" aria-pressed={view === "files"} onClick={() => {
            if (!mayLeave()) return
            filesState.current = { dirty: false, busy: false }
            setView("files")
          }}>{catalogWord("inline", "a11ac5efe91d")}</button>
        </div>}
        {view === "files" && projectPath && <>
          {loading && <p role="status">{catalogWord("inline", "a33893db1b83")}</p>}
          {!loading && error && <p role="alert">{error} <button type="button" onClick={() => void load()}>{catalogWord("inline", "7e59d0f16293")}</button></p>}
          {!loading && !error && !visiblePlaces[0] && <p role="status">{catalogWord("inline", "0b2d6de09d19")}</p>}
          {!loading && !error && visiblePlaces[0] && <ProjectExplorer key={visiblePlaces[0].id} place={visiblePlaces[0]} />}
        </>}
        {view === "settings" && <>
        <div className="project-setup-heading">
          <p>{catalogWord("inline", "9efc27432575")}</p>
          {!projectPath && !loading && places.length > 0 && <span className="project-setup-total">{complete}/{places.length}<small>{catalogWord("inline", "1f18df830a84")}</small></span>}
        </div>
        {projectPath && visiblePlaces[0] && <ProjectUnify key={visiblePlaces[0].id} place={visiblePlaces[0]} />}
        {projectPath && visiblePlaces[0] && <ProjectFiles key={visiblePlaces[0].id} place={visiblePlaces[0]} onState={updateFilesState} />}
        {loading && <p className="project-setup-loading" role="status">{catalogWord("inline", "db8653492c3a")}</p>}
        {!loading && !error && visiblePlaces.length > 0 && <ol className="project-readiness-list">
          {visiblePlaces.map(place => <ProjectReadiness key={place.id} place={place} select={select} />)}
        </ol>}
        {!loading && !error && visiblePlaces.length === 0 && <p className="project-setup-empty" role="status">{projectPath ? catalogWord("literal", "0e8fb42d973e") : <>{catalogWord("inline", "76d12ea59749")} <code>{catalogWord("inline", "fa482be8fef0")}</code>{catalogWord("inline", "7aa0955db160")}</>}</p>}
        {error && <p role="alert">{error}</p>}
        <button className="project-setup-refresh" type="button" disabled={loading} onClick={() => void load()}>{catalogWord("inline", "358a13c304aa")}</button>
        </>}
      </div>
    </dialog>, document.body)}
  </>
}
