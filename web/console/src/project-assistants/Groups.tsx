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
export const sameGroupProject=(a:W.ProjectAssistantProjectScope,b:W.ProjectAssistantProjectScope)=>a.machine_id===b.machine_id&&a.kind===b.kind&&(a.kind==="personal"||a.project_id===b.project_id&&a.canonical_id===b.canonical_id)
const identity=(key:W.ProjectAssistantBotKey)=>JSON.stringify([key.machine_id,key.bot_id])
type GroupSource={source:SourceClient;groups:W.ProjectAssistantGroup[];status:W.ProjectAssistantSourceStatus}
function canStartGroup(bot:W.ProjectAssistantBot,sources:readonly GroupSource[]) {
  return !bot.hidden&&sources.some(({source,status})=>source.machineID===bot.key.machine_id&&canWrite(status)&&(bot.project.kind!=="personal"||source.canPersonalGroups?.()))
}
const groupProjectKey=(project:W.ProjectAssistantProjectScope)=>JSON.stringify(project.kind==="personal"?[project.machine_id,project.kind]:[project.machine_id,project.kind,project.project_id,project.canonical_id])
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
  const bots=items.filter(item=>canRead(item.source)).map(item=>item.bot)
  const canCreate=bots.some(bot=>canStartGroup(bot,active))
  const rows=active.flatMap(p=>p.groups.filter(g=>(!machine||g.key.machine_id===machine)&&(!query||[g.name,projectNames.get(JSON.stringify([g.key.machine_id,g.project.project_id]))??""].some(text=>text.toLocaleLowerCase().includes(query.toLocaleLowerCase())))).map(group=>({source:p.source,group,status:p.status})))
  const content=<>
    {shownSourceError(error)&&!retryableSourceError(error)&&<p role="status">{groupWord("groupUnavailable")}</p>}
    {creating&&<GroupEditor sources={active} bots={bots} projectNames={projectNames} projectIcons={projectIcons} current={current} onClose={()=>setCreating(false)} onSaved={(source,group)=>{setCreating(false);open({source,group});refresh(n=>n+1)}}/>}
    {selected&&selectedLive&&<GroupRoom key={identity(selected.group.key)} source={selected.source} current={current} group={selected.group} bots={bots} projectNames={projectNames} active={!selectedBot} onOpenMember={onSelectMember?key=>{
      if(!sources.includes(selected.source)||selected.source.current?.()===false||key.machine_id!==selected.source.machineID)return
      const item=items.find(item=>identity(item.bot.key)===identity(key)&&canRead(item.source))
      if(item)onSelectMember(item)
    }:undefined} onClose={close} onChanged={group=>{if(group.deleted)close();else setSelected({...selected,group});refresh(n=>n+1)}}/>}
    <TeamApprovals sources={sources.filter(source=>!source.canTeamCards?.())} bots={items.filter(i=>rosterMood(i)!=="idle").map(i=>i.bot).filter(bot=>identity(bot.key)!==selectedBot)}/>
  </>
  return children({rows,open,selected:selectedLive&&!selectedBot?identity(selected.group.key):null,dismiss,create:canCreate?()=>setCreating(true):undefined,content})
}

// Match the existing group-name limit in internal/domain/projectassistant.
const GroupNameRunes=128
// The source's MaxGroupDescriptionRunes, counted in characters as it counts them.
const GroupDescriptionRunes=500

export function GroupEditor({sources,bots,projectNames,projectIcons,current,group,addMembers=false,onSaved,onClose,onDelete}:{onDelete?:()=>void;sources:readonly GroupSource[];bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;current:()=>readonly SourceClient[];group?:W.ProjectAssistantGroup;addMembers?:boolean;onSaved:(source:SourceClient,group:W.ProjectAssistantGroup)=>void;onClose:()=>void}) {
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
  const choices=new Map(bots.filter(bot=>canStartGroup(bot,sources)).map(({project})=>[groupProjectKey(project),project]))
  const [projectKey,setProjectKey]=useState(choices.keys().next().value),[name,setName]=useState(group?.name??""),[members,setMembers]=useState<string[]>(group?.members.map(m=>identity(m.key))??[]),[busy,setBusy]=useState(false),[error,setError]=useState(""),[unknown,setUnknown]=useState(false)
  const project=group?.project??(projectKey===undefined?choices.values().next().value:choices.get(projectKey))
  // Codync's member list: who is in, a remove button each, then a search to add more.
  const [adding,setAdding]=useState(addMembers||!group),[query,setQuery]=useState("")
  // Codync's "What this group works on": sent only to a source that stores it.
  const [about,setAbout]=useState(group?.description??"")
  const aboutTooLong=Array.from(about.trim()).length>GroupDescriptionRunes
  const draft = JSON.stringify([projectKey, name, members, about])
  const initialDraft = useRef(draft)
  const pending=useRef<{fingerprint:string;request:W.ProjectAssistantGroupCreateRequest|W.ProjectAssistantGroupEditRequest}|null>(null)
  const source=sources.find(s=>s.source.machineID===project?.machine_id&&canWrite(s.status))?.source
  const existing=group?.members.map(member=>member.key)??[]
  const eligible=bots.filter(bot=>project&&sameGroupProject(bot.project,project)&&(!bot.hidden||existing.some(key=>identity(key)===identity(bot.key))))
  const complete=members.every(id=>eligible.some(b=>identity(b.key)===id))
  const selection=orderedGroupMembers(existing,members,eligible.map(bot=>bot.key))
  const memberNames=selection.flatMap(key=>{const bot=eligible.find(bot=>identity(bot.key)===identity(key));return bot?[bot.name]:[]})
  const defaultName=Array.from(new Intl.ListFormat(currentCatalogTag(),{type:"unit"}).format(memberNames)).slice(0,GroupNameRunes).join("").trimEnd()
  const savedName=name.trim()||(group?"":defaultName)
  const canSave=!aboutTooLong&&!!source&&!!project&&!busy&&!unknown&&!closing&&!!savedName&&selection.length>0&&complete&&(!addMembers||selection.length>existing.length)
  const picked=selection.flatMap(key=>eligible.filter(bot=>identity(bot.key)===identity(key)))
  const needle=query.trim().toLocaleLowerCase()
  const candidates=eligible.filter(bot=>!members.includes(identity(bot.key))&&(!needle||bot.name.toLocaleLowerCase().includes(needle)))
  const toggle=(id:string)=>{setMembers(old=>old.includes(id)?old.filter(key=>key!==id):[...old,id]);setQuery("")}
  const named=(key:string,value:string)=>groupWord(key).replace("{name}",value)
  const formTitle=groupWord(addMembers?"groupMembers":group?"groupEdit":"groupCreate")
  const close=()=>{if(!busy&&!closing)setClosing(()=>onClose)}
  async function save(){
    if(!source||!project||!canSave)return
    const describes=!!source.canGroupDescription?.(),text=about.trim()
    const edited=describes&&!addMembers&&text!==(group?.description??"")
    const settings={name:savedName,members:selection}
    const target={machine_id:project.machine_id,project_id:project.kind==="personal"?"":project.project_id}
    const fingerprint=JSON.stringify([target,settings,edited||!group?text:null])
    let request=pending.current?.fingerprint===fingerprint?pending.current.request:null
    if(!request){const key=group?.key??{machine_id:project.machine_id,bot_id:crypto.randomUUID()};request=group?{command:command(key,"edit"),settings:{...settings,expected_version:group.version,...(edited?{description:{text}}:{})}}:{command:command(key,"create"),settings:{...settings,target,...(describes&&text?{description:text}:{})}};pending.current={fingerprint,request}}
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
            <GroupAvatar keys={selection} bots={eligible} size={84}/>
            {addMembers?<strong className="pa-group-name-fixed">{group?.name}</strong>:<input className="pa-group-name" aria-label={groupWord("groupName")} value={name} placeholder={!group&&defaultName?defaultName:groupWord("groupName")} onChange={e=>setName(e.target.value)}
              onKeyDown={event=>{if(event.key==="Enter"&&(event.nativeEvent.isComposing||event.keyCode===229))event.preventDefault()}}/>}
            {addMembers?group?.description&&<p className="pa-group-about-fixed">{group.description}</p>
              :sources.find(s=>s.source.machineID===project?.machine_id)?.source.canGroupDescription?.()&&<textarea className="pa-group-about" rows={1} value={about} aria-invalid={aboutTooLong||undefined}
                aria-label={groupWord("groupAbout")} placeholder={groupWord("groupAboutPlaceholder")} onChange={e=>setAbout(e.target.value)}/>}
            {aboutTooLong&&<p className="pa-group-note" role="alert">{groupWord("groupAboutTooLong").replace("{count}",String(GroupDescriptionRunes))}</p>}
          </div>
          {!group&&<PaSelect label={groupWord("groupProject")} value={project?groupProjectKey(project):""} onChange={val=>{setProjectKey(val);setMembers([])}} options={[...choices].map(([value,choice])=>{
            const keyStr=JSON.stringify([choice.machine_id,choice.project_id]),source=sources.find(s=>s.source.machineID===choice.machine_id)?.source
            const pName=choice.kind==="personal"?groupWord("groupPersonalWorkspace"):projectNames.get(keyStr)??choice.project_id,icon=projectIcons?.get(keyStr)
            const machineSuffix=sources.length>1?` · ${source?.machineName??choice.machine_id}`:""
            return {value,label:`${pName}${machineSuffix}`,icon:<ProjectMark icon={icon} size={16}/>}
          })}/>}
          <section className="pa-group-members" aria-label={groupWord("groupMembers")}>
            <h3>{groupWord("groupMembers")}</h3>
            {picked.length>0&&<ul>{picked.map(bot=>{
              const id=identity(bot.key),fixed=addMembers&&existing.some(key=>identity(key)===id)
              return <li className="pa-group-member-row" key={id}><BotAvatar appearance={bot.appearance} size={32}/><span>{bot.name}</span>
                {!fixed&&<button type="button" className="pa-group-row-icon" aria-label={named("groupRemoveMember",bot.name)} title={named("groupRemoveMember",bot.name)} onClick={()=>toggle(id)}>×</button>}</li>
            })}</ul>}
            {adding?<>
              {(candidates.length>0||!!needle)&&<input className="pa-group-search" type="search" value={query} aria-label={groupWord("groupSearchBots")} placeholder={groupWord("groupSearchBots")} onChange={e=>setQuery(e.target.value)}
                onKeyDown={event=>{
                  const composing=event.nativeEvent.isComposing||event.keyCode===229
                  if(event.key==="Enter"){event.preventDefault();if(!composing&&candidates[0])toggle(identity(candidates[0].key))}
                  if(event.key==="Escape"&&!composing&&group){event.preventDefault();event.stopPropagation();setAdding(false);setQuery("")}
                }}/>}
              {candidates.map(bot=><button type="button" key={identity(bot.key)} className="pa-group-member-row pa-group-candidate" aria-label={named("groupAddNamed",bot.name)} onClick={()=>toggle(identity(bot.key))}>
                <BotAvatar appearance={bot.appearance} size={32}/><span>{bot.name}</span><span className="pa-group-plus" aria-hidden="true">+</span></button>)}
              {!candidates.length&&needle&&<p className="pa-group-note">{groupWord("groupNoMatch")}</p>}
            </>:candidates.length>0&&<button type="button" className="pa-group-member-row pa-group-add" onClick={()=>setAdding(true)}><span className="pa-group-add-icon" aria-hidden="true">+</span><span>{groupWord("groupAddMember")}</span></button>}
            <p className="pa-group-note">{groupWord("groupMentionHint")}</p>
          </section>
        </fieldset>
        {!group&&<p className="pa-group-note">{groupWord("groupSameProject")}</p>}
        {!complete&&<p role="status">{groupWord("groupMembersMissing")}</p>}
        {error&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} ({error})</p>}
        {group&&!addMembers&&onDelete&&<button type="button" className="pa-group-delete" disabled={!source||busy||unknown||!!closing} onClick={()=>setClosing(()=>onDelete)}>{groupWord("groupDelete")}</button>}
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
