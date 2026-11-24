/** Synthetic browser-only source: no daemon, private data or model is opened. */
import React,{useState} from "react"
import {createRoot} from "react-dom/client"
import type * as W from "@clawdline/contract"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {DevServers} from "../../src/project-assistants/DevServers"
import {Chat} from "../../src/project-assistants/Chat"
import {FallbackSourceScope} from "../../src/project-assistants/FallbackSettings"
import {fixtureSource} from "../../src/project-assistants/fixtures"
import {createViewState} from "../../src/project-assistants/view-state"
import {words} from "../../src/project-assistants/words"
import {bootCatalog} from "../../src/catalog"
import "../../src/legacy/tokens.css"
import "../../src/project-assistants/styles.css"
const query=new URL(location.href).searchParams,language=query.get("lang")??"en"
localStorage.setItem("ui_language",language);await bootCatalog(tag=>"/catalog/"+tag+".json")
const grant={accountKey:query.has("remote")?"synthetic-cloud":"local",readerID:"synthetic-person",machineID:"synthetic-machine",authorizationRevision:"one",apiLevel:query.has("old")?91:query.has("future")?93:92,paired:true,online:true,writable:!query.has("readonly")}
// Observe this fixture's copy request without changing the person's clipboard.
Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:async(text:string)=>{
 await fetch("/fixture/copy",{method:"POST",body:JSON.stringify({text})})
 if(query.has("copy-fails"))throw new DOMException("synthetic clipboard refusal","NotAllowedError")
}}})
const nativeOpen=window.open.bind(window)
window.open=((url?:string|URL,target?:string,features?:string)=>{
 void fetch("/fixture/open",{method:"POST",body:JSON.stringify({url:String(url),target,features})})
 return nativeOpen(url,target,features)
}) as typeof window.open
let revoked=false
const fixtureFetch=(path:string,init?:RequestInit)=>{
 const headers=new Headers(init?.headers);headers.set("X-Synthetic-Server-Support",query.get("support")??"true")
 return fetch(path,{...init,headers})
}
const source=new ProjectAssistantSourceClient(grant,fixtureFetch,()=>revoked?null:grant)
await source.status(new AbortController().signal)
const project={machine_id:grant.machineID,project_id:"synthetic-project",canonical_id:"synthetic-canonical",kind:"repository" as const}
function bot(id:string):W.ProjectAssistantBot{return {key:{machine_id:grant.machineID,bot_id:id},project,name:id,agent:"codex",appearance:{shape:"blob",color:"#7fa3ff"},conversation_id:id+"-conversation",version:1,created_at:"2026-11-19T00:00:00Z",updated_at:"2026-11-19T00:00:00Z",permission_policy:"ask",instructions:{text:"Synthetic fixture",revision:1,role_snapshot_ref:""}}}
function Fixture(){
 const [selected,setSelected]=useState("Builder"),[active,setActive]=useState(true),[live,setLive]=useState(true),[frontier,setFrontier]=useState(0),w=words(language),current=bot(selected)
 return <div className="project-assistants"><nav aria-label="Synthetic controls"><button onClick={()=>setSelected(s=>s==="Builder"?"Other Bot":"Builder")}>Switch Bot</button><button onClick={()=>setActive(a=>!a)}>Toggle details</button><button onClick={()=>{revoked=true;setLive(false)}}>Revoke source</button><button onClick={()=>void fetch("/fixture/lose-next",{method:"POST"})}>Lose next reply</button><button onClick={()=>void fetch("/fixture/invalid-next",{method:"POST"})}>Invalidate next reply</button><button onClick={()=>void fetch("/fixture/hold-read",{method:"POST"})}>Hold next read</button><button onClick={()=>void fetch("/fixture/release-read",{method:"POST"})}>Release read</button><button onClick={()=>void fetch("/fixture/add-server",{method:"POST"}).then(()=>setFrontier(n=>n+1))}>Complete new server</button></nav><main className="pa-server-fixture" hidden={!active}>
  <DevServers key={selected} source={source} bot={current} enabled={live&&grant.writable} active={active} frontier={frontier} w={w}/>
 </main></div>
}
const base=fixtureSource(grant.machineID)
const view=base.view!.bind(base)
const chatSource=Object.assign(base,{
 authorizationRevision:source.authorizationRevision,
 current:()=>source.current(),browserRemote:source.browserRemote,
 canSourceRules:()=>source.canSourceRules(),canDevServers:()=>source.canDevServers(),devServers:source.devServers.bind(source),devServerLogs:source.devServerLogs.bind(source),controlDevServer:source.controlDevServer.bind(source),
 view:async(...args:Parameters<typeof view>)=>{const result=await view(...args);void fetch("/fixture/chat-observed",{method:"POST",body:JSON.stringify({mode:args[0].mode,event_frontier:result.head.event_frontier})});return result},
})
const page=await chatSource.list({machine_id:grant.machineID,cursor:"",query:"",page_size:100},new AbortController().signal)
const chatView=createViewState(localStorage)
function FullChatFixture(){
 const [selected,setSelected]=useState(0),[revision,setRevision]=useState(0),summary=page.items[selected],w=words(language)
 return <div className="pa-server-chat-container"><nav aria-label="Synthetic chat controls">
  <button onClick={()=>void fetch("/fixture/add-server?bot="+summary.bot.key.bot_id,{method:"POST"})}>Add background server</button>
  <button onClick={()=>void fetch("/fixture/hold-read",{method:"POST"})}>Hold server read</button>
  <button onClick={()=>void fetch("/fixture/release-read",{method:"POST"})}>Release server read</button>
  <button onClick={()=>setSelected(n=>n?0:1)}>Switch Chat Bot</button>
  <button onClick={()=>{revoked=true;setRevision(n=>n+1)}}>Revoke source</button>
 </nav><main className="project-assistants pa-server-chat-fixture" data-fixture-revision={revision}><FallbackSourceScope source={chatSource}><Chat key={summary.bot.key.bot_id} summary={summary} source={chatSource} current={()=>[chatSource]} accountKey={grant.accountKey} readerID={grant.readerID} view={chatView} w={w} language={language} onBack={()=>{}} onBotChanged={()=>{}} projectName="Synthetic project" machineName="Synthetic computer"/></FallbackSourceScope></main></div>
}
createRoot(document.getElementById("app")!).render(<React.StrictMode>{query.has("chat")?<FullChatFixture/>:<Fixture/>}</React.StrictMode>)
