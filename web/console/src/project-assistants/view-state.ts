import type * as W from "@clawdline/contract"

export const botIdentity = (key: W.ProjectAssistantBotKey) => JSON.stringify([key.machine_id, key.bot_id])
export const draftIdentity = (account: string, key: W.ProjectAssistantBotKey) => JSON.stringify(["project-assistants", account, key.machine_id, key.bot_id])
export const sameBot = (a: W.ProjectAssistantBotKey, b: W.ProjectAssistantBotKey) => a.machine_id === b.machine_id && a.bot_id === b.bot_id
/** Persisted facts belong to a conversation, across execution replacements. */
export const sameConversation = (a: Pick<W.ProjectAssistantFence, "key" | "project" | "conversation_id">, b: Pick<W.ProjectAssistantFence, "key" | "project" | "conversation_id">) => sameBot(a.key, b.key)
  && a.project.machine_id === b.project.machine_id && a.project.project_id === b.project.project_id
  && a.project.canonical_id === b.project.canonical_id && a.project.kind === b.project.kind
  && a.conversation_id === b.conversation_id
export const sameFence = (a: W.ProjectAssistantFence, b: W.ProjectAssistantFence) => sameConversation(a, b)
  && a.binding_id === b.binding_id && a.generation === b.generation
export const canRead = (source: W.ProjectAssistantSourceStatus) => source.readable && source.access !== "hidden" && source.state !== "revoked" && source.state !== "unpaired"
export const canWrite = (source: W.ProjectAssistantSourceStatus) => canRead(source) && source.writable && source.access === "interactive" && source.state === "online" && source.fresh
export function mergeRoster(pages: readonly (readonly W.ProjectAssistantBotSummary[])[]): W.ProjectAssistantBotSummary[] {
  const byKey = new Map<string, W.ProjectAssistantBotSummary>()
  for (const item of pages.flat()) {
    if (!canRead(item.source)) continue
    const id = botIdentity(item.bot.key), old = byKey.get(id)
    if (!old || item.version >= old.version) byKey.set(id, item)
  }
  return [...byKey.values()].sort((a,b) => b.activity_at.localeCompare(a.activity_at)
    || a.bot.key.machine_id.localeCompare(b.bot.key.machine_id) || a.bot.key.bot_id.localeCompare(b.bot.key.bot_id))
}

export interface ReadingPosition { top: number; following: boolean }
/**
 * One unobserved send or unresolved effect per account/Bot, owned by the host. Never replayed.
 * A send's open request belongs to this lifetime too, so leaving its Chat keeps its receipt.
 */
export interface HeldOutcome {
  intent: W.ProjectAssistantSendRequest | W.ProjectAssistantPermissionAnswerRequest; receipt: W.ProjectAssistantReceipt | null
  /** Pictures this send carried. Their data is never stored, so after a reload only the count remains. */
  pictures?: number
  /** Names of the uploaded files the send referenced by `upload_ids`. */
  files?: string[]
  /** Read back from storage: the intent lost its pictures' data with the page that sent it. */
  restored?: boolean
}
/**
 * A held text larger than the source accepts (`bots.message_bytes`,
 * pa.MaxMessageBytes) is not journaled. Retained history does not consume
 * the source's pending execution slots.
 */
export const MAX_HELD_TEXT_BYTES = 32 << 10
/** Why this device cannot vouch for a held send after a reload. */
export type HeldJournalProblem = "unsaved" | "unreadable"
type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">
interface Journal { active: HeldOutcome | null; aside: HeldOutcome[]; problem: HeldJournalProblem | null; unreadable: boolean; preservedRaw: string[]; unreadableRaw?: string }
export const heldJournalKey = (draftKey: string) => "clawdline.bot-held." + draftKey
const bytes = (text: string) => new TextEncoder().encode(text).length
const str = (v: unknown): v is string => typeof v === "string"
const admitted = new Set<W.ProjectAssistantEffectState>(["accepted", "executing", "delivered", "observed", "read"])
function decodeOutcome(v: unknown, draftKey: string): HeldOutcome | null {
  const o = v as Record<string, any>
  const i = o?.intent, c = i?.command, f = i?.fence
  // Only a message send is journaled; a permission answer stays host-owned.
  if (!o || typeof o !== "object" || !i || !c || c.operation !== "send" || !str(c.idempotency_key) || !c.idempotency_key || !str(c.content_hash)) return null
  if (!c.key || !str(c.key.machine_id) || !str(c.key.bot_id)) return null
  let owner: unknown; try { owner = JSON.parse(draftKey) } catch { return null }
  if (!Array.isArray(owner) || owner[2] !== c.key.machine_id || owner[3] !== c.key.bot_id) return null
  if (!f || !f.key || !sameBot(f.key, c.key) || !f.project || !str(f.conversation_id) || !str(f.binding_id) || typeof f.generation !== "number") return null
  if (typeof i.text !== "string" || bytes(i.text) > MAX_HELD_TEXT_BYTES || !str(i.quote_message_id) || "images" in i) return null
  if (i.upload_ids !== undefined && (!Array.isArray(i.upload_ids) || i.upload_ids.length > 4 || !i.upload_ids.every((x: unknown) => str(x)))) return null
  if (o.files !== undefined && (!Array.isArray(o.files) || o.files.length > 4 || !o.files.every((x: unknown) => str(x)))) return null
  if (o.pictures !== undefined && (!Number.isInteger(o.pictures) || o.pictures < 0 || o.pictures > 4)) return null
  const r = o.receipt
  if (r !== null) {
    if (!r || typeof r !== "object" || !r.command || r.command.idempotency_key !== c.idempotency_key) return null
    // Keep old uncertain records readable. New known admissions retain their
    // own source receipt until the matching message is actually observed.
    if (r.state !== "unknown" && (!admitted.has(r.state) || r.command.operation !== "send" || !r.command.key || !sameBot(r.command.key,c.key) || !str(r.command.content_hash) || !str(r.id) || !r.id || !str(r.resource_id) || !r.resource_id || !Number.isSafeInteger(r.event_seq) || r.event_seq < 0)) return null
  }
  return { intent: i, receipt: r, ...(o.pictures ? { pictures: o.pictures } : {}), ...(o.files?.length ? { files: o.files } : {}), restored: true }
}
/** What storage keeps of a held send: its command, text, quote, upload references and receipt. No picture data, no credentials. */
function encodeOutcome(o: HeldOutcome) {
  const { images, ...intent } = o.intent as W.ProjectAssistantSendRequest
  const pictures = o.pictures ?? images?.length ?? 0
  return { intent, receipt: o.receipt, ...(pictures ? { pictures } : {}), ...(o.files?.length ? { files: o.files } : {}) }
}
/**
 * The held sends for each account/Bot. The one in the box is `active`; the
 * person may set it aside to write a new message, and it stays in `aside`
 * with its own check. A send is journaled to storage before its transport so
 * a full reload still holds it; nothing here ever sends.
 */
export class HeldOutcomes {
  private readonly journals = new Map<string, Journal>()
  private readonly storage?: Store
  constructor(storage?: Store) { this.storage = storage }
  private load(key: string): Journal {
    let j = this.journals.get(key)
    if (j) return j
    j = { active: null, aside: [], problem: null, unreadable: false, preservedRaw: [] }
    if (this.storage) {
      let raw: string | null = null
      try { raw = this.storage.getItem(heldJournalKey(key)) } catch { j.problem = "unsaved" }
      if (raw !== null) {
        try {
          const v = JSON.parse(raw)
          if (v?.v !== 1 || !Array.isArray(v.aside)) throw new Error("shape")
          if (v.preservedRaw !== undefined && (!Array.isArray(v.preservedRaw) || !v.preservedRaw.every(str))) throw new Error("preserved")
          const active = v.active === null ? null : decodeOutcome(v.active, key)
          if (v.active !== null && !active) throw new Error("active")
          const aside = v.aside.map((x: unknown) => decodeOutcome(x, key))
          if (aside.some((x: HeldOutcome | null) => !x)) throw new Error("aside")
          j.active = active; j.aside = aside; j.unreadable = v.unreadable === true; j.preservedRaw = v.preservedRaw ?? []
        } catch { j.problem = "unreadable"; j.unreadableRaw = raw }
      }
    }
    this.journals.set(key, j)
    return j
  }
  private save(key: string, j: Journal) {
    // An unreadable record is kept until the person explicitly sets it aside.
    if (!this.storage || j.problem === "unreadable") return false
    const sends = (o: HeldOutcome | null) => !!o && o.intent.command.operation === "send"
    const active = sends(j.active) ? j.active : null
    try {
      if (!active && !j.aside.length && !j.unreadable && !j.preservedRaw.length) this.storage.removeItem(heldJournalKey(key))
      else {
        const all = [...active ? [active] : [], ...j.aside]
        if (all.some(o => bytes((o.intent as W.ProjectAssistantSendRequest).text ?? "") > MAX_HELD_TEXT_BYTES)) throw new Error("bound")
        this.storage.setItem(heldJournalKey(key), JSON.stringify({ v: 1, active: active && encodeOutcome(active), aside: j.aside.map(encodeOutcome), ...(j.unreadable ? { unreadable: true } : {}), ...(j.preservedRaw.length ? { preservedRaw: j.preservedRaw } : {}) }))
      }
      if (j.problem === "unsaved") j.problem = null
      return true
    } catch { j.problem = "unsaved"; return false }
  }
  get size() { let n = 0; for (const j of this.journals.values()) if (j.active) n++; return n }
  has(key: string) { return !!this.load(key).active }
  get(key: string) { return this.load(key).active ?? undefined }
  set(key: string, outcome: HeldOutcome) { const j = this.load(key); j.active = outcome; this.save(key, j); return this }
  delete(key: string) { const j = this.load(key), had = !!j.active; j.active = null; this.save(key, j); return had }
  /** Sends the person set aside to write a new message; each keeps its own check. */
  aside(key: string): readonly HeldOutcome[] { return this.load(key).aside }
  /** Why this device could not keep, or could not read back, the held record. */
  problem(key: string) { return this.load(key).problem }
  /** An earlier record could not be read; set aside by the person, it stays named. */
  unreadable(key: string) { return this.load(key).unreadable }
  /**
   * Moves the held send out of the box so the person can write a new message.
   * A permission answer is not a message and is never released here.
   */
  setAside(key: string): boolean {
    const j = this.load(key)
    if (j.problem === "unreadable") {
      const next: Journal = { ...j, problem: null, unreadable: true, preservedRaw: [...j.preservedRaw, j.unreadableRaw!] }
      // Keep the original record and lock if its replacement cannot preserve it.
      if (!this.save(key, next)) return false
      this.journals.set(key, next); return true
    }
    const active = j.active
    if (!active || active.intent.command.operation !== "send") return false
    j.aside = [...j.aside, active]; j.active = null; this.save(key, j); return true
  }
  /** Removes one set-aside send once its own receipt settled it. */
  settleAside(key: string, idempotencyKey: string) {
    const j = this.load(key), before = j.aside.length
    j.aside = j.aside.filter(o => o.intent.command.idempotency_key !== idempotencyKey)
    if (j.aside.length !== before) this.save(key, j)
  }
  dismissUnreadable(key: string) { const j = this.load(key); if (j.unreadable) { j.unreadable = false; this.save(key, j) } }
  /**
   * Records one send's own answer by its command identity. A newer held send
   * in the box is never replaced, and a send set aside keeps its place: its
   * answer updates or settles only that entry. Returns where the send was.
   */
  /** The held send with this command identity, in the box or set aside. */
  find(key: string, idempotencyKey: string) {
    const j = this.load(key)
    return j.active?.intent.command.idempotency_key === idempotencyKey ? j.active : j.aside.find(o => o.intent.command.idempotency_key === idempotencyKey)
  }
  settle(key: string, idempotencyKey: string, outcome: HeldOutcome | null): "active" | "aside" | "none" {
    const j = this.load(key)
    if (j.active?.intent.command.idempotency_key === idempotencyKey) { j.active = outcome; this.save(key, j); return "active" }
    const at = j.aside.findIndex(o => o.intent.command.idempotency_key === idempotencyKey)
    if (at < 0) return "none"
    j.aside = outcome ? j.aside.map((o, n) => n === at ? outcome : o) : j.aside.filter((_, n) => n !== at)
    this.save(key, j); return "aside"
  }
  // Sends whose request this page still has open. Memory only: a reload loses
  // the request, so its journaled send stays genuinely unknown.
  private readonly transports = new Map<string, Set<string>>()
  private readonly listeners = new Map<string, Set<(landed: SendLanded) => void>>()
  /** The send's bytes may cross now; its answer will land here, whichever Chat is mounted. */
  beginTransport(key: string, idempotencyKey: string) {
    const open = this.transports.get(key) ?? new Set<string>(); open.add(idempotencyKey); this.transports.set(key, open)
  }
  transporting(key: string, idempotencyKey: string) { return !!this.transports.get(key)?.has(idempotencyKey) }
  /** The request ended, answered or not; every Chat watching this Bot is told once. */
  endTransport(key: string, idempotencyKey: string, receipt: W.ProjectAssistantReceipt | null) {
    const open = this.transports.get(key)
    if (!open?.delete(idempotencyKey)) return
    if (!open.size) this.transports.delete(key)
    for (const listen of [...this.listeners.get(key) ?? []]) listen({ idempotency_key: idempotencyKey, receipt })
  }
  subscribe(key: string, listen: (landed: SendLanded) => void) {
    const set = this.listeners.get(key) ?? new Set(); set.add(listen); this.listeners.set(key, set)
    return () => { set.delete(listen); if (!set.size && this.listeners.get(key) === set) this.listeners.delete(key) }
  }
}
/** One send's request ended; its outcome is already in the journal. */
export interface SendLanded { idempotency_key: string; receipt: W.ProjectAssistantReceipt | null }
/**
 * What one explicit read of the source's durable receipts says about a held
 * send. `delivered`: the source admitted exactly this command and its turn is
 * in this conversation. `failed`: the source recorded that it did not start.
 * `unknown`: the source recorded an unknown effect. `missing`: no receipt yet,
 * which never proves non-delivery. `invalid`: the answer cannot be trusted for
 * this Bot, Project, conversation or command.
 */
export type HeldInspection =
  | { state: "delivered"; receipt: W.ProjectAssistantReceipt; turn_id: string }
  | { state: "failed" | "unknown"; receipt: W.ProjectAssistantReceipt }
  | { state: "missing" | "invalid" }
export function inspectHeldSend(value: W.ProjectAssistantExport, held: HeldOutcome, scope: Pick<W.ProjectAssistantFence, "key" | "project" | "conversation_id">): HeldInspection {
  const intent = held.intent, c = intent.command
  // A permission answer is host-owned and never becomes a message outcome.
  if (c.operation !== "send" || !("text" in intent) || !sameBot(c.key, scope.key)) return { state: "invalid" }
  if (!value?.bot?.key || !value.bot.project || !value.conversation || !Array.isArray(value.receipts) || !Array.isArray(value.turns)) return { state: "invalid" }
  if (!sameConversation({ key: value.bot.key, project: value.bot.project, conversation_id: value.conversation.id }, scope)) return { state: "invalid" }
  const matches = value.receipts.filter(r => !!r?.command?.key && sameBot(r.command.key, c.key) && r.command.operation === "send" && r.command.idempotency_key === c.idempotency_key)
  if (!matches.length) return { state: "missing" }
  if (matches.length > 1) return { state: "invalid" }
  const receipt = matches[0]
  if (receipt.state === "failed") return { state: "failed", receipt }
  const turn = value.turns.find(t => t?.id === receipt.resource_id)
  if (admitted.has(receipt.state) || receipt.state === "unknown") {
    // Admission creates the turn and its message together; a receipt without
    // its own turn in this conversation is not evidence for this chat.
    if (!turn || turn.idempotency_key !== c.idempotency_key || !turn.fence || !sameConversation(turn.fence, scope)) return receipt.state === "unknown" ? { state: "unknown", receipt } : { state: "invalid" }
    return { state: "delivered", receipt, turn_id: turn.id }
  }
  return { state: "invalid" }
}
/** A plain draft has no command identity and cannot establish any delivery fact. */
export interface UnverifiedDraft {
  scope: Pick<W.ProjectAssistantFence, "key" | "project" | "conversation_id">
  text: string
  quote: string
  continued: boolean
}
export const unverifiedDraftKey = (draftKey: string) => "clawdline.bot-unverified." + draftKey
/** Local migration evidence only: never inspected, replayed or converted into a command. */
export class UnverifiedDrafts {
  private readonly records = new Map<string, UnverifiedDraft[]>()
  private readonly problems = new Map<string, HeldJournalProblem>()
  private readonly storage?: Store
  constructor(storage?: Store) { this.storage = storage }
  private load(key: string): UnverifiedDraft[] {
    const cached = this.records.get(key)
    if (cached) return cached
    let rows: UnverifiedDraft[] = []
    try {
      const raw = this.storage?.getItem(unverifiedDraftKey(key))
      if (raw) {
        const value = JSON.parse(raw), owner = JSON.parse(key)
        if (value?.v !== 1 || !Array.isArray(value.drafts)) throw new Error("shape")
        for (const d of value.drafts) {
          const f = d?.scope, p = f?.project
          if (!f?.key || f.key.machine_id !== owner[2] || f.key.bot_id !== owner[3]
            || !p || ![p.machine_id,p.project_id,p.canonical_id,p.kind,f.conversation_id,d.text,d.quote].every(str)
            || typeof d.continued !== "boolean") throw new Error("draft")
        }
        rows = value.drafts
      }
    } catch { this.problems.set(key, "unreadable") }
    this.records.set(key, rows); return rows
  }
  private save(key: string, rows: UnverifiedDraft[]) {
    if (this.problems.get(key) === "unreadable") return false
    try {
      if (!this.storage) throw new Error("storage")
      this.storage.setItem(unverifiedDraftKey(key), JSON.stringify({ v: 1, drafts: rows }))
      this.problems.delete(key); return true
    } catch { this.problems.set(key, "unsaved"); return false }
  }
  history(key: string, scope: UnverifiedDraft["scope"]) { return this.load(key).filter(d => sameConversation(d.scope, scope)) }
  active(key: string, scope: UnverifiedDraft["scope"]) { return this.history(key, scope).find(d => !d.continued) }
  continuing(key: string, scope: UnverifiedDraft["scope"]) { const rows = this.history(key, scope); return rows.some(d => d.continued) && !rows.some(d => !d.continued) }
  problem(key: string) { this.load(key); return this.problems.get(key) }
  capture(key: string, scope: UnverifiedDraft["scope"], text: string, quote: string) {
    if (!text || this.active(key, scope) || this.continuing(key, scope)) return
    const draft: UnverifiedDraft = { scope: { key: { ...scope.key }, project: { ...scope.project }, conversation_id: scope.conversation_id }, text, quote, continued: false }
    const rows = [...this.load(key), draft]; this.records.set(key, rows); this.save(key, rows)
  }
  /** Preserve first, clear the old plain slot, then publish permission to start empty. */
  writeNew(key: string, scope: UnverifiedDraft["scope"]) {
    const rows = this.load(key), active = this.active(key, scope)
    if (!active || !this.save(key, rows)) return false
    try { this.storage!.removeItem(key) } catch { this.problems.set(key, "unsaved"); return false }
    const next = rows.map(d => d === active ? { ...d, continued: true } : d)
    // A failed final write leaves the durable active record and lock intact.
    if (!this.save(key, next)) return false
    this.records.set(key, next); return true
  }
}
/** Owned by the feature host so destination remounts keep drafts and reading positions. */
export function createViewState(storage?: Store) {
  const drafts = new Map<string, string>(), positions = new Map<string, ReadingPosition>()
  const quotes = new Map<string, string>()
  const recoveryRequired = new Set<string>()
  const heldOutcomes = new HeldOutcomes(storage)
  const unverifiedDrafts = new UnverifiedDrafts(storage)
  return {
    draft(key: string) {
      if (!drafts.has(key)) { try { drafts.set(key, storage?.getItem(key) ?? "") } catch { drafts.set(key, "") } }
      return drafts.get(key) ?? ""
    },
    stage(key: string, text: string) { drafts.set(key, text) },
    flush(key: string) {
      try { if(!storage)return false;const text = drafts.get(key); if (text) storage.setItem(key,text); else storage.removeItem(key); return true } catch { return false }
    },
    position: positions,
    quotes,
    recoveryRequired,
    heldOutcomes,
    unverifiedDrafts,
  }
}
export type ViewState = ReturnType<typeof createViewState>
