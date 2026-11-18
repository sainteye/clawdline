// liveTokens counts what a GPT-Live append carries (copilot-live.ts).
import { liveTokens } from './copilot-live.ts'
import assert from 'node:assert/strict'
import { test, afterEach } from 'node:test'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { build } from 'esbuild'

const { outputFiles } = await build({
  stdin:{contents:'export {RealtimeCopilotClient} from "./realtime-copilot.ts"; export {voiceWork,useCallWorkTools} from "../voice-work/runtime.ts"',resolveDir:process.env.COPILOT_SOURCE_DIR??fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},
  bundle: true, platform: 'node', format: 'cjs', packages: 'external', write: false,
})
const compiled = { exports: {} }
new Function('require', 'module', 'exports', outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { RealtimeCopilotClient,voiceWork,useCallWorkTools } = compiled.exports
const origin = { id: '%7', label: 'Prepare screenshots' }
const row = { ...origin, assistant: 'codex', state: 'idle', sessionId: 'conversation-seven' }
const tick = () => new Promise(resolve => setImmediate(resolve))
const fixtures=new Set()
afterEach(()=>{for(const c of fixtures){if(c.dc)c.dc.readyState='closed';c.resetSessionReports();clearTimeout(c.configurationTimer);clearInterval(c.liveTicker)}fixtures.clear()})

function fixture(managed, target = origin) {
  globalThis.document??={documentElement:{lang:'en'}};globalThis.location??={hash:''}
  const sent = [], reads = []
  let release
  const held = new Promise(resolve => { release = resolve })
  const c = new RealtimeCopilotClient()
  fixtures.add(c)
  c.dc = { readyState: 'open', send: text => {const e=JSON.parse(text);sent.push(e);if(e.type==='session.update')queueMicrotask(()=>void c.handleRealtimeEvent({type:'session.updated',session:e.session}))}, close() {} }
  c.call = { memory: '', snapshot: '', options: { workEnabled: managed, origin: target } }
  c.focus = target?.id
  c.snapshot = Promise.resolve('')
  c.addLog = () => {}
  c.toolDeps = () => ({
    observe:spec=>c.observeOperation(spec),
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

test('a list call automatically reads summaries before reporting in both engines and modes', async () => {
  globalThis.document = { documentElement: { lang: 'en' } }
  globalThis.location = { hash: '' }
  for (const live of [false, true]) for (const managed of [false, true]) {
    const f = fixture(managed, null)
    f.c.live = live
    f.c.dc.send = text => f.sent.push(JSON.parse(text))
    const ready = live ? f.c.configureLive() : f.c.sendSessionConfiguration()
    await tick()
    assert.ok(f.reads.some(r => r.path.startsWith('/v1/transcript?')), 'the list call requests each summary without waiting for a user command')
    assert.equal(f.responses().length, 0, 'no invented status before the summaries arrive')
    f.release()
    await ready
    await tick()
    if (!live) await f.c.handleRealtimeEvent({type:'session.updated',session:f.sent.find(e=>e.type==='session.update').session})
    const speech = live ? f.sent.filter(e=>e.type==='session.commentary.append') : f.responses()
    assert.ok(speech.length > 0, 'summary content is submitted automatically')
    assert.match(JSON.stringify(speech), /All six screenshots are captured/)
    assert.doesNotMatch(JSON.stringify(speech), /Greet them in one short/)
    f.c.resetSessionReports()
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

for (const [managed,protocol,engine] of [[false,3,'realtime'],[true,3,'realtime'],[true,2,'realtime'],[null,3,'realtime'],[false,3,'live']]) test(`the real start path validates mode and capability before a provider connection (managed=${managed}, protocol=${protocol}, engine=${engine})`, async () => {
  const oldFetch = globalThis.fetch
  const oldNavigator = Object.getOwnPropertyDescriptor(globalThis, 'navigator')
  const oldDocument = globalThis.document, oldWindow = globalThis.window, oldPeer = globalThis.RTCPeerConnection
  const sent = [], requests = [];let ringStops=0,ringStoppedBeforeSettings=false
  const stream = { getAudioTracks: () => [{ stop() {} }], getTracks: () => [{ stop() {} }] }
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { userAgent: 'synthetic-client', mediaDevices: { getUserMedia: async () => stream } } })
  globalThis.document = { documentElement: { lang: 'en' }, querySelector: () => null, dispatchEvent: () => {}, createElement: () => ({}) }
  const listening = new Set()
  globalThis.window = { addEventListener(type) { listening.add(type) }, removeEventListener(type) { listening.delete(type) } }
  globalThis.RTCPeerConnection = class {
    iceGatheringState = 'complete'
    addTrack() {}
    createDataChannel() { return this.channel = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} } }
    async createOffer() { return { type: 'offer', sdp: 'synthetic-offer' } }
    async setLocalDescription() {}
    async setRemoteDescription() {}
    close() {}
  }
  globalThis.fetch = async (path, init) => {
    requests.push({ path, init })
    if(path==='/v1/settings/copilot')ringStoppedBeforeSettings=ringStops>0
    if (path === '/v1/settings/copilot') return managed===null?new Response('',{status:503}):Response.json({ work_enabled: managed, engine, speed: '1.1' })
    if (path === '/v1/voice/copilot-live') return Response.json({ sdp: 'synthetic-live-answer', session_id: 'synthetic-live-session' })
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
  c.meter = { read:()=>({input:0,output:0}), open: () => null, ring: () => () => {ringStops++}, attachInput: () => null, close() {} }
  c.addLog = () => {}
  c.state.engine = 'previous-call-engine'
  const previous=c.operations.begin({name:'previous-call-tool',source:'tool'});previous.end({outcome:'done'})
  try {
    await c.start(origin)
    assert.equal(ringStoppedBeforeSettings,false,'ringback survives initial history and log state updates')
    assert.ok(!c.state.tools.some(row=>row.name==='previous-call-tool'),'the public start path resets the previous call history')
    if (managed===null || managed && protocol!==3) {
      assert.equal(c.state.status,'error')
      assert.ok(!requests.some(r=>r.path==='/v1/voice/copilot-session'||String(r.path).startsWith('https://api.openai.com/')),'unsupported calls stop before minting a secret or connecting a provider')
      return
    }
    assert.equal(c.state.status, 'connected')
    const saved=requests.filter(r=>r.path==='/v1/voice/copilot-journal').flatMap(r=>JSON.parse(r.init.body).entries)
    const phases=[...saved,...c.journalLines].filter(entry=>entry.text==='connection_stage').map(entry=>entry.tool)
    assert.equal(phases[0].phase,'settings')
    assert.equal(phases[0].engine,undefined,'the new call cannot inherit the previous engine in diagnostics')
    assert.ok(phases.some(entry=>entry.phase==='provider'))
    assert.ok(phases.every(entry=>entry.elapsed_ms>=0&&entry.previous_ms>=0))
    assert.ok(!JSON.stringify(phases).includes('synthetic-offer'),'timing records contain no SDP')
    if (engine === 'live') {
      const request = requests.find(r => r.path === '/v1/voice/copilot-live')
      assert.match(JSON.parse(request.init.body).instructions, /Speak noticeably faster/)
      assert.match(JSON.parse(request.init.body).instructions, /Before calling a message send tool/)
      assert.equal(c.call.options.speed, '1.1', 'the call keeps its saved speed for a later faster/slower request')
      assert.ok(!requests.some(r => r.path === '/v1/voice/copilot-session'), 'Live does not open a Realtime call')
      return
    }
    // Pages change a Session's address with history.replaceState, which fires
    // no hashchange; the call has to hear their own signal (2026-11-13).
    assert.ok(listening.has('clawdline:session-address'), 'a call follows the Session the page moves to')
    await c.sendSessionConfiguration()
    await c.handleRealtimeEvent({type:'session.updated',session:sent.find(e=>e.type==='session.update').session})
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

test('Live speed changes reach the speaking model and are saved, including a return to normal', async () => {
  const f = fixture(false, null)
  const oldFetch = globalThis.fetch
  const saved = []
  globalThis.fetch = async (path, init) => {
    assert.equal(path, '/v1/settings/copilot')
    saved.push(JSON.parse(init.body))
    return Response.json({})
  }
  f.c.live = true
  f.c.call.options = { language: 'en', speed: '0.9' }
  try {
    for (const [speed, phrase] of [['1.1', 'noticeably faster'], ['0.8', 'distinctly slow'], ['1.0', 'everyday conversational pace']]) {
      f.sent.length = 0
      assert.deepEqual(await f.c.adjust({ speed }), { saved: true })
      const updates = f.sent.filter(e => e.type === 'session.instructions.append')
      assert.equal(updates.length, 1)
      assert.equal(updates[0].delegation_id, null)
      assert.ok(updates[0].content.includes(phrase))
      assert.match(updates[0].content, /supersedes all earlier pacing instructions/)
      assert.ok(!f.sent.some(e => e.session?.audio), 'Live receives no unsupported Realtime audio speed event')
    }
    f.sent.length = 0
    await f.c.adjust({ brief: false })
    assert.ok(!f.sent.some(e => e.type === 'session.instructions.append'), 'an answer-length change does not resend pacing')
    assert.deepEqual(saved, [{ speed: '1.1' }, { speed: '0.8' }, { speed: '1.0' }, { brief: false }])
  } finally { globalThis.fetch = oldFetch }
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
 const oldDeps=voiceWork.deps,oldState=voiceWork.state,oldContinuation=voiceWork.continuation
 let continuation=null
 const requests=[]
 const state={id:'test-workspace',revision:1,plan_version:0,inputs:[],tasks:[],planner_error:'voice_work_planning_failed'}
 voiceWork.state=state
 voiceWork.deps={...oldDeps,request:async(change)=>{
   if(change){requests.push(change);state.revision++
     if(change.action==='input')state.inputs.push({id:change.id,call:change.call,text:change.text,focus:change.focus,deferred:change.deferred,planned:false,at:1})
     if(change.action==='plan')for(const i of state.inputs)if(change.input_ids.includes(i.id))i.deferred=false
     if(change.action==='prepare_message'){
       const source=state.inputs.find(i=>i.id===change.input_ids[0]);assert.ok(source&&!source.planned);source.planned=true;source.deferred=false
       state.tasks.push({id:'proposal',text:change.text,tool:'send_message_to_session',args:{session_id:change.id,session_label:'Prepare screenshots',message:change.text},version:1,status:'waiting_confirmation',sources:change.input_ids,depends_on:[],priority:0})
     }
     if(change.action==='confirm_message'){
       const task=state.tasks.find(t=>t.id===change.id);assert.equal(task.version,change.version);task.status='queued';task.approved_version=task.version
       state.inputs.find(i=>i.id===change.input_ids[0]).planned=true
     }
     if(change.action==='continue_batch'){continuation=null;state.inputs.find(i=>i.id===change.input_ids?.[0]).planned=true}
     if(change.action==='answer')for(const i of state.inputs)if(i.id===change.id)i.planned=true
   }
   return {protocol:3,message_confirmation:true,auto_continue:true,continuation,state,prepared:change?.action==='prepare_message'?state.tasks.at(-1):undefined}
 }}
 f.c.journalCall='test-call';useCallWorkTools(f.c.toolDeps(),'test-call')
 try{await run(state,requests,{setOffer:offer=>continuation=offer});await tick()}finally{useCallWorkTools(undefined);voiceWork.deps=oldDeps;voiceWork.state=oldState;voiceWork.continuation=oldContinuation}
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

test('moving from Session A to B mid-call makes B the Session the call talks about',async()=>{
 // 2026-11-13: the page changed its address with history.replaceState, which
 // fires no hashchange, so 「我現在所在的這個 Session」 kept answering about A.
 const f=fixture(true);const old=globalThis.location
 globalThis.location={hash:'#machine=mac-1&session=%258&generation=0123456789abcdef0123456789abcdef'}
 try {
  await f.c.onScreenChange();await tick();await tick()
  assert.equal(f.c.focus,'%8')
  assert.ok(f.sent.some(e=>JSON.stringify(e).includes('Other work')),'the call is told which Session is on screen')
 } finally {globalThis.location=old}
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

test('a plain yes with no read-back bound to it confirms nothing',async()=>{
 const f=fixture(true);f.c.call.options.messageActions=true
 await withLedger(f,async(state,requests)=>{
  state.tasks.push({id:'proposal',tool:'send_message_to_session',text:'Exact message',args:{session_id:'%7',message:'Exact message'},sources:['original'],version:1,status:'waiting_confirmation'})
  f.c.messageProposal={id:'proposal',version:1,workspace:state.id,target:'Prepare screenshots',message:'Exact message',announced:false}
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'unbound'})
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'unbound'})
  await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'unbound',transcript:'好'})
  assert.equal(requests.some(r=>r.action==='confirm_message'),false)
  assert.equal(state.tasks[0].status,'waiting_confirmation')
 })
})

test('a managed call opens a Session on screen at once instead of planning it',async()=>{
 // Call 8e9e22c2 on 2026-11-13: open_session was refused twice and queued for the work list.
 const f=fixture(true)
 await f.c.handleToolCall({call_id:'open',name:'open_session',arguments:'{"session_id":"%7"}'})
 const output=JSON.parse(f.sent.find(e=>e.item?.call_id==='open').item.output)
 assert.notEqual(output.error,'voice_work_confirmation_required')
 assert.ok(f.reads.length>0,'it read the directory to open the Session')
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

test('an acknowledgement cannot consume a write request before it has a durable proposal',async()=>{
 const f=fixture(true)
 await withLedger(f,async(state)=>{
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'reply-request'})
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'reply-request'})
  await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'reply-request',transcript:'Please reply to the Session: use the customer language.'})
  await f.c.handleRealtimeEvent({type:'response.created',response:{id:'ack'}})
  await f.c.handleRealtimeEvent({type:'response.done',response:{id:'ack',status:'completed',output:[{type:'message'}]}})
  await tick()
  assert.equal(state.inputs[0].planned,false,'saying it will reply is not answering the request')
 })
})

test('a delivery question reads the actual work receipt, without querying recipient activity',async()=>{
 const f=fixture(true)
 await withLedger(f,async(state,requests)=>{
  state.inputs.push({id:'failed-send',call:'test-call',text:'Send a message',planned:false,planning_error:'voice_work_timeout'})
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'status'})
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'status'})
  await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'status',transcript:'你有送出嗎？'})
  const output=f.responses().at(-1).response.instructions
  assert.match(output,/voice_work_timeout/);assert.match(output,/not sent yet/)
  assert.equal(f.reads.filter(r=>r.path.startsWith('/v1/transcript?')).length,0)
  assert.equal(requests.filter(r=>r.action==='plan').length,0)
 })
})

test('connected cue precedes the automatic report and plays once after tools are configured',async()=>{
 const f=fixture(true,null);let connected=0
 f.c.state.status='connected';f.c.meter={connected:()=>connected++,pending(){}}
 f.c.dc.send=text=>f.sent.push(JSON.parse(text))
 const preparing=f.c.sendSessionConfiguration();await tick()
 assert.equal(connected,0)
 await f.c.handleRealtimeEvent({type:'session.updated',session:{tools:[]}})
 assert.equal(connected,0)
 const configured=f.sent.find(e=>e.type==='session.update').session
 await f.c.handleRealtimeEvent({type:'session.updated',session:configured})
 await f.c.handleRealtimeEvent({type:'session.updated',session:configured})
 assert.equal(connected,1);assert.equal(f.c.state.connectionReady,true)
 assert.equal(f.responses().length,0,'read summaries before speaking')
 f.release();await preparing
 assert.equal(f.responses().length,1)
 assert.match(JSON.stringify(f.responses()[0]),/All six screenshots are captured/)
 f.c.resetSessionReports()
})

test('a held Session query plays its tool-start cue once and remains silent while waiting',async()=>{
 const f=fixture(true);let cues=0
 f.c.state.status='connected';f.c.meter={pending:()=>cues++,chime(){}}
 await withLedger(f,async()=>{
  const running=f.c.handleToolCall({call_id:'held',name:'get_session_recap',arguments:'{"session_id":"%7"}'})
  await tick()
  assert.equal(f.c.state.progress.phase,'reading');assert.equal(cues,1)
  f.c.updateState({});assert.equal(cues,1)
  f.release();await running
  await f.c.handleRealtimeEvent({type:'response.done',response:{id:'finished',status:'completed',output:[{type:'message'}]}})
  assert.notEqual(f.c.state.progress?.phase,'reading')
 })
})

for(const mode of ['complete','interrupted','changed'])test(`spoken next-batch approval is bound to the heard selection: ${mode}`,async()=>{
 const f=fixture(true)
 await withLedger(f,async(state,requests,control)=>{
  const offer={epoch:1,limit:3,tasks:[{id:'next-a',version:1},{id:'next-b',version:1}]}
  state.tasks.push(...offer.tasks.map((ref,i)=>({...ref,text:i?'Read deployment status':'Read pending questions',tool:'get_session_summary',args:{session_id:String(i)},sources:[],depends_on:[],priority:0,status:'queued'})))
  control.setOffer(offer);await voiceWork.refresh();f.c.offerNextBatch()
  await f.c.handleRealtimeEvent({type:'response.created',response:{id:'batch-read'}})
  await f.c.handleRealtimeEvent({type:'response.output_audio.delta'})
  await f.c.handleRealtimeEvent({type:'response.output_audio_transcript.delta',delta:'Read pending questions. Read deployment status. Shall I continue this batch?'})
  await f.c.handleRealtimeEvent({type:'response.done',response:{id:'batch-read',status:'completed',output:[{type:'message'}]}})
  assert.equal(f.c.batchProposal.announced,false)
  await f.c.handleRealtimeEvent({type:mode==='interrupted'?'output_audio_buffer.cleared':'output_audio_buffer.stopped'})
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'batch-consent'})
  if(mode==='changed')control.setOffer({...offer,tasks:[{id:'next-a',version:2},{id:'next-b',version:1}]})
  await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped',item_id:'batch-consent'})
  await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'batch-consent',transcript:'可以，繼續。'})
  assert.equal(requests.filter(r=>r.action==='continue_batch').length,mode==='complete'?1:0)
  assert.equal(requests.some(r=>r.action==='confirm_message'||r.action==='plan'),false)
 })
})

test('a Session report goes one Project at a time and names the Project before its Sessions',async()=>{
 const f=fixture(true);const reads=[]
 const project=['/w/alpha','/w/beta','/w/alpha','/w/beta','/w/alpha']
 const rows=project.map((key,i)=>({id:`%${i+1}`,label:`Work ${String.fromCharCode(65+i)}`,assistant:'codex',state:'working',sessionId:`conversation-${i}`,project_key:key,activity:{at:100-i,known:true}}))
 f.c.toolDeps=()=>({sleep:async()=>{},now:()=>101,fetch:async(path)=>{
  if(path==='/v1/sessions')return Response.json({sessions:rows})
  if(path.startsWith('/v1/transcript?')){reads.push(new URLSearchParams(path.split('?')[1]).get('session'));return Response.json({entries:[{role:'assistant',text:'Progress'}]})}
  assert.fail(path)
 }})
 await withLedger(f,async()=>{
  await f.c.handleToolCall({call_id:'group',name:'get_sessions_summary',arguments:'{"which":"working"}'})
  const answer=JSON.parse(f.sent.find(e=>e.item?.type==='function_call_output').item.output)
  assert.equal(answer.project,'alpha')
  assert.deepEqual(answer.sessions.map(s=>s.session_id),['%1','%3'],'the newest Project starts with at most two Sessions')
  assert.match(answer.reporting_rule,/which Project these Sessions belong to/)
  await tick()
  assert.deepEqual(reads,['%1','%3','%5'],'the remaining Session of the same Project is prepared first')
 })
})

for(const managed of [false,true])test(`Session reports speak two at a time and retain the actual spoken order (${managed?'managed':'direct'})`,async()=>{
 const f=fixture(managed)
 let releaseNext;const nextReady=new Promise(resolve=>releaseNext=resolve);const reads=[]
 const rows=Array.from({length:4},(_,i)=>({id:`%${i+1}`,label:`Work ${String.fromCharCode(65+i)}`,assistant:'codex',state:'working',sessionId:`conversation-${i}`,activity:{at:100-i,known:true}}))
 f.c.toolDeps=()=>({sleep:async()=>{},now:()=>101,decide:async()=>assert.fail('a verified spoken reference must not ask a classifier'),fetch:async(path)=>{
  if(path==='/v1/sessions')return Response.json({sessions:rows})
  if(path.startsWith('/v1/transcript?')){const id=new URLSearchParams(path.split('?')[1]).get('session');reads.push(id);if(Number(id.slice(1))>2)await nextReady;return Response.json({entries:[{role:'assistant',text:`Actual progress for ${id}`} ]})}
  assert.fail(path)
 }})
 await withLedger(f,async()=>{
  await f.c.handleToolCall({call_id:'group',name:'get_sessions_summary',arguments:'{"which":"working"}'})
  const answer=JSON.parse(f.sent.find(e=>e.item?.type==='function_call_output').item.output)
  assert.deepEqual(answer.sessions.map(s=>s.session_id),['%1','%2'])
  assert.equal(answer.more_pending,2);assert.equal(answer.remaining,undefined,'unspoken batch details stay out of this reply')
  await tick()
  assert.deepEqual(reads,['%1','%2','%3','%4'],'only one next batch is prefetched')
  await f.c.handleRealtimeEvent({type:'response.created',response:{id:'first-batch'}})
  await f.c.handleRealtimeEvent({type:'response.output_audio.delta'})
  await f.c.handleRealtimeEvent({type:'response.output_audio_transcript.delta',delta:'Work A. Work B.'})
  await f.c.handleRealtimeEvent({type:'response.done',response:{id:'first-batch',status:'completed',output:[{type:'message'}]}})
  assert.equal(f.c.spokenSessions.resolve('the last session')?.target,undefined)
  const count=f.responses().length
  await f.c.handleRealtimeEvent({type:'output_audio_buffer.stopped'})
  assert.equal(f.c.spokenSessions.resolve('the last session').target.id,'%2')
  assert.equal(f.responses().length,count,'a held next read cannot invent a second report')
  releaseNext();await tick();await tick()
  assert.equal(f.responses().length,count+1)
  assert.equal(f.responses().at(-1).response.tool_choice,'none')
  await f.c.handleRealtimeEvent({type:'response.created',response:{id:'second-batch'}})
  await f.c.handleRealtimeEvent({type:'response.output_audio.delta'})
  await f.c.handleRealtimeEvent({type:'response.output_audio_transcript.delta',delta:'Work D. Work C.'})
  await f.c.handleRealtimeEvent({type:'response.done',response:{id:'second-batch',status:'completed',output:[{type:'message'}]}})
  await f.c.handleRealtimeEvent({type:'output_audio_buffer.stopped'})
  assert.equal(f.c.spokenSessions.resolve('你講的第二個 Session').target.id,'%3')
  f.c.heardNow='你最後提到的那個 Session，讀一下摘要。'
  await f.c.handleToolCall({call_id:'follow-up',name:'get_session_summary',arguments:'{"session_id":"%2","said":"Work B"}'})
  const follow=JSON.parse(f.sent.find(e=>e.item?.call_id==='follow-up').item.output)
  assert.equal(follow.session_id,'%3')
  f.c.resetSessionReports()
 })
})

test('a cleared Session introduction exposes resume and reuses prepared content without another read',async()=>{
 const f=fixture(true)
 const rows=Array.from({length:4},(_,i)=>({id:`%${i+1}`,label:`Work ${String.fromCharCode(65+i)}`,assistant:'codex',state:'working',sessionId:`conversation-${i}`}))
 const reads=[];f.c.toolDeps=()=>({sleep:async()=>{},fetch:async path=>{
  reads.push(path);if(path==='/v1/sessions')return Response.json({sessions:rows})
  if(path.startsWith('/v1/transcript?'))return Response.json({entries:[{role:'assistant',text:'Confirmed current progress.'}]})
  assert.fail(path)
 }})
 await withLedger(f,async()=>{
  await f.c.handleToolCall({call_id:'report',name:'get_sessions_summary',arguments:'{"which":"working"}'})
  await tick()
  await f.c.handleRealtimeEvent({type:'response.created',response:{id:'partial'}})
  await f.c.handleRealtimeEvent({type:'response.output_audio.delta'})
  await f.c.handleRealtimeEvent({type:'response.output_audio_transcript.delta',delta:'Work A is handling'})
  await f.c.handleRealtimeEvent({type:'response.done',response:{id:'partial',status:'cancelled',output:[{type:'message'}]}})
  await f.c.handleRealtimeEvent({type:'output_audio_buffer.cleared'})
  assert.equal(f.c.state.sessionReportCanContinue,true)
  assert.equal(f.c.spokenSessions.resolve('the last session').target,undefined)
  const before=reads.length,count=f.responses().length
  f.c.continueSessionReport()
  assert.equal(reads.length,before);assert.equal(f.responses().length,count+1)
  assert.equal(f.c.state.sessionReportCanContinue,false)
  f.c.resetSessionReports()
 })
})

test('in a call the copilot reads its own length and cost, and hangs up after its goodbye when asked', async (t) => {
  globalThis.document = { documentElement: { lang: 'zh-Hant' } }
  globalThis.location = { hash: '' }
  const oldWindow = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {} }
  const sent = []
  const c = new RealtimeCopilotClient()
  t.after(() => { clearInterval(c.liveTicker); globalThis.window = oldWindow })
  Object.assign(c, { live: true, attempt: 1, journalCall: '', addLog: () => {}, model: 'gpt-live-1',
    call: { memory: '', snapshot: '', options: {} }, snapshot: Promise.resolve(''), memory: [],
    dc: { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} } })
  c.state = { ...c.state, status: 'connected', cost: { model: 'gpt-live-1', responses: 0, inputTokens: 0, outputTokens: 0, audioOutTokens: 0, usd: 0.0731, priced: true } }
  c.connectedAt = Date.now() - 95_000
  c.endQuietMs = { realtime: 20, live: 20 }
  c.toolDeps = () => ({ ...c.constructor.prototype.toolDeps.call(c), fetch: async () => Response.json({}) })
  const replyOf = (callID) => JSON.parse(sent.find(e => e.type === 'response.item.create' && e.item?.call_id === callID).item.output)

  await c.runLiveTool('call_status', 'get_call_status', '{}')
  const status = replyOf('call_status')
  assert.equal(status.engine, 'GPT-Live')
  assert.ok(status.duration_seconds >= 95 && status.duration_seconds < 100, String(status.duration_seconds))
  assert.equal(status.cost_usd, 0.0731)

  // Asked about closing a Session, the call stays up.
  c.heardNow = '把結帳改版那個 Session 關閉'
  await c.runLiveTool('call_end_1', 'end_call', JSON.stringify({ said: '結束通話' }))
  assert.equal(replyOf('call_end_1').error, 'voice_end_not_asked')
  await new Promise(r => setTimeout(r, 300))
  assert.equal(c.state.status, 'connected')

  // Asked to end the call, it says goodbye first and then hangs up.
  c.heardNow = '我們這一次的對話已經結束了，可以關閉'
  await c.runLiveTool('call_end_2', 'end_call', JSON.stringify({ said: c.heardNow }))
  assert.equal(replyOf('call_end_2').ending, true)
  await new Promise(r => setTimeout(r, 700))
  assert.equal(c.state.status, 'connected', 'it does not hang up before the goodbye')
  c.handleLiveEvent({ type: 'session.output_transcript.delta', delta: '好，那我們下次再聊，掰掰！' })
  for (let i = 0; i < 40 && c.state.status === 'connected'; i++) await new Promise(r => setTimeout(r, 100))
  assert.notEqual(c.state.status, 'connected', 'the call ends once the goodbye has been said')
  assert.ok(sent.some(e => e.type === 'session.close'), 'GPT-Live is told the session is over')
})

test('a GPT-Live call configures its backend after start, starts its report, and answers the backend with the same tools', async (t) => {
  globalThis.document = { documentElement: { lang: 'zh-Hant' } }
  globalThis.location = { hash: '' }
  const sent = [], reads = []
  const c = new RealtimeCopilotClient()
  t.after(() => clearInterval(c.liveTicker))
  c.live = true
  c.attempt = 1
  c.state = { ...c.state, status: 'connected' }
  c.journalCall = 'call-live-test'
  c.dc = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} }
  c.call = { memory: '', snapshot: '', options: {} }
  c.snapshot = Promise.resolve('【通話開始時的 Session 快照】 one Session')
  c.memory = []
  c.addLog = () => {}
  c.toolDeps = () => ({
    sleep: async () => {},
    fetch: async (path, init) => {
      reads.push(path)
      assert.ok(!init?.method || init.method === 'GET')
      if (path === '/v1/sessions') return Response.json({ sessions: [row] })
      return Response.json({})
    },
  })

  c.handleLiveEvent({ type: 'session.started' })
  await tick(); await tick()
  assert.ok(c.state.cost?.live, 'the GPT-Live length and cost show from the start')
  assert.equal(c.state.cost.live.billedSeconds, 0)
  const update = sent.find(e => e.type === 'session.update')
  assert.ok(update, 'the backend is configured once the session has started')
  assert.match(update.session.delegation.responses.instructions, /one Session/)
  assert.equal(sent.filter(e => e.type === 'session.commentary.append').length, 1, 'one initial report')
  assert.equal(c.state.connectionReady, true)
  assert.equal(sent.filter(e => e.type === 'response.create' && e.response).length, 0, 'no Realtime response is ever sent to GPT-Live')

  sent.length = 0
  c.handleLiveEvent({ type: 'session.input_transcript.delta', delta: '現在有' })
  c.handleLiveEvent({ type: 'session.input_transcript.delta', delta: '哪些 Session？' })
  assert.equal(c.state.lastTranscript, '現在有哪些 Session？')
  c.handleLiveEvent({ type: 'session.delegation.created', delegation: { id: 'item_1', target: 'responses' } })
  let cues = 0
  c.meter.pending = () => { cues++ }
  c.handleLiveEvent({ type: 'response.event', delegation_id: 'item_1', event: { type: 'response.output_item.done', response_id: 'resp_1', item: { type: 'function_call', call_id: 'call_1', name: 'list_sessions', arguments: '{}' } } })
  assert.equal(cues, 1, 'a Session lookup plays one tool-start cue')
  for (let i = 0; i < 20 && !sent.some(e => e.type === 'response.create'); i++) await tick()
  assert.ok(reads.includes('/v1/sessions'), 'the tool ran in this browser')
  const result = sent.find(e => e.type === 'response.item.create')
  assert.equal(result?.item.call_id, 'call_1')
  assert.match(result.item.output, /Prepare screenshots/)
  assert.deepEqual(sent.map(e => e.type), ['response.item.create', 'response.create'])

  // A Session read is spoken at once, before the backend's own answer.
  sent.length = 0
  c.toolDeps = () => ({ sleep: async () => {}, fetch: async (path) => path === '/v1/sessions' ? Response.json({ sessions: [{ ...row, recap: { text: 'Six screenshots are captured.', at: Math.floor(Date.now() / 1000), current: true } }] }) : Response.json({}) })
  c.handleLiveEvent({ type: 'response.event', delegation_id: 'item_2', event: { type: 'response.output_item.done', response_id: 'resp_2', item: { type: 'function_call', call_id: 'call_2', name: 'get_session_summary', arguments: JSON.stringify({ session_id: '%7', said: 'it' }) } } })
  for (let i = 0; i < 30 && !sent.some(e => e.type === 'response.create'); i++) await tick()
  assert.deepEqual(sent.map(e => e.type), ['session.commentary.append', 'response.item.create', 'response.create'])
  // Live accepts a delegation id only for client delegations; a Responses
  // delegation's id is refused ("Unknown client or Decision delegation"), so
  // the note goes session-wide, as the delegation guide steers a live call.
  assert.equal(sent[0].delegation_id, null)
  assert.match(sent[0].content, /Six screenshots are captured/)
  assert.equal(c.pendingTools.size, 0, 'the cue stops once the tool has answered')
  const journaled = c.journalLines.find(l => l.kind === 'tool' && l.text === 'list_sessions')
  assert.match(journaled?.tool?.steps ?? '', /\/v1\/sessions \d+ms/, 'each machine request of a tool is timed in the journal')

  c.handleLiveEvent({ type: 'session.usage.updated', usage: { seconds: 60 } })
  c.handleLiveEvent({ type: 'response.event', event: { type: 'response.completed', response: { id: 'backend-response-1', usage: { input_tokens: 10000, output_tokens: 0 } } } })
  assert.ok(Math.abs(c.state.cost.usd - (0.05 + 0.001)) < 1e-9, String(c.state.cost.usd))
  assert.equal(c.state.cost.priced, true)
  const once=c.state.cost.usd
  c.handleLiveEvent({type:'response.event',event:{type:'response.completed',response:{id:'backend-response-1',usage:{input_tokens:10000,output_tokens:0}}}})
  assert.equal(c.state.cost.usd,once,'a repeated completion is not billed twice in the estimate')
  assert.ok(c.state.cost.live.seconds >= 60)
  clearInterval(c.liveTicker)
})

test('a GPT-Live call placed from a Session on every machine\'s list talks about that Session as the tools list it', async (t) => {
  globalThis.document = { documentElement: { lang: 'zh-Hant' } }
  globalThis.location = { hash: '' }
  const sent = []
  const c = new RealtimeCopilotClient()
  t.after(() => clearInterval(c.liveTicker))
  c.live = true
  c.attempt = 1
  c.state = { ...c.state, status: 'connected' }
  c.dc = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} }
  // 2026-11-15: placed from %7 on 所有機器; the tools list it as fleet:<machine>:%257.
  c.call = { memory: '', snapshot: '', options: { origin: { id: '%7', label: 'Voice realism', machine: 'mac_a' } } }
  c.focus = '%7'
  c.snapshot = Promise.resolve('')
  c.memory = []
  c.addLog = () => {}
  const fleetRow = { machine: 'mac_a', machineName: 'studio-mac', row: { id: '%7', label: 'Voice realism', assistant: 'claude', state: 'working', sessionId: 'conv-547' } }
  c.toolDeps = () => ({ sleep: async () => {}, fleet: async () => [fleetRow, { ...fleetRow, machine: 'linux_b', machineName: 'linux' }], fetch: async () => Response.json({}) })
  c.handleLiveEvent({ type: 'session.started' })
  for (let i = 0; i < 20 && !c.state.connectionReady; i++) await tick()
  assert.equal(c.state.connectionReady, true)
  assert.match(c.focus, /^fleet:mac_a:/, `the call is about the listed row, not the page's raw id (${c.focus})`)
  assert.equal(c.call.options.origin.id, c.focus)
  const update = sent.find(e => e.type === 'session.update')
  assert.ok(update.session.delegation.responses.instructions.includes(c.focus), 'the backend is told the listed id')
})

test('the sheet\'s toggle places a ringing or live call again on the other voice system, from the same Session', () => {
  const c = new RealtimeCopilotClient()
  const events = []
  c.start = (origin, engine) => { events.push(['start', origin, engine]) }
  c.disconnect = () => { events.push(['disconnect']) }
  const origin = { id: 'fleet:mac_a:%257', label: 'Voice realism' }
  c.call = { memory: '', snapshot: '', options: { origin } }
  c.lastEngine = 'live'
  c.state = { ...c.state, status: 'connecting' }
  c.switchEngine('live')
  assert.deepEqual(events, [], 'the system already in use changes nothing')
  c.switchEngine('realtime')
  assert.deepEqual(events, [['disconnect'], ['start', origin, 'realtime']])
  // With no call there is nothing to switch.
  events.length = 0
  c.state = { ...c.state, status: 'disconnected' }
  c.switchEngine('live')
  assert.deepEqual(events, [])
})

test('a GPT-Live call follows the Session opened on screen with its summary', async (t) => {
  globalThis.document = { documentElement: { lang: 'zh-Hant' } }
  const old = globalThis.location
  globalThis.location = { hash: '#machine=mac-1&session=%258&generation=0123456789abcdef0123456789abcdef' }
  const now = Math.floor(Date.now() / 1000)
  const sent = []
  const c = new RealtimeCopilotClient()
  t.after(() => { clearInterval(c.liveTicker); c.dc.readyState = 'closed'; globalThis.location = old })
  c.live = true
  c.attempt = 1
  c.liveQuietMs = 0
  c.livePollMs = 1
  c.personSpoke = true
  c.focus = '%7'
  c.journalCall = 'call-test'
  c.state = { ...c.state, status: 'connected', speakingState: 'idle' }
  c.dc = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} }
  c.call = { memory: '', snapshot: '', options: {} }
  c.snapshot = Promise.resolve('')
  c.memory = []
  c.addLog = () => {}
  c.recapMode = 'background'
  const other = { ...row, id: '%8', label: 'Keyboard fix', recap: { text: 'The keyboard fix is being written.', at: now - 60, current: false } }
  c.toolDeps = () => ({ ...c.constructor.prototype.toolDeps.call(c), sleep: async () => {}, tasks: undefined, fleet: undefined,
    fetch: async (path) => path === '/v1/sessions' ? Response.json({ sessions: [row, other] }) : Response.json({}) })
  await c.onScreenChange()
  for (let i = 0; i < 40; i++) await tick()
  assert.equal(c.focus, '%8')
  const notes = sent.filter(e => e.type === 'session.commentary.append').map(e => e.content)
  assert.equal(notes.length, 1, 'the Session opened on screen is told from its summary without being asked')
  assert.match(notes[0], /switched the call to Session "Keyboard fix".*The keyboard fix is being written/)
  assert.ok(c.journalLines.some(l => l.text === 'switch_summary' && l.tool.args.includes('%8')), 'every switch leaves a trace in the call journal')
  // The Session on screen when a call starts is where it starts, not a switch.
  const first = new RealtimeCopilotClient(), firstSent = []
  t.after(() => clearInterval(first.liveTicker))
  Object.assign(first, { live: true, attempt: 1, journalCall: 'call-first', addLog: () => {}, call: { memory: '', snapshot: '', options: {} }, snapshot: Promise.resolve(''), memory: [], toolDeps: c.toolDeps,
    state: { ...first.state, status: 'connected' }, dc: { readyState: 'open', send: text => firstSent.push(JSON.parse(text)), close() {} } })
  await first.onScreenChange()
  for (let i = 0; i < 20; i++) await tick()
  assert.equal(first.focus, '%8')
  assert.equal(firstSent.filter(e => e.type === 'session.commentary.append').length, 0, 'no summary is spoken for the Session a call starts on')
  first.dc.readyState = 'closed'
  // A call that starts on the list, with no Session open, and then opens one is a switch
  // (a phone call on 2026-11-16 opened Release 2.4 from the list and was told nothing of it).
  const listed = new RealtimeCopilotClient(), listedSent = []
  t.after(() => clearInterval(listed.liveTicker))
  Object.assign(listed, { live: true, attempt: 1, journalCall: 'call-listed', addLog: () => {}, call: { memory: '', snapshot: '', options: {} }, snapshot: Promise.resolve(''), memory: [], toolDeps: c.toolDeps,
    state: { ...listed.state, status: 'connected' }, dc: { readyState: 'open', send: text => listedSent.push(JSON.parse(text)), close() {} } })
  globalThis.location = { hash: '' }
  await listed.onScreenChange()
  globalThis.location = { hash: '#machine=mac-1&session=%258&generation=0123456789abcdef0123456789abcdef' }
  await listed.onScreenChange()
  for (let i = 0; i < 20; i++) await tick()
  assert.equal(listed.focus, '%8')
  assert.match(listedSent.filter(e => e.type === 'session.commentary.append')[0]?.content ?? '', /switched the call to Session "Keyboard fix"/)
  listed.dc.readyState = 'closed'
  // A Session the list does not have is journaled as such, not dropped silently.
  globalThis.location = { hash: '#machine=mac-1&session=%259&generation=0123456789abcdef0123456789abcdef' }
  c.focus = '%9'
  await c.openFromOrigin(true)
  assert.ok(c.journalLines.some(l => l.text === 'switch_summary' && l.tool.outcome === 'failed' && /not on the Session list/.test(l.tool.answer)))
})

test('a Live screen switch waits for speech and only the newest Session opens', async () => {
  const f = fixture(false)
  f.c.live = true
  f.c.liveQuietMs = 1500
  f.c.livePollMs = 1
  const realNow = Date.now
  let now = realNow()
  Date.now = () => now
  const fresh = { text: 'Current work.', at: Math.floor(now / 1000), current: true }
  f.c.toolDeps = () => ({ fetch: async () => Response.json({ sessions: [
    { ...row, recap: fresh }, { ...row, id: '%8', label: 'Other work', recap: fresh },
  ] }) })
  const notes = () => f.sent.filter(e => e.type === 'session.commentary.append')
  try {
    f.c.handleLiveEvent({ type: 'session.output_transcript.delta', delta: 'The first report is still being spoken.' })
    const first = f.c.openFromOrigin(true)
    for (let i = 0; i < 10; i++) await tick()
    assert.equal(notes().length, 0, 'an opening must not be injected into ongoing speech')
    f.c.focus = '%8'
    const newest = f.c.openFromOrigin(true)
    now += 1000
    f.c.handleLiveEvent({ type: 'session.output_transcript.delta', delta: 'The end of the report.' })
    now += 1499
    await new Promise(r => setTimeout(r, 5))
    assert.equal(notes().length, 0, 'each speech delta restarts the quiet window')
    now++
    await Promise.all([first, newest])
    assert.equal(notes().length, 1)
    assert.match(notes()[0].content, /Other work/)
  } finally { Date.now = realNow; f.c.dc.readyState = 'closed' }
})

test('a GPT-Live call switched to another Session says its summary at once and goes on with the latest one', async (t) => {
  globalThis.document = { documentElement: { lang: 'zh-Hant' } }
  globalThis.location = { hash: '' }
  const now = Math.floor(Date.now() / 1000)
  const sent = [], forks = []
  const c = new RealtimeCopilotClient()
  t.after(() => { clearInterval(c.liveTicker); c.dc.readyState = 'closed' })
  c.live = true
  c.attempt = 1
  c.liveQuietMs = 0
  c.livePollMs = 1
  c.personSpoke = true
  c.state = { ...c.state, status: 'connected', speakingState: 'idle' }
  c.dc = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} }
  c.call = { memory: '', snapshot: '', options: {} }
  c.snapshot = Promise.resolve('')
  c.memory = []
  c.addLog = () => {}
  c.recapMode = 'background'
  const other = { ...row, id: '%8', label: 'Keyboard fix', recap: { text: 'The keyboard fix is being written.', at: now - 1200, current: false } }
  c.toolDeps = () => ({ ...c.constructor.prototype.toolDeps.call(c), sleep: async () => {}, tasks: undefined, fleet: undefined,
    fetch: async (path, init) => {
      if (path === '/v1/sessions') return Response.json({ sessions: [row, other] })
      if (path === '/v1/voice/copilot-recap') { forks.push(JSON.parse(String(init.body))); return Response.json({ recap: 'The keyboard fix is deployed and waits for your check.', at: now, current: true }) }
      return Response.json({})
    } })
  const notes = () => sent.filter(e => e.type === 'session.commentary.append').map(e => e.content)
  const realNow = Date.now, at = realNow()
  Date.now = () => at
  try {
    // The person has already been talking; switching still tells them about the new Session from its summary.
    c.turnTo({ id: '%8', label: 'Keyboard fix' })
    for (let i = 0; i < 40 && notes().length === 0; i++) await tick()
    assert.equal(notes().length, 1)
    assert.match(notes()[0], /switched the call to Session "Keyboard fix".*The keyboard fix is being written/)
    for (let i = 0; i < 40 && forks.length === 0; i++) await tick()
    assert.equal(forks.length, 1, 'the latest summary is asked for at the same time')
    c.handleLiveEvent({ type: 'session.output_transcript.delta', delta: 'The keyboard fix is being written.' })
    for (let i = 0; i < 400 && notes().length < 2; i++) await new Promise(r => setTimeout(r, 5))
    assert.match(notes()[1], /latest state .*deployed and waits for your check/)
  } finally { Date.now = realNow }
})

test('a GPT-Live call placed from a Session opens on its summary at once and goes on with the latest one', async (t) => {
  globalThis.document = { documentElement: { lang: 'zh-Hant' } }
  globalThis.location = { hash: '' }
  const now = Math.floor(Date.now() / 1000)
  const history = '這個 Session 在準備截圖。一開始你交代我拍四張，後來改成六張，中間重拍了兩次，' + '又調整了好幾次版面與文字，'.repeat(40)
  const run = async ({ latest, keptAge = 1200, reports = [] }) => {
    const sent = [], forks = [], reads = []
    const c = new RealtimeCopilotClient()
    t.after(() => clearInterval(c.liveTicker))
    c.live = true
    c.attempt = 1
    c.liveQuietMs = 0
    c.livePollMs = 1
    c.state = { ...c.state, status: 'connected' }
    c.dc = { readyState: 'open', send: text => sent.push(JSON.parse(text)), close() {} }
    c.call = { memory: '', snapshot: '', options: { origin: { id: '%7', label: 'Prepare screenshots' } } }
    c.focus = '%7'
    c.snapshot = Promise.resolve('')
    c.memory = []
    c.addLog = () => {}
    c.recapMode = 'background'
    const listed = { ...row, recap: { text: 'Four screenshots were taken.', at: now - keptAge, current: false } }
    c.toolDeps = () => ({ ...c.constructor.prototype.toolDeps.call(c), sleep: async () => {}, tasks: undefined, fleet: undefined,
      fetch: async (path, init) => {
        reads.push(path)
        if (path === '/v1/sessions') return Response.json({ sessions: [listed] })
        if (path === '/v1/voice/copilot-recap') { forks.push(JSON.parse(String(init.body))); return Response.json({ recap: latest, at: now, current: true }) }
        if (String(path).startsWith('/v1/transcript')) return Response.json({ items: reports.map((text, i) => ({ id: `m${i}`, role: 'assistant', text })) })
        return Response.json({})
      } })
    const notes = () => sent.filter(e => e.type === 'session.commentary.append').map(e => e.content)
    // The clock stands still, as it may within one millisecond on a fast runner:
    // speech in the same millisecond as the opening note still counts as the
    // opening being said (CI 43708656326 lost the note when it did not).
    const realNow = Date.now, at = realNow()
    Date.now = () => at
    try {
    c.handleLiveEvent({ type: 'session.started' })
    for (let i = 0; i < 40 && notes().length === 0; i++) await tick()
    for (let i = 0; i < 20; i++) await tick()
    const beforeSpeech = notes().length
    // The opening is spoken; the latest state follows only once it has been said.
    c.handleLiveEvent({ type: 'session.output_transcript.delta', delta: 'Four screenshots were taken.' })
    // Waits for the note itself, not a fixed time: CI runners are slower (CI 43708656326).
    const expected = latest === 'Four screenshots were taken.' || keptAge <= 300 ? 1 : 2
    for (let i = 0; i < 400 && notes().length < expected; i++) await new Promise(r => setTimeout(r, 5))
    for (let i = 0; i < 4; i++) await new Promise(r => setTimeout(r, 5))
    return { sent: notes(), beforeSpeech, forks, reads }
    } finally { Date.now = realNow; c.dc.readyState = 'closed' }
  }
  const changed = await run({ latest: 'All six screenshots are done; it waits for your approval.' })
  assert.equal(changed.forks.length, 1, 'the latest summary is asked for, the kept one being 20 minutes old')
  assert.ok(changed.reads.some(p => String(p).startsWith('/v1/transcript')), 'the newest reports are read at the same time')
  assert.equal(changed.beforeSpeech, 1, 'the latest state waits while the opening is spoken')
  assert.equal(changed.sent.length, 2)
  assert.match(changed.sent[0], /Open the call .*Four screenshots were taken/, 'the kept summary is said at once')
  assert.doesNotMatch(changed.sent[0], /Greet/)
  assert.match(changed.sent[1], /latest state .*All six screenshots are done/)
  const long = await run({ latest: history + '現在六張都拍好了，等你核准。' })
  assert.match(long.sent[1], /現在六張都拍好了，等你核准。/, 'a long recap keeps its end, where it stands now')
  assert.match(long.sent[1], /這個 Session 在準備截圖。/, 'and its first sentence')
  const same = await run({ latest: 'Four screenshots were taken.' })
  assert.equal(same.sent.length, 1, 'an unchanged latest summary with no new reports is not said again')
  const reworded = await run({ latest: 'Four screenshots: taken, four.' })
  assert.equal(reworded.sent.length, 1, 'a latest summary in other words with nothing new is not said as news')
  const fresh = await run({ latest: 'unused', keptAge: 60 })
  assert.equal(fresh.forks.length, 0, 'a summary at most five minutes old is the latest; nothing more is read')
  assert.equal(fresh.sent.length, 1)
})

test('a Live list report advances after each bound narration and quiet window, then pauses for speech',async()=>{
 globalThis.location={hash:'#session=%251'}
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=3;f.c.liveStartGraceMs=8000;f.c.livePollMs=1
 const rows=Array.from({length:4},(_,i)=>({id:`%${i+1}`,label:`Work ${String.fromCharCode(65+i)}`,state:'idle',sessionId:`conversation-${i}`,recap:{text:`Current summary ${i}`,at:Math.floor(Date.now()/1000),current:true}}))
 const reads=[]
 f.c.toolDeps=()=>({sleep:async()=>{},fetch:async path=>{
  reads.push(path)
  if(path==='/v1/sessions')return Response.json({sessions:rows})
  if(path.startsWith('/v1/transcript?'))return Response.json({entries:[{role:'assistant',text:'Current progress'}]})
  assert.fail(path)
 }})
 await f.c.configureLive();await tick()
 assert.equal(f.c.focus,'%1','initial screen focus is captured before the list report starts')
 const reports=()=>f.sent.filter(e=>e.type==='session.commentary.append')
 assert.equal(reports().length,1)
 assert.match(reports()[0].content,/Work A/);assert.doesNotMatch(reports()[0].content,/Work B/)
 // Every append fits what GPT-Live accepts: the rules and evidence go as thinking pieces.
 for(const e of f.sent.filter(e=>/^session\.(commentary|thinking)\.append$/.test(e.type)))assert.ok(liveTokens(e.content)<=500,`${e.type} ${liveTokens(e.content)}`)
 assert.ok(f.sent.some(e=>e.type==='session.thinking.append'&&/Session report rules/.test(e.content)),'the report rules reach the live model')
 assert.ok(f.sent.some(e=>e.type==='session.thinking.append'&&/Machine evidence.*Current summary 0/.test(e.content)),'and the evidence')
 assert.doesNotMatch(reports()[0].content,/Work C/)
 await new Promise(r=>setTimeout(r,4));assert.equal(reports().length,1,'prepared content is not already spoken')
 f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:'Work A is done.'})
 const deadline=Date.now()+3000
 while(reports().length<2&&Date.now()<deadline)await new Promise(r=>setTimeout(r,5))
 assert.equal(reports().length,2)
 assert.match(reports()[1].content,/Work B/)
 f.c.handleLiveEvent({type:'session.input_transcript.delta',delta:'等一下'})
 assert.equal(f.c.sessionReports.hasPending(),true)
 const before=reads.length
 await new Promise(r=>setTimeout(r,8))
 assert.equal(reports().length,2)
 f.c.liveHeard.take()
 f.c.continueSessionReport();await tick()
 assert.equal(reads.length,before,'continuation reuses the prepared interrupted batch')
 globalThis.location={hash:''}
})

test('speaking during a Live list summary read cancels automatic narration without creating work',async()=>{
 const f=fixture(true,null);f.c.live=true
 const preparing=f.c.configureLive();await tick()
 f.c.handleLiveEvent({type:'session.input_transcript.delta',delta:'先等一下'})
 f.release();await preparing
 assert.equal(f.sent.filter(e=>e.type==='session.commentary.append').length,0)
 assert.equal(f.reads.some(r=>r.path.includes('copilot-work')),false)
})


test('menu questions send visible text and explicit read routing in both engines without a planning model',async()=>{
  for(const live of [false,true]){
    const f=fixture(false)
    f.c.live=live
    f.c.state.status='connected';f.c.state.connectionReady=true
    const original=f.c.toolDeps()
    f.c.toolDeps=()=>({...original,fetch:async(path,init)=>path==='/v1/sessions'?Response.json({sessions:[{...row,project_key:'/synthetic/alpha',icon:{cells:[['#123456']]}}]}):original.fetch(path,init)})
    const menu=await f.c.questionTargets()
    assert.equal(menu.targets[0].project,'alpha')
    assert.deepEqual(menu.targets[0].icon,{cells:[['#123456']]})
    assert.equal(f.reads.filter(r=>r.path.includes('/v1/transcript')).length,0,'opening the menu is directory-only')
    await f.c.askQuestion('insights','Read the last five replies.',menu.targets.find(r=>r.id===origin.id))
    assert.match(f.c.getState().lastTranscript,/Read the last five replies/)
    const text=JSON.stringify(f.sent)
    assert.match(text,/explain_session_reply/)
    assert.ok(!text.includes('#123456'),'project artwork stays out of model instructions')
    assert.match(text,/replies/)
    assert.match(text,/Read the last five replies/)
    assert.ok(f.reads.every(r=>!r.init?.method),'menu submission does not execute a mutation or planner call')
    if(live)assert.equal(f.sent.filter(e=>e.type==='session.commentary.append').length,1)
    else assert.equal(f.sent.filter(e=>e.type==='conversation.item.create'&&e.item.role==='user').length,1)
    f.c.narrator.reset()
  }
})

test('a menu target replaced by another conversation sends nothing',async()=>{
  const f=fixture(false)
  f.c.state.status='connected';f.c.state.connectionReady=true
  await assert.rejects(f.c.askQuestion('reply','Explain its reply.',{id:origin.id,label:origin.label,conversation:'another-conversation'}))
  assert.equal(f.sent.length,0)
})

test('a disconnected question neither starts a paid call nor sends a request',async()=>{
  const f=fixture(false)
  await assert.rejects(f.c.askQuestion('summary','Summarize.',origin))
  assert.equal(f.sent.length,0)
  assert.equal(f.reads.length,0)
})


test('a managed menu question is saved deferred with exact focus and never submitted for planning',async()=>{
  const f=fixture(true)
  f.c.state.status='connected';f.c.state.connectionReady=true
  await withLedger(f,async(state,requests)=>{
    const menu=await f.c.questionTargets()
    await f.c.askQuestion('insights','Read the last five replies.',menu.targets.find(r=>r.id===origin.id))
    assert.equal(state.inputs.length,1)
    assert.equal(state.inputs[0].deferred,true)
    assert.equal(state.inputs[0].focus.id,origin.id)
    assert.equal(state.inputs[0].focus.conversation,'conversation-seven')
    assert.equal(requests.filter(r=>r.action==='plan').length,0)
    assert.match(JSON.stringify(f.sent),new RegExp(state.inputs[0].id))
  })
})


test('both call engines reuse the exact analysis context and include its text-model cost',async()=>{
 for(const live of [false,true]){
  const f=fixture(false);f.c.live=live
  const calls=[]
  f.c.toolDeps=()=>({fetch:async(path,init)=>{
   assert.equal(path,'/v1/voice/copilot-question')
   const body=JSON.parse(init.body);calls.push(body)
   return Response.json({session_id:body.session,conversation:body.conversation,context:'context-one',analysis:{answer:'A is faster'},model:'gpt-4.1-mini',usage:{prompt_tokens:1000,completion_tokens:200,prompt_tokens_details:{cached_tokens:100}}})
  }})
  const selected={id:'%7',label:'Compare options',conversation:'conversation-seven'}
  await f.c.analyzeQuestion(selected,'Why A?',3)
  await f.c.analyzeQuestion(selected,'What about cost?',3)
  assert.equal(calls[0].context,undefined)
  assert.equal(calls[1].context,'context-one')
  assert.ok(f.c.getState().cost.usd>0)
  if(live) assert.equal(f.c.getState().cost.live.backendUSD,f.c.questionUSD)
  await f.c.analyzeQuestion({...selected,conversation:'replacement'},'Why A?',3)
  assert.equal(calls[2].context,undefined,'context never crosses conversation identity')
 }
})

test('an old daemon has no question endpoint and cannot spend on an unrelated recap',async()=>{
 const f=fixture(false);let reads=0
 f.c.toolDeps=()=>({fetch:async(path)=>{assert.equal(path,'/v1/voice/copilot-question');reads++;return new Response('',{status:404})}})
 assert.equal(await f.c.analyzeQuestion({id:'%7',label:'Compare',conversation:'c'},'Why?',3),undefined)
 assert.equal(reads,1)
})


test('Live menu routing fits the append limit even for a long Chinese Session title',async()=>{
 const f=fixture(false);f.c.live=true;f.c.state.status='connected';f.c.state.connectionReady=true
 const deps=f.c.toolDeps(),label='比較各個模型方案與條件'.repeat(12)
 f.c.toolDeps=()=>({...deps,fetch:async(path,init)=>path==='/v1/sessions'?Response.json({sessions:[{...row,label}]}):deps.fetch(path,init)})
 const menu=await f.c.questionTargets()
 await f.c.askQuestion('insights','讀它最後五則回覆，整理最重要的洞見，以及對我的影響。',menu.targets[0])
 const appends=f.sent.filter(e=>/^session\.(commentary|thinking)\.append$/.test(e.type))
 assert.ok(appends.length)
 for(const e of appends)assert.ok(liveTokens(e.content)<=500,`${e.type} ${liveTokens(e.content)} estimated tokens`)
 assert.match(JSON.stringify(appends),/explain_session_reply/)
 assert.match(JSON.stringify(appends),/conversation-seven/)
 assert.match(JSON.stringify(appends),/讀它最後五則回覆/)
 assert.equal(appends.filter(e=>e.type==='session.commentary.append').length,1)
})

test('interrupting the opening aborts its reads and immediately releases its progress cue',async()=>{
 const f=fixture(false,null);globalThis.location={hash:''}
 let signal
 f.c.toolDeps=()=>({sleep:async()=>{},fetch:async(path,init)=>{
  if(path==='/v1/sessions')return Response.json({sessions:[{id:'%1',label:'Work',state:'idle'}]})
  if(path.startsWith('/v1/transcript?'))return new Promise((resolve,reject)=>{signal=init.signal;signal.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')),{once:true})})
  assert.fail(path)
 }})
 const pending=f.c.prepareListReport();await tick()
 assert.ok(f.c.pendingTools.size>0)
 f.c.cancelOpening()
 assert.equal(signal.aborted,true)
 assert.equal(f.c.pendingTools.size,0)
 await pending
 assert.equal(f.responses().length,0)
})

test('Live report continuation clears the consumed input instead of waiting for its own speech',async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=1;f.c.livePollMs=1
 f.c.liveHeard.add('其他的呢');f.c.heardNow='其他的呢'
 f.c.sessionReports.start({sessions:[{session_id:'one',label:'Detailed title'}]},async()=>assert.fail('no next read'),page=>f.c.presentSessionReport(page,()=>f.c.dc?.readyState==='open'))
 await f.c.runLiveTool('continue-test','get_sessions_summary',JSON.stringify({continue_report:true}))
 for(let i=0;i<20;i++)await tick()
 assert.equal(f.c.liveHeard.peek(),'','the tool consumed this request; output must not be required to clear it')
 assert.ok(f.sent.some(e=>e.type==='session.commentary.append'&&/Detailed title/.test(e.content)))
})

test('Live reports bind one narration item at a time and advance without verbatim titles',async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=2;f.c.livePollMs=1
 const pages=[]
 f.c.sessionReports.start({sessions:[{session_id:'one',label:'A long exact title',recap:'First content.'},{session_id:'two',label:'Another long exact title',recap:'Second content.'}]},async()=>assert.fail('no next read'),page=>{pages.push(page);f.c.presentSessionReport(page,()=>f.c.dc?.readyState==='open')})
 f.c.sessionReports.resume();await tick()
 const notes=()=>f.sent.filter(e=>e.type==='session.commentary.append')
 assert.match(notes()[0].content,/A long exact title/)
 assert.doesNotMatch(notes()[0].content,/Another long exact title/,'two items cannot become one untrackable paraphrase')
 f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:'First content is complete.'})
 for(let i=0;i<100&&notes().length<2;i++)await new Promise(r=>setTimeout(r,2))
 assert.equal(notes().length,2)
 assert.match(notes()[1].content,/Another long exact title/)
 f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:'Second content is ready.'})
 for(let i=0;i<100&&f.c.sessionReports.hasPending();i++)await new Promise(r=>setTimeout(r,2))
 assert.equal(f.c.sessionReports.hasPending(),false)
 assert.deepEqual(f.c.spokenSessions.heardIDs(),[],'presentation progress never fabricates exact spoken identity evidence')
})

for(const words of ['拜拜','Goodbye'])test(`a direct goodbye ends the call without a backend end_call: ${words}`,async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=1;f.c.endGoodbyeMaxMs=1;f.c.state.status='connected'
 let ends=0;f.c.disconnect=()=>{ends++;f.c.state.status='disconnected';f.c.resetSessionReports()}
 f.c.handleLiveEvent({type:'session.input_transcript.delta',delta:words})
 for(let i=0;i<100&&!ends;i++)await new Promise(r=>setTimeout(r,5))
 assert.equal(ends,1);assert.equal(f.sent.some(e=>e.item?.name==='end_call'),false)
})

test('continuing a completed report does not reread or restart the directory',async()=>{
 for(const live of [true,false]){
  const f=fixture(false,null);f.c.live=live;f.release()
  const row={session_id:'one',label:'Completed item'}
  f.c.sessionReports.start({sessions:[row]},async()=>assert.fail('no next read'),()=>{})
  f.c.sessionReports.played([{id:'one',label:row.label}])
  assert.equal(f.c.sessionReports.hasPending(),false)
  if(live)await f.c.runLiveTool('completed','get_sessions_summary',JSON.stringify({continue_report:true}))
  else await f.c.handleToolCall({call_id:'completed',name:'get_sessions_summary',arguments:JSON.stringify({continue_report:true})})
  assert.deepEqual(f.reads,[],'continuation must not start another directory query')
 }
})

function messageFixture(live){
 const f=fixture(false);f.c.live=live;f.c.state.status='connected'
 let current={...row},posts=0,levels={input:0,output:0},cues=0
 f.c.audioEl={paused:false,muted:false}
 f.c.meter={read:()=>levels,pending:()=>cues++,chime(){}}
 f.c.toolDeps=()=>({sleep:async()=>{},fetch:async(path,init)=>{
  if(path==='/v1/sessions')return Response.json({sessions:[current]})
  if(path.endsWith('/send')){posts++;return Response.json({ok:true,id:row.id,action:'typed'})}
  return Response.json({})
 }})
 const message='Please investigate the sound.'
 const args=(text=message)=>JSON.stringify({session_id:row.id,session_label:row.label,message:text,switch_confirmed:true})
 const send=async(text)=>live?f.c.runLiveTool(crypto.randomUUID(),'send_message_to_session',args(text)):f.c.handleToolCall({call_id:crypto.randomUUID(),name:'send_message_to_session',arguments:args(text)})
 const speak=async(text,interrupted=false)=>{
  if(live){f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:text});levels={input:0,output:0.5};f.c.readLevels();levels={input:0,output:0}}
  else{
   await f.c.handleRealtimeEvent({type:'response.created',response:{id:'readback'}})
   await f.c.handleRealtimeEvent({type:'response.output_audio_transcript.delta',delta:text})
   await f.c.handleRealtimeEvent({type:'response.done',response:{id:'readback',status:'completed',output:[]}})
   await f.c.handleRealtimeEvent({type:interrupted?'output_audio_buffer.cleared':'output_audio_buffer.stopped'})
  }
 }
 const answer=async(text)=>{
  if(live)f.c.handleLiveEvent({type:'session.input_transcript.delta',delta:text})
  else{
   await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'approval'})
   await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_stopped'})
   await f.c.handleRealtimeEvent({type:'conversation.item.input_audio_transcription.completed',item_id:'approval',transcript:text})
   await f.c.handleRealtimeEvent({type:'response.created',response:{id:'after-approval'}})
  }
 }
 return {...f,message,send,speak,answer,posts:()=>posts,cues:()=>cues,replace:()=>{current={...row,sessionId:'replacement-conversation'}}}
}


test('visible saving, reading, responding, planning and queued states never trigger periodic execution sounds',async()=>{
 const realNow=Date.now;let now=100000,cues=0;Date.now=()=>now;const f=fixture(true)
 f.c.state.status='connected';f.c.meter={pending:()=>cues++,chime(){}}
 try{await withLedger(f,async(state)=>{
  state.inputs.push({id:'source',call:'test-call',text:'Read status',planned:false,deferred:false,at:1})
  state.tasks.push({id:'task',tool:'get_session_summary',text:'Read status',args:{},sources:['source'],status:'queued',version:1})
  for(const status of ['queued','waiting_confirmation','failed','done']){
   state.tasks[0].status=status
   for(const activity of ['saving','reading','responding','planning']){
    f.c.receivingInputs.clear();f.c.pendingTools.clear();f.c.awaitingResponse=false
    if(activity==='saving')f.c.receivingInputs.add('input')
    if(activity==='reading')f.c.pendingTools.add('read')
    if(activity==='responding')f.c.awaitingResponse=true
    for(let i=0;i<4;i++){now+=4000;f.c.updateState({})}
   }
  }
  assert.equal(cues,0)
  state.tasks[0].status='running';f.c.updateState({});assert.equal(cues,1)
  now+=4000;f.c.updateState({});assert.equal(cues,1)
  f.c.playing=true;now+=4000;f.c.updateState({});assert.equal(cues,1)
  f.c.playing=false;state.tasks[0].status='done';now+=4000;f.c.updateState({});assert.equal(cues,1)
 })}finally{Date.now=realNow}
})

test('changing the call during recipient resolution blocks the admitted transport effect',async()=>{
 const f=messageFixture(false)
 const base=f.c.toolDeps;let release
 f.c.toolDeps=()=>({...base(),fetch:async(path,init)=>{if(path==='/v1/sessions')await new Promise(resolve=>release=resolve);return base().fetch(path,init)}})
 const sending=f.send();await tick();f.c.attempt++;release();await sending
 assert.equal(f.posts(),0)
})

test('the call tool history survives the next Live answer and Realtime utterance',async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.state.status='connected'
 await f.c.runLiveTool('history-list','list_sessions','{}')
 assert.ok(f.c.state.tools.some(t=>t.name==='list_sessions'))
 f.c.handleLiveEvent({type:'session.input_transcript.delta',delta:'Tell me more.'})
 f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:'Here is the summary.'})
 assert.ok(f.c.state.tools.some(t=>t.name==='list_sessions'),'Live speech must preserve this call history')
 await f.c.handleRealtimeEvent({type:'input_audio_buffer.speech_started',item_id:'next'})
 assert.ok(f.c.state.tools.some(t=>t.name==='list_sessions'),'new speech must preserve history')
})

test('automatic opening exposes each background Session transcript read in the tool history',async()=>{
 const f=fixture(false,null);f.c.state.status='connected'
 const preparing=f.c.prepareListReport();await tick();f.release();await preparing;await tick()
 assert.ok(f.reads.some(r=>r.path.startsWith('/v1/transcript?')))
 assert.equal(f.c.state.tools.filter(t=>t.name==='read_session_transcript').length,2)
 assert.ok(f.c.state.tools.some(t=>t.name==='get_sessions_summary'&&t.source==='background'))
 f.c.resetSessionReports()
})


test('background history journal events carry the same unique IDs, outcomes and durations as the visible call ledger',async()=>{
 const f=fixture(false,null);f.c.journalCall='history-journal-call';f.c.state.status='connected'
 const reading=f.c.prepareListReport();await tick();f.release();await reading;await tick()
 const ended=f.c.state.tools.filter(row=>row.outcome!=='running')
 const saved=f.c.journalLines.filter(row=>row.text==='call_tool_operation').map(row=>row.tool)
 assert.equal(saved.length,ended.length)
 assert.deepEqual(new Set(saved.map(row=>row.id)),new Set(ended.map(row=>row.id)))
 for(const row of saved){assert.equal(row.call,'history-journal-call');assert.equal(row.source,'background');assert.ok(row.ms>=0);assert.ok(row.ended>=row.started)}
 f.c.resetSessionReports()
})

test('cancelled background reads settle in the call history and never return as successful entries',async()=>{
 const f=fixture(false,null);f.c.state.status='connected'
 const original=f.c.toolDeps
 f.c.toolDeps=()=>({...original(),fetch:async(path,init)=>{
  if(path.startsWith('/v1/transcript?'))return await new Promise((resolve,reject)=>init.signal.addEventListener('abort',()=>reject(new DOMException('Cancelled','AbortError')),{once:true}))
  return original().fetch(path,init)
 }})
 const reading=f.c.prepareListReport();await tick();f.c.cancelOpening();await reading
 const transcripts=f.c.state.tools.filter(row=>row.name==='read_session_transcript')
 assert.equal(transcripts.length,2)
 assert.ok(transcripts.every(row=>row.outcome==='cancelled'))
 assert.ok(!f.c.state.tools.some(row=>row.outcome==='running'))
})

for(const request of ['請介紹其他 Session 的狀況','請你去介紹一下其他 Session','Please introduce the other sessions'])test(`an explicit other-Session request advances past the interrupted item: ${request}`,async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=1;f.c.livePollMs=1
 const first={session_id:'one',label:'First work'},next={session_id:'two',label:'Second work'}
 f.c.sessionReports.start({sessions:[first,next]},async()=>assert.fail('prepared items must be reused'),page=>f.c.presentSessionReport(page,()=>f.c.dc?.readyState==='open'))
 f.c.liveReportItem={ref:{id:first.session_id,label:first.label},text:'An interrupted explanation.',interrupted:true}
 f.c.sessionReports.interrupt()
 f.c.liveHeard.add(request);f.c.heardNow=request
 await f.c.runLiveTool('other-items','get_sessions_summary',JSON.stringify({continue_report:true}))
 await tick()
 const evidence=f.sent.filter(e=>e.type==='session.thinking.append'&&e.content.startsWith('Machine evidence')).map(e=>JSON.parse(e.content.slice(e.content.indexOf('{'))))
 assert.deepEqual(evidence.at(-1)?.sessions.map(r=>r.session_id),['two'])
 assert.equal(f.c.liveReportItem.ref.id,'two')
 assert.deepEqual(f.c.spokenSessions.heardIDs(),[],'skipping is not completed playback or action-target evidence')
 assert.equal(f.reads.length,0,'moving to prepared content must not reread or execute work')
})


test('continuation retains an interrupted item while repeated next requests cannot skip twice in one turn',async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=1;f.c.livePollMs=1
 const refs=[{session_id:'one',label:'First work'},{session_id:'two',label:'Second work'},{session_id:'three',label:'Third work'}]
 f.c.sessionReports.start({sessions:refs},async()=>assert.fail('no reads'),()=>{})
 f.c.liveReportItem={ref:{id:'one',label:'First work'},text:'Partial explanation.',interrupted:true}
 f.c.sessionReports.interrupt();f.c.liveHeard.add('繼續報告');f.c.heardNow='繼續報告'
 f.c.continueSessionReport();assert.equal(f.c.sessionReports.current().id,'one')
 f.c.questionSerial++;f.c.liveHeard.add('請介紹其他 Session 的進度');f.c.continueSessionReport()
 assert.equal(f.c.sessionReports.current().id,'two')
 f.c.liveReportItem={ref:{id:'two',label:'Second work'},text:'',interrupted:true}
 f.c.sessionReports.interrupt();f.c.continueSessionReport('請介紹其他 Session 的進度')
 assert.equal(f.c.sessionReports.current().id,'two','a second callback for the same utterance is not another skip')
 f.c.questionSerial++;f.c.continueSessionReport('請介紹其他 Session 的進度')
 assert.equal(f.c.sessionReports.current().id,'three','a fresh user turn may ask for another item')
 assert.deepEqual(f.c.spokenSessions.heardIDs(),[])
})

test('Live journals the completed item before moving to the next narration item',async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.liveQuietMs=1;f.c.livePollMs=1;f.c.liveStartGraceMs=20
 const saved=[];f.c.journal=(kind,text)=>saved.push({kind,text});f.c.flushJournal=()=>{}
 const rows=[{session_id:'one',label:'First work'},{session_id:'two',label:'Second work'}]
 f.c.dc.send=text=>{const e=JSON.parse(text);f.sent.push(e);if(e.type==='session.commentary.append')queueMicrotask(()=>f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:e.content.includes('First work')?'First explanation.':'Second explanation.'}))}
 await f.c.presentLiveReport({sessions:rows},()=>f.c.dc?.readyState==='open')
 assert.deepEqual(saved.filter(e=>e.kind==='said').map(e=>e.text),['First explanation.'])
 assert.equal(f.c.liveSaid.peek(),'Second explanation.','the final item remains available for the normal journal flush')
})

test('tool starts sound once, nested reads do not repeat it, and a long wait is silent',()=>{
 const f=fixture(false);f.c.live=true;f.c.state.status='connected';f.c.state.connectionReady=true
 let sounds=0;f.c.meter={read:()=>({input:0,output:0}),pending:()=>sounds++}
 const root=f.c.observeOperation({name:'list_sessions',source:'tool'})
 const child=f.c.observeOperation({name:'read_session_transcript',source:'background',parent:root.id})
 assert.equal(sounds,1)
 for(let i=0;i<8;i++)f.c.updateState({})
 assert.equal(sounds,1,'waiting is not another tool start')
 child.end({outcome:'done'});root.end({outcome:'done'});assert.equal(sounds,1)
 f.c.observeOperation({name:'get_sessions_summary',source:'background'});assert.equal(sounds,2)
})

for(const live of [false,true])for(const managed of [false,true])test(`the voice model decides message confirmation without a local transcript gate (live=${live}, managed=${managed})`,async()=>{
 const f=fixture(managed);f.c.live=live;f.c.call.options.messageActions=true
 let sends=0
 f.c.toolDeps=()=>({sleep:async()=>{},fetch:async(path,init)=>{
  if(path==='/v1/sessions')return Response.json({sessions:[row]})
  if(path.endsWith('/send')){sends++;assert.equal(JSON.parse(init.body).text,'Keep the dial tone.');return Response.json({ok:true,id:row.id,action:'typed'})}
  return Response.json({})
 }})
 const args=JSON.stringify({session_id:row.id,message:'Keep the dial tone.',session_label:row.label})
 if(live)await f.c.runLiveTool('model-approved','send_message_to_session',args)
 else await f.c.handleToolCall({call_id:'model-approved',name:'send_message_to_session',arguments:args})
 assert.equal(sends,1)
 assert.equal(f.c.state.messageProposal,undefined)
})

for(const managed of [false,true])test(`message tools carry conversational confirmation instructions without a second local gate (managed=${managed})`,async()=>{
 const f=fixture(managed);f.release();await f.c.sendSessionConfiguration()
 const session=f.sent.find(e=>e.type==='session.update').session
 const tool=session.tools.find(t=>t.name==='send_message_to_session')
 assert.ok(tool)
 assert.match(tool.description,/obtained their agreement/)
 assert.match(session.instructions,/先完整說明收件對象與訊息全文/)
 assert.doesNotMatch(tool.description,/only prepares|durable proposal/)
})

test('Session narration changes use a separate melody once per item, without cues for read-ahead',async()=>{
 const f=fixture(false,null);f.c.live=true;f.c.state.status='connected';f.c.liveQuietMs=1;f.c.livePollMs=1
 let switches=0;f.c.meter={sessionChanged:()=>switches++}
 f.c.observeOperation({name:'read_session_summary',source:'background',target:'Another work'})
 assert.equal(switches,0,'read-ahead alone is not a Session switch')
 f.c.dc.send=text=>{const e=JSON.parse(text);f.sent.push(e);if(e.type==='session.commentary.append')queueMicrotask(()=>f.c.handleLiveEvent({type:'session.output_transcript.delta',delta:'A complete synthetic explanation.'}))}
 const first={session_id:'one',label:'First work'},second={session_id:'two',label:'Second work'}
 await f.c.presentLiveReport({sessions:[first,second]},()=>true)
 assert.equal(switches,2)
 await f.c.presentLiveReport({sessions:[second]},()=>true)
 assert.equal(switches,2,'continuing the same item is not another Session switch')
})

test('Realtime Session name anchors cue each next item without repeating earlier names',async()=>{
 const f=fixture(false,null);f.c.state.status='connected';let switches=0;f.c.meter={sessionChanged:()=>switches++}
 f.c.presentSessionReport({sessions:[{session_id:'one',label:'First work'},{session_id:'two',label:'Second work'}]},()=>true)
 await f.c.handleRealtimeEvent({type:'response.created',response:{id:'cue-names'}})
 for(const [delta,count] of [['First work is being reviewed.',1],[' Second work has completed.',2],[' Its next step is verification.',2]]){
  await f.c.handleRealtimeEvent({type:'response.output_audio_transcript.delta',delta});assert.equal(switches,count)
 }
})
