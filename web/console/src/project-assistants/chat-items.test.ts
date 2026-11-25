import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { buildChat,buildTimelineChat,isSendingAdmitted,mergeMessages,partitionMessages,placeTurn,queueAhead,queuedLabel,workingSubject } from "./chat-items.ts"
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
test("a message with other work ahead waits from the start instead of jumping into the queue later",()=>{
  // 2026-11-25: the person's message released a restart hold. Nothing ran
  // for a moment, so it was drawn as sent, then moved below the indicator
  // once an older report started. Work ahead of it keeps it waiting.
  const report={...message("report",1,"notice")},mine=message("mine",2,"user")
  const turns=[{message_id:"report",state:"queued"},{message_id:"mine",state:"queued"}] as W.ProjectAssistantTurn[]
  const parts=partitionMessages([report,mine],turns)
  assert.deepEqual(parts.queued,[mine])
  assert.equal(queueAhead("mine",turns),1)
  assert.equal(queueAhead("mine",[{message_id:"x",state:"running"},...turns] as W.ProjectAssistantTurn[]),2)
  // Queue order, not message order: a message the worker runs first has nothing ahead.
  const first=[{message_id:"mine",state:"queued"},{message_id:"report",state:"queued"}] as W.ProjectAssistantTurn[]
  assert.equal(queueAhead("mine",first),0)
  assert.deepEqual(partitionMessages([report,mine],first).queued,[])
  assert.equal(queueAhead("mine",[{message_id:"old",state:"succeeded"},{message_id:"mine",state:"queued"}] as W.ProjectAssistantTurn[]),0)
})
test("the working indicator names a work report it is reviewing, not the person's message",()=>{
  const words={workingOnReport:"Work report: {title}",workingOnReportUntitled:"Work report",workingOnRestart:"Continuing"}
  const review={...message("review",1,"notice"),notice:{key:"work_review",cause:"task-1"}} as W.ProjectAssistantMessage
  const resume={...message("resume",2,"notice"),notice:{key:"restart_continuation",cause:""}} as W.ProjectAssistantMessage
  const mine=message("mine",3,"user")
  const work={task_id:"task-1",title:"Clean up checkouts"} as W.ProjectAssistantWorkLink
  const rows=(...items:(W.ProjectAssistantMessage|W.ProjectAssistantWorkLink)[])=>items.map(item=>"seq" in item?{id:item.id,kind:"message",message:item}:{id:"w",kind:"work",work:item}) as W.ProjectAssistantReadViewItem[]
  assert.equal(workingSubject({message_id:"review"},rows(review,mine,work),words),"Work report: Clean up checkouts")
  assert.equal(workingSubject({message_id:"review"},rows(review),words),"Work report")
  assert.equal(workingSubject({message_id:"resume"},rows(resume),words),"")
  assert.equal(workingSubject({message_id:"mine"},rows(mine,work),words),"")
  assert.equal(workingSubject(undefined,rows(mine,work),words),"")
})
test("a live event places the releasing message where the daemon runs it",()=>{
  const t=(id:string,origin:string,extra={})=>({id,message_id:id,state:"queued",origin,...extra}) as W.ProjectAssistantReadTurnSummary
  const held=[t("earlier","user"),t("report","host_notice"),t("reply","bot_reply")]
  assert.deepEqual(placeTurn(held,t("mine","user",{released_restart:true})).map(x=>x.id),["earlier","mine","report","reply"])
  assert.deepEqual(placeTurn(held,t("plain","user")).map(x=>x.id),["earlier","report","reply","plain"])
  const running={...t("report","host_notice"),state:"running"} as W.ProjectAssistantReadTurnSummary
  assert.deepEqual(placeTurn(held,running).map(x=>x.id),["earlier","report","reply"],"a known turn keeps its place")
  const words={queued:"Waiting",queuePaused:"Paused",queuedAhead:"{count} ahead"}
  assert.equal(queuedLabel("reply",held,false,words),"Waiting · 2 ahead")
  assert.equal(queuedLabel("earlier",held,true,words),"Paused")
})
