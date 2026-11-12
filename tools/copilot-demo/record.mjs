#!/usr/bin/env node
// Records the voice copilot demo from the built console (docs/copilot-demo.md).
//
//   node tools/copilot-demo/record.mjs --lang en|zh-Hant --dist web/console/dist --out <file.mp4> [--phone] [--keep <dir>]
//
// --phone records a 390 x 844 phone with touch, as a portrait 1080 x 2338 film.
//
// A stand-in daemon serves the built console and story.mjs; call-stand-in.js
// stands in for the microphone and OpenAI's Realtime peer. Chrome runs
// headless through the DevTools protocol; its screencast frames keep their
// own timestamps, become a constant 30 fps H.264 video, and the voices are
// laid under it at the moments the page started them.
import { spawn, execFileSync } from 'node:child_process'
import { createServer } from 'node:http'
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, extname, join, normalize, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { IDS, story, steps, LANGS } from './story.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const arg = (name, fallback) => { const i = process.argv.indexOf('--' + name); return i > 0 ? process.argv[i + 1] : fallback }
const LANG = arg('lang', 'en'), DIST = resolve(arg('dist', join(here, '../../web/console/dist'))), OUT = arg('out')
const KEEP = arg('keep')
const PHONE = process.argv.includes('--phone')
// The page as it is drawn, the screencast's largest frame, and the film's size.
const VIEW = PHONE ? { width: 390, height: 844, deviceScaleFactor: 3, mobile: true } : { width: 1600, height: 900, deviceScaleFactor: 1.2, mobile: false }
const FRAME = PHONE ? { maxWidth: 1170, maxHeight: 2532 } : { maxWidth: 1920, maxHeight: 1080 }
const FILM = PHONE ? '1080:2338' : '1920:1080'
const CHROME = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
if (!OUT || !LANGS.includes(LANG)) { console.error(`usage: record.mjs --lang ${LANGS.join('|')} --dist <dist> --out <file.mp4> [--phone] [--keep <dir>]`); process.exit(2) }
for (const need of [join(DIST, 'index.html'), CHROME]) if (!existsSync(need)) { console.error('missing: ' + need); process.exit(2) }
const VOICES = join(here, 'voices', LANG)
const S = story(LANG)
for (const id of Object.keys(S.lines)) if (!existsSync(join(VOICES, id + '.ogg'))) { console.error(`missing voice ${LANG}/${id}.ogg: run voices.mjs --lang ${LANG}`); process.exit(2) }

// ---------- the stand-in daemon ----------
const TYPES = { '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.json': 'application/json', '.png': 'image/png', '.webp': 'image/webp', '.ico': 'image/x-icon', '.woff2': 'font/woff2', '.ogg': 'audio/ogg' }
const json = (res, status, v) => { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(v)) }
const snapshot = () => ({ at: Date.now(), scan: { complete: true, completed: { complete: true, sequence: 1 }, emptyAuthoritative: true, epoch: 1, generation: 1, provenance: 'fixture' }, sessions: S.sessions() })
const streams = new Set()
const push = () => { for (const s of streams) s.write('event: sessions\ndata: ' + JSON.stringify(snapshot()) + '\n\n') }
const body = (req) => new Promise((ok) => { let s = ''; req.on('data', (c) => s += c); req.on('end', () => { try { ok(JSON.parse(s || '{}')) } catch { ok({}) } }) })
function page() {
  const html = readFileSync(join(DIST, 'index.html'), 'utf8')
  const words = JSON.parse(readFileSync(join(DIST, 'strings', 'zh-Hant.json'), 'utf8'))
  words.lang = 'zh-Hant'; words.dir = 'ltr'
  return html.replace('<!-- clawdline:strings -->', `<script>localStorage.setItem('ui_language',${JSON.stringify(LANG)});window.__strings=` + JSON.stringify(words).replaceAll('</', '<\\/') + '</script>').replace('<!-- clawdline:cloud -->', '')
}
const server = createServer(async (req, res) => {
  const url = new URL(req.url ?? '/', 'http://demo'), path = url.pathname
  if (path.startsWith('/demo-voices/')) { res.writeHead(200, { 'content-type': 'audio/ogg' }); return res.end(readFileSync(join(VOICES, path.slice(13).replace(/[^\w.-]/g, '')))) }
  if (path === '/v1/strings') { const f = join(DIST, 'catalogs', (url.searchParams.get('lang') || LANG) + '.json'); return existsSync(f) ? json(res, 200, JSON.parse(readFileSync(f, 'utf8'))) : json(res, 404, { error: 'not_found' }) }
  if (path === '/v1/health') return json(res, 200, { ok: true })
  if (path === '/v1/sessions') return json(res, 200, snapshot())
  if (path === '/v1/events') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
    res.write('event: sessions\ndata: ' + JSON.stringify(snapshot()) + '\n\n'); streams.add(res); req.on('close', () => streams.delete(res)); return
  }
  if (path === '/v1/transcript') { const id = url.searchParams.get('session') ?? ''; return json(res, 200, { entries: S.transcript(id), evidence: 'process', id, signature: 'demo-' + S.state.releaseSent }) }
  const send = /^\/v1\/sessions\/([^/]+)\/send$/.exec(path)
  if (send && req.method === 'POST') {
    const b = await body(req)
    if (decodeURIComponent(send[1]) !== IDS.release) return json(res, 404, { error: { code: 'not_found' } })
    S.state.releaseSent = true; S.state.sentText = String(b.text || ''); push()
    return json(res, 200, { ok: true })
  }
  const about = /^\/v1\/sessions\/([^/]+)\/(info|git)$/.exec(path)
  if (about) {
    const row = S.sessions().find((s) => s.id === decodeURIComponent(about[1])); if (!row) return json(res, 404, { error: 'not_found' })
    if (about[2] === 'git') return json(res, 200, { git: { ahead: 0, behind: 0, branch: row.id === IDS.release ? 'release/2.4' : 'main', clean: true, head: '0123456789abcdef', files: [] } })
    return json(res, 200, { info: { session: { id: row.id, title: row.label, assistant: row.assistant, namingAssistant: row.assistant, sessionId: row.sessionId, cwd: row.cwd, model: row.assistant === 'codex' ? 'gpt-5.5' : 'claude-opus-5-5' },
      models: [{ id: 'claude-opus-5-5', name: 'Opus 5.5' }], context: { usedPercent: 22 }, deploy: [], links: [] } })
  }
  if (path === '/v1/voice/copilot-session') return json(res, 200, { client_secret: 'ek_demo', model: 'gpt-realtime-mini', eagerness: 'low', voice: 'marin', speed: '1.0', brief: true, briefing: true })
  if (path === '/v1/voice/copilot-briefing') return json(res, 200, { briefing: S.briefing })
  if (path === '/v1/personas') return json(res, 200, { license: 'MIT', personas: [] })
  if (path === '/v1/places') return json(res, 200, { places: S.places, assistants: [{ id: 'claude', label: 'Claude' }, { id: 'codex', label: 'Codex' }] })
  if (path.startsWith('/v1/work/v2/human-interventions/')) return json(res, 200, { ok: true, rows: [], pruned_resolved: 0 })
  if (path.startsWith('/v1/work/v2/session-todos/')) return json(res, 200, { ok: true, assigned_items: [], recent_items: [], direct_todos: [], open_decisions: [], decisions_error: null, truncated: false })
  if (path === '/v1/work/v2/items') return json(res, 200, { ok: true, rows: [], counts: {}, truncated: false })
  if (path === '/v1/work/v2/proposals') return json(res, 200, { rows: [], truncated: false })
  if (path === '/v1/work/decisions') return json(res, 200, { counts: {}, rows: [], next_cursor: null })
  if (path === '/v1/orchestrator/tasks') return json(res, 200, { at: Date.now(), tasks: [] })
  if (path === '/v1/orchestrator/schedules') return json(res, 200, { at: Date.now(), schedules: [] })
  if (path.startsWith('/v1/')) return json(res, 404, { error: 'not_found', detail: path })
  if (path === '/') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(page()) }
  const file = normalize(join(DIST, path))
  if (!file.startsWith(DIST + '/') || !existsSync(file) || !statSync(file).isFile()) return json(res, 404, {})
  res.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' }); res.end(readFileSync(file))
})
await new Promise((ok) => server.listen(0, '127.0.0.1', ok))
const origin = 'http://127.0.0.1:' + server.address().port

// ---------- Chrome through the DevTools protocol ----------
const profile = mkdtempSync(join(tmpdir(), 'clawdline-copilot-demo-'))
const chrome = spawn(CHROME, ['--headless=new', '--remote-debugging-port=0', '--user-data-dir=' + profile, '--no-first-run', '--no-default-browser-check',
  '--autoplay-policy=no-user-gesture-required', '--hide-scrollbars', `--window-size=${VIEW.width},${VIEW.height}`, 'about:blank'])
const endpoint = await new Promise((ok, fail) => {
  let said = ''; const timer = setTimeout(() => fail(new Error('Chrome did not start: ' + said)), 20000)
  chrome.stderr.on('data', (c) => { said += c; const m = /DevTools listening on (ws:\/\/\S+)/.exec(said); if (m) { clearTimeout(timer); ok(m[1]) } })
})
const ws = new WebSocket(endpoint)
await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error('Chrome unreachable')) })
let seq = 0; const pending = new Map(); const listeners = []
ws.onmessage = (ev) => {
  const msg = JSON.parse(String(ev.data))
  if (msg.id === undefined) { for (const l of listeners) l(msg); return }
  const p = pending.get(msg.id); pending.delete(msg.id)
  if (msg.error) p?.fail(new Error(msg.error.message)); else p?.ok(msg.result)
}
const { targetId } = await cdp('Target.createTarget', { url: 'about:blank' })
const { sessionId } = await cdp('Target.attachToTarget', { targetId, flatten: true })
function cdp(method, params = {}, session, ms = 30000) {
  const id = ++seq; ws.send(JSON.stringify({ id, method, params, sessionId: session }))
  return new Promise((ok, fail) => { const t = setTimeout(() => fail(new Error(method + ' timed out')), ms); pending.set(id, { ok: (v) => { clearTimeout(t); ok(v) }, fail: (e) => { clearTimeout(t); fail(e) } }) })
}
const send = (method, params, ms) => cdp(method, params, sessionId, ms)
const run = async (expression, ms) => {
  const { result, exceptionDetails } = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, ms)
  if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
  return result.value
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (expression, ms = 30000) => { const end = Date.now() + ms; while (!(await run(expression))) { if (Date.now() > end) throw new Error('never reached: ' + expression); await sleep(50) } }

const CURSOR = `addEventListener('DOMContentLoaded', () => {
  const c = document.createElement('div')
  c.innerHTML = '<svg width="22" height="28" viewBox="0 0 22 28"><path d="M2 2 L2 22 L7.5 17 L11 25.5 L14.5 24 L11 15.8 L18.5 15.8 Z" fill="#fff" stroke="#111" stroke-width="1.6" stroke-linejoin="round"/></svg>'
  Object.assign(c.style, { position: 'fixed', left: '0', top: '0', zIndex: 2147483647, pointerEvents: 'none', transform: 'translate(-100px,-100px)', filter: 'drop-shadow(0 2px 3px rgba(0,0,0,.45))' })
  document.body.appendChild(c)
  addEventListener('mousemove', (e) => { c.style.transform = 'translate(' + (e.clientX - 2) + 'px,' + (e.clientY - 2) + 'px)' }, true)
  addEventListener('mousedown', () => { c.style.scale = '0.85' }, true); addEventListener('mouseup', () => { c.style.scale = '1' }, true)
})`
// On a phone a finger has no pointer: each tap shows a ring where it landed.
const TAPS = `addEventListener('DOMContentLoaded', () => {
  addEventListener('touchstart', (e) => {
    const t = e.touches[0]; if (!t) return
    const r = document.createElement('div')
    Object.assign(r.style, { position: 'fixed', left: (t.clientX - 22) + 'px', top: (t.clientY - 22) + 'px', width: '44px', height: '44px', borderRadius: '50%',
      background: 'rgba(255,255,255,0.35)', border: '2px solid rgba(255,255,255,0.85)', zIndex: 2147483647, pointerEvents: 'none',
      transition: 'transform .45s ease-out, opacity .45s ease-out' })
    document.body.appendChild(r)
    requestAnimationFrame(() => { r.style.transform = 'scale(1.6)'; r.style.opacity = '0' })
    setTimeout(() => r.remove(), 600)
  }, true)
})`
await send('Page.enable'); await send('Runtime.enable')
await send('Emulation.setDeviceMetricsOverride', VIEW)
if (PHONE) await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 })
await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'dark' }] })
// No Web Push in the recording's browser, so the sidebar's offer to set it up stays hidden.
const NO_PUSH = 'try { delete window.PushManager; delete ServiceWorkerRegistration.prototype.pushManager } catch {}'
// On a phone the console is opened from the Home Screen, where it runs standalone (iOS asks for that before it offers notifications).
const HOME_SCREEN = "try { Object.defineProperty(navigator, 'standalone', { get: () => true }) } catch {}"
for (const source of [...(PHONE ? [HOME_SCREEN] : []), NO_PUSH, 'window.__DEMO_LINES=' + JSON.stringify(S.lines) + ';', readFileSync(join(here, 'call-stand-in.js'), 'utf8'), PHONE ? TAPS : CURSOR])
  await send('Page.addScriptToEvaluateOnNewDocument', { source })
await send('Page.navigate', { url: origin + '/' })
await until(`!!document.querySelector('#copilot-call') && document.querySelectorAll('li.row').length >= 6`)
await sleep(1500)
if (!(await run(`document.documentElement.lang.startsWith(${JSON.stringify(LANG.slice(0, 2))})`))) throw new Error('the console is not in ' + LANG)

let mouse = { x: 1200, y: 600 }
async function moveTo(x, y, stepsN = 28) {
  if (PHONE) return
  const from = mouse
  for (let i = 1; i <= stepsN; i++) {
    const k = i / stepsN, e = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2
    mouse = { x: from.x + (x - from.x) * e, y: from.y + (y - from.y) * e }
    await send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: mouse.x, y: mouse.y }); await sleep(16)
  }
}
const centre = (sel) => run(`(() => { const r = document.querySelector(${JSON.stringify(sel)}).getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 } })()`)
async function click(sel) {
  const { x, y } = await centre(sel)
  // A tap lands on whatever is drawn there; say so when it is not the control meant.
  const hit = await run(`(() => { const want = document.querySelector(${JSON.stringify(sel)}), at = document.elementFromPoint(${x}, ${y}); return !at ? 'nothing' : want.contains(at) ? '' : (at.className || at.tagName) + ' over ' + ${JSON.stringify(sel)} + ' at ' + ${Math.round(x)} + ',' + ${Math.round(y)} })()`)
  if (hit) throw new Error('the tap would land on ' + hit)
  if (PHONE) {
    await sleep(350)
    await send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] }); await sleep(90)
    await send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    return
  }
  await moveTo(x, y); await sleep(250)
  await send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', clickCount: 1 }); await sleep(90)
  await send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', clickCount: 1 })
}
/** Drags with a finger from the centre of `sel` to (x, y). */
async function drag(sel, x, y) {
  const from = await centre(sel), n = 24
  await sleep(300)
  await send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [from] })
  for (let i = 1; i <= n; i++) {
    const k = i / n, e = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2
    await send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: from.x + (x - from.x) * e, y: from.y + (y - from.y) * e }] }); await sleep(20)
  }
  await send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
}
const wheel = (dy) => send('Input.dispatchMouseEvent', { type: 'mouseWheel', x: mouse.x, y: mouse.y, deltaX: 0, deltaY: dy })
/** Scrolls the page's tallest scrolling area to `to` (0 is the top, 1 the bottom) over `ms`, as a reader would. */
const glide = (to, ms) => run(`new Promise((done) => {
  const box = [...document.querySelectorAll('*')].filter((e) => e.scrollHeight - e.clientHeight > 40 && getComputedStyle(e).overflowY.match(/auto|scroll/) && e.getClientRects().length)
    .sort((a, b) => (b.scrollHeight - b.clientHeight) - (a.scrollHeight - a.clientHeight))[0]
  if (!box) return done(false)
  const from = box.scrollTop, end = ${to} * (box.scrollHeight - box.clientHeight), t0 = performance.now()
  const step = (now) => { const k = Math.min(1, (now - t0) / ${ms}); box.scrollTop = from + (end - from) * (k < .5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2); k < 1 ? requestAnimationFrame(step) : done(true) }
  requestAnimationFrame(step)
})`, ms + 10000)

// ---------- the take ----------
const frames = []
listeners.push((msg) => {
  if (msg.method !== 'Page.screencastFrame' || msg.sessionId !== sessionId) return
  frames.push({ ts: msg.params.metadata.timestamp, data: msg.params.data })
  send('Page.screencastFrameAck', { sessionId: msg.params.sessionId }).catch(() => {})
})
await send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: mouse.x, y: mouse.y })
await send('Page.startScreencast', { format: 'jpeg', quality: 92, ...FRAME, everyNthFrame: 1 })
await sleep(1800)
// The whole call is one promise in the page; it takes a minute and a half.
const call = run(`window.__demo.run(${JSON.stringify(steps(S.send, S.words.names.release))})`, 300000)
await sleep(300)
await click('#copilot-call')
await until(`window.__demo.stage === 'hold:minimize'`, 120000)
await sleep(400)
await click('.realtime-minimize-btn')
await moveTo(1250, 560, 25)
// On a phone the bubble rests mid-height over the conversation; move it down beside the composer.
if (PHONE) { await until(`!!document.querySelector('.copilot-bubble-body')`, 5000); await sleep(600); await drag('.copilot-bubble-body', VIEW.width - 40, VIEW.height - 235) }
await run(`window.__demo.release = 'minimize'`)
// While the copilot tells Search rebuild's story, read down its conversation.
await until(`window.__demo.tools.length >= 2`, 120000)
if (PHONE) {
  await glide(0, 1500); await sleep(1500); await glide(1, 15000)
} else {
  await moveTo(1150, 520, 20)
  for (let i = 0; i < 40; i++) { await wheel(-90); await sleep(40) }
  await sleep(1500)
  for (let i = 0; i < 150; i++) { await wheel(14); await sleep(100) }
}
await call
await sleep(2200)
await click('.copilot-bubble-hangup')
await sleep(2200)
await send('Page.stopScreencast')
const clips = await run('window.__demo.clips')
const tools = await run('window.__demo.tools.map((t) => ({ name: t.name, output: t.output }))')
ws.close(); chrome.kill(); server.close(); for (const s of streams) s.end()
await sleep(500); rmSync(profile, { recursive: true, force: true, maxRetries: 5 })

// ---------- frames, video and voices ----------
const work = KEEP ? resolve(KEEP) : mkdtempSync(join(tmpdir(), 'clawdline-copilot-demo-take-'))
mkdirSync(join(work, 'frames'), { recursive: true })
let list = ''
frames.forEach((f, i) => {
  const name = `f${String(i).padStart(5, '0')}.jpg`; writeFileSync(join(work, 'frames', name), Buffer.from(f.data, 'base64'))
  list += `file 'frames/${name}'\nduration ${((frames[i + 1]?.ts ?? f.ts + 1 / 30) - f.ts).toFixed(4)}\n`
})
list += `file 'frames/f${String(frames.length - 1).padStart(5, '0')}.jpg'\n`
writeFileSync(join(work, 'frames.txt'), list)
const ff = (...a) => execFileSync('ffmpeg', ['-v', 'error', '-y', ...a], { cwd: work, stdio: 'inherit' })
ff('-f', 'concat', '-safe', '0', '-i', 'frames.txt', '-vf', `fps=30,scale=${FILM}:flags=lanczos,format=yuv420p`, '-c:v', 'libx264', '-preset', 'slow', '-crf', '17', 'video.mp4')
const duration = Number(execFileSync('ffprobe', ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', join(work, 'video.mp4')]).toString())
const first = frames[0].ts * 1000
const inputs = clips.flatMap((c) => ['-i', join(VOICES, c.id + '.ogg')])
const graph = clips.map((c, i) => `[${i + 1}]adelay=${Math.max(0, Math.round(c.wallMs - first))}:all=1[a${i}]`).join(';') + ';' +
  clips.map((_, i) => `[a${i}]`).join('') + `amix=inputs=${clips.length}:normalize=0,pan=stereo|c0=c0|c1=c0,apad=whole_dur=${duration.toFixed(3)},alimiter=limit=0.95[aout]`
ff('-i', 'video.mp4', ...inputs, '-filter_complex', graph, '-map', '0:v', '-map', '[aout]', '-c:v', 'copy', '-c:a', 'aac', '-b:a', '256k', '-ar', '48000', '-t', duration.toFixed(3), '-movflags', '+faststart', resolve(OUT))
writeFileSync(join(work, 'take.json'), JSON.stringify({ lang: LANG, phone: PHONE, frames: frames.length, duration, clips: clips.map((c) => ({ id: c.id, at: (c.wallMs - first) / 1000 })), tools }, null, 2))
if (!KEEP) rmSync(work, { recursive: true, force: true })

const ran = tools.map((t) => t.name).join(', ')
const expected = ['open_session', 'get_session_recap', 'open_session', 'send_message_to_session']
if (ran !== expected.join(', ') || !S.state.releaseSent) { console.error(`the call did not run as written: ${ran}; sent=${S.state.releaseSent}`); process.exit(1) }
console.log(`copilot-demo: ${resolve(OUT)} (${LANG}${PHONE ? ', phone' : ''}, ${duration.toFixed(1)} s, ${frames.length} frames; tools: ${ran})`)
process.exit(0)
