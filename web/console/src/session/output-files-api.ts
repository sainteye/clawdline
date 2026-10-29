import { VIDEO_MAX_BYTES, VIDEO_CHUNK_BYTES } from "../legacy/js/net/document-links.js"

export interface OutputFile {
  id: string; name: string; path: string; absolute_path: string
  kind: "created" | "modified" | "published"; first_at: number; updated_at: number; updates: number
  bytes: number; state: "available" | "missing" | "too_large"; media_type: string; source: string; anchor: number
}
export interface OutputListing { files: OutputFile[]; more: boolean; partial: boolean; coverage: string }
const hash = /^[0-9a-f]{64}$/
const bad = () => Object.assign(new Error("Invalid Session output answer"), { code: "bad_payload" })
export function outputListing(raw: unknown): OutputListing {
  const value = raw as OutputListing
  if (!value || !Array.isArray(value.files) || typeof value.more !== "boolean" || typeof value.partial !== "boolean" || typeof value.coverage !== "string") throw bad()
  const seen = new Set<string>()
  for (const f of value.files) {
    if (!f || !hash.test(f.id) || seen.has(f.id) || !f.name || typeof f.path !== "string" || typeof f.absolute_path !== "string" ||
      !["created", "modified", "published"].includes(f.kind) || !["available", "missing", "too_large"].includes(f.state) ||
      ![f.bytes, f.first_at, f.updated_at, f.updates, f.anchor].every(Number.isSafeInteger) || f.bytes < 0 || f.updates < 1 ||
      typeof f.media_type !== "string" || typeof f.source !== "string") throw bad()
    seen.add(f.id)
  }
  return value
}
async function jsonRead(url: string, signal: AbortSignal): Promise<unknown> {
  const response = await fetch(url, { signal, cache: "no-store" })
  const body = await response.json().catch(() => { throw bad() })
  if (!response.ok) {
    const code = typeof body?.error === "string" ? body.error : body?.error?.code
    throw Object.assign(new Error("Session output read failed"), { code: typeof code === "string" ? code : "request_failed", status: response.status })
  }
  return body
}
export async function readOutputs(session: string, signal: AbortSignal): Promise<OutputListing> {
  return outputListing(await jsonRead("/v1/sessions/" + encodeURIComponent(session) + "/outputs", signal))
}
interface OutputChunk { id: string; name: string; media_type: string; total_bytes: number; offset: number; revision: string; data: string }
export function outputChunk(raw: unknown, file: OutputFile, offset: number, revision: string, thumbnail = false): { chunk: OutputChunk; bytes: Uint8Array } {
  const c = raw as OutputChunk
  if (!c || c.id !== file.id || c.name !== file.name || c.offset !== offset || !hash.test(c.revision) ||
    (revision && c.revision !== revision) || !Number.isSafeInteger(c.total_bytes) || c.total_bytes < 0 || c.total_bytes > VIDEO_MAX_BYTES ||
    offset > c.total_bytes || typeof c.data !== "string" || c.data.length > Math.ceil(VIDEO_CHUNK_BYTES / 3) * 4 ||
    (thumbnail ? c.media_type !== "image/jpeg" : c.media_type !== file.media_type)) throw bad()
  let bytes: Uint8Array
  try { bytes = Uint8Array.from(atob(c.data), char => char.charCodeAt(0)) } catch { throw bad() }
  if (bytes.length !== Math.min(VIDEO_CHUNK_BYTES, c.total_bytes - offset) || (thumbnail && c.total_bytes > VIDEO_CHUNK_BYTES)) throw bad()
  return { chunk: c, bytes }
}
export async function readOutput(session: string, file: OutputFile, signal: AbortSignal, progress: (percent: number) => void, thumbnail = false): Promise<Blob> {
  const parts: Uint8Array<ArrayBuffer>[] = []
  let offset = 0, revision = "", total = -1, media = file.media_type
  do {
    const query = new URLSearchParams({ offset: String(offset), revision, size: thumbnail ? "thumb" : "" })
    const raw = await jsonRead("/v1/sessions/" + encodeURIComponent(session) + "/outputs/" + file.id + "?" + query, signal)
    const { chunk, bytes } = outputChunk(raw, file, offset, revision, thumbnail)
    if (total !== -1 && chunk.total_bytes !== total) throw bad()
    total = chunk.total_bytes; revision = chunk.revision; media = chunk.media_type
    parts.push(new Uint8Array(bytes)); offset += bytes.length
    progress(total ? Math.round(offset / total * 100) : 100)
    if (signal.aborted) throw new DOMException("Aborted", "AbortError")
  } while (offset < total)
  return new Blob(parts, { type: media })
}
export function outputCategory(file: OutputFile): "images" | "documents" | "other" {
  if (file.media_type.startsWith("image/")) return "images"
  if (file.media_type.startsWith("text/")) return "documents"
  return "other"
}
export function filterOutputs(files: OutputFile[], kind: string, search: string): OutputFile[] {
  const query = search.trim().toLocaleLowerCase()
  return files.filter(file => (kind === "all" || outputCategory(file) === kind) && (!query || (file.name + "\n" + file.path).toLocaleLowerCase().includes(query)))
}

/** A failed system share still leaves a usable link on desktop and phone. */
export async function shareOutputLink(title: string, url: string, actions: {
  share?: (data: { title: string; url: string }) => Promise<void>
  copy: (text: string) => Promise<void>
}): Promise<"shared" | "copied" | "cancelled"> {
  if (actions.share) {
    try { await actions.share({ title, url }); return "shared" }
    catch (error) { if ((error as Error)?.name === "AbortError") return "cancelled" }
  }
  await actions.copy(url)
  return "copied"
}

/** A terminal coordinate alone must not keep another conversation's panel alive. */
export function outputSessionKey(row: { id: string; sessionId?: string; execution_generation?: string; machine?: string } | null): string {
  return JSON.stringify([row?.machine, row?.id, row?.sessionId, row?.execution_generation])
}
