import type { MarkupPicture } from "../session/ImageMarkup.js"
import type { DraftInput } from "./inputs"

export interface SendAttachments {
  images: MarkupPicture[]
  files: DraftInput[]
}

export const sendAttachmentKey = (draftKey: string, conversation: string, id: string, attempt = "") =>
  JSON.stringify([draftKey, conversation, id, attempt])

/** Keep bytes out of the synchronous journal, and scope them to the original chat and send. */
async function sendAttachments<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const open = indexedDB.open("clawdline-bot-outgoing", 1)
    open.onupgradeneeded = () => open.result.createObjectStore("attachments")
    open.onsuccess = () => resolve(open.result)
    open.onerror = () => reject(open.error)
    open.onblocked = () => reject(new Error("Outgoing attachment storage is blocked"))
  })
  try {
    return await new Promise<T>((resolve, reject) => {
      const transaction = db.transaction("attachments", mode)
      const request = run(transaction.objectStore("attachments"))
      transaction.oncomplete = () => resolve(request.result)
      transaction.onabort = () => reject(transaction.error)
    })
  } finally {
    db.close()
  }
}

export const loadSendAttachments = (key: string): Promise<SendAttachments | undefined> =>
  sendAttachments("readonly", store => store.get(key))

export const saveSendAttachments = (key: string, value: SendAttachments) =>
  sendAttachments("readwrite", store => store.put(value, key))

export const clearSendAttachments = (key: string) =>
  sendAttachments("readwrite", store => store.delete(key)).catch(() => {})
