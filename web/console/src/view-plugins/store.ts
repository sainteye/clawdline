import { fail, parsePackage, type ViewPackage } from "./package.js"
import { PLUGIN_LIMITS as B } from "./limits.js"

interface Revision { archive: ArrayBuffer; digest: string }
interface Saved { id: string; enabled: boolean; active: Revision; previous?: Revision }
export interface Installed { id: string; enabled: boolean; active: ViewPackage; previous?: ViewPackage }
const listeners = new Set<() => void>()
let database: Promise<IDBDatabase> | null = null
let broadcast: BroadcastChannel | null = null
function notify() { for (const fn of listeners) fn() }
export function subscribePackages(fn: () => void): () => void {
  listeners.add(fn)
  if (!broadcast && typeof BroadcastChannel !== "undefined") {
    broadcast = new BroadcastChannel("clawdline-view-plugins-v1")
    broadcast.onmessage = notify // A hint only; the authoritative registry is re-read.
  }
  return () => { listeners.delete(fn); if (!listeners.size) { broadcast?.close(); broadcast = null } }
}
function changed() { notify(); broadcast?.postMessage("changed") }
function db(): Promise<IDBDatabase> {
  if (!database) database = new Promise((resolve, reject) => {
    if (typeof indexedDB === "undefined") { reject(new Error("storage_unavailable")); return }
    const opening = indexedDB.open("clawdline-view-plugins", 1)
    let refused = false
    opening.onupgradeneeded = () => opening.result.createObjectStore("packages", { keyPath: "id" })
    opening.onerror = () => { database = null; reject(new Error("storage_unavailable")) }
    opening.onblocked = () => { refused = true; database = null; reject(new Error("storage_unavailable")) }
    opening.onsuccess = () => { const value = opening.result; if (refused) { value.close(); return }; value.onversionchange = () => { value.close(); database = null }; resolve(value) }
  })
  return database
}
const request = <T>(r: IDBRequest<T>) => new Promise<T>((resolve, reject) => { r.onsuccess = () => resolve(r.result); r.onerror = () => reject(new Error("storage_unavailable")) })
async function unpack(r: Revision): Promise<ViewPackage> {
  if (!r || !(r.archive instanceof ArrayBuffer)) fail("storage_unavailable")
  const result = await parsePackage(r.archive)
  if (result.digest !== r.digest) fail("storage_unavailable")
  return result
}
export async function installedPackages(): Promise<Installed[]> {
  const conn = await db(), tx = conn.transaction("packages", "readonly")
  const saved = await request(tx.objectStore("packages").getAll(undefined, B.packages + 1)) as Saved[]
  if (saved.length > B.packages) fail("storage_unavailable")
  return Promise.all(saved.map(async r => {
    const active = await unpack(r.active), previous = r.previous ? await unpack(r.previous) : undefined
    if (r.id !== active.manifest.id || (previous && previous.manifest.id !== r.id) || typeof r.enabled !== "boolean") fail("storage_unavailable")
    return { id: r.id, enabled: r.enabled, active, previous }
  }))
}
async function change(id: string, expected: string | null, apply: (r: Saved | undefined, count: number) => Saved | null): Promise<void> {
  const conn = await db()
  await new Promise<void>((resolve, reject) => {
    const tx = conn.transaction("packages", "readwrite"), store = tx.objectStore("packages")
    let error: unknown
    tx.oncomplete = () => resolve()
    tx.onabort = () => reject(error ?? new Error("storage_unavailable"))
    tx.onerror = () => { error ??= new Error("storage_unavailable") }
    void Promise.all([request(store.get(id)), request(store.count())]).then(([old, count]) => {
      try {
        const row = old as Saved | undefined
        if ((row?.active.digest ?? null) !== expected) fail("registry_changed")
        const next = apply(row, count)
        if (next) store.put(next); else store.delete(id)
      } catch (e) { error = e; tx.abort() }
    }, e => { error = e; tx.abort() })
  })
  changed()
}
export async function installPackage(archive: ArrayBuffer, expectedDigest: string | null): Promise<void> {
  const value = await parsePackage(archive)
  await change(value.manifest.id, expectedDigest, (old, count) => {
    if (!old && count >= B.packages) fail("package_capacity")
    if (old?.active.digest === value.digest) fail("already_installed")
    return { id: value.manifest.id, enabled: old?.enabled ?? true,
      active: { archive: archive.slice(0), digest: value.digest }, previous: old?.active }
  })
}
export const enablePackage = (p: Installed, enabled: boolean) => change(p.id, p.active.digest, old => {
  if (!old) fail("registry_changed"); return { ...old, enabled }
})
export const removePackage = (p: Installed) => change(p.id, p.active.digest, () => null)
export const rollbackPackage = (p: Installed) => change(p.id, p.active.digest, old => {
  if (!old?.previous) fail("registry_changed")
  return { ...old, active: old.previous, previous: old.active }
})
