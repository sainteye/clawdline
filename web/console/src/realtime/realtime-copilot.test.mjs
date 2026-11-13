import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { build } from 'esbuild'

const { outputFiles } = await build({
  entryPoints: [fileURLToPath(new URL('./realtime-copilot.ts', import.meta.url))],
  bundle: true, platform: 'node', format: 'cjs', packages: 'external', write: false,
})
const compiled = { exports: {} }
new Function('require', 'module', 'exports', outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { RealtimeCopilotClient } = compiled.exports
const origin = { id: '%7', label: 'Prepare screenshots' }
const row = { ...origin, assistant: 'codex', state: 'idle', sessionId: 'conversation-seven' }
const tick = () => new Promise(resolve => setImmediate(resolve))

function fixture(managed, target = origin) {
  const sent = [], reads = []
  let release
  const held = new Promise(resolve => { release = resolve })
  const c = new RealtimeCopilotClient()
  c.dc = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} }
  c.call = { memory: '', snapshot: '', options: { workEnabled: managed, origin: target } }
  c.focus = target?.id
  c.snapshot = Promise.resolve('')
  c.addLog = () => {}
  c.toolDeps = () => ({
    sleep: async () => {},
    decide: () => assert.fail('a clicked Session must not use a model to select its target'),
    fetch: async (path, init) => {
      reads.push({ path, init })
      assert.ok(!init?.method || init.method === 'GET', 'opening cannot submit a planner request or mutation')
      if (path === '/v1/sessions') return Response.json({ sessions: [row] })
      if (path.startsWith('/v1/transcript?')) {
        await held
        return Response.json({ entries: [
          { role: 'user', text: 'Capture six screenshots.' },
          { role: 'assistant', text: 'All six screenshots are captured. Please approve the layout.' },
        ] })
      }
      if (path.includes('/human-interventions/')) return Response.json({ rows: [] })
      assert.fail(`unexpected opening request: ${path}`)
    },
  })
  return { c, sent, reads, release, responses: () => sent.filter(e => e.type === 'response.create') }
}

test('connecting without a Session waits silently in direct and managed calls', async () => {
  globalThis.document = { documentElement: { lang: 'en' } }
  globalThis.location = { hash: '' }
  for (const managed of [false, true]) {
    const f = fixture(managed, null)
    f.c.memory = [{ role: 'user', text: 'An earlier request.', at: Math.floor(Date.now()/1000) }]
    await f.c.sendSessionConfiguration()
    assert.equal(f.responses().length, 0)
    assert.equal(f.reads.length, 0)
    assert.equal(f.sent.filter(e => e.type === 'session.update').length, 1)
  }
})

for (const managed of [false, true]) test(`Session opening waits for real evidence and speaks once (managed=${managed})`, async () => {
  const f = fixture(managed)
  const configured = f.c.sendSessionConfiguration()
  await tick()
  assert.equal(f.responses().length, 0, 'no greeting or planning-status narration before content')
  assert.ok(f.reads.some(r => r.path.startsWith('/v1/transcript?')))
  assert.ok(f.reads.some(r => r.path.includes('/human-interventions/')), 'notes start while the transcript is pending')
  f.release()
  await configured
  assert.equal(f.responses().length, 1)
  const response = f.responses()[0].response
  assert.equal(response.tool_choice, 'none')
  assert.match(response.instructions, /All six screenshots are captured/)
  assert.match(response.instructions, /two sentences/)
  assert.match(response.instructions, /no greeting/i)
  assert.equal(f.reads.filter(r => r.path === '/v1/sessions').length, 1)
})

test('speaking before preparation finishes supersedes the automatic opening', async () => {
  const f = fixture(true)
  const configured = f.c.sendSessionConfiguration()
  await tick()
  await f.c.handleRealtimeEvent({ type: 'input_audio_buffer.speech_started' })
  f.release()
  await configured
  assert.equal(f.responses().length, 0)
})

test('an old opening cannot speak into a replacement call', async () => {
  const f = fixture(true)
  const configured = f.c.sendSessionConfiguration()
  await tick()
  f.c.attempt++
  f.c.dc = { readyState: 'open', send: text => f.sent.push(JSON.parse(text)) }
  f.release()
  await configured
  assert.equal(f.responses().length, 0)
})

test('turning an existing managed call to a Session also reads before speaking', async () => {
  const f = fixture(true, null)
  f.c.turnTo(origin)
  await tick()
  assert.equal(f.responses().length, 0)
  assert.ok(f.reads.some(r => r.path.startsWith('/v1/transcript?')))
  f.release()
  await tick()
  assert.equal(f.responses().length, 1)
  assert.equal(f.responses()[0].response.tool_choice, 'none')
})

test('a queued opening is discarded when the person speaks while other audio plays', async () => {
  const f = fixture(true)
  f.c.narrator.event('output_audio_buffer.started')
  const configured = f.c.sendSessionConfiguration()
  f.release()
  await configured
  assert.equal(f.responses().length, 0)
  await f.c.handleRealtimeEvent({ type: 'input_audio_buffer.speech_started' })
  f.c.narrator.event('output_audio_buffer.stopped')
  f.c.narrator.received()
  assert.equal(f.responses().length, 0)
})

for (const managed of [false, true]) test(`the real start path creates no input or planner turn before speech (managed=${managed})`, async () => {
  const oldFetch = globalThis.fetch
  const oldNavigator = Object.getOwnPropertyDescriptor(globalThis, 'navigator')
  const oldDocument = globalThis.document, oldWindow = globalThis.window, oldPeer = globalThis.RTCPeerConnection
  const sent = [], requests = []
  const stream = { getAudioTracks: () => [{ stop() {} }], getTracks: () => [{ stop() {} }] }
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { userAgent: 'synthetic-client', mediaDevices: { getUserMedia: async () => stream } } })
  globalThis.document = { documentElement: { lang: 'en' }, querySelector: () => null, dispatchEvent: () => {}, createElement: () => ({}) }
  globalThis.window = { addEventListener() {}, removeEventListener() {} }
  globalThis.RTCPeerConnection = class {
    addTrack() {}
    createDataChannel() { return this.channel = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} } }
    async createOffer() { return { type: 'offer', sdp: 'synthetic-offer' } }
    async setLocalDescription() {}
    async setRemoteDescription() {}
    close() {}
  }
  globalThis.fetch = async (path, init) => {
    requests.push({ path, init })
    if (path === '/v1/settings/copilot') return Response.json({ work_enabled: managed })
    if (path === '/v1/voice/copilot-session') return Response.json({ client_secret: 'fixture-not-a-key', work_enabled: managed })
    // This replaces the provider transport. No request leaves this process.
    if (String(path).startsWith('https://api.openai.com/v1/realtime/calls')) return new Response('synthetic-answer')
    if (path === '/v1/sessions') return Response.json({ sessions: [row] })
    if (path === '/v1/voice/copilot-work') {
      assert.equal(init?.method, undefined, 'startup must not create input or plan')
      return Response.json({ state: { id: 'workspace', revision: 1, inputs: [], tasks: [], reply: 'Old history must not be read aloud.' } })
    }
    if (path.startsWith('/v1/transcript?')) return Response.json({ entries: [{ role: 'assistant', text: 'All six screenshots are captured.' }] })
    if (path.includes('/human-interventions/')) return Response.json({ rows: [] })
    if (path === '/v1/voice/copilot-journal') return Response.json({ ok: true })
    assert.fail(`unexpected request: ${path}`)
  }
  const c = new RealtimeCopilotClient()
  c.meter = { open: () => null, ring: () => () => {}, attachInput: () => null, close() {} }
  c.addLog = () => {}
  try {
    await c.start(origin)
    assert.equal(c.state.status, 'connected')
    await c.sendSessionConfiguration()
    await tick()
    assert.equal(sent.filter(e => e.type === 'response.create').length, 1)
    assert.equal(requests.filter(r => r.path === '/v1/voice/copilot-work' && r.init?.method === 'POST').length, 0)
    assert.equal(requests.filter(r => String(r.path).includes('copilot-decide')).length, 0)
  } finally {
    c.disconnect()
    await tick()
    globalThis.fetch = oldFetch
    if (oldNavigator) Object.defineProperty(globalThis, 'navigator', oldNavigator)
    else delete globalThis.navigator
    globalThis.document = oldDocument; globalThis.window = oldWindow; globalThis.RTCPeerConnection = oldPeer
  }
})


test('speech during initial directory loading also supersedes the automatic opening', async () => {
  const f = fixture(true)
  let ready
  f.c.snapshot=new Promise(resolve=>{ready=resolve})
  const configured=f.c.sendSessionConfiguration()
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started'})
  ready('The directory is now ready.')
  await configured
  assert.equal(f.responses().length,0)
  assert.equal(f.reads.length,0)
})
