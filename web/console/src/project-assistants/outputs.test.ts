import {test} from "node:test"
import assert from "node:assert/strict"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node loads the source extension.
import {loadOutput,outputMetadata} from "./outputs.ts"
// @ts-expect-error -- Node loads the source extension.
import {errorCode} from "./client.ts"

const key={machine_id:"machine",bot_id:"bot"},q:W.ProjectAssistantOutputRequest={key,conversation_id:"chat",message_id:"message",id:"output",artifact_id:"",offset:0}
async function fixture(bytes:Uint8Array<ArrayBuffer>) {
  const hash=[...new Uint8Array(await crypto.subtle.digest("SHA-256",bytes))].map(n=>n.toString(16).padStart(2,"0")).join("")
  const output:W.ProjectAssistantOutput={id:"output",name:"binary",bytes:bytes.length,sha256:hash,artifact_id:""}
  const calls:number[]=[]
  const source={machineID:key.machine_id,current:()=>true,async output(r:W.ProjectAssistantOutputRequest):Promise<W.ProjectAssistantOutputResponse>{calls.push(r.offset);return{contract_version:"project-assistants.v1",key,conversation_id:"chat",message_id:"message",offset:r.offset,output,data:Buffer.from(bytes.subarray(r.offset,r.offset+384*1024)).toString("base64"),source:{machine_id:key.machine_id,state:"online",access:"interactive",readable:true,writable:true,fresh:true,observed_at:"",capabilities:[]}}}} as unknown as SourceClient
  return{source,output,calls}
}

test("output reads verify all chunks and exact empty/binary snapshots before export",async()=>{
  for(const bytes of [new Uint8Array(),new Uint8Array(384*1024+3).fill(255)]){
    const f=await fixture(bytes),r=await loadOutput(f.source,q,new AbortController().signal)
    assert.deepEqual(new Uint8Array(await r.blob.arrayBuffer()),bytes)
    assert.deepEqual(f.calls,bytes.length?[0,384*1024]:[0])
  }
})

test("output reads refuse corrupt, partial, foreign and changed snapshots",async()=>{
  for(const change of ["corrupt","partial","foreign","changed"]){
    const f=await fixture(new Uint8Array(384*1024+1)),read=f.source.output!
    f.source.output=async(r,s)=>{const answer=await read(r,s);if(change==="corrupt")answer.data="/"+answer.data.slice(1);if(change==="partial")answer.data="";if(change==="foreign")answer.conversation_id="foreign";if(change==="changed"&&r.offset)answer.output={...answer.output,id:"replacement"};return answer}
    await assert.rejects(()=>loadOutput(f.source,q,new AbortController().signal),e=>["pa_invalid","pa_scope_denied","pa_stale_version"].includes(errorCode(e)))
  }
  assert.throws(()=>outputMetadata({id:"x",name:"../escape",bytes:0,sha256:"0".repeat(64),artifact_id:""}),e=>errorCode(e)==="pa_invalid")
})

test("closing or revoking a view discards ignored late bytes and stops subsequent reads",async()=>{
  for(const revoke of [false,true]){
    const f=await fixture(new Uint8Array(384*1024+1)),control=new AbortController(),read=f.source.output!;let live=true,release!:()=>void
    f.source.current=()=>live
    f.source.output=async(r,s)=>{const answer=await read(r,s);await new Promise<void>(resolve=>{release=resolve});return answer}
    const pending=loadOutput(f.source,q,control.signal)
    while(!release)await new Promise(resolve=>setImmediate(resolve))
    if(revoke)live=false;else control.abort();release()
    await assert.rejects(pending,e=>revoke?errorCode(e)==="pa_revoked":(e as Error).name==="AbortError")
    assert.deepEqual(f.calls,[0])
  }
})
