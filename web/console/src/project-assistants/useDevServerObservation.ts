import {useCallback,useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {ServerObservation,emptyServerObservation,type ServerObservationState} from "./dev-server-observation"

export function serverObservationScope(source:SourceClient,bot:W.ProjectAssistantBot):string {
  const p=bot.project,k=bot.key
  return JSON.stringify([source.machineID,source.authorizationRevision,k.machine_id,k.bot_id,p.machine_id,p.project_id,p.canonical_id,p.kind])
}
export interface DevServerObservation {
  source:SourceClient
  scope:string
  supported:boolean
  active:boolean
  state:ServerObservationState
  refresh:()=>void
  mutate:(write:(signal:AbortSignal)=>Promise<unknown>)=>boolean
}
interface Held {source:SourceClient;scope:string;store:ServerObservation;active:boolean}
function visible(){return typeof document==="undefined"||document.visibilityState!=="hidden"}
function activate(held:Held,active:boolean){
  const changed=held.active!==active;held.active=active;held.store.setActive(active)
  if(active&&!changed)held.store.pulse()
}

/** Shared by the title and details; callers supply existing chat read pulses. */
export function useDevServerObservation({source,bot,enabled,active=true,pulse}:{source:SourceClient;bot:W.ProjectAssistantBot;enabled:boolean;active?:boolean;pulse?:unknown}):DevServerObservation {
  const scope=serverObservationScope(source,bot),supported=source.canDevServers?.()===true&&!!source.devServers&&!!source.devServerLogs
  const writable=useRef(enabled),observing=useRef(active),held=useRef<Held|null>(null)
  writable.current=enabled;observing.current=active
  const [reading,setReading]=useState<{source:SourceClient;scope:string;value:ServerObservationState}>(()=>({source,scope,value:emptyServerObservation()}))
  useEffect(()=>{
    if(!supported){held.current=null;setReading({source,scope,value:emptyServerObservation()});return}
    const key={machine_id:bot.key.machine_id,bot_id:bot.key.bot_id}
    const store=new ServerObservation({
      read:async signal=>(await source.devServers!(key,signal)).servers,
      current:()=>source.current?.()===true,
      writable:()=>writable.current,
      changed:value=>{if(held.current?.store===store)setReading({source,scope,value})},
    })
    const owner:Held={source,scope,store,active:false};held.current=owner
    // The following effect performs the first pulse, once. A StrictMode
    // cleanup closes this owner; its second setup creates a new one.
    store.setActive(false)
    setReading({source,scope,value:store.snapshot()})
    const visibility=()=>{if(held.current===owner)activate(owner,observing.current&&visible())}
    document.addEventListener("visibilitychange",visibility)
    return()=>{document.removeEventListener("visibilitychange",visibility);store.close();if(held.current===owner)held.current=null}
  },[source,scope,supported])
  useEffect(()=>{
    const owner=held.current
    if(owner&&owner.source===source&&owner.scope===scope)activate(owner,active&&visible())
  },[source,scope,supported,active,pulse])
  const refresh=useCallback(()=>{
    if(!observing.current||!visible())return
    const owner=held.current
    if(owner&&owner.source===source&&owner.scope===scope)owner.store.refresh()
  },[source,scope])
  const mutate=useCallback((write:(signal:AbortSignal)=>Promise<unknown>)=>{
    if(!observing.current||!visible())return false
    const owner=held.current
    return !!owner&&owner.source===source&&owner.scope===scope&&owner.store.mutate(write)
  },[source,scope])
  const same=reading.source===source&&reading.scope===scope
  const state=same?reading.value:emptyServerObservation()
  // Do not wait for an effect to hide another principal/scope's prior body.
  const permitted=supported&&source.current?.()===true,observed=active&&visible()
  const value=!permitted&&supported?{...state,rows:null,fresh:false,error:"pa_revoked"}:!observed?{...state,fresh:false}:state
  return {source,scope,supported,active:permitted&&observed,state:value,refresh,mutate}
}
