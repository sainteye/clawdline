import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { ProjectAssistantRosterFrameTypeValues, ProjectAssistantErrorCodeValues } from "../../../contract/src/generated.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { failure } from "./client.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import { readSource, readBotMetadata } from "./read-views.ts"
import { READ_PLAINTEXT_BYTES_LIMIT } from "../legacy/js/net/read-compression.js"

const encoder = new TextEncoder()
const bytes = (s: string) => encoder.encode(s).byteLength
const object = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v)
const text = (v: unknown): v is string => typeof v === "string" && v.length > 0
const integer = (v: unknown, min = 0): v is number => Number.isSafeInteger(v) && Number(v) >= min
const cursor = (v: unknown): v is string => typeof v === "string" && bytes(v) <= 4096 && !/[\r\n\0]/.test(v)
const key = (v: unknown): v is W.ProjectAssistantBotKey => object(v) && text(v.machine_id) && text(v.bot_id)
const sameKey = (a: W.ProjectAssistantBotKey, b: W.ProjectAssistantBotKey) => a.machine_id === b.machine_id && a.bot_id === b.bot_id
const eventBytes = Math.max(...ProjectAssistantRosterFrameTypeValues.map(bytes))

/** A terminal reason is separate from its source error. None acknowledges a cursor. */
export class RosterStreamFailure extends Error {
  readonly contract_version = "project-assistants.v1"
  readonly error: W.ProjectAssistantFailure
  readonly kind: "invalid" | "capacity" | "eof" | "resync" | "closure"
  constructor(kind: RosterStreamFailure["kind"], detail?: W.ProjectAssistantFailure) {
    super(`roster_${kind}`)
    this.kind = kind
    this.error = detail ?? { ...failure(kind === "capacity" ? "pa_capacity_full" : kind === "eof" ? "pa_offline" : "pa_invalid").error, field: `roster_${kind}` }
  }
}

export function validateRosterRequest(value: W.ProjectAssistantRosterRequest): void {
  if (!object(value) || !text(value.machine_id) || value.project_id !== undefined && typeof value.project_id !== "string" || value.cursor !== undefined && !cursor(value.cursor)) throw failure("pa_invalid")
}

function validateFailure(value: unknown): asserts value is W.ProjectAssistantFailure {
  if (!object(value) || !ProjectAssistantErrorCodeValues.includes(value.code as W.ProjectAssistantErrorCode) || typeof value.field !== "string" || !integer(value.retry_after_seconds)) throw failure("pa_invalid")
}

function summary(value: unknown, change: W.ProjectAssistantBotKey, request: W.ProjectAssistantRosterRequest): void {
  if (!object(value) || !object(value.bot) || !key(value.bot.key) || !object(value.bot.project)) throw failure("pa_invalid")
  const v = value as unknown as W.ProjectAssistantBotSummary, b = v.bot, r = v.read_cursor
  if (!sameKey(b.key, change) || b.project.machine_id !== request.machine_id || request.project_id && b.project.project_id !== request.project_id) throw failure("pa_scope_denied")
  readBotMetadata(b, { key: change, project: b.project, conversation_id: b.conversation_id })
  if (b.hidden !== undefined && typeof b.hidden !== "boolean" || b.mode !== undefined && typeof b.mode !== "string" ||
    typeof v.latest_message !== "string" || typeof v.activity !== "string" || typeof v.activity_at !== "string" || !Number.isFinite(Date.parse(v.activity_at)) ||
    !integer(v.unread_count) || !integer(v.pending_permissions) || !integer(v.version, 1) || v.version !== b.version ||
    v.completed_turn_id !== undefined && typeof v.completed_turn_id !== "string" ||
    v.has_activity !== undefined && typeof v.has_activity !== "boolean" ||
    v.active_work !== undefined && !integer(v.active_work) ||
    v.status !== undefined && !["idle", "working", "needsInput", "error"].includes(v.status) ||
    !object(r) || !key(r.key) || !text(r.conversation_id) || !text(r.reader_id) || !integer(r.message_seq)) throw failure("pa_invalid")
  if (!sameKey(r.key, change) || r.conversation_id !== b.conversation_id) throw failure("pa_scope_denied")
  readSource(v.source, request.machine_id)
  if (v.source.state === "revoked") throw failure("pa_revoked")
  if (!v.source.fresh || v.source.state !== "online") throw failure("pa_offline")
  if (!v.source.readable || v.source.access === "hidden") throw failure("pa_scope_denied")
  const work = v.background_work
  if (work !== undefined && (!object(work) || !integer(work.working) || !integer(work.waiting) || !integer(work.needs_input))) {
    throw failure("pa_invalid")
  }
  const dispatched = v.dispatched_work
  if (dispatched !== undefined && (!object(dispatched) || !integer(dispatched.running) || !integer(dispatched.needs_you) || !integer(dispatched.delivered))) {
    throw failure("pa_invalid")
  }
  if (v.failure !== undefined) validateFailure(v.failure)
}

/** Validate the entire frame before returning anything, including for injected clients. */
export function validateRosterFrame(value: unknown, request: W.ProjectAssistantRosterRequest): W.ProjectAssistantRosterFrame {
  validateRosterRequest(request)
  let encoded: string | undefined
  try { encoded = JSON.stringify(value) } catch { throw failure("pa_invalid") }
  if (encoded !== undefined && bytes(encoded) > READ_PLAINTEXT_BYTES_LIMIT) throw new RosterStreamFailure("capacity")
  if (!object(value) || !ProjectAssistantRosterFrameTypeValues.includes(value.type as W.ProjectAssistantRosterFrameType) ||
    value.cursor !== undefined && !cursor(value.cursor) || value.source_epoch !== undefined && typeof value.source_epoch !== "string" ||
    value.changes !== undefined && !Array.isArray(value.changes)) throw failure("pa_invalid")
  const frame = value as unknown as W.ProjectAssistantRosterFrame, changes = frame.changes ?? []
  const control = frame.type === "resync" || frame.type === "closure", hello = frame.type === "hello"
  if (control || hello) {
    if (frame.cursor || changes.length || (control ? !!frame.source_epoch : !text(frame.source_epoch))) throw failure("pa_invalid")
    if (control) validateFailure(frame.failure)
    else if (frame.failure !== undefined) throw failure("pa_invalid")
  } else {
    if (!text(frame.cursor) || !text(frame.source_epoch) || frame.failure !== undefined || changes.length > (frame.type === "changes" ? 256 : 100)) throw failure("pa_invalid")
    for (const row of changes) {
      if (!object(row) || !key(row.key) || !integer(row.revision) || typeof row.removed !== "boolean" || !row.removed && row.summary === undefined) throw failure("pa_invalid")
      if (row.key.machine_id !== request.machine_id) throw failure("pa_scope_denied")
      // Replay can repeat a key/revision. Tombstones may carry an optional summary.
      if (row.summary !== undefined) summary(row.summary, row.key, request)
    }
  }
  return { ...frame, changes }
}

/** Incremental SSE with no inherited ID, replay history, or cursor ownership.
 * Only data/event/id values are retained. Unknown fields and comments are
 * discarded while reading, even if an untrusted peer never ends their line.
 * Data uses the existing 8 MiB plaintext budget; framing never steals from it.
 */
export class RosterSSEDecoder {
  private readonly utf8 = new TextDecoder("utf-8", { fatal: true })
  private prefix = ""
  private field: "data" | "event" | "id" | "skip" | null = null
  private value = ""
  private valueBytes = 0
  private firstValue = false
  private afterCR = false
  private data = ""
  private dataBytes = 0
  private hasData = false
  private event: string | undefined
  private id: string | undefined
  private readonly request: W.ProjectAssistantRosterRequest
  constructor(request: W.ProjectAssistantRosterRequest) { validateRosterRequest(request); this.request = { ...request } }

  push(chunk: Uint8Array, receive: (frame: W.ProjectAssistantRosterFrame) => void): void {
    let decoded: string
    try { decoded = this.utf8.decode(chunk, { stream: true }) } catch { throw new RosterStreamFailure("invalid") }
    this.consume(decoded, receive)
  }
  end(): never {
    try { this.utf8.decode() } catch { throw new RosterStreamFailure("invalid") }
    // Even EOF immediately after a complete frame is unexpected. It is not an acknowledgement.
    throw new RosterStreamFailure("eof")
  }
  private append(part: string) {
    if (this.field === "skip") return
    if (this.firstValue && part) { this.firstValue = false; if (part[0] === " ") part = part.slice(1) }
    this.valueBytes += bytes(part)
    const limit = this.field === "data" ? READ_PLAINTEXT_BYTES_LIMIT - this.dataBytes - (this.hasData ? 1 : 0) : this.field === "id" ? 4096 : eventBytes
    if (this.valueBytes > limit) throw new RosterStreamFailure("capacity")
    this.value += part
  }
  private consume(chunk: string, receive: (frame: W.ProjectAssistantRosterFrame) => void) {
    let at = 0
    while (at < chunk.length) {
      const c = chunk[at]
      if (this.afterCR) { this.afterCR = false; if (c === "\n") { at++; continue } }
      if (c === "\r" || c === "\n") {
        this.line(receive); this.afterCR = c === "\r"; at++; continue
      }
      if (this.field === null) {
        if (c === ":") { this.field = this.prefix === "data" || this.prefix === "event" || this.prefix === "id" ? this.prefix : "skip"; this.firstValue = true }
        else { this.prefix += c; if (this.prefix.length > "event".length) { this.prefix = ""; this.field = "skip" } }
        at++; continue
      }
      const stop = chunk.slice(at).search(/[\r\n]/), end = stop < 0 ? chunk.length : at + stop
      this.append(chunk.slice(at, end)); at = end
    }
  }
  private line(receive: (frame: W.ProjectAssistantRosterFrame) => void) {
    const blank = this.field === null && !this.prefix
    const field = this.field ?? this.prefix
    if (field === "data") {
      // SSE inserts one LF between data lines. Count it as plaintext too.
      this.dataBytes += this.valueBytes + (this.hasData ? 1 : 0)
      if (this.dataBytes > READ_PLAINTEXT_BYTES_LIMIT) throw new RosterStreamFailure("capacity")
      this.data += (this.hasData ? "\n" : "") + this.value; this.hasData = true
    } else if (field === "event" || field === "id") {
      if (this[field] !== undefined) throw new RosterStreamFailure("invalid")
      this[field] = this.value
    }
    this.prefix = ""; this.field = null; this.value = ""; this.valueBytes = 0; this.firstValue = false
    if (!blank) return
    if (!this.hasData) {
      if (this.event !== undefined || this.id !== undefined) throw new RosterStreamFailure("invalid")
      return
    }
    let value: unknown
    try { value = JSON.parse(this.data) } catch { throw new RosterStreamFailure("invalid") }
    const frame = validateRosterFrame(value, this.request)
    if (this.event !== frame.type || this.id !== (frame.cursor || undefined)) throw new RosterStreamFailure("invalid")
    this.data = ""; this.dataBytes = 0; this.hasData = false; this.event = undefined; this.id = undefined
    receive(frame)
    if (frame.type === "resync" || frame.type === "closure") throw new RosterStreamFailure(frame.type, frame.failure)
  }
}
