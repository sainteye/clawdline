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
export interface HeldOutcome { intent: W.ProjectAssistantSendRequest | W.ProjectAssistantPermissionAnswerRequest; receipt: W.ProjectAssistantReceipt | null }
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
export function createViewState(storage?: Pick<Storage, "getItem" | "setItem" | "removeItem">) {
  const drafts = new Map<string, string>(), positions = new Map<string, ReadingPosition>()
  const quotes = new Map<string, string>()
  const recoveryRequired = new Set<string>()
  const heldOutcomes = new Map<string,HeldOutcome>()
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
