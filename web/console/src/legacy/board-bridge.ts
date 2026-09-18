// The Board page's way into its copied module.
//
// `js/view/board.js`, `js/net/client.js` and `js/net/session-links.js` are the
// Swift app's, byte for byte. The page is bound here as that app's `main.js`
// binds it: one table of the page's elements by id, and an environment whose
// reads are this daemon's routes spelled as the original's local transport
// spells them (`net/live.js`'s `board` and `boardItems`).
//
// What differs, and why:
//
// - `assignSession` is absent. The original opens its assignment sheet
//   (`input/board-assignment.js`), whose only action is an item write, and this
//   daemon refuses item writes by name (501, docs/board-design.md §6 #2). The
//   module draws no "Assign Session" button for an environment without it,
//   which is what the original does for such an environment.
// - `openSession` is the half of `input/board-session.js`'s `open` that needs no
//   sheet: a conversation with exactly one live Session opens that Session, as
//   there (`openObserved` → `openLive`). Anything else — no live match, or two —
//   answers `{ error }`, and the module says in its own words that nothing was
//   opened and nothing was resumed. The original would show its Session sheet
//   there, with history and Resume; that sheet is not on this console.
// - `onMode` is `BoardControls.apply` (`input/board-settings.js`) for the parts
//   this console has: the Projects lede, the root's board mode, the hidden
//   drawer row, and the Settings block, which draws from `boardMode` below. The
//   Usage and ledger rows it also shows or hides are App's.
// - A refusal on this daemon is `{ error: "code", detail }`, not
//   `{ error: { code, message } }`; `jsonFetch` reads both, as projects-bridge's.
import { T } from "./js/core/i18n.js"
import { S } from "./js/core/state.js"
import { drawIcon } from "./js/core/pixels.js"
import { tint } from "./js/core/util.js"
import {
  bindBoardPage as bindBoardPageOriginal,
  boardLocatorFromHash as boardLocatorFromHashOriginal,
  enterProjectBoard as enterProjectBoardOriginal,
} from "./js/view/board.js"

/** `main.js`'s element table for `bindBoardPage`, in its order. */
export const BOARD_ELEMENT_IDS = [
  "board", "board-title", "board-project-mark", "board-subtitle", "board-items", "board-detail", "board-status",
  "board-search", "board-back", "board-timeline-tab", "board-refresh",
] as const

export interface BoardLocator {
  machine: string
  project: string
  item: string | null
}

/** What `bindBoardPage` hands back. */
export interface BoardPage {
  enter(project?: string | null, item?: string | null): Promise<unknown>
  leave(): void
  refresh(): Promise<unknown>
  escape(): unknown
  open(project?: string | null, item?: string | null, presentation?: unknown, machine?: string | null): Promise<unknown>
  openLocator(locator: BoardLocator | null, error?: string | null): Promise<unknown>
  state: { projectId: string | null; itemId: string | null; machine: string | null; projectPresentation: unknown }
}

/** What the page needs from its host that is not a read. */
export interface BoardHost {
  /** `Pages.go(name)`. */
  navigate(name: string): void
  /** `openSession` in `main.js`: show one live Session. */
  openLive(id: string): void
}

/** The part of a board answer `BoardControls.apply` reads. */
export interface BoardMode {
  enabled: boolean
  revision: number
  narrativeConsent?: string | null
  viewer?: { canManage?: boolean; canWrite?: boolean; narrativeProvider?: string } | null
}

type Coded = Error & { code?: string; retryable?: boolean }

/** `net/fetch.js`'s `jsonFetch`, reading this daemon's refusal shape as well as the original's. */
async function jsonFetch(path: string, options?: RequestInit): Promise<Record<string, unknown>> {
  let res: Response
  try {
    res = await fetch(path, options)
  } catch {
    const dead: Coded = new Error(T.webOffline)
    dead.code = "offline"
    throw dead
  }
  const body = await res.text()
  let data: Record<string, unknown> | null = null
  try {
    data = body ? JSON.parse(body) : null
  } catch {
    /* below */
  }
  if (!res.ok) {
    const raw = data?.error
    const err =
      typeof raw === "string"
        ? { code: raw, message: typeof data?.detail === "string" ? data.detail : raw }
        : raw && typeof raw === "object"
          ? (raw as { code?: string; message?: string })
          : { code: "http_" + res.status, message: res.statusText || T.webRequestFailed }
    const e2: Coded = new Error(err.message || err.code)
    e2.code = err.code
    throw e2
  }
  if (!data) throw new Error(T.webNotJSON)
  return data
}

const post = (body: unknown): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
})

/** `net/live.js`'s `board`: the catalog, one Project, or one item. The machine is this one. */
function board(project?: string | null, item?: string | null): Promise<Record<string, unknown>> {
  const query = new URLSearchParams()
  if (project) query.set("project", project)
  if (item) query.set("item", item)
  return jsonFetch("/v1/board" + (query.size ? "?" + query.toString() : ""))
}

/** `net/live.js`'s `boardItems`: one audience of one Project's cards, paged. */
function boardItems(project: string, audience?: string, cursor?: number, limit?: number): Promise<Record<string, unknown>> {
  const query = new URLSearchParams()
  query.set("project", project)
  query.set("audience", audience || "human")
  if (cursor) query.set("cursor", String(cursor))
  if (limit) query.set("limit", String(limit))
  return jsonFetch("/v1/board?" + query.toString())
}

/** `net/live.js`'s `boardCommand`. */
export function boardCommand(body: unknown): Promise<Record<string, unknown>> {
  return jsonFetch("/v1/board", post(body))
}

/** The fleet as `bridge.ts`'s `publish` left it: `S.sessions` in `main.js`. */
function sessions(): { id: string; sessionId?: string; machine?: string }[] {
  const rows = (S as Record<string, unknown>).sessions
  return Array.isArray(rows) ? rows : []
}

const UUID = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i
const conversationKey = (value: unknown) => (UUID.test(String(value || "")) ? String(value).toLowerCase() : String(value || ""))

/* ---- BoardControls.apply, the half this console has ---------------------- */

let latest: BoardMode | null = null
const listeners = new Set<() => void>()

/** The board mode as last applied: what the Settings block draws. */
export function boardMode(): BoardMode | null {
  return latest
}

export function subscribeBoardMode(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function words(en: string, zh: string): string {
  return /^zh/i.test(document.documentElement.lang || navigator.language || "") ? zh : en
}

/**
 * `BoardControls.apply`: only an answer that says whether the board is on, and
 * never an older revision over a newer one. A command's answer carries no
 * viewer of its own, so the last one read stands.
 */
export function applyBoardMode(answer: unknown): void {
  let next = answer as BoardMode | null
  if (!next || typeof next.enabled !== "boolean") return
  if (latest && next.revision < latest.revision) return
  if (!next.viewer && latest) next = { ...next, viewer: latest.viewer }
  latest = next
  const lede = document.getElementById("projects-lede")
  if (lede) {
    lede.textContent = next.enabled
      ? words("Choose a project to see its work, progress and results.", "選擇專案，了解正在進行的工作與已落地的成果。")
      : words("Directories an assistant has actually been run in, and that are still there.", "assistant 真的跑過、而且還在的目錄。")
  }
  document.documentElement.dataset.boardMode = next.enabled ? "board" : "standard"
  const row = document.getElementById("nav-board")
  if (row) row.hidden = true
  for (const listener of listeners) listener()
}

let reading: Promise<Record<string, unknown>> | null = null

/**
 * `BoardControls.refresh`: one read at a time, applied when it arrives. A
 * failure is the caller's to say — the Settings block says it in its status.
 */
export function refreshBoardMode(): Promise<Record<string, unknown>> {
  if (reading) return reading
  reading = board()
    .then((result) => {
      applyBoardMode(result.board)
      return result
    })
    .finally(() => {
      reading = null
    })
  return reading
}

/* ---- the page ----------------------------------------------------------- */

let bound: BoardPage | null = null
let host: BoardHost | null = null

/**
 * Bind the page once its markup is in the document, with `main.js`'s
 * environment less the assignment sheet (see the top of this file).
 */
export function bindBoard(doc: Document, pageHost: BoardHost): BoardPage {
  if (bound) return bound
  host = pageHost
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of BOARD_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  bound = (bindBoardPageOriginal as (elements: Record<string, HTMLElement | null>, environment: Record<string, unknown>) => BoardPage)(
    elements,
    {
      read: (project?: string, item?: string) => board(project, item),
      readItems: (project: string, audience: string, cursor: number, limit: number) =>
        boardItems(project, audience, cursor, limit),
      sessions,
      copy: (value: string) => navigator.clipboard.writeText(value),
      replaceURL: (value: string) => history.replaceState(null, "", new URL(value).hash),
      drawIcon,
      tint,
      navigate: (name: string) => pageHost.navigate(name),
      openSession: (id: string) => {
        const wanted = conversationKey(id)
        const matches = sessions().filter((row) => conversationKey(row.sessionId) === wanted)
        if (matches.length !== 1) return { error: matches.length ? "session_ambiguous" : "session_unavailable" }
        pageHost.openLive(matches[0].id)
        return undefined
      },
      onMode: applyBoardMode,
    },
  )
  return bound
}

/**
 * `BoardControls.open` (`main.js`): a Project with no id is the Projects page;
 * otherwise the page opens it and is shown. Answers false when the page has
 * not been bound, so the caller can do what it would without a Board.
 */
export function openBoard(project: string | null | undefined, item?: string | null, presentation?: unknown, machine?: string | null): boolean {
  if (!bound || !host) return false
  if (!project) {
    host.navigate("projects")
    return true
  }
  void bound.open(project, item, presentation, machine)
  host.navigate("board")
  return true
}

/** `enterProjectBoard`: the page with a Project, or the Projects page when there is none. */
export const enterProjectBoard = enterProjectBoardOriginal as (page: BoardPage, navigate: (name: string) => void) => Promise<unknown>

/**
 * `boardIntent` (`input/route.js`): whether a fragment is a Board link, and if
 * so its locator or the reason it has none. `#page=board` alone is not one.
 */
export function boardIntent(hash: string): { locator: BoardLocator | null; error: string | null } | null {
  const source = String(hash || "")
  if (!/(?:^|[#&])page=board(?:&|$)/.test(source)) return null
  if (source === "#page=board" || source === "page=board") return null
  const locator = (boardLocatorFromHashOriginal as (h: string) => BoardLocator | null)(source)
  return locator
    ? { locator, error: null }
    : { locator: null, error: "The Board link is malformed or carries an unsupported field." }
}
