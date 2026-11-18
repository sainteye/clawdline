import { useModalBackdrop } from "../modal-backdrop.js"
import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {loadInput} from "./inputs"
import type {Words} from "./words"

export function InputCard({message,file,source,w}:{message:W.ProjectAssistantMessage;file:W.ProjectAssistantInputFile;source:SourceClient;w:Words}) {
  const [open,setOpen]=useState(false)
  return <div className="pa-output-card pa-input-card"><button type="button" disabled={!source.input||!source.canInputs?.()} onClick={()=>setOpen(true)}><strong>▤ {file.name}</strong><small>{w.attachedFile} · {file.bytes.toLocaleString()} B</small></button>{open&&<InputViewer message={message} file={file} source={source} w={w} onClose={()=>setOpen(false)}/>}</div>
}

function InputViewer({message,file,source,w,onClose}:{message:W.ProjectAssistantMessage;file:W.ProjectAssistantInputFile;source:SourceClient;w:Words;onClose:()=>void}) {
  const [blob,setBlob]=useState<Blob|null>(null),[error,setError]=useState(""),[progress,setProgress]=useState(0),[attempt,setAttempt]=useState(0)
  const backdrop = useModalBackdrop(onClose)
  const close=useRef<HTMLButtonElement>(null)
  useEffect(()=>{close.current?.focus();const key=(e:KeyboardEvent)=>{if(e.key==="Escape")onClose()};document.addEventListener("keydown",key);return()=>document.removeEventListener("keydown",key)},[onClose])
  useEffect(()=>{
    const control=new AbortController();setBlob(null);setError("");setProgress(0)
    void loadInput(source,{key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:file.id,offset:0},file,control.signal,(n,total)=>{if(!control.signal.aborted)setProgress(total?Math.floor(n*100/total):100)}).then(value=>{if(!control.signal.aborted)setBlob(value)}).catch(e=>{if(!control.signal.aborted)setError(errorCode(e))})
    const timer=setInterval(()=>{if(source.current?.()===false){control.abort();setBlob(null);setError("pa_revoked")}},3000)
    return()=>{control.abort();clearInterval(timer)}
  },[source,message,file,attempt])
  const save=()=>{if(!blob||source.current?.()===false)return;const url=URL.createObjectURL(blob),a=document.createElement("a");a.href=url;a.download=file.name;a.click();setTimeout(()=>URL.revokeObjectURL(url),0)}
  return <div className="pa-output-scrim" {...backdrop}><div className="pa-output-overlay" role="dialog" aria-modal="true" aria-label={file.name}><header><h3>{file.name}</h3><button type="button" ref={close} onClick={onClose}>{w.close}</button></header>{!blob&&!error&&<p role="status">{w.openingOutput} {progress}%</p>}{error&&<p role="alert">{w.outputFailed} ({error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>}{blob&&<button type="button" onClick={save}>{w.downloadFile}</button>}</div></div>
}
