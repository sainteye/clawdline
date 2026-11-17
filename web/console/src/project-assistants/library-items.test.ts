import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node loads the source extension.
import {libraryItems,libraryGroups,libraryRevision,resolvedLibraryTitle,mergeLibraryMessages} from "./library-items.ts"

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

test("a verified old artifact title changes its card without changing publication content or scope",()=>{
  const original=message("old-publication",1,"2026-11-14T12:00:00",{output:{...output("v1","artifact"),name:"Old",title_version:1}})
  const other=message("other",2,"2026-11-14T13:00:00",{output:output("other","another-artifact")})
  const foreign={...original,id:"foreign",fence:{...fence,key:{...fence.key,bot_id:"another-bot"}}}
  const resolved={...original.output!,id:"new-publication",name:"Renamed",title_version:2,bytes:99,sha256:"a".repeat(64)}
  const rows=resolvedLibraryTitle([original,other,foreign],fence,resolved)
  assert.equal(rows[0].output!.name,"Renamed")
  assert.equal(rows[0].output!.title_version,2)
  assert.equal(rows[0].output!.id,original.output!.id)
  assert.equal(rows[0].output!.sha256,original.output!.sha256)
  assert.equal(rows[0].output!.bytes,original.output!.bytes)
  assert.equal(rows[0].at,original.at)
  assert.equal(rows[0].fence,original.fence)
  assert.equal(rows[1],other);assert.equal(rows[2],foreign)
  assert.equal(resolvedLibraryTitle(rows,fence,original.output!),rows,"a late old title replaced the observed name")
})

test("a delayed library history page cannot undo a newer title or carry it to a foreign conversation",()=>{
  const original=message("artifact",1,"2026-11-14T12:00:00",{output:{...output("v1","artifact"),name:"Old",title_version:1}})
  const fresh={...original,output:{...original.output!,name:"Renamed",title_version:2}}
  const delayed=mergeLibraryMessages([fresh],[original])
  assert.equal(libraryItems(delayed)[0].message.output!.name,"Renamed")
  const other={...original,fence:{...fence,conversation_id:"another-chat"}}
  assert.equal(mergeLibraryMessages([fresh],[other])[0].output!.name,"Old")
})
