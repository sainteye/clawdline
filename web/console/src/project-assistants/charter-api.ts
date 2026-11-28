// The one place the console reads and writes a Project's team charter.
//
// Every call is one route under /v1/project-assistant-charters (API 122; removal
// API 123; the Bots' conflict reports API 124; playbooks API 127; the workflow,
// its proposals and the change-request target API 128), sent
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
  /** Open conflict reports from the Project's Bots; 0 from a machine older than API 124. */
  open_conflicts: number
  /** Visible Bots of the Project, and how many have heard what is current for them. */
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
  if (code === "pa_capacity_full" && (field === "charter_text" || field === "charter_request" || field === "charter_reason" || field.startsWith("charter_playbook") || field === "charter_document" || field.startsWith("charter_workflow"))) return new CharterRefusal("too_large", field, code)
  if (code === "pa_not_found") return new CharterRefusal("not_found", field, code)
  return new CharterRefusal("unknown", field, code || "unavailable")
}

export interface CharterApi {
  list(signal: AbortSignal): Promise<W.ProjectAssistantChartersResponse>
  /** version 0 reads the current charter only; a positive version also answers it as `selected`. */
  read(project: CharterProject, version: number, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  save(project: CharterProject, request: { expected_version: number; text: string; reason: string }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  restore(project: CharterProject, request: { expected_version: number; version: number }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  /** Takes the charter away at the version read; undefined on a machine older than API 123. */
  remove?(project: CharterProject, request: { expected_version: number }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  readers(project: CharterProject, signal: AbortSignal): Promise<W.ProjectAssistantCharterReadersResponse>
  proposals(project: CharterProject, signal: AbortSignal): Promise<W.ProjectAssistantCharterProposalsResponse>
  applyProposal(project: CharterProject, id: string, request: { expected_version: number; playbook_id?: string }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  dismissProposal(project: CharterProject, id: string, signal: AbortSignal): Promise<W.ProjectAssistantCharterProposalsResponse>
  /** The Bots' reports where their own sentences disagree with the charter; undefined before API 124. */
  conflicts?(project: CharterProject, signal: AbortSignal): Promise<W.ProjectAssistantCharterConflictsResponse>
  /** The person's choice for one report; answers the reports after it. */
  resolveConflict?(project: CharterProject, request: { conflict_id: string; action: W.ProjectAssistantCharterConflictAction }, signal: AbortSignal): Promise<W.ProjectAssistantCharterConflictsResponse>
  /**
   * Creates (no id) or replaces (id) one playbook as the next version; the text
   * and the other playbooks are kept. Undefined before API 127.
   */
  savePlaybook?(project: CharterProject, request: { expected_version: number; playbook: CharterPlaybookDraft; reason?: string }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  /** Removes one playbook as the next version. Undefined before API 127. */
  removePlaybook?(project: CharterProject, request: { expected_version: number; id: string }, signal: AbortSignal): Promise<W.ProjectAssistantCharterResponse>
  /**
   * The person's words to the machine's lead, which may answer with a proposal.
   * target "workflow" asks for a workflow proposal; a machine older than API 128
   * refuses it before anything is sent (field target), so ask workflow first.
   */
  requestChange(project: CharterProject, request: { text: string; target?: CharterChangeTarget }, signal: AbortSignal): Promise<W.ProjectAssistantCharterChangeResponse>
  /** Whether this machine answers workflows, workflow proposals and the change-request target (API 128). */
  workflow: boolean
}

/** Which part of the charter a change request is for. */
export type CharterChangeTarget = "text" | "workflow"

/** A playbook as the person writes it; no id creates one. */
export interface CharterPlaybookDraft { id?: string; title: string; text: string; bot_ids: string[] }

export const chartersPath = "/v1/project-assistant-charters"
type Send = <T>(path: string, method: "GET" | "PUT" | "POST", body: CharterRequestBody | undefined, signal: AbortSignal) => Promise<T>

const query = (fields: Record<string, string | number>) => "?" + new URLSearchParams(Object.entries(fields).map(([key, value]) => [key, String(value)])).toString()
const newKey = () => typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : Math.random().toString(36).slice(2)

/** The routes, built over any sender with the source's request semantics. */
export function charterRoutes(machineID: string, send: Send, options: { remove?: boolean; conflicts?: boolean; playbooks?: boolean; workflow?: boolean } = {}): CharterApi {
  const call = async <T>(path: string, method: "GET" | "PUT" | "POST", body: CharterRequestBody | undefined, signal: AbortSignal) => {
    try { return await send<T>(path, method, body, signal) } catch (error) { throw charterRefusal(error) }
  }
  const own = (project: CharterProject) => {
    if (project.machine_id !== machineID) throw new CharterRefusal("not_found", "machine_id")
    return { machine_id: machineID, project_id: project.project_id }
  }
  const write = <T extends CharterRequestBody>(project: CharterProject, fields: Omit<T, "machine_id" | "project_id" | "idempotency_key">) => ({ ...own(project), idempotency_key: newKey(), ...fields }) as T
  const routes: Omit<CharterApi, "workflow"> = {
    list: signal => call(chartersPath + query({ machine_id: machineID }), "GET", undefined, signal),
    read: (project, version, signal) => call(chartersPath + "/charter" + query({ ...own(project), version }), "GET", undefined, signal),
    // Over Cloud a save always names its reason, so it is always sent.
    save: (project, request, signal) => call(chartersPath + "/charter", "PUT", write<W.ProjectAssistantCharterSaveRequest>(project, { expected_version: request.expected_version, text: request.text, reason: request.reason }), signal),
    restore: (project, request, signal) => call(chartersPath + "/restore", "POST", write<W.ProjectAssistantCharterRestoreRequest>(project, { expected_version: request.expected_version, version: request.version }), signal),
    ...(options.remove ? { remove: (project: CharterProject, request: { expected_version: number }, signal: AbortSignal) => call<W.ProjectAssistantCharterResponse>(chartersPath + "/remove", "POST", write<W.ProjectAssistantCharterRemoveRequest>(project, { expected_version: request.expected_version }), signal) } : {}),
    ...(options.conflicts ? {
      conflicts: (project: CharterProject, signal: AbortSignal) => call<W.ProjectAssistantCharterConflictsResponse>(chartersPath + "/conflicts" + query(own(project)), "GET", undefined, signal),
      resolveConflict: (project: CharterProject, request: { conflict_id: string; action: W.ProjectAssistantCharterConflictAction }, signal: AbortSignal) =>
        call<W.ProjectAssistantCharterConflictsResponse>(chartersPath + "/conflict-resolve", "POST", write<W.ProjectAssistantCharterConflictResolveRequest>(project, { conflict_id: request.conflict_id, action: request.action }), signal),
    } : {}),
    ...(options.playbooks ? {
      // An empty id is left out: the daemon creates a playbook only when none is named.
      savePlaybook: (project: CharterProject, request: { expected_version: number; playbook: CharterPlaybookDraft; reason?: string }, signal: AbortSignal) => {
        const { id, title, text, bot_ids } = request.playbook
        return call<W.ProjectAssistantCharterResponse>(chartersPath + "/playbook", "PUT", write<W.ProjectAssistantCharterPlaybookSaveRequest>(project,
          { expected_version: request.expected_version, playbook: { ...(id ? { id } : {}), title, text, bot_ids: [...bot_ids] }, reason: request.reason ?? "" }), signal)
      },
      removePlaybook: (project: CharterProject, request: { expected_version: number; id: string }, signal: AbortSignal) =>
        call<W.ProjectAssistantCharterResponse>(chartersPath + "/playbook-remove", "POST", write<W.ProjectAssistantCharterPlaybookRemoveRequest>(project, { expected_version: request.expected_version, id: request.id }), signal),
    } : {}),
    readers: (project, signal) => call(chartersPath + "/readers" + query(own(project)), "GET", undefined, signal),
    proposals: (project, signal) => call(chartersPath + "/proposals" + query(own(project)), "GET", undefined, signal),
    applyProposal: (project, id, request, signal) => call(chartersPath + "/proposal-apply", "POST", write<W.ProjectAssistantCharterApplyRequest>(project, { proposal_id: id, expected_version: request.expected_version, ...(request.playbook_id ? { playbook_id: request.playbook_id } : {}) }), signal),
    dismissProposal: (project, id, signal) => call(chartersPath + "/proposal-dismiss", "POST", write<W.ProjectAssistantCharterDismissRequest>(project, { proposal_id: id }), signal),
    // The text target is the default and is left out, so an older machine reads the same request.
    requestChange: (project, request, signal) => {
      if (request.target === "workflow" && !options.workflow) throw new CharterRefusal("unknown", "target")
      return call(chartersPath + "/change-request", "POST", write<W.ProjectAssistantCharterChangeRequest>(project, { text: request.text, ...(request.target === "workflow" ? { target: "workflow" } : {}) }), signal)
    },
  }
  // A refusal found while shaping a request (another machine's Project) is a rejected promise too.
  return { ...Object.fromEntries(Object.entries(routes).map(([name, route]) => [name, async (...args: unknown[]) => (route as (...a: unknown[]) => unknown)(...args)])), workflow: options.workflow === true } as unknown as CharterApi
}

/** The charter client for one machine, or undefined when its source cannot answer charters. */
export function charterApiFor(source: SourceClient | undefined): CharterApi | undefined {
  if (!source || source.canCharters?.() !== true || typeof source.charterRequest !== "function") return undefined
  // One client per source object, so a screen holding it does not reread on every render.
  let api = clients.get(source)
  if (!api) {
    api = charterRoutes(source.machineID, (path, method, body, signal) => source.charterRequest!(path, method, body, signal), { remove: source.canRemoveCharter?.() === true, conflicts: source.canCharterConflicts?.() === true, playbooks: source.canCharterPlaybooks?.() === true, workflow: source.canCharterWorkflow?.() === true })
    clients.set(source, api)
  }
  return api
}
const clients = new WeakMap<SourceClient, CharterApi>()

export const charterProjectKey = (project: CharterProject) => JSON.stringify([project.machine_id, project.project_id])

/** Visible readers only: a hidden Bot is never named on the page. */
export const shownReaders = (answer: W.ProjectAssistantCharterReadersResponse) => answer.readers.filter(reader => !reader.hidden)

/**
 * The version a Bot must have heard to be current: the newest at which its charter
 * text or its playbooks changed. A machine older than API 127 has no playbooks, so
 * every Bot's is the charter's version.
 */
export const deliveryVersion = (answer: W.ProjectAssistantCharterReadersResponse, reader: W.ProjectAssistantCharterReader) => reader.delivery_version ?? answer.version

/** Whether the Bot's latest prompt carried what is current for it. */
export const readsCurrent = (answer: W.ProjectAssistantCharterReadersResponse, reader: W.ProjectAssistantCharterReader) => readsCurrentAt(answer.version, reader)

/** readsCurrent against a charter version the page already holds. */
export const readsCurrentAt = (version: number, reader: W.ProjectAssistantCharterReader) => version > 0 && reader.version >= (reader.delivery_version ?? version)

/** The current playbooks; none from a machine older than API 127. */
export const charterPlaybooks = (charter: W.ProjectAssistantCharterResponse): W.ProjectAssistantCharterPlaybook[] => charter.playbooks ?? []

/** The current workflow, or the selected version's; null when it has none or the machine is older than API 128. */
export const charterWorkflow = (charter: W.ProjectAssistantCharterResponse, selected = false): W.ProjectAssistantCharterWorkflow | null =>
  (selected ? charter.selected?.workflow : charter.workflow) ?? null

/** What a proposal replaces; a machine older than API 128 has only text proposals. */
export const proposalKind = (proposal: W.ProjectAssistantCharterProposal): CharterChangeTarget | "playbook" => proposal.kind === "workflow" || proposal.kind === "playbook" ? proposal.kind : "text"

/** The pending workflow proposals, oldest first. */
export const workflowProposals = (answer: W.ProjectAssistantCharterProposalsResponse) => answer.proposals.filter(proposal => proposalKind(proposal) === "workflow")

/**
 * A workflow refusal's rule and the state id or from->to transition it names, from
 * a field such as workflow_unreachable:review; subject is empty for a whole-graph rule.
 */
export function workflowRefusalSubject(field: string): { rule: string; subject: string } {
  const at = field.indexOf(":")
  return at < 0 ? { rule: field, subject: "" } : { rule: field.slice(0, at), subject: field.slice(at + 1) }
}

/** One playbook's assigned visible Bots: who has heard its latest content and who has not. */
export interface PlaybookReaders { playbook: W.ProjectAssistantCharterPlaybook; read: W.ProjectAssistantCharterReader[]; unread: W.ProjectAssistantCharterReader[] }

/**
 * For each current playbook, its visible assigned Bots split by whether their latest
 * prompt carried it as it is now. A Bot assigned to it hears its latest content at
 * that Bot's delivery version, so read means version ≥ delivery_version.
 */
export function playbookReaders(charter: W.ProjectAssistantCharterResponse, readers: W.ProjectAssistantCharterReadersResponse): PlaybookReaders[] {
  const shown = shownReaders(readers)
  return charterPlaybooks(charter).map(playbook => {
    const assigned = shown.filter(reader => playbook.bot_ids.includes(reader.bot_id))
    return { playbook, read: assigned.filter(reader => readsCurrent(readers, reader)), unread: assigned.filter(reader => !readsCurrent(readers, reader)) }
  })
}

/** Every Project of the listing, with or without a charter (version 0), with its Bots. */
export async function charterSummaries(api: CharterApi, machineID: string, signal: AbortSignal): Promise<CharterSummary[]> {
  const listing = await api.list(signal)
  const rows = [...listing.charters, ...listing.projects_without_charter.map(project_id => ({ project_id, version: 0, pending_proposals: 0, open_conflicts: 0 }))]
  return Promise.all(rows.map(async row => {
    const project = { machine_id: machineID, project_id: row.project_id }
    let names: string[] = [], current = 0
    try {
      const answer = await api.readers(project, signal), readers = shownReaders(answer)
      names = readers.map(reader => reader.name)
      current = readers.filter(reader => readsCurrentAt(row.version, reader)).length
    } catch (error) { if ((error as { name?: unknown } | null)?.name === "AbortError") throw error }
    return { project, version: row.version, pending_proposals: row.pending_proposals, open_conflicts: row.open_conflicts ?? 0, bots: names.length, readers_current: current, names }
  }))
}

/** Each personal workspace's Bot name, keyed as charterProjectKey keys its Project. */
export function personalWorkspaceNames(items: readonly { bot: Pick<W.ProjectAssistantBot, "key" | "name" | "project"> }[]): ReadonlyMap<string, string> {
  return new Map(items.filter(item => item.bot.project.kind === "personal")
    .map(item => [charterProjectKey({ machine_id: item.bot.key.machine_id, project_id: item.bot.project.project_id }), item.bot.name]))
}

/** Each Bot's appearance, keyed by machine and Bot, for the charter's Bot avatars. */
export function charterAppearances(items: readonly { bot: Pick<W.ProjectAssistantBot, "key" | "appearance"> }[]): ReadonlyMap<string, W.ProjectAssistantAppearance> {
  return new Map(items.map(item => [charterBotKey(item.bot.key.machine_id, item.bot.key.bot_id), item.bot.appearance]))
}
export const charterBotKey = (machineID: string, botID: string) => JSON.stringify([machineID, botID])
