import type { ProjectAssistantTrace,ProjectAssistantReadTraceSummary,ProjectAssistantPermissionOffer } from "@clawdline/contract"
import { useState } from "react"
import type { Words } from "./words"

export function Trace({items=[],summaries=[],details={},total=items.length,more=false,onMore,onDetail,w}:{items?:readonly ProjectAssistantTrace[];summaries?:readonly ProjectAssistantReadTraceSummary[];details?:Readonly<Record<string,ProjectAssistantTrace|ProjectAssistantPermissionOffer>>;total?:number;more?:boolean;onMore?:()=>void;onDetail?:(summary:ProjectAssistantReadTraceSummary)=>void;w:Words}) {
  const [openIds, setOpenIds] = useState<Record<string, boolean>>({})

  const toggle = (id: string, summary?: ProjectAssistantReadTraceSummary) => {
    setOpenIds(prev => {
      const next = !prev[id]
      if (next && summary && !details[id]) {
        onDetail?.(summary)
      }
      return { ...prev, [id]: next }
    })
  }

  if (summaries.length || !items.length && total > 0 && summaries.length > 0) {
    return <details className="pa-trace"><summary>{w.trace} · {total}</summary><div className="pa-trace-list">{summaries.map(row => {
      const value = details[row.id], full = value && "text" in value ? value : undefined
      const info = formatTraceItem(row.category, full?.text, w)
      const trimmedFull = full?.text?.trim() ?? ""
      const hasBody = trimmedFull.length > 0 && trimmedFull !== (info.text ?? "").trim() && trimmedFull !== info.label
      const isOpen = !!openIds[row.id]
      return <div key={row.id} data-trace-id={row.id} className="pa-trace-row">
        {hasBody ? <button type="button" className="pa-trace-toggle" onClick={() => toggle(row.id, row)}>
          <span className="pa-trace-chevron">{isOpen ? "▾" : "▸"}</span>
          <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
          <span className="pa-trace-title">{info.label}</span>
          {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
        </button> : <div className="pa-trace-line">
          <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
          <span className="pa-trace-title">{info.label}</span>
          {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
        </div>}
        {hasBody && isOpen && <pre className="pa-trace-body">{trimmedFull}</pre>}
      </div>
    })}{more && <button type="button" className="pa-trace-more-btn" onClick={onMore}>{w.more}</button>}</div></details>
  }
  if (!items.length) return null
  const turns = new Map<string, ProjectAssistantTrace[]>()
  for (const item of items) turns.set(item.turn_id, [...(turns.get(item.turn_id) ?? []), item])
  return <details className="pa-trace"><summary>{w.trace} · {items.length}</summary><div className="pa-trace-list">{[...turns].map(([id, rows]) => <div key={id} className="pa-trace-turn">{rows.map((row, i) => {
    const info = formatTraceItem(row.category, row.text, w)
    const turnKey = `${id}-${i}`
    const trimmed = row.text?.trim() ?? ""
    const hasBody = trimmed.length > 0 && trimmed !== (info.text ?? "").trim() && trimmed !== info.label
    const isOpen = !!openIds[turnKey]
    return <div key={i} className="pa-trace-row">
      {hasBody ? <button type="button" className="pa-trace-toggle" onClick={() => setOpenIds(prev => ({ ...prev, [turnKey]: !prev[turnKey] }))}>
        <span className="pa-trace-chevron">{isOpen ? "▾" : "▸"}</span>
        <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
        <span className="pa-trace-title">{info.label}</span>
        {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
      </button> : <div className="pa-trace-line">
        <span className="pa-trace-icon" aria-hidden="true">{info.icon}</span>
        <span className="pa-trace-title">{info.label}</span>
        {info.text ? <span className="pa-trace-desc">{info.text}</span> : null}
      </div>}
      {hasBody && isOpen && <pre className="pa-trace-body">{trimmed}</pre>}
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
  let text = rawText
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

  return { label, text, icon }
}
