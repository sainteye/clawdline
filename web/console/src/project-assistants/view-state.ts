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
/** One unresolved effect per account/Bot, owned by the existing host lifetime. Never replayed. */
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
 * At most as many uncertain sends as the source can hold pending turns for one
 * Bot (capacity `bots.pending_turns`, pa.MaxPendingTurns); a held text larger
 * than the source accepts (`bots.message_bytes`, pa.MaxMessageBytes) is not
 * journaled. Both bounds are the source's own registered limits.
 */
export const MAX_HELD_SENDS = 16, MAX_HELD_TEXT_BYTES = 32 << 10
/** Why this device cannot vouch for a held send after a reload. */
export type HeldJournalProblem = "unsaved" | "unreadable"
type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">
interface Journal { active: HeldOutcome | null; aside: HeldOutcome[]; problem: HeldJournalProblem | null; unreadable: boolean }
export const heldJournalKey = (draftKey: string) => "clawdline.bot-held." + draftKey
const bytes = (text: string) => new TextEncoder().encode(text).length
const str = (v: unknown, max = 512): v is string => typeof v === "string" && v.length <= max
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
  if (r !== null && (!r || typeof r !== "object" || r.state !== "unknown" || !r.command || r.command.idempotency_key !== c.idempotency_key)) return null
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
    j = { active: null, aside: [], problem: null, unreadable: false }
    if (this.storage) {
      let raw: string | null = null
      try { raw = this.storage.getItem(heldJournalKey(key)) } catch { j.problem = "unsaved" }
      if (raw !== null) {
        try {
          const v = JSON.parse(raw)
          if (v?.v !== 1 || !Array.isArray(v.aside) || v.aside.length > MAX_HELD_SENDS) throw new Error("shape")
          const active = v.active === null ? null : decodeOutcome(v.active, key)
          if (v.active !== null && !active) throw new Error("active")
          const aside = v.aside.map((x: unknown) => decodeOutcome(x, key))
          if (aside.some((x: HeldOutcome | null) => !x)) throw new Error("aside")
          j.active = active; j.aside = aside; j.unreadable = v.unreadable === true
        } catch { j.problem = "unreadable" }
      }
    }
    this.journals.set(key, j)
    return j
  }
  private save(key: string, j: Journal) {
    // An unreadable record is kept until the person explicitly sets it aside.
    if (!this.storage || j.problem === "unreadable") return
    const sends = (o: HeldOutcome | null) => !!o && o.intent.command.operation === "send"
    const active = sends(j.active) ? j.active : null
    try {
      if (!active && !j.aside.length && !j.unreadable) this.storage.removeItem(heldJournalKey(key))
      else {
        const all = [...active ? [active] : [], ...j.aside]
        if (all.some(o => bytes((o.intent as W.ProjectAssistantSendRequest).text ?? "") > MAX_HELD_TEXT_BYTES)) throw new Error("bound")
        this.storage.setItem(heldJournalKey(key), JSON.stringify({ v: 1, active: active && encodeOutcome(active), aside: j.aside.map(encodeOutcome), ...(j.unreadable ? { unreadable: true } : {}) }))
      }
      if (j.problem === "unsaved") j.problem = null
    } catch { j.problem = "unsaved" }
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
   * A permission answer is not a message and is never released here. Refused
   * when the aside list is full.
   */
  setAside(key: string): boolean {
    const j = this.load(key)
    if (j.problem === "unreadable") { j.problem = null; j.unreadable = true; this.save(key, j); return true }
    const active = j.active
    if (!active || active.intent.command.operation !== "send" || j.aside.length >= MAX_HELD_SENDS) return false
    j.aside = [...j.aside, active]; j.active = null; this.save(key, j); return true
  }
  /** Removes one set-aside send once its own receipt settled it. */
  settleAside(key: string, idempotencyKey: string) {
    const j = this.load(key), before = j.aside.length
    j.aside = j.aside.filter(o => o.intent.command.idempotency_key !== idempotencyKey)
    if (j.aside.length !== before) this.save(key, j)
  }
  dismissUnreadable(key: string) { const j = this.load(key); if (j.unreadable) { j.unreadable = false; this.save(key, j) } }
}
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
const admitted = new Set<W.ProjectAssistantEffectState>(["accepted", "executing", "delivered", "observed", "read"])
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
/** Owned by the feature host so destination remounts keep drafts and reading positions. */
export function createViewState(storage?: Store) {
  const drafts = new Map<string, string>(), positions = new Map<string, ReadingPosition>()
  const quotes = new Map<string, string>()
  const recoveryRequired = new Set<string>()
  const heldOutcomes = new HeldOutcomes(storage)
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
  }
}
export type ViewState = ReturnType<typeof createViewState>
