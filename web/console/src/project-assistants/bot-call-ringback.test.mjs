import {test} from 'node:test'
import assert from 'node:assert/strict'
import {build} from 'esbuild'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'

// The Bot's browser call rings, as a telephone does, while it opens, and at least one whole ring.
const {outputFiles}=await build({stdin:{contents:'export {BotCall,CallAudioError} from "./bot-call.ts"; export {callRingback} from "../realtime/copilot-meter.ts"',resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false})
const module={exports:{}};new Function('require','module','exports',outputFiles[0].text)(createRequire(import.meta.url),module,module.exports)
const {BotCall,CallAudioError,callRingback}=module.exports

const fence={key:{machine_id:'source',bot_id:'bot'},conversation_id:'conversation',binding_id:'',generation:0,
  project:{machine_id:'source',project_id:'project',canonical_id:'project',kind:'repository'}}

/** A fake browser audio stack that records what sounds and what is closed. */
function speakers(state='running'){
  const made=[]
  class FakeContext extends EventTarget{
    constructor(){super();this.state=state;this.currentTime=1;this.destination={};this.tones=[];this.closed=0;made.push(this)}
    resume(){return Promise.resolve()}
    close(){this.closed++;this.state='closed';return Promise.resolve()}
    createGain(){const gain={connect(){},disconnect(){},gain:{value:0,cancelScheduledValues(){},setValueAtTime(){},linearRampToValueAtTime(){}}};return gain}
    createOscillator(){const ctx=this;const tone={frequency:{value:0},playing:false,connect(){},disconnect(){},start(){tone.playing=true;ctx.tones.push(tone)},stop(){tone.playing=false}};return tone}
  }
  const before=Object.getOwnPropertyDescriptor(globalThis,'window')
  Object.defineProperty(globalThis,'window',{configurable:true,value:{AudioContext:FakeContext}})
  return {made,
    sounding:()=>made.some(ctx=>ctx.state!=='closed'&&ctx.tones.some(tone=>tone.playing)),
    hz:()=>made.flatMap(ctx=>ctx.tones.map(tone=>tone.frequency.value)),
    restore(){if(before)Object.defineProperty(globalThis,'window',before);else Reflect.deleteProperty(globalThis,'window')}}
}

function call({open}){
  const phases=[]
  const ports={
    current:()=>({owner:'account/source',fence,activeTurn:'',turns:[],messages:[],caughtUp:true,frontier:0}),
    submit:async()=>({state:'admitted',turn:'turn-1',eventSeq:1}),
    changed:state=>phases.push(state.phase),
    ringback:()=>callRingback(),
  }
  return {call:new BotCall(ports),phases,source:{open}}
}

const audio={listen(){},play:()=>Promise.resolve(),close(){}}

test('placing a Bot call rings at once and finishes that ring after it is listening', async t=>{
  t.mock.timers.enable({apis:['setInterval','setTimeout']})
  const out=speakers()
  try{
    let answer
    const c=call({open:()=>new Promise(resolve=>{answer=resolve})})
    const started=c.call.start(c.source)
    assert.equal(c.call.state.phase,'opening')
    assert.ok(out.sounding(),'the ringback sounds while the call opens')
    assert.deepEqual(out.hz(),[440,480])
    c.call.mute()
    assert.ok(out.sounding(),'muting does not end the opening ring')
    answer(audio);await started
    assert.equal(c.call.state.phase,'listening')
    assert.ok(out.sounding(),'an answer inside the first ring lets that ring finish')
    t.mock.timers.tick(1100)
    assert.equal(out.sounding(),false,'listening leaves no ring behind once the ring ends')
    t.mock.timers.tick(10000)
    assert.equal(out.made.every(ctx=>ctx.closed===1),true,'the ring context is closed once')
  }finally{out.restore()}
})

test('a call answered at once still rings one whole ring, and a hangup cuts it short', async t=>{
  t.mock.timers.enable({apis:['setInterval','setTimeout']})
  const out=speakers()
  try{
    const quick=call({open:async()=>audio})
    await quick.call.start(quick.source)
    assert.equal(quick.call.state.phase,'listening')
    t.mock.timers.tick(500)
    assert.ok(out.sounding(),'half a second after answering the first ring still sounds')
    t.mock.timers.tick(600)
    assert.equal(out.sounding(),false)

    const hung=call({open:async()=>audio})
    await hung.call.start(hung.source)
    assert.ok(out.sounding())
    hung.call.end()
    assert.equal(out.sounding(),false,'hanging up silences the ring at once')
    assert.equal(out.made.every(ctx=>ctx.closed===1),true)
  }finally{out.restore()}
})

test('a greeting call stops ringing after the ring it sends the greeting in', async t=>{
  t.mock.timers.enable({apis:['setInterval','setTimeout']})
  const out=speakers()
  try{
    const c=call({open:async()=>audio})
    const started=c.call.start(c.source,true)
    assert.ok(out.sounding())
    await started
    assert.notEqual(c.call.state.phase,'opening')
    t.mock.timers.tick(1100)
    assert.equal(out.sounding(),false)
  }finally{out.restore()}
})

test('a refused, failed or hung-up opening never leaves the ring playing', async t=>{
  t.mock.timers.enable({apis:['setInterval']})
  const out=speakers()
  try{
    const refused=call({open:async()=>{throw new CallAudioError('language')}})
    await refused.call.start(refused.source)
    assert.equal(refused.call.state.phase,'language')
    assert.equal(out.sounding(),false)

    let fail
    const dropped=call({open:(_signal,_heard,failed)=>new Promise(()=>{fail=failed})})
    void dropped.call.start(dropped.source)
    assert.ok(out.sounding())
    fail('disconnected')
    assert.equal(dropped.call.state.phase,'disconnected')
    assert.equal(out.sounding(),false)

    const hung=call({open:()=>new Promise(()=>{})})
    void hung.call.start(hung.source)
    assert.ok(out.sounding())
    hung.call.end()
    assert.equal(hung.call.state.phase,'idle')
    assert.equal(out.sounding(),false)
    assert.equal(out.made.every(ctx=>ctx.closed===1),true)
  }finally{out.restore()}
})

test('a call opened outside a tap rings once the browser lets its audio run', async t=>{
  t.mock.timers.enable({apis:['setInterval']})
  const out=speakers('suspended')
  try{
    const c=call({open:()=>new Promise(()=>{})})
    void c.call.start(c.source)
    assert.equal(out.sounding(),false)
    const [ctx]=out.made
    ctx.state='running';ctx.dispatchEvent(new Event('statechange'))
    assert.ok(out.sounding())
    c.call.end()
    assert.equal(out.sounding(),false)
  }finally{out.restore()}
})
