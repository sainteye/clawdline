import {test} from 'node:test'
import assert from 'node:assert/strict'
// @ts-expect-error -- Node executes the TypeScript source.
import {VoiceWorkRuntime,workRequest,shortResult,type VoiceState,type VoiceTask} from './engine.ts'
const state=():VoiceState=>({id:'workspace',revision:1,plan_version:0,inputs:[],tasks:[]})
const task=(id:string):VoiceTask=>({id,text:id,tool:'get_session_summary',args:{session_id:id},sources:['input'],depends_on:[],priority:0,version:1,status:'running',action_key:'action:'+id})
const tick=()=>new Promise<void>(r=>setImmediate(r))
test('a late read or change from the old machine cannot populate the newly selected workspace',async()=>{
 let release!:(a:{state:VoiceState})=>void
 const old=new Promise<{state:VoiceState}>(r=>release=r)
 let calls=0
 const runtime=new VoiceWorkRuntime({request:async()=>++calls===1?old:{state:{...state(),id:'new'}},execute:async()=>({result:'unused'})})
 const read=runtime.refresh();runtime.switchMachine();release({state:{...state(),id:'old'}});await read
 assert.equal(runtime.state,null)
 await runtime.refresh();assert.equal((runtime.state as VoiceState|null)?.id,'new')
})
test('the claimed action key is carried through progress and completion, once',async()=>{
 const changes:Record<string,unknown>[]=[];let claimed=false;const t=task('one')
 let finish!:()=>void;const done=new Promise<void>(r=>finish=r)
 const runtime=new VoiceWorkRuntime({request:async(change)=>{
  if(change)changes.push(change)
  if(change?.action==='claim'&&!claimed){claimed=true;return {state:{...state(),tasks:[t]},claimed:t}}
  if(change?.action==='finish'){finish();return {state:{...state(),revision:3,tasks:[{...t,status:'done'}]}}}
  return {state:{...state(),tasks:claimed?[t]:[{...t,status:'queued'}]}}
 },execute:async(_,__,progress)=>{await progress('one prepared result');return {result:'all returned'}}})
 const stop=runtime.activate();await done;stop();await tick()
 assert.equal(changes.filter(c=>c.action==='finish').length,1)
 for(const c of changes.filter(c=>c.action==='finish'||c.action==='progress'))assert.equal(c.key,'action:one')
})
test('work changes keep the request key and surface a typed refusal without automatic retry',async()=>{
 const calls:RequestInit[]=[]
 await assert.rejects(workRequest((async(_,init)=>{calls.push(init!);return new Response(JSON.stringify({error:{code:'voice_work_conflict'}}),{status:409})}) as typeof fetch,{action:'confirm',id:'one',version:1}),{code:'voice_work_conflict',uncertain:false})
 assert.equal(calls.length,1);assert.ok((calls[0].headers as Record<string,string>)['Idempotency-Key'])
 assert.equal(Array.from(shortResult('字'.repeat(5000))).length,4000)
})

test('a stalled old-machine read is aborted and cannot prevent reading the new machine',async()=>{
 let started!:()=>void;const reading=new Promise<void>(r=>started=r)
 let calls=0;let oldSignal:AbortSignal|undefined
 const runtime=new VoiceWorkRuntime({request:async(_,signal)=>{
  if(++calls===1){oldSignal=signal;started();return new Promise((_,reject)=>signal!.addEventListener('abort',()=>reject(new Error('aborted')),{once:true}))}
  return {state:{...state(),id:'new-machine'}}
 },execute:async()=>({result:'unused'})})
 const old=runtime.refresh();await reading;runtime.switchMachine();await runtime.refresh();await old
 assert.equal(oldSignal?.aborted,true);assert.equal(runtime.state?.id,'new-machine')
})

test('transport failure is typed and never retries an uncertain mutation',async()=>{
 let count=0
 await assert.rejects(workRequest((async()=>{count++;throw new TypeError('disconnected')}) as typeof fetch,{action:'confirm'}),{code:'voice_work_transport_unknown',uncertain:true})
 assert.equal(count,1)
})
