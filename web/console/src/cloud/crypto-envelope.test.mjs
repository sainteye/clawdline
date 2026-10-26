import { test } from "node:test"
import assert from "node:assert/strict"
import { base64Bytes, bytesBase64, importMasterSecret, openEnvelope, sealEnvelope } from "../legacy/js/net/cloud-crypto.js"

test("canonical base64 accepts attachment-sized bytes without exhausting the stack", () => {
  const bytes = Buffer.alloc(4_000_000, 173)
  assert.deepEqual(base64Bytes(bytes.toString("base64")), new Uint8Array(bytes))
})

test("canonical base64 still refuses whitespace, bad padding and nonzero padding bits", () => {
  for (const text of [" AA==", "AA==\n", "AA", "A===", "AA=A", "!!!!", "AB==", "AAB=", "ＡA=="]) {
    assert.throws(() => base64Bytes(text, "fixture"), /fixture is not canonical base64/, JSON.stringify(text))
  }
  for (const text of ["", "AA==", "AAA=", "AAAA", "+/8="]) {
    assert.equal(bytesBase64(base64Bytes(text)), text)
  }
})

test("a six-picture prompt seals, verifies and decrypts with its original bytes", async () => {
  const signing = await crypto.subtle.generateKey("Ed25519", false, ["sign", "verify"])
  const master = await importMasterSecret(new Uint8Array(32).fill(7))
  const picture = "data:image/png;base64," + Buffer.alloc(600_000, 173).toString("base64")
  const prompt = JSON.stringify({ type: "send", text: "Please review these screenshots.", images: Array(6).fill(picture) })
  const envelope = await sealEnvelope({ ch: "ctl/machine-test", seq: 1, ts: 1,
    class: "ctl", key_id: "key-test", sender: "viewer-test" }, prompt, master, signing.privateKey)
  const opened = await openEnvelope(envelope, master, signing.publicKey)
  assert.equal(new TextDecoder().decode(opened), prompt)
})
