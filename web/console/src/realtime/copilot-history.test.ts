import {test} from 'node:test'
import assert from 'node:assert/strict'
// @ts-expect-error -- Node executes source TypeScript in tests.
import {CallHistory,historySummary,type CallOperation} from './copilot-history.ts'

test('start and completion update one call-owned operation and keep more than fifty observations',()=>{
 let now=0,wall=1000,records:CallOperation[]=[],events:CallOperation[]=[]
 const history=new CallHistory((rows,event)=>{records=rows;if(event)events.push(event)},()=>now,()=>wall)
 history.reset('call')
 const parent=history.begin({name:'list',source:'tool'})
 assert.equal(records[0].outcome,'running')
 for(let i=0;i<60;i++){const span=history.begin({name:'read',source:'background',parent:parent.id,target:`Session ${i}`});now++;wall++;span.end({outcome:'done'})}
 now=80;wall=1080;parent.end({outcome:'done'});parent.end({outcome:'failed'})
 assert.equal(records.length,61)
 assert.equal(records[0].outcome,'done')
 assert.equal(records[0].ms,80)
 assert.equal(events.filter(row=>row.id===parent.id).length,2,'one start and one finish')
 assert.deepEqual(historySummary(records).tools,1)
 assert.equal(historySummary(records).background,60)
 assert.equal(historySummary(records).activeMs,80,'nested reads do not inflate elapsed time')
})

test('overlapping operations count elapsed coverage once; additive groups still show individual work',()=>{
 let now=0,records:CallOperation[]=[]
 const history=new CallHistory(rows=>records=rows,()=>now,()=>1000+now)
 history.reset('overlap')
 const a=history.begin({name:'summary',source:'background'})
 now=20;const b=history.begin({name:'summary',source:'background'})
 now=70;a.end({outcome:'cached'});now=100;b.end({outcome:'done'})
 const stats=historySummary(records)
 assert.equal(stats.activeMs,100)
 assert.equal(stats.groups[0].ms,150)
 assert.equal(stats.cached,1)
})

test('hangup settles pending observations and a previous call cannot finish into the next call',()=>{
 let now=0,records:CallOperation[]=[]
 const history=new CallHistory(rows=>records=rows,()=>now,()=>1000+now)
 history.reset('old');const old=history.begin({name:'summary',source:'background'})
 now=15;history.cancel();assert.equal(records[0].outcome,'cancelled');assert.equal(records[0].ms,15)
 old.end({outcome:'done'});assert.equal(records[0].outcome,'cancelled')
 history.reset('new');const fresh=history.begin({name:'summary',source:'background'})
 old.end({outcome:'failed'});assert.equal(records.length,1);assert.equal(records[0].call,'new')
 now=30;fresh.end({outcome:'done'});assert.equal(records[0].ms,15)
})

test('wall clock corrections do not alter operation durations or overlap accounting',()=>{
 let monotonic=0,wall=1000,records:CallOperation[]=[]
 const history=new CallHistory(rows=>records=rows,()=>monotonic,()=>wall)
 history.reset('clock')
 const span=history.begin({name:'read',source:'background'})
 monotonic=40;wall=-1000;span.end({outcome:'done'})
 assert.equal(records[0].ms,40)
 assert.equal(historySummary(records).activeMs,40)
})

test('a pending call contributes to count and running state once, then settles in place',()=>{
 let records:CallOperation[]=[]
 const history=new CallHistory(rows=>records=rows)
 history.reset('pending');const pending=history.begin({name:'read',source:'background'})
 assert.equal(historySummary(records).groups[0].running,1)
 pending.end({outcome:'failed'})
 assert.equal(historySummary(records).groups[0].running,0)
 assert.equal(historySummary(records).groups[0].count,1)
})

test('ending observation of an in-flight action does not claim that the action was cancelled',()=>{
 let records:CallOperation[]=[]
 const history=new CallHistory(rows=>records=rows)
 history.reset('hangup');const span=history.begin({name:'send',source:'tool'})
 history.cancel('unknown');span.end({outcome:'done'})
 assert.equal(records[0].outcome,'unknown')
})
