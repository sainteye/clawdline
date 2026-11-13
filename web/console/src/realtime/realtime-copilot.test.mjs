import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { build } from 'esbuild'

const { outputFiles } = await build({
  stdin:{contents:'export {RealtimeCopilotClient} from "./realtime-copilot.ts"; export {voiceWork,useCallWorkTools} from "../voice-work/runtime.ts"',resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},
  bundle: true, platform: 'node', format: 'cjs', packages: 'external', write: false,
})
const compiled = { exports: {} }
new Function('require', 'module', 'exports', outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { RealtimeCopilotClient,voiceWork,useCallWorkTools } = compiled.exports
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
      if (path === '/v1/sessions') return Response.json({ sessions: [row,{...row,id:'%8',label:'Other work',sessionId:'conversation-eight'}] })
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

for (const [managed,protocol] of [[false,3],[true,3],[true,2],[null,3]]) test(`the real start path validates mode and capability before a provider connection (managed=${managed}, protocol=${protocol})`, async () => {
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
    if (path === '/v1/settings/copilot') return managed===null?new Response('',{status:503}):Response.json({ work_enabled: managed })
    if (path === '/v1/voice/copilot-session') return Response.json({ client_secret: 'fixture-not-a-key', work_enabled: managed })
    // This replaces the provider transport. No request leaves this process.
    if (String(path).startsWith('https://api.openai.com/v1/realtime/calls')) return new Response('synthetic-answer')
    if (path === '/v1/sessions') return Response.json({ sessions: [row] })
    if (path === '/v1/voice/copilot-work') {
      assert.equal(init?.method, undefined, 'startup must not create input or plan')
      return Response.json({ protocol, state: { id: 'workspace', revision: 1, inputs: [], tasks: [], reply: 'Old history must not be read aloud.' } })
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
    if (managed===null || managed && protocol!==3) {
      assert.equal(c.state.status,'error')
      assert.ok(!requests.some(r=>r.path==='/v1/voice/copilot-session'||String(r.path).startsWith('https://api.openai.com/')),'unsupported calls stop before minting a secret or connecting a provider')
      return
    }
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
  assert.ok(f.reads.every(r=>r.path==='/v1/sessions'),'only the captured speech focus is read')
})


async function withLedger(f,run) {
 const oldDeps=voiceWork.deps,oldState=voiceWork.state
 const requests=[]
 const state={id:'test-workspace',revision:1,plan_version:0,inputs:[],tasks:[],planner_error:'voice_work_planning_failed'}
 voiceWork.state=state
 voiceWork.deps={...oldDeps,request:async(change)=>{
   if(change){requests.push(change);state.revision++
     if(change.action==='input')state.inputs.push({id:change.id,call:change.call,text:change.text,focus:change.focus,deferred:change.deferred,planned:false,at:1})
     if(change.action==='plan')for(const i of state.inputs)if(change.input_ids.includes(i.id))i.deferred=false
     if(change.action==='answer')for(const i of state.inputs)if(i.id===change.id)i.planned=true
   }
   return {protocol:3,state}
 }}
 f.c.journalCall='test-call';useCallWorkTools(f.c.toolDeps(),'test-call')
 try{await run(state,requests);await tick()}finally{useCallWorkTools(undefined);voiceWork.deps=oldDeps;voiceWork.state=oldState}
}

test('managed speech is saved with its original focus but starts no planning or receipt narration',async()=>{
 const f=fixture(true)
 await withLedger(f,async(state,requests)=>{
   await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'spoken-one'})
   f.c.focus='%8'
   await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'spoken-one'})
   await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'spoken-one',transcript:'Summarize this Session.'})
   assert.equal(requests.filter(r=>r.action==='plan').length,0)
   assert.equal(state.inputs[0].focus.id,'%7')
   assert.equal(state.inputs[0].focus.conversation,'conversation-seven')
   assert.equal(state.inputs[0].deferred,true)
   assert.equal(f.responses().length,1)
   assert.equal(f.responses()[0].response.tool_choice,'auto')
   assert.equal(f.responses()[0].response.instructions,undefined,'ordinary conversation uses the configured tool instructions, not a fabricated receipt')
   assert.match(JSON.stringify(f.sent),/spoken-one/)
 })
})

test('managed immediate queries work while an earlier planner request has failed',async()=>{
 const f=fixture(true);f.release()
 await withLedger(f,async(_,requests)=>{
   await f.c.handleToolCall({call_id:'query',name:'get_session_recap',arguments:'{"session_id":"%7"}'})
   const reply=f.sent.find(e=>e.item?.call_id==='query')
   assert.match(reply.item.output,/All six screenshots are captured/)
   assert.ok(f.reads.some(r=>r.path.startsWith('/v1/transcript?')))
   assert.equal(requests.filter(r=>r.action==='plan').length,0)
 })
})

test('a managed mutation cannot bypass the durable confirmation fence',async()=>{
 const f=fixture(true)
 await f.c.handleToolCall({call_id:'rename',name:'smart_rename_sessions',arguments:'{"sessions":["%7"],"confirmed":true}'})
 const reply=f.sent.find(e=>e.item?.call_id==='rename')
 assert.equal(JSON.parse(reply.item.output).error,'voice_work_confirmation_required')
 assert.equal(f.reads.length,0)
})

test('only explicit queue selection promotes the saved input to background planning',async()=>{
 const f=fixture(true)
 await withLedger(f,async(state,requests)=>{
   await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'task-one'})
   await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'task-one'})
   await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'task-one',transcript:'Rename this Session using smart naming.'})
   await f.c.handleToolCall({call_id:'queue',name:'queue_voice_work',arguments:'{"input_ids":["task-one"]}'})
   assert.equal(requests.filter(r=>r.action==='plan').length,1)
   assert.equal(state.inputs[0].deferred,false)
   assert.equal(state.inputs[0].planned,false,'submission does not complete the request')
   assert.equal(JSON.parse(f.sent.find(e=>e.item?.call_id==='queue').item.output).accepted,true)
   assert.ok(!requests.some(r=>r.action==='confirm'))
 })
})

test('speech captures the new address before asynchronous screen following finishes',async()=>{
 const f=fixture(true);const old=globalThis.location
 globalThis.location={hash:'#session=%258'}
 try {await withLedger(f,async(state)=>{
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'new-screen'})
  globalThis.location.hash='#session=%257'
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'new-screen'})
  await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'new-screen',transcript:'Read this Session.'})
  assert.equal(state.inputs[0].focus.id,'%8')
 })}finally{globalThis.location=old}
})

test('a managed read cannot open a browser as an undeclared side effect',async()=>{
 const f=fixture(true)
 await f.c.handleToolCall({call_id:'open-link',name:'get_devstack_links',arguments:'{"open":true}'})
 assert.equal(JSON.parse(f.sent.find(e=>e.item?.call_id==='open-link').item.output).error,'voice_work_confirmation_required')
 assert.equal(f.reads.length,0)
})

test('unknown or foreign input IDs cannot be queued as this call\'s instructions',async()=>{
 const f=fixture(true)
 await withLedger(f,async(state,requests)=>{
   state.inputs.push({id:'foreign',call:'another-call',text:'Other work',planned:false,deferred:true})
   for(const id of ['missing','foreign'])assert.equal((await f.c.workTool('queue_voice_work',{input_ids:[id]},'')).error,'voice_work_input_unavailable')
   assert.equal(requests.filter(r=>r.action==='plan').length,0)
 })
})

test('planner retry requires an explicit new retry utterance and permits one submission',async()=>{
 const f=fixture(true)
 await withLedger(f,async(state,requests)=>{
   state.inputs.push({id:'failed',text:'Previous task',planned:false,planning_error:'voice_work_quota'})
   state.inputs.push({id:'question',call:'test-call',text:'Why did it fail?',planned:false,deferred:true})
   state.inputs.push({id:'retry',call:'test-call',text:'請重試剛才失敗的要求',planned:false,deferred:true})
   assert.equal((await f.c.workTool('retry_voice_work',{input_ids:['failed']},'question')).error,'voice_work_explicit_retry_required')
   assert.equal((await f.c.workTool('retry_voice_work',{input_ids:['failed']},'retry')).accepted,true)
   assert.equal((await f.c.workTool('retry_voice_work',{input_ids:['failed']},'retry')).error,'voice_work_explicit_retry_required')
   assert.equal(requests.filter(r=>r.action==='plan'&&r.retry).length,1)
 })
})
