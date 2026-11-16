import type * as W from "@clawdline/contract"

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
