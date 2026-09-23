import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { isPicture, prepareReferencePicture } from "../../legacy/shots-bridge.js"
import { sessionFragment } from "../../session/address.js"
import { Mark } from "../../session/List.js"
import { failureWords, when } from "./shared.js"
import { WorkMilestones } from "./WorkMilestones.js"
import {
  assignNewWorkV2,
  assignWorkV2,
  addWorkV2Image,
  createWorkV2,
  deleteWorkV2,
  deleteWorkV2Image,
  editWorkV2,
  readProjectPlaces,
  readSessionWorkV2,
  readSessionsForWorkV2,
  readWorkV2,
  readWorkV2Proposals,
  resolveWorkV2Proposal,
  type ProjectPlace,
  type WorkV2Item,
  type WorkV2Kind,
  type WorkV2Proposal,
  type SessionWorkV2,
} from "./api.js"
import { sessionActivityName, sessionWorkCounts, sessionWorkStateName } from "./session-assignment.js"

const KINDS: WorkV2Kind[] = ["feature", "issue", "epic", "refactor", "plan"]
const PHASES = ["assigning", "assigned", "implementing", "verifying", "merging", "deploying"]
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
  const unassigned = items.filter((item) => item.area === "unassigned" && !item.closed_at)
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
      <BoardRegion title="規劃區" items={planning} sessions={sessions} busy={busy} failure={failure} clearFailure={() => setFailure("")} run={run} />
      <BoardRegion title="待指派" items={unassigned} sessions={sessions} busy={busy} failure={failure} clearFailure={() => setFailure("")} run={run} />
      {PHASES.map((phase) => <BoardRegion key={phase} title={phaseName(phase)}
        items={items.filter((item) => item.area === phase && !item.closed_at)} sessions={sessions} busy={busy}
        failure={failure} clearFailure={() => setFailure("")} run={run} />)}
      {done.length > 0 && <details className="work-section work-done"><summary><div className="work-section-head"><h2>已關閉</h2><span className="work-count">{done.length}</span></div></summary>
        <div className="work-cards">{done.map((item) => <WorkCard key={item.id} item={item} sessions={sessions} busy={busy}
          failure={failure} clearFailure={() => setFailure("")} run={run} />)}</div>
      </details>}
    </div>
    {creating && <NewWorkModal places={places} initialProject={project} busy={!!busy} failure={failure} onClose={() => setCreating(false)} onCreate={(body, files) => {
      void run("create", async () => {
        const answer = await createWorkV2(body)
        setCreating(false)
        let version = answer.item.version
        try {
          for (let index = 0; index < files.length; index++) {
            const picture = await prepareReferencePicture(files[index])
            const uploaded = await addWorkV2Image(answer.item.id, version, picture, index)
            version = uploaded.item.version
          }
        } catch (error) {
          await load()
          throw error
        }
      })
    }} />}
  </section>
}

function BoardRegion({ title, items, sessions, busy, failure, clearFailure, run }: {
  title: string
  items: WorkV2Item[]
  sessions: SessionRow[]
  busy: string
  failure: string
  clearFailure: () => void
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  if (!items.length) return null
  return <section className="work-section"><div className="work-section-head"><h2>{title}</h2><span className="work-count">{items.length}</span></div>
    <div className="work-cards">{items.map((item) => <WorkCard key={item.id} item={item} sessions={sessions} busy={busy}
      failure={failure} clearFailure={clearFailure} run={run} />)}</div>
  </section>
}

function WorkCard({ item, sessions, busy, failure, clearFailure, run }: {
  item: WorkV2Item
  sessions: SessionRow[]
  busy: string
  failure: string
  clearFailure: () => void
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  const [terminal, setTerminal] = useState("")
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const imagePicker = useRef<HTMLInputElement>(null)
  const eligible = useMemo(() => sessions.filter((s) => s.cwd === item.project.path && s.sessionId), [sessions, item.project.path])
  const owner = item.owner_session ? sessions.find((session) => session.sessionId === item.owner_session) : undefined
  const assignable = item.area !== "planning" && !item.closed_at && !item.owner_session
  return <article className="work-card work-v2-card" data-work-id={item.id} data-phase={item.phase}>
    <div className="work-card-toolbar">
      <div className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span>{item.project.label}</span></div>
      <div className="work-card-controls" aria-label="項目操作">
        <button type="button" disabled={!!busy} onClick={() => { clearFailure(); setEditing(true) }}><span aria-hidden="true">✎</span> 編輯</button>
        {!item.closed_at && <button className="danger" type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setDeleting(true) }}><span aria-hidden="true">⌫</span> 刪除</button>}
      </div>
    </div>
    <span className="work-state">{item.kind} · {phaseName(item.phase)}</span>
    <h3>{item.title}</h3>
    <p>{item.description}</p>
    <WorkMilestones phase={item.phase} />
    {!!item.images?.length && <div className="work-reference-images" role="group" aria-label="參考圖片">
      {item.images.map((image) => <WorkReferenceImage key={image.id} item={item} image={image} busy={busy} run={run} />)}
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
    <div className="work-meta"><span>{item.project.available ? (item.condition || "正常") : "project_unavailable"}</span><span>更新 {when(item.updated_at)}</span>
      {owner ? <a className="work-session-link" href={sessionFragment(owner.id)}
        aria-label={`前往正在實作「${item.title}」的 Session`}>前往 Session · {owner.label || owner.id}<span aria-hidden="true">→</span></a>
        : item.owner_session && <span>Session {item.owner_session.slice(0, 8)}</span>}
    </div>
    {assignable && <div className="work-assignment">
      <SessionAssignmentPicker sessions={eligible} value={terminal} onChange={setTerminal} />
      <button className="chip on" type="button" disabled={!terminal || !!busy} onClick={() => void run(item.id, () => assignWorkV2(item, terminal))}>指派</button>
      <button className="chip" type="button" disabled={!!busy} onClick={() => void run(item.id, () => assignNewWorkV2(item))}>開新 Session</button>
    </div>}
    {editing && <EditWorkModal item={item} busy={!!busy} failure={failure} onClose={() => setEditing(false)} onSave={(title, description) => {
      void run(`edit-${item.id}`, () => editWorkV2(item, title, description)).then((ok) => { if (ok) setEditing(false) })
    }} />}
    {deleting && <DeleteWorkModal item={item} busy={!!busy} failure={failure} onClose={() => setDeleting(false)} onDelete={() => {
      void run(`delete-${item.id}`, () => deleteWorkV2(item)).then((ok) => { if (ok) setDeleting(false) })
    }} />}
  </article>
}

interface SessionWorkReading {
  page?: SessionWorkV2
  loading?: boolean
  error?: string
}

function SessionAssignmentPicker({ sessions, value, onChange }: {
  sessions: SessionRow[]
  value: string
  onChange: (id: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [readings, setReadings] = useState<Record<string, SessionWorkReading>>({})
  const root = useRef<HTMLDivElement>(null)
  const ticket = useRef(0)
  const selected = sessions.find((session) => session.id === value)
  const selectedReading = value ? readings[value] : undefined

  useEffect(() => () => { ticket.current += 1 }, [])

  const load = useCallback(async () => {
    const mine = ++ticket.current
    setReadings((current) => {
      const next = { ...current }
      for (const session of sessions) next[session.id] = { ...next[session.id], loading: true, error: undefined }
      return next
    })
    await Promise.all(sessions.map(async (session) => {
      try {
        const page = await readSessionWorkV2(session.id)
        if (mine !== ticket.current) return
        setReadings((current) => ({ ...current, [session.id]: { page } }))
      } catch (error) {
        if (mine !== ticket.current) return
        setReadings((current) => ({ ...current, [session.id]: { error: failureWords(error) } }))
      }
    }))
  }, [sessions])

  useEffect(() => {
    if (!open) return
    void load()
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
  }, [open, load])

  const choose = (id: string) => { onChange(id); setOpen(false) }
  return <div className="work-session-picker" ref={root}>
    <button className="work-session-trigger" type="button" aria-label="指派既有 Session" aria-haspopup="listbox"
      aria-expanded={open} onClick={() => setOpen((shown) => !shown)}>
      {selected ? <><SessionStateDot session={selected} /><span><b>{selected.label || selected.id}</b>
        <small>{sessionActivityName(selected.state)} · {sessionWorkStateName(selected.work_state)}</small></span></>
        : <><span className="work-session-placeholder" aria-hidden="true">◌</span><span>選擇既有 Session</span></>}
      <span className="work-project-chevron" aria-hidden="true">⌄</span>
    </button>
    {open && <div className="work-session-menu" role="listbox" aria-label="可指派的 Session">
      {sessions.map((session) => <SessionChoice key={session.id} session={session} reading={readings[session.id]}
        selected={session.id === value} onChoose={() => choose(session.id)} />)}
      {!sessions.length && <p className="work-project-empty">這個 Project 目前沒有可用的 Session。</p>}
    </div>}
    {selected && <SessionAssignmentDetail session={selected} reading={selectedReading} />}
  </div>
}

function SessionChoice({ session, reading, selected, onChoose }: {
  session: SessionRow
  reading?: SessionWorkReading
  selected: boolean
  onChoose: () => void
}) {
  const counts = reading?.page ? sessionWorkCounts(reading.page) : null
  return <button className="work-session-option" type="button" role="option" aria-selected={selected} onClick={onChoose}>
    <SessionStateDot session={session} />
    <span><b>{session.label || session.id}</b><small>{sessionActivityName(session.state)} · {sessionWorkStateName(session.work_state)}</small></span>
    <span className="work-session-counts">{reading?.loading ? "讀取中…" : reading?.error ? "讀不到工作" : counts
      ? `${counts.board} 看板 · ${counts.todos} TODO` : "—"}</span>
  </button>
}

function SessionStateDot({ session }: { session: SessionRow }) {
  return <span className="work-session-state-dot" data-state={session.state} aria-hidden="true" />
}

function SessionAssignmentDetail({ session, reading }: { session: SessionRow; reading?: SessionWorkReading }) {
  const page = reading?.page
  const counts = page ? sessionWorkCounts(page) : null
  return <section className="work-session-detail" aria-label={`${session.label || session.id} 的狀況`} aria-live="polite">
    <div className="work-session-detail-head"><strong>Session 狀況</strong><span>{sessionActivityName(session.state)} · {sessionWorkStateName(session.work_state)}</span></div>
    {(session.line || session.work_note) && <p>{session.line || session.work_note}</p>}
    {reading?.loading && !page ? <p>正在讀取看板與 TODO…</p> : reading?.error ? <p className="work-note" role="alert">工作資訊讀取失敗：{reading.error}</p> : page ? <>
      <p>尚未完成：{counts?.board ?? 0} 個看板項目 · {counts?.todos ?? 0} 個 TODO</p>
      <SessionWorkList title="還在做" empty="目前沒有負責中的看板項目。" rows={page.assigned_items.map((item) => ({
        id: item.id, title: item.title, meta: `${item.project.label} · ${phaseName(item.phase)}${item.condition ? ` · ${item.condition}` : ""}`,
      }))} />
      <SessionWorkList title="直接待辦" empty="目前沒有未完成的 TODO。" rows={page.direct_todos.map((todo) => ({
        id: todo.id, title: todo.text, meta: todo.read_at ? "已讀" : todo.sent_at ? "已傳送" : "尚未傳送",
      }))} />
      <SessionWorkList title="最近完成" empty="目前沒有最近完成的看板項目。" rows={(page.recent_items ?? []).map((item) => ({
        id: item.id, title: item.title, meta: item.project.label,
      }))} />
      {page.truncated && <small className="work-session-truncated">還有更多工作未列出；請進入 Session 查看完整清單。</small>}
    </> : <p>展開 Session 清單後讀取它的工作資訊。</p>}
  </section>
}

function SessionWorkList({ title, empty, rows }: {
  title: string
  empty: string
  rows: { id: string; title: string; meta: string }[]
}) {
  return <div className="work-session-work-list"><b>{title}</b>{rows.length ? <ul>{rows.map((row) => <li key={row.id}>
    <span>{row.title}</span><small>{row.meta}</small>
  </li>)}</ul> : <small>{empty}</small>}</div>
}

function EditWorkModal({ item, busy, failure, onClose, onSave }: {
  item: WorkV2Item
  busy: boolean
  failure: string
  onClose: () => void
  onSave: (title: string, description: string) => void
}) {
  const [title, setTitle] = useState(item.title)
  const [description, setDescription] = useState(item.description)
  const ready = !!title.trim() && !!description.trim()
  useModalDismiss(busy, onClose)
  return <div className="session-todo-modal work-edit-modal" role="dialog" aria-modal="true" aria-labelledby={`work-edit-title-${item.id}`}
    onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}>
    <form onSubmit={(event) => { event.preventDefault(); if (ready) onSave(title.trim(), description.trim()) }}>
      <div className="work-modal-head"><div><p className="board-eyebrow">EDIT WORK ITEM</p><h2 id={`work-edit-title-${item.id}`}>編輯看板項目</h2></div>
        <button className="work-modal-close" type="button" aria-label="關閉" disabled={busy} onClick={onClose}>×</button></div>
      <label>標題<input className="work-input" value={title} maxLength={240} autoFocus onChange={(event) => setTitle(event.target.value)} /></label>
      <label>描述<textarea value={description} maxLength={65536} onChange={(event) => setDescription(event.target.value)} /></label>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>{busy ? "儲存中…" : "儲存變更"}</button>
        <button className="chip" type="button" disabled={busy} onClick={onClose}>取消</button></div>
    </form>
  </div>
}

function DeleteWorkModal({ item, busy, failure, onClose, onDelete }: {
  item: WorkV2Item
  busy: boolean
  failure: string
  onClose: () => void
  onDelete: () => void
}) {
  useModalDismiss(busy, onClose)
  return <div className="session-todo-modal work-delete-modal" role="dialog" aria-modal="true" aria-labelledby={`work-delete-title-${item.id}`}
    onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}>
    <form onSubmit={(event) => { event.preventDefault(); onDelete() }}>
      <div className="work-modal-head"><div><p className="board-eyebrow">DELETE WORK ITEM</p><h2 id={`work-delete-title-${item.id}`}>刪除看板項目？</h2></div>
        <button className="work-modal-close" type="button" aria-label="關閉" disabled={busy} onClick={onClose}>×</button></div>
      <p><strong>{item.title}</strong> 會從看板與負責 Session 的待辦移除。執行紀錄仍會保留，避免工作憑空消失。</p>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <div className="work-actions"><button className="chip danger" type="submit" disabled={busy}>{busy ? "刪除中…" : "確認刪除"}</button>
        <button className="chip" type="button" disabled={busy} onClick={onClose}>保留項目</button></div>
    </form>
  </div>
}

function useModalDismiss(busy: boolean, onClose: () => void) {
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === "Escape" && !busy) onClose() }
    document.addEventListener("keydown", close)
    return () => document.removeEventListener("keydown", close)
  }, [busy, onClose])
}

function WorkReferenceImage({ item, image, busy, run }: {
  item: WorkV2Item
  image: NonNullable<WorkV2Item["images"]>[number]
  busy: string
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  const [source, setSource] = useState("")
  const [failed, setFailed] = useState("")
  useEffect(() => {
    let active = true
    let objectURL = ""
    setFailed("")
    void fetch(`/v1/work/v2/images/${image.id}`, { credentials: "same-origin" }).then(async (response) => {
      if (!response.ok) throw new Error(`reference image answered ${response.status}`)
      objectURL = URL.createObjectURL(await response.blob())
      if (active) setSource(objectURL)
      else URL.revokeObjectURL(objectURL)
    }).catch((error: unknown) => { if (active) setFailed(failureWords(error)) })
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [image.id])
  return <figure className="work-reference-image">
    {source ? <a href={source} target="_blank" rel="noreferrer" aria-label={`開啟參考圖片 ${image.title}`}>
      <img src={source} alt={image.title} width={image.width} height={image.height} />
    </a> : <div className="work-reference-loading" role={failed ? "alert" : undefined}>{failed || "載入圖片…"}</div>}
    <figcaption title={image.title}>{image.title}</figcaption>
    {!item.closed_at && <button type="button" aria-label={`移除參考圖片 ${image.title}`} disabled={!!busy}
      onClick={() => void run(`image-delete-${image.id}`, () => deleteWorkV2Image(item, image.id))}>×</button>}
  </figure>
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

function NewWorkModal({ places, initialProject, busy, failure, onClose, onCreate }: { places: ProjectPlace[]; initialProject: string; busy: boolean; failure: string; onClose: () => void; onCreate: (body: Parameters<typeof createWorkV2>[0], images: File[]) => void }) {
  const [projectID, setProjectID] = useState(initialProject)
  const [kind, setKind] = useState<WorkV2Kind>("feature")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [images, setImages] = useState<File[]>([])
  const imagePicker = useRef<HTMLInputElement>(null)
  const ready = !!projectID && !!title.trim() && !!description.trim()
  useModalDismiss(busy, onClose)
  return <div className="session-todo-modal work-new-modal" role="dialog" aria-modal="true" aria-labelledby="work-new-v2-title"
    onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}><form onSubmit={(e) => {
    e.preventDefault(); if (!ready) return
    onCreate({ project_id: projectID, kind, title: title.trim(), description: description.trim(), deployment_policy: "agent_decides" }, images)
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
    <div className="work-modal-images">
      <span>參考圖片</span>
      <input ref={imagePicker} type="file" accept="image/*,.heic,.heif" multiple hidden onChange={(event) => {
        const selected = Array.from(event.currentTarget.files ?? []).filter(isPicture)
        event.currentTarget.value = ""
        setImages((current) => [...current, ...selected].slice(0, 6))
      }} />
      <div className="work-modal-image-tools">
        <button className="chip" type="button" disabled={busy || images.length >= 6} onClick={() => imagePicker.current?.click()}>＋ 加入參考圖片</button>
        <small>{images.length} / 6 · 建立項目後上傳</small>
      </div>
      {!!images.length && <ul className="work-modal-image-list">{images.map((file, index) => <li key={`${file.name}-${file.lastModified}-${index}`}>
        <span title={file.name}>{file.name}</span><button type="button" disabled={busy} aria-label={`移除 ${file.name}`}
          onClick={() => setImages((current) => current.filter((_, at) => at !== index))}>×</button>
      </li>)}</ul>}
    </div>
    {failure && <p className="work-note" role="alert">{failure}</p>}
    <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>{busy ? "建立中…" : "建立"}</button><button className="chip" type="button" disabled={busy} onClick={onClose}>取消</button></div>
  </form></div>
}

function phaseName(phase: string): string {
  return ({ created: "建立", assigning: "認領中", assigned: "已認領", implementing: "實作", verifying: "驗證", merging: "Merge 回 Git", deploying: "部署", done: "完成", cancelled: "取消" } as Record<string, string>)[phase] ?? phase
}
