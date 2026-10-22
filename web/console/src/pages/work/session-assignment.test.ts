import assert from "node:assert/strict"
import test from "node:test"
import type { SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"
// @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
import { ASSIGNMENT_SETTLE_MS, SessionListReader, readSessionListWith, sessionListFailureCode, assignmentCandidates, assignmentChoiceRows, assignmentChoices, assignmentChoicesWords, assistantName, sessionListComplete, awaitsAcceptance, rememberAssistant, rememberedAssistant, sessionActivityName, sessionWorkCounts, sessionWorkLabel, sessionWorkStateName } from "./session-assignment.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"

withCatalog("zh-Hant")

test("assignment choices distinguish live activity from unreadable state", () => {
  assert.equal(sessionActivityName("working"), "Working")
  assert.equal(sessionActivityName("idle"), "Idle")
  assert.equal(sessionActivityName("unknown"), "狀況不明")
})

test("assignment choices explain what an otherwise idle Session needs", () => {
  assert.equal(sessionWorkStateName("ready" as SessionRow["work_state"]), "可接工作")
  assert.equal(sessionWorkStateName("waiting_you" as SessionRow["work_state"]), "等你回應")
  assert.equal(sessionWorkStateName("milestone_complete" as SessionRow["work_state"]), "待驗收")
})

test("unfinished counts include open Board items, their pending steps, and direct to-dos", () => {
  const page = {
    assigned_items: [{ id: "board-a", steps: [{ done: false }, { done: true }] }, { id: "board-b" }],
    direct_todos: [{ id: "todo-a" }],
    recent_items: [{ id: "done-a" }, { id: "done-b" }],
  } as SessionWorkV2
  assert.deepEqual(sessionWorkCounts(page), { board: 2, todos: 2, unfinished: 4 })
})

test("assignment choices name which company's assistant each Session runs", () => {
  assert.equal(assistantName("claude"), "Claude Code")
  assert.equal(assistantName("codex"), "Codex")
  assert.equal(assistantName("antigravity"), "Antigravity")
  assert.equal(assistantName(undefined), "助理不明")
})

test("a new Session keeps the assistant last chosen, and Codex before any choice", () => {
  const kept = new Map<string, string>()
  const storage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v) }
  assert.equal(rememberedAssistant(storage), "codex")
  rememberAssistant("claude", storage)
  assert.equal(rememberedAssistant(storage), "claude")
  rememberAssistant("antigravity", storage)
  assert.equal(rememberedAssistant(storage), "antigravity")
  kept.set("clawdline.work.new-session-assistant", "gemini")
  assert.equal(rememberedAssistant(storage), "codex")
  const blocked = { getItem: () => { throw new Error("blocked") }, setItem: () => { throw new Error("blocked") } }
  assert.equal(rememberedAssistant(blocked), "codex")
  assert.doesNotThrow(() => rememberAssistant("claude", blocked))
})

test("an owned item can be handed to any other Session in its Project, never back to its owner", () => {
  const row = (id: string, sessionId: string, cwd = "/p") => ({ id, sessionId, cwd }) as SessionRow
  const sessions = [row("%1", "owner"), row("%2", "other"), row("%3", ""), row("%4", "elsewhere", "/q")]
  const item = { project: { path: "/p" }, owner_session: "owner" }
  assert.deepEqual(assignmentCandidates(sessions, item).map((s) => s.id), ["%2"])
  assert.deepEqual(assignmentCandidates(sessions, { ...item, owner_session: null }).map((s) => s.id), ["%1", "%2"])
})

const emptySentence = "這個 Project 目前沒有可用的 Session。"
const incompleteSentence = "Session 清單還沒讀完整，這個 Project 的 Session 可能還不在裡面。"
const loadingSentence = "正在讀取 Session 清單…"
const candidate = (id: string, sessionId: string | undefined, cwd: string | undefined = "/p", extra: Partial<SessionRow> = {}) =>
  ({ id, sessionId, cwd, ...extra }) as SessionRow
const project = { project: { path: "/p" }, owner_session: null }
type Reading = Parameters<typeof assignmentChoices>[0]
/** A complete or partial answer in which every row was first listed just now. */
const ready = (complete: boolean, sessions: SessionRow[], settling = sessions.map((s) => s.id)): Reading =>
  ({ phase: "ready", complete, settling })
const said = (reading: Reading, sessions: SessionRow[]) =>
  assignmentChoicesWords(assignmentChoices(reading, sessions, project))

test("a Session list still being read never says the Project has no Session", () => {
  assert.equal(said({ phase: "loading" }, []), loadingSentence)
  assert.notEqual(said({ phase: "loading" }, []), emptySentence)
  // Rows kept from an earlier reading do not make a pending one an answer.
  assert.equal(said({ phase: "loading" }, [candidate("%1", "s1", "/q")]), loadingSentence)
})

test("a failed Session read says so and offers to read again", () => {
  const choices = assignmentChoices({ phase: "failed", error: "逾時" }, [], project)
  assert.equal(choices.phase, "failed")
  assert.equal(assignmentChoicesWords(choices), "讀不到 Session 清單：逾時")
  // Rows from an earlier reading stay choosable beside the failure.
  const kept = assignmentChoices({ phase: "failed", error: "逾時" }, [candidate("%1", "s1")], project)
  assert.deepEqual(assignmentChoiceRows(kept).map((s) => s.id), ["%1"])
})

test("only a complete answer with no candidate says the Project has no Session", () => {
  assert.equal(said(ready(true, []), []), emptySentence)
  const elsewhere = [candidate("%1", "s1", "/q")]
  assert.equal(said(ready(true, elsewhere), elsewhere), emptySentence)
  const here = [candidate("%1", "s1")]
  const found = assignmentChoices(ready(true, here), here, project)
  assert.equal(found.phase, "candidates")
  assert.equal(assignmentChoicesWords(found), null)
  assert.deepEqual(assignmentChoiceRows(found).map((s) => s.id), ["%1"])
})

test("a partial list, or a row still settling without its cwd or conversation, is incomplete rather than empty", () => {
  assert.equal(said(ready(false, []), []), incompleteSentence)
  const unnamed = [candidate("%1", undefined)]
  assert.equal(said(ready(true, unnamed), unnamed), incompleteSentence)
  const placeless = [candidate("%1", "s1", "")]
  assert.equal(said(ready(true, placeless), placeless), incompleteSentence)
  // The item's own owner cannot be offered, so lacking a cwd it hides nothing.
  const owner = [candidate("%1", "owner", "")]
  assert.equal(assignmentChoicesWords(assignmentChoices(ready(true, owner), owner, { ...project, owner_session: "owner" })), emptySentence)
  // Candidates already found are still offered while the list settles.
  const here = [candidate("%1", "s1")]
  const partial = assignmentChoices(ready(false, here), here, project)
  assert.deepEqual([partial.phase, assignmentChoiceRows(partial).map((s) => s.id)], ["incomplete", ["%1"]])
})

test("a row that will never be named stops holding the list open once it has settled", () => {
  const unnamed = [candidate("%1", undefined), candidate("%2", "s2", "")]
  // Listed for longer than the settle window: the list is an answer again.
  assert.equal(said(ready(true, unnamed, []), unnamed), emptySentence)
  // A row the machine said it cannot name is an answer from the start.
  const unnameable = [candidate("%1", undefined, "/p", { identity: "ambiguous" }), candidate("%2", undefined, "/p", { identity: "unreadable" })]
  assert.equal(said(ready(true, unnameable), unnameable), emptySentence)
  // One nobody has typed into yet may still name itself while it is new.
  const fresh = [candidate("%1", undefined, "/p", { identity: "no_record" })]
  assert.equal(said(ready(true, fresh), fresh), incompleteSentence)
})

test("a Session in a subdirectory or linked worktree of the Project is offered by the daemon's project key", () => {
  const rows = [
    candidate("%1", "s1", "/p/.worktrees/feature", { project_key: "/p" }),
    candidate("%2", "s2", "/p/web", { project_key: "/p" }),
    candidate("%3", "s3", "/other", { project_key: "/other" }),
    // A daemon too old to send the key is compared by its cwd, as before.
    candidate("%4", "s4", "/p"),
    candidate("%5", "s5", "/p/sub"),
  ]
  assert.deepEqual(assignmentCandidates(rows, project).map((s) => s.id), ["%1", "%2", "%4"])
  // A worktree row with no conversation yet keeps the list open only for this Project.
  const settlingElsewhere = [candidate("%1", undefined, "/q/.wt", { project_key: "/q" })]
  assert.equal(said(ready(true, settlingElsewhere), settlingElsewhere), emptySentence)
  const settlingHere = [candidate("%1", undefined, "/p/.wt", { project_key: "/p" })]
  assert.equal(said(ready(true, settlingHere), settlingHere), incompleteSentence)
})

test("a scan may say none only as the daemon's emptyAuthoritative does: complete, with no source turned off", () => {
  const source = (complete: boolean, disabled?: boolean) => ({ complete, disabled }) as NonNullable<Parameters<typeof sessionListComplete>[0]["sources"]>[number]
  assert.equal(sessionListComplete({ complete: true }), true)
  assert.equal(sessionListComplete({ complete: true, sources: [source(true)] }), true)
  // Cloud's relay snapshot names no sources and says complete only once every machine's list arrived.
  assert.equal(sessionListComplete({ complete: false }), false)
  // A source turned off was not looked at; the daemon says the same of its empty list.
  assert.equal(sessionListComplete({ complete: false, sources: [source(true), source(false, true)] }), false)
  assert.equal(sessionListComplete({ complete: true, sources: [source(true), source(false, true)] }), false)
  assert.equal(sessionListComplete({ complete: false, sources: [source(true), source(false)] }), false)
})

/** A Session read the test answers by hand, in any order. */
function pendingReads() {
  const calls: { resolve: (answer: { sessions: SessionRow[]; scan: { complete: boolean } }) => void; reject: (error: unknown) => void }[] = []
  const read = () => new Promise<{ sessions: SessionRow[]; scan: { complete: boolean } }>((resolve, reject) => { calls.push({ resolve, reject }) })
  return { calls, read }
}
const flush = () => new Promise((resolve) => setImmediate(resolve))
// A read answered out of order is awaited by hand; one that never settles is a failure, not a hang.
const settled = { timeout: 2_000 }

function reader(read: () => Promise<{ sessions: SessionRow[]; scan: { complete: boolean } }>, now = () => 0) {
  const published: Reading[] = []
  const recovered: string[] = []
  const it = new SessionListReader({ read, now, words: (error) => String(error),
    publish: (state) => { published.push(state.reading) }, recovered: (notice) => { recovered.push(notice) } })
  return { it, published, recovered }
}

test("before its first answer the Board's Session list is loading, never an empty answer", () => {
  const { it } = reader(pendingReads().read)
  assert.deepEqual(it.state.reading, { phase: "loading" })
  assert.equal(assignmentChoicesWords(assignmentChoices(it.state.reading, it.state.sessions, project)), loadingSentence)
})

test("a failed Session read shows the failure and keeps the rows it had", settled, async () => {
  const reads = pendingReads()
  const { it } = reader(reads.read)
  void it.refresh()
  reads.calls[0].resolve({ sessions: [candidate("%1", "s1")], scan: { complete: true } })
  await flush()
  const outcome = it.refresh()
  reads.calls[1].reject("逾時")
  assert.deepEqual(await outcome, { applied: true, error: "逾時" })
  assert.deepEqual(it.state.reading, { phase: "failed", error: "逾時" })
  assert.deepEqual(it.state.sessions.map((s) => s.id), ["%1"])
})

test("read again asks for the Session list once, says loading at once, and clears the failure notice it answers", settled, async () => {
  const reads = pendingReads()
  const { it, published, recovered } = reader(reads.read)
  void it.refresh()
  reads.calls[0].reject("逾時")
  await flush()
  it.retry()
  assert.equal(reads.calls.length, 2, "the retry is one Session read and nothing else")
  assert.deepEqual(published.at(-1), { phase: "loading" })
  reads.calls[1].resolve({ sessions: [], scan: { complete: true } })
  await flush()
  assert.deepEqual(it.state.reading, { phase: "ready", complete: true, settling: [] })
  assert.deepEqual(recovered, ["逾時"])
})

test("an older answer never replaces a newer one, whichever arrives last", settled, async () => {
  // The 30-second load starts first and fails late; the retry started after it already answered.
  const reads = pendingReads()
  const { it, recovered } = reader(reads.read)
  const load = it.refresh()
  it.retry()
  reads.calls[1].resolve({ sessions: [candidate("%1", "s1")], scan: { complete: true } })
  await flush()
  reads.calls[0].reject("逾時")
  assert.deepEqual(await load, { applied: false })
  assert.equal(it.state.reading.phase, "ready")
  assert.deepEqual(recovered, [])
  // And the other way: a retry's late failure does not hide a newer load's list.
  const late = it.retry // as the picker calls it: unbound
  late()
  const fresh = it.refresh()
  reads.calls[3].resolve({ sessions: [candidate("%2", "s2")], scan: { complete: true } })
  await fresh
  reads.calls[2].reject("逾時")
  await flush()
  assert.deepEqual(it.state.sessions.map((s) => s.id), ["%2"])
  assert.equal(it.state.reading.phase, "ready")
  // Nor does an older list that succeeds late.
  const slow = it.refresh()
  it.retry()
  reads.calls[5].resolve({ sessions: [candidate("%3", "s3")], scan: { complete: true } })
  await flush()
  reads.calls[4].resolve({ sessions: [candidate("%4", "s4")], scan: { complete: false } })
  assert.deepEqual(await slow, { applied: false })
  assert.deepEqual(it.state.sessions.map((s) => s.id), ["%3"])
  assert.deepEqual(it.state.reading, { phase: "ready", complete: true, settling: ["%3"] })
})

test("a row is settling only for the settle window after it was first listed", settled, async () => {
  let clock = 0
  const reads = pendingReads()
  const { it } = reader(reads.read, () => clock)
  const rows = [candidate("%1", undefined)]
  void it.refresh(); reads.calls[0].resolve({ sessions: rows, scan: { complete: true } }); await flush()
  assert.deepEqual(it.state.reading, { phase: "ready", complete: true, settling: ["%1"] })
  clock = ASSIGNMENT_SETTLE_MS
  void it.refresh(); reads.calls[1].resolve({ sessions: [...rows, candidate("%2", undefined)], scan: { complete: true } }); await flush()
  assert.deepEqual(it.state.reading, { phase: "ready", complete: true, settling: ["%2"] })
  assert.equal(assignmentChoicesWords(assignmentChoices(it.state.reading, it.state.sessions, project)), incompleteSentence)
  // A row that left and came back is new again.
  void it.refresh(); reads.calls[2].resolve({ sessions: [], scan: { complete: true } }); await flush()
  clock = 2 * ASSIGNMENT_SETTLE_MS
  void it.refresh(); reads.calls[3].resolve({ sessions: rows, scan: { complete: true } }); await flush()
  assert.deepEqual(it.state.reading, { phase: "ready", complete: true, settling: ["%1"] })
})

test("a reported turn reads 本輪已回報 until an owned item awaits acceptance, and says when the Board was unread", () => {
  const reported = (acceptance?: SessionRow["acceptance"], work_state = "milestone_complete") => ({
    work_state: work_state as SessionRow["work_state"],
    disposition: { scope: "session", evidence: "authenticated_session_delivery", title: "summary" },
    acceptance,
  })
  assert.equal(sessionWorkLabel(reported({ state: "none" })), "本輪已回報")
  // A daemon that sends no answer has not said there is none.
  assert.equal(sessionWorkLabel(reported(undefined)), "本輪已回報 · 讀不到是否有待驗收項目")
  assert.equal(sessionWorkLabel(reported({ state: "pending", work_id: "w", title: "Ship it", phase: "deploying", count: 1 })), "待驗收 · Ship it")
  assert.equal(sessionWorkLabel(reported({ state: "pending", work_id: "w", title: "Ship it", phase: "done", count: 3 })), "待驗收 · Ship it · 共 3 項")
  assert.equal(sessionWorkLabel(reported({ state: "unknown" })), "本輪已回報 · 讀不到是否有待驗收項目")
  // A pending answer that names nothing is not trusted as one.
  assert.equal(sessionWorkLabel(reported({ state: "pending" })), "本輪已回報 · 讀不到是否有待驗收項目")
  assert.equal(sessionWorkLabel(reported({ state: "none" }, "work_complete")), "工作已完成")
  assert.equal(sessionWorkLabel(reported({ state: "pending", title: "Ship it" }, "work_complete")), "待驗收 · Ship it")
  // A task's delivery and an unfinished Session keep their own words.
  assert.equal(sessionWorkLabel({ work_state: "milestone_complete", disposition: { scope: "task", evidence: "authenticated_task_delivery", title: "t" },
    acceptance: { state: "pending", title: "Ship it" } }), "待驗收")
  assert.equal(sessionWorkLabel({ work_state: "working", acceptance: { state: "pending", title: "Ship it" } }), "執行中")
})

test("only deploying and done items wait for the person to look", () => {
  assert.equal(awaitsAcceptance("deploying"), true)
  assert.equal(awaitsAcceptance("done"), true)
  assert.equal(awaitsAcceptance("verifying"), false)
  assert.equal(awaitsAcceptance("cancelled"), false)
})

test("a failed Session list read is reported with its code and how long it took", settled, async () => {
  const reads = pendingReads()
  const failures: unknown[] = []
  let t = 1000
  const it = new SessionListReader({ read: reads.read, now: () => t, words: String, publish: () => {},
    failed: (failure) => { failures.push(failure) } })
  const outcome = it.refresh()
  t = 16_000
  reads.calls[0].reject(Object.assign(new Error("GET /v1/sessions did not complete"), { name: "TransportError", cause: new DOMException("Timed out", "AbortError") }))
  await outcome
  assert.deepEqual(failures, [{ code: "timeout", elapsedMs: 15_000 }])
})

test("a Session list failure code is the refusal's code or what kind of failure, never its message", () => {
  assert.equal(sessionListFailureCode(Object.assign(new Error("x"), { code: "cloud_reconnecting" })), "cloud_reconnecting")
  assert.equal(sessionListFailureCode(new DOMException("Timed out", "AbortError")), "timeout")
  assert.equal(sessionListFailureCode(Object.assign(new Error("/private/path"), { name: "TransportError" })), "transport")
  assert.equal(sessionListFailureCode(null), "unknown")
})

test("the Board's Session read asks for the held list and waits for the reader's late answer", async () => {
  const asked: unknown[][] = []
  await readSessionListWith(async (...args) => { asked.push(args); return null })
  assert.equal(asked.length, 1)
  const [path, init, timeoutMs, answerMs] = asked[0] as [string, RequestInit, number, number]
  assert.equal(path, "/v1/sessions")
  // Without the header the relay reader treats the Board's poll as a manual
  // refresh and waits for a pass that an unchanged Session set never makes.
  assert.equal(new Headers(init.headers).get("X-Clawdline-Initial-Read"), "1")
  assert.ok(answerMs > 0, "the list the reader hands over at the deadline is taken, not dropped")
  assert.ok(timeoutMs > 0)
})
