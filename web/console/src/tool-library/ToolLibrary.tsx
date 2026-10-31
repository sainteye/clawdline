import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import type { SessionRow } from "@clawdline/contract"
import { currentCatalogTag } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { mayWriteThroughCurrentTransport } from "../client.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { libraryConnection } from "./api.js"
import { filterProjects, localized, repositoryURL, sourceLink, toolTarget, type Library, type Prepared, type Project, type Skill } from "./model.js"
import "./tool-library.css"

const w = (key: string) => nextWord(("toolLibrary" + key) as Parameters<typeof nextWord>[0])
const name = (value: Record<string, string> | undefined) => localized(value, currentCatalogTag())
const modeName = (mode: string) => w(mode === "runtime" ? "Runtime" : mode === "framework" ? "Framework" : "Instruction")
const projectKind = (p: Project) => p.integration_kind === "framework" ? "EvaluationFramework" : p.integration_kind === "library" ? "Library" : "Skills"
const projectNote = (p: Project) => p.integration_kind === "framework" ? "EvaluationFrameworkNote" : p.integration_kind === "library" ? "LibraryNote" : "Discover"

export function ToolLibrary({ row, machine, onClose, onPrepared }: {
  row: SessionRow; machine?: string | null; onClose(): void; onPrepared(value: Prepared, identity: string): void
}) {
  const identity = toolTarget(row, machine || "")
  const current = useRef(identity); current.current = identity
  const dialog = useRef<HTMLDialogElement>(null)
  const connection = useRef(libraryConnection())
  const operation = useRef<AbortController | null>(null)
  const [data, setData] = useState<Library | null>(null)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [tab, setTab] = useState<"saved" | "recommended" | null>(null)
  const [category, setCategory] = useState("all")
  const [query, setQuery] = useState("")
  const [project, setProject] = useState<Project | null>(null)
  const [preparing, setPreparing] = useState(false)
  const [adding, setAdding] = useState(false)
  const [flow, setFlow] = useState(false)
  const [skillID, setSkillID] = useState("")
  const [target, setTarget] = useState("")
  const [task, setTask] = useState("")
  const [command, setCommand] = useState("")
  const [scope, setScope] = useState("task")
  const [url, setURL] = useState("")
  const [title, setTitle] = useState("")
  const [summary, setSummary] = useState("")
  const [customPath, setCustomPath] = useState("")
  const [customCategory, setCustomCategory] = useState("development")
  const retryWrite = useRef<(() => void) | null>(null)
  const write = mayWriteThroughCurrentTransport()
  const fail = (caught: unknown) => {
    const code = (caught as { code?: string })?.code
    setError(["not_found", "not_implemented", "unknown_command", "cloud_not_carried"].includes(code || "") ? w("Unsupported")
      : code === "version_conflict" ? w("Conflict") : ["stale_session", "execution_generation_changed"].includes(code || "") ? w("Stale") : w("Failed") + (code ? ` (${code})` : ""))
  }
  const load = () => {
    operation.current?.abort()
    const abort = new AbortController(), mine = identity
    operation.current = abort; retryWrite.current = null; setLoading(true); setError("")
    void connection.current.read(abort.signal).then(value => {
      if (!abort.signal.aborted && mine === current.current) {
        setData(value)
        setTab(selected => selected ?? (value.saved.length ? "saved" : "recommended"))
      }
    }, caught => { if (!abort.signal.aborted && mine === current.current) fail(caught) })
      .finally(() => { if (!abort.signal.aborted && mine === current.current) setLoading(false) })
  }
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null
    showModalWithoutChoice(dialog.current)
    load()
    return () => { operation.current?.abort(); dialog.current?.close(); if (opener?.isConnected) opener.focus({ preventScroll: true }) }
    // One dialog belongs to one machine/conversation/execution.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useEffect(() => { if (identity !== initial.current || !connection.current.current()) onClose() }, [identity])
  const initial = useRef(identity)
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => { if (body.current) body.current.scrollTop = 0 }, [project?.id, preparing, adding, tab, flow])

  const change = (request: { expected_version: number; project?: Project; remove?: string }, key = crypto.randomUUID()) => {
    if (busy || !write) return
    const abort = new AbortController(), mine = identity
    operation.current = abort; setBusy(true); setError("")
    const retry = () => change(request, key)
    void connection.current.write(request, key, abort.signal).then(answer => {
      if (abort.signal.aborted || current.current !== mine) return
      setData(old => old ? { ...old, ...answer } : null); retryWrite.current = null; setAdding(false)
    }, caught => { if (!abort.signal.aborted && current.current === mine) { retryWrite.current = retry; fail(caught) } })
      .finally(() => { if (!abort.signal.aborted && current.current === mine) setBusy(false) })
  }
  const saved = (p: Project) => data?.saved.some(x => x.id === p.id) ?? false
  const toggleSave = (p: Project) => { if (data) change(saved(p) ? { expected_version: data.version, remove: p.id } : { expected_version: data.version, project: p }) }
  const open = (p: Project, prepare = false, sk?: Skill) => {
    setProject(p); setPreparing(prepare); setAdding(false); setFlow(false); setError(""); setSkillID(sk?.id || p.skills[0]?.id || ""); setCommand("")
  }
  const prepare = () => {
    if (!project || busy || !identity || !write) return
    const abort = new AbortController(), mine = identity
    operation.current = abort; setBusy(true); setError("")
    void connection.current.prepare({ session: row.id, conversation: row.sessionId, generation: row.execution_generation || "",
      project_id: project.id, skill_id: skillID, task, target, command, mode: scope, locale: currentCatalogTag() }, crypto.randomUUID(), abort.signal).then(value => {
      if (!abort.signal.aborted && current.current === mine && connection.current.current()) onPrepared(value, mine)
    }, caught => { if (!abort.signal.aborted && current.current === mine) fail(caught) })
      .finally(() => { if (!abort.signal.aborted && current.current === mine) setBusy(false) })
  }
  const customSave = () => {
    const parsed = repositoryURL(url)
    if (!data || !parsed) { setError(w("InvalidURL")); return }
    const existing = [...data.saved, ...data.catalog.projects].find(p => p.repo_url.toLowerCase() === parsed.url.toLowerCase())
    if (existing) { change({ expected_version: data.version, project: existing }); return }
    const localizedText = (s: string): Record<string,string> => currentCatalogTag() === "zh-Hant" ? { en: s, "zh-Hant": s } : { en: s }
    const p: Project = { id: (parsed.owner + "--" + parsed.repo).toLowerCase(), repo_url: parsed.url, owner: parsed.owner, repo: parsed.repo,
      name: localizedText(title.trim() || parsed.repo), summary: localizedText(summary.trim()), category_id: customCategory,
      category: {}, source_commit: "", license: null, skills: customPath.trim() ? [{ id: "custom", native_name: "", name: { en: "SKILL.md" }, purpose: {},
        path: customPath.trim(), mode: "instruction", requirements: [], source_url: "" }] : [],
      icon: { url: "", kind: "owner_avatar", source_url: "" } }
    change({ expected_version: data.version, project: p })
  }
  const back = () => { setProject(null); setPreparing(false); setAdding(false); setFlow(false); setError("") }
  const projects = data ? (tab === "saved" ? data.saved : [...data.catalog.projects].sort((a, b) => Number(b.recommendation_tier === "start_here") - Number(a.recommendation_tier === "start_here"))) : []
  const shown = filterProjects(projects, category, query, currentCatalogTag())
  const button = (p: Project) => <button type="button" className="tl-primary" disabled={!write || busy || !identity} onClick={() => open(p, true)}>{w("Use")}</button>
  const source = (p: Project) => <dl className="tl-facts">
    <dt>{w("Category")}</dt><dd>{name(p.category)}</dd><dt>{w("Revision")}</dt><dd><code>{p.source_commit || w("ResolveRevision")}</code></dd>
    {["library", "framework"].includes(p.integration_kind || "") && <><dt>{w("Mode")}</dt><dd>{w(projectKind(p))}</dd></>}
    <dt>{w("License")}</dt><dd>{p.license || w("Unknown")}</dd><dt>{w("Icon")}</dt><dd>{w(p.icon.kind === "project" ? "IconProject" : p.icon.asset ? "IconOwner" : "IconInitials")}</dd>
    <dt>{w("Source")}</dt><dd><a href={p.repo_url} target="_blank" rel="noreferrer">{p.owner}/{p.repo} ↗</a></dd>
  </dl>
  return createPortal(<dialog ref={dialog} className="tl-dialog" aria-labelledby="tl-title" onCancel={e => { e.preventDefault(); onClose() }}>
    <header className="tl-head"><div><h2 id="tl-title">{w("Title")}</h2><p>{w("Description")}</p></div><button type="button" disabled={busy} onClick={() => {
      back(); setURL(""); setTitle(""); setSummary(""); setCustomPath(""); setCustomCategory("development"); setAdding(true)
    }}>+ {w("Add")}</button><button type="button" aria-label={w("Close")} onClick={onClose}>×</button></header>
    <div className="tl-layout">
      <nav className="tl-sidebar" aria-label={w("Category")}><button type="button" className={category === "all" ? "active" : ""} onClick={() => { back(); setCategory("all") }}>{w("All")} <small>{projects.length}</small></button>
        {data?.catalog.categories.map(c => <button type="button" key={c.id} className={category === c.id ? "active" : ""} onClick={() => { back(); setCategory(c.id) }}>{name(c.name)} <small>{projects.filter(p => p.category_id === c.id).length}</small></button>)}
        <button type="button" className="tl-flow-link" onClick={() => { back(); setFlow(true) }}>{w("Flow")}</button><p>{w("Existing")}</p>
      </nav>
      <div ref={body} className="tl-main">
        {error && <div role="alert" className="tl-error"><p>{error}</p>{retryWrite.current && <p>{w("WriteFailed")}</p>}<button type="button" disabled={busy} onClick={() => retryWrite.current ? retryWrite.current() : load()}>{w("Retry")}</button>{retryWrite.current && <button type="button" onClick={load}>{w("Loading").replace("…", "")}</button>}</div>}
        {loading && <p role="status">{w("Loading")}</p>}
        {!write && <p className="tl-notice">{w("Readonly")}</p>}
        {!row.sessionId && <p className="tl-notice">{w("NotStarted")}</p>}
        {adding ? <section className="tl-form"><button type="button" className="tl-back" onClick={back}>‹ {w("Back")}</button><h3>{w("Add")}</h3><p>{w("CustomNote")}</p>
          <label>{w("URL")}<input type="url" value={url} onChange={e => setURL(e.target.value)} placeholder="https://github.com/owner/repository" /></label>
          <label>{w("Name")}<input value={title} onChange={e => setTitle(e.target.value)} /></label><label>{w("Summary")}<textarea value={summary} onChange={e => setSummary(e.target.value)} /></label>
          <label>{w("Category")}<select value={customCategory} onChange={e => setCustomCategory(e.target.value)}>{data?.catalog.categories.map(c => <option key={c.id} value={c.id}>{name(c.name)}</option>)}</select></label>
          <label>{w("Path")}<input value={customPath} onChange={e => setCustomPath(e.target.value)} placeholder="skills/example/SKILL.md" /></label><button type="button" className="tl-primary" onClick={customSave} disabled={!write || busy || !data}>{w("Save")}</button>
        </section> : flow ? <section><button type="button" className="tl-back" onClick={back}>‹ {w("Back")}</button><h3>{w("Flow")}</h3><p className="tl-flow">{w("FlowText")}</p><p>{w("PrepareNote")}</p><p className="tl-notice">{w("NoReceipt")}</p></section>
          : project ? <section>
            <div className="tl-detail-nav"><button type="button" className="tl-back" onClick={() => preparing ? setPreparing(false) : back()}>‹ {w("Back")}</button><span>{saved(project) ? w("IsSaved") : w("RecommendationNote")}</span></div>
            <div className="tl-project-title"><ProjectIcon project={project} /><div><h3>{name(project.name)}</h3><a href={project.repo_url} target="_blank" rel="noreferrer">{project.owner}/{project.repo} ↗</a></div></div>
            {preparing ? <div className="tl-form"><h3>{w("PrepareTitle")}</h3><p>{w("PrepareNote")}</p>
              {project.skills.length > 0 && <label>{w("Skill")}<select value={skillID} onChange={e => setSkillID(e.target.value)}>{project.skills.map(sk => <option key={sk.id} value={sk.id}>{name(sk.name)} · {sk.native_name || sk.path}</option>)}</select></label>}
              {!project.skills.length && <p className="tl-notice">{w(projectNote(project))}</p>}
              <label>{w("Command")}<input value={command} onChange={e => setCommand(e.target.value)} placeholder={project.repo === "impeccable" ? "critique / polish / audit" : ""} /></label>
              <label>{w("Target")}<input value={target} onChange={e => setTarget(e.target.value)} /></label><label>{w("Task")}<textarea value={task} onChange={e => setTask(e.target.value)} placeholder={w("TaskPlaceholder")} /></label>
              <fieldset className="tl-install"><legend>{w("Installation")}</legend><label><input type="radio" name="tl-scope" value="task" checked={scope === "task"} onChange={() => setScope("task")} /><span><b>{w("TaskOnly")}</b><small>{w("TaskOnlyDescription")}</small></span></label><label><input type="radio" name="tl-scope" value="project" checked={scope === "project"} onChange={() => setScope("project")} /><span><b>{w("ProjectInstall")}</b><small>{w("ProjectDescription")}</small></span></label></fieldset>
              <p className="tl-notice">{w("NoReceipt")}</p><button type="button" className="tl-primary" onClick={prepare} disabled={busy || !write || !identity || !task.trim()}>{busy ? w("Preparing") : w("PrepareButton")}</button>
            </div> : <><p className="tl-summary">{name(project.summary)}</p><div className="tl-detail-grid"><div><h4>{w(projectKind(project))}</h4>{project.skills.map(sk => <div key={sk.id} className="tl-skill"><div><b>{name(sk.name)}</b><p>{name(sk.purpose)}</p><code>{sk.native_name} · {modeName(sk.mode)}</code><p><code>{sk.path}</code></p><small>{sk.requirements?.length ? sk.requirements.map(name).join(" · ") : w("NoRequirements")}</small></div><button type="button" disabled={!write || busy || !identity} onClick={() => open(project, true, sk)}>{w("Use")}</button></div>)}
              {!project.skills.length && <p>{w(projectNote(project))}</p>}
              {!!project.requirements?.length && <><h4>{w("Requirements")}</h4>{project.requirements.map((v, i) => <p key={i}>{name(v)}</p>)}</>}
              <details className="tl-detail-extra"><summary>{w("Evidence")}</summary>{project.evidence?.map((e, i) => <p key={i}><a href={sourceLink(e.url)} target="_blank" rel="noreferrer">{e.kind} {e.rank ? `#${e.rank}` : ""} ↗</a><br /><small>{e.scope} · {e.snapshot_at || e.observed_at}<br />{name(e.note)}</small></p>)}</details>
              <details className="tl-detail-extra"><summary>{w("Limitations")}</summary>{project.limitations?.map((v, i) => <p key={i}>{name(v)}</p>)}<p>{w("NoReceipt")}</p></details>
            </div><aside>{source(project)}<div className="tl-detail-buttons"><button type="button" onClick={() => toggleSave(project)} disabled={!write || busy || !data}>{w(saved(project) ? "Remove" : "Save")}</button>{button(project)}</div></aside></div></>}
          </section> : <>
            <div className="tl-tabs" role="tablist" aria-label={w("Title")}>{(["saved", "recommended"] as const).map(id => <button type="button" key={id} role="tab" aria-selected={tab === id} onClick={() => { setTab(id); setCategory("all"); setQuery("") }}>{w(id === "saved" ? "Saved" : "Recommended")} <small>{id === "saved" ? data?.saved.length : data?.catalog.projects.length}</small></button>)}</div>
            <input className="tl-search" type="search" aria-label={w("Search")} placeholder={w("Search")} value={query} onChange={e => setQuery(e.target.value)} />
            <div className="tl-mobile-categories"><label>{w("Category")}<select value={category} onChange={e => setCategory(e.target.value)}><option value="all">{w("All")}</option>{data?.catalog.categories.map(c => <option key={c.id} value={c.id}>{name(c.name)}</option>)}</select></label></div>
            {tab === "recommended" && <p className="tl-caption">{w("RecommendationNote")}</p>}
            {!loading && data && !shown.length && <p className="tl-empty">{tab === "saved" && !data.saved.length && !query && category === "all" ? w("Empty") : w("NoMatch")}</p>}
            <div className="tl-projects">{shown.map(p => <article key={p.id} className="tl-project"><div className="tl-project-title"><ProjectIcon project={p} /><div><h3>{name(p.name)}</h3><a href={p.repo_url} target="_blank" rel="noreferrer">{p.owner}/{p.repo} ↗</a></div><span className="tl-tag">{saved(p) ? w("IsSaved") : p.recommendation_tier === "start_here" ? w("Start") : name(p.category)}</span></div><p>{name(p.summary)}</p><div className="tl-skill-tags">{p.skills.slice(0, 3).map(sk => <span key={sk.id}>{name(sk.name)}</span>)}</div><div className="tl-project-footer"><small>{name(p.category)} · {["library", "framework"].includes(p.integration_kind || "") ? w(projectKind(p)) : `${p.skills.length} Skills`}</small><div><button type="button" onClick={() => open(p)}>{w("Details")}</button>{button(p)}</div></div></article>)}</div>
            <button type="button" className="tl-mobile-flow tl-back" onClick={() => setFlow(true)}>{w("Flow")}</button>
          </>}
      </div>
    </div><footer className="tl-footer">{w("Scope")}</footer>
  </dialog>, document.body)
}

function ProjectIcon({ project }: { project: Project }) {
  const [broken, setBroken] = useState(false)
  const asset = project.icon.asset
  // Only compiled, same-origin assets are fetched automatically. A custom
  // repository uses initials until its agent has inspected a real project icon.
  return asset?.startsWith("/tool-library/") && !broken
    ? <img className="tl-project-icon" src={asset} alt={name(project.name)} loading="lazy" onError={() => setBroken(true)} />
    : <span className="tl-project-icon tl-initial" aria-label={name(project.name)}>{project.repo.slice(0, 2).toUpperCase()}</span>
}
