import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error Node executes TypeScript directly.
import { CPaceError, CPaceParty, generator, generatorString, lvCat, prependLen, scalarMultVfy, transcriptIR } from "./cpace.ts"

// The same file internal/domain/cloud/cpace_test.go reads: the draft's vectors.
const vectors = JSON.parse(readFileSync(new URL("../../../../internal/domain/cloud/testdata/cpace-ristretto255-v1.json", import.meta.url), "utf8"))
const unhex = (s: string) => Uint8Array.from(Buffer.from(s, "hex"))
const hex = (b: Uint8Array) => Buffer.from(b).toString("hex")
const g = vectors.generator
const x = vectors.exchange
const prs = unhex(g.prs), ci = unhex(g.ci), sid = unhex(g.sid)

test("prepend_len and lv_cat match the draft vectors", () => {
  for (const c of vectors.prepend_len) assert.equal(hex(prependLen(unhex(c.input))), c.output)
  assert.equal(hex(lvCat(...vectors.lv_cat.inputs.map(unhex))), vectors.lv_cat.output)
  assert.deepEqual([...prependLen(new Uint8Array(128)).slice(0, 2)], [0x80, 0x01])
})

test("the generator string and generator match the draft vectors", () => {
  assert.equal(hex(generatorString(prs, ci, sid)), g.generator_string)
  assert.equal(hex(generator(prs, ci, sid)), g.g)
})

test("both messages, K, the transcript and the ISK match the draft vectors", () => {
  const a = new CPaceParty(unhex(x.ya), prs, ci, sid, unhex(x.ada), true)
  const b = new CPaceParty(unhex(x.yb), prs, ci, sid, unhex(x.adb), false)
  assert.equal(hex(a.message), x.Ya)
  assert.equal(hex(b.message), x.Yb)
  assert.equal(hex(scalarMultVfy(unhex(x.ya), b.message)!), x.K)
  assert.equal(hex(transcriptIR(a.message, unhex(x.ada), b.message, unhex(x.adb))), x.transcript_ir)
  assert.equal(hex(a.finish(b.message, unhex(x.adb))), x.isk_ir)
  assert.equal(hex(b.finish(unhex(x.Ya), unhex(x.ada))), x.isk_ir)
})

test("an invalid or identity peer element aborts the run", () => {
  for (const p of vectors.invalid.points) {
    assert.equal(scalarMultVfy(unhex(vectors.invalid.s), unhex(p)), null)
    const party = CPaceParty.create(new TextEncoder().encode("pw"), new Uint8Array(), new TextEncoder().encode("sid"), new Uint8Array(), true)
    assert.throws(() => party.finish(unhex(p), new Uint8Array()), (e: unknown) => e instanceof CPaceError && e.code === "cpace_invalid_peer")
  }
})

test("a wrong password gives different keys, a right one the same; a party finishes once", () => {
  const enc = (s: string) => new TextEncoder().encode(s)
  const s = enc("invite-sid")
  const a = CPaceParty.create(enc("K7M4Q9"), new Uint8Array(), s, enc("browser"), true)
  const b = CPaceParty.create(enc("K7M4Q8"), new Uint8Array(), s, enc("machine"), false)
  assert.notEqual(hex(a.finish(b.message, enc("machine"))), hex(b.finish(a.message, enc("browser"))))
  const c = CPaceParty.create(enc("K7M4Q9"), new Uint8Array(), s, enc("browser"), true)
  const d = CPaceParty.create(enc("K7M4Q9"), new Uint8Array(), s, enc("machine"), false)
  assert.equal(hex(c.finish(d.message, enc("machine"))), hex(d.finish(c.message, enc("browser"))))
  assert.throws(() => c.finish(d.message, enc("machine")), (e: unknown) => e instanceof CPaceError && e.code === "cpace_used")
})

test("a zero or non-canonical scalar is refused", () => {
  assert.throws(() => new CPaceParty(new Uint8Array(32), new Uint8Array(1), ci, sid, new Uint8Array(), true), CPaceError)
  assert.throws(() => new CPaceParty(new Uint8Array(32).fill(0xff), new Uint8Array(1), ci, sid, new Uint8Array(), true), CPaceError)
})
