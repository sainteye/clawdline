import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error Node executes source extensions.
import { buildChat,mergeMessages,partitionMessages } from "./chat-items.ts"
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
test("queue to running transition keeps each message on exactly one surface",()=>{
  const a=message("a",1,"user"),b=message("b",2)
  const queued=partitionMessages([a,b],[{message_id:"a",state:"queued"} as W.ProjectAssistantTurn])
  assert.deepEqual(queued.queued,[a]);assert.deepEqual(queued.chat,[b])
  const running=partitionMessages([a,b],[{message_id:"a",state:"running"} as W.ProjectAssistantTurn])
  assert.deepEqual(running.queued,[]);assert.deepEqual(running.chat,[a,b])
})
