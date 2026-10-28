import { SourceList } from "../support/sources.js"
import type { ReactNode } from "react"
import { ConversationAvatar } from "../support/avatars.js"
import { supportWord as w, supportError, type SupportWord } from "../support/words.js"
import { richText } from "../legacy/js/view/markdown.js"
import { SupportImageGallery } from "../support/images.js"
import type { RequestType } from "../support/model.js"
import { type CommunityTopic, type ProgressStage, type PublicMessage, topicClosed } from "./model.js"
export function CommunityIcon({type}:{type:RequestType}) {
  return <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">{type==="issue"?<><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="2" fill="currentColor" stroke="none"/></>:type==="feature_request"?<><path d="M9 18h6m-5 3h4M8.5 14.5a6 6 0 1 1 7 0c-.8.6-1.5 1.5-1.5 3.5h-4c0-2-.7-2.9-1.5-3.5Z"/></>:<path d="M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-6 4V6a2 2 0 0 1 2-2Z"/>}</svg>
}
export const typeWord=(type:RequestType):SupportWord=>type==="issue"?"communityIssues":type==="feature_request"?"communityIdeas":"communityQuestions"
export const actionWord=(type:RequestType):SupportWord=>type==="issue"?"communityReport":type==="feature_request"?"communitySuggest":"ask"
const stageWords:Record<ProgressStage,SupportWord>={triage:"communityStageTriage",needs_information:"communityStageNeedsInformation",confirmed:"communityStageConfirmed",discussion:"communityStageDiscussion",accepted:"communityStageAccepted",planned:"communityStagePlanned",in_progress:"communityStageInProgress",ready_for_release:"communityStageReady",released:"communityStageReleasedIssue",declined:"communityStageDeclined",duplicate:"communityStageDuplicate"}
export function stageWord(stage:ProgressStage,type:RequestType):SupportWord{return stage==="released"&&type==="feature_request"?"communityStageReleasedIdea":stageWords[stage]}
export function CommunityStatus({topic}:{topic:CommunityTopic}) {
  const complete=topicClosed(topic),checked=topic.request_type==="question"?(topic.state==="answered"||topic.state==="resolved"):topic.progress?.stage==="released",key=topic.progress?stageWord(topic.progress.stage,topic.request_type):(["queued","answering","needs_information","needs_human","answered","resolved"].includes(topic.state)?topic.state as SupportWord:"queued")
  return <span className={"community-state "+(checked?"is-complete":"")} role="status">{checked?<svg width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">{complete?<circle cx="10" cy="10" r="7.3"/>:null}<path d="m5 10 3.5 3.5 6.5-7"/></svg>:<span className="community-dot"/>}{w(key)}{topic.progress?.release_version?<small> · {topic.progress.release_version}</small>:null}</span>
}
export function CommunityLink({to,go,children,primary=false,current=false}:{to:string;go:(path:string)=>void;children:ReactNode;primary?:boolean;current?:boolean}) {
  return <a href={to} className={"help-btn "+(primary?"help-primary":"")} aria-current={current?"page":undefined} onClick={e=>{if(e.button===0&&!e.ctrlKey&&!e.metaKey&&!e.shiftKey&&!e.altKey){e.preventDefault();go(to)}}}>{children}</a>
}
export function CommunityError({code,retry}:{code:string|null;retry?:()=>void}) {
  if(!code)return null
  const key=code.includes("outcome_unknown")?"communityPending":/revision|conflict/.test(code)?"communityChanged":/locked/.test(code)?"communityLocked":/permission|forbidden|not_author/.test(code)?"communityNoPermission":null
  return <div className="help-error" role="alert"><p>{key?w(key):code.startsWith("support_")?supportError(code):w("communityUnavailable")}</p>{retry?<button className="help-btn" onClick={retry}>{w("retry")}</button>:null}</div>
}
export function CommunityMessage({message,origin,id,latest=false,original=false}:{message:PublicMessage;origin:string;id:string;latest?:boolean;original?:boolean}) {
  const name=message.kind==="agent"?w("agent"):message.kind==="operator"?w("operator"):message.author.display_name??w("communityMember")
  return <article className={"help-message "+(original?"help-card community-original":"")}>
    <div className="help-message-meta"><div className="help-message-author"><ConversationAvatar kind={message.kind} avatar={message.author.avatar_url}/><strong>{name}</strong>{message.kind!=="member"?<span className="community-role">{message.kind==="agent"?"AI":w("communityStaff")}</span>:null}</div><div className="help-message-stamp">{latest?<span className="help-latest-reply">{w("latestReply")}</span>:null}<small>{original?w("communityOriginal")+" · ":null}{new Date(message.at).toLocaleString()}</small></div></div>
    <div className="help-prose help-markdown" lang={message.original_language??message.locale} dangerouslySetInnerHTML={{__html:richText(message.body,{codeCopy:false})}}/>
    <SupportImageGallery images={message.images} origin={origin} question={id} admin={false} community/>
    <SourceList sources={message.sources}/>
  </article>
}
