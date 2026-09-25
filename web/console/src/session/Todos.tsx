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
import { useReferenceImage } from "../pages/work/useReferenceImage.js"
import { workWord } from "../pages/work/words.js"
import { Mark } from "./List.js"
import { todoSend } from "./todo-send.js"
import { addedBySession } from "./todo-author.js"
import { todoProgress, todoProgressLabel, type TodoProgress } from "./todo-progress.js"
import { OneRead, readFailureReason, todoHeaderState, watchTodoRefresh } from "./todo-refresh.js"
import { nextWord } from "../next-strings.js"
import "../pages/work/work.css"
import "./todos.css"

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
  // A failed read is its own state, apart from a failed action: the header
  // shows it, and only a later successful read clears it.
  const [readFailure, setReadFailure] = useState<{ words: string; reason: string } | null>(null)
  const [reading, setReading] = useState(false)
  const [busy, setBusy] = useState("")
  const ticket = useRef(0)
  const reader = useRef<OneRead | null>(null)
  const rowID = row?.id ?? ""

  const load = useCallback(async () => {
    if (!rowID) return
    const mine = ++ticket.current
    setReading(true)
    try {
      const next = await readSessionWorkV2(rowID)
      if (mine === ticket.current) { setPage(next); setReadFailure(null) }
    } catch (e) {
      // The last good page stays: a refresh that failed marks it, it does
      // not blank it.
      if (mine === ticket.current) setReadFailure({ words: failureWords(e), reason: readFailureReason(e) })
    } finally {
      if (mine === ticket.current) setReading(false)
    }
  }, [rowID])

  useEffect(() => {
    // Fleet refreshes replace SessionRow objects even when this is still the
    // same Session. Key the answer to its stable id: otherwise every refresh
    // clears a good answer, flashes "loading", and asks the work API again.
    ticket.current += 1
    setOpen(false); setAdding(false); setText(""); setImages([]); setPage(null); setDetail(null); setDetailFailure(""); setDetailActionFailure(""); setDetailNotice(""); setFailure("")
    setReadFailure(null); setReading(false)
    if (!rowID) return
    // One read in flight per Session; the page asks again every fifteen
    // seconds while it is visible, and at once when it comes back, so rows a
    // Session writes appear without reopening it.
    const one = new OneRead(load)
    reader.current = one
    void one.ask()
    const stop = watchTodoRefresh(() => { void one.ask() })
    return () => {
      stop()
      if (reader.current === one) reader.current = null
    }
  }, [rowID, load])

  /** Ask again; `fresh` when the answer must postdate something just done. */
  const refresh = useCallback((fresh = false) => reader.current?.ask(fresh) ?? Promise.resolve(), [])

  if (!row) return null
  const openDirect = page?.direct_todos.filter((todo) => !todo.completed_at) ?? []
  const completedDirect = page?.direct_todos.filter((todo) => !!todo.completed_at) ?? []
  const hasAssigned = !!page?.assigned_items.length
  const hasRecent = !!page?.recent_items.length
  const hasDirect = !!openDirect.length
  const hasCompletedDirect = !!completedDirect.length
  const empty = page !== null && !hasAssigned && !hasRecent && !hasDirect && !hasCompletedDirect
  const run = async (key: string, task: () => Promise<unknown>) => {
    if (busy) return false
    setBusy(key); setFailure("")
    try { await task(); await refresh(true); return true } catch (e) { setFailure(failureWords(e)); return false } finally { setBusy("") }
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
      await refresh(true)
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
          if (next && page !== null) void refresh()
        }}>
        <summary>
          <b>{workWord("todosTitle")}</b>
          <button className="session-todos-add" type="button" aria-label="新增 Session 待辦"
            onClick={(ev) => { ev.preventDefault(); ev.stopPropagation(); setAdding(true) }}><WorkIcon name="add" /></button>
          {page ? <TodoProgressSummary progress={todoProgress(page, row.sessionId)} />
            : !readFailure && <span id="session-todos-count">{L.strings.webLoading}</span>}
          {readFailure && <ReadFailure state={todoHeaderState(page !== null, true)} reason={readFailure.reason}
            retrying={reading} onRetry={() => { void refresh(true) }} />}
          {agentCount !== undefined ? <span className="session-todos-agent-count">
            {L.strings.webAgents} {agentCount === null ? "?" : agentCount}
          </span> : null}
        </summary>
        <div className="session-todos-body">
          {agentPanel}
          {readFailure && <p className="work-note" role="alert">{readFailure.words}</p>}
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

/**
 * The header's own word that the last read failed: "讀取失敗" with no page,
 * "更新失敗" beside a page kept from before. It is a button, and tapping it
 * asks again without opening or closing the fold; the reason is its title and
 * label, because a phone has no hover and the fold may be closed.
 */
function ReadFailure({ state, reason, retrying, onRetry }: {
  state: "failed" | "stale" | "loading" | "loaded"
  reason: string
  retrying: boolean
  onRetry: () => void
}) {
  const tip = nextWord("todosRetryTip", { reason })
  const words = retrying ? nextWord("todosRetrying") : state === "stale" ? nextWord("todosRefreshFailed") : nextWord("todosReadFailed")
  return <button className="session-todos-failed" id={state === "failed" ? "session-todos-count" : undefined} type="button"
    data-state={state} title={tip} aria-label={`${words}. ${tip}`} aria-busy={retrying} disabled={retrying}
    onClick={(ev) => { ev.preventDefault(); ev.stopPropagation(); onRetry() }}>
    <span aria-hidden="true">↻</span>{words}
  </button>
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
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  const send = todoSend(todo, now)
  // An unread delivery's Send comes back by itself when its window closes.
  useEffect(() => {
    if (send.kind !== "wait") return
    const timer = window.setTimeout(() => setNow(Math.floor(Date.now() / 1000)), Math.max(0, send.until - now) * 1000 + 250)
    return () => window.clearTimeout(timer)
  }, [send.kind, send.kind === "wait" ? send.until : 0, now])
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
    <div className="work-actions session-todo-actions">
      {send.kind !== "none" && <button className={`chip session-todo-send${send.kind === "send" ? " on" : ""}`} type="button" disabled={busy || send.kind === "wait"}
        title={send.kind === "wait" ? "剛傳送過；兩分鐘內 Session 沒讀取才能再送" : undefined}
        onClick={() => onAction("send")}><WorkIcon name="send" />{send.label}</button>}
      <button className="session-todo-delete" type="button" disabled={busy} aria-label="刪除待辦" title="刪除待辦"
        onClick={() => onAction("delete")}><WorkIcon name="delete" /></button>
    </div>
  </article>
}

function ReferenceImage({ image, compact = false }: { image: WorkV2Image; compact?: boolean }) {
  // The row reads the small copy, which is what says the picture is there;
  // the link opens the original, read on the press (`useReferenceImage`).
  const { source, failed, full, fullFailed, openFull } = useReferenceImage(image.id)
  if (compact) return source ? <a className="session-todo-image-link" href={full || source} target="_blank" rel="noreferrer"
    aria-label={`開啟參考圖片 ${image.title}`} title={image.title} onClick={openFull}>
    <span>{image.title}</span><span aria-hidden={fullFailed ? undefined : "true"} role={fullFailed ? "alert" : undefined}>{fullFailed || "↗"}</span>
  </a> : <div className="session-todo-image-link" data-state={failed ? "failed" : "loading"} role={failed ? "alert" : undefined}>
    <span title={image.title}>{image.title}</span><span>{failed || "載入中…"}</span>
  </div>
  return <figure className="work-reference-image">
    {source ? <a href={full || source} target="_blank" rel="noreferrer" aria-label={`開啟參考圖片 ${image.title}`} onClick={openFull}>
      <img src={source} alt={image.title} width={image.width} height={image.height} />
    </a> : <div className="work-reference-loading" role={failed ? "alert" : undefined}>{failed || "載入圖片…"}</div>}
    <figcaption title={image.title} role={fullFailed ? "alert" : undefined}>{fullFailed || image.title}</figcaption>
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

/**
 * The fold's count, as GitHub shows a milestone: one bar split by state, then
 * each state's count behind its mark. Finished is green, being worked on is
 * the console's amber, not yet started is the faint ring; a state with none is
 * left out of the words but the bar still spans all of them.
 */
function TodoProgressSummary({ progress }: { progress: TodoProgress }) {
  const total = progress.done + progress.active + progress.waiting
  const parts: { key: keyof TodoProgress; icon: "check" | "half" | "circle"; word: string }[] = [
    { key: "done", icon: "check", word: "完成" },
    { key: "active", icon: "half", word: "進行中" },
    { key: "waiting", icon: "circle", word: "未開始" },
  ]
  return <span className="session-todos-progress" id="session-todos-count" role="img" aria-label={todoProgressLabel(progress)}
    title={todoProgressLabel(progress)}>
    {total > 0 && <span className="session-todos-bar" aria-hidden="true">
      {parts.map((part) => progress[part.key] > 0 &&
        <span key={part.key} data-state={part.key} style={{ flexGrow: progress[part.key] }} />)}
    </span>}
    {total === 0 ? <span className="session-todos-none" aria-hidden="true">0</span>
      : parts.map((part) => progress[part.key] > 0 && <span key={part.key} className="session-todos-state" data-state={part.key}
        aria-hidden="true"><WorkIcon name={part.icon} />{progress[part.key]}<span className="session-todos-word">{part.word}</span></span>)}
  </span>
}
