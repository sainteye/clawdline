import { useState } from "react"
import type { Words } from "./words"
import { errorCode } from "./client"
import { botFailureText } from "./source-errors"

/** Codync's Hide from list and Delete, at the end of a Bot's settings. */
export function BotManage({onHide,onDelete,enabled,hideDisabled,w}:{onHide:()=>Promise<void>;onDelete:()=>Promise<void>;enabled:boolean;hideDisabled?:boolean;w:Words}) {
  const [confirming,setConfirming]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState("")
  const run=async(action:()=>Promise<void>)=>{setBusy(true);setError("");try{await action()}catch(e){setError(errorCode(e))}finally{setBusy(false)}}
  return <section className="pa-manage" aria-label={w.manageTitle}>
    <h3>{w.manageTitle}</h3>
    <div className="pa-manage-row"><div><strong>{w.hideBot}</strong><p className="pa-approval-detail">{w.hideBotHint}</p></div><button type="button" disabled={!enabled||busy||hideDisabled} onClick={()=>void run(onHide)}>{w.hideBot}</button></div>
    {!confirming?<div className="pa-manage-row"><div><strong>{w.deleteBot}</strong><p className="pa-approval-detail">{w.deleteBotHint}</p></div><button type="button" className="pa-danger" disabled={!enabled||busy} onClick={()=>setConfirming(true)}>{w.deleteBot}</button></div>
      :<div className="pa-manage-confirm" role="alertdialog" aria-label={w.deleteBot}><p>{w.deleteBotConfirm}</p><div className="pa-actions"><button type="button" disabled={busy} onClick={()=>setConfirming(false)}>{w.cancel}</button><button type="button" className="pa-danger" disabled={busy} onClick={()=>void run(onDelete)}>{w.deleteBotAndChat}</button></div></div>}
    {error&&<p role="alert" className="pa-form-error">{error==="pa_stale_version"?w.deleteBusy:botFailureText(error,w as unknown as Record<string,string>)} <small>({error})</small></p>}
  </section>
}
