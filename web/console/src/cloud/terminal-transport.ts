import { base64Bytes, bytesBase64, envelopeSigningBytes } from "../legacy/js/net/cloud-crypto.js"
import type { TerminalFrame } from "@clawdline/contract"
import type { TerminalObservation } from "./terminal-observation.js"

export interface TerminalEnvelope {
  v: number; ch: string; seq: number; ts: number; class: string; key_id: string
  nonce: string; ct: string; sender: string; sig: string
}
type RelayEvent = { type: string; channels?: string[]; code?: string; error?: { code?: string }; status?: string; ch?: string; seq?: number }
export interface TerminalCloudClient {
  deviceID: string | null
  devicePrivateKey: CryptoKey | null
  ready: boolean
  retired: boolean
  nextSequence(device: string): Promise<number>
  socketSubscriptions: Map<string, number>
  subscriptionHolds: Map<string, number>
  pendingSubscriptions: Set<string>
  subscriptionLimit: number
  _trimSubscriptions(incoming: number, keep: string[]): void
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
/** A bad envelope, with the narrower reason only the diagnostic timeline reads; the tab still sees `terminal_bad_envelope`. */
function refused(diagnostic: string): Error { return Object.assign(fail("terminal_bad_envelope"), { diagnostic }) }
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
function route(kind: "termi" | "term" | "termr" | "termd", machine: string, viewer: string, connection?: string): string {
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
  private static receivers = new WeakMap<TerminalCloudClient, {
    original: TerminalCloudClient["_receiveEnvelope"]
    wrapper: TerminalCloudClient["_receiveEnvelope"]
    adapters: Set<TerminalChannelTransport>
  }>()
  private readonly viewer: string
  private readonly pairing: Promise<{ masterKey: CryptoKey; keyID: string; senderKey: CryptoKey; senderID: string }>
  private readonly stopEvents: () => void
  private readonly keys = new Map<string, { id: string; key: CryptoKey; lastSeq: { term: number; termr: number; termd: number };
    inFlight: Set<number>; nonces: Set<string> }>()
  private readonly confirmed = new Set<string>()
  private readonly deltaConfirmed = new Set<string>()
  private readonly deltaWaiting = new Map<string, () => void>()
  private readonly sent = new Set<number>()
  private readonly listeners = new Map<string, (event: TerminalChannelEvent) => void>()
  private readonly verifiedFrames = new Map<string, number>()
  private readonly waiting = new Map<string, { resolve: () => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }>()
  private readonly receiveTails = new Map<string, Promise<void>>()

  // Declared fields rather than parameter properties, so `node --test
  // --experimental-strip-types` can load this file and its suite runs.
  private readonly client: TerminalCloudClient
  private readonly machine: string
  private readonly observation?: TerminalObservation
  constructor(client: TerminalCloudClient, machine: string, observation?: TerminalObservation) {
    this.client = client
    this.machine = machine
    this.observation = observation
    if (!client.deviceID || !segment.test(client.deviceID)) throw fail("terminal_forbidden")
    this.viewer = client.deviceID
    this.pairing = client._outboundMachinePairing(machine)
    let receiver = TerminalChannelTransport.receivers.get(client)
    if (!receiver) {
      const original = client._receiveEnvelope
      const adapters = new Set<TerminalChannelTransport>()
      const wrapper: TerminalCloudClient["_receiveEnvelope"] = async (envelope, realign) => {
        if (envelope?.ch?.startsWith("term/") || envelope?.ch?.startsWith("termr/") || envelope?.ch?.startsWith("termd/")) {
          const owner = [...adapters].find((adapter) => adapter.receives(envelope)) ??
            [...adapters].find((adapter) => adapter.matchesMachine(envelope))
          if (owner) return owner.receive(envelope, realign)
        }
        return original.call(client, envelope, realign)
      }
      receiver = { original, wrapper, adapters }
      TerminalChannelTransport.receivers.set(client, receiver)
      client._receiveEnvelope = wrapper
    }
    receiver.adapters.add(this)
    this.stopEvents = client.events((event) => this.relay(event))
  }

  /** Waits for the relay's subscription confirmation before an unretained receipt can be sent. */
  async subscribeTerminal(connection: string, keyID: string, raw: Uint8Array, listener: (event: TerminalChannelEvent) => void): Promise<void> {
    if (!connectionID.test(connection) || !/^rk-[A-Za-z0-9_-]{22}$/.test(keyID) || raw.length !== 32) throw fail("terminal_invalid")
    if (!this.client.ready || this.client.retired) throw fail("cloud_reconnecting")
    const key = await crypto.subtle.importKey("raw", raw.slice().buffer as ArrayBuffer, "AES-GCM", false, ["decrypt"])
    this.keys.set(connection, { id: keyID, key, lastSeq: { term: -1, termr: -1, termd: -1 }, inFlight: new Set(), nonces: new Set() })
    this.listeners.set(connection, listener)
    const channels = this.channels(connection)
    const confirmation = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => { this.waiting.delete(connection); reject(fail("terminal_subscription_timeout")) }, 5_000)
      this.waiting.set(connection, { resolve, reject, timer })
    })
    // A synchronous subscribe refusal still runs the cleanup path below.
    // Observe that waiter's rejection even when we never reach `await`.
    void confirmation.catch(() => undefined)
    if (this.client.subscriptionHolds.size + channels.filter((ch) => !this.client.subscriptionHolds.has(ch)).length > this.client.subscriptionLimit) {
      clearTimeout(this.waiting.get(connection)!.timer)
      this.waiting.delete(connection)
      this.listeners.delete(connection)
      this.keys.delete(connection)
      throw fail("cloud_read_busy")
    }
    for (const ch of channels) this.client.subscriptionHolds.set(ch, (this.client.subscriptionHolds.get(ch) ?? 0) + 1)
    try {
      // Legacy subscribe() validates only its session channel types. The
      // terminal channels use the same socket capacity and idle eviction,
      // then send their own typed relay subscription frame.
      this.client._trimSubscriptions(channels.length, channels)
      if (this.client.socketSubscriptions.size + channels.length > this.client.subscriptionLimit) throw fail("cloud_read_busy")
      this.client._sendSubscriptionFrame("subscribe", channels)
      for (const ch of channels) {
        this.client.pendingSubscriptions.add(ch)
        this.client.socketSubscriptions.set(ch, Date.now())
      }
      await confirmation
      this.confirmed.add(connection)
      this.observation?.record("subscription_confirmed", { connection })
      if (this.tryDeltaSubscription(connection) && !this.deltaConfirmed.has(connection)) {
        await new Promise<void>((resolve) => {
          const timer = setTimeout(() => { this.deltaWaiting.delete(connection); resolve() }, 150)
          this.deltaWaiting.set(connection, () => { clearTimeout(timer); this.deltaWaiting.delete(connection); resolve() })
        })
      }
    } catch (error) {
      this.unsubscribeTerminal(connection)
      throw error
    }
  }

  unsubscribeTerminal(connection: string): void {
    const delta = route("termd", this.machine, this.viewer, connection)
    const channels = [...this.channels(connection), ...(this.client.subscriptionHolds.has(delta) ? [delta] : [])]
    const waiter = this.waiting.get(connection)
    if (waiter) { clearTimeout(waiter.timer); this.waiting.delete(connection); waiter.reject(fail("cloud_reconnecting")) }
    for (const ch of channels) {
      const n = (this.client.subscriptionHolds.get(ch) ?? 0) - 1
      if (n > 0) this.client.subscriptionHolds.set(ch, n)
      else this.client.subscriptionHolds.delete(ch)
      this.client.socketSubscriptions.delete(ch)
      this.client.pendingSubscriptions.delete(ch)
    }
    if (this.client.ready && !this.client.retired) this.client._sendSubscriptionFrame("unsubscribe", channels)
    this.listeners.delete(connection)
    this.keys.delete(connection)
    this.confirmed.delete(connection)
    this.deltaConfirmed.delete(connection)
    this.deltaWaiting.get(connection)?.()
    this.verifiedFrames.delete(connection)
    for (const channel of channels) this.receiveTails.delete(channel)
  }

  deltaAvailable(connection: string): boolean { return this.deltaConfirmed.has(connection) }

  private tryDeltaSubscription(connection: string): boolean {
    const ch = route("termd", this.machine, this.viewer, connection)
    if (this.client.subscriptionHolds.size >= this.client.subscriptionLimit ||
      this.client.socketSubscriptions.size >= this.client.subscriptionLimit) return false
    try {
      this.client.subscriptionHolds.set(ch, 1)
      this.client.pendingSubscriptions.add(ch)
      this.client.socketSubscriptions.set(ch, Date.now())
      this.client._sendSubscriptionFrame("subscribe", [ch])
      return true
    } catch {
      this.client.subscriptionHolds.delete(ch)
      this.client.pendingSubscriptions.delete(ch)
      this.client.socketSubscriptions.delete(ch)
      return false
    }
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
    this.observation?.record("request_sent", { connection: request.connection,
      requestID: request.request_id, operation: typeof request.operation === "string" ? request.operation : undefined })
    return { sender: this.viewer, seq } // Relay acceptance is not a machine receipt.
  }

  async openTerminalEnvelope(envelope: TerminalEnvelope, connection: string): Promise<unknown> {
    const state = this.keys.get(connection)
    if (!state || !this.listeners.has(connection)) throw fail("terminal_old_connection")
    const frame = route("term", this.machine, this.viewer, connection)
    const receipt = route("termr", this.machine, this.viewer, connection)
    const delta = route("termd", this.machine, this.viewer, connection)
    if (envelope.ch !== frame && envelope.ch !== receipt && envelope.ch !== delta) throw fail("terminal_wrong_viewer")
    if (envelope.ch === delta && !this.deltaConfirmed.has(connection)) throw fail("terminal_bad_envelope")
    const kind = envelope.ch === frame ? "term" : envelope.ch === receipt ? "termr" : "termd"
    if (Object.keys(envelope).length !== names.length || names.some((name) => !(name in envelope)) ||
      envelope.v !== 1 || envelope.sender !== this.machine ||
      envelope.class !== (envelope.ch === receipt ? "ctl" : "stream") ||
      !Number.isSafeInteger(envelope.seq) || !Number.isSafeInteger(envelope.ts) || base64Bytes(envelope.nonce).length !== 12) throw fail("terminal_bad_envelope")
    if (envelope.key_id !== state.id) throw refused("terminal_key_id_mismatch")
    if (envelope.seq <= state.lastSeq[kind] || state.inFlight.has(envelope.seq) || state.nonces.has(envelope.nonce)) throw refused("terminal_out_of_order")
    state.inFlight.add(envelope.seq)
    try {
      const pairing = await this.pairing
      if (pairing.senderID !== envelope.sender || !await crypto.subtle.verify("Ed25519", pairing.senderKey,
        base64Bytes(envelope.sig), envelopeSigningBytes(envelope))) throw fail("terminal_bad_signature")
      const clear = await crypto.subtle.decrypt({ name: "AES-GCM", iv: base64Bytes(envelope.nonce) }, state.key, base64Bytes(envelope.ct))
      const plaintext = JSON.parse(dec.decode(clear))
      if (this.keys.get(connection) !== state) throw refused("terminal_old_connection")
      if (envelope.seq <= state.lastSeq[kind] || state.nonces.has(envelope.nonce)) throw refused("terminal_out_of_order")
      state.lastSeq[kind] = envelope.seq
      state.nonces.add(envelope.nonce)
      if (state.nonces.size > 256) state.nonces.delete(state.nonces.values().next().value!)
      return plaintext
    } catch (error) {
      if ((error as Error).message === "terminal_bad_signature" || (error as Error).message === "terminal_bad_envelope") throw error
      throw fail("terminal_bad_key")
    } finally { state.inFlight.delete(envelope.seq) }
  }

  /** Releases relay fanout only after the terminal session accepted this verified full frame. */
  observeTerminalFrame(envelope: TerminalEnvelope): void {
    const connection = envelope.ch.split("/")[3]
    if (!this.client.ready || this.client.retired || !this.confirmed.has(connection) ||
      ![route("term", this.machine, this.viewer, connection), route("termd", this.machine, this.viewer, connection)].includes(envelope.ch) ||
      this.verifiedFrames.get(connection) !== envelope.seq) throw fail("terminal_bad_envelope")
    this.client._send({ type: "terminal_frame_observed", machine: this.machine, viewer: this.viewer,
      connection, envelope_seq: envelope.seq })
    this.observation?.record("frame_observed", { connection, channel: envelope.ch.startsWith("termd/") ? undefined : "term" })
    this.verifiedFrames.delete(connection)
  }

  private channels(connection: string): string[] { return [route("term", this.machine, this.viewer, connection), route("termr", this.machine, this.viewer, connection)] }
  private matchesMachine(envelope: TerminalEnvelope): boolean {
    const parts = envelope.ch.split("/")
    return parts.length === 4 && (parts[0] === "term" || parts[0] === "termr" || parts[0] === "termd") &&
      parts[1] === this.machine && parts[2] === this.viewer
  }
  private receives(envelope: TerminalEnvelope): boolean {
    const connection = envelope.ch.split("/")[3]
    return this.matchesMachine(envelope) && !!connection && this.listeners.has(connection)
  }
  private receive(envelope: TerminalEnvelope, realign: boolean): Promise<void> {
    // Relay delivery preserves order on one channel, but signature and AES work
    // is async. Serialize only that channel; a term frame cannot delay termr.
    const prior = this.receiveTails.get(envelope.ch) ?? Promise.resolve()
    const next = prior.then(() => this.receiveOne(envelope, realign))
    this.receiveTails.set(envelope.ch, next)
    void next.then(() => { if (this.receiveTails.get(envelope.ch) === next) this.receiveTails.delete(envelope.ch) })
    return next
  }
  private async receiveOne(envelope: TerminalEnvelope, realign: boolean): Promise<void> {
    const connection = envelope.ch.split("/")[3]
    const listener = this.listeners.get(connection)
    const channel = envelope.ch.startsWith("termr/") ? "termr" : envelope.ch.startsWith("termd/") ? undefined : "term"
    this.observation?.record("raw_received", { connection, channel })
    if (!listener) {
      this.observation?.record("envelope_rejected", { connection, channel, code: "terminal_old_connection" })
      return
    }
    try {
      const plaintext = await this.openTerminalEnvelope(envelope, connection)
      const value = plaintext && typeof plaintext === "object" ? plaintext as Record<string, unknown> : null
      if (value?.type === "terminal_receipt" || value?.type === "terminal_notice") {
        this.observation?.record("envelope_opened", { connection, channel,
          requestID: typeof value.request_id === "string" ? value.request_id : undefined,
          operation: typeof value.operation === "string" ? value.operation : undefined })
      } else if (value?.type === "terminal_frame" && !this.verifiedFrames.has(connection)) {
        this.observation?.record("envelope_opened", { connection, channel })
      }
      if ((envelope.ch === route("term", this.machine, this.viewer, connection) && isCompleteFrame(plaintext, connection)) ||
        (envelope.ch === route("termd", this.machine, this.viewer, connection) && value?.type === "terminal_frame_delta")) {
        this.verifiedFrames.set(connection, envelope.seq)
      }
      listener({ envelope, plaintext, realign })
    }
    catch (error) {
      const code = error instanceof Error ? (error as Error & { diagnostic?: string }).diagnostic ?? error.message : "terminal_bad_envelope"
      this.observation?.record("envelope_rejected", { connection, channel, code })
      // An unauthenticated or obsolete envelope cannot change this tab's lease state.
    }
  }
  private relay(event: RelayEvent): void {
    const code = event.code ?? event.error?.code ?? "relay_error"
    if (event.type === "error" && (code === "invalid_channel" || code === "unsupported_channel" || code === "cloud_read_busy" || code === "too_many_subscriptions")) {
      for (const connection of this.confirmed) {
        const delta = route("termd", this.machine, this.viewer, connection)
        if (this.client.pendingSubscriptions.has(delta) && (!event.ch || event.ch === delta)) {
          this.client.pendingSubscriptions.delete(delta)
          this.client.socketSubscriptions.delete(delta)
          this.client.subscriptionHolds.delete(delta)
          this.deltaWaiting.get(connection)?.()
          return
        }
      }
    }
    if (event.type === "error" && this.waiting.size) {
      for (const [connection, waiter] of this.waiting) {
        // The shared Cloud client retracts the channels belonging to a refused
        // subscribe before it emits this error. Match that evidence instead of
        // guessing from a short list of refusal codes: a budget refusal must
        // reach the caller as itself, not as a five-second timeout.
        if (code !== "forbidden" && code !== "terminal_forbidden" &&
          this.channels(connection).every((ch) => this.client.socketSubscriptions.has(ch))) continue
        clearTimeout(waiter.timer)
        this.waiting.delete(connection)
        waiter.reject(fail(code))
      }
      return
    }
    if (event.type === "subscriptions" && event.channels) {
      for (const connection of this.confirmed) {
        const delta = route("termd", this.machine, this.viewer, connection)
        if (event.channels.includes(delta)) {
          this.deltaConfirmed.add(connection); this.client.pendingSubscriptions.delete(delta)
          this.deltaWaiting.get(connection)?.()
        }
      }
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
      for (const listener of this.listeners.values()) listener({ error: code })
    }
    if (event.type === "ack" && ours && (event.status === "machine_offline" || event.status === "machine_stale")) {
      for (const listener of this.listeners.values()) listener({ error: event.status })
    }
  }
  dispose(): void {
    for (const connection of [...this.keys.keys()]) this.unsubscribeTerminal(connection)
    this.stopEvents()
    const receiver = TerminalChannelTransport.receivers.get(this.client)
    receiver?.adapters.delete(this)
    if (receiver && receiver.adapters.size === 0) {
      if (this.client._receiveEnvelope === receiver.wrapper) this.client._receiveEnvelope = receiver.original
      TerminalChannelTransport.receivers.delete(this.client)
    }
  }
}
