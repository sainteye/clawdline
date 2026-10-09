// The remaining compatibility seams for the retired Project Board.
//
// The page itself is gone. Two consumers remain deliberately separate from
// it: Settings still reads and writes the board-level enabled setting,
// and the byte-for-byte Projects view still calls `openBoard` for catalog rows
// that carry an old board Project id. The latter is redirected to the Projects
// page's own repository detail; it never opens or reads an old card.
import { T } from "./js/core/i18n.js"
import { machineWording } from "./machine-copy.js"
import { makeJSONFetch } from "@clawdline/core/refusal"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../catalog.ts"

/** The part of a board answer the Settings block reads. */
export interface BoardMode {
  enabled: boolean
  revision: number
  narrativeConsent?: string | null
  viewer?: { canManage?: boolean; canWrite?: boolean; narrativeProvider?: string } | null
}

/** Read this daemon's flat or nested refusal shape through the shared transport. */
const jsonFetch = makeJSONFetch({
  words: { offline: machineWording(T.webOffline, "en"), requestFailed: T.webRequestFailed, notJSON: T.webNotJSON },
})

const post = (body: unknown): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
})

/** The settings-only board read. No old card selector is sent. */
function board(): Promise<Record<string, unknown>> {
  return jsonFetch("/v1/board")
}

/** The board-level settings command retained by Settings. */
export function boardCommand(body: unknown): Promise<Record<string, unknown>> {
  return jsonFetch("/v1/board", post(body))
}

let latest: BoardMode | null = null
const listeners = new Set<() => void>()

export function boardMode(): BoardMode | null {
  return latest
}

export function subscribeBoardMode(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** Apply only an answer that names the mode, and never an older revision. */
export function applyBoardMode(answer: unknown): void {
  let next = answer as BoardMode | null
  if (!next || typeof next.enabled !== "boolean") return
  if (latest && next.revision < latest.revision) return
  if (!next.viewer && latest) next = { ...next, viewer: latest.viewer }
  latest = next
  const lede = document.getElementById("projects-lede")
  if (lede) {
    lede.textContent = next.enabled
      ? catalogWord("projects", "boardLede")
      : catalogWord("next", "projectsLede")
  }
  document.documentElement.dataset.boardMode = next.enabled ? "board" : "standard"
  for (const listener of listeners) listener()
}

let reading: Promise<Record<string, unknown>> | null = null

/** One settings read at a time; the caller owns its refusal sentence. */
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

export interface RetiredBoardProject {
  id?: string
  boardProjectId?: string
  [key: string]: unknown
}

let openProjectDetail: ((project: RetiredBoardProject) => void) | null = null

/** Connect the copied Projects module's old seam to its own repository detail. */
export function registerRetiredBoardProjectFallback(open: (project: RetiredBoardProject) => void): () => void {
  openProjectDetail = open
  return () => {
    if (openProjectDetail === open) openProjectDetail = null
  }
}

/**
 * Compatibility name called only by the byte-for-byte Projects module bridge.
 * The Board page no longer exists: the old association is discarded before
 * the Projects page opens the same repository as an ordinary Project.
 */
export function openBoard(
  _project: string | null | undefined,
  _item?: string | null,
  presentation?: unknown,
  _machine?: string | null,
): boolean {
  if (!openProjectDetail || !presentation || typeof presentation !== "object") return false
  openProjectDetail(presentation as RetiredBoardProject)
  return true
}
