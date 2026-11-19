/** Real Chat with manually staged source facts, using only browser-local synthetic data. */
import React from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {Chat} from "../../src/project-assistants/Chat"
import {contractVersion,fixtureSource} from "../../src/project-assistants/fixtures"
import {createViewState} from "../../src/project-assistants/view-state"
import {words} from "../../src/project-assistants/words"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"

const source=fixtureSource("stages"),bot=source.bots[0],sources=[source],current=()=>sources
source.pictures=()=>true
const signal=new AbortController().signal
const initial=await source.view!({key:bot.key,conversation_id:bot.conversation_id,mode:"snapshot",source_epoch:"",cursor:"",after_seq:0,page_size:100},signal)
const fence=initial.head.current_fence
const language=new URL(location.href).searchParams.get("lang")??"en",w=words(language)
const storageKey="synthetic-stages"
type Fact={kind:"message";id:string;author:W.ProjectAssistantMessageKind;text:string;turn:string}|{kind:"event";event:W.ProjectAssistantEvent}
const data=JSON.parse(sessionStorage.getItem(storageKey)??'{"facts":[],"sends":[],"answers":[],"stops":[]}') as {
  facts:Fact[];sends:W.ProjectAssistantSendRequest[];answers:W.ProjectAssistantPermissionAnswerRequest[];stops:W.ProjectAssistantStopRequest[]
}
const persist=()=>sessionStorage.setItem(storageKey,JSON.stringify(data))
const apply=(fact:Fact)=>fact.kind==="message"?source.scriptMessage(bot,fact.id,fact.author,fact.text,fact.turn):source.push(fact.event)
for(const fact of data.facts)apply(fact)
const originalView=source.view!
source.view=async(request,signal)=>{
  const page=await originalView({...request,cursor:request.cursor.split("!")[0]},signal)
  if(request.mode==="traces")for(const row of page.items){
    const summary=row.trace_summary
    if(summary?.category!=="agent_message_chunk")continue
    const revision=data.facts.filter(fact=>fact.kind==="event"&&fact.event.trace?.turn_id===summary.turn_id&&fact.event.trace.category===summary.category).length
    summary.detail_cursor+="!"+revision
  }
  return page
}
const add=(fact:Fact)=>{data.facts.push(fact);persist();apply(fact)}
const emit=(kind:W.ProjectAssistantEventKind,value:Partial<W.ProjectAssistantEvent>)=>
  add({kind:"event",event:{kind,id:crypto.randomUUID(),seq:0,at:new Date().toISOString(),fence,version:contractVersion,...value}})
const author=(turn:string,id:string,kind:W.ProjectAssistantMessageKind,text:string)=>add({kind:"message",turn,id,author:kind,text})
const activeTurns=new Map<string,W.ProjectAssistantTurnState>()
for(const fact of data.facts)if(fact.kind==="event"&&fact.event.turn)activeTurns.set(fact.event.turn.id,fact.event.turn.state)
const turn=(id:string,state:W.ProjectAssistantTurnState)=>{
  activeTurns.set(id,state)
  emit("turn_updated",{turn:{id,fence,message_id:"person-"+id,state,started_at:new Date().toISOString(),
    idempotency_key:data.sends.at(-1)?.command.idempotency_key??"fixture",memory_revision:1,instructions:bot.instructions}})
}
const receipt=(command:W.ProjectAssistantCommand,id:string,state:W.ProjectAssistantEffectState):W.ProjectAssistantReceiptResponse=>
  ({contract_version:contractVersion,receipt:{id:crypto.randomUUID(),command,resource_id:id,state,event_seq:0}})
let releaseSend:((outcome:"failed"|"accepted"|"unknown")=>void)|undefined
let releaseStop:(()=>void)|undefined
source.send=async request=>{
  data.sends.push(request);persist()
  const result=await new Promise<"failed"|"accepted"|"unknown">(resolve=>{releaseSend=resolve})
  const id="turn-"+data.sends.length
  if(result==="accepted"){
    author(id,"person-"+id,"user",request.text)
    turn(id,[...activeTurns.values()].includes("running")?"queued":"accepted")
  }
  return receipt(request.command,id,result)
}
source.stop=async request=>{
  data.stops.push(request);persist()
  await new Promise<void>(resolve=>{releaseStop=resolve})
  for(const [id,state] of activeTurns){
    if(id===request.turn_id)turn(id,"unknown")
    else if(state==="queued")turn(id,"cancelled")
  }
  return receipt(request.command,request.turn_id,"accepted")
}
let offer:W.ProjectAssistantPermissionOffer|undefined
for(const fact of data.facts)if(fact.kind==="event"&&fact.event.permission)offer=fact.event.permission
source.answer=async request=>{
  data.answers.push(request);persist()
  if(offer){
    offer={...offer,state:"answered",selected_option_id:request.option_id}
    emit("permission_answered",{permission:offer})
    turn(offer.turn_id,"running")
  }
  return receipt(request.command,request.offer_id,"delivered")
}
const animations:{name:string;at:number;id:string}[]=[]
document.addEventListener("animationstart",event=>{
  const el=event.target as HTMLElement,id=el.closest<HTMLElement>("[data-message-id]")?.dataset.messageId??""
  animations.push({name:event.animationName,at:Math.round(performance.now()),id})
})
const fixture={
  release:(state:"failed"|"accepted"|"unknown")=>{const done=releaseSend;releaseSend=undefined;done?.(state)},
  releaseStop:()=>{const done=releaseStop;releaseStop=undefined;done?.()},
  sending:()=>!!releaseSend,
  running:(id:string)=>turn(id,"running"),
  step:(id:string,category:string,text:string)=>emit("trace_updated",{trace:{turn_id:id,category,text,complete:false}}),
  publish:(id:string,text:string)=>author(id,"bot-"+id,"assistant",text),
  finish:(id:string)=>turn(id,"succeeded"),
  holdDetail:()=>source.holdView("detail"),
  releaseDetail:()=>source.releaseView(),
  failDetail:(value:boolean)=>source.configureViews({detailFailure:value?"pa_offline":null}),
  offer:(id:string,turnID:string,expiresIn:number)=>{
    turn(turnID,"waiting_permission")
    offer={id,turn_id:turnID,fence,native_request_id:"native-"+id,selected_option_id:"",
      state:"pending",expires_at:new Date(Date.now()+expiresIn).toISOString(),
      action:{tool_kind:"execute",title:"Inspect the synthetic workspace",detail:"synthetic command detail",diffs:[],shortened:false},
      options:[{id:"native-session",kind:"allow_always",label:"Allow for this session",allow:true,read_only:false},
        {id:"native-persistent",kind:"allow_always",label:"Always allow",allow:true,read_only:false},
        {id:"native-deny",kind:"reject_once",label:"Reject once",allow:false,read_only:false}]}
    emit("permission_offered",{permission:offer})
  },
  data,animations,
}
Object.assign(window,{stages:fixture})
const summary=(await source.list({machine_id:bot.key.machine_id,cursor:"",query:"",page_size:100},signal)).items[0]
createRoot(document.querySelector("#app")!).render(<div className="project-assistants" style={{height:"100dvh",background:"var(--pa-bg)"}}>
  <Chat summary={summary} source={source} current={current} accountKey="stages" view={createViewState(window.localStorage)}
    w={w} language={language} onBack={()=>{}} onBotChanged={()=>{}}/>
</div>)
