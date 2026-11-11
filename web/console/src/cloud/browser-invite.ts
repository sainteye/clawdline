// The invited browser's half of a browser invitation (docs/browser-invite.md).
// Its Go twin is internal/domain/cloud/browser_invite.go; both are held to
// internal/domain/cloud/testdata/browser-invite-v1.json.
//
// Order: parse the typed code, make a non-extractable Ed25519 device key, run
// CPace as initiator through Cloud, seal the confirmation, show the six
// digits, wait for the owner to approve on their own device, open the
// handover, then claim the invited session with a signature. Cloud sees two
// CPace messages and two AES-GCM blobs; the secret never leaves this page.

// @ts-expect-error Node's test runner needs the source extension.
import { CPaceParty, lvCat } from "./cpace.ts"

export const INVITE_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
export const INVITE_ID_CHARS = 5
export const INVITE_SECRET_CHARS = 6
export const BROWSER_AD = new TextEncoder().encode("clawdline browser invite v1 browser")
export const MACHINE_AD = new TextEncoder().encode("clawdline browser invite v1 machine")
const CLAIM_CONTEXT = "clawdline browser invite claim v1"
const NONCE_BYTES = 12

export type InviteFailure =
  | "bad_code" | "unknown_invite" | "invite_used" | "invite_expired" | "rate_limited"
  | "denied" | "code_mismatch" | "device_limit" | "network" | "failed"

export class InviteError extends Error {
  readonly code: InviteFailure
  constructor(code: InviteFailure, message: string = code) { super(message); this.code = code }
}

const utf8 = (s: string) => new TextEncoder().encode(s)
/** WebCrypto takes ArrayBuffer-backed views; every view here is one. */
const bs = (u: Uint8Array) => u as unknown as BufferSource

/** Reads what a person typed: case, spaces and dashes ignored; O→0, I/L→1. */
export function parseInviteCode(typed: string): { id: string; secret: string } | null {
  let out = ""
  for (let c of typed.toUpperCase()) {
    if (c === " " || c === "-" || c === "\t") continue
    if (c === "O") c = "0"
    if (c === "I" || c === "L") c = "1"
    if (!INVITE_ALPHABET.includes(c)) return null
    out += c
  }
  if (out.length !== INVITE_ID_CHARS + INVITE_SECRET_CHARS) return null
  return { id: out.slice(0, INVITE_ID_CHARS), secret: out.slice(INVITE_ID_CHARS) }
}

/** PRS, CI and sid, as InviteCPaceInputs; the machine id is bound in the sealed messages instead. */
export function cpaceInputs(id: string, secret: string) {
  return { prs: utf8(secret), ci: lvCat(utf8("clawdline browser invite v1")), sid: utf8("clawdline-invite:" + id) }
}

export interface InviteKeys { confirm: CryptoKey; handover: CryptoKey; code: string; raw?: { confirm: Uint8Array; handover: Uint8Array } }

/** HKDF-SHA256 over the ISK, salted by the id, exactly as DeriveInviteKeys. */
export async function deriveInviteKeys(isk: Uint8Array, id: string, keepRaw = false): Promise<InviteKeys> {
  const base = await crypto.subtle.importKey("raw", bs(isk), "HKDF", false, ["deriveBits"])
  const salt = utf8("clawdline-invite:" + id)
  const bits = async (label: string, bytes: number) => new Uint8Array(await crypto.subtle.deriveBits(
    { name: "HKDF", hash: "SHA-256", salt: bs(salt), info: bs(utf8(label)) }, base, bytes * 8))
  const confirmRaw = await bits("clawdline browser invite v1 confirm key", 32)
  const handoverRaw = await bits("clawdline browser invite v1 handover key", 32)
  const digits = await bits("clawdline browser invite v1 comparison code", 4)
  const n = ((digits[0] << 24) >>> 0) + (digits[1] << 16) + (digits[2] << 8) + digits[3]
  const aes = (raw: Uint8Array) => crypto.subtle.importKey("raw", bs(raw), "AES-GCM", false, ["encrypt", "decrypt"])
  return {
    confirm: await aes(confirmRaw), handover: await aes(handoverRaw), code: String(n % 1_000_000).padStart(6, "0"),
    ...(keepRaw ? { raw: { confirm: confirmRaw, handover: handoverRaw } } : {}),
  }
}

function toBase64(bytes: Uint8Array): string {
  let s = ""
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s)
}

function fromBase64(text: string): Uint8Array {
  const s = atob(text)
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}

/** AES-256-GCM, nonce prepended, the invite id as associated data. */
export async function sealInvite(key: CryptoKey, inviteID: string, value: unknown, nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES))): Promise<string> {
  const plain = utf8(JSON.stringify(value))
  const sealed = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: bs(nonce), additionalData: bs(utf8(inviteID)) }, key, bs(plain)))
  const out = new Uint8Array(nonce.length + sealed.length)
  out.set(nonce)
  out.set(sealed, nonce.length)
  return toBase64(out)
}

export async function openInvite(key: CryptoKey, inviteID: string, sealed: string): Promise<Record<string, unknown>> {
  let raw: Uint8Array
  try { raw = fromBase64(sealed) } catch { throw new InviteError("failed", "the sealed message is not base64") }
  if (raw.length < NONCE_BYTES + 16) throw new InviteError("failed", "the sealed message is too short")
  let plain: ArrayBuffer
  try {
    plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: bs(raw.slice(0, NONCE_BYTES)), additionalData: bs(utf8(inviteID)) },
      key, bs(raw.slice(NONCE_BYTES)))
  } catch {
    throw new InviteError("code_mismatch", "the handover does not open with this invitation's key")
  }
  const value = JSON.parse(new TextDecoder().decode(plain))
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new InviteError("failed", "the sealed message is not an object")
  return value as Record<string, unknown>
}

/** The confirmation, with the field order Go writes, so the two seal identical bytes. */
export function confirmBody(c: { inviteID: string; machineID: string; deviceID: string; publicKey: string; name: string }) {
  return { v: 1, type: "clawdline_browser_invite_confirm", invite_id: c.inviteID, machine_id: c.machineID,
    device_id: c.deviceID, public_key: c.publicKey, name: c.name }
}

export interface InviteHandover {
  inviteID: string; accountID: string; machineID: string; machineSigningKey: string; machineFingerprint: string
  keyID: string; masterSecret: string; deviceID: string; scope: string; expiresAt: number
}

export function readHandover(value: Record<string, unknown>, inviteID: string, deviceID: string, machineID: string): InviteHandover {
  const str = (k: string) => typeof value[k] === "string" ? value[k] as string : ""
  if (value.v !== 1 || value.type !== "clawdline_browser_invite_handover" || str("invite_id") !== inviteID
      || str("device_id") !== deviceID || str("machine_id") !== machineID || !str("master_secret")
      || !str("machine_signing_key") || typeof value.expires_at !== "number") {
    throw new InviteError("failed", "the handover names a different invitation, browser or machine")
  }
  return { inviteID, accountID: str("account_id"), machineID, machineSigningKey: str("machine_signing_key"),
    machineFingerprint: str("machine_fingerprint"), keyID: str("key_id"), masterSecret: str("master_secret"),
    deviceID, scope: str("scope"), expiresAt: value.expires_at as number }
}

export function claimMessage(inviteID: string, deviceID: string): Uint8Array {
  return utf8(`${CLAIM_CONTEXT}|${inviteID}|${deviceID}`)
}

// --- the network walk ---

export type JoinStage =
  | { stage: "joining" }
  | { stage: "exchanging"; machineName: string }
  | { stage: "compare"; code: string; machineName: string; expiry: string }
  | { stage: "claiming"; machineName: string }

export interface JoinResult {
  accountID: string; deviceID: string; machineID: string; machineName: string
  handover: InviteHandover; devicePrivateKey: CryptoKey; devicePublicKey: Uint8Array; expiresAt: string | null
}

interface Fetcher { (url: string, init?: RequestInit): Promise<Response> }

const POLL_MS = 1500
const sleep = (ms: number, signal?: AbortSignal) => new Promise<void>((resolve, reject) => {
  const t = setTimeout(resolve, ms)
  signal?.addEventListener("abort", () => { clearTimeout(t); reject(new InviteError("failed", "stopped")) }, { once: true })
})

function failureOf(status: number, code: unknown): InviteError {
  const c = typeof code === "string" ? code : ""
  if (status === 429) return new InviteError("rate_limited")
  if (c === "unknown_invite" || status === 404) return new InviteError("unknown_invite")
  if (c === "invite_used") return new InviteError("invite_used")
  if (c === "invite_expired") return new InviteError("invite_expired")
  if (c === "invite_denied") return new InviteError("denied")
  if (c === "device_limit_reached") return new InviteError("device_limit")
  return new InviteError("failed", c || `HTTP ${status}`)
}

async function call(fetcher: Fetcher, url: string, init: RequestInit): Promise<Record<string, unknown>> {
  let res: Response
  try { res = await fetcher(url, { credentials: "include", ...init }) } catch { throw new InviteError("network") }
  let body: Record<string, unknown> = {}
  try { body = await res.json() as Record<string, unknown> } catch { /* an empty answer */ }
  if (!res.ok) throw failureOf(res.status, (body.error as { code?: unknown } | undefined)?.code)
  return body
}

/** Runs the invited browser's side from a typed code to a claimed session. */
export async function joinBrowserInvite(apiOrigin: string, typed: string, name: string,
  onStage: (s: JoinStage) => void, options: { fetch?: Fetcher; signal?: AbortSignal } = {}): Promise<JoinResult> {
  const fetcher: Fetcher = options.fetch ?? ((url, init) => fetch(url, init))
  const parsed = parseInviteCode(typed)
  if (!parsed) throw new InviteError("bad_code")
  const { id, secret } = parsed
  const base = `${apiOrigin}/v1/pairing/browser-invites/${id}`
  onStage({ stage: "joining" })
  const pair = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]) as CryptoKeyPair
  const publicKey = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey))

  const inputs = cpaceInputs(id, secret)
  const party = CPaceParty.create(inputs.prs, inputs.ci, inputs.sid, BROWSER_AD, true)
  const joined = await call(fetcher, `${base}/join`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ browser_message: toBase64(party.message), device_public_key: toBase64(publicKey), name }),
  })
  const token = typeof joined.join_token === "string" ? joined.join_token : ""
  const deviceID = typeof joined.device_id === "string" ? joined.device_id : ""
  const machineID = typeof joined.machine_id === "string" ? joined.machine_id : ""
  const machineName = typeof joined.machine_name === "string" ? joined.machine_name : ""
  const expiry = typeof joined.expiry === "string" ? joined.expiry : ""
  if (!token || !deviceID || !machineID) throw new InviteError("failed", "Cloud's join answer is incomplete")
  const tokenHeaders = { "content-type": "application/json", "x-clawdline-join-token": token }
  onStage({ stage: "exchanging", machineName })

  const until = async (want: (b: Record<string, unknown>) => boolean): Promise<Record<string, unknown>> => {
    for (;;) {
      const body = await call(fetcher, `${base}/browser`, { headers: { "x-clawdline-join-token": token } })
      if (body.status === "denied") throw new InviteError("denied")
      if (body.status === "expired") throw new InviteError("invite_expired")
      if (want(body)) return body
      await sleep(POLL_MS, options.signal)
    }
  }
  const answered = await until((b) => typeof b.machine_message === "string")
  let isk: Uint8Array
  try {
    isk = party.finish(fromBase64(answered.machine_message as string), MACHINE_AD)
  } catch {
    throw new InviteError("code_mismatch", "the machine's answer is not a valid element")
  }
  const keys = await deriveInviteKeys(isk, id)
  isk.fill(0)
  const sealed = await sealInvite(keys.confirm, id, confirmBody({ inviteID: id, machineID, deviceID,
    publicKey: toBase64(publicKey), name }))
  await call(fetcher, `${base}/confirm`, { method: "POST", headers: tokenHeaders, body: JSON.stringify({ browser_confirm: sealed }) })
  onStage({ stage: "compare", code: keys.code, machineName, expiry })

  // A wrong secret is found by the machine (it cannot open the confirmation) and answered
  // `denied`; the owner declining is the same answer. Either way nothing was handed over.
  const admitted = await until((b) => b.status === "admitted" && typeof b.handover === "string")
  const handover = readHandover(await openInvite(keys.handover, id, admitted.handover as string), id, deviceID, machineID)
  onStage({ stage: "claiming", machineName })
  const signature = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, pair.privateKey, bs(claimMessage(id, deviceID))))
  const claimed = await call(fetcher, `${base}/claim`, { method: "POST", headers: tokenHeaders,
    body: JSON.stringify({ signature: toBase64(signature) }) })
  const accountID = typeof claimed.account_id === "string" ? claimed.account_id : handover.accountID
  if (accountID !== handover.accountID || claimed.machine_id !== machineID) {
    throw new InviteError("failed", "Cloud claimed a different account or machine than the machine sealed")
  }
  return { accountID, deviceID, machineID, machineName, handover, devicePrivateKey: pair.privateKey,
    devicePublicKey: publicKey, expiresAt: typeof claimed.expires_at === "string" ? claimed.expires_at : null }
}
