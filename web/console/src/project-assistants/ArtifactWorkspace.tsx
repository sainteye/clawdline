import {lazy,Suspense,useLayoutEffect,useMemo,useRef,useState,type ReactNode} from "react"
import type {ProjectAssistantMessage} from "@clawdline/contract"
import {catalogWord} from "../catalog"
import {afterSurfaceExit} from "./surface-motion"
import {artifactFrame} from "./outputs"
import {errorCode} from "./client"
import type {Words} from "./words"
import "./artifact-workspace.css"

const tr=(key:string)=>catalogWord("projectAssistants",key)
const ArtifactComposer=lazy(()=>import("./Composer").then(module=>({default:module.Composer})))

export type ArtifactSendResult =
  | {state:"accepted";turn:string}
  | {state:"refused"|"unknown";code?:string}

export interface ArtifactComposerAccess {
  blocked:boolean
  read:(id:string)=>string
  write:(id:string,text:string)=>void
  send:(message:ProjectAssistantMessage,text:string)=>Promise<ArtifactSendResult>
  progress:(turn:string)=>"replied"|"failed"|null
}

export function ArtifactIcon({size=20}:{size?:number}){
  return <svg viewBox="0 0 24 24" width={size} height={size} stroke="currentColor" strokeWidth="1.6" fill="none" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="4.5" y="2.5" width="15" height="19" rx="2"/><path d="M8 7h8M8 11h8m-8 4h6m-6 3h4"/></svg>
}

export function ArtifactWorkspace({name,message,html,links,w,state,rename,access,onClose,save,openBrowser,refresh}:{
  name:string;message:ProjectAssistantMessage;html:string|undefined;links:readonly string[];w:Words;
  state:ReactNode;rename?:ReactNode;access?:ArtifactComposerAccess;onClose:()=>void;save:()=>void;openBrowser:()=>void;refresh:()=>void;
}) {
  const artifactID=message.output!.artifact_id
  const frame=useMemo(()=>html===undefined?undefined:artifactFrame(html),[html])
  const [draft,setDraft]=useState(()=>access?.read(artifactID)??"")
  const [sending,setSending]=useState(false),[receipt,setReceipt]=useState<ArtifactSendResult|null>(null)
  const [closing,setClosing]=useState(false)
  const dialog=useRef<HTMLDialogElement>(null),close=useRef<HTMLButtonElement>(null),share=useRef<HTMLDetailsElement>(null)
  const pending=useRef(false)
  const action=useRef(onClose);action.current=onClose
  useLayoutEffect(()=>{
    const node=dialog.current,opener=document.activeElement
    node?.showModal();close.current?.focus()
    return()=>{
      node?.close()
      if(opener instanceof HTMLElement&&opener.isConnected)opener.focus({preventScroll:true})
    }
  },[])
  useLayoutEffect(()=>{if(closing&&dialog.current)return afterSurfaceExit(dialog.current,()=>action.current())},[closing])
  const update=(value:string)=>{setDraft(value);access?.write(artifactID,value);setReceipt(null)}
  const send=async()=>{
    if(!draft.trim()||!access||access.blocked||pending.current)return
    pending.current=true;setSending(true);setReceipt(null)
    try{
      const result=await access.send(message,draft)
      if(result.state==="accepted")update("")
      setReceipt(result)
    }catch(error){
      setReceipt({state:"refused",code:errorCode(error)})
    }
    finally{pending.current=false;setSending(false)}
  }
  const delivery=receipt?.state==="accepted"?access?.progress(receipt.turn):null
  return <dialog className="pa-artifact-workspace" ref={dialog} data-closing={closing||undefined} inert={closing} aria-label={name}
    onCancel={event=>{
      event.preventDefault()
      if(share.current?.open){share.current.open=false;share.current.querySelector("summary")?.focus();return}
      if(document.activeElement?.closest(".pa-artifact-rename form, .pa-artifact-composer textarea"))return
      setClosing(true)
    }}>
    <header className="pa-artifact-bar">
      <button type="button" className="pa-circle" ref={close} aria-label={tr("closeArtifact")} onClick={()=>setClosing(true)}>×</button>
      {rename??<span className="pa-artifact-heading"><ArtifactIcon size={17}/><span>{name}</span></span>}
      <details className="pa-artifact-share" ref={share}>
        <summary className="pa-circle" aria-label={tr("shareArtifact")} title={tr("shareArtifact")}>
          <svg viewBox="0 0 20 20" width="19" height="19" stroke="currentColor" strokeWidth="1.45" fill="none" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M10 12V2m-3 3 3-3 3 3m1 3h3v9H3V8h3"/></svg>
        </summary>
        <div className="pa-artifact-share-choices">
          <button type="button" disabled={html===undefined} onClick={save}>{w.saveHTML}</button>
          <button type="button" disabled={html===undefined} onClick={openBrowser}>{tr("libraryOpenBrowser")}</button>
          <button type="button" disabled={html===undefined} onClick={refresh}>{w.refresh}</button>
          {!!links.length&&<details><summary>{w.artifactLinks}</summary>{links.map(href=><a key={href} href={href} target="_blank" rel="noopener noreferrer">{href}</a>)}</details>}
        </div>
      </details>
    </header>
    <div className="pa-artifact-body">
      {html===undefined?<div className="pa-artifact-load">{state}</div>:<iframe className="pa-artifact-frame" title={name} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={frame}/>}
    </div>
    {access&&<div className="pa-artifact-composer">
      {(sending||receipt)&&<div className="pa-artifact-send-status">
        <span role={receipt?.state==="refused"||receipt?.state==="unknown"?"alert":"status"}>
          {sending?<><span className="pa-send-spinner" aria-hidden="true"/>{w.sending}</>:receipt?.state==="accepted"
            ?delivery==="replied"?w.artifactReplied:delivery==="failed"?w.artifactReplyFailed:w.artifactSent
            :receipt?.state==="unknown"?w.artifactSendUnknown:<>{tr("artifactSendFailed")}{receipt?.code&&<small>({receipt.code})</small>}</>}
        </span>
        {!sending&&receipt?.state!=="refused"&&<button type="button" onClick={()=>setClosing(true)}>{w.artifactViewChat}</button>}
      </div>}
      <Suspense fallback={null}><ArtifactComposer plain text={draft} onText={update} onSend={()=>void send()} disabled={access.blocked||sending||html===undefined} draftEditable={!sending&&html!==undefined} busy={sending} onClearQuote={()=>{}}
        w={{...w,compose:tr("artifactCompose").replace("{title}",name)}}/></Suspense>
    </div>}
  </dialog>
}
