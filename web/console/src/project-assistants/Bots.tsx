import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, command, errorCode, projectChoices, botFromFragment } from "./client"
import { BotRoster } from "./BotRoster"
import { FallbackSourceScope } from "./FallbackSettings"
import { BotCreate } from "./BotCreate"
import { SourceStatusPanel } from "./SourceStatusPanel"
import { sourceHealth } from "./source-status"
import { HiddenBots } from "./HiddenBots"
import { Chat, EventRefreshMs } from "./Chat"
import { followLiveRoster } from "./live-roster"
import { readSnapshotRoster, serialSnapshotRefresh, followSnapshotRoster } from "./snapshot-roster"
import { botIdentity, canRead, canWrite, createViewState, mergeRoster, type ViewState } from "./view-state"
import { words } from "./words"
import { shownSourceError } from "./source-errors"
import "./styles.css"

export interface BotsProps {
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
}
interface SourcePage {revision:string;query:string;items:W.ProjectAssistantBotSummary[];cursor:string;statuses:W.ProjectAssistantSourceStatus[];error:string;loadedPages?:number}
export function Bots({sources,onClose,language="en",accountKey,readerID,viewState,returnFocus,onOpenWork,manageHistory=false,onSelectionChange}:BotsProps) {
  const w=words(language),[selected,setSelected]=useState<string|null>(null),[query,setQuery]=useState(""),[machine,setMachine]=useState("")
  const [pages,setPages]=useState<Record<string,SourcePage>>({}),[loading,setLoading]=useState(true),[creating,setCreating]=useState(false)
  const [destination,setDestination]=useState(()=>botFromFragment(location.hash))
  const [livePending,setLivePending]=useState<Record<string,boolean>>({})
  const [projectNames,setProjectNames]=useState(new Map<string,string>()),[projectIcons,setProjectIcons]=useState(new Map<string,W.Icon>())
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
  const matching=allItems.filter(i=>i.bot.name.toLowerCase().includes(query.toLowerCase()))
  const items=matching.filter(i=>!i.bot.hidden),hiddenItems=matching.filter(i=>i.bot.hidden)
  const rosterLoading=loading||sources.some(s=>livePending[s.machineID])
  const unhide=async(item:W.ProjectAssistantBotSummary)=>{const source=sources.find(s=>s.machineID===item.bot.key.machine_id);if(!source?.setHidden)return;await authorized(source,current,s=>source.setHidden!({command:command(item.bot.key,"visibility"),hidden:false},s),new AbortController().signal);void load()}
  const selectedItem=allItems.find(i=>botIdentity(i.bot.key)===selected)
  const selectedSource=selectedItem?sources.find(s=>s.machineID===selectedItem.bot.key.machine_id):undefined
  const statuses=Object.entries(pages).flatMap(([id,p])=>sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)?p.statuses:[])
  const errors=Object.entries(pages).filter(([id,p])=>shownSourceError(p.error)&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([id,p])=>[id,`${w.sourceError} (${p.error})`] as [string,string])
  const machineNames=new Map(sources.map(s=>[s.machineID,s.machineName??""] as [string,string]))
  const withoutBots=Object.entries(pages).filter(([id,p])=>p.error&&!shownSourceError(p.error)&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([id])=>id)
  const health=sourceHealth(statuses,machineNames,withoutBots,w as unknown as Record<string,string>)
  const load=useCallback((append=false)=>loader.current(append),[])
  useEffect(()=>{
    const control=new AbortController()
    deniedSnapshots.current.clear()
    loader.current=serialSnapshotRefresh(async append=>{
    if(append||!Object.values(pagesRef.current).some(p=>p.query===query))setLoading(true)
    await Promise.all(current().map(async source=>{
      if(source.canRoster?.()&&source.subscribeRoster)return
      if(deniedSnapshots.current.has(source))return
      const previous=pagesRef.current[source.machineID],cursor=append&&previous?.revision===source.authorizationRevision&&previous.query===query?previous.cursor:""
      if(append&&!cursor)return
      try {
        const coverage=previous?.revision===source.authorizationRevision&&previous.query===query?previous.loadedPages??1:1
        const response=append?await authorized(source,current,async s=>{const status=await source.status(s);const page=await source.list({machine_id:source.machineID,cursor,query,page_size:100},s);return {status,items:page.items,cursor:page.next_cursor,loadedPages:coverage+1}},control.signal):await readSnapshotRoster(source,current,query,coverage,control.signal)
        const {status}=response
        if(control.signal.aborted)return
        const own=status.sources.find(s=>s.machine_id===source.machineID)
        const rows=response.items.filter(i=>i.bot.key.machine_id===source.machineID&&canRead(i.source)).map(i=>own?{...i,source:own}:i)
        setPages(old=>({...old,[source.machineID]:{revision:source.authorizationRevision,query,items:append&&old[source.machineID]?.revision===source.authorizationRevision&&old[source.machineID]?.query===query?mergeRoster([old[source.machineID].items,rows]):rows,cursor:response.cursor,loadedPages:response.loadedPages,statuses:status.sources.filter(s=>s.machine_id===source.machineID),error:""}}))
      }catch(e){if(!control.signal.aborted){const code=errorCode(e),denied=["pa_revoked","pa_scope_denied","pa_unpaired"].includes(code);if(denied)deniedSnapshots.current.add(source);setPages(old=>({...old,[source.machineID]:{revision:source.authorizationRevision,query,items:!denied&&old[source.machineID]?.revision===source.authorizationRevision&&old[source.machineID]?.query===query?old[source.machineID].items.map(i=>({...i,source:{...i.source,fresh:false,writable:false}})):[],cursor:!denied&&old[source.machineID]?.query===query?old[source.machineID]?.cursor??"":"",loadedPages:old[source.machineID]?.loadedPages,statuses:old[source.machineID]?.statuses.map(s=>({...s,fresh:false,writable:false}))??[],error:code}}))}}
    }))
    if(!control.signal.aborted)setLoading(false)
    },control.signal)
    if(selected||current().every(s=>s.canRoster?.()&&s.subscribeRoster))void load()
    return()=>control.abort()
  },[authorization,query,current])
  useEffect(()=>{setSelected(null);setPages({});setMachine("")},[authorization])
  useEffect(()=>{
    if(selected||!sources.some(s=>!s.canRoster?.()||!s.subscribeRoster))return
    const control=new AbortController()
    followSnapshotRoster(async()=>{await load();if(!current().some(s=>s.current?.()!==false&&!deniedSnapshots.current.has(s)&&(!s.canRoster?.()||!s.subscribeRoster)))control.abort()},control.signal,EventRefreshMs,document,window)
    return()=>control.abort()
  },[selected,authorization,query,load])
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
          return {...old,[source.machineID]:{revision:source.authorizationRevision,query:"",items,cursor:"",statuses:value.statuses??previous?.statuses??[],error:value.error}}
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
  const select=(item:W.ProjectAssistantBotSummary)=>{
    lastSelected.current=botIdentity(item.bot.key)
    setSelected(botIdentity(item.bot.key));selectRef.current?.(item.bot.key)
    if(manageHistory){
      const state={...history.state,paView:historyID.current,paBot:botIdentity(item.bot.key)}
      if(history.state?.paView===historyID.current&&history.state?.paBot)history.replaceState(state,"")
      else history.pushState(state,"")
    }
  }
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
    if(manageHistory&&history.state?.paView!==historyID.current)history.pushState({...history.state,paView:historyID.current,paBot:null},"")
    const pop=()=>{if(history.state?.paView===historyID.current){const identity=history.state.paBot??null;setSelected(identity);const item=pagesRef.current;const key=Object.values(item).flatMap(page=>page.items).find(row=>botIdentity(row.bot.key)===identity)?.bot.key??null;selectRef.current?.(key)}else closeRef.current()}
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
  useEffect(()=>{if(!selected&&lastSelected.current){const button=[...host.current?.querySelectorAll<HTMLButtonElement>('[data-bot-key]')??[]].find(b=>b.dataset.botKey===lastSelected.current);button?.focus({preventScroll:true})}},[selected])
  return <div ref={host} className={`project-assistants pa-shell ${selectedItem?"pa-selected":""}`} lang={language}>
    <BotRoster sources={sources} groupsItems={allItems} items={items} machines={sources.map(s=>s.machineID)} selected={selected} query={query} onQuery={setQuery} machine={machine} onMachine={setMachine} onSelect={select} onClose={onClose} onCreate={statuses.some(canWrite)?()=>setCreating(true):undefined} w={w} language={language} loading={rosterLoading} errors={errors} status={<SourceStatusPanel health={health} w={w}/>} machineNames={machineNames} projectIcons={projectIcons} hidden={hiddenItems.length?<HiddenBots items={hiddenItems} onUnhide={item=>void unhide(item).catch(()=>{})} w={w}/>:undefined} onMore={!rosterLoading&&Object.values(pages).some(p=>p.query===query&&!!p.cursor)?()=>void load(true):undefined} projectNames={projectNames}/>
    <main className="pa-main">{selectedItem&&selectedSource?<FallbackSourceScope source={selectedSource}><Chat key={JSON.stringify([namespace,selected,selectedSource.authorizationRevision])} summary={selectedItem} projectName={projectNames.get(JSON.stringify([selectedItem.bot.key.machine_id,selectedItem.bot.project.project_id]))} projectIcon={projectIcons.get(JSON.stringify([selectedItem.bot.key.machine_id,selectedItem.bot.project.project_id]))} machineName={sources.length>1?machineNames.get(selectedItem.bot.key.machine_id)||w.thisMachine:undefined} source={selectedSource} current={current} accountKey={namespace} readerID={readerID} view={view} w={w} language={language} onBack={back} onOpenWork={onOpenWork} onRead={()=>{const read=selected;setPages(old=>Object.fromEntries(Object.entries(old).map(([id,p])=>[id,{...p,items:p.items.map(i=>botIdentity(i.bot.key)===read?{...i,unread_count:0}:i)}])))}} onDeleted={()=>{const gone=selected;back();setPages(old=>Object.fromEntries(Object.entries(old).map(([id,p])=>[id,{...p,items:p.items.filter(i=>botIdentity(i.bot.key)!==gone)}])))}} onBotChanged={bot=>setPages(old=>Object.fromEntries(Object.entries(old).map(([id,p])=>[id,{...p,items:p.items.map(i=>botIdentity(i.bot.key)===botIdentity(bot.key)?{...i,bot}:i)}])))}/></FallbackSourceScope>:<p className="pa-select-prompt">{w.selectBot}</p>}</main>
    {creating&&<BotCreate sources={sources} current={current} w={w} onClose={()=>setCreating(false)} onCreated={()=>{setCreating(false);void load()}}/>}
  </div>
}
