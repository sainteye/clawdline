import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- Node's strip-types tests import this module directly.
import { IdentityPairingError, pairThisViewer } from "./identity-pair.ts"

const b64url = (text: string) => Buffer.from(text).toString("base64url")
const b64 = (n: number, fill: number) => Buffer.alloc(n, fill).toString("base64")

async function fragment(): Promise<string> {
  const machine = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]) as CryptoKeyPair
  const signing = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]) as CryptoKeyPair
  const raw = new Uint8Array(await crypto.subtle.exportKey("raw", signing.publicKey))
  const { ed25519Fingerprint, canonicalJSON } = await import("../../legacy/js/net/cloud-pairing.js")
  return b64url(canonicalJSON({
    v: 1, type: "pairing_qr", pairing_id: "pai_test", claim_nonce: b64(32, 1), expires_at: Date.now() + 60_000,
    account_id: "usr_test", machine_id: "mac_test", machine_signing_key: Buffer.from(raw).toString("base64"),
    machine_fingerprint: await ed25519Fingerprint(raw),
    machine_ephemeral_key: Buffer.from(new Uint8Array(await crypto.subtle.exportKey("raw", machine.publicKey))).toString("base64"),
    pairing_nonce: b64(32, 2),
  }))
}

function deps(over: Record<string, unknown>) {
  return {
    apiOrigin: "https://api.test", accountID: "usr_test", deviceID: "wd_test", signingPublicKey: new Uint8Array(32).fill(7),
    confirmCode: async () => true, persist: async () => { throw new Error("must not persist") },
    isCurrent: () => true, now: () => Date.now(), sleep: async () => {},
    fetch: async () => new Response("{}", { status: 200 }),
    ...over,
  }
}

test("a sign-out while the offer is in flight persists nothing", async () => {
  let current = true
  let persisted = 0
  const run = pairThisViewer(await fragment(), deps({
    isCurrent: () => current,
    fetch: async () => { current = false; return new Response("{}", { status: 200 }) },
    persist: async () => { persisted++ },
  }))
  await assert.rejects(run, (e: unknown) => e instanceof IdentityPairingError && e.code === "stale")
  assert.equal(persisted, 0)
})

test("an unconfirmed code sends nothing", async () => {
  let calls = 0
  await assert.rejects(pairThisViewer(await fragment(), deps({
    confirmCode: async () => false, fetch: async () => { calls++; return new Response("{}") },
  })), (e: unknown) => e instanceof IdentityPairingError && e.code === "not_confirmed")
  assert.equal(calls, 0)
})

test("a QR for another account is refused before any request", async () => {
  let calls = 0
  await assert.rejects(pairThisViewer(await fragment(), deps({
    accountID: "usr_other", fetch: async () => { calls++; return new Response("{}") },
  })), (e: unknown) => e instanceof IdentityPairingError && e.code === "wrong_account")
  assert.equal(calls, 0)
})

test("a Cloud refusal keeps its code", async () => {
  await assert.rejects(pairThisViewer(await fragment(), deps({
    fetch: async () => new Response(JSON.stringify({ error: { code: "identity_epoch_changed" } }), { status: 409 }),
  })), (e: unknown) => e instanceof IdentityPairingError && e.code === "identity_epoch_changed")
})

test("a confirmed later-epoch identity grant opens ordinary envelopes with the full durable ID", async () => {
  const { canonicalJSON, ed25519Fingerprint, derivePhaseKey, identityPhaseDigest } = await import("../../legacy/js/net/cloud-pairing.js")
  const { sealEnvelope, openEnvelope } = await import("../../legacy/js/net/cloud-crypto.js")
  const machine = await crypto.subtle.generateKey("X25519", true, ["deriveBits"]) as CryptoKeyPair
  const signer = await crypto.subtle.generateKey("Ed25519", true, ["sign", "verify"]) as CryptoKeyPair
  const publicKey = new Uint8Array(await crypto.subtle.exportKey("raw", signer.publicKey))
  const ownEph = Buffer.from(await crypto.subtle.exportKey("raw", machine.publicKey)).toString("base64")
  const qr = { v: 1, type: "pairing_qr", pairing_id: "pai_epoch", claim_nonce: b64(32, 1), expires_at: Date.now() + 60_000,
    account_id: "usr_test", machine_id: "mac_test", machine_signing_key: Buffer.from(publicKey).toString("base64"),
    machine_fingerprint: await ed25519Fingerprint(publicKey), machine_ephemeral_key: ownEph, pairing_nonce: b64(32, 2) }
  const raw = crypto.getRandomValues(new Uint8Array(32))
  const keyID = "mk-7-" + Buffer.from(await crypto.subtle.digest("SHA-256", raw)).toString("hex")
  let offer: any
  let activate: any
  async function seal(phase: string, plain: unknown) {
    const peer = await crypto.subtle.importKey("raw", Buffer.from(offer.ephemeral_key, "base64"), "X25519", false, [])
    const shared = new Uint8Array(await crypto.subtle.deriveBits({ name: "X25519", public: peer }, machine.privateKey, 256))
    const bits = await derivePhaseKey({ shared, pairingNonce: Buffer.from(qr.pairing_nonce, "base64"),
      pairingID: qr.pairing_id, claimNonce: Buffer.from(qr.claim_nonce, "base64"), phase })
    const key = await crypto.subtle.importKey("raw", bits, "AES-GCM", false, ["encrypt"])
    const aad = { v: 1, phase, pairing_id: qr.pairing_id, sender_device_id: qr.machine_id, ephemeral_key: ownEph }
    const nonce = crypto.getRandomValues(new Uint8Array(12))
    const ct = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce,
      additionalData: new TextEncoder().encode(canonicalJSON(aad)) }, key, new TextEncoder().encode(canonicalJSON(plain)))
    return { ...aad, nonce: Buffer.from(nonce).toString("base64"), ct: Buffer.from(ct).toString("base64") }
  }
  let persisted: any
  const result = await pairThisViewer(b64url(canonicalJSON(qr)), deps({
    persist: async (value: unknown) => { persisted = value },
    fetch: async (url: string, init: RequestInit) => {
      const body = JSON.parse(String(init.body))
      if (url.endsWith("/offer")) offer = body.blob
      else if (url.endsWith("/activate")) activate = body.blob
      else if (body.phase === "grant") return Response.json({ status: "ready", blob: await seal("grant", {
        v: 1, type: "identity_grant", pairing_id: qr.pairing_id, account_id: qr.account_id, machine_id: qr.machine_id,
        machine_signing_key: qr.machine_signing_key, machine_fingerprint: qr.machine_fingerprint,
        viewer_device_id: "wd_test", offer_sha256: await identityPhaseDigest(offer), content_key_epoch: 7, identity_epoch: 4,
        master_secret: Buffer.from(raw).toString("base64"),
      }) })
      else if (body.phase === "confirm") return Response.json({ status: "ready", blob: await seal("confirm", {
        v: 1, type: "identity_confirm", pairing_id: qr.pairing_id, account_id: qr.account_id, machine_id: qr.machine_id,
        viewer_device_id: "wd_test", content_key_epoch: 7, activate_sha256: await identityPhaseDigest(activate),
      }) })
      return Response.json({})
    },
  }))
  assert.equal(persisted, result)
  assert.equal(result.contentKeyEpoch, 7)
  assert.equal(result.contentKeyID, keyID)
  assert.equal(result.machineSigningKey, qr.machine_signing_key)
  assert.equal(result.masterKey.extractable, false)
  const encryptKey = await crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt"])
  const envelope = await sealEnvelope({ ch: "s/mac_test/session", seq: 1, ts: Date.now(), class: "stream", key_id: keyID, sender: "mac_test" }, "current epoch", encryptKey, signer.privateKey)
  assert.equal(new TextDecoder().decode(await openEnvelope(envelope, result.masterKey, signer.publicKey)), "current epoch")
})
