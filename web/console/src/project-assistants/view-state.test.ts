import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botIdentity,draftIdentity,sameConversation,sameFence,canRead,canWrite,createViewState,mergeRoster } from "./view-state.ts"
// @ts-expect-error Node executes source extensions.
import * as viewStateModule from "./view-state.ts"
const key={machine_id:"one",bot_id:"same"},project={machine_id:"one",project_id:"p",canonical_id:"repo",kind:"repository" as const}
const fence={key,project,conversation_id:"c",binding_id:"b",generation:1}
test("machine and account identity isolate drafts and same-named Bots",()=>{
  assert.notEqual(botIdentity(key),botIdentity({...key,machine_id:"two"}))
  assert.notEqual(draftIdentity("a",key),draftIdentity("b",key))
  const disk=new Map<string,string>(),view=createViewState({getItem:k=>disk.get(k)??null,setItem:(k,v)=>{disk.set(k,v)},removeItem:k=>{disk.delete(k)}})
  view.stage(draftIdentity("a",key),"draft");assert.equal(disk.size,0)
  assert.equal(view.flush(draftIdentity("a",key)),true)
  assert.equal(createViewState({getItem:k=>disk.get(k)??null,setItem:()=>{},removeItem:()=>{}}).draft(draftIdentity("a",key)),"draft")
  assert.equal(view.draft(draftIdentity("b",key)),"")
})
test("storage refusal keeps the in-memory draft and reports that it is not durable",()=>{
  const view=createViewState({getItem:()=>{throw new Error("blocked")},setItem:()=>{throw new Error("blocked")},removeItem:()=>{throw new Error("blocked")}})
  assert.equal(view.draft("key"),"");view.stage("key","retained");assert.equal(view.flush("key"),false);assert.equal(view.draft("key"),"retained")
  assert.equal(createViewState().flush("key"),false)
})
test("host-owned recovery requirements are isolated by account and Bot without storage",()=>{
  const view=createViewState({getItem:()=>null,setItem:()=>assert.fail("recovery must not persist to storage"),removeItem:()=>assert.fail("recovery must not persist to storage")})
  const own=draftIdentity("account",key),otherAccount=draftIdentity("other",key),otherBot=draftIdentity("account",{...key,bot_id:"other"})
  view.recoveryRequired.add(own)
  assert.equal(view.recoveryRequired.has(own),true)
  assert.equal(view.recoveryRequired.has(otherAccount),false)
  assert.equal(view.recoveryRequired.has(otherBot),false)
  view.recoveryRequired.add(otherBot);view.recoveryRequired.delete(own)
  assert.equal(view.recoveryRequired.has(own),false)
  assert.equal(view.recoveryRequired.has(otherBot),true)
  assert.equal(createViewState().recoveryRequired.size,0)
})
test("one held unknown intent survives host consumers, remains separate from recovery and never crosses accounts or Bots",()=>{
  const view=createViewState(),own=draftIdentity("account",key)
  const intent:W.ProjectAssistantSendRequest={command:{key,operation:"send",idempotency_key:"same-request",content_hash:""},fence,text:"retained draft",quote_message_id:"original-quote"}
  view.heldOutcomes.set(own,{intent,receipt:null})
  view.recoveryRequired.add(own);view.recoveryRequired.delete(own)
  const remountedConsumer=view
  assert.deepEqual(remountedConsumer.heldOutcomes.get(own),{intent,receipt:null})
  assert.equal(view.heldOutcomes.has(draftIdentity("other",key)),false)
  assert.equal(view.heldOutcomes.has(draftIdentity("account",{...key,bot_id:"other"})),false)
  assert.equal(createViewState().heldOutcomes.size,0)
  assert.equal(view.heldOutcomes.size,1)
})
test("a revoked, stale or disabled source never admits a write",()=>{
  const source={state:"online",access:"interactive",readable:true,writable:true,fresh:true} as W.ProjectAssistantSourceStatus
  assert.equal(canWrite(source),true)
  for(const changed of [{fresh:false},{access:"history_only"},{state:"offline"},{state:"revoked"},{state:"unpaired"}])assert.equal(canWrite({...source,...changed} as W.ProjectAssistantSourceStatus),false)
  assert.equal(canRead({...source,state:"revoked"}),false)
  assert.equal(canRead({...source,state:"offline",fresh:false}),true)
})
test("every permission fence dimension must match",()=>{
  assert.equal(sameFence(fence,{...fence}),true)
  for(const changed of [{generation:2},{binding_id:"old"},{conversation_id:"other"},{key:{...key,machine_id:"other"}},{project:{...project,canonical_id:"other"}},{project:{...project,kind:"place" as const}}])assert.equal(sameFence(fence,{...fence,...changed}),false)
})
test("persisted facts cross executions only inside the exact conversation scope",()=>{
  const replaced={...fence,binding_id:"replacement",generation:2}
  assert.equal(sameConversation(fence,replaced),true)
  assert.equal(sameFence(fence,replaced),false)
  for(const changed of [{key:{...key,machine_id:"other"}},{key:{...key,bot_id:"other"}},{project:{...project,machine_id:"other"}},{project:{...project,project_id:"other"}},{project:{...project,canonical_id:"other"}},{project:{...project,kind:"place" as const}},{conversation_id:"other"}]) {
    assert.equal(sameConversation(fence,{...replaced,...changed}),false)
    assert.equal(sameFence(fence,{...replaced,...changed}),false)
  }
})
test("roster uses activity order, stable source tie-break and versions without duplicate rows",()=>{
  const row=(machine:string,version:number,at:string)=>({bot:{key:{machine_id:machine,bot_id:"same"}},source:{readable:true,access:"interactive",state:"online"},version,activity_at:at}) as W.ProjectAssistantBotSummary
  const result=mergeRoster([[row("b",1,"2"),row("a",1,"2")],[row("a",2,"3"),row("b",0,"9")]])
  assert.deepEqual(result.map(r=>[r.bot.key.machine_id,r.version]),[["a",2],["b",1]])
})

type Inspect=(value:W.ProjectAssistantExport,held:{intent:W.ProjectAssistantSendRequest|W.ProjectAssistantPermissionAnswerRequest;receipt:null},scope:{key:W.ProjectAssistantBotKey;project:typeof project;conversation_id:string})=>{state:string;turn_id?:string}
const inspect=(...args:Parameters<Inspect>)=>{const f=(viewStateModule as {inspectHeldSend?:Inspect}).inspectHeldSend;assert.ok(f,"a held send can be inspected against durable receipts");return f(...args)}
const scope={key,project,conversation_id:"c"}
const heldSend=(id="held-key"):{intent:W.ProjectAssistantSendRequest;receipt:null}=>({intent:{command:{key,operation:"send",idempotency_key:id,content_hash:""},fence,text:"synthetic held text",quote_message_id:""},receipt:null})
const turnOf=(id:string,key_="held-key",f=fence)=>({id,fence:f,idempotency_key:key_,message_id:"m-"+id,state:"queued",memory_revision:1,instructions:{text:"",revision:1,role_snapshot_ref:""}} as W.ProjectAssistantTurn)
const receiptOf=(state:W.ProjectAssistantEffectState,over:Partial<W.ProjectAssistantCommand>={},resource="turn-1")=>({id:"r-"+state+(over.idempotency_key??""),command:{key,operation:"send",idempotency_key:"held-key",content_hash:"canonical",...over},event_seq:3,resource_id:resource,state} as W.ProjectAssistantReceipt)
const exportOf=(receipts:W.ProjectAssistantReceipt[],turns:W.ProjectAssistantTurn[]=[turnOf("turn-1")],over:Partial<{bot:W.ProjectAssistantBotKey;project:typeof project;conversation:string}>={})=>({bot:{key:over.bot??key,project:over.project??project},conversation:{id:over.conversation??"c"},receipts,turns} as unknown as W.ProjectAssistantExport)
test("send recovery: only the exact admitted command for this Bot and conversation reattaches",()=>{
  for(const state of ["accepted","executing","delivered","observed","read"] as const)assert.deepEqual(inspect(exportOf([receiptOf(state)]),heldSend(),scope),{state:"delivered",receipt:receiptOf(state),turn_id:"turn-1"})
  assert.equal(inspect(exportOf([receiptOf("failed")]),heldSend(),scope).state,"failed")
  // A source-recorded unknown effect stays unknown unless its own turn proves admission.
  assert.equal(inspect(exportOf([receiptOf("unknown")],[]),heldSend(),scope).state,"unknown")
  assert.equal(inspect(exportOf([receiptOf("unknown")]),heldSend(),scope).state,"delivered")
})
test("send recovery: absence, foreign identity and inconsistent receipts never release a held send",()=>{
  assert.equal(inspect(exportOf([]),heldSend(),scope).state,"missing","a missing receipt is not proof of non-delivery")
  assert.equal(inspect(exportOf([receiptOf("accepted",{idempotency_key:"other-key"})]),heldSend(),scope).state,"missing")
  assert.equal(inspect(exportOf([receiptOf("accepted",{key:{...key,bot_id:"other"}})]),heldSend(),scope).state,"missing","same key on a different Bot")
  assert.equal(inspect(exportOf([receiptOf("accepted",{operation:"answer"})]),heldSend(),scope).state,"missing","wrong operation")
  assert.equal(inspect(exportOf([receiptOf("accepted")],[],{bot:{...key,bot_id:"other"}}),heldSend(),scope).state,"invalid","export of another Bot")
  assert.equal(inspect(exportOf([receiptOf("accepted")],undefined,{project:{...project,project_id:"other"}}),heldSend(),scope).state,"invalid","another Project")
  assert.equal(inspect(exportOf([receiptOf("accepted")],undefined,{conversation:"other"}),heldSend(),scope).state,"invalid","another conversation")
  assert.equal(inspect(exportOf([receiptOf("accepted")],[turnOf("turn-1","other-key")]),heldSend(),scope).state,"invalid","turn of another command")
  assert.equal(inspect(exportOf([receiptOf("accepted")],[turnOf("turn-1","held-key",{...fence,conversation_id:"other"})]),heldSend(),scope).state,"invalid","turn of another conversation")
  assert.equal(inspect(exportOf([receiptOf("accepted")],[]),heldSend(),scope).state,"invalid","accepted receipt without its turn")
  assert.equal(inspect(exportOf([receiptOf("accepted"),{...receiptOf("failed"),id:"second"}]),heldSend(),scope).state,"invalid","duplicate receipts")
  assert.equal(inspect({} as W.ProjectAssistantExport,heldSend(),scope).state,"invalid")
})
test("send recovery: a held permission answer is never inspected as a message send",()=>{
  const answer={intent:{command:{key,operation:"answer",idempotency_key:"held-key",content_hash:""},fence,offer_id:"offer",turn_id:"turn-1",option_id:"allow"},receipt:null} as const
  assert.equal(inspect(exportOf([receiptOf("accepted",{operation:"answer"})]),answer,scope).state,"invalid")
})
