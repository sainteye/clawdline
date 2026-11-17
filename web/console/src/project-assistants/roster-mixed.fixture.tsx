/** Synthetic records exercise actual roster and room components without a daemon or provider. */
import React,{useState} from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {BotRoster} from "./BotRoster"
import {words} from "./words"
import {activateCatalog,type CatalogTag} from "../catalog"
import zh from "../../public/catalogs/zh-Hant.json"
import type {SourceClient} from "./client"
import type {RosterRoom} from "./roster-conversations"
import "./styles.css"
const language=(new URL(location.href).searchParams.get("lang")??"en") as CatalogTag
if(language==="zh-Hant")activateCatalog(zh,language)
const key=(bot_id:string)=>({machine_id:"fixture-machine",bot_id})
const status={machine_id:"fixture-machine",state:"online",access:"interactive",fresh:true,readable:true,writable:true,capabilities:[],observed_at:""} as W.ProjectAssistantSourceStatus
const project={machine_id:"fixture-machine",project_id:"fixture-project",canonical_id:"fixture-project",kind:"repository"} as W.ProjectAssistantProjectScope
const bots=["One","Two","Three","Four","Five","Six"].map((name,i)=>({bot:{key:key(name),name,project,agent:"codex",appearance:{shape:"blob",color:i%2?"#e39567":"#73cda5"}},activity_at:"2026-11-18T01:00:00Z",activity:"idle",latest_message:"An earlier Bot reply",pending_permissions:0,unread_count:0,source:status} as W.ProjectAssistantBotSummary))
const group:RosterRoom={key:key("room"),name:"Release discussion",project,members:bots.slice(0,2).map(item=>({key:item.bot.key,joined_revision:1})),conversation_id:"room-conversation",created_at:"2026-11-17T00:00:00Z",updated_at:"2026-11-17T00:00:00Z",membership_revision:1,version:1,deleted:false,last_entry:{seq:2,kind:"member",author:key("Two"),text:"The final group reply is ready for review.",at:"2026-11-18T02:00:00Z"},room:{epoch:1,pending:0,running:0,revision:2,stopped_at:0}}
const plain={...group,key:key("old-room"),name:"Older source without a preview",last_entry:undefined,room:undefined}
const many={...group,key:key("many"),name:"Six member room",members:bots.map(item=>({key:item.bot.key,joined_revision:1}))}
const counts={groups:0,history:0,writes:0};(window as unknown as {fixture:typeof counts}).fixture=counts
const source={machineID:"fixture-machine",authorizationRevision:"fixture",canGroups:()=>true,current:()=>true,status:async()=>({sources:[status]}),groups:async()=>{counts.groups++;return {groups:[group,plain,many],source:status}},groupHistory:async({key:target}:W.ProjectAssistantGroupHistoryRequest)=>{counts.history++;const chosen=[group,plain,many].find(room=>room.key.bot_id===target.bot_id)!;return {group:chosen,room:group.room,lines:chosen.last_entry?[chosen.last_entry]:[],source:status,has_more:false,next_before_seq:0}}} as unknown as SourceClient
const sources=[source],names=new Map([[JSON.stringify(["fixture-machine","fixture-project"]),"Fixture Project"]])
function App(){
 const [query,setQuery]=useState(""),[selected,setSelected]=useState("")
 return <main className="project-assistants pa-shell" style={{height:"100vh"}}><BotRoster rosterScope="mixed-fixture" items={bots.filter(item=>!query||item.bot.name.toLowerCase().includes(query.toLowerCase())||"fixture project".includes(query.toLowerCase()))} groupsItems={bots} machines={[source.machineID]} selected={selected} query={query} onQuery={setQuery} machine="" onMachine={()=>{}} onSelect={item=>setSelected(JSON.stringify([item.bot.key.machine_id,item.bot.key.bot_id]))} onClose={()=>{}} w={words(language)} language={language} loading={false} errors={[]} projectNames={names} sources={sources}/><section className="pa-main"><output>{selected}</output></section></main>
}
createRoot(document.querySelector("#app")!).render(<App/>)
