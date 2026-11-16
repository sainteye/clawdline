// A Bot's reply is Markdown, read as Claude Code's answers are elsewhere in the console:
// richText escapes everything first and leaves anything it does not recognise as text.
import { botReplyHasBrowser } from "./bot-browser-entry"
import { catalogWord } from "../catalog"
import {InputCard} from "./InputCard"
import {OutputCard} from "./OutputCard"
import { richText } from "../legacy/js/view/markdown.js"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import type * as W from "@clawdline/contract"
import { useEffect, useState } from "react"
import type React from "react"
import { buildTimelineChat } from "./chat-items"
import { WorkResultCard } from "./WorkResultCard"
import type { Words } from "./words"
export function ChatRows({timeline,onQuote,onOpenWork,w,language,attachmentURL,source,botKey,browserTraces=[],onOpenBrowser}: {browserTraces?:readonly W.ProjectAssistantTrace[];onOpenBrowser?:()=>void;timeline:readonly W.ProjectAssistantReadViewItem[];onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void;onQuote:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey}) {
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
  const byID=new Map(messages.map(m=>[m.id,m]))
  const artifacts=new Map<string,W.ProjectAssistantMessage>();for(const m of messages){if(m.output?.artifact_id)artifacts.set(m.output.artifact_id,m)}
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
  return <>{buildTimelineChat(timeline).map(item=>item.kind==="separator"?<div className="pa-separator" key={JSON.stringify([item.kind,item.id])}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:item.kind==="work"?<WorkResultCard key={JSON.stringify([item.kind,item.id])} work={item.work} w={w} onOpen={onOpenWork}/>:<article key={JSON.stringify([item.kind,item.id])} data-message-id={item.id} data-message-seq={item.message.seq} {...(item.message.kind==="user"?{tabIndex:0,"aria-haspopup":"menu" as const,"aria-expanded":open===item.id,onClick:(e:{target:EventTarget})=>toggle(item.id,e),onKeyDown:(e:React.KeyboardEvent)=>{if((e.key==="Enter"||e.key===" ")&&e.target===e.currentTarget){e.preventDefault();toggle(item.id,e)}}}:{})} className={`pa-message ${open===item.id?"pa-message-open":""} ${item.message.kind==="user"?"pa-mine":""} ${item.message.kind==="notice"?"pa-notice":""} ${item.groupStart?"pa-group-start":""}`}>
    {source&&item.message.files?.map(file=><InputCard key={file.id} message={item.message} file={file} source={source} w={w}/>)}{item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=><BotPicture key={a.id} id={a.id} localURL={attachmentURL?.(a.id)??null} source={source} botKey={botKey} w={w}/>)}</div>}{item.message.output&&source&&<OutputCard message={item.message.output.artifact_id?(artifacts.get(item.message.output.artifact_id)??item.message):item.message} source={source} w={w}/>}{item.message.text&&!item.message.output&&(item.message.kind==="assistant"?<div className="pa-markdown" dangerouslySetInnerHTML={{__html:richText(item.message.text,{codeCopy:false})}}/>:<p>{item.message.kind==="notice"&&item.message.text.startsWith("[Clawdline restarted")?w.resumedNotice:item.message.text}</p>)}{onOpenBrowser&&botReplyHasBrowser(item.message,browserTraces)&&<button type="button" className="pa-reply-browser pa-pill" onClick={onOpenBrowser}>{catalogWord("botBrowser","openReply")}</button>}{item.message.kind!=="user"&&<button type="button" className="pa-quote" onClick={()=>onQuote(item.message)}><QuoteIcon/>{w.quote}</button>}{open===item.id&&<div className="pa-message-actions" role="menu" aria-label={w.messageActions}><button role="menuitem" autoFocus onClick={()=>{onQuote(item.message);setOpen(null)}}>{w.quote}</button>{item.message.output&&source&&<OutputCard message={item.message.output.artifact_id?(artifacts.get(item.message.output.artifact_id)??item.message):item.message} source={source} w={w}/>}{item.message.text&&<button role="menuitem" onClick={()=>copy(item.message)}>{copied===item.id?w.copiedMessage:w.copyMessage}</button>}</div>}
  </article>)}</>
}

/** A reply arrow: the mark messaging apps use for answering one message. */
function QuoteIcon(){
  return <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d="M6.5 4 2.5 8l4 4"/><path d="M2.5 8h6.5a4.5 4.5 0 0 1 4.5 4.5V13"/></svg>
}

/** Each mounted picture owns its request; a source change discards late bytes. */
function BotPicture({id,localURL,source,botKey,w}:{id:string;localURL:string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey;w:Words}){
  const [result,setResult]=useState<{source:SourceClient;id:string;machine:string;bot:string;url?:string;error?:string}|null>(null)
  const [attempt,setAttempt]=useState(0)
  const machine=botKey?.machine_id??"",bot=botKey?.bot_id??""
  useEffect(()=>{
    if(localURL||!source?.attachment||!machine||!bot)return
    const controller=new AbortController();let objectURL:string|undefined
    setResult(null)
    void source.attachment({machine_id:machine,bot_id:bot},id,controller.signal).then(blob=>{
      if(!controller.signal.aborted){objectURL=URL.createObjectURL(blob);setResult({source,id,machine,bot,url:objectURL})}
    },error=>{if(!controller.signal.aborted)setResult({source,id,machine,bot,error:errorCode(error)})})
    return()=>{controller.abort();if(objectURL)URL.revokeObjectURL(objectURL)}
  },[localURL,source,machine,bot,id,attempt])
  const current=result?.source===source&&result?.id===id&&result?.machine===machine&&result?.bot===bot?result:null
  const url=localURL??current?.url
  return url?<a href={url} target="_blank" rel="noreferrer"><img src={url} alt={w.picture} loading="lazy"/></a>:<span className="pa-picture-missing" role="status">{current?.error?<>{w.pictureUnavailable} ({current.error}) <button type="button" onClick={()=>setAttempt(n=>n+1)}>{w.retry}</button></>:source?.attachment?w.pictureLoading:w.picture}</span>
}
