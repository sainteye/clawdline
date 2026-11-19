import { useLayoutEffect, useRef, useState, type FormEvent } from "react"
import type { ProjectAssistantBotSummary } from "@clawdline/contract"
import { errorCode } from "./client"
import type { Words } from "./words"

export type RosterPrompt = {item:ProjectAssistantBotSummary;action:"section"|"delete"|"new-session"}

export function RosterActionDialog({prompt,w,onClose,apply}:{
  prompt:RosterPrompt;w:Words;onClose:()=>void;
  apply:(prompt:RosterPrompt,name:string,signal:AbortSignal)=>Promise<void>;
}) {
  const dialog=useRef<HTMLDialogElement>(null)
  const control=useRef<AbortController|null>(null)
  const [name,setName]=useState("")
  const [saving,setSaving]=useState(false)
  const [error,setError]=useState("")
  const title=prompt.action==="section"?w.rosterNewSection:prompt.action==="delete"?w.deleteBot:w.rosterNewSession
  useLayoutEffect(()=>{
    const node=dialog.current
    const opener=document.activeElement
    node?.showModal()
    return()=>{
      control.current?.abort()
      node?.close()
      if(opener instanceof HTMLElement&&opener.isConnected)opener.focus({preventScroll:true})
    }
  },[])
  const submit=async(event:FormEvent)=>{
    event.preventDefault()
    if(control.current)return
    const request=new AbortController()
    control.current=request
    setSaving(true);setError("")
    try {
      await apply(prompt,name.trim(),request.signal)
      if(!request.signal.aborted)onClose()
    }catch(e){
      if(!request.signal.aborted)setError(errorCode(e))
    }finally{
      if(control.current===request)control.current=null
      if(!request.signal.aborted)setSaving(false)
    }
  }
  return <dialog ref={dialog} className="pa-roster-confirm" aria-label={title} onCancel={event=>{event.preventDefault();if(!saving)onClose()}}>
    <form onSubmit={event=>void submit(event)}>
      <h2>{title}</h2>
      {prompt.action==="section"?<label>{w.rosterSectionName}<input required value={name} onChange={event=>setName(event.target.value)} autoFocus disabled={saving}/></label>:
        <p>{prompt.action==="delete"?w.deleteBotConfirm:w.rosterNewSessionConfirm}</p>}
      {error&&<p role="alert">{w.unavailable} ({error})</p>}
      <div className="pa-actions">
        <button type="button" onClick={onClose} disabled={saving} autoFocus={prompt.action!=="section"}>{w.cancel}</button>
        <button type="submit" className={prompt.action==="delete"?"pa-danger":undefined} disabled={saving||prompt.action==="section"&&!name.trim()}>{saving?w.loading:prompt.action==="section"?w.save:title}</button>
      </div>
    </form>
  </dialog>
}
