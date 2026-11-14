import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, failure, projectChoices } from "./client"
import { AppearancePicker, BotAvatar } from "./BotAvatar"
import { canWrite } from "./view-state"
import type { Words } from "./words"
import { ApprovalSelect } from "./ApprovalSelect"
import { AgentMark, agentName } from "./AgentMark"
import { botFailureText } from "./source-errors"
import { withApproval } from "./approvals"

interface CatalogPage {source:SourceClient;answer:W.ProjectAssistantProjectsResponse;cursors:string[]}
export function BotCreate({sources,current,onClose,onCreated,w}: {sources:readonly SourceClient[];current:()=>readonly SourceClient[];onClose:()=>void;onCreated:(bot:W.ProjectAssistantBot)=>void;w:Words}) {
  const [machine,setMachine]=useState(sources[0]?.machineID??""), [name,setName]=useState("")
  const [appearance,setAppearance]=useState<W.ProjectAssistantAppearance>({shape:"blob",color:"#e39567"})
  const [agent,setAgent]=useState<W.ProjectAssistantAgent>("claude"),[project,setProject]=useState(""),[instructions,setInstructions]=useState("")
  const [policy,setPolicy]=useState<W.ProjectAssistantPermissionPolicy>("ask"),[mode,setMode]=useState<string|undefined>(undefined),[catalog,setCatalog]=useState<CatalogPage|null>(null)
  const [busy,setBusy]=useState(false),[loading,setLoading]=useState(true),[error,setError]=useState(""),[createError,setCreateError]=useState("")
  const pending=useRef<{source:SourceClient;request:W.ProjectAssistantCreateRequest;unknown:boolean}|null>(null)
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
    setCatalog(null);setProject("");setBusy(false);void load()
    return()=>{control.abort();read.current?.abort();sequence.current++}
  },[source,current])
  const own=catalog&&catalog.source===source?catalog.answer:null
  const ready=!!own&&!loading&&!error&&canWrite(own.source)
  const canRetryCreate=!!pending.current&&pending.current.source===source&&!!own&&canWrite(own.source)&&!loading
  const submit=async()=>{
    if(!source||busy||(!ready&&!canRetryCreate)||!name.trim()||!own?.items.some(item=>item.target.project_id===project)||!own.agents.includes(agent)||pending.current&&pending.current.source!==source)return
    const control=lifetime.current
    if(!control||control.signal.aborted)return
    setBusy(true);setCreateError("")
    const settings:W.ProjectAssistantCreateSettings={name:name.trim(),appearance,agent,instructions,permission_policy:policy,...(mode?{mode}:{}),role_snapshot_ref:"",target:{machine_id:machine,project_id:project}}
    // Unknown outcomes retain this exact source, command and settings for manual retry.
    if(!pending.current)pending.current={source,unknown:false,request:{command:command({machine_id:machine,bot_id:crypto.randomUUID()},"create"),settings}}
    const held=pending.current
    try {
      const response=await authorized(source,current,async signal=>{
        const status=await authorized(source,current,s=>source.status(s),signal),own=status.sources.find(s=>s.machine_id===machine)
        if(!own||!canWrite(own))throw failure("pa_read_only")
        if(control.signal.aborted||selected.current!==source)throw failure("pa_revoked")
        return authorized(source,current,s=>source.create(held.request,s),signal)
      },control.signal)
      if(!control.signal.aborted&&selected.current===source)onCreated(response.bot)
    }catch(e){
      if(!control.signal.aborted&&selected.current===source){
        const code=errorCode(e);setCreateError(code)
        if(["pa_outcome_unknown","execution_effect_unknown","unavailable"].includes(code))held.unknown=true
        else if(!held.unknown)pending.current=null
      }
    }finally{if(!control.signal.aborted&&selected.current===source)setBusy(false)}
  }
  const close=()=>{lifetime.current?.abort();read.current?.abort();sequence.current++;onClose()}
  // A manually retried unknown create uses its held settings, never a new command.
  return <section className="pa-form-panel" role="dialog" aria-modal="true" aria-label={w.create}>
    <header className="pa-toolbar"><button onClick={close}>{w.cancel}</button><h2>{w.create}</h2><button disabled={busy||(!ready&&!canRetryCreate)||!name.trim()||!project||!own?.agents.includes(agent)} onClick={()=>void submit()}>{pending.current?w.retry:w.createSubmit}</button></header>
    <div className="pa-form-body"><div className="pa-form-avatar"><BotAvatar appearance={appearance} size={70}/></div><fieldset disabled={busy||!!pending.current}>
      <AppearancePicker value={appearance} onChange={setAppearance} w={w}/><label>{w.name}<input required value={name} onChange={e=>setName(e.target.value)}/></label>
      <label>{w.machine}<select value={machine} onChange={e=>{setProject("");setCatalog(null);setMachine(e.target.value)}}>{sources.map(s=><option key={s.machineID} value={s.machineID}>{s.machineName||w.thisMachine}</option>)}</select></label>
      <div className="pa-field" role="radiogroup" aria-label={w.agent}><span>{w.agent}</span><div className="pa-agent-choices">{(own?.agents??["claude","codex"]).map(a=><button key={a} type="button" role="radio" aria-checked={agent===a} className="pa-agent-choice" disabled={!own?.agents.includes(a)} onClick={()=>{setAgent(a);setMode(undefined)}}><AgentMark agent={a}/><span>{agentName(a)}</span></button>)}</div></div>
      <label>{w.project}<select required value={project} aria-busy={loading} disabled={loading||!!error||!own} onChange={e=>setProject(e.target.value)}><option value="">—</option>{own?.items.map(p=><option key={p.target.project_id} value={p.target.project_id}>{p.label}</option>)}</select></label>
      {loading&&<p role="status">{w.projectsLoading}</p>}
      {own?.has_more&&!loading&&!error&&<button type="button" onClick={()=>void load(own.next_cursor,catalog)}>{w.more}</button>}
      {error&&!pending.current&&<button type="button" disabled={loading} onClick={()=>void load(retry.current,retry.current?catalog:null)}>{w.reloadProjects}</button>}
      <ApprovalSelect agent={agent} policy={policy} mode={mode} w={w} onChange={id=>{const next=withApproval(id);setPolicy(next.permission_policy);setMode(next.mode)}}/>
      <label>{w.instructions}<textarea value={instructions} onChange={e=>setInstructions(e.target.value)}/></label>
    </fieldset><p>{w.createHint}</p>{error&&<p role="alert" className="pa-form-error">{w.projectsFailed} <small>({error})</small></p>}
      {createError&&<p role="alert" className="pa-form-error">{botFailureText(createError,w as unknown as Record<string,string>)} <small>({createError})</small></p>}
      <button type="button" className="pa-create-bottom" disabled={busy||(!ready&&!canRetryCreate)||!name.trim()||!project||!own?.agents.includes(agent)} onClick={()=>void submit()}>{pending.current?w.retry:w.createSubmit}</button></div>
  </section>
}
