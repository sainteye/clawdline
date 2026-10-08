import { RefusalError, TransportError, isRefusal } from "@clawdline/core"
import { fetchWithDeadline } from "./fetch-deadline.js"
import type { PersonaSuggestionReply, SessionsSnapshot, UsageItem, UsageSession, WorkGateCompactRead, WorkGateDetailRead, WorkGateDecisionAction } from "@clawdline/contract"
import type { CreatedVia } from "./words.js"

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

/** `withdrawn`: the Session that asked stopped waiting before anyone answered. */
export type DecisionState = "open" | "answered" | "defaulted" | "withdrawn"

export interface Decision {
  id: string
  session_id: string
  work_id: string | null
  project: string | null
  question: string
  options: { id: string; label: string }[]
  default: string
  blocking: boolean
  state: DecisionState
  answer?: string | null
  created_at: number
  due_at: number
}

export interface DecisionPage {
  counts: Record<string, number>
  rows: Decision[]
  next_cursor: string | null
}

export interface ProjectPlace {
  id: string
  label: string
  path: string
  icon?: unknown
  repo?: string
  setup?: ProjectSetupEvidence
}

export interface ProjectSetupEvidence {
  icon: "mirrored" | "override" | "registry" | "generated"
  deploy: "ready" | "missing" | "attention" | "not_applicable"
  deploy_activity: "idle" | "running" | "succeeded" | "failed" | "unknown"
  servers: "ready" | "missing" | "empty" | "attention"
  server_count: number
  sync: "ready" | "missing"
}

export interface ProjectPlacePage {
  places: ProjectPlace[]
}

async function call<T>(path: string, init: RequestInit = {}, timeoutMs = 15_000): Promise<T> {
  let res: Response
  try {
    res = await fetchWithDeadline(path, { credentials: "same-origin", ...init }, timeoutMs)
  } catch (cause) {
    throw new TransportError(`${init.method ?? "GET"} ${path} did not complete`, cause)
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

export const readDecisions = () => call<DecisionPage>("/v1/work/decisions")
export const readDecision = (id: string) => call<{ decision: Decision }>(`/v1/work/decisions/${encodeURIComponent(id)}`)
/** The machine's real project directory: existing places it recognizes, newest first (at most forty). */
export const copyProjectIcon = (id: string, icon: unknown, expected: unknown) =>
  mutate<{ ok: boolean; icon: unknown }>(`/v1/projects/${encodeURIComponent(id)}/icon`, { icon, expected }, "PUT")
export const readProjectPlaces = (machine?: string) => call<ProjectPlacePage>("/v1/places" + query({ machine }))
export const removeProjectPlace = (place: ProjectPlace) =>
  mutate<{ registered: boolean }>("/v1/places", { place: place.id, paths: [place.path] }, "DELETE")
/** Project settings sync (docs/project-sync.md), as internal/domain/projectsync spells it. */
export interface SyncFile { path: string; sha256: string; size: number; content?: string }
export interface SyncEntry { repo: string; clone_url: string; label: string; icon: unknown; files: SyncFile[]; withheld?: string[]; revision: string }
export interface SyncManifest { version: number; at: number; revision: string; projects: SyncEntry[]; skipped: { label: string; path: string; reason: string }[] }
export interface SyncSource { machine: string; name: string }
export interface SyncRecord { repo: string; path: string; source: SyncSource; revision: string; label: string; icon: unknown; files: Record<string, string>; applied_at: number }
export interface SyncClone { repo: string; state: "cloning" | "clone_failed"; dest: string; source: SyncSource; started: number; error?: string }
export interface SyncMirror { clone_root: string; projects: SyncRecord[]; clones: SyncClone[] }
export interface SyncResult { repo: string; state: "applied" | "unchanged" | "missing" | "cloning"; path?: string; revision: string; written: string[]; deleted: string[]; kept: { path: string; reason: string }[] }
export const readProjectMirror = () => call<SyncMirror>("/v1/project-sync/mirror")
export const applyProjectMirror = (source: SyncSource, project: SyncEntry, clone: boolean, replaceSource = false) =>
  mutate<{ ok: boolean; result: SyncResult }>("/v1/project-sync/mirror", { source, project, clone, replace_source: replaceSource })
export const detachProjectMirror = (repo: string) =>
  call<{ ok: boolean; removed: boolean }>("/v1/project-sync/mirror" + query({ repo }), { method: "DELETE", headers: { "Idempotency-Key": mintKey() } })
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
async function decide<T>(path: string, body: unknown, key = mintKey()): Promise<T> {
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
export type WorkV2ExecutableKind = Extract<WorkV2Kind, "feature" | "issue" | "epic" | "refactor">
export type WorkV2Phase = "created" | "assigning" | "assigned" | "implementing" | "verifying" | "merging" | "deploying" | "done" | "cancelled"
export type WorkV2Status = "open" | "done" | "all"

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
  acceptance_criteria: string
  acceptance_version: number
  acceptance_digest: string
  gate_snapshot_cycle: number
  gate_snapshot_at: number | null
  planning_gate: boolean
  verify_gate: boolean
  verification?: WorkGateCompactRead | WorkGateDetailRead
  phase: WorkV2Phase
  condition: string | null
  user_action: string
  /** The open decision an Agent's waiting_user points at; absent otherwise. */
  decision_id?: string
  area: "planning" | "unassigned" | WorkV2Phase
  deployment_policy: "required" | "not_required" | "agent_decides"
  /**
   * The person's "Needs independent review" choice. Meaningful only for a
   * Feature: with the planning gate on, a checked Feature needs a plan and an
   * independent plan review before implementing; an unchecked one needs only
   * acceptance criteria. Always false on other kinds.
   */
  review_required: boolean
  owner_session: string | null
  created_by?: string
  /** Present when a Session created the item on the person's message through Clawdline. */
  created_via?: CreatedVia
  /** Present when the owning Session claimed the item on the person's message through Clawdline. */
  claimed_via?: CreatedVia
  /** The Epic this item was created under by the Epic's owning Session; empty or absent when none. */
  parent_id?: string
  created_at: number
  updated_at: number
  closed_at: number | null
  phase_entered_at?: number | null
  deployment_evidence?: string
  no_deployment_reason?: string
  cycle: number
  version: number
  documents?: WorkV2Document[]
  images?: WorkV2Image[]
  steps?: WorkV2Step[]
}

export interface WorkV2Document {
  id: string
  role: "spec" | "design" | "test" | "deploy" | "completion_report" | "plan" | "plan_review" | "other"
  title: string
  body: string
  reference: string
  position: number
  version: number
  created_at: number
}

export interface WorkV2Step {
  id: string
  title: string
  done: boolean
  position: number
  created_by: string
  completed_by: string
  completed_at: number | null
  version: number
}

export interface WorkV2Image {
  id: string
  title: string
  // A photograph is stored as the JPEG it is; anything else, and every picture
  // stored before that, as PNG.
  media_type: "image/png" | "image/jpeg"
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
  /** Who wrote the row: the person's actor, or the Session's own conversation id. */
  created_by?: string
  version: number
  images?: WorkV2Image[]
}

export interface SessionWorkV2 {
  ok: boolean
  assigned_items: WorkV2Item[]
  recent_items: WorkV2Item[]
  direct_todos: DirectTodoV2[]
  open_decisions: Decision[]
  decisions_error?: string | null
  truncated: boolean
}

export interface SessionWorkSummaryV2 {
  ok: boolean
  done: number
  active: number
  waiting: number
  truncated: boolean
}

export interface HumanInterventionV2 {
  id: string
  source_conversation: string
  source_label: string
  target_conversation: string
  target_session: string
  kind: "read" | "answer" | "action" | "report"
  title: string
  summary: string
  action: string
  reason: string
  detail?: string
  options: { label: string; draft: string }[]
  document_url?: string
  created_at: number
  read_at: number | null
  resolved_at: number | null
  resolution?: string
  version: number
}

export interface HumanInterventionsV2 {
  ok: boolean
  rows: HumanInterventionV2[]
  pruned_resolved: number
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
  source_work_id: string
  source_todo_id: string
  state: string
  created_at: number
}

export interface WorkV2Page {
  ok: boolean
  rows: WorkV2Item[]
  counts: Record<string, number>
  next_cursor: string | null
  page_size: number
  truncated: boolean
}

export const readWorkV2 = (projectID?: string, status: WorkV2Status = "open", search = "", cursor = "") =>
  call<WorkV2Page>(
    "/v1/work/v2/items" + query({ project: projectID, status, q: search || undefined, cursor: cursor || undefined }),
  )
export const readWorkV2Item = (id: string) =>
  call<{ ok: boolean; item: WorkV2Item }>(`/v1/work/v2/items/${id}`)
/** The person opened this item while it showed `phase`; a Session card stops showing it as awaiting acceptance. */
export const markWorkV2Seen = (id: string, phase: WorkV2Phase) =>
  call<{ ok: boolean; marked: boolean }>(`/v1/work/v2/items/${id}/seen`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ phase }),
  })
export const readWorkV2Proposals = () => call<{ rows: WorkV2Proposal[]; truncated: boolean }>("/v1/work/v2/proposals?state=pending")
export const resolveWorkV2Proposal = (id: string, decision: "accept" | "reject") =>
  mutate<unknown>(`/v1/work/v2/proposals/${id}/${decision}`, {})

export type CreateWorkV2Body = {
  project_id: string
  kind: WorkV2Kind
  title: string
  description: string
  acceptance_criteria?: string
  deployment_policy: "required" | "not_required" | "agent_decides"
  /** Sent only for a Feature; the daemon refuses `true` on any other kind. */
  review_required?: boolean
}

export const createWorkV2 = (body: CreateWorkV2Body, key?: string) =>
  decide<{ item: WorkV2Item }>("/v1/work/v2/items", body, key)

export const assignWorkV2 = (item: WorkV2Item, terminalID: string) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/assign`, {
    expected_version: item.version,
    mode: "existing_session",
    terminal_id: terminalID,
  })

/** `persona` is sent only when one was chosen: a new Session with none is the Session it always was. */
export const assignNewWorkV2 = (item: WorkV2Item, assistant: "codex" | "claude", persona?: string) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/assign`, {
    expected_version: item.version,
    mode: "new_session",
    assistant,
    model: "default",
    ...(persona ? { persona } : {}),
  })

/** One explicit press; reading or rendering an item never calls this route. */
export const suggestPersonaWorkV2 = (item: WorkV2Item) =>
  decide<PersonaSuggestionReply>(`/v1/work/v2/items/${item.id}/persona-suggestion`, {
    expected_version: item.version,
  })

export const remindWorkV2 = (item: WorkV2Item) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/remind`, {
    expected_version: item.version,
  })

export const editWorkV2 = (item: WorkV2Item, title: string, description: string, acceptance?: string) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}`, {
    expected_version: item.version,
    title,
    description,
    ...(acceptance === undefined ? {} : { acceptance_criteria: acceptance }),
  }, "PATCH")

/** The person's one-press "Needs independent review" choice on a Feature; nothing else on the item changes. */
export const setWorkV2ReviewRequired = (item: WorkV2Item, required: boolean) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}`, {
    expected_version: item.version,
    review_required: required,
  }, "PATCH")

export interface GateExport {
  ok: boolean
  manifest: { item_id: string; item_version: number; sha256: string; byte_count: number; round_count: number }
  /** Exact UTF-8 export bytes, including the trailing newline, for manifest verification. */
  document: string
}

/** Person-only gate actions. Parent-Epic decisions deliberately have no UI route here. */
export const decideWorkGate = (item: WorkV2Item, action: WorkGateDecisionAction, reason: string,
  extra: { direction?: string; acceptance_criteria?: string; target_session_id?: string } = {}) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/gate-decision`, {
    expected_version: item.version, action, reason, ...extra,
  })
export const exportWorkGate = (itemID: string) =>
  call<GateExport>(`/v1/work/v2/items/${itemID}/gate-export`)
export const purgeWorkGate = (itemID: string, expectedVersion: number, sha256: string) =>
  mutate<{ ok: boolean; purged_rounds: number; sha256: string }>(`/v1/work/v2/items/${itemID}/gate-purge`, {
    expected_version: expectedVersion, sha256,
  })

export const convertWorkV2 = (item: WorkV2Item, kind: WorkV2ExecutableKind | "plan") =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/convert`, {
    expected_version: item.version,
    kind,
  })

// The lifecycle calls this cancellation rather than erasing its audit trail.
// To the person it is Delete: the item leaves the Board and its Session to-do
// immediately, while Clawdline retains why an assigned item disappeared.
export const deleteWorkV2 = (item: WorkV2Item) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/cancel`, {
    expected_version: item.version,
    reason: "Deleted by the person from the Board.",
  })

// The person's override: closes the item as done without the evidence an
// owning Session must give, whatever its steps say.
export const completeWorkV2 = (item: WorkV2Item) =>
  mutate<{ item: WorkV2Item }>(`/v1/work/v2/items/${item.id}/complete`, {
    expected_version: item.version,
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

// A durable conversation selector lets a read survive a temporarily
// unverified terminal inventory. Mutations still use the terminal id because
// Send and the other actions need the live target they act on.
export const readSessionWorkV2 = (terminalID: string, conversationID = "") => {
  const target = conversationID ? `conversation:${conversationID}` : terminalID
  return call<SessionWorkV2>(`/v1/work/v2/session-todos/${encodeURIComponent(target)}`, {}, 30_000)
}
export const readSessionWorkSummaryV2 = (terminalID: string, conversationID = "") => {
  const target = conversationID ? `conversation:${conversationID}` : terminalID
  return call<SessionWorkSummaryV2>(`/v1/work/v2/session-todos/${encodeURIComponent(target)}?summary=1`, {}, 15_000)
}
export const readHumanInterventionsV2 = (conversationID: string) =>
  call<HumanInterventionsV2>(`/v1/work/v2/human-interventions/${encodeURIComponent(`conversation:${conversationID}`)}`, {}, 30_000)
export const humanInterventionActionV2 = (
  conversationID: string, note: HumanInterventionV2, action: "read" | "resolve" | "reopen",
) => mutate<{ note: HumanInterventionV2 }>(
  `/v1/work/v2/human-interventions/${encodeURIComponent(`conversation:${conversationID}`)}/${note.id}/${action}`,
  { expected_version: note.version },
)
export const readSessionsForWorkV2 = () => call<SessionsSnapshot>("/v1/sessions")
/** The token ledger's bill of a Board item, and of one session (docs/token-ledger.md). */
export const readItemUsage = (itemID: string) => call<UsageItem>(`/v1/usage/items/${encodeURIComponent(itemID)}`)
export const readSessionUsage = (conversation: string) =>
  call<UsageSession>(`/v1/usage/sessions/${encodeURIComponent(conversation)}`)

export const createDirectTodoV2 = (terminalID: string, text: string) =>
  mutate<{ todo: DirectTodoV2 }>(`/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}`, { text })

export const addDirectTodoV2Image = (terminalID: string, todoID: string, expectedVersion: number, picture: {
  url: string
  name: string
}, position: number) => mutate<{ todo: DirectTodoV2 }>(
  `/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}/${todoID}/images`,
  { expected_version: expectedVersion, title: picture.name, data_url: picture.url, position },
)

export const directTodoActionV2 = (terminalID: string, todoID: string, action: "send" | "complete" | "reopen" | "delete") =>
  mutate<{ todo?: DirectTodoV2; deleted?: string }>(
    `/v1/work/v2/session-todos/${encodeURIComponent(terminalID)}/${todoID}/${action}`,
    {},
  )

export const answerDecision = (id: string, option: string) =>
  decide<unknown>(`/v1/work/decisions/${id}`, { answer: option })
