import React from "react"
import {createRoot} from "react-dom/client"
import {Chat} from "../../src/project-assistants/Chat"
import {GroupRoom} from "../../src/project-assistants/GroupRoom"
import {FallbackSourceScope} from "../../src/project-assistants/FallbackSettings"
import {createViewState} from "../../src/project-assistants/view-state"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {words} from "../../src/project-assistants/words"
import {bootCatalog} from "../../src/catalog"
import "../../src/project-assistants/styles.css"
declare const READ_MACHINE:string,READ_BOT:string,ALIGN_GROUP:import("@clawdline/contract").ProjectAssistantGroup,ALIGN_BOTS:import("@clawdline/contract").ProjectAssistantBot[]
Object.defineProperty(document,"hasFocus",{value:()=>false})
const q=new URL(location.href).searchParams,language=q.get("lang")||"en",readonly=q.has("read"),room=q.has("room")
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
let grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:room?ALIGN_GROUP.key.machine_id:READ_MACHINE,authorizationRevision:"synthetic-grant",apiLevel:84,paired:true,online:true,writable:!readonly}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>{const url=new URL(input,location.origin);if(room)url.searchParams.set("fixture_group","1");if(readonly)url.searchParams.set("fixture_read","1");return fetch(url.pathname+url.search,init)},()=>grant),current=()=>[source],w=words(language)
Object.assign(window,{fixtureRevoke:()=>{grant={...grant,authorizationRevision:"changed-grant"}}})
const root=createRoot(document.getElementById("app")!)
if(room){await source.status(new AbortController().signal);root.render(<main className="project-assistants"><GroupRoom source={source} current={current} group={ALIGN_GROUP} bots={ALIGN_BOTS} projectNames={new Map()} onClose={()=>{}} onChanged={()=>{}}/></main>)}
else {
  const key={machine_id:READ_MACHINE,bot_id:READ_BOT},got=await source.get(key,new AbortController().signal)
  const summary={bot:got.bot,source:got.source,activity:"idle",activity_at:got.bot.updated_at,latest_message:"",pending_permissions:0,read_cursor:{key,conversation_id:got.conversation.id,message_seq:0,reader_id:"synthetic-reader"},unread_count:0,version:got.bot.version}
  root.render(<main className="project-assistants"><FallbackSourceScope source={source}><Chat summary={summary} source={source} current={current} accountKey="synthetic-account" view={createViewState(localStorage)} w={w} language={language} projectName="Synthetic project" machineName="Synthetic computer" onBack={()=>{}} onBotChanged={()=>{}}/></FallbackSourceScope></main>)
}
