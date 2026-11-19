import {useEffect,useLayoutEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {command,errorCode,provablyNotSent} from "./client"
import {sameConversation,canWrite} from "./view-state"
import type {ReactNode} from "react"
import {catalogWord} from "../catalog"
import type {Words} from "./words"

const tr=(key:string)=>catalogWord("projectAssistants",key)
const known=new Set(["pa_invalid","pa_scope_denied","pa_stale_version","pa_stale_generation","pa_read_only","pa_disabled","pa_unpaired","pa_unsupported","pa_not_found","pa_idempotency_conflict"])

/** A stored intent survives closing the viewer. Only a person's explicit retry sends it again. */
export function ArtifactRename({source,message,output,w,onRenamed,titleIcon}:{titleIcon?:ReactNode;source:SourceClient;message:W.ProjectAssistantMessage;output:W.ProjectAssistantOutput;w:Words;onRenamed:()=>void}) {
  const [editing,setEditing]=useState(false),[draft,setDraft]=useState(output.name),[busy,setBusy]=useState(false),[error,setError]=useState(""),[held,setHeld]=useState<W.ProjectAssistantRenameArtifactRequest|null>(null),[blocked,setBlocked]=useState(false)
  const active=useRef<AbortController|null>(null),live=useRef(true)
  const titleButton=useRef<HTMLButtonElement>(null),wasEditing=useRef(false)
  useLayoutEffect(()=>{
    if(wasEditing.current&&!editing)titleButton.current?.focus({preventScroll:true})
    wasEditing.current=editing
  },[editing])
  const key="clawdline.artifact-rename."+JSON.stringify([source.intentAccount??"local",message.fence.key.machine_id,message.fence.key.bot_id,message.fence.conversation_id,output.artifact_id])
  useEffect(()=>{
    live.current=true
    try {
      const raw=localStorage.getItem(key)
      if(raw){const q=JSON.parse(raw) as W.ProjectAssistantRenameArtifactRequest
        if(q.command?.operation!=="rename-artifact"||!q.command.idempotency_key||!sameConversation(q.fence,message.fence)||q.command.key.machine_id!==message.fence.key.machine_id||q.command.key.bot_id!==message.fence.key.bot_id||q.artifact_id!==output.artifact_id||typeof q.title!=="string"||typeof q.expected_output_id!=="string"||!Number.isSafeInteger(q.expected_version)||q.expected_version<1)throw Error("invalid intent")
        setHeld(q);setDraft(q.title);setEditing(true);setError("pa_outcome_unknown")
      }
    }catch{setBlocked(true);setError("pa_outcome_unknown")}
    return()=>{live.current=false;active.current?.abort()}
  },[key])
  const send=async()=>{
    if(busy||blocked||!source.renameArtifact||source.current?.()===false)return
    const control=new AbortController();active.current=control;setBusy(true);setError("")
    let intent=held,sent=false
    try {
      if(!intent){
        const title=draft.trim()
        if(!title||new TextEncoder().encode(title).length>200||/\p{Cc}/u.test(title))throw {error:{code:"pa_invalid"}}
        const got=await source.get(message.fence.key,control.signal)
        const fence={key:got.bot.key,project:got.bot.project,conversation_id:got.conversation.id,binding_id:got.conversation.binding_id,generation:got.conversation.generation}
        if(!sameConversation(fence,message.fence)||!canWrite(got.source))throw {error:{code:"pa_read_only"}}
        intent={command:command(message.fence.key,"rename-artifact"),fence,artifact_id:output.artifact_id,expected_output_id:output.id,expected_version:output.title_version??1,title}
        localStorage.setItem(key,JSON.stringify(intent));setHeld(intent)
      }
      sent=true
      const answer=await source.renameArtifact(intent,control.signal)
      if(answer.contract_version!=="project-assistants.v1"||answer.receipt.command.key.machine_id!==intent.command.key.machine_id||answer.receipt.command.key.bot_id!==intent.command.key.bot_id||answer.receipt.command.operation!=="rename-artifact"||answer.receipt.command.idempotency_key!==intent.command.idempotency_key||answer.receipt.resource_id!==intent.artifact_id||answer.receipt.state!=="delivered")throw {error:{code:"pa_outcome_unknown"}}
      localStorage.removeItem(key)
      if(live.current&&!control.signal.aborted){setHeld(null);setEditing(false);onRenamed()}
    }catch(e){
      const code=errorCode(e)
      if(!sent||provablyNotSent(e)||known.has(code)){if(intent)try{localStorage.removeItem(key)}catch{};if(live.current)setHeld(null)}
      if(live.current)setError(code)
    }finally{if(live.current)setBusy(false)}
  }
  if(!output.artifact_id||!source.renameArtifact||source.canRenameArtifact?.()!==true)return null
  return <div className={`pa-artifact-rename${titleIcon?" pa-artifact-title-edit":""}`}>{editing?<form onSubmit={e=>{e.preventDefault();void send()}} onKeyDown={e=>{if(e.key==="Escape"){e.preventDefault();e.stopPropagation();if(!busy&&!held&&!blocked){setEditing(false);setError("")}}}}><label>{tr("artifactTitle")}<input autoFocus value={draft} disabled={busy||!!held||blocked} onChange={e=>setDraft(e.target.value)}/></label><button disabled={busy||blocked} type="submit">{busy?tr("renamingArtifact"):held?w.retry:w.save}</button>{!held&&!blocked&&<button type="button" disabled={busy} onClick={()=>{setEditing(false);setError("")}}>{tr("libraryCancel")}</button>}</form>:<button type="button" ref={titleButton} className={titleIcon?"pa-artifact-heading":undefined} aria-label={tr("renameArtifact")} onClick={()=>{setDraft(output.name);setEditing(true)}}>{titleIcon?<>{titleIcon}<span>{output.name}</span></>:tr("renameArtifact")}</button>}{error&&<p role="alert">{held||blocked?tr("renameArtifactUnknown"):tr("renameArtifactFailed")} ({error})</p>}</div>
}
