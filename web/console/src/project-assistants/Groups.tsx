import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import type {GroupRosterEntry} from "./roster-conversations"
import type * as W from "@clawdline/contract"
import { catalogWord, currentCatalogTag } from "../catalog"
import { authorized, command, errorCode, type SourceClient } from "./client"
import { canRead, canWrite } from "./view-state"
import { GroupRoom } from "./GroupRoom"
import { EventRefreshMs } from "./Chat"
import { retryableSourceError, shownSourceError } from "./source-errors"
import { rosterMood } from "./working"
import { BotAvatar } from "./BotAvatar"
import { GroupAvatar } from "./GroupAvatar"
import { ProjectMark } from "./ProjectMark"
import { PaSelect } from "./PaSelect"
import "./group-editor.css"
import {orderedGroupMembers} from "./group-editor-order"
import {afterSurfaceExit} from "./surface-motion"

export const groupWord=(key:string)=>catalogWord("projectAssistants",key)
export const sameGroupProject=(a:W.ProjectAssistantProjectScope,b:W.ProjectAssistantProjectScope)=>a.machine_id===b.machine_id&&a.project_id===b.project_id&&a.canonical_id===b.canonical_id&&a.kind===b.kind
const identity=(key:W.ProjectAssistantBotKey)=>JSON.stringify([key.machine_id,key.bot_id])
type GroupSource={source:SourceClient;groups:W.ProjectAssistantGroup[];status:W.ProjectAssistantSourceStatus}
type GroupRosterView={
  rows:readonly GroupRosterEntry[]
  open:(entry:GroupRosterEntry,opener?:HTMLElement)=>void
  selected:string|null
  dismiss:()=>void
  create?:()=>void
  content:ReactNode
}

/** Group entries live beside Bots, but open a room rather than a native chat. */
export function Groups({sources,items,projectNames,projectIcons,query="",machine="",selected:selectedBot,children,onOpenGroup,onSelectMember,onGroupOpenChange,groupRoute,onGroupBack}:{sources:readonly SourceClient[];items:readonly W.ProjectAssistantBotSummary[];projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;query?:string;machine?:string;selected?:string|null;onOpenGroup?:(key:W.ProjectAssistantBotKey)=>void;groupRoute?:string|null;onGroupBack?:()=>boolean;onSelectMember?:(item:W.ProjectAssistantBotSummary)=>void;onGroupOpenChange?:(open:boolean)=>void;children:(view:GroupRosterView)=>ReactNode}) {
  const [pages,setPages]=useState<GroupSource[]>([]),[error,setError]=useState(""),[creating,setCreating]=useState(false),[selected,setSelected]=useState<{source:SourceClient;group:W.ProjectAssistantGroup}|null>(null)
  const opener=useRef<HTMLElement|null>(null),originScroll=useRef<{element:HTMLElement;top:number}|null>(null)
  const restoreOrigin=useRef(false),reportedOpen=useRef<boolean|undefined>(undefined)
  const reportOpen=(value:boolean)=>{
    if(reportedOpen.current===value)return
    reportedOpen.current=value
    onGroupOpenChange?.(value)
  }
  const open=(entry:Pick<GroupRosterEntry,"source"|"group">,target?:HTMLElement)=>{
    restoreOrigin.current=false
    opener.current=target??(document.activeElement instanceof HTMLElement?document.activeElement:null)
    const scroll=opener.current?.closest<HTMLElement>(".pa-roster-scroll")
    originScroll.current=scroll?{element:scroll,top:scroll.scrollTop}:null
    onOpenGroup?.(entry.group.key)
    setSelected(entry)
  }
  const dismiss=()=>{
    if(!selected)return
    setSelected(null)
    // Clear the member return route before the roster records the next Bot.
    reportOpen(false)
  }
  const close=()=>{if(onGroupBack?.())return;restoreOrigin.current=true;dismiss()}
  useLayoutEffect(()=>{
    if(selected||!restoreOrigin.current)return
    restoreOrigin.current=false
    if(opener.current?.isConnected)opener.current.focus({preventScroll:true})
    if(originScroll.current?.element.isConnected)originScroll.current.element.scrollTop=originScroll.current.top
  },[selected])
  const selectedLive=!!selected&&sources.includes(selected.source)&&selected.source.current?.()!==false
  useLayoutEffect(()=>{reportOpen(selectedLive)},[selectedLive,onGroupOpenChange])
  const live=useRef(sources);live.current=sources
  const current=()=>live.current
  const [revision,refresh]=useState(0)
  const supported=sources.filter(s=>s.canGroups?.()&&s.groups)
  useEffect(()=>{
    const control=new AbortController()
    const load=async()=>{
      await Promise.all(supported.map(async source=>{
        try {
          const page=await authorized(source,current,signal=>source.groups!({machine_id:source.machineID},signal),control.signal)
          if(!control.signal.aborted)setPages(old=>[...old.filter(p=>p.source!==source),{source,groups:page.groups.filter(g=>!g.deleted),status:page.source}])
          if(!control.signal.aborted)setError("")
        }catch(e){if(!control.signal.aborted){setPages(old=>old.filter(p=>p.source!==source));setError(errorCode(e))}}
      }))
    }
    void load();const timer=setInterval(()=>void load(),EventRefreshMs)
    return()=>{control.abort();clearInterval(timer)}
  },[sources,revision])
  const active=pages.filter(p=>sources.includes(p.source)&&p.source.current?.()!==false)
  useLayoutEffect(()=>{
    if(groupRoute===undefined||groupRoute===(selected?identity(selected.group.key):null))return
    if(groupRoute===null){
      restoreOrigin.current=true
      setSelected(null)
      return
    }
    // Forward can restore only a room on a source that is still readable.
    for(const page of active){
      const group=canRead(page.status)?page.groups.find(group=>identity(group.key)===groupRoute):undefined
      if(!group)continue
      if(opener.current?.dataset.groupKey!==groupRoute){opener.current=null;originScroll.current=null}
      setSelected({source:page.source,group})
      return
    }
    if(selected)setSelected(null)
  },[groupRoute,pages,sources,selected])
  const canCreate=active.some(p=>canWrite(p.status)&&items.some(b=>b.bot.key.machine_id===p.source.machineID))
  const rows=active.flatMap(p=>p.groups.filter(g=>(!machine||g.key.machine_id===machine)&&(!query||[g.name,projectNames.get(JSON.stringify([g.key.machine_id,g.project.project_id]))??""].some(text=>text.toLocaleLowerCase().includes(query.toLocaleLowerCase())))).map(group=>({source:p.source,group,status:p.status})))
  const content=<>
    {shownSourceError(error)&&!retryableSourceError(error)&&<p role="status">{groupWord("groupUnavailable")}</p>}
    {creating&&<GroupEditor sources={active} bots={items.map(i=>i.bot)} projectNames={projectNames} projectIcons={projectIcons} current={current} onClose={()=>setCreating(false)} onSaved={(source,group)=>{setCreating(false);open({source,group});refresh(n=>n+1)}}/>}
    {selected&&selectedLive&&<GroupRoom key={identity(selected.group.key)} source={selected.source} current={current} group={selected.group} bots={items.map(i=>i.bot)} projectNames={projectNames} active={!selectedBot} onOpenMember={onSelectMember?key=>{
      if(!sources.includes(selected.source)||selected.source.current?.()===false||key.machine_id!==selected.source.machineID)return
      const item=items.find(item=>identity(item.bot.key)===identity(key)&&canRead(item.source))
      if(item)onSelectMember(item)
    }:undefined} onClose={close} onChanged={group=>{if(group.deleted)close();else setSelected({...selected,group});refresh(n=>n+1)}}/>}
    <TeamApprovals sources={sources} bots={items.filter(i=>rosterMood(i)!=="idle").map(i=>i.bot).filter(bot=>identity(bot.key)!==selectedBot)}/>
  </>
  return children({rows,open,selected:selectedLive&&!selectedBot?identity(selected.group.key):null,dismiss,create:canCreate?()=>setCreating(true):undefined,content})
}

// Match the existing group-name limit in internal/domain/projectassistant.
const GroupNameRunes=128

export function GroupEditor({sources,bots,projectNames,projectIcons,current,group,addMembers=false,onSaved,onClose}:{sources:readonly GroupSource[];bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;current:()=>readonly SourceClient[];group?:W.ProjectAssistantGroup;addMembers?:boolean;onSaved:(source:SourceClient,group:W.ProjectAssistantGroup)=>void;onClose:()=>void}) {
  const dialog=useRef<HTMLDialogElement>(null)
  const opener=useRef<HTMLElement|null>(null)
  const [closing,setClosing]=useState<(()=>void)|null>(null)
  useLayoutEffect(()=>{
    const node=dialog.current!
    opener.current=document.activeElement instanceof HTMLElement?document.activeElement:null
    bindNativeModalBackdrop(node)
    node.showModal()
    return()=>{if(node.open){node.close();opener.current?.focus({preventScroll:true})}}
  },[])
  useLayoutEffect(()=>{
    const node=dialog.current
    if(!closing||!node)return
    return afterSurfaceExit(node,()=>{
      node.close()
      opener.current?.focus({preventScroll:true})
      closing()
    })
  },[closing])
  const choices=bots.filter(b=>sources.some(s=>s.source.machineID===b.key.machine_id&&canWrite(s.status))).filter((b,i,all)=>all.findIndex(other=>sameGroupProject(b.project,other.project))===i)
  const [project,setProject]=useState(group?.project??choices[0]?.project),[name,setName]=useState(group?.name??""),[members,setMembers]=useState<string[]>(group?.members.map(m=>identity(m.key))??[]),[busy,setBusy]=useState(false),[error,setError]=useState(""),[unknown,setUnknown]=useState(false)
  const draft = JSON.stringify([project, name, members])
  const initialDraft = useRef(draft)
  const pending=useRef<{fingerprint:string;request:W.ProjectAssistantGroupCreateRequest|W.ProjectAssistantGroupEditRequest}|null>(null)
  const source=sources.find(s=>s.source.machineID===project?.machine_id&&canWrite(s.status))?.source
  const eligible=bots.filter(b=>project&&sameGroupProject(b.project,project))
  const complete=members.every(id=>eligible.some(b=>identity(b.key)===id))
  const existing=group?.members.map(member=>member.key)??[]
  const selection=orderedGroupMembers(existing,members,eligible.map(bot=>bot.key))
  const memberNames=selection.flatMap(key=>{const bot=eligible.find(bot=>identity(bot.key)===identity(key));return bot?[bot.name]:[]})
  const defaultName=Array.from(new Intl.ListFormat(currentCatalogTag(),{type:"unit"}).format(memberNames)).slice(0,GroupNameRunes).join("").trimEnd()
  const savedName=name.trim()||(group?"":defaultName)
  const canSave=!!source&&!!project&&!busy&&!unknown&&!closing&&!!savedName&&selection.length>0&&complete&&(!addMembers||selection.length>existing.length)
  const formTitle=groupWord(addMembers?"groupMembers":group?"groupEdit":"groupCreate")
  const close=()=>{if(!busy&&!closing)setClosing(()=>onClose)}
  async function save(){
    if(!source||!project||!canSave)return
    const settings={name:savedName,members:selection}
    const fingerprint=JSON.stringify([project,settings])
    let request=pending.current?.fingerprint===fingerprint?pending.current.request:null
    if(!request){const key=group?.key??{machine_id:project.machine_id,bot_id:crypto.randomUUID()};request=group?{command:command(key,"edit"),settings:{...settings,expected_version:group.version}}:{command:command(key,"create"),settings:{...settings,target:{machine_id:project.machine_id,project_id:project.project_id}}};pending.current={fingerprint,request}}
    setBusy(true);setError("")
    try {
      const answer=await authorized(source,current,signal=>group?source.editGroup!(request as W.ProjectAssistantGroupEditRequest,signal):source.createGroup!(request as W.ProjectAssistantGroupCreateRequest,signal),new AbortController().signal)
      setClosing(()=>()=>onSaved(source,answer.group))
    }catch(e){const code=errorCode(e);setError(code);if(code==="pa_outcome_unknown")setUnknown(true)}finally{setBusy(false)}
  }
  return <dialog data-modal-dismiss={String(!busy&&!unknown&&!closing&&draft===initialDraft.current)} data-closing={!!closing||undefined} ref={dialog} className="pa-form-panel pa-group-editor" aria-label={formTitle}
    onCancel={event=>{event.preventDefault();close()}} onKeyDown={event=>{if(event.key==="Escape")event.stopPropagation()}}>
    <form className="pa-group-form" inert={!!closing||undefined} onSubmit={event=>{event.preventDefault();void save()}}>
      <header className="pa-toolbar"><button type="button" disabled={busy} onClick={close}>{groupWord("groupCancel")}</button><h2>{formTitle}</h2><button type="submit" disabled={!canSave}>{groupWord(busy?"groupSaving":group?"groupSave":"createSubmit")}</button></header>
      <div className="pa-form-body">
        <fieldset disabled={busy||unknown}>
          <div className="pa-group-form-identity">
            <GroupAvatar keys={selection} bots={eligible} size={88}/>
            {addMembers?<strong>{group?.name}</strong>:<label>{groupWord("groupName")}<input value={name} placeholder={!group?defaultName:undefined} onChange={e=>setName(e.target.value)}
              onKeyDown={event=>{if(event.key==="Enter"&&(event.nativeEvent.isComposing||event.keyCode===229))event.preventDefault()}}/></label>}
          </div>
          {!group&&<PaSelect label={groupWord("groupProject")} value={project?JSON.stringify(project):""} onChange={val=>{setProject(JSON.parse(val));setMembers([])}} options={choices.map(b=>{
            const keyStr=JSON.stringify([b.key.machine_id,b.project.project_id]),source=sources.find(s=>s.source.machineID===b.key.machine_id)?.source
            const pName=projectNames.get(keyStr)??b.project.project_id,icon=projectIcons?.get(keyStr)
            const machineSuffix=sources.length>1?` · ${source?.machineName??b.key.machine_id}`:""
            return {value:JSON.stringify(b.project),label:`${pName}${machineSuffix}`,icon:<ProjectMark icon={icon} size={16}/>}
          })}/>}
          <fieldset className="pa-group-choice-field">
            <legend>{groupWord("groupMembers")}</legend>
            <div className="pa-group-choices">{eligible.map(bot=>{
              const id=identity(bot.key),checked=members.includes(id)
              const fixed=addMembers&&existing.some(key=>identity(key)===id)
              return <label className="pa-group-choice" key={id}>
                <input type="checkbox" disabled={fixed} checked={checked} onChange={e=>setMembers(old=>e.target.checked?[...old,id]:old.filter(key=>key!==id))}/>
                <span className="pa-group-choice-face"><BotAvatar appearance={bot.appearance} size={42}/><span className="pa-group-choice-check" aria-hidden="true">✓</span></span>
                <span className="pa-group-choice-name" title={bot.name}>{bot.name}</span>
              </label>
            })}</div>
          </fieldset>
        </fieldset>
        {!group&&<p>{groupWord("groupSameProject")}</p>}
        {!complete&&<p role="status">{groupWord("groupMembersMissing")}</p>}
        {error&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} ({error})</p>}
      </div>
    </form>
  </dialog>
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
