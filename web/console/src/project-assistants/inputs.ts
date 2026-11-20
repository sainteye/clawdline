import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import {command,failure,errorCode} from "./client.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import {readSource} from "./read-views.ts"

/** Same registered limits as the source. Selection keeps File objects, not bytes. */
export const MAX_INPUT_BYTES = 100*1024*1024
const CHUNK = 384*1024
export interface DraftInput {file:File;id:string;metadata?:W.ProjectAssistantInputFile;fence?:string}
export function inputMetadata(f:W.ProjectAssistantInputFile):W.ProjectAssistantInputFile {
  if(!f||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(f.id)||typeof f.name!=="string"||!f.name||f.name==="."||f.name===".."||/[\/\\\p{Cc}]/u.test(f.name)||new TextEncoder().encode(f.name).length>128||!Number.isSafeInteger(f.bytes)||f.bytes<0||f.bytes>MAX_INPUT_BYTES||!/^[0-9a-f]{64}$/.test(f.sha256))throw failure("pa_invalid")
  return f
}
const sameInput=(a:W.ProjectAssistantInputFile,b:W.ProjectAssistantInputFile)=>a.id===b.id&&a.name===b.name&&a.bytes===b.bytes&&a.sha256===b.sha256
const digest=async(data:ArrayBuffer)=>Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",data))).map(n=>n.toString(16).padStart(2,"0")).join("")

/** Explicit retry starts at zero with the same IDs; the source compares bytes.
 * Upload failure never calls send, and no content goes into an offline mailbox. */
export async function uploadInputs(source:SourceClient,fence:W.ProjectAssistantFence,files:readonly DraftInput[],signal:AbortSignal,progress:(n:number,total:number)=>void,revision=0):Promise<string[]> {
  if(!source.upload||!source.canInputs?.())throw failure("pa_unsupported")
  const check=()=>{if(signal.aborted)throw new DOMException("Aborted","AbortError");if(source.current?.()===false)throw failure("pa_revoked")}
  const total=files.reduce((n,f)=>n+f.file.size,0);let sent=0
  for(const draft of files){
    check()
    if(draft.file.size>MAX_INPUT_BYTES)throw failure("pa_capacity_full")
    const epoch=JSON.stringify({fence,revision})
    if(draft.fence&&draft.fence!==epoch){draft.id=crypto.randomUUID();draft.metadata=undefined}
    draft.fence=epoch
    if(!draft.metadata)draft.metadata=inputMetadata({id:draft.id,name:draft.file.name,bytes:draft.file.size,sha256:await digest(await draft.file.arrayBuffer())})
    check();const file=draft.metadata
    for(let offset=0;;){
      const bytes=new Uint8Array(await draft.file.slice(offset,offset+CHUNK).arrayBuffer());check()
      let raw="";for(const n of bytes)raw+=String.fromCharCode(n)
      const done=offset+bytes.length===file.bytes
      let answer:W.ProjectAssistantUploadResponse
      try{answer=await source.upload({command:command(fence.key,"upload"),fence,file,offset,data:btoa(raw),done},signal)}catch(e){if(["pa_stale_version","pa_idempotency_conflict","pa_invalid"].includes(errorCode(e))){draft.id=crypto.randomUUID();draft.metadata=undefined;draft.fence=undefined}throw e}check()
      if(answer.contract_version!=="project-assistants.v1"||answer.key.machine_id!==fence.key.machine_id||answer.key.bot_id!==fence.key.bot_id||!sameInput(inputMetadata(answer.file),file)||answer.offset!==offset+bytes.length||answer.done!==done)throw failure("pa_invalid")
      offset+=bytes.length;sent+=bytes.length;progress(sent,total)
      if(done)break
    }
  }
  return files.map(f=>f.id)
}

/** Check every chunk's scope and the complete digest before download. */
export async function loadInput(source:SourceClient,q:W.ProjectAssistantInputRequest,expected:W.ProjectAssistantInputFile,signal:AbortSignal,progress:(n:number,total:number)=>void):Promise<Blob> {
  if(!source.input)throw failure("pa_unsupported")
  const file=inputMetadata(expected),bytes=new Uint8Array(file.bytes)
  const check=()=>{if(signal.aborted)throw new DOMException("Aborted","AbortError");if(source.current?.()===false)throw failure("pa_revoked")}
  let offset=0
  do {
    check();const r=await source.input({...q,offset},signal);check()
    if(r.contract_version!=="project-assistants.v1"||r.key.machine_id!==q.key.machine_id||r.key.bot_id!==q.key.bot_id||r.conversation_id!==q.conversation_id||r.message_id!==q.message_id||r.offset!==offset)throw failure("pa_scope_denied")
    if(!sameInput(inputMetadata(r.file),file))throw failure("pa_invalid")
    const state=readSource(r.source,q.key.machine_id)
    if(!state.fresh||!state.readable||state.state!=="online"||state.access==="hidden")throw failure("pa_revoked")
    if(typeof r.data!=="string"||r.data.length>4*Math.ceil(CHUNK/3))throw failure("pa_invalid")
    let raw:string;try{raw=atob(r.data)}catch{throw failure("pa_invalid")}
    if(raw.length!==Math.min(CHUNK,file.bytes-offset))throw failure("pa_invalid")
    for(let i=0;i<raw.length;i++)bytes[offset+i]=raw.charCodeAt(i)
    offset+=raw.length;progress(offset,file.bytes)
  }while(offset<file.bytes)
  const hash=await digest(bytes.buffer);check();if(hash!==file.sha256)throw failure("pa_invalid")
  return new Blob([bytes],{type:"application/octet-stream"})
}
