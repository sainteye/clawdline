// A Bot's reply is Markdown, read as Claude Code's answers are elsewhere in the console:
// richText escapes everything first and leaves anything it does not recognise as text.
import { richText } from "../legacy/js/view/markdown.js"
import type * as W from "@clawdline/contract"
import { useEffect, useState } from "react"
import type React from "react"
import { buildChat } from "./chat-items"
import type { Words } from "./words"
export function ChatRows({messages,onQuote,w,language,attachmentURL}: {messages:readonly W.ProjectAssistantMessage[];onQuote:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null}) {
  const byID=new Map(messages.map(m=>[m.id,m]))
  // Actions stay out of the way until a message is tapped, as in a messaging app.
  const [open,setOpen]=useState<string|null>(null),[copied,setCopied]=useState<string|null>(null)
  useEffect(()=>{
    if(!open)return
    const away=(e:Event)=>{if(!(e.target as Element|null)?.closest?.(`[data-message-id="${CSS.escape(open)}"]`))setOpen(null)}
    const key=(e:KeyboardEvent)=>{if(e.key==="Escape")setOpen(null)}
    document.addEventListener("pointerdown",away);document.addEventListener("keydown",key)
    return()=>{document.removeEventListener("pointerdown",away);document.removeEventListener("keydown",key)}
  },[open])
  const toggle=(id:string,e:{target:EventTarget})=>{
    // Following a link, opening a picture or selecting text is not asking for actions.
    if((e.target as Element).closest?.("a,button,img,input,textarea")||window.getSelection?.()?.toString())return
    setOpen(o=>o===id?null:id);setCopied(null)
  }
  const copy=(m:W.ProjectAssistantMessage)=>{void navigator.clipboard?.writeText(m.text).then(()=>setCopied(m.id),()=>{})}
  return <>{buildChat(messages).map(item=>item.kind==="separator"?<div className="pa-separator" key={item.id}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:<article key={item.id} data-message-id={item.id} data-message-seq={item.message.seq} {...(item.message.kind==="user"?{tabIndex:0,"aria-haspopup":"menu" as const,"aria-expanded":open===item.id,onClick:(e:{target:EventTarget})=>toggle(item.id,e),onKeyDown:(e:React.KeyboardEvent)=>{if((e.key==="Enter"||e.key===" ")&&e.target===e.currentTarget){e.preventDefault();toggle(item.id,e)}}}:{})} className={`pa-message ${open===item.id?"pa-message-open":""} ${item.message.kind==="user"?"pa-mine":""} ${item.message.kind==="notice"?"pa-notice":""} ${item.groupStart?"pa-group-start":""}`}>
    {item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=>{const src=attachmentURL?.(a.id);return src?<a key={a.id} href={src} target="_blank" rel="noreferrer"><img src={src} alt={w.picture} loading="lazy"/></a>:<span key={a.id} className="pa-picture-missing">{w.picture}</span>})}</div>}{item.message.text&&(item.message.kind==="assistant"?<div className="pa-markdown" dangerouslySetInnerHTML={{__html:richText(item.message.text,{codeCopy:false})}}/>:<p>{item.message.kind==="notice"&&item.message.text.startsWith("[Clawdline restarted")?w.resumedNotice:item.message.text}</p>)}{item.message.kind!=="user"&&<button type="button" className="pa-quote" onClick={()=>onQuote(item.message)}><QuoteIcon/>{w.quote}</button>}{open===item.id&&<div className="pa-message-actions" role="menu" aria-label={w.messageActions}><button role="menuitem" autoFocus onClick={()=>{onQuote(item.message);setOpen(null)}}>{w.quote}</button>{item.message.text&&<button role="menuitem" onClick={()=>copy(item.message)}>{copied===item.id?w.copiedMessage:w.copyMessage}</button>}</div>}
  </article>)}</>
}

/** A reply arrow: the mark messaging apps use for answering one message. */
function QuoteIcon(){
  return <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M6.5 4 2.5 8l4 4"/><path d="M2.5 8h6.5a4.5 4.5 0 0 1 4.5 4.5V13"/></svg>
}
