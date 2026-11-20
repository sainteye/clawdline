import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, projectChoices, botFromFragment } from "./client"
import { markConversationRead, canMarkConversationRead } from "./conversation-read"
import { rosterIcon } from "./roster-icons"
import { RosterActionDialog, type RosterPrompt } from "./RosterActionDialog"
import type { PaMenuItem } from "./PaMenu"
import { BotRoster } from "./BotRoster"
import { FallbackSourceScope } from "./FallbackSettings"
import { BotCreate } from "./BotCreate"
import { HiddenBots } from "./HiddenBots"
import { Chat, EventRefreshMs } from "./Chat"
import { followLiveRoster } from "./live-roster"
import { readSnapshotRoster, serialSnapshotRefresh, followSnapshotRoster } from "./snapshot-roster"
import { botIdentity, canRead, canWrite, createViewState, mergeRoster, type ViewState } from "./view-state"
import { words } from "./words"
import { retryableSourceError, shownSourceError } from "./source-errors"
import { matchesRosterQuery, snapshotRosterQuery } from "./roster-search"
import { firstCallableBot } from "./roster-conversations"
import { emptyRosterState, getRosterState } from "./roster-state"
import { TowerSlot, savedBotsView, saveBotsView, type BotsView } from "./tower/TowerSlot"
import "./styles.css"

export interface BotsProps {
  onRosterObserved?:(items:readonly W.ProjectAssistantBotSummary[],loadedSources:readonly string[])=>void
  sources: readonly SourceClient[]
  onClose:()=>void
  language?:string
  /** Authenticated account namespace and reader principal, supplied by the integration host. */
  accountKey?:string
  readerID?:string
  viewState?:ViewState
  returnFocus?:HTMLElement|null
  onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void
  /** The shell may own route history instead. A mount always begins with no selection. */
  manageHistory?:boolean
  onSelectionChange?:(key:W.ProjectAssistantBotKey|null)=>void
  voiceShortcut?:number
}
interface SourcePage {revision:string;query:string;requestQuery:string;items:W.ProjectAssistantBotSummary[];cursor:string;statuses:W.ProjectAssistantSourceStatus[];error:string;loadedPages?:number}
export function Bots({sources,onClose,language="en",accountKey,readerID,viewState,returnFocus,onOpenWork,onRosterObserved,manageHistory=false,onSelectionChange,voiceShortcut=0}:BotsProps) {
  const w=words(language),[selected,setSelected]=useState<string|null>(null),[query,setQuery]=useState(""),[machine,setMachine]=useState("")
  const [pages,setPages]=useState<Record<string,SourcePage>>({}),[loading,setLoading]=useState(true),[creating,setCreating]=useState(false)
  const [rosterPrompt,setRosterPrompt]=useState<RosterPrompt|null>(null)
  const [rosterNotice,setRosterNotice]=useState<{text:string;error?:boolean}|null>(null)
  const [profileRequest,setProfileRequest]=useState<{id:string;count:number}|null>(null)
  const [groupOpen,setGroupOpen]=useState(false)
  const [savedView,setSavedView]=useState<BotsView|null>(savedBotsView)
  const changeView=(next:BotsView)=>{setSavedView(next);saveBotsView(next)}
  const narrow=useNarrow()
  const [rosterHover,setRosterHover]=useState<string|null>(null)
  const [groupRoute,setGroupRoute]=useState<string|null>(null)
  const [destination,setDestination]=useState(()=>botFromFragment(location.hash))
  const [livePending,setLivePending]=useState<Record<string,boolean>>({})
  const [projectNames,setProjectNames]=useState(new Map<string,string>()),[projectIcons,setProjectIcons]=useState(new Map<string,W.Icon>())
  const leadPreferenceKey=JSON.stringify(["clawdline.defaultLead",accountKey??"local",readerID??""])
  const [preferredLead,setPreferredLead]=useState("")
  useEffect(()=>{try{setPreferredLead(localStorage.getItem(leadPreferenceKey)??"")}catch{setPreferredLead("")}},[leadPreferenceKey])
  const [callTarget,setCallTarget]=useState<{key:string;request:number}|null>(null)
  const handledShortcut=useRef(0)
  const host=useRef<HTMLDivElement>(null),sourceRef=useRef(sources),closeRef=useRef(onClose),selectRef=useRef(onSelectionChange)
  sourceRef.current=sources;closeRef.current=onClose;selectRef.current=onSelectionChange
  const current=useCallback(()=>sourceRef.current,[])
  const authorization=sources.map(s=>JSON.stringify([s.machineID,s.authorizationRevision])).join("|")
  const namespace=accountKey??authorization
  const view=useMemo(()=>{if(viewState)return viewState;try{return createViewState(window.localStorage)}catch{return createViewState()}},[viewState,namespace])
  const historyID=useRef(typeof crypto!=="undefined"?crypto.randomUUID():"")
  const lastSelected=useRef<string|null>(null)
  const loader=useRef<(append?:boolean)=>Promise<void>>(async()=>{})
  const deniedSnapshots=useRef(new Set<SourceClient>())
  const pagesRef=useRef(pages);pagesRef.current=pages
  const allItems=mergeRoster(Object.entries(pages).filter(([id,p])=>sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([,p])=>p.items))
  useEffect(()=>{
    const loaded=Object.entries(pages).filter(([id,p])=>!p.error&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([id])=>id)
    if(loaded.length)onRosterObserved?.(allItems,loaded)
  },[pages,authorization,onRosterObserved])
  const matching=allItems.filter(i=>matchesRosterQuery(i,query,projectNames))
  const items=matching.filter(i=>!i.bot.hidden),hiddenItems=matching.filter(i=>i.bot.hidden)
  const rosterLoading=loading||sources.some(s=>livePending[s.machineID])
  const unhide=async(item:W.ProjectAssistantBotSummary)=>{const source=sources.find(s=>s.machineID===item.bot.key.machine_id);if(!source?.setHidden)return;await authorized(source,current,s=>source.setHidden!({command:command(item.bot.key,"visibility"),hidden:false},s),new AbortController().signal);void load()}
  // The tower shows every visible Bot of the chosen machine; search narrows only the list.
  const leads=sources.flatMap(source=>allItems.filter(item=>item.bot.key.machine_id===source.machineID&&item.bot.lead&&!item.bot.hidden&&canRead(item.source)))
  const preferred=(items:W.ProjectAssistantBotSummary[])=>items.find(item=>botIdentity(item.bot.key)===preferredLead)??items[0]
  const lead=preferred(leads)
  const availableLead=preferred(leads.filter(item=>canWrite(item.source)))
  const leadOptions=leads.length>1?[
    {value:"",label:w.automaticLead},
    ...leads.map(item=>({value:botIdentity(item.bot.key),label:`${item.bot.name} · ${sources.find(s=>s.machineID===item.bot.key.machine_id)?.machineName??w.thisMachine}`}))
  ]:undefined
  function saveLead(key:string){
    setPreferredLead(key)
    try{if(key)localStorage.setItem(leadPreferenceKey,key);else localStorage.removeItem(leadPreferenceKey)}catch{}
  }
  const towerItems=allItems.filter(i=>!i.bot.hidden&&(!machine||i.bot.key.machine_id===machine))
  // Until the person chooses, a desktop with Bots opens on the tower; a phone opens on the list.
  const botsView:BotsView=savedView??(!narrow&&towerItems.length>0?"tower":"list")
  const tower=botsView==="tower"?<TowerSlot items={towerItems} highlight={narrow?null:rosterHover} w={w} language={language} paused={groupOpen} onList={()=>changeView("list")} onSelect={item=>select(item)} loading={rosterLoading} sources={sources} current={current} projectName={item=>item.bot.project.kind==="personal"?w.personalWorkspace:projectNames.get(JSON.stringify([item.bot.key.machine_id,item.bot.project.project_id]))}/>:undefined
  const selectedItem=allItems.find(i=>botIdentity(i.bot.key)===selected)
  const selectedSource=selectedItem?sources.find(s=>s.machineID===selectedItem.bot.key.machine_id):undefined
  const statuses=Object.entries(pages).flatMap(([id,p])=>!p.error&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)?p.statuses:[])
  const rosterUnavailable=!rosterLoading&&!statuses.some(status=>(!machine||status.machine_id===machine)&&canRead(status))
    &&Object.entries(pages).some(([id,p])=>(!machine||id===machine)&&shownSourceError(p.error)&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision))
  const machineNames=new Map(sources.map(s=>[s.machineID,s.machineName??""] as [string,string]))
  const load=useCallback((append=false)=>loader.current(append),[])
  // Live sources publish absolute unread counts. Snapshot sources read them back
  // after the observed cursor is acknowledged, including arrivals during the write.
  const refreshAfterRead=async(source:SourceClient)=>{
    if(current().includes(source)&&(!source.canRoster?.()||!source.subscribeRoster))await load()
  }
  const searchProjectNames=query?projectNames:null
  useEffect(()=>{
    const control=new AbortController()
    deniedSnapshots.current.clear()
    loader.current=serialSnapshotRefresh(async append=>{
    if(append||!Object.values(pagesRef.current).some(p=>p.query===query))setLoading(true)
    await Promise.all(current().map(async source=>{
      if(source.canRoster?.()&&source.subscribeRoster)return
      if(deniedSnapshots.current.has(source))return
      const requestQuery=searchProjectNames?snapshotRosterQuery(source.machineID,query,searchProjectNames):""
      const previous=pagesRef.current[source.machineID],sameQuery=previous?.query===query&&previous.requestQuery===requestQuery,cursor=append&&previous?.revision===source.authorizationRevision&&sameQuery?previous.cursor:""
      if(append&&!cursor)return
      try {
        const coverage=previous?.revision===source.authorizationRevision&&sameQuery?previous.loadedPages??1:1
        const response=await readSnapshotRoster(source,current,requestQuery,append?1:coverage,control.signal,cursor)
        if(control.signal.aborted)return
        const own=response.sources.find(s=>s.machine_id===source.machineID)
        const rows=response.items.filter(i=>i.bot.key.machine_id===source.machineID&&canRead(i.source)).map(i=>own?{...i,source:own}:i)
        setPages(old=>({...old,[source.machineID]:{revision:source.authorizationRevision,query,requestQuery,items:append&&old[source.machineID]?.revision===source.authorizationRevision&&old[source.machineID]?.query===query&&old[source.machineID]?.requestQuery===requestQuery?mergeRoster([old[source.machineID].items,rows]):rows,cursor:response.cursor,loadedPages:append?coverage+response.loadedPages:response.loadedPages,statuses:response.sources.filter(s=>s.machine_id===source.machineID),error:""}}))
      }catch(e){if(!control.signal.aborted){const code=errorCode(e),denied=["pa_revoked","pa_scope_denied","pa_unpaired"].includes(code);if(!retryableSourceError(code))deniedSnapshots.current.add(source);setPages(old=>({...old,[source.machineID]:{revision:source.authorizationRevision,query,requestQuery,items:!denied&&old[source.machineID]?.revision===source.authorizationRevision&&old[source.machineID]?.query===query&&old[source.machineID]?.requestQuery===requestQuery?old[source.machineID].items.map(i=>({...i,source:{...i.source,fresh:false,writable:false}})):[],cursor:!denied&&old[source.machineID]?.query===query&&old[source.machineID]?.requestQuery===requestQuery?old[source.machineID]?.cursor??"":"",loadedPages:old[source.machineID]?.loadedPages,statuses:old[source.machineID]?.statuses.map(s=>({...s,fresh:false,writable:false}))??[],error:code}}))}}
    }))
    if(!control.signal.aborted)setLoading(false)
    },control.signal)
    if(selected||current().every(s=>s.canRoster?.()&&s.subscribeRoster))void load()
    return()=>control.abort()
  },[authorization,query,current,searchProjectNames])
  useEffect(()=>{setSelected(null);setPages({});setMachine("")},[authorization])
  useEffect(()=>{
    if(selected||!sources.some(s=>!s.canRoster?.()||!s.subscribeRoster))return
    const control=new AbortController()
    followSnapshotRoster(async()=>{await load();if(!current().some(s=>!deniedSnapshots.current.has(s)&&(!s.canRoster?.()||!s.subscribeRoster)))control.abort()},control.signal,EventRefreshMs,document,window,{readOnEntry:true})
    return()=>control.abort()
  },[selected,authorization,query,searchProjectNames,load])
  useEffect(()=>{
    const control=new AbortController()
    setLivePending({})
    for(const source of sources) {
      if(!source.canRoster?.()||!source.subscribeRoster)continue
      void followLiveRoster(source,current,control.signal,EventRefreshMs,value=>{
        if(control.signal.aborted||!current().includes(source))return
        setLivePending(old=>({...old,[source.machineID]:value.pending}))
        setPages(old=>{
          const previous=old[source.machineID]?.revision===source.authorizationRevision?old[source.machineID]:undefined
          const items=value.items??(value.error?previous?.items.map(i=>({...i,source:{...i.source,fresh:false,writable:false}})):previous?.items)??[]
          return {...old,[source.machineID]:{revision:source.authorizationRevision,query:"",requestQuery:"",items,cursor:"",statuses:value.statuses??previous?.statuses??[],error:value.error}}
        })
      })
    }
    return()=>control.abort()
  },[sources,current])
  useEffect(()=>{
    const control=new AbortController()
    setProjectNames(new Map());setProjectIcons(new Map())
    for(const source of sources)void authorized(source,current,async s=>{const request={machine_id:source.machineID,cursor:"",page_size:100};return projectChoices(await source.projects(request,s),request)},control.signal).then(answer=>{if(!control.signal.aborted)setProjectNames(old=>{const next=new Map(old);for(const p of answer.items)next.set(JSON.stringify([source.machineID,p.target.project_id]),p.label);return next});setProjectIcons(old=>{const next=new Map(old);for(const p of answer.items)if(p.icon)next.set(JSON.stringify([source.machineID,p.target.project_id]),p.icon);return next})}).catch(()=>{})
    return()=>control.abort()
  },[authorization,current])
  const writeHistory=(bot:string|null,group:string|null,replace:boolean)=>{
    const current=history.state
    const depth=current?.paView===historyID.current?(current.paDepth??1):1
    // A member chat is a step above its group. Switching conversations on
    // desktop replaces only the current step and keeps the actual depth.
    const state={...current,paView:historyID.current,paBot:bot,paGroup:group,paDepth:depth+(replace?0:1)}
    if(replace)history.replaceState(state,"")
    else history.pushState(state,"")
  }
  const select=(item:W.ProjectAssistantBotSummary,fromGroup=false)=>{
    const id=botIdentity(item.bot.key)
    lastSelected.current=id
    setSelected(id);selectRef.current?.(item.bot.key)
    if(manageHistory){
      const previous=history.state
      const group=fromGroup&&previous?.paView===historyID.current?previous.paGroup??null:null
      setGroupRoute(group)
      writeHistory(id,group,previous?.paView===historyID.current&&!!(previous.paBot||previous.paGroup&&!fromGroup))
    }
  }
  const removeFromList=(key:W.ProjectAssistantBotKey)=>{
    const id=botIdentity(key)
    if(selected===id)back()
    setPages(old=>Object.fromEntries(Object.entries(old).map(([machine,p])=>[machine,{...p,items:p.items.filter(i=>botIdentity(i.bot.key)!==id)}])))
  }
  const updateBot=(bot:W.ProjectAssistantBot)=>setPages(old=>Object.fromEntries(Object.entries(old).map(([id,p])=>[id,{...p,items:p.items.map(i=>botIdentity(i.bot.key)===botIdentity(bot.key)?{...i,bot}:i)}])))
  const withBot=<T,>(item:W.ProjectAssistantBotSummary,action:(source:SourceClient,signal:AbortSignal)=>Promise<T>,signal?:AbortSignal)=>{
    const source=sources.find(client=>client.machineID===item.bot.key.machine_id)
    if(!source)return Promise.reject(new Error("Source unavailable"))
    return authorized(source,current,s=>action(source,s),signal??new AbortController().signal)
  }
  const changeRoster=async(item:W.ProjectAssistantBotSummary,roster:W.ProjectAssistantRosterEdit,signal?:AbortSignal)=>{
    const result=await withBot(item,async(source,s)=>{
      const {bot}=await authorized(source,current,s=>source.get(item.bot.key,s),s)
      // The source checks the version captured by this read at publication.
      return authorized(source,current,s=>source.edit({command:command(item.bot.key,"edit"),settings:{
        expected_version:bot.version,name:bot.name,appearance:bot.appearance,instructions:bot.instructions.text,roster
      }},s),s)
    },signal)
    updateBot(result.bot)
    setRosterNotice({text:roster.fresh_context?w.rosterNewSession:w.rosterUpdated})
  }
  const runRoster=(action:()=>Promise<void>)=>{
    setRosterNotice(null)
    void Promise.resolve().then(action).catch(error=>setRosterNotice({text:`${w.unavailable} (${errorCode(error)})`,error:true}))
  }
  const updateSidebar=(item:W.ProjectAssistantBotSummary,roster:W.ProjectAssistantRosterEdit)=>runRoster(()=>changeRoster(item,roster))
  const applyPrompt=async(prompt:RosterPrompt,name:string,signal:AbortSignal)=>{
    switch(prompt.action){
      case "section": await changeRoster(prompt.item,{section:{name}},signal);break
      case "new-session": await changeRoster(prompt.item,{fresh_context:true},signal);break
      case "delete":
        await withBot(prompt.item,(source,s)=>source.deleteBot!({command:command(prompt.item.bot.key,"delete")},s),signal)
        removeFromList(prompt.item.bot.key)
    }
  }
  const botActions=(item:W.ProjectAssistantBotSummary):PaMenuItem[]=>{
    const {bot}=item, id=botIdentity(bot.key), source=sources.find(client=>client.machineID===bot.key.machine_id)
    const writable=!!source&&canWrite(item.source), modern=writable&&source.canRosterActions?.()===true
    const manage=writable&&source.canManage?.()===true
    return [
      {id:"read",label:w.rosterMarkRead,icon:rosterIcon("read"),disabled:!source||!readerID||!item.unread_count||!canMarkConversationRead(item.source),onSelect:()=>runRoster(async()=>{
        await withBot(item,(source,s)=>markConversationRead(source,current,bot,readerID!,s))
        await refreshAfterRead(source!)
        setRosterNotice({text:w.readMarked})
      })},
      {id:"profile",label:w.rosterEditProfile,icon:rosterIcon("edit"),disabled:!writable,onSelect:()=>{
        if(selected!==id)select(item)
        setProfileRequest(old=>({id,count:(old?.count??0)+1}))
      }},
      {id:"copy",label:w.rosterCopyID,icon:rosterIcon("copy"),disabled:!navigator.clipboard,onSelect:()=>{
        // Start the clipboard operation while the pointer or key activation is current.
        void navigator.clipboard.writeText(bot.conversation_id).then(()=>setRosterNotice({text:w.rosterCopiedID}),error=>setRosterNotice({text:`${w.unavailable} (${errorCode(error)})`,error:true}))
      }},
      {id:"session",label:w.rosterNewSession,icon:rosterIcon("renew"),disabled:!modern,onSelect:()=>setRosterPrompt({item,action:"new-session"})},
      ...(!bot.lead?[{id:"lead",label:w.rosterSetLead,icon:rosterIcon("lead"),disabled:!modern,onSelect:()=>updateSidebar(item,{designate_lead:true})}]:[]),
      ...(!bot.lead?[{id:"hide",label:w.rosterHide,icon:rosterIcon("hide"),disabled:!manage||!source?.setHidden,onSelect:()=>runRoster(async()=>{
        const result=await withBot(item,(source,s)=>source.setHidden!({command:command(bot.key,"visibility"),hidden:true},s))
        updateBot(result.bot)
        if(selected===id)back()
        setRosterNotice({text:w.rosterUpdated})
      })}]:[]),
      {id:"delete",label:w.deleteBot,icon:rosterIcon("delete"),danger:true,disabled:!manage||!source?.deleteBot,onSelect:()=>setRosterPrompt({item,action:"delete"})}
    ]
  }
  const groupOpened=(key:W.ProjectAssistantBotKey)=>{
    lastSelected.current=null
    setGroupOpen(true);setDestination(null);setSelected(null);selectRef.current?.(null)
    if(manageHistory){
      const id=botIdentity(key),previous=history.state
      setGroupRoute(id)
      writeHistory(null,id,previous?.paView===historyID.current&&!!(previous.paBot||previous.paGroup))
    }
  }
  const groupBack=()=>{
    if(!manageHistory||history.state?.paView!==historyID.current||!history.state.paGroup||history.state.paBot)return false
    history.back()
    return true
  }
  const groupOpenChanged=useCallback((open:boolean)=>{setGroupOpen(open);if(!open)lastSelected.current=null},[])
  useEffect(()=>{
    if(!voiceShortcut||handledShortcut.current===voiceShortcut||rosterLoading)return
    let target: W.ProjectAssistantBotSummary|undefined=selectedItem&&canWrite(selectedItem.source)?selectedItem:availableLead
    if(!target&&!narrow){
      let order=emptyRosterState()
      try{order=getRosterState(namespace,window.localStorage)}catch{/* The list still has its default order. */}
      target=firstCallableBot(allItems,order)
    }
    handledShortcut.current=voiceShortcut
    if(!target){changeView("list");return}
    if(selected!==botIdentity(target.bot.key))select(target)
    setCallTarget({key:botIdentity(target.bot.key),request:voiceShortcut})
  },[voiceShortcut,rosterLoading,selected,selectedItem,availableLead,narrow,namespace,allItems])
  const back=()=>{
    setDestination(null)
    if(manageHistory&&history.state?.paView===historyID.current&&history.state?.paBot){history.back();return}
    setSelected(null);selectRef.current?.(null)
  }
  useEffect(()=>{
    const follow=()=>setDestination(botFromFragment(location.hash))
    window.addEventListener("hashchange",follow)
    return()=>window.removeEventListener("hashchange",follow)
  },[])
  useEffect(()=>{
    if(!destination)return
    const matches=allItems.filter(i=>i.bot.key.bot_id===destination.bot&&(!destination.machine||i.bot.key.machine_id===destination.machine))
    // An ambiguous source never falls through to whichever machine is active.
    if(matches.length!==1)return
    if(botIdentity(matches[0].bot.key)!==selected)select(matches[0])
    setDestination(null)
  },[destination,pages,authorization,selected])
  useEffect(()=>{
    const opener=returnFocus??(document.activeElement instanceof HTMLElement?document.activeElement:null)
    const scrollX=window.scrollX,scrollY=window.scrollY
    if(manageHistory&&history.state?.paView!==historyID.current)history.pushState({...history.state,paView:historyID.current,paBot:null,paGroup:null,paDepth:1},"")
    const pop=(event:PopStateEvent)=>{
      const state=event.state
      if(state?.paView!==historyID.current){closeRef.current();return}
      const id=state.paBot??null
      setSelected(id);setGroupRoute(state.paGroup??null)
      const key=Object.values(pagesRef.current).flatMap(page=>page.items).find(row=>botIdentity(row.bot.key)===id)?.bot.key??null
      selectRef.current?.(key)
    }
    if(manageHistory)window.addEventListener("popstate",pop)
    return()=>{window.removeEventListener("popstate",pop);requestAnimationFrame(()=>{if(opener?.isConnected)opener.focus({preventScroll:true});window.scrollTo(scrollX,scrollY)})}
  },[manageHistory])
  useEffect(()=>{
    const dialog=host.current?.querySelector<HTMLElement>('[role="dialog"]')
    if(!dialog)return
    const opener=document.activeElement instanceof HTMLElement?document.activeElement:null
    dialog.querySelector<HTMLElement>("button,input,textarea,select")?.focus()
    const key=(event:KeyboardEvent)=>{
      if(event.key==="Escape"){event.preventDefault();if(creating)setCreating(false);return}
      if(event.key!=="Tab")return
      const nodes=[...dialog.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled),[tabindex="0"]')]
      if(!nodes.length)return
      if(event.shiftKey&&document.activeElement===nodes[0]){event.preventDefault();nodes.at(-1)?.focus()}
      else if(!event.shiftKey&&document.activeElement===nodes.at(-1)){event.preventDefault();nodes[0].focus()}
    }
    dialog.addEventListener("keydown",key)
    return()=>{dialog.removeEventListener("keydown",key);if(opener?.isConnected)opener.focus({preventScroll:true})}
  },[creating])
  // Pointing at or focusing a roster row raises that Bot's tag in the tower beside it.
  useEffect(()=>{
    const root=host.current
    if(!root)return
    const row=(t:EventTarget|null)=>t instanceof Element?t.closest<HTMLElement>(".pa-roster [data-bot-key]")?.dataset.botKey??null:null
    const enter=(e:Event)=>setRosterHover(row(e.target))
    const leave=(e:Event)=>{const next=row((e as PointerEvent|FocusEvent).relatedTarget);setRosterHover(next)}
    root.addEventListener("pointerover",enter);root.addEventListener("pointerout",leave)
    root.addEventListener("focusin",enter);root.addEventListener("focusout",leave)
    return()=>{root.removeEventListener("pointerover",enter);root.removeEventListener("pointerout",leave);root.removeEventListener("focusin",enter);root.removeEventListener("focusout",leave)}
  },[])
  useEffect(()=>{if(!selected&&!groupOpen&&lastSelected.current){const button=[...host.current?.querySelectorAll<HTMLButtonElement>(botsView==="tower"?'.pa-tower [data-bot-key]':'[data-bot-key]')??[]].find(b=>b.dataset.botKey===lastSelected.current);button?.focus({preventScroll:true})}},[selected,groupOpen])
  return <div ref={host} className={`project-assistants pa-shell ${selectedItem?"pa-selected":""}`} lang={language}>
    {rosterNotice&&<div className="pa-roster-notice" role={rosterNotice.error?"alert":"status"}>
      <span>{rosterNotice.text}</span><button type="button" onClick={()=>setRosterNotice(null)} aria-label={w.close}>×</button>
    </div>}
    {rosterPrompt&&<RosterActionDialog prompt={rosterPrompt} w={w} onClose={()=>setRosterPrompt(null)} apply={applyPrompt}/>}
    <BotRoster leadOptions={leadOptions} preferredLead={leadOptions?.some(option=>option.value===preferredLead)?preferredLead:""} onPreferLead={saveLead} openLead={lead?()=>select(lead):undefined} botActions={botActions} saveRoster={updateSidebar} newSection={item=>setRosterPrompt({item,action:"section"})} key={namespace} view={botsView} onView={changeView} tower={narrow?tower:undefined} rosterScope={namespace} sources={sources} current={current} readerID={readerID} onRead={refreshAfterRead} groupsItems={allItems} items={items} machines={sources.map(s=>s.machineID)} selected={selected} onOpenGroup={groupOpened} onGroupOpenChange={groupOpenChanged} groupRoute={manageHistory?groupRoute:undefined} onGroupBack={groupBack} query={query} onQuery={setQuery} machine={machine} onMachine={setMachine} onSelect={select} onClose={onClose} onCreate={statuses.some(canWrite)?()=>setCreating(true):undefined} w={w} language={language} loading={rosterLoading} unavailable={rosterUnavailable} machineNames={machineNames} projectIcons={projectIcons} hidden={hiddenItems.length?<HiddenBots items={hiddenItems} onUnhide={item=>void unhide(item).catch(()=>{})} w={w}/>:undefined} onMore={!rosterLoading&&Object.values(pages).some(p=>p.query===query&&!!p.cursor)?()=>void load(true):undefined} projectNames={projectNames}/>
    <main className="pa-main">{selectedItem&&selectedSource?<FallbackSourceScope source={selectedSource}><Chat key={JSON.stringify([namespace,selected,selectedSource.authorizationRevision])} summary={selectedItem} voiceShortcut={callTarget?.key===selected?callTarget?.request:undefined} roster={allItems.map(item=>item.bot)} editProfileRequest={profileRequest?.id===selected?profileRequest?.count:undefined} projectName={selectedItem.bot.project.kind==="personal"?w.personalWorkspace:projectNames.get(JSON.stringify([selectedItem.bot.key.machine_id,selectedItem.bot.project.project_id]))} projectIcon={projectIcons.get(JSON.stringify([selectedItem.bot.key.machine_id,selectedItem.bot.project.project_id]))} machineName={sources.length>1?machineNames.get(selectedItem.bot.key.machine_id)||w.thisMachine:undefined} source={selectedSource} current={current} accountKey={namespace} readerID={readerID} view={view} w={w} language={language} showBack={groupOpen||botsView==="tower"} backLabel={groupOpen?w.backToGroup:botsView==="tower"?w.towerBack:undefined} onBack={back} onOpenWork={onOpenWork} onRead={()=>void refreshAfterRead(selectedSource)} onDeleted={()=>removeFromList(selectedItem.bot.key)} onBotChanged={updateBot}/></FallbackSourceScope>:!narrow&&tower?tower:<p className="pa-select-prompt">{w.selectBot}</p>}</main>
    {creating&&<BotCreate sources={sources} current={current} w={w} onClose={()=>setCreating(false)} onCreated={()=>{setCreating(false);void load()}}/>}
  </div>
}

/** The phone layout puts the tower in the roster; wider screens use the main pane. */
function useNarrow() {
  const query=typeof matchMedia==="function"?matchMedia("(max-width:700px)"):undefined
  const [narrow,setNarrow]=useState(!!query?.matches)
  useEffect(()=>{
    if(!query)return
    const change=()=>setNarrow(query.matches)
    query.addEventListener("change",change)
    return()=>query.removeEventListener("change",change)
  },[])
  return narrow
}
