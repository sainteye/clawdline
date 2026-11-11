// CPace over ristretto255 with SHA-512 (draft-irtf-cfrg-cpace-21), the
// browser half of a browser invitation. The Go half is
// internal/domain/cloud/cpace.go; both are checked against
// internal/domain/cloud/testdata/cpace-ristretto255-v1.json, which holds the
// draft's own vectors.
//
// The short secret half of an invitation code is the password. Cloud relays
// both messages and never learns it: a relay that substitutes a message gets
// one online guess, and then the invitation is spent.

import { ristretto255, ristretto255_hasher } from "@noble/curves/ed25519.js"
import { bytesToNumberLE } from "@noble/curves/utils.js"
import { sha512 } from "@noble/hashes/sha2.js"

export const CPACE_DSI = "CPaceRistretto255"
const HASH_BLOCK_BYTES = 128
const ELEMENT_BYTES = 32
const Point = ristretto255.Point
const ORDER = Point.Fn.ORDER

export class CPaceError extends Error {
  readonly code: "cpace_invalid_peer" | "cpace_scalar" | "cpace_used"
  constructor(code: CPaceError["code"], message: string) {
    super(message)
    this.code = code
  }
}

const utf8 = (s: string) => new TextEncoder().encode(s)

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let at = 0
  for (const p of parts) { out.set(p, at); at += p.length }
  return out
}

/** prepend_len: the LEB128 length of data, then data. */
export function prependLen(data: Uint8Array): Uint8Array {
  const len: number[] = []
  let n = data.length
  for (;;) {
    len.push(n < 128 ? n : (n & 0x7f) | 0x80)
    n = Math.floor(n / 128)
    if (n === 0) break
  }
  return concat(Uint8Array.from(len), data)
}

/** lv_cat: each argument with its length prepended. */
export function lvCat(...parts: Uint8Array[]): Uint8Array {
  return concat(...parts.map(prependLen))
}

/** generator_string(DSI, PRS, CI, sid, 128). */
export function generatorString(prs: Uint8Array, ci: Uint8Array, sid: Uint8Array): Uint8Array {
  const dsi = utf8(CPACE_DSI)
  const zpad = Math.max(0, HASH_BLOCK_BYTES - 1 - prependLen(prs).length - prependLen(dsi).length)
  return lvCat(dsi, prs, new Uint8Array(zpad), ci, sid)
}

function generatorPoint(prs: Uint8Array, ci: Uint8Array, sid: Uint8Array) {
  const derive = ristretto255_hasher.deriveToCurve
  if (!derive) throw new Error("ristretto255 element derivation is unavailable")
  return derive(sha512(generatorString(prs, ci, sid)))
}

/** The encoded generator, exposed for the vectors. */
export function generator(prs: Uint8Array, ci: Uint8Array, sid: Uint8Array): Uint8Array {
  return generatorPoint(prs, ci, sid).toBytes()
}

function scalarFrom(bytes: Uint8Array): bigint {
  if (bytes.length !== 32) throw new CPaceError("cpace_scalar", "a CPace scalar is 32 bytes")
  const y = bytesToNumberLE(bytes)
  if (y === 0n || y >= ORDER) throw new CPaceError("cpace_scalar", "the scalar is not a canonical ristretto255 scalar")
  return y
}

/** scalar_mult_vfy: null when X does not decode or the product is the identity. */
export function scalarMultVfy(scalar: Uint8Array, x: Uint8Array): Uint8Array | null {
  const y = scalarFrom(scalar)
  if (x.length !== ELEMENT_BYTES) return null
  let point
  try { point = Point.fromBytes(x) } catch { return null }
  if (point.is0()) return null
  const k = point.multiply(y)
  return k.is0() ? null : k.toBytes()
}

/** transcript_ir(Ya, ADa, Yb, ADb). */
export function transcriptIR(ya: Uint8Array, ada: Uint8Array, yb: Uint8Array, adb: Uint8Array): Uint8Array {
  return concat(lvCat(ya, ada), lvCat(yb, adb))
}

/** ISK = SHA-512(lv_cat(DSI || "_ISK", sid, K) || transcript). */
export function isk(sid: Uint8Array, k: Uint8Array, transcript: Uint8Array): Uint8Array {
  return sha512(concat(lvCat(utf8(CPACE_DSI + "_ISK"), sid, k), transcript))
}

/** One side of one CPace run; finish() may be called once. */
export class CPaceParty {
  #scalar: Uint8Array | null
  readonly #sid: Uint8Array
  readonly #ad: Uint8Array
  readonly #initiator: boolean
  readonly message: Uint8Array

  /** Samples as Section 8.3 recommends: 32 random bytes, bits above 252 cleared. */
  static create(prs: Uint8Array, ci: Uint8Array, sid: Uint8Array, ad: Uint8Array, initiator: boolean): CPaceParty {
    for (;;) {
      const raw = crypto.getRandomValues(new Uint8Array(32))
      raw[31] &= 0x0f
      if (raw.some((b) => b !== 0)) return new CPaceParty(raw, prs, ci, sid, ad, initiator)
    }
  }

  /** A given little-endian scalar; only the vectors and tests choose one. */
  constructor(scalar: Uint8Array, prs: Uint8Array, ci: Uint8Array, sid: Uint8Array, ad: Uint8Array, initiator: boolean) {
    const y = scalarFrom(scalar)
    this.#scalar = Uint8Array.from(scalar)
    this.#sid = Uint8Array.from(sid)
    this.#ad = Uint8Array.from(ad)
    this.#initiator = initiator
    this.message = generatorPoint(prs, ci, sid).multiply(y).toBytes()
  }

  /** The 64-byte ISK; throws on an invalid or identity peer element. */
  finish(peer: Uint8Array, peerAD: Uint8Array): Uint8Array {
    const scalar = this.#scalar
    if (!scalar) throw new CPaceError("cpace_used", "this CPace party has already finished")
    this.#scalar = null
    const k = scalarMultVfy(scalar, peer)
    scalar.fill(0)
    if (!k) throw new CPaceError("cpace_invalid_peer", "the peer's element is not a valid non-identity ristretto255 encoding")
    const transcript = this.#initiator
      ? transcriptIR(this.message, this.#ad, peer, peerAD)
      : transcriptIR(peer, peerAD, this.message, this.#ad)
    return isk(this.#sid, k, transcript)
  }
}
