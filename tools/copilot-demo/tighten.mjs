#!/usr/bin/env node
// Shortens the waits in a GPT-Live demo film (docs/copilot-demo.md). record.mjs
// --live runs it on every take; it also runs alone on a film already made:
//
//   node tools/copilot-demo/tighten.mjs <film.mp4>
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
/**
 * Shortens every stretch in which neither voice is heard to TIGHT_KEEP s
 * (half at each end), when it is longer than TIGHT_GAP s. The opening keeps
 * its first TIGHT_OPENING s, where the call is placed, and the close keeps
 * TIGHT_CLOSE s after the last word, where it is hung up. `actions` are the
 * film's seconds at which something visible is done; the moment before and
 * after each is kept. Returns the cut.
 */
export function tighten(file, untrimmed = '', actions = []) {
  const TIGHT_GAP = 1.2, TIGHT_KEEP = 0.6, TIGHT_OPENING = 2.6, TIGHT_CLOSE = 1.6, STEP = 0.05, RATE = 8000
  const raw = execFileSync('ffmpeg', ['-v', 'error', '-i', file, '-ac', '1', '-ar', String(RATE), '-f', 's16le', '-'], { maxBuffer: 1 << 28 })
  const n = RATE * STEP, quiet = []
  for (let i = 0; (i + n) * 2 <= raw.length; i += n) {
    let sum = 0; for (let j = 0; j < n; j++) { const v = raw.readInt16LE((i + j) * 2); sum += v * v }
    // A visible action (a click, with the pointer moving to it) is kept like speech.
    const t = quiet.length * STEP
    quiet.push(Math.sqrt(sum / n) < 120 && !actions.some((at) => t >= at - 1.2 && t <= at + 0.6))
  }
  const keep = []; let from = 0, i = 0, removed = 0
  while (i < quiet.length) {
    if (!quiet[i]) { i++; continue }
    let j = i; while (j < quiet.length && quiet[j]) j++
    const a = i * STEP, b = j * STEP
    if (j === quiet.length) { keep.push([from, Math.min(b, a + TIGHT_CLOSE)]); removed += Math.max(0, b - a - TIGHT_CLOSE); from = -1; break }
    const head = i === 0 ? TIGHT_OPENING : TIGHT_KEEP / 2
    if (b - a > Math.max(TIGHT_GAP, head + TIGHT_KEEP / 2)) { keep.push([from, a + head]); from = b - TIGHT_KEEP / 2; removed += b - a - head - TIGHT_KEEP / 2 }
    i = j
  }
  if (from >= 0) keep.push([from, quiet.length * STEP + 1])
  if (keep.length < 2) return { removed: 0, segments: 1 }
  // Each kept stretch is cut from picture and sound alike and the pieces are joined,
  // so the two stay together (select/aselect cut only the picture).
  const parts = keep.map(([a, b], k) => `[0:v]trim=${a.toFixed(3)}:${b.toFixed(3)},setpts=PTS-STARTPTS[v${k}];[0:a]atrim=${a.toFixed(3)}:${b.toFixed(3)},asetpts=PTS-STARTPTS[a${k}]`).join(';')
  const graph = `${parts};${keep.map((_, k) => `[v${k}][a${k}]`).join('')}concat=n=${keep.length}:v=1:a=1[v][a]`
  const tight = file.replace(/\.mp4$/, '') + '.tight.mp4'
  execFileSync('ffmpeg', ['-v', 'error', '-y', '-i', file, '-filter_complex', graph, '-map', '[v]', '-map', '[a]',
    '-c:v', 'libx264', '-preset', 'slow', '-crf', '17', '-pix_fmt', 'yuv420p', '-r', '30', '-c:a', 'aac', '-b:a', '256k', '-movflags', '+faststart', tight], { stdio: 'inherit' })
  if (untrimmed) execFileSync('cp', [file, untrimmed])
  execFileSync('mv', [tight, file])
  return { removed: Math.round(removed * 10) / 10, segments: keep.length }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const file = process.argv[2]
  if (!file) { console.error('usage: tighten.mjs <film.mp4>'); process.exit(2) }
  const cut = tighten(resolve(file))
  console.log(`tighten: ${cut.removed} s of waiting cut in ${cut.segments - 1} places`)
}
