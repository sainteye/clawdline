/**
 * What the Ops page decides from the machine's answers, kept free of React and
 * the DOM so `node --test` can exercise every rule with fake answers.
 *
 * Nothing here invents state the machine did not report: an entry's type and
 * status are the server's, and a resolve to-do's delivery is `todo_state` as
 * read, never guessed from elapsed time.
 */
import type { OpsCollection, OpsEntry, OpsEntryType, OpsResolve, OpsSetup, OpsVisibility } from "./ops-api.js"

// ---------------------------------------------------------------- filters

export const OPS_FILTERS = ["all", "markdown", "notion", "google", "web", "attention"] as const
export type OpsFilter = typeof OPS_FILTERS[number]

/** Statuses a person has to act on; `resolving` is the agent's turn, not theirs. */
export function needsAttention(entry: OpsEntry): boolean {
  return entry.status === "needs_access" || entry.status === "missing" || entry.status === "unreadable"
}

export function entryMatches(entry: OpsEntry, filter: OpsFilter): boolean {
  switch (filter) {
    case "all": return true
    case "markdown": return entry.type === "markdown"
    case "notion": return entry.type === "notion_page"
    case "google": return entry.type.startsWith("google_")
    case "web": return entry.type === "web"
    case "attention": return needsAttention(entry)
  }
}

export function filterCounts(entries: readonly OpsEntry[]): Record<OpsFilter, number> {
  const counts = Object.fromEntries(OPS_FILTERS.map((filter) => [filter, 0])) as Record<OpsFilter, number>
  for (const entry of entries) for (const filter of OPS_FILTERS) if (entryMatches(entry, filter)) counts[filter]++
  return counts
}

/** The phone list: newest change first, so what was just added is on top. */
export function phoneRows(entries: readonly OpsEntry[], filter: OpsFilter): OpsEntry[] {
  return entries.filter((entry) => entryMatches(entry, filter))
    .sort((a, b) => b.updated_at - a.updated_at || a.title.localeCompare(b.title))
}

// ---------------------------------------------------------------- tree

export interface OpsTreeGroup {
  /** A collection id, `links` for uncollected links, `files` for uncollected Project files. */
  key: string
  collection: OpsCollection | null
  entries: OpsEntry[]
}

const byPosition = (a: OpsEntry, b: OpsEntry) => a.position - b.position || a.title.localeCompare(b.title) || a.id.localeCompare(b.id)

/**
 * The desktop tree: every collection in its order with its entries, then the
 * links in no collection, then the Project files in no collection. An entry
 * naming a collection this answer does not list belongs to no collection,
 * rather than vanishing. Empty collections stay (a person just made one); the
 * two catch-all groups appear only when they hold something.
 */
export function buildTree(collections: readonly OpsCollection[], entries: readonly OpsEntry[]): OpsTreeGroup[] {
  const known = new Set(collections.map((collection) => collection.id))
  const ordered = [...collections].sort((a, b) => a.position - b.position || a.name.localeCompare(b.name))
  const groups: OpsTreeGroup[] = ordered.map((collection) => ({
    key: collection.id, collection, entries: entries.filter((entry) => entry.collection === collection.id).sort(byPosition),
  }))
  const loose = entries.filter((entry) => !known.has(entry.collection))
  const links = loose.filter((entry) => entry.kind === "link").sort(byPosition)
  const files = loose.filter((entry) => entry.kind === "file").sort(byPosition)
  if (links.length) groups.push({ key: "links", collection: null, entries: links })
  if (files.length) groups.push({ key: "files", collection: null, entries: files })
  return groups
}

/** The next integer position at the end of a collection. */
export function nextPosition(entries: readonly OpsEntry[], collection: string): number {
  return entries.filter((entry) => entry.collection === collection).reduce((max, entry) => Math.max(max, entry.position + 1), 0)
}

// ---------------------------------------------------------------- type and status

export type OpsTypeWord = "typeMarkdown" | "typeText" | "typeFile" | "typeNotion" | "typeGoogleDoc" | "typeGoogleSheet" |
  "typeGoogleSlides" | "typeGoogleForm" | "typeGoogleDrive" | "typeWeb"

export function typeWord(type: OpsEntryType): OpsTypeWord {
  switch (type) {
    case "markdown": return "typeMarkdown"
    case "text": return "typeText"
    case "file": return "typeFile"
    case "notion_page": return "typeNotion"
    case "google_doc": return "typeGoogleDoc"
    case "google_sheet": return "typeGoogleSheet"
    case "google_slides": return "typeGoogleSlides"
    case "google_form": return "typeGoogleForm"
    case "google_drive": return "typeGoogleDrive"
    default: return "typeWeb"
  }
}

/** The icon family an entry's type draws. */
export type OpsIcon = "markdown" | "text" | "file" | "notion" | "google" | "web"
export function typeIcon(type: OpsEntryType): OpsIcon {
  if (type === "markdown" || type === "text" || type === "file") return type
  if (type === "notion_page") return "notion"
  if (type.startsWith("google_")) return "google"
  return "web"
}

export type ResolveWord = "resolveUnsent" | "resolveSent" | "resolveReading" | "resolveDone" | "resolveMissing"
export interface ResolveView { word: ResolveWord; action: "send" | "retry" | null }

/**
 * The delivery of the to-do that asks a Session to read a link. `unsent` is a
 * row that exists but was never delivered (the send was refused or its answer
 * lost): it offers Send, which sends that row instead of creating another.
 * `missing` means the to-do is gone, so the only way on is a new request.
 */
export function resolveView(resolve: OpsResolve): ResolveView {
  switch (resolve.todo_state) {
    case "unsent": return { word: "resolveUnsent", action: "send" }
    case "sent": return { word: "resolveSent", action: null }
    case "read": return { word: "resolveReading", action: null }
    case "done": return { word: "resolveDone", action: null }
    default: return { word: "resolveMissing", action: "retry" }
  }
}

export type StatusWord = "statusReady" | "statusResolving" | "statusNeedsAccess" | "statusMissing" | "statusUnreadable"
export type StatusAction = "send" | "retryResolve" | "retryAccess" | "openOriginal" | "removeFromOps"
export interface StatusView {
  word: StatusWord
  tone: "ready" | "busy" | "attention"
  resolve: ResolveView | null
  actions: StatusAction[]
}

export function statusView(entry: OpsEntry): StatusView {
  const resolve = entry.resolve ? resolveView(entry.resolve) : null
  switch (entry.status) {
    case "ready": return { word: "statusReady", tone: "ready", resolve: null, actions: entry.kind === "link" ? ["openOriginal"] : [] }
    case "resolving": return {
      word: "statusResolving", tone: "busy", resolve,
      actions: [...(resolve?.action === "send" ? ["send" as const] : resolve?.action === "retry" ? ["retryResolve" as const] : []), "openOriginal"],
    }
    case "needs_access": return { word: "statusNeedsAccess", tone: "attention", resolve, actions: ["retryAccess", "openOriginal"] }
    case "missing": return { word: "statusMissing", tone: "attention", resolve: null, actions: ["removeFromOps"] }
    default: return { word: "statusUnreadable", tone: "attention", resolve: null, actions: [] }
  }
}

/** Only a readable Markdown or text file can be edited here. */
export function editable(entry: OpsEntry): boolean {
  return entry.kind === "file" && entry.status === "ready" && (entry.type === "markdown" || entry.type === "text")
}

// ---------------------------------------------------------------- setup

/** The first New .md must ask about git: no folder yet, inside a repository. */
export function needsGitChoice(setup: OpsSetup): boolean {
  return !setup.folder_exists && setup.git === "repo"
}

/** A private repository may carry the notes; public or unconfirmed keeps them out. */
export function includeInGitDefault(visibility: OpsVisibility): boolean {
  return visibility === "private"
}

export type VisibilityWord = "visibilityPublic" | "visibilityPrivate" | "visibilityUnknown"
export function visibilityWord(visibility: OpsVisibility): VisibilityWord {
  return visibility === "public" ? "visibilityPublic" : visibility === "private" ? "visibilityPrivate" : "visibilityUnknown"
}

// ---------------------------------------------------------------- input checks (contract bounds)

const bytes = (text: string) => new TextEncoder().encode(text).length
export const OPS_TITLE_BYTES = 200
export const OPS_COLLECTION_NAME_BYTES = 100
export const OPS_FILE_NAME_BYTES = 255
export const OPS_URL_BYTES = 2048
export const OPS_READ_IDS = 16

export type InputProblem = "empty" | "tooLong" | "slash" | "badUrl" | null

export function checkLinkUrl(url: string): InputProblem {
  const text = url.trim()
  if (!text) return "empty"
  if (bytes(text) > OPS_URL_BYTES) return "tooLong"
  try {
    const parsed = new URL(text)
    return parsed.protocol === "http:" || parsed.protocol === "https:" ? null : "badUrl"
  } catch { return "badUrl" }
}

export function checkTitle(title: string, limit = OPS_TITLE_BYTES): InputProblem {
  const text = title.trim()
  if (!text) return "empty"
  return bytes(text) > limit ? "tooLong" : null
}

/** A New .md name is sent without `.md`; a typed extension is dropped rather than doubled. */
export function markdownName(input: string): string {
  return input.trim().replace(/\.(md|markdown)$/i, "")
}

export function checkFileName(name: string): InputProblem {
  const text = name.trim()
  if (!text || text === "." || text === "..") return "empty"
  if (text.includes("/") || text.includes("\\")) return "slash"
  return bytes(text) > OPS_FILE_NAME_BYTES ? "tooLong" : null
}

export function fileNameOf(path: string): string {
  return path.split("/").pop() ?? path
}

// ---------------------------------------------------------------- Playbook references

const OPS_ID = /^ope_[0-9a-f]{24}$/
const READ_LINE = /^[ \t]*clawdline ops read((?:[ \t]+\S+)+)[ \t]*$/

function lineIds(line: string): string[] | null {
  const match = READ_LINE.exec(line)
  if (!match) return null
  const ids = match[1].trim().split(/[ \t]+/)
  return ids.every((id) => OPS_ID.test(id)) ? ids : null
}

/** Every Ops id the body's `clawdline ops read …` lines name, first mention first. */
export function opsIdsIn(body: string): string[] {
  const seen = new Set<string>()
  for (const line of body.split("\n")) for (const id of lineIds(line) ?? []) seen.add(id)
  return [...seen]
}

export type InsertResult = { body: string; added: string[] } | { overflow: true }

/**
 * Adds ids to the body as one `clawdline ops read <id> …` line. An existing
 * such line takes them (once each, in order); otherwise a new line ends the
 * body. One line reads at most `OPS_READ_IDS` ids, which the agent command
 * enforces too, so a merge past it is refused rather than truncated.
 */
export function insertOpsIds(body: string, ids: readonly string[]): InsertResult {
  const lines = body.split("\n")
  const index = lines.findIndex((line) => lineIds(line) !== null)
  const present = index >= 0 ? lineIds(lines[index])! : []
  const added = ids.filter((id, at) => OPS_ID.test(id) && !present.includes(id) && ids.indexOf(id) === at)
  const merged = [...present, ...added]
  if (merged.length > OPS_READ_IDS) return { overflow: true }
  if (!added.length) return { body, added }
  const line = `clawdline ops read ${merged.join(" ")}`
  if (index >= 0) { lines[index] = line; return { body: lines.join("\n"), added } }
  const trimmed = body.replace(/\s+$/, "")
  return { body: trimmed ? `${trimmed}\n\n${line}` : line, added }
}

export interface OpsReference { id: string; title: string | null }

/** The ids a body references with their current titles; `null` is not in Ops. */
export function opsReferences(body: string, entries: readonly OpsEntry[]): OpsReference[] {
  const titles = new Map(entries.map((entry) => [entry.id, entry.title]))
  return opsIdsIn(body).map((id) => ({ id, title: titles.get(id) ?? null }))
}

// ---------------------------------------------------------------- sessions

/** Only Sessions of the same canonical Project may be asked to read a link. */
export function sameProjectSessions<T extends { project_key?: string }>(sessions: readonly T[], projectKey: string): T[] {
  return projectKey ? sessions.filter((session) => session.project_key === projectKey) : []
}

// ---------------------------------------------------------------- idempotency

/**
 * One key per person's action. The same action retried (same signature, not
 * yet settled) keeps its key, so a lost answer cannot create two records;
 * a settled action or a changed input gets a new one.
 */
export class IntentKeys {
  private open = new Map<string, string>()
  private readonly mint: () => string
  constructor(mint: () => string = () => globalThis.crypto.randomUUID()) { this.mint = mint }
  key(signature: string): string {
    let key = this.open.get(signature)
    if (!key) { key = this.mint(); this.open.set(signature, key) }
    return key
  }
  settle(signature: string): void { this.open.delete(signature) }
}

/** Why a resolve to-do could not be typed into its Session, from the send route's refusal. */
export type SendProblem = "asking" | "busy" | "other"

/**
 * The send route answers `send_failed` with the terminal's reason first in its
 * detail (`session_asking: …`), or that reason as its own code. A question on
 * the Session's screen is the common first-run case (a folder trust prompt),
 * and the person can fix it; it must not read as an unexplained failure.
 */
export function sendProblem(code: string, detail: string): SendProblem {
  const reason = code === "send_failed" ? detail.split(":", 1)[0].trim() : code
  if (reason === "session_asking") return "asking"
  if (reason === "busy") return "busy"
  return "other"
}
