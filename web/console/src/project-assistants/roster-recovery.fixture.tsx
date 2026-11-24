/** Synthetic faults around the real roster; no daemon, provider or external API. */
import React from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {Bots} from "./Bots"
import {fixtureSource} from "./fixtures"
import {ViewerEventLog} from "../legacy/js/net/cloud-viewer-events.js"
import {failure,type SourceClient} from "./client"
import {capturedAssistantRelayFetch,ProjectAssistantRegistry,ProjectAssistantSourceClient,type AssistantGrant} from "../cloud/project-assistants"
import "./styles.css"

function controlled(machine:string,live:boolean,initial:W.ProjectAssistantErrorCode="pa_offline") {
  const base=fixtureSource(machine)
  let code:W.ProjectAssistantErrorCode|null=initial,hold=false,release:(()=>void)|undefined,drop:(()=>void)|undefined
  let reads=0,active=0,maxActive=0,streams=0,maxStreams=0
  const cursors:string[]=[]
  const observe=async<T,>(signal:AbortSignal, read:()=>Promise<T>):Promise<T>=>{
    reads++;active++;maxActive=Math.max(maxActive,active)
    try {
      if(hold)await new Promise<void>(resolve=>{release=resolve;signal.addEventListener("abort",()=>resolve(),{once:true})})
      if(signal.aborted)throw new DOMException("Aborted","AbortError")
      if(code)throw failure(code)
      return await read()
    }finally{active--}
  }
  const source:SourceClient={...base,canRoster:()=>live,canRosterActions:()=>true,canManage:()=>true,
    async deleteBot(){throw new Error("Deletion must stay at the confirmation step in this fixture")},
    status:signal=>live?observe(signal,()=>base.status(signal)):Promise.reject(new Error("Snapshot roster must not probe machine status")),
    list:(request,signal)=>live?base.list(request,signal):observe(signal,()=>base.list(request,signal)),
    ...(live?{async subscribeRoster(request:W.ProjectAssistantRosterRequest,receive:(frame:W.ProjectAssistantRosterFrame)=>void,signal:AbortSignal){
      cursors.push(request.cursor??"");streams++;maxStreams=Math.max(maxStreams,streams)
      try {
        const response=await base.list({machine_id:machine,cursor:"",query:"",page_size:100},signal)
        receive({type:"hello",source_epoch:"fixture-epoch"})
        receive({type:"snapshot-complete",source_epoch:"fixture-epoch",cursor:"fixture-cursor",changes:response.items.map((summary,index)=>({key:summary.bot.key,revision:index+1,removed:false,summary}))})
        await new Promise<void>(resolve=>{drop=resolve;signal.addEventListener("abort",()=>resolve(),{once:true})})
        if(!signal.aborted)throw failure(code??"pa_offline")
      }finally{streams--;drop=undefined}
    }}:{}),
  }
  return {source,setCode(value:W.ProjectAssistantErrorCode|null){code=value;drop?.()},hold(){hold=true},release(){hold=false;release?.();release=undefined},snapshot:()=>({reads,active,maxActive,streams,maxStreams,cursors})}
}
function cloudRoster() {
  const registry = new ProjectAssistantRegistry("cloud-fixture", "fixture")
  const machines = new Map(["studio","notebook"].map(id=>[id,{source:fixtureSource(id),ready:false,reads:0}]))
  const word = "project-assistants.list"
  const connection = {
    machineDescriptor:()=>({machine:{commands:["transcript"]}}),
    async _machineRequest(id:string,command:string,fields:Record<string,unknown>) {
      const machine = machines.get(id)
      if(!machine||command!==word||fields.machine_id!==id)throw Error("Unexpected roster destination")
      machine.reads++
      if(!machine.ready)throw Object.assign(new Error("Relay is reconnecting"),{code:"offline"})
      return machine.source.list(fields as unknown as W.ProjectAssistantListRequest,new AbortController().signal)
    },
  }
  registry.sync([...machines.keys()].map(machineID=>({accountKey:registry.accountKey,readerID:registry.readerID,
    authorizationRevision:"fixture",machineID,apiLevel:undefined,paired:true,online:true,writable:true})),
    grant=>capturedAssistantRelayFetch(grant.machineID,()=>connection,[word]))
  Object.assign(window,{cloudRoster:{
    restore(id:string){machines.get(id)!.ready=true},
    reads(id:string){return machines.get(id)!.reads},
  }})
  return registry.getSnapshot()
}

function cloudChat() {
  const machineID = "studio"
  const base = fixtureSource(machineID)
  const list = "project-assistants.list", view = "project-assistants.view"
  const snapshots = new Map<string,unknown>()
  const grant: AssistantGrant = {accountKey:"cloud-chat-fixture",readerID:"fixture",machineID,authorizationRevision:"fixture",
    apiLevel:undefined,descriptorPending:true,paired:true,online:true,writable:true}
  let snapshotsRead = 0
  const connection = {
    orchestratorSnapshots:snapshots,
    machineDescriptor:()=>({machine:{commands:[list]}}),
    async _machineRequest(id:string,command:string,fields:Record<string,unknown>) {
      if(id!==machineID)throw Error("Unexpected chat destination")
      const signal = new AbortController().signal
      if(command===list)return base.list(fields as unknown as W.ProjectAssistantListRequest,signal)
      if(command!==view)throw Error("Unexpected chat read")
      if(fields.mode==="snapshot")snapshotsRead++
      return base.view(fields as unknown as W.ProjectAssistantReadViewRequest,signal)
    },
  }
  const relay = capturedAssistantRelayFetch(machineID,()=>connection,[list,view])
  Object.assign(window,{cloudChat:{
    advertise(supportsChat:boolean){
      grant.apiLevel=115
      grant.descriptorPending=false
      snapshots.set(machineID,{machine:{commands:supportsChat?[list,view]:[list]}})
    },
    snapshotsRead:()=>snapshotsRead,
  }})
  return [new ProjectAssistantSourceClient(grant,relay,()=>grant)]
}

const params=new URL(location.href).searchParams
const initial=params.has("retry")?"pa_invalid":"pa_offline"
const studio=controlled("studio",!params.has("snapshots"),initial),notebook=controlled("notebook",false,initial)
const language=params.get("lang")??"en"
const sources=params.has("chat")?cloudChat():params.has("cloud")?cloudRoster():[studio.source,notebook.source]
if(params.has("hold")){studio.hold();notebook.hold()}
const diagnostic = new ViewerEventLog({storage:localStorage,key:"roster-recovery-diagnostic"})
createRoot(document.getElementById("app")!).render(<Bots sources={sources} language={language} accountKey="roster-recovery-fixture" readerID="fixture" onClose={()=>{}}
  onRosterUnavailable={params.has("hold") ? evidence => diagnostic.recordImmediately("cloud.bots.roster.unavailable",evidence) : undefined}/>)
Object.assign(window,{rosterRecovery:{studio,notebook}})
