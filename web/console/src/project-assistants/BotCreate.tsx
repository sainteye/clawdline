import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, failure } from "./client"
import { AppearancePicker, BotAvatar } from "./BotAvatar"
import { canWrite } from "./view-state"
import type { Words } from "./words"

export function BotCreate({sources,current,onClose,onCreated,w}: {sources:readonly SourceClient[];current:()=>readonly SourceClient[];onClose:()=>void;onCreated:(bot:W.ProjectAssistantBot)=>void;w:Words}) {
  const [machine,setMachine]=useState(sources[0]?.machineID??""), [name,setName]=useState("")
  const [appearance,setAppearance]=useState<W.ProjectAssistantAppearance>({shape:"blob",color:"#e39567"})
  const [agent,setAgent]=useState<W.ProjectAssistantAgent>("claude"),[project,setProject]=useState(""),[instructions,setInstructions]=useState("")
  const [policy,setPolicy]=useState<W.ProjectAssistantPermissionPolicy>("ask"), [projects,setProjects]=useState<W.CatalogProject[]>([])
  const [agents,setAgents]=useState<W.ProjectAssistantAgent[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState("")
  const pending=useRef<W.ProjectAssistantCreateRequest|null>(null)
  const source=sources.find(s=>s.machineID===machine)
  useEffect(()=>{
    const control=new AbortController();setProjects([]);setProject("");setAgents([]);setError("")
    if(source)void authorized(source,current,async signal=>{
      const [status,answer]=await Promise.all([source.status(signal),source.projects(signal)])
      if(!answer.catalog.available)throw failure("pa_scope_denied")
      const own=status.sources.find(s=>s.machine_id===machine)
      if(!own||!canWrite(own))throw failure("pa_read_only")
      const available=[...new Set(own.capabilities.filter(c=>c.read_only_enforced&&c.native_memory_verified&&!c.unavailable).map(c=>c.agent))]
      if(!control.signal.aborted){setProjects(answer.catalog.projects);setAgents(available);setAgent(available[0]??"claude")}
    },control.signal).catch(e=>{if(!control.signal.aborted)setError(errorCode(e))})
    return()=>control.abort()
  },[source,machine,current])
  const submit=async()=>{
    if(!source||busy||!name.trim()||!project||!agents.includes(agent))return
    setBusy(true);setError("")
    const settings:W.ProjectAssistantCreateSettings={name:name.trim(),appearance,agent,instructions,permission_policy:policy,role_snapshot_ref:"",target:{machine_id:machine,project_id:project}}
    // Retain one admission identity after an unknown result. Changed settings require an explicit new form.
    if(!pending.current)pending.current={command:command({machine_id:machine,bot_id:crypto.randomUUID()},"create"),settings}
    try { const response=await authorized(source,current,async signal=>{
      const status=await source.status(signal),own=status.sources.find(s=>s.machine_id===machine)
      if(!own||!canWrite(own))throw failure("pa_read_only")
      return source.create(pending.current!,signal)
    },new AbortController().signal);onCreated(response.bot) }
    catch(e){const code=errorCode(e);setError(code);if(code!=="pa_outcome_unknown"&&code!=="execution_effect_unknown"&&code!=="unavailable")pending.current=null}finally{setBusy(false)}
  }
  return <section className="pa-form-panel" role="dialog" aria-modal="true" aria-label={w.create}>
    <header className="pa-toolbar"><button onClick={onClose}>{w.cancel}</button><h2>{w.create}</h2><button disabled={busy||!name.trim()||!project||!agents.includes(agent)||!!pending.current&&error==="pa_outcome_unknown"} onClick={()=>void submit()}>{w.createSubmit}</button></header>
    <div className="pa-form-body"><div className="pa-form-avatar"><BotAvatar appearance={appearance} size={70}/></div><fieldset disabled={busy||!!pending.current}>
      <AppearancePicker value={appearance} onChange={setAppearance} w={w}/><label>{w.name}<input required value={name} onChange={e=>setName(e.target.value)}/></label>
      <label>{w.machine}<select value={machine} onChange={e=>setMachine(e.target.value)}>{sources.map(s=><option key={s.machineID}>{s.machineID}</option>)}</select></label>
      <label>{w.agent}<select value={agent} onChange={e=>setAgent(e.target.value as W.ProjectAssistantAgent)}>{agents.map(a=><option key={a} value={a}>{a==="claude"?"Claude Code":"Codex"}</option>)}</select></label>
      <label>{w.project}<select required value={project} onChange={e=>setProject(e.target.value)}><option value="">—</option>{projects.map(p=><option key={p.id} value={p.id}>{p.label||p.name}</option>)}</select></label>
      <label>{w.policy}<select value={policy} onChange={e=>setPolicy(e.target.value as W.ProjectAssistantPermissionPolicy)}><option value="ask">{w.ask}</option><option value="auto_read">{w.auto_read}</option></select></label>
      <label>{w.instructions}<textarea value={instructions} onChange={e=>setInstructions(e.target.value)}/></label>
    </fieldset><p>{w.createHint}</p>{error&&<p role="alert">{w.unavailable} ({error})</p>}</div>
  </section>
}
