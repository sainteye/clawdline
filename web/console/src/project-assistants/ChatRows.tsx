// A Bot's reply is Markdown, read as Claude Code's answers are elsewhere in the console:
// richText escapes everything first and leaves anything it does not recognise as text.
import {DecisionPointer} from "./DecisionCard"
import {MessageReactions} from "./MessageReactions"
import { botReplyHasBrowser } from "./bot-browser-entry"
import { catalogWord } from "../catalog"
import { ChevronIcon } from "../ui/ChevronIcon.js"
import {InputCard} from "./InputCard"
import {ArtifactIcon,type ArtifactComposerAccess} from "./ArtifactWorkspace"
import {OutputCard} from "./OutputCard"
import {OutputViewer} from "./OutputViewer"
import { BotMessageMarkdown } from "./BotMessageMarkdown"
import { richText } from "../legacy/js/view/markdown.js"
import { compactedNoticeText } from "./notice-text"
import { BotMentionText, type BotMentionCandidate } from "./BotMentionField"
import type {SourceClient} from "./client"
import {errorCode} from "./client"
import type * as W from "@clawdline/contract"
import { Fragment, useCallback, useEffect, useState } from "react"
import type React from "react"
import { buildTimelineChat, peerDispatchNoticeText, workReviewNotice, type ChatItem } from "./chat-items"
import { chatTimeFormat } from "./chat-time"
import { PeerDispatchStatusCard, WorkResultCard } from "./WorkResultCard"
import { usePeerDispatchCards } from "./BotsSettingsModal"
import { ExchangePanel } from "./ExchangePanel"
import { exchangeWords } from "./exchange-words"
import { BotAvatar } from "./BotAvatar"
import { exchangeBot, exchangeEntries, exchangePeer, exchangeSummary, loadExchangeBot } from "./exchanges"
import type { ExchangeScope } from "./exchanges"
import { sameConversation, sameBot } from "./view-state"
import type { Words } from "./words"
import { sessionReportLine } from "./session-report"
import { decisionReplyOf } from "./decision-reply"

function renderTimelineFact(item: Exclude<ChatItem,{kind:"message"}>, {
  timeFormat, reviews, w, language, source, onOpenWork, requestAbove, renderPermission, peerDispatchCards,
}: {
  timeFormat: Intl.DateTimeFormat; reviews: ReadonlyMap<string,{text?:string;details?:string}>; w: Words; language: string;
  source?: SourceClient; onOpenWork?: (work:W.ProjectAssistantWorkLink,view?:"session")=>void;
  requestAbove?: (work:W.ProjectAssistantWorkLink)=>React.ReactNode;
  renderPermission?: (offer:W.ProjectAssistantPermissionOffer)=>React.ReactNode; peerDispatchCards: boolean;
}) {
  const key = JSON.stringify(item.kind==="work"&&item.dispatch?["dispatch",item.dispatch.task]:[item.kind,item.id])
  if(item.kind==="separator")return <div className="pa-separator" key={key}><time dateTime={item.at}>{timeFormat.format(Date.parse(item.at))}</time></div>
  if(item.kind==="work")return <Fragment key={key}>{requestAbove?.(item.work)}<WorkResultCard work={item.work} review={reviews.get(item.work.task_id)} dispatch={item.dispatch} language={language} w={w} source={source} onOpen={onOpenWork}/></Fragment>
  if(item.kind==="dispatch")return <Fragment key={key}><PeerDispatchStatusCard dispatch={item.dispatch} showRequester={peerDispatchCards} w={w}/></Fragment>
  return <Fragment key={key}>{renderPermission?.(item.offer)}</Fragment>
}

const previewEntities:Record<string,string>={"&amp;":"&","&lt;":"<","&gt;":">","&quot;":'"',"&#39;":"'"}

function exchangePreview(text:string) {
  const lines=richText(text,{codeCopy:false})
    .replace(/<\/(?:p|h[1-6]|li|blockquote|pre|td|th)>|<br>|<li>/g,"\n")
    .replace(/<[^>]+>/g,"")
    .replace(/&(amp|lt|gt|quot|#39);/g,entity=>previewEntities[entity]!)
    .replace(/^[\s·]+/,"")
    .split("\n").map(line=>line.trim()).filter(Boolean)
  // Recognize ASCII and wide sentence punctuation in the original reply.
  const sentenceEnd=/[.?!:;\u3002\uFF01\uFF0E\uFF1A\uFF1B\uFF1F]$/u
  return lines.map((line,index)=>{
    if(index===0)return line
    return (sentenceEnd.test(lines[index-1]!)?" ":" · ")+line
  }).join("")
}

function systemNoticeText(message:W.ProjectAssistantMessage,compactTurn:boolean,w:Words) {
  if(message.kind!=="notice")return null
  // Older transcript pages carry the stored text even when their turn summary is no longer loaded.
  if(compactTurn||message.text==="Compact now / 立即壓縮")return w.compactNow
  if(message.notice?.key==="compacted"||!message.notice&&message.id.startsWith("compacted-"))return compactedNoticeText(message,w)
  return null
}

function systemNoticeLabels(messages:readonly W.ProjectAssistantMessage[],turns:readonly W.ProjectAssistantReadTurnSummary[],w:Words) {
  const compactRequests=new Set(turns.filter(turn=>turn.origin==="compact").map(turn=>turn.message_id))
  const labels=new Map<string,string>()
  for(const message of messages){
    const label=systemNoticeText(message,compactRequests.has(message.id),w)
    if(label)labels.set(message.id,label)
  }
  return labels
}

function ExchangeCard({bot,name,outgoing,reference,words,onOpen,children}: {
  bot?:W.ProjectAssistantBot;name:string;outgoing:boolean;reference:W.ProjectAssistantExchange
  words:ReturnType<typeof exchangeWords>;onOpen:()=>void;children?:React.ReactNode
}) {
  const failed=["failed","cancelled","unknown"].includes(reference.outcome)
  // Bind each middle dot to the preceding text so it cannot start a wrapped line.
  const preview=!outgoing&&reference.reply
    ?exchangeSummary({...reference,reply:exchangePreview(reference.reply)}).replaceAll(" · ","\u00a0· ")
    :""
  const direction=outgoing?words.to:words.from
  return <div className="pa-exchange-card">
    <button type="button" className="pa-exchange-entry" data-failed={failed||undefined} onClick={onOpen}
      aria-label={`${direction} ${name}`} aria-haspopup="dialog" title={words.open}>
      {failed&&<span aria-hidden="true">!</span>}<span>{direction}</span>
      {bot&&<BotAvatar appearance={bot.appearance} size={17}/>}<strong>{name}</strong><span aria-hidden="true">›</span>
    </button>
    {preview&&<p className="pa-exchange-summary">{preview}</p>}
    {children}
  </div>
}

function copyCodeFromMessage(e:React.MouseEvent) {
  const button=(e.target as Element).closest?.<HTMLButtonElement>("button.codecopy")
  const code=button?.getAttribute("data-code-copy")
  if(button&&code){
    void navigator.clipboard?.writeText(code).then(()=>{
      button.classList.add("copied")
      setTimeout(()=>button.classList.remove("copied"),1500)
    })
  }
}

export function ChatRows({requestAbove,onOpenBot,renderExchangeDecision,renderExchangeMemory,renderExchangeWork,timeline,turns=[],onQuote,onOpenWork,w,language,attachmentURL,source,botKey,browserTraces=[],onOpenBrowser,dismissActionsKey,arrivals,onArrivalEnd,afterMessage,renderPermission,roster,artifactComposer,mentions}: {requestAbove?:(work:W.ProjectAssistantWorkLink)=>React.ReactNode;mentions?:readonly BotMentionCandidate[];artifactComposer?:ArtifactComposerAccess;roster?:readonly W.ProjectAssistantBot[];dismissActionsKey?:string;browserTraces?:readonly W.ProjectAssistantTrace[];onOpenBrowser?:(opener:HTMLButtonElement)=>void;timeline:readonly W.ProjectAssistantReadViewItem[];turns?:readonly W.ProjectAssistantReadTurnSummary[];onOpenWork?:(work:W.ProjectAssistantWorkLink, view?:"session")=>void;onQuote?:(m:W.ProjectAssistantMessage)=>void;w:Words;language:string;attachmentURL?:(id:string)=>string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey;arrivals?:ReadonlySet<string>;onArrivalEnd?:(id:string)=>void;afterMessage?:(message:W.ProjectAssistantMessage)=>React.ReactNode;renderPermission?:(offer:W.ProjectAssistantPermissionOffer)=>React.ReactNode;onOpenBot?:(key:W.ProjectAssistantBotKey)=>void;renderExchangeDecision?:(exchangeID:string)=>React.ReactNode;renderExchangeMemory?:(exchange:W.ProjectAssistantExchange)=>React.ReactNode;renderExchangeWork?:(exchangeID:string,sender:W.ProjectAssistantBot|undefined,name:string)=>React.ReactNode}) {
  const messages=timeline.flatMap(row=>row.message?[row.message]:[])
  const byID=new Map(timeline.filter(row=>row.message).map(row=>[row.message!.id,row]))
  const artifacts=new Map<string,W.ProjectAssistantMessage>();for(const m of messages){if(m.output?.artifact_id)artifacts.set(m.output.artifact_id,m)}
  const workIDs=new Set(timeline.flatMap(row=>row.work?[row.work.task_id]:[]))
  const states=new Map(turns.map(turn=>[turn.id,turn.state]))
  const systemNotices=systemNoticeLabels(messages,turns,w)
  const reviews=new Map<string,{text?:string;details?:string}>()
  for(const message of messages){
    const notice=workReviewNotice(message)
    if(!notice)continue
    const state=states.get(message.turn_id)
    if(notice.key==="work_review_held")reviews.set(notice.task,{text:w.workReviewHeld,details:message.text})
    else if(notice.key==="work_review_waiting")reviews.set(notice.task,{text:w.workReviewQueued})
    else if(state==="queued"||state==="accepted"||state==="running"||state==="waiting_permission"){
      const text=notice.key==="restart_continuation"?w.workReviewResuming:state==="queued"||state==="accepted"?w.workReviewQueued:w.workReviewRunning
      reviews.set(notice.task,{text,details:notice.key==="work_review"?message.text:reviews.get(notice.task)?.details})
    }else if(state==="unknown"||state==="failed"||state==="cancelled")reviews.set(notice.task,{text:w.workReviewHeld})
    else reviews.set(notice.task,{details:notice.key==="work_review"?message.text:reviews.get(notice.task)?.details})
  }
  const timeFormat=chatTimeFormat(language),peerDispatchCards=usePeerDispatchCards(source)
  const chat=buildTimelineChat(timeline.filter(item=>{
    const message=item.message
    if(!message)return true
    if(message.output?.quiet||message.notice?.key==="restart_continuation")return false
    const review=workReviewNotice(message)
    return !review||!workIDs.has(review.task)&&review.key==="work_review_held"
  }),peerDispatchCards)
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
  const copy=(m:W.ProjectAssistantMessage)=>{void navigator.clipboard?.writeText(m.text).then(()=>setCopied(m.id),()=>{})}
  return <>{chat.map(item=>item.kind!=="message"?renderTimelineFact(item,{timeFormat,reviews,w,language,source,onOpenWork,requestAbove,renderPermission,peerDispatchCards})
    :item.message.exchange&&source&&botKey&&exchangeScope?(()=>{
    const entry=exchanges.get(item.message.exchange.id)
    if(!entry||first.has(entry.exchange.id)||!sameConversation(item.message.fence,exchangeScope))return null
    first.add(entry.exchange.id)
    const {exchange:reference}=entry,bot=exchangeBot(participants,exchangePeer(reference,botKey)),name=bot?.name||exchangeWord.unknownBot
    const outgoing=sameBot(reference.source.key,botKey)
    return <div key={item.id} className="pa-exchange-attribution" data-message-id={item.id} data-message-seq={item.message.seq}>
      <ExchangeCard bot={bot} name={name} outgoing={outgoing} reference={reference} words={exchangeWord} onOpen={()=>setExchange({message:item.message,source})}>
        {!outgoing&&renderExchangeWork?.(reference.id,bot,name)}
      </ExchangeCard>
      {outgoing&&reference.outcome==="needs_person"&&<DecisionPointer name={name} w={w} onOpen={onOpenBot?()=>onOpenBot(reference.target.key):undefined}/>}
      {!outgoing&&renderExchangeDecision?.(reference.id)}
      {outgoing&&renderExchangeMemory?.(reference)}
    </div>
  })():systemNotices.has(item.id)?<Fragment key={JSON.stringify([item.kind,item.id])}>
    <article className="pa-message pa-notice pa-system-notice" data-message-id={item.id} data-message-seq={item.message.seq}
      data-arrival={arrivals?.has(item.id)||undefined} onAnimationEnd={e=>{if(e.animationName==="pa-bubble-enter")onArrivalEnd?.(item.id)}}>
      <div className="pa-message-bubble"><p>{systemNotices.get(item.id)}</p></div>
    </article>
    {afterMessage?.(item.message)}
  </Fragment>:<Fragment key={JSON.stringify([item.kind,item.id])}><article data-arrival={arrivals?.has(item.id)||undefined} onAnimationEnd={e=>{if(e.animationName==="pa-bubble-enter")onArrivalEnd?.(item.id)}} data-message-id={item.id} data-message-seq={item.message.seq} onClick={copyCodeFromMessage} tabIndex={0} onKeyDown={(e:React.KeyboardEvent)=>{if((e.key==="Enter"||e.key===" ")&&e.target===e.currentTarget){e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}}} onContextMenu={e=>{e.preventDefault();e.currentTarget.querySelector<HTMLButtonElement>(".pa-message-more")?.click()}} className={`pa-message ${item.message.kind==="user"?"pa-mine":""} ${item.message.kind==="notice"?"pa-notice":""} ${item.message.kind==="work"?"pa-session-report":""} ${item.groupStart?"pa-group-start":""}`}>
    <div className="pa-message-bubble">{source&&item.message.files?.map(file=><InputCard key={file.id} message={item.message} file={file} source={source} w={w}/>)}{item.message.quote_message_id&&<blockquote>{systemNotices.get(item.message.quote_message_id)??byID.get(item.message.quote_message_id)?.message?.text??w.quoteMissing}</blockquote>}{(item.message.attachments?.length??0)>0&&<div className="pa-message-pictures">{item.message.attachments!.map(a=><BotPicture key={a.id} id={a.id} localURL={attachmentURL?.(a.id)??null} source={source} botKey={botKey} w={w}/>)}</div>}{item.message.output&&source&&<OutputCard artifactComposer={artifactComposer} message={item.message.output.artifact_id?(artifacts.get(item.message.output.artifact_id)??item.message):item.message} source={source} w={w}/>}
      {item.message.text&&!item.message.output&&(
        item.message.kind==="work"?<details>
          <summary><span className="pa-session-report-head"><span>{w.sessionReport}</span>{sessionReportLine(item.message.text)&&<span className="pa-session-report-line">{sessionReportLine(item.message.text)}</span>}</span><ChevronIcon direction="right"/></summary>
          <p>{item.message.text}</p>
        </details>:item.message.kind==="assistant"?<BotMessageMarkdown text={item.message.text} w={w}/>:
          foldedMessageBody(item.message,w)??<p>{item.dispatch?peerDispatchNoticeText(item.dispatch,w):item.message.kind==="notice"&&item.message.text.startsWith("[Clawdline restarted")?w.resumedNotice:compactedNoticeText(item.message,w)??<BotMentionText text={item.message.text} candidates={mentions}/>}</p>
      )}
      {byID.get(item.id)?.joined_turn_id&&<small className="pa-steered-status">{w.steeredMessage}</small>}
      {onOpenBrowser&&botReplyHasBrowser(item.message,browserTraces)&&<button type="button" className="pa-reply-browser pa-pill" onClick={event=>onOpenBrowser(event.currentTarget)}>{catalogWord("botBrowser","openReply")}</button>}
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
export function BotPicture({id,localURL,source,botKey,w}:{id:string;localURL:string|null;source?:SourceClient;botKey?:W.ProjectAssistantBotKey;w:Words}){
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

/** A message shown folded: a decision card's answer, or a held work review. */
function foldedMessageBody(message:W.ProjectAssistantMessage,w:Words){
  const reply=decisionReplyOf(message)
  if(reply)return <DecisionReplyBody reply={reply} w={w}/>
  if(workReviewNotice(message)?.key==="work_review_held")return <details><summary>{w.workReviewHeld}</summary><p>{message.text}</p></details>
  return null
}

/** A person's answer to a decision card: the card it answered, then the reply. */
function DecisionReplyBody({reply,w}:{reply:{question:string;answer:string};w:Words}){
  return <div className="pa-decision-reply">
    <span className="pa-decision-reply-label">{w.decisionReplyLabel}</span>
    <p className="pa-decision-reply-answer">{reply.answer}</p>
    {reply.question&&<details className="pa-decision-reply-question">
      <summary>{w.decisionReplyQuestion.replace("{text}",reply.question)}</summary>
      <p>{reply.question}</p>
    </details>}
  </div>
}
