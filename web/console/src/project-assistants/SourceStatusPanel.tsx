import { useState } from "react"
import { botFailureText } from "./source-errors"
import type { SourceHealth } from "./source-status"
import type { Words } from "./words"

const mark = (state: "ok" | "bad" | "unknown") => state === "ok"
  ? <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden="true"><path d="M5 12l5 5 9-10"/></svg>
  : state === "bad"
    ? <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16v.5"/></svg>
    : <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M8 12h8"/></svg>

/** Connection problems and recovery stay behind the roster footer summary. */
export function SourceStatusPanel({health,w,errors=[],retrying={},onRetry,machineNames}:{health:SourceHealth;w:Words;errors?:readonly [string,string,boolean][];retrying?:Readonly<Record<string,boolean>>;onRetry?:(id:string)=>void;machineNames?:ReadonlyMap<string,string>}) {
  const [open,setOpen]=useState(false)
  const named=(id:string)=>machineNames?.get(id)||w.thisMachine
  const message=(code:string)=>
    // refusal-ok: The original typed code remains in the expandable connection details.
    code==="pa_offline"||code==="unavailable"?w.sourceOffline:code==="pa_revoked"||code==="pa_unpaired"||code==="pa_scope_denied"?w.sourcePairingRequired:botFailureText(code,w)
  const ok=health.ok&&!errors.length
  const summary=errors.length?`${named(errors[0][0])} · ${message(errors[0][1])}`:health.summary||w.statusWithoutBots
  if(!health.machines.length&&!health.withoutBots.length&&!errors.length)return null
  return <section className="pa-source-status" data-ok={ok||undefined} data-open={open||undefined}>
    <button type="button" className="pa-source-summary" aria-expanded={open} onClick={()=>setOpen(v=>!v)} title={open?w.statusHide:w.statusShow}>
      <span className="pa-source-dot" aria-hidden="true"/>
      <span className="pa-source-text"><span>{summary}</span>{!errors.length&&!health.ok&&<small>{health.fix}</small>}</span>
      <svg className="pa-source-chevron" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M6 15l6-6 6 6"/></svg>
    </button>
    {open&&<>
      <div className="pa-source-backdrop" aria-hidden="true" onClick={()=>setOpen(false)}/>
      <div className="pa-source-checks">
        {errors.map(([id,code,retryable])=><section role="status" className="pa-source-error pa-connection-notice" key={id}>
          <div className="pa-connection-heading"><strong>{named(id)}</strong>{retryable&&onRetry&&<button type="button" onClick={()=>onRetry(id)} disabled={!!retrying[id]} aria-busy={!!retrying[id]}>{retrying[id]?w.sourceRetrying:w.retry}</button>}</div>
          <p>{message(code)}</p>
          {retryable&&<p className="pa-connection-hint">{w.sourceRetryHint}</p>}
          <details><summary>{w.sourceDetails}</summary><code>{code}</code></details>
        </section>)}
        {health.machines.map(m=><div className="pa-source-machine" key={m.id}>
          <strong>{m.name}</strong>
          <ul>{m.rows.map((r,i)=><li key={i} data-state={r.state}>{mark(r.state)}<span>{r.label}</span>{r.detail&&<small>{r.detail}</small>}</li>)}</ul>
          {m.problem&&<p className="pa-source-fix">{m.fix}</p>}
          <small className="pa-source-id">{w.statusMachineId} {m.id}</small>
        </div>)}
        {health.withoutBots.map(m=><div className="pa-source-machine pa-source-quiet" key={m.id}><strong>{m.name}</strong><small>{w.statusWithoutBots}</small><small className="pa-source-id">{w.statusMachineId} {m.id}</small></div>)}
      </div>
    </>}
  </section>
}
