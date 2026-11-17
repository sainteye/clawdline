import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {Words} from "./words"
import type {RoutineIntent} from "./routine-intent"
import {routineWord as tr} from "./routine-words"

/** Secrets live only in this mounted panel, never in its parent's command journal. */
export function RoutineHook({id,allowed,busy,held,blocked,onRun,onClose,w}:{
  id:string;allowed:boolean;busy:boolean;held:RoutineIntent|null;blocked:boolean
  onRun:(rotate:boolean,replay:boolean)=>Promise<W.ProjectAssistantRoutineHookResponse|null>
  onClose:()=>void;w:Words
}) {
  const [answer,setAnswer]=useState<W.ProjectAssistantRoutineHookResponse|null>(null),[confirm,setConfirm]=useState(false)
  const mounted=useRef(true),permitted=useRef(allowed)
  permitted.current=allowed
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
  useEffect(()=>{if(!allowed){setAnswer(null);setConfirm(false)}},[allowed])
  const act=async(rotate:boolean,replay=false)=>{
    if(!permitted.current||busy||blocked)return
    setAnswer(null);setConfirm(false)
    const result=await onRun(rotate,replay)
    if(mounted.current&&permitted.current&&result)setAnswer(result)
  }
  const uncertain=held?.action==="hook"&&held.request.id===id
  return <section aria-label={tr("manageHook")}>
    <header className="pa-toolbar"><h4>{tr("manageHook")}</h4><button type="button" onClick={onClose}>{w.close}</button></header>
    <p>{tr("localHint")}</p>
    {!allowed&&<p role="status">{tr("hookReadOnly")}</p>}
    {allowed&&answer&&<dl><dt>{tr("localURL")}</dt><dd><code>{answer.url}</code></dd><dt>{tr("key")}</dt><dd><code>{answer.key}</code></dd></dl>}
    {uncertain?<div role="alert"><p>{tr("hookHeld")}</p><button type="button" disabled={!allowed||busy||blocked} onClick={()=>void act(held.request.rotate,true)}>{tr("retryHook")}</button></div>:<div className="pa-toolbar">
      <button type="button" disabled={!allowed||busy||!!held||blocked} onClick={()=>void act(false)}>{tr("viewKey")}</button>
      <button type="button" disabled={!allowed||busy||!!held||blocked} onClick={()=>{setAnswer(null);setConfirm(true)}}>{tr("rotateKey")}</button>
    </div>}
    {allowed&&answer&&<button type="button" onClick={()=>setAnswer(null)}>{tr("hideKey")}</button>}
    {confirm&&allowed&&<div role="alert"><p>{tr("rotateWarning")}</p><button type="button" disabled={busy||!!held||blocked} onClick={()=>void act(true)}>{tr("confirmRotate")}</button><button type="button" onClick={()=>setConfirm(false)}>{w.cancel}</button></div>}
  </section>
}
