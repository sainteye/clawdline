/** Synthetic source around the real host and Chat; no HTTP, provider or model. */
import React from "react"
import {createRoot} from "react-dom/client"
import {useProjectAssistantsHost} from "../pages/project-assistants"
import {fixtureSource} from "./fixtures"
import {createViewState} from "./view-state"
import {Chat} from "./Chat"
import {FallbackSourceScope} from "./FallbackSettings"
import {words} from "./words"
import type * as W from "@clawdline/contract"
import "./styles.css"

const source=fixtureSource("motion"),baseList=source.list.bind(source),baseView=source.view!.bind(source)
const failureField=new URL(location.href).searchParams.get("failure")??""
let phase:"idle"|"working"|"needsInput"|"error"=failureField?"error":"working",revision=1
let background:"waiting"|"active"|"waiting_permission"|undefined,backgroundActivity=""
const listeners=new Set<(frame:W.ProjectAssistantRosterFrame)=>void>()
const disconnects=new Set<()=>void>()
const query={machine_id:source.machineID,cursor:"",query:"",page_size:100}
source.list=async(r,s)=>{
  const page=await baseList(r,s)
  return {...page,items:page.items.map((item,i)=>({...item,status:i===0?phase:i===1&&phase!=="idle"?"working":"idle",activity:i===0&&phase==="working"?"Read notes.md":"",pending_permissions:i===0&&phase==="needsInput"?1:0,
    background_work:i===0&&background?{working:Number(background==="active"),waiting:Number(background==="waiting"),needs_input:Number(background==="waiting_permission")}:undefined}))}
}
source.view=async(r,s)=>{
  const page=await baseView(r,s)
  if(r.key.bot_id!==source.bots[0].key.bot_id)return page
  const terminal=phase==="idle"||phase==="error"
  page.head.conversation.active_turn_id=terminal?"":"motion-turn"
  page.head.unknown_turn_count=phase==="error"?1:0
  page.head.pending_permission_count=phase==="needsInput"?1:0
  if(background){
    page.head.background_tasks=[{
      id:"fixture-background", title:"Check the notes", origin_message_id:"motion-message", state:background,
      ...(backgroundActivity?{activity:backgroundActivity}:{}),
      ...(background==="waiting"?{}:{started_at:new Date().toISOString(),worker:{...r.key,bot_id:"thread-fixture-background"}}),
    }]
  }
  if(page.snapshot){
    const turn:W.ProjectAssistantReadTurnSummary={id:"motion-turn",message_id:"motion-message",fence:page.head.current_fence,state:phase==="error"?"unknown":phase==="idle"?"succeeded":phase==="needsInput"?"waiting_permission":"running",started_at:"2026-11-16T00:00:00Z",failure_code:phase==="error"?"execution_effect_unknown":"",failure_field:failureField}
    page.snapshot.pending_turns=terminal?[]:[turn]
    page.snapshot.last_turn=terminal?turn:undefined
  }
  return page
}
source.canRoster=()=>true
source.subscribeRoster=async(r,receive,signal)=>{
  receive({type:"hello",source_epoch:"motion"})
  const page=await source.list(query,signal)
  receive({type:"snapshot-complete",source_epoch:"motion",cursor:String(revision),changes:page.items.map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})
  listeners.add(receive)
  await new Promise<void>((resolve,reject)=>{
    const fail=()=>{listeners.delete(receive);disconnects.delete(fail);reject({code:"pa_offline"})}
    disconnects.add(fail)
    signal.addEventListener("abort",()=>{listeners.delete(receive);disconnects.delete(fail);resolve()},{once:true})
    if(signal.aborted)resolve()
  })
}
const sources=[source],registry={accountKey:"motion",readerID:"motion",viewState:createViewState(),subscribe:()=>()=>{},getSnapshot:()=>sources}
async function publish(){
  revision++
  const page=await source.list(query,new AbortController().signal)
  for(const receive of listeners)receive({type:"changes",source_epoch:"motion",cursor:String(revision),changes:page.items.map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})
}
const fixture={
  phase(next:typeof phase){phase=next;return publish()},
  background(next:typeof background,activity=""){background=next;backgroundActivity=activity;return publish()},
  offline(){source.setState("offline");for(const disconnect of [...disconnects])disconnect()},
  subscribers:()=>listeners.size,
  writes:()=>source.log.filter(row=>["send","stop","answer","create"].includes(row.method)).length,
}
;(window as unknown as {fixtureMotion:typeof fixture}).fixtureMotion=fixture
function App(){
  const language=new URL(location.href).searchParams.get("lang")??"en"
  const host=useProjectAssistantsHost({registry:registry as never,cloud:false,language})
  return <><main className="pane-list"><h1>Sessions</h1></main><footer aria-label="Status bar">{host.dock}</footer>{host.content}</>
}
function FailureChat(){
  const [summary,setSummary]=React.useState<W.ProjectAssistantBotSummary>()
  const language=new URL(location.href).searchParams.get("lang")??"en"
  React.useEffect(()=>{void source.list(query,new AbortController().signal).then(page=>setSummary(page.items[0]))},[])
  return summary?<div className="project-assistants" style={{height:"100vh"}}><FallbackSourceScope source={source}><Chat summary={summary} source={source} current={()=>sources} accountKey="motion" view={registry.viewState} w={words(language)} language={language} onBack={()=>{}} onBotChanged={()=>{}}/></FallbackSourceScope></div>:null
}
createRoot(document.querySelector("#app")!).render(failureField?<FailureChat/>:<App/>)
