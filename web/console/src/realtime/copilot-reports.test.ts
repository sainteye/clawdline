import {test} from 'node:test'
import assert from 'node:assert/strict'
// @ts-expect-error -- Node runs the TypeScript source.
import {SpokenSessions,SessionReportQueue,relativeSession,sessionBatchSize,sessionNamePosition,type SessionReportPage} from './copilot-reports.ts'
const a={id:'one',label:'Browser keyboard',machine:'machine-a',conversation:'conversation-a'}
const b={id:'two',label:'Voice workflow',machine:'machine-b',conversation:'conversation-b'}
const c={id:'three',label:'Board planning',machine:'machine-a',conversation:'conversation-c'}
const tick=()=>new Promise<void>(resolve=>setImmediate(resolve))
test('relative references use actual completed speech order, not read or directory order',()=>{
 const spoken=new SpokenSessions();for(const ref of [a,b,c])spoken.observe(ref)
 spoken.generated('Board planning. Browser keyboard. Voice workflow.',true)
 assert.equal(spoken.resolve('你最後提到的 Session')?.target,undefined,'generated is not heard')
 spoken.played()
 assert.equal(spoken.resolve('你最後提到那個 Session')?.target?.id,'two')
 assert.equal(spoken.resolve('你講的第二個 Session')?.target?.id,'one')
 assert.equal(spoken.resolve('the third session')?.target?.id,'two')
 spoken.generated('Browser keyboard.',true);spoken.played()
 assert.equal(spoken.resolve('the last one')?.target?.id,'one')
 assert.equal(spoken.resolve('the second session')?.target?.id,'one','a detail preserves the previous list')
 spoken.reset();assert.equal(spoken.resolve('the second session')?.target,undefined)
})
test('a spoken list on 所有機器 binds positions even though labels carry the machine name',()=>{
 // Call f91a9eb8 on 2026-11-13: three idle Sessions were read aloud without
 // their （machine） suffix, and 「跟我講你最後的那一個」 was answered with the second.
 const m=(id:string,label:string)=>({id,label:`${label}\uff08work-box\uff09`,machine:'mac-1',conversation:id})
 const x=m('s-first','對齊 CodyNC：以瀏覽器實測迭代修正 Clawdline UI'),y=m('s-second','閱讀交接摘要並接續里程碑'),z=m('s-third','Clawdline 語音與清單延遲瓶頸盤查')
 const spoken=new SpokenSessions();for(const ref of [x,y,z])spoken.observe(ref)
 spoken.generated('目前有三個閒置的 Session。第一個是「對齊 CodyNC：以瀏覽器實測迭代修正 Clawdline UI」，第二個是「閱讀交接摘要並接續里程碑」，第三個是「Clawdline 語音與清單延遲瓶頸盤查」。',true);spoken.played()
 for(const words of ['跟我講你最後的那一個','最後那個','最後那一個呢','the last one'])assert.equal(spoken.resolve(words)?.target?.id,'s-third',words)
 assert.equal(spoken.resolve('第二個')?.target?.id,'s-second')
 spoken.reset();spoken.observe(x);spoken.observe({...x,id:'s-other',machine:'mac-2',label:'對齊 CodyNC：以瀏覽器實測迭代修正 Clawdline UI\uff08home-box\uff09'})
 spoken.generated('「對齊 CodyNC：以瀏覽器實測迭代修正 Clawdline UI」',true);spoken.played()
 assert.equal(spoken.resolve('最後那個')?.target,undefined,'the same name on two machines is not guessed')
})
test('positional words are recognized as positions, not handed to a guess',()=>{
 for(const words of ['跟我講你最後的那一個','最後那個','最后那个','最後一個','你最後提到的'])assert.equal(relativeSession(words),'last',words)
 for(const words of ['最後怎麼了','它最後做完什麼'])assert.equal(relativeSession(words),undefined,words)
})
test('interrupted, duplicate-name and unanchored speech cannot silently reuse an old identity',()=>{
 const spoken=new SpokenSessions();spoken.observe(a);spoken.observe(b)
 spoken.generated('Browser keyboard. Voice workflow.',true);spoken.played()
 spoken.generated('Voice workflow. Browser keyboard.',true);spoken.interrupted();spoken.played()
 assert.equal(spoken.resolve('最後一個')?.target,undefined)
 spoken.generated('Browser keyboard. Voice workflow.',true);spoken.played()
 spoken.generated('This other Session is handling something else.',true);spoken.played()
 assert.equal(spoken.resolve('the last one')?.target,undefined)
 spoken.reset();spoken.observe(a);spoken.observe({...a,id:'another',machine:'another-machine'})
 spoken.generated('Browser keyboard.',true);spoken.played()
 assert.equal(spoken.resolve('the last one')?.target,undefined)
})
test('a next batch can read in the background but cannot speak before current playback finishes',async()=>{
 let release!:(page:SessionReportPage)=>void;const held=new Promise<SessionReportPage>(resolve=>release=resolve)
 const spoken:SessionReportPage[]=[],reads:string[][]=[];const queue=new SessionReportQueue()
 const page={sessions:[{session_id:a.id,label:a.label}],remaining:[b,c]}
 queue.start(page,async refs=>{reads.push(refs.map(r=>r.id));return held},p=>spoken.push(p))
 assert.deepEqual(reads,[['two','three']]);assert.equal(spoken.length,0)
 release({sessions:[{session_id:b.id,label:b.label},{session_id:c.id,label:c.label}]});await tick()
 assert.equal(spoken.length,0)
 queue.played([a]);assert.equal(spoken.length,1)
 queue.played([b,c]);assert.equal(queue.hasPending(),false)
})
test('an interruption pauses automatic presentation and a replaced report ignores old preparation',async()=>{
 let release!:(page:SessionReportPage)=>void;const held=new Promise<SessionReportPage>(resolve=>release=resolve)
 const spoken:SessionReportPage[]=[];const queue=new SessionReportQueue()
 queue.start({sessions:[{session_id:a.id,label:a.label}],remaining:[b]},async()=>held,p=>spoken.push(p))
 queue.interrupt();release({sessions:[{session_id:b.id,label:b.label}]});await tick();queue.played([a])
 assert.equal(spoken.length,0);assert.equal(queue.resume(),true);assert.equal(spoken.length,1)
 queue.start({sessions:[{session_id:c.id,label:c.label}]},async()=>assert.fail('no next batch'),p=>spoken.push(p))
 queue.reset();queue.played([c]);assert.equal(queue.hasPending(),false)
})


test('presentation batches use two or three where possible instead of leaving a final single Session',()=>{
 for(const [total,expected] of [[4,[2,2]],[5,[3,2]],[7,[3,2,2]],[8,[3,3,2]]]){
  const sizes:number[]=[];let left=total as number
  while(left){const size=sessionBatchSize(left);sizes.push(size);left-=size}
  assert.deepEqual(sizes,expected)
 }
})


test('a short name inside another word cannot create a spoken identity',()=>{
 assert.equal(sessionNamePosition('The chair is ready.','AI'),-1)
 assert.equal(sessionNamePosition('A guide is ready.','UI'),-1)
 assert.ok(sessionNamePosition('Read 「Work A」.','Work A')>=0)
 assert.ok(sessionNamePosition('Clawdline 語音與清單正在調整。','Clawdline：語音與清單')>=0)
})
