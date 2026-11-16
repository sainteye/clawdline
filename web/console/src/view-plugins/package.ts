import { RETIRED_PACKAGES } from "./retired.js"
export { RETIRED_PACKAGES } from "./retired.js"
import { PLUGIN_LIMITS as B } from "./limits.js"

export type Capability = "sessions.read" | "sessions.open"
export interface Manifest {
  manifestVersion: 1; id: string; version: string; hostApi: 1
  name: Record<string, string>; entry: string; capabilities: Capability[]; cover?: string
}
export interface ViewPackage { manifest: Manifest; html: string; digest: string }
export function newerStableVersion(next: string, installed: string): boolean {
  if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(next)) return false
  const prior = installed.split("+")[0], core = prior.split("-")[0]
  if (!/^\d+\.\d+\.\d+$/.test(core)) return false
  const a = next.split(".").map(BigInt), b = core.split(".").map(BigInt)
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return a[i] > b[i]
  return prior.includes("-")
}
export class PluginError extends Error {
  constructor(readonly code: string) { super(code); this.name = "PluginError" }
}
export function fail(code: string): never { throw new PluginError(code) }
export function plain(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v) &&
    (Object.getPrototypeOf(v) === Object.prototype || Object.getPrototypeOf(v) === null)
}
export function exact(v: Record<string, unknown>, keys: string[]): boolean {
  return Object.keys(v).every(k => keys.includes(k))
}
const version = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$/
export const text = (v: unknown): v is string => typeof v === "string" && v.length > 0 && [...v].length <= B.textCharacters
export function validateManifest(raw: unknown, allowLegacy = false): Manifest {
  if (!plain(raw) || !exact(raw, ["manifestVersion", "id", "version", "hostApi", "name", "entry", "capabilities", "cover"])) fail("invalid_manifest")
  if (raw.manifestVersion !== 1 || raw.hostApi !== 1) fail("protocol_mismatch")
  if (!text(raw.id) || !/^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$/.test(raw.id) ||
      !text(raw.version) || !version.test(raw.version) || !text(raw.entry) || !safePath(raw.entry) || !raw.entry.endsWith(".html")) fail("invalid_manifest")
  if (RETIRED_PACKAGES.includes(raw.id)) fail("retired_package")
  if (!plain(raw.name) || !text(raw.name.en) || !text(raw.name["zh-Hant"]) || !Object.values(raw.name).every(text)) fail("invalid_manifest")
  if (!Array.isArray(raw.capabilities) || !raw.capabilities.includes("sessions.read") ||
      new Set(raw.capabilities).size !== raw.capabilities.length ||
      !raw.capabilities.every(c => c === "sessions.read" || c === "sessions.open")) fail("invalid_manifest")
  if (raw.cover === undefined && !allowLegacy) fail("missing_cover")
  if (raw.cover !== undefined && (typeof raw.cover !== "string" || !raw.cover.startsWith("data:image/png;base64,"))) fail("invalid_cover")
  return raw as unknown as Manifest
}
function safePath(name: string): boolean {
  return /^[A-Za-z0-9_.\/-]+$/.test(name) && !name.includes("\\") &&
    name.split("/").every(p => !!p && !p.startsWith("."))
}
function decode(bytes: Uint8Array): string {
  try { const value = new TextDecoder("utf-8", { fatal: true }).decode(bytes); if (value.includes("\0")) fail("invalid_package"); return value }
  catch { return fail("invalid_package") }
}
export function crc32(bytes: Uint8Array): number {
  let crc = 0xffffffff
  for (const b of bytes) { crc ^= b; for (let j = 0; j < 8; j++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0) }
  return (crc ^ 0xffffffff) >>> 0
}

/** Covers are bounded, square, non-interlaced RGBA8 PNGs, never remote resources.
 * Check the actual chunk CRCs and inflated scanlines rather than trusting MIME/IHDR alone. */
async function validateCover(cover: string): Promise<void> {
  const encoded = cover.slice("data:image/png;base64,".length)
  if (!encoded || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) fail("invalid_cover")
  let raw: string
  try { raw = atob(encoded); if (btoa(raw) !== encoded) fail("invalid_cover") } catch { fail("invalid_cover") }
  const png = Uint8Array.from(raw!, c => c.charCodeAt(0)), data = new DataView(png.buffer)
  if (png.length < 57 || ![137,80,78,71,13,10,26,10].every((n,i) => png[i] === n)) fail("invalid_cover")
  let at = 8, side = 0, ended = false, seenData = false
  const compressed: Uint8Array<ArrayBuffer>[] = []
  while (at < png.length) {
    if (ended || at + 12 > png.length) fail("invalid_cover")
    const size = data.getUint32(at), end = at + 12 + size
    if (end > png.length || crc32(png.subarray(at + 4, end - 4)) !== data.getUint32(end - 4)) fail("invalid_cover")
    const kind = String.fromCharCode(...png.subarray(at + 4, at + 8))
    if (at === 8) {
      if (kind !== "IHDR" || size !== 13) fail("invalid_cover")
      side = data.getUint32(at + 8)
      if (!side || side > B.coverSide || data.getUint32(at + 12) !== side || png[at + 16] !== 8 || png[at + 17] !== 6 ||
          png[at + 18] !== 0 || png[at + 19] !== 0 || png[at + 20] !== 0) fail("invalid_cover")
    } else if (kind === "IDAT") {
      seenData = true; compressed.push(png.slice(at + 8, end - 4))
    } else if (kind === "IEND") {
      if (size !== 0 || !seenData || end !== png.length) fail("invalid_cover")
      ended = true
    } else fail("invalid_cover") // No animation or ancillary metadata decompression.
    at = end
  }
  if (!ended) fail("invalid_cover")
  const stride = side * 4 + 1, expected = side * stride
  let total = 0
  try {
    // Keep validation in memory; WebKit's Blob stream can outlive a closing page.
    const input = new ReadableStream<BufferSource>({ start(controller) { for (const chunk of compressed) controller.enqueue(chunk); controller.close() } })
    const stream = input.pipeThrough(new DecompressionStream("deflate")), reader = stream.getReader()
    try {
      while (true) {
        const part = await reader.read(); if (part.done) break
        if (total + part.value.length > expected) fail("invalid_cover")
        for (let n = (stride - total % stride) % stride; n < part.value.length; n += stride) if (part.value[n] > 4) fail("invalid_cover")
        total += part.value.length
      }
    } finally { await reader.cancel() }
    if (total !== expected) fail("invalid_cover")
  } catch { fail("invalid_cover") }
}

/** The first profile is a portable two-file ZIP STORE, generated by the public packer.
 * Central/local headers, lengths and CRCs are checked before any script can run. */
export async function parsePackage(input: ArrayBuffer | Uint8Array, { allowLegacy = false } = {}): Promise<ViewPackage> {
  const bytes = input instanceof Uint8Array ? new Uint8Array(input) : new Uint8Array(input.slice(0))
  if (bytes.length > B.archiveBytes) fail("package_too_large")
  if (bytes.length < 22) fail("invalid_package")
  const data = new DataView(bytes.buffer)
  // This profile has no ZIP comment, data descriptors, encryption, ZIP64 or extra assets.
  const end = bytes.length - 22
  if (data.getUint32(end, true) !== 0x06054b50 || data.getUint16(end + 20, true) !== 0 ||
      data.getUint16(end + 4, true) !== 0 || data.getUint16(end + 6, true) !== 0 ||
      data.getUint16(end + 8, true) !== B.files || data.getUint16(end + 10, true) !== B.files) fail("invalid_package")
  let at = data.getUint32(end + 16, true)
  const centralStart = at, centralEnd = at + data.getUint32(end + 12, true)
  if (centralEnd !== end || at >= end) fail("invalid_package")
  const files = new Map<string, Uint8Array>(), ranges: [number, number][] = []
  let expanded = 0
  for (let i = 0; i < B.files; i++) {
    if (at + 46 > centralEnd || data.getUint32(at, true) !== 0x02014b50) fail("invalid_package")
    const flags = data.getUint16(at + 8, true), method = data.getUint16(at + 10, true)
    if (flags & ~0x800 || method !== 0) fail("unsupported_zip")
    const compressed = data.getUint32(at + 20, true), size = data.getUint32(at + 24, true)
    const nameSize = data.getUint16(at + 28, true), extra = data.getUint16(at + 30, true), comment = data.getUint16(at + 32, true)
    const next = at + 46 + nameSize + extra + comment, offset = data.getUint32(at + 42, true)
    if (next > centralEnd || data.getUint16(at + 34, true) !== 0 ||
        ((data.getUint32(at + 38, true) >>> 16) & 0xf000) === 0xa000) fail("invalid_package")
    const name = decode(bytes.subarray(at + 46, at + 46 + nameSize))
    if (!safePath(name) || files.has(name) || compressed !== size || (expanded += size) > B.expandedBytes) fail("invalid_package")
    if (offset + 30 > centralStart || data.getUint32(offset, true) !== 0x04034b50 ||
        data.getUint16(offset + 6, true) !== flags || data.getUint16(offset + 8, true) !== method ||
        data.getUint32(offset + 18, true) !== compressed || data.getUint32(offset + 22, true) !== size ||
        data.getUint32(offset + 14, true) !== data.getUint32(at + 16, true)) fail("invalid_package")
    const localNameSize = data.getUint16(offset + 26, true), localExtra = data.getUint16(offset + 28, true)
    const begin = offset + 30 + localNameSize + localExtra, finish = begin + size
    if (finish > centralStart || begin > centralStart ||
        decode(bytes.subarray(offset + 30, offset + 30 + localNameSize)) !== name ||
        ranges.some(([lo, hi]) => offset < hi && finish > lo)) fail("invalid_package")
    const content = bytes.subarray(begin, finish)
    if (crc32(content) !== data.getUint32(at + 16, true)) fail("invalid_package")
    ranges.push([offset, finish]); files.set(name, content); at = next
  }
  if (at !== centralEnd) fail("invalid_package")
  const manifestFile = files.get("manifest.json")
  if (!manifestFile || manifestFile.length > B.manifestBytes) fail("invalid_manifest")
  let raw: unknown
  try { raw = JSON.parse(decode(manifestFile)) } catch { fail("invalid_manifest") }
  const manifest = validateManifest(raw, allowLegacy), entry = files.get(manifest.entry)
  if (!entry || manifest.entry === "manifest.json") fail("missing_entry")
  if (manifest.cover !== undefined) await validateCover(manifest.cover)
  const digest = [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))].map(b => b.toString(16).padStart(2, "0")).join("")
  return { manifest, html: decode(entry), digest }
}
