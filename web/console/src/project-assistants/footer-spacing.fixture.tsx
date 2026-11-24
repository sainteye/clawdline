/** The real Chat and Composer, with dispatched work in the footer and a first load the test releases. */
import React from "react"
import {createRoot} from "react-dom/client"
import {fixtureSource} from "./fixtures"
import {createViewState} from "./view-state"
import {Chat} from "./Chat"
import {FallbackSourceScope} from "./FallbackSettings"
import {words} from "./words"
import type * as W from "@clawdline/contract"
import "./styles.css"

const source=fixtureSource("footer"),baseView=source.view!.bind(source)
let release=()=>{}
const loaded=new Promise<void>(resolve=>{release=resolve})
source.view=async(r,s)=>{await loaded;return baseView(r,s)}
const query={machine_id:source.machineID,cursor:"",query:"",page_size:100}
const sources=[source],view=createViewState()
;(window as unknown as {fixtureFooter:{release:()=>void}}).fixtureFooter={release:()=>release()}
function FooterChat(){
  const [summary,setSummary]=React.useState<W.ProjectAssistantBotSummary>()
  const language=new URL(location.href).searchParams.get("lang")??"en"
  React.useEffect(()=>{void source.list(query,new AbortController().signal).then(page=>setSummary({...page.items[0],dispatched_work:{running:1,needs_you:0,delivered:11}}))},[])
  return summary?<div className="project-assistants" style={{height:"100vh",display:"flex",flexDirection:"column"}}><FallbackSourceScope source={source}><Chat summary={summary} source={source} current={()=>sources} accountKey="footer" view={view} w={words(language)} language={language} onBack={()=>{}} onBotChanged={()=>{}}/></FallbackSourceScope></div>:null
}
createRoot(document.querySelector("#app")!).render(<FooterChat/>)
