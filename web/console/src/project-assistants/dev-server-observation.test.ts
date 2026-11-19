import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {ServerObservation,type ServerObservationState} from "./dev-server-observation.ts"

function deferred<T>(){
  let resolve!:(value:T)=>void,reject!:(error:unknown)=>void
  const promise=new Promise<T>((yes,no)=>{resolve=yes;reject=no})
  return {promise,resolve,reject}
}
const row=(id:string):W.ProjectAssistantDevServer=>({id,name:id,command:"synthetic",folder:"/synthetic/project",state:"running",attempt:"synthetic-attempt",revision:1,fingerprint:"f".repeat(64),ports_known:true,ports:[]})
const tick=()=>new Promise<void>(resolve=>setImmediate(resolve))
function fixture(){
  const reads:{reply:ReturnType<typeof deferred<readonly W.ProjectAssistantDevServer[]>>;signal:AbortSignal}[]=[],changes:ServerObservationState[]=[]
  let current=true,writable=true
  const watch=new ServerObservation({current:()=>current,writable:()=>writable,changed:value=>changes.push(value),read:signal=>{const reply=deferred<readonly W.ProjectAssistantDevServer[]>();reads.push({reply,signal});return reply.promise}})
  return {watch,reads,changes,revoke:()=>{current=false},readonly:()=>{writable=false}}
}

test("a slow read coalesces pulses and retains only one following demand",async()=>{
  const f=fixture();f.watch.pulse()
  for(let i=0;i<100;i++)f.watch.pulse()
  assert.equal(f.reads.length,1)
  f.reads[0].reply.resolve([row("first")]);await tick()
  assert.equal(f.reads.length,2);assert.equal(f.watch.snapshot().rows?.[0].id,"first")
  f.reads[1].reply.resolve([row("latest")]);await tick()
  assert.equal(f.reads.length,2);assert.equal(f.watch.snapshot().rows?.[0].id,"latest");assert.equal(f.watch.snapshot().fresh,true)
  f.watch.close()
})
test("a write shares the read slot and requires a fresh source read afterwards",async()=>{
  const f=fixture(),reply=deferred<void>();let writes=0
  const write=()=>{writes++;return reply.promise}
  f.watch.pulse();assert.equal(f.watch.mutate(write),false);assert.equal(writes,0)
  f.reads[0].reply.resolve([row("before")]);await tick()
  assert.equal(f.watch.mutate(write),true);assert.equal(f.watch.mutate(write),false)
  for(let i=0;i<100;i++)f.watch.pulse()
  assert.equal(f.reads.length,1);assert.equal(writes,1);assert.equal(f.watch.snapshot().inspect,true)
  reply.resolve();await tick();assert.equal(f.reads.length,2);assert.equal(f.watch.snapshot().fresh,false)
  f.reads[1].reply.resolve([row("after")]);await tick()
  assert.equal(f.watch.snapshot().inspect,false);assert.equal(f.watch.snapshot().rows?.[0].id,"after")
  f.watch.close()
})
test("an uncertain write does not replay or read again until explicit inspection",async()=>{
  const f=fixture();let writes=0
  f.watch.pulse();f.reads[0].reply.resolve([row("retained")]);await tick()
  f.watch.mutate(async()=>{writes++;throw {error:{code:"pa_outcome_unknown"}}});await tick()
  for(let i=0;i<100;i++)f.watch.pulse()
  assert.equal(writes,1);assert.equal(f.reads.length,1);assert.equal(f.watch.snapshot().error,"pa_outcome_unknown");assert.equal(f.watch.snapshot().fresh,false)
  assert.equal(f.watch.snapshot().rows?.[0].id,"retained")
  f.watch.refresh();assert.equal(f.reads.length,2)
  f.reads[1].reply.resolve([]);await tick();assert.equal(f.watch.snapshot().fresh,true);assert.equal(f.watch.snapshot().inspect,false)
  f.watch.close()
})
test("pausing retains a non-cooperative read slot and discards its late body",async()=>{
  const f=fixture();f.watch.pulse();f.watch.setActive(false)
  assert.equal(f.reads[0].signal.aborted,true)
  f.watch.setActive(true);f.watch.pulse();assert.equal(f.reads.length,1)
  f.reads[0].reply.resolve([row("old-hidden")]);await tick()
  assert.equal(f.reads.length,2);assert.equal(f.changes.some(s=>s.rows?.[0]?.id==="old-hidden"),false)
  f.reads[1].reply.resolve([row("visible")]);await tick();assert.equal(f.watch.snapshot().rows?.[0].id,"visible")
  f.watch.close()
})
test("pausing an admitted write preserves uncertainty instead of treating abort as refusal",async()=>{
  const f=fixture(),reply=deferred<void>();f.watch.pulse();f.reads[0].reply.resolve([row("before")]);await tick()
  let signal:AbortSignal|undefined
  f.watch.mutate(s=>{signal=s;return reply.promise});f.watch.setActive(false);f.watch.setActive(true)
  assert.equal(signal?.aborted,true);assert.equal(f.watch.snapshot().error,"pa_outcome_unknown")
  reply.resolve();await tick();f.watch.pulse();assert.equal(f.reads.length,1);assert.equal(f.watch.snapshot().inspect,true)
  f.watch.refresh();assert.equal(f.reads.length,2);f.reads[1].reply.resolve([]);await tick();f.watch.close()
})
test("revocation and closure never publish a late result for another owner",async()=>{
  const f=fixture();f.watch.pulse();f.revoke();f.reads[0].reply.resolve([row("private")]);await tick()
  assert.equal(f.watch.snapshot().rows,null);assert.equal(f.watch.snapshot().error,"pa_revoked")
  const g=fixture();g.watch.pulse();g.watch.close();const before=g.changes.length
  g.reads[0].reply.resolve([row("closed")]);await tick();assert.equal(g.changes.length,before)
})
test("a failed read is stale rather than an empty authoritative set",async()=>{
  const f=fixture();f.watch.pulse();f.reads[0].reply.resolve([row("retained")]);await tick()
  f.watch.pulse();f.reads[1].reply.reject({error:{code:"pa_offline"}});await tick();f.watch.pulse()
  assert.equal(f.reads.length,2);assert.equal(f.watch.snapshot().rows?.[0].id,"retained");assert.equal(f.watch.snapshot().fresh,false);assert.equal(f.watch.snapshot().error,"pa_offline")
  f.watch.refresh();f.reads[2].reply.resolve([]);await tick();assert.equal(f.watch.snapshot().fresh,true);f.watch.close()
})
test("read-only authority never invokes the mutation callback",async()=>{
  const f=fixture();f.watch.pulse();f.reads[0].reply.resolve([]);await tick();f.readonly()
  let writes=0;assert.equal(f.watch.mutate(async()=>{writes++}),false);assert.equal(writes,0);assert.equal(f.watch.snapshot().fresh,true)
  f.watch.close()
})
