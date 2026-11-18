import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import { useEffect, useRef, useState } from "react"
import type { SourceClient } from "./client"
import { authorized } from "./client"
import type { ReadScope } from "./read-views"
import { readableTemplate } from "./bot-template"
import type { Words } from "./words"
import "./bot-template.css"

/** Read and copy only this mounted conversation's currently authorized settings. */
export function BotTemplate({opener,source,current,scope,available,w,onClose}: {
  opener:HTMLButtonElement;source:SourceClient;current:()=>readonly SourceClient[];scope:ReadScope;available:()=>boolean;w:Words;onClose:()=>void;
}) {
  const [preview,setPreview]=useState<string|null>(null),[state,setState]=useState("loading")
  const dialog=useRef<HTMLDialogElement>(null),control=useRef<AbortController|null>(null),busy=useRef(false)
  const closeRef=useRef(onClose);closeRef.current=onClose
  const availableRef=useRef(available);availableRef.current=available
  const marker=useRef(crypto.randomUUID()).current
  const close=()=>{if(history.state?.paTemplate===marker)history.back();else closeRef.current()}
  useEffect(()=>{
    setPreview(null);setState("loading")
    const node=dialog.current!
    const before=history.state
    const controller=new AbortController();control.current=controller
    // StrictMode's discarded effect must not leave an extra browser Back entry.
    queueMicrotask(()=>{if(!controller.signal.aborted)history.pushState({...before,paTemplate:marker},"")})
    const back=()=>closeRef.current()
    window.addEventListener("popstate",back)
    bindNativeModalBackdrop(node)
    node.showModal()
    void authorized(source,current,s=>source.get(scope.key,s),controller.signal).then(answer=>{
      if(controller.signal.aborted)return
      if(!availableRef.current()||source.current?.()===false)throw Error("unavailable")
      setPreview(readableTemplate(answer,scope));setState("ready")
    }).catch(()=>{if(!controller.signal.aborted){setPreview(null);setState("unavailable")}})
    return()=>{
      controller.abort();node.close();window.removeEventListener("popstate",back)
      if(history.state?.paTemplate===marker)history.replaceState(before,"")
      // Browser Back restores its own focus after the popstate listener returns.
      requestAnimationFrame(()=>{
        if(opener.isConnected&&!document.querySelector("dialog[open]"))opener.focus({preventScroll:true})
      })
    }
  },[opener,source,current,scope,marker])
  const copy=async()=>{
    if(busy.current||preview===null)return
    busy.current=true;setState("copying")
    const controller=control.current!
    try {
      if(controller.signal.aborted||!availableRef.current()||source.current?.()===false)throw Error("unavailable")
      const answer=await authorized(source,current,s=>source.get(scope.key,s),controller.signal)
      const encoded=readableTemplate(answer,scope)
      if(controller.signal.aborted||!availableRef.current()||source.current?.()===false||encoded!==preview)throw Error("unavailable")
      // Never copy a different snapshot from the one the person reviewed.
      if(!navigator.clipboard?.writeText){setState("clipboard");return}
      try{await navigator.clipboard.writeText(preview)}catch{if(!controller.signal.aborted)setState("clipboard");return}
      // Once the authorized clipboard write succeeds, report its actual outcome.
      if(!controller.signal.aborted)setState("copied")
    } catch {
      if(!controller.signal.aborted){setPreview(null);setState("unavailable")}
    } finally {busy.current=false}
  }
  return <dialog data-modal-dismiss={String(state !== "copying")} ref={dialog} className="pa-bot-template" aria-label={w.templateCreate} onCancel={event=>{event.preventDefault();close()}} onKeyDown={event=>{
    // Enter is never a window-wide shortcut; composition cannot activate a button.
    if(event.key==="Enter"&&event.nativeEvent.isComposing)event.preventDefault()
    event.stopPropagation()
  }}>
    <header><h2>{w.templateCreate}</h2><button type="button" onClick={close}>{w.close}</button></header>
    <p>{w.templatePrivacy}</p><p>{w.templateCoverage}</p>
    {state==="loading"&&<p role="status">{w.templateLoading}</p>}
    {preview!==null&&<textarea aria-label={w.templatePreview} readOnly value={preview} spellCheck={false}/>}
    {state==="unavailable"&&<p role="alert">{w.templateUnavailable}</p>}
    {state==="clipboard"&&<p role="alert">{w.templateClipboardError}</p>}
    {state==="copied"&&<p role="status">{w.templateCopied}</p>}
    <footer><button type="button" disabled={preview===null||state==="copying"} aria-busy={state==="copying"||undefined} onClick={()=>void copy()}>{state==="copying"?w.templateCopying:w.templateCopy}</button></footer>
  </dialog>
}
