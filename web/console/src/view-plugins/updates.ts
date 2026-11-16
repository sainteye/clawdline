import { PLUGIN_LIMITS as B } from "./limits.js"
import { exact, plain, PluginError, parsePackage, newerStableVersion, type Capability, type ViewPackage } from "./package.js"
import type { Installed } from "./store.js"

export interface OfficialRelease {
  id: string; version: string; url: string; sha256: string; bytes: number
  hostApi: number; capabilities: Capability[]; knownDigests: string[]
}
export interface OfficialCatalog { schemaVersion: 1; packages: OfficialRelease[] }
const digest = (v: unknown): v is string => typeof v === "string" && /^[a-f0-9]{64}$/.test(v)
const bad = () => { throw new PluginError("update_catalog_invalid") }
export function parseOfficialCatalog(value: unknown, allowed: readonly string[]): OfficialCatalog {
  if (!plain(value) || !exact(value, ["schemaVersion", "packages"]) || value.schemaVersion !== 1 ||
      !Array.isArray(value.packages) || value.packages.length > B.catalogEntries) return bad()
  const ids = new Set<string>(), packages: OfficialRelease[] = []
  for (const row of value.packages) {
    if (!plain(row) || !exact(row, ["id", "version", "url", "sha256", "bytes", "hostApi", "capabilities", "knownDigests"]) ||
        typeof row.id !== "string" || !/^com\.clawdline\.session-[a-z-]+$/.test(row.id) || ids.has(row.id) ||
        typeof row.version !== "string" || row.version.length > B.textCharacters || !newerStableVersion(row.version, "0.0.0-preview.0") ||
        row.url !== "/view-plugins/" + row.id.slice("com.clawdline.".length) + ".zip" || !digest(row.sha256) ||
        !Number.isSafeInteger(row.bytes) || (row.bytes as number) <= 0 || (row.bytes as number) > B.archiveBytes ||
        !Number.isSafeInteger(row.hostApi) || (row.hostApi as number) < 1 ||
        !Array.isArray(row.capabilities) || !row.capabilities.includes("sessions.read") ||
        new Set(row.capabilities).size !== row.capabilities.length || row.capabilities.some(c => c !== "sessions.read" && c !== "sessions.open") ||
        !Array.isArray(row.knownDigests) || !row.knownDigests.every(digest) || !row.knownDigests.includes(row.sha256)) return bad()
    ids.add(row.id); if(allowed.includes(row.id))packages.push(row as unknown as OfficialRelease)
  }
  return { schemaVersion: 1, packages }
}

/** Same-origin only, no redirects, bounded bytes and the existing request deadline. */
export async function updateBytes(url: string, maximum: number, signal: AbortSignal): Promise<ArrayBuffer> {
  const control = new AbortController(), abort = () => control.abort(signal.reason)
  if (signal.aborted) abort()
  signal.addEventListener("abort", abort, { once: true })
  const timer = setTimeout(() => control.abort(), B.updateMilliseconds)
  try {
    const response = await fetch(url, { credentials: "same-origin", cache: "no-store", redirect: "error", signal: control.signal })
    if (!response.ok || !response.body) throw new PluginError("package_download_failed")
    const reader = response.body.getReader(), chunks: Uint8Array[] = []; let bytes = 0
    try { while (true) { const part = await reader.read(); if (part.done) break
      bytes += part.value.byteLength; if (bytes > maximum) throw new PluginError("package_too_large"); chunks.push(part.value)
    } } finally { await reader.cancel() }
    const result = new Uint8Array(bytes); let at = 0
    for (const chunk of chunks) { result.set(chunk, at); at += chunk.byteLength }
    return result.buffer
  } finally { clearTimeout(timer); signal.removeEventListener("abort", abort) }
}
export async function readOfficialCatalog(allowed: readonly string[], signal: AbortSignal): Promise<OfficialCatalog> {
  const bytes = await updateBytes("/view-plugins/catalog.json", B.catalogBytes, signal)
  try { return parseOfficialCatalog(JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes)), allowed) }
  catch (error) { if (error instanceof PluginError) throw error; return bad() }
}
export function isOfficialRevision(row: Installed, release: OfficialRelease): boolean {
  return row.id === release.id && release.knownDigests.includes(row.active.digest)
}
export function automaticUpdateAllowed(row: Installed, release: OfficialRelease): boolean {
  return !!row.official && !!row.autoUpdate && release.hostApi === 1 && newerStableVersion(release.version, row.active.manifest.version) &&
    release.capabilities.every(capability => row.active.manifest.capabilities.includes(capability))
}
export async function downloadOfficialRelease(release: OfficialRelease, signal: AbortSignal): Promise<{ archive: ArrayBuffer; pkg: ViewPackage }> {
  if (release.hostApi !== 1) throw new PluginError("protocol_mismatch")
  const archive = await updateBytes(release.url, B.archiveBytes, signal), pkg = await parsePackage(archive)
  if (archive.byteLength !== release.bytes || pkg.digest !== release.sha256 || pkg.manifest.id !== release.id || pkg.manifest.version !== release.version ||
      pkg.manifest.hostApi !== release.hostApi || pkg.manifest.capabilities.length !== release.capabilities.length ||
      pkg.manifest.capabilities.some(capability => !release.capabilities.includes(capability))) throw new PluginError("update_package_mismatch")
  return { archive, pkg }
}

const runtimeLock = "clawdline-view-plugins-runtime-v1"
/** A shared lease spans a running view. Missing Web Locks disables silent application. */
export async function holdViewRuntime(signal: AbortSignal): Promise<() => void> {
  if (!navigator.locks) return () => {}
  return new Promise((resolve, reject) => {
    void navigator.locks.request(runtimeLock, { mode: "shared", signal }, async () => {
      await new Promise<void>(release => resolve(release))
    }).catch(reject)
  })
}
export async function whileViewsIdle(operation: () => Promise<void | boolean>): Promise<boolean> {
  if (!navigator.locks) return false
  return navigator.locks.request(runtimeLock, { ifAvailable: true }, async lock => {
    if (!lock) return false
    return (await operation()) !== false
  })
}
