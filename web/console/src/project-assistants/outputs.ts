import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import {failure} from "./client.ts"
// @ts-expect-error -- Node's strip-types runner loads the source extension.
import {readSource} from "./read-views.ts"

/** The source owns the same registered 100 MiB/5 MiB/384 KiB bounds. */
export function outputMetadata(o:W.ProjectAssistantOutput):W.ProjectAssistantOutput {
  if(!o||typeof o.id!=="string"||!o.id||typeof o.name!=="string"||!o.name||new TextEncoder().encode(o.name).length>(o.artifact_id?200:128)||/\p{Cc}/u.test(o.name)||!o.artifact_id&&/[\/\\]/.test(o.name)||typeof o.artifact_id!=="string"||!/^[0-9a-f]{64}$/.test(o.sha256)||!Number.isSafeInteger(o.bytes)||o.bytes<0||o.bytes>100*1024*1024||o.artifact_id&&o.bytes>5*1024*1024)throw failure("pa_invalid")
  if(o.quiet!==undefined&&(typeof o.quiet!=="boolean"||o.quiet&&!o.artifact_id))throw failure("pa_invalid")
  if(o.title_version!==undefined&&(!Number.isSafeInteger(o.title_version)||o.title_version<0))throw failure("pa_invalid")
  return o
}

/** A new load starts at zero. No bytes escape until the whole digest agrees. */
export async function loadOutput(source:SourceClient,q:W.ProjectAssistantOutputRequest,signal:AbortSignal,progress:(n:number,total:number)=>void=()=>{}):Promise<{output:W.ProjectAssistantOutput;blob:Blob}> {
  if(!source.output)throw failure("pa_unsupported")
  const check=()=>{if(signal.aborted)throw new DOMException("Aborted","AbortError");if(source.current?.()===false)throw failure("pa_revoked")}
  let offset=0,metadata:W.ProjectAssistantOutput|null=null,bytes:Uint8Array<ArrayBuffer>|null=null
  do {
    check();const r=await source.output({...q,offset},signal);check()
    if(!r||r.contract_version!=="project-assistants.v1"||r.key?.machine_id!==q.key.machine_id||r.key?.bot_id!==q.key.bot_id||r.conversation_id!==q.conversation_id||r.offset!==offset||!r.message_id||!q.artifact_id&&r.message_id!==q.message_id)throw failure("pa_scope_denied")
    const state=readSource(r.source,q.key.machine_id)
    if(!state.fresh||!state.readable||state.state!=="online"||state.access==="hidden")throw failure("pa_revoked")
    const out=outputMetadata(r.output)
    if(q.artifact_id?out.artifact_id!==q.artifact_id:out.id!==q.id)throw failure("pa_scope_denied")
    if(metadata&&(out.id!==metadata.id||out.sha256!==metadata.sha256||out.bytes!==metadata.bytes||out.name!==metadata.name||out.title_version!==metadata.title_version||out.quiet!==metadata.quiet))throw failure("pa_stale_version")
    if(!metadata){metadata=out;bytes=new Uint8Array(out.bytes)}
    if(typeof r.data!=="string"||r.data.length>4*Math.ceil(384*1024/3))throw failure("pa_invalid")
    let raw:string;try{raw=atob(r.data)}catch{throw failure("pa_invalid")}
    if(raw.length>384*1024||raw.length!==Math.min(384*1024,out.bytes-offset))throw failure("pa_invalid")
    for(let i=0;i<raw.length;i++)bytes![offset+i]=raw.charCodeAt(i)
    offset+=raw.length;progress(offset,out.bytes)
  }while(offset<metadata!.bytes)
  const hash=new Uint8Array(await crypto.subtle.digest("SHA-256",bytes!));check()
  if([...hash].map(n=>n.toString(16).padStart(2,"0")).join("")!==metadata!.sha256)throw failure("pa_invalid")
  return {output:metadata!,blob:new Blob([bytes!],{type:metadata!.artifact_id?"text/html":"application/octet-stream"})}
}

const attribute=(s:string)=>s.replaceAll("&","&amp;").replaceAll('"',"&quot;").replaceAll("<","&lt;")
/** The trusted outer frame's policy rejects every navigation by its child.
 * Both frames have opaque origins, no app storage, popups, forms or top navigation.
 * Explicit links are offered by the app outside this frame after a real click. */
export function artifactFrame(html:string):string {
  const inner=`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"><script>document.addEventListener("click",function(e){if(e.target.closest&&e.target.closest("a[href],area[href]"))e.preventDefault()},true)</script>${html}`
  return `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; frame-src 'none'; base-uri 'none'; form-action 'none'"><style>html,body,iframe{margin:0;width:100%;height:100%;border:0}</style><iframe title="Artifact content" sandbox="allow-scripts" srcdoc="${attribute(inner)}"></iframe>`
}
