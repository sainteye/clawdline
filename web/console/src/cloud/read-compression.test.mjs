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
    const tampered = Buffer.from(envelope.ct,"base64")
    tampered[0] ^= 1
    await assert.rejects(openEnvelope({...envelope,ct:tampered.toString("base64")},key,signer.publicKey),/signature/)
  }
})
test("decompression refuses corrupt data and stops at the registered output ceiling", async () => {
  await assert.rejects(unpackReadPayload(new Uint8Array([103,122,49,0,1,2,3])))
  await assert.rejects(unpackReadPayload(framed(Buffer.alloc(READ_PLAINTEXT_BYTES_LIMIT+1,65))), error => error.code === "read_payload_too_large")
  assert.equal((await unpackReadPayload(framed(Buffer.alloc(READ_PLAINTEXT_BYTES_LIMIT,65)))).length,READ_PLAINTEXT_BYTES_LIMIT)
})

test("unknown and corrupt formats have typed isolated failures",async()=>{
 await assert.rejects(unpackReadPayload(new Uint8Array([103,122,50,0,1])),error=>error.code==="read_encoding_unsupported")
 await assert.rejects(unpackReadPayload(new Uint8Array([103,122,49,0,1,2,3])),error=>error.code==="read_payload_corrupt")
 const next=new TextEncoder().encode('{"read":"next","body":{"ok":true}}')
 assert.deepEqual(await unpackReadPayload(framed(next)),next,"a failed frame cannot poison the next read")
})

test("invalid signatures are refused before any decompressor is created",async()=>{
 const key=await crypto.subtle.generateKey({name:"AES-GCM",length:256},false,["encrypt","decrypt"])
 const signer=await crypto.subtle.generateKey("Ed25519",false,["sign","verify"])
 const value=framed(new TextEncoder().encode('{"read":"fixture"}'))
 const envelope=await sealEnvelope({ch:"t/m/s",seq:1,ts:1,class:"stream",key_id:"key",sender:"machine"},value,key,signer.privateKey)
 const saved=globalThis.DecompressionStream
 let opened=0
 globalThis.DecompressionStream=class {constructor(){opened++;throw new Error("must not decompress")}}
 try {
  const sig=Buffer.from(envelope.sig,"base64");sig[0]^=1
  await assert.rejects(openEnvelope({...envelope,sig:sig.toString("base64")},key,signer.publicKey),/signature/)
  assert.equal(opened,0)
 } finally {globalThis.DecompressionStream=saved}
})
