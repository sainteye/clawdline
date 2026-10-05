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
      <small>{STATUS[file.status]}{file.source === "global" ? " · 唯讀" : ""}</small>
    </button>)}
  </>
}

const STATUS: Record<ProjectFile["status"], string> = {
  ready: "可檢視", missing: "尚未建立", unsafe: "連結或非一般檔案，已略過",
  unreadable: "目前無法讀取", too_large: "檔案過大",
}

function describe(error: unknown): string {
  if (error instanceof ProjectFileError) {
    switch (error.code) {
      case "file_changed": return "檔案已在別處變更。草稿仍在這裡；請複製草稿，再重新讀取並比對。"
      case "file_permission": case "forbidden": case "file_read_only": return "這個連線沒有儲存權限。草稿仍在這裡；可先複製內容。"
      case "project_not_found": return "專案已不在這台機器的清單中。請回專案列表重新選擇。"
      case "unsafe_file": return "檔案或資料夾已變成連結。未儲存；請重新讀取清單。"
      case "file_not_found": return "檔案已移除。草稿仍在這裡；請重新讀取清單。"
      case "file_too_large": case "body_too_large": return "檔案超過此介面的大小上限；請在機器上編輯。"
      case "not_text": return "檔案不是可編輯的 UTF-8 文字；請在機器上檢查。"
      case "cloud_not_carried": case "cloud_feature_unavailable": return "這個連線尚未提供檔案檢視。請確認機器與 Cloud 的版本。"
      default: return "目前無法讀取或儲存檔案；請檢查機器上的檔案與連線後重試。"
    }
  }
  return "目前無法完成操作。請檢查連線後重試。"
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
    if (dirty && !window.confirm("放棄這個檔案尚未儲存的內容？")) { editor.current?.focus(); return }
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
    setBusy(true); onState({ dirty: true, busy: true }); setError(""); setNotice("正在儲存…")
    const key = crypto.randomUUID()
    try {
      const saved = await saveProjectFile(place.id, file.id, content.version, value, key)
      if (ticket !== serial.current) return
      savedConfirmed = true
      setContent(saved); setDraft(saved.text); setEditing(false); setNotice("檔案已儲存並重新讀取。")
      void reloadList()
    } catch (reason) {
      if (ticket !== serial.current) return
      if (reason instanceof ProjectFileError && reason.uncertain) {
        setNotice("儲存結果待確認，正在重新讀取…")
        try {
          const live = await readProjectFile(place.id, file.id)
          if (ticket !== serial.current) return
          if (live.text === value) { savedConfirmed = true; setContent(live); setDraft(live.text); setEditing(false); setNotice("重新讀取後確認檔案已儲存。") }
          else { setError("無法確認這次儲存是否生效。草稿仍在這裡；請複製並比對目前檔案。") }
        } catch (rereadReason) { if (ticket === serial.current) setError(`無法確認是否已儲存：${describe(rereadReason)} 草稿仍在這裡；恢復後請重新讀取。`) }
      } else setError(describe(reason))
    } finally { setBusy(false); onState({ dirty: !savedConfirmed, busy: false }) }
  }

  const reloadSelected = async () => {
    if (!selected || busy) return
    const file = list?.files.find(f => f.id === selected)
    if (file) await choose(file)
  }

  const groups = useMemo(() => ([
    { id: "codex", title: "Codex 指令" }, { id: "claude", title: "Claude Code 指令" }, { id: "skills", title: "Skills 技能" },
  ] as const), [])
  const chosen = list?.files.find(f => f.id === selected)
  return <section className="project-files" aria-labelledby="project-files-title">
    <div className="project-files-head">
      <div><h3 id="project-files-title">指令與技能檔案</h3>
        <p>列出這個專案及本機全域找到的磁碟候選檔案；Session 實際採用的指令與技能可能因覆寫、外掛及啟動時間而不同。</p></div>
      <button type="button" disabled={loading || busy || dirty} onClick={() => void reloadList()}>重新讀取清單</button>
    </div>
    {listError && <p role="alert">檔案清單讀取失敗：{listError} 請按「重新讀取清單」。</p>}
    {loading && <p role="status">正在讀取檔案…</p>}
    {list && <>
      <div className="project-files-layout">
        <nav className="project-files-nav" aria-label="專案指令與技能檔案">
          {groups.map(group => <div className="project-files-group" key={group.id}>
            <h4>{group.title}</h4>
            {list.files.filter(f => groupOf(f) === group.id).length === 0 && <p>沒有找到檔案。</p>}
            {(["project", "global"] as const).map(source => {
              const files = list.files.filter(f => groupOf(f) === group.id && f.source === source)
              return files.length > 0 && <div className="project-files-source" key={source}>
                <h5>{source === "project" ? "此專案" : "本機全域"}</h5>
                <FolderRows folder={fileTree(files)} selected={selected} busy={busy} choose={file => void choose(file)} />
              </div>
            })}
          </div>)}
        </nav>
        <div className="project-files-detail">
          {!selected && <p className="project-files-hint">選擇檔案查看實際內容與可編輯狀態。</p>}
          {chosen && <>
            <h4 ref={contentHeading} tabIndex={-1}>{chosen.name}</h4>
            <p className="project-files-location">{chosen.source === "global" ? "本機全域" : "此專案"} · <code>{chosen.location}</code></p>
            {chosen.source === "global" && <p>這是全域檔案，可能影響所有專案；此處僅供閱讀。</p>}
            {chosen.status !== "ready" && <p role="status">{STATUS[chosen.status]}。{chosen.status === "missing" ? "目前不提供建立；可在專案目錄建立後重新讀取。" : "請在機器上檢查檔案，再重新讀取清單。"}</p>}
            {error && <p role="alert">{error}</p>}
            {notice && <p role="status">{notice}</p>}
            {content && <>
              {editing ? <label className="project-files-editor">檔案內容<textarea ref={editor} value={draft} onChange={event => { setDraft(event.target.value); onState({ dirty: event.target.value !== content.text, busy }) }} spellCheck={false} disabled={busy} /></label>
                : <pre className="project-files-text" tabIndex={0}>{content.text}</pre>}
              <div className="project-files-actions">
                {!editing && content.file.editable && <button type="button" onClick={() => { setEditing(true); requestAnimationFrame(() => editor.current?.focus()) }}>編輯檔案</button>}
                {editing && <><button type="button" disabled={!dirty || busy} onClick={() => void save()}>儲存檔案</button>
                  <button type="button" disabled={busy} onClick={() => { if (!dirty || window.confirm("放棄尚未儲存的內容？")) { setDraft(content.text); setEditing(false) } }}>取消編輯</button>
                  <button type="button" disabled={busy} onClick={() => void navigator.clipboard.writeText(draft).then(() => setNotice("已複製草稿。"),
                    // refusal-ok: a clipboard write is refused by the browser's permission, which carries no machine code to name.
                    () => setError("無法複製；請從編輯欄選取文字。"))}>複製草稿</button></>}
                {error && <button type="button" disabled={busy} onClick={() => void reloadSelected()}>重新讀取檔案</button>}
              </div>
            </>}
          </>}
        </div>
      </div>
      {list.truncated && <p role="status">檔案數量超出清單上限，仍有項目未列出。</p>}
      {list.skipped.length > 0 && <p role="status">有 {list.skipped.length} 個技能資料夾無法安全讀取，已略過；請在機器上檢查連結或權限。</p>}
    </>}
  </section>
}
