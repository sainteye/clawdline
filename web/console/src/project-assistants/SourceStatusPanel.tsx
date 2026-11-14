import { useState } from "react"
import type { SourceHealth } from "./source-status"
import type { Words } from "./words"

const mark = (state: "ok" | "bad" | "unknown") => state === "ok"
  ? <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden="true"><path d="M5 12l5 5 9-10"/></svg>
  : state === "bad"
    ? <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16v.5"/></svg>
    : <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M8 12h8"/></svg>

/** The roster's status: one line when all is well, a card with the fix when not; either opens the checklist. */
export function SourceStatusPanel({health,w}:{health:SourceHealth;w:Words}) {
  const [open,setOpen]=useState(false)
  if(!health.machines.length&&!health.withoutBots.length)return null
  return <section className="pa-source-status" data-ok={health.ok||undefined}>
    <button type="button" className="pa-source-summary" aria-expanded={open} onClick={()=>setOpen(v=>!v)} title={open?w.statusHide:w.statusShow}>
      <span className="pa-source-dot" aria-hidden="true"/>
      <span className="pa-source-text"><span>{health.summary||w.statusWithoutBots}</span>{!health.ok&&<small>{health.fix}</small>}</span>
      <svg className="pa-source-chevron" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
    </button>
    {open&&<div className="pa-source-checks">
      {health.machines.map(m=><div className="pa-source-machine" key={m.id}>
        <strong>{m.name}</strong>
        <ul>{m.rows.map((r,i)=><li key={i} data-state={r.state}>{mark(r.state)}<span>{r.label}</span>{r.detail&&<small>{r.detail}</small>}</li>)}</ul>
        {m.problem&&<p className="pa-source-fix">{m.fix}</p>}
        <small className="pa-source-id">{w.statusMachineId} {m.id}</small>
      </div>)}
      {health.withoutBots.map(m=><div className="pa-source-machine pa-source-quiet" key={m.id}><strong>{m.name}</strong><small>{w.statusWithoutBots}</small><small className="pa-source-id">{w.statusMachineId} {m.id}</small></div>)}
    </div>}
  </section>
}
