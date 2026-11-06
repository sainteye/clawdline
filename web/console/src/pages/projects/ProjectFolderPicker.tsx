import { useEffect, useRef, useState } from "react"
import { ActionProgress } from "../../ui/ActionProgress.js"
import { RefusalError, asMachineNeedsUpdate } from "@clawdline/core"
import { catalogWord } from "../../catalog.js"
import { showModalWithoutChoice } from "../../modal-focus.js"
import { failureSentence } from "../../legacy/bridge.js"
import { addProjectPlace, createProjectFolder, mintKey, readProjectFolders, type ProjectFolderPage } from "../work/api.js"
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
      <h2 id="project-folder-picker-title">{word("folderTitle")}</h2>
      <p>{word("folderHint")}</p>
      <form onSubmit={event => { event.preventDefault(); void browse(typed) }}>
        <label htmlFor="project-folder-path">{word("folderPath")}</label>
        <div className="project-folder-path-row">
          <input id="project-folder-path" value={typed} onChange={event => setTyped(event.target.value)}
            disabled={busy} autoComplete="off" spellCheck={false} />
          <button type="submit" disabled={busy || !typed}>{word("folderGo")}</button>
        </div>
      </form>
      {page && <>
        <div className="project-folder-toolbar">
          <p className="project-folder-current">{page.path}</p>
          <button type="button" disabled={busy} onClick={() => { setCreating(true); setName(""); setError(""); attempt.current = null }}>{word("folderCreate")}</button>
        </div>
        {created && <p className="project-folder-created" role="status">{word("folderCreated")}</p>}
        <div className="project-folder-list" role="group" aria-label={word("folderList")}>
          {page.parent && <button type="button" disabled={busy} onClick={() => void browse(page.parent)}>{word("folderParent")}</button>}
          {page.folders.map(folder => <button type="button" key={folder.path} disabled={busy}
            onClick={() => void browse(folder.path)}>📁 {folder.name}</button>)}
          {!page.folders.length && <p>{word("folderEmpty")}</p>}
        </div>
        {page.truncated && <p role="status">{word("folderTruncated")}</p>}
      </>}
      {error && <p role="alert">{error}</p>}
      <div className="project-folder-actions">
        <button type="button" disabled={busy} onClick={onClose}>{word("folderCancel")}</button>
        <button type="button" disabled={busy || !page} onClick={() => void add()}>{word("folderAdd")}</button>
      </div>
    </>}
  </dialog>
}
