// Fault injection for tower floor placements: each failure names itself, a
// refusal before sending changes nothing, and nothing is placed twice. The
// synthetic source keeps one document as a machine would; no HTTP.
import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const {outputFiles}=await build({stdin:{contents:'export {readFloors,placeFloor,placedFloors,FloorError} from "./tower/tower-floors"',resolveDir:fileURLToPath(new URL("..",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {readFloors,placeFloor,placedFloors,FloorError}=compiled.exports

const notSent=(code)=>Object.assign({contract_version:"project-assistants.v1",error:{code,field:"",retry_after_seconds:0}},{clawdline_delivery:"not_sent"})
const answered=(code)=>({contract_version:"project-assistants.v1",error:{code,field:"",retry_after_seconds:0}})

function machine(over={}) {
  const doc={version:0,floors:new Map()},sent=[]
  const answer=()=>({contract_version:"project-assistants.v1",machine_id:"m1",version:doc.version,floors:[...doc.floors].map(([project_id,floor])=>({project_id,floor}))})
  const source={machineID:"m1",authorizationRevision:"r1",canTowerFloors:()=>true,
    async towerFloors(){return answer()},
    async saveTowerFloor(r){
      sent.push(r)
      if(over.before)throw over.before(r)
      if(doc.floors.get(r.project_id)!==r.floor){
        if(r.expected_version!==doc.version)throw answered("pa_stale_version")
        doc.floors.set(r.project_id,r.floor);doc.version++
      }
      if(over.after)throw over.after(r)
      return answer()
    },...over.source}
  return {source,doc,sent,live:()=>[source]}
}
const signal=new AbortController().signal
const outcome=async(promise)=>{try{await promise}catch(error){if(error instanceof FloorError)return error.outcome;throw error}assert.fail("expected a refusal")}

test("a placement is stored and read back as this machine's Projects", async () => {
  const {source,live}=machine()
  const first=await readFloors(source,live,4,signal)
  assert.equal(first.version,0)
  const placed=await placeFloor(source,live,first,"p1",3,4,signal)
  assert.equal(placed.version,1)
  assert.deepEqual([...placedFloors([placed])],[[JSON.stringify(["m1","p1"]),3]])
})

test("another device's change first is stale, not overwritten", async () => {
  const m=machine(),seen=await readFloors(m.source,m.live,4,signal)
  m.doc.floors.set("p2",0);m.doc.version=1
  assert.equal(await outcome(placeFloor(m.source,m.live,seen,"p1",3,4,signal)),"stale")
  assert.equal(m.doc.floors.has("p1"),false)
})

test("an answer lost after sending is unknown, and the machine's floors decide", async () => {
  const m=machine({after:()=>new Error("connection reset")}),seen=await readFloors(m.source,m.live,4,signal)
  assert.equal(await outcome(placeFloor(m.source,m.live,seen,"p1",3,4,signal)),"unknown")
  assert.equal(m.doc.floors.get("p1"),3,"the effect happened; the page must read it rather than send again")
  assert.equal(m.sent.length,1)
})

test("a refusal before sending changes nothing and says why", async () => {
  const m=machine({before:()=>notSent("pa_read_only")}),seen=await readFloors(m.source,m.live,4,signal)
  assert.equal(await outcome(placeFloor(m.source,m.live,seen,"p1",3,4,signal)),"refused")
  assert.equal(m.doc.version,0)
})

test("an older daemon is unsupported without a request", async () => {
  const m=machine({source:{canTowerFloors:()=>false}})
  assert.equal(await outcome(readFloors(m.source,m.live,4,signal)),"unsupported")
  assert.equal(await outcome(placeFloor(m.source,m.live,{machine:"m1",version:0,floors:new Map()},"p1",1,4,signal)),"unsupported")
  assert.equal(m.sent.length,0)
})

test("a source this page no longer holds is never asked", async () => {
  const m=machine(),seen=await readFloors(m.source,m.live,4,signal)
  const error=await placeFloor(m.source,()=>[],seen,"p1",1,4,signal).catch(e=>e)
  assert.equal(error.outcome,"refused");assert.equal(error.code,"pa_revoked")
  assert.equal(m.sent.length,0)
})

test("an answer that is not this machine's floors is refused", async () => {
  for (const bad of [{machine_id:"m2"},{version:-1},{floors:[{project_id:"p",floor:4}]},{floors:[{project_id:"p",floor:1},{project_id:"p",floor:2}]}]) {
    const m=machine({source:{async towerFloors(){return {contract_version:"project-assistants.v1",machine_id:"m1",version:0,floors:[],...bad}}}})
    assert.equal(await outcome(readFloors(m.source,m.live,4,signal)),"refused")
  }
})

test("a floor outside the building is not sent", async () => {
  const m=machine()
  assert.equal(await outcome(placeFloor(m.source,m.live,{machine:"m1",version:0,floors:new Map()},"p1",4,4,signal)),"refused")
  assert.equal(m.sent.length,0)
})
