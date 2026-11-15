/** Actual source and Bots UI, backed by the private same-store HTTP fixture. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "./Bots"
import {ProjectAssistantSourceClient} from "../cloud/project-assistants"
import "./styles.css"

const config=await fetch("/fixture.json").then(r=>r.json()),params=new URL(location.href).searchParams
const reader=params.get("reader")??"reader",language=params.get("lang")??"en"
const grant={accountKey:"local",readerID:reader,machineID:config.machine,authorizationRevision:"live-fixture-1",apiLevel:71,paired:true,online:true,writable:params.get("readonly")!=="1"}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input,{...init,headers:{...Object.fromEntries(new Headers(init?.headers)),"X-Fixture-Reader":reader}}),()=>grant)
;(window as unknown as {rosterFixture:{revoke:()=>void}}).rosterFixture={revoke:()=>source.revoke()}
const sources=[source]
createRoot(document.querySelector("#app")!).render(<Bots sources={sources} language={language} accountKey={`live-fixture-${reader}`} readerID={reader} onClose={()=>{}} manageHistory={false}/>)
