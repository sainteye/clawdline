import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import type { SourceClient } from "./client"
import { authorized, errorCode, projectChoices } from "./client"
import { BotRoster } from "./BotRoster"
import { BotCreate } from "./BotCreate"
import { SourceStatusPanel } from "./SourceStatusPanel"
import { sourceHealth } from "./source-status"
import { Chat } from "./Chat"
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
interface SourcePage {revision:string;query:string;items:W.ProjectAssistantBotSummary[];cursor:string;statuses:W.ProjectAssistantSourceStatus[];error:string}
export function Bots({sources,onClose,language="en",accountKey,readerID,viewState,returnFocus,onOpenWork,manageHistory=false,onSelectionChange}:BotsProps) {
  const w=words(language),[selected,setSelected]=useState<string|null>(null),[query,setQuery]=useState(""),[machine,setMachine]=useState("")
  const [pages,setPages]=useState<Record<string,SourcePage>>({}),[loading,setLoading]=useState(true),[creating,setCreating]=useState(false)
  const [projectNames,setProjectNames]=useState(new Map<string,string>())
  const host=useRef<HTMLDivElement>(null),sourceRef=useRef(sources),closeRef=useRef(onClose),selectRef=useRef(onSelectionChange)
  sourceRef.current=sources;closeRef.current=onClose;selectRef.current=onSelectionChange
  const current=useCallback(()=>sourceRef.current,[])
  const authorization=sources.map(s=>JSON.stringify([s.machineID,s.authorizationRevision])).join("|")
  const namespace=accountKey??authorization
  const view=useMemo(()=>{if(viewState)return viewState;try{return createViewState(window.localStorage)}catch{return createViewState()}},[viewState,namespace])
  const historyID=useRef(typeof crypto!=="undefined"?crypto.randomUUID():"")
  const lastSelected=useRef<string|null>(null)
  const controller=useRef<AbortController|null>(null),request=useRef(0)
  const pagesRef=useRef(pages);pagesRef.current=pages
  const items=mergeRoster(Object.entries(pages).filter(([id,p])=>sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([,p])=>p.items))
  const selectedItem=items.find(i=>botIdentity(i.bot.key)===selected)
  const selectedSource=selectedItem?sources.find(s=>s.machineID===selectedItem.bot.key.machine_id):undefined
  const statuses=Object.entries(pages).flatMap(([id,p])=>sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)?p.statuses:[])
  const errors=Object.entries(pages).filter(([id,p])=>shownSourceError(p.error)&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([id,p])=>[id,`${w.sourceError} (${p.error})`] as [string,string])
  const machineNames=new Map(sources.map(s=>[s.machineID,s.machineName??""] as [string,string]))
  const withoutBots=Object.entries(pages).filter(([id,p])=>p.error&&!shownSourceError(p.error)&&sources.some(s=>s.machineID===id&&s.authorizationRevision===p.revision)).map(([id])=>id)
  const health=sourceHealth(statuses,machineNames,withoutBots,w as unknown as Record<string,string>)
  const load=useCallback(async(append=false)=>{
    if(!append){controller.current?.abort();controller.current=new AbortController()}
    const control=controller.current??new AbortController(),id=++request.current
    setLoading(true)
    await Promise.all(current().map(async source=>{
      const previous=pagesRef.current[source.machineID],cursor=append&&previous?.revision===source.authorizationRevision&&previous.query===query?previous.cursor:""
      if(append&&!cursor)return
      try {
        const [status,response]=await authorized(source,current,s=>Promise.all([source.status(s),source.list({machine_id:source.machineID,cursor,query,page_size:100},s)]),control.signal)
        if(control.signal.aborted||id!==request.current)return
        const own=status.sources.find(s=>s.machine_id===source.machineID)
        const rows=response.items.filter(i=>i.bot.key.machine_id===source.machineID&&canRead(i.source)).map(i=>own?{...i,source:own}:i)
        setPages(old=>({...old,[source.machineID]:{revision:source.authorizationRevision,query,items:append&&old[source.machineID]?.revision===source.authorizationRevision&&old[source.machineID]?.query===query?mergeRoster([old[source.machineID].items,rows]):rows,cursor:response.next_cursor,statuses:status.sources.filter(s=>s.machine_id===source.machineID),error:""}}))
      }catch(e){if(!control.signal.aborted&&id===request.current)setPages(old=>({...old,[source.machineID]:{revision:source.authorizationRevision,query,items:old[source.machineID]?.revision===source.authorizationRevision?old[source.machineID].items.map(i=>({...i,source:{...i.source,fresh:false,writable:false}})):[],cursor:old[source.machineID]?.query===query?old[source.machineID]?.cursor??"":"",statuses:old[source.machineID]?.statuses??[],error:errorCode(e)}}))}
    }))
    if(!control.signal.aborted&&id===request.current)setLoading(false)
  },[authorization,query,current])
  useEffect(()=>{setSelected(null);setPages({});setMachine("");void load();return()=>controller.current?.abort()},[authorization])
  useEffect(()=>{void load();return()=>controller.current?.abort()},[query])
  useEffect(()=>{
    const control=new AbortController()
    setProjectNames(new Map())
    for(const source of sources)void authorized(source,current,async s=>{const request={machine_id:source.machineID,cursor:"",page_size:100};return projectChoices(await source.projects(request,s),request)},control.signal).then(answer=>{if(!control.signal.aborted)setProjectNames(old=>{const next=new Map(old);for(const p of answer.items)next.set(JSON.stringify([source.machineID,p.target.project_id]),p.label);return next})}).catch(()=>{})
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
    if(manageHistory&&history.state?.paView===historyID.current&&history.state?.paBot){history.back();return}
    setSelected(null);selectRef.current?.(null)
  }
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
    <BotRoster items={items} machines={sources.map(s=>s.machineID)} selected={selected} query={query} onQuery={setQuery} machine={machine} onMachine={setMachine} onSelect={select} onClose={onClose} onCreate={statuses.some(canWrite)?()=>setCreating(true):undefined} w={w} language={language} loading={loading} errors={errors} status={<SourceStatusPanel health={health} w={w}/>} machineNames={machineNames} onMore={!loading&&Object.values(pages).some(p=>p.query===query&&!!p.cursor)?()=>void load(true):undefined} projectNames={projectNames}/>
    <main className="pa-main">{selectedItem&&selectedSource?<Chat key={JSON.stringify([namespace,selected,selectedSource.authorizationRevision])} summary={selectedItem} projectName={projectNames.get(JSON.stringify([selectedItem.bot.key.machine_id,selectedItem.bot.project.project_id]))} source={selectedSource} current={current} accountKey={namespace} readerID={readerID} view={view} w={w} language={language} onBack={back} onOpenWork={onOpenWork} onBotChanged={bot=>setPages(old=>Object.fromEntries(Object.entries(old).map(([id,p])=>[id,{...p,items:p.items.map(i=>botIdentity(i.bot.key)===botIdentity(bot.key)?{...i,bot}:i)}])))}/>:<p className="pa-select-prompt">{w.selectBot}</p>}</main>
    {creating&&<BotCreate sources={sources} current={current} w={w} onClose={()=>setCreating(false)} onCreated={()=>{setCreating(false);void load()}}/>}
  </div>
}
