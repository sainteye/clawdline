import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { isPicture, prepareReferencePicture } from "../../legacy/shots-bridge.js"
import { Mark } from "../../session/List.js"
import { failureWords, when } from "./shared.js"
import {
  assignNewWorkV2,
  assignWorkV2,
  addWorkV2Image,
  createWorkV2,
  deleteWorkV2Image,
  readProjectPlaces,
  readSessionsForWorkV2,
  readWorkV2,
  readWorkV2Proposals,
  resolveWorkV2Proposal,
  type ProjectPlace,
  type WorkV2Item,
  type WorkV2Kind,
  type WorkV2Proposal,
} from "./api.js"

const KINDS: WorkV2Kind[] = ["feature", "issue", "epic", "refactor", "plan"]
const PHASES = ["created", "assigning", "assigned", "implementing", "verifying", "merging", "deploying"]
const KIND_META: Record<WorkV2Kind, { icon: string; label: string; description: string }> = {
  feature: { icon: "✦", label: "Feature", description: "加入一項使用者可以感受到的新能力" },
  issue: { icon: "!", label: "Issue", description: "修正錯誤、異常或不符合預期的行為" },
  epic: { icon: "◆", label: "Epic", description: "先放在規劃區的大型工作主題" },
  refactor: { icon: "↻", label: "Refactor", description: "先放在規劃區的內部結構改善" },
  plan: { icon: "≡", label: "Plan", description: "先放在規劃區的研究或實作計畫" },
}

export function WorkV2Page({ shown }: { shown: boolean }) {
  const [items, setItems] = useState<WorkV2Item[]>([])
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [sessions, setSessions] = useState<SessionRow[]>([])
  const [proposals, setProposals] = useState<WorkV2Proposal[]>([])
  const [project, setProject] = useState("")
  const [creating, setCreating] = useState(false)
  const [busy, setBusy] = useState("")
  const [failure, setFailure] = useState("")

  const load = useCallback(async () => {
    try {
      const [work, projects, live, suggestions] = await Promise.all([readWorkV2(project || undefined), readProjectPlaces(), readSessionsForWorkV2(), readWorkV2Proposals()])
      setItems(work.rows); setPlaces(projects.places); setSessions(live.sessions); setProposals(suggestions.rows); setFailure("")
    } catch (e) { setFailure(failureWords(e)) }
  }, [project])
  useEffect(() => {
    if (!shown) return
    void load()
    const timer = setInterval(() => { if (document.visibilityState === "visible") void load() }, 30_000)
    return () => clearInterval(timer)
  }, [shown, load])

  const run = async (key: string, task: () => Promise<unknown>) => {
    if (busy) return false
    setBusy(key); setFailure("")
    try { await task(); await load(); return true } catch (e) { setFailure(failureWords(e)); return false } finally { setBusy("") }
  }
  const planning = items.filter((item) => item.area === "planning" && !item.closed_at)
  const done = items.filter((item) => item.closed_at)

  return <section id="work" className="page board-page work-page" data-page-view="work" hidden={!shown} aria-labelledby="work-v2-title">
    <header className="board-head">
      <div><p className="board-eyebrow">WORK SYSTEM V2</p><h1 id="work-v2-title">看板</h1></div>
      <div className="work-head-tools">
        <button className="board-button" type="button" onClick={() => setCreating(true)}>＋ 建立項目</button>
        <button className="board-button" type="button" disabled={!!busy} onClick={() => void load()}>{L.strings.webInfoRefresh}</button>
      </div>
    </header>
    <div className="work-wrap">
      <p className="work-lede">所有項目由你建立與指派；Session 負責推進實作、驗證、Merge 與部署。</p>
      <ProjectPicker places={places} value={project} onChange={setProject} allowAll />
      {failure && <p className="work-note" role="alert">{failure}</p>}
      {proposals.length > 0 && <details className="work-fold" open><summary><strong>Agent 提案</strong><span className="work-count">{proposals.length}</span></summary>
        <div className="work-fold-body work-cards">{proposals.map((p) => <article className="work-card" key={p.id}>
          <span className="work-state">{p.kind} · {p.session_id.slice(0, 8)}</span><h3>{p.title}</h3><p>{p.description}</p><small>{p.reason}</small>
          <div className="work-actions"><button className="chip on" disabled={!!busy} onClick={() => void run(p.id, () => resolveWorkV2Proposal(p.id, "accept"))}>接受並建立</button>
            <button className="chip danger" disabled={!!busy} onClick={() => void run(p.id, () => resolveWorkV2Proposal(p.id, "reject"))}>拒絕</button></div>
        </article>)}</div>
      </details>}
      <BoardRegion title="規劃區" items={planning} sessions={sessions} busy={busy} run={run} />
      {PHASES.map((phase) => <BoardRegion key={phase} title={phaseName(phase)}
        items={items.filter((item) => item.area === "execution" && item.phase === phase)} sessions={sessions} busy={busy} run={run} />)}
      {done.length > 0 && <details className="work-section work-done"><summary><div className="work-section-head"><h2>已關閉</h2><span className="work-count">{done.length}</span></div></summary>
        <div className="work-cards">{done.map((item) => <WorkCard key={item.id} item={item} sessions={sessions} busy={busy} run={run} />)}</div>
      </details>}
    </div>
    {creating && <NewWorkModal places={places} initialProject={project} busy={!!busy} failure={failure} onClose={() => setCreating(false)} onCreate={(body) => {
      void run("create", () => createWorkV2(body)).then((ok) => { if (ok) setCreating(false) })
    }} />}
  </section>
}

function BoardRegion({ title, items, sessions, busy, run }: { title: string; items: WorkV2Item[]; sessions: SessionRow[]; busy: string; run: (key: string, task: () => Promise<unknown>) => Promise<boolean> }) {
  if (!items.length) return null
  return <section className="work-section"><div className="work-section-head"><h2>{title}</h2><span className="work-count">{items.length}</span></div>
    <div className="work-cards">{items.map((item) => <WorkCard key={item.id} item={item} sessions={sessions} busy={busy} run={run} />)}</div>
  </section>
}

function WorkCard({ item, sessions, busy, run }: { item: WorkV2Item; sessions: SessionRow[]; busy: string; run: (key: string, task: () => Promise<unknown>) => Promise<boolean> }) {
  const [terminal, setTerminal] = useState("")
  const imagePicker = useRef<HTMLInputElement>(null)
  const eligible = useMemo(() => sessions.filter((s) => s.cwd === item.project.path && s.sessionId), [sessions, item.project.path])
  const assignable = item.area === "execution" && !item.closed_at
  return <article className="work-card work-v2-card" data-work-id={item.id} data-phase={item.phase}>
    <div className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span>{item.project.label}</span></div>
    <span className="work-state">{item.kind} · {phaseName(item.phase)}</span>
    <h3>{item.title}</h3>
    <p>{item.description}</p>
    {!!item.images?.length && <div className="work-reference-images" role="group" aria-label="參考圖片">
      {item.images.map((image) => <figure key={image.id} className="work-reference-image">
        <a href={`/v1/work/v2/images/${image.id}`} target="_blank" rel="noreferrer" aria-label={`開啟參考圖片 ${image.title}`}>
          <img src={`/v1/work/v2/images/${image.id}`} alt={image.title} width={image.width} height={image.height} loading="lazy" />
        </a>
        <figcaption title={image.title}>{image.title}</figcaption>
        {!item.closed_at && <button type="button" aria-label={`移除參考圖片 ${image.title}`} disabled={!!busy}
          onClick={() => void run(`image-delete-${image.id}`, () => deleteWorkV2Image(item, image.id))}>×</button>}
      </figure>)}
    </div>}
    {!item.closed_at && <div className="work-reference-tools">
      <input ref={imagePicker} type="file" accept="image/*,.heic,.heif" multiple hidden onChange={(event) => {
        const files = Array.from(event.currentTarget.files ?? []).filter(isPicture)
        event.currentTarget.value = ""
        if (!files.length) return
        void run(`image-add-${item.id}`, async () => {
          if ((item.images?.length ?? 0) + files.length > 6) {
            throw new RefusalError(507, { error: "images_full", detail: "Each item keeps at most six reference images." })
          }
          let version = item.version
          for (let index = 0; index < files.length; index++) {
            const picture = await prepareReferencePicture(files[index])
            const answer = await addWorkV2Image(item.id, version, picture, (item.images?.length ?? 0) + index)
            version = answer.item.version
          }
        })
      }} />
      <button className="chip" type="button" disabled={!!busy || (item.images?.length ?? 0) >= 6}
        onClick={() => imagePicker.current?.click()}>＋ 參考圖片</button>
      <small>{item.images?.length ?? 0} / 6</small>
    </div>}
    <div className="work-meta"><span>{item.project.available ? (item.condition || "正常") : "project_unavailable"}</span><span>更新 {when(item.updated_at)}</span>{item.owner_session && <span>Session {item.owner_session.slice(0, 8)}</span>}</div>
    {assignable && <div className="work-assignment">
      <select className="work-input" value={terminal} onChange={(e) => setTerminal(e.target.value)} aria-label="指派既有 Session">
        <option value="">選擇既有 Session</option>{eligible.map((s) => <option key={s.id} value={s.id}>{s.label || s.id}</option>)}
      </select>
      <button className="chip on" type="button" disabled={!terminal || !!busy} onClick={() => void run(item.id, () => assignWorkV2(item, terminal))}>指派</button>
      <button className="chip" type="button" disabled={!!busy} onClick={() => void run(item.id, () => assignNewWorkV2(item))}>開新 Session</button>
    </div>}
  </article>
}

function ProjectPicker({ places, value, onChange, allowAll = false }: {
  places: ProjectPlace[]
  value: string
  onChange: (id: string) => void
  allowAll?: boolean
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const selected = places.find((place) => place.id === value)
  useEffect(() => {
    if (!open) return
    const closeOutside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("pointerdown", closeOutside)
    document.addEventListener("keydown", closeOnEscape)
    return () => {
      document.removeEventListener("pointerdown", closeOutside)
      document.removeEventListener("keydown", closeOnEscape)
    }
  }, [open])
  const choose = (id: string) => { onChange(id); setOpen(false) }
  return <div className="work-project-picker" ref={root}>
    <button className="work-project-trigger" type="button" aria-label="Project" aria-haspopup="listbox"
      aria-expanded={open} onClick={() => setOpen((shown) => !shown)}>
      {selected ? <Mark icon={selected.icon as SessionRow["icon"]} cellPx={3} /> : <span className="work-project-placeholder" aria-hidden="true">▦</span>}
      <span>{selected?.label || (allowAll ? "所有 Project" : "選擇 Project")}</span>
      <span className="work-project-chevron" aria-hidden="true">⌄</span>
    </button>
    {open && <div className="work-project-menu" role="listbox" aria-label="Project">
      {allowAll && <button type="button" role="option" aria-selected={!value} className="work-project-option"
        onClick={() => choose("")}><span className="work-project-placeholder" aria-hidden="true">▦</span><span>所有 Project</span></button>}
      {places.map((place) => <button type="button" role="option" aria-selected={place.id === value}
        className="work-project-option" key={place.id} onClick={() => choose(place.id)}>
        <Mark icon={place.icon as SessionRow["icon"]} cellPx={3} /><span>{place.label}</span>
        {place.id === value && <span className="work-project-check" aria-hidden="true">✓</span>}
      </button>)}
      {!places.length && <p className="work-project-empty">目前沒有可用的 Project。</p>}
    </div>}
  </div>
}

function NewWorkModal({ places, initialProject, busy, failure, onClose, onCreate }: { places: ProjectPlace[]; initialProject: string; busy: boolean; failure: string; onClose: () => void; onCreate: (body: Parameters<typeof createWorkV2>[0]) => void }) {
  const [projectID, setProjectID] = useState(initialProject)
  const [kind, setKind] = useState<WorkV2Kind>("feature")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const ready = !!projectID && !!title.trim() && !!description.trim()
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === "Escape" && !busy) onClose() }
    document.addEventListener("keydown", close)
    return () => document.removeEventListener("keydown", close)
  }, [busy, onClose])
  return <div className="session-todo-modal work-new-modal" role="dialog" aria-modal="true" aria-labelledby="work-new-v2-title"
    onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}><form onSubmit={(e) => {
    e.preventDefault(); if (!ready) return
    onCreate({ project_id: projectID, kind, title: title.trim(), description: description.trim(), deployment_policy: "agent_decides" })
  }}>
    <div className="work-modal-head"><div><p className="board-eyebrow">NEW WORK ITEM</p><h2 id="work-new-v2-title">建立看板項目</h2></div>
      <button className="work-modal-close" type="button" aria-label="關閉" disabled={busy} onClick={onClose}>×</button></div>
    <div className="work-modal-field"><span>Project</span><ProjectPicker places={places} value={projectID} onChange={setProjectID} /></div>
    <fieldset className="work-kind-field"><legend>類型</legend><div className="work-kind-list">
      {KINDS.map((value) => { const meta = KIND_META[value]; return <button key={value} type="button" className="work-kind-option"
        aria-pressed={kind === value} onClick={() => setKind(value)}><span className="work-kind-icon" aria-hidden="true">{meta.icon}</span>
        <span><b>{meta.label}</b><small>{meta.description}</small></span><span className="work-kind-radio" aria-hidden="true">{kind === value ? "●" : "○"}</span></button> })}
    </div></fieldset>
    <label>標題<input className="work-input" value={title} maxLength={240} autoFocus onChange={(e) => setTitle(e.target.value)} /></label>
    <label>描述<textarea value={description} onChange={(e) => setDescription(e.target.value)} /></label>
    {failure && <p className="work-note" role="alert">{failure}</p>}
    <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>{busy ? "建立中…" : "建立"}</button><button className="chip" type="button" disabled={busy} onClick={onClose}>取消</button></div>
  </form></div>
}

function phaseName(phase: string): string {
  return ({ created: "建立", assigning: "認領中", assigned: "已認領", implementing: "實作", verifying: "驗證", merging: "Merge 回 Git", deploying: "部署", done: "完成", cancelled: "取消" } as Record<string, string>)[phase] ?? phase
}
