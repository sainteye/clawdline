import type * as W from "@clawdline/contract"
import type {SourceClient} from "./client"
// @ts-expect-error Node executes source extensions.
import {botIdentity} from "./view-state.ts"
// @ts-expect-error Node executes source extensions.
import {orderedKeys, type RosterState} from "./roster-state.ts"
// @ts-expect-error Node executes source extensions.
import {projectKey,pinnedGroupKey} from "./roster-groups.ts"

/** List projections are optional on older sources; never fetch a history per row. */
export type RosterRoom = W.ProjectAssistantGroup & {last_entry?:W.ProjectAssistantRoomEntry & {at?:string};room?:W.ProjectAssistantRoomState}
export type GroupRosterEntry = {source:SourceClient;group:RosterRoom;status:W.ProjectAssistantSourceStatus}
export type RosterConversation = {kind:"bot";key:string;machineID:string;projectID:string;item:W.ProjectAssistantBotSummary}|{kind:"group";key:string;machineID:string;projectID:string;entry:GroupRosterEntry}
export type ConversationSection = {key:string;kind:"pinned"|"project";machineID:string;items:RosterConversation[]}
// A group and a Bot may have the same source-qualified ID without sharing preferences.
export const groupConversationKey=(key:W.ProjectAssistantBotKey)=>JSON.stringify(["group",key.machine_id,key.bot_id])
export const GroupPreviewAvatars=4
export function groupPreviewMembers<T>(all:readonly T[]){
  const members=all.slice(0,GroupPreviewAvatars)
  return {members,more:all.length-members.length}
}

export function conversations(items:readonly W.ProjectAssistantBotSummary[],rooms:readonly GroupRosterEntry[]):RosterConversation[]{
  const rows:RosterConversation[]=[...items.map(item=>({kind:"bot" as const,key:botIdentity(item.bot.key),machineID:item.bot.key.machine_id,projectID:item.bot.project.project_id,item})),...rooms.filter(({group})=>!group.deleted).map(entry=>({kind:"group" as const,key:groupConversationKey(entry.group.key),machineID:entry.group.key.machine_id,projectID:entry.group.project.project_id,entry}))]
  const time=(row:RosterConversation)=>Date.parse(row.kind==="bot"?row.item.activity_at:row.entry.group.last_entry?.at??"")
  const dated=rows.filter(row=>Number.isFinite(time(row))).sort((a,b)=>time(b)-time(a))
  let next=0
  // An absent timestamp keeps its slot; no sequence or metadata date impersonates it.
  return rows.map(row=>Number.isFinite(time(row))?dated[next++]:row)
}
export function conversationSections(rows:readonly RosterConversation[],state:RosterState):ConversationSection[]{
  const byKey=new Map(rows.map(row=>[row.key,row])),sections=new Map<string,ConversationSection>()
  const pinned:ConversationSection={key:pinnedGroupKey,kind:"pinned",machineID:"",items:[]}
  // Input order remains stable when an old source has no last-message timestamp.
  for(const row of rows){
    if(state.pinnedBotKeys.includes(row.key))continue
    const key=projectKey(row.machineID,row.projectID)
    if(!sections.has(key))sections.set(key,{key,kind:"project",machineID:row.machineID,items:[]})
  }
  for(const key of orderedKeys([...byKey.keys()],state.botSortOrder)){
    const row=byKey.get(key)!
    if(state.pinnedBotKeys.includes(key))pinned.items.push(row)
    else sections.get(projectKey(row.machineID,row.projectID))!.items.push(row)
  }
  return [...(pinned.items.length?[pinned]:[]),...orderedKeys([...sections.keys()],state.projectSortOrder).map(key=>sections.get(key)!)]
}

export function groupLastSpeaker(group:RosterRoom,bots:readonly W.ProjectAssistantBot[],person:string):{speaker:string;text:string}|null{
  const line=group.last_entry
  if(!line||!line.text.trim()||!["person","member"].includes(line.kind))return null
  const speaker=line.kind==="person"?person:line.author?bots.find(bot=>botIdentity(bot.key)===botIdentity(line.author!))?.name??line.author.bot_id:""
  return speaker?{speaker,text:line.text}:null
}
