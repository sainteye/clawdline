import { test } from "node:test"
import assert from "node:assert/strict"
import http from "node:http"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import { ProjectAssistantSourceClient, ProjectAssistantRegistry, assistantRelayRequest, cloudAssistantGrants, capturedAssistantRelayFetch, type AssistantGrant, type AssistantFetch } from "./project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import { command, errorCode } from "../project-assistants/client.ts"
// @ts-expect-error -- Node executes source extensions.
import * as clientModule from "../project-assistants/client.ts"
// @ts-expect-error -- Node executes source extensions.
import {validLoginIntent} from "../project-assistants/login-profile-intent.ts"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
// @ts-expect-error -- Node executes source extensions.
import { CARRIED } from "./carry-generated.ts"
// @ts-expect-error -- Node executes source extensions.
import { RosterStreamFailure } from "../project-assistants/roster-stream.ts"

test("local roster crosses a real Node HTTP SSE connection and cancels cleanly", async () => {
  let closed!: () => void
  const disconnected = new Promise<void>(resolve => { closed = resolve })
  const seen: string[] = []
  const snapshot = rosterFrame()
  snapshot.source_epoch = "synthetic-epoch"
  snapshot.changes![0].summary!.read_cursor.reader_id = "reader:synthetic-owner-identity"
  const server = http.createServer((request, response) => {
    seen.push(request.url ?? "")
    assert.equal(request.headers.accept, "text/event-stream")
    response.writeHead(200, { "Content-Type": "text/event-stream" })
    response.write('event: hello\ndata: {"type":"hello","source_epoch":"synthetic-epoch"}\n\n')
    response.write(rosterWire(snapshot))
    response.on("close", closed)
  })
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve))
  try {
    const address = server.address()
    assert.ok(address && typeof address === "object")
    const local: AssistantGrant = { accountKey:"local",readerID:"local",machineID:"one",authorizationRevision:"synthetic-grant",apiLevel:71,paired:true,online:true,writable:false }
    const source = new ProjectAssistantSourceClient(local, (path, init) => fetch(`http://127.0.0.1:${address.port}`+path, init), () => local)
    const control = new AbortController(), frames: W.ProjectAssistantRosterFrame[] = []
    assert.equal(source.canRoster(), true)
    await assert.rejects(source.subscribeRoster({machine_id:local.machineID}, frame => { frames.push(frame); if(frame.type==="snapshot-complete")control.abort() }, control.signal), e => (e as Error).name === "AbortError")
    await disconnected
    assert.deepEqual(frames, [{type:"hello",source_epoch:"synthetic-epoch",changes:[]},snapshot])
    assert.deepEqual(seen, ["/v1/project-assistants/roster-events?machine_id=one"])
    console.log("REAL_NODE_ROSTER_SSE_RECEIPT "+JSON.stringify({requests:seen.length,frames:frames.length,readOnly:true,callerAbort:true,serverObservedClose:true,providerRequests:0}))
  } finally {
    server.closeAllConnections()
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()))
  }
})

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

test("explicit output reads keep their captured scope and distinct Cloud word behind API69",async()=>{
  const paths:string[]=[],request:W.ProjectAssistantOutputRequest={key,conversation_id:"chat",message_id:"card",id:"file",artifact_id:"",offset:393216}
  const source=make(async path=>{paths.push(path);return Response.json({})},{...grant(),apiLevel:69}).source
  assert.equal(source.canOutputs(),true)
  await source.output(request,signal())
  const routed=assistantRelayRequest(paths[0])
  assert.equal(routed.word,"project-assistants.output");assert.equal(routed.kind,"read")
  assert.deepEqual(routed.fields,{machine_id:key.machine_id,bot_id:key.bot_id,conversation_id:"chat",message_id:"card",id:"file",artifact_id:"",offset:393216})
  assert.equal("project-assistants.output" in CARRIED,true)
  let calls=0;const old=make(async()=>{calls++;return Response.json({})},{...grant(),apiLevel:68}).source
  assert.equal(old.canOutputs(),false);await assert.rejects(old.output(request,signal()),e=>errorCode(e)==="pa_unsupported");assert.equal(calls,0)
  for(const offset of ["-1","1.5","104857601"]){const url=new URL(paths[0],"http://source.invalid");url.searchParams.set("offset",offset);assert.throws(()=>assistantRelayRequest(url.pathname+url.search),e=>errorCode(e)==="pa_invalid")}
})
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
test("exact group quotes require API88 without disabling plain groups on API76",async()=>{
  const sent: {path:string;body:unknown}[]=[]
  const transport:AssistantFetch=async(path,init)=>{
    if(init?.method!=="GET")sent.push({path,body:JSON.parse(String(init?.body))})
    return Response.json(path.endsWith("/status")?sourceStatus():{})
  }
  const plain:W.ProjectAssistantGroupSendRequest={command:command(key,"send","stable-group-send"),text:"Synthetic reply"}
  const quoted:W.ProjectAssistantGroupSendRequest={...plain,quote:{seq:3}}
  const old=make(transport,{...grant(),apiLevel:87}).source
  await old.status(signal())
  assert.equal(old.canGroupContext(),false)
  await assert.rejects(old.sendGroup(quoted,signal()),e=>errorCode(e)==="pa_unsupported")
  assert.equal(sent.length,0,"unsupported quotes must be refused before transport")
  const fresh=make(transport,{...grant(),apiLevel:88}).source
  await fresh.status(signal());assert.equal(fresh.canGroupContext(),true)
  await fresh.sendGroup(quoted,signal())
  assert.deepEqual(sent[0].body,quoted)
  const carried=assistantRelayRequest(sent[0].path,{method:"POST",body:JSON.stringify(quoted),headers:{"Idempotency-Key":quoted.command.idempotency_key}})
  assert.equal(carried.word,"project-assistants.group-send")
  assert.deepEqual(carried.fields,quoted)
  for(const seq of [0,-1,1.5,Number.MAX_SAFE_INTEGER+1]){
    await assert.rejects(fresh.sendGroup({...quoted,quote:{seq}},signal()),e=>errorCode(e)==="pa_invalid")
  }
  assert.equal(sent.length,1)
  const legacy=make(transport,{...grant(),apiLevel:76}).source
  await legacy.status(signal());await legacy.sendGroup(plain,signal())
  assert.deepEqual(sent[1].body,plain)
})
test("group reactions begin at API90 with current write authority",async()=>{
  const sent:{path:string;init?:RequestInit}[]=[]
  const transport:AssistantFetch=async(path,init)=>{
    if(init?.method!=="GET")sent.push({path,init})
    return Response.json(path.endsWith("/status")?sourceStatus():{})
  }
  const request:W.ProjectAssistantGroupReactRequest={
    command:command(key,"react","stable-room-react"),conversation_id:"room-chat",
    group_version:1,message_seq:3,emoji:"👍",present:true,expected_revision:0,
  }
  const old=make(transport,{...grant(),apiLevel:89}).source
  await old.status(signal())
  assert.equal(old.canGroupContext(),true)
  assert.equal(old.canGroupReactions(),false)
  await assert.rejects(old.groupReact(request,signal()),e=>errorCode(e)==="pa_unsupported")
  const read=make(transport,{...grant(),apiLevel:90,writable:false}).source
  await read.status(signal())
  assert.equal(read.canGroupReactions(),false)
  await assert.rejects(read.groupReact(request,signal()),e=>errorCode(e)==="pa_read_only")
  assert.equal(sent.length,0)

  const current=make(transport,{...grant(),apiLevel:90})
  await current.source.status(signal())
  assert.equal(current.source.canGroupReactions(),true)
  await current.source.groupReact(request,signal())
  assert.equal(sent.length,1)
  const carried=assistantRelayRequest(sent[0].path,sent[0].init)
  assert.equal(carried.word,"project-assistants.group-react")
  assert.equal(carried.id,request.command.idempotency_key)
  assert.deepEqual(carried.fields,request)
  current.setGrant({...grant(),apiLevel:90,authorizationRevision:"revoked"})
  assert.equal(current.source.canGroupReactions(),false)
  await assert.rejects(current.source.groupReact(request,signal()),e=>errorCode(e)==="pa_revoked")
  assert.equal(sent.length,1)
})
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

test("Bot controls follow the live API level after the source first arrives", () => {
  const registry = new ProjectAssistantRegistry("account","trusted-reader")
  const transport = () => async () => Response.json(sourceStatus())
  const controls = () => {
    const source = registry.getSnapshot()[0] as ProjectAssistantSourceClient
    return [source.pictures(), source.canUnqueue(), source.canManage(), source.canRules(), source.canSendNow()]
  }
  registry.sync([{...grant(),apiLevel:undefined}],transport)
  assert.deepEqual(controls(),[false,false,false,false,false])
  registry.sync([{...grant(),apiLevel:68}],transport)
  assert.deepEqual(controls(),[true,true,true,true,true])
  registry.sync([{...grant(),apiLevel:61}],transport)
  assert.deepEqual(controls(),[false,false,false,false,false])
  registry.revoke()
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
  assert.deepEqual(words.sort(),["react","network-settings","network-settings-save","connector-credential","connector-secret","memory-view","configuration","configuration-profile","extensions","login-profiles","login-profile","login-secret","rename-artifact","groups","group-history","group-create","group-edit","group-delete","group-react","group-send","group-stop","team-plans","team-answer","team-settings","routines","routine-runs","routine-save","routine-enabled","routine-delete","routine-test","routine-hook","input","upload","send-files","answer","attachment","send-pictures","create","delete","edit","events","export","forget","get","history","list","memory","observed","output","read","rule-remove","rules","send","send-now","status","stop","projects","view","visibility"].map(word => "project-assistants."+word).sort())
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

test("a Bot draft is kept in this browser's storage",()=>{
  const saved=new Map<string,string>()
  const storage={getItem:(k:string)=>saved.get(k)??null,setItem:(k:string,v:string)=>{saved.set(k,v)},removeItem:(k:string)=>{saved.delete(k)}}
  const g=globalThis as {window?:unknown}, before=g.window
  g.window={localStorage:storage}
  try{
    const view=new ProjectAssistantRegistry("account","trusted-reader").viewState
    view.stage("draft-key","unsent words")
    assert.equal(view.flush("draft-key"),true)
    assert.equal(saved.get("draft-key"),"unsent words")
    assert.equal(new ProjectAssistantRegistry("account","trusted-reader").viewState.draft("draft-key"),"unsent words")
  }finally{g.window=before}
})

// Every paired device may act (2026-11-14): a machine whose operation flag has
// not arrived yet is not read-only; only one that turned Cloud operation off is.
test("a paired phone may change Bots unless the machine turned Cloud operation off",()=>{
  const machine = (operationAllowed: boolean | null) => ({ id: "m", pairing: "paired", freshness: "current", operationAllowed }) as never
  const grant = (operationAllowed: boolean | null) => cloudAssistantGrants("acct", [machine(operationAllowed)], () => 63, () => "r")[0]
  assert.equal(grant(null).writable, true)
  assert.equal(grant(true).writable, true)
  assert.equal(grant(false).writable, false)
})

test("Cloud pictures require the carried API tier and keep picture-only sends on their own word",async()=>{
  const calls:unknown[]=[]
  const source=make(async(path,init)=>{calls.push({path,init});return Response.json(sourceStatus())},{...grant(),apiLevel:68}).source
  assert.equal(source.pictures(),true)
  assert.equal(make(async()=>Response.json({}),{...grant(),apiLevel:67}).source.pictures(),false)
  const request={command:command(key,"send","picture-only"),fence:viewWire().head.current_fence,text:"",quote_message_id:"",images:["data:image/png;base64,cG5n"]}
  const relay=assistantRelayRequest("/v1/project-assistants/same%2Fid/messages",{method:"POST",body:JSON.stringify(request),headers:{"Idempotency-Key":"picture-only"}})
  assert.equal(relay.word,"project-assistants.send-pictures")
  assert.deepEqual(relay.fields,request)
  const read=assistantRelayRequest("/v1/project-assistants/same%2Fid/attachments/"+"a".repeat(64)+"?machine_id=one")
  assert.equal(read.word,"project-assistants.attachment")
  assert.deepEqual(read.fields,{machine_id:"one",bot_id:"same/id",id:"a".repeat(64)})
  assert.equal(calls.length,0)
})

test("Cloud attachment reads validate the bytes and recheck revocation after hashing",async()=>{
  const data=Buffer.from("picture"),id=Buffer.from(await crypto.subtle.digest("SHA-256",data)).toString("hex")
  const good={contract_version:"project-assistants.v1",id,media_type:"image/png",byte_count:data.length,data:data.toString("base64")}
  const source=make(async()=>Response.json(good),{...grant(),apiLevel:68,writable:false}).source
  const blob=await source.attachment(key,id,signal());assert.equal(blob.type,"image/png");assert.deepEqual(Buffer.from(await blob.arrayBuffer()),data)
  for(const changed of [{id:"b".repeat(64)},{media_type:"image/svg+xml"},{byte_count:data.length+1},{data:"not base64"},{data:Buffer.from("changed").toString("base64"),byte_count:7},{contract_version:"wrong"}]){
    const broken=make(async()=>Response.json({...good,...changed}),{...grant(),apiLevel:68}).source
    await assert.rejects(broken.attachment(key,id,signal()),e=>errorCode(e)==="pa_invalid")
  }
  let release!:(value:Response)=>void
  const next=make(()=>new Promise(resolve=>{release=resolve}),{...grant(),apiLevel:68})
  const request=next.source.attachment(key,id,signal())
  next.setGrant(null);release(Response.json(good))
  await assert.rejects(request,e=>errorCode(e)==="pa_revoked")
  const cancelled=new AbortController();cancelled.abort()
  await assert.rejects(source.attachment(key,id,cancelled.signal),e=>(e as Error).name==="AbortError")
  await assert.rejects(source.attachment({...key,machine_id:"other"},id,signal()),e=>errorCode(e)==="pa_scope_denied")
})

test("Cloud refuses oversized picture commands before transport, retaining typed source errors",async()=>{
  let writes=0
  const relay=capturedAssistantRelayFetch("one",()=>({machineDescriptor:()=>({machine:{commands:Object.keys(CARRIED)}}),_machineRequestAs:async()=>{writes++;return {}}}),Object.keys(CARRIED))
  const request={command:command(key,"send","oversize"),fence:viewWire().head.current_fence,text:"",quote_message_id:"",images:["x".repeat(16*1024*1024)]}
  await assert.rejects(relay("/v1/project-assistants/same%2Fid/messages",{method:"POST",body:JSON.stringify(request),headers:{"Idempotency-Key":"oversize"}}),e=>errorCode(e)==="pa_capacity_full")
  assert.equal(writes,0)
  const refusal={contract_version:"project-assistants.v1",error:{code:"pa_not_found",field:"attachment",retry_after_seconds:0}}
  const source=make(async()=>Response.json(refusal,{status:404}),{...grant(),apiLevel:68}).source
  await assert.rejects(source.attachment(key,"a".repeat(64),signal()),e=>{assert.deepEqual(e,refusal);return true})
})

const rosterGrant = (): AssistantGrant => ({...grant(),accountKey:"local",apiLevel:71,writable:false})
const rosterRequest: W.ProjectAssistantRosterRequest = {machine_id:"one",project_id:"project",cursor:"signed + opaque"}
const rosterFrame = (): W.ProjectAssistantRosterFrame => ({type:"snapshot-complete",source_epoch:"epoch",cursor:"next",changes:[{key,revision:0,removed:false,summary:{bot:viewWire().snapshot!.bot,source:{...sourceStatus().sources[0],access:"history_only",writable:false},read_cursor:{key,conversation_id:"chat",reader_id:"trusted-reader",message_seq:0},activity:"",activity_at:"0001-01-01T00:00:00Z",latest_message:"",pending_permissions:0,unread_count:4,version:1,status:"idle"}}]})
const rosterWire = (f: W.ProjectAssistantRosterFrame) => `${f.cursor?`id: ${f.cursor}\n`:""}event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`
const rosterResponse = (input: string, cancel: ()=>void = ()=>{}) => new Response(new ReadableStream<Uint8Array>({start(c){c.enqueue(new TextEncoder().encode(input))},cancel}),{headers:{"Content-Type":"text/event-stream; charset=utf-8"}})

test("roster is local API71 only and cannot enter the Cloud recorder",async()=>{
  let calls=0
  for(const first of [grant(),{...grant(),apiLevel:71},{...rosterGrant(),apiLevel:70},{...rosterGrant(),apiLevel:undefined}]) {
    const {source}=make(async()=>{calls++;return rosterResponse("")},first)
    assert.equal(source.canRoster(),false)
    await assert.rejects(source.subscribeRoster(rosterRequest,()=>assert.fail("unsupported callback"),signal()),e=>errorCode(e)==="pa_unsupported")
  }
  for(const suffix of ["", "?machine_id=one"]) assert.throws(()=>assistantRelayRequest("/v1/project-assistants/roster-events"+suffix),e=>errorCode(e)==="pa_unsupported")
  const relay=capturedAssistantRelayFetch("one",()=>({async _machineRequest(){calls++;return {}}}),["project-assistants.get"])
  await assert.rejects(relay("/v1/project-assistants/roster-events?machine_id=one"),e=>errorCode(e)==="pa_unsupported")
  assert.equal(calls,0)
})

test("read-only roster keeps its entire query and rechecks grant before each callback",async()=>{
  let calls=0,cancelled=0
  const first=rosterFrame(),second={...rosterFrame(),cursor:"second"}
  const {source,setGrant}=make(async(path,init)=>{
    calls++;const url=new URL(path,"http://synthetic.invalid")
    assert.deepEqual(Object.fromEntries(url.searchParams),rosterRequest)
    assert.equal(init?.cache,"no-store")
    assert.equal(new Headers(init?.headers).has("Last-Event-ID"),false)
    return rosterResponse(rosterWire(first)+rosterWire(second),()=>{cancelled++})
  },rosterGrant())
  const seen:W.ProjectAssistantRosterFrame[]=[]
  await assert.rejects(source.subscribeRoster(rosterRequest,f=>{seen.push(f);setGrant({...rosterGrant(),authorizationRevision:"replaced"})},signal()),e=>errorCode(e)==="pa_revoked")
  assert.equal(calls,1);assert.equal(cancelled,1);assert.deepEqual(seen,[first])
})

test("roster accepts source-issued read identities independently of the host logical reader",async()=>{
  const first={...rosterGrant(),readerID:"local"},value=rosterFrame()
  value.changes![0].summary!.read_cursor.reader_id="reader:synthetic-owner-identity"
  const {source}=make(async()=>rosterResponse(rosterWire(value)),first),control=new AbortController()
  const seen:W.ProjectAssistantRosterFrame[]=[]
  await assert.rejects(source.subscribeRoster(rosterRequest,f=>{seen.push(f);control.abort()},control.signal),e=>(e as Error).name==="AbortError")
  assert.deepEqual(seen,[value])
})

test("roster rejects malformed later rows before any publication",async()=>{
  for(const change of [(f:W.ProjectAssistantRosterFrame)=>{f.changes!.push({...f.changes![0],key:{...key,machine_id:"foreign"}})}]){
    const value=rosterFrame();change(value)
    let callbacks=0,cancelled=0
    const {source}=make(async()=>rosterResponse(rosterWire(value),()=>{cancelled++}),rosterGrant())
    await assert.rejects(source.subscribeRoster(rosterRequest,()=>{callbacks++},signal()),e=>errorCode(e)==="pa_scope_denied")
    assert.equal(callbacks,0);assert.equal(cancelled,1)
  }
})

test("roster grant replacement while a read is pending cannot publish its late bytes",async()=>{
  for(const state of ["account","reader","machine","revision","revoke","abort"] as const) {
    let controller!: ReadableStreamDefaultController<Uint8Array>,started!:()=>void,cancelled=0
    const reading=new Promise<void>(resolve=>{started=resolve})
    const body=new ReadableStream<Uint8Array>({start(c){controller=c},pull(){started()},cancel(){cancelled++}})
    const {source,setGrant}=make(async()=>new Response(body,{headers:{"Content-Type":"text/event-stream"}}),rosterGrant())
    const control=new AbortController();let callbacks=0
    const pending=source.subscribeRoster(rosterRequest,()=>{callbacks++},control.signal)
    await reading;await Promise.resolve()
    if(state==="revoke")source.revoke()
    else if(state==="abort")control.abort()
    else setGrant({...rosterGrant(),...(state==="account"?{accountKey:"next"}:state==="reader"?{readerID:"next"}:state==="machine"?{machineID:"next"}:{authorizationRevision:"next"})})
    if(state!=="revoke"&&state!=="abort")controller.enqueue(new TextEncoder().encode(rosterWire(rosterFrame())))
    await assert.rejects(pending,e=>state==="abort"?(e as Error).name==="AbortError":errorCode(e)==="pa_revoked")
    assert.equal(callbacks,0);assert.equal(cancelled,1)
  }
})

test("roster authority is rechecked after JSON parsing and HTTP refusal decoding",async()=>{
  const before=JSON.parse
  const {source,setGrant}=make(async()=>rosterResponse(rosterWire(rosterFrame())),rosterGrant())
  let callbacks=0
  JSON.parse=(text: string,reviver?: Parameters<typeof JSON.parse>[1])=>{const value=before(text,reviver);setGrant(null);return value}
  try {await assert.rejects(source.subscribeRoster(rosterRequest,()=>{callbacks++},signal()),e=>errorCode(e)==="pa_revoked")} finally {JSON.parse=before}
  assert.equal(callbacks,0)
  let release!:(v:unknown)=>void,started!:()=>void
  const reading=new Promise<void>(resolve=>{started=resolve}),body=new Promise(resolve=>{release=resolve})
  const response=new Response("{}",{status:403});response.json=()=>{started();return body}
  const late=make(async()=>response,rosterGrant()),control=new AbortController()
  const pending=late.source.subscribeRoster(rosterRequest,()=>assert.fail("late HTTP callback"),control.signal)
  await reading;late.setGrant(null);release({contract_version:"project-assistants.v1",error:{code:"pa_scope_denied",field:"roster",retry_after_seconds:0}})
  await assert.rejects(pending,e=>errorCode(e)==="pa_revoked")
})

test("roster failures preserve source refusals, terminal frames and unexpected EOF without retry",async()=>{
  const refusal:W.ProjectAssistantErrorResponse={contract_version:"project-assistants.v1",error:{code:"pa_capacity_full",field:"roster",retry_after_seconds:7}}
  const {source}=make(async()=>Response.json(refusal,{status:429}),rosterGrant())
  await assert.rejects(source.subscribeRoster(rosterRequest,()=>assert.fail("HTTP callback"),signal()),e=>{assert.deepEqual(e,refusal);return true})
  for(const type of ["resync","closure"] as const) {
    let calls=0,cancelled=0;const value:W.ProjectAssistantRosterFrame={type,failure:refusal.error},seen:W.ProjectAssistantRosterFrame[]=[]
    const {source}=make(async()=>{calls++;return rosterResponse(rosterWire(value),()=>{cancelled++})},rosterGrant())
    await assert.rejects(source.subscribeRoster(rosterRequest,f=>seen.push(f),signal()),e=>e instanceof RosterStreamFailure&&e.kind===type&&e.error.code===refusal.error.code)
    assert.deepEqual(seen,[{...value,changes:[]}]);assert.equal(calls,1);assert.equal(cancelled,1)
  }
  const eof=make(async()=>new Response(rosterWire(rosterFrame()),{headers:{"Content-Type":"text/event-stream"}}),rosterGrant()).source
  const seen:unknown[]=[]
  await assert.rejects(eof.subscribeRoster(rosterRequest,f=>seen.push(f),signal()),e=>e instanceof RosterStreamFailure&&e.kind==="eof")
  assert.equal(seen.length,1)
})

test("roster never upgrades a read-only grant and releases ignored-abort late HTTP bodies",async()=>{
  const value=rosterFrame();value.changes![0].summary!.source={...sourceStatus().sources[0]}
  const control=new AbortController(),seen:W.ProjectAssistantRosterFrame[]=[]
  const {source}=make(async()=>rosterResponse(rosterWire(value)),rosterGrant())
  await assert.rejects(source.subscribeRoster(rosterRequest,f=>{seen.push(f);control.abort()},control.signal),e=>(e as Error).name==="AbortError")
  assert.equal(seen[0].changes![0].summary!.source.writable,false)
  let release!:(response:Response)=>void,cancelled=0
  const late=make(()=>new Promise(resolve=>{release=resolve}),rosterGrant())
  const pending=late.source.subscribeRoster(rosterRequest,()=>assert.fail("late callback"),signal())
  late.setGrant(null);release(rosterResponse(rosterWire(value),()=>{cancelled++}))
  await assert.rejects(pending,e=>errorCode(e)==="pa_revoked")
  assert.equal(cancelled,1)
})

test("roster rejects invalid requests without transport and retains transport versus protocol failure evidence",async()=>{
  let calls=0
  const {source}=make(async()=>{calls++;return rosterResponse("")},rosterGrant())
  for(const request of [{...rosterRequest,machine_id:"foreign"},{...rosterRequest,cursor:"界".repeat(1366)},{...rosterRequest,cursor:"forged\nid"}]) await assert.rejects(source.subscribeRoster(request,()=>assert.fail("invalid callback"),signal()))
  const control=new AbortController();control.abort()
  await assert.rejects(source.subscribeRoster(rosterRequest,()=>assert.fail("aborted callback"),control.signal),e=>(e as Error).name==="AbortError")
  assert.equal(calls,0)
  const network=new Error("synthetic disconnect")
  const broken=make(async()=>{throw network},rosterGrant()).source
  await assert.rejects(broken.subscribeRoster(rosterRequest,()=>{},signal()),e=>errorCode(e)==="pa_offline"&&(e as {response:unknown}).response===network)
  const invalid=make(async()=>Response.json({}),rosterGrant()).source
  await assert.rejects(invalid.subscribeRoster(rosterRequest,()=>{},signal()),e=>e instanceof RosterStreamFailure&&e.kind==="invalid")
  const failed=make(async()=>Response.json({reason:"synthetic unavailable"},{status:503}),rosterGrant()).source
  await assert.rejects(failed.subscribeRoster(rosterRequest,()=>{},signal()),e=>errorCode(e)==="pa_offline"&&(e as {response:{reason:string}}).response.reason==="synthetic unavailable")
})

test("roster cleans caller and lifetime abort listeners, reader lock and the transport signal",async()=>{
  for(const stop of ["caller","lifetime"] as const) {
    const control=new AbortController(),held:Response[]=[],signals:AbortSignal[]=[];let cancelled=0,adds=0,removes=0,ready!:()=>void
    const started=new Promise<void>(resolve=>{ready=resolve})
    const add=control.signal.addEventListener.bind(control.signal),remove=control.signal.removeEventListener.bind(control.signal)
    control.signal.addEventListener=((...args:Parameters<typeof add>)=>{adds++;add(...args)}) as typeof add
    control.signal.removeEventListener=((...args:Parameters<typeof remove>)=>{removes++;remove(...args)}) as typeof remove
    const {source}=make(async(_path,init)=>{signals.push(init!.signal!);const response=rosterResponse(rosterWire({type:"hello",source_epoch:"epoch"}),()=>{cancelled++});held.push(response);return response},rosterGrant())
    const pending=source.subscribeRoster(rosterRequest,()=>ready(),control.signal)
    await started
    if(stop==="caller")control.abort();else source.revoke()
    await assert.rejects(pending,e=>stop==="caller"?(e as Error).name==="AbortError":errorCode(e)==="pa_revoked")
    assert.equal(adds,1);assert.equal(removes,1);assert.equal(held[0].body!.locked,false);assert.equal(signals[0].aborted,true);assert.equal(cancelled,1)
  }
})

const notSentEvidence = (e: unknown) => (clientModule as {provablyNotSent?: (e: unknown) => boolean}).provablyNotSent?.(e) === true
const sendRequest = (id: string) => ({command:command(key,"send",id),fence:viewWire().head.current_fence,text:"synthetic held text",quote_message_id:""}) as W.ProjectAssistantSendRequest
test("send recovery: a send refused before its transport proves nothing left and never touches the transport",async()=>{
  let sends = 0
  const {source,setGrant} = make(async path => {if (path.endsWith("/status")) return Response.json(sourceStatus()); sends++; return Response.json({})})
  await source.status(signal())
  setGrant({...grant(),authorizationRevision:"grant-2"})
  await assert.rejects(source.send(sendRequest("revoked-before"),signal()),e => {assert.equal(errorCode(e),"pa_revoked");assert.equal(notSentEvidence(e),true);return true})
  setGrant(grant())
  const cancelled = new AbortController(); cancelled.abort()
  await assert.rejects(source.send(sendRequest("aborted-before"),cancelled.signal),e => {assert.equal((e as Error).name,"AbortError");assert.equal(notSentEvidence(e),true);return true})
  assert.equal(sends,0)
})
test("send recovery: authority that changes after the transport keeps a write outcome unknown with its evidence",async()=>{
  for (const change of ["offline","unpaired","revoked","abort"] as const) {
    let release!: (value: Response) => void, sends = 0
    const control = new AbortController()
    const {source,setGrant} = make(async path => {if (path.endsWith("/status")) return Response.json(sourceStatus()); sends++; return new Promise<Response>(resolve => {release=resolve})})
    await source.status(signal())
    const pending = source.send(sendRequest("crossed-"+change),control.signal)
    await new Promise(resolve => setTimeout(resolve,0))
    if (change === "abort") control.abort()
    else setGrant(change === "offline" ? {...grant(),online:false} : change === "unpaired" ? {...grant(),paired:false} : {...grant(),authorizationRevision:"grant-2"})
    release(Response.json({contract_version:"project-assistants.v1",receipt:{id:"r",command:sendRequest("crossed-"+change).command,event_seq:1,resource_id:"turn",state:"accepted"}}))
    await assert.rejects(pending,e => {assert.equal(errorCode(e),"pa_outcome_unknown",change);assert.equal(notSentEvidence(e),false);return true})
    assert.equal(sends,1)
  }
})
test("send recovery: a lost send response is outcome unknown, while a source refusal stays definitive",async()=>{
  const lost = make(async path => {if (path.endsWith("/status")) return Response.json(sourceStatus()); throw new TypeError("network lost after write")}).source
  await lost.status(signal())
  await assert.rejects(lost.send(sendRequest("lost"),signal()),e => errorCode(e) === "pa_outcome_unknown" && !notSentEvidence(e))
  const refusal: W.ProjectAssistantErrorResponse = {contract_version:"project-assistants.v1",error:{code:"pa_stale_generation",field:"fence",retry_after_seconds:0}}
  const refused = make(async path => path.endsWith("/status") ? Response.json(sourceStatus()) : Response.json(refusal,{status:409})).source
  await refused.status(signal())
  await assert.rejects(refused.send(sendRequest("refused"),signal()),e => {assert.deepEqual(e,refusal);return true})
})
test("send recovery: Cloud relay refusals before the encrypted request are marked as not sent",async()=>{
  let sent = 0
  const connection = {machineDescriptor:() => ({machine:{commands:["project-assistants.send"]}}),async _machineRequestAs(){sent++;throw new TypeError("relay dropped after write")}}
  const body = JSON.stringify(sendRequest("relay")), init = (signal?: AbortSignal): RequestInit => ({method:"POST",body,headers:{"Idempotency-Key":"relay"},signal})
  const relay = capturedAssistantRelayFetch("one",() => connection,["project-assistants.send"])
  await assert.rejects(capturedAssistantRelayFetch("one",() => null,["project-assistants.send"])("/v1/project-assistants/same%2Fid/messages",init()),e => errorCode(e) === "pa_offline" && notSentEvidence(e))
  const cancelled = new AbortController(); cancelled.abort()
  await assert.rejects(relay("/v1/project-assistants/same%2Fid/messages",init(cancelled.signal)),e => (e as Error).name === "AbortError" && notSentEvidence(e))
  await assert.rejects(capturedAssistantRelayFetch("one",() => ({machineDescriptor:() => ({machine:{commands:[]}})}),["project-assistants.send"])("/v1/project-assistants/same%2Fid/messages",init()),e => errorCode(e) === "pa_unsupported" && notSentEvidence(e))
  assert.equal(sent,0)
  await assert.rejects(relay("/v1/project-assistants/same%2Fid/messages",init()),e => e instanceof TypeError && !notSentEvidence(e))
  assert.equal(sent,1)
  // Through the captured client, a relay that may have written stays unknown.
  const {source} = make(async (path,requestInit) => path.endsWith("/status") ? Response.json(sourceStatus()) : relay(path,requestInit))
  await source.status(signal())
  await assert.rejects(source.send(sendRequest("relay"),signal()),e => errorCode(e) === "pa_outcome_unknown" && !notSentEvidence(e))
})

test("login and selected-account configuration keep source API and exact carried shapes",async()=>{
 const paths:string[]=[];const fresh=make(async path=>{paths.push(path);return Response.json({profiles:[],catalog:{agent:"codex",model:"",options:[]}})},{...grant(),apiLevel:81}).source;
 await fresh.loginProfiles(key,"chat","codex",signal());assert.equal(assistantRelayRequest(paths[0]).word,"project-assistants.login-profiles");
 await fresh.extensions(key,"chat",signal());assert.equal(assistantRelayRequest(paths[1]).word,"project-assistants.extensions");
 await fresh.configuration(key,"chat","codex","",signal(),"a".repeat(32));const routed=assistantRelayRequest(paths[2]);assert.equal(routed.word,"project-assistants.configuration-profile");assert.equal((routed.fields as Record<string,unknown>).login_profile,"a".repeat(32));
 let calls=0;const old=make(async()=>{calls++;return Response.json({})},{...grant(),apiLevel:80}).source;await assert.rejects(old.configuration(key,"chat","codex","",signal(),"a".repeat(32)),e=>errorCode(e)==="pa_unsupported");assert.equal(calls,0)
});

test("stored login recovery cannot cross Bot, Project or conversation",()=>{
 const bot=viewWire().snapshot!.bot;const request:W.ProjectAssistantLoginProfileAction={command:command(bot.key,"login-profile"),fence:{key:bot.key,project:bot.project,conversation_id:bot.conversation_id,binding_id:"",generation:0},expected_version:bot.version,profile_id:"",revision:0,agent:bot.agent,action:"create",name:"Synthetic account"};assert.equal(validLoginIntent(request,bot),true);
 for(const change of [(q:W.ProjectAssistantLoginProfileAction)=>q.command.key={...q.command.key,bot_id:"another"},(q:W.ProjectAssistantLoginProfileAction)=>q.fence.project={...q.fence.project,canonical_id:"another"},(q:W.ProjectAssistantLoginProfileAction)=>q.fence.conversation_id="another"]){const copy=structuredClone(request);change(copy);assert.equal(validLoginIntent(copy,bot),false)}
});

test("connector credential operations require API83 and retain closed carried scopes",async()=>{
 const requests:{path:string;init?:RequestInit}[]=[];
 const client=make(async(path,init)=>{if(path.endsWith("/status"))return Response.json(sourceStatus());requests.push({path,init});return Response.json({contract_version:"project-assistants.v1",credential:{state:"prepared",revision:1}})},{...grant(),apiLevel:83}).source;
 await client.status(signal());
 const bot=viewWire().snapshot!.bot;
 const request:W.ProjectAssistantConnectorCredentialAction={command:command(key,"connector-credential"),fence:{key,project:bot.project,conversation_id:"chat",binding_id:"",generation:0},expected_version:1,ref:{id:"connector:fixture",fingerprint:"a".repeat(64)},action:"prepare",target:""};
 await client.connectorCredential(request,signal());
 const prepared=assistantRelayRequest(requests[0].path,requests[0].init);assert.equal(prepared.word,"project-assistants.connector-credential");
 await client.connectorSecret({...request,action:"submit",target:"synthetic-target"},"SYNTHETIC_CONNECTOR_ONLY",signal());
 const secret=assistantRelayRequest(requests[1].path,requests[1].init);assert.equal(secret.word,"project-assistants.connector-secret");assert.equal((secret.fields as Record<string,unknown>).value,"SYNTHETIC_CONNECTOR_ONLY");
 let calls=0;const old=make(async()=>{calls++;return Response.json({})},{...grant(),apiLevel:82}).source;
 await assert.rejects(old.connectorSecret(request,"SYNTHETIC_CONNECTOR_ONLY",signal()),e=>errorCode(e)==="pa_unsupported");assert.equal(calls,0);
});

test("network settings keep API84, exact source routing and explicit false preferences",async()=>{
 const requests:{path:string;init?:RequestInit}[]=[];
 const source=make(async(path,init)=>{if(path.endsWith("/status"))return Response.json(sourceStatus());requests.push({path,init});return Response.json({contract_version:"project-assistants.v1",network:{}})},{...grant(),apiLevel:84}).source;
 await source.status(signal());await source.networkSettings(key,"chat",signal());
 const read=assistantRelayRequest(requests[0].path);assert.equal(read.word,"project-assistants.network-settings");assert.deepEqual(read.fields,{...key,conversation_id:"chat"});
 const bot=viewWire().snapshot!.bot,request:W.ProjectAssistantNetworkSettingsRequest={command:command(key,"network-settings"),fence:{key,project:bot.project,conversation_id:"chat",binding_id:"",generation:0},expected_version:1,expected_revision:0,patch:{offered_to_network:{enabled:false}}};
 await source.saveNetworkSettings(request,signal());const action=assistantRelayRequest(requests[1].path,requests[1].init);assert.equal(action.word,"project-assistants.network-settings-save");assert.deepEqual(action.fields,request);assert.deepEqual(JSON.parse(String(requests[1].init?.body)).patch,{offered_to_network:{enabled:false}});
 let calls=0;const old=make(async()=>{calls++;return Response.json({})},{...grant(),apiLevel:83}).source;await assert.rejects(old.networkSettings(key,"chat",signal()),e=>errorCode(e)==="pa_unsupported");assert.equal(calls,0);
 for(const suffix of ["&account_id=foreign","&conversation_id=other"]){assert.throws(()=>assistantRelayRequest(requests[0].path+suffix),e=>errorCode(e)==="pa_invalid")}
});

test("reaction requests preserve desired state, fixed identity and source API ceiling",async()=>{
 const requests:{path:string;init?:RequestInit}[]=[];let live={...grant(),apiLevel:85};const source=new ProjectAssistantSourceClient(live,async(path,init)=>{requests.push({path,init});return Response.json(path.endsWith("/status")?sourceStatus():{})},()=>live);
 await source.status(signal());assert.equal(source.canReactions(),true);const bot=viewWire().snapshot!.bot;
 const q:W.ProjectAssistantReactRequest={command:command(bot.key,"react","fixed-reaction"),fence:{key:bot.key,project:bot.project,conversation_id:"chat",binding_id:"",generation:0},message_id:"message",emoji:"👍",present:true,expected_revision:0};
 await source.react(q,signal());await source.react(q,signal());assert.equal(requests[1].path,requests[2].path);assert.equal(requests[1].init?.body,requests[2].init?.body);const routed=assistantRelayRequest(requests[1].path,requests[1].init);assert.equal(routed.word,"project-assistants.react");assert.deepEqual(routed.fields,q);
 live={...live,apiLevel:84};assert.equal(source.canReactions(),false);await assert.rejects(source.react(q,signal()),e=>errorCode(e)==="pa_unsupported");assert.equal(requests.length,3)
});


test("topic memory writes require API89 before transport while short facts retain their tier",async()=>{
 let writes=0
 const request={command:command(key,"memory_save"),expected_revision:1,entry:{id:"topic",key,project:{machine_id:"one",project_id:"project",canonical_id:"canonical",kind:"repository"},text:"On-demand body",sources:[],revision:1,forgotten:false,kind:"note",title:"Checklist",summary:"Readiness"}} as W.ProjectAssistantMemoryWriteRequest
 const old=make(async(path,init)=>{if(init?.method!=="GET")writes++;return Response.json(path.endsWith("/status")?sourceStatus():{})},{...grant(),apiLevel:88}).source
 await old.status(signal());assert.equal(old.canTopicMemory(),false)
 await assert.rejects(old.saveMemory(request,signal()),error=>errorCode(error)==="pa_unsupported");assert.equal(writes,0)
 const current=make(async(path,init)=>{if(init?.method!=="GET")writes++;return Response.json(path.endsWith("/status")?sourceStatus():{})},{...grant(),apiLevel:89}).source
 await current.status(signal());assert.equal(current.canTopicMemory(),true)
 await current.saveMemory(request,signal());assert.equal(writes,1)
 await assert.rejects(old.saveMemory({...request,entry:{...request.entry,kind:"",title:undefined,summary:undefined}},signal()),error=>errorCode(error)==="pa_unsupported");assert.equal(writes,1,"empty new metadata was sent to an old closed decoder")
 const plain={...request,entry:{...request.entry,kind:undefined,title:undefined,summary:undefined}}
 await old.saveMemory(plain,signal());assert.equal(writes,2)
})
