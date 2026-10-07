import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { useEffect, useRef, useState } from "react"
import type { ProjectPlace } from "../work/api.js"
import { ProjectTreeIcon } from "./ProjectTreeIcon.js"
import { ProjectFileError } from "./project-files-api.js"
import { listProjectDirectory, readProjectTreeFile, type TreeContent, type TreeEntry, type TreeListing } from "./project-tree-api.js"
import "./project-explorer.css"

function failure(reason: unknown): string {
  if (reason instanceof ProjectFileError) {
    switch (reason.code) {
      case "not_text": return catalogWord("literal", "ef78406e2d2a")
      case "file_too_large": return catalogWord("literal", "3d56bf3cb7fb")
      case "unsafe_file": return catalogWord("literal", "fffb2491d13a")
      case "file_permission": case "forbidden": return catalogWord("literal", "2233fee94e8e")
      case "file_not_found": return catalogWord("literal", "d244c8aec386")
      case "project_not_found": return catalogWord("literal", "e04c8f261fd5")
      case "cloud_not_carried": case "cloud_feature_unavailable": return catalogWord("literal", "bdfe8879aeee")
      default: return catalogWord("literal", "febcaa56ae82")
    }
  }
  return catalogWord("literal", "febcaa56ae82")
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
          {loading.has(entry.path) && <p role="status">{catalogWord("inline", "a58b39df6cd2")}</p>}
          {errors[entry.path] && <p role="alert">{errors[entry.path]} <button type="button" onClick={() => retry(entry.path)}>{catalogWord("inline", "7e59d0f16293")}</button></p>}
          {listings[entry.path] && <DirectoryRows listing={listings[entry.path]} listings={listings} loading={loading} errors={errors} selected={selected} open={open} choose={choose} retry={retry} />}
        </div>
      </details> : entry.kind === "file" ? <button type="button" className="project-explorer-file" aria-current={selected === entry.path ? "true" : undefined} onClick={() => choose(entry)}>
        <ProjectTreeIcon kind="file" /><span>{entry.name}</span>
      </button> : <span className="project-explorer-unavailable" title={entry.kind === "link" ? catalogWord("literal", "fafba231d972") : catalogWord("literal", "bdff2374d8c9")}>
        <ProjectTreeIcon kind={entry.kind} /><span>{entry.name}</span><small>{entry.kind === "link" ? catalogWord("literal", "eacc3c13a4ba") : catalogWord("literal", "0bdbbebb7c1e")}</small>
      </span>}
    </li>)}
    {listing.entries.length === 0 && <li className="project-explorer-empty">{catalogWord("inline", "b96e60969fd5")}</li>}
    {listing.truncated && <li className="project-explorer-truncated" role="status">{catalogWord("inline", "03348feb5b04")}</li>}
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
      <div><h3 id="project-explorer-title">{catalogWord("inline", "3336bfb819f5")}</h3><p>{catalogWord("inline", "ea9cfe4f4afe")}</p></div>
      <button type="button" disabled={loading.has("")} onClick={() => void load("")}>{catalogWord("inline", "9146d108b060")}</button>
    </div>
    <div className="project-explorer-layout">
      <nav ref={nav} className="project-explorer-nav" aria-label={catalogFormat("template", "b744f91deceb", [place.label])}>
        <strong className="project-explorer-root">{place.label}</strong>
        {loading.has("") && <p role="status">{catalogWord("inline", "3b92a032b6cc")}</p>}
        {errors[""] && <p role="alert">{errors[""]} <button type="button" onClick={() => void load("")}>{catalogWord("inline", "7e59d0f16293")}</button></p>}
        {root && <DirectoryRows listing={root} listings={listings} loading={loading} errors={errors} selected={selected}
          open={directory => { if (!listings[directory]) void load(directory) }} choose={entry => void choose(entry)} retry={directory => void load(directory)} />}
      </nav>
      <div className="project-explorer-preview">
        {!selected && <p className="project-explorer-hint">{catalogWord("inline", "fb0d9c173c82")}</p>}
        {selected && <>
          <div className="project-explorer-preview-head">
            <div><h4 ref={heading} tabIndex={-1}>{selected.split("/").at(-1)}</h4><code>{selected}</code></div>
            <button type="button" onClick={() => { const row = nav.current?.querySelector<HTMLButtonElement>('button[aria-current="true"]'); row?.scrollIntoView({ block: "nearest" }); row?.focus({ preventScroll: true }) }}>{catalogWord("inline", "fdfd628f99b8")}</button>
          </div>
          {fileLoading && <p role="status">{catalogWord("inline", "2b3d59f666b3")}</p>}
          {fileError && <p role="alert">{fileError} <button type="button" onClick={() => void choose({ name: selected.split("/").at(-1) ?? selected, path: selected, kind: "file" })}>{catalogWord("inline", "7e59d0f16293")}</button></p>}
          {content && <pre className="project-explorer-text" tabIndex={0}>{content.text}</pre>}
        </>}
      </div>
    </div>
  </section>
}
