// A Bot's reply is Markdown, read as Claude Code's answers are elsewhere in the console:
// richText escapes everything first and leaves anything it does not recognise as text.
import {MessageReactions} from "./MessageReactions"
import { botReplyHasBrowser } from "./bot-browser-entry"
import { catalogWord } from "../catalog"
import {InputCard} from "./InputCard"
import {ArtifactIcon,type ArtifactComposerAccess} from "./ArtifactWorkspace"
import {OutputCard} from "./OutputCard"
import {OutputViewer} from "./OutputViewer"
import { BotMessageMarkdown } from "./BotMessageMarkdown"
import { BotMentionText, type BotMentionCandidate } from "./BotMentionField"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import type * as W from "@clawdline/contract"
import { Fragment, useCallback, useEffect, useState } from "react"
import type React from "react"
import { buildTimelineChat } from "./chat-items"
import { WorkResultCard } from "./WorkResultCard"
import { ExchangePanel } from "./ExchangePanel"
import { exchangeWords } from "./exchange-words"
import { BotAvatar } from "./BotAvatar"
import { exchangeBot, exchangeEntries, exchangePeer, loadExchangeBot } from "./exchanges"
import type { ExchangeScope } from "./exchanges"
import { sameConversation, sameBot } from "./view-state"
import type { Words } from "./words"
export function ChatRows({timeline,onQuote,onOpenWork,w,language,attachmentURL,source,botKey,browserTraces=[],onOpenBrowser,dismissActionsKey,arrivals,onArrivalEnd,afterMessage,roster,artifactComposer,mentions}: {mentions?:readonly BotMentionCandidate[];artifactComposer?:ArtifactComposerAccess;roster?:readonly W.ProjectAssistantBot[];dismissActionsKey?:string;browserTraces?:readonly W.ProjectAssistantTrace[];onOpenBrowser?:(opener:HTMLButtonElement)=>void;timeline:readonly W.ProjectAssistantReadViewItem[];onOpenWork?:(work:W.ProjectAssistantWorkLink)=>void;onQuote?:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey;arrivals?:ReadonlySet<string>;onArrivalEnd?:(id:string)=>void;afterMessage?:(message:W.ProjectAssistantMessage)=>React.ReactNode}) {
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
  const byID=new Map(messages.map(m=>[m.id,m]))
  const artifacts=new Map<string,W.ProjectAssistantMessage>();for(const m of messages){if(m.output?.artifact_id)artifacts.set(m.output.artifact_id,m)}
  const chat=buildTimelineChat(timeline.filter(item=>!item.message?.output?.quiet))
  // A reply can follow a publication by several bubbles. Keep the original
  // card in sequence and make its latest version reachable from that reply.
  const lastReplyByTurn=new Map<string,W.ProjectAssistantMessage>()
  for(const item of chat){
    if(item.kind!=="message")continue
    const m=item.message
    if(m.kind==="assistant"&&m.turn_id&&m.text.trim()&&!m.output&&!m.exchange)lastReplyByTurn.set(m.turn_id,m)
  }
  const artifactsByReply=new Map<string,Map<string,W.ProjectAssistantMessage>>()
  for(const item of chat){
    if(item.kind!=="message")continue
    const m=item.message,id=m.output?.artifact_id,reply=lastReplyByTurn.get(m.turn_id)
    if(!id||!reply||m.seq>=reply.seq||!sameConversation(reply.fence,m.fence))continue
    const links=artifactsByReply.get(reply.id)??new Map<string,W.ProjectAssistantMessage>()
    links.set(id,artifacts.get(id)??m)
    artifactsByReply.set(reply.id,links)
  }
  const [openArtifact,setOpenArtifact]=useState<{source:SourceClient;revision:string;id:string}|null>(null)
  const closeArtifact=useCallback(()=>setOpenArtifact(null),[])
  const canOpenArtifact=!!source?.output&&source.canOutputs?.()!==false
  const selectedArtifact=openArtifact&&openArtifact.source===source&&openArtifact.revision===source?.authorizationRevision&&canOpenArtifact
    ?artifacts.get(openArtifact.id):undefined
  const [copied,setCopied]=useState<string|null>(null)
  const [exchange,setExchange]=useState<{message:W.ProjectAssistantMessage;source:SourceClient}|null>(null)
  const exchangeWord=exchangeWords(language)
  const exchangeScope=messages.find(m=>botKey&&sameBot(m.fence.key,botKey))?.fence
  const entries=exchangeScope?exchangeEntries(messages,exchangeScope):[]
  const exchanges=new Map(entries.map(entry=>[entry.exchange.id,entry]))
  const exchangeMessage=exchange?.source===source&&exchange?messages.find(m=>m.id===exchange.message.id&&sameConversation(m.fence,exchange.message.fence)&&m.exchange?.id===exchange.message.exchange?.id):null
  const first=new Set<string>()
  const peers=exchangeScope?entries.map(entry=>exchangePeer(entry.exchange,exchangeScope.key)):[]
  const resolved=useExchangeParticipants(source,roster,peers)
  const participants=resolved.length?[...roster??[],...resolved]:roster
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
  return <>{chat.map(item=>item.kind==="separator"?<div className="pa-separator" key={JSON.stringify([item.kind,item.id])}><time dateTime={item.at}>{new Intl.DateTimeFormat(language,{month:"short",day:"numeric",hour:"numeric",minute:"2-digit"}).format(Date.parse(item.at))}</time></div>:item.kind==="work"?<WorkResultCard key={JSON.stringify([item.kind,item.id])} work={item.work} w={w} onOpen={onOpenWork}/>:item.message.exchange&&source&&botKey&&exchangeScope?(()=>{
    const entry=exchanges.get(item.message.exchange.id)
    if(!entry||first.has(entry.exchange.id)||!sameConversation(item.message.fence,exchangeScope))return null
    first.add(entry.exchange.id)
    const {exchange:reference}=entry,bot=exchangeBot(participants,exchangePeer(reference,botKey)),name=bot?.name||exchangeWord.unknownBot
    const outgoing=sameBot(reference.source.key,botKey),failed=["failed","cancelled","unknown"].includes(reference.outcome)
    return <div key={item.id} className="pa-exchange-attribution" data-message-id={item.id} data-message-seq={item.message.seq}>
      <button type="button" className="pa-exchange-entry" data-failed={failed||undefined} onClick={()=>setExchange({message:item.message,source})}
        aria-label={`${outgoing?exchangeWord.to:exchangeWord.from} ${name}`} title={exchangeWord.open}>
        {failed&&<span aria-hidden="true">!</span>}<span>{outgoing?exchangeWord.to:exchangeWord.from}</span>
        {bot&&<BotAvatar appearance={bot.appearance} size={17}/>}<strong>{name}</strong><span aria-hidden="true">›</span>
      </button>
    </div>
  })():<Fragment key={JSON.stringify([item.kind,item.id])}><article data-arrival={arrivals?.has(item.id)||undefined} onAnimationEnd={e=>{if(e.animationName==="pa-bubble-enter")onArrivalEnd?.(item.id)}} data-message-id={item.id} data-message-seq={item.message.seq} onClick={(e:React.MouseEvent)=>onArticleClick(e)} tabIndex={0} onKeyDown={(e:React.KeyboardEvent)=>{if((e.key==="Enter"||e.key===" ")&&e.target===e.currentTarget){e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}}} onContextMenu={e=>{e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}} className={`pa-message ${item.message.kind==="user"?"pa-mine":""} ${item.message.kind==="notice"?"pa-notice":""} ${item.groupStart?"pa-group-start":""}`}>
    <div className="pa-message-bubble">{source&&item.message.files?.map(file=><InputCard key={file.id} message={item.message} file={file} source={source} w={w}/>)}{item.message.quote_message_id&&<blockquote>{byID.get(item.message.quote_message_id)?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=><BotPicture key={a.id} id={a.id} localURL={attachmentURL?.(a.id)??null} source={source} botKey={botKey} w={w}/>)}</div>}{item.message.output&&source&&<OutputCard artifactComposer={artifactComposer} message={item.message.output.artifact_id?(artifacts.get(item.message.output.artifact_id)??item.message):item.message} source={source} w={w}/>}{item.message.text&&!item.message.output&&(item.message.kind==="assistant"?<BotMessageMarkdown text={item.message.text} w={w}/>:<p>{item.message.kind==="notice"&&item.message.text.startsWith("[Clawdline restarted")?w.resumedNotice:<BotMentionText text={item.message.text} candidates={mentions}/>}</p>)}{onOpenBrowser&&botReplyHasBrowser(item.message,browserTraces)&&<button type="button" className="pa-reply-browser pa-pill" onClick={event=>onOpenBrowser(event.currentTarget)}>{catalogWord("botBrowser","openReply")}</button>}
    {source&&artifactsByReply.has(item.id)&&<div className="pa-reply-artifacts">
      {Array.from(artifactsByReply.get(item.id)!,([id,publication])=><button type="button" key={id} aria-haspopup="dialog" disabled={!canOpenArtifact}
        onClick={()=>setOpenArtifact({source,revision:source.authorizationRevision,id})}>
        <ArtifactIcon size={18}/><span>{publication.output!.name}</span><span aria-hidden="true">›</span>
      </button>)}
    </div>}</div>
    <MessageReactions key={JSON.stringify([source?.machineID,source?.authorizationRevision,item.message.id])} message={item.message} source={onQuote?source:undefined} dismissKey={dismissActionsKey} onQuote={onQuote?()=>onQuote(item.message):undefined} onCopy={item.message.text?()=>copy(item.message):undefined} copyLabel={copied===item.id?w.copiedMessage:w.copyMessage}/>

  </article>{afterMessage?.(item.message)}</Fragment>)}
    {selectedArtifact&&source&&<OutputViewer artifactComposer={artifactComposer} item={{kind:"output",key:selectedArtifact.output!.artifact_id,message:selectedArtifact}}
      source={source} w={w} onClose={closeArtifact}/>}
    {exchangeMessage?.exchange&&source&&botKey&&exchangeMessage.fence.key.machine_id===botKey.machine_id&&exchangeMessage.fence.key.bot_id===botKey.bot_id&&<ExchangePanel key={JSON.stringify([source.authorizationRevision,exchangeMessage.fence.key,exchangeMessage.fence.conversation_id,exchangeMessage.exchange.id])} source={source} scope={exchangeMessage.fence} reference={exchangeMessage.exchange} entries={entries} roster={participants} language={language} w={w} onClose={()=>setExchange(null)}/>}</>
}

/** Resolve peers outside the loaded roster with one captured read in flight. */
function useExchangeParticipants(source:SourceClient|undefined,roster:readonly W.ProjectAssistantBot[]|undefined,peers:readonly ExchangeScope[]) {
  const missing=JSON.stringify(Array.from(new Set(peers.filter(peer=>!exchangeBot(roster,peer))
    .map(({key,project})=>JSON.stringify({key,project})))).sort())
  const revision=source?.authorizationRevision
  const [result,setResult]=useState<{source:SourceClient;revision:string;bots:W.ProjectAssistantBot[]}|null>(null)
  const loaded=result&&result.source===source&&result.revision===revision&&source?.current?.()!==false?result.bots:[]
  useEffect(()=>{
    if(!source||revision===undefined||missing==="[]")return
    const control=new AbortController()
    const bots=[...loaded]
    void (async()=>{
      for(const identity of JSON.parse(missing) as string[]){
        if(control.signal.aborted||source.current?.()===false||source.authorizationRevision!==revision)return
        const party=JSON.parse(identity) as Pick<ExchangeScope,"key"|"project">
        if(exchangeBot(bots,party))continue
        try {
          bots.push(await loadExchangeBot(source,party,control.signal))
          if(!control.signal.aborted)setResult({source,revision,bots:[...bots]})
        }catch{/* A missing or inaccessible profile keeps its fallback label. */}
      }
    })()
    return()=>control.abort()
  },[source,revision,missing])
  return loaded
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
