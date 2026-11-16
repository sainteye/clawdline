import {test} from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node executes source extensions.
import {readRoutineIntent} from "./routine-intent.ts"

const key={machine_id:"synthetic-source",bot_id:"synthetic-bot"}
const project={machine_id:key.machine_id,project_id:"project",canonical_id:"canonical",kind:"repository" as const}
const scope={key,project,conversation_id:"chat"}
const original={action:"test",request:{command:{key,operation:"edit",idempotency_key:"original-action",content_hash:""},fence:{key,project,conversation_id:"chat",binding_id:"old-binding",generation:2},id:"routine",expected_revision:1}}

test("a held routine action retains its exact identity and historical fence",()=>{
  const raw=JSON.stringify(original)
  const storage={getItem:()=>raw}
  assert.deepEqual(readRoutineIntent(storage,"journal",scope),{state:"held",intent:original})
  assert.equal(storage.getItem(),raw)
  assert.deepEqual(readRoutineIntent({getItem:()=>null},"journal",scope),{state:"none"})
})

test("unreadable, unsupported and foreign routine commands stay blocked without changing storage",()=>{
  const variants=["",'{"action":"test","request":',"null","[]",JSON.stringify({...original,action:"future-action"}),JSON.stringify({...original,extra:"future-fact"})]
  for(const change of [
    (v:typeof original)=>{v.request.command.key={...key,bot_id:"other"}},
    (v:typeof original)=>{v.request.fence.conversation_id="other"},
    (v:typeof original)=>{v.request.fence.project={...project,project_id:"other"}},
    (v:typeof original)=>{v.request.fence.project={...project,machine_id:"other"}},
  ]) {const value=structuredClone(original);change(value);variants.push(JSON.stringify(value))}
  for(const raw of variants){
    let bytes=raw,writes=0
    const storage={getItem:()=>bytes,setItem:(_key:string,value:string)=>{writes++;bytes=value},removeItem:()=>{writes++;bytes=""}}
    assert.deepEqual(readRoutineIntent(storage,"journal",scope),{state:"blocked"})
    assert.equal(bytes,raw);assert.equal(writes,0)
  }
  assert.deepEqual(readRoutineIntent({getItem:()=>{throw Error("storage unavailable")}},"journal",scope),{state:"blocked"})
})
