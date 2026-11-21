import {useCallback,useEffect,useMemo,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
import {OutputViewer} from "./OutputViewer"
import {ArtifactIcon,type ArtifactComposerAccess} from "./ArtifactWorkspace"
import {artifactFrame,inlineImageType,loadOutput} from "./outputs"
import type {Words} from "./words"

export function OutputCard({message,source,w,artifactComposer}:{message:W.ProjectAssistantMessage;source:SourceClient;w:Words;artifactComposer?:ArtifactComposerAccess}) {
  const output=message.output!,[open,setOpen]=useState(false)
  const close=useCallback(()=>setOpen(false),[])
  const enabled=!!source.output&&source.canOutputs?.()!==false
  return <div className={`pa-output-card${output.artifact_id?" pa-artifact-card":""}`}>
    {output.artifact_id?<><ArtifactPreview message={message} source={source} enabled={enabled}/>
      <button type="button" disabled={!enabled} onClick={()=>setOpen(true)}><span className="pa-artifact-card-footer"><ArtifactIcon size={29}/>
        <span><strong>{output.name}</strong><small>{w.artifact}</small></span></span></button></>:
      <button type="button" disabled={!enabled} onClick={()=>setOpen(true)}>{inlineImageType(output)&&<ImagePreview message={message} source={source} enabled={enabled}/>}<strong>{output.name}</strong><small>{w.sharedFile} · {output.bytes.toLocaleString()} B</small></button>}
    {open&&<OutputViewer artifactComposer={artifactComposer} item={{kind:"output",key:output.artifact_id||output.id,message}} source={source} w={w} onClose={close}/>}
  </div>
}

function ArtifactPreview({message,source,enabled}:{message:W.ProjectAssistantMessage;source:SourceClient;enabled:boolean}){
  const box=useRef<HTMLSpanElement>(null)
  const [visible,setVisible]=useState(false)
  const [preview,setPreview]=useState<{source:SourceClient;revision:string;token:string;html:string}|null>(null)
  const output=message.output!,revision=source.authorizationRevision
  const token=JSON.stringify([message.fence.key,message.id,output.artifact_id,output.sha256])
  useEffect(()=>{
    const element=box.current
    if(!element||visible||!enabled)return
    if(typeof IntersectionObserver==="undefined")return
    const observer=new IntersectionObserver(entries=>{
      if(entries.some(entry=>entry.isIntersecting)){setVisible(true);observer.disconnect()}
    })
    observer.observe(element)
    return()=>observer.disconnect()
  },[visible,enabled])
  useEffect(()=>{
    if(!visible||!enabled)return
    const control=new AbortController()
    const request={key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:"",artifact_id:output.artifact_id,offset:0}
    void loadOutput(source,request,control.signal).then(({blob})=>blob.text()).then(html=>{
      if(!control.signal.aborted&&source.current?.()!==false)setPreview({source,revision,token,html})
    },()=>{
      // refusal-ok: The thumbnail is decorative. Opening the card shows any source refusal in the full viewer.
      if(!control.signal.aborted)setPreview(null)
    })
    return()=>control.abort()
  },[source,revision,token,visible,enabled])
  const html=enabled&&source.current?.()!==false&&preview?.source===source&&preview.revision===revision&&preview.token===token?preview.html:undefined
  const frame=useMemo(()=>html===undefined?undefined:artifactFrame(html),[html])
  return <span className="pa-artifact-preview" ref={box} aria-hidden="true">
    {frame&&<iframe tabIndex={-1} title="" sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={frame}/>}
  </span>
}

/** A shared picture shows in the chat once it scrolls into view, as Codync's
 * file card does; one that will not decode leaves the plain file card. */
function ImagePreview({message,source,enabled}:{message:W.ProjectAssistantMessage;source:SourceClient;enabled:boolean}){
  const box=useRef<HTMLSpanElement>(null)
  const [visible,setVisible]=useState(false),[url,setURL]=useState(""),[broken,setBroken]=useState(false)
  const output=message.output!,token=JSON.stringify([message.fence.key,message.fence.conversation_id,message.id,output.id,output.sha256])
  useEffect(()=>{
    const element=box.current
    if(!element||visible||!enabled)return
    if(typeof IntersectionObserver==="undefined"){setVisible(true);return}
    const observer=new IntersectionObserver(entries=>{
      if(entries.some(entry=>entry.isIntersecting)){setVisible(true);observer.disconnect()}
    },{rootMargin:"200px"})
    observer.observe(element)
    return()=>observer.disconnect()
  },[visible,enabled])
  useEffect(()=>{
    if(!visible||!enabled)return
    const control=new AbortController()
    let made=""
    const request={key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:output.id,artifact_id:"",offset:0}
    void loadOutput(source,request,control.signal).then(({blob})=>{
      if(control.signal.aborted||source.current?.()===false)return
      made=URL.createObjectURL(blob);setURL(made)
    },()=>{
      // refusal-ok: The thumbnail is decorative. Opening the card shows any source refusal in the full viewer.
      if(!control.signal.aborted)setURL("")
    })
    return()=>{control.abort();if(made)URL.revokeObjectURL(made);setURL("")}
  },[source,token,visible,enabled])
  if(broken)return null
  return <span className="pa-image-preview" ref={box} aria-hidden="true">
    {url&&<img src={url} alt="" draggable={false} onError={()=>setBroken(true)}/>}
  </span>
}
