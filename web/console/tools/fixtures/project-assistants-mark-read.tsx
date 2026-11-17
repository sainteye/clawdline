import React from "react"
import {createRoot} from "react-dom/client"
import {Chat} from "../../src/project-assistants/Chat"
import {createViewState} from "../../src/project-assistants/view-state"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {words} from "../../src/project-assistants/words"
import {bootCatalog} from "../../src/catalog"
import "../../src/project-assistants/styles.css"

declare const READ_MACHINE:string, READ_BOT:string
// Keep automatic visible-row observations out of this explicit-action fixture.
Object.defineProperty(document,"hasFocus",{value:()=>false})
const q=new URL(location.href).searchParams,language=q.get("lang")||"en",readonly=q.has("read")
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
let grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:READ_MACHINE,authorizationRevision:"synthetic-grant",apiLevel:84,paired:true,online:true,writable:!readonly}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input+(readonly?(input.includes("?")?"&":"?")+"fixture_read=1":""),init),()=>grant)
const current=()=>[source],key={machine_id:READ_MACHINE,bot_id:READ_BOT}
const got=await source.get(key,new AbortController().signal)
Object.assign(window,{fixtureRevoke:()=>{grant={...grant,authorizationRevision:"changed-grant"}}})
const summary={bot:got.bot,source:got.source,activity:"idle",activity_at:got.bot.updated_at,latest_message:"",pending_permissions:0,read_cursor:{key,conversation_id:got.conversation.id,message_seq:0,reader_id:"synthetic-reader"},unread_count:0,version:got.bot.version}
createRoot(document.getElementById("app")!).render(<React.StrictMode><main className="project-assistants"><Chat summary={summary} source={source} current={current} accountKey="synthetic-account" view={createViewState(localStorage)} w={words(language)} language={language} readerID="synthetic-reader" onRead={()=>{document.documentElement.dataset.readAcknowledged="true"}} onBack={()=>{}} onBotChanged={()=>{}}/></main></React.StrictMode>)
