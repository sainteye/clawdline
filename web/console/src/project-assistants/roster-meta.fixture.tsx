/** Four roster rows, one per combination of time, dispatched count, read action and unread count. */
import React from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {BotRoster} from "./BotRoster"
import {words} from "./words"
import {activateCatalog,type CatalogTag} from "../catalog"
import zh from "../../public/catalogs/zh-Hant.json"
import type {SourceClient} from "./client"
import {getRosterState,saveRosterState} from "./roster-state"
import "./styles.css"
const params=new URL(location.href).searchParams
const language=(params.get("lang")??"en") as CatalogTag
if(language==="zh-Hant")activateCatalog(zh,language)
const key=(bot_id:string)=>({machine_id:"fixture-machine",bot_id})
const status={machine_id:"fixture-machine",state:"online",access:"interactive",fresh:true,readable:true,writable:true,capabilities:[],observed_at:""} as W.ProjectAssistantSourceStatus
const project={machine_id:"fixture-machine",project_id:"fixture-project",canonical_id:"fixture-project",kind:"repository"} as W.ProjectAssistantProjectScope
const at=new Date(Date.now()-20_000).toISOString()
const long="A long reply that keeps going well past the width of the roster so the preview has to be truncated before it reaches the right side"
const row=(name:string,i:number,extra:Partial<W.ProjectAssistantBotSummary>)=>({bot:{key:key(name),name,project,agent:"codex",conversation_id:"c-"+name,appearance:{shape:"blob",color:i%2?"#e39567":"#73cda5"}},activity_at:at,activity:"idle",latest_message:long,pending_permissions:0,unread_count:0,source:status,...extra}) as unknown as W.ProjectAssistantBotSummary
const work=(running:number)=>({running,needs_you:0,queued:0,delivered:0}) as unknown as W.ProjectAssistantDispatchedWork
const bots=[
 row("TimeOnly",0,{}),
 row("TimeUnread",1,{unread_count:3}),
 row("TimeClones",2,{activity:"running",dispatched_work:work(1)}),
 row("Everything",3,{activity:"running",dispatched_work:work(1),unread_count:1}),
]
const source={machineID:"fixture-machine",authorizationRevision:"fixture",canGroups:()=>false,current:()=>true,status:async()=>({sources:[status]})} as unknown as SourceClient
const order=params.get("order"),scope="meta-fixture-"+(order??"default")
if(order==="unread"||order==="machine")saveRosterState(scope,{...getRosterState(scope,localStorage),order},localStorage)
const names=new Map([[JSON.stringify(["fixture-machine","fixture-project"]),"Fixture Project"]])
function App(){
 return <main className="project-assistants pa-shell" style={{height:"100vh"}}><BotRoster rosterScope={scope} items={bots} machines={[source.machineID]} selected="" query="" onQuery={()=>{}} machine="" onMachine={()=>{}} onSelect={()=>{}} onClose={()=>{}} w={words(language)} language={language} loading={false} projectNames={names} sources={[source]} current={()=>[source]} readerID="fixture-reader" onRead={async()=>{}}/><section className="pa-main"/></main>
}
createRoot(document.getElementById("app")!).render(<App/>)
