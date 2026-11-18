import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "../../src/project-assistants/Bots"
import {createViewState} from "../../src/project-assistants/view-state"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {bootCatalog} from "../../src/catalog"
import "../../src/project-assistants/styles.css"

declare const READ_MACHINE:string, READ_BOT:string
// Keep automatic visible-row observations out of this explicit-action fixture.
Object.defineProperty(document,"hasFocus",{value:()=>false})
const q=new URL(location.href).searchParams,language=q.get("lang")||"en",readonly=q.has("read")
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
let grant={accountKey:"synthetic-account",readerID:"synthetic-reader",machineID:READ_MACHINE,authorizationRevision:"synthetic-grant",apiLevel:84,paired:true,online:true,writable:!readonly}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>{
  const url=new URL(input,location.origin)
  if(readonly)url.searchParams.set("fixture_read","1")
  if(q.has("force_unread"))url.searchParams.set("fixture_force_unread","1")
  return fetch(url,init)
},()=>grant)
Object.assign(window,{fixtureRevoke:()=>{grant={...grant,authorizationRevision:"changed-grant"}}})
createRoot(document.getElementById("app")!).render(<React.StrictMode><main className="project-assistants" style={{height:"100dvh"}}><Bots sources={[source]} accountKey="synthetic-account" viewState={createViewState(localStorage)} language={language} readerID="synthetic-reader" onClose={()=>{}} onRosterObserved={rows=>{
  if(rows.some(row=>row.bot.key.bot_id===READ_BOT&&row.unread_count===0))document.documentElement.dataset.readAcknowledged="true"
}}/></main></React.StrictMode>)
