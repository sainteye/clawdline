import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node executes source extensions.
import {authorized,errorCode,command,type SourceClient} from "./client.ts"
test("grant revocation rejects an operation both before execution and after a delayed response",async()=>{
  const source={machineID:"one",authorizationRevision:"grant"} as SourceClient
  let sources=[source],called=0
  await assert.rejects(authorized(source,()=>[],async()=>{called++;return 1},new AbortController().signal),e=>errorCode(e)==="pa_revoked")
  assert.equal(called,0)
  await assert.rejects(authorized(source,()=>sources,async()=>{sources=[];return 1},new AbortController().signal),e=>errorCode(e)==="pa_revoked")
})
test("aborted reads cannot return a successful stale value",async()=>{
  const source={machineID:"one",authorizationRevision:"grant"} as SourceClient,control=new AbortController()
  await assert.rejects(authorized(source,()=>[source],async()=>{control.abort();return "stale"},control.signal),{name:"AbortError"})
})
test("admissions retain an explicit stable key and do not claim a client content hash",()=>{
  const key={machine_id:"one",bot_id:"two"}
  assert.deepEqual(command(key,"send","stable"),{key,operation:"send",idempotency_key:"stable",content_hash:""})
})
