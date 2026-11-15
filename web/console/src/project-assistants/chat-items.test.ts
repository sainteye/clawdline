import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { buildChat,buildTimelineChat,mergeMessages,partitionMessages } from "./chat-items.ts"
const message=(id:string,seq:number,kind:W.ProjectAssistantMessageKind="assistant",at="2026-11-12T00:00:00Z")=>({id,seq,kind,at,text:id}) as W.ProjectAssistantMessage
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
