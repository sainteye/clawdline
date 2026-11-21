import { useId, useLayoutEffect, useRef, useState, type ChangeEvent, type KeyboardEvent, type ReactNode, type RefObject, type SyntheticEvent } from "react"
import type * as W from "@clawdline/contract"
import { BotAvatar } from "./BotAvatar"
import { AgentBadge, agentName } from "./AgentMark"
import { ProjectMark } from "./ProjectMark"
import { botMentionQuery, botMentionSegments, decodeBotMentions, encodeBotMentions, insertBotMention } from "./bot-mentions"
import type { Words } from "./words"
import "./bot-mentions.css"

/** A Bot this one may message, with what the @ list shows about it. */
export type BotMentionCandidate={id:string;name:string;appearance:W.ProjectAssistantAppearance;agent:W.ProjectAssistantAgent;project:string;projectIcon?:W.Icon}

/** Every visible Bot on the same machine except the sender itself. */
export function botMentionCandidates(self:W.ProjectAssistantBot,roster:readonly W.ProjectAssistantBot[]|undefined,project:(bot:W.ProjectAssistantBot)=>string,icon?:(bot:W.ProjectAssistantBot)=>W.Icon|undefined):BotMentionCandidate[] {
  const seen=new Set<string>()
  return (roster??[]).filter(bot=>!bot.hidden&&bot.key.machine_id===self.key.machine_id&&bot.key.bot_id!==self.key.bot_id&&!seen.has(bot.key.bot_id)&&!!seen.add(bot.key.bot_id))
    .map(bot=>({id:bot.key.bot_id,name:bot.name,appearance:bot.appearance,agent:bot.agent,project:project(bot),projectIcon:icon?.(bot)}))
}

/**
 * Wires the @ list into one textarea. The textarea shows @Name; onText
 * receives @[Name](bot:ID) so the parent's draft and saved text carry the ID.
 */
export function useBotMentions({text,onText,candidates,editor,w}:{text:string;onText:(text:string)=>void;candidates?:readonly BotMentionCandidate[];editor:RefObject<HTMLTextAreaElement|null>;w:Words}) {
  const listID=useId()
  const {display,picks}=decodeBotMentions(text)
  const [caret,setCaret]=useState<number|null>(null),[picked,setPicked]=useState(0),[dismissed,setDismissed]=useState<string|null>(null)
  const pendingCaret=useRef<number|null>(null)
  useLayoutEffect(()=>{
    const at=pendingCaret.current,el=editor.current
    if(at===null||!el)return
    pendingCaret.current=null
    el.focus({preventScroll:true});el.setSelectionRange(at,at);setCaret(at)
  })
  const active=!!candidates?.length&&caret!==null?botMentionQuery(display,caret):null
  const query=active?.query.toLowerCase()??""
  const options=active&&dismissed!==display?(candidates??[]).filter(c=>c.name.toLowerCase().includes(query)||c.project.toLowerCase().includes(query)):[]
  const open=!!active&&dismissed!==display
  const index=Math.min(picked,Math.max(0,options.length-1))
  const choose=(c:BotMentionCandidate)=>{
    if(!active||caret===null)return
    const next=insertBotMention(display,active.start,caret,c.name)
    pendingCaret.current=next.caret
    setPicked(0)
    onText(encodeBotMentions(next.display,[...picks.filter(p=>p.name!==c.name),{id:c.id,name:c.name}]))
  }
  const track=(e:SyntheticEvent<HTMLTextAreaElement>)=>setCaret(e.currentTarget.selectionStart===e.currentTarget.selectionEnd?e.currentTarget.selectionStart:null)
  const change=(e:ChangeEvent<HTMLTextAreaElement>)=>{setPicked(0);setDismissed(null);track(e);onText(encodeBotMentions(e.target.value,picks))}
  /** True when the key belonged to the open @ list. */
  const keyDown=(e:KeyboardEvent<HTMLTextAreaElement>):boolean=>{
    if(!open||e.nativeEvent.isComposing)return false
    if(e.key==="Escape"){e.preventDefault();setDismissed(display);return true}
    if(!options.length)return false
    if(e.key==="ArrowDown"||e.key==="ArrowUp"){e.preventDefault();setPicked((index+(e.key==="ArrowDown"?1:options.length-1))%options.length);return true}
    if(e.key==="Tab"||e.key==="Enter"&&!e.shiftKey&&!e.repeat){e.preventDefault();choose(options[index]);return true}
    return false
  }
  const textareaProps={value:display,onChange:change,onSelect:track,onClick:track,onKeyUp:track,onBlur:()=>setCaret(null),
    "aria-autocomplete":"list" as const,"aria-expanded":open&&options.length>0,"aria-controls":open&&options.length?listID:undefined,"aria-activedescendant":open&&options.length?`${listID}-${index}`:undefined}
  const picker:ReactNode=open?<div className="pa-mention-list" id={listID} role="listbox" aria-label={w.mentionBots}>
    <div className="pa-mention-heading" aria-hidden="true">{w.mentionBots}</div>
    {options.length?options.map((c,i)=><button key={c.id} id={`${listID}-${i}`} type="button" role="option" aria-selected={i===index} tabIndex={-1}
      onMouseDown={e=>e.preventDefault()} onMouseEnter={()=>setPicked(i)} onClick={()=>choose(c)}>
      <span className="pa-mention-avatar"><BotAvatar appearance={c.appearance} size={32}/><AgentBadge agent={c.agent}/></span>
      <span className="pa-mention-text"><strong>{c.name}</strong><small><ProjectMark icon={c.projectIcon} size={12}/><span>{c.project} · {agentName(c.agent)}</span></small></span>
    </button>):<p className="pa-mention-empty">{w.mentionNone}</p>}
  </div>:null
  /** Mirror content: highlights each intact @Name behind the transparent textarea text. */
  const marks:ReactNode=<>{botMentionSegments(encodeBotMentions(display,picks)).map((s,i)=>typeof s==="string"?s:<mark key={i} className="pa-mention-mark">@{s.name}</mark>)}{"​"}</>
  return {display,textareaProps,keyDown,picker,marks,open}
}

/** Shows saved text with each picked Bot as an inline avatar chip. */
export function BotMentionText({text,candidates}:{text:string;candidates?:readonly BotMentionCandidate[]}) {
  const segments=botMentionSegments(text)
  if(segments.every(s=>typeof s==="string"))return <>{text}</>
  return <>{segments.map((s,i)=>{
    if(typeof s==="string")return s
    const c=candidates?.find(c=>c.id===s.id)
    return <span key={i} className="pa-mention-chip" title={c?`${c.name} · ${c.project}`:s.name}>{c&&<BotAvatar appearance={c.appearance} size={16}/>}@{c?.name??s.name}</span>
  })}</>
}
