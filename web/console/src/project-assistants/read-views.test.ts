import assert from "node:assert/strict"
import { test } from "node:test"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import { readView,readViewRequest,mergeTimeline,eventTimeline,turnSummary,replyInputPreference,steerFallback,queuedSendNowShown,steerFallbackReason,replyInputPatch } from "./read-views.ts"
// @ts-expect-error -- Node executes source extensions.
import { en,zhHant as zh } from "./words.ts"
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
test("interruption details survive snapshot and live-event summaries without requiring newer sources",()=>{
  const a=answer()
  const summary={id:"interrupted",message_id:"input",fence,state:"unknown" as const,failure_code:"execution_effect_unknown",started_at:""}
  a.snapshot!.last_turn={...summary,failure_field:"prompt_timeout"}
  assert.equal(readView(a,request(),fence).snapshot!.last_turn!.failure_field,"prompt_timeout")
  a.snapshot!.last_turn=summary
  assert.equal(readView(a,request(),fence).snapshot!.last_turn!.failure_field,undefined)
  for(const value of [true,7,{}]){
    a.snapshot!.last_turn={...summary,failure_field:value as never}
    assert.throws(()=>readView(a,request(),fence),e=>errorCode(e)==="pa_invalid")
  }
  const live=turnSummary({id:"interrupted",message_id:"input",fence,state:"unknown",failure:{code:"execution_effect_unknown",field:"prompt_timeout",retry_after_seconds:0}} as W.ProjectAssistantTurn)
  assert.equal(live.failure_field,"prompt_timeout")
  assert.equal(live.state,"unknown")
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
test("a running resume or routine notice keeps the whole chat readable",()=>{
  const a=answer(),s=a.snapshot!
  s.pending_turns=[{id:"host-turn",message_id:"host-input",fence,state:"running",failure_code:"",started_at:""},
    {id:"person-turn",message_id:"person-input",fence,state:"queued",failure_code:"",started_at:""}]
  s.queued_messages=[{...message("host-input",601),kind:"notice",fence,turn_id:"host-turn"},
    {...message("person-input",602),kind:"user",fence,turn_id:"person-turn"}]
  const accepted=readView(a,request(),fence).snapshot!
  assert.deepEqual(partitionMessages(accepted.queued_messages,accepted.pending_turns).queued.map(x=>x.id),["person-input"])
  const invalid=structuredClone(a);invalid.snapshot!.queued_messages[0].kind="assistant"
  assert.throws(()=>readView(invalid,request(),fence),e=>errorCode(e)==="pa_invalid")
})
test("the retained conversation memory revision can precede the source memory head",()=>{
  const a=answer();a.head.memory_revision=2
  assert.equal(readView(a,request(),fence).head.conversation.memory_revision,1)
  assert.equal(readView(a,request(),fence).head.memory_revision,2)
})
test("an own-chat terminal outcome survives a later room execution generation",()=>{
  const a=answer(),old={...fence,binding_id:"earlier-own-chat",generation:1}
  a.snapshot!.last_turn={id:"own-chat",message_id:"own-input",fence:old,state:"succeeded",failure_code:"",started_at:""}
  assert.equal(readView(a,request(),fence).snapshot!.last_turn!.id,"own-chat")
  a.snapshot!.last_turn.fence={...old,generation:3}
  assert.throws(()=>readView(a,request(),fence),e=>errorCode(e)==="pa_invalid")
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
test("work arriving live sorts after its source turn exactly as on reload",()=>{
  const earlier=row({...message("earlier",4),turn_id:"other"})
  const notice=row({...message("peer-dispatch-task",5),kind:"notice"})
  const later=row({...message("later",6),turn_id:"later"})
  const work:W.ProjectAssistantWorkLink={id:"work",turn_id:"turn",fence,board_item_id:"",task_id:"task",state:"accepted",landing_commit:"",result_message_id:""}
  const event:W.ProjectAssistantEvent={id:"work-event",seq:604,kind:"work_updated",fence,work,at:"2026-11-12T00:00:00Z",version:"project-assistants.v1"}
  const live=eventTimeline([earlier,notice,later],[{id:event.id,kind:"event",anchor_message_seq:5,observed_event_seq:604,event}])
  assert.deepEqual(live.map(r=>r.id),["earlier","peer-dispatch-task","work","later"])
  assert.equal(live.find(r=>r.work)?.anchor_message_seq,5)
  const oldEvent={id:event.id,kind:"event" as const,anchor_message_seq:0,observed_event_seq:604,event}
  const oldSource=eventTimeline([earlier,notice,later],[oldEvent])
  assert.deepEqual(oldSource,live)
  const newest=eventTimeline([earlier,later],[oldEvent])
  assert.deepEqual(newest.map(r=>r.id),["earlier","later","work"])
  assert.deepEqual(mergeTimeline([newest,[notice]]),live)
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

test("artifact renames update earlier cards while stale rename events cannot downgrade a newer snapshot",()=>{
  const output:W.ProjectAssistantOutput={id:"publication",artifact_id:"artifact",name:"Original",bytes:0,sha256:"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",title_version:1}
  const earlier=row({...message("earlier",1),output:{...output,id:"older-publication"}})
  const latest={...message("latest",2),output}
  const event:W.ProjectAssistantEvent={id:"rename",seq:604,version:"project-assistants.v1",fence:latest.fence,kind:"artifact_renamed",at:latest.at,message:{...latest,output:{...output,name:"Renamed",title_version:2}}}
  const changed=eventTimeline([earlier,row(latest)],[{id:event.id,kind:"event",anchor_message_seq:0,observed_event_seq:604,event}])
  assert.deepEqual(changed.map(r=>r.message?.output?.name),["Renamed","Renamed"])
  assert.equal(changed[0].message?.output?.id,"older-publication")
  assert.equal(earlier.message?.output?.name,"Original","input publication is unchanged")
  const newer={...earlier,observed_event_seq:605,message:{...earlier.message!,output:{...earlier.message!.output!,name:"Later title",title_version:3}}}
  assert.equal(eventTimeline([newer],[{id:event.id,kind:"event",anchor_message_seq:0,observed_event_seq:604,event}]).find(r=>r.id==="earlier")?.message?.output?.name,"Later title")
})

test("reaction events preserve artifact titles, chips survive renames and unloaded targets stay unloaded",()=>{
 const reaction:W.ProjectAssistantReactionActor={id:"human-reader",kind:"human",name:"",revision:1,emojis:["👍"],own:true}
 const output:W.ProjectAssistantOutput={id:"publication",artifact_id:"artifact",name:"Renamed",bytes:0,sha256:"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",title_version:2}
 const m={...message("published",1),output}
 const wrap=(e:W.ProjectAssistantEvent):W.ProjectAssistantReadViewItem=>({id:e.id,kind:"event",anchor_message_seq:0,observed_event_seq:e.seq,event:e})
 const react:W.ProjectAssistantEvent={id:"react",seq:603,version:"project-assistants.v1",fence:m.fence,at:m.at,kind:"reactions_changed",message:{...m,output:{...output,name:"Original",title_version:1},reactions:[reaction]}}
 const reacted=eventTimeline([row(m)],[wrap(react)])
 assert.equal(reacted[0].message?.output?.name,"Renamed");assert.deepEqual(reacted[0].message?.reactions,[reaction])
 const rename:W.ProjectAssistantEvent={...react,id:"rename",seq:604,kind:"artifact_renamed",message:{...m,output:{...output,name:"Later",title_version:3}}}
 const renamed=eventTimeline(reacted,[wrap(rename)])
 assert.equal(renamed[0].message?.output?.name,"Later");assert.deepEqual(renamed[0].message?.reactions,[reaction])
 assert.deepEqual(eventTimeline([], [wrap(react)]),[])
 assert.equal(m.output.name,"Renamed")
})

test("trace details retain distinct step identities and accept legacy category identities",()=>{
  for(const id of [undefined,"segment-1","tool-read"]){
    const a=answer("detail"),trace={...(id?{id}:{}),turn_id:"turn",category:"agent_message_chunk",text:"Complete step",complete:false}
    a.items=[{id:JSON.stringify([trace.turn_id,id||trace.category]),kind:"trace",anchor_message_seq:0,observed_event_seq:602,trace}]
    assert.equal(readView(a,request("detail"),fence).items[0].trace?.text,"Complete step")
    a.items[0].trace!.id=42 as never
    assert.throws(()=>readView(a,request("detail"),fence),e=>errorCode(e)==="pa_invalid")
  }
})

test("a Bot without a saved reply-input choice steers, and only an explicit queue choice queues",()=>{
  assert.equal(replyInputPreference(bot),"steer")
  assert.equal(replyInputPreference({...bot,reply_input:"queue"}),"queue")
  assert.equal(replyInputPreference({...bot,reply_input:"steer"}),"steer")
})
test("a queued turn's steer fallback reason survives the snapshot and live-turn summaries",()=>{
  const a=answer()
  const queued={id:"queued",message_id:"later",fence,state:"queued" as const,failure_code:"",started_at:"",steer_fallback:"permission_pending"}
  a.snapshot!.pending_turns=[queued as W.ProjectAssistantReadTurnSummary]
  a.snapshot!.queued_messages=[{...message("later",601),kind:"user",turn_id:"queued"}]
  a.head.conversation.last_message_seq=601
  assert.equal(steerFallback(readView(a,request(),fence).snapshot!.pending_turns[0]),"permission_pending")
  rejects(b=>{b.snapshot!.pending_turns=[{...queued,steer_fallback:7} as unknown as W.ProjectAssistantReadTurnSummary];b.snapshot!.queued_messages=[{...message("later",601),kind:"user",turn_id:"queued"}];b.head.conversation.last_message_seq=601})
  const live={id:"queued",fence,message_id:"later",state:"queued",steer_fallback:"images_unsupported"} as unknown as W.ProjectAssistantTurn
  assert.equal(steerFallback(turnSummary(live)),"images_unsupported")
  assert.equal(steerFallback(turnSummary({...live,steer_fallback:undefined} as unknown as W.ProjectAssistantTurn)),"")
})
test("send now appears for a steering Bot only after the source fell back to the queue",()=>{
  assert.equal(queuedSendNowShown("steer",""),false)
  assert.equal(queuedSendNowShown("steer","agent_unsupported"),true)
  assert.equal(queuedSendNowShown("queue",""),true)
})
test("each steer fallback code reads as a short reason, and an unknown code as plain queued",()=>{
  for(const [language,words] of [["en",en],["zh-Hant",zh]] as const){
    const reasons=["agent_unsupported","images_unsupported","permission_pending","turn_ended","scope"].map(code=>steerFallbackReason(code,words))
    assert.equal(new Set(reasons).size,reasons.length,language)
    for(const reason of reasons)assert.ok(reason&&reason!==words.steerFallbackQueued,language)
    assert.equal(steerFallbackReason("something_new",words),words.steerFallbackQueued)
    assert.equal(steerFallbackReason("",words),"")
  }
  assert.equal(steerFallbackReason("agent_unsupported",zh),"這個 agent 不支援插話，已排隊")
  assert.equal(steerFallbackReason("images_unsupported",zh),"這個 agent 不支援帶圖插話，已排隊")
  assert.equal(steerFallbackReason("permission_pending",zh),"等待權限確認，已排隊")
})
test("saving settings sends reply_input only when the switch moved from what the Bot holds",()=>{
  assert.deepEqual(replyInputPatch(bot,"steer"),{})
  assert.deepEqual(replyInputPatch(bot,"queue"),{reply_input:"queue"})
  assert.deepEqual(replyInputPatch({...bot,reply_input:"queue"},"queue"),{})
  assert.deepEqual(replyInputPatch({...bot,reply_input:"queue"},"steer"),{reply_input:"steer"})
})
