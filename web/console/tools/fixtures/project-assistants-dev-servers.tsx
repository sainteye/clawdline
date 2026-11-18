/** Synthetic browser-only source: no daemon, private data or model is opened. */
import React,{useState} from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {DevServers} from "../../src/project-assistants/DevServers"
import {BotDetailsPanel} from "../../src/project-assistants/BotDetailsPanel"
import {words} from "../../src/project-assistants/words"
import {bootCatalog} from "../../src/catalog"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"
const query=new URL(location.href).searchParams,language=query.get("lang")??"en"
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json")
const grant={accountKey:"local",readerID:"synthetic-person",machineID:"synthetic-machine",authorizationRevision:"one",apiLevel:query.has("old")?89:90,paired:true,online:true,writable:!query.has("readonly")}
let revoked=false
const source=new ProjectAssistantSourceClient(grant,fetch.bind(window),()=>revoked?null:grant)
await source.status(new AbortController().signal)
const project={machine_id:grant.machineID,project_id:"synthetic-project",canonical_id:"synthetic-canonical",kind:"repository" as const}
function bot(id:string):W.ProjectAssistantBot{return {key:{machine_id:grant.machineID,bot_id:id},project,name:id,agent:"codex",appearance:{shape:"blob",color:"#7fa3ff"},conversation_id:id+"-conversation",version:1,created_at:"2026-11-19T00:00:00Z",updated_at:"2026-11-19T00:00:00Z",permission_policy:"ask",instructions:{text:"Synthetic fixture",revision:1,role_snapshot_ref:""}}}
function Fixture(){
 const [selected,setSelected]=useState("Builder"),[active,setActive]=useState(true),[live,setLive]=useState(true),[frontier,setFrontier]=useState(0),w=words(language),current=bot(selected)
 return <div className="project-assistants"><nav aria-label="Synthetic controls"><button onClick={()=>setSelected(s=>s==="Builder"?"Other Bot":"Builder")}>Switch Bot</button><button onClick={()=>setActive(a=>!a)}>Toggle details</button><button onClick={()=>{revoked=true;setLive(false)}}>Revoke source</button><button onClick={()=>void fetch("/fixture/lose-next",{method:"POST"})}>Lose next reply</button><button onClick={()=>void fetch("/fixture/hold-read",{method:"POST"})}>Hold next read</button><button onClick={()=>void fetch("/fixture/release-read",{method:"POST"})}>Release read</button><button onClick={()=>void fetch("/fixture/add-server",{method:"POST"}).then(()=>setFrontier(n=>n+1))}>Complete new server</button></nav><main className="pa-server-fixture" hidden={!active}>
  <DevServers key={selected} source={source} bot={current} enabled={live&&grant.writable} active={active} frontier={frontier} w={w}/><BotDetailsPanel bot={current} projectName="Synthetic Project" machineName="Synthetic computer" onSettings={()=>{}} onRoutines={()=>{}} w={w}/>
 </main></div>
}
createRoot(document.getElementById("app")!).render(<Fixture/> )
