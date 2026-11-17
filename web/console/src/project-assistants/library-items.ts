import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node loads the source extension.
import {sameConversation} from "./view-state.ts"

export type LibraryItem =
  | {kind:"output";key:string;message:W.ProjectAssistantMessage}
  | {kind:"input";key:string;message:W.ProjectAssistantMessage;file:W.ProjectAssistantInputFile}
  | {kind:"picture";key:string;message:W.ProjectAssistantMessage;attachment:W.ProjectAssistantAttachment}

/** One artifact row follows its latest publication; attachments retain their message scope. */
export function libraryItems(messages:readonly W.ProjectAssistantMessage[]):LibraryItem[] {
  const rows=new Map<string,LibraryItem>()
  for(const message of [...messages].sort((a,b)=>a.seq-b.seq)) {
    if(message.output){const key="output:"+(message.output.artifact_id||message.output.id);rows.set(key,{key,kind:"output",message})}
    for(const file of message.files??[]){const key=`input:${message.id}:${file.id}`;rows.set(key,{key,kind:"input",message,file})}
    for(const attachment of message.attachments??[]){const key=`picture:${message.id}:${attachment.id}`;rows.set(key,{key,kind:"picture",message,attachment})}
  }
  return [...rows.values()].sort((a,b)=>Date.parse(b.message.at)-Date.parse(a.message.at)||b.message.seq-a.message.seq||a.key.localeCompare(b.key))
}

export function libraryGroups(items:readonly LibraryItem[]):{date:string;items:LibraryItem[]}[] {
  const groups=new Map<string,LibraryItem[]>()
  for(const item of items){
    const d=new Date(item.message.at),date=`${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,"0")}-${String(d.getDate()).padStart(2,"0")}`
    const group=groups.get(date)??[];group.push(item);groups.set(date,group)
  }
  return [...groups].sort(([a],[b])=>b.localeCompare(a)).map(([date,items])=>({date,items}))
}

/** Re-read shared content changes without fetching for ordinary text/trace updates. */
export function libraryRevision(messages:readonly W.ProjectAssistantMessage[]):string {
  return JSON.stringify(messages.filter(m=>m.output||m.files?.length||m.attachments?.length).map(m=>[m.id,m.seq,m.output,m.files,m.attachments]))
}

/** A verified artifact read can refresh a title outside the latest chat page.
 * Only logical title metadata changes; original publication bytes and dates stay. */
export function resolvedLibraryTitle(messages:W.ProjectAssistantMessage[],scope:Pick<W.ProjectAssistantFence,"key"|"project"|"conversation_id">,output:W.ProjectAssistantOutput):W.ProjectAssistantMessage[] {
  if(!output.artifact_id)return messages
  let changed=false
  const next=messages.map(message=>{
    const old=message.output
    if(!old||old.artifact_id!==output.artifact_id||!sameConversation(message.fence,scope)||(output.title_version??0)<(old.title_version??0)||old.name===output.name&&old.title_version===output.title_version)return message
    changed=true
    return {...message,output:{...old,name:output.name,title_version:output.title_version}}
  })
  return changed?next:messages
}

/** Delayed history cannot replace a newer observed title with its old one. */
export function mergeLibraryMessages(previous:W.ProjectAssistantMessage[],incoming:W.ProjectAssistantMessage[]):W.ProjectAssistantMessage[] {
  const rows=new Map(previous.map(message=>[message.id,message]))
  for(let message of incoming){
    const old=rows.get(message.id),before=old?.output,next=message.output
    if(old&&before?.artifact_id&&next?.artifact_id===before.artifact_id&&sameConversation(old.fence,message.fence)&&(before.title_version??0)>(next.title_version??0))message={...message,output:{...next,name:before.name,title_version:before.title_version}}
    rows.set(message.id,message)
  }
  return [...rows.values()]
}
