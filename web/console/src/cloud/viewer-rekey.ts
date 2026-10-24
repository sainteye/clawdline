// @ts-expect-error Node executes TypeScript directly.
import { openRekeyAnswer, signRekeyOffer, REKEY_LIFETIME_MS_LIMIT } from "./rekey-crypto.ts"
import type { RekeyAuthority, RekeyOffer } from "./rekey-crypto.ts"

// One operation across the client, no retained envelopes, no automatic retries.
export const VIEWER_REKEY_PENDING_LIMIT = 1
export const VIEWER_REKEY_COOLDOWN_MS = 120_000
export interface RekeyPairing {
 machineID: string; senderID: string; keyID: string; epoch?: number; legacy?: boolean;
 masterKey: CryptoKey; senderKey: CryptoKey; revision?: string;
}
interface Pending {
 machine: string; pairing: RekeyPairing; expires: number; offer: string;
 ephemeral?: CryptoKey; timer?: ReturnType<typeof setTimeout>; adopting: boolean;
}
interface Options {
 account: string; viewer: string; signing: CryptoKey; viewerKey: Uint8Array;
 now(): number; send(raw: string): void;
 persist(pairing: RekeyPairing, state: {epoch: number; key_id: string; key: Uint8Array}, operation: string, valid: () => boolean): Promise<RekeyPairing>;
 adopted(pairing: RekeyPairing): void;
 changed?(machine: string): void;
 setTimeout?: typeof setTimeout; clearTimeout?: typeof clearTimeout;
}
const rejected = () => Object.assign(new Error("content key exchange rejected"), { code: "unknown_key" })
const b64 = (b: Uint8Array) => btoa(String.fromCharCode(...b))

/** Relay receipts carry routing information, never key or pairing authority. */
export function validRekeyControl(value: unknown): value is {type: string; frame: string; operation_id?: string; code?: string} {
 if (!value || typeof value !== "object" || Array.isArray(value)) return false
 const f = value as Record<string, unknown>
 const routed = f.type === "content_key_routed"
 if (!routed && f.type !== "content_key_error") return false
 if (f.frame !== "content_key_offer" && f.frame !== "content_key_answer") return false
 const fields = routed ? "type frame operation_id delivered" : "type frame code operation_id"
 if (Object.keys(f).some(key => !fields.split(" ").includes(key))) return false
 if (f.operation_id !== undefined && (typeof f.operation_id !== "string" || !/^[\x21-\x7e]{1,128}$/.test(f.operation_id) || /[<>&]/.test(f.operation_id))) return false
 if (routed) return typeof f.operation_id === "string" && Number.isSafeInteger(f.delivered) && (f.delivered as number) > 0
 return ["content_key_malformed", "content_key_forbidden", "content_key_no_target", "content_key_rate_limited", "content_key_unavailable"].includes(f.code as string)
}

/** The account and pins here come from boot/storage, never a relay header or roster. */
export class ViewerRekey {
 private pending: Pending | null = null
 private nextAt = 0
 private options: Options
 constructor(options: Options) { this.options = options }
 get pendingCount() { return this.pending ? VIEWER_REKEY_PENDING_LIMIT : 0 }
 isPending(machine: string) { return this.pending?.machine === machine }
 control(frame: unknown) {
  if (!validRekeyControl(frame)) throw Object.assign(new Error("content key exchange rejected"), {code:"bad_frame"})
  const p = this.pending
  if (frame.type !== "content_key_error" || frame.frame !== "content_key_offer" || !p?.offer ||
      this.options.now() >= p.expires || frame.operation_id !== JSON.parse(p.offer).operation_id) return
  this.cancel()
  throw Object.assign(new Error("content key exchange rejected"), {code:"cloud_reconnecting"})
 }
 private authority(machine: string): RekeyAuthority {
  return { account_id: this.options.account, machine_id: machine,
   viewer_device_id: this.options.viewer, activeViewerKey: this.options.viewerKey }
 }
 async request(pairing: RekeyPairing): Promise<boolean> {
  const o = this.options, now = o.now()
  if (this.pending || now < this.nextAt || pairing.legacy !== false) return false
  const floor = pairing.epoch ?? (pairing.keyID === "ms-1" ? 1 : 0)
  if (!Number.isSafeInteger(floor) || floor < 1 || !o.account || !o.viewer || o.signing.extractable !== false) return false
  const p: Pending = { machine: pairing.machineID, pairing, expires: now + REKEY_LIFETIME_MS_LIMIT,
   offer: "", adopting: false }
  this.pending = p
  this.nextAt = now + VIEWER_REKEY_COOLDOWN_MS
  p.timer = (o.setTimeout ?? setTimeout)(() => { if (this.pending === p) this.cancel() }, REKEY_LIFETIME_MS_LIMIT)
  try {
   const ephemeral = await crypto.subtle.generateKey("X25519", true, ["deriveBits"]) as CryptoKeyPair
   p.ephemeral = ephemeral.privateKey
   const offer: RekeyOffer = { type: "content_key_offer", v: 1, account_id: o.account,
    machine_id: p.machine, viewer_device_id: o.viewer,
    operation_id: crypto.randomUUID(), from_epoch: floor, from_key_id: pairing.keyID,
    viewer_ephemeral_key: b64(new Uint8Array(await crypto.subtle.exportKey("raw", ephemeral.publicKey))),
    nonce: b64(crypto.getRandomValues(new Uint8Array(32))), expires_at_ms: p.expires, signature: "" }
   p.offer = await signRekeyOffer(offer, o.signing, o.now())
   if (this.pending !== p || o.now() >= p.expires) return false
   o.send(p.offer)
   return true
  } catch (error) { if (this.pending === p) this.cancel(); throw error }
 }
 async answer(raw: string): Promise<boolean> {
  const p = this.pending, o = this.options
  if (!p || !p.offer || !p.ephemeral || p.adopting || o.now() >= p.expires) throw rejected()
  // Reserve before the first await: duplicate raw frames cannot race durable adoption.
  p.adopting = true
  try {
   const state = await openRekeyAnswer(raw, p.offer, this.authority(p.machine), p.ephemeral,
    p.pairing.senderKey, p.pairing.epoch ?? 1, p.pairing.keyID, o.now())
   const valid = () => this.pending === p && o.now() < p.expires
   if (!valid()) throw rejected()
   const adopted = await o.persist(p.pairing, state, JSON.parse(p.offer).operation_id, valid)
   if (!valid()) return false
   this.cancel()
   o.adopted(adopted)
   return true
  } catch (error) {
   if (this.pending === p) {
    p.adopting = false
    if (error && typeof error === "object" && "code" in error && error.code === "machine_pairing_changed") {
     this.cancel()
     o.changed?.(p.machine)
     throw Object.assign(new Error("content key exchange rejected"), {code:"cloud_reconnecting"})
    }
   }
   throw error
  }
 }
 cancel() {
  const p = this.pending
  this.pending = null
  if (p?.timer !== undefined) (this.options.clearTimeout ?? clearTimeout)(p.timer)
 }
}

