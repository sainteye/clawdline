/** Actual Bots UI and Cloud SourceClient against the send-lifetime Go fixture. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "./Bots"
import {ProjectAssistantSourceClient} from "../cloud/project-assistants"
import "./styles.css"

const config=await fetch("/fixture.json").then(r=>r.json())
const grant={accountKey:"fixture-cloud",readerID:"reader",machineID:config.machine,authorizationRevision:"send-lifetime-1",apiLevel:71,paired:true,online:true,writable:true}
const source=new ProjectAssistantSourceClient(grant,(input,init)=>fetch(input,init),()=>grant)
createRoot(document.querySelector("#app")!).render(<Bots sources={[source]} language="en" accountKey="send-lifetime" readerID="reader" onClose={()=>{}} manageHistory/>)
// The product's own back control; its label is an icon the harness cannot click by text.
for(const [label,run] of [["Fixture revoke",()=>source.revoke()],["Fixture reload",()=>location.reload()],["Fixture leave chat",()=>document.querySelector<HTMLButtonElement>(".pa-chat-back")?.click()]] as const){const b=document.createElement("button");b.textContent=label;b.onclick=run;document.body.append(b)}
// One line the harness reads: what the person can see and do in the chat now.
const report=document.createElement("output");report.id="fixture-observation";document.body.append(report)
setInterval(()=>{
  const box=document.querySelector<HTMLTextAreaElement>(".pa-composer textarea"),replies=[...document.querySelectorAll(".pa-message")].filter(m=>m.textContent?.includes("Scripted reply to")).length
  const value=`obs chat=${box?1:0} composer=${box?(box.disabled?"disabled":"enabled"):"none"} held=${document.querySelectorAll(".pa-held-send").length} sending=${document.querySelectorAll(".pa-sending").length} replies=${replies} draft=[${box?.value??""}] end`
  if(report.textContent!==value)report.textContent=value
},100)
