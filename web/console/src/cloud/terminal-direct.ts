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
  createDataChannel(label: string, init: { ordered: boolean }): DirectChannelLike
  createOffer(): Promise<{ type: string; sdp?: string }>
  setLocalDescription(description: { type: string; sdp?: string }): Promise<void>
  setRemoteDescription(description: { type: "answer"; sdp: string }): Promise<void>
  close(): void
  onicegatheringstatechange: ((event?: unknown) => void) | null
  onconnectionstatechange: ((event?: unknown) => void) | null
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

  constructor(factory: DirectPeerFactory, onEnvelope: (envelope: TerminalEnvelope) => void, onDown: (code: string) => void) {
    this.onEnvelope = onEnvelope
    this.onDown = onDown
    this.peer = factory({ iceServers: DIRECT_STUN.map((urls) => ({ urls })) })
    this.channel = this.peer.createDataChannel(DIRECT_LABEL, { ordered: true })
    this.reassembler = new DirectReassembler(() => this.close("terminal_direct_chunk_timeout"))
    this.channel.onopen = () => { if (this.closed) return; this.opened = true; this.opening?.resolve() }
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
      if (this.peer.connectionState === "failed" || this.peer.connectionState === "closed") this.close("terminal_direct_ice_failed")
    }
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
    return sdp
  }

  /** Applies the machine's answer and waits, at most DIRECT_NEGOTIATE_MS, for the DC to open. */
  async answer(sdp: string): Promise<void> {
    if (this.closed) throw fail("terminal_direct_closed")
    const open = new Promise<void>((resolve, reject) => { this.opening = { resolve, reject } })
    void open.catch(() => undefined)
    const timer = setTimeout(() => this.close("terminal_direct_ice_failed"), DIRECT_NEGOTIATE_MS)
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
    this.reassembler.reset()
    this.opening?.reject(fail(code))
    try { this.channel.close() } catch { /* already closed */ }
    try { this.peer.close() } catch { /* already closed */ }
    this.onDown(code)
  }
}
