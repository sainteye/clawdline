import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "../../src/project-assistants/Bots"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {bootCatalog} from "../../src/catalog"
import "../../src/project-assistants/styles.css"
import "../../src/legacy/tokens.css"
declare const READ_MACHINE:string,READ_BOT:string
const q=new URL(location.href).searchParams,language=q.get("lang")||"en",readonly=q.has("read")
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
const grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:READ_MACHINE,authorizationRevision:"synthetic-grant",apiLevel:q.has("old")?88:89,paired:true,online:true,writable:!readonly}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>{const url=new URL(input,location.origin);if(readonly)url.searchParams.set("fixture_read","1");return fetch(url.pathname+url.search,init)},()=>grant)
const key={machine_id:READ_MACHINE,bot_id:READ_BOT},got=await source.get(key,new AbortController().signal)
// Match the actual roster/chat parent instead of recreating its CSS shell.
source.canRoster=()=>false
history.replaceState(null,"",location.pathname+location.search+"#"+new URLSearchParams({bot:key.bot_id,conversation:got.conversation.id,machine:key.machine_id}))
createRoot(document.getElementById("app")!).render(<Bots sources={[source]} accountKey="synthetic-account" readerID="synthetic-reader" language={language} manageHistory={true} onClose={()=>{}}/>)
