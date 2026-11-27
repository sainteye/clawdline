// An in-memory charter source for the synthetic Bots fixture and its browser
// test. It answers the real route shapes of /v1/project-assistant-charters with
// the generated contract types and the source's refusal envelope; no HTTP,
// daemon or model. The lead's proposals are scripted. Playbooks follow the
// daemon's delivery rule: a Bot's delivery version moves when the charter text
// or workflow changes or when the playbooks it hears change.
import type * as W from "@clawdline/contract"
import type { CharterRequestBody } from "./client"
import type { CharterPlaybookDraft } from "./charter-api"

// The daemon's path, written out so the fixture loads in a plain Node test too.
const chartersPath = "/v1/project-assistant-charters"

interface FixtureVersion {
  version: number; text: string; author: W.ProjectAssistantCharterAuthor; reason: string; created_at: string
  playbooks: W.ProjectAssistantCharterPlaybook[]
  workflow?: W.ProjectAssistantCharterWorkflow
  /** The version at which the text last changed, and the Bots whose playbooks changed after it. */
  text_version: number; deliveries: Record<string, number>
}
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
  canCharterPlaybooks(): boolean
  canCharterWorkflow(): boolean
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
  /** The Bot's next prompt carries what is current for it, as a turn would. */
  markRead(projectID: string, botID: string): void
}

/**
 * Two synthetic playbooks for a fixture's seed: the first assigned to the first two
 * Bots, the second to the third. Seeded playbooks are saved as versions after the
 * seeded charter, so their Bots have not heard them until markRead.
 */
export function samplePlaybooks(botIDs: readonly string[]): CharterPlaybookDraft[] {
  return [
    { title: "Release steps", text: "1. Tag the commit.\n2. Write the release note.\n3. Ask before deploying.", bot_ids: botIDs.slice(0, 2) },
    { title: "Review checklist", text: "- Read the whole diff.\n- Run the focused tests.\n- 回覆時列出驗證過的項目。", bot_ids: botIDs.slice(2, 3) },
  ]
}

/** The Bots a team workflow names, by role; empty strings mean any Bot. */
export interface WorkflowTeam { atlas: string; xavia: string; fuse: string; lens: string; beacon: string }

/**
 * The current team's flow of one piece of work, for the UI to render: open a
 * card, the lead dispatches and reviews the evidence, the integrator lands it,
 * a reliability review steps in when failure handling is touched, the visual
 * check passes it on or sends it back, then Cloud is deployed and, when people
 * can see the change, the website and README are updated.
 */
export function sampleWorkflow(team: WorkflowTeam): W.ProjectAssistantCharterWorkflow {
  const state = (id: string, title: string, bot_id: string, kind: "start" | "step" | "done" = "step", note = "") => ({ id, title, bot_id, kind, note })
  const go = (from: string, to: string, label = "", kind: "normal" | "pass" | "return" = "normal") => ({ from, to, label, kind })
  return {
    states: [state("card", "開交辦卡", "", "start"), state("review", "派工・審證據", team.atlas), state("integrate", "整合・推 GitHub・裝 dev", team.xavia),
      state("reliability", "可靠性審查", team.fuse), state("visual", "畫面驗收", team.lens), state("deploy", "部署 Cloud", team.xavia),
      state("website", "更新官網與 README", team.beacon), state("done", "完成", "", "done")],
    transitions: [go("card", "review"), go("review", "integrate", "審過", "pass"), go("review", "reliability", "動到失敗處理"),
      go("reliability", "integrate", "通過", "pass"), go("integrate", "visual"), go("visual", "deploy", "通過", "pass"), go("visual", "review", "不通過", "return"),
      go("deploy", "website", "對外看得到"), go("deploy", "done", "其他"), go("website", "done")],
  }
}

/** The lead's pending proposal for the sample: a failed reliability review goes back to the lead. */
export function sampleWorkflowProposal(workflow: W.ProjectAssistantCharterWorkflow): W.ProjectAssistantCharterWorkflow {
  const out = structuredClone(workflow)
  out.transitions.push({ from: "reliability", to: "review", label: "不通過", kind: "return" })
  return out
}

const contractVersion = "project-assistants.v1"
const at = (n: number) => new Date(Date.UTC(2026, 10, 20 + n, 2, 30)).toISOString()
const refuse = (code: W.ProjectAssistantErrorCode, field = ""): W.ProjectAssistantErrorResponse => ({ contract_version: contractVersion, error: { code, field, retry_after_seconds: 0 } })
const limits: W.ProjectAssistantCharterLimits = { text_bytes: 32768, reason_bytes: 500, request_bytes: 4096, versions: 50, proposals: 5,
  playbook_text_bytes: 16384, playbooks: 20, playbook_title_bytes: 120, playbook_bots: 64, document_bytes: 131072,
  workflow_states: 40, workflow_transitions: 80, workflow_title_bytes: 80, workflow_note_bytes: 300, workflow_id_bytes: 40 }

// A removal is a version with no text; while it is newest the Project has none.
const current = (entry: FixtureProject) => entry.versions.at(-1)?.author === "removed" ? 0 : entry.versions.length
const live = (entry: FixtureProject) => current(entry) ? entry.versions.at(-1)! : undefined

/** The version at which what bot hears last changed; 0 without a charter. */
const deliveryVersion = (entry: FixtureProject, bot: string) => { const v = live(entry); return v ? v.deliveries[bot] ?? v.text_version : 0 }
const heard = (playbooks: W.ProjectAssistantCharterPlaybook[], bot: string) => JSON.stringify(playbooks.filter(p => p.bot_ids.includes(bot)).map(p => [p.id, p.title, p.text]))

/** NextCharterDeliveries: a text change reaches every Bot, a playbook change only the Bots it concerns. */
function deliveries(prior: FixtureVersion | undefined, next: FixtureVersion) {
  if (!prior || prior.author === "removed" || next.author === "removed" || prior.text !== next.text || JSON.stringify(prior.workflow ?? null) !== JSON.stringify(next.workflow ?? null)) return { text_version: next.version, deliveries: {} }
  const out = { ...prior.deliveries }
  for (const bot of new Set([...prior.playbooks, ...next.playbooks].flatMap(p => p.bot_ids))) if (heard(prior.playbooks, bot) !== heard(next.playbooks, bot)) out[bot] = next.version
  return { text_version: prior.text_version, deliveries: out }
}

function charterAnswer(machineID: string, entry: FixtureProject, selected: number): W.ProjectAssistantCharterResponse {
  const latest = entry.versions.at(-1)
  const wire = (v: FixtureVersion, text: boolean): W.ProjectAssistantCharterVersion => ({ version: v.version, author: v.author, reason: v.reason, created_at: v.created_at, ...(text ? { text: v.text, playbooks: v.playbooks, ...(v.workflow ? { workflow: v.workflow } : {}) } : {}) })
  const out: W.ProjectAssistantCharterResponse = { contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry), text: current(entry) ? latest?.text ?? "" : "",
    playbooks: live(entry)?.playbooks ?? [], ...(live(entry)?.workflow ? { workflow: live(entry)!.workflow } : {}),
    history: entry.versions.map(v => wire(v, false)).reverse(), pending_proposals: entry.proposals.length, limits }
  if (selected > 0) {
    const v = entry.versions[selected - 1]
    if (!v) throw refuse("pa_not_found", "version")
    out.selected = wire(v, true)
  }
  return structuredClone(out)
}

function conflictsAnswer(machineID: string, entry: FixtureProject): W.ProjectAssistantCharterConflictsResponse {
  return { contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry),
    conflicts: structuredClone([...entry.conflicts].sort((a, b) => Number(b.state === "open") - Number(a.state === "open")).map(c => ({ ...c, outdated: c.version !== deliveryVersion(entry, c.bot_id) }))),
    checked_without_report: entry.checkedWithoutReport, limits: { open: 50, resolved: 50, sentence_bytes: 2048 } }
}

/** The conflict list and the person's choice; the daemon rewrites a memory fact or instructions itself, a file goes to the Bot. */
function conflictRoute(machineID: string, entry: FixtureProject, method: string, resolve: W.ProjectAssistantCharterConflictResolveRequest | undefined) {
  if (method === "GET" || !resolve) return conflictsAnswer(machineID, entry)
  const item = entry.conflicts.find(c => c.id === resolve.conflict_id)
  if (!item) throw refuse("pa_not_found", "charter_conflict")
  if (item.state !== "open") throw refuse("pa_stale_version", "charter_conflict")
  if (resolve.action === "dismiss") entry.conflicts = entry.conflicts.filter(c => c !== item)
  // The fixture leaves a playbook's text alone; the daemon saves a new version.
  else Object.assign(item, { state: resolve.action === "change_charter" ? "handed_to_charter" : item.source === "workspace_file" ? "handed_to_bot" : "fixed", resolved_at: at(9) })
  return conflictsAnswer(machineID, entry)
}

function addConflict(entry: FixtureProject, id: string, report: Parameters<CharterFixture["reportConflict"]>[1], checked?: number) {
  const name = entry.bots.find(b => b.bot_id === report.bot_id)?.name ?? ""
  entry.conflicts.unshift({ id, bot_name: name, turn_id: `turn-${id}`, version: deliveryVersion(entry, report.bot_id), outdated: false, path: "", memory_entry_id: "", suggested: "", state: "open",
    created_at: new Date(Date.UTC(2026, 10, 26, 5, 12)).toISOString(), resolved_at: "", ...report })
  if (checked !== undefined) entry.checkedWithoutReport = checked
  return id
}

type FixtureSeed = Parameters<typeof createCharterFixture>[1][number]

/** Readers heard the seeded charter; playbooks seeded after it are news to their Bots. */
function seedProject(item: FixtureSeed, newPlaybook: (draft: CharterPlaybookDraft) => W.ProjectAssistantCharterPlaybook): FixtureProject {
  const entry: FixtureProject = { project_id: item.project_id, bots: item.bots, versions: [], proposals: [], conflicts: [], checkedWithoutReport: 0, readers: new Map() }
  for (const v of item.versions ?? []) append(entry, v.text, v.author, v.reason)
  entry.readers = new Map(item.bots.map(bot => [bot.bot_id, { version: entry.versions.length, running: false, queued: false }]))
  if (current(entry)) for (const draft of item.playbooks ?? []) append(entry, entry.versions.at(-1)!.text, "person", "", [...live(entry)!.playbooks, newPlaybook(draft)])
  if (current(entry) && item.workflow) append(entry, live(entry)!.text, "person", "", live(entry)!.playbooks, item.workflow)
  // The lead's pending workflow proposal, made on the seeded version.
  if (current(entry) && item.workflow_proposal) entry.proposals.push({ id: "cp-workflow", base_version: current(entry), kind: "workflow", text: "", workflow: structuredClone(item.workflow_proposal),
    summary: "可靠性審查不通過時退回派工・審證據。", bot_id: (item.bots.find(b => b.lead) ?? item.bots[0]).bot_id, turn_id: "turn-workflow", stale: false, created_at: at(entry.versions.length) })
  return entry
}

/** Saves the next version; it keeps the current playbooks and workflow unless given others. */
function append(entry: FixtureProject, text: string, author: W.ProjectAssistantCharterAuthor, reason: string, playbooks = live(entry)?.playbooks ?? [], workflow = live(entry)?.workflow) {
  if (new TextEncoder().encode(text).length > limits.text_bytes) throw refuse("pa_capacity_full", "charter_text")
  if (playbooks.length > limits.playbooks!) throw refuse("pa_capacity_full", "charter_playbooks")
  const next: FixtureVersion = { version: entry.versions.length + 1, text, author, reason, created_at: at(entry.versions.length), playbooks: structuredClone(playbooks),
    ...(workflow ? { workflow: structuredClone(workflow) } : {}), text_version: 0, deliveries: {} }
  Object.assign(next, deliveries(entry.versions.at(-1), next))
  entry.versions.push(next)
}

function readersAnswer(machineID: string, entry: FixtureProject): W.ProjectAssistantCharterReadersResponse {
  return { contract_version: contractVersion, machine_id: machineID, project_id: entry.project_id, version: current(entry),
    readers: entry.bots.map(bot => ({ bot_id: bot.bot_id, name: bot.name, hidden: false, lead: !!bot.lead, turn_id: "", ...entry.readers.get(bot.bot_id)!,
      delivery_version: deliveryVersion(entry, bot.bot_id), playbook_ids: (live(entry)?.playbooks ?? []).filter(p => p.bot_ids.includes(bot.bot_id)).map(p => p.id) })) }
}

/** The two playbook writes; each saves the next version through append. */
function playbookWrite(entry: FixtureProject, rest: string, request: CharterRequestBody | undefined, newPlaybook: (draft: CharterPlaybookDraft) => W.ProjectAssistantCharterPlaybook) {
  const latest = live(entry)
  if (!latest) throw refuse("pa_invalid", "charter")
  if (rest === "/playbook-remove") {
    const id = (request as W.ProjectAssistantCharterPlaybookRemoveRequest).id
    if (!latest.playbooks.some(p => p.id === id)) throw refuse("pa_not_found", "charter_playbook")
    append(entry, latest.text, "person", "", latest.playbooks.filter(p => p.id !== id))
    return
  }
  const save = request as W.ProjectAssistantCharterPlaybookSaveRequest
  if (save.playbook.bot_ids.some(id => !entry.bots.some(b => b.bot_id === id))) throw refuse("pa_invalid", "bot_ids")
  if (new TextEncoder().encode(save.playbook.text).length > limits.playbook_text_bytes!) throw refuse("pa_capacity_full", "charter_playbook_text")
  const playbooks = [...latest.playbooks], index = save.playbook.id ? playbooks.findIndex(p => p.id === save.playbook.id) : -1
  if (save.playbook.id && index < 0) throw refuse("pa_not_found", "charter_playbook")
  if (index >= 0) playbooks[index] = newPlaybook({ ...save.playbook, id: save.playbook.id })
  else playbooks.push(newPlaybook({ ...save.playbook, id: "" }))
  append(entry, latest.text, "person", save.reason, playbooks)
}

export function createCharterFixture(machineID: string, seed: { project_id: string; bots: { bot_id: string; name: string; lead?: boolean }[]; versions?: { text: string; author: W.ProjectAssistantCharterAuthor; reason: string }[]; playbooks?: CharterPlaybookDraft[];
  /** Saved as the version after the playbooks; workflow_proposal waits as the lead's proposal on it. */
  workflow?: W.ProjectAssistantCharterWorkflow; workflow_proposal?: W.ProjectAssistantCharterWorkflow }[]): CharterFixture {
  const projects = new Map<string, FixtureProject>()
  let playbookSequence = 0
  const newPlaybook = (draft: CharterPlaybookDraft): W.ProjectAssistantCharterPlaybook => ({ id: draft.id || `pb-${(++playbookSequence).toString(16).padStart(16, "0")}`, title: draft.title, text: draft.text, bot_ids: [...draft.bot_ids] })
  for (const item of seed) projects.set(item.project_id, seedProject(item, newPlaybook))
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
        append(entry, old.text, "restore", `Restored version ${old.version}`, old.playbooks, old.workflow)
        return answer(entry)
      }
      case "GET /readers": return readersAnswer(machineID, find(q.project_id))
      case "GET /proposals": return proposalsAnswer(find(q.project_id))
      case "POST /remove": {
        const entry = find(body!.project_id)
        if (!current(entry)) throw refuse("pa_stale_version", "charter_version")
        expect(entry, body!.expected_version)
        entry.proposals = []
        append(entry, "", "removed", "", [], undefined)
        return answer(entry)
      }
      case "POST /proposal-apply": {
        const entry = find(body!.project_id), proposal = entry.proposals.find(p => p.id === body!.proposal_id)
        if (!proposal) throw refuse("pa_not_found", "proposal_id")
        expect(entry, body!.expected_version)
        if (proposal.base_version !== current(entry)) throw refuse("pa_stale_version", "charter_proposal_base")
        entry.proposals = entry.proposals.filter(p => p !== proposal)
        if (proposal.kind === "workflow") append(entry, live(entry)!.text, "lead_proposal", proposal.summary, live(entry)!.playbooks, proposal.workflow)
        else append(entry, proposal.text, "lead_proposal", proposal.summary)
        return answer(entry)
      }
      case "POST /proposal-dismiss": {
        const entry = find(body!.project_id)
        entry.proposals = entry.proposals.filter(p => p.id !== body!.proposal_id)
        return proposalsAnswer(entry)
      }
      case "PUT /playbook": case "POST /playbook-remove": {
        const entry = find((request as W.ProjectAssistantCharterPlaybookRemoveRequest).project_id)
        expect(entry, (request as W.ProjectAssistantCharterPlaybookRemoveRequest).expected_version)
        playbookWrite(entry, rest, request, newPlaybook)
        return answer(entry)
      }
      case "GET /conflicts": case "POST /conflict-resolve": return conflictRoute(machineID, find(q.project_id ?? body?.project_id), method, request as W.ProjectAssistantCharterConflictResolveRequest | undefined)
      case "POST /change-request": {
        const entry = find(body!.project_id), latest = entry.versions.at(-1)
        if (!latest) throw refuse("pa_not_found", "charter")
        const lead = leadName(entry), target = (request as W.ProjectAssistantCharterChangeRequest).target
        if (target !== undefined && target !== "text" && target !== "workflow") throw refuse("pa_invalid", "target")
        // The scripted lead adds the request as a line at the end, or, for the
        // workflow, as the start state's note of the current workflow.
        const flow = target === "workflow" && latest.workflow ? structuredClone(latest.workflow) : undefined
        if (flow) flow.states[0].note = String(body!.text).slice(0, limits.workflow_note_bytes!)
        const proposal: W.ProjectAssistantCharterProposal = { id: `cp-${++sequence}`, base_version: latest.version, bot_id: lead.bot_id, turn_id: `turn-${sequence}`, stale: false,
          summary: String(body!.text), created_at: at(entry.versions.length),
          ...(target === "workflow" ? { kind: "workflow" as const, text: "", ...(flow ? { workflow: flow } : {}) } : { kind: "text" as const, text: latest.text.replace(/\n*$/, "") + `\n- ${body!.text}\n` }) }
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
    canCharterPlaybooks: () => true,
    canCharterWorkflow: () => true,
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
    reportConflict(projectID, report, checked) { return addConflict(find(projectID), `cc-${++sequence}`, report, checked) },
    text(projectID) { return find(projectID).versions.at(-1)?.text ?? "" },
    version(projectID) { return current(find(projectID)) },
    markRead(projectID, botID) { const entry = find(projectID), r = entry.readers.get(botID); if (r) r.version = current(entry) },
  }
}
