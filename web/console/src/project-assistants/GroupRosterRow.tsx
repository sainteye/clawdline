import type * as W from "@clawdline/contract"
import {GroupAvatar} from "./GroupAvatar"
import {plainPreview} from "./BotRow"
import {canRead,botIdentity} from "./view-state"
import {groupWord} from "./Groups"
import {groupLastSpeaker,type GroupRosterEntry} from "./roster-conversations"

export function GroupRosterRow({entry,bots,selected,onSelect,projectName,machineName,grouped=false}:{entry:GroupRosterEntry;bots:readonly W.ProjectAssistantBot[];selected:boolean;onSelect:(target:HTMLButtonElement)=>void;projectName?:string;machineName?:string;grouped?:boolean}){
  const {group,status}=entry,last=groupLastSpeaker(group,bots,groupWord("groupPerson"))
  const preview=last?`${last.speaker}: ${plainPreview(last.text)}`:[grouped?undefined:projectName,`${group.members.length} ${groupWord("groupMembers")}`].filter(Boolean).join(" · ")
  return <button type="button" className="pa-bot-row pa-group-row" data-group-key={botIdentity(group.key)} aria-current={selected||undefined} disabled={!canRead(status)} onClick={event=>onSelect(event.currentTarget)} aria-label={`${group.name}, ${groupWord("groups")}, ${group.members.length} ${groupWord("groupMembers")}, ${preview}`}>
    <GroupAvatar keys={group.members.map(member=>member.key)} bots={bots} size={44}/>
    <span className="pa-row-body"><strong>{group.name}</strong><span className="pa-row-preview" title={preview}><span>{preview}</span></span>{!grouped&&(projectName||machineName)&&<small>{[projectName,machineName].filter(Boolean).join(" · ")}</small>}{(!status.fresh||status.state!=="online")&&<small>{groupWord("stale")}</small>}</span>
  </button>
}
