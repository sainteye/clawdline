/**
 * A QR code encoder, byte mode only, for the one QR this window draws: the
 * pairing link.
 *
 * Written here rather than pulled in because what it has to do is small and
 * fixed — one segment of UTF-8 bytes, versions 1 to 40, the four error
 * correction levels, the eight masks — and every package that does it also
 * does kanji, numeric and alphanumeric segments, structured append, ECI and a
 * renderer this window does not need. It runs entirely in the page: the link
 * carries a one-time secret in its fragment, so it must never be sent to
 * anything that would draw it for us.
 *
 * The construction follows ISO/IEC 18004 in the order the standard lays it
 * out; the tables are the standard's. What proves it right is not this comment
 * but a decoder that did not write it reading the result back — see the report
 * that shipped with this file for the round trip.
 */

/** Error correction level, lowest first. Each step spends more of the symbol on recovery. */
export type QrEcc = "L" | "M" | "Q" | "H"

export type QrCode = {
  /** 1..40. The symbol is `17 + 4 * version` modules on a side. */
  version: number
  /** Modules on a side, without the quiet zone. */
  size: number
  ecc: QrEcc
  mask: number
  /** `dark[y * size + x]`, 1 for a dark module. */
  dark: Uint8Array
}

const ECC_ORDER: QrEcc[] = ["L", "M", "Q", "H"]

/** The two format bits each level is written as. Not in ECC_ORDER's order: that is the standard's choice. */
const FORMAT_BITS: Record<QrEcc, number> = { L: 1, M: 0, Q: 3, H: 2 }

// Error correction codewords per block, and the number of blocks, indexed by
// version (index 0 unused). ISO/IEC 18004 Table 9.
const ECC_PER_BLOCK: Record<QrEcc, number[]> = {
  L: [0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  M: [0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28],
  Q: [0, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  H: [0, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
}
const BLOCKS: Record<QrEcc, number[]> = {
  L: [0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25],
  M: [0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49],
  Q: [0, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68],
  H: [0, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81],
}

/** Modules left for data and error correction once every function pattern is drawn. */
function rawDataModules(version: number): number {
  let result = (16 * version + 128) * version + 64
  if (version >= 2) {
    const align = Math.floor(version / 7) + 2
    result -= (25 * align - 10) * align - 55
    if (version >= 7) result -= 36
  }
  return result
}

/** Data codewords a version holds at a level, once its error correction is taken out. */
function dataCodewords(version: number, ecc: QrEcc): number {
  return Math.floor(rawDataModules(version) / 8) - ECC_PER_BLOCK[ecc][version] * BLOCKS[ecc][version]
}

/** Bits a byte-mode segment of `bytes` bytes needs: mode, count, payload. */
function segmentBits(version: number, bytes: number): number {
  return 4 + (version <= 9 ? 8 : 16) + 8 * bytes
}

/** The smallest version that holds `bytes` bytes at `ecc`, or 0 when none does. */
function fittingVersion(bytes: number, ecc: QrEcc): number {
  for (let version = 1; version <= 40; version++) {
    const cap = version <= 9 ? 0xff : 0xffff
    if (bytes <= cap && segmentBits(version, bytes) <= dataCodewords(version, ecc) * 8) return version
  }
  return 0
}

/**
 * Encodes `text` as UTF-8 in one byte-mode segment.
 *
 * The version is the smallest that fits at `minimum`; the level is then raised
 * as far as it goes without growing the symbol, because a stronger code at the
 * same module count costs nothing on screen. Throws when the text does not fit
 * in version 40, which no pairing link comes near.
 */
export function encodeQr(text: string, minimum: QrEcc = "L"): QrCode {
  const bytes = new TextEncoder().encode(text)
  const version = fittingVersion(bytes.length, minimum)
  if (version === 0) throw new Error(`${bytes.length} bytes do not fit in a QR code at level ${minimum}`)
  let ecc = minimum
  for (const stronger of ECC_ORDER.slice(ECC_ORDER.indexOf(minimum) + 1)) {
    if (segmentBits(version, bytes.length) <= dataCodewords(version, stronger) * 8) ecc = stronger
  }
  const codewords = withErrorCorrection(dataStream(bytes, version, ecc), version, ecc)
  return place(codewords, version, ecc)
}

// MARK: the bit stream

function dataStream(bytes: Uint8Array, version: number, ecc: QrEcc): Uint8Array {
  const capacity = dataCodewords(version, ecc) * 8
  const bits: number[] = []
  const push = (value: number, length: number) => {
    for (let i = length - 1; i >= 0; i--) bits.push((value >>> i) & 1)
  }
  push(0b0100, 4)
  push(bytes.length, version <= 9 ? 8 : 16)
  for (const b of bytes) push(b, 8)
  // Terminator, then to a byte boundary, then the two pad bytes alternating.
  push(0, Math.min(4, capacity - bits.length))
  push(0, (8 - (bits.length % 8)) % 8)
  for (let pad = 0xec; bits.length < capacity; pad ^= 0xec ^ 0x11) push(pad, 8)
  const out = new Uint8Array(capacity / 8)
  for (let i = 0; i < bits.length; i++) out[i >>> 3] |= bits[i] << (7 - (i & 7))
  return out
}

// MARK: Reed–Solomon over GF(256), x^8 + x^4 + x^3 + x^2 + 1

function gfMultiply(x: number, y: number): number {
  let z = 0
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d)
    z ^= ((y >>> i) & 1) * x
  }
  return z
}

/** The generator polynomial of `degree`, highest term dropped (it is always 1). */
function rsDivisor(degree: number): Uint8Array {
  const result = new Uint8Array(degree)
  result[degree - 1] = 1
  let root = 1
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < degree; j++) {
      result[j] = gfMultiply(result[j], root)
      if (j + 1 < degree) result[j] ^= result[j + 1]
    }
    root = gfMultiply(root, 0x02)
  }
  return result
}

function rsRemainder(data: Uint8Array, divisor: Uint8Array): Uint8Array {
  const result = new Uint8Array(divisor.length)
  for (const b of data) {
    const factor = b ^ result[0]
    result.copyWithin(0, 1)
    result[result.length - 1] = 0
    for (let i = 0; i < result.length; i++) result[i] ^= gfMultiply(divisor[i], factor)
  }
  return result
}

/** Splits the data into blocks, appends each block's error correction, and interleaves. */
function withErrorCorrection(data: Uint8Array, version: number, ecc: QrEcc): Uint8Array {
  const blockCount = BLOCKS[ecc][version]
  const eccLength = ECC_PER_BLOCK[ecc][version]
  const raw = Math.floor(rawDataModules(version) / 8)
  const shortBlocks = blockCount - (raw % blockCount)
  const shortLength = Math.floor(raw / blockCount)
  const divisor = rsDivisor(eccLength)

  const blocks: Uint8Array[] = []
  for (let i = 0, k = 0; i < blockCount; i++) {
    const take = shortLength - eccLength + (i < shortBlocks ? 0 : 1)
    const chunk = data.subarray(k, k + take)
    k += take
    blocks.push(Uint8Array.from([...chunk, ...rsRemainder(chunk, divisor)]))
  }

  // Column by column across the blocks. A short block has one data byte fewer
  // than a long one, so it has no byte in that column.
  const out: number[] = []
  const dataLongest = shortLength - eccLength + 1
  for (let i = 0; i < dataLongest; i++) {
    for (const block of blocks) {
      const dataLength = block.length - eccLength
      if (i < dataLength) out.push(block[i])
    }
  }
  for (let i = 0; i < eccLength; i++) {
    for (const block of blocks) out.push(block[block.length - eccLength + i])
  }
  return Uint8Array.from(out)
}

// MARK: the symbol

function alignmentPositions(version: number): number[] {
  if (version === 1) return []
  const count = Math.floor(version / 7) + 2
  const size = 17 + 4 * version
  const step = version === 32 ? 26 : Math.ceil((version * 4 + 4) / (count * 2 - 2)) * 2
  const result = [6]
  for (let pos = size - 7; result.length < count; pos -= step) result.splice(1, 0, pos)
  return result
}

function maskBit(mask: number, x: number, y: number): boolean {
  switch (mask) {
    case 0:
      return (x + y) % 2 === 0
    case 1:
      return y % 2 === 0
    case 2:
      return x % 3 === 0
    case 3:
      return (x + y) % 3 === 0
    case 4:
      return (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0
    case 5:
      return ((x * y) % 2) + ((x * y) % 3) === 0
    case 6:
      return (((x * y) % 2) + ((x * y) % 3)) % 2 === 0
    default:
      return (((x + y) % 2) + ((x * y) % 3)) % 2 === 0
  }
}

function place(codewords: Uint8Array, version: number, ecc: QrEcc): QrCode {
  const size = 17 + 4 * version
  const dark = new Uint8Array(size * size)
  const fixed = new Uint8Array(size * size)
  const set = (x: number, y: number, on: boolean) => {
    dark[y * size + x] = on ? 1 : 0
    fixed[y * size + x] = 1
  }

  // Timing patterns first; the finders and alignments drawn next overwrite
  // the ends of them, as the standard's figures show.
  for (let i = 0; i < size; i++) {
    set(6, i, i % 2 === 0)
    set(i, 6, i % 2 === 0)
  }
  const finder = (cx: number, cy: number) => {
    for (let dy = -4; dy <= 4; dy++) {
      for (let dx = -4; dx <= 4; dx++) {
        const x = cx + dx
        const y = cy + dy
        if (x < 0 || y < 0 || x >= size || y >= size) continue
        const d = Math.max(Math.abs(dx), Math.abs(dy))
        set(x, y, d !== 2 && d !== 4)
      }
    }
  }
  finder(3, 3)
  finder(size - 4, 3)
  finder(3, size - 4)
  const aligns = alignmentPositions(version)
  const last = aligns.length - 1
  aligns.forEach((cx, i) => {
    aligns.forEach((cy, j) => {
      if ((i === 0 && j === 0) || (i === 0 && j === last) || (i === last && j === 0)) return
      for (let dy = -2; dy <= 2; dy++) {
        for (let dx = -2; dx <= 2; dx++) set(cx + dx, cy + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1)
      }
    })
  })
  // Reserve the format areas (drawn for real once the mask is chosen) and
  // write the version blocks, which do not depend on it.
  drawFormat(set, size, ecc, 0)
  if (version >= 7) {
    let rem = version
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25)
    const bits = (version << 12) | rem
    for (let i = 0; i < 18; i++) {
      const on = ((bits >>> i) & 1) === 1
      const a = size - 11 + (i % 3)
      const b = Math.floor(i / 3)
      set(a, b, on)
      set(b, a, on)
    }
  }

  // The codewords, two columns at a time from the right, snaking up and down
  // and stepping over the vertical timing pattern.
  let bit = 0
  const total = codewords.length * 8
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5
    const upward = ((right + 1) & 2) === 0
    for (let vert = 0; vert < size; vert++) {
      const y = upward ? size - 1 - vert : vert
      for (let j = 0; j < 2; j++) {
        const x = right - j
        if (fixed[y * size + x] || bit >= total) continue
        dark[y * size + x] = (codewords[bit >>> 3] >>> (7 - (bit & 7))) & 1
        bit++
      }
    }
  }

  // All eight masks, and the one the standard's penalty likes best.
  let best = 0
  let bestScore = Infinity
  let bestDark = dark
  for (let mask = 0; mask < 8; mask++) {
    const trial = dark.slice()
    const setTrial = (x: number, y: number, on: boolean) => {
      trial[y * size + x] = on ? 1 : 0
    }
    for (let y = 0; y < size; y++) {
      for (let x = 0; x < size; x++) {
        if (!fixed[y * size + x] && maskBit(mask, x, y)) trial[y * size + x] ^= 1
      }
    }
    drawFormat(setTrial, size, ecc, mask)
    const score = penalty(trial, size)
    if (score < bestScore) {
      best = mask
      bestScore = score
      bestDark = trial
    }
  }
  return { version, size, ecc, mask: best, dark: bestDark }
}

/** The fifteen format bits, both copies, and the one module that is always dark. */
function drawFormat(set: (x: number, y: number, on: boolean) => void, size: number, ecc: QrEcc, mask: number) {
  const data = (FORMAT_BITS[ecc] << 3) | mask
  let rem = data
  for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537)
  const bits = ((data << 10) | rem) ^ 0x5412
  const on = (i: number) => ((bits >>> i) & 1) === 1
  for (let i = 0; i <= 5; i++) set(8, i, on(i))
  set(8, 7, on(6))
  set(8, 8, on(7))
  set(7, 8, on(8))
  for (let i = 9; i < 15; i++) set(14 - i, 8, on(i))
  for (let i = 0; i < 8; i++) set(size - 1 - i, 8, on(i))
  for (let i = 8; i < 15; i++) set(8, size - 15 + i, on(i))
  set(8, size - 8, true)
}

/**
 * The standard's four penalties: long runs, 2×2 blocks, finder look-alikes,
 * and a dark share far from half. Only the choice of mask depends on this, and
 * every mask decodes, so it is about making the symbol easy to read rather
 * than making it correct.
 */
function penalty(dark: Uint8Array, size: number): number {
  let score = 0
  const at = (x: number, y: number) => dark[y * size + x]
  const finderLike = [1, 0, 1, 1, 1, 0, 1]
  for (let pass = 0; pass < 2; pass++) {
    for (let a = 0; a < size; a++) {
      const line: number[] = []
      for (let b = 0; b < size; b++) line.push(pass === 0 ? at(b, a) : at(a, b))
      let run = 1
      for (let b = 1; b <= size; b++) {
        if (b < size && line[b] === line[b - 1]) {
          run++
          continue
        }
        if (run >= 5) score += 3 + (run - 5)
        run = 1
      }
      // A finder look-alike with four light modules on either side; outside
      // the symbol counts as light, since the quiet zone is.
      for (let b = 0; b + 7 <= size; b++) {
        if (!finderLike.every((v, k) => line[b + k] === v)) continue
        const lightBefore = [1, 2, 3, 4].every((k) => b - k < 0 || line[b - k] === 0)
        const lightAfter = [0, 1, 2, 3].every((k) => b + 7 + k >= size || line[b + 7 + k] === 0)
        if (lightBefore || lightAfter) score += 40
      }
    }
  }
  for (let y = 0; y + 1 < size; y++) {
    for (let x = 0; x + 1 < size; x++) {
      const c = at(x, y)
      if (c === at(x + 1, y) && c === at(x, y + 1) && c === at(x + 1, y + 1)) score += 3
    }
  }
  let darkCount = 0
  for (const v of dark) darkCount += v
  const total = size * size
  score += (Math.ceil(Math.abs(darkCount * 20 - total * 10) / total) - 1) * 10
  return score
}

// MARK: drawing

/** The quiet zone the standard asks for: four modules of light on every side. */
export const QR_QUIET = 4

/**
 * One SVG path for every dark module, in module units with the quiet zone
 * already added, so a `viewBox` of `0 0 (size + 8) (size + 8)` is the whole
 * symbol. One horizontal run is one subpath, which keeps a 65-module code to a
 * few hundred commands instead of two thousand rectangles.
 */
export function qrPath(code: QrCode): string {
  const parts: string[] = []
  for (let y = 0; y < code.size; y++) {
    let x = 0
    while (x < code.size) {
      if (!code.dark[y * code.size + x]) {
        x++
        continue
      }
      const start = x
      while (x < code.size && code.dark[y * code.size + x]) x++
      parts.push(`M${start + QR_QUIET} ${y + QR_QUIET}h${x - start}v1h${start - x}z`)
    }
  }
  return parts.join("")
}
