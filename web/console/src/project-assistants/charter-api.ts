// The one place the console reads and writes a Project's team charter.
//
// Every call is one route under /v1/project-assistant-charters (API 122), sent
// through the machine's captured source: locally over HTTP, from the phone as
// the project-assistants.charter* Cloud words. The screens see the generated
// contract shapes and a typed refusal, never a transport.
import type * as W from "@clawdline/contract"
import type { CharterRequestBody, SourceClient } from "./client"

/** A Project on one machine. */
export interface CharterProject { machine_id: string; project_id: string }

/** One roster row: the listing's version and proposals, with how many Bots use it. */
export interface CharterSummary {
  project: CharterProject
  version: number
  pending_proposals: number
  /** Visible Bots of the Project, and how many worked under the current version last. */
  bots: number
  readers_current: number
  /** The visible Bots' names, for the template's first section. */
  names: string[]
}

export type CharterRefusalCode = "version_conflict" | "too_large" | "not_found" | "unknown"

/**
 * A typed refusal. A stale write names `field`: charter_version for a save or
 * restore, charter_proposal_base for a proposal written against an older version.
 * The source does not say which version is current; the screen rereads.
 */
export class CharterRefusal extends Error {
  readonly code: CharterRefusalCode
  readonly field: string
  readonly source: string
  constructor(code: CharterRefusalCode, field = "", source = "") {
    super(code)
    this.name = "CharterRefusal"
    this.code = code
    this.field = field
    this.source = source
  }
}

export function charterRefusalCode(error: unknown): CharterRefusalCode {
  return error instanceof CharterRefusal ? error.code : "unknown"
}

/** Translates the source's refusal envelope; anything else stays unknown. */
export function charterRefusal(error: unknown): CharterRefusal | unknown {
  if (error instanceof CharterRefusal || (error as { name?: unknown } | null)?.name === "AbortError") return error
  const refusal = (error as Partial<W.ProjectAssistantErrorResponse> | null)?.error
  const code = refusal?.code ?? "", field = refusal?.field ?? ""
  if (code === "pa_stale_version") return new CharterRefusal("version_conflict", field, code)
  if (code === "pa_capacity_full" && (field === "charter_text" || field === "charter_request" || field === "charter_reason")) return new CharterRefusal("too_large", field, code)
  if (code === "pa_not_found") return new CharterRefusal("not_found", field, code)
  return new CharterRefusal("unknown", field, code || "unavailable")
}

export interface CharterApi {
  list(signal: AbortSignal): Promise<W.ProjectAssistantChartersResponse>
  /** version 0 reads the current charter only; a positive version also answers it as `selected`. */
  read(project: CharterProject, version: number, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  save(project: CharterProject, request: { expected_version: number; text: string; reason: string }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  restore(project: CharterProject, request: { expected_version: number; version: number }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  readers(project: CharterProject, signal: AbortSignal): Promise<W.ProjectAssistantCharterReadersResponse>
  proposals(project: CharterProject, signal: AbortSignal): Promise<W.ProjectAssistantCharterProposalsResponse>
  applyProposal(project: CharterProject, id: string, request: { expected_version: number }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  dismissProposal(project: CharterProject, id: string, signal: AbortSignal): Promise<W.ProjectAssistantCharterProposalsResponse>
  /** The person's words to the machine's lead, which may answer with a proposal. */
  requestChange(project: CharterProject, request: { text: string }, signal: AbortSignal): Promise<W.ProjectAssistantCharterChangeResponse>
}

export const chartersPath = "/v1/project-assistant-charters"
type Send = <T>(path: string, method: "GET" | "PUT" | "POST", body: CharterRequestBody | undefined, signal: AbortSignal) => Promise<T>

const query = (fields: Record<string, string | number>) => "?" + new URLSearchParams(Object.entries(fields).map(([key, value]) => [key, String(value)])).toString()
const newKey = () => typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : Math.random().toString(36).slice(2)

/** The routes, built over any sender with the source's request semantics. */
export function charterRoutes(machineID: string, send: Send): CharterApi {
  const call = async <T>(path: string, method: "GET" | "PUT" | "POST", body: CharterRequestBody | undefined, signal: AbortSignal) => {
    try { return await send<T>(path, method, body, signal) } catch (error) { throw charterRefusal(error) }
  }
  const own = (project: CharterProject) => {
    if (project.machine_id !== machineID) throw new CharterRefusal("not_found", "machine_id")
    return { machine_id: machineID, project_id: project.project_id }
  }
  const write = <T extends CharterRequestBody>(project: CharterProject, fields: Omit<T, "machine_id" | "project_id" | "idempotency_key">) => ({ ...own(project), idempotency_key: newKey(), ...fields }) as T
  const routes: CharterApi = {
    list: signal => call(chartersPath + query({ machine_id: machineID }), "GET", undefined, signal),
    read: (project, version, signal) => call(chartersPath + "/charter" + query({ ...own(project), version }), "GET", undefined, signal),
    // Over Cloud a save always names its reason, so it is always sent.
    save: (project, request, signal) => call(chartersPath + "/charter", "PUT", write<W.ProjectAssistantCharterSaveRequest>(project, { expected_version: request.expected_version, text: request.text, reason: request.reason }), signal),
    restore: (project, request, signal) => call(chartersPath + "/restore", "POST", write<W.ProjectAssistantCharterRestoreRequest>(project, { expected_version: request.expected_version, version: request.version }), signal),
    readers: (project, signal) => call(chartersPath + "/readers" + query(own(project)), "GET", undefined, signal),
    proposals: (project, signal) => call(chartersPath + "/proposals" + query(own(project)), "GET", undefined, signal),
    applyProposal: (project, id, request, signal) => call(chartersPath + "/proposal-apply", "POST", write<W.ProjectAssistantCharterApplyRequest>(project, { proposal_id: id, expected_version: request.expected_version }), signal),
    dismissProposal: (project, id, signal) => call(chartersPath + "/proposal-dismiss", "POST", write<W.ProjectAssistantCharterDismissRequest>(project, { proposal_id: id }), signal),
    requestChange: (project, request, signal) => call(chartersPath + "/change-request", "POST", write<W.ProjectAssistantCharterChangeRequest>(project, { text: request.text }), signal),
  }
  // A refusal found while shaping a request (another machine's Project) is a rejected promise too.
  return Object.fromEntries(Object.entries(routes).map(([name, route]) => [name, async (...args: unknown[]) => (route as (...a: unknown[]) => unknown)(...args)])) as unknown as CharterApi
}

/** The charter client for one machine, or undefined when its source cannot answer charters. */
export function charterApiFor(source: SourceClient | undefined): CharterApi | undefined {
  if (!source || source.canCharters?.() !== true || typeof source.charterRequest !== "function") return undefined
  // One client per source object, so a screen holding it does not reread on every render.
  let api = clients.get(source)
  if (!api) {
    api = charterRoutes(source.machineID, (path, method, body, signal) => source.charterRequest!(path, method, body, signal))
    clients.set(source, api)
  }
  return api
}
const clients = new WeakMap<SourceClient, CharterApi>()

export const charterProjectKey = (project: CharterProject) => JSON.stringify([project.machine_id, project.project_id])

/** Visible readers only: a hidden Bot is never named on the page. */
export const shownReaders = (answer: W.ProjectAssistantCharterReadersResponse) => answer.readers.filter(reader => !reader.hidden)

/** Every Project of the listing, with or without a charter (version 0), with its Bots. */
export async function charterSummaries(api: CharterApi, machineID: string, signal: AbortSignal): Promise<CharterSummary[]> {
  const listing = await api.list(signal)
  const rows = [...listing.charters, ...listing.projects_without_charter.map(project_id => ({ project_id, version: 0, pending_proposals: 0 }))]
  return Promise.all(rows.map(async row => {
    const project = { machine_id: machineID, project_id: row.project_id }
    let names: string[] = [], current = 0
    try {
      const readers = shownReaders(await api.readers(project, signal))
      names = readers.map(reader => reader.name)
      current = readers.filter(reader => reader.version === row.version).length
    } catch (error) { if ((error as { name?: unknown } | null)?.name === "AbortError") throw error }
    return { project, version: row.version, pending_proposals: row.pending_proposals, bots: names.length, readers_current: current, names }
  }))
}
