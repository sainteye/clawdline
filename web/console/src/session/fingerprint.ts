/*
 * The name of the question a waiting card answers.
 *
 * A digit answers whatever picker is on the screen when it lands. A page that
 * drew one question and whose press lands on another — its first press landed
 * and the reply was lost, a second device answered first, the permission
 * prompt for the next tool call replaced this one — would approve something
 * nobody read. So a press carries this name (`expect`), and the Mac types
 * nothing unless the question on its screen still has it
 * (`internal/app/keys.go`, `session.MenuFingerprint`).
 *
 * It names the question, not the moment: the prose above the rows (for a
 * permission prompt, the command it asks about), each row's number and words,
 * and a set's tab bar. The caret, the ticks, a row's note and the button move
 * while the same question is up, and are left out.
 *
 * The canonical bytes are the Go function's exactly — every field
 * length-prefixed in UTF-8 bytes — and both ends assert the same vector
 * (`fingerprint.test.ts`, `fingerprint_test.go`).
 *
 * SHA-256 is computed here rather than by `crypto.subtle`, which a browser
 * gives only to a secure context: the console served by the daemon over plain
 * http on a home network has none, and a press there must be able to name its
 * question too. Nothing is imported at run time, so `node --test` loads it.
 */
import type { SessionMenu } from "@clawdline/contract"

/** The question's name: sixty-four lowercase hex digits. */
export function menuFingerprint(menu: SessionMenu): string {
  const encoder = new TextEncoder()
  const parts: Uint8Array[] = [encoder.encode("menu/1")]
  const field = (tag: string, text: string) => {
    const bytes = encoder.encode(text)
    parts.push(encoder.encode(tag + String(bytes.length) + ":"), bytes)
  }
  field("q", typeof menu.question === "string" ? menu.question : "")
  for (const o of menu.options ?? []) field("o" + String(o.n) + ",", o.label == null ? "" : String(o.label))
  for (const s of menu.steps ?? []) field("s" + (s.done ? "1" : "0") + ",", s.label == null ? "" : String(s.label))
  let length = 0
  for (const p of parts) length += p.length
  const all = new Uint8Array(length)
  let at = 0
  for (const p of parts) {
    all.set(p, at)
    at += p.length
  }
  return sha256Hex(all)
}

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98,
  0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
  0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8,
  0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819,
  0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
  0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
  0xc67178f2,
])

/** FIPS 180-4 SHA-256 of `data`, as lowercase hex. */
export function sha256Hex(data: Uint8Array): string {
  const h = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19])
  // The message, a 1 bit, zeros, and its length in bits as 64 bits.
  const blocks = Math.ceil((data.length + 9) / 64)
  const padded = new Uint8Array(blocks * 64)
  padded.set(data)
  padded[data.length] = 0x80
  const bits = data.length * 8
  const view = new DataView(padded.buffer)
  view.setUint32(padded.length - 8, Math.floor(bits / 0x100000000))
  view.setUint32(padded.length - 4, bits >>> 0)
  const w = new Uint32Array(64)
  for (let block = 0; block < blocks; block++) {
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(block * 64 + i * 4)
    for (let i = 16; i < 64; i++) {
      const a = w[i - 15]
      const b = w[i - 2]
      const s0 = ((a >>> 7) | (a << 25)) ^ ((a >>> 18) | (a << 14)) ^ (a >>> 3)
      const s1 = ((b >>> 17) | (b << 15)) ^ ((b >>> 19) | (b << 13)) ^ (b >>> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0
    }
    let [a, b, c, d, e, f, g, hh] = h
    for (let i = 0; i < 64; i++) {
      const s1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7))
      const ch = (e & f) ^ (~e & g)
      const t1 = (hh + s1 + ch + K[i] + w[i]) >>> 0
      const s0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10))
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (s0 + maj) >>> 0
      hh = g
      g = f
      f = e
      e = (d + t1) >>> 0
      d = c
      c = b
      b = a
      a = (t1 + t2) >>> 0
    }
    h[0] = (h[0] + a) >>> 0
    h[1] = (h[1] + b) >>> 0
    h[2] = (h[2] + c) >>> 0
    h[3] = (h[3] + d) >>> 0
    h[4] = (h[4] + e) >>> 0
    h[5] = (h[5] + f) >>> 0
    h[6] = (h[6] + g) >>> 0
    h[7] = (h[7] + hh) >>> 0
  }
  let out = ""
  for (const word of h) out += word.toString(16).padStart(8, "0")
  return out
}
