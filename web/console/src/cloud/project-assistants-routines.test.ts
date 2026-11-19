import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import {ProjectAssistantSourceClient,assistantRelayRequest,type AssistantGrant} from "./project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import {command,errorCode} from "../project-assistants/client.ts"

const key={machine_id:"synthetic-source",bot_id:"bot alpha"},project:W.ProjectAssistantProjectScope={machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository"}
const fence:W.ProjectAssistantFence={key,project,conversation_id:"chat",binding_id:"binding",generation:1}
const query:W.ProjectAssistantRoutineListRequest={key,project,conversation_id:"chat",routine_id:"",cursor:"",page_size:100}
const signal=()=>new AbortController().signal
const grant=():AssistantGrant=>({accountKey:"account",readerID:"reader",machineID:key.machine_id,authorizationRevision:"grant",apiLevel:73,paired:true,online:true,writable:true})
const sourceStatus=()=>({machine_id:key.machine_id,state:"online",access:"interactive",fresh:true,readable:true,writable:true,observed_at:"",capabilities:[]})
const page=()=>({contract_version:"project-assistants.v1",source:sourceStatus(),fence,definitions:[],runs:[],next_cursor:""})

test("routine hook relay keeps its captured source and stable command",()=>{
 const request={command:command(key,"edit","hook-key"),fence,id:"synthetic-routine",expected_revision:1,rotate:true}
 const routed=assistantRelayRequest(`/v1/project-assistants/${encodeURIComponent(key.bot_id)}/routines/synthetic-routine/hook`,{method:"POST",headers:{"Idempotency-Key":"hook-key"},body:JSON.stringify(request)})
 assert.equal(routed.word,"project-assistants.routine-hook")
 assert.equal(routed.id,"hook-key")
 assert.deepEqual(routed.fields,request)
 assert.throws(()=>assistantRelayRequest(`/v1/project-assistants/${encodeURIComponent(key.bot_id)}/routines/other/hook`,{method:"POST",headers:{"Idempotency-Key":"hook-key"},body:JSON.stringify(request)}),e=>errorCode(e)==="pa_invalid")
})

test("routine management negotiates API73 before transport and keeps captured Project scope",async()=>{
  let calls=0;const old={...grant(),apiLevel:72},source=new ProjectAssistantSourceClient(old,async()=>{calls++;return Response.json(page())},()=>old)
  assert.equal(source.canRoutines(),false)
  await assert.rejects(source.routines(query,signal()),e=>errorCode(e)==="pa_unsupported");assert.equal(calls,0)
  const g=grant(),seen:string[]=[],fresh=new ProjectAssistantSourceClient(g,async input=>{seen.push(input);return Response.json(page())},()=>g)
  await fresh.routines(query,signal());const routed=assistantRelayRequest(seen[0])
  assert.equal(routed.word,"project-assistants.routines");assert.deepEqual(routed.fields,{key,project,conversation_id:"chat",cursor:"",page_size:100})
  const wrong=new ProjectAssistantSourceClient(g,async()=>Response.json({...page(),fence:{...fence,project:{...project,canonical_id:"wrong"}}}),()=>g)
  await assert.rejects(wrong.routines(query,signal()),e=>errorCode(e)==="pa_scope_denied")
})

test("routine preview is a read on the captured Bot source and validates the answer",async()=>{
  const preview:W.ProjectAssistantRoutinePreviewRequest={key,project,conversation_id:"chat",expression:"17 9 * * 1-5",time_zone:"Asia/Taipei"}
  const g={...grant(),apiLevel:96},requests:string[]=[]
  const source=new ProjectAssistantSourceClient(g,async input=>{
    requests.push(input)
    return Response.json({contract_version:"project-assistants.v1",next_due:"2026-11-23T01:17:00Z"})
  },()=>g)
  assert.equal(source.canRoutinePreview(),true)
  assert.equal((await source.routinePreview(preview,signal())).next_due,"2026-11-23T01:17:00Z")
  const routed=assistantRelayRequest(requests[0])
  assert.equal(routed.word,"project-assistants.routine-preview")
  assert.equal(routed.kind,"read")
  assert.deepEqual(routed.fields,preview)
  assert.throws(()=>assistantRelayRequest(requests[0]+"&expression=second"),e=>errorCode(e)==="pa_invalid")
  await assert.rejects(source.routinePreview({...preview,project:{...project,machine_id:"another"}},signal()),e=>errorCode(e)==="pa_scope_denied")
  assert.equal(requests.length,1)
})

test("routine reads discard late revoked grants and disabled history cannot write",async()=>{
  let live:AssistantGrant|null=grant(),release!:()=>void
  const g=live,waiting=new Promise<void>(resolve=>release=resolve)
  const source=new ProjectAssistantSourceClient(g,async()=>{await waiting;return Response.json(page())},()=>live)
  const pending=source.routines(query,signal());live=null;release();await assert.rejects(pending,e=>errorCode(e)==="pa_revoked")
  live=grant();let calls=0
  const history=new ProjectAssistantSourceClient(live,async()=>{calls++;return Response.json({...page(),source:{...sourceStatus(),access:"history_only",writable:false}})},()=>live)
  await history.routines(query,signal())
  const action={command:command(key,"edit","stable-command"),fence,id:"routine",expected_revision:1}
  await assert.rejects(history.testRoutine(action,signal()),e=>errorCode(e)==="pa_read_only");assert.equal(calls,1)
})

test("routine writes preserve exact command bytes and all six Cloud routes",async()=>{
  const g=grant(),requests:{input:string;init?:RequestInit}[]=[],source=new ProjectAssistantSourceClient(g,async(input,init)=>{requests.push({input,init});return Response.json(input.endsWith("/status")?{contract_version:"project-assistants.v1",sources:[sourceStatus()]}:{contract_version:"project-assistants.v1",receipt:{id:"receipt",resource_id:"routine",command:JSON.parse(init?.body as string).command,state:"delivered",event_seq:0}})},()=>g)
  await source.status(signal())
  const base={command:command(key,"edit","stable-command"),fence,id:"routine",expected_revision:2}
  const save:W.ProjectAssistantRoutineSaveRequest={...base,name:"Unchanged milliseconds",instruction:"Synthetic task",timeout_seconds:3600,enabled:true,triggers:[{type:"once",at:1900000000123,seconds:0,expression:"",timeZone:""},{type:"cron",at:0,seconds:0,expression:"0 9 * * *",timeZone:"Asia/Taipei"}]}
  await source.saveRoutine(save,signal());await source.saveRoutine(save,signal())
  assert.equal(requests[1].init?.body,requests[2].init?.body)
  await source.enableRoutine({...base,enabled:false},signal());await source.deleteRoutine(base,signal());await source.testRoutine(base,signal())
  const actions=requests.slice(1).map(r=>assistantRelayRequest(r.input,r.init))
  assert.deepEqual(actions.map(a=>a.word),["project-assistants.routine-save","project-assistants.routine-save","project-assistants.routine-enabled","project-assistants.routine-delete","project-assistants.routine-test"])
  assert.deepEqual(actions[0].fields,save);assert.equal(actions[0].id,"stable-command")
  const url="/v1/project-assistants/bot%20alpha/routines/routine/runs?"+new URLSearchParams({machine_id:key.machine_id,project_id:project.project_id,canonical_id:project.canonical_id,kind:project.kind,conversation_id:"chat",cursor:"",page_size:"100"})
  const history=assistantRelayRequest(url);assert.equal(history.word,"project-assistants.routine-runs");assert.equal((history.fields as Record<string,unknown>).routine_id,"routine")
})

test("unknown or malformed routine receipts stay unknown instead of claiming success",async()=>{
 const g=grant(),base={command:command(key,"edit","uncertain"),fence,id:"routine",expected_revision:1}
 let writing=false
 const source=new ProjectAssistantSourceClient(g,async()=>Response.json(writing?{contract_version:"project-assistants.v1",receipt:{id:"unknown",resource_id:"run",command:base.command,state:"unknown",event_seq:0}}:{contract_version:"project-assistants.v1",sources:[sourceStatus()]}),()=>g)
 await source.status(signal());writing=true
 await assert.rejects(source.testRoutine(base,signal()),e=>errorCode(e)==="pa_outcome_unknown")
})
