import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { botIdentity,draftIdentity,sameConversation,sameFence,canRead,canWrite,createViewState,mergeRoster } from "./view-state.ts"
// @ts-expect-error Node executes source extensions.
import * as viewStateModule from "./view-state.ts"
const key={machine_id:"one",bot_id:"same"},project={machine_id:"one",project_id:"p",canonical_id:"repo",kind:"repository" as const}
const scope={key,project,conversation_id:"c"}
const fence={...scope,binding_id:"b",generation:1}
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

const memoryStore=()=>{const disk=new Map<string,string>();return {disk,store:{getItem:(k:string)=>disk.get(k)??null,setItem:(k:string,v:string)=>{disk.set(k,v)},removeItem:(k:string)=>{disk.delete(k)}}}}
const journalSend=(id:string,text="retained draft",extra:Partial<W.ProjectAssistantSendRequest>={}):W.ProjectAssistantSendRequest=>({command:{key,operation:"send",idempotency_key:id,content_hash:""},fence,text,quote_message_id:"quote-1",...extra})
test("send recovery journal: accepted but unobserved text survives reload with its own receipt",()=>{
  const {store}=memoryStore(),own=draftIdentity("account",key),intent=journalSend("accepted-send")
  const receipt={id:"accepted-receipt",command:intent.command,state:"accepted" as const,resource_id:"admitted-turn",event_seq:10}
  createViewState(store).heldOutcomes.set(own,{intent,receipt})
  const reloaded=createViewState(store).heldOutcomes
  assert.equal(reloaded.problem(own),null)
  assert.equal((reloaded.get(own)?.intent as W.ProjectAssistantSendRequest).text,"retained draft")
  assert.equal(reloaded.get(own)?.receipt?.resource_id,"admitted-turn")
  createViewState(store).heldOutcomes.set(own,{intent,receipt:{...receipt,command:{...receipt.command,key:{...key,bot_id:"other"}}}})
  assert.equal(createViewState(store).heldOutcomes.problem(own),"unreadable","a foreign receipt must not be adopted")
})
test("send recovery journal: a held send survives a full reload with its identity, text, quote and references, without picture data",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key)
  createViewState(store).heldOutcomes.set(own,{intent:journalSend("original",undefined,{images:["data:image/png;base64,AAAA"],upload_ids:["up-1"]}),receipt:null,files:["notes.txt"]})
  const raw=disk.get(viewStateModule.heldJournalKey(own))!
  assert.equal(raw.includes("data:image"),false,"picture data is never stored")
  const reloaded=createViewState(store).heldOutcomes.get(own)!
  assert.equal(reloaded.intent.command.idempotency_key,"original")
  assert.equal((reloaded.intent as W.ProjectAssistantSendRequest).text,"retained draft")
  assert.equal((reloaded.intent as W.ProjectAssistantSendRequest).quote_message_id,"quote-1")
  assert.deepEqual((reloaded.intent as W.ProjectAssistantSendRequest).upload_ids,["up-1"])
  assert.equal("images" in reloaded.intent,false)
  assert.equal(reloaded.pictures,1);assert.deepEqual(reloaded.files,["notes.txt"]);assert.equal(reloaded.restored,true)
  assert.equal(createViewState(store).heldOutcomes.has(draftIdentity("other",key)),false,"another account never sees it")
  assert.equal(createViewState(store).heldOutcomes.has(draftIdentity("account",{...key,machine_id:"two"})),false,"another source never sees it")
})
test("send recovery journal: a permission answer on disk is unreadable and stays intact",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key),view=createViewState(store)
  const answer:W.ProjectAssistantPermissionAnswerRequest={command:{key,operation:"answer",idempotency_key:"answer-1",content_hash:""},fence,offer_id:"o",turn_id:"t",option_id:"allow"}
  const storageKey=viewStateModule.heldJournalKey(own),raw=JSON.stringify({v:1,active:{intent:answer,receipt:null},aside:[]})
  disk.set(storageKey,raw)
  assert.equal(view.heldOutcomes.get(own),undefined)
  assert.equal(view.heldOutcomes.problem(own),"unreadable")
  assert.equal(disk.get(storageKey),raw)
})
test("send recovery journal: setting a send aside keeps it, with its own identity, across a reload; a new send is a different command",()=>{
  const {store}=memoryStore(),own=draftIdentity("account",key),view=createViewState(store)
  view.heldOutcomes.set(own,{intent:journalSend("old"),receipt:null})
  assert.equal(view.heldOutcomes.setAside(own),true)
  assert.equal(view.heldOutcomes.has(own),false)
  view.heldOutcomes.set(own,{intent:journalSend("new","different"),receipt:null})
  const reloaded=createViewState(store).heldOutcomes
  assert.equal(reloaded.get(own)?.intent.command.idempotency_key,"new")
  assert.deepEqual(reloaded.aside(own).map(o=>o.intent.command.idempotency_key),["old"])
  reloaded.settleAside(own,"old")
  assert.deepEqual(createViewState(store).heldOutcomes.aside(own),[])
})
test("deleting an unsent message removes it from this device's journal, so a reload does not bring it back",()=>{
  const {store}=memoryStore(),own=draftIdentity("account",key),view=createViewState(store)
  view.heldOutcomes.set(own,{intent:journalSend("refused","never admitted"),receipt:null,needsEdit:true});view.heldOutcomes.setAside(own)
  view.heldOutcomes.set(own,{intent:journalSend("kept","still held"),receipt:null});view.heldOutcomes.setAside(own)
  assert.equal(view.heldOutcomes.discard(own,"refused"),true)
  assert.deepEqual(view.heldOutcomes.aside(own).map(o=>o.intent.command.idempotency_key),["kept"])
  assert.deepEqual(createViewState(store).heldOutcomes.aside(own).map(o=>o.intent.command.idempotency_key),["kept"],"the deleted message stays gone after a reload")
  assert.equal(createViewState(store).heldOutcomes.discard(own,"missing"),false)
})
test("a message whose delivery is unknown can be cleared too, and a reload does not bring it back",()=>{
  const {store}=memoryStore(),own=draftIdentity("account",key),view=createViewState(store)
  view.heldOutcomes.set(own,{intent:journalSend("unknown","maybe arrived"),receipt:{id:"r",command:journalSend("unknown").command,state:"unknown",resource_id:"",event_seq:0}});view.heldOutcomes.setAside(own)
  assert.equal(createViewState(store).heldOutcomes.aside(own).length,1)
  assert.equal(view.heldOutcomes.discard(own,"unknown"),true)
  assert.deepEqual(createViewState(store).heldOutcomes.aside(own),[],"the cleared message stays gone after a reload")
})
test("a delete this device cannot save keeps the message, on screen and after a reload",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key)
  const view=createViewState(store)
  view.heldOutcomes.set(own,{intent:journalSend("refused"),receipt:null,needsEdit:true});view.heldOutcomes.setAside(own)
  const refusing={...store,setItem:()=>{throw new Error("full")},removeItem:()=>{throw new Error("full")}}
  const blocked=createViewState(refusing).heldOutcomes
  assert.equal(blocked.discard(own,"refused"),false)
  assert.deepEqual(blocked.aside(own).map(o=>o.intent.command.idempotency_key),["refused"],"an unsaved delete does not pretend the message is gone")
  assert.ok(disk.has(viewStateModule.heldJournalKey(own)))
})
test("send recovery journal: corruption and storage failure are reported and never make a held send resendable",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key)
  disk.set(viewStateModule.heldJournalKey(own),"{not json")
  const view=createViewState(store)
  assert.equal(view.heldOutcomes.problem(own),"unreadable")
  view.heldOutcomes.delete(own)
  assert.equal(disk.get(viewStateModule.heldJournalKey(own)),"{not json","an unreadable record is kept until the person acts")
  assert.equal(view.heldOutcomes.setAside(own),true)
  assert.equal(view.heldOutcomes.problem(own),null);assert.equal(view.heldOutcomes.unreadable(own),true)
  assert.equal(createViewState(store).heldOutcomes.unreadable(own),true,"the person is still told after another reload")
  assert.deepEqual(JSON.parse(disk.get(viewStateModule.heldJournalKey(own))!).preservedRaw,["{not json"],"continuing keeps the original unreadable bytes")
  view.heldOutcomes.dismissUnreadable(own)
  assert.deepEqual(JSON.parse(disk.get(viewStateModule.heldJournalKey(own))!).preservedRaw,["{not json"],"dismissing a notice does not erase its original data")
  // A record for another Bot under this key, or with picture data, is unreadable too.
  disk.set(viewStateModule.heldJournalKey(own),JSON.stringify({v:1,active:{intent:{...journalSend("x"),command:{key:{...key,bot_id:"other"},operation:"send",idempotency_key:"x",content_hash:""}},receipt:null},aside:[]}))
  assert.equal(createViewState(store).heldOutcomes.problem(own),"unreadable")
  disk.set(viewStateModule.heldJournalKey(own),JSON.stringify({v:1,active:{intent:journalSend("x",undefined,{images:["data:"]}),receipt:null},aside:[]}))
  assert.equal(createViewState(store).heldOutcomes.problem(own),"unreadable")
  const full=createViewState({getItem:()=>null,setItem:()=>{throw new Error("quota")},removeItem:()=>{}})
  full.heldOutcomes.set(own,{intent:journalSend("unsaved"),receipt:null})
  assert.equal(full.heldOutcomes.has(own),true,"this tab still holds it");assert.equal(full.heldOutcomes.problem(own),"unsaved")
  const huge=createViewState(memoryStore().store)
  huge.heldOutcomes.set(own,{intent:journalSend("huge","x".repeat(viewStateModule.MAX_HELD_TEXT_BYTES+1)),receipt:null})
  assert.equal(huge.heldOutcomes.problem(own),"unsaved","a text beyond the source's message bound is not journaled, visibly")
})
test("send recovery journal: failed preservation cannot replace unreadable original data",()=>{
  const original="{retained original",own=draftIdentity("account",key)
  let raw=original
  const view=createViewState({getItem:()=>raw,setItem:()=>{throw new Error("storage full")},removeItem:()=>{raw=""}})
  assert.equal(view.heldOutcomes.problem(own),"unreadable")
  assert.equal(view.heldOutcomes.setAside(own),false)
  assert.equal(view.heldOutcomes.problem(own),"unreadable")
  assert.equal(raw,original)
})
test("send recovery journal: retained history is independent of the source's pending execution count",()=>{
  const {store}=memoryStore(),view=createViewState(store),own=draftIdentity("account",key)
  for(let i=0;i<32;i++){view.heldOutcomes.set(own,{intent:journalSend("k"+i),receipt:null});assert.equal(view.heldOutcomes.setAside(own),true)}
  const restored=createViewState(store).heldOutcomes.aside(own)
  assert.equal(restored.length,32)
  assert.deepEqual(restored.map(o=>o.intent.command.idempotency_key),Array.from({length:32},(_,i)=>"k"+i))
})

test("legacy unverified draft: original text, quote and full scope survive separately from the new draft",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key),view=createViewState(store)
  disk.set(own,"Legacy uncertain draft")
  view.unverifiedDrafts.capture(own,scope,"Legacy uncertain draft","quote-1")
  const active=createViewState(store).unverifiedDrafts.active(own,scope)!
  assert.equal(active.text,"Legacy uncertain draft");assert.equal(active.quote,"quote-1")
  assert.equal("intent" in active,false);assert.equal("command" in active,false)
  assert.deepEqual(view.archiveUnverifiedDraft(own,scope),{text:"",quote:""})
  view.stage(own,"New manually entered draft");assert.equal(view.flush(own),true)
  const reloaded=createViewState(store)
  assert.equal(reloaded.draft(own),"New manually entered draft")
  assert.equal(reloaded.unverifiedDrafts.active(own,scope),undefined)
  assert.equal(reloaded.unverifiedDrafts.continuing(own,scope),true)
  reloaded.unverifiedDrafts.capture(own,scope,"New manually entered draft","")
  assert.equal(reloaded.unverifiedDrafts.history(own,scope).length,1,"a new draft is never mistaken for the old one on reload")
  assert.equal(reloaded.unverifiedDrafts.history(own,scope)[0].text,"Legacy uncertain draft")
  for(const changed of [{key:{...key,machine_id:"other"}},{key:{...key,bot_id:"other"}},{project:{...project,machine_id:"other"}},{project:{...project,project_id:"other"}},{project:{...project,canonical_id:"other"}},{project:{...project,kind:"place" as const}},{conversation_id:"other"}]){
    assert.equal(reloaded.unverifiedDrafts.history(own,{...scope,...changed}).length,0)
    assert.equal(reloaded.unverifiedDrafts.continuing(own,{...scope,...changed}),false)
  }
  assert.equal(reloaded.unverifiedDrafts.history(draftIdentity("other",key),scope).length,0)
})
test("removing an archived draft keeps later drafts out of legacy recovery after reload",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key),view=createViewState(store)
  view.unverifiedDrafts.capture(own,scope,"Old draft","old quote")
  assert.equal(view.unverifiedDrafts.dismiss(own,scope),false,"a draft still being recovered cannot be dismissed")
  assert.ok(view.archiveUnverifiedDraft(own,scope))
  disk.set(own,"A later draft")
  const blocked=createViewState({...store,setItem:()=>{throw new Error("blocked")}})
  assert.equal(blocked.unverifiedDrafts.dismiss(own,scope),false)
  assert.equal(blocked.unverifiedDrafts.history(own,scope)[0].text,"Old draft")
  assert.equal(view.unverifiedDrafts.dismiss(own,{...scope,conversation_id:"another conversation"}),false)
  assert.equal(view.unverifiedDrafts.dismiss(own,scope),true)
  const reloaded=createViewState(store)
  assert.equal(reloaded.draft(own),"A later draft")
  assert.equal(reloaded.unverifiedDrafts.continuing(own,scope),true)
  reloaded.unverifiedDrafts.capture(own,scope,"A later draft","")
  assert.deepEqual(reloaded.unverifiedDrafts.history(own,scope),[])
  assert.doesNotMatch(disk.get(viewStateModule.unverifiedDraftKey(own))??"",/Old draft|old quote/)
})
test("legacy unverified draft: every storage refusal retains the original and the lock",()=>{
  for(const failAt of [1,2,3]){
    const {disk,store}=memoryStore(),own=draftIdentity("account",key)
    disk.set(own,"Legacy uncertain draft")
    let writes=0,fail=false
    const refusal=()=>{if(fail&&++writes===failAt)throw new Error("synthetic refusal")}
    const storage={getItem:store.getItem,setItem:(k:string,v:string)=>{refusal();store.setItem(k,v)},removeItem:(k:string)=>{refusal();store.removeItem(k)}}
    const view=createViewState(storage)
    view.unverifiedDrafts.capture(own,scope,"Legacy uncertain draft","quote-1")
    fail=true
    assert.equal(view.archiveUnverifiedDraft(own,scope),undefined)
    assert.equal(view.unverifiedDrafts.active(own,scope)?.text,"Legacy uncertain draft")
    assert.equal(createViewState(store).unverifiedDrafts.active(own,scope)?.text,"Legacy uncertain draft")
    assert.equal(view.unverifiedDrafts.continuing(own,scope),false)
    fail=false
    assert.ok(view.archiveUnverifiedDraft(own,scope))
    assert.equal(createViewState(store).unverifiedDrafts.continuing(own,scope),true)
  }
})
test("legacy unverified draft: a failed initial preservation and unreadable records never unlock or overwrite originals",()=>{
  const {disk,store}=memoryStore(),own=draftIdentity("account",key)
  disk.set(own,"Legacy uncertain draft")
  const view=createViewState({...store,setItem:()=>{throw new Error("blocked")}})
  view.unverifiedDrafts.capture(own,scope,"Legacy uncertain draft","quote-1")
  assert.equal(view.unverifiedDrafts.problem(own),"unsaved")
  assert.equal(view.archiveUnverifiedDraft(own,scope),undefined)
  assert.equal(disk.get(own),"Legacy uncertain draft")
  const raw="opaque undecodable original"
  disk.set(viewStateModule.unverifiedDraftKey(own),raw)
  const unreadable=createViewState(store)
  unreadable.unverifiedDrafts.capture(own,scope,"Legacy uncertain draft","")
  assert.equal(unreadable.archiveUnverifiedDraft(own,scope),undefined)
  assert.equal(disk.get(viewStateModule.unverifiedDraftKey(own)),raw)
  assert.equal(disk.get(own),"Legacy uncertain draft")
})

test("archiving an old draft preserves newer text written by another tab or staged in this one",()=>{
  for(const staged of [false,true]){
    const {disk,store}=memoryStore(),own=draftIdentity("account",key)
    disk.set(own,"Old draft")
    const view=createViewState(store)
    assert.equal(view.draft(own),"Old draft")
    view.quotes.set(own,"old quote")
    view.unverifiedDrafts.capture(own,scope,"Old draft","old quote")
    if(staged){
      view.stage(own,"New staged draft")
      view.quotes.set(own,"new quote")
    }
    disk.set(own,"New stored draft")
    assert.deepEqual(view.archiveUnverifiedDraft(own,scope),staged?{text:"New staged draft",quote:"new quote"}:{text:"New stored draft",quote:""})
    assert.equal(disk.get(own),"New stored draft")
    assert.equal(view.unverifiedDrafts.history(own,scope)[0].text,"Old draft")
  }
})

test("legacy plain draft retention does not borrow the send-size bound",()=>{
  const {store}=memoryStore(),own=draftIdentity("account",key),text="x".repeat((32<<10)+1)
  const view=createViewState(store)
  view.unverifiedDrafts.capture(own,scope,text,"quote-1")
  const restored=createViewState(store)
  assert.equal(restored.unverifiedDrafts.active(own,scope)?.text,text)
  assert.equal(restored.unverifiedDrafts.problem(own),undefined)
  assert.ok(restored.archiveUnverifiedDraft(own,scope))
  assert.equal(createViewState(store).unverifiedDrafts.history(own,scope)[0]?.text,text)
})
test("a send's late answer settles only its own entry and reaches a Chat opened after the sender left",()=>{
  const disk=new Map<string,string>(),store={getItem:(k:string)=>disk.get(k)??null,setItem:(k:string,v:string)=>{disk.set(k,v)},removeItem:(k:string)=>{disk.delete(k)}}
  const view=createViewState(store),own=draftIdentity("account",key)
  const send=(id:string,text:string):W.ProjectAssistantSendRequest=>({command:{key,operation:"send",idempotency_key:id,content_hash:""},fence,text,quote_message_id:""})
  const unknown=(id:string)=>({id,command:{key,operation:"send",idempotency_key:id,content_hash:""},state:"unknown"}) as unknown as W.ProjectAssistantReceipt
  view.heldOutcomes.set(own,{intent:send("old","first"),receipt:null});view.heldOutcomes.beginTransport(own,"old")
  const heard:string[]=[];const stop=view.heldOutcomes.subscribe(own,n=>heard.push(n.idempotency_key+":"+(n.receipt?.state??"none")))
  // A full reload is a new page: the journal comes back without the request, so it stays genuinely unknown.
  const reloaded=createViewState(store)
  assert.equal(reloaded.heldOutcomes.get(own)?.intent.command.idempotency_key,"old")
  assert.equal(reloaded.heldOutcomes.transporting(own,"old"),false)
  assert.equal(view.heldOutcomes.transporting(own,"old"),true)
  assert.equal(view.heldOutcomes.transporting(draftIdentity("other",key),"old"),false)
  // The person set the old send aside and a newer one is held in the box, with a new draft and quote.
  assert.equal(view.heldOutcomes.setAside(own),true)
  view.heldOutcomes.set(own,{intent:send("new","second"),receipt:null});view.stage(own,"new draft");view.quotes.set(own,"new-quote")
  // A late unknown answer for the old send updates it where it is.
  assert.equal(view.heldOutcomes.settle(own,"old",{intent:send("old","first"),receipt:unknown("old")}),"aside")
  assert.equal(view.heldOutcomes.get(own)?.intent.command.idempotency_key,"new")
  assert.equal(view.heldOutcomes.aside(own)[0].receipt?.state,"unknown")
  // Its accepted answer removes only that aside entry.
  assert.equal(view.heldOutcomes.settle(own,"old",null),"aside")
  view.heldOutcomes.endTransport(own,"old",null)
  assert.equal(view.heldOutcomes.get(own)?.intent.command.idempotency_key,"new")
  assert.equal(view.heldOutcomes.aside(own).length,0)
  assert.equal(view.draft(own),"new draft");assert.equal(view.quotes.get(own),"new-quote")
  assert.deepEqual(heard,["old:none"])
  // Ending twice, or a request that was never opened, tells nobody again.
  view.heldOutcomes.endTransport(own,"old",null);view.heldOutcomes.endTransport(own,"never",null)
  assert.deepEqual(heard,["old:none"])
  assert.equal(view.heldOutcomes.settle(own,"never",null),"none")
  // The newer held send settles by its own identity, and the journal on disk agrees.
  view.heldOutcomes.beginTransport(own,"new");assert.equal(view.heldOutcomes.settle(own,"new",null),"active");view.heldOutcomes.endTransport(own,"new",unknown("new"))
  assert.deepEqual(heard,["old:none","new:unknown"])
  assert.equal(createViewState(store).heldOutcomes.has(own),false)
  stop();view.heldOutcomes.beginTransport(own,"later");view.heldOutcomes.endTransport(own,"later",null)
  assert.equal(heard.length,2)
})
