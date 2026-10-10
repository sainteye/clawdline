// The one direct carrier a page has to a machine (docs/cloud-terminal-wire.md).
//
// A terminal upgraded to a WebRTC data channel first, and the channel belonged
// to the terminal. The machine allows **one** peer per viewer, so a Session
// read that wanted the same short path could not negotiate its own: it had to
// be the same channel. This module is where that channel now lives, so that
// either side may open it and both use it:
//
//   - A Session read opens it by itself, with a `carrier_offer`, and needs no
//     terminal at all — which is the whole point, because a phone reading a
//     transcript has no terminal open.
//   - A terminal that upgrades while the carrier is already open **borrows**
//     it: the machine's `rekey_connection` asks only that this viewer have an
//     open peer, so no second negotiation happens.
//
// Nothing here decides what may be read. The carrier moves bytes; every read
// that travels on it is authorized by the machine one read at a time, exactly
// as a read off the relay is.
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load this file in its suite.
import { DirectLink, DIRECT_BUSY_RETRY_MS, DIRECT_RETRY_MS, browserDirectPeers, type DirectPeerFactory } from "./terminal-direct.ts"
import type { SharedCarrier, TerminalEnvelope } from "./terminal-transport.js"
import { base64Bytes, bytesBase64 } from "../legacy/js/net/cloud-crypto.js"

const enc = new TextEncoder()
const dec = new TextDecoder("utf-8", { fatal: true })
function fail(code: string): Error { return Object.assign(new Error(code), { code }) }

/** The machine reply session every machine-scoped read answer names. */
export const MACHINE_REPLY_SESSION = "__clawdline_machine__"

/** What a page sends to open a carrier, and what the machine sends back. */
export interface CarrierOffer {
  request: string
  connection: string
  keyID: string
  key: Uint8Array
  sdp: string
}
export interface CarrierAnswer {
  /** The sealed answer SDP, base64 of nonce ‖ AES-256-GCM under the offer's key. */
  sdpSealed: string
  /** The machine offers to put a borrowing terminal's everyday receipts on the DC. */
  directReceipts: boolean
}

/** The little of the Cloud client a carrier uses. A test stands in for all of it. */
export interface CarrierClient {
  deviceID: string | null
  ready: boolean
  retired: boolean
  /** Publishes one signed `carrier_offer` and resolves with the machine's answer. */
  publishCarrierOffer(machine: string, offer: CarrierOffer): Promise<CarrierAnswer>
  /** Hands one envelope that arrived on the carrier to the read machinery, with the channel it
   *  arrived on: the machine's envelope sequence is that channel's own. */
  receiveCarrierEnvelope(machine: string, envelope: TerminalEnvelope, channelGeneration: number): void
  /** Whether this machine's own descriptor says it opens carriers. */
  carrierSupported(machine: string): boolean
}

function random22(): string {
  return bytesBase64(crypto.getRandomValues(new Uint8Array(16))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

/** The answer SDP, opened with the key this page minted for the offer it answers. */
export async function openCarrierAnswer(raw: Uint8Array, connection: string, sealed: string): Promise<string> {
  const bytes = base64Bytes(sealed)
  if (bytes.length < 12 + 16) throw fail("carrier_bad_answer")
  const key = await crypto.subtle.importKey("raw", raw.slice().buffer as ArrayBuffer, "AES-GCM", false, ["decrypt"])
  const clear = await crypto.subtle.decrypt({ name: "AES-GCM", iv: bytes.slice(0, 12),
    additionalData: enc.encode(`clawdline-direct-answer-v1/${connection}`) }, key, bytes.slice(12))
  return dec.decode(clear)
}

/** `term`/`termd`/`termr` go to the terminal; a read's answer goes to the Session reader. */
function carrierAudience(channel: string): "terminal" | "session" | null {
  if (channel.startsWith("term/") || channel.startsWith("termd/") || channel.startsWith("termr/")) return "terminal"
  if (channel.startsWith("t/")) return "session"
  return null
}

const registry = new WeakMap<CarrierClient, Map<string, DirectCarrier>>()

export class DirectCarrier implements SharedCarrier {
  private readonly client: CarrierClient
  private readonly machine: string
  private readonly peers: DirectPeerFactory | null
  private link: DirectLink | null = null
  /** Set while this carrier's own negotiation is in flight, so two reads make one offer. */
  private opening: Promise<boolean> | null = null
  /**
   * How many channels this carrier has had, counted from one.
   *
   * The machine numbers a carrier's envelopes per peer (`directPeer.carrierSeq`), so a channel
   * that replaced another starts again at one. Measured on 2026-10-11: a reader that kept one
   * sequence floor per machine read every answer on the second channel as a replay, dropped it,
   * and the read waiting for it timed out on a carrier that was open — which nothing falls back
   * from, because nothing had failed. The floor belongs to the channel, and this is its name.
   */
  private gen = 0
  private retryAt = 0
  private receipts = false
  private readonly listeners = new Map<"terminal" | "session", (envelope: TerminalEnvelope) => void>()
  private readonly down = new Set<(code: string) => void>()

  constructor(client: CarrierClient, machine: string, peers: DirectPeerFactory | null = browserDirectPeers()) {
    this.client = client
    this.machine = machine
    this.peers = peers
  }

  /** The one carrier for this client and machine. Both the terminal and the reader ask for it here. */
  static for(client: CarrierClient, machine: string, peers?: DirectPeerFactory | null): DirectCarrier {
    let byMachine = registry.get(client)
    if (!byMachine) { byMachine = new Map(); registry.set(client, byMachine) }
    let carrier = byMachine.get(machine)
    if (!carrier) {
      carrier = peers === undefined ? new DirectCarrier(client, machine) : new DirectCarrier(client, machine, peers)
      byMachine.set(machine, carrier)
    }
    return carrier
  }

  get open(): boolean { return !!this.link?.open }
  /** Which channel this carrier is on. A read answer's sequence is only a number within it. */
  get channelGeneration(): number { return this.gen }
  /** True when a borrowing terminal may ask for its everyday receipts on the DC. */
  get directReceipts(): boolean { return this.open && this.receipts }
  supported(): boolean { return !!this.peers && this.client.carrierSupported(this.machine) }

  /** Where envelopes of this audience go once the carrier delivers them. */
  listen(audience: "terminal" | "session", listener: (envelope: TerminalEnvelope) => void): () => void {
    this.listeners.set(audience, listener)
    return () => { if (this.listeners.get(audience) === listener) this.listeners.delete(audience) }
  }

  /** Called once each time the carrier goes, so a terminal can rekey back to the relay. */
  whenDown(listener: (code: string) => void): () => void {
    this.down.add(listener)
    return () => { this.down.delete(listener) }
  }

  /**
   * Opens the carrier, or reports that it could not be opened. It is idempotent and it is the
   * only path that negotiates for Session reads: two reads racing make one offer, and a machine
   * that refused one is not asked again until its retry bound has passed.
   */
  async ensure(): Promise<boolean> {
    if (this.open) return true
    if (this.opening) return this.opening
    if (!this.supported() || !this.client.ready || this.client.retired || Date.now() < this.retryAt) return false
    const attempt = this.negotiate()
    this.opening = attempt
    try { return await attempt }
    finally { if (this.opening === attempt) this.opening = null }
  }

  private async negotiate(): Promise<boolean> {
    const connection = random22()
    const keyID = "rk-" + random22()
    const key = crypto.getRandomValues(new Uint8Array(32))
    const request = crypto.randomUUID()
    const link: DirectLink = new DirectLink(this.peers!, (envelope) => this.deliver(link, envelope),
      (code) => this.lost(link, code))
    try {
      const sdp = await link.offer()
      const answer = await this.client.publishCarrierOffer(this.machine, { request, connection, keyID, key, sdp })
      await link.answer(await openCarrierAnswer(key, connection, answer.sdpSealed))
      if (!link.open) throw fail("carrier_not_open")
      this.install(link, answer.directReceipts)
      return true
    } catch (error) {
      link.close("carrier_failed")
      // `terminal_busy` is the machine saying "not now" — its roster read failed, or its own
      // peers are full. Everything else is this network or this build: wait the longer bound.
      const code = (error as { code?: string })?.code ?? "carrier_failed"
      this.retryAt = Date.now() + (code === "terminal_busy" ? DIRECT_BUSY_RETRY_MS : DIRECT_RETRY_MS)
      return false
    }
  }

  /**
   * A terminal's own `direct_offer` made this link. It becomes the shared carrier, so a Session
   * read that follows takes the short path too and no second channel is ever negotiated.
   *
   * A link made elsewhere calls back to whoever made it, so its owner hands this carrier what
   * arrives on it (`receive`) and tells it when the link is gone (`dropped`).
   */
  adopt(link: DirectLink, directReceipts: boolean): void {
    if (this.link && this.link !== link) this.link.close("carrier_replaced")
    this.install(link, directReceipts)
  }

  /** One channel becomes this carrier's, and counts its envelopes from one. */
  private install(link: DirectLink, directReceipts: boolean): void {
    this.link = link
    this.receipts = directReceipts
    this.gen++
  }

  /**
   * The reader could not accept what this channel delivered, so the channel goes.
   *
   * An open carrier whose envelopes a page refuses answers no read ever again, and because
   * nothing failed nothing falls back: closing it is what tells the machine and what hands the
   * reads it is holding back to the relay. A number from a channel this carrier has already
   * replaced says nothing about the one in use, and closes nothing.
   */
  failed(channelGeneration: number, code: string): void {
    if (channelGeneration !== this.gen) return
    this.close(code)
  }

  /** One envelope an adopted link delivered, routed as this carrier's own would be. */
  receive(envelope: TerminalEnvelope): void { this.route(envelope) }

  /** An adopted link is gone: every read waiting on this carrier is told, once. */
  dropped(link: DirectLink, code: string): void { if (this.link === link) this.lost(link, code) }

  /** The link, for the terminal transport that still owns its own request path. */
  current(): DirectLink | null { return this.link?.open ? this.link : null }

  /** Writes one envelope to the carrier. It throws rather than fall back: the caller decides that. */
  send(envelope: TerminalEnvelope): void {
    const link = this.link
    if (!link?.open) throw fail("carrier_closed")
    link.send(envelope)
  }

  ack(connection: string, frameSeq: number): void { this.link?.ack(connection, frameSeq) }

  close(code = "carrier_closed"): void { this.link?.close(code) }

  private deliver(link: DirectLink, envelope: TerminalEnvelope): void {
    if (link !== this.link && this.link !== null) return
    this.route(envelope)
  }

  private route(envelope: TerminalEnvelope): void {
    const audience = typeof envelope?.ch === "string" ? carrierAudience(envelope.ch) : null
    if (!audience) return
    if (audience === "session") {
      this.client.receiveCarrierEnvelope(this.machine, envelope, this.gen)
      return
    }
    this.listeners.get("terminal")?.(envelope)
  }

  private lost(link: DirectLink, code: string): void {
    if (this.link === link) this.link = null
    this.receipts = false
    // A carrier this page lets go is closed, never only forgotten. The machine
    // keeps one carrier per viewer and has no idle bound to fall back on, so a
    // channel this page stops using while the other end still believes in it is
    // a page that can never open another (`supersedeCarrier`, which is the other
    // half of this). `close` is idempotent, so the usual order — the link closed,
    // which is why we are here — is unchanged.
    link.close(code)
    this.retryAt = Date.now() + (code === "terminal_busy" ? DIRECT_BUSY_RETRY_MS : DIRECT_RETRY_MS)
    for (const listener of [...this.down]) listener(code)
  }
}

/** The provider `StatusCloudClient` is given, so the copied reader needs no import of this file. */
export function directCarriers(client: CarrierClient, peers?: DirectPeerFactory | null) {
  return {
    for(machine: string): DirectCarrier | null {
      if (typeof machine !== "string" || !machine) return null
      return DirectCarrier.for(client, machine, peers)
    },
  }
}
