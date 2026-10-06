import { localizedLiteralMap } from "../../catalog.js"
import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { useEffect, useMemo, useRef, useState } from "react"
import type { ProjectPlace } from "../work/api.js"
import { ProjectTreeIcon } from "./ProjectTreeIcon.js"
import {
  listProjectFiles, readProjectFile, saveProjectFile, ProjectFileError,
  type ProjectFile, type ProjectFileContent, type ProjectFileList,
} from "./project-files-api.js"
import "./project-files.css"

type EditState = { dirty: boolean; busy: boolean }
type Folder = { name: string; path: string; folders: Map<string, Folder>; files: ProjectFile[] }

function fileTree(files: ProjectFile[]): Folder {
  const root: Folder = { name: "", path: "", folders: new Map(), files: [] }
  for (const file of files) {
    const parts = file.location.split("/").filter(Boolean)
    let parent = root
    for (const name of parts.slice(0, -1)) {
      let child = parent.folders.get(name)
      if (!child) {
        child = { name, path: parent.path ? `${parent.path}/${name}` : name, folders: new Map(), files: [] }
        parent.folders.set(name, child)
      }
      parent = child
    }
    parent.files.push(file)
  }
  return root
}

function FolderRows({ folder, selected, busy, choose }: {
  folder: Folder; selected: string; busy: boolean; choose(file: ProjectFile): void
}) {
  const folders = [...folder.folders.values()].sort((a, b) => a.name.localeCompare(b.name))
  const files = [...folder.files].sort((a, b) => a.name.localeCompare(b.name))
  return <>
    {folders.map(child => <details className="project-files-folder" key={child.path}>
      <summary><span aria-hidden="true" className="project-files-folder-arrow">▸</span><ProjectTreeIcon kind="folder" /><span>{child.name}</span></summary>
      <div className="project-files-folder-children"><FolderRows folder={child} selected={selected} busy={busy} choose={choose} /></div>
    </details>)}
    {files.map(file => <button type="button" key={file.id}
      className="project-files-row" aria-current={selected === file.id ? "true" : undefined}
      disabled={busy} onClick={() => choose(file)}>
      <ProjectTreeIcon kind="file" />
      <strong>{file.name}</strong><span>{file.location}</span>
      <small>{STATUS[file.status]}{catalogWord("literal", "cfa37229f340")}</small>
    </button>)}
  </>
}

const STATUS: Record<ProjectFile["status"], string> = localizedLiteralMap({
  ready: "92363068f5b3", missing: "8c6019531435", unsafe: "bec019f69985",
  unreadable: "82c6e1d1683d", too_large: "abc92ff61a07",
})

function describe(error: unknown): string {
  if (error instanceof ProjectFileError) {
    switch (error.code) {
      case "file_changed": return catalogWord("literal", "591f563d21e9")
      case "file_permission": case "forbidden": case "file_read_only": return catalogWord("literal", "4e6f482a5e6b")
      case "project_not_found": return catalogWord("literal", "a5ae08fd969e")
      case "unsafe_file": return catalogWord("literal", "4bacde8e39d3")
      case "file_not_found": return catalogWord("literal", "35eabd8dfb75")
      case "file_too_large": case "body_too_large": return catalogWord("literal", "c04de8c5889e")
      case "not_text": return catalogWord("literal", "b1d6d32d82f8")
      case "cloud_not_carried": case "cloud_feature_unavailable": return catalogWord("literal", "19a12eac60f0")
      default: return catalogWord("literal", "5ce9bf90d01a")
    }
  }
  return catalogWord("literal", "d928fb3f9c80")
}

function groupOf(file: ProjectFile): "codex" | "claude" | "skills" {
  return file.kind === "skill" ? "skills" : file.assistant
}

export function ProjectFiles({ place, onState }: { place: ProjectPlace; onState(state: EditState): void }) {
  const [list, setList] = useState<ProjectFileList | null>(null)
  const [listError, setListError] = useState("")
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState("")
  const [content, setContent] = useState<ProjectFileContent | null>(null)
  const [draft, setDraft] = useState("")
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState("")
  const [error, setError] = useState("")
  const serial = useRef(0)
  const contentHeading = useRef<HTMLHeadingElement>(null)
  const editor = useRef<HTMLTextAreaElement>(null)
  const dirty = !!content && editing && draft !== content.text
  useEffect(() => onState({ dirty, busy }), [dirty, busy, onState])

  const reloadList = async () => {
    const ticket = ++serial.current
    setLoading(true); setListError("")
    try {
      const next = await listProjectFiles(place.id)
      if (ticket === serial.current) setList(next)
    } catch (reason) {
      if (ticket === serial.current) { setList(null); setListError(describe(reason)) }
    } finally { if (ticket === serial.current) setLoading(false) }
  }

  useEffect(() => {
    setList(null); setSelected(""); setContent(null); setDraft(""); setEditing(false); setNotice(""); setError("")
    void reloadList()
    return () => { serial.current++ }
  // The component is keyed by Project in its parent; this effect runs once for that identity.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [place.id])

  const choose = async (file: ProjectFile) => {
    if (busy) return
    if (dirty && !window.confirm(catalogWord("literal", "0651603b0c62"))) { editor.current?.focus(); return }
    const ticket = ++serial.current
    setSelected(file.id); setContent(null); setDraft(""); setEditing(false); setError(""); setNotice("")
    if (file.status !== "ready") return
    setLoading(true)
    try {
      const next = await readProjectFile(place.id, file.id)
      if (ticket !== serial.current) return
      setContent(next); setDraft(next.text)
      requestAnimationFrame(() => { contentHeading.current?.scrollIntoView({ block: "nearest" }); contentHeading.current?.focus({ preventScroll: true }) })
    } catch (reason) { if (ticket === serial.current) setError(describe(reason)) }
    finally { if (ticket === serial.current) setLoading(false) }
  }

  const save = async () => {
    if (!content || !dirty || busy) return
    const file = content.file
    const value = draft
    const ticket = ++serial.current
    let savedConfirmed = false
    setBusy(true); onState({ dirty: true, busy: true }); setError(""); setNotice(catalogWord("literal", "d19eb852ab9d"))
    const key = crypto.randomUUID()
    try {
      const saved = await saveProjectFile(place.id, file.id, content.version, value, key)
      if (ticket !== serial.current) return
      savedConfirmed = true
      setContent(saved); setDraft(saved.text); setEditing(false); setNotice(catalogWord("literal", "d54b3df23fc9"))
      void reloadList()
    } catch (reason) {
      if (ticket !== serial.current) return
      if (reason instanceof ProjectFileError && reason.uncertain) {
        setNotice(catalogWord("literal", "725c63536f66"))
        try {
          const live = await readProjectFile(place.id, file.id)
          if (ticket !== serial.current) return
          if (live.text === value) { savedConfirmed = true; setContent(live); setDraft(live.text); setEditing(false); setNotice(catalogWord("literal", "693907cea312")) }
          else { setError(catalogWord("literal", "cfa6a364a5c2")) }
        } catch (rereadReason) { if (ticket === serial.current) setError(catalogFormat("template", "f5dc01fdffbd", [describe(rereadReason)])) }
      } else setError(describe(reason))
    } finally { setBusy(false); onState({ dirty: !savedConfirmed, busy: false }) }
  }

  const reloadSelected = async () => {
    if (!selected || busy) return
    const file = list?.files.find(f => f.id === selected)
    if (file) await choose(file)
  }

  const groups = useMemo(() => ([
    { id: "codex", title: catalogWord("literal", "8669637f659c") }, { id: "claude", title: catalogWord("literal", "4a596dd065ba") }, { id: "skills", title: catalogWord("literal", "03f843cf3e55") },
  ] as const), [])
  const chosen = list?.files.find(f => f.id === selected)
  return <section className="project-files" aria-labelledby="project-files-title">
    <div className="project-files-head">
      <div><h3 id="project-files-title">{catalogWord("inline", "7a67b3e6ab4e")}</h3>
        <p>{catalogWord("inline", "2c899f0c0469")}</p></div>
      <button type="button" disabled={loading || busy || dirty} onClick={() => void reloadList()}>{catalogWord("inline", "db1659dac0df")}</button>
    </div>
    {listError && <p role="alert">{catalogWord("inline", "5751fdee7531")}{listError}{catalogWord("inline", "fdf3ba351a22")}</p>}
    {loading && <p role="status">{catalogWord("inline", "2b3d59f666b3")}</p>}
    {list && <>
      <div className="project-files-layout">
        <nav className="project-files-nav" aria-label={catalogWord("inline", "d4d68a08e0e6")}>
          {groups.map(group => <div className="project-files-group" key={group.id}>
            <h4>{group.title}</h4>
            {list.files.filter(f => groupOf(f) === group.id).length === 0 && <p>{catalogWord("inline", "df2e1b644818")}</p>}
            {(["project", "global"] as const).map(source => {
              const files = list.files.filter(f => groupOf(f) === group.id && f.source === source)
              return files.length > 0 && <div className="project-files-source" key={source}>
                <h5>{source === "project" ? catalogWord("literal", "450e5d731947") : catalogWord("literal", "23bf8eafeef3")}</h5>
                <FolderRows folder={fileTree(files)} selected={selected} busy={busy} choose={file => void choose(file)} />
              </div>
            })}
          </div>)}
        </nav>
        <div className="project-files-detail">
          {!selected && <p className="project-files-hint">{catalogWord("inline", "e0e91da1e831")}</p>}
          {chosen && <>
            <h4 ref={contentHeading} tabIndex={-1}>{chosen.name}</h4>
            <p className="project-files-location">{chosen.source === "global" ? catalogWord("literal", "23bf8eafeef3") : catalogWord("literal", "450e5d731947")} · <code>{chosen.location}</code></p>
            {chosen.source === "global" && <p>{catalogWord("inline", "bb3ee00241a5")}</p>}
            {chosen.status !== "ready" && <p role="status">{STATUS[chosen.status]}。{chosen.status === "missing" ? catalogWord("literal", "ddc43085429e") : catalogWord("literal", "565085a5da03")}</p>}
            {error && <p role="alert">{error}</p>}
            {notice && <p role="status">{notice}</p>}
            {content && <>
              {editing ? <label className="project-files-editor">{catalogWord("inline", "e3f57fd6eacc")}<textarea ref={editor} value={draft} onChange={event => { setDraft(event.target.value); onState({ dirty: event.target.value !== content.text, busy }) }} spellCheck={false} disabled={busy} /></label>
                : <pre className="project-files-text" tabIndex={0}>{content.text}</pre>}
              <div className="project-files-actions">
                {!editing && content.file.editable && <button type="button" onClick={() => { setEditing(true); requestAnimationFrame(() => editor.current?.focus()) }}>{catalogWord("inline", "74aed729535a")}</button>}
                {editing && <><button type="button" disabled={!dirty || busy} onClick={() => void save()}>{catalogWord("inline", "9c6bde88026e")}</button>
                  <button type="button" disabled={busy} onClick={() => { if (!dirty || window.confirm(catalogWord("literal", "c5a4b86fa44f"))) { setDraft(content.text); setEditing(false) } }}>{catalogWord("inline", "7a3efd8a888d")}</button>
                  <button type="button" disabled={busy} onClick={() => void navigator.clipboard.writeText(draft).then(() => setNotice(catalogWord("literal", "d9489690da4d")),
                    // refusal-ok: a clipboard write is refused by the browser's permission, which carries no machine code to name.
                    () => setError(catalogWord("literal", "3b77a5005a39")))}>{catalogWord("inline", "c6438a8858d3")}</button></>}
                {error && <button type="button" disabled={busy} onClick={() => void reloadSelected()}>{catalogWord("inline", "ffeb28983cdb")}</button>}
              </div>
            </>}
          </>}
        </div>
      </div>
      {list.truncated && <p role="status">{catalogWord("inline", "b3980647fa20")}</p>}
      {list.skipped.length > 0 && <p role="status">{catalogFormat("count", "skillFoldersSkipped", [list.skipped.length])}</p>}
    </>}
  </section>
}
