import {test} from 'node:test'
import assert from 'node:assert/strict'
import {build} from 'esbuild'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
const {outputFiles}=await build({stdin:{contents:'export {CallMeter} from "./copilot-meter.ts"; export {borrowAudioSession} from "./chime.ts"',resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false})
const module={exports:{}};new Function('require','module','exports',outputFiles[0].text)(createRequire(import.meta.url),module,module.exports)
const {CallMeter,borrowAudioSession}=module.exports
function context(){
 const tones=[],gains=[],events=new EventTarget()
 const audio={state:'running',currentTime:1,destination:{},tones,gains,
  resume:async()=>{},
  addEventListener:events.addEventListener.bind(events),removeEventListener:events.removeEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events),
  createGain(){const events=[];const node={events,disconnected:false,connect(){},disconnect(){this.disconnected=true},gain:{cancelScheduledValues(at){events.push(['cancel',at])},setValueAtTime(value,at){events.push(['set',value,at])},linearRampToValueAtTime(value,at){events.push(['ramp',value,at])}}};gains.push(node);return node},
  createOscillator(){return {type:'sine',frequency:{value:0},onended:undefined,disconnected:false,connect(){},disconnect(){this.disconnected=true},start(at){this.record={hz:this.frequency.value,type:this.type,at};tones.push(this.record)},stop(at){if(this.record)this.record.stoppedAt=at;this.onended?.()}}},
 }
 return audio
}
test('ready and processing cues still sound after the completion chime moves to its shared module',()=>{
 const meter=new CallMeter(),audio=context();meter.context=audio
 meter.connected();assert.deepEqual(audio.tones.map(t=>t.hz),[480,660])
 assert.ok(audio.tones[1].at>audio.tones[0].at)
 meter.pending();assert.deepEqual(audio.tones.map(t=>t.hz),[480,660,330])
 assert.equal(audio.gains.every(g=>g.disconnected),true)
 audio.state='suspended';meter.pending();meter.connected();assert.equal(audio.tones.length,3)
})

test('ending a local call detaches capture before a descending cue and closes only its own context',()=>{
 const meter=new CallMeter(),audio=context();audio.closed=0;audio.close=async()=>{audio.closed++};meter.context=audio
 meter.close(true)
 assert.deepEqual(audio.tones.map(t=>t.hz),[660,480]);assert.equal(audio.closed,1);assert.equal(meter.context,null)
 meter.close(true);assert.equal(audio.tones.length,2,'repeated close cannot replay the hangup sound')
})

test('an interrupted hangup cue releases its context even when its ended event never arrives',async()=>{
 const meter=new CallMeter(),audio=context(),nodes=[]
 audio.closed=0;audio.close=async()=>{audio.closed++}
 const make=audio.createOscillator.bind(audio)
 audio.createOscillator=()=>{const tone=make();tone.stop=()=>{};nodes.push(tone);return tone}
 meter.context=audio;meter.close(true)
 assert.deepEqual(audio.tones.map(t=>t.hz),[660,480]);assert.equal(audio.closed,0)
 const next=context();meter.context=next
 audio.state='suspended';audio.dispatchEvent(new Event('statechange'))
 assert.equal(audio.closed,1,'backgrounding must not leave the old audio session open')
 assert.equal(meter.context,next,'the next call retains its own context')
 nodes.at(-1).onended?.();assert.equal(audio.closed,1,'late ended events cannot close twice')
})

test('entering a Session has a distinct short local melody and releases its nodes',()=>{
 const meter=new CallMeter(),audio=context();meter.context=audio
 meter.sessionChanged();assert.deepEqual(audio.tones.map(t=>t.hz),[660,880,660]);assert.equal(audio.gains.every(g=>g.disconnected),true)
})


test('background processing is one lower triangle note, distinct from the active tool sine note',()=>{
 const meter=new CallMeter(),audio=context();meter.context=audio
 meter.pending('background');assert.deepEqual(audio.tones.map(t=>({hz:t.hz,type:t.type})),[{hz:220,type:'triangle'}])
 meter.pending('tool');assert.deepEqual(audio.tones.map(t=>({hz:t.hz,type:t.type})),[{hz:220,type:'triangle'},{hz:330,type:'sine'}])
 assert.equal(audio.gains.every(g=>g.disconnected),true)
 assert.equal(audio.tones[0].stoppedAt-audio.tones[0].at, audio.tones[1].stoppedAt-audio.tones[1].at)
 assert.deepEqual(audio.gains[0].events,audio.gains[1].events,'the duration and loudness envelope remain unchanged')
 audio.state='suspended';meter.pending('background');assert.equal(audio.tones.length,2)
})

test('ringback starts on audio resume and stops on the call boundary', t => {
  t.mock.timers.enable({apis:['setInterval']})
  const meter = new CallMeter(), audio = context(), states = []
  meter.context = audio
  audio.state = 'suspended'
  const stop = meter.ring(state => states.push(state))
  t.mock.timers.tick(5000)
  assert.deepEqual(audio.tones, [])
  audio.state = 'running'
  audio.dispatchEvent(new Event('statechange'))
  assert.deepEqual(audio.tones.map(tone => tone.hz), [440, 480])
  assert.ok(audio.gains[0].events.some(([event, level]) => event === 'ramp' && level > 0))
  audio.state = 'suspended'
  audio.dispatchEvent(new Event('statechange'))
  const paused = audio.gains[0].events.length
  t.mock.timers.tick(5000)
  assert.equal(audio.gains[0].events.length, paused)
  audio.state = 'running'
  audio.dispatchEvent(new Event('statechange'))
  assert.ok(audio.gains[0].events.length > paused, 'recovery starts a fresh ring immediately')
  stop()
  const finished = audio.gains[0].events.length
  audio.dispatchEvent(new Event('statechange'))
  t.mock.timers.tick(5000)
  assert.equal(audio.gains[0].events.length, finished)
  assert.deepEqual(states, ['suspended', 'running', 'suspended', 'running'])
  assert.ok(audio.gains[0].disconnected)
})

test('audio recovery plays a pending tool cue only while its operation is still active',async()=>{
 const meter=new CallMeter(),audio=context()
 meter.context=audio
 audio.state='suspended'
 let resume
 audio.resume=()=>new Promise(resolve=>{resume=resolve})
 let active=true
 meter.pending('tool',()=>active)
 assert.deepEqual(audio.tones,[])
 assert.equal(typeof resume,'function')
 audio.state='running';resume();await Promise.resolve()
 assert.deepEqual(audio.tones.map(t=>t.hz),[330])

 audio.state='suspended'
 meter.pending('background',()=>active)
 active=false
 audio.state='running';resume();await Promise.resolve()
 assert.deepEqual(audio.tones.map(t=>t.hz),[330])
})

test('an audio mode borrowed for a call is restored after hangup playback',t=>{
 const oldWindow=globalThis.window
 const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator')
 const session={type:'auto'}
 const audio=context()
 audio.close=async()=>{audio.state='closed'}
 globalThis.window={AudioContext:class{constructor(){return audio}}}
 Object.defineProperty(globalThis,'navigator',{value:{audioSession:session},configurable:true})
 t.after(()=>{
  globalThis.window=oldWindow
  if(oldNavigator)Object.defineProperty(globalThis,'navigator',oldNavigator)
  else delete globalThis.navigator
 })
 const meter=new CallMeter()
 meter.open()
 assert.equal(session.type,'play-and-record')
 meter.close(true)
 assert.equal(session.type,'auto')
 const cue=borrowAudioSession('playback')
 assert.equal(session.type,'playback')
 cue()
 assert.equal(session.type,'auto')
})

test('a suspended hangup cue can play on prompt recovery and closes on timeout',async t=>{
 t.mock.timers.enable({apis:['setTimeout']})
 const meter=new CallMeter(),audio=context()
 meter.context=audio
 audio.state='suspended'
 audio.close=async()=>{audio.state='closed'}
 let resume
 audio.resume=()=>new Promise(resolve=>{resume=resolve})
 meter.close(true)
 assert.equal(audio.state,'suspended')
 audio.state='running';resume();await Promise.resolve()
 assert.deepEqual(audio.tones.map(t=>t.hz),[660,480])
 assert.equal(audio.state,'closed')

 const stalled=context()
 meter.context=stalled
 stalled.state='suspended'
 stalled.resume=()=>new Promise(()=>{})
 stalled.close=async()=>{stalled.state='closed'}
 meter.close(true)
 t.mock.timers.tick(1000)
 assert.equal(stalled.state,'closed')
 assert.deepEqual(stalled.tones,[])
})
