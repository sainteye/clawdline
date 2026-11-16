import { useEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { authorized, command, errorCode, type SourceClient } from "./client"
import { canWrite } from "./view-state"
import { GroupEditor, TeamApprovals, groupWord } from "./Groups"
import { EventRefreshMs } from "./Chat"

const identity=(k:W.ProjectAssistantBotKey)=>JSON.stringify([k.machine_id,k.bot_id])
export function GroupRoom({source,current,group:initial,bots,projectNames,onClose,onChanged}:{source:SourceClient;current:()=>readonly SourceClient[];group:W.ProjectAssistantGroup;bots:readonly W.ProjectAssistantBot[];projectNames:ReadonlyMap<string,string>;onClose:()=>void;onChanged:(group:W.ProjectAssistantGroup)=>void}) {
  const [group,setGroup]=useState(initial),[page,setPage]=useState<W.ProjectAssistantGroupHistoryResponse|null>(null),[lines,setLines]=useState<W.ProjectAssistantRoomEntry[]>([]),[text,setText]=useState(""),[reply,setReply]=useState<W.ProjectAssistantBotKey|undefined>(),[error,setError]=useState(""),[busy,setBusy]=useState(false),[unknown,setUnknown]=useState(false),[editing,setEditing]=useState(false),[deleting,setDeleting]=useState(false)
  const pending=useRef<W.ProjectAssistantGroupSendRequest|null>(null),control=useRef(new AbortController()),refresh=useRef<()=>Promise<void>>(async()=>{})
  const currentRef=useRef(current);currentRef.current=current
  const scroll=useRef<HTMLDivElement>(null),[more,setMore]=useState<{before:number;hasMore:boolean}|null>(null)
  const linesRef=useRef(lines),moreRef=useRef(more)
  const rememberMore=(value:{before:number;hasMore:boolean})=>{moreRef.current=value;setMore(value)}
  const merge=(previous:W.ProjectAssistantRoomEntry[],incoming:W.ProjectAssistantRoomEntry[])=>[...new Map([...previous,...incoming].map(l=>[l.seq,l])).values()].sort((a,b)=>a.seq-b.seq)
  async function load(before=0){
    try {
      const answer=await authorized(source,currentRef.current,signal=>source.groupHistory!({key:initial.key,before_seq:before,page_size:100},signal),control.current.signal)
      if(control.current.signal.aborted)return
      const previous=linesRef.current,last=previous.at(-1)?.seq??0
      const gap=!before&&previous.length>0&&answer.lines.length>0&&answer.lines[0].seq>last+1
      const next=gap?answer.lines:merge(previous,answer.lines);linesRef.current=next
      setPage(answer);setGroup(old=>answer.group.version>=old.version?answer.group:old);setLines(next);if(before||!moreRef.current||gap)rememberMore({before:answer.next_before_seq,hasMore:answer.has_more});setError("")
      if(answer.group.deleted)onChanged(answer.group)
    }catch(e){if(!control.current.signal.aborted){const code=errorCode(e);setError(code);setPage(null);if(["pa_revoked","pa_scope_denied","pa_unpaired"].includes(code)){linesRef.current=[];setLines([])}}}
  }
  refresh.current=()=>load()
  useEffect(()=>{
    control.current=new AbortController();void refresh.current();const timer=setInterval(()=>void refresh.current(),EventRefreshMs)
    return()=>{control.current.abort();clearInterval(timer)}
  },[source,initial.key.machine_id,initial.key.bot_id])
  const writable=!!page&&canWrite(page.source)&&source.current?.()!==false&&!group.deleted
  const author=(key?:W.ProjectAssistantBotKey)=>key?(bots.find(b=>identity(b.key)===identity(key))?.name??key.bot_id):groupWord("groupPerson")
  async function effect(run:(signal:AbortSignal)=>Promise<unknown>){
    if(!writable||busy||unknown)return
    setBusy(true);setError("")
    try {await authorized(source,currentRef.current,run,control.current.signal);await load()}
    catch(e){const code=errorCode(e);setError(code);if(code==="pa_outcome_unknown")setUnknown(true)}finally{setBusy(false)}
  }
  async function send(){
    if(!text.trim()||!writable||busy||unknown)return
    const request=pending.current??{command:command(group.key,"send"),text,...(reply?{reply_to_author:reply}:{})}
    pending.current=request
    await effect(async signal=>{const answer=await source.sendGroup!(request,signal);pending.current=null;setText("");setReply(undefined);const next=merge(linesRef.current,[answer.entry]);linesRef.current=next;setLines(next);requestAnimationFrame(()=>scroll.current?.scrollTo({top:scroll.current.scrollHeight}));return answer})
  }
  return <section className="pa-form-panel pa-chat" role="dialog" aria-modal="true" aria-label={group.name} style={{maxWidth:"min(900px, 100%)",padding:0,zIndex:8}}>
    <header className="pa-chat-header"><button type="button" onClick={onClose}>{groupWord("groupBack")}</button><div><h2>{group.name}</h2><small>{group.members.map(m=>author(m.key)).join(" · ")}</small></div><button type="button" disabled={!writable||busy||unknown} onClick={()=>setEditing(true)}>{groupWord("groupEdit")}</button></header>
    <details style={{padding:"8px 20px"}}><summary>{groupWord("groupMembers")} ({group.members.length})</summary><ul>{group.members.map(m=><li key={identity(m.key)}>{author(m.key)}</li>)}</ul></details>
    <div className="pa-chat-scroll" ref={scroll}><div className="pa-chat-content">
      <TeamApprovals sources={[source]} bots={page?.room.running?bots.filter(bot=>group.members.some(m=>identity(m.key)===identity(bot.key))):[]}/>
      {more?.hasMore&&<button type="button" disabled={busy} onClick={()=>void load(more.before)}>{groupWord("groupOlder")}</button>}
      {lines.map(line=><article key={line.seq} className={line.kind==="person"?"pa-message pa-person":"pa-message pa-assistant"} data-room-seq={line.seq}><strong>{line.kind==="person"?groupWord("groupPerson"):author(line.author)}</strong>{line.reply_to_author&&<small> · {groupWord("groupReplyTo")} {author(line.reply_to_author)}</small>}<p style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{line.text}</p>{line.kind==="member"&&line.author&&<button type="button" disabled={!writable||busy||unknown} onClick={()=>setReply(line.author)}>{groupWord("groupReply")}</button>}</article>)}
      {!lines.length&&<p>{groupWord("groupEmpty")}</p>}
    </div></div>
    <footer className="pa-chat-footer">
      {!!page?.room.running&&<p role="status">{groupWord("groupRunning")}</p>}
      {reply&&<p>{groupWord("groupReplyTo")} {author(reply)} <button type="button" disabled={busy||unknown} onClick={()=>{setReply(undefined);pending.current=null}}>{groupWord("groupCancel")}</button></p>}
      <label>{groupWord("groupMessage")}<textarea value={text} disabled={!writable||busy||unknown} onChange={e=>{setText(e.target.value);pending.current=null}}/></label>
      <div className="pa-chat-tools"><button type="button" disabled={!writable||busy||unknown||!text.trim()} onClick={()=>void send()}>{groupWord(busy?"groupWorking":"groupSend")}</button><button type="button" disabled={!writable||busy||unknown} onClick={()=>void effect(signal=>source.stopGroup!({command:command(group.key,"stop")},signal))}>{groupWord("groupStop")}</button><button type="button" disabled={!writable||busy||unknown} onClick={()=>setDeleting(true)}>{groupWord("groupDelete")}</button></div>
      {deleting&&<div role="alert"><p>{groupWord("groupDeleteHint")}</p><button type="button" disabled={busy||unknown} onClick={()=>setDeleting(false)}>{groupWord("groupCancel")}</button><button type="button" disabled={busy||unknown} onClick={()=>void effect(async signal=>{const result=await source.deleteGroup!({command:command(group.key,"delete"),expected_version:group.version},signal);onChanged({...group,deleted:true});return result})}>{groupWord("groupDeleteConfirm")}</button></div>}
      {(error||unknown)&&<p role="alert">{groupWord(unknown?"groupUnknown":"groupUnavailable")} {error&&`(${error})`}</p>}
    </footer>
    {editing&&page&&<GroupEditor sources={[{source,groups:[group],status:page.source}]} bots={bots} current={current} projectNames={projectNames} group={group} onClose={()=>setEditing(false)} onSaved={(_,changed)=>{setGroup(changed);setEditing(false);onChanged(changed);void load()}}/>}
  </section>
}
