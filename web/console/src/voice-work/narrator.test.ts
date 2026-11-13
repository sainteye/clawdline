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


test('a discarded opening never advances the Session report cursor',()=>{
 const sent:Record<string,unknown>[]=[];let reported=0
 const n=new WorkNarrator(e=>sent.push(e),()=> 'en')
 n.setReady(false)
 n.prepared('opening','old recap','',()=>{reported++})
 n.discard('opening');n.setReady(true)
 assert.equal(sent.length,0);assert.equal(reported,0)
 n.prepared('opening','current recap','',()=>{reported++})
 assert.equal(sent.length,1);assert.equal(reported,1)
})

test('each outstanding transcription is acknowledged before conversation can start',()=>{
 const sent:Record<string,unknown>[]=[];const n=new WorkNarrator(e=>sent.push(e),()=> 'en')
 n.event('input_audio_buffer.speech_started','one');n.event('input_audio_buffer.speech_stopped','one')
 n.event('input_audio_buffer.speech_started','two');n.event('input_audio_buffer.speech_stopped','two')
 n.conversation('one');n.received('one');assert.equal(sent.length,0)
 n.received('two');assert.equal(sent.length,1)
 assert.equal((sent[0].response as {tool_choice:string}).tool_choice,'auto')
})

test('parallel tools produce one continuation after every result, with marks held until submission',()=>{
 const sent:Record<string,unknown>[]=[],marks:string[]=[]
 const n=new WorkNarrator(e=>sent.push(e),()=> 'en')
 n.event('response.created');n.toolStarted('one');n.toolStarted('two')
 n.conversation('same-input',()=>marks.push('one'))
 n.event('response.done')
 assert.equal(sent.length,0);assert.equal(marks.length,0)
 n.conversation('same-input',()=>marks.push('two'));n.toolFinished('one')
 assert.equal(sent.length,0)
 n.toolFinished('two')
 assert.equal(sent.length,1);assert.deepEqual(marks,['one','two'])
 n.event('response.done');assert.equal(sent.length,1)
})
