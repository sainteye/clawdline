import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "../../src/project-assistants/Bots"
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "../../src/cloud/project-assistants"
import {bootCatalog} from "../../src/catalog"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"

declare const READ_MACHINE:string
const query=new URL(location.href).searchParams,language=query.get("lang")??"en"
let visibility="hidden"
Object.defineProperty(document,"visibilityState",{configurable:true,get:()=>visibility})
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
const carried=["status","list","projects","view","send","read","export"].map(word=>"project-assistants."+word)
const phases:unknown[]=[]
async function call(id:string,word:string,fields:object,signal?:AbortSignal){
  phases.push({phase:"emitted",word,...fields,at:performance.now()})
  const response=await fetch("/relay",{method:"POST",signal,headers:{"Content-Type":"application/json"},body:JSON.stringify({type:word,session:"__clawdline_machine__",request:id,...fields})})
  const result=await response.json(),answer=result.body??{error:result.error??{code:"pa_offline"}}
  phases.push({phase:"returned",word,...fields,receipt:answer.receipt,head:answer.head,pending:answer.snapshot?.pending_turns,lastTurn:answer.snapshot?.last_turn,events:answer.items?.flatMap((row:{event?:{turn?:unknown}})=>row.event?.turn?[row.event.turn]:[]),at:performance.now()})
  return answer
}
const relay={machineDescriptor:()=>({machine:{commands:carried}}),
  _machineRequest:async(_machine:string,word:string,fields:object,_kind:unknown,_timeout:unknown,options?:{signal?:AbortSignal})=>call("read-"+crypto.randomUUID(),word,fields,options?.signal),
  _machineRequestAs:async(id:string,_machine:string,word:string,fields:object)=>call(id,word,fields),
}
const grant={accountKey:"synthetic-cloud-account",readerID:"synthetic-viewer",machineID:READ_MACHINE,authorizationRevision:"fixed-grant",apiLevel:86,paired:true,online:true,writable:true}
const source=new ProjectAssistantSourceClient(grant,capturedAssistantRelayFetch(READ_MACHINE,()=>relay,carried),()=>grant)
source.canGroups=()=>false
const sources=[source]
Object.assign(window,{chatObservation:{phases,visibility:(next:string)=>{visibility=next;document.dispatchEvent(new Event("visibilitychange"))},revoke:()=>source.revoke()}})
createRoot(document.getElementById("app")!).render(<Bots sources={sources} language={language} accountKey={grant.accountKey} readerID={grant.readerID} onClose={()=>{}}/>)
