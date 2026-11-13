import type { ProjectAssistantBotSummary } from "@clawdline/contract"
import { BotAvatar } from "./BotAvatar"
import { botIdentity, canRead } from "./view-state"
import type { Words } from "./words"
export function BotRow({item,selected,onSelect,w,language,projectName}: {item:ProjectAssistantBotSummary;selected:boolean;onSelect:()=>void;w:Words;language:string;projectName?:string}) {
  const {bot}=item, preview=item.pending_permissions?w.pending:item.activity||item.latest_message
  const stamp=Date.parse(item.activity_at)
  return <button type="button" className="pa-bot-row" data-bot-key={botIdentity(bot.key)} aria-current={selected||undefined} disabled={!canRead(item.source)} onClick={onSelect}
    aria-label={`${bot.name}, ${bot.key.machine_id}, ${bot.project.project_id}, ${item.unread_count}, ${w.pending}: ${item.pending_permissions}`}>
    <span className="pa-avatar-status"><BotAvatar appearance={bot.appearance}/>{item.pending_permissions>0&&<span className="pa-attention" aria-hidden="true">!</span>}</span>
    <span className="pa-row-body"><strong>{bot.name}</strong><span className={item.pending_permissions?"pa-warning":""}>{preview}</span><small>{projectName||bot.project.project_id} · {bot.agent==="claude"?"Claude Code":"Codex"} · {bot.key.machine_id}</small>{(!item.source.fresh||item.source.state!=="online")&&<small>{w[item.source.state]} · {w.stale}</small>}</span>
    <span className="pa-row-meta"><time dateTime={item.activity_at}>{Number.isFinite(stamp)?new Intl.DateTimeFormat(language,{hour:"numeric",minute:"2-digit"}).format(stamp):""}</time>{item.unread_count>0&&<span className="pa-unread">{item.unread_count}</span>}</span>
  </button>
}
