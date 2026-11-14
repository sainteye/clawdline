import { useEffect, useRef, useState } from "react"
import { ActionProgress } from "../../ui/ActionProgress.js"
import { RefusalError, asMachineNeedsUpdate } from "@clawdline/core"
import { catalogWord } from "../../catalog.js"
import { showModalWithoutChoice } from "../../modal-focus.js"
import { failureSentence } from "../../legacy/bridge.js"
import { addProjectPlace, createProjectFolder, mintKey, readProjectFolders, searchProjectFolders, type ProjectFolderPage, type ProjectFolderSearch } from "../work/api.js"
import { hostedConsole } from "../terminal/api.js"
import { nextWord } from "../../next-strings.js"
import { notePlaces } from "../../session/first-run.js"
import { projectFolderPath, validProjectFolderName } from "./project-folder-name.js"
import "./project-folder-picker.css"

const word = (key: string) => catalogWord("projects", key)

/** Browse or create a folder on the selected machine, then register it. */
export function ProjectFolderPicker({ open, machine, onClose, onAdded }: {
  open: boolean
  machine?: string
  onClose: () => void
  onAdded: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const nameInput = useRef<HTMLInputElement>(null)
  const browseGeneration = useRef(0)
  const attempt = useRef<{ parent: string; name: string; key: string } | null>(null)
  const [page, setPage] = useState<ProjectFolderPage | null>(null)
  const [typed, setTyped] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState("")
  const [created, setCreated] = useState(false)
  // Searching: this machine's daemon looks two levels under home; another
  // machine (or a daemon too old to search) filters the folder shown below.
  const [find, setFind] = useState("")
  const [found, setFound] = useState<ProjectFolderSearch | null>(null)
  const [findFailed, setFindFailed] = useState(false)
  const [deepFind, setDeepFind] = useState(true)
  const findGeneration = useRef(0)
  const deep = deepFind && !machine && !hostedConsole()
  const findText = find.trim()
  useEffect(() => {
    const generation = ++findGeneration.current
    setFound(null); setFindFailed(false)
    if (!findText || !deep) return
    const timer = window.setTimeout(() => {
      searchProjectFolders(findText).then(answer => {
        if (generation === findGeneration.current) setFound(answer)
      }, cause => {
        if (generation !== findGeneration.current) return
        // A daemon from before search refuses `q`; filtering still helps.
        if (cause instanceof RefusalError && cause.status === 400 && cause.code === "bad_request") setDeepFind(false)
        else setFindFailed(true)
      })
    }, 250)
    return () => window.clearTimeout(timer)
  }, [findText, deep])
  const filtered = !deep && findText && page
    ? page.folders.filter(folder => folder.name.toLowerCase().includes(findText.toLowerCase())) : null

  async function browse(path?: string) {
    const generation = ++browseGeneration.current
    setBusy(true); setError(""); setCreated(false)
    try {
      const result = await readProjectFolders(path, machine)
      if (generation !== browseGeneration.current) return
      setPage(result)
      setTyped(result.path)
    } catch (cause) {
      if (generation === browseGeneration.current) setError(failureSentence(cause, word("folderReadFailed")))
    } finally { if (generation === browseGeneration.current) setBusy(false) }
  }

  useEffect(() => {
    const node = dialog.current
    if (!node) return
    setCreating(false); setName(""); setCreated(false); attempt.current = null
    setFind(""); setFound(null); setFindFailed(false); setDeepFind(true)
    if (open) {
      showModalWithoutChoice(node)
      setPage(null); setTyped(""); setError("")
      void browse()
    } else if (node.open) node.close()
    return () => { browseGeneration.current++ }
  }, [open, machine])

  useEffect(() => { if (creating) nameInput.current?.focus() }, [creating])

  function cancelCreate() {
    if (busy) return
    setCreating(false); setError("")
  }

  async function create() {
    if (!page || busy || !validProjectFolderName(name)) return
    const generation = ++browseGeneration.current
    const parent = page.path
    if (attempt.current?.parent !== parent || attempt.current?.name !== name) {
      attempt.current = { parent, name, key: mintKey() }
    }
    setBusy(true); setError("")
    try {
      const folder = await createProjectFolder(parent, name, attempt.current.key, machine)
      if (generation !== browseGeneration.current) return
      setCreating(false); setCreated(true); attempt.current = null
      setTyped(folder.path)
      // The receipt establishes creation even if the subsequent listing fails.
      setPage({ path: folder.path, parent, folders: [], truncated: false })
      try {
        const result = await readProjectFolders(folder.path, machine)
        if (generation === browseGeneration.current) setPage(result)
      } catch (cause) {
        if (generation === browseGeneration.current) setError(failureSentence(cause, word("folderReadFailed")))
      }
    } catch (cause) {
      if (generation !== browseGeneration.current) return
      if (cause instanceof RefusalError && cause.status < 500 && !["request_in_progress", "request_outcome_unknown", "receipt_expired", "idempotency_key_reused"].includes(cause.code)) attempt.current = null
      setError(asMachineNeedsUpdate(cause) ? catalogWord("next", "machineNeedsUpdate") : failureSentence(cause, word("folderCreateFailed")))
    } finally { if (generation === browseGeneration.current) setBusy(false) }
  }

  async function add() {
    if (!page || busy) return
    setBusy(true); setError("")
    try {
      await addProjectPlace(page.path, machine)
      if (!machine) notePlaces(1)
      onAdded()
      onClose()
    } catch (cause) {
      setError(failureSentence(cause, word("folderAddFailed")))
    } finally { setBusy(false) }
  }

  const invalidName = !!name && !validProjectFolderName(name)
  return <dialog ref={dialog} className="project-folder-picker"
    aria-labelledby="project-folder-picker-title"
    onCancel={event => { if (busy || creating) event.preventDefault(); if (creating) cancelCreate(); else if (!busy) onClose() }}
    onClose={onClose}>
    {creating ? <>
      <button type="button" className="project-folder-back" disabled={busy} onClick={cancelCreate}>{word("folderBack")}</button>
      <h2 id="project-folder-picker-title">{word("folderCreate")}</h2>
      <p className="project-folder-location">{word("folderCreateLocation")}<span>{page?.path}</span></p>
      <form onSubmit={event => { event.preventDefault(); void create() }}>
        <label htmlFor="project-folder-name">{word("folderName")}</label>
        <input ref={nameInput} id="project-folder-name" className="project-folder-name" value={name}
          onChange={event => { setName(event.target.value); setError(""); attempt.current = null }}
          disabled={busy} autoComplete="off" autoCapitalize="off" spellCheck={false}
          placeholder={word("folderNameExample")} aria-invalid={invalidName}
          aria-describedby={invalidName ? "project-folder-name-error" : undefined} />
        {invalidName && <p id="project-folder-name-error" role="alert">{word("folderNameInvalid")}</p>}
        <div className="project-folder-preview">
          <span>{word("folderFullPath")}</span>
          <output htmlFor="project-folder-name">{projectFolderPath(page?.path ?? "", name || word("folderName"))}</output>
        </div>
        <p className="project-folder-create-hint">{word("folderCreateHint")}</p>
        {error && <p role="alert">{error}</p>}
        <div className="project-folder-actions">
          <button type="button" disabled={busy} onClick={cancelCreate}>{word("folderCancel")}</button>
          <button type="submit" className="project-folder-confirm" aria-busy={busy} disabled={busy || !validProjectFolderName(name)}>
            <ActionProgress busy={busy} label={word("folderCreating")}>{word("folderCreateConfirm")}</ActionProgress>
          </button>
        </div>
      </form>
    </> : <>
      <h2 id="project-folder-picker-title">{nextWord("folderPickerTitle")}</h2>
      <p>{word("folderHint")}</p>
      <div className="project-folder-search">
        <label htmlFor="project-folder-search">{nextWord("folderSearchLabel")}</label>
        <input id="project-folder-search" type="search" value={find} onChange={event => setFind(event.target.value)}
          disabled={busy} autoComplete="off" autoCapitalize="off" spellCheck={false} maxLength={64}
          aria-describedby="project-folder-search-hint" />
        <p id="project-folder-search-hint" className="project-folder-search-hint">
          {!deep ? nextWord("folderFilterHint") : found?.protected_skipped ? nextWord("folderSearchHintMac") : nextWord("folderSearchHint")}
        </p>
      </div>
      {deep && findText ? <div className="project-folder-list" role="group" aria-label={nextWord("folderSearchLabel")} aria-busy={!found && !findFailed}>
        {found?.folders.map(folder => <button type="button" key={folder.path} disabled={busy}
          onClick={() => { setFind(""); void browse(folder.path) }}>
          <span className="project-folder-found-name">📁 {folder.name}</span>
          <span className="project-folder-found-where">{folder.path.slice(0, Math.max(0, folder.path.length - folder.name.length - 1))}</span>
        </button>)}
        {found && !found.folders.length && <p>{nextWord("folderSearchEmpty", { q: findText })}</p>}
        {findFailed && <p role="alert">{nextWord("folderSearchFailed")}</p>}
        {found?.truncated && <p role="status">{nextWord("folderSearchTruncated")}</p>}
      </div> : null}
      <form hidden={deep && !!findText} onSubmit={event => { event.preventDefault(); void browse(typed) }}>
        <label htmlFor="project-folder-path">{word("folderPath")}</label>
        <div className="project-folder-path-row">
          <input id="project-folder-path" value={typed} onChange={event => setTyped(event.target.value)}
            disabled={busy} autoComplete="off" spellCheck={false} />
          <button type="submit" disabled={busy || !typed}>{word("folderGo")}</button>
        </div>
      </form>
      {page && !(deep && findText) && <>
        <div className="project-folder-toolbar">
          <p className="project-folder-current">{page.path}</p>
          <button type="button" disabled={busy} onClick={() => { setCreating(true); setName(""); setError(""); attempt.current = null }}>{word("folderCreate")}</button>
        </div>
        {created && <p className="project-folder-created" role="status">{word("folderCreated")}</p>}
        <div className="project-folder-list" role="group" aria-label={word("folderList")}>
          {page.parent && <button type="button" disabled={busy} onClick={() => void browse(page.parent)}>{word("folderParent")}</button>}
          {(filtered ?? page.folders).map(folder => <button type="button" key={folder.path} disabled={busy}
            onClick={() => { setFind(""); void browse(folder.path) }}>📁 {folder.name}</button>)}
          {filtered && !filtered.length && <p>{nextWord("folderSearchEmpty", { q: findText })}</p>}
          {!filtered && !page.folders.length && <p>{word("folderEmpty")}</p>}
        </div>
        {page.truncated && <p role="status">{word("folderTruncated")}</p>}
      </>}
      {error && <p role="alert">{error}</p>}
      <div className="project-folder-actions">
        <button type="button" disabled={busy} onClick={onClose}>{word("folderCancel")}</button>
        <button type="button" className="project-folder-add" disabled={busy || !page || (deep && !!findText)} onClick={() => void add()}>
          {page ? nextWord("folderAddNamed", { name: page.path.split(/[\\/]/).filter(Boolean).pop() || page.path }) : word("folderAdd")}
        </button>
      </div>
    </>}
  </dialog>
}
