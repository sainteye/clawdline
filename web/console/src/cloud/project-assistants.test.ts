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
const key = {machine_id:"one",bot_id:"same/id"}
const grant = (machineID = "one"): AssistantGrant => ({accountKey:"account",readerID:"trusted-reader",machineID,authorizationRevision:"grant-1",apiLevel:56,paired:true,online:true,writable:true})
const sourceStatus = (access: W.ProjectAssistantAccess = "interactive"): W.ProjectAssistantSourcesResponse => ({contract_version:"project-assistants.v1",sources:[{machine_id:"one",access,state:"online",fresh:true,readable:true,writable:access === "interactive",observed_at:"",capabilities:[]}]})
const make = (transport: AssistantFetch, first = grant()) => {
  let current: AssistantGrant | null = first
  const source = new ProjectAssistantSourceClient(first,transport,() => current)
  return {source,setGrant(next: AssistantGrant | null) {current = next}}
}
test("old, unknown, unpaired and offline sources issue no nonexistent API requests",async () => {
  let calls = 0
  for (const changed of [{apiLevel:55},{apiLevel:undefined},{paired:false},{online:false}]) {
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
  assert.deepEqual(words.sort(),["answer","create","edit","events","export","forget","get","history","list","memory","observed","read","send","status","stop"].map(word => "project-assistants."+word).sort())
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
