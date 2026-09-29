import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react"
import { createPortal } from "react-dom"
import type { Assistant, SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { isPicture, prepareReferencePicture } from "../../legacy/shots-bridge.js"
import { sessionFragment } from "../../session/address.js"
import { Mark } from "../../session/List.js"
import { PersonaBot, PersonaTag, usePersonas } from "../../session/PersonaBot.js"
import { personaById, personaName, personaTitle, rememberTeam, rememberedTeam, shownTeam, suggestedPersonaForItem, switchTeam } from "../../personas.js"
import { RoleRow } from "../../session/RoleRow.js"
import { nextWord } from "../../next-strings.js"
import { workProjectID, workRouteFromHash } from "../../page-route.js"
import { failureWords, when } from "./shared.js"
import { onOpenNewWorkItem, onOpenWorkItem, type NewWorkItemDraft } from "./new-item.js"
import { WorkMilestones } from "./WorkMilestones.js"
import { WorkGateAttention, WorkGateDetail, WorkGateLine } from "./WorkGate.js"
import { gateSnapshotText } from "./gate-status.js"
import { WorkSteps } from "./WorkSteps.js"
import { WorkCompletionReports, WorkEpicPlanDocuments, WorkItemDocuments } from "./WorkCompletionReport.js"
import { EPIC_GATE_HINT, epicGate, epicGateDetailShown, epicGateShown, isEpic } from "./epic-gate.js"
import { epicChildren, epicParent, epicProgress, epicProgressWords, needsFamilyList, shortWorkID } from "./epic-family.js"
import { WorkIcon } from "./WorkIcon.js"
import { MAX_REFERENCE_PICTURES, markedFile, PendingPictures, PictureMarkup } from "./ReferencePictures.js"
import { useReferenceImage } from "./useReferenceImage.js"
import { VoiceTextarea } from "./VoiceTextarea.js"
import { ItemUsageCard } from "./TokenBill.js"
import { arrangeWorkItems, workItemPlaces } from "./board-order.js"
import { useBoardMotion } from "./board-motion.js"
import { completeConfirmWords } from "./complete-item.js"
import { decisionsForWorkItem, proposalsForProject } from "./board-attention.js"
import {
  answerDecision,
  assignNewWorkV2,
  assignWorkV2,
  addWorkV2Image,
  completeWorkV2,
  convertWorkV2,
  createWorkV2,
  deleteWorkV2,
  deleteWorkV2Image,
  editWorkV2,
  readDecisions,
  readProjectPlaces,
  readSessionWorkV2,
  readSessionsForWorkV2,
  readWorkV2,
  readWorkV2Item,
  readWorkV2Proposals,
  remindWorkV2,
  resolveWorkV2Proposal,
  suggestPersonaWorkV2,
  type Decision,
  type ProjectPlace,
  type WorkV2Item,
  type WorkV2ExecutableKind,
  type WorkV2Kind,
  type WorkV2Proposal,
  type WorkV2Page,
  type WorkV2Status,
  type SessionWorkV2,
} from "./api.js"
import {
  assignmentCandidates,
  assistantName,
  NEW_SESSION_ASSISTANTS,
  rememberAssistant,
  rememberedAssistant,
  sessionActivityName,
  sessionWorkCounts,
  sessionWorkStateName,
} from "./session-assignment.js"
import { workV2CreateDecision, type WorkV2CreateDecision } from "./create-decision.js"
import { claimedViaLine, createdViaLine, workWord } from "./words.js"
import { appendWorkPage } from "./work-pages.js"
import { visibleWorkItems } from "./plan-visibility.js"

const KINDS: WorkV2Kind[] = ["feature", "issue", "epic", "refactor", "plan"]
const EXECUTABLE_KINDS: WorkV2ExecutableKind[] = ["feature", "issue", "epic"]
const PHASES = ["assigning", "assigned", "implementing", "verifying", "merging", "deploying"]
const KIND_META: Record<WorkV2Kind, { icon: string; label: string; description: string }> = {
  feature: { icon: "✦", label: "Feature", description: "加入一項使用者可以感受到的新能力" },
  issue: { icon: "!", label: "Issue", description: "修正錯誤、異常或不符合預期的行為" },
  epic: { icon: "◆", label: "Epic", description: "可指派的大型工作；指派時若啟用規劃 gate，Session 要先寫計劃書並經 Child Session review" },
  refactor: { icon: "↻", label: "Refactor", description: "先放在規劃區的內部結構改善" },
  plan: { icon: "≡", label: "Plan", description: "先放在規劃區的研究或實作計畫" },
}

export function WorkV2Page({ shown }: { shown: boolean }) {
  const [items, setItems] = useState<WorkV2Item[]>([])
  const [places, setPlaces] = useState<ProjectPlace[]>([])
  const [sessions, setSessions] = useState<SessionRow[]>([])
  const [proposals, setProposals] = useState<WorkV2Proposal[]>([])
  const [decisions, setDecisions] = useState<Decision[]>([])
  const [project, setProject] = useState("")
  const [routeProject, setRouteProject] = useState(() => typeof location === "undefined" ? "" : workRouteFromHash(location.hash).project)
  const [status, setStatus] = useState<WorkV2Status>("open")
  const [showPlans, setShowPlans] = useState(false)
  const [searchInput, setSearchInput] = useState("")
  const [search, setSearch] = useState("")
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [family, setFamily] = useState<{ rows: WorkV2Item[]; truncated: boolean }>({ rows: [], truncated: false })
  const [loaded, setLoaded] = useState(false)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [paging, setPaging] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createDraft, setCreateDraft] = useState<NewWorkItemDraft>({})
  const [createdItem, setCreatedItem] = useState<WorkV2Item | null>(null)
  const [openedItem, setOpenedItem] = useState<WorkV2Item | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [busy, setBusy] = useState("")
  const [failure, setFailure] = useState("")
  const loadGeneration = useRef(0)
  const loadedView = useRef("")
  const board = useRef<HTMLElement>(null)
  // What is on screen keeps its place until the view is opened afresh, its
  // filter changes, or Refresh is pressed; see `board-order.ts`.
  const arrangement = useRef<{ view: string; places: Map<string, number> } | null>(null)
  const rearrange = useRef(true)
  useBoardMotion(board)

  const load = useCallback(async (announce = false) => {
    const generation = ++loadGeneration.current
    if (announce) setRefreshing(true)
    const requestedView = `${routeProject}\n${status}\n${search}`
    const replacing = loadedView.current !== requestedView
    if (replacing) {
      setLoading(true); setFailure(""); setItems([]); setFamily({ rows: [], truncated: false }); setNextCursor(null)
    }
    try {
      // Sessions can be the slowest inventory read. Start the independent
      // reads now; the Board waits only for the Project catalog it needs to
      // resolve the URL, never for those reads before it starts its own.
      const supportRead = Promise.all([readSessionsForWorkV2(), readWorkV2Proposals(), readDecisions()])
      // If the Project catalog fails first, these already-started reads still
      // have a rejection handler; awaiting the same promise below keeps their
      // real failure when the catalog succeeds.
      void supportRead.catch(() => {})
      const projects = await readProjectPlaces()
      const selectedProject = workProjectID(routeProject, projects.places)
      const [work, [live, suggestions, questions]] = await Promise.all([
        readWorkV2(selectedProject || undefined, status, search), supportRead,
      ])
      const relatives = await readEpicFamily(selectedProject, status, search, work)
      if (generation !== loadGeneration.current) return
      setProject(selectedProject)
      const view = `${selectedProject}\n${status}\n${search}`
      const kept = rearrange.current || arrangement.current?.view !== view ? null : arrangement.current.places
      rearrange.current = false
      const rows = arrangeWorkItems(work.rows, kept)
      arrangement.current = { view, places: workItemPlaces(rows) }
      setItems(rows); setNextCursor(work.next_cursor); setFamily(relatives); setLoaded(true); setLoading(false)
      loadedView.current = requestedView
      setPlaces(projects.places); setSessions(live.sessions); setProposals(suggestions.rows); setDecisions(questions.rows); setFailure("")
      setCreatedItem((current) => current ? (work.rows.find((item) => item.id === current.id) ?? current) : null)
      setOpenedItem((current) => {
        if (!current) return null
        const listed = work.rows.find((item) => item.id === current.id)
        return listed && listed.version !== current.version ? listed : current
      })
    } catch (e) {
      if (generation === loadGeneration.current) { setLoaded(true); setLoading(false); setFailure(failureWords(e)) }
    } finally {
      if (generation === loadGeneration.current) setRefreshing(false)
    }
  }, [routeProject, status, search])
  const loadMore = useCallback(async () => {
    if (!nextCursor || paging) return
    const generation = loadGeneration.current
    setPaging(true); setFailure("")
    try {
      const page = await readWorkV2(project || undefined, status, search, nextCursor)
      if (generation !== loadGeneration.current) return
      setItems((current) => {
        const merged = appendWorkPage(current, page.rows)
        const rows = arrangeWorkItems(merged, arrangement.current?.places ?? null)
        arrangement.current = { view: `${project}\n${status}\n${search}`, places: workItemPlaces(rows) }
        return rows
      })
      setFamily((current) => ({ rows: appendWorkPage(current.rows, page.rows), truncated: !!page.next_cursor }))
      setNextCursor(page.next_cursor)
    } catch (e) {
      if (generation === loadGeneration.current) setFailure(failureWords(e))
    } finally {
      if (generation === loadGeneration.current) setPaging(false)
    }
  }, [nextCursor, paging, project, status, search])
  const refreshPlaces = useCallback(async () => {
    try {
      const projects = await readProjectPlaces()
      setPlaces(projects.places)
      setFailure("")
    } catch (e) {
      setFailure(failureWords(e))
      throw e
    }
  }, [])
  useEffect(() => {
    if (!shown || typeof location === "undefined") return
    const syncRoute = () => setRouteProject(workRouteFromHash(location.hash).project)
    syncRoute()
    window.addEventListener("hashchange", syncRoute)
    return () => window.removeEventListener("hashchange", syncRoute)
  }, [shown])
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(searchInput.trim()), 220)
    return () => window.clearTimeout(timer)
  }, [searchInput])
  useEffect(() => { if (shown) rearrange.current = true }, [shown])
  useEffect(() => {
    if (!shown && !creating && !createdItem && !openedItem) return
    void load()
    const timer = setInterval(() => { if (document.visibilityState === "visible") void load() }, 30_000)
    return () => clearInterval(timer)
  }, [shown, creating, createdItem?.id, openedItem?.id, load])
  useEffect(() => onOpenNewWorkItem((draft) => {
    setFailure("")
    setCreatedItem(null)
    setCreateDraft(draft)
    setCreating(true)
  }), [])
  const refreshDetail = useCallback((id: string) => {
    setDetailLoading(true); setDetailError("")
    void readWorkV2Item(id).then((answer) => setOpenedItem((current) => current?.id === id ? answer.item : current))
      .catch((error: unknown) => setDetailError(failureWords(error)))
      .finally(() => setDetailLoading(false))
  }, [])
  const openItem = useCallback((item: WorkV2Item) => {
    setFailure("")
    setOpenedItem(item)
    refreshDetail(item.id)
  }, [refreshDetail])
  useEffect(() => onOpenWorkItem(openItem), [openItem])

  const run = async (key: string, task: () => Promise<unknown>, refreshInBackground = false) => {
    if (busy) return false
    setBusy(key); setFailure("")
    let succeeded = false
    try {
      const answer = await task()
      if (answer && typeof answer === "object" && "item" in answer) {
        const changed = (answer as { item?: WorkV2Item }).item
        if (changed) {
          setCreatedItem((current) => current?.id === changed.id ? changed : current)
          setOpenedItem((current) => current?.id === changed.id ? changed : current)
        }
      }
      // Assignment has already succeeded when its response arrives. The
      // supporting Board, Session, and image reads can finish after the person
      // closes this card; they must not keep its action pending.
      if (!refreshInBackground) await load()
      succeeded = true
      return true
    } catch (e) { setFailure(failureWords(e)); return false } finally {
      setBusy("")
      if (succeeded && refreshInBackground) void load()
    }
  }
  const visibleItems = visibleWorkItems(items, showPlans)
  const hiddenPlans = items.length - visibleItems.length
  const planning = visibleItems.filter((item) => item.area === "planning" && !item.closed_at)
  const unassigned = visibleItems.filter((item) => item.area === "unassigned" && !item.closed_at)
  const done = visibleItems.filter((item) => item.closed_at)
  const visibleProposals = proposalsForProject(proposals, project)
  const familyView = useMemo<EpicFamilyView>(() => ({ rows: family.rows, truncated: family.truncated, onBoard: new Set(items.map((item) => item.id)) }),
    [family, items])

  return <EpicFamilyContext.Provider value={familyView}>
  <section ref={board} id="work" className="page board-page work-page" data-page-view="work" hidden={!shown} aria-labelledby="work-v2-title"
    aria-busy={loading || refreshing || paging ? "true" : undefined}>
    <header className="board-head">
      <div><p className="board-eyebrow">WORK SYSTEM V2</p><h1 id="work-v2-title">看板</h1></div>
      <div className="work-head-tools">
        <button className="board-button" type="button" onClick={() => { setFailure(""); setCreatedItem(null); setCreateDraft({}); setCreating(true) }}>＋ 建立項目</button>
        <button className="board-button" type="button" disabled={!!busy || loading || refreshing || paging}
          aria-busy={refreshing ? "true" : undefined} aria-label={refreshing ? "正在重新整理看板" : undefined}
          onClick={() => { rearrange.current = true; void load(true) }}>{L.strings.webInfoRefresh}</button>
      </div>
    </header>
    <div className="work-wrap">
      <p className="work-lede">所有項目由你建立與指派；Session 負責推進實作、驗證、Merge 與部署。</p>
      <div className="work-filter-bar">
        <ProjectPicker places={places} value={project} onChange={(value) => setRouteProject(value)} onOpen={refreshPlaces} allowAll />
        <div className="work-filter-controls">
          <div className="work-status-filter" role="group" aria-label="篩選項目狀態">
            {([['open', '進行中'], ['done', '已完成'], ['all', '全部']] as [WorkV2Status, string][]).map(([value, label]) =>
              <button key={value} type="button" aria-pressed={status === value} onClick={() => setStatus(value)}>{label}</button>)}
          </div>
          <button className="work-plan-toggle" type="button" aria-pressed={showPlans}
            onClick={() => setShowPlans((current) => !current)}>顯示 Plan</button>
        </div>
        <label className="work-search">
          <WorkIcon name="search" />
          <input type="search" aria-label="搜尋標題與內容" placeholder="搜尋標題與內容" maxLength={1024} value={searchInput}
            onChange={(event) => setSearchInput(event.currentTarget.value)} />
          {searchInput && <button type="button" aria-label="清除搜尋" onClick={() => setSearchInput("")}><WorkIcon name="close" /></button>}
        </label>
      </div>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      {loading ? <BoardSkeleton /> : <>
      {visibleProposals.length > 0 && <ProposalQueue proposals={visibleProposals} items={items} places={places} busy={busy} run={run} />}
      <BoardRegion title="規劃區" items={planning} decisions={decisions} onOpen={openItem} />
      <BoardRegion title="待指派" items={unassigned} decisions={decisions} onOpen={openItem} />
      {PHASES.map((phase) => <BoardRegion key={phase} title={phaseName(phase)}
        items={visibleItems.filter((item) => item.area === phase && !item.closed_at)} decisions={decisions} onOpen={openItem} />)}
      {done.length > 0 && status === "done" && <BoardRegion title="已完成" items={done} decisions={decisions} onOpen={openItem} />}
      {done.length > 0 && status !== "done" && search && <BoardRegion title="已關閉" items={done} decisions={decisions} onOpen={openItem} />}
      {done.length > 0 && status !== "done" && !search && <details className="work-section work-done"><summary><div className="work-section-head"><h2>已關閉</h2><span className="work-count">{done.length}</span></div></summary>
        <div className="work-cards">{done.map((item) => <CompactWorkCard key={item.id} item={item}
          decisions={decisionsForWorkItem(decisions, item.id)} onOpen={() => openItem(item)} />)}</div>
      </details>}
      {loaded && !failure && visibleItems.length === 0 && <p className="work-empty work-filter-empty" role="status">
        {!showPlans && hiddenPlans > 0
          ? search ? "符合搜尋的 Plan 項目目前隱藏；開啟「顯示 Plan」即可查看。" : "這個範圍的 Plan 項目目前隱藏；開啟「顯示 Plan」即可查看。"
          : search ? `找不到包含「${search}」的項目。` : status === "done" ? "還沒有已完成的項目。" : "這個範圍目前沒有項目。"}
      </p>}
      {nextCursor && <div className="work-pagination">
        <button className="board-button work-more" type="button" disabled={paging} aria-busy={paging ? "true" : undefined}
          onClick={() => void loadMore()}>{paging ? "正在載入…" : "載入更多項目"}</button>
      </div>}
      </>}
    </div>
  </section>
    {creating && <NewWorkModal places={places} initialProject={project} initialDraft={createDraft} busy={!!busy} failure={failure}
      onRefreshPlaces={refreshPlaces} onClose={() => setCreating(false)} onCreate={(body, files, decisionKey) => {
      void run("create", async () => {
        const answer = await createWorkV2(body, decisionKey)
        let created = answer.item
        let version = answer.item.version
        try {
          for (let index = 0; index < files.length; index++) {
            const picture = await prepareReferencePicture(files[index])
            const uploaded = await addWorkV2Image(answer.item.id, version, picture, index)
            version = uploaded.item.version
            created = uploaded.item
          }
        } catch (error) {
          setCreating(false)
          setCreatedItem(created)
          await load()
          throw error
        }
        setCreating(false)
        setCreatedItem(created)
      })
    }} />}
    {createdItem && <CreatedWorkModal item={createdItem} sessions={sessions} decisions={decisionsForWorkItem(decisions, createdItem.id)} busy={busy} failure={failure}
      detailLoading={false} detailError="" retryDetail={() => void readWorkV2Item(createdItem.id).then((answer) => setCreatedItem(answer.item)).catch((error: unknown) => setFailure(failureWords(error)))}
      clearFailure={() => setFailure("")} run={run} onClose={() => setCreatedItem(null)} />}
    {openedItem && <CreatedWorkModal item={openedItem} created={false} sessions={sessions} decisions={decisionsForWorkItem(decisions, openedItem.id)} busy={busy} failure={failure}
      detailLoading={detailLoading} detailError={detailError} retryDetail={() => refreshDetail(openedItem.id)}
      clearFailure={() => setFailure("")} run={run} onClose={() => setOpenedItem(null)} />}
  </EpicFamilyContext.Provider>
}

/**
 * The list an Epic's children and a child's parent are read from. The Board's
 * own list leaves out closed items under 「進行中」 and anything a search does
 * not match, so when an Epic or a child is on screen the family is read again
 * with every status and no search. A failure here leaves the Board as it is
 * and the family drawn from what is on screen.
 */
async function readEpicFamily(project: string, status: WorkV2Status, search: string,
  work: WorkV2Page): Promise<{ rows: WorkV2Item[]; truncated: boolean }> {
  if (!needsFamilyList(work.rows)) return { rows: [], truncated: false }
  if (status === "all" && !search) return work
  try {
    return await readWorkV2(project || undefined, "all")
  } catch {
    return { rows: work.rows, truncated: true }
  }
}

/** The first page's lane and card geometry, using the Board's own surfaces. */
function BoardSkeleton() {
  return <div className="work-board-skeleton" role="status" aria-label={L.strings.webLoading}>
    {[0, 1, 2].map((lane) => <section className="work-skeleton-lane" aria-hidden="true" key={lane}>
      <span className="work-skeleton-line work-skeleton-heading" />
      <div className="work-cards">
        {[0, 1].map((card) => <div className="work-card work-skeleton-card" key={card}>
          <span className="work-skeleton-line work-skeleton-meta" />
          <span className="work-skeleton-line work-skeleton-title" />
          <span className="work-skeleton-line" />
          <span className="work-skeleton-line work-skeleton-short" />
        </div>)}
      </div>
    </section>)}
  </div>
}

interface EpicFamilyView {
  rows: WorkV2Item[]
  truncated: boolean
  /** Items that have a card on the Board, which a family link can bring into view. */
  onBoard: Set<string>
}

const EpicFamilyContext = createContext<EpicFamilyView>({ rows: [], truncated: false, onBoard: new Set() })

/**
 * Brings another card on the Board into view and moves focus to it, opening
 * the folded 已關閉 section when the card is inside it. Only the Board's own
 * cards are searched; a modal's copy of a card is not a place to go to.
 */
function showWorkCard(id: string) {
  const card = document.querySelector<HTMLElement>(`#work [data-work-id="${CSS.escape(id)}"]`)
  if (!card) return
  for (let fold = card.closest("details"); fold; fold = fold.parentElement?.closest("details") ?? null) fold.open = true
  const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches
  card.scrollIntoView({ block: "center", behavior: still ? "auto" : "smooth" })
  card.focus({ preventScroll: true })
  card.setAttribute("data-arrived", "")
  window.setTimeout(() => card.removeAttribute("data-arrived"), 1600)
}

/** A link to another card on the Board, or its words alone when that card is not shown. */
function WorkCardLink({ id, onBoard, className, children }: { id: string; onBoard: boolean; className: string; children: ReactNode }) {
  return onBoard
    ? <button className={className} type="button" onClick={() => showWorkCard(id)}>{children}</button>
    : <span className={className}>{children}</span>
}

/** On an Epic: the items created under it, how far they have got, and who has each. */
function EpicChildren({ item, sessions }: { item: WorkV2Item; sessions: SessionRow[] }) {
  const family = useContext(EpicFamilyContext)
  const personas = usePersonas()
  const children = epicChildren(family.rows, item.id)
  if (!children.length) return null
  const progress = epicProgress(children)
  const complete = progress.total > 0 && progress.done === progress.total
  return <section className="work-epic-children" aria-label="子項目" data-complete={complete ? "" : undefined}>
    <div className="work-epic-children-head">
      <strong>子項目</strong>
      <span>{epicProgressWords(progress)}{family.truncated && " · 清單不完整"}</span>
    </div>
    {progress.total > 0 && <div className="work-epic-progress" role="progressbar" aria-label="子項目完成度"
      aria-valuemin={0} aria-valuemax={progress.total} aria-valuenow={progress.done}>
      <i style={{ width: `${(progress.done / progress.total) * 100}%` }} />
    </div>}
    <ul>{children.map((child) => {
      const owner = child.owner_session ? sessions.find((session) => session.sessionId === child.owner_session) : undefined
      const who = owner ? (owner.label || owner.id) : child.owner_session ? `Session ${child.owner_session.slice(0, 8)}` : "未指派"
      const ownerPersona = personaById(personas, owner?.persona)
      return <li key={child.id} data-phase={child.phase}>
        <WorkCardLink id={child.id} onBoard={family.onBoard.has(child.id)} className="work-epic-child">
          <span className="work-epic-child-kind" aria-label={KIND_META[child.kind].label} title={KIND_META[child.kind].label}>{KIND_META[child.kind].icon}</span>
          <span className="work-epic-child-title">{child.title}</span>
          <span className="work-epic-child-meta">{phaseName(child.phase)} · {ownerPersona
            ? <span className="work-owner-persona" title={personaTitle(ownerPersona)}><PersonaBot persona={ownerPersona} cellPx={2} className="persona-tag-bot" />{who}</span>
            : who}</span>
        </WorkCardLink>
      </li>
    })}</ul>
  </section>
}

/** On a child: the Epic it was created under. */
function EpicParentLine({ item }: { item: WorkV2Item }) {
  const family = useContext(EpicFamilyContext)
  const parent = epicParent(item, family.rows)
  if (!parent) return null
  return <p className="work-epic-parent">屬於 Epic：<WorkCardLink id={parent.id} onBoard={family.onBoard.has(parent.id)} className="work-epic-parent-link">
    {parent.title ? `〈${parent.title}〉` : <code>{shortWorkID(parent.id)}</code>}</WorkCardLink></p>
}

/**
 * Proposals are an inbox, not another Board column. The row says the proposed
 * outcome and why it matters; the longer scope and acceptance stay one
 * disclosure away instead of making every suggestion as tall as a work card.
 */
function ProposalQueue({ proposals, items, places, busy, run }: {
  proposals: WorkV2Proposal[]
  items: WorkV2Item[]
  places: ProjectPlace[]
  busy: string
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  return <details className="work-fold work-proposal-fold" open>
    <summary><strong>Agent 提案</strong><span className="work-count">{proposals.length}</span>
      <span className="work-fold-hint">先用白話說清楚，需要時再 Explain</span></summary>
    <div className="work-fold-body"><ul className="work-proposals">{proposals.map((proposal) => {
      const project = places.find((place) => place.id === proposal.project_id)
      const source = proposal.source_work_id ? items.find((item) => item.id === proposal.source_work_id) : undefined
      const sourceLine = source ? `從「${source.title}」延伸`
        : proposal.source_work_id ? `來自看板項目 #${proposal.source_work_id.slice(0, 8)}`
          : proposal.source_todo_id ? "來自 Session 待辦" : "來源資料不完整"
      return <li key={proposal.id} className="work-proposal" data-proposal-id={proposal.id}>
        <div className="work-proposal-main">
          <span className="work-state">建議建立 {KIND_META[proposal.kind].label}</span>
          <h3>{proposal.title}</h3>
          <p className="work-proposal-context">{project?.label ?? proposal.project_id} · {sourceLine}</p>
          <p className="work-proposal-reason"><strong>為什麼要做</strong><span>{proposal.reason}</span></p>
          <details className="work-proposal-detail">
            <summary><span lang="en">Explain</span><span>詳細說明</span></summary>
            <dl><div><dt>會改什麼</dt><dd>{proposal.description}</dd></div>
              <div><dt>完成後會看到什麼</dt><dd>{proposal.suggested_acceptance || "這筆舊提案沒有記下可觀察的完成結果。"}</dd></div></dl>
          </details>
        </div>
        <div className="work-actions work-proposal-actions" aria-label={`處理提案「${proposal.title}」`}>
          <button className="chip on" type="button" disabled={!!busy}
            onClick={() => void run(proposal.id, () => resolveWorkV2Proposal(proposal.id, "accept"))}>接受並建立</button>
          <button className="chip danger" type="button" disabled={!!busy}
            onClick={() => void run(proposal.id, () => resolveWorkV2Proposal(proposal.id, "reject"))}>拒絕</button>
        </div>
      </li>
    })}</ul></div>
  </details>
}

/** A person's answer is part of its item, immediately after the item's scope. */
function WorkItemDecisions({ decisions, busy, run }: {
  decisions: Decision[]
  busy: boolean
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  if (!decisions.length) return null
  return <section className="work-item-decisions" aria-label="這個項目需要你回答的問題">
    <div className="work-item-decisions-head"><strong>需要你決定</strong><span>{decisions.length}</span></div>
    {decisions.map((decision) => {
      const fallback = decision.options.find((option) => option.id === decision.default)?.label ?? decision.default
      return <div className="work-item-decision" key={decision.id} data-decision-id={decision.id}>
        <p className="work-item-decision-state">{decision.blocking ? "回答前，這個項目的工作暫停" : "這個問題不會暫停工作"}</p>
        <h4>{decision.question}</h4>
        <p className="work-clock">到 {when(decision.due_at)} 還沒回答，就採用「{fallback}」</p>
        <div className="work-actions" role="group" aria-label={`回答「${decision.question}」`}>
          {decision.options.map((option) => <button key={option.id} type="button"
            className={option.id === decision.default ? "chip on" : "chip"} disabled={busy}
            onClick={() => void run(decision.id, () => answerDecision(decision.id, option.id))}>{option.label}</button>)}
        </div>
      </div>
    })}
  </section>
}

function BoardRegion({ title, items, decisions, onOpen }: {
  title: string
  items: WorkV2Item[]
  decisions: Decision[]
  onOpen: (item: WorkV2Item) => void
}) {
  if (!items.length) return null
  return <section className="work-section"><div className="work-section-head"><h2>{title}</h2><span className="work-count">{items.length}</span></div>
    <div className="work-cards">{items.map((item) => <CompactWorkCard key={item.id} item={item}
      decisions={decisionsForWorkItem(decisions, item.id)} onOpen={() => onOpen(item)} />)}</div>
  </section>
}

/**
 * The Board is an index: enough context to choose an item, never the whole
 * item's working surface. The one button avoids nested controls and gives a
 * keyboard and screen-reader user the same route into the shared detail modal.
 */
function CompactWorkCard({ item, decisions, onOpen }: {
  item: WorkV2Item
  decisions: Decision[]
  onOpen: () => void
}) {
  const epic = isEpic(item)
  const gateShown = epicGateDetailShown(item)
  const attention = decisions.length > 0 || !!item.user_action
  const gateDescriptionID = `work-card-${item.id}-gate`
  const gateSnapshotDescriptionID = `work-card-${item.id}-gate-snapshot`
  const attentionDescriptionID = `work-card-${item.id}-attention`
  const describedBy = [gateShown && gateSnapshotDescriptionID, gateShown && gateDescriptionID,
    attention && attentionDescriptionID].filter(Boolean).join(" ") || undefined
  return <article className={epic ? "work-card work-v2-card work-summary-card work-epic-card" : "work-card work-v2-card work-summary-card"}
    data-work-id={item.id} data-phase={item.phase} data-kind={item.kind} tabIndex={-1}>
    <button className="work-card-summary" type="button" aria-haspopup="dialog"
      aria-label={`查看「${item.title}」的完整內容，${phaseName(item.phase)}`} aria-describedby={describedBy} onClick={onOpen}>
      <span className="work-card-summary-top">
        <span className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span title={item.project.label}>{item.project.label}</span></span>
        <span className={epic ? "work-state work-epic-label" : "work-state"}>{epic ? "EPIC · " : `${item.kind} · `}{phaseName(item.phase)}</span>
      </span>
      <span className="work-card-summary-title">{item.title}</span>
      <span className="work-card-summary-description">{item.description}</span>
      {gateShown && <><WorkGateLine item={item} id={gateDescriptionID} />
        <span id={gateSnapshotDescriptionID} className="work-gate-snapshot">本輪：{gateSnapshotText(item.gate_snapshot_cycle, item.planning_gate, item.verify_gate)}</span></>}
      <span className="work-card-summary-foot">
        <span>{item.closed_at ? `完成 ${when(item.closed_at)}` : `更新 ${when(item.updated_at)}`}</span>
        {attention && <span id={attentionDescriptionID} className="work-card-attention">需要你處理{decisions.length > 1 ? ` · ${decisions.length} 個問題` : ""}</span>}
        <span className="work-card-open">查看完整內容 <WorkIcon name="open" /></span>
      </span>
    </button>
  </article>
}

function WorkCard({ item, sessions, decisions, busy, failure, clearFailure, run, detailLoading, detailError, retryDetail, focusAssignment = false, reportsExpanded = false, foldDescription = false }: {
  item: WorkV2Item
  sessions: SessionRow[]
  decisions: Decision[]
  busy: string
  failure: string
  clearFailure: () => void
  run: (key: string, task: () => Promise<unknown>, refreshInBackground?: boolean) => Promise<boolean>
  detailLoading: boolean
  detailError: string
  retryDetail: () => void
  focusAssignment?: boolean
  reportsExpanded?: boolean
  foldDescription?: boolean
}) {
  const [terminal, setTerminal] = useState("")
  const [assistant, setAssistant] = useState<Assistant>(() => rememberedAssistant())
  // The role a new Session is opened as. Until the person picks one, the
  // item's words suggest a role when they distinguish one; otherwise the
  // unique kind default remains (epic → architect, issue → minimal-change).
  // "" is none, explicitly chosen by the person.
  const personas = usePersonas()
  const [personaChoice, setPersonaChoice] = useState<string | null>(null)
  const [team, setTeam] = useState(rememberedTeam)
  const personaSuggestion = suggestedPersonaForItem(personas, item)
  const persona = personaById(personas, personaChoice ?? personaSuggestion?.persona.id)
  const [aiSuggestion, setAISuggestion] = useState<Awaited<ReturnType<typeof suggestPersonaWorkV2>> | null>(null)
  const [aiSuggestionBusy, setAISuggestionBusy] = useState(false)
  const [aiSuggestionFailure, setAISuggestionFailure] = useState("")
  const itemVersion = useRef(item.version)
  itemVersion.current = item.version
  const aiPersona = aiSuggestion?.outcome === "recommend" ? personaById(personas, aiSuggestion.persona_id) : null
  const aiSuggestionID = `work-persona-ai-${item.id}`
  const aiSuggestionOverridden = !!aiPersona && personaChoice !== null && personaChoice !== aiPersona.id
  useEffect(() => {
    setAISuggestion(null)
    setAISuggestionFailure("")
    setAISuggestionBusy(false)
  }, [item.version])
  const askAIForPersona = async () => {
    if (aiSuggestionBusy) return
    const askedVersion = item.version
    setAISuggestionBusy(true)
    setAISuggestionFailure("")
    try {
      const answer = await suggestPersonaWorkV2(item)
      if (itemVersion.current !== askedVersion) return
      if (answer.outcome === "recommend") {
        const picked = personaById(personas, answer.persona_id)
        if (!picked) {
          setAISuggestionFailure("AI 回傳的角色不在目前清單中，因此沒有變更選擇。")
          return
        }
        const nextTeam = shownTeam(personas, picked.id, team)
        setPersonaChoice(picked.id)
        setTeam(nextTeam)
        rememberTeam(nextTeam)
      }
      setAISuggestion(answer)
    } catch (error) {
      if (itemVersion.current === askedVersion) setAISuggestionFailure(personaAIError(error))
    } finally {
      if (itemVersion.current === askedVersion) setAISuggestionBusy(false)
    }
  }
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [completing, setCompleting] = useState(false)
  const [converting, setConverting] = useState(false)
  const [conversionKind, setConversionKind] = useState<WorkV2ExecutableKind>("feature")
  const [reminded, setReminded] = useState(false)
  // Moving an owned item to another Session opens the same picker an
  // unassigned card shows, without its owner among the choices.
  const [reassigning, setReassigning] = useState(false)
  // An assignment that failed says so on this card: opening a new Session can
  // take a minute and a half, and the page-level note it used to land in is
  // scrolled out of sight on a phone, where the press looked like nothing.
  const [assignFailed, setAssignFailed] = useState(false)
  const [assigningRoute, setAssigningRoute] = useState<"existing" | "new" | null>(null)
  const assign = (route: "existing" | "new", task: () => Promise<unknown>) => {
    clearFailure(); setAssignFailed(false)
    setAssigningRoute(route)
    void run(item.id, task, true).then((ok) => {
      setAssigningRoute(null)
      setAssignFailed(!ok)
      if (ok) setReassigning(false)
    })
  }
  const imagePicker = useRef<HTMLInputElement>(null)
  const eligible = useMemo(() => assignmentCandidates(sessions, item), [sessions, item.project.path, item.owner_session])
  const owner = item.owner_session ? sessions.find((session) => session.sessionId === item.owner_session) : undefined
  const assignable = item.area !== "planning" && !item.closed_at && !item.owner_session
  const reassignable = item.area !== "planning" && !item.closed_at && !!item.owner_session
  const epic = isEpic(item)
  const plan = item.kind === "plan"
  const convertible = !item.closed_at && (item.phase === "created" || item.phase === "assigned") && !item.parent_id &&
    (plan || item.kind === "epic" || item.kind === "feature")
  const cardClass = epic ? "work-card work-v2-card work-epic-card"
    : plan ? "work-card work-v2-card work-plan-card" : "work-card work-v2-card"
  const conversionPanel = converting && convertible ? <section id={`work-convert-${item.id}`} className="work-plan-convert"
    aria-label={plan ? "把 Plan 轉成可執行項目" : `把 ${KIND_META[item.kind].label} 轉成 Plan`}>
    <div><strong>{plan ? "準備執行這份 Plan" : "移到規劃區"}</strong>
      <p>{plan ? "轉換後會移到「待指派」" : "轉換後會移到「規劃區」"}；若已指派，會解除目前指派。標題、描述、圖片、步驟與歷史紀錄都會保留。</p></div>
    {plan && <fieldset className="work-plan-kind-field"><legend>轉換後的項目類型</legend>
      <div className="work-plan-kind-list">
        {EXECUTABLE_KINDS.map((kind) => <label key={kind} className="work-plan-kind-option">
          <input type="radio" name={`plan-conversion-${item.id}`} value={kind} checked={conversionKind === kind}
            disabled={!!busy} onChange={() => setConversionKind(kind)} />
          <span aria-hidden="true">{KIND_META[kind].icon}</span><b>{KIND_META[kind].label}</b>
        </label>)}
      </div>
    </fieldset>}
    <div className="work-actions">
      <button className="chip on" type="button" disabled={!!busy} aria-busy={busy === `convert-${item.id}`}
        onClick={() => { void run(`convert-${item.id}`, () => convertWorkV2(item, plan ? conversionKind : "plan")).then((ok) => { if (ok) setConverting(false) }) }}>
        {busy === `convert-${item.id}` ? "轉換中…" : `確認轉成 ${plan ? KIND_META[conversionKind].label : "Plan"}`}</button>
      <button className="chip" type="button" disabled={!!busy} onClick={() => setConverting(false)}>取消</button>
    </div>
    {failure && <p className="work-note" role="alert">轉換失敗：{failure}</p>}
  </section> : null
  return <article className={cardClass} data-work-id={item.id}
    data-phase={item.phase} data-kind={item.kind} tabIndex={-1}>
    <div className="work-card-toolbar">
      <div className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span title={item.project.label}>{item.project.label}</span></div>
      <div className="work-card-controls" aria-label="項目操作">
        {!!item.owner_session && !item.closed_at && <button type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setReminded(false); void run(`remind-${item.id}`, () => remindWorkV2(item)).then(setReminded) }}>
          <WorkIcon name={reminded ? "check" : "remind"} /> {reminded ? "已提醒" : "提醒 Session"}
        </button>}
        {reassignable && <button type="button" disabled={!!busy} aria-expanded={reassigning}
          onClick={() => { clearFailure(); setAssignFailed(false); setTerminal(""); setReassigning((shown) => !shown) }}>
          <WorkIcon name="reassign" /> 改派</button>}
        {!item.closed_at && <button type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setCompleting(true) }}><WorkIcon name="check" /> 完成</button>}
        <button type="button" disabled={!!busy} onClick={() => { clearFailure(); setEditing(true) }}><WorkIcon name="edit" /> 編輯</button>
        {convertible && !plan && <button type="button" disabled={!!busy} aria-expanded={converting}
          aria-controls={`work-convert-${item.id}`}
          onClick={() => { clearFailure(); setConverting((shown) => !shown) }}>轉成 Plan</button>}
        {!item.closed_at && <button className="danger" type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setDeleting(true) }}><WorkIcon name="delete" /> 刪除</button>}
      </div>
    </div>
    {/* The person's override closes the item without the owning Session's
        evidence, so it asks once more, here on the card, before it does. */}
    {completing && !item.closed_at && <div className="work-actions" role="group" aria-label="確認標記完成">
      <p className="work-note">{completeConfirmWords(item)}</p>
      <button className="chip on" type="button" disabled={!!busy} aria-busy={busy === `complete-${item.id}`}
        onClick={() => { void run(`complete-${item.id}`, () => completeWorkV2(item)).then((ok) => { if (ok) setCompleting(false) }) }}>
        <WorkIcon name="check" />{busy === `complete-${item.id}` ? "標記中…" : "確認標記完成"}</button>
      <button className="chip" type="button" disabled={busy === `complete-${item.id}`} onClick={() => setCompleting(false)}>取消</button>
      {failure && <p className="work-note" role="alert">標記完成失敗：{failure}</p>}
    </div>}
    {epic
      ? <span className="work-state work-epic-label"><b>EPIC · 大型項目</b> · {phaseName(item.phase)}</span>
      : plan ? <span className="work-state work-plan-label"><b>PLAN · 未排入執行</b></span>
      : <span className="work-state">{item.kind} · {phaseName(item.phase)}</span>}
    <h3 id={`work-card-title-${item.id}`}>{item.title}</h3>
    <EpicParentLine item={item} />
    <CreatedViaNote item={item} />
    <ClaimedViaNote item={item} />
    {!plan && conversionPanel}
    {foldDescription ? <WorkDescription key={item.id} description={item.description} id={item.id} /> : <p>{item.description}</p>}
    {convertible && plan && <div className="work-convert-entry">
      <button className="work-convert-cta" type="button" disabled={!!busy} aria-expanded={converting}
        aria-controls={`work-convert-${item.id}`}
        onClick={() => { clearFailure(); setConverting((shown) => !shown) }}>轉成可執行項目</button>
    </div>}
    {plan && conversionPanel}
    {epicGateDetailShown(item) && <WorkGateDetail item={item} loading={detailLoading} error={detailError}
      sessions={sessions.filter((session) => !!session.sessionId).map((session) => ({ id: session.sessionId || "", label: session.label || session.sessionId || "Session" }))}
      run={run} retry={retryDetail} />}
    {!epic && <WorkGateAttention item={item}
      sessions={sessions.filter((session) => !!session.sessionId).map((session) => ({ id: session.sessionId || "", label: session.label || session.sessionId || "Session" }))}
      run={run} />}
    <WorkItemDecisions decisions={decisions} busy={!!busy} run={run} />
    {epicGateShown(item) && <EpicGateChecklist item={item} />}
    {epic && <EpicChildren item={item} sessions={sessions} />}
    {item.user_action && <section className="work-user-action" aria-label="需要你做的事">
      <strong>需要你做的事</strong><p>{item.user_action}</p>
    </section>}
    {/* Choosing who does the work is what an unassigned card is for, so the
        picker sits under what the work is, above its progress and pictures. */}
    {(assignable || (reassignable && reassigning)) && <div className="work-assignment">
      {epic && <p className="work-epic-assign-note">若指派時規劃 gate 開啟，Session 須先寫計劃書並請 Child Session review，通過後才開始實作；關閉時可略過。</p>}
      {reassignable && <p className="work-reassign-note">改派給其他 Session：目前的 phase、steps 與文件都會保留，新 Session
        會被告知從哪裡接手；原本的 Session 會收到停止通知。</p>}
      <section className="work-assignment-route" aria-labelledby={`work-assign-existing-${item.id}`}>
        <h4 id={`work-assign-existing-${item.id}`}>指派給既有 Session</h4>
        <SessionAssignmentPicker sessions={eligible} value={terminal} onChange={setTerminal} autoFocus={focusAssignment || (epic && reassigning)} />
        <button className="work-assignment-cta" type="button" disabled={!terminal || !!busy} aria-busy={busy === item.id && assigningRoute === "existing"}
          onClick={() => assign("existing", () => assignWorkV2(item, terminal))}>
          {busy === item.id && assigningRoute === "existing" && <span className="work-assignment-spinner" aria-hidden="true" />}
          {busy === item.id && assigningRoute === "existing" ? "正在指派給所選 Session…" : "指派給所選 Session"}</button>
      </section>
      <section className="work-assignment-route" aria-labelledby={`work-assign-new-${item.id}`}>
        <h4 id={`work-assign-new-${item.id}`}>開啟新 Session</h4>
        <div className="work-new-session" role="radiogroup" aria-label="新 Session 使用的助理">
        {/* The product mark alone: the button beside it already spells out the
            chosen assistant, so the name is kept for the label and tooltip. */}
        {NEW_SESSION_ASSISTANTS.map((choice) => <button key={choice} className={`chip${choice === assistant ? " on" : ""}`} type="button"
          role="radio" aria-checked={choice === assistant} disabled={!!busy || aiSuggestionBusy}
          aria-label={assistantName(choice)} title={assistantName(choice)}
          onClick={() => { setAssistant(choice); rememberAssistant(choice) }}
          dangerouslySetInnerHTML={{ __html: L.assistantLogoHTML(choice) }} />)}
        </div>
        {personas.length > 0 && <button className="chip work-persona-ai-button" type="button" disabled={!!busy || aiSuggestionBusy}
          aria-busy={aiSuggestionBusy} aria-describedby={aiSuggestion ? aiSuggestionID : undefined}
          onClick={() => void askAIForPersona()}>{aiSuggestionBusy ? "AI 建議中…" : "AI 建議"}</button>}
        {aiSuggestion?.outcome === "recommend" && aiPersona && <p className="work-persona-ai-result" id={aiSuggestionID} role="status">
          <strong>AI 建議：{personaName(aiPersona)}</strong>
          <span>{aiSuggestionOverridden ? "目前已改選其他角色" : "已預先選取，仍可手動改選"}</span>
        </p>}
        {aiSuggestion?.outcome === "ambiguous" && <p className="work-persona-ai-result" id={aiSuggestionID} role="status">
          <strong>AI 無法可靠判斷</strong><span>保留目前的角色選擇，請手動決定。</span>
        </p>}
        {aiSuggestionFailure && <p className="work-note" role="alert">{aiSuggestionFailure}</p>}
        {personas.length > 0 && <RoleRow className="work-new-session work-new-persona" personas={personas} chosen={persona?.id ?? ""}
          team={team} disabled={!!busy || aiSuggestionBusy} press="radio" describedBy={aiSuggestion ? aiSuggestionID : undefined}
          onPick={(id) => setPersonaChoice(id)}
          onTeam={(next) => {
            const switched = switchTeam(personas, persona?.id, next)
            setTeam(next)
            rememberTeam(next)
            setPersonaChoice(switched.chosen)
          }} />}
        <button className="work-assignment-cta" type="button" disabled={!!busy || aiSuggestionBusy} aria-busy={busy === item.id && assigningRoute === "new"}
          onClick={() => assign("new", () => assignNewWorkV2(item, assistant, persona?.id))}>
          {busy === item.id && assigningRoute === "new" && <span className="work-assignment-spinner" aria-hidden="true" />}
          {persona
            ? nextWord(busy === item.id && assigningRoute === "new" ? "personaOpeningSession" : "personaNewSession", { assistant: assistantName(assistant), persona: personaName(persona) })
            : busy === item.id && assigningRoute === "new" ? `正在開啟 ${assistantName(assistant)} Session…` : `開新 ${assistantName(assistant)} Session`}</button>
      </section>
      {busy === item.id && <p className="work-assignment-status" role="status">正在處理指派；你可以關閉視窗，完成後看板會更新。</p>}
      {reassignable && <button className="chip" type="button" disabled={!!busy} onClick={() => setReassigning(false)}>取消</button>}
      {assignFailed && failure && <p className="work-note" role="alert">{failure}</p>}
    </div>}
    <WorkEpicPlanDocuments item={item} />
    <WorkItemDocuments item={item} placement="before_steps" />
    <WorkSteps steps={item.steps} />
    <WorkMilestones phase={item.phase} />
    <WorkItemDocuments item={item} placement="after_steps" />
    <WorkCompletionReports item={item} expanded={reportsExpanded} />
    {!!item.images?.length && <div className="work-reference-images" role="group" aria-label="參考圖片">
      {item.images.map((image) => <WorkReferenceImage key={image.id} item={item} image={image} busy={busy} run={run} />)}
    </div>}
    {!item.closed_at && <div className="work-reference-tools">
      <input ref={imagePicker} type="file" accept="image/*,.heic,.heif" multiple hidden onChange={(event) => {
        const files = Array.from(event.currentTarget.files ?? []).filter(isPicture)
        event.currentTarget.value = ""
        if (!files.length) return
        void run(`image-add-${item.id}`, async () => {
          if ((item.images?.length ?? 0) + files.length > MAX_REFERENCE_PICTURES) {
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
    <ItemUsageCard itemId={item.id} version={item.version} />
    <div className="work-meta"><span>{item.project.available ? (item.condition || "正常") : "project_unavailable"}</span>
      <span>{item.closed_at ? `完成 ${when(item.closed_at)}` : `更新 ${when(item.updated_at)}`}</span>
      {owner ? <a className="work-session-link" href={sessionFragment(owner.id)}
        aria-label={`前往正在實作「${item.title}」的 Session`}>前往 Session · {owner.label || owner.id}<WorkIcon name="open" /></a>
        : item.owner_session && <span>Session {item.owner_session.slice(0, 8)}</span>}
    </div>
    {editing && <EditWorkModal item={item} busy={!!busy} failure={failure} onClose={() => setEditing(false)} onSave={(title, description) => {
      void run(`edit-${item.id}`, () => editWorkV2(item, title, description)).then((ok) => { if (ok) setEditing(false) })
    }} />}
    {deleting && <DeleteWorkModal item={item} busy={!!busy} failure={failure} onClose={() => setDeleting(false)} onDelete={() => {
      void run(`delete-${item.id}`, () => deleteWorkV2(item)).then((ok) => { if (ok) setDeleting(false) })
    }} />}
  </article>
}

function WorkDescription({ description, id }: { description: string; id: string }) {
  const paragraph = useRef<HTMLParagraphElement>(null)
  const [long, setLong] = useState(false)
  const [expanded, setExpanded] = useState(false)
  useLayoutEffect(() => {
    const node = paragraph.current
    if (!node) return
    const measure = () => setLong(node.scrollHeight > 6 * parseFloat(getComputedStyle(node).lineHeight) + 1)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [description])
  return <>
    <p ref={paragraph} id={`work-description-${id}`} className="work-card-description" data-expanded={expanded}>{description}</p>
    {long && <button className="work-description-toggle" type="button" aria-expanded={expanded}
      aria-controls={`work-description-${id}`} onClick={() => setExpanded((shown) => !shown)}>
      {expanded ? "收合描述" : "顯示完整描述"}
    </button>}
  </>
}

function personaAIError(error: unknown): string {
  if (error instanceof RefusalError) {
    switch (error.code) {
      case "ai_consent_required": return "這台機器仍使用需要另行設定的舊版 AI 建議；請先更新 Clawdline。"
      case "no_persona_suggester": return "這台機器沒有可用的 Codex，因此沒有變更角色。"
      case "persona_suggester_out_of_quota": return "Codex 目前沒有可用額度，因此沒有變更角色。"
      case "persona_suggestion_failed": return "AI 沒有回傳可用的角色，因此保留目前選擇。"
      case "busy": return "這台機器正在處理其他 AI 工作，請稍後再按一次。"
      case "version_conflict": return "項目內容已更新；請確認最新內容後再用 AI 判斷。"
    }
  }
  return failureWords(error)
}

/**
 * Who asked for this item, when a Session created it on the person's message:
 * one small line on the card, the excerpt as its tooltip, and the message
 * quoted when the line is opened. A person's own item shows nothing here.
 */
function CreatedViaNote({ item }: { item: WorkV2Item }) {
  const line = createdViaLine(item.created_via)
  if (!line) return null
  const excerpt = item.created_via?.excerpt ?? ""
  if (!excerpt) return <p className="work-created-via">{line}</p>
  return <details className="work-created-via">
    <summary title={excerpt}>{line}</summary>
    <blockquote aria-label={workWord("createdViaQuote")}>{excerpt}</blockquote>
  </details>
}

/**
 * Who took this item, when its Session claimed it on the person's message:
 * the same small line and quote as CreatedViaNote. An item the person
 * assigned shows nothing here.
 */
function ClaimedViaNote({ item }: { item: WorkV2Item }) {
  const line = claimedViaLine(item.claimed_via)
  if (!line) return null
  const excerpt = item.claimed_via?.excerpt ?? ""
  if (!excerpt) return <p className="work-created-via">{line}</p>
  return <details className="work-created-via">
    <summary title={excerpt}>{line}</summary>
    <blockquote aria-label={workWord("createdViaQuote")}>{excerpt}</blockquote>
  </details>
}

/** Where an Epic stands against the plan gate before it may start implementing. */
function EpicGateChecklist({ item }: { item: WorkV2Item }) {
  const gate = epicGate(item.documents)
  return <section className="work-epic-gate" aria-label="Epic 實作前檢查" data-ready={gate.ready ? "" : undefined}>
    <ul>
      <li data-state={gate.plan ? "done" : "open"}><WorkIcon name={gate.plan ? "check" : "circle"} />計劃書</li>
      <li data-state={gate.review ? "done" : "open"}><WorkIcon name={gate.review ? "check" : "circle"} />Child Review</li>
    </ul>
    {!gate.ready && <p>{EPIC_GATE_HINT}</p>}
  </section>
}

interface SessionWorkReading {
  page?: SessionWorkV2
  loading?: boolean
  error?: string
}

function SessionAssignmentPicker({ sessions, value, onChange, autoFocus = false }: {
  sessions: SessionRow[]
  value: string
  onChange: (id: string) => void
  autoFocus?: boolean
}) {
  const [open, setOpen] = useState(false)
  const personas = usePersonas()
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
    {/* The menu hangs from the trigger alone: anchored to the whole picker, it
        opened under the selected Session's detail instead of under the button. */}
    <div className="work-session-anchor">
    <button className="work-session-trigger" type="button" aria-label="指派既有 Session" aria-haspopup="listbox"
      aria-expanded={open} autoFocus={autoFocus} onClick={() => setOpen((shown) => !shown)}>
      {selected ? <><SessionStateDot session={selected} /><span><b>{selected.label || selected.id}</b><PersonaTag id={selected.persona} personas={personas} />
        <small>{assistantName(selected.assistant)} · {sessionActivityName(selected.state)} · {sessionWorkStateName(selected.work_state)}</small></span></>
        : <><span className="work-session-placeholder" aria-hidden="true">◌</span><span>選擇既有 Session</span></>}
      <span className="work-project-chevron" aria-hidden="true">⌄</span>
    </button>
    {open && <div className="work-session-menu" role="listbox" aria-label="可指派的 Session">
      {sessions.map((session) => <SessionChoice key={session.id} session={session} reading={readings[session.id]}
        selected={session.id === value} onChoose={() => choose(session.id)} />)}
      {!sessions.length && <p className="work-project-empty">這個 Project 目前沒有可用的 Session。</p>}
    </div>}
    </div>
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
  const personas = usePersonas()
  return <button className="work-session-option" type="button" role="option" aria-selected={selected} onClick={onChoose}>
    <SessionStateDot session={session} />
    <span><b>{session.label || session.id}</b><PersonaTag id={session.persona} personas={personas} /><small>{assistantName(session.assistant)} · {sessionActivityName(session.state)} · {sessionWorkStateName(session.work_state)}</small></span>
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
        id: item.id, title: item.title, meta: `${item.project.label} · 完成 ${when(item.closed_at)}`,
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

function CreatedWorkModal({ item, created = true, sessions, decisions, busy, failure, clearFailure, run, detailLoading, detailError, retryDetail, onClose }: {
  item: WorkV2Item
  created?: boolean
  sessions: SessionRow[]
  decisions: Decision[]
  busy: string
  failure: string
  clearFailure: () => void
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
  detailLoading: boolean
  detailError: string
  retryDetail: () => void
  onClose: () => void
}) {
  const modal = useRef<HTMLDivElement>(null)
  const initialFocus = useRef<HTMLButtonElement>(null)
  useModalDismiss(false, onClose)
  useModalFocus(modal, initialFocus)
  // Opened from a Session too, so it lives beside the app root, never inside
  // the fixed Session pane: a phone then keeps one scroll surface.
  return createPortal(<div ref={modal} tabIndex={-1} className={created ? "session-todo-modal work-created-modal" : "session-todo-modal work-created-modal work-item-detail-modal"}
    role="dialog" aria-modal="true" aria-labelledby={`work-created-title-${item.id} work-card-title-${item.id}`}
    onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <div className={created ? "work-created-panel" : "work-created-panel work-item-detail-panel"}>
      <div className="work-modal-head"><div><p className="board-eyebrow">{created ? "WORK ITEM CREATED" : "BOARD ITEM"}</p>
        <h2 id={`work-created-title-${item.id}`}>{created ? "看板項目已建立" : "看板項目"}</h2></div>
        <button ref={initialFocus} className="work-modal-close" type="button" aria-label="關閉" onClick={onClose}><WorkIcon name="close" /></button></div>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <WorkCard item={item} sessions={sessions} decisions={decisions} busy={busy} failure={failure} clearFailure={clearFailure} run={run}
        detailLoading={detailLoading} detailError={detailError} retryDetail={retryDetail} focusAssignment={created} reportsExpanded={!created} foldDescription={!created} />
    </div>
  </div>, document.body)
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
        <button className="work-modal-close" type="button" aria-label="關閉" disabled={busy} onClick={onClose}><WorkIcon name="close" /></button></div>
      <label>標題<input className="work-input" value={title} maxLength={240} autoFocus onChange={(event) => setTitle(event.target.value)} /></label>
      <VoiceTextarea label="描述" value={description} maxLength={65536} onValue={setDescription} />
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
        <button className="work-modal-close" type="button" aria-label="關閉" disabled={busy} onClick={onClose}><WorkIcon name="close" /></button></div>
      <p><strong>{item.title}</strong> 會從看板與負責 Session 的待辦移除。執行紀錄仍會保留，避免工作憑空消失。</p>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <div className="work-actions"><button className="chip danger" type="submit" disabled={busy}>{busy ? "刪除中…" : "確認刪除"}</button>
        <button className="chip" type="button" disabled={busy} onClick={onClose}>保留項目</button></div>
    </form>
  </div>
}

function useModalDismiss(busy: boolean, onClose: () => void) {
  useEffect(() => {
    const close = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || busy) return
      event.preventDefault()
      onClose()
    }
    document.addEventListener("keydown", close)
    return () => document.removeEventListener("keydown", close)
  }, [busy, onClose])
}

/** Keep the keyboard in the modal and put it back on the summary that opened it. */
function useModalFocus(container: RefObject<HTMLDivElement | null>, initialFocus: RefObject<HTMLElement | null>) {
  // StrictMode runs an effect's setup/cleanup/setup sequence once in
  // development. Keep the opener across that rehearsal and cancel its false
  // restoration when the second setup starts.
  const previous = useRef<HTMLElement | null>(null)
  const restoreTimer = useRef<number | null>(null)
  if (!previous.current && document.activeElement instanceof HTMLElement) previous.current = document.activeElement
  useEffect(() => {
    if (restoreTimer.current !== null) window.clearTimeout(restoreTimer.current)
    initialFocus.current?.focus({ preventScroll: true })
    const keepFocus = (event: KeyboardEvent) => {
      if (event.key !== "Tab" || !container.current) return
      const controls = [...container.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )].filter((node) => node.getClientRects().length > 0)
      if (!controls.length) {
        event.preventDefault()
        container.current.focus({ preventScroll: true })
        return
      }
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (event.shiftKey && (document.activeElement === first || !container.current.contains(document.activeElement))) {
        event.preventDefault(); last.focus()
      } else if (!event.shiftKey && (document.activeElement === last || !container.current.contains(document.activeElement))) {
        event.preventDefault(); first.focus()
      }
    }
    document.addEventListener("keydown", keepFocus)
    return () => {
      document.removeEventListener("keydown", keepFocus)
      // React removes the portal after effect cleanup. Restore on the next
      // task so the disappearing close button cannot hand focus back to body.
      restoreTimer.current = window.setTimeout(() => {
        if (previous.current?.isConnected) previous.current.focus({ preventScroll: true })
      }, 0)
    }
  }, [container, initialFocus])
}

function WorkReferenceImage({ item, image, busy, run }: {
  item: WorkV2Item
  image: NonNullable<WorkV2Item["images"]>[number]
  busy: string
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
}) {
  // The card draws the small copy; the red pen and the new tab get the
  // original, read when they are opened (`useReferenceImage`).
  const { source, failed, full, fullFailed, opening, loadFull, openFull } = useReferenceImage(image.id)
  const [marking, setMarking] = useState(false)
  const mark = () => { void loadFull().then(() => setMarking(true), () => {}) }
  const editable = !item.closed_at
  // The marked copy takes the original's place: added at its position, then
  // the original removed. A full item has no room for the copy first, so it
  // removes the original first and puts it back if the copy is refused.
  const replaceWithMarks = (canvas: HTMLCanvasElement) => {
    const marked = markedFile(canvas, image.title)
    void run(`image-mark-${image.id}`, async () => {
      const picture = await prepareReferencePicture(marked)
      if ((item.images?.length ?? 0) < MAX_REFERENCE_PICTURES) {
        const added = await addWorkV2Image(item.id, item.version, picture, image.position)
        return deleteWorkV2Image(added.item, image.id)
      }
      // The original goes back, not the thumbnail the card is drawn from.
      const original = await prepareReferencePicture(new File([await (await fetch(full || await loadFull())).blob()], image.title, { type: image.media_type }))
      const removed = await deleteWorkV2Image(item, image.id)
      try {
        return await addWorkV2Image(item.id, removed.item.version, picture, image.position)
      } catch (error) {
        await addWorkV2Image(item.id, removed.item.version, original, image.position).catch(() => {})
        throw error
      }
    })
    return true
  }
  return <figure className="work-reference-image">
    {source ? editable
      ? <button className="work-reference-open" type="button" disabled={!!busy || opening} aria-label={`用紅筆標記參考圖片 ${image.title}`}
        title="用紅筆標記" onClick={mark}>
        <img src={source} alt={image.title} width={image.width} height={image.height} />
      </button>
      : <a href={full || source} target="_blank" rel="noreferrer" aria-label={`開啟參考圖片 ${image.title}`} onClick={openFull}>
        <img src={source} alt={image.title} width={image.width} height={image.height} />
      </a> : <div className="work-reference-loading" role={failed ? "alert" : undefined}>{failed || "載入圖片…"}</div>}
    <figcaption title={image.title} role={fullFailed ? "alert" : undefined}>{fullFailed || image.title}</figcaption>
    {marking && full && <PictureMarkup picture={{ id: image.id, url: full }} onCancel={() => setMarking(false)} onSave={replaceWithMarks} />}
    {!item.closed_at && <button className="work-reference-remove" type="button" aria-label={`移除參考圖片 ${image.title}`} disabled={!!busy}
      onClick={() => void run(`image-delete-${image.id}`, () => deleteWorkV2Image(item, image.id))}><WorkIcon name="close" /></button>}
  </figure>
}

function ProjectPicker({ places, value, onChange, onOpen, allowAll = false }: {
  places: ProjectPlace[]
  value: string
  onChange: (id: string) => void
  onOpen?: () => Promise<void>
  allowAll?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
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
  const toggle = () => {
    const opening = !open
    setOpen(opening)
    if (!opening || !onOpen) return
    setRefreshing(true)
    void onOpen().catch(() => {}).finally(() => setRefreshing(false))
  }
  return <div className="work-project-picker" ref={root}>
    <button className="work-project-trigger" type="button" aria-label="Project" aria-haspopup="listbox"
      aria-expanded={open} onClick={toggle}>
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
        {place.id === value && <span className="work-project-check"><WorkIcon name="check" /></span>}
      </button>)}
      {!places.length && <p className="work-project-empty">{refreshing ? "正在讀取 Project…" : "目前沒有可用的 Project。"}</p>}
    </div>}
  </div>
}

function NewWorkModal({ places, initialProject, initialDraft, busy, failure, onRefreshPlaces, onClose, onCreate }: { places: ProjectPlace[]; initialProject: string; initialDraft: NewWorkItemDraft; busy: boolean; failure: string; onRefreshPlaces: () => Promise<void>; onClose: () => void; onCreate: (body: Parameters<typeof createWorkV2>[0], images: File[], decisionKey: string) => void }) {
  const [projectID, setProjectID] = useState(initialDraft.projectID || initialProject)
  const [kind, setKind] = useState<WorkV2Kind>(initialDraft.kind || "feature")
  const [title, setTitle] = useState(initialDraft.title || "")
  const [description, setDescription] = useState(initialDraft.description || "")
  const [images, setImages] = useState<File[]>([])
  const createDecision = useRef<WorkV2CreateDecision | null>(null)
  const projectPlaces = initialDraft.project && !places.some((place) => place.id === initialDraft.project?.id)
    ? [initialDraft.project, ...places]
    : places
  const ready = !!projectID && !!title.trim() && !!description.trim()
  const reviewingDraft = !!(initialDraft.projectID || initialDraft.title || initialDraft.description)
  useModalDismiss(busy, onClose)
  return <div className="session-todo-modal work-new-modal" role="dialog" aria-modal="true" aria-labelledby="work-new-v2-title"
    onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}><form onSubmit={(e) => {
    e.preventDefault(); if (!ready) return
    const body = { project_id: projectID, kind, title: title.trim(), description: description.trim(), acceptance_criteria: "", deployment_policy: "agent_decides" as const }
    const decision = workV2CreateDecision(body, createDecision.current)
    createDecision.current = decision
    onCreate(body, images, decision.key)
  }}>
    <div className="work-modal-head"><div><p className="board-eyebrow">{reviewingDraft ? "REVIEW WORK ITEM" : "NEW WORK ITEM"}</p><h2 id="work-new-v2-title">{reviewingDraft ? "確認看板項目" : "建立看板項目"}</h2></div>
      <button className="work-modal-close" type="button" aria-label="關閉" disabled={busy} onClick={onClose}><WorkIcon name="close" /></button></div>
    {reviewingDraft && <p className="work-note">語音已填入草稿；按「建立」前不會新增看板項目。</p>}
    <div className="work-modal-field"><span>Project</span><ProjectPicker places={projectPlaces} value={projectID} onChange={setProjectID} onOpen={onRefreshPlaces} /></div>
    <fieldset className="work-kind-field"><legend>類型</legend><div className="work-kind-list">
      {KINDS.map((value) => { const meta = KIND_META[value]; return <button key={value} type="button" className="work-kind-option"
        aria-pressed={kind === value} onClick={() => setKind(value)}><span className="work-kind-icon" aria-hidden="true">{meta.icon}</span>
        <span><b>{meta.label}</b><small>{meta.description}</small></span><span className="work-kind-radio"><WorkIcon name={kind === value ? "radio" : "circle"} /></span></button> })}
    </div></fieldset>
    <label>標題<input className="work-input" value={title} maxLength={240} onChange={(e) => setTitle(e.target.value)} /></label>
    <VoiceTextarea label="描述" value={description} onValue={setDescription} />
    <PendingPictures images={images} busy={busy} note="建立項目後上傳" onChange={setImages} />
    {failure && <p className="work-note" role="alert">{failure}</p>}
    <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>{busy ? "建立中…" : "建立"}</button><button className="chip" type="button" disabled={busy} onClick={onClose}>取消</button></div>
  </form></div>
}

function phaseName(phase: string): string {
  return ({ created: "建立", assigning: "認領中", assigned: "已認領", implementing: "實作", verifying: "驗證", merging: "Merge 回 Git", deploying: "部署", done: "完成", cancelled: "取消" } as Record<string, string>)[phase] ?? phase
}
