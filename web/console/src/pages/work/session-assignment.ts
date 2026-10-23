// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat } from "../../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../../catalog.ts"
import type { Assistant, Scan, SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { workWord } from "./words.ts"

export interface SessionWorkCounts {
  board: number
  todos: number
  unfinished: number
}

/** The terminal observation answers whether a Session is working right now. */
export function sessionActivityName(state: SessionRow["state"]): string {
  return ({ working: "Working", waiting: catalogWord("literal", "ad8aadf7bf19"), idle: "Idle", unknown: catalogWord("literal", "68c054f499d6") } as const)[state] ?? catalogWord("literal", "68c054f499d6")
}

/** Work state explains what the Session needs even when its terminal is idle. */
export function sessionWorkStateName(state: SessionRow["work_state"]): string {
  return ({
    working: catalogWord("literal", "6db78eb344c7"),
    waiting_you: catalogWord("literal", "098adc2b04e2"),
    waiting_session: catalogWord("literal", "619d09f68466"),
    holding: catalogWord("literal", "92d2ca17aee9"),
    milestone_complete: catalogWord("literal", "f3a995d001b1"),
    work_complete: catalogWord("literal", "389a958c1c0b"),
    ready: catalogWord("literal", "65901131fccc"),
    unknown: catalogWord("literal", "6815fa90cb2c"),
  } as const)[state] ?? catalogWord("literal", "6815fa90cb2c")
}

/**
 * The work state as a Session card says it. A Session's own turn report reads
 * 本輪已回報 unless the daemon names one of its Board items that reached
 * deploying or done and the person has not opened since: then 待驗收 and that
 * item. The rule is the legacy card's (`sessionReceiptCopy` in derive.js), so
 * both lists say the same thing about the same row.
 */
export function sessionWorkLabel(row: Pick<SessionRow, "work_state" | "disposition" | "acceptance">): string {
  const finished = row.work_state === "milestone_complete" || row.work_state === "work_complete"
  if (!finished || row.disposition?.scope !== "session") return sessionWorkStateName(row.work_state)
  const acceptance = row.acceptance
  if (acceptance?.state === "pending" && acceptance.title) {
    return catalogFormat("template", "19566a87444b", [acceptance.title, (acceptance.count ?? 0) > 1 ? catalogFormat("template", "b98d29fea593", [acceptance.count]) : ""])
  }
  const said = row.work_state === "work_complete" ? sessionWorkStateName(row.work_state) : catalogWord("literal", "8b456ffd3d10")
  // Only a reading of none is none: a missing field (an older daemon) is not.
  return acceptance?.state === "none" ? said : catalogFormat("template", "3d747201508d", [said])
}

/** The phases in which an item waits for the person to look at what was delivered. */
export function awaitsAcceptance(phase: string): boolean {
  return phase === "deploying" || phase === "done"
}

export function sessionWorkCounts(page: SessionWorkV2): SessionWorkCounts {
  const board = page.assigned_items.length
  const todos = page.direct_todos.length + page.assigned_items.reduce(
    (count, item) => count + (item.steps?.filter((step) => !step.done).length ?? 0), 0,
  )
  return { board, todos, unfinished: board + todos }
}

/**
 * The Project a Session would be accepted into. The daemon decides it with the
 * claim's own rule (`project_key`: the nearest repository, a linked worktree
 * followed to the repository it belongs to); a machine too old to say falls
 * back to the exact working directory, which is what it was compared by before.
 */
export function sessionProjectKey(session: Pick<SessionRow, "cwd" | "project_key">): string | undefined {
  return session.project_key || session.cwd || undefined
}

/**
 * The Sessions an item can be handed to: those working in its Project with a
 * conversation id, less the one that already owns it — moving an item to its
 * own owner is refused by the daemon (`assignment_unchanged`).
 */
export function assignmentCandidates(
  sessions: SessionRow[],
  item: { project: { path: string }; owner_session: string | null },
): SessionRow[] {
  return sessions.filter((s) => sessionProjectKey(s) === item.project.path && s.sessionId && s.sessionId !== item.owner_session)
}

/**
 * What the Board knows about the Session list it picks from. The list is read
 * after the Board itself, so until that read answers there is no list at all,
 * which is not the same as a list with nothing in it. `settling` names the rows
 * first listed less than ASSIGNMENT_SETTLE_MS before this answer: only they may
 * still be waiting for a cwd or a conversation id.
 */
export type SessionListReading =
  | { phase: "loading" }
  | { phase: "failed"; error: string }
  | { phase: "ready"; complete: boolean; settling: readonly string[] }

/**
 * How long a row without its cwd or conversation id keeps the list from saying
 * "none". A Session that is still being read names itself within a pass or two;
 * one that never will (nobody typed into it, its transcripts are ambiguous, the
 * machine cannot read its open files, it is not an assistant at all) must not
 * hold the dialog incomplete for as long as it runs.
 */
export const ASSIGNMENT_SETTLE_MS = 60_000

/**
 * Whether a `/v1/sessions` answer may say there is nothing, by the daemon's
 * own rule for `emptyAuthoritative` (internal/transport/http/sessions.go,
 * `EmptyAuthoritative: inv.Complete && len(inv.DisabledSources) == 0`): a
 * source turned off was not looked at, so an empty list says nothing about it
 * either. Cloud's relay snapshot says `complete: false` until every machine's
 * inventory has arrived.
 */
export function sessionListComplete(scan: Pick<Scan, "complete" | "sources">): boolean {
  return scan.complete && !(scan.sources ?? []).some((source) => source.disabled)
}

/** The choices "assign to an existing Session" can offer, and whether "none" may be said. */
export type AssignmentChoices =
  | { phase: "loading" }
  | { phase: "failed"; error: string; rows: SessionRow[] }
  | { phase: "incomplete"; rows: SessionRow[] }
  | { phase: "empty" }
  | { phase: "candidates"; rows: SessionRow[] }

/**
 * Only a complete answer may say the Project has no Session. A row that could
 * be in the Project but has no cwd or conversation id yet keeps the list
 * incomplete while it is settling: it may be the Session the person is looking
 * for. A row the machine said it cannot name (`ambiguous`, `unreadable`) is an
 * answer, not a wait.
 */
export function assignmentChoices(
  reading: SessionListReading,
  sessions: SessionRow[],
  item: { project: { path: string }; owner_session: string | null },
): AssignmentChoices {
  if (reading.phase === "loading") return { phase: "loading" }
  const rows = assignmentCandidates(sessions, item)
  if (reading.phase === "failed") return { phase: "failed", error: reading.error, rows }
  const settling = new Set(reading.settling)
  const unsettled = sessions.some((s) => {
    if (s.sessionId && s.sessionId === item.owner_session) return false
    if (!settling.has(s.id) || s.identity === "ambiguous" || s.identity === "unreadable") return false
    const key = sessionProjectKey(s)
    return !key || (key === item.project.path && !s.sessionId)
  })
  if (!reading.complete || unsettled) return { phase: "incomplete", rows }
  return rows.length ? { phase: "candidates", rows } : { phase: "empty" }
}

/** A `/v1/sessions` answer, as the Board reads it. */
export interface SessionListAnswer {
  sessions: SessionRow[]
  scan: Pick<Scan, "complete" | "sources">
}

/**
 * How the Board reads the Session list it picks from.
 *
 * Over Cloud a plain `/v1/sessions` is a person's refresh of the session list:
 * the relay reader asks the machine to restate its Sessions and then waits for
 * a new pass (`initialSnapshot`, cloud/relay-reader.ts). A machine publishes a
 * new pass only when its set changes, so with nothing changed that wait lasted
 * until the deadline every time, and the deadline threw away the list the
 * reader answered with when it fired. The Board is another view of the list
 * the page already holds: `X-Clawdline-Initial-Read` answers a complete held
 * list at once and asks the machine only when it is incomplete. `answerMs` is
 * how long the reader's answer at the deadline is waited for
 * (`fetchWithDeadline`). The machine's own daemon ignores the header.
 */
export const SESSION_LIST_READ = {
  path: "/v1/sessions",
  headers: { "X-Clawdline-Initial-Read": "1" },
  timeoutMs: 15_000,
  answerMs: 5_000,
} as const

/**
 * The Board's Session read, given the page's `call`: the one place the read's
 * path, header, deadline and late-answer window are put together, so the
 * Board cannot send a plain list read that waits for a pass that never comes.
 */
export function readSessionListWith<T>(call: (path: string, init: RequestInit, timeoutMs: number, answerMs: number) => Promise<T>): Promise<T> {
  return call(SESSION_LIST_READ.path, { headers: SESSION_LIST_READ.headers }, SESSION_LIST_READ.timeoutMs, SESSION_LIST_READ.answerMs)
}

/**
 * A failed Session list read as one word for the page's diagnostics: the
 * refusal's code, `timeout` for a deadline, otherwise the error's name. Never
 * its message, which can carry a path.
 */
export function sessionListFailureCode(error: unknown): string {
  const failure = (error ?? null) as { code?: unknown; name?: unknown; cause?: { name?: unknown } | null } | null
  if (typeof failure?.code === "string" && failure.code) return failure.code
  if (failure?.name === "AbortError" || failure?.cause?.name === "AbortError") return "timeout"
  if (failure?.name === "TransportError") return "transport"
  return typeof failure?.name === "string" && failure.name ? failure.name : "unknown"
}

/** A failed read of the Session list, for the page's diagnostics. */
export interface SessionListFailure {
  code: string
  elapsedMs: number
}

/** What one read of the Session list did: whether it reached the page, and its failure. */
export interface SessionListOutcome {
  applied: boolean
  error?: string
}

/** What the page shows of the Session list. */
export interface SessionListState {
  reading: SessionListReading
  sessions: SessionRow[]
}

export interface SessionListReaderOptions {
  read: () => Promise<SessionListAnswer>
  publish: (state: SessionListState) => void
  words: (error: unknown) => string
  /** A newer answer arrived after this failure was shown: its notice is stale. */
  recovered?: (failure: string) => void
  /** A read failed: what it failed with and how long it took, whether or not it is shown. */
  failed?: (failure: SessionListFailure) => void
  now?: () => number
}

/**
 * The Board's reading of the Session list: the 30-second Board load and the
 * picker's retry both read through it. Every read takes a sequence number and
 * an answer is shown only when it is newer than the one on screen, so a slow
 * old answer — success or failure — never replaces a newer one. The retry
 * reads `/v1/sessions` and nothing else; the Board behind the dialog stays as
 * it is.
 */
export class SessionListReader {
  state: SessionListState = { reading: { phase: "loading" }, sessions: [] }
  private issued = 0
  private shown = 0
  private failure = ""
  /** When each listed row was first seen, by row id; only listed rows are kept. */
  private firstSeen = new Map<string, number>()

  private readonly options: SessionListReaderOptions

  constructor(options: SessionListReaderOptions) {
    this.options = options
  }

  /** The Board load's read: what is on screen stays until the answer. */
  refresh(): Promise<SessionListOutcome> {
    return this.ask()
  }

  /** The picker's "read again": says loading at once and reads only the Session list. */
  readonly retry = (): void => {
    this.set({ ...this.state, reading: { phase: "loading" } })
    void this.ask()
  }

  private async ask(): Promise<SessionListOutcome> {
    const sequence = ++this.issued
    const clock = this.options.now ?? Date.now
    const started = clock()
    let answer: SessionListAnswer
    try {
      answer = await this.options.read()
    } catch (error) {
      try {
        this.options.failed?.({ code: sessionListFailureCode(error), elapsedMs: Math.max(0, clock() - started) })
      } catch { /* diagnostics cannot fail the read */ }
      if (sequence <= this.shown) return { applied: false }
      this.shown = sequence
      const words = this.options.words(error)
      this.failure = words
      this.set({ ...this.state, reading: { phase: "failed", error: words } })
      return { applied: true, error: words }
    }
    if (sequence <= this.shown) return { applied: false }
    this.shown = sequence
    const now = clock()
    const seen = new Map<string, number>()
    for (const row of answer.sessions) seen.set(row.id, this.firstSeen.get(row.id) ?? now)
    this.firstSeen = seen
    const settling = answer.sessions.filter((row) => now - (seen.get(row.id) ?? now) < ASSIGNMENT_SETTLE_MS).map((row) => row.id)
    this.set({ sessions: answer.sessions, reading: { phase: "ready", complete: sessionListComplete(answer.scan), settling } })
    const failure = this.failure
    this.failure = ""
    if (failure) this.options.recovered?.(failure)
    return { applied: true }
  }

  private set(state: SessionListState): void {
    this.state = state
    this.options.publish(state)
  }
}

/** The rows a picker lists for these choices. */
export function assignmentChoiceRows(choices: AssignmentChoices): SessionRow[] {
  return "rows" in choices ? choices.rows : []
}

/** The sentence the picker says with its rows, or null when the rows say it all. */
export function assignmentChoicesWords(choices: AssignmentChoices): string | null {
  switch (choices.phase) {
    case "loading": return workWord("assignSessionsLoading")
    case "failed": return workWord("assignSessionsFailed", { error: choices.error })
    case "incomplete": return workWord("assignSessionsIncomplete")
    case "empty": return catalogWord("inline", "7b9c3fe68d48")
    case "candidates": return null
  }
}

/** The assistants a new Session can be opened with, in the order the Board offers them. */
export const NEW_SESSION_ASSISTANTS: readonly Assistant[] = ["codex", "claude", "antigravity"]

/** Which company's assistant a Session runs, as the person reads it. */
export function assistantName(assistant: SessionRow["assistant"]): string {
  return assistant === "claude" ? "Claude Code" : assistant === "codex" ? "Codex" : assistant === "antigravity" ? "Gemini" : catalogWord("literal", "1d6ea8ca2af5")
}

const LAST_ASSISTANT = "clawdline.work.new-session-assistant"

/** The last assistant chosen for a new Session on this browser; Codex before any choice. */
export function rememberedAssistant(storage: Pick<Storage, "getItem"> | undefined = globalThis.localStorage): Assistant {
  try {
    const value = storage?.getItem(LAST_ASSISTANT)
    return value === "claude" || value === "codex" || value === "antigravity" ? value : "codex"
  } catch {
    return "codex"
  }
}

export function rememberAssistant(assistant: Assistant, storage: Pick<Storage, "setItem"> | undefined = globalThis.localStorage): void {
  try {
    storage?.setItem(LAST_ASSISTANT, assistant)
  } catch {
    // A private window keeps the choice for this card only.
  }
}
