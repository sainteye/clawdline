import { catalogFormat } from "../../catalog.js"
import { catalogWord, catalogWordLanguage } from "../../catalog.js"
import { catalogLabel, closingMark, fullWidthPunctuation, quotedTitle } from "../../punctuation.js"
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from "react"
import { createPortal } from "react-dom"
import type { Assistant, SessionRow } from "@clawdline/contract"
import { RefusalError, asMachineNeedsUpdate, type MachineNeedsUpdate } from "@clawdline/core"
import { NeedsUpdate } from "../../machine/NeedsUpdate.js"
import * as L from "../../legacy/bridge.js"
import { isPicture, prepareReferencePicture } from "../../legacy/shots-bridge.js"
import { sessionFragment } from "../../session/address.js"
import { Mark } from "../../session/List.js"
import { PersonaBot, PersonaTag, usePersonas } from "../../session/PersonaBot.js"
import { personaById, personaName, personaTitle, rememberTeam, rememberedTeam, shownTeam, switchTeam } from "../../personas.js"
import { RoleRow } from "../../session/RoleRow.js"
import { nextWord } from "../../next-strings.js"
import { workProjectID, workRouteFromHash } from "../../page-route.js"
import { failureWords, when } from "./shared.js"
import { onOpenNewWorkItem, onOpenWorkItem, type NewWorkItemDraft } from "./new-item.js"
import { announceWorkItemChanged } from "./item-changed.js"
import { WorkMilestones } from "./WorkMilestones.js"
import { WorkGateAttention, WorkGateDetail, WorkGateLine } from "./WorkGate.js"
import { gateSnapshotText } from "./gate-status.js"
import { WorkSteps } from "./WorkSteps.js"
import { WorkCompletionReports, WorkEpicPlanDocuments, WorkItemDocuments } from "./WorkCompletionReport.js"
import { epicGate, epicGateDetailShown, epicGateShown, featureLike, isEpic, planGateHint } from "./epic-gate.js"
import { epicChildren, epicParent, epicProgress, epicProgressWords, needsFamilyList, readMissingParents, shortWorkID, type EpicParent } from "./epic-family.js"
import { WorkIcon } from "./WorkIcon.js"
import { MAX_REFERENCE_PICTURES, markedFile, PendingPictures, PictureMarkup } from "./ReferencePictures.js"
import { useReferenceImage } from "./useReferenceImage.js"
import { VoiceTextarea } from "./VoiceTextarea.js"
import { ItemUsageCard } from "./TokenBill.js"
import { arrangeWorkItems, workItemPlaces } from "./board-order.js"
import { useBoardMotion } from "./board-motion.js"
import { completeConfirmWords } from "./complete-item.js"
import { phaseName } from "./phase-name.js"
import { confirmDecisionAnswer, decisionsForWorkItem, matchingDecisionAnswer, proposalsForProject, withoutAnsweredDecision, withoutAnsweredWait, type DecisionAnswerStatus } from "./board-attention.js"
import { conditionWords, deploymentWords, needsPerson, nextActionWords, ownerOnlineWords, phaseStayWords } from "./board-card-facts.js"
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
  markWorkV2Seen,
  setWorkV2ReviewRequired,
  readDecisions,
  readDecision,
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
  awaitsAcceptance,
  NEW_SESSION_ASSISTANTS,
  rememberAssistant,
  rememberedAssistant,
  sessionActivityName,
  sessionWorkCounts,
  sessionWorkLabel,
} from "./session-assignment.js"
import { workV2CreateDecision, type WorkV2CreateDecision } from "./create-decision.js"
import { claimedViaLine, createdViaLine, epicOwnerLine, originLine, workOrigin, workWord } from "./words.js"
import { appendWorkPage } from "./work-pages.js"
import { visibleWorkItems } from "./plan-visibility.js"
import { TerminalEntry } from "../terminal/TerminalEntry.js"

const KINDS: WorkV2Kind[] = ["feature", "issue", "epic", "refactor", "plan"]
const EXECUTABLE_KINDS: WorkV2ExecutableKind[] = ["feature", "issue", "epic", "refactor"]
const PHASES = ["assigning", "assigned", "implementing", "verifying", "merging", "deploying"]
const KIND_META: Record<WorkV2Kind, { icon: string; label: string; description: string }> = {
  feature: { icon: "✦", label: "Feature", description: "56f8e506038b" },
  issue: { icon: "!", label: "Issue", description: "33ca73248c20" },
  epic: { icon: "◆", label: "Epic", description: "7ef41cddac8e" },
  refactor: { icon: "↻", label: "Refactor", description: "728f2c662e96" },
  plan: { icon: "≡", label: "Plan", description: "ba5eb53b1697" },
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
  const [nowSeconds, setNowSeconds] = useState(() => Math.floor(Date.now() / 1000))
  const loadGeneration = useRef(0)
  const answeredDecisionIDs = useRef(new Set<string>())
  const submittingDecisionIDs = useRef(new Set<string>())
  const [decisionAnswers, setDecisionAnswers] = useState<Record<string, DecisionAnswerStatus>>({})
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
      setItems(rows.map((item) => [...answeredDecisionIDs.current].reduce(withoutAnsweredWait, item))); setNextCursor(work.next_cursor); setFamily(relatives); setLoaded(true); setLoading(false)
      loadedView.current = requestedView
      setPlaces(projects.places); setSessions(live.sessions); setProposals(suggestions.rows); setDecisions(questions.rows.filter((row) => !answeredDecisionIDs.current.has(row.id))); setFailure("")
      setCreatedItem((current) => current ? [...answeredDecisionIDs.current].reduce(withoutAnsweredWait, work.rows.find((item) => item.id === current.id) ?? current) : null)
      setOpenedItem((current) => {
        if (!current) return null
        const listed = work.rows.find((item) => item.id === current.id)
        return [...answeredDecisionIDs.current].reduce(withoutAnsweredWait, listed && listed.version !== current.version ? listed : current)
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
        return rows.map((item) => [...answeredDecisionIDs.current].reduce(withoutAnsweredWait, item))
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
    if (!shown) return
    const timer = window.setInterval(() => setNowSeconds(Math.floor(Date.now() / 1000)), 60_000)
    return () => window.clearInterval(timer)
  }, [shown])
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
    void readWorkV2Item(id).then((answer) => setOpenedItem((current) => current?.id === id
      ? [...answeredDecisionIDs.current].reduce(withoutAnsweredWait, answer.item) : current))
      .catch((error: unknown) => setDetailError(failureWords(error)))
      .finally(() => setDetailLoading(false))
  }, [])
  // Every request to open an item takes a ticket; a read that answers after a
  // later one was made is dropped rather than replacing what is now open.
  const openTicket = useRef(0)
  const openItem = useCallback((item: WorkV2Item) => {
    openTicket.current++
    setFailure("")
    setOpenedItem([...answeredDecisionIDs.current].reduce(withoutAnsweredWait, item))
    refreshDetail(item.id)
  }, [refreshDetail])
  useEffect(() => onOpenWorkItem(openItem), [openItem])
  // Opening an item that reached deploying or done is the person's receipt for
  // it: its Session card stops saying 待驗收 on every device. The receipt rides
  // along with the view; a failed one leaves the card as it was, and the next
  // opening sends it again.
  const seenPhase = openedItem && awaitsAcceptance(openedItem.phase) ? openedItem.phase : ""
  useEffect(() => {
    if (!openedItem?.id || !seenPhase) return
    void markWorkV2Seen(openedItem.id, seenPhase).catch(() => undefined)
  }, [openedItem?.id, seenPhase])
  const listedRows = useRef<WorkV2Item[]>([])
  listedRows.current = items.concat(family.rows)
  /**
   * Opens an item's detail by id: from the Board or family row when one is
   * loaded, otherwise after reading it, and only when that read succeeds.
   * Answers "" when it opened (or a later request took over) and the failure
   * words otherwise, for the caller to show beside the link that was pressed.
   */
  const openWorkItemById = useCallback(async (id: string): Promise<string> => {
    const ticket = ++openTicket.current
    const listed = listedRows.current.find((row) => row.id === id)
    if (listed) { openItem(listed); return "" }
    try {
      const answer = await readWorkV2Item(id)
      if (ticket !== openTicket.current) return ""
      setFailure(""); setDetailError(""); setOpenedItem([...answeredDecisionIDs.current].reduce(withoutAnsweredWait, answer.item))
      return ""
    } catch (error) {
      return ticket === openTicket.current ? failureWords(error) : ""
    }
  }, [openItem])
  // The control that opened the detail dialog gets focus back when it closes,
  // however many items were opened from inside it in between.
  const detailReturn = useRef<ModalReturn>({ opener: null, timer: null })

  const run = async (key: string, task: () => Promise<unknown>, refreshInBackground = false) => {
    if (busy) return false
    setBusy(key); setFailure("")
    let succeeded = false
    try {
      const answer = await task()
      // The machine accepted it: a Session fold showing this item reads again
      // now, not on its next tick.
      announceWorkItemChanged()
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
  const answerWorkDecision = async (decisionID: string, optionID: string): Promise<boolean> => {
    if (submittingDecisionIDs.current.has(decisionID) || answeredDecisionIDs.current.has(decisionID)) return false
    const decision = decisions.find((row) => row.id === decisionID)
    const option = decision?.options.find((row) => row.id === optionID)
    if (!decision || !option) return false
    submittingDecisionIDs.current.add(decisionID)
    const base = { option: optionID, label: option.label, workID: decision.work_id ?? "", question: decision.question }
    setDecisionAnswers((current) => ({ ...current, [decisionID]: { ...base, phase: "pending" } }))
    const confirmed = () => {
      answeredDecisionIDs.current.add(decisionID)
      setDecisionAnswers((current) => ({ ...current, [decisionID]: { ...base, phase: "confirmed" } }))
      setDecisions((current) => withoutAnsweredDecision(current, decisionID))
      setItems((current) => current.map((item) => withoutAnsweredWait(item, decisionID)))
      setCreatedItem((current) => current && withoutAnsweredWait(current, decisionID))
      setOpenedItem((current) => current && withoutAnsweredWait(current, decisionID))
    }
    try {
      await confirmDecisionAnswer(() => answerDecision(decisionID, optionID), confirmed)
      announceWorkItemChanged()
      void load()
      return true
    } catch (error) {
      // refusal-ok: an unconfirmed send is re-read before it is called anything, and a named refusal goes to failureWords (shared.ts), which ends in failureSentence.
      if (!(error instanceof RefusalError) || error.code === "request_in_progress") {
        try {
          const observed = (await readDecision(decisionID)).decision
          if (matchingDecisionAnswer(observed, optionID)) { confirmed(); announceWorkItemChanged(); void load(); return true }
          if (observed.state === "answered") {
            setDecisionAnswers((current) => ({ ...current, [decisionID]: { ...base, phase: "rejected", message: catalogWord("literal", "5aabeb560dbd") } }))
            return false
          }
        } catch { /* A failed read cannot prove whether the POST arrived. */ }
        setDecisionAnswers((current) => ({ ...current, [decisionID]: { ...base, phase: "retry", message: catalogWord("literal", "e2379153f479") } }))
      } else {
        setDecisionAnswers((current) => ({ ...current, [decisionID]: { ...base, phase: "rejected", message: catalogFormat("template", "e3ac6cdc91b5", [failureWords(error)]) } }))
      }
      return false
    } finally { submittingDecisionIDs.current.delete(decisionID) }
  }
  const visibleItems = visibleWorkItems(items, showPlans)
  const hiddenPlans = items.length - visibleItems.length
  const planning = visibleItems.filter((item) => item.area === "planning" && !item.closed_at)
  const unassigned = visibleItems.filter((item) => item.area === "unassigned" && !item.closed_at)
  const done = visibleItems.filter((item) => item.closed_at)
  const visibleProposals = proposalsForProject(proposals, project)
  const familyView = useMemo<EpicFamilyView>(() => ({ rows: family.rows, truncated: family.truncated, open: openWorkItemById }),
    [family, openWorkItemById])

  return <EpicFamilyContext.Provider value={familyView}>
  <section ref={board} id="work" className="page board-page work-page" data-page-view="work" hidden={!shown} aria-labelledby="work-v2-title"
    aria-busy={loading || refreshing || paging ? "true" : undefined}>
    <header className="board-head">
      <div><p className="board-eyebrow">{catalogWord("inline", "68441095f928")}</p><h1 id="work-v2-title">{catalogWord("inline", "0072da457a09")}</h1></div>
      <div className="work-head-tools">
        <button className="board-button" type="button" onClick={() => { setFailure(""); setCreatedItem(null); setCreateDraft({}); setCreating(true) }}>{catalogWord("inline", "e5a0a01cc43c")}</button>
        <button className="board-button" type="button" disabled={!!busy || loading || refreshing || paging}
          aria-busy={refreshing ? "true" : undefined} aria-label={refreshing ? catalogWord("literal", "8e0d15ad92d4") : undefined}
          onClick={() => { rearrange.current = true; void load(true) }}>{L.strings.webInfoRefresh}</button>
      </div>
    </header>
    <div className="work-wrap">
      <p className="work-lede">{catalogWord("inline", "984a81d05631")}</p>
      <div className="work-filter-bar">
        <ProjectPicker places={places} value={project} onChange={(value) => setRouteProject(value)} onOpen={refreshPlaces} allowAll />
        <div className="work-filter-controls">
          <div className="work-status-filter" role="group" aria-label={catalogWord("inline", "ce34a838f587")}>
            {([['open', catalogWord("literal", "8f643bcd5a10")], ['done', catalogWord("literal", "20df2a7775cd")], ['all', catalogWord("literal", "aa44a36dc811")]] as [WorkV2Status, string][]).map(([value, label]) =>
              <button key={value} type="button" aria-pressed={status === value} onClick={() => setStatus(value)}>{label}</button>)}
          </div>
          <button className="work-plan-toggle" type="button" aria-pressed={showPlans}
            onClick={() => setShowPlans((current) => !current)}>{catalogWord("inline", "187097a69436")}</button>
        </div>
        <label className="work-search">
          <WorkIcon name="search" />
          <input type="search" aria-label={catalogWord("inline", "60005ee63c14")} placeholder={catalogWord("inline", "60005ee63c14")} maxLength={1024} value={searchInput}
            onChange={(event) => setSearchInput(event.currentTarget.value)} />
          {searchInput && <button type="button" aria-label={catalogWord("inline", "0c2d1a3aeaff")} onClick={() => setSearchInput("")}><WorkIcon name="close" /></button>}
        </label>
      </div>
      <TerminalEntry key={project} project={project} label={places.find((place) => place.id === project)?.label ?? ""} />
      {failure && <p className="work-note" role="alert">{failure}</p>}
      {loading ? <BoardSkeleton /> : <>
      {visibleProposals.length > 0 && <ProposalQueue proposals={visibleProposals} items={items} places={places} busy={busy} run={run} />}
      <BoardRegion title={catalogWord("inline", "944fa4126f45")} items={planning} sessions={sessions} nowSeconds={nowSeconds} decisions={decisions} onOpen={openItem} />
      <BoardRegion title={catalogWord("inline", "109be039b4c0")} items={unassigned} sessions={sessions} nowSeconds={nowSeconds} decisions={decisions} onOpen={openItem} />
      {PHASES.map((phase) => <BoardRegion key={phase} title={phaseName(phase)}
        items={visibleItems.filter((item) => item.area === phase && !item.closed_at)} sessions={sessions} nowSeconds={nowSeconds} decisions={decisions} onOpen={openItem} />)}
      {done.length > 0 && status === "done" && <BoardRegion title={catalogWord("inline", "f28461bb49c8")} items={done} sessions={sessions} nowSeconds={nowSeconds} decisions={decisions} onOpen={openItem} />}
      {done.length > 0 && status !== "done" && search && <BoardRegion title={catalogWord("inline", "075493f7aa67")} items={done} sessions={sessions} nowSeconds={nowSeconds} decisions={decisions} onOpen={openItem} />}
      {done.length > 0 && status !== "done" && !search && <details className="work-section work-done"><summary><div className="work-section-head"><h2>{catalogWord("inline", "075493f7aa67")}</h2><span className="work-count">{done.length}</span></div></summary>
        <div className="work-cards">{done.map((item) => <CompactWorkCard key={item.id} item={item}
          sessions={sessions} nowSeconds={nowSeconds} decisions={decisionsForWorkItem(decisions, item.id)} onOpen={() => openItem(item)} />)}</div>
      </details>}
      {loaded && !failure && visibleItems.length === 0 && <p className="work-empty work-filter-empty" role="status">
        {!showPlans && hiddenPlans > 0
          ? search ? catalogWord("literal", "f653840acc04") : catalogWord("literal", "000c45f69d2f")
          : search ? catalogFormat("template", "1619c02fca4b", [search]) : status === "done" ? catalogWord("literal", "6bb45a6f3d11") : catalogWord("literal", "9c2aa7fb1748")}
      </p>}
      {nextCursor && <div className="work-pagination">
        <button className="board-button work-more" type="button" disabled={paging} aria-busy={paging ? "true" : undefined}
          onClick={() => void loadMore()}>{paging ? catalogWord("literal", "223a19c135b4") : catalogWord("literal", "70ee17f5b118")}</button>
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
    {createdItem && <CreatedWorkModal item={createdItem} sessions={sessions} decisions={decisionsForWorkItem(decisions, createdItem.id)} decisionAnswers={decisionAnswers} busy={busy} failure={failure}
      detailLoading={false} detailError="" retryDetail={() => void readWorkV2Item(createdItem.id).then((answer) => setCreatedItem([...answeredDecisionIDs.current].reduce(withoutAnsweredWait, answer.item))).catch((error: unknown) => setFailure(failureWords(error)))}
      clearFailure={() => setFailure("")} run={run} answerWorkDecision={answerWorkDecision} onClose={() => setCreatedItem(null)} />}
    {openedItem && <CreatedWorkModal item={openedItem} created={false} key={openedItem.id} back={detailReturn.current} sessions={sessions} decisions={decisionsForWorkItem(decisions, openedItem.id)} decisionAnswers={decisionAnswers} busy={busy} failure={failure}
      detailLoading={detailLoading} detailError={detailError} retryDetail={() => refreshDetail(openedItem.id)}
      clearFailure={() => setFailure("")} run={run} answerWorkDecision={answerWorkDecision} onClose={() => setOpenedItem(null)} />}
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
  let family: { rows: WorkV2Item[]; truncated: boolean }
  if (status === "all" && !search) family = work
  else {
    try {
      family = await readWorkV2(project || undefined, "all")
    } catch {
      family = { rows: work.rows, truncated: true }
    }
  }
  // The family list is one page, so an Epic can be missing from it while its
  // child is on the Board; those parents are read one by one, each once.
  const listed = work.rows.concat(family.rows)
  const parents = await readMissingParents(listed, listed, async (id) => (await readWorkV2Item(id)).item)
  return parents.length ? { rows: family.rows.concat(parents), truncated: family.truncated } : family
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
  /** Opens another item's detail by id; answers the failure words, or "" once it opened. */
  open: (id: string) => Promise<string>
}

const EpicFamilyContext = createContext<EpicFamilyView>({ rows: [], truncated: false, open: async () => "" })

/** On an Epic: the items created under it, how far they have got, and who has each. */
function EpicChildren({ item, sessions }: { item: WorkV2Item; sessions: SessionRow[] }) {
  const family = useContext(EpicFamilyContext)
  const personas = usePersonas()
  const [failure, setFailure] = useState("")
  const children = epicChildren(family.rows, item.id)
  if (!children.length) return null
  const progress = epicProgress(children)
  const complete = progress.total > 0 && progress.done === progress.total
  return <section className="work-epic-children" aria-label={catalogWord("inline", "c89d5d10a699")} data-complete={complete ? "" : undefined}>
    <div className="work-epic-children-head">
      <strong>{catalogWord("inline", "c89d5d10a699")}</strong>
      <span>{epicProgressWords(progress)}{family.truncated && catalogWord("literal", "94d08f62dcc8")}</span>
    </div>
    {progress.total > 0 && <div className="work-epic-progress" role="progressbar" aria-label={catalogWord("inline", "01f6acc771a3")}
      aria-valuemin={0} aria-valuemax={progress.total} aria-valuenow={progress.done}>
      <i style={{ width: `${(progress.done / progress.total) * 100}%` }} />
    </div>}
    <ul>{children.map((child) => {
      const owner = child.owner_session ? sessions.find((session) => session.sessionId === child.owner_session) : undefined
      const who = owner ? (owner.label || owner.id) : child.owner_session ? `Session ${child.owner_session.slice(0, 8)}` : catalogWord("literal", "f40cef79943e")
      const ownerPersona = personaById(personas, owner?.persona)
      return <li key={child.id} data-phase={child.phase}>
        <button className="work-epic-child" type="button" onClick={() => { setFailure(""); void family.open(child.id).then(setFailure) }}>
          <span className="work-epic-child-kind" aria-label={KIND_META[child.kind].label} title={KIND_META[child.kind].label}>{KIND_META[child.kind].icon}</span>
          <span className="work-epic-child-title">{child.title}</span>
          <span className="work-epic-child-meta">{phaseName(child.phase)} · {ownerPersona
            ? <span className="work-owner-persona" title={personaTitle(ownerPersona)}><PersonaBot persona={ownerPersona} cellPx={2} className="persona-tag-bot" />{who}</span>
            : who}</span>
        </button>
      </li>
    })}</ul>
    {failure && <p className="work-note" role="alert">{workWord("openItemFailed", { reason: failure })}</p>}
  </section>
}

/** On a child: the Epic it was created under. */
function EpicParentLine({ item }: { item: WorkV2Item }) {
  const family = useContext(EpicFamilyContext)
  const [failure, setFailure] = useState("")
  const parent = epicParent(item, family.rows)
  if (!parent) return null
  return <>
    <p className="work-epic-parent">{catalogLabel("inline", "f738987d8bdc")}<button className="work-epic-parent-link" type="button"
      onClick={() => { setFailure(""); void family.open(parent.id).then(setFailure) }}>
      {parent.title ? quotedTitle(parent.title) : <code>{shortWorkID(parent.id)}</code>}</button></p>
    {failure && <p className="work-note" role="alert">{workWord("openItemFailed", { reason: failure })}</p>}
  </>
}

/**
 * On a Board card: the Epic it belongs to, as its own control beside the
 * card's summary button rather than inside it, opening the Epic's detail.
 */
function CardParentLine({ parent }: { parent: EpicParent }) {
  const family = useContext(EpicFamilyContext)
  const [opening, setOpening] = useState(false)
  const [failure, setFailure] = useState("")
  const title = parent.title || shortWorkID(parent.id)
  const phase = family.rows.find((row) => row.id === parent.id)?.phase
  const closed = phase === "done" ? workWord("epicParentDone") : phase === "cancelled" ? workWord("epicParentCancelled") : ""
  return <div className="work-card-parent">
    <button className="work-card-parent-link" type="button" aria-busy={opening || undefined}
      aria-label={[workWord("cardEpicParentLabel", { title }), closed].filter(Boolean).join(" · ")}
      onClick={() => {
        setOpening(true); setFailure("")
        void family.open(parent.id).then((words) => { setFailure(words); setOpening(false) })
      }}>
      <span className="work-card-parent-title">{workWord("cardEpicParent", { title })}</span>
      {closed && <span className="work-card-parent-state">{closed}</span>}
    </button>
    {failure && <p className="work-note" role="alert">{workWord("openItemFailed", { reason: failure })}</p>}
  </div>
}

/** A small robot head: the mark an Agent-made card's badge carries beside its words. */
function AgentGlyph() {
  return <svg className="work-card-origin-glyph" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" focusable="false">
    <path d="M8 1.5v2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    <rect x="2.5" y="4" width="11" height="9" rx="2.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
    <circle cx="6" cy="8.5" r="1.2" fill="currentColor" /><circle cx="10" cy="8.5" r="1.2" fill="currentColor" />
  </svg>
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
    <summary><strong>{catalogWord("inline", "90b290a48f82")}</strong><span className="work-count">{proposals.length}</span>
      <span className="work-fold-hint" lang={catalogWordLanguage("inline", "2be2b494050d")}>{catalogWord("inline", "2be2b494050d")}</span></summary>
    <div className="work-fold-body"><ul className="work-proposals">{proposals.map((proposal) => {
      const project = places.find((place) => place.id === proposal.project_id)
      const source = proposal.source_work_id ? items.find((item) => item.id === proposal.source_work_id) : undefined
      const sourceLine = source ? catalogFormat("template", "d03a9f051a31", [source.title])
        : proposal.source_work_id ? catalogFormat("template", "029ac348ef8a", [proposal.source_work_id.slice(0, 8)])
          : proposal.source_todo_id ? catalogWord("literal", "89d0cc30dc8b") : catalogWord("literal", "15d308223697")
      return <li key={proposal.id} className="work-proposal" data-proposal-id={proposal.id}>
        <div className="work-proposal-main">
          <span className="work-state">{catalogWord("inline", "e9dfb2772400")} {KIND_META[proposal.kind].label}</span>
          <h3>{proposal.title}</h3>
          <p className="work-proposal-context">{project?.label ?? proposal.project_id} · {sourceLine}</p>
          <p className="work-proposal-reason"><strong>{catalogWord("inline", "0dc513d1a44e")}</strong><span>{proposal.reason}</span></p>
          <details className="work-proposal-detail">
            <summary><span lang={catalogWordLanguage("inline", "3b61e4e93652")}>{catalogWord("inline", "3b61e4e93652")}</span><span lang={catalogWordLanguage("inline", "b3b7ae4f6384")}>{catalogWord("inline", "b3b7ae4f6384")}</span></summary>
            <dl><div><dt>{catalogWord("inline", "e78cea7271c7")}</dt><dd>{proposal.description}</dd></div>
              <div><dt>{catalogWord("inline", "e2a4b9a60cac")}</dt><dd>{proposal.suggested_acceptance || catalogWord("literal", "865569dfdfe1")}</dd></div></dl>
          </details>
        </div>
        <div className="work-actions work-proposal-actions" aria-label={catalogFormat("template", "9e9521d6a3b6", [proposal.title])}>
          <button className="chip on" type="button" disabled={!!busy}
            onClick={() => void run(proposal.id, () => resolveWorkV2Proposal(proposal.id, "accept"))}>{catalogWord("inline", "44ae4672eed2")}</button>
          <button className="chip danger" type="button" disabled={!!busy}
            onClick={() => void run(proposal.id, () => resolveWorkV2Proposal(proposal.id, "reject"))}>{catalogWord("inline", "0f7e826a2f9f")}</button>
        </div>
      </li>
    })}</ul></div>
  </details>
}

/** A person's answer is part of its item, immediately after the item's scope. */
function WorkItemDecisions({ decisions, decisionAnswers, waitingOn, busy, answerWorkDecision }: {
  decisions: Decision[]
  decisionAnswers: Record<string, DecisionAnswerStatus>
  /** The decision the item's waiting_user points at, if any. */
  waitingOn?: string
  busy: boolean
  answerWorkDecision: (decisionID: string, optionID: string) => Promise<boolean>
}) {
  const receipts = Object.entries(decisionAnswers).filter(([id, status]) => status.phase === "confirmed" && !decisions.some((decision) => decision.id === id))
  if (!decisions.length && !receipts.length) return null
  return <section className="work-item-decisions" aria-label={catalogWord("inline", "60ec1b4faaf6")}>
    <div className="work-item-decisions-head"><strong>{decisions.length ? catalogWord("literal", "06e9721c22a0") : catalogWord("literal", "da65d95a49fd")}</strong><span>{decisions.length || ""}</span></div>
    {receipts.map(([id, receipt]) => <div className="work-item-decision" key={id} id={`work-decision-${id}`}>
      <h4>{receipt.question}</h4><p className="work-decision-feedback" role="status" aria-live="polite">{catalogWord("inline", "e3de4a9b5b50")}{receipt.label}{closingMark(catalogWord("inline", "e3de4a9b5b50"))}</p>
    </div>)}
    {decisions.map((decision) => {
      const fallback = decision.options.find((option) => option.id === decision.default)?.label ?? decision.default
      const feedback = decisionAnswers[decision.id]
      return <div className="work-item-decision" key={decision.id} data-decision-id={decision.id} id={`work-decision-${decision.id}`}>
        <p className="work-item-decision-state">{decision.id === waitingOn ? catalogWord("literal", "cbc015194d93")
          : decision.blocking ? catalogWord("literal", "b81539b6dfef") : catalogWord("literal", "29feba66a834")}</p>
        <h4>{decision.question}</h4>
        <p className="work-clock">{catalogWord("inline", "b56f36b529db")} {when(decision.due_at)}{fullWidthPunctuation() ? "" : ". "}{catalogWord("inline", "2220fdf0d620")}{fallback}{closingMark(catalogWord("inline", "2220fdf0d620"))}</p>
        <div className="work-actions" role="group" aria-label={catalogFormat("template", "1bc187210168", [decision.question])}>
          {decision.options.map((option) => <button key={option.id} type="button"
            className="chip" disabled={busy || feedback?.phase === "pending"}
            aria-pressed={feedback?.option === option.id && feedback.phase === "pending"}
            onClick={() => void answerWorkDecision(decision.id, option.id)}>{option.label}</button>)}
        </div>
        {feedback && <p className={`work-decision-feedback ${feedback.phase}`} role={feedback.phase === "rejected" ? "alert" : "status"} aria-live="polite">
          {feedback.phase === "pending" ? catalogFormat("template", "34bb2cdffe3e", [feedback.label]) : feedback.message}
        </p>}
      </div>
    })}
  </section>
}

function BoardRegion({ title, items, sessions, nowSeconds, decisions, onOpen }: {
  title: string
  items: WorkV2Item[]
  sessions: SessionRow[]
  nowSeconds: number
  decisions: Decision[]
  onOpen: (item: WorkV2Item) => void
}) {
  if (!items.length) return null
  return <section className="work-section"><div className="work-section-head"><h2>{title}</h2><span className="work-count">{items.length}</span></div>
    <div className="work-cards">{items.map((item) => <CompactWorkCard key={item.id} item={item}
      sessions={sessions} nowSeconds={nowSeconds} decisions={decisionsForWorkItem(decisions, item.id)} onOpen={() => onOpen(item)} />)}</div>
  </section>
}

/**
 * The Board is an index: enough context to choose an item, never the whole
 * item's working surface. The one button avoids nested controls and gives a
 * keyboard and screen-reader user the same route into the shared detail modal.
 */
function CompactWorkCard({ item, sessions, nowSeconds, decisions, onOpen }: {
  item: WorkV2Item
  sessions: SessionRow[]
  nowSeconds: number
  decisions: Decision[]
  onOpen: () => void
}) {
  const epic = isEpic(item)
  const gateShown = epicGateDetailShown(item)
  const attention = needsPerson(item, decisions.length)
  const activePhase = item.phase === "merging" || item.phase === "deploying"
  const family = useContext(EpicFamilyContext)
  const parent = epicParent(item, family.rows)
  // An Agent-made card carries a badge; its sentence is the summary button's
  // description, since the button's own label is explicit.
  const origin = workOrigin(item.created_by)
  const originSentence = originLine(item)
  const gateDescriptionID = `work-card-${item.id}-gate`
  const gateSnapshotDescriptionID = `work-card-${item.id}-gate-snapshot`
  const attentionDescriptionID = `work-card-${item.id}-attention`
  const conditionDescriptionID = `work-card-${item.id}-condition`
  const actionDescriptionID = `work-card-${item.id}-action`
  const nextAction = nextActionWords(item, decisions)
  const progressDescriptionID = `work-card-${item.id}-progress`
  const deploymentDescriptionID = `work-card-${item.id}-deployment`
  const originDescriptionID = `work-card-${item.id}-origin`
  const describedBy = [gateShown && gateSnapshotDescriptionID, gateShown && gateDescriptionID,
    item.condition && conditionDescriptionID, nextAction && actionDescriptionID,
    activePhase && progressDescriptionID, item.phase === "done" && deploymentDescriptionID,
    attention && attentionDescriptionID, originSentence && originDescriptionID].filter(Boolean).join(" ") || undefined
  return <article className={epic ? "work-card work-v2-card work-summary-card work-epic-card" : "work-card work-v2-card work-summary-card"}
    data-work-id={item.id} data-phase={item.phase} data-kind={item.kind} data-origin={originSentence ? origin : undefined} tabIndex={-1}>
    <button className="work-card-summary" type="button" aria-haspopup="dialog"
      aria-label={catalogFormat("template", "1640b97725ed", [item.title, attention ? catalogWord("literal", "0ec51d2f1b3c") : catalogWord("literal", "87a8d926d9e7"), phaseName(item.phase)])} aria-describedby={describedBy} onClick={onOpen}>
      <span className="work-card-summary-top">
        <span className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span title={item.project.label}>{item.project.label}</span></span>
        {originSentence && <span className="work-card-origin" title={originSentence}><AgentGlyph />{workWord("agentMadeBadge")}</span>}
        <span className={epic ? "work-state work-epic-label" : "work-state"}>{KIND_META[item.kind].label} · {phaseName(item.phase)}</span>
      </span>
      <span className="work-card-summary-title">{item.title}</span>
      {attention && <span id={attentionDescriptionID} className="work-card-attention">{catalogWord("inline", "c85c228e97c0")}{decisions.length > 1 ? catalogFormat("template", "b6aa195d5775", [decisions.length]) : ""}</span>}
      {item.condition && <span id={conditionDescriptionID} className="work-card-condition">{conditionWords(item)}</span>}
      {nextAction && <span id={actionDescriptionID} className="work-card-next-action">{nextAction}</span>}
      <span className="work-card-summary-description">{item.description}</span>
      {activePhase && <span id={progressDescriptionID} className="work-card-progress">{phaseStayWords(item.phase_entered_at, nowSeconds)} · {ownerOnlineWords(item.owner_session, sessions)}</span>}
      {item.phase === "done" && <span id={deploymentDescriptionID} className="work-card-deployment">{deploymentWords(item)}</span>}
      {gateShown && <><WorkGateLine item={item} id={gateDescriptionID} />
        <span id={gateSnapshotDescriptionID} className="work-gate-snapshot">{catalogLabel("inline", "947720d10478")}{gateSnapshotText(item.gate_snapshot_cycle, item.planning_gate, item.verify_gate)}</span></>}
      <span className="work-card-summary-foot">
        <span>{item.closed_at ? catalogFormat("template", "966e0c24f6bc", [when(item.closed_at)]) : catalogFormat("template", "d15f0c565e65", [when(item.updated_at)])}</span>
        <span className="work-card-open">{item.decision_id ? catalogWord("literal", "8722de1b1769") : attention ? catalogWord("literal", "b8f8e3ceb51b") : catalogWord("literal", "bfc3ff801ec6")} <WorkIcon name="open" /></span>
      </span>
      {originSentence && <span id={originDescriptionID} className="work-card-origin-sentence">{originSentence}</span>}
    </button>
    {parent && <CardParentLine parent={parent} />}
  </article>
}

function WorkCard({ item, sessions, decisions, decisionAnswers, busy, failure, clearFailure, run, answerWorkDecision, detailLoading, detailError, retryDetail, focusAssignment = false, reportsExpanded = false, foldDescription = false }: {
  item: WorkV2Item
  sessions: SessionRow[]
  decisions: Decision[]
  decisionAnswers: Record<string, DecisionAnswerStatus>
  busy: string
  failure: string
  clearFailure: () => void
  run: (key: string, task: () => Promise<unknown>, refreshInBackground?: boolean) => Promise<boolean>
  answerWorkDecision: (decisionID: string, optionID: string) => Promise<boolean>
  detailLoading: boolean
  detailError: string
  retryDetail: () => void
  focusAssignment?: boolean
  reportsExpanded?: boolean
  foldDescription?: boolean
}) {
  const [terminal, setTerminal] = useState("")
  const [assistant, setAssistant] = useState<Assistant>(() => rememberedAssistant())
  // A new Session has no role until the person chooses one or asks AI.
  const personas = usePersonas()
  const [personaChoice, setPersonaChoice] = useState("")
  const [team, setTeam] = useState(rememberedTeam)
  const persona = personaById(personas, personaChoice)
  const [aiSuggestion, setAISuggestion] = useState<Awaited<ReturnType<typeof suggestPersonaWorkV2>> | null>(null)
  const [aiSuggestionBusy, setAISuggestionBusy] = useState(false)
  const [aiSuggestionFailure, setAISuggestionFailure] = useState("")
  const itemVersion = useRef(item.version)
  itemVersion.current = item.version
  const aiPersona = aiSuggestion?.outcome === "recommend" ? personaById(personas, aiSuggestion.persona_id) : null
  const aiSuggestionID = `work-persona-ai-${item.id}`
  const aiSuggestionOverridden = !!aiPersona && personaChoice !== aiPersona.id
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
          setAISuggestionFailure(catalogWord("literal", "e2d3fee22432"))
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
  const [reviewRequiredFailed, setReviewRequiredFailed] = useState(false)
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
    (plan || item.kind === "epic" || featureLike(item))
  const cardClass = epic ? "work-card work-v2-card work-epic-card"
    : plan ? "work-card work-v2-card work-plan-card" : "work-card work-v2-card"
  const conversionPanel = converting && convertible ? <section id={`work-convert-${item.id}`} className="work-plan-convert"
    aria-label={plan ? catalogWord("literal", "43500bdb3e8e") : catalogFormat("template", "fe204a407a5a", [KIND_META[item.kind].label])}>
    <div><strong>{plan ? catalogWord("literal", "e30e9ddc70f5") : catalogWord("literal", "a2e583309304")}</strong>
      <p>{plan ? catalogWord("literal", "aa49b8a6ba21") : catalogWord("literal", "24f7f6ef66e1")}{catalogWord("inline", "9596ec6c7c96")}</p></div>
    {plan && <fieldset className="work-plan-kind-field"><legend>{catalogWord("inline", "bccb6492eb93")}</legend>
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
        {busy === `convert-${item.id}` ? catalogWord("literal", "729d5136c5cb") : catalogFormat("template", "06453a1153b7", [plan ? KIND_META[conversionKind].label : "Plan"])}</button>
      <button className="chip" type="button" disabled={!!busy} onClick={() => setConverting(false)}>{catalogWord("inline", "2cd0f3be8738")}</button>
    </div>
    {failure && <p className="work-note" role="alert">{catalogLabel("inline", "c82438204056")}{failure}</p>}
  </section> : null
  return <article className={cardClass} data-work-id={item.id}
    data-phase={item.phase} data-kind={item.kind} tabIndex={-1}>
    <div className="work-card-toolbar">
      <div className="work-v2-project"><Mark icon={item.project.icon as SessionRow["icon"]} cellPx={4} /><span title={item.project.label}>{item.project.label}</span></div>
      <div className="work-card-controls" aria-label={catalogWord("inline", "6c459fe37882")}>
        {!!item.owner_session && !item.closed_at && <button type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setReminded(false); void run(`remind-${item.id}`, () => remindWorkV2(item)).then(setReminded) }}>
          <WorkIcon name={reminded ? "check" : "remind"} /> {reminded ? catalogWord("literal", "635c296257d2") : catalogWord("literal", "ca263b730ae8")}
        </button>}
        {reassignable && <button type="button" disabled={!!busy} aria-expanded={reassigning}
          onClick={() => { clearFailure(); setAssignFailed(false); setTerminal(""); setReassigning((shown) => !shown) }}>
          <WorkIcon name="reassign" />{catalogWord("inline", "1ac7cfab4805")}</button>}
        {!item.closed_at && <button type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setCompleting(true) }}><WorkIcon name="check" />{catalogWord("inline", "c0b3fbff51cc")}</button>}
        <button type="button" disabled={!!busy} onClick={() => { clearFailure(); setEditing(true) }}><WorkIcon name="edit" />{catalogWord("inline", "e0d4485966bd")}</button>
        {convertible && !plan && <button type="button" disabled={!!busy} aria-expanded={converting}
          aria-controls={`work-convert-${item.id}`}
          onClick={() => { clearFailure(); setConverting((shown) => !shown) }}>{catalogWord("inline", "827a48d61eb8")}</button>}
        {!item.closed_at && <button className="danger" type="button" disabled={!!busy}
          onClick={() => { clearFailure(); setDeleting(true) }}><WorkIcon name="delete" />{catalogWord("inline", "3c8f5b363ab3")}</button>}
      </div>
    </div>
    {/* The person's override closes the item without the owning Session's
        evidence, so it asks once more, here on the card, before it does. */}
    {completing && !item.closed_at && <div className="work-actions" role="group" aria-label={catalogWord("inline", "5b58c9188a2f")}>
      <p className="work-note">{completeConfirmWords(item)}</p>
      <button className="chip on" type="button" disabled={!!busy} aria-busy={busy === `complete-${item.id}`}
        onClick={() => { void run(`complete-${item.id}`, () => completeWorkV2(item)).then((ok) => { if (ok) setCompleting(false) }) }}>
        <WorkIcon name="check" />{busy === `complete-${item.id}` ? catalogWord("literal", "935d3ef2ee45") : catalogWord("literal", "5a651fa3c7ca")}</button>
      <button className="chip" type="button" disabled={busy === `complete-${item.id}`} onClick={() => setCompleting(false)}>{catalogWord("inline", "2cd0f3be8738")}</button>
      {failure && <p className="work-note" role="alert">{catalogLabel("inline", "a7042be56aeb")}{failure}</p>}
    </div>}
    {epic
      ? <span className="work-state work-epic-label"><b>{catalogWord("inline", "64d13f155730")}</b> · {phaseName(item.phase)}</span>
      : plan ? <span className="work-state work-plan-label"><b>{catalogWord("inline", "589939ed89dd")}</b></span>
      : <span className="work-state">{KIND_META[item.kind].label} · {phaseName(item.phase)}</span>}
    <h3 id={`work-card-title-${item.id}`}>{item.title}</h3>
    <EpicParentLine item={item} />
    <CreatedViaNote item={item} />
    <ClaimedViaNote item={item} />
    {!plan && conversionPanel}
    {foldDescription ? <WorkDescription key={item.id} description={item.description} id={item.id} /> : <p>{item.description}</p>}
    {!epic && !!item.acceptance_criteria?.trim() && <section className="work-item-acceptance" aria-label={catalogWord("work", "acceptanceCriteria")}>
      <h4>{catalogWord("work", "acceptanceCriteria")} <small>{catalogFormat("work", "acceptanceVersion", [item.acceptance_version])}</small></h4>
      <div dangerouslySetInnerHTML={{ __html: L.richTextHTML(item.acceptance_criteria) }} />
    </section>}
    {convertible && plan && <div className="work-convert-entry">
      <button className="work-convert-cta" type="button" disabled={!!busy} aria-expanded={converting}
        aria-controls={`work-convert-${item.id}`}
        onClick={() => { clearFailure(); setConverting((shown) => !shown) }}>{catalogWord("inline", "437b148fabf3")}</button>
    </div>}
    {plan && conversionPanel}
    {epicGateDetailShown(item) && <WorkGateDetail item={item} loading={detailLoading} error={detailError}
      sessions={sessions.filter((session) => !!session.sessionId).map((session) => ({ id: session.sessionId || "", label: session.label || session.sessionId || "Session" }))}
      run={run} retry={retryDetail} />}
    {!epic && <WorkGateAttention item={item}
      sessions={sessions.filter((session) => !!session.sessionId).map((session) => ({ id: session.sessionId || "", label: session.label || session.sessionId || "Session" }))}
      run={run} />}
    <WorkItemDecisions decisions={decisions} decisionAnswers={Object.fromEntries(Object.entries(decisionAnswers).filter(([, answer]) => answer.workID === item.id))} waitingOn={item.decision_id} busy={!!busy} answerWorkDecision={answerWorkDecision} />
    {featureLike(item) && <ReviewRequiredField id={`work-review-required-${item.id}`} checked={item.review_required === true}
      disabled={!!busy || !!item.closed_at} busy={busy === `review-required-${item.id}`}
      onChange={(checked) => { clearFailure(); setReviewRequiredFailed(false)
        void run(`review-required-${item.id}`, () => setWorkV2ReviewRequired(item, checked)).then((ok) => setReviewRequiredFailed(!ok)) }} />}
    {reviewRequiredFailed && failure && <p className="work-note" role="alert">{failure}</p>}
    {epicGateShown(item) && <EpicGateChecklist item={item} />}
    {epic && <EpicChildren item={item} sessions={sessions} />}
    {item.decision_id ? <section className="work-user-action" aria-label={catalogWord("inline", "ee001bfec7fe")}>
      <strong>{catalogWord("inline", "ee001bfec7fe")}</strong>
      <p>{nextActionWords(item, decisions)}{decisionsForWorkItem(decisions, item.id).some((d) => d.id === item.decision_id)
        ? <> · <button type="button" className="chip" onClick={() =>
          document.getElementById(`work-decision-${item.decision_id}`)?.scrollIntoView({ block: "nearest" })}>{catalogWord("inline", "77ba6068fc8e")}</button></> : null}</p>
    </section> : item.user_action && <section className="work-user-action" aria-label={catalogWord("inline", "ee001bfec7fe")}>
      <strong>{catalogWord("inline", "ee001bfec7fe")}</strong><p>{item.user_action}</p>
    </section>}
    {/* Choosing who does the work is what an unassigned card is for, so the
        picker sits under what the work is, above its progress and pictures. */}
    {(assignable || (reassignable && reassigning)) && <div className="work-assignment">
      {epic && <p className="work-epic-assign-note">{catalogWord("inline", "b5e4c6c82f31")}</p>}
      {reassignable && <p className="work-reassign-note">{catalogWord("inline", "58f4894c12b1")}</p>}
      <section className="work-assignment-route" aria-labelledby={`work-assign-existing-${item.id}`}>
        <h4 id={`work-assign-existing-${item.id}`}>{catalogWord("inline", "c8232bd59817")}</h4>
        <SessionAssignmentPicker sessions={eligible} value={terminal} onChange={setTerminal} autoFocus={focusAssignment || (epic && reassigning)} />
        {terminal && <button className="chip on work-assignment-action" type="button" disabled={!!busy} aria-busy={busy === item.id && assigningRoute === "existing"}
          onClick={() => assign("existing", () => assignWorkV2(item, terminal))}>
          {busy === item.id && assigningRoute === "existing" && <span className="work-assignment-spinner" aria-hidden="true" />}
          {busy === item.id && assigningRoute === "existing" ? catalogWord("literal", "c147921b47db") : catalogWord("literal", "d87caeb7f606")}</button>}
      </section>
      <section className="work-assignment-route" aria-labelledby={`work-assign-new-${item.id}`}>
        <h4 id={`work-assign-new-${item.id}`}>{catalogWord("inline", "ae3df530a0ca")}</h4>
        <div className="work-new-session" role="radiogroup" aria-label={catalogWord("inline", "d7c6293edaf2")}>
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
          onClick={() => void askAIForPersona()}>{aiSuggestionBusy ? catalogWord("literal", "29e124139608") : catalogWord("literal", "2bbea1c41037")}</button>}
        {aiSuggestion?.outcome === "recommend" && aiPersona && <p className="work-persona-ai-result" id={aiSuggestionID} role="status">
          <strong>{catalogLabel("inline", "366cbb69bcce")}{personaName(aiPersona)}</strong>
          <span>{aiSuggestionOverridden ? catalogWord("literal", "74a97b8b8227") : catalogWord("literal", "c1083b6ca5e0")}</span>
        </p>}
        {aiSuggestion?.outcome === "ambiguous" && <p className="work-persona-ai-result" id={aiSuggestionID} role="status">
          <strong>{catalogWord("inline", "b4933eb2248e")}</strong><span>{catalogWord("inline", "56eb16ed4470")}</span>
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
        <button className="chip on work-assignment-action" type="button" disabled={!!busy || aiSuggestionBusy} aria-busy={busy === item.id && assigningRoute === "new"}
          onClick={() => assign("new", () => assignNewWorkV2(item, assistant, persona?.id))}>
          {busy === item.id && assigningRoute === "new" && <span className="work-assignment-spinner" aria-hidden="true" />}
          {persona
            ? nextWord(busy === item.id && assigningRoute === "new" ? "personaOpeningSession" : "personaNewSession", { assistant: assistantName(assistant), persona: personaName(persona) })
            : busy === item.id && assigningRoute === "new" ? catalogFormat("template", "ab717b6453b6", [assistantName(assistant)]) : catalogFormat("template", "c8e602657653", [assistantName(assistant)])}</button>
      </section>
      {busy === item.id && <p className="work-assignment-status" role="status">{catalogWord("inline", "cf865cda2b1b")}</p>}
      {reassignable && <button className="chip" type="button" disabled={!!busy} onClick={() => setReassigning(false)}>{catalogWord("inline", "2cd0f3be8738")}</button>}
      {assignFailed && failure && <p className="work-note" role="alert">{failure}</p>}
    </div>}
    <WorkEpicPlanDocuments item={item} />
    <WorkItemDocuments item={item} placement="before_steps" />
    <WorkSteps steps={item.steps} />
    <WorkMilestones phase={item.phase} verifyGate={item.verify_gate} />
    <WorkItemDocuments item={item} placement="after_steps" />
    <WorkCompletionReports item={item} expanded={reportsExpanded} />
    {!!item.images?.length && <div className="work-reference-images" role="group" aria-label={catalogWord("inline", "0d8b8b072dbd")}>
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
        onClick={() => imagePicker.current?.click()}>{catalogWord("inline", "e17357ea2310")}</button>
      <small>{item.images?.length ?? 0} / 6</small>
    </div>}
    <ItemUsageCard itemId={item.id} version={item.version} />
    <div className="work-meta"><span>{conditionWords(item)}</span>
      <span>{item.closed_at ? catalogFormat("template", "966e0c24f6bc", [when(item.closed_at)]) : catalogFormat("template", "d15f0c565e65", [when(item.updated_at)])}</span>
      {owner ? <a className="work-session-link" href={sessionFragment(owner.id)}
        aria-label={catalogFormat("template", "e0059ef8ad57", [item.title])}>{catalogWord("inline", "31d7501ec843")} {owner.label || owner.id}<WorkIcon name="open" /></a>
        : item.owner_session && <span>{catalogWord("inline", "6959b4159575")} {item.owner_session.slice(0, 8)}</span>}
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
      {expanded ? catalogWord("literal", "0e8fa59ff18e") : catalogWord("literal", "7b47ff3dceba")}
    </button>}
  </>
}

function personaAIError(error: unknown): string {
  if (error instanceof RefusalError) {
    switch (error.code) {
      case "ai_consent_required": return catalogWord("literal", "038bac9c971f")
      case "no_persona_suggester": return catalogWord("literal", "de75668d4759")
      case "persona_suggester_out_of_quota": return catalogWord("literal", "d5c34e496d87")
      case "persona_suggestion_failed": return catalogWord("literal", "15266fe5c659")
      case "busy": return catalogWord("literal", "4a77fbfcb87e")
      case "version_conflict": return catalogWord("literal", "bb8e3857f2cf")
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
  const line = workOrigin(item.created_by) === "epic_owner" ? epicOwnerLine(item.created_via) : createdViaLine(item.created_via)
  if (!line) return null
  const excerpt = item.created_via?.excerpt ?? ""
  if (!excerpt) return <p className="work-created-via">{line}</p>
  return <details className="work-created-via">
    <summary title={excerpt}>{line}</summary>
    <blockquote aria-label={workWord("createdViaQuote")}>{excerpt}</blockquote>
  </details>
}

/**
 * Who took this item, when its Session claimed it on the person's message, or
 * which Session handed it to a new Session with which persona: the same small
 * line and quote as CreatedViaNote. An item the person assigned shows nothing
 * here.
 */
function ClaimedViaNote({ item }: { item: WorkV2Item }) {
  const personas = usePersonas()
  const persona = personaById(personas, item.claimed_via?.persona)
  const line = claimedViaLine(item.claimed_via, undefined, persona ? personaName(persona) : undefined)
  if (!line) return null
  const excerpt = item.claimed_via?.excerpt ?? ""
  if (!excerpt) return <p className="work-created-via">{line}</p>
  return <details className="work-created-via">
    <summary title={excerpt}>{line}</summary>
    <blockquote aria-label={workWord("createdViaQuote")}>{excerpt}</blockquote>
  </details>
}

/** Where an Epic, or a Feature that needs independent review, stands against the plan gate before it may start implementing. */
function EpicGateChecklist({ item }: { item: WorkV2Item }) {
  const gate = epicGate(item.documents)
  return <section className="work-epic-gate" aria-label={isEpic(item) ? catalogWord("literal", "dbf828033b91") : catalogWord("literal", "c67e7799cd48")} data-ready={gate.ready ? "" : undefined}>
    <ul>
      <li data-state={gate.plan ? "done" : "open"}><WorkIcon name={gate.plan ? "check" : "circle"} />{catalogWord("inline", "1aaa1e3eda43")}</li>
      <li data-state={gate.review ? "done" : "open"}><WorkIcon name={gate.review ? "check" : "circle"} />{catalogWord("inline", "4f468f5b65d4")}</li>
    </ul>
    {!gate.ready && <p>{planGateHint(item)}</p>}
  </section>
}

interface SessionWorkReading {
  page?: SessionWorkV2
  loading?: boolean
  error?: string
  /** The machine is too old for this read; said instead of `error` (docs/updates.md). */
  update?: MachineNeedsUpdate | null
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
        setReadings((current) => ({ ...current, [session.id]: { error: failureWords(error), update: asMachineNeedsUpdate(error) } }))
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
    <button className="work-session-trigger" type="button" aria-label={catalogWord("inline", "c4bde41c6d80")} aria-haspopup="listbox"
      aria-expanded={open} autoFocus={autoFocus} onClick={() => setOpen((shown) => !shown)}>
      {selected ? <><SessionStateDot session={selected} /><span><b>{selected.label || selected.id}</b><PersonaTag id={selected.persona} personas={personas} />
        <small>{assistantName(selected.assistant)} · {sessionActivityName(selected.state)} · {sessionWorkLabel(selected)}</small></span></>
        : <><span className="work-session-placeholder" aria-hidden="true">◌</span><span>{catalogWord("inline", "b7efeae38921")}</span></>}
      <span className="work-project-chevron" aria-hidden="true">⌄</span>
    </button>
    {open && <div className="work-session-menu" role="listbox" aria-label={catalogWord("inline", "498ac2845b60")}>
      {sessions.map((session) => <SessionChoice key={session.id} session={session} reading={readings[session.id]}
        selected={session.id === value} onChoose={() => choose(session.id)} />)}
      {!sessions.length && <p className="work-project-empty">{catalogWord("inline", "7b9c3fe68d48")}</p>}
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
    <span><b>{session.label || session.id}</b><PersonaTag id={session.persona} personas={personas} /><small>{assistantName(session.assistant)} · {sessionActivityName(session.state)} · {sessionWorkLabel(session)}</small></span>
    <span className="work-session-counts">{reading?.loading ? catalogWord("literal", "58ea8fb4addc") : reading?.update ? nextWord("machineNeedsUpdateShort") : reading?.error ? catalogWord("literal", "40e3c0a7bbd8") : counts
      ? catalogFormat("template", "4ce1db07d816", [counts.board, counts.todos]) : "—"}</span>
  </button>
}

function SessionStateDot({ session }: { session: SessionRow }) {
  return <span className="work-session-state-dot" data-state={session.state} aria-hidden="true" />
}

function SessionAssignmentDetail({ session, reading }: { session: SessionRow; reading?: SessionWorkReading }) {
  const page = reading?.page
  const counts = page ? sessionWorkCounts(page) : null
  return <section className="work-session-detail" aria-label={catalogFormat("template", "ecd25f9c83f5", [session.label || session.id])} aria-live="polite">
    <div className="work-session-detail-head"><strong>{catalogWord("inline", "3f44b79c30e6")}</strong><span>{sessionActivityName(session.state)} · {sessionWorkLabel(session)}</span></div>
    {(session.line || session.work_note) && <p>{session.line || session.work_note}</p>}
    {reading?.loading && !page ? <p>{catalogWord("inline", "926adfdf9989")}</p> : reading?.update ? <NeedsUpdate update={reading.update} /> : reading?.error ? <p className="work-note" role="alert">{catalogLabel("inline", "e85c2c330c2e")}{reading.error}</p> : page ? <>
      <p>{catalogFormat("count", "unfinishedWork", [counts?.board ?? 0, counts?.todos ?? 0])}</p>
      <SessionWorkList title={catalogWord("inline", "7cd8f523be1c")} empty={catalogWord("literal", "4a085d55298e")} rows={page.assigned_items.map((item) => ({
        id: item.id, title: item.title, meta: `${item.project.label} · ${phaseName(item.phase)}${item.condition ? ` · ${conditionWords(item)}` : ""}`,
      }))} />
      <SessionWorkList title={catalogWord("inline", "fc3806d4036b")} empty={catalogWord("literal", "f5520c76f525")} rows={page.direct_todos.map((todo) => ({
        id: todo.id, title: todo.text, meta: todo.read_at ? catalogWord("literal", "d2defbef2b6a") : todo.sent_at ? catalogWord("literal", "a5d037c48e9f") : catalogWord("literal", "1c39ef00f1ce"),
      }))} />
      <SessionWorkList title={catalogWord("inline", "684134665ec0")} empty={catalogWord("literal", "5aa68b48f74d")} rows={(page.recent_items ?? []).map((item) => ({
        id: item.id, title: item.title, meta: catalogFormat("template", "b6b8b9faacfb", [item.project.label, when(item.closed_at)]),
      }))} />
      {page.truncated && <small className="work-session-truncated">{catalogWord("inline", "ee52fbed3fad")}</small>}
    </> : <p>{catalogWord("inline", "1a1328fe795c")}</p>}
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

function CreatedWorkModal({ item, created = true, back, sessions, decisions, decisionAnswers, busy, failure, clearFailure, run, answerWorkDecision, detailLoading, detailError, retryDetail, onClose }: {
  item: WorkV2Item
  created?: boolean
  /** Shared by successive dialogs opened from one another, so focus returns to the first opener. */
  back?: ModalReturn
  sessions: SessionRow[]
  decisions: Decision[]
  decisionAnswers: Record<string, DecisionAnswerStatus>
  busy: string
  failure: string
  clearFailure: () => void
  run: (key: string, task: () => Promise<unknown>) => Promise<boolean>
  answerWorkDecision: (decisionID: string, optionID: string) => Promise<boolean>
  detailLoading: boolean
  detailError: string
  retryDetail: () => void
  onClose: () => void
}) {
  const modal = useRef<HTMLDivElement>(null)
  const initialFocus = useRef<HTMLButtonElement>(null)
  useModalDismiss(false, onClose)
  useModalFocus(modal, initialFocus, back)
  // Opened from a Session too, so it lives beside the app root, never inside
  // the fixed Session pane: a phone then keeps one scroll surface.
  return createPortal(<div ref={modal} tabIndex={-1} className={created ? "session-todo-modal work-created-modal" : "session-todo-modal work-created-modal work-item-detail-modal"}
    role="dialog" aria-modal="true" aria-labelledby={`work-created-title-${item.id} work-card-title-${item.id}`}
    onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <div className={created ? "work-created-panel" : "work-created-panel work-item-detail-panel"}>
      <div className="work-modal-head"><div><p className="board-eyebrow">{created ? catalogWord("ui", "workItemCreated") : catalogWord("ui", "boardItem")}</p>
        <h2 id={`work-created-title-${item.id}`}>{created ? catalogWord("literal", "935ee1ab45ca") : catalogWord("literal", "79a3cb6e5cdb")}</h2></div>
        <button ref={initialFocus} className="work-modal-close" type="button" aria-label={catalogWord("inline", "c7fdddf79eaa")} onClick={onClose}><WorkIcon name="close" /></button></div>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <WorkCard item={item} sessions={sessions} decisions={decisions} decisionAnswers={decisionAnswers} busy={busy} failure={failure} clearFailure={clearFailure} run={run} answerWorkDecision={answerWorkDecision}
        detailLoading={detailLoading} detailError={detailError} retryDetail={retryDetail} focusAssignment={created} reportsExpanded={!created} foldDescription={!created} />
    </div>
  </div>, document.body)
}

/** The person's "Needs independent review" choice on a Feature, with the sentence saying what it costs. */
function ReviewRequiredField({ id, checked, disabled, busy = false, onChange }: {
  id: string
  checked: boolean
  disabled: boolean
  busy?: boolean
  onChange: (checked: boolean) => void
}) {
  return <div className="work-review-required">
    <label htmlFor={id}><input id={id} type="checkbox" checked={checked} disabled={disabled} aria-busy={busy || undefined}
      aria-describedby={`${id}-hint`} onChange={(event) => onChange(event.target.checked)} /> {workWord("reviewRequiredLabel")}</label>
    <small id={`${id}-hint`}>{workWord("reviewRequiredHint")}</small>
  </div>
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
      <div className="work-modal-head"><div><p className="board-eyebrow">{catalogWord("inline", "308278979452")}</p><h2 id={`work-edit-title-${item.id}`}>{catalogWord("inline", "9293ae04361c")}</h2></div>
        <button className="work-modal-close" type="button" aria-label={catalogWord("inline", "c7fdddf79eaa")} disabled={busy} onClick={onClose}><WorkIcon name="close" /></button></div>
      <label>{catalogWord("inline", "6fe38ed1ee10")}<input className="work-input" value={title} maxLength={240} autoFocus onChange={(event) => setTitle(event.target.value)} /></label>
      <VoiceTextarea label={catalogWord("literal", "8561515b8b34")} value={description} maxLength={65536} onValue={setDescription} />
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>{busy ? catalogWord("literal", "21aa64dd7446") : catalogWord("literal", "9a8097d8f563")}</button>
        <button className="chip" type="button" disabled={busy} onClick={onClose}>{catalogWord("inline", "2cd0f3be8738")}</button></div>
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
      <div className="work-modal-head"><div><p className="board-eyebrow">{catalogWord("inline", "2f1f9c448f96")}</p><h2 id={`work-delete-title-${item.id}`}>{catalogWord("inline", "4d05a8ee3b50")}</h2></div>
        <button className="work-modal-close" type="button" aria-label={catalogWord("inline", "c7fdddf79eaa")} disabled={busy} onClick={onClose}><WorkIcon name="close" /></button></div>
      <p><strong>{item.title}</strong>{catalogWord("inline", "8e71e614ab8e")}</p>
      {failure && <p className="work-note" role="alert">{failure}</p>}
      <div className="work-actions"><button className="chip danger" type="submit" disabled={busy}>{busy ? catalogWord("literal", "a5df1fce3ed1") : catalogWord("literal", "2f1a94aefc74")}</button>
        <button className="chip" type="button" disabled={busy} onClick={onClose}>{catalogWord("inline", "ec3a32317a03")}</button></div>
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

/** Where focus goes back to when a dialog closes, and the pending move there. */
interface ModalReturn {
  opener: HTMLElement | null
  timer: number | null
}

/** Keep the keyboard in the modal and put it back on the summary that opened it. */
function useModalFocus(container: RefObject<HTMLDivElement | null>, initialFocus: RefObject<HTMLElement | null>, shared?: ModalReturn) {
  // StrictMode runs an effect's setup/cleanup/setup sequence once in
  // development. Keep the opener across that rehearsal and cancel its false
  // restoration when the second setup starts. A dialog replaced by the next
  // item's dialog is the same rehearsal: the new one cancels the restoration
  // and keeps the first opener, because `shared` outlives both.
  const own = useRef<ModalReturn>({ opener: null, timer: null })
  const back = shared ?? own.current
  if (!back.opener && document.activeElement instanceof HTMLElement) back.opener = document.activeElement
  useEffect(() => {
    if (back.timer !== null) { window.clearTimeout(back.timer); back.timer = null }
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
      back.timer = window.setTimeout(() => {
        const opener = back.opener
        back.timer = null; back.opener = null
        if (opener?.isConnected) opener.focus({ preventScroll: true })
      }, 0)
    }
  }, [container, initialFocus, back])
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
      ? <button className="work-reference-open" type="button" disabled={!!busy || opening} aria-label={catalogFormat("template", "337439125770", [image.title])}
        title={catalogWord("inline", "cf3c6f547bd1")} onClick={mark}>
        <img src={source} alt={image.title} width={image.width} height={image.height} />
      </button>
      : <a href={full || source} target="_blank" rel="noreferrer" aria-label={catalogFormat("template", "37ba92bd09dc", [image.title])} onClick={openFull}>
        <img src={source} alt={image.title} width={image.width} height={image.height} />
      </a> : <div className="work-reference-loading" role={failed ? "alert" : undefined}>{failed || catalogWord("literal", "7ae221f38b02")}</div>}
    <figcaption title={image.title} role={fullFailed ? "alert" : undefined}>{fullFailed || image.title}</figcaption>
    {marking && full && <PictureMarkup picture={{ id: image.id, url: full }} onCancel={() => setMarking(false)} onSave={replaceWithMarks} />}
    {!item.closed_at && <button className="work-reference-remove" type="button" aria-label={catalogFormat("template", "da1cebd6b045", [image.title])} disabled={!!busy}
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
    <button className="work-project-trigger" type="button" aria-label={catalogWord("inline", "985959785319")} aria-haspopup="listbox"
      aria-expanded={open} onClick={toggle}>
      {selected ? <Mark icon={selected.icon as SessionRow["icon"]} cellPx={3} /> : <span className="work-project-placeholder" aria-hidden="true">▦</span>}
      <span>{selected?.label || (allowAll ? catalogWord("literal", "b2ee45d95857") : catalogWord("literal", "ef8fe1c8ac54"))}</span>
      <span className="work-project-chevron" aria-hidden="true">⌄</span>
    </button>
    {open && <div className="work-project-menu" role="listbox" aria-label={catalogWord("inline", "985959785319")}>
      {allowAll && <button type="button" role="option" aria-selected={!value} className="work-project-option"
        onClick={() => choose("")}><span className="work-project-placeholder" aria-hidden="true">▦</span><span>{catalogWord("inline", "24926b27517e")}</span></button>}
      {places.map((place) => <button type="button" role="option" aria-selected={place.id === value}
        className="work-project-option" key={place.id} onClick={() => choose(place.id)}>
        <Mark icon={place.icon as SessionRow["icon"]} cellPx={3} /><span>{place.label}</span>
        {place.id === value && <span className="work-project-check"><WorkIcon name="check" /></span>}
      </button>)}
      {!places.length && <p className="work-project-empty">{refreshing ? catalogWord("literal", "eec20299e1b1") : catalogWord("literal", "dc9a2139ef07")}</p>}
    </div>}
  </div>
}

function NewWorkModal({ places, initialProject, initialDraft, busy, failure, onRefreshPlaces, onClose, onCreate }: { places: ProjectPlace[]; initialProject: string; initialDraft: NewWorkItemDraft; busy: boolean; failure: string; onRefreshPlaces: () => Promise<void>; onClose: () => void; onCreate: (body: Parameters<typeof createWorkV2>[0], images: File[], decisionKey: string) => void }) {
  const [projectID, setProjectID] = useState(initialDraft.projectID || initialProject)
  const [kind, setKind] = useState<WorkV2Kind>(initialDraft.kind || "feature")
  const [title, setTitle] = useState(initialDraft.title || "")
  const [description, setDescription] = useState(initialDraft.description || "")
  const [reviewRequired, setReviewRequired] = useState(false)
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
    const body = { project_id: projectID, kind, title: title.trim(), description: description.trim(), deployment_policy: "agent_decides" as const,
      ...(featureLike({ kind }) ? { review_required: reviewRequired } : {}) }
    const decision = workV2CreateDecision(body, createDecision.current)
    createDecision.current = decision
    onCreate(body, images, decision.key)
  }}>
    <div className="work-modal-head"><div><p className="board-eyebrow">{reviewingDraft ? catalogWord("ui", "reviewWorkItem") : catalogWord("ui", "newWorkItem")}</p><h2 id="work-new-v2-title">{reviewingDraft ? catalogWord("literal", "30947e72b04b") : catalogWord("literal", "e2956f80c3ac")}</h2></div>
      <button className="work-modal-close" type="button" aria-label={catalogWord("inline", "c7fdddf79eaa")} disabled={busy} onClick={onClose}><WorkIcon name="close" /></button></div>
    {reviewingDraft && <p className="work-note">{catalogWord("inline", "a2d091d25a73")}</p>}
    <div className="work-modal-field"><span>{catalogWord("inline", "985959785319")}</span><ProjectPicker places={projectPlaces} value={projectID} onChange={setProjectID} onOpen={onRefreshPlaces} /></div>
    <fieldset className="work-kind-field"><legend>{catalogWord("inline", "1588dd8c9e73")}</legend><div className="work-kind-list">
      {KINDS.map((value) => { const meta = KIND_META[value]; return <button key={value} type="button" className="work-kind-option"
        aria-pressed={kind === value} onClick={() => setKind(value)}><span className="work-kind-icon" aria-hidden="true">{meta.icon}</span>
        <span><b>{meta.label}</b><small>{catalogWord("literal", meta.description)}</small></span><span className="work-kind-radio"><WorkIcon name={kind === value ? "radio" : "circle"} /></span></button> })}
    </div></fieldset>
    <label>{catalogWord("inline", "6fe38ed1ee10")}<input className="work-input" value={title} maxLength={240} onChange={(e) => setTitle(e.target.value)} /></label>
    <VoiceTextarea label={catalogWord("literal", "8561515b8b34")} value={description} onValue={setDescription} />
    {featureLike({ kind }) && <ReviewRequiredField id="work-new-review-required" checked={reviewRequired} disabled={busy} onChange={setReviewRequired} />}
    <PendingPictures images={images} busy={busy} note={catalogWord("literal", "983dac4efa86")} onChange={setImages} />
    {failure && <p className="work-note" role="alert">{failure}</p>}
    <div className="work-actions"><button className="chip on" type="submit" disabled={busy || !ready}>{busy ? catalogWord("literal", "aca2a4fc28ff") : catalogWord("literal", "c5d8aaa266d8")}</button><button className="chip" type="button" disabled={busy} onClick={onClose}>{catalogWord("inline", "2cd0f3be8738")}</button></div>
  </form></div>
}
