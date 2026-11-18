import { useModalBackdrop } from "../modal-backdrop.js"
import type * as W from "@clawdline/contract"
import {ArtifactRename} from "./ArtifactRename"
import {useEffect,useRef,useState} from "react"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import {catalogWord} from "../catalog"
import {artifactFrame,loadOutput} from "./outputs"
import {loadInput} from "./inputs"
import type {LibraryItem} from "./library-items"
import type {Words} from "./words"

const tr=(key:string)=>catalogWord("projectAssistants",key)
export function libraryItemName(item:LibraryItem):string {
  return item.kind==="output"?item.message.output!.name:item.kind==="input"?item.file.name:tr("libraryPicture")+" "+item.attachment.id.slice(0,8)
}

/** Shared transfer/view UI for chat outputs and every library item. */
export function OutputViewer({item,source,w,onClose,onRenamed,onResolved}:{item:LibraryItem;source:SourceClient;w:Words;onClose:()=>void;onRenamed?:()=>void;onResolved?:(output:W.ProjectAssistantOutput)=>void}) {
  const [value,setValue]=useState<{blob:Blob;name:string;html:string;links:string[];pictureURL:string;output?:W.ProjectAssistantOutput}|null>(null),[error,setError]=useState(""),[progress,setProgress]=useState(0),[attempt,setAttempt]=useState(0),[cancelled,setCancelled]=useState(false)
  const panel = useRef<HTMLDivElement>(null)
  const backdrop = useModalBackdrop(() => {
    if (!panel.current?.querySelector(".pa-artifact-rename form")) onClose()
  })
  const close=useRef<HTMLButtonElement>(null),active=useRef<AbortController|null>(null),tabURLs=useRef<string[]>([])
  const name=libraryItemName(item),artifact=item.kind==="output"&&!!item.message.output!.artifact_id
  const kind=item.kind,message=item.message,file=item.kind==="input"?item.file:null,attachment=item.kind==="picture"?item.attachment:null
  useEffect(()=>{close.current?.focus();const escape=(e:KeyboardEvent)=>{if(e.key==="Escape"&&!e.defaultPrevented&&!(e.target instanceof HTMLElement&&e.target.closest("form"))){e.stopImmediatePropagation();onClose()}};document.addEventListener("keydown",escape);return()=>document.removeEventListener("keydown",escape)},[onClose])
  useEffect(()=>()=>{tabURLs.current.forEach(url=>URL.revokeObjectURL(url))},[])
  useEffect(()=>{
    const control=new AbortController();active.current=control;setValue(null);setError("");setProgress(0);setCancelled(false)
    let pictureURL=""
    const progress=(n:number,total:number)=>{if(!control.signal.aborted)setProgress(total?Math.floor(n*100/total):100)}
    void (async()=>{
      let blob:Blob,loadedName=name,html="",output:W.ProjectAssistantOutput|undefined
      if(kind==="output"){
        const o=message.output!,result=await loadOutput(source,{key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:o.artifact_id?"":o.id,artifact_id:o.artifact_id,offset:0},control.signal,progress)
        blob=result.blob;output=result.output;loadedName=result.output.name;html=artifact?await blob.text():""
      }else if(file)blob=await loadInput(source,{key:message.fence.key,conversation_id:message.fence.conversation_id,message_id:message.id,id:file.id,offset:0},file,control.signal,progress)
      else {
        if(!source.attachment||!attachment)throw {error:{code:"pa_unsupported"}}
        blob=await source.attachment(message.fence.key,attachment.id,control.signal)
        if(blob.size!==attachment.bytes||blob.type!==attachment.media_type)throw {error:{code:"pa_invalid"}}
        loadedName=attachment.id+"."+({"image/png":"png","image/jpeg":"jpg","image/gif":"gif","image/webp":"webp"}[blob.type]??"bin");progress(blob.size,blob.size)
      }
      if(control.signal.aborted||source.current?.()===false)return
      if(output)onResolved?.(output)
      const doc=new DOMParser().parseFromString(html,"text/html"),links=[...new Set([...doc.querySelectorAll("a[href]")].map(a=>a.getAttribute("href")!).filter(h=>/^(https?:\/\/|mailto:)/i.test(h)))]
      if(attachment)pictureURL=URL.createObjectURL(blob)
      setValue({blob,name:loadedName,html,links,pictureURL,output})
    })().catch(e=>{if(!control.signal.aborted)setError(errorCode(e))})
    const timer=setInterval(()=>{if(source.current?.()===false){control.abort();if(pictureURL)URL.revokeObjectURL(pictureURL);tabURLs.current.forEach(url=>URL.revokeObjectURL(url));setValue(null);setError("pa_revoked")}},3000)
    return()=>{control.abort();clearInterval(timer);if(pictureURL)URL.revokeObjectURL(pictureURL)}
  },[source,kind,message,file,attachment,attempt,onResolved])
  const save=()=>{if(!value||source.current?.()===false)return;const url=URL.createObjectURL(value.blob),a=document.createElement("a");a.href=url;a.download=value.name.replace(/[\/\\]/g,"_")+(artifact?".html":"");a.click();setTimeout(()=>URL.revokeObjectURL(url),0)}
  const openBrowser=()=>{
    if(!value||!artifact||source.current?.()===false)return
    // Untrusted HTML stays in TWO sandboxed opaque frames in the new tab too.
    const shell=document.implementation.createHTMLDocument(value.name),policy=shell.createElement("meta"),frame=shell.createElement("iframe")
    policy.httpEquiv="Content-Security-Policy";policy.content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-src 'none'; base-uri 'none'; form-action 'none'";shell.head.append(policy)
    const style=shell.createElement("style");style.textContent="html,body,iframe{margin:0;width:100%;height:100%;border:0}";shell.head.append(style)
    frame.setAttribute("sandbox","allow-scripts");frame.referrerPolicy="no-referrer";frame.title=value.name;frame.srcdoc=artifactFrame(value.html);shell.body.append(frame)
    const url=URL.createObjectURL(new Blob(["<!doctype html>"+shell.documentElement.outerHTML],{type:"text/html"}));tabURLs.current.push(url)
    const a=document.createElement("a");a.href=url;a.target="_blank";a.rel="noopener noreferrer";a.click()
  }
  return <div className="pa-output-scrim" {...backdrop}><div ref={panel} className="pa-output-overlay" role="dialog" aria-modal="true" aria-label={value?.name??name}><header><h3>{value?.name??name}</h3><button type="button" ref={close} onClick={onClose}>{w.close}</button>{value?.output&&artifact&&<ArtifactRename source={source} message={message} output={value.output} w={w} onRenamed={()=>{setAttempt(n=>n+1);onRenamed?.()}}/>}</header>{!value&&!error&&!cancelled&&<><p role="status">{w.openingOutput} {progress}%</p><button type="button" onClick={()=>{active.current?.abort();setCancelled(true)}}>{tr("libraryCancel")}</button></>}{cancelled&&<p role="status">{tr("libraryCancelled")} <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>}{error&&<p role="alert">{w.outputFailed} ({error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></p>}{value&&<><div className="pa-library-actions">{artifact&&<button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.refresh}</button>}<button type="button" onClick={save}>{artifact?w.saveHTML:w.downloadFile}</button>{artifact&&<button type="button" onClick={openBrowser}>{tr("libraryOpenBrowser")}</button>}</div>{artifact&&<iframe className="pa-artifact-frame" title={value.name} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={artifactFrame(value.html)}/>} {value.pictureURL&&<img className="pa-library-picture" alt={name} src={value.pictureURL}/>} {!!value.links.length&&<details><summary>{w.artifactLinks}</summary>{value.links.map(h=><p key={h}><a href={h} target="_blank" rel="noopener noreferrer">{h}</a></p>)}</details>}</>}</div></div>
}
