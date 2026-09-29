import { useEffect, useRef, useState } from "react"
import type { ProjectPlace } from "../work/api.js"
import { ProjectTreeIcon } from "./ProjectTreeIcon.js"
import { ProjectFileError } from "./project-files-api.js"
import { listProjectDirectory, readProjectTreeFile, type TreeContent, type TreeEntry, type TreeListing } from "./project-tree-api.js"
import "./project-explorer.css"

function failure(reason: unknown): string {
  if (reason instanceof ProjectFileError) {
    switch (reason.code) {
      case "not_text": return "這不是可預覽的 UTF-8 文字檔。請在機器上開啟。"
      case "file_too_large": return "檔案超過文字預覽的大小上限。請在機器上開啟。"
      case "unsafe_file": return "檔案或資料夾是連結，或已在讀取時變動。請重新讀取資料夾。"
      case "file_permission": case "forbidden": return "這個連線沒有讀取權限。請在機器上檢查。"
      case "file_not_found": return "檔案已移動或刪除。請重新讀取資料夾。"
      case "project_not_found": return "專案已不在這台機器的清單中。請回專案列表重新選擇。"
      case "cloud_not_carried": case "cloud_feature_unavailable": return "這台機器尚未提供檔案樹。請確認機器與 Cloud 的版本。"
      default: return "目前無法讀取。請檢查機器與連線後重試。"
    }
  }
  return "目前無法讀取。請檢查機器與連線後重試。"
}

function DirectoryRows({ listing, listings, loading, errors, selected, open, choose, retry }: {
  listing: TreeListing
  listings: Record<string, TreeListing>
  loading: Set<string>
  errors: Record<string, string>
  selected: string
  open(path: string): void
  choose(entry: TreeEntry): void
  retry(path: string): void
}) {
  return <ul className="project-explorer-entries">
    {listing.entries.map(entry => <li key={entry.path}>
      {entry.kind === "directory" ? <details className="project-explorer-folder" onToggle={event => { if (event.currentTarget.open) open(entry.path) }}>
        <summary><span aria-hidden="true" className="project-explorer-caret">▸</span><ProjectTreeIcon kind="folder" /><span>{entry.name}</span></summary>
        <div className="project-explorer-children">
          {loading.has(entry.path) && <p role="status">正在讀取…</p>}
          {errors[entry.path] && <p role="alert">{errors[entry.path]} <button type="button" onClick={() => retry(entry.path)}>重試</button></p>}
          {listings[entry.path] && <DirectoryRows listing={listings[entry.path]} listings={listings} loading={loading} errors={errors} selected={selected} open={open} choose={choose} retry={retry} />}
        </div>
      </details> : entry.kind === "file" ? <button type="button" className="project-explorer-file" aria-current={selected === entry.path ? "true" : undefined} onClick={() => choose(entry)}>
        <ProjectTreeIcon kind="file" /><span>{entry.name}</span>
      </button> : <span className="project-explorer-unavailable" title={entry.kind === "link" ? "連結不會從檔案樹開啟" : "不支援的檔案類型"}>
        <ProjectTreeIcon kind={entry.kind} /><span>{entry.name}</span><small>{entry.kind === "link" ? "連結" : "無法預覽"}</small>
      </span>}
    </li>)}
    {listing.entries.length === 0 && <li className="project-explorer-empty">這個資料夾沒有檔案。</li>}
    {listing.truncated && <li className="project-explorer-truncated" role="status">這個資料夾項目過多，僅顯示已讀取的部分。請在機器上查看其餘項目。</li>}
  </ul>
}

export function ProjectExplorer({ place }: { place: ProjectPlace }) {
  const [listings, setListings] = useState<Record<string, TreeListing>>({})
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [selected, setSelected] = useState("")
  const [content, setContent] = useState<TreeContent | null>(null)
  const [fileError, setFileError] = useState("")
  const [fileLoading, setFileLoading] = useState(false)
  const generation = useRef(0)
  const readSerial = useRef(0)
  const inFlight = useRef(new Set<string>())
  const nav = useRef<HTMLElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)

  const load = async (directory: string) => {
    if (inFlight.current.has(directory)) return
    inFlight.current.add(directory)
    const ticket = generation.current
    setLoading(new Set(inFlight.current))
    setErrors(previous => ({ ...previous, [directory]: "" }))
    try {
      const listing = await listProjectDirectory(place.id, directory)
      if (ticket === generation.current) setListings(previous => ({ ...previous, [directory]: listing }))
    } catch (reason) {
      if (ticket === generation.current) setErrors(previous => ({ ...previous, [directory]: failure(reason) }))
    } finally {
      inFlight.current.delete(directory)
      if (ticket === generation.current) setLoading(new Set(inFlight.current))
    }
  }

  useEffect(() => {
    generation.current++
    void load("")
    return () => { generation.current++; readSerial.current++ }
  // This component is keyed by Project. A new instance owns the next Project.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [place.id])

  const choose = async (entry: TreeEntry) => {
    const ticket = ++readSerial.current
    setSelected(entry.path); setContent(null); setFileError(""); setFileLoading(true)
    try {
      const file = await readProjectTreeFile(place.id, entry.path)
      if (ticket !== readSerial.current) return
      setContent(file)
      requestAnimationFrame(() => { heading.current?.scrollIntoView({ block: "nearest" }); heading.current?.focus({ preventScroll: true }) })
    } catch (reason) { if (ticket === readSerial.current) setFileError(failure(reason)) }
    finally { if (ticket === readSerial.current) setFileLoading(false) }
  }

  const root = listings[""]
  return <section className="project-explorer" aria-labelledby="project-explorer-title">
    <div className="project-explorer-head">
      <div><h3 id="project-explorer-title">專案檔案</h3><p>逐層展開資料夾；選取檔案後才讀取文字內容。此處僅供閱讀，不會修改檔案。</p></div>
      <button type="button" disabled={loading.has("")} onClick={() => void load("")}>重新讀取根目錄</button>
    </div>
    <div className="project-explorer-layout">
      <nav ref={nav} className="project-explorer-nav" aria-label={`${place.label} 的檔案樹`}>
        <strong className="project-explorer-root">{place.label}</strong>
        {loading.has("") && <p role="status">正在讀取資料夾…</p>}
        {errors[""] && <p role="alert">{errors[""]} <button type="button" onClick={() => void load("")}>重試</button></p>}
        {root && <DirectoryRows listing={root} listings={listings} loading={loading} errors={errors} selected={selected}
          open={directory => { if (!listings[directory]) void load(directory) }} choose={entry => void choose(entry)} retry={directory => void load(directory)} />}
      </nav>
      <div className="project-explorer-preview">
        {!selected && <p className="project-explorer-hint">從左側選擇文字檔，內容會顯示在這裡。</p>}
        {selected && <>
          <div className="project-explorer-preview-head">
            <div><h4 ref={heading} tabIndex={-1}>{selected.split("/").at(-1)}</h4><code>{selected}</code></div>
            <button type="button" onClick={() => { const row = nav.current?.querySelector<HTMLButtonElement>('button[aria-current="true"]'); row?.scrollIntoView({ block: "nearest" }); row?.focus({ preventScroll: true }) }}>返回檔案列表</button>
          </div>
          {fileLoading && <p role="status">正在讀取檔案…</p>}
          {fileError && <p role="alert">{fileError} <button type="button" onClick={() => void choose({ name: selected.split("/").at(-1) ?? selected, path: selected, kind: "file" })}>重試</button></p>}
          {content && <pre className="project-explorer-text" tabIndex={0}>{content.text}</pre>}
        </>}
      </div>
    </div>
  </section>
}
