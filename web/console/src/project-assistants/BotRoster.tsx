import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react"
import type { ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { BotRow } from "./BotRow"
import { BotRosterReadAction } from "./BotRosterReadAction"
import { botIdentity } from "./view-state"
import type { Words } from "./words"
import { PaSelect,PaMenu } from "./PaSelect"
import { ProjectMark } from "./ProjectMark"
import { projectKey, pinnedGroupKey } from "./roster-groups"
import { LoadingSkeleton } from "../LoadingSkeleton"
import { useShowAfter } from "./PaLoading"
import type { SourceClient } from "./client"
import { Groups } from "./Groups"
import {GroupRosterRow} from "./GroupRosterRow"
import {conversations,conversationSections,type GroupRosterEntry} from "./roster-conversations"
import { useRosterDrag } from "./useRosterDrag"
import { moveRosterBot, pinRosterBot, reorderKeys, type RosterState } from "./roster-state"
import { useRosterState } from "./useRosterState"
import { RosterHandle, type RosterDrag } from "./RosterHandle"
import "./roster-controls.css"
export function BotRoster({rosterScope,items,machines,selected,query,onQuery,machine,onMachine,onSelect,onCreate,onClose,w,language,loading,errors,onMore,projectNames,projectIcons,status,machineNames,hidden,sources=[],current,readerID,onRead,groupsItems,onOpenGroup,onGroupOpenChange}: {
  onOpenGroup?:()=>void;onGroupOpenChange?:(open:boolean)=>void;rosterScope:string;items:W.ProjectAssistantBotSummary[];machines:readonly string[];selected:string|null;query:string;onQuery:(s:string)=>void;machine:string;onMachine:(s:string)=>void;
  onSelect:(i:W.ProjectAssistantBotSummary)=>void;onCreate?:()=>void;onClose:()=>void;w:Words;language:string;loading:boolean;
  current?:()=>readonly SourceClient[];readerID?:string;onRead?:(source:SourceClient)=>Promise<void>;
  errors:readonly [string,string,boolean][];onMore?:()=>void;projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;status?:ReactNode;machineNames?:ReadonlyMap<string,string>;hidden?:ReactNode;sources?:readonly SourceClient[];groupsItems?:readonly W.ProjectAssistantBotSummary[]
}) {
  const {state: rosterState, problem, update} = useRosterState(rosterScope)
  const root=useRef<HTMLElement>(null)
  const draft = useRef<RosterState|null>(null)
  const started=useRef(rosterState)
  const direct = useRef(false)
  const [preview,setPreview]=useState<RosterState|null>(null)
  const moves=useRef<(target:RosterDrag)=>void>(()=>{})
  const state=preview??rosterState
  const gesture=useRosterDrag(root,{
    start: (source,isDirect)=>{started.current=rosterState;direct.current=isDirect;draft.current=rosterState;setPreview(rosterState);setActive(source)},
    over: target=>moves.current(target),
    finish: commit=>{if(commit&&started.current===rosterState&&draft.current&&draft.current!==rosterState)update(draft.current);draft.current=null;setPreview(null);setActive(null);setOver(null)}
  })
  useEffect(()=>{if(draft.current&&started.current!==rosterState)gesture.cancel()},[rosterState])
  const [sorting,setSorting]=useState(false)
  const sortFocus=useRef<string|undefined>(undefined),restoreOptions=useRef(false)
  const [active, setActive] = useState<RosterDrag | null>(null), [over, setOver] = useState<RosterDrag | null>(null)
  const shown = items.filter(i => !machine || i.bot.key.machine_id === machine)
  const endDrag = () => gesture.cancel()
  const entryFor=(key:string)=>[...root.current?.querySelectorAll<HTMLElement>(".pa-roster-entry")??[]].find(row=>row.dataset.rosterDropKey===key)
  const pin=(key:string,pinned:boolean)=>{update(pinRosterBot(rosterState,key,pinned));requestAnimationFrame(()=>entryFor(key)?.querySelector<HTMLButtonElement>(".pa-bot-row")?.focus({preventScroll:true}))}
  const focusSortTarget=()=>{const target=sortFocus.current?entryFor(sortFocus.current):root.current;target?.querySelector<HTMLButtonElement>(".pa-roster-handle")?.focus({preventScroll:true})}
  const beginSorting=(key?:string)=>{sortFocus.current=key;if(sorting)focusSortTarget();else setSorting(true)}
  const finishSorting=()=>{endDrag();restoreOptions.current=true;setSorting(false)}
  // Focus belongs to the mode's commit, never a later frame that can steal a subsequent choice.
  useLayoutEffect(()=>{if(sorting)focusSortTarget();else if(restoreOptions.current){restoreOptions.current=false;root.current?.querySelector<HTMLButtonElement>(".pa-toolbar .pa-menu > button")?.focus({preventScroll:true})}},[sorting])
  // One machine needs no machine filter and no machine name on every row.
  const several=machines.length>1,named=(id:string)=>machineNames?.get(id)||w.thisMachine
  // The first read draws placeholder rows after 150ms; once Bots are listed, a later read only says so.
  const skeleton=useShowAfter(loading&&!items.length&&!errors.length)
  const renderRows=(roomEntries:readonly GroupRosterEntry[],openGroup:(entry:GroupRosterEntry,opener?:HTMLElement)=>void,selectedGroup:string|null)=>{
    const allRows=conversations(items,roomEntries),groups=conversationSections(conversations(shown,roomEntries),state)
    const drop = (target: RosterDrag, source: RosterDrag, currentState=rosterState, save=true) => {
      const apply=(next:RosterState)=>{if(save)update(next);else {draft.current=next;setPreview(next)}}
      if (!source || source.key === target.key) return
      if (source.kind === "project" && target.kind === "project" && target.key !== pinnedGroupKey) {
        const current = conversationSections(allRows,{...currentState,projectSortOrder:[],botSortOrder:[]}).filter(g => g.kind === "project").map(g => g.key)
        const full = [...currentState.projectSortOrder, ...current.filter(key => !currentState.projectSortOrder.includes(key))]
        apply({...currentState, projectSortOrder: reorderKeys(full, source.key, target.key)})
      } else if (source.kind === "bot") {
        const item = allRows.find(row => row.key === source.key)
        if (!item) return
        const group = target.kind === "bot" ? groups.find(g => g.items.some(i => i.key === target.key)) : groups.find(g => g.key === target.key)
        const pinned = target.key === pinnedGroupKey || group?.kind === "pinned"
        // Moving a row changes its presentation, never the Project it belongs to.
        if (!pinned && group?.key !== projectKey(item.machineID, item.projectID)) return
        if (target.kind === "bot") apply(moveRosterBot(currentState, source.key, target.key, pinned, allRows.map(row=>row.key)))
        else apply(pinRosterBot(currentState, source.key, pinned))
      }
    }
    moves.current=target=>{
      if(!active||!draft.current||active.key===target.key)return
      if(direct.current){
        const from=groups.find(g=>g.items.some(i=>i.key===active.key)),to=groups.find(g=>g.items.some(i=>i.key===target.key))
        if(target.kind!=="bot"||!from||from!==to)return
      }
      setOver(target);drop(target,active,draft.current,false)
    }
    const keyboard = (source: RosterDrag, keys: string[]) => (event: KeyboardEvent<HTMLElement>) => {
      if (event.key === "Escape") {endDrag(); return}
      const index = keys.indexOf(source.key)
      const next = event.key === "ArrowUp" ? index - 1 : event.key === "ArrowDown" ? index + 1 : event.key === "Home" ? 0 : event.key === "End" ? keys.length - 1 : -1
      if (next < 0 || next >= keys.length || index < 0) return
      event.preventDefault(); drop({kind: source.kind, key: keys[next]}, source)
    }
    const targetProps = (target: RosterDrag) => ({
      "data-roster-drop-kind": target.kind, "data-roster-drop-key": target.key,
      "data-dragging": active?.kind === target.kind && active.key === target.key || undefined,
      "data-drop": over?.kind === target.kind && over.key === target.key || undefined
    })
    const handle = (source: RosterDrag, name: string, keys: string[]) => <RosterHandle label={`${w.rosterReorder}: ${name}`} hint={w.rosterReorderHint} onKey={keyboard(source, keys)}/>
    return <>
      {!loading&&!items.length&&!roomEntries.length&&!errors.length&&<p className="pa-empty">{w.empty}</p>}
      {problem&&<p role="status" className="pa-roster-save-error">{w.rosterUnsaved}</p>}
      {groups.map(group => {
        const pinned = group.kind === "pinned", name = pinned ? w.pinned : projectNames.get(group.key)||w.unnamedProject
        const keys = group.items.map(item => item.key)
        return <section className="pa-project-group" key={group.key} aria-label={name} {...targetProps({kind:"project", key:group.key})}>
          <h2 className="pa-project-heading">{pinned ? <span>{w.pinned}</span> : <><ProjectMark icon={projectIcons?.get(group.key)} size={12}/><span>{name}</span></>}{several&&!pinned&&<small>{named(group.machineID)}</small>}{sorting&&!pinned&&handle({kind:"project",key:group.key},name,groups.filter(g=>g.kind==="project").map(g=>g.key))}</h2>
          {group.items.map(item => {
            const key = item.key, project = projectKey(item.machineID,item.projectID), name=item.kind==="bot"?item.item.bot.name:item.entry.group.name
            const pinLabel=item.kind==="bot"?(pinned?w.rosterUnpin:w.rosterPin):(pinned?w.rosterUnpinGroup:w.rosterPinGroup)
            const source=item.kind==="bot"&&sources.find(source=>source.machineID===item.machineID)
            return <div className="pa-roster-entry" key={key} {...targetProps({kind:"bot",key})}
              onKeyDown={event=>{if(event.target instanceof HTMLElement&&event.target.matches(".pa-bot-row")){if(event.altKey&&["ArrowUp","ArrowDown","Home","End"].includes(event.key)){keyboard({kind:"bot",key},keys)(event);requestAnimationFrame(()=>entryFor(key)?.querySelector<HTMLButtonElement>(".pa-bot-row")?.focus({preventScroll:true}))}else if(event.key==="F10"&&event.shiftKey){event.preventDefault();beginSorting(key)}}}}
              onContextMenu={event=>{event.preventDefault();if(!active)beginSorting(key)}}>
              {item.kind==="bot"?<BotRow item={item.item} grouped={!pinned} selected={selected===key} onSelect={()=>onSelect(item.item)} w={w} language={language} projectName={projectNames.get(project)} projectIcon={projectIcons?.get(project)} machineName={several&&pinned?named(item.machineID):undefined}/>:<GroupRosterRow grouped={!pinned} entry={item.entry} bots={(groupsItems??items).map(i=>i.bot)} selected={selectedGroup===botIdentity(item.entry.group.key)} onSelect={target=>openGroup(item.entry,target)} projectName={projectNames.get(project)} machineName={several&&pinned?named(item.machineID):undefined}/>}
              {item.kind==="bot"&&source&&current&&readerID&&onRead&&<BotRosterReadAction key={JSON.stringify([source.authorizationRevision,readerID,item.item.bot.conversation_id])} item={item.item} source={source} current={current} readerID={readerID} onRead={onRead} w={w}/>}
              {sorting&&<div className="pa-roster-actions"><button type="button" className="pa-roster-pin" aria-label={`${pinLabel}: ${name}`} aria-pressed={pinned} title={pinLabel} onClick={()=>pin(key,!pinned)}><svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill={pinned?"currentColor":"none"} stroke="currentColor" strokeWidth="1.3"><path d="m8 1.7 1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.4l-3.8 2.1.7-4.3-3.1-3 4.3-.6Z"/></svg></button>{handle({kind:"bot",key},name,keys)}</div>}
            </div>
          })}
        </section>
      })}
    </>
  }
  return <aside ref={root} className="pa-roster pa-roster-compact" data-sorting={sorting||undefined} aria-label={w.allBots}>
    <header className="pa-toolbar"><button className="pa-circle pa-collapse" onClick={onClose} aria-label={w.collapse} title={w.collapse}><svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3.5 6 8 10.5 12.5 6"/></svg></button><h1>{w.allBots}</h1>{onCreate&&<button className="pa-circle" onClick={onCreate} aria-label={w.create}>+</button>}{sorting?<button className="pa-roster-sort-done" type="button" onClick={finishSorting}>{w.rosterSortDone}</button>:<PaMenu label={w.rosterOptions} items={[{id:"sort",label:w.rosterSort,onSelect:()=>beginSorting()}]}/>}</header>
    <label className="pa-search"><svg className="pa-search-icon" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="6.5" cy="6.5" r="4.5"/><path d="M10 10l3.5 3.5"/></svg><input type="search" placeholder={w.search} aria-label={w.search} value={query} onChange={e=>onQuery(e.target.value)}/>{query&&<button type="button" className="pa-search-clear" aria-label={w.clearQuote||"Clear"} onClick={()=>onQuery("")}><svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 4l8 8M12 4L4 12"/></svg></button>}</label>
    {several&&<div className="pa-roster-filter"><span>{!items.length&&errors.length?(errors.some(([, ,retryable])=>retryable)?w.sourceConnecting:w.sourceError):<>{w.total} {loading&&!items.length?"…":shown.length}</>}</span><PaSelect compact ariaLabel={w.machine} value={machine} onChange={onMachine} options={[{value:"",label:w.allMachines},...machines.map(m=>({value:m,label:named(m)}))]}/></div>}
    <div className="pa-roster-scroll">{loading&&items.length>0&&<p role="status">{w.loading}</p>}{skeleton&&<LoadingSkeleton kind="rows" id="bots-roster" label={w.loading}/>}
      <Groups sources={sources} items={groupsItems??items} projectNames={projectNames} projectIcons={projectIcons} query={query} machine={machine} selected={selected} onOpenGroup={onOpenGroup} onSelectMember={onSelect} onGroupOpenChange={onGroupOpenChange}>{renderRows}</Groups>
      {onMore&&<button className="pa-pill" onClick={onMore}>{w.more}</button>}{hidden}
    </div>
    {status&&<footer className="pa-status-bar">{status}</footer>}
  </aside>
}
