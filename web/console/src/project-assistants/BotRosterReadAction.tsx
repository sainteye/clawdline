import {useEffect,useRef,useState} from "react"
import type * as W from "@clawdline/contract"
import {errorCode,type SourceClient} from "./client"
import {canMarkConversationRead,markConversationRead} from "./conversation-read"
import type {Words} from "./words"

export function BotRosterReadAction({item,source,current,readerID,onRead,w,focusRow}:{
  item:W.ProjectAssistantBotSummary;source:SourceClient;current:()=>readonly SourceClient[];
  readerID:string;onRead:(source:SourceClient)=>Promise<void>;w:Words;
  focusRow?:()=>HTMLButtonElement|null|undefined;
}) {
  const [pending,setPending]=useState(false)
  const [result,setResult]=useState("")
  const active=useRef<AbortController|null>(null)
  const {bot}=item
  useEffect(()=>{
    setPending(false);setResult("")
    return()=>{active.current?.abort();active.current=null}
  },[source,readerID,bot.conversation_id])
  const allowed=canMarkConversationRead(item.source)&&source.current?.()!==false
  const mark=async(button:HTMLButtonElement)=>{
    if(active.current||!allowed)return
    const control=new AbortController()
    active.current=control;setPending(true);setResult("")
    try {
      await markConversationRead(source,current,bot,readerID,control.signal)
      if(control.signal.aborted)return
      if(document.activeElement===button)(focusRow?.()??button.parentElement?.querySelector<HTMLButtonElement>(".pa-bot-row"))?.focus({preventScroll:true})
      setResult("marked")
      await onRead(source)
    }catch(error){if(!control.signal.aborted)setResult(errorCode(error))}
    finally{if(active.current===control){active.current=null;if(!control.signal.aborted)setPending(false)}}
  }
  return <>
    {(item.unread_count>0||pending)&&<button type="button" className="pa-roster-read" title={w.markAllRead}
      aria-label={`${w.markAllRead} · ${bot.name}`} disabled={!allowed}
      aria-disabled={pending||undefined} aria-busy={pending||undefined} onClick={event=>void mark(event.currentTarget)}>
      <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="m2 10 4 4 7-8m-2 8 7-8"/></svg>
    </button>}
    <span role="status" className="pa-roster-read-status">{pending?w.markingRead:result==="marked"?w.readMarked:""}</span>
    {result&&result!=="marked"&&item.unread_count>0&&<p role="alert" className="pa-roster-read-error">{w.unavailable} ({result})</p>}
  </>
}
