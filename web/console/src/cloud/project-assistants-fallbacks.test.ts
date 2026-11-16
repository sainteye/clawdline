import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import {ProjectAssistantSourceClient,type AssistantGrant} from "./project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import {command,errorCode,provablyNotSent} from "../project-assistants/client.ts"
const signal=()=>new AbortController().signal
const key={machine_id:"fixture",bot_id:"bot"}
const request=(settings:Partial<W.ProjectAssistantEditSettings>):W.ProjectAssistantEditRequest=>({command:command(key,"edit"),settings:{name:"fixture",appearance:{shape:"blob",color:"blue"},instructions:"",expected_version:1,...settings}})
const status={contract_version:"project-assistants.v1",sources:[{machine_id:"fixture",state:"online",access:"interactive",readable:true,writable:true,fresh:true,observed_at:"",capabilities:[]}]}
function fixture(apiLevel=72,writable=true,disabled=false){
  const grant:AssistantGrant={accountKey:"local",readerID:"local",machineID:key.machine_id,authorizationRevision:"fixture",apiLevel,paired:true,online:true,writable}
  let live:AssistantGrant|null=grant, calls=0, effect:(()=>void)|undefined
  const bodies:unknown[]=[]
  const source=new ProjectAssistantSourceClient(grant,async(_path,init)=>{calls++;if(init?.body){bodies.push(JSON.parse(String(init.body)));effect?.();return Response.json({contract_version:"project-assistants.v1"})}return Response.json(disabled?{...status,sources:status.sources.map(s=>({...s,writable:false,access:"history_only"}))}:status)},()=>live)
  return {source,bodies,count:()=>calls,revoke:()=>{live=null},duringWrite:(f:()=>void)=>{effect=f}}
}
test("API72 fallback writes use captured source; old metadata edits omit new fields",async()=>{
  const f=fixture();assert.equal(f.source.canFallbacks(),true);await f.source.status(signal());await f.source.edit(request({fallback_agents:["codex"],expected_version:1}),signal());assert.deepEqual((f.bodies[0] as W.ProjectAssistantEditRequest).settings.fallback_agents,["codex"])
  const old=fixture(71);await old.source.status(signal());assert.equal(old.source.canFallbacks(),false)
  await assert.rejects(old.source.edit(request({fallback_agents:[]}),signal()),e=>errorCode(e)==="pa_unsupported"&&provablyNotSent(e));assert.equal(old.count(),1)
  await old.source.edit(request({name:"renamed"}),signal());assert.equal(old.count(),2);assert.equal("fallback_agents" in (old.bodies[0] as W.ProjectAssistantEditRequest).settings,false)
})
test("read-only, revoked and late revocation retain the write barrier without retry",async()=>{
  const read=fixture(72,false);await read.source.status(signal());await assert.rejects(read.source.edit(request({fallback_agents:[]}),signal()),e=>errorCode(e)==="pa_read_only"&&provablyNotSent(e));assert.equal(read.count(),1)
  const revoked=fixture();await revoked.source.status(signal());revoked.revoke();assert.equal(revoked.source.canFallbacks(),false);await assert.rejects(revoked.source.edit(request({fallback_agents:[]}),signal()),e=>errorCode(e)==="pa_revoked"&&provablyNotSent(e));assert.equal(revoked.count(),1)
  const late=fixture();await late.source.status(signal());late.duringWrite(late.revoke);await assert.rejects(late.source.edit(request({fallback_agents:[]}),signal()),e=>errorCode(e)==="pa_outcome_unknown"&&!provablyNotSent(e));assert.equal(late.count(),2)
})

test("disabled source status refuses fallback writes before transport",async()=>{const f=fixture(72,true,true);await f.source.status(signal());await assert.rejects(f.source.edit(request({fallback_agents:["codex"]}),signal()),e=>errorCode(e)==="pa_read_only"&&provablyNotSent(e));assert.equal(f.count(),1)})
