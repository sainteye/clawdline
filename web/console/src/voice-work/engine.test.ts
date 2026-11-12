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

test('a cancellation observed from another browser aborts the current executor',async()=>{
 const t=task('cancel');let claimed=false;let signal:AbortSignal|undefined;let stopped!:()=>void
 const started=new Promise<void>(r=>stopped=r)
 const runtime=new VoiceWorkRuntime({request:async(change)=>{
  if(change?.action==='claim'&&!claimed){claimed=true;return {state:{...state(),revision:2,tasks:[t]},claimed:t}}
  if(change?.action==='cancel')return {state:{...state(),revision:3,tasks:[{...t,status:'cancel_requested'}]}}
  return {state:{...state(),revision:claimed?2:1,tasks:claimed?[t]:[{...t,status:'queued'}]}}
 },execute:async(_,s)=>{signal=s;stopped();return new Promise(()=>{})}})
 const release=runtime.activate();await started
 await runtime.change({action:'cancel',id:t.id,version:1});release()
 assert.equal(signal?.aborted,true)
 await tick();runtime.switchMachine()
})

// @ts-expect-error -- Node executes TypeScript source.
import {WorkObservations} from './observations.ts'
// @ts-expect-error -- Node executes TypeScript source.
import {retryableTask} from './engine.ts'
test('durable progress from another browser preserves every member and is spoken once',()=>{
 const observer=new WorkObservations(state())
 const one={...task('group'),attempt:1,progress:[{sequence:1,value:'first'},{sequence:2,value:'second'}]}
 const progress={...state(),revision:2,tasks:[one]}
 assert.equal(observer.update(progress).length,2)
 assert.equal(observer.update(progress).length,0)
 const done={...progress,revision:3,tasks:[{...one,status:'done',result_revision:3,result:'finished'}]}
 assert.equal(observer.update(done).length,1)
 assert.equal(observer.update(done).length,0)
 assert.equal(new WorkObservations(done).update(done).length,0,'old history is not replayed')
})
test('unknown reads offer retry, unknown writes and navigation effects do not',()=>{
 assert.equal(retryableTask({...task('read'),status:'outcome_unknown'}),true)
 assert.equal(retryableTask({...task('write'),tool:'send_message_to_session',status:'outcome_unknown'}),false)
 assert.equal(retryableTask({...task('open'),tool:'get_devstack_links',args:{open:true},status:'outcome_unknown'}),false)
})

test('cancellation after effect admission retains an unknown outcome rather than claiming rollback',async()=>{
 const t={...task('effect'),tool:'send_message_to_session'};let claimed=false;let ready!:()=>void;let finished!:(x:Record<string,unknown>)=>void
 const admitted=new Promise<void>(r=>ready=r),done=new Promise<Record<string,unknown>>(r=>finished=r)
 const runtime=new VoiceWorkRuntime({request:async(change)=>{
  if(change?.action==='claim'&&!claimed){claimed=true;return {state:{...state(),revision:2,tasks:[t]},claimed:t}}
  if(change?.action==='cancel')return {state:{...state(),revision:4,tasks:[{...t,status:'cancel_requested'}]}}
  if(change?.action==='finish')finished(change)
  return {state:{...state(),revision:claimed?3:1,tasks:claimed?[t]:[{...t,status:'queued'}]}}
 },execute:async(_,__,___,beginEffect)=>{await beginEffect();ready();return new Promise(()=>{})}})
 const stop=runtime.activate()
 try {await admitted;await runtime.change({action:'cancel',id:t.id,version:1});const result=await done;assert.equal(result.unknown,true);assert.equal(result.error,'voice_work_stopped')}
 finally {stop();runtime.switchMachine();await tick()}
})

test('partial progress followed by failure never claims that all reads returned',()=>{
 const observer=new WorkObservations(state())
 const events=observer.update({...state(),revision:2,tasks:[{...task('partial'),progress:[{sequence:1,value:'one member prepared'}],status:'outcome_unknown',error:'connection lost',result_revision:2}]})
 assert.equal(events.length,2)
 assert.doesNotMatch(events[1].value,/All requested reads have returned/)
 assert.match(events[1].value,/outcome_unknown/)
})
