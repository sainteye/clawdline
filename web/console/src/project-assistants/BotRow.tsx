import type { Icon, ProjectAssistantBotSummary } from "@clawdline/contract"
import { lastActivityRelative } from "../relative-time"
import { nextWord } from "../next-strings"
import { ProjectMark } from "./ProjectMark"
import { BotAvatar } from "./BotAvatar"
import { AgentBadge } from "./AgentMark"
import { botIdentity, canRead } from "./view-state"
import type { Words } from "./words"
import { botRosterActivity, dispatchedCount, hasExecutingDispatch } from "./working"
import {ThinkingOrb} from "./ThinkingOrb"
import { plainPreview } from "./session-report"
export { plainPreview } from "./session-report"
export function BotRow({item,selected,onSelect,w,language,now,projectName,projectIcon,machineName,grouped=false,showDispatched=false}: {item:ProjectAssistantBotSummary;grouped?:boolean;showDispatched?:boolean;selected:boolean;onSelect:()=>void;w:Words;language:string;now:number;projectName?:string;projectIcon?:Icon;machineName?:string}) {
  const {bot}=item, activity=botRosterActivity(item,w,showDispatched),clones=showDispatched&&activity.live?dispatchedCount(item):0,preview=plainPreview(activity.caption),waiting=activity.state==="needsInput",working=activity.state==="working",overseeing=activity.state==="waitingOnWork"
  const showOrb = waiting || (working && !activity.queuedOnly) ||
    (overseeing && hasExecutingDispatch(item.dispatched_work))
  // A grey dot: news that does not need the person, shown only without a count.
  const activityOnly=!(item.unread_count>0)&&item.has_activity===true
  const relative=lastActivityRelative(Date.parse(item.activity_at)/1000,now,language),project=projectName||w.unnamedProject
  const said=[bot.name,bot.lead?w.rosterLead:"",project,machineName,preview,relative?nextWord("sessionLastActivity",{time:relative}):"",item.unread_count>0?`${w.unreadCount} ${item.unread_count}`:activityOnly?w.rosterNewActivity:"",clones>0?w.rosterDispatchedCount.replace("{count}",String(clones)):""].filter(Boolean).join(", ")
  return <button type="button" className="pa-bot-row" data-bot-key={botIdentity(bot.key)} data-status={activity.state} data-unread={item.unread_count>0||undefined} data-activity={activityOnly||undefined} aria-current={selected||undefined} disabled={!canRead(item.source)} onClick={onSelect}
    aria-label={said}>
    <span className="pa-avatar-status"><BotAvatar appearance={bot.appearance} mood={waiting?"needsInput":working?"working":overseeing?"waitingOnWork":"idle"}/><AgentBadge agent={bot.agent}/>{bot.lead&&<span className="pa-roster-lead" aria-hidden="true">★</span>}{waiting?<span className="pa-attention" aria-hidden="true">!</span>:item.unread_count>0?<span className="pa-unread-dot" aria-hidden="true"/>:activityOnly&&<span className="pa-activity-dot" aria-hidden="true"/>}</span>
    <span className="pa-row-body pa-row-grid"><strong>{bot.name}</strong>{relative&&<time className="pa-row-time" dateTime={item.activity_at}>{relative}</time>}<span className={`pa-row-preview${waiting?" pa-warning":activity.state==="error"?" pa-row-failed":""}`} title={preview}>{showOrb&&<ThinkingOrb state={waiting?"listening":"working"} size={waiting?16:13} color={waiting?"var(--warning,#F0A030)":"var(--pa-dim,#999)"}/>}<span>{preview}</span></span>{(clones>0||item.unread_count>0)&&<span className="pa-row-badges">{clones>0&&<span className="pa-clones" title={w.rosterDispatchedCount.replace("{count}",String(clones))}><CloneMark/>{clones}</span>}{item.unread_count>0&&<span className="pa-unread">{item.unread_count}</span>}</span>}{(!grouped||machineName)&&<small className="pa-row-project">{!grouped&&<ProjectMark icon={projectIcon} size={11}/>}<span>{[grouped?"":project,machineName].filter(Boolean).join(" · ")}</span></small>}</span>
  </button>
}

/** Two outlines, one behind the other: the same 分身 the avatar's echo draws. */
export function CloneMark(){
  return <svg className="pa-clone-mark" width="13" height="11" viewBox="0 0 13 11" aria-hidden="true" focusable="false"><rect x="4.5" y="0.75" width="7.75" height="6.5" rx="2.4" fill="none" stroke="currentColor" strokeWidth="1.1" opacity=".55"/><rect x="0.75" y="3.75" width="7.75" height="6.5" rx="2.4" fill="var(--pa-panel,#fff)" stroke="currentColor" strokeWidth="1.3"/></svg>
}

