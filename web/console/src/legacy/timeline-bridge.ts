// The Project Timeline's way into its copied module.
//
// `js/view/timeline.js` is the Swift app's, byte for byte, and carries its own
// English and Chinese rather than reading the catalog.
//
// What differs, and why:
//
//   - `command` is the Swift app's write path. This daemon has none: the
//     Timeline is a projection of records it already keeps (design-decisions
//     D01, D41), so there is nothing to switch on or off. The module only ever
//     calls it from `settings-timeline-toggle`, which is not in this console's
//     markup; it is supplied all the same, refusing by name, because a
//     function that is missing fails differently from one that says no;
//   - `openBoard` is the Board page, as there: an entry's work-item pills and
//     the Back button lead back to the Project's board;
//   - a refusal on this daemon is `{ error: "code", detail }`, which
//     `jsonFetch` reads as well as the original's shape.
import { T } from "./js/core/i18n.js"
import { bindTimelinePage } from "./js/view/timeline.js"

/** What `bindTimelinePage` hands back. */
export interface TimelinePage {
  enter(project?: string | null, presentation?: unknown): Promise<unknown>
  leave(): void
  refresh(cursor?: string | null): Promise<unknown>
  escape(): boolean
  state: { projectId: string | null; entryId: string | null }
}

/** `main.js`'s element table for `bindTimelinePage`, in its order. */
export const TIMELINE_ELEMENT_IDS = [
  "timeline",
  "timeline-back",
  "timeline-refresh",
  "timeline-board-tab",
  "timeline-title",
  "timeline-project-mark",
  "timeline-subtitle",
  "timeline-status",
  "timeline-environment",
  "timeline-category",
  "timeline-upcoming",
  "timeline-items",
  "timeline-detail",
  "timeline-environment-label",
  "timeline-category-label",
  "timeline-upcoming-label",
] as const

type Coded = Error & { code?: string; retryable?: boolean }

/** `net/fetch.js`'s `jsonFetch`, reading this daemon's refusal shape as well as the original's. */
async function jsonFetch(path: string): Promise<Record<string, unknown>> {
  let res: Response
  try {
    res = await fetch(path)
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
    const failed: Coded = new Error(err.message || err.code || "")
    failed.code = err.code
    throw failed
  }
  if (!data) throw new Error(T.webNotJSON)
  return data
}

/** `net/live.js`'s `timeline`, with this route's closed parameter set. */
function read(
  project: string,
  entry: string | null,
  cursor: string | null,
  environment: string,
  category: string | null,
  includeUpcoming: boolean,
): Promise<Record<string, unknown>> {
  const query = new URLSearchParams()
  query.set("project", project)
  if (entry) query.set("entry", entry)
  if (cursor) query.set("cursor", cursor)
  if (environment) query.set("environment", environment)
  if (category) query.set("category", category)
  query.set("upcoming", includeUpcoming ? "true" : "false")
  return jsonFetch("/v1/timeline?" + query.toString())
}

/** Bind the page, as `main.js` binds it. */
export function bindTimeline(doc: Document, openBoard: (project: string | null, item: string | null) => void): TimelinePage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of TIMELINE_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  return bindTimelinePage(elements, {
    read,
    command: () => {
      const refused: Coded = new Error("This daemon keeps no Timeline of its own to switch on or off.")
      refused.code = "timeline_read_only"
      refused.retryable = false
      return Promise.reject(refused)
    },
    openBoard,
  }) as TimelinePage
}

// Which Project the Timeline was last asked for, and by whom.
//
// `main.js` keeps the same thing in `timelineRequested`: the Board's tab hands
// the page a Project and then changes page, and the page reads it on arrival.
// It is here rather than in the page's own file so that the Board can ask
// without importing the page — the two are separate files on purpose, which is
// what let this page and the ledger be built at once.
let requested: string | null = null
let cameFrom = "projects"

/**
 * `main.js`'s `timeline.enter(project, …)`, less the arrival.
 *
 * `from` is this repository's own addition and it is what let the Timeline
 * stop hanging off a dead page (work-system-review §5.2, W6). The Swift app
 * had exactly one way in — the old Board's tab — so "back" could be written
 * into the page. It now also opens from a work item, and a page that sent a
 * reader somewhere they did not come from is worse than one with no way back
 * at all: they lose their place and are shown a screen that, on this machine,
 * is empty. The old Board's own call leaves it alone and keeps its way back.
 */
export function openTimeline(project: string | null, from = "projects"): void {
  requested = project && project.trim() ? project : null
  cameFrom = from
}

/** The Project the page should enter, or null when nobody named one. */
export function requestedTimeline(): string | null {
  return requested
}

/** The page a reader of the Timeline should be given back to. */
export function timelineReturn(): string {
  return cameFrom
}
