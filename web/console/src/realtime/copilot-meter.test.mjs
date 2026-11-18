import {test} from 'node:test'
import assert from 'node:assert/strict'
import {build} from 'esbuild'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
const {outputFiles}=await build({stdin:{contents:'export {CallMeter} from "./copilot-meter.ts"',resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},bundle:true,platform:'node',format:'cjs',write:false})
const module={exports:{}};new Function('require','module','exports',outputFiles[0].text)(createRequire(import.meta.url),module,module.exports)
const {CallMeter}=module.exports
function context(){
 const tones=[],gains=[]
 const audio={state:'running',currentTime:1,destination:{},tones,gains,
  createGain(){const node={disconnected:false,connect(){},disconnect(){this.disconnected=true},gain:{setValueAtTime(){},linearRampToValueAtTime(){}}};gains.push(node);return node},
  createOscillator(){return {frequency:{value:0},onended:undefined,disconnected:false,connect(){},disconnect(){this.disconnected=true},start(at){tones.push({hz:this.frequency.value,at})},stop(){this.onended?.()}}},
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

test('entering a Session has a distinct short local melody and releases its nodes',()=>{
 const meter=new CallMeter(),audio=context();meter.context=audio
 meter.sessionChanged();assert.deepEqual(audio.tones.map(t=>t.hz),[660,880,660]);assert.equal(audio.gains.every(g=>g.disconnected),true)
})
