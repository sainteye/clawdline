/** Synthetic identities, real roster. No daemon, provider calls or execution permissions. */
import React, {useState} from "react"
import {createRoot} from "react-dom/client"
import type {ProjectAssistantBotSummary} from "@clawdline/contract"
import {BotRoster} from "./BotRoster"
import {words} from "./words"
import "./styles.css"
const item=(id:string,machine:string,project:string)=>({bot:{name:id,key:{machine_id:machine,bot_id:id},project:{project_id:project},agent:"codex",appearance:{shape:"blob",color:"#e39567"}},activity_at:"2026-11-18T00:00:00Z",activity:"idle",latest_message:"Fixture reply",pending_permissions:0,unread_count:0,source:{state:"online",fresh:true,readable:true,access:"interactive",writable:false}} as unknown as ProjectAssistantBotSummary)
const items=[item("Alpha One","m1","alpha"),item("Beta One","m1","beta"),item("Alpha Two","m1","alpha"),item("Other Alpha","m2","alpha"),item("Named Pinned","m1","pinned")]
function App(){
  const [account,setAccount]=useState(new URL(location.href).searchParams.get("account")??"fixture-a"),[query,setQuery]=useState(""),[machine,setMachine]=useState(""),[selected,setSelected]=useState("")
  const language=new URL(location.href).searchParams.get("lang")??"en",w=words(language)
  return <><button onClick={()=>setAccount(account==="fixture-a"?"fixture-b":"fixture-a")}>Switch fixture account</button><output id="selection">{selected}</output><div className="project-assistants" style={{height:"calc(100vh - 24px)",width:"min(340px,100vw)"}}><BotRoster key={account} rosterScope={account} items={items.filter(i=>i.bot.name.toLowerCase().includes(query.toLowerCase()))} machines={["m1","m2"]} selected={null} query={query} onQuery={setQuery} machine={machine} onMachine={setMachine} onSelect={i=>setSelected(i.bot.name)} onClose={()=>{}} w={w} language={language} loading={false} errors={[]} projectNames={new Map([[JSON.stringify(["m1","alpha"]),"Alpha"],[JSON.stringify(["m1","beta"]),"Beta"],[JSON.stringify(["m2","alpha"]),"Other Alpha Project"],[JSON.stringify(["m1","pinned"]),"Real Pinned Project"]])}/></div></>
}
createRoot(document.querySelector("#app")!).render(<App/>)
