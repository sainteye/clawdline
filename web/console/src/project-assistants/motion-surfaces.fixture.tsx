/** Synthetic source around the real host and Chat; no HTTP, provider or model. */
import React from "react"
import {createRoot} from "react-dom/client"
import {useProjectAssistantsHost} from "../pages/project-assistants"
import {fixtureSource} from "./fixtures"
import {createViewState} from "./view-state"
import {setDockAvailable} from "./BotOrb"
import {setOrbPlace} from "./orb-dock"
import type * as W from "@clawdline/contract"
import "./styles.css"

const source=fixtureSource("motion"),baseList=source.list.bind(source),baseView=source.view!.bind(source)
let phase:"idle"|"working"|"needsInput"="working",revision=1
const listeners=new Set<(frame:W.ProjectAssistantRosterFrame)=>void>()
const disconnects=new Set<()=>void>()
const query={machine_id:source.machineID,cursor:"",query:"",page_size:100}
source.list=async(r,s)=>{
  const page=await baseList(r,s)
  return {...page,items:page.items.map((item,i)=>({...item,status:i===0?phase:i===1&&phase!=="idle"?"working":"idle",activity:i===0&&phase==="working"?"Read notes.md":"",pending_permissions:i===0&&phase==="needsInput"?1:0}))}
}
source.view=async(r,s)=>{
  const page=await baseView(r,s)
  if(r.key.bot_id!==source.bots[0].key.bot_id)return page
  page.head.conversation.active_turn_id=phase==="idle"?"":"motion-turn"
  page.head.pending_permission_count=phase==="needsInput"?1:0
  if(page.snapshot){
    const turn:W.ProjectAssistantReadTurnSummary={id:"motion-turn",message_id:"motion-message",fence:page.head.current_fence,state:phase==="idle"?"succeeded":phase==="needsInput"?"waiting_permission":"running",started_at:"2026-11-16T00:00:00Z",failure_code:""}
    page.snapshot.pending_turns=phase==="idle"?[]:[turn]
    page.snapshot.last_turn=phase==="idle"?turn:undefined
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
setDockAvailable(true);setOrbPlace({docked:true,right:16,bottom:72})
const fixture={
  async phase(next:typeof phase){
    phase=next;revision++
    const page=await source.list(query,new AbortController().signal)
    for(const receive of listeners)receive({type:"changes",source_epoch:"motion",cursor:String(revision),changes:page.items.map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})
  },
  offline(){source.setState("offline");for(const disconnect of [...disconnects])disconnect()},
  subscribers:()=>listeners.size,
  writes:()=>source.log.filter(row=>["send","stop","answer","create"].includes(row.method)).length,
}
;(window as unknown as {fixtureMotion:typeof fixture}).fixtureMotion=fixture
function App(){
  const language=new URL(location.href).searchParams.get("lang")??"en"
  const host=useProjectAssistantsHost({registry:registry as never,cloud:false,language})
  return <><main className="pane-list"><h1>Sessions</h1>{host.entry}</main><footer aria-label="Status bar">{host.dock}</footer>{host.content}</>
}
createRoot(document.querySelector("#app")!).render(<App/>)
