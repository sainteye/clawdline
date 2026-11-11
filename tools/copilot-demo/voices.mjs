#!/usr/bin/env node
// Speaks the demo's lines with OpenAI's text-to-speech (docs/copilot-demo.md).
//
//   OPENAI_API_KEY=… node tools/copilot-demo/voices.mjs --lang en|zh-Hant [--force]
//
// Writes voices/<lang>/<line>.ogg (Opus, silence trimmed) for every line in
// story.mjs that has no file yet; --force speaks them all again. This is a
// paid call, one request per line written; the committed voices make it
// unnecessary unless a line changes.
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { story, LANGS } from './story.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const i = process.argv.indexOf('--lang'), lang = i > 0 ? process.argv[i + 1] : ''
const force = process.argv.includes('--force')
if (!LANGS.includes(lang)) { console.error(`usage: voices.mjs --lang ${LANGS.join('|')} [--force]`); process.exit(2) }
const key = process.env.OPENAI_API_KEY
if (!key) { console.error('OPENAI_API_KEY is not set'); process.exit(2) }
const S = story(lang), out = join(here, 'voices', lang), work = mkdtempSync(join(tmpdir(), 'clawdline-copilot-voices-'))
mkdirSync(out, { recursive: true })
let spoken = 0
try {
  for (const [id, [voice, input]] of Object.entries(S.lines)) {
    const file = join(out, id + '.ogg')
    if (existsSync(file) && !force) continue
    const res = await fetch('https://api.openai.com/v1/audio/speech', {
      method: 'POST', headers: { Authorization: 'Bearer ' + key, 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: 'gpt-4o-mini-tts', voice, input, instructions: S.words.voiceStyle[voice], response_format: 'wav' }),
    })
    if (!res.ok) throw new Error(`${id}: HTTP ${res.status} ${await res.text()}`)
    const raw = join(work, id + '.wav'); writeFileSync(raw, Buffer.from(await res.arrayBuffer()))
    const trim = 'silenceremove=start_periods=1:start_threshold=-45dB,areverse,silenceremove=start_periods=1:start_threshold=-45dB,areverse'
    execFileSync('ffmpeg', ['-v', 'error', '-y', '-i', raw, '-af', trim, '-c:a', 'libopus', '-b:a', '48k', '-ar', '48000', '-ac', '1', file])
    spoken++; console.log('spoke ' + id)
  }
} finally { rmSync(work, { recursive: true, force: true }) }
console.log(`voices: ${spoken} line(s) spoken in ${lang} (${spoken} paid request(s)); ${Object.keys(S.lines).length} lines in all`)
