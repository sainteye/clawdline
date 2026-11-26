// An in-memory charter source for the synthetic Bots fixture and its browser
// test. No HTTP, daemon or model: proposals are scripted.
import { CharterRefusal, type CharterApi, type CharterDocument, type CharterProject, type CharterProposal, type CharterReader, type CharterVersion } from "./charter-api"

interface FixtureProject {
  project: CharterProject
  bots: { bot_id: string; name: string }[]
  versions: { text: string; meta: CharterVersion }[]
  readers: Map<string, { version_used: number; running: boolean; queued: boolean }>
  proposals: CharterProposal[]
}

export interface CharterFixture extends CharterApi {
  log: { method: string; project?: string; request?: unknown }[]
  /** Saves a version as if another device did, so the next save conflicts. */
  saveElsewhere(projectID: string, text: string): void
  /** The lead's proposal, replacing `find` with `replace` in the current text. */
  propose(projectID: string, find: string, replace: string, explanation: string): void
  setRunning(projectID: string, botID: string, running: boolean): void
  text(projectID: string): string
  version(projectID: string): number
}

const at = (n: number) => new Date(Date.UTC(2026, 10, 20 + n, 2, 30)).toISOString()

export function createCharterFixture(machineID: string, seed: { project_id: string; bots: { bot_id: string; name: string }[]; versions?: { text: string; author: string; author_kind: "person" | "bot"; kind: CharterVersion["kind"]; reason: string }[] }[]): CharterFixture {
  const projects = new Map<string, FixtureProject>()
  for (const item of seed) {
    const versions = (item.versions ?? []).map((v, i) => ({ text: v.text, meta: { version: i + 1, author: v.author, author_kind: v.author_kind, at: at(i), kind: v.kind, reason: v.reason } }))
    const current = versions.length
    projects.set(item.project_id, {
      project: { machine_id: machineID, project_id: item.project_id }, bots: item.bots, versions, proposals: [],
      readers: new Map(item.bots.map(bot => [bot.bot_id, { version_used: current, running: false, queued: false }])),
    })
  }
  const log: CharterFixture["log"] = []
  const find = (project: CharterProject | string) => {
    const id = typeof project === "string" ? project : project.project_id
    if (typeof project !== "string" && project.machine_id !== machineID) throw new CharterRefusal("not_found")
    const entry = projects.get(id)
    if (!entry) throw new CharterRefusal("not_found")
    return entry
  }
  const check = (method: string, project: CharterProject | undefined, request: unknown, signal: AbortSignal) => {
    log.push({ method, project: project?.project_id, request })
    if (signal.aborted) throw new DOMException("Aborted", "AbortError")
  }
  const document = (entry: FixtureProject): CharterDocument => {
    const current = entry.versions.at(-1)
    if (!current) throw new CharterRefusal("not_found")
    return structuredClone({ project: entry.project, version: current.meta.version, text: current.text, updated_at: current.meta.at, history: entry.versions.map(v => v.meta).reverse() })
  }
  const append = (entry: FixtureProject, text: string, meta: Omit<CharterVersion, "version" | "at">) => {
    if (new TextEncoder().encode(text).length > 64 * 1024) throw new CharterRefusal("too_large")
    entry.versions.push({ text, meta: { ...meta, version: entry.versions.length + 1, at: at(entry.versions.length) } })
    return document(entry)
  }
  const expect = (entry: FixtureProject, version: number) => {
    if (entry.versions.length !== version) throw new CharterRefusal("version_conflict", entry.versions.length)
  }
  const fixture: CharterFixture = {
    log,
    async list(signal) {
      check("list", undefined, null, signal)
      return [...projects.values()].map(entry => {
        const version = entry.versions.length
        return { project: entry.project, version, bots: entry.bots.length, pending_proposals: entry.proposals.length,
          readers_current: version ? [...entry.readers.values()].filter(r => r.version_used === version).length : 0 }
      })
    },
    async read(project, signal) { check("read", project, null, signal); return document(find(project)) },
    async readVersion(project, version, signal) {
      check("readVersion", project, { version }, signal)
      const entry = find(project), item = entry.versions[version - 1]
      if (!item) throw new CharterRefusal("not_found")
      return { version, text: item.text, previous_text: entry.versions[version - 2]?.text ?? "" }
    },
    async save(project, request, signal) {
      check("save", project, request, signal)
      const entry = find(project)
      expect(entry, request.expected_version)
      return append(entry, request.text, { author: "你", author_kind: "person", kind: request.expected_version ? "edit" : "create", reason: request.reason })
    },
    async restore(project, request, signal) {
      check("restore", project, request, signal)
      const entry = find(project)
      expect(entry, request.expected_version)
      const old = entry.versions[request.version - 1]
      if (!old) throw new CharterRefusal("not_found")
      return append(entry, old.text, { author: "你", author_kind: "person", kind: "restore", reason: `restore ${request.version}`, restored_from: request.version })
    },
    async readers(project, signal) {
      check("readers", project, null, signal)
      const entry = find(project)
      return entry.bots.map((bot): CharterReader => ({ bot_id: bot.bot_id, name: bot.name, ...entry.readers.get(bot.bot_id)! }))
    },
    async proposals(project, signal) { check("proposals", project, null, signal); return structuredClone(find(project).proposals) },
    async applyProposal(project, id, request, signal) {
      check("applyProposal", project, { id, ...request }, signal)
      const entry = find(project), proposal = entry.proposals.find(p => p.id === id)
      if (!proposal) throw new CharterRefusal("not_found")
      expect(entry, request.expected_version)
      if (proposal.base_version !== request.expected_version) throw new CharterRefusal("version_conflict", entry.versions.length)
      entry.proposals = entry.proposals.filter(p => p.id !== id)
      return append(entry, proposal.text, { author: proposal.author, author_kind: "bot", kind: "proposal", reason: proposal.explanation })
    },
    async dismissProposal(project, id, signal) {
      check("dismissProposal", project, { id }, signal)
      const entry = find(project)
      entry.proposals = entry.proposals.filter(p => p.id !== id)
    },
    async requestChange(project, request, signal) {
      check("requestChange", project, request, signal)
      const entry = find(project), current = entry.versions.at(-1)
      if (!current) throw new CharterRefusal("not_found")
      // The scripted lead adds the request as a line to the last section.
      entry.proposals = [{ id: `proposal-${log.length}`, project: entry.project, base_version: current.meta.version, author: entry.bots[0]?.name ?? "Lead",
        explanation: request.text, text: current.text.replace(/\n*$/, "") + `\n- ${request.text}\n`, at: at(entry.versions.length) }]
    },
    saveElsewhere(projectID, text) { append(find(projectID), text, { author: "Phone", author_kind: "person", kind: "edit", reason: "edit" }) },
    propose(projectID, from, to, explanation) {
      const entry = find(projectID), current = entry.versions.at(-1)!
      entry.proposals = [{ id: `proposal-${log.length}-${entry.proposals.length}`, project: entry.project, base_version: current.meta.version, author: entry.bots[0]?.name ?? "Lead", explanation, text: current.text.replace(from, to), at: at(entry.versions.length) }]
    },
    setRunning(projectID, botID, running) { const r = find(projectID).readers.get(botID); if (r) r.running = running },
    text(projectID) { return find(projectID).versions.at(-1)?.text ?? "" },
    version(projectID) { return find(projectID).versions.length },
  }
  return fixture
}
