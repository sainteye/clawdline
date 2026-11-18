import React from "react"
import {createRoot} from "react-dom/client"
import {Chat} from "../../src/project-assistants/Chat"
import {FallbackSourceScope} from "../../src/project-assistants/FallbackSettings"
import {createViewState} from "../../src/project-assistants/view-state"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {words} from "../../src/project-assistants/words"
import {bootCatalog} from "../../src/catalog"
import "../../src/project-assistants/styles.css"
declare const READ_MACHINE:string,READ_BOT:string
const q=new URL(location.href).searchParams,language=q.get("lang")||"en",readonly=q.has("read")
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
const grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:READ_MACHINE,authorizationRevision:"synthetic-grant",apiLevel:88,paired:true,online:true,writable:!readonly}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>{const url=new URL(input,location.origin);if(readonly)url.searchParams.set("fixture_read","1");return fetch(url.pathname+url.search,init)},()=>grant)
const key={machine_id:READ_MACHINE,bot_id:READ_BOT},got=await source.get(key,new AbortController().signal)
const summary={bot:got.bot,source:got.source,activity:"idle",activity_at:got.bot.updated_at,latest_message:"",pending_permissions:0,read_cursor:{key,conversation_id:got.conversation.id,message_seq:0,reader_id:"synthetic-reader"},unread_count:0,version:got.bot.version}
createRoot(document.getElementById("app")!).render(<main className="project-assistants"><FallbackSourceScope source={source}><Chat summary={summary} source={source} current={()=>[source]} accountKey="synthetic-account" view={createViewState(localStorage)} w={words(language)} language={language} projectName="Synthetic project" machineName="Synthetic computer" onBack={()=>{}} onBotChanged={()=>{}}/></FallbackSourceScope></main>)
