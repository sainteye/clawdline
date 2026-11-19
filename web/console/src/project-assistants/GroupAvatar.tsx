import type * as W from "@clawdline/contract"
import {BotAvatar} from "./BotAvatar"
import {groupPreviewMembers} from "./roster-conversations"
import {botIdentity} from "./view-state"

export function GroupAvatar({keys,bots,size}:{keys:readonly W.ProjectAssistantBotKey[];bots:readonly W.ProjectAssistantBot[];size:number}) {
  const {members,more}=groupPreviewMembers(keys)
  const tile=size/2
  return <span className="pa-group-avatars" style={{width:size,height:size,gridTemplateColumns:`repeat(2,${tile}px)`}} aria-hidden="true">
    {members.length===0&&<span className="pa-group-avatar-empty" style={{width:size,height:size}}><GroupActionIcon group size={tile}/></span>}
    {members.map(key=>{
      const bot=bots.find(bot=>botIdentity(bot.key)===botIdentity(key))
      return <span key={botIdentity(key)} style={{width:tile,height:tile}}>{bot?<BotAvatar appearance={bot.appearance} size={tile}/>:<span className="pa-group-unknown">?</span>}</span>
    })}
    {more>0&&<span className="pa-group-more">+{more}</span>}
  </span>
}

export function GroupActionIcon({group=false,size=16}:{group?:boolean;size?:number}) {
  return <svg viewBox="0 0 20 20" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {group?<><circle cx="7" cy="6" r="2.7"/><path d="M2.5 16v-1a4.5 4.5 0 0 1 9 0v1M13 3.5a2.7 2.7 0 0 1 0 5.2m.8 2.3a4.5 4.5 0 0 1 3.7 4v1"/></>:<path d="M10 4v12M4 10h12"/>}
  </svg>
}
