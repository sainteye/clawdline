import type { Icon, ProjectAssistantBotSummary } from "@clawdline/contract"
import { ProjectMark } from "./ProjectMark"
import { BotAvatar } from "./BotAvatar"
import { botIdentity, canRead } from "./view-state"
import type { Words } from "./words"
import { rowActivity } from "./working"
export function BotRow({item,selected,onSelect,w,language,projectName,projectIcon,machineName}: {item:ProjectAssistantBotSummary;selected:boolean;onSelect:()=>void;w:Words;language:string;projectName?:string;projectIcon?:Icon;machineName?:string}) {
  const {bot}=item, preview=item.pending_permissions?w.pending:rowActivity(item.activity,w)||item.latest_message
  const stamp=Date.parse(item.activity_at),project=projectName||w.unnamedProject,agent=bot.agent==="claude"?"Claude Code":"Codex"
  const said=[bot.name,project,machineName,item.unread_count>0?`${w.unreadCount} ${item.unread_count}`:"",item.pending_permissions>0?`${w.pending} ${item.pending_permissions}`:""].filter(Boolean).join(", ")
  return <button type="button" className="pa-bot-row" data-bot-key={botIdentity(bot.key)} aria-current={selected||undefined} disabled={!canRead(item.source)} onClick={onSelect}
    aria-label={said}>
    <span className="pa-avatar-status"><BotAvatar appearance={bot.appearance}/>{item.pending_permissions>0&&<span className="pa-attention" aria-hidden="true">!</span>}</span>
    <span className="pa-row-body"><strong>{bot.name}</strong><span className={item.pending_permissions?"pa-warning":""}>{preview}</span><small className="pa-row-project"><ProjectMark icon={projectIcon} size={11}/><span>{[project,agent,machineName].filter(Boolean).join(" · ")}</span></small>{(!item.source.fresh||item.source.state!=="online")&&<small>{w[item.source.state]} · {w.stale}</small>}</span>
    <span className="pa-row-meta"><time dateTime={item.activity_at}>{Number.isFinite(stamp)?new Intl.DateTimeFormat(language,{hour:"numeric",minute:"2-digit"}).format(stamp):""}</time>{item.unread_count>0&&<span className="pa-unread">{item.unread_count}</span>}</span>
  </button>
}
