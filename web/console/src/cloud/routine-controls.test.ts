import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {ProjectAssistantSourceClient,assistantRelayRequest,type AssistantGrant} from "./project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import {errorCode,provablyNotSent} from "../project-assistants/client.ts"
// @ts-expect-error -- Node executes source extensions.
import {newRoutineTrigger} from "../project-assistants/routine-events.ts"
const key={machine_id:"one",bot_id:"bot"},project={machine_id:"one",project_id:"project",canonical_id:"canonical",kind:"repository" as const}
const fence={key,project,conversation_id:"chat",binding_id:"binding",generation:1}
const hook={command:{key,operation:"edit" as const,idempotency_key:"exact-key-read",content_hash:""},fence,id:"routine",expected_revision:1,rotate:false}
const grant:AssistantGrant={accountKey:"account",readerID:"reader",machineID:"one",authorizationRevision:"grant",apiLevel:75,paired:true,online:true,writable:true}
const signal=()=>new AbortController().signal
const status=(access="interactive",writable=true)=>({contract_version:"project-assistants.v1",sources:[{machine_id:"one",access,state:"online",fresh:true,readable:true,writable,observed_at:"",capabilities:[]}]})

test("API75 gates hook and event writes before transport while older calendar operations remain usable",async()=>{
  for(const apiLevel of [73,74]){
    let calls=0
    const g={...grant,apiLevel},source=new ProjectAssistantSourceClient(g,async()=>{calls++;return Response.json(status())},()=>g)
    await source.status(signal());assert.equal(source.canRoutines(),true);assert.equal(source.canRoutineEvents(),false)
    await assert.rejects(source.routineHook(hook,signal()),e=>errorCode(e)==="pa_unsupported"&&provablyNotSent(e))
    for(const type of ["event","webhook"])assert.throws(()=>source.saveRoutine({...hook,name:"event",instruction:"test",triggers:[newRoutineTrigger(type)],timeout_seconds:3600,enabled:true},signal()),e=>errorCode(e)==="pa_unsupported")
    assert.equal(calls,1)
  }
})

test("API74 calendar saves retain the old wire and live API downgrades refuse event fields",async()=>{
  let calls=0
  let live={...grant,apiLevel:74}
  const calendar={...hook,command:{...hook.command,idempotency_key:"calendar"},name:"Calendar",instruction:"Inspect",triggers:[newRoutineTrigger()],timeout_seconds:3600,enabled:true}
  const source=new ProjectAssistantSourceClient(live,async(path,init)=>{
    calls++
    if(path.endsWith("/status"))return Response.json(status())
    assert.deepEqual(JSON.parse(String(init?.body)).triggers,[{type:"interval",seconds:3600,at:0,expression:"",timeZone:""}])
    return Response.json({contract_version:"project-assistants.v1",receipt:{id:"receipt",resource_id:"routine",command:calendar.command,state:"accepted"}})
  },()=>live)
  await source.status(signal());await source.saveRoutine(calendar,signal());assert.equal(calls,2)
  live={...grant,apiLevel:75}
  const fresh=new ProjectAssistantSourceClient(live,async()=>{calls++;return Response.json(status())},()=>live)
  await fresh.status(signal());live={...grant,apiLevel:74}
  await assert.rejects(fresh.saveRoutine({...calendar,triggers:[newRoutineTrigger("event")]},signal()),e=>errorCode(e)==="pa_unsupported"&&provablyNotSent(e))
  assert.equal(calls,3)
})

test("hook controls use the human edit grant and refuse hidden/read-only or mismatched scopes",async()=>{
  for(const [access,writable] of [["hidden",false],["history-only",false],["interactive",false]] as const){
    let calls=0
    const source=new ProjectAssistantSourceClient(grant,async()=>{calls++;return Response.json(status(access,writable))},()=>grant)
    await source.status(signal())
    await assert.rejects(source.routineHook(hook,signal()),e=>errorCode(e)==="pa_read_only")
    assert.equal(calls,1)
  }
  let calls=0
  const source=new ProjectAssistantSourceClient(grant,async()=>{calls++;return Response.json(status())},()=>grant)
  await source.status(signal())
  await assert.rejects(source.routineHook({...hook,fence:{...fence,key:{...key,bot_id:"other"}}},signal()),e=>errorCode(e)==="pa_scope_denied")
  assert.equal(calls,1)
})

test("hook requests keep exact relay identity; revocation discards late key data and diagnostic evidence",async()=>{
  let live:AssistantGrant|null=grant,release!:(value:Response)=>void
  const waiting=new Promise<Response>(resolve=>{release=resolve}),paths:string[]=[],bodies:string[]=[]
  const source=new ProjectAssistantSourceClient(grant,async(path,init)=>{
    if(path.endsWith("/status"))return Response.json(status())
    paths.push(path);bodies.push(String(init?.body))
    const routed=assistantRelayRequest(path,init)
    assert.equal(routed.word,"project-assistants.routine-hook");assert.equal(routed.kind,"action");assert.equal(routed.id,hook.command.idempotency_key);assert.deepEqual(routed.fields,hook)
    return waiting
  },()=>live)
  await source.status(signal())
  const result=source.routineHook(hook,signal())
  live={...grant,accountKey:"other"}
  release(Response.json({contract_version:"project-assistants.v1",url:"/hooks/routines/routine",key:"a".repeat(64)}))
  await assert.rejects(result,e=>errorCode(e)==="pa_outcome_unknown"&&JSON.stringify(e).indexOf("a".repeat(64))<0)
  assert.deepEqual(paths,["/v1/project-assistants/bot/routines/routine/hook"]);assert.equal(bodies[0],JSON.stringify(hook))
})

test("a public or foreign hook URL is refused without retaining key evidence",async()=>{
  const source=new ProjectAssistantSourceClient(grant,async path=>Response.json(path.endsWith("/status")?status():{contract_version:"project-assistants.v1",url:"https://synthetic.invalid/hooks/routines/routine",key:"a".repeat(64)}),()=>grant)
  await source.status(signal())
  await assert.rejects(source.routineHook(hook,signal()),e=>errorCode(e)==="pa_outcome_unknown"&&!JSON.stringify(e).includes("a".repeat(64)))
})
