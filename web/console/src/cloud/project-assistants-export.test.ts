import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import { ProjectAssistantSourceClient, capturedAssistantRelayFetch, type AssistantGrant } from "./project-assistants.ts"

test("downloaded Bot export retains stored routine and group fields through captured Cloud reads", async () => {
  const key = {machine_id:"export-machine",bot_id:"export-bot"}
  const project = {machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository"}
  const payload = {
    contract_version:"project-assistants.v1", schema_version:2,
    bot:{key},
    routines:[{id:"routine",key,project,conversation_id:"chat",interval_seconds:0,
      triggers:[{type:"webhook"},{type:"event",source:"github",event:"push",filters:{branch:"main"}}],
      progress:"Handled through #40",progress_revision:1,next_runs:["0001-01-01T00:00:00Z"]}],
    routine_runs:[{id:"run",routine_id:"routine",key,project,delivery_id:"delivery",memory_revision:1,
      state:"succeeded",event:{source:"github",event:"push",nested:{ok:true},count:2}}],
    groups:[{group:{key:{...key,bot_id:"group"},members:[{key,joined_revision:1}],membership_revision:1},
      room:{revision:2,epoch:1,overdue:[{member:key,epoch:1,joined_revision:1,late:true}]}}],
    group_schema_version:1,
  }
  const calls:unknown[]=[]
  const connection = {
    machineDescriptor:()=>({machine:{commands:["project-assistants.export"]}}),
    async _machineRequest(machine:string,word:string,fields:Record<string,unknown>) {
      calls.push({machine,word,fields})
      return structuredClone(payload)
    },
  }
  // The current SourceClient admits exports at API 60; these additions do not
  // require a newer viewer, source capability, or write permission.
  const grant:AssistantGrant={accountKey:"synthetic",readerID:"reader",machineID:key.machine_id,
    authorizationRevision:"grant",apiLevel:60,paired:true,online:true,writable:false}
  const source = new ProjectAssistantSourceClient(grant,
    capturedAssistantRelayFetch(key.machine_id,()=>connection,["project-assistants.export"]),()=>grant)
  const downloaded = await source.export(key,new AbortController().signal)
  assert.deepEqual(downloaded,payload)
  assert.deepEqual(JSON.parse(JSON.stringify(downloaded)),payload,"the downloaded JSON retains every nested field")
  assert.deepEqual(calls,[{machine:key.machine_id,word:"project-assistants.export",fields:key}])
})
