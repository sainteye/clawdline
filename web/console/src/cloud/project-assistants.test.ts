import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import { ProjectAssistantSourceClient, ProjectAssistantRegistry, assistantRelayRequest, capturedAssistantRelayFetch, type AssistantGrant, type AssistantFetch } from "./project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import { command, errorCode } from "../project-assistants/client.ts"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
// @ts-expect-error -- Node executes source extensions.
import { CARRIED } from "./carry-generated.ts"

const signal = () => new AbortController().signal
const viewRequest:W.ProjectAssistantReadViewRequest={key:{machine_id:"one",bot_id:"same/id"},conversation_id:"chat",mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100}
test("bounded view keeps API58 metadata and requires API59 only for its captured read",async()=>{
  let calls=0
  const old=make(async()=>{calls++;return Response.json(sourceStatus())}).source
  await old.status(signal())
  await assert.rejects(old.view(viewRequest,signal()),e=>errorCode(e)==="pa_unsupported")
  assert.equal(calls,1)
  const paths:string[]=[]
  const fresh=make(async path=>{paths.push(path);return Response.json(viewWire())},{...grant(),apiLevel:61}).source
  await fresh.view(viewRequest,signal())
  const routed=assistantRelayRequest(paths[0])
  assert.equal(routed.word,"project-assistants.view");assert.equal(routed.kind,"read")
  assert.deepEqual(routed.fields,viewRequest)
  assert.equal(new URL(paths[0],"http://source.invalid").pathname,"/v1/project-assistants/same%2Fid/view")
})
const key = {machine_id:"one",bot_id:"same/id"}
const grant = (machineID = "one"): AssistantGrant => ({accountKey:"account",readerID:"trusted-reader",machineID,authorizationRevision:"grant-1",apiLevel:60,paired:true,online:true,writable:true})
const sourceStatus = (access: W.ProjectAssistantAccess = "interactive"): W.ProjectAssistantSourcesResponse => ({contract_version:"project-assistants.v1",sources:[{machine_id:"one",access,state:"online",fresh:true,readable:true,writable:access === "interactive",observed_at:"",capabilities:[]}]})
const viewWire=():W.ProjectAssistantReadViewResponse=>{
  const project:W.ProjectAssistantProjectScope={machine_id:"one",project_id:"project",canonical_id:"canonical",kind:"repository"}
  const fence:W.ProjectAssistantFence={key,project,conversation_id:"chat",binding_id:"",generation:0}
  const bot:W.ProjectAssistantBot={key,project,conversation_id:"chat",name:"Synthetic Bot",agent:"codex",permission_policy:"ask",appearance:{shape:"blob",color:"#e39567"},instructions:{text:"",revision:1,role_snapshot_ref:""},version:1,created_at:"2026-11-12T00:00:00Z",updated_at:"2026-11-12T00:00:00Z"}
  return {contract_version:"project-assistants.v1",mode:"snapshot",source:sourceStatus().sources[0],source_epoch:"epoch",head:{key,project,current_fence:fence,conversation:{key,id:"chat",binding_id:"",generation:0,active_turn_id:"",last_message_seq:0,memory_revision:1,version:1},memory_revision:1,bot_version:1,event_frontier:0,unknown_turn_count:0,pending_permission_count:0,trace_category_count:0},snapshot:{bot,pending_turns:[],queued_messages:[]},items:[],has_more:false,next_cursor:"",next_seq:0}
}
const make = (transport: AssistantFetch, first = grant()) => {
  let current: AssistantGrant | null = first
  const source = new ProjectAssistantSourceClient(first,transport,() => current)
  return {source,setGrant(next: AssistantGrant | null) {current = next}}
}
test("view rejects raw truthy Source flags before a captured writable grant can coerce them",async()=>{
  for(const writable of [true,false])for(const field of ["readable","writable","fresh"]){
    const source=sourceStatus().sources[0]
    ;(source as unknown as Record<string,unknown>)[field]="false"
    const client=make(async()=>Response.json({...viewWire(),source}),{...grant(),apiLevel:61,writable}).source
    await assert.rejects(client.view(viewRequest,signal()),e=>errorCode(e)==="pa_invalid")
  }
})
test("old, unknown, unpaired and offline sources issue no nonexistent API requests",async () => {
  let calls = 0
  for (const changed of [{apiLevel:57},{apiLevel:undefined},{paired:false},{online:false}]) {
    const {source} = make(async () => {calls++; return Response.json({})},{...grant(),...changed})
    const answer = await source.status(signal())
    assert.equal(answer.sources[0].readable,false)
    await assert.rejects(source.list({machine_id:"one",cursor:"",query:"",page_size:100},signal()))
  }
  assert.equal(calls,0)
})
test("each captured source routes identical Bot ids independently of the active machine",async () => {
  const calls: string[] = []
  const a = make(async path => {calls.push("one:"+path); return Response.json({})}).source
  const b = make(async path => {calls.push("two:"+path); return Response.json({})},grant("two")).source
  await Promise.all([a.get(key,signal()),b.get({...key,machine_id:"two"},signal())])
  assert.deepEqual(calls,["one:/v1/project-assistants/same%2Fid?machine_id=one","two:/v1/project-assistants/same%2Fid?machine_id=two"])
  await assert.rejects(a.get({...key,machine_id:"two"},signal()),e => errorCode(e) === "pa_scope_denied")
})
test("a grant change aborts its in-flight signal and rejects late successful responses",async () => {
  let release!: (value: Response) => void
  const captured: AbortSignal[] = []
  const waiting = new Promise<Response>(resolve => {release=resolve})
  const {source,setGrant} = make(async (_path,init) => {if(init?.signal)captured.push(init.signal); return waiting})
  const request = source.get(key,signal())
  setGrant({...grant(),authorizationRevision:"grant-2"}); source.revoke()
  assert.equal(captured[0].aborted,true)
  release(Response.json({bot:"old-account"}))
  await assert.rejects(request,e => errorCode(e) === "pa_revoked")
})
test("logout during JSON decoding cannot publish a response to the next account",async () => {
  let release!: (value: unknown) => void
  const body = new Promise(resolve => {release=resolve})
  const response = new Response("{}")
  response.json = () => body
  const {source,setGrant} = make(async () => response)
  const request = source.get(key,signal())
  await Promise.resolve(); setGrant(null); release({contract_version:"project-assistants.v1"})
  await assert.rejects(request,e => errorCode(e) === "pa_revoked")
})
test("source errors cross HTTP unchanged and unknown send outcomes are never retried",async () => {
  const refusal: W.ProjectAssistantErrorResponse = {contract_version:"project-assistants.v1",error:{code:"pa_stale_generation",field:"fence",retry_after_seconds:0}}
  const {source} = make(async () => Response.json(refusal,{status:409}))
  await assert.rejects(source.get(key,signal()),error => {assert.deepEqual(error,refusal); return true})
  let sends = 0
  const second = make(async path => {if (path.endsWith("/status")) return Response.json(sourceStatus()); sends++; throw new TypeError("network lost")}).source
  await second.status(signal())
  const send = {command:command(key,"send","stable"),text:"hello",quote_message_id:""} as W.ProjectAssistantSendRequest
  await assert.rejects(second.send(send,signal()),e => errorCode(e) === "pa_outcome_unknown")
  assert.equal(sends,1)
})
test("history-only permits history and export but rejects editing, creating and sending",async () => {
  let writes = 0
  const {source} = make(async (path,init) => {if (init?.method !== "GET") writes++; return Response.json(path.endsWith("/status") ? sourceStatus("history_only") : {})})
  await source.status(signal()); await source.get(key,signal()); await source.export(key,signal())
  await assert.rejects(source.send({command:command(key,"send"),text:"no"} as W.ProjectAssistantSendRequest,signal()),e => errorCode(e) === "pa_read_only")
  await assert.rejects(source.edit({command:command(key,"edit")} as W.ProjectAssistantEditRequest,signal()),e => errorCode(e) === "pa_read_only")
  await assert.rejects(source.create({command:command(key,"create")} as W.ProjectAssistantCreateRequest,signal()),e => errorCode(e) === "pa_read_only")
  assert.equal(writes,0)
})
test("write headers preserve the command key and read cursors use the host reader principal",async () => {
  const bodies: {path:string;init:RequestInit|undefined}[] = []
  const {source} = make(async (path,init) => {bodies.push({path,init});return Response.json(path.endsWith("/status") ? sourceStatus() : {})})
  await source.status(signal())
  const request: W.ProjectAssistantReadRequest = {command:command(key,"read","read-key"),cursor:{key,conversation_id:"chat",message_seq:2,reader_id:"arbitrary-device"}}
  await source.markRead(request,signal())
  const written = bodies.at(-1)!
  assert.equal(new Headers(written.init?.headers).get("Idempotency-Key"),"read-key")
  assert.equal(JSON.parse(String(written.init?.body)).cursor.reader_id,"trusted-reader")
})
test("GET envelopes preserve generated snake_case fields and writes preserve complete requests",() => {
  const history = assistantRelayRequest("/v1/project-assistants/bot/messages?machine_id=one&conversation_id=chat&before_seq=9&page_size=100")
  assert.equal(history.word,"project-assistants.history")
  assert.deepEqual(history.fields,{key:{machine_id:"one",bot_id:"bot"},conversation_id:"chat",before_seq:9,page_size:100})
  for (const [path,word] of [["read","read"],["observed","observed"],["memory","memory"],["memory/forget","forget"],["permissions/offer/answer","answer"]]) {
    const body = {command:command(key,"read","stable"),extra:"kept"}
    const envelope = assistantRelayRequest("/v1/project-assistants/bot/"+path,{method:"POST",body:JSON.stringify(body),headers:{"Idempotency-Key":"stable"}})
    assert.equal(envelope.word,"project-assistants."+word); assert.equal(envelope.id,"stable"); assert.deepEqual(envelope.fields,body)
  }
})
test("uncarried Cloud operations are typed unsupported before touching the connection",async () => {
  let reads = 0
  const fetch = capturedAssistantRelayFetch("one",() => {reads++;return null},[])
  const answer = await fetch("/v1/project-assistants/status")
  assert.equal(answer.status,501); assert.equal((await answer.json()).error.code,"cloud_not_carried"); assert.equal(reads,1)
})
test("account registry retains other machines and drafts through active-machine changes, then revokes",async () => {
  const registry = new ProjectAssistantRegistry("account","trusted-reader"), transport = () => async () => Response.json({})
  registry.sync([grant(),grant("two")],transport)
  const [one,two] = registry.getSnapshot()
  registry.viewState.stage("draft","keep")
  const unchanged = registry.getSnapshot()
  registry.sync([grant(),grant("two")],transport)
  assert.equal(registry.getSnapshot(),unchanged)
  assert.equal(registry.getSnapshot()[0],one);assert.equal(registry.getSnapshot()[1],two)
  registry.revoke("one")
  assert.equal(registry.getSnapshot()[0],two);assert.equal(registry.viewState.draft("draft"),"keep")
  await assert.rejects(one.get(key,signal()),e => errorCode(e) === "pa_revoked")
  registry.sync([grant(),grant("two")],transport)
  assert.notEqual(registry.getSnapshot().find(source => source.machineID === "one")?.authorizationRevision,one.authorizationRevision)
  registry.revoke();assert.equal(registry.getSnapshot().length,0)
  assert.throws(() => registry.sync([{...grant(),accountKey:"other"}],transport),error => errorCode(error) === "pa_revoked")
})

test("a source's readable status never grants content writes by itself",async () => {
  let writes=0
  const {source} = make(async (path,init) => {if(init?.method !== "GET")writes++;const answer=sourceStatus();answer.sources[0].writable=false;return Response.json(path.endsWith("/status")?answer:{})})
  await source.status(signal())
  await assert.rejects(source.send({command:command(key,"send"),text:"no"} as W.ProjectAssistantSendRequest,signal()),error => errorCode(error) === "pa_read_only")
  assert.equal(writes,0)
})

test("captured relay reads and actions never substitute the active machine and keep the write ref",async () => {
  const calls: {machine:string;word:string;body:Record<string,unknown>;ref?:string}[]=[]
  const client = {
    machineDescriptor:() => ({machine:{commands:["project-assistants.get","project-assistants.observed"]}}),
    async _machineRequest(machine:string,word:string,body:Record<string,unknown>) {calls.push({machine,word,body});return {contract_version:"project-assistants.v1"}},
    async _machineRequestAs(ref:string,machine:string,word:string,body:Record<string,unknown>) {calls.push({machine,word,body,ref});return {contract_version:"project-assistants.v1"}},
  }
  const one = capturedAssistantRelayFetch("one",() => client,["project-assistants.get","project-assistants.observed"])
  const two = capturedAssistantRelayFetch("two",() => client,["project-assistants.get","project-assistants.observed"])
  await one("/v1/project-assistants/same?machine_id=one")
  await two("/v1/project-assistants/same?machine_id=two")
  const request: W.ProjectAssistantObserveRequest={command:command(key,"observe","stable-ref"),event_seq:2,receipt_id:"receipt"}
  await one("/v1/project-assistants/same/observed",{method:"POST",body:JSON.stringify(request),headers:{"Idempotency-Key":"stable-ref"}})
  assert.deepEqual(calls.map(call => call.machine),["one","two","one"])
  assert.equal(calls.at(-1)?.ref,"stable-ref");assert.deepEqual(calls.at(-1)?.body,request)
})

test("Board reads keep source authority through an App remount and reject mismatched Projects",async () => {
  const registry = new ProjectAssistantRegistry("account","trusted-reader")
  const work: W.ProjectAssistantWorkLink={id:"link",turn_id:"turn",board_item_id:"item",task_id:"task",state:"accepted",landing_commit:"",result_message_id:"",
    fence:{key,project:{machine_id:"one",project_id:"project",canonical_id:"canonical",kind:"repository"},conversation_id:"chat",binding_id:"binding",generation:1}}
  registry.sync([grant()],() => async () => Response.json({item:{id:"item",project:{id:"project"}}}))
  const captured = await registry.captureWorkItem(work,signal())
  assert.equal(captured.machineID,"one");assert.equal(captured.item.id,"item");assert.equal(captured.current(),true)
  registry.revoke("one");assert.equal(captured.current(),false)
  registry.sync([grant()],() => async () => Response.json({item:{id:"item",project:{id:"other"}}}))
  await assert.rejects(registry.captureWorkItem(work,signal()),error => errorCode(error) === "pa_scope_denied")
})

test("fresh readable viewers acknowledge their own history without content-write rights", async () => {
  const writes: {path:string;body:Record<string,unknown>}[] = []
  const {source} = make(async (path,init) => {
    if (init?.method !== "GET") writes.push({path,body:JSON.parse(String(init?.body))})
    return Response.json(path.endsWith("/status") ? sourceStatus("history_only") : {})
  }, {...grant(),writable:false})
  await source.status(signal())
  await source.markRead({command:command(key,"read","own-read"),cursor:{key,conversation_id:"chat",message_seq:2,reader_id:"forged"}},signal())
  await source.observe({command:command(key,"observe","own-observation"),event_seq:2,receipt_id:"receipt"},signal())
  assert.deepEqual(writes.map(write => write.path),["/v1/project-assistants/same%2Fid/read","/v1/project-assistants/same%2Fid/observed"])
  assert.equal((writes[0].body.cursor as {reader_id:string}).reader_id,"trusted-reader")
  await assert.rejects(source.send({command:command(key,"send"),text:"no"} as W.ProjectAssistantSendRequest,signal()),error => errorCode(error) === "pa_read_only")
  await assert.rejects(source.edit({command:command(key,"edit")} as W.ProjectAssistantEditRequest,signal()),error => errorCode(error) === "pa_read_only")
  await assert.rejects(source.create({command:command(key,"create")} as W.ProjectAssistantCreateRequest,signal()),error => errorCode(error) === "pa_read_only")
  await assert.rejects(source.stop({command:command(key,"stop")} as W.ProjectAssistantStopRequest,signal()),error => errorCode(error) === "pa_read_only")
  await assert.rejects(source.answer({command:command(key,"answer")} as W.ProjectAssistantPermissionAnswerRequest,signal()),error => errorCode(error) === "pa_read_only")
  await assert.rejects(source.forget({command:command(key,"forget")} as W.ProjectAssistantForgetRequest,signal()),error => errorCode(error) === "pa_read_only")
  await assert.rejects(source.saveMemory({command:command(key,"memory_save")} as W.ProjectAssistantMemoryWriteRequest,signal()),error => errorCode(error) === "pa_read_only")
  assert.equal(writes.length,2)
})

test("reader acknowledgements require an observed fresh readable online source", async () => {
  for (const change of [null,{fresh:false},{state:"offline"},{readable:false}] as const) {
    let writes=0
    const {source} = make(async (path,init) => {
      if (init?.method !== "GET") writes++
      const answer=sourceStatus("history_only")
      if (change) Object.assign(answer.sources[0],change)
      return Response.json(path.endsWith("/status") ? answer : {})
    })
    if (change) await source.status(signal())
    const expected=change && "readable" in change && change.readable === false ? "pa_scope_denied" : "pa_offline"
    await assert.rejects(source.markRead({command:command(key,"read"),cursor:{key,conversation_id:"chat",message_seq:2,reader_id:"reader"}},signal()),error => errorCode(error) === expected)
    await assert.rejects(source.observe({command:command(key,"observe"),event_seq:2,receipt_id:"receipt"},signal()),error => errorCode(error) === expected)
    assert.equal(writes,0)
  }
})

test("generated PA errors survive the actual copied reply reader into the captured SourceClient",async () => {
  const client = Object.create(CloudClient.prototype)
  client.machineDescriptor = () => ({machine:{commands:Object.keys(CARRIED)}})
  client.readWaiters = new Map()
  client._emit = () => {}
  let settled: unknown, rejected: unknown, calls = 0
  client._settleRead = (_key:string,body:unknown,error:unknown) => {settled=body;rejected=error}
  const refusal: W.ProjectAssistantErrorResponse = {contract_version:"project-assistants.v1",error:{code:"pa_capacity_full",field:"pending_turns",retry_after_seconds:7}}
  client._read = async (identity:{machine:string;session:string},word:string,_fields:Record<string,unknown>,read:string) => {
    calls++
    client._applySnapshot({kind:"transcript",...identity},{read,status:word.endsWith(".status")?200:429,body:word.endsWith(".status")?sourceStatus():refusal},{seq:calls,ts:Date.now()},false)
    if (rejected) throw rejected
    return settled
  }
  const source = new ProjectAssistantSourceClient(grant(),capturedAssistantRelayFetch("one",() => client,Object.keys(CARRIED)),() => grant())
  await source.status(signal())
  for (const code of ["pa_stale_generation","pa_capacity_full","pa_outcome_unknown"] as const) {
    refusal.error.code=code
    const before=calls
    await assert.rejects(source.send({command:command({machine_id:"one",bot_id:"bot"},"send","stable-receipt"),text:"hello",quote_message_id:""} as W.ProjectAssistantSendRequest,signal()),error => {assert.deepEqual(error,refusal);return true})
    assert.equal(calls,before+1,"an unknown or capacity result is never automatically resent")
  }
})

test("every Bot word is carried and read-only metadata retains action refs",async () => {
  const words=Object.keys(CARRIED).filter(word => word.startsWith("project-assistants."))
  assert.deepEqual(words.sort(),["answer","create","edit","events","export","forget","get","history","list","memory","observed","read","send","status","stop","projects","view"].map(word => "project-assistants."+word).sort())
  const captured: {ref:string;word:string;fields:Record<string,unknown>}[]=[]
  const client={machineDescriptor:()=>({machine:{commands:words}}),async _machineRequestAs(ref:string,_machine:string,word:string,fields:Record<string,unknown>){captured.push({ref,word,fields});return {}}}
  const relay=capturedAssistantRelayFetch("one",() => client,words)
  for (const word of ["read","observed"]) {
    const fields={command:command({machine_id:"one",bot_id:"bot"},word==="read"?"read":"observe","stable-"+word),cursor:{reader_id:"untrusted-hint"}}
    await relay("/v1/project-assistants/bot/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Idempotency-Key":"stable-"+word}})
    assert.equal(captured.at(-1)?.ref,"stable-"+word)
    assert.deepEqual(captured.at(-1)?.fields,fields)
  }
})

test("view checks API59 and every captured authority again after delayed JSON decoding",async()=>{
  for(const change of ["abort","account","reader","machine","revision","api"]){
    let release!:(answer:unknown)=>void,started!:()=>void
    const body=new Promise(resolve=>{release=resolve}),reading=new Promise<void>(resolve=>{started=resolve})
    const response=new Response("{}");response.json=()=>{started();return body}
    const first={...grant(),apiLevel:61},{source,setGrant}=make(async()=>response,first),control=new AbortController()
    const waiting=source.view(viewRequest,control.signal)
    await reading
    if(change==="abort")control.abort()
    else setGrant({...first,...change==="api"?{apiLevel:60}:change==="account"?{accountKey:"other"}:change==="reader"?{readerID:"other"}:change==="machine"?{machineID:"two"}:{authorizationRevision:"grant-2"}})
    release({})
    await assert.rejects(waiting,e=>change==="abort"?(e as Error).name==="AbortError":errorCode(e)===(change==="api"?"pa_unsupported":"pa_revoked"))
  }
})
test("view Cloud routing keeps empty strings and zeroes and rejects extra or duplicate query fields",()=>{
  const path="/v1/project-assistants/same%2Fid/view?machine_id=one&conversation_id=chat&mode=snapshot&source_epoch=&cursor=&after_seq=0&page_size=100"
  assert.deepEqual(assistantRelayRequest(path).fields,viewRequest)
  for(const invalid of [path+"&cursor=x",path+"&reader_id=forged",path.replace("after_seq=0","after_seq=1e2"),path.replace("source_epoch=", "epoch="),path.replace("page_size=100","page_size=257"),path.replace("mode=snapshot","mode=export")])assert.throws(()=>assistantRelayRequest(invalid),e=>errorCode(e)==="pa_invalid")
  const history={...viewRequest,mode:"history",source_epoch:"epoch",cursor:"signed +<&opaque"}
  const query=new URLSearchParams({machine_id:"one",conversation_id:"chat",mode:"history",source_epoch:history.source_epoch,cursor:history.cursor,after_seq:"0",page_size:"100"})
  assert.deepEqual(assistantRelayRequest("/v1/project-assistants/same%2Fid/view?"+query).fields,history)
})
test("captured view transport sends the generated request once and preserves typed refusals",async()=>{
  let calls=0
  const refusal:W.ProjectAssistantErrorResponse={contract_version:"project-assistants.v1",error:{code:"pa_capacity_full",field:"snapshot",retry_after_seconds:7}}
  const connection={machineDescriptor:()=>({machine:{commands:["project-assistants.view"]}}),async _machineRequest(machine:string,word:string,fields:Record<string,unknown>){calls++;assert.equal(machine,"one");assert.equal(word,"project-assistants.view");assert.deepEqual(fields,viewRequest);return refusal}}
  const first={...grant(),apiLevel:61},source=new ProjectAssistantSourceClient(first,capturedAssistantRelayFetch("one",()=>connection,["project-assistants.view"]),()=>first)
  await assert.rejects(source.view(viewRequest,signal()),e=>{assert.deepEqual(e,refusal);return true})
  assert.equal(calls,1)
})

const projectsRequest: W.ProjectAssistantProjectsRequest={machine_id:"one",cursor:"opaque +<&page",page_size:100}
const catalog = (): W.ProjectAssistantProjectsResponse => ({contract_version:"project-assistants.v1",source:sourceStatus().sources[0],agents:["claude","codex"],revision:"registered-1",items:[{target:{machine_id:"one",project_id:"project-a"},label:"Same name"},{target:{machine_id:"one",project_id:"project-b"},label:"Same name"}],has_more:true,next_cursor:"next-page"})
test("registered Projects use API58, captured query and configuration agents without native readiness",async()=>{
  const paths:string[]=[]
  const {source}=make(async path=>{paths.push(path);return Response.json(catalog())})
  const response=await source.projects(projectsRequest,signal())
  assert.deepEqual(response.agents,["claude","codex"])
  assert.deepEqual(response.source.capabilities,[])
  assert.deepEqual(response.items.map(item=>item.target.project_id),["project-a","project-b"])
  assert.equal(paths.length,1)
  const request=assistantRelayRequest(paths[0])
  assert.equal(new URL(paths[0],"http://fixture").pathname,"/v1/project-assistant-targets")
  assert.equal(request.word,"project-assistants.projects");assert.deepEqual(request.fields,projectsRequest)
  await assert.rejects(source.projects({...projectsRequest,machine_id:"two"},signal()),e=>errorCode(e)==="pa_scope_denied")
  assert.equal(paths.length,1)
  for(const apiLevel of [undefined,56,57]){
    const old=make(async()=>{assert.fail("legacy catalog fallback")},{...grant(),apiLevel}).source
    assert.equal((await old.status(signal())).sources[0].state,"unsupported")
    await assert.rejects(old.projects(projectsRequest,signal()),e=>errorCode(e)==="pa_unsupported")
  }
})
test("Project catalogs accept fresh read-only metadata and require fresh interactive status for create",async()=>{
  let writes=0
  const response=catalog();response.source={...response.source,access:"history_only",writable:false}
  const {source}=make(async (_path,init)=>{if(init?.method!=="GET")writes++;return Response.json(response)},{...grant(),writable:false})
  assert.deepEqual((await source.projects(projectsRequest,signal())).agents,["claude","codex"])
  await assert.rejects(source.create({command:command(key,"create")} as W.ProjectAssistantCreateRequest,signal()),e=>errorCode(e)==="pa_read_only")
  assert.equal(writes,0)
})
test("foreign, duplicate, invalid and stale Project pages never publish options",async()=>{
  const changes:[(answer:W.ProjectAssistantProjectsResponse)=>void,string][]=[
    [a=>{a.contract_version="other"},"pa_invalid"],
    [a=>{a.source.machine_id="two"},"pa_scope_denied"],
    [a=>{a.items[0].target.machine_id="two"},"pa_scope_denied"],
    [a=>{a.items.push(a.items[0])},"pa_invalid"],
    [a=>{a.agents.push("codex")},"pa_invalid"],
    [a=>{a.source.fresh=false},"pa_offline"],
    [a=>{a.source.readable=false},"pa_scope_denied"],
    [a=>{a.source.state="revoked"},"pa_revoked"],
    [a=>{a.next_cursor=projectsRequest.cursor},"pa_invalid"],
    [a=>{a.has_more=false},"pa_invalid"],
    [a=>{a.revision=""},"pa_invalid"],
  ]
  for(const [change,code]of changes){const response=catalog();change(response);const {source}=make(async()=>Response.json(response));await assert.rejects(source.projects(projectsRequest,signal()),e=>errorCode(e)===code)}
  for(const path of ["?machine_id=one&page_size=100","?machine_id=one&cursor=&page_size=100&principal=forged","?machine_id=one&cursor=&page_size=100&cursor=x","?machine_id=one&cursor=&page_size=1e2"]){assert.throws(()=>assistantRelayRequest("/v1/project-assistant-targets"+path),e=>errorCode(e)==="pa_invalid")}
})
test("late Project pages reject abort, revoke and reader replacement after JSON decoding",async()=>{
  for(const state of ["abort","revoke","reader"]){
    let release!:(answer:unknown)=>void,started!:()=>void
    const body=new Promise(resolve=>{release=resolve}),reading=new Promise<void>(resolve=>{started=resolve})
    const response=new Response("{}");response.json=()=>{started();return body}
    const {source,setGrant}=make(async()=>response),control=new AbortController()
    const waiting=source.projects(projectsRequest,control.signal)
    await reading
    if(state==="abort")control.abort();else if(state==="revoke")setGrant(null);else setGrant({...grant(),readerID:"other-reader"})
    release(catalog())
    await assert.rejects(waiting,e=>state==="abort"?(e as Error).name==="AbortError":errorCode(e)==="pa_revoked")
  }
})
test("captured Project cursor, revocation and budget refusals retain their entire envelope without retry",async()=>{
  for(const code of ["pa_cursor_expired","pa_capacity_full","pa_revoked","pa_context_stale"] as const){
    let calls=0
    const refusal:W.ProjectAssistantErrorResponse={contract_version:"project-assistants.v1",error:{code,field:"catalog",retry_after_seconds:7}}
    const connection={machineDescriptor:()=>({machine:{commands:["project-assistants.projects"]}}),async _machineRequest(machine:string,word:string,fields:Record<string,unknown>){calls++;assert.equal(machine,"one");assert.equal(word,"project-assistants.projects");assert.deepEqual(fields,projectsRequest);return refusal}}
    const source=new ProjectAssistantSourceClient(grant(),capturedAssistantRelayFetch("one",()=>connection,["project-assistants.projects"]),()=>grant())
    await assert.rejects(source.projects(projectsRequest,signal()),e=>{assert.deepEqual(e,refusal);return true})
    assert.equal(calls,1)
  }
})


test("captured bounded reads preserve all six complete query envelopes", async () => {
  const key={machine_id:"one",bot_id:"same/id"}
  const requests:W.ProjectAssistantReadViewRequest[]=[
    {key,conversation_id:"chat",mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100},
    {key,conversation_id:"chat",mode:"history",source_epoch:"epoch",cursor:"signed +<&opaque",after_seq:0,page_size:100},
    {key,conversation_id:"chat",mode:"events",source_epoch:"epoch",cursor:"",after_seq:602,page_size:256},
    {key,conversation_id:"chat",mode:"permissions",source_epoch:"epoch",cursor:"",after_seq:0,page_size:100},
    {key,conversation_id:"chat",mode:"traces",source_epoch:"epoch",cursor:"signed trace",after_seq:0,page_size:100},
    {key,conversation_id:"chat",mode:"detail",source_epoch:"epoch",cursor:"signed detail",after_seq:0,page_size:1},
  ]
  const captured:Record<string,unknown>[]=[]
  const connection={machineDescriptor:()=>({machine:{commands:["project-assistants.view"]}}),async _machineRequest(machine:string,word:string,fields:Record<string,unknown>){assert.equal(machine,"one");assert.equal(word,"project-assistants.view");captured.push(fields);return {contract_version:"project-assistants.v1"}}}
  const relay=capturedAssistantRelayFetch("one",()=>connection,["project-assistants.view"])
  for(const request of requests){
    const {key,...fields}=request
    const query=new URLSearchParams(Object.entries({machine_id:key.machine_id,...fields}).map(([k,v])=>[k,String(v)]))
    await relay("/v1/project-assistants/same%2Fid/view?"+query,{method:"GET"})
    assert.deepEqual(captured.at(-1),request)
  }
  assert.equal(captured.length,6)
})

test("bounded relay reads refuse malformed queries and bodies before transport", async () => {
  let calls=0
  const connection={machineDescriptor:()=>({machine:{commands:["project-assistants.view"]}}),async _machineRequest(){calls++;return {}}}
  const relay=capturedAssistantRelayFetch("one",()=>connection,["project-assistants.view"])
  const path="/v1/project-assistants/bot/view?machine_id=one&conversation_id=chat&mode=snapshot&source_epoch=&cursor=&after_seq=0&page_size=100"
  const invalid=[path+"&cursor=duplicate",path+"&reader_id=forged",path.replace("source_epoch=", "epoch="),path.replace("after_seq=0","after_seq=1e2"),path.replace("after_seq=0","after_seq=9007199254740992"),path.replace("page_size=100","page_size=257"),path.replace("mode=snapshot","mode=export")]
  for(const query of invalid)await assert.rejects(relay(query),e=>errorCode(e)==="pa_invalid")
  await assert.rejects(relay(path,{method:"GET",body:"{}"}),e=>errorCode(e)==="pa_invalid")
  assert.equal(calls,0)
})
