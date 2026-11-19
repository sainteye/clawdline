// A Bot's reply is Markdown, read as Claude Code's answers are elsewhere in the console:
// richText escapes everything first and leaves anything it does not recognise as text.
import {MessageReactions} from "./MessageReactions"
import { botReplyHasBrowser } from "./bot-browser-entry"
import { catalogWord } from "../catalog"
import {InputCard} from "./InputCard"
import {OutputCard} from "./OutputCard"
import { BotMessageMarkdown } from "./BotMessageMarkdown"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import type * as W from "@clawdline/contract"
import { Fragment, useEffect, useState } from "react"
import type React from "react"
import { buildTimelineChat } from "./chat-items"
import { WorkResultCard } from "./WorkResultCard"
import { ExchangePanel } from "./ExchangePanel"
import { exchangeWords } from "./exchange-words"
import { sameConversation } from "./view-state"
import type { Words } from "./words"
export function ChatRows({timeline,onQuote,onOpenWork,w,language,attachmentURL,source,botKey,browserTraces=[],onOpenBrowser,dismissActionsKey,arrivals,onArrivalEnd,afterMessage}: {dismissActionsKey?:string;browserTraces?:readonly W.ProjectAssistantTrace[];onOpenBrowser?:(opener:HTMLButtonElement)=>void;timeline:readonly W.ProjectAssistantReadViewItem[];onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void;onQuote?:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey;arrivals?:ReadonlySet<string>;onArrivalEnd?:(id:string)=>void;afterMessage?:(message:W.ProjectAssistantMessage)=>React.ReactNode}) {
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
  const byID=new Map(messages.map(m=>[m.id,m]))
  const artifacts=new Map<string,W.ProjectAssistantMessage>();for(const m of messages){if(m.output?.artifact_id)artifacts.set(m.output.artifact_id,m)}
  const [copied,setCopied]=useState<string|null>(null)
  const [exchange,setExchange]=useState<{message:W.ProjectAssistantMessage;source:SourceClient}|null>(null)
  const exchangeWord=exchangeWords(language)
  const exchangeMessage=exchange?.source===source&&exchange?messages.find(m=>m.id===exchange.message.id&&sameConversation(m.fence,exchange.message.fence)&&m.exchange?.id===exchange.message.exchange?.id):null
  const onArticleClick=(e:React.MouseEvent)=>{
    const copyBtn=(e.target as Element).closest?.<HTMLButtonElement>("button.codecopy")
    if(copyBtn){
      const code=copyBtn.getAttribute("data-code-copy")
      if(code){
        void navigator.clipboard?.writeText(code).then(()=>{
          copyBtn.classList.add("copied")
          setTimeout(()=>copyBtn.classList.remove("copied"),1500)
        })
      }
      return
    }

  }
  const copy=(m:W.ProjectAssistantMessage)=>{void navigator.clipboard?.writeText(m.text).then(()=>setCopied(m.id),()=>{})}
  return <>{buildTimelineChat(timeline).map(item=>item.kind==="separator"?<div className="pa-separator" key={JSON.stringify([item.kind,item.id])}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:item.kind==="work"?<WorkResultCard key={JSON.stringify([item.kind,item.id])} work={item.work} w={w} onOpen={onOpenWork}/>:<Fragment key={JSON.stringify([item.kind,item.id])}><article data-arrival={arrivals?.has(item.id)||undefined} onAnimationEnd={e=>{if(e.animationName==="pa-bubble-enter")onArrivalEnd?.(item.id)}} data-message-id={item.id} data-message-seq={item.message.seq} onClick={(e:React.MouseEvent)=>onArticleClick(e)} tabIndex={0} onKeyDown={(e:React.KeyboardEvent)=>{if((e.key==="Enter"||e.key===" ")&&e.target===e.currentTarget){e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}}} onContextMenu={e=>{e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}} className={`pa-message ${item.message.kind==="user"?"pa-mine":""} ${item.message.kind==="notice"?"pa-notice":""} ${item.groupStart?"pa-group-start":""}`}>
    <div className="pa-message-bubble">{source&&item.message.files?.map(file=><InputCard key={file.id} message={item.message} file={file} source={source} w={w}/>)}{item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=><BotPicture key={a.id} id={a.id} localURL={attachmentURL?.(a.id)??null} source={source} botKey={botKey} w={w}/>)}</div>}{item.message.output&&source&&<OutputCard message={item.message.output.artifact_id?(artifacts.get(item.message.output.artifact_id)??item.message):item.message} source={source} w={w}/>}{item.message.text&&!item.message.output&&(item.message.kind==="assistant"?<BotMessageMarkdown text={item.message.text} w={w}/>:<p>{item.message.kind==="notice"&&item.message.text.startsWith("[Clawdline restarted")?w.resumedNotice:item.message.text}</p>)}{item.message.exchange&&source&&botKey&&item.message.fence.key.machine_id===botKey.machine_id&&item.message.fence.key.bot_id===botKey.bot_id&&<button type="button" className="pa-exchange-entry pa-pill" onClick={()=>setExchange({message:item.message,source})}>{exchangeWord.open}</button>}{onOpenBrowser&&botReplyHasBrowser(item.message,browserTraces)&&<button type="button" className="pa-reply-browser pa-pill" onClick={event=>onOpenBrowser(event.currentTarget)}>{catalogWord("botBrowser","openReply")}</button>}</div>
    <MessageReactions key={JSON.stringify([source?.machineID,source?.authorizationRevision,item.message.id])} message={item.message} source={onQuote?source:undefined} dismissKey={dismissActionsKey} onQuote={onQuote?()=>onQuote(item.message):undefined} onCopy={item.message.text?()=>copy(item.message):undefined} copyLabel={copied===item.id?w.copiedMessage:w.copyMessage}/>

  </article>{afterMessage?.(item.message)}</Fragment>)}{exchangeMessage?.exchange&&source&&botKey&&exchangeMessage.fence.key.machine_id===botKey.machine_id&&exchangeMessage.fence.key.bot_id===botKey.bot_id&&<ExchangePanel source={source} scope={exchangeMessage.fence} reference={exchangeMessage.exchange} language={language} onClose={()=>setExchange(null)}/>}</>
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
