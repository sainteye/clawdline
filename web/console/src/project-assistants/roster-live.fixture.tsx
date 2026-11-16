/** Actual source and Bots UI, backed by the private same-store HTTP fixture. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "./Bots"
import {ProjectAssistantSourceClient} from "../cloud/project-assistants"
import "./styles.css"

const config=await fetch("/fixture.json").then(r=>r.json()),params=new URL(location.href).searchParams
const reader=params.get("reader")??"reader",language=params.get("lang")??"en"
const grant={accountKey:params.has("cloud")?"fixture-cloud":"local",readerID:reader,machineID:config.machine,authorizationRevision:"live-fixture-1",apiLevel:71,paired:true,online:true,writable:params.get("readonly")!=="1"}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input,{...init,headers:{...Object.fromEntries(new Headers(init?.headers)),"X-Fixture-Reader":reader}}),()=>grant)
;(window as unknown as {rosterFixture:{revoke:()=>void}}).rosterFixture={revoke:()=>source.revoke()}
const sources=[source]
createRoot(document.querySelector("#app")!).render(<Bots sources={sources} language={language} accountKey={`live-fixture-${reader}`} readerID={reader} onClose={()=>{}} manageHistory={params.has("cloud")}/>)
if(params.has("cloud")) {
  const report=document.createElement("output");report.id="fixture-observation";document.body.append(report)
  const back=document.createElement("button");back.textContent="Fixture history back";back.onclick=()=>history.back();document.body.append(back)
  for(const hidden of [true,false]){const button=document.createElement("button");button.textContent=hidden?"Fixture background":"Fixture foreground";button.onclick=()=>{Object.defineProperty(document,"visibilityState",{configurable:true,value:hidden?"hidden":"visible"});document.dispatchEvent(new Event("visibilitychange"))};document.body.append(button)}
  const revoke=document.createElement("button");revoke.textContent="Fixture revoke";revoke.onclick=()=>source.revoke();document.body.append(revoke)
  new MutationObserver(()=>{const rows=[...document.querySelectorAll<HTMLElement>("[data-bot-key]")];const value=rows.filter(row=>JSON.parse(row.dataset.botKey!)[1]==="reading").map(row=>`reading=${row.dataset.status}`).join(" ")+` rows=${rows.length} chatWorking=${!!document.querySelector(".pa-working")}`;if(report.textContent!==value)report.textContent=value}).observe(document.querySelector("#app")!,{subtree:true,childList:true,attributes:true,attributeFilter:["data-status"]})
}
