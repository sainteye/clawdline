import type { ProjectAssistantTrace,ProjectAssistantReadTraceSummary,ProjectAssistantPermissionOffer } from "@clawdline/contract"
import { useEffect, useRef, useState } from "react"
import { plainPreview } from "./BotRow"
import { richText } from "../legacy/js/view/markdown.js"
import type { Words } from "./words"

export function Trace({items=[],summaries=[],details={},total=items.length,incomplete=false,more=false,onMore,onDetail,w}:{items?:readonly ProjectAssistantTrace[];summaries?:readonly ProjectAssistantReadTraceSummary[];details?:Readonly<Record<string,ProjectAssistantTrace|ProjectAssistantPermissionOffer>>;total?:number;incomplete?:boolean;more?:boolean;onMore?:()=>void;onDetail?:(summary:ProjectAssistantReadTraceSummary)=>void;w:Words}) {
  const requested = useRef(new Map<string,string>())
  const [openIds, setOpenIds] = useState<Record<string, boolean>>({})

  const toggle = (id: string) => {
    setOpenIds(prev => {
      const next = !prev[id]
      if(next)requested.current.delete(id)
      return { ...prev, [id]: next }
    })
  }

  useEffect(() => {
    for (const row of summaries) {
      if (!openIds[row.id] || requested.current.get(row.id) === row.detail_cursor) continue
      requested.current.set(row.id,row.detail_cursor)
      onDetail?.(row)
    }
  },[summaries,openIds,onDetail])

  if (summaries.length || !items.length && total > 0) {
    return <details className="pa-trace"><summary>{w.trace} · {total}{incomplete?"+":""}</summary><div className="pa-trace-list">{[...summaries].reverse().map(row => {
      const value = details[row.id], full = value && "text" in value ? value : undefined
      const info = formatTraceItem(row.category, full?.text, w)
      const trimmedFull = full?.text?.trim() ?? ""
      const hasBody = !full || isNarration(row.category) || (trimmedFull.length > 0 && trimmedFull !== (info.text ?? "").trim() && trimmedFull !== info.label)
      const isOpen = !!openIds[row.id]
      return <div key={row.id} data-trace-id={row.id} className="pa-trace-row">
        {hasBody ? <button type="button" className="pa-trace-toggle" aria-expanded={isOpen} onClick={() => toggle(row.id)}>
          <span className="pa-trace-chevron">{isOpen ? "▾" : "▸"}</span>
          <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
          <span className="pa-trace-title">{info.label}</span>
          {info.text ? <span className="pa-trace-desc">{info.text}</span> : <span className="pa-trace-desc pa-trace-detail-hint">{w.details}</span>}
        </button> : <div className="pa-trace-line">
          <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
          <span className="pa-trace-title">{info.label}</span>
          {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
        </div>}
        {hasBody && isOpen && <TraceBody category={row.category} text={trimmedFull}/>}
      </div>
    })}{more && <button type="button" className="pa-trace-more-btn" onClick={onMore}>{w.moreTrace}</button>}</div></details>
  }
  if (!items.length) return null
  const turns = new Map<string, ProjectAssistantTrace[]>()
  for (const item of items) turns.set(item.turn_id, [...(turns.get(item.turn_id) ?? []), item])
  return <details className="pa-trace"><summary>{w.trace} · {items.length}</summary><div className="pa-trace-list">{[...turns].map(([id, rows]) => <div key={id} className="pa-trace-turn">{rows.map((row, i) => {
    const info = formatTraceItem(row.category, row.text, w)
    const turnKey = `${id}-${i}`
    const trimmed = row.text?.trim() ?? ""
    const hasBody = isNarration(row.category) || trimmed.length > 0 && trimmed !== (info.text ?? "").trim() && trimmed !== info.label
    const isOpen = !!openIds[turnKey]
    return <div key={i} className="pa-trace-row">
      {hasBody ? <button type="button" className="pa-trace-toggle" aria-expanded={isOpen} onClick={() => setOpenIds(prev => ({ ...prev, [turnKey]: !prev[turnKey] }))}>
        <span className="pa-trace-chevron">{isOpen ? "▾" : "▸"}</span>
        <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
        <span className="pa-trace-title">{info.label}</span>
        {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
      </button> : <div className="pa-trace-line">
        <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
        <span className="pa-trace-title">{info.label}</span>
        {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
      </div>}
      {hasBody && isOpen && <TraceBody category={row.category} text={trimmed}/>}
    </div>
  })}</div>)}</div></details>
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
function TraceBody({category,text}:{category:string;text:string}) {
  return isNarration(category)?<div className="pa-trace-body pa-message-text" dangerouslySetInnerHTML={{__html:richText(text)}}/>:<pre className="pa-trace-body">{text}</pre>
}
