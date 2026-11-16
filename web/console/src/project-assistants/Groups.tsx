import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { catalogWord } from "../catalog"
import { authorized, command, errorCode, type SourceClient } from "./client"
import { canWrite } from "./view-state"
import { GroupRoom } from "./GroupRoom"
import { EventRefreshMs } from "./Chat"
import { rosterMood } from "./working"

export const groupWord=(key:string)=>catalogWord("projectAssistants",key)
export const sameGroupProject=(a:W.ProjectAssistantProjectScope,b:W.ProjectAssistantProjectScope)=>a.machine_id===b.machine_id&&a.project_id===b.project_id&&a.canonical_id===b.canonical_id&&a.kind===b.kind
const identity=(key:W.ProjectAssistantBotKey)=>JSON.stringify([key.machine_id,key.bot_id])
type GroupSource={source:SourceClient;groups:W.ProjectAssistantGroup[];status:W.ProjectAssistantSourceStatus}

/** Group entries live beside Bots, but open a room rather than a native chat. */
export function Groups({sources,items,projectNames,query="",machine="",selected:selectedBot}:{sources:readonly SourceClient[];items:readonly W.ProjectAssistantBotSummary[];projectNames:ReadonlyMap<string,string>;query?:string;machine?:string;selected?:string|null}) {
  const [pages,setPages]=useState<GroupSource[]>([]),[error,setError]=useState(""),[creating,setCreating]=useState(false),[selected,setSelected]=useState<{source:SourceClient;group:W.ProjectAssistantGroup}|null>(null)
  const live=useRef(sources);live.current=sources
  const current=()=>live.current
  const [revision,refresh]=useState(0)
  const supported=sources.filter(s=>s.canGroups?.()&&s.groups)
  useEffect(()=>{
    const control=new AbortController()
    const load=async()=>{
      await Promise.all(supported.map(async source=>{
        try {
          const page=await authorized(source,current,async signal=>{await source.status(signal);return source.groups!({machine_id:source.machineID},signal)},control.signal)
          if(!control.signal.aborted)setPages(old=>[...old.filter(p=>p.source!==source),{source,groups:page.groups.filter(g=>!g.deleted),status:page.source}])
          if(!control.signal.aborted)setError("")
        }catch(e){if(!control.signal.aborted){setPages(old=>old.filter(p=>p.source!==source));setError(errorCode(e))}}
      }))
    }
    void load();const timer=setInterval(()=>void load(),EventRefreshMs)
    return()=>{control.abort();clearInterval(timer)}
  },[sources,revision])
  const active=pages.filter(p=>sources.includes(p.source)&&p.source.current?.()!==false)
  const canCreate=active.some(p=>canWrite(p.status)&&items.some(b=>b.bot.key.machine_id===p.source.machineID))
  const rows=active.flatMap(p=>p.groups.filter(g=>(!machine||g.key.machine_id===machine)&&(!query||g.name.toLocaleLowerCase().includes(query.toLocaleLowerCase()))).map(group=>({source:p.source,group})))
  if(!supported.length)return null
  return <section aria-label={groupWord("groups")} className="pa-project-group">
    <div className="pa-toolbar"><h2>{groupWord("groups")}</h2>{canCreate&&<button type="button" onClick={()=>setCreating(true)}>{groupWord("groupCreate")}</button>}</div>
    {rows.map(({source,group})=><button type="button" className="pa-row" key={identity(group.key)} data-group-key={identity(group.key)} onClick={()=>setSelected({source,group})}><span aria-hidden="true">◎</span><span><strong>{group.name}</strong><small>{projectNames.get(JSON.stringify([group.key.machine_id,group.project.project_id]))??groupWord("groupProject")} · {group.members.length} {groupWord("groupMembers")}</small></span></button>)}
    {error&&<p role="status">{groupWord("groupUnavailable")} ({error})</p>}
    {creating&&<GroupEditor sources={active} bots={items.map(i=>i.bot)} projectNames={projectNames} current={current} onClose={()=>setCreating(false)} onSaved={(source,group)=>{setCreating(false);setSelected({source,group});refresh(n=>n+1)}}/>}
    {selected&&sources.includes(selected.source)&&selected.source.current?.()!==false&&<GroupRoom source={selected.source} current={current} group={selected.group} bots={items.map(i=>i.bot)} projectNames={projectNames} onClose={()=>setSelected(null)} onChanged={group=>{setSelected(group.deleted?null:{...selected,group});refresh(n=>n+1)}}/>}
    <TeamApprovals sources={sources} bots={items.filter(i=>rosterMood(i)!=="idle").map(i=>i.bot).filter(bot=>identity(bot.key)!==selectedBot)}/>
  </section>
}

export function GroupEditor({sources,bots,projectNames,current,group,onSaved,onClose}:{sources:readonly GroupSource[];bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;current:()=>readonly SourceClient[];group?:W.ProjectAssistantGroup;onSaved:(source:SourceClient,group:W.ProjectAssistantGroup)=>void;onClose:()=>void}) {
  const choices=bots.filter(b=>sources.some(s=>s.source.machineID===b.key.machine_id&&canWrite(s.status))).filter((b,i,all)=>all.findIndex(other=>sameGroupProject(b.project,other.project))===i)
  const [project,setProject]=useState(group?.project??choices[0]?.project),[name,setName]=useState(group?.name??""),[members,setMembers]=useState<string[]>(group?.members.map(m=>identity(m.key))??[]),[busy,setBusy]=useState(false),[error,setError]=useState(""),[unknown,setUnknown]=useState(false)
  const pending=useRef<{fingerprint:string;request:W.ProjectAssistantGroupCreateRequest|W.ProjectAssistantGroupEditRequest}|null>(null)
  const source=sources.find(s=>s.source.machineID===project?.machine_id&&canWrite(s.status))?.source
  const eligible=bots.filter(b=>project&&sameGroupProject(b.project,project))
  const complete=members.every(id=>eligible.some(b=>identity(b.key)===id))
  async function save(){
    if(!source||!project||busy||unknown)return
    const settings={name:name.trim(),members:eligible.filter(b=>members.includes(identity(b.key))).map(b=>b.key)}
    const fingerprint=JSON.stringify([project,settings])
    let request=pending.current?.fingerprint===fingerprint?pending.current.request:null
    if(!request){const key=group?.key??{machine_id:project.machine_id,bot_id:crypto.randomUUID()};request=group?{command:command(key,"edit"),settings:{...settings,expected_version:group.version}}:{command:command(key,"create"),settings:{...settings,target:{machine_id:project.machine_id,project_id:project.project_id}}};pending.current={fingerprint,request}}
    setBusy(true);setError("")
    try {
      const answer=await authorized(source,current,signal=>group?source.editGroup!(request as W.ProjectAssistantGroupEditRequest,signal):source.createGroup!(request as W.ProjectAssistantGroupCreateRequest,signal),new AbortController().signal)
      onSaved(source,answer.group)
    }catch(e){const code=errorCode(e);setError(code);if(code==="pa_outcome_unknown")setUnknown(true)}finally{setBusy(false)}
  }
  return <section className="pa-form-panel" role="dialog" aria-modal="true" aria-label={groupWord(group?"groupEdit":"groupCreate")}>
    <header className="pa-toolbar"><button type="button" onClick={onClose}>{groupWord("groupCancel")}</button><h2>{groupWord(group?"groupEdit":"groupCreate")}</h2><button type="button" disabled={!source||busy||unknown||!name.trim()||!members.length||!complete} onClick={()=>void save()}>{groupWord(busy?"groupSaving":"groupSave")}</button></header>
    <div className="pa-form-body"><fieldset disabled={busy||unknown}>
      <label>{groupWord("groupName")}<input value={name} onChange={e=>setName(e.target.value)}/></label>
      <label>{groupWord("groupProject")}<select disabled={!!group} value={project?JSON.stringify(project):""} onChange={e=>{setProject(JSON.parse(e.target.value));setMembers([])}}>{choices.map(b=><option key={JSON.stringify(b.project)} value={JSON.stringify(b.project)}>{projectNames.get(JSON.stringify([b.key.machine_id,b.project.project_id]))??b.project.project_id}{sources.length>1?` · ${sources.find(s=>s.source.machineID===b.key.machine_id)?.source.machineName??b.key.machine_id}`:""}</option>)}</select></label>
      <fieldset><legend>{groupWord("groupMembers")}</legend>{eligible.map(bot=><label key={identity(bot.key)} style={{display:"flex",gap:8,alignItems:"center"}}><input type="checkbox" checked={members.includes(identity(bot.key))} onChange={e=>setMembers(old=>e.target.checked?[...old,identity(bot.key)]:old.filter(k=>k!==identity(bot.key)))}/>{bot.name}</label>)}</fieldset>
    </fieldset><p>{groupWord("groupSameProject")}</p>{!complete&&<p role="status">{groupWord("groupMembersMissing")}</p>}{error&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} ({error})</p>}</div>
  </section>
}

/** Pending proposals remain visible on read-only devices; only a live human
 * write grant can answer them. No UI answer becomes a native approval option. */
export function TeamApprovals({sources,bots}:{sources:readonly SourceClient[];bots:readonly W.ProjectAssistantBot[]}) {
  const [cards,setCards]=useState<{source:SourceClient;plan:W.ProjectAssistantTeamPlan;writable:boolean}[]>([]),[busy,setBusy]=useState<string[]>([]),[errors,setErrors]=useState<Record<string,string>>({}),[unknown,setUnknown]=useState<string[]>([])
  const live=useRef(sources);live.current=sources
  const current=()=>live.current
  const managers=bots.filter(b=>b.manage_team&&sources.some(s=>s.machineID===b.key.machine_id&&s.canGroups?.()&&s.teamPlans))
  const ids=managers.map(b=>JSON.stringify([b.key,b.version])).join("|")
  useEffect(()=>{
    if(!managers.length){setCards([]);return}
    const control=new AbortController()
    const load=async()=>{
      const statuses=new Map<SourceClient,Promise<W.ProjectAssistantSourcesResponse>>()
      const out=(await Promise.all(managers.map(async bot=>{
        const source=sources.find(s=>s.machineID===bot.key.machine_id)!
        try {const result=await authorized(source,current,async signal=>{let pending=statuses.get(source);if(!pending){pending=source.status(signal);statuses.set(source,pending)}const status=await pending;const page=await source.teamPlans!({key:bot.key},signal);return {page,writable:status.sources.some(s=>s.machine_id===source.machineID&&canWrite(s))}},control.signal);return result.page.plans.map(plan=>({source,plan,writable:result.writable}))}catch{return []}
      }))).flat()
      if(!control.signal.aborted)setCards(out)
    }
    void load();const timer=setInterval(()=>void load(),EventRefreshMs)
    return()=>{control.abort();clearInterval(timer)}
  },[sources,ids])
  async function answer(source:SourceClient,plan:W.ProjectAssistantTeamPlan,allow:boolean){
    setBusy(old=>[...old,plan.id])
    try {await authorized(source,current,signal=>source.answerTeamPlan!({command:command(plan.manager,"answer"),plan_id:plan.id,allow},signal),new AbortController().signal);setCards(old=>old.filter(c=>c.plan.id!==plan.id))}
    catch(e){const code=errorCode(e);setErrors(old=>({...old,[plan.id]:code}));if(code==="pa_outcome_unknown")setUnknown(old=>[...old,plan.id])}
    finally{setBusy(old=>old.filter(id=>id!==plan.id))}
  }
  return <>{cards.filter(c=>sources.includes(c.source)&&c.source.current?.()!==false).map(({source,plan,writable})=><article className="pa-permission" key={plan.id} aria-label={groupWord("teamApproval")}><h3>{plan.manager_name} · {groupWord(plan.operation==="create"?"teamCreateProposal":"teamEditProposal")}</h3><p>{groupWord("teamApprovalHint")}</p><pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere",fontSize:12}}>{plan.detail}</pre><small>{groupWord("teamExpires")} {new Date(plan.expires_at).toLocaleTimeString()}</small><div className="pa-chat-tools"><button type="button" disabled={!writable||busy.includes(plan.id)||unknown.includes(plan.id)} onClick={()=>void answer(source,plan,false)}>{groupWord("teamDecline")}</button><button type="button" disabled={!writable||busy.includes(plan.id)||unknown.includes(plan.id)} onClick={()=>void answer(source,plan,true)}>{groupWord(busy.includes(plan.id)?"teamApplying":"teamAccept")}</button></div>{errors[plan.id]&&<p role="alert">{groupWord(unknown.includes(plan.id)?"groupUnknown":"groupUnavailable")} ({errors[plan.id]})</p>}</article>)}</>
}
