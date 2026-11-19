import type { ProjectAssistantTrace,ProjectAssistantReadTraceSummary,ProjectAssistantPermissionOffer } from "@clawdline/contract"
import { useEffect, useRef, useState } from "react"
import { plainPreview } from "./BotRow"
import { richText } from "../legacy/js/view/markdown.js"
import { ThinkingOrb } from "./ThinkingOrb"
import type { Words } from "./words"

type Summary=ProjectAssistantReadTraceSummary

export function Trace({summaries,details,incomplete=false,more=false,inline=false,expanded=false,onMore,onDetail,w}:{
  summaries:readonly Summary[];details:Readonly<Record<string,ProjectAssistantTrace|ProjectAssistantPermissionOffer>>;
  incomplete?:boolean;more?:boolean;inline?:boolean;expanded?:boolean;onMore?:()=>void;
  onDetail:(row:Summary)=>Promise<boolean|undefined>;w:Words
}) {
  const [open,setOpen]=useState(false)
  const visible=inline?expanded:open
  const [settled,setSettled]=useState<Record<string,{cursor:string;ok:boolean}>>({})
  const [retry,setRetry]=useState(0)
  const activeRead=useRef({summaries,onDetail,settled})
  activeRead.current={summaries,onDetail,settled}

  const eligible=summaries.filter(row=>workingCategory(row.category)||!inline&&!internalTrace(row.category))
  const signature=JSON.stringify(eligible.map(row=>row.detail_cursor))
  useEffect(()=>{
    if(!visible)return
    let cancelled=false
    const allowed=new Set<string>(JSON.parse(signature))
    const load=async()=>{
      for(const row of [...activeRead.current.summaries].reverse()){
        if(cancelled)return
        const current=activeRead.current
        if(!allowed.has(row.detail_cursor)||current.settled[row.id]?.cursor===row.detail_cursor)continue
        const received=await current.onDetail(row)
        if(!cancelled)setSettled(old=>({...old,[row.id]:{cursor:row.detail_cursor,ok:received===true}}))
      }
    }
    void load()
    return()=>{cancelled=true}
  },[visible,signature,retry])

  const ready=eligible.flatMap(row=>{
    const trace=details[row.id]
    return trace&&"text" in trace&&visibleTrace(trace)?[{row,trace}]:[]
  })
  const waiting=eligible.some(row=>!details[row.id]&&settled[row.id]?.cursor!==row.detail_cursor)
  const failed=eligible.some(row=>!details[row.id]&&settled[row.id]?.cursor===row.detail_cursor&&settled[row.id]?.ok===false)
  const count=visible&&!waiting&&!failed?ready.length:eligible.length
  if(!inline&&!eligible.length&&!more)return null
  const content=<div className="pa-trace-list">
    {ready.reverse().map(({row,trace})=><TraceStep key={row.id} id={row.id} trace={trace} w={w}/>)}
    {waiting&&<span className="pa-trace-state" role="status"><ThinkingOrb size={14} color="var(--pa-dim)"/>{w.traceLoading}</span>}
    {!waiting&&failed&&<button type="button" className="pa-trace-more-btn" onClick={()=>{
      setSettled(old=>Object.fromEntries(Object.entries(old).filter(([,read])=>read.ok)));setRetry(value=>value+1)
    }}>{w.traceRetry}</button>}
    {!ready.length&&!waiting&&!failed&&<p className="pa-trace-state">{w.workNoSteps}</p>}
    {more&&<button type="button" className="pa-trace-more-btn" onClick={onMore}>{w.moreTrace}</button>}
  </div>
  return inline?visible?content:null:<details className="pa-trace" data-trace-count={eligible.length} onToggle={event=>setOpen(event.currentTarget.open)}>
    <summary>{w.trace}{count>0&&` · ${count}${incomplete?"+":""}`}</summary>{visible&&content}
  </details>
}

function internalTrace(category:string){return category==="tool_activity"||category==="no_visible_reply"}
export function workingCategory(category:string){
  return isNarration(category)||category==="thought"||category==="plan"||isTool(category)
}
function isTool(category:string){return category==="tool_call"||category==="tool_call_update"||category==="tool"}
export function visibleTrace(trace:ProjectAssistantTrace){
  const text=trace.text.trim()
  if(!text||internalTrace(trace.category))return false
  if(trace.category==="agent_message_chunk"&&/^\(?pass\)?$/i.test(text.replace(/^[".*]+|[".*]+$/g,"").trim()))return false
  if(!isTool(trace.category))return true
  const title=text.split("\n",1)[0].trim()
  return !/^(?:mcp[. _]+)?(?:clawdline[. _-]+)?(?:chat|memory)[. _:]+(?:send_message|read_memory|write_memory|edit_memory|search_memory|list_memory)(?:$|[ (])/i.test(title)
}

function TraceStep({id,trace,w}:{id:string;trace:ProjectAssistantTrace;w:Words}) {
  const text=trace.text.trim()
  const info=formatTraceItem(trace.category,text,w)
  if(isTool(trace.category)){
    const [title,...lines]=text.split("\n")
    const first=lines[0]?.trim()
    const status=/^(completed|failed|in_progress|pending)$/.test(first??"")?lines.shift()!.trim():""
    const body=lines.join("\n").trim()
    const label=<><span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
      <span className="pa-trace-tool-title">{info.text||title}</span>
      {status==="completed"||status==="failed"?<span className="pa-trace-result" data-failed={status==="failed"||undefined}
        aria-label={status==="failed"?w.traceToolFailed:w.traceToolCompleted}>{status==="failed"?"×":"✓"}</span>:
        !trace.complete?<ThinkingOrb size={13} color="var(--pa-dim)"/>:null}
    </>
    return body?<details className="pa-trace-row pa-trace-tool" data-trace-id={id}>
      <summary className="pa-trace-line">{label}<span className="pa-trace-chevron" aria-hidden="true">›</span></summary>
      <pre className="pa-trace-body">{body}</pre>
    </details>:<div className="pa-trace-row pa-trace-line" data-trace-id={id}>{label}</div>
  }
  return <div className="pa-trace-row" data-trace-id={id}>
    <span className="pa-trace-title"><span className="pa-trace-icon" aria-hidden="true">{info.icon}</span> {trace.category==="agent_message_chunk"?w.traceNarration:info.label}</span>
    {trace.category==="agent_message_chunk"?<div className="pa-trace-narration pa-markdown" dangerouslySetInnerHTML={{__html:richText(text)}}/>:
      <p className="pa-trace-narration">{text}</p>}
  </div>
}

/** What one trace step is, in the person's words; the provider's own category name stays out of view. */
export function traceLabel(category:string,w:Words){
  const said:Record<string,string>={agent_message_chunk:w.traceReply,agent_thought_chunk:w.traceThought,tool_call:w.traceTool,tool_call_update:w.traceTool,tool:w.traceTool,plan:w.tracePlan,no_visible_reply:w.traceNoReply,tool_activity:w.traceTool}
  return said[category]??w.traceStep
}

/** Present trace steps with clear icons and human-readable tool descriptions. */
export function formatTraceItem(category: string, rawText: string | undefined, w: Words): { label: string; text?: string; icon: string } {
  let label = traceLabel(category, w)
  let text = rawText?.split("\n",1)[0]
  let icon = "⚡"

  if (category === "agent_thought_chunk" || category === "thought") {
    icon = "💭"
    label = w.traceThought
  } else if (category === "tool_call" || category === "tool_call_update" || category === "tool" || category === "tool_activity") {
    icon = "🛠️"
    label = w.traceTool
  } else if (category === "plan") {
    icon = "📋"
    label = w.tracePlan
  } else if (category === "agent_message_chunk") {
    if (text && text.trim().length <= 2 && !/\p{L}|\p{N}/u.test(text.trim())) {
      text = undefined
    }
  }

  if (text) {
    const hostMatch = /^host-[a-f0-9]+-([A-Za-z0-9_-]+)(?:-\d+)?$/.exec(text.trim())
    if (hostMatch) {
      const tool = hostMatch[1]
      const isZh = typeof w.trace === "string" && w.trace.includes("處理")
      const toolNamesZh: Record<string, string> = {
        teamAvailable: "檢查團隊協同狀態",
        connector: "載入擴充連接器",
        team: "協調整合團隊任務",
        networkBot: "建立跨機器網路通訊",
        listBots: "查詢團隊 Bot 名單",
        bash: "執行終端命令",
        read_file: "讀取檔案",
        write_file: "寫入檔案",
        web_search: "搜尋網路",
        browser: "操作瀏覽器網頁",
      }
      const toolNamesEn: Record<string, string> = {
        teamAvailable: "Check team availability",
        connector: "Load extension connector",
        team: "Coordinate team tasks",
        networkBot: "Connect cross-machine network",
        listBots: "List available team bots",
        bash: "Run terminal command",
        read_file: "Read file",
        write_file: "Write file",
        web_search: "Search the web",
        browser: "Browser action",
      }
      text = (isZh ? toolNamesZh : toolNamesEn)[tool] ?? tool
    }
  }

  return { label, text: text&&isNarration(category)?plainPreview(text):text, icon }
}

function isNarration(category:string) {return category==="agent_message_chunk"||category==="agent_thought_chunk"}
