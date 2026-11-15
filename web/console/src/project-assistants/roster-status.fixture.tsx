/** Real Bots and source client, served by the private Store/HTTP status fixture. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "./Bots"
import {ProjectAssistantSourceClient} from "../cloud/project-assistants"
import "./styles.css"

const config=await fetch("/fixture.json").then(r=>r.json()),language=new URL(location.href).searchParams.get("lang")??"en"
let online=true
const grant={accountKey:"local",readerID:"roster-fixture",machineID:config.machine,authorizationRevision:"roster-fixture-1",apiLevel:69,paired:true,online:true,writable:true}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>{
  if(!online)return Promise.reject({code:"pa_offline"})
  return fetch(input,init)
},()=>grant)
;(window as unknown as {rosterFixture:{offline:()=>void;online:()=>void}}).rosterFixture={offline:()=>{online=false},online:()=>{online=true}}
createRoot(document.querySelector("#app")!).render(<Bots sources={[source]} language={language} accountKey="roster-fixture" readerID="roster-fixture" onClose={()=>{}} manageHistory={false}/>)
