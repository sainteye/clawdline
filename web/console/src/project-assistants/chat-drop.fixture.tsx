/** The real Chat on a synthetic source: file drops, and a run of Bot exchange capsules. No HTTP, provider or model. */
import React from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {Chat} from "./Chat"
import {fixtureSource} from "./fixtures"
import {createViewState} from "./view-state"
import {words} from "./words"
import "./styles.css"
import "./chat-presentation.css"
import "./ExchangePanel.css"

const params=new URL(location.href).searchParams,language=params.get("lang")??"en"
const source=fixtureSource("drop"),[own,atlas]=source.bots
atlas.name="Atlas"
// "images": the Bot takes pictures only; "files": pictures and general files.
source.pictures=()=>true
if(params.get("accept")==="files"){
  source.canInputs=()=>true
  source.upload=async()=>{throw new Error("The fixture never uploads")}
}
for(let i=0;i<5;i++){
  const message=source.scriptMessage(own,`exchange-${i}`,"notice","Review requested",`exchange-turn-${i}`)
  const exchange:W.ProjectAssistantExchange={id:`exchange-${i}`,project:own.project,source:{key:atlas.key,conversation_id:atlas.conversation_id},target:{key:own.key,conversation_id:own.conversation_id},source_turn_id:`atlas-turn-${i}`,request_turn_id:`exchange-turn-${i}`,request:"Review the plan",outcome:"completed",revision:1,hops:1}
  Object.assign(message,{exchange})
}
const state=params.get("state")
if(state==="readonly")source.setState("online","history_only")
const page=await source.list({machine_id:source.machineID,query:"",cursor:"",page_size:100},new AbortController().signal)
if(state==="offline")source.setState("offline")
const summary=page.items.find(item=>item.bot.key.bot_id===own.key.bot_id)!
createRoot(document.querySelector("#app")!).render(<main className="project-assistants" style={{height:"100dvh",background:"#080808"}}>
  <Chat summary={summary} source={source} current={()=>[source]} accountKey="drop" readerID="fixture" view={createViewState(localStorage)} w={words(language)} language={language} onBack={()=>{}} onBotChanged={()=>{}} roster={source.bots} showBack/>
</main>)
