import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { buildChat,buildTimelineChat,isSendingAdmitted,mergeMessages,partitionMessages,selectInlineProgress } from "./chat-items.ts"
// @ts-expect-error Node executes source extensions.
import {admittedReceipt,admittedMessage,sendAcknowledgement,ChatObservation} from "./chat-observation.ts"
const message=(id:string,seq:number,kind:W.ProjectAssistantMessageKind="assistant",at="2026-11-12T00:00:00Z")=>({id,seq,kind,at,text:id}) as W.ProjectAssistantMessage
test("an accepted send is replaced only by its own source turn and message",()=>{
  const key={machine_id:"machine",bot_id:"bot"},project={machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"place" as const}
  const fence={key,project,conversation_id:"conversation",binding_id:"old-binding",generation:1}
  const intent={command:{key,operation:"send" as const,idempotency_key:"own-send",content_hash:""},fence,text:"same words",quote_message_id:""}
  const receipt={command:intent.command,resource_id:"own-turn",state:"accepted",id:"receipt",event_seq:1} as W.ProjectAssistantReceipt
  const canonical={...message("own-message",1,"user"),text:intent.text,turn_id:"own-turn",fence}
  assert.equal(admittedMessage(intent,receipt,[{...canonical,turn_id:"other-turn"}]),false)
  assert.equal(admittedMessage(intent,receipt,[{...canonical,fence:{...fence,conversation_id:"other"}}]),false)
  assert.equal(admittedMessage(intent,{...receipt,command:{...receipt.command,idempotency_key:"other-command"}},[canonical]),false)
  assert.equal(admittedMessage(intent,receipt,[canonical]),true)
  const answer={contract_version:"project-assistants.v1",receipt}
  assert.equal(sendAcknowledgement(answer,intent),true)
  assert.equal(sendAcknowledgement({contract_version:"project-assistants.v1"},intent),false)
  assert.equal(sendAcknowledgement({...answer,receipt:{...receipt,command:{...receipt.command,idempotency_key:"other-send"}}},intent),false)
  assert.equal(admittedReceipt({...receipt,state:"unknown"}),false)
  assert.equal(admittedReceipt({...receipt,state:"failed"}),false)
})
test("an owned turn needs a final snapshot after a terminal event, including reordered answers",()=>{
  const receipt={resource_id:"own-turn",state:"accepted",event_seq:10,command:{operation:"send"}} as W.ProjectAssistantReceipt
  const watch=new ChatObservation(receipt)
  assert.equal(watch.active,true)
  watch.apply({head:{event_frontier:9},snapshot:{pending_turns:[]},items:[]} as unknown as W.ProjectAssistantReadViewResponse)
  assert.equal(watch.active,true,"an older busy read cannot retire a later admitted turn")
  watch.apply({head:{event_frontier:10},snapshot:{pending_turns:[]},items:[]} as unknown as W.ProjectAssistantReadViewResponse)
  assert.equal(watch.active,true,"an absent pending row is not the owned turn's terminal evidence")
  watch.apply({items:[{event:{turn:{id:"other-turn",state:"succeeded"}}}]} as W.ProjectAssistantReadViewResponse)
  assert.equal(watch.finalSnapshot,false)
  watch.apply({items:[{event:{turn:{id:"own-turn",state:"succeeded"}}}]} as W.ProjectAssistantReadViewResponse)
  assert.equal(watch.active,true);assert.equal(watch.finalSnapshot,true)
  watch.apply({head:{event_frontier:20},snapshot:{pending_turns:[],last_turn:{id:"own-turn",state:"succeeded"}},items:[{message:message("final-answer",2)}]} as unknown as W.ProjectAssistantReadViewResponse)
  assert.equal(watch.active,false);assert.equal(watch.finalSnapshot,false)
  watch.accept(receipt);watch.stop();assert.equal(watch.active,false)
  watch.accept({...receipt,state:"unknown"});assert.equal(watch.active,false)
})
test("replay and earlier history produce one bubble per message in source order",()=>{
  const a=message("a",1),b=message("b",2)
  assert.deepEqual(mergeMessages([[b],[a,b]]).map(m=>m.id),["a","b"])
  assert.equal(buildChat([a,b,b]).filter(i=>i.kind==="message").length,2)
})
test("Codync author grouping resets after a time separator",()=>{
  const rows=buildChat([message("a",1),message("b",2),message("c",3,"assistant","2026-11-12T02:00:00Z")])
  assert.deepEqual(rows.filter(i=>i.kind==="message").map(i=>i.groupStart),[true,false,true])
  assert.equal(rows.filter(i=>i.kind==="separator").length,2)
})
test("chat marks a half-hour pause or a new local day between messages",()=>{
  const rows=buildChat([
    message("first",1,"user","2026-11-12T22:50:00"),
    message("immediate",2,"assistant","2026-11-12T22:51:00"),
    message("after-pause",3,"assistant","2026-11-12T23:21:00"),
    message("before-midnight",4,"user","2026-11-12T23:58:00"),
    message("after-midnight",5,"assistant","2026-11-13T00:02:00"),
  ])
  assert.deepEqual(rows.filter(i=>i.kind==="separator").map(i=>i.id),
    ["sep-first","sep-after-pause","sep-before-midnight","sep-after-midnight"])
})
test("work is interleaved at admission, resets author grouping and has its own ID namespace",()=>{
  const a=message("shared",1),b=message("b",2),c=message("c",3,"user","2026-11-12T02:00:00Z")
  const rows=[a,b,c].map(message=>({id:message.id,kind:"message" as const,message,anchor_message_seq:message.seq,observed_event_seq:10}))
  const work={id:"shared",turn_id:"turn",state:"landed",landing_commit:"new-result"} as W.ProjectAssistantWorkLink
  const card:W.ProjectAssistantReadViewItem={id:work.id,kind:"work",work,anchor_message_seq:1,observed_event_seq:20}
  const items=buildTimelineChat([rows[2],card,rows[0],rows[1],card])
  assert.deepEqual(items.filter(i=>i.kind!=="separator").map(i=>[i.kind,i.id]),[["message","shared"],["work","shared"],["message","b"],["message","c"]])
  assert.deepEqual(items.filter(i=>i.kind==="message").map(i=>i.groupStart),[true,true,true])
  assert.equal(items.filter(i=>i.kind==="separator").length,2,"work has no invented date; message gaps still separate")
  assert.equal(items.find(i=>i.kind==="work")?.work.landing_commit,"new-result")
})
test("queue to running transition keeps each message on exactly one surface",()=>{
  const a=message("a",1,"user"),b=message("b",2)
  const queued=partitionMessages([a,b],[{message_id:"x",state:"running"} as W.ProjectAssistantTurn,{message_id:"a",state:"queued"} as W.ProjectAssistantTurn])
  assert.deepEqual(queued.queued,[a]);assert.deepEqual(queued.chat,[b])
  // Nothing running: the message about to run is in the conversation, not a queue.
  const next=partitionMessages([a,b],[{message_id:"a",state:"queued"} as W.ProjectAssistantTurn])
  assert.deepEqual(next.queued,[]);assert.deepEqual(next.chat,[a,b])
  const running=partitionMessages([a,b],[{message_id:"a",state:"running"} as W.ProjectAssistantTurn])
  assert.deepEqual(running.queued,[]);assert.deepEqual(running.chat,[a,b])
})
test("the held message and the work behind it remain editable when the Bot becomes idle",()=>{
  const held=message("held",1,"user"),behind=message("behind",2,"user")
  const parts=partitionMessages([held,behind],[
    {message_id:held.id,state:"queued",queue_edit_id:"edit"},
    {message_id:behind.id,state:"queued"},
  ])
  assert.deepEqual(parts.queued,[held,behind])
  assert.deepEqual(parts.chat,[])
})
test("in-flight sending bubble is suppressed once conversation admits the message",()=>{
  const prior=message("msg-1",1,"user")
  prior.text="hello"
  // 1. No sending text: not admitted
  assert.equal(isSendingAdmitted(null,[],0),false)
  assert.equal(isSendingAdmitted("",[],0),false)

  // 2. Sending in flight, messages do not have it yet: not admitted
  assert.equal(isSendingAdmitted("hello",[],0),false)
  assert.equal(isSendingAdmitted("new message",[prior],1),false)

  // 3. Repeated identical text in history with seq <= baseline: not admitted
  assert.equal(isSendingAdmitted("hello",[prior],1),false)

  // 4. Same text from non-user (e.g. assistant reply): not admitted
  const assistantEcho=message("bot-echo",2,"assistant")
  assistantEcho.text="hello"
  assert.equal(isSendingAdmitted("hello",[prior,assistantEcho],1),false)

  // 5. Admitted new user message with seq > baseline and matching text: admitted!
  const admitted=message("msg-2",2,"user")
  admitted.text="hello"
  assert.equal(isSendingAdmitted("hello",[prior,admitted],1),true)
})

test("selectInlineProgress folds completed narration and post-reply wrap-up into processing details while keeping live turns inline",()=>{
  const timeline:W.ProjectAssistantReadViewItem[]=[
    {id:"u1",kind:"message",anchor_message_seq:1,observed_event_seq:1,message:{...message("u1",1,"user","2026-11-25T05:00:00Z"),turn_id:"turn-done",text:"Check status"}},
    {id:"a1",kind:"message",anchor_message_seq:2,observed_event_seq:3,message:{...message("a1",2,"assistant","2026-11-25T05:00:02Z"),turn_id:"turn-done",text:"核對完了，沒有重做任何操作。"}},
    {id:"u2",kind:"message",anchor_message_seq:3,observed_event_seq:6,message:{...message("u2",3,"user","2026-11-25T05:01:00Z"),turn_id:"turn-live",text:"Run next check"}},
    {id:"a2",kind:"message",anchor_message_seq:4,observed_event_seq:7,message:{...message("a2",4,"assistant","2026-11-25T05:01:02Z"),turn_id:"turn-live",text:"First check finished."}},
  ]
  const progressRows:W.ProjectAssistantReadViewItem[]=[
    {id:"p-mid",kind:"trace",anchor_message_seq:0,observed_event_seq:2,occurred_at:"2026-11-25T05:00:01Z",trace:{id:"mid",turn_id:"turn-done",category:"agent_message_chunk",text:"Checking records...",complete:true}},
    {id:"p-exact",kind:"trace",anchor_message_seq:0,observed_event_seq:4,occurred_at:"2026-11-25T05:00:03Z",trace:{id:"exact",turn_id:"turn-done",category:"agent_message_chunk",text:"核對完了，沒有重做任何操作。",complete:true}},
    {id:"p-wrapup",kind:"trace",anchor_message_seq:0,observed_event_seq:5,occurred_at:"2026-11-25T05:00:04Z",trace:{id:"wrapup",turn_id:"turn-done",category:"agent_message_chunk",text:"I checked the records only; nothing was redone.",complete:true}},
    {id:"p-live-dup",kind:"trace",anchor_message_seq:0,observed_event_seq:8,occurred_at:"2026-11-25T05:01:02Z",trace:{id:"live-dup",turn_id:"turn-live",category:"agent_message_chunk",text:"First check finished.",complete:false}},
    {id:"p-live",kind:"trace",anchor_message_seq:0,observed_event_seq:9,occurred_at:"2026-11-25T05:01:03Z",trace:{id:"live",turn_id:"turn-live",category:"agent_message_chunk",text:"Checking second part...",complete:false}},
  ]
  const selected=selectInlineProgress(progressRows,timeline,{
    activeTurnID:"turn-live",
    progressThrough:Date.parse("2026-11-25T05:00:00Z"),
    isVisible:t=>t.text.trim().length>0,
  })
  assert.deepEqual(selected.inlineProgress.map(p=>[p.id,p.text]),[["p-live","Checking second part..."]])
  assert.equal(selected.projectedSteps.has("p-exact"),true,"exact reply duplicate stays suppressed from Trace")
  assert.equal(selected.projectedSteps.has("p-wrapup"),false,"post-reply wrap-up returns to collapsible Trace")
  assert.equal(selected.projectedSteps.has("p-mid"),false,"completed intermediate narration returns to collapsible Trace")
  assert.equal(selected.projectedSteps.has("p-live"),true,"live inline narration is not duplicated in live Trace")
  assert.equal(selected.projectedSteps.has("p-live-dup"),true)
  assert.deepEqual([...selected.completedProgressTurns],["turn-done"])
})
