import type { SessionRow } from "@clawdline/contract"
import { sessionSelectionIdentity } from "../legacy/js/session/selection.js"

export interface ComposerDraft { text: string }
export interface DraftStorage {
  read(key: string): Promise<string>
  write(key: string, text: string): Promise<void>
}

/** A restarted execution of the same conversation keeps its draft; a reused terminal does not. */
export function composerDraftKey(row: SessionRow | null): string {
  const identity = sessionSelectionIdentity(row)
  return identity ? JSON.stringify([identity.machine, identity.session, row?.sessionId || null]) : ""
}

/** Pictures belong to the current editor; only unsaved text is retained here. */
export function createComposerDrafts(storage: DraftStorage) {
  const pending = new Map<string, string>()
  const write = async (key: string, text: string) => {
    if (!key) return
    pending.set(key, text)
    await storage.write(key, text)
    if (pending.get(key) === text) pending.delete(key)
  }
  return {
    async read(key: string): Promise<ComposerDraft & { durable: boolean }> {
      if (!key) return { text: "", durable: true }
      let text = ""
      let durable = true
      try { text = await storage.read(key) } catch { durable = false }
      return { text: pending.get(key) ?? text, durable }
    },
    stageText(key: string, text: string) {
      if (key) pending.set(key, text)
    },
    flushText(key: string): Promise<void> {
      const text = pending.get(key)
      return text === undefined ? Promise.resolve() : write(key, text)
    },
    text: write,
  }
}

/** No await before a text write: pagehide/unmount must finish it before the page goes. */
export function browserDraftStorage(): DraftStorage {
  return {
    async read(key) {
      return localStorage.getItem("clawdline.composer-draft:" + key) ?? ""
    },
    async write(key, text) {
      const storageKey = "clawdline.composer-draft:" + key
      if (text.length) localStorage.setItem(storageKey, text)
      else localStorage.removeItem(storageKey)
    },
  }
}

export const composerDrafts = createComposerDrafts(browserDraftStorage())
