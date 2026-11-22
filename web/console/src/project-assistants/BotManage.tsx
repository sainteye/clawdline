import { useEffect, useId, useRef, useState } from "react"
import type { Words } from "./words"
import { errorCode } from "./client"
import { botFailureText } from "./source-errors"

/** Hide from list and the Bot settings deletion confirmation. */
export function BotManage({onHide,onDelete,enabled,hideDisabled,w,bare=false}:{bare?:boolean;onHide:()=>Promise<void>;onDelete:()=>Promise<void>;enabled:boolean;hideDisabled?:boolean;w:Words}) {
  const [confirming,setConfirming]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState("")
  const confirm=useRef<HTMLDivElement>(null),cancel=useRef<HTMLButtonElement>(null),start=useRef<HTMLButtonElement>(null),opened=useRef(false),textID=useId()
  // The confirmation replaces the row in place; on a phone it can sit below
  // the fold, so it is brought into view and the safe choice takes focus.
  useEffect(()=>{
    if(confirming){opened.current=true;confirm.current?.scrollIntoView?.({block:"nearest",behavior:"smooth"});cancel.current?.focus({preventScroll:true})}
    else if(opened.current){opened.current=false;start.current?.focus({preventScroll:true})}
  },[confirming])
  const run=async(action:()=>Promise<void>)=>{setBusy(true);setError("");try{await action()}catch(e){setError(errorCode(e))}finally{setBusy(false)}}
  return <section className="pa-manage" aria-label={w.manageTitle}>
    {!bare&&<h3>{w.manageTitle}</h3>}
    <div className="pa-manage-row"><div><strong>{w.hideBot}</strong><p className="pa-approval-detail">{w.hideBotHint}</p></div><button type="button" disabled={!enabled||busy||hideDisabled} onClick={()=>void run(onHide)}>{w.hideBot}</button></div>
    {!confirming?<div className="pa-manage-row"><div><strong>{w.deleteBot}</strong><p className="pa-approval-detail">{w.deleteBotHint}</p></div><button ref={start} type="button" className="pa-danger" disabled={!enabled||busy} onClick={()=>setConfirming(true)}>{w.deleteBot}</button></div>
      :<div ref={confirm} className="pa-manage-confirm" role="alertdialog" aria-label={w.deleteBot} aria-describedby={textID}><p id={textID}>{w.deleteBotConfirm}</p><div className="pa-actions"><button ref={cancel} type="button" disabled={busy} onClick={()=>setConfirming(false)}>{w.cancel}</button><button type="button" className="pa-danger" disabled={busy} onClick={()=>void run(onDelete)}>{w.deleteBotAndChat}</button></div></div>}
    {error&&<p role="alert" className="pa-form-error">{error==="pa_stale_version"?w.deleteBusy:botFailureText(error,w as unknown as Record<string,string>)} <small>({error})</small></p>}
  </section>
}
