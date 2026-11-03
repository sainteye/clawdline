import type { ReactNode } from "react"

/** Shared presentation; providers keep ownership of tool data and expansion state. */
export function ToolRow({ label, labelTitle, name, subject, meta, open, onToggle, foldKey, live, agentID, children }: {
  label: string; labelTitle?: string; name?: string; subject: string; meta?: ReactNode
  open: boolean; onToggle: () => void; foldKey?: string; live?: boolean; agentID?: string; children?: ReactNode
}) {
  return <div className="entry toolrow" data-role="tool" data-live={live ? "1" : undefined} data-agent={agentID}>
    <div className="who" title={labelTitle}>{label}</div>
    <div className="body">
      <button type="button" className="toolline" data-fold={foldKey} aria-expanded={open} onClick={onToggle}>
        {name ? <span className="toolname">{name}</span> : <span className="caret">{open ? "⏷" : "⏵"}</span>}
        <span className="subject" title={subject}>{subject}</span>
        {meta && <span className="activity-meta">{meta}</span>}
      </button>
      {children}
    </div>
  </div>
}

export function ToolRunToggle({ label, labelTitle, count, description, meta, open, onToggle, foldKey, live, agentID }: {
  label: string; labelTitle?: string; count: string; description: string; meta?: ReactNode
  open: boolean; onToggle: () => void; foldKey?: string; live?: boolean; agentID?: string
}) {
  return <div className="entry folded" data-role="tool" data-live={live ? "1" : undefined} data-agent={agentID}>
    <div className="who" title={labelTitle}>{label}</div>
    <div className="body">
      <button type="button" className="pill" data-fold={foldKey} aria-expanded={open} onClick={onToggle}>
        <span className="caret">{open ? "⏷" : "⏵"}</span>
        <span className="steps">{count}</span>
        {meta && <span className="activity-meta">{meta}</span>}
        {!open && <span className="what">{description}</span>}
      </button>
    </div>
  </div>
}
