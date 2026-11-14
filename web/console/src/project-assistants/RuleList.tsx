import { useEffect, useState } from "react"
import type * as W from "@clawdline/contract"
import type { Words } from "./words"
import { errorCode } from "./client"

/** The standing rules Always allow and Never left for the Bot's Project, each revocable. */
export function RuleList({load,remove,enabled,w}:{load:(signal:AbortSignal)=>Promise<W.ProjectAssistantPermissionRule[]>;remove:(rule:W.ProjectAssistantPermissionRule)=>Promise<void>;enabled:boolean;w:Words}) {
  const [rules,setRules]=useState<W.ProjectAssistantPermissionRule[]|null>(null),[error,setError]=useState(""),[busy,setBusy]=useState(false),[tick,setTick]=useState(0)
  useEffect(()=>{const control=new AbortController();setError("");load(control.signal).then(r=>{if(!control.signal.aborted)setRules(r)}).catch(e=>{if(!control.signal.aborted)setError(errorCode(e))});return()=>control.abort()},[load,tick])
  return <section className="pa-rules" aria-label={w.rulesTitle}><h3>{w.rulesTitle}</h3><p className="pa-approval-detail">{w.rulesHint}</p>
    {error&&<p role="alert">{w.unavailable} ({error})</p>}
    {rules&&!rules.length&&<p className="pa-approval-detail">{w.rulesEmpty}</p>}
    {rules&&rules.length>0&&<ul>{rules.map(r=><li key={r.behavior+r.rule}><span className="pa-rule-kind" data-behavior={r.behavior}>{r.behavior==="allow"?w.ruleAllow:w.ruleDeny}</span><code>{r.rule}</code><button type="button" disabled={!enabled||busy} onClick={async()=>{setBusy(true);setError("");try{await remove(r);setTick(t=>t+1)}catch(e){setError(errorCode(e))}finally{setBusy(false)}}}>{w.ruleRevoke}</button></li>)}</ul>}
  </section>
}
