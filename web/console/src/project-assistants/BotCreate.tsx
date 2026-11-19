import { useModalBackdrop } from "../modal-backdrop.js"
import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, failure, projectChoices, provablyNotSent } from "./client"
import { AppearancePicker, BotAvatar } from "./BotAvatar"
import { canRead, canWrite, sameBot } from "./view-state"
import type { Words } from "./words"
import { ApprovalSelect } from "./ApprovalSelect"
import { PaSelect } from "./PaSelect"
import { ProjectMark } from "./ProjectMark"
import { AgentMark, agentName } from "./AgentMark"
import { botFailureText } from "./source-errors"
import { withApproval } from "./approvals"

interface CatalogPage {source:SourceClient;answer:W.ProjectAssistantProjectsResponse;cursors:string[]}
interface CreateAttempt {
  source:SourceClient
  request:W.ProjectAssistantCreateRequest
  unknown:boolean
  missing:boolean
}
export function BotCreate({sources,current,onClose,onCreated,w}: {sources:readonly SourceClient[];current:()=>readonly SourceClient[];onClose:()=>void;onCreated:(bot:W.ProjectAssistantBot)=>void;w:Words}) {
  const [machine,setMachine]=useState(sources[0]?.machineID??""), [name,setName]=useState("")
  const [appearance,setAppearance]=useState<W.ProjectAssistantAppearance>({shape:"blob",color:"#e39567"})
  const [agent,setAgent]=useState<W.ProjectAssistantAgent>("claude"),[project,setProject]=useState(""),[instructions,setInstructions]=useState("")
  const [policy,setPolicy]=useState<W.ProjectAssistantPermissionPolicy>("ask"),[mode,setMode]=useState<string|undefined>(undefined),[catalog,setCatalog]=useState<CatalogPage|null>(null)
  const [operation,setOperation]=useState<"create"|"check"|null>(null),[loading,setLoading]=useState(true),[error,setError]=useState(""),[createError,setCreateError]=useState("")
  const busy=operation!==null
  const pending=useRef<CreateAttempt|null>(null)
  const source=sources.find(s=>s.machineID===machine),selected=useRef(source);selected.current=source
  const lifetime=useRef<AbortController|null>(null),read=useRef<AbortController|null>(null),sequence=useRef(0)
  const retry=useRef("")
  const load=async(cursor="",previous:CatalogPage|null=null)=>{
    read.current?.abort()
    const control=new AbortController(),id=++sequence.current;read.current=control
    setLoading(true);setError("");retry.current=cursor
    try {
      if(!source)throw failure("pa_revoked")
      const request:W.ProjectAssistantProjectsRequest={machine_id:source.machineID,cursor,page_size:100}
      const answer=await authorized(source,current,async signal=>projectChoices(await source.projects(request,signal),request),control.signal)
      if(control.signal.aborted||selected.current!==source||id!==sequence.current)return
      if(previous){
        if(previous.source!==source||answer.revision!==previous.answer.revision||JSON.stringify(answer.agents)!==JSON.stringify(previous.answer.agents))throw failure("pa_cursor_expired")
        const ids=new Set(previous.answer.items.map(item=>item.target.project_id))
        if(answer.items.some(item=>ids.has(item.target.project_id))||answer.next_cursor&&previous.cursors.includes(answer.next_cursor))throw failure("pa_invalid")
      }
      const items=previous?[...previous.answer.items,...answer.items]:answer.items
      setCatalog({source,answer:{...answer,items},cursors:[...previous?.cursors??[],cursor]})
      setProject(old=>items.some(item=>item.target.project_id===old)?old:"")
      setAgent(old=>answer.agents.includes(old)?old:answer.agents[0]??"claude")
    }catch(e){
      if(!control.signal.aborted&&selected.current===source&&id===sequence.current){
        const code=errorCode(e);setError(code)
        // An expired revision needs a fresh first page, only after explicit retry.
        if(code==="pa_cursor_expired"||code==="pa_invalid"||code==="pa_scope_denied")retry.current=""
      }
    }finally{if(!control.signal.aborted&&selected.current===source&&id===sequence.current)setLoading(false)}
  }
  useEffect(()=>{
    const control=new AbortController();lifetime.current=control
    setCatalog(null);setProject("");setOperation(null);void load()
    return()=>{control.abort();read.current?.abort();sequence.current++}
  },[source,current])
  const own=catalog&&catalog.source===source?catalog.answer:null
  const ready=!!own&&!loading&&!error&&canWrite(own.source)
  const canRetryCreate=!!pending.current&&pending.current.source===source&&!!own&&canWrite(own.source)&&!loading
  const verify=async(held:CreateAttempt,control:AbortController)=>{
    held.missing=false
    const key=held.request.command.key
    try {
      const answer=await authorized(held.source,current,signal=>held.source.get(key,signal),control.signal)
      if(control.signal.aborted||selected.current!==held.source)return
      if(answer?.contract_version!=="project-assistants.v1"||!answer.bot?.key||!answer.source)throw failure("pa_invalid")
      if(!sameBot(answer.bot.key,key)||answer.source.machine_id!==key.machine_id||!canRead(answer.source))throw failure("pa_scope_denied")
      if(!answer.source.fresh||answer.source.state!=="online")throw failure("pa_offline")
      onCreated(answer.bot)
    }catch(e){
      if(control.signal.aborted||selected.current!==held.source)return
      const code=errorCode(e)
      held.missing=code==="pa_not_found"
      setCreateError(code)
    }
  }
  const check=async()=>{
    const held=pending.current,control=lifetime.current
    if(busy||!held||held.source!==source||!control||control.signal.aborted)return
    setOperation("check");setCreateError("")
    try {await verify(held,control)}
    finally {if(!control.signal.aborted&&selected.current===source)setOperation(null)}
  }
  const submit=async()=>{
    if(!source||busy||(!ready&&!canRetryCreate)||!name.trim()||!own?.items.some(item=>item.target.project_id===project)||!own.agents.includes(agent)||pending.current&&pending.current.source!==source)return
    const control=lifetime.current
    if(!control||control.signal.aborted)return
    setOperation("create");setCreateError("")
    const settings:W.ProjectAssistantCreateSettings={name:name.trim(),appearance,agent,instructions,permission_policy:policy,...(mode?{mode}:{}),role_snapshot_ref:"",target:{machine_id:machine,project_id:project}}
    // Unknown outcomes retain this exact source, command and settings for manual retry.
    if(!pending.current)pending.current={source,unknown:false,missing:false,request:{command:command({machine_id:machine,bot_id:crypto.randomUUID()},"create"),settings}}
    const held=pending.current
    let attempted=false
    try {
      const response=await authorized(source,current,async signal=>{
        const status=await authorized(source,current,s=>source.status(s),signal),own=status.sources.find(s=>s.machine_id===machine)
        if(!own||!canWrite(own))throw failure("pa_read_only")
        if(control.signal.aborted||selected.current!==source)throw failure("pa_revoked")
        return authorized(source,current,s=>{attempted=true;return source.create(held.request,s)},signal)
      },control.signal)
      if(!response?.bot?.key||!sameBot(response.bot.key,held.request.command.key))throw failure("pa_outcome_unknown")
      if(!control.signal.aborted&&selected.current===source)onCreated(response.bot)
    }catch(e){
      if(!control.signal.aborted&&selected.current===source){
        const code=errorCode(e);setCreateError(code)
        if(attempted&&!provablyNotSent(e)&&["pa_outcome_unknown","execution_effect_unknown","pa_revoked","unavailable"].includes(code)){
          held.unknown=true
          setOperation("check")
          await verify(held,control)
        }else if(!held.unknown)pending.current=null
      }
    }finally{if(!control.signal.aborted&&selected.current===source)setOperation(null)}
  }
  const uncertain=pending.current?.unknown===true
  const wantsCheck=uncertain&&!pending.current?.missing
  const actionText=operation==="check"?w.checkingSend:operation==="create"?w.createSaving:wantsCheck?w.checkSend:uncertain?w.retry:w.createSubmit
  const actionDisabled=busy||(wantsCheck?pending.current?.source!==source:(!ready&&!canRetryCreate)||!name.trim()||!project||!own?.agents.includes(agent))
  const action=()=>void(wantsCheck?check():submit())
  const close=()=>{lifetime.current?.abort();read.current?.abort();sequence.current++;onClose()}
  // A manually retried unknown create uses its held settings, never a new command.
  const backdrop = useModalBackdrop(() => {
    if (!busy && !pending.current && !name && !instructions) close()
  })
  return <div className="pa-form-scrim" {...backdrop}><section className="pa-form-panel" role="dialog" aria-modal="true" aria-label={w.create}>
    <header className="pa-toolbar"><button onClick={close}>{uncertain?w.close:w.cancel}</button><h2>{w.create}</h2><button disabled={actionDisabled} onClick={action}>{actionText}</button></header>
    <div className="pa-form-body"><div className="pa-form-avatar"><BotAvatar appearance={appearance} size={70}/></div><fieldset disabled={busy||!!pending.current}>
      <AppearancePicker value={appearance} onChange={setAppearance} w={w}/><label>{w.name}<input required value={name} onChange={e=>setName(e.target.value)}/></label>
      {sources.length>1&&<PaSelect label={w.machine} value={machine} onChange={value=>{setProject("");setCatalog(null);setMachine(value)}} options={sources.map(s=>({value:s.machineID,label:s.machineName||w.thisMachine}))}/>}
      <div className="pa-field" role="radiogroup" aria-label={w.agent}><span>{w.agent}</span><div className="pa-agent-choices">{(own?.agents??["claude","codex"]).map(a=><button key={a} type="button" role="radio" aria-checked={agent===a} className="pa-agent-choice" disabled={!own?.agents.includes(a)} onClick={()=>{setAgent(a);setMode(undefined)}}><AgentMark agent={a}/><span>{agentName(a)}</span></button>)}</div></div>
      <PaSelect label={w.project} value={project} busy={loading} disabled={loading||!!error||!own} placeholder={w.chooseProject} onChange={setProject} options={own?.items.map(p=>({value:p.target.project_id,label:p.label,icon:<ProjectMark icon={p.icon} size={16}/>}))??[]}/>
      {loading&&<p role="status">{w.projectsLoading}</p>}
      {own?.has_more&&!loading&&!error&&<button type="button" onClick={()=>void load(own.next_cursor,catalog)}>{w.more}</button>}
      {error&&!pending.current&&<button type="button" disabled={loading} onClick={()=>void load(retry.current,retry.current?catalog:null)}>{w.reloadProjects}</button>}
      <ApprovalSelect agent={agent} policy={policy} mode={mode} w={w} onChange={id=>{const next=withApproval(id);setPolicy(next.permission_policy);setMode(next.mode)}}/>
      <label>{w.instructions}<textarea value={instructions} onChange={e=>setInstructions(e.target.value)}/></label>
    </fieldset><p>{w.createHint}</p>{error&&<p role="alert" className="pa-form-error">{w.projectsFailed} <small>({error})</small></p>}
      {createError&&!busy&&<p role="alert" className="pa-form-error">{uncertain&&createError==="pa_not_found"?w.createCheckMissing:wantsCheck?w.createCheckUnknown:botFailureText(createError,w as unknown as Record<string,string>)} <small>({createError})</small></p>}
      <button type="button" className="pa-create-bottom" disabled={actionDisabled} onClick={action}>{actionText}</button></div>
  </section></div>
}
