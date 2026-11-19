/** Real parent, roster and room; only the outer transport is a local fixture. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "../../src/project-assistants/Bots"
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "../../src/cloud/project-assistants"
import {bootCatalog} from "../../src/catalog"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"

const q=new URL(location.href).searchParams,mode=q.get("mode")??"local",language=q.get("lang")??"en"
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json")
const carried=["status","list","projects","get","view","read","observe","export","groups","group-history","group-create","group-edit","group-delete","group-send","group-react","group-stop","team-plans","team-answer","team-settings"].map(word=>"project-assistants."+word)
const relay={machineDescriptor:()=>({machine:{commands:carried}}),
 _machineRequest:async(_machine:string,word:string,fields:object,_kind:unknown,_timeout:unknown,options?:{signal?:AbortSignal})=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),signal:options?.signal})).json(),
 _machineRequestAs:async(id:string,_machine:string,word:string,fields:object)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Idempotency-Key":id}})).json(),
}
let grant={accountKey:"synthetic-navigation-"+mode,readerID:"person",machineID:"machine",authorizationRevision:"one",apiLevel:mode==="old"?87:91,paired:true,online:true,writable:mode!=="read"}
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch("machine",()=>relay,carried):(input,init)=>fetch("/proxy/"+mode+input,init),()=>grant)
// Navigation is tested against the same bounded snapshot consumer on both rails.
source.canRoster=()=>false
const sources=[source],selections:unknown[]=[]
Object.assign(window,{navigationFixture:{selections,revoke:()=>{grant={...grant,authorizationRevision:"revoked"}}}})
createRoot(document.getElementById("app")!).render(<Bots sources={sources} language={language} accountKey={grant.accountKey} readerID={grant.readerID} manageHistory={q.get("history")==="1"} onSelectionChange={key=>selections.push(key)} onClose={()=>{}}/>)
