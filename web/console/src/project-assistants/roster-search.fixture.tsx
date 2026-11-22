/** The snapshot transport filters Bot names, as the real store does. */
import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "./Bots"
import {fixtureSource} from "./fixtures"
import {failure} from "./client"
import type {SourceClient} from "./client"
import type * as W from "@clawdline/contract"
import "./styles.css"

const requests:{machine:string;query:string;cursor:string}[]=[]
let code:W.ProjectAssistantErrorCode|null=null
const base=fixtureSource("studio")
const source:SourceClient={...base,canRoster:()=>false,canGroups:()=>false,
  async list(request,signal){
    if(signal.aborted)throw new DOMException("Aborted","AbortError")
    if(code)throw failure(code)
    requests.push({machine:"studio",query:request.query,cursor:request.cursor})
    const matches=base.bots.filter(bot=>bot.name.toLowerCase().includes(request.query.toLowerCase()))
    const offset=Number(request.cursor||0),selected=matches.slice(offset,offset+3)
    const all=await base.list({machine_id:"studio",query:"",cursor:"",page_size:100},signal)
    const summaries=await Promise.all(selected.map(async bot=>{
      const page=await base.list({machine_id:"studio",query:bot.name,cursor:"",page_size:100},signal)
      const summary=page.items.find(item=>item.bot.key.bot_id===bot.key.bot_id)!
      return bot.key.bot_id==="bot-1"?{...summary,activity_at:"2026-11-12T06:21:00Z"}:summary
    }))
    return {...all,items:summaries,next_cursor:offset+3<matches.length?String(offset+3):""}
  },
}
const other=fixtureSource("unpaired")
const denied:SourceClient={...other,canRoster:()=>false,canGroups:()=>false,async list(){throw failure("pa_unpaired")}}
const sources=[source,denied]
Object.assign(window,{rosterSearch:{requests,fail(value:W.ProjectAssistantErrorCode|null){code=value}}})
createRoot(document.getElementById("app")!).render(<Bots sources={sources} language={new URL(location.href).searchParams.get("lang")??"en"} accountKey="synthetic-search" readerID="fixture" onClose={()=>{}}/>)
