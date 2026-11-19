import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import {errorCode} from "./client.ts"

export interface ServerObservationState {
  rows: readonly W.ProjectAssistantDevServer[] | null
  pending: boolean
  fresh: boolean
  inspect: boolean
  error: string
}
export function emptyServerObservation():ServerObservationState {
  return {rows:null,pending:false,fresh:false,inspect:false,error:""}
}
interface Flight {kind:"read"|"write";control:AbortController}
interface Options {
  read:(signal:AbortSignal)=>Promise<readonly W.ProjectAssistantDevServer[]>
  current:()=>boolean
  writable:()=>boolean
  changed:(value:ServerObservationState)=>void
}

/** One owner, one request, one coalesced read demand. No timer or write replay. */
export class ServerObservation {
  private state=emptyServerObservation()
  private flight:Flight|null=null
  private demand=false
  private blocked=false
  private active=true
  private closed=false
  private options:Options
  constructor(options:Options){this.options=options}
  snapshot():ServerObservationState{return this.state}
  private publish(change:Partial<ServerObservationState>){
    if(this.closed)return
    this.state={...this.state,...change};this.options.changed(this.state)
  }
  private authority():boolean {
    if(this.closed)return false
    if(this.options.current())return true
    this.blocked=true;this.demand=false;this.flight?.control.abort()
    this.publish({rows:null,fresh:false,error:"pa_revoked",inspect:this.state.inspect||this.flight?.kind==="write"})
    return false
  }
  private current(flight:Flight):boolean {
    return !this.closed&&this.flight===flight&&!flight.control.signal.aborted&&this.active&&this.authority()
  }
  /** A pulse comes from the existing visible-chat refresh, not a new clock. */
  pulse(){
    if(!this.authority()||!this.active||this.blocked)return
    this.demand=true;this.read()
  }
  /** Explicit inspection can recover a failed read or an uncertain mutation. */
  refresh(){
    if(!this.authority()||!this.active)return
    this.blocked=false;this.demand=true;this.read()
  }
  setActive(active:boolean){
    if(this.closed||this.active===active)return
    this.active=active
    if(active){this.pulse();return}
    this.demand=false
    const writing=this.flight?.kind==="write"
    if(writing)this.blocked=true
    this.flight?.control.abort()
    this.publish({fresh:false,...writing?{inspect:true,error:"pa_outcome_unknown"}:{}})
  }
  private read(){
    if(this.closed||!this.active||this.blocked||this.flight||!this.demand)return
    if(!this.authority())return
    this.demand=false
    const flight:Flight={kind:"read",control:new AbortController()};this.flight=flight
    this.publish({pending:true})
    void (async()=>{
      try{
        const rows=await this.options.read(flight.control.signal)
        if(!this.current(flight))return
        if(!Array.isArray(rows))throw {error:{code:"pa_invalid"}}
        this.publish({rows,fresh:true,inspect:false,error:""})
      }catch(error){
        if(!this.current(flight))return
        this.blocked=true;this.demand=false
        this.publish({fresh:false,error:errorCode(error)})
      }finally{this.finish(flight)}
    })()
  }
  /** True means local admission only; the source owns effects and receipts. */
  mutate(write:(signal:AbortSignal)=>Promise<unknown>):boolean {
    if(!this.authority()||!this.active||!this.options.writable()||this.flight||!this.state.fresh||this.state.inspect)return false
    const flight:Flight={kind:"write",control:new AbortController()};this.flight=flight
    this.demand=false;this.publish({pending:true,fresh:false,inspect:true,error:""})
    void (async()=>{
      try{
        await write(flight.control.signal)
        if(!this.current(flight))return
        this.demand=true
      }catch(error){
        if(!this.current(flight))return
        this.blocked=true;this.demand=false
        this.publish({fresh:false,inspect:true,error:errorCode(error)})
      }finally{this.finish(flight)}
    })()
    return true
  }
  private finish(flight:Flight){
    if(this.flight!==flight)return
    // An abort request is not proof that a non-cooperative request finished.
    // Retain the slot until its promise actually settles.
    this.flight=null;this.publish({pending:false});this.read()
  }
  close(){
    this.closed=true;this.demand=false;this.flight?.control.abort()
  }
}
