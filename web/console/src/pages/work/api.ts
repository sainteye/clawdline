import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import type { SessionsSnapshot } from "@clawdline/contract"

/**
 * The board's routes as this page reads them (internal/transport/http/work.go,
 * proposals.go, todos.go). The shapes are the wire's, field for field; nothing
 * here is derived, so what the page shows is what the daemon answered.
 *
 * A person's command goes to `/v1/work/*` and nowhere else. Every one carries
 * an Idempotency-Key, minted once per decision: a retry after the connection
 * dropped reuses it, so the daemon answers the first attempt's outcome rather
 * than carrying the decision out twice (D03).
 */

export type Section = "decide" | "active" | "scheduled" | "done"
export const SECTIONS: Section[] = ["decide", "active", "scheduled", "done"]

export interface TaskCounts {
  total: number
  live: number
  landed: number
  delivered: number
  failed: number
}

export interface Derived {
  state: string
  reason: string
  landed: boolean
  tasks: TaskCounts
  last_evidence_at: number | null
  stall_at: number | null
  closure_due_at: number | null
}

export interface Item {
  id: string
  project: string
  title: string
  acceptance: string | null
  created_at: number
  created_by: string
  place: "board" | "backlog" | "todo"
  state: string | null
  owner: string | null
  commitment: string | null
  start_on: string | null
  rank: number | null
  closed_reason: string | null
  closed_at: number | null
  placed_at: number
  version: number
  section: Section | null
  derived: Derived
  unknown_tasks: number
}

export interface Sweep {
  at: number | null
  tick_seconds: number
  stalled: boolean
  error?: string
}

export interface BoardPage {
  ok: boolean
  counts: Record<Section, number>
  rows: Item[]
  next_cursor: string | null
  page_size: number
  truncated: boolean
  sweep: Sweep
}

export interface BacklogPage {
  ok: boolean
  counts: { planned: number; dropped: number }
  rows: Item[]
  next_cursor: string | null
  page_size: number
}

export interface Proposal {
  id: string
  work_id: string
  /**
   * The dispatch the proposal names. For a line of work it is that line's
   * first task; for a leftover it is only provenance — which delivery said it
   * did not do this — and the subject is a line nobody has dispatched
   * anything for (PT-9). The two must not read the same on a card.
   */
  task_id: string | null
  session_id: string
  source: string
  project: string
  title: string
  signals: string[]
  effects: string[]
  /** Current to-do evidence the sweep can use to re-decide this proposal. */
  subject_status: "unknown" | "owed" | "settled"
  state: string
  created_at: number
  expires_at: number
  /** Which fact about the subject ended the question; null while it is one. */
  withdrawn_reason: string | null
  withdrawn_at: number | null
  /** Checked conclusion and source for an evidence-backed resolution. */
  resolution: string | null
  resolution_evidence: string | null
  resolved_by: string | null
  resolved_at: number | null
}

export interface ProposalPage {
  counts: Record<string, number>
  rows: Proposal[]
  next_cursor: string | null
}

export interface Decision {
  id: string
  session_id: string
  work_id: string | null
  project: string | null
  question: string
  options: { id: string; label: string }[]
  default: string
  blocking: boolean
  state: string
  created_at: number
  due_at: number
}

export interface DecisionPage {
  counts: Record<string, number>
  rows: Decision[]
  next_cursor: string | null
}

export interface DigestSection {
  total: number
  lines: { work_id?: string; id?: string; title: string; what: string; at: number }[]
}

export interface DigestBody {
  completed: DigestSection
  landed: number
  stalled: DigestSection
  from_backlog: DigestSection
  automatic: DigestSection
  moves_truncated: boolean
  proposals_pending: number
  proposals_expired: DigestSection
  decisions_open: number
  decisions_defaulted: DigestSection
  awaiting_closure: number
  closure_asked: DigestSection
  handed_off_todos: number
  backlog_stale: DigestSection
}

export interface Digest {
  key: string
  kind: string
  from: number
  to: number
  created_at: number
  body: DigestBody
}

export interface Todo {
  id: string
  origin: string
  task_id: string
  work_id: string | null
  title: string
  project: string
  state: string
  reason: string
  landing_state: string | null
  created_at: number
  updated_at: number
  closed_at: number | null
  escalation: string[]
}

export interface TodoPage {
  session_id: string
  state: string
  counts: Record<string, number>
  todos: Todo[]
  next_cursor: string | null
}

export interface ProjectPlace {
  id: string
  label: string
  path: string
  icon?: unknown
}

export interface ProjectPlacePage {
  places: ProjectPlace[]
}

async function call<T>(path: string, init: RequestInit = {}): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15_000)
  let res: Response
  try {
    res = await fetch(path, { credentials: "same-origin", ...init, signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as T
}

function query(params: Record<string, string | undefined>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v) q.set(k, v)
  const s = q.toString()
  return s ? "?" + s : ""
}

export const readBoard = (project?: string, cursor?: string) =>
  call<BoardPage>("/v1/work/board" + query({ project, cursor }))
export const readBacklog = (project?: string, cursor?: string) =>
  call<BacklogPage>("/v1/work/backlog" + query({ project, cursor }))
export const readProposals = (project?: string) => call<ProposalPage>("/v1/work/proposals" + query({ project }))
export const readDecisions = () => call<DecisionPage>("/v1/work/decisions")
export const readDigests = () => call<{ rows: Digest[] }>("/v1/work/digests?kind=daily")
/** The machine's real project directory: existing places it recognizes, newest first (at most forty). */
export const readProjectPlaces = () => call<ProjectPlacePage>("/v1/places")
export const readTodos = (sessionRowId: string, state?: "outstanding" | "closed" | "all") =>
  call<TodoPage>(`/v1/sessions/${encodeURIComponent(sessionRowId)}/todos` + query({ state }))

/**
 * A fresh Idempotency-Key. `crypto.randomUUID` exists only in a secure
 * context, and a paired phone on this Mac's own network reaches the page over
 * plain http; `getRandomValues` is there in both.
 */
function mintKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return "web-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")
}

/**
 * One decision, sent under one key. A connection that dropped is asked again
 * with the same key — the daemon replays what it answered the first time, or
 * says the first attempt is still being carried out — twice at most; a
 * refusal is the answer and is never retried.
 */
async function decide<T>(path: string, body: unknown): Promise<T> {
  const key = mintKey()
  const init: RequestInit = {
    method: "POST",
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(body),
  }
  for (let attempt = 0; ; attempt++) {
    try {
      return await call<T>(path, init)
    } catch (e) {
      const again =
        (e instanceof TransportError && attempt < 2) ||
        (e instanceof RefusalError && e.code === "request_in_progress" && attempt < 2)
      if (!again) throw e
      await new Promise((r) => setTimeout(r, 400 * (attempt + 1)))
    }
  }
}

async function mutate<T>(path: string, body: unknown, method = "POST"): Promise<T> {
  const key = mintKey()
  return call<T>(path, {
    method,
    headers: { "Content-Type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(body),
  })
}

export type WorkV2Kind = "feature" | "issue" | "epic" | "refactor" | "plan"
export type WorkV2Phase = "created" | "assigning" | "assigned" | "implementing" | "verifying" | "merging" | "deploying" | "done" | "cancelled"

export interface WorkV2Project {
  id: string
  label: string
  path: string
  icon: unknown
  available: boolean
}

export interface WorkV2Item {
  id: string
  project: WorkV2Project
  kind: WorkV2Kind
  title: string
  description: string
  phase: WorkV2Phase
  condition: string | null
  area: "planning" | "unassigned" | WorkV2Phase
  deployment_policy: "required" | "not_required" | "agent_decides"
  owner_session: string | null
  created_at: number
  updated_at: number
  closed_at: number | null
  cycle: number
  version: number
  images?: WorkV2Image[]
}

export interface WorkV2Image {
  id: string
  title: string
  media_type: "image/png"
  byte_count: number
  width: number
  height: number
  position: number
  created_by: string
  created_at: number
}

export interface DirectTodoV2 {
  id: string
  text: string
  created_at: number
  sent_at: number | null
  read_at: number | null
  completed_at: number | null
  completed_by?: string
  version: number
  images?: WorkV2Image[]
}

export interface SessionWorkV2 {
  ok: boolean
  assigned_items: WorkV2Item[]
  recent_items: WorkV2Item[]
  direct_todos: DirectTodoV2[]
  truncated: boolean
}

export interface WorkV2Proposal {
  id: string
  project_id: string
  kind: WorkV2Kind
  title: string
  description: string
  reason: string
  suggested_acceptance: string
  session_id: string
  state: string
  created_at: number
}

export const readWorkV2 = (projectID?: string) =>
  call<{ ok: boolean; rows: WorkV2Item[]; counts: Record<string, number>; truncated: boolean }>(
    "/v1/work/v2/items" + query({ project: projectID }),
  )
export const readWorkV2Proposals = () => call<{ rows: WorkV2Proposal[]; truncated: boolean }>("/v1/work/v2/proposals?state=pending")
export const resolveWorkV2Proposal = (id: string, decision: "accept" | "reject") =>
  mutate<unknown>(`/v1/work/v2/proposals/${id}/${decision}`, {})

export const createWorkV2 = (body: {
  project_id: string
  kind: WorkV2Kind
  title: string
  description: string
  deployment_policy: "required" | "not_required" | "agent_decides"
}) => mutate<{ item: WorkV2Item }>("/v1/work/v2/items", body)

export const assignWorkV2 = (item: WorkV2Item, terminalID: string) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/assign`, {
    expected_version: item.version,
    mode: "existing_session",
    terminal_id: terminalID,
  })

export const assignNewWorkV2 = (item: WorkV2Item, assistant: "codex" | "claude" = "codex") =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/assign`, {
    expected_version: item.version,
    mode: "new_session",
    assistant,
    model: "default",
  })

export const editWorkV2 = (item: WorkV2Item, title: string, description: string) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}`, {
    expected_version: item.version,
    title,
    description,
  }, "PATCH")

// The lifecycle calls this cancellation rather than erasing its audit trail.
// To the person it is Delete: the item leaves the Board and its Session to-do
// immediately, while Clawdline retains why an assigned item disappeared.
export const deleteWorkV2 = (item: WorkV2Item) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/cancel`, {
    expected_version: item.version,
    reason: "Deleted by the person from the Board.",
  })

export const addWorkV2Image = (itemID: string, expectedVersion: number, picture: {
  url: string
  name: string
}, position: number) => mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${itemID}/images`, {
  expected_version: expectedVersion,
  title: picture.name,
  data_url: picture.url,
  position,
})

export const deleteWorkV2Image = (item: WorkV2Item, imageID: string) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/images/${imageID}`, {
    expected_version: item.version,
  }, "DELETE")

export const readSessionWorkV2 = (terminalID: string) =>
  call<SessionWorkV2>(`/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}`)
export const readSessionsForWorkV2 = () => call<SessionsSnapshot>("/v1/sessions")

export const createDirectTodoV2 = (terminalID: string, text: string) =>
  mutate<{ todo: DirectTodoV2 }>(`/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}`, { text })

export const addDirectTodoV2Image = (terminalID: string, todoID: string, expectedVersion: number, picture: {
  url: string
  name: string
}, position: number) => mutate<{ todo: DirectTodoV2 }>(
  `/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}/${todoID}/images`,
  { expected_version: expectedVersion, title: picture.name, data_url: picture.url, position },
)

export const directTodoActionV2 = (terminalID: string, todoID: string, action: "send" | "complete" | "delete") =>
  mutate<{ todo?: DirectTodoV2; deleted?: string }>(
    `/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}/${todoID}/${action}`,
    {},
  )

export type Op =
  | "start"
  | "schedule"
  | "defer"
  | "accept"
  | "done_elsewhere"
  | "rework"
  | "drop"
  | "handover"
  | "untrack"
  | "rank"

export interface Command {
  op: Op
  owner?: string
  start_on?: string
  rank?: number
  /** Why no delivery named this item: required by `done_elsewhere`, refused empty (BD-17). */
  reason?: string
}

export const command = (item: Item, c: Command) =>
  decide<{ item: Item }>(`/v1/work/items/${item.id}`, { ...c, expected_version: item.version })

export const createItem = (n: { title: string; project: string; place: "board" | "backlog" }) =>
  decide<{ item: Item }>("/v1/work/items", n)

export const answerProposal = (id: string, answer: "track" | "later" | "no") =>
  decide<unknown>(`/v1/work/proposals/${id}`, { answer })

export const resolveProposal = (id: string, resolution: string, evidence: string) =>
  decide<unknown>(`/v1/work/proposals/${id}/resolve`, { resolution, evidence })

export const answerDecision = (id: string, option: string) =>
  decide<unknown>(`/v1/work/decisions/${id}`, { answer: option })
