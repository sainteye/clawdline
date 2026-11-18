/** Deliberately different candidate/member orders over the real source client. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Groups} from "../../src/project-assistants/Groups"
import {GroupRosterRow} from "../../src/project-assistants/GroupRosterRow"
import {ProjectAssistantSourceClient,capturedAssistantRelayFetch} from "../../src/cloud/project-assistants"
import {bootCatalog} from "../../src/catalog"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"
const q=new URL(location.href).searchParams,mode=q.get("mode")??"local",language=q.get("lang")??"en"
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json")
const carried=["status","list","projects","get","groups","group-history","group-create","group-edit","group-delete","group-send","group-stop","team-plans","team-answer","team-settings"].map(word=>"project-assistants."+word)
const relay={machineDescriptor:()=>({machine:{commands:carried}}),_machineRequest:async(_machine:string,word:string,fields:object,_kind:unknown,_timeout:unknown,options?:{signal?:AbortSignal})=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),signal:options?.signal})).json(),_machineRequestAs:async(id:string,_machine:string,word:string,fields:object)=>await(await fetch("/relay/"+word,{method:"POST",body:JSON.stringify(fields),headers:{"Idempotency-Key":id}})).json()}
const grant={accountKey:"synthetic-group-order",readerID:"person",machineID:"machine",authorizationRevision:"one",apiLevel:88,paired:true,online:true,writable:true}
const source=new ProjectAssistantSourceClient(grant,mode==="cloud"?capturedAssistantRelayFetch("machine",()=>relay,carried):(input,init)=>fetch("/proxy/local"+input,init),()=>grant)
await source.status(new AbortController().signal)
const page=await source.list({machine_id:"machine",query:"",cursor:"",page_size:100},new AbortController().signal)
// Candidate ordering is an input: a name-sorted list must not reorder a room.
const items=[...page.items].sort((a,b)=>a.bot.name.localeCompare(b.bot.name)),sources=[source]
createRoot(document.getElementById("app")!).render(<section className="pa-shell"><aside className="pa-roster"><Groups sources={sources} items={items} projectNames={new Map([[JSON.stringify(["machine","project"]),"Synthetic Project"]])}>{(rows,open)=>rows.map(entry=><GroupRosterRow key={entry.group.key.bot_id} entry={entry} bots={items.map(item=>item.bot)} selected={false} onSelect={target=>open(entry,target)}/>)}</Groups></aside><main className="pa-main"/></section>)
