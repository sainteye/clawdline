import {test} from 'node:test'
import assert from 'node:assert/strict'
// @ts-expect-error -- Node executes the TypeScript source.
import {WorkNarrator} from './narrator.ts'
test('prepared results wait for generation and actual audio playback to finish',()=>{
 const sent:Record<string,unknown>[]=[];const n=new WorkNarrator(e=>sent.push(e),()=> 'zh-Hant')
 n.event('response.created');n.event('response.output_audio.delta')
 n.prepared('first','first result');n.event('response.done');assert.equal(sent.length,0)
 n.event('output_audio_buffer.stopped');assert.equal(sent.length,1)
 n.prepared('second','second result');assert.equal(sent.length,1)
 n.event('response.created');n.event('response.done');assert.equal(sent.length,2)
})
test('continued speech waits for a durable acknowledgement and coalesces superseded updates',()=>{
 const sent:Record<string,unknown>[]=[];const n=new WorkNarrator(e=>sent.push(e),()=> 'en')
 n.event('input_audio_buffer.speech_started');n.prepared('same task','old');n.prepared('same task','new')
 n.event('input_audio_buffer.speech_stopped');assert.equal(sent.length,0)
 n.received();assert.equal(sent.length,1);assert.match(JSON.stringify(sent[0]),/new/);assert.doesNotMatch(JSON.stringify(sent[0]),/Evidence: old/)
 n.reset();n.event('response.done');assert.equal(sent.length,1)
})
