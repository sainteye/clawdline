import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botIdentity,draftIdentity,sameFence,canRead,canWrite,createViewState,mergeRoster } from "./view-state.ts"
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
test("roster uses activity order, stable source tie-break and versions without duplicate rows",()=>{
  const row=(machine:string,version:number,at:string)=>({bot:{key:{machine_id:machine,bot_id:"same"}},source:{readable:true,access:"interactive",state:"online"},version,activity_at:at}) as W.ProjectAssistantBotSummary
  const result=mergeRoster([[row("b",1,"2"),row("a",1,"2")],[row("a",2,"3"),row("b",0,"9")]])
  assert.deepEqual(result.map(r=>[r.bot.key.machine_id,r.version]),[["a",2],["b",1]])
})
