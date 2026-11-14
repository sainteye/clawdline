import assert from "node:assert/strict"
import { test } from "node:test"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import { readView,readViewRequest,mergeTimeline,eventTimeline } from "./read-views.ts"
// @ts-expect-error -- Node executes source extensions.
import { errorCode } from "./client.ts"
// @ts-expect-error -- Node executes source extensions.
import { partitionMessages } from "./chat-items.ts"

const key={machine_id:"synthetic",bot_id:"bot"}
const project:W.ProjectAssistantProjectScope={machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository"}
const fence:W.ProjectAssistantFence={key,project,conversation_id:"chat",binding_id:"binding",generation:2}
const request=(mode:W.ProjectAssistantReadViewMode="snapshot"):W.ProjectAssistantReadViewRequest=>({key,conversation_id:"chat",mode,source_epoch:mode==="snapshot"?"":"epoch",cursor:mode==="history"?"opaque history +<&":mode==="detail"?"opaque detail":"",after_seq:mode==="events"?602:0,page_size:mode==="detail"?1:mode==="events"?256:100})
const bot:W.ProjectAssistantBot={key,project,conversation_id:"chat",name:"Synthetic Bot",agent:"codex",appearance:{shape:"blob",color:"#e39567"},permission_policy:"ask",instructions:{revision:1,role_snapshot_ref:"",text:"kept only in bot metadata"},created_at:"2026-11-12T00:00:00Z",updated_at:"2026-11-12T00:00:00Z",version:1}
const message=(id="message",seq=600):W.ProjectAssistantMessage=>({id,seq,fence:{...fence,generation:1},kind:"assistant",text:id,at:"2026-11-12T00:00:00Z",turn_id:"turn",quote_message_id:""})
const row=(m=message()):W.ProjectAssistantReadViewItem=>({id:m.id,kind:"message",message:m,anchor_message_seq:m.seq,observed_event_seq:602})
function answer(mode:W.ProjectAssistantReadViewMode="snapshot"):W.ProjectAssistantReadViewResponse {
  return {contract_version:"project-assistants.v1",mode,source_epoch:"epoch",source:{machine_id:key.machine_id,access:"interactive",state:"online",fresh:true,readable:true,writable:true,observed_at:"",capabilities:[]},head:{key,project,current_fence:fence,conversation:{key,id:"chat",binding_id:"binding",generation:2,active_turn_id:"",last_message_seq:600,memory_revision:1,version:1},bot_version:1,memory_revision:1,event_frontier:602,unknown_turn_count:17,pending_permission_count:101,trace_category_count:101},items:mode==="snapshot"||mode==="history"?[row()]:[],snapshot:mode==="snapshot"?{bot,pending_turns:[],queued_messages:[]}:undefined,has_more:false,next_cursor:"",next_seq:mode==="events"?602:0}
}
const rejects=(change:(a:W.ProjectAssistantReadViewResponse)=>void,code="pa_invalid",mode:W.ProjectAssistantReadViewMode="snapshot")=>{
  const a=structuredClone(answer(mode));change(a)
  assert.throws(()=>readView(a,request(mode),fence),e=>errorCode(e)===code)
}

test("snapshot adopts an exact cross-Bot machine frontier while retaining older execution facts",()=>{
  const a=answer(),value=readView(a,request(),fence)
  assert.equal(value.head.event_frontier,602)
  assert.equal(value.items[0].message!.seq,600)
  assert.equal(value.items[0].message!.fence.generation,1)
  assert.equal(value.head.unknown_turn_count,17)
  assert.equal(value.head.pending_permission_count,101)
  assert.equal(value.head.trace_category_count,101)
})
test("truthy Source strings never authorize a successful snapshot",()=>{
  for(const field of ["readable","writable","fresh"] as const)rejects(a=>{(a.source as unknown as Record<string,unknown>)[field]="false"})
})
test("missing mandatory Bot values never authorize a successful snapshot",()=>{
  for(const field of ["name","appearance","agent","permission_policy","instructions","created_at","updated_at"])rejects(a=>{delete (a.snapshot!.bot as unknown as Record<string,unknown>)[field]})
})
test("consumed Source and Bot primitives and enums reject malformed successful envelopes",()=>{
  const changes:((a:W.ProjectAssistantReadViewResponse)=>void)[]=[
    a=>{a.source.state="other" as W.ProjectAssistantSourceState},
    a=>{a.source.access="other" as W.ProjectAssistantAccess},
    a=>{a.source.observed_at=0 as unknown as string},
    a=>{a.source.capabilities=null as unknown as W.ProjectAssistantCapabilities[]},
    a=>{a.snapshot!.bot.name=1 as unknown as string},
    a=>{a.snapshot!.bot.agent="other" as W.ProjectAssistantAgent},
    a=>{a.snapshot!.bot.permission_policy="other" as W.ProjectAssistantPermissionPolicy},
    a=>{a.snapshot!.bot.appearance.shape="other" as W.ProjectAssistantShape},
    a=>{a.snapshot!.bot.appearance.color=0 as unknown as string},
    a=>{a.snapshot!.bot.instructions.text=false as unknown as string},
    a=>{a.snapshot!.bot.instructions.role_snapshot_ref=null as unknown as string},
    a=>{a.snapshot!.bot.instructions.revision=-1},
    a=>{a.snapshot!.bot.created_at="not a date"},
    a=>{a.snapshot!.bot.updated_at=null as unknown as string},
  ]
  for(const change of changes)rejects(change)
  const capability:W.ProjectAssistantCapabilities={key,project,agent:"codex",cli_version:"unverified",adapter_version:"unverified",evidence:"synthetic",native_memory:"unknown",native_memory_verified:false,read_only_enforced:false,resume:false,send_message:false}
  const valid=answer();valid.source.capabilities=[capability]
  assert.equal(readView(valid,request(),fence).source.capabilities[0].read_only_enforced,false)
  for(const field of ["native_memory_verified","read_only_enforced","resume","send_message"]){const a=structuredClone(valid);(a.source.capabilities[0] as unknown as Record<string,unknown>)[field]="false";assert.throws(()=>readView(a,request(),fence),e=>errorCode(e)==="pa_invalid")}
})
test("all pending user messages map exactly to their summary without moving running turns into the queue",()=>{
  const a=answer(),s=a.snapshot!
  s.pending_turns=(["accepted","queued","running","waiting_permission"] as const).map((state,i)=>({id:`pending-${i}`,fence,message_id:`message-${i}`,state,failure_code:"",started_at:""}))
  s.queued_messages=s.pending_turns.map((t,i)=>({...message(t.message_id,i+1),kind:"user",turn_id:t.id}))
  assert.equal(readView(a,request(),fence).snapshot!.queued_messages.length,4)
  const partition=partitionMessages(s.queued_messages,s.pending_turns)
  assert.deepEqual(partition.queued.map(m=>m.turn_id),["pending-0","pending-1"])
  assert.deepEqual(partition.chat.map(m=>m.turn_id),["pending-2","pending-3"])
  const wrong=structuredClone(a);wrong.snapshot!.queued_messages[2].turn_id="different-turn"
  assert.throws(()=>readView(wrong,request(),fence),e=>errorCode(e)==="pa_invalid")
})
test("the retained conversation memory revision can precede the source memory head",()=>{
  const a=answer();a.head.memory_revision=2
  assert.equal(readView(a,request(),fence).head.conversation.memory_revision,1)
  assert.equal(readView(a,request(),fence).head.memory_revision,2)
})
test("invalid counts, mismatched mode, foreign scope, epochs and dishonest continuation are typed refusals",()=>{
  for(const count of ["event_frontier","unknown_turn_count","pending_permission_count","trace_category_count"] as const)for(const value of [-1,NaN,1.5,Number.MAX_SAFE_INTEGER+1])rejects(a=>{a.head[count]=value})
  rejects(a=>{a.mode="history"})
  rejects(a=>{a.head.project.canonical_id="foreign"},"pa_scope_denied")
  rejects(a=>{a.head.current_fence.project.project_id="foreign"},"pa_scope_denied")
  rejects(a=>{a.items[0].message!.fence.key.bot_id="foreign"},"pa_scope_denied")
  rejects(a=>{a.head.conversation.id="foreign"},"pa_scope_denied")
  rejects(a=>{a.source.machine_id="foreign"},"pa_scope_denied")
  rejects(a=>{a.source_epoch="old"},"pa_cursor_expired","history")
  rejects(a=>{a.has_more=true})
  rejects(a=>{a.next_cursor="cursor"})
  rejects(a=>{a.has_more=true;a.next_cursor=request("history").cursor},"pa_invalid","history")
  rejects(a=>{a.has_more=true;a.next_cursor="next";a.items=[]})
  rejects(a=>{a.items[0].work={id:"wrong"} as W.ProjectAssistantWorkLink})
  rejects(a=>{a.items[0].kind="work"})
  rejects(a=>{a.items.push(a.items[0])})
  rejects(a=>{a.snapshot=undefined})
  rejects(a=>{a.snapshot=answer().snapshot},"pa_invalid","history")
})
test("snapshot contains all sixteen pending summaries and queued messages without a fabricated full Turn",()=>{
  const a=answer(),s=a.snapshot!
  s.pending_turns=Array.from({length:16},(_,i)=>({id:`turn-${i}`,fence,message_id:`queued-${i}`,state:"queued",failure_code:"",started_at:""}))
  s.queued_messages=s.pending_turns.map((t,i)=>({...message(t.message_id,i+1),kind:"user",turn_id:t.id}))
  assert.equal(readView(a,request(),fence).snapshot!.queued_messages.length,16)
  assert.equal("instructions" in s.pending_turns[0],false)
  const excess=structuredClone(a);excess.snapshot!.pending_turns.push({...s.pending_turns[0],id:"turn-17"})
  assert.throws(()=>readView(excess,request(),fence),e=>errorCode(e)==="pa_invalid")
  const missing=structuredClone(a);missing.snapshot!.queued_messages.pop()
  assert.throws(()=>readView(missing,request(),fence),e=>errorCode(e)==="pa_invalid")
})
test("only included events advance next_seq, even when the page head has later unseen events",()=>{
  const a=answer("events"),e:W.ProjectAssistantEvent={id:"event-603",seq:603,version:"project-assistants.v1",at:"2026-11-12T00:00:00Z",kind:"message_created",fence,message:message("after-snapshot",601)}
  a.head.event_frontier=604;a.items=[{id:e.id,kind:"event",anchor_message_seq:0,observed_event_seq:603,event:e}];a.next_seq=603;a.has_more=true
  assert.equal(readView(a,request("events"),fence).next_seq,603)
  const skipped=structuredClone(a);skipped.next_seq=604
  assert.throws(()=>readView(skipped,request("events"),fence),e=>errorCode(e)==="pa_invalid")
  a.items=[];a.next_seq=602;a.has_more=false
  assert.equal(readView(a,request("events"),fence).next_seq,602)
  a.has_more=true
  assert.throws(()=>readView(a,request("events"),fence),e=>errorCode(e)==="pa_invalid")
})
test("current-fence summaries have no answering capability; full details must match payload and scope",()=>{
  const a=answer("permissions"),summary:W.ProjectAssistantReadPermissionSummary={id:"offer",fence,turn_id:"turn",state:"pending",expires_at:"2099-01-01T00:00:00Z",detail_cursor:"signed ref"}
  a.items=[{id:summary.id,kind:"permission_summary",anchor_message_seq:0,observed_event_seq:602,permission_summary:summary}]
  assert.equal(readView(a,request("permissions"),fence).items[0].permission,undefined)
  a.items[0].permission_summary!.fence={...fence,generation:1}
  assert.throws(()=>readView(a,request("permissions"),fence),e=>errorCode(e)==="pa_stale_generation")
  const detail=answer("detail"),permission:W.ProjectAssistantPermissionOffer={...summary,fence,native_request_id:"stored native ref",selected_option_id:"",options:[{id:"once",kind:"allow_once",label:"stored label",allow:true,read_only:true}]}
  detail.items=[{id:"offer",kind:"permission",anchor_message_seq:0,observed_event_seq:602,permission}]
  assert.deepEqual(readView(detail,request("detail"),fence).items[0].permission!.options,permission.options)
  detail.items[0].trace={turn_id:"turn",category:"read",text:"wrong payload",complete:true}
  assert.throws(()=>readView(detail,request("detail"),fence),e=>errorCode(e)==="pa_invalid")
})
test("live older pages never overwrite a newer old-generation work event and keep source tuple ordering",()=>{
  const work:W.ProjectAssistantWorkLink={id:"work",turn_id:"turn",fence:{...fence,generation:1},board_item_id:"item",task_id:"task",state:"accepted",landing_commit:"",result_message_id:""}
  const old:W.ProjectAssistantReadViewItem={id:"source-work-row",kind:"work",anchor_message_seq:3,observed_event_seq:602,work}
  const event:W.ProjectAssistantEvent={id:"work-event",seq:604,kind:"work_updated",fence:work.fence,work:{...work,state:"landed",landing_commit:"delivered"},at:"2026-11-12T00:00:00Z",version:"project-assistants.v1"}
  const live=eventTimeline([old],[{id:event.id,kind:"event",anchor_message_seq:0,observed_event_seq:604,event}])
  const merged=mergeTimeline([live,[old,row(message("A",3)),row(message("a",3))]])
  assert.equal(merged.find(r=>r.work)?.work?.state,"landed")
  assert.equal(merged.find(r=>r.work)?.id,"source-work-row")
  assert.equal(merged.find(r=>r.work)?.anchor_message_seq,3)
  assert.deepEqual(merged.map(r=>r.id),["A","a","source-work-row"])
  assert.deepEqual(mergeTimeline([merged,merged]),merged)
  const eventFirst=eventTimeline([],[{id:event.id,kind:"event",anchor_message_seq:0,observed_event_seq:604,event}])
  const positioned=mergeTimeline([eventFirst,[old]])[0]
  assert.equal(positioned.id,"source-work-row");assert.equal(positioned.anchor_message_seq,3);assert.equal(positioned.work?.state,"landed")
})
test("timeline IDs are owned by kind while same-kind duplicate rows remain invalid",()=>{
  const a=answer(),m=message("shared",1)
  const work:W.ProjectAssistantWorkLink={id:"other",fence,turn_id:"turn",board_item_id:"",task_id:"task",state:"accepted",landing_commit:"",result_message_id:""}
  a.items=[row(m),{id:work.id,kind:"work",work,anchor_message_seq:1,observed_event_seq:602}]
  assert.doesNotThrow(()=>readView(a,request(),fence),"distinct-ID control")
  a.items[1].id="shared";a.items[1].work!.id="shared"
  assert.doesNotThrow(()=>readView(a,request(),fence),"the source permits the same stable ID in message and work namespaces")
  assert.deepEqual(mergeTimeline([a.items]).map(r=>[r.kind,r.id]),[["message","shared"],["work","shared"]],"source IDs remain unchanged")
  for(const index of [0,1]){const duplicate=structuredClone(a);duplicate.items.push(duplicate.items[index]);assert.throws(()=>readView(duplicate,request(),fence),e=>errorCode(e)==="pa_invalid","same-kind duplicates still fail")}
})
test("view requests enforce each mode and retain complete opaque cursors",()=>{
  for(const mode of ["snapshot","events","history","permissions","traces","detail"] as const)assert.deepEqual(readViewRequest(request(mode)),request(mode))
  for(const q of [{...request(),source_epoch:"epoch"},{...request("events"),cursor:"unused"},{...request("history"),after_seq:1},{...request("detail"),page_size:100},{...request(),page_size:101},{...request("events"),page_size:257},{...request("history"),cursor:"界".repeat(4096)}])assert.throws(()=>readViewRequest(q),e=>errorCode(e)==="pa_invalid")
})
test("timeline and control payloads validate their consumed primitives, enums and complete event kind",()=>{
  for(const field of ["id","text","turn_id","quote_message_id","at"]){rejects(a=>{(a.items[0].message as unknown as Record<string,unknown>)[field]=false})}
  rejects(a=>{a.items[0].message!.kind="system" as W.ProjectAssistantMessageKind})
  const w:W.ProjectAssistantWorkLink={id:"work",turn_id:"turn",fence,state:"accepted",board_item_id:"item",task_id:"task",landing_commit:"",result_message_id:""}
  for(const field of ["state","board_item_id","task_id","landing_commit","result_message_id"]){
    const a=answer();a.items=[{id:"work",kind:"work",anchor_message_seq:1,observed_event_seq:602,work:{...w}}]
    ;(a.items[0].work as unknown as Record<string,unknown>)[field]=field==="state"?"other":false
    assert.throws(()=>readView(a,request(),fence),e=>errorCode(e)==="pa_invalid")
  }
  const p:W.ProjectAssistantPermissionOffer={id:"offer",fence,turn_id:"turn",native_request_id:"stored",state:"pending",expires_at:"2099-01-01T00:00:00Z",selected_option_id:"",options:[{id:"once",kind:"allow_once",allow:true,read_only:true,label:"stored"}]}
  const invalidOfferFields:[string,unknown,boolean][]=[["native_request_id",false,false],["selected_option_id",0,false],["state","other",false],["kind","other",true],["read_only","false",true],["allow","true",true],["label",null,true]]
  for(const [field,value,option] of invalidOfferFields){
    const a=answer("detail");a.items=[{id:p.id,kind:"permission",anchor_message_seq:0,observed_event_seq:602,permission:structuredClone(p)}]
    const target=(option?a.items[0].permission!.options[0]:a.items[0].permission!) as unknown as Record<string,unknown>;target[field]=value
    assert.throws(()=>readView(a,request("detail"),fence),e=>errorCode(e)==="pa_invalid")
  }
  const event:W.ProjectAssistantEvent={id:"event",seq:603,at:"2026-11-12T00:00:00Z",version:"project-assistants.v1",fence,kind:"work_updated",work:w}
  const eventChanges:((e:W.ProjectAssistantEvent)=>void)[]=[e=>{e.kind="message_created"},e=>{e.kind="other" as W.ProjectAssistantEventKind},e=>{e.at=false as unknown as string},e=>{e.work!.state="other" as W.ProjectAssistantWorkState},e=>{e.source=answer().source}]
  for(const change of eventChanges){
    const a=answer("events");a.head.event_frontier=603;a.items=[{id:event.id,kind:"event",anchor_message_seq:0,observed_event_seq:603,event:structuredClone(event)}];a.next_seq=603;change(a.items[0].event!)
    assert.throws(()=>readView(a,request("events"),fence),e=>errorCode(e)==="pa_invalid")
  }
})
