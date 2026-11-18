import {test} from 'node:test'
import assert from 'node:assert/strict'
import {build} from 'esbuild'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
const {outputFiles}=await build({stdin:{contents:'export {CallMeter} from "./copilot-meter.ts"',resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false})
const module={exports:{}};new Function('require','module','exports',outputFiles[0].text)(createRequire(import.meta.url),module,module.exports)
const {CallMeter}=module.exports
function context(){
 const tones=[],gains=[],events=new EventTarget()
 const audio={state:'running',currentTime:1,destination:{},tones,gains,
  addEventListener:events.addEventListener.bind(events),removeEventListener:events.removeEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events),
  createGain(){const events=[];const node={events,disconnected:false,connect(){},disconnect(){this.disconnected=true},gain:{setValueAtTime(value,at){events.push(['set',value,at])},linearRampToValueAtTime(value,at){events.push(['ramp',value,at])}}};gains.push(node);return node},
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
