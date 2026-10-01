import { base64Bytes, bytesBase64, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"
import type { TerminalFrame } from "@clawdline/contract"

export interface TerminalEnvelope {
  v: number; ch: string; seq: number; ts: number; class: string; key_id: string
  nonce: string; ct: string; sender: string; sig: string
}
type RelayEvent = { type: string; channels?: string[]; code?: string; status?: string; ch?: string; seq?: number }
export interface TerminalCloudClient {
  deviceID: string | null
  devicePrivateKey: CryptoKey | null
  ready: boolean
  retired: boolean
  nextSequence(device: string): Promise<number>
  socketSubscriptions: Map<string, number>
  subscriptionHolds: Map<string, number>
  _outboundMachinePairing(machine: string): Promise<{ masterKey: CryptoKey; keyID: string; senderKey: CryptoKey; senderID: string }>
  _send(frame: unknown): void
  _sendSubscriptionFrame(type: string, channels: string[]): void
  _receiveEnvelope(envelope: TerminalEnvelope, realign: boolean): Promise<unknown>
  events(listener: (event: RelayEvent) => void): () => void
}
export type TerminalChannelEvent = { envelope: TerminalEnvelope; plaintext: unknown; realign: boolean } | { error: string }
const enc = new TextEncoder()
const dec = new TextDecoder("utf-8", { fatal: true })
const names = ["v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct", "sender", "sig"]
const segment = /^[A-Za-z0-9_-]{1,128}$/
const connectionID = /^[A-Za-z0-9_-]{22}$/
function fail(code: string): Error { return Object.assign(new Error(code), { code }) }
export function completeTerminalFrame(frame: TerminalFrame | undefined): frame is TerminalFrame {
  return !!frame && typeof frame.rev === "string" && Number.isFinite(frame.at) &&
    Number.isSafeInteger(frame.cols) && frame.cols > 0 && Number.isSafeInteger(frame.rows) && frame.rows > 0 &&
    !!frame.cursor && Number.isSafeInteger(frame.cursor.x) && Number.isSafeInteger(frame.cursor.y) &&
    typeof frame.cursor.visible === "boolean" && !!frame.modes &&
    typeof frame.modes.app_cursor === "boolean" && typeof frame.modes.app_keypad === "boolean" &&
    typeof frame.modes.mouse_sgr === "boolean" && typeof frame.modes.alt === "boolean" &&
    ["none", "standard", "button", "any"].includes(frame.modes.mouse) &&
    Array.isArray(frame.lines) && frame.lines.length === frame.rows && frame.lines.every((line) => typeof line === "string")
}
function isCompleteFrame(value: unknown, connection: string): boolean {
  if (!value || typeof value !== "object") return false
  const frame = value as { v?: unknown; type?: unknown; connection?: unknown; terminal_id?: unknown;
    frame_seq?: unknown; captured_at?: unknown; frame?: TerminalFrame }
  return frame.v === 1 && frame.type === "terminal_frame" && frame.connection === connection &&
    typeof frame.terminal_id === "string" && Number.isSafeInteger(frame.frame_seq) &&
    typeof frame.captured_at === "number" && Number.isFinite(frame.captured_at) && completeTerminalFrame(frame.frame)
}
function route(kind: "termi" | "term" | "termr", machine: string, viewer: string, connection?: string): string {
  if (!segment.test(machine) || !segment.test(viewer) || (connection && !connectionID.test(connection))) throw fail("terminal_invalid")
  const value = `${kind}/${machine}/${viewer}${connection ? "/" + connection : ""}`
  if (value.length > 300) throw fail("terminal_invalid")
  return value
}
function random22(): string { return bytesBase64(crypto.getRandomValues(new Uint8Array(16))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") }
export function freshTerminalConnection(): { connection: string; keyID: string; key: Uint8Array } {
  const connection = random22()
  return { connection, keyID: "rk-" + random22(), key: crypto.getRandomValues(new Uint8Array(32)) }
}

/** Adds only terminal channels to the existing authenticated socket, leaving the copied client intact. */
export class TerminalChannelTransport {
  private readonly viewer: string
  private readonly pairing: Promise<{ masterKey: CryptoKey; keyID: string; senderKey: CryptoKey; senderID: string }>
  private readonly oldReceive: TerminalCloudClient["_receiveEnvelope"]
  private readonly stopEvents: () => void
  private readonly keys = new Map<string, { id: string; key: CryptoKey; lastSeq: number; nonces: Set<string> }>()
  private readonly confirmed = new Set<string>()
  private readonly sent = new Set<number>()
  private readonly listeners = new Map<string, (event: TerminalChannelEvent) => void>()
  private readonly verifiedFrames = new Map<string, number>()
  private readonly waiting = new Map<string, { resolve: () => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }>()

  constructor(private readonly client: TerminalCloudClient, private readonly machine: string) {
    if (!client.deviceID || !segment.test(client.deviceID)) throw fail("terminal_forbidden")
    this.viewer = client.deviceID
    this.pairing = client._outboundMachinePairing(machine)
    this.oldReceive = client._receiveEnvelope.bind(client)
    client._receiveEnvelope = async (envelope, realign) => {
      if (envelope?.ch?.startsWith("term/" ) || envelope?.ch?.startsWith("termr/")) {
        await this.receive(envelope, realign)
        return
      }
      return this.oldReceive(envelope, realign)
    }
    this.stopEvents = client.events((event) => this.relay(event))
  }

  /** Waits for the relay's subscription confirmation before an unretained receipt can be sent. */
  async subscribeTerminal(connection: string, keyID: string, raw: Uint8Array, listener: (event: TerminalChannelEvent) => void): Promise<void> {
    if (!connectionID.test(connection) || !/^rk-[A-Za-z0-9_-]{22}$/.test(keyID) || raw.length !== 32) throw fail("terminal_invalid")
    if (!this.client.ready || this.client.retired) throw fail("cloud_reconnecting")
    const key = await crypto.subtle.importKey("raw", raw.slice().buffer as ArrayBuffer, "AES-GCM", false, ["decrypt"])
    this.keys.set(connection, { id: keyID, key, lastSeq: -1, nonces: new Set() })
    this.listeners.set(connection, listener)
    const channels = this.channels(connection)
    const confirmation = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => { this.waiting.delete(connection); reject(fail("terminal_subscription_timeout")) }, 5_000)
      this.waiting.set(connection, { resolve, reject, timer })
    })
    for (const ch of channels) this.client.subscriptionHolds.set(ch, (this.client.subscriptionHolds.get(ch) ?? 0) + 1)
    try {
      this.client._sendSubscriptionFrame("subscribe", channels)
      for (const ch of channels) this.client.socketSubscriptions.set(ch, Date.now())
      await confirmation
      this.confirmed.add(connection)
    } catch (error) {
      this.unsubscribeTerminal(connection)
      throw error
    }
  }

  unsubscribeTerminal(connection: string): void {
    const channels = this.channels(connection)
    const waiter = this.waiting.get(connection)
    if (waiter) { clearTimeout(waiter.timer); this.waiting.delete(connection); waiter.reject(fail("cloud_reconnecting")) }
    for (const ch of channels) {
      const n = (this.client.subscriptionHolds.get(ch) ?? 0) - 1
      if (n > 0) this.client.subscriptionHolds.set(ch, n)
      else this.client.subscriptionHolds.delete(ch)
      this.client.socketSubscriptions.delete(ch)
    }
    if (this.client.ready && !this.client.retired) this.client._sendSubscriptionFrame("unsubscribe", channels)
    this.listeners.delete(connection)
    this.keys.delete(connection)
    this.confirmed.delete(connection)
    this.verifiedFrames.delete(connection)
  }

  async publishTerminal(request: Record<string, unknown>): Promise<{ sender: string; seq: number }> {
    if (!this.client.ready || this.client.retired) throw fail("cloud_reconnecting")
    if (!this.client.devicePrivateKey || !this.client.deviceID) throw fail("missing_device_key")
    if (request.v !== 1 || request.type !== "terminal_request" || typeof request.request_id !== "string" ||
      typeof request.connection !== "string" || !this.confirmed.has(request.connection)) throw fail("terminal_invalid")
    const pairing = await this.pairing
    const seq = await this.client.nextSequence(this.viewer)
    if (!Number.isSafeInteger(seq) || seq < 0) throw fail("bad_sequence")
    const nonce = crypto.getRandomValues(new Uint8Array(12))
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, pairing.masterKey, enc.encode(JSON.stringify(request)))
    const envelope: TerminalEnvelope = { v: 1, ch: route("termi", this.machine, this.viewer), seq, ts: Date.now(), class: "ctl",
      key_id: pairing.keyID, nonce: bytesBase64(nonce), ct: bytesBase64(new Uint8Array(ct)), sender: this.viewer, sig: "" }
    envelope.sig = bytesBase64(new Uint8Array(await crypto.subtle.sign("Ed25519", this.client.devicePrivateKey, envelopeSigningBytes(envelope))))
    this.sent.add(seq)
    try { this.client._send({ type: "publish", envelope }) }
    catch (error) { this.sent.delete(seq); throw error }
    return { sender: this.viewer, seq } // Relay acceptance is not a machine receipt.
  }

  async openTerminalEnvelope(envelope: TerminalEnvelope, connection: string): Promise<unknown> {
    const state = this.keys.get(connection)
    if (!state || !this.listeners.has(connection)) throw fail("terminal_old_connection")
    const frame = route("term", this.machine, this.viewer, connection)
    const receipt = route("termr", this.machine, this.viewer, connection)
    if (envelope.ch !== frame && envelope.ch !== receipt) throw fail("terminal_wrong_viewer")
    if (Object.keys(envelope).length !== names.length || names.some((name) => !(name in envelope)) ||
      envelope.v !== 1 || envelope.sender !== this.machine || envelope.key_id !== state.id ||
      envelope.class !== (envelope.ch === frame ? "stream" : "ctl") ||
      !Number.isSafeInteger(envelope.seq) || envelope.seq <= state.lastSeq ||
      !Number.isSafeInteger(envelope.ts) || base64Bytes(envelope.nonce).length !== 12 || state.nonces.has(envelope.nonce)) throw fail("terminal_bad_envelope")
    const pairing = await this.pairing
    if (pairing.senderID !== envelope.sender || !await crypto.subtle.verify("Ed25519", pairing.senderKey,
      base64Bytes(envelope.sig), envelopeSigningBytes(envelope))) throw fail("terminal_bad_signature")
    let plaintext: unknown
    try {
      const clear = await crypto.subtle.decrypt({ name: "AES-GCM", iv: base64Bytes(envelope.nonce) }, state.key, base64Bytes(envelope.ct))
      plaintext = JSON.parse(dec.decode(clear))
    } catch { throw fail("terminal_bad_key") }
    state.lastSeq = envelope.seq
    state.nonces.add(envelope.nonce)
    if (state.nonces.size > 256) state.nonces.delete(state.nonces.values().next().value!)
    return plaintext
  }

  /** Releases relay fanout only after the terminal session accepted this verified full frame. */
  observeTerminalFrame(envelope: TerminalEnvelope): void {
    const connection = envelope.ch.split("/")[3]
    if (!this.client.ready || this.client.retired || !this.confirmed.has(connection) ||
      envelope.ch !== route("term", this.machine, this.viewer, connection) ||
      this.verifiedFrames.get(connection) !== envelope.seq) throw fail("terminal_bad_envelope")
    this.client._send({ type: "terminal_frame_observed", machine: this.machine, viewer: this.viewer,
      connection, envelope_seq: envelope.seq })
    this.verifiedFrames.delete(connection)
  }

  private channels(connection: string): string[] { return [route("term", this.machine, this.viewer, connection), route("termr", this.machine, this.viewer, connection)] }
  private async receive(envelope: TerminalEnvelope, realign: boolean): Promise<void> {
    const connection = envelope.ch.split("/")[3]
    const listener = this.listeners.get(connection)
    if (!listener) return
    try {
      const plaintext = await this.openTerminalEnvelope(envelope, connection)
      if (envelope.ch === route("term", this.machine, this.viewer, connection) && isCompleteFrame(plaintext, connection)) {
        this.verifiedFrames.set(connection, envelope.seq)
      }
      listener({ envelope, plaintext, realign })
    }
    catch (e) { listener({ error: (e as { code?: string }).code ?? "terminal_bad_envelope" }) }
  }
  private relay(event: RelayEvent): void {
    if (event.type === "subscriptions" && event.channels) {
      for (const [connection, waiter] of this.waiting) {
        if (this.channels(connection).every((ch) => event.channels!.includes(ch))) {
          clearTimeout(waiter.timer); this.waiting.delete(connection); waiter.resolve()
        }
      }
    }
    const ours = event.ch === route("termi", this.machine, this.viewer) &&
      typeof event.seq === "number" && this.sent.has(event.seq)
    if (ours) this.sent.delete(event.seq!)
    if ((event.type === "publish_error" && ours) || event.type === "error") {
      for (const listener of this.listeners.values()) listener({ error: event.code ?? "relay_error" })
    }
    if (event.type === "ack" && ours && (event.status === "machine_offline" || event.status === "machine_stale")) {
      for (const listener of this.listeners.values()) listener({ error: event.status })
    }
  }
  dispose(): void {
    for (const connection of [...this.keys.keys()]) this.unsubscribeTerminal(connection)
    this.stopEvents()
    this.client._receiveEnvelope = this.oldReceive
  }
}
