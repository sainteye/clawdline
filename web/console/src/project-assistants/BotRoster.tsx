import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react"
import type { ReactNode } from "react"
import type * as W from "@clawdline/contract"
import { BotRow } from "./BotRow"
import { BotRosterReadAction } from "./BotRosterReadAction"
import { catalogWord } from "../catalog"
import { botIdentity, canWrite } from "./view-state"
import type { Words } from "./words"
import { PaSelect } from "./PaSelect"
import { PaMenu, type PaMenuAnchor, type PaMenuItem } from "./PaMenu"
import { ProjectMark } from "./ProjectMark"
import { projectKey, pinnedGroupKey } from "./roster-groups"
import { LoadingSkeleton } from "../LoadingSkeleton"
import { strings } from "../legacy/bridge.js"
import { usePullToRefresh } from "../session/use-pull-to-refresh"
import type { Swipes } from "../session/swipe"
import { useShowAfter } from "./PaLoading"
import type { SourceClient } from "./client"
import { Groups, groupWord } from "./Groups"
import {GroupRosterRow} from "./GroupRosterRow"
import {GroupActionIcon} from "./GroupAvatar"
import {conversations,conversationSections,orderedConversationSections,personalRosterProject,type GroupRosterEntry} from "./roster-conversations"
import { useRosterDrag } from "./useRosterDrag"
import { moveRosterBot, pinRosterBot, reorderKeys, type RosterState } from "./roster-state"
import { useRosterState } from "./useRosterState"
import { RosterHandle, type RosterDrag } from "./RosterHandle"
import { rosterIcon } from "./roster-icons"
import "./roster-controls.css"
export function BotRoster({view="list",onView,tower,rosterScope,items,machines,selected,query,onQuery,machine,onMachine,onSelect,onCreate,onClose,w,language,loading,unavailable,onRetry,onRefresh,onMore,projectNames,projectIcons,machineNames,hidden,sources=[],current,readerID,onRead,groupsItems,onOpenGroup,onGroupOpenChange,groupRoute,onGroupBack,botActions,saveRoster,newSection}: {
  saveRoster?:(item:W.ProjectAssistantBotSummary,edit:W.ProjectAssistantRosterEdit)=>void;
  newSection?:(item:W.ProjectAssistantBotSummary)=>void;
  botActions?:(item:W.ProjectAssistantBotSummary)=>PaMenuItem[];
  view?:"list"|"tower";onView?:(view:"list"|"tower")=>void;tower?:ReactNode;
  onOpenGroup?:(key:W.ProjectAssistantBotKey)=>void;onGroupOpenChange?:(open:boolean)=>void;groupRoute?:string|null;onGroupBack?:()=>boolean;rosterScope:string;items:W.ProjectAssistantBotSummary[];machines:readonly string[];selected:string|null;query:string;onQuery:(s:string)=>void;machine:string;onMachine:(s:string)=>void;
  onSelect:(i:W.ProjectAssistantBotSummary,fromGroup?:boolean)=>void;onCreate?:()=>void;onClose:()=>void;w:Words;language:string;loading:boolean;
  current?:()=>readonly SourceClient[];readerID?:string;onRead?:(source:SourceClient)=>Promise<void>;
  unavailable?:boolean;onRetry?:()=>void;onRefresh?:()=>Promise<void>;onMore?:()=>void;projectNames:ReadonlyMap<string,string>;projectIcons?:ReadonlyMap<string,W.Icon>;machineNames?:ReadonlyMap<string,string>;hidden?:ReactNode;sources?:readonly SourceClient[];groupsItems?:readonly W.ProjectAssistantBotSummary[]
}) {
  const {state: rosterState, problem, update} = useRosterState(rosterScope)
  const root=useRef<HTMLElement>(null)
  const scroll=useRef<HTMLDivElement>(null),pullPad=useRef<HTMLDivElement>(null)
  const [swipe]=useState<Pick<Swipes,"axis">>(()=>({
    axis:()=>root.current?.dataset.rosterDragging?"row":"undecided",
  }))
  const pullWord=usePullToRefresh(scroll,pullPad,()=>onRefresh?.(),swipe)
  const draft = useRef<RosterState|null>(null)
  const pinAtStart=useRef<{item:W.ProjectAssistantBotSummary;value:boolean}|null>(null)
  const started=useRef(rosterState)
  const direct = useRef(false)
  const [preview,setPreview]=useState<RosterState|null>(null)
  const moves=useRef<(target:RosterDrag)=>void>(()=>{})
  const state=preview??rosterState
  const gesture=useRosterDrag(root,{
    start: (source,isDirect)=>{
      started.current=rosterState
      direct.current=isDirect
      const item=source.kind==="bot"?items.find(item=>botIdentity(item.bot.key)===source.key):undefined
      const shared=item&&sharedRoster(item)
      const value=item?isPinned(item):false
      pinAtStart.current=shared?{item,value}:null
      draft.current=shared?pinRosterBot(rosterState,source.key,value):rosterState
      setPreview(draft.current)
      setActive(source)
    },
    over: target=>moves.current(target),
    finish: commit=>{
      const shared=pinAtStart.current
      if(commit&&started.current===rosterState&&draft.current&&draft.current!==rosterState){
        if(shared){
          const key=botIdentity(shared.item.bot.key),pinned=draft.current.pinnedBotKeys.includes(key)
          if(pinned!==shared.value)setPinned(shared.item,pinned)
          update({...draft.current,pinnedBotKeys:rosterState.pinnedBotKeys})
        }else update(draft.current)
      }
      pinAtStart.current=null
      draft.current=null
      setPreview(null);setActive(null);setOver(null)
    }
  })
  useEffect(()=>{if(draft.current&&started.current!==rosterState)gesture.cancel()},[rosterState])
  const [context,setContext]=useState<{key:string;anchor:PaMenuAnchor|null}|null>(null)
  const [sectionsOpen,setSectionsOpen]=useState(false)
  const [sorting,setSorting]=useState(false)
  const sortFocus=useRef<string|undefined>(undefined),restoreOptions=useRef(false)
  const [active, setActive] = useState<RosterDrag | null>(null), [over, setOver] = useState<RosterDrag | null>(null)
  const order=rosterState.order??"default"
  const defaultLayout=sorting||order!=="machine"
  const shown = items.filter(i => !machine || i.bot.key.machine_id === machine)
  const [,tickClock]=useState(Date.now)
  const showingBots=!tower&&shown.length>0
  useEffect(()=>{
    if(!showingBots)return
    const timer=window.setInterval(()=>tickClock(Date.now()),60_000)
    return()=>window.clearInterval(timer)
  },[showingBots])
  const endDrag = () => gesture.cancel()
  const entryFor=(key:string)=>[...root.current?.querySelectorAll<HTMLElement>(".pa-roster-entry")??[]].find(row=>row.dataset.rosterDropKey===key)
  const pin=(key:string,pinned:boolean)=>{update(pinRosterBot(rosterState,key,pinned));requestAnimationFrame(()=>entryFor(key)?.querySelector<HTMLButtonElement>(".pa-bot-row")?.focus({preventScroll:true}))}
  const openContext=(event:MouseEvent<HTMLDivElement>|KeyboardEvent<HTMLElement>,key:string)=>{
    event.preventDefault()
    if(active)return
    const opener=event.currentTarget.querySelector<HTMLElement>(".pa-bot-row")
    if(opener){
      setSectionsOpen(false)
      setContext({key,anchor:{opener,point:"clientX" in event&&(event.clientX||event.clientY)?{x:event.clientX,y:event.clientY}:undefined}})
    }
  }
  const sharedRoster=(item:W.ProjectAssistantBotSummary)=>!!saveRoster&&sources.find(source=>source.machineID===item.bot.key.machine_id)?.canRosterActions?.()===true
  const isPinned=(item:W.ProjectAssistantBotSummary)=>item.bot.sidebar_pinned?item.bot.sidebar_pinned==="pinned":rosterState.pinnedBotKeys.includes(botIdentity(item.bot.key))
  const setPinned=(item:W.ProjectAssistantBotSummary,value:boolean)=>{
    if(sharedRoster(item)){
      saveRoster?.(item,{pinned:value?"pinned":"unpinned"})
    }else pin(botIdentity(item.bot.key),value)
  }
  const focusSortTarget=()=>{const target=sortFocus.current?entryFor(sortFocus.current):root.current;target?.querySelector<HTMLButtonElement>(".pa-roster-handle")?.focus({preventScroll:true})}
  const beginSorting=(key?:string)=>{sortFocus.current=key;if(sorting)focusSortTarget();else setSorting(true)}
  const finishSorting=()=>{endDrag();restoreOptions.current=true;setSorting(false)}
  // Focus belongs to the mode's commit, never a later frame that can steal a subsequent choice.
  useLayoutEffect(()=>{if(sorting)focusSortTarget();else if(restoreOptions.current){restoreOptions.current=false;root.current?.querySelector<HTMLButtonElement>(".pa-toolbar > .pa-menu:last-child > button")?.focus({preventScroll:true})}},[sorting])
  // One machine needs no machine filter and no machine name on every row.
  const several=machines.length>1,named=(id:string)=>machineNames?.get(id)||w.thisMachine
  // The first read draws placeholder rows after 150ms; once Bots are listed, a later read only says so.
  const skeleton=useShowAfter(loading&&!items.length)
  const renderRows=(roomEntries:readonly GroupRosterEntry[],openGroup:(entry:GroupRosterEntry,opener?:HTMLElement)=>void,selectedGroup:string|null,dismissGroup:()=>void)=>{
    const now=Date.now()
    const allRows=conversations(items,roomEntries)
    const override=preview&&pinAtStart.current?botIdentity(pinAtStart.current.item.bot.key):null
    const previewItems=override?shown.map(item=>botIdentity(item.bot.key)===override?{...item,bot:{...item.bot,sidebar_pinned:undefined}}:item):shown
    const sections=conversationSections(conversations(previewItems,roomEntries),state)
    const groups=orderedConversationSections(sections,sorting?"default":order)
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
        const group = target.kind === "bot" ? sections.find(g => g.items.some(i => i.key === target.key)) : sections.find(g => g.key === target.key)
        const pinned = target.key === pinnedGroupKey || group?.kind === "pinned"
        // Moving a row changes its presentation, never the Project it belongs to.
        const section=item.kind==="bot"?item.item.bot.sidebar_section:undefined
        const normal=section?JSON.stringify(["sidebar-section",item.machineID,section]):projectKey(item.machineID,item.projectID)
        if (!pinned && group?.key !== normal) return
        if(item.kind==="bot"&&sharedRoster(item.item)&&!canWrite(item.item.source)&&pinned!==isPinned(item.item))return
        if(item.kind==="bot"&&sharedRoster(item.item)&&save&&pinned!==isPinned(item.item))setPinned(item.item,pinned)
        if (target.kind === "bot") apply(moveRosterBot(currentState, source.key, target.key, pinned, allRows.map(row=>row.key)))
        else apply(pinRosterBot(currentState, source.key, pinned))
      }
    }
    moves.current=target=>{
      if(!active||!draft.current||active.key===target.key)return
      if(direct.current&&active.kind==="bot"){
        const from=sections.find(g=>g.items.some(i=>i.key===active.key)),to=sections.find(g=>g.items.some(i=>i.key===target.key))
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
      "data-roster-drop-kind": defaultLayout?target.kind:undefined, "data-roster-drop-key": target.key,
      "data-dragging": active?.kind === target.kind && active.key === target.key || undefined,
      "data-drop": over?.kind === target.kind && over.key === target.key || undefined
    })
    const handle = (source: RosterDrag, name: string, keys: string[]) => <RosterHandle label={`${w.rosterReorder}: ${name}`} hint={w.rosterReorderHint} onKey={keyboard(source, keys)}/>
    return <>
      {!loading&&!shown.length&&!roomEntries.length&&(unavailable?<div className="pa-roster-unavailable"><p role="status">{w.rosterUnavailable}</p>{onRetry&&<button type="button" className="pa-pill" onClick={onRetry}>{w.retry}</button>}</div>:<p className="pa-empty">{query?w.searchEmpty:w.empty}</p>)}
      {problem&&<p role="status" className="pa-roster-save-error">{w.rosterUnsaved}</p>}
      {context&&(()=>{
        const row=allRows.find(row=>row.kind==="bot"&&row.key===context.key)
        if(row?.kind!=="bot")return null
        const bot=row.item.bot,pinned=isPinned(row.item)
        const source=sources.find(source=>source.machineID===row.machineID)
        const editable=!!source?.canRosterActions?.()&&canWrite(row.item.source)
        const names=[...new Set((groupsItems??items).filter(item=>item.bot.key.machine_id===row.machineID).map(item=>item.bot.sidebar_section).filter((name):name is string=>!!name))].sort((a,b)=>a.localeCompare(b,language))
        const actions:PaMenuItem[]=sectionsOpen?[
          {id:"back",label:w.back,onSelect:()=>setSectionsOpen(false),keepOpen:true},
          {id:"default",label:w.rosterDefaultSection,disabled:!bot.sidebar_section,onSelect:()=>saveRoster?.(row.item,{section:{name:""}})},
          ...names.map(name=>({id:`section-${name}`,label:name,disabled:bot.sidebar_section===name,onSelect:()=>saveRoster?.(row.item,{section:{name}})})),
          {id:"new-section",label:w.rosterNewSection,onSelect:()=>newSection?.(row.item)}
        ]:[
          {id:"pin",label:pinned?w.rosterUnpin:w.rosterPin,disabled:source?.canRosterActions?.()===true&&!editable,icon:rosterIcon("pin"),onSelect:()=>setPinned(row.item,!pinned)},
          {id:"section",label:w.rosterMoveSection,disabled:!editable,icon:rosterIcon("section"),onSelect:()=>setSectionsOpen(true),keepOpen:true,submenu:true},
          ...(botActions?.(row.item)??[])
        ]
        return <PaMenu label={sectionsOpen?w.rosterMoveSection:w.rosterOptions} externalAnchor={context.anchor}
          onClose={()=>setContext(old=>old?{...old,anchor:null}:old)} items={actions} floating/>
      })()}
      {groups.map(group => {
        // The lead section reads like the pinned one: no Project mark, each row its own Project.
        const lead=group.kind==="lead", pinned=group.kind==="pinned", byMachine=group.kind==="machine", unread=group.kind==="unread"
        const flat=pinned||lead||byMachine||unread, name=unread?w.unread:byMachine?named(group.machineID):lead?`★ ${w.rosterLead}`:pinned?w.pinned:group.name||(group.items[0]?.projectID===personalRosterProject?w.personalBots:projectNames.get(group.key))||w.unnamedProject
        const keys = group.items.map(item => item.key)
        return <section className="pa-project-group" key={group.key} aria-label={name} data-roster-project={group.kind==="project"||undefined} {...(!unread&&targetProps({kind:"project",key:group.key}))}>
          <h2 className="pa-project-heading">{flat ? <span>{name}</span> : <><ProjectMark icon={projectIcons?.get(group.key)} size={12}/><span>{name}</span></>}{several&&!flat&&<small>{named(group.machineID)}</small>}{sorting&&group.kind==="project"&&handle({kind:"project",key:group.key},name,sections.filter(g=>g.kind==="project").map(g=>g.key))}</h2>
          {group.items.map(item => {
            const key = item.key, project = projectKey(item.machineID,item.projectID), name=item.kind==="bot"?item.item.bot.name:item.entry.group.name
            const pinLabel=item.kind==="bot"?(pinned?w.rosterUnpin:w.rosterPin):(pinned?w.rosterUnpinGroup:w.rosterPinGroup)
            const source=item.kind==="bot"&&sources.find(source=>source.machineID===item.machineID)
            return <div className="pa-roster-entry" key={key} {...(!unread&&targetProps({kind:"bot",key}))}
              onKeyDown={event=>{if(event.target instanceof HTMLElement&&event.target.matches(".pa-bot-row")){if(defaultLayout&&!unread&&event.altKey&&["ArrowUp","ArrowDown","Home","End"].includes(event.key)){keyboard({kind:"bot",key},keys)(event);requestAnimationFrame(()=>entryFor(key)?.querySelector<HTMLButtonElement>(".pa-bot-row")?.focus({preventScroll:true}))}else if(event.key==="F10"&&event.shiftKey){if(item.kind==="bot")openContext(event,key);else{event.preventDefault();beginSorting(key)}}}}}
              onContextMenu={event=>{if(item.kind==="bot")openContext(event,key);else{event.preventDefault();if(!active)beginSorting(key)}}}>
              {item.kind==="bot"?<BotRow item={item.item} grouped={!flat} selected={selected===key} onSelect={()=>{dismissGroup();onSelect(item.item)}} w={w} language={language} now={now} projectName={item.item.bot.project.kind==="personal"?w.personalWorkspace:projectNames.get(project)} projectIcon={projectIcons?.get(project)} machineName={several&&flat&&!byMachine?named(item.machineID):undefined}/>:<GroupRosterRow grouped={!flat} entry={item.entry} bots={(groupsItems??items).map(i=>i.bot)} selected={selectedGroup===botIdentity(item.entry.group.key)} onSelect={target=>openGroup(item.entry,target)} projectName={projectNames.get(project)} machineName={several&&flat&&!byMachine?named(item.machineID):undefined}/>}
              {item.kind==="bot"&&source&&current&&readerID&&onRead&&<BotRosterReadAction key={JSON.stringify([source.authorizationRevision,readerID,item.item.bot.conversation_id])} item={item.item} source={source} current={current} readerID={readerID} onRead={onRead} w={w}
                focusRow={unread?()=>entryFor(key)?.querySelector<HTMLButtonElement>(".pa-bot-row"):undefined}/>}
              {sorting&&<div className="pa-roster-actions">{!lead&&<button type="button" className="pa-roster-pin" aria-label={`${pinLabel}: ${name}`} aria-pressed={pinned} title={pinLabel} disabled={item.kind==="bot"&&sharedRoster(item.item)&&!canWrite(item.item.source)} onClick={()=>item.kind==="bot"?setPinned(item.item,!pinned):pin(key,!pinned)}><svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill={pinned?"currentColor":"none"} stroke="currentColor" strokeWidth="1.3"><path d="m8 1.7 1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.4l-3.8 2.1.7-4.3-3.1-3 4.3-.6Z"/></svg></button>}{handle({kind:"bot",key},name,keys)}</div>}
            </div>
          })}
        </section>
      })}
    </>
  }
  return <aside ref={root} className="pa-roster pa-roster-compact" data-sorting={sorting||undefined} aria-label={w.allBots}>
    <Groups sources={sources} items={groupsItems??items} projectNames={projectNames} projectIcons={projectIcons} query={query} machine={machine} selected={selected} onOpenGroup={onOpenGroup} onSelectMember={item=>onSelect(item,true)} onGroupOpenChange={onGroupOpenChange} groupRoute={groupRoute} onGroupBack={onGroupBack}>
      {group=>{
        const createItems:PaMenuItem[]=[]
        if(onCreate)createItems.push({id:"bot",label:w.create,icon:<GroupActionIcon/>,onSelect:onCreate})
        if(group.create)createItems.push({id:"group",label:groupWord("groupCreate"),icon:<GroupActionIcon group/>,onSelect:group.create})
        return <>
          <header className="pa-toolbar">
            <button className="pa-circle pa-collapse" onClick={onClose} aria-label={w.collapse} title={w.collapse}><svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3.5 6 8 10.5 12.5 6"/></svg></button>
            {onView&&!sorting&&<button type="button" className="pa-circle pa-view-toggle" aria-pressed={view==="tower"} aria-label={w.towerView} title={w.towerView} onClick={()=>onView(view==="tower"?"list":"tower")}><svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round"><path d="M3 14.5V3.5l5-2 5 2v11M1.5 14.5h13M6 5.5h1M9 5.5h1M6 8.5h1M9 8.5h1M7 14.5v-3h2v3"/></svg></button>}
            {createItems.length>0&&<PaMenu label={catalogWord("projectAssistants","rosterCreate")} items={createItems} triggerIcon={<GroupActionIcon size={20}/>}/>}
            {sorting?<button className="pa-roster-sort-done" type="button" onClick={finishSorting}>{w.rosterSortDone}</button>:<PaMenu label={w.rosterOptions} items={[{id:"sort",label:w.rosterSort,onSelect:()=>beginSorting()}]}/>}
          </header>
          {view!=="tower"&&<label className="pa-search"><svg className="pa-search-icon" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="6.5" cy="6.5" r="4.5"/><path d="M10 10l3.5 3.5"/></svg><input type="search" placeholder={w.search} aria-label={w.search} value={query} onChange={e=>onQuery(e.target.value)}/>{query&&<button type="button" className="pa-search-clear" aria-label={w.clearSearch} onClick={()=>onQuery("")}><svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 4l8 8M12 4L4 12"/></svg></button>}</label>}
          {(!tower||several)&&<div className="pa-roster-filter">
            <span>{(!loading&&!unavailable||shown.length>0)&&`${w.total} ${shown.length}`}</span>
            {!tower&&!sorting&&<div className="pa-roster-order">
              <PaSelect compact ariaLabel={w.rosterOrder} value={order}
                options={[{value:"default",label:w.rosterOrderDefault},{value:"unread",label:w.rosterOrderUnread},{value:"machine",label:w.rosterOrderMachine}]}
                onChange={next=>{endDrag();update({...rosterState,order:next==="unread"||next==="machine"?next:undefined})}}/>
            </div>}
            {several&&<PaSelect compact ariaLabel={w.machine} value={machine} onChange={onMachine} options={[{value:"",label:w.allMachines},...machines.map(m=>({value:m,label:named(m)}))]}/>}
          </div>}
          <div className="pa-roster-scroll" ref={scroll}>
            <div className="ptr" ref={pullPad}>
              <span>{pullWord==="release"?strings.webPullRelease:pullWord==="busy"?strings.webPullBusy:strings.webPull}</span>
            </div>
            {/* On a phone the tower stands in for the rows: list placeholders above it would push the picture down. */}
            {skeleton&&!tower&&<LoadingSkeleton kind="rows" id="bots-roster" label={w.loading}/>}
            {tower??renderRows(group.rows,group.open,group.selected,group.dismiss)}
            {group.content}
            {onMore&&<button className="pa-pill" onClick={onMore}>{w.more}</button>}{hidden}
          </div>
        </>
      }}
    </Groups>
  </aside>
}
