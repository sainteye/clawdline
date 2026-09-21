// The Projects page's way into its copied modules.
//
// `js/view/projects.js` and `js/view/worktrees.js` are the Swift app's, byte for
// byte. They are bound here as that app's `main.js` binds them: one table of the
// page's elements by id, and an environment whose reads are this daemon's routes
// spelled as the original's `api` spells them (`net/live.js`).
//
// What differs, and why:
//
// - `board` reads `GET /v1/projects`, this daemon's name for the Board store's
//   Project catalog, and hands the module the `{ board }` answer it expects. The
//   original's `/v1/board` is the whole Board; this daemon's `/v1/board` is a
//   different, older reading and is not touched here.
// - `openBoard` is `BoardControls.open(place.boardProjectId, null, place)`, as
//   there: a row that names a Board Project opens it on the Board page
//   (`board-bridge.ts`). A row with no Board Project opens the Project here.
// - `openWorktreeOwner` is absent: the original opens the owner in the Board's
//   session viewer, which this console does not have. The owner button is still
//   drawn, as the module draws it, and does nothing.
// - A refusal on this daemon is `{ error: "code", detail }`, not
//   `{ error: { code, message } }`; `jsonFetch` below reads both, and — as the
//   original's — sets no `status`, so the sentences that append one do not.
import { T } from "./js/core/i18n.js"
import { drawIcon } from "./js/core/pixels.js"
import { tint } from "./js/core/util.js"
import { bindProjectsPage as bindProjectsPageOriginal, readProjectPlaces } from "./js/view/projects.js"
import { openBoard } from "./board-bridge.js"
import { projectFeatureMeasurementWords } from "../pages/projects/measurement.js"

/** `net/client.js`'s LOCAL_MACHINE: a page served by this daemon is looking at this machine. */
const LOCAL_MACHINE = "this-mac"

/** What `bindProjectsPage` hands back. */
export interface ProjectsPage {
  enter(): Promise<void>
  leave(): void
  escape(): void
  state: { view: "list" | "detail" }
}

/** `main.js`'s element table for `bindProjectsPage`, in its order. */
export const PROJECTS_ELEMENT_IDS = [
  "projects",
  "projects-list-view",
  "projects-detail-view",
  "projects-title", "projects-count",
  "projects-status", "projects-rows",
  "projects-back",
  "project-mark", "project-name",
  "project-path", "project-status",
  "project-truncated",
  "project-delivered",
  "project-delivered-count",
  "project-delivered-title",
  "project-delivered-say",
  "project-delivered-list",
  "project-delivered-none",
  "project-none", "project-groups",
  "project-excluded",
  "project-unattributed",
  "project-unattributed-title",
  "project-unattributed-say",
  "project-read",
  "project-worktree-lifecycle",
  "project-worktree-status",
  "project-worktree-summary",
  "project-worktree-rows",
  "project-worktree-refresh",
] as const

type Coded = Error & { code?: string }

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

type Place = { id: string; boardProjectId?: string; path?: string; machine?: string }

const post = (body: unknown): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
})

let measuredProjectWords: { empty: string; read: string } | null = null
let unmeasuredProjectWords: { empty: string; read: string } | null = null

function applyProjectFeatureMeasurementWords(doc: Document, answer: Record<string, unknown>): void {
  const project = answer.projectWorktrees as { status?: unknown; read?: { featureRowsStatus?: unknown } } | undefined
  const unmeasured = project?.status === "not_measured" || project?.read?.featureRowsStatus === "not_measured"
  // Remember the active catalog only while it is not our replacement. This
  // preserves the loaded locale if a later machine really does measure the
  // field and the copied view should return to its original sentence.
  if (!unmeasuredProjectWords || T.webProjectNoWorktrees !== unmeasuredProjectWords.empty) {
    measuredProjectWords = { empty: T.webProjectNoWorktrees, read: T.webProjectRead }
  }
  if (!unmeasured) {
    if (measuredProjectWords) {
      T.webProjectNoWorktrees = measuredProjectWords.empty
      T.webProjectRead = measuredProjectWords.read
    }
    return
  }
  const chinese = /^zh/i.test(doc.documentElement.lang || navigator.language || "")
  unmeasuredProjectWords = projectFeatureMeasurementWords(chinese)
  T.webProjectNoWorktrees = unmeasuredProjectWords.empty
  T.webProjectRead = unmeasuredProjectWords.read
}

/** The transport `readProjectPlaces` reads, as `net/live.js` spells it. */
const transport = {
  places: (machine?: string) => {
    if (machine !== undefined && machine !== LOCAL_MACHINE) {
      return Promise.reject(Object.assign(new Error("This machine is not available."), { code: "machine_unavailable" }))
    }
    return jsonFetch("/v1/places")
  },
  /** The Board catalog, in the envelope `readProjectPlaces` checks: `available`, `enabled`, `projects`. */
  board: async () => {
    const answer = await jsonFetch("/v1/projects")
    const catalog = (answer.catalog ?? {}) as Record<string, unknown> & {
      available?: boolean
      readState?: { error?: { code: string; message: string } }
    }
    const board = catalog.available === false
      ? { ...catalog, error: catalog.readState?.error ?? { code: "board_unavailable", message: "Board unavailable" } }
      : catalog
    return { board }
  },
  projectWorktrees: (project: Place | string) => {
    const path = typeof project === "object" ? project.path : project
    return jsonFetch("/v1/orchestrator/usage/project-worktrees?project=" + encodeURIComponent(path ?? ""))
  },
  projectWorktreeLifecycle: async (project: string) => {
    const answer = await jsonFetch("/v1/projects/" + encodeURIComponent(project) + "/worktrees")
    return { ...answer, machine: LOCAL_MACHINE }
  },
  projectWorktreeLifecycleRefresh: async (project: string) => {
    const answer = await jsonFetch("/v1/projects/" + encodeURIComponent(project) + "/worktrees/refresh", post({}))
    return { ...answer, machine: LOCAL_MACHINE }
  },
}

/**
 * The part of `BoardControls.apply` (`input/board-settings.js`) that this page
 * shows: its lede, and the mode on the root. The drawer rows it also hides are
 * App's, and the Settings toggle is not on this console.
 */
function applyBoardMode(board: { enabled?: unknown }): void {
  if (typeof board?.enabled !== "boolean") return
  const zh = /^zh/i.test(document.documentElement.lang || navigator.language || "")
  const words = (en: string, chinese: string) => (zh ? chinese : en)
  const lede = document.getElementById("projects-lede")
  if (lede) {
    lede.textContent = board.enabled
      ? words("Choose a project to see its work, progress and results.", "選擇專案，了解正在進行的工作與已落地的成果。")
      : words("Directories an assistant has actually been run in, and that are still there.", "assistant 真的跑過、而且還在的目錄。")
  }
  document.documentElement.dataset.boardMode = board.enabled ? "board" : "standard"
}

/**
 * Bind the page once its markup is in the document, with `main.js`'s
 * environment less the two surfaces this console does not have.
 */
export function bindProjects(doc: Document, navigate: (name: string) => void): ProjectsPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of PROJECTS_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  return (bindProjectsPageOriginal as (elements: Record<string, HTMLElement | null>, environment: Record<string, unknown>) => ProjectsPage)(
    elements,
    {
      carries: () => true,
      places: () => (readProjectPlaces as (t: unknown, onMode?: unknown) => Promise<unknown>)(transport, applyBoardMode),
      openBoard: (place: Place) => openBoard(place.boardProjectId, null, place),
      projectWorktrees: async (place: Place) => {
        const answer = await transport.projectWorktrees(place)
        // The copied view reads these two keys in its continuation of this
        // promise, so the replacement is in place before the first paint.
        applyProjectFeatureMeasurementWords(doc, answer)
        return answer
      },
      lifecycleAvailable: () => true,
      projectWorktreeLifecycle: (place: Place) => transport.projectWorktreeLifecycle(place.boardProjectId || place.id),
      projectWorktreeLifecycleRefresh: (place: Place) =>
        transport.projectWorktreeLifecycleRefresh(place.boardProjectId || place.id),
      drawIcon,
      tint,
      navigate,
    },
  )
}

/** `static.js`'s three words for this page, painted once the catalog is in. */
export function paintProjectsStatic(doc: Document): void {
  const text = (id: string, value: string | undefined) => {
    const node = doc.getElementById(id)
    if (node && value) node.textContent = value
  }
  const strings = T as Record<string, string>
  text("projects-title", strings.webProjects)
  text("projects-lede", strings.webProjectsLede)
  if (strings.webProjects) text("projects-back", "‹ " + strings.webProjects)
}
