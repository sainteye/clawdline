import type { Icon, ProjectAssistantBotSummary } from "@clawdline/contract"
import { ProjectMark } from "./ProjectMark"
import { BotAvatar } from "./BotAvatar"
import { botIdentity, canRead } from "./view-state"
import type { Words } from "./words"
import { rosterActivity } from "./working"
import {ThinkingOrb} from "./ThinkingOrb"
export function BotRow({item,selected,onSelect,w,language,projectName,projectIcon,machineName,grouped=false}: {item:ProjectAssistantBotSummary;grouped?:boolean;selected:boolean;onSelect:()=>void;w:Words;language:string;projectName?:string;projectIcon?:Icon;machineName?:string}) {
  const {bot}=item, activity=rosterActivity(item,w),preview=plainPreview(activity.caption),waiting=activity.state==="needsInput",working=activity.state==="working"
  const stamp=Date.parse(item.activity_at),project=projectName||w.unnamedProject,agent=bot.agent==="claude"?"Claude Code":"Codex"
  const said=[bot.name,project,machineName,preview,item.unread_count>0?`${w.unreadCount} ${item.unread_count}`:""].filter(Boolean).join(", ")
  return <button type="button" className="pa-bot-row" data-bot-key={botIdentity(bot.key)} data-status={activity.state} data-unread={item.unread_count>0||undefined} aria-current={selected||undefined} disabled={!canRead(item.source)} onClick={onSelect}
    aria-label={said}>
    <span className="pa-avatar-status"><BotAvatar appearance={bot.appearance} mood={waiting?"needsInput":working?"working":"idle"}/>{waiting?<span className="pa-attention" aria-hidden="true">!</span>:item.unread_count>0&&<span className="pa-unread-dot" aria-hidden="true"/>}</span>
    <span className="pa-row-body"><strong>{bot.name}</strong><span className={`pa-row-preview${waiting?" pa-warning":activity.state==="error"?" pa-row-failed":""}`} title={preview}>{(waiting||working)&&<ThinkingOrb state={waiting?"listening":"working"} size={waiting?16:13} color={waiting?"var(--warning,#F0A030)":"var(--pa-dim,#999)"}/>}<span>{preview}</span></span><small className="pa-row-project">{!grouped&&<ProjectMark icon={projectIcon} size={11}/>}<span>{[grouped?"":project,agent,grouped?"":machineName].filter(Boolean).join(" · ")}</span></small>{(!item.source.fresh||item.source.state!=="online")&&<small>{w[item.source.state]} · {w.stale}</small>}</span>
    <span className="pa-row-meta"><time dateTime={item.activity_at}>{Number.isFinite(stamp)?new Intl.DateTimeFormat(language,{hour:"numeric",minute:"2-digit"}).format(stamp):""}</time>{item.unread_count>0&&<span className="pa-unread">{item.unread_count}</span>}</span>
  </button>
}

/** A reply's first words as a one-line preview: Markdown marks are dropped, not shown. */
export function plainPreview(text:string){
  return text.replace(/\[([^\]]*)\]\([^)]*\)/g,"$1").replace(/^\s*(?:[-*+]|\d+\.|#{1,6}|>)\s+/gm,"").replace(/(\*\*|__|`)/g,"").replace(/\s+/g," ").trim()
}
