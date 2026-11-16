import { test } from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node executes source extensions.
import { ProjectAssistantSourceClient, assistantRelayRequest, type AssistantGrant } from "../cloud/project-assistants.ts"
// @ts-expect-error -- Node executes source extensions.
import { command, errorCode, provablyNotSent } from "./client.ts"

const signal=()=>new AbortController().signal,key={machine_id:"machine",bot_id:"room"}
const grant=():AssistantGrant=>({accountKey:"local",readerID:"person",machineID:"machine",authorizationRevision:"one",apiLevel:76,paired:true,online:true,writable:true})
const sourceStatus=()=>({contract_version:"project-assistants.v1",sources:[{machine_id:"machine",state:"online",access:"interactive",readable:true,writable:true,fresh:true,observed_at:"",capabilities:[]}]})
test("group operations keep their exact captured source, method, key and command over Cloud",async()=>{
  const seen:ReturnType<typeof assistantRelayRequest>[]=[],held=grant()
  const source=new ProjectAssistantSourceClient(held,async(path,init)=>{if(path.endsWith("/status"))return Response.json(sourceStatus());seen.push(assistantRelayRequest(path,init));return Response.json({contract_version:"project-assistants.v1"})},()=>held)
  await source.status(signal())
  await source.groups({machine_id:"machine"},signal())
  await source.groupHistory({key,before_seq:7,page_size:100},signal())
  const create:W.ProjectAssistantGroupCreateRequest={command:command(key,"create","one"),settings:{target:{machine_id:"machine",project_id:"p"},name:"Review",members:[{machine_id:"machine",bot_id:"a"}]}}
  await source.createGroup(create,signal())
  await source.editGroup({command:command(key,"edit","two"),settings:{expected_version:1,name:"Changed",members:create.settings.members}},signal())
  await source.sendGroup({command:command(key,"send","three"),text:"Hello",reply_to_author:create.settings.members[0]},signal())
  await source.stopGroup({command:command(key,"stop","four")},signal())
  await source.deleteGroup({command:command(key,"delete","five"),expected_version:2},signal())
  await source.teamPlans({key},signal())
  await source.answerTeamPlan({command:command(key,"answer","six"),plan_id:"plan",allow:false},signal())
  await source.edit({command:command(key,"edit","seven"),settings:{expected_version:1,name:"Name",instructions:"",appearance:{shape:"blob",color:"#e39567"},manage_team:false}},signal())
  assert.deepEqual(seen.map(r=>r.word),["groups","group-history","group-create","group-edit","group-send","group-stop","group-delete","team-plans","team-answer","team-settings"].map(w=>"project-assistants."+w))
  assert.deepEqual(seen[2].fields,create)
  assert.equal((seen[8].fields as Record<string,unknown>).allow,false)
  assert.equal(((seen[9].fields as Record<string,unknown>).settings as W.ProjectAssistantEditSettings).manage_team,false)
  assert.deepEqual(seen.filter(r=>r.kind==="action").map(r=>r.id),["one","two","three","four","five","six","seven"])
})
test("old and read-only sources refuse group writes before any transport",async()=>{
  for(const held of [{...grant(),apiLevel:75},{...grant(),writable:false}]){
    let writes=0
    const source=new ProjectAssistantSourceClient(held,async(path,init)=>{if(init?.method&&init.method!=="GET")writes++;return Response.json(path.endsWith("/status")?sourceStatus():{contract_version:"project-assistants.v1"})},()=>held)
    await source.status(signal())
    if(held.apiLevel===76)await source.groupHistory({key,before_seq:0,page_size:100},signal())
    await assert.rejects(source.sendGroup({command:command(key,"send","held"),text:"No effect"},signal()),e=>errorCode(e)===(held.apiLevel===75?"pa_unsupported":"pa_read_only")&&provablyNotSent(e))
    assert.equal(writes,0)
  }
})
test("a revoked grant after an accepted room send preserves unknown outcome and never replays",async()=>{
  let held:AssistantGrant|null=grant(),writes=0,settle!:(response:Response)=>void,started!:()=>void
  const began=new Promise<void>(resolve=>{started=resolve})
  const source=new ProjectAssistantSourceClient(held,async(path)=>{if(path.endsWith("/status"))return Response.json(sourceStatus());writes++;started();return new Promise<Response>(resolve=>{settle=resolve})},()=>held)
  await source.status(signal())
  const request={command:command(key,"send","retained-key"),text:"One send"}
  const sent=source.sendGroup(request,signal());await began;held=null;source.revoke();settle(Response.json({contract_version:"project-assistants.v1"}))
  await assert.rejects(sent,e=>errorCode(e)==="pa_outcome_unknown")
  assert.equal(writes,1);assert.equal(request.command.idempotency_key,"retained-key")
})
test("group relay mapping rejects mismatched targets, duplicate queries and unsafe pagination",()=>{
  const request={command:command(key,"send","same"),text:"Hello"},init={method:"POST",headers:{"Idempotency-Key":"same"},body:JSON.stringify(request)}
  assert.throws(()=>assistantRelayRequest("/v1/project-assistants/rooms/groups/other/messages",init))
  assert.throws(()=>assistantRelayRequest("/v1/project-assistants/rooms/groups?machine_id=machine&machine_id=other"))
  assert.throws(()=>assistantRelayRequest("/v1/project-assistants/rooms/groups/room?machine_id=machine&before_seq=9007199254740992&page_size=100"))
})
