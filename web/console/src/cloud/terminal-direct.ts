import { bytesBase64 } from "../legacy/js/net/cloud-crypto.js"
import type { TerminalEnvelope } from "./terminal-transport.js"

/** The browser's numbers from "Direct carrier" in docs/cloud-terminal-wire.md. */
export const DIRECT_LABEL = "clawdline-terminal-v1"
export const DIRECT_STUN = ["stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"]
export const DIRECT_GATHER_MS = 3_000
/** CloudTerminalDirectNegotiateSecondsLimit: from the answer to an open DC. */
export const DIRECT_NEGOTIATE_MS = 5_000
/** CloudTerminalDirectSDPBytesLimit: the browser refuses a bigger offer before sealing it. */
export const DIRECT_SDP_BYTES = 16 * 1024
/** One `chunk` piece, in UTF-16 code units; an `env` text longer than this many bytes is chunked. */
export const DIRECT_CHUNK_UNITS = 16_384
/** CloudTerminalDirectMessageBytesLimit, per reassembled message. */
export const DIRECT_MESSAGE_BYTES = 9 * 1024 * 1024
/** CloudTerminalDirectChunkSecondsLimit, for a partial message. */
export const DIRECT_CHUNK_MS = 2_000
/** CloudTerminalDirectRetrySecondsLimit, before another upgrade of the same terminal. */
export const DIRECT_RETRY_MS = 30_000
/** CloudTerminalDirectBusyRetrySecondsLimit, after the relay refused the upgrade for its own load. */
export const DIRECT_BUSY_RETRY_MS = 5_000
/** Direct paths of one terminal that may fail in a row before it stays on the relay for DIRECT_PAUSE_MS. */
export const DIRECT_PAUSE_AFTER_FAILURES = 2
/** How long a terminal whose direct path keeps failing stays on the relay before it tries again. */
export const DIRECT_PAUSE_MS = 5 * 60_000
// A chunk's JSON may escape each code unit of `d` (a lone surrogate becomes `\udxxx`), plus its fields.
const CHUNK_TEXT_UNITS = DIRECT_CHUNK_UNITS * 6 + 256

const enc = new TextEncoder()
function fail(code: string): Error { return Object.assign(new Error(code), { code }) }
function utf8Length(text: string): number {
  let bytes = 0
  for (let i = 0; i < text.length; i++) {
    const unit = text.charCodeAt(i)
    if (unit < 0x80) bytes += 1
    else if (unit < 0x800) bytes += 2
    else if (unit >= 0xd800 && unit <= 0xdbff && i + 1 < text.length &&
      text.charCodeAt(i + 1) >= 0xdc00 && text.charCodeAt(i + 1) <= 0xdfff) { bytes += 4; i++ }
    else bytes += 3
  }
  return bytes
}

/** The offer SDP sealed under the connection key, readable only by the machine: base64(nonce ‖ AES-256-GCM). */
export async function sealDirectOffer(key: CryptoKey, connection: string, sdp: string): Promise<string> {
  const plain = enc.encode(sdp)
  if (plain.length > DIRECT_SDP_BYTES) throw fail("terminal_direct_sdp_too_large")
  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce,
    additionalData: enc.encode(`clawdline-direct-offer-v1/${connection}`) }, key, plain))
  const sealed = new Uint8Array(12 + ct.length)
  sealed.set(nonce); sealed.set(ct, 12)
  return bytesBase64(sealed)
}

/** The DC text messages for one envelope: one `env`, or contiguous ordered `chunk` pieces of its text. */
export function directMessages(envelope: TerminalEnvelope, id: number): string[] {
  const text = JSON.stringify({ t: "env", e: envelope })
  if (utf8Length(text) <= DIRECT_CHUNK_UNITS) return [text]
  if (utf8Length(text) > DIRECT_MESSAGE_BYTES) throw fail("terminal_direct_message_too_large")
  const n = Math.ceil(text.length / DIRECT_CHUNK_UNITS)
  return Array.from({ length: n }, (_, i) =>
    JSON.stringify({ t: "chunk", id, i, n, d: text.slice(i * DIRECT_CHUNK_UNITS, (i + 1) * DIRECT_CHUNK_UNITS) }))
}

/**
 * Rebuilds machine → browser `env` messages. Every violation of the DC message rules throws
 * `terminal_direct_violation`, and the owner closes the DC; a partial message still incomplete
 * after DIRECT_CHUNK_MS calls `onTimeout`.
 */
export class DirectReassembler {
  private partial: { id: number; n: number; next: number; parts: string[]; units: number; timer: ReturnType<typeof setTimeout> } | null = null
  private readonly onTimeout: () => void
  constructor(onTimeout: () => void) { this.onTimeout = onTimeout }

  push(data: unknown): TerminalEnvelope | null {
    // Bounds before JSON: code units never exceed UTF-8 bytes, so the cheap check runs first.
    if (typeof data !== "string" || data.length > DIRECT_MESSAGE_BYTES || utf8Length(data) > DIRECT_MESSAGE_BYTES) throw this.violation()
    // A piece is at most DIRECT_CHUNK_UNITS of `d`, so a chunk text past this is malformed.
    if (this.partial && data.length > CHUNK_TEXT_UNITS) throw this.violation()
    let message: { t?: unknown; e?: unknown; id?: unknown; i?: unknown; n?: unknown; d?: unknown }
    try { message = JSON.parse(data) } catch { throw this.violation() }
    if (!message || typeof message !== "object") throw this.violation()
    // Only the authenticated, offer-bound DTLS peer can supply this control.
    // It settles a read; it never installs keys or grants pairing authority.
    if (message.t === "read_error" && Object.keys(message).length === 2 &&
      (message as { code?: unknown }).code === "machine_pairing_required") {
      throw Object.assign(new Error("machine_pairing_required"), { code: "machine_pairing_required" })
    }
    if (message.t === "env") {
      if (this.partial) throw this.violation()
      return this.envelope(message)
    }
    if (message.t !== "chunk") throw this.violation()
    const { id, i, n, d } = message
    if (!Number.isSafeInteger(id) || (id as number) < 0 || !Number.isSafeInteger(i) || !Number.isSafeInteger(n) ||
      (n as number) < 1 || (i as number) >= (n as number) || typeof d !== "string" || d.length > DIRECT_CHUNK_UNITS) throw this.violation()
    if (!this.partial) {
      if (i !== 0) throw this.violation()
      this.partial = { id: id as number, n: n as number, next: 0, parts: [], units: 0,
        timer: setTimeout(() => { this.reset(); this.onTimeout() }, DIRECT_CHUNK_MS) }
    }
    const partial = this.partial
    if (id !== partial.id || n !== partial.n || i !== partial.next) throw this.violation()
    partial.units += d.length
    // Code units never exceed UTF-8 bytes, so this rejects an oversized message before it is joined.
    if (partial.units > DIRECT_MESSAGE_BYTES) throw this.violation()
    partial.parts.push(d)
    partial.next++
    if (partial.next < partial.n) return null
    const text = partial.parts.join("")
    this.reset()
    if (utf8Length(text) > DIRECT_MESSAGE_BYTES) throw this.violation()
    let whole: { t?: unknown; e?: unknown }
    try { whole = JSON.parse(text) } catch { throw this.violation() }
    if (!whole || whole.t !== "env") throw this.violation()
    return this.envelope(whole)
  }
  reset(): void {
    if (this.partial) clearTimeout(this.partial.timer)
    this.partial = null
  }
  private envelope(message: { e?: unknown }): TerminalEnvelope {
    const e = message.e as TerminalEnvelope | undefined
    if (!e || typeof e !== "object" || Array.isArray(e) || typeof e.ch !== "string") throw this.violation()
    return e
  }
  private violation(): Error { this.reset(); return fail("terminal_direct_violation") }
}

/**
 * The content-free trace of one direct attempt (docs/cloud-terminal-wire.md, Direct attempt trace).
 *
 * The machine writes its own line for the same attempt id (`internal/transport/cloud/direct_trace.go`);
 * this is the phone's half, sent as a `cloud.direct.attempt` viewer event so it lands beside it. What
 * is read from a candidate is its type and the family of its address, never the address, its port,
 * the SDP or its ICE credentials.
 */
export type DirectAttemptStage = "open" | "no_answer" | "connected_no_channel" | "no_remote_candidates" |
  "checks_failed" | "timed_out" | "closed_early"
export interface DirectAttemptTrace {
  /** The connection id the offer was made under; the machine keys its line by the same id. */
  attempt: string
  kind: "terminal" | "carrier"
  /** Records the finished summary. It is called once, and nothing it throws reaches the link. */
  report(data: Record<string, unknown>): void
}

const CANDIDATE_TYPES = new Set(["host", "srflx", "prflx", "relay"])

/** ipv4, ipv6, mdns (a `.local` name) or name: which kind of address, never which address. */
export function directAddressFamily(address: unknown): string {
  if (typeof address !== "string" || !address) return "unknown"
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(address)) return "ipv4"
  if (address.includes(":")) return "ipv6"
  if (address.toLowerCase().endsWith(".local")) return "mdns"
  return "name"
}

/** `type/family` counts of the candidate lines in an SDP. Nothing else of the SDP is kept. */
export function directCandidateCounts(sdp: string): Record<string, number> {
  const counts: Record<string, number> = {}
  for (const raw of sdp.split("\n")) {
    const line = raw.trim()
    if (!line.startsWith("a=candidate:")) continue
    const fields = line.slice("a=candidate:".length).split(/\s+/)
    const at = fields.indexOf("typ", 5)
    const type = at > 0 && CANDIDATE_TYPES.has(fields[at + 1]) ? fields[at + 1] : "unknown"
    const key = `${type}/${directAddressFamily(fields[4])}`
    counts[key] = (counts[key] ?? 0) + 1
  }
  return counts
}

/** Counts as `key:n,key:n` in key order, or `-`. */
export function directCountsText(counts: Record<string, number>): string {
  const keys = Object.keys(counts).sort()
  return keys.length ? keys.map((key) => `${key}:${counts[key]}`).join(",") : "-"
}

/**
 * Where an attempt stopped, by the machine's own order (`directTraceStage`): opening wins; then
 * whether an answer came; then what ICE reached; then why ICE had nothing to reach.
 */
export function directAttemptStage(facts: { opened: boolean; answered: boolean; remote: number;
  ice: readonly string[]; timedOut: boolean }): DirectAttemptStage {
  const reached = (...states: string[]) => facts.ice.some((state) => states.includes(state))
  if (facts.opened) return "open"
  if (!facts.answered) return "no_answer"
  if (reached("connected", "completed")) return "connected_no_channel"
  if (facts.remote === 0) return "no_remote_candidates"
  if (reached("failed")) return "checks_failed"
  if (facts.timedOut) return "timed_out"
  return "closed_early"
}

/** The parts of one `getStats` entry this trace reads. */
export interface DirectStatsEntry {
  id?: string; type?: string; state?: string; nominated?: boolean; selected?: boolean
  selectedCandidatePairId?: string; localCandidateId?: string; remoteCandidateId?: string
  candidateType?: string; address?: string; ip?: string
}

/**
 * The selected pair, as `local type/family>remote type/family`, from a `getStats` report: the pair
 * the transport names, else a nominated succeeded one. Its addresses are read for their family only.
 */
export function directSelectedPair(stats: Iterable<DirectStatsEntry>): string | null {
  const byID = new Map<string, DirectStatsEntry>()
  for (const entry of stats) if (entry && typeof entry.id === "string") byID.set(entry.id, entry)
  let pair: DirectStatsEntry | undefined
  for (const entry of byID.values()) {
    if (entry.type === "transport" && entry.selectedCandidatePairId) pair = byID.get(entry.selectedCandidatePairId)
  }
  if (!pair) {
    for (const entry of byID.values()) {
      if (entry.type === "candidate-pair" && (entry.selected || (entry.nominated && entry.state === "succeeded"))) { pair = entry; break }
    }
  }
  if (!pair) return null
  const side = (id?: string) => {
    const candidate = id ? byID.get(id) : undefined
    if (!candidate) return "unknown/unknown"
    const type = CANDIDATE_TYPES.has(candidate.candidateType ?? "") ? candidate.candidateType : "unknown"
    return `${type}/${directAddressFamily(candidate.address ?? candidate.ip)}`
  }
  return `${side(pair.localCandidateId)}>${side(pair.remoteCandidateId)}`
}

/**
 * The recorder a client's failure log offers (`cloud-viewer-events.js`), as a trace's `report`, or
 * null when this client has none. The machine writes the row into its log beside its own line.
 */
export function directAttemptRecorder(client: unknown, machine: string): ((data: Record<string, unknown>) => void) | null {
  const log = (client as { viewerEvents?: { record?(event: string, data: Record<string, unknown>, key?: string): unknown } } | null)?.viewerEvents
  if (typeof log?.record !== "function") return null
  // The failure log is delivered to one paired machine, which need not be the one this attempt was
  // made to, so the row names it.
  return (data) => {
    log.record!("cloud.direct.attempt", { ...data, machine }, ["cloud.direct.attempt", data.kind, data.stage].join("|"))
  }
}

/** The parts of RTCDataChannel the browser carrier uses, so a test can stand in for it. */
export interface DirectChannelLike {
  readonly readyState: string
  send(data: string): void
  close(): void
  onopen: ((event?: unknown) => void) | null
  onclose: ((event?: unknown) => void) | null
  onerror: ((event?: unknown) => void) | null
  onmessage: ((event: { data: unknown }) => void) | null
}
/** The parts of RTCPeerConnection the browser carrier uses. */
export interface DirectPeerLike {
  readonly localDescription: { sdp: string } | null
  readonly iceGatheringState: string
  readonly connectionState?: string
  readonly iceConnectionState?: string
  createDataChannel(label: string, init: { ordered: boolean }): DirectChannelLike
  createOffer(): Promise<{ type: string; sdp?: string }>
  setLocalDescription(description: { type: string; sdp?: string }): Promise<void>
  setRemoteDescription(description: { type: "answer"; sdp: string }): Promise<void>
  close(): void
  onicegatheringstatechange: ((event?: unknown) => void) | null
  onconnectionstatechange: ((event?: unknown) => void) | null
  oniceconnectionstatechange?: ((event?: unknown) => void) | null
  /** RTCStatsReport is map-like; a stand-in may leave this out, and the trace names no pair. */
  getStats?(): Promise<{ forEach(callback: (entry: DirectStatsEntry) => void): void }>
}
export type DirectPeerFactory = (config: { iceServers: Array<{ urls: string }> }) => DirectPeerLike

/** The browser's RTCPeerConnection, when this page has one. */
export function browserDirectPeers(): DirectPeerFactory | null {
  const Peer = (globalThis as { RTCPeerConnection?: new (config: unknown) => unknown }).RTCPeerConnection
  return Peer ? (config) => new Peer(config) as DirectPeerLike : null
}

/**
 * One browser ↔ machine peer and its data channel. It carries envelopes and acks, and calls
 * `onDown` once when the DC closes, fails, breaks a message rule or never opens.
 */
export class DirectLink {
  private readonly peer: DirectPeerLike
  private readonly channel: DirectChannelLike
  private readonly reassembler: DirectReassembler
  private readonly onEnvelope: (envelope: TerminalEnvelope) => void
  private readonly onDown: (code: string) => void
  private nextChunk = 0
  private opened = false
  private closed = false
  private opening: { resolve: () => void; reject: (error: Error) => void } | null = null
  private readonly trace: DirectAttemptTrace | null
  private readonly started = Date.now()
  private readonly states = { ice: [] as Array<[string, number]>, pc: [] as Array<[string, number]> }
  private local: Record<string, number> = {}
  private remote: Record<string, number> = {}
  private gather = "-"
  private answered = false
  private timedOut = false
  private traced = false

  constructor(factory: DirectPeerFactory, onEnvelope: (envelope: TerminalEnvelope) => void, onDown: (code: string) => void,
    trace: DirectAttemptTrace | null = null) {
    this.onEnvelope = onEnvelope
    this.onDown = onDown
    this.trace = trace
    this.peer = factory({ iceServers: DIRECT_STUN.map((urls) => ({ urls })) })
    this.channel = this.peer.createDataChannel(DIRECT_LABEL, { ordered: true })
    this.reassembler = new DirectReassembler(() => this.close("terminal_direct_chunk_timeout"))
    this.channel.onopen = () => {
      if (this.closed) return
      this.opened = true
      this.report("open", null)
      this.opening?.resolve()
    }
    this.channel.onclose = () => this.close("terminal_direct_closed")
    this.channel.onerror = () => this.close("terminal_direct_closed")
    this.channel.onmessage = (event) => {
      if (this.closed) return
      let envelope: TerminalEnvelope | null
      try { envelope = this.reassembler.push(event.data) }
      catch (error) { this.close((error as Error).message); return }
      if (envelope) this.onEnvelope(envelope)
    }
    this.peer.onconnectionstatechange = () => {
      this.state("pc", this.peer.connectionState)
      if (this.peer.connectionState === "failed" || this.peer.connectionState === "closed") this.close("terminal_direct_ice_failed")
    }
    this.peer.oniceconnectionstatechange = () => this.state("ice", this.peer.iceConnectionState)
  }

  /** Each state once, at the first time it was reached: the list is bounded by the enum. */
  private state(which: "ice" | "pc", state: string | undefined): void {
    if (!this.trace || this.traced || typeof state !== "string") return
    const list = this.states[which]
    if (!list.some(([seen]) => seen === state)) list.push([state, Date.now() - this.started])
  }

  /**
   * Sends this attempt's one summary. The stats are read before the peer is closed, because a
   * closed RTCPeerConnection refuses `getStats`; the summary is recorded when they answer.
   */
  private report(end: "open" | "closed", code: string | null): void {
    if (!this.trace || this.traced) return
    this.traced = true
    const trace = this.trace
    const timeline = (list: Array<[string, number]>) => list.length ? list.map(([state, ms]) => `${state}@${ms}ms`).join(",") : "-"
    const data: Record<string, unknown> = {
      attempt: trace.attempt, kind: trace.kind,
      stage: directAttemptStage({ opened: end === "open", answered: this.answered,
        remote: Object.values(this.remote).reduce((sum, n) => sum + n, 0),
        ice: this.states.ice.map(([state]) => state), timedOut: this.timedOut }),
      code, ms: Date.now() - this.started, answer: this.answered ? "received" : "none",
      offer_cands: directCountsText(this.local), gather: this.gather, answer_cands: directCountsText(this.remote),
      browser_srflx: Object.keys(this.local).some((key) => key.startsWith("srflx/")),
      ice: timeline(this.states.ice), pc: timeline(this.states.pc), pair: null as string | null,
    }
    const send = () => { try { trace.report(data) } catch { /* a recorder cannot fail the link it records */ } }
    let stats: Promise<{ forEach(callback: (entry: DirectStatsEntry) => void): void }> | undefined
    try { stats = this.peer.getStats?.() } catch { stats = undefined }
    if (!stats) { send(); return }
    stats.then((report) => {
      const entries: DirectStatsEntry[] = []
      report.forEach((entry) => entries.push(entry))
      data.pair = directSelectedPair(entries)
    }, () => undefined).finally(send)
  }

  get open(): boolean { return this.opened && !this.closed && this.channel.readyState === "open" }

  /** The offer SDP once ICE gathering completes, or what was gathered after DIRECT_GATHER_MS. */
  async offer(): Promise<string> {
    const offer = await this.peer.createOffer()
    await this.peer.setLocalDescription(offer)
    if (this.peer.iceGatheringState !== "complete") {
      await new Promise<void>((resolve) => {
        const timer = setTimeout(resolve, DIRECT_GATHER_MS)
        this.peer.onicegatheringstatechange = () => {
          if (this.peer.iceGatheringState === "complete") { clearTimeout(timer); resolve() }
        }
      })
    }
    if (this.closed) throw fail("terminal_direct_closed")
    const sdp = this.peer.localDescription?.sdp
    if (!sdp) throw fail("terminal_direct_no_offer")
    if (this.trace) {
      this.local = directCandidateCounts(sdp)
      this.gather = this.peer.iceGatheringState === "complete" ? "complete" : "timeout"
    }
    return sdp
  }

  /** Applies the machine's answer and waits, at most DIRECT_NEGOTIATE_MS, for the DC to open. */
  async answer(sdp: string): Promise<void> {
    if (this.closed) throw fail("terminal_direct_closed")
    const open = new Promise<void>((resolve, reject) => { this.opening = { resolve, reject } })
    void open.catch(() => undefined)
    const timer = setTimeout(() => { this.timedOut = true; this.close("terminal_direct_ice_failed") }, DIRECT_NEGOTIATE_MS)
    if (this.trace) { this.answered = true; this.remote = directCandidateCounts(sdp) }
    try {
      await this.peer.setRemoteDescription({ type: "answer", sdp })
      if (this.opened) return
      await open
    } catch (error) {
      this.close("terminal_direct_ice_failed")
      throw error
    } finally { clearTimeout(timer); this.opening = null }
  }

  send(envelope: TerminalEnvelope): void {
    if (!this.open) throw fail("terminal_direct_closed")
    const messages = directMessages(envelope, this.nextChunk)
    if (messages.length > 1) this.nextChunk++
    try { for (const message of messages) this.channel.send(message) }
    catch { this.close("terminal_direct_closed"); throw fail("terminal_direct_closed") }
  }

  /** The carrier acknowledgement of a verified, decrypted and drawn DC frame. */
  ack(connection: string, frameSeq: number): void {
    if (!this.open) return
    try { this.channel.send(JSON.stringify({ t: "ack", connection, frame_seq: frameSeq })) }
    catch { this.close("terminal_direct_closed") }
  }

  close(code = "terminal_direct_closed"): void {
    if (this.closed) return
    this.closed = true
    this.report("closed", code)
    this.reassembler.reset()
    this.opening?.reject(fail(code))
    try { this.channel.close() } catch { /* already closed */ }
    try { this.peer.close() } catch { /* already closed */ }
    this.onDown(code)
  }
}
