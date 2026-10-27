import { test } from "node:test"
import assert from "node:assert/strict"
import { gzipSync } from "node:zlib"
import { unpackReadPayload, READ_PLAINTEXT_BYTES_LIMIT } from "../legacy/js/net/read-compression.js"
import { sealEnvelope, openEnvelope } from "../legacy/js/net/cloud-crypto.js"

const framed = bytes => new Uint8Array(Buffer.concat([Buffer.from([103,122,49,0]), gzipSync(bytes)]))
test("both plain and authenticated compressed Cloud replies open to the same bytes", async () => {
  const plain = new TextEncoder().encode(JSON.stringify({ read: "transcript", body: { text: "line\n".repeat(1000) } }))
  const key = await crypto.subtle.generateKey({name:"AES-GCM",length:256},false,["encrypt","decrypt"])
  const signer = await crypto.subtle.generateKey("Ed25519",false,["sign","verify"])
  for (const value of [plain, framed(plain)]) {
    const envelope = await sealEnvelope({ ch:"t/m/s",seq:1,ts:1,class:"stream",key_id:"key",sender:"machine" }, value, key, signer.privateKey)
    assert.deepEqual(await openEnvelope(envelope,key,signer.publicKey),plain)
    await assert.rejects(openEnvelope({...envelope,ct:Buffer.concat([Buffer.from([99]),Buffer.from(envelope.ct,"base64").subarray(1)]).toString("base64")},key,signer.publicKey),/signature/)
  }
})
test("decompression refuses corrupt data and stops at the registered output ceiling", async () => {
  await assert.rejects(unpackReadPayload(new Uint8Array([103,122,49,0,1,2,3])))
  await assert.rejects(unpackReadPayload(framed(Buffer.alloc(READ_PLAINTEXT_BYTES_LIMIT+1,65))), error => error.code === "read_payload_too_large")
  assert.equal((await unpackReadPayload(framed(Buffer.alloc(READ_PLAINTEXT_BYTES_LIMIT,65)))).length,READ_PLAINTEXT_BYTES_LIMIT)
})
