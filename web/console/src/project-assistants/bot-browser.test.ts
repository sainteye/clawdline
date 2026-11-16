import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { ProjectAssistantSourceClient, assistantRelayRequest, capturedAssistantRelayFetch, type AssistantGrant } from "../cloud/project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import { botFromFragment } from "./client.ts"

test("a Bot notice selects its chat without accepting a partial or ambiguous destination",()=>{
  assert.deepEqual(botFromFragment("#bot=b&conversation=c&machine=source-one"),{bot:"b",conversation:"c",machine:"source-one"})
  assert.equal(botFromFragment("#bot=b"),null)
  assert.equal(botFromFragment("#bot=b&bot=other&conversation=c"),null)
  assert.equal(botFromFragment("#bot=b&conversation=c&session=foreign"),null)
})

const signal = () => new AbortController().signal
const grant: AssistantGrant = {accountKey:"test",readerID:"reader",machineID:"source-one",authorizationRevision:"one",apiLevel:74,paired:true,online:true,writable:true}
const key = {machine_id:grant.machineID,bot_id:"bot-one"}
const status = {contract_version:"project-assistants.v1",sources:[{machine_id:grant.machineID,state:"online",access:"interactive",readable:true,writable:true,fresh:true,capabilities:[]}]}

test("Bot browser pins both target fields to its SourceClient for every route", async () => {
  const seen: {path:string;init?:RequestInit}[] = []
  const source = new ProjectAssistantSourceClient(grant,async(path,init)=>{seen.push({path,init});return Response.json(path.endsWith("/status")?status:{ok:true})},()=>grant)
  await source.status(signal());seen.length=0
  for(const route of ["status","activity","audit","screenshot"]) await source.browser(key,"conversation-one","GET","/v1/browser/"+route+(route==="audit"?"?limit=200":route==="screenshot"?"?tab=9&quality=45":""),undefined,signal())
  for(const route of ["control","person","open","mute"]) await source.browser(key,"conversation-one","POST","/v1/browser/"+route,route==="open"?{url:"https://example.com"}:route==="mute"?{muted:true}:route==="person"?{tab:9,op:"key",args:{key:"Tab"}}:{tab:9,person:true},signal())
  assert.equal(seen.length,8)
  for(const request of seen) {
    const fields=request.init?.body?JSON.parse(String(request.init.body)):Object.fromEntries(new URL(request.path,"http://test").searchParams)
    assert.equal(fields.bot_id,key.bot_id);assert.equal(fields.conversation,"conversation-one")
    assert.equal("machine_id" in fields,false);assert.equal("owner" in fields,false)
  }
})

test("old, unpaired, foreign and read-only Bot sources refuse before transport", async () => {
  for(const [patch,code] of [[{apiLevel:73},"pa_unsupported"],[{paired:false},"pa_unpaired"],[{writable:false},"pa_read_only"]] as const) {
    const live={...grant,...patch};let requests=0
    const source=new ProjectAssistantSourceClient(live,async()=>{requests++;return Response.json(status)},()=>live)
    if(live.paired)await source.status(signal());requests=0
    await assert.rejects(source.browser(key,"conversation-one","POST","/v1/browser/control",{tab:9,person:true},signal()),e=>(e as any).error?.code===code)
    assert.equal(requests,0)
  }
  const source=new ProjectAssistantSourceClient(grant,async()=>{throw Error("must not send")},()=>grant)
  await assert.rejects(source.browser({...key,machine_id:"other"},"c","GET","/v1/browser/status",undefined,signal()),e=>(e as any).error?.code==="pa_scope_denied")
  await assert.rejects(source.browser(key,"","GET","/v1/browser/status",undefined,signal()),e=>(e as any).error?.code==="pa_invalid")
})

test("Cloud browser words carry the same Bot target and action identity", () => {
  const read=assistantRelayRequest("/v1/browser/screenshot?tab=9&bot_id=bot-one&conversation=c&quality=45",{method:"GET"})
  assert.equal(read.word,"chrome-tab-screenshot");assert.deepEqual(read.fields,{tab:"9",bot_id:"bot-one",conversation:"c",quality:"45"})
  const write=assistantRelayRequest("/v1/browser/control",{method:"POST",headers:{"Idempotency-Key":"one"},body:JSON.stringify({tab:9,person:true,bot_id:"bot-one",conversation:"c"})})
  assert.equal(write.word,"chrome-tab-control");assert.equal(write.id,"one")
  assert.throws(()=>assistantRelayRequest("/v1/browser/status?bot_id=bot-one",{method:"GET"}))
  assert.throws(()=>assistantRelayRequest("/v1/browser/audit?bot_id=bot-one&conversation=c",{method:"GET"}))
  assert.throws(()=>assistantRelayRequest("/v1/browser/status?bot_id=bot-one&conversation=c&owner=other",{method:"GET"}))
})

test("stale conversation and unknown outcomes retain their refusal without replay", async () => {
  let actions=0
  const source=new ProjectAssistantSourceClient(grant,async path=>{
    if(path==="/v1/project-assistants/status")return Response.json(status)
    actions++;return Response.json({contract_version:"project-assistants.v1",error:{code:"pa_stale_version",field:"conversation"}},{status:409})
  },()=>grant)
  await source.status(signal())
  await assert.rejects(source.browser(key,"old","POST","/v1/browser/control",{tab:9,person:true},signal()),e=>(e as any).code==="pa_stale_version")
  assert.equal(actions,1)
})

test("Cloud Bot browser reaches the captured source while another machine is selected",async()=>{
  const calls:{machine:string;word:string;fields:unknown}[]=[]
  const carried=["project-assistants.status","chrome-tab-status","chrome-tab-control"]
  const connection={
    machineDescriptor:()=>({machine:{commands:carried}}),
    _machineRequest:async(machine:string,word:string,fields:unknown)=>{calls.push({machine,word,fields});return word==="project-assistants.status"?status:{ok:true}},
    _machineRequestAs:async(_id:string,machine:string,word:string,fields:unknown)=>{calls.push({machine,word,fields});return {ok:true}},
  }
  const source=new ProjectAssistantSourceClient(grant,capturedAssistantRelayFetch(grant.machineID,()=>connection as any,carried),()=>grant)
  await source.status(signal())
  await source.browser(key,"current","GET","/v1/browser/status",undefined,signal())
  await source.browser(key,"current","POST","/v1/browser/control",{tab:9,person:true},signal())
  assert.deepEqual(calls.map(call=>call.machine),[grant.machineID,grant.machineID,grant.machineID])
  assert.deepEqual(calls[2].fields,{tab:9,person:true,bot_id:key.bot_id,conversation:"current"})
})

test("a grant revoked during a browser write reports unknown effect and never resends",async()=>{
  let live:AssistantGrant|null=grant,actions=0
  const source=new ProjectAssistantSourceClient(grant,async path=>{
    if(path==="/v1/project-assistants/status")return Response.json(status)
    actions++;live=null;return Response.json({ok:true})
  },()=>live)
  await source.status(signal())
  await assert.rejects(source.browser(key,"current","POST","/v1/browser/control",{tab:9,person:true},signal()),e=>(e as any).error?.code==="pa_outcome_unknown")
  assert.equal(actions,1)
})
