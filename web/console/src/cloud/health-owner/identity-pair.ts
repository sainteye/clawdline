/*
 * This viewer's half of identity pairing over Cloud's strict routes
 * (`/v1/pairing/identity/phases/:phase`, `/poll`). The crypto is
 * `legacy/js/net/cloud-pairing.js`; this file only carries it and decides when
 * the result may be kept.
 *
 * Nothing is kept until the Mac's `confirm` names this viewer's exact
 * `activate`: a grant that opened but was never confirmed leaves no key. The
 * person compares the six-digit code with the Mac before the offer is sent.
 * `isCurrent` is asked after every await; a sign-out or account switch while
 * this runs throws `stale` and nothing is persisted.
 */
import {
  canonicalJSON, createIdentityOffer, decodeIdentityPairingQR, checkIdentityPairingQR, openIdentityGrant, openIdentityConfirm,
} from "../../legacy/js/net/cloud-pairing.js"

export const IDENTITY_POLL_INTERVAL_MS = 1_000
export const IDENTITY_POLL_MAX_ATTEMPTS = 600
export const IDENTITY_REQUEST_TIMEOUT_MS = 15_000

type Fetch = (input: string, init: RequestInit) => Promise<Response>

export interface IdentityViewerDeps {
  apiOrigin: string
  fetch: Fetch
  accountID: string
  deviceID: string
  /** This browser's registered Ed25519 public key, 32 bytes. */
  signingPublicKey: Uint8Array
  /** The person compares this code and fingerprint with the Mac; false stops the pairing. */
  confirmCode(code: string, machineFingerprint: string): Promise<boolean>
  /** Called once, after the confirm verified, with non-extractable keys only. */
  persist(result: IdentityViewerResult): Promise<void>
  isCurrent(): boolean
  now(): number
  sleep(ms: number): Promise<void>
}

export interface IdentityViewerResult {
  accountID: string
  machineID: string
  machineFingerprint: string
  contentKeyEpoch: number
  identityEpoch: number
  contentKeyID?: string
  machineSigningKey?: string
  masterKey: CryptoKey
  hkdfKey: CryptoKey
}

export class IdentityPairingError extends Error {
  readonly code: string
  constructor(code: string, message?: string) {
    super(message || code)
    this.code = code
  }
}

async function call(deps: IdentityViewerDeps, path: string, body: unknown): Promise<{ status: number; json: any }> {
  let response: Response
  try {
    response = await deps.fetch(deps.apiOrigin + path, {
      method: "POST", credentials: "include", cache: "no-store",
      headers: { "Content-Type": "application/json" }, body: canonicalJSON(body),
      signal: AbortSignal.timeout(IDENTITY_REQUEST_TIMEOUT_MS),
    })
  } catch {
    throw new IdentityPairingError("unavailable", "Cloud could not be reached")
  }
  let json: any = null
  try { json = await response.json() } catch { /* typed below */ }
  if (!response.ok) throw new IdentityPairingError(json?.error?.code || `http_${response.status}`)
  return { status: response.status, json }
}

function guard(deps: IdentityViewerDeps): void {
  if (!deps.isCurrent()) throw new IdentityPairingError("stale", "the account changed while pairing ran")
}

async function poll(deps: IdentityViewerDeps, qr: any, phase: string): Promise<any> {
  for (let attempt = 0; attempt < IDENTITY_POLL_MAX_ATTEMPTS; attempt++) {
    const { status, json } = await call(deps, "/v1/pairing/identity/poll",
      { pairing_id: qr.pairing_id, phase, claim_nonce: qr.claim_nonce })
    guard(deps)
    if (status === 200 && json?.status === "ready") return json.blob
    await deps.sleep(IDENTITY_POLL_INTERVAL_MS)
    guard(deps)
  }
  throw new IdentityPairingError("poll_exhausted", "the machine did not answer before the pairing expired")
}

/** Runs offer → (grant) → activate → (confirm) and persists only after confirm. */
export async function pairThisViewer(fragment: string, deps: IdentityViewerDeps): Promise<IdentityViewerResult> {
  const qr = await checkIdentityPairingQR(decodeIdentityPairingQR(fragment, deps.now()))
  if (qr.account_id !== deps.accountID) throw new IdentityPairingError("wrong_account", "the QR belongs to another account")
  const offered = await createIdentityOffer({ qr, deviceID: deps.deviceID, signingPublicKey: deps.signingPublicKey })
  if (!await deps.confirmCode(offered.shortCode, qr.machine_fingerprint)) {
    throw new IdentityPairingError("not_confirmed", "the code was not confirmed")
  }
  guard(deps)
  await call(deps, "/v1/pairing/identity/phases/offer", { blob: offered.wrapper, claim_nonce: qr.claim_nonce })
  guard(deps)
  const grant = await poll(deps, qr, "grant")
  const activated = await openIdentityGrant({ qr, offered, wrapper: grant, deviceID: deps.deviceID })
  guard(deps)
  await call(deps, "/v1/pairing/identity/phases/activate", { blob: activated.wrapper, claim_nonce: qr.claim_nonce })
  guard(deps)
  const confirm = await poll(deps, qr, "confirm")
  await openIdentityConfirm({ qr, offered, activated, wrapper: confirm, deviceID: deps.deviceID })
  guard(deps)
  const result: IdentityViewerResult = {
    accountID: qr.account_id, machineID: activated.machineID, machineFingerprint: activated.machineFingerprint,
    contentKeyEpoch: activated.contentKeyEpoch, identityEpoch: activated.identityEpoch,
    contentKeyID: activated.contentKeyID,
    machineSigningKey: btoa(String.fromCharCode(...activated.machineSigningKey)),
    masterKey: activated.masterKey, hkdfKey: activated.hkdfKey,
  }
  await deps.persist(result)
  return result
}
