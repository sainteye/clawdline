// A Bot's reply is Markdown, read as Claude Code's answers are elsewhere in the console:
// richText escapes everything first and leaves anything it does not recognise as text.
import {MessageReactions} from "./MessageReactions"
import { botReplyHasBrowser } from "./bot-browser-entry"
import { catalogWord } from "../catalog"
import {InputCard} from "./InputCard"
import type {ArtifactComposerAccess} from "./ArtifactWorkspace"
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
import { BotAvatar } from "./BotAvatar"
import { exchangeEntries, exchangePeer } from "./exchanges"
import { sameConversation, sameBot } from "./view-state"
import type { Words } from "./words"
export function ChatRows({timeline,onQuote,onOpenWork,w,language,attachmentURL,source,botKey,browserTraces=[],onOpenBrowser,dismissActionsKey,arrivals,onArrivalEnd,afterMessage,roster,artifactComposer}: {artifactComposer?:ArtifactComposerAccess;roster?:readonly W.ProjectAssistantBot[];dismissActionsKey?:string;browserTraces?:readonly W.ProjectAssistantTrace[];onOpenBrowser?:(opener:HTMLButtonElement)=>void;timeline:readonly W.ProjectAssistantReadViewItem[];onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void;onQuote?:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey;arrivals?:ReadonlySet<string>;onArrivalEnd?:(id:string)=>void;afterMessage?:(message:W.ProjectAssistantMessage)=>React.ReactNode}) {
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
  const byID=new Map(messages.map(m=>[m.id,m]))
  const artifacts=new Map<string,W.ProjectAssistantMessage>();for(const m of messages){if(m.output?.artifact_id)artifacts.set(m.output.artifact_id,m)}
  const [copied,setCopied]=useState<string|null>(null)
  const [exchange,setExchange]=useState<{message:W.ProjectAssistantMessage;source:SourceClient}|null>(null)
  const exchangeWord=exchangeWords(language)
  const exchangeScope=messages.find(m=>botKey&&sameBot(m.fence.key,botKey))?.fence
  const entries=exchangeScope?exchangeEntries(messages,exchangeScope):[]
  const exchanges=new Map(entries.map(entry=>[entry.exchange.id,entry]))
  const exchangeMessage=exchange?.source===source&&exchange?messages.find(m=>m.id===exchange.message.id&&sameConversation(m.fence,exchange.message.fence)&&m.exchange?.id===exchange.message.exchange?.id):null
  const first=new Set<string>()
  const findPeer=(entry:W.ProjectAssistantExchange,scope:W.ProjectAssistantFence)=>{
    const peer=exchangePeer(entry,scope.key)
    return roster?.find(bot=>sameBot(bot.key,peer.key)&&bot.conversation_id===peer.conversation_id&&sameConversation({key:bot.key,project:bot.project,conversation_id:bot.conversation_id},{key:peer.key,project:scope.project,conversation_id:peer.conversation_id}))
  }
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
  return <>{buildTimelineChat(timeline).map(item=>item.kind==="separator"?<div className="pa-separator" key={JSON.stringify([item.kind,item.id])}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:item.kind==="work"?<WorkResultCard key={JSON.stringify([item.kind,item.id])} work={item.work} w={w} onOpen={onOpenWork}/>:item.message.exchange&&source&&botKey&&exchangeScope?(()=>{
    const entry=exchanges.get(item.message.exchange.id)
    if(!entry||first.has(entry.exchange.id)||!sameConversation(item.message.fence,exchangeScope))return null
    first.add(entry.exchange.id)
    const {exchange:reference}=entry, peer=exchangePeer(reference,botKey),bot=findPeer(reference,item.message.fence)
    const outgoing=sameBot(reference.source.key,botKey),failed=["failed","cancelled","unknown"].includes(reference.outcome)
    return <div key={item.id} className="pa-exchange-attribution" data-message-id={item.id} data-message-seq={item.message.seq}>
      <button type="button" className="pa-exchange-entry" data-failed={failed||undefined} onClick={()=>setExchange({message:item.message,source})}
        aria-label={`${outgoing?exchangeWord.to:exchangeWord.from} ${bot?.name||peer.key.bot_id}`} title={exchangeWord.open}>
        {failed&&<span aria-hidden="true">!</span>}<span>{outgoing?exchangeWord.to:exchangeWord.from}</span>
        {bot&&<BotAvatar appearance={bot.appearance} size={17}/>}<strong>{bot?.name||peer.key.bot_id}</strong><span aria-hidden="true">›</span>
      </button>
    </div>
  })():<Fragment key={JSON.stringify([item.kind,item.id])}><article data-arrival={arrivals?.has(item.id)||undefined} onAnimationEnd={e=>{if(e.animationName==="pa-bubble-enter")onArrivalEnd?.(item.id)}} data-message-id={item.id} data-message-seq={item.message.seq} onClick={(e:React.MouseEvent)=>onArticleClick(e)} tabIndex={0} onKeyDown={(e:React.KeyboardEvent)=>{if((e.key==="Enter"||e.key===" ")&&e.target===e.currentTarget){e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}}} onContextMenu={e=>{e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}} className={`pa-message ${item.message.kind==="user"?"pa-mine":""} ${item.message.kind==="notice"?"pa-notice":""} ${item.groupStart?"pa-group-start":""}`}>
    <div className="pa-message-bubble">{source&&item.message.files?.map(file=><InputCard key={file.id} message={item.message} file={file} source={source} w={w}/>)}{item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=><BotPicture key={a.id} id={a.id} localURL={attachmentURL?.(a.id)??null} source={source} botKey={botKey} w={w}/>)}</div>}{item.message.output&&source&&<OutputCard artifactComposer={artifactComposer} message={item.message.output.artifact_id?(artifacts.get(item.message.output.artifact_id)??item.message):item.message} source={source} w={w}/>}{item.message.text&&!item.message.output&&(item.message.kind==="assistant"?<BotMessageMarkdown text={item.message.text} w={w}/>:<p>{item.message.kind==="notice"&&item.message.text.startsWith("[Clawdline restarted")?w.resumedNotice:item.message.text}</p>)}{onOpenBrowser&&botReplyHasBrowser(item.message,browserTraces)&&<button type="button" className="pa-reply-browser pa-pill" onClick={event=>onOpenBrowser(event.currentTarget)}>{catalogWord("botBrowser","openReply")}</button>}</div>
    <MessageReactions key={JSON.stringify([source?.machineID,source?.authorizationRevision,item.message.id])} message={item.message} source={onQuote?source:undefined} dismissKey={dismissActionsKey} onQuote={onQuote?()=>onQuote(item.message):undefined} onCopy={item.message.text?()=>copy(item.message):undefined} copyLabel={copied===item.id?w.copiedMessage:w.copyMessage}/>

  </article>{afterMessage?.(item.message)}</Fragment>)}{exchangeMessage?.exchange&&source&&botKey&&exchangeMessage.fence.key.machine_id===botKey.machine_id&&exchangeMessage.fence.key.bot_id===botKey.bot_id&&<ExchangePanel source={source} scope={exchangeMessage.fence} reference={exchangeMessage.exchange} entries={entries} roster={roster} language={language} w={w} onClose={()=>setExchange(null)}/>}</>
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
