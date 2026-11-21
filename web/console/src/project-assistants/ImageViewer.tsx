import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {loadOutput} from "./outputs"
import {useModalBackdrop} from "../modal-backdrop.js"
import type {Words} from "./words"

/**
 * A Bot's shared picture, full size, as Codync's AttachmentViewer shows it:
 * the name and download beside close, and the picture itself. It opens on the
 * bytes the chat card already holds, so nothing is fetched again; only a tap
 * before the thumbnail arrived loads it, with a spinner rather than a file
 * transfer. The page disables pinch zoom, so a tap switches between fitting the
 * screen and the picture's own size, which then scrolls.
 */
export function ImageViewer({message,source,w,picture,onClose}:{message:W.ProjectAssistantMessage;source:SourceClient;w:Words;picture:Blob|null;onClose:()=>void}) {
  const output=message.output!
  const [blob,setBlob]=useState<Blob|null>(picture),[url,setURL]=useState(""),[error,setError]=useState(""),[attempt,setAttempt]=useState(0),[actual,setActual]=useState(false)
  const close=useRef<HTMLButtonElement>(null)
  const backdrop=useModalBackdrop(onClose)
  useEffect(()=>{close.current?.focus()},[])
  useEffect(()=>{
    const escape=(e:KeyboardEvent)=>{if(e.key==="Escape"&&!e.defaultPrevented){e.stopImmediatePropagation();onClose()}}
    document.addEventListener("keydown",escape)
    return()=>document.removeEventListener("keydown",escape)
  },[onClose])
  useEffect(()=>{
    if(blob)return
    const control=new AbortController()
    setError("")
    const request={key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:output.id,artifact_id:"",offset:0}
    void loadOutput(source,request,control.signal).then(result=>{
      if(!control.signal.aborted&&source.current?.()!==false)setBlob(result.blob)
    },e=>{if(!control.signal.aborted)setError(errorCode(e))})
    return()=>control.abort()
  },[blob,source,message,output.id,attempt])
  useEffect(()=>{
    if(!blob)return
    const made=URL.createObjectURL(blob)
    setURL(made)
    return()=>{URL.revokeObjectURL(made);setURL("")}
  },[blob])
  const save=()=>{
    if(!blob||source.current?.()===false)return
    const href=URL.createObjectURL(blob),a=document.createElement("a")
    a.href=href;a.download=output.name.replace(/[\/\\]/g,"_");a.click()
    setTimeout(()=>URL.revokeObjectURL(href),0)
  }
  return <div className="pa-output-scrim pa-image-viewer-scrim" {...backdrop}>
    <div className="pa-image-viewer" role="dialog" aria-modal="true" aria-label={output.name}>
      <header>
        <h3>{output.name}</h3>
        <button type="button" disabled={!blob} onClick={save}>{w.downloadFile}</button>
        <button type="button" ref={close} onClick={onClose}>{w.close}</button>
      </header>
      <div className="pa-image-viewer-stage" data-actual={actual||undefined}>
        {url?<img src={url} alt={output.name} draggable={false} onClick={()=>setActual(value=>!value)}/>:
          error?<p role="alert">{w.outputFailed} ({error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>:
          <span className="pa-send-spinner pa-image-viewer-spinner" role="status" aria-label={w.openingOutput}/>}
      </div>
    </div>
  </div>
}
