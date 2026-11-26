/** Synthetic live source around the real host: reading a chat must move the status-bar badge too. */
import {createRoot} from "react-dom/client"
import {useProjectAssistantsHost} from "../pages/project-assistants"
import {fixtureSource} from "./fixtures"
import {createViewState} from "./view-state"
import type * as W from "@clawdline/contract"
import "./styles.css"

const source=fixtureSource("unread"),baseMarkRead=source.markRead.bind(source)
const listeners=new Set<(frame:W.ProjectAssistantRosterFrame)=>void>()
const query={machine_id:source.machineID,cursor:"",query:"",page_size:100}
let revision=1
// Like the daemon, an acknowledged read cursor is published as a roster change.
async function publish(){
  revision++
  const page=await source.list(query,new AbortController().signal)
  for(const receive of listeners)receive({type:"changes",source_epoch:"unread",cursor:String(revision),changes:page.items.map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})
}
source.markRead=async(r,s)=>{const answer=await baseMarkRead(r,s);await publish();return answer}
source.canRoster=()=>true
source.subscribeRoster=async(r,receive,signal)=>{
  receive({type:"hello",source_epoch:"unread"})
  const page=await source.list(query,signal)
  receive({type:"snapshot-complete",source_epoch:"unread",cursor:String(revision),changes:page.items.map(summary=>({key:summary.bot.key,revision,removed:false,summary}))})
  listeners.add(receive)
  await new Promise<void>(resolve=>{
    signal.addEventListener("abort",()=>{listeners.delete(receive);resolve()},{once:true})
    if(signal.aborted)resolve()
  })
}
const sources=[source],registry={accountKey:"unread",readerID:"unread",viewState:createViewState(),subscribe:()=>()=>{},getSnapshot:()=>sources}
function App(){
  const language=new URL(location.href).searchParams.get("lang")??"en"
  const host=useProjectAssistantsHost({registry:registry as never,cloud:false,language})
  return <><main className="pane-list"><h1>Sessions</h1></main><footer aria-label="Status bar">{host.dock}</footer>{host.content}</>
}
createRoot(document.querySelector("#app")!).render(<App/>)
