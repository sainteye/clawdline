import type * as W from "@clawdline/contract"

export const botIdentity = (key: W.ProjectAssistantBotKey) => JSON.stringify([key.machine_id, key.bot_id])
export const draftIdentity = (account: string, key: W.ProjectAssistantBotKey) => JSON.stringify(["project-assistants", account, key.machine_id, key.bot_id])
export const sameBot = (a: W.ProjectAssistantBotKey, b: W.ProjectAssistantBotKey) => a.machine_id === b.machine_id && a.bot_id === b.bot_id
export const sameFence = (a: W.ProjectAssistantFence, b: W.ProjectAssistantFence) => sameBot(a.key, b.key)
  && a.project.machine_id === b.project.machine_id && a.project.project_id === b.project.project_id
  && a.project.canonical_id === b.project.canonical_id && a.project.kind === b.project.kind
  && a.conversation_id === b.conversation_id && a.binding_id === b.binding_id && a.generation === b.generation
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
/** Owned by the feature host so destination remounts keep drafts and reading positions. */
export function createViewState(storage?: Pick<Storage, "getItem" | "setItem" | "removeItem">) {
  const drafts = new Map<string, string>(), positions = new Map<string, ReadingPosition>()
  const quotes = new Map<string, string>()
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
  }
}
export type ViewState = ReturnType<typeof createViewState>
