// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogRefusalDetail } from "../../catalog.ts"

/**
 * The Ops documents panel's machine routes, as the frozen wire contract for
 * board Epic 84eeaa89 names them. The shapes are the wire's, field for field.
 *
 * Every call goes through `fetch` on the machine-relative `/v1/projects/...`
 * path, the same way `project-files-api.ts` does, so the Cloud carry seam can
 * map each route to its vocabulary word (`project-ops-*`) without this module
 * knowing which transport answered.
 *
 * Every write carries an `Idempotency-Key` minted once per person's action
 * (`IntentKeys` in ops-model.ts): a retry after a lost answer reuses it, so the
 * machine answers the first attempt's outcome instead of acting twice.
 */

export type OpsEntryType = "markdown" | "text" | "file" | "notion_page" | "google_doc" | "google_sheet" |
  "google_slides" | "google_form" | "google_drive" | "web"
export type OpsEntryStatus = "ready" | "resolving" | "needs_access" | "missing" | "unreadable"
export type OpsTodoState = "unsent" | "sent" | "read" | "done" | "missing"

export interface OpsResolve { terminal: string; todo: string; todo_state: OpsTodoState }

export interface OpsEntry {
  id: string
  kind: "file" | "link"
  title: string
  title_source: "user" | "agent" | "file"
  collection: string
  position: number
  path?: string
  url?: string
  type: OpsEntryType
  status: OpsEntryStatus
  access_note?: string
  resolve?: OpsResolve
  created_at: number
  updated_at: number
}

export interface OpsCollection { id: string; name: string; position: number }

export type OpsVisibility = "public" | "private" | "unknown" | "unchecked"

export interface OpsSetup {
  folder: "clawdline-ops"
  folder_exists: boolean
  git: "repo" | "none"
  excluded: boolean
  visibility: OpsVisibility
  visibility_detail?: string
}

export interface OpsList { project_key: string; collections: OpsCollection[]; entries: OpsEntry[] }
export interface OpsRead { entry: OpsEntry; content?: string; version?: string }

export type OpsCreate =
  | { kind: "link"; url: string; title?: string; collection?: string }
  | { kind: "file"; path: string; title?: string; collection?: string }
  | { kind: "markdown"; name: string; content: string; collection?: string; git?: "include" | "exclude" }
  | { kind: "collection"; name: string }

export type OpsEntryPatch =
  | { title?: string; collection?: string; position?: number }
  | { rename: string }
  | { content: string; expected_version: string }

export class OpsError extends Error {
  readonly code: string
  readonly status: number | undefined
  /** The write may have happened: its answer was lost or the machine said `outcome: unknown`. */
  readonly uncertain: boolean
  constructor(code: string, message: string, status?: number, uncertain = false) {
    super(message)
    this.code = code
    this.status = status
    this.uncertain = uncertain
  }
}

async function answer<T>(url: string, init?: RequestInit): Promise<T> {
  const write = !!init?.method && init.method !== "GET"
  let response: Response
  try { response = await fetch(url, { credentials: "same-origin", ...init }) }
  catch { throw new OpsError("network", "network", undefined, write) }
  let data: any
  try { data = await response.json() }
  catch { throw new OpsError("invalid_response", "invalid_response", response.status, write) }
  if (!response.ok) {
    const code = typeof data?.error === "string" ? data.error : data?.error?.code || "unavailable"
    const detail = catalogRefusalDetail(data)?.text
    const uncertain = write && (data?.outcome === "unknown" || data?.error?.outcome === "unknown" || response.status >= 500)
    throw new OpsError(code, detail || code, response.status, uncertain)
  }
  return data as T
}

const base = (place: string) => `/v1/projects/${encodeURIComponent(place)}/ops`
const item = (place: string, id: string) => `${base(place)}/${encodeURIComponent(id)}`
const send = (method: string, body: unknown, key: string): RequestInit => ({
  method,
  headers: { "Content-Type": "application/json", "Idempotency-Key": key },
  body: JSON.stringify(body),
})

export const listOps = (place: string) => answer<OpsList>(base(place))
export const readOpsEntry = (place: string, id: string) => answer<OpsRead>(item(place, id))
export const readOpsSetup = (place: string, probeGitHub = false) =>
  answer<OpsSetup>(`${base(place)}/setup${probeGitHub ? "?probe=github" : ""}`)
export const setOpsGit = (place: string, git: "include" | "exclude", key: string) =>
  answer<OpsSetup>(`${base(place)}/setup`, send("PATCH", { git }, key))
export const createOps = (place: string, body: OpsCreate, key: string) =>
  answer<{ entry?: OpsEntry; collection?: OpsCollection }>(base(place), send("POST", body, key))
export const updateOpsEntry = (place: string, id: string, patch: OpsEntryPatch, key: string) =>
  answer<{ entry: OpsEntry; version?: string }>(item(place, id), send("PATCH", patch, key))
export const updateOpsCollection = (place: string, id: string, patch: { name?: string; position?: number }, key: string) =>
  answer<{ collection: OpsCollection }>(item(place, id), send("PATCH", patch, key))
export const deleteOps = (place: string, id: string, key: string) =>
  answer<{ ok: true; deleted: string }>(item(place, id), { method: "DELETE", headers: { "Idempotency-Key": key } })
export const requestOpsResolve = (place: string, id: string, terminal: string, key: string) =>
  answer<{ entry: OpsEntry; todo: { id: string } }>(`${item(place, id)}/resolve-request`, send("POST", { terminal }, key))
