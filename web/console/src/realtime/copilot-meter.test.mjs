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
