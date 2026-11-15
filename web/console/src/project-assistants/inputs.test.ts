import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node loads the source extension.
import {uploadInputs,loadInput,inputMetadata,type DraftInput} from "./inputs.ts"
// @ts-expect-error -- Node loads the source extension.
import {errorCode} from "./client.ts"

const key={machine_id:"source",bot_id:"bot"},fence:W.ProjectAssistantFence={key,project:{machine_id:"source",project_id:"project",canonical_id:"canonical",kind:"repository"},conversation_id:"chat",binding_id:"binding",generation:1}
const request:W.ProjectAssistantInputRequest={key,conversation_id:"chat",message_id:"message",id:"11111111-1111-4111-8111-111111111111",offset:0}
const signal=()=>new AbortController().signal
async function fixture(bytes:Uint8Array<ArrayBuffer>) {
  const sha256=[...new Uint8Array(await crypto.subtle.digest("SHA-256",bytes))].map(n=>n.toString(16).padStart(2,"0")).join("")
  const file:W.ProjectAssistantInputFile={id:request.id,name:"binary",bytes:bytes.length,sha256},reads:number[]=[],uploads:W.ProjectAssistantUploadRequest[]=[]
  const source={machineID:key.machine_id,current:()=>true,canInputs:()=>true,
    async upload(r:W.ProjectAssistantUploadRequest){uploads.push(r);return{contract_version:"project-assistants.v1",key,file:r.file,offset:r.offset+atob(r.data).length,done:r.done}},
    async input(r:W.ProjectAssistantInputRequest){reads.push(r.offset);return{contract_version:"project-assistants.v1",key,conversation_id:"chat",message_id:"message",file,offset:r.offset,data:Buffer.from(bytes.subarray(r.offset,r.offset+384*1024)).toString("base64"),source:{machine_id:key.machine_id,state:"online",access:"interactive",readable:true,writable:true,fresh:true,observed_at:"",capabilities:[]}}}} as unknown as SourceClient
  return{source,file,reads,uploads}
}

test("explicit input transfers preserve empty and binary bytes across chunk boundaries",async()=>{
  for(const bytes of [new Uint8Array(),new Uint8Array(384*1024+3).fill(255)]){
    const f=await fixture(bytes),draft:DraftInput={id:request.id,file:new File([bytes],"binary")}
    assert.deepEqual(await uploadInputs(f.source,fence,[draft],signal(),()=>{}),[request.id])
    assert.deepEqual(f.uploads.map(r=>r.offset),bytes.length?[0,384*1024]:[0])
    assert.equal(f.uploads.at(-1)!.done,true)
    assert.deepEqual(new Uint8Array(await(await loadInput(f.source,request,f.file,signal(),()=>{})).arrayBuffer()),bytes)
  }
})

test("failed uploads keep the complete draft and retries keep exact upload identities",async()=>{
  const bytes=new Uint8Array(384*1024+3),f=await fixture(bytes),upload=f.source.upload!
  const draft:DraftInput={id:request.id,file:new File([bytes],"binary")};let failed=false
  f.source.upload=async(r,s)=>{if(r.offset&&!failed){failed=true;throw {error:{code:"pa_offline"}}}return upload(r,s)}
  await assert.rejects(uploadInputs(f.source,fence,[draft],signal(),()=>{}),e=>errorCode(e)==="pa_offline")
  assert.equal(draft.file.size,bytes.length);assert.equal(draft.id,request.id)
  assert.deepEqual(await uploadInputs(f.source,fence,[draft],signal(),()=>{}),[request.id])
  assert.deepEqual(f.uploads.map(r=>r.offset),[0,0,384*1024])
  assert.equal(new Set(f.uploads.map(r=>r.file.id)).size,1)
})

test("input downloads refuse foreign metadata, partial bytes, corruption and revoked authority",async()=>{
  for(const change of ["foreign","partial","corrupt","metadata","revoked"]){
    const f=await fixture(new Uint8Array([1,2,3])),read=f.source.input!
    f.source.input=async(r,s)=>{const a=await read(r,s);if(change==="foreign")a.message_id="other";if(change==="partial")a.data="";if(change==="corrupt")a.data="AAAA";if(change==="metadata")a.file={...a.file,sha256:"0".repeat(64)};if(change==="revoked")a.source.state="revoked";return a}
    await assert.rejects(loadInput(f.source,request,f.file,signal(),()=>{}),e=>["pa_invalid","pa_scope_denied","pa_revoked"].includes(errorCode(e)))
  }
  const f=await fixture(new Uint8Array())
  assert.throws(()=>inputMetadata({...f.file,name:"../outside"}),e=>errorCode(e)==="pa_invalid")
})

test("cancel and revocation discard ignored late upload and download replies",async()=>{
  for(const direction of ["upload","input"] as const){
    const f=await fixture(new Uint8Array(384*1024+1)),controller=new AbortController();let release!:()=>void,live=true
    f.source.current=()=>live
    if(direction==="upload"){const run=f.source.upload!;f.source.upload=async(r,s)=>{const a=await run(r,s);await new Promise<void>(resolve=>{release=resolve});return a}}
    else{const run=f.source.input!;f.source.input=async(r,s)=>{const a=await run(r,s);await new Promise<void>(resolve=>{release=resolve});return a}}
    const pending=direction==="upload"?uploadInputs(f.source,fence,[{id:request.id,file:new File([new Uint8Array(384*1024+1)],"binary")}],controller.signal,()=>{}):loadInput(f.source,request,f.file,controller.signal,()=>{})
    while(!release)await new Promise(resolve=>setImmediate(resolve))
    if(direction==="upload")controller.abort();else live=false;release()
    await assert.rejects(pending,e=>direction==="upload"?(e as Error).name==="AbortError":errorCode(e)==="pa_revoked")
    assert.equal(direction==="upload"?f.uploads.length:f.reads.length,1)
  }
})
