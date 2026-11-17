import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "../../src/project-assistants/Bots"
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "../../src/cloud/project-assistants"
import {errorCode} from "../../src/project-assistants/client"
import {bootCatalog} from "../../src/catalog"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"

declare const READ_MACHINE:string
const query=new URL(location.href).searchParams,language=query.get("lang")??"en",scenario=query.get("scenario")??"ready"
const phases:{phase:string;word?:string;code?:string;at:number}[]=[]
// A controlled hidden-on-entry fault, not an assertion about the real viewer.
Object.defineProperty(document,"visibilityState",{configurable:true,get:()=>"hidden"})
localStorage.setItem("ui_language",language)
await bootCatalog(tag=>"/catalog/"+tag+".json")
const carried=["status","list","projects"].map(word=>"project-assistants."+word)
const relay={
  machineDescriptor:()=>({machine:{commands:carried}}),
  _machineRequest:async(machine:string,word:string,fields:unknown,_kind:unknown,_request:unknown,options?:{signal?:AbortSignal})=>{
    phases.push({phase:"emitted",word,at:performance.now()})
    const response=await fetch("/relay?scenario="+scenario,{method:"POST",signal:options?.signal,headers:{"Content-Type":"application/json"},body:JSON.stringify({type:word,session:"__clawdline_machine__",request:"read-"+crypto.randomUUID(),...fields as object})})
    const result=await response.json()
    phases.push({phase:"returned",word,at:performance.now()})
    return result.body??{error:result.error??{code:"pa_offline"}}
  },
}
const grant={accountKey:"synthetic-cloud-account",readerID:"synthetic-viewer",machineID:READ_MACHINE,authorizationRevision:"fixed-grant",apiLevel:86,paired:true,online:true,writable:true}
const source=new ProjectAssistantSourceClient(grant,capturedAssistantRelayFetch(READ_MACHINE,()=>relay,carried),()=>grant)
// Isolate entry from group and badge traffic; the roster/client/decoder/Host
// under test are the real implementations.
source.canGroups=()=>false
const status=source.status.bind(source),list=source.list.bind(source)
source.status=async signal=>{try{const answer=await status(signal);phases.push({phase:"status-applied",at:performance.now()});return answer}catch(error){phases.push({phase:"failure",code:errorCode(error),at:performance.now()});throw error}}
source.list=async(request,signal)=>{const answer=await list(request,signal);phases.push({phase:"snapshot-received",at:performance.now()});return answer}
Object.assign(window,{rosterEntry:{phases}})
createRoot(document.getElementById("app")!).render(<Bots sources={[source]} language={language} accountKey={grant.accountKey} readerID={grant.readerID} onClose={()=>{}}/>)
