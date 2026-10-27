import type { SessionRow } from "@clawdline/contract"
import { sessionSelectionIdentity } from "../legacy/js/session/selection.js"

export interface ComposerDraft { text: string; images: string[] }
type Part = keyof ComposerDraft
export interface DraftStorage {
  read(key: string): Promise<ComposerDraft>
  write(key: string, part: Part, value: string | string[]): Promise<void>
}

/** A restarted execution of the same conversation keeps its draft; a reused terminal does not. */
export function composerDraftKey(row: SessionRow | null): string {
  const identity = sessionSelectionIdentity(row)
  return identity ? JSON.stringify([identity.machine, identity.session, row?.sessionId || null]) : ""
}

/** Only unsaved fields stay in memory. Failed storage keeps them for this page's lifetime. */
export function createComposerDrafts(storage: DraftStorage) {
  const pending = new Map<string, Partial<ComposerDraft>>()
  const imageListeners = new Map<string, Set<(urls: string[]) => void>>()
  const write = async <K extends Part>(key: string, part: K, value: ComposerDraft[K]) => {
    if (!key) return
    const fields = pending.get(key) ?? {}
    fields[part] = value
    pending.set(key, fields)
    if (part === "images") for (const listener of imageListeners.get(key) ?? []) listener(value as string[])
    await storage.write(key, part, value)
    if (fields[part] === value) delete fields[part]
    if (!Object.keys(fields).length && pending.get(key) === fields) pending.delete(key)
  }
  return {
    subscribeImages(key: string, listener: (urls: string[]) => void): () => void {
      const listeners = imageListeners.get(key) ?? new Set()
      listeners.add(listener)
      imageListeners.set(key, listeners)
      return () => {
        listeners.delete(listener)
        if (!listeners.size) imageListeners.delete(key)
      }
    },
    async read(key: string): Promise<ComposerDraft & { durable: boolean }> {
      if (!key) return { text: "", images: [], durable: true }
      let saved: ComposerDraft = { text: "", images: [] }
      let durable = true
      try { saved = await storage.read(key) } catch { durable = false }
      // Overlay edits that happened while the database was opening or reading.
      return { ...saved, ...pending.get(key), durable }
    },
    stageText(key: string, text: string) {
      if (!key) return
      const fields = pending.get(key) ?? {}
      fields.text = text
      pending.set(key, fields)
    },
    flushText(key: string): Promise<void> {
      const text = pending.get(key)?.text
      return text === undefined ? Promise.resolve() : write(key, "text", text)
    },
    text: (key: string, text: string) => write(key, "text", text),
    images: (key: string, images: string[]) => write(key, "images", images),
  }
}

/** Text is synchronously saved at lifecycle boundaries; picture changes use IndexedDB. */
export function browserDraftStorage(factory: IDBFactory | undefined = globalThis.indexedDB): DraftStorage {
  let database: Promise<IDBDatabase> | undefined
  const open = () => database ??= new Promise<IDBDatabase>((resolve, reject) => {
    if (!factory) { reject(new Error("IndexedDB is unavailable")); return }
    const request = factory.open("clawdline-composer-drafts", 1)
    request.onupgradeneeded = () => { request.result.createObjectStore("fields") }
    request.onerror = () => reject(request.error)
    request.onblocked = () => reject(new Error("Draft storage is blocked"))
    request.onsuccess = () => {
      const db = request.result
      db.onversionchange = () => { db.close(); database = undefined }
      resolve(db)
    }
  })
  const transaction = async <T>(mode: IDBTransactionMode, run: (store: IDBObjectStore) => () => T): Promise<T> => {
    const db = await open()
    return new Promise<T>((resolve, reject) => {
      const tx = db.transaction("fields", mode)
      const answer = run(tx.objectStore("fields"))
      tx.oncomplete = () => resolve(answer())
      tx.onabort = tx.onerror = () => reject(tx.error ?? new Error("Draft storage failed"))
    })
  }
  return {
    async read(key) {
      const text = localStorage.getItem("clawdline.composer-draft:" + key) ?? ""
      const images = await transaction("readonly", store => {
        const request = store.get([key, "images"])
        return () => Array.isArray(request.result) && request.result.every((url: unknown) => typeof url === "string") ? request.result as string[] : []
      })
      return { text, images }
    },
    async write(key, part, value) {
      // No await before the text write: pagehide/unmount must finish it before the page goes.
      if (part === "text") {
        const storageKey = "clawdline.composer-draft:" + key
        if (value.length) localStorage.setItem(storageKey, value as string)
        else localStorage.removeItem(storageKey)
        return
      }
      await transaction("readwrite", store => {
        if (value.length) store.put(value, [key, part])
        else store.delete([key, part])
        return () => undefined
      })
    },
  }
}

export const composerDrafts = createComposerDrafts(browserDraftStorage())
