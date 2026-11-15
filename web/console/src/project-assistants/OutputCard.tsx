import {useCallback,useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {artifactFrame,loadOutput} from "./outputs"
import type {Words} from "./words"

export function OutputCard({message,source,w}:{message:W.ProjectAssistantMessage;source:SourceClient;w:Words}) {
  const o=message.output!,[open,setOpen]=useState(false)
  const close=useCallback(()=>setOpen(false),[])
  return <div className="pa-output-card"><button type="button" disabled={!source.output||source.canOutputs?.()===false} onClick={()=>setOpen(true)}><strong>{o.name}</strong><small>{o.artifact_id?w.artifact:w.sharedFile} · {o.bytes.toLocaleString()} B</small></button>{open&&<OutputViewer message={message} source={source} w={w} onClose={close}/>}</div>
}

function OutputViewer({message,source,w,onClose}:{message:W.ProjectAssistantMessage;source:SourceClient;w:Words;onClose:()=>void}) {
  const [value,setValue]=useState<{blob:Blob;output:W.ProjectAssistantOutput;html:string;links:string[]}|null>(null),[error,setError]=useState(""),[progress,setProgress]=useState(0),[attempt,setAttempt]=useState(0)
  const close=useRef<HTMLButtonElement>(null)
  useEffect(()=>{close.current?.focus();const escape=(e:KeyboardEvent)=>{if(e.key==="Escape")onClose()};document.addEventListener("keydown",escape);return()=>document.removeEventListener("keydown",escape)},[onClose])
  useEffect(()=>{
    const control=new AbortController();setValue(null);setError("");setProgress(0)
    const o=message.output!,q:W.ProjectAssistantOutputRequest={key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:o.artifact_id?"":o.id,artifact_id:o.artifact_id,offset:0}
    void loadOutput(source,q,control.signal,(n,total)=>{if(!control.signal.aborted)setProgress(total?Math.floor(n*100/total):100)}).then(async result=>{
      const html=result.output.artifact_id?await result.blob.text():""
      if(control.signal.aborted||source.current?.()===false)return
      const doc=new DOMParser().parseFromString(html,"text/html"),links=[...new Set([...doc.querySelectorAll("a[href]")].map(a=>a.getAttribute("href")!).filter(h=>/^(https?:\/\/|mailto:)/i.test(h)))]
      setValue({...result,html,links})
    }).catch(e=>{if(!control.signal.aborted)setError(errorCode(e))})
    // Grant changes invalidate displayed HTML as well as late transfer responses.
    const timer=setInterval(()=>{if(source.current?.()===false){control.abort();setValue(null);setError("pa_revoked")}},3000)
    return()=>{control.abort();clearInterval(timer)}
  },[source,message,attempt])
  const save=()=>{if(!value||source.current?.()===false)return;const url=URL.createObjectURL(value.blob),a=document.createElement("a");a.href=url;a.download=value.output.name.replace(/[\/\\]/g,"_")+(value.output.artifact_id?".html":"");a.click();setTimeout(()=>URL.revokeObjectURL(url),0)}
  return <div className="pa-output-overlay" role="dialog" aria-modal="true" aria-label={message.output!.name}><header><h3>{value?.output.name??message.output!.name}</h3><button type="button" ref={close} onClick={onClose}>{w.close}</button></header>{!value&&!error&&<p role="status">{w.openingOutput} {progress}%</p>}{error&&<p role="alert">{w.outputFailed} ({error}) <button onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>}{value&&<>{value.output.artifact_id&&<button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.refresh}</button>}<button type="button" onClick={save}>{value.output.artifact_id?w.saveHTML:w.downloadFile}</button>{value.output.artifact_id&&<iframe className="pa-artifact-frame" title={value.output.name} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={artifactFrame(value.html)}/>} {!!value.links.length&&<details><summary>{w.artifactLinks}</summary>{value.links.map(h=><p key={h}><a href={h} target="_blank" rel="noopener noreferrer">{h}</a></p>)}</details>}</>}</div>
}
