// An in-memory charter source for the synthetic Bots fixture and its browser
// test. It answers the real route shapes of /v1/project-assistant-charters with
// the generated contract types and the source's refusal envelope; no HTTP,
// daemon or model. The lead's proposals are scripted.
import type * as W from "@clawdline/contract"
import type { CharterRequestBody } from "./client"
import { chartersPath } from "./charter-api"

interface FixtureVersion { version: number; text: string; author: W.ProjectAssistantCharterAuthor; reason: string; created_at: string }
interface FixtureProject {
  project_id: string
  bots: { bot_id: string; name: string; lead?: boolean }[]
  versions: FixtureVersion[]
  readers: Map<string, { version: number; running: boolean; queued: boolean }>
  proposals: W.ProjectAssistantCharterProposal[]
  conflicts: W.ProjectAssistantCharterConflict[]
  /** Bots that compared the current version and reported nothing. */
  checkedWithoutReport: number
}

export interface CharterFixture {
  canCharters(): boolean
  canRemoveCharter(): boolean
  canCharterConflicts(): boolean
  charterRequest<T>(path: string, method: "GET" | "PUT" | "POST", body: CharterRequestBody | undefined, signal: AbortSignal): Promise<T>
  log: { method: string; path: string; body?: unknown }[]
  /** Saves a version as if another device did, so the next save is stale. */
  saveElsewhere(projectID: string, text: string): void
  /** The lead's proposal, replacing `find` with `replace` in the current text. */
  propose(projectID: string, find: string, replace: string, summary: string): void
  setRunning(projectID: string, botID: string, running: boolean): void
  /** While held, a change request is accepted and its proposal waits for releaseProposal, as a lead takes seconds. */
  holdProposals(held: boolean): void
  releaseProposal(projectID: string): void
  /** A Bot's report against the current version, as report_charter_conflict stores it. */
  reportConflict(projectID: string, report: Pick<W.ProjectAssistantCharterConflict, "bot_id" | "source" | "bot_sentence" | "charter_sentence" | "section"> & Partial<W.ProjectAssistantCharterConflict>, checkedWithoutReport?: number): string
  text(projectID: string): string
  version(projectID: string): number
}

const contractVersion = "project-assistants.v1"
const at = (n: number) => new Date(Date.UTC(2026, 10, 20 + n, 2, 30)).toISOString()
const refuse = (code: W.ProjectAssistantErrorCode, field = ""): W.ProjectAssistantErrorResponse => ({ contract_version: contractVersion, error: { code, field, retry_after_seconds: 0 } })
const limits: W.ProjectAssistantCharterLimits = { text_bytes: 32768, reason_bytes: 500, request_bytes: 4096, versions: 50, proposals: 5 }

// A removal is a version with no text; while it is newest the Project has none.
const current = (entry: FixtureProject) => entry.versions.at(-1)?.author === "removed" ? 0 : entry.versions.length

function charterAnswer(machineID: string, entry: FixtureProject, selected: number): W.ProjectAssistantCharterResponse {
  const latest = entry.versions.at(-1)
  const wire = (v: FixtureVersion, text: boolean): W.ProjectAssistantCharterVersion => ({ version: v.version, author: v.author, reason: v.reason, created_at: v.created_at, ...(text ? { text: v.text } : {}) })
  const out: W.ProjectAssistantCharterResponse = { contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry), text: current(entry) ? latest?.text ?? "" : "",
    history: entry.versions.map(v => wire(v, false)).reverse(), pending_proposals: entry.proposals.length, limits }
  if (selected > 0) {
    const v = entry.versions[selected - 1]
    if (!v) throw refuse("pa_not_found", "version")
    out.selected = wire(v, true)
  }
  return structuredClone(out)
}

export function createCharterFixture(machineID: string, seed: { project_id: string; bots: { bot_id: string; name: string; lead?: boolean }[]; versions?: { text: string; author: W.ProjectAssistantCharterAuthor; reason: string }[] }[]): CharterFixture {
  const projects = new Map<string, FixtureProject>()
  for (const item of seed) {
    const versions = (item.versions ?? []).map((v, i) => ({ ...v, version: i + 1, created_at: at(i) }))
    projects.set(item.project_id, { project_id: item.project_id, bots: item.bots, versions, proposals: [], conflicts: [], checkedWithoutReport: 0,
      readers: new Map(item.bots.map(bot => [bot.bot_id, { version: versions.length, running: false, queued: false }])) })
  }
  const log: CharterFixture["log"] = []
  let sequence = 0, held = false
  const waiting = new Map<string, W.ProjectAssistantCharterProposal>()
  const find = (id: unknown) => {
    const entry = typeof id === "string" ? projects.get(id) : undefined
    if (!entry) throw refuse("pa_not_found", "project_id")
    return entry
  }
  const answer = (entry: FixtureProject, selected = 0) => charterAnswer(machineID, entry, selected)
  const proposalsAnswer = (entry: FixtureProject): W.ProjectAssistantCharterProposalsResponse => ({ contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry),
    proposals: structuredClone(entry.proposals.map(p => ({ ...p, stale: p.base_version !== current(entry) }))) })
  const conflictsAnswer = (entry: FixtureProject): W.ProjectAssistantCharterConflictsResponse => ({ contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry),
    conflicts: structuredClone([...entry.conflicts].sort((a, b) => Number(b.state === "open") - Number(a.state === "open")).map(c => ({ ...c, outdated: c.version !== current(entry) }))),
    checked_without_report: entry.checkedWithoutReport, limits: { open: 50, resolved: 50, sentence_bytes: 2048 } })
  const append = (entry: FixtureProject, text: string, author: W.ProjectAssistantCharterAuthor, reason: string) => {
    if (new TextEncoder().encode(text).length > limits.text_bytes) throw refuse("pa_capacity_full", "charter_text")
    entry.versions.push({ version: entry.versions.length + 1, text, author, reason, created_at: at(entry.versions.length) })
  }
  const expect = (entry: FixtureProject, version: unknown, field = "charter_version") => { if (version !== current(entry)) throw refuse("pa_stale_version", field) }
  const leadName = (entry: FixtureProject) => (entry.bots.find(b => b.lead) ?? entry.bots[0])
  const route = (path: string, method: string, request: CharterRequestBody | undefined) => {
    // The route below decides which request shape this is.
    const body = request as (Partial<W.ProjectAssistantCharterSaveRequest & W.ProjectAssistantCharterRestoreRequest & W.ProjectAssistantCharterApplyRequest> & { machine_id: string; project_id: string }) | undefined
    const url = new URL(path, "http://fixture.invalid"), q = Object.fromEntries(url.searchParams), rest = url.pathname.slice(chartersPath.length)
    if (q.machine_id !== undefined && q.machine_id !== machineID || body && body.machine_id !== machineID) throw refuse("pa_scope_denied", "machine_id")
    switch (`${method} ${rest}`) {
      case "GET ": return { contract_version: contractVersion, machine_id: machineID, limits,
        charters: [...projects.values()].filter(p => current(p) > 0).map(p => ({ project_id: p.project_id, version: current(p), pending_proposals: p.proposals.length, open_conflicts: p.conflicts.filter(c => c.state === "open").length, updated_at: p.versions.at(-1)!.created_at, updated_by: p.versions.at(-1)!.author })),
        projects_without_charter: [...projects.values()].filter(p => !current(p)).map(p => p.project_id) } satisfies W.ProjectAssistantChartersResponse
      case "GET /charter": return answer(find(q.project_id), Number(q.version))
      case "PUT /charter": {
        const entry = find(body!.project_id)
        expect(entry, body!.expected_version)
        append(entry, String(body!.text), body!.expected_version === 0 ? "template" : "person", String(body!.reason ?? ""))
        return answer(entry)
      }
      case "POST /restore": {
        const entry = find(body!.project_id)
        expect(entry, body!.expected_version)
        const old = entry.versions[Number(body!.version) - 1]
        if (!old) throw refuse("pa_not_found", "version")
        append(entry, old.text, "restore", `Restored version ${old.version}`)
        return answer(entry)
      }
      case "GET /readers": {
        const entry = find(q.project_id)
        return { contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry),
          readers: entry.bots.map(bot => ({ bot_id: bot.bot_id, name: bot.name, hidden: false, lead: !!bot.lead, turn_id: "", ...entry.readers.get(bot.bot_id)! })) } satisfies W.ProjectAssistantCharterReadersResponse
      }
      case "GET /proposals": return proposalsAnswer(find(q.project_id))
      case "POST /remove": {
        const entry = find(body!.project_id)
        if (!current(entry)) throw refuse("pa_stale_version", "charter_version")
        expect(entry, body!.expected_version)
        entry.proposals = []
        append(entry, "", "removed", "")
        return answer(entry)
      }
      case "POST /proposal-apply": {
        const entry = find(body!.project_id), proposal = entry.proposals.find(p => p.id === body!.proposal_id)
        if (!proposal) throw refuse("pa_not_found", "proposal_id")
        expect(entry, body!.expected_version)
        if (proposal.base_version !== current(entry)) throw refuse("pa_stale_version", "charter_proposal_base")
        entry.proposals = entry.proposals.filter(p => p !== proposal)
        append(entry, proposal.text, "lead_proposal", proposal.summary)
        return answer(entry)
      }
      case "POST /proposal-dismiss": {
        const entry = find(body!.project_id)
        entry.proposals = entry.proposals.filter(p => p.id !== body!.proposal_id)
        return proposalsAnswer(entry)
      }
      case "GET /conflicts": return conflictsAnswer(find(q.project_id))
      case "POST /conflict-resolve": {
        const entry = find(body!.project_id), resolve = request as W.ProjectAssistantCharterConflictResolveRequest
        const item = entry.conflicts.find(c => c.id === resolve.conflict_id)
        if (!item) throw refuse("pa_not_found", "charter_conflict")
        if (item.state !== "open") throw refuse("pa_stale_version", "charter_conflict")
        // The daemon rewrites a memory fact or instructions itself; a file goes to the Bot.
        if (resolve.action === "dismiss") entry.conflicts = entry.conflicts.filter(c => c !== item)
        else Object.assign(item, { state: resolve.action === "change_charter" ? "handed_to_charter" : item.source === "workspace_file" ? "handed_to_bot" : "fixed", resolved_at: at(9) })
        return conflictsAnswer(entry)
      }
      case "POST /change-request": {
        const entry = find(body!.project_id), latest = entry.versions.at(-1)
        if (!latest) throw refuse("pa_not_found", "charter")
        const lead = leadName(entry)
        // The scripted lead adds the request as a line at the end.
        const proposal = { id: `cp-${++sequence}`, base_version: latest.version, bot_id: lead.bot_id, turn_id: `turn-${sequence}`, stale: false,
          summary: String(body!.text), text: latest.text.replace(/\n*$/, "") + `\n- ${body!.text}\n`, created_at: at(entry.versions.length) }
        if (held) waiting.set(entry.project_id, proposal)
        else entry.proposals = [proposal]
        return { contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, lead_bot_id: lead.bot_id,
          receipt: { id: `receipt-${sequence}`, state: "accepted", event_seq: sequence, resource_id: lead.bot_id } as unknown as W.ProjectAssistantReceipt } satisfies W.ProjectAssistantCharterChangeResponse
      }
      default: throw refuse("pa_invalid", "route")
    }
  }
  return {
    log,
    canCharters: () => true,
    canRemoveCharter: () => true,
    canCharterConflicts: () => true,
    async charterRequest<T>(path: string, method: "GET" | "PUT" | "POST", body: CharterRequestBody | undefined, signal: AbortSignal) {
      log.push({ method, path, body })
      if (signal.aborted) throw new DOMException("Aborted", "AbortError")
      return route(path, method, body ? structuredClone(body) : undefined) as T
    },
    saveElsewhere(projectID, text) { append(find(projectID), text, "person", "") },
    propose(projectID, from, to, summary) {
      const entry = find(projectID), latest = entry.versions.at(-1)!, lead = leadName(entry)
      entry.proposals = [{ id: `cp-${++sequence}`, base_version: latest.version, bot_id: lead.bot_id, turn_id: `turn-${sequence}`, stale: false, summary, text: latest.text.replace(from, to), created_at: at(entry.versions.length) }]
    },
    holdProposals(on) { held = on },
    releaseProposal(projectID) { const p = waiting.get(projectID); if (p) { find(projectID).proposals = [p]; waiting.delete(projectID) } },
    setRunning(projectID, botID, running) { const r = find(projectID).readers.get(botID); if (r) r.running = running },
    reportConflict(projectID, report, checked) {
      const entry = find(projectID), name = entry.bots.find(b => b.bot_id === report.bot_id)?.name ?? ""
      const id = `cc-${++sequence}`
      entry.conflicts.unshift({ id, bot_name: name, turn_id: `turn-${sequence}`, version: current(entry), outdated: false, path: "", memory_entry_id: "", suggested: "", state: "open",
        created_at: new Date(Date.UTC(2026, 10, 26, 5, 12)).toISOString(), resolved_at: "", ...report })
      if (checked !== undefined) entry.checkedWithoutReport = checked
      return id
    },
    text(projectID) { return find(projectID).versions.at(-1)?.text ?? "" },
    version(projectID) { return current(find(projectID)) },
  }
}
