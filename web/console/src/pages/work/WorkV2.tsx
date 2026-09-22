import { useCallback, useEffect, useMemo, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import * as L from "../../legacy/bridge.js"
import { Mark } from "../../session/List.js"
import { failureWords, when } from "./shared.js"
import {
  assignNewWorkV2,
  assignWorkV2,
  createWorkV2,
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
      <select className="work-input" value={project} aria-label="Project" onChange={(e) => setProject(e.target.value)}>
        <option value="">所有 Project</option>
        {places.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
      </select>
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
    {creating && <NewWorkModal places={places} initialProject={project} busy={!!busy} onClose={() => setCreating(false)} onCreate={(body) => {
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
  const eligible = useMemo(() => sessions.filter((s) => s.cwd === item.project.path && s.sessionId), [sessions, item.project.path])
  const assignable = item.area === "execution" && !item.closed_at
  return <article className="work-card work-v2-card" data-work-id={item.id} data-phase={item.phase}>
    <div className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span>{item.project.label}</span></div>
    <span className="work-state">{item.kind} · {phaseName(item.phase)}</span>
    <h3>{item.title}</h3>
    <p>{item.description}</p>
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

function NewWorkModal({ places, initialProject, busy, onClose, onCreate }: { places: ProjectPlace[]; initialProject: string; busy: boolean; onClose: () => void; onCreate: (body: Parameters<typeof createWorkV2>[0]) => void }) {
  const [projectID, setProjectID] = useState(initialProject)
  const [kind, setKind] = useState<WorkV2Kind>("feature")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const ready = !!projectID && !!title.trim() && !!description.trim()
  return <div className="session-todo-modal" role="dialog" aria-modal="true" aria-labelledby="work-new-v2-title"><form onSubmit={(e) => {
    e.preventDefault(); if (!ready) return
    onCreate({ project_id: projectID, kind, title: title.trim(), description: description.trim(), deployment_policy: "agent_decides" })
  }}>
    <h2 id="work-new-v2-title">建立看板項目</h2>
    <label>Project<select className="work-input" value={projectID} onChange={(e) => setProjectID(e.target.value)}><option value="">選擇 Project</option>{places.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}</select></label>
    <label>類型<select className="work-input" value={kind} onChange={(e) => setKind(e.target.value as WorkV2Kind)}>{KINDS.map((k) => <option key={k} value={k}>{k}</option>)}</select></label>
    <label>標題<input className="work-input" value={title} maxLength={240} autoFocus onChange={(e) => setTitle(e.target.value)} /></label>
    <label>描述<textarea value={description} onChange={(e) => setDescription(e.target.value)} /></label>
    <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>建立</button><button className="chip" type="button" onClick={onClose}>取消</button></div>
  </form></div>
}

function phaseName(phase: string): string {
  return ({ created: "建立", assigning: "認領中", assigned: "已認領", implementing: "實作", verifying: "驗證", merging: "Merge 回 Git", deploying: "部署", done: "完成", cancelled: "取消" } as Record<string, string>)[phase] ?? phase
}
