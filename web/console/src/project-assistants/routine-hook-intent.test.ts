import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {performRoutineHook,routineScopeIdentity} from "./routine-hook-intent.ts"
// @ts-expect-error -- Node executes source extensions.
import {readRoutineIntent} from "./routine-intent.ts"
const key={machine_id:"synthetic",bot_id:"bot"},project={machine_id:"synthetic",project_id:"project",canonical_id:"canonical",kind:"repository" as const}
const scope={key,project,conversation_id:"chat"}
const request={command:{key,operation:"edit" as const,idempotency_key:"original-rotation",content_hash:""},fence:{...scope,binding_id:"original-binding",generation:2},id:"routine",expected_revision:3,rotate:true}
const answer={contract_version:"project-assistants.v1",url:"/hooks/routines/routine",key:"a".repeat(64)}

test("lost rotation response and reload expose only the exact explicit replay, without storing secrets",async()=>{
  let bytes="",calls=0
  const persist=(value:unknown)=>{bytes=JSON.stringify(value)}
  await assert.rejects(performRoutineHook(request,{current:()=>true,persist,send:async q=>{calls++;assert.equal(bytes,JSON.stringify({action:"hook",request:q}));throw Error("lost response")}}))
  const original=bytes
  const reopened=readRoutineIntent({getItem:()=>bytes},"journal",scope)
  assert.equal(calls,1);assert.equal(bytes.includes(answer.key),false)
  assert.equal(reopened.state,"held")
  if(reopened.state!=="held"||reopened.intent.action!=="hook")throw Error("missing hook intent")
  const result=await performRoutineHook(reopened.intent.request,{current:()=>true,persist,send:async q=>{calls++;assert.deepEqual(q,request);return answer}})
  assert.ok(result===answer);assert.equal(calls,2);assert.equal(bytes,original)
  for(const changed of [{...scope,conversation_id:"other"},{...scope,project:{...project,canonical_id:"other"}},{...scope,key:{...key,bot_id:"other"}}])assert.deepEqual(readRoutineIntent({getItem:()=>bytes},"journal",changed),{state:"blocked"})
})

test("hidden, read-only or changed authority sends nothing and late key results are discarded",async()=>{
  let current=false,calls=0,writes=0
  const options={current:()=>current,persist:()=>{writes++},send:async()=>{calls++;return answer}}
  assert.equal(await performRoutineHook(request,options),null);assert.equal(calls,0);assert.equal(writes,0)
  current=true
  const result=await performRoutineHook(request,{...options,send:async()=>{calls++;current=false;return answer}})
  assert.equal(result,null);assert.equal(calls,1)
  const source={machineID:"synthetic",intentAccount:"account",authorizationRevision:"grant"},identity=routineScopeIdentity(source,scope)
  for(const change of [{...source,intentAccount:"other"},{...source,authorizationRevision:"new"},{...source,machineID:"other"}])assert.notEqual(routineScopeIdentity(change,scope),identity)
  assert.notEqual(routineScopeIdentity(source,{...scope,project:{...project,project_id:"other"}}),identity)
})

test("an unreadable journal or storage refusal never sends a replacement",async()=>{
  let calls=0
  assert.deepEqual(readRoutineIntent({getItem:()=>JSON.stringify({action:"hook",request:{...request,key:answer.key}})},"journal",scope),{state:"blocked"})
  await assert.rejects(performRoutineHook(request,{current:()=>true,persist:()=>{throw Error("storage refused")},send:async()=>{calls++;return answer}}))
  assert.equal(calls,0)
})
