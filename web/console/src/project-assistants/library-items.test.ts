import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node loads the source extension.
import {libraryItems,libraryGroups,libraryRevision} from "./library-items.ts"

const fence={key:{machine_id:"machine",bot_id:"bot"},conversation_id:"chat",project:{machine_id:"machine",project_id:"project",canonical_id:"canonical",kind:"repository"},binding_id:"",generation:1} as W.ProjectAssistantFence
const output=(id:string,artifact_id="")=>({id,artifact_id,name:id,bytes:1,sha256:"0".repeat(64)})
const message=(id:string,seq:number,at:string,extra:Partial<W.ProjectAssistantMessage>={}):W.ProjectAssistantMessage=>({id,seq,at,fence,kind:"assistant",text:"",quote_message_id:"",turn_id:"",...extra})

test("library merges output revisions, sent files, input files and pictures without losing repeated attachments",()=>{
  const first=message("old",1,"2026-11-14T10:00:00Z",{output:output("v1","artifact")})
  const latest=message("new",4,"2026-11-16T10:00:00Z",{output:output("v2","artifact")})
  const attached=message("input",3,"2026-11-15T10:00:00Z",{kind:"user",files:[{id:"file",name:"input.txt",bytes:1,sha256:"0".repeat(64)}],attachments:[{id:"picture",bytes:1,media_type:"image/png"}]})
  const repeated=message("repeat",5,"2026-11-16T11:00:00Z",{kind:"user",attachments:attached.attachments})
  const items=libraryItems([first,attached,latest,attached,message("sent",2,"2026-11-14T11:00:00Z",{output:output("sent")}),repeated])
  assert.equal(items.length,5)
  assert.deepEqual(items.map(i=>i.message.id),["repeat","new","input","input","sent"])
  assert.equal(items.filter(i=>i.kind==="output"&&i.message.output?.artifact_id).length,1)
  assert.equal(new Set(items.map(i=>i.key)).size,items.length)
})

test("library dates follow local calendar days with newest groups first",()=>{
  const items=libraryItems([message("a",1,"2026-11-14T12:00:00",{output:output("a")}),message("b",2,"2026-11-15T00:05:00",{output:output("b")}),message("c",3,"2026-11-15T23:00:00",{output:output("c")})])
  const groups=libraryGroups(items)
  assert.deepEqual(groups.map(g=>g.date),["2026-11-15","2026-11-14"])
  assert.deepEqual(groups[0].items.map(i=>i.message.id),["c","b"])
})

test("library refresh revision detects changed counts and replacement metadata but ignores plain chat text",()=>{
  const plain=message("plain",1,"2026-11-14T12:00:00")
  const item=message("artifact",2,"2026-11-14T12:00:00",{output:output("v1","artifact")})
  assert.equal(libraryRevision([item]),libraryRevision([plain,item]))
  assert.notEqual(libraryRevision([item]),libraryRevision([{...item,output:{...item.output!,name:"Renamed"}}]))
  assert.notEqual(libraryRevision([item]),libraryRevision([item,{...plain,files:[{id:"file",name:"data",bytes:0,sha256:"0".repeat(64)}]}]))
})
