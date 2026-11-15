/** The real Chat and source client, served only by the private output MCP fixture. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Chat} from "./Chat"
import {words} from "./words"
import {createViewState} from "./view-state"
import {ProjectAssistantSourceClient} from "../cloud/project-assistants"
import "./styles.css"

const config=await fetch("/fixture.json").then(r=>r.json())
const grant={accountKey:"local",readerID:"fixture-reader",machineID:config.machine,authorizationRevision:"fixture-1",apiLevel:69,paired:true,online:true,writable:true}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input,init),()=>grant)
await source.status(new AbortController().signal)
const page=await source.list({machine_id:grant.machineID,query:"",cursor:"",page_size:100},new AbortController().signal)
const summary=page.items.find(b=>b.bot.key.bot_id===config.bot)!,language=new URL(location.href).searchParams.get("lang")??"en"
createRoot(document.querySelector("#app")!).render(<main className="project-assistants" style={{height:"100vh",background:"#080808"}}><Chat summary={summary} source={source} current={()=>[source]} accountKey="fixture" readerID="fixture-reader" view={createViewState()} w={words(language)} language={language} onBack={()=>{}} onBotChanged={()=>{}}/></main>)
