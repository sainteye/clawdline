import { useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { createPortal } from "react-dom"
import type { SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { prepareReferencePicture } from "../legacy/shots-bridge.js"
import { addDirectTodoV2Image, createDirectTodoV2, directTodoActionV2, readSessionWorkV2, readWorkV2Item, remindWorkV2, type DirectTodoV2, type SessionWorkV2, type WorkV2Image, type WorkV2Item } from "../pages/work/api.js"
import { failureWords, when } from "../pages/work/shared.js"
import { WorkMilestones } from "../pages/work/WorkMilestones.js"
import { WorkSteps } from "../pages/work/WorkSteps.js"
import { completionReports, WorkCompletionReports } from "../pages/work/WorkCompletionReport.js"
import { WorkIcon } from "../pages/work/WorkIcon.js"
import { PendingPictures } from "../pages/work/ReferencePictures.js"
import { VoiceTextarea } from "../pages/work/VoiceTextarea.js"
import { workWord } from "../pages/work/words.js"
import { Mark } from "./List.js"
import { addedBySession } from "./todo-author.js"
import "../pages/work/work.css"

/** The authoritative projection of unfinished assigned items plus direct user to-dos. */
export function Todos({ row, agentCount, agentPanel }: {
  row: SessionRow | null
  agentCount?: number | null
  agentPanel?: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [adding, setAdding] = useState(false)
  const [text, setText] = useState("")
  const [images, setImages] = useState<File[]>([])
  const [page, setPage] = useState<SessionWorkV2 | null>(null)
  const [detail, setDetail] = useState<WorkV2Item | null>(null)
  const [detailFailure, setDetailFailure] = useState("")
  const [detailActionFailure, setDetailActionFailure] = useState("")
  const [detailNotice, setDetailNotice] = useState("")
  const [failure, setFailure] = useState("")
  const [busy, setBusy] = useState("")
  const ticket = useRef(0)
  const rowID = row?.id ?? ""

  const load = useCallback(async () => {
    if (!rowID) return
    const mine = ++ticket.current
    try {
      const next = await readSessionWorkV2(rowID)
      if (mine === ticket.current) { setPage(next); setFailure("") }
    } catch (e) {
      if (mine === ticket.current) setFailure(failureWords(e))
    }
  }, [rowID])

  useEffect(() => {
    // Fleet refreshes replace SessionRow objects even when this is still the
    // same Session. Key the answer to its stable id: otherwise every refresh
    // clears a good answer, flashes "loading", and asks the work API again.
    ticket.current += 1
    setOpen(false); setAdding(false); setText(""); setImages([]); setPage(null); setDetail(null); setDetailFailure(""); setDetailActionFailure(""); setDetailNotice(""); setFailure("")
    if (rowID) void load()
  }, [rowID, load])

  if (!row) return null
  const openDirect = page?.direct_todos.filter((todo) => !todo.completed_at) ?? []
  const completedDirect = page?.direct_todos.filter((todo) => !!todo.completed_at) ?? []
  const count = (page?.assigned_items.length ?? 0) + openDirect.length
  const completedCount = (page?.recent_items.length ?? 0) + completedDirect.length
  const hasAssigned = !!page?.assigned_items.length
  const hasRecent = !!page?.recent_items.length
  const hasDirect = !!openDirect.length
  const hasCompletedDirect = !!completedDirect.length
  const empty = page !== null && !hasAssigned && !hasRecent && !hasDirect && !hasCompletedDirect
  const run = async (key: string, task: () => Promise<unknown>) => {
    if (busy) return false
    setBusy(key); setFailure("")
    try { await task(); await load(); return true } catch (e) { setFailure(failureWords(e)); return false } finally { setBusy("") }
  }
  const showDetail = (item: WorkV2Item) => {
    setDetail(item); setDetailFailure(""); setDetailActionFailure(""); setDetailNotice("")
    void readWorkV2Item(item.id).then((answer) => setDetail((current) => current?.id === item.id ? answer.item : current))
      .catch((error: unknown) => setDetailFailure(failureWords(error)))
  }
  const remindDetail = async (item: WorkV2Item) => {
    const key = `remind-${item.id}`
    if (busy) return
    setBusy(key); setDetailActionFailure(""); setDetailNotice("")
    try {
      const answer = await remindWorkV2(item)
      setDetail((current) => current?.id === item.id ? answer.item : current)
      setDetailNotice("已再次提醒這個 Session。")
      await load()
    } catch (error) {
      setDetailActionFailure(failureWords(error))
    } finally {
      setBusy("")
    }
  }

  return (
    <>
      <details className="session-todos" id="session-todos" open={open}
        onToggle={(ev) => {
          const next = (ev.currentTarget as HTMLDetailsElement).open
          setOpen(next)
          // The first answer is already in flight when a Session opens. Once
          // there is an answer, opening the fold is an explicit freshness ask.
          if (next && page !== null) void load()
        }}>
        <summary>
          <b>{workWord("todosTitle")}</b>
          <button className="session-todos-add" type="button" aria-label="新增 Session 待辦"
            onClick={(ev) => { ev.preventDefault(); ev.stopPropagation(); setAdding(true) }}><WorkIcon name="add" /></button>
          <span id="session-todos-count">{page ? count : L.strings.webLoading}</span>
          {!!completedCount && <span className="session-todos-completed" aria-label={`最近完成 ${completedCount} 個項目`}>
            <WorkIcon name="check" />{completedCount}
          </span>}
          {agentCount !== undefined ? <span className="session-todos-agent-count">
            {L.strings.webAgents} {agentCount === null ? "?" : agentCount}
          </span> : null}
        </summary>
        <div className="session-todos-body">
          {agentPanel}
          {failure && <p className="work-note" role="alert">{failure}</p>}
          {page && hasAssigned && <section className="session-todos-list" aria-label="負責項目">
            <p>這個 Session 尚未關閉的負責項目</p>
            {page.assigned_items.map((item) => (
              <SessionOwnedItem item={item} key={item.id} onOpen={() => showDetail(item)} />
            ))}
          </section>}
          {page && hasRecent && <section className="session-todos-list session-recent-work" aria-label="最近完成的項目">
            <p>最近完成的看板項目</p>
            {page.recent_items.map((item) => <SessionOwnedItem item={item} completed key={item.id} onOpen={() => showDetail(item)} />)}
          </section>}
          {page && hasDirect && <section className="session-todos-list" aria-label="直接待辦">
            <p>直接交給這個 Session 的待辦。✓✓ 只表示已同步到 Session，尚未完成；需要時可以再次 Send。</p>
            {openDirect.map((todo) => (
              <DirectTodo key={todo.id} todo={todo} conversation={row.sessionId} busy={busy === todo.id}
                onAction={(action) => { void run(todo.id, () => directTodoActionV2(rowID, todo.id, action)) }} />
            ))}
          </section>}
          {page && hasCompletedDirect && <section className="session-todos-list session-recent-todos" aria-label="最近完成的直接待辦">
            <p>最近完成的直接待辦</p>
            {completedDirect.map((todo) => <DirectTodo key={todo.id} todo={todo} conversation={row.sessionId} busy={busy === todo.id}
              onAction={(action) => { void run(todo.id, () => directTodoActionV2(rowID, todo.id, action)) }} />)}
          </section>}
          {empty && <p className="session-todos-empty">目前沒有待辦。</p>}
        </div>
        <button className="session-todos-backdrop" type="button" tabIndex={-1} aria-label="收起 Session 待辦"
          onClick={() => setOpen(false)} />
      </details>
      {adding && <div className="session-todo-modal" role="dialog" aria-modal="true" aria-labelledby="session-todo-modal-title">
        <form onSubmit={(ev) => {
          ev.preventDefault(); const value = text.trim(); if (!value) return
          void run("new", async () => {
            const pictures = await Promise.all(images.map((file) => prepareReferencePicture(file)))
            const answer = await createDirectTodoV2(rowID, value)
            let version = answer.todo.version
            for (let index = 0; index < pictures.length; index++) {
              const uploaded = await addDirectTodoV2Image(rowID, answer.todo.id, version, pictures[index], index)
              version = uploaded.todo.version
            }
          }).then((ok) => { if (ok) { setText(""); setImages([]); setAdding(false); setOpen(true) } })
        }}>
          <h2 id="session-todo-modal-title">新增 Session 待辦</h2>
          <p>輸入你希望這個 Session 接下來完成的事情。</p>
          <VoiceTextarea value={text} autoFocus maxLength={8192} aria-label="待辦內容" onValue={setText} />
          <PendingPictures images={images} busy={busy === "new"} note="建立待辦後上傳" onChange={setImages} />
          <div className="work-actions">
            <button className="chip on" type="submit" disabled={busy === "new" || !text.trim()}>新增</button>
            <button className="chip" type="button" onClick={() => { setAdding(false); setImages([]) }}>取消</button>
          </div>
        </form>
      </div>}
      {detail && <WorkItemDetailModal item={detail} failure={detailFailure} actionFailure={detailActionFailure} notice={detailNotice}
        reminding={busy === `remind-${detail.id}`} onRemind={() => { void remindDetail(detail) }}
        onClose={() => { setDetail(null); setDetailFailure(""); setDetailActionFailure(""); setDetailNotice("") }} />}
    </>
  )
}

function SessionOwnedItem({ item, completed = false, onOpen }: { item: WorkV2Item; completed?: boolean; onOpen: () => void }) {
  const hasReport = completionReports(item).length > 0
  return <article className={`session-owned-item${completed ? " completed" : ""}`} data-phase={item.phase}>
    <button className="session-owned-summary" type="button" onClick={onOpen}
      aria-label={hasReport ? `開啟「${item.title}」的結案報告` : `查看「${item.title}」的項目詳情`}>
      <Mark icon={item.project.icon as SessionRow["icon"]} cellPx={3} />
      <span><b>{item.title}</b><small>{item.project.label} · {item.kind} · {completed ? `已完成 ${when(item.closed_at)}` : phaseName(item.phase)}
        {item.condition ? <span className="session-work-condition"> · {conditionName(item.condition)}</span> : null}</small>
        {hasReport && <em className="session-owned-report">結案報告</em>}</span>
      <span className="session-owned-state">
        {completed && <span className="session-owned-complete" role="img" aria-label="已完成"><WorkIcon name="check" /></span>}
        <span className="session-owned-open"><WorkIcon name="open" /></span>
      </span>
    </button>
    <WorkMilestones phase={item.phase} />
    <WorkSteps steps={item.steps} />
  </article>
}

function WorkItemDetailModal({ item, failure, actionFailure, notice, reminding, onRemind, onClose }: {
  item: WorkV2Item
  failure: string
  actionFailure: string
  notice: string
  reminding: boolean
  onRemind: () => void
  onClose: () => void
}) {
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") onClose() }
    document.addEventListener("keydown", close)
    return () => document.removeEventListener("keydown", close)
  }, [onClose])
  // A phone's Session pane is itself fixed to the visual viewport. Keeping a
  // second fixed scroller inside it leaves iOS with no reliable pan target, so
  // the dialog lives beside the app root and owns the one scroll surface.
  return createPortal(<div className="session-todo-modal work-item-detail-modal" role="dialog" aria-modal="true"
    aria-labelledby={`session-work-detail-title-${item.id}`} onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <article className="work-created-panel work-item-detail-panel">
      <div className="work-modal-head"><div><p className="board-eyebrow">BOARD ITEM</p>
        <h2 id={`session-work-detail-title-${item.id}`}>{item.title}</h2></div>
        <button className="work-modal-close" type="button" aria-label="關閉" autoFocus onClick={onClose}><WorkIcon name="close" /></button></div>
      <div className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} />
        <span>{item.project.label} · {item.kind} · {phaseName(item.phase)}</span></div>
      {item.condition && <p className="session-work-detail-condition">{conditionName(item.condition)}</p>}
      {item.user_action && <section className="work-user-action" aria-label="需要你做的事">
        <strong>需要你做的事</strong><p>{item.user_action}</p>
      </section>}
      <p className="session-work-detail-description">{item.description}</p>
      <WorkMilestones phase={item.phase} />
      <WorkCompletionReports item={item} expanded />
      {!!item.images?.length && <div className="work-reference-images" role="group" aria-label="參考圖片">
        {item.images.map((image) => <ReferenceImage key={image.id} image={image} />)}
      </div>}
      {failure && <p className="work-note" role="alert">最新資料讀取失敗：{failure}</p>}
      {actionFailure && <p className="work-note" role="alert">提醒傳送失敗：{actionFailure}</p>}
      {notice && <p className="work-note" role="status">{notice}</p>}
      {!!item.owner_session && !item.closed_at && <div className="work-actions">
        <button className="chip on" type="button" disabled={reminding} onClick={onRemind}>
          {reminding ? "提醒中…" : notice ? "✓ 已提醒" : "再次提醒 Session"}
        </button>
      </div>}
      <div className="work-meta"><span>{deploymentPolicyName(item.deployment_policy)}</span>
        <span>{item.closed_at ? `完成 ${when(item.closed_at)}` : `更新 ${when(item.updated_at)}`}</span></div>
    </article>
  </div>, document.body)
}

function DirectTodo({ todo, conversation, busy, onAction }: { todo: DirectTodoV2; conversation?: string; busy: boolean; onAction: (action: "send" | "complete" | "delete") => void }) {
  const completed = !!todo.completed_at
  const own = addedBySession(todo, conversation)
  const receipt = todo.read_at ? { mark: "✓✓", words: "已同步到 Session，尚未完成", state: "read" }
    : todo.sent_at ? { mark: "✓", words: "已傳送，等待 Session 同步", state: "sent" }
      : { mark: "", words: "尚未傳送；Session 會在下一次讀取待辦時看到", state: "unsent" }
  return <article className={`session-direct-todo${completed ? " completed" : ""}`} data-author={own ? "session" : "person"}>
    {completed
      ? <button className="session-todo-check completed" type="button" disabled aria-label="已完成"><WorkIcon name="boxChecked" /></button>
      : <button className="session-todo-check" type="button" disabled={busy} aria-label="完成" onClick={() => onAction("complete")}><WorkIcon name="box" /></button>}
    <div><b>{todo.text}</b><small>{completed ? `完成 ${when(todo.completed_at!)}` : when(todo.created_at)} {own
      && <span className="session-todo-author" aria-label={workWord("todoAddedBySessionLabel")}
        title={workWord("todoAddedBySessionLabel")}>{workWord("todoAddedBySession")}</span>}{(completed || !own) && <span
      className="session-todo-receipt" data-state={completed ? "completed" : receipt.state}
      aria-label={completed ? "已完成" : receipt.words}>{completed ? "已完成" : <><span className="session-todo-receipt-mark" aria-hidden="true">{receipt.mark}</span>{receipt.words}</>}</span>}</small></div>
    {!!todo.images?.length && <div className="work-reference-images session-todo-images" role="group" aria-label="待辦參考圖片">
      {todo.images.map((image) => <ReferenceImage key={image.id} image={image} compact />)}
    </div>}
    <div className="work-actions">
      {!completed && !todo.sent_at && !todo.read_at && <button className="chip" type="button" disabled={busy} onClick={() => onAction("send")}>Send</button>}
      {!completed && !!todo.read_at && <button className="chip" type="button" disabled={busy} onClick={() => onAction("send")}>再次 Send</button>}
      <button className="chip danger" type="button" disabled={busy} onClick={() => onAction("delete")}>Delete</button>
    </div>
  </article>
}

function ReferenceImage({ image, compact = false }: { image: WorkV2Image; compact?: boolean }) {
  const [source, setSource] = useState("")
  const [failed, setFailed] = useState("")
  useEffect(() => {
    let active = true
    let objectURL = ""
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
  if (compact) return source ? <a className="session-todo-image-link" href={source} target="_blank" rel="noreferrer"
    aria-label={`開啟參考圖片 ${image.title}`} title={image.title}>
    <span>{image.title}</span><span aria-hidden="true">↗</span>
  </a> : <div className="session-todo-image-link" data-state={failed ? "failed" : "loading"} role={failed ? "alert" : undefined}>
    <span title={image.title}>{image.title}</span><span>{failed || "載入中…"}</span>
  </div>
  return <figure className="work-reference-image">
    {source ? <a href={source} target="_blank" rel="noreferrer" aria-label={`開啟參考圖片 ${image.title}`}>
      <img src={source} alt={image.title} width={image.width} height={image.height} />
    </a> : <div className="work-reference-loading" role={failed ? "alert" : undefined}>{failed || "載入圖片…"}</div>}
    <figcaption title={image.title}>{image.title}</figcaption>
  </figure>
}

function phaseName(phase: string): string {
  return ({ created: "建立", assigning: "認領中", assigned: "已認領", implementing: "實作", verifying: "驗證", merging: "Merge", deploying: "部署", done: "完成", cancelled: "取消" } as Record<string, string>)[phase] ?? phase
}

function conditionName(condition: string): string {
  return ({ waiting_user: "等待你的決定", blocked: "遇到阻礙", evidence_unknown: "缺少可驗證證據",
    owner_required: "等待負責人", owner_offline: "負責 Session 離線", assignment_failed: "指派失敗",
    assigned_unnotified: "已指派，尚未通知" } as Record<string, string>)[condition] ?? condition
}

function deploymentPolicyName(policy: WorkV2Item["deployment_policy"]): string {
  return ({ required: "需要部署", not_required: "不需要部署", agent_decides: "由 Agent 判斷是否部署" } as const)[policy]
}
